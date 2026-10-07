package graalgo.runtime;

import graalgo.values.GoComplex32;
// verifies: GG-0020 (WS G: fmt verb-conformance matrix differential vs gc)
import graalgo.values.GoComplex64;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoString;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/**
 * The fmt verb-conformance matrix, differential vs stock gc (WS G, D4;
 * verifies GG-0020).
 *
 * <p>Each row of {@code corpus/fmt/matrix.tsv} is {@code format<TAB>arg-DSL
 * <TAB>gc-output}, where the gc-output column was produced by stock gc
 * 1.27.1 (see {@code corpus/fmt/README.md} for the oracle command) and is
 * escaped ({@code \\}, {@code \t}, {@code \n}). A trailing {@code |} in a
 * format is a sentinel making trailing spaces visible; it is part of both
 * the format and the expectation.
 *
 * <p>Red-green-first (SPEC §5.2): the matrix was built by running the gc
 * oracle first and the test fails on any divergence. Address-bearing rows
 * ({@code %p}, {@code %v} of pointers) are excluded from the matrix by
 * construction — gc addresses are nondeterministic; graalgo erases them to
 * {@code 0x0} by documented design (see {@code testAddressErasure}).
 */
public final class FmtVerbMatrixTest {
    private static final Path MATRIX = Path.of("corpus", "fmt", "matrix.tsv");

    public void testVerbMatrix() throws Exception {
        List<String> lines = Files.readAllLines(MATRIX, StandardCharsets.UTF_8);
        int rows = 0;
        List<String> failures = new ArrayList<>();
        for (String line : lines) {
            if (line.isBlank() || line.startsWith("#")) {
                continue;
            }
            rows++;
            String[] cols = line.split("\t", -1);
            if (cols.length != 3) {
                throw new AssertionError("matrix row " + rows + " has "
                    + cols.length + " columns (want 3): " + line);
            }
            String format = unescape(cols[0]);
            Fmt.Arg[] args = parseArgs(unescape(cols[1]));
            String want = unescape(cols[2]);
            String got;
            try {
                got = Fmt.sprintf(format, args).toString();
            } catch (Exception e) {
                failures.add("row " + rows + " fmt=" + format + " threw " + e);
                continue;
            }
            if (!want.equals(got)) {
                failures.add("row " + rows + " fmt=" + quote(format)
                    + " args=" + quote(cols[1]) + "\n  want=" + quote(want)
                    + "\n  got =" + quote(got));
            }
        }
        if (rows < 100) {
            throw new AssertionError("matrix shrank to " + rows + " rows");
        }
        if (!failures.isEmpty()) {
            throw new AssertionError(failures.size() + " matrix divergences:\n"
                + String.join("\n", failures.subList(0, Math.min(10, failures.size()))));
        }
        System.out.println("  (fmt matrix: " + rows + " rows, all match gc)");
    }

    /** Address erasure: nondeterministic under gc, pinned to 0x0 by design. */
    public void testAddressErasure() {
        Fmt.Arg p = new Fmt.Arg(new GoPointer(42L), GoKind.POINTER, "*int");
        assertEq("0x0", Fmt.sprintf("%p", new Fmt.Arg[]{p}).toString(), "%p erasure");
        assertEq("0x0", Fmt.sprintf("%v", new Fmt.Arg[]{p}).toString(), "%v erasure");
        // Error values keep their method dispatch (not addresses).
        Fmt.Arg e = errArg("oops");
        assertEq("oops", Fmt.sprintf("%v", new Fmt.Arg[]{e}).toString(), "error %v");
        assertEq("0x0", Fmt.sprintf("%p", new Fmt.Arg[]{e}).toString(), "error %p");
    }

    // -- DSL parsing -------------------------------------------------------

    private static Fmt.Arg[] parseArgs(String dsl) {
        if (dsl.isEmpty()) {
            return new Fmt.Arg[0];
        }
        String[] parts = dsl.split(";", -1);
        Fmt.Arg[] args = new Fmt.Arg[parts.length];
        for (int i = 0; i < parts.length; i++) {
            args[i] = parseArg(parts[i]);
        }
        return args;
    }

