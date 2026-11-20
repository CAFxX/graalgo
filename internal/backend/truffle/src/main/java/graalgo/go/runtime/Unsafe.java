package graalgo.go.runtime;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.types.Descriptors;
import graalgo.go.types.GoType;
import graalgo.go.types.GoType.Leaf;
import graalgo.go.values.Complex;
import graalgo.go.values.Flat;
import graalgo.go.values.Iface;
import graalgo.go.values.Ptr;
import graalgo.go.values.RawIface;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;
import graalgo.go.values.View;
import java.util.concurrent.ConcurrentHashMap;

/**
 * The pointer model of SPEC §3.4 / GG-0062 over the flat values (ADR-0015): everything that
 * addresses an object by gc byte offset. A pointer is the referent {@link Flat} (its start), a
 * {@link Ptr} (slot offsets) or a {@link View} (byte offset); {@link #offset} gives the gc byte
 * offset of any of them from the referent type's inverse leaf table.
 *
 * <ul>
 * <li>{@link #convert}: {@code unsafe.Pointer} to {@code *U}, checked — the same typed pointer
 * when {@code U}'s leaves coincide with the referent's leaves at the location, else a view.
 * <li>{@link #loadScalar}/{@link #storeScalar}, {@link #loadRef}/{@link #storeRef},
 * {@link #loadFlat}/{@link #storeFlat}: access through a view. An N-byte scalar is assembled
 * from or spread over the leaves it covers; a pointer word is one reference leaf or a word of a
 * string, slice or interface header; headers are read from the object in the slot and written
 * by <em>replacing</em> it, so no header object is ever mutated (ADR-0015 decision 2).
 * <li>{@code uintptr} conversions through the process's range-allocating {@link Handles}.
 * </ul>
 *
 * <p>Whatever leaves the referent, or needs a word that is not there, is a Go fatal error with
 * gc's {@code checkptr} text ({@link Panics#fatal}): never a Java exception, never silent. All
 * of this is slow-path code behind boundaries; typed loads and stores never come here.
 */
public final class Unsafe {
    private Unsafe() {}

    public static final String INVALID = "checkptr: pointer arithmetic result points to invalid allocation";
    public static final String STRADDLES = "checkptr: converted pointer straddles multiple allocations";
    public static final String BAD_VALUE = "checkptr: pointer arithmetic computed bad pointer value";
    public static final String MISALIGNED = "checkptr: misaligned pointer conversion";
    public static final String SLICE_STRADDLES = "checkptr: unsafe.Slice result straddles multiple allocations";
    public static final String STRING_STRADDLES = "checkptr: unsafe.String result straddles multiple allocations";

    // ---------------------------------------------------------------- addresses

    /** Whether {@code p} is one of the three pointer shapes into a flat object. */
    public static boolean isFlatPointer(Object p) {
        return p instanceof Flat || p instanceof Ptr || p instanceof View;
    }

    /** The object a (non-nil, flat) pointer points into. */
    public static Flat referent(Object p) {
        if (p instanceof Flat f) {
            return f;
        } else if (p instanceof Ptr q) {
            return q.obj();
        }
        return ((View) p).obj();
    }

    private static GoType layout(Flat obj) {
        if (obj.type == null) {
            throw new IllegalStateException("pointer into an object without a layout");
        }
        return obj.type;
    }

    /** gc's byte offset of the location a (non-nil, flat) pointer names, inside its referent. */
    @TruffleBoundary
    public static long offset(Object p) {
        if (p instanceof Flat) {
            return 0;
        } else if (p instanceof Ptr q) {
            return layout(q.obj()).gcOffOfLeaf(q.prim() + q.ref() + q.byt());
        }
        return ((View) p).off();
    }

    /** The size in gc bytes of an object: its type's, or element size times count for a backing store. */
    @TruffleBoundary
    public static long extent(Flat obj) {
        GoType t = layout(obj);
        if (t.kind != GoType.BACKING) {
            return t.gcSize();
        }
        GoType e = t.elem;
        long count = e.nByte() > 0 ? obj.bytes.length / e.nByte()
                : e.nPrim() > 0 ? obj.prims.length / e.nPrim() : e.nRef() > 0 ? obj.refs.length / e.nRef() : 0;
        return count * e.gcSize();
    }

    /** The untyped pointer to byte {@code off} of {@code obj}. */
    public static Object pointer(Flat obj, long off) {
        return off == 0 ? obj : new View(obj, off);
    }

    /** Go pointer equality when a view is involved: same referent, same byte. */
    @TruffleBoundary
    public static boolean sameLocation(Object a, Object b) {
        if (a == null || b == null || !isFlatPointer(a) || !isFlatPointer(b)) {
            return false;
        }
        return referent(a) == referent(b) && offset(a) == offset(b);
    }

    /** A hash consistent with pointer equality over all three shapes. */
    @TruffleBoundary
    public static int hashPointer(Object p) {
        if (p == null) {
            return 0;
        } else if (!(p instanceof Ptr) && !(p instanceof View)) {
            return System.identityHashCode(p);
        }
        long off = offset(p);
        return off == 0 ? System.identityHashCode(referent(p)) : System.identityHashCode(referent(p)) * 31 + Long.hashCode(off);
    }

    // ---------------------------------------------------------------- checked conversion

    private record Key(GoType referent, long off, GoType pointee) {}

    private static final ConcurrentHashMap<Key, Boolean> COINCIDES = new ConcurrentHashMap<>();

    /**
     * {@code (*U)(p)} for an {@code unsafe.Pointer} {@code p} (ADR-0015 decision 1): the typed
     * pointer to the location when {@code U}'s leaves are the referent's leaves from there on,
     * else a view. gc's checkptr conditions are fatal: {@code U} reaching past the referent,
     * a misaligned pointer to a type with pointers.
     */
    @TruffleBoundary
    public static Object convert(Node n, Object p, GoType u) {
        if (p == null || !isFlatPointer(p)) {
            return p; // nil, or a word that is not a pointer into a flat object (a type word)
        }
        return typed(n, referent(p), offset(p), u, p);
    }

