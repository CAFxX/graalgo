package work;
import loomsim.*;
public class SelMini {
    @SuppressWarnings({"unchecked","rawtypes"})
    public static void main(String[] a) throws Exception {
        Safepoint.mode = Safepoint.Mode.POLL; Watchdog.start();
        int nCases = 3, iters = 20;
        GoChannel<Integer>[] chs = new GoChannel[nCases];
        for (int i = 0; i < nCases; i++) chs[i] = new GoChannel<>(1);
        GoSelect.Case[] cases = new GoSelect.Case[nCases];
        for (int i = 0; i < nCases; i++) cases[i] = new GoSelect.RecvCase(chs[i]);
        for (int i = 0; i < nCases; i++) {
            final GoChannel<Integer> ch = chs[i]; final int id = i;
            Go.go(() -> { for (int k = 0; k < 1000; k++) ch.send(id); });
        }
        Thread.sleep(500);
        Object[] out = new Object[nCases];
        long t0 = System.nanoTime();
        for (int k = 0; k < iters; k++) {
            int idx = GoSelect.select(cases, false, out);
            if (k % 5 == 0) System.out.println("iter " + k + " -> " + idx + " t=" + (System.nanoTime()-t0)/1000000 + "ms");
        }
        System.out.println("done");
        System.exit(0);
    }
}
