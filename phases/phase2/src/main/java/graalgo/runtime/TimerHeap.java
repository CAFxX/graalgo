package graalgo.runtime;

import java.util.HashMap;
import java.util.Map;
import java.util.PriorityQueue;
import java.util.concurrent.atomic.AtomicLong;

import graalgo.sched.SchedulerCore;
import graalgo.sched.GoroutineHandle;

/**
 * Scheduler-owned timer heap (P2-M3, WS C; GG-SCHED-0004, GG-0018).
 *
 * <p>A priority queue of deadlines served by a single daemon platform
 * thread (not a carrier — it never runs goroutines and never counts
 * against the in-flight cap). Fire actions:
 * <ul>
 *   <li>SLEEP entries: {@code sched.unpark(handle)} — the waiter parked
 *       via {@code SchedulerCore.parkCurrent} (sleep/startTimer).
 *       UNPARK-before-PARKED is safe via the drainer's unparkPending
 *       (P2-D7), so registering before parking cannot lose the wakeup.
 *   <li>CHAN entries: non-blocking {@code trySend} of the fire nanotime
 *       on the channel, from the timer thread (never parks). A full
 *       buffer drops the tick — gc ticker parity for slow receivers.
 * </ul>
 *
 * <p>Locking: all heap/byId mutation under {@link #lock}. Re-arm and
 * staleness checks additionally synchronize on the entry owner, always
 * in the order heap-lock → owner-lock (never the reverse).
 *
 * <p>verifies: GG-SCHED-0004 (timer slice), GG-0018 (timer heap, not a
 * netpoller — readiness-based I/O does not exist here)
 */
public final class TimerHeap {
    /** Callback for channel-timer re-arm/staleness (implemented by GoTimer). */
    interface ChanOwner {
        /** Period for tickers; 0 for one-shot timers. */
        long periodNanos();

        /**
         * True iff (id, generation) is still the owner's current timer.
         * Called with the heap lock held; implementations must not take
         * the heap lock (order is heap → owner).
         */
        boolean stillCurrent(long id, long generation);
    }

    private static final class Entry implements Comparable<Entry> {
        long deadlineNanos;
        final long id;
        final long seq;
        final GoroutineHandle handle; // SLEEP: unpark target (null for CHAN/CALLBACK)
        final GoChan chan; // CHAN: delivery target (null for SLEEP/CALLBACK)
        final Runnable callback; // CALLBACK: runs on the timer thread (null otherwise)
        final ChanOwner owner; // CHAN ticker/re-arm owner (null for one-shot/SLEEP)
        final long generation; // owner generation at arm time
        /** Ticker period; 0 for one-shot timers. Mutable only for re-arm. */
        long periodNanos;
        boolean cancelled;

        Entry(long deadlineNanos, long id, long seq,
              GoroutineHandle handle, GoChan chan, Runnable callback,
              ChanOwner owner, long generation) {
            this.deadlineNanos = deadlineNanos;
            this.id = id;
            this.seq = seq;
            this.handle = handle;
            this.chan = chan;
            this.callback = callback;
            this.owner = owner;
            this.generation = generation;
        }

        @Override
        public int compareTo(Entry o) {
            int c = Long.compare(deadlineNanos, o.deadlineNanos);
            return c != 0 ? c : Long.compare(seq, o.seq);
        }
    }

    private final SchedulerCore sched;
    private final Object lock = new Object();
    private final PriorityQueue<Entry> heap = new PriorityQueue<>();
    private final Map<Long, Entry> byId = new HashMap<>();
    private final AtomicLong idGen = new AtomicLong(1);
    private final AtomicLong seqGen = new AtomicLong(1);
    private Thread timerThread;

    public TimerHeap(SchedulerCore sched) {
        if (sched == null) {
            throw new IllegalArgumentException("TimerHeap needs a SchedulerCore");
        }
        this.sched = sched;
    }

    /** Starts the timer thread. Idempotent; called by SchedulerCore.start(). */
    public synchronized void start() {
        if (timerThread != null) {
            return;
        }
        timerThread = Thread.ofPlatform().daemon(true).name("graalgo-timer")
                .start(this::timerLoop);
    }

