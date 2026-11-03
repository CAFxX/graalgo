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
 *       from gc (loudly, not silently). The finalizer runs on the Cleaner's
 *       JVM thread, not a goroutine: touching scheduler state from a
 *       finalizer body fail-fasts. <b>Not divergent:</b> a panicking
 *       finalizer terminates the program with exit status 2, like gc
 *       (REVIEW-2026-11-03-16 P1-B5); previously it only printed and the
 *       program continued. See {@link #reportFinalizerPanic}.
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
     * STRONG keys: the registration keeps the GoPointer alive until the
     * program either cancels (SetFinalizer(x, nil)) or calls runtime.GC().
     * This is necessary because the interpreter frame's reference may not
     * be a timely GC root (JIT liveness can collect the GoPointer between
     * the register and cancel calls, before the Go program nils x).
     *
     * <p>Weak keys so the registry never keeps a finalizable object alive
     * (that would defeat the phantom). The weak key is safe because
     * {@link #PINNED} keeps the GoPointer strongly reachable until the
     * program calls {@link #gc()} or cancels. GoPointer uses identity
     * equality, so weak-key semantics are identity semantics here. Guarded
     * by {@link #FINALIZER_LOCK}.
     */
    private static final Object FINALIZER_LOCK = new Object();
    private static final Map<Object, FinalizerAction> BY_OBJ =
            new WeakHashMap<>();
    /**
     * Strong references to registered GoPointers, released on {@link #gc()}.
     * Keeps the referent alive across the register/cancel window. The
     * explicit GC call is the program's signal that setup is complete; after
     * release, lifetimes are natural (tied to Go variables). Guarded by
     * {@link #FINALIZER_LOCK}.
     */
    private static final java.util.Set<Object> PINNED = new java.util.HashSet<>();

    /**
     * How a panicking finalizer terminates the program. The production
     * handler exits the JVM with status 2 (gc: a panicking finalizer
     * crashes the program); the handler is replaceable only so unit tests
     * can observe the fatal routing without killing the test JVM.
     * Production code must never replace it.
     */
    interface FinalizerFatalHandler {
        void fatal(Throwable t);
    }

    private static final FinalizerFatalHandler DEFAULT_FATAL_HANDLER = t -> {
        System.exit(2);
    };

    private static volatile FinalizerFatalHandler finalizerFatalHandler = DEFAULT_FATAL_HANDLER;

    /**
     * Test-only hook: replaces the finalizer-fatal handler. Pass null to
     * restore the production default (exit status 2). Callers must restore
     * it in a finally block; a leaked test handler would swallow a real
     * fatal finalizer panic in later tests.
     */
    static void setFinalizerFatalHandlerForTest(FinalizerFatalHandler handler) {
        finalizerFatalHandler = handler == null ? DEFAULT_FATAL_HANDLER : handler;
    }

    /**
     * Reports a panicking finalizer and terminates the program, like gc.
     *
     * <p>gc semantics: a panic escaping a finalizer is unrecoverable — the
     * runtime prints the panic and crashes with exit status 2. The old
     * graalgo behavior (print to stderr and continue) silently survived a
     * fatal condition: a program gc would kill kept running. The Cleaner
     * thread cannot propagate the panic to the main thread (there is no
     * goroutine to unwind into), so termination here is explicit:
     * diagnostic first, stderr flushed, then the fatal handler (production:
     * {@code System.exit(2)}).
     *
     * <p>Package-private so unit tests can drive the fatal routing
     * deterministically without waiting on the Cleaner.
     */
    static void reportFinalizerPanic(Throwable t) {
        System.err.println("graalgo: finalizer panicked: " + t);
        t.printStackTrace(System.err);
        System.err.println("graalgo: finalizer panicked, terminating with exit status 2 "
                + "(gc: a panicking finalizer crashes the program)");
        System.err.flush();
        finalizerFatalHandler.fatal(t);
    }

    /**
     * A cancellable finalizer action. The Cleaner API has no "unregister"
     * (Cleanable.clean() *runs* the action); cancellation is via flag.
     *
     * <p>Design note (P1-B5, "keep a strong referent until invocation"):
     * that design cannot work with a Cleaner or a ReferenceQueue — any
     * strong reference held by the finalizer machinery is a GC root, so
     * the object never becomes phantom-reachable and the finalizer never
     * fires. The only JVM mechanism that yields the referent at
     * collection time is a finalizer-guardian cycle via
     * {@code Object.finalize()}, which is deprecated for removal, has no
     * ordering guarantees, and is not a supported Truffle/Native Image
     * mechanism — rejected, not deferred silently. The nil argument
     * (DIV-M4-1) therefore stands until the value model moves off the
     * JVM heap or the JVM gains a supported referent-yielding phantom
     * mechanism; what this lane fixes is the survivable half: a finalizer
     * that dereferences its nil argument (or panics for any other reason)
     * now kills the program like gc instead of being survived.
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
            try {
                GoCalls.invoke(fn, new Object[] { null });
            } catch (Throwable t) {
                reportFinalizerPanic(t);
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
                PINNED.remove(obj);
            }
            if (fn == null) {
                return;
            }
            // The action must not capture obj (else the phantom never clears).
            // fn is the Go closure; the nil argument is DIV-M4-1.
            FinalizerAction action = new FinalizerAction(fn, System.identityHashCode(obj));
            CLEANER.register(obj, action);
            BY_OBJ.put(obj, action);
            PINNED.add(obj);
            // Fence: keep obj alive until the registration is complete.
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
        // Release pinned GoPointers: the explicit GC is the program's signal
        // that setup is complete. After this, lifetimes are natural (tied to
        // Go variables), and the Cleaner can collect them. BY_OBJ (weak) is
        // retained for cancellation lookups. (See PINNED javadoc.)
        synchronized (FINALIZER_LOCK) {
            PINNED.clear();
        }
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
