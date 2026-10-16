package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoConvert;
import graalgo.values.GoKind;

/**
 * A Go conversion {@code T(x)} (frontend {@code convert} node, M3 WS D).
 *
 * <p>The full conversion matrix with Go-exact semantics (verified against
 * stock gc):
 * <ul>
 *   <li>int/uint ↔ int/uint: bit truncation / sign- or zero-extension.</li>
 *   <li>int/uint → float32/64: correctly rounded (Java casts are; uint64
 *       uses {@link GoConvert#uint64ToDouble}/{@link GoConvert#uint64ToFloat}).</li>
 *   <li>float → int/uint: truncation toward zero; out-of-range mirrors
 *       gc/amd64 ({@link GoConvert}).</li>
 *   <li>float32 ↔ float64: widening exact, narrowing correctly rounded.</li>
 *   <li>complex64 ↔ complex128: widening exact, narrowing rounds each
 *       component to float32 (verified against stock gc).</li>
 *   <li>untyped int/float constant → complex64/128: the real part comes
 *       from the constant (exact decimal for untyped ints), the imaginary
 *       part is zero; go/types guarantees representability.</li>
 *   <li>int → string: the rune conversion (out-of-range → U+FFFD).</li>
 *   <li>struct → pointer: implicit {@code &T{}} (e.g. element elision in
 *       {@code []*T{{...}}} composite literals); wraps the struct value
 *       in a GoPointer.</li>
 *   <li>interface → chan: type-parameter desugaring (same as interface →
 *       pointer); extracts the channel from the interface's data.</li>
 * </ul>
 * Untyped constants never reach this node for same-family targets (they
 * are transparent — {@link GoKind#transparent}); the one untyped source
 * that does ({@code string(65)}) uses the constant's exact decimal.
 */
public final class ConvertNode extends GoExprNode {
    private final GoKind from;
    @Child private GoExprNode child;
    /** Element kind for STRING<->SLICE conversions (null otherwise). */
    private final GoKind sliceElemKind;

    public ConvertNode(GoKind from, GoKind to, GoExprNode child) {
        this(from, to, child, null);
    }

    public ConvertNode(GoKind from, GoKind to, GoExprNode child, GoKind sliceElemKind) {
        super(to);
        checkCombo(from, to, sliceElemKind);
        this.from = from;
        this.child = child;
        this.sliceElemKind = sliceElemKind;
    }

    private static void checkCombo(GoKind from, GoKind to, GoKind sliceElemKind) {
        boolean numeric = (from.isIntKind() || from.isFloatKind())
                && (to.isIntKind() || to.isFloatKind());
        boolean intToString = from.isIntKind() && to == GoKind.STRING;
        boolean stringToString = from == GoKind.STRING && to == GoKind.STRING;
        boolean boolToBool = from == GoKind.BOOL && to == GoKind.BOOL;
        boolean unsafeEscape = from == GoKind.UNSAFE_POINTER && to == GoKind.UINT64;
        // M7: string <-> []byte/[]rune conversions.
        boolean stringToSlice = (from == GoKind.STRING || from == GoKind.UNTYPED_STRING)
                && to == GoKind.SLICE && (sliceElemKind == GoKind.UINT8 || sliceElemKind == GoKind.INT32);
        boolean sliceToString = from == GoKind.SLICE && to == GoKind.STRING
                && (sliceElemKind == GoKind.UINT8 || sliceElemKind == GoKind.INT32);
        // P3-M9: interface{} -> *T assertion (desugared pair fields where
        // v0 is interface{} but holds a concrete pointer; the Go source
        // uses a type parameter P, and the exporter emits a convert).
        // At runtime the interface holds the pointer; extract its data.
        boolean ifaceToPointer = from == GoKind.INTERFACE && to == GoKind.POINTER;
        // fix-interface-chan-convert: interface{} -> chan T assertion
        // (same desugaring as ifaceToPointer: the Go source uses a type
        // parameter P, and the exporter emits a convert). At runtime the
        // interface holds the channel; extract its data.
        boolean ifaceToChan = from == GoKind.INTERFACE && to == GoKind.CHAN;
        // P3-M9: []T -> *T (e.g., &s[0] desugared as convert in generic
        // contexts). At runtime, create a pointer to the first element.
        boolean sliceToPointer = from == GoKind.SLICE && to == GoKind.POINTER;
        // real-x509 lane: STRUCT -> POINTER (e.g., the implicit &T{}
        // element elision in []*T{{...}} composite literals, where each
        // {…} is a T struct literal but the slice element type is *T).
        // General Go language mechanism, not package-specific. At runtime,
        // wrap the struct value in a GoPointer (address-of semantics).
        boolean structToPointer = from == GoKind.STRUCT && to == GoKind.POINTER;
        // WS G: complex <-> complex (complex64 <-> complex128, either
        // direction; go/types rejects complex <-> float/int and
        // float/int <-> complex, so those never reach this node).
        boolean complexToComplex = from.isComplexKind() && to.isComplexKind();
        // WS G: untyped int/float constant -> complex (e.g. complex128(1.5),
        // complex64(3)); the constant is the real part, imaginary part zero.
        // go/types guarantees representability in the target. Untyped
        // complex constants are transparent to complex targets (GoKind),
        // so they only reach here via direct construction.
        boolean untypedConstToComplex = (from == GoKind.UNTYPED_INT || from == GoKind.UNTYPED_FLOAT)
                && to.isComplexKind();
        if (!(numeric || intToString || stringToString || boolToBool || unsafeEscape
                || stringToSlice || sliceToString || ifaceToPointer || ifaceToChan || sliceToPointer
                || structToPointer
                || complexToComplex || untypedConstToComplex)) {
            throw new IllegalArgumentException(
                    "illegal conversion: " + from + " -> " + to);
        }
    }

