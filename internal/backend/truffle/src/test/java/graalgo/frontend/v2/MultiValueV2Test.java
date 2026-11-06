package graalgo.frontend.v2;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.Source;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoRootNode;
import graalgo.launcher.ToolPaths;
import graalgo.runtime.PanicException;
import graalgo.values.GoKind;
import graalgo.values.GoString;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HexFormat;
import java.util.List;
import java.util.concurrent.TimeUnit;

/**
 * Multi-value end-to-end tests for the v2 factory (V12-P1-1 + V12-P1-2).
 *
 * <p>Each program runs both ways and must agree with stock gc:
 * <ul>
 *   <li>the graalgo path: the Go program is encoded to a v2 binary document
 *       by {@code encode-demo}, built by {@link NodeFactoryV2}, and executed
 *       by wrapping {@code func main} in a {@link GoRootNode} on a manually
 *       created frame (the v1 corpus-test pattern) — results are read back
 *       from main's frame slots;</li>
 *   <li>the gc path: the same program runs under stock {@code go run} with
 *       a type-aware {@code println} injected before {@code main}'s closing
 *       brace (the {@code ArithmeticCorpusTest} oracle pattern — the
 *       injection and parsing below mirror it).</li>
 * </ul>
 * The programs use only the v2-supported subset (no imports, no structs,
 * no slices/maps — those are other lanes' gaps): plain ints/strings, user
 * functions, and multi-value calls. Every returned value is observed, so a
 * dropped second result fails the test instead of silently passing.
 */
public final class MultiValueV2Test {

    private static Path repoDir() {
        return Path.of(System.getProperty("graalgo.repo.dir", "."));
    }

    private static Path tmpDir() throws IOException {
        Path base = Path.of(System.getProperty("java.io.tmpdir", "."));
        return Files.createTempDirectory(base, "graalgo-mv-v2-");
    }

    /** Lazily built encode-demo binary (built once per JVM). */
    private static Path encodeDemo;

    /**
     * Shared Go work dir (one per JVM): the build cache, GOPATH, and
     * GOTMPDIR for every go invocation this test shells out to. Shared
     * (not per-program) so the stdlib is compiled once, not once per
     * program — a fresh cache per program would recompile the stdlib
     * ~15 times and burn hundreds of MB under java.io.tmpdir.
     */
    private static Path goWorkDir;

    private static synchronized Path goWorkDir() throws IOException {
        if (goWorkDir == null) {
            goWorkDir = tmpDir();
        }
        return goWorkDir;
    }

    /** Points a go invocation's cache/tmp/gopath at the shared work dir. */
    private static void goEnv(ProcessBuilder pb, Path workDir) {
        pb.environment().put("GOCACHE", workDir.resolve("gocache").toString());
        pb.environment().put("GOTMPDIR", workDir.toString());
        pb.environment().put("GOPATH", workDir.resolve("gopath").toString());
    }

    private static synchronized Path encodeDemo() throws Exception {
        if (encodeDemo != null) {
            return encodeDemo;
        }
        Path dir = tmpDir();
        Path bin = dir.resolve("encode-demo");
        Path v2mod = repoDir().resolve("internal").resolve("frontend").resolve("v2");
        ProcessBuilder pb = new ProcessBuilder(
                ToolPaths.stockGo().toString(), "build", "-o", bin.toString(),
                "./encode/cmd/encode-demo");
        pb.directory(v2mod.toFile());
        pb.environment().put("GOTOOLCHAIN", "local");
        goEnv(pb, goWorkDir());
        pb.environment().put("GOMAXPROCS", "1");
        pb.redirectErrorStream(true);
        Process p = pb.start();
        String out = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
        boolean done = p.waitFor(300, TimeUnit.SECONDS);
        if (!done) {
            p.destroyForcibly();
            throw new AssertionError("go build encode-demo timed out");
        }
        if (p.exitValue() != 0) {
            throw new AssertionError("go build encode-demo failed (exit " + p.exitValue()
                    + "): " + out);
        }
        encodeDemo = bin;
        return bin;
    }

