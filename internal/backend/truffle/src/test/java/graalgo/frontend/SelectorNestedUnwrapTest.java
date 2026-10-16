package graalgo.frontend;

import java.util.List;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.runtime.GoErrorValue;
import graalgo.runtime.PanicException;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import graalgo.values.GoTypeDescriptor;

/**
 * Red-green for the P1-2 follow-up to PKG-074: struct field navigation
 * must unwrap <em>arbitrarily nested</em> {@link GoInterface}/{@link GoPointer}
 * wrappers, not just one level.
 *
 * <p>Root cause: {@code fdd76a7c} made {@link SelectorNode#deref} and
 * {@link FieldAddrNode}'s private {@code unwrap} peel exactly one wrapper
 * before the {@code (GoStruct)} cast. But {@link InterfaceBoxNode} boxes
 * {@code var i any = &s} as {@code GoInterface(data=GoPointer(GoStruct))}
 * ({@link Boxing#box} takes the Object slot path for POINTER kind, and
 * {@link Boxing#copyForAssign} passes a {@code GoPointer} through
 * untouched), so a field-selection site receiving the nested shape got
 * {@code deref -> GoPointer -> (GoStruct)} — a fatal JVM
 * {@code ClassCastException}, not a Go panic.
 *
 * <p>RED (pre-fix): the nested-shape tests throw
 * {@code ClassCastException: class graalgo.values.GoPointer cannot be cast
 * to class graalgo.values.GoStruct} (and the nested-nil test throws CCE
 * instead of the Go nil-dereference panic).
 * GREEN (post-fix): {@code deref} loops until the value is neither
 * wrapper, and {@code FieldAddrNode.unwrap} delegates to it.
 */
public final class SelectorNestedUnwrapTest {

    private static final GoTypeDescriptor ANY_DESC =
            new GoTypeDescriptor("any", GoKind.INTERFACE, true);

    private static GoStructDescriptor oneFieldDesc() {
        return new GoStructDescriptor("T", List.of(
                new GoStructDescriptor.Field("x", GoKind.INT32, true, 0, 0, null, null)));
    }

    private static GoStruct structWith(long x) {
        return new GoStruct(oneFieldDesc(), new long[]{x}, new Object[0]);
    }

    /** The production shape: {@code var i any = &s}. */
    private static GoInterface nested(Object data) {
        return new GoInterface(ANY_DESC, data, null);
    }

    /** A base node evaluating to a fixed value (frame is unused). */
    private static GoExprNode constBase(Object value, GoKind kind) {
        return new GoExprNode(kind) {
            @Override
            public Object executeObject(VirtualFrame frame) {
                return value;
            }
        };
    }