    /**
     * {@code (*[N]T)(p)} as the operand of a slice expression: gc's checkptr sizes this
     * conversion by the slice bounds, not by the array type (cmd/compile
     * walkCheckPtrAlignment), so an array that overruns the referent is not an error here. It
     * yields a view; slicing it checks the elements actually taken ({@link #sliceOver}).
     */
    @TruffleBoundary
    public static Object convertSliced(Node n, Object p, GoType array) {
        if (p == null || !isFlatPointer(p)) {
            return p;
        }
        Flat obj = referent(p);
        long off = offset(p);
        long ext = extent(obj);
        if (array.u().kind == GoType.ARRAY && off >= 0 && off <= ext && off + array.gcSize() > ext) {
            return p instanceof View ? p : new View(obj, off);
        }
        return typed(n, obj, off, array, p);
    }

    /** The {@code *U} pointer to byte {@code off} of {@code obj}; {@code same}: a pointer already naming it. */
    @TruffleBoundary
    public static Object typed(Node n, Flat obj, long off, GoType u, Object same) {
        long size = u.gcSize();
        long ext = extent(obj);
        if (off < 0 || off > ext || size > 1 && off + size > ext) {
            throw Panics.fatal(n, STRADDLES);
        }
        if (u.hasPointers() && off % u.gcAlign() != 0) {
            throw Panics.fatal(n, MISALIGNED);
        }
        GoType lt = layout(obj);
        // The end of the object (and a one-byte pointee there, which gc's checkptr lets
        // through) is a pointer that addresses no leaf: a view, fatal when dereferenced.
        boolean inside = off + size <= ext && (off < ext || off == 0);
        if (!inside || !coincides(lt, off, u)) {
            return same instanceof View ? same : new View(obj, off);
        }
        if (same != null && !(same instanceof View)) {
            return same;
        }
        if (off == 0) {
            return obj;
        }
        Leaf l = lt.leafAt(off);
        return Ptr.make(obj, l.prim, l.ref, l.byt);
    }

    /** Whether the leaves of {@code u} are the leaves of a {@code lt} object from byte {@code off} on. */
    static boolean coincides(GoType lt, long off, GoType u) {
        if (off == 0 && lt.u() == u.u()) {
            return true;
        }
        long cls = off;
        if (lt.kind == GoType.BACKING && lt.elem.gcSize() > 0) {
            cls = off % lt.elem.gcSize(); // a backing store's layout repeats per element
        }
        Key key = new Key(lt, cls, u);
        Boolean hit = COINCIDES.get(key);
        if (hit == null) {
            hit = coincidesUncached(lt, cls, u);
            COINCIDES.put(key, hit);
        }
        return hit;
    }

    private static boolean coincidesUncached(GoType lt, long off, GoType u) {
        int n = u.nLeaves();
        Leaf first = lt.leafAt(off);
        if (n == 0) {
            // Nothing to address: any position a typed pointer can name.
            return off == 0 || first != null && first.gcOff == off;
        }
        if (first == null || first.gcOff != off) {
            return false;
        }
        for (int j = 0; j < n; j++) {
            long uo = u.gcOffOfLeaf(j);
            Leaf ul = u.leafAt(uo);
            Leaf rl = lt.leafAt(off + uo);
            if (rl == null || rl.gcOff != off + uo || rl.index != first.index + j || !compatible(rl.type, ul.type)) {
                return false;
            }
        }
        return true;
    }

    /**
     * Whether a slot written as leaf type {@code a} reads correctly as leaf type {@code b}:
     * same region and same slot encoding. Bytes are raw; 64-bit scalars are raw (an integer or
     * the bits of a double); narrower scalars are in the canonical extended form of their own
     * kind (and a float32 is a double), so they agree only with themselves.
     */
    static boolean compatible(GoType a, GoType b) {
        a = a.u();
        b = b.u();
        if (a == b) {
            return true;
        }
        if (a.nByte() == 1 || b.nByte() == 1) {
            return a.nByte() == b.nByte();
        } else if (a.nPrim() == 1 || b.nPrim() == 1) {
            return a.nPrim() == b.nPrim() && a.gcSize() == b.gcSize() && (a.gcSize() == 8 || a.kind == b.kind);
        } else if (a.isPointerShaped() || b.isPointerShaped()) {
            return a.isPointerShaped() && b.isPointerShaped();
        } else if (a.kind != b.kind) {
            return false;
        }
        return a.kind != GoType.SLICE || sameLeaves(a.elem, b.elem);
    }

    /** Two element types with one layout: slices of them are interchangeable headers. */
    static boolean sameLeaves(GoType a, GoType b) {
        return a.gcSize() == b.gcSize() && a.nLeaves() == b.nLeaves() && (a.nLeaves() == 0 || coincides(a, 0, b));
    }

    // ---------------------------------------------------------------- uintptr

    /** {@code uintptr(p)}: the referent's range base plus the byte offset (GG-0062). */
    @TruffleBoundary
    public static long toInt(Node n, Object p) {
        if (p == null) {
            return 0;
        }
        Handles h = GoProcess.get(n).handles;
        if (!isFlatPointer(p)) {
            return h.base(p, 0);
        }
        Flat obj = referent(p);
        return h.base(obj, extent(obj)) + offset(p);
    }

