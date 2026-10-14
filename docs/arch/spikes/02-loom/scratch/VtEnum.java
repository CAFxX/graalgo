public class VtEnum {
    public static void main(String[] a) throws Exception {
        Thread v = Thread.ofVirtual().name("parked-vt").start(() -> {
            try { Thread.sleep(30000); } catch (InterruptedException e) {}
        });
        Thread.sleep(500);
        int n = 0;
        for (Thread th : Thread.getAllStackTraces().keySet()) {
            if (th.isVirtual()) { n++; System.out.println("found vthread: " + th.getName() + " " + th.getState()); }
        }
        System.out.println("vthreads enumerated: " + n);
        System.exit(0);
    }
}
