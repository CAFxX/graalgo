package graalgo.frontend;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.Source;
import graalgo.launcher.Main;
import graalgo.launcher.ToolPaths;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import java.util.stream.Stream;

/**
 * M4 acceptance (WS E): the control-flow corpus, differential vs stock gc.
 *
 * <p>Red-green-first (SPEC §5.2): for each corpus program the expected
 * values come from stock gc at test time — the test cannot pass by
 * construction. The oracle path: inject a {@code println(vars...)} before
 * {@code func main}'s closing brace and before every bare {@code return},
 * run {@code go run} with {@code GOTOOLCHAIN=local GOMAXPROCS=1}, and parse
 * the first stderr line's space-separated values. The graalgo path:
 * {@code gofrontend} → {@link NodeFactory} → execute on a manually created
 * frame → frame-slot reads.
 *
 * <p>Corpus: {@code corpus/controlflow/cf*.go}, each ending with a
 * {@code // observe:} trailer listing the scalar variables to compare.
 * Range-over-map programs reduce to order-independent sums/counts, so the
 * comparison never depends on gc's randomized iteration order.
 */
// verifies: GG-0014 (M4: statement nodes — if/for/range/switch/type-switch/
// break/continue/goto/labels — execute with gc-identical observable behavior).
public final class ControlFlowCorpusTest {

    private static Path phase1Dir() {
        return Path.of(System.getProperty("graalgo.phase1.dir", "."));
    }

    private static Path corpusDir() {
        return phase1Dir().resolve("corpus").resolve("controlflow");
    }

    /** Guards against accidental corpus shrinkage. */
    public void testCorpusHasEnoughPrograms() throws Exception {
        long n;
        try (Stream<Path> s = Files.list(corpusDir())) {
            n = s.map(p -> p.getFileName().toString())
                    .filter(x -> x.startsWith("cf") && x.endsWith(".go"))
                    .count();
        }
        if (n < 11) {
            throw new AssertionError("control-flow corpus shrank to " + n + " programs");
        }
    }

    public void testCf01IfElse() throws Exception { checkProgram("cf01_ifelse"); }
    public void testCf02For() throws Exception { checkProgram("cf02_for"); }
    public void testCf03While() throws Exception { checkProgram("cf03_while"); }
    public void testCf04Infinite() throws Exception { checkProgram("cf04_infinite"); }
    public void testCf05Labeled() throws Exception { checkProgram("cf05_labeled"); }
    public void testCf06RangeSlice() throws Exception { checkProgram("cf06_range_slice"); }
    public void testCf07RangeArrayString() throws Exception { checkProgram("cf07_range_array_string"); }
    public void testCf08RangeMap() throws Exception { checkProgram("cf08_range_map"); }
    public void testCf09Switch() throws Exception { checkProgram("cf09_switch"); }
    public void testCf10TypeSwitch() throws Exception { checkProgram("cf10_typeswitch"); }
    public void testCf11Goto() throws Exception { checkProgram("cf11_goto"); }
    public void testCf12LoopcapBody() throws Exception { checkProgram("cf12_loopcap_body"); }
    public void testCf13LoopcapHeader() throws Exception { checkProgram("cf13_loopcap_header"); }
    public void testCf14LoopcapNested() throws Exception { checkProgram("cf14_loopcap_nested"); }
    public void testCf15LoopcapRange() throws Exception { checkProgram("cf15_loopcap_range"); }
    public void testCf16LoopcapWritethrough() throws Exception { checkProgram("cf16_loopcap_writethrough"); }
    public void testCf17LoopcapGoto() throws Exception { checkProgram("cf17_loopcap_goto"); }
    public void testCf18LoopcapDefer() throws Exception { checkProgram("cf18_loopcap_defer"); }
    public void testCf19LoopcapBreak() throws Exception { checkProgram("cf19_loopcap_break"); }
    public void testCf20AttackNestedInit() throws Exception { checkProgram("cf20_attack_nested_init"); }
    public void testCf21AttackModify() throws Exception { checkProgram("cf21_attack_modify"); }
    public void testCf22AttackShadow() throws Exception { checkProgram("cf22_attack_shadow"); }
    public void testCf23AttackMultiheader() throws Exception { checkProgram("cf23_attack_multiheader"); }
    public void testCf24P0d1Stalecell() throws Exception { checkProgram("cf24_p0d1_stalecell"); }
    public void testCf25P0d2Structfield() throws Exception { checkProgram("cf25_p0d2_structfield"); }
    public void testCf26P0d3Postclosure() throws Exception { checkProgram("cf26_p0d3_postclosure"); }
    public void testCf27P0d4Rangeassign() throws Exception { checkProgram("cf27_p0d4_rangeassign"); }
    public void testCf28P0d5Nestedcapture() throws Exception { checkProgram("cf28_p0d5_nestedcapture"); }
    public void testCf29Adv02Perclosure() throws Exception { checkProgram("cf29_adv02_perclosure"); }
    public void testCf30Adv16Structheader() throws Exception { checkProgram("cf30_adv16_structheader"); }
    public void testCf31Adv17Fieldwritethrough() throws Exception { checkProgram("cf31_adv17_fieldwritethrough"); }
    public void testCf32Adv01Outerbodyvar() throws Exception { checkProgram("cf32_adv01_outerbodyvar"); }
    public void testCf33Adv03Duringiter() throws Exception { checkProgram("cf33_adv03_duringiter"); }
    public void testCf34Adv04Rangemap() throws Exception { checkProgram("cf34_adv04_rangemap"); }
    public void testCf35Adv05While() throws Exception { checkProgram("cf35_adv05_while"); }
    public void testCf36Adv10Callboundary() throws Exception { checkProgram("cf36_adv10_callboundary"); }
    public void testCf37Adv13Shadow() throws Exception { checkProgram("cf37_adv13_shadow"); }
    public void testCf38Adv14Continue() throws Exception { checkProgram("cf38_adv14_continue"); }
    public void testCf39Adv15Rangestring() throws Exception { checkProgram("cf39_adv15_rangestring"); }
    public void testCf40P1d7StructPtrAlias() throws Exception { checkProgram("cf40_p1d7_struct_ptr_alias"); }