    /** {@code unsafe.Pointer(u)}: a floor lookup in the handle table; an integer inside no live range is fatal. */
    @TruffleBoundary
    public static Object fromInt(Node n, long v) {
        if (v == 0) {
            return null;
        }
        Object[] hit = GoProcess.get(n).handles.resolve(v);
        if (hit == null) {
            throw Panics.fatal(n, Long.compareUnsigned(v, 4096) < 0 ? BAD_VALUE : INVALID);
        }
        long off = (long) hit[1];
        if (hit[0] instanceof Flat obj) {
            return pointer(obj, off);
        } else if (off != 0) {
            throw Panics.fatal(n, INVALID);
        }
        return hit[0];
    }

    /** {@code unsafe.Add(p, delta)}: stays inside the referent (its end included) or is fatal. */
    @TruffleBoundary
    public static Object add(Node n, Object p, long delta) {
        if (p == null) {
            return fromInt(n, delta);
        } else if (!isFlatPointer(p)) {
            if (delta != 0) {
                throw Panics.fatal(n, INVALID);
            }
            return p;
        } else if (delta == 0) {
            return p;
        }
        Flat obj = referent(p);
        long off = offset(p) + delta;
        if (off < 0 || off > extent(obj)) {
            throw Panics.fatal(n, INVALID);
        }
        return pointer(obj, off);
    }

    // ---------------------------------------------------------------- scalar access by byte offset

    private static long mask(long bytes) {
        return bytes >= 8 ? -1L : (1L << (8 * bytes)) - 1;
    }

    private static void checkRange(Node n, Flat obj, long off, long size) {
        if (off < 0 || off + size > extent(obj)) {
            throw Panics.fatal(n, INVALID);
        }
    }

    /** The gc word (or narrower scalar) of leaf {@code l} that starts {@code w} bytes into it, as raw bits. */
    private static long chunk(Node n, Flat obj, Leaf l, long w) {
        GoType t = l.type;
        if (t.nByte() == 1) {
            return obj.bytes[l.byt] & 0xFFL;
        } else if (t.nPrim() == 1) {
            long v = obj.prims[l.prim];
            return t.kind == GoType.FLOAT32 ? Float.floatToRawIntBits((float) Double.longBitsToDouble(v)) & 0xFFFFFFFFL
                    : v & mask(t.gcSize());
        }
        Object ref = obj.refs[l.ref];
        switch (t.kind) {
            case GoType.COMPLEX64: {
                Complex c = (Complex) ref;
                return Float.floatToRawIntBits((float) c.re()) & 0xFFFFFFFFL | (long) Float.floatToRawIntBits((float) c.im()) << 32;
            }
            case GoType.COMPLEX128: {
                Complex c = (Complex) ref;
                return Double.doubleToRawLongBits(w == 0 ? c.re() : c.im());
            }
            case GoType.STRING:
                return w == 0 ? toInt(n, strData(n, (Str) ref)) : ((Str) ref).len;
            case GoType.SLICE: {
                Slice s = (Slice) ref;
                return w == 0 ? toInt(n, sliceData(s)) : w == 8 ? s.len() : s.cap();
            }
            case GoType.INTERFACE:
                return toInt(n, w == 0 ? typeWord(ref) : dataWord(n, ref));
            default:
                return toInt(n, ref);
        }
    }

    /** Replaces the chunk {@link #chunk} reads. */
    private static void setChunk(Node n, Flat obj, Leaf l, long w, long bits) {
        GoType t = l.type;
        if (t.nByte() == 1) {
            obj.bytes[l.byt] = (byte) bits;
            return;
        } else if (t.nPrim() == 1) {
            obj.prims[l.prim] = canonical(t, bits);
            return;
        }
        Object ref = obj.refs[l.ref];
        switch (t.kind) {
            case GoType.COMPLEX64:
                obj.refs[l.ref] = new Complex(Float.intBitsToFloat((int) bits), Float.intBitsToFloat((int) (bits >>> 32)));
                break;
            case GoType.COMPLEX128: {
                Complex c = (Complex) ref;
                double d = Double.longBitsToDouble(bits);
                obj.refs[l.ref] = w == 0 ? new Complex(d, c.im()) : new Complex(c.re(), d);
                break;
            }
            case GoType.STRING:
                obj.refs[l.ref] = w == 0 ? strWithData(n, (Str) ref, fromInt(n, bits)) : strWithLen(n, (Str) ref, bits);
                break;
            case GoType.SLICE:
                obj.refs[l.ref] = w == 0 ? sliceWithData(n, (Slice) ref, fromInt(n, bits), t.elem)
                        : sliceWithLenCap(n, (Slice) ref, w == 8 ? bits : ((Slice) ref).len(), w == 8 ? ((Slice) ref).cap() : bits);
                break;
            case GoType.INTERFACE:
                obj.refs[l.ref] = w == 0 ? makeIface(n, fromInt(n, bits), dataWord(n, ref))
                        : makeIface(n, typeWord(ref), fromInt(n, bits));
                break;
            default:
                if (t.kind != GoType.POINTER && t.kind != GoType.UNSAFE_POINTER && bits != 0) {
                    throw Panics.fatal(n, INVALID); // an integer is no map, channel or function
                }
                obj.refs[l.ref] = fromInt(n, bits);
        }
    }

    /** The slot form of raw gc bits for a scalar leaf type (canonical extended integers; a float32 as a double). */
    private static long canonical(GoType t, long bits) {
        switch (t.u().kind) {
            case GoType.INT8:
                return (byte) bits;
            case GoType.BOOL, GoType.UINT8:
                return bits & 0xFF;
            case GoType.INT16:
                return (short) bits;
            case GoType.UINT16:
                return bits & 0xFFFF;
            case GoType.INT32:
                return (int) bits;
            case GoType.UINT32:
                return bits & 0xFFFFFFFFL;
            case GoType.FLOAT32:
                return Double.doubleToRawLongBits(Float.intBitsToFloat((int) bits));
            default:
                return bits;
        }
    }

