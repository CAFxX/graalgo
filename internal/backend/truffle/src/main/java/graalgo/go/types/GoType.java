package graalgo.go.types;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import graalgo.go.values.Complex;
import graalgo.go.values.Flat;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.function.Supplier;

/**
 * A Go type descriptor, interned per canonical key in the language-level {@link TypeTable}
 * (toolexec-design §3.4, D7): type identity is reference equality. A defined type has kind
 * {@link #NAMED} and resolves its underlying type and method sets lazily from the declaring
 * package's TYPEDEFS; every other descriptor is structural.
 *
 * <p>The descriptor also owns the flat layout of its values (GG-0061): how many scalar and
 * reference slots a value occupies and at which slot offsets each struct field starts.
 */
public final class GoType {
    // Basic kinds use the GIR BasicKind numbering (1..18).
    public static final int BOOL = 1, INT = 2, INT8 = 3, INT16 = 4, INT32 = 5, INT64 = 6, UINT = 7, UINT8 = 8,
            UINT16 = 9, UINT32 = 10, UINT64 = 11, UINTPTR = 12, FLOAT32 = 13, FLOAT64 = 14, COMPLEX64 = 15,
            COMPLEX128 = 16, STRING = 17, UNSAFE_POINTER = 18;
    public static final int POINTER = 30, SLICE = 31, ARRAY = 32, MAP = 33, CHAN = 34, STRUCT = 35, FUNC = 36,
            INTERFACE = 37, TUPLE = 38, NAMED = 40;

    /** Carriers: how a value of the type travels through nodes and where it is stored. */
    public static final int C_I64 = 0, C_F64 = 1, C_BOOL = 2, C_REF = 3, C_FLAT = 4;

    /** A struct field with its slot offsets inside the struct. */
    public static final class Field {
        public final String name;
        /** Import path qualifying an unexported name, "" when exported. */
        public final String pkg;
        public final GoType type;
        public final boolean embedded;
        public final String tag;
        public int primOff;
        public int refOff;
        public int byteOff;
        /** gc's byte offset of the field inside the struct ({@code unsafe.Offsetof}). */
        public long gcOff;

        public Field(String name, String pkg, GoType type, boolean embedded, String tag) {
            this.name = name;
            this.pkg = pkg;
            this.type = type;
            this.embedded = embedded;
            this.tag = tag;
        }
    }

    /** An interface method: its lookup key ({@code name} or {@code pkg.name}) and signature. */
    public static final class IMethod {
        public final String name;
        public final String key;
        public final GoType sig;

        public IMethod(String name, String pkg, GoType sig) {
            this.name = name;
            this.key = pkg.isEmpty() ? name : pkg + "." + name;
            this.sig = sig;
        }
    }

    public final String key;
    public final int kind;
    // Defined types.
    public final String pkg;
    public final String name;
    public final GoType[] targs;
    private volatile GoType under;
    private Supplier<GoType> underSupplier;
    private Supplier<Map<String, Method>> valueMethodsSupplier;
    private Supplier<Map<String, Method>> ptrMethodsSupplier;
    private volatile Map<String, Method> valueMethods;
    private volatile Map<String, Method> ptrMethods;
    // Structure.
    public final GoType elem;
    public final GoType mapKey;
    public final long len;
    public final int dir;
    public final Field[] fields;
    public final GoType[] params;
    public final GoType[] results;
    public final boolean variadic;
    public final IMethod[] imethods;
    // Layout, computed on first use (a struct may mention itself through pointers).
    private volatile boolean laidOut;
    private int nPrim;
    private int nRef;
    private int nByte;
    /** gc's size and alignment of a value in bytes (linux/amd64), computed with the layout. */
    private long gcSize;
    private long gcAlign;
    /** The frontend's gc layout facts for a defined type, checked against ours once. */
    private long[] expectedLayout;
    /** A defined type's alignment as the frontend records it; 0: none recorded. */
    private long declaredAlign;
    private Object[] zeroRefs;
    private volatile String str;
    private final ConcurrentHashMap<GoType, Object> itabs = new ConcurrentHashMap<>();
    private static final Object NO_ITAB = new Object();
    /** Display string override for package-name qualification; see {@link #str()}. */
    String pkgName;

    /** One lock for every lazy resolution in a type table (the table itself). */
    private final Object lock;