    private static Fmt.Arg parseArg(String spec) {
        if (spec.equals("nil")) {
            return new Fmt.Arg(null, GoKind.INTERFACE, "");
        }
        int colon = spec.indexOf(':');
        if (colon < 0) {
            throw new AssertionError("bad arg spec: " + spec);
        }
        String kind = spec.substring(0, colon);
        // The DSL value was escaped once for the DSL and once for TSV;
        // unescape twice (identity for values without backslashes).
        String val = unescape(unescape(spec.substring(colon + 1)));
        return switch (kind) {
            case "bool" -> new Fmt.Arg(Boolean.parseBoolean(val), GoKind.BOOL, "bool");
            case "int" -> new Fmt.Arg(Long.parseLong(val), GoKind.INT64, "int");
            case "int8" -> new Fmt.Arg(Byte.parseByte(val), GoKind.INT8, "int8");
            case "int16" -> new Fmt.Arg(Short.parseShort(val), GoKind.INT16, "int16");
            case "int32" -> new Fmt.Arg(Integer.parseInt(val), GoKind.INT32, "int32");
            case "int64" -> new Fmt.Arg(Long.parseLong(val), GoKind.INT64, "int64");
            case "uint" -> new Fmt.Arg(Long.parseUnsignedLong(val), GoKind.UINT64, "uint");
            case "uint8" ->
                new Fmt.Arg((byte) Integer.parseInt(val), GoKind.UINT8, "uint8");
            case "uint16" ->
                new Fmt.Arg((short) Integer.parseInt(val), GoKind.UINT16, "uint16");
            case "uint32" ->
                new Fmt.Arg(Integer.parseUnsignedInt(val), GoKind.UINT32, "uint32");
            case "uint64" ->
                new Fmt.Arg(Long.parseUnsignedLong(val), GoKind.UINT64, "uint64");
            case "float32" -> new Fmt.Arg(parseFloat32(val), GoKind.FLOAT32, "float32");
            case "float64" -> new Fmt.Arg(parseFloat64(val), GoKind.FLOAT64, "float64");
            case "complex64" -> {
                double[] rc = parseComplex(val);
                yield new Fmt.Arg(new GoComplex32((float) rc[0], (float) rc[1]),
                    GoKind.COMPLEX64, "complex64");
            }
            case "complex128" -> {
                double[] rc = parseComplex(val);
                yield new Fmt.Arg(new GoComplex64(rc[0], rc[1]),
                    GoKind.COMPLEX128, "complex128");
            }
            case "string" ->
                new Fmt.Arg(GoString.fromUtf8(val), GoKind.STRING, "string");
            case "bytes" -> {
                int[] arr = new int[val.length() / 2];
                for (int i = 0; i < arr.length; i++) {
                    arr[i] = Integer.parseInt(val.substring(2 * i, 2 * i + 2), 16);
                }
                // []byte backs onto int[] (Fmt.boxArrayElement/sliceBytes).
                // The type name is the alias "[]byte" (what gc prints).
                yield new Fmt.Arg(new GoSlice(arr, 0, arr.length, arr.length),
                    GoKind.SLICE, "[]byte", null, null, GoKind.UINT8, "uint8");
            }
            case "ints" -> {
                String[] ns = val.split(",", -1);
                int[] arr = new int[ns.length];
                for (int i = 0; i < ns.length; i++) {
                    arr[i] = Integer.parseInt(ns[i]);
                }
                yield new Fmt.Arg(new GoSlice(arr, 0, arr.length, arr.length),
                    GoKind.SLICE, "[]int", null, null, GoKind.INT32, "int");
            }
            case "mapsi" -> {
                GoMap m = new GoMap();
                for (String kv : val.split(",", -1)) {
                    String[] kvs = kv.split(":", 2);
                    m.put(GoString.fromUtf8(kvs[0]), Integer.parseInt(kvs[1]));
                }
                yield new Fmt.Arg(m, GoKind.MAP, "map[string]int",
                    GoKind.STRING, "string", GoKind.INT32, "int");
            }
            case "error" -> errArg(val);
            case "ptrint" -> new Fmt.Arg(new GoPointer(Long.parseLong(val)),
                GoKind.POINTER, "*int");
            default -> throw new AssertionError("unknown arg kind: " + kind);
        };
    }

    private static Fmt.Arg errArg(String msg) {
        // Must use Fmt's canonical construction: the *errors.errorString
        // representation is unique (GoPointer -> struct{ s string }); a
        // hand-built shape here would drift from the real one.
        GoInterface gi = Fmt.errorStringInterface(GoString.fromUtf8(msg));
        return new Fmt.Arg(gi, GoKind.INTERFACE, "error");
    }

    private static double parseFloat64(String v) {
        return switch (v) {
            case "Inf" -> Double.POSITIVE_INFINITY;
            case "-Inf" -> Double.NEGATIVE_INFINITY;
            case "NaN" -> Double.NaN;
            default -> Double.parseDouble(v);
        };
    }

    private static float parseFloat32(String v) {
        return switch (v) {
            case "Inf" -> Float.POSITIVE_INFINITY;
            case "-Inf" -> Float.NEGATIVE_INFINITY;
            case "NaN" -> Float.NaN;
            default -> Float.parseFloat(v);
        };
    }

    /** Parses "1+2i" / "1-2i" into {re, im}. */
    private static double[] parseComplex(String v) {
        String t = v.endsWith("i") ? v.substring(0, v.length() - 1) : v;
        int split = Math.max(t.lastIndexOf('+'), t.lastIndexOf('-'));
        if (split <= 0) {
            throw new AssertionError("bad complex spec: " + v);
        }
        return new double[]{
            Double.parseDouble(t.substring(0, split)),
            Double.parseDouble(t.substring(split))};
    }

    private static String unescape(String s) {
        StringBuilder sb = new StringBuilder(s.length());
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            if (c == '\\' && i + 1 < s.length()) {
                char e = s.charAt(++i);
                switch (e) {
                    case 'n' -> sb.append('\n');
                    case 't' -> sb.append('\t');
                    case '\\' -> sb.append('\\');
                    default -> throw new AssertionError("bad escape \\" + e);
                }
            } else {
                sb.append(c);
            }
        }
        return sb.toString();
    }

    private static String quote(String s) {
        return "\"" + s.replace("\\", "\\\\").replace("\n", "\\n") + "\"";
    }

    private static void assertEq(String want, String got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": want " + quote(want)
                + " got " + quote(got));
        }
    }
}
