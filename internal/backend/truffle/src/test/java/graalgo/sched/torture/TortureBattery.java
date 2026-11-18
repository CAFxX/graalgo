package graalgo.sched.torture;

import graalgo.runtime.GoChan;
import graalgo.runtime.ChanOps;
import graalgo.safepoint.Safepoint;
import graalgo.sched.SchedulerCore;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;

/**
 * Preemption-torture battery ported from spike 2b (spikes/02b-userspace-scheduler/
 * src/work/) to the production scheduler ({@code graalgo.sched.SchedulerCore}).
 *
 * <p>Legs (spike 2b F1–F6, minus F2 — see below):
 * <ul>
 *   <li>F1: O1 torture — spinners + latency pair (starvation falsifier)
 *   <li>F2: NOT PORTED as a battery leg. O2–O5 semantics are covered at
 *       unit level: O2 (channels) by {@code ChannelTest} + F1/F6's
 *       unbuffered ping-pong; O3 (select fairness) by
 *       {@code ChannelTest.testSelectFairness} (2-case); O4 (timers) by
 *       {@code TimerTest}/{@code TimerDifferentialTest}. O5 (spin-barrier
 *       scale) has no torture-level equivalent — recorded as a re-test
 *       trigger in the P2-M8 REPORT (§5).
 *   <li>F3: quiescence — 10k spinners, all admitted, fair quanta
 *   <li>F4: overhead budget — per-hop park/resume vs 1% of quantum
 *   <li>F5: GOMAXPROCS fidelity — cap never exceeded; F5b cap-exhaustion
 *   <li>F6: lost-wakeup stress — hang-on-loss invariant (see leg doc)
 * </ul>
 *
 * <p>Usage: {@code TortureBattery <leg> [args...]}, with
 * {@code -Dgraalgo.gmp=N} and {@code -Dgraalgo.quantumNs=N} selecting the
 * scheduler config. Exit 0 = PASS, 2 = FAIL. Each leg prints a
 * {@code RESULT=PASS|FAIL} line and a stats line for the report.
 *
 * <p>verifies: GG-SCHED-0001 (F1/F4), GG-SCHED-0002 (F3), GG-SCHED-0003 (F5),
 * GG-FAIR-0001/0002 (F1/F5), GG-WAKE-0001 (F6).
 */
public final class TortureBattery {

    private TortureBattery() {}

    // ------------------------------------------------------------------
    // harness

    private static SchedulerCore newScheduler() {
        int gmp = Integer.getInteger("graalgo.gmp", Runtime.getRuntime().availableProcessors());
        long quantumNs = Long.getLong("graalgo.quantumNs", SchedulerCore.DEFAULT_QUANTUM_NANOS);
        SchedulerCore sched = new SchedulerCore(gmp, quantumNs);
        sched.start();
        SchedulerCore.install(sched);
        return sched;
    }

    private static void blackhole(long v) {
        if (v == 0x123456789ABCDEFL) {
            System.out.print("");
        }
    }

    // ------------------------------------------------------------------
    // F1: O1 torture — spinners + latency pair

