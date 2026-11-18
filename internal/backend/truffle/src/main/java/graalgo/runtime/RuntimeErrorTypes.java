package graalgo.runtime;

import java.util.Map;
import java.util.Set;

import graalgo.values.GoKind;
import graalgo.values.GoTypeDescriptor;

/**
 * The dynamic types of runtime panics (T0.A panic value model; SPEC §3.5).
 *
 * <p>gc panics with values of runtime-internal types —
 * {@code runtime.errorString}, {@code runtime.plainError},
 * {@code runtime.boundsError}, {@code *runtime.TypeAssertionError},
 * {@code *runtime.PanicNilError} — every one of which implements
 * {@code error} and {@code runtime.Error} ({@code Error() string} plus the
 * {@code RuntimeError()} marker). graalgo carries these values as
 * {@link GoErrorValue}s that the frontend never compiled, so neither the
 * program document's method sets nor its method tables know them. This
 * class is the runtime's own type table for them: one canonical
 * {@link GoTypeDescriptor} per type (so recovered values compare and
 * dedupe by identity) and their method set, in the exporter's implkey
 * form, which type assertions/switches consult when the document has no
 * entry for the dynamic type. The method <em>bodies</em> are installed
 * into the interface method tables by the interop launcher
 * ({@code graalgo.iface.RuntimeErrorMethods}).
 */
public final class RuntimeErrorTypes {
    private RuntimeErrorTypes() {}

    /**
     * The exporter's implkeys (export.go {@code implKeyOf}: name, NUL,
     * structural signature; {@code basic:17} is {@code string}) of
     * {@code Error() string} and {@code RuntimeError()}.
     */
    public static final String ERROR_IMPLKEY = "Error\u0000func()(basic:17)";
    public static final String RUNTIME_ERROR_IMPLKEY = "RuntimeError\u0000func()()";

    private static final Set<String> METHOD_SET = Set.of(ERROR_IMPLKEY, RUNTIME_ERROR_IMPLKEY);

    /**
     * Canonical descriptors by gc type name. String-kinded types carry
     * their message as a {@code string} (gc: {@code type errorString
     * string}); the others are opaque (their fields are unexported).
     */
    private static final Map<String, GoTypeDescriptor> DESCRIPTORS = Map.of(
            "runtime.errorString", new GoTypeDescriptor("runtime.errorString", GoKind.STRING, false),
            "runtime.plainError", new GoTypeDescriptor("runtime.plainError", GoKind.STRING, false),
            "runtime.boundsError", new GoTypeDescriptor("runtime.boundsError", GoKind.STRING, false),
            "*runtime.TypeAssertionError",
            new GoTypeDescriptor("*runtime.TypeAssertionError", GoKind.STRING, false),
            "*runtime.PanicNilError", PanicNilError.descriptor());

    /** All runtime error type names (method-table installation). */
    public static Set<String> typeNames() {
        return DESCRIPTORS.keySet();
    }

    /**
     * The canonical descriptor for a runtime panic value, or null when
     * {@code typeName} is not a runtime error type.
     */
    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    public static GoTypeDescriptor descriptor(String typeName) {
        return typeName == null ? null : DESCRIPTORS.get(typeName);
    }

    /**
     * The method implkey set of a runtime error type, or null when
     * {@code descriptorName} is not one (callers then fall back to the
     * empty set, exactly as for any type without methods).
     */
    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    public static Set<String> methodSet(String descriptorName) {
        return DESCRIPTORS.containsKey(descriptorName) ? METHOD_SET : null;
    }
}
