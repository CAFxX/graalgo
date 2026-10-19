package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.Intrinsics;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;
import graalgo.values.GoString;

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
    /**
     * Element descriptor for struct kinds (P3-M9): makeslice zero-initializes
     * struct elements; null for non-struct kinds.
     */
    private final graalgo.values.GoStructDescriptor elemDesc;

    /** Slice form. */
    public MakeNode(GoKind elemKind, GoExprNode lenChild, GoExprNode capChild,
            graalgo.values.GoStructDescriptor elemDesc) {
        super(GoKind.SLICE);
        this.elemKind = elemKind;
        this.lenChild = lenChild;
        this.capChild = capChild;
        this.elemDesc = elemDesc;
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
        this.elemDesc = null;
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
            // D5: Panic.throwPanic takes the Go value in its D5 mapping — a raw
            // java.lang.String is not a Go string, and with a null descriptor
            // recover() would hand the value back unboxed, ClassCastExceptioning
            // GoInterface.narrow inside TypeAssertNode (RV-P0-06 family). Wrap in
            // GoString and pass the canonical predeclared-string descriptor (the
            // same identity NodeFactory.typeDescriptorFor uses for `string`, via
            // the process-wide basicDescriptorForName cache) so recover() boxes
            // it as a GoInterface and r.(string) holds.
            Panic.throwPanic(GoString.fromUtf8("makeslice: len out of range"),
                    NodeFactory.basicDescriptorForName("string", GoKind.STRING),
                    null, this);
        }
        return Intrinsics.makeslice(elemKind, elemDesc, (int) len, (int) cap);
    }
}