    private static NodeFactoryV2.V2Program buildProgram(String name, String src) throws Exception {
        Path dir = tmpDir();
        Path srcFile = dir.resolve(name + ".go");
        Files.write(srcFile, src.getBytes(StandardCharsets.UTF_8));
        Path binFile = dir.resolve(name + ".bin");
        ProcessBuilder pb = new ProcessBuilder(
                encodeDemo().toString(), srcFile.toString(), binFile.toString());
        pb.redirectErrorStream(true);
        Process p = pb.start();
        String out = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
        boolean done = p.waitFor(120, TimeUnit.SECONDS);
        if (!done) {
            p.destroyForcibly();
            throw new AssertionError(name + ": encode-demo timed out");
        }
        if (p.exitValue() != 0) {
            throw new AssertionError(name + ": encode-demo failed (exit " + p.exitValue()
                    + "): " + out);
        }
        byte[] data = Files.readAllBytes(binFile);
        Source source = Source.newBuilder("go", src, srcFile.getFileName().toString()).build();
        return new NodeFactoryV2(source).buildProgram(data);
    }

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /**
     * Runs one program both ways and asserts every observed variable is
     * bit-identical (as kind-normalized strings). {@code vars}/{@code kinds}
     * are main's locals, observed after main returns.
     */
    private static void checkProgram(String name, String src,
            List<String> vars, List<GoKind> kinds) throws Exception {
        Path dir = tmpDir();
        Path srcFile = dir.resolve(name + ".go");
        Files.write(srcFile, src.getBytes(StandardCharsets.UTF_8));
        List<String> expected = gcOracle(srcFile, vars, kinds);
        List<String> actual = graalgoValues(name, src, vars, kinds);
        for (int i = 0; i < vars.size(); i++) {
            if (!expected.get(i).equals(actual.get(i))) {
                throw new AssertionError(name + "." + vars.get(i) + " (" + kinds.get(i) + "): gc=<"
                        + expected.get(i) + "> graalgo=<" + actual.get(i) + ">");
            }
        }
    }

    /** graalgo path: build, execute main, read frame slots. */
    private static List<String> graalgoValues(String name, String src,
            List<String> vars, List<GoKind> kinds) throws Exception {
        NodeFactoryV2.V2Program program = buildProgram(name, src);
        NodeFactoryV2.V2Function main = program.mainFunction();
        check(main != null, name + ": no func main");
        check(main.hasBody(), name + ": func main has no body");
        VirtualFrame frame = Truffle.getRuntime()
                .createVirtualFrame(new Object[0], main.frameDescriptor());
        new GoRootNode(main.frameDescriptor(), main.body()).execute(frame);
        return readSlots(frame, main, vars, kinds);
    }

    private static List<String> readSlots(VirtualFrame frame, NodeFactoryV2.V2Function main,
            List<String> vars, List<GoKind> kinds) {
        List<String> out = new ArrayList<>(vars.size());
        for (int i = 0; i < vars.size(); i++) {
            Integer slot = main.slotForName(vars.get(i));
            check(slot != null, "no frame slot for observed var <" + vars.get(i) + ">");
            out.add(switch (kinds.get(i)) {
                case INT64 -> Long.toString(frame.getLong(slot));
                case STRING ->
                        HexFormat.of().formatHex(((GoString) frame.getObject(slot)).bytes());
                case BOOL -> Boolean.toString(frame.getBoolean(slot));
                case FLOAT64 ->
                        Long.toUnsignedString(Double.doubleToLongBits(frame.getDouble(slot)));
                default -> throw new AssertionError(
                        "cannot observe kind " + kinds.get(i) + " (only int/string/bool/float64 in this test)");
            });
        }
        return out;
    }

