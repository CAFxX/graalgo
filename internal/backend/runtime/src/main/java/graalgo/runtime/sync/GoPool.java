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
 *       because Ps are immortal; our threads are not. Locals of dead
 *       threads stay registered while they still hold items — their items
 *       remain stealable — but a dead thread's <em>empty</em> Local is
 *       pruned by the slow path (see {@link #purgeDeadEmptyLocals}):
 *       goroutines are one virtual thread each, so without pruning the
 *       registry would grow with every goroutine that ever touched the
 *       pool and every slow-path {@code get} would scan it all. The
 *       purge is amortized (doubling high-water, not per-call), so the
 *       slow-path scan stays proportional to live threads plus Locals
 *       still holding stealable items, never to threads-ever.</li>
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

    /**
     * High-water mark for the {@link #allLocals} registry: the slow path
     * purges dead empty Locals only once the registry exceeds this size,
     * so small pools (the common case) pay no purge cost at all.
     */
    private static final int PURGE_HIGH_WATER = 128;

    /** Per-thread pool (gc's {@code poolLocal}). */
    private static final class Local {
        /**
         * The thread this Local belongs to, weakly held: a strong
         * reference here would pin every dead goroutine's thread object
         * (and, through it, its stack/frame state) for the pool's
         * lifetime — the very retention the purge exists to avoid. The
         * referent of a terminated thread is either already collected
         * (reference cleared) or reports {@code !isAlive()}; a live
         * thread's object is strongly reachable from the VM, so its
         * weak reference never clears spuriously while the thread runs.
         * Captured in the constructor, which the {@code ThreadLocal}
         * initialiser runs on the owning thread.
         */
        final WeakReference<Thread> owner =
                new WeakReference<>(Thread.currentThread());
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
    /** All per-thread Locals ever used with this pool (gc's allPools). */
    private final Set<Local> allLocals =
            Collections.newSetFromMap(new IdentityHashMap<>());
    /**
     * Registry size just after the last purge (pool-lock-guarded). The
     * next purge runs only once the registry has at least doubled since
     * then, so a purge's O(n) scan is amortized O(1) per registration
     * instead of recurring on every slow-path {@code get} while dead
     * non-empty Locals keep the size above the high-water mark.
     */
    private int purgeBaseline;
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

    /**
     * Removes Locals whose owner thread is dead <em>and</em> which hold
     * no items. Callers must hold {@link #lock}. Dead Locals that still
     * hold items are deliberately kept: their items are stealable pool
     * content (see the class javadoc), and they become purgeable as soon
     * as a thief drains them. A Local is never removed while its owner
     * is alive — the owner reuses it through its {@code ThreadLocal} —
     * and a purged Local can never be re-registered, because its owner
     * is dead and its {@code ThreadLocal} entry dies with the thread.
     */
    private void purgeDeadEmptyLocals() {
        allLocals.removeIf(o -> {
            Thread t = o.owner.get();
            if (t != null && t.isAlive()) {
                return false;
            }
            return o.privateSlot.get() == null && o.shared.isEmpty();
        });
        purgeBaseline = allLocals.size();
    }

    /** Number of Locals currently registered (tests only). */
    int registeredLocalCount() {
        synchronized (lock) {
            return allLocals.size();
        }
    }

    /** The calling thread's Local, registered on first use. */
    private Local local() {
        Local l = locals.get();
        if (!l.registered) {
            synchronized (lock) {
                allLocals.add(l);
                l.registered = true;
            }
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
            // Amortized purge (review-pool-io): only once the registry
            // is large and has at least doubled since the last purge,
            // so the hot slow path below never pays a purge scan per call
            // and never scans dead empty Locals once purged. The first
            // purge (baseline 0) runs as soon as the high-water mark is
            // crossed.
            if (allLocals.size() > PURGE_HIGH_WATER
                    && allLocals.size() >= 2 * purgeBaseline) {
                purgeDeadEmptyLocals();
            }
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
