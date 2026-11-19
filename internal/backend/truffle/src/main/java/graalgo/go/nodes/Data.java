package graalgo.go.nodes;

import com.oracle.truffle.api.CompilerDirectives.CompilationFinal;
import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.ExplodeLoop;
import com.oracle.truffle.api.profiles.BranchProfile;
import graalgo.go.nodes.Mem.Loc;
import graalgo.go.nodes.N.Expr;
import graalgo.go.nodes.N.GoNode;
import graalgo.go.nodes.N.I64;
import graalgo.go.nodes.N.Ref;
import graalgo.go.nodes.N.Stmt;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.Panics;
import graalgo.go.runtime.Strs;
import graalgo.go.runtime.Values;
import graalgo.go.types.GoType;
import graalgo.go.values.Flat;
import graalgo.go.values.GoMap;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;
import graalgo.runtime.GoErrors.BoundsCode;
import graalgo.runtime.Intrinsics;
import java.util.Arrays;

/**
 * String, slice, array, map and composite-literal nodes and the data builtins
 * ({@code len cap append copy clear delete make new print}). Element layouts (slot counts per
 * element) are node constants chosen by the factory; nothing here tests a Java array class.
 * Allocations on fast paths are the Go-visible objects only: slice headers, backing stores,
 * strings, literals.
 */
public final class Data {
    private Data() {}

    // ---------------------------------------------------------------- strings

    public static final class StrLen extends I64 {
        @Child private Expr s;

        public StrLen(Expr s) {
            this.s = s;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return ((Str) s.executeGeneric(frame)).len;
        }
    }

    public static final class StrIndex extends I64 {
        @Child private Expr s;
        @Child private I64 index;
        private final BranchProfile outOfRange = BranchProfile.create();
        private final boolean signed;

        public StrIndex(Expr s, I64 index, boolean signed) {
            this.s = s;
            this.index = index;
            this.signed = signed;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            Str str = (Str) s.executeGeneric(frame);
            long i = index.executeLong(frame);
            if (Long.compareUnsigned(i, str.len) >= 0) {
                outOfRange.enter();
                throw Panics.index(this, i, str.len, signed);
            }
            return str.b[str.off + (int) i] & 0xFF;
        }
    }

    /** The rune at a byte index, packed as {@code (width << 32) | rune} (range over string). */
    public static final class StrDecode extends I64 {
        @Child private Expr s;
        @Child private I64 index;

        public StrDecode(Expr s, I64 index) {
            this.s = s;
            this.index = index;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            Str str = (Str) s.executeGeneric(frame);
            return Strs.decode(str.b, str.off + (int) index.executeLong(frame), str.off + str.len);
        }
    }

    public static final class StrSlice extends Ref {
        @Child private Expr s;
        @Child private I64 lo;
        @Child private I64 hi;
        private final BranchProfile bad = BranchProfile.create();
        private final int unsigned;

        /** {@code unsigned}: {@link #UNSIGNED_LO}/{@link #UNSIGNED_HI} for unsigned index types. */
        public StrSlice(Expr s, I64 lo, I64 hi, int unsigned) {
            this.s = s;
            this.lo = lo;
            this.hi = hi;
            this.unsigned = unsigned;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Str str = (Str) s.executeGeneric(frame);
            long l = lo == null ? 0 : lo.executeLong(frame);
            long h = hi == null ? str.len : hi.executeLong(frame);
            if (Long.compareUnsigned(h, str.len) > 0) {
                bad.enter();
                throw Panics.bounds(this, BoundsCode.SLICE_ALEN, h, str.len, (unsigned & UNSIGNED_HI) == 0);
            } else if (Long.compareUnsigned(l, h) > 0) {
                bad.enter();
                throw Panics.bounds(this, BoundsCode.SLICE_B, l, h, (unsigned & UNSIGNED_LO) == 0);
            }
            return Strs.slice(str, (int) l, (int) h);
        }
    }

