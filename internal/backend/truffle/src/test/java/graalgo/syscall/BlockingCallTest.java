package graalgo.syscall;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.RejectedExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.go.values.Flat;
import graalgo.sched.SchedulerCore;

/**
 * May-block system calls off the carrier pool (ADR-0019): failure modes of the hand-off itself.
 *
 * <p>verifies: GG-SCHED-0006 (slice: a may-block call that cannot get its thread fails its
 * goroutine and leaves the scheduler's other calls running)
 */
public final class BlockingCallTest {
    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    /** Linux x86-64/arm64 {@code getpid}: may-block for the test, returns at once. */
    private static long getpidNr() {
        return "aarch64".equals(System.getProperty("os.arch")) ? 172 : 39;
    }

    /**
     * BUG-0057: when the off-pool executor refused a may-block call (no thread: rejection or
     * OutOfMemoryError), the call was already marked submitted, so the goroutine "abandoned" it:
     * every system call of the live program was aborted, the buffers were never freed and the
     * call stayed in flight (its abort descriptor could never close), and the goroutine froze
     * for an exit that never came. Forced with an executor that refuses: the failure reaches the
     * goroutine, nothing is aborted, nothing stays in flight, and the next call of the same
     * scheduler runs. Mutation: {@code submitted = true} before {@code execute} — red (the
     * aborter is aborted; the goroutine never finishes).
     */
    public void testRefusedCallFailsItsGoroutineOnly() throws Exception {
        SchedulerCore s = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        s.start();
        SchedulerCore prev = SchedulerCore.install(s);
        Entry e = new Entry((int) getpidNr(), "getpid", true, 0, new Role[0]);
        AtomicReference<Throwable> refused = new AtomicReference<>();
        AtomicReference<Object> second = new AtomicReference<>();
        CountDownLatch finished = new CountDownLatch(1);
        try {
            s.spawn(() -> {
                try {
                    try {
                        BlockingCall.run(null, s, e, e.nr, new long[6], e.roles, new Flat[0], new long[0],
                                new byte[0][], 0, r -> {
                                    throw new RejectedExecutionException("planted: no thread");
                                });
                    } catch (RejectedExecutionException x) {
                        refused.set(x);
                    }
                    second.set(BlockingCall.run(null, s, e, e.nr, new long[6], e.roles, new Flat[0], new long[0],
                            new byte[0][], 0)[0]);
                } finally {
                    finished.countDown();
                }
            });
            assertTrue(finished.await(20, TimeUnit.SECONDS), "the goroutine finished (it froze for an exit)");
        } finally {
            SchedulerCore.install(prev);
        }
        assertTrue(refused.get() != null, "the refusal reached the goroutine");
        BlockingCall.Aborter a = BlockingCall.Aborter.of(s);
        assertTrue(!a.aborted, "a refused call aborted the live program's system calls");
        assertTrue(a.inflightCount() == 0, "calls left in flight: " + a.inflightCount());
        assertTrue(Long.valueOf(ProcessHandle.current().pid()).equals(second.get()),
                "the next call ran: " + second.get());
        assertTrue(s.terminate(10_000).isEmpty(), "carriers stopped");
    }
}
