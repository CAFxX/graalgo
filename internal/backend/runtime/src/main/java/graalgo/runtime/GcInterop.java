package graalgo.runtime;

import graalgo.values.GoFunc;

import java.lang.management.GarbageCollectorMXBean;
import java.lang.management.ManagementFactory;
import java.lang.management.MemoryMXBean;
import java.lang.management.MemoryUsage;
import java.lang.ref.Reference;
import java.lang.ref.WeakReference;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.WeakHashMap;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;

import javax.management.Notification;
import javax.management.NotificationEmitter;
import javax.management.NotificationListener;
import javax.management.openmbean.CompositeData;

import com.sun.management.GarbageCollectionNotificationInfo;

/**
 * Go-observable GC surface (P2-M4, ADR-0002 D4).
 *
 * <p>Shadows the Phase-1 stub of the same fully-qualified name (phase-2
 * classes first on the classpath — the documented overlay mechanism).
 *
 * <p>Design (ADR-0002): all Go objects live in the JVM heap and the JVM GC
 * <em>is</em> the Go GC (D1); roots are the JVM's, exact by construction
 * (D2); no Truffle-level write barrier — JVM barriers cover every store
 * (D3). This class implements only the <em>observable</em> surface:
 *
 * <ul>
 *   <li>{@code runtime.SetFinalizer} — single shared
 *       {@link java.lang.ref.Cleaner} (one dedicated thread ⇒ serialized,
 *       like gc's finalizer goroutine). <b>Accepted divergence:</b> the
 *       finalizer is invoked with a {@code nil} argument — a Cleaner action
 *       is denied the referent and the original object cannot be recovered
 *       on a pure JVM. Finalizers that dereference their argument diverge
 *       from gc (loudly, not silently). The finalizer body runs on a
 *       scheduler goroutine when one is installed (L2: so it may park,
 *       and so an uncaught panic is recorded as a Go panic, exit 2,
 *       exactly like any other goroutine's panic); with no scheduler
 *       installed (unit tests, Phase-1 runs) it runs on the Cleaner
 *       thread with the old loud-print fallback.
 *   <li>{@code runtime.KeepAlive} — {@link java.lang.ref.Reference#reachabilityFence}.
 *   <li>{@code runtime.GC()} — {@code System.gc()} + poll of GC MXBean
 *       collection counts until they advance, bounded 30 s timeout. If the
 *       counts never advance (explicit GC disabled), returns anyway —
 *       <b>documented approximation:</b> callers must not depend on
 *       reclamation having completed.
 *   <li>{@code weak.Pointer} vehicle — {@link WeakReference} wrapper.
 *   <li>{@code runtime.ReadMemStats} — {@link #memStats()} field table;
 *       unmappable fields are absent (see {@code DIVERGENCES.md}).
 * </ul>
 *
 * <p>verifies: WS D (ADR-0002 D4)
 */
public final class GcInterop {
    private GcInterop() {}

    // ------------------------------------------------------------------
    // runtime.SetFinalizer
    // ------------------------------------------------------------------

    /**
     * Single shared Cleaner (ADR-0002 D4). One Cleaner ⇒ one dedicated
     * finalizer thread ⇒ all finalizers serialized, like gc's finalizer
     * goroutine. Actions are phantoms: denied the referent by construction,
     * hence the nil finalizer argument (DIV-M4-1).
     */
    private static final java.lang.ref.Cleaner CLEANER = java.lang.ref.Cleaner.create();
    /**
     * Obj -> registration, for {@code SetFinalizer(obj, nil)} removal.
     *
     * <p>Weak keys so the registry never keeps a finalizable object alive
     * (that would defeat the phantom). L2: there is deliberately NO
     * strong pin set here. An earlier design kept every registered
     * GoPointer strongly reachable until the program called
     * {@code runtime.GC()} (or cancelled), which meant a finalizer
     * could never fire without an explicit GC call — and the retained
     * objects leaked forever in programs that never call it (the
     * documented DIV-M4-10 divergence). The pin now lasts only for
     * the register/cancel window itself: {@code setFinalizer} holds
     * {@code obj} as a parameter for the whole call and ends with a
     * {@link java.lang.ref.Reference#reachabilityFence}, so the object
     * cannot be collected mid-registration or mid-cancellation.
     * After the call returns, lifetime is natural (tied to Go
     * variables, exactly like gc): a Go program that needs the object
     * alive past its last use must use {@code runtime.KeepAlive},
     * like gc. GoPointer uses identity equality, so weak-key semantics
     * are identity semantics here. Guarded by {@link #FINALIZER_LOCK}.
     */
    private static final Object FINALIZER_LOCK = new Object();
    private static final Map<Object, FinalizerAction> BY_OBJ =
            new WeakHashMap<>();