    GoType(Object lock, String key, int kind, String pkg, String name, GoType[] targs, GoType elem, GoType mapKey, long len,
            int dir, Field[] fields, GoType[] params, GoType[] results, boolean variadic, IMethod[] imethods) {
        this.lock = lock;
        this.key = key;
        this.kind = kind;
        this.pkg = pkg;
        this.name = name;
        this.targs = targs;
        this.elem = elem;
        this.mapKey = mapKey;
        this.len = len;
        this.dir = dir;
        this.fields = fields;
        this.params = params;
        this.results = results;
        this.variadic = variadic;
        this.imethods = imethods;
    }

    /** Installs the lazy definition of a defined type (once, by whoever interned it). */
    void define(Supplier<GoType> underlying, Supplier<Map<String, Method>> valueMethods,
            Supplier<Map<String, Method>> ptrMethods) {
        synchronized (lock) {
            if (underSupplier == null && under == null) {
                this.underSupplier = underlying;
                this.valueMethodsSupplier = valueMethods;
                this.ptrMethodsSupplier = ptrMethods;
            }
        }
    }

    /**
     * Attaches the method sets of an unnamed struct type with promoted methods (the frontend
     * emits them with the type's TYPEDEFS entry); the first attachment wins.
     */
    public void attachMethods(Supplier<Map<String, Method>> valueMethods, Supplier<Map<String, Method>> ptrMethods) {
        synchronized (lock) {
            if (valueMethodsSupplier == null && ptrMethodsSupplier == null) {
                this.valueMethodsSupplier = valueMethods;
                this.ptrMethodsSupplier = ptrMethods;
            }
        }
    }

    /** The underlying (structural) type; {@code this} for a type that is not defined. */
    public GoType u() {
        if (kind != NAMED) {
            return this;
        }
        GoType t = under;
        return t != null ? t : resolveUnder();
    }

    @TruffleBoundary
    private GoType resolveUnder() {
        synchronized (lock) {
            if (under == null) {
                if (underSupplier == null) {
                    throw new IllegalStateException("type " + key + " has no definition in the bundle");
                }
                under = underSupplier.get().u();
            }
            return under;
        }
    }

    public boolean isNamed() {
        return kind == NAMED;
    }

    public boolean isInterface() {
        return u().kind == INTERFACE;
    }

    public boolean isInteger() {
        int k = u().kind;
        return k >= INT && k <= UINTPTR;
    }

    public boolean isFloat() {
        int k = u().kind;
        return k == FLOAT32 || k == FLOAT64;
    }

    public boolean isComplex() {
        int k = u().kind;
        return k == COMPLEX64 || k == COMPLEX128;
    }

    public boolean isUnsigned() {
        int k = u().kind;
        return k >= UINT && k <= UINTPTR;
    }

    /** Bit width of an integer or float type. */
    public int bits() {
        return switch (u().kind) {
            case INT8, UINT8 -> 8;
            case INT16, UINT16 -> 16;
            case INT32, UINT32, FLOAT32 -> 32;
            default -> 64;
        };
    }

    public int carrier() {
        int k = u().kind;
        if (k == BOOL) {
            return C_BOOL;
        } else if (k >= INT && k <= UINTPTR) {
            return C_I64;
        } else if (k == FLOAT32 || k == FLOAT64) {
            return C_F64;
        } else if (k == STRUCT || k == ARRAY) {
            return C_FLAT;
        }
        return C_REF;
    }

    /**
     * Whether a value is one byte-sized leaf — {@code uint8}, {@code int8}, {@code bool} — and
     * so lives in the byte region of its object (ADR-0015 decision 3).
     */
    public boolean isByteLeaf() {
        int k = u().kind;
        return k == BOOL || k == INT8 || k == UINT8;
    }

    public int nPrim() {
        layout();
        return nPrim;
    }

    public int nRef() {
        layout();
        return nRef;
    }

    /** The number of byte-sized leaves of a value (the byte region, ADR-0015 decision 3). */
    public int nByte() {
        layout();
        return nByte;
    }

    private void layout() {
        if (kind == NAMED) {
            GoType t = u();
            nPrim = t.nPrim();
            nRef = t.nRef();
            nByte = t.nByte;
            gcSize = t.gcSize;
            gcAlign = declaredAlign > 0 ? declaredAlign : t.gcAlign;
            if (expectedLayout != null) {
                checkExpectedLayout(t);
            }
            return;
        }
        if (!laidOut) {
            computeLayout();
        }
    }

