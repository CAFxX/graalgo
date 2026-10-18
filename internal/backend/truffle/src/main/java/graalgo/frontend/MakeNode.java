package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoErrors;
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
 * <p>Slice allocation uses {@link Intrinsics#makeslice}. Out-of-range
 * len/cap panic here with gc's messages (gc parity): negative len/cap or
 * len > cap panic "makeslice: len out of range"; len/cap beyond
 * {@link Integer#MAX_VALUE} — which no Java array can address — panic
 * "makeslice: len/cap out of range" respectively. The range checks precede
 * the {@code (int)} narrowing so a huge len/cap can never silently wrap
 * (RV18-P1-13).
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
            Panic.throwPanic(GoErrors.makesliceLenOutOfRange(), null, null, this);
        }
        if (len > Integer.MAX_VALUE) {
            // RV18-P1-13: a Java array cannot hold more than
            // Integer.MAX_VALUE elements, so the (int) narrowing below
            // would silently wrap (e.g. make([]byte, 1<<40) -> an empty
            // slice). gc panics "makeslice: len out of range" once len
            // exceeds its maxAlloc; the unrepresentable range must panic
            // loudly here too. (Hot path cost: two predictable branches.)
            Panic.throwPanic(GoErrors.makesliceLenOutOfRange(), null, null, this);
        }
        if (cap > Integer.MAX_VALUE) {
            // gc: "makeslice: cap out of range" panic.
            Panic.throwPanic(GoErrors.makesliceCapOutOfRange(), null, null, this);
        }
        return Intrinsics.makeslice(elemKind, elemDesc, (int) len, (int) cap);
    }
}
