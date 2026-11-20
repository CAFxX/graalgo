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

    /**
     * Direct handoff starvation freedom (WP-SCHED, GG-SCHED-0001): at
     * GOMAXPROCS 1 a channel ping-pong pair passes the only slot back and
     * forth by direct handoff, never through the drainer's lanes. The pair
     * must still yield the slot at the quantum (a handoff inherits the
     * slot's quantum deadline), so a CPU-bound spinner queued in the run
     * lane gets its share: here at least a fifth of the wall time (about
     * half is expected). A handoff that granted a fresh quantum would let
     * the pair hold the slot forever and the spinner would get none.
     */
    public void testHandoffPairDoesNotStarveSpinner() throws Exception {
        final long runNanos = 600_000_000L;
        final long quantum = 2_000_000L;
        final AtomicLong spinnerRunNanos = new AtomicLong();
        final AtomicLong rounds = new AtomicLong();
        final java.util.concurrent.atomic.AtomicBoolean stop =
                new java.util.concurrent.atomic.AtomicBoolean();
        final long[] window = new long[2];
        withScheduler(1, quantum, () -> {
            GoChan ping = new GoChan(0, 0L);
            GoChan pong = new GoChan(0, 0L);
            sched.spawn(() -> {
                for (;;) {
                    GoChan.RecvResult r = ping.recv();
                    if (!r.ok()) {
                        return;
                    }
                    pong.send(r.value());
                    graalgo.safepoint.Safepoint.poll();
                }
            });
            sched.spawn(() -> {
                window[0] = System.nanoTime();
                while (!stop.get()) {
                    ping.send(1L);
                    pong.recv();
                    rounds.incrementAndGet();
                    graalgo.safepoint.Safepoint.poll();
                }
                ping.close();
            });
            sched.spawn(() -> {
                long last = System.nanoTime();
                long ran = 0;
                long start = last;
                while (last - start < runNanos) {
                    graalgo.safepoint.Safepoint.poll();
                    long now = System.nanoTime();
                    if (now - last < 50_000L) {
                        ran += now - last; // still mounted: count it as run time
                    }
                    last = now;
                }
                spinnerRunNanos.set(ran);
                window[1] = System.nanoTime();
                stop.set(true);
            });
        });
        long wall = window[1] - window[0];
        double share = (double) spinnerRunNanos.get() / wall;
        System.out.printf("  [handoff-fairness] spinner share %.2f of %d ms, pair rounds %d%n",
                share, wall / 1_000_000, rounds.get());
        if (share < 0.2) {
            throw new AssertionError("ping-pong pair starved the spinner: share " + share);
        }
        if (rounds.get() == 0) {
            throw new AssertionError("spinner starved the ping-pong pair");
        }
    }

    /**
     * BUG-0038 (torture leg F6): a {@code select} receiving on {@code ping}
     * and {@code quit} races a plain {@code ping} send on every round. While
     * the selector sweeps {@code quit} it holds its transient self-claim; a
     * sender claiming the selector's {@code ping} waiter in that window must
     * keep the waiter (and retry), never drop it as "claimed by a waker" —
     * that strands both goroutines parked with no wake-up in flight. Half the
     * pairs end with a send on {@code quit}, half with a close (the close path
     * classifies select waiters the same way). On the pre-fix code a single
     * repetition (32 pairs x 300 rounds) hung in 2 of 3 runs; four fresh
     * schedulers make a miss unlikely (the quiescence timeout reports it).
     * The deterministic form of the race is SelectSelfClaimTest's
     * {@code *SelfClaimedInClaimWindow} tests.
     */
    public void testSelectRecvRacesSend() throws Exception {
        for (int rep = 0; rep < 4; rep++) {
            selectRecvRacesSendOnce();
        }
    }

    private void selectRecvRacesSendOnce() throws Exception {
        final int pairs = 32, rounds = 300;
        final AtomicLong ponged = new AtomicLong();
        final AtomicLong bad = new AtomicLong();
        withScheduler(4, SchedulerCore.DEFAULT_QUANTUM_NANOS, () -> {
            for (int i = 0; i < pairs; i++) {
                final boolean closeQuit = (i & 1) == 1;
                GoChan ping = new GoChan(0, 0L);
                GoChan pong = new GoChan(0, 0L);
                GoChan quit = new GoChan(0, 0L);
                sched.spawn(() -> {
                    for (;;) {
                        int idx = graalgo.runtime.ChanOps.selectGo(
                                new graalgo.runtime.ChanOps.SelectCase[]{
                                    new graalgo.runtime.ChanOps.SelectCase(ping, false, null),
                                    new graalgo.runtime.ChanOps.SelectCase(quit, false, null),
                                });
                        if (idx == 1) {
                            if (graalgo.runtime.ChanOps.selectRecvOk() == closeQuit) {
                                bad.incrementAndGet(); // ok must be !closed
                            }
                            return;
                        }
                        pong.send((Long) graalgo.runtime.ChanOps.selectRecvValue() + 1);
                    }
                });
                sched.spawn(() -> {
                    for (long k = 0; k < rounds; k++) {
                        ping.send(k);
                        if ((Long) pong.recv().value() != k + 1) {
                            bad.incrementAndGet();
                        }
                        ponged.incrementAndGet();
                    }
                    if (closeQuit) {
                        quit.close();
                    } else {
                        quit.send(0L);
                    }
                });
            }
        });
        assertEquals((long) pairs * rounds, ponged.get(), "select/send ping-pong rounds");
        assertEquals(0, bad.get(), "select/send ping-pong wrong values or ok bits");
    }
}
