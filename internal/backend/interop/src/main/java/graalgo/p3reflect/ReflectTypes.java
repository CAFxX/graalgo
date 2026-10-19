package graalgo.p3reflect;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;
import graalgo.frontend.FrontendException;
import graalgo.frontend.Json;
import graalgo.frontend.SelectorNode;
import graalgo.frontend.TypeTable;
import graalgo.iface.IfaceItab;
import graalgo.iface.IfaceMethodTable;
import graalgo.iface.IfaceMethodTarget;
import graalgo.runtime.GoCalls;
import graalgo.values.GoFunc;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoTypeDescriptor;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;

/**
 * The runtime GoRType table (P3-M8, wave 2, reflect package).
 *
 * <p>Built once per program run from the wire type table (which already
 * carries field offsets/tags, method tables, and signatures). Types are
 * identified by dense {@code long} tokens, canonical per structural
 * identity: the same Go type always maps to the same token, so
 * {@code TypeOf(c1) == TypeOf(c2)} holds for channels (whose
 * {@link graalgo.values.GoTypeDescriptor}s are per-build-context, not
 * structural) exactly as it does for every other type. No NodeFactory
 * semantic change is involved — canonicalization lives entirely in this
 * table.
 *
 * <p>Method tables come from the wire {@code named}/{@code pointer}
 * entries' {@code "methods"} lists (name, objId, recv). Method signatures
 * (with receiver) resolve via the wire {@code funcdecl} nodes'
 * {@code obj}→{@code sig} link. Tables are sorted by method name (gc
 * order); unexported methods are included (gc's Method/MethodByName
 * filter them at query time).
 *
 * <p>Install via {@link #install(String, Map)} (called from the launcher,
 * staged patch 05); {@link #table()} fails fast when no run is installed.
 * All state is per-run; nothing leaks across runs.
 */
public final class ReflectTypes {
    private ReflectTypes() {}

    /** Channel direction (mirrors the wire {@code "dir"} values). */
    public enum ChanDir {
        RECV, SEND, BOTH
    }

    /** One struct field. */
    public static final class FieldInfo {
        public final String name;
        /** Package path for unexported fields; "" when exported. */
        public final String pkgPath;
        public final TypeInfo type;
        public final String tag;
        public final long offset;
        public final int[] index;
        public final boolean anonymous;

        FieldInfo(String name, String pkgPath, TypeInfo type, String tag,
                long offset, int[] index, boolean anonymous) {
            this.name = name;
            this.pkgPath = pkgPath;
            this.type = type;
            this.tag = tag;
            this.offset = offset;
            this.index = index;
            this.anonymous = anonymous;
        }

        public boolean isExported() {
            return pkgPath.isEmpty();
        }
    }

    /** One method in a type's method set. */
    public static final class MethodInfo {
        public final String name;
        /** Package path for unexported methods; "" when exported. */
        public final String pkgPath;
        /** The method's func type, WITH the receiver as the first parameter. */
        public final TypeInfo funcType;
        public final GoFunc func;
        public final boolean pointerReceiver;
        public final int index;
        /** Wire implkey (name + receiver-stripped signature key). */
        public final String implKey;

        MethodInfo(String name, String pkgPath, TypeInfo funcType, GoFunc func,
                boolean pointerReceiver, int index, String implKey) {
            this.name = name;
            this.pkgPath = pkgPath;
            this.funcType = funcType;
            this.func = func;
            this.pointerReceiver = pointerReceiver;
            this.index = index;
            this.implKey = implKey;
        }

        public boolean isExported() {
            return pkgPath.isEmpty();
        }
    }

    /** One canonical type. */
    public static final class TypeInfo {
        public final long tok;
        public final GoKind kind;
        /** Qualified name for named types ("main.T"); "" for unnamed. */
        public final String name;
        /** Package path for named types; "" for unnamed/builtin. */
        public final String pkgPath;
        public final long size;
        public final int align;
        // Composites (null when not applicable).
        public final TypeInfo elem;
        public final TypeInfo key;
        public final int arrayLen;
        public final ChanDir chanDir;
        public final List<FieldInfo> fields;
        public final List<MethodInfo> methods;
        public final List<TypeInfo> ins;
        public final List<TypeInfo> outs;
        public final boolean variadic;
        /** Interface method implkeys (INTERFACE kind only). */
        public final List<String> interfaceMethods;
        /** Wire basic type name (BASIC kind only; "" otherwise). */
        public final String basicName;
        /** Representative wire type id (for diagnostics). */
        public final int wireId;

        TypeInfo(long tok, GoKind kind, String name, String pkgPath, long size, int align,
                TypeInfo elem, TypeInfo key, int arrayLen, ChanDir chanDir,
                List<FieldInfo> fields, List<MethodInfo> methods,
                List<TypeInfo> ins, List<TypeInfo> outs, boolean variadic,
                List<String> interfaceMethods, String basicName, int wireId) {
            this.tok = tok;
            this.kind = kind;
            this.name = name;
            this.pkgPath = pkgPath;
            this.size = size;
            this.align = align;
            this.elem = elem;
            this.key = key;
            this.arrayLen = arrayLen;
            this.chanDir = chanDir;
            this.fields = fields;
            this.methods = methods;
            this.ins = ins;
            this.outs = outs;
            this.variadic = variadic;
            this.interfaceMethods = interfaceMethods;
            this.basicName = basicName;
            this.wireId = wireId;
        }

        public boolean isNamed() {
            return !name.isEmpty();
        }
    }

    /** The installed per-run table. */
    public static final class Table {
        final List<TypeInfo> byTok = new ArrayList<>();
        final Map<String, Long> keyToTok = new HashMap<>();
        private final Map<GoKind, Long> basicKindToTok = new HashMap<>();
        /** Wire type id → canonical token (for descriptor lookup). */
        private final Map<Integer, Long> wireIdToTok = new HashMap<>();
        /** Token → canonical GoTypeDescriptor (for `any` boxing; memoized). */
        private final Map<Long, graalgo.values.GoTypeDescriptor> tokToDesc = new HashMap<>();
        /**
         * Wire ids currently being built (phase-B recursion stack). Used
         * to cut reference cycles: a reference to an id in this set
         * resolves to its shell instead of recursing forever.
         */
        final Set<Integer> building = new HashSet<>();

        Table() {
            byTok.add(null); // tok 0 = invalid
        }

        /** The TypeInfo for a token (fails fast on unknown). */
        public TypeInfo info(long tok) {
            if (tok <= 0 || tok >= byTok.size()) {
                throw new IllegalArgumentException("reflect: unknown type token " + tok);
            }
            return byTok.get((int) tok);
        }

        /** Token for a basic kind (fails fast when absent). */
        public long tokForKind(GoKind kind) {
            Long t = basicKindToTok.get(kind);
            if (t == null) {
                throw new IllegalStateException("reflect: no basic type for kind " + kind);
            }
            return t;
        }
    }

    private static volatile Table installed;

    /** Installs the per-run table from the parsed wire document.
     * @param doc the parsed frontend/v1 document (parse-once: caller reuses
     *            NodeFactory.document())
     */
    public static void install(Json.Object doc, Map<Integer, GoFunc> functions) {
        TypeTable types = new TypeTable(doc.array("types", "document"));
        installed = build(doc, types, functions);
        cacheTypes(types);
        captureDescriptors(types);
    }

    /** The current run's table; fails fast when no run is installed. */
    public static Table table() {
        Table t = installed;
        if (t == null) {
            throw new IllegalStateException(
                    "reflect: no run installed (launcher must call ReflectTypes.install)");
        }
        return t;
    }

    /** Token for a GoTypeDescriptor (via the patch-03 wireId registry). */
    public static long tokForDescriptor(Object descriptor) {
        Table table = table();
        Integer wireId = graalgo.frontend.NodeFactory.wireIdForDescriptor(
                (graalgo.values.GoTypeDescriptor) descriptor);
        if (wireId == null) {
            throw new IllegalStateException(
                    "reflect: descriptor has no wire id: " + descriptor);
        }
        Long tok = table.wireIdToTok.get(wireId);
        if (tok == null) {
            throw new IllegalStateException(
                    "reflect: wire id " + wireId + " has no token");
        }
        return tok;
    }

    /**
     * Canonical GoTypeDescriptor for a token, for boxing D5 values as
     * {@code any} (the {@code interface}/{@code rawinterface} intrinsics).
     *
     * <p>Named and basic types canonicalize by name process-wide, so they
     * are built directly via the frontend's canonical maps — identical to
     * the instance {@code typeDescriptorFor} would build, and immune to
     * cross-document wire-id collisions in the process-wide
     * {@code WIRE_ID_DESCRIPTORS} registry (wire ids are per-document).
     * Unnamed composites fall back to the wire-id registry, then to
     * on-demand structural construction.
     */
    public static graalgo.values.GoTypeDescriptor descriptorForTok(long tok) {
        Table table = table();
        graalgo.values.GoTypeDescriptor d = table.tokToDesc.get(tok);
        if (d == null) {
            TypeInfo t = table.info(tok);
            if (t.isNamed()) {
                d = graalgo.frontend.NodeFactory.namedDescriptorFor(t.name, t.kind);
            } else if (!t.basicName.isEmpty()) {
                d = graalgo.frontend.NodeFactory.basicDescriptorForName(
                        canonicalBasicName(t.basicName), t.kind);
            } else if (t.kind == graalgo.values.GoKind.POINTER
                    && t.elem != null && t.elem.isNamed()) {
                d = graalgo.frontend.NodeFactory.namedDescriptorFor(
                        "*" + t.elem.name, t.kind);
            } else {
                d = graalgo.frontend.NodeFactory.descriptorForWireId(t.wireId);
                if (d == null) {
                    for (Map.Entry<Integer, Long> e : table.wireIdToTok.entrySet()) {
                        if (e.getValue() == tok) {
                            d = graalgo.frontend.NodeFactory.descriptorForWireId(e.getKey());
                            if (d != null) {
                                break;
                            }
                        }
                    }
                }
                if (d == null) {
                    d = buildStructuralDescriptorOnDemand(t);
                }
            }
            table.tokToDesc.put(tok, d);
        }
        return d;
    }

    /**
     * Builds the canonical GoTypeDescriptor for an unnamed composite
     * TypeInfo the frontend never materialized. Unnamed composites have
     * no methods, so the descriptor name is only used for %T/diagnostics;
     * canonicalization is per structural shape in a reflect-side
     * process-wide map keyed by the Go-syntax type string, giving
     * self-consistent identity within reflect boxing.
     */
    private static final java.util.concurrent.ConcurrentHashMap<String,
            graalgo.values.GoTypeDescriptor> ON_DEMAND_STRUCTURAL_DESCS =
            new java.util.concurrent.ConcurrentHashMap<>();

    private static graalgo.values.GoTypeDescriptor buildStructuralDescriptorOnDemand(TypeInfo t) {
        String key = typeString(t.tok);
        graalgo.values.GoTypeDescriptor d = ON_DEMAND_STRUCTURAL_DESCS.get(key);
        if (d == null) {
            d = new graalgo.values.GoTypeDescriptor(key, t.kind, false);
            graalgo.values.GoTypeDescriptor prev =
                    ON_DEMAND_STRUCTURAL_DESCS.putIfAbsent(key, d);
            if (prev != null) {
                d = prev;
            }
        }
        return d;
    }

