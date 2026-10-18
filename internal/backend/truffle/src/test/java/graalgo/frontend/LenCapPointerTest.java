package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Red-green for L27 (P1-23/24): {@code len(p)} / {@code cap(p)} on
 * {@code p *[N]T} must return N without touching the referent — gc returns
 * N even for a nil pointer, and rejects nothing at build time.
 *
 * <p>RED (pre-fix): {@code new LenNode(ptrChild).executeLong(null)} on a
 * nil pointer threw a host exception ({@code NullPointerException} from
 * {@code Array.getLength(null)}), and {@code new CapNode(ptrChild)}
 * refused to build at all ({@code FrontendException: cap of POINTER is not
 * implemented}).
 *
 * <p>GREEN (fixed): the pointed-to array length travels with the node from
 * construction (resolved by the factory from the static type), so nil and
 * non-nil pointers alike return N with no dereference. A POINTER node
 * built without a resolved length fails fast in the constructor.
 */
public final class LenCapPointerTest {

    /** POINTER-kind child returning the given pointer value (nil allowed). */
    private static final class PtrChild extends GoExprNode {
        private final GoPointer value;

        PtrChild(GoPointer value) {
            super(GoKind.POINTER);
            this.value = value;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return value;
        }
    }

    /**
     * POINTER-kind child with an observable side effect: gc evaluates the
     * builtin operand even though the length itself is static, so the node
     * must evaluate its child too.
     */
    private static final class SideEffectChild extends GoExprNode {
        int evaluations;

        SideEffectChild() {
            super(GoKind.POINTER);
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            evaluations++;
            return new GoPointer((Object) null);
        }
    }

    /** ARRAY-kind child returning the given Java array. */
    private static final class ArrayChild extends GoExprNode {
        private final Object value;

        ArrayChild(Object value) {
            super(GoKind.ARRAY);
            this.value = value;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return value;
        }
    }

    /** SLICE-kind child returning the given slice (null allowed). */
    private static final class SliceChild extends GoExprNode {
        private final GoSlice value;

        SliceChild(GoSlice value) {
            super(GoKind.SLICE);
            this.value = value;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return value;
        }
    }

    /** STRING-kind child returning the given string. */
    private static final class StringChild extends GoExprNode {
        private final GoString value;

        StringChild(GoString value) {
            super(GoKind.STRING);
            this.value = value;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return value;
        }
    }

    private static GoPointer nilPointer() {
        return new GoPointer((Object) null);
    }

    private static void checkEq(long expected, long actual, String what) {
        if (expected != actual) {
            throw new AssertionError(what + ": expected " + expected + " but got " + actual);
        }
    }

    public void testLenNilPointerToArray() {
        checkEq(5, new LenNode(new PtrChild(nilPointer()), 5).executeLong(null),
                "len(nil *[5]int)");
    }

    public void testCapNilPointerToArray() {
        checkEq(5, new CapNode(new PtrChild(nilPointer()), 5).executeLong(null),
                "cap(nil *[5]int)");
    }

    public void testLenNonNilPointerToArray() {
        checkEq(5, new LenNode(new PtrChild(new GoPointer(new int[5])), 5).executeLong(null),
                "len(non-nil *[5]int)");
    }

    public void testCapNonNilPointerToArray() {
        checkEq(5, new CapNode(new PtrChild(new GoPointer(new int[5])), 5).executeLong(null),
                "cap(non-nil *[5]int)");
    }

    public void testLenZeroLengthPointerToArray() {
        checkEq(0, new LenNode(new PtrChild(nilPointer()), 0).executeLong(null),
                "len(nil *[0]int)");
        checkEq(0, new CapNode(new PtrChild(nilPointer()), 0).executeLong(null),
                "cap(nil *[0]int)");
    }

    public void testLenPointerWithoutResolvedLengthFailsFast() {
        try {
            new LenNode(new PtrChild(nilPointer()), -1);
        } catch (FrontendException expected) {
            return;
        }
        throw new AssertionError("LenNode(POINTER, -1) must throw FrontendException");
    }

    public void testCapPointerWithoutResolvedLengthFailsFast() {
        try {
            new CapNode(new PtrChild(nilPointer()), -1);
        } catch (FrontendException expected) {
            return;
        }
        throw new AssertionError("CapNode(POINTER, -1) must throw FrontendException");
    }

    /**
     * Go evaluates the builtin operand (side effects happen) even though the
     * length is static: {@code len(f())} calls {@code f}. The node must
     * evaluate its child; only the dereference is skipped.
     */
    public void testLenEvaluatesOperand() {
        SideEffectChild child = new SideEffectChild();
        checkEq(5, new LenNode(child, 5).executeLong(null), "len(side-effect *[5]int)");
        checkEq(1, child.evaluations, "len must evaluate its operand once");
    }

    public void testCapEvaluatesOperand() {
        SideEffectChild child = new SideEffectChild();
        checkEq(5, new CapNode(child, 5).executeLong(null), "cap(side-effect *[5]int)");
        checkEq(1, child.evaluations, "cap must evaluate its operand once");
    }

    /** Regression: array/slice/string paths are unchanged. */
    public void testLenArrayUnchanged() {
        checkEq(7, new LenNode(new ArrayChild(new int[7]), -1).executeLong(null),
                "len([7]int)");
    }

    public void testCapArrayUnchanged() {
        checkEq(7, new CapNode(new ArrayChild(new int[7]), -1).executeLong(null),
                "cap([7]int)");
    }

    public void testLenNilSliceStillZero() {
        checkEq(0, new LenNode(new SliceChild(null), -1).executeLong(null),
                "len(nil slice)");
    }

    public void testCapNilSliceStillZero() {
        checkEq(0, new CapNode(new SliceChild(null), -1).executeLong(null),
                "cap(nil slice)");
    }

    public void testCapSliceUnchanged() {
        checkEq(8, new CapNode(new SliceChild(new GoSlice(new int[10], 2, 3, 8)), -1)
                .executeLong(null), "cap(slice)");
    }

    public void testLenStringUnchanged() {
        checkEq(5, new LenNode(new StringChild(GoString.fromUtf8("hello")), -1)
                .executeLong(null), "len(string)");
    }
}