    /** {@code size} bytes at {@code off}, little-endian; padding reads as zero. */
    private static long loadBits(Node n, Flat obj, long off, long size) {
        checkRange(n, obj, off, size);
        GoType lt = layout(obj);
        long v = 0;
        long pos = off;
        long end = off + size;
        while (pos < end) {
            Leaf l = lt.leafAt(pos);
            if (l == null) {
                pos++;
                continue;
            }
            long w = (pos - l.gcOff) & ~7L;
            long start = l.gcOff + w;
            long take = Math.min(end, start + Math.min(8, l.size() - w)) - pos;
            if (take <= 0) {
                throw new IllegalStateException("leaf table of " + lt.key + " at byte " + pos);
            }
            long bits = chunk(n, obj, l, w) >>> (8 * (pos - start));
            v |= (bits & mask(take)) << (8 * (pos - off));
            pos += take;
        }
        return v;
    }

    /** Spreads {@code size} bytes over the leaves at {@code off}; writes to padding are dropped. */
    private static void storeBits(Node n, Flat obj, long off, long size, long v) {
        checkRange(n, obj, off, size);
        GoType lt = layout(obj);
        long pos = off;
        long end = off + size;
        while (pos < end) {
            Leaf l = lt.leafAt(pos);
            if (l == null) {
                pos++;
                continue;
            }
            long w = (pos - l.gcOff) & ~7L;
            long start = l.gcOff + w;
            long width = Math.min(8, l.size() - w);
            long take = Math.min(end, start + width) - pos;
            if (take <= 0) {
                throw new IllegalStateException("leaf table of " + lt.key + " at byte " + pos);
            }
            long part = v >>> (8 * (pos - off)) & mask(take);
            if (take == width) {
                setChunk(n, obj, l, w, part);
            } else {
                int shift = (int) (8 * (pos - start));
                long old = chunk(n, obj, l, w);
                setChunk(n, obj, l, w, old & ~(mask(take) << shift) | part << shift);
            }
            pos += take;
        }
    }

    /**
     * Copies the {@code len} gc bytes at {@code off} of {@code obj} into {@code dst} (the syscall
     * executor's copy-in, graalgo.syscall): one array copy when they are byte leaves, else leaf by
     * leaf as an {@code unsafe} reinterpretation reads them. A range past the object is the
     * checkptr fatal error.
     */
    @TruffleBoundary
    public static void readBytes(Node n, Flat obj, long off, byte[] dst, int len) {
        if (len == 0) {
            return;
        }
        checkRange(n, obj, off, len);
        Leaf first = byteRun(obj, off, len);
        if (first != null) {
            System.arraycopy(obj.bytes, first.byt, dst, 0, len);
            return;
        }
        for (int i = 0; i < len;) {
            int k = Math.min(8, len - i);
            long v = loadBits(n, obj, off + i, k);
            for (int j = 0; j < k; j++) {
                dst[i + j] = (byte) (v >>> (8 * j));
            }
            i += k;
        }
    }

    /** The inverse of {@link #readBytes}: stores {@code len} bytes of {@code src} at {@code off}. */
    @TruffleBoundary
    public static void writeBytes(Node n, Flat obj, long off, byte[] src, int len) {
        if (len == 0) {
            return;
        }
        checkRange(n, obj, off, len);
        Leaf first = byteRun(obj, off, len);
        if (first != null) {
            System.arraycopy(src, 0, obj.bytes, first.byt, len);
            return;
        }
        for (int i = 0; i < len;) {
            int k = Math.min(8, len - i);
            long v = 0;
            for (int j = 0; j < k; j++) {
                v |= (src[i + j] & 0xFFL) << (8 * j);
            }
            storeBits(n, obj, off + i, k, v);
            i += k;
        }
    }

    /** A scalar of type {@code t} at byte {@code off}, in slot form (what a typed load of the slot yields). */
    @TruffleBoundary
    public static long loadScalar(Node n, Flat obj, long off, GoType t) {
        return canonical(t, loadBits(n, obj, off, t.gcSize()));
    }

    /** Stores a scalar of type {@code t} given in slot form. */
    @TruffleBoundary
    public static void storeScalar(Node n, Flat obj, long off, GoType t, long raw) {
        long bits = t.u().kind == GoType.FLOAT32 ? Float.floatToRawIntBits((float) Double.longBitsToDouble(raw)) : raw;
        storeBits(n, obj, off, t.gcSize(), bits & mask(t.gcSize()));
    }

    // ---------------------------------------------------------------- pointer words

    /** The pointer in the gc word at {@code off}: a reference leaf, a header word, or an integer that resolves. */
    private static Object loadWord(Node n, Flat obj, long off) {
        checkRange(n, obj, off, 8);
        Leaf l = layout(obj).leafAt(off);
        if (l == null) {
            throw Panics.fatal(n, STRADDLES);
        }
        long w = off - l.gcOff;
        GoType t = l.type;
        if (t.nRef() == 0) {
            if (w != 0 || t.gcSize() != 8) {
                throw Panics.fatal(n, STRADDLES);
            }
            return fromInt(n, obj.prims[l.prim]);
        }
        Object ref = obj.refs[l.ref];
        if (t.isPointerShaped() && w == 0) {
            return ref;
        } else if (t.kind == GoType.STRING && w == 0) {
            return strData(n, (Str) ref);
        } else if (t.kind == GoType.SLICE && w == 0) {
            return sliceData((Slice) ref);
        } else if (t.kind == GoType.INTERFACE && (w == 0 || w == 8)) {
            return w == 0 ? typeWord(ref) : dataWord(n, ref);
        }
        throw Panics.fatal(n, STRADDLES);
    }