    /**
     * Records gc's own layout of a defined type as the frontend computed it
     * ({@code types.SizesFor("gc", GOARCH)}: size, alignment, field offsets of a struct). The
     * backend derives every layout from the type's structure (it has to: instantiated and
     * unnamed types have no record); the first layout of this type checks the two against each
     * other, so a divergence of the backend's rules from gc's stops the program at once.
     *
     * <p>The recorded alignment is authoritative: gc gives some zero-size types of its own
     * library a larger alignment than their structure has (the 64-bit alignment marker of
     * {@code sync/atomic}), which no structural rule can know. For a type with a size the
     * recorded and the computed alignment must agree.
     */
    public void expectLayout(long size, long align, long[] offsets) {
        synchronized (lock) {
            if (expectedLayout == null) {
                long[] e = new long[2 + offsets.length];
                e[0] = size;
                e[1] = align;
                System.arraycopy(offsets, 0, e, 2, offsets.length);
                expectedLayout = e;
                declaredAlign = align;
            }
        }
    }

    @TruffleBoundary
    private void checkExpectedLayout(GoType t) {
        long[] e;
        synchronized (lock) {
            e = expectedLayout;
            expectedLayout = null;
        }
        if (e == null) {
            return;
        }
        boolean ok = e[0] == t.gcSize && (e[1] == t.gcAlign || e[0] == 0);
        if (t.kind == STRUCT) {
            ok &= e.length - 2 == t.fields.length;
            for (int i = 0; ok && i < t.fields.length; i++) {
                ok = t.fields[i].gcOff == e[2 + i];
            }
        }
        if (!ok) {
            StringBuilder sb = new StringBuilder("gc layout of " + key + ": the frontend records size " + e[0]
                    + " align " + e[1] + " offsets");
            for (int i = 2; i < e.length; i++) {
                sb.append(' ').append(e[i]);
            }
            sb.append("; the backend computes size ").append(t.gcSize).append(" align ").append(t.gcAlign)
                    .append(" offsets");
            if (t.kind == STRUCT) {
                for (Field f : t.fields) {
                    sb.append(' ').append(f.gcOff);
                }
            }
            throw new IllegalStateException(sb.toString());
        }
    }

    @TruffleBoundary
    private void computeLayout() {
        synchronized (lock) {
            computeLayoutLocked();
        }
    }

    private static long roundUp(long n, long align) {
        return (n + align - 1) / align * align;
    }

    /**
     * The slot layout and, with it, gc's layout (cmd/compile's CalcSize, which
     * {@code go/types.SizesFor("gc", "amd64")} mirrors): fields at their alignment, the size
     * rounded up to the alignment, and one byte of padding after a zero-size last field of a
     * struct that is not itself zero-size.
     */
    private void computeLayoutLocked() {
        if (laidOut) {
            return;
        }
        int p = 0;
        int r = 0;
        int b = 0;
        long size;
        long align;
        switch (kind) {
            case BOOL, INT8, UINT8 -> {
                b = 1;
                size = align = 1;
            }
            case INT16, UINT16 -> {
                p = 1;
                size = align = 2;
            }
            case INT32, UINT32, FLOAT32 -> {
                p = 1;
                size = align = 4;
            }
            case INT, INT64, UINT, UINT64, UINTPTR, FLOAT64 -> {
                p = 1;
                size = align = 8;
            }
            case COMPLEX64 -> {
                r = 1;
                size = 8;
                align = 4;
            }
            case COMPLEX128, STRING, INTERFACE -> {
                r = 1;
                size = 16;
                align = 8;
            }
            case SLICE -> {
                r = 1;
                size = 24;
                align = 8;
            }
            case ARRAY -> {
                p = Math.toIntExact(len * elem.nPrim());
                r = Math.toIntExact(len * elem.nRef());
                b = Math.toIntExact(len * elem.nByte);
                size = Math.multiplyExact(len, elem.gcSize);
                align = elem.gcAlign;
            }
            case STRUCT -> {
                long off = 0;
                align = 1;
                long last = 0;
                for (Field f : fields) {
                    f.primOff = p;
                    f.refOff = r;
                    f.byteOff = b;
                    p += f.type.nPrim();
                    r += f.type.nRef();
                    b += f.type.nByte;
                    long a = f.type.gcAlign;
                    off = roundUp(off, a);
                    f.gcOff = off;
                    last = f.type.gcSize;
                    off += last;
                    align = Math.max(align, a);
                }
                if (fields.length > 0 && last == 0 && off > 0) {
                    off++;
                }
                size = roundUp(off, align);
            }
            default -> {
                // Pointer-shaped: pointers, maps, channels, functions, unsafe.Pointer.
                r = 1;
                size = align = 8;
            }
        }
        nPrim = p;
        nRef = r;
        nByte = b;
        gcSize = size;
        gcAlign = align;
        laidOut = true;
    }

