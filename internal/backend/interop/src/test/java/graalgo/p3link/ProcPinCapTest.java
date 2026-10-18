package graalgo.p3link;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

import graalgo.sched.SchedulerCore;

/**
 * P1-29: {@code SyncRuntime.procPin} sizes P-ids from the scheduler's
 * <em>effective</em> admission cap, not the requested GOMAXPROCS — the two
 * disagree when GOMAXPROCS exceeds the carrier parallelism.
 *
 * <p>Red-green: pre-fix, {@code procPin} reduced mod the requested value
 * ({@code GoMaxProcsNode.current()}, the host CPU count here), so with a
 * cap=1 scheduler installed some thread observed a nonzero id and
 * {@link #testProcPinUsesEffectiveCap} failed.
 */
public final class ProcPinCapTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /**
     * With a cap=1 scheduler installed, every {@code procPin} id is 0 —
     * even though the requested GOMAXPROCS (the host CPU count on this
     * box) is larger. Eight threads pin concurrently so per-thread dense
     * ids actually vary; pre-fix their residues mod the requested value
     * escaped 0.
     */
    public void testProcPinUsesEffectiveCap() throws Exception {
        SchedulerCore sched = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        if (sched.carrierParallelism() < 2) {
            return; // need requested != effective to discriminate
        }
        assertTrue(graalgo.p3builtin.nodes.GoMaxProcsNode.current() > 1,
                "precondition: requested GOMAXPROCS exceeds the cap=1 under test");
        SchedulerCore prev = SchedulerCore.install(sched);
        try {
            int threads = 8;
            CountDownLatch ready = new CountDownLatch(threads);
            CountDownLatch go = new CountDownLatch(1);
            AtomicInteger nonzero = new AtomicInteger();
            Thread[] ts = new Thread[threads];
            for (int i = 0; i < threads; i++) {
                ts[i] = Thread.ofPlatform().start(() -> {
                    ready.countDown();
                    try {
                        if (!go.await(10, TimeUnit.SECONDS)) {
                            return;
                        }
                    } catch (InterruptedException e) {
                        return;
                    }
                    for (int k = 0; k < 200; k++) {
                        if (SyncRuntime.procPin() != 0) {
                            nonzero.incrementAndGet();
                        }
                    }
                });
            }
            assertTrue(ready.await(10, TimeUnit.SECONDS), "threads ready");
            go.countDown();
            for (Thread t : ts) {
                t.join(15_000);
                assertTrue(!t.isAlive(), "pin thread joined");
            }
            assertTrue(nonzero.get() == 0,
                    "procPin ids escaped the effective cap=1: " + nonzero.get() + " nonzero");
        } finally {
            SchedulerCore.install(prev);
        }
    }

    /**
     * Scheduler-less runs: {@code procPin} still works, falling back to
     * the requested GOMAXPROCS (Phase-1 parity — no scheduler entry
     * point is reached).
     */
    public void testProcPinWithoutScheduler() {
        SchedulerCore prev = SchedulerCore.install(null);
        try {
            int gmp = graalgo.p3builtin.nodes.GoMaxProcsNode.current();
            assertTrue(gmp >= 1, "sanity: requested GOMAXPROCS >= 1");
            for (int k = 0; k < 100; k++) {
                int pid = SyncRuntime.procPin();
                assertTrue(pid >= 0 && pid < gmp, "pid in [0, requested): " + pid);
            }
        } finally {
            SchedulerCore.install(prev);
        }
    }
}