    /** The string conversions; {@code kind} selects the helper at construction. */
    public abstract static class StrConv extends Ref {
        @Child protected Expr x;

        StrConv(Expr x) {
            this.x = x;
        }
    }

    public static final class RuneToStr extends StrConv {
        public RuneToStr(Expr x) {
            super(x);
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return Strs.fromRune((long) x.executeGeneric(frame));
        }
    }

    public static final class BytesToStr extends StrConv {
        public BytesToStr(Expr x) {
            super(x);
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return Strs.fromBytes((Slice) x.executeGeneric(frame));
        }
    }

    public static final class RunesToStr extends StrConv {
        public RunesToStr(Expr x) {
            super(x);
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return Strs.fromRunes((Slice) x.executeGeneric(frame));
        }
    }

    public static final class StrToBytes extends StrConv {
        public StrToBytes(Expr x) {
            super(x);
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return Strs.toBytes((Str) x.executeGeneric(frame));
        }
    }

    public static final class StrToRunes extends StrConv {
        public StrToRunes(Expr x) {
            super(x);
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return Strs.toRunes((Str) x.executeGeneric(frame));
        }
    }

    // ---------------------------------------------------------------- slices

    public static final class SliceLen extends I64 {
        @Child private Expr s;

        public SliceLen(Expr s) {
            this.s = s;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return ((Slice) s.executeGeneric(frame)).len;
        }
    }

    public static final class SliceCap extends I64 {
        @Child private Expr s;

        public SliceCap(Expr s) {
            this.s = s;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return ((Slice) s.executeGeneric(frame)).cap;
        }
    }

    /** {@code s[lo:hi:max]} on a slice. */
    public static final class SliceOfSlice extends Ref {
        @Child private Expr s;
        @Child private I64 lo;
        @Child private I64 hi;
        @Child private I64 max;
        private final int np;
        private final int nr;
        private final int nb;
        private final BranchProfile bad = BranchProfile.create();
        private final int unsigned;

        /** {@code unsigned}: the {@code UNSIGNED_*} bits of the index operands. */
        public SliceOfSlice(Expr s, I64 lo, I64 hi, I64 max, GoType elem, int unsigned) {
            this.unsigned = unsigned;
            this.s = s;
            this.lo = lo;
            this.hi = hi;
            this.max = max;
            this.np = elem.nPrim();
            this.nr = elem.nRef();
            this.nb = elem.nByte();
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Slice sl = (Slice) s.executeGeneric(frame);
            long l = lo == null ? 0 : lo.executeLong(frame);
            long h = hi == null ? sl.len : hi.executeLong(frame);
            long m = max == null ? sl.cap : max.executeLong(frame);
            if (Long.compareUnsigned(m, sl.cap) > 0 || Long.compareUnsigned(h, m) > 0
                    || Long.compareUnsigned(l, h) > 0) {
                bad.enter();
                throw boundsPanic(this, max != null, l, h, m, sl.cap, true, unsigned);
            }
            return new Slice(sl.base, sl.prim + (int) l * np, sl.ref + (int) l * nr, sl.byt + (int) l * nb,
                    (int) (h - l), (int) (m - l));
        }
    }

    /** {@code a[lo:hi:max]} on an addressable array (or through a pointer to one). */
    public static final class SliceOfArray extends Ref {
        @Child private Loc array;
        @Child private I64 lo;
        @Child private I64 hi;
        @Child private I64 max;
        private final long len;
        private final int np;
        private final int nr;
        private final int nb;
        private final BranchProfile bad = BranchProfile.create();
        private final int unsigned;

