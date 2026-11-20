package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.nodes.Data;
import graalgo.go.runtime.Panics;
import graalgo.go.runtime.Unsafe;
import graalgo.go.runtime.Values;
import graalgo.go.types.Descriptors;
import graalgo.go.types.GoType;
import graalgo.go.values.Closure;
import graalgo.go.values.Flat;
import graalgo.go.values.GoMap;
import graalgo.go.values.Iface;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;
import graalgo.runtime.GoChan;

/**
 * What gc's runtime and compiler provide to {@code reflect} (toolexec-design §5.6, W9b;
 * abi-spike.md §1): the functions without a body in upstream's reflect — {@code //go:linkname}
 * pulls of runtime/runtime1.go, mbarrier.go, iface.go, chan.go, map.go, malloc.go and type.go —
 * and the compiler intrinsics of {@code internal/abi} and {@code internal/runtime/sys} it
 * calls; registered in {@code internal/toolexec/intrinsics/registry.txt}. reflect itself is
 * upstream's Go code, reading descriptor images ({@link Descriptors}) through the pointer model.
 *
 * <p>A {@code *abi.Type} argument is a descriptor image; {@link Descriptors#typeOf} maps it back
 * to the type whose layout the operation uses. A map or channel behind an
 * {@code unsafe.Pointer} is the backend's object itself.
 */
final class ReflectIntrinsics {
    private ReflectIntrinsics() {}

    private static GoType type(Node node, Object rtype) {
        GoType t = Descriptors.typeOf(rtype);
        if (t == null) {
            // A descriptor this runtime did not build (reflect's StructOf and friends).
            throw Panics.fatal(node, "type descriptor not built by the runtime");
        }
        return t;
    }

    // ---------------------------------------------------------------- internal/abi, internal/runtime/sys

    /**
     * {@code abi.FuncPCABI0(f)}/{@code FuncPCABIInternal(f)}: the "entry pc" of a function, here
     * the {@code uintptr} handle of its code (the call target, one per function).
     */
    @TruffleBoundary
    static long internal_abi_FuncPCABI0(Node node, Object f) {
        Iface i = (Iface) f;
        if (i == null || !(i.data instanceof Closure c)) {
            return 0;
        }
        return Unsafe.toInt(node, c.target);
    }

    static long internal_abi_FuncPCABIInternal(Node node, Object f) {
        return internal_abi_FuncPCABI0(node, f);
    }

    /** {@code sys.GetCallerPC}: unknown, as {@code runtime.Caller} (docs/arch/limitations.md). */
    static long internal_runtime_sys_GetCallerPC(Node node) {
        return 0;
    }

    // ---------------------------------------------------------------- descriptors

    static Object reflect_resolveNameOff(Node node, Object ptrInModule, long off) {
        return ReflectliteIntrinsics.internal_reflectlite_resolveNameOff(node, ptrInModule, off);
    }

    static Object reflect_resolveTypeOff(Node node, Object rtype, long off) {
        return ReflectliteIntrinsics.internal_reflectlite_resolveTypeOff(node, rtype, off);
    }

    /**
     * {@code resolveTextOff(rtype, off)}: the code pointer of a method ({@code abi.Method.Ifn},
     * {@code Tfn}). Descriptor images carry no text offsets yet (reflect's call path, W9b).
     */
    @TruffleBoundary
    static Object reflect_resolveTextOff(Node node, Object rtype, long off) {
        throw Panics.fatal(node, "reflect: method code pointers are not available (graalgo: reflect call path)");
    }

    /**
     * {@code addReflectOff(ptr)}: registers a name or type made at run time ({@code StructOf},
     * {@code FuncOf}, ...) so that an offset can name it.
     */
    @TruffleBoundary
    static long reflect_addReflectOff(Node node, Object ptr) {
        throw Panics.fatal(node, "reflect: types made at run time are not supported (graalgo)");
    }

    /**
     * {@code compiledTypelinks()}: the sorted type lists reflect searches before it makes a
     * pointer, slice, map, channel or func type itself. None is recorded.
     */
    static Object reflect_compiledTypelinks(Node node) {
        return new Object[] {null, null};
    }

    /** AIX only; never called on the tracked platform. */
    static Object reflect_adjustAIXGCDataForRuntime(Node node, Object p) {
        return p;
    }