    /**
     * Stock-gc oracle: type-aware println injection, {@code go run}, parse
     * stderr. Mirrors {@code ArithmeticCorpusTest.gcOracle} (same encoding,
     * same injection rule: before main's closing brace, which is the last
     * {@code }}-alone line because main is always the last function in
     * these programs).
     */
    private static List<String> gcOracle(Path src, List<String> vars, List<GoKind> kinds)
            throws Exception {
        // The oracle's go run needs a writable build cache: point it at a
        // temp dir (home quota may be tight; the cache is regenerable).
        Path dir = tmpDir();
        List<String> lines = Files.readAllLines(src, StandardCharsets.UTF_8);
        boolean needHex = false;
        List<String> dump = new ArrayList<>(vars.size());
        for (int i = 0; i < vars.size(); i++) {
            String v = vars.get(i);
            switch (kinds.get(i)) {
                case STRING -> {
                    needHex = true;
                    dump.add("hex.EncodeToString([]byte(" + v + "))");
                }
                default -> dump.add(v);
            }
        }
        String probe = "\tprintln(" + String.join(", ", dump) + ")";
        List<String> out = new ArrayList<>();
        boolean pkgSeen = false;
        for (String line : lines) {
            out.add(line);
            if (!pkgSeen && line.startsWith("package ")) {
                pkgSeen = true;
                if (needHex) {
                    out.add("import \"encoding/hex\"");
                }
            }
        }
        int insertAt = -1;
        for (int i = 0; i < out.size(); i++) {
            if (out.get(i).equals("}")) {
                insertAt = i;
            }
        }
        if (insertAt < 0) {
            throw new AssertionError(src + ": no closing brace for println injection");
        }
        out.add(insertAt, probe);
        Path injected = dir.resolve("oracle.go");
        Files.write(injected, out, StandardCharsets.UTF_8);

        ProcessBuilder pb = new ProcessBuilder(
                ToolPaths.stockGo().toString(), "run", injected.toString());
        pb.environment().put("GOTOOLCHAIN", "local");
        goEnv(pb, goWorkDir());
        pb.environment().put("GOMAXPROCS", "1");
        pb.redirectErrorStream(false);
        Process p = pb.start();
        String stdout = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
        String stderr = new String(p.getErrorStream().readAllBytes(), StandardCharsets.UTF_8);
        boolean done = p.waitFor(120, TimeUnit.SECONDS);
        if (!done) {
            p.destroyForcibly();
            throw new AssertionError(src + ": stock go run timed out");
        }
        if (p.exitValue() != 0) {
            throw new AssertionError(src + ": stock go run failed (exit " + p.exitValue()
                    + "): stdout=<" + stdout + "> stderr=<" + stderr + ">");
        }
        String firstLine = null;
        for (String l : stderr.split("\n")) {
            if (!l.trim().isEmpty()) {
                firstLine = l;
                break;
            }
        }
        if (firstLine == null) {
            throw new AssertionError(src + ": oracle printed nothing: stderr=<" + stderr + ">");
        }
        String[] parts = firstLine.trim().split("\\s+");
        if (parts.length != vars.size()) {
            throw new AssertionError(src + ": oracle output shape mismatch: <" + firstLine.trim()
                    + "> for vars " + vars);
        }
        return List.of(parts);
    }

    /** Runs a program under stock go and asserts it panics with the given text. */
    private static void assertGcPanics(String name, String src, String want) throws Exception {
        Path dir = tmpDir();
        Path srcFile = dir.resolve(name + ".go");
        Files.write(srcFile, src.getBytes(StandardCharsets.UTF_8));
        ProcessBuilder pb = new ProcessBuilder(
                ToolPaths.stockGo().toString(), "run", srcFile.toString());
        pb.environment().put("GOTOOLCHAIN", "local");
        goEnv(pb, goWorkDir());
        pb.environment().put("GOMAXPROCS", "1");
        pb.redirectErrorStream(true);
        Process p = pb.start();
        String out = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
        boolean done = p.waitFor(120, TimeUnit.SECONDS);
        if (!done) {
            p.destroyForcibly();
            throw new AssertionError(name + ": stock go run timed out");
        }
        check(p.exitValue() != 0, name + ": stock go run did not panic: <" + out + ">");
        check(out.contains(want), name + ": stock panic <" + out + "> does not contain <" + want + ">");
    }

    /** Executes main and returns the frame (for panic-control tests). */
    private static VirtualFrame executeMainExpectingPanic(
            NodeFactoryV2.V2Program program, String name) throws Exception {
        NodeFactoryV2.V2Function main = program.mainFunction();
        check(main != null, name + ": no func main");
        VirtualFrame frame = Truffle.getRuntime()
                .createVirtualFrame(new Object[0], main.frameDescriptor());
        try {
            new GoRootNode(main.frameDescriptor(), main.body()).execute(frame);
        } catch (PanicException e) {
            return frame;
        }
        throw new AssertionError(name + ": expected a Go panic, main returned normally");
    }

