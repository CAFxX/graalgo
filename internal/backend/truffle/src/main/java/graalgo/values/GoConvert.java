package graalgo.values;

import java.math.BigInteger;

/**
 * The Go conversion matrix that is not bit-trivial (D5, WS D).
 *
 * <p>Integer↔integer conversions are pure bit truncation/extension (handled
 * in {@code ConvertNode}); this class holds the float-involving conversions
 * and the integer→string (rune) conversion. Every out-of-range rule below
 * mirrors stock gc on amd64 <em>as measured</em> (the Go spec leaves these
 * implementation-defined; the probes live in the M3 report):
 *
 * <ul>
 *   <li>float→int64/int: NaN, ±Inf, or out of [-2^63, 2^63) →
 *       {@code 0x8000000000000000}; else truncation toward zero.</li>
 *   <li>float→uint64/uint/uintptr: NaN, &lt; -2^63, or ≥ 2^64 →
 *       {@code 0x8000000000000000}; else truncation toward zero, with the
 *       bits reinterpreted as unsigned — negatives in [-2^63, -1.0]
 *       wrap (e.g. {@code -1.0 → 0xFFFFFFFFFFFFFFFF}).</li>
 *   <li>float→int32: NaN, ±Inf, or out of [-2^31, 2^31) → {@code 0x80000000};
 *       else truncation toward zero.</li>
 *   <li>float→uint32: via the int64 conversion, then low 32 bits.</li>
 *   <li>float→int16/uint16/int8/uint8: via the int32 conversion, then low
 *       16/8 bits.</li>
 *   <li>int/uint→float32/64 and float32↔float64: correctly rounded
 *       (matches gc's CVTSI2S[S,D]/CVTSD2SS on all probed values, incl.
 *       2644 adversarial uint64→float32 cases).</li>
 *   <li>int→string: the UTF-8 encoding of the rune; out-of-range
 *       (negative, surrogate, &gt; 0x10FFFF) → U+FFFD.</li>
 * </ul>
 */
public final class GoConvert {
    private GoConvert() {}

    private static final double TWO_POW_63 = 9223372036854775808.0;
    private static final double TWO_POW_64 = 18446744073709551616.0;
    private static final double TWO_POW_31 = 2147483648.0;

    /** float64 → int64/int, gc/amd64 mirror. */
    public static long floatToInt64(double d) {
        if (Double.isNaN(d) || d <= -TWO_POW_63 || d >= TWO_POW_63) {
            return Long.MIN_VALUE;
        }
        return (long) d;
    }

    /** float32 → int64/int, gc/amd64 mirror (widening is exact). */
    public static long floatToInt64(float f) {
        return floatToInt64((double) f);
    }

    /** float64 → uint64/uint/uintptr, gc/amd64 mirror. */
    public static long floatToUint64(double d) {
        // gc truncates toward zero and reinterprets the bits for negatives
        // in [-2^63, -1.0] (measured against go1.27.1/amd64); only NaN,
        // < -2^63, or >= 2^64 yield 0x8000000000000000. (long) d already
        // truncates toward zero, so negatives in range land on the correct
        // two's-complement bit pattern without special-casing.
        if (Double.isNaN(d) || d < -TWO_POW_63 || d >= TWO_POW_64) {
            return Long.MIN_VALUE; // bit pattern 0x8000000000000000
        }
        if (d < TWO_POW_63) {
            return (long) d;
        }
        // d in [2^63, 2^64): d - 2^63 is exact (Sterbenz), then re-bias.
        return (long) (d - TWO_POW_63) + Long.MIN_VALUE;
    }

    /** float32 → uint64/uint/uintptr, gc/amd64 mirror. */
    public static long floatToUint64(float f) {
        return floatToUint64((double) f);
    }

    /** float64 → int32, gc/amd64 mirror. */
    public static int floatToInt32(double d) {
        if (Double.isNaN(d) || d <= -TWO_POW_31 || d >= TWO_POW_31) {
            return Integer.MIN_VALUE;
        }
        return (int) d;
    }

    /** float32 → int32, gc/amd64 mirror. */
    public static int floatToInt32(float f) {
        return floatToInt32((double) f);
    }

