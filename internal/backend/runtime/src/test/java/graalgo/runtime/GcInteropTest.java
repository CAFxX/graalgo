package graalgo.runtime;

import java.lang.ref.WeakReference;
import java.util.Map;
import java.util.concurrent.atomic.AtomicBoolean;

import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.nodes.RootNode;

import graalgo.values.GoFunc;
import graalgo.values.GoPointer;

/**
 * P2-M4 unit tests for {@link GcInterop} (WS D; ADR-0002 D4).
 *
 * <p>verifies: WS D (GcInterop unit behavior)
 */
public final class GcInteropTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(Object expected, Object actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /** weak.Pointer vehicle: referent visible while live, null after collection. */
    public void testWeakRefLifecycle() throws Exception {
        Object target = new Object();
        WeakReference<Object> ref = GcInterop.newWeakRef(target);
        assertTrue(ref != null, "newWeakRef of non-null must be non-null");
        assertTrue(GcInterop.weakRefValue(ref) == target, "weak value must be the referent");
        assertTrue(GcInterop.newWeakRef(null) == null, "newWeakRef(nil) must be nil");
        assertTrue(GcInterop.weakRefValue(null) == null, "weak value of nil ref must be nil");

        // Drop the strong ref; force collection; the weak must clear.
        WeakReference<Object> ref2 = GcInterop.newWeakRef(new Object());
        for (int i = 0; i < 50 && GcInterop.weakRefValue(ref2) != null; i++) {
            GcInterop.gc();
            @SuppressWarnings("unused")
            byte[] pressure = new byte[1 << 20];
        }
        assertTrue(GcInterop.weakRefValue(ref2) == null,
                "weak reference must clear after its referent is collected");
    }

    /** memStats: mapped fields present with plausible values. */
    public void testMemStatsFields() {
        Map<String, Long> m = GcInterop.memStats();
        for (String f : new String[] {
                "HeapAlloc", "HeapSys", "Sys", "NumGC", "PauseTotalNs",
                "NumForcedGC", "PauseNsLast" }) {
            assertTrue(m.containsKey(f), "memStats must contain " + f);
            assertTrue(m.get(f) >= 0, "memStats." + f + " must be non-negative");
        }
        assertTrue(m.get("HeapAlloc") <= m.get("HeapSys"),
                "HeapAlloc (used) must not exceed HeapSys (committed)");
        assertTrue(m.get("HeapSys") <= m.get("Sys"),
                "HeapSys must not exceed Sys (heap+non-heap committed)");
    }

    /** memStats: NumForcedGC tracks explicit gc() calls. */
    public void testMemStatsForcedGcCount() {
        long before = GcInterop.memStats().get("NumForcedGC");
        GcInterop.gc();
        long after = GcInterop.memStats().get("NumForcedGC");
        assertEquals(before + 1, after, "NumForcedGC must increment per gc() call");
    }

    /** keepAlive: callable, no-op observably (must not throw). */
    public void testKeepAliveNoop() {
        Object x = new Object();
        GcInterop.keepAlive(x);
        GcInterop.keepAlive(null); // gc allows nil
    }

    /** setFinalizer: argument validation matches gc's panics. */
    public void testSetFinalizerValidation() {
        boolean threw = false;
        try {
            GcInterop.setFinalizer(null, null);
        } catch (IllegalArgumentException e) {
            threw = true;
        }
        assertTrue(threw, "SetFinalizer(nil, nil) must throw");

        threw = false;
        try {
            GcInterop.setFinalizer("not-a-pointer", null);
        } catch (IllegalArgumentException e) {
            threw = true;
        }
        assertTrue(threw, "SetFinalizer(non-pointer, nil) must throw");

        threw = false;
        try {
            GoPointer p = new GoPointer(new Object());
            GcInterop.setFinalizer(p, "not-a-func");
        } catch (IllegalArgumentException e) {
            threw = true;
        }
        assertTrue(threw, "SetFinalizer(ptr, non-func) must throw");
    }

    /** setFinalizer(ptr, nil) on an object with no finalizer is a silent no-op. */
    public void testSetFinalizerRemoveAbsent() {
        GoPointer p = new GoPointer(new Object());
        GcInterop.setFinalizer(p, null); // must not throw
    }

    /** gc(): returns promptly (does not hang) on the normal path. */
    public void testGcReturnsPromptly() throws Exception {
        // Note: this exercises the fast path where MXBean counts advance.
        // The 30s-timeout path (explicit GC disabled via -XX:+DisableExplicitGC)
        // cannot be tested in-process; it is covered by code inspection
        // (bounded poll loop with deadline).
        long start = System.nanoTime();
        GcInterop.gc();
        long ms = (System.nanoTime() - start) / 1_000_000L;
        assertTrue(ms < 60_000L, "gc() must return within the documented bound, took " + ms + "ms");
    }

    /**
     * Minimal {@link RootCallTarget} test double: records invocation in a
     * flag. The finalizer machinery only needs a callable GoFunc; the body
     * is irrelevant to what these tests prove (registration reachability),
     * and the end-to-end corpus test (gc07) covers a real compiled
     * finalizer through the exporter.
     */
    private static final class FlagCallTarget implements RootCallTarget {
        final AtomicBoolean ran = new AtomicBoolean();
        @Override
        public Object call(Object... arguments) {
            ran.set(true);
            return null;
        }
        @Override
        public RootNode getRootNode() {
            throw new UnsupportedOperationException("test double has no RootNode");
        }
    }

    /**
     * Spins {@code System.gc()} + allocation pressure until the finalizer
     * flag is set or the attempt budget is exhausted. Deliberately never
     * calls {@link GcInterop#gc()} — that is the point of
     * {@link #testFinalizerRunsWithoutExplicitGc}.
     */
    private static void awaitFinalizer(AtomicBoolean ran) throws Exception {
        for (int i = 0; i < 300 && !ran.get(); i++) {
            System.gc();
            @SuppressWarnings("unused")
            byte[] pressure = new byte[1 << 20];
            if (!ran.get()) {
                Thread.sleep(20);
            }
        }
    }

    /**
     * R-P0-3: registering a finalizer must not pin its object. With no
     * {@code runtime.GC()} call at all, the object must still become
     * collectable and the finalizer must run. (Pre-fix this failed:
     * PINNED held every finalizable object strongly until an explicit
     * gc(), leaking them forever.)
     */
    public void testFinalizerRunsWithoutExplicitGc() throws Exception {
        FlagCallTarget target = new FlagCallTarget();
        GoPointer p = new GoPointer(new Object());
        GcInterop.setFinalizer(p, new GoFunc(target, null));
        p = null; // the only strong ref; the Cleaner phantom + weak BY_OBJ remain
        // Deliberately no GcInterop.gc() anywhere in this test.
        awaitFinalizer(target.ran);
        assertTrue(target.ran.get(),
                "finalizer must run without any runtime.GC() call (object must be collectable)");
    }

    /**
     * R-P0-3: {@code runtime.GC()} must not release finalizer registrations
     * (gc semantics). A still-reachable object must not be finalized across
     * a gc() call, and the registration must survive gc() so the finalizer
     * still fires once the object is actually dropped.
     */
    public void testFinalizerNotFiredEarlyByGc() throws Exception {
        FlagCallTarget target = new FlagCallTarget();
        GoPointer p = new GoPointer(new Object());
        GcInterop.setFinalizer(p, new GoFunc(target, null));
        // p stays strongly reachable: gc() must not finalize it early.
        // (Deterministic: the Cleaner only enqueues phantoms of unreachable
        // referents, so no wait can make this fire while p is reachable.)
        GcInterop.gc();
        System.gc();
        Thread.sleep(500);
        assertTrue(!target.ran.get(),
                "finalizer must not run for a still-reachable object, even across runtime.GC()");
        // Drop p: the retained registration must still fire.
        p = null;
        awaitFinalizer(target.ran);
        assertTrue(target.ran.get(),
                "finalizer registration must survive runtime.GC() and fire once the object is dropped");
    }
}
