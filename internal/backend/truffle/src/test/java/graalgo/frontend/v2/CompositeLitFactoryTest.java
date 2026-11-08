package graalgo.frontend.v2;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.Source;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoRootNode;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Factory execution tests for v2 composite literals and uint64 literals
 * (REVIEW-2026-11-06-12 V12-P0-1 + V12-P1-8).
 *
 * <p>Each test hand-builds a v2 document mirroring exactly what the real Go
 * emitter produces (verified against {@code internal/frontend/v2/emit}):
 * struct-literal keys are kind {@code "fieldname"} (emitIdent, for a
 * Uses-recorded field var), basic literals carry the canonical decimal
 * value (emitBasicLit / R-P0-2), and type-table entries carry the
 * describeType detail shapes. The document is encoded with the test-side
 * encoder below (a mirror of {@link BinaryDecoder}'s spec: magic + varint
 * version + tagged values, zigzag ints), built with {@link NodeFactoryV2},
 * executed on a manually created frame (the v1 corpus idiom), and the
 * returned value is asserted.
 *
 * <p>Every test fails loudly on the pre-fix factory: struct literals NPE
 * in buildStruct, array literals throw NegativeArraySizeException, slice
 * indexing and string-keyed map reads throw ClassCastException, and
 * uint64-max literals throw FrontendException "unsupported literal".
 */
public final class CompositeLitFactoryTest {

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void checkEq(long actual, long expected, String what) {
        check(actual == expected, what + ": expected " + expected + ", got " + actual);
    }

    // --- Minimal map builder (insertion-ordered, like the Go encoder) ---

    private static Map<String, Object> map(Object... kv) {
        Map<String, Object> m = new LinkedHashMap<>();
        for (int i = 0; i < kv.length; i += 2) {
            m.put((String) kv[i], kv[i + 1]);
        }
        return m;
    }

    // --- Type-table builder (ids are indices into Types.list) ---

    private static final class Types {
        final List<Object> list = new ArrayList<>();

        int add(Map<String, Object> t) {
            list.add(t);
            return list.size() - 1;
        }

        int basic(String name, String cls) {
            return add(map("kind", "basic",
                "detail", map("name", name, "class", cls, "untyped", Boolean.FALSE)));
        }

        int slice(int elem) {
            return add(map("kind", "slice", "detail", map("elem", (long) elem)));
        }

        int array(long len, int elem) {
            return add(map("kind", "array",
                "detail", map("len", len, "elem", (long) elem)));
        }

        int mapType(int key, int elem) {
            return add(map("kind", "map",
                "detail", map("key", (long) key, "elem", (long) elem)));
        }

        /** Field pairs alternate String name, Integer type id. */
        int structType(Object... nameAndType) {
            List<Object> fields = new ArrayList<>();
            for (int i = 0; i < nameAndType.length; i += 2) {
                fields.add(map("name", nameAndType[i], "type", (long) (Integer) nameAndType[i + 1],
                    "tag", "", "embedded", Boolean.FALSE, "offset", 0L));
            }
            return add(map("kind", "struct", "detail", map("fields", fields)));
        }

        int named(String name, int underlying) {
            return add(map("kind", "named",
                "detail", map("name", name, "underlying", (long) underlying,
                    "methods", List.of())));
        }

        int sig() {
            return add(map("kind", "signature",
                "detail", map("params", List.of(), "results", List.of(),
                    "variadic", Boolean.FALSE)));
        }
    }

    // --- Node builders (mirror internal/frontend/v2/emit/nodes.go) ---

    private static Map<String, Object> node(String kind, Map<String, Object> fields) {
        return map("kind", kind, "fields", fields);
    }

    private static Map<String, Object> basiclit(String value, int type) {
        return node("basiclit", map("value", value, "type", (long) type));
    }

    private static Map<String, Object> ident(int obj, String name, int type) {
        return node("ident", map("name", name, "obj", (long) obj, "type", (long) type));
    }

