package work;

import loomsim.*;

/**
 * Back-edge yield overhead. Tight loop of N iterations with Safepoint.poll()
 * at the back-edge; modes OFF/POLL/UNCOND. Reports ns/iteration.
 *
 * Honesty note: this measures the INTERPRETED-model cost (an explicit poll
 * call per back-edge, as the Truffle AST would emit before PE). After partial
 * evaluation Graal would fold the POLL check into a single branch on a
 * volatile read; the compiled-mode cost is bounded above by these numbers.
 */
public final class YieldOverhead {
    public static void main(String[] args) {
        Safepoint.Mode mode = Safepoint.Mode.valueOf(args[0]);
        long iters = Long.parseLong(args[1]);
        Safepoint.mode = mode;
        if (mode == Safepoint.Mode.POLL) Watchdog.start();

        // warmup
        long x = 0;
        for (long i = 0; i < iters / 10; i++) { x += i * 0x9E3779B97F4A7C15L; Safepoint.poll(); }

        long t0 = System.nanoTime();
        for (long i = 0; i < iters; i++) { x += i * 0x9E3779B97F4A7C15L; Safepoint.poll(); }
        long dt = System.nanoTime() - t0;
        Blackhole.consume(x);
        System.out.printf("mode=%s iters=%d ns_per_iter=%.2f%n", mode, iters, (double) dt / iters);
        Watchdog.stop();
    }
}
