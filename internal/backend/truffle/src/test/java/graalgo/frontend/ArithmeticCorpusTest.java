package graalgo.frontend;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.Source;
import graalgo.launcher.Main;
import graalgo.launcher.ToolPaths;
import graalgo.values.GoKind;
import graalgo.values.GoString;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HexFormat;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import java.util.stream.Stream;

/**
 * M3 acceptance: the arithmetic corpus, differential vs stock gc.
 *
 * <p>Red-green-first (SPEC §5.2): for each corpus program the expected values
 * come from stock gc at test time — the test cannot pass by construction.
 * The oracle path: inject a single type-aware {@code println(vars...)} before
 * {@code func main}'s closing brace (corpus files are gofmt-clean, so the
 * last {@code }}-alone line is that brace). Each variable is dumped in a
 * bit-exact encoding for its Go kind (see {@code corpus/arithmetic/README.md}:
 * ints as decimal, floats as {@code math.Float32/64bits}, strings as hex;
 * the {@code math}/{@code hex} imports are injected into the oracle copy
 * only — the corpus files stay import-free, inside frontend scope).
 * The graalgo path: {@code gofrontend} → {@link NodeFactory} → execute on a
 * manually created frame → per-kind frame-slot reads.
 *
 * <p>Run from the phase1 directory ({@code make test}); override with
 * {@code -Dgraalgo.phase1.dir=...}.
 */
// verifies: GG-0015 (M3: expressions + values differential-green on the
// arithmetic corpus; every observed value asserted bit-identical to stock
// gc across all int/uint/float widths, conversions, and comparisons).
public final class ArithmeticCorpusTest {

    private static Path repoDir() {
        return Path.of(System.getProperty("graalgo.repo.dir", "."));
    }

    private static Path corpusDir() {
        return repoDir().resolve("test").resolve("corpus").resolve("phase1").resolve("arithmetic");
    }

    private static List<String> programs() throws Exception {
        try (Stream<Path> s = Files.list(corpusDir())) {
            return s.map(p -> p.getFileName().toString())
                    .filter(n -> n.startsWith("ar") && n.endsWith(".go"))
                    .sorted()
                    .map(n -> n.substring(0, n.length() - 3))
                    .toList();
        }
    }

    /** Guards against accidental corpus shrinkage: the width/conv cover below must stay. */
    public void testCorpusHasEnoughPrograms() throws Exception {
        List<String> ps = programs();
        if (ps.size() < 15) {
            throw new AssertionError(
                    "arithmetic corpus shrank to " + ps.size() + " programs: " + ps);
        }
    }

    public void testAr01Int8() throws Exception { checkProgram("ar01_int8"); }
    public void testAr02Int16() throws Exception { checkProgram("ar02_int16"); }
    public void testAr03Int32() throws Exception { checkProgram("ar03_int32"); }
    public void testAr04Int64() throws Exception { checkProgram("ar04_int64"); }
    public void testAr05Uint8() throws Exception { checkProgram("ar05_uint8"); }
    public void testAr06Uint16() throws Exception { checkProgram("ar06_uint16"); }
    public void testAr07Uint32() throws Exception { checkProgram("ar07_uint32"); }
    public void testAr08Uint64() throws Exception { checkProgram("ar08_uint64"); }
    public void testAr09IntUint() throws Exception { checkProgram("ar09_int_uint"); }
    public void testAr10Float32() throws Exception { checkProgram("ar10_float32"); }
    public void testAr11Float64() throws Exception { checkProgram("ar11_float64"); }
    public void testAr12ConvInt() throws Exception { checkProgram("ar12_conv_int"); }
    public void testAr13ConvFloat() throws Exception { checkProgram("ar13_conv_float"); }
    public void testAr14ConvString() throws Exception { checkProgram("ar14_conv_string"); }
    public void testAr15Compare() throws Exception { checkProgram("ar15_compare"); }
    public void testAr16BoolLogic() throws Exception { checkProgram("ar16_bool_logic"); }
    public void testAr17ShiftsWidths() throws Exception { checkProgram("ar17_shifts_widths"); }

