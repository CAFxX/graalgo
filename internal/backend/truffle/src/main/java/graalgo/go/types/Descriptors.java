package graalgo.go.types;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import graalgo.go.types.GoType.Field;
import graalgo.go.types.GoType.IMethod;
import graalgo.go.types.GoType.Leaf;
import graalgo.go.values.Flat;
import graalgo.go.values.Slice;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.IdentityHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Descriptor images (toolexec-design §3.4, §5.6, W9a; abi-spike §1.3): for a Go type, the type
 * descriptor gc's compiler would emit — the kind-specific {@code internal/abi} struct, the
 * trailing {@code UncommonType}, the parameter, field or interface-method array and the method
 * array, in gc's order (cmd/compile/internal/reflectdata.writeType) — as an ordinary addressable
 * {@link Flat}. Its layout type is a struct of {@code internal/abi}'s own types, so upstream's
 * {@code internal/abi} and {@code internal/reflectlite} read it through the checked
 * {@code (*abi.Type)(unsafe.Pointer(...))} conversions of the pointer model (ADR-0015) like any
 * other object.
 *
 * <p>Nothing here knows an offset of a descriptor struct: every field is written at the gc byte
 * offset the bundle's own {@code internal/abi} gives its name, so a release that moves a field
 * needs no change. A release that renames or drops one fails loudly at the first image.
 *
 * <p>An interface value keeps holding its {@link GoType} (typed dispatch, itabs and type
 * switches never come here); the image is what the <em>type word</em> of an interface header
 * reads as through a view ({@link #typeWord}), and {@link #typeOf} is the way back. Images are
 * built on first use and are canonical per type, so descriptor pointer equality is type
 * identity. {@code NameOff}/{@code TypeOff}/{@code TextOff} values are ids in this table's
 * offset list, negative as the ones gc's runtime hands out for descriptors outside a module
 * ({@code reflectOffs}); {@link #resolve} is {@code resolveNameOff}/{@code resolveTypeOff}.
 *
 * <p>Not built here (W9b): method code offsets ({@code Method.Ifn}/{@code Tfn} are gc's
 * "unreachable" value -1).
 */
public final class Descriptors {
    /** What the loaded program provides: {@code internal/abi}'s types and func values. */
    public interface Host {
        /** The defined type {@code internal/abi.name}. */
        GoType abiType(String name);

        /** The name of the package with import path {@code path}. */
        String packageName(String path);

        /** The defined type {@code path.name} of a package of the program; null if the package is not in it. */
        GoType namedType(String path, String name);

        /** A Go func value {@code func(p, q unsafe.Pointer) bool} comparing two values of type {@code t}. */
        Object equalFunc(GoType t);

        /** A Go func value {@code func(p unsafe.Pointer, seed uintptr) uintptr} hashing a value of type {@code t}. */
        Object hashFunc(GoType t);
    }

    static final int TFLAG_UNCOMMON = 1, TFLAG_EXTRA_STAR = 2, TFLAG_NAMED = 4, TFLAG_REGULAR_MEMORY = 8,
            TFLAG_GC_MASK_ON_DEMAND = 16, TFLAG_DIRECT_IFACE = 32;
    static final int K_ARRAY = 17, K_CHAN = 18, K_FUNC = 19, K_INTERFACE = 20, K_MAP = 21, K_POINTER = 22, K_SLICE = 23,
            K_STRING = 24, K_STRUCT = 25, K_UNSAFE_POINTER = 26;
    /** internal/abi: MapGroupSlots, MapMaxKeyBytes/MapMaxElemBytes, MaxPtrmaskBytes, the map flags. */
    static final int MAP_GROUP_SLOTS = 8, MAP_MAX_INLINE = 128, MAX_PTRMASK_BYTES = 16;
    static final int MAP_NEED_KEY_UPDATE = 1, MAP_HASH_MIGHT_PANIC = 2, MAP_INDIRECT_KEY = 4, MAP_INDIRECT_ELEM = 8;
    /** cmd/compile's algorithm classes, as far as descriptors depend on them. */
    static final int ALG_NOEQ = 0, ALG_MEM = 1, ALG_SPECIAL = 2, ALG_OTHER = 3;

    /**
     * Test hook of the imgcheck gate (test/imgcheck): the name of one deliberate builder error
     * ({@code kind}, {@code size}, {@code offset}, {@code nameoff}, {@code methods}), which the
     * gate must report. Empty in every other run.
     */
    private static final String MUTATE = System.getProperty("graalgo.descriptors.mutate", "");

    private final TypeTable table;
    private volatile Host host;
    /** The offset table: entry {@code i} is id {@code -(i + 2)}; a {@link GoType} or a name object. */
    private final List<Object> offs = new ArrayList<>();
    private final Map<Object, Integer> offIds = new IdentityHashMap<>();
    private final Map<String, Flat> names = new HashMap<>();
    private final Map<GoType, Flat> building = new IdentityHashMap<>();
    /** Types gc marks Noalg: the map group types (no equality function, not regular memory). */
    private final Set<GoType> noalg = Collections.newSetFromMap(new IdentityHashMap<>());
    private final Map<String, GoType> abiTypes = new HashMap<>();

    Descriptors(TypeTable table) {
        this.table = table;
    }

    /** Connects the table to a program that links {@code internal/abi}; the first one stays. */
    public void attach(Host h) {
        synchronized (table) {
            if (host == null) {
                host = h;
            }
        }
    }

    /**
     * The type word of an interface value of dynamic type {@code t}: its descriptor image, or
     * the type itself in a program that does not link {@code internal/abi} (nothing there can
     * read a descriptor; the word is still a stable, comparable pointer).
     */
    public Object typeWord(GoType t) {
        if (host == null) {
            return t;
        }
        Flat img = t.image;
        return img != null ? img : image(t);
    }

    /** The type a type word stands for, or null when {@code word} is not a type word. */
    public static GoType typeOf(Object word) {
        if (word instanceof GoType t) {
            return t;
        } else if (word instanceof Flat f && f.type != null && f.type.imageLayout) {
            return (GoType) f.refs[f.refs.length - 1];
        }
        return null;
    }

    /** {@code resolveNameOff}/{@code resolveTypeOff}: the object an offset id names; nil for 0 and -1. */
    @TruffleBoundary
    public Object resolve(long id) {
        if (id == 0 || id == -1) {
            return null;
        }
        Object o;
        synchronized (table) {
            long i = -id - 2;
            if (i < 0 || i >= offs.size()) {
                return null;
            }
            o = offs.get((int) i);
        }
        return o instanceof GoType t ? image(t) : o;
    }

    /**
     * The defined type {@code path.name} of a package of the program, or null when the program
     * does not link {@code internal/abi} or that package.
     */
    public GoType programType(String path, String name) {
        Host h = host;
        return h == null ? null : h.namedType(path, name);
    }

    /** The descriptor image of {@code t}. */
    @TruffleBoundary
    public Flat image(GoType t) {
        Flat img = t.image;
        if (img != null) {
            return img;
        }
        synchronized (table) {
            if (host == null) {
                throw new IllegalStateException("type descriptor of " + t.key + ": internal/abi is not in the program");
            }
            boolean outer = building.isEmpty();
            try {
                img = get(t);
                if (outer) {
                    // Publish the whole closure at once: no reader sees a half-filled image.
                    for (Map.Entry<GoType, Flat> e : building.entrySet()) {
                        e.getKey().image = e.getValue();
                    }
                }
            } finally {
                if (outer) {
                    building.clear();
                }
            }
            return img;
        }
    }

    private Flat get(GoType t) {
        Flat img = t.image;
        if (img == null) {
            img = building.get(t);
        }
        return img != null ? img : build(t);
    }

    // ---------------------------------------------------------------- internal/abi's own layout

    private GoType abi(String name) {
        GoType t = abiTypes.get(name);
        if (t == null) {
            t = host.abiType(name);
            t.gcSize();
            abiTypes.put(name, t);
        }
        return t;
    }

    /** gc's byte offset of field {@code name} of the abi struct {@code s}. */
    private static long fo(GoType s, String name) {
        for (Field f : s.u().fields) {
            if (f.name.equals(name)) {
                return f.gcOff;
            }
        }
        throw new IllegalStateException("internal/abi: " + s.key + " has no field " + name);
    }

    private static boolean has(GoType s, String name) {
        for (Field f : s.u().fields) {
            if (f.name.equals(name)) {
                return true;
            }
        }
        return false;
    }

    private static Leaf leaf(Flat img, long off) {
        Leaf l = img.type.leafAt(off);
        if (l == null || l.gcOff != off) {
            throw new IllegalStateException("descriptor layout " + img.type.key + ": no leaf at byte " + off);
        }
        return l;
    }

    /** Stores an integer at gc byte {@code off}, in the slot form of the leaf found there. */
    private static void putInt(Flat img, long off, long v) {
        Leaf l = leaf(img, off);
        if (l.type.nRef() != 0) {
            throw new IllegalStateException("descriptor layout " + img.type.key + ": reference leaf at byte " + off);
        } else if (l.type.nByte() == 1) {
            img.bytes[l.byt] = (byte) v;
            return;
        }
        img.prims[l.prim] = switch (l.type.kind) {
            case GoType.INT16 -> (short) v;
            case GoType.UINT16 -> v & 0xFFFF;
            case GoType.INT32 -> (int) v;
            case GoType.UINT32 -> v & 0xFFFFFFFFL;
            default -> v;
        };
    }

    private static void putRef(Flat img, long off, Object v) {
        Leaf l = leaf(img, off);
        if (l.type.nRef() != 1) {
            throw new IllegalStateException("descriptor layout " + img.type.key + ": scalar leaf at byte " + off);
        }
        img.refs[l.ref] = v;
    }

    /** A slice header at {@code off} over the {@code n} elements that start at byte {@code data} of the image. */
    private static void putSlice(Flat img, long off, long data, int n) {
        if (n > 0) {
            Leaf first = leaf(img, data);
            putRef(img, off, new Slice(img, first.prim, first.ref, first.byt, n, n));
        }
    }

    // ---------------------------------------------------------------- offsets and names

    /**
     * {@code reflect.addReflectOff}: the offset id of an object reflect wants to name by offset
     * (a descriptor, or a name it encoded itself); {@link #resolve} maps the id back.
     */
    @TruffleBoundary
    public int addOff(Object ptr) {
        GoType t = typeOf(ptr);
        synchronized (table) {
            return offId(t != null ? t : ptr);
        }
    }

    private int offId(Object target) {
        Integer id = offIds.get(target);
        if (id == null) {
            offs.add(target);
            id = -(offs.size() + 1);
            offIds.put(target, id);
        }
        return id;
    }

    private static int varint(byte[] buf, int p, int n) {
        while (true) {
            int b = n & 0x7f;
            n >>>= 7;
            if (n == 0) {
                buf[p++] = (byte) b;
                return p;
            }
            buf[p++] = (byte) (b | 0x80);
        }
    }

    /**
     * gc's name encoding (reflectdata.dnameData): flag byte, varint length, bytes, optional
     * varint and tag, and for a name qualified by a package the offset id of the path's name.
     */
    private Flat name(String name, String tag, boolean exported, boolean embedded, String pkg) {
        String key = name + '\0' + tag + '\0' + (exported ? 1 : 0) + (embedded ? 1 : 0) + '\0' + (pkg == null ? "" : pkg);
        Flat cached = names.get(key);
        if (cached != null) {
            return cached;
        }
        byte[] n = name.getBytes(StandardCharsets.UTF_8);
        byte[] g = tag.getBytes(StandardCharsets.UTF_8);
        byte[] buf = new byte[1 + 5 + n.length + 5 + g.length + 4];
        int p = 0;
        buf[p++] = (byte) ((exported ? 1 : 0) | (g.length > 0 ? 2 : 0) | (pkg != null ? 4 : 0) | (embedded ? 8 : 0));
        p = varint(buf, p, n.length);
        System.arraycopy(n, 0, buf, p, n.length);
        p += n.length;
        if (g.length > 0) {
            p = varint(buf, p, g.length);
            System.arraycopy(g, 0, buf, p, g.length);
            p += g.length;
        }
        if (pkg != null) {
            int id = offId(name(pkg, "", false, false, null));
            for (int i = 0; i < 4; i++) {
                buf[p++] = (byte) (id >>> (8 * i));
            }
        }
        Flat f = new Flat(table.basic(GoType.UINT8).backing(), Flat.NO_PRIMS, Flat.NO_REFS, java.util.Arrays.copyOf(buf, p));
        names.put(key, f);
        return f;
    }

    static boolean isExported(String name) {
        return !name.isEmpty() && Character.isUpperCase(name.codePointAt(0));
    }

    // ---------------------------------------------------------------- gc's facts about a type

    private static boolean isBasic(GoType t) {
        return t.kind <= GoType.UNSAFE_POINTER;
    }

    /** Whether gc's type has a symbol: a defined type or a predeclared one. */
    private static boolean hasSym(GoType t) {
        return t.kind == GoType.NAMED || isBasic(t);
    }

    /** The {@code internal/abi.Kind} of a type (what a descriptor's {@code Kind_} holds). */
    public static int abiKind(GoType t) {
        int k = t.u().kind;
        return switch (k) {
            case GoType.STRING -> K_STRING;
            case GoType.UNSAFE_POINTER -> K_UNSAFE_POINTER;
            case GoType.POINTER -> K_POINTER;
            case GoType.SLICE -> K_SLICE;
            case GoType.ARRAY -> K_ARRAY;
            case GoType.MAP -> K_MAP;
            case GoType.CHAN -> K_CHAN;
            case GoType.STRUCT -> K_STRUCT;
            case GoType.FUNC -> K_FUNC;
            case GoType.INTERFACE -> K_INTERFACE;
            default -> {
                if (k < GoType.BOOL || k > GoType.COMPLEX128) {
                    throw new IllegalStateException("no type descriptor for " + t.key);
                }
                yield k; // GIR's BasicKind numbers Bool..Complex128 as internal/abi does
            }
        };
    }

    private static String kindStruct(int abiKind) {
        return switch (abiKind) {
            case K_ARRAY -> "ArrayType";
            case K_CHAN -> "ChanType";
            case K_FUNC -> "FuncType";
            case K_INTERFACE -> "InterfaceType";
            case K_MAP -> "MapType";
            case K_POINTER -> "PtrType";
            case K_SLICE -> "SliceType";
            case K_STRUCT -> "StructType";
            default -> "Type";
        };
    }

    /** gc's PtrDataSize: the length of the prefix of a value that can hold pointers. */
    public static long ptrBytes(GoType t) {
        GoType u = t.u();
        switch (u.kind) {
            case GoType.STRING, GoType.SLICE, GoType.POINTER, GoType.UNSAFE_POINTER, GoType.MAP, GoType.CHAN, GoType.FUNC:
                return 8;
            case GoType.INTERFACE:
                return 16;
            case GoType.ARRAY: {
                long x = ptrBytes(u.elem);
                return x == 0 || u.len == 0 ? 0 : u.elem.gcSize() * (u.len - 1) + x;
            }
            case GoType.STRUCT:
                u.gcSize();
                for (int i = u.fields.length - 1; i >= 0; i--) {
                    long x = ptrBytes(u.fields[i].type);
                    if (x > 0) {
                        return u.fields[i].gcOff + x;
                    }
                }
                return 0;
            default:
                return 0;
        }
    }

    /** Sets the bit of every pointer word of a {@code t} at byte {@code off} (reflectdata.fillptrmask). */
    private static void fillMask(GoType t, long off, byte[] mask) {
        GoType u = t.u();
        switch (u.kind) {
            case GoType.STRING, GoType.SLICE, GoType.POINTER, GoType.UNSAFE_POINTER, GoType.MAP, GoType.CHAN, GoType.FUNC:
                mask[(int) (off / 64)] |= (byte) (1 << (off / 8 % 8));
                break;
            case GoType.INTERFACE:
                // The type or itab word points outside the heap: only the data word is a
                // pointer for the collector (PtrBytes still covers both words).
                mask[(int) ((off + 8) / 64)] |= (byte) (1 << ((off + 8) / 8 % 8));
                break;
            case GoType.ARRAY:
                if (ptrBytes(u.elem) > 0) {
                    for (long i = 0; i < u.len; i++) {
                        fillMask(u.elem, off + i * u.elem.gcSize(), mask);
                    }
                }
                break;
            case GoType.STRUCT:
                u.gcSize();
                for (Field f : u.fields) {
                    fillMask(f.type, off + f.gcOff, mask);
                }
                break;
            default:
                break;
        }
    }

    private static int strongest(int a, int b) {
        // types.algPriority: Noeq over Special over the others over Mem.
        int pa = a == ALG_NOEQ ? 3 : a == ALG_SPECIAL ? 2 : a == ALG_OTHER ? 1 : 0;
        int pb = b == ALG_NOEQ ? 3 : b == ALG_SPECIAL ? 2 : b == ALG_OTHER ? 1 : 0;
        return pb > pa ? b : a;
    }

    /** cmd/compile/internal/types' algorithm class of a type (CalcSize, CalcStructSize, CalcArraySize). */
    static int alg(GoType t) {
        GoType u = t.u();
        switch (u.kind) {
            case GoType.SLICE, GoType.MAP, GoType.FUNC:
                return ALG_NOEQ;
            case GoType.FLOAT32, GoType.FLOAT64, GoType.COMPLEX64, GoType.COMPLEX128, GoType.STRING, GoType.INTERFACE:
                return ALG_OTHER;
            case GoType.ARRAY: {
                int a = alg(u.elem);
                if (a == ALG_MEM || a == ALG_NOEQ) {
                    return a;
                }
                return u.len == 0 ? ALG_MEM : u.len == 1 ? a : ALG_SPECIAL;
            }
            case GoType.STRUCT: {
                Field[] fs = u.fields;
                long size = u.gcSize();
                if (fs.length == 1 && !fs[0].name.equals("_")) {
                    return alg(fs[0].type);
                }
                int out = ALG_MEM;
                for (int i = 0; i < fs.length; i++) {
                    int a = alg(fs[i].type);
                    if (a == ALG_MEM) {
                        long end = fs[i].gcOff + fs[i].type.gcSize();
                        long next = i + 1 < fs.length ? fs[i + 1].gcOff : size;
                        if (fs[i].name.equals("_") || end != next) {
                            a = ALG_SPECIAL;
                        }
                    } else if (a != ALG_NOEQ) {
                        a = ALG_SPECIAL;
                    }
                    out = strongest(out, a);
                }
                return out;
            }
            default:
                return ALG_MEM;
        }
    }

    private static boolean needKeyUpdate(GoType t) {
        GoType u = t.u();
        switch (u.kind) {
            case GoType.FLOAT32, GoType.FLOAT64, GoType.COMPLEX64, GoType.COMPLEX128, GoType.INTERFACE, GoType.STRING:
                return true;
            case GoType.ARRAY:
                return needKeyUpdate(u.elem);
            case GoType.STRUCT:
                for (Field f : u.fields) {
                    if (needKeyUpdate(f.type)) {
                        return true;
                    }
                }
                return false;
            default:
                return false;
        }
    }

    private static boolean hashMightPanic(GoType t) {
        GoType u = t.u();
        switch (u.kind) {
            case GoType.INTERFACE:
                return true;
            case GoType.ARRAY:
                return hashMightPanic(u.elem);
            case GoType.STRUCT:
                for (Field f : u.fields) {
                    if (hashMightPanic(f.type)) {
                        return true;
                    }
                }
                return false;
            default:
                return false;
        }
    }

    /** The import path of the package gc attributes an (unnamed or named) type to, or null (reflectdata.typePkg). */
    private static String typePkg(GoType t) {
        GoType s = t;
        if (!hasSym(t) && (t.kind == GoType.ARRAY || t.kind == GoType.SLICE || t.kind == GoType.POINTER
                || t.kind == GoType.CHAN)) {
            s = t.elem;
        }
        if (s.kind == GoType.NAMED) {
            return s.pkg.isEmpty() ? null : s.pkg;
        }
        return s.kind == GoType.UNSAFE_POINTER ? "unsafe" : null;
    }

    /** The name of the symbol of a type that has one (its package aside). */
    private static String symName(GoType t) {
        return t.kind == GoType.NAMED ? t.name : t.kind == GoType.UNSAFE_POINTER ? "Pointer" : t.key;
    }

    // ---------------------------------------------------------------- gc's type strings

    /** cmd/internal/objabi.PathToPrefix: the form of an import path inside a symbol name. */
    static String pathToPrefix(String s) {
        byte[] in = s.getBytes(StandardCharsets.UTF_8);
        int slash = -1;
        for (int i = 0; i < in.length; i++) {
            slash = in[i] == '/' ? i : slash;
        }
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < in.length; i++) {
            int c = in[i] & 0xFF;
            if (c <= ' ' || c == '.' && i > slash || c == '%' || c == '"' || c >= 0x7F) {
                sb.append('%').append(Character.forDigit(c >> 4, 16)).append(Character.forDigit(c & 15, 16));
            } else {
                sb.append((char) c);
            }
        }
        return sb.toString();
    }

    /** strconv.Quote for a struct tag. */
    static String quote(String s) {
        StringBuilder sb = new StringBuilder("\"");
        for (int i = 0; i < s.length();) {
            int c = s.codePointAt(i);
            i += Character.charCount(c);
            switch (c) {
                case '"' -> sb.append("\\\"");
                case '\\' -> sb.append("\\\\");
                case '\n' -> sb.append("\\n");
                case '\t' -> sb.append("\\t");
                case '\r' -> sb.append("\\r");
                case 7 -> sb.append("\\a");
                case '\b' -> sb.append("\\b");
                case '\f' -> sb.append("\\f");
                case 11 -> sb.append("\\v");
                default -> {
                    if (c < ' ' || c == 0x7F) {
                        sb.append(String.format("\\x%02x", c));
                    } else {
                        sb.appendCodePoint(c);
                    }
                }
            }
        }
        return sb.append('"').toString();
    }

    /**
     * The type as gc writes it: {@code link} false, the descriptor's string (types.NameString:
     * package names, local types without their index); true, the link string whose hash is the
     * descriptor's {@code Hash} (types.LinkString: escaped import paths, qualified unexported
     * names).
     */
    String gcString(GoType t, boolean link) {
        StringBuilder sb = new StringBuilder();
        write(sb, t, link);
        return sb.toString();
    }

    private void write(StringBuilder sb, GoType t, boolean link) {
        switch (t.kind) {
            case GoType.NAMED -> {
                if (!t.pkg.isEmpty()) {
                    sb.append(link ? pathToPrefix(t.pkg)
                            : t.pkgName != null ? t.pkgName : t.pkg.substring(t.pkg.lastIndexOf('/') + 1)).append('.');
                }
                // A local type is Name·N (gc's own numbering, from the frontend); gc keeps the
                // suffix after the type arguments and drops it from the descriptor's string.
                int dot = t.name.indexOf('·');
                sb.append(dot < 0 ? t.name : t.name.substring(0, dot));
                if (t.targs.length > 0) {
                    sb.append('[');
                    for (int i = 0; i < t.targs.length; i++) {
                        sb.append(i > 0 ? "," : "");
                        write(sb, t.targs[i], link);
                    }
                    sb.append(']');
                }
                if (link && dot >= 0) {
                    sb.append(t.name, dot, t.name.length());
                }
            }
            case GoType.POINTER -> {
                sb.append('*');
                write(sb, t.elem, link);
            }
            case GoType.SLICE -> {
                sb.append("[]");
                write(sb, t.elem, link);
            }
            case GoType.ARRAY -> {
                sb.append('[').append(t.len).append(']');
                write(sb, t.elem, link);
            }
            case GoType.MAP -> {
                sb.append("map[");
                write(sb, t.mapKey, link);
                sb.append(']');
                write(sb, t.elem, link);
            }
            case GoType.CHAN -> {
                sb.append(t.dir == 3 ? "<-chan " : t.dir == 2 ? "chan<- " : "chan ");
                boolean paren = t.dir != 3 && t.elem.kind == GoType.CHAN && t.elem.dir == 3;
                sb.append(paren ? "(" : "");
                write(sb, t.elem, link);
                sb.append(paren ? ")" : "");
            }
            case GoType.STRUCT -> {
                sb.append("struct {");
                for (int i = 0; i < t.fields.length; i++) {
                    Field f = t.fields[i];
                    sb.append(i > 0 ? "; " : " ");
                    if (!f.embedded || link && !embeddedAsItsType(f)) {
                        if (link && !isExported(f.name) && !f.pkg.isEmpty()) {
                            sb.append(pathToPrefix(f.pkg)).append('.');
                        }
                        // The link string writes an embedded field whose name is not its
                        // type's as "name = type" (types.fldconv).
                        sb.append(f.name).append(f.embedded ? " = " : " ");
                    }
                    write(sb, f.type, link);
                    if (!f.tag.isEmpty()) {
                        sb.append(' ').append(quote(f.tag));
                    }
                }
                sb.append(t.fields.length > 0 ? " }" : "}");
            }
            case GoType.FUNC -> {
                sb.append("func");
                writeSig(sb, t, link);
            }
            case GoType.INTERFACE -> {
                sb.append("interface {");
                List<IMethod> ms = sortedIMethods(t);
                for (int i = 0; i < ms.size(); i++) {
                    IMethod m = ms.get(i);
                    sb.append(i > 0 ? "; " : " ");
                    String pkg = imethodPkg(m);
                    if (!pkg.isEmpty()) {
                        // gc qualifies an unexported method in both forms (types.tconv).
                        sb.append(link ? pathToPrefix(pkg) : host.packageName(pkg)).append('.');
                    }
                    sb.append(m.name);
                    writeSig(sb, m.sig, link);
                }
                sb.append(ms.isEmpty() ? "}" : " }");
            }
            default -> sb.append(t.key);
        }
    }

    /**
     * Whether the link string writes embedded field {@code f} as just its type: the field's
     * symbol is the type's own, or the type's name is exported and is the field's name. An
     * embedded predeclared type ({@code struct { int }}: the field is the package's
     * {@code int}), an instantiated type and an alias of an unnamed type are written with the
     * field name.
     */
    private static boolean embeddedAsItsType(Field f) {
        GoType typ = f.type.kind == GoType.POINTER ? f.type.elem : f.type;
        if (!hasSym(typ) || typ.kind == GoType.NAMED && typ.targs.length > 0) {
            return false;
        }
        String tname = symName(typ);
        String tpkg = typ.kind == GoType.NAMED ? typ.pkg : typ.kind == GoType.UNSAFE_POINTER ? "unsafe" : "";
        return f.name.equals(tname) && (isExported(tname) || f.pkg.equals(tpkg));
    }

    private void writeSig(StringBuilder sb, GoType s, boolean link) {
        sb.append('(');
        for (int i = 0; i < s.params.length; i++) {
            sb.append(i > 0 ? ", " : "");
            if (s.variadic && i == s.params.length - 1) {
                sb.append("...");
                write(sb, s.params[i].elem, link);
            } else {
                write(sb, s.params[i], link);
            }
        }
        sb.append(')');
        if (s.results.length == 1) {
            sb.append(' ');
            write(sb, s.results[0], link);
        } else if (s.results.length > 1) {
            sb.append(" (");
            for (int i = 0; i < s.results.length; i++) {
                sb.append(i > 0 ? ", " : "");
                write(sb, s.results[i], link);
            }
            sb.append(')');
        }
    }

    /** cmd/compile's TypeHash: the first four bytes, little-endian, of cmd/internal/hash's sum of the link string. */
    static int typeHash(String link) {
        try {
            byte[] h = MessageDigest.getInstance("SHA-256").digest(link.getBytes(StandardCharsets.UTF_8));
            h[0] ^= (byte) 0xff;
            return (h[0] & 0xff) | (h[1] & 0xff) << 8 | (h[2] & 0xff) << 16 | (h[3] & 0xff) << 24;
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalStateException(e);
        }
    }

    // ---------------------------------------------------------------- method tables

    /** One method of a descriptor's method table. */
    private record M(String name, String pkg, GoType sig) {}

    /** gc's order (types.CompareSyms): exported names first, then by name, then by package. */
    private static int compareSyms(String an, String ap, String bn, String bp) {
        boolean ae = isExported(an);
        boolean be = isExported(bn);
        if (ae != be) {
            return ae ? -1 : 1;
        }
        int c = an.compareTo(bn);
        return c != 0 || ae ? c : ap.compareTo(bp);
    }

    private static String imethodPkg(IMethod m) {
        return m.key.length() > m.name.length() ? m.key.substring(0, m.key.length() - m.name.length() - 1) : "";
    }

    private static List<IMethod> sortedIMethods(GoType iface) {
        List<IMethod> ms = new ArrayList<>(List.of(iface.u().imethods));
        ms.sort((a, b) -> compareSyms(a.name, imethodPkg(a), b.name, imethodPkg(b)));
        return ms;
    }

    /** The methods gc lists for {@code t} in its uncommon section, in gc's order. */
    private static List<M> methods(GoType t) {
        List<M> ms = new ArrayList<>();
        for (Method m : t.methodSet().values()) {
            GoType sig = m.sig();
            if (sig == null) {
                continue; // a generic method: not callable through an interface, not in gc's table
            }
            int dot = m.key.lastIndexOf('.');
            ms.add(new M(m.key.substring(dot + 1), dot < 0 ? "" : m.key.substring(0, dot), sig));
        }
        ms.sort((a, b) -> compareSyms(a.name, a.pkg, b.name, b.pkg));
        if (MUTATE.equals("methods")) {
            Collections.reverse(ms);
        }
        return ms;
    }

    // ---------------------------------------------------------------- the builder

    /** The group type of a map (reflectdata.MapGroupType, the interleaved layout). */
    private GoType mapGroup(GoType m) {
        GoType key = m.mapKey.gcSize() > MAP_MAX_INLINE ? table.pointer(m.mapKey) : m.mapKey;
        GoType elem = m.elem.gcSize() > MAP_MAX_INLINE ? table.pointer(m.elem) : m.elem;
        GoType slot = table.struct(new Field[] {new Field("key", "", key, false, ""), new Field("elem", "", elem, false, "")});
        GoType slots = table.array(MAP_GROUP_SLOTS, slot);
        GoType group = table.struct(new Field[] {new Field("ctrl", "", table.basic(GoType.UINT64), false, ""),
                new Field("slots", "", slots, false, "")});
        noalg.add(slot);
        noalg.add(slots);
        noalg.add(group);
        return group;
    }

    private Flat build(GoType t) {
        GoType u = t.u();
        int kind = abiKind(t);
        GoType abiType = abi("Type");
        GoType ks = abi(kindStruct(kind));
        GoType unc = abi("UncommonType");
        GoType method = abi("Method");
        List<M> ms = methods(t);
        boolean extra = hasSym(t) || !ms.isEmpty();

        // The variable-length data between the uncommon section and the method table.
        GoType dataElem = null;
        int nData = 0;
        List<IMethod> ims = List.of();
        switch (kind) {
            case K_FUNC -> {
                dataElem = table.pointer(abiType);
                nData = u.params.length + u.results.length;
            }
            case K_INTERFACE -> {
                dataElem = abi("Imethod");
                ims = sortedIMethods(u);
                nData = ims.size();
            }
            case K_STRUCT -> {
                dataElem = abi("StructField");
                nData = u.fields.length;
            }
            default -> { }
        }

        // writeType's sections: [0,B) kind struct, [B,C) uncommon, [C,D) data, [D,E) methods.
        long b = ks.gcSize();
        long c = b + (extra ? unc.gcSize() : 0);
        long dataAdd = nData == 0 ? 0 : nData * dataElem.gcSize();
        long d = c + dataAdd;
        List<Field> lf = new ArrayList<>();
        lf.add(new Field("k", "", ks, false, ""));
        if (extra) {
            lf.add(new Field("u", "", unc, false, ""));
        }
        if (nData > 0) {
            lf.add(new Field("d", "", table.array(nData, dataElem), false, ""));
        }
        if (!ms.isEmpty()) {
            lf.add(new Field("m", "", table.array(ms.size(), method), false, ""));
        }
        lf.add(new Field("t", "", table.basic(GoType.UNSAFE_POINTER), false, ""));
        GoType layout = table.imageLayout(lf.toArray(new Field[0]));
        layout.gcSize();
        for (Field f : layout.fields) {
            long want = switch (f.name) {
                case "k" -> 0;
                case "u" -> b;
                case "d" -> c;
                case "m" -> d;
                default -> f.gcOff;
            };
            if (f.gcOff != want) {
                throw new IllegalStateException("descriptor layout of " + t.key + ": section " + f.name + " at byte "
                        + f.gcOff + ", gc writes it at " + want);
            }
        }
        Flat img = layout.zeroFlat();
        img.refs[img.refs.length - 1] = t;
        building.put(t, img);

        // internal/abi.Type (reflectdata.dcommontype).
        long ty = ks == abiType ? 0 : fo(ks, "Type");
        long size = t.gcSize();
        long ptrdata = ptrBytes(t);
        boolean isNoalg = noalg.contains(t);
        int a = isNoalg ? ALG_NOEQ : alg(t);
        boolean named = hasSym(t);
        String str = gcString(t, false);
        int tflag = (extra ? TFLAG_UNCOMMON : 0) | (named ? TFLAG_NAMED : 0)
                | (a == ALG_MEM ? TFLAG_REGULAR_MEMORY : 0) | (size == 8 && ptrdata == 8 ? TFLAG_DIRECT_IFACE : 0);
        boolean exported;
        if (!str.startsWith("*")) {
            str = "*" + str;
            tflag |= TFLAG_EXTRA_STAR;
            exported = named && isExported(symName(t));
        } else {
            exported = t.elem != null && hasSym(t.elem) && isExported(symName(t.elem));
        }
        long words = ptrdata / 8;
        Flat gcdata;
        if (words <= MAX_PTRMASK_BYTES * 8) {
            byte[] mask = new byte[(int) (((words + 7) / 8 + 7) & ~7L)];
            fillMask(t, 0, mask);
            gcdata = new Flat(table.basic(GoType.UINT8).backing(), Flat.NO_PRIMS, Flat.NO_REFS, mask);
        } else {
            // The mask is built on demand: GCData points at the word that will hold its address.
            tflag |= TFLAG_GC_MASK_ON_DEMAND;
            gcdata = new Flat(table.basic(GoType.UINTPTR).backing(), new long[1], Flat.NO_REFS, Flat.NO_BYTES);
        }
        putInt(img, ty + fo(abiType, "Size_"), MUTATE.equals("size") && size > 0 ? size + 8 : size);
        putInt(img, ty + fo(abiType, "PtrBytes"), ptrdata);
        putInt(img, ty + fo(abiType, "Hash"), typeHash(gcString(t, true)));
        putInt(img, ty + fo(abiType, "TFlag"), tflag);
        putInt(img, ty + fo(abiType, "Align_"), t.gcAlign());
        putInt(img, ty + fo(abiType, "FieldAlign_"), t.gcAlign());
        putInt(img, ty + fo(abiType, "Kind_"), MUTATE.equals("kind") && kind == K_STRUCT ? K_ARRAY : kind);
        if (a != ALG_NOEQ) {
            putRef(img, ty + fo(abiType, "Equal"), host.equalFunc(t));
        }
        putRef(img, ty + fo(abiType, "GCData"), gcdata);
        putInt(img, ty + fo(abiType, "Str"),
                offId(name(MUTATE.equals("nameoff") && kind == K_POINTER ? "*?" : str, "", exported, false, null)));
        if (t.kind != GoType.POINTER && (named || !table.pointer(t).methodSet().isEmpty())) {
            putInt(img, ty + fo(abiType, "PtrToThis"), offId(table.pointer(t)));
        }

        // The kind-specific fields and the data section (reflectdata.writeType).
        switch (kind) {
            case K_POINTER, K_SLICE -> putRef(img, fo(ks, "Elem"), get(u.elem));
            case K_ARRAY -> {
                putRef(img, fo(ks, "Elem"), get(u.elem));
                putRef(img, fo(ks, "Slice"), get(table.slice(u.elem)));
                putInt(img, fo(ks, "Len"), u.len);
            }
            case K_CHAN -> {
                putRef(img, fo(ks, "Elem"), get(u.elem));
                // GIR ChanDir (1 both, 2 send, 3 recv) to internal/abi's (recv 1, send 2, both 3).
                putInt(img, fo(ks, "Dir"), u.dir == 1 ? 3 : u.dir == 2 ? 2 : 1);
            }
            case K_MAP -> {
                GoType group = mapGroup(u);
                GoType slot = group.fields[1].type.elem;
                long keysOff = group.gcSize() == 0 ? 0 : group.fields[1].gcOff;
                long elemOff = slot.gcSize() == 0 ? 0 : slot.fields[1].gcOff;
                putRef(img, fo(ks, "Key"), get(u.mapKey));
                putRef(img, fo(ks, "Elem"), get(u.elem));
                putRef(img, fo(ks, "Group"), get(group));
                putRef(img, fo(ks, "Hasher"), host.hashFunc(u.mapKey));
                putInt(img, fo(ks, "GroupSize"), group.gcSize());
                putInt(img, fo(ks, "KeysOff"), keysOff);
                putInt(img, fo(ks, "KeyStride"), slot.gcSize());
                putInt(img, fo(ks, "ElemsOff"), keysOff + elemOff);
                putInt(img, fo(ks, "ElemStride"), slot.gcSize());
                if (has(ks, "ElemOff")) {
                    putInt(img, fo(ks, "ElemOff"), elemOff);
                }
                putInt(img, fo(ks, "Flags"), (needKeyUpdate(u.mapKey) ? MAP_NEED_KEY_UPDATE : 0)
                        | (hashMightPanic(u.mapKey) ? MAP_HASH_MIGHT_PANIC : 0)
                        | (u.mapKey.gcSize() > MAP_MAX_INLINE ? MAP_INDIRECT_KEY : 0)
                        | (u.elem.gcSize() > MAP_MAX_INLINE ? MAP_INDIRECT_ELEM : 0));
            }
            case K_FUNC -> {
                putInt(img, fo(ks, "InCount"), u.params.length);
                putInt(img, fo(ks, "OutCount"), u.results.length | (u.variadic ? 1 << 15 : 0));
                long p = c;
                for (GoType x : u.params) {
                    putRef(img, p, get(x));
                    p += 8;
                }
                for (GoType x : u.results) {
                    putRef(img, p, get(x));
                    p += 8;
                }
            }
            case K_INTERFACE -> {
                String tpkg = t.kind == GoType.NAMED && !t.pkg.isEmpty() ? t.pkg : null;
                if (tpkg != null) {
                    putRef(img, fo(ks, "PkgPath") + fo(abi("Name"), "Bytes"), name(tpkg, "", false, false, null));
                }
                putSlice(img, fo(ks, "Methods"), c, nData);
                for (int i = 0; i < nData; i++) {
                    IMethod m = ims.get(i);
                    String mpkg = imethodPkg(m);
                    boolean exp = isExported(m.name);
                    long at = c + i * dataElem.gcSize();
                    putInt(img, at + fo(dataElem, "Name"),
                            offId(name(m.name, "", exp, false, !exp && !mpkg.equals(tpkg) ? mpkg : null)));
                    putInt(img, at + fo(dataElem, "Typ"), offId(m.sig));
                }
            }
            case K_STRUCT -> {
                String spkg = null;
                for (Field f : u.fields) {
                    if (!isExported(f.name)) {
                        spkg = f.pkg.isEmpty() ? null : f.pkg;
                        break;
                    }
                }
                long nameBytes = fo(abi("Name"), "Bytes");
                if (spkg != null) {
                    putRef(img, fo(ks, "PkgPath") + nameBytes, name(spkg, "", false, false, null));
                }
                putSlice(img, fo(ks, "Fields"), c, nData);
                for (int i = 0; i < nData; i++) {
                    Field f = u.fields[i];
                    long at = c + i * dataElem.gcSize();
                    putRef(img, at + fo(dataElem, "Name") + nameBytes,
                            name(f.name, f.tag, isExported(f.name), f.embedded, null));
                    putRef(img, at + fo(dataElem, "Typ"), get(f.type));
                    putInt(img, at + fo(dataElem, "Offset"), MUTATE.equals("offset") && i == 1 ? f.gcOff + 8 : f.gcOff);
                }
            }
            default -> { }
        }

        // internal/abi.UncommonType and the method table (reflectdata.dextratype).
        if (extra) {
            String tpkg = typePkg(t);
            if (tpkg != null) {
                putInt(img, b + fo(unc, "PkgPath"), offId(name(tpkg, "", false, false, null)));
            }
            int xcount = 0;
            for (M m : ms) {
                xcount += isExported(m.name) ? 1 : 0;
            }
            putInt(img, b + fo(unc, "Mcount"), ms.size());
            putInt(img, b + fo(unc, "Xcount"), xcount);
            putInt(img, b + fo(unc, "Moff"), unc.gcSize() + dataAdd);
            for (int i = 0; i < ms.size(); i++) {
                M m = ms.get(i);
                boolean exp = isExported(m.name);
                long at = d + i * method.gcSize();
                putInt(img, at + fo(method, "Name"),
                        offId(name(m.name, "", exp, false, !exp && !m.pkg.equals(tpkg) ? m.pkg : null)));
                putInt(img, at + fo(method, "Mtyp"), offId(m.sig));
                // No code offsets yet (W9b): gc's value for a method the linker dropped.
                putInt(img, at + fo(method, "Ifn"), -1);
                putInt(img, at + fo(method, "Tfn"), -1);
            }
        }
        return img;
    }
}
