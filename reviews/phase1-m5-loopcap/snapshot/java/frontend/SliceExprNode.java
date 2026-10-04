package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Slice expression {@code x[lo:hi]} / {@code x[lo:hi:max]} (M4, WS E) over
 * slices, arrays, and strings, with gc-exact bounds checks (M5, WS F).
 *
 * <p>Check order mirrors gc's ssagen ({@code slice()}): the high/max bound
 * is checked against length/capacity before the low bound is checked
 * against the high bound, so the <em>first</em> violated bound names the
 * panic. Messages are verbatim from runtime/error.go's
 * {@code boundsErrorFmts}/{@code boundsNegErrorFmts}; each bound renders
 * signed or unsigned per its Go kind. Slices report capacity, arrays and
 * strings report length.
 *
 * <p>Slicing shares the backing array (no copy); string slicing copies the
 * byte subrange (strings are immutable values). {@code s[0:0]} (and
 * {@code s[0:0:0]}) on a nil slice is legal and stays nil.
 */
public final class SliceExprNode extends GoExprNode {
    @Child private GoExprNode x;
    @Child private GoExprNode lo;
    @Child private GoExprNode hi;
    @Child private GoExprNode max;
    private final GoKind containerKind;

    public SliceExprNode(GoExprNode x, GoExprNode lo, GoExprNode hi, GoExprNode max,
                         GoKind containerKind, GoKind nodeKind) {
        super(nodeKind);
        this.x = x;
        this.lo = lo;
        this.hi = hi;
        this.max = max;
        this.containerKind = containerKind;
        if (max != null && hi == null) {
            // go/types rejects this at export time ("middle index required
            // in 3-index slice"); this is a defensive invariant.
            throw new FrontendException("middle index required in 3-index slice");
        }
    }

    /** A bound value with its Go signedness (selects gc's U check variants). */
    private record Bound(long v, boolean signed) {}

    private static Bound readBound(GoExprNode n, VirtualFrame frame) {
        return new Bound(n.executeLong(frame), n.kind().isSigned());
    }

    /**
     * Single bounds check with gc-exact panic. A negative signed bound
     * selects the overriding negative format; otherwise the bound must not
     * exceed {@code limit} (compared unsigned for unsigned bounds).
     */
    private static void checkBound(GoErrors.BoundsCode code, Bound b, long limit) {
        boolean bad = b.signed ? (b.v < 0 || b.v > limit) : Long.compareUnsigned(b.v, limit) > 0;
        if (bad) {
            Panic.throwPanic(GoErrors.bounds(code, b.v, limit, b.signed));
        }
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Bound l = lo == null ? new Bound(0, true) : readBound(lo, frame);
        switch (containerKind) {
            case SLICE -> {
                GoSlice s = (GoSlice) x.executeObject(frame);
                long len = s == null ? 0 : s.len();
                long cap = s == null ? 0 : s.cap();
                if (max == null) {
                    Bound h = hi == null ? new Bound(len, true) : readBound(hi, frame);
                    if (hi != null) {
                        checkBound(GoErrors.BoundsCode.SLICE_ACAP, h, cap);
                    }
                    checkBound(GoErrors.BoundsCode.SLICE_B, l, h.v);
                    if (s == null) {
                        return null; // s[0:0] on a nil slice is legal (stays nil)
                    }
                    return new GoSlice(s.array(), s.offset() + (int) l.v,
                            (int) (h.v - l.v), (int) (cap - l.v));
                }
                Bound m = readBound(max, frame);
                checkBound(GoErrors.BoundsCode.SLICE3_ACAP, m, cap);
                Bound h = readBound(hi, frame);
                checkBound(GoErrors.BoundsCode.SLICE3_B, h, m.v);
                checkBound(GoErrors.BoundsCode.SLICE3_C, l, h.v);
                if (s == null) {
                    return null; // s[0:0:0] on a nil slice is legal (stays nil)
                }
                return new GoSlice(s.array(), s.offset() + (int) l.v,
                        (int) (h.v - l.v), (int) (m.v - l.v));
            }
            case ARRAY -> {
                Object arr = x.executeObject(frame);
                long n = java.lang.reflect.Array.getLength(arr);
                if (max == null) {
                    Bound h = hi == null ? new Bound(n, true) : readBound(hi, frame);
                    if (hi != null) {
                        checkBound(GoErrors.BoundsCode.SLICE_ALEN, h, n);
                    }
                    checkBound(GoErrors.BoundsCode.SLICE_B, l, h.v);
                    return new GoSlice(arr, (int) l.v, (int) (h.v - l.v), (int) (n - l.v));
                }
                Bound m = readBound(max, frame);
                checkBound(GoErrors.BoundsCode.SLICE3_ALEN, m, n);
                Bound h = readBound(hi, frame);
                checkBound(GoErrors.BoundsCode.SLICE3_B, h, m.v);
                checkBound(GoErrors.BoundsCode.SLICE3_C, l, h.v);
                return new GoSlice(arr, (int) l.v, (int) (h.v - l.v), (int) (m.v - l.v));
            }
            case STRING -> {
                byte[] bytes = ((GoString) x.executeObject(frame)).bytes();
                if (max != null) {
                    throw new FrontendException("3-index slice of a string (WS E)");
                }
                Bound h = hi == null ? new Bound(bytes.length, true) : readBound(hi, frame);
                if (hi != null) {
                    checkBound(GoErrors.BoundsCode.SLICE_ALEN, h, bytes.length);
                }
                checkBound(GoErrors.BoundsCode.SLICE_B, l, h.v);
                byte[] sub = new byte[(int) (h.v - l.v)];
                System.arraycopy(bytes, (int) l.v, sub, 0, (int) (h.v - l.v));
                return new GoString(sub);
            }
            default -> throw new FrontendException("slice of kind " + containerKind + " (WS E)");
        }
    }
}
