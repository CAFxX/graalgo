package graalgo.iface;

import java.util.Map;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;

import graalgo.frontend.FrontendException;
import graalgo.values.GoFunc;

/**
 * Unit tests for {@link IfaceMethodTables#resolve} (P3-M3, WS D,
 * ADR-0003): doc type-table → descriptor name → (method → GoFunc).
 * verifies: GG-0014
 */
public final class IfaceMethodTablesTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(Object expected, Object actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    private static GoFunc goFunc() {
        RootNode root = new RootNode(null) {
            @Override
            public Object execute(VirtualFrame frame) {
                return null;
            }
        };
        RootCallTarget target = root.getCallTarget();
        return new GoFunc(target, null, null);
    }

    private static final String DOC = """
            {"frontend": "v1",
             "types": [
               {"kind": "basic", "detail": {"name": "int", "class": "int"}},
               {"kind": "named", "detail": {"name": "main.T", "underlying": 0,
                 "methods": [{"name": "M", "obj": 7, "recv": "value"}]}},
               {"kind": "pointer", "detail": {"elem": 1,
                 "methods": [{"name": "M", "obj": 8, "recv": "pointer"},
                             {"name": "N", "obj": 9, "recv": "pointer"}]}},
               {"kind": "named", "detail": {"name": "main.U", "underlying": 0,
                 "methods": []}}
             ],
             "objects": [], "funcs": []}""";

    /**
     * Named entries key by detail.name; pointer entries key by the
     * factory's structural descriptor name; objIDs resolve through
     * Program.functions().
     * verifies: GG-0014
     */
    public void testResolve() {
        GoFunc fm = goFunc();
        GoFunc fmp = goFunc();
        GoFunc fnp = goFunc();
        Map<Integer, GoFunc> functions = Map.of(7, fm, 8, fmp, 9, fnp);
        Map<String, Map<String, GoFunc>> out = IfaceMethodTables.resolve(DOC, functions);
        assertEquals(2, out.size(), "two descriptors with methods");
        assertEquals(Map.of("M", fm), out.get("main.T"), "named entry keyed by detail.name");
        String ptrName = DescriptorNames.descriptorName(
                new graalgo.frontend.TypeTable(
                        ((graalgo.frontend.Json.Object) graalgo.frontend.Json.parse(DOC))
                                .array("types", "t")),
                2);
        Map<String, GoFunc> ptrMethods = out.get(ptrName);
        assertTrue(ptrMethods != null, "pointer entry present under " + ptrName);
        assertEquals(Map.of("M", fmp, "N", fnp), ptrMethods, "pointer method set");
        assertTrue(!out.containsKey("main.U"), "empty method list installs nothing");
    }

    /**
     * A method objID missing from Program.functions() fails fast naming
     * the descriptor, method, and objID.
     * verifies: GG-0014
     */
    public void testMissingObjIdFailsFast() {
        try {
            IfaceMethodTables.resolve(DOC, Map.of(7, goFunc(), 8, goFunc()));
        } catch (FrontendException fe) {
            String msg = fe.getMessage();
            assertTrue(msg.contains("\"N\""), "names the method: " + msg);
            assertTrue(msg.contains("9"), "names the objID: " + msg);
            return;
        }
        throw new AssertionError("expected FrontendException for missing objID 9");
    }

    /**
     * A document without any method tables resolves to an empty map
     * (install is still called — no stale tables).
     * verifies: GG-0014
     */
    public void testEmptyDoc() {
        String doc = """
                {"frontend": "v1",
                 "types": [{"kind": "basic", "detail": {"name": "int", "class": "int"}}],
                 "objects": [], "funcs": []}""";
        assertEquals(Map.of(), IfaceMethodTables.resolve(doc, Map.of()), "empty table map");
    }
}
