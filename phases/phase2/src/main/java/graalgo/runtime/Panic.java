package graalgo.runtime;

import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoInterface;

/**
 * Phase-2 same-FQN overlay of Phase-1 {@code graalgo.runtime.Panic}
 * (P2-M5, WS E; P2-D8 shadowing pattern: phase2/out/classes precedes
 * phase1/out/classes, so this class shadows the Phase-1 original at
 * runtime).
 *
 * <p>Identical to the Phase-1 original except for the additive
 * GODEBUG=panicnil support: when {@link #setPanicNilOldBehavior(boolean)}
 * is enabled, a nil panic value is <em>not</em> wrapped in
 * {@code *PanicNilError}, restoring the pre-Go-1.21 semantics where
 * {@code recover()} after {@code panic(nil)} returns nil. With the flag
 * off (default), behavior is exactly the Phase-1 gc ≥1.21 behavior.
 *
 * <p>Drift guard: {@code PanicOverlayDriftTest} reflectively asserts that
 * every public method of the Phase-1 original exists here with an
 * identical signature, so a future Phase-1 change to this class fails
 * the suite instead of silently desyncing the overlay.
 *
 * <p>Do NOT add other behavior here: this overlay exists solely for the
 * panicnil knob. (P2-M5 triage: EMULATED.)
 */
public final class Panic {
    private Panic() {}

    /**
     * When true, {@link #throwPanic} preserves a nil panic value instead
     * of wrapping it in {@code *PanicNilError} (GODEBUG=panicnil=1,
     * pre-1.21 semantics). Volatile: set once at startup by
     * {@code GoDebug.apply()}, read on every panic.
     */
    private static volatile boolean panicNilOldBehavior = false;

    /**
     * Enables or disables pre-1.21 {@code panic(nil)} semantics.
     * Additive w.r.t. the Phase-1 surface (not in the freeze).
     */
    public static void setPanicNilOldBehavior(boolean enabled) {
        panicNilOldBehavior = enabled;
    }

    /** Whether pre-1.21 {@code panic(nil)} semantics are enabled. */
    public static boolean isPanicNilOldBehavior() {
        return panicNilOldBehavior;
    }

    /**
     * Throws the panic exception carrying the Go value (frozen).
     *
     * <p>A nil value (untyped {@code panic(nil)}, or a nil interface value)
     * becomes {@code *PanicNilError}, replicating gc ≥1.21 exactly (GG-0045
     * stance) — unless {@link #isPanicNilOldBehavior()} is set, in which
     * case the nil is preserved (pre-1.21 semantics). A {@link GoInterface}
     * value is unboxed to its dynamic type/value first.
     */
    public static void throwPanic(Object goValue) {
        throwPanic(goValue, null, null, null);
    }

    /**
     * Throws the panic exception with full rendering/boxing info (additive).
     *
     * @param goValue    the Go value in its D5 mapping (a
     *                   {@link GoInterface} is unboxed; nil becomes
     *                   {@code *PanicNilError} unless old behavior is on).
     * @param descriptor the dynamic-type descriptor (a
     *                   {@code graalgo.values.GoTypeDescriptor}), or null.
     * @param goTypeName Go-syntax type name for composite rendering, or null.
     * @param throwSite  the throwing AST node, or null.
     */
    public static void throwPanic(Object goValue, Object descriptor, String goTypeName, Node throwSite) {
        Object v = goValue;
        Object d = descriptor;
        if (v instanceof GoInterface gi) {
            if (gi.isNil()) {
                if (!panicNilOldBehavior) {
                    v = PanicNilError.INSTANCE;
                    d = PanicNilError.descriptor();
                }
            } else {
                v = gi.data();
                d = gi.descriptor();
            }
        }
        if (v == null && !panicNilOldBehavior) {
            v = PanicNilError.INSTANCE;
            d = PanicNilError.descriptor();
        }
        throw new PanicException(v, d, goTypeName, throwSite);
    }

    /**
     * Returns the in-flight panic value, or Go nil if none (frozen).
     *
     * <p>Non-nil only when called <em>directly</em> by a deferred function
     * while a panic is in flight: the {@link DeferChain} arms the
     * recover-allowed flag around each deferred call during panic unwinding
     * and clears it for nested calls and deferred builtin calls
     * (gc-verified). The returned value is boxed as a Go {@code any}
     * ({@link GoInterface}) when the descriptor is known, so type switches
     * and assertions on the recovered value behave like gc's.
     *
     * <p>With {@link #isPanicNilOldBehavior()}, a recovered
     * {@code panic(nil)} yields nil (pre-1.21), indistinguishable from
     * "no panic" — exactly like gc.
     */
    public static Object recover() {
        GoroutineState s = GoroutineState.current();
        if (s == null || !s.isRecoverAllowed()) {
            return null;
        }
        PanicException p = s.panicking();
        if (p == null) {
            return null;
        }
        p.markRecovered();
        if (p.descriptor() != null) {
            return new GoInterface(p.descriptor(), p.goValue(), null);
        }
        return p.goValue();
    }
}
