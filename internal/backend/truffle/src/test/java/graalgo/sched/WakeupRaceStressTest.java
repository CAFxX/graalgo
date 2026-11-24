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
     * lane gets its share. A handoff that granted a fresh quantum would let
     * the pair hold the slot forever and the spinner would get none.
     *
     * <p>The metric counts <em>turns</em>, not wall time, so an oversubscribed
     * host cannot read as starvation: with one slot, the spinner sees a turn
     * boundary when the pair's round counter moved between two of its polls
     * (the pair ran in between). It asserts (1) the spinner gets
     * {@value #HANDOFF_TURNS} turns before a generous cap (the starvation
     * the test guards gives zero) and (2) the median pair turn, measured by
     * the spinner as the gap it was off the slot, is within
     * {@value #HANDOFF_SLICE_RATIO}x the median spinner turn (both are about
     * a quantum when fair; a median ignores the turns the OS descheduled the
     * carrier in, which inflate both kinds equally and rarely). The previous
     * metric, spinner run time (inter-poll gaps under 50 us) over the wall
     * window, charged every millisecond the host kept the carrier off the
     * CPU, in the spinner's turns ({@code os-gaps}) or in the pair's and the
     * switches, against the spinner: at load 20 on 16 cores it read 0.13-0.18
     * (threshold 0.2) in 3 of 10 runs whose turns were fair (median pair turn
     * 1.1-1.8x the spinner's). Planting a fresh quantum per handoff gives
     * 5 (carrier) and 34 (thread mode) turns in the cap, with pair turns of
     * 118-318 ms.
     */
    public void testHandoffPairDoesNotStarveSpinner() throws Exception {
        final long capNanos = 5_000_000_000L; // only a starved run reaches it
        final long quantum = 2_000_000L;
        final AtomicLong rounds = new AtomicLong();
        final java.util.concurrent.atomic.AtomicBoolean stop =
                new java.util.concurrent.atomic.AtomicBoolean();
        final long[] spinnerTurns = new long[HANDOFF_TURNS];
        final long[] pairTurns = new long[HANDOFF_TURNS];
        final int[] turns = new int[1];
        final long[] osGapNanos = new long[1];
        final long[] mountedNanos = new long[1];
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
                long start = System.nanoTime();
                window[0] = start;
                // The cap ends a starved run (the spinner never runs to set stop).
                while (!stop.get() && System.nanoTime() - start < capNanos) {
                    ping.send(1L);
                    pong.recv();
                    rounds.incrementAndGet();
                    graalgo.safepoint.Safepoint.poll();
                }
                ping.close();
            });
            sched.spawn(() -> {
                long start = System.nanoTime();
                long last = start;
                long turnStart = start;
                long seen = rounds.get();
                int n = 0;
                while (n < HANDOFF_TURNS && last - start < capNanos) {
                    graalgo.safepoint.Safepoint.poll();
                    long now = System.nanoTime();
                    long r = rounds.get();
                    if (r != seen) {
                        // The pair ran between the two polls: a turn boundary.
                        spinnerTurns[n] = last - turnStart;
                        pairTurns[n] = now - last;
                        n++;
                        seen = r;
                        turnStart = now;
                    } else if (now - last >= 50_000L) {
                        osGapNanos[0] += now - last; // off the CPU, still holding the slot
                    } else {
                        mountedNanos[0] += now - last; // the old metric's run time
                    }
                    last = now;
                }
                turns[0] = n;
                window[1] = System.nanoTime();
                stop.set(true);
            });
        });
        int n = turns[0];
        long wall = window[1] - window[0];
        long spinnerMed = medianOf(spinnerTurns, n);
        long pairMed = medianOf(pairTurns, n);
        System.out.printf("  [handoff-fairness] %d turns in %d ms: median spinner turn %d us, "
                + "median pair turn %d us, os-gaps %d ms, old wall-share metric %.2f, pair rounds %d%n",
                n, wall / 1_000_000, spinnerMed / 1_000, pairMed / 1_000, osGapNanos[0] / 1_000_000,
                (double) mountedNanos[0] / Math.max(1, wall), rounds.get());
        if (n < HANDOFF_TURNS) {
            throw new AssertionError("ping-pong pair starved the spinner: " + n + " of "
                    + HANDOFF_TURNS + " turns in " + wall / 1_000_000 + " ms");
        }
        if (pairMed > HANDOFF_SLICE_RATIO * Math.max(spinnerMed, quantum)) {
            throw new AssertionError("ping-pong pair holds the slot for " + pairMed / 1_000
                    + " us per turn against the spinner's " + spinnerMed / 1_000 + " us");
        }
        if (rounds.get() == 0) {
            throw new AssertionError("spinner starved the ping-pong pair");
        }
    }

    /** Turns {@link #testHandoffPairDoesNotStarveSpinner} waits for (~4 ms each when fair). */
    private static final int HANDOFF_TURNS = 40;

    /** Bound on median pair turn / median spinner turn (about 1 when fair). */
    private static final int HANDOFF_SLICE_RATIO = 4;

    private static long medianOf(long[] xs, int n) {
        if (n == 0) {
            return 0;
        }
        long[] c = java.util.Arrays.copyOf(xs, n);
        java.util.Arrays.sort(c);
        return c[n / 2];
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