        /** {@code unsigned}: the {@code UNSIGNED_*} bits of the index operands. */
        public SliceOfArray(Loc array, long len, I64 lo, I64 hi, I64 max, GoType elem, int unsigned) {
            this.unsigned = unsigned;
            this.array = array;
            this.len = len;
            this.lo = lo;
            this.hi = hi;
            this.max = max;
            this.np = elem.nPrim();
            this.nr = elem.nRef();
            this.nb = elem.nByte();
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Flat o = array.resolve(frame);
            int bp = array.prim(frame);
            int br = array.ref(frame);
            int bb = array.byt(frame);
            long l = lo == null ? 0 : lo.executeLong(frame);
            long h = hi == null ? len : hi.executeLong(frame);
            long m = max == null ? len : max.executeLong(frame);
            if (Long.compareUnsigned(m, len) > 0 || Long.compareUnsigned(h, m) > 0 || Long.compareUnsigned(l, h) > 0) {
                bad.enter();
                throw boundsPanic(this, max != null, l, h, m, len, false, unsigned);
            }
            return new Slice(o, bp + (int) l * np, br + (int) l * nr, bb + (int) l * nb, (int) (h - l), (int) (m - l));
        }
    }

    /** Slice-expression index operands of an unsigned type (the bounds panic prints them unsigned). */
    public static final int UNSIGNED_LO = 1;
    public static final int UNSIGNED_HI = 2;
    public static final int UNSIGNED_MAX = 4;

    @TruffleBoundary
    static RuntimeException boundsPanic(GoNode n, boolean three, long l, long h, long m, long cap, boolean slice,
            int unsigned) {
        boolean sl = (unsigned & UNSIGNED_LO) == 0;
        boolean sh = (unsigned & UNSIGNED_HI) == 0;
        boolean sm = (unsigned & UNSIGNED_MAX) == 0;
        if (three) {
            if (Long.compareUnsigned(m, cap) > 0) {
                return Panics.bounds(n, slice ? BoundsCode.SLICE3_ACAP : BoundsCode.SLICE3_ALEN, m, cap, sm);
            } else if (Long.compareUnsigned(h, m) > 0) {
                return Panics.bounds(n, BoundsCode.SLICE3_B, h, m, sh);
            }
            return Panics.bounds(n, BoundsCode.SLICE3_C, l, h, sl);
        }
        if (Long.compareUnsigned(h, cap) > 0) {
            return Panics.bounds(n, slice ? BoundsCode.SLICE_ACAP : BoundsCode.SLICE_ALEN, h, cap, sh);
        }
        return Panics.bounds(n, BoundsCode.SLICE_B, l, h, sl);
    }

    /** {@code make([]T, len, cap)}. */
    public static final class MakeSlice extends Ref {
        @Child private I64 len;
        @Child private I64 cap;
        private final int np;
        private final int nr;
        private final int nb;
        @CompilationFinal(dimensions = 1) private final Object[] zeroElem;
        private final long maxElems;
        private final BranchProfile bad = BranchProfile.create();

        public MakeSlice(GoType elem, I64 len, I64 cap) {
            this.len = len;
            this.cap = cap;
            this.np = elem.nPrim();
            this.nr = elem.nRef();
            this.nb = elem.nByte();
            this.zeroElem = elem.zeroRefsAreNull() ? null : elem.zeroRefs();
            this.maxElems = maxElems(np, nr, nb);
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            long n = len.executeLong(frame);
            long c = cap == null ? n : cap.executeLong(frame);
            // gc makeslice: the cap check first, reported as a len error when len alone fails.
            // The JVM bound: every backing array holds count * slots-per-element entries.
            if (n < 0 || n > maxElems) {
                bad.enter();
                throw Panics.error(this, "makeslice: len out of range");
            } else if (c < n || c > maxElems) {
                bad.enter();
                throw Panics.error(this, "makeslice: cap out of range");
            }
            return new Slice(backing((int) c, np, nr, nb, zeroElem), 0, 0, 0, (int) n, (int) c);
        }
    }

    /** The largest Java array, conservatively (the VM reserves a few header words). */
    static final int MAX_ARRAY = Integer.MAX_VALUE - 8;