    /**
     * Canonical basic-type name for a wire basic name, mirroring the
     * frontend's canonicalization (see NodeFactory.canonicalBasicName).
     */
    private static String canonicalBasicName(String wireName) {
        // The frontend maps aliases/legacy spellings to the canonical Go
        // name; replicate the mapping used by basicDescriptorForName.
        return graalgo.frontend.NodeFactory.canonicalBasicName(wireName);
    }

    // ------------------------------------------------------------------
    // Construction.
    // ------------------------------------------------------------------

    private static Table build(Json.Object doc, TypeTable types, Map<Integer, GoFunc> functions) {
        Map<Integer, Integer> funcdeclSig = collectFuncdeclSigs(doc);
        Table table = new Table();
        int n = types.size();
        // Pass 1: shells (kind, name, size).
        TypeInfo[] infos = new TypeInfo[n];
        for (int id = 0; id < n; id++) {
            infos[id] = shell(types, id);
        }
        // Pass 2: resolve references and canonicalize. Single call:
        // resolve() mints tokens for every id (phase A) and builds every
        // TypeInfo (phase B) itself; the M8 draft's per-id loop re-ran
        // both phases n times (the id parameter was unused), an O(n^2)
        // key recomputation that dominated startup on large type tables
        // (e.g. the synthetic os package: 2297 types, minutes in build()).
        // Both phases are idempotent (containsKey / byTok guards), so one
        // call is semantics-preserving.
        resolve(table, types, infos, functions, funcdeclSig);
        // Pass 3: rebuild every TypeInfo now that all children are built.
        // Pass 2 embeds cycle-cut shells (tok == -1) for types referenced
        // while under construction (e.g. *N's elem while building N);
        // with the full table built, infoOrShell always hits byTok, so one
        // rebuild pass yields shell-free infos. buildInfo is side-effect
        // free, so rebuilding is idempotent for acyclic types.
        for (int id = 0; id < n; id++) {
            buildOne(table, types, infos, id, functions, funcdeclSig, true);
        }
        return table;
    }

    /** Walks the document for funcdecl nodes; maps obj id → sig type id. */
    private static Map<Integer, Integer> collectFuncdeclSigs(Json.Object doc) {
        Map<Integer, Integer> out = new HashMap<>();
        collectFuncdeclSigs(doc, out);
        return out;
    }

    private static void collectFuncdeclSigs(Json.Value v, Map<Integer, Integer> out) {
        if (v instanceof Json.Object o) {
            if (isKind(o, "funcdecl")) {
                Json.Object f = o.object("fields", "funcdecl");
                Json.Value obj = f.opt("obj");
                Json.Value sig = f.opt("sig");
                if (obj instanceof Json.Num on && sig instanceof Json.Num sn) {
                    out.put((int) on.asLong("funcdecl.fields.obj"),
                            (int) sn.asLong("funcdecl.fields.sig"));
                }
            }
            for (Map.Entry<String, Json.Value> e : o.members().entrySet()) {
                collectFuncdeclSigs(e.getValue(), out);
            }
        } else if (v instanceof Json.Array a) {
            for (int i = 0; i < a.size(); i++) {
                collectFuncdeclSigs(a.get(i), out);
            }
        }
    }

    private static boolean isKind(Json.Object o, String kind) {
        Json.Value kv = o.opt("kind");
        return kv instanceof Json.Str s && kind.equals(s.value());
    }

    // ------------------------------------------------------------------
    // Pass 1: shells.
    // ------------------------------------------------------------------

    /** Builds the unresolved shell for one wire type id. */
    private static TypeInfo shell(TypeTable types, int id) {
        String path = "types[" + id + "]";
        Json.Object entry = types.entry(id, path);
        Json.Object d = entry.object("detail", path);
        String kind = entry.string("kind", path);
        return switch (kind) {
            case "basic" -> {
                String name = d.string("name", path);
                GoKind gk = basicKind(name);
                yield new TypeInfo(-1, gk, "", "", basicSize(gk), basicAlign(gk),
                        null, null, 0, null, List.of(), List.of(),
                        List.of(), List.of(), false, List.of(), "", id);
            }
            case "named" -> {
                String qname = d.string("name", path);
                // Staged patch 06 adds "pkgpath"; tolerate its absence.
                Json.Value pp = d.opt("pkgpath");
                String pkgPath = pp instanceof Json.Str s ? s.value() : "";
                yield new TypeInfo(-1, null, qname, pkgPath, -1, -1,
                        null, null, 0, null, List.of(), List.of(),
                        List.of(), List.of(), false, List.of(), "", id);
            }
            case "alias" -> {
                // P3-M8 fix: resolve aliases to their RHS. The alias name
                // is preserved for display, but the kind/size/align come
                // from the target type.
                int rhsId = (int) d.longInt("rhs", path);
                TypeInfo rhs = shell(types, rhsId);
                String aliasName = d.string("name", path);
                yield new TypeInfo(-1, rhs.kind, aliasName, rhs.pkgPath,
                        rhs.size, rhs.align, rhs.elem, rhs.key,
                        rhs.arrayLen, rhs.chanDir, rhs.fields, rhs.methods,
                        rhs.ins, rhs.outs, rhs.variadic,
                        rhs.interfaceMethods, rhs.basicName, id);
            }
            case "array" -> new TypeInfo(-1, GoKind.ARRAY, "", "", -1, -1,
                    null, null, (int) d.longInt("len", path), null,
                    List.of(), List.of(), List.of(), List.of(), false, List.of(), "", id);
            case "slice" -> new TypeInfo(-1, GoKind.SLICE, "", "", -1, -1,
                    null, null, 0, null, List.of(), List.of(),
                    List.of(), List.of(), false, List.of(), "", id);
            case "map" -> new TypeInfo(-1, GoKind.MAP, "", "", -1, -1,
                    null, null, 0, null, List.of(), List.of(),
                    List.of(), List.of(), false, List.of(), "", id);
            case "pointer" -> new TypeInfo(-1, GoKind.POINTER, "", "", -1, -1,
                    null, null, 0, null, List.of(), List.of(),
                    List.of(), List.of(), false, List.of(), "", id);
            case "chan" -> {
                String dir = d.string("dir", path);
                ChanDir cd = switch (dir) {
                    case "recv" -> ChanDir.RECV;
                    case "send" -> ChanDir.SEND;
                    default -> ChanDir.BOTH;
                };
                yield new TypeInfo(-1, GoKind.CHAN, "", "", -1, -1,
                        null, null, 0, cd, List.of(), List.of(),
                        List.of(), List.of(), false, List.of(), "", id);
            }
            case "struct" -> new TypeInfo(-1, GoKind.STRUCT, "", "", -1, -1,
                    null, null, 0, null, List.of(), List.of(),
                    List.of(), List.of(), false, List.of(), "", id);
            case "interface" -> new TypeInfo(-1, GoKind.INTERFACE, "", "", -1, -1,
                    null, null, 0, null, List.of(), List.of(),
                    List.of(), List.of(), false, List.of(), "", id);
            case "signature" -> new TypeInfo(-1, GoKind.FUNC, "", "", -1, -1,
                    null, null, 0, null, List.of(), List.of(),
                    List.of(), List.of(), false, List.of(), "", id);
            case "typeparam" -> {
                // A type parameter has no value representation: at runtime
                // every value of the parameter's type carries its
                // *instantiated* type (the frontend monomorphizes
                // generics), so a typeparam wire entry only ever denotes
                // a template position — e.g. the [0]*T field of the
                // sync/atomic.Pointer[T] template — never an executable
                // type. The shell is an opaque placeholder: null kind
                // (not a GoKind; mirrors the unresolved named shell),
                // unknown size/align. Canonical identity comes from
                // canonicalKey's "typeparam:" key, which can never
                // collide with a concrete type's key.
                // (Adjudicated 2026-10-15: supersedes the constraint-transparent
                // design — a constraint's layout is not the instantiated
                // type's layout, so inheriting it would silently miscompile;
                // fail loud instead per the P1-4 invalid-type rule.)
                yield new TypeInfo(-1, null, "", "", -1, -1,
                        null, null, 0, null, List.of(), List.of(),
                        List.of(), List.of(), false, List.of(), "", id);
            }
            default -> throw new FrontendException(path + ": reflect: unsupported type kind " + kind);
        };
    }

    private static GoKind basicKind(String name) {
        return switch (name) {
            case "bool" -> GoKind.BOOL;
            case "int" -> GoKind.INT64;
            case "int8" -> GoKind.INT8;
            case "int16" -> GoKind.INT16;
            case "int32", "rune" -> GoKind.INT32;
            case "int64" -> GoKind.INT64;
            case "uint" -> GoKind.UINT64;
            case "uint8", "byte" -> GoKind.UINT8;
            case "uint16" -> GoKind.UINT16;
            case "uint32" -> GoKind.UINT32;
            case "uint64" -> GoKind.UINT64;
            case "uintptr" -> GoKind.UINT64;
            case "float32" -> GoKind.FLOAT32;
            case "float64" -> GoKind.FLOAT64;
            case "complex64" -> GoKind.COMPLEX64;
            case "complex128" -> GoKind.COMPLEX128;
            case "string" -> GoKind.STRING;
            // go/types names the unsafe.Pointer basic "Pointer"
            // (universe.go); the exporter sends t.Name() verbatim, so the
            // wire name is "Pointer", never "unsafe.Pointer". Accept both.
            case "Pointer", "unsafe.Pointer" -> GoKind.UNSAFE_POINTER;
            // P3-M8 fix: untyped basics map to their default types.
            case "untyped bool" -> GoKind.BOOL;
            case "untyped int" -> GoKind.INT64;
            case "untyped rune" -> GoKind.INT32;
            case "untyped float" -> GoKind.FLOAT64;
            case "untyped complex" -> GoKind.COMPLEX128;
            case "untyped string" -> GoKind.STRING;
            case "untyped nil" -> GoKind.UNSAFE_POINTER;
            default -> throw new FrontendException("reflect: unknown basic type " + name);
        };
    }

    private static long basicSize(GoKind k) {
        return switch (k) {
            case BOOL, INT8, UINT8 -> 1;
            case INT16, UINT16 -> 2;
            case INT32, UINT32, FLOAT32 -> 4;
            case INT64, UINT64, FLOAT64, COMPLEX64, UNSAFE_POINTER -> 8;
            case STRING -> 16;
            case COMPLEX128 -> 16;
            default -> 8;
        };
    }

    private static int basicAlign(GoKind k) {
        return (int) Math.min(basicSize(k), 8);
    }

    // ------------------------------------------------------------------
    // Pass 2: resolve + canonicalize.
    // ------------------------------------------------------------------

    // ------------------------------------------------------------------
    // Pass 2: resolve + canonicalize (two-phase for recursive types).
    //
    // Phase A: compute every wire id's canonical key (pure string
    // recursion over wire ids — terminates) and mint dense tokens.
    // Phase B: fill each TypeInfo by resolving references to tokens.
    // Phase B builds depth-first (see infoOrShell): when a type references
    // a not-yet-built wire id, that id is built immediately, so forward
    // references resolve to complete TypeInfos. The only baked shells are
    // true reference cycles (struct containing *T, type F func(F)), cut at
    // the back edge exactly as before — recursive types work because a
    // cycle cut only needs the token, never completed field lists.
    // ------------------------------------------------------------------

