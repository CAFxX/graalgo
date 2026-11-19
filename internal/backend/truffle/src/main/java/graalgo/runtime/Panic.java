package graalgo.runtime;

import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoString;
import graalgo.values.GoTypeDescriptor;

/**
 * Go panic surface: {@code panic}, {@code recover}, runtime errors.
 *
 * <p>GODEBUG=panicnil support: when {@link #setPanicNilOldBehavior(boolean)}
 * is enabled, a nil panic value is <em>not</em> wrapped in
 * {@code *PanicNilError}, restoring the pre-Go-1.21 semantics where
 * {@code recover()} after {@code panic(nil)} returns nil. With the flag
 * off (default), behavior is gc's ≥1.21 behavior.
 *
 * <p>{@link #throwPanic} infers the {@code GoErrorValue} descriptor for
 * error values (bug-019, fix-redmaster-r43; see its comment there).
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
    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    public static void setPanicNilOldBehavior(boolean enabled) {
        panicNilOldBehavior = enabled;
    }

    /** Whether pre-1.21 {@code panic(nil)} semantics are enabled. */
    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
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
    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
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
    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    public static void throwPanic(Object goValue, Object descriptor, String goTypeName, Node throwSite) {
        Object v = goValue;
        Object d = descriptor;
        if (v instanceof GoInterface gi) {
            if (gi.isNil()) {
                if (!panicNilOldBehavior) {
                    v = PanicNilError.INSTANCE;
                    d = RuntimeErrorTypes.panicNilDescriptor();
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
            d = RuntimeErrorTypes.panicNilDescriptor();
        }
        // fix-redmaster-r43 (bug-019): a GoErrorValue (runtime panic like
        // "comparing uncomparable type") thrown without a descriptor needs
        // one for recover() to box it as a GoInterface. Infer gc's type
        // name from the message: most runtime errors are
        // runtime.errorString; interface conversion is
        // *runtime.TypeAssertionError. Without it recover() returned the
        // raw GoErrorValue and downstream interface consumers failed
        // (snapshot 48: bare empty-interface descriptor at
        // buildInterfaceView).
        if (v instanceof GoErrorValue gev && d == null) {
            // T0.A: the value's own gc type when it records one (bounds
            // errors are runtime.boundsError, nil-map writes
            // runtime.plainError, ...), else the message-based inference.
            // Canonical runtime descriptors (RuntimeErrorTypes) carry the
            // error/runtime.Error method set for assertions and switches.
            String typeName = gev.typeName();
            if (RuntimeErrorTypes.descriptor(typeName) == null) {
                typeName = gev.errorString().startsWith("interface conversion: ")
                        ? "*runtime.TypeAssertionError"
                        : "runtime.errorString";
            }
            d = RuntimeErrorTypes.descriptor(typeName);
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
    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
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
