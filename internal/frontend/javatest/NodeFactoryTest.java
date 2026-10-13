import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.source.Source;
import com.oracle.truffle.api.source.SourceSection;
import graalgo.frontend.BlockNode;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoRootNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/**
 * NodeFactory tests (red-green-first for WS B, Java side).
 *
 * Run: compile with javac -Xlint:deprecation -Werror -cp &lt;jars&gt; and run
 * with the javatest dir as argv[0]:
 *   java --enable-native-access=ALL-UNNAMED -cp classes:jars NodeFactoryTest javatest/
 *
 * Golden JSON files are produced by gofrontend from the sibling .go files
 * (frontend/testdata); the .go sources are copied here so each SourceSection
 * points at real Go source text.
 */
public final class NodeFactoryTest {
    static int failures = 0;

    static void check(boolean cond, String name) {
        System.out.println((cond ? "ok: " : "FAIL: ") + name);
        if (!cond) {
            failures++;
        }
    }

    static void expectFrontendException(Runnable r, String name) {
        try {
            r.run();
            check(false, name + " (no exception)");
        } catch (FrontendException e) {
            check(true, name + " -> " + e.getMessage());
        } catch (Throwable t) {
            check(false, name + " (wrong exception: " + t + ")");
        }
    }

    static NodeFactory.Program build(Path dir, String base) throws Exception {
        String go = Files.readString(dir.resolve(base + ".go"));
        String json = Files.readString(dir.resolve(base + ".json"));
        Source src = Source.newBuilder("go", go, base + ".go").build();
        return new NodeFactory(src).buildProgram(json);
    }

    static List<String> childKinds(Node n) {
        List<String> kinds = new ArrayList<>();
        for (Node c : n.getChildren()) {
            kinds.add(c.getClass().getSimpleName());
        }
        return kinds;
    }

    public static void main(String[] args) throws Exception {
        Path dir = Path.of(args[0]);

        // --- createSection semantics probe: (line, col, len) understanding.
        Source probe = Source.newBuilder("go", "package main\n", "probe.go").build();
        SourceSection ps = probe.createSection(1, 1, 5);
        check(ps.getCharIndex() == 0 && ps.getCharLength() == 5,
                "createSection(1,1,5) is (line=1,col=1,len=5)");

        // --- straight01: build, shape, execute, positions.
        NodeFactory.Program p1 = build(dir, "golden_straight01");
        VirtualFrame f1 = Truffle.getRuntime().createVirtualFrame(new Object[0], p1.descriptor());
        p1.root().execute(f1);
        check(p1.slotsByName().containsKey("z"), "straight01: slot for z exists");
        check(f1.getLong(p1.slotsByName().get("z")) == 42L, "straight01: z == 42");

        List<String> shape = childKinds(p1.root().getBody());
        check(shape.equals(List.of("VarDeclNode", "VarDeclNode", "VarDeclNode", "AssignNode")),
                "straight01: body shape " + shape);

        // Position cross-check: first stmt's section must match the exporter's pos.
        Json.Object doc1 = (Json.Object) Json.parse(Files.readString(dir.resolve("golden_straight01.json")));
        Json.Object stmt0 = (Json.Object) ((Json.Object) ((Json.Object) doc1.array("funcs", "d").get(0))
                .object("fields", "f").object("body", "f")).object("fields", "b").array("stmts", "s").get(0);
        Json.Object pos0 = stmt0.object("pos", "s");
        Node first = p1.root().getBody().getChildren().iterator().next();
        SourceSection sec0 = first.getSourceSection();
        check(sec0 != null
                        && sec0.getCharIndex() == pos0.intValue("off", "p")
                        && sec0.getCharLength() == pos0.intValue("len", "p")
                        && sec0.getSource().getName().equals("golden_straight01.go"),
                "straight01: first stmt section matches exporter pos (off="
                        + pos0.intValue("off", "p") + ", len=" + pos0.intValue("len", "p") + ")");

        // --- straight02: full operator matrix, oracle values from stock gc.
        NodeFactory.Program p2 = build(dir, "golden_straight02");
        VirtualFrame f2 = Truffle.getRuntime().createVirtualFrame(new Object[0], p2.descriptor());
        p2.root().execute(f2);
        check(f2.getLong(p2.slotsByName().get("a")) == 20L, "straight02: a == 20 (gc oracle)");
        check(f2.getLong(p2.slotsByName().get("b")) == 44L, "straight02: b == 44 (gc oracle)");
        check(f2.getLong(p2.slotsByName().get("c")) == 84L, "straight02: c == 84 (gc oracle)");
        // sum == 194 also proves the post-return `sum = -1` never executed.
        check(f2.getLong(p2.slotsByName().get("sum")) == 194L, "straight02: sum == 194 (gc oracle; return skipped tail)");

        // --- negative cases: every one must fail with FrontendException.
        // NOTE: the source must be the real .go text — the factory validates
        // positions eagerly, so a short dummy source would fail first on
        // "line out of bounds" instead of the intended semantic error.
        String go1 = Files.readString(dir.resolve("golden_straight01.go"));
        Source src1 = Source.newBuilder("go", go1, "golden_straight01.go").build();
        String badKind = Files.readString(dir.resolve("badkind.json"));
        String badVersion = Files.readString(dir.resolve("badversion.json"));
        String badOp = Files.readString(dir.resolve("badop.json"));
        expectFrontendException(() -> new NodeFactory(src1).buildProgram(badKind), "unknown node kind rejected");
        expectFrontendException(
                () -> new NodeFactory(src1).buildProgram(badVersion), "frontend v2 rejected");
        expectFrontendException(() -> new NodeFactory(src1).buildProgram(badOp), "unknown binary op rejected");
        expectFrontendException(
                () -> new NodeFactory(src1).buildProgram("{oops"), "malformed JSON rejected");
        expectFrontendException(
                () -> new NodeFactory(src1).registerExprKind("binary", (n, c) -> null),
                "duplicate kind registration rejected");

        // --- Json unit checks (non-tautological: wrong types must throw).
        Json.Object o = (Json.Object) Json.parse("{\"a\":[1,true,null,\"x\"],\"b\":-2.5,\"c\":{}}");
        check(o.array("a", "t").size() == 4, "json: array size");
        check(((Json.Num) o.array("a", "t").get(0)).asLong("t") == 1L, "json: int element");
        check(o.array("a", "t").get(2) instanceof Json.Null, "json: null element");
        expectFrontendException(() -> o.longInt("b", "t"), "json: non-integral asLong throws");
        expectFrontendException(() -> o.string("b", "t"), "json: wrong accessor type throws");

        if (failures > 0) {
            System.out.println(failures + " FAILURES");
            System.exit(1);
        }
        System.out.println("ALL PASS");
    }
}
