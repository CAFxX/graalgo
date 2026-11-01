package graalgo.p3unsafe;

import graalgo.runtime.PanicException;
import graalgo.runtime.PanicRendering;
import graalgo.values.GoString;
import graalgo.values.UnsafeHandles;

/**
 * Review finding R5: a D5 unsafe trap must surface to the guest program
 * as a Go panic — recoverable, with a gc-shaped {@code panic: <message>}
 * header — never as a scheduler {@code "fatal internal error"} crash.
 *
 * <p>These tests pin the boundary conversion itself
 * ({@link UnsafeOps#panicFromTrap}, used by {@link UnsafeFromUintNode},
 * {@link UnsafeDeriveNode} and {@link UnsafeAddNode} at their known throw
 * sites). The node-level behavior (uncaught trap → exit 2 with a panic
 * header; deferred recover observing the message) is covered end to end
 * by the guest programs in {@code test/corpus/wave2/unsafe/03c_*} and
 * {@code test/regression/unsafe-trap-panic/}; the low-level trap contract
 * (table-level {@code derive} still throws
 * {@code UnsafeEscapeException}) is pinned by
 * {@link UnsafeDeriveTest#testNilBaseNonzeroOffsetTraps}.
 *
 * <p>Falsifiability: on the unfixed code there is no
 * {@code panicFromTrap} at all, so this test does not compile there; a
 * conversion that passed the raw Java exception through, or a panic
 * value that rendered as {@code "(?) 0xADDR"} (the degraded path a raw
 * Java {@code String} value takes), fails the assertions below.
 */
public final class UnsafeTrapPanicTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(Object expected, Object actual, String msg) {
        boolean eq = expected == null ? actual == null : expected.equals(actual);
        if (!eq) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /** The exact low-level trap message the from-uint node converts. */
    private static final String FROM_UINT_MESSAGE =
            "unsafe: uintptr -> unsafe.Pointer conversion traps "
                    + "(D5: a handle can never be materialized from an integer; "
                    + "re-test: byte-addressed value model)";

    /** The exact low-level trap message the nil-base derivation converts. */
    private static final String NIL_BASE_MESSAGE =
            "unsafe: byte derivation from a nil unsafe.Pointer traps "
                    + "(D5: no referent to derive from)";

    private static PanicException capturePanic(String message) {
        try {
            UnsafeOps.panicFromTrap(new UnsafeHandles.UnsafeEscapeException(message), null);
        } catch (PanicException pe) {
            return pe;
        }
        throw new AssertionError("panicFromTrap must throw PanicException for: " + message);
    }

    public void testFromUintTrapBecomesGoPanicWithMessageValue() {
        PanicException pe = capturePanic(FROM_UINT_MESSAGE);
        assertTrue(pe.goValue() instanceof GoString,
                "panic value must be a GoString so the header renders the message, got: "
                        + pe.goValue().getClass().getName());
        assertEquals(FROM_UINT_MESSAGE,
                new String(((GoString) pe.goValue()).bytes(), java.nio.charset.StandardCharsets.UTF_8),
                "panic value must carry the trap message verbatim");
    }

    public void testNilBaseTrapBecomesGoPanicWithMessageValue() {
        PanicException pe = capturePanic(NIL_BASE_MESSAGE);
        assertTrue(pe.goValue() instanceof GoString,
                "panic value must be a GoString, got: " + pe.goValue().getClass().getName());
        assertEquals(NIL_BASE_MESSAGE,
                new String(((GoString) pe.goValue()).bytes(), java.nio.charset.StandardCharsets.UTF_8),
                "panic value must carry the trap message verbatim");
    }

    public void testConvertedPanicRendersGcShapedHeader() {
        PanicException pe = capturePanic(FROM_UINT_MESSAGE);
        assertEquals("panic: " + FROM_UINT_MESSAGE, PanicRendering.renderPanicLine(pe),
                "uncaught header must be gc-shaped, not a degraded (?) rendering");
    }

    public void testConvertedPanicIsNotAnInternalError() {
        // The scheduler records PanicException via its panic path and any
        // other RuntimeException via its internal-error path; catching
        // exactly PanicException here proves the conversion produced the
        // panic type (an UnsafeEscapeException leaking through would
        // escape this catch and fail the test).
        try {
            UnsafeOps.panicFromTrap(
                    new UnsafeHandles.UnsafeEscapeException(FROM_UINT_MESSAGE), null);
            throw new AssertionError("panicFromTrap must throw");
        } catch (PanicException expected) {
            assertTrue(expected.goValue() instanceof GoString,
                    "converted panic must carry a GoString value");
        }
    }
}