    /**
     * The most elements a backing store of {@code np} scalar, {@code nr} reference and
     * {@code nb} byte slots per element can hold: every array must stay below
     * {@link #MAX_ARRAY} (an element count times slots that overflows int would otherwise wrap
     * to a short array).
     */
    static long maxElems(int np, int nr, int nb) {
        int k = Math.max(np, Math.max(nr, nb));
        return k == 0 ? MAX_ARRAY : MAX_ARRAY / k;
    }

    /** A zeroed backing store for {@code cap} elements. */
    static Flat backing(int cap, int np, int nr, int nb, Object[] zeroElem) {
        Object[] refs = nr == 0 ? Flat.NO_REFS : new Object[cap * nr];
        if (zeroElem != null) {
            fillZero(refs, 0, cap, zeroElem);
        }
        return new Flat(null, np == 0 ? Flat.NO_PRIMS : new long[cap * np], refs,
                nb == 0 ? Flat.NO_BYTES : new byte[cap * nb]);
    }

    private static void fillZero(Object[] refs, int from, int to, Object[] zeroElem) {
        int nr = zeroElem.length;
        if (nr == 1) {
            Arrays.fill(refs, from, to, zeroElem[0]);
        } else {
            for (int i = from; i < to; i++) {
                System.arraycopy(zeroElem, 0, refs, i * nr, nr);
            }
        }
    }

    /** One field or element initializer of a composite literal. */
    public abstract static class Init extends GoNode {
        public abstract void init(VirtualFrame frame, Flat o);
    }

    public static final class InitPrim extends Init {
        private final int off;
        @Child private I64 value;

        public InitPrim(int off, I64 value) {
            this.off = off;
            this.value = value;
        }

        @Override
        public void init(VirtualFrame frame, Flat o) {
            o.prims[off] = value.executeLong(frame);
        }
    }

    public static final class InitByte extends Init {
        private final int off;
        @Child private I64 value;

        public InitByte(int off, I64 value) {
            this.off = off;
            this.value = value;
        }

        @Override
        public void init(VirtualFrame frame, Flat o) {
            o.bytes[off] = (byte) value.executeLong(frame);
        }
    }

    public static final class InitRef extends Init {
        private final int off;
        @Child private Expr value;

        public InitRef(int off, Expr value) {
            this.off = off;
            this.value = value;
        }

        @Override
        public void init(VirtualFrame frame, Flat o) {
            o.refs[off] = value.executeGeneric(frame);
        }
    }

    public static final class InitSub extends Init {
        private final int p;
        private final int r;
        private final int b;
        @Child private Expr value;

        public InitSub(int p, int r, int b, Expr value) {
            this.p = p;
            this.r = r;
            this.b = b;
            this.value = value;
        }

        @Override
        public void init(VirtualFrame frame, Flat o) {
            o.store(p, r, b, (Flat) value.executeGeneric(frame));
        }
    }

    /** A struct or array literal: a fresh zero value, then the initializers in source order. */
    public static final class FlatLit extends Ref {
        private final GoType type;
        private final int np;
        private final int nb;
        @CompilationFinal(dimensions = 1) private final Object[] zeroRefs;
        @Children private final Init[] inits;

        public FlatLit(GoType type, Init[] inits) {
            this.type = type;
            this.np = type.nPrim();
            this.nb = type.nByte();
            this.zeroRefs = type.zeroRefs();
            this.inits = inits;
        }

        @Override
        @ExplodeLoop
        public Object executeGeneric(VirtualFrame frame) {
            Flat o = new Flat(type, np == 0 ? Flat.NO_PRIMS : new long[np],
                    zeroRefs.length == 0 ? Flat.NO_REFS : zeroRefs.clone(), nb == 0 ? Flat.NO_BYTES : new byte[nb]);
            for (Init i : inits) {
                i.init(frame, o);
            }
            return o;
        }
    }