    private static void storeWord(Node n, Flat obj, long off, Object p) {
        checkRange(n, obj, off, 8);
        Leaf l = layout(obj).leafAt(off);
        if (l == null) {
            throw Panics.fatal(n, STRADDLES);
        }
        long w = off - l.gcOff;
        GoType t = l.type;
        if (t.nRef() == 0) {
            if (w != 0 || t.gcSize() != 8) {
                throw Panics.fatal(n, STRADDLES);
            }
            obj.prims[l.prim] = toInt(n, p);
            return;
        }
        Object ref = obj.refs[l.ref];
        if (t.isPointerShaped() && w == 0) {
            obj.refs[l.ref] = p;
        } else if (t.kind == GoType.STRING && w == 0) {
            obj.refs[l.ref] = strWithData(n, (Str) ref, p);
        } else if (t.kind == GoType.SLICE && w == 0) {
            obj.refs[l.ref] = sliceWithData(n, (Slice) ref, p, t.elem);
        } else if (t.kind == GoType.INTERFACE && (w == 0 || w == 8)) {
            obj.refs[l.ref] = w == 0 ? makeIface(n, p, dataWord(n, ref)) : makeIface(n, typeWord(ref), p);
        } else {
            throw Panics.fatal(n, STRADDLES);
        }
    }

    /** The reference leaf of a kind interchangeable with {@code t} that starts exactly at {@code off}, or null. */
    private static Leaf exactRef(Flat obj, long off, GoType t) {
        Leaf l = layout(obj).leafAt(off);
        return l != null && l.gcOff == off && l.type.nRef() == 1 && compatible(l.type, t) ? l : null;
    }

    /**
     * A value of reference-carrier type {@code t} (pointer-shaped, string, slice, interface,
     * complex) at byte {@code off}: the object in the slot when a leaf of that kind is there,
     * else composed from the gc words found there.
     */
    @TruffleBoundary
    public static Object loadRef(Node n, Flat obj, long off, GoType t) {
        GoType u = t.u();
        checkRange(n, obj, off, u.gcSize());
        Leaf l = exactRef(obj, off, u);
        Object v;
        if (l != null) {
            v = obj.refs[l.ref];
        } else {
            switch (u.kind) {
                case GoType.STRING:
                    v = stringOver(n, loadWord(n, obj, off), loadBits(n, obj, off + 8, 8));
                    break;
                case GoType.SLICE:
                    v = sliceOver(n, loadWord(n, obj, off), loadBits(n, obj, off + 8, 8), loadBits(n, obj, off + 16, 8),
                            u.elem, STRADDLES);
                    break;
                case GoType.INTERFACE:
                    v = makeIface(n, loadWord(n, obj, off), loadWord(n, obj, off + 8));
                    break;
                case GoType.COMPLEX64: {
                    long bits = loadBits(n, obj, off, 8);
                    v = new Complex(Float.intBitsToFloat((int) bits), Float.intBitsToFloat((int) (bits >>> 32)));
                    break;
                }
                case GoType.COMPLEX128:
                    v = new Complex(Double.longBitsToDouble(loadBits(n, obj, off, 8)),
                            Double.longBitsToDouble(loadBits(n, obj, off + 8, 8)));
                    break;
                default:
                    v = loadWord(n, obj, off);
            }
        }
        return u.kind == GoType.POINTER ? convert(n, v, u.elem) : v;
    }

    @TruffleBoundary
    public static void storeRef(Node n, Flat obj, long off, GoType t, Object v) {
        GoType u = t.u();
        checkRange(n, obj, off, u.gcSize());
        Leaf l = exactRef(obj, off, u);
        if (l != null) {
            obj.refs[l.ref] = v;
            return;
        }
        switch (u.kind) {
            case GoType.STRING:
                storeWord(n, obj, off, strData(n, (Str) v));
                storeBits(n, obj, off + 8, 8, ((Str) v).len);
                break;
            case GoType.SLICE: {
                Slice s = (Slice) v;
                storeWord(n, obj, off, sliceData(s));
                storeBits(n, obj, off + 8, 8, s.len());
                storeBits(n, obj, off + 16, 8, s.cap());
                break;
            }
            case GoType.INTERFACE:
                storeWord(n, obj, off, typeWord(v));
                storeWord(n, obj, off + 8, dataWord(n, v));
                break;
            case GoType.COMPLEX64: {
                Complex c = (Complex) v;
                storeBits(n, obj, off, 8, Float.floatToRawIntBits((float) c.re()) & 0xFFFFFFFFL
                        | (long) Float.floatToRawIntBits((float) c.im()) << 32);
                break;
            }
            case GoType.COMPLEX128: {
                Complex c = (Complex) v;
                storeBits(n, obj, off, 8, Double.doubleToRawLongBits(c.re()));
                storeBits(n, obj, off + 8, 8, Double.doubleToRawLongBits(c.im()));
                break;
            }
            default:
                storeWord(n, obj, off, v);
        }
    }

    // ---------------------------------------------------------------- whole values

    /** A struct or array value of type {@code t} read leaf by leaf at byte {@code off}. */
    @TruffleBoundary
    public static Flat loadFlat(Node n, Flat obj, long off, GoType t) {
        checkRange(n, obj, off, t.gcSize());
        Flat out = t.zeroFlat();
        int leaves = t.nLeaves();
        for (int j = 0; j < leaves; j++) {
            long lo = t.gcOffOfLeaf(j);
            Leaf l = t.leafAt(lo);
            if (l.type.nRef() == 1) {
                out.refs[l.ref] = loadRef(n, obj, off + lo, l.type);
            } else if (l.type.nByte() == 1) {
                out.bytes[l.byt] = (byte) loadBits(n, obj, off + lo, 1);
            } else {
                out.prims[l.prim] = loadScalar(n, obj, off + lo, l.type);
            }
        }
        return out;
    }

