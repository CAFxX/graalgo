package graalgo.launcher;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Paths;

/**
 * Driver↔backend status channel for {@code graalgo run} exit-code
 * disambiguation (fix-p1-run-exitcode).
 *
 * <p>Problem: the backend JVM's exit code is the only driver←backend
 * channel, and codes 1/2 are overloaded — 1 = backend input/internal
 * error <em>or</em> program {@code os.Exit(1)}; 2 = backend build
 * failure <em>or</em> program {@code os.Exit(2)} / uncaught Go panic /
 * fatal deadlock. Without an out-of-band signal the driver cannot tell a
 * backend failure from a program outcome, and misreports backend failures
 * as {@code exit status N} (gc parity: {@code go run} prints no such line
 * for its own build failures).
 *
 * <p>Convention: the driver passes {@code --status-file=<path>} as an
 * explicit argv flag (argv, never env — the PKG-001 precedent: env is
 * inherited by every child process). The launcher writes a small status
 * document <em>only on its own failure paths</em>. File presence = "the
 * backend itself failed" (driver stays silent, rc 1); absence = "the exit
 * code / signal is the program's outcome" (driver keeps today's
 * {@code exit status N} treatment, which is gc-exact for program exits).
 *
 * <p>The flag is optional: direct invocations (graalgo-run,
 * graalgo-sync-run, diffharness, batch mode) behave exactly as today —
 * exit codes 0/1/2 unchanged, no sentinel. The differential corpus drives
 * the launchers directly and pins 0/1/2, so it is unaffected.
 */
public final class BackendStatus {
    private BackendStatus() {}

    /** Prefix of the driver-internal argv flag carrying the status path. */
    public static final String FLAG_PREFIX = "--status-file=";

    /**
     * A launcher run's outcome: the process exit code, plus whether a
     * non-zero code is the backend's own failure (sentinel written) or the
     * program's outcome (no sentinel — driver keeps the
     * {@code exit status N} treatment).
     */
    public record RunResult(int exitCode, boolean backendFailure) {
        /** Program outcome: no sentinel; driver applies `exit status N`. */
        public static RunResult program(int exitCode) {
            return new RunResult(exitCode, false);
        }

        /** Backend's own failure: driver writes a sentinel; driver stays silent. */
        public static RunResult backend(int exitCode) {
            return new RunResult(exitCode, true);
        }
    }

    /**
     * Pre-pass: extracts the status-file path from argv, or null if the
     * flag is absent (or has an empty value — treated as absent, a
     * malformed-flag fallback that can only degrade to today's behavior).
     * Runs before the main arg loop so even usage errors during parsing
     * still produce a sentinel.
     */
    public static String extractStatusFile(String[] args) {
        for (String a : args) {
            if (a.startsWith(FLAG_PREFIX)) {
                String path = a.substring(FLAG_PREFIX.length());
                return path.isEmpty() ? null : path;
            }
        }
        return null;
    }

    /**
     * Strips every occurrence of the status-file flag from argv, so it is
     * never mistaken for an unknown flag or an input file, and never
     * reaches the guest's {@code os.Args} (the guest only sees args after
     * {@code --}). Returns the original array when the flag is absent.
     */
    public static String[] stripStatusFile(String[] args) {
        int n = 0;
        for (String a : args) {
            if (a.startsWith(FLAG_PREFIX)) {
                n++;
            }
        }
        if (n == 0) {
            return args;
        }
        String[] out = new String[args.length - n];
        int i = 0;
        for (String a : args) {
            if (!a.startsWith(FLAG_PREFIX)) {
                out[i++] = a;
            }
        }
        return out;
    }

    /**
     * Writes the sentinel document. Best-effort: write failures (disk
     * full, unwritable dir) are swallowed — the driver then falls back to
     * the legacy {@code exit status N} treatment (degraded but safe).
     * Never throws.
     */
    public static void writeStatusFile(String path, int exitCode) {
        try {
            String doc = "{\"backend_failure\":true,\"exit_code\":" + exitCode + "}";
            Files.writeString(Paths.get(path), doc, StandardCharsets.UTF_8);
        } catch (Exception ignored) {
            // Best-effort by design (see class Javadoc).
        }
    }
}
