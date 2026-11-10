package graalgo.frontend;

import java.util.List;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.values.GoKind;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * R22-F-3: {@link AppendNode} must deep-copy struct/array elements on
 * store — {@code append(s, v)} followed by {@code t[0].F = 1} must not
 * mutate {@code v}. Covers the in-capacity fast path, the growslice path,
 * and the ellipsis slice-spread path.
 *
 * <p>RED (pre-fix): {@code store()} stores the element object directly,
 * so the appended element aliases the caller's value.
 * Registered in {@code graalgo.test.Runner}.
 */
public final class ValueCopyBulkTest {

    /** Descriptor for struct{F int64}: one long field at index 0. */
    private static GoStructDescriptor structDesc() {
        return new GoStructDescriptor("S", List.of(
                new GoStructDescriptor.Field("F", GoKind.INT64, true, 0, -1, null, null, null)));
    }

    private static GoStruct structWith(long f) {
        return new GoStruct(structDesc(), new long[]{f}, new Object[0]);
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /** A GoExprNode evaluating to a constant object. */
    static final class ConstNode extends GoExprNode {
        private final Object objVal;

        ConstNode(GoKind kind, Object objVal) {
            super(kind);
            this.objVal = objVal;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return objVal;
        }
    }

    private static VirtualFrame frame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0],
                FrameDescriptor.newBuilder().build());
    }

    /**
     * In-capacity append: the element is stored into the shared backing
     * array, so the store itself must copy — sharing the backing is
     * correct, sharing the element object is not.
     */
    public void testAppendStoreCopiesStructInCapacity() {
        GoStruct v = structWith(1);
        Object[] backing = new Object[]{null, null};
        GoSlice s = new GoSlice(backing, 0, 0, 2);
        AppendNode n = new AppendNode(new ConstNode(GoKind.SLICE, s), GoKind.STRUCT,
                new GoExprNode[]{new ConstNode(GoKind.STRUCT, v)},
                false, 8, structDesc());
        GoSlice out = (GoSlice) n.executeObject(frame());
        assertTrue(out.len() == 1, "append result len is " + out.len());
        Object stored = ((Object[]) out.array())[0];
        assertTrue(stored != v,
                "AppendNode.store shared the struct element (R22-F-3)");
        ((GoStruct) stored).setLong(0, 99);
        assertTrue(v.getLong(0) == 1,
                "mutating the appended element mutated the source (R22-F-3): F="
                        + v.getLong(0));
    }

    /**
     * Growing append: the element is stored after growslice reallocates.
     * Both the growslice copy of the old elements and the store of the new
     * element must detach (the store is the defect pinned here).
     */
    public void testAppendStoreCopiesStructOnGrowth() {
        GoStruct v = structWith(1);
        GoStruct old = structWith(7);
        Object[] backing = new Object[]{old};
        GoSlice s = new GoSlice(backing, 0, 1, 1);
        AppendNode n = new AppendNode(new ConstNode(GoKind.SLICE, s), GoKind.STRUCT,
                new GoExprNode[]{new ConstNode(GoKind.STRUCT, v)},
                false, 8, structDesc());
        GoSlice out = (GoSlice) n.executeObject(frame());
        assertTrue(out.len() == 2, "append result len is " + out.len());
        Object[] arr = (Object[]) out.array();
        assertTrue(arr[1] != v,
                "AppendNode.store shared the struct element on growth (R22-F-3)");
        ((GoStruct) arr[1]).setLong(0, 99);
        assertTrue(v.getLong(0) == 1,
                "mutating the appended element mutated the source (R22-F-3)");
        assertTrue(arr[0] != old,
                "growslice shared the old struct element (R22-F-3)");
    }

    /**
     * Ellipsis slice-spread: {@code append(dst, src...)} must copy the
     * spread struct elements — mutating the result must not touch src.
     */
    public void testAppendSpreadCopiesStructs() {
        GoStruct srcElem = structWith(1);
        GoSlice src = new GoSlice(new Object[]{srcElem}, 0, 1, 1);
        GoSlice dst = new GoSlice(new Object[]{null, null}, 0, 0, 2);
        AppendNode n = new AppendNode(new ConstNode(GoKind.SLICE, dst), GoKind.STRUCT,
                new GoExprNode[]{new ConstNode(GoKind.SLICE, src)},
                true, 8, structDesc());
        GoSlice out = (GoSlice) n.executeObject(frame());
        assertTrue(out.len() == 1, "append result len is " + out.len());
        Object stored = ((Object[]) out.array())[0];
        assertTrue(stored != srcElem,
                "append spread shared the struct element (R22-F-3)");
        ((GoStruct) stored).setLong(0, 99);
        assertTrue(srcElem.getLong(0) == 1,
                "mutating the spread element mutated the source (R22-F-3)");
    }

    /**
     * Array elements are values too: appending a [2]int element must copy
     * the inner array.
     */
    public void testAppendStoreCopiesArray() {
        int[] arr = new int[]{1, 2};
        Object[] backing = new Object[]{null, null};
        GoSlice s = new GoSlice(backing, 0, 0, 2);
        AppendNode n = new AppendNode(new ConstNode(GoKind.SLICE, s), GoKind.ARRAY,
                new GoExprNode[]{new ConstNode(GoKind.ARRAY, arr)},
                false, 8, null);
        GoSlice out = (GoSlice) n.executeObject(frame());
        Object stored = ((Object[]) out.array())[0];
        assertTrue(stored != arr,
                "AppendNode.store shared the array element (R22-F-3)");
        ((int[]) stored)[0] = 99;
        assertTrue(arr[0] == 1,
                "mutating the appended array mutated the source (R22-F-3)");
    }
}
