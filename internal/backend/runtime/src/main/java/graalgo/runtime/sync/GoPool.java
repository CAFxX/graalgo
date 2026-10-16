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
 *       threads are purged from the registry — with their items migrated
 *       to the global queue, so they stay stealable — on registration and
 *       on the {@code get()} slow path (P1-8); the registry is therefore
 *       bounded by live threads, not by every goroutine that ever touched
 *       the pool.</li>
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
 *
 * <p>P1-8 design note — why purge-on-get, not a weak-keyed registry:
 * <ul>
 *   <li>A weak-keyed {@code allLocals} would clean up only when the JVM
 *       GC happens to collect dead {@code Local}s: the bound would be
 *       GC-timing-dependent and untestable, and an entry could vanish
 *       between a steal scan's iterations.</li>
 *   <li>Purge-before-scan <em>without</em> migration would strand dead
 *       threads' items, breaking steal-from-dead-thread parity
 *       ({@code GoPoolTest.testCrossThreadSteal}).</li>
 * </ul>
 * The chosen design purges under the pool lock on registration and on
 * the {@code get()} slow path — both already O(registered) — checking
 * liveness via {@code Thread.isAlive()} (exact at purge time, no GC
 * dependence) and migrating items to the global queue. The fast paths
 * are untouched: no extra branches, no allocation.
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
        /**
         * Owning thread, weakly held (P1-8): a strong reference would pin
         * a dead virtual thread — stack frames included — in the
         * registry once the owner dies. Liveness
         * ({@code Thread.isAlive()}) drives {@link GoPool#purgeDeadLocals}.
         */
        final WeakReference<Thread> owner;
        /** Whether this Local is in {@link GoPool#allLocals}. Written
         * under the pool lock; the benign read race is idempotent. */
        boolean registered;

        Local(Thread owner) {
            this.owner = new WeakReference<>(owner);
        }
    }

    private final ThreadLocal<Local> locals =
            ThreadLocal.withInitial(() -> new Local(Thread.currentThread()));

    /** Pool-lock-guarded shared state. */
    private final Object lock = new Object();
    /** All per-thread Locals ever used with this pool (gc's allPools). */
    private final Set<Local> allLocals =
            Collections.newSetFromMap(new IdentityHashMap<>());
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
     * Registry size; white-box leak tests only (P1-8).
     */
    int registeredLocalCountForTest() {
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
                // P1-8: registration is also a purge point, so put-only
                // workloads stay bounded too.
                purgeDeadLocals();
            }
        }
        return l;
    }

    /**
     * Drops registry entries whose owner thread is dead (P1-8),
     * migrating their items to the global queue so nothing stealable is
     * lost ({@code sync.Pool} allows items to vanish at any time; we
     * preserve them — strictly more than the contract requires, and it
     * keeps steal-from-dead-thread parity with gc's {@code getSlow}).
     *
     * <p>Must be called under {@link #lock}. Runs on registration and on
     * the {@code get()} slow path, so the registry holds at most the
     * live threads' entries instead of growing one entry per goroutine
     * that ever touched the pool.
     */
    private void purgeDeadLocals() {
        var it = allLocals.iterator();
        while (it.hasNext()) {
            Local o = it.next();
            Thread t = o.owner.get();
            if (t == null || !t.isAlive()) {
                Object p = o.privateSlot.getAndSet(null);
                if (p != null) {
                    globalShared.addLast(p);
                }
                Object s;
                while ((s = o.shared.pollFirst()) != null) {
                    globalShared.addLast(s);
                }
                while (globalShared.size() > GLOBAL_SHARED_CAP) {
                    victim.addLast(new WeakReference<>(globalShared.pollFirst()));
                }
                it.remove();
            }
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
            // P1-8: purge dead threads' Locals (migrating their items to
            // the global queue above) before scanning, so the registry —
            // and this scan — stay bounded by live threads.
            purgeDeadLocals();
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
