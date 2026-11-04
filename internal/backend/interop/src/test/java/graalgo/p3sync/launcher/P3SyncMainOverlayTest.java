package graalgo.p3sync.launcher;

/**
 * Review 2026-11-04 P1-10: P3SyncMain's all-8-classes overlay guard
 * passes on the correct classpath (the interop test classpath puts
 * interop/out/classes first, then runtime/out/classes, so the
 * Phase-2 overlays load).
 *
 * <p>Fail-fast coverage for a partial/stale classpath lives in
 * {@code graalgo.launcher.LauncherOverlayTest} (child-JVM tests for
 * Main, whose guard runs before help handling and which shares this
 * guard's shape): P3SyncMain's guard runs only after argument parsing
 * and scheduler setup, so a child-JVM negative test here would need a
 * full guest-program run rather than a bare {@code --help}.
 */
public final class P3SyncMainOverlayTest {

    /** verifies: review 2026-11-04 P1-10 (P3SyncMain guard passes on the correct classpath). */
    public void testOverlayCheckPasses() {
        P3SyncMain.assertPhase2Overlay();
    }
}
