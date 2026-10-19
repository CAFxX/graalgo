package graalgo.frontend.v2;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.source.Source;
import graalgo.frontend.CompositeLitNode;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoStmtNode;
import graalgo.frontend.v2.NodeFactoryV2.V2Program;
import graalgo.values.GoMap;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import java.nio.charset.StandardCharsets;
import java.util.ArrayDeque;
import java.util.Deque;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * Unit tests for {@code NodeFactoryV2.buildCompositeLit} (review V-1,
 * 2026-10-19).
 *
 * <p>Each test hand-builds the decoded-document shape {@link BinaryDecoder}
 * produces (types table + one funcdecl whose body evaluates a single
 * composite literal) and executes the built {@link CompositeLitNode}.
 * Red-green: before the V-1 fix every construction test fails for a distinct
 * reason (NPE on structDesc, NegativeArraySizeException on arrayLen=-1,
 * Object[] instead of long[] for []int, mis-keyed maps); after the fix all
 * pass. The rejection test is a control that passes before and after.
 *
 * <p>These tests cover the factory layer only. The Go-pipeline → encode →
 * Java round trip is the V-2 lane's scope, not duplicated here.
 */
public final class NodeFactoryV2CompositeLitTest {

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static Map<String, Object> m(Object... kv) {
        check(kv.length % 2 == 0, "m() needs an even argument count");
        Map<String, Object> map = new HashMap<>();
        for (int i = 0; i < kv.length; i += 2) {
            map.put((String) kv[i], kv[i + 1]);
        }
        return map;
    }

    @SafeVarargs
    private static List<Object> l(Object... items) {
        return List.of(items);
    }

    // --- Type-table entry builders (mirror internal/frontend/v2/emit/emit.go) ---

    private static Map<String, Object> basicType(String name, String cls) {
        return m("kind", "basic",
                "detail", m("name", name, "class", cls, "untyped", false));
    }

    private static Map<String, Object> sliceType(int elem) {
        return m("kind", "slice", "detail", m("elem", (long) elem));
    }

    private static Map<String, Object> arrayType(long len, int elem) {
        return m("kind", "array", "detail", m("len", len, "elem", (long) elem));
    }

    private static Map<String, Object> mapType(int key, int elem) {
        return m("kind", "map", "detail", m("key", (long) key, "elem", (long) elem));
    }

    private static Map<String, Object> structType(Object... nameTypePairs) {
        check(nameTypePairs.length % 2 == 0, "structType needs name/type pairs");
        List<Object> fields = new java.util.ArrayList<>();
        for (int i = 0; i < nameTypePairs.length; i += 2) {
            fields.add(m("name", nameTypePairs[i], "type", (long) (Integer) nameTypePairs[i + 1],
                    "tag", "", "embedded", false, "offset", 0L));
        }
        return m("kind", "struct", "detail", m("fields", fields));
    }

    private static Map<String, Object> namedType(String name, int underlying) {
        return m("kind", "named",
                "detail", m("name", name, "underlying", (long) underlying, "methods", l()));
    }

    private static Map<String, Object> pointerType(int elem) {
        return m("kind", "pointer", "detail", m("elem", (long) elem));
    }

    // --- Node builders (mirror internal/frontend/v2/emit/nodes.go) ---

    private static Map<String, Object> basicLit(String value, int type) {
        return m("kind", "basiclit", "fields", m("value", value, "type", (long) type));
    }

    private static Map<String, Object> identKey(String name) {
        return m("kind", "ident", "fields", m("name", name));
    }

    private static Map<String, Object> keyValue(Map<String, Object> key, Map<String, Object> value) {
        return m("kind", "keyvalue", "fields", m("key", key, "value", value));
    }

    private static Map<String, Object> compositeLit(int type, List<Object> elems) {
        return m("kind", "compositelit", "fields", m("type", (long) type, "elems", elems));
    }

    /** Builds a program with one function whose body is a single expression statement. */
    private static V2Program buildProg(List<Object> types, Map<String, Object> expr) {
        Map<String, Object> body = m("kind", "block", "fields",
                m("stmts", l(m("kind", "exprstmt", "fields", m("x", expr)))));
        Map<String, Object> fn = m("kind", "funcdecl", "fields",
                m("name", "main", "params", l(), "body", body));
        Map<String, Object> doc = m("frontend", "v2", "types", types, "objects", l(),
                "funcs", l(fn));
        NodeFactoryV2 factory =
                new NodeFactoryV2(Source.newBuilder("v2test", "", "v2test").build());
        return factory.buildProgram(doc);
    }

