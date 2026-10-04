package work;

import loomsim.*;

/** Mid-run dump for the 2-spinner starvation case. */
public class DumpStarve {
    public static void main(String[] a) throws Exception {
        Safepoint.mode = Safepoint.Mode.POLL;
        PreemptTorture.stop = false;
        Thread t = new Thread(() -> {
            try { PreemptTorture.main(new String[]{"POLL", "2", "20"}); }
            catch (Exception e) { e.printStackTrace(); }
        });
        t.setDaemon(true);
        t.start();
        Thread.sleep(8000);
        System.out.println("=== vthread states at t=8s ===");
        Thread.getAllStackTraces().forEach((th, st) -> {
            if (th.isVirtual()) {
                String top = st.length > 0 ? st[0].toString() : "-";
                String inChan = "";
                for (StackTraceElement s : st) {
                    if (s.getClassName().startsWith("loomsim") || s.getClassName().startsWith("work")) {
                        inChan = s.toString(); break;
                    }
                }
                System.out.println("VTH " + th.getName() + " state=" + th.getState() + " @ " + inChan);
            } else if (th.getName().startsWith("ForkJoinPool")) {
                String top = st.length > 2 ? st[2].toString() : "?";
                System.out.println("CAR " + th.getName() + " state=" + th.getState() + " @ " + top);
            }
        });
        PreemptTorture.stop = true;
        t.join(20000);
        System.exit(0);
    }
}