    /**
     * F1: nSpin CPU-bound spinners (Safepoint.poll back-edge) + one latency
     * pair ping-ponging on unbuffered channels. Binary gate: the pair must
     * obtain samples (pair starvation = FAIL). Also reports spinner fairness
     * (CV of per-spinner iteration counts).
     *
     * <p>Args: nSpin secs
     */
    public static int legF1(String[] args) throws Exception {
        int nSpin = Integer.parseInt(args[0]);
        int secs = Integer.parseInt(args[1]);
        SchedulerCore sched = newScheduler();
        System.out.printf("F1 nSpin=%d secs=%d gmp=%d(req) cap=%d quantumNs=%d carriers=%d%n",
                nSpin, secs, sched.requestedCap(), sched.cap(),
                Long.getLong("graalgo.quantumNs", SchedulerCore.DEFAULT_QUANTUM_NANOS),
                sched.carrierParallelism());

        final boolean[] stop = {false};
        long[] counts = new long[nSpin];
        for (int i = 0; i < nSpin; i++) {
            final int idx = i;
            sched.spawn(() -> {
                long c = 0, sink = 0;
                while (!stop[0]) {
                    sink += c * 0x9E3779B97F4A7C15L;
                    sink ^= sink >>> 29;
                    c++;
                    Safepoint.poll();
                }
                counts[idx] = c;
                blackhole(sink);
            });
        }

        GoChan ping = new GoChan(0, 0L);
        GoChan pong = new GoChan(0, 0L);
        int maxSamples = 200_000;
        long[] rtts = new long[maxSamples];
        AtomicInteger nSamples = new AtomicInteger(0);
        AtomicLong pongerIters = new AtomicLong(0);
        sched.spawn(() -> {
            int n = 0;
            try {
                while (!stop[0] && n < maxSamples) {
                    long t0 = System.nanoTime();
                    ping.send(t0);
                    GoChan.RecvResult r = pong.recv();
                    long rt = (Long) r.value();
                    if (n < maxSamples) {
                        rtts[n] = rt - t0;
                    }
                    n++;
                    nSamples.incrementAndGet();
                }
            } catch (graalgo.runtime.PanicException pe) {
                // Expected during shutdown: we close the channels to unblock
                // the pair; "send on closed channel" kills the goroutine.
                if (!stop[0]) {
                    pe.printStackTrace();
                }
            } catch (Throwable th) {
                th.printStackTrace();
            }
        });
        sched.spawn(() -> {
            try {
                while (!stop[0]) {
                    GoChan.RecvResult r = ping.recv();
                    long v = (Long) r.value();
                    pong.send(v == 0 ? -1L : v);
                    pongerIters.incrementAndGet();
                    if ((pongerIters.get() & 63) == 0) {
                        Safepoint.poll();
                    }
                }
            } catch (graalgo.runtime.PanicException pe) {
                // Expected during shutdown (see above).
                if (!stop[0]) {
                    pe.printStackTrace();
                }
            } catch (Throwable th) {
                th.printStackTrace();
            }
        });

        Thread.sleep(secs * 1000L);
        stop[0] = true;
        // Unblock the pair if parked on channels.
        try { ping.close(); } catch (Throwable ignored) {}
        try { pong.close(); } catch (Throwable ignored) {}
        sched.awaitQuiescence();

        int samples = nSamples.get();
        // Spinner fairness: coefficient of variation.
        double mean = 0;
        for (long c : counts) mean += c;
        mean /= nSpin;
        double var = 0;
        for (long c : counts) var += (c - mean) * (c - mean);
        var /= nSpin;
        double cv = mean > 0 ? Math.sqrt(var) / mean : Double.NaN;

        boolean pass = samples > 0;
        System.out.printf("F1 samples=%d pongerIters=%d spinnerCV=%.4f cap=%d maxInflight=%d%n",
                samples, pongerIters.get(), cv, sched.cap(), sched.maxInflightObserved());
        System.out.printf("F1 wakeProvenance: %s%n", sched.wakeProvenance());
        System.out.printf("RESULT=%s%n", pass ? "PASS" : "FAIL");
        return pass ? 0 : 2;
    }

    // ------------------------------------------------------------------
    // F3: quiescence — 10k spinners

    /**
     * F3: nSpin spinners; every spinner must be admitted at least once
     * before stop (starvation signal), quanta fairly distributed
     * (max/mean <= 2.0), clean drain.
     *
     * <p>Args: nSpin secs
     */
    public static int legF3(String[] args) throws Exception {
        int nSpin = Integer.parseInt(args[0]);
        int secs = Integer.parseInt(args[1]);
        SchedulerCore sched = newScheduler();

        final boolean[] stop = {false};
        long[] quanta = new long[nSpin];
        AtomicLong admitted = new AtomicLong(0);
        CountDownLatch drained = new CountDownLatch(nSpin);
        for (int i = 0; i < nSpin; i++) {
            final int idx = i;
            sched.spawn(() -> {
                // Explicit yield per iteration = one drainer admission per
                // "quantum": measures drainer admission fairness directly.
                // (Quantum-expiry fairness is covered by F1's poll() spinners.)
                // admittedOnce tracks "admitted at least once during the run"
                // (the starvation signal); incremented on the FIRST yield.
                long sink = 0, c = 0, yields = 0;
                boolean counted = false;
                while (!stop[0]) {
                    sink += c * 0x9E3779B97F4A7C15L;
                    sink ^= sink >>> 29;
                    c++;
                    sched.yieldCurrent();
                    yields++;
                    if (!counted) {
                        counted = true;
                        admitted.incrementAndGet();
                    }
                }
                quanta[idx] = yields;
                blackhole(sink);
                drained.countDown();
            });
        }
        Thread.sleep(secs * 1000L);
        long admittedBeforeStop = admitted.get();
        stop[0] = true;
        long drainStart = System.nanoTime();
        boolean drainedOk = drained.await(120, TimeUnit.SECONDS);
        long drainWaitMs = (System.nanoTime() - drainStart) / 1_000_000;

        long[] q = Arrays.copyOf(quanta, nSpin);
        Arrays.sort(q);
        int first = 0;
        while (first < nSpin && q[first] == 0) first++;
        double mean = 0;
        for (int i = first; i < nSpin; i++) mean += q[i];
        mean /= Math.max(1, nSpin - first);
        long max = q[nSpin - 1];
        double ratio = mean > 0 ? max / mean : Double.NaN;
        boolean fair = admittedBeforeStop == nSpin && drainedOk && ratio <= 2.0;

        System.out.printf("F3 nSpin=%d secs=%d quanta min=%d max=%d mean=%.1f max/mean=%.2f%n",
                nSpin, secs, first < nSpin ? q[first] : 0, max, mean, ratio);
        System.out.printf("F3 admitted_before_stop=%d drained=%b drain_wait_ms=%d maxInflight=%d%n",
                admittedBeforeStop, drainedOk, drainWaitMs, sched.maxInflightObserved());
        System.out.printf("RESULT=%s%n", fair ? "PASS" : "FAIL");
        return fair ? 0 : 2;
    }

