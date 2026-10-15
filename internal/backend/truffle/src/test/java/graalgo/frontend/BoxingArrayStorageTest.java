package graalgo.frontend;

import graalgo.values.GoKind;

/**
 * Regression test for his 2026-10-15 int-storage verdict: Go int kinds
 * narrower than 32 bits must not be backed by Java {@code int[]} — that
 * costs 4x memory for {@code []int8} and 2x for {@code []int16}.
 *
 * <p>RED (pre-fix): {@code testBackingStoreMapping} fails because the old
 * {@code Boxing.newArray} mapped {@code INT8, INT16, INT32, UINT8, UINT16,
 * UINT32} all to {@code new int[len]}; {@code testByteSliceStringRoundtrip}
 * fails with {@code ClassCastException [I→[B} because {@code stringToSlice}
 * produced {@code int[]} while the readers cast to {@code byte[]}.
 * GREEN: narrow kinds get {@code byte[]}/{@code short[]} backing and the
 * roundtrip is byte-identical to stock gc.
 */
public final class BoxingArrayStorageTest {

    /**
     * The kind→backing-store mapping itself. Cheap, deterministic, no
     * backend boot needed. This is the assertion the 15-minute bodge scan's
     * pattern 7 greps for; the committed test pins it.
     */
    public void testBackingStoreMapping() {
        assertBacking(GoKind.INT8, byte[].class);
        assertBacking(GoKind.UINT8, byte[].class);
        assertBacking(GoKind.INT16, short[].class);
        assertBacking(GoKind.UINT16, short[].class);
        assertBacking(GoKind.INT32, int[].class);
        assertBacking(GoKind.UINT32, int[].class);
        assertBacking(GoKind.INT64, long[].class);
        assertBacking(GoKind.UINT64, long[].class);
        assertBacking(GoKind.UNTYPED_INT, long[].class);
        assertBacking(GoKind.FLOAT32, float[].class);
        assertBacking(GoKind.FLOAT64, double[].class);
        assertBacking(GoKind.BOOL, boolean[].class);
    }

    private static void assertBacking(GoKind kind, Class<?> expected) {
        Object arr = Boxing.newArray(kind, 4);
        if (!arr.getClass().equals(expected)) {
            throw new AssertionError("Boxing.newArray(" + kind + ") = "
                    + arr.getClass().getSimpleName() + ", expected "
                    + expected.getSimpleName());
        }
        if (java.lang.reflect.Array.getLength(arr) != 4) {
            throw new AssertionError("Boxing.newArray(" + kind
                    + ") wrong length");
        }
    }

    /**
     * Narrow backing stores must preserve Go value semantics: sign extension
     * on load for signed kinds, zero extension for unsigned, truncation
     * (wrapping) on store.
     */
    public void testNarrowRoundTrip() {
        // int8: -128..127, wrap on store
        Object i8 = Boxing.newArray(GoKind.INT8, 3);
        Boxing.arraySet(i8, GoKind.INT8, 0, Integer.valueOf(-1));
        Boxing.arraySet(i8, GoKind.INT8, 1, Integer.valueOf(127));
        Boxing.arraySet(i8, GoKind.INT8, 2, Integer.valueOf(128)); // wraps to -128
        assertBoxed(-1, Boxing.arrayGet(i8, GoKind.INT8, 0), "int8(-1)");
        assertBoxed(127, Boxing.arrayGet(i8, GoKind.INT8, 1), "int8(127)");
        assertBoxed(-128, Boxing.arrayGet(i8, GoKind.INT8, 2), "int8(128 wraps)");

        // uint8: 0..255, zero-extended on load
        Object u8 = Boxing.newArray(GoKind.UINT8, 2);
        Boxing.arraySet(u8, GoKind.UINT8, 0, Integer.valueOf(255));
        Boxing.arraySet(u8, GoKind.UINT8, 1, Integer.valueOf(-1)); // wraps to 255
        assertBoxed(255, Boxing.arrayGet(u8, GoKind.UINT8, 0), "uint8(255)");
        assertBoxed(255, Boxing.arrayGet(u8, GoKind.UINT8, 1), "uint8(-1 wraps)");

        // int16 / uint16
        Object i16 = Boxing.newArray(GoKind.INT16, 2);
        Boxing.arraySet(i16, GoKind.INT16, 0, Integer.valueOf(-32768));
        Boxing.arraySet(i16, GoKind.INT16, 1, Integer.valueOf(32767));
        assertBoxed(-32768, Boxing.arrayGet(i16, GoKind.INT16, 0), "int16(min)");
        assertBoxed(32767, Boxing.arrayGet(i16, GoKind.INT16, 1), "int16(max)");

        Object u16 = Boxing.newArray(GoKind.UINT16, 1);
        Boxing.arraySet(u16, GoKind.UINT16, 0, Integer.valueOf(65535));
        assertBoxed(65535, Boxing.arrayGet(u16, GoKind.UINT16, 0), "uint16(max)");
    }

    private static void assertBoxed(int expected, Object actual, String what) {
        if (!(actual instanceof Integer) || ((Integer) actual).intValue() != expected) {
            throw new AssertionError(what + ": got " + actual
                    + ", expected " + expected);
        }
    }

    /**
     * Go-level {@code []byte(s)} / {@code string([]byte)} roundtrip,
     * differential vs stock gc. The pre-fix backend threw
     * {@code ClassCastException [I→[B} here.
     */
    public void testByteSliceStringRoundtrip() throws Exception {
        String prog = "package main\n"
                + "\n"
                + "func main() {\n"
                + "\ts := \"hello\"\n"
                + "\tb := []byte(s)\n"
                + "\tback := string(b)\n"
                + "\tvar sum int = 0\n"
                + "\tvar sumBack int = 0\n"
                + "\tfor i := 0; i < len(b); i++ {\n"
                + "\t\tsum += int(b[i])\n"
                + "\t}\n"
                + "\tfor i := 0; i < len(back); i++ {\n"
                + "\t\tsumBack += int(back[i])\n"
                + "\t}\n"
                + "\tdiff := len(b) - len(back)\n"
                + "\t_ = diff\n"
                + "\t// observe: sum sumBack diff\n"
                + "}\n";
        java.nio.file.Path dir =
                java.nio.file.Files.createTempDirectory("int8-roundtrip");
        java.nio.file.Path src = dir.resolve("rt.go");
        java.nio.file.Files.writeString(src, prog, java.nio.charset.StandardCharsets.UTF_8);
        java.util.List<String> vars = ControlFlowCorpusTest.observeVars(src);
        long[] expected = ControlFlowCorpusTest.gcOracle(src, vars);
        long[] actual = ControlFlowCorpusTest.graalgoValues(src, vars);
        // sum == sumBack == 'h'+'e'+'l'+'l'+'o' == 532, diff == 0
        if (expected[0] != 532 || expected[1] != 532 || expected[2] != 0) {
            throw new AssertionError("gc oracle unexpected: sum=" + expected[0]
                    + " sumBack=" + expected[1] + " diff=" + expected[2]);
        }
        for (int i = 0; i < vars.size(); i++) {
            if (expected[i] != actual[i]) {
                throw new AssertionError("roundtrip." + vars.get(i) + ": gc="
                        + expected[i] + " graalgo=" + actual[i]);
            }
        }
    }
}
