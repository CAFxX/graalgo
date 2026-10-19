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
 *       via {@code SchedulerCore.parkCurrent} (sleep only — P2-4:
 *       startTimer never parks).
 *       UNPARK-before-PARKED is safe via the drainer's unparkPending
 *       (P2-D7), so registering before parking cannot lose the wakeup.
 *   <li>CHAN entries: non-blocking {@code trySend} of the fire nanotime
 *       on the channel, from the timer thread (never parks). A full
 *       buffer drops the tick — gc ticker parity for slow receivers.
 * </ul>
 *
 * <p>Locking: all heap/byId mutation under {@link #lock}. For channel
 * entries, the staleness check, the non-blocking send, and the ticker
 * re-arm execute atomically under the heap lock (the send cannot
 * interleave with stop/reset — this closes gc's stale-send race
 * structurally; gc needs its seq/sendLock protocol only because it
 * cannot hold its timer lock across a blocking send). Re-arm and
 * staleness checks additionally synchronize on the entry owner, always
 * in the order heap-lock → owner-lock (never the reverse). Channel
 * monitor is the leaf: heap → owner → chan.
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
        return addCallbackWithId(reserveId(), deadlineNanos, callback);
    }

    /**
     * Reserves a timer id for a subsequent {@link #addCallbackWithId}
     * call. Lets a caller publish the id (e.g. in a pollDesc) BEFORE the
     * heap entry becomes fireable, closing the publish-vs-fire race: a
     * callback that fires after the reservation observes the reserved
     * id even if the caller has not finished arming yet (R-1).
     */
    public long reserveId() {
        return idGen.getAndIncrement();
    }

    /**
     * Arms a raw callback timer under a previously {@linkplain #reserveId
     * reserved} id: at {@code deadlineNanos}, runs {@code callback} on
     * the timer thread (same contract as {@link #addCallback}). The id is
     * never reassigned; a reserved-but-never-added id is harmless (it
     * simply never becomes fireable).
     */
    public long addCallbackWithId(long reservedId, long deadlineNanos, Runnable callback) {
        if (callback == null) {
            throw new IllegalArgumentException("callback timer needs a callback");
        }
        if (reservedId < 0) {
            throw new IllegalArgumentException("callback timer needs a reserved id");
        }
        synchronized (lock) {
            Entry e = new Entry(deadlineNanos, reservedId, seqGen.getAndIncrement(),
                    null, null, callback, null, 0);
            heap.add(e);
            byId.put(reservedId, e);
            lock.notifyAll();
            return reservedId;
        }
    }

    /**
     * Arms a channel timer: at {@code deadlineNanos}, delivers the fire
     * nanotime on {@code chan} (non-blocking; drop if full).
     * {@code owner} is the GoTimer (also the re-arm/staleness callback).
     * {@code periodNanos} is the ticker period (0 for one-shot).
     *
     * <p>P1-1: the owner's id is assigned under heap→owner locks
     * <em>before</em> the entry becomes visible in the heap, so the
     * timer thread can never observe a partially-constructed owner.
     */
    void addChan(long deadlineNanos, GoChan chan, GoTimer owner,
                 long periodNanos, long generation) {
        if (chan == null) {
            throw new IllegalArgumentException("channel timer needs a channel");
        }
        if (owner == null) {
            throw new IllegalArgumentException("channel timer needs an owner");
        }
        synchronized (lock) {
            synchronized (owner) {
                long id = idGen.getAndIncrement();
                owner.setIdLocked(id);
                Entry e = new Entry(deadlineNanos, id, seqGen.getAndIncrement(),
                        null, chan, null, owner, generation);
                e.periodNanos = periodNanos;
                heap.add(e);
                byId.put(id, e);
                lock.notifyAll();
            }
        }
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
     * active (gc Timer.Stop parity).
     *
     * <p>Go 1.23+ parity (gc {@code timer.stop} + {@code timerchandrain}):
     * the channel is drained <em>unconditionally</em>; a drained stale
     * value counts as active (returns true). This covers the live-ticker
     * case: {@code stop()} on a ticker with a buffered tick drains it
     * and returns true — a subsequent receive blocks rather than
     * observing a stale pre-stop tick. Heap lock → owner lock order.
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
                // Evaluate the drain unconditionally (no short-circuit:
                // gc drains even when the timer was active).
                boolean drained = drainLocked(owner);
                return wasActive || drained;
            }
        }
    }

    /**
     * Resets a channel timer: cancels the current entry (if any),
     * re-arms for {@code deadlineNanos}. Returns true iff the timer had
     * been active (gc Timer.Reset / {@code timer.modify} parity).
     *
     * <p>Go 1.23+ parity: the channel is drained unconditionally before
     * re-arming (gc {@code timerchandrain}); a drained stale value
     * counts as active. Heap lock → owner lock order.
     */
    boolean resetChan(GoTimer owner, long deadlineNanos) {
        synchronized (lock) {
            synchronized (owner) {
                long id = owner.currentId();
                boolean wasActive = false;
                if (id >= 0) {
                    wasActive = cancelLocked(id);
                    owner.clearIdLocked();
                }
                owner.bumpGenerationLocked();
                boolean drained = drainLocked(owner);
                // addLocked assigns the new id (P1-1: before publication).
                addLocked(deadlineNanos, owner);
                return wasActive || drained;
            }
        }
    }

    /**
     * Discards all pending values on the owner's channel (gc
     * {@code timerchandrain} parity). Caller holds heap+owner locks;
     * the channel monitor is the leaf (P0-1). Returns true iff a stale
     * value was discarded. Only the timer thread sends on this channel,
     * so any buffered value is a stale timer value.
     */
    private boolean drainLocked(GoTimer owner) {
        GoChan ch = owner.channelImpl();
        synchronized (ch) {
            boolean drained = false;
            // tryRecv is non-blocking and monitor-held; a null return
            // means the buffer is empty (no send waiters exist — only
            // the timer thread sends, via non-blocking trySend).
            while (ch.tryRecv(sched) != null) {
                drained = true;
            }
            return drained;
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
        // P1-1: id assigned before the entry is visible.
        owner.setIdLocked(id);
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
                if (e.chan != null) {
                    // P0-3/P0-4: the staleness check, the send, and the
                    // ticker re-arm are atomic w.r.t. stopChan/resetChan:
                    // the heap lock is held throughout, so no stop/reset
                    // can interleave between pop and send. This closes the
                    // stale-send window structurally (stronger than gc's
                    // seq/sendLock protocol, which exists because gc
                    // cannot hold its timer lock across a blocking send;
                    // our trySend never blocks). Lock order throughout is
                    // heap → owner → chan (leaf); trySend/unpark never
                    // block and never acquire heap/owner locks.
                    fireChanLocked(e);
                    continue;
                }
            }
            fireNonChan(e);
        }
    }

    /**
     * Fires a channel entry; caller holds the heap lock (see timerLoop).
     * P0-1: the channel monitor is held across the monitor-held
     * {@code trySend}. P2-1: no {@code !e.cancelled} check — the peek
     * loop skips cancelled heads, so a polled entry is never cancelled.
     */
    private void fireChanLocked(Entry e) {
        // P3-1: the owner==null branch is dead (addChan rejects null
        // owners); kept as a defensive guard.
        if (e.owner == null || e.owner.stillCurrent(e.id, e.generation)) {
            synchronized (e.chan) {
                e.chan.trySend(System.nanoTime(), sched);
            }
            if (e.periodNanos > 0) {
                // Ticker re-arm, atomic w.r.t. stop/reset (heap lock
                // held). gc grid alignment (runtime/time.go unlockAndRun):
                // next = when + period*(1+delay/period), skipping missed
                // ticks rather than bursting.
                long delay = Math.max(0, System.nanoTime() - e.deadlineNanos);
                e.deadlineNanos += e.periodNanos * (1 + delay / e.periodNanos);
                if (e.deadlineNanos < 0) {
                    e.deadlineNanos = Long.MAX_VALUE; // overflow guard
                }
                heap.add(e);
                byId.put(e.id, e);
                lock.notifyAll();
            }
        }
    }

    /**
     * Fires a sleep/callback entry, outside the heap lock. Raw callbacks
     * run on the timer thread (gc timer-proc parity — must not block).
     */
    private void fireNonChan(Entry e) {
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
        // SLEEP: wake the parked waiter (unpark-before-parked is safe).
        sched.unpark(e.handle);
    }
}
