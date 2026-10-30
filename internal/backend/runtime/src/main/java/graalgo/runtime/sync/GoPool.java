package graalgo.runtime.sync;

import java.lang.ref.WeakReference;
import java.util.ArrayDeque;
import java.util.Deque;
import java.util.IdentityHashMap;
import java.util.Iterator;
import java.util.Map;
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
 *       because Ps are immortal; our threads are not. Locals of dead
 *       threads do NOT stay registered: the registry maps each
 *       {@code Local} to a weak reference to its owner thread, and the
 *       slow path (plus every new registration) evicts entries whose
 *       owner has terminated, draining their items into the bounded
 *       global queue first — so dead threads' items remain stealable
 *       while the registry itself stays bounded by the number of live
 *       threads that touch the pool, not by every thread that ever did.
 *       Goroutines are virtual threads and therefore unbounded, so a
 *       strong never-evicted registry would grow (and pin pooled items)
 *       without bound on a long-lived program with a global Pool.</li>
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
         * under the pool lock; the benign read race is idempotent.
         * Cleared when the entry is evicted, so a Local whose owner
         * somehow outlives its eviction can re-register. */
        boolean registered;
    }

    private final ThreadLocal<Local> locals = ThreadLocal.withInitial(Local::new);

    /** Pool-lock-guarded shared state. */
    private final Object lock = new Object();
    /**
     * Per-thread Locals currently registered with this pool (gc's
     * allPools), each mapped to a weak reference to its owner thread.
     * The Local itself is held strongly — that is what makes a dead
     * owner's items drainable rather than silently lost — but the entry
     * is evicted as soon as its owner thread has terminated (see
     * {@link #purgeDeadLocalsLocked}), so the registry is bounded by
     * live threads, not by every thread that ever touched the pool.
     * Identity semantics: two threads never share a Local, and a Local
     * is never re-keyed. Guarded by {@link #lock} for all mutation and
     * iteration; the steal scan already holds the lock.
     */
    private final IdentityHashMap<Local, WeakReference<Thread>> allLocals =
            new IdentityHashMap<>();
    private final Deque<Object> globalShared = new ArrayDeque<>();
    private final Deque<WeakReference<Object>> victim = new ArrayDeque<>();

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
                // Registration is the bound on registry growth for
                // put-only workloads (which never take the get slow
                // path): every new registrant first evicts the
                // terminated owners, so the registry holds at most the
                // live threads plus threads that died since the last
                // registration or slow-path get. The scan is once per
                // thread lifetime, not per operation.
                purgeDeadLocalsLocked();
                allLocals.put(l, new WeakReference<>(Thread.currentThread()));
                l.registered = true;
            }
        }
        return l;
    }

    /**
     * Appends x to the bounded global queue, spilling the oldest items
     * into the weakly-held victim while the queue is over cap. Caller
     * must hold {@link #lock}; used by both the put spill path and the
     * dead-Local drain, so the two paths cannot drift apart on the cap.
     */
    private void offerGlobalLocked(Object x) {
        globalShared.addLast(x);
        while (globalShared.size() > GLOBAL_SHARED_CAP) {
            victim.addLast(new WeakReference<>(globalShared.pollFirst()));
        }
    }

    /**
     * Evicts every registry entry whose owner thread has terminated
     * (weak reference cleared, or {@code isAlive()} false — the latter
     * covers a terminated thread whose Thread object is still strongly
     * reachable elsewhere, so eviction is deterministic and does not
     * wait for a GC cycle). Each evicted Local's items are drained into
     * the bounded global queue first — shared queue in FIFO order, then
     * the private slot, matching the steal scan's order — so a dead
     * thread's items stay retrievable by Get (the existing
     * cross-thread-steal contract) while the strongly-held total stays
     * bounded by live Locals plus the global cap: nothing a dead thread
     * held remains pinned in a dead Local. Caller must hold
     * {@link #lock}: the drain races no live owner (the owner is dead,
     * so no lock-free put can land mid-drain) and is mutually exclusive
     * with every thief's steal scan, which holds the same lock.
     */
    private void purgeDeadLocalsLocked() {
        Iterator<Map.Entry<Local, WeakReference<Thread>>> it =
                allLocals.entrySet().iterator();
        while (it.hasNext()) {
            Map.Entry<Local, WeakReference<Thread>> e = it.next();
            Thread owner = e.getValue().get();
            if (owner != null && owner.isAlive()) {
                continue;
            }
            Local dead = e.getKey();
            Object x;
            while ((x = dead.shared.pollFirst()) != null) {
                offerGlobalLocked(x);
            }
            x = dead.privateSlot.getAndSet(null);
            if (x != null) {
                offerGlobalLocked(x);
            }
            dead.registered = false;
            it.remove();
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
            int n = l.shared.size() / 2;
            for (int i = 0; i < n; i++) {
                offerGlobalLocked(l.shared.pollFirst());
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
            // Evict terminated owners first: their items land in the
            // global queue below, so the steal scan that follows sees
            // only live threads, and the registry never accumulates
            // dead entries between registrations.
            purgeDeadLocalsLocked();
            // Slow path (gc's getSlow): steal from other threads'
            // shared queues, then their private slots (JVM adaptation —
            // see the privateSlot field doc), then the global spillover,
            // then the victim.
            for (Local o : allLocals.keySet()) {
                if (o == l) {
                    continue;
                }
                x = o.shared.pollFirst();
                if (x != null) {
                    return x;
                }
            }
            for (Local o : allLocals.keySet()) {
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
