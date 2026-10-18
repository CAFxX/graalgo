package graalgo.runtime;

import graalgo.values.GoInterface;
import graalgo.values.GoKind;

/**
 * P0-F (independent review 2026-10-18): rendering of a nil panic value.
 *
 * <p>With {@code GODEBUG=panicnil=1}, {@code panic(nil)} preserves the nil
 * ({@link Panic#throwPanic} produces {@code PanicException(null, null, …)}),
 * and gc's pre-1.21 behavior prints {@code panic: nil}. The renderer
 * previously degraded the null to {@code "(?) 0xADDR"}, contradicting both
 * gc and the overlay's documented intent. With the default knob, nil is
 * wrapped in {@code *PanicNilError} before rendering, so default output is
 * unchanged.
 *
 * <p>verifies: GG-0055 (panicnil old behavior: uncaught panic(nil) prints
 * "panic: nil"; default behavior prints the PanicNilError text).
 */
public final class PanicRenderingTest {

    /** verifies: GG-0055 (a nil panic value renders as "nil"). */
    public void testRenderValueNullValueRendersNil() {
        assertEquals("nil", PanicRendering.renderValue(null, null, null),
                "null value, no kind/type name");
    }

    /**
     * verifies: GG-0055 (nil has no kind — a null value renders "nil" even
     * when a kind or type name is supplied, e.g. the knob branch passes
     * through a goTypeName).
     */
    public void testRenderValueNullValueWithKindOrTypeNameRendersNil() {
        assertEquals("nil", PanicRendering.renderValue(null, GoKind.INT64, null),
                "null value with a kind");
        assertEquals("nil", PanicRendering.renderValue(null, null, "main.T"),
                "null value with a type name");
        assertEquals("nil", PanicRendering.renderValue(null, GoKind.POINTER, "*int"),
                "null value with kind and type name");
    }

    /**
     * verifies: GG-0055 (uncaught panic(nil) under panicnil=1 renders
     * "panic: nil" — the exact shape Panic.throwPanic produces for the
     * knob path).
     */
    public void testPanicNilOldBehaviorRendersPanicNil() {
        PanicException p = new PanicException(null, null, null, null);
        assertEquals("panic: nil", PanicRendering.renderPanicLine(p),
                "renderPanicLine for the knob's (null, null) panic");
    }

    /**
     * verifies: GG-0055 (end-to-end through Panic.throwPanic with the knob
     * on: the nil is preserved, not wrapped, and renders "panic: nil").
     */
    public void testThrowPanicNilWithKnobRendersPanicNil() {
        boolean prev = Panic.isPanicNilOldBehavior();
        try {
            Panic.setPanicNilOldBehavior(true);
            assertEquals("panic: nil", renderThrownPanicLine(null),
                    "panic(nil) with panicnil=1");
            assertEquals("panic: nil", renderThrownPanicLine(GoInterface.NIL),
                    "panic(nil-interface) with panicnil=1");
        } finally {
            Panic.setPanicNilOldBehavior(prev);
        }
    }

    /**
     * verifies: GG-0055 (negative control — the default knob wraps nil in
     * *PanicNilError, so rendering is unchanged by the null special-case).
     */
    public void testDefaultKnobRenderingUnchanged() {
        boolean prev = Panic.isPanicNilOldBehavior();
        try {
            Panic.setPanicNilOldBehavior(false);
            String line = renderThrownPanicLine(null);
            assertEquals("panic: runtime error: panic called with nil argument",
                    line, "default knob still renders the PanicNilError text");
            assertTrue(!line.contains("0xADDR"), "no degraded rendering: " + line);
        } finally {
            Panic.setPanicNilOldBehavior(prev);
        }
    }

    /**
     * Sanity pin: non-nil rendering is untouched by the null special-case.
     */
    public void testRenderValueNonNilPathsUnchanged() {
        assertEquals("5", PanicRendering.renderValue(5L, GoKind.INT64, null),
                "int value");
        assertEquals("main.MyInt(5)",
                PanicRendering.renderValue(5L, GoKind.INT64, "main.MyInt"),
                "named int value");
        assertEquals("(?) 0xADDR",
                PanicRendering.renderValue(new Object(), null, null),
                "degraded path for non-nil value without kind");
    }

    /** Throws via Panic.throwPanic, catches, renders the panic line. */
    private static String renderThrownPanicLine(Object goValue) {
        try {
            Panic.throwPanic(goValue);
        } catch (PanicException p) {
            return PanicRendering.renderPanicLine(p);
        }
        throw new AssertionError("Panic.throwPanic did not throw");
    }

    private static void assertEquals(String expected, String actual, String what) {
        if (!expected.equals(actual)) {
            throw new AssertionError(what + ": expected <" + expected
                    + "> but was <" + actual + ">");
        }
    }

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what);
        }
    }
}
