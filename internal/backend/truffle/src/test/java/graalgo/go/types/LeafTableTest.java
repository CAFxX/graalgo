package graalgo.go.types;

import graalgo.go.types.GoType.Field;
import graalgo.go.types.GoType.Leaf;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/**
 * The leaf tables of the flat value model (SPEC §3.4, GG-0072; ADR-0015 step 1) against gc's
 * own answers: test/m1w13/layout_corpus.txt is the output, under gc, of the program
 * {@code test/m1w13/gen.py layout} generates — per struct type of the corpus its
 * {@code unsafe.Sizeof}, {@code unsafe.Alignof} and the byte offset and kind of every leaf.
 * The test rebuilds each type from the fixture's description and checks size, alignment, the
 * forward table (byte offset → leaf, padding → none) and the inverse table (leaf → byte
 * offset) over every byte of every type.
 */
public final class LeafTableTest {
    private final TypeTable table = new TypeTable();
    private String src;
    private int pos;

    private static List<String> corpus() throws IOException {
        String repo = System.getProperty("graalgo.repo.dir");
        if (repo == null) {
            throw new AssertionError("graalgo.repo.dir is not set");
        }
        List<String> lines = Files.readAllLines(Path.of(repo, "test", "m1w13", "layout_corpus.txt"));
        if (lines.size() < 300) {
            throw new AssertionError("layout corpus has only " + lines.size() + " types");
        }
        return lines;
    }

    private GoType parse() {
        char c = src.charAt(pos);
        if (c == '[') {
            int close = src.indexOf(']', pos);
            long n = Long.parseLong(src.substring(pos + 1, close));
            pos = close + 1;
            return table.array(n, parse());
        } else if (c == '{') {
            pos++;
            List<Field> fields = new ArrayList<>();
            while (src.charAt(pos) != '}') {
                if (src.charAt(pos) == ' ') {
                    pos++;
                    continue;
                }
                fields.add(new Field("F" + fields.size(), "", parse(), false, ""));
            }
            pos++;
            return table.struct(fields.toArray(new Field[0]));
        }
        int end = pos;
        while (end < src.length() && (Character.isLetterOrDigit(src.charAt(end)))) {
            end++;
        }
        String tag = src.substring(pos, end);
        pos = end;
        return switch (tag) {
            case "b" -> table.basic(GoType.BOOL);
            case "i8" -> table.basic(GoType.INT8);
            case "u8" -> table.basic(GoType.UINT8);
            case "i16" -> table.basic(GoType.INT16);
            case "u16" -> table.basic(GoType.UINT16);
            case "i32" -> table.basic(GoType.INT32);
            case "u32" -> table.basic(GoType.UINT32);
            case "i64" -> table.basic(GoType.INT64);
            case "u64" -> table.basic(GoType.UINT64);
            case "f32" -> table.basic(GoType.FLOAT32);
            case "f64" -> table.basic(GoType.FLOAT64);
            case "c64" -> table.basic(GoType.COMPLEX64);
            case "c128" -> table.basic(GoType.COMPLEX128);
            case "str" -> table.basic(GoType.STRING);
            case "ptr" -> table.pointer(table.basic(GoType.INT64));
            case "sl" -> table.slice(table.basic(GoType.INT32));
            case "if" -> table.emptyInterface;
            default -> throw new AssertionError("unknown leaf tag " + tag);
        };
    }

    private static String kindOf(GoType leaf) {
        GoType u = leaf.u();
        if (u.kind == GoType.BOOL) {
            return "bool";
        } else if (u.isInteger()) {
            return u.isUnsigned() ? "uint" : "int";
        } else if (u.isFloat()) {
            return "float";
        } else if (u.isComplex()) {
            return "complex";
        }
        return switch (u.kind) {
            case GoType.STRING -> "string";
            case GoType.SLICE -> "slice";
            case GoType.INTERFACE -> "iface";
            default -> "pointer";
        };
    }

    private static void check(boolean ok, String type, String what) {
        if (!ok) {
            throw new AssertionError(type + ": " + what);
        }
    }