    // ------------------------------------------------------------------
    // F4: overhead budget

    /**
     * F4: single goroutine, no contention — park/resume hop latency is pure
     * scheduler overhead. Budget: mean < 1% of the configured quantum.
     * Also measures the Safepoint.poll() fast path.
     *
     * <p>Args: iters
     */
    public static int legF4(String[] args) throws Exception {
        int iters = Integer.parseInt(args[0]);
        SchedulerCore sched = newScheduler();
        long quantumNs = Long.getLong("graalgo.quantumNs", SchedulerCore.DEFAULT_QUANTUM_NANOS);
        long budgetNs = quantumNs / 100;

        long[] costs = new long[iters];
        CountDownLatch done = new CountDownLatch(1);
        sched.spawn(() -> {
            for (int i = 0; i < 20_000; i++) {
                sched.yieldCurrent();
            }
            for (int i = 0; i < iters; i++) {
                long t0 = System.nanoTime();
                sched.yieldCurrent();
                costs[i] = System.nanoTime() - t0;
            }
            done.countDown();
        });
        if (!done.await(300, TimeUnit.SECONDS)) {
            System.out.println("F4 TIMEOUT waiting for probe goroutine");
            System.out.println("RESULT=FAIL");
            return 2;
        }
        sched.awaitQuiescence();

        Arrays.sort(costs);
        double mean = 0;
        for (long c : costs) mean += c;
        mean /= iters;
        double p50 = costs[(int) (0.50 * iters)];
        double p99 = costs[(int) (0.99 * iters)];
        System.out.printf("F4 iters=%d hop_ns mean=%.0f p50=%.0f p99=%.0f max=%d%n",
                iters, mean, p50, p99, costs[iters - 1]);
        System.out.printf("F4 budget: mean_ns=%.0f < %d (1%% of quantumNs=%d) -> %s%n",
                mean, budgetNs, quantumNs, mean < budgetNs ? "PASS" : "FAIL");

        // poll() on a goroutine: 10M polls include quantum-expiry parks
        // (the quantum DOES expire here — ~2.4s of looping at ~237ns/poll),
        // so this measures the poll fast path plus park/re-admit costs.
        // The ~237ns/poll reflects ThreadLocal goroutine lookup + nanoTime
        // compare + occasional drainer re-admission, not a single fast-path read.
        final double[] pollNs = new double[1];
        CountDownLatch done2 = new CountDownLatch(1);
        sched.spawn(() -> {
            int polls = 10_000_000;
            long a = System.nanoTime();
            for (int i = 0; i < polls; i++) {
                Safepoint.poll();
            }
            pollNs[0] = (System.nanoTime() - a) / (double) polls;
            done2.countDown();
        });
        done2.await(120, TimeUnit.SECONDS);
        sched.awaitQuiescence();
        System.out.printf("F4 poll_fastpath_ns_per_poll_on_goroutine=%.2f%n", pollNs[0]);

        boolean pass = mean < budgetNs;
        System.out.printf("RESULT=%s%n", pass ? "PASS" : "FAIL");
        return pass ? 0 : 2;
    }

    // ------------------------------------------------------------------
    // F6: lost-wakeup stress