    /** A slice literal: backing store of {@code len} elements plus the header. */
    public static final class SliceLit extends Ref {
        private final int len;
        private final int np;
        private final int nr;
        private final int nb;
        @CompilationFinal(dimensions = 1) private final Object[] zeroElem;
        @Children private final Init[] inits;

        public SliceLit(GoType elem, int len, Init[] inits) {
            this.len = len;
            this.np = elem.nPrim();
            this.nr = elem.nRef();
            this.nb = elem.nByte();
            this.zeroElem = elem.zeroRefsAreNull() ? null : elem.zeroRefs();
            this.inits = inits;
        }

        @Override
        @ExplodeLoop
        public Object executeGeneric(VirtualFrame frame) {
            Flat o = backing(len, np, nr, nb, zeroElem);
            for (Init i : inits) {
                i.init(frame, o);
            }
            return new Slice(o, 0, 0, 0, len, len);
        }
    }

    // ---------------------------------------------------------------- append / copy / clear

    /** Room for {@code add} more elements: the slice itself or a grown copy (gc growslice caps). */
    static Slice reserve(Slice s, int add, int np, int nr, int nb, long elemSize, boolean pointers,
            Object[] zeroElem) {
        int need = s.len + add;
        if (need <= s.cap) {
            return s;
        }
        return grow(s, need, np, nr, nb, elemSize, pointers, zeroElem);
    }

    @TruffleBoundary
    private static Slice grow(Slice s, int need, int np, int nr, int nb, long elemSize, boolean pointers,
            Object[] zeroElem) {
        if (need < 0 || need > maxElems(np, nr, nb)) {
            throw new OutOfMemoryError("growslice: len out of range");
        }
        int cap = elemSize == 0 ? need : Intrinsics.nextSliceCap(need, s.cap, elemSize, pointers);
        if (cap > maxElems(np, nr, nb)) {
            cap = need; // gc's rounding up past the JVM bound: the exact need still fits
        }
        long[] prims = np == 0 ? Flat.NO_PRIMS : new long[cap * np];
        Object[] refs = nr == 0 ? Flat.NO_REFS : new Object[cap * nr];
        byte[] bytes = nb == 0 ? Flat.NO_BYTES : new byte[cap * nb];
        if (s.len > 0) {
            System.arraycopy(s.base.prims, s.prim, prims, 0, s.len * np);
            System.arraycopy(s.base.refs, s.ref, refs, 0, s.len * nr);
            System.arraycopy(s.base.bytes, s.byt, bytes, 0, s.len * nb);
        }
        if (zeroElem != null) {
            fillZero(refs, s.len, cap, zeroElem);
        }
        return new Slice(new Flat(null, prims, refs, bytes), 0, 0, 0, s.len, cap);
    }

    /** Shared configuration of the append nodes. */
    public abstract static class AppendBase extends Ref {
        @Child protected Expr slice;
        protected final int np;
        protected final int nr;
        protected final int nb;
        protected final long elemSize;
        protected final boolean pointers;
        @CompilationFinal(dimensions = 1) protected final Object[] zeroElem;

        AppendBase(Expr slice, GoType elem) {
            this.slice = slice;
            this.np = elem.nPrim();
            this.nr = elem.nRef();
            this.nb = elem.nByte();
            this.elemSize = elem.gcSize();
            this.pointers = elem.hasPointers();
            this.zeroElem = elem.zeroRefsAreNull() ? null : elem.zeroRefs();
        }

        protected final Slice room(Slice s, int add) {
            return reserve(s, add, np, nr, nb, elemSize, pointers, zeroElem);
        }

        protected final Slice extended(Slice s, int add) {
            return new Slice(s.base, s.prim, s.ref, s.byt, s.len + add, s.cap);
        }
    }

    /** {@code append(s, v...)} for scalar element types (raw slot values; a byte leaf's low byte). */
    public static final class AppendPrim extends AppendBase {
        @Children private final I64[] values;
        private final boolean byteElem;

