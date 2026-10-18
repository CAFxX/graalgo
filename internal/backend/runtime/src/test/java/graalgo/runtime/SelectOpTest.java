package graalgo.runtime;

import java.util.concurrent.atomic.AtomicReference;

import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;

/**
 * Direct unit tests for {@link SelectOp} (RV22-P1-5).
 *
 * <p>Covers the paths no Go-level test can reach deterministically:
 * <ol>
 *   <li>Spurious wakeup: a park return with no waker must fail loudly
 *       ({@code IllegalStateException}), never return -1 as a case index.
 *   <li>Close()-poison: {@code close()} on a channel with a blocked select
 *       send waiter poisons the op; {@code execute()} must raise the Go
 *       panic {@code "send on closed channel"} on resume.
 *   <li>Claimed-by-other: a waker that wins the shared claim while the
 *       selector is mid-sweep drives the {@code claimedByOther} path;
 *       {@code execute()} must return the waker's case index and payload.
 * </ol>
 *
 * <p>Red-green discipline: {@code testSpuriousWakeupFailsLoud} fails
 * against the pre-fix implementation (it returned -1 silently); the other
 * two lock in the poison and claim-handoff contracts.
 */
public final class SelectOpTest {

    private SchedulerCore sched;

    // ---------- harness (mirrors ChannelTest) ----------

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(Object expected, Object actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /** Runs body with an installed scheduler, then awaits quiescence. */
    private void withScheduler(int gmp, ThrowingRunnable body) throws Exception {
        sched = new SchedulerCore(gmp, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            body.run();
            awaitQuiescence(15000);
        } finally {
            SchedulerCore.install(null);
        }
    }

    /** Quiescence with a hang diagnosis (lost wakeup => loud failure). */
    private void awaitQuiescence(long timeoutMs) throws Exception {
        long deadline = System.currentTimeMillis() + timeoutMs;
        while (sched.runnableCount() != 0) {
            if (System.currentTimeMillis() > deadline) {
                throw new AssertionError(
                        "quiescence timeout: " + sched.runnableCount()
                        + " goroutines still live — lost wakeup?");
            }
            Thread.sleep(10);
        }
        sched.awaitQuiescence();
    }

    /** Polls cond until true or timeout (for cross-goroutine assertions). */
    private static void waitFor(java.util.function.BooleanSupplier cond, String what)
            throws Exception {
        long deadline = System.currentTimeMillis() + 10000;
        while (!cond.getAsBoolean()) {
            if (System.currentTimeMillis() > deadline) {
                throw new AssertionError("timeout waiting for: " + what);
            }
            Thread.sleep(5);
        }
    }

    private interface ThrowingRunnable {
        void run() throws Exception;
    }

    // ---------- 1. spurious wakeup: -1 must never escape ----------

    /**
     * A park return with no waker (resultIndex still -1, no poison) fails
     * loudly instead of returning -1 as a case index. The unpark is issued
     * with no claim won and no result set; the drainer stashes a
     * pre-park unpark, so the test is order-independent.
     */
    public void testSpuriousWakeupFailsLoud() throws Exception {
        AtomicReference<Throwable> err = new AtomicReference<>();
        AtomicReference<Integer> idx = new AtomicReference<>();
        withScheduler(2, () -> {
            GoChan a = new GoChan(0, 0L); // never ready: unbuffered, no peer
            SelectOp op = new SelectOp(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(a, false, null)});
            GoroutineHandle h = sched.spawn(() -> {
                try {
                    idx.set(op.execute());
                } catch (Throwable t) {
                    err.set(t);
                }
            });
            // No claim won, no setResult: a bare wakeup, exactly what the
            // exactly-once-unpark invariant says cannot happen.
            sched.unpark(h);
        });
        assertTrue(idx.get() == null, "execute() must not return on a waker-less wakeup");
        assertTrue(err.get() instanceof IllegalStateException,
                "waker-less wakeup must throw IllegalStateException, got: " + err.get());
        assertTrue(err.get().getMessage().contains("no winning case"),
                "exception must name the invariant, got: " + err.get().getMessage());
    }

    // ---------- 2. close() poisons a blocked select send case ----------

