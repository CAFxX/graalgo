package graalgo.p3time.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.p3time.TimeDescriptors;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * {@code time.Now()} (P3-M5, WS E, ADR-0005 D1): the {@code "timenow"}
 * wire node, evaluating to a {@code time.Time} ({@code struct{ unixnano
 * int64}}) built from the wall clock.
 *
 * <p>The descriptor is the canonical one resolved by
 * {@link TimeDescriptors} (descriptor identity is struct type identity),
 * so this value compares and boxes exactly like factory-built Times.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class TimeNowNode extends GoExprNode {

    private final GoStructDescriptor timeDesc;

    private TimeNowNode(GoStructDescriptor timeDesc) {
        super(GoKind.STRUCT);
        this.timeDesc = timeDesc;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        // Wall clock, gc time.Now parity (the synthetic Time carries no
        // monotonic strip — unixnano only, per ADR-0005 D1).
        return new GoStruct(timeDesc,
                new long[]{System.currentTimeMillis() * 1_000_000L}, new Object[0]);
    }

    /**
     * Builds the node from the {@code "timenow"} wire node. Fields:
     * {@code type} (type-table id — the named {@code time.Time}).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "timenow");
        String path = "timenow.fields";
        int typeId = TimeWire.requireTypeId(f, "timenow");
        Json.Object entry = ctx.types.entry(typeId, path);
        if (!"named".equals(entry.string("kind", path))
                || !"time.Time".equals(entry.object("detail", path).string("name", path))) {
            throw new FrontendException(path + ": timenow result must be the named time.Time");
        }
        return new TimeNowNode(TimeDescriptors.resolve(ctx, path));
    }
}