        public AppendPrim(Expr slice, GoType elem, I64[] values) {
            super(slice, elem);
            this.values = values;
            this.byteElem = elem.isByteLeaf();
        }

        @Override
        @ExplodeLoop
        public Object executeGeneric(VirtualFrame frame) {
            Slice s = (Slice) slice.executeGeneric(frame);
            long[] v = new long[values.length];
            for (int i = 0; i < values.length; i++) {
                v[i] = values[i].executeLong(frame);
            }
            s = room(s, values.length);
            if (byteElem) {
                byte[] bytes = s.base.bytes;
                for (int i = 0; i < values.length; i++) {
                    bytes[s.byt + s.len + i] = (byte) v[i];
                }
            } else {
                long[] prims = s.base.prims;
                for (int i = 0; i < values.length; i++) {
                    prims[s.prim + s.len + i] = v[i];
                }
            }
            return extended(s, values.length);
        }
    }

    /** {@code append(s, v...)} for single-reference element types. */
    public static final class AppendRef extends AppendBase {
        @Children private final Expr[] values;

        public AppendRef(Expr slice, GoType elem, Expr[] values) {
            super(slice, elem);
            this.values = values;
        }

        @Override
        @ExplodeLoop
        public Object executeGeneric(VirtualFrame frame) {
            Slice s = (Slice) slice.executeGeneric(frame);
            Object[] v = new Object[values.length];
            for (int i = 0; i < values.length; i++) {
                v[i] = values[i].executeGeneric(frame);
            }
            s = room(s, values.length);
            Object[] refs = s.base.refs;
            for (int i = 0; i < values.length; i++) {
                refs[s.ref + s.len + i] = v[i];
            }
            return extended(s, values.length);
        }
    }

    /** {@code append(s, v...)} for struct and array element types. */
    public static final class AppendFlat extends AppendBase {
        @Children private final Expr[] values;

        public AppendFlat(Expr slice, GoType elem, Expr[] values) {
            super(slice, elem);
            this.values = values;
        }

        @Override
        @ExplodeLoop
        public Object executeGeneric(VirtualFrame frame) {
            Slice s = (Slice) slice.executeGeneric(frame);
            Object[] v = new Object[values.length];
            for (int i = 0; i < values.length; i++) {
                v[i] = values[i].executeGeneric(frame);
            }
            s = room(s, values.length);
            for (int i = 0; i < values.length; i++) {
                s.base.store(s.prim + (s.len + i) * np, s.ref + (s.len + i) * nr, s.byt + (s.len + i) * nb,
                        (Flat) v[i]);
            }
            return extended(s, values.length);
        }
    }

    /** {@code append(s, t...)}. */
    public static final class AppendSlice extends AppendBase {
        @Child private Expr tail;

        public AppendSlice(Expr slice, GoType elem, Expr tail) {
            super(slice, elem);
            this.tail = tail;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Slice s = (Slice) slice.executeGeneric(frame);
            Slice t = (Slice) tail.executeGeneric(frame);
            if (t.len == 0) {
                return s;
            }
            Slice d = room(s, t.len);
            System.arraycopy(t.base.prims, t.prim, d.base.prims, d.prim + d.len * np, t.len * np);
            System.arraycopy(t.base.refs, t.ref, d.base.refs, d.ref + d.len * nr, t.len * nr);
            System.arraycopy(t.base.bytes, t.byt, d.base.bytes, d.byt + d.len * nb, t.len * nb);
            return extended(d, t.len);
        }
    }

    /** {@code append([]byte, string...)}. */
    public static final class AppendStr extends AppendBase {
        @Child private Expr tail;

        public AppendStr(Expr slice, GoType elem, Expr tail) {
            super(slice, elem);
            this.tail = tail;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Slice s = (Slice) slice.executeGeneric(frame);
            Str t = (Str) tail.executeGeneric(frame);
            if (t.len == 0) {
                return s;
            }
            Slice d = room(s, t.len);
            System.arraycopy(t.b, t.off, d.base.bytes, d.byt + d.len, t.len);
            return extended(d, t.len);
        }
    }