    private static void assertEquals(Object expected, Object actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + " — expected=" + expected + " actual=" + actual);
        }
    }

    /** Asserts the thunk raises Go's nil-dereference panic (not an NPE/CCE). */
    private static void assertNilDereferencePanic(Runnable thunk, String msg) {
        try {
            thunk.run();
        } catch (PanicException e) {
            Object v = e.goValue();
            if (v instanceof GoErrorValue gev
                    && gev.errorString().contains("nil pointer dereference")) {
                return;
            }
            throw new AssertionError(msg + " — wrong panic value: " + v);
        }
        throw new AssertionError(msg + " — expected Go nil-dereference panic, none thrown");
    }

    /**
     * {@code i.x} where {@code i} is {@code GoInterface(GoPointer(GoStruct))}
     * reads the field through both wrappers.
     */
    public void testNestedInterfacePointerStructReadsField() {
        Object base = nested(new GoPointer(structWith(7)));
        Object got = SelectorNode.readFieldPath(base, new int[]{0}, false, null);
        assertEquals(Long.valueOf(7), got, "field read through GoInterface(GoPointer(GoStruct))");
    }

    /**
     * Deeper nesting still unwraps: interface → pointer → interface →
     * pointer → struct. Only the loop handles this; one-level unwrap
     * cannot.
     */
    public void testDoublyNestedUnwrapReadsField() {
        Object inner = nested(new GoPointer(structWith(9)));
        Object base = nested(new GoPointer(inner));
        Object got = SelectorNode.readFieldPath(base, new int[]{0}, false, null);
        assertEquals(Long.valueOf(9), got, "field read through doubly-nested wrappers");
    }

    /** Wrapper order variant: pointer → interface → pointer → struct. */
    public void testPointerToNestedInterfaceReadsField() {
        Object base = new GoPointer(nested(new GoPointer(structWith(11))));
        Object got = SelectorNode.readFieldPath(base, new int[]{0}, false, null);
        assertEquals(Long.valueOf(11), got, "field read through GoPointer(GoInterface(GoPointer(GoStruct)))");
    }

    /**
     * A nil pointer nested inside an interface still raises Go's
     * nil-dereference panic (pre-fix this threw ClassCastException).
     */
    public void testNilPointerInsideInterfacePanicsGo() {
        Object base = nested(new GoPointer((Object) null));
        assertNilDereferencePanic(
                () -> SelectorNode.readFieldPath(base, new int[]{0}, false, null),
                "GoInterface(nil *T) field read");
    }

    /** Nil-pointer base keeps the old behavior: Go panic, attributed to site. */
    public void testNilPointerBasePanicsGo() {
        assertNilDereferencePanic(
                () -> SelectorNode.readFieldPath(new GoPointer((Object) null),
                        new int[]{0}, false, null),
                "nil *T field read");
    }

    /** Nil-interface base keeps the old behavior: Go panic, not NPE. */
    public void testNilInterfaceBasePanicsGo() {
        assertNilDereferencePanic(
                () -> SelectorNode.readFieldPath(GoInterface.NIL, new int[]{0}, false, null),
                "nil any field read");
    }

    /**
     * Already-fixed shapes are unchanged: a direct struct and a
     * single-level pointer still navigate (no behavior change for the
     * PKG-074 shapes).
     */
    public void testDirectStructAndPointerShapesUnchanged() {
        assertEquals(Long.valueOf(3),
                SelectorNode.readFieldPath(structWith(3), new int[]{0}, false, null),
                "direct GoStruct field read");
        assertEquals(Long.valueOf(5),
                SelectorNode.readFieldPath(new GoPointer(structWith(5)), new int[]{0}, false, null),
                "GoPointer(GoStruct) field read");
        assertEquals(Long.valueOf(13),
                SelectorNode.readFieldPath(nested(structWith(13)), new int[]{0}, false, null),
                "GoInterface(GoStruct) field read (PKG-074 shape)");
    }

    /**
     * {@code &i.x} where {@code i} is the nested shape: the unified
     * unwrap (FieldAddrNode delegating to SelectorNode.deref) peels both
     * wrappers and yields a pointer to the field's storage.
     */
    public void testFieldAddrThroughNestedInterface() {
        GoStruct s = structWith(0);
        FieldAddrNode n = new FieldAddrNode(
                constBase(nested(new GoPointer(s)), GoKind.POINTER),
                new int[]{0}, GoKind.INT32);
        Object r = n.executeObject(null);
        if (!(r instanceof GoPointer gp) || gp.isNil()) {
            throw new AssertionError("FieldAddrNode on nested interface must yield "
                    + "a non-nil GoPointer, got "
                    + (r == null ? "null" : r.getClass().getName()));
        }
    }

    /**
     * {@code i.x = v} through the nested shape (SelectorWriteNode reuses
     * deref): the write lands in the underlying struct's field.
     */
    public void testSelectorWriteThroughNestedInterface() {
        GoStruct s = structWith(0);
        GoExprNode value = new GoExprNode(GoKind.INT32) {
            @Override
            public int executeInt(VirtualFrame frame) {
                return 42;
            }
        };
        SelectorWriteNode n = new SelectorWriteNode(
                constBase(nested(new GoPointer(s)), GoKind.POINTER),
                value, new int[]{0}, GoKind.INT32);
        n.executeVoid(null);
        assertEquals(42L, s.getLong(0), "write through GoInterface(GoPointer(GoStruct))");
    }
}