    /** Finds the outermost CompositeLitNode in a built function body. */
    private static CompositeLitNode findLit(GoStmtNode body) {
        Deque<Node> work = new ArrayDeque<>();
        work.add(body);
        while (!work.isEmpty()) {
            Node n = work.removeFirst();
            if (n instanceof CompositeLitNode lit) {
                return lit;
            }
            for (Node c : n.getChildren()) {
                work.add(c);
            }
        }
        throw new AssertionError("no composite literal found in built body");
    }

    private static Object evalCompositeLit(List<Object> types, Map<String, Object> expr) {
        V2Program prog = buildProg(types, expr);
        check(prog.functionCount() == 1, "expected one function");
        CompositeLitNode lit = findLit(prog.functions().get(0).body());
        VirtualFrame frame =
                Truffle.getRuntime().createVirtualFrame(new Object[0], new FrameDescriptor());
        return lit.executeObject(frame);
    }

    private static GoString gs(String s) {
        return new GoString(s.getBytes(StandardCharsets.UTF_8));
    }

    // --- Tests ---

    public void testSliceOfIntBuildsLongArray() {
        // types: 0=int, 1=[]int
        List<Object> types = l(basicType("int", "int"), sliceType(0));
        Object v = evalCompositeLit(types, compositeLit(1,
                l(basicLit("1", 0), basicLit("2", 0), basicLit("3", 0))));
        check(v instanceof GoSlice, "[]int{1,2,3} must build a GoSlice");
        Object arr = ((GoSlice) v).array();
        check(arr instanceof long[], "[]int{1,2,3} backing must be long[], got "
                + (arr == null ? "null" : arr.getClass()));
        long[] longs = (long[]) arr;
        check(longs.length == 3 && longs[0] == 1 && longs[1] == 2 && longs[2] == 3,
                "[]int{1,2,3} contents wrong");
    }

    public void testArrayLitUsesDeclaredLength() {
        // types: 0=int, 1=[2]int
        List<Object> types = l(basicType("int", "int"), arrayType(2, 0));
        Object v = evalCompositeLit(types, compositeLit(1,
                l(basicLit("5", 0), basicLit("6", 0))));
        check(v instanceof long[], "[2]int must build a long[], got "
                + (v == null ? "null" : v.getClass()));
        long[] arr = (long[]) v;
        check(arr.length == 2 && arr[0] == 5 && arr[1] == 6, "[2]int{5,6} contents wrong");
    }

    public void testKeyedArrayLitZeroFills() {
        // types: 0=int, 1=[3]int ; [3]int{1: 9} == {0, 9, 0}
        List<Object> types = l(basicType("int", "int"), arrayType(3, 0));
        Object v = evalCompositeLit(types, compositeLit(1,
                l(keyValue(basicLit("1", 0), basicLit("9", 0)))));
        check(v instanceof long[], "keyed [3]int must build a long[]");
        long[] arr = (long[]) v;
        check(arr.length == 3 && arr[0] == 0 && arr[1] == 9 && arr[2] == 0,
                "[3]int{1:9} must be {0,9,0}");
    }

    public void testMapLitBoxesStringKeys() {
        // types: 0=string, 1=int, 2=map[string]int
        List<Object> types = l(basicType("string", "string"), basicType("int", "int"),
                mapType(0, 1));
        Object v = evalCompositeLit(types, compositeLit(2,
                l(keyValue(basicLit("\"a\"", 0), basicLit("1", 1)))));
        check(v instanceof GoMap, "map literal must build a GoMap");
        GoMap map = (GoMap) v;
        check(map.size() == 1, "map[string]int{\"a\":1} must have one entry");
        check(Long.valueOf(1).equals(map.get(gs("a"))),
                "map lookup by string key must find the int value");
    }

    public void testStructLitPositional() {
        // types: 0=int, 1=string, 2=struct{X int; Y string}
        List<Object> types = l(basicType("int", "int"), basicType("string", "string"),
                structType("X", 0, "Y", 1));
        Object v = evalCompositeLit(types, compositeLit(2,
                l(basicLit("7", 0), basicLit("\"s\"", 1))));
        check(v instanceof GoStruct, "struct literal must build a GoStruct");
        GoStruct st = (GoStruct) v;
        check(st.getLong(0) == 7, "struct field X must be 7");
        check(gs("s").equals(st.getObject(1)), "struct field Y must be \"s\"");
    }