    @TruffleBoundary
    public static void storeFlat(Node n, Flat obj, long off, GoType t, Flat v) {
        checkRange(n, obj, off, t.gcSize());
        int leaves = t.nLeaves();
        for (int j = 0; j < leaves; j++) {
            long lo = t.gcOffOfLeaf(j);
            Leaf l = t.leafAt(lo);
            if (l.type.nRef() == 1) {
                storeRef(n, obj, off + lo, l.type, v.refs[l.ref]);
            } else if (l.type.nByte() == 1) {
                storeBits(n, obj, off + lo, 1, v.bytes[l.byt] & 0xFFL);
            } else {
                storeScalar(n, obj, off + lo, l.type, v.prims[l.prim]);
            }
        }
    }

    /** A value of any type at byte {@code off}, boxed in carrier form. */
    @TruffleBoundary
    public static Object loadValue(Node n, Flat obj, long off, GoType t) {
        switch (t.carrier()) {
            case GoType.C_I64:
                return loadScalar(n, obj, off, t);
            case GoType.C_F64:
                return Double.longBitsToDouble(loadScalar(n, obj, off, t));
            case GoType.C_BOOL:
                return loadScalar(n, obj, off, t) != 0;
            case GoType.C_REF:
                return loadRef(n, obj, off, t);
            default:
                return loadFlat(n, obj, off, t);
        }
    }

    /** The value of type {@code t} a (non-nil) pointer of any shape points at, boxed in carrier form. */
    @TruffleBoundary
    public static Object loadThrough(Node n, Object p, GoType t) {
        Object q = convert(n, p, t);
        if (q instanceof Flat f) {
            return Values.load(t, f, 0, 0, 0);
        } else if (q instanceof Ptr ptr) {
            return Values.load(t, ptr.obj(), ptr.prim(), ptr.ref(), ptr.byt());
        } else if (q instanceof View v) {
            return loadValue(n, v.obj(), v.off(), t);
        }
        throw Panics.fatal(n, INVALID);
    }

    /** Stores a value of any type, boxed in carrier form, at byte {@code off}. */
    @TruffleBoundary
    public static void storeValue(Node n, Flat obj, long off, GoType t, Object v) {
        switch (t.carrier()) {
            case GoType.C_I64:
                storeScalar(n, obj, off, t, (long) v);
                break;
            case GoType.C_F64:
                storeScalar(n, obj, off, t, Double.doubleToRawLongBits((double) v));
                break;
            case GoType.C_BOOL:
                storeScalar(n, obj, off, t, (boolean) v ? 1 : 0);
                break;
            case GoType.C_REF:
                storeRef(n, obj, off, t, v);
                break;
            default:
                storeFlat(n, obj, off, t, (Flat) v);
        }
    }

    /** Stores a value of type {@code t}, boxed in carrier form, where a (non-nil) pointer of any shape points. */
    @TruffleBoundary
    public static void storeThrough(Node n, Object p, GoType t, Object v) {
        Object q = convert(n, p, t);
        if (q instanceof Flat f) {
            Values.store(t, f, 0, 0, 0, v);
        } else if (q instanceof Ptr ptr) {
            Values.store(t, ptr.obj(), ptr.prim(), ptr.ref(), ptr.byt(), v);
        } else if (q instanceof View w) {
            storeValue(n, w.obj(), w.off(), t, v);
        } else {
            throw Panics.fatal(n, INVALID);
        }
    }

    // ---------------------------------------------------------------- string and slice headers

    /** The pointer to a string's bytes ({@code unsafe.StringData}); nil for a string without storage. */
    @TruffleBoundary
    public static Object strData(Node n, Str s) {
        if (s.b.length == 0) {
            return null;
        }
        Flat f = s.flat;
        if (f == null) {
            // One wrapper per string object: its storage as a backing store of bytes.
            GoType bytes = GoProcess.get(n).types.basic(GoType.UINT8).backing();
            f = new Flat(bytes, Flat.NO_PRIMS, Flat.NO_REFS, s.b);
            s.flat = f;
        }
        return Ptr.make(f, 0, 0, s.off);
    }

    /** The pointer to a slice's element 0 ({@code unsafe.SliceData}); nil for a nil slice. */
    public static Object sliceData(Slice s) {
        return s.base() == null ? null : Ptr.make(s.base(), s.prim(), s.ref(), s.byt());
    }

    /**
     * The first of {@code n} consecutive byte leaves at {@code p}, or null when the bytes there
     * are not a run of byte leaves (n ≥ 1; the range is inside the referent).
     */
    private static Leaf byteRun(Flat obj, long off, long n) {
        GoType lt = layout(obj);
        Leaf first = lt.leafAt(off);
        Leaf last = lt.leafAt(off + n - 1);
        // n leaves in n bytes: every one of them is a byte leaf and there is no padding.
        return first != null && last != null && first.type.nByte() == 1 && last.type.nByte() == 1
                && last.byt - first.byt == n - 1 && last.index - first.index == n - 1 ? first : null;
    }

    /**
     * The string of {@code len} bytes at {@code p} ({@code unsafe.String}; the caller has
     * checked {@code len} and nil): it shares the storage when the bytes are byte leaves, else
     * it is a copy of the bytes found there — a string is immutable either way.
     */
    @TruffleBoundary
    public static Str stringOver(Node n, Object p, long len) {
        if (len == 0) {
            return Str.EMPTY;
        } else if (p == null || !isFlatPointer(p) || len < 0) {
            throw Panics.fatal(n, STRING_STRADDLES);
        }
        Flat obj = referent(p);
        long off = offset(p);
        if (off + len > extent(obj) || len > Integer.MAX_VALUE) {
            throw Panics.fatal(n, STRING_STRADDLES);
        }
        Leaf first = byteRun(obj, off, len);
        if (first != null) {
            Str s = new Str(obj.bytes, first.byt, (int) len);
            s.flat = obj.type.kind == GoType.BACKING && obj.type.elem.isByteLeaf() ? obj : null;
            return s;
        }
        byte[] copy = new byte[(int) len];
        for (int i = 0; i < copy.length; i++) {
            copy[i] = (byte) loadBits(n, obj, off + i, 1);
        }
        return new Str(copy);
    }

