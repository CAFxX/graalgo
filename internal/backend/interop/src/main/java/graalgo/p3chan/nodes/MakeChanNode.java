package graalgo.p3chan.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.GoChan;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;
import graalgo.values.GoString;

/**
 * {@code make(chan T)} / {@code make(chan T, n)} (P3-M5, WS E, ADR-0005
 * Appendix C): the {@code "makechan"} wire node.
 *
 * <p>Construction mirrors the Phase-2 tests exactly:
 * {@code new GoChan(capacity, zeroValue)} — unbuffered (capacity 0) when
 * the wire carries no {@code size}. The element zero value is the
 * type-directed zero of the {@code elem} type-table id (so recv on a
 * closed, drained channel observes Go's zero value, per the
 * {@code ChanOps.makeChan} contract).
 *
 * <p>gc parity: a negative size panics ({@code "makechan: negative
 * size"}); a size beyond int range panics ({@code "makechan: size out of
 * range"}).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1/Phase-2 change.
 *
 * <p>verifies: GG-0017 (chan slice)
 */
public final class MakeChanNode extends GoExprNode {

    @Child
    private GoExprNode size;
    private final Object zeroValue;

    private MakeChanNode(GoExprNode size, Object zeroValue) {
        super(GoKind.CHAN);
        this.size = size;
        this.zeroValue = zeroValue;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        long n = size == null ? 0L : size.executeLong(frame);
        if (n < 0) {
            Panic.throwPanic(GoString.fromUtf8("makechan: negative size"));
        }
        if (n > Integer.MAX_VALUE) {
            Panic.throwPanic(GoString.fromUtf8("makechan: size out of range"));
        }
        return new GoChan((int) n, zeroValue);
    }

    /**
     * Builds the node from the {@code "makechan"} wire node. Fields:
     * {@code elem} (element type-table id), {@code size} (optional buffer
     * capacity: literal number or int-valued expression node),
     * {@code type} (optional channel type-table id — validated when
     * present, never used for codegen).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "makechan");
        String path = "makechan.fields";
        int elemId = ChanWire.requireElemId(f, path);
        Json.Value tv = f.opt("type");
        if (tv instanceof Json.Num num) {
            int chanId = (int) num.asLong(path + ".type");
            Json.Object entry = ctx.types.entry(chanId, path);
            if (!"chan".equals(entry.string("kind", path))) {
                throw new FrontendException(path + ": makechan type must be a chan, got "
                        + entry.string("kind", path));
            }
            int entryElem = entry.object("detail", path).intValue("elem", path);
            if (entryElem != elemId) {
                throw new FrontendException(path + ": makechan type/elem mismatch: type "
                        + chanId + " has elem " + entryElem + " but elem field is " + elemId);
            }
        } else if (tv != null) {
            throw new FrontendException(path + ": makechan type must be a numeric type id");
        }
        Object zero;
        try {
            zero = ChanWire.zeroTemplate(ctx, elemId, path);
        } catch (FrontendException e) {
            throw new FrontendException(path + ": bad makechan elem type: " + e.getMessage());
        }
        GoExprNode size = ChanWire.sizeNode(f, factory, ctx, path);
        return new MakeChanNode(size, zero);
    }
}
