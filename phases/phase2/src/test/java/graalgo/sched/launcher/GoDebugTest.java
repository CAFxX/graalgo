package graalgo.sched.launcher;

import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.HexFormat;
import java.util.List;
import java.util.concurrent.TimeUnit;

/**
 * P2-M5 (WS E): GODEBUG surface triage tests.
 *
 * <p>Unit tests cover the parser/validator and the versioned registry;
 * subprocess tests cover the SchedMain startup hook end-to-end.
 *
 * <p>verifies: GG-0055 (GODEBUG triage: versioned registry, unknown-knob
 * deny-with-error, godebug-diff green)
 */
public final class GoDebugTest {

    // ------------------------------------------------------------------
    // Registry machine checks
    // ------------------------------------------------------------------

    /** verifies: GG-0055 (registry versioned; knob add/remove must bump). */
    public void testRegistryVersion() {
        assertEquals(2, GoDebug.registryVersion(), "registry version");
    }

    /**
     * verifies: GG-0055 (any knob add/remove/rename breaks the hash until
     * the version bump + hash update is made deliberately).
     */
    public void testRegistryKnobHash() throws Exception {
        String joined = String.join(",", GoDebug.knobNames());
        MessageDigest md = MessageDigest.getInstance("SHA-256");
        String hash = HexFormat.of().formatHex(
                md.digest(joined.getBytes(StandardCharsets.UTF_8)));
        // If this fails, the registry changed: bump the version in
        // registry.json AND update EXPECTED_HASH here in the same commit.
        final String EXPECTED_HASH =
                "944ee8d6d9c148fd25dc6a09c62938164ce8b593ec6ff2608be0c40b86d62e30";
        assertEquals(EXPECTED_HASH, hash, "registry knob-name-set hash");
    }

    /** verifies: GG-0055 (every knob documented). */
    public void testEveryKnobHasJustification() {
        for (String name : GoDebug.knobNames()) {
            GoDebug.Knob k = GoDebug.knob(name);
            assertTrue(k.summary != null && !k.summary.isEmpty(),
                    name + ": missing summary");
            assertTrue(k.justification != null && !k.justification.isEmpty(),
                    name + ": missing justification");
        }
    }

    /** verifies: GG-0055 (registry covers the gc knob universe). */
    public void testRegistryKnobCount() {
        // 33 dbgvars + memprofilerate + 45 non-runtime godebugs = 79.
        assertEquals(79, GoDebug.knobNames().size(), "knob count");
    }

    // ------------------------------------------------------------------
    // Parser / validator unit tests (pure, no subprocess)
    // ------------------------------------------------------------------

    /** verifies: GG-0055 (unknown knob is a hard error — the falsifiable core). */
    public void testUnknownKnobDenied() {
        GoDebug.GoDebugException e = assertThrows("bogusknob=1");
        assertTrue(e.getMessage().contains("unknown GODEBUG"),
                "message names the problem: " + e.getMessage());
        assertTrue(e.getMessage().contains("bogusknob"),
                "message names the knob: " + e.getMessage());
    }

    /** verifies: GG-0055 (typo-class: near-miss of a real knob). */
    public void testNearMissKnobDenied() {
        // "schedtace" (missing r) must not silently behave like unset.
        GoDebug.GoDebugException e = assertThrows("schedtace=100");
        assertTrue(e.getMessage().contains("unknown GODEBUG"), e.getMessage());
    }

    /** verifies: GG-0055 (bare field without '=' is an error, not skipped). */
    public void testBareFieldDenied() {
        GoDebug.GoDebugException e = assertThrows("schedtrace");
        assertTrue(e.getMessage().contains("without '='"), e.getMessage());
    }

    /** verifies: GG-0055 (malformed value is an error, not kept-old). */
    public void testMalformedValueDenied() {
        assertThrows("schedtrace=abc");
        assertThrows("schedtrace=1.5");
        assertThrows("schedtrace=99999999999999999999");
    }

    /** verifies: GG-0055 (unsupported knob denied on non-default). */
    public void testUnsupportedDeniedWhenNonDefault() throws Exception {
        GoDebug.GoDebugException e = assertThrows("scavtrace=1");
        assertTrue(e.getMessage().contains("scavtrace"), e.getMessage());
        assertThrows("gccheckmark=1");
        assertThrows("gctrace=1,scavtrace=1"); // denied even among valid ones
    }

    /** verifies: GG-0055 (unsupported knob accepted at default). */
    public void testUnsupportedAcceptedWhenDefault() throws Exception {
        GoDebug.Settings s = GoDebug.parse("scavtrace=0");
        assertEquals(0, s.get("scavtrace"), "scavtrace");
    }

    /** verifies: GG-0055 (no-op knobs accepted, any value). */
    public void testNoopAccepted() throws Exception {
        GoDebug.Settings s = GoDebug.parse("cgocheck=1,disablethp=1,http2debug=1");
        assertEquals(1, s.get("cgocheck"), "cgocheck");
    }