    /**
     * {@code close()} on a channel with a parked select send waiter
     * poisons the op ({@code GoChan.close} &rarr; {@code setPoison}); the
     * resumed {@code execute()} raises the Go panic "send on closed
     * channel".
     */
    public void testClosePoisonsBlockedSelectSend() throws Exception {
        AtomicReference<Object> panicVal = new AtomicReference<>();
        withScheduler(2, () -> {
            GoChan a = new GoChan(0, 0L); // unbuffered: send case never ready
            SelectOp op = new SelectOp(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(a, true, 42L)});
            sched.spawn(() -> {
                try {
                    op.execute();
                    panicVal.set("NO_PANIC");
                } catch (PanicException pe) {
                    panicVal.set(pe.goValue().toString());
                }
            });
            waitFor(() -> a.sendWaitersCount() == 1, "select send waiter parked");
            ChanOps.close(a);
        });
        assertEquals("send on closed channel", panicVal.get(),
                "close() must poison the blocked select send case");
    }

    // ---------- 3. claimedByOther: waker wins the claim mid-sweep ----------

    /**
     * A waker that wins the shared claim while the selector is mid-sweep
     * drives the {@code claimedByOther} path: the selector parks, consumes
     * the in-flight unpark, and returns the waker's case index and
     * payload.
     *
     * <p>The interleaving is constructed deterministically: the test
     * thread plays the waker and wins the claim (via a real
     * {@code SelectCaseWaiter.claim()} CAS) <em>before</em> the selector
     * goroutine exists, so its sweep must observe the lost self-claim.
     * The triple {@code claim()} &rarr; {@code setResult()} &rarr;
     * {@code unpark()} is exactly what a real channel waker performs
     * ({@code GoChan} send/recv/close paths). The waiter carries a null
     * handle because no sweep-created waiter exists yet; the handle is
     * never dereferenced on this path.
     */
    public void testClaimedByOtherReturnsWakerCase() throws Exception {
        AtomicReference<Integer> idx = new AtomicReference<>();
        AtomicReference<Throwable> err = new AtomicReference<>();
        withScheduler(2, () -> {
            GoChan a = new GoChan(0, 0L);
            GoChan b = new GoChan(0, 0L);
            SelectOp op = new SelectOp(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(a, false, null),
                    new ChanOps.SelectCase(b, false, null)});
            // The waker wins the shared claim before the selector runs.
            SelectOp.SelectCaseWaiter w =
                    new SelectOp.SelectCaseWaiter(op, 1, b, false, null, null);
            assertTrue(w.claim(), "test waker must win the uncontended claim");
            op.setResult(1, 99L, true);
            GoroutineHandle h = sched.spawn(() -> {
                try {
                    idx.set(op.execute());
                } catch (Throwable t) {
                    err.set(t);
                }
            });
            // Winner's unpark, in flight before the selector parks (stashed
            // by the drainer if it arrives early — no wakeup lost).
            sched.unpark(h);
        });
        assertTrue(err.get() == null, "execute() must not throw, got: " + err.get());
        assertEquals(1, idx.get(), "claimedByOther must return the waker's case index");
    }

    /**
     * The waker's payload is delivered with the claimed case: value and ok
     * stashed by {@code setResult} are visible after {@code execute()}
     * returns through the {@code claimedByOther} path.
     */
    public void testClaimedByOtherDeliversPayload() throws Exception {
        AtomicReference<Throwable> err = new AtomicReference<>();
        SelectOp[] opBox = new SelectOp[1];
        withScheduler(2, () -> {
            GoChan a = new GoChan(0, 0L);
            GoChan b = new GoChan(0, 0L);
            SelectOp op = new SelectOp(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(a, false, null),
                    new ChanOps.SelectCase(b, false, null)});
            opBox[0] = op;
            SelectOp.SelectCaseWaiter w =
                    new SelectOp.SelectCaseWaiter(op, 0, a, false, null, null);
            assertTrue(w.claim(), "test waker must win the uncontended claim");
            op.setResult(0, 7L, false);
            GoroutineHandle h = sched.spawn(() -> {
                try {
                    op.execute();
                } catch (Throwable t) {
                    err.set(t);
                }
            });
            sched.unpark(h);
        });
        assertTrue(err.get() == null, "execute() must not throw, got: " + err.get());
        assertEquals(7L, opBox[0].resultValue(), "waker's value must be stashed");
        assertEquals(false, opBox[0].resultOk(), "waker's ok must be stashed");
    }
}