    /**
     * Arms a sleep timer: at {@code deadlineNanos} (System.nanoTime
     * basis), unparks {@code handle}. Returns the timer id (for
     * {@link #cancel}).
     */
    public long addSleep(long deadlineNanos, GoroutineHandle handle) {
        if (handle == null) {
            throw new IllegalArgumentException("sleep timer needs a handle");
        }
        return add(deadlineNanos, handle, null, null, null, 0, 0);
    }

    /**
     * Arms a raw callback timer: at {@code deadlineNanos}, runs
     * {@code callback} on the timer thread (gc timer-proc parity). The
     * callback must not block and must not call scheduler park
     * operations. Returns the timer id (for {@link #cancel}).
     */
    public long addCallback(long deadlineNanos, Runnable callback) {
        if (callback == null) {
            throw new IllegalArgumentException("callback timer needs a callback");
        }
        return add(deadlineNanos, null, null, callback, null, 0, 0);
    }

    /**
     * Arms a channel timer: at {@code deadlineNanos}, delivers the fire
     * nanotime on {@code chan} (non-blocking; drop if full).
     * {@code owner} is non-null for tickers (re-arm); {@code periodNanos}
     * is the ticker period (0 for one-shot).
     */
    long addChan(long deadlineNanos, GoChan chan, ChanOwner owner,
                 long periodNanos, long generation) {
        if (chan == null) {
            throw new IllegalArgumentException("channel timer needs a channel");
        }
        return add(deadlineNanos, null, chan, null, owner, periodNanos, generation);
    }

    private long add(long deadlineNanos, GoroutineHandle handle, GoChan chan,
                     Runnable callback, ChanOwner owner,
                     long periodNanos, long generation) {
        synchronized (lock) {
            long id = idGen.getAndIncrement();
            Entry e = new Entry(deadlineNanos, id, seqGen.getAndIncrement(),
                    handle, chan, callback, owner, generation);
            // Ticker entries carry their period via the owner; store it
            // on the entry for the re-arm path.
            e.periodNanos = periodNanos;
            heap.add(e);
            byId.put(id, e);
            lock.notifyAll();
            return id;
        }
    }

    /**
     * Cancels a timer. Returns true iff the timer was still pending
     * (a firing/fired timer returns false — gc Stop parity, including
     * the racy pop-then-fire window).
     */
    public boolean cancel(long id) {
        synchronized (lock) {
            Entry e = byId.remove(id);
            if (e == null) {
                return false;
            }
            e.cancelled = true;
            // Lazy removal from the heap: the timer loop skips cancelled
            // heads. Wake it in case it is waiting on this entry.
            lock.notifyAll();
            return true;
        }
    }

    /**
     * Stops a channel timer owned by {@code owner}: cancels the current
     * entry and clears the owner's id. Returns true iff the timer was
     * active (gc Timer.Stop parity). Go 1.23+ parity: a fired-but-
     * undrained timer counts as active — its pending value is drained
     * and Stop returns true. Heap lock → owner lock order.
     */
    boolean stopChan(GoTimer owner) {
        synchronized (lock) {
            synchronized (owner) {
                long id = owner.currentId();
                if (id < 0) {
                    return false;
                }
                boolean wasActive = cancelLocked(id);
                owner.clearIdLocked();
                owner.bumpGenerationLocked();
                if (!wasActive && owner.channelImpl().buffered() > 0) {
                    // Fired but undrained (Go 1.23+): discard the stale
                    // value; only the timer sends here, so it is ours.
                    owner.channelImpl().tryRecv(sched);
                    return true;
                }
                return wasActive;
            }
        }
    }

