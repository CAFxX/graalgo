package usched;

import java.util.PriorityQueue;

/**
 * Timer model wired to the user-space scheduler (spike 2b). A single daemon
 * platform thread owns a deadline priority queue; on fire it wins the
 * sleeper's wakeup via the armed-ticket CAS. Sleepers park IDLE (not
 * runnable) so they don't consume GOMAXPROCS slots while waiting.
 */
public final class GoTimer {
    private record Deadline(long fireNanos, Goroutine g, long ticket)
            implements Comparable<Deadline> {
        public int compareTo(Deadline o) { return Long.compare(fireNanos, o.fireNanos); }
    }

    private static final PriorityQueue<Deadline> pq = new PriorityQueue<>();
    private static volatile boolean running = false;

    public static synchronized void start() {
        if (running) return;
        running = true;
        Thread.ofPlatform().daemon(true).name("usched-timer").start(() -> {
            for (;;) {
                Deadline d;
                long waitNs;
                synchronized (GoTimer.class) {
                    while (pq.isEmpty()) {
                        try {
                            GoTimer.class.wait();
                        } catch (InterruptedException e) {
                            Thread.currentThread().interrupt();
                            return;
                        }
                    }
                    d = pq.peek();
                    waitNs = d.fireNanos() - System.nanoTime();
                    if (waitNs > 0) {
                        try {
                            GoTimer.class.wait(waitNs / 1_000_000L, (int) (waitNs % 1_000_000L));
                        } catch (InterruptedException e) {
                            Thread.currentThread().interrupt();
                            return;
                        }
                        continue;
                    }
                    pq.poll();
                }
                UserScheduler.instance().wakeup(d.g(), d.ticket());
            }
        });
    }

    /** Park the current goroutine until nanos elapse. */
    public static void sleep(long nanos) {
        Goroutine g = Goroutine.current();
        UserScheduler us = UserScheduler.instance();
        long t = us.armPark(g);
        synchronized (GoTimer.class) {
            pq.add(new Deadline(System.nanoTime() + nanos, g, t));
            GoTimer.class.notifyAll();
        }
        us.parkIdle(g, t);
    }

    private GoTimer() {}
}
