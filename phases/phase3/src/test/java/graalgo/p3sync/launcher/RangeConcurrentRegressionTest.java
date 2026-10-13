package graalgo.p3sync.launcher;

import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * Regression test for M10 F-1 (scheduler lost goroutine partial sums).
 *
 * <p>Root cause: the {@code Range*Node} family
 * ({@code RangeSliceNode}, {@code RangeArrayNode},
 * {@code RangeStringNode}, {@code RangeMapNode}, {@code RangeIntNode})
 * kept iteration state ({@code started}, the range operand, the index)
 * in <em>node instance fields</em>. Truffle AST node objects are shared
 * by every goroutine executing the same compiled function, so concurrent
 * workers racing through the same range loop clobbered each other's
 * slice/length/index — one worker could iterate another worker's slice,
 * see a stale index, or terminate early, silently losing partial sums
 * (e.g. {@code 14_goroutines_sum} printing 32/34 instead of 36,
 * non-deterministically). The fix moves iteration state to per-invocation
 * frame slots; these tests pin the fix by running concurrent range loops
 * over all five rangeable kinds repeatedly through the real P3SyncMain
 * stack.
 *
 * <p>Runs each program several times: the bug was flaky (a scheduling
 * race), so a single run cannot catch a regression.
 */
public final class RangeConcurrentRegressionTest {

    /** F-1's exact shape: 4 workers sum slice partitions, collect via channel. */
    private static final String PROG_SLICE =
            "package main\n\nimport (\n\t\"fmt\"\n\t\"sync\"\n)\n\n"
            + "func main() {\n"
            + "\tnums := []int{1, 2, 3, 4, 5, 6, 7, 8}\n"
            + "\tch := make(chan int, 4)\n"
            + "\tvar wg sync.WaitGroup\n"
            + "\tfor w := 0; w < 4; w++ {\n"
            + "\t\twg.Add(1)\n"
            + "\t\tgo func(w int) {\n"
            + "\t\t\tdefer wg.Done()\n"
            + "\t\t\tsum := 0\n"
            + "\t\t\tfor _, v := range nums[w*2 : w*2+2] {\n"
            + "\t\t\t\tsum += v\n"
            + "\t\t\t}\n"
            + "\t\t\tch <- sum\n"
            + "\t\t}(w)\n"
            + "\t}\n"
            + "\tgo func() { wg.Wait(); close(ch) }()\n"
            + "\ttotal := 0\n"
            + "\tfor s := range ch {\n"
            + "\t\ttotal += s\n"
            + "\t}\n"
            + "\tfmt.Println(total)\n"
            + "}\n";

    /** Concurrent range over arrays: each worker sums its own array. */
    private static final String PROG_ARRAY =
            "package main\n\nimport (\n\t\"fmt\"\n\t\"sync\"\n)\n\n"
            + "func main() {\n"
            + "\tch := make(chan int, 4)\n"
            + "\tvar wg sync.WaitGroup\n"
            + "\tfor w := 0; w < 4; w++ {\n"
            + "\t\twg.Add(1)\n"
            + "\t\tgo func(w int) {\n"
            + "\t\t\tdefer wg.Done()\n"
            + "\t\t\ta := [3]int{w * 10, w*10 + 1, w*10 + 2}\n"
            + "\t\t\tsum := 0\n"
            + "\t\t\tfor _, v := range a {\n"
            + "\t\t\t\tsum += v\n"
            + "\t\t\t}\n"
            + "\t\t\tch <- sum\n"
            + "\t\t}(w)\n"
            + "\t}\n"
            + "\tgo func() { wg.Wait(); close(ch) }()\n"
            + "\ttotal := 0\n"
            + "\tfor s := range ch {\n"
            + "\t\ttotal += s\n"
            + "\t}\n"
            + "\tfmt.Println(total)\n"
            + "}\n";
    // Per-worker sums: 0+1+2=3, 10+11+12=33, 20+21+22=63, 30+31+32=93; total 192.