    /**
     * Runs one corpus program through both backends and asserts every
     * observed variable is bit-identical (as kind-normalized strings).
     */
    static void checkProgram(String base) throws Exception {
        Path src = corpusDir().resolve(base + ".go");
        List<String> vars = StraightLineCorpusTest.observeVars(src);
        NodeFactory.Program program = buildProgram(src);
        List<GoKind> kinds = kindsFor(program, src, vars);
        List<String> expected = gcOracle(src, vars, kinds);
        List<String> actual = graalgoValues(program, vars, kinds);
        for (int i = 0; i < vars.size(); i++) {
            if (!expected.get(i).equals(actual.get(i))) {
                throw new AssertionError(base + "." + vars.get(i) + " (" + kinds.get(i) + "): gc=<"
                        + expected.get(i) + "> graalgo=<" + actual.get(i) + ">");
            }
        }
    }

    static NodeFactory.Program buildProgram(Path src) throws Exception {
        String json = Main.runGofrontend(src);
        String text = Files.readString(src, StandardCharsets.UTF_8);
        Source source = Source.newBuilder("go", text, src.getFileName().toString()).build();
        return new NodeFactory(source).buildProgram(json);
    }

    static List<GoKind> kindsFor(NodeFactory.Program program, Path src, List<String> vars) {
        Map<String, GoKind> kinds = program.kindsByName();
        List<GoKind> out = new ArrayList<>(vars.size());
        for (String v : vars) {
            GoKind k = kinds.get(v);
            if (k == null) {
                throw new AssertionError(src + ": no kind for observed var <" + v + ">");
            }
            out.add(k);
        }
        return out;
    }

    /** Stock-gc oracle: type-aware println injection, {@code go run}, parse stderr. */
    static List<String> gcOracle(Path src, List<String> vars, List<GoKind> kinds) throws Exception {
        List<String> lines = Files.readAllLines(src, StandardCharsets.UTF_8);
        boolean needMath = false;
        boolean needHex = false;
        List<String> dump = new ArrayList<>(vars.size());
        for (int i = 0; i < vars.size(); i++) {
            String v = vars.get(i);
            switch (kinds.get(i)) {
                case FLOAT32 -> {
                    needMath = true;
                    dump.add("math.Float32bits(" + v + ")");
                }
                case FLOAT64 -> {
                    needMath = true;
                    dump.add("math.Float64bits(" + v + ")");
                }
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
                if (needMath) {
                    out.add("import \"math\"");
                }
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
        Path dir = Files.createTempDirectory("graalgo-arith-oracle");
        Path injected = dir.resolve("oracle.go");
        Files.write(injected, out, StandardCharsets.UTF_8);

        ProcessBuilder pb = new ProcessBuilder(
                ToolPaths.stockGo().toString(), "run", injected.toString());
        pb.environment().put("GOTOOLCHAIN", "local");
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

    /** graalgo path: execute → per-kind frame-slot reads, kind-normalized strings. */
    static List<String> graalgoValues(NodeFactory.Program program, List<String> vars, List<GoKind> kinds) {
        VirtualFrame frame = Truffle.getRuntime()
                .createVirtualFrame(new Object[0], program.descriptor());
        program.root().execute(frame);
        Map<String, Integer> slots = program.slotsByName();
        List<String> out = new ArrayList<>(vars.size());
        for (int i = 0; i < vars.size(); i++) {
            Integer slot = slots.get(vars.get(i));
            if (slot == null) {
                throw new AssertionError("no frame slot for observed var <" + vars.get(i) + ">");
            }
            out.add(switch (kinds.get(i)) {
                case INT8, INT16, INT32 -> Integer.toString(frame.getInt(slot));
                case UINT8, UINT16, UINT32 -> Integer.toUnsignedString(frame.getInt(slot));
                case INT64, UNTYPED_INT -> Long.toString(frame.getLong(slot));
                case UINT64 -> Long.toUnsignedString(frame.getLong(slot));
                case FLOAT32 -> Integer.toUnsignedString(Float.floatToIntBits(frame.getFloat(slot)));
                case FLOAT64, UNTYPED_FLOAT ->
                        Long.toUnsignedString(Double.doubleToLongBits(frame.getDouble(slot)));
                case BOOL, UNTYPED_BOOL -> Boolean.toString(frame.getBoolean(slot));
                case STRING, UNTYPED_STRING ->
                        HexFormat.of().formatHex(((GoString) frame.getObject(slot)).bytes());
                default -> throw new AssertionError(
                        "cannot observe kind " + kinds.get(i) + " (only scalars/strings in M3)");
            });
        }
        return out;
    }
}