    static void checkProgram(String base) throws Exception {
        Path src = corpusDir().resolve(base + ".go");
        List<String> vars = observeVars(src);
        long[] expected = gcOracle(src, vars);
        long[] actual = graalgoValues(src, vars);
        if (expected.length != vars.size() || actual.length != vars.size()) {
            throw new AssertionError(base + ": value count mismatch (vars=" + vars.size()
                    + " gc=" + expected.length + " graalgo=" + actual.length + ")");
        }
        for (int i = 0; i < vars.size(); i++) {
            if (expected[i] != actual[i]) {
                throw new AssertionError(base + "." + vars.get(i) + ": gc="
                        + Long.toUnsignedString(expected[i])
                        + " graalgo=" + Long.toUnsignedString(actual[i]));
            }
        }
    }

    static List<String> observeVars(Path src) throws Exception {
        List<String> lines = Files.readAllLines(src, StandardCharsets.UTF_8);
        for (int i = lines.size() - 1; i >= 0; i--) {
            String t = lines.get(i).trim();
            if (t.startsWith("// observe:")) {
                List<String> vars = new ArrayList<>();
                for (String w : t.substring("// observe:".length()).trim().split("\\s+")) {
                    if (!w.isEmpty()) {
                        vars.add(w);
                    }
                }
                if (vars.isEmpty()) {
                    throw new AssertionError(src + ": empty // observe: trailer");
                }
                return vars;
            }
        }
        throw new AssertionError(src + ": missing // observe: trailer");
    }

    static long[] gcOracle(Path src, List<String> vars) throws Exception {
        List<String> lines = Files.readAllLines(src, StandardCharsets.UTF_8);
        String probe = "\tprintln(" + String.join(", ", vars) + ")";
        List<String> out = new ArrayList<>();
        for (String line : lines) {
            if (line.trim().equals("return")) {
                out.add(probe);
            }
            out.add(line);
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
        Path dir = Files.createTempDirectory("graalgo-oracle");
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
        long[] result = new long[parts.length];
        for (int i = 0; i < parts.length; i++) {
            try {
                result[i] = parts[i].startsWith("-")
                        ? Long.parseLong(parts[i])
                        : Long.parseUnsignedLong(parts[i]);
            } catch (NumberFormatException e) {
                throw new AssertionError(src + ": oracle value <" + parts[i]
                        + "> is not an integer", e);
            }
        }
        return result;
    }

    static long[] graalgoValues(Path src, List<String> vars) throws Exception {
        String json = Main.runGofrontend(src);
        String text = Files.readString(src, StandardCharsets.UTF_8);
        Source source = Source.newBuilder("go", text, src.getFileName().toString()).build();
        NodeFactory.Program program = new NodeFactory(source).buildProgram(json);
        VirtualFrame frame = Truffle.getRuntime()
                .createVirtualFrame(new Object[0], program.descriptor());
        program.root().execute(frame);
        Map<String, Integer> slots = program.slotsByName();
        long[] out = new long[vars.size()];
        for (int i = 0; i < vars.size(); i++) {
            Integer slot = slots.get(vars.get(i));
            if (slot == null) {
                throw new AssertionError(src + ": no frame slot for observed var <"
                        + vars.get(i) + "> (slots: " + slots.keySet() + ")");
            }
            out[i] = frame.getLong(slot);
        }
        return out;
    }
}