    private static int extendInt(long bits, GoKind to) {
        int v = (int) bits;
        return switch (to) {
            case INT8 -> (int) (byte) v;
            case INT16 -> (int) (short) v;
            case UINT8 -> v & 0xFF;
            case UINT16 -> v & 0xFFFF;
            default -> v;
        };
    }

    private static long extendLong(long bits, GoKind to) {
        return switch (to) {
            case INT8 -> (long) (byte) bits;
            case INT16 -> (long) (short) bits;
            case INT32 -> (long) (int) bits;
            // P3-M8b: UINT8/UINT16 must mask in executeLong too (array indices
            // call executeLong; without masking, uint8(x) returns x unchanged
            // when x > 255, causing index-out-of-range panics).
            // P3-M9 (flate-encoder-fix): UINT32 was missing — uint32(x) in
            // executeLong returned x unchanged, breaking inline comparisons
            // like uint32(cv) == loadLE32(...) (BinaryOpNode compares via
            // executeLong). Zero-extend to 32 bits.
            case UINT8 -> bits & 0xFFL;
            case UINT16 -> bits & 0xFFFFL;
            case UINT32 -> bits & 0xFFFFFFFFL;
            default -> bits;
        };
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        GoKind to = kind();
        if (to == GoKind.UINT64 && from == GoKind.UNSAFE_POINTER) {
            return GoConvert.unsafePointerToUintptr(child.executeLong(frame));
        }
        if (from.isFloatKind()) {
            double d = from == GoKind.FLOAT32 ? (double) child.executeFloat(frame) : child.executeDouble(frame);
            return to == GoKind.UINT64 ? GoConvert.floatToUint64(d) : GoConvert.floatToInt64(d);
        }
        // int/uint -> int64/uint64: the child's extended long is already exact.
        return extendLong(child.executeLong(frame), to);
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        GoKind to = kind();
        if (from.isFloatKind()) {
            double d = from == GoKind.FLOAT32 ? (double) child.executeFloat(frame) : child.executeDouble(frame);
            int w = to == GoKind.UINT32 ? GoConvert.floatToUint32(d) : GoConvert.floatToInt32(d);
            return extendInt(w, to);
        }
        return extendInt(child.executeLong(frame), to);
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        if (from == GoKind.FLOAT64) {
            return (float) child.executeDouble(frame);
        }
        if (from == GoKind.UNTYPED_INT) {
            // Defensive: the factory keeps untyped ints transparent to float
            // kinds, so this only runs on direct construction. The child can
            // be any untyped-constant subtree (unary isn't folded), hence the
            // node protocol instead of a ConstNode cast.
            return Float.parseFloat(child.exactDecimal());
        }
        long v = child.executeLong(frame);
        // from is an int/uint kind (bool/string/float32 sources can't land here).
        return from.isUintKind() ? GoConvert.uint64ToFloat(v) : (float) v;
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        if (from == GoKind.FLOAT32) {
            return (double) child.executeFloat(frame);
        }
        if (from.isFloatKind()) {
            return child.executeDouble(frame);
        }
        if (from == GoKind.UNTYPED_INT) {
            // Defensive: see executeFloat.
            return Double.parseDouble(child.exactDecimal());
        }
        long v = child.executeLong(frame);
        return from.isUintKind() ? GoConvert.uint64ToDouble(v) : (double) v;
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return child.executeBoolean(frame);
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        GoKind to = kind();
        // WS G: complex-typed conversions. Complex values travel as objects
        // (GoComplex32 for complex64, GoComplex64 for complex128 — D5).
        if (to.isComplexKind()) {
            return convertToComplex(frame);
        }
        if (to == GoKind.STRING && from == GoKind.STRING) {
            return child.executeObject(frame);
        }
        if (to == GoKind.STRING && from == GoKind.UNTYPED_INT) {
            // The child can be any untyped-constant subtree (unary isn't
            // folded by the exporter), hence the node protocol.
            return GoConvert.untypedIntToString(child.exactDecimal());
        }
        if (to == GoKind.STRING) {
            // M7: []byte/[]rune -> string.
            if (from == GoKind.SLICE) {
                return sliceToString((graalgo.values.GoSlice) child.executeObject(frame));
            }
            return GoConvert.intToString(child.executeLong(frame));
        }
        // M7: string -> []byte/[]rune.
        if (to == GoKind.SLICE && (from == GoKind.STRING || from == GoKind.UNTYPED_STRING)) {
            return stringToSlice((graalgo.values.GoString) child.executeObject(frame));
        }
        // P3-M9: interface{} -> *T (desugared pair field assertion).
        // The interface holds the concrete pointer; extract its data.
        if (to == GoKind.POINTER && from == GoKind.INTERFACE) {
            Object v = child.executeObject(frame);
            if (v instanceof graalgo.values.GoInterface gi) {
                return gi.data();
            }
            return v;
        }
        // fix-interface-chan-convert: interface{} -> chan T (same
        // desugaring as ifaceToPointer: the Go source uses a type
        // parameter P, and the exporter emits a convert). At runtime the
        // interface holds the channel; extract its data.
        if (to == GoKind.CHAN && from == GoKind.INTERFACE) {
            Object v = child.executeObject(frame);
            if (v instanceof graalgo.values.GoInterface gi) {
                return gi.data();
            }
            return v;
        }
        // P3-M9: []T -> *T (pointer to first element, e.g., &s[0]).
        if (to == GoKind.POINTER && from == GoKind.SLICE) {
            Object v = child.executeObject(frame);
            if (v instanceof graalgo.values.GoSlice gs) {
                Object arr = gs.array();
                int idx = gs.offset();
                return new graalgo.values.GoPointer(arr, idx);
            }
            // Java array (should not happen for SLICE kind, but be safe).
            return new graalgo.values.GoPointer(v, 0);
        }
        // real-x509 lane: STRUCT -> POINTER (implicit &T{} in []*T{{...}}
        // composite literals, and any other struct-to-pointer conversion
        // the exporter emits). General mechanism: wrap the struct value
        // in a GoPointer (address-of semantics). The struct value is a
        // Java object; the pointer refers to it directly.
        if (to == GoKind.POINTER && from == GoKind.STRUCT) {
            Object v = child.executeObject(frame);
            return new graalgo.values.GoPointer(v);
        }
        throw new UnsupportedOperationException("conversion " + from + " -> " + to + " has no object value");
    }

