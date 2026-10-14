package graalgo.rtstack;

import java.util.Arrays;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.GoFrames;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;

/**
 * {@code runtime.CallersFrames(pcs)}: the {@code "runtimecallersframes"}
 * wire node.
 *
 * <p>Returns a {@code *runtime.Frames} (a {@link GoPointer} to a
 * {@link GoFrames} holding a copy of the PCs) for iteration via
 * {@code (*Frames).Next}. A nil slice yields a valid empty {@code Frames},
 * like gc.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class RuntimeCallersFramesNode extends GoExprNode {

    @Child private GoExprNode pcs;

    private RuntimeCallersFramesNode(GoExprNode pcs) {
        super(GoKind.POINTER);
        this.pcs = pcs;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object v = pcs.executeObject(frame);
        long[] ids;
        if (v instanceof GoSlice s) {
            Object arr = s.array();
            if (!(arr instanceof long[] longs)) {
                throw new FrontendException(
                        "runtimecallersframes: []uintptr backing is not long[]");
            }
            ids = Arrays.copyOfRange(longs, s.offset(), s.offset() + s.len());
        } else {
            ids = new long[0];
        }
        return new GoPointer(new GoFrames(ids));
    }

    /**
     * Builds the node from the {@code "runtimecallersframes"} wire node.
     * Fields: {@code pcs} (expression node), {@code type} (result
     * type-table id — {@code *runtime.Frames}, a pointer kind).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
                                   NodeFactory factory) {
        Json.Object f = node.object("fields", "runtimecallersframes");
        String path = "runtimecallersframes.fields";
        Json.Value pv = f.opt("pcs");
        if (!(pv instanceof Json.Object pcsObj)) {
            throw new FrontendException(
                    path + ": runtimecallersframes needs a \"pcs\" node object");
        }
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(
                    path + ": runtimecallersframes needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        if (NodeFactory.kindOf(ctx, typeId, path) != GoKind.POINTER) {
            throw new FrontendException(
                    path + ": runtimecallersframes result must be a pointer kind");
        }
        return new RuntimeCallersFramesNode(factory.buildExpr(pcsObj, ctx));
    }
}
