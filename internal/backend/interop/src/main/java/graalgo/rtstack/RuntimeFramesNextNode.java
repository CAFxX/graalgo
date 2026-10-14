package graalgo.rtstack;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.GoFrames;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * {@code (*runtime.Frames).Next()}: the {@code "runtimeframesnext"} wire
 * node.
 *
 * <p>Returns the {@code framesNextPair{frame, more}} struct for the
 * multi-result desugar (mrRuntimeStack). gc semantics: {@code more}
 * reports whether the <em>next</em> call to {@code Next} will return a
 * valid frame; PCs that resolve to no known function are skipped, like
 * gc's {@code findfunc} misses; exhaustion yields the zero
 * {@code runtime.Frame} and {@code more=false}.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class RuntimeFramesNextNode extends GoExprNode {

    @Child private GoExprNode f;
    private final GoStructDescriptor pairDesc;
    private final GoStructDescriptor frameDesc;

    private RuntimeFramesNextNode(GoExprNode f, GoStructDescriptor pairDesc,
            GoStructDescriptor frameDesc) {
        super(GoKind.STRUCT);
        this.f = f;
        this.pairDesc = pairDesc;
        this.frameDesc = frameDesc;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        GoFrames gf = RtStack.derefFrames(f.executeObject(frame), this);
        GoStruct frameVal;
        boolean more;
        long pc = gf.nextValid();
        if (pc < 0) {
            frameVal = GoStruct.zero(frameDesc);
            more = false;
        } else {
            frameVal = RtStack.buildFrame(frameDesc, pc);
            more = gf.hasValidNext();
        }
        long[] longs = new long[pairDesc.longsCount()];
        Object[] objs = new Object[pairDesc.objectsCount()];
        objs[RtStack.objIndex(pairDesc, "frame", "runtimeframesnext")] = frameVal;
        longs[RtStack.longIndex(pairDesc, "more", "runtimeframesnext")] = more ? 1 : 0;
        return new GoStruct(pairDesc, longs, objs);
    }

    /**
     * Builds the node from the {@code "runtimeframesnext"} wire node.
     * Fields: {@code f} (the {@code *runtime.Frames} expression node),
     * {@code type} (result type-table id — the named
     * {@code framesNextPair} struct).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
                                   NodeFactory factory) {
        Json.Object f = node.object("fields", "runtimeframesnext");
        String path = "runtimeframesnext.fields";
        Json.Value fv = f.opt("f");
        if (!(fv instanceof Json.Object fObj)) {
            throw new FrontendException(path + ": runtimeframesnext needs an \"f\" node object");
        }
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(
                    path + ": runtimeframesnext needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        if (NodeFactory.kindOf(ctx, typeId, path) != GoKind.STRUCT) {
            throw new FrontendException(
                    path + ": runtimeframesnext result must be a struct kind");
        }
        GoStructDescriptor pairDesc = NodeFactory.structDescriptorFor(ctx, typeId, path);
        int frameTypeId = RtStack.structFieldTypeId(ctx, typeId, "frame", path);
        GoStructDescriptor frameDesc =
                NodeFactory.structDescriptorFor(ctx, frameTypeId, path);
        return new RuntimeFramesNextNode(factory.buildExpr(fObj, ctx), pairDesc, frameDesc);
    }
}
