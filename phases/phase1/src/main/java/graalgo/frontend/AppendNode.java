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
    /**
     * Element size in bytes for the growslice math (P3-M4): for structs,
     * the ABI size from the descriptor (0 = unknown → the zero-size
     * path, which is only correct for actual zero-size types).
     */
    private final long elemSizeBytes;

    public AppendNode(GoExprNode sliceChild, GoKind elemKind, GoExprNode[] elemChildren) {
        this(sliceChild, elemKind, elemChildren, false, 0);
    }

    public AppendNode(GoExprNode sliceChild, GoKind elemKind, GoExprNode[] elemChildren,
            boolean ellipsis) {
        this(sliceChild, elemKind, elemChildren, ellipsis, 0);
    }

    public AppendNode(GoExprNode sliceChild, GoKind elemKind, GoExprNode[] elemChildren,
            boolean ellipsis, long elemSizeBytes) {
        super(GoKind.SLICE);
        this.sliceChild = sliceChild;
        this.elemKind = elemKind;
        this.elemChildren = elemChildren;
        this.ellipsis = ellipsis;
        this.elemSizeBytes = elemSizeBytes;
    }

    /** Effective element size for growslice: the struct ABI size when
     * supplied, else the kind's natural size. */
    private long effectiveElemSize() {
        return elemSizeBytes != 0 ? elemSizeBytes : Intrinsics.elemSizeBytes(elemKind);
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
                // Remediation (R1 P1-2): append with no elements is the
                // identity — gc returns s unchanged, preserving nil-ness.
                if (n == 0) {
                    return s;
                }
                GoSlice out;
                if (s != null && n <= s.cap() - s.len()) {
                    out = new GoSlice(s.array(), s.offset(), s.len() + n, s.cap());
                } else {
                    int newLen1 = (s == null ? 0 : s.len()) + n;
                    out = Intrinsics.growslice(s, elemKind, newLen1, effectiveElemSize(),
                            Intrinsics.elemHasPointers(elemKind));
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
                // Remediation (R1 P1-2): append with no elements is the
                // identity — gc returns s unchanged, preserving nil-ness.
                if (n == 0) {
                    return s;
                }
                GoSlice out;
                if (s != null && n <= s.cap() - s.len()) {
                    out = new GoSlice(s.array(), s.offset(), s.len() + n, s.cap());
                } else {
                    int newLen1 = (s == null ? 0 : s.len()) + n;
                    out = Intrinsics.growslice(s, elemKind, newLen1, effectiveElemSize(),
                            Intrinsics.elemHasPointers(elemKind));
                }
                Object arr = out.array();
                int base = out.offset() + (s == null ? 0 : s.len());
                Object spreadArr = spread.array();
                int spreadBase = spread.offset();
                // Remediation (R2 P0-1): overlap-safe copy. When the spread
                // aliases out's backing array (e.g. append(s[:2], s[:8]...)),
                // a forward element loop overwrites not-yet-read source
                // elements; gc has memmove semantics here.
                if (spreadArr.getClass().getComponentType()
                        .equals(arr.getClass().getComponentType())) {
                    Intrinsics.memmove(arr, base, spreadArr, spreadBase, n);
                } else {
                    // Defensive: different component types cannot alias the
                    // same array; snapshot through a temp buffer anyway.
                    Object[] tmp = new Object[n];
                    for (int i = 0; i < n; i++) {
                        tmp[i] = Boxing.arrayGet(spreadArr, elemKind, spreadBase + i);
                    }
                    for (int i = 0; i < n; i++) {
                        Boxing.arraySet(arr, elemKind, base + i, tmp[i]);
                    }
                }
                return out;
            }
        }
        int n = elemChildren.length;
        // Remediation (R1 P1-2): append(s) with no elements is the identity —
        // gc returns s unchanged, preserving nil-ness.
        if (n == 0) {
            return s;
        }
        // Remediation (class hunt from R2 P0-1): Go evaluates all call
        // arguments before the builtin runs. The old loop interleaved
        // element reads and stores, so append(s, t[3], t[2]) with t sharing
        // s's backing array read the just-written value (gc reads the
        // original). Evaluate every element value first, then grow, then
        // store.
        Object[] vals = new Object[n];
        for (int i = 0; i < n; i++) {
            vals[i] = readElem(elemChildren[i], frame);
        }
        // M7: fast path — if the slice has capacity, extend the header
        // (shares the backing array), exactly like gc's append. Only call
        // growslice when len+n exceeds cap; unconditional growth
        // over-allocates (bi01: caps 16,256,7168 vs gc's 8,16,32).
        GoSlice out;
        if (s != null && n <= s.cap() - s.len()) {
            out = new GoSlice(s.array(), s.offset(), s.len() + n, s.cap());
        } else {
            int newLen2 = (s == null ? 0 : s.len()) + n;
            out = Intrinsics.growslice(s, elemKind, newLen2, effectiveElemSize(),
                    Intrinsics.elemHasPointers(elemKind));
        }
        Object arr = out.array();
        // M7: appending to a nil slice is legal (treat as empty); growslice
        // already handles the null input, but the element base must not
        // dereference it.
        int base = out.offset() + (s == null ? 0 : s.len());
        for (int i = 0; i < n; i++) {
            store(arr, base + i, vals[i]);
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
            case INT64 -> ((long[]) arr)[idx] = ((Number) v).longValue();
            case UINT64 -> ((long[]) arr)[idx] = ((Number) v).longValue();
            case FLOAT32 -> ((float[]) arr)[idx] = (Float) v;
            case FLOAT64 -> ((double[]) arr)[idx] = (Double) v;
            case BOOL -> ((boolean[]) arr)[idx] = (Boolean) v;
            default -> ((Object[]) arr)[idx] = v;
        }
    }
}
