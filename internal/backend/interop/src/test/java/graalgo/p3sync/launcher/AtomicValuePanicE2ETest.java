package graalgo.p3sync.launcher;

import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * HELD — NOT registered in IfaceTestRunner (deliberately).
 *
 * <p>End-to-end tests for the RV-P0-07 {@code atomic.Value}
 * type-consistency protocol: real Go programs through the full
 * P3SyncMain stack, each asserting the gc-verbatim panic string via
 * defer/recover. The 12 programs are validated against real gc by the
 * RV-P0-07 lane's differential probe (all produce the asserted outputs
 * under {@code go run}).
 *
 * <p>Written by the fix-p0-atomic-value (RV-P0-07) lane. The
 * fix-p0-atomic-lowering lane (2026-10-17) restored the method-call →
 * syncop lowering, resolving blocker (1) below, and verified the 12
 * programs export to the correct wire shapes — but the test still cannot
 * pass at this tree for a pre-existing reason unrelated to either lane:
 * the real {@code fmt} package's comma-ok/multi-result assigns
 * (e.g. {@code w.err, _ = w.arg.(error)}, {@code r, size, err :=
 * r.ReadRune()}) export as a single RHS node against a multi-target LHS,
 * and the backend's AST builder rejects them ("assign.fields: lhs/rhs
 * length mismatch"). Reproduces with fmt+recover and no atomic code at
 * all. Register this class in {@code IfaceTestRunner} once the fmt
 * multi-result assign export is fixed.
 * verifies: RV-P0-07, RV-P0-atomic-lowering (when registered)
 */
public final class AtomicValuePanicE2ETest {

    private static final String PREAMBLE =
            "package main\n\nimport (\n\t\"fmt\"\n\t\"sync/atomic\"\n)\n\n"
            + "func main() {\n"
            + "\tdefer func() {\n"
            + "\t\tif r := recover(); r != nil {\n"
            + "\t\t\tfmt.Println(r)\n"
            + "\t\t}\n"
            + "\t}()\n";

    private static String panicProg(String body) {
        return PREAMBLE + body + "}\n";
    }

    private static final String PROG_STORE_NIL = panicProg(
            "\tvar v atomic.Value\n\tv.Store(nil)\n\tfmt.Println(\"NO PANIC\")\n");

    private static final String PROG_STORE_MISMATCH = panicProg(
            "\tvar v atomic.Value\n\tv.Store(1)\n\tv.Store(\"x\")\n\tfmt.Println(\"NO PANIC\")\n");

    private static final String PROG_SWAP_NIL = panicProg(
            "\tvar v atomic.Value\n\tv.Swap(nil)\n\tfmt.Println(\"NO PANIC\")\n");

    private static final String PROG_SWAP_MISMATCH = panicProg(
            "\tvar v atomic.Value\n\tv.Store(1)\n\tv.Swap(\"x\")\n\tfmt.Println(\"NO PANIC\")\n");

    private static final String PROG_CAS_NIL = panicProg(
            "\tvar v atomic.Value\n\tv.Store(1)\n\tv.CompareAndSwap(1, nil)\n\tfmt.Println(\"NO PANIC\")\n");

    private static final String PROG_CAS_OLD_NEW_MISMATCH = panicProg(
            "\tvar v atomic.Value\n\tv.Store(1)\n\tv.CompareAndSwap(\"a\", 2)\n\tfmt.Println(\"NO PANIC\")\n");

    private static final String PROG_CAS_STORED_MISMATCH = panicProg(
            "\tvar v atomic.Value\n\tv.Store(1)\n\tv.CompareAndSwap(\"a\", \"b\")\n\tfmt.Println(\"NO PANIC\")\n");

    /** Deterministic two-type race: filled with ints, then a string store must panic. */
    private static final String PROG_TWO_TYPE_RACE = panicProg(
            "\tvar v atomic.Value\n\tv.Store(42)\n\tv.Store(\"boom\")\n\tfmt.Println(\"NO PANIC\")\n");

    /** Zero Value Load must be nil (regression: already worked). */
    private static final String PROG_ZERO_LOAD =
            "package main\n\nimport \"fmt\"\n\nimport \"sync/atomic\"\n\n"
            + "func main() {\n"
            + "\tvar v atomic.Value\n"
            + "\tif v.Load() == nil {\n"
            + "\t\tfmt.Println(\"NIL\")\n"
            + "\t} else {\n"
            + "\t\tfmt.Println(\"NOT NIL\")\n"
            + "\t}\n"
            + "}\n";

    /** Swap on an empty Value returns nil and stores. */
    private static final String PROG_SWAP_EMPTY =
            "package main\n\nimport \"fmt\"\n\nimport \"sync/atomic\"\n\n"
            + "func main() {\n"
            + "\tvar v atomic.Value\n"
            + "\tif v.Swap(7) != nil {\n"
            + "\t\tfmt.Println(\"SWAP NOT NIL\")\n"
            + "\t\treturn\n"
            + "\t}\n"
            + "\tif v.Load() != 7 {\n"
            + "\t\tfmt.Println(\"LOAD NOT 7\")\n"
            + "\t\treturn\n"
            + "\t}\n"
            + "\tfmt.Println(\"OK\")\n"
            + "}\n";

