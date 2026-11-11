package graalgo.runtime.sync;

import java.lang.ref.Cleaner;
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
 *       because Ps are immortal; our threads are not. Locals of dead
 *       threads are deregistered when the owner thread becomes unreachable
 *       (a Cleaner action drains their items into the weakly-held victim
 *       — gc's poolCleanup primary-to-victim rotation), so dead threads'
 *       items remain retrievable until the JVM GC reclaims the victim refs.
 *       The registry is therefore bounded by the live threads touching
 *       the pool, like gc's per-P bound.</li>
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
    }

    private final ThreadLocal<Local> locals = ThreadLocal.withInitial(Local::new);

    /** Pool-lock-guarded shared state. */
    private final Object lock = new Object();
    /**
     * Per-thread Locals of live threads using this pool (gc's allPools).
     * A Local leaves the set when its owner thread becomes unreachable
     * (see {@link ReclaimAction}); it is never removed while its owner
     * can still issue pool operations.
     */
    private final Set<Local> allLocals =
            Collections.newSetFromMap(new IdentityHashMap<>());
    private final Deque<Object> globalShared = new ArrayDeque<>();
    private final Deque<WeakReference<Object>> victim = new ArrayDeque<>();

    /** The New field ({@code func() any} as a GoFunc, or null). */
    private volatile Object newFunc;

    /**
     * Own Cleaner for pool-local reclamation (one parked daemon thread
     * total, shared by all pools). Deliberately NOT the shared
     * {@code GcInterop} Cleaner: pool cleanup must never queue behind
     * arbitrary user finalizers.
     */
    private static final Cleaner POOL_CLEANER = Cleaner.create();

    /**
     * Deregisters one thread's Local when its owner thread becomes
     * phantom-reachable, draining the Local's items into the weakly-held
     * victim generation (gc's poolCleanup primary-to-victim rotation, so
     * dead threads' items stay retrievable). Static, and holding the pool
     * only weakly: capturing the owner Thread would keep the phantom from
     * ever clearing, and holding the pool strongly would keep every pool
     * alive as long as any thread that ever touched it lives — trading
     * the thread-lifetime leak for a pool-lifetime leak.
     *
     * <p>Thread-safety: the action runs on the Cleaner thread and takes
     * {@code pool.lock} — the same lock guarding every mutation and every
     * iteration of {@code allLocals} — so a Local can never vanish
     * mid-iteration, and the {@code remove()} guard makes the action
     * exactly-once. The dead owner issues no further pool operations, so
     * no race with the owner is possible.
     */
    private static final class ReclaimAction implements Runnable {
        private final WeakReference<GoPool> poolRef;
        private final Local local;

        ReclaimAction(GoPool pool, Local local) {
            this.poolRef = new WeakReference<>(pool);
            this.local = local;
        }

        @Override
        public void run() {
            GoPool pool = poolRef.get();
            if (pool == null) {
                return; // pool already collected; nothing to deregister from
            }
            synchronized (pool.lock) {
                if (!pool.allLocals.remove(local)) {
                    return; // already deregistered; exactly-once guard
                }
                Object p = local.privateSlot.getAndSet(null);
                if (p != null) {
                    pool.victim.addLast(new WeakReference<>(p));
                }
                Object s;
                while ((s = local.shared.pollFirst()) != null) {
                    pool.victim.addLast(new WeakReference<>(s));
                }
            }
        }
    }

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
                allLocals.add(l);
                l.registered = true;
            }
            // Reclaim the Local when its owner thread becomes unreachable:
            // without this, allLocals pins one Local (plus up to 33 pooled
            // objects) per thread ever, for the pool's lifetime (A-P1-1).
            // The action holds the pool only weakly, so pool lifetime stays
            // independent of thread lifetime.
            POOL_CLEANER.register(Thread.currentThread(), new ReclaimAction(this, l));
        }
        return l;
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
                globalShared.addLast(l.shared.pollFirst());
            }
            while (globalShared.size() > GLOBAL_SHARED_CAP) {
                victim.addLast(new WeakReference<>(globalShared.pollFirst()));
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

    /**
     * Number of Locals currently registered with this pool (test-only
     * observability for the A-P1-1 reclamation probe; does not itself
     * register the calling thread).
     */
    int registeredLocalCount() {
        synchronized (lock) {
            return allLocals.size();
        }
    }

    /**
     * The calling thread's Local, as an Object (test-only identity handle
     * for the A-P1-1 reclamation probe).
     */
    Object currentLocalForTest() {
        return locals.get();
    }
}