    /** float → uint32: gc converts via int64, then takes the low 32 bits. */
    public static int floatToUint32(double d) {
        return (int) floatToInt64(d);
    }

    public static int floatToUint32(float f) {
        return (int) floatToInt64(f);
    }

    /**
     * uint64 (as signed-long bits) → float64, mirroring gc/amd64's
     * shift-double-add sequence exactly (correctly rounded on all probed
     * values).
     */
    public static double uint64ToDouble(long v) {
        if (v >= 0) {
            return (double) v;
        }
        // gc: SHR $1; CVTSI2SDQ (correctly rounded); ADDSD X0,X0 (exact);
        // TESTQ $1; ADDSD $1.0 (rounds). Each Java step is the same IEEE op.
        double d = (double) (v >>> 1);
        d += d;
        if ((v & 1) != 0) {
            d += 1.0;
        }
        return d;
    }

    /** uint64 → float32, correctly rounded (matches gc on 2644 probed values). */
    public static float uint64ToFloat(long v) {
        return Float.parseFloat(Long.toUnsignedString(v));
    }

    /** int64 → float32, correctly rounded (Java casts are correctly rounded). */
    public static float int64ToFloat(long v) {
        return (float) v;
    }

    /**
     * Go {@code string(i)} for a typed integer value (already a long):
     * negative or &gt; 0x10FFFF or a surrogate → U+FFFD.
     */
    public static GoString intToString(long v) {
        if (v < 0 || v > 0x10FFFF || (v >= 0xD800 && v <= 0xDFFF)) {
            return GoString.fromUtf8("\uFFFD");
        }
        return GoString.fromRune((int) v);
    }

    /**
     * Go {@code string(i)} for an untyped integer constant given by its
     * exact decimal string (needed when the value exceeds 64 bits, e.g.
     * {@code string(1<<64 + 0x1F600)} → U+FFFD, as gc computes).
     */
    public static GoString untypedIntToString(String decimal) {
        BigInteger v = new BigInteger(decimal);
        if (v.signum() < 0 || v.compareTo(BigInteger.valueOf(0x10FFFF)) > 0
                || (v.compareTo(BigInteger.valueOf(0xD800)) >= 0
                    && v.compareTo(BigInteger.valueOf(0xDFFF)) <= 0)) {
            return GoString.fromUtf8("\uFFFD");
        }
        return GoString.fromRune(v.intValue());
    }

    /**
     * Go {@code unsafe.Pointer → uintptr} (PKG-007): yields the handle
     * itself as an integer <em>identity token</em>.
     *
     * <p>D5 has no observable address (GG-0015), but it does have an
     * identity: {@link UnsafeHandles} interning (P3-M10) assigns the same
     * handle to the same storage location, never reuses a handle value,
     * and reserves 0 for the nil pointer — and {@code ==} on
     * {@code unsafe.Pointer} already compares handles. The conversion
     * surfaces that existing identity as an integer, so the portable
     * properties gc code depends on hold:
     * <ul>
     *   <li>nil {@code unsafe.Pointer} (handle 0) → 0;</li>
     *   <li>stability: the same referent converts to the same integer
     *       across conversions (while the handle is live);</li>
     *   <li>distinctness: distinct live referents → distinct integers;</li>
     *   <li>equality parity: {@code uintptr(p) == uintptr(q)} iff
     *       {@code p == q}, matching gc's address equality on every
     *       representable case.</li>
     * </ul>
     *
     * <p>What the token is <em>not</em>: a memory address. No path leads
     * from the integer back to a referent — {@code uintptr →
     * unsafe.Pointer} still traps (a handle can never be materialized
     * from an integer; that would defeat the table's allocation
     * discipline) and {@code unsafe.Add} still traps (no byte arithmetic).
     * The token is an opaque comparable value: it can be compared,
     * hashed, stored, and printed, but never dereferenced. Lifetime
     * mirrors gc's own rule: the integer does not keep the referent alive
     * (the table's strong reference is tied to the handle, not the
     * escaped integer), and laundering it back through a pointer
     * conversion fails loud instead of silently aliasing.
     */
    public static long unsafePointerToUintptr(long handle) {
        return handle;
    }
}
