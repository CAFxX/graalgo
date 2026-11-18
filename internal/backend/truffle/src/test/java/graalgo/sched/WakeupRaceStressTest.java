package graalgo.sched;

import java.util.concurrent.atomic.AtomicLong;

import graalgo.runtime.GoChan;
import graalgo.runtime.Timers;

/**
 * P2-M9: JVM-level race stress on the wakeup paths (done-gate 4).
 *
 * <p>Companion to {@link WakeupModelTest}: the explorer is exhaustive but
 * works on token scripts; this runs REAL virtual threads under contention
 * on the exact paths the explorer models — thundering herds on one
 * channel, timer storms, quantum expiry racing park/unpark, and
 * multi-channel receiver herds. Any hang trips the quiescence timeout (a
 * lost wakeup is LOUD, never a silent pass); any drainer invariant
 * violation halts the JVM via the drainer's fatal path. All counts are
 * asserted exactly at the end.
 */
public final class WakeupRaceStressTest {

    private SchedulerCore sched;

    private void withScheduler(int gmp, long quantumNanos, ThrowingRunnable body)
            throws Exception {
        sched = new SchedulerCore(gmp, quantumNanos);
        sched.start();
        SchedulerCore.install(sched);
        try {
            body.run();
            awaitQuiescence(60_000);
        } finally {
            SchedulerCore.install(null);
        }
        if (sched.panicked() || sched.crashed()) {
            throw new AssertionError("goroutine panic/crash during race stress");
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

    private interface ThrowingRunnable {
        void run() throws Exception;
    }

    private static void assertEquals(long expected, long actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /**
     * Thundering herd: 32 senders x 32 receivers on ONE unbuffered
     * channel, 200 round trips each. Every park/unpark pair races;
     * every value must transfer exactly once.
     *
     * <p>The start barrier is scheduler-aware (spin + yieldCurrent):
     * a JVM-blocking barrier (e.g. CountDownLatch.await) would hold
     * scheduler slots while parked and deadlock admission under the cap
     * — that deadlock is the test's bug, not the scheduler's.
     */
    public void testThunderingHerd() throws Exception {
        final int senders = 32, receivers = 32, iters = 200;
        final int total = senders + receivers;
        final AtomicLong sent = new AtomicLong();
        final AtomicLong received = new AtomicLong();
        withScheduler(4, SchedulerCore.DEFAULT_QUANTUM_NANOS, () -> {
            GoChan ch = new GoChan(0, null);
            AtomicLong arrived = new AtomicLong();
            for (int i = 0; i < senders; i++) {
                final long base = (long) i * iters;
                sched.spawn(() -> {
                    herdBarrier(arrived, total);
                    for (long k = 0; k < iters; k++) {
                        ch.send(base + k);
                        sent.incrementAndGet();
                    }
                });
            }
            for (int i = 0; i < receivers; i++) {
                sched.spawn(() -> {
                    herdBarrier(arrived, total);
                    for (long k = 0; k < iters; k++) {
                        ch.recv();
                        received.incrementAndGet();
                    }
                });
            }
        });
        // Exact accounting: every send paired with exactly one receive.
        assertEquals((long) senders * iters, sent.get(), "thundering-herd sends");
        assertEquals(sent.get(), received.get(), "thundering-herd recv==send");
    }

    /** Scheduler-aware start barrier: spin + yield (releases the slot). */
    private void herdBarrier(AtomicLong arrived, int total) {
        arrived.incrementAndGet();
        while (arrived.get() < total) {
            sched.yieldCurrent();
        }
    }

    /**
     * Timer storm: 48 goroutines x 25 short sleeps (1-3ms) while 8
     * sender/receiver pairs move traffic on a side channel. The timer
     * thread fires unparks concurrently with quantum expiry and channel
     * park/unpark traffic.
     */
    public void testTimerStorm() throws Exception {
        final int n = 48, iters = 25;
        final AtomicLong sleeps = new AtomicLong();
        final AtomicLong moved = new AtomicLong();
        withScheduler(4, SchedulerCore.DEFAULT_QUANTUM_NANOS, () -> {
            GoChan ch = new GoChan(16, null);
            for (int i = 0; i < n; i++) {
                sched.spawn(() -> {
                    for (int k = 0; k < iters; k++) {
                        Timers.sleep(1_000_000L + (k % 3) * 1_000_000L);
                        sleeps.incrementAndGet();
                    }
                });
            }
            for (int i = 0; i < 8; i++) {
                sched.spawn(() -> {
                    for (int k = 0; k < iters * 6; k++) {
                        ch.send(k);
                    }
                });
                sched.spawn(() -> {
                    for (int k = 0; k < iters * 6; k++) {
                        ch.recv();
                        moved.incrementAndGet();
                    }
                });
            }
        });
        assertEquals((long) n * iters, sleeps.get(), "timer-storm sleeps");
        assertEquals((long) 8 * iters * 6, moved.get(), "timer-storm side traffic");
    }

    /**
     * Quantum expiry racing park/unpark: 100us quantum (100x the default)
     * with channel ping-pong, so quantumPark (RESUME token) constantly
     * interleaves with PARKED/UNPARK on the same goroutines.
     */
    public void testTinyQuantumWithChannels() throws Exception {
        final int pairs = 16, iters = 300;
        final AtomicLong rounds = new AtomicLong();
        withScheduler(2, 100_000L, () -> {
            for (int i = 0; i < pairs; i++) {
                GoChan ch = new GoChan(0, null);
                sched.spawn(() -> {
                    for (int k = 0; k < iters; k++) {
                        ch.send(k);
                    }
                });
                sched.spawn(() -> {
                    long sink = 0;
                    for (int k = 0; k < iters; k++) {
                        ch.recv();
                        // Burn a little CPU per round: the ~40us channel
                        // round-trip latency (not this ~0.5us burn) is what
                        // actually pushes goroutines past the 100us quantum,
                        // so quantumPark keeps racing PARKED/UNPARK here.
                        for (int j = 0; j < 500; j++) {
                            sink += j;
                        }
                        rounds.incrementAndGet();
                    }
                    if (sink == -1) {
                        throw new RuntimeException("unreachable");
                    }
                });
            }
        });
        assertEquals((long) pairs * iters, rounds.get(), "tiny-quantum ping-pong rounds");
    }

    /**
     * Receiver herd: 24 goroutines receiving across 4 channels with 4
     * senders. Stresses channel wakeup paths under contention (the
     * select shared-claim protocol itself is covered by ChannelTest;
     * here the target is park/unpark pairing at scale).
     */
    public void testReceiverHerd() throws Exception {
        final int chans = 4, receivers = 24, perReceiver = 100;
        final AtomicLong received = new AtomicLong();
        withScheduler(4, SchedulerCore.DEFAULT_QUANTUM_NANOS, () -> {
            GoChan[] chs = new GoChan[chans];
            for (int i = 0; i < chans; i++) {
                chs[i] = new GoChan(0, null);
            }
            for (int i = 0; i < receivers; i++) {
                final GoChan ch = chs[i % chans];
                sched.spawn(() -> {
                    for (int k = 0; k < perReceiver; k++) {
                        ch.recv();
                        received.incrementAndGet();
                    }
                });
            }
            for (int i = 0; i < chans; i++) {
                final GoChan ch = chs[i];
                sched.spawn(() -> {
                    int total = receivers * perReceiver / chans;
                    for (int k = 0; k < total; k++) {
                        ch.send(k);
                    }
                });
            }
        });
        assertEquals((long) receivers * perReceiver, received.get(),
                "receiver-herd total receives");
    }
}
