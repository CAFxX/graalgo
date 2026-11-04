package graalgo.runtime;

import graalgo.values.GoKind;
import graalgo.values.GoTypeDescriptor;

/**
 * NEW-P1-10: the preprint provider hook in {@link PanicRendering}.
 *
 * <p>These tests pin the hook contract only (provider text wins, null /
 * throwing provider falls back to kind-based rendering, a
 * {@link GoErrorValue} still wins over the provider). The provider
 * implementation itself ({@code graalgo.iface.PanicPreprint}, which
 * dispatches a real Go {@code Error()}/{@code String()} method) is
 * covered end to end by the uuid case08 probe and the panic probes,
 * which need a full program + method tables.
 *
 * <p>Registered in {@code graalgo.sched.SchedTestRunner}. Every test
 * resets the provider: the hook is process-wide static state and must
 * never leak into another test's rendering.
 */
public final class PanicRenderingTest {

    private static PanicException structPanic() {
        return new PanicException(new Object(),
                new GoTypeDescriptor("uuid.invalidLengthError", GoKind.STRUCT, false),
                "uuid.invalidLengthError", null);
    }

    private static void assertEquals(String want, String got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": want <" + want + "> got <" + got + ">");
        }
    }

    public void testPreprintProviderTextWins() {
        PanicRendering.setPreprintProvider(p -> "invalid UUID length: 5");
        try {
            assertEquals("panic: invalid UUID length: 5",
                    PanicRendering.renderPanicLine(structPanic()), "provider text");
        } finally {
            PanicRendering.setPreprintProvider(null);
        }
    }

    public void testPreprintMultilineIsIndented() {
        PanicRendering.setPreprintProvider(p -> "first\nsecond");
        try {
            assertEquals("panic: first\n\tsecond",
                    PanicRendering.renderPanicLine(structPanic()), "indented preprint");
        } finally {
            PanicRendering.setPreprintProvider(null);
        }
    }

    public void testPreprintNullFallsBack() {
        PanicRendering.setPreprintProvider(p -> null);
        try {
            assertEquals("panic: (uuid.invalidLengthError) 0xADDR",
                    PanicRendering.renderPanicLine(structPanic()), "null fallback");
        } finally {
            PanicRendering.setPreprintProvider(null);
        }
    }

    public void testPreprintThrowingFallsBack() {
        PanicRendering.setPreprintProvider(p -> {
            throw new RuntimeException("Error() itself panicked");
        });
        try {
            assertEquals("panic: (uuid.invalidLengthError) 0xADDR",
                    PanicRendering.renderPanicLine(structPanic()), "throwing fallback");
        } finally {
            PanicRendering.setPreprintProvider(null);
        }
    }

    public void testGoErrorValueBeatsProvider() {
        PanicRendering.setPreprintProvider(p -> "provider text must lose");
        try {
            PanicException p = new PanicException(new GoErrorValue("runtime error: x"),
                    new GoTypeDescriptor("runtime.errorString", GoKind.STRING, false),
                    null, null);
            assertEquals("panic: runtime error: x",
                    PanicRendering.renderPanicLine(p), "GoErrorValue precedence");
        } finally {
            PanicRendering.setPreprintProvider(null);
        }
    }

    public void testNoProviderFallsBack() {
        PanicRendering.setPreprintProvider(null);
        assertEquals("panic: (uuid.invalidLengthError) 0xADDR",
                PanicRendering.renderPanicLine(structPanic()), "no-provider fallback");
    }
}
