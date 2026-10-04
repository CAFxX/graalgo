package work;

import loomsim.*;

/**
 * Pinning audit. Exercises the blocking operations the runtime uses
 * (channel park, select park, timer sleep, synchronized+wait) on virtual
 * threads, plus a FORCED-pin control (blocking native file read from a pipe,
 * which executes a native call and pins the carrier per JEP 491).
 *
 * Run under JFR (-XX:StartFlightRecording) and count jdk.VirtualThreadPinned
 * events with `jfr print`. NOTE: -Djdk.tracePinnedThreads no longer exists
 * on JDK 24+ (verified absent from JDK 25 sources); the JFR event is the
 * supported audit path (T7 §4.2).
 */
public final class PinAudit {
    public static void main(String[] args) throws Exception {
        boolean forcePin = args.length > 0 && args[0].equals("forcepin");
        Safepoint.mode = Safepoint.Mode.POLL;
        Watchdog.start();
        GoTimer.start();

        GoChannel<Integer> ch = new GoChannel<>();
        GoChannel<Integer> done = new GoChannel<>(1);

        // 1. channel ping-pong (park/unpark path)
        Go.go(() -> {
            for (int i = 0; i < 2000; i++) { ch.send(i); ch.recv(); }
            done.send(1);
        });
        Go.go(() -> {
            for (int i = 0; i < 2000; i++) { ch.recv(); ch.send(i); }
        });

        // 2. select blocking path
        GoChannel<Integer> a = new GoChannel<>(), b = new GoChannel<>();
        Go.go(() -> {
            GoTimer.sleep(50_000_000L);
            a.send(42);
        });
        Go.go(() -> {
            Object[] out = new Object[2];
            int idx = GoSelect.select(
                new GoSelect.Case[]{ new GoSelect.RecvCase<>(a), new GoSelect.RecvCase<>(b) },
                false, out);
            Blackhole.consume(idx + (Integer) out[idx]);
        });

        // 3. synchronized block + wait (JEP 491: must NOT pin)
        Object mon = new Object();
        Go.go(() -> {
            synchronized (mon) {
                try { mon.wait(80); } catch (InterruptedException e) { Thread.currentThread().interrupt(); }
            }
        });

        if (forcePin) {
            // 4. CONTROL: blocking native accept() pins the carrier (JEP 491:
            //    native calls still pin). Proves the JFR event fires at all.
            java.net.ServerSocket ss = new java.net.ServerSocket(0);
            Go.go(() -> {
                try {
                    ss.accept(); // blocks in native accept -> pinned carrier
                } catch (Exception e) { /* expected on close */ }
            });
            Thread.sleep(300);
            ss.close(); // release the acceptor
        }

        done.recv(); // wait for the ping-pong to finish
        Thread.sleep(500); // let select/wait paths complete
        System.out.println("audit workload complete forcePin=" + forcePin);
        Watchdog.stop();
        System.exit(0);
    }
}
