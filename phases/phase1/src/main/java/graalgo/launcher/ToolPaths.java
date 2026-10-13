package graalgo.launcher;

import java.net.URISyntaxException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

/**
 * Locates the external tools the launcher shells out to.
 *
 * <p>{@code gofrontend} (the Go frontend service) resolution order:
 * <ol>
 *   <li>the {@code GRAALGO_GOFRONTEND} environment variable, when it names
 *       an executable file;</li>
 *   <li>{@code <repo>/internal/frontend/gofrontend}, found by walking up from this
 *       class's own code location (covers both {@code make run} and the
 *       {@code graalgo-run} wrapper, whose classes live under
 *       {@code <repo>/phases/phase1/out/});</li>
 *   <li>bare {@code gofrontend} on {@code PATH} (last resort; resolved by
 *       the OS at process-launch time).</li>
 * </ol>
 *
 * <p>Stock Go (differential-test oracle only) resolution order:
 * {@code GRAALGO_GO}, else {@code ~/tools/go/bin/go}, else bare {@code go}
 * on {@code PATH}.
 */
public final class ToolPaths {
    private ToolPaths() {}

    /** Resolves the gofrontend binary per the order documented above. */
    public static Path gofrontend() {
        String env = System.getenv("GRAALGO_GOFRONTEND");
        if (env != null && !env.isEmpty()) {
            Path p = Paths.get(env);
            if (Files.isExecutable(p)) {
                return p;
            }
        }
        Path anchor = codeSourceDir();
        for (int up = 0; anchor != null && up < 5; up++, anchor = anchor.getParent()) {
            Path cand = anchor.resolve("internal/frontend/gofrontend");
            if (Files.isExecutable(cand)) {
                return cand;
            }
        }
        return Paths.get("gofrontend");
    }

    /** Resolves the stock-Go binary (test oracle) per the order documented above. */
    public static Path stockGo() {
        String env = System.getenv("GRAALGO_GO");
        if (env != null && !env.isEmpty()) {
            Path p = Paths.get(env);
            if (Files.isExecutable(p)) {
                return p;
            }
        }
        // Note: the JVM's user.home is /root here while the shell HOME is
        // /home/hatch (pinned toolchain lives under the latter) — try both.
        for (String home : new String[] {System.getenv("HOME"), System.getProperty("user.home")}) {
            if (home != null && !home.isEmpty()) {
                Path p = Paths.get(home, "tools", "go", "bin", "go");
                if (Files.isExecutable(p)) {
                    return p;
                }
            }
        }
        return Paths.get("go");
    }

    /** Directory holding this class's bytecode (out/classes or a jar's dir). */
    private static Path codeSourceDir() {
        try {
            Path loc = Paths.get(
                    ToolPaths.class.getProtectionDomain().getCodeSource().getLocation().toURI());
            return Files.isDirectory(loc) ? loc : loc.getParent();
        } catch (URISyntaxException | SecurityException e) {
            return null;
        }
    }
}