    // --- Programs (main is always last: the oracle injects before its brace). ---

    public void testMultiValueAssign() throws Exception {
        checkProgram("mv_assign",
                "package main\n"
                + "func f() (int, string) { return 42, \"hi\" }\n"
                + "func main() {\n"
                + "\tvar a int\n"
                + "\tvar b string\n"
                + "\ta, b = f()\n"
                + "}\n",
                List.of("a", "b"), List.of(GoKind.INT64, GoKind.STRING));
    }

    public void testMultiValueDefine() throws Exception {
        checkProgram("mv_define",
                "package main\n"
                + "func f() (int, int) { return 3, 4 }\n"
                + "func main() {\n"
                + "\ta, b := f()\n"
                + "}\n",
                List.of("a", "b"), List.of(GoKind.INT64, GoKind.INT64));
    }

    public void testMultiValueVarDecl() throws Exception {
        checkProgram("mv_vardecl",
                "package main\n"
                + "func f() (int, int) { return 5, 6 }\n"
                + "func main() {\n"
                + "\tvar a, b = f()\n"
                + "}\n",
                List.of("a", "b"), List.of(GoKind.INT64, GoKind.INT64));
    }

    public void testMultiValueReturnPassthrough() throws Exception {
        checkProgram("mv_passthrough",
                "package main\n"
                + "func f(x int) (int, int) { return x + 1, x - 1 }\n"
                + "func h(x int) (int, int) { return f(x) }\n"
                + "func main() {\n"
                + "\ta, b := h(10)\n"
                + "}\n",
                List.of("a", "b"), List.of(GoKind.INT64, GoKind.INT64));
    }

    public void testSecondResultEvaluated() throws Exception {
        // The old factory returned only the first value (b would be 1);
        // the side-effecting second result proves both are evaluated.
        checkProgram("mv_second",
                "package main\n"
                + "func g() int { return 7 }\n"
                + "func f() (int, int) { return 1, g() }\n"
                + "func main() {\n"
                + "\ta, b := f()\n"
                + "}\n",
                List.of("a", "b"), List.of(GoKind.INT64, GoKind.INT64));
    }

    public void testBlankDiscards() throws Exception {
        checkProgram("mv_blank",
                "package main\n"
                + "func f() (int, int) { return 8, 9 }\n"
                + "func main() {\n"
                + "\t_, b := f()\n"
                + "}\n",
                List.of("b"), List.of(GoKind.INT64));
    }

    public void testSwapRegression() throws Exception {
        // Multi-assign pairing (a, b = b, a) must keep working.
        checkProgram("mv_swap",
                "package main\n"
                + "func main() {\n"
                + "\ta := 1\n"
                + "\tb := 2\n"
                + "\ta, b = b, a\n"
                + "}\n",
                List.of("a", "b"), List.of(GoKind.INT64, GoKind.INT64));
    }

    public void testSingleValueCallUnchanged() throws Exception {
        checkProgram("mv_single",
                "package main\n"
                + "func f(x int) int { return x * 2 }\n"
                + "func main() {\n"
                + "\ta := f(21)\n"
                + "}\n",
                List.of("a"), List.of(GoKind.INT64));
    }

    public void testBareReturnZeroTuple() throws Exception {
        // A bare return from a multi-result function yields zero values.
        checkProgram("mv_bare",
                "package main\n"
                + "func f() (int, string) { return }\n"
                + "func main() {\n"
                + "\ta, b := f()\n"
                + "}\n",
                List.of("a", "b"), List.of(GoKind.INT64, GoKind.STRING));
    }

    public void testFallOffZeroTuple() throws Exception {
        // Falling off the end of a multi-result function yields zero values.
        checkProgram("mv_falloff",
                "package main\n"
                + "func f() (int, int) { }\n"
                + "func main() {\n"
                + "\ta, b := f()\n"
                + "}\n",
                List.of("a", "b"), List.of(GoKind.INT64, GoKind.INT64));
    }

