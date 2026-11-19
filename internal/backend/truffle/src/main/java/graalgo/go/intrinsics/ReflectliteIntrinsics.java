package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RootNode;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.Panics;
import graalgo.go.runtime.Unsafe;
import graalgo.go.types.Descriptors;
import graalgo.go.types.GoType;
import graalgo.go.types.TypeTable;
import graalgo.go.values.Closure;
import graalgo.go.values.Env;
import graalgo.go.values.Flat;
import graalgo.go.values.GoMap;
import graalgo.go.values.Iface;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;
import graalgo.language.GoLanguage;
import graalgo.runtime.GoChan;

/**
 * What gc's runtime provides to {@code internal/reflectlite} by {@code //go:linkname}: its
 * seven functions without a body (toolexec-design §5.6, W9a); registered in
 * {@code internal/toolexec/intrinsics/registry.txt}. reflectlite itself is upstream's Go code,
 * reading descriptor images ({@link Descriptors}) through the pointer model.
 *
 * <p>A {@code *abi.Type} argument is a descriptor image; {@link Descriptors#typeOf} maps it back
 * to the type whose layout the operation then uses.
 *
 * <p>Two upstream functions WITH a body are replaced here, temporarily (BUG-0037, work package
 * WP-REFLECTLITE-VIEW of docs/arch/RECOVERY.md): {@link #internal_reflectlite_Swapper} and
 * {@link #internal_reflectlite_pkgPath}. Their bodies need a byte or word view of storage whose
 * leaves are wider scalars (a {@code []float32} as {@code []int32}, an {@code int32} as
 * {@code [4]byte}), which the one slice shape of the flat value model does not give (SPEC §3.4);
 * the intrinsics do the same work through the typed accessors. Both go when checked views over
 * wider scalars land, and reflectlite then runs verbatim; the registry marks the entries
 * {@code TEMPORARY (BUG-0037)} and a test fails once that bug is marked fixed.
 */
final class ReflectliteIntrinsics {
    private ReflectliteIntrinsics() {}

    private static GoType type(Node node, Object rtype) {
        GoType t = Descriptors.typeOf(rtype);
        if (t == null) {
            // A descriptor this runtime did not build (reflect's StructOf and friends, W9b).
            throw Panics.fatal(node, "type descriptor not built by the runtime");
        }
        return t;
    }

    /** {@code resolveNameOff(ptrInModule unsafe.Pointer, off int32) unsafe.Pointer}. */
    static Object internal_reflectlite_resolveNameOff(Node node, Object ptrInModule, long off) {
        return GoProcess.get(node).types.descriptors.resolve(off);
    }

    /** {@code resolveTypeOff(rtype unsafe.Pointer, off int32) unsafe.Pointer}. */
    static Object internal_reflectlite_resolveTypeOff(Node node, Object rtype, long off) {
        return GoProcess.get(node).types.descriptors.resolve(off);
    }

    /** {@code unsafe_New(*abi.Type) unsafe.Pointer}: a zeroed variable of the type. */
    @TruffleBoundary
    static Object internal_reflectlite_unsafe_New(Node node, Object rtype) {
        return type(node, rtype).zeroFlat();
    }

    /** {@code typedmemmove(t *abi.Type, dst, src unsafe.Pointer)}: {@code *dst = *src} for a value of type t. */
    @TruffleBoundary
    static void internal_reflectlite_typedmemmove(Node node, Object rtype, Object dst, Object src) {
        GoType t = type(node, rtype);
        if (t.gcSize() == 0 || Unsafe.sameLocation(dst, src)) {
            return;
        }
        if (dst == null || src == null) {
            throw Panics.nilDeref(node);
        }
        Unsafe.storeThrough(node, dst, t, Unsafe.loadThrough(node, src, t));
    }

    /**
     * {@code ifaceE2I(t *abi.Type, src any, dst unsafe.Pointer)}: {@code *dst = src.(T)} for the
     * interface type T that t describes (gc's assertE2I: the conversion panic when the dynamic
     * type lacks a method).
     */
    @TruffleBoundary
    static void internal_reflectlite_ifaceE2I(Node node, Object rtype, Object src, Object dst) {
        GoType t = type(node, rtype);
        Iface i = (Iface) src;
        GoType any = GoProcess.get(node).types.emptyInterface;
        if (i == null || i.type.itab(t) == null) {
            throw Panics.typeAssert(node, any, i == null ? null : i.type, t);
        }
        Unsafe.storeThrough(node, dst, t, i);
    }