    /**
     * Builds the complex value for a complex-typed conversion (WS G).
     *
     * <p>complex64 ↔ complex128 converts component-wise: widening is exact,
     * narrowing rounds each component to float32 (Java {@code (float)} casts
     * are correctly rounded, matching gc's CVTSD2SS). An untyped int/float
     * constant becomes {@code (real, 0)}: the real part comes from the
     * constant — the exact decimal for untyped ints (so {@code 1<<70} stays
     * exact), the exporter's correctly-rounded f64 bits for untyped floats
     * (unary negation included via the child node's own execute method) —
     * and go/types guarantees representability in the target.
     */
    private Object convertToComplex(VirtualFrame frame) {
        GoKind to = kind();
        if (from.isComplexKind()) {
            Object v = child.executeObject(frame);
            double re, im;
            if (v instanceof graalgo.values.GoComplex64 c) {
                re = c.re();
                im = c.im();
            } else if (v instanceof graalgo.values.GoComplex32 c) {
                re = c.re();
                im = c.im();
            } else {
                throw new FrontendException(
                        "complex conversion of non-complex value " + v + " (WS G)");
            }
            return to == GoKind.COMPLEX64
                    ? new graalgo.values.GoComplex32((float) re, (float) im)
                    : new graalgo.values.GoComplex64(re, im);
        }
        double re = from == GoKind.UNTYPED_INT
                ? Double.parseDouble(child.exactDecimal())
                : child.executeDouble(frame);
        return to == GoKind.COMPLEX64
                ? new graalgo.values.GoComplex32((float) re, 0f)
                : new graalgo.values.GoComplex64(re, 0d);
    }