    /** verifies: GG-0055 (emulated knobs parse). */
    public void testEmulatedParsed() throws Exception {
        GoDebug.Settings s = GoDebug.parse("schedtrace=100,scheddetail=1,gctrace=1,panicnil=1");
        assertEquals(100, s.get("schedtrace"), "schedtrace");
        assertEquals(1, s.get("scheddetail"), "scheddetail");
        assertEquals(1, s.get("gctrace"), "gctrace");
        assertEquals(1, s.get("panicnil"), "panicnil");
        // Unset knobs report their registry defaults.
        assertEquals(0, s.get("asyncpreemptoff"), "default");
    }

    /** verifies: GG-0055 (gc startup semantics: later overrides earlier). */
    public void testLaterOverridesEarlier() throws Exception {
        GoDebug.Settings s = GoDebug.parse("schedtrace=100,schedtrace=0");
        assertEquals(0, s.get("schedtrace"), "override");
    }

    /** verifies: GG-0055 (removed knob: old value denied like gc's fatal). */
    public void testRemovedOldValueDenied() {
        GoDebug.GoDebugException e = assertThrows("asynctimerchan=1");
        assertTrue(e.getMessage().contains("removed"), e.getMessage());
        assertThrows("asynctimerchan=2");
    }

    /** verifies: GG-0055 (removed knob: non-old value accepted like gc). */
    public void testRemovedNewValueAccepted() throws Exception {
        GoDebug.Settings s = GoDebug.parse("asynctimerchan=0");
        assertTrue(true, "accepted");
    }

    /** verifies: GG-0055 (empty / null env is fine). */
    public void testEmptyEnv() throws Exception {
        GoDebug.Settings s = GoDebug.parse("");
        assertEquals(0, s.get("schedtrace"), "empty");
        GoDebug.Settings s2 = GoDebug.parse(null);
        assertEquals(0, s2.get("gctrace"), "null");
        GoDebug.Settings s3 = GoDebug.parse("schedtrace=100,");
        assertEquals(100, s3.get("schedtrace"), "trailing comma");
    }

    /** verifies: GG-0055 (gc fatals on cgocheck>1; we deny too). */
    public void testCgocheck2Denied() {
        GoDebug.GoDebugException e = assertThrows("cgocheck=2");
        assertTrue(e.getMessage().contains("cgocheck"), e.getMessage());
    }

    /** verifies: GG-0055 (non-ASCII digits rejected like Go's ParseInt). */
    public void testNonAsciiDigitsDenied() {
        // Arabic-Indic digits: Long.parseLong accepts, Go rejects.
        assertThrows("schedtrace=١٢٣");
    }

    /** verifies: GG-0055 (memprofilerate accepts full int range like gc). */
    public void testMemprofilerateHugeAccepted() throws Exception {
        GoDebug.Settings s = GoDebug.parse("memprofilerate=99999999999");
        assertTrue(true, "accepted");
    }

    /** verifies: GG-0055 (negative schedtrace denied at apply time). */
    public void testNegativeSchedtraceDenied() throws Exception {
        try {
            GoDebug.applySettings(GoDebug.parse("schedtrace=-1"));
        } catch (GoDebug.GoDebugException e) {
            assertTrue(e.getMessage().contains("schedtrace"), e.getMessage());
            return;
        }
        throw new AssertionError("expected GoDebugException for schedtrace=-1");
    }

    // ------------------------------------------------------------------
    // applySettings side effects (with cleanup)
    // ------------------------------------------------------------------

    /** verifies: GG-0055 (emulated knobs install). */
    public void testApplySettingsInstalls() throws Exception {
        boolean prevPanicNil = graalgo.runtime.Panic.isPanicNilOldBehavior();
        try {
            GoDebug.applySettings(GoDebug.parse("schedtrace=50,panicnil=1,gctrace=1"));
            assertTrue(graalgo.sched.SchedTrace.isRunning(), "schedtrace daemon");
            assertTrue(graalgo.runtime.Panic.isPanicNilOldBehavior(), "panicnil flag");
            assertTrue(GcTrace.isStarted(), "gctrace listener");
        } finally {
            graalgo.sched.SchedTrace.stop();
            graalgo.runtime.Panic.setPanicNilOldBehavior(prevPanicNil);
            GcTrace.resetForTests();
        }
    }

    // ------------------------------------------------------------------
    // Subprocess tests: SchedMain startup hook end-to-end
    // ------------------------------------------------------------------

    /** verifies: GG-0055 (unknown knob denies at startup, exit 2). */
    public void testUnknownKnobDeniesAtStartup() throws Exception {
        ProcessResult r = runSchedMain("bogusknob=1", "db01_hello.go");
        assertEquals(2, r.exitCode, "exit code");
        assertTrue(r.stderr.contains("unknown GODEBUG"),
                "stderr names the problem:\n" + r.stderr);
        assertTrue(r.stderr.contains("bogusknob"), "stderr names the knob");
    }

