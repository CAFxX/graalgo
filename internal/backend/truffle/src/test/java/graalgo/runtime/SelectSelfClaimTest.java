package graalgo.runtime;

import java.lang.reflect.Field;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.jfr.ParkReason;
import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;

/**
 * White-box regression tests for the select self-claim lost-wakeup bug
 * (CI run 41943236460: ChannelTest.testSelectRacyCompleteNoStaleWakeup
 * "timeout waiting for: select done").
 *
 * <p>Root cause: while a select sweeps, it transiently holds a SELF_CLAIM
 * on its shared claim (one channel at a time, under that channel's
 * monitor). A waker on another channel that observes the claim taken
 * must NOT drop the select's waiter, and must NOT treat the queue as
 * empty and park: unlike a claim won by a waker (unpark in flight), a
 * self-claim carries no wakeup. The v2 contract, tested here:
 * <ul>
 *   <li>{@code trySend}/{@code tryRecv} report RETRY (not WOULD_BLOCK)
 *       when the only waiter is self-claimed and the buffer cannot
 *       complete the operation — and complete via the buffer even past
 *       a self-claimed waiter when it can.</li>
 *   <li>{@code close()} RETRIES a self-claimed waiter outside the
 *       monitor until its transient self-claim resolves (claiming it
 *       then), instead of dropping it or returning with it stranded.</li>
 *   <li>{@code canSend}/{@code canRecv} never purge a self-claimed
 *       waiter (purge is waker-claim-only).</li>
 * </ul>
 *
 * <p>Each test constructs the self-claimed state directly (via
 * reflection on the private claim field — the only reflective access).
 * Tests that never deliver to a waiter use null handles (unpark is only
 * called for accepted waiters); the close-retry test uses a real
 * spawned goroutine's handle so the claiming unpark is delivered and
 * observed. Every test fails on a mutant that drops/ignores the
 * self-claimed waiter (see the lane report for the mutant run).
 */
