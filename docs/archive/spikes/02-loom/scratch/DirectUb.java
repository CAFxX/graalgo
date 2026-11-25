package work;
import loomsim.*;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.locks.LockSupport;
public class DirectUb {
    static volatile boolean stop = false;
    public static void main(String[] args) throws Exception {
        int secs = Integer.parseInt(args[0]);
        Safepoint.mode = Safepoint.Mode.POLL; Watchdog.start();
        for (int i = 0; i < 2; i++) {
            Go.go(() -> { long s=0,c=0; while(!stop){ s+=c*0x9E3779B97F4A7C15L; s^=s>>>29; c++; Safepoint.poll(); } Blackhole.consume(s); });
        }
        AtomicInteger woke = new AtomicInteger(0);
        Thread sleeper = Go.go(() -> { LockSupport.park(); woke.set(1); });
        Thread.sleep(500);
        System.out.println("sleeper state before unpark: " + sleeper.getState());
        // unpark from a PLATFORM thread (main)
        LockSupport.unpark(sleeper);
        Thread.sleep(500);
        System.out.println("woke=" + woke.get() + " sleeper state after: " + sleeper.getState());
        stop = true;
        System.exit(0);
    }
}
