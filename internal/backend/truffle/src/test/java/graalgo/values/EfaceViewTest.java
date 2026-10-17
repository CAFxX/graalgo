package graalgo.values;

import graalgo.frontend.SelectorNode;
import graalgo.runtime.PanicException;
import java.util.Map;

/**
 * P3-M10 regression tests: the unsafe-reinterpretation views for
 * {@code internal/abi} ({@link EfaceRef}, {@link AbiTypeRef}) in
 * {@link SelectorNode#readFieldPath}.
 *
 * <p>Every test is demonstrably capable of failing: the pointer-form tests
 * threw {@link ClassCastException} before the EfaceRef branch moved after
 * {@code deref}, and the null test threw a Java {@link NullPointerException}
 * before nulls became gc's nil-dereference panic.
 */
public final class EfaceViewTest {

    /** A fake {@code internal/abi.Type} view: field 6 (Kind_) = 25. */
    private static AbiTypeRef typeRef() {
        return new AbiTypeRef(0x1234L, "internal/abi.Type", Map.of(6, 25L));
    }

    private static void assertEq(long expected, long actual, String what) {
        if (expected != actual) {
            throw new AssertionError(what + ": expected " + expected + " but got " + actual);
        }
    }

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what);
        }
    }

    /** The returned *abi.Type must be a GoPointer wrapping the AbiTypeRef. */
    private static AbiTypeRef assertAbiType(Object v, String what) {
        assertTrue(v instanceof GoPointer, what + ": not a pointer: " + v);
        Object referent = ((GoPointer) v).referent();
        assertTrue(referent instanceof AbiTypeRef, what + ": referent not an AbiTypeRef: " + referent);
        return (AbiTypeRef) referent;
    }

    public void testDerefFormTypeField() {
        // (*p).Type — the *(*abi.EmptyInterface)(unsafe.Pointer(&x)) idiom
        // from internal/abi.TypeOf, as used by internal/reflectlite.
        EfaceRef ef = new EfaceRef(typeRef(), 77L);
        AbiTypeRef got = assertAbiType(
                SelectorNode.readFieldPath(ef, new int[]{0}, false), "deref-form .Type");
        assertEq(0x1234L, got.tok(), "deref-form .Type token");
    }

    public void testPointerFormTypeField() {
        // p.Type on a *abi.EmptyInterface — auto-deref through the pointer.
        EfaceRef ef = new EfaceRef(typeRef(), 77L);
        AbiTypeRef got = assertAbiType(
                SelectorNode.readFieldPath(new GoPointer(ef), new int[]{0}, false),
                "pointer-form .Type");
        assertEq(0x1234L, got.tok(), "pointer-form .Type token");
    }

    public void testPointerFormDataField() {
        // p.Data on a *abi.EmptyInterface — the data word as unsafe.Pointer.
        EfaceRef ef = new EfaceRef(typeRef(), 77L);
        Object v = SelectorNode.readFieldPath(new GoPointer(ef), new int[]{1}, false);
        assertTrue(v instanceof Long, "pointer-form .Data not a Long: " + v);
        assertEq(77L, (Long) v, "pointer-form .Data");
    }

    public void testNilInterfaceView() {
        // A nil interface viewed as eface: Type is nil *abi.Type, Data is 0.
        EfaceRef ef = new EfaceRef(null, 0L);
        Object t = SelectorNode.readFieldPath(new GoPointer(ef), new int[]{0}, false);
        assertTrue(t instanceof GoPointer && ((GoPointer) t).isNil(),
                "nil-iface .Type not a nil pointer: " + t);
        Object d = SelectorNode.readFieldPath(new GoPointer(ef), new int[]{1}, false);
        assertEq(0L, (Long) d, "nil-iface .Data");
    }

    public void testAbiTypeRefBehindPointer() {
        // t.Kind_ on a *abi.Type — the pre-existing path; the EfaceRef
        // reorder must not disturb it.
        Object v = SelectorNode.readFieldPath(new GoPointer(typeRef()), new int[]{6}, false);
        assertTrue(v instanceof Long, ".Kind_ not a Long: " + v);
        assertEq(25L, (Long) v, ".Kind_");
    }

    public void testTypedNilDataPanicsNotNpe() {
        // A typed-nil interface's data (Java null) reaching a struct field
        // selection: gc's nil-dereference panic, never a Java NPE. This is
        // the crash that killed `import "os"` (exit 1) before the P1 fix.
        GoInterface typedNil = new GoInterface(new Object(), null, null);
        try {
            SelectorNode.readFieldPath(typedNil, new int[]{0}, false);
        } catch (PanicException expected) {
            return; // gc's nil-dereference panic — correct
        }
        throw new AssertionError("expected PanicException (Go nil-dereference panic)");
    }
}