    /** Concurrent range over strings: each worker counts runes. */
    private static final String PROG_STRING =
            "package main\n\nimport (\n\t\"fmt\"\n\t\"sync\"\n)\n\n"
            + "func main() {\n"
            + "\tstrs := []string{\"hello\", \"world!\", \"go\", \"tru\\u00fffle\"}\n"
            + "\tch := make(chan int, 4)\n"
            + "\tvar wg sync.WaitGroup\n"
            + "\tfor w := 0; w < 4; w++ {\n"
            + "\t\twg.Add(1)\n"
            + "\t\tgo func(w int) {\n"
            + "\t\t\tdefer wg.Done()\n"
            + "\t\t\tn := 0\n"
            + "\t\t\tfor range strs[w] {\n"
            + "\t\t\t\tn++\n"
            + "\t\t\t}\n"
            + "\t\t\tch <- n\n"
            + "\t\t}(w)\n"
            + "\t}\n"
            + "\tgo func() { wg.Wait(); close(ch) }()\n"
            + "\ttotal := 0\n"
            + "\tfor s := range ch {\n"
            + "\t\ttotal += s\n"
            + "\t}\n"
            + "\tfmt.Println(total)\n"
            + "}\n";
    // Rune counts: 5, 6, 2, 7 (ü is one rune); total 20.

    /** Concurrent range over maps: each worker sums its own map's values. */
    private static final String PROG_MAP =
            "package main\n\nimport (\n\t\"fmt\"\n\t\"sync\"\n)\n\n"
            + "func main() {\n"
            + "\tch := make(chan int, 4)\n"
            + "\tvar wg sync.WaitGroup\n"
            + "\tfor w := 0; w < 4; w++ {\n"
            + "\t\twg.Add(1)\n"
            + "\t\tgo func(w int) {\n"
            + "\t\t\tdefer wg.Done()\n"
            + "\t\t\tm := map[int]int{1: w, 2: w * 2, 3: w * 3}\n"
            + "\t\t\tsum := 0\n"
            + "\t\t\tfor _, v := range m {\n"
            + "\t\t\t\tsum += v\n"
            + "\t\t\t}\n"
            + "\t\t\tch <- sum\n"
            + "\t\t}(w)\n"
            + "\t}\n"
            + "\tgo func() { wg.Wait(); close(ch) }()\n"
            + "\ttotal := 0\n"
            + "\tfor s := range ch {\n"
            + "\t\ttotal += s\n"
            + "\t}\n"
            + "\tfmt.Println(total)\n"
            + "}\n";
    // Per-worker sums: 6*w for w in 0..3; total 36.

    /** Concurrent integer range: each worker sums 0..w. */
    private static final String PROG_INT =
            "package main\n\nimport (\n\t\"fmt\"\n\t\"sync\"\n)\n\n"
            + "func main() {\n"
            + "\tch := make(chan int, 4)\n"
            + "\tvar wg sync.WaitGroup\n"
            + "\tfor w := 0; w < 4; w++ {\n"
            + "\t\twg.Add(1)\n"
            + "\t\tgo func(w int) {\n"
            + "\t\t\tdefer wg.Done()\n"
            + "\t\t\tsum := 0\n"
            + "\t\t\tfor i := range w + 1 {\n"
            + "\t\t\t\tsum += i\n"
            + "\t\t\t}\n"
            + "\t\t\tch <- sum\n"
            + "\t\t}(w)\n"
            + "\t}\n"
            + "\tgo func() { wg.Wait(); close(ch) }()\n"
            + "\ttotal := 0\n"
            + "\tfor s := range ch {\n"
            + "\t\ttotal += s\n"
            + "\t}\n"
            + "\tfmt.Println(total)\n"
            + "}\n";
    // Per-worker sums: 0, 1, 3, 6; total 10.

    /** Repetitions per program: the race was flaky, one run proves nothing. */
    private static final int REPS = 10;

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static Path writeTempGo(String name, String src) throws Exception {
        Path dir = Files.createTempDirectory("graalgo-range-conc-test");
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
                    name + " rep " + i + ": expected stdout '" + expect
                            + "', got <" + r.stdout().trim() + ">");
        }
    }

    /**
     * M10 F-1: concurrent range over slices must not lose partial sums.
     * Before the fix this printed 32/34 instead of 36 non-deterministically.
     */
    public void testConcurrentRangeSlice() throws Exception {
        checkRepeated("rangeslice.go", PROG_SLICE, "36");
    }

    /** Concurrent range over arrays. */
    public void testConcurrentRangeArray() throws Exception {
        checkRepeated("rangearray.go", PROG_ARRAY, "192");
    }

    /** Concurrent range over strings. */
    public void testConcurrentRangeString() throws Exception {
        checkRepeated("rangestring.go", PROG_STRING, "20");
    }

    /** Concurrent range over maps. */
    public void testConcurrentRangeMap() throws Exception {
        checkRepeated("rangemap.go", PROG_MAP, "36");
    }

    /** Concurrent integer range. */
    public void testConcurrentRangeInt() throws Exception {
        checkRepeated("rangeint.go", PROG_INT, "10");
    }
}
