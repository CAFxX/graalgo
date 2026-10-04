package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Composite literal (M4, WS E): slice, array, map, struct, and pointer-to-
 * struct ({@code &T{...}}, incl. the implicit {@code &T{}} element elision
 * — the exporter resolves elided types, so the wire always carries the
 * explicit type).
 *
 * <p>Every evaluation builds a fresh value: keyed literals fill missing
 * positions/fields with fresh zero values (never shared template objects).
 */
public final class CompositeLitNode extends GoExprNode {
    @Children private GoExprNode[] keyNodes; // null unless keyed map/array/slice
    @Children private GoExprNode[] valueNodes;
    /** Field indices parallel to {@link #valueNodes} for keyed struct literals; null otherwise. */
    private final int[] fieldIdx;
    private final boolean keyed;
    private final boolean isPointer;
    // Slice/array/map element + map key kinds (resolved at build).
    private final GoKind elemKind;
    private final GoKind keyKind;
    private final int arrayLen;
    private final GoStructDescriptor structDesc;
    /** Per-field zero templates (struct/pointer kinds), parallel to descriptor fields. */
    private final Object[] fieldZeroTemplates;
    /** Zero template for array/slice elements (struct/array element kinds). */
    private final Object elemZeroTemplate;

    public CompositeLitNode(GoKind kind, GoExprNode[] keyNodes, int[] fieldIdx,
                            GoExprNode[] valueNodes, boolean keyed, boolean isPointer,
                            GoKind elemKind, GoKind keyKind, int arrayLen,
                            GoStructDescriptor structDesc, Object[] fieldZeroTemplates,
                            Object elemZeroTemplate) {
        super(kind);
        this.keyNodes = keyNodes;
        this.fieldIdx = fieldIdx;
        this.valueNodes = valueNodes;
        this.keyed = keyed;
        this.isPointer = isPointer;
        this.elemKind = elemKind;
        this.keyKind = keyKind;
        this.arrayLen = arrayLen;
        this.structDesc = structDesc;
        this.fieldZeroTemplates = fieldZeroTemplates;
        this.elemZeroTemplate = elemZeroTemplate;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return switch (kind()) {
            case SLICE -> buildSlice(frame);
            case ARRAY -> buildArray(frame);
            case MAP -> buildMap(frame);
            case STRUCT -> buildStruct(frame);
            case POINTER -> new GoPointer(buildStruct(frame));
            default -> throw new FrontendException("composite literal of kind " + kind() + " (WS E)");
        };
    }

    private Object buildSlice(VirtualFrame frame) {
        int n = valueNodes.length;
        if (keyed) {
            int max = -1;
            for (GoExprNode k : keyNodes) {
                max = Math.max(max, (int) k.executeLong(frame));
            }
            n = max + 1;
        }
        Object arr = Boxing.newArray(elemKind, n);
        fillArray(arr, n, frame);
        return new GoSlice(arr, 0, n, n);
    }

    private Object buildArray(VirtualFrame frame) {
        Object arr = Boxing.newArray(elemKind, arrayLen);
        fillArray(arr, arrayLen, frame);
        return arr;
    }

    private void fillArray(Object arr, int n, VirtualFrame frame) {
        if (!keyed) {
            for (int i = 0; i < valueNodes.length; i++) {
                Boxing.arraySet(arr, elemKind, i, elemValue(frame, i));
            }
            // Missing elements: fresh zeros for composite element kinds.
            for (int i = valueNodes.length; i < n; i++) {
                if (elemZeroTemplate != null) {
                    Boxing.arraySet(arr, elemKind, i, Boxing.freshZeroCopy(elemZeroTemplate));
                }
            }
            return;
        }
        boolean[] set = new boolean[n];
        for (int j = 0; j < valueNodes.length; j++) {
            int idx = (int) keyNodes[j].executeLong(frame);
            if (idx < 0 || idx >= n) {
                // Unreachable via valid Go (go/types rejects out-of-range
                // literal indices at compile time); gc-exact shape anyway.
                Panic.throwPanic(GoErrors.indexOutOfRange(idx, n, true));
            }
            Boxing.arraySet(arr, elemKind, idx, elemValue(frame, j));
            set[idx] = true;
        }
        for (int i = 0; i < n; i++) {
            if (!set[i] && elemZeroTemplate != null) {
                Boxing.arraySet(arr, elemKind, i, Boxing.freshZeroCopy(elemZeroTemplate));
            }
        }
    }

    /** One element value: boxed, through the value-copy rule. */
    private Object elemValue(VirtualFrame frame, int j) {
        return Boxing.copyForAssign(elemKind, Boxing.box(elemKind, valueNodes[j], frame));
    }

    private Object buildMap(VirtualFrame frame) {
        GoMap m = new GoMap(Math.max(8, valueNodes.length * 2));
        for (int j = 0; j < valueNodes.length; j++) {
            Object k = MapKeys.box(keyKind, keyNodes[j], frame);
            Object v = Boxing.copyForAssign(elemKind, Boxing.box(elemKind, valueNodes[j], frame));
            m.put(k, v);
        }
        return m;
    }

    private GoStruct buildStruct(VirtualFrame frame) {
        long[] longs = new long[structDesc.longsCount()];
        Object[] objects = new Object[structDesc.objectsCount()];
        // Missing fields start at their zero value (fresh per evaluation).
        for (int i = 0; i < structDesc.fields().size(); i++) {
            GoStructDescriptor.Field f = structDesc.field(i);
            if (!f.inLongs() && fieldZeroTemplates[i] != null) {
                objects[f.index()] = Boxing.freshZeroCopy(fieldZeroTemplates[i]);
            } else if (!f.inLongs() && f.kind() == GoKind.STRING) {
                objects[f.index()] = graalgo.values.GoString.EMPTY;
            }
        }
        for (int j = 0; j < valueNodes.length; j++) {
            int fi = keyed ? fieldIdx[j] : j;
            GoStructDescriptor.Field f = structDesc.field(fi);
            Object b = Boxing.box(f.kind(), valueNodes[j], frame);
            if (f.inLongs()) {
                longs[f.index()] = Boxing.toLongBits(f.kind(), b);
            } else {
                objects[f.index()] = Boxing.copyForAssign(f.kind(), b);
            }
        }
        return new GoStruct(structDesc, longs, objects);
    }
}