public final class SelectSelfClaimTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(long expected, long actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /** Puts op's shared claim into the SELF_CLAIM state. */
    private static void setSelfClaim(SelectOp op) throws Exception {
        setClaim(op, "SELF_CLAIM");
    }

    /** Releases op's shared claim back to UNCLAIMED. */
    private static void setUnclaimed(SelectOp op) throws Exception {
        setClaim(op, "UNCLAIMED");
    }

    private static void setClaim(SelectOp op, String fieldName) throws Exception {
        Field claimField = SelectOp.class.getDeclaredField("claim");
        claimField.setAccessible(true);
        @SuppressWarnings("unchecked")
        AtomicReference<Object> claim =
                (AtomicReference<Object>) claimField.get(op);
        Field sentinel = SelectOp.class.getDeclaredField(fieldName);
        sentinel.setAccessible(true);
        claim.set(sentinel.get(null));
    }

    /** Enqueues one select waiter for cases[0] on ch (monitor taken here). */
    private static SelectOp.SelectCaseWaiter enqueueWaiter(SelectOp op, GoChan ch,
            boolean isSend, Object sendValue, GoroutineHandle handle) {
        SelectOp.SelectCaseWaiter w = new SelectOp.SelectCaseWaiter(
                op, 0, ch, isSend, sendValue, handle);
        synchronized (ch) {
            ch.enqueueSelectWaiter(w, isSend);
        }
        return w;
    }

    private static SchedulerCore installScheduler() {
        SchedulerCore sched = new SchedulerCore(2, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        return sched;
    }

    private static void awaitQuiescence(SchedulerCore sched) throws Exception {
        long deadline = System.currentTimeMillis() + 5000;
        while (sched.runnableCount() != 0) {
            if (System.currentTimeMillis() > deadline) {
                throw new AssertionError(
                        "quiescence timeout: " + sched.runnableCount() + " live");
            }
            Thread.sleep(5);
        }
    }

    /**
     * trySend reports RETRY (not WOULD_BLOCK) when the buffer is full
     * and the only recv waiter is self-claimed; the waiter stays
     * enqueued and the buffer is untouched.
     */
    public void testTrySendReturnsRetryWhenBlockedBySelfClaim() throws Exception {
        SchedulerCore sched = installScheduler();
        try {
            GoChan ch = new GoChan(1, 0L);
            assertTrue(ch.trySend(1L, sched) == GoChan.SendAttempt.SENT,
                    "buffer fill must send");
            SelectOp op = new SelectOp(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(ch, false, null)});
            setSelfClaim(op);
            enqueueWaiter(op, ch, false, null, null);
            assertTrue(ch.trySend(2L, sched) == GoChan.SendAttempt.RETRY,
                    "send past self-claimed waiter with full buffer must RETRY");
            assertEquals(1, ch.buffered(), "buffer untouched by RETRY");
            assertEquals(1, ch.recvWaitersCount(),
                    "RETRY must not drop the self-claimed waiter");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * trySend completes via the buffer even past a self-claimed waiter
     * when space exists; the waiter stays enqueued for its selector.
     */
    public void testTrySendBuffersPastSelfClaimedWaiter() throws Exception {
        SchedulerCore sched = installScheduler();
        try {
            GoChan ch = new GoChan(1, 0L);
            SelectOp op = new SelectOp(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(ch, false, null)});
            setSelfClaim(op);
            enqueueWaiter(op, ch, false, null, null);
            assertTrue(ch.trySend(7L, sched) == GoChan.SendAttempt.SENT,
                    "send completes via buffer");
            assertEquals(1, ch.buffered(), "value buffered");
            assertEquals(1, ch.recvWaitersCount(),
                    "buffer completion must not drop the self-claimed waiter");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * tryRecv reports retry (not an empty result) when the buffer is
     * empty, the channel is open, and the only send waiter is
     * self-claimed; the waiter stays enqueued.
     */
    public void testTryRecvReturnsRetryWhenBlockedBySelfClaim() throws Exception {
        SchedulerCore sched = installScheduler();
        try {
            GoChan ch = new GoChan(0, 0L);
            SelectOp op = new SelectOp(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(ch, true, 5L)});
            setSelfClaim(op);
            enqueueWaiter(op, ch, true, 5L, null);
            GoChan.RecvAttempt a = ch.tryRecv(sched);
            assertTrue(a.retry(), "recv past self-claimed sender must retry");
            assertTrue(a.result() == null, "retry carries no value");
            assertEquals(1, ch.sendWaitersCount(),
                    "RETRY must not drop the self-claimed waiter");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * tryRecv returns a buffered value even past a self-claimed send
     * waiter; the waiter stays enqueued for its selector.
     */
    public void testTryRecvReturnsBufferedPastSelfClaimedSender() throws Exception {
        SchedulerCore sched = installScheduler();
        try {
            GoChan ch = new GoChan(1, 0L);
            assertTrue(ch.trySend(9L, sched) == GoChan.SendAttempt.SENT,
                    "buffer fill must send");
            SelectOp op = new SelectOp(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(ch, true, 5L)});
            setSelfClaim(op);
            enqueueWaiter(op, ch, true, 5L, null);
            GoChan.RecvAttempt a = ch.tryRecv(sched);
            assertTrue(!a.retry(), "buffered value beats RETRY");
            assertTrue(a.result() != null && Long.valueOf(9L).equals(a.result().value()),
                    "buffered value returned");
            assertEquals(1, ch.sendWaitersCount(),
                    "buffered recv must not drop the self-claimed waiter");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * close() RETRIES a self-claimed select recv waiter instead of
     * dropping it: while the self-claim is held, close() must not
     * return; once released, close() claims the waiter, unparks its
     * goroutine (observed via quiescence), and removes it.
     */
    public void testCloseRetriesSelfClaimedWaiter() throws Exception {
        SchedulerCore sched = installScheduler();
        try {
            GoChan ch = new GoChan(0, 0L);
            SelectOp op = new SelectOp(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(ch, false, null)});
            // A real goroutine that parks (its handle is unparked by
            // close's claiming retry); it exits once resumed.
            GoroutineHandle h = sched.spawn(() ->
                    SchedulerCore.current().parkCurrent(null, ParkReason.CHANNEL));
            setSelfClaim(op);
            SelectOp.SelectCaseWaiter w = enqueueWaiter(op, ch, false, null, h);
            assertEquals(1, ch.recvWaitersCount(), "waiter enqueued");
            AtomicBoolean closerDone = new AtomicBoolean(false);
            Thread closer = new Thread(() -> {
                ChanOps.close(ch);
                closerDone.set(true);
            });
            closer.start();
            Thread.sleep(150);
            assertTrue(!closerDone.get(),
                    "close must still be retrying while the self-claim is held");
            setUnclaimed(op);
            closer.join(5000);
            assertTrue(closerDone.get(), "close must return after the claim resolves");
            assertTrue(ch.isClosed(), "channel closed");
            assertTrue(w.isClaimedByWaker(), "retry must claim the waiter");
            assertEquals(0, ch.recvWaitersCount(), "claimed waiter removed");
            awaitQuiescence(sched);
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * canRecv() must preserve a self-claimed select send waiter: it is
     * not deliverable right now (so canRecv is false), but it is not
     * dead either — purging it strands the selecting goroutine.
     */
    public void testCanRecvPreservesSelfClaimedSendWaiter() throws Exception {
        installScheduler();
        try {
            GoChan ch = new GoChan(0, 0L);
            SelectOp op = new SelectOp(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(ch, true, 5L)});
            setSelfClaim(op);
            enqueueWaiter(op, ch, true, 5L, null);
            assertTrue(!ch.canRecv(), "self-claimed sender is not deliverable now");
            assertEquals(1, ch.sendWaitersCount(),
                    "canRecv must not purge a self-claimed select waiter");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * Installs a one-shot {@link SelectOp#claimRaceHookForTest} that puts op
     * into SELF_CLAIM inside the waker's claim window — exactly what a
     * selector sweeping ANOTHER channel (under that channel's monitor) does
     * between the waker reading UNCLAIMED and its claiming CAS (BUG-0038).
     */
    private static void selfClaimInClaimWindow(SelectOp op) {
        AtomicBoolean fired = new AtomicBoolean();
        SelectOp.claimRaceHookForTest = () -> {
            if (fired.compareAndSet(false, true)) {
                try {
                    setSelfClaim(op);
                } catch (Exception e) {
                    throw new AssertionError(e);
                }
            }
        };
    }

    /**
     * BUG-0038: the selector self-claims while a sender is claiming its
     * waiter. The sender's failed CAS raced a SELF_CLAIM, not a waker: it
     * must report RETRY and keep the waiter. (The pre-fix code peeked
     * "self-claimed?" before the CAS, then took the failed CAS for a waker
     * claim and dropped the waiter: the select parked forever — leg F6.)
     */
    public void testTrySendKeepsWaiterSelfClaimedInClaimWindow() throws Exception {
        SchedulerCore sched = installScheduler();
        try {
            GoChan ch = new GoChan(0, 0L);
            SelectOp op = new SelectOp(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(ch, false, null)});
            enqueueWaiter(op, ch, false, null, null);
            selfClaimInClaimWindow(op);
            GoChan.SendAttempt a;
            synchronized (ch) {
                a = ch.trySend(1L, sched);
            }
            assertTrue(a == GoChan.SendAttempt.RETRY,
                    "send racing a self-claim must RETRY, got " + a);
            assertEquals(1, ch.recvWaitersCount(),
                    "a waiter self-claimed in the claim window must stay enqueued");
        } finally {
            SelectOp.claimRaceHookForTest = null;
            SchedulerCore.install(null);
        }
    }

    /** BUG-0038, receive side: tryRecv racing a self-claim keeps the send waiter. */
    public void testTryRecvKeepsWaiterSelfClaimedInClaimWindow() throws Exception {
        SchedulerCore sched = installScheduler();
        try {
            GoChan ch = new GoChan(0, 0L);
            SelectOp op = new SelectOp(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(ch, true, 5L)});
            enqueueWaiter(op, ch, true, 5L, null);
            selfClaimInClaimWindow(op);
            GoChan.RecvAttempt a;
            synchronized (ch) {
                a = ch.tryRecv(sched);
            }
            assertTrue(a.retry() && a.result() == null,
                    "recv racing a self-claim must retry");
            assertEquals(1, ch.sendWaitersCount(),
                    "a waiter self-claimed in the claim window must stay enqueued");
        } finally {
            SelectOp.claimRaceHookForTest = null;
            SchedulerCore.install(null);
        }
    }

    /**
     * BUG-0038, close side: close() racing a self-claim defers the waiter
     * (retrying until the self-claim resolves) instead of dropping it.
     */
    public void testCloseDefersWaiterSelfClaimedInClaimWindow() throws Exception {
        SchedulerCore sched = installScheduler();
        try {
            GoChan ch = new GoChan(0, 0L);
            SelectOp op = new SelectOp(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(ch, false, null)});
            GoroutineHandle h = sched.spawn(() ->
                    SchedulerCore.current().parkCurrent(null, ParkReason.CHANNEL));
            SelectOp.SelectCaseWaiter w = enqueueWaiter(op, ch, false, null, h);
            selfClaimInClaimWindow(op);
            AtomicBoolean closerDone = new AtomicBoolean(false);
            Thread closer = new Thread(() -> {
                ChanOps.close(ch);
                closerDone.set(true);
            });
            closer.start();
            Thread.sleep(150);
            assertTrue(!closerDone.get(),
                    "close must keep retrying the waiter while the self-claim is held");
            assertEquals(1, ch.recvWaitersCount(),
                    "close must not drop a waiter self-claimed in the claim window");
            setUnclaimed(op);
            closer.join(5000);
            assertTrue(closerDone.get(), "close must return after the claim resolves");
            assertTrue(w.isClaimedByWaker(), "close's retry must claim the waiter");
            assertEquals(0, ch.recvWaitersCount(), "claimed waiter removed");
            awaitQuiescence(sched);
        } finally {
            SelectOp.claimRaceHookForTest = null;
            SchedulerCore.install(null);
        }
    }

    /**
     * Guard: an UNCLAIMED select send waiter still counts as a live
     * waiter for readiness (the scan must not over-exclude).
     */
    public void testCanRecvSeesUnclaimedSelectWaiter() throws Exception {
        installScheduler();
        try {
            GoChan ch = new GoChan(0, 0L);
            SelectOp op = new SelectOp(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(ch, true, 5L)});
            enqueueWaiter(op, ch, true, 5L, null);
            assertTrue(ch.canRecv(), "unclaimed select sender is deliverable");
            assertEquals(1, ch.sendWaitersCount(), "waiter still enqueued");
        } finally {
            SchedulerCore.install(null);
        }
    }
}
