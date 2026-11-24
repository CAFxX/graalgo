package graalgo.runtime;

import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoString;
import graalgo.values.GoTypeDescriptor;

/**
 * Go's {@code panic} / {@code recover} (M5, WS F; GG-0019).
 *
 * <p>Replaces the WS-G local verification stub (which carried this exact
 * frozen surface): panic is now the real {@link PanicException}-based
 * mechanism with deferred-call unwinding and lazy traces.
 *
 * <p>The frozen cross-workstream surface (verbatim):
 * <ul>
 *   <li>{@link #throwPanic(Object)} — throws the panic exception carrying
 *       the Go value;
 *   <li>{@link #recover()} — returns the in-flight panic value, or Go nil
 *       if none (callable only from deferred frames; defined behavior
 *       elsewhere = nil).
 * </ul>
 *
 * <p>The additive overload {@link #throwPanic(Object, Object, String, Node)}
 * carries the dynamic-type descriptor, the Go-syntax type name, and the
 * throw site for {@code printpanicval}-compatible rendering and
 * {@code recover()} boxing; it does not change the frozen signatures.
 */
public final class Panic {
    private Panic() {}

    /**
     * Throws the panic exception carrying the Go value (frozen).
     *
     * <p>A nil value (untyped {@code panic(nil)}, or a nil interface value)
     * becomes {@code *PanicNilError}, replicating gc ≥1.21 exactly (GG-0045
     * stance). A {@link GoInterface} value is unboxed to its dynamic
     * type/value first.
     */
    public static void throwPanic(Object goValue) {
        throwPanic(goValue, null, null, null);
    }

    /**
     * Throws the panic exception with full rendering/boxing info (additive).
     *
     * @param goValue    the Go value in its D5 mapping (a
     *                   {@link GoInterface} is unboxed; nil becomes
     *                   {@code *PanicNilError}).
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
                v = PanicNilError.INSTANCE;
                d = PanicNilError.descriptor();
            } else {
                v = gi.data();
                d = gi.descriptor();
            }
        }
        if (v == null) {
            v = PanicNilError.INSTANCE;
            d = PanicNilError.descriptor();
        }
        // bug-019: a GoErrorValue (runtime panic like "comparing uncomparable
        // type") thrown without a descriptor needs one for recover() to box
        // it as a GoInterface. Infer gc's type name from the message: most
        // runtime errors are runtime.errorString; interface conversion is
        // *runtime.TypeAssertionError.
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
