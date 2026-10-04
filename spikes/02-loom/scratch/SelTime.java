package work;
import loomsim.*;
public class SelTime {
    @SuppressWarnings({"unchecked","rawtypes"})
    public static void main(String[] a) throws Exception {
        Safepoint.mode = Safepoint.Mode.OFF;
        int nCases = 3, iters = 30;
        GoChannel<Integer>[] chs = new GoChannel[nCases];
        for (int i = 0; i < nCases; i++) chs[i] = new GoChannel<>(1);
        GoSelect.Case[] cases = new GoSelect.Case[nCases];
        for (int i = 0; i < nCases; i++) cases[i] = new GoSelect.RecvCase(chs[i]);
        for (int i = 0; i < nCases; i++) {
            final GoChannel<Integer> ch = chs[i]; final int id = i;
            Go.go(() -> { for (int k = 0; k < 1000000; k++) ch.send(id); });
        }
        Thread.sleep(500);
        Object[] out = new Object[nCases];
        for (int k = 0; k < iters; k++) {
            long t0 = System.nanoTime();
            int idx = GoSelect.select(cases, false, out);
            long dt = System.nanoTime() - t0;
            System.out.printf("iter %d -> %d dt=%.1fms%n", k, idx, dt/1e6);
        }
        System.exit(0);
    }
}
