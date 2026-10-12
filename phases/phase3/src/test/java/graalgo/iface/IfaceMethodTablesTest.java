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
 * verifies: GG-0017 (iface slice)
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
                 "methods": [{"name": "M", "obj": 8, "recv": "pointer", "index": []},
                             {"name": "N", "obj": 9, "recv": "pointer", "index": [0]}]}},
               {"kind": "named", "detail": {"name": "main.U", "underlying": 0,
                 "methods": []}}
             ],
             "objects": [], "funcs": []}""";

    /**
     * Named entries key by detail.name; pointer entries key by the
     * factory's structural descriptor name; objIDs resolve through
     * Program.functions(); the wire "index" becomes the receiver path
     * (missing "index" = direct).
     * verifies: GG-0014
     */
    public void testResolve() {
        GoFunc fm = goFunc();
        GoFunc fmp = goFunc();
        GoFunc fnp = goFunc();
        Map<Integer, GoFunc> functions = Map.of(7, fm, 8, fmp, 9, fnp);
        Map<String, Map<String, IfaceMethodTarget>> out = IfaceMethodTables.resolve(DOC, functions);
        assertEquals(2, out.size(), "two descriptors with methods");
        IfaceMethodTarget mt = out.get("main.T").get("M");
        assertTrue(mt != null, "main.T has M");
        assertTrue(mt.func() == fm, "M resolves to obj 7");
        assertTrue(mt.isDirect(), "missing wire index = direct method");
        String ptrName = DescriptorNames.descriptorName(
                new graalgo.frontend.TypeTable(
                        ((graalgo.frontend.Json.Object) graalgo.frontend.Json.parse(DOC))
                                .array("types", "t")),
                2);
        Map<String, IfaceMethodTarget> ptrMethods = out.get(ptrName);
        assertTrue(ptrMethods != null, "pointer entry present under " + ptrName);
        assertTrue(ptrMethods.get("M").func() == fmp, "M resolves to obj 8");
        assertTrue(ptrMethods.get("M").isDirect(), "empty index = direct");
        assertTrue(ptrMethods.get("N").func() == fnp, "N resolves to obj 9");
        assertTrue(!ptrMethods.get("N").isDirect(), "index [0] = promoted");
        assertEquals(1, ptrMethods.get("N").recvPath().length, "recvPath length");
        assertEquals(0, ptrMethods.get("N").recvPath()[0], "recvPath[0]");
        assertTrue(!out.containsKey("main.U"), "empty method list installs nothing");
    }

    private static void assertEquals(int expected, int actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /**
     * P3-M9: a wire {@code "embeddedIface"} entry (method promoted via an
     * embedded interface field) resolves to a redispatch target — null
     * GoFunc, the embedding path, and the embedded interface's id +
     * method names for the inner itab. A non-interface embeddedIface id
     * fails fast.
     */
    public void testEmbeddedIfaceRedispatchResolves() {
        String doc = """
                {"frontend": "v1",
                 "types": [
                   {"kind": "interface", "detail": {"methods": [{"name": "M"}, {"name": "N"}]}},
                   {"kind": "named", "detail": {"name": "main.Outer", "underlying": 0,
                     "methods": [{"name": "M", "obj": -1, "recv": "value", "index": [0],
                                  "embeddedIface": 0}]}},
                   {"kind": "basic", "detail": {"name": "int", "class": "int"}}
                 ],
                 "objects": [], "funcs": []}""";
        Map<String, Map<String, IfaceMethodTarget>> out =
                IfaceMethodTables.resolve(doc, Map.of());
        IfaceMethodTarget t = out.get("main.Outer").get("M");
        assertTrue(t != null, "method present");
        assertTrue(t.func() == null, "redispatch entry carries no GoFunc");
        assertTrue(t.isEmbeddedRedispatch(), "embeddedIfaceId >= 0 marks redispatch");
        assertEquals(0, t.embeddedIfaceId(), "embeddedIface id");
        assertEquals(java.util.List.of("M", "N"), t.embeddedIfaceMethods(),
                "embedded interface method names in declaration order");
        assertEquals(1, t.recvPath().length, "embedding path length");
        assertEquals(0, t.recvPath()[0], "embedding path");

        String badDoc = """
                {"frontend": "v1",
                 "types": [
                   {"kind": "basic", "detail": {"name": "int", "class": "int"}},
                   {"kind": "named", "detail": {"name": "main.Outer", "underlying": 0,
                     "methods": [{"name": "M", "obj": -1, "recv": "value", "index": [0],
                                  "embeddedIface": 0}]}}
                 ],
                 "objects": [], "funcs": []}""";
        try {
            IfaceMethodTables.resolve(badDoc, Map.of());
        } catch (FrontendException fe) {
            assertTrue(fe.getMessage().contains("not an interface type"),
                    "named fail-fast: " + fe.getMessage());
            return;
        }
        throw new AssertionError("expected FrontendException for non-interface embeddedIface id");
    }

    /**
     * A wire {@code "index": null} marks an unsupported promotion: the
     * target resolves but reports unsupported (the itab build fails fast
     * with the named P3-M3 scope error, not the table resolve).
     * verifies: GG-0014
     */
    public void testNullIndexMarksUnsupported() {
        String doc = """
                {"frontend": "v1",
                 "types": [
                   {"kind": "named", "detail": {"name": "main.V", "underlying": 0,
                     "methods": [{"name": "M", "obj": 3, "recv": "pointer", "index": null}]}},
                   {"kind": "basic", "detail": {"name": "int", "class": "int"}}
                 ],
                 "objects": [], "funcs": []}""";
        Map<String, Map<String, IfaceMethodTarget>> out =
                IfaceMethodTables.resolve(doc, Map.of(3, goFunc()));
        IfaceMethodTarget t = out.get("main.V").get("M");
        assertTrue(t != null, "method present");
        assertTrue(!t.supported(), "null index = unsupported promotion");
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

    /**
     * P3-M7R2: named interfaces with a slashed package path key by the
     * display name (e.g. "json.Number"), not the qualified name — the
     * dispatch lookup side uses iface.name(), the display name. A revert
     * to qualified-name keying would silently break itabForIntrinsic.
     * verifies: GG-0014
     */
    public void testResolveInterfacesSlashedPathKeysByDisplayName() {
        String doc = """
                {"frontend": "v1",
                 "types": [
                   {"kind": "interface", "detail": {"empty": false,
                     "methods": [{"name": "M", "sig": 2}], "embedded": []}},
                   {"kind": "named", "detail": {"name": "example.com/foo.Bar",
                     "underlying": 0, "methods": []}},
                   {"kind": "signature", "detail": {"params": [], "results": []}}],
                 "objects": [], "funcs": []}""";
        Map<String, IfaceMethodTable.IfaceWireEntry> out =
                IfaceMethodTables.resolveInterfaces(doc);
        assertTrue(out.containsKey("foo.Bar"),
                "slashed-path named interface keys by display name; keys=" + out.keySet());
        assertTrue(!out.containsKey("example.com/foo.Bar"),
                "must NOT key by qualified name");
        assertTrue(out.containsKey("interface#0"),
                "anonymous interface keys by interface#id");
    }
}
