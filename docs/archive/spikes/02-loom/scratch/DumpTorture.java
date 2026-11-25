package work;
import loomsim.*;


public class DumpTorture {
    public static void main(String[] a) throws Exception {
        Safepoint.mode = Safepoint.Mode.POLL;
        Watchdog.start();
        GoTimer.start();
        PreemptTorture.stop = false;
        Thread t = new Thread(() -> {
            try { PreemptTorture.main(new String[]{"POLL", "1", "12"}); }
            catch (Exception e) { e.printStackTrace(); }
        });
        t.setDaemon(true);
        t.start();
        Thread.sleep(6000);
        final int[] ncar = {0};
        Thread.getAllStackTraces().forEach((th, st) -> {
            if (th.isVirtual()) {
                System.out.println("VTH " + th.getName() + " state=" + th.getState());
                for (int i = 0; i < Math.min(5, st.length); i++) System.out.println("    " + st[i]);
            } else if (th.getName().startsWith("ForkJoinPool")) {
                ncar[0]++;
                System.out.println("CAR " + th.getName() + " state=" + th.getState());
                for (int i = 0; i < Math.min(8, st.length); i++) System.out.println("    " + st[i]);
            }
        });
        System.out.println("carriers=" + ncar[0]);
        PreemptTorture.stop = true;
        t.join(15000);
        System.exit(0);
    }
}
