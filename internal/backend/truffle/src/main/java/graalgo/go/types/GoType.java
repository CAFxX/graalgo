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

    public int nPrim() {
        layout();
        return nPrim;
    }

    public int nRef() {
        layout();
        return nRef;
    }

    private void layout() {
        if (kind == NAMED) {
            GoType t = u();
            nPrim = t.nPrim();
            nRef = t.nRef();
            return;
        }
        if (!laidOut) {
            computeLayout();
        }
    }

    @TruffleBoundary
    private void computeLayout() {
        synchronized (lock) {
            computeLayoutLocked();
        }
    }

    private void computeLayoutLocked() {
        if (laidOut) {
            return;
        }
        int p = 0;
        int r = 0;
        switch (carrier()) {
            case C_I64, C_F64, C_BOOL -> p = 1;
            case C_REF -> r = 1;
            default -> {
                if (kind == ARRAY) {
                    p = Math.toIntExact(len * elem.nPrim());
                    r = Math.toIntExact(len * elem.nRef());
                } else {
                    for (Field f : fields) {
                        f.primOff = p;
                        f.refOff = r;
                        p += f.type.nPrim();
                        r += f.type.nRef();
                    }
                }
            }
        }
        nPrim = p;
        nRef = r;
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
        return new Flat(this, np == 0 ? Flat.NO_PRIMS : new long[np], z.length == 0 ? Flat.NO_REFS : z.clone());
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

    /** gc's size of a value in bytes (amd64); feeds the slice growth size classes. */
    @TruffleBoundary
    public long gcSize() {
        GoType t = u();
        switch (t.kind) {
            case BOOL, INT8, UINT8:
                return 1;
            case INT16, UINT16:
                return 2;
            case INT32, UINT32, FLOAT32:
                return 4;
            case COMPLEX128, STRING, INTERFACE:
                return 16;
            case SLICE:
                return 24;
            case ARRAY:
                return t.len * t.elem.gcSize();
            case STRUCT: {
                long off = 0;
                long align = 1;
                for (Field f : t.fields) {
                    long a = f.type.gcAlign();
                    off = (off + a - 1) / a * a + f.type.gcSize();
                    align = Math.max(align, a);
                }
                return (off + align - 1) / align * align;
            }
            default:
                return 8;
        }
    }

    @TruffleBoundary
    public long gcAlign() {
        GoType t = u();
        switch (t.kind) {
            case ARRAY:
                return t.elem.gcAlign();
            case STRUCT: {
                long align = 1;
                for (Field f : t.fields) {
                    align = Math.max(align, f.type.gcAlign());
                }
                return align;
            }
            case COMPLEX64:
                return 4;
            default:
                return Math.min(8, Math.max(1, t.gcSize()));
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
        if (kind == NAMED) {
            return valueMethods().get(methodKey);
        } else if (kind == POINTER && elem.kind == NAMED) {
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
            if (impl[i] == null) {
                return NO_ITAB;
            }
        }
        return new Itab(this, iface, impl);
    }

    /** The first method of {@code iface} this type lacks (for the conversion panic), or null. */
    @TruffleBoundary
    public String missingMethod(GoType iface) {
        for (IMethod m : iface.u().imethods) {
            if (findMethod(m.key) == null) {
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