    /** The zero value in carrier form, boxed; a fresh object for struct and array types. */
    @TruffleBoundary
    public Object zero() {
        GoType t = u();
        return switch (t.carrier()) {
            case C_I64 -> 0L;
            case C_F64 -> 0.0;
            case C_BOOL -> Boolean.FALSE;
            case C_FLAT -> zeroFlat();
            default -> t.zeroRef();
        };
    }

    private Object zeroRef() {
        return switch (kind) {
            case STRING -> Str.EMPTY;
            case SLICE -> Slice.NIL;
            case COMPLEX64, COMPLEX128 -> Complex.ZERO;
            default -> null;
        };
    }

    /** A zeroed value of this (struct, array or boxed scalar) type. */
    public Flat zeroFlat() {
        Object[] z = zeroRefs();
        int np = nPrim();
        int nb = nByte();
        return new Flat(this, np == 0 ? Flat.NO_PRIMS : new long[np], z.length == 0 ? Flat.NO_REFS : z.clone(),
                nb == 0 ? Flat.NO_BYTES : new byte[nb]);
    }

    /** The reference slots of a zero value (shared template: clone before use). */
    public Object[] zeroRefs() {
        GoType t = u();
        Object[] z = t.zeroRefs;
        return z != null ? z : t.buildZeroRefs();
    }

    @TruffleBoundary
    private Object[] buildZeroRefs() {
        synchronized (lock) {
            if (zeroRefs == null) {
                Object[] z = new Object[nRef()];
                fillZero(this, z, 0);
                zeroRefs = z;
            }
            return zeroRefs;
        }
    }

    private static int fillZero(GoType type, Object[] out, int at) {
        GoType t = type.u();
        if (t.carrier() == C_REF) {
            out[at] = t.zeroRef();
            return at + 1;
        } else if (t.kind == ARRAY) {
            for (long i = 0; i < t.len && t.elem.nRef() > 0; i++) {
                at = fillZero(t.elem, out, at);
            }
        } else if (t.kind == STRUCT) {
            for (Field f : t.fields) {
                at = fillZero(f.type, out, at);
            }
        }
        return at;
    }

    /** True when the reference slots of a zero value are all Java null. */
    public boolean zeroRefsAreNull() {
        for (Object o : zeroRefs()) {
            if (o != null) {
                return false;
            }
        }
        return true;
    }

    // ---------------------------------------------------------------- gc layout facts

    /** gc's size of a value in bytes ({@code unsafe.Sizeof}). */
    public long gcSize() {
        layout();
        return gcSize;
    }

    /** gc's alignment of a value in bytes ({@code unsafe.Alignof} of a variable). */
    public long gcAlign() {
        layout();
        return gcAlign;
    }

    // ---------------------------------------------------------------- leaf table

    /**
     * A leaf of a flat value: one scalar or one reference slot, with the gc bytes it stands
     * for. {@code type} is the leaf's own (underlying) type: an integer, float or boolean
     * type for a scalar slot; for a reference slot a pointer-shaped type (one gc word), a
     * string or interface type (two words), a slice type (three words) or a complex type.
     */
    public static final class Leaf {
        public final GoType type;
        /** gc byte offset of the leaf's first byte, relative to the value the table is of. */
        public final long gcOff;
        /** The leaf's index among the leaves of the value, in layout order. */
        public final int index;
        /** The slot offsets of the leaf's position; the one of its {@link #region} is its slot. */
        public final int prim;
        public final int ref;
        public final int byt;

        Leaf(GoType type, long gcOff, int prim, int ref, int byt) {
            this.type = type;
            this.gcOff = gcOff;
            this.index = prim + ref + byt;
            this.prim = prim;
            this.ref = ref;
            this.byt = byt;
        }

        /** gc's size of the leaf in bytes. */
        public long size() {
            return type.gcSize();
        }

        /** Which array of the object holds the leaf: {@link #R_PRIM}, {@link #R_REF}, {@link #R_BYTE}. */
        public int region() {
            return type.nRef() == 1 ? R_REF : type.nByte() == 1 ? R_BYTE : R_PRIM;
        }

