package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.Intrinsics;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;

// verifies: GG-0020 (WS G builtin)
/**
 * The {@code copy} builtin (WS G; verifies GG-0020).
 *
 * <p>{@code copy(dst, src)} copies {@code min(len(dst), len(src))} elements
 * from {@code src} to {@code dst} and returns the count as a Go {@code int}.
 * Overlapping slices are handled (memmove semantics). The destination slice
 * header is not modified.
 */
public final class CopyNode extends GoExprNode {
    @Child private GoExprNode dstChild;
    @Child private GoExprNode srcChild;
    private final GoKind elemKind;

    public CopyNode(GoExprNode dstChild, GoExprNode srcChild, GoKind elemKind) {
        super(GoKind.INT64);
        this.dstChild = dstChild;
        this.srcChild = srcChild;
        this.elemKind = elemKind;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        GoSlice dst = (GoSlice) dstChild.executeObject(frame);
        GoSlice src = (GoSlice) srcChild.executeObject(frame);
        return Intrinsics.slicecopy(dst, src, elemKind);
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return (int) executeLong(frame);
    }
}