    /** Converts a Go string to a []byte or []rune slice. */
    private graalgo.values.GoSlice stringToSlice(graalgo.values.GoString s) {
        byte[] bytes = s.bytes();
        if (sliceElemKind == GoKind.UINT8) {
            // []byte is byte[]-backed (D5).
            byte[] arr = bytes.clone();
            return new graalgo.values.GoSlice(arr, 0, arr.length, arr.length);
        } else { // INT32 (rune)
            // bug-005: Go's []rune(string) decodes with utf8.DecodeRuneInString
            // semantics — each invalid byte yields one RuneError, advancing one
            // byte. Java's UTF-8 decoder consumes a multi-byte invalid sequence
            // as a single replacement char, disagreeing with gc (6 vs 7 runes).
            // Route through the same decoder the range-over-string loop uses
            // (RangeStringNode.decodeRune), which matches gc exactly.
            java.util.ArrayList<Integer> runes = new java.util.ArrayList<>();
            int off = 0;
            while (off < bytes.length) {
                int[] dr = RangeStringNode.decodeRune(bytes, off);
                runes.add(dr[0]);
                off += dr[1];
            }
            int[] arr = new int[runes.size()];
            for (int i = 0; i < arr.length; i++) {
                arr[i] = runes.get(i);
            }
            return new graalgo.values.GoSlice(arr, 0, arr.length, arr.length);
        }
    }

    /** Converts a []byte or []rune slice to a Go string. */
    private graalgo.values.GoString sliceToString(graalgo.values.GoSlice s) {
        if (s == null) {
            return graalgo.values.GoString.EMPTY;
        }
        Object raw = s.array();
        int offset = s.offset();
        int len = s.len();
        if (sliceElemKind == GoKind.UINT8) {
            // []byte is byte[]-backed (D5): direct copy.
            byte[] bytes = new byte[len];
            System.arraycopy((byte[]) raw, offset, bytes, 0, len);
            return new graalgo.values.GoString(bytes);
        } else { // INT32 (rune)
            int[] arr = (int[]) raw;
            StringBuilder sb = new StringBuilder(len);
            for (int i = 0; i < len; i++) {
                int cp = arr[offset + i];
                // Remediation (R1 P1-3): gc substitutes U+FFFD for values
                // that are not valid Unicode scalar values (surrogates,
                // >0x10FFFF, negative); appendCodePoint would either throw
                // (out of range) or emit a lone surrogate that the UTF-8
                // encoder then mangles to '?'. Same predicate as
                // GoConvert.intToString. (Character.isValidCodePoint is
                // insufficient: it admits surrogates.)
                if (cp < 0 || cp > 0x10FFFF || (cp >= 0xD800 && cp <= 0xDFFF)) {
                    cp = 0xFFFD;
                }
                sb.appendCodePoint(cp);
            }
            return graalgo.values.GoString.fromUtf8(sb.toString());
        }
    }
}
