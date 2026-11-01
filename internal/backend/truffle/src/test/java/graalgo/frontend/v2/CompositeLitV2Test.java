package graalgo.frontend.v2;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.source.Source;
import graalgo.frontend.CompositeLitNode;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * I2 (v2-only, P1) regression: {@code NodeFactoryV2.buildCompositeLit}
 * hardcoded {@code elemKind=INTERFACE}, {@code keyKind=INT64},
 * {@code arrayLen=-1} and null struct descriptor / zero templates,
 * discarding every type parameter of the literal's type.
 *
 * <p>Each test builds one v2 binary document through
 * {@link NodeFactoryV2#buildProgram} (the factory under test, not the
 * node class directly), finds the built {@link CompositeLitNode} in the
 * function body, executes it, and asserts the gc-correct value. Before
 * the fix every test below fails: struct literals NPE on the null
 * descriptor, array literals throw on the negative length, string-keyed
 * maps throw on the int64 key boxing, and slice literals come back on
 * the wrong (Object[]) backing store with null gaps.
 */
public final class CompositeLitV2Test {

    // --- tiny v2 binary encoder (mirrors BinaryDecoder's wire format) ---

    private static byte[] encodeDocument(Map<String, Object> doc) throws IOException {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        out.write('g');
        out.write('2');
        out.write('b');
        out.write('i');
        writeUvarint(out, 1); // format version
        encodeValue(out, doc);
        return out.toByteArray();
    }

    @SuppressWarnings("unchecked")
    private static void encodeValue(ByteArrayOutputStream out, Object v) throws IOException {
        if (v == null) {
            out.write(0x00);
        } else if (v instanceof Boolean b) {
            out.write(0x01);
            out.write(b ? 1 : 0);
        } else if (v instanceof Long l) {
            out.write(0x02);
            writeUvarint(out, (l << 1) ^ (l >> 63)); // zigzag
        } else if (v instanceof String s) {
            byte[] bytes = s.getBytes(StandardCharsets.UTF_8);
            out.write(0x03);
            writeUvarint(out, bytes.length);
            out.write(bytes);
        } else if (v instanceof List<?> list) {
            out.write(0x04);
            writeUvarint(out, list.size());
            for (Object e : list) {
                encodeValue(out, e);
            }
        } else if (v instanceof Map<?, ?> map) {
            out.write(0x05);
            writeUvarint(out, map.size());
            for (Map.Entry<?, ?> e : ((Map<String, Object>) map).entrySet()) {
                String keyStr = (String) e.getKey();
                byte[] key = keyStr.getBytes(StandardCharsets.UTF_8);
                writeUvarint(out, key.length);
                out.write(key);
                encodeValue(out, e.getValue());
            }
        } else {
            throw new IllegalArgumentException("cannot encode " + v);
        }
    }

    private static void writeUvarint(ByteArrayOutputStream out, long v) {
        while (true) {
            int b = (int) (v & 0x7F);
            v >>>= 7;
            if (v == 0) {
                out.write(b);
                return;
            }
            out.write(b | 0x80);
        }
    }

    // --- document builders ---

    private static Map<String, Object> map(Object... kv) {
        Map<String, Object> m = new LinkedHashMap<>();
        for (int i = 0; i < kv.length; i += 2) {
            m.put((String) kv[i], kv[i + 1]);
        }
        return m;
    }

    private static List<Object> list(Object... elems) {
        List<Object> l = new ArrayList<>();
        for (Object e : elems) {
            l.add(e);
        }
        return l;
    }

    private static Map<String, Object> basic(String name, String cls) {
        return map("kind", "basic", "detail", map("name", name, "class", cls, "untyped", false));
    }

    private static Map<String, Object> slice(long elem) {
        return map("kind", "slice", "detail", map("elem", elem));
    }

    private static Map<String, Object> array(long len, long elem) {
        return map("kind", "array", "detail", map("len", len, "elem", elem));
    }

    private static Map<String, Object> mapType(long key, long elem) {
        return map("kind", "map", "detail", map("key", key, "elem", elem));
    }

    private static Map<String, Object> structType(Object... fieldNameAndType) {
        List<Object> fields = new ArrayList<>();
        for (int i = 0; i < fieldNameAndType.length; i += 2) {
            fields.add(map("name", fieldNameAndType[i], "type", fieldNameAndType[i + 1],
                    "tag", "", "embedded", false, "offset", 0L));
        }
        return map("kind", "struct", "detail", map("fields", fields));
    }

