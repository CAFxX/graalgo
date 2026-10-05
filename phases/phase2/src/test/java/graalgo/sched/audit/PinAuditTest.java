package graalgo.sched.audit;

import java.nio.file.Paths;
import java.util.List;

/**
 * P2-M7: red-green proof that {@link PinAudit} detects genuine pins.
 *
 * <p>The fixture {@code src/test/resources/pinproof.jfr} is a real recording
 * of a virtual thread parking inside a JNI critical section
 * ({@code GetPrimitiveArrayCritical} + {@code LockSupport.parkNanos}) on
 * GraalVM 25 — exactly one {@code jdk.VirtualThreadPinned} event. If the
 * audit tool ever stops detecting pins, this test goes red.
 *
 * <p>verifies: GG-0057 (audit tool detects pins)
 */
public final class PinAuditTest {

    private static final String FIXTURE = "src/test/resources/pinproof.jfr";

    public void testDetectsGenuinePin() throws Exception {
        List<PinAudit.Pin> pins =
                PinAudit.readPins(Paths.get(FIXTURE), "<fixture>", List.of()).pins;
        if (pins.size() != 1) {
            throw new AssertionError(
                    "expected exactly 1 pin event, got " + pins.size());
        }
        PinAudit.Pin p = pins.get(0);
        if (p.expected) {
            throw new AssertionError("pin must be UNEXPECTED with empty allowlist");
        }
        if (!p.thread.equals("pin-jni-vt")) {
            throw new AssertionError("wrong thread: " + p.thread);
        }
        boolean hasPark = p.frames.stream().anyMatch(f -> f.contains("doPark"));
        boolean hasNative = p.frames.stream().anyMatch(f -> f.contains("parkWhileCritical"));
        if (!hasPark || !hasNative) {
            throw new AssertionError("stack missing pin frames: " + p.frames);
        }
    }

    public void testAllowlistMarksExpected() throws Exception {
        List<PinAudit.Pin> pins = PinAudit.readPins(Paths.get(FIXTURE), "<fixture>",
                List.of("pin-jni-vt|doPark")).pins;
        if (pins.size() != 1 || !pins.get(0).expected) {
            throw new AssertionError("allowlisted pin must be EXPECTED");
        }
        if (!"pin-jni-vt|doPark".equals(pins.get(0).allowMatch)) {
            throw new AssertionError("allowMatch not recorded");
        }
    }

    public void testAllowlistThreadPrefixMismatch() throws Exception {
        List<PinAudit.Pin> pins = PinAudit.readPins(Paths.get(FIXTURE), "<fixture>",
                List.of("other-thread|doPark")).pins;
        if (pins.size() != 1 || pins.get(0).expected) {
            throw new AssertionError("non-matching allowlist must stay UNEXPECTED");
        }
    }
}