    /** CAS first-store protocol: nil old stores and returns true; non-nil old returns false. */
    private static final String PROG_CAS_FIRST_STORE =
            "package main\n\nimport \"fmt\"\n\nimport \"sync/atomic\"\n\n"
            + "func main() {\n"
            + "\tvar v atomic.Value\n"
            + "\tif v.CompareAndSwap(1, 2) {\n"
            + "\t\tfmt.Println(\"NON-NIL OLD STORED\")\n"
            + "\t\treturn\n"
            + "\t}\n"
            + "\tif v.Load() != nil {\n"
            + "\t\tfmt.Println(\"CELL NOT EMPTY\")\n"
            + "\t\treturn\n"
            + "\t}\n"
            + "\tif !v.CompareAndSwap(nil, 3) {\n"
            + "\t\tfmt.Println(\"NIL OLD FAILED\")\n"
            + "\t\treturn\n"
            + "\t}\n"
            + "\tif v.Load() != 3 {\n"
            + "\t\tfmt.Println(\"LOAD NOT 3\")\n"
            + "\t\treturn\n"
            + "\t}\n"
            + "\tfmt.Println(\"OK\")\n"
            + "}\n";

    /** Same-type concurrent hammer: must never panic; final Load is an int. */
    private static final String PROG_HAMMER =
            "package main\n\nimport (\n\t\"fmt\"\n\t\"sync\"\n\t\"sync/atomic\"\n)\n\n"
            + "func main() {\n"
            + "\tvar v atomic.Value\n"
            + "\tv.Store(0)\n"
            + "\tvar wg sync.WaitGroup\n"
            + "\tfor w := 0; w < 8; w++ {\n"
            + "\t\twg.Add(1)\n"
            + "\t\tgo func(w int) {\n"
            + "\t\t\tdefer wg.Done()\n"
            + "\t\t\tfor i := 0; i < 200; i++ {\n"
            + "\t\t\t\tx := w*1000 + i\n"
            + "\t\t\t\tv.Store(x)\n"
            + "\t\t\t\tv.Swap(x + 1)\n"
            + "\t\t\t\tv.CompareAndSwap(x+1, x+2)\n"
            + "\t\t\t}\n"
            + "\t\t}(w)\n"
            + "\t}\n"
            + "\twg.Wait()\n"
            + "\tswitch v.Load().(type) {\n"
            + "\tcase int:\n"
            + "\t\tfmt.Println(\"OK\")\n"
            + "\tdefault:\n"
            + "\t\tfmt.Println(\"BAD\")\n"
            + "\t}\n"
            + "}\n";

    private static final int REPS = 3;

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static Path writeTempGo(String name, String src) throws Exception {
        Path dir = Files.createTempDirectory("graalgo-atomic-value-e2e");
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
            code = P3SyncMain.run(new String[] {goFile.toString()});
        } finally {
            System.setOut(oldOut);
            System.setErr(oldErr);
        }
        return new RunResult(code, outBuf.toString(StandardCharsets.UTF_8),
                errBuf.toString(StandardCharsets.UTF_8));
    }

    private record RunResult(int exit, String stdout, String stderr) {}

    private static void checkRepeated(String name, String prog, String expect) throws Exception {
        Path go = writeTempGo(name, prog);
        for (int i = 0; i < REPS; i++) {
            RunResult r = runProgram(go);
            assertTrue(r.exit() == 0,
                    name + " rep " + i + ": expected exit 0, got " + r.exit()
                            + "; stderr: " + r.stderr());
            assertTrue(r.stdout().trim().equals(expect),
                    name + " rep " + i + ": expected stdout <" + expect
                            + ">, got <" + r.stdout().trim() + ">; stderr: " + r.stderr());
        }
    }

    public void testE2EStoreNil() throws Exception {
        checkRepeated("store_nil.go", PROG_STORE_NIL,
                "sync/atomic: store of nil value into Value");
    }

    public void testE2EStoreMismatch() throws Exception {
        checkRepeated("store_mismatch.go", PROG_STORE_MISMATCH,
                "sync/atomic: store of inconsistently typed value into Value");
    }

    public void testE2ESwapNil() throws Exception {
        checkRepeated("swap_nil.go", PROG_SWAP_NIL,
                "sync/atomic: swap of nil value into Value");
    }

    public void testE2ESwapMismatch() throws Exception {
        checkRepeated("swap_mismatch.go", PROG_SWAP_MISMATCH,
                "sync/atomic: swap of inconsistently typed value into Value");
    }

    public void testE2ECasNil() throws Exception {
        checkRepeated("cas_nil.go", PROG_CAS_NIL,
                "sync/atomic: compare and swap of nil value into Value");
    }

    public void testE2ECasOldNewMismatch() throws Exception {
        checkRepeated("cas_oldnew.go", PROG_CAS_OLD_NEW_MISMATCH,
                "sync/atomic: compare and swap of inconsistently typed values");
    }

    public void testE2ECasStoredMismatch() throws Exception {
        checkRepeated("cas_stored.go", PROG_CAS_STORED_MISMATCH,
                "sync/atomic: compare and swap of inconsistently typed value into Value");
    }

    public void testE2ETwoTypeRace() throws Exception {
        checkRepeated("two_type_race.go", PROG_TWO_TYPE_RACE,
                "sync/atomic: store of inconsistently typed value into Value");
    }

    public void testE2EZeroValueLoadNil() throws Exception {
        checkRepeated("zero_load.go", PROG_ZERO_LOAD, "NIL");
    }

    public void testE2ESwapEmpty() throws Exception {
        checkRepeated("swap_empty.go", PROG_SWAP_EMPTY, "OK");
    }

    public void testE2ECasFirstStore() throws Exception {
        checkRepeated("cas_first.go", PROG_CAS_FIRST_STORE, "OK");
    }

    public void testE2ESameTypeHammer() throws Exception {
        checkRepeated("hammer.go", PROG_HAMMER, "OK");
    }
}