    // ---------------------------------------------------------------- memory

    static Object reflect_unsafe_New(Node node, Object rtype) {
        return ReflectliteIntrinsics.internal_reflectlite_unsafe_New(node, rtype);
    }

    static void reflect_typedmemmove(Node node, Object rtype, Object dst, Object src) {
        ReflectliteIntrinsics.internal_reflectlite_typedmemmove(node, rtype, dst, src);
    }

    static void reflect_ifaceE2I(Node node, Object rtype, Object src, Object dst) {
        ReflectliteIntrinsics.internal_reflectlite_ifaceE2I(node, rtype, src, dst);
    }

    /**
     * {@code verifyNotInHeapPtr(p)}: whether {@code p} may be stored in a pointer to a
     * not-in-heap type. No Go pointer names off-heap memory here; nil is the only such value.
     */
    static boolean reflect_verifyNotInHeapPtr(Node node, long p) {
        return p == 0;
    }

    // ---------------------------------------------------------------- channels and maps

    static long reflect_chanlen(Node node, Object ch) {
        return ReflectliteIntrinsics.internal_reflectlite_chanlen(node, ch);
    }

    static long reflect_maplen(Node node, Object m) {
        return ReflectliteIntrinsics.internal_reflectlite_maplen(node, m);
    }

    @TruffleBoundary
    static void reflect_chanclose(Node node, Object ch) {
        if (ch == null) {
            throw Panics.plain(node, "close of nil channel");
        }
        ((GoChan) ch).close();
    }

    /** A fresh variable of type {@code t} holding the carrier value {@code v}; its pointer. */
    private static Flat boxed(GoType t, Object v) {
        Flat box = t.zeroFlat();
        if (t.gcSize() != 0) {
            if (t.carrier() == GoType.C_BOOL && v instanceof Long l) {
                v = l != 0;
            }
            Values.store(t, box, 0, 0, 0, v);
        }
        return box;
    }

    /**
     * {@code internal/runtime/maps.iterNext(t, m, state)} (the facade's iterator step): advances
     * the backend iterator — created on the first call — and returns it with pointers to copies
     * of the entry's key and element, nil at the end. The iteration has a range statement's
     * guarantees (random start, an entry at most once, a deleted one never).
     */
    @TruffleBoundary
    static Object internal_runtime_maps_iterNext(Node node, Object t, Object m, Object state) {
        GoMap map = (GoMap) m;
        GoMap.Iter it = state == null ? new GoMap.Iter(map) : (GoMap.Iter) state;
        int e = it.next();
        if (e < 0) {
            return new Object[] {it, null, null};
        }
        Flat key = boxed(map.keyType, map.keyAt(e));
        Flat elem = boxed(map.valType, Values.load(map.valType, map.vals, e * map.vp, e * map.vr, e * map.vb));
        return new Object[] {it, key, elem};
    }

    // ---------------------------------------------------------------- arrays and slices

    /** {@code unsafe_NewArray(t, n)}: a zeroed backing store of n elements of type t. */
    @TruffleBoundary
    static Object reflect_unsafe_NewArray(Node node, Object rtype, long n) {
        if (n < 0 || n > Integer.MAX_VALUE) {
            throw Panics.plain(node, "reflect: array size out of range");
        }
        return Data.backing(type(node, rtype), (int) n);
    }

    /** {@code typedmemclr(t, ptr)}: {@code *ptr = zero value of t}. */
    @TruffleBoundary
    static void reflect_typedmemclr(Node node, Object rtype, Object ptr) {
        GoType t = type(node, rtype);
        if (t.gcSize() != 0) {
            Unsafe.storeThrough(node, ptr, t, t.zero());
        }
    }

    /** {@code typedarrayclear(elemType, ptr, len)}: zeroes len elements from ptr on. */
    @TruffleBoundary
    static void reflect_typedarrayclear(Node node, Object rtype, Object ptr, long len) {
        GoType t = type(node, rtype);
        long size = t.gcSize();
        for (long i = 0; size != 0 && i < len; i++) {
            Unsafe.storeThrough(node, Unsafe.add(node, ptr, i * size), t, t.zero());
        }
    }

