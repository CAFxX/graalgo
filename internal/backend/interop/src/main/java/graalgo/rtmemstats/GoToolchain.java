package graalgo.rtmemstats;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.concurrent.TimeUnit;

/**
 * The Go toolchain this backend emulates, for {@code runtime.GOROOT()} and
 * {@code runtime.Version()}.
 *
 * <p>gc semantics: {@code GOROOT()} returns the {@code GOROOT} environment
 * variable at process start when set, else the toolchain's baked-in default
 * ({@code defaultGOROOT}); {@code Version()} returns the link-time
 * {@code buildVersion} (e.g. {@code "go1.27.1"}).
 *
 * <p>graalgo has no link-time constants, so the equivalents are resolved
 * once per process and cached:
 * <ul>
 *   <li>GOROOT: the {@code GOROOT} environment variable when non-empty
 *       (exactly gc's rule); otherwise a one-shot {@code go env GOROOT}
 *       probe (the toolchain on {@code PATH}); empty string when the probe
 *       fails.
 *   <li>Version: the first line of {@code $GOROOT/VERSION} when readable
 *       (that file's first line is the release tag, e.g. {@code "go1.27.1"});
 *       otherwise {@link #PINNED_GO_VERSION}.
 * </ul>
 */
final class GoToolchain {
    private GoToolchain() {}

    /**
     * The Go release this backend release emulates. Keep in sync with the
     * pinned toolchain (repo standing order: {@code ~/tools/go}); used only
     * when the toolchain's own {@code VERSION} file cannot be read.
     */
    static final String PINNED_GO_VERSION = "go1.27.1";

    private static volatile String cachedGoroot;
    private static volatile String cachedVersion;

    /** {@code runtime.GOROOT()} value: env GOROOT, else {@code go env GOROOT} probe. */
    static String goroot() {
        String g = cachedGoroot;
        if (g == null) {
            g = resolveGoroot();
            cachedGoroot = g;
        }
        return g;
    }

    /** {@code runtime.Version()} value: {@code $GOROOT/VERSION}, else the pinned release. */
    static String version() {
        String v = cachedVersion;
        if (v == null) {
            v = resolveVersion();
            cachedVersion = v;
        }
        return v;
    }

    private static String resolveGoroot() {
        String env = System.getenv("GOROOT");
        if (env != null && !env.isEmpty()) {
            return env;
        }
        // Best-effort stand-in for gc's baked-in defaultGOROOT: ask the
        // toolchain on PATH. One-shot, cached; failures yield "".
        try {
            Process p = new ProcessBuilder("go", "env", "GOROOT")
                    .redirectErrorStream(true).start();
            if (!p.waitFor(10, TimeUnit.SECONDS)) {
                p.destroyForcibly();
                return "";
            }
            String out = new String(p.getInputStream().readAllBytes(),
                    StandardCharsets.UTF_8).trim();
            if (p.exitValue() == 0 && !out.isEmpty()) {
                return out;
            }
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        } catch (IOException | SecurityException e) {
            // Probe unavailable; fall through to "".
        }
        return "";
    }

    private static String resolveVersion() {
        String goroot = goroot();
        if (!goroot.isEmpty()) {
            try {
                List<String> lines = Files.readAllLines(
                        Path.of(goroot, "VERSION"), StandardCharsets.UTF_8);
                if (!lines.isEmpty() && !lines.get(0).isBlank()) {
                    return lines.get(0).trim();
                }
            } catch (IOException | SecurityException e) {
                // Fall through to the pinned release.
            }
        }
        return PINNED_GO_VERSION;
    }
}