    /** {@code copy(dst, src)} on slices (overlap-safe: arraycopy). */
    public static final class CopySlice extends I64 {
        @Child private Expr dst;
        @Child private Expr src;
        private final int np;
        private final int nr;
        private final int nb;

        public CopySlice(Expr dst, Expr src, GoType elem) {
            this.dst = dst;
            this.src = src;
            this.np = elem.nPrim();
            this.nr = elem.nRef();
            this.nb = elem.nByte();
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            Slice d = (Slice) dst.executeGeneric(frame);
            Slice s = (Slice) src.executeGeneric(frame);
            int n = Math.min(d.len, s.len);
            if (n > 0) {
                System.arraycopy(s.base.prims, s.prim, d.base.prims, d.prim, n * np);
                System.arraycopy(s.base.refs, s.ref, d.base.refs, d.ref, n * nr);
                System.arraycopy(s.base.bytes, s.byt, d.base.bytes, d.byt, n * nb);
            }
            return n;
        }
    }

    /** {@code copy([]byte, string)}. */
    public static final class CopyStr extends I64 {
        @Child private Expr dst;
        @Child private Expr src;

        public CopyStr(Expr dst, Expr src) {
            this.dst = dst;
            this.src = src;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            Slice d = (Slice) dst.executeGeneric(frame);
            Str s = (Str) src.executeGeneric(frame);
            int n = Math.min(d.len, s.len);
            if (n > 0) {
                System.arraycopy(s.b, s.off, d.base.bytes, d.byt, n);
            }
            return n;
        }
    }

    public static final class ClearSlice extends Stmt {
        @Child private Expr slice;
        private final int np;
        private final int nr;
        private final int nb;
        @CompilationFinal(dimensions = 1) private final Object[] zeroElem;

        public ClearSlice(Expr slice, GoType elem) {
            this.slice = slice;
            this.np = elem.nPrim();
            this.nr = elem.nRef();
            this.nb = elem.nByte();
            this.zeroElem = elem.zeroRefsAreNull() ? null : elem.zeroRefs();
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            Slice s = (Slice) slice.executeGeneric(frame);
            if (s.len == 0) {
                return;
            }
            Arrays.fill(s.base.prims, s.prim, s.prim + s.len * np, 0);
            Arrays.fill(s.base.bytes, s.byt, s.byt + s.len * nb, (byte) 0);
            if (zeroElem == null) {
                Arrays.fill(s.base.refs, s.ref, s.ref + s.len * nr, null);
            } else {
                for (int i = 0; i < s.len; i++) {
                    System.arraycopy(zeroElem, 0, s.base.refs, s.ref + i * nr, nr);
                }
            }
        }
    }

    // ---------------------------------------------------------------- maps

    public static final class MakeMap extends Ref {
        private final GoType key;
        private final GoType val;
        @Child private I64 hint;

        public MakeMap(GoType key, GoType val, I64 hint) {
            this.key = key;
            this.val = val;
            this.hint = hint;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            long h = hint == null ? 0 : hint.executeLong(frame);
            return create(h);
        }

        @TruffleBoundary
        private GoMap create(long h) {
            return new GoMap(key, val, (int) Math.max(0, Math.min(h, 1 << 20)));
        }
    }

    public static final class MapLen extends I64 {
        @Child private Expr m;

        public MapLen(Expr m) {
            this.m = m;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            GoMap map = (GoMap) m.executeGeneric(frame);
            return map == null ? 0 : map.len();
        }
    }

    /** {@code delete(m, k)}, integer/boolean keys. */
    public static final class MapDeleteI extends Stmt {
        @Child private Expr m;
        @Child private I64 key;