    private static Map<String, Object> named(String name, long underlying) {
        return map("kind", "named",
                "detail", map("name", name, "underlying", underlying, "methods", list()));
    }

    private static Map<String, Object> node(String kind, Map<String, Object> fields) {
        return map("kind", kind, "fields", fields);
    }

    private static Map<String, Object> basiclit(String value, long type) {
        return node("basiclit", map("value", value, "type", type));
    }

    private static Map<String, Object> keyvalue(Map<String, Object> key, Map<String, Object> value) {
        return node("keyvalue", map("key", key, "value", value, "type", 0L));
    }

    private static Map<String, Object> ident(String name) {
        return node("ident", map("name", name));
    }

    private static Map<String, Object> compositelit(long type, List<Object> elems) {
        return node("compositelit", map("type", type, "elems", elems));
    }

    /** Builds a one-function program returning {@code lit} and executes the literal. */
    private static Object runLiteral(List<Object> types, Map<String, Object> lit) throws Exception {
        Map<String, Object> ret = node("return", map("results", list(lit)));
        Map<String, Object> body = node("block", map("stmts", list(ret)));
        Map<String, Object> fn = node("funcdecl",
                map("name", "f", "params", list(), "body", body));
        Map<String, Object> doc = map("frontend", "v2", "types", types,
                "objects", list(), "funcs", list(fn));
        Source src = Source.newBuilder("go", "", "test.go").build();
        NodeFactoryV2.V2Program program = new NodeFactoryV2(src).buildProgram(encodeDocument(doc));
        check(program.functionCount() == 1, "one function built");
        NodeFactoryV2.V2Function f = program.functions().get(0);
        CompositeLitNode litNode = findLit(f.body());
        check(litNode != null, "composite literal node found in built function");
        VirtualFrame frame = Truffle.getRuntime()
                .createVirtualFrame(new Object[0], f.frameDescriptor());
        return litNode.executeObject(frame);
    }

    private static CompositeLitNode findLit(Node n) {
        if (n instanceof CompositeLitNode c) {
            return c;
        }
        for (Node child : n.getChildren()) {
            CompositeLitNode found = findLit(child);
            if (found != null) {
                return found;
            }
        }
        return null;
    }

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static GoString gs(String s) {
        return new GoString(s.getBytes(StandardCharsets.UTF_8));
    }

    /** Type ids shared by the tests: 0=int, 1=string, 2=struct{X int, Y string}, 3=named main.Point. */
    private static List<Object> pointTypes() {
        return list(
                basic("int", "int"),
                basic("string", "string"),
                structType("X", 0L, "Y", 1L),
                named("main.Point", 2L));
    }

    public void testStructKeyed() throws Exception {
        Object v = runLiteral(pointTypes(),
                compositelit(3L, list(keyvalue(ident("X"), basiclit("7", 0L)))));
        check(v instanceof GoStruct, "keyed struct literal yields GoStruct, got " + v);
        GoStruct s = (GoStruct) v;
        check(s.descriptor().fields().size() == 2, "struct has 2 fields");
        check(s.getLong(0) == 7L, "Point{X:7}.X == 7, got " + s.getLong(0));
        check(GoString.EMPTY.equals(s.getObject(1)),
                "missing field Y is the zero string, got " + s.getObject(1));
    }

    public void testStructPositional() throws Exception {
        Object v = runLiteral(pointTypes(),
                compositelit(3L, list(basiclit("3", 0L), basiclit("\"hi\"", 1L))));
        check(v instanceof GoStruct, "positional struct literal yields GoStruct, got " + v);
        GoStruct s = (GoStruct) v;
        check(s.getLong(0) == 3L, "Point{3,...}.X == 3, got " + s.getLong(0));
        check(gs("hi").equals(s.getObject(1)), "Point{...,\"hi\"}.Y == hi, got " + s.getObject(1));
    }

    public void testArray() throws Exception {
        List<Object> types = list(basic("int", "int"), array(3L, 0L));
        Object v = runLiteral(types,
                compositelit(1L, list(basiclit("1", 0L), basiclit("2", 0L), basiclit("3", 0L))));
        check(v instanceof long[], "[3]int literal yields long[], got "
                + (v == null ? "null" : v.getClass().getSimpleName()));
        long[] a = (long[]) v;
        check(a.length == 3 && a[0] == 1L && a[1] == 2L && a[2] == 3L,
                "[3]int{1,2,3} == [1 2 3], got " + java.util.Arrays.toString(a));
    }