    private static Map<String, Object> fieldname(String name) {
        return node("fieldname", map("name", name));
    }

    private static Map<String, Object> compositelit(int type, List<Object> elems) {
        return node("compositelit", map("type", (long) type, "elems", elems));
    }

    private static Map<String, Object> keyvalue(Map<String, Object> key, Map<String, Object> value) {
        return map("kind", "keyvalue",
            "fields", map("key", key, "value", value, "type", null));
    }

    private static Map<String, Object> index(Map<String, Object> x, Map<String, Object> idx, int type) {
        return node("index", map("x", x, "index", idx, "type", (long) type));
    }

    private static Map<String, Object> selector(Map<String, Object> x, String sel, int type) {
        return node("selector", map("x", x, "sel", sel, "selkind", "field",
            "index", List.of(0L), "type", (long) type));
    }

    private static Map<String, Object> vardecl(String name, int obj, int type,
            Map<String, Object> value) {
        return node("vardecl", map("names", List.of(name), "objs", List.of((long) obj),
            "define", Boolean.FALSE, "type", (long) type, "values", List.of(value)));
    }

    private static Map<String, Object> block(List<Object> stmts) {
        return node("block", map("stmts", stmts));
    }

    private static Map<String, Object> ret(Map<String, Object> expr) {
        return node("return", map("results", List.of(expr)));
    }

    private static Map<String, Object> funcdecl(String name, int sig, Map<String, Object> body) {
        return node("funcdecl", map("name", name, "sig", (long) sig, "receiver", null,
            "params", List.of(), "body", body, "pkgpath", "main"));
    }

    private static Map<String, Object> object(String name, int type) {
        return map("name", name, "type", (long) type);
    }

    private static Map<String, Object> doc(Types types, List<Object> objects,
            Map<String, Object> fn) {
        return map("frontend", "v2",
            "package", map("name", "main", "files", List.of(), "lang", "go"),
            "types", types.list, "objects", objects,
            "funcs", List.of(fn), "globals", List.of());
    }

    // --- Test-side binary encoder (mirror of BinaryDecoder's spec) ---

    private static byte[] encodeDocument(Object doc) throws IOException {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        out.write(new byte[]{'g', '2', 'b', 'i'});
        writeVarint(out, 1); // format version
        writeValue(out, doc);
        return out.toByteArray();
    }

    private static void writeValue(ByteArrayOutputStream out, Object v) throws IOException {
        if (v == null) {
            out.write(0x00);
        } else if (v instanceof Boolean b) {
            out.write(0x01);
            out.write(b ? 1 : 0);
        } else if (v instanceof Long l) {
            out.write(0x02);
            writeVarint(out, zigzag(l));
        } else if (v instanceof Integer i) {
            out.write(0x02);
            writeVarint(out, zigzag(i.longValue()));
        } else if (v instanceof String s) {
            byte[] bytes = s.getBytes(StandardCharsets.UTF_8);
            out.write(0x03);
            writeVarint(out, bytes.length);
            out.write(bytes);
        } else if (v instanceof List<?> list) {
            out.write(0x04);
            writeVarint(out, list.size());
            for (Object e : list) {
                writeValue(out, e);
            }
        } else if (v instanceof Map<?, ?> m) {
            out.write(0x05);
            writeVarint(out, m.size());
            for (Map.Entry<?, ?> e : m.entrySet()) {
                byte[] k = ((String) e.getKey()).getBytes(StandardCharsets.UTF_8);
                writeVarint(out, k.length);
                out.write(k);
                writeValue(out, e.getValue());
            }
        } else {
            throw new IllegalArgumentException("cannot encode value: " + v);
        }
    }

    private static long zigzag(long x) {
        return (x << 1) ^ (x >> 63);
    }

