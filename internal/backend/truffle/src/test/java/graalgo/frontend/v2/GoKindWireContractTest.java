package graalgo.frontend.v2;

import graalgo.values.GoKind;

/**
 * Cross-language contract tests for the v2 basic-type class vocabulary.
 *
 * <p>The Go emitter ({@code internal/frontend/v2/emit}, {@code basicClass})
 * tags every basic type descriptor with a {@code class} string; the backend
 * maps {@code (name, class)} pairs through {@link GoKind#fromBasic}. The two
 * sides must agree on the vocabulary, or compilation of ordinary Go code
 * fails in the factory.
 *
 * <p>Regression test for V2-1: {@code basicClass} masked
 * {@code b.Info()} with {@code types.IsNumeric}, which contains neither
 * {@code IsUnsigned} nor {@code IsBoolean} in go1.27's go/types, so every
 * unsigned type emitted {@code class:"int"} and {@code bool} emitted
 * {@code class:"unknown"} — both rejected by {@code fromBasic}.
 *
 * <p>Keep in sync with the Go side:
 * {@code internal/frontend/v2/emit/basic_class_test.go}
 * ({@code TestBasicClassWireContract}) asserts the emitter produces exactly
 * the pairs exercised here.
 */
public final class GoKindWireContractTest {

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void checkKind(String name, String cls, GoKind want) {
        GoKind got;
        try {
            got = GoKind.fromBasic(name, cls);
        } catch (IllegalArgumentException e) {
            throw new AssertionError(
                    "fromBasic(" + name + ", " + cls + ") threw: " + e.getMessage());
        }
        check(got == want,
                "fromBasic(" + name + ", " + cls + "): want " + want + " got " + got);
    }

    /** Every (name, class) pair the Go emitter may produce must map. */
    public void testEmitterPairsMap() {
        checkKind("bool", "bool", GoKind.BOOL);
        checkKind("int", "int", GoKind.INT64);
        checkKind("int8", "int", GoKind.INT8);
        checkKind("int16", "int", GoKind.INT16);
        checkKind("int32", "int", GoKind.INT32);
        checkKind("rune", "int", GoKind.INT32);
        checkKind("int64", "int", GoKind.INT64);
        checkKind("uint", "uint", GoKind.UINT64);
        checkKind("uint8", "uint", GoKind.UINT8);
        checkKind("byte", "uint", GoKind.UINT8);
        checkKind("uint16", "uint", GoKind.UINT16);
        checkKind("uint32", "uint", GoKind.UINT32);
        checkKind("uint64", "uint", GoKind.UINT64);
        checkKind("uintptr", "uint", GoKind.UINT64);
        checkKind("float32", "float", GoKind.FLOAT32);
        checkKind("float64", "float", GoKind.FLOAT64);
        checkKind("complex64", "complex", GoKind.COMPLEX64);
        checkKind("complex128", "complex", GoKind.COMPLEX128);
        checkKind("string", "string", GoKind.STRING);
        // Untyped constants ride the family class (there is no "untyped" class).
        checkKind("untyped int", "int", GoKind.UNTYPED_INT);
        checkKind("untyped rune", "int", GoKind.UNTYPED_INT);
        checkKind("untyped bool", "bool", GoKind.UNTYPED_BOOL);
        checkKind("untyped float", "float", GoKind.UNTYPED_FLOAT);
        checkKind("untyped complex", "complex", GoKind.UNTYPED_COMPLEX);
        checkKind("untyped string", "string", GoKind.UNTYPED_STRING);
    }

    /**
     * The historically mis-emitted pairs must NOT be silently accepted as
     * something else: V2-1's ("uint8","int") and ("bool","unknown") have to
     * keep failing loudly rather than map to a wrong kind.
     */
    public void testMisclassifiedPairsStillRejected() {
        assertThrows(() -> GoKind.fromBasic("uint8", "int"));
        assertThrows(() -> GoKind.fromBasic("uint", "int"));
        assertThrows(() -> GoKind.fromBasic("bool", "unknown"));
        assertThrows(() -> GoKind.fromBasic("untyped bool", "untyped"));
    }

    private static void assertThrows(Runnable r) {
        try {
            r.run();
        } catch (IllegalArgumentException expected) {
            return;
        }
        throw new AssertionError("expected IllegalArgumentException, none thrown");
    }
}
