package graalgo.frontend.v2;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.frontend.FrontendException;
import graalgo.frontend.FuncConstNode;
import graalgo.frontend.MethodValueNode;
import graalgo.frontend.SelectorNode;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * Method-dispatch contract tests for the v2 factory (review R-P1-2).
 *
 * <p>Hand-rolled style like {@link BinaryDecoderTest}: {@code check()} +
 * {@code AssertionError}, public no-arg {@code test*} methods, registered
 * in {@code graalgo.test.Runner}.
 *
 * <p>Most tests build documents as Java maps in the exact wire shape the
 * Go emitter produces (see {@code internal/frontend/v2/emit}), and call
 * the package-private {@code buildProgram(Map)} overload — the same build
 * the {@code byte[]} overload delegates to after decoding, so the factory
 * logic is tested without re-implementing the binary encoder in Java.
 * One test ({@link #testGoldenBytesBuild}) goes through real Go-encoded
 * bytes produced by the emitter + encoder (see the Go generator, removed
 * after producing the hex: {@code emit.TestGenGoldenMethodValue}).
 */
public final class NodeFactoryV2MethodDispatchTest {

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static byte[] hex(String s) {
        check(s.length() % 2 == 0, "odd hex length");
        byte[] out = new byte[s.length() / 2];
        for (int i = 0; i < out.length; i++) {
            out[i] = (byte) Integer.parseInt(s.substring(2 * i, 2 * i + 2), 16);
        }
        return out;
    }

    // --- Minimal document builders (exact Go wire shape) ---

    private static Map<String, Object> basicType(String name, String cls) {
        Map<String, Object> detail = new HashMap<>();
        detail.put("class", cls);
        detail.put("name", name);
        detail.put("untyped", Boolean.FALSE);
        Map<String, Object> t = new HashMap<>();
        t.put("kind", "basic");
        t.put("detail", detail);
        return t;
    }

    /** Signature type entry; params/results are {name, type} maps. */
    private static Map<String, Object> sigType(List<Map<String, Object>> params,
            List<Map<String, Object>> results) {
        Map<String, Object> detail = new HashMap<>();
        detail.put("params", new ArrayList<>(params));
        detail.put("results", new ArrayList<>(results));
        detail.put("variadic", Boolean.FALSE);
        Map<String, Object> t = new HashMap<>();
        t.put("kind", "signature");
        t.put("detail", detail);
        return t;
    }

    private static Map<String, Object> sigResult(String name, long typeId) {
        Map<String, Object> r = new HashMap<>();
        r.put("name", name);
        r.put("type", typeId);
        return r;
    }

    private static Map<String, Object> object(String kind, String name, long typeId) {
        return object(kind, name, typeId, false);
    }

    private static Map<String, Object> object(String kind, String name, long typeId,
            boolean param) {
        Map<String, Object> o = new HashMap<>();
        o.put("kind", kind);
        o.put("name", name);
        o.put("type", typeId);
        o.put("param", param);
        return o;
    }

    private static Map<String, Object> node(String kind, Map<String, Object> fields) {
        Map<String, Object> n = new HashMap<>();
        n.put("kind", kind);
        n.put("fields", fields);
        return n;
    }

    private static Map<String, Object> block(List<Map<String, Object>> stmts) {
        Map<String, Object> f = new HashMap<>();
        f.put("stmts", new ArrayList<>(stmts));
        return node("block", f);
    }

    private static Map<String, Object> basiclit(String value, long typeId) {
        Map<String, Object> f = new HashMap<>();
        f.put("value", value);
        f.put("type", typeId);
        return node("basiclit", f);
    }

    private static Map<String, Object> ident(String name, long objId, long typeId) {
        Map<String, Object> f = new HashMap<>();
        f.put("name", name);
        f.put("obj", objId);
        f.put("type", typeId);
        return node("ident", f);
    }

    private static Map<String, Object> funcdecl(String name, long objId, long sigId,
            Map<String, Object> receiver, List<Map<String, Object>> params,
            Map<String, Object> body) {
        Map<String, Object> f = new HashMap<>();
        f.put("name", name);
        f.put("obj", objId);
        f.put("sig", sigId);
        if (receiver != null) {
            f.put("receiver", receiver);
        }
        f.put("params", new ArrayList<>(params));
        f.put("body", body);
        f.put("pkgpath", "main");
        return node("funcdecl", f);
    }

    private static Map<String, Object> param(String name, long objId, long typeId) {
        Map<String, Object> f = new HashMap<>();
        f.put("name", name);
        f.put("obj", objId);
        f.put("type", typeId);
        return node("param", f);
    }

    /** Method-set entry for a concrete (named/pointer/struct) type. */
    private static Map<String, Object> methodEntry(String name, long objId, long sigId,
            String implkey, String recv, List<Long> index) {
        Map<String, Object> m = new HashMap<>();
        m.put("name", name);
        m.put("obj", objId);
        m.put("sig", sigId);
        m.put("implkey", implkey);
        m.put("recv", recv);
        m.put("index", new ArrayList<>(index));
        m.put("addrOf", Boolean.FALSE);
        return m;
    }

    /** Method-set entry for an interface type: {name, sig, implkey} only. */
    private static Map<String, Object> ifaceMethodEntry(String name, long sigId,
            String implkey) {
        Map<String, Object> m = new HashMap<>();
        m.put("name", name);
        m.put("sig", sigId);
        m.put("implkey", implkey);
        return m;
    }

    private static NodeFactoryV2 factory() {
        return new NodeFactoryV2(null);
    }

    private static NodeFactoryV2.V2Function findFunction(NodeFactoryV2.V2Program program,
            String name) {
        for (NodeFactoryV2.V2Function f : program.functions()) {
            if (name.equals(f.name())) {
                return f;
            }
        }
        throw new AssertionError("no function named " + name);
    }

    /** Recursively finds the first node of the given class under root. */
    private static <T extends Node> T findNode(Node root, Class<T> cls) {
        if (cls.isInstance(root)) {
            return cls.cast(root);
        }
        for (Node child : root.getChildren()) {
            T found = findNode(child, cls);
            if (found != null) {
                return found;
            }
        }
        return null;
    }

    /**
     * Asserts that building the document throws FrontendException whose
     * message contains the given fragment. Returns the message.
     */
    private static String expectBuildError(Map<String, Object> doc, String fragment) {
        try {
            factory().buildProgram(doc);
        } catch (FrontendException e) {
            check(e.getMessage() != null && e.getMessage().contains(fragment),
                "error message <" + e.getMessage() + "> must contain <" + fragment + ">");
            return e.getMessage();
        }
        throw new AssertionError("expected build error containing <" + fragment + ">");
    }

    // --- The Count/Inc method-value document ---
    //
    // type Count int
    // func (c Count) Inc() int { return 1 }
    // func main() {
    //   var c Count = 5
    //   f := c.Inc
    //   _ = f
    // }
    //
    // Types: 0 int, 1 func() int, 2 func(), 3 named Count.
    // Objects: 0 func Inc, 1 func main, 2 var c (Inc receiver, param),
    //          3 var c (main local), 4 var f (main local).

    @SuppressWarnings("unchecked")
    private static Map<String, Object> countDoc() {
        List<Object> types = new ArrayList<>();
        types.add(basicType("int", "int")); // 0
        types.add(sigType(List.of(), List.of(sigResult("", 0L)))); // 1 func() int
        types.add(sigType(List.of(), List.of())); // 2 func()
        Map<String, Object> countDetail = new HashMap<>();
        countDetail.put("name", "main.Count");
        countDetail.put("underlying", 0L);
        countDetail.put("methods", new ArrayList<>(List.of(
            methodEntry("Inc", 0L, 1L, "Inc\u0000func()(basic:2)", "value", List.of(0L)))));
        Map<String, Object> count = new HashMap<>();
        count.put("kind", "named");
        count.put("detail", countDetail);
        types.add(count); // 3

        List<Object> objects = new ArrayList<>();
        objects.add(object("func", "Inc", 1L)); // 0
        objects.add(object("func", "main", 2L)); // 1
        objects.add(object("var", "c", 3L, true)); // 2 Inc receiver
        objects.add(object("var", "c", 3L)); // 3 main local
        objects.add(object("var", "f", 1L)); // 4 main local

        Map<String, Object> incBody = block(List.of(
            node("return", Map.of("results", List.of(basiclit("1", 0L))))));
        Map<String, Object> inc = funcdecl("Inc", 0L, 1L,
            param("c", 2L, 3L), List.of(), incBody);

        Map<String, Object> varC = node("vardecl", new HashMap<>(Map.of(
            "names", List.of("c"),
            "objs", List.of(3L),
            "define", Boolean.FALSE,
            "type", 3L,
            "values", List.of(basiclit("5", 0L)))));
        Map<String, Object> sel = node("selector", new HashMap<>(Map.of(
            "x", ident("c", 3L, 3L),
            "sel", "Inc",
            "selkind", "method",
            "index", List.of(0L),
            "type", 1L,
            "obj", 0L)));
        Map<String, Object> assignF = node("assign", Map.of(
            "tok", ":=",
            "lhs", List.of(ident("f", 4L, 1L)),
            "rhs", List.of(sel)));
        Map<String, Object> assignBlank = node("assign", Map.of(
            "tok", "=",
            "lhs", List.of(ident("_", -1L, 1L)),
            "rhs", List.of(ident("f", 4L, 1L))));
        Map<String, Object> mainBody = block(List.of(varC, assignF, assignBlank));
        Map<String, Object> main = funcdecl("main", 1L, 2L, null, List.of(), mainBody);

        Map<String, Object> doc = new HashMap<>();
        doc.put("frontend", "v2");
        doc.put("package", Map.of("name", "main", "files", List.of("src.go"), "lang", "go1.27"));
        doc.put("types", types);
        doc.put("objects", objects);
        doc.put("funcs", List.of(inc, main));
        doc.put("globals", List.of());
        return doc;
    }

    /** The method selector map inside countDoc's main body. */
    @SuppressWarnings("unchecked")
    private static Map<String, Object> countSelector(Map<String, Object> doc) {
        List<Object> funcs = (List<Object>) doc.get("funcs");
        Map<String, Object> main = (Map<String, Object>) funcs.get(1);
        Map<String, Object> fields = (Map<String, Object>) main.get("fields");
        Map<String, Object> body = (Map<String, Object>) fields.get("body");
        List<Object> stmts = (List<Object>) ((Map<String, Object>) body.get("fields")).get("stmts");
        Map<String, Object> assign = (Map<String, Object>) stmts.get(1);
        List<Object> rhs = (List<Object>) ((Map<String, Object>) assign.get("fields")).get("rhs");
        return (Map<String, Object>) rhs.get(0);
    }

    /** The Count named type's detail map inside countDoc. */
    @SuppressWarnings("unchecked")
    private static Map<String, Object> countDetail(Map<String, Object> doc) {
        List<Object> types = (List<Object>) doc.get("types");
        Map<String, Object> count = (Map<String, Object>) types.get(3);
        return (Map<String, Object>) count.get("detail");
    }

    /** The Inc method entry inside countDoc's Count method set. */
    @SuppressWarnings("unchecked")
    private static Map<String, Object> countMethodEntry(Map<String, Object> doc) {
        List<Object> methods = (List<Object>) countDetail(doc).get("methods");
        return (Map<String, Object>) methods.get(0);
    }

    // --- Tests ---

    public void testValidMethodSetDocBuilds() {
        NodeFactoryV2.V2Program program = factory().buildProgram(countDoc());
        check(program.functionCount() == 2, "2 functions, got " + program.functionCount());
        check(findFunction(program, "Inc").objId() == 0, "Inc objId 0");
        check(findFunction(program, "main").objId() == 1, "main objId 1");
        // The funcTable is empty until the v2 launcher registers
        // RootCallTargets (see V2Program.funcTable): method values resolve
        // lazily, so the build succeeds with an empty table.
        check(program.funcTable().isEmpty(), "funcTable starts empty");
        Node mainBody = findFunction(program, "main").body();
        check(findNode(mainBody, MethodValueNode.class) != null,
            "main body contains a MethodValueNode");
    }

    public void testMethodValueExecFailsLoudlyUnresolved() {
        NodeFactoryV2.V2Program program = factory().buildProgram(countDoc());
        NodeFactoryV2.V2Function main = findFunction(program, "main");
        VirtualFrame frame = Truffle.getRuntime()
            .createVirtualFrame(new Object[0], main.frameDescriptor());
        try {
            main.body().executeVoid(frame);
        } catch (FrontendException e) {
            check(e.getMessage() != null && e.getMessage().contains("unresolved method object 0"),
                "message names unresolved method object 0, got: " + e.getMessage());
            return;
        }
        throw new AssertionError("executing c.Inc did not throw");
    }

    public void testMethodExprBuildsFuncConstNode() {
        // f := Count.Inc — the selector's x is the TypeName ident, which
        // is not a value: the factory must not try to build it as one.
        Map<String, Object> doc = countDoc();
        @SuppressWarnings("unchecked")
        List<Object> objects = (List<Object>) doc.get("objects");
        objects.add(object("typename", "Count", 3L)); // 5
        Map<String, Object> sel = countSelector(doc);
        @SuppressWarnings("unchecked")
        Map<String, Object> fields = (Map<String, Object>) sel.get("fields");
        fields.put("x", ident("Count", 5L, 3L));
        fields.put("selkind", "methodexpr");
        NodeFactoryV2.V2Program program = factory().buildProgram(doc);
        Node mainBody = findFunction(program, "main").body();
        check(findNode(mainBody, FuncConstNode.class) != null,
            "main body contains a FuncConstNode");
        check(findNode(mainBody, MethodValueNode.class) == null,
            "no MethodValueNode for a method expression");
        // Executing it fails loudly the same way (empty funcTable).
        NodeFactoryV2.V2Function main = findFunction(program, "main");
        VirtualFrame frame = Truffle.getRuntime()
            .createVirtualFrame(new Object[0], main.frameDescriptor());
        try {
            main.body().executeVoid(frame);
        } catch (FrontendException e) {
            check(e.getMessage() != null
                    && e.getMessage().contains("unresolved function object 0"),
                "message names unresolved function object 0, got: " + e.getMessage());
            return;
        }
        throw new AssertionError("executing Count.Inc did not throw");
    }

    public void testInterfaceReceiverMethodRejected() {
        // func Use(s Shaper) float64 { return s.Area() } — the method
        // selector's receiver has an interface type (named Shaper whose
        // underlying type is the interface).
        List<Object> types = new ArrayList<>();
        types.add(basicType("float64", "float")); // 0
        types.add(sigType(List.of(), List.of(sigResult("", 0L)))); // 1 func() float64
        types.add(sigType(List.of(), List.of())); // 2 func()
        Map<String, Object> shaperDetail = new HashMap<>();
        shaperDetail.put("empty", Boolean.FALSE);
        shaperDetail.put("methods", new ArrayList<>(List.of(
            ifaceMethodEntry("Area", 1L, "Area\u0000func()(basic:6)"))));
        shaperDetail.put("embedded", List.of());
        Map<String, Object> shaperIface = new HashMap<>();
        shaperIface.put("kind", "interface");
        shaperIface.put("detail", shaperDetail);
        types.add(shaperIface); // 3
        Map<String, Object> shaperNamedDetail = new HashMap<>();
        shaperNamedDetail.put("name", "main.Shaper");
        shaperNamedDetail.put("underlying", 3L);
        shaperNamedDetail.put("methods", new ArrayList<>(List.of(
            methodEntry("Area", 0L, 1L, "Area\u0000func()(basic:6)", "value", List.of(0L)))));
        Map<String, Object> shaperNamed = new HashMap<>();
        shaperNamed.put("kind", "named");
        shaperNamed.put("detail", shaperNamedDetail);
        types.add(shaperNamed); // 4
        types.add(sigType(List.of(sigResult("s", 4L)),
            List.of(sigResult("", 0L)))); // 5 func(Shaper) float64

        List<Object> objects = new ArrayList<>();
        objects.add(object("func", "Area", 1L)); // 0 interface method func
        objects.add(object("func", "Use", 5L)); // 1
        objects.add(object("var", "s", 4L, true)); // 2 Use param

        Map<String, Object> sel = node("selector", new HashMap<>(Map.of(
            "x", ident("s", 2L, 4L),
            "sel", "Area",
            "selkind", "method",
            "index", List.of(0L),
            "type", 1L,
            "obj", 0L)));
        Map<String, Object> call = node("call", new HashMap<>(Map.of(
            "fun", sel,
            "args", List.of(),
            "type", 0L,
            "ellipsis", Boolean.FALSE)));
        Map<String, Object> useBody = block(List.of(
            node("return", Map.of("results", List.of(call)))));
        Map<String, Object> use = funcdecl("Use", 1L, 5L, null,
            List.of(param("s", 2L, 4L)), useBody);

        Map<String, Object> doc = new HashMap<>();
        doc.put("frontend", "v2");
        doc.put("package", Map.of("name", "main", "files", List.of("src.go"), "lang", "go1.27"));
        doc.put("types", types);
        doc.put("objects", objects);
        doc.put("funcs", List.of(use));
        doc.put("globals", List.of());

        expectBuildError(doc, "interface dispatch");
    }

    public void testRedispatchObjMinusOneRejected() {
        // Embedded-interface promotion entry (obj -1) reaching a selector:
        // redispatch is not implemented yet, so the build must fail loudly
        // instead of binding the wrong function.
        Map<String, Object> doc = countDoc();
        Map<String, Object> sel = countSelector(doc);
        @SuppressWarnings("unchecked")
        Map<String, Object> fields = (Map<String, Object>) sel.get("fields");
        fields.put("obj", -1L);
        expectBuildError(doc, "redispatch");
    }

    public void testMissingFuncdeclObjRejected() {
        Map<String, Object> doc = countDoc();
        @SuppressWarnings("unchecked")
        List<Object> funcs = (List<Object>) doc.get("funcs");
        Map<String, Object> main = (Map<String, Object>) funcs.get(1);
        @SuppressWarnings("unchecked")
        Map<String, Object> fields = (Map<String, Object>) main.get("fields");
        fields.remove("obj");
        expectBuildError(doc, "without obj");
    }

    public void testMissingSelectorObjRejected() {
        Map<String, Object> doc = countDoc();
        Map<String, Object> sel = countSelector(doc);
        @SuppressWarnings("unchecked")
        Map<String, Object> fields = (Map<String, Object>) sel.get("fields");
        fields.remove("obj");
        expectBuildError(doc, "without obj");
    }

    public void testBadMethodTableRejected() {
        // Duplicate method name in one set.
        Map<String, Object> dup = countDoc();
        @SuppressWarnings("unchecked")
        List<Object> methods = (List<Object>) countDetail(dup).get("methods");
        methods.add(new HashMap<>(countMethodEntry(dup)));
        expectBuildError(dup, "duplicate method");

        // Method obj resolving to a var object, not a func object.
        Map<String, Object> badObj = countDoc();
        countMethodEntry(badObj).put("obj", 3L);
        expectBuildError(badObj, "want func");

        // Method sig resolving to a basic type, not a signature.
        Map<String, Object> badSig = countDoc();
        countMethodEntry(badSig).put("sig", 0L);
        expectBuildError(badSig, "want signature");

        // Empty implkey.
        Map<String, Object> badKey = countDoc();
        countMethodEntry(badKey).put("implkey", "");
        expectBuildError(badKey, "without an implkey");

        // Bad receiver kind.
        Map<String, Object> badRecv = countDoc();
        countMethodEntry(badRecv).put("recv", "weird");
        expectBuildError(badRecv, "recv must be");

        // embeddedIface resolving to a non-interface type.
        Map<String, Object> badIface = countDoc();
        countMethodEntry(badIface).put("embeddedIface", 0L);
        expectBuildError(badIface, "want interface");
    }

    // --- Struct offsets ---

    /**
     * Builds a document with one named struct type (Point) and a main
     * that reads a field, so the factory exercises the struct path.
     * Types: 0 int, 1 func(), 2 struct Point, 3 named Point.
     * Objects: 0 func main, 1 var p, 2 var x.
     */
    @SuppressWarnings("unchecked")
    private static Map<String, Object> pointDoc(List<Long> offsets) {
        List<Object> types = new ArrayList<>();
        types.add(basicType("int", "int")); // 0
        types.add(sigType(List.of(), List.of())); // 1 func()
        List<Object> fields = new ArrayList<>();
        String[] names = {"X", "Y"};
        for (int i = 0; i < names.length; i++) {
            Map<String, Object> f = new HashMap<>();
            f.put("name", names[i]);
            f.put("type", 0L);
            f.put("tag", "");
            f.put("embedded", Boolean.FALSE);
            f.put("offset", offsets.get(i));
            fields.add(f);
        }
        Map<String, Object> structDetail = new HashMap<>();
        structDetail.put("fields", fields);
        structDetail.put("methods", List.of());
        Map<String, Object> struct = new HashMap<>();
        struct.put("kind", "struct");
        struct.put("detail", structDetail);
        types.add(struct); // 2
        Map<String, Object> namedDetail = new HashMap<>();
        namedDetail.put("name", "main.Point");
        namedDetail.put("underlying", 2L);
        namedDetail.put("methods", List.of());
        Map<String, Object> named = new HashMap<>();
        named.put("kind", "named");
        named.put("detail", namedDetail);
        types.add(named); // 3

        List<Object> objects = new ArrayList<>();
        objects.add(object("func", "main", 1L)); // 0
        objects.add(object("var", "p", 3L)); // 1
        objects.add(object("var", "x", 0L)); // 2

        // var p Point; x := p.X — the field selector's receiver has the
        // NAMED struct type, which the factory must resolve to the struct.
        Map<String, Object> varP = node("vardecl", new HashMap<>(Map.of(
            "names", List.of("p"),
            "objs", List.of(1L),
            "define", Boolean.FALSE,
            "type", 3L,
            "values", List.of())));
        Map<String, Object> sel = node("selector", new HashMap<>(Map.of(
            "x", ident("p", 1L, 3L),
            "sel", "X",
            "selkind", "field",
            "index", List.of(0L),
            "type", 0L)));
        Map<String, Object> assignX = node("assign", Map.of(
            "tok", ":=",
            "lhs", List.of(ident("x", 2L, 0L)),
            "rhs", List.of(sel)));
        Map<String, Object> mainBody = block(List.of(varP, assignX));
        Map<String, Object> main = funcdecl("main", 0L, 1L, null, List.of(), mainBody);

        Map<String, Object> doc = new HashMap<>();
        doc.put("frontend", "v2");
        doc.put("package", Map.of("name", "main", "files", List.of("src.go"), "lang", "go1.27"));
        doc.put("types", types);
        doc.put("objects", objects);
        doc.put("funcs", List.of(main));
        doc.put("globals", List.of());
        return doc;
    }

    public void testFieldSelectorOnNamedStructBuilds() {
        NodeFactoryV2.V2Program program = factory().buildProgram(pointDoc(List.of(0L, 8L)));
        Node mainBody = findFunction(program, "main").body();
        check(findNode(mainBody, SelectorNode.class) != null,
            "main body contains a SelectorNode for p.X");
    }

    public void testValidStructOffsetsAccepted() {
        // Non-decreasing offsets, including a repeated offset (zero-size
        // field shares its predecessor's offset) and the -1 unknown
        // sentinel (generic template): all valid per the contract.
        factory().buildProgram(pointDoc(List.of(0L, 8L)));
        factory().buildProgram(pointDoc(List.of(-1L, -1L)));
    }

    public void testNonMonotonicStructOffsetsRejected() {
        // X@8 then Y@0: offsets must be non-decreasing.
        Map<String, Object> doc = pointDoc(List.of(8L, 0L));
        expectBuildError(doc, "non-decreasing");
    }

    public void testInterfaceMethodWithObjRejected() {
        // Interface method entries must not carry obj (no funcdecl exists).
        // Add an interface type whose method carries obj to countDoc.
        Map<String, Object> doc = countDoc();
        @SuppressWarnings("unchecked")
        List<Object> types = (List<Object>) doc.get("types");
        Map<String, Object> ifaceDetail = new HashMap<>();
        ifaceDetail.put("empty", Boolean.FALSE);
        ifaceDetail.put("methods", new ArrayList<>(List.of(
            methodEntry("M", 0L, 1L, "M\u0000func()(basic:2)", "value", List.of(0L)))));
        ifaceDetail.put("embedded", List.of());
        Map<String, Object> iface = new HashMap<>();
        iface.put("kind", "interface");
        iface.put("detail", ifaceDetail);
        types.add(iface);
        expectBuildError(doc, "must not carry obj");
    }

    // --- Golden bytes (real emitter + encoder output) ---

    /**
     * Golden document, byte-identical to the Go encoder output for:
     * <pre>
     *   package main
     *   type Count int
     *   func (c Count) Inc() int { return 1 }
     *   func main() {
     *     var c Count = 5
     *     f := c.Inc
     *     _ = f
     *   }
     * </pre>
     * Produced by {@code emit.TestGenGoldenMethodValue} (temporary
     * generator, removed after producing the hex). If the emitter changes
     * the wire shape, regenerate the hex the same way.
     */
    private static final String GOLDEN_METHOD_VALUE_HEX =
        "673262690105070866726f6e74656e64030276320566756e637304020503066669656c6473050704626f6479050306666965" +
        "6c647305010573746d747304010503066669656c6473050107726573756c747304010503066669656c6473050305636f6e73" +
        "74030131047479706502020576616c7565030131046b696e64030862617369636c697403706f73050503636f6c0246046669" +
        "6c650200036c656e0202046c696e65020a036f6666028001046b696e64030672657475726e03706f73050503636f6c023804" +
        "66696c650200036c656e0210046c696e65020a036f66660272046b696e640305626c6f636b03706f73050503636f6c023404" +
        "66696c650200036c656e0218046c696e65020a036f6666026e046e616d650303496e63036f626a020006706172616d730400" +
        "07706b677061746803046d61696e0872656365697665720503066669656c64730503046e616d65030163036f626a02040474" +
        "7970650206046b696e640305706172616d03706f73050503636f6c020e0466696c650200036c656e0202046c696e65020a03" +
        "6f66660248037369670200046b696e64030866756e636465636c03706f73050503636f6c02020466696c650200036c656e02" +
        "4a046c696e65020a036f6666023c0503066669656c6473050704626f64790503066669656c647305010573746d7473040305" +
        "03066669656c6473050506646566696e650100056e616d65730401030163046f626a7304010206047479706502060676616c" +
        "75657304010503066669656c6473050305636f6e7374030135047479706502060576616c7565030135046b696e6403086261" +
        "7369636c697403706f73050503636f6c02200466696c650200036c656e0202046c696e650210036f666602c401046b696e64" +
        "03077661726465636c03706f73050503636f6c020c0466696c650200036c656e0216046c696e650210036f666602b0010503" +
        "066669656c64730503036c687304010503066669656c64730503046e616d65030166036f626a020804747970650208046b69" +
        "6e6403056964656e7403706f73050503636f6c02040466696c650200036c656e0202046c696e650212036f666602ca010372" +
        "687304010503066669656c6473050605696e64657804010200036f626a02000373656c0303496e630773656c6b696e640306" +
        "6d6574686f640474797065020801780503066669656c64730503046e616d65030163036f626a020604747970650206046b69" +
        "6e6403056964656e7403706f73050503636f6c020e0466696c650200036c656e0202046c696e650212036f666602d401046b" +
        "696e64030873656c6563746f7203706f73050503636f6c020e0466696c650200036c656e020a046c696e650212036f666602" +
        "d40103746f6b03023a3d046b696e64030661737369676e03706f73050503636f6c02040466696c650200036c656e0214046c" +
        "696e650212036f666602ca010503066669656c64730503036c687304010503066669656c64730501046e616d6503015f046b" +
        "696e6403056964656e7403706f73050503636f6c02040466696c650200036c656e0202046c696e650214036f666602e20103" +
        "72687304010503066669656c64730503046e616d65030166036f626a020804747970650208046b696e6403056964656e7403" +
        "706f73050503636f6c020c0466696c650200036c656e0202046c696e650214036f666602ea0103746f6b03013d046b696e64" +
        "030661737369676e03706f73050503636f6c02040466696c650200036c656e020a046c696e650214036f666602e201046b69" +
        "6e640305626c6f636b03706f73050503636f6c021a0466696c650200036c656e024e046c696e65020e036f666602a201046e" +
        "616d6503046d61696e036f626a020206706172616d73040007706b677061746803046d61696e087265636569766572000373" +
        "69670204046b696e64030866756e636465636c03706f73050503636f6c02020466696c650200036c656e0266046c696e6502" +
        "0e036f6666028a010967656e657261746f72050306676f726f6f740308676f312e32372e3104746f6f6c030d676f66726f6e" +
        "74656e642d76320776657273696f6e0305302e312e3007676c6f62616c730400076f626a6563747304050503046b696e6403" +
        "0466756e63046e616d650303496e63047479706502000503046b696e64030466756e63046e616d6503046d61696e04747970" +
        "6502040504046b696e640303766172046e616d6503016305706172616d0101047479706502060504046b696e640303766172" +
        "046e616d6503016305706172616d0100047479706502060504046b696e640303766172046e616d6503016605706172616d01" +
        "0004747970650208077061636b61676505030566696c6573040103067372632e676f046c616e670306676f312e3237046e61" +
        "6d6503046d61696e057479706573040505020664657461696c050306706172616d73040007726573756c747304010502046e" +
        "616d650300047479706502020876617269616469630100046b696e6403097369676e617475726505020664657461696c0503" +
        "05636c6173730303696e74046e616d650303696e7407756e74797065640100046b696e640305626173696305020664657461" +
        "696c050306706172616d73040007726573756c747304000876617269616469630100046b696e6403097369676e6174757265" +
        "05020664657461696c0503076d6574686f64730401050706616464724f66010007696d706c6b65790313496e630066756e63" +
        "28292862617369633a322905696e6465780400046e616d650303496e63036f626a02000472656376030576616c7565037369" +
        "670200046e616d65030a6d61696e2e436f756e740a756e6465726c79696e670202046b696e6403056e616d65640502066465" +
        "7461696c050306706172616d73040007726573756c747304010502046e616d65030004747970650202087661726961646963" +
        "0100046b696e6403097369676e6174757265";

    public void testGoldenBytesBuild() throws Exception {
        NodeFactoryV2.V2Program program =
            factory().buildProgram(hex(GOLDEN_METHOD_VALUE_HEX));
        check(program.functionCount() == 2, "2 functions, got " + program.functionCount());
        check(findFunction(program, "Inc").objId() == 0, "Inc objId 0");
        check(findFunction(program, "main").objId() == 1, "main objId 1");
        Node mainBody = findFunction(program, "main").body();
        check(findNode(mainBody, MethodValueNode.class) != null,
            "golden main body contains a MethodValueNode");
    }
}