    /** verifies: GG-0055 (unsupported non-default denies at startup). */
    public void testUnsupportedDeniesAtStartup() throws Exception {
        ProcessResult r = runSchedMain("scavtrace=1", "db01_hello.go");
        assertEquals(2, r.exitCode, "exit code");
        assertTrue(r.stderr.contains("unsupported GODEBUG"), "stderr:\n" + r.stderr);
    }

    /** verifies: GG-0055 (valid GODEBUG runs the program; schedtrace emits). */
    public void testSchedtraceEmitsLines() throws Exception {
        ProcessResult r = runSchedMain("schedtrace=50", "db04_spin.go");
        assertEquals(0, r.exitCode, "exit code\nstderr:\n" + r.stderr);
        assertTrue(r.stdout.contains("spun"), "program ran:\n" + r.stdout);
        int lines = 0;
        for (String line : r.stderr.split("\n")) {
            if (line.startsWith("SCHED ") && line.contains("goroutines=")) {
                lines++;
            }
        }
        assertTrue(lines >= 1, "expected >=1 graalgo SCHED line, stderr:\n" + r.stderr);
    }

    // ------------------------------------------------------------------
    // Harness
    // ------------------------------------------------------------------

    private static final Path CORPUS = Paths.get("corpus/godebug");

    private static final class ProcessResult {
        final String stdout;
        final String stderr;
        final int exitCode;
        ProcessResult(String stdout, String stderr, int exitCode) {
            this.stdout = stdout;
            this.stderr = stderr;
            this.exitCode = exitCode;
        }
    }

    private ProcessResult runSchedMain(String godebug, String program) throws Exception {
        Path goFile = CORPUS.resolve(program).toAbsolutePath();
        String javaBin = Paths.get(System.getProperty("java.home"), "bin", "java").toString();
        List<String> cmd = new ArrayList<>();
        cmd.add(javaBin);
        cmd.add("--enable-native-access=ALL-UNNAMED");
        cmd.add("-Xss32m");
        cmd.add("-cp");
        cmd.add(System.getProperty("java.class.path"));
        cmd.add("graalgo.sched.launcher.SchedMain");
        cmd.add("--gmp=1");
        cmd.add(goFile.toString());
        ProcessBuilder pb = new ProcessBuilder(cmd);
        pb.environment().put("GODEBUG", godebug);
        Process p = pb.start();
        java.util.concurrent.Future<String> outF = readAsync(p.getInputStream());
        java.util.concurrent.Future<String> errF = readAsync(p.getErrorStream());
        boolean done = p.waitFor(300, TimeUnit.SECONDS);
        if (!done) {
            p.destroyForcibly();
            throw new AssertionError(program + ": subprocess timed out");
        }
        String stderr;
        try {
            stderr = errF.get(10, TimeUnit.SECONDS);
        } catch (Exception e) {
            throw new AssertionError(program + ": failed to read stderr", e);
        }
        String stdout;
        try {
            stdout = outF.get(10, TimeUnit.SECONDS);
        } catch (Exception e) {
            throw new AssertionError(program + ": failed to read stdout", e);
        }
        return new ProcessResult(stripJvmWarnings(stdout), stripJvmWarnings(stderr),
                p.exitValue());
    }

    private static java.util.concurrent.Future<String> readAsync(java.io.InputStream in) {
        java.util.concurrent.ExecutorService ex =
                java.util.concurrent.Executors.newSingleThreadExecutor();
        java.util.concurrent.Future<String> f = ex.submit(() -> {
            try (var stream = in) {
                return new String(stream.readAllBytes(), StandardCharsets.UTF_8);
            }
        });
        ex.shutdown();
        return f;
    }

    private static String stripJvmWarnings(String s) {
        StringBuilder sb = new StringBuilder();
        for (String line : s.split("\n", -1)) {
            if (line.startsWith("WARNING:") || line.startsWith("OpenJDK 64-Bit Server VM warning:")) {
                continue;
            }
            if (sb.length() > 0) {
                sb.append('\n');
            }
            sb.append(line);
        }
        return sb.toString();
    }

    // --- minimal assertions (mirrors the repo's no-framework style) ---

    private static void assertEquals(long expected, long actual, String what) {
        if (expected != actual) {
            throw new AssertionError(what + ": expected " + expected + " but got " + actual);
        }
    }

    private static void assertEquals(String expected, String actual, String what) {
        if (!expected.equals(actual)) {
            throw new AssertionError(what + ": expected <" + expected + "> but got <" + actual + ">");
        }
    }

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError("expected true: " + what);
        }
    }

    private static GoDebug.GoDebugException assertThrows(String godebug) {
        try {
            GoDebug.parse(godebug);
        } catch (GoDebug.GoDebugException e) {
            return e;
        }
        throw new AssertionError("expected GoDebugException for: " + godebug);
    }
}