    private static void resolve(Table table, TypeTable types, TypeInfo[] shells,
            Map<Integer, GoFunc> functions, Map<Integer, Integer> funcdeclSig) {
        // Phase A: mint tokens for every id.
        int n = types.size();
        for (int i = 0; i < n; i++) {
            String path = "types[" + i + "]";
            Json.Object entry = types.entry(i, path);
            Json.Object d = entry.object("detail", path);
            String kind = entry.string("kind", path);
            String key = canonicalKey(table, types, shells, i, kind, d, functions, funcdeclSig, path,
                    new HashSet<>());
            if (!table.keyToTok.containsKey(key)) {
                long tok = table.byTok.size();
                table.keyToTok.put(key, tok);
                table.byTok.add(null); // filled in phase B
            }
        }
        // Phase B: build each TypeInfo (depth-first via infoOrShell).
        for (int i = 0; i < n; i++) {
            buildOne(table, types, shells, i, functions, funcdeclSig, false);
        }
    }

    /**
     * Builds the TypeInfo for one wire id (phase B), unless its canonical
     * token was already built via canonical sharing. Re-entrant: called
     * from infoOrShell for forward references.
     */
    private static void buildOne(Table table, TypeTable types, TypeInfo[] shells,
            int id, Map<Integer, GoFunc> functions, Map<Integer, Integer> funcdeclSig,
            boolean force) {
        String key = keyForId(table, types, shells, id, functions, funcdeclSig);
        Long tok = table.keyToTok.get(key);
        if (tok == null) {
            throw new FrontendException("reflect: no token for type id " + id);
        }
        // Always record the wire-id -> token mapping, even when the token
        // was already built via canonical sharing: tokForDescriptor looks
        // up by the descriptor's recorded wire id, which may be any of the
        // ids sharing this token (last writer wins in the registry).
        table.wireIdToTok.put(id, tok);
        if (!force && table.byTok.get((int) (long) tok) != null) {
            return; // already built via canonical sharing
        }
        if (!table.building.add(id)) {
            return; // already being built higher on the stack (cycle);
                    // the caller falls back to the shell
        }
        try {
            String path = "types[" + id + "]";
            Json.Object entry = types.entry(id, path);
            Json.Object d = entry.object("detail", path);
            String kind = entry.string("kind", path);
            TypeInfo info = buildInfo(table, types, shells, id, kind, d, tok,
                    functions, funcdeclSig, path);
            table.byTok.set((int) (long) tok, info);
            if (info.kind != null && kind.equals("basic")) {
                table.basicKindToTok.putIfAbsent(info.kind, tok);
            }
        } finally {
            table.building.remove(id);
        }
    }

    /** Token for a wire id (phase A must have run). */
    private static long tokForId(Table table, TypeTable types, TypeInfo[] shells,
            int id, Map<Integer, GoFunc> functions, Map<Integer, Integer> funcdeclSig) {
        String key = keyForId(table, types, shells, id, functions, funcdeclSig);
        Long tok = table.keyToTok.get(key);
        if (tok == null) {
            throw new FrontendException("reflect: no token for type id " + id);
        }
        return tok;
    }

    /**
     * P3-M8 (reviewer-2 P1-5): resolves the built TypeInfo for a wire id.
     * If the target hasn't been built yet in phase B (a forward reference),
     * it is built NOW, depth-first — so parents never bake incomplete
     * shells. The shell is returned only for a true reference cycle (the
     * id is already being built higher on the stack); that cycle cut is
     * the pre-existing mechanism for recursive types, and shells never
     * survive into byTok from acyclic references.
     *
     * <p>Shell-safety contract (for the remaining cycle-cut shells, which
     * carry tok == -1): safe to read are kind (non-null except for named
     * shells), basic size/align, and basicName. NOT safe: elem/key (null),
     * fields/methods/ins/outs (empty), named size/align (-1), tok (-1 —
     * Table.info fails fast on it). Consumers must not assume a child
     * TypeInfo is complete unless its tok != -1.
     */
    private static TypeInfo infoOrShell(Table table, TypeTable types, TypeInfo[] shells,
            int id, Map<Integer, GoFunc> functions, Map<Integer, Integer> funcdeclSig) {
        long tok = tokForId(table, types, shells, id, functions, funcdeclSig);
        TypeInfo info = table.byTok.get((int) tok);
        if (info != null) {
            return info;
        }
        if (table.building.contains(id)) {
            // Reference cycle: cut it with the shell (kind only).
            return shells[id];
        }
        buildOne(table, types, shells, id, functions, funcdeclSig, false);
        TypeInfo built = table.byTok.get((int) tok);
        if (built == null) {
            throw new FrontendException(
                    "reflect: build of types[" + id + "] produced no TypeInfo");
        }
        return built;
    }

    private static String keyForId(Table table, TypeTable types, TypeInfo[] shells,
            int id, Map<Integer, GoFunc> functions, Map<Integer, Integer> funcdeclSig) {
        String path = "types[" + id + "]";
        Json.Object entry = types.entry(id, path);
        Json.Object d = entry.object("detail", path);
        String kind = entry.string("kind", path);
        return canonicalKey(table, types, shells, id, kind, d, functions, funcdeclSig, path,
                new HashSet<>());
    }

    /** Canonical structural key for one wire id. */
    private static String canonicalKey(Table table, TypeTable types, TypeInfo[] shells,
            int id, String kind, Json.Object d,
            Map<Integer, GoFunc> functions, Map<Integer, Integer> funcdeclSig, String path,
            Set<Integer> tpActive) {
        return switch (kind) {
            case "basic" -> "basic:" + d.string("name", path);
            case "named" -> {
                Json.Value pp = d.opt("pkgpath");
                String pkgPath = pp instanceof Json.Str s ? s.value() : "";
                // The wire "name" is qualified ("main.T"); split it.
                String qname = d.string("name", path);
                yield "named:" + pkgPath + "." + qname;
            }
            case "array" -> "array:" + d.longInt("len", path) + ":"
                    + keyOf(table, types, shells, (int) d.longInt("elem", path),
                            functions, funcdeclSig, tpActive);
            case "slice" -> "slice:"
                    + keyOf(table, types, shells, (int) d.longInt("elem", path),
                            functions, funcdeclSig, tpActive);
            case "map" -> "map:"
                    + keyOf(table, types, shells, (int) d.longInt("key", path),
                            functions, funcdeclSig, tpActive)
                    + ":" + keyOf(table, types, shells, (int) d.longInt("elem", path),
                            functions, funcdeclSig, tpActive);
            case "pointer" -> "ptr:"
                    + keyOf(table, types, shells, (int) d.longInt("elem", path),
                            functions, funcdeclSig, tpActive);
            case "chan" -> "chan:" + d.string("dir", path) + ":"
                    + keyOf(table, types, shells, (int) d.longInt("elem", path),
                            functions, funcdeclSig, tpActive);
            case "struct" -> structKey(table, types, shells, id, d, functions, funcdeclSig, path, tpActive);
            case "interface" -> interfaceKey(table, types, shells, id, d, functions, funcdeclSig, path, tpActive);
            case "signature" -> signatureKey(table, types, shells, id, d, functions, funcdeclSig, path, tpActive);
            // P3-M8 fix: aliases resolve to their RHS for key purposes.
            case "alias" -> keyOf(table, types, shells, (int) d.longInt("rhs", path),
                    functions, funcdeclSig, tpActive);
            case "typeparam" -> {
                // A type parameter's structural identity is the parameter
                // itself (name) plus its constraint's type set. The
                // "typeparam:" prefix keeps it out of every concrete
                // type's key space, so a placeholder can never share a
                // token with a real type. The constraint key is computed
                // with a re-entrancy guard: an F-bounded constraint (e.g.
                // T constrained by interface{ M() T }) refers back to the
                // parameter, and without the guard the key recursion
                // would not terminate — the same cycle the frontend's
                // describeType survives via its typeparamIDs
                // memo-before-describe.
                // (Adjudicated 2026-10-15: keeps the "typeparam:" scheme;
                // adds the cycle guard the other design lacked.)
                String pname = d.string("name", path);
                if (!tpActive.add(id)) {
                    yield "typeparam<" + pname + ">";
                }
                try {
                    yield "typeparam:" + pname + ":"
                            + keyOf(table, types, shells,
                                    (int) d.longInt("constraint", path),
                                    functions, funcdeclSig, tpActive);
                } finally {
                    tpActive.remove(id);
                }
            }
            default -> throw new FrontendException(path + ": reflect: key of " + kind);
        };
    }

    private static String keyOf(Table table, TypeTable types, TypeInfo[] shells,
            int id, Map<Integer, GoFunc> functions, Map<Integer, Integer> funcdeclSig,
            Set<Integer> tpActive) {
        String path = "types[" + id + "]";
        Json.Object entry = types.entry(id, path);
        Json.Object d = entry.object("detail", path);
        String kind = entry.string("kind", path);
        return canonicalKey(table, types, shells, id, kind, d, functions, funcdeclSig, path, tpActive);
    }

    /** Struct key: ordered field (name, tag, anonymous, type-key) list. */
    private static String structKey(Table table, TypeTable types, TypeInfo[] shells,
            int id, Json.Object d, Map<Integer, GoFunc> functions,
            Map<Integer, Integer> funcdeclSig, String path, Set<Integer> tpActive) {
        Json.Array fields = d.array("fields", path);
        StringBuilder sb = new StringBuilder("struct{");
        for (int i = 0; i < fields.size(); i++) {
            Json.Object f = arrObj(fields, i, path);
            if (i > 0) {
                sb.append(';');
            }
            sb.append(f.string("name", path)).append(',')
                    .append(f.string("tag", path)).append(',')
                    .append(isEmbedded(f)).append(',')
                    .append(keyOf(table, types, shells, (int) f.longInt("type", path),
                            functions, funcdeclSig, tpActive));
        }
        return sb.append('}').toString();
    }

    /** Interface key: ordered method (name, sig-key) list. */
    private static String interfaceKey(Table table, TypeTable types, TypeInfo[] shells,
            int id, Json.Object d, Map<Integer, GoFunc> functions,
            Map<Integer, Integer> funcdeclSig, String path, Set<Integer> tpActive) {
        Json.Array methods = d.array("methods", path);
        StringBuilder sb = new StringBuilder("interface{");
        for (int i = 0; i < methods.size(); i++) {
            Json.Object m = arrObj(methods, i, path);
            if (i > 0) {
                sb.append(';');
            }
            sb.append(m.string("name", path)).append(',')
                    .append(keyOf(table, types, shells, (int) m.longInt("sig", path),
                            functions, funcdeclSig, tpActive));
        }
        return sb.append('}').toString();
    }

    /** Signature key: params/results type keys + variadic. */
    private static String signatureKey(Table table, TypeTable types, TypeInfo[] shells,
            int id, Json.Object d, Map<Integer, GoFunc> functions,
            Map<Integer, Integer> funcdeclSig, String path, Set<Integer> tpActive) {
        StringBuilder sb = new StringBuilder("func(");
        Json.Array params = d.array("params", path);
        for (int i = 0; i < params.size(); i++) {
            if (i > 0) {
                sb.append(',');
            }
            sb.append(keyOf(table, types, shells,
                    (int) arrObj(params, i, path).longInt("type", path),
                    functions, funcdeclSig, tpActive));
        }
        sb.append(")(");
        Json.Array results = d.array("results", path);
        for (int i = 0; i < results.size(); i++) {
            if (i > 0) {
                sb.append(',');
            }
            sb.append(keyOf(table, types, shells,
                    (int) arrObj(results, i, path).longInt("type", path),
                    functions, funcdeclSig, tpActive));
        }
        sb.append(')');
        Json.Value var = d.opt("variadic");
        if (var instanceof Json.Bool b && b.value()) {
            sb.append("...");
        }
        return sb.toString();
    }

