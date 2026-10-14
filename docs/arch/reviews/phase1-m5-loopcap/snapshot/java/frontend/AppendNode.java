package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.Intrinsics;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;

// verifies: GG-0020 (WS G builtin)
/**
 * The {@code append} builtin (WS G; verifies GG-0020).
 *
 * <p>{@code append(s, x1, ..., xn)} grows {@code s} by {@code n} elements
 * with gc's growth policy ({@link Intrinsics#growslice}) and stores the new
 * elements. The slice header is a value: the result is a new
 * {@link GoSlice}; the input slice is never mutated.
 *
 * <p>Element reads use the execute method matching {@code elemKind}; stores
 * go through the typed Java arrays (D5).
 */
public final class AppendNode extends GoExprNode {
    @Child private GoExprNode sliceChild;
    @Children private GoExprNode[] elemChildren;
    private final GoKind elemKind;

    public AppendNode(GoExprNode sliceChild, GoKind elemKind, GoExprNode[] elemChildren) {
        super(GoKind.SLICE);
        this.sliceChild = sliceChild;
        this.elemKind = elemKind;
        this.elemChildren = elemChildren;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        GoSlice s = (GoSlice) sliceChild.executeObject(frame);
        int n = elemChildren.length;
        GoSlice out = Intrinsics.growslice(s, elemKind, n);
        Object arr = out.array();
        int base = out.offset() + s.len();
        for (int i = 0; i < n; i++) {
            store(arr, base + i, readElem(elemChildren[i], frame));
        }
        return out;
    }

    private Object readElem(GoExprNode child, VirtualFrame frame) {
        return switch (elemKind.slotKind()) {
            case Int -> child.executeInt(frame);
            case Long -> child.executeLong(frame);
            case Float -> child.executeFloat(frame);
            case Double -> child.executeDouble(frame);
            case Boolean -> child.executeBoolean(frame);
            case Object -> child.executeObject(frame);
            default -> throw new FrontendException("append of " + elemKind + " not supported");
        };
    }

    private void store(Object arr, int idx, Object v) {
        switch (elemKind) {
            case INT8 -> ((int[]) arr)[idx] = (Byte) v;
            case INT16 -> ((int[]) arr)[idx] = (Short) v;
            case INT32 -> ((int[]) arr)[idx] = (Integer) v;
            case UINT8 -> ((int[]) arr)[idx] = ((Byte) v) & 0xFF;
            case UINT16 -> ((int[]) arr)[idx] = ((Short) v) & 0xFFFF;
            case UINT32 -> ((int[]) arr)[idx] = (Integer) v;
            case INT64 -> ((long[]) arr)[idx] = (Long) v;
            case UINT64 -> ((long[]) arr)[idx] = (Long) v;
            case FLOAT32 -> ((float[]) arr)[idx] = (Float) v;
            case FLOAT64 -> ((double[]) arr)[idx] = (Double) v;
            case BOOL -> ((boolean[]) arr)[idx] = (Boolean) v;
            default -> ((Object[]) arr)[idx] = v;
        }
    }
}