        public MapDeleteI(Expr m, I64 key) {
            this.m = m;
            this.key = key;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            GoMap map = (GoMap) m.executeGeneric(frame);
            long k = key.executeLong(frame);
            if (map != null) {
                map.delete(map.find(k));
            }
        }
    }

    public static final class MapDeleteG extends Stmt {
        @Child private Expr m;
        @Child private Expr key;

        public MapDeleteG(Expr m, Expr key) {
            this.m = m;
            this.key = key;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            GoMap map = (GoMap) m.executeGeneric(frame);
            Object k = key.executeGeneric(frame);
            if (map != null) {
                try {
                    map.delete(map.find(k));
                } catch (Values.Uncomparable u) {
                    throw Panics.unhashable(this, u.type);
                }
            }
        }
    }

    public static final class ClearMap extends Stmt {
        @Child private Expr m;

        public ClearMap(Expr m) {
            this.m = m;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            GoMap map = (GoMap) m.executeGeneric(frame);
            if (map != null) {
                map.clear();
            }
        }
    }

    /** {@code v, ok := m[k]}: the pair as a tuple (slow path: boxed, behind a boundary). */
    public static final class MapGetOk extends Ref {
        @Child private Expr m;
        @Child private Expr key;
        private final GoType val;
        private final boolean primKey;

        public MapGetOk(Expr m, Expr key, GoType val, boolean primKey) {
            this.m = m;
            this.key = key;
            this.val = val;
            this.primKey = primKey;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            GoMap map = (GoMap) m.executeGeneric(frame);
            Object k = key.executeGeneric(frame);
            try {
                return lookup(map, k);
            } catch (Values.Uncomparable u) {
                throw Panics.unhashable(this, u.type);
            }
        }

        @TruffleBoundary
        private Object[] lookup(GoMap map, Object k) {
            int e = -1;
            if (map != null) {
                e = primKey ? map.find(k instanceof Boolean b ? (b ? 1L : 0L) : (long) k) : map.find(k);
            }
            if (e < 0) {
                return new Object[] {val.zero(), false};
            }
            return new Object[] {Values.load(val, map.vals, e * map.vp, e * map.vr, e * map.vb), true};
        }
    }

    /** A new range iterator over a map (nil allowed). */
    public static final class MapIter extends Ref {
        @Child private Expr m;

        public MapIter(Expr m) {
            this.m = m;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return new GoMap.Iter((GoMap) m.executeGeneric(frame));
        }
    }

    /** Advances a map iterator: the next entry index, or -1. */
    public static final class MapNext extends I64 {
        @Child private Expr iter;

        public MapNext(Expr iter) {
            this.iter = iter;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return ((GoMap.Iter) iter.executeGeneric(frame)).next();
        }
    }

    /** The key of a map entry, boxed in carrier form. */
    public static final class MapKey extends Ref {
        @Child private Expr m;
        @Child private I64 entry;

        public MapKey(Expr m, I64 entry) {
            this.m = m;
            this.entry = entry;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            GoMap map = (GoMap) m.executeGeneric(frame);
            return map.keyAt((int) entry.executeLong(frame));
        }
    }

    // ---------------------------------------------------------------- print

    /** {@code print}/{@code println}: formats behind a boundary, writes to the context's stderr. */
    public static final class Print extends Stmt {
        @CompilationFinal(dimensions = 1) private final GoType[] types;
        @Children private final Expr[] args;
        private final boolean newline;

        public Print(GoType[] types, Expr[] args, boolean newline) {
            this.types = types;
            this.args = args;
            this.newline = newline;
        }

        @Override
        @ExplodeLoop
        public void executeVoid(VirtualFrame frame) {
            Object[] values = new Object[args.length];
            for (int i = 0; i < args.length; i++) {
                values[i] = args[i].executeGeneric(frame);
            }
            GoProcess.get(this).writeErr(Values.format(types, values, newline));
        }
    }
}
