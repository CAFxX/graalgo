package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.Intrinsics;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;

// verifies: GG-0020 (WS G builtin)
/**
 * The {@code make} builtin (WS G; verifies GG-0020).
 *
 * <p>Forms:
 * <ul>
 *   <li>{@code make([]T, len)} — slice with len and cap = len;</li>
 *   <li>{@code make([]T, len, cap)} — slice with len and cap;</li>
 *   <li>{@code make(map[K]V)} / {@code make(map[K]V, hint)} — empty map
 *       (hint sizes the table);</li>
 *   <li>{@code make(chan T)} — Phase-2 scope error (no channels in Phase 1).</li>
 * </ul>
 *
 * <p>Slice allocation uses {@link Intrinsics#makeslice}. Negative len/cap
 * panic via the intrinsic (gc parity).
 */
public final class MakeNode extends GoExprNode {
    @Child private GoExprNode lenChild;
    @Child private GoExprNode capChild; // null when absent
    private final GoKind elemKind;
    private final boolean isMap;
    private final int mapHint; // -1 when absent

    /** Slice form. */
    public MakeNode(GoKind elemKind, GoExprNode lenChild, GoExprNode capChild) {
        super(GoKind.SLICE);
        this.elemKind = elemKind;
        this.lenChild = lenChild;
        this.capChild = capChild;
        this.isMap = false;
        this.mapHint = -1;
    }

    /** Map form. */
    public MakeNode(int mapHint) {
        super(GoKind.MAP);
        this.elemKind = null;
        this.lenChild = null;
        this.capChild = null;
        this.isMap = true;
        this.mapHint = mapHint;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        if (isMap) {
            return mapHint < 0 ? new GoMap() : new GoMap(mapHint);
        }
        long len = lenChild.executeLong(frame);
        long cap = capChild != null ? capChild.executeLong(frame) : len;
        if (len < 0 || cap < 0 || len > cap) {
            // gc: "makeslice: len out of range" panic.
            Panic.throwPanic("makeslice: len out of range");
        }
        return Intrinsics.makeslice(elemKind, (int) len, (int) cap);
    }
}
