package graalgo.runtime.sync;

import java.lang.ref.WeakReference;
import java.util.ArrayDeque;
import java.util.Collections;
import java.util.Deque;
import java.util.IdentityHashMap;
import java.util.Set;
import java.util.concurrent.ConcurrentLinkedDeque;
import java.util.concurrent.atomic.AtomicReference;

/**
 * {@code sync.Pool} (graalgo): per-thread private + shared pools, cross-
 * thread stealing, a global spillover, and a weakly-held victim
 * generation.
 *
 * <p>gc mapping (see {@code sync/pool.go} in go1.27.1):
 * <ul>
 *   <li>Each JVM thread is a "P": the per-P {@code poolLocal} becomes a
 *       {@link ThreadLocal} {@code Local} with a 1-item {@code private}
 *       slot and a bounded {@code shared} queue. The fast paths
 *       ({@link #put} filling private/shared, {@link #get} draining them)
 *       are lock-free — each {@code Local} is thread-confined on its
 *       fast path, exactly like gc's per-P pools under {@code pin()}.</li>
 *   <li>Cross-thread sharing (gc's {@code getSlow} stealing from other
 *       Ps' shared queues via the {@code allPools} registry) scans the
 *       registered {@code Local}s' shared queues under the pool lock,
 *       then their {@code private} slots. The shared queues are
 *       {@link ConcurrentLinkedDeque}s and the private slot an
 *       {@link java.util.concurrent.atomic.AtomicReference}, so an owner
 *       racing a thief is safe. Stealing {@code private} is a deliberate
 *       JVM adaptation (see the field doc): gc's {@code getSlow} skips it
 *       because Ps are immortal; our threads are not, and under this
 *       scheduler every goroutine is its own short-lived vthread that
 *       terminates. Locals of dead threads are pruned (amortized, on the
 *       pool-lock paths): their items are moved to the global queue —
 *       remaining stealable, exactly as before — and the Local is dropped
 *       from the registry. The registry is therefore bounded by the
 *       threads currently alive plus a small pruning lag, not by every
 *       thread that ever touched the pool.</li>
 *   <li>Victim cache + GC interaction: gc rotates primary→victim at each
 *       GC stop-the-world ({@code poolCleanup}); there is no STW hook on
 *       the JVM, so the victim holds {@link WeakReference}s. The JVM GC
 *       reclaims them under memory pressure — the documented "any item
 *       stored in the Pool may be removed automatically at any time"
 *       behavior, genuinely GC-driven. Overflow from a full thread-local
 *       shared queue spills to the global queue; overflow from a full
 *       global queue spills into the victim — memory stays bounded.</li>
 * </ul>
 *
 * <p>Go semantics preserved:
 * <ul>
 *   <li>{@code Put(nil)} is a no-op (gc parity).</li>
 *   <li>{@code Get} returns an arbitrary previously-Put item, including
 *       items Put by other threads; a nil return with {@code New} set
 *       means the caller invokes {@code New} (done by the syncop node,
 *       outside any lock — gc likewise calls {@code New} without the
 *       pool lock).</li>
 *   <li>The {@code New} field ({@code func() any}) is a volatile slot;
 *       gc forbids changing it concurrently with {@code Get}, publication
 *       is still safe.</li>
 *   <li>A Pool value copied after first use shares the pools (the Go value
 *       IS this object — ADR-0002 D1); gc's noCopy is vet-level only.</li>
 * </ul>
 *
 * <p>No scheduler interaction: pool operations never block.
 */
public final class GoPool {

    /** Bound for a thread-local shared queue; beyond it, half spills to
     * the global queue under the pool lock. */
    private static final int LOCAL_SHARED_CAP = 32;
    /** Bound for the global shared queue; beyond it, the oldest items
     * spill into the weakly-held victim. */
    private static final int GLOBAL_SHARED_CAP = 256;

    /** Per-thread pool (gc's {@code poolLocal}). */
    private static final class Local {
        /**
         * 1-item fast path (gc's {@code poolLocal.private}). An
         * {@link AtomicReference} — not for contention (the owner uses it
         * lock-free) but so the slow path can steal it race-free with a
         * concurrent owner {@code put}. Deliberate JVM adaptation: gc's
         * {@code getSlow} never steals {@code private} because Ps are
         * immortal; our threads are not, and an unreachable private slot
         * would strand items no thief can see. Stealing can only make
         * {@code get} return more previously-Put items, never fewer than
         * the contract allows.
         */
        final AtomicReference<Object> privateSlot = new AtomicReference<>();
        /** Shared queue (gc's {@code poolLocalShared}). Concurrent: the
         * owner uses it lock-free on the fast path; thieves steal under
         * the pool lock. */
        final ConcurrentLinkedDeque<Object> shared = new ConcurrentLinkedDeque<>();
        /** Whether this Local is in {@link GoPool#allLocals}. Written
         * under the pool lock; the benign read race is idempotent. */
        boolean registered;
        /**
         * The owning thread, held weakly: a registered Local must never
         * keep a dead thread alive. When the owner dies its items are
         * harvested into the global queue by {@link GoPool#pruneDeadLocals}
         * and the Local is dropped from {@link GoPool#allLocals}, so the
         * registry stays bounded even when every goroutine is its own
         * short-lived vthread.
         */
        final WeakReference<Thread> owner;

        Local(Thread owner) {
            this.owner = new WeakReference<>(owner);
        }
    }

    private final ThreadLocal<Local> locals =
            ThreadLocal.withInitial(() -> new Local(Thread.currentThread()));

