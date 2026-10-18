package graalgo.p3abi;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;

/**
 * The {@code internal/abi.TypeOf} intrinsic (fix-redmaster-buildok r5):
 * the {@code "abitypeof"} wire node.
 *
 * <p>gc implements {@code abi.TypeOf} via unsafe interface reinterpretation
 * ({@code *(*EmptyInterface)(unsafe.Pointer(&a))}), which relies on the
 * memory layout of interfaces. graalgo represents interfaces as high-level
 * {@link GoInterface} objects, so the reinterpretation cannot work. This
 * intrinsic implements {@code TypeOf} directly: given an {@code any} value,
 * it materializes the corresponding {@code *internal/abi.Type} as a
 * fully-populated most-derived GoStruct (via
 * {@link graalgo.p3reflect.ReflectTypes#materialize(long)}).
 *
 * <p>Fields: {@code "arg"} (the {@code any} value), {@code "type"} (result
 * {@code *internal/abi.Type} type-table id).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 */
public final class AbiTypeOfNode extends GoExprNode {
    @Child private GoExprNode arg;

    private AbiTypeOfNode(GoExprNode arg) {
        super(GoKind.POINTER);
        this.arg = arg;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object v = arg.executeObject(frame);
        // Unwrap the interface to get the dynamic type.
        GoInterface gi;
        if (v instanceof GoInterface g) {
            gi = g;
        } else {
            // Non-interface value: shouldn't happen (TypeOf takes any),
            // but handle gracefully.
            return null;
        }
        if (gi.isNil()) {
            // Nil interface: TypeOf returns nil *Type.
            return null;
        }
        long tok = graalgo.p3reflect.ReflectValues.typeof(gi);
        GoStruct abiType = graalgo.p3reflect.ReflectTypes.materialize(tok);
        return new GoPointer(abiType);
    }

    /**
     * Builds the node from the {@code "abitypeof"} wire node. Fields:
     * {@code arg} (expr node), {@code type} (result type-table id — must be
     * a pointer type).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory,
            NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "abitypeof");
        String path = "abitypeof.fields";
        Json.Value av = f.opt("arg");
        if (!(av instanceof Json.Object ao)) {
            throw new FrontendException(path + ": abitypeof needs \"arg\"");
        }
        GoExprNode argNode = factory.buildExpr(ao, ctx);
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": abitypeof needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        GoExprNode n = new AbiTypeOfNode(argNode);
        n.setTypeId(typeId);
        return n;
    }
}
