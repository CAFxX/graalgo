package graalgo.p3sync.launcher;

import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * End-to-end tests for the {@code unsafe.Add} D5 trap (P3-M8).
 *
 * <p>verifies: RV22-P1-8 (B-6 interim) — {@code unsafe.Add} must trap with
 * a Go panic carrying the documented message — recoverable via Go
 * {@code recover()} — and never with an internal-error crash
 * ({@code UnsafeEscapeException}, the pre-fix behavior).
 *
 * <p>These tests shell out to nothing: they run real Go programs through
 * {@code P3SyncMain} in-process (same pattern as
 * {@code LinknameEndToEndTest}); they do not involve gc — gc implements
 * {@code unsafe.Add} instead of trapping.
 */
public final class UnsafeAddTrapTest {

    /** The documented trap message (must match {@code UnsafeOps.addTrap}). */
    static final String TRAP_MESSAGE =
            "unsafe.Add: pointer arithmetic on unsafe.Pointer traps "
                    + "(D5: handles admit no byte arithmetic; "
                    + "re-test: byte-addressed value model)";

    private static final String PROG_RECOVER =
            "package main\n\nimport \"unsafe\"\n\n"
            + "const wantTrap = \"" + TRAP_MESSAGE + "\"\n\n"
            + "var result = \"NO-TRAP\"\n\n"
            + "func tryAdd() {\n"
            + "\tdefer func() {\n"
            + "\t\tr := recover()\n"
            + "\t\tif r == wantTrap {\n"
            + "\t\t\tresult = \"RECOVERED-OK\"\n"
            + "\t\t} else {\n"
            + "\t\t\tresult = \"MESSAGE-MISMATCH\"\n"
            + "\t\t}\n"
            + "\t}()\n"
            + "\tp := unsafe.Pointer(new(int))\n"
            + "\t_ = unsafe.Add(p, 1)\n"
            + "}\n\n"
            + "func main() {\n"
            + "\ttryAdd()\n"
            + "\tprintln(result)\n"
            + "\tprintln(\"SURVIVED\")\n"
            + "}\n";

    private static final String PROG_UNCAUGHT =
            "package main\n\nimport \"unsafe\"\n\n"
            + "func main() {\n"
            + "\tp := unsafe.Pointer(new(int))\n"
            + "\t_ = unsafe.Add(p, 1)\n"
            + "}\n";

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static Path writeTempGo(String name, String src) throws Exception {
        Path dir = Files.createTempDirectory("graalgo-unsafeadd-test");
        Path f = dir.resolve(name);
        Files.writeString(f, src, StandardCharsets.UTF_8);
        return f;
    }

    /** Runs one program through P3SyncMain, capturing stdout+stderr. */
    private static RunResult runProgram(Path goFile) {
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

    private record RunResult(int exit, String stdout, String stderr) {}

    /** First stderr line, skipping JVM WARNING noise (harness convention). */
    private static String firstStderrLine(String stderr) {
        for (String line : stderr.split("\n")) {
            String t = line.trim();
            if (t.isEmpty() || t.startsWith("WARNING: ")) {
                continue;
            }
            return t;
        }
        return "";
    }

    /**
     * verifies: RV22-P1-8 — the {@code unsafe.Add} trap is a Go panic:
     * it fires (the {@code NO-TRAP} outcome is never reached), it carries
     * the exact documented message (compared as a Go value inside the
     * deferred function), and Go {@code recover()} catches it so the
     * program continues. Under the old behavior this program died with an
     * internal-error crash (non-zero exit, no {@code RECOVERED-OK}).
     *
     * <p>The program uses the {@code println} builtin (stderr), not
     * {@code fmt}: {@code fmt} is unusable at the current master tip
     * (pre-existing, unrelated to this lane — every {@code fmt} program
     * fails AST-building on {@code internal/cpu.doinit}'s 3-valued
     * {@code ifacemethod} multi-assign, which
     * {@code NodeFactory.buildAssign} rejects).
     */
    public void testAddTrapIsRecoverableGoPanic() throws Exception {
        Path go = writeTempGo("add_recover.go", PROG_RECOVER);
        RunResult r = runProgram(go);
        assertTrue(r.exit() == 0,
                "expected exit 0 (panic recovered), got " + r.exit()
                        + "; stderr: " + r.stderr());
        assertTrue(r.stderr().contains("RECOVERED-OK"),
                "recover() must catch the trap with the exact documented message; stderr=<"
                        + r.stderr() + ">");
        assertTrue(r.stderr().contains("SURVIVED"),
                "program must continue after recover(); stderr=<" + r.stderr() + ">");
        assertTrue(!r.stderr().contains("NO-TRAP"),
                "unsafe.Add must trap (NO-TRAP outcome must be unreachable); stderr=<"
                        + r.stderr() + ">");
        assertTrue(!r.stderr().contains("MESSAGE-MISMATCH"),
                "recovered message must equal the documented message; stderr=<"
                        + r.stderr() + ">");
        assertTrue(!r.stderr().contains("UnsafeEscapeException"),
                "must not crash with the old internal error; stderr=<" + r.stderr() + ">");
        assertTrue(!r.stderr().contains("fatal internal error"),
                "must not crash with an internal error; stderr=<" + r.stderr() + ">");
    }

    /**
     * verifies: RV22-P1-8 — uncaught, the trap renders as a Go panic whose
     * first stderr line is {@code panic: <documented message>} (this pins
     * the corpus {@code 03a_add_trap.panic} expectation in the fast suite).
     */
    public void testAddTrapUncaughtRendersGoPanicLine() throws Exception {
        Path go = writeTempGo("add_uncaught.go", PROG_UNCAUGHT);
        RunResult r = runProgram(go);
        assertTrue(r.exit() != 0,
                "uncaught trap must fail the program, got exit 0");
        String first = firstStderrLine(r.stderr());
        assertTrue(first.equals("panic: " + TRAP_MESSAGE),
                "first stderr line must be the Go panic line, got <" + first
                        + ">; full stderr=<" + r.stderr() + ">");
        assertTrue(!r.stderr().contains("UnsafeEscapeException"),
                "must not crash with the old internal error; stderr=<" + r.stderr() + ">");
    }
}
