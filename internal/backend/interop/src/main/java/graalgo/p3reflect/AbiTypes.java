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
        fillAbiStruct(shell, desc, info);
        return ptr;
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
            ReflectTypes.TypeInfo info) {
        String structName = abiStructName(info.kind);
        if (!"internal/abi.Type".equals(structName)) {
            // Kind-specific structs embed Type: fill the nested header, then
            // the extension fields on the shell itself.
            GoStruct nested = GoStruct.zero(ReflectTypes.structDescriptorFor("internal/abi.Type"));
            fillTypeFields(nested, info);
            setObjectByName(desc, shell, "Type", nested);
            fillExtension(shell, desc, info);
        } else {
            fillTypeFields(shell, info);
        }
    }

    /** Fills the {@code internal/abi.Type} header fields (no arithmetic needed). */
    private static void fillTypeFields(GoStruct s, ReflectTypes.TypeInfo info) {
        GoStructDescriptor desc = s.descriptor();
        setLongByName(desc, s, "Size_", info.size);
        setLongByName(desc, s, "PtrBytes", 0);
        setLongByName(desc, s, "Hash", stableHash(info));
        setLongByName(desc, s, "TFlag", 0);
        setLongByName(desc, s, "Align_", info.align);
        setLongByName(desc, s, "FieldAlign_", info.align);
        // kindOrdinal (not a GoKind switch): the wire basic name
        // distinguishes int/int64, uint/uint64/uintptr, byte/uint8,
        // rune/int32, which GoKind conflates.
        setLongByName(desc, s, "Kind_", ReflectTypes.kindOrdinal(info));
        setObjectByName(desc, s, "Equal", comparable(info, new HashSet<>()) ? equalFunc() : null);
        setObjectByName(desc, s, "GCData", null);
        setLongByName(desc, s, "Str", 0);
        setLongByName(desc, s, "PtrToThis", 0);
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
                setObjectByName(desc, shell, "PkgPath", zeroName());
                Object[] elems = new Object[info.fields.size()];
                GoStructDescriptor fieldDesc =
                        ReflectTypes.structDescriptorFor("internal/abi.StructField");
                for (int i = 0; i < info.fields.size(); i++) {
                    ReflectTypes.FieldInfo f = info.fields.get(i);
                    GoStruct sf = GoStruct.zero(fieldDesc);
                    setObjectByName(fieldDesc, sf, "Name", zeroName());
                    setObjectByName(fieldDesc, sf, "Typ", abiPointerForTok(f.type.tok));
                    setLongByName(fieldDesc, sf, "Offset", f.offset);
                    elems[i] = sf;
                }
                setObjectByName(desc, shell, "Fields",
                        new GoSlice(elems, 0, elems.length, elems.length));
            }
            case INTERFACE -> {
                setObjectByName(desc, shell, "PkgPath", zeroName());
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
