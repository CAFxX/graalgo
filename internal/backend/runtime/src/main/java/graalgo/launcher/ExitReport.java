package graalgo.launcher;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.nio.file.StandardOpenOption;

/**
 * Out-of-band exit-outcome report for the {@code graalgo run} driver.
 *
 * <p>The backend's process exit codes 1 and 2 are ambiguous at the driver
 * level: 1 is both "input error" and a program's {@code os.Exit(1)}, 2 is
 * both "build failure" and "uncaught Go panic" (gc parity: a panicking
 * program exits 2) as well as a program's {@code os.Exit(2)}. Any 0-255
 * code is a legitimate program exit, so no exit-code range can be reserved
 * for backend failures. Only the backend knows which side of the ambiguity
 * a given run fell on, so it reports the outcome category here, out of
 * band, when the driver passes {@code --exit-report=PATH}.
 *
 * <p>Protocol: the file holds one line, {@code "program-exit <code>"} when
 * the Go program ran (to completion, panic, os.Exit, or runtime.throw/fatal)
 * and {@code "backend-failure <code>"} when the backend failed on its own
 * (usage/input error, build failure, internal error). The driver combines
 * this category with the process wait status to emulate {@code go run}'s
 * exit contract byte-for-byte: "exit status N" plus exit 1 for program
 * exits, bare exit 1 for backend failures.
 *
 * <p>The report is strictly best-effort and never touches stdout/stderr:
 * if the path is not installed (direct harness/launcher use) every method
 * is a no-op, and a write failure is swallowed — the driver treats a
 * missing or malformed report as a backend failure (exit 1, no wrapper),
 * which is the safe default (a crash before reporting must never gain a
 * bogus "exit status" line).
 *
 * <p>This is launcher/exit-path code: it runs once per process, never on a
 * compiled guest path, so there is no Truffle compilation concern here.
 */
public final class ExitReport {
    private ExitReport() {}

    /** Installed report path, or null when the driver did not request one. */
    private static volatile Path reportPath;

    /**
     * Installs the report path (from the launcher's {@code --exit-report=}
     * flag). Idempotent; later calls replace earlier ones.
     */
    public static void install(String path) {
        if (path == null || path.isEmpty()) {
            return;
        }
        reportPath = Paths.get(path);
    }

    /**
     * Guest-requested process death ({@code os.Exit}, {@code runtime.throw},
     * {@code runtime.fatal}): records a program-exit verdict, then exits the
     * JVM with the same code. Never returns.
     */
    public static void programExit(int code) {
        write("program-exit", code);
        System.exit(code);
    }

    /**
     * Launcher-level terminal outcome: records the verdict and returns the
     * code for the caller to pass to {@code System.exit}. Used by launcher
     * {@code main} methods after they have classified their own return.
     *
     * @param programExit true when the Go program ran (any outcome,
     *     including panic); false when the backend failed on its own
     */
    public static int terminal(boolean programExit, int code) {
        write(programExit ? "program-exit" : "backend-failure", code);
        return code;
    }

    private static void write(String kind, int code) {
        Path p = reportPath;
        if (p == null) {
            return;
        }
        try {
            Files.writeString(p, kind + " " + code + "\n", StandardCharsets.UTF_8,
                    StandardOpenOption.CREATE,
                    StandardOpenOption.TRUNCATE_EXISTING,
                    StandardOpenOption.WRITE);
        } catch (Exception ignored) {
            // Best-effort by design (see class javadoc): never fail the run
            // over the report.
        }
    }
}