    /**
     * A cancellable finalizer action. The Cleaner API has no "unregister"
     * (Cleanable.clean() *runs* the action); cancellation is via flag.
     */
    private static final class FinalizerAction implements Runnable {
        final GoFunc fn;
        final int objHash; // for debug
        volatile boolean cancelled;
        FinalizerAction(GoFunc fn, int objHash) {
            this.fn = fn;
            this.objHash = objHash;
        }
        @Override
        public void run() {
            if (cancelled) {
                return;
            }
            // L2: run the body on a scheduler goroutine, not the
            // Cleaner thread. On the Cleaner thread a finalizer body
            // could not park (scheduler state fail-fasts off-goroutine)
            // and a panic had nowhere to go (only a stderr print).
            // On a goroutine, SchedulerCore.spawn records an uncaught
            // Go panic exactly like any other goroutine's (printed
            // traceback, launcher exit 2) and a Goexit kills just that
            // goroutine — gc's finalizer-goroutine semantics. The
            // spawn is fire-and-forget: the Cleaner thread must not
            // block on the goroutine (a finalizer that parks would
            // deadlock the Cleaner), and quiescence accounting (the
            // spawned goroutine counts as live until it finishes) means
            // a launcher still in awaitQuiescence waits for it. (A
            // finalizer that fires after the launcher has already passed
            // quiescence still races process exit, as in gc — no
            // finalizer is guaranteed to run before exit.)
            graalgo.sched.SchedulerCore sched =
                    graalgo.sched.SchedulerCore.installedOrNull();
            if (sched != null && !sched.isStopped()) {
                try {
                    sched.spawn(() -> GoCalls.invoke(fn, new Object[] { null }));
                    return;
                } catch (RuntimeException e) {
                    // Scheduler gone mid-dispatch (stopped concurrently):
                    // fall through to the direct fallback below rather
                    // than dropping the finalizer silently.
                }
            }
            try {
                GoCalls.invoke(fn, new Object[] { null });
            } catch (Throwable t) {
                // No scheduler (unit tests / Phase-1 runs): the old
                // loud-print fallback. A panicking finalizer here cannot
                // crash the host from the Cleaner thread; be loud.
                System.err.println("graalgo: finalizer panicked: " + t);
                t.printStackTrace(System.err);
            }
        }
    }

    /**
     * {@code runtime.SetFinalizer(obj, finalizer)}.
     *
     * @param obj must be a Go pointer ({@code *T}); nil clears.
     * @param finalizer a {@link GoFunc} taking the pointer, or null to remove.
     */
    public static void setFinalizer(Object obj, Object finalizer) {
        if (obj == null) {
            throw new IllegalArgumentException(
                    "runtime.SetFinalizer: nil object");
        }
        if (!(obj instanceof graalgo.values.GoPointer)) {
            throw new IllegalArgumentException(
                    "runtime.SetFinalizer: first argument must be a pointer");
        }
        final GoFunc fn;
        if (finalizer == null) {
            fn = null; // removal
        } else if (finalizer instanceof GoFunc g) {
            fn = g;
        } else {
            throw new IllegalArgumentException(
                    "runtime.SetFinalizer: finalizer must be a function");
        }
        synchronized (FINALIZER_LOCK) {
            FinalizerAction old = BY_OBJ.remove(obj);
            if (old != null) {
                if (fn != null) {
                    // gc fatals "finalizer already set" — setting twice
                    // without an intervening nil is a programming bug.
                    // Restore the registration before throwing.
                    BY_OBJ.put(obj, old);
                    throw new IllegalStateException(
                            "runtime.SetFinalizer: finalizer already set");
                }
                old.cancelled = true; // cancel; the Cleaner action will no-op
            }
            if (fn == null) {
                return;
            }
            // The action must not capture obj (else the phantom never clears).
            // fn is the Go closure; the nil argument is DIV-M4-1.
            FinalizerAction action = new FinalizerAction(fn, System.identityHashCode(obj));
            CLEANER.register(obj, action);
            BY_OBJ.put(obj, action);
            // Fence: keep obj alive until the registration is complete.
            // This fence (plus the parameter itself) is the whole pin:
            // it lasts for this call only (see BY_OBJ javadoc).
            // Without this, the JIT may consider obj dead after its last use
            // above and the Cleaner may fire prematurely.
            java.lang.ref.Reference.reachabilityFence(obj);
        }
    }