        /** The leaf's index in the array of its region. */
        public int slot() {
            return switch (region()) {
                case R_REF -> ref;
                case R_BYTE -> byt;
                default -> prim;
            };
        }
    }

    /** Regions of a flat object: scalar slots, reference slots, byte-sized leaves. */
    public static final int R_PRIM = 0, R_REF = 1, R_BYTE = 2;

    /** The number of leaves (scalar plus reference slots) of a value. */
    public int nLeaves() {
        return nPrim() + nRef() + nByte();
    }

    /**
     * The leaf table, forward direction (SPEC §3.4, GG-0061): the leaf that holds gc byte
     * {@code off} of a value of this type, or null when the byte is padding or outside the
     * value. Structural, so an array of any length costs one division per level.
     */
    @TruffleBoundary
    public Leaf leafAt(long off) {
        return leafAt(off, 0, 0, 0, 0);
    }

    private Leaf leafAt(long off, long base, int p, int r, int b) {
        GoType t = u();
        if (off < 0 || off >= t.gcSize()) {
            return null;
        }
        switch (t.kind) {
            case ARRAY: {
                long es = t.elem.gcSize();
                long i = off / es;
                return t.elem.leafAt(off - i * es, base + i * es, p + (int) i * t.elem.nPrim(),
                        r + (int) i * t.elem.nRef(), b + (int) i * t.elem.nByte());
            }
            case STRUCT:
                for (int k = t.fields.length - 1; k >= 0; k--) {
                    Field f = t.fields[k];
                    if (off >= f.gcOff) {
                        // The last field that starts at or before the byte; past its end: padding.
                        if (f.type.gcSize() == 0 && k > 0) {
                            continue;
                        }
                        return f.type.leafAt(off - f.gcOff, base + f.gcOff, p + f.primOff, r + f.refOff,
                                b + f.byteOff);
                    }
                }
                return null;
            default:
                return new Leaf(t, base, p, r, b);
        }
    }

    /**
     * The leaf table, inverse direction: gc's byte offset of the position that {@code k} leaves
     * precede — a pointer's slot offsets name it, {@code k} being their sum, since every leaf
     * is in exactly one region. {@code k == nLeaves()} is the end of the value.
     */
    @TruffleBoundary
    public long gcOffOfLeaf(int k) {
        GoType t = u();
        int n = t.nLeaves();
        if (k < 0 || k > n) {
            throw new IllegalArgumentException("leaf " + k + " of " + key);
        }
        if (k == n) {
            return k == 0 ? 0 : t.gcSize();
        }
        switch (t.kind) {
            case ARRAY: {
                int en = t.elem.nLeaves();
                int i = k / en;
                return i * t.elem.gcSize() + t.elem.gcOffOfLeaf(k - i * en);
            }
            case STRUCT:
                for (int f = t.fields.length - 1; f >= 0; f--) {
                    Field fld = t.fields[f];
                    int start = fld.primOff + fld.refOff + fld.byteOff;
                    if (k >= start && fld.type.nLeaves() > 0) {
                        return fld.gcOff + fld.type.gcOffOfLeaf(k - start);
                    }
                }
                throw new IllegalStateException("leaf " + k + " of " + key);
            default:
                return 0;
        }
    }

    /** Whether gc would scan values of the type (they contain pointers). */
    public boolean hasPointers() {
        return nRef() > 0 && !isComplex();
    }

    // ---------------------------------------------------------------- methods and itabs

    /** The method {@code key} of this type's method set as a dynamic type, or null. */
    @TruffleBoundary
    public Method findMethod(String methodKey) {
        if (kind == NAMED || kind == STRUCT) {
            return valueMethods().get(methodKey);
        } else if (kind == POINTER && (elem.kind == NAMED || elem.kind == STRUCT)) {
            return elem.ptrMethods().get(methodKey);
        }
        return null;
    }

    private Map<String, Method> valueMethods() {
        Map<String, Method> m = valueMethods;
        if (m == null) {
            synchronized (lock) {
                u();
                m = valueMethodsSupplier == null ? Map.of() : valueMethodsSupplier.get();
                valueMethods = m;
            }
        }
        return m;
    }

