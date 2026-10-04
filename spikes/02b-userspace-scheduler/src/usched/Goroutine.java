package usched;

import java.util.concurrent.atomic.AtomicLong;

/**
 * Goroutine control block for spike 2b. One per virtual thread.
 *
 * parkSeq/acked/armed implement the lost-wakeup-free park protocol with the
 * UserScheduler drainer (see UserScheduler for the full contract):
 * - parkSeq: next park generation, reserved via armPark() before the waiter
 *   becomes visible to wakers.
 * - acked: highest generation the drainer has admitted (set BEFORE unpark).
 * - armed: currently-armed generation, or -1 none, or -2 consumed-by-wakeup.
 */
public final class Goroutine {
    private static final AtomicLong nextId = new AtomicLong(1);
    private static final ThreadLocal<Goroutine> current = new ThreadLocal<>();

    public final long id = nextId.getAndIncrement();
    volatile Thread thread;

    final AtomicLong parkSeq = new AtomicLong(0);
    final AtomicLong acked = new AtomicLong(-1);
    final AtomicLong armed = new AtomicLong(-1);

    /** Quantum accounting: start of the current quantum (nanos). */
    volatile long quantumStart = System.nanoTime();
    /** Probe slot for LatencyProbe (diagnostic only). */
    public volatile int probeSlot = -1;
    /** Back-edge poll counter (amortizes the nanoTime check). */
    long pollCount = 0;
    /** Quantum parks taken (fairness metric for the quiescence probe). */
    public long quanta = 0;

    static Goroutine enter(Thread t) {
        Goroutine g = new Goroutine();
        g.thread = t;
        current.set(g);
        return g;
    }

    static void exit() {
        current.remove();
    }

    public static Goroutine current() {
        return current.get();
    }

    private Goroutine() {}
}
