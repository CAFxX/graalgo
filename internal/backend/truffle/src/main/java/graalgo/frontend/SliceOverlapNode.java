package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;

/**
 * The {@code crypto/internal/fips140/alias.AnyOverlap} and
 * {@code InexactOverlap} intrinsics (P3-M9, unsafe patterns).
 *
 * <p>The real stdlib implements these via
 * {@code uintptr(unsafe.Pointer(...))} address arithmetic, which is
 * meaningless on D5 identity tokens (GG-0015/GG-0016: handles are not
 * addresses and admit no byte arithmetic — the token carries identity
 * only). This node implements the overlap detection directly on the
 * {@link GoSlice} values: two slices overlap iff they share the same
 * backing array object and their {@code [offset, offset+len)} ranges
 * intersect.
 *
 * <p>For {@code InexactOverlap}, additionally returns false when
 * {@code &x[0] == &y[0]} (same start address), per the stdlib spec.
 */
public final class SliceOverlapNode extends GoExprNode {
    @Child private GoExprNode xChild;
    @Child private GoExprNode yChild;
    private final boolean inexact;

    public SliceOverlapNode(GoExprNode xChild, GoExprNode yChild, boolean inexact) {
        super(GoKind.BOOL);
        this.xChild = xChild;
        this.yChild = yChild;
        this.inexact = inexact;
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        Object xObj = xChild.executeObject(frame);
        Object yObj = yChild.executeObject(frame);
        // Nil slices never overlap (len == 0).
        if (xObj == null || yObj == null) {
            return false;
        }
        GoSlice x = (GoSlice) xObj;
        GoSlice y = (GoSlice) yObj;
        if (x.len() == 0 || y.len() == 0) {
            return false;
        }
        // Different backing arrays: no overlap.
        if (x.array() != y.array()) {
            return false;
        }
        int xStart = x.offset();
        int xEnd = xStart + x.len();
        int yStart = y.offset();
        int yEnd = yStart + y.len();
        // InexactOverlap: false when &x[0] == &y[0].
        if (inexact && xStart == yStart) {
            return false;
        }
        // Ranges [xStart, xEnd) and [yStart, yEnd) intersect?
        return xStart < yEnd && yStart < xEnd;
    }
}