    /**
     * The slice of {@code cap} elements of type {@code elem} at {@code p} ({@code unsafe.Slice},
     * a slice header composed from words): the elements must be the referent's own leaves — a
     * slice shares storage — else the fatal error {@code straddles}.
     */
    @TruffleBoundary
    public static Slice sliceOver(Node n, Object p, long len, long cap, GoType elem, String straddles) {
        if (p == null) {
            if (len != 0 || cap != 0) {
                throw Panics.fatal(n, straddles);
            }
            return Slice.NIL;
        } else if (!isFlatPointer(p) || len < 0 || cap < len || cap > Integer.MAX_VALUE) {
            throw Panics.fatal(n, straddles);
        }
        Flat obj = referent(p);
        long off = offset(p);
        long size = elem.gcSize();
        if (off + cap * size > extent(obj)) {
            throw Panics.fatal(n, straddles);
        }
        GoType lt = layout(obj);
        int[] at = null;
        if (cap == 0 || elem.nLeaves() == 0) {
            Leaf l = lt.leafAt(off);
            at = off == 0 ? new int[3] : l != null && l.gcOff == off ? new int[] {l.prim, l.ref, l.byt} : null;
            if (at == null && off == extent(obj) && p instanceof Ptr q) {
                at = new int[] {q.prim(), q.ref(), q.byt()};
            }
        } else if (elem.isByteLeaf()) {
            Leaf first = byteRun(obj, off, cap);
            at = first == null ? null : new int[] {first.prim, first.ref, first.byt};
        } else {
            // Check one element per position of the joint period of the two layouts (a backing
            // store repeats per element), or every element of an object with a layout of its own.
            long count = cap;
            if (lt.kind == GoType.BACKING && lt.elem.gcSize() > 0) {
                long es = lt.elem.gcSize();
                count = Math.min(cap, es / gcd(es, size));
            }
            boolean ok = true;
            for (long i = 0; ok && i < count; i++) {
                ok = coincides(lt, off + i * size, elem);
            }
            if (ok) {
                Leaf l = lt.leafAt(off);
                at = new int[] {l.prim, l.ref, l.byt};
            }
        }
        if (at == null) {
            throw Panics.fatal(n, straddles);
        }
        return new Slice(obj, at[0], at[1], at[2], (int) len, (int) cap);
    }

    private static long gcd(long a, long b) {
        return b == 0 ? a : gcd(b, a % b);
    }

    private static Str strWithData(Node n, Str old, Object p) {
        if (p == null) {
            return Str.EMPTY;
        }
        Flat obj = referent(p);
        long off = offset(p);
        // A header is written word by word: the old length stands as far as the new bytes
        // reach, and a string of length 0 keeps its data pointer for the length to come.
        long len = Math.min(old.len, extent(obj) - off);
        Leaf l = layout(obj).leafAt(off);
        if (l == null || l.type.nByte() != 1 || len > 0 && byteRun(obj, off, len) == null) {
            return stringOver(n, p, len);
        }
        Str s = new Str(obj.bytes, l.byt, (int) len);
        s.flat = obj.type.kind == GoType.BACKING && obj.type.elem.isByteLeaf() ? obj : null;
        return s;
    }

    private static Str strWithLen(Node n, Str old, long len) {
        if (len < 0 || len > old.b.length - old.off) {
            throw Panics.fatal(n, STRING_STRADDLES);
        }
        Str s = new Str(old.b, old.off, (int) len);
        s.flat = old.flat;
        return s;
    }

    /**
     * The slice in a slot of type {@code []elem} after its data word is written. The words of a
     * header are written one at a time and in any order (reflect's {@code Value.Slice} writes
     * the length and capacity of a {@code []unsafe.Pointer} first and then the data pointer of
     * a slice of another element type), so:
     * <ul>
     * <li>a length and capacity written before the data stand ({@link #sliceWithLenCap} keeps
     * them on a slice without storage);
     * <li>when the storage at {@code p} is not made of {@code elem} leaves, the slot holds an
     * <em>untyped header</em> — the location of {@code p} with the length and capacity as
     * written — which names the same data word ({@link #sliceData}) and is read back through
     * the type the reader gives ({@link #loadRef} composes the slice from the words, checked
     * against the storage). Indexing it as {@code []elem} is the misuse it would be under gc.
     * </ul>
     */
    private static Slice sliceWithData(Node n, Slice old, Object p, GoType elem) {
        if (p == null) {
            return old.len() == 0 && old.cap() == 0 ? Slice.NIL : new Slice(null, 0, 0, 0, old.len(), old.cap());
        } else if (!isFlatPointer(p)) {
            throw Panics.fatal(n, STRADDLES);
        }
        Flat obj = referent(p);
        long off = offset(p);
        GoType lt = layout(obj);
        boolean typed = elem.gcSize() == 0 || elem.nLeaves() == 0;
        if (!typed) {
            // One element decides: storage repeats per element (sliceOver checks the rest).
            typed = off + elem.gcSize() <= extent(obj) ? coincides(lt, off, elem) : off == extent(obj);
        }
        if (typed) {
            long avail = elem.gcSize() == 0 ? old.cap() : (extent(obj) - off) / elem.gcSize();
            return sliceOver(n, p, Math.min(old.len(), avail), Math.min(old.cap(), avail), elem, STRADDLES);
        }
        Leaf l = lt.leafAt(off);
        if (l == null || l.gcOff != off) {
            throw Panics.fatal(n, STRADDLES);
        }
        return new Slice(obj, l.prim, l.ref, l.byt, old.len(), old.cap());
    }

