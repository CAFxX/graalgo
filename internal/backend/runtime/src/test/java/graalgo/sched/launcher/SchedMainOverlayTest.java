package graalgo.sched.launcher;

/**
 * Review 2026-11-04 P1-10: SchedMain's all-8-classes overlay guard
 * passes on the correct classpath (the runtime test classpath puts
 * runtime/out/classes first, so the Phase-2 overlays load).
 *
 * <p>Fail-fast coverage for a partial/stale classpath lives in
 * {@code graalgo.launcher.LauncherOverlayTest} (child-JVM tests for
 * Main, whose guard runs before help handling and which shares this
 * guard's shape): SchedMain's guard runs only after argument parsing
 * and program build, so a child-JVM negative test here would need a
 * full guest-program run rather than a bare {@code --help}.
 */
public final class SchedMainOverlayTest {

    /** verifies: review 2026-11-04 P1-10 (SchedMain guard passes on the correct classpath). */
    public void testOverlayCheckPasses() {
        SchedMain.assertPhase2Overlay();
    }
}
