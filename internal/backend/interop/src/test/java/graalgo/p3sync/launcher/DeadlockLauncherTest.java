package graalgo.p3sync.launcher;

import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;

/**
 * RV-P1-19 launcher tests (design §6.2): {@code P3SyncMain.run}
 * in-process (no {@code System.exit}), stderr captured, Go sources
 * written under {@code out/test-tmp} (never {@code /tmp} — shared 512M
 * tmpfs).
 *
 * <p>Why {@code P3SyncMain} and not {@code SchedMain}: the phase-2
 * {@code SchedMain} frontend never registered the {@code select} /
 * {@code makechan} / {@code send} / {@code recv} wire-kind hooks, so it
 * cannot even parse the deadlock programs (it fails in AST building,
 * long before the scheduler). {@code P3SyncMain} is the product
 * launcher — the same backend the differential corpus runs against —
 * and carries the identical C1/C2/C3 deadlock contract, so the exit
 * codes and gc-identical fatal lines are asserted here.
 *
 * <p>Each run executes on a watchdog thread: a broken detector/launcher
 * must fail loudly (timeout → assertion), never hang the suite.
 *
 * <p>verifies: RV-P1-19 (C1/C2/C3 launcher contract — exit codes and the
 * gc-identical fatal lines).
 */
public final class DeadlockLauncherTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(int expected, int actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    private static final Path TMP =
            Paths.get("out/test-tmp/deadlock-launcher");

    private static final class RunResult {
        final int exitCode;
        final String stderr;

        RunResult(int exitCode, String stderr) {
            this.exitCode = exitCode;
            this.stderr = stderr;
        }

        String firstStderrLine() {
            int nl = stderr.indexOf('\n');
            return nl < 0 ? stderr : stderr.substring(0, nl);
        }
    }

    /**
     * Runs {@code P3SyncMain.run} on {@code program} (written to a temp
     * .go file) with stderr captured. Watchdog: 120s, then loud failure.
     */
    private static RunResult runGo(String name, String program) throws Exception {
        Files.createDirectories(TMP);
        Path goFile = TMP.resolve(name + ".go");
        Files.writeString(goFile, program, StandardCharsets.UTF_8);
        PrintStream origErr = System.err;
        ByteArrayOutputStream errBuf = new ByteArrayOutputStream();
        System.setErr(new PrintStream(errBuf, true, StandardCharsets.UTF_8));
        AtomicInteger code = new AtomicInteger(-1);
        AtomicReference<Throwable> thrown = new AtomicReference<>();
        Thread t = Thread.ofPlatform().daemon(true).name("deadlock-launcher-" + name)
                .start(() -> {
                    try {
                        code.set(P3SyncMain.run(
                                new String[]{"--gmp=2", goFile.toString()}));
                    } catch (Throwable th) {
                        thrown.set(th);
                    }
                });
        try {
            t.join(TimeUnit.SECONDS.toMillis(120));
        } finally {
            System.setErr(origErr);
        }
        if (t.isAlive()) {
            throw new AssertionError(name + ": P3SyncMain.run hung (watchdog 120s)");
        }
        if (thrown.get() != null) {
            throw new AssertionError(name + ": P3SyncMain.run threw", thrown.get());
        }
        return new RunResult(code.get(), errBuf.toString(StandardCharsets.UTF_8));
    }

    /**
     * {@code func main() { select {} }} → exit 2, gc-identical fatal
     * first line plus a goroutine dump (gc probe p1).
     */
    public void testSelectEmptyDeadlocks() throws Exception {
        RunResult r = runGo("select_empty",
                "package main\n\nfunc main() { select {} }\n");
        assertEquals(2, r.exitCode, "select{} exit code");
        assertTrue(r.firstStderrLine()
                        .equals("fatal error: all goroutines are asleep - deadlock!"),
                "gc-identical fatal line, stderr was:\n" + r.stderr);
        // Id-agnostic: goroutine ids come from a process-global counter
        // shared with every other test in the runner JVM, so main's id is
        // not necessarily 1 — only the label shape is asserted.
        assertTrue(java.util.regex.Pattern.compile("(?m)^goroutine \\d+ \\[select\\]:$")
                        .matcher(r.stderr).find(),
                "dump names a goroutine with the select label, stderr was:\n" + r.stderr);
    }

    /**
     * Fire-and-forget child + main returns → exit 0 (gc probe p2; C1).
     * Would hang before C1; would falsely fatal with the detector but
     * without C1.
     */
    public void testMainReturnExitsZero() throws Exception {
        RunResult r = runGo("main_returns",
                "package main\n\nfunc main() { go func() { select {} }() }\n");
        assertEquals(0, r.exitCode,
                "main return abandons the blocked child, stderr was:\n" + r.stderr);
        assertTrue(!r.stderr.contains("deadlock"),
                "no deadlock report, stderr was:\n" + r.stderr);
    }

    /**
     * {@code runtime.Goexit()} in main alone → exit 2 with gc's distinct
     * message (gc probe p3; C2).
     */
    public void testGoexitAloneMessage() throws Exception {
        RunResult r = runGo("goexit_alone",
                "package main\n\nimport \"runtime\"\n\nfunc main() { runtime.Goexit() }\n");
        assertEquals(2, r.exitCode, "Goexit-alone exit code");
        assertTrue(r.firstStderrLine().equals(
                        "fatal error: no goroutines (main called runtime.Goexit) - deadlock!"),
                "gc-identical Goexit message, stderr was:\n" + r.stderr);
    }

    /**
     * Non-deadlock control: main receives while a child sends — the
     * send/recv rendezvous, so this completes with exit 0 (contrast with
     * the crossed variant below, which genuinely deadlocks).
     */
    public void testChannelRendezvousControl() throws Exception {
        RunResult r = runGo("chan_pair",
                "package main\n\n"
                        + "func main() {\n"
                        + "    ch := make(chan int)\n"
                        + "    go func() { ch <- 1 }()\n"
                        + "    <-ch\n"
                        + "}\n");
        // Note: the send/recv rendezvous, so this completes with exit 0 —
        // it is the non-deadlock control. The real deadlock pair is the
        // crossed variant below.
        assertEquals(0, r.exitCode, "rendezvous pair completes, stderr was:\n" + r.stderr);
    }

    /**
     * Crossed channel deadlock: main receives on ch1 while the child
     * receives on ch2, each holding the other's send — neither can ever
     * proceed → exit 2 with dump.
     */
    public void testCrossedChannelDeadlock() throws Exception {
        RunResult r = runGo("chan_crossed",
                "package main\n\n"
                        + "func main() {\n"
                        + "    ch1 := make(chan int)\n"
                        + "    ch2 := make(chan int)\n"
                        + "    go func() { <-ch2; ch1 <- 1 }()\n"
                        + "    <-ch1\n"
                        + "}\n");
        assertEquals(2, r.exitCode, "crossed channel deadlock exit code");
        assertTrue(r.firstStderrLine()
                        .equals("fatal error: all goroutines are asleep - deadlock!"),
                "gc-identical fatal line, stderr was:\n" + r.stderr);
    }
}
