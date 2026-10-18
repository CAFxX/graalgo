package graalgo.sched.launcher;

/**
 * Startup guard for the P2-D8 same-FQN classpath overlay (bridge B-8,
 * design: {@code docs/arch/p2-d8-same-fqn-overlay.md}).
 *
 * <p>Eight classes exist twice under identical fully-qualified names (the
 * truffle module's Phase-1 copies and the runtime module's Phase-2
 * replacements); the loaded copy is decided by classpath order
 * ({@code runtime/out/classes} must precede {@code truffle/out/classes}).
 * With the wrong order, the Phase-1 stubs throw
 * {@code UnsupportedOperationException} on first use — and worse, the
 * Phase-1 static {@code GoroutineState} holder would be silently shared
 * across goroutines. Never silent: every entry point that requires the
 * Phase-2 overlay calls {@link #assertPhase2Overlay()} at startup.
 *
 * <p>The check itself is one call to the Phase-2-only diagnostic
 * {@code graalgo.runtime.Scheduler.isPhase2()}: the Phase-1 stub has no
 * such method, so a wrong-order classpath surfaces as
 * {@code NoSuchMethodError}, converted here into a loud
 * {@code IllegalStateException} naming the required order.
 */
public final class OverlayGuard {
    private OverlayGuard() {}

    /**
     * Fails fast unless the Phase-2 overlay is active: the
     * {@code graalgo.runtime.Scheduler} on the classpath must be the
     * Phase-2 replacement, i.e. {@code runtime/out/classes} precedes
     * {@code truffle/out/classes}.
     *
     * @throws IllegalStateException if the Phase-1 stub was loaded instead
     */
    public static void assertPhase2Overlay() {
        try {
            if (!graalgo.runtime.Scheduler.isPhase2()) {
                throw new IllegalStateException(
                        "graalgo.runtime.Scheduler.isPhase2() returned false");
            }
        } catch (NoSuchMethodError e) {
            throw new IllegalStateException(
                    "FATAL: Phase-1 Scheduler stub loaded instead of the Phase-2 "
                    + "overlay; runtime/out/classes must precede "
                    + "truffle/out/classes on the classpath",
                    e);
        }
    }
}
