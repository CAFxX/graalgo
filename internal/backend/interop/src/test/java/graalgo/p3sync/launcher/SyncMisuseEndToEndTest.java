package graalgo.p3sync.launcher;

import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * End-to-end pins for sync-misuse and {@code runtime.SetFinalizer}-misuse
 * failure modes (2026-10-18-10 independent review: fatal-vs-panic
 * divergence; ADR-0002 D4).
 *
 * <p>gc's failure modes, verified against go1.27.1
 * ({@code src/internal/sync/mutex.go}, {@code src/sync/rwmutex.go},
 * {@code src/sync/waitgroup.go}, {@code src/runtime/mfinal.go}):
 * <ul>
 *   <li>mutex unlock misuse → {@code fatal()} — unrecoverable,
 *       {@code fatal error: <msg>} on stderr, exit 2;</li>
 *   <li>WaitGroup negative counter → {@code panic()} — recoverable,
 *       {@code panic: <msg>} if uncaught, exit 2;</li>
 *   <li>every {@code runtime.SetFinalizer} misuse (nil object, non-pointer,
 *       non-function, typed-nil pointer, double-set) → {@code throw()} —
 *       unrecoverable, {@code fatal error: <msg>} on stderr, exit 2.</li>
 * </ul>
 *
 * <p>Fatal paths call {@code System.exit}, so they run in a forked JVM;
 * panic paths run in-JVM through {@link P3SyncMain#run} with captured
 * stdio (the {@code LinknameEndToEndTest} pattern).
 *
 * <p>Note: {@code sync.RWMutex} guest programs use the real Go bodies
 * (PKG-053), whose {@code fatal()} wiring is a separate issue in the
 * real-sync lane; the {@code SyncOpNode} RWMutex binding itself is pinned
 * by the in-JVM signal tests in {@code SyncStressTest}.
 *
 * <p>verifies: ADR-0002 D4 (sync-bindings + gc-interop)
 */
public final class SyncMisuseEndToEndTest {

    private static final String PROG_MUTEX_UNLOCK = """
            package main

            import "sync"

            func main() {
                var m sync.Mutex
                m.Unlock()
            }
            """;

    private static final String PROG_MUTEX_UNLOCK_RECOVER = """
            package main

            import "sync"

            func main() {
                var m sync.Mutex
                defer func() {
                    recover()
                }()
                m.Unlock()
            }
            """;

    private static final String PROG_WG_NEG_UNCAUGHT = """
            package main

            import "sync"

            func main() {
                var wg sync.WaitGroup
                wg.Done()
            }
            """;

    private static final String PROG_WG_NEG_RECOVER = """
            package main

            import "sync"

            func main() {
                var wg sync.WaitGroup
                defer func() {
                    recover()
                }()
                wg.Done()
            }
            """;

    private static final String PROG_SF_NIL = """
            package main

            import "runtime"

            func main() {
                runtime.SetFinalizer(nil, func(*int) {})
            }
            """;

    private static final String PROG_SF_NONPOINTER = """
            package main

            import "runtime"

            func main() {
                s := "notapointer"
                runtime.SetFinalizer(s, func(*int) {})
            }
            """;

    private static final String PROG_SF_NONFUNC = """
            package main

            import "runtime"

            func main() {
                x := 1
                s := "notafunc"
                runtime.SetFinalizer(&x, s)
            }
            """;

    private static final String PROG_SF_DOUBLESET = """
            package main

            import "runtime"

            func main() {
                x := 1
                p := &x
                runtime.SetFinalizer(p, func(*int) {})
                runtime.SetFinalizer(p, func(*int) {})
            }
            """;

    private static final String PROG_SF_RECOVER = """
            package main

            import "runtime"

            func main() {
                defer func() {
                    recover()
                }()
                runtime.SetFinalizer(nil, func(*int) {})
            }
            """;

    /** Writes the program to a temp dir and returns the file path. */
    private static Path writeTempGo(String name, String src) throws Exception {
        Path dir = Files.createTempDirectory("graalgo-misuse-test");
        Path f = dir.resolve(name);
        Files.writeString(f, src, StandardCharsets.UTF_8);
        return f;
    }

    /** Result of a forked child run. */
    private record ForkResult(int exit, String stdout, String stderr) {}

    /**
     * Runs {@code P3SyncMain.main} in a forked JVM (fatal paths call
     * {@code System.exit}). Returns exit code + captured stdio.
     */
    private static ForkResult runForked(Path goFile) throws Exception {
        String javaBin = System.getProperty("java.home") + "/bin/java";
        String cp = System.getProperty("java.class.path");
        ProcessBuilder pb = new ProcessBuilder(
                javaBin, "-cp", cp,
                "graalgo.p3sync.launcher.P3SyncMain",
                goFile.toString());
        pb.redirectErrorStream(false);
        Process p = pb.start();
        String stdout = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
        String stderr = new String(p.getErrorStream().readAllBytes(), StandardCharsets.UTF_8);
        int exit = p.waitFor();
        return new ForkResult(exit, stdout, stderr);
    }

    /**
     * First non-blank stderr line, skipping JVM/runtime WARNING lines
     * (e.g. the GraalVM {@code sun.misc.Unsafe} deprecation notice and
     * graalgo's own {@code P3Log} warnings, which precede program output).
     */
    private static String firstStderrLine(String stderr) {
        for (String line : stderr.split("\n")) {
            String t = line.trim();
            if (t.isEmpty() || t.startsWith("WARNING:")) {
                continue;
            }
            // Skip graalgo's own log warnings ("Oct 18, 2026 ... P3Log warn"
            // followed by the "WARNING: ..." line already skipped above).
            if (t.contains("P3Log warn")) {
                continue;
            }
            return t;
        }
        return "";
    }

    /**
     * First stderr line starting with {@code prefix}, skipping WARNING/
     * log lines (see {@link #firstStderrLine}).
     */
    private static String findStderrLine(String stderr, String prefix) {
        for (String line : stderr.split("\n")) {
            String t = line.trim();
            if (t.isEmpty() || t.startsWith("WARNING:") || t.contains("P3Log warn")) {
                continue;
            }
            if (t.startsWith(prefix)) {
                return t;
            }
        }
        return "";
    }

    private static void assertForkedFatal(String tag, ForkResult r, String wantFirstLine) {
        if (r.exit != 2) {
            throw new AssertionError(tag + ": fatal must exit 2, got " + r.exit
                    + "\nstdout:\n" + r.stdout + "\nstderr:\n" + r.stderr);
        }
        String first = firstStderrLine(r.stderr);
        if (!first.equals(wantFirstLine)) {
            throw new AssertionError(tag + ": first stderr line must be [" + wantFirstLine
                    + "], got [" + first + "]\nstderr:\n" + r.stderr);
        }
    }

    // ---- mutex: fatal, unrecoverable ----

    public static void testMutexUnlockIsFatal() throws Exception {
        Path f = writeTempGo("mu_fatal.go", PROG_MUTEX_UNLOCK);
        ForkResult r = runForked(f);
        assertForkedFatal("mutex-unlock", r, "fatal error: sync: unlock of unlocked mutex");
    }

    public static void testMutexUnlockRecoverDoesNotSave() throws Exception {
        Path f = writeTempGo("mu_recover.go", PROG_MUTEX_UNLOCK_RECOVER);
        ForkResult r = runForked(f);
        // A deferred recover() must NOT save the program: gc fatals.
        assertForkedFatal("mutex-unlock-recover", r, "fatal error: sync: unlock of unlocked mutex");
    }

    // ---- WaitGroup: panic, recoverable ----

    public static void testWaitGroupNegativeUncaughtPanics() throws Exception {
        Path f = writeTempGo("wg_panic.go", PROG_WG_NEG_UNCAUGHT);
        ForkResult r = runForked(f);
        if (r.exit != 2) {
            throw new AssertionError("uncaught WaitGroup panic must exit 2, got " + r.exit
                    + "\nstderr:\n" + r.stderr);
        }
        String line = findStderrLine(r.stderr, "panic: sync: negative WaitGroup counter");
        if (line.isEmpty()) {
            throw new AssertionError("stderr must contain [panic: sync: negative WaitGroup counter]"
                    + "\nstderr:\n" + r.stderr);
        }
    }

    public static void testWaitGroupNegativeIsRecoverable() throws Exception {
        // In-JVM: a deferred recover() must catch the WaitGroup panic and
        // the program must exit 0 (proves recoverability, unlike fatal).
        Path f = writeTempGo("wg_recover.go", PROG_WG_NEG_RECOVER);
        PrintStream oldOut = System.out;
        PrintStream oldErr = System.err;
        ByteArrayOutputStream outBuf = new ByteArrayOutputStream();
        ByteArrayOutputStream errBuf = new ByteArrayOutputStream();
        System.setOut(new PrintStream(outBuf));
        System.setErr(new PrintStream(errBuf));
        try {
            P3SyncMain.run(new String[]{f.toString()});
        } finally {
            System.setOut(oldOut);
            System.setErr(oldErr);
        }
        String stdout = outBuf.toString(StandardCharsets.UTF_8);
        String stderr = errBuf.toString(StandardCharsets.UTF_8);
        // P3SyncMain.run returns normally (no System.exit in-JVM); the
        // recovered program must not print a panic.
        String panicLine = findStderrLine(stderr, "panic:");
        if (!panicLine.isEmpty()) {
            throw new AssertionError("recovered WaitGroup panic must not print panic:, got ["
                    + panicLine + "]\nstderr:\n" + stderr + "\nstdout:\n" + stdout);
        }
    }

    // ---- SetFinalizer: fatal on every misuse, unrecoverable ----

    public static void testSetFinalizerNilIsFatal() throws Exception {
        Path f = writeTempGo("sf_nil.go", PROG_SF_NIL);
        ForkResult r = runForked(f);
        assertForkedFatal("setfinalizer-nil", r,
                "fatal error: runtime.SetFinalizer: first argument is nil");
    }

    public static void testSetFinalizerNonPointerIsFatal() throws Exception {
        Path f = writeTempGo("sf_nonptr.go", PROG_SF_NONPOINTER);
        ForkResult r = runForked(f);
        assertForkedFatal("setfinalizer-nonpointer", r,
                "fatal error: runtime.SetFinalizer: first argument is string, not pointer");
    }

    public static void testSetFinalizerNonFuncIsFatal() throws Exception {
        Path f = writeTempGo("sf_nonfunc.go", PROG_SF_NONFUNC);
        ForkResult r = runForked(f);
        assertForkedFatal("setfinalizer-nonfunc", r,
                "fatal error: runtime.SetFinalizer: second argument is string, not a function");
    }

    public static void testSetFinalizerDoubleSetIsFatal() throws Exception {
        Path f = writeTempGo("sf_doubleset.go", PROG_SF_DOUBLESET);
        ForkResult r = runForked(f);
        assertForkedFatal("setfinalizer-doubleset", r,
                "fatal error: runtime.SetFinalizer: finalizer already set");
    }

    public static void testSetFinalizerMisuseRecoverDoesNotSave() throws Exception {
        Path f = writeTempGo("sf_recover.go", PROG_SF_RECOVER);
        ForkResult r = runForked(f);
        // A deferred recover() must NOT save the program: gc throw()s.
        assertForkedFatal("setfinalizer-recover", r,
                "fatal error: runtime.SetFinalizer: first argument is nil");
    }
}
