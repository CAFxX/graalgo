package work;

import loomsim.*;

/**
 * O3 — select fairness. 8 channels pre-filled (buffered, no feeders — pure
 * poll-order fairness, no wakeup-latency confound); 20k iterations of
 * select-over-all with random poll order (Go: fastrand). Measures per-case
 * selection counts; Go's select is pseudo-random among ready cases, so the
 * distribution must be ~uniform. Reports counts, expected, max |z| vs uniform.
 */
public final class SelectFairness {
    @SuppressWarnings({"unchecked", "rawtypes"})
    public static void main(String[] args) throws Exception {
        int nCases = 8, iters = 20_000;
        if (args.length > 0) iters = Integer.parseInt(args[0]);
        Safepoint.mode = Safepoint.Mode.OFF; // select fairness is about poll order, not preemption

        int perCh = iters / nCases + 100;
        GoChannel<Integer>[] chs = new GoChannel[nCases];
        for (int i = 0; i < nCases; i++) {
            chs[i] = new GoChannel<>(perCh);
            for (int k = 0; k < perCh; k++) chs[i].trySend(i);
        }
        GoSelect.Case[] cases = new GoSelect.Case[nCases];
        for (int i = 0; i < nCases; i++) cases[i] = new GoSelect.RecvCase(chs[i]);

        long[] hits = new long[nCases];
        Object[] out = new Object[nCases];
        for (int k = 0; k < iters; k++) {
            int idx = GoSelect.select(cases, false, out);
            hits[idx]++;
        }
        double exp = (double) iters / nCases;
        double maxZ = 0;
        for (int i = 0; i < nCases; i++) {
            double z = Math.abs(hits[i] - exp) / Math.sqrt(exp * (1 - 1.0 / nCases));
            maxZ = Math.max(maxZ, z);
        }
        System.out.printf("cases=%d iters=%d expected=%.0f%n", nCases, iters, exp);
        System.out.printf("hits=%s max_abs_z=%.2f%n", java.util.Arrays.toString(hits), maxZ);
        System.exit(0);
    }
}
