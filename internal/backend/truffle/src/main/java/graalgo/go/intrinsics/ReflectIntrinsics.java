package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RootNode;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.Receivers;
import graalgo.go.types.Method;
import graalgo.go.values.Env;
import graalgo.go.values.TypedChan;
import graalgo.language.GoLanguage;
import graalgo.runtime.ChanOps;
import graalgo.runtime.GoErrorValue;
import graalgo.runtime.PanicException;
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
        return Unsafe.toInt(node, c.target());
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
     * {@code addReflectOff(ptr)}: an offset that names an object reflect made or found at run
     * time (a field's type, a name), resolved by {@code resolveNameOff}/{@code resolveTypeOff}.
     */
    @TruffleBoundary
    static long reflect_addReflectOff(Node node, Object ptr) {
        return GoProcess.get(node).types.descriptors.addOff(ptr);
    }

    /**
     * {@code compiledTypelinks()}: the sorted type lists reflect searches before it makes a
     * pointer, slice, map, channel or func type itself. None is recorded.
     */
    static Object reflect_compiledTypelinks(Node node) {
        return new Object[] {Slice.NIL, Slice.NIL};
    }

    /** AIX only; never called on the tracked platform. */
    static Object reflect_adjustAIXGCDataForRuntime(Node node, Object p) {
        return p;
    }

    // ---------------------------------------------------------------- type constructors

    /** The descriptor image of an interned type. */
    private static Object image(Node node, GoType t) {
        return GoProcess.get(node).types.descriptors.typeWord(t);
    }

    private static GoType[] types(Node node, Object slice) {
        Slice s = (Slice) slice;
        GoType[] out = new GoType[s.len()];
        for (int i = 0; i < s.len(); i++) {
            out[i] = type(node, s.base().refs[s.ref() + i]);
        }
        return out;
    }

    /** {@code typePointerTo(elem)} (the patched {@code (*rtype).ptrTo}): the descriptor of {@code *elem}. */
    @TruffleBoundary
    static Object reflect_typePointerTo(Node node, Object elem) {
        return image(node, GoProcess.get(node).types.pointer(type(node, elem)));
    }

    /** {@code typeSliceOf(elem)} ({@code SliceOf}). */
    @TruffleBoundary
    static Object reflect_typeSliceOf(Node node, Object elem) {
        return image(node, GoProcess.get(node).types.slice(type(node, elem)));
    }

    /** {@code typeArrayOf(length, elem)} ({@code ArrayOf}). */
    @TruffleBoundary
    static Object reflect_typeArrayOf(Node node, long length, Object elem) {
        return image(node, GoProcess.get(node).types.array(length, type(node, elem)));
    }

    /** {@code typeChanOf(elem, dir)} ({@code ChanOf}); dir is reflect's ChanDir (1 recv, 2 send, 3 both). */
    @TruffleBoundary
    static Object reflect_typeChanOf(Node node, Object elem, long dir) {
        // GIR's ChanDir: 1 both, 2 send, 3 recv.
        int d = dir == 3 ? 1 : dir == 2 ? 2 : 3;
        return image(node, GoProcess.get(node).types.chan(d, type(node, elem)));
    }

    /** {@code typeMapOf(key, elem)} ({@code MapOf}). */
    @TruffleBoundary
    static Object reflect_typeMapOf(Node node, Object key, Object elem) {
        return image(node, GoProcess.get(node).types.map(type(node, key), type(node, elem)));
    }

    /** {@code typeFuncOf(in, out, variadic)} ({@code FuncOf}). */
    @TruffleBoundary
    static Object reflect_typeFuncOf(Node node, Object in, Object out, boolean variadic) {
        return image(node, GoProcess.get(node).types.func(types(node, in), types(node, out), variadic));
    }

    /**
     * {@code typeStructOf(names, pkgPaths, tags, types, embedded)} ({@code StructOf}, after
     * upstream's validation of the fields).
     */
    @TruffleBoundary
    static Object reflect_typeStructOf(Node node, Object names, Object pkgPaths, Object tags, Object types,
            Object embedded) {
        Slice n = (Slice) names;
        Slice p = (Slice) pkgPaths;
        Slice g = (Slice) tags;
        Slice e = (Slice) embedded;
        GoType[] ts = types(node, types);
        GoType.Field[] fields = new GoType.Field[n.len()];
        for (int i = 0; i < n.len(); i++) {
            fields[i] = new GoType.Field(n.base().refs[n.ref() + i].toString(), p.base().refs[p.ref() + i].toString(), ts[i],
                    e.base().bytes[e.byt() + i] != 0, g.base().refs[g.ref() + i].toString());
        }
        return image(node, GoProcess.get(node).types.struct(fields));
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
        int n = Math.min(d.len(), s.len());
        if (n == 0 || d.base() == null || s.base() == null) {
            return n;
        }
        // System.arraycopy is a memmove: overlapping ranges of one backing store are safe.
        System.arraycopy(s.base().prims, s.prim(), d.base().prims, d.prim(), n * t.nPrim());
        System.arraycopy(s.base().refs, s.ref(), d.base().refs, d.ref(), n * t.nRef());
        System.arraycopy(s.base().bytes, s.byt(), d.base().bytes, d.byt(), n * t.nByte());
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
        Slice whole = new Slice(s.base(), s.prim(), s.ref(), s.byt(), s.cap(), s.cap());
        Slice g = Data.reserve(whole, (int) need - s.cap(), t);
        Flat r = h.type.zeroFlat();
        r.refs[0] = t.gcSize() == 0 ? h.refs[0] : Unsafe.sliceData(g);
        r.prims[0] = len;
        r.prims[1] = g.cap();
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

    /**
     * {@code typehash(t, p, h) uintptr}: the hash of the value of type t at p, seeded with h (the
     * hasher of a map type made by {@code MapOf}).
     */
    @TruffleBoundary
    static long reflect_typehash(Node node, Object rtype, Object p, long h) {
        GoType t = type(node, rtype);
        try {
            int v = t.gcSize() == 0 ? 0 : Values.hash(t, Unsafe.loadThrough(node, p, t));
            return (h * 31 + v) * 0x9E3779B97F4A7C15L;
        } catch (Values.Uncomparable u) {
            throw Panics.unhashable(node, u.type);
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
        return new TypedChan(t.elem, (int) size);
    }

    @TruffleBoundary
    static long reflect_chancap(Node node, Object ch) {
        return ch == null ? 0 : ((GoChan) ch).capacity();
    }

    /** The element type of a channel made by this node package. */
    private static GoType chanElem(Object ch) {
        return ((TypedChan) ch).elem;
    }

    private static RuntimeException convert(Node node, PanicException e) {
        return Panics.of(node, (GoErrorValue) e.goValue());
    }

    /**
     * Stores a channel element at {@code p}: the address of a variable of the element type, or
     * (reflect's {@code &val.ptr}) of a pointer word for a pointer-shaped element.
     */
    private static void storeElem(Node node, Object p, GoType elem, Object v) {
        if (elem.gcSize() != 0) {
            Unsafe.storeThrough(node, p, elem, v);
        }
    }

    /**
     * {@code chanrecv(ch, nb, val) (selected, received)}: receives into {@code *val}; with
     * {@code nb} it does not block ({@code selected} false when nothing was ready).
     */
    @TruffleBoundary
    static Object reflect_chanrecv(Node node, Object ch, boolean nb, Object val) {
        if (ch == null) {
            if (nb) {
                return new Object[] {false, false};
            }
            GoProcess.get(node).blockForever("chan receive (nil chan)");
        }
        GoChan c = (GoChan) ch;
        GoType elem = chanElem(ch);
        if (nb) {
            ChanOps.SelectCase[] cases = {new ChanOps.SelectCase(c, false, null), new ChanOps.SelectCase(null, false, null)};
            if (ChanOps.selectGo(cases) != 0) {
                return new Object[] {false, false};
            }
            boolean ok = ChanOps.selectRecvOk();
            storeElem(node, val, elem, ok ? ChanOps.selectRecvValue() : elem.zero());
            return new Object[] {true, ok};
        }
        GoChan.RecvResult r = ChanOps.recvOk(c);
        storeElem(node, val, elem, r.ok() ? r.value() : elem.zero());
        return new Object[] {true, r.ok()};
    }

    /** {@code chansend0(ch, val, nb) bool}: sends {@code *val}; with {@code nb} it does not block. */
    @TruffleBoundary
    static boolean reflect_chansend0(Node node, Object ch, Object val, boolean nb) {
        if (ch == null) {
            if (nb) {
                return false;
            }
            GoProcess.get(node).blockForever("chan send (nil chan)");
        }
        GoChan c = (GoChan) ch;
        GoType elem = chanElem(ch);
        Object v = elem.gcSize() == 0 ? elem.zero() : Unsafe.loadThrough(node, val, elem);
        try {
            if (nb) {
                ChanOps.SelectCase[] cases = {new ChanOps.SelectCase(c, true, v), new ChanOps.SelectCase(null, false, null)};
                return ChanOps.selectGo(cases) == 0;
            }
            ChanOps.send(c, v);
            return true;
        } catch (PanicException e) {
            throw convert(node, e);
        }
    }

    /**
     * {@code rselect(cases []runtimeSelect) (chosen int, recvOK bool)}: a select statement over
     * the cases ({@code dir} 1 send, 2 receive, 3 default; {@code val} the address the element
     * is sent from or received into).
     */
    @TruffleBoundary
    static Object reflect_rselect(Node node, Object casesSlice) {
        Slice s = (Slice) casesSlice;
        GoType ct = s.len() == 0 ? null : s.base().type.elem.u();
        int n = s.len();
        int def = -1;
        ChanOps.SelectCase[] cases = new ChanOps.SelectCase[n];
        Object[] vals = new Object[n];
        GoChan[] chans = new GoChan[n];
        int[] order = new int[n];
        int m = 0;
        for (int i = 0; i < n; i++) {
            int p = s.prim() + i * ct.nPrim();
            int r = s.ref() + i * ct.nRef();
            long dir = s.base().prims[p + ct.fields[0].primOff];
            Object ch = s.base().refs[r + ct.fields[2].refOff];
            Object val = s.base().refs[r + ct.fields[3].refOff];
            vals[i] = val;
            if (dir == 3) {
                def = i;
                continue;
            }
            // A nil channel is never ready: a private channel nobody else holds.
            GoChan c = ch == null ? new GoChan(0, null) : (GoChan) ch;
            chans[i] = ch == null ? null : c;
            Object v = null;
            if (dir == 1 && ch != null) {
                GoType elem = chanElem(ch);
                v = elem.gcSize() == 0 ? elem.zero() : Unsafe.loadThrough(node, val, elem);
            }
            order[m] = i;
            cases[m++] = new ChanOps.SelectCase(c, dir == 1, v);
        }
        if (def >= 0) {
            order[m] = def;
            cases[m++] = new ChanOps.SelectCase(null, false, null);
        } else if (m == 0) {
            GoProcess.get(node).blockForever("select (no cases)");
        }
        int k;
        try {
            k = ChanOps.selectGo(java.util.Arrays.copyOf(cases, m));
        } catch (PanicException e) {
            throw convert(node, e);
        }
        int chosen = order[k];
        boolean recvOk = false;
        long dir = s.base().prims[s.prim() + chosen * ct.nPrim() + ct.fields[0].primOff];
        if (dir == 2) {
            recvOk = ChanOps.selectRecvOk();
            GoType elem = chanElem(chans[chosen]);
            storeElem(node, vals[chosen], elem, recvOk ? ChanOps.selectRecvValue() : elem.zero());
        }
        return new Object[] {(long) chosen, recvOk};
    }

    // ---------------------------------------------------------------- call path

    private static GoType funcType(Node node, Object rtype) {
        GoType t = type(node, rtype).u();
        if (t.kind != GoType.FUNC) {
            throw new IllegalStateException("not a func type: " + t.str());
        }
        return t;
    }

    /** The carrier values at the addresses in {@code args}, after {@code lead} leading slots. */
    private static Object[] loadArgs(Node node, GoType ft, Object args, int lead) {
        Slice a = (Slice) args;
        if (a.len() != ft.params.length) {
            throw new IllegalStateException("reflect call: " + a.len() + " arguments for " + ft.str());
        }
        Object[] out = new Object[lead + a.len()];
        for (int i = 0; i < a.len(); i++) {
            GoType pt = ft.params[i];
            out[lead + i] = pt.gcSize() == 0 ? pt.zero() : Unsafe.loadThrough(node, a.base().refs[a.ref() + i], pt);
        }
        return out;
    }

    private static void storeResults(Node node, GoType ft, Object rets, Object result) {
        Slice r = (Slice) rets;
        int n = ft.results.length;
        for (int i = 0; i < n; i++) {
            GoType rt = ft.results[i];
            if (rt.gcSize() != 0) {
                Unsafe.storeThrough(node, r.base().refs[r.ref() + i], rt, n == 1 ? result : ((Object[]) result)[i]);
            }
        }
    }

    /**
     * {@code callFunc(t, fn, args, rets)} (the patched {@code Value.call}): calls the func value
     * {@code fn} of type {@code t} with the values at the addresses in {@code args} and stores
     * its results at the addresses in {@code rets}.
     */
    @TruffleBoundary
    static void reflect_callFunc(Node node, Object rtype, Object fn, Object args, Object rets) {
        GoType ft = funcType(node, rtype);
        Closure c = (Closure) fn;
        Object[] a = loadArgs(node, ft, args, 1);
        a[0] = c.env();
        storeResults(node, ft, rets, c.target().call(a));
    }

    /**
     * {@code callMethodByName(rcvr, name, t, args, rets)}: calls the method {@code name} of the
     * dynamic type of {@code rcvr} on it, as a call through an interface does.
     */
    @TruffleBoundary
    static void reflect_callMethodByName(Node node, Object rcvr, Str name, Object rtype, Object args, Object rets) {
        GoType ft = funcType(node, rtype);
        Iface r = (Iface) rcvr;
        Method m = r == null ? null : r.type.findMethod(name.toString());
        if (m == null) {
            throw new IllegalStateException("reflect call: no method " + name + " on " + (r == null ? "nil" : r.type.str()));
        }
        Object[] a = loadArgs(node, ft, args, 2);
        a[1] = Receivers.adapt(m, r.data, node);
        storeResults(node, ft, rets, m.target().call(a));
    }

    private static final FrameDescriptor FUNC_ENV = Env.layout(2);

    /**
     * {@code makeFuncValue(t, impl) unsafe.Pointer}: a func value of type {@code t} whose call
     * runs {@code impl(args, rets []unsafe.Pointer)} with the addresses of copies of its
     * arguments and of zeroed result variables ({@code MakeFunc}, method values).
     */
    @TruffleBoundary
    static Object reflect_makeFuncValue(Node node, Object rtype, Object impl) {
        MaterializedFrame env = Env.create(FUNC_ENV);
        Env.set(env, 0, funcType(node, rtype));
        Env.set(env, 1, impl);
        GoLanguage lang = GoLanguage.get(node);
        return new Closure(lang.runtimeTypes().sharedTarget("reflect.makeFuncValue.func", () -> new MadeFuncRoot(lang)), env);
    }

    /** The code of every func value {@link #reflect_makeFuncValue} returns: env {@code [func type, impl]}. */
    private static final class MadeFuncRoot extends RootNode {
        MadeFuncRoot(GoLanguage language) {
            super(language);
        }

        @Override
        public String getName() {
            return "reflect.makeFuncStub";
        }

        @Override
        public boolean isInternal() {
            return true;
        }

        @Override
        public Object execute(VirtualFrame frame) {
            Object[] a = frame.getArguments();
            return run((GoType) Env.get(a[0], 0), (Closure) Env.get(a[0], 1), a);
        }

        @TruffleBoundary
        private Object run(GoType ft, Closure impl, Object[] a) {
            GoType ptr = GoProcess.get(this).types.basic(GoType.UNSAFE_POINTER);
            int nin = ft.params.length;
            int nout = ft.results.length;
            Flat args = Data.backing(ptr, nin);
            for (int i = 0; i < nin; i++) {
                args.refs[i] = boxed(ft.params[i], a[1 + i]);
            }
            Flat rets = Data.backing(ptr, nout);
            for (int i = 0; i < nout; i++) {
                rets.refs[i] = ft.results[i].zeroFlat();
            }
            impl.target().call(impl.env(), new Slice(args, 0, 0, 0, nin, nin), new Slice(rets, 0, 0, 0, nout, nout));
            if (nout == 0) {
                return null;
            }
            Object[] out = new Object[nout];
            for (int i = 0; i < nout; i++) {
                GoType rt = ft.results[i];
                out[i] = rt.gcSize() == 0 ? rt.zero() : Values.load(rt, (Flat) rets.refs[i], 0, 0, 0);
            }
            return nout == 1 ? out[0] : out;
        }
    }

    private static final FrameDescriptor METHOD_ENV = Env.layout(1);

    /**
     * {@code methodFunc(t, name) unsafe.Pointer}: the func value of the method expression
     * {@code T.name} ({@code Type.Method(i).Func}): its first argument is the receiver.
     */
    @TruffleBoundary
    static Object reflect_methodFunc(Node node, Object rtype, Str name) {
        GoType t = type(node, rtype);
        Method m = t.findMethod(name.toString());
        if (m == null) {
            throw new IllegalStateException("reflect: no method " + name + " on " + t.str());
        }
        MaterializedFrame env = Env.create(METHOD_ENV);
        Env.set(env, 0, m);
        GoLanguage lang = GoLanguage.get(node);
        return new Closure(lang.runtimeTypes().sharedTarget("reflect.methodFunc.func", () -> new MethodExprRoot(lang)), env);
    }

    /** The code of every func value {@link #reflect_methodFunc} returns: env {@code [Method]}. */
    private static final class MethodExprRoot extends RootNode {
        MethodExprRoot(GoLanguage language) {
            super(language);
        }

        @Override
        public String getName() {
            return "reflect.methodFunc.func1";
        }

        @Override
        public boolean isInternal() {
            return true;
        }

        @Override
        public Object execute(VirtualFrame frame) {
            return run(frame.getArguments());
        }

        @TruffleBoundary
        private Object run(Object[] a) {
            Method m = (Method) Env.get(a[0], 0);
            Object[] b = a.clone();
            b[0] = null;
            b[1] = Receivers.adapt(m, a[1], this);
            return m.target().call(b);
        }
    }

    /**
     * {@code reflect.methodValueCall}: upstream's assembly trampoline behind a method value made
     * by {@code Value.Method}. Reached only by calling such a func value (reflect's call path).
     */
    @TruffleBoundary
    static void reflect_methodValueCall(Node node) {
        throw Panics.fatal(node, "reflect: calling a method value is not supported (graalgo: reflect call path)");
    }
}