    /** Size, alignment and both directions of the leaf table, for every byte of every type. */
    public void testLeafTablesMatchGc() throws IOException {
        int leaves = 0;
        for (String line : corpus()) {
            int close = matching(line);
            String desc = line.substring(0, close + 1);
            String[] rest = line.substring(close + 1).trim().split(" ");
            src = desc;
            pos = 0;
            GoType t = parse();
            long size = Long.parseLong(rest[0]);
            long align = Long.parseLong(rest[1]);
            check(t.gcSize() == size, desc, "size " + t.gcSize() + ", gc " + size);
            check(t.gcAlign() == align, desc, "align " + t.gcAlign() + ", gc " + align);
            int n = rest.length - 2;
            check(t.nLeaves() == n, desc, "leaves " + t.nLeaves() + ", gc " + n);
            boolean[] covered = new boolean[(int) size];
            int prims = 0;
            int refs = 0;
            int bytes = 0;
            for (int i = 0; i < n; i++) {
                int colon = rest[2 + i].indexOf(':');
                long off = Long.parseLong(rest[2 + i].substring(0, colon));
                String kind = rest[2 + i].substring(colon + 1);
                check(t.gcOffOfLeaf(i) == off, desc, "leaf " + i + " at " + t.gcOffOfLeaf(i) + ", gc " + off);
                Leaf first = t.leafAt(off);
                check(first != null, desc, "no leaf at byte " + off);
                long lsize = first.size();
                for (long b = off; b < off + lsize; b++) {
                    Leaf l = t.leafAt(b);
                    check(l != null && l.index == i && l.gcOff == off, desc, "byte " + b + " is not in leaf " + i);
                    check(kindOf(l.type).equals(kind), desc, "leaf " + i + " is " + kindOf(l.type) + ", gc " + kind);
                    covered[(int) b] = true;
                }
                switch (first.region()) {
                    case GoType.R_PRIM -> check(first.slot() == prims++, desc, "leaf " + i + " scalar slot");
                    case GoType.R_REF -> check(first.slot() == refs++, desc, "leaf " + i + " reference slot");
                    default -> check(first.slot() == bytes++, desc, "leaf " + i + " byte slot");
                }
                leaves++;
            }
            check(prims == t.nPrim() && refs == t.nRef() && bytes == t.nByte(), desc, "slot counts");
            for (int b = 0; b < size; b++) {
                check(covered[b] || t.leafAt(b) == null, desc, "padding byte " + b + " maps to a leaf");
            }
            check(t.leafAt(size) == null && t.leafAt(-1) == null, desc, "a byte outside the value maps to a leaf");
            check(t.gcOffOfLeaf(n) == (n == 0 ? 0 : size), desc, "end position");
        }
        if (leaves < 1000) {
            throw new AssertionError("only " + leaves + " leaves checked");
        }
    }

    /** One-byte leaves live in the byte region (ADR-0015 decision 3); wider scalars do not. */
    public void testRegions() {
        check(table.basic(GoType.BOOL).leafAt(0).region() == GoType.R_BYTE, "bool", "region");
        check(table.basic(GoType.INT8).leafAt(0).region() == GoType.R_BYTE, "int8", "region");
        check(table.basic(GoType.UINT8).leafAt(0).region() == GoType.R_BYTE, "uint8", "region");
        check(table.basic(GoType.UINT16).leafAt(0).region() == GoType.R_PRIM, "uint16", "region");
        check(table.basic(GoType.FLOAT32).leafAt(0).region() == GoType.R_PRIM, "float32", "region");
        check(table.basic(GoType.STRING).leafAt(0).region() == GoType.R_REF, "string", "region");
        check(table.basic(GoType.COMPLEX64).leafAt(0).region() == GoType.R_REF, "complex64", "region");
    }

    private static int matching(String line) {
        int depth = 0;
        for (int i = 0; i < line.length(); i++) {
            char c = line.charAt(i);
            if (c == '{') {
                depth++;
            } else if (c == '}' && --depth == 0) {
                return i;
            }
        }
        throw new AssertionError("bad corpus line " + line);
    }
}
