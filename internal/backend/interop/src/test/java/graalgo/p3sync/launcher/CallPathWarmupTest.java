package graalgo.p3sync.launcher;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;

/**
 * Pins {@link graalgo.launcher.ClassPreloader#initializeCallPathHolders}:
 * the Truffle call path's lazily-initialized holder classes are
 * initialized on the launcher's shallow stack, so a guest stack overflow
 * cannot poison them (the CI flake of 2026-11-18: {@code
 * OptimizedCallTarget$ReturnProfile} initialized first at the bottom of a
 * 100k-deep recursion, the class became erroneous, and the "stack
 * overflow" fatal error leaked a {@code NoClassDefFoundError} JVM trace —
 * see {@link StackDepthLimitTest}).
 *
 * <p>Each case runs {@link Probe} in a CHILD JVM (class initialization is
 * per-JVM state; an in-process run would see whatever earlier tests did).
 * The probe exhausts a thread's stack and, at the bottom, initializes every
 * holder: a holder not initialized before is initialized there, on an
 * exhausted stack, and is poisoned.
 *
 * <p>Falsifiability: the {@code cold} control (no warm-up) must poison a
 * holder — if it did not, the probe could not tell the two apart and the
 * test fails rather than passing vacuously. The {@code warm} case must
 * poison none, and the warm-up must have found at least one holder (a
 * Truffle upgrade that renames the class turns this red instead of
 * silently disabling the warm-up).
 */
public final class CallPathWarmupTest {

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /** Child-JVM entry point: {@code Probe warm|cold}. */
    public static final class Probe {
        private static List<String> holders;
        private static boolean[] settled;
        private static ClassLoader probeLoader;

        public static void main(String[] args) throws Exception {
            // Initializing the runtime is not the warm-up: the launcher's
            // Context creation does it in both cases.
            ClassLoader loader = com.oracle.truffle.api.Truffle.getRuntime().getClass().getClassLoader();
            probeLoader = loader;
            holders = new ArrayList<>();
            for (String outer : graalgo.launcher.ClassPreloader.callPathClasses()) {
                for (Class<?> n : Class.forName(outer, false, loader).getDeclaredClasses()) {
                    holders.add(n.getName());
                }
            }
            settled = new boolean[holders.size()];
            if (args[0].equals("warm")) {
                graalgo.launcher.ClassPreloader.initializeCallPathHolders();
            }
            Thread t = new Thread(null, Probe::exhaust, "probe", 256 * 1024);
            t.start();
            t.join();
            int poisoned = 0;
            for (String h : holders) {
                try {
                    Class.forName(h, true, loader);
                } catch (Throwable e) {
                    poisoned++;
                    System.out.println("POISONED " + h + " " + e);
                }
            }
            System.out.println("HOLDERS " + holders.size());
            System.out.println("POISONED_COUNT " + poisoned);
        }

        private static void exhaust() {
            try {
                recurse(0);
            } catch (StackOverflowError expected) {
                // Unwound.
            }
        }

        private static int recurse(int n) {
            try {
                return recurse(n + 1) + 1;
            } catch (StackOverflowError soe) {
                // Unwinding one frame at a time: initialize each holder at
                // the deepest frame where initialization starts at all, as
                // guest-triggered profiling at the bottom of a recursion
                // would. Too little stack for Class.forName itself leaves
                // the holder untouched (retried one frame up); running out
                // inside the initializer poisons it.
                for (int i = 0; i < holders.size(); i++) {
                    if (settled[i]) {
                        continue;
                    }
                    try {
                        Class.forName(holders.get(i), true, probeLoader);
                        settled[i] = true;
                    } catch (NoClassDefFoundError | ExceptionInInitializerError e) {
                        settled[i] = true; // poisoned; reported by main
                    } catch (StackOverflowError again) {
                        // Not reached yet (or poisoned: main's check tells).
                    } catch (ClassNotFoundException e) {
                        throw new IllegalStateException(e);
                    }
                }
                throw soe;
            }
        }
    }

    private static String run(String mode) throws Exception {
        Path dir = Files.createTempDirectory("graalgo-warmup");
        String java = Path.of(System.getProperty("java.home"), "bin", "java").toString();
        ProcessBuilder pb = new ProcessBuilder(java, "-Xmx256m", "-XX:-UsePerfData",
                "-cp", System.getProperty("java.class.path"), Probe.class.getName(), mode);
        pb.redirectErrorStream(true);
        pb.redirectOutput(dir.resolve("out").toFile());
        Process p = pb.start();
        check(p.waitFor(120, TimeUnit.SECONDS), "probe did not finish in 120 s");
        String out = Files.readString(dir.resolve("out"), StandardCharsets.UTF_8);
        check(p.exitValue() == 0, "probe " + mode + " failed: <" + out + ">");
        return out;
    }

    public void testWarmupPreventsHolderPoisoning() throws Exception {
        String cold = run("cold");
        check(!cold.contains("HOLDERS 0\n"), "no Truffle call-path holders found: <" + cold + ">");
        check(!cold.contains("POISONED_COUNT 0\n"),
                "control: an exhausted stack must poison a cold holder, else the probe proves nothing: <"
                        + cold + ">");
        String warm = run("warm");
        check(warm.contains("POISONED_COUNT 0\n"), "warm-up left a holder poisonable: <" + warm + ">");
    }
}
