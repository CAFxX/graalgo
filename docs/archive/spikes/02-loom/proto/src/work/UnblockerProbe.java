package work;

import loomsim.*;
import java.util.concurrent.ConcurrentLinkedQueue;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.locks.LockSupport;

/**
 * Workaround probe: does routing wakeups through a platform-thread unblocker
 * (so woken continuations take the EXTERNAL submit path to a probe queue,
 * not the notifier's carrier-local LIFO queue) fix the O1 starvation?
 *
 * Ping-pong via LockSupport.park/unpark; the unpark is issued by a dedicated
 * platform thread fed by a queue. 2 spinners saturate 2 carriers (POLL).
 */
public final class UnblockerProbe {
    static volatile boolean stop = false;

    static final class Unblocker {
        private final ConcurrentLinkedQueue<Thread> q = new ConcurrentLinkedQueue<>();
        private Thread thread;
        Unblocker() {
            thread = Thread.ofPlatform().daemon(true).name("unblocker").start(() -> {
                for (;;) {
                    Thread t = q.poll();
                    if (t != null) LockSupport.unpark(t);
                    else LockSupport.parkNanos(50_000L);
                }
            });
        }
        void unpark(Thread t) { q.add(t); LockSupport.unpark(thread); }
    }

    public static void main(String[] args) throws Exception {
        int secs = Integer.parseInt(args[0]);
        Safepoint.mode = Safepoint.Mode.POLL;
        Watchdog.start();
        Unblocker ub = new Unblocker();

        for (int i = 0; i < 2; i++) {
            Go.go(() -> {
                long sink = 0, c = 0;
                while (!stop) { sink += c * 0x9E3779B97F4A7C15L; sink ^= sink >>> 29; c++; Safepoint.poll(); }
                Blackhole.consume(sink);
            });
        }

        // ping-pong with explicit partner threads + platform-thread unblocker
        final Thread[] partner = new Thread[2];
        AtomicInteger nSamples = new AtomicInteger(0);
        partner[0] = Go.go(() -> {
            int n = 0;
            while (!stop && n < 1_000_000) {
                LockSupport.park();          // wait for ponger
                if (stop) break;
                ub.unpark(partner[1]);       // wake ponger via platform thread
                n++;
                nSamples.set(n);
            }
        });
        partner[1] = Go.go(() -> {
            while (!stop) {
                ub.unpark(partner[0]);       // wake pinger via platform thread
                LockSupport.park();          // wait for pinger
            }
        });
        // kick-start
        Thread.sleep(200);
        ub.unpark(partner[0]);

        Thread.sleep(secs * 1000L);
        stop = true;
        ub.unpark(partner[0]); ub.unpark(partner[1]);
        Thread.sleep(300);
        int n = nSamples.get();
        System.out.printf("samples=%d watchdog_pulses=%d yields=%d%n",
                n, Watchdog.pulses.get(), Safepoint.yieldsHonored.get());
        System.out.println(n > 100 ? "WORKAROUND: PAIR PROGRESSES" : "WORKAROUND: STILL STARVED");
        Watchdog.stop();
        System.exit(0);
    }
}