    public void testVoidCallExecutes() throws Exception {
        // A void call must execute (not just decode): the panic proves it
        // ran, and x staying 0 proves control never reached past it.
        String src = "package main\n"
                + "func boom() { panic(\"boom\") }\n"
                + "func main() {\n"
                + "\tvar x int\n"
                + "\tx = 0\n"
                + "\tboom()\n"
                + "\tx = 1\n"
                + "}\n";
        assertGcPanics("mv_voidpanic", src, "panic: boom");
        NodeFactoryV2.V2Program program = buildProgram("mv_voidpanic", src);
        NodeFactoryV2.V2Function main = program.mainFunction();
        check(main != null, "mv_voidpanic: no func main");
        VirtualFrame frame = executeMainExpectingPanic(program, "mv_voidpanic");
        Integer slot = main.slotForName("x");
        check(slot != null, "mv_voidpanic: no slot for x");
        check(frame.getLong(slot) == 0L,
                "mv_voidpanic: x = " + frame.getLong(slot) + ", want 0 (boom() must have run)");
    }

    public void testSecondResultSideEffectRuns() throws Exception {
        // The second result's side effect (a panic) must execute: the old
        // factory never evaluated results[1:], so it would not panic.
        String src = "package main\n"
                + "func g() int { panic(\"second\") }\n"
                + "func f() (int, int) { return 1, g() }\n"
                + "func main() {\n"
                + "\tvar a int\n"
                + "\tvar b int\n"
                + "\ta = 0\n"
                + "\tb = 0\n"
                + "\ta, b = f()\n"
                + "}\n";
        assertGcPanics("mv_secondpanic", src, "panic: second");
        NodeFactoryV2.V2Program program = buildProgram("mv_secondpanic", src);
        NodeFactoryV2.V2Function main = program.mainFunction();
        check(main != null, "mv_secondpanic: no func main");
        VirtualFrame frame = executeMainExpectingPanic(program, "mv_secondpanic");
        Integer aSlot = main.slotForName("a");
        Integer bSlot = main.slotForName("b");
        check(aSlot != null && bSlot != null, "mv_secondpanic: missing slots");
        check(frame.getLong(aSlot) == 0L && frame.getLong(bSlot) == 0L,
                "mv_secondpanic: a/b changed before the panic (a=" + frame.getLong(aSlot)
                + " b=" + frame.getLong(bSlot) + ")");
    }

    public void testVoidCallStatement() throws Exception {
        // A void call as a statement decodes, executes, and lets control
        // continue (V12-P1-1's empty-tuple decode path, end to end).
        checkProgram("mv_voidstmt",
                "package main\n"
                + "func f() {}\n"
                + "func main() {\n"
                + "\tvar x int\n"
                + "\tx = 3\n"
                + "\tf()\n"
                + "\tx = 4\n"
                + "}\n",
                List.of("x"), List.of(GoKind.INT64));
    }

    public void testSpreadFailsLoudly() throws Exception {
        // g(f()) with f multi-value is a spread: the factory must fail
        // loudly at build time, never silently pass a tuple as one arg.
        String src = "package main\n"
                + "func f() (int, int) { return 1, 2 }\n"
                + "func g(a int, b int) int { return a + b }\n"
                + "func main() {\n"
                + "\tx := g(f())\n"
                + "}\n";
        try {
            buildProgram("mv_spread", src);
        } catch (FrontendException e) {
            check(e.getMessage().contains("spread"),
                    "mv_spread: wrong failure message: " + e.getMessage());
            return;
        }
        throw new AssertionError("mv_spread: spread call built successfully (must fail loudly)");
    }

    public void testArityMismatchFailsLoudly() throws Exception {
        // A 3-value call into 2 variables must fail loudly at build time.
        String src = "package main\n"
                + "func f() (int, int, int) { return 1, 2, 3 }\n"
                + "func main() {\n"
                + "\ta, b := f()\n"
                + "}\n";
        try {
            buildProgram("mv_arity", src);
        } catch (FrontendException e) {
            check(e.getMessage().contains("multi-value call returns 3 values for 2 variables"),
                    "mv_arity: wrong failure message: " + e.getMessage());
            return;
        }
        throw new AssertionError("mv_arity: arity mismatch built successfully (must fail loudly)");
    }
}
