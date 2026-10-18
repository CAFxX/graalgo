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

    /** setFinalizer: misuse signals carry gc's exact messages (ADR-0002 D4). */
    public void testSetFinalizerValidation() {
        // gc throw()s "runtime.SetFinalizer: first argument is nil"
        // (fatal, unrecoverable — SetFinalizerCallNode maps the signal).
        assertMisuseSignal(
                () -> GcInterop.setFinalizer(null, null),
                "runtime.SetFinalizer: first argument is nil",
                "SetFinalizer(nil, nil)");

        // gc: "runtime.SetFinalizer: first argument is <T>, not pointer"
        assertMisuseSignal(
                () -> GcInterop.setFinalizer("not-a-pointer", null),
                "runtime.SetFinalizer: first argument is string, not pointer",
                "SetFinalizer(non-pointer, nil)");

        // Typed-nil pointer: gc throw()s "pointer not in allocated block".
        assertMisuseSignal(
                () -> GcInterop.setFinalizer(new GoPointer((Object) null), null),
                "runtime.SetFinalizer: pointer not in allocated block",
                "SetFinalizer(typed-nil ptr, nil)");

        // gc: "runtime.SetFinalizer: second argument is <T>, not a function"
        GoPointer p = new GoPointer(new Object());
        assertMisuseSignal(
                () -> GcInterop.setFinalizer(p, "not-a-func"),
                "runtime.SetFinalizer: second argument is string, not a function",
                "SetFinalizer(ptr, non-func)");
    }

    /** A SetFinalizer misuse signal: IllegalArgumentException + exact message. */
    private interface Misuse {
        void trigger();
    }

    private static void assertMisuseSignal(Misuse m, String gcMessage, String what) {
        try {
            m.trigger();
        } catch (IllegalArgumentException e) {
            assertEquals(gcMessage, e.getMessage(), what + " signal message");
            return;
        }
        throw new AssertionError(what + " must signal, expected message: " + gcMessage);
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
}