    private static boolean isEmbedded(Json.Object f) {
        Json.Value v = f.opt("embedded");
        return v instanceof Json.Bool b && b.value();
    }

    private static Json.Object arrObj(Json.Array a, int i, String path) {
        Json.Value v = a.get(i);
        if (!(v instanceof Json.Object o)) {
            throw new FrontendException(path + ": expected object at index " + i);
        }
        return o;
    }

    /** Builds the full TypeInfo for one wire id (references resolved). */
    private static TypeInfo buildInfo(Table table, TypeTable types, TypeInfo[] shells,
            int id, String kind, Json.Object d, long tok,
            Map<Integer, GoFunc> functions, Map<Integer, Integer> funcdeclSig, String path) {
        return switch (kind) {
            case "basic" -> {
                TypeInfo sh = shells[id];
                String bname = d.string("name", path);
                yield new TypeInfo(tok, sh.kind, "", "", sh.size, sh.align,
                        null, null, 0, null, List.of(), List.of(),
                        List.of(), List.of(), false, List.of(), bname, id);
            }
            case "named" -> {
                int underlying = (int) d.longInt("underlying", path);
                TypeInfo u = infoOrShell(table, types, shells, underlying, functions, funcdeclSig);
                TypeInfo sh = shells[id];
                List<MethodInfo> methods = readMethods(table, types, shells, d, path,
                        functions, funcdeclSig, sh);
                // Named type size/align come from the underlying.
                yield new TypeInfo(tok, u.kind, sh.name, sh.pkgPath, u.size, u.align,
                        u.elem, u.key, u.arrayLen, u.chanDir, u.fields, methods,
                        u.ins, u.outs, u.variadic, List.of(), u.basicName, id);
            }
            case "array" -> {
                TypeInfo elem = infoOrShell(table, types, shells, (int) d.longInt("elem", path), functions, funcdeclSig);
                int len = (int) d.longInt("len", path);
                long size = elem.size < 0 ? -1 : elem.size * len;
                int align = elem.align;
                yield new TypeInfo(tok, GoKind.ARRAY, "", "", size, align,
                        elem, null, len, null, List.of(), List.of(),
                        List.of(), List.of(), false, List.of(), "", id);
            }
            case "slice" -> {
                TypeInfo elem = infoOrShell(table, types, shells, (int) d.longInt("elem", path), functions, funcdeclSig);
                yield new TypeInfo(tok, GoKind.SLICE, "", "", 24, 8,
                        elem, null, 0, null, List.of(), List.of(),
                        List.of(), List.of(), false, List.of(), "", id);
            }
            case "map" -> {
                TypeInfo key = infoOrShell(table, types, shells, (int) d.longInt("key", path), functions, funcdeclSig);
                TypeInfo elem = infoOrShell(table, types, shells, (int) d.longInt("elem", path), functions, funcdeclSig);
                yield new TypeInfo(tok, GoKind.MAP, "", "", 8, 8,
                        elem, key, 0, null, List.of(), List.of(),
                        List.of(), List.of(), false, List.of(), "", id);
            }
            case "pointer" -> {
                TypeInfo elem = infoOrShell(table, types, shells, (int) d.longInt("elem", path), functions, funcdeclSig);
                List<MethodInfo> methods = readMethods(table, types, shells, d, path,
                        functions, funcdeclSig, shells[id]);
                yield new TypeInfo(tok, GoKind.POINTER, "", "", 8, 8,
                        elem, null, 0, null, List.of(), methods,
                        List.of(), List.of(), false, List.of(), "", id);
            }
            case "chan" -> {
                TypeInfo elem = infoOrShell(table, types, shells, (int) d.longInt("elem", path), functions, funcdeclSig);
                String dir = d.string("dir", path);
                ChanDir cd = switch (dir) {
                    case "recv" -> ChanDir.RECV;
                    case "send" -> ChanDir.SEND;
                    default -> ChanDir.BOTH;
                };
                yield new TypeInfo(tok, GoKind.CHAN, "", "", 8, 8,
                        elem, null, 0, cd, List.of(), List.of(),
                        List.of(), List.of(), false, List.of(), "", id);
            }
            case "struct" -> {
                List<FieldInfo> fields = readFields(table, types, shells, d, path,
                        functions, funcdeclSig);
                long size = -1;
                int align = 1;
                // Size from the last field's offset + size when available.
                if (!fields.isEmpty()) {
                    FieldInfo last = fields.get(fields.size() - 1);
                    if (last.type.size >= 0) {
                        size = last.offset + last.type.size;
                    }
                    for (FieldInfo f : fields) {
                        align = Math.max(align, f.type.align);
                    }
                } else {
                    size = 0;
                }
                yield new TypeInfo(tok, GoKind.STRUCT, "", "", size, align,
                        null, null, 0, null, fields, List.of(),
                        List.of(), List.of(), false, List.of(), "", id);
            }
            case "interface" -> {
                List<String> ifaceMethods = new ArrayList<>();
                Json.Array methods = d.array("methods", path);
                for (int i = 0; i < methods.size(); i++) {
                    Json.Object m = arrObj(methods, i, path);
                    Json.Value ik = m.opt("implkey");
                    ifaceMethods.add(ik instanceof Json.Str s ? s.value() : m.string("name", path));
                }
                yield new TypeInfo(tok, GoKind.INTERFACE, "", "", 16, 8,
                        null, null, 0, null, List.of(), List.of(),
                        List.of(), List.of(), false, List.copyOf(ifaceMethods), "", id);
            }
            case "signature" -> {
                List<TypeInfo> ins = new ArrayList<>();
                List<TypeInfo> outs = new ArrayList<>();
                Json.Array params = d.array("params", path);
                for (int i = 0; i < params.size(); i++) {
                    ins.add(infoOrShell(table, types, shells,
                            (int) arrObj(params, i, path).longInt("type", path),
                            functions, funcdeclSig));
                }
                Json.Array results = d.array("results", path);
                for (int i = 0; i < results.size(); i++) {
                    outs.add(infoOrShell(table, types, shells,
                            (int) arrObj(results, i, path).longInt("type", path),
                            functions, funcdeclSig));
                }
                Json.Value var = d.opt("variadic");
                boolean variadic = var instanceof Json.Bool b && b.value();
                yield new TypeInfo(tok, GoKind.FUNC, "", "", 8, 8,
                        null, null, 0, null, List.of(), List.of(),
                        List.copyOf(ins), List.copyOf(outs), variadic, List.of(), "", id);
            }
            case "alias" -> {
                // P3-M8 fix: build alias as its RHS (resolved).
                int rhsId = (int) d.longInt("rhs", path);
                TypeInfo rhs = infoOrShell(table, types, shells, rhsId, functions, funcdeclSig);
                String aliasName = d.string("name", path);
                yield new TypeInfo(tok, rhs.kind, aliasName, rhs.pkgPath,
                        rhs.size, rhs.align, rhs.elem, rhs.key,
                        rhs.arrayLen, rhs.chanDir, rhs.fields, rhs.methods,
                        rhs.ins, rhs.outs, rhs.variadic,
                        rhs.interfaceMethods, rhs.basicName, id);
            }
            case "typeparam" -> {
                // Opaque placeholder for a type parameter in a template
                // position (see the shell case): null kind, unknown
                // size/align. kindOrdinal maps null kind to Invalid(0);
                // typeString and descriptorForTok fail loud on it — a
                // type parameter must never be observed at runtime (any
                // observation means the frontend leaked a template type
                // into executable code), so fabricating a kind here would
                // be a silent miscompilation (cf. the P1-4 "invalid type"
                // rule: fail loud, never launder into a fake kind).
                // (Adjudicated 2026-10-15: supersedes the constraint-transparent
                // design — inheriting the constraint's layout would silently
                // use the wrong layout for instantiated types.)
                yield new TypeInfo(tok, null, "", "", -1, -1,
                        null, null, 0, null, List.of(), List.of(),
                        List.of(), List.of(), false, List.of(), "", id);
            }
            default -> throw new FrontendException(path + ": reflect: build of " + kind);
        };
    }

    /** Reads a struct entry's fields. */
    private static List<FieldInfo> readFields(Table table, TypeTable types, TypeInfo[] shells,
            Json.Object d, String path,
            Map<Integer, GoFunc> functions, Map<Integer, Integer> funcdeclSig) {
        Json.Array fields = d.array("fields", path);
        List<FieldInfo> out = new ArrayList<>(fields.size());
        for (int i = 0; i < fields.size(); i++) {
            Json.Object f = arrObj(fields, i, path);
            String name = f.string("name", path);
            TypeInfo ft = infoOrShell(table, types, shells, (int) f.longInt("type", path), functions, funcdeclSig);
            String tag = f.string("tag", path);
            long offset = f.longInt("offset", path);
            // Unexported fields carry their package path in "pkgpath"
            // (staged patch 06); without it, lowercase name ⇒ unexported
            // in the current package (unknown path — recorded as such).
            Json.Value pp = f.opt("pkgpath");
            String pkgPath;
            if (pp instanceof Json.Str s) {
                pkgPath = s.value();
            } else if (!name.isEmpty() && Character.isLowerCase(name.charAt(0))) {
                pkgPath = "<unknown>";
            } else {
                pkgPath = "";
            }
            out.add(new FieldInfo(name, pkgPath, ft, tag, offset, new int[]{i},
                    isEmbedded(f)));
        }
        return List.copyOf(out);
    }

