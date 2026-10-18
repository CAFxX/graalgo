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
 *   <li>mutex / RWMutex unlock misuse → {@code fatal()} — unrecoverable,
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

            import (
                "fmt"
                "sync"
            )

            func main() {
                var m sync.Mutex
                defer func() {
                    if r := recover(); r != nil {
                        fmt.Println("RECOVERED:", r)
                    } else {
                        fmt.Println("NOT RECOVERED")
                    }
                }()
                m.Unlock()
            }
            """;

    private static final String PROG_RWMUTEX_UNLOCK = """
            package main

            import "sync"

            func main() {
                var m sync.RWMutex
                m.Unlock()
            }
            """;

    private static final String PROG_RWMUTEX_RUNLOCK = """
            package main

            import "sync"

            func main() {
                var m sync.RWMutex
                m.RUnlock()
            }
            """;

    private static final String PROG_WG_NEGATIVE_RECOVERED = """
            package main

            import (
                "fmt"
                "sync"
            )

            func main() {
                var wg sync.WaitGroup
                defer func() {
                    if r := recover(); r != nil {
                        fmt.Println("RECOVERED:", r)
                    }
                }()
                wg.Add(-1)
            }
            """;

    private static final String PROG_WG_NEGATIVE_UNCAUGHT = """
            package main

            import "sync"

            func main() {
                var wg sync.WaitGroup
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
                runtime.SetFinalizer(42, func(*int) {})
            }
            """;

    private static final String PROG_SF_NONFUNC = """
            package main

            import "runtime"

            func main() {
                x := 1
                runtime.SetFinalizer(&x, 42)
            }
            """;

    private static final String PROG_SF_DOUBLESET = """
            package main

            import "runtime"

            func main() {
                x := 1
                runtime.SetFinalizer(&x, func(*int) {})
                runtime.SetFinalizer(&x, func(*int) {})
            }
            """;

    private static final String PROG_SF_NIL_RECOVER = """
            package main

            import (
                "fmt"
                "runtime"
            )

            func main() {
                defer func() {
                    if r := recover(); r != nil {
                        fmt.Println("RECOVERED")
                    } else {
                        fmt.Println("NOT RECOVERED")
                    }
                }()
                runtime.SetFinalizer(nil, func(*int) {})
            }
            """;

    private record RunResult(int exit, String stdout, String stderr) {
        String firstStderrLine() {
            int nl = stderr.indexOf('\n');
            return nl < 0 ? stderr : stderr.substring(0, nl);
        }
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static Path writeTempGo(String name, String src) throws Exception {
        Path dir = Files.createTempDirectory("graalgo-misuse-test");
        Path f = dir.resolve(name);
        Files.writeString(f, src, StandardCharsets.UTF_8);
        return f;
    }

    /** Runs one program in-JVM through P3SyncMain, capturing stdout+stderr. */
    private static RunResult runInJvm(Path goFile) {
        PrintStream oldOut = System.out;
        PrintStream oldErr = System.err;
        ByteArrayOutputStream outBuf = new ByteArrayOutputStream();
        ByteArrayOutputStream errBuf = new ByteArrayOutputStream();
        System.setOut(new PrintStream(outBuf, true, StandardCharsets.UTF_8));
        System.setErr(new PrintStream(errBuf, true, StandardCharsets.UTF_8));
        int code;
        try {
            code = P3SyncMain.run(new String[] {"--gmp=1", goFile.toString()});
        } finally {
            System.setOut(oldOut);
            System.setErr(oldErr);
        }
        return new RunResult(code, outBuf.toString(StandardCharsets.UTF_8),
                errBuf.toString(StandardCharsets.UTF_8));
    }

    /**
     * Runs one program in a forked JVM (the fatal paths call
     * {@code System.exit}, which must not kill the test JVM). The child
     * inherits the test classpath and the module working directory, so
     * gofrontend discovery behaves as in the in-JVM tests.
     */
    private static RunResult runForked(Path goFile) throws Exception {
        String javaBin = System.getProperty("java.home") + "/bin/java";
        ProcessBuilder pb = new ProcessBuilder(javaBin, "--enable-native-access=ALL-UNNAMED",
                "-cp", System.getProperty("java.class.path"),
                "graalgo.p3sync.launcher.P3SyncMain", "--gmp=1", goFile.toString());
        pb.directory(Path.of(System.getProperty("user.dir")).toFile());
        Process p = pb.start();
        String stdout = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
        String stderr = new String(p.getErrorStream().readAllBytes(), StandardCharsets.UTF_8);
        int code = p.waitFor();
        return new RunResult(code, stdout, stderr);
    }

    private static void assertFatal(RunResult r, String message, String prog) {
        assertTrue(r.exit() == 2,
                prog + ": fatal must exit 2, got " + r.exit()
                        + "\nstdout:\n" + r.stdout() + "\nstderr:\n" + r.stderr());
        assertTrue(("fatal error: " + message).equals(r.firstStderrLine()),
                prog + ": first stderr line must be [fatal error: " + message + "], got ["
                        + r.firstStderrLine() + "]\nstderr:\n" + r.stderr());
    }

    // ------------------------------------------------------------------
    // sync.Mutex / sync.RWMutex unlock misuse: gc fatal()s — unrecoverable.
    // ------------------------------------------------------------------

    /** verifies: ADR-0002 D4 — mutex double-unlock is fatal, exit 2. */
    public void testMutexUnlockIsFatal() throws Exception {
        RunResult r = runForked(writeTempGo("mu_fatal.go", PROG_MUTEX_UNLOCK));
        assertFatal(r, "sync: unlock of unlocked mutex", "mutex-unlock");
    }

    /**
     * verifies: ADR-0002 D4 — a guest recover() must NOT succeed where gc
     * dies (the pre-fix behavior: recoverable Go panic).
     */
    public void testMutexUnlockRecoverDoesNotSave() throws Exception {
        RunResult r = runForked(writeTempGo("mu_recover.go", PROG_MUTEX_UNLOCK_RECOVER));
        assertFatal(r, "sync: unlock of unlocked mutex", "mutex-unlock-recover");
        assertTrue(!r.stdout().contains("RECOVERED:"),
                "mutex double-unlock must be unrecoverable; stdout:\n" + r.stdout());
    }

    /** verifies: ADR-0002 D4 — RWMutex double-unlock is fatal, exit 2. */
    public void testRWMutexUnlockIsFatal() throws Exception {
        RunResult r = runForked(writeTempGo("rwu_fatal.go", PROG_RWMUTEX_UNLOCK));
        assertFatal(r, "sync: Unlock of unlocked RWMutex", "rwmutex-unlock");
    }

    /** verifies: ADR-0002 D4 — RWMutex double-runlock is fatal, exit 2. */
    public void testRWMutexRUnlockIsFatal() throws Exception {
        RunResult r = runForked(writeTempGo("rwr_fatal.go", PROG_RWMUTEX_RUNLOCK));
        assertFatal(r, "sync: RUnlock of unlocked RWMutex", "rwmutex-runlock");
    }

    // ------------------------------------------------------------------
    // sync.WaitGroup negative counter: gc panic()s — recoverable.
    // ------------------------------------------------------------------

    /** verifies: ADR-0002 D4 — WaitGroup misuse stays a recoverable panic. */
    public void testWaitGroupNegativeIsRecoverable() throws Exception {
        RunResult r = runInJvm(writeTempGo("wg_rec.go", PROG_WG_NEGATIVE_RECOVERED));
        assertTrue(r.exit() == 0,
                "recovered WaitGroup misuse must exit 0, got " + r.exit()
                        + "\nstderr:\n" + r.stderr());
        assertTrue(r.stdout().contains("RECOVERED: sync: negative WaitGroup counter"),
                "recovered value must be gc's message; stdout:\n" + r.stdout());
    }

    /** verifies: ADR-0002 D4 — uncaught WaitGroup panic: panic: line, exit 2. */
    public void testWaitGroupNegativeUncaughtPanics() throws Exception {
        RunResult r = runInJvm(writeTempGo("wg_uncaught.go", PROG_WG_NEGATIVE_UNCAUGHT));
        assertTrue(r.exit() == 2,
                "uncaught WaitGroup panic must exit 2, got " + r.exit()
                        + "\nstderr:\n" + r.stderr());
        assertTrue(r.firstStderrLine().startsWith("panic: sync: negative WaitGroup counter"),
                "first stderr line must start with [panic: sync: negative WaitGroup counter], got ["
                        + r.firstStderrLine() + "]");
    }

    // ------------------------------------------------------------------
    // runtime.SetFinalizer misuse: gc throw()s — unrecoverable, exit 2.
    // ------------------------------------------------------------------

    /** verifies: ADR-0002 D4 — SetFinalizer(nil, _) is fatal, exit 2. */
    public void testSetFinalizerNilIsFatal() throws Exception {
        RunResult r = runForked(writeTempGo("sf_nil.go", PROG_SF_NIL));
        assertFatal(r, "runtime.SetFinalizer: first argument is nil", "setfinalizer-nil");
    }

    /** verifies: ADR-0002 D4 — SetFinalizer(non-pointer, _) is fatal. */
    public void testSetFinalizerNonPointerIsFatal() throws Exception {
        RunResult r = runForked(writeTempGo("sf_nonptr.go", PROG_SF_NONPOINTER));
        assertFatal(r, "runtime.SetFinalizer: first argument is int, not pointer",
                "setfinalizer-nonpointer");
    }

    /** verifies: ADR-0002 D4 — SetFinalizer(_, non-func) is fatal. */
    public void testSetFinalizerNonFuncIsFatal() throws Exception {
        RunResult r = runForked(writeTempGo("sf_nonfunc.go", PROG_SF_NONFUNC));
        assertFatal(r, "runtime.SetFinalizer: second argument is int, not a function",
                "setfinalizer-nonfunc");
    }

    /** verifies: ADR-0002 D4 — double SetFinalizer is fatal, exit 2. */
    public void testSetFinalizerDoubleSetIsFatal() throws Exception {
        RunResult r = runForked(writeTempGo("sf_double.go", PROG_SF_DOUBLESET));
        assertFatal(r, "runtime.SetFinalizer: finalizer already set", "setfinalizer-doubleset");
    }

    /**
     * verifies: ADR-0002 D4 — a guest recover() must NOT succeed where gc
     * dies (the review's premise was "gc panics"; go1.27.1 throw()s).
     */
    public void testSetFinalizerMisuseRecoverDoesNotSave() throws Exception {
        RunResult r = runForked(writeTempGo("sf_recover.go", PROG_SF_NIL_RECOVER));
        assertFatal(r, "runtime.SetFinalizer: first argument is nil", "setfinalizer-recover");
        assertTrue(!r.stdout().contains("RECOVERED"),
                "SetFinalizer misuse must be unrecoverable; stdout:\n" + r.stdout());
    }
}
