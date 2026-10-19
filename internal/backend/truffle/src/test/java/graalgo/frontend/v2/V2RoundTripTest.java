package graalgo.frontend.v2;

import com.oracle.truffle.api.source.Source;
import java.io.IOException;
import java.io.InputStream;
import java.util.ArrayList;
import java.util.List;

/**
 * Cross-language round-trip tests for the v2 Go→Java interface.
 *
 * <p>Each golden {@code .bin} in test resources
 * ({@code graalgo/frontend/v2/roundtrip/*.bin}) is produced by the real Go
 * v2 pipeline (parser → checker → emitter → encode) via
 * {@code internal/frontend/v2/encode/roundtrip_golden_test.go}
 * ({@code go test ./encode/ -run TestRoundTripGoldens -update} regenerates
 * them; the Go side pins the bytes and the wire contract). This test feeds
 * every golden through {@link NodeFactoryV2#buildProgram} and asserts the
 * built program's shape — the end-to-end coverage the v2 track was missing
 * (review V-2: no Java test ever fed a real pipeline-produced Document
 * through {@code buildProgram}).
 *
 * <p>Corpus (in dependency order):
 * <ul>
 *   <li>{@code hello} — minimal wiring: vardecl, basiclit, builtin call.
 *   <li>{@code alias} — {@code type MyInt = int}: the alias must resolve to
 *       its right-hand-side type on the wire (the missing
 *       {@code *types.Alias} emitter case made the backend throw).
 *   <li>{@code struct} — named struct with selector reads and writes
 *       ({@code p.X}): covers the named-struct selector path in
 *       {@code buildProgram}.
 * </ul>
 */
public final class V2RoundTripTest {

    private static final String RESOURCE_BASE = "/graalgo/frontend/v2/roundtrip/";

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /** Reads a checked-in golden .bin from the test classpath. */
    private static byte[] goldenBytes(String name) throws IOException {
        String path = RESOURCE_BASE + name + ".bin";
        try (InputStream in = V2RoundTripTest.class.getResourceAsStream(path)) {
            check(in != null, "golden missing from test resources: " + path);
            return in.readAllBytes();
        }
    }

    /** Runs one golden through the real buildProgram entry point. */
    private static NodeFactoryV2.V2Program buildProgram(String golden) throws IOException {
        Source source = Source.newBuilder("go", "", "v2roundtrip-" + golden).build();
        return new NodeFactoryV2(source).buildProgram(goldenBytes(golden));
    }

    private static List<String> functionNames(NodeFactoryV2.V2Program prog) {
        List<String> names = new ArrayList<>();
        for (NodeFactoryV2.V2Function fn : prog.functions()) {
            names.add(fn.name());
        }
        return names;
    }

    private static NodeFactoryV2.V2Function onlyFunction(NodeFactoryV2.V2Program prog,
            String golden) {
        check(prog.functionCount() == 1,
            golden + ": expected 1 function, got " + prog.functionCount());
        NodeFactoryV2.V2Function fn = prog.functions().get(0);
        check("main".equals(fn.name()),
            golden + ": expected function main, got " + fn.name());
        check(fn.hasBody(), golden + ": main has no body");
        check(fn.body() != null, golden + ": main body node is null");
        check(fn.frameDescriptor() != null, golden + ": main has no frame descriptor");
        return fn;
    }

    public void testHelloBuilds() throws IOException {
        NodeFactoryV2.V2Program prog = buildProgram("hello");
        onlyFunction(prog, "hello");
        check(functionNames(prog).equals(List.of("main")),
            "hello: function names = " + functionNames(prog));
    }

    public void testAliasBuilds() throws IOException {
        // type MyInt = int: exercises the *types.Alias emitter case. Before
        // the fix the wire carried {"kind":"basic","class":"unknown"} and
        // buildProgram threw; now the alias resolves to basic/int.
        NodeFactoryV2.V2Program prog = buildProgram("alias");
        onlyFunction(prog, "alias");
    }

    public void testStructBuilds() throws IOException {
        // Named-struct selector path: `var p Point` (named type), selector
        // writes `p.X = 3` and reads `p.X + p.Y`. This is the path the
        // findFieldIndex named-type bug broke (review finding 1 / K-3):
        // until that fix integrates, buildProgram throws
        // `selector on non-struct type named` here — loudly, not silently.
        NodeFactoryV2.V2Program prog = buildProgram("struct");
        onlyFunction(prog, "struct");
    }
}
