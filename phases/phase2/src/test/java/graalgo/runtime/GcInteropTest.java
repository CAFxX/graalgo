package graalgo.runtime;

import java.lang.ref.WeakReference;
import java.util.Map;

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

    /** gc(): returns (does not hang) even with explicit GC disabled. */
    public void testGcReturnsWithExplicitGcDisabled() throws Exception {
        // -XX:+DisableExplicitGC makes System.gc() a no-op; the MXBean poll
        // must still return via the bounded timeout rather than hang.
        // We cannot set the flag from inside the test; instead assert the
        // plain call returns promptly.
        long start = System.nanoTime();
        GcInterop.gc();
        long ms = (System.nanoTime() - start) / 1_000_000L;
        assertTrue(ms < 60_000L, "gc() must return within the documented bound, took " + ms + "ms");
    }
}