    public void testKeyedNamedStructLit() {
        // types: 0=int, 1=struct{X int}, 2=Point (named -> 1)
        List<Object> types = l(basicType("int", "int"), structType("X", 0),
                namedType("main.Point", 1));
        Object v = evalCompositeLit(types, compositeLit(2,
                l(keyValue(identKey("X"), basicLit("1", 0)))));
        check(v instanceof GoStruct, "keyed named struct literal must build a GoStruct");
        GoStruct st = (GoStruct) v;
        check(st.getLong(0) == 1, "Point{X:1}.X must be 1");
        check("main.Point".equals(st.descriptor().typeName()),
                "descriptor must carry the named type's qualified name");
    }

    public void testPointerToStructLit() {
        // types: 0=int, 1=struct{X int}, 2=Point (named -> 1), 3=*Point
        List<Object> types = l(basicType("int", "int"), structType("X", 0),
                namedType("main.Point", 1), pointerType(2));
        Object v = evalCompositeLit(types, compositeLit(3,
                l(keyValue(identKey("X"), basicLit("2", 0)))));
        check(v instanceof GoPointer, "&Point{...} must build a GoPointer");
        Object ref = ((GoPointer) v).referent();
        check(ref instanceof GoStruct, "&Point{X:2} referent must be a GoStruct");
        check(((GoStruct) ref).getLong(0) == 2, "&Point{X:2}.X must be 2");
    }

    public void testSliceOfStructKeyedZeroFills() {
        // types: 0=int, 1=struct{X int}, 2=[]struct{X int}
        // []struct{X int}{1: {X: 2}} — index 0 must be a zero struct, not null.
        List<Object> types = l(basicType("int", "int"), structType("X", 0), sliceType(1));
        Map<String, Object> inner = compositeLit(1, l(basicLit("2", 0)));
        Object v = evalCompositeLit(types,
                compositeLit(2, l(keyValue(basicLit("1", 0), inner))));
        check(v instanceof GoSlice, "slice-of-struct must build a GoSlice");
        Object backing = ((GoSlice) v).array();
        check(backing instanceof Object[], "slice-of-struct backing must be an Object[]");
        Object[] arr = (Object[]) backing;
        check(arr.length == 2, "slice must have length 2");
        check(arr[0] instanceof GoStruct && ((GoStruct) arr[0]).getLong(0) == 0,
                "missing struct element must be a zero GoStruct, not null");
        check(arr[1] instanceof GoStruct && ((GoStruct) arr[1]).getLong(0) == 2,
                "given struct element must keep its value");
    }

    public void testNestedStructMissingFieldZeroFills() {
        // types: 0=int, 1=struct{V int} (Inner), 2=struct{In Inner; N int} (Outer)
        // Outer{N: 3} — the missing Inner field must be a zero struct, not null.
        List<Object> types = l(basicType("int", "int"), structType("V", 0),
                structType("In", 1, "N", 0));
        Object v = evalCompositeLit(types, compositeLit(2,
                l(keyValue(identKey("N"), basicLit("3", 0)))));
        check(v instanceof GoStruct, "Outer{N:3} must build a GoStruct");
        GoStruct outer = (GoStruct) v;
        check(outer.getLong(1) == 3, "Outer.N must be 3");
        Object in = outer.getObject(0);
        check(in instanceof GoStruct, "missing nested struct field must be a zero GoStruct");
        check(((GoStruct) in).getLong(0) == 0, "nested zero struct field must be 0");
    }

    public void testStringSliceKeyedZeroFills() {
        // types: 0=string, 1=int, 2=[]string ; []string{1: "b"} == {"", "b"}
        List<Object> types = l(basicType("string", "string"), basicType("int", "int"),
                sliceType(0));
        Object v = evalCompositeLit(types, compositeLit(2,
                l(keyValue(basicLit("1", 1), basicLit("\"b\"", 0)))));
        check(v instanceof GoSlice, "[]string must build a GoSlice");
        Object backing = ((GoSlice) v).array();
        check(backing instanceof Object[], "[]string backing must be an Object[]");
        Object[] arr = (Object[]) backing;
        check(arr.length == 2, "slice must have length 2");
        check(gs("").equals(arr[0]), "missing string element must be \"\", not null");
        check(gs("b").equals(arr[1]), "given string element must be \"b\"");
    }

    public void testCompositeLitOfBasicTypeFails() {
        // Control: a composite literal of a non-composite type is rejected loudly.
        List<Object> types = l(basicType("int", "int"));
        try {
            evalCompositeLit(types, compositeLit(0, l(basicLit("1", 0))));
        } catch (FrontendException e) {
            check(e.getMessage().contains("composite literal of"),
                    "error must name the composite literal problem, got: " + e.getMessage());
            return;
        }
        throw new AssertionError("composite literal of int must be rejected");
    }
}