    /**
     * Reads a named/pointer entry's method table, sorted by name (gc
     * order). Signatures resolve via funcdecl obj→sig.
     */
    private static List<MethodInfo> readMethods(Table table, TypeTable types, TypeInfo[] shells,
            Json.Object d, String path,
            Map<Integer, GoFunc> functions, Map<Integer, Integer> funcdeclSig,
            TypeInfo receiver) {
        Json.Value mv = d.opt("methods");
        if (!(mv instanceof Json.Array methods) || methods.size() == 0) {
            return List.of();
        }
        TreeMap<String, MethodInfo> sorted = new TreeMap<>();
        for (int i = 0; i < methods.size(); i++) {
            Json.Object m = arrObj(methods, i, path);
            String name = m.string("name", path);
            int objId = (int) m.longInt("obj", path);
            // P3-M9 (C-1 fix): embedded-interface redispatch entries
            // (obj -1, wire "embeddedIface") carry no GoFunc, but reflect
            // must list them — gc includes promoted methods in
            // NumMethod/MethodByName. Build a MethodInfo with the
            // interface method's signature (receiver prepended) and a
            // synthetic GoFunc that performs the redispatch.
            if (objId == -1) {
                MethodInfo redispatch = readRedispatchMethod(table, types, shells,
                        path, functions, funcdeclSig, m, name, receiver,
                        sorted.size());
                sorted.put(name, redispatch);
                continue;
            }
            String recv = m.string("recv", path);
            // PkgPath for unexported methods (staged patch 06); without
            // it, lowercase ⇒ unexported, path unknown.
            Json.Value pp = m.opt("pkgpath");
            String pkgPath;
            if (pp instanceof Json.Str s) {
                pkgPath = s.value();
            } else if (!name.isEmpty() && Character.isLowerCase(name.charAt(0))) {
                pkgPath = "<unknown>";
            } else {
                pkgPath = "";
            }
            GoFunc func = functions.get(objId);
            if (func == null) {
                // Disk-MR stubs are skipped by the frontend; the method is
                // not available via reflect. Skip instead of failing.
                // (No warning: this is an expected condition for every
                // desugared multi-result method, and logging it would
                // pollute the guest program's stderr observable.)
                continue;
            }
            Integer sigId = funcdeclSig.get(objId);
            if (sigId == null) {
                throw new FrontendException(path + ": reflect: method \"" + name
                        + "\" (obj " + objId + ") has no funcdecl signature");
            }
            TypeInfo funcType = infoOrShell(table, types, shells, sigId, functions, funcdeclSig);
            Json.Value ik = m.opt("implkey");
            String implKey = ik instanceof Json.Str s ? s.value() : name;
            // P3-M9 (C-1 follow-up): methods promoted via an embedded
            // STRUCT field carry a non-empty wire "index" (embedding path).
            // The GoFunc expects the embedded struct type as receiver, but
            // reflect's Method.Func passes the outer struct. Wrap the func
            // to follow the embedding path first. (Embedded INTERFACE
            // promotion uses obj == -1 and is handled above.)
            int[] embPath = readWireIndex(m, path);
            GoFunc effectiveFunc = func;
            TypeInfo effectiveFuncType = funcType;
            if (embPath.length > 0) {
                boolean isPointerRecv = "pointer".equals(recv);
                effectiveFunc = makeStructEmbedWrapper(func, embPath, name, isPointerRecv);
                // The func type's receiver must be the OUTER type (what
                // reflect passes), not the embedded type. Rebuild with the
                // receiver TypeInfo prepended.
                //
                // The ins/outs/variadic come from the WIRE signature
                // (sigId), not from funcType.ins/outs: funcType may be a
                // cycle-cut shell (tok == -1, empty ins/outs) when the
                // method's signature references the type under construction
                // (e.g. func(t *Type) int while building *Type — the
                // internal/abi descriptor cycle). The wire params[0] is the
                // declared receiver (dropped here); helper-mapped methods
                // use the prependRecv convention so params[0] is likewise
                // the receiver.
                List<TypeInfo> wrapperIns = new ArrayList<>();
                wrapperIns.add(receiver);
                List<TypeInfo> wrapperOuts = new ArrayList<>();
                String sigPath = "types[" + sigId + "]";
                Json.Object sigDetail = types.entry(sigId, sigPath).object("detail", sigPath);
                Json.Array sigParams = sigDetail.array("params", sigPath);
                for (int pi = 1; pi < sigParams.size(); pi++) {
                    wrapperIns.add(infoOrShell(table, types, shells,
                            (int) arrObj(sigParams, pi, sigPath).longInt("type", sigPath),
                            functions, funcdeclSig));
                }
                Json.Array sigResults = sigDetail.array("results", sigPath);
                for (int ri = 0; ri < sigResults.size(); ri++) {
                    wrapperOuts.add(infoOrShell(table, types, shells,
                            (int) arrObj(sigResults, ri, sigPath).longInt("type", sigPath),
                            functions, funcdeclSig));
                }
                Json.Value sigVar = sigDetail.opt("variadic");
                boolean wrapperVariadic = sigVar instanceof Json.Bool b && b.value();
                int wrapperTok = table.byTok.size();
                effectiveFuncType = new TypeInfo(wrapperTok, GoKind.FUNC, "", "", 8, 8,
                        null, null, 0, null, List.of(), List.of(),
                        List.copyOf(wrapperIns), List.copyOf(wrapperOuts), wrapperVariadic,
                        List.of(), "", funcType.wireId);
                table.byTok.add(effectiveFuncType);
            }
            sorted.put(name, new MethodInfo(name, pkgPath, effectiveFuncType, effectiveFunc,
                    "pointer".equals(recv), sorted.size(), implKey));
        }
        return List.copyOf(sorted.values());
    }

    /**
     * Builds a MethodInfo for an embedded-interface redispatch entry
     * (obj -1, wire "embeddedIface"; P3-M9 C-1 fix).
     *
     * <p>The method is promoted via an embedded interface field (e.g.
     * {@code (*crypto/tls.listener).Addr} from the embedded
     * {@code net.Listener}). Its signature is resolved from the embedded
     * interface's type-table entry; the receiver type is prepended to
     * form the method's func type. The GoFunc is synthetic: it reads the
     * embedding path to the interface field value and dispatches the
     * method on it, mirroring IfaceDispatchNode's redispatch.
     */
    private static MethodInfo readRedispatchMethod(Table table, TypeTable types,
            TypeInfo[] shells, String path,
            Map<Integer, GoFunc> functions, Map<Integer, Integer> funcdeclSig,
            Json.Object m, String name, TypeInfo receiver, int index) {
        // PkgPath for unexported methods (same rule as ordinary methods).
        Json.Value pp = m.opt("pkgpath");
        String pkgPath;
        if (pp instanceof Json.Str s) {
            pkgPath = s.value();
        } else if (!name.isEmpty() && Character.isLowerCase(name.charAt(0))) {
            pkgPath = "<unknown>";
        } else {
            pkgPath = "";
        }

        // Embedded interface type-table id (frontend invariant: present
        // exactly when obj == -1).
        Json.Value embv = m.opt("embeddedIface");
        if (!(embv instanceof Json.Num n)) {
            throw new FrontendException(path + ": reflect: method \"" + name
                    + "\" has obj -1 but no embeddedIface (exporter bug)");
        }
        int embeddedIfaceId = (int) n.asLong(path);

        // Embedding path from the wire "index" array.
        int[] embPath = readWireIndex(m, path);

        // Resolve the method's signature from the embedded interface's
        // type-table entry (detail.methods[] carries name/sig/implkey).
        String ipath = "types[" + embeddedIfaceId + "]";
        Json.Object ientry = types.entry(embeddedIfaceId, ipath);
        Json.Object idetail = ientry.object("detail", ipath);
        Json.Value imv = idetail.opt("methods");
        if (!(imv instanceof Json.Array imethods)) {
            throw new FrontendException(path + ": reflect: embeddedIface "
                    + embeddedIfaceId + " has no methods list");
        }
        Integer sigId = null;
        List<String> embMethodNames = new ArrayList<>();
        for (int i = 0; i < imethods.size(); i++) {
            Json.Object im = arrObj(imethods, i, ipath);
            String iname = im.string("name", ipath);
            embMethodNames.add(iname);
            if (name.equals(iname)) {
                Json.Value sv = im.opt("sig");
                if (sv instanceof Json.Num sn) {
                    sigId = (int) sn.asLong(ipath);
                }
            }
        }
        if (sigId == null) {
            throw new FrontendException(path + ": reflect: method \"" + name
                    + "\" not found in embeddedIface " + embeddedIfaceId);
        }

        // Func type: interface signature with the receiver prepended.
        // (Interface method signatures never declare a receiver.)
        // The synthetic TypeInfo is registered in the table so its token
        // is valid for reflect boxing (Method.Func.Type etc.).
        TypeInfo sigInfo = infoOrShell(table, types, shells, sigId, functions, funcdeclSig);
        List<TypeInfo> ins = new ArrayList<>();
        ins.add(receiver);
        ins.addAll(sigInfo.ins);
        int funcTok = table.byTok.size();
        TypeInfo funcType = new TypeInfo(funcTok, GoKind.FUNC, "", "", 8, 8,
                null, null, 0, null, List.of(), List.of(),
                List.copyOf(ins), sigInfo.outs, sigInfo.variadic,
                List.of(), "", sigId);
        table.byTok.add(funcType);

        GoFunc func = makeRedispatchFunc(name, embPath, embeddedIfaceId,
                List.copyOf(embMethodNames));

        Json.Value ik = m.opt("implkey");
        String implKey = ik instanceof Json.Str s ? s.value() : name;
        // Redispatch entries hardcode recv "value" in the exporter.
        return new MethodInfo(name, pkgPath, funcType, func, false, index, implKey);
    }

    /** Reads a wire "index" int array (embedding path); null → null. */
    private static int[] readWireIndex(Json.Object eo, String path) {
        Json.Value iv = eo.opt("index");
        if (iv == null || iv instanceof Json.Null) {
            return new int[0];
        }
        if (!(iv instanceof Json.Array arr)) {
            throw new FrontendException(path + ": \"index\" must be an array");
        }
        int[] out = new int[arr.size()];
        for (int i = 0; i < arr.size(); i++) {
            Json.Value e = arr.get(i);
            if (!(e instanceof Json.Num num)) {
                throw new FrontendException(path + ": \"index\" entries must be numbers");
            }
            out[i] = (int) num.asLong(path);
        }
        return out;
    }

    /**
     * Wraps a GoFunc for a method promoted via an embedded STRUCT field
     * (P3-M9 C-1 follow-up).
     *
     * <p>The wrapped func expects the embedded struct type as receiver,
     * but reflect's Method.Func passes the outer struct value. This wrapper
     * follows the embedding path to extract the embedded value, then
     * invokes the inner func with it as receiver.
     *
     * <p>args[0] is the GoFunc itself (prepended by GoCalls.invoke);
     * args[1] is the outer receiver; args[2..] are method arguments.
     *
     * <p>For pointer-receiver methods, the embedded value is wrapped in a
     * GoPointer. Note: this points to a copy, not the field itself, so
     * mutations won't write back to the outer struct. This matches the
     * pre-existing value semantics for embedded fields via reflect; the
     * alternative (crash) is strictly worse.
     */
    private static GoFunc makeStructEmbedWrapper(GoFunc inner, int[] embPath,
            String methodName, boolean pointerReceiver) {
        RootNode root = new RootNode(null) {
            @Override
            public Object execute(VirtualFrame frame) {
                Object[] args = frame.getArguments();
                Object recv = args[1];
                Object data = recv instanceof GoPointer gp ? gp.referent() : recv;
                Object embedded = SelectorNode.readFieldPath(data, embPath, false);
                Object effectiveRecv = embedded;
                if (pointerReceiver && !(embedded instanceof GoPointer)) {
                    // Pointer-receiver method needs *Inner; wrap the value.
                    effectiveRecv = new GoPointer(embedded);
                }
                Object[] callArgs = new Object[args.length - 1];
                callArgs[0] = effectiveRecv;
                System.arraycopy(args, 2, callArgs, 1, args.length - 2);
                return GoCalls.invoke(inner, callArgs);
            }
        };
        return new GoFunc(root.getCallTarget(), null);
    }