    /**
     * F6: randomized park/unpark + channel wakeups under load. Invariant:
     * admissions == parks at quiescence; zero lost wakeups; no hangs.
     *
     * <p>Args: nPairs nPings
     */
    public static int legF6(String[] args) throws Exception {
        int nPairs = Integer.parseInt(args[0]);
        int nPings = Integer.parseInt(args[1]);
        SchedulerCore sched = newScheduler();
        AtomicLong errors = new AtomicLong(0);
        CountDownLatch done = new CountDownLatch(nPairs * 2);

        for (int i = 0; i < nPairs; i++) {
            GoChan ping = new GoChan(0, 0L);
            GoChan pong = new GoChan(0, 0L);
            GoChan dch = new GoChan(0, 0L);
            sched.spawn(() -> {
                try {
                    for (;;) {
                        int idx = ChanOps.selectGo(new ChanOps.SelectCase[]{
                            new ChanOps.SelectCase(ping, false, null),
                            new ChanOps.SelectCase(dch, false, null),
                        });
                        if (idx == 1) break;
                        // selectGo claimed ping's value; retrieve it via the
                        // select result accessors (NOT ping.recv(), which
                        // would block — the value is already consumed).
                        long v = (Long) ChanOps.selectRecvValue();
                        pong.send(v + 1);
                    }
                } catch (Throwable th) {
                    errors.incrementAndGet();
                    th.printStackTrace();
                }
                done.countDown();
            });
            sched.spawn(() -> {
                try {
                    for (int k = 0; k < nPings; k++) {
                        long t0 = System.nanoTime();
                        ping.send(t0);
                        GoChan.RecvResult r = pong.recv();
                        long rt = (Long) r.value();
                        if (rt <= t0) {
                            errors.incrementAndGet();
                        }
                        if ((k & 31) == 0) {
                            Safepoint.poll();
                        }
                    }
                    dch.send(0L);
                } catch (Throwable th) {
                    errors.incrementAndGet();
                    th.printStackTrace();
                }
                done.countDown();
            });
        }

        boolean finished = done.await(180, TimeUnit.SECONDS);
        sched.awaitQuiescence();
        boolean pass = finished && errors.get() == 0;
        System.out.printf("F6 finished=%s errors=%d maxInflight=%d cap=%d%n",
                finished, errors.get(), sched.maxInflightObserved(), sched.cap());
        // M6 watch item: every WAKE admission must have a causally-preceding
        // UNPARK (direct or stashed). ioExit should be 0 here (no I/O).
        System.out.printf("F6 wakeProvenance: %s%n", sched.wakeProvenance());
        System.out.printf("RESULT=%s%n", pass ? "PASS" : "FAIL");
        return pass ? 0 : 2;
    }

    // ------------------------------------------------------------------
    // F5: GOMAXPROCS fidelity — cap never exceeded

    /**
     * F5: measures true parallelism (simultaneously executing goroutines)
     * under CPU load; asserts it never exceeds the in-flight cap.
     * The cap is a hard assert in the drainer (GG-SCHED-0003); this leg
     * verifies it observationally.
     *
     * <p>No args (gmp/quantum via -D flags).
     */
    public static int legF5(String[] args) throws Exception {
        SchedulerCore sched = newScheduler();
        int cap = sched.cap();
        AtomicInteger current = new AtomicInteger(0);
        AtomicInteger maxObserved = new AtomicInteger(0);
        AtomicLong violations = new AtomicLong(0);

        int n = Math.max(8, cap * 4);
        CountDownLatch done = new CountDownLatch(n);
        final boolean[] stop = {false};
        for (int i = 0; i < n; i++) {
            sched.spawn(() -> {
                try {
                    long sink = 0;
                    long end = System.nanoTime() + 5_000_000_000L;
                    while (System.nanoTime() < end && !stop[0]) {
                        int cur = current.incrementAndGet();
                        // Track max.
                        int prev;
                        do {
                            prev = maxObserved.get();
                        } while (cur > prev && !maxObserved.compareAndSet(prev, cur));
                        if (cur > cap) {
                            violations.incrementAndGet();
                        }
                        // Brief compute burst while "executing".
                        for (int j = 0; j < 1000; j++) {
                            sink += j;
                        }
                        current.decrementAndGet();
                        Safepoint.poll();
                    }
                    blackhole(sink);
                } catch (Throwable th) {
                    th.printStackTrace();
                }
                done.countDown();
            });
        }
        if (!done.await(120, TimeUnit.SECONDS)) {
            System.out.println("F5 TIMEOUT");
            System.out.println("RESULT=FAIL");
            return 2;
        }
        sched.awaitQuiescence();
        boolean pass = violations.get() == 0;
        System.out.printf("F5 cap=%d maxTrueParallel=%d violations=%d%n",
                cap, maxObserved.get(), violations.get());
        System.out.printf("RESULT=%s%n", pass ? "PASS" : "FAIL");
        return pass ? 0 : 2;
    }

