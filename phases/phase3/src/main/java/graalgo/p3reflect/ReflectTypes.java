package graalgo.p3reflect;

import graalgo.frontend.FrontendException;
import graalgo.frontend.Json;
import graalgo.frontend.TypeTable;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
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

    /** Installs the per-run table from the wire document. */
    public static void install(String jsonText, Map<Integer, GoFunc> functions) {
        Json.Value v = Json.parse(jsonText);
        if (!(v instanceof Json.Object doc)) {
            throw new FrontendException("reflect: document must be a JSON object");
        }
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
        // Pass 2: resolve references and canonicalize.
        for (int id = 0; id < n; id++) {
            resolve(table, types, infos, id, functions, funcdeclSig);
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
            case INT64, UINT64, FLOAT64, COMPLEX64, STRING,
                 UNSAFE_POINTER -> 8;
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
            int id, Map<Integer, GoFunc> functions, Map<Integer, Integer> funcdeclSig) {
        // Phase A: mint tokens for every id.
        int n = types.size();
        for (int i = 0; i < n; i++) {
            String path = "types[" + i + "]";
            Json.Object entry = types.entry(i, path);
            Json.Object d = entry.object("detail", path);
            String kind = entry.string("kind", path);
            String key = canonicalKey(table, types, shells, i, kind, d, functions, funcdeclSig, path);
            if (!table.keyToTok.containsKey(key)) {
                long tok = table.byTok.size();
                table.keyToTok.put(key, tok);
                table.byTok.add(null); // filled in phase B
            }
        }
        // Phase B: build each TypeInfo (depth-first via infoOrShell).
        for (int i = 0; i < n; i++) {
            buildOne(table, types, shells, i, functions, funcdeclSig);
        }
    }

    /**
     * Builds the TypeInfo for one wire id (phase B), unless its canonical
     * token was already built via canonical sharing. Re-entrant: called
     * from infoOrShell for forward references.
     */
    private static void buildOne(Table table, TypeTable types, TypeInfo[] shells,
            int id, Map<Integer, GoFunc> functions, Map<Integer, Integer> funcdeclSig) {
        String key = keyForId(table, types, shells, id, functions, funcdeclSig);
        Long tok = table.keyToTok.get(key);
        if (tok == null) {
            throw new FrontendException("reflect: no token for type id " + id);
        }
        if (table.byTok.get((int) (long) tok) != null) {
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
            table.wireIdToTok.put(id, tok);
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
        buildOne(table, types, shells, id, functions, funcdeclSig);
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
        return canonicalKey(table, types, shells, id, kind, d, functions, funcdeclSig, path);
    }

    /** Canonical structural key for one wire id. */
    private static String canonicalKey(Table table, TypeTable types, TypeInfo[] shells,
            int id, String kind, Json.Object d,
            Map<Integer, GoFunc> functions, Map<Integer, Integer> funcdeclSig, String path) {
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
                            functions, funcdeclSig);
            case "slice" -> "slice:"
                    + keyOf(table, types, shells, (int) d.longInt("elem", path),
                            functions, funcdeclSig);
            case "map" -> "map:"
                    + keyOf(table, types, shells, (int) d.longInt("key", path),
                            functions, funcdeclSig)
                    + ":" + keyOf(table, types, shells, (int) d.longInt("elem", path),
                            functions, funcdeclSig);
            case "pointer" -> "ptr:"
                    + keyOf(table, types, shells, (int) d.longInt("elem", path),
                            functions, funcdeclSig);
            case "chan" -> "chan:" + d.string("dir", path) + ":"
                    + keyOf(table, types, shells, (int) d.longInt("elem", path),
                            functions, funcdeclSig);
            case "struct" -> structKey(table, types, shells, id, d, functions, funcdeclSig, path);
            case "interface" -> interfaceKey(table, types, shells, id, d, functions, funcdeclSig, path);
            case "signature" -> signatureKey(table, types, shells, id, d, functions, funcdeclSig, path);
            // P3-M8 fix: aliases resolve to their RHS for key purposes.
            case "alias" -> keyOf(table, types, shells, (int) d.longInt("rhs", path),
                    functions, funcdeclSig);
            default -> throw new FrontendException(path + ": reflect: key of " + kind);
        };
    }

    private static String keyOf(Table table, TypeTable types, TypeInfo[] shells,
            int id, Map<Integer, GoFunc> functions, Map<Integer, Integer> funcdeclSig) {
        String path = "types[" + id + "]";
        Json.Object entry = types.entry(id, path);
        Json.Object d = entry.object("detail", path);
        String kind = entry.string("kind", path);
        return canonicalKey(table, types, shells, id, kind, d, functions, funcdeclSig, path);
    }

    /** Struct key: ordered field (name, tag, anonymous, type-key) list. */
    private static String structKey(Table table, TypeTable types, TypeInfo[] shells,
            int id, Json.Object d, Map<Integer, GoFunc> functions,
            Map<Integer, Integer> funcdeclSig, String path) {
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
                            functions, funcdeclSig));
        }
        return sb.append('}').toString();
    }

    /** Interface key: ordered method (name, sig-key) list. */
    private static String interfaceKey(Table table, TypeTable types, TypeInfo[] shells,
            int id, Json.Object d, Map<Integer, GoFunc> functions,
            Map<Integer, Integer> funcdeclSig, String path) {
        Json.Array methods = d.array("methods", path);
        StringBuilder sb = new StringBuilder("interface{");
        for (int i = 0; i < methods.size(); i++) {
            Json.Object m = arrObj(methods, i, path);
            if (i > 0) {
                sb.append(';');
            }
            sb.append(m.string("name", path)).append(',')
                    .append(keyOf(table, types, shells, (int) m.longInt("sig", path),
                            functions, funcdeclSig));
        }
        return sb.append('}').toString();
    }

    /** Signature key: params/results type keys + variadic. */
    private static String signatureKey(Table table, TypeTable types, TypeInfo[] shells,
            int id, Json.Object d, Map<Integer, GoFunc> functions,
            Map<Integer, Integer> funcdeclSig, String path) {
        StringBuilder sb = new StringBuilder("func(");
        Json.Array params = d.array("params", path);
        for (int i = 0; i < params.size(); i++) {
            if (i > 0) {
                sb.append(',');
            }
            sb.append(keyOf(table, types, shells,
                    (int) arrObj(params, i, path).longInt("type", path),
                    functions, funcdeclSig));
        }
        sb.append(")(");
        Json.Array results = d.array("results", path);
        for (int i = 0; i < results.size(); i++) {
            if (i > 0) {
                sb.append(',');
            }
            sb.append(keyOf(table, types, shells,
                    (int) arrObj(results, i, path).longInt("type", path),
                    functions, funcdeclSig));
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
                        functions, funcdeclSig);
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
                        functions, funcdeclSig);
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
            Map<Integer, GoFunc> functions, Map<Integer, Integer> funcdeclSig) {
        Json.Value mv = d.opt("methods");
        if (!(mv instanceof Json.Array methods) || methods.size() == 0) {
            return List.of();
        }
        TreeMap<String, MethodInfo> sorted = new TreeMap<>();
        for (int i = 0; i < methods.size(); i++) {
            Json.Object m = arrObj(methods, i, path);
            String name = m.string("name", path);
            int objId = (int) m.longInt("obj", path);
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
                throw new FrontendException(path + ": reflect: method \"" + name
                        + "\" refers to unknown function object id " + objId);
            }
            Integer sigId = funcdeclSig.get(objId);
            if (sigId == null) {
                throw new FrontendException(path + ": reflect: method \"" + name
                        + "\" (obj " + objId + ") has no funcdecl signature");
            }
            TypeInfo funcType = infoOrShell(table, types, shells, sigId, functions, funcdeclSig);
            Json.Value ik = m.opt("implkey");
            String implKey = ik instanceof Json.Str s ? s.value() : name;
            sorted.put(name, new MethodInfo(name, pkgPath, funcType, func,
                    "pointer".equals(recv), sorted.size(), implKey));
        }
        return List.copyOf(sorted.values());
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
            sb.append(f.name).append(' ').append(typeString(f.type.tok));
            if (!f.tag.isEmpty()) {
                sb.append(' ').append(f.tag);
            }
        }
        return sb.append(" }").toString();
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

    /** Structural key of an already-built TypeInfo (for ptrTo). */
    private static String structuralKey(TypeInfo t) {
        // Reuse the canonical key scheme: named types by name, composites
        // by structure. (Only used for ptrTo synthesis.)
        if (t.isNamed()) {
            return "named:" + t.pkgPath + "." + t.name;
        }
        return switch (t.kind) {
            case ARRAY -> "array:" + t.arrayLen + ":" + structuralKey(t.elem);
            case SLICE -> "slice:" + structuralKey(t.elem);
            case MAP -> "map:" + structuralKey(t.key) + ":" + structuralKey(t.elem);
            case POINTER -> "ptr:" + structuralKey(t.elem);
            case CHAN -> "chan:" + t.chanDir + ":" + structuralKey(t.elem);
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
    private static volatile graalgo.values.GoTypeDescriptor typeIfaceDesc;

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
        try {
            typeIfaceDesc = descriptorFor(types, "reflect.Type");
        } catch (FrontendException e) {
            typeIfaceDesc = null;
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
     * (GoInterface over an rtype struct, with itab).
     */
    public static graalgo.values.GoInterface boxType(long tok) {
        graalgo.values.GoStruct rt = new graalgo.values.GoStruct(
                rtypeDescStruct, new long[0], new Object[]{tok});
        Object itab = graalgo.runtime.Intrinsics.itabFor(rtypeDesc, typeIfaceDesc);
        return new graalgo.values.GoInterface(rtypeDesc, rt, itab);
    }
}