    // ------------------------------------------------------------------
    // runtime.KeepAlive
    // ------------------------------------------------------------------

    /**
     * {@code runtime.KeepAlive(x)} — keeps {@code x} reachable at least until
     * this call. A plain no-op intrinsic would not suffice: after inlining,
     * the JIT may consider {@code x} dead at any point past its last use,
     * which is precisely why {@link java.lang.ref.Reference#reachabilityFence}
     * exists (ADR-0002 D4, reviewer P1-2).
     */
    public static void keepAlive(Object x) {
        java.lang.ref.Reference.reachabilityFence(x);
    }

    // ------------------------------------------------------------------
    // runtime.GC
    // ------------------------------------------------------------------

    /** Number of explicit {@code runtime.GC()} calls (for memstats). */
    private static final AtomicLong FORCED_GC_COUNT = new AtomicLong();

    private static long totalCollections() {
        long total = 0;
        boolean any = false;
        for (GarbageCollectorMXBean b : ManagementFactory.getGarbageCollectorMXBeans()) {
            long c = b.getCollectionCount();
            if (c >= 0) {
                total += c;
                any = true;
            }
        }
        return any ? total : -1;
    }

    /**
     * {@code runtime.GC()} — runs a garbage collection and blocks the caller
     * until a collection cycle has completed, mirroring gc's contract as
     * closely as the JVM allows: {@code System.gc()} followed by a poll of
     * GC MXBean collection counts until they advance, with a bounded 30 s
     * timeout. If the counts never advance (explicit GC disabled), returns
     * anyway — <b>documented approximation:</b> callers must not depend on
     * reclamation having completed (ADR-0002 D4).
     */
    public static void gc() {
        FORCED_GC_COUNT.incrementAndGet();
        // L2: no pin set to release anymore — lifetimes are natural
        // from registration on (see BY_OBJ javadoc), so an explicit GC
        // is a collection request only, not a finalizer gate.
        long before = totalCollections();
        System.gc();
        if (before < 0) {
            return; // no countable collectors; best effort only
        }
        long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(30);
        while (System.nanoTime() < deadline) {
            if (totalCollections() > before) {
                return;
            }
            try {
                Thread.sleep(50);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                return;
            }
        }
        // Timeout: explicit GC is likely disabled. Documented approximation —
        // return rather than hang (see class javadoc).
    }

    // ------------------------------------------------------------------
    // weak.Pointer vehicle
    // ------------------------------------------------------------------

    /**
     * {@code weak.Make} vehicle: wraps {@code obj} in a {@link WeakReference}.
     * Null in → null out (Go: {@code weak.Make} of nil is nil).
     */
    public static WeakReference<Object> newWeakRef(Object obj) {
        return obj == null ? null : new WeakReference<>(obj);
    }

    /**
     * {@code weak.Pointer.Value} vehicle: the referent, or null if collected
     * (or the reference itself is null).
     */
    public static Object weakRefValue(WeakReference<Object> ref) {
        return ref == null ? null : ref.get();
    }

    // ------------------------------------------------------------------
    // runtime.ReadMemStats vehicle
    // ------------------------------------------------------------------