    public void testMapStringInt() throws Exception {
        List<Object> types = list(basic("int", "int"), basic("string", "string"),
                mapType(1L, 0L));
        Object v = runLiteral(types, compositelit(2L, list(
                keyvalue(basiclit("\"a\"", 1L), basiclit("1", 0L)),
                keyvalue(basiclit("\"b\"", 1L), basiclit("2", 0L)))));
        check(v instanceof GoMap, "map literal yields GoMap, got " + v);
        GoMap m = (GoMap) v;
        check(m.size() == 2, "map has 2 entries, got " + m.size());
        check(Long.valueOf(1L).equals(m.get(gs("a"))), "m[\"a\"] == 1, got " + m.get(gs("a")));
        check(Long.valueOf(2L).equals(m.get(gs("b"))), "m[\"b\"] == 2, got " + m.get(gs("b")));
    }

    public void testSlicePositional() throws Exception {
        List<Object> types = list(basic("int", "int"), slice(0L));
        Object v = runLiteral(types,
                compositelit(1L, list(basiclit("1", 0L), basiclit("2", 0L), basiclit("3", 0L))));
        check(v instanceof GoSlice, "slice literal yields GoSlice, got " + v);
        GoSlice s = (GoSlice) v;
        check(s.len() == 3, "slice len 3, got " + s.len());
        check(s.array() instanceof long[], "[]int backing is long[], got "
                + (s.array() == null ? "null" : s.array().getClass().getSimpleName()));
        long[] a = (long[]) s.array();
        check(a[0] == 1L && a[1] == 2L && a[2] == 3L,
                "[]int{1,2,3} == [1 2 3], got " + java.util.Arrays.toString(a));
    }

    public void testSliceKeyedGap() throws Exception {
        List<Object> types = list(basic("int", "int"), slice(0L));
        Object v = runLiteral(types, compositelit(1L, list(
                keyvalue(basiclit("1", 0L), basiclit("10", 0L)),
                keyvalue(basiclit("3", 0L), basiclit("30", 0L)))));
        check(v instanceof GoSlice, "keyed slice literal yields GoSlice, got " + v);
        GoSlice s = (GoSlice) v;
        check(s.len() == 4, "keyed slice len 4, got " + s.len());
        check(s.array() instanceof long[], "[]int backing is long[], got "
                + (s.array() == null ? "null" : s.array().getClass().getSimpleName()));
        long[] a = (long[]) s.array();
        check(a[0] == 0L && a[1] == 10L && a[2] == 0L && a[3] == 30L,
                "[]int{1:10, 3:30} == [0 10 0 30] (gaps are zeros), got "
                        + java.util.Arrays.toString(a));
    }

    public void testSliceStructElemGap() throws Exception {
        // []Point{1: Point{X:5}}: the gap at index 0 must be a zero Point,
        // not null — this is what the element zero template is for.
        List<Object> types = list(pointTypes().get(0), pointTypes().get(1),
                pointTypes().get(2), pointTypes().get(3), slice(3L));
        Map<String, Object> inner = compositelit(3L,
                list(keyvalue(ident("X"), basiclit("5", 0L))));
        Object v = runLiteral(types, compositelit(4L,
                list(keyvalue(basiclit("1", 0L), inner))));
        check(v instanceof GoSlice, "struct slice literal yields GoSlice, got " + v);
        GoSlice s = (GoSlice) v;
        check(s.len() == 2, "slice len 2, got " + s.len());
        Object[] a = (Object[]) s.array();
        check(a[0] instanceof GoStruct, "gap element is a zero GoStruct, got " + a[0]);
        GoStruct gap = (GoStruct) a[0];
        check(gap.getLong(0) == 0L && GoString.EMPTY.equals(gap.getObject(1)),
                "gap element is Point{0, \"\"}, got X=" + gap.getLong(0) + " Y=" + gap.getObject(1));
        check(a[1] instanceof GoStruct && ((GoStruct) a[1]).getLong(0) == 5L,
                "element 1 is Point{X:5}, got " + a[1]);
    }
}