    private static void writeVarint(ByteArrayOutputStream out, long v) {
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

    // --- Execution (v1 corpus idiom: build, wrap in GoRootNode, run) ---

    private static Object runMain(Map<String, Object> doc) throws Exception {
        byte[] bytes = encodeDocument(doc);
        Source source = Source.newBuilder("go", "package main\nfunc main() {}", "main.go").build();
        NodeFactoryV2.V2Program program = new NodeFactoryV2(source).buildProgram(bytes);
        check(program.functionCount() == 1, "one function, got " + program.functionCount());
        NodeFactoryV2.V2Function fn = program.functions().get(0);
        check("main".equals(fn.name()), "main, got " + fn.name());
        check(fn.hasBody(), "main has a body");
        VirtualFrame frame = Truffle.getRuntime()
                .createVirtualFrame(new Object[0], fn.frameDescriptor());
        return new GoRootNode(fn.frameDescriptor(), fn.body()).execute(frame);
    }

    private static long runLong(Map<String, Object> doc) throws Exception {
        Object v = runMain(doc);
        check(v instanceof Long, "expected Long return, got " + v);
        return (Long) v;
    }

    // --- V12-P0-1: struct literals ---

    public void testStructLiteralKeyedReturnsField() throws Exception {
        Types t = new Types();
        int intId = t.basic("int", "int");
        int structId = t.structType("X", intId, "Y", intId);
        int pointId = t.named("main.Point", structId);
        int sigId = t.sig();
        Map<String, Object> lit = compositelit(pointId, List.of(
            keyvalue(fieldname("X"), basiclit("1", intId)),
            keyvalue(fieldname("Y"), basiclit("2", intId))));
        Map<String, Object> body = block(List.of(
            vardecl("p", 0, pointId, lit),
            ret(selector(ident(0, "p", pointId), "X", intId))));
        Map<String, Object> d = doc(t, List.of(object("p", pointId)),
            funcdecl("main", sigId, body));
        checkEq(runLong(d), 1L, "p.X");
    }

    public void testStructLiteralUnkeyedReturnsField() throws Exception {
        Types t = new Types();
        int intId = t.basic("int", "int");
        int structId = t.structType("X", intId, "Y", intId);
        int pointId = t.named("main.Point", structId);
        int sigId = t.sig();
        Map<String, Object> lit = compositelit(pointId, List.of(
            basiclit("3", intId), basiclit("4", intId)));
        Map<String, Object> body = block(List.of(
            vardecl("p", 0, pointId, lit),
            ret(selector(ident(0, "p", pointId), "Y", intId))));
        Map<String, Object> d = doc(t, List.of(object("p", pointId)),
            funcdecl("main", sigId, body));
        checkEq(runLong(d), 4L, "p.Y");
    }

    public void testStructLiteralMissingFieldIsZero() throws Exception {
        Types t = new Types();
        int intId = t.basic("int", "int");
        int structId = t.structType("X", intId, "Y", intId);
        int pointId = t.named("main.Point", structId);
        int sigId = t.sig();
        Map<String, Object> lit = compositelit(pointId, List.of(
            keyvalue(fieldname("X"), basiclit("5", intId))));
        Map<String, Object> body = block(List.of(
            vardecl("p", 0, pointId, lit),
            ret(selector(ident(0, "p", pointId), "Y", intId))));
        Map<String, Object> d = doc(t, List.of(object("p", pointId)),
            funcdecl("main", sigId, body));
        checkEq(runLong(d), 0L, "p.Y (missing field is zero)");
    }

    // --- V12-P0-1: array literals ---

    public void testArrayLiteralReturnsElement() throws Exception {
        Types t = new Types();
        int intId = t.basic("int", "int");
        int arrId = t.array(3, intId);
        int sigId = t.sig();
        Map<String, Object> lit = compositelit(arrId, List.of(
            basiclit("10", intId), basiclit("20", intId), basiclit("30", intId)));
        Map<String, Object> body = block(List.of(
            vardecl("a", 0, arrId, lit),
            ret(index(ident(0, "a", arrId), basiclit("2", intId), intId))));
        Map<String, Object> d = doc(t, List.of(object("a", arrId)),
            funcdecl("main", sigId, body));
        checkEq(runLong(d), 30L, "a[2]");
    }

    public void testArrayLiteralKeyedMissingElementIsZero() throws Exception {
        Types t = new Types();
        int intId = t.basic("int", "int");
        int arrId = t.array(3, intId);
        int sigId = t.sig();
        Map<String, Object> lit = compositelit(arrId, List.of(
            keyvalue(basiclit("2", intId), basiclit("7", intId))));
        Map<String, Object> body = block(List.of(
            vardecl("a", 0, arrId, lit),
            ret(index(ident(0, "a", arrId), basiclit("0", intId), intId))));
        Map<String, Object> d = doc(t, List.of(object("a", arrId)),
            funcdecl("main", sigId, body));
        checkEq(runLong(d), 0L, "a[0] (missing element is zero)");
    }

    // --- V12-P0-1: slice literals + index reads ---

    public void testSliceLiteralIndexFirst() throws Exception {
        Types t = new Types();
        int intId = t.basic("int", "int");
        int sliceId = t.slice(intId);
        int sigId = t.sig();
        Map<String, Object> lit = compositelit(sliceId, List.of(
            basiclit("1", intId), basiclit("2", intId), basiclit("3", intId)));
        Map<String, Object> body = block(List.of(
            vardecl("s", 0, sliceId, lit),
            ret(index(ident(0, "s", sliceId), basiclit("0", intId), intId))));
        Map<String, Object> d = doc(t, List.of(object("s", sliceId)),
            funcdecl("main", sigId, body));
        checkEq(runLong(d), 1L, "s[0]");
    }

    public void testSliceLiteralIndexLast() throws Exception {
        Types t = new Types();
        int intId = t.basic("int", "int");
        int sliceId = t.slice(intId);
        int sigId = t.sig();
        Map<String, Object> lit = compositelit(sliceId, List.of(
            basiclit("1", intId), basiclit("2", intId), basiclit("3", intId)));
        Map<String, Object> body = block(List.of(
            vardecl("s", 0, sliceId, lit),
            ret(index(ident(0, "s", sliceId), basiclit("2", intId), intId))));
        Map<String, Object> d = doc(t, List.of(object("s", sliceId)),
            funcdecl("main", sigId, body));
        checkEq(runLong(d), 3L, "s[2]");
    }

    // --- V12-P0-1: map literals ---

    public void testStringKeyedMapLiteralReturnsValue() throws Exception {
        Types t = new Types();
        int intId = t.basic("int", "int");
        int strId = t.basic("string", "string");
        int mapId = t.mapType(strId, intId);
        int sigId = t.sig();
        Map<String, Object> lit = compositelit(mapId, List.of(
            keyvalue(basiclit("a", strId), basiclit("1", intId)),
            keyvalue(basiclit("b", strId), basiclit("2", intId))));
        Map<String, Object> body = block(List.of(
            vardecl("m", 0, mapId, lit),
            ret(index(ident(0, "m", mapId), basiclit("a", strId), intId))));
        Map<String, Object> d = doc(t, List.of(object("m", mapId)),
            funcdecl("main", sigId, body));
        checkEq(runLong(d), 1L, "m[\"a\"]");
    }

    public void testIntKeyedMapLiteralReturnsValue() throws Exception {
        Types t = new Types();
        int intId = t.basic("int", "int");
        int mapId = t.mapType(intId, intId);
        int sigId = t.sig();
        Map<String, Object> lit = compositelit(mapId, List.of(
            keyvalue(basiclit("1", intId), basiclit("42", intId))));
        Map<String, Object> body = block(List.of(
            vardecl("m", 0, mapId, lit),
            ret(index(ident(0, "m", mapId), basiclit("1", intId), intId))));
        Map<String, Object> d = doc(t, List.of(object("m", mapId)),
            funcdecl("main", sigId, body));
        checkEq(runLong(d), 42L, "m[1]");
    }

    // --- V12-P1-8: uint64-range literals ---

    public void testUint64MaxDecodes() throws Exception {
        Types t = new Types();
        int uint64Id = t.basic("uint64", "uint");
        int sigId = t.sig();
        Map<String, Object> body = block(List.of(
            vardecl("u", 0, uint64Id, basiclit("18446744073709551615", uint64Id)),
            ret(ident(0, "u", uint64Id))));
        Map<String, Object> d = doc(t, List.of(object("u", uint64Id)),
            funcdecl("main", sigId, body));
        Object v = runMain(d);
        check(v instanceof Long, "expected Long return, got " + v);
        check("18446744073709551615".equals(Long.toUnsignedString((Long) v)),
            "uint64 max: got " + Long.toUnsignedString((Long) v));
    }

    public void testUintptrMaxDecodes() throws Exception {
        Types t = new Types();
        int uintptrId = t.basic("uintptr", "uint");
        int sigId = t.sig();
        Map<String, Object> body = block(List.of(
            vardecl("w", 0, uintptrId, basiclit("18446744073709551615", uintptrId)),
            ret(ident(0, "w", uintptrId))));
        Map<String, Object> d = doc(t, List.of(object("w", uintptrId)),
            funcdecl("main", sigId, body));
        Object v = runMain(d);
        check(v instanceof Long, "expected Long return, got " + v);
        check("18446744073709551615".equals(Long.toUnsignedString((Long) v)),
            "uintptr max: got " + Long.toUnsignedString((Long) v));
    }

    public void testUint64HexMaxDecodes() throws Exception {
        Types t = new Types();
        int uint64Id = t.basic("uint64", "uint");
        int sigId = t.sig();
        // Hand-built document value (the real emitter canonicalizes hex to
        // decimal first, so this pins the factory's own hex branch).
        Map<String, Object> body = block(List.of(
            vardecl("h", 0, uint64Id, basiclit("0xFFFFFFFFFFFFFFFF", uint64Id)),
            ret(ident(0, "h", uint64Id))));
        Map<String, Object> d = doc(t, List.of(object("h", uint64Id)),
            funcdecl("main", sigId, body));
        Object v = runMain(d);
        check(v instanceof Long, "expected Long return, got " + v);
        check("18446744073709551615".equals(Long.toUnsignedString((Long) v)),
            "uint64 hex max: got " + Long.toUnsignedString((Long) v));
    }

    public void testUint64OverflowFailsLoudly() {
        Types t = new Types();
        int uint64Id = t.basic("uint64", "uint");
        int sigId = t.sig();
        Map<String, Object> body = block(List.of(
            vardecl("u", 0, uint64Id, basiclit("18446744073709551616", uint64Id)),
            ret(ident(0, "u", uint64Id))));
        Map<String, Object> d = doc(t, List.of(object("u", uint64Id)),
            funcdecl("main", sigId, body));
        try {
            runMain(d);
        } catch (FrontendException e) {
            check(e.getMessage().contains("unsupported literal"),
                "overflow must fail as unsupported literal, got: " + e.getMessage());
            return;
        } catch (Exception e) {
            throw new AssertionError("overflow must fail as FrontendException, got: " + e);
        }
        throw new AssertionError("uint64 2^64 did not fail");
    }

    public void testInt64MaxStillDecodes() throws Exception {
        Types t = new Types();
        int int64Id = t.basic("int64", "int");
        int sigId = t.sig();
        Map<String, Object> body = block(List.of(
            vardecl("i", 0, int64Id, basiclit("9223372036854775807", int64Id)),
            ret(ident(0, "i", int64Id))));
        Map<String, Object> d = doc(t, List.of(object("i", int64Id)),
            funcdecl("main", sigId, body));
        checkEq(runLong(d), Long.MAX_VALUE, "int64 max");
    }
}
