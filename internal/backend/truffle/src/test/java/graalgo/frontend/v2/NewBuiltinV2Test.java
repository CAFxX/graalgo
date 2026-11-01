package graalgo.frontend.v2;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.source.Source;
import graalgo.frontend.ConstNode;
import graalgo.frontend.DerefNode;
import graalgo.frontend.DerefWriteNode;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.NewNode;
import graalgo.frontend.SelectorNode;
import graalgo.frontend.SelectorWriteNode;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Factory-level reproducer / regression tests for the v2 {@code new} builtin
 * (review finding I1): the v2 factory built {@code new(T)} as
 * {@code new NewNode(GoKind.POINTER, null)}, discarding T, so
 * {@code new(int)} allocated a pointer-typed zero, {@code new(S)} had no
 * field layout, and {@code new([N]T)} had no array lengths.
 *
 * <p>Each test builds a minimal v2 binary document (via the same wire format
 * the Go encoder produces) holding one {@code var p = new(T)} declaration,
 * runs it through {@link NodeFactoryV2#buildProgram}, walks the built tree
 * to the {@link NewNode}, and drives the allocated pointer through the
 * standard deref/selector nodes — the consumer path for a {@code new}
 * result. The v2 factory currently has no deref expression syntax
 * ({@code *p} as an expression, and {@code *p = v} as an assignment LHS,
 * are not emittable node kinds), so the write/read half is exercised via
 * {@link DerefNode}/{@link DerefWriteNode} directly; that gap is separate
 * from I1 and is not covered here.
 */
public final class NewBuiltinV2Test {

    // Type-table ids shared by every test document.
    private static final int T_INT = 0;
    private static final int T_PTR_INT = 1;
    private static final int T_STRUCT_ANON = 2;
    private static final int T_PTR_STRUCT_ANON = 3;
    private static final int T_STRUCT_NAMED = 4;
    private static final int T_PTR_STRUCT_NAMED = 5;
    private static final int T_ARRAY3_INT = 6;
    private static final int T_PTR_ARRAY3_INT = 7;
    private static final int T_PTR_PTR_INT = 8;
    private static final int T_ARRAY2_ARRAY3_INT = 9;
    private static final int T_PTR_ARRAY2_ARRAY3_INT = 10;

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static Map<String, Object> map(Object... kv) {
        Map<String, Object> m = new LinkedHashMap<>();
        for (int i = 0; i < kv.length; i += 2) {
            m.put((String) kv[i], kv[i + 1]);
        }
        return m;
    }

    private static List<Object> list(Object... items) {
        List<Object> l = new ArrayList<>();
        for (Object o : items) {
            l.add(o);
        }
        return l;
    }

    private static Map<String, Object> basicType(String name, String cls) {
        return map("kind", "basic",
                "detail", map("name", name, "class", cls, "untyped", Boolean.FALSE));
    }

    private static Map<String, Object> ptrType(int elem) {
        return map("kind", "pointer", "detail", map("elem", (long) elem));
    }

    private static Map<String, Object> structType(String fieldName, int fieldType) {
        return map("kind", "struct",
                "detail", map("fields", list(
                        map("name", fieldName, "type", (long) fieldType,
                                "tag", "", "embedded", Boolean.FALSE, "offset", 0L))));
    }

    private static List<Object> typeTable() {
        return list(
                basicType("int", "int"), // 0
                ptrType(T_INT), // 1: *int
                structType("X", T_INT), // 2: struct{X int}
                ptrType(T_STRUCT_ANON), // 3
                map("kind", "named", // 4: main.S
                        "detail", map("name", "main.S",
                                "underlying", (long) T_STRUCT_ANON, "methods", list())),
                ptrType(T_STRUCT_NAMED), // 5: *main.S
                map("kind", "array", // 6: [3]int
                        "detail", map("len", 3L, "elem", (long) T_INT)),
                ptrType(T_ARRAY3_INT), // 7: *[3]int
                ptrType(T_PTR_INT), // 8: **int
                map("kind", "array", // 9: [2][3]int
                        "detail", map("len", 2L, "elem", (long) T_ARRAY3_INT)),
                ptrType(T_ARRAY2_ARRAY3_INT)); // 10: *[2][3]int
    }

    /** Builds a v2 document whose single function declares {@code v = new(T)}. */
    private static byte[] newDocument(int resultTypeId) {
        Map<String, Object> call = map("kind", "call",
                "fields", map(
                        "fun", map("kind", "ident",
                                "fields", map("name", "new", "builtin", "new")),
                        "args", list(),
                        "builtin", "new",
                        "type", (long) resultTypeId));
        Map<String, Object> vardecl = map("kind", "vardecl",
                "fields", map(
                        "names", list("v"),
                        "objs", list(0L),
                        "values", list(call)));
        Map<String, Object> body = map("kind", "block",
                "fields", map("stmts", list(vardecl)));
        Map<String, Object> func = map("kind", "funcdecl",
                "fields", map(
                        "name", "main",
                        "params", list(),
                        "body", body));
        Map<String, Object> doc = map(
                "frontend", "v2",
                "package", map("name", "main"),
                "types", typeTable(),
                "objects", list(map("name", "v", "type", (long) resultTypeId)),
                "funcs", list(func),
                "globals", list());
        return encode(doc);
    }

    /** Minimal encoder for the v2 wire format (see BinaryDecoder). */
    private static byte[] encode(Map<String, Object> doc) {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        out.write('g');
        out.write('2');
        out.write('b');
        out.write('i');
        writeVarint(out, 1); // format version
        writeValue(out, doc);
        return out.toByteArray();
    }

    @SuppressWarnings("unchecked")
    private static void writeValue(ByteArrayOutputStream out, Object v) {
        if (v == null) {
            out.write(0x00);
        } else if (v instanceof Boolean b) {
            out.write(0x01);
            out.write(b ? 1 : 0);
        } else if (v instanceof Long l) {
            out.write(0x02);
            writeVarint(out, (l.longValue() << 1) ^ (l.longValue() >> 63));
        } else if (v instanceof String s) {
            byte[] bytes = s.getBytes(StandardCharsets.UTF_8);
            out.write(0x03);
            writeVarint(out, bytes.length);
            out.writeBytes(bytes);
        } else if (v instanceof List<?> l) {
            out.write(0x04);
            writeVarint(out, l.size());
            for (Object e : l) {
                writeValue(out, e);
            }
        } else if (v instanceof Map<?, ?> m) {
            out.write(0x05);
            writeVarint(out, m.size());
            for (Map.Entry<?, ?> e : ((Map<String, Object>) m).entrySet()) {
                byte[] key = ((String) e.getKey()).getBytes(StandardCharsets.UTF_8);
                writeVarint(out, key.length);
                out.writeBytes(key);
                writeValue(out, e.getValue());
            }
        } else {
            throw new IllegalArgumentException("unsupported test value: " + v);
        }
    }

    private static void writeVarint(ByteArrayOutputStream out, long v) {
        long x = v;
        while (true) {
            int b = (int) (x & 0x7F);
            x >>>= 7;
            if (x == 0) {
                out.write(b);
                return;
            }
            out.write(b | 0x80);
        }
    }

    private static NewNode findNewNode(NodeFactoryV2.V2Program program) throws Exception {
        check(program.functionCount() == 1, "expected 1 function");
        List<NewNode> found = new ArrayList<>();
        collect(program.functions().get(0).body(), found);
        check(found.size() == 1, "expected exactly 1 NewNode, found " + found.size());
        return found.get(0);
    }

    private static void collect(com.oracle.truffle.api.nodes.Node node, List<NewNode> out) {
        if (node instanceof NewNode n) {
            out.add(n);
        }
        for (com.oracle.truffle.api.nodes.Node child : node.getChildren()) {
            collect(child, out);
        }
    }

    private static VirtualFrame frame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0], new FrameDescriptor());
    }

    private static NewNode buildNew(int resultTypeId) throws Exception {
        Source source = Source.newBuilder("go", "", "test").build();
        NodeFactoryV2.V2Program program =
                new NodeFactoryV2(source).buildProgram(newDocument(resultTypeId));
        return findNewNode(program);
    }

    /** A constant pointer node, for driving deref/selector nodes in tests. */
    private static GoExprNode constPtr(final GoPointer ptr) {
        return new GoExprNode(GoKind.POINTER) {
            @Override
            public Object executeObject(VirtualFrame frame) {
                return ptr;
            }
        };
    }

    public void testNewInt() throws Exception {
        VirtualFrame f = frame();
        Object o = buildNew(T_PTR_INT).executeObject(f);
        check(o instanceof GoPointer, "new(int) must return a GoPointer, got " + o);
        GoPointer p = (GoPointer) o;
        // Zero value readable before any write (this is what the bug broke:
        // the buggy node allocated a pointer-typed zero, i.e. null, and the
        // read below died instead of returning 0).
        check(new DerefNode(GoKind.INT64, constPtr(p)).executeLong(f) == 0L,
                "new(int) zero value must read as 0");
        new DerefWriteNode(constPtr(p), ConstNode.ofInt(GoKind.INT64, 42), GoKind.INT64)
                .executeVoid(f);
        check(new DerefNode(GoKind.INT64, constPtr(p)).executeLong(f) == 42L,
                "*p must read back 42 after *p = 42");
    }

    public void testNewNamedStruct() throws Exception {
        VirtualFrame f = frame();
        Object o = buildNew(T_PTR_STRUCT_NAMED).executeObject(f);
        check(o instanceof GoPointer, "new(S) must return a GoPointer, got " + o);
        GoPointer p = (GoPointer) o;
        check(p.referent() instanceof GoStruct,
                "new(S) referent must be a GoStruct, got " + p.referent());
        GoStruct s = (GoStruct) p.referent();
        check(s.descriptor().fields().size() == 1
                        && "X".equals(s.descriptor().fields().get(0).name()),
                "new(S) descriptor must carry field X, got " + s.descriptor().fields());
        check(new SelectorNode(constPtr(p), new int[]{0}, GoKind.INT64).executeLong(f) == 0L,
                "new(S).X zero value must read as 0");
        new SelectorWriteNode(constPtr(p), ConstNode.ofInt(GoKind.INT64, 7),
                new int[]{0}, GoKind.INT64).executeVoid(f);
        check(new SelectorNode(constPtr(p), new int[]{0}, GoKind.INT64).executeLong(f) == 7L,
                "new(S).X must read back 7 after assignment");
    }

    public void testNewAnonymousStruct() throws Exception {
        VirtualFrame f = frame();
        Object o = buildNew(T_PTR_STRUCT_ANON).executeObject(f);
        check(o instanceof GoPointer, "new(struct{X int}) must return a GoPointer");
        GoPointer p = (GoPointer) o;
        check(p.referent() instanceof GoStruct,
                "anonymous struct referent must be a GoStruct, got " + p.referent());
        check(new SelectorNode(constPtr(p), new int[]{0}, GoKind.INT64).executeLong(f) == 0L,
                "anonymous struct field zero value must read as 0");
    }

    public void testNewArray() throws Exception {
        VirtualFrame f = frame();
        Object o = buildNew(T_PTR_ARRAY3_INT).executeObject(f);
        check(o instanceof GoPointer, "new([3]int) must return a GoPointer, got " + o);
        Object ref = ((GoPointer) o).referent();
        check(ref instanceof long[], "new([3]int) referent must be long[3], got " + ref);
        long[] arr = (long[]) ref;
        check(arr.length == 3, "new([3]int) length must be 3, got " + arr.length);
        check(arr[0] == 0L && arr[1] == 0L && arr[2] == 0L,
                "new([3]int) elements must be zero");
    }

    public void testNewNestedArray() throws Exception {
        VirtualFrame f = frame();
        Object o = buildNew(T_PTR_ARRAY2_ARRAY3_INT).executeObject(f);
        check(o instanceof GoPointer, "new([2][3]int) must return a GoPointer");
        Object ref = ((GoPointer) o).referent();
        check(ref instanceof Object[], "new([2][3]int) referent must be Object[2], got " + ref);
        Object[] outer = (Object[]) ref;
        check(outer.length == 2, "outer length must be 2, got " + outer.length);
        check(outer[0] instanceof long[] && ((long[]) outer[0]).length == 3,
                "inner arrays must be long[3] (no null elements), got " + outer[0]);
        check(outer[1] instanceof long[] && ((long[]) outer[1]).length == 3,
                "inner arrays must be long[3] (no null elements), got " + outer[1]);
    }

    public void testNewPointer() throws Exception {
        VirtualFrame f = frame();
        Object o = buildNew(T_PTR_PTR_INT).executeObject(f);
        check(o instanceof GoPointer, "new(*int) must return a GoPointer, got " + o);
        GoPointer p = (GoPointer) o;
        check(p.referent() == null, "new(*int) zero value must be nil, got " + p.referent());
    }
}
