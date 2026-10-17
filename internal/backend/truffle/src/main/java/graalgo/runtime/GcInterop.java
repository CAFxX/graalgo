package graalgo.runtime;

/**
 * Compile-time Go-observable GC surface (P2-M4, ADR-0002 D4; bridge B-8,
 * inventoried in {@code docs/arch/bridge-inventory.md}).
 *
 * <p>This class exists so the truffle module compiles against the GC
 * interop surface despite the truffle&#8596;runtime compile cycle (the
 * runtime module compiles the joint round first; the truffle module then
 * compiles this same-FQN class against it). The real implementation lives
 * in the runtime module ({@code graalgo.runtime.GcInterop} in
 * {@code internal/backend/runtime}: single shared
 * {@code java.lang.ref.Cleaner} for {@code SetFinalizer},
 * {@code Reference.reachabilityFence} for {@code KeepAlive},
 * {@code System.gc()} + MXBean poll wait for {@code GC}, per ADR-0002 D4).
 * At launch, {@code internal/backend/graalgo-run} puts
 * {@code runtime/out/classes} <em>before</em> {@code truffle/out/classes} on
 * the classpath, so the real implementation shadows this class — this stub
 * is only ever loaded if the launcher classpath order is violated (a launch
 * bug). Every method throws {@link UnsupportedOperationException}; the
 * signatures are the contract (they mirror gc's {@code runtime} package
 * surface added to the synthetic import overlay).
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
