package graalgo.values;

import java.util.Arrays;
import java.util.List;
import java.util.Map;

/**
 * Unit tests for the D5 value model (GG-0015).
 *
 * <p>Float-conversion expectations are the stock-gc/amd64 values measured
 * in the M3 probe battery (the spec leaves out-of-range float→int
 * implementation-defined; we mirror gc).
 */
// verifies: GG-0015 (M3: value-model classes implement the D5 mapping;
// every conversion expectation below is a gc-measured value).
public final class ValuesTest {

    private static void assertEq(long want, long got, String what) {
        if (want != got) {
            throw new AssertionError(what + ": want " + want + " got " + got);
        }
    }

    private static void assertEq(int want, int got, String what) {
        if (want != got) {
            throw new AssertionError(what + ": want " + want + " got " + got);
        }
    }

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what + ": expected true");
        }
    }

    private static void assertBytes(byte[] want, byte[] got, String what) {
        if (!Arrays.equals(want, got)) {
            throw new AssertionError(what + ": want " + Arrays.toString(want)
                    + " got " + Arrays.toString(got));
        }
    }

    public void testGoKindFromBasic() {
        assertTrue(GoKind.fromBasic("int8", "int") == GoKind.INT8, "int8");
        assertTrue(GoKind.fromBasic("int16", "int") == GoKind.INT16, "int16");
        assertTrue(GoKind.fromBasic("int32", "int") == GoKind.INT32, "int32");
        assertTrue(GoKind.fromBasic("int64", "int") == GoKind.INT64, "int64");
        assertTrue(GoKind.fromBasic("int", "int") == GoKind.INT64, "int");
        assertTrue(GoKind.fromBasic("uint8", "uint") == GoKind.UINT8, "uint8");
        assertTrue(GoKind.fromBasic("uint16", "uint") == GoKind.UINT16, "uint16");
        assertTrue(GoKind.fromBasic("uint32", "uint") == GoKind.UINT32, "uint32");
        assertTrue(GoKind.fromBasic("uint64", "uint") == GoKind.UINT64, "uint64");
        assertTrue(GoKind.fromBasic("uint", "uint") == GoKind.UINT64, "uint");
        assertTrue(GoKind.fromBasic("uintptr", "uint") == GoKind.UINT64, "uintptr");
        assertTrue(GoKind.fromBasic("float32", "float") == GoKind.FLOAT32, "float32");
        assertTrue(GoKind.fromBasic("float64", "float") == GoKind.FLOAT64, "float64");
        assertTrue(GoKind.fromBasic("bool", "bool") == GoKind.BOOL, "bool");
        assertTrue(GoKind.fromBasic("string", "string") == GoKind.STRING, "string");
        assertTrue(GoKind.fromBasic("untyped int", "int") == GoKind.UNTYPED_INT, "untyped int");
        assertTrue(GoKind.fromBasic("untyped rune", "int") == GoKind.UNTYPED_INT, "untyped rune");
        assertTrue(GoKind.fromBasic("untyped float", "float") == GoKind.UNTYPED_FLOAT, "untyped float");
        assertTrue(GoKind.fromBasic("untyped bool", "bool") == GoKind.UNTYPED_BOOL, "untyped bool");
        assertTrue(GoKind.fromBasic("untyped string", "string") == GoKind.UNTYPED_STRING, "untyped string");
        assertTrue(GoKind.fromBasic("unsafe.Pointer", "pointer") == GoKind.UNSAFE_POINTER, "unsafe.Pointer");
        // Slot kinds (D5).
        assertTrue(GoKind.INT32.slotKind() == com.oracle.truffle.api.frame.FrameSlotKind.Int, "int32 slot");
        assertTrue(GoKind.INT64.slotKind() == com.oracle.truffle.api.frame.FrameSlotKind.Long, "int64 slot");
        assertTrue(GoKind.UINT64.slotKind() == com.oracle.truffle.api.frame.FrameSlotKind.Long, "uint64 slot");
        assertTrue(GoKind.FLOAT32.slotKind() == com.oracle.truffle.api.frame.FrameSlotKind.Float, "float32 slot");
        assertTrue(GoKind.FLOAT64.slotKind() == com.oracle.truffle.api.frame.FrameSlotKind.Double, "float64 slot");
        assertTrue(GoKind.BOOL.slotKind() == com.oracle.truffle.api.frame.FrameSlotKind.Boolean, "bool slot");
        assertTrue(GoKind.STRING.slotKind() == com.oracle.truffle.api.frame.FrameSlotKind.Object, "string slot");
        // complex kinds map since WS G (builtin complex/real/imag, GG-0020).
        assertTrue(GoKind.fromBasic("complex64", "complex") == GoKind.COMPLEX64, "complex64");
        assertTrue(GoKind.fromBasic("complex128", "complex") == GoKind.COMPLEX128, "complex128");
        assertTrue(GoKind.fromBasic("untyped complex", "complex") == GoKind.UNTYPED_COMPLEX, "untyped complex");
    }

    public void testGoKindTransparent() {
        assertTrue(GoKind.transparent(GoKind.UNTYPED_INT, GoKind.INT32), "untyped int -> int32");
        assertTrue(GoKind.transparent(GoKind.UNTYPED_INT, GoKind.UINT64), "untyped int -> uint64");
        assertTrue(GoKind.transparent(GoKind.UNTYPED_INT, GoKind.FLOAT64), "untyped int -> float64");
        assertTrue(GoKind.transparent(GoKind.UNTYPED_INT, GoKind.FLOAT32), "untyped int -> float32");
        assertTrue(GoKind.transparent(GoKind.UNTYPED_FLOAT, GoKind.FLOAT32), "untyped float -> float32");
        assertTrue(!GoKind.transparent(GoKind.UNTYPED_FLOAT, GoKind.INT64), "untyped float -/-> int64");
        assertTrue(!GoKind.transparent(GoKind.INT32, GoKind.INT64), "int32 -/-> int64");
        assertTrue(GoKind.transparent(GoKind.BOOL, GoKind.BOOL), "bool -> bool");
    }

    public void testGoStringRune() {
        assertBytes(new byte[]{65}, GoString.fromRune(65).bytes(), "A");
        assertBytes(new byte[]{(byte) 0xF0, (byte) 0x9F, (byte) 0x98, (byte) 0x80},
                GoString.fromRune(0x1F600).bytes(), "emoji");
        byte[] fffd = {(byte) 0xEF, (byte) 0xBF, (byte) 0xBD};
        assertBytes(fffd, GoString.fromRune(-1).bytes(), "negative -> FFFD");
        assertBytes(fffd, GoString.fromRune(0xD800).bytes(), "surrogate -> FFFD");
        assertBytes(fffd, GoString.fromRune(0x110000).bytes(), ">max -> FFFD");
        assertBytes(new byte[]{0}, GoString.fromRune(0).bytes(), "NUL");
        assertBytes(new byte[]{(byte) 0xC3, (byte) 0xBF}, GoString.fromRune(255).bytes(), "0xFF");
        assertTrue(GoString.EMPTY.length() == 0, "empty");
    }

    public void testGoStringCompare() {
        GoString abc = GoString.fromUtf8("abc");
        GoString abd = GoString.fromUtf8("abd");
        assertTrue(abc.compareTo(abd) < 0, "abc<abd");
        assertTrue(abd.compareTo(abc) > 0, "abd>abc");
        assertTrue(abc.compareTo(GoString.fromUtf8("abc")) == 0, "abc==abc");
        assertTrue(GoString.EMPTY.compareTo(abc) < 0, "empty<abc");
        // Unsigned byte order: 0xFF > 0x7F.
        GoString high = new GoString(new byte[]{(byte) 0xFF});
        GoString low = new GoString(new byte[]{0x7F});
        assertTrue(high.compareTo(low) > 0, "unsigned byte order");
        assertTrue(abc.concat(abd).equals(GoString.fromUtf8("abcabd")), "concat");
    }

    public void testGoMap() {
        GoMap m = new GoMap();
        assertTrue(m.get(1L) == null, "absent -> null");
        assertEq(0, m.size(), "empty size");
        for (long i = 0; i < 100; i++) {
            m.put(i, i * 2);
        }
        assertEq(100, m.size(), "size after puts");
        for (long i = 0; i < 100; i++) {
            assertEq(i * 2, (Long) m.get(i), "get " + i);
        }
        m.put(5L, 999L);
        assertEq(999L, (Long) m.get(5L), "replace");
        assertEq(100, m.size(), "size after replace");
        m.delete(5L);
        assertTrue(m.get(5L) == null, "deleted -> null");
        assertEq(99, m.size(), "size after delete");
        m.delete(5L); // no-op
        assertEq(99, m.size(), "double delete no-op");
        // String keys.
        GoMap sm = new GoMap();
        sm.put(GoString.fromUtf8("k"), 1L);
        assertEq(1L, (Long) sm.get(GoString.fromUtf8("k")), "string key");
        // NaN keys never match (Go semantics).
        GoMap fm = new GoMap();
        fm.put(Double.NaN, 1L);
        assertTrue(fm.get(Double.NaN) == null, "NaN key never matches");
        assertEq(1, fm.size(), "NaN key still stored");
    }

    public void testGoStruct() {
        GoStructDescriptor d = GoStructDescriptor.layout("T",
                List.of("a", "b", "s"), List.of(GoKind.INT32, GoKind.STRING, GoKind.BOOL));
        assertEq(2, d.longsCount(), "longs count");
        assertEq(1, d.objectsCount(), "objects count");
        GoStruct z = GoStruct.zero(d);
        assertEq(0, z.getLong(0), "zero int field");
        assertTrue(z.getObject(1) == GoString.EMPTY, "zero string field");
        assertEq(0, z.getLong(2), "zero bool field");
        z.setLong(0, 42);
        z.setObject(1, GoString.fromUtf8("hi"));
        GoStruct c = z.copy();
        assertEq(42, c.getLong(0), "copy int");
        assertTrue(c.getObject(1).equals(GoString.fromUtf8("hi")), "copy string");
        c.setLong(0, 7);
        assertEq(42, z.getLong(0), "copy is independent");
        assertTrue(z.equals(z.copy()), "struct equals");
    }

    public void testGoEqualityScalarComplex() {
        // R22-F-1: distinct-but-equal scalar complex values compare equal
        // (pre-fix they fell through to `return false`).
        assertTrue(GoEquality.equal(new GoComplex32(1f, 2f), new GoComplex32(1f, 2f)),
                "c32 equal");
        assertTrue(GoEquality.equal(new GoComplex64(1d, 2d), new GoComplex64(1d, 2d)),
                "c128 equal");
        assertTrue(!GoEquality.equal(new GoComplex32(1f, 2f), new GoComplex32(1f, 3f)),
                "c32 im differs");
        assertTrue(!GoEquality.equal(new GoComplex64(1d, 2d), new GoComplex64(3d, 2d)),
                "c128 re differs");
        // Cross-carrier: complex64 != complex128 even with equal components.
        assertTrue(!GoEquality.equal(new GoComplex32(1f, 2f), new GoComplex64(1d, 2d)),
                "c32 vs c128");
        // IEEE: a NaN component never equals — including the same reference
        // (the identity fast path must not save it, as for Double/Float).
        GoComplex64 nan = new GoComplex64(Double.NaN, 0d);
        assertTrue(!GoEquality.equal(nan, nan), "NaN re, same ref");
        assertTrue(!GoEquality.equal(new GoComplex32(Float.NaN, 0f),
                new GoComplex32(Float.NaN, 0f)), "NaN re c32");
        assertTrue(!GoEquality.equal(new GoComplex64(0d, Double.NaN),
                new GoComplex64(0d, Double.NaN)), "NaN im c128");
        // -0.0 == 0.0 component-wise (== semantics).
        assertTrue(GoEquality.equal(new GoComplex64(-0.0, 0d), new GoComplex64(0d, 0d)),
                "-0 re == 0 re");
        // Hash consistency: equal values hash equal (pre-fix scalar complex
        // hashed by identity, so two equal keys hashed differently).
        assertEq(GoEquality.hash(new GoComplex32(1f, 2f)),
                GoEquality.hash(new GoComplex32(1f, 2f)), "c32 hash");
        assertEq(GoEquality.hash(new GoComplex64(1d, 2d)),
                GoEquality.hash(new GoComplex64(1d, 2d)), "c128 hash");
        // Struct with a complex64 field: equal structs compare equal
        // (pre-fix always false — complex fields are object-carried).
        GoStructDescriptor d = GoStructDescriptor.layout("S",
                List.of("c"), List.of(GoKind.COMPLEX64));
        GoStruct s1 = GoStruct.zero(d);
        GoStruct s2 = GoStruct.zero(d);
        s1.setObject(0, new GoComplex32(3f, 4f));
        s2.setObject(0, new GoComplex32(3f, 4f));
        assertTrue(GoEquality.equal(s1, s2), "struct complex64 field equal");
        assertTrue(s1.equals(s2), "GoStruct.equals complex64");
        assertEq(s1.hashCode(), s2.hashCode(), "struct complex64 hash");
        s2.setObject(0, new GoComplex32(3f, 5f));
        assertTrue(!GoEquality.equal(s1, s2), "struct complex64 field differs");
        // Identity path: composites are NOT short-circuited — a
        // same-reference struct/array with a NaN element still compares
        // false (the `nan == nan` probe case; the identity fast path
        // used to return true here).
        GoStruct n1 = GoStruct.zero(d);
        n1.setObject(0, new GoComplex32(Float.NaN, 0f));
        assertTrue(!GoEquality.equal(n1, n1), "same-ref struct NaN complex field");
        GoComplex32[] arr = {new GoComplex32(Float.NaN, 0f)};
        assertTrue(!GoEquality.equal(arr, arr), "same-ref complex array NaN element");
        // Uncovered array carrier: falls through every branch, so
        // identity is the only thing that can save it (D-P1-1
        // fall-through contract: same ref true, distinct refs false).
        char[] ch = {'a'};
        assertTrue(GoEquality.equal(ch, ch), "same-ref char[]");
        assertTrue(!GoEquality.equal(new char[]{'a'}, new char[]{'a'}), "distinct char[]");
        // Map with a complex struct key round-trips (pre-fix always a miss).
        GoMap m = new GoMap();
        GoStruct k1 = GoStruct.zero(d);
        k1.setObject(0, new GoComplex32(3f, 4f));
        m.put(k1, 7L);
        GoStruct k2 = GoStruct.zero(d);
        k2.setObject(0, new GoComplex32(3f, 4f));
        assertEq(7L, (Long) m.get(k2), "map complex struct key round-trip");
    }

    public void testGoComplexRecordGoSemantics() {
        // D-P1-1: the GoComplex records carry Go ==/hash semantics, not
        // the Java record defaults (Float/Double.equals: NaN == NaN,
        // -0.0 != +0.0). Every -0.0/NaN assertion below fails on the
        // pre-fix generated equals/hashCode.
        GoComplex64 c64 = new GoComplex64(1d, 2d);
        assertTrue(c64.equals(new GoComplex64(1d, 2d)), "c128 record equal");
        assertTrue(!c64.equals(new GoComplex64(1d, 3d)), "c128 record differs");
        assertTrue(!c64.equals(new GoComplex32(1f, 2f)), "c128 vs c32 record");
        assertTrue(!c64.equals(null), "record vs null");
        assertTrue(!c64.equals("1+2i"), "record vs other type");
        // IEEE: a NaN component never equals — including the same reference
        // (pre-fix the generated equals returned true here).
        GoComplex64 nan64 = new GoComplex64(Double.NaN, 0d);
        assertTrue(!nan64.equals(nan64), "NaN re, same ref, record equals");
        assertTrue(!new GoComplex64(0d, Double.NaN).equals(new GoComplex64(0d, Double.NaN)),
                "NaN im, record equals");
        GoComplex32 nan32 = new GoComplex32(Float.NaN, 0f);
        assertTrue(!nan32.equals(nan32), "NaN re c32, same ref, record equals");
        // -0.0 == +0.0 component-wise (pre-fix the generated equals
        // returned false here).
        assertTrue(new GoComplex64(-0.0, 0d).equals(new GoComplex64(0d, 0d)),
                "-0 re == 0 re, record equals");
        assertTrue(new GoComplex64(0d, -0.0).equals(new GoComplex64(0d, 0d)),
                "-0 im == 0 im, record equals");
        assertTrue(new GoComplex32(-0.0f, 0f).equals(new GoComplex32(0f, 0f)),
                "-0 re c32, record equals");
        // Hash: equal values hash equal. -0.0 canonicalizes to +0.0
        // (pre-fix Float/Double.hashCode distinguished them).
        assertEq(new GoComplex64(-0.0, 0d).hashCode(), new GoComplex64(0d, 0d).hashCode(),
                "c128 -0 re hash");
        assertEq(new GoComplex64(0d, -0.0).hashCode(), new GoComplex64(0d, 0d).hashCode(),
                "c128 -0 im hash");
        assertEq(new GoComplex32(-0.0f, 0f).hashCode(), new GoComplex32(0f, 0f).hashCode(),
                "c32 -0 re hash");
        assertEq(c64.hashCode(), new GoComplex64(1d, 2d).hashCode(), "c128 equal hash");
        // GoEquality.hash agrees with the record hashCode by construction
        // (it delegates) and canonicalizes -0.0 the same way.
        assertEq(GoEquality.hash(new GoComplex64(-0.0, 0d)),
                GoEquality.hash(new GoComplex64(0d, 0d)), "GoEquality -0 re hash");
        assertEq(GoEquality.hash(new GoComplex32(-0.0f, 0f)),
                GoEquality.hash(new GoComplex32(0f, 0f)), "GoEquality -0 re c32 hash");
        assertEq(GoEquality.hash(c64), c64.hashCode(), "GoEquality == record hash");
        // Map with a scalar complex key round-trips across -0.0/+0.0
        // (pre-fix the -0.0 key hashed to a different bucket: a miss).
        GoMap m = new GoMap();
        m.put(new GoComplex64(0d, 0d), 7L);
        assertEq(7L, (Long) m.get(new GoComplex64(-0.0, 0d)), "map -0 re key hit");
        assertEq(7L, (Long) m.get(new GoComplex64(0d, -0.0)), "map -0 im key hit");
        m.put(new GoComplex32(0f, 0f), 9L);
        assertEq(9L, (Long) m.get(new GoComplex32(-0.0f, 0f)), "map -0 re c32 key hit");
        // Complex array keys: equal arrays (modulo -0.0) hash equal.
        assertEq(GoEquality.hash(new GoComplex64[]{new GoComplex64(-0.0, 0d)}),
                GoEquality.hash(new GoComplex64[]{new GoComplex64(0d, 0d)}),
                "c128 array -0 hash");
        assertEq(GoEquality.hash(new GoComplex32[]{new GoComplex32(-0.0f, 0f)}),
                GoEquality.hash(new GoComplex32[]{new GoComplex32(0f, 0f)}),
                "c32 array -0 hash");
    }

    public void testGoInterface() {
        assertTrue(GoInterface.NIL.isNil(), "nil interface");
        GoInterface v = new GoInterface("desc", 42L, "itab");
        assertTrue(!v.isNil(), "non-nil");
        assertTrue(v.itab() == "itab", "itab");
    }

    public void testUnsafeHandles() {
        UnsafeHandles h = new UnsafeHandles();
        assertEq(0, h.alloc(null), "null -> 0");
        Object o = new Object();
        long hd = h.alloc(o);
        assertTrue(hd != 0, "non-zero handle");
        assertTrue(h.get(hd) == o, "resolve");
        assertTrue(h.get(0) == null, "nil handle -> null");
        h.free(hd);
        try {
            h.get(hd);
            throw new AssertionError("use after free should trap");
        } catch (UnsafeHandles.UnsafeEscapeException expected) {
        }
        try {
            h.free(hd);
            throw new AssertionError("double free should trap");
        } catch (UnsafeHandles.UnsafeEscapeException expected) {
        }
    }

    public void testUnsafePointerToUintptrIdentityToken() {
        // PKG-007: unsafe.Pointer → uintptr yields the handle as an integer
        // identity token — never a raw address, never trapping.
        UnsafeHandles h = new UnsafeHandles();
        assertEq(0, GoConvert.unsafePointerToUintptr(0), "nil handle -> 0");
        Object o1 = new Object();
        Object o2 = new Object();
        long hd1 = h.alloc(o1);
        long hd2 = h.alloc(o2);
        // Identity: the token IS the handle (stable across conversions).
        assertEq(hd1, GoConvert.unsafePointerToUintptr(hd1), "token == handle");
        assertEq(hd1, GoConvert.unsafePointerToUintptr(hd1), "token stable");
        // Distinctness: distinct handles -> distinct tokens.
        assertTrue(GoConvert.unsafePointerToUintptr(hd1)
                != GoConvert.unsafePointerToUintptr(hd2), "distinct tokens");
        // Non-nil tokens are non-zero (nil is the only 0).
        assertTrue(GoConvert.unsafePointerToUintptr(hd1) != 0, "non-nil -> non-zero");
        // The token is detached from the handle's lifetime (gc: the uintptr
        // does not keep the object alive): freeing the handle leaves the
        // already-escaped integer untouched — it just names a dead handle.
        long tok = GoConvert.unsafePointerToUintptr(hd1);
        h.free(hd1);
        assertEq(hd1, tok, "escaped token survives free");
        // But the dead handle still fails loud on resolution (no silent
        // aliasing), and a fresh alloc never reuses the value.
        try {
            h.get(hd1);
            throw new AssertionError("use after free should trap");
        } catch (UnsafeHandles.UnsafeEscapeException expected) {
        }
        long hd3 = h.alloc(new Object());
        assertTrue(hd3 != hd1, "handle never reused after free");
    }

    public void testGoConvertFloatToInt() {
        // gc/amd64-measured values (M3 probes).
        assertEq(Long.MIN_VALUE, GoConvert.floatToInt64(1e300), "1e300->int64");
        assertEq(Long.MIN_VALUE, GoConvert.floatToInt64(Double.NaN), "NaN->int64");
        assertEq(Long.MIN_VALUE, GoConvert.floatToInt64(Double.POSITIVE_INFINITY), "+Inf->int64");
        assertEq(Long.MIN_VALUE, GoConvert.floatToInt64(-9.3e18), "-9.3e18->int64");
        assertEq(123L, GoConvert.floatToInt64(123.999), "123.999->int64");
        assertEq(-123L, GoConvert.floatToInt64(-123.999), "-123.999->int64");
        assertEq(0L, GoConvert.floatToInt64(0.9), "0.9->int64");
        assertEq(0L, GoConvert.floatToInt64(-0.9), "-0.9->int64");
        // uint64: 0x8000000000000000 on out-of-range.
        assertEq(Long.MIN_VALUE, GoConvert.floatToUint64(1e300), "1e300->uint64");
        assertEq(Long.MIN_VALUE, GoConvert.floatToUint64(-1.5), "-1.5->uint64");
        assertEq(Long.MIN_VALUE, GoConvert.floatToUint64(Double.NaN), "NaN->uint64");
        assertEq(Long.parseUnsignedLong("9300000000000000000"), GoConvert.floatToUint64(9.3e18), "9.3e18->uint64");
        assertEq(0L, GoConvert.floatToUint64(0.9), "0.9->uint64");
        // int32: 0x80000000 on out-of-range.
        assertEq(Integer.MIN_VALUE, GoConvert.floatToInt32(1e300), "1e300->int32");
        assertEq(Integer.MIN_VALUE, GoConvert.floatToInt32(3.5e9), "3.5e9->int32");
    }

    public void testGoConvertFloatToIntNarrow() {
        // float->uint32 goes via int64 then low 32 bits (gc-measured).
        assertEq(705032704, GoConvert.floatToUint32(5e9), "5e9->uint32");
        assertEq(0, GoConvert.floatToUint32(1e300), "1e300->uint32");
        assertEq(-1, GoConvert.floatToUint32(-1.5), "-1.5->uint32 (=-1)");
        // float->int16/uint16/int8/uint8 go via int32 then truncation.
        assertEq(0, (int) (short) GoConvert.floatToInt32(5e9), "5e9->int16");
        assertEq(300, (int) (short) GoConvert.floatToInt32(300.5), "300.5->int16");
        assertEq(44, (int) (byte) GoConvert.floatToInt32(300.0), "300.0->uint8");
        assertEq(-64, (int) (byte) GoConvert.floatToInt32(-40000.9), "-40000.9->int8");
    }

    public void testGoConvertIntToFloat() {
        // uint64 max -> 2^64 (gc-measured bits 4895412794951729152).
        assertEq(4895412794951729152L,
                Double.doubleToLongBits(GoConvert.uint64ToDouble(0xFFFFFFFFFFFFFFFFL)),
                "uint64max->float64");
        assertEq(4845873199050653696L,
                Double.doubleToLongBits(GoConvert.uint64ToDouble(9007199254740993L)),
                "2^53+1->float64");
        assertEq(1602224128, Float.floatToIntBits(GoConvert.uint64ToFloat(0xFFFFFFFFFFFFFFFFL)),
                "uint64max->float32");
        // int64 -> float32 correct rounding: 2^24+1 is exactly between the
        // adjacent floats 2^24 (even mantissa) and 2^24+2 (odd mantissa);
        // round-half-even picks 2^24. Verified against gc (probe 2026-10-04).
        assertEq(Float.floatToIntBits(16777216f),
                Float.floatToIntBits(GoConvert.int64ToFloat((1L << 24) + 1)), "2^24+1->float32");
    }

    public void testGoConvertIntToString() {
        assertBytes(new byte[]{65}, GoConvert.intToString(65).bytes(), "65");
        assertBytes(new byte[]{(byte) 0xEF, (byte) 0xBF, (byte) 0xBD},
                GoConvert.intToString(-1).bytes(), "-1 -> FFFD");
        assertBytes(new byte[]{(byte) 0xEF, (byte) 0xBF, (byte) 0xBD},
                GoConvert.intToString(0xFFFFFFFFFFFFFFFFL).bytes(), "uint64max -> FFFD");
        assertBytes(new byte[]{(byte) 0xEF, (byte) 0xBF, (byte) 0xBD},
                GoConvert.intToString(0xD800).bytes(), "surrogate -> FFFD");
        // Untyped huge constant: string(1<<64 + 0x1F600) -> FFFD (gc-measured).
        assertBytes(new byte[]{(byte) 0xEF, (byte) 0xBF, (byte) 0xBD},
                GoConvert.untypedIntToString("18446744073709680128").bytes(), "2^64+0x1F600 -> FFFD");
        assertBytes(new byte[]{(byte) 0xF0, (byte) 0x9F, (byte) 0x98, (byte) 0x80},
                GoString.fromRune(0x1F600).bytes(), "sanity emoji");
    }

    public void testInterfaceDispatchProbes() {
        InterfaceDispatchProbes.reset();
        InterfaceDispatchProbes.setEnabled(false);
        InterfaceDispatchProbes.Site s = InterfaceDispatchProbes.registerSite("test");
        Object itab = new Object();
        s.record(itab);
        assertEq(0, s.total(), "disabled -> no recording");
        InterfaceDispatchProbes.setEnabled(true);
        s.record(itab);
        s.record(itab);
        s.record(new Object());
        assertEq(3, s.total(), "enabled -> recorded");
        Map<Integer, Long> hist = s.histogram();
        assertEq(2, hist.size(), "two distinct itabs");
        assertEq(2L, hist.get(System.identityHashCode(itab)), "itab count");
        InterfaceDispatchProbes.setEnabled(false);
        InterfaceDispatchProbes.reset();
    }

    public void testGoSlice() {
        int[] backing = new int[10];
        GoSlice s = new GoSlice(backing, 2, 5, 8);
        assertEq(5, s.len(), "len");
        assertEq(8, s.cap(), "cap");
        GoSlice sub = s.slice(1, 3);
        assertEq(2, sub.len(), "sub len");
        assertTrue(sub.array() == backing, "shares backing");
        try {
            s.slice(0, 9);
            throw new AssertionError("out-of-range subslice should fail");
        } catch (IndexOutOfBoundsException expected) {
        }
    }
}
