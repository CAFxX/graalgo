package graalgo.interop;

import graalgo.frontend.DerefInterop;
import graalgo.p3reflect.ReflectTypes;
import graalgo.p3reflect.ReflectValues;
import graalgo.p3unsafe.UnsafeTable;
import graalgo.values.GoInterface;

/**
 * {@link DerefInterop} provider: the interop module's implementation of the
 * services {@code DerefNode} (truffle module) needs from p3unsafe/p3reflect.
 *
 * <p>Discovered via {@link java.util.ServiceLoader} (see
 * {@code META-INF/services/graalgo.frontend.DerefInterop}) so the truffle
 * module does not need a compile-time dependency on the interop module —
 * the same circular-dependency break as {@code P3KindRegistrations} provides
 * for {@code WireKindRegistrar}.
 *
 * <p>Public no-arg constructor is required for {@code ServiceLoader}
 * instantiation.
 */
public final class DerefInteropProvider implements DerefInterop {
    /** Public for {@link java.util.ServiceLoader} instantiation. */
    public DerefInteropProvider() {}

    @Override
    public long internUnsafeWord(Object word) {
        return UnsafeTable.current().intern(word);
    }

    @Override
    public AbiTypeInfo abiTypeInfo(GoInterface gi) {
        long tok = ReflectValues.typeof(gi);
        ReflectTypes.TypeInfo ti = ReflectTypes.table().info(tok);
        return new AbiTypeInfo(ti.kind, ti.size, ti.align);
    }
}