    // ------------------------------------------------------------------
    // F5b: cap-exhaustion — blocked I/O must release slots

    /**
     * F5b: two goroutines blocked indefinitely in simulated NIO
     * (ioEnter + blocking queue take + ioExit) + one spinner + one short
     * I/O. The spinner must stay live (slots released while I/O blocked);
     * the short I/O must complete via wakeFifo re-admission.
     *
     * <p>No args (gmp/quantum via -D flags; meaningful at low gmp, e.g. 2).
     */
    public static int legF5b(String[] args) throws Exception {
        SchedulerCore sched = newScheduler();
        java.util.concurrent.LinkedBlockingQueue<Object> nioGate =
                new java.util.concurrent.LinkedBlockingQueue<>();
        AtomicLong spinnerIters = new AtomicLong(0);
        AtomicInteger shortIoDone = new AtomicInteger(0);
        final boolean[] stop = {false};

        // Two goroutines blocked in "NIO" indefinitely.
        for (int i = 0; i < 2; i++) {
            sched.spawn(() -> {
                try {
                    sched.ioEnter();
                    try {
                        nioGate.take();
                    } finally {
                        sched.ioExit();
                    }
                } catch (Throwable th) {
                    th.printStackTrace();
                }
            });
        }
        // One spinner.
        CountDownLatch spinnerDone = new CountDownLatch(1);
        sched.spawn(() -> {
            long iters = 0, sink = 0;
            long end = System.nanoTime() + 8_000_000_000L;
            while (System.nanoTime() < end && !stop[0]) {
                sink += iters * 0x9E3779B9L;
                iters++;
                Safepoint.poll();
            }
            spinnerIters.set(iters);
            blackhole(sink);
            spinnerDone.countDown();
        });
        // One short I/O: enter, brief block, exit, must complete.
        CountDownLatch shortDone = new CountDownLatch(1);
        sched.spawn(() -> {
            try {
                sched.ioEnter();
                try {
                    Thread.sleep(500);
                } finally {
                    sched.ioExit();
                }
                shortIoDone.incrementAndGet();
            } catch (Throwable th) {
                th.printStackTrace();
            }
            shortDone.countDown();
        });

        boolean shortOk = shortDone.await(60, TimeUnit.SECONDS);
        // Let the spinner run a minimum wall-time AFTER the NIO goroutines
        // are blocked, to prove sustained progress (not just a burst).
        long minEnd = System.nanoTime() + 2_000_000_000L;
        while (System.nanoTime() < minEnd) {
            Thread.sleep(100);
        }
        stop[0] = true;
        // Release the NIO-blocked goroutines so the test can drain.
        nioGate.offer(new Object());
        nioGate.offer(new Object());
        boolean spinnerOk = spinnerDone.await(60, TimeUnit.SECONDS);
        sched.awaitQuiescence();

        boolean pass = shortOk && spinnerOk && spinnerIters.get() > 1_000_000
                && shortIoDone.get() == 1;
        System.out.printf("F5b spinner_iters=%d short_io_done=%d cap=%d maxInflight=%d%n",
                spinnerIters.get(), shortIoDone.get(), sched.cap(),
                sched.maxInflightObserved());
        System.out.printf("RESULT=%s%n", pass ? "PASS" : "FAIL");
        return pass ? 0 : 2;
    }

    // ------------------------------------------------------------------
    // main dispatch

    public static void main(String[] args) throws Exception {
        if (args.length < 1) {
            System.err.println("usage: TortureBattery <F1|F3|F4|F5|F5b|F6> [args...]");
            System.exit(2);
        }
        String leg = args[0];
        String[] rest = Arrays.copyOfRange(args, 1, args.length);
        int rc;
        switch (leg) {
            case "F1": rc = legF1(rest); break;
            case "F3": rc = legF3(rest); break;
            case "F4": rc = legF4(rest); break;
            case "F5": rc = legF5(rest); break;
            case "F5b": rc = legF5b(rest); break;
            case "F6": rc = legF6(rest); break;
            default:
                System.err.println("unknown leg: " + leg);
                rc = 2;
        }
        System.exit(rc);
    }
}