    private Map<String, Method> ptrMethods() {
        Map<String, Method> m = ptrMethods;
        if (m == null) {
            synchronized (lock) {
                u();
                m = ptrMethodsSupplier == null ? Map.of() : ptrMethodsSupplier.get();
                ptrMethods = m;
            }
        }
        return m;
    }

    /**
     * The itab of this dynamic type for interface type {@code iface}, or null when the type
     * does not implement it. Computed at the first conversion, assertion or call and cached;
     * itabs are immutable (SPEC §3.3).
     */
    @TruffleBoundary
    public Itab itab(GoType iface) {
        Object cached = itabs.get(iface);
        if (cached == null) {
            cached = buildItab(iface);
            itabs.put(iface, cached);
        }
        return cached == NO_ITAB ? null : (Itab) cached;
    }

    private Object buildItab(GoType iface) {
        IMethod[] want = iface.u().imethods;
        Method[] impl = new Method[want.length];
        for (int i = 0; i < want.length; i++) {
            impl[i] = findMethod(want[i].key);
            if (impl[i] == null || !impl[i].hasSig(want[i].sig)) {
                return NO_ITAB;
            }
        }
        return new Itab(this, iface, impl);
    }

    /** The first method of {@code iface} this type lacks (for the conversion panic), or null. */
    @TruffleBoundary
    public String missingMethod(GoType iface) {
        for (IMethod m : iface.u().imethods) {
            Method have = findMethod(m.key);
            if (have == null || !have.hasSig(m.sig)) {
                return m.name;
            }
        }
        return null;
    }

    // ---------------------------------------------------------------- display

    /** The type as gc's runtime prints it (package names, not import paths). */
    @TruffleBoundary
    public String str() {
        String s = str;
        if (s == null) {
            s = buildStr();
            str = s;
        }
        return s;
    }

    private String buildStr() {
        switch (kind) {
            case NAMED -> {
                StringBuilder sb = new StringBuilder();
                if (!pkg.isEmpty()) {
                    sb.append(pkgName != null ? pkgName : pkg.substring(pkg.lastIndexOf('/') + 1)).append('.');
                }
                int dot = name.indexOf('·');
                sb.append(dot < 0 ? name : name.substring(0, dot));
                if (targs.length > 0) {
                    sb.append('[');
                    for (int i = 0; i < targs.length; i++) {
                        sb.append(i > 0 ? "," : "").append(targs[i].str());
                    }
                    sb.append(']');
                }
                return sb.toString();
            }
            case POINTER -> {
                return "*" + elem.str();
            }
            case SLICE -> {
                return "[]" + elem.str();
            }
            case ARRAY -> {
                return "[" + len + "]" + elem.str();
            }
            case MAP -> {
                return "map[" + mapKey.str() + "]" + elem.str();
            }
            case CHAN -> {
                return (dir == 3 ? "<-chan " : dir == 2 ? "chan<- " : "chan ") + elem.str();
            }
            case STRUCT -> {
                StringBuilder sb = new StringBuilder("struct {");
                for (int i = 0; i < fields.length; i++) {
                    sb.append(i > 0 ? "; " : " ");
                    if (!fields[i].embedded) {
                        sb.append(fields[i].name).append(' ');
                    }
                    sb.append(fields[i].type.str());
                }
                return sb.append(fields.length > 0 ? " }" : "}").toString();
            }
            case FUNC -> {
                return "func" + sigStr();
            }
            case INTERFACE -> {
                StringBuilder sb = new StringBuilder("interface {");
                for (int i = 0; i < imethods.length; i++) {
                    sb.append(i > 0 ? "; " : " ").append(imethods[i].name).append(imethods[i].sig.sigStr());
                }
                return sb.append(imethods.length > 0 ? " }" : "}").toString();
            }
            default -> {
                return key;
            }
        }
    }

    private String sigStr() {
        StringBuilder sb = new StringBuilder("(");
        for (int i = 0; i < params.length; i++) {
            sb.append(i > 0 ? ", " : "");
            if (variadic && i == params.length - 1) {
                sb.append("...").append(params[i].elem.str());
            } else {
                sb.append(params[i].str());
            }
        }
        sb.append(')');
        if (results.length == 1) {
            sb.append(' ').append(results[0].str());
        } else if (results.length > 1) {
            sb.append(" (");
            for (int i = 0; i < results.length; i++) {
                sb.append(i > 0 ? ", " : "").append(results[i].str());
            }
            sb.append(')');
        }
        return sb.toString();
    }

    @Override
    public String toString() {
        return key;
    }
}
