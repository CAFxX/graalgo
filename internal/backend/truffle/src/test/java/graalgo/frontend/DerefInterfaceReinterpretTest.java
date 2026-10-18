package graalgo.frontend;

import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import java.util.List;

/**
 * Regression test for the Finding-A NPE (2026-10-18):
 * {@code NullPointerException: Cannot invoke "GoStruct.descriptor()"
 * because "<local8>" is null} at SelectorNode.readFieldPath:63.
 *
 * <p>Root cause: Go code {@code *(*EmptyInterface)(unsafe.Pointer(&a))}
 * (in {@code internal/abi.TypeOf}) reinterprets a GoInterface's memory as
 * a struct. DerefNode returned the GoInterface itself instead of
 * synthesizing the struct, so the subsequent field selection saw a
 * GoInterface with null data (typed nil) and NPE'd.
 *
 * <p>The fix: DerefNode synthesizes the struct from the interface's
 * (descriptor, data) when dereferencing a *S pointer whose referent is a
 * GoInterface.
 */
public final class DerefInterfaceReinterpretTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(Object expected, Object actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + ": expected <" + expected + "> but was <" + actual + ">");
        }
    }

    /**
     * Positive: a 2-field struct descriptor synthesizes correctly from a
     * GoInterface. Field 0 gets the descriptor, field 1 gets the data.
     * This is the abi.EmptyInterface layout: {Type *Type, Data unsafe.Pointer}.
     */
    public void testSynthesizeTwoFieldStruct() {
        GoStructDescriptor desc = GoStructDescriptor.layout("EmptyInterface",
                List.of("Type", "Data"),
                List.of(GoKind.POINTER, GoKind.UNSAFE_POINTER));
        // Typed nil: interface with descriptor but null data.
        Object fakeTypeDesc = new Object(); // stand-in for *Type
        GoInterface gi = new GoInterface(fakeTypeDesc, null, null);

        GoStruct s = DerefNode.synthesizeStructFromInterface(desc, gi);

        assertTrue(s.descriptor() == desc, "struct descriptor");
        assertTrue(s.getObject(0) == fakeTypeDesc, "field 0 (Type) gets descriptor");
        assertTrue(s.getObject(1) == null, "field 1 (Data) gets null data");
    }

    /**
     * Positive: non-nil data flows through to field 1.
     */
    public void testSynthesizeWithData() {
        GoStructDescriptor desc = GoStructDescriptor.layout("EmptyInterface",
                List.of("Type", "Data"),
                List.of(GoKind.POINTER, GoKind.UNSAFE_POINTER));
        Object fakeTypeDesc = new Object();
        Object fakeData = new Object();
        GoInterface gi = new GoInterface(fakeTypeDesc, fakeData, null);

        GoStruct s = DerefNode.synthesizeStructFromInterface(desc, gi);

        assertTrue(s.getObject(0) == fakeTypeDesc, "field 0 gets descriptor");
        assertTrue(s.getObject(1) == fakeData, "field 1 gets data");
    }

    /**
     * Negative: a struct with != 2 fields fails fast with FrontendException,
     * not a silent wrong result or NPE.
     */
    public void testSynthesizeWrongFieldCountFailsFast() {
        GoStructDescriptor desc = GoStructDescriptor.layout("ThreeFields",
                List.of("a", "b", "c"),
                List.of(GoKind.POINTER, GoKind.POINTER, GoKind.POINTER));
        GoInterface gi = new GoInterface(new Object(), null, null);
        try {
            DerefNode.synthesizeStructFromInterface(desc, gi);
        } catch (FrontendException e) {
            if (!e.getMessage().contains("expected 2")) {
                throw new AssertionError("expected 'expected 2' in message, got: " + e.getMessage());
            }
            return; // expected
        }
        throw new AssertionError("3-field struct must fail fast with FrontendException");
    }

    /**
     * Negative: null descriptor (nil interface) is not synthesized here —
     * DerefNode.read handles the nil case before reaching synthesis.
     * This documents the boundary.
     */
    public void testNilInterfaceNotSynthesized() {
        // GoInterface.NIL has null descriptor; synthesize would put null in field 0.
        // The caller (DerefNode.read) must check for nil before calling synthesize.
        // This test documents that synthesize does not special-case NIL.
        GoStructDescriptor desc = GoStructDescriptor.layout("EmptyInterface",
                List.of("Type", "Data"),
                List.of(GoKind.POINTER, GoKind.UNSAFE_POINTER));
        GoStruct s = DerefNode.synthesizeStructFromInterface(desc, GoInterface.NIL);
        assertTrue(s.getObject(0) == null, "NIL descriptor maps to null field 0");
    }
}
