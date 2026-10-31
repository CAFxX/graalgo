package graalgo.runtime;

import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoString;
import graalgo.values.GoTypeDescriptor;

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
 * <p>Ported Phase-1 behavior (fix-redmaster-r43): the bug-019
 * GoErrorValue-descriptor inference in {@link #throwPanic} (see its
 * comment there). The "solely for the panicnil knob" restriction is
 * superseded for this one port: the overlay shadows the Phase-1 class
 * at runtime, so leaving bug-019 unported meant the live panic path
 * lacked the descriptor inference the frozen class already had.
 * No signatures change, so the drift guard is unaffected.
 */
public final class Panic {
    private Panic() {}

    /**
     * Phase-2-only diagnostic: true when THIS class (the Phase-2
     * replacement) is loaded, absent on the Phase-1 original. The
     * launcher calls it at startup (R-02 overlay check) to fail fast
     * if the classpath order is wrong. NOT part of the frozen contract
     * — intentionally not covered by FreezeSignatureTest (same
     * precedent as the panicnil knob above: additive, not frozen).
     */
    public static boolean isPhase2() {
        return true;
    }

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
                } else {
                    // Pre-1.21: panic of a nil interface value is a nil
                    // panic (like untyped panic(nil)); recover() returns
                    // nil and uncaught prints "panic: nil".
                    v = null;
                    d = null;
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
        // fix-redmaster-r43: ported from the Phase-1 original (bug-019).
        // A GoErrorValue (runtime panic like "comparing uncomparable
        // type") thrown without a descriptor needs one for recover() to
        // box it as a GoInterface. Infer gc's type name from the
        // message: most runtime errors are runtime.errorString;
        // interface conversion is *runtime.TypeAssertionError. Without
        // this port the overlay (which shadows the Phase-1 class at
        // runtime) returned the raw GoErrorValue from recover(), and
        // downstream interface consumers failed (snapshot 48: bare
        // empty-interface descriptor at buildInterfaceView).
        if (v instanceof GoErrorValue gev && d == null) {
            String typeName = gev.errorString().startsWith("interface conversion: ")
                    ? "*runtime.TypeAssertionError"
                    : "runtime.errorString";
            d = new GoTypeDescriptor(typeName, GoKind.STRING, false);
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
     *
     * <p>fix-redmaster-r43: a string-kind runtime error's interface data
     * IS its message string (gc: {@code errorString} is
     * {@code type errorString string}). The panic carrier is a
     * {@code GoErrorValue} wrapper, not the string: boxing the wrapper
     * as the interface data makes every kind-STRING consumer read a
     * non-string (fmt's {@code append([]byte, s...)} CCEs). Unwrap to
     * the message GoString here, at the recover boxing site only — the
     * uncaught path reads the PanicException directly and is
     * untouched. Struct-kind runtime errors (boundsError) keep the
     * wrapper as data: their field values are not recoverable from
     * the carrier, so that path stays loud rather than fabricated.
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
            Object data = p.goValue();
            if (data instanceof GoErrorValue gev
                    && p.descriptor() instanceof GoTypeDescriptor gd
                    && gd.kind() == GoKind.STRING) {
                data = GoString.fromUtf8(gev.errorString());
            }
            return new GoInterface(p.descriptor(), data, null);
        }
        return p.goValue();
    }
}
