package work;
import loomsim.*;
public class SelDump {
    @SuppressWarnings({"unchecked","rawtypes"})
    public static void main(String[] a) throws Exception {
        Safepoint.mode = Safepoint.Mode.POLL; Watchdog.start();
        int nCases = 3;
        GoChannel<Integer>[] chs = new GoChannel[nCases];
        for (int i = 0; i < nCases; i++) chs[i] = new GoChannel<>(1);
        final Thread[] feeders = new Thread[nCases];
        for (int i = 0; i < nCases; i++) {
            final GoChannel<Integer> ch = chs[i]; final int id = i;
            feeders[i] = Go.go(() -> { for (int k = 0; k < 1000000; k++) ch.send(id); });
        }
        Thread.sleep(3000);
        for (Thread f : feeders) {
            System.out.println(f.getName() + " state=" + f.getState());
            for (StackTraceElement s : f.getStackTrace()) {
                System.out.println("    " + s);
                if (s.getClassName().startsWith("loomsim")) break;
            }
        }
        System.exit(0);
    }
}
