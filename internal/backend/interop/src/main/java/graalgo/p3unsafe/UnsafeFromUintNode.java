package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;

/**
 * {@code uintptr → unsafe.Pointer} (P3-M8): the {@code "unsafefromuint"}
 * wire node.
 *
 * <p>D5: a handle can never be forged from an arbitrary integer. However,
 * a uintptr that <em>is</em> a currently-live handle value materializes
 * that handle — this is the legitimate round-trip pattern (e.g.
 * {@code abi.NoEscape}'s {@code unsafe.Pointer(uintptr(p) ^ 0)}, which is
 * identity on the handle value). Any other integer traps.
 * The operand still evaluates (gc evaluates the argument).
 */
public final class UnsafeFromUintNode extends GoExprNode {
    @Child private GoExprNode child;

    private UnsafeFromUintNode(GoExprNode child) {
        super(GoKind.UNSAFE_POINTER);
        this.child = child;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        long x = child.executeLong(frame);
        // D5 refinement: a uintptr that IS a currently-live handle value
        // materializes that handle. This allows legitimate round-trips
        // (e.g. abi.NoEscape's unsafe.Pointer(uintptr(p) ^ 0), which is
        // identity on the handle) while still trapping forged integers:
        // a handle can never be materialized from an arbitrary integer,
        // only from a value that is already a live handle.
        // Handle 0 (nil) is always live.
        if (x == 0 || isLiveHandle(x)) {
            return x;
        }
        UnsafeOps.fromUintTrap();
        throw new AssertionError("unreachable");
    }

    private static boolean isLiveHandle(long handle) {
        try {
            UnsafeTable.current().resolve(handle);
            return true;
        } catch (graalgo.values.UnsafeHandles.UnsafeEscapeException e) {
            return false;
        }
    }

    /**
     * Builds the node from the {@code "unsafefromuint"} wire node. Fields:
     * {@code x} (the uintptr operand), {@code type} (result type-table id —
     * the {@code unsafe.Pointer} type).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafefromuint");
        int typeId = UnsafeWire.requireTypeId(f, "unsafefromuint");
        GoExprNode n = new UnsafeFromUintNode(
                UnsafeWire.child(f, "unsafefromuint", "x", factory, ctx));
        n.setTypeId(typeId);
        return n;
    }
}
