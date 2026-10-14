package work;

import usched.*;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.atomic.AtomicLong;

/**
 * P0-2 falsifier: cap-exhaustion starvation via scheduler-transparent I/O.
 *
 * Setup (cap=2): two goroutines block INDEFINITELY in simulated blocking I/O
 * (LinkedBlockingQueue.take() as the stand-in for a blocking NIO read: the
 * carrier unmounts, the vthread never parks on the user-space scheduler),
 * one spinner must still make progress, and a fourth goroutine performs one
 * short I/O to exercise ioExit slot re-acquisition via wakeFifo.
 *
 * The I/O-blocked goroutines start first and are given 500 ms to occupy
 * their slots before the spinner starts, so the negative control is crisp.
 *
 * - With slot-releasing ioEnter: blocked I/O holds no in-flight slot; the
 *   spinner and the short I/O proceed -> PASS.
 * - Negative control (-Dcapexhaustion.raw=true): block WITHOUT ioEnter
 *   (the pre-P0-2 behavior). Both slots are held forever; the spinner and
 *   the short I/O (stuck in initFifo) starve -> FAIL. This run is the red
 *   half of the red-green pair and is NOT part of the battery.
 *
 * Args: secs
 * Daemon-exit shutdown.
 */
public final class CapExhaustion {
    static volatile boolean stop = false;

    public static void main(String[] args) throws Exception {
        int secs = Integer.parseInt(args[0]);
        boolean raw = Boolean.getBoolean("capexhaustion.raw");
        GoTimer.start();
        UserScheduler us = UserScheduler.instance();
        LinkedBlockingQueue<Object> neverReady = new LinkedBlockingQueue<>();
        LinkedBlockingQueue<Object> quick = new LinkedBlockingQueue<>();
        AtomicLong completions = new AtomicLong(0);
        AtomicLong spinIters = new AtomicLong(0);

        // Two indefinitely-blocked I/O goroutines: start first, grab slots.
        for (int i = 0; i < 2; i++) {
            final int idx = i;
            Go.go("ioblocked-" + idx, () -> {
                Goroutine g = Goroutine.current();
                long ticket = raw ? -1 : us.ioEnter(g);
                try {
                    neverReady.take(); // stand-in: blocking NIO read
                } catch (InterruptedException ie) {
                    Thread.currentThread().interrupt();
                } finally {
                    if (!raw) us.ioExit(g, ticket);
                }
            });
        }
        Thread.sleep(500); // let the blocked pair occupy their slots

        // One short I/O: exercises ioExit re-acquisition through wakeFifo.
        Go.go("ioshort", () -> {
            Goroutine g = Goroutine.current();
            long ticket = raw ? -1 : us.ioEnter(g);
            try {
                Object o = quick.take();
                Blackhole.consume(o == null ? 0 : 1);
            } catch (InterruptedException ie) {
                Thread.currentThread().interrupt();
            } finally {
                if (!raw) {
                    us.ioExit(g, ticket);
                    completions.incrementAndGet();
                }
            }
        });
        Go.go("spinner", () -> {
            long c = 0, sink = 0;
            while (!stop) { sink += c * 0x9E3779B97F4A7C15L; sink ^= sink >>> 29; c++; Safepoint.poll(); }
            spinIters.set(c);
            Blackhole.consume(sink);
        });

        Thread.sleep(1000); // let everyone settle, then satisfy the short I/O
        quick.offer(new Object());

        Thread.sleep(secs * 1000L);
        stop = true;
        Thread.sleep(500);

        long spins = spinIters.get();
        long done = completions.get();
        // One spinner at cap=2 does ~20M iters/s (O1: 2 spinners ~103M/5s
        // each); 1M is a 100x-margin liveness floor. In raw mode the
        // spinner may snatch a quantum during startup, but never millions.
        boolean live = spins > 1_000_000;
        boolean ioOk = done == 1;
        System.out.printf("capexhaustion raw=%b secs=%d cap=%d spinner_iters=%d short_io_done=%d%n",
                raw, secs, us.cap(), spins, done);
        System.out.printf("liveness(spinner>1M iters)=%b short_io_reacquired=%b%n", live, ioOk);
        System.out.println(us.statsLine());
        boolean pass = live && (raw || ioOk);
        System.out.printf("RESULT=%s%n", pass ? "PASS" : "FAIL");
        System.out.flush();
        System.exit(pass ? 0 : 2);
    }
}
