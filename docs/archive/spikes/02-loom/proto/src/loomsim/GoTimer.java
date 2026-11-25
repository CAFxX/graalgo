package loomsim;

import java.util.PriorityQueue;

/**
 * Go time.After / time.Sleep model. A single daemon PLATFORM thread (not a
 * carrier — like Go's dedicated timer machinery, but far simpler) owns a
 * deadline priority queue; on fire it delivers to a buffered-1 channel via
 * trySend (never blocks the timer thread — Go's runtime timer send likewise
 * never blocks because the channel buffer is empty).
 */
public final class GoTimer {
    private record Deadline(long fireNanos, GoChannel<Long> ch) implements Comparable<Deadline> {
        public int compareTo(Deadline o) { return Long.compare(fireNanos, o.fireNanos); }
    }

    private static final PriorityQueue<Deadline> pq = new PriorityQueue<>();
    private static volatile boolean running = false;

    public static synchronized void start() {
        if (running) return;
        running = true;
        Thread.ofPlatform().daemon(true).name("loom-timer").start(() -> {
            for (;;) {
                Deadline d;
                long waitNs;
                synchronized (GoTimer.class) {
                    while (pq.isEmpty()) {
                        try { GoTimer.class.wait(); }
                        catch (InterruptedException e) { Thread.currentThread().interrupt(); return; }
                    }
                    d = pq.peek();
                    waitNs = d.fireNanos() - System.nanoTime();
                    if (waitNs > 0) {
                        try { GoTimer.class.wait(waitNs / 1_000_000L, (int) (waitNs % 1_000_000L)); }
                        catch (InterruptedException e) { Thread.currentThread().interrupt(); return; }
                        continue;
                    }
                    pq.poll();
                }
                d.ch().trySend(System.nanoTime());
            }
        });
    }

    /** Like time.After(d): channel receiving one value after d. */
    public static GoChannel<Long> after(long delayNanos) {
        GoChannel<Long> ch = new GoChannel<>(1);
        synchronized (GoTimer.class) {
            pq.add(new Deadline(System.nanoTime() + delayNanos, ch));
            GoTimer.class.notifyAll();
        }
        return ch;
    }

    /** Like time.Sleep(d). */
    public static void sleep(long delayNanos) {
        after(delayNanos).recv();
    }

    private GoTimer() {}
}
