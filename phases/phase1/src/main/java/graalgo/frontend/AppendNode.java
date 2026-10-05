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
    /** True for append(s, x...) where x is a string (spread its bytes). */
    private final boolean ellipsis;

    public AppendNode(GoExprNode sliceChild, GoKind elemKind, GoExprNode[] elemChildren) {
        this(sliceChild, elemKind, elemChildren, false);
    }

    public AppendNode(GoExprNode sliceChild, GoKind elemKind, GoExprNode[] elemChildren,
            boolean ellipsis) {
        super(GoKind.SLICE);
        this.sliceChild = sliceChild;
        this.elemKind = elemKind;
        this.elemChildren = elemChildren;
        this.ellipsis = ellipsis;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        GoSlice s = (GoSlice) sliceChild.executeObject(frame);
        // M7: append(s, x...) spreads x's elements. Two cases:
        // - string spread (append([]byte, str...)): append the string's bytes.
        // - slice spread (append(s1, s2...)): append s2's elements.
        if (ellipsis) {
            if (elemChildren.length != 1) {
                throw new FrontendException("append with ellipsis takes exactly one spread argument");
            }
            GoExprNode spreadNode = elemChildren[0];
            if (spreadNode.kind() == GoKind.STRING || spreadNode.kind() == GoKind.UNTYPED_STRING) {
                graalgo.values.GoString str = (graalgo.values.GoString) spreadNode.executeObject(frame);
                byte[] bytes = str.bytes();
                int n = bytes.length;
                GoSlice out;
                if (s != null && n <= s.cap() - s.len()) {
                    out = new GoSlice(s.array(), s.offset(), s.len() + n, s.cap());
                } else {
                    out = Intrinsics.growslice(s, elemKind, n);
                }
                Object arr = out.array();
                int base = out.offset() + (s == null ? 0 : s.len());
                int[] intArr = (int[]) arr;
                for (int i = 0; i < n; i++) {
                    intArr[base + i] = bytes[i] & 0xFF;
                }
                return out;
            } else {
                // Slice spread: append the slice's elements.
                GoSlice spread = (GoSlice) spreadNode.executeObject(frame);
                int n = spread == null ? 0 : (int) spread.len();
                GoSlice out;
                if (s != null && n <= s.cap() - s.len()) {
                    out = new GoSlice(s.array(), s.offset(), s.len() + n, s.cap());
                } else {
                    out = Intrinsics.growslice(s, elemKind, n);
                }
                Object arr = out.array();
                int base = out.offset() + (s == null ? 0 : s.len());
                Object spreadArr = spread == null ? null : spread.array();
                int spreadBase = spread == null ? 0 : spread.offset();
                for (int i = 0; i < n; i++) {
                    Object elem = Boxing.arrayGet(spreadArr, elemKind, spreadBase + i);
                    Boxing.arraySet(arr, elemKind, base + i, elem);
                }
                return out;
            }
        }
        int n = elemChildren.length;
        // M7: fast path — if the slice has capacity, extend the header
        // (shares the backing array), exactly like gc's append. Only call
        // growslice when len+n exceeds cap; unconditional growth
        // over-allocates (bi01: caps 16,256,7168 vs gc's 8,16,32).
        GoSlice out;
        if (s != null && n <= s.cap() - s.len()) {
            out = new GoSlice(s.array(), s.offset(), s.len() + n, s.cap());
        } else {
            out = Intrinsics.growslice(s, elemKind, n);
        }
        Object arr = out.array();
        // M7: appending to a nil slice is legal (treat as empty); growslice
        // already handles the null input, but the element base must not
        // dereference it.
        int base = out.offset() + (s == null ? 0 : s.len());
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
        // M7: readElem returns Integer for all Int slot kinds (executeInt);
        // the old (Byte)/(Short) casts assumed boxed small types and threw
        // ClassCastException. Convert via Number instead.
        switch (elemKind) {
            case INT8 -> ((int[]) arr)[idx] = ((Number) v).byteValue();
            case INT16 -> ((int[]) arr)[idx] = ((Number) v).shortValue();
            case INT32 -> ((int[]) arr)[idx] = ((Number) v).intValue();
            case UINT8 -> ((int[]) arr)[idx] = ((Number) v).intValue() & 0xFF;
            case UINT16 -> ((int[]) arr)[idx] = ((Number) v).intValue() & 0xFFFF;
            case UINT32 -> ((int[]) arr)[idx] = ((Number) v).intValue();
            case INT64 -> ((long[]) arr)[idx] = (Long) v;
            case UINT64 -> ((long[]) arr)[idx] = (Long) v;
            case FLOAT32 -> ((float[]) arr)[idx] = (Float) v;
            case FLOAT64 -> ((double[]) arr)[idx] = (Double) v;
            case BOOL -> ((boolean[]) arr)[idx] = (Boolean) v;
            default -> ((Object[]) arr)[idx] = v;
        }
    }
}