    /**
     * Builds a synthetic GoFunc that dispatches a promoted method via the
     * embedded interface field (P3-M9 C-1 fix).
     *
     * <p>args[0] is the receiver (struct value or pointer to struct);
     * args[1..] are the method arguments. The function reads the embedding
     * path to the interface field value, builds the inner itab, and
     * invokes the method — mirroring IfaceDispatchNode's redispatch loop,
     * including chaining through nested embedded interfaces.
     */
    private static GoFunc makeRedispatchFunc(String methodName, int[] embPath,
            int embeddedIfaceId, List<String> embMethodNames) {
        RootNode root = new RootNode(null) {
            @Override
            public Object execute(VirtualFrame frame) {
                Object[] args = frame.getArguments();
                // GoCalls.invoke prepends the GoFunc itself as args[0];
                // args[1] is the receiver, args[2..] are method arguments.
                Object recv = args[1];
                Object data = recv instanceof GoPointer gp ? gp.referent() : recv;
                Object fieldVal = SelectorNode.readFieldPath(data, embPath, false);
                if (!(fieldVal instanceof GoInterface gi)) {
                    throw new FrontendException("reflect: redispatch method \""
                            + methodName + "\": embedded field is not an interface value");
                }
                // Dispatch on the interface value's itab, chaining through
                // nested embedded-interface redispatches if needed.
                Object curData = gi.data();
                String descName = ((GoTypeDescriptor) gi.descriptor()).name();
                IfaceItab itab = IfaceMethodTable.itabFor(descName, embeddedIfaceId,
                        embMethodNames);
                IfaceMethodTarget target = itab.methods().get(methodName);
                if (target == null) {
                    throw new FrontendException("reflect: redispatch method \""
                            + methodName + "\" missing from itab for \"" + descName + "\"");
                }
                while (target.isEmbeddedRedispatch()) {
                    Object fv = SelectorNode.readFieldPath(curData, target.recvPath(), false);
                    if (!(fv instanceof GoInterface cgi)) {
                        throw new FrontendException("reflect: redispatch method \""
                                + methodName + "\": nested embedded field is not an interface");
                    }
                    curData = cgi.data();
                    String idesc = ((GoTypeDescriptor) cgi.descriptor()).name();
                    IfaceItab iitab = IfaceMethodTable.itabFor(idesc,
                            target.embeddedIfaceId(), target.embeddedIfaceMethods());
                    target = iitab.methods().get(methodName);
                    if (target == null) {
                        throw new FrontendException("reflect: redispatch method \""
                                + methodName + "\" missing from nested itab for \"" + idesc + "\"");
                    }
                }
                GoFunc inner = target.func();
                if (inner == null) {
                    throw new FrontendException("reflect: redispatch method \""
                            + methodName + "\" resolved to null function");
                }
                // callArgs excludes the leading GoFunc (GoCalls.invoke
                // prepends it): [receiver, methodArgs...].
                Object[] callArgs = new Object[args.length - 1];
                callArgs[0] = curData;
                System.arraycopy(args, 2, callArgs, 1, args.length - 2);
                return GoCalls.invoke(inner, callArgs);
            }
        };
        return new GoFunc(root.getCallTarget(), null);
    }

    // ------------------------------------------------------------------
    // Query operations (the __reflect_t* intrinsics).
    // ------------------------------------------------------------------

    /** Kind of a token as the synthetic Kind ordinal. */
    public static int kindOf(long tok) {
        return kindOrdinal(table().info(tok));
    }

    /** Maps a TypeInfo to the synthetic reflect.Kind constant ordinal.
     * The wire basic name distinguishes int/int64, uint/uint64/uintptr,
     * byte/uint8, rune/int32 (GoKind conflates them). */
    public static int kindOrdinal(TypeInfo t) {
        if (t.kind == null) {
            return 0; // Invalid
        }
        if (!t.basicName.isEmpty()) {
            return switch (t.basicName) {
                case "bool" -> 1;
                case "int" -> 2;
                case "int8" -> 3;
                case "int16" -> 4;
                case "int32", "rune" -> 5;
                case "int64" -> 6;
                case "uint" -> 7;
                case "uint8", "byte" -> 8;
                case "uint16" -> 9;
                case "uint32" -> 10;
                case "uint64" -> 11;
                case "uintptr" -> 12;
                case "float32" -> 13;
                case "float64" -> 14;
                case "complex64" -> 15;
                case "complex128" -> 16;
                case "string" -> 24;
                // Wire name is "Pointer" (see basicKind); accept the
                // qualified spelling too.
                case "Pointer", "unsafe.Pointer" -> 26;
                default -> throw new IllegalStateException("reflect: kind of " + t.basicName);
            };
        }
        return switch (t.kind) {
            case ARRAY -> 17;
            case CHAN -> 18;
            case FUNC -> 19;
            case INTERFACE -> 20;
            case MAP -> 21;
            case POINTER -> 22;
            case SLICE -> 23;
            case STRUCT -> 25;
            default -> throw new IllegalStateException("reflect: kind of " + t.kind);
        };
    }

    /** gc-exact Type.String() rendering. */
    public static String typeString(long tok) {
        TypeInfo t = table().info(tok);
        if (t.isNamed()) {
            return t.name;
        }
        if (!t.basicName.isEmpty()) {
            // gc renders unsafe.Pointer qualified even though the wire
            // (and go/types) name is bare "Pointer".
            if (t.basicName.equals("Pointer")) {
                return "unsafe.Pointer";
            }
            return t.basicName;
        }
        if (t.kind == null) {
            // A type parameter has no runtime type identity (see the
            // "typeparam" cases in shell/buildInfo): rendering one means
            // a template type leaked into a runtime-observable position.
            // Fail loud with the wire id; never fabricate a type string.
            throw new IllegalStateException(
                    "reflect: String of type parameter (types[" + t.wireId + "])");
        }
        return switch (t.kind) {
            case BOOL -> "bool";
            case STRING -> "string";
            case UNSAFE_POINTER -> "unsafe.Pointer";
            case ARRAY -> "[" + t.arrayLen + "]" + typeString(t.elem.tok);
            case SLICE -> "[]" + typeString(t.elem.tok);
            case POINTER -> "*" + typeString(t.elem.tok);
            case MAP -> "map[" + typeString(t.key.tok) + "]" + typeString(t.elem.tok);
            case CHAN -> switch (t.chanDir) {
                case RECV -> "<-chan " + typeString(t.elem.tok);
                case SEND -> "chan<- " + typeString(t.elem.tok);
                case BOTH -> "chan " + typeString(t.elem.tok);
            };
            case FUNC -> funcString(t);
            case STRUCT -> structString(t);
            case INTERFACE -> interfaceString(t);
            default -> throw new IllegalStateException("reflect: String of " + t.kind);
        };
    }

    private static String funcString(TypeInfo t) {
        StringBuilder sb = new StringBuilder("func(");
        for (int i = 0; i < t.ins.size(); i++) {
            if (i > 0) {
                sb.append(", ");
            }
            TypeInfo in = t.ins.get(i);
            if (t.variadic && i == t.ins.size() - 1 && in.kind == GoKind.SLICE) {
                sb.append("...").append(typeString(in.elem.tok));
            } else {
                sb.append(typeString(in.tok));
            }
        }
        sb.append(')');
        if (t.outs.isEmpty()) {
            return sb.toString();
        }
        sb.append(' ');
        if (t.outs.size() > 1) {
            sb.append('(');
        }
        for (int i = 0; i < t.outs.size(); i++) {
            if (i > 0) {
                sb.append(", ");
            }
            sb.append(typeString(t.outs.get(i).tok));
        }
        if (t.outs.size() > 1) {
            sb.append(')');
        }
        return sb.toString();
    }

    private static String structString(TypeInfo t) {
        StringBuilder sb = new StringBuilder("struct { ");
        for (int i = 0; i < t.fields.size(); i++) {
            if (i > 0) {
                sb.append("; ");
            }
            FieldInfo f = t.fields.get(i);
            // gc renders an embedded field as just its type (no field
            // name): "struct { main.E }", "struct { *main.E }".
            if (!f.anonymous) {
                sb.append(f.name).append(' ');
            }
            sb.append(typeString(f.type.tok));
            // gc renders the tag Go-quoted: struct { X int "json:\"x\"" }.
            if (!f.tag.isEmpty()) {
                sb.append(' ').append(goQuote(f.tag));
            }
        }
        return sb.append(" }").toString();
    }

