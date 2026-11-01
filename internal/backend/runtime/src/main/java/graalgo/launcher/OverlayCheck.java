package graalgo.launcher;

/**
 * Single shared fail-fast for the same-FQN shadow overlay (lane C,
 * review P1-3). Eight classes used to exist twice — the truffle
 * module's Phase-1 classes and the runtime module's replacements — with
 * classpath order deciding which behaviour a program got. The 2026-10-31
 * fail-fast duplicated one narrow probe ({@code Scheduler.isPhase2()})
 * in every launcher by hand; this class is the one implementation every
 * launcher, test runner, and per-invocation classloader check calls.
 *
 * <p>The check has two layers, and both must pass:
 * <ol>
 *   <li>The canary: {@code graalgo.runtime.Scheduler} must be the
 *       Phase-2 replacement (it carries {@code isPhase2()}; the
 *       Phase-1 class does not, so a wrong order dies here with a
 *       {@code NoSuchMethodError} first, exactly as before).</li>
 *   <li>The whole overlay: every class in {@link #OVERLAY_CLASSES} must
 *       resolve from the same classpath root as the canary. A single
 *       canary cannot catch a fragmented classpath (for example a
 *       caller-constructed loader that puts the runtime
 *       {@code Scheduler} first but a truffle {@code Timers} first);
 *       comparing resource roots catches exactly that. No overlay
 *       class is loaded or initialized for this layer — only its
 *       {@code .class} resource is located — so classes with static
 *       initializers (notably {@code GcInterop}) are never initialized
 *       by the check itself.</li>
 * </ol>
 *
 * <p>The pair set in {@link #OVERLAY_CLASSES} must equal the runtime
 * Makefile's {@code STUB_REL} and bridge-inventory entry B-8;
 * {@code .github/scripts/check_duplicate_fqn.py} fails the build if
 * the three disagree, so the set cannot drift silently. When the last
 * pair retires, this class is deleted with it (see
 * {@code docs/arch/shadow-overlay-retirement.md}); it is scaffolding
 * for the overlay's lifetime, not a permanent fixture.
 *
 * <p>Failure is always loud: every failure mode throws
 * {@code IllegalStateException} with a {@code FATAL} message naming
 * the class and the classpath roots involved. There is no silent skip:
 * a missing resource, an unresolvable root, or a missing
 * {@code OverlayCheck} itself (in the fast-cycle daemon's reflective
 * call) is a failure, never a pass.
 */
public final class OverlayCheck {
    private OverlayCheck() {}

    /**
     * Fully-qualified names that still exist twice (truffle Phase-1
     * class + runtime replacement). One name per line, double-quoted:
     * {@code check_duplicate_fqn.py} parses this list mechanically and
     * requires it to equal {@code STUB_REL} and inventory B-8.
     * {@code graalgo.safepoint.Safepoint} is absent: its truffle copy
     * was deleted by the lane-C pilot retirement, leaving the runtime
     * module as its sole definition.
     */
    static final String[] OVERLAY_CLASSES = {
        "graalgo.runtime.ChanOps",
        "graalgo.runtime.GcInterop",
        "graalgo.runtime.GoroutineState",
        "graalgo.runtime.Netpoll",
        "graalgo.runtime.Panic",
        "graalgo.runtime.Scheduler",
        "graalgo.runtime.Timers",
    };

    /** Fail fast unless the overlay resolves to the runtime side. */
    public static void assertOverlayActive() {
        // Layer 1 — the canary (pre-existing R-01 semantics, wording kept
        // so LauncherOverlayTest's FATAL-message assertions still hold).
        try {
            if (!graalgo.runtime.Scheduler.isPhase2()) {
                throw new IllegalStateException(
                        "FATAL: graalgo.runtime.Scheduler.isPhase2() returned false; "
                        + "the runtime module's classes must precede the truffle "
                        + "module's on the classpath");
            }
        } catch (NoSuchMethodError e) {
            throw new IllegalStateException(
                    "FATAL: Phase-1 Scheduler stub loaded instead of the Phase-2 "
                    + "overlay; runtime/out/classes must precede truffle/out/classes "
                    + "on the classpath",
                    e);
        }

        // Layer 2 — every overlay class from the canary's classpath root.
        // The loader is the one that defined this class: in a launcher
        // that is the application loader; in the fast-cycle daemon's
        // reflective call it is the per-invocation loader, which is
        // exactly the loader under test.
        ClassLoader loader = OverlayCheck.class.getClassLoader();
        if (loader == null) {
            loader = ClassLoader.getSystemClassLoader();
        }
        String schedulerRoot = resourceRoot(loader, "graalgo/runtime/Scheduler.class");
        if (schedulerRoot == null) {
            throw new IllegalStateException(
                    "FATAL: overlay incomplete: graalgo/runtime/Scheduler.class "
                    + "not found on the classpath");
        }
        for (String fqn : OVERLAY_CLASSES) {
            String path = fqn.replace('.', '/') + ".class";
            String root = resourceRoot(loader, path);
            if (root == null) {
                throw new IllegalStateException(
                        "FATAL: overlay incomplete: " + path + " not found on the "
                        + "classpath; the runtime module's classes must be on "
                        + "the classpath");
            }
            if (!root.equals(schedulerRoot)) {
                throw new IllegalStateException(
                        "FATAL: overlay split across classpath entries: " + fqn
                        + " resolves from " + root + " but "
                        + "graalgo.runtime.Scheduler resolves from " + schedulerRoot
                        + "; all overlay classes must come from the runtime "
                        + "module's classes directory");
            }
        }
    }

    /**
     * Classpath root a {@code .class} resource resolves from: the
     * directory for {@code file:} URLs, the jar for {@code jar:file:}
     * URLs (both with a trailing separator, so equality is a same-root
     * test). Any other URL form yields the whole URL as its root: two
     * classes from the same exotic location still compare equal, and a
     * class from anywhere else fails loud with both URLs in the
     * message. Returns null only when the resource is absent.
     */
    private static String resourceRoot(ClassLoader loader, String path) {
        java.net.URL url = loader.getResource(path);
        if (url == null) {
            return null;
        }
        String s = url.toString();
        String dirSuffix = "/" + path;
        if (s.endsWith(dirSuffix)) {
            return s.substring(0, s.length() - path.length());
        }
        String jarSuffix = "!/" + path;
        int bang = s.indexOf(jarSuffix);
        if (bang >= 0) {
            return s.substring(0, bang + 1);
        }
        return s;
    }
}