    /** The fields of an {@code unsafeheader.Slice} passed by value: {@code {Data, Len, Cap}}. */
    private static Slice slice(Node node, Object header, GoType elem) {
        Flat h = (Flat) header;
        Object data = h.refs[0];
        long len = h.prims[0];
        long cap = h.prims[1];
        if (data == null || cap == 0 || elem.gcSize() == 0) {
            return new Slice(null, 0, 0, 0, (int) len, (int) cap);
        }
        return Unsafe.sliceOver(node, data, len, cap, elem, Unsafe.SLICE_STRADDLES);
    }

    /** {@code typedslicecopy(t, dst, src unsafeheader.Slice) int}: the built-in copy for element type t. */
    @TruffleBoundary
    static long reflect_typedslicecopy(Node node, Object rtype, Object dst, Object src) {
        GoType t = type(node, rtype);
        Slice d = slice(node, dst, t);
        Slice s = slice(node, src, t);
        int n = Math.min(d.len, s.len);
        if (n == 0 || d.base == null || s.base == null) {
            return n;
        }
        // System.arraycopy is a memmove: overlapping ranges of one backing store are safe.
        System.arraycopy(s.base.prims, s.prim, d.base.prims, d.prim, n * t.nPrim());
        System.arraycopy(s.base.refs, s.ref, d.base.refs, d.ref, n * t.nRef());
        System.arraycopy(s.base.bytes, s.byt, d.base.bytes, d.byt, n * t.nByte());
        return n;
    }

    /**
     * {@code growslice(t, old unsafeheader.Slice, num) unsafeheader.Slice}: a slice of the same
     * length with room for num more elements (the caller guarantees {@code len+num > cap}), its
     * capacity grown as {@code append} grows it; the elements up to the old capacity are kept.
     */
    @TruffleBoundary
    static Object reflect_growslice(Node node, Object rtype, Object old, long num) {
        GoType t = type(node, rtype);
        Flat h = (Flat) old;
        long len = h.prims[0];
        Slice s = slice(node, old, t);
        long need = len + num;
        if (need < 0 || need > Integer.MAX_VALUE) {
            throw Panics.plain(node, "reflect.Value.Grow: slice overflow");
        }
        Slice whole = new Slice(s.base, s.prim, s.ref, s.byt, s.cap, s.cap);
        Slice g = Data.reserve(whole, (int) need - s.cap, t);
        Flat r = h.type.zeroFlat();
        r.refs[0] = t.gcSize() == 0 ? h.refs[0] : Unsafe.sliceData(g);
        r.prims[0] = len;
        r.prims[1] = g.cap;
        return r;
    }

    /** {@code unsafeslice(t, ptr, len)}: gc's validity check of {@code unsafe.Slice(ptr, len)}. */
    @TruffleBoundary
    static void reflect_unsafeslice(Node node, Object rtype, Object ptr, long len) {
        if (len < 0) {
            throw Panics.error(node, "unsafe.Slice: len out of range");
        }
        if (ptr == null && len > 0) {
            throw Panics.error(node, "unsafe.Slice: ptr is nil and len is not zero");
        }
    }

    // ---------------------------------------------------------------- maps (run-time helpers)

    private static GoType mapType(Node node, Object rtype) {
        GoType t = type(node, rtype).u();
        if (t.kind != GoType.MAP) {
            throw new IllegalStateException("not a map type: " + t.str());
        }
        return t;
    }

    private static int find(Node node, GoMap m, GoType keyType, Object k) {
        try {
            if (k instanceof Boolean b) {
                return m.find(b ? 1L : 0L);
            }
            return keyType.carrier() == GoType.C_I64 ? m.find((long) k) : m.find(k);
        } catch (Values.Uncomparable u) {
            throw Panics.unhashable(node, u.type);
        }
    }

    private static int put(Node node, GoMap m, GoType keyType, Object k) {
        try {
            if (k instanceof Boolean b) {
                return m.put(b ? 1L : 0L);
            }
            return keyType.carrier() == GoType.C_I64 ? m.put((long) k) : m.put(k);
        } catch (Values.Uncomparable u) {
            throw Panics.unhashable(node, u.type);
        }
    }

    /** {@code makemap(t, cap)}. */
    @TruffleBoundary
    static Object reflect_makemap(Node node, Object rtype, long cap) {
        GoType t = mapType(node, rtype);
        return new GoMap(t.mapKey, t.elem, (int) Math.max(0, Math.min(cap, 1 << 20)));
    }