    /** Go-syntax double-quoted string literal (strconv.Quote equivalent). */
    private static String goQuote(String s) {
        StringBuilder sb = new StringBuilder("\"");
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            switch (c) {
                case '"' -> sb.append("\\\"");
                case '\\' -> sb.append("\\\\");
                case '\n' -> sb.append("\\n");
                case '\r' -> sb.append("\\r");
                case '\t' -> sb.append("\\t");
                default -> {
                    if (c < 0x20 || c == 0x7F) {
                        sb.append(String.format("\\x%02x", (int) c));
                    } else {
                        sb.append(c);
                    }
                }
            }
        }
        return sb.append('"').toString();
    }

    private static String interfaceString(TypeInfo t) {
        // Interface method lists are not retained (only needed for
        // Implements, which uses the wire directly); render structurally.
        return "interface {}";
    }

    /** Implements: does type t implement interface u? */
    public static boolean implementsType(long tok, long uTok) {
        Table table = table();
        TypeInfo t = table.info(tok);
        TypeInfo u = table.info(uTok);
        return implementsDetailed(t, u);
    }

    /**
     * t implements u iff every method in u's method set has a method in
     * t's method set with the same implkey (name + receiver-stripped
     * signature; unexported names are package-qualified, so cross-package
     * unexported methods never match — gc-exact). The empty interface is
     * implemented by every type.
     */
    private static boolean implementsDetailed(TypeInfo t, TypeInfo u) {
        if (u.interfaceMethods.isEmpty()) {
            return true;
        }
        if (t.methods.isEmpty()) {
            return false;
        }
        java.util.Set<String> tKeys = new java.util.HashSet<>();
        for (MethodInfo m : t.methods) {
            tKeys.add(m.implKey);
        }
        for (String need : u.interfaceMethods) {
            if (!tKeys.contains(need)) {
                return false;
            }
        }
        return true;
    }

    /** AssignableTo: gc's assignability rules. */
    public static boolean assignableTo(long tok, long uTok) {
        if (tok == uTok) {
            return true;
        }
        Table table = table();
        TypeInfo t = table.info(tok);
        TypeInfo u = table.info(uTok);
        // Unnamed types with identical underlying are assignable.
        if (!t.isNamed() && !u.isNamed() && t.kind == u.kind) {
            return compositeAssignable(t, u);
        }
        // A type is assignable to an interface it implements (this covers
        // the empty interface: implementsDetailed is true for it).
        if (u.kind == GoKind.INTERFACE) {
            return implementsDetailed(t, u);
        }
        return false;
    }

    private static boolean compositeAssignable(TypeInfo t, TypeInfo u) {
        if (t.kind != u.kind) {
            return false;
        }
        return switch (t.kind) {
            case ARRAY -> t.arrayLen == u.arrayLen && t.elem.tok == u.elem.tok;
            case SLICE -> t.elem.tok == u.elem.tok;
            case MAP -> t.key.tok == u.key.tok && t.elem.tok == u.elem.tok;
            case POINTER -> t.elem.tok == u.elem.tok;
            case CHAN -> t.elem.tok == u.elem.tok
                    && (t.chanDir == u.chanDir || u.chanDir == ChanDir.BOTH);
            case FUNC -> t.ins.equals(u.ins) && t.outs.equals(u.outs)
                    && t.variadic == u.variadic;
            case STRUCT -> t.tok == u.tok; // unnamed structs: same tok ⇔ identical
            default -> t.tok == u.tok;
        };
    }

    private static boolean isEmptyInterface(TypeInfo u) {
        return u.kind == GoKind.INTERFACE;
    }

    /** ConvertibleTo: gc's convertibility rules (v1-relevant subset). */
    public static boolean convertibleTo(long tok, long uTok) {
        if (tok == uTok || assignableTo(tok, uTok)) {
            return true;
        }
        Table table = table();
        TypeInfo t = table.info(tok);
        TypeInfo u = table.info(uTok);
        int tk = kindOrdinal(t);
        int uk = kindOrdinal(u);
        boolean tInt = tk >= 2 && tk <= 12;
        boolean uInt = uk >= 2 && uk <= 12;
        boolean tFloat = tk == 13 || tk == 14;
        boolean uFloat = uk == 13 || uk == 14;
        boolean tComplex = tk == 15 || tk == 16;
        boolean uComplex = uk == 15 || uk == 16;
        // Numeric ↔ numeric.
        if ((tInt || tFloat || tComplex) && (uInt || uFloat || uComplex)) {
            return true;
        }
        // String ↔ []byte / []rune (slice of uint8/int32).
        if (t.kind == GoKind.STRING && u.kind == GoKind.SLICE
                && (u.elem.kind == GoKind.UINT8 || u.elem.kind == GoKind.INT32)) {
            return true;
        }
        if (u.kind == GoKind.STRING && t.kind == GoKind.SLICE
                && (t.elem.kind == GoKind.UINT8 || t.elem.kind == GoKind.INT32)) {
            return true;
        }
        // Integer → string (gofrontend rejects this in v1? gc allows with
        // vet warning; keep it convertible — the runtime converts).
        if (tInt && u.kind == GoKind.STRING) {
            return true;
        }
        // Slice ↔ array-pointer, etc. are not in v1 scope.
        return false;
    }

    /** ptrTo: token for *T (synthesized structurally; method table empty). */
    public static long ptrTo(long tok) {
        Table table = table();
        TypeInfo t = table.info(tok);
        String key = "ptr:" + structuralKey(t);
        Long existing = table.keyToTok.get(key);
        if (existing != null) {
            return existing;
        }
        // Synthesize: no wire id, empty method table. Documented in the
        // design: *T from PtrTo has no methods; when the wire carries a
        // *T entry (methods present) it canonicalizes to this same key
        // only if the method sets agree — otherwise the wire entry keeps
        // its own token and this synthesized one is method-less.
        // (In practice the wire always carries *T for named T, so this
        // path serves genuinely new pointers.)
        long newTok = table.byTok.size();
        table.keyToTok.put(key, newTok);
        TypeInfo info = new TypeInfo(newTok, GoKind.POINTER, "", "", 8, 8,
                t, null, 0, null, List.of(), List.of(),
                List.of(), List.of(), false, List.of(), "", -1);
        table.byTok.add(info);
        return newTok;
    }

    /** arrayOf: token for [n]T (synthesized; no methods). */
    public static long arrayOf(long elemTok, int n) {
        Table table = table();
        TypeInfo elem = table.info(elemTok);
        String key = "array:" + n + ":" + structuralKey(elem);
        Long existing = table.keyToTok.get(key);
        if (existing != null) {
            return existing;
        }
        long newTok = table.byTok.size();
        table.keyToTok.put(key, newTok);
        long size = elem.size * n;
        TypeInfo info = new TypeInfo(newTok, GoKind.ARRAY, "", "", size, elem.align,
                elem, null, n, null, List.of(), List.of(),
                List.of(), List.of(), false, List.of(), "", -1);
        table.byTok.add(info);
        return newTok;
    }

    /** sliceOf: token for []T (synthesized; no methods). */
    public static long sliceOf(long elemTok) {
        Table table = table();
        TypeInfo elem = table.info(elemTok);
        String key = "slice:" + structuralKey(elem);
        Long existing = table.keyToTok.get(key);
        if (existing != null) {
            return existing;
        }
        long newTok = table.byTok.size();
        table.keyToTok.put(key, newTok);
        // Slice header: ptr, len, cap (3 words)
        TypeInfo info = new TypeInfo(newTok, GoKind.SLICE, "", "", 24, 8,
                elem, null, 0, null, List.of(), List.of(),
                List.of(), List.of(), false, List.of(), "", -1);
        table.byTok.add(info);
        return newTok;
    }

    /** mapOf: token for map[K]V (synthesized; no methods). */
    public static long mapOf(long keyTok, long elemTok) {
        Table table = table();
        TypeInfo key = table.info(keyTok);
        TypeInfo elem = table.info(elemTok);
        String keyStr = "map:" + structuralKey(key) + ":" + structuralKey(elem);
        Long existing = table.keyToTok.get(keyStr);
        if (existing != null) {
            return existing;
        }
        long newTok = table.byTok.size();
        table.keyToTok.put(keyStr, newTok);
        // Map is a pointer-sized header in the value model
        TypeInfo info = new TypeInfo(newTok, GoKind.MAP, "", "", 8, 8,
                elem, key, 0, null, List.of(), List.of(),
                List.of(), List.of(), false, List.of(), "", -1);
        table.byTok.add(info);
        return newTok;
    }

    /** chanOf: token for chan T with direction (synthesized; no methods). */
    public static long chanOf(long elemTok, int dirOrdinal) {
        Table table = table();
        TypeInfo elem = table.info(elemTok);
        ChanDir dir = ChanDir.values()[dirOrdinal];
        String key = "chan:" + dir + ":" + structuralKey(elem);
        Long existing = table.keyToTok.get(key);
        if (existing != null) {
            return existing;
        }
        long newTok = table.byTok.size();
        table.keyToTok.put(key, newTok);
        TypeInfo info = new TypeInfo(newTok, GoKind.CHAN, "", "", 8, 8,
                elem, null, 0, dir, List.of(), List.of(),
                List.of(), List.of(), false, List.of(), "", -1);
        table.byTok.add(info);
        return newTok;
    }

    /** funcOf: token for func(in...) out (synthesized; no methods). */
    public static long funcOf(long[] inToks, long[] outToks, boolean variadic) {
        Table table = table();
        StringBuilder keySb = new StringBuilder("func:");
        List<TypeInfo> ins = new ArrayList<>();
        for (long tok : inToks) {
            TypeInfo ti = table.info(tok);
            ins.add(ti);
            keySb.append(structuralKey(ti)).append(",");
        }
        keySb.append("->");
        List<TypeInfo> outs = new ArrayList<>();
        for (long tok : outToks) {
            TypeInfo ti = table.info(tok);
            outs.add(ti);
            keySb.append(structuralKey(ti)).append(",");
        }
        if (variadic) {
            keySb.append("variadic");
        }
        String key = keySb.toString();
        Long existing = table.keyToTok.get(key);
        if (existing != null) {
            return existing;
        }
        long newTok = table.byTok.size();
        table.keyToTok.put(key, newTok);
        TypeInfo info = new TypeInfo(newTok, GoKind.FUNC, "", "", 8, 8,
                null, null, 0, null, List.of(), List.of(),
                ins, outs, variadic, List.of(), "", -1);
        table.byTok.add(info);
        return newTok;
    }

    /**
     * structOf: token for struct{...} (synthesized; method promotion from
     * anonymous fields is computed by {@link #promoteMethods}).
     *
     * <p>Field offsets follow the gc ABI layout: each field is placed at
     * the next offset aligned to its alignment, and the struct size is
     * aligned up to the maximum field alignment. (Verified field-by-field
     * against gc 1.27.1 by probe.)
     */
    public static long structOf(String[] names, long[] toks, String[] tags,
            String[] pkgPaths, boolean[] anon) {
        Table table = table();
        StringBuilder keySb = new StringBuilder("struct{");
        List<FieldInfo> fields = new ArrayList<>(names.length);
        long offset = 0;
        int maxAlign = 1;
        for (int i = 0; i < names.length; i++) {
            TypeInfo ft = table.info(toks[i]);
            int a = Math.max(ft.align, 1);
            offset = alignUp(offset, a);
            if (a > maxAlign) {
                maxAlign = a;
            }
            fields.add(new FieldInfo(names[i], pkgPaths[i], ft, tags[i], offset,
                    new int[]{i}, anon[i]));
            if (i > 0) {
                keySb.append(';');
            }
            keySb.append(names[i]).append(',').append(tags[i]).append(',')
                    .append(pkgPaths[i]).append(',').append(anon[i]).append(',')
                    .append(structuralKey(ft));
            offset += ft.size;
        }
        keySb.append('}');
        String key = keySb.toString();
        Long existing = table.keyToTok.get(key);
        if (existing != null) {
            return existing;
        }
        long size = alignUp(offset, maxAlign);
        long newTok = table.byTok.size();
        table.keyToTok.put(key, newTok);
        List<FieldInfo> fieldInfos = List.copyOf(fields);
        // Reserve the struct's index BEFORE promoteMethods appends wrapper
        // func types: newTok must address the struct, not a wrapper.
        TypeInfo info = new TypeInfo(newTok, GoKind.STRUCT, "", "", size, maxAlign,
                null, null, 0, null, fieldInfos, List.of(),
                List.of(), List.of(), false, List.of(), "", -1);
        table.byTok.add(info);
        // Promoted methods from anonymous (embedded) fields, gc rules:
        // shallower embedding depth wins; same-depth duplicates are
        // ambiguous and dropped; only exported method names promote.
        List<MethodInfo> promoted = promoteMethods(table, info, fieldInfos);
        if (!promoted.isEmpty()) {
            table.byTok.set((int) newTok, new TypeInfo(newTok, GoKind.STRUCT, "", "", size, maxAlign,
                    null, null, 0, null, fieldInfos, List.copyOf(promoted),
                    List.of(), List.of(), false, List.of(), "", -1));
        }
        return newTok;
    }

    private static long alignUp(long offset, int align) {
        long a = align;
        return (offset + a - 1) & ~(a - 1);
    }

    /**
     * Computes the promoted method set for a synthesized struct type,
     * mirroring gc's StructOf promotion (reflect/type.go) exactly:
     * single-level, in field order; the Go side (StructOf validation)
     * already enforced the Issue-15924 position/count panics, so any
     * embedded field with methods here is necessarily the first and
     * only field.
     *
     * <ul>
     *   <li>Embedded struct E: promote E's methods.
     *   <li>Embedded *E: promote *E's methods, then E's methods (gc
     *       appends both; duplicates are kept — observed gc behavior).
     *   <li>Embedded interface: gc promotes via a stub; the redispatch
     *       machinery needs a wire interface id which synthesized types
     *       lack — documented gap, skipped (the Go side still mirrors
     *       gc's unexported-method panic).
     * </ul>
     *
     * <p>Each promoted method wraps the embedded type's GoFunc so the
     * outer struct value is routed down the embedding path. A
     * value-receiver method promoted through *E embedding needs the
     * pointer dereferenced (gc semantics); that uses a dedicated
     * wrapper.
     */
    private static List<MethodInfo> promoteMethods(Table table, TypeInfo outer,
            List<FieldInfo> fields) {
        List<MethodInfo> out = new ArrayList<>();
        for (int i = 0; i < fields.size(); i++) {
            FieldInfo f = fields.get(i);
            if (!f.anonymous) {
                continue;
            }
            TypeInfo ft = f.type;
            if (ft.kind == GoKind.INTERFACE) {
                continue;
            }
            int[] path = new int[]{i};
            if (ft.kind == GoKind.POINTER && ft.elem != null) {
                boolean ptrEmbed = true;
                for (MethodInfo m : ft.methods) {
                    out.add(promoteOne(table, outer, m, path, ptrEmbed, out.size()));
                }
                if (ft.elem.kind == GoKind.STRUCT) {
                    for (MethodInfo m : ft.elem.methods) {
                        out.add(promoteOne(table, outer, m, path, ptrEmbed, out.size()));
                    }
                }
            } else if (ft.kind == GoKind.STRUCT) {
                for (MethodInfo m : ft.methods) {
                    out.add(promoteOne(table, outer, m, path, false, out.size()));
                }
            }
            // Other kinds (e.g. *int): no methods to promote.
        }
        return List.copyOf(out);
    }

    /** Promotes one method: wraps it to route the outer receiver down the embedding path. */
    private static MethodInfo promoteOne(Table table, TypeInfo outer, MethodInfo m,
            int[] path, boolean embeddedIsPointer, int index) {
        GoFunc wrapped;
        if (embeddedIsPointer && !m.pointerReceiver) {
            // Value-receiver method via *E: the path yields the *E
            // pointer; dereference it for the value receiver.
            wrapped = makeDerefEmbedWrapper(m.func, path);
        } else {
            wrapped = makeStructEmbedWrapper(m.func, path, m.name, m.pointerReceiver);
        }
        // The method's func type gets the OUTER struct as receiver.
        List<TypeInfo> ins = new ArrayList<>();
        ins.add(outer);
        ins.addAll(m.funcType.ins.subList(1, m.funcType.ins.size()));
        long wrapperTok = table.byTok.size();
        TypeInfo wrapperType = new TypeInfo(wrapperTok, GoKind.FUNC,
                "", "", 8, 8, null, null, 0, null,
                List.of(), List.of(), List.copyOf(ins),
                m.funcType.outs, m.funcType.variadic,
                List.of(), "", -1);
        table.byTok.add(wrapperType);
        // gc reports PkgPath "" for promoted methods (observed).
        return new MethodInfo(m.name, "", wrapperType, wrapped,
                m.pointerReceiver, index, m.implKey);
    }

    /**
     * Wraps a GoFunc for a value-receiver method promoted via an embedded
     * *E field. The embedding path yields the *E pointer; it is
     * dereferenced so the value-receiver method gets the E value (gc
     * semantics). Mirrors {@link #makeStructEmbedWrapper}'s shape.
     */
    private static GoFunc makeDerefEmbedWrapper(GoFunc inner, int[] embPath) {
        RootNode root = new RootNode(null) {
            @Override
            public Object execute(VirtualFrame frame) {
                Object[] args = frame.getArguments();
                Object recv = args[1];
                Object data = recv instanceof GoPointer gp ? gp.referent() : recv;
                Object embedded = SelectorNode.readFieldPath(data, embPath, false);
                Object effectiveRecv = embedded instanceof GoPointer gp2 ? gp2.referent() : embedded;
                Object[] callArgs = new Object[args.length - 1];
                callArgs[0] = effectiveRecv;
                System.arraycopy(args, 2, callArgs, 1, args.length - 2);
                return GoCalls.invoke(inner, callArgs);
            }
        };
        return new GoFunc(root.getCallTarget(), null);
    }

    /** Structural key of an already-built TypeInfo (for ptrTo). */
    private static String structuralKey(TypeInfo t) {
        // Reuse the canonical key scheme: named types by name, composites
        // by structure. Synthesized struct/func/interface types (from
        // StructOf/FuncOf) key by their full structure so PtrTo(ArrayOf(
        // StructOf(...))) etc. canonicalize correctly instead of
        // colliding on kind.
        if (t.isNamed()) {
            return "named:" + t.pkgPath + "." + t.name;
        }
        return switch (t.kind) {
            case ARRAY -> "array:" + t.arrayLen + ":" + structuralKey(t.elem);
            case SLICE -> "slice:" + structuralKey(t.elem);
            case MAP -> "map:" + structuralKey(t.key) + ":" + structuralKey(t.elem);
            case POINTER -> "ptr:" + structuralKey(t.elem);
            case CHAN -> "chan:" + t.chanDir + ":" + structuralKey(t.elem);
            case STRUCT -> {
                StringBuilder sb = new StringBuilder("struct{");
                for (int i = 0; i < t.fields.size(); i++) {
                    if (i > 0) {
                        sb.append(';');
                    }
                    FieldInfo f = t.fields.get(i);
                    sb.append(f.name).append(',').append(f.tag).append(',')
                            .append(f.anonymous).append(',')
                            .append(structuralKey(f.type));
                }
                yield sb.append('}').toString();
            }
            case FUNC -> {
                StringBuilder sb = new StringBuilder("func(");
                for (TypeInfo in : t.ins) {
                    sb.append(structuralKey(in)).append(',');
                }
                sb.append(")->(");
                for (TypeInfo out : t.outs) {
                    sb.append(structuralKey(out)).append(',');
                }
                if (t.variadic) {
                    sb.append("...");
                }
                yield sb.append(')').toString();
            }
            case INTERFACE -> "interface{" + String.join(",", t.interfaceMethods) + "}";
            default -> "basic:" + t.kind;
        };
    }

    // ------------------------------------------------------------------
    // Runtime descriptors and interface boxing.
    // ------------------------------------------------------------------

    private static volatile graalgo.values.GoStructDescriptor valueDesc;
    private static volatile graalgo.values.GoStructDescriptor methodDesc;
    private static volatile graalgo.values.GoStructDescriptor structFieldDesc;
    private static volatile graalgo.values.GoStructDescriptor rtypeDescStruct;
    private static volatile graalgo.values.GoTypeDescriptor rtypeDesc;

    /**
     * Captures the struct descriptors for the synthetic reflect structs.
     * Called at install from the wire (built via layout(), identical to
     * NodeFactory's). The qualified names are "reflect.Value",
     * "reflect.Method", "reflect.StructField", "reflect.rtype".
     */
    static void captureDescriptors(TypeTable types) {
        // P3-M8 fix: tolerant of programs that don't import reflect.
        // If the types aren't present, leave descriptors null; reflect
        // operations will fail fast when actually used.
        try {
            valueDesc = structDescFor(types, "reflect.Value");
        } catch (FrontendException e) {
            valueDesc = null;
        }
        try {
            methodDesc = structDescFor(types, "reflect.Method");
        } catch (FrontendException e) {
            methodDesc = null;
        }
        try {
            structFieldDesc = structDescFor(types, "reflect.StructField");
        } catch (FrontendException e) {
            structFieldDesc = null;
        }
        try {
            rtypeDescStruct = structDescFor(types, "reflect.rtype");
        } catch (FrontendException e) {
            rtypeDescStruct = null;
        }
        // The GoTypeDescriptors come from the patch-03 registry.
        try {
            rtypeDesc = descriptorFor(types, "reflect.rtype");
        } catch (FrontendException e) {
            rtypeDesc = null;
        }
    }

    /** Public: struct descriptor for any synthetic named struct type. */
    public static graalgo.values.GoStructDescriptor structDescriptorFor(String qualifiedName) {
        Table table = table();
        // Find the wire id via the table's wireIdToTok keys... instead,
        // rescan: we need the TypeTable. Cached at install.
        graalgo.frontend.TypeTable types = cachedTypes;
        if (types == null) {
            throw new IllegalStateException("reflect: type table not cached");
        }
        return structDescFor(types, qualifiedName);
    }

    private static volatile graalgo.frontend.TypeTable cachedTypes;

    static void cacheTypes(graalgo.frontend.TypeTable types) {
        cachedTypes = types;
    }

    private static graalgo.values.GoStructDescriptor structDescFor(
            TypeTable types, String qualifiedName) {
        int n = types.size();
        for (int id = 0; id < n; id++) {
            String path = "types[" + id + "]";
            Json.Object entry = types.entry(id, path);
            if (!"named".equals(entry.string("kind", path))) {
                continue;
            }
            Json.Object d = entry.object("detail", path);
            if (!qualifiedName.equals(d.string("name", path))) {
                continue;
            }
            int underlying = (int) d.longInt("underlying", path);
            Json.Object uentry = types.entry(underlying, path);
            Json.Object ud = uentry.object("detail", path);
            Json.Array fields = ud.array("fields", path);
            List<String> names = new ArrayList<>(fields.size());
            List<GoKind> kinds = new ArrayList<>(fields.size());
            for (int i = 0; i < fields.size(); i++) {
                Json.Object f = arrObj(fields, i, path);
                names.add(f.string("name", path));
                kinds.add(fieldKind(types, (int) f.longInt("type", path), path));
            }
            return graalgo.values.GoStructDescriptor.layout(qualifiedName, names, kinds);
        }
        throw new FrontendException("reflect: no named type " + qualifiedName);
    }

    /** GoKind for a wire type id (following named→underlying). */
    private static GoKind fieldKind(TypeTable types, int id, String path) {
        Json.Object entry = types.entry(id, path);
        String kind = entry.string("kind", path);
        Json.Object d = entry.object("detail", path);
        return switch (kind) {
            case "basic" -> basicKind(d.string("name", path));
            case "named" -> fieldKind(types, (int) d.longInt("underlying", path), path);
            case "array", "slice", "map", "pointer", "chan" -> kindToGoKind(kind);
            case "struct" -> GoKind.STRUCT;
            case "interface" -> GoKind.INTERFACE;
            case "signature" -> GoKind.FUNC;
            default -> throw new FrontendException(path + ": reflect: kind of " + kind);
        };
    }

    private static GoKind kindToGoKind(String kind) {
        return switch (kind) {
            case "array" -> GoKind.ARRAY;
            case "slice" -> GoKind.SLICE;
            case "map" -> GoKind.MAP;
            case "pointer" -> GoKind.POINTER;
            case "chan" -> GoKind.CHAN;
            default -> throw new IllegalArgumentException(kind);
        };
    }

    private static graalgo.values.GoTypeDescriptor descriptorFor(
            TypeTable types, String qualifiedName) {
        int n = types.size();
        for (int id = 0; id < n; id++) {
            String path = "types[" + id + "]";
            Json.Object entry = types.entry(id, path);
            if (!"named".equals(entry.string("kind", path))) {
                continue;
            }
            Json.Object d = entry.object("detail", path);
            if (qualifiedName.equals(d.string("name", path))) {
                graalgo.values.GoTypeDescriptor desc =
                        graalgo.frontend.NodeFactory.descriptorForWireId(id);
                if (desc == null) {
                    throw new FrontendException(
                            "reflect: no descriptor for " + qualifiedName + " (patch 03)");
                }
                return desc;
            }
        }
        throw new FrontendException("reflect: no named type " + qualifiedName);
    }

    /** The Value struct descriptor. */
    public static graalgo.values.GoStructDescriptor valueDescriptor() {
        graalgo.values.GoStructDescriptor d = valueDesc;
        if (d == null) {
            throw new IllegalStateException("reflect: descriptors not captured");
        }
        return d;
    }

    /** The Method struct descriptor. */
    public static graalgo.values.GoStructDescriptor methodDescriptor() {
        graalgo.values.GoStructDescriptor d = methodDesc;
        if (d == null) {
            throw new IllegalStateException("reflect: descriptors not captured");
        }
        return d;
    }

    /** The StructField struct descriptor. */
    public static graalgo.values.GoStructDescriptor structFieldDescriptor() {
        graalgo.values.GoStructDescriptor d = structFieldDesc;
        if (d == null) {
            throw new IllegalStateException("reflect: descriptors not captured");
        }
        return d;
    }

    /**
     * Boxes a type token as a {@code reflect.Type} interface value
     * (GoInterface over an rtype struct). The itab is left null and built
     * lazily by the dispatch path from the descriptor name — exactly like
     * {@code InterfaceBoxNode}; the dispatch path never reads a stored
     * itab. (An earlier version eagerly called itabFor with a captured
     * {@code reflect.Type} interface descriptor, but that descriptor is
     * only built when the frontend references the named interface id, so
     * the capture was null and every boxType crashed.)
     */
    public static graalgo.values.GoInterface boxType(long tok) {
        graalgo.values.GoStruct rt = new graalgo.values.GoStruct(
                rtypeDescStruct, new long[0], new Object[]{tok});
        return new graalgo.values.GoInterface(rtypeDesc, rt, null);
    }
}