    /** {@code chanlen(ch unsafe.Pointer) int}. */
    @TruffleBoundary
    static long internal_reflectlite_chanlen(Node node, Object ch) {
        return ch == null ? 0 : ((GoChan) ch).buffered();
    }

    /** {@code maplen(m unsafe.Pointer) int}. */
    @TruffleBoundary
    static long internal_reflectlite_maplen(Node node, Object m) {
        return m == null ? 0 : ((GoMap) m).len();
    }

    // ---------------------------------------------------------------- BUG-0037 (temporary)

    private static final String SLICE_RANGE = "reflect: slice index out of range";
    private static final FrameDescriptor SWAP_ENV = Env.layout(3);

    /**
     * TEMPORARY (BUG-0037): {@code Swapper(slice any) func(i, j int)}. Upstream's body swaps
     * pointer-free elements of 1, 2, 4 or 8 bytes through a {@code []int8 .. []int64} view of the
     * slice, which the flat model refuses for elements that are not one leaf of that integer
     * type; this swaps the element's slots in each region of the backing object instead — the
     * typed move upstream's general path makes with {@code typedmemmove} — for every element
     * type. The observable contract is upstream's: the {@code *ValueError} panic for a non-slice,
     * the length-0/1 functions, and per element class the index panic of a Go slice (the fast
     * paths) or {@code "reflect: slice index out of range"} (the general path).
     */
    @TruffleBoundary
    static Object internal_reflectlite_Swapper(Node node, Object slice) {
        Iface v = (Iface) slice;
        GoType t = v == null ? null : v.type;
        if (t == null || t.u().kind != GoType.SLICE) {
            throw Panics.user(node, valueError(node, "Swapper", t == null ? 0 : Descriptors.abiKind(t)));
        }
        Slice s = (Slice) v.data;
        GoType elem = t.u().elem;
        long size = elem.gcSize();
        // Upstream's fast paths index a Go slice: a bad index is the runtime's index panic.
        boolean fast = elem.hasPointers() ? size == 8 || elem.u().kind == GoType.STRING
                : size == 1 || size == 2 || size == 4 || size == 8;
        MaterializedFrame env = Env.create(SWAP_ENV);
        Env.set(env, 0, s);
        Env.set(env, 1, elem);
        Env.set(env, 2, s.len <= 1 ? (Object) Integer.valueOf(s.len) : fast ? Boolean.TRUE : Boolean.FALSE);
        GoLanguage lang = GoLanguage.get(node);
        return new Closure(lang.runtimeTypes().sharedTarget("internal/reflectlite.Swapper.func", () -> new SwapRoot(lang)), env);
    }

    /**
     * The code of every func value {@link #internal_reflectlite_Swapper} returns (one shared
     * root): the environment holds {@code [Slice, element type, class]}, where {@code class} is
     * the length (0 or 1) of a short slice, else TRUE for upstream's fast-path element classes.
     */
    private static final class SwapRoot extends RootNode {
        SwapRoot(GoLanguage language) {
            super(language);
        }

        @Override
        public String getName() {
            return "internal/reflectlite.Swapper.func1";
        }

        @Override
        public boolean isInternal() {
            return true;
        }

        @Override
        public Object execute(VirtualFrame frame) {
            Object[] a = frame.getArguments();
            swap((Slice) Env.get(a[0], 0), (GoType) Env.get(a[0], 1), Env.get(a[0], 2), (long) a[1], (long) a[2]);
            return null;
        }

        @TruffleBoundary
        private void swap(Slice s, GoType elem, Object cls, long i, long j) {
            if (cls instanceof Integer n) {
                if (n == 0 || i != 0 || j != 0) {
                    throw Panics.user(this, new Iface(GoLanguage.get(this).runtimeTypes().table.string, Str.of(SLICE_RANGE)));
                }
                return;
            }
            long len = s.len;
            if (Long.compareUnsigned(i, len) >= 0 || Long.compareUnsigned(j, len) >= 0) {
                if (cls == Boolean.TRUE) {
                    // ss[i], ss[j] = ss[j], ss[i]: the right-hand side's indexes are checked first.
                    long bad = Long.compareUnsigned(j, len) >= 0 ? j : i;
                    throw Panics.index(this, bad, len, true);
                }
                throw Panics.user(this, new Iface(GoLanguage.get(this).runtimeTypes().table.string, Str.of(SLICE_RANGE)));
            }
            if (i == j) {
                return;
            }
            swapSlots(s, elem, (int) i, (int) j);
        }
    }

