package graalgo.runtime;

import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoTypeDescriptor;

/**
 * Go's {@code panic} / {@code recover} (M5, WS F; GG-0019). Same-FQN shadow
 * (bridge B-8, inventoried in {@code docs/arch/bridge-inventory.md}).
 *
 * <p>This is a <em>forked</em> implementation of the panic mechanism used by
 * the truffle module's compiled code: panic is the real
 * {@link PanicException}-based mechanism with deferred-call unwinding and
 * lazy traces. The runtime module carries the same fully-qualified name
 * ({@code graalgo.runtime.Panic} in {@code internal/backend/runtime}) with
 * one additive delta: the {@code GODEBUG=panicnil} knob (a nil panic value
 * is not wrapped in {@code *PanicNilError}, restoring the pre-Go-1.21
 * semantics). With the flag off (default), the runtime copy's behavior is
 * exactly this file's gc &#8805;1.21 behavior.
 *
 * <p>Same-FQN overlay mechanism: the truffle module compiles this class as
 * part of its own source set (the runtime module compiles the joint round
 * first; the truffle module then compiles these same-FQN classes against
 * it). At launch, {@code internal/backend/graalgo-run} puts
 * {@code runtime/out/classes} <em>before</em> {@code truffle/out/classes} on
 * the classpath, so the runtime module's copy — including the panicnil
 * knob — shadows this file's. The signatures are the frozen contract; a
 * {@code PanicOverlayDriftTest} reflectively asserts signature parity
 * between the two copies, so a change here that desyncs the overlay fails
 * the suite instead of silently diverging.
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