    private static Slice sliceWithLenCap(Node n, Slice old, long len, long cap) {
        if (old.base() == null) {
            if (len < 0 || cap < 0 || len > Integer.MAX_VALUE || cap > Integer.MAX_VALUE) {
                throw Panics.fatal(n, STRADDLES);
            }
            // No storage yet: the words stand until the data word arrives (sliceWithData).
            return len == 0 && cap == 0 ? Slice.NIL : new Slice(null, 0, 0, 0, (int) len, (int) cap);
        }
        // The capacity may grow back up to the end of the storage; the length follows it.
        Slice s = new Slice(old.base(), old.prim(), old.ref(), old.byt(), (int) Math.min(len, Integer.MAX_VALUE),
                (int) Math.min(cap, Integer.MAX_VALUE));
        if (len < 0 || cap < 0) {
            throw Panics.fatal(n, STRADDLES);
        }
        return s;
    }

    // ---------------------------------------------------------------- interface headers

    /**
     * Whether {@code t} is a struct or array type that gc stores directly in an interface's
     * data word ({@code abi.TFlagDirectIface}: size == pointer bytes == one word): its one leaf
     * is a pointer word.
     */
    @TruffleBoundary
    public static boolean directFlat(GoType t) {
        return t.carrier() == GoType.C_FLAT && t.gcSize() == 8 && Descriptors.ptrBytes(t) == 8;
    }

    /**
     * The type word of an interface slot: the dynamic type's descriptor image (W9a; the type
     * itself in a program without {@code internal/abi}), nil for a nil interface.
     */
    public static Object typeWord(Object slot) {
        return slot == null ? null : slot instanceof Iface i ? i.type.typeWord() : ((RawIface) slot).typ();
    }

    /**
     * The data word of an interface slot: the pointer itself for a pointer-shaped dynamic type,
     * else a pointer to the value (a struct or array value is its own object; a scalar, string,
     * slice or interface is boxed).
     */
    @TruffleBoundary
    public static Object dataWord(Node n, Object slot) {
        if (slot == null) {
            return null;
        } else if (slot instanceof RawIface r) {
            return r.data();
        }
        Iface i = (Iface) slot;
        if (i.type.isPointerShaped()) {
            return i.data;
        } else if (i.type.carrier() == GoType.C_FLAT) {
            // gc stores a one-pointer-word struct or array (struct{p *T}, [1]func()) directly:
            // its data word is that pointer, not a pointer to the value (abi.TFlagDirectIface).
            return directFlat(i.type) ? ((Flat) i.data).refs[0] : i.data;
        }
        Object w = i.word();
        if (w != null) {
            return w;
        }
        Flat box = i.type.zeroFlat();
        Values.store(i.type, box, 0, 0, 0, i.data);
        return i.cacheWord(box); // one word per value, as gc's (BUG-0045)
    }

    /**
     * Whether the leaves at {@code off} are the gc words of a header of kind {@code u} (slice:
     * pointer, int, int; string: pointer, int) rather than a leaf of that kind.
     */
    private static boolean headerWords(Flat obj, long off, GoType u) {
        int ints = u.kind == GoType.SLICE ? 2 : u.kind == GoType.STRING ? 1 : -1;
        if (ints < 0) {
            return false;
        }
        GoType lt = layout(obj);
        Leaf p = lt.leafAt(off);
        if (p == null || p.gcOff != off || p.type.nRef() != 1 || !p.type.isPointerShaped()) {
            return false;
        }
        for (int i = 1; i <= ints; i++) {
            Leaf l = lt.leafAt(off + 8L * i);
            if (l == null || l.gcOff != off + 8L * i || l.type.nPrim() != 1 || l.type.gcSize() != 8) {
                return false;
            }
        }
        return true;
    }

    /** The interface slot content for a type word and a data word (the inverse of the two above). */
    @TruffleBoundary
    public static Object makeIface(Node n, Object typ, Object data) {
        if (typ == null && data == null) {
            return null;
        }
        GoType t = Descriptors.typeOf(typ);
        if (t == null) {
            return new RawIface(typ, data);
        }
        if (t.u().kind != GoType.POINTER && t.isPointerShaped()) {
            return new Iface(t, data); // a map, channel, function or unsafe.Pointer: the word itself
        }
        if (directFlat(t)) {
            Flat v = t.zeroFlat();
            v.refs[0] = data;
            return new Iface(t, v);
        }
        // The two words are written one at a time, so a type word may meet the data word of
        // the value it replaces. They form an interface value when the data word points at
        // leaves of the type's layout (for a pointer type: of its pointee's); until then the
        // slot holds the words as they are.
        GoType pointee = t.u().kind == GoType.POINTER ? t.u().elem : t;
        if (data == null) {
            return t.u().kind == GoType.POINTER ? new Iface(t, null) : new RawIface(typ, null);
        } else if (!isFlatPointer(data)) {
            return new RawIface(typ, data);
        }
        Flat obj = referent(data);
        long off = offset(data);
        if (off + pointee.gcSize() > extent(obj)) {
            return new RawIface(typ, data);
        } else if (!coincides(layout(obj), off, pointee)) {
            if (t.u().kind != GoType.POINTER && headerWords(obj, off, t.u())) {
                // A slice or string spelled out as its words (reflect.MakeSlice points a
                // []T Value at an unsafeheader.Slice): the value those words compose.
                return new Iface(t, loadRef(n, obj, off, t));
            }
            return new RawIface(typ, data);
        }
        Object p = typed(n, obj, off, pointee, data);
        return new Iface(t, t.u().kind == GoType.POINTER ? p : loadThrough(n, p, t));
    }
}
