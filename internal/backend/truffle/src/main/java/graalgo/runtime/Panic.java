package graalgo.runtime;

import com.oracle.truffle.api.nodes.Node;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
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
     * (gc-verified). The returned value is always boxed as a Go {@code any}
     * ({@link GoInterface}), so type switches and assertions on the
     * recovered value behave like gc's. When the panic carried no
     * descriptor (a deferred {@code panic(x)} whose defer site did not
     * attach one, or a makeslice / timer / chan / unimplemented throw
     * site — R22-F-5), the descriptor is synthesized from the Java
     * carrier type (see {@link #synthesizeDescriptorless}); a null value
     * stays Java null (Go nil).
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
        Object descriptor = p.descriptor();
        Object data = p.goValue();
        if (descriptor == null && data != null) {
            // R22-F-5: a descriptor-less panic used to return the raw Java
            // value here; every consumer assumes a GoInterface
            // (TypeAssertNode narrows blindly, TypeSwitchNode casts), so
            // `r := recover(); r.(int)` threw ClassCastException.
            Synthesized synth = synthesizeDescriptorless(data);
            descriptor = synth.descriptor();
            data = synth.data();
        }
        if (descriptor != null) {
            if (data instanceof GoErrorValue gev
                    && descriptor instanceof GoTypeDescriptor gd
                    && gd.kind() == GoKind.STRING) {
                // fix-redmaster-r43 (ported from the phase-2 overlay): a
                // string-kind runtime error's interface data IS its message
                // string (gc: errorString is `type errorString string`).
                // The panic carrier is a GoErrorValue wrapper, not the
                // string: boxing the wrapper as the interface data makes
                // every kind-STRING consumer read a non-string (fmt's
                // `append([]byte, s...)` CCEs). Unwrap to the message
                // GoString here, at the recover boxing site only — the
                // uncaught path reads the PanicException directly and is
                // untouched. Struct-kind runtime errors (boundsError) keep
                // the wrapper as data: their field values are not
                // recoverable from the carrier, so that path stays loud
                // rather than fabricated.
                data = GoString.fromUtf8(gev.errorString());
            }
            return new GoInterface(descriptor, data, null);
        }
        return null;
    }

    /** A synthesized descriptor plus the (possibly normalized) panic value. */
    private record Synthesized(Object descriptor, Object data) {}

    /**
     * R22-F-5: synthesizes the canonical descriptor for a descriptor-less
     * panic value, plus the data normalized to its kind's D5 carrier, so
     * {@link #recover()} can always return a GoInterface.
     *
     * <p>The Java-carrier → Go-type mapping mirrors
     * {@code GoFuncRootNode.wrapInterface} (a D5 {@code Long} is the
     * 64-bit carrier shared by Go {@code int} and {@code int64}; "int64"
     * is used — callers that know the exact Go type must box via the type
     * table instead, which the deferred-panic path does: DeferNode /
     * DeferredCall carry the exact descriptor from the defer site). All
     * descriptors come from {@link NodeFactory#basicDescriptorForName},
     * the single unification point (P3-M7R2): a fresh
     * {@code new GoTypeDescriptor} would break identity comparisons
     * ("is int64, not int64"). The GoErrorValue branch is the sole
     * exception — it mirrors {@link #throwPanic}'s bug-019 inference
     * verbatim, and {@link #recover()}'s STRING-kind unwrap converts the
     * data to the message GoString.
     *
     * <p>A raw {@code java.lang.String} (a descriptor-less throw site such
     * as a timer / chan / unimplemented panic) normalizes to a
     * {@code GoString}: a GoInterface of kind STRING carries a GoString,
     * never a java.lang.String. Structured values
     * (GoStruct / GoSlice / GoMap / GoChan / GoFunc / GoPointer, arrays)
     * carry no recoverable exact dynamic type at this site, so they fall
     * back to the empty-interface descriptor (the wrapInterface
     * convention): type switches and assertions then fail with a proper
     * Go panic instead of a JVM ClassCastException, and recover() keeps
     * its GoInterface invariant.
     */
    private static Synthesized synthesizeDescriptorless(Object v) {
        if (v instanceof String s) {
            return new Synthesized(
                    NodeFactory.basicDescriptorForName("string", GoKind.STRING),
                    GoString.fromUtf8(s));
        }
        if (v instanceof GoString) {
            return new Synthesized(
                    NodeFactory.basicDescriptorForName("string", GoKind.STRING), v);
        }
        if (v instanceof GoErrorValue gev) {
            String typeName = gev.errorString().startsWith("interface conversion: ")
                    ? "*runtime.TypeAssertionError"
                    : "runtime.errorString";
            return new Synthesized(new GoTypeDescriptor(typeName, GoKind.STRING, false), v);
        }
        if (v instanceof Long) {
            return new Synthesized(
                    NodeFactory.basicDescriptorForName("int64", GoKind.INT64), v);
        }
        if (v instanceof Integer) {
            return new Synthesized(
                    NodeFactory.basicDescriptorForName("int32", GoKind.INT32), v);
        }
        if (v instanceof Double) {
            return new Synthesized(
                    NodeFactory.basicDescriptorForName("float64", GoKind.FLOAT64), v);
        }
        if (v instanceof Float) {
            return new Synthesized(
                    NodeFactory.basicDescriptorForName("float32", GoKind.FLOAT32), v);
        }
        if (v instanceof Boolean) {
            return new Synthesized(
                    NodeFactory.basicDescriptorForName("bool", GoKind.BOOL), v);
        }
        if (v instanceof GoComplex32) {
            return new Synthesized(
                    NodeFactory.basicDescriptorForName("complex64", GoKind.COMPLEX64), v);
        }
        if (v instanceof GoComplex64) {
            return new Synthesized(
                    NodeFactory.basicDescriptorForName("complex128", GoKind.COMPLEX128), v);
        }
        return new Synthesized(
                NodeFactory.basicDescriptorForName("interface", GoKind.INTERFACE), v);
    }
}
