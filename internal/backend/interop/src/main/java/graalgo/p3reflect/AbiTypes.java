package graalgo.p3reflect;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;
import graalgo.p3unsafe.UnsafeTable;
import graalgo.values.GoCell;
import graalgo.values.GoEquality;
import graalgo.values.GoFunc;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

import java.util.HashMap;
import java.util.HashSet;
import java.util.IdentityHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Canonical {@code internal/abi} type descriptors for the real-reflect era
 * (r37).
 *
 * <p>Since 2026-10-16 the real {@code reflect}/{@code reflectlite}/
 * {@code internal/abi} sources compile as ordinary code, so their runtime
 * behavior depends on real {@code abi.Type} structs — which no longer come
 * from the synthetic-reflect token world. This class synthesizes those
 * structs from the frontend's {@link ReflectTypes.TypeInfo} (the single
 * source of type truth at runtime), one canonical {@code GoStruct} per
 * type token, memoized so pointer identity matches gc's type identity
 * ({@code TypeOf(x) == TypeOf(x)}).
 *
 * <p>What is synthesized (V1, the surface the snapshot and conformance
 * suites need first):
 * <ul>
 *   <li>{@code Type} header fields read without pointer arithmetic:
 *       {@code Size_}, {@code Align_}, {@code FieldAlign_}, {@code Kind_},
 *       and a real {@code Equal} func value (non-null exactly for
 *       comparable types, so {@code Comparable()} is correct).</li>
 *   <li>Kind-specific extensions: {@code PtrType.Elem},
 *       {@code ArrayType.Elem}/{@code Len}, {@code ChanType.Elem}/
 *       {@code Dir}, {@code SliceType.Elem}, {@code MapType.Key}/
 *       {@code Elem}, {@code StructType.Fields} (with {@code Typ} and
 *       {@code Offset} correct), {@code FuncType.InCount}/{@code OutCount}
 *       (including the variadic bit).</li>
 *   <li>Interface-to-struct views ({@link #buildInterfaceView}): the
 *       {@code *(*EmptyInterface)(unsafe.Pointer(&a))} reinterpretation
 *       that {@code abi.TypeOf} performs. The unsafe-pointer round trip
 *       through a cell yields the {@code GoInterface} itself, not a
 *       {@code {Type, Data}} struct; the view materializes that struct
 *       from the interface's words so the {@code eface.Type} selector
 *       finds a real struct.</li>
 * </ul>
 *
 * <p>Deliberate V1 limits (not oversights — each needs the byte-addressed
 * value model or a linknamed runtime helper that D5 currently traps, and
 * is recorded here so it is not mistaken for done):
 * <ul>
 *   <li>{@code Str}/{@code PtrToThis} are 0 and {@code TFlag} is 0 (no
 *       uncommon data): {@code Name()}/{@code String()}/{@code PkgPath()}
 *       need {@code resolveNameOff}/{@code resolveTypeOff} plus
 *       {@code Name.Data} byte-offset arithmetic, and {@code PtrTo()}
 *       needs {@code resolveTypeOff}. They fail at those intrinsics/
 *       arithmetic, not silently.</li>
 *   <li>{@code InterfaceType.Methods} is empty: method-set synthesis
 *       needs {@code Imethod} name/type pairs, and the table carries only
 *       implkeys for interface methods. {@code Implements} for named
 *       interfaces is therefore incomplete (it matters for
 *       {@code errors.As}, which is outside the snapshot corpus).</li>
 *   <li>{@code FuncType} In/Out trailing arrays, {@code StructField}
 *       names/tags, and {@code Name} strings need byte-offset reads
 *       ({@code Name.DataChecked}) that trap under D5.</li>
 *   <li>{@code Hash}/{@code PtrBytes}/{@code GCData} are placeholders
 *       (stable hash from the type name; 0/null): the reflect surface
 *       never reads them, and they are runtime/GC internals.</li>
 *   <li>The interface view is a <em>read snapshot</em>: it reflects the
 *       interface's words at conversion time, and writes through the
 *       view pointer do not propagate back to the source interface
 *       (a write-through view needs a derived-pointer kind that does
 *       not exist yet).</li>
 * </ul>
 */
public final class AbiTypes {
    private AbiTypes() {}

    /** Role of one view-target field in the interface-word layout. */
    public enum ViewRole {
        /** Receives the synthesized {@code *abi.Type} for the dynamic type. */
        ABI_TYPE,
        /** Receives the interface's itab (may be null when not yet built). */
        ITAB,
        /** Receives the data word as an unsafe-pointer handle. */
        DATA
    }

    /** One field of an interface-view target struct, captured at build time. */
    public record ViewField(String name, long offset, ViewRole role) {}

    /** Canonical abi struct pointer per type token (shell-first, see below). */
    private static final Map<Long, GoPointer> ABI_PTRS = new HashMap<>();

    /**
     * Reverse of {@link #ABI_PTRS}: canonical abi shell → its type
     * token (fix-redmaster-r40). Lets the interface-view inverse
     * ({@link #unpackInterfaceView}) recover the dynamic type of an
     * {@code EmptyInterface} struct value from its Type word. Shells
     * not minted by {@link #abiPointerForTok} (dynamic types built by
     * reflect's own constructors) are absent and do not unpack.
     */
    private static final Map<GoStruct, Long> SHELL_TOKS = new IdentityHashMap<>();

    // ------------------------------------------------------------------
    // Name/type offset registries (fix-redmaster-r41).
    //
    // gc resolves reflect's nameOff/typeOff/textOff against moduledata
    // ranges, falling back to the runtime reflectOffs map for pointers
    // registered at run time (IDs allocated from -1 downward,
    // deduplicated by pointer). graalgo has no moduledata; the same two
    // namespaces are kept here:
    //
    //   - positive IDs minted by this class: NAME_OFFS maps an ID to a
    //     type's encoded name (the ID is stored in abi.Type.Str), and
    //     TYPE_OFFS maps an ID to a pointer type's shell pointer (the
    //     ID is stored in abi.Type.PtrToThis);
    //   - negative IDs (gc's scheme, from -1 downward) for pointers
    //     reflect itself registers through addReflectOff:
    //     REFLECT_OFFS maps an ID to the registered D5 pointer value.
    //
    // All three are per-run state, cleared with the shell memo when
    // the installed table changes (see abiPointerForTok).
    // ------------------------------------------------------------------

    /** Compile-time name encodings by Str ID (positive, from 1). */
    private static final Map<Integer, byte[]> NAME_OFFS = new HashMap<>();

    /** Pointer-type shell pointers by PtrToThis ID (positive, from 1). */
    private static final Map<Integer, Object> TYPE_OFFS = new HashMap<>();

    /** Run-time registered pointers by addReflectOff ID (negative, from -1). */
    private static final Map<Integer, Object> REFLECT_OFFS = new HashMap<>();

    /** Reverse of {@link #REFLECT_OFFS} for gc's pointer dedup. */
    private static final Map<Object, Integer> REFLECT_OFFS_INV = new HashMap<>();

    private static int nextNameOff = 1;
    private static int nextTypeOff = 1;
    private static int nextReflectOff = -1;

    /**
     * The {@link ReflectTypes.Table} the memo above was built against
     * (r38). Tables are per-run installs: tokens are document-scoped, so
     * a token from a later run may denote a different type than the same
     * token in an earlier run. When the installed table changes, the memo
     * is dropped rather than serving a previous run's shells — the same
     * no-leak rule the launchers apply to every other per-run table.
     * The {@code Equal} func value is stateless (it resolves handles
     * through the current table at call time) and survives across runs.
     */
    private static ReflectTypes.Table cachedTable;

    /** Singleton {@code Equal} func value (real equality, see {@link #equalFunc}). */
    private static GoFunc equalFunc;

    /**
     * The canonical {@code *abi.Type} pointer for a type token. Shell-first
     * memoization: the shell is memoized before its fields are filled, so
     * cyclic type graphs (a struct naming a pointer to itself, via
     * {@code PtrType.Elem} back to the struct) terminate with the shell
     * rather than recursing forever.
     */
    public static synchronized GoPointer abiPointerForTok(long tok) {
        ReflectTypes.Table table = ReflectTypes.table();
        if (table != cachedTable) {
            ABI_PTRS.clear();
            SHELL_TOKS.clear();
            NAME_OFFS.clear();
            TYPE_OFFS.clear();
            REFLECT_OFFS.clear();
            REFLECT_OFFS_INV.clear();
            nextNameOff = 1;
            nextTypeOff = 1;
            nextReflectOff = -1;
            cachedTable = table;
        }
        GoPointer existing = ABI_PTRS.get(tok);
        if (existing != null) {
            return existing;
        }
        ReflectTypes.TypeInfo info = ReflectTypes.table().info(tok);
        GoStructDescriptor desc = ReflectTypes.structDescriptorFor(abiStructName(info.kind));
        GoStruct shell = GoStruct.zero(desc);
        GoPointer ptr = new GoPointer(shell);
        ABI_PTRS.put(tok, ptr);
        SHELL_TOKS.put(shell, tok);
        fillAbiStruct(shell, desc, info, tok);
        return ptr;
    }

    /** The type token of a canonical abi shell; 0 when not minted here. */
    public static long tokForShell(GoStruct shell) {
        Long tok = SHELL_TOKS.get(shell);
        return tok == null ? 0 : tok;
    }

    /**
     * gc's {@code reflect_addReflectOff}: registers a pointer value and
     * returns its offset ID (negative, from -1 downward; the same
     * pointer value deduplicates to the same ID).
     */
    public static synchronized int addReflectOff(Object ptr) {
        Integer existing = REFLECT_OFFS_INV.get(ptr);
        if (existing != null) {
            return existing;
        }
        int id = nextReflectOff--;
        REFLECT_OFFS.put(id, ptr);
        REFLECT_OFFS_INV.put(ptr, id);
        return id;
    }

    /**
     * The encoded name bytes for a {@code resolveNameOff} offset: a
     * positive ID reads the compile-time registry, a negative ID the
     * value reflect registered (which must be the name-bytes pointer it
     * was given). Null when the offset is unknown.
     */
    public static synchronized Object nameOffPointer(int off) {
        byte[] enc = NAME_OFFS.get(off);
        if (enc != null) {
            return new GoPointer(enc, 0);
        }
        return REFLECT_OFFS.get(off);
    }

    /**
     * The pointer value for a {@code resolveTypeOff}/{@code
     * resolveTextOff} offset: a positive ID reads the compile-time
     * PtrToThis registry, a negative ID the run-time registry. Null
     * when the offset is unknown.
     */
    public static synchronized Object typeOffPointer(int off) {
        Object v = TYPE_OFFS.get(off);
        if (v != null) {
            return v;
        }
        return REFLECT_OFFS.get(off);
    }

    /**
     * gc's PtrBytes for a type: how many leading bytes of a value
     * contain pointers. Pointer-shaped kinds are one word; a string or
     * slice contributes its data word; an interface both words; an
     * aggregate extends to the end of its last pointer-containing
     * element/field. The {@code visiting} set cuts named-type cycles
     * (a struct naming itself through a pointer — the pointer kind
     * itself terminates the recursion, so this is belt-and-braces).
     */
    private static long ptrBytes(ReflectTypes.TypeInfo info, Set<Long> visiting) {
        switch (info.kind) {
            case POINTER, MAP, CHAN, FUNC, UNSAFE_POINTER, STRING, SLICE -> {
                return 8;
            }
            case INTERFACE -> {
                return 16;
            }
            case ARRAY -> {
                if (info.elem == null || !visiting.add(info.tok)) {
                    return 0;
                }
                long elemPtr = ptrBytes(info.elem, visiting);
                visiting.remove(info.tok);
                if (elemPtr == 0 || info.arrayLen == 0) {
                    return 0;
                }
                return (info.arrayLen - 1L) * info.elem.size + elemPtr;
            }
            case STRUCT -> {
                if (info.fields == null || !visiting.add(info.tok)) {
                    return 0;
                }
                long max = 0;
                for (ReflectTypes.FieldInfo f : info.fields) {
                    long fp = ptrBytes(f.type, visiting);
                    if (fp > 0) {
                        max = Math.max(max, f.offset + fp);
                    }
                }
                visiting.remove(info.tok);
                return max;
            }
            default -> {
                return 0;
            }
        }
    }

    /** Registers a type's name encoding; returns its Str (nameOff) ID. */
    private static synchronized int registerTypeName(String typeString, boolean exported) {
        byte[] enc = encodeName(typeString, "", exported, false);
        int id = nextNameOff++;
        NAME_OFFS.put(id, enc);
        return id;
    }

    /** Registers a pointer type's shell pointer; returns its PtrToThis ID. */
    private static synchronized int registerPtrToThis(Object shellPtr) {
        int id = nextTypeOff++;
        TYPE_OFFS.put(id, shellPtr);
        return id;
    }

    /** Descriptor name for a kind's abi struct (basic kinds use plain {@code Type}). */
    private static String abiStructName(GoKind kind) {
        return switch (kind) {
            case POINTER -> "internal/abi.PtrType";
            case ARRAY -> "internal/abi.ArrayType";
            case CHAN -> "internal/abi.ChanType";
            case MAP -> "internal/abi.MapType";
            case SLICE -> "internal/abi.SliceType";
            case STRUCT -> "internal/abi.StructType";
            case INTERFACE -> "internal/abi.InterfaceType";
            case FUNC -> "internal/abi.FuncType";
            default -> "internal/abi.Type";
        };
    }

    /** Fills a memoized shell: the {@code Type} header plus kind-specific fields. */
    private static void fillAbiStruct(GoStruct shell, GoStructDescriptor desc,
            ReflectTypes.TypeInfo info, long tok) {
        String structName = abiStructName(info.kind);
        if (!"internal/abi.Type".equals(structName)) {
            // Kind-specific structs embed Type: fill the nested header, then
            // the extension fields on the shell itself.
            GoStruct nested = GoStruct.zero(ReflectTypes.structDescriptorFor("internal/abi.Type"));
            fillTypeFields(nested, info, tok);
            setObjectByName(desc, shell, "Type", nested);
            fillExtension(shell, desc, info);
        } else {
            fillTypeFields(shell, info, tok);
        }
    }

    /** Fills the {@code internal/abi.Type} header fields (no arithmetic needed). */
    private static void fillTypeFields(GoStruct s, ReflectTypes.TypeInfo info, long tok) {
        GoStructDescriptor desc = s.descriptor();
        setLongByName(desc, s, "Size_", info.size);
        // PtrBytes: the size of the value's pointer-containing prefix
        // (internal/abi). (*rtype).Pointers() is PtrBytes != 0, and
        // Value.pointer() panics when it is false — with 0 here, every
        // pointer-shaped value (chan, map, func, string, slice,
        // interface) died in Len/Cap/SetMapIndex-style paths with
        // "can't call pointer on a non-pointer Value"
        // (fix-redmaster-r41).
        setLongByName(desc, s, "PtrBytes", ptrBytes(info, new HashSet<>()));
        setLongByName(desc, s, "Hash", stableHash(info));
        // tflagNamed (internal/abi: 1<<2) for named types — the guest's
        // (*rtype).isNamed()/Name() read it. The ExtraStar storage
        // trick is NOT reproduced: the registered name (below) is the
        // exact String() form, so String() returns it verbatim and
        // Name() derives the short name from it in guest code —
        // observably identical to gc (fix-redmaster-r41).
        // tflagDirectIface (1<<5) for the pointer-shaped kinds —
        // (*rtype).IsDirectIface() reads it; reflect.New panics
        // ("may not be allocated in heap") when a pointer type lacks
        // it. This matches gc's data generation exactly:
        // cmd/compile's reflectdata sets TFlagDirectIface and leaves
        // the Kind_ byte at the plain kind ordinal — the Kind_ direct
        // bit (abi.KindDirectIface) is a runtime-internal encoding
        // with no guest-visible reader, and setting it here corrupts
        // graalgo's interface handling (a methods-bearing struct
        // printed via fmt died in an Elem() call on *error), so Kind_
        // stays unmodified (fix-redmaster-r41, bisected).
        boolean directIface = switch (info.kind) {
            case POINTER, CHAN, FUNC, MAP, UNSAFE_POINTER -> true;
            default -> false;
        };
        long tflag = (info.isNamed() ? (1 << 2) : 0) | (directIface ? (1 << 5) : 0);
        setLongByName(desc, s, "TFlag", tflag);
        setLongByName(desc, s, "Align_", info.align);
        setLongByName(desc, s, "FieldAlign_", info.align);
        // kindOrdinal (not a GoKind switch): the wire basic name
        // distinguishes int/int64, uint/uint64/uintptr, byte/uint8,
        // rune/int32, which GoKind conflates.
        setLongByName(desc, s, "Kind_", ReflectTypes.kindOrdinal(info));
        setObjectByName(desc, s, "Equal", comparable(info, new HashSet<>()) ? equalFunc() : null);
        setObjectByName(desc, s, "GCData", null);
        // Str: the nameOff ID of the type's String() form, registered
        // in NAME_OFFS so the guest's nameOff → resolveNameOff path
        // materializes it. Previously 0, which made every guest
        // Type.String()/Name() read an empty name — the visible half
        // of the (?) 0xADDR family (the resolve helpers themselves
        // were unimplemented stubs; see ReflectLinknames).
        boolean exported = false;
        if (info.isNamed()) {
            String shortName = info.name.substring(info.name.lastIndexOf('.') + 1);
            exported = !shortName.isEmpty()
                    && Character.isUpperCase(shortName.charAt(0));
        }
        setLongByName(desc, s, "Str",
                registerTypeName(ReflectTypes.typeString(tok), exported));
        // PtrToThis: the typeOff ID of *T's shell when the table
        // already carries *T (no synthesis — minting types from a
        // field fill would recurse through ptrTo's always-synthesize
        // path). Gives (*rtype).ptrTo() its fast path.
        long ptrTok = ReflectTypes.existingPtrTo(tok);
        if (ptrTok != 0) {
            setLongByName(desc, s, "PtrToThis",
                    registerPtrToThis(abiPointerForTok(ptrTok)));
        } else {
            setLongByName(desc, s, "PtrToThis", 0);
        }
    }

    /** Fills the kind-specific extension fields of a non-{@code Type} abi struct. */
    private static void fillExtension(GoStruct shell, GoStructDescriptor desc,
            ReflectTypes.TypeInfo info) {
        switch (info.kind) {
            case POINTER -> {
                requireNonNull(info.elem, info, "Elem");
                setObjectByName(desc, shell, "Elem", abiPointerForTok(info.elem.tok));
            }
            case ARRAY -> {
                requireNonNull(info.elem, info, "Elem");
                setObjectByName(desc, shell, "Elem", abiPointerForTok(info.elem.tok));
                // Slice (the []Elem type) is not carried by TypeInfo; left nil (V1).
                setLongByName(desc, shell, "Len", info.arrayLen);
            }
            case CHAN -> {
                requireNonNull(info.elem, info, "Elem");
                setObjectByName(desc, shell, "Elem", abiPointerForTok(info.elem.tok));
                if (info.chanDir == null) {
                    throw new IllegalStateException("reflect: chan type without direction: " + info.name);
                }
                int dir = switch (info.chanDir) {
                    case RECV -> 1;
                    case SEND -> 2;
                    case BOTH -> 3;
                };
                setLongByName(desc, shell, "Dir", dir);
            }
            case MAP -> {
                requireNonNull(info.key, info, "Key");
                requireNonNull(info.elem, info, "Elem");
                setObjectByName(desc, shell, "Key", abiPointerForTok(info.key.tok));
                setObjectByName(desc, shell, "Elem", abiPointerForTok(info.elem.tok));
                // Group/Hasher/offsets/flags are runtime map internals, unused
                // by the reflect surface; left zero (V1).
            }
            case SLICE -> {
                requireNonNull(info.elem, info, "Elem");
                setObjectByName(desc, shell, "Elem", abiPointerForTok(info.elem.tok));
            }
            case STRUCT -> {
                if (info.fields == null) {
                    throw new IllegalStateException("reflect: struct type without fields: " + info.name);
                }
                setObjectByName(desc, shell, "PkgPath", nameFor(info.pkgPath, "", true, false));
                Object[] elems = new Object[info.fields.size()];
                GoStructDescriptor fieldDesc =
                        ReflectTypes.structDescriptorFor("internal/abi.StructField");
                for (int i = 0; i < info.fields.size(); i++) {
                    ReflectTypes.FieldInfo f = info.fields.get(i);
                    GoStruct sf = GoStruct.zero(fieldDesc);
                    setObjectByName(fieldDesc, sf, "Name",
                            nameFor(f.name, f.tag, f.isExported(), f.anonymous));
                    setObjectByName(fieldDesc, sf, "Typ", abiPointerForTok(f.type.tok));
                    setLongByName(fieldDesc, sf, "Offset", f.offset);
                    elems[i] = sf;
                }
                setObjectByName(desc, shell, "Fields",
                        new GoSlice(elems, 0, elems.length, elems.length));
            }
            case INTERFACE -> {
                setObjectByName(desc, shell, "PkgPath", nameFor(info.pkgPath, "", true, false));
                // Methods left empty (V1): the table carries only implkeys for
                // interface methods, not the Imethod name/type pairs (see header).
                setObjectByName(desc, shell, "Methods",
                        new GoSlice(new Object[0], 0, 0, 0));
            }
            case FUNC -> {
                if (info.ins == null || info.outs == null) {
                    throw new IllegalStateException("reflect: func type without signatures: " + info.name);
                }
                setLongByName(desc, shell, "InCount", info.ins.size());
                long outCount = info.outs.size();
                if (info.variadic) {
                    outCount |= (1L << 15);
                }
                setLongByName(desc, shell, "OutCount", outCount);
                // In/Out trailing arrays need pointer arithmetic (V1 limit).
            }
            default -> throw new IllegalStateException(
                    "reflect: no abi extension for kind " + info.kind);
        }
    }

    /** A zero {@code internal/abi.Name} (Bytes nil, so Name() reads empty). */
    private static GoStruct zeroName() {
        return GoStruct.zero(ReflectTypes.structDescriptorFor("internal/abi.Name"));
    }

    /**
     * A real {@code internal/abi.Name} for a struct field or package
     * path (fix-redmaster-r40): the {@code Bytes} word points at the
     * gc name encoding ({@code abi.NewName}'s layout — a flags byte,
     * a varint length, the name bytes, and an optional varint-length
     * tag), materialized as an element pointer into a fresh byte
     * array, so the compiled {@code Name} methods ({@code Name()},
     * {@code IsExported()}, {@code Tag()}, {@code Embedded()}) work
     * through the ordinary byte-pointer machinery (including the
     * r39 provenance derivation behind {@code Name.DataChecked}).
     * A null/empty name yields the zero Name. This replaces the V1
     * zero-Name shells for struct fields: {@code reflect.Value.Field}
     * calls {@code field.Name.IsExported()} unconditionally, and the
     * zero Name's nil {@code Bytes} made every Field call on a struct
     * value die with a nil dereference.
     */
    private static GoStruct nameFor(String name, String tag, boolean exported, boolean embedded) {
        GoStructDescriptor nameDesc = ReflectTypes.structDescriptorFor("internal/abi.Name");
        GoStruct s = GoStruct.zero(nameDesc);
        if (name == null || name.isEmpty()) {
            return s;
        }
        byte[] enc = encodeName(name, tag == null ? "" : tag, exported, embedded);
        setObjectByName(nameDesc, s, "Bytes", new GoPointer(enc, 0));
        return s;
    }

    /** The {@code abi.NewName} byte encoding (see {@link #nameFor}). */
    private static byte[] encodeName(String name, String tag, boolean exported, boolean embedded) {
        byte[] nameBytes = name.getBytes(java.nio.charset.StandardCharsets.UTF_8);
        byte[] tagBytes = tag.getBytes(java.nio.charset.StandardCharsets.UTF_8);
        byte[] nameLen = writeVarint(nameBytes.length);
        byte[] tagLen = writeVarint(tagBytes.length);
        int bits = 0;
        int total = 1 + nameLen.length + nameBytes.length;
        if (exported) {
            bits |= 1 << 0;
        }
        if (tagBytes.length > 0) {
            bits |= 1 << 1;
            total += tagLen.length + tagBytes.length;
        }
        if (embedded) {
            bits |= 1 << 3;
        }
        byte[] out = new byte[total];
        int p = 0;
        out[p++] = (byte) bits;
        System.arraycopy(nameLen, 0, out, p, nameLen.length);
        p += nameLen.length;
        System.arraycopy(nameBytes, 0, out, p, nameBytes.length);
        p += nameBytes.length;
        if (tagBytes.length > 0) {
            System.arraycopy(tagLen, 0, out, p, tagLen.length);
            p += tagLen.length;
            System.arraycopy(tagBytes, 0, out, p, tagBytes.length);
        }
        return out;
    }

    /** Unsigned LEB128, as {@code abi.writeVarint} emits for name/tag lengths. */
    private static byte[] writeVarint(int n) {
        byte[] tmp = new byte[5];
        int i = 0;
        for (;;) {
            byte b = (byte) (n & 0x7F);
            n >>>= 7;
            if (n == 0) {
                tmp[i++] = b;
                break;
            }
            tmp[i++] = (byte) (b | 0x80);
        }
        return java.util.Arrays.copyOf(tmp, i);
    }

    /** Stable non-zero hash for a type (the reflect surface never reads it). */
    private static long stableHash(ReflectTypes.TypeInfo info) {
        String seed = info.name.isEmpty() ? info.kind.name() + ":" + info.size : info.name;
        int h = seed.hashCode();
        if (h == 0) {
            h = 1;
        }
        return h & 0xFFFFFFFFL;
    }

    /** Whether a type is comparable (its {@code Equal} is non-null in gc). */
    private static boolean comparable(ReflectTypes.TypeInfo info, Set<Long> visiting) {
        switch (info.kind) {
            case FUNC, SLICE, MAP -> {
                return false;
            }
            case STRUCT -> {
                if (info.fields == null || !visiting.add(info.tok)) {
                    return true;
                }
                try {
                    for (ReflectTypes.FieldInfo f : info.fields) {
                        if (!comparable(f.type, visiting)) {
                            return false;
                        }
                    }
                    return true;
                } finally {
                    visiting.remove(info.tok);
                }
            }
            case ARRAY -> {
                if (info.elem == null || !visiting.add(info.tok)) {
                    return true;
                }
                try {
                    return comparable(info.elem, visiting);
                } finally {
                    visiting.remove(info.tok);
                }
            }
            default -> {
                return true;
            }
        }
    }

    /**
     * The real {@code Equal} func value: resolves both unsafe-pointer
     * handles and compares the values with the runtime's equality (the
     * same semantics as {@code ==}). Only nil-ness is read by
     * {@code Comparable()}; the body is real so a direct call is not a
     * stub.
     */
    private static synchronized GoFunc equalFunc() {
        if (equalFunc != null) {
            return equalFunc;
        }
        RootNode root = new RootNode(null) {
            @Override
            public Object execute(VirtualFrame frame) {
                Object[] args = frame.getArguments();
                // args[0] is the func value; args[1..2] are the two pointers.
                long ha = (Long) args[1];
                long hb = (Long) args[2];
                Object a = UnsafeTable.current().resolve(ha);
                Object b = UnsafeTable.current().resolve(hb);
                return GoEquality.equal(a, b);
            }
        };
        equalFunc = new GoFunc(root.getCallTarget(), null);
        return equalFunc;
    }

    /**
     * Builds the struct view of an interface value for an
     * {@code *(*T)(unsafe.Pointer(&iface))} reinterpretation, or
     * {@code null} when the target layout does not fit the interface-word
     * shape (caller falls back to the pre-view behavior — a pointer to
     * the cell — rather than crashing).
     *
     * <p>Strict rule: every target field must sit at word offset 0 (for
     * {@code ABI_TYPE}/{@code ITAB}) or 8 (for {@code DATA}), and every
     * field must be covered. {@code EmptyInterface} ({@code Type} at 0,
     * {@code Data} at 8) and {@code NonEmptyInterface} ({@code Itab} at
     * 0, {@code Data} at 8) fit; anything else falls back.
     */
    public static GoStruct buildInterfaceView(GoInterface gi, String targetQName,
            List<ViewField> layout) {
        for (ViewField f : layout) {
            switch (f.role()) {
                case ABI_TYPE, ITAB -> {
                    if (f.offset() != 0) {
                        return null;
                    }
                }
                case DATA -> {
                    if (f.offset() != 8) {
                        return null;
                    }
                }
            }
        }
        GoStructDescriptor desc;
        try {
            desc = ReflectTypes.structDescriptorFor(targetQName);
        } catch (RuntimeException e) {
            return null;
        }
        GoStruct view = GoStruct.zero(desc);
        long dynamicTok = ReflectTypes.tokForDescriptor(gi.descriptor());
        GoPointer abiPtr = abiPointerForTok(dynamicTok);
        long dataHandle = dataHandleFor(gi.data());
        for (ViewField f : layout) {
            int ordinal = fieldOrdinal(desc, f.name());
            if (ordinal < 0) {
                return null;
            }
            GoStructDescriptor.Field field = desc.field(ordinal);
            switch (f.role()) {
                case ABI_TYPE -> {
                    if (field.inLongs()) {
                        return null;
                    }
                    view.setObject(ordinal, abiPtr);
                }
                case ITAB -> {
                    if (field.inLongs()) {
                        return null;
                    }
                    view.setObject(ordinal, gi.itab());
                }
                case DATA -> {
                    // The data word is a Long handle, but its carrier is
                    // descriptor-determined: unsafe.Pointer fields are
                    // objects-carried in this model (UNSAFE_POINTER is not
                    // an int/float/bool kind), so box the handle when the
                    // field is objects-carried instead of rejecting it.
                    if (field.inLongs()) {
                        view.setLong(ordinal, dataHandle);
                    } else {
                        view.setObject(ordinal, Long.valueOf(dataHandle));
                    }
                }
            }
        }
        return view;
    }

    /**
     * The inverse of {@link #buildInterfaceView} (fix-redmaster-r40):
     * unpacks an interface-word struct value (an
     * {@code abi.EmptyInterface} / {@code abi.NonEmptyInterface} built
     * by guest code — reflect's {@code packEface} is the canonical
     * producer) into the model's {@link GoInterface}, or {@code null}
     * when the struct does not fit the layout or its Type word is not
     * a canonical abi shell minted by {@link #abiPointerForTok} (the
     * caller then falls back to the pre-unpack behavior).
     *
     * <p>The Type word of an empty-interface struct is a pointer to
     * the canonical shell for the dynamic type, recovered through
     * {@link #SHELL_TOKS}; for a non-empty-interface struct the Itab
     * word's own {@code Type} field names the dynamic type the same
     * way. The Data word is an unsafe-pointer handle (the exact
     * inverse of {@link #dataHandleFor}), resolved through
     * {@link ReflectRefs#denotedValue} with the dynamic type's kind —
     * the same denoted-value read the reflect intrinsics use. A zero
     * Type word with a zero Data word is the nil interface.
     */
    public static GoInterface unpackInterfaceView(GoStruct s, List<ViewField> layout) {
        Object typeWord = null;
        Object itabWord = null;
        long dataHandle = 0;
        boolean sawData = false;
        GoStructDescriptor desc = s.descriptor();
        for (ViewField f : layout) {
            int ordinal = fieldOrdinal(desc, f.name());
            if (ordinal < 0) {
                return null;
            }
            GoStructDescriptor.Field field = desc.field(ordinal);
            switch (f.role()) {
                case ABI_TYPE -> {
                    if (f.offset() != 0 || field.inLongs()) {
                        return null;
                    }
                    typeWord = s.getObject(ordinal);
                }
                case ITAB -> {
                    if (f.offset() != 0 || field.inLongs()) {
                        return null;
                    }
                    itabWord = s.getObject(ordinal);
                }
                case DATA -> {
                    if (f.offset() != 8) {
                        return null;
                    }
                    sawData = true;
                    if (field.inLongs()) {
                        dataHandle = s.getLong(ordinal);
                    } else {
                        Object w = s.getObject(ordinal);
                        if (w == null) {
                            dataHandle = 0;
                        } else if (w instanceof Long l) {
                            dataHandle = l;
                        } else {
                            return null;
                        }
                    }
                }
            }
        }
        if (!sawData) {
            return null;
        }
        if (typeWord == null && itabWord == null) {
            return dataHandle == 0 ? GoInterface.NIL : null;
        }
        // Recover the dynamic type token from the type word (for an
        // itab word, through the itab's own Type field).
        Object typePtr = typeWord;
        if (typePtr == null) {
            if (!(itabWord instanceof GoPointer ip) || ip.isNil()
                    || !(ip.referent() instanceof GoStruct itab)) {
                return null;
            }
            int ord = fieldOrdinal(itab.descriptor(), "Type");
            if (ord < 0 || itab.descriptor().field(ord).inLongs()) {
                return null;
            }
            typePtr = itab.getObject(ord);
        }
        if (!(typePtr instanceof GoPointer tp) || tp.isNil()
                || !(tp.referent() instanceof GoStruct shell)) {
            return null;
        }
        Long tok = SHELL_TOKS.get(shell);
        if (tok == null) {
            return null;
        }
        ReflectTypes.TypeInfo info;
        Object descriptor;
        try {
            info = ReflectTypes.table().info(tok);
            descriptor = ReflectTypes.descriptorForTok(tok);
        } catch (RuntimeException e) {
            return null;
        }
        Object data = dataHandle == 0
                ? null
                : ReflectRefs.denotedValue(dataHandle, info.kind);
        return new GoInterface(descriptor, data, itabWord);
    }

    /**
     * The data word as an unsafe-pointer handle. A pointer-valued datum
     * keeps its storage identity (the unsafefromptr rule); a scalar datum
     * points at element 0 of its normalized carrier array (so dereference
     * yields the scalar, not the array); anything else is interned as-is.
     * Null data (typed nil) is handle 0.
     */
    private static long dataHandleFor(Object data) {
        if (data == null) {
            return 0;
        }
        UnsafeTable table = UnsafeTable.current();
        if (data instanceof GoPointer gp) {
            return UnsafeTable.handleForPointer(gp);
        }
        if (data instanceof Integer || data instanceof Long
                || data instanceof Float || data instanceof Double
                || data instanceof Boolean) {
            long carrierHandle = table.intern(data);
            Object carrier = table.resolve(carrierHandle);
            return table.internEqual(new UnsafeTable.ArrayRef(carrier, 0));
        }
        return table.intern(data);
    }

    /** Descriptor field ordinal by name, or -1. */
    private static int fieldOrdinal(GoStructDescriptor desc, String name) {
        for (int i = 0; i < desc.fields().size(); i++) {
            if (desc.field(i).name().equals(name)) {
                return i;
            }
        }
        return -1;
    }

    private static void setLongByName(GoStructDescriptor desc, GoStruct s,
            String name, long value) {
        int ordinal = fieldOrdinal(desc, name);
        if (ordinal < 0) {
            throw new IllegalStateException("reflect: abi struct " + desc.typeName()
                    + " has no field " + name);
        }
        s.setLong(ordinal, value);
    }

    private static void setObjectByName(GoStructDescriptor desc, GoStruct s,
            String name, Object value) {
        int ordinal = fieldOrdinal(desc, name);
        if (ordinal < 0) {
            throw new IllegalStateException("reflect: abi struct " + desc.typeName()
                    + " has no field " + name);
        }
        s.setObject(ordinal, value);
    }

    private static void requireNonNull(Object v, ReflectTypes.TypeInfo info, String what) {
        if (v == null) {
            throw new IllegalStateException("reflect: " + info.kind + " type without "
                    + what + ": " + info.name);
        }
    }
}
