package graalgo.runtime;

/**
 * Go-observable GC surface (P2-M4, ADR-0002 D4).
 *
 * <p><strong>Phase-1 stub.</strong> Every method throws
 * {@link UnsupportedOperationException}; the Phase-2 module shadows this
 * class with the real implementation (same fully-qualified name, phase-2
 * classes first on the classpath — the documented overlay mechanism).
 * The signatures are the contract (they mirror gc's {@code runtime}
 * package surface added to the synthetic import overlay); bodies are
 * INTERNAL per freeze §1.
 */
public final class GcInterop {
    private GcInterop() {}

    /**
     * STUB — {@code runtime.SetFinalizer(obj, finalizer)}. Phase 2:
     * single shared {@code java.lang.ref.Cleaner} (ADR-0002 D4).
     */
    public static void setFinalizer(Object obj, Object finalizer) {
        throw new UnsupportedOperationException(
                "Phase 2: runtime.SetFinalizer needs the GC interop layer (P2-M4)");
    }

    /**
     * STUB — {@code runtime.KeepAlive(x)}. Phase 2:
     * {@code Reference.reachabilityFence} (ADR-0002 D4).
     */
    public static void keepAlive(Object x) {
        throw new UnsupportedOperationException(
                "Phase 2: runtime.KeepAlive needs the GC interop layer (P2-M4)");
    }

    /**
     * STUB — {@code runtime.GC()}. Phase 2: {@code System.gc()} + MXBean
     * poll wait with bounded timeout (ADR-0002 D4).
     */
    public static void gc() {
        throw new UnsupportedOperationException(
                "Phase 2: runtime.GC needs the GC interop layer (P2-M4)");
    }
}