    /** Pool-lock-guarded shared state. */
    private final Object lock = new Object();
    /**
     * All per-thread Locals currently in use with this pool (gc's
     * allPools). Locals of dead threads are harvested (their items moved
     * to {@link #globalShared}, staying stealable) and dropped by
     * {@link #pruneDeadLocals}, so this set does NOT grow without bound:
     * it is bounded by the threads currently alive plus a small amortized
     * pruning lag.
     */
    private final Set<Local> allLocals =
            Collections.newSetFromMap(new IdentityHashMap<>());
    private final Deque<Object> globalShared = new ArrayDeque<>();
    private final Deque<WeakReference<Object>> victim = new ArrayDeque<>();

    /**
     * Lock acquisitions since the last prune pass (lock-guarded). A prune
     * pass scans all of {@link #allLocals}, so it runs only every
     * {@link #PRUNE_INTERVAL} lock acquisitions — amortized O(1) per op —
     * while still guaranteeing that dead threads' Locals are reclaimed
     * promptly (well within any workload that keeps touching the pool).
     */
    private long opsSincePrune;
    private static final long PRUNE_INTERVAL = 64;

    /** The New field ({@code func() any} as a GoFunc, or null). */
    private volatile Object newFunc;

    /** Reads the New field. */
    public Object getNewFunc() {
        return newFunc;
    }

    /** Writes the New field. */
    public void setNewFunc(Object f) {
        newFunc = f;
    }

    /** The calling thread's Local, registered on first use. */
    private Local local() {
        Local l = locals.get();
        if (!l.registered) {
            synchronized (lock) {
                maybePrune();
                allLocals.add(l);
                l.registered = true;
            }
        }
        return l;
    }

    /**
     * Amortized pruning: runs a {@link #pruneDeadLocals} pass every
     * {@link #PRUNE_INTERVAL} lock acquisitions. Must be called with the
     * pool lock held.
     */
    private void maybePrune() {
        if (++opsSincePrune < PRUNE_INTERVAL) {
            return;
        }
        opsSincePrune = 0;
        pruneDeadLocals();
    }

    /**
     * Drops Locals whose owning thread has died (a terminated thread can
     * never touch its ThreadLocal again, so removal is race-free), after
     * moving their items into the global queue — dead threads' items stay
     * stealable, exactly as before, but the registry no longer leaks one
     * entry per dead thread. Must be called with the pool lock held.
     */
    private void pruneDeadLocals() {
        var it = allLocals.iterator();
        while (it.hasNext()) {
            Local o = it.next();
            Thread t = o.owner.get();
            if (t != null && t.isAlive()) {
                continue;
            }
            Object x = o.privateSlot.getAndSet(null);
            if (x != null) {
                spillToGlobal(x);
            }
            while ((x = o.shared.pollFirst()) != null) {
                spillToGlobal(x);
            }
            o.registered = false;
            it.remove();
        }
    }

    /**
     * Moves x into the global queue, pushing the oldest global items into
     * the weakly-held victim when the global cap is exceeded — memory
     * stays bounded. Must be called with the pool lock held.
     */
    private void spillToGlobal(Object x) {
        globalShared.addLast(x);
        while (globalShared.size() > GLOBAL_SHARED_CAP) {
            victim.addLast(new WeakReference<>(globalShared.pollFirst()));
        }
    }

    /**
     * Test-visible: the number of currently registered per-thread Locals.
     */
    int registeredLocalCount() {
        synchronized (lock) {
            return allLocals.size();
        }
    }

    /**
     * Puts x into the pool (gc's {@code Pool.Put}): a nil x is a no-op.
     * Lock-free fast path on the calling thread's local pool; spills to
     * the global queue (and then the victim) when the local queue is full.
     */
    public void put(Object x) {
        if (x == null) {
            return;
        }
        Local l = local();
        if (l.privateSlot.compareAndSet(null, x)) {
            return;
        }
        if (l.shared.size() < LOCAL_SHARED_CAP) {
            l.shared.addLast(x);
            return;
        }
        synchronized (lock) {
            maybePrune();
            int n = l.shared.size() / 2;
            for (int i = 0; i < n; i++) {
                spillToGlobal(l.shared.pollFirst());
            }
        }
        l.shared.addLast(x);
    }

    /**
     * Gets an arbitrary previously-Put item (gc's {@code Pool.Get} fast +
     * slow paths), or null when the pool (including the victim) is empty.
     * The caller invokes {@code New} on a null return when it is set.
     */
    public Object get() {
        Local l = local();
        Object x = l.privateSlot.getAndSet(null);
        if (x != null) {
            return x;
        }
        x = l.shared.pollFirst();
        if (x != null) {
            return x;
        }
        synchronized (lock) {
            // Reclaim locals of dead threads first (amortized): their
            // items were moved to the global queue, staying stealable.
            maybePrune();
            // Slow path (gc's getSlow): steal from other threads'
            // shared queues, then their private slots (JVM adaptation —
            // see the privateSlot field doc), then the global spillover,
            // then the victim.
            for (Local o : allLocals) {
                if (o == l) {
                    continue;
                }
                x = o.shared.pollFirst();
                if (x != null) {
                    return x;
                }
            }
            for (Local o : allLocals) {
                if (o == l) {
                    continue;
                }
                x = o.privateSlot.getAndSet(null);
                if (x != null) {
                    return x;
                }
            }
            x = globalShared.pollFirst();
            if (x != null) {
                return x;
            }
            // Victim generation: skip entries the JVM GC reclaimed.
            while (!victim.isEmpty()) {
                Object v = victim.pollFirst().get();
                if (v != null) {
                    return v;
                }
            }
            return null;
        }
    }
}