    /** {@code mapaccess(t, m, key) unsafe.Pointer}: a pointer to a copy of the element, nil if absent. */
    @TruffleBoundary
    static Object reflect_mapaccess(Node node, Object rtype, Object m, Object key) {
        GoType t = mapType(node, rtype);
        Object k = Unsafe.loadThrough(node, key, t.mapKey);
        GoMap map = (GoMap) m;
        if (map == null || map.len() == 0) {
            if (Values.hashMightPanic(t.mapKey)) {
                Panics.checkMapKey(node, t.mapKey, k);
            }
            return null;
        }
        int e = find(node, map, t.mapKey, k);
        return e < 0 ? null : boxed(map.valType, Values.load(map.valType, map.vals, e * map.vp, e * map.vr, e * map.vb));
    }

    @TruffleBoundary
    static Object reflect_mapaccess_faststr(Node node, Object rtype, Object m, Str key) {
        GoMap map = (GoMap) m;
        if (map == null) {
            return null;
        }
        int e = map.find(key);
        return e < 0 ? null : boxed(map.valType, Values.load(map.valType, map.vals, e * map.vp, e * map.vr, e * map.vb));
    }

    private static void assign(Node node, GoMap map, GoType t, Object k, Object val) {
        if (map == null) {
            throw Panics.nilMap(node);
        }
        int e = put(node, map, t.mapKey, k);
        if (t.elem.gcSize() != 0) {
            Values.store(t.elem, map.vals, e * map.vp, e * map.vr, e * map.vb, Unsafe.loadThrough(node, val, t.elem));
        }
    }

    /** {@code mapassign0(t, m, key, val)}: {@code m[*key] = *val}. */
    @TruffleBoundary
    static void reflect_mapassign0(Node node, Object rtype, Object m, Object key, Object val) {
        GoType t = mapType(node, rtype);
        assign(node, (GoMap) m, t, Unsafe.loadThrough(node, key, t.mapKey), val);
    }

    @TruffleBoundary
    static void reflect_mapassign_faststr0(Node node, Object rtype, Object m, Str key, Object val) {
        assign(node, (GoMap) m, mapType(node, rtype), key, val);
    }

    /** {@code mapdelete(t, m, key)}. */
    @TruffleBoundary
    static void reflect_mapdelete(Node node, Object rtype, Object m, Object key) {
        GoType t = mapType(node, rtype);
        Object k = Unsafe.loadThrough(node, key, t.mapKey);
        GoMap map = (GoMap) m;
        if (map == null || map.len() == 0) {
            if (Values.hashMightPanic(t.mapKey)) {
                Panics.checkMapKey(node, t.mapKey, k);
            }
            return;
        }
        map.delete(find(node, map, t.mapKey, k));
    }

    @TruffleBoundary
    static void reflect_mapdelete_faststr(Node node, Object rtype, Object m, Str key) {
        GoMap map = (GoMap) m;
        if (map != null) {
            map.delete(map.find(key));
        }
    }

    @TruffleBoundary
    static void reflect_mapclear(Node node, Object rtype, Object m) {
        if (m != null) {
            ((GoMap) m).clear();
        }
    }

    // ---------------------------------------------------------------- channels (run-time helpers)

    /** {@code makechan(typ, size)}. */
    @TruffleBoundary
    static Object reflect_makechan(Node node, Object rtype, long size) {
        GoType t = type(node, rtype).u();
        if (size < 0 || size > Integer.MAX_VALUE) {
            throw Panics.plain(node, "makechan: size out of range");
        }
        return new GoChan((int) size, t.elem.zero());
    }

    @TruffleBoundary
    static long reflect_chancap(Node node, Object ch) {
        return ch == null ? 0 : ((GoChan) ch).capacity();
    }

    // ---------------------------------------------------------------- call path

    /**
     * {@code reflect.methodValueCall}: upstream's assembly trampoline behind a method value made
     * by {@code Value.Method}. Reached only by calling such a func value (reflect's call path).
     */
    @TruffleBoundary
    static void reflect_methodValueCall(Node node) {
        throw Panics.fatal(node, "reflect: calling a method value is not supported (graalgo: reflect call path)");
    }
}
