package graalgo.launcher;

import java.io.IOException;
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
 *   <li>{@code <backend>/../frontend/gofrontend} (i.e.
 *       {@code internal/frontend/gofrontend}), found by walking up from this
 *       class's own code location (covers both {@code make run} and the
 *       {@code graalgo-run} wrapper, whose classes live under
 *       {@code internal/backend/<module>/out/});</li>
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
        for (int up = 0; anchor != null && up < 8; up++, anchor = anchor.getParent()) {
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

    /**
     * Resolves the stock-Go toolchain root (the directory whose
     * {@code bin/go} is the stock binary), for consumers that build paths
     * off the toolchain layout — e.g. the diffharness {@code DH_GO}
     * variable, used as {@code {GO}/bin/go}.
     *
     * <p>{@link #stockGo()} alone cannot serve this: when no pinned
     * toolchain exists it returns the bare name {@code go}, whose
     * {@code getParent()} is null, so the naive
     * {@code stockGo().getParent().getParent()} computation throws a
     * NullPointerException (scheduled full-suite run 41441646157, where
     * CI's setup-go toolchain is only on PATH). This method instead
     * resolves a bare name through PATH and resolves symlinks (a PATH
     * {@code go} is commonly a link into the toolchain's real
     * {@code bin/}), then takes the grandparent. When no toolchain can be
     * found it fails fast with an IllegalStateException naming what was
     * tried, never a bare NPE.
     */
    public static Path stockGoRoot() {
        Path go = stockGo();
        Path resolved = resolveGoBinary(go);
        Path bin = resolved.getParent();
        if (bin == null) {
            throw new IllegalStateException(
                    "cannot determine Go toolchain root: resolved go binary " + resolved
                            + " has no parent directory");
        }
        Path root = bin.getParent();
        if (root == null) {
            throw new IllegalStateException(
                    "cannot determine Go toolchain root: go binary " + resolved
                            + " is not under a <root>/bin layout");
        }
        return root;
    }

    /** Resolves {@code go} to a real absolute binary path (PATH + symlinks). */
    private static Path resolveGoBinary(Path go) {
        Path abs = go.isAbsolute() ? go : null;
        if (abs == null && go.getParent() == null) {
            // Bare name: search PATH exactly as process launching would.
            String pathEnv = System.getenv("PATH");
            if (pathEnv != null && !pathEnv.isEmpty()) {
                String sep = System.getProperty("path.separator", ":");
                for (String dir : pathEnv.split(java.util.regex.Pattern.quote(sep))) {
                    Path cand = dir.isEmpty()
                            ? Paths.get(go.toString())
                            : Paths.get(dir).resolve(go.toString());
                    if (Files.isExecutable(cand)) {
                        abs = cand.toAbsolutePath();
                        break;
                    }
                }
            }
        }
        if (abs == null) {
            abs = go.toAbsolutePath();
        }
        try {
            return abs.toRealPath();
        } catch (IOException e) {
            return abs.normalize();
        }
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
