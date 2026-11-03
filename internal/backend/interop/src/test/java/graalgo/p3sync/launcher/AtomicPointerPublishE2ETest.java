package graalgo.p3sync.launcher;

import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * P1-1 (REVIEW-2026-11-03-09): end-to-end publication through
 * {@code atomic.LoadPointer} / {@code atomic.StorePointer} on a plain
 * {@code unsafe.Pointer} variable.
 *
 * <p>The writer stores a payload through plain (non-atomic) writes, then
 * publishes its address with {@code StorePointer}; the reader spins on
 * {@code LoadPointer} until it observes a non-nil pointer and then reads
 * the payload through that pointer. Under gc's memory model the store is
 * a release and the load an acquire, so the reader must observe the
 * payload the writer wrote before publishing — never a zero payload.
 * The pre-fix lowering (a plain deref read/write, no ordering edge)
 * permitted the reader to observe the pointer without the payload, with
 * no error anywhere.
 *
 * <p>Honesty note: a concurrent timing test cannot <em>deterministically</em>
 * fail on the old lowering — a stale read is permitted by the memory
 * model, not guaranteed to occur on any given schedule, and on this
 * runtime the old program usually prints the right answer. The
 * deterministic red-green gate is the frontend wire-shape test
 * ({@code TestAtomicPointerOpsLowerToAtomicPtrOp} in
 * {@code internal/frontend/atomic_pointer_p1_test.go}: the old lowering
 * emits {@code deref}/{@code assign} nodes where the new one emits
 * {@code atomicptrop}). This end-to-end test pins the observable
 * behavior instead: publication must actually work, repeatedly, through
 * the real P3SyncMain stack, for both the plain-field path and the
 * {@code atomic.Pointer[T]} method path (whose bodies route through the
 * same package-level calls). A second program pins the sequential
 * semantics of all four ops (load/store/swap/CAS round-trips, CAS
 * failure, nil load) so an ordering fix cannot silently change values.
 */
public final class AtomicPointerPublishE2ETest {

    /**
     * Writer publishes {@code data} via {@code StorePointer}; reader
     * spins on {@code LoadPointer} (yielding with {@code Gosched} so a
     * cooperative schedule still makes progress) and checks the payload.
     * The payload is written as several separate plain stores before
     * publication, so a reader that saw the pointer but not the payload
     * would print a wrong sum, not just a stale pointer.
     */
    private static final String PROG_PUBLISH =
            "package main\n\nimport (\n\t\"fmt\"\n\t\"runtime\"\n\t\"sync/atomic\"\n\t\"unsafe\"\n)\n\n"
            + "var data [4]int\n"
            + "var p unsafe.Pointer\n\n"
            + "func main() {\n"
            + "\tgo func() {\n"
            + "\t\tdata[0] = 10\n"
            + "\t\tdata[1] = 20\n"
            + "\t\tdata[2] = 30\n"
            + "\t\tdata[3] = 40\n"
            + "\t\tatomic.StorePointer(&p, unsafe.Pointer(&data))\n"
            + "\t}()\n"
            + "\tfor atomic.LoadPointer(&p) == nil {\n"
            + "\t\truntime.Gosched()\n"
            + "\t}\n"
            + "\tgot := (*[4]int)(atomic.LoadPointer(&p))\n"
            + "\tfmt.Println(got[0] + got[1] + got[2] + got[3])\n"
            + "}\n";

    /**
     * Sequential semantics of all four ops on a plain field, plus the
     * {@code atomic.Pointer[T]} method path (its bodies call the same
     * package-level functions). Expected output: stored value, swapped
     * old value, CAS success, CAS failure, method round-trip, nil load.
     */
    private static final String PROG_SEMANTICS =
            "package main\n\nimport (\n\t\"fmt\"\n\t\"sync/atomic\"\n\t\"unsafe\"\n)\n\n"
            + "func main() {\n"
            + "\tvar p unsafe.Pointer\n"
            + "\ta := 1\n"
            + "\tb := 2\n"
            + "\tatomic.StorePointer(&p, unsafe.Pointer(&a))\n"
            + "\tfmt.Println(*(*int)(atomic.LoadPointer(&p)))\n"
            + "\told := atomic.SwapPointer(&p, unsafe.Pointer(&b))\n"
            + "\tfmt.Println(*(*int)(old))\n"
            + "\tfmt.Println(atomic.CompareAndSwapPointer(&p, unsafe.Pointer(&b), unsafe.Pointer(&a)))\n"
            + "\tfmt.Println(atomic.CompareAndSwapPointer(&p, unsafe.Pointer(&b), unsafe.Pointer(&a)))\n"
            + "\tvar mp atomic.Pointer[int]\n"
            + "\tmp.Store(&b)\n"
            + "\tfmt.Println(*mp.Load())\n"
            + "\tvar nq atomic.Pointer[int]\n"
            + "\tif nq.Load() == nil {\n"
            + "\t\tfmt.Println(\"nil\")\n"
            + "\t}\n"
            + "}\n";
    // Semantics program output, in order: 1 (stored a), 1 (swap's old),
    // true (CAS b->a succeeds), false (CAS b->a now fails: current is
    // a), 2 (method round-trip), nil (zero Pointer load).

    private static final int REPS = 10;

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static Path writeTempGo(String name, String src) throws Exception {
        Path dir = Files.createTempDirectory("graalgo-atomic-ptr-test");
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
                            + ">, got <" + r.stdout().trim() + ">");
        }
    }

    /** Publication: the reader must see the full payload (sum 100), every rep. */
    public void testPublishSeesPayload() throws Exception {
        checkRepeated("atomicptrpublish.go", PROG_PUBLISH, "100");
    }

    /** Sequential semantics of all four ops plus the Pointer[T] method path. */
    public void testPointerOpSemantics() throws Exception {
        checkRepeated("atomicptrsemantics.go", PROG_SEMANTICS,
                "1\n1\ntrue\nfalse\n2\nnil");
    }
}