    /** Exchanges elements {@code i} and {@code j} of {@code s}, slot by slot in each region. */
    private static void swapSlots(Slice s, GoType elem, int i, int j) {
        Flat b = s.base;
        int np = elem.nPrim(), nr = elem.nRef(), nb = elem.nByte();
        for (int k = 0; k < np; k++) {
            int x = s.prim + i * np + k, y = s.prim + j * np + k;
            long tmp = b.prims[x];
            b.prims[x] = b.prims[y];
            b.prims[y] = tmp;
        }
        for (int k = 0; k < nr; k++) {
            int x = s.ref + i * nr + k, y = s.ref + j * nr + k;
            Object tmp = b.refs[x];
            b.refs[x] = b.refs[y];
            b.refs[y] = tmp;
        }
        for (int k = 0; k < nb; k++) {
            int x = s.byt + i * nb + k, y = s.byt + j * nb + k;
            byte tmp = b.bytes[x];
            b.bytes[x] = b.bytes[y];
            b.bytes[y] = tmp;
        }
    }

    /**
     * {@code &ValueError{Method: method, Kind: kind}} as a panic value: the struct is the
     * program's own {@code internal/reflectlite.ValueError}, so its {@code Error} method is
     * upstream's.
     */
    private static Iface valueError(Node node, String method, int kind) {
        TypeTable types = GoProcess.get(node).types;
        GoType ve = types.descriptors.programType("internal/reflectlite", "ValueError");
        if (ve == null) {
            throw Panics.fatal(node, "internal/reflectlite.ValueError is not in the program");
        }
        Flat obj = ve.zeroFlat();
        for (GoType.Field f : ve.u().fields) {
            if (f.name.equals("Method")) {
                Unsafe.storeValue(node, obj, f.gcOff, f.type, Str.of(method));
            } else if (f.name.equals("Kind")) {
                Unsafe.storeValue(node, obj, f.gcOff, f.type, (long) kind);
            }
        }
        return new Iface(types.pointer(ve), obj);
    }

    /**
     * TEMPORARY (BUG-0037): {@code pkgPath(n abi.Name) string}, the package qualifier of a name
     * (gc's dnameData encoding: flag byte, varint length, bytes, optional varint and tag, then
     * a 4-byte name offset when flag bit 2 is set). Upstream copies those 4 bytes into an
     * {@code int32} through a {@code [4]byte} view of the variable, which the flat model does
     * not give; this reads them as bytes of the name.
     */
    @TruffleBoundary
    static Str internal_reflectlite_pkgPath(Node node, Object n) {
        Object bytes = ((Flat) n).refs[0];
        if (bytes == null) {
            return Str.EMPTY;
        }
        if ((readByte(node, bytes, 0) & 4) == 0) {
            return Str.EMPTY;
        }
        long[] l = new long[1];
        long off = 1 + varint(node, bytes, 1, l);
        off += l[0];
        if ((readByte(node, bytes, 0) & 2) != 0) {
            off += varint(node, bytes, off, l);
            off += l[0];
        }
        int nameOff = 0;
        for (int i = 0; i < 4; i++) {
            nameOff |= readByte(node, bytes, off + i) << (8 * i);
        }
        Object pkg = GoProcess.get(node).types.descriptors.resolve(nameOff);
        if (pkg == null) {
            return Str.EMPTY;
        }
        long start = 1 + varint(node, pkg, 1, l);
        return Unsafe.stringOver(node, Unsafe.add(node, pkg, start), l[0]);
    }

    /** Byte {@code off} of the name at {@code p}. */
    private static int readByte(Node node, Object p, long off) {
        byte[] b = new byte[1];
        Unsafe.readBytes(node, Unsafe.referent(p), Unsafe.offset(p) + off, b, 1);
        return b[0] & 0xFF;
    }

    /** abi.Name.ReadVarint: the varint at {@code off} into {@code value[0]}; returns its length. */
    private static int varint(Node node, Object p, long off, long[] value) {
        long v = 0;
        for (int i = 0;; i++) {
            int x = readByte(node, p, off + i);
            v += (long) (x & 0x7f) << (7 * i);
            if ((x & 0x80) == 0) {
                value[0] = v;
                return i + 1;
            }
        }
    }
}
