package graalgo.launcher;

/**
 * Single implementation of the same-FQN shadow-overlay fail-fast
 * (review 2026-10-31 P1-5; R-01 established the check, this lane
 * centralises it).
 *
 * <p>Eight classes exist twice: the truffle module's frozen Phase-1
 * stubs ({@code internal/backend/truffle/src/main/java/graalgo/runtime/}
 * plus {@code graalgo/safepoint/Safepoint}) and this module's Phase-2
 * replacements. The overlay is only correct when
 * {@code runtime/out/classes} precedes {@code truffle/out/classes} on
 * the classpath. With the wrong order the Phase-1 stub throws on first
 * use — and worse, the Phase-1 static {@code GoroutineState} holder
 * would be silently shared across goroutines. Never silent.
 *
 * <p>Every entry point that constructs a backend classpath calls this
 * gate before doing anything else — including {@code --check} and
 * {@code --help}, which must not exit 0 with the stub loaded. The
 * {@code .github/scripts/check_overlay_assert.py} gate fails CI on any
 * new {@code main()}-style launcher under
 * {@code internal/backend/<module>/src/main/java},
 * {@code internal/backend/<module>/src/test/java}, or
 * {@code internal/backend/fast-cycle/} that lacks an
 * {@code assertSchedulerOverlay} call. (The fast-cycle daemon cannot
 * call this class — it compiles standalone — so it carries its own
 * reflective copy of the check; see {@code FastCycleDaemon}.)
 *
 * <p>The probe is {@code graalgo.runtime.Scheduler.isPhase2()}: the
 * Phase-1 stub has no such method, so a stub loaded first surfaces as
 * {@code NoSuchMethodError}, mapped here to a FATAL
 * {@code IllegalStateException}. The elimination lane (P1-5 long term)
 * deletes the truffle stubs, at which point this gate and its tests
 * retire with the overlay itself — do not "simplify" it away while
 * the stubs still exist.
 */
public final class OverlayGate {
    private OverlayGate() {}

    /**
     * Fail fast unless the Phase-2 overlay is active: the
     * {@code graalgo.runtime.Scheduler} on the classpath must be the
     * Phase-2 replacement, i.e. {@code runtime/out/classes} precedes
     * {@code truffle/out/classes}.
     */
    public static void assertSchedulerOverlay() {
        try {
            if (!graalgo.runtime.Scheduler.isPhase2()) {
                throw new IllegalStateException(
                        "graalgo.runtime.Scheduler.isPhase2() returned false");
            }
        } catch (NoSuchMethodError e) {
            throw new IllegalStateException(
                    "FATAL: Phase-1 Scheduler stub loaded instead of the Phase-2 "
                    + "overlay; runtime/out/classes must precede truffle/out/classes "
                    + "on the classpath",
                    e);
        }
    }
}