    /**
     * Last-256 GC pause durations in nanoseconds, fed by MXBean notifications.
     * (Go's {@code MemStats.PauseNs[256]} analog; reviewer P1-3/P1-4.)
     */
    private static final long[] PAUSE_RING = new long[256];
    private static int pauseRingNext;
    private static final Object PAUSE_LOCK = new Object();

    static {
        NotificationListener gcListener = (Notification notification, Object handback) -> {
            if (!com.sun.management.GarbageCollectionNotificationInfo.GARBAGE_COLLECTION_NOTIFICATION
                    .equals(notification.getType())) {
                return;
            }
            CompositeData cd = (CompositeData) notification.getUserData();
            com.sun.management.GarbageCollectionNotificationInfo info =
                    com.sun.management.GarbageCollectionNotificationInfo.from(cd);
            long durMs = info.getGcInfo().getDuration();
            synchronized (PAUSE_LOCK) {
                PAUSE_RING[pauseRingNext] = durMs * 1_000_000L;
                pauseRingNext = (pauseRingNext + 1) % PAUSE_RING.length;
            }
        };
        for (GarbageCollectorMXBean b : ManagementFactory.getGarbageCollectorMXBeans()) {
            if (b instanceof NotificationEmitter emitter) {
                try {
                    emitter.addNotificationListener(gcListener, null, null);
                } catch (Exception e) {
                    // Best effort: without notifications PauseNs stays zero.
                }
            }
        }
    }

    /**
     * Field-by-field Go {@code runtime.MemStats} mapping (ADR-0002 D4).
     *
     * <p>Keyed by Go field name. Three classes:
     * <ul>
     *   <li><b>exact/measured:</b> HeapAlloc, HeapSys, Sys, NumGC,
     *       PauseTotalNs, NumForcedGC, PauseNs (last-256 ring, ns);
     *   <li><b>absent (no MXBean analog):</b> BySize, HeapObjects,
     *       TotalAlloc, Mallocs, Frees, HeapIdle, HeapInuse, HeapReleased,
     *       StackInuse, StackSys, MSpanInuse, MSpanSys, MCacheInuse,
     *       MCacheSys, BuckHashSys, GCSys, OtherSys, NextGC, LastGC,
     *       GCCPUFraction — recorded in {@code DIVERGENCES.md}.
     * </ul>
     */
    public static Map<String, Long> memStats() {
        Map<String, Long> m = new LinkedHashMap<>();
        MemoryMXBean mem = ManagementFactory.getMemoryMXBean();
        MemoryUsage heap = mem.getHeapMemoryUsage();
        MemoryUsage nonHeap = mem.getNonHeapMemoryUsage();
        m.put("HeapAlloc", heap.getUsed());
        m.put("HeapSys", heap.getCommitted());
        m.put("Sys", heap.getCommitted() + nonHeap.getCommitted());
        long numGC = 0;
        long pauseTotalNs = 0;
        for (GarbageCollectorMXBean b : ManagementFactory.getGarbageCollectorMXBeans()) {
            long c = b.getCollectionCount();
            if (c >= 0) {
                numGC += c;
            }
            long t = b.getCollectionTime();
            if (t >= 0) {
                pauseTotalNs += t * 1_000_000L;
            }
        }
        m.put("NumGC", numGC);
        m.put("PauseTotalNs", pauseTotalNs);
        m.put("NumForcedGC", FORCED_GC_COUNT.get());
        long lastPauseNs;
        synchronized (PAUSE_LOCK) {
            lastPauseNs = PAUSE_RING[(pauseRingNext + PAUSE_RING.length - 1) % PAUSE_RING.length];
        }
        m.put("PauseNsLast", lastPauseNs);
        return m;
    }

    /**
     * Returns the last-256 GC pause durations in nanoseconds, oldest first
     * (Go {@code MemStats.PauseNs} analog; zeros where no pause recorded yet).
     */
    public static long[] pauseNsRing() {
        long[] out = new long[PAUSE_RING.length];
        synchronized (PAUSE_LOCK) {
            for (int i = 0; i < out.length; i++) {
                out[i] = PAUSE_RING[(pauseRingNext + i) % PAUSE_RING.length];
            }
        }
        return out;
    }
}
