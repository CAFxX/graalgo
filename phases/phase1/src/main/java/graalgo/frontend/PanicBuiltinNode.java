package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.Panic;
import graalgo.values.GoInterface;
import graalgo.values.GoTypeDescriptor;

/**
 * The {@code panic(x)} builtin (M5, WS F).
 *
 * <p>Throws the panic exception carrying the Go value. The value's dynamic
 * type descriptor and Go-syntax type name drive gc-exact rendering
 * ({@code printpanicval}) and {@code recover()} boxing. A nil value becomes
 * {@code *PanicNilError} (GG-0045 stance, gc ≥1.21).
 *
 * <p>If the static type is an interface, the dynamic descriptor/name are
 * derived from the interface value at runtime (a named dynamic type renders
 * as {@code Name(value)}; the int/int64 conflation is an M4 limitation).
 *
 * <p>Minimal WS-F wiring: worker-G's builtin surface (GG-0020) absorbs this
 * node when it lands; the runtime call ({@link Panic#throwPanic}) is the
 * frozen interface and does not change.
 */
public final class PanicBuiltinNode extends GoExprNode {
    @Child private GoExprNode value;
    private final GoTypeDescriptor descriptor;
    private final String typeName; // Go-syntax; non-null iff named basic or composite
    private final boolean staticIsInterface;

    public PanicBuiltinNode(GoExprNode value, GoTypeDescriptor descriptor, String typeName,
                            boolean staticIsInterface) {
        super(value.kind());
        this.value = value;
        this.descriptor = descriptor;
        this.typeName = typeName;
        this.staticIsInterface = staticIsInterface;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object v = Boxing.box(value.kind(), value, frame);
        if (staticIsInterface && v instanceof GoInterface gi && !gi.isNil()
                && gi.descriptor() instanceof GoTypeDescriptor gtd
                && !NodeFactory.isPredeclaredBasic(gtd)) {
            // Dynamic named/composite type: render as Name(value) / (Name) 0xADDR.
            Panic.throwPanic(gi.data(), gtd, gtd.name(), this);
        }
        Panic.throwPanic(v, descriptor, typeName, this);
        throw new AssertionError("unreachable");
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        executeObject(frame);
        throw new AssertionError("unreachable");
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        executeObject(frame);
        throw new AssertionError("unreachable");
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        executeObject(frame);
        throw new AssertionError("unreachable");
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        executeObject(frame);
        throw new AssertionError("unreachable");
    }
}