    /**
     * Resets a channel timer: cancels the current entry (if any),
     * re-arms for {@code deadlineNanos}. Returns true iff the timer had
     * been active (gc Timer.Reset parity, incl. the Go 1.23+ expired-
     * undrained → true + drain rule). Heap lock → owner lock order.
     */
    boolean resetChan(GoTimer owner, long deadlineNanos) {
        synchronized (lock) {
            synchronized (owner) {
                long id = owner.currentId();
                boolean wasActive = false;
                if (id >= 0) {
                    wasActive = cancelLocked(id);
                    if (!wasActive && owner.channelImpl().buffered() > 0) {
                        owner.channelImpl().tryRecv(sched);
                        wasActive = true;
                    }
                }
                owner.bumpGenerationLocked();
                long nid = addLocked(deadlineNanos, owner);
                owner.setIdLocked(nid);
                return wasActive;
            }
        }
    }

    /** Heap-lock-held cancel (no notify needed by callers that re-arm). */
    private boolean cancelLocked(long id) {
        Entry e = byId.remove(id);
        if (e == null) {
            return false;
        }
        e.cancelled = true;
        return true;
    }

    /** Heap-lock-held channel re-arm for owner (used by resetChan). */
    private long addLocked(long deadlineNanos, GoTimer owner) {
        long id = idGen.getAndIncrement();
        Entry e = new Entry(deadlineNanos, id, seqGen.getAndIncrement(),
                null, owner.channelImpl(), null, owner, owner.generationLocked());
        e.periodNanos = owner.periodNanos();
        heap.add(e);
        byId.put(id, e);
        lock.notifyAll();
        return id;
    }

    /** Pending (uncancelled, unfired) entry count; tests only. */
    int pendingCount() {
        synchronized (lock) {
            return byId.size();
        }
    }

    private void timerLoop() {
        for (;;) {
            final Entry e;
            synchronized (lock) {
                Entry next;
                for (;;) {
                    next = heap.peek();
                    if (next == null) {
                        try {
                            lock.wait();
                        } catch (InterruptedException ie) {
                            continue; // non-terminating, like the drainer
                        }
                        continue;
                    }
                    if (next.cancelled) {
                        heap.poll();
                        continue;
                    }
                    long wait = next.deadlineNanos - System.nanoTime();
                    if (wait <= 0) {
                        break;
                    }
                    try {
                        lock.wait(wait / 1_000_000L, (int) (wait % 1_000_000L));
                    } catch (InterruptedException ie) {
                        // Re-check: a new earlier timer may have arrived.
                    }
                }
                e = heap.poll();
                byId.remove(e.id);
            }
            fire(e);
        }
    }

    /**
     * Fires one entry, outside the heap lock. Channel delivery is
     * non-blocking (never parks the timer thread); ticker re-arm
     * re-checks ownership to close the fire-vs-reset race. Raw callbacks
     * run on the timer thread (gc timer-proc parity — must not block).
     */
    private void fire(Entry e) {
        if (e.callback != null) {
            try {
                e.callback.run();
            } catch (Throwable t) {
                // A throwing timer callback must not kill the timer
                // thread (that would stall every timer). Loud, then
                // continue — same discipline as the drainer's fatal halt,
                // but scoped: only this callback is lost.
                System.err.println("graalgo-timer: callback threw: " + t);
            }
            return;
        }
        if (e.chan != null) {
            e.chan.trySend(System.nanoTime(), sched);
            if (e.periodNanos > 0 && e.owner != null) {
                boolean rearm;
                synchronized (lock) {
                    synchronized (e.owner) {
                        rearm = !e.cancelled && e.owner.stillCurrent(e.id, e.generation);
                    }
                    if (rearm) {
                        e.deadlineNanos += e.periodNanos;
                        long now = System.nanoTime();
                        if (e.deadlineNanos < now) {
                            // More than a period behind (long stall):
                            // skip the missed ticks rather than spinning
                            // (gc drops ticks for slow receivers).
                            e.deadlineNanos = now + e.periodNanos;
                        }
                        heap.add(e);
                        byId.put(e.id, e);
                        lock.notifyAll();
                    }
                }
            }
            return;
        }
        // SLEEP: wake the parked waiter (unpark-before-parked is safe).
        sched.unpark(e.handle);
    }
}
