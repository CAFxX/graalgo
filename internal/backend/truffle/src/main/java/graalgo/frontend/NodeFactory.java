package graalgo.frontend;

import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.FrameSlotKind;
import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.source.SourceSection;
import com.oracle.truffle.api.source.Source;
import graalgo.runtime.sync.GoPool;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import java.util.ArrayList;
import java.util.ArrayDeque;
import java.util.Arrays;
import java.util.Collections;
import java.util.Deque;
import java.util.HashMap;
import java.util.HashSet;
import java.util.HexFormat;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Builds Truffle node trees from frontend/v1 JSON documents.
 *
 * <p>Protocol: {@code buildProgram} parses the document, rejects anything
 * whose {@code "frontend"} is not {@code "v1"} (additive-only versioning
 * rule — a breaking wire change bumps the major version and needs a new
 * factory), then recursively builds nodes. Every node gets an accurate
 * {@link com.oracle.truffle.api.source.SourceSection} from the exporter
 * position (line/column are 1-based on both sides).
 *
 * <p>Extension hooks (the WS D/E integration points — see
 * docs/frontend-v1.md §9 for the full list):
 * <ol>
 *   <li>{@link #registerExprKind} — WS D registers new expression kinds
 *       (unary/call/composite/selector/... full coverage).</li>
 *   <li>{@link #registerStmtKind} — WS E registers new statement kinds
 *       (if/for/switch/branch/...).</li>
 *   <li>{@link #registerTypeDecoder} — WS C/D register type-table kinds
 *       beyond the v1 int64 subset (floats, strings, slices, structs...).</li>
 * </ol>
 * Unknown kinds fail with {@link FrontendException} naming the owning
 * workstream — the factory never silently miscompiles.
 *
 * <p>v1 straight-line subset: integer constants, binary/unary arithmetic,
 * variable declaration and assignment, {@code ++}/{@code --}, expression
 * statements, bare {@code return}. All values are unboxed {@code long}
 * (D5 mapping for Go integer kinds). Multi-assignments lower Go-exact
 * two-phase (all right-hand sides before any write) via
 * {@link #assignTwoPhase}; see the package README's "Semantic lowerings".
 */
public final class NodeFactory {
    /**
     * M10 (P1-3): bound for type-chain following loops ({@code named}→
     * {@code underlying}, {@code alias}→{@code rhs}). Real chains are a
     * handful of steps; a cycle must fail fast with FrontendException,
     * never hang.
     */
    private static final int MAX_TYPE_CHAIN_STEPS = 10_000;

    /** Builds an expression node for one registered expression kind. WS D hook. */
    public interface ExprBuilder {
        GoExprNode build(Json.Object node, BuildCtx ctx);
    }

    /** Builds a statement node for one registered statement kind. WS E hook. */
    public interface StmtBuilder {
        GoStmtNode build(Json.Object node, BuildCtx ctx);
    }

    /**
     * Maps a type-table entry to a frame slot kind. Returns {@code null} when
     * this decoder does not handle the entry; the factory tries the next one
     * and fails with {@link FrontendException} if none handles it. WS C/D hook.
     */
    public interface TypeDecoder {
        FrameSlotKind decode(int typeId, Json.Object entry);
    }

    /** Mutable build state threaded through the recursive build. */
    public static final class BuildCtx {
        public final Source source;
        public final FrameDescriptor.Builder frames;
        public final TypeTable types;
        public final Json.Array objects;
        /** object id → frame slot index. */
        public final Map<Integer, Integer> objSlots = new HashMap<>();
        /**
         * variable name → frame slot index. Test/introspection aid;
         * last-wins on shadowing (WS E refines scoping).
         */
        public final Map<String, Integer> slotsByName = new HashMap<>();
        /** variable name → Go kind. Test/introspection aid (M3). */
        public final Map<String, GoKind> kindsByName = new HashMap<>();
        /**
         * Pending Go label for the statement being built (M4, WS E):
         * set by the {@code labeled} builder, consumed by loop/switch
         * builders. Null when no label applies.
         */
        public String pendingLabel;
        /** Canonical struct descriptors per type-table id (M4, WS E). */
        public final Map<Integer, GoStructDescriptor> structDescs = new HashMap<>();
        /** Struct field type ids parallel to descriptor fields (M4, WS E). */
        public final Map<Integer, int[]> structFieldTypeIds = new HashMap<>();
        /** Zero-value templates per struct type-table id (M4, WS E). */
        public final Map<Integer, GoStruct> structZeroTemplates = new HashMap<>();
        /**
         * M10 (P1-3): type ids currently being zero-templated (recursion
         * guard). A cyclic wire graph must fail fast with FrontendException,
         * not overflow the stack.
         */
        public final Set<Integer> zeroTemplateVisiting = new HashSet<>();
        /**
         * P3-M9: struct type ids currently being descriptor-built (recursion
         * guard for array-element-struct resolution in structDescriptorFor).
         * A cyclic type graph must fail fast with FrontendException, not
         * overflow the stack (Go forbids a struct containing an array of
         * itself, so this only trips on illegal input).
         */
        public final Set<Integer> structDescVisiting = new HashSet<>();
        /** Canonical dynamic-type descriptors per type-table id (M4, WS E). */
        public final Map<Integer, graalgo.values.GoTypeDescriptor> typeDescs = new HashMap<>();
        /**
         * P3-M3 (WS D): descriptor name → method names, from the type
         * table's {@code named}/{@code pointer} entries' {@code "methods"}.
         * Memoized per build (the type table is per-document); consumed by
         * type assertions to non-empty interfaces, which check the dynamic
         * type's method set at runtime.
         */
        public Map<String, Set<String>> dynamicMethodSets = null;
        /**
         * Enclosing function's BuildCtx for function literals (M5, WS F):
         * null for {@code func main} and named func decls. Variable
         * resolution walks this chain for captured variables.
         */
        public final BuildCtx parent;
        /**
         * Result slot for function literals with results (M5, WS F): the
         * named result variable's slot, or an allocated slot for unnamed
         * results. -1 when the function has no results. {@link ReturnNode}
         * assigns this slot before throwing {@code GoReturn}; the root node
         * reads it after deferred calls run.
         */
        public int resultSlot = -1;
        /** Kind of the result slot (M5, WS F); null when resultSlot is -1. */
        public GoKind resultKind;
        /**
         * P3-M9 (WS D): the named result's address is taken — its slot
         * holds a {@link graalgo.values.GoBox} cell instead of the raw
         * value (see {@link ReturnNode} / {@code GoFuncRootNode}).
         */
        public boolean resultBoxed = false;

        /** Declared result type-table id (-1 if the function has no result). */
        public int resultTypeId = -1;
        /** Function name for Go stack traces (M5, WS F). */
        public String funcName = "main";
        /**
         * M9 (P1-1): the enclosing funcdecl's object id, for probe site
         * disambiguation. Each monomorphized instantiation is a separate
         * funcdecl (own objId) whose call nodes re-use the origin's source
         * positions; without this, per-instantiation sites merge under one
         * {@code file:line:col} name and polymorphism is overstated.
         * Inherited by funclit child contexts (a closure body belongs to
         * the enclosing instantiation).
         */
        public int funcObjId = -1;
        /**
         * The {@code $closure} frame slot index (M5, WS F): slot 0 in every
         * function-literal frame, -1 for {@code func main} (no closure).
         */
        public int closureSlot = -1;
        /**
         * True if this function literal's body captures variables from an
         * enclosing scope (M5, WS F): set when a CapReadNode/CapWriteNode is
         * created. Used instead of AST scanning (which misses nodes nested
         * in switches).
         */
        public boolean hasCapture = false;
        /**
         * Object ids of per-iteration (boxed, M6) variables: loop-declared
         * variables (for-init/range :=, loop-body vardecls) referenced by a
         * function literal inside the loop, from the factory pre-scan (no
         * exporter change; D1 protocol frozen). A boxed variable's slot
         * holds a {@link graalgo.values.GoBox} cell, refreshed per iteration.
         * Uncaptured variables are never added: the plain-slot fast path is
         * untouched.
         */
        public final Set<Integer> boxed = new HashSet<>();
        /**
         * For-header boxed variables: object id → per-iteration cell
         * slot. The primary slot in {@link #objSlots} is the <em>carry</em>
         * cell (gc's {@code z'}). Init-seed, post, cond, body, and closures
         * all use the per-iteration cell; the carry is only touched by the
         * declaration, the refresh prologue, and the write-back.
         */
        public final Map<Integer, Integer> boxIterSlots = new HashMap<>();
        /**
         * Captured boxed variables of this function literal (loopcap): object
         * id → snapshot slot in this literal's frame. The snapshot cell is
         * installed by {@link GoFuncRootNode} on entry from the
         * {@link graalgo.values.GoFunc}'s snapshot.
         */
        public final Map<Integer, Integer> boxSnapSlots = new HashMap<>();
        /**
         * Snapshot spec triples (depth, slot, snapSlot) for
         * {@link FuncLitNode} (loopcap): collected during the body build as
         * boxed captures are encountered; drained by {@code buildFuncLit}.
         */
        public final List<int[]> boxSpec = new ArrayList<>();
        /**
         * GoKind for each boxed variable (loopcap): populated when the box
         * slots are allocated.
         */
        public final Map<Integer, GoKind> boxKinds = new HashMap<>();

        BuildCtx(Source source, FrameDescriptor.Builder frames, TypeTable types, Json.Array objects) {
            this(source, frames, types, objects, null);
        }

        BuildCtx(Source source, FrameDescriptor.Builder frames, TypeTable types, Json.Array objects,
                 BuildCtx parent) {
            this.source = source;
            this.frames = frames;
            this.types = types;
            this.objects = objects;
            this.parent = parent;
        }
    }

    /** A built program plus introspection data for tests and the launcher. */
    public record Program(GoRootNode root, FrameDescriptor descriptor, Map<String, Integer> slotsByName,
            Map<String, GoKind> kindsByName, Map<Integer, GoFunc> functions) {}

    private final Source source;
    /** One Truffle Source per input file; position "file" indices select
     * among them (frontend/v1 wire format). source is sources[0]. */
    private final Source[] sources;
    private final Map<String, ExprBuilder> exprBuilders = new LinkedHashMap<>();
    private final Map<String, StmtBuilder> stmtBuilders = new LinkedHashMap<>();
    private final List<TypeDecoder> typeDecoders = new ArrayList<>();
    /** The parsed frontend/v1 document from the last buildProgram call.
     * P3-M9: parse-once — downstream installers (IfaceMethodTables,
     * ReflectTypes, LinknameIntrinsics) reuse this instead of re-parsing
     * the JSON text (5x parse of a 200MB document was the gmp1 bottleneck). */
    private Json.Object document;
    /** Shared function-literal counter for unique trace names (M5, WS F). */
    private int funclitCounter;
    /**
     * Loop-phase stack for M6 per-iteration capture. The build is
     * single-threaded, so one factory-level stack suffices: {@code buildFor}
     * pushes (loopId, phase) and pops on exit. Nested loops nest correctly,
     * unlike a single headerMode/postMode boolean. Object ids are
     * program-global, so loop ids are factory-global.
     */
    private final Deque<LoopPhase> loopStack = new ArrayDeque<>();
    /** Next loop id (loopcap). */
    private int nextLoopId;
    /** Boxed for-header var oid → declaring loop id (loopcap). */
    private final Map<Integer, Integer> headerLoopIds = new HashMap<>();
    /**
     * Package-level variables (M7): object id → slot in the program's
     * globals frame. Filled by {@link #buildProgram} before any function
     * body is built; consulted by {@link #readVar} and
     * {@link #classifyLhs} ahead of the per-function frame maps. A global
     * object id can never collide with a frame object id — the exporter's
     * defVar is idempotent per go/types object, so each variable owns
     * exactly one id for the whole program.
     */
    private final Map<Integer, Integer> globalSlots = new HashMap<>();
    /**
     * The program's globals frame (M7): one typed slot per package-level
     * variable, initialized at the top of {@code func main} in dependency
     * order. Null when the program declares no globals.
     */
    private MaterializedFrame globalsFrame;
    /**
     * Object ids of variables whose address is taken anywhere in the
     * program (P3-M9, WS D): every {@code addrof} wire node whose operand
     * is an {@code ident}. Computed once per {@link #buildProgram} by
     * {@link #collectAddrTaken}, before any slot is allocated. Local
     * variables and parameters with an id in this set are boxed (their
     * frame slot holds a {@link graalgo.values.GoBox} cell shared with
     * every {@code &x} pointer); package-level variables get a boxed
     * globals-frame slot. Object ids are program-global, so one set
     * serves every function.
     */
    private final Set<Integer> addrTaken = new HashSet<>();
    /** Loop phases (loopcap). */
    private static final int LP_INIT = 0, LP_COND = 1, LP_POST = 2, LP_BODY = 3;
    /** One entry of {@link #loopStack}. */
    private record LoopPhase(int loopId, int phase) {}
    /**
     * Named-function / instantiation table (M6, WS H): object id →
     * {@link GoFunc}. Filled by {@link #buildProgram} after all function
     * bodies are built; {@link FuncConstNode} resolves lazily at execution
     * time so recursive references work.
     */
    private final Map<Integer, GoFunc> funcTable = new LinkedHashMap<>();

    public NodeFactory(Source source) {
        this(new Source[]{source});
    }

    /** Multi-file constructor: sources[i] backs position file index i. */
    public NodeFactory(Source[] sources) {
        if (sources == null || sources.length == 0) {
            throw new IllegalArgumentException("NodeFactory requires at least one Source");
        }
        this.source = sources[0];
        this.sources = sources;
        registerExprKind("ident", this::buildIdent);
        registerExprKind("basiclit", this::buildBasicLit);
        registerExprKind("binary", this::buildBinary);
        registerExprKind("unary", this::buildUnary);
        registerExprKind("paren", this::buildParen);
        registerExprKind("deref", this::buildDeref); // P3-M9 (WS D): *p read
        registerExprKind("call", this::buildCall);
        registerExprKind("convert", this::buildConvert); // M3 (WS D): Go conversions T(x)
        registerStmtKind("block", this::buildBlock);
        registerStmtKind("vardecl", this::buildVarDecl);
        registerStmtKind("assign", this::buildAssign);
        registerStmtKind("exprstmt", this::buildExprStmt);
        registerStmtKind("return", this::buildReturn);
        registerStmtKind("incdec", this::buildIncDec);
        // M4 (WS E): control flow + composite types.
        registerStmtKind("if", this::buildIf);
        registerStmtKind("for", this::buildFor);
        registerStmtKind("range", this::buildRange);
        registerStmtKind("switch", this::buildSwitch);
        registerStmtKind("typeswitch", this::buildTypeSwitch);
        registerStmtKind("branch", this::buildBranch);
        registerStmtKind("labeled", this::buildLabeled);
        registerExprKind("complit", this::buildComplit);
        registerExprKind("index", this::buildIndex);
        registerExprKind("sliceexpr", this::buildSliceExpr);
        registerExprKind("selector", this::buildSelector);
        registerExprKind("typeassert", this::buildTypeAssert);
        registerExprKind("nil", this::buildNil);
        // M5 (WS F): defer/panic/recover/Goexit + function literals.
        registerStmtKind("defer", this::buildDefer);
        registerExprKind("funclit", this::buildFuncLit);
        // M6 (WS H): the D4 fmt stub via the Phase-1 import overlay.
        registerExprKind("fmtcall", this::buildFmtCall);
        // P3-M9 (WS D): implicit address-of for method receivers. When a
        // pointer-receiver method is called on an addressable value, the
        // exporter wraps the receiver in an "addrof" node (x.M() → (&x).M()).
        registerExprKind("addrof", this::buildAddrOf);
        // P3-M9 (WS H): method values x.M in value position — a GoFunc
        // with the receiver bound (see MethodValueNode).
        registerExprKind("methodvalue", this::buildMethodValue);
        // P3-M9 (unsafe patterns): crypto/internal/fips140/alias.AnyOverlap
        // and InexactOverlap lower to the "sliceoverlap" wire node (see
        // SliceOverlapNode). The real stdlib uses uintptr(unsafe.Pointer(...))
        // which traps under D5; this implements overlap detection directly.
        registerExprKind("sliceoverlap", this::buildSliceOverlap);
        // Default type decoder: the D5 value-model mapping (M3, WS C). Named
        // types are resolved to their underlying type before decoders run
        // (see allocSlot/kindOf); a decoder that returns null abstains and
        // the factory fails naming this hook.
        registerTypeDecoder((id, entry) -> {
            String path = "types[" + id + "]";
            String k = entry.string("kind", path);
            try {
                if ("basic".equals(k)) {
                    Json.Object detail = entry.object("detail", path);
                    return GoKind.fromBasic(detail.string("name", path), detail.string("class", path))
                            .slotKind();
                }
                return GoKind.fromCompositeKind(k).slotKind();
            } catch (IllegalArgumentException e) {
                return null;
            }
        });
    }

    /** WS D hook: register a builder for an expression kind. Duplicate = error. */
    public void registerExprKind(String kind, ExprBuilder builder) {
        if (exprBuilders.putIfAbsent(kind, builder) != null) {
            throw new FrontendException("duplicate expression kind registration: \"" + kind + "\"");
        }
    }

    /** WS E hook: register a builder for a statement kind. Duplicate = error. */
    public void registerStmtKind(String kind, StmtBuilder builder) {
        if (stmtBuilders.putIfAbsent(kind, builder) != null) {
            throw new FrontendException("duplicate statement kind registration: \"" + kind + "\"");
        }
    }

    /** WS C/D hook: register a type-table → slot-kind decoder. */
    public void registerTypeDecoder(TypeDecoder decoder) {
        typeDecoders.add(decoder);
    }

    /**
     * Parses a frontend/v1 document and builds the program.
     *
     * <p>M6 (WS H): the document carries one {@code funcdecl} per top-level
     * function/method plus one per monomorphized instantiation. Each is
     * built with its own frame into its own root node ({@code func main}
     * into a {@link GoRootNode}, the rest into {@link GoFuncRootNode}s
     * registered in the function table); call sites resolve by object id.
     */
    /**
     * Allocates the program's globals frame (M7): one typed slot per
     * package-level variable, in document order. The frame is created
     * eagerly so {@link GlobalReadNode}/{@link GlobalWriteNode} can
     * reference it while function bodies are built; the initialization
     * statements themselves run at the top of {@code func main} (see the
     * isMain branch of {@link #buildProgram}). Every global is explicitly
     * initialized (zero value when the declaration has no initializer),
     * so no frame default-value sentinel is needed.
     */
    private void buildGlobals(Json.Object doc, TypeTable types, Json.Array objects) {
        globalSlots.clear();
        globalsFrame = null;
        Json.Value gv = doc.opt("globals");
        if (!(gv instanceof Json.Array globals) || globals.size() == 0) {
            return;
        }
        FrameDescriptor.Builder gb = FrameDescriptor.newBuilder();
        BuildCtx gctx = new BuildCtx(source, gb, types, objects);
        for (int i = 0; i < globals.size(); i++) {
            String gpath = "globals[" + i + "]";
            Json.Object e = asObject(globals.get(i), gpath);
            int oid = e.intValue("obj", gpath);
            if (oid < 0) {
                continue; // side-effect-only blank initializer: no slot
            }
            Json.Value oe = objects.get(oid);
            if (!(oe instanceof Json.Object objEnt)) {
                throw new FrontendException(gpath + ": object id " + oid + " is not an object");
            }
            String varName = objEnt.string("name", "objects[" + oid + "]");
            int typeId = objEnt.intValue("type", "objects[" + oid + "]");
            // P3-M9 (WS D): an address-taken global lives in a GoBox cell
            // (FrameSlotKind.Object), like a boxed local — every &x shares
            // the cell, so *p writes are visible through the variable and
            // vice versa.
            int slot;
            if (addrTaken.contains(oid)) {
                slot = gb.addSlot(FrameSlotKind.Object, varName, null);
            } else {
                slot = allocGlobalSlot(gctx, typeId, varName, gpath);
            }
            if (globalSlots.put(oid, slot) != null) {
                throw new FrontendException(gpath + ": duplicate global object id " + oid);
            }
        }
        globalsFrame = Truffle.getRuntime().createMaterializedFrame(new Object[0], gb.build());
    }

    /**
     * Allocates one globals-frame slot for a package-level variable (M7).
     * Mirrors {@link #allocSlot}'s type-decoder selection, but records the
     * slot in {@link #globalSlots} instead of a function's frame maps —
     * globals are deliberately absent from {@code slotsByName} (the
     * {@code --dump-vars} channel reports main-frame slots only).
     */
    private int allocGlobalSlot(BuildCtx gctx, int typeId, String varName, String path) {
        int rid = resolveAlias(gctx, typeId, path);
        Json.Object entry = gctx.types.entry(rid, path);
        while ("named".equals(entry.string("kind", path))) {
            rid = entry.object("detail", path).intValue("underlying", path);
            entry = gctx.types.entry(rid, path);
        }
        FrameSlotKind slotKind = null;
        for (TypeDecoder d : typeDecoders) {
            slotKind = d.decode(rid, entry);
            if (slotKind != null) {
                break;
            }
        }
        if (slotKind == null) {
            throw new FrontendException(path + ": no TypeDecoder handles type id " + typeId
                    + " for a package-level variable (M7)");
        }
        return gctx.frames.addSlot(slotKind, varName, null);
    }

    /**
     * Builds the initialization statements for the program's globals (M7):
     * one {@link GlobalWriteNode} per variable in document (dependency)
     * order — the initializer expression, or the zero value when the
     * declaration has none — plus an expression statement for each
     * side-effect-only blank initializer ({@code var _ = ...}).
     */
    private List<GoStmtNode> buildGlobalInits(Json.Object doc, BuildCtx ctx) {
        List<GoStmtNode> out = new ArrayList<>();
        Json.Value gv = doc.opt("globals");
        if (!(gv instanceof Json.Array globals) || globals.size() == 0) {
            return out;
        }
        for (int i = 0; i < globals.size(); i++) {
            String gpath = "globals[" + i + "]";
            Json.Object e = asObject(globals.get(i), gpath);
            int oid = e.intValue("obj", gpath);
            Json.Value initV = e.opt("init");
            if (oid < 0) {
                // Side-effect-only blank initializer.
                if (initV instanceof Json.Object initObj) {
                    out.add(new ExprStmtNode(buildExpr(initObj, ctx)));
                }
                continue;
            }
            Integer slot = globalSlots.get(oid);
            if (slot == null) {
                throw new FrontendException(gpath + ": no globals-frame slot for object id " + oid);
            }
            int typeId = objTypeId(ctx, oid, gpath);
            GoKind kind = kindOf(ctx, typeId, gpath);
            GoExprNode init;
            if (initV instanceof Json.Object initObj) {
                init = maybeConvert(buildExpr(initObj, ctx), kind, typeId, ctx, gpath);
            } else {
                init = zeroNode(kind, typeId, ctx, gpath);
            }
            // P3-M9 (WS D): an address-taken global installs a GoBox cell
            // once, at initialization; every later access shares the cell.
            GoStmtNode w = addrTaken.contains(oid)
                    ? new GlobalBoxInitNode(kind, globalsFrame, slot, init)
                    : new GlobalWriteNode(kind, globalsFrame, slot, init);
            w.setSourceSection(sectionFor(e, gpath));
            out.add(w);
        }
        return out;
    }

    /** Returns the parsed frontend/v1 document from the last buildProgram call.
     * P3-M9: parse-once — lets downstream installers reuse the DOM instead
     * of re-parsing the JSON text. */
    public Json.Object document() {
        if (document == null) {
            throw new FrontendException("document: buildProgram has not been called");
        }
        return document;
    }

    public Program buildProgram(String jsonText) {
        Json.Value v = Json.parse(jsonText);
        if (!(v instanceof Json.Object doc)) {
            throw new FrontendException("document: must be a JSON object");
        }
        this.document = doc;
        String fv = doc.string("frontend", "document");
        if (!"v1".equals(fv)) {
            throw new FrontendException("unsupported frontend version \"" + fv
                    + "\": this factory implements frontend/v1 only"
                    + " (additive-only rule: breaking wire changes bump the major version)");
        }
        TypeTable types = new TypeTable(doc.array("types", "document"));
        Json.Array objects = doc.array("objects", "document");
        Json.Array funcs = doc.array("funcs", "document");

        // M7: allocate the globals frame before any function body is built
        // (bodies may reference globals; initializers run at the top of
        // func main in dependency order).
        // P3-M9 (WS D): collect address-taken variables first — an
        // address-taken global gets a boxed (GoBox) globals-frame slot.
        addrTaken.clear();
        for (Json.Value fv2 : funcs.items()) {
            if (fv2 instanceof Json.Object fn2 && "funcdecl".equals(fn2.string("kind", "funcs"))) {
                collectAddrTaken(fn2.object("fields", "funcdecl").opt("body"),
                        addrTaken, "funcdecl.fields.body");
            }
        }
        Json.Value gv0 = doc.opt("globals");
        if (gv0 instanceof Json.Array garr) {
            for (Json.Value ge : garr.items()) {
                if (ge instanceof Json.Object geo) {
                    collectAddrTaken(geo.opt("init"), addrTaken, "globals.init");
                }
            }
        }
        buildGlobals(doc, types, objects);

        GoRootNode mainRoot = null;
        FrameDescriptor mainDescriptor = null;
        Map<String, Integer> mainSlots = null;
        Map<String, GoKind> mainKinds = null;
        for (int i = 0; i < funcs.size(); i++) {
            Json.Value f = funcs.get(i);
            if (!(f instanceof Json.Object fn) || !"funcdecl".equals(fn.string("kind", "funcs[" + i + "]"))) {
                throw new FrontendException("funcs[" + i + "]: must be a funcdecl node");
            }
            Json.Object fields = fn.object("fields", "funcdecl");
            String name = fields.string("name", "funcdecl.fields");
            int objId = fields.intValue("obj", "funcdecl.fields");
            boolean isMain = "main".equals(name) && !fields.has("instof");
            BuildCtx ctx = new BuildCtx(
                    source, FrameDescriptor.newBuilder().defaultValueIllegal(), types, objects);
            ctx.funcObjId = objId;
            if (isMain) {
                if (mainRoot != null) {
                    throw new FrontendException("document has more than one func main");
                }
                // Parameters: none in the v1 scope, but allocate slots generically so
                // WS D's fuller signatures slot straight in.
                for (Json.Value p : fields.array("params", "funcdecl.fields").items()) {
                    if (!(p instanceof Json.Object param) || !"param".equals(param.string("kind", "param"))) {
                        throw new FrontendException("funcdecl params: must be param nodes");
                    }
                    Json.Object pf = param.object("fields", "param");
                    allocSlot(pf.string("name", "param.fields"), pf.intValue("obj", "param.fields"),
                            pf.intValue("type", "param.fields"), "param.fields", ctx);
                }
                prescanAddrTakenDecls(fields.object("body", "funcdecl.fields"), ctx,
                        "funcdecl.fields");
                GoStmtNode body = buildStmt(fields.object("body", "funcdecl.fields"), ctx);
                // M7: package-level variable initialization runs before
                // main's first statement, in dependency order (Go spec,
                // Package initialization).
                List<GoStmtNode> inits = buildGlobalInits(doc, ctx);
                if (!inits.isEmpty()) {
                    List<GoStmtNode> seq = new ArrayList<>(inits.size() + 1);
                    seq.addAll(inits);
                    seq.add(body);
                    body = new BlockNode(seq.toArray(new GoStmtNode[0]));
                }
                mainDescriptor = ctx.frames.build();
                mainRoot = new GoRootNode(mainDescriptor, body);
                mainRoot.setSourceSection(sectionFor(fn, "funcdecl"));
                mainSlots = Map.copyOf(ctx.slotsByName);
                mainKinds = Map.copyOf(ctx.kindsByName);
            } else {
                // P3-M4 (WS C, ADR-0004): a funcdecl carrying "linkname"
                // (exporter-validated //go:linkname on a bodyless function)
                // builds a LinknameRootNode dispatching by symbol name
                // instead of a Go body. Old consumers fail fast on the
                // missing "body" field (additive wire data, freeze §2.3).
                //
                // A funcdecl carrying "unimplemented" (bodyless function
                // without //go:linkname, typically an assembly
                // implementation) builds an UnimplementedRootNode that
                // panics if called. This is general: it applies uniformly
                // to all bodyless functions, preserving type identity
                // while failing loudly on invocation.
                Json.Value linkname = fields.opt("linkname");
                Json.Value unimplemented = fields.opt("unimplemented");
                RootCallTarget target;
                if (linkname instanceof Json.Str sym) {
                    // P3-M4: the declared result type decides whether the
                    // D5 dispatch result is boxed into a GoInterface
                    // (interface result, like a Go-bodied function).
                    boolean boxResult = false;
                    Json.Value results = fields.opt("results");
                    if (results instanceof Json.Array rarr && rarr.size() > 0
                            && rarr.get(0) instanceof Json.Object r0) {
                        Json.Object rf = r0.object("fields", "funcdecl.fields.results[0]");
                        String rp = "funcdecl.fields.results[0].fields";
                        if (rf.opt("type") instanceof Json.Num) {
                            int resultTid = rf.intValue("type", rp);
                            boxResult = kindOf(ctx, resultTid, rp) == GoKind.INTERFACE;
                        }
                    }
                    LinknameRootNode root = new LinknameRootNode(sym.value(), name, boxResult);
                    root.setSourceSection(sectionFor(fn, "funcdecl"));
                    target = root.getCallTarget();
                } else if (unimplemented instanceof Json.Bool b && b.value()) {
                    // Bodyless function without //go:linkname: build a
                    // stub that panics if called. The pkgpath field
                    // identifies the declaring package for the message.
                    String pkgPath = fields.opt("pkgpath") instanceof Json.Str ps
                            ? ps.value() : "";
                    UnimplementedRootNode root = new UnimplementedRootNode(name, pkgPath);
                    root.setSourceSection(sectionFor(fn, "funcdecl"));
                    target = root.getCallTarget();
                } else {
                    GoFuncRootNode root = buildNamedFunc(fn, fields, ctx);
                    target = root.getCallTarget();
                }
                if (funcTable.put(objId, new GoFunc(target, null, null)) != null) {
                    throw new FrontendException("duplicate function object id " + objId
                            + " for " + name + " (WS H)");
                }
            }
        }
        if (mainRoot == null) {
            throw new FrontendException("document has no func main");
        }
        return new Program(mainRoot, mainDescriptor, mainSlots, mainKinds, Map.copyOf(funcTable));
    }

    /**
     * Builds a named top-level function or method (M6, WS H) into a
     * {@link GoFuncRootNode}.
     *
     * <p>Each function gets a fresh top-level {@link BuildCtx} (no parent:
     * named functions cannot capture enclosing scopes) and its own frame;
     * slot 0 is the {@code $closure} slot (always null — the
     * {@link GoFuncRootNode} calling convention installs it from the
     * {@link GoFunc} value). Parameters and the single result become frame
     * slots; the result slot is zeroed on entry (Go semantics). Function
     * literals inside may capture this function's variables via the usual
     * parent chain.
     */
    private GoFuncRootNode buildNamedFunc(Json.Object fn, Json.Object f, BuildCtx ctx) {
        String path = "funcdecl.fields";
        String funcName = f.string("name", path);
        ctx.funcName = funcName;
        int closureSlot = ctx.frames.addSlot(FrameSlotKind.Object, "$closure", null);
        ctx.closureSlot = closureSlot;
        Json.Array params = f.array("params", path);
        int[] paramSlots = new int[params.size()];
        GoKind[] paramKinds = new GoKind[params.size()];
        List<GoStmtNode> prologue = new ArrayList<>();
        for (int i = 0; i < params.size(); i++) {
            Json.Object p = asObject(params.get(i), path + ".params[" + i + "]");
            if (!"param".equals(p.string("kind", path + ".params[" + i + "]"))) {
                throw new FrontendException(path + ".params[" + i + "]: must be a param node");
            }
            Json.Object pf = p.object("fields", path + ".params[" + i + "]");
            String pname = pf.string("name", path);
            int oid = pf.intValue("obj", path);
            int typeId = pf.intValue("type", path);
            GoKind kind = kindOf(ctx, typeId, path);
            int slot = allocParamSlot(pname, oid, typeId, path + ".params[" + i + "]", ctx,
                    prologue);
            paramSlots[i] = slot;
            paramKinds[i] = kind;
        }
        // Results: at most one in v1 (multi-value returns are WS D).
        int resultSlot = -1;
        GoKind resultKind = null;
        int resultTypeId = -1;
        Json.Value resultsV = f.opt("results");
        if (resultsV instanceof Json.Array results && results.size() > 0) {
            if (results.size() > 1) {
                throw new FrontendException(path + ": function with multiple results"
                        + " is not supported in v1 (WS D)");
            }
            Json.Object r = asObject(results.get(0), path + ".results[0]");
            Json.Object rf = r.object("fields", path + ".results[0]");
            String rname = rf.string("name", path);
            int roid = rf.intValue("obj", path);
            resultTypeId = rf.intValue("type", path);
            resultKind = kindOf(ctx, resultTypeId, path);
            // P3-M9 (WS D): an address-taken named result lives in a
            // GoBox cell in the result slot.
            if (addrTaken.contains(roid)) {
                resultSlot = allocBoxSlot(rname.isEmpty() ? "result" : rname, roid,
                        resultTypeId, path + ".results[0]", ctx);
                ctx.resultBoxed = true;
            } else {
                resultSlot = allocSlot(rname.isEmpty() ? "result" : rname, roid, resultTypeId,
                        path + ".results[0]", ctx);
            }
            ctx.resultSlot = resultSlot;
            ctx.resultKind = resultKind;
            ctx.resultTypeId = resultTypeId;
        }
        prescanAddrTakenDecls(f.object("body", path), ctx, path);
        GoStmtNode body = buildStmt(f.object("body", path), ctx);
        // Entry: box address-taken parameters, then zero the result slot
        // (Go semantics: results start at zero values, so a bare `return`
        // yields zeros; a boxed result's zero goes into its cell).
        List<GoStmtNode> entryStmts = new ArrayList<>(prologue);
        if (resultSlot >= 0) {
            GoExprNode zeroVal = zeroNode(resultKind, resultTypeId, ctx, path);
            entryStmts.add(ctx.resultBoxed
                    ? new BoxDeclNode(resultSlot, resultKind, zeroVal)
                    : new AssignNode(resultSlot, resultKind, zeroVal));
        }
        entryStmts.add(body);
        GoStmtNode entry = entryStmts.size() == 1 ? body
                : new BlockNode(entryStmts.toArray(new GoStmtNode[0]));
        GoFuncRootNode root = new GoFuncRootNode(ctx.frames.build(), entry, closureSlot,
                paramSlots, paramKinds, resultSlot, resultKind, funcName, null,
                ctx.resultBoxed);
        root.setSourceSection(sectionFor(fn, "funcdecl"));
        return root;
    }

    // ------------------------------------------------------------------
    // Recursive build.
    // ------------------------------------------------------------------

    public GoExprNode buildExpr(Json.Object node, BuildCtx ctx) {
        String kind = node.string("kind", "node");
        ExprBuilder b = exprBuilders.get(kind);
        if (b == null) {
            throw new FrontendException("unsupported expression kind \"" + kind + "\""
                    + " (WS D hook: NodeFactory.registerExprKind)");
        }
        GoExprNode n = b.build(node, ctx);
        setSection(n, node, kind);
        // Stamp the static type-table id (M4, WS E): interface boxing and
        // dynamic-type descriptors need it. Every expression kind on the
        // wire carries fields.type (possibly null for calls).
        Json.Value tv = node.object("fields", kind).opt("type");
        if (tv instanceof Json.Num num) {
            n.setTypeId((int) num.asLong(kind + ".fields.type"));
        }
        return n;
    }

    public GoStmtNode buildStmt(Json.Object node, BuildCtx ctx) {
        String kind = node.string("kind", "node");
        StmtBuilder b = stmtBuilders.get(kind);
        if (b == null) {
            throw new FrontendException("unsupported statement kind \"" + kind + "\""
                    + " (WS E hook: NodeFactory.registerStmtKind)");
        }
        GoStmtNode n = b.build(node, ctx);
        setSection(n, node, kind);
        return n;
    }

    private SourceSection sectionFor(Json.Object node, String kind) {
        Json.Object pos = node.object("pos", kind);
        String p = kind + ".pos";
        int fileIdx = pos.intValue("file", p);
        int line = pos.intValue("line", p);
        int col = pos.intValue("col", p);
        int len = pos.intValue("len", p);
        // Multi-file: the position's file index selects the Source. Clamp
        // defensively; synthetic nodes carry file 0.
        Source src = source;
        if (fileIdx >= 0 && fileIdx < sources.length) {
            src = sources[fileIdx];
        }
        try {
            return src.createSection(line, col, len);
        } catch (IllegalArgumentException outOfRange) {
            // P3-M5 (WS E; ADR-0005 §D5 documented exception): synthesized
            // nodes (e.g. from the synthetic time/context packages) may
            // carry positions past the end of the source text, which
            // Truffle's TextMap rejects with "line out of bounds". Clamp
            // to a valid section — zero-length at column 1 of the nearest
            // valid line — instead of failing the build. In-range
            // positions take the identical path as before (additive,
            // behavior-preserving).
            int lineCount = Math.max(src.getLineCount(), 1);
            int clamped = Math.min(Math.max(line, 1), lineCount);
            return src.createSection(clamped, 1, 0);
        }
    }

    private void setSection(GoExprNode n, Json.Object node, String kind) {
        n.setSourceSection(sectionFor(node, kind));
    }

    private void setSection(GoStmtNode n, Json.Object node, String kind) {
        n.setSourceSection(sectionFor(node, kind));
    }

    private static Json.Object fieldsOf(Json.Object node, String kind) {
        return node.object("fields", kind);
    }

    // ------------------------------------------------------------------
    // Expressions.
    // ------------------------------------------------------------------

    /**
     * Maps a type-table id to its {@link GoKind}, resolving named types to
     * their underlying type and aliases to their right-hand side. Unknown
     * kinds fail naming the WS C/D {@code registerTypeDecoder} hook.
     */
    public static GoKind kindOf(BuildCtx ctx, int typeId, String path) {
        Json.Object entry = ctx.types.entry(typeId, path);
        // M10 (P1-3): bounded chain walk; a cycle fails fast, never hangs.
        for (int steps = 0; ; steps++) {
            if (steps > MAX_TYPE_CHAIN_STEPS) {
                throw new FrontendException(path + ": type chain exceeds "
                        + MAX_TYPE_CHAIN_STEPS + " steps (cyclic type graph?) (WS E)");
            }
            String ek = entry.string("kind", path);
            if ("named".equals(ek)) {
                typeId = entry.object("detail", path).intValue("underlying", path);
                entry = ctx.types.entry(typeId, path);
            } else if ("alias".equals(ek)) {
                // An alias *is* its right-hand side (Go spec).
                typeId = entry.object("detail", path).intValue("rhs", path);
                entry = ctx.types.entry(typeId, path);
            } else {
                break;
            }
        }
        String k = entry.string("kind", path);
        try {
            if ("basic".equals(k)) {
                Json.Object detail = entry.object("detail", path);
                return GoKind.fromBasic(detail.string("name", path), detail.string("class", path));
            }
            return GoKind.fromCompositeKind(k);
        } catch (IllegalArgumentException e) {
            throw new FrontendException(path + ": " + e.getMessage()
                    + " (WS C/D hook: NodeFactory.registerTypeDecoder)");
        }
    }

    /**
     * Resolves one "named"/"alias" step, returning the (possibly same)
     * type-table id. Used where the *declared* type matters (dynamic-type
     * descriptors keep named types distinct).
     */
    private static int resolveAlias(BuildCtx ctx, int typeId, String path) {
        return resolveAlias(ctx.types, typeId, path);
    }

    /**
     * Returns [arrayLen, elemTypeId] for array types, [-1, -1] otherwise.
     * Used to pre-allocate array fields in GoStruct.zero (P3-M8b).
     */
    private static int[] arrayInfo(BuildCtx ctx, int typeId, String path) {
        Json.Object entry = ctx.types.entry(typeId, path);
        for (int steps = 0; ; steps++) {
            if (steps > MAX_TYPE_CHAIN_STEPS) {
                return new int[]{-1, -1};
            }
            String ek = entry.string("kind", path);
            if ("named".equals(ek)) {
                typeId = entry.object("detail", path).intValue("underlying", path);
                entry = ctx.types.entry(typeId, path);
            } else if ("alias".equals(ek)) {
                typeId = entry.object("detail", path).intValue("rhs", path);
                entry = ctx.types.entry(typeId, path);
            } else {
                break;
            }
        }
        if (!"array".equals(entry.string("kind", path))) {
            return new int[]{-1, -1};
        }
        Json.Object detail = entry.object("detail", path);
        int len = detail.intValue("len", path);
        int elemId = detail.intValue("elem", path);
        return new int[]{len, elemId};
    }

    /**
     * ABI size in bytes of an array type (P3 bug-042): {@code len * elemSize},
     * recursing for nested arrays. Used by append's growslice math — arrays
     * are NOT zero-size types, so growslice must not take its zero-size fast
     * path (which skips copying old elements) for them.
     */
    private static long arraySizeBytes(BuildCtx ctx, int typeId, String path) {
        int[] info = arrayInfo(ctx, typeId, path);
        if (info[0] < 0) {
            throw new FrontendException(path + ": arraySizeBytes on non-array type");
        }
        int len = info[0];
        int elemId = info[1];
        GoKind elemKind = kindOf(ctx, elemId, path);
        long elemSize;
        if (elemKind == GoKind.ARRAY) {
            elemSize = arraySizeBytes(ctx, elemId, path);
        } else if (elemKind == GoKind.STRUCT) {
            elemSize = graalgo.runtime.Intrinsics.structSizeBytes(
                    structDescriptorFor(ctx, elemId, path));
        } else {
            elemSize = graalgo.runtime.Intrinsics.elemSizeBytes(elemKind);
            if (elemSize == 0) {
                // Slice/map/interface/string/pointer kinds: use Go ABI sizes.
                // (elemSizeBytes returns 0 for these by design.)
                elemSize = switch (elemKind) {
                    case SLICE -> 24;
                    case STRING -> 16;
                    case MAP, POINTER, FUNC, CHAN, UNSAFE_POINTER, INTERFACE -> 8;
                    default -> throw new FrontendException(path
                            + ": arraySizeBytes: unsupported element kind " + elemKind);
                };
            }
        }
        return (long) len * elemSize;
    }

    /**
     * Alias resolution over a bare type table (P3-M7R2): overload for
     * naming helpers that don't have a {@link BuildCtx}. Public for the
     * phase-3 {@code DescriptorNames} replica.
     */
    public static int resolveAlias(TypeTable types, int typeId, String path) {
        Json.Object entry = types.entry(typeId, path);
        // M10 (P1-3): bounded chain walk; a cycle fails fast, never hangs.
        for (int steps = 0; ; steps++) {
            if (steps > MAX_TYPE_CHAIN_STEPS) {
                throw new FrontendException(path + ": alias chain exceeds "
                        + MAX_TYPE_CHAIN_STEPS + " steps (cyclic type graph?) (WS E)");
            }
            if (!"alias".equals(entry.string("kind", path))) {
                return typeId;
            }
            typeId = entry.object("detail", path).intValue("rhs", path);
            entry = types.entry(typeId, path);
        }
    }

    /**
     * Canonical {@link GoStructDescriptor} for a struct type-table id
     * (M4, WS E). Descriptor identity is type identity: two values of the
     * same struct type always share the instance (map keys and struct
     * equality hash by it).
     *
     * <p>P3-M8b: public for phase-3 replicas (ChanWire) — the canonical
     * instance must be shared, not re-minted.
     */
    public static GoStructDescriptor structDescriptorFor(BuildCtx ctx, int typeId, String path) {
        typeId = resolveAlias(ctx, typeId, path);
        GoStructDescriptor d = ctx.structDescs.get(typeId);
        if (d != null) {
            return d;
        }
        Json.Object entry = ctx.types.entry(typeId, path);
        // P3-M8b: capture the qualified name before the named→underlying
        // walk, for the canonical cache key below.
        String qname = "named".equals(entry.string("kind", path))
                ? entry.object("detail", path).string("name", path)
                : null;
        // M10 (P1-3): bounded chain walk; a cycle fails fast, never hangs.
        for (int steps = 0; ; steps++) {
            if (steps > MAX_TYPE_CHAIN_STEPS) {
                throw new FrontendException(path + ": named-type chain exceeds "
                        + MAX_TYPE_CHAIN_STEPS + " steps (cyclic type graph?) (WS E)");
            }
            if (!"named".equals(entry.string("kind", path))) {
                break;
            }
            typeId = entry.object("detail", path).intValue("underlying", path);
            entry = ctx.types.entry(typeId, path);
        }
        if (!"struct".equals(entry.string("kind", path))) {
            throw new FrontendException(path + ": not a struct type");
        }
        // P3-M9: recursion guard for the array-element-struct resolution
        // below (a struct containing an array of itself is illegal Go, but
        // fail fast rather than overflowing the stack).
        if (!ctx.structDescVisiting.add(typeId)) {
            throw new FrontendException(path + ": cyclic struct type graph (WS E)");
        }
        try {
            return structDescriptorForInner(ctx, typeId, qname, entry, path);
        } finally {
            ctx.structDescVisiting.remove(typeId);
        }
    }

    /**
     * Body of {@link #structDescriptorFor} once the type id is resolved to a
     * struct entry and the recursion guard is held.
     */
    private static GoStructDescriptor structDescriptorForInner(
            BuildCtx ctx, int typeId, String qname, Json.Object entry, String path) {
        Json.Array fields = entry.object("detail", path).array("fields", path);
        List<String> names = new ArrayList<>(fields.size());
        List<GoKind> kinds = new ArrayList<>(fields.size());
        List<Integer> arrayLens = new ArrayList<>(fields.size());
        List<graalgo.values.GoKind> arrayElemKinds = new ArrayList<>(fields.size());
        List<GoStructDescriptor> arrayElemDescs = new ArrayList<>(fields.size());
        int[] fieldTypeIds = new int[fields.size()];
        for (int i = 0; i < fields.size(); i++) {
            Json.Value fv = fields.get(i);
            if (!(fv instanceof Json.Object fo)) {
                throw new FrontendException(path + ": struct field " + i + " is not an object");
            }
            names.add(fo.string("name", path));
            int ftid = fo.intValue("type", path);
            fieldTypeIds[i] = ftid;
            kinds.add(kindOf(ctx, ftid, path));
            // P3-M8b: array length + element kind for GoStruct.zero pre-allocation.
            int[] arrInfo = arrayInfo(ctx, ftid, path);
            arrayLens.add(arrInfo[0]);
            GoKind elemKind = arrInfo[0] >= 0 ? kindOf(ctx, arrInfo[1], path) : null;
            arrayElemKinds.add(elemKind);
            // P3-M9: element struct descriptor for ARRAY-of-STRUCT fields, so
            // GoStruct.zero can zero-fill struct array elements (same root
            // cause as the make([]T,n) slice fix in Intrinsics). Recursion
            // terminates on legal Go (no struct contains an array of
            // itself); the visiting guard above fails fast otherwise.
            arrayElemDescs.add(elemKind == GoKind.STRUCT
                    ? structDescriptorFor(ctx, arrInfo[1], path) : null);
        }
        // P3-M8b: canonicalize process-wide so descriptor identity == type
        // identity across BuildCtxs (functions/packages). The per-BuildCtx
        // cache minted a fresh instance per function, so struct ==
        // (GoStruct.equals is descriptor-identity) failed across functions —
        // e.g. reflect rtype values built in the synthetic package never
        // equaled rtype values built in user code (pt.Elem() == t). The key
        // carries the qualified name (when named) plus the full field
        // layout, so same-named/different-layout types across runs never
        // collide; identical anonymous structs correctly share (Go: identical
        // anonymous struct types are identical).
        // P3-M9: the key also carries array element kinds and the structural
        // keys of array element struct descriptors — otherwise two anonymous
        // structs differing only in their array element struct type would
        // share a descriptor and GoStruct.zero would fill the wrong element
        // type. Element keys are structural (not type-id-based) so identical
        // anonymous structs still share across BuildCtxs.
        String key = (qname != null ? "N:" + qname : "S:") + "|" + names + "|" + kinds + "|" + arrayLens
                + "|" + arrayElemKinds + "|" + elemDescKeys(arrayElemDescs);
        GoStructDescriptor cd = STRUCT_DESCS.get(key);
        if (cd == null) {
            // P0-fmt: use the qualified name for named structs so %#v renders
            // main.Inner not struct#N; anonymous structs keep the struct#id
            // (rendered structurally via GoTypeStrings).
            GoStructDescriptor nd = GoStructDescriptor.layout(
                    qname != null ? qname : "struct#" + typeId, names, kinds, arrayLens,
                    arrayElemKinds, arrayElemDescs);
            GoStructDescriptor prev = STRUCT_DESCS.putIfAbsent(key, nd);
            cd = prev != null ? prev : nd;
        }
        ctx.structDescs.put(typeId, cd);
        ctx.structFieldTypeIds.put(typeId, fieldTypeIds);
        return cd;
    }

    /**
     * Structural keys for array element struct descriptors (P3-M9): "-"
     * for absent, otherwise the recursive layout key. Structural (not
     * type-id-based) so identical anonymous structs keep sharing
     * descriptors across BuildCtxs.
     */
    private static String elemDescKeys(List<GoStructDescriptor> descs) {
        StringBuilder sb = new StringBuilder("[");
        for (GoStructDescriptor d : descs) {
            sb.append(d == null ? "-" : structLayoutKey(d)).append(',');
        }
        return sb.append(']').toString();
    }

    /** Recursive structural layout key for a struct descriptor (P3-M9). */
    private static String structLayoutKey(GoStructDescriptor d) {
        StringBuilder sb = new StringBuilder("{");
        for (GoStructDescriptor.Field f : d.fields()) {
            sb.append(f.name()).append(':').append(f.kind());
            if (f.kind() == GoKind.ARRAY) {
                sb.append('[').append(f.arrayLen()).append(']').append(f.arrayElemKind());
                if (f.arrayElemDesc() != null) {
                    sb.append(structLayoutKey(f.arrayElemDesc()));
                }
            }
            sb.append(';');
        }
        return sb.append('}').toString();
    }

    /**
     * Field type-table ids parallel to a struct descriptor's fields (M4,
     * WS E): resolves aliases and named types exactly like
     * {@link #structDescriptorFor}.
     */
    static int[] structFieldTypeIds(BuildCtx ctx, int typeId, String path) {
        structDescriptorFor(ctx, typeId, path); // ensures the cache is populated
        typeId = resolveAlias(ctx, typeId, path);
        Json.Object entry = ctx.types.entry(typeId, path);
        // M10 (P1-3): bounded chain walk; a cycle fails fast, never hangs.
        for (int steps = 0; ; steps++) {
            if (steps > MAX_TYPE_CHAIN_STEPS) {
                throw new FrontendException(path + ": named-type chain exceeds "
                        + MAX_TYPE_CHAIN_STEPS + " steps (cyclic type graph?) (WS E)");
            }
            if (!"named".equals(entry.string("kind", path))) {
                break;
            }
            typeId = entry.object("detail", path).intValue("underlying", path);
            entry = ctx.types.entry(typeId, path);
        }
        int[] ftids = ctx.structFieldTypeIds.get(typeId);
        if (ftids == null) {
            throw new FrontendException(path + ": missing struct field type ids (WS E)");
        }
        return ftids;
    }

    /**
     * Canonical dynamic-type descriptor for a type-table id (M4, WS E):
     * one instance per basic {@link GoKind}, per named type, and per
     * structural shape of every other composite type (aliases resolve to
     * their rhs).
     *
     * <p>Structural canonicalization (M6, WS H): the exporter mints one
     * type-table id per <em>syntactic occurrence</em> of an anonymous type,
     * and every function body gets a fresh {@link BuildCtx} (hence a fresh
     * {@code typeDescs} map) — so without this, {@code case struct{a int}}
     * in one function could never match a {@code struct{a int}} value
     * boxed in another (the descriptors were never identical). Unnamed
     * struct/array/slice/map/pointer types now canonicalize by structure
     * in a process-wide cache. Named types stay per-ctx by qualified name
     * (a named type and its underlying type are different dynamic types);
     * sharing an instance across programs is benign — a descriptor is an
     * immutable identity token and dynamic-type universes never mix.
     */
    static graalgo.values.GoTypeDescriptor typeDescriptorFor(BuildCtx ctx, int typeId, String path) {
        typeId = resolveAlias(ctx, typeId, path);
        // bug-019: comparability is a property of the type; compute once
        // from the type table so interface == can panic like gc.
        boolean comparable = isComparableType(ctx, typeId, path);
        Json.Object entry = ctx.types.entry(typeId, path);
        String k = entry.string("kind", path);
        if ("basic".equals(k)) {
            // P3-M7: basic types are named by their Go name ("int" vs
            // "int64"), not just kind — %T must distinguish them, and
            // type switches must not conflate them. Canonicalized by
            // name in a process-wide cache (see basicDescriptorForName).
            String name = entry.object("detail", path).string("name", path);
            name = canonicalBasicName(name);
            graalgo.values.GoTypeDescriptor bd = basicDescriptorForName(name, kindForBasicName(name));
            recordDescriptorWireId(bd, typeId);
            return bd;
        }
        // P3-M7: a pointer to a named type is named "*"+qualifiedName
        // (like gc's %T), not structural. Two distinct named types with
        // identical structure must not share a descriptor, and
        // *errors.errorString must be recognizable by name (Fmt's
        // errorString stand-in check). Pointers to unnamed types stay
        // structural.
        if ("pointer".equals(k)) {
            int elemId = resolveAlias(ctx, entry.object("detail", path).intValue("elem", path), path);
            Json.Object elemEntry = ctx.types.entry(elemId, path);
            if ("named".equals(elemEntry.string("kind", path))) {
                String name = "*" + elemEntry.object("detail", path).string("name", path);
                graalgo.values.GoTypeDescriptor nd = NAMED_DESCS.get(name);
                if (nd == null) {
                    nd = new graalgo.values.GoTypeDescriptor(name,
                            kindOf(ctx, typeId, path), false, comparable);
                    graalgo.values.GoTypeDescriptor prev = NAMED_DESCS.putIfAbsent(name, nd);
                    if (prev != null) {
                        nd = prev;
                    }
                }
                ctx.typeDescs.put(typeId, nd);
                recordDescriptorWireId(nd, typeId);
                return nd;
            }
        }
        if (isStructuralComposite(k)) {
            String key = structuralKey(ctx, typeId, path);
            graalgo.values.GoTypeDescriptor d = STRUCTURAL_DESCS.get(key);
            if (d == null) {
                // P3-M7: use a Go-syntax type name for the descriptor (used
                // by %T) instead of the opaque k#hash — e.g. "func()",
                // "chan int". Falls back to k#hash if the name can't be built.
                String dispName;
                try {
                    dispName = goSyntaxName(ctx.types, typeId, path);
                } catch (FrontendException e) {
                    dispName = k + "#" + Integer.toHexString(key.hashCode());
                }
                d = new graalgo.values.GoTypeDescriptor(dispName,
                        kindOf(ctx, typeId, path), false, comparable);
                graalgo.values.GoTypeDescriptor prev =
                        STRUCTURAL_DESCS.putIfAbsent(key, d);
                if (prev != null) {
                    d = prev;
                }
            }
            recordDescriptorWireId(d, typeId);
            return d;
        }
        graalgo.values.GoTypeDescriptor d = ctx.typeDescs.get(typeId);
        if (d != null) {
            return d;
        }
        String name;
        boolean emptyIface = false;
        if ("named".equals(k)) {
            // M6 (WS H, issue52026): named types canonicalize by qualified
            // name in a process-wide cache — each function gets a fresh
            // BuildCtx (hence a fresh typeDescs), so without this a
            // Some[int] boxed in returnOption[int] could never match a
            // Some[int] asserted in main.
            // P3-M7: the cache key stays the full qualified name (identity),
            // but the descriptor's display name (for %T) uses the package
            // NAME ("json.Number", not "encoding/json.Number"), and
            // encoding/json.RawMessage displays as jsontext.Value (gc 1.27
            // alias).
            String qname = entry.object("detail", path).string("name", path);
            graalgo.values.GoTypeDescriptor nd = NAMED_DESCS.get(qname);
            if (nd == null) {
                nd = new graalgo.values.GoTypeDescriptor(displayNameFor(qname),
                        kindOf(ctx, typeId, path), false, comparable);
                graalgo.values.GoTypeDescriptor prev = NAMED_DESCS.putIfAbsent(qname, nd);
                if (prev != null) {
                    nd = prev;
                }
            }
            ctx.typeDescs.put(typeId, nd);
            recordDescriptorWireId(nd, typeId);
            return nd;
        } else if ("interface".equals(k)) {
            emptyIface = entry.object("detail", path).bool("empty", path);
            name = emptyIface ? "any" : "interface#" + typeId;
        } else {
            name = k + "#" + typeId;
        }
        d = new graalgo.values.GoTypeDescriptor(name, kindOf(ctx, typeId, path), emptyIface, comparable);
        ctx.typeDescs.put(typeId, d);
        recordDescriptorWireId(d, typeId);
        return d;
    }

    /**
     * bug-019: whether values of the given type-table type support ==.
     * Mirrors gc's type comparability (used for the "comparing uncomparable
     * type" interface-== panic): func, slice, and map types are never
     * comparable; structs and arrays are comparable iff all fields/elements
     * are; everything else (basic, pointer, chan, interface) is comparable.
     * Follows named/alias chains like {@link #kindOf}; cycles (impossible in
     * valid Go, but cheap to guard) fail open to comparable.
     */
    static boolean isComparableType(BuildCtx ctx, int typeId, String path) {
        return isComparableType(ctx, typeId, path, new java.util.HashSet<>());
    }

    private static boolean isComparableType(BuildCtx ctx, int typeId, String path,
                                           java.util.Set<Integer> visiting) {
        for (int steps = 0; ; steps++) {
            if (steps > MAX_TYPE_CHAIN_STEPS) {
                throw new FrontendException(path + ": type chain exceeds "
                        + MAX_TYPE_CHAIN_STEPS + " steps (cyclic type graph?) (WS E)");
            }
            Json.Object entry = ctx.types.entry(typeId, path);
            String ek = entry.string("kind", path);
            if ("named".equals(ek)) {
                typeId = entry.object("detail", path).intValue("underlying", path);
            } else if ("alias".equals(ek)) {
                typeId = entry.object("detail", path).intValue("rhs", path);
            } else {
                break;
            }
        }
        if (!visiting.add(typeId)) {
            return true;
        }
        try {
            Json.Object entry = ctx.types.entry(typeId, path);
            String k = entry.string("kind", path);
            Json.Object detail = entry.object("detail", path);
            switch (k) {
                case "basic", "pointer", "chan", "interface":
                    return true;
                case "signature", "slice", "map":
                    return false;
                case "array":
                    return isComparableType(ctx, detail.intValue("elem", path), path, visiting);
                case "struct": {
                    Json.Array fields = detail.array("fields", path);
                    for (int i = 0; i < fields.size(); i++) {
                        Json.Object f = (Json.Object) fields.get(i);
                        if (!isComparableType(ctx, f.intValue("type", path), path, visiting)) {
                            return false;
                        }
                    }
                    return true;
                }
                default:
                    return true;
            }
        } finally {
            visiting.remove(typeId);
        }
    }

    /**
     * P3-M3 remediation (R1 P0-2, ADR-0003): public accessor returning the
     * factory's canonical descriptor for a type-table id — the
     * process-wide {@code NAMED_DESCS}/{@code STRUCTURAL_DESCS} instances
     * for named/structural types (M6, WS H), {@code basicDescriptorFor} for
     * basic kinds. Pure delegation to {@link #typeDescriptorFor}: additive
     * new method, no behavior change to any existing path (the factory's
     * own boxing already goes through {@code typeDescriptorFor}).
     *
     * <p>Consumed by the phase-3 {@code IfaceDispatchBuilder}'s
     * call-site arg adaptation: boxing an argument whose type was never
     * otherwise interned in the enclosing function's {@code BuildCtx} must
     * still produce the canonical instance, because type assertions, type
     * switches, and interface {@code ==} compare descriptors by identity.
     */
    public static graalgo.values.GoTypeDescriptor canonicalDescriptorFor(BuildCtx ctx, int typeId,
            String path) {
        return typeDescriptorFor(ctx, typeId, path);
    }

    /**
     * Display name for a qualified named-type name (P3-M7): the form gc's
     * %T prints. Uses the package NAME, not the full path ("json.Number",
     * not "encoding/json.Number"). gc 1.27 aliases encoding/json.RawMessage
     * to encoding/json/jsontext.Value, so it displays as jsontext.Value.
     *
     * <p>Public (P3-M7R2) for the phase-3 {@code DescriptorNames} replica:
     * leaf naming is a single source of truth; the replica independently
     * pins the branch dispatch.
     */
    public static String displayNameFor(String qname) {
        if ("encoding/json.RawMessage".equals(qname)) {
            return "jsontext.Value";
        }
        int slash = qname.lastIndexOf('/');
        if (slash >= 0) {
            int dot = qname.indexOf('.', slash);
            if (dot >= 0) {
                // "encoding/json.Number" -> "json.Number"
                return qname.substring(slash + 1, dot + 1) + qname.substring(dot + 1);
            }
            return qname.substring(slash + 1);
        }
        return qname;
    }

    /**
     * Go-syntax type name for a type-table entry (P3-M7): used as the
     * display name of structural descriptors (so %T prints "func()",
     * "chan int", etc., like gc). Only the kinds that can appear as
     * structural descriptors are handled; others throw FrontendException
     * (caller falls back to k#hash).
     *
     * <p>Public (P3-M7R2) for the phase-3 {@code DescriptorNames} replica.
     * Takes a bare {@link TypeTable}: naming is pure over the table.
     */
    public static String goSyntaxName(TypeTable types, int typeId, String path) {
        typeId = resolveAlias(types, typeId, path);
        Json.Object entry = types.entry(typeId, path);
        String k = entry.string("kind", path);
        Json.Object detail = entry.object("detail", path);
        return switch (k) {
            case "basic" -> detail.string("name", path);
            case "named" -> displayNameFor(detail.string("name", path));
            case "signature" -> {
                StringBuilder sb = new StringBuilder("func(");
                Json.Array params = detail.array("params", path);
                for (int i = 0; i < params.size(); i++) {
                    if (i > 0) {
                        sb.append(", ");
                    }
                    Json.Object p = (Json.Object) params.get(i);
                    sb.append(goSyntaxName(types, p.intValue("type", path), path));
                }
                sb.append(')');
                Json.Array results = detail.array("results", path);
                if (results.size() == 1) {
                    sb.append(' ').append(goSyntaxName(types,
                            ((Json.Object) results.get(0)).intValue("type", path), path));
                } else if (results.size() > 1) {
                    sb.append(" (");
                    for (int i = 0; i < results.size(); i++) {
                        if (i > 0) {
                            sb.append(", ");
                        }
                        Json.Object r = (Json.Object) results.get(i);
                        sb.append(goSyntaxName(types, r.intValue("type", path), path));
                    }
                    sb.append(')');
                }
                yield sb.toString();
            }
            case "chan" -> "chan " + goSyntaxName(types, detail.intValue("elem", path), path);
            case "pointer" -> {
                int elemId = resolveAlias(types, detail.intValue("elem", path), path);
                Json.Object elemEntry = types.entry(elemId, path);
                if ("named".equals(elemEntry.string("kind", path))) {
                    yield "*" + elemEntry.object("detail", path).string("name", path);
                }
                yield "*" + goSyntaxName(types, elemId, path);
            }
            case "slice" -> "[]" + goSyntaxName(types, detail.intValue("elem", path), path);
            case "array" -> "[" + detail.intValue("len", path) + "]"
                    + goSyntaxName(types, detail.intValue("elem", path), path);
            case "map" -> "map[" + goSyntaxName(types, detail.intValue("key", path), path) + "]"
                    + goSyntaxName(types, detail.intValue("elem", path), path);
            case "struct" -> {
                StringBuilder sb = new StringBuilder("struct{");
                Json.Array fields = detail.array("fields", path);
                for (int i = 0; i < fields.size(); i++) {
                    if (i > 0) {
                        sb.append("; ");
                    }
                    Json.Object fo = (Json.Object) fields.get(i);
                    sb.append(fo.string("name", path)).append(' ')
                            .append(goSyntaxName(types, fo.intValue("type", path), path));
                }
                yield sb.append('}').toString();
            }
            case "interface" -> detail.bool("empty", path) ? "interface {}" : "interface{...}";
            default -> throw new FrontendException(path + ": no Go-syntax name for kind " + k);
        };
    }

    /** Unnamed composite kinds canonicalized by structure (M6, WS H). */
    private static boolean isStructuralComposite(String k) {
        // P3-M7: "signature" added — func types are structural (two func()
        // types are identical even across packages; the type switch in
        // synthetic json's marshalValue must match main-package funcs).
        // P3-M7: "chan" added — chan types are structural too.
        return "struct".equals(k) || "array".equals(k) || "slice".equals(k)
                || "map".equals(k) || "pointer".equals(k) || "signature".equals(k)
                || "chan".equals(k);
    }

    /** Process-wide canonical descriptors for named types, keyed by qualified
     * name (M6, WS H); see {@link #typeDescriptorFor}. */
    private static final java.util.concurrent.ConcurrentHashMap<String,
            graalgo.values.GoTypeDescriptor> NAMED_DESCS =
            new java.util.concurrent.ConcurrentHashMap<>();

    /**
     * Process-wide canonical descriptors for unnamed composite types (M6,
     * WS H); see {@link #typeDescriptorFor}. A {@link java.util.concurrent.ConcurrentHashMap}
     * because test suites may build programs on multiple threads.
     */
    private static final java.util.concurrent.ConcurrentHashMap<String,
            graalgo.values.GoTypeDescriptor> STRUCTURAL_DESCS =
            new java.util.concurrent.ConcurrentHashMap<>();

    /**
     * Process-wide canonical struct descriptors (P3-M8b): descriptor
     * identity is type identity, so struct {@code ==} (GoStruct.equals is
     * descriptor-identity) and struct map keys work across functions and
     * packages. Keyed by qualified name (named types) or field structure
     * (anonymous) plus the full field layout — same-named types with
     * different layouts across runs never share an instance.
     * ConcurrentHashMap: test suites may build programs on multiple threads.
     */
    private static final java.util.concurrent.ConcurrentHashMap<String,
            graalgo.values.GoStructDescriptor> STRUCT_DESCS =
            new java.util.concurrent.ConcurrentHashMap<>();

    /**
     * P3-M8 (wave 2, reflect): descriptor <-> wire type id registry, and
     * its inverse. Recorded additively in {@link #typeDescriptorFor}; zero
     * behavior change. Consumed by the reflect package to map a
     * GoTypeDescriptor (from a GoInterface) to its canonical reflect token.
     */
    private static final java.util.concurrent.ConcurrentHashMap<
            graalgo.values.GoTypeDescriptor, Integer> DESCRIPTOR_WIRE_IDS =
            new java.util.concurrent.ConcurrentHashMap<>();
    private static final java.util.concurrent.ConcurrentHashMap<
            Integer, graalgo.values.GoTypeDescriptor> WIRE_ID_DESCRIPTORS =
            new java.util.concurrent.ConcurrentHashMap<>();

    /** Wire type id for a descriptor, or null when unknown. */
    public static Integer wireIdForDescriptor(graalgo.values.GoTypeDescriptor d) {
        return DESCRIPTOR_WIRE_IDS.get(d);
    }

    /** Descriptor for a wire type id, or null when unknown. */
    public static graalgo.values.GoTypeDescriptor descriptorForWireId(int typeId) {
        return WIRE_ID_DESCRIPTORS.get(typeId);
    }

    /**
     * P3-M9 (reflect): canonical GoTypeDescriptor for a named type by
     * qualified name, for reflect's {@code Interface()} boxing. The
     * reflect package's per-run type table knows a type's qualified name
     * and kind but has no BuildCtx; this builds the same canonical
     * instance {@link #typeDescriptorFor} would (process-wide
     * {@code NAMED_DESCS} keyed by qualified name, display name via
     * {@link #displayNameFor}), so descriptor identity holds whether the
     * descriptor was first materialized by the frontend or by reflect.
     * Callers pass {@code "*" + qname} for pointer-to-named, mirroring
     * {@code typeDescriptorFor}'s naming.
     */
    public static graalgo.values.GoTypeDescriptor namedDescriptorFor(
            String qualifiedName, graalgo.values.GoKind kind) {
        graalgo.values.GoTypeDescriptor nd = NAMED_DESCS.get(qualifiedName);
        if (nd == null) {
            nd = new graalgo.values.GoTypeDescriptor(
                    displayNameFor(qualifiedName), kind, false);
            graalgo.values.GoTypeDescriptor prev =
                    NAMED_DESCS.putIfAbsent(qualifiedName, nd);
            if (prev != null) {
                nd = prev;
            }
        }
        return nd;
    }

    /** Records a descriptor <-> wire id pair (additive; no behavior change). */
    private static void recordDescriptorWireId(
            graalgo.values.GoTypeDescriptor d, int typeId) {
        DESCRIPTOR_WIRE_IDS.put(d, typeId);
        WIRE_ID_DESCRIPTORS.put(typeId, d);
    }

    /**
     * Structural identity key for a type-table entry (M6, WS H). Named
     * types contribute their qualified name (never expanded — expansion
     * would not terminate on recursive named types, and qualified names
     * are unique within a program); everything else is fully structural.
     */
    private static String structuralKey(BuildCtx ctx, int typeId, String path) {
        typeId = resolveAlias(ctx, typeId, path);
        Json.Object entry = ctx.types.entry(typeId, path);
        String k = entry.string("kind", path);
        Json.Object detail = entry.object("detail", path);
        switch (k) {
            case "basic":
                return "basic:" + kindOf(ctx, typeId, path).name();
            case "named":
                return "named(" + detail.string("name", path) + ")";
            case "struct": {
                StringBuilder sb = new StringBuilder("struct{");
                Json.Array fields = detail.array("fields", path);
                for (int i = 0; i < fields.size(); i++) {
                    Json.Value fv = fields.get(i);
                    if (!(fv instanceof Json.Object fo)) {
                        throw new FrontendException(path + ": struct field not an object");
                    }
                    sb.append(fo.string("name", path)).append(':')
                            .append(structuralKey(ctx, fo.intValue("type", path), path))
                            .append(';');
                }
                return sb.append('}').toString();
            }
            case "array":
                return "array[" + detail.intValue("len", path) + "]"
                        + structuralKey(ctx, detail.intValue("elem", path), path);
            case "slice":
                return "slice[" + structuralKey(ctx, detail.intValue("elem", path), path) + "]";
            case "map":
                return "map[" + structuralKey(ctx, detail.intValue("key", path), path) + "]"
                        + structuralKey(ctx, detail.intValue("elem", path), path);
            case "pointer":
                return "ptr(" + structuralKey(ctx, detail.intValue("elem", path), path) + ")";
            case "chan":
                // P3-M7: chan types are structural (chan int is chan int
                // across packages).
                return "chan(" + structuralKey(ctx, detail.intValue("elem", path), path) + ")";
            case "signature": {
                // P3-M7: func types are structural (two func() types are
                // identical, even across packages). Params/results are
                // [{"name":..,"type":tid}] objects.
                StringBuilder sb = new StringBuilder("func(");
                Json.Array params = detail.array("params", path);
                for (int i = 0; i < params.size(); i++) {
                    Json.Object p = (Json.Object) params.get(i);
                    sb.append(structuralKey(ctx, p.intValue("type", path), path)).append(',');
                }
                sb.append(")(");
                Json.Array results = detail.array("results", path);
                for (int i = 0; i < results.size(); i++) {
                    Json.Object r = (Json.Object) results.get(i);
                    sb.append(structuralKey(ctx, r.intValue("type", path), path)).append(',');
                }
                sb.append(')');
                if (detail.opt("variadic") instanceof Json.Bool b && b.value()) {
                    sb.append("variadic");
                }
                return sb.toString();
            }
            default:
                // interface, invalid: program-local identity.
                return k + "#" + typeId;
        }
    }

    /**
     * Canonical dynamic-type descriptors per basic Go type name (P3-M7):
     * "int" and "int64" are distinct types (different %T, different type
     * switch cases) even though they share a kind. Process-wide cache.
     *
     * <p>P3-M7R2: this is the SINGLE unification point for basic-type
     * identity. Every path that mints a descriptor for a Go basic type —
     * {@code typeDescriptorFor}, {@code basicDescriptorFor},
     * {@code boxInterface}, {@code wrapInterface} — must go through
     * {@link #basicDescriptorForName}, or two paths will mint different
     * objects for one Go type and identity comparisons (type assertions,
     * type switches, interface ==) will fail with the maddening
     * "interface {} is int64, not int64" symptom. The kind-keyed
     * {@code BASIC_DESCS} cache was removed: a GoKind alone cannot
     * identify a Go type (INT64 is both "int" and "int64").
     */
    private static final java.util.concurrent.ConcurrentHashMap<String,
            graalgo.values.GoTypeDescriptor> BASIC_NAMED_DESCS =
            new java.util.concurrent.ConcurrentHashMap<>();

    /**
     * Canonical descriptor for the Go basic type {@code goName} (P3-M7R2):
     * the unification point for basic-type identity. {@code kind} is the
     * value-representation kind (e.g. INT64 for both "int" and "int64").
     */
    public static graalgo.values.GoTypeDescriptor basicDescriptorForName(String goName,
            GoKind kind) {
        graalgo.values.GoTypeDescriptor d = BASIC_NAMED_DESCS.get(goName);
        if (d == null) {
            d = new graalgo.values.GoTypeDescriptor(goName, kind, false);
            graalgo.values.GoTypeDescriptor prev = BASIC_NAMED_DESCS.putIfAbsent(goName, d);
            if (prev != null) {
                d = prev;
            }
        }
        return d;
    }

    /**
     * Canonical Go type name for a basic type-table entry name (P3-M7R2):
     * untyped constants normalize to their default type ("untyped int" →
     * "int"); alias spellings normalize to canonical names ("rune" →
     * "int32", "byte" → "uint8"). Public for the phase-3
     * {@code DescriptorNames} replica — single source of truth.
     */
    public static String canonicalBasicName(String name) {
        if (name.startsWith("untyped ")) {
            return defaultBasicName(name);
        }
        if ("rune".equals(name)) {
            return "int32";
        }
        if ("byte".equals(name)) {
            return "uint8";
        }
        return name;
    }

    /**
     * Default Go type name for an untyped constant (P3-M7R2): the type gc
     * gives the value when it acquires one. Note "int", not "int64"; an
     * untyped rune constant defaults to {@code rune}, which is identical
     * to {@code int32} (so the canonical "int32" descriptor is used —
     * gc's %T prints "int32" for {@code any('a')}).
     *
     * <p>Public for the phase-3 {@code DescriptorNames} naming replica.
     */
    public static String defaultBasicName(String untypedName) {
        return switch (untypedName) {
            case "untyped int" -> "int";
            case "untyped rune" -> "int32";
            case "untyped float" -> "float64";
            case "untyped complex" -> "complex128";
            case "untyped string" -> "string";
            case "untyped bool" -> "bool";
            default -> throw new FrontendException(
                    "no default type for " + untypedName + " (WS E)");
        };
    }

    /** Value-representation kind for a Go basic type name (P3-M7R2). */
    private static GoKind kindForBasicName(String name) {
        return switch (name) {
            case "int8" -> GoKind.INT8;
            case "int16" -> GoKind.INT16;
            case "int32" -> GoKind.INT32;
            case "int", "int64" -> GoKind.INT64;
            case "uint8" -> GoKind.UINT8;
            case "uint16" -> GoKind.UINT16;
            case "uint32" -> GoKind.UINT32;
            case "uint", "uint64", "uintptr" -> GoKind.UINT64;
            case "float32" -> GoKind.FLOAT32;
            case "float64" -> GoKind.FLOAT64;
            case "complex64" -> GoKind.COMPLEX64;
            case "complex128" -> GoKind.COMPLEX128;
            case "string" -> GoKind.STRING;
            case "bool" -> GoKind.BOOL;
            // P3-M8b: the synthetic unsafe package's Pointer alias may surface
            // as a basic name; it denotes unsafe.Pointer.
            case "Pointer", "unsafe.Pointer" -> GoKind.UNSAFE_POINTER;
            default -> throw new FrontendException(
                    "no kind for basic type name " + name + " (WS E)");
        };
    }

    /**
     * Canonical dynamic-type descriptor per basic kind (M4, WS E).
     * P3-M7R2: delegates to the name-keyed unification point with the
     * kind's canonical Go spelling ("int64" for INT64). Callers that know
     * the actual Go type name (e.g. "int" vs "int64", or an untyped
     * constant's default type) must use {@link #basicDescriptorForName}
     * instead — a kind alone cannot distinguish them.
     *
     * @deprecated Use {@link #basicDescriptorForName(String, GoKind)} with
     * the exact Go type name; a kind alone cannot identify a Go type.
     */
    @Deprecated
    public static graalgo.values.GoTypeDescriptor basicDescriptorFor(GoKind kind) {
        return basicDescriptorForName(kind.name().toLowerCase(), kind);
    }

    /**
     * True if the descriptor is one of the canonical predeclared-basic
     * descriptors (M5, WS F): used by panic rendering to distinguish
     * {@code 5} (bare) from {@code main.MyInt(5)} (named).
     */
    public static boolean isPredeclaredBasic(graalgo.values.GoTypeDescriptor d) {
        return BASIC_NAMED_DESCS.containsValue(d);
    }

    /** Default kind of an untyped constant kind (for interface boxing).
     * Public for the phase-3 boxing replica. */
    public static GoKind defaultKind(GoKind untyped) {
        return switch (untyped) {
            case UNTYPED_INT -> GoKind.INT64;
            case UNTYPED_FLOAT -> GoKind.FLOAT64;
            case UNTYPED_COMPLEX -> GoKind.COMPLEX128;
            case UNTYPED_STRING -> GoKind.STRING;
            case UNTYPED_BOOL -> GoKind.BOOL;
            default -> throw new FrontendException("no default type for kind " + untyped + " (WS E)");
        };
    }

    /** True when the type-table id denotes the empty interface. */
    static boolean isEmptyInterface(BuildCtx ctx, int typeId, String path) {
        typeId = resolveAlias(ctx, typeId, path);
        Json.Object entry = ctx.types.entry(typeId, path);
        return "interface".equals(entry.string("kind", path))
                && entry.object("detail", path).bool("empty", path);
    }

    /**
     * P3-M3 remediation (R1 P1-2): fail fast with the named P3-M3 scope
     * error when a generic instantiation (e.g. {@code Box[int]}) would be
     * boxed into a non-empty interface. The exporter's method tables skip
     * template methods, so the instantiation's wire {@code "methods"} list
     * is empty (marked {@code "templateMethodsSkipped"}) and the first
     * dispatch would otherwise die with the misleading "no method table
     * installed" error. Boxing into the empty interface stays legal (no
     * method table is consulted there).
     *
     * <p>Greppable re-test trigger: when instantiation method tables are
     * supported, delete this check and the exporter's
     * {@code "templateMethodsSkipped"} marker.
     */
    public static void checkInstantiationBoxedAsInterface(BuildCtx ctx, int fromId, int toTypeId,
            String path) {
        if (fromId < 0 || isEmptyInterface(ctx, toTypeId, path)) {
            return;
        }
        int tid = resolveAlias(ctx, fromId, path);
        Json.Object entry = ctx.types.entry(tid, path);
        String k = entry.string("kind", path);
        if (!"named".equals(k) && !"pointer".equals(k)) {
            return;
        }
        Json.Object detail = entry.object("detail", path);
        Json.Value v = detail.opt("templateMethodsSkipped");
        if (!(v instanceof Json.Bool b) || !b.value()) {
            return;
        }
        String display = "named".equals(k) ? detail.string("name", path)
                : "*" + namedDisplayName(ctx, detail.intValue("elem", path), path);
        throw new FrontendException(path + ": cannot use generic instantiation " + display
                + " as a non-empty interface: method tables for instantiated methods"
                + " are not supported (P3-M3 scope)");
    }

    /** Display name for a type-table id: the qualified name for named types, else the kind. */
    private static String namedDisplayName(BuildCtx ctx, int typeId, String path) {
        int tid = resolveAlias(ctx, typeId, path);
        Json.Object entry = ctx.types.entry(tid, path);
        if ("named".equals(entry.string("kind", path))) {
            return entry.object("detail", path).string("name", path);
        }
        return "<" + entry.string("kind", path) + ">";
    }

    /**
     * Fresh Go zero value for a struct type id, recursively zeroing nested
     * struct/array fields (M4, WS E). The template is built once per type;
     * callers copy it per evaluation (never share mutable zeros).
     */
    static GoStruct structZeroTemplate(BuildCtx ctx, int typeId, String path) {
        typeId = resolveAlias(ctx, typeId, path);
        GoStruct t = ctx.structZeroTemplates.get(typeId);
        if (t != null) {
            return t;
        }
        // M10 (P1-3): cycle guard. The memo above only fills after children
        // are built, so a cyclic wire graph would recurse forever. Fail fast
        // with FrontendException instead of StackOverflowError.
        if (!ctx.zeroTemplateVisiting.add(typeId)) {
            throw new FrontendException(path + ": cyclic struct type graph at type id "
                    + typeId + " (WS E)");
        }
        try {
            GoStructDescriptor desc = structDescriptorFor(ctx, typeId, path);
            int[] fieldTypeIds = structFieldTypeIds(ctx, typeId, path);
            Object[] objects = new Object[desc.objectsCount()];
            for (int i = 0; i < desc.fields().size(); i++) {
                GoStructDescriptor.Field f = desc.field(i);
                if (!f.inLongs()) {
                    objects[f.index()] = fieldZeroTemplate(ctx, f.kind(), fieldTypeIds[i], path);
                }
            }
            t = new GoStruct(desc, new long[desc.longsCount()], objects);
            ctx.structZeroTemplates.put(typeId, t);
            return t;
        } finally {
            ctx.zeroTemplateVisiting.remove(typeId);
        }
    }

    /** Zero template for one struct field / array element (null when trivially zero). */
    private static Object fieldZeroTemplate(BuildCtx ctx, GoKind kind, int fieldTypeId, String path) {
        return switch (kind) {
            case STRING -> GoString.EMPTY;
            // WS G: a complex field's zero value is (0+0i), not nil.
            case COMPLEX64 -> graalgo.values.GoComplex32.ZERO;
            case COMPLEX128, UNTYPED_COMPLEX -> graalgo.values.GoComplex64.ZERO;
            // P3-M9 (driver-harden, go-cmp lane): sync primitive fields get
            // usable Java primitive objects, not empty structs. Each
            // evaluation gets its own instance (the template is copied
            // per use via Boxing.freshZeroCopy for the non-pool cases;
            // pools are freshened in arrayZeroTemplate/deepCopyArray).
            case STRUCT -> {
                String prim = syncPrimitiveName(ctx, fieldTypeId, path);
                if (prim != null) {
                    yield newSyncPrimitive(prim);
                }
                yield structZeroTemplate(ctx, fieldTypeId, path);
            }
            case ARRAY -> arrayZeroTemplate(ctx, fieldTypeId, path);
            default -> null;
        };
    }

    /** Manufactures a fresh Java object for a sync primitive type name. */
    static Object newSyncPrimitive(String prim) {        return switch (prim) {
            case "Mutex" -> new graalgo.runtime.sync.GoMutex();
            case "RWMutex" -> new graalgo.runtime.sync.GoRWMutex();
            case "WaitGroup" -> new graalgo.runtime.sync.GoWaitGroup();
            case "Once" -> new graalgo.runtime.sync.GoOnce();
            case "Pool" -> new GoPool();
            // fix-linkname: sync.Map.
            case "Map" -> new graalgo.runtime.sync.GoSyncMap();
            // fix-linkname: sync/atomic wrapper cells (struct fields).
            case "atomic.Int64", "atomic.Uint64", "atomic.Uintptr" ->
                new graalgo.runtime.sync.GoAtomicInt64();
            case "atomic.Int32", "atomic.Uint32", "atomic.Bool" ->
                new graalgo.runtime.sync.GoAtomicInt32();
            case "atomic.Pointer", "atomic.Value" ->
                new java.util.concurrent.atomic.AtomicReference<>();
            default -> throw new IllegalArgumentException("unknown sync primitive " + prim);
        };
    }

    /**
     * Whether the type-table id denotes one of the synthetic sync/atomic
     * primitive named types (resolving aliases). Returns the primitive
     * name ("Mutex", ..., "Map", "atomic.Int64", ..., "atomic.Pointer",
     * "atomic.Value") or null. Generic instantiations (e.g.
     * {@code sync/atomic.Pointer[T]}) match by their base name.
     */
    private static String syncPrimitiveName(BuildCtx ctx, int typeId, String path) {
        int rid = resolveAlias(ctx, typeId, path);
        Json.Object entry = ctx.types.entry(rid, path);
        if (!"named".equals(entry.string("kind", path))) {
            return null;
        }
        String name = entry.object("detail", path).string("name", path);
        if (name == null) {
            return null;
        }
        if (name.startsWith("sync.")) {
            String base = name.substring(5);
            switch (base) {
                case "Mutex", "RWMutex", "WaitGroup", "Once", "Pool",
                     // fix-linkname: sync.Map.
                     "Map":
                    return base;
            }
        }
        if (name.startsWith("sync/atomic.")) {
            String base = name.substring(12);
            // Strip generic instantiation suffix: Pointer[T] -> Pointer.
            int br = base.indexOf('[');
            if (br >= 0) {
                base = base.substring(0, br);
            }
            switch (base) {
                case "Int64", "Uint64", "Uintptr", "Int32", "Uint32", "Bool",
                     "Pointer", "Value":
                    return "atomic." + base;
            }
        }
        return null;
    }

    private static Object arrayZeroTemplate(BuildCtx ctx, int arrayTypeId, String path) {
        int rid = resolveAlias(ctx, arrayTypeId, path);
        Json.Object entry = ctx.types.entry(rid, path);
        // M10 (P1-3): bounded chain walk; a cycle fails fast, never hangs.
        for (int steps = 0; ; steps++) {
            if (steps > MAX_TYPE_CHAIN_STEPS) {
                throw new FrontendException(path + ": named-type chain exceeds "
                        + MAX_TYPE_CHAIN_STEPS + " steps (cyclic type graph?) (WS E)");
            }
            if (!"named".equals(entry.string("kind", path))) {
                break;
            }
            rid = entry.object("detail", path).intValue("underlying", path);
            entry = ctx.types.entry(rid, path);
        }
        Json.Object detail = entry.object("detail", path);
        int len = detail.intValue("len", path);
        int elemId = detail.intValue("elem", path);
        GoKind elemKind = kindOf(ctx, elemId, path);
        Object arr = Boxing.newArray(elemKind, len);
        Object elemZero = fieldZeroTemplate(ctx, elemKind, elemId, path);
        if (elemZero != null && arr instanceof Object[] oa) {
            // Elements share the template; Boxing.freshZeroCopy gives each
            // evaluation its own copy (fresh primitives for sync types).
            Arrays.fill(oa, elemZero);
        }
        return arr;
    }

    /**
     * Inserts a {@link ConvertNode} when {@code child}'s kind is not usable
     * directly where {@code to} is expected. Untyped constants are
     * transparent ({@link GoKind#transparent}) and pass through — this
     * mirrors go/types, where they assume the context type.
     *
     * <p>M4 (WS E): concrete→interface conversions box the value into a
     * {@link GoInterface} with the value's dynamic-type descriptor
     * ({@link InterfaceBoxNode}); interface→interface across different
     * types passes only for the empty interface. P3-M3 (WS D): the old
     * "needs method tables" fail-fasts are retired — concrete values now
     * box into non-empty interfaces too. Boxing needs only the dynamic
     * type descriptor; itabs are built lazily at dispatch from the
     * method tables (phase-3 {@code IfaceMethodTable}), so no method
     * tables are required at the conversion site. go/types already
     * verified the conversion's assignability (signatures included);
     * the box preserves the dynamic type for the dispatch-time check.
     */
    private GoExprNode maybeConvert(GoExprNode child, GoKind to, int toTypeId, BuildCtx ctx, String path) {
        return maybeConvert(child, to, toTypeId, ctx, path, null);
    }

    /**
     * True when the type-table id denotes an interface type (named
     * interface types count: follows {@code named}→{@code underlying};
     * P3-M3, WS D). Alias steps resolve first, as everywhere else.
     */
    static boolean isInterfaceType(BuildCtx ctx, int typeId, String path) {
        typeId = resolveAlias(ctx, typeId, path);
        for (int steps = 0; steps < MAX_TYPE_CHAIN_STEPS; steps++) {
            Json.Object entry = ctx.types.entry(typeId, path);
            String k = entry.string("kind", path);
            if ("interface".equals(k)) {
                return true;
            }
            if (!"named".equals(k)) {
                return false;
            }
            typeId = entry.object("detail", path).intValue("underlying", path);
        }
        throw new FrontendException(path + ": type chain exceeds bound following named→underlying (WS E)");
    }

    /**
     * Method implkeys of an interface type-table id (P3-M3, WS D): the
     * {@code "implkey"} of each entry in the {@code "interface"} entry's
     * {@code "methods"} list, following {@code named}→{@code underlying}
     * and aliases. Empty when the id does not denote an interface.
     *
     * <p>An implkey is the method-identity key the exporter computes
     * (method name, unexported names package-qualified, plus a structural
     * signature key with aliases expanded — see export.go {@code implKeyOf});
     * comparing implkeys (not bare names) is what makes the assertion
     * implements check signature-aware (R1 P0-1).
     */
    static Set<String> interfaceMethodImplKeys(BuildCtx ctx, int typeId, String path) {
        typeId = resolveAlias(ctx, typeId, path);
        for (int steps = 0; steps < MAX_TYPE_CHAIN_STEPS; steps++) {
            Json.Object entry = ctx.types.entry(typeId, path);
            String k = entry.string("kind", path);
            if ("interface".equals(k)) {
                return methodImplKeys(entry.object("detail", path), path);
            }
            if (!"named".equals(k)) {
                return Set.of();
            }
            typeId = entry.object("detail", path).intValue("underlying", path);
        }
        throw new FrontendException(path + ": type chain exceeds bound following named→underlying (WS E)");
    }

    /**
     * Implkeys of a type-table entry's {@code detail.methods} list
     * (tolerates a missing list). A missing {@code "implkey"} on an entry
     * is a corrupt 0.4.0 document — fail fast, never fall back to the
     * name-only comparison (that fallback IS the R1 P0-1 soundness hole).
     */
    private static Set<String> methodImplKeys(Json.Object detail, String path) {
        Set<String> keys = new HashSet<>();
        Json.Value mv = detail.opt("methods");
        if (!(mv instanceof Json.Array methods)) {
            return keys;
        }
        for (int i = 0; i < methods.size(); i++) {
            Json.Value e = methods.get(i);
            if (e instanceof Json.Object mo) {
                keys.add(mo.string("implkey", path));
            }
        }
        return keys;
    }

    /**
     * Sentinel member of a descriptor's implkey set (P3-M3 remediation, R1
     * P1-2): the wire method table skipped template methods, so the set is
     * empty by design — not because the type has no methods. The runtime
     * implements check fails fast with the named P3-M3 scope error on this
     * sentinel instead of reporting "not implemented": the instantiation
     * may well implement the interface (gc decides by its real method
     * set), and a bare {@code false} would be a silent divergence.
     */
    public static final String TEMPLATE_METHODS_SKIPPED =
            "\u0000<instantiation: template methods skipped (P3-M3 scope)>";

    /** True when a type-table entry's detail carries the templateMethodsSkipped marker. */
    private static boolean templateMethodsSkipped(Json.Object detail) {
        return detail.opt("templateMethodsSkipped") instanceof Json.Bool b && b.value();
    }

    /**
     * P3-M3 (WS D): descriptor name → method implkeys for every
     * {@code named}/{@code pointer} type-table entry, memoized per build.
     * Descriptor names replicate {@link #typeDescriptorFor} exactly
     * ({@code named} → display name; {@code pointer} → "*"+qualifiedName
     * for pointer-to-named, structural otherwise), so the keys match the
     * dynamic-type descriptors boxing produces at runtime. Each implkey is the
     * exporter's method-identity key (name + structural signature, see
     * {@link #interfaceMethodImplKeys}) — the implements check compares
     * implkeys, not bare names, so same-name/wrong-signature methods do
     * not satisfy an assertion (R1 P0-1). Consumed by type assertions to
     * non-empty interfaces.
     *
     * <p>P3-M7R2: the pointer branch mirrors dbd728c and the named branch
     * mirrors fa099f2's displayNameFor — both install sides must agree
     * with typeDescriptorFor byte-exactly, or assertions miss.
     */
    static Map<String, Set<String>> dynamicMethodSets(BuildCtx ctx, String path) {
        if (ctx.dynamicMethodSets != null) {
            return ctx.dynamicMethodSets;
        }
        Map<String, Set<String>> out = new HashMap<>();
        for (int id = 0; id < ctx.types.size(); id++) {
            Json.Object entry = ctx.types.entry(id, path);
            String k = entry.string("kind", path);
            Json.Object detail = entry.object("detail", path);
            if ("named".equals(k)) {
                Set<String> implkeys = methodImplKeys(detail, path);
                if (templateMethodsSkipped(detail)) {
                    implkeys.add(TEMPLATE_METHODS_SKIPPED);
                }
                out.put(displayNameFor(detail.string("name", path)), implkeys);
            } else if ("pointer".equals(k)) {
                // Must equal typeDescriptorFor's naming: "*"+qualifiedName
                // for pointer-to-named; goSyntaxName (e.g. "*[]int") with
                // "pointer#"+hex fallback for pointer-to-unnamed.
                int elemId = resolveAlias(ctx,
                        detail.intValue("elem", path), path);
                Json.Object elemEntry = ctx.types.entry(elemId, path);
                String key;
                if ("named".equals(elemEntry.string("kind", path))) {
                    key = "*" + elemEntry.object("detail", path).string("name", path);
                } else {
                    try {
                        key = goSyntaxName(ctx.types, id, path);
                    } catch (FrontendException e) {
                        String skey = structuralKey(ctx, id, path);
                        key = "pointer#" + Integer.toHexString(skey.hashCode());
                    }
                }
                Set<String> implkeys = methodImplKeys(detail, path);
                if (templateMethodsSkipped(detail)) {
                    implkeys.add(TEMPLATE_METHODS_SKIPPED);
                }
                out.put(key, implkeys);
            }
        }
        ctx.dynamicMethodSets = out;
        return out;
    }

    private GoExprNode maybeConvert(GoExprNode child, GoKind to, int toTypeId, BuildCtx ctx,
            String path, GoKind sliceElemKind) {
        GoKind from = child.kind();
        // M7: untyped nil is assignable to every nillable kind without
        // conversion (e.g. Addr{0, 0, nil} with z *byte, issue51840.go).
        // The NilNode evaluates to null, the correct zero for all of these;
        // routing it through ConvertNode would throw "illegal conversion:
        // INTERFACE -> POINTER". A nil-to-non-nillable conversion can never
        // appear here: go/types rejects such programs before export.
        // P3-M3 (WS D): the shortcut does NOT apply for INTERFACE targets —
        // a typed nil (e.g. (*Buf)(nil)) must box to a non-nil interface
        // holding a nil pointer, not collapse to the nil interface.
        if (child instanceof NilNode && isNillable(to) && to != GoKind.INTERFACE) {
            return child;
        }
        if (GoKind.transparent(from, to)) {
            return child;
        }
        if (to == GoKind.INTERFACE) {
            // R2 P2-5: the old from==INTERFACE inner branch (empty-interface
            // fast path / "needs method tables" throw) is unreachable —
            // GoKind.transparent(INTERFACE, INTERFACE) returns true above,
            // so interface-to-interface never reaches here; the WS-D
            // retirement amendment covers the live paths.
            // P3-M3 remediation (R1 P1-2): a generic instantiation boxed
            // into a non-empty interface fails fast with the named P3-M3
            // scope error (its wire method table is empty by design).
            checkInstantiationBoxedAsInterface(ctx, child.typeId(), toTypeId, path);
            // P3-M3 (WS D): concrete→non-empty-interface boxes (see javadoc
            // above). The empty-interface fast path below is subsumed.
            return boxInterface(child, ctx, path);
        }
        ConvertNode n = new ConvertNode(from, to, child, sliceElemKind);
        n.setSourceSection(child.getSourceSection());
        return n;
    }

    /**
     * Boxes {@code child} into an interface value (P3-M10). This is the
     * public hook for interop node builders whose wire ops take
     * interface-typed operands without an explicit conversion node on
     * the wire — e.g. the {@code atomic.Value} syncop args, which are
     * declared {@code any}, so concrete arg nodes must box exactly as
     * call args do. Behavior is identical to the implicit-conversion
     * path: already-interface children pass through untouched
     * ({@link GoKind#transparent}), concrete children box with their
     * dynamic-type descriptor. No existing kind's behavior changes.
     *
     * <p>The target is always the empty interface, so the P3-M3
     * "generic instantiation as non-empty interface" fail-fast (which
     * needs the target's type-table id) is vacuous here and skipped —
     * boxing an instantiation into {@code any} is always legal.
     */
    public GoExprNode boxToInterface(GoExprNode child, BuildCtx ctx, String path) {
        if (GoKind.transparent(child.kind(), GoKind.INTERFACE)) {
            return child;
        }
        return boxInterface(child, ctx, path);
    }

    /**
     * Boxes a concrete value into an interface (M4, WS E). The dynamic type
     * is the value's static type id (named types stay distinct); an untyped
     * constant takes its default type.
     */
    private GoExprNode boxInterface(GoExprNode child, BuildCtx ctx, String path) {        GoKind from = child.kind();
        GoKind dataKind;
        graalgo.values.GoTypeDescriptor descriptor;
        if (from.isUntyped()) {
            dataKind = defaultKind(from);
            // P3-M7R2: the default Go type name ("int", not "int64"; untyped
            // rune -> "int32"), resolved from the node's type-table id when
            // available so every boxing path mints the identical descriptor.
            descriptor = untypedDefaultDescriptor(ctx, child, dataKind, path);
        } else {
            dataKind = from;
            int fromId = child.typeId();
            if (fromId < 0) {
                throw new FrontendException(path + ": cannot box a node without a type-table id (WS E)");
            }
            descriptor = typeDescriptorFor(ctx, fromId, path);
        }
        InterfaceBoxNode n = new InterfaceBoxNode(child, dataKind, descriptor);
        n.setSourceSection(child.getSourceSection());
        n.setTypeId(child.typeId());
        return n;
    }

    /**
     * Canonical descriptor for an untyped constant's default Go type
     * (P3-M7R2): resolves the exact untyped name from the node's
     * type-table id when available ("untyped int" -> "int", "untyped
     * rune" -> "int32"), so boxing, type switches, and assertions all
     * mint the identical instance. Falls back to the kind's conventional
     * default when the node carries no type id. Public for the phase-3
     * {@code IfaceDispatchBuilder} boxing replica.
     */
    public static graalgo.values.GoTypeDescriptor untypedDefaultDescriptor(BuildCtx ctx,
            GoExprNode child, GoKind dataKind, String path) {
        int tid = child.typeId();
        if (tid >= 0) {
            String name = ctx.types.entry(tid, path).object("detail", path)
                    .string("name", path);
            if (name.startsWith("untyped ")) {
                String defName = defaultBasicName(name);
                return basicDescriptorForName(defName, kindForBasicName(defName));
            }
        }
        return basicDescriptorForName(defaultNameForKind(dataKind), dataKind);
    }

    /**
     * Conventional default Go type name for a value kind (P3-M7R2):
     * used when no type-table id is available to resolve the exact
     * untyped name.
     */
    private static String defaultNameForKind(GoKind kind) {
        return switch (kind) {
            case INT64 -> "int";
            case FLOAT64 -> "float64";
            case COMPLEX128 -> "complex128";
            case STRING -> "string";
            case BOOL -> "bool";
            default -> throw new FrontendException(
                    "no default basic type name for kind " + kind + " (WS E)");
        };
    }

    /**
     * Type-id-free form for call sites where no interface conversion can
     * arise (binary/unary operands).
     */
    private static GoExprNode maybeConvert(GoExprNode child, GoKind to, String path) {
        GoKind from = child.kind();
        if (GoKind.transparent(from, to)) {
            return child;
        }
        if (to == GoKind.INTERFACE || from == GoKind.INTERFACE) {
            throw new FrontendException(path + ": interface conversion needs type ids (WS E)");
        }
        ConvertNode n = new ConvertNode(from, to, child);
        n.setSourceSection(child.getSourceSection());
        return n;
    }

    /**
     * Go kinds that admit the predeclared {@code nil} as a value (M7).
     */
    private static boolean isNillable(GoKind k) {
        return k == GoKind.POINTER || k == GoKind.SLICE || k == GoKind.MAP
                || k == GoKind.FUNC || k == GoKind.INTERFACE || k == GoKind.UNSAFE_POINTER;
    }

    private int slotForObj(Json.Object fields, String kind, BuildCtx ctx) {
        int oid = fields.intValue("obj", kind + ".fields");
        Integer slot = ctx.objSlots.get(oid);
        if (slot == null) {
            throw new FrontendException(kind + ": reference to unknown object id " + oid);
        }
        return slot;
    }

    /**
     * Resolves an object id through the funclit parent chain (M5, WS F).
     * Returns the depth (0 = current function, >0 = ancestor) and the slot
     * in that ancestor's frame. Throws if the oid is not found.
     */
    private static final class VarResolution {
        final int depth;
        final int slot;
        final BuildCtx owner;

        VarResolution(int depth, int slot, BuildCtx owner) {
            this.depth = depth;
            this.slot = slot;
            this.owner = owner;
        }
    }

    private VarResolution resolveVar(BuildCtx ctx, int oid, String path) {
        BuildCtx c = ctx;
        int depth = 0;
        while (c != null) {
            Integer slot = c.objSlots.get(oid);
            if (slot != null) {
                return new VarResolution(depth, slot, c);
            }
            c = c.parent;
            depth++;
        }
        throw new FrontendException(path + ": reference to unknown object id " + oid);
    }

    /**
     * Resolution of a boxed (loopcap) variable: like {@link #resolveVar} but
     * also consults each level's {@code boxSnapSlots} (a funclit that
     * captured the variable keeps the snapshot cell in its own frame).
     * The caller guarantees {@code oid} is boxed (transitively).
     */
    private static final class BoxResolution {
        final int depth; // hops from ctx's frame to the holding frame
        final int slot;  // slot in the holding frame (cell or snapshot)
        final boolean isSnapshot; // true if slot is a snapshot slot

        BoxResolution(int depth, int slot, boolean isSnapshot) {
            this.depth = depth;
            this.slot = slot;
            this.isSnapshot = isSnapshot;
        }
    }

    private BoxResolution resolveBoxed(BuildCtx ctx, int oid, String path) {
        BuildCtx c = ctx;
        int depth = 0;
        while (c != null) {
            Integer snap = c.boxSnapSlots.get(oid);
            if (snap != null) {
                return new BoxResolution(depth, snap, true);
            }
            Integer slot = c.objSlots.get(oid);
            if (slot != null) {
                return new BoxResolution(depth, slot, false);
            }
            c = c.parent;
            depth++;
        }
        throw new FrontendException(path + ": reference to unknown boxed object id " + oid);
    }

    /**
     * Records a boxed capture for the funclit whose body is being
     * built in {@code ctx}: allocates the snapshot slot in the funclit's
     * frame and appends the {@code (depth, slot, snapSlot)} triple to
     * {@code ctx.boxSpec}. The {@code (depth, slot)} is resolved in the
     * <em>creating</em> frame ({@code ctx.parent}), which is where the
     * {@link FuncLitNode} executes.
     *
     * <p>For-header variables are per-iteration in every phase: a funclit
     * created in the loop body, cond, or post snapshots the per-iteration
     * cell (gc's {@code loopvar} desugar puts cond/post inside the
     * per-iteration region). A funclit created in the init statement
     * snapshots the carry cell instead, matching gc's
     * {@code init : s/z/z'} substitution (the per-iteration cell does not
     * exist yet). An ancestor's existing snapshot cell is always reused
     * as-is (never re-resolved).
     *
     * <p>Transitive capture (P0-D5): when the reference passes through
     * intermediate function literals, each of them snapshots the variable
     * too (outermost first), so the triple chain resolves through
     * per-iteration snapshot cells instead of the live frame slot at the
     * inner literal's (possibly much later) creation time.
     */
    private int snapshotBoxedVar(BuildCtx ctx, int oid, String path) {
        Integer existing = ctx.boxSnapSlots.get(oid);
        if (existing != null) {
            return existing;
        }
        BuildCtx creator = ctx.parent;
        if (creator == null) {
            throw new FrontendException(path + ": boxed capture in func main (no enclosing frame)");
        }
        ensureFunclitSnapshots(ctx, oid, path);
        BoxResolution b = resolveBoxed(creator, oid, path);
        int slot = b.slot;
        if (!b.isSnapshot) {
            int iter = headerIterSlot(creator, oid);
            if (iter >= 0 && headerPhase(oid) != LP_INIT) {
                // Per-iteration capture (body/cond/post, or a nested
                // loop's phase of an outer header var): the per-iteration
                // cell. The depth is unchanged: the per-iteration cell
                // lives in the same frame as the carry cell.
                slot = iter;
            }
            // LP_INIT (or no loop phase): the carry cell (b.slot).
        }
        int snapSlot = ctx.frames.addSlot(FrameSlotKind.Object, "boxsnap$" + oid, null);
        ctx.boxSnapSlots.put(oid, snapSlot);
        ctx.boxSpec.add(new int[]{b.depth, slot, snapSlot});
        ctx.hasCapture = true;
        return snapSlot;
    }

    /**
     * P0-D5: ensures every function-literal context strictly between
     * {@code ctx} (the funclit being built) and the declaring function of
     * the boxed variable {@code oid} snapshots the variable (outermost
     * first). Without this, a nested funclit that alone references a loop
     * variable would resolve its capture through the live frame slot at
     * its own creation time — which may be iterations later, or after the
     * loop — instead of the per-iteration cell of the iteration that
     * created the intermediate literal.
     */
    private void ensureFunclitSnapshots(BuildCtx ctx, int oid, String path) {
        BuildCtx decl = null;
        for (BuildCtx c = ctx.parent; c != null; c = c.parent) {
            if (c.objSlots.containsKey(oid) || c.boxSnapSlots.containsKey(oid)) {
                decl = c;
                break;
            }
        }
        if (decl == null) {
            throw new FrontendException(path + ": boxed oid " + oid + " has no declaring context");
        }
        List<BuildCtx> chain = new ArrayList<>();
        for (BuildCtx c = ctx.parent; c != null && c != decl; c = c.parent) {
            chain.add(c);
        }
        Collections.reverse(chain);
        for (BuildCtx f : chain) {
            if (f.closureSlot == 0) {
                snapshotBoxedVar(f, oid, path);
            }
        }
    }

    /**
     * True if {@code oid} is a boxed variable visible from {@code ctx}:
     * checks {@code ctx} and all enclosing function contexts. Needed
     * because a funclit's own {@code boxed} set is empty — its loop lives
     * in an enclosing function.
     */
    private static boolean isBoxedTransitive(BuildCtx ctx, int oid) {
        for (BuildCtx c = ctx; c != null; c = c.parent) {
            if (c.boxed.contains(oid)) {
                return true;
            }
        }
        return false;
    }

    /**
     * The per-iteration cell slot for a for-header variable,
     * or -1. Walks the context chain: the declaring loop may belong to an
     * enclosing function.
     */
    private static int headerIterSlot(BuildCtx ctx, int oid) {
        for (BuildCtx c = ctx; c != null; c = c.parent) {
            Integer s = c.boxIterSlots.get(oid);
            if (s != null) {
                return s;
            }
        }
        return -1;
    }

    /**
     * The current phase of the loop that declared the for-header variable
     * {@code oid} (loopcap): {@link #LP_INIT}/{@link #LP_COND}/{@link #LP_POST}/
     * {@link #LP_BODY}, or -1 if the declaring loop is not on the stack
     * (cannot happen for in-scope references; treated as outer).
     *
     * <p>A factory-level stack (not a boolean) so nested loops resolve
     * correctly: an inner loop's init referencing an outer loop's header
     * variable sees the outer loop's BODY phase, not the inner INIT.
     */
    private int headerPhase(int oid) {
        Integer loopId = headerLoopIds.get(oid);
        if (loopId == null) {
            return -1;
        }
        for (LoopPhase lp : loopStack) {
            if (lp.loopId() == loopId) {
                return lp.phase();
            }
        }
        return -1;
    }

    /**
     * Builds a read of a variable, capture-aware (M5, WS F): a depth-0
     * resolution is a {@link VarReadNode}; a deeper one is a
     * {@link CapReadNode} walking the {@code $closure} chain. Boxed (loopcap)
     * variables read their {@link graalgo.values.GoBox} cell instead.
     */
    private GoExprNode readVar(GoKind kind, BuildCtx ctx, int oid, String path) {
        // M7: package-level variables live in the globals frame, shared by
        // every function; they are never captured and never boxed.
        // P3-M9 (WS D): ... except when their address is taken — then the
        // globals-frame slot holds a GoBox cell shared with every &x.
        Integer gslot = globalSlots.get(oid);
        if (gslot != null) {
            return addrTaken.contains(oid)
                    ? new GlobalBoxReadNode(kind, globalsFrame, gslot)
                    : new GlobalReadNode(kind, globalsFrame, gslot);
        }
        if (isBoxedTransitive(ctx, oid)) {
            return readBoxedVar(kind, ctx, oid, path);
        }
        VarResolution r = resolveVar(ctx, oid, path);
        if (r.depth == 0) {
            return new VarReadNode(kind, r.slot);
        }
        ctx.hasCapture = true;
        return new CapReadNode(kind, r.depth - 1, r.slot, ctx.closureSlot);
    }

    /**
     * Reads a boxed variable. A snapshot slot (this funclit's capture)
     * is read directly; an ancestor's variable is snapshotted on first
     * reference. A local for-header variable reads its per-iteration cell
     * in every phase except LP_INIT (the init statement defines the carry
     * cell — gc's {@code init : s/z/z'} substitution — and the
     * per-iteration cell does not exist yet): init-seed, post, cond, and
     * body all operate on the one per-iteration variable (gc's
     * {@code loopvar} desugar runs the post on the fresh per-iteration
     * cell at the top of the iteration, before the condition). The carry
     * cell is otherwise only touched by the declaration, the refresh
     * prologue, and the write-back. Every other boxed variable reads its
     * single cell slot.
     */
    private GoExprNode readBoxedVar(GoKind kind, BuildCtx ctx, int oid, String path) {
        Integer snap = ctx.boxSnapSlots.get(oid);
        if (snap != null) {
            return new BoxReadNode(kind, snap);
        }
        BoxResolution b = resolveBoxed(ctx, oid, path);
        if (b.depth > 0 || b.isSnapshot) {
            return new BoxReadNode(kind, snapshotBoxedVar(ctx, oid, path));
        }
        int iter = headerIterSlot(ctx, oid);
        if (iter >= 0 && headerPhase(oid) != LP_INIT) {
            return new BoxReadNode(kind, iter);
        }
        return new BoxReadNode(kind, b.slot);
    }

    /**
     * The frame slot whose {@link graalgo.values.GoBox} cell receives a
     * write to the boxed variable {@code oid}. A funclit-captured
     * variable writes its local snapshot cell; a local for-header variable
     * writes its per-iteration cell in every phase except LP_INIT, where
     * the declaration seeds the carry cell instead (gc's
     * {@code init : s/z/z'}). The post runs on the per-iteration cell —
     * at the top of the iteration, before the condition — and the
     * write-back propagates to the carry.
     */
    /**
     * The frame slot holding the {@link graalgo.values.GoBox} cell of the
     * boxed variable {@code oid}, as seen from {@code ctx}: a funclit
     * snapshot slot for captured variables, the per-iteration cell for a
     * for-header variable (the carry cell in the init phase), else the
     * declaring slot. Writes ({@link BoxWriteNode}), reads, and address-of
     * ({@link BoxAddrNode}) must all resolve to this same cell.
     */
    private int boxWriteSlot(BuildCtx ctx, int oid, String path) {
        Integer snap = ctx.boxSnapSlots.get(oid);
        if (snap != null) {
            return snap;
        }
        BoxResolution b = resolveBoxed(ctx, oid, path);
        if (b.depth > 0 || b.isSnapshot) {
            return snapshotBoxedVar(ctx, oid, path);
        }
        int iter = headerIterSlot(ctx, oid);
        if (iter >= 0 && headerPhase(oid) != LP_INIT) {
            return iter;
        }
        return b.slot;
    }

    // ------------------------------------------------------------------
    // loopcap pre-scan (factory side; no exporter/wire change).
    // ------------------------------------------------------------------

    /** The "kind" string of a JSON AST node, or null. */
    private static String nodeKind(Json.Value v) {
        if (v instanceof Json.Object o && o.opt("kind") instanceof Json.Str s) {
            return s.value();
        }
        return null;
    }

    /**
     * Collects object ids from "vardecl" nodes' "objs" arrays in the subtree.
     * Does not descend into function literals: their declarations belong to
     * the literal's own function, not the loop.
     */
    private void collectVardeclObjs(Json.Value v, Set<Integer> out, String path) {
        if (v instanceof Json.Object o) {
            String kind = nodeKind(o);
            if ("funclit".equals(kind)) {
                return;
            }
            if ("vardecl".equals(kind)) {
                Json.Array objs = o.object("fields", path).array("objs", path);
                for (Json.Value item : objs.items()) {
                    if (item instanceof Json.Num n) {
                        out.add((int) n.asLong(path));
                    }
                }
            }
            if (o.opt("fields") instanceof Json.Object fields) {
                for (Json.Value m : fields.members().values()) {
                    collectVardeclObjs(m, out, path);
                }
            }
            return;
        }
        if (v instanceof Json.Array a) {
            for (Json.Value item : a.items()) {
                collectVardeclObjs(item, out, path);
            }
        }
    }

    /** Collects every ident "obj" id in the subtree. */
    private void collectIdents(Json.Value v, Set<Integer> out, String path) {
        if (v instanceof Json.Object o) {
            if ("ident".equals(nodeKind(o))) {
                Json.Value obj = o.object("fields", path).opt("obj");
                if (obj instanceof Json.Num n) {
                    out.add((int) n.asLong(path));
                }
            }
            if (o.opt("fields") instanceof Json.Object fields) {
                for (Json.Value m : fields.members().values()) {
                    collectIdents(m, out, path);
                }
            }
            return;
        }
        if (v instanceof Json.Array a) {
            for (Json.Value item : a.items()) {
                collectIdents(item, out, path);
            }
        }
    }

    /**
     * Collects ident "obj" ids referenced inside function-literal subtrees
     * (nested literals included: a reference in a doubly-nested literal
     * still captures the loop variable, via the $closure chain).
     */
    private void collectFunclitRefs(Json.Value v, Set<Integer> out, String path) {
        if (v instanceof Json.Object o) {
            if ("funclit".equals(nodeKind(o))) {
                collectIdents(o, out, path);
                return;
            }
            if (o.opt("fields") instanceof Json.Object fields) {
                for (Json.Value m : fields.members().values()) {
                    collectFunclitRefs(m, out, path);
                }
            }
            return;
        }
        if (v instanceof Json.Array a) {
            for (Json.Value item : a.items()) {
                collectFunclitRefs(item, out, path);
            }
        }
    }

    /**
     * Collects the object ids of variables whose address is taken in the
     * subtree (P3-M9, WS D): every {@code addrof} wire node whose operand
     * is an {@code ident} contributes the ident's {@code obj} id. The
     * implicit address-of the exporter inserts for pointer-receiver
     * method calls uses the same {@code addrof} kind, so those variables
     * are covered too. Does not descend into function literals: their
     * bodies belong to the literal's own function, which the
     * program-level scan reaches separately (object ids are
     * program-global, so one set serves every function).
     */
    private void collectAddrTaken(Json.Value v, Set<Integer> out, String path) {
        if (v instanceof Json.Object o) {
            String kind = nodeKind(o);
            // P3-M9 (WS D): descend into function literals too — a &x in a
            // closure body takes the address of the (outer) variable just
            // the same; missing it would leave the variable unboxed while
            // the pointer expects its cell.
            if ("addrof".equals(kind)
                    && o.opt("fields") instanceof Json.Object fields
                    && fields.opt("x") instanceof Json.Object x
                    && "ident".equals(nodeKind(x))
                    && x.opt("fields") instanceof Json.Object xf
                    && xf.opt("obj") instanceof Json.Num n) {
                out.add((int) n.asLong(path));
            }
            if (o.opt("fields") instanceof Json.Object flds) {
                for (Json.Value m : flds.members().values()) {
                    collectAddrTaken(m, out, path);
                }
            }
            return;
        }
        if (v instanceof Json.Array a) {
            for (Json.Value item : a.items()) {
                collectAddrTaken(item, out, path);
            }
        }
    }

    /**
     * Pre-scan for a "for" loop: loop-declared variables (for-init and
     * body vardecls) referenced by a function literal anywhere in the loop
     * are boxed (per-iteration cells). For-init variables additionally get
     * the carry/per-iteration treatment: recorded in
     * {@link #headerLoopIds}. Returns the boxed for-header oids in
     * declaration order (for per-iteration slot allocation and the
     * refresh/write-back nodes).
     */
    private List<Integer> prescanForBoxes(Json.Object node, Json.Object f, String path, BuildCtx ctx, int loopId) {
        Set<Integer> initDecls = new LinkedHashSet<>();
        if (f.opt("init") instanceof Json.Object initObj) {
            collectVardeclObjs(initObj, initDecls, path);
        }
        Set<Integer> bodyDecls = new HashSet<>();
        collectVardeclObjs(f.object("body", path), bodyDecls, path);
        Set<Integer> captured = new HashSet<>();
        collectFunclitRefs(node, captured, path);
        List<Integer> headerOids = new ArrayList<>();
        for (int oid : initDecls) {
            // P3-M9 (WS D): an address-taken header variable needs the
            // per-iteration treatment too (each iteration's &i must address
            // that iteration's cell).
            if (captured.contains(oid) || addrTaken.contains(oid)) {
                ctx.boxed.add(oid);
                headerLoopIds.put(oid, loopId);
                headerOids.add(oid);
            }
        }
        for (int oid : bodyDecls) {
            if (captured.contains(oid) || addrTaken.contains(oid)) {
                ctx.boxed.add(oid);
            }
        }
        return headerOids;
    }

    /**
     * Pre-scan for a "range" loop (loopcap): the := -declared key/value variables
     * and body vardecls referenced by a function literal are boxed. Range
     * variables are single-cell (per-iteration); no header treatment.
     */
    private void prescanRangeBoxes(Json.Object node, Json.Object f, String path, BuildCtx ctx) {
        Set<Integer> rangeDecls = new HashSet<>();
        if (f.bool("define", path)) {
            if (f.opt("key") instanceof Json.Num kn) {
                rangeDecls.add((int) kn.asLong(path));
            }
            if (f.opt("value") instanceof Json.Num vn) {
                rangeDecls.add((int) vn.asLong(path));
            }
        }
        Set<Integer> bodyDecls = new HashSet<>();
        collectVardeclObjs(f.object("body", path), bodyDecls, path);
        Set<Integer> captured = new HashSet<>();
        collectFunclitRefs(node, captured, path);
        for (int oid : rangeDecls) {
            // P3-M9 (WS D): address-taken range variables get the
            // per-iteration cell (each iteration's &v addresses that
            // iteration's variable).
            if (captured.contains(oid) || addrTaken.contains(oid)) {
                ctx.boxed.add(oid);
            }
        }
        for (int oid : bodyDecls) {
            if (captured.contains(oid) || addrTaken.contains(oid)) {
                ctx.boxed.add(oid);
            }
        }
    }

    private GoExprNode buildIdent(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "ident");
        String path = "ident.fields";
        int oid = f.intValue("obj", path);
        // M6 (WS H): an ident denoting a named function or a monomorphized
        // instantiation evaluates to the shared GoFunc value.
        String objKind = objectKind(ctx, oid);
        if ("func".equals(objKind) || "inst".equals(objKind)) {
            return new FuncConstNode(funcTable, oid);
        }
        GoKind kind = kindOf(ctx, f.intValue("type", path), path);
        return readVar(kind, ctx, oid, "ident");
    }

    /**
     * The object-table kind of an object id ("var", "func", "inst").
     * Unknown ids fail fast.
     */
    private String objectKind(BuildCtx ctx, int oid) {
        Json.Value e = oid >= 0 && oid < ctx.objects.size() ? ctx.objects.get(oid) : null;
        if (!(e instanceof Json.Object o)) {
            throw new FrontendException("object id " + oid + " out of range (WS H)");
        }
        return o.string("kind", "objects[" + oid + "]");
    }

    private GoExprNode buildBasicLit(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "basiclit");
        String path = "basiclit.fields";
        if (!f.has("const")) {
            throw new FrontendException(
                    "non-constant basiclit is not supported in v1 (WS D: typed constant evaluation)");
        }
        GoKind kind = kindOf(ctx, f.intValue("type", path), path);
        String c = f.string("const", path);
        GoExprNode lit = switch (kind) {
            case INT8, INT16, INT32, INT64, UINT8, UINT16, UINT32, UINT64 ->
                ConstNode.ofInt(kind, parseGoIntConst(c, path));
            case UNTYPED_INT ->
                ConstNode.ofUntypedInt(parseGoIntConst(c, path), c);
            case FLOAT32, FLOAT64 ->
                ConstNode.ofFloat(kind, f.intValue("f32bits", path), f.longInt("f64bits", path));
            case UNTYPED_FLOAT ->
                ConstNode.ofUntypedFloat(f.intValue("f32bits", path), f.longInt("f64bits", path));
            case BOOL -> ConstNode.ofBool("true".equals(c));
            case UNTYPED_BOOL -> ConstNode.ofUntypedBool("true".equals(c));
            case STRING -> ConstNode.ofString(new GoString(HexFormat.of().parseHex(f.string("sbytes", path))));
            case UNTYPED_STRING ->
                ConstNode.ofUntypedString(new GoString(HexFormat.of().parseHex(f.string("sbytes", path))));
            // M7: complex constants (from folded complex() calls or literals).
            case COMPLEX64 -> ConstNode.ofObject(kind,
                    new graalgo.values.GoComplex32(
                            Float.intBitsToFloat(f.intValue("creal32", path)),
                            Float.intBitsToFloat(f.intValue("cimag32", path))));
            case COMPLEX128, UNTYPED_COMPLEX -> ConstNode.ofObject(kind,
                    new graalgo.values.GoComplex64(
                            Double.longBitsToDouble(f.longInt("creal64", path)),
                            Double.longBitsToDouble(f.longInt("cimag64", path))));
            default -> throw new FrontendException(
                    path + ": constant of kind " + kind + " is not supported (WS C/D)");
        };
        // P3-M7R2: carry the type-table id so interface boxing can resolve
        // the exact untyped name ("untyped int" vs "untyped rune") for the
        // default-type descriptor.
        lit.setTypeId(f.intValue("type", path));
        return lit;
    }

    /**
     * Parses a go/constant ExactString for an integer constant, returning the
     * low 64 bits. go/types guarantees typed constants fit their type, so
     * extension there is exact; untyped constants wider than 64 bits (e.g.
     * {@code 1<<70}) are only ever observed via {@code exactDecimal()},
     * never via this long.
     */
    static long parseGoIntConst(String s, String path) {
        String t = s;
        boolean neg = false;
        if (t.startsWith("-")) {
            neg = true;
            t = t.substring(1);
        } else if (t.startsWith("+")) {
            t = t.substring(1);
        }
        int radix = 10;
        if (t.startsWith("0x") || t.startsWith("0X")) {
            radix = 16;
            t = t.substring(2);
        } else if (t.startsWith("0o") || t.startsWith("0O")) {
            radix = 8;
            t = t.substring(2);
        } else if (t.startsWith("0b") || t.startsWith("0B")) {
            radix = 2;
            t = t.substring(2);
        }
        java.math.BigInteger mag;
        try {
            mag = new java.math.BigInteger(t, radix);
        } catch (NumberFormatException e) {
            throw new FrontendException(path + ": cannot parse integer constant \"" + s + "\" (WS D)");
        }
        // longValue(): the low 64 bits (two's complement for negatives).
        return neg ? mag.negate().longValue() : mag.longValue();
    }

    private GoExprNode buildBinary(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "binary");
        String path = "binary.fields";
        BinaryOpNode.Op op = BinaryOpNode.Op.fromToken(f.string("op", path));
        GoKind kind = kindOf(ctx, f.intValue("type", path), path);
        GoExprNode x = buildExpr(f.object("x", path), ctx);
        GoExprNode y = buildExpr(f.object("y", path), ctx);
        if (op == BinaryOpNode.Op.EQ || op == BinaryOpNode.Op.NE) {
            // M7: `v == nil` / `v != nil` for a nillable concrete kind
            // (slice, map, pointer, func, chan, unsafe.Pointer). The
            // exporter types every nil literal as the empty interface, so
            // without this the comparison would attempt an interface
            // conversion; instead the null carrier is tested directly.
            // (INTERFACE operands keep the M5 interface-equality path.)
            boolean xNil = "nil".equals(nodeKind(f.object("x", path)));
            boolean yNil = "nil".equals(nodeKind(f.object("y", path)));
            if (xNil != yNil) {
                GoExprNode other = xNil ? y : x;
                switch (other.kind()) {
                    case SLICE, MAP, POINTER, FUNC, CHAN, UNSAFE_POINTER ->
                        { return new NilCheckNode(other, op == BinaryOpNode.Op.NE); }
                    default -> { /* fall through to the general path */ }
                }
            }
        }
        if (op.isComparison() || op.isLogical()) {
            // Operand kind: both sides agree, or one side is an untyped
            // constant (transparent). The result kind is BOOL.
            GoKind ok = x.kind().isUntyped() ? y.kind() : x.kind();
            // WS E: if either operand is an interface, use interface equality.
            // Box the concrete side (if any) with the type-id-carrying form
            // so BinaryOpNode's M5 interface-equality path compares the
            // boxed dynamic values. The P3-M8b fix only handled interface on
            // the left (ok == INTERFACE); concrete on the left (e.g.
            // myErr == errIface) left ok as the concrete kind, and the
            // type-id-free maybeConvert below threw "interface conversion
            // needs type ids" on the interface operand.
            if (x.kind() == GoKind.INTERFACE || y.kind() == GoKind.INTERFACE) {
                if (x.kind() != y.kind()) {
                    GoExprNode iface = x.kind() == GoKind.INTERFACE ? x : y;
                    GoExprNode conc = x.kind() == GoKind.INTERFACE ? y : x;
                    int ifaceId = iface.typeId();
                    if (ifaceId < 0) {
                        throw new FrontendException(path
                                + ": cannot box concrete operand to interface without a type-table id (WS E)");
                    }
                    conc = maybeConvert(conc, GoKind.INTERFACE, ifaceId, ctx, path);
                    if (x.kind() == GoKind.INTERFACE) {
                        y = conc;
                    } else {
                        x = conc;
                    }
                }
                ok = GoKind.INTERFACE;
            }
            return new BinaryOpNode(op, GoKind.BOOL, ok, maybeConvert(x, ok, path), maybeConvert(y, ok, path));
        }
        if (op.isShift()) {
            // Go spec: the result type is the left operand's type (the
            // exporter already sets binary.type to it). The count is read
            // as a long; numeric nodes extend it correctly, so no wrapper.
            return new BinaryOpNode(op, kind, maybeConvert(x, kind, path), y);
        }
        return new BinaryOpNode(op, kind, maybeConvert(x, kind, path), maybeConvert(y, kind, path));
    }

    private GoExprNode buildUnary(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "unary");
        String path = "unary.fields";
        UnaryOpNode.Op op = UnaryOpNode.Op.fromToken(f.string("op", path));
        GoKind kind = kindOf(ctx, f.intValue("type", path), path);
        if (op == UnaryOpNode.Op.LOGNOT) {
            // P3-M7: go/types types every comparison as untyped bool (spec:
            // "comparison operators ... yield an untyped boolean value"),
            // so !(a && b) carries UNTYPED_BOOL on the wire while its
            // operand materializes to BOOL (comparisons and &&/|| build
            // BOOL nodes). The result of ! is always a bool at runtime —
            // mirror buildBinary's logical-op handling and use BOOL.
            kind = GoKind.BOOL;
        }
        return new UnaryOpNode(op, kind, maybeConvert(buildExpr(f.object("x", path), ctx), kind, path));
    }

    private GoExprNode buildParen(Json.Object node, BuildCtx ctx) {
        return buildExpr(fieldsOf(node, "paren").object("x", "paren.fields"), ctx);
    }

    /**
     * Pointer dereference read {@code *p} (P3-M9, WS D). The wire node
     * carries the pointer expression ("x") and the element type ("type",
     * i.e. the type of {@code *p}).
     */
    private GoExprNode buildDeref(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "deref");
        String path = "deref.fields";
        GoExprNode ptr = buildExpr(f.object("x", path), ctx);
        GoKind elemKind = kindOf(ctx, f.intValue("type", path), path);
        return new DerefNode(elemKind, ptr);
    }

    /**
     * Builds an implicit address-of {@code &x} for method receivers
     * (P3-M9, WS D). When a pointer-receiver method is called on an
     * addressable value, the exporter wraps the receiver in an "addrof"
     * node. The factory evaluates the inner expression and wraps the
     * result in a GoPointer.
     */
    private GoExprNode buildAddrOf(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "addrof");
        String path = "addrof.fields";
        // P3-M9 (WS D): &arr[i] emits array/index fields (element pointer).
        // The array may be a GoSlice (for slices) or a Java array (for arrays).
        if (f.opt("array") != null) {
            GoExprNode arrNode = buildExpr(f.object("array", path), ctx);
            GoExprNode idxNode = buildExpr(f.object("index", path), ctx);
            return new GoExprNode(GoKind.POINTER) {
                @Override
                public Object executeObject(VirtualFrame frame) {
                    Object arr = arrNode.executeObject(frame);
                    // P3-M9: box the index per its kind (it may be Int/Long/
                    // etc., not just Object — e.g. &buf[i/j] or &buf[i+1]).
                    // Boxing.box reads via the node's own kind; the old
                    // idxNode.executeObject assumed Object slot kind and
                    // threw for every integer index expression.
                    int idx = (int) Boxing.toLong(Boxing.box(idxNode.kind(), idxNode, frame),
                            idxNode.kind());
                    // Unwrap GoSlice to the underlying Java array.
                    Object javaArray = arr;
                    if (arr instanceof graalgo.values.GoSlice gs) {
                        javaArray = gs.array();
                        idx += gs.offset();
                    } else if (arr instanceof graalgo.values.GoPointer gp) {
                        // P3-M9: &ptr[i] where ptr is *[N]T — dereference
                        // the pointer to the underlying Java array (Go:
                        // implicit (*ptr)[i]). A nil pointer is gc's
                        // nil-dereference panic.
                        if (gp.isNil()) {
                            graalgo.runtime.Panic.throwPanic(
                                    graalgo.runtime.GoErrors.nilDereference(), null, null, this);
                        }
                        javaArray = gp.referent();
                        // Defensive: the referent may itself be a GoSlice.
                        if (javaArray instanceof graalgo.values.GoSlice gs2) {
                            idx += gs2.offset();
                            javaArray = gs2.array();
                        }
                    }
                    return new GoPointer(javaArray, idx);
                }
            };
        }
        Json.Object operand = f.object("x", path);
        String opKind = nodeKind(operand);
        // P3-M9 (WS D): &*p is p itself — no dereference happens (so
        // &*nil is the nil pointer, not a panic).
        if ("deref".equals(opKind)) {
            return buildExpr(operand.object("fields", path).object("x", path), ctx);
        }
        // P3-M9 (WS D): &x for a variable x must address the variable's
        // storage (its GoBox cell), not a snapshot of its current value —
        // otherwise *p = v is invisible through x and vice versa.
        if ("ident".equals(opKind)) {
            Json.Object xf = operand.object("fields", path);
            int oid = xf.intValue("obj", path);
            if (oid >= 0) {
                Integer gslot = globalSlots.get(oid);
                if (gslot != null) {
                    if (addrTaken.contains(oid)) {
                        return new GlobalBoxAddrNode(globalsFrame, gslot);
                    }
                } else if (isBoxedTransitive(ctx, oid)) {
                    return new BoxAddrNode(boxWriteSlot(ctx, oid, path));
                }
            }
            // Unboxed variable (its address is never taken per the
            // program scan — defensive): fall through to the snapshot.
        }
        // P3-M9 (WS D): &s.f addresses the field's storage (GoFieldCell).
        if ("selector".equals(opKind)) {
            GoExprNode fieldAddr = buildFieldAddr(operand, ctx, path);
            if (fieldAddr != null) {
                return fieldAddr;
            }
        }
        GoExprNode x = buildExpr(operand, ctx);
        GoKind xKind = x.kind();
        return new GoExprNode(GoKind.POINTER) {
            @Override
            public Object executeObject(VirtualFrame frame) {
                // P3-M9: box the value per its kind (x may be Int/Long/etc.,
                // not just Object). Boxing.box handles the kind correctly.
                Object val = Boxing.box(xKind, x, frame);
                return new GoPointer(val);
            }
        };
    }

    /**
     * Builds {@code &s.f} (P3-M9, WS D): flattens the selector chain's
     * promotion paths (innermost first) into one field-index path and
     * addresses the final field's storage via {@link FieldAddrNode}.
     * Returns null (caller falls back to the snapshot form) when the
     * base is not a struct / pointer-to-struct expression.
     */
    private GoExprNode buildFieldAddr(Json.Object selector, BuildCtx ctx, String path) {
        List<Json.Object> chain = new ArrayList<>();
        Json.Object base = selector;
        while ("selector".equals(nodeKind(base))) {
            chain.add(base);
            base = base.object("fields", path).object("x", path);
        }
        Json.Value baseType = base.object("fields", path).opt("type");
        if (!(baseType instanceof Json.Num btn)) {
            return null;
        }
        GoKind baseKind = kindOf(ctx, (int) btn.asLong(path), path);
        if (baseKind != GoKind.STRUCT && baseKind != GoKind.POINTER) {
            return null;
        }
        List<Integer> hops = new ArrayList<>();
        for (int i = chain.size() - 1; i >= 0; i--) {
            Json.Object sf = chain.get(i).object("fields", path);
            for (Json.Value v : sf.array("selpath", path).items()) {
                hops.add((int) ((Json.Num) v).asLong(path));
            }
        }
        if (hops.isEmpty()) {
            return null;
        }
        GoKind fieldKind = kindOf(ctx,
                selector.object("fields", path).intValue("type", path), path);
        int[] hopArr = hops.stream().mapToInt(Integer::intValue).toArray();
        return new FieldAddrNode(buildExpr(base, ctx), hopArr, fieldKind);
    }

    /**
     * Builds a method value {@code x.M} (frontend {@code methodvalue} node,
     * P3-M9 WS H): evaluates the receiver once and wraps the resolved
     * method in a GoFunc with the receiver bound.
     */
    private GoExprNode buildMethodValue(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "methodvalue");
        String path = "methodvalue.fields";
        int objId = f.intValue("obj", path);
        GoExprNode recv = buildExpr(f.object("recv", path), ctx);
        return new MethodValueNode(recv, funcTable, objId);
    }

    /**
     * Builds the {@code sliceoverlap} wire node for
     * crypto/internal/fips140/alias.AnyOverlap and InexactOverlap
     * (P3-M9, unsafe patterns). Fields: "x", "y" (the two []byte slices),
     * "inexact" (bool).
     */
    private GoExprNode buildSliceOverlap(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "sliceoverlap");
        String path = "sliceoverlap.fields";
        GoExprNode x = buildExpr(f.object("x", path), ctx);
        GoExprNode y = buildExpr(f.object("y", path), ctx);
        boolean inexact = f.bool("inexact", path);
        return new SliceOverlapNode(x, y, inexact);
    }

    /** Builds a Go conversion {@code T(x)} (frontend {@code convert} node). */
    private GoExprNode buildConvert(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "convert");
        String path = "convert.fields";
        int toId = f.intValue("to", path);
        GoKind to = kindOf(ctx, toId, path);
        GoExprNode x = buildExpr(f.object("x", path), ctx);
        // P3-M3 (WS D): T(nil) is the nil value of T — retype the NilNode
        // to the target kind instead of passing the exporter's
        // empty-interface-typed nil through. Otherwise a typed nil loses
        // its type: (*Buf)(nil) would box as the nil interface rather
        // than a *Buf-typed nil, and dispatch would panic instead of
        // running the method with a nil receiver (if07).
        if (x instanceof NilNode && isNillable(to)) {
            GoStructDescriptor nilSd =
                    to == GoKind.STRUCT ? structDescriptorFor(ctx, toId, path) : null;
            NilNode n = new NilNode(to, nilSd, zeroTemplateOrNull(to, toId, ctx, path));
            n.setTypeId(toId);
            return n;
        }
        // M7: string <-> slice conversions need the element kind, from the
        // "from" (source type) field.
        GoKind sliceElemKind = null;
        GoKind from = x.kind();
        if ((to == GoKind.SLICE && (from == GoKind.STRING || from == GoKind.UNTYPED_STRING))
                || (from == GoKind.SLICE && to == GoKind.STRING)) {
            Json.Value fromVal = f.opt("from");
            if (fromVal instanceof Json.Num n) {
                int fromId = (int) n.longValue();
                int sliceId = to == GoKind.SLICE ? toId : fromId;
                sliceElemKind = builtinElemKind(ctx, sliceId, path, "convert");
            }
        }
        return maybeConvert(x, to, toId, ctx, path, sliceElemKind);
    }

    /**
     * Call dispatch (M5, WS F).
     *
     * <p>Handles: the {@code panic}/{@code recover} builtins (minimal WS-F
     * wiring; worker-G's GG-0020 surface absorbs these nodes), the
     * {@code runtime.Goexit()} selector (via the M5 import overlay),
     * function-literal calls ({@code func(){...}()}), calls to
     * func-typed variables, calls to named functions and monomorphized
     * instantiations (M6, WS H — {@code ident} with a func/inst object id),
     * and method calls (M6, WS H — {@code methodref} fun target).
     * Every other builtin or selector call is a scope error naming WS G
     * (intrinsics) or WS D (methods beyond the M6 subset).
     */
    private GoExprNode buildCall(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "call");
        String path = "call.fields";
        // runtime.Goexit(): the exporter marks it with goexit:true.
        if (f.has("goexit")) {
            return new GoexitCallNode();
        }
        // P2-M4 (GC interop, ADR-0002 D4): runtime.SetFinalizer/KeepAlive/GC
        // via the synthetic runtime import overlay. The exporter marks the
        // call node with gcinterop:<Name> (no "fun" key, like goexit); the
        // call delegates to graalgo.runtime.GcInterop (Phase-1 stub,
        // Phase-2 real).
        if (f.has("gcinterop")) {
            String which = f.string("gcinterop", path);
            Json.Array gciArgs = f.array("args", path);
            GoExprNode[] gciArgNodes = new GoExprNode[gciArgs.size()];
            for (int i = 0; i < gciArgNodes.length; i++) {
                gciArgNodes[i] = buildExpr(asObject(gciArgs.get(i), path + ".args[" + i + "]"), ctx);
            }
            switch (which) {
                case "SetFinalizer":
                    return new SetFinalizerCallNode(gciArgNodes);
                case "KeepAlive":
                    return new KeepAliveCallNode(gciArgNodes);
                case "GC":
                    return new GcCallNode();
                default:
                    throw new IllegalArgumentException(
                            "unknown gcinterop marker: " + which);
            }
        }
        String builtin = f.has("builtin") ? f.string("builtin", path) : null;
        if (builtin != null) {
            return buildBuiltinCall(builtin, f, ctx, path);
        }
        Json.Object fun = f.object("fun", path);
        String funKind = fun.string("kind", path + ".fun");
        Json.Array argNodes = f.array("args", path);
        GoExprNode[] args = new GoExprNode[argNodes.size()];
        for (int i = 0; i < args.length; i++) {
            args[i] = buildExpr(asObject(argNodes.get(i), path + ".args[" + i + "]"), ctx);
        }
        if ("funclit".equals(funKind)) {
            GoExprNode lit = buildFuncLit(fun, ctx);
            args = adaptArgsForInterfaceParams(args, funclitParamTypeIds(fun, path), ctx, path);
            return new FuncValueCallNode(lit, args, callResultKind(node, ctx, path),
                    false, ctx.funcObjId);
        }
        if ("methodref".equals(funKind)) {
            // M6 (WS H): method call — the receiver was exported as the
            // methodref's recv; it evaluates first and is passed as the
            // first argument (the method's params[0]).
            Json.Object ff = fun.object("fields", path + ".fun");
            int oid = ff.intValue("obj", path + ".fun.fields");
            GoExprNode recv = buildExpr(ff.object("recv", path + ".fun.fields"), ctx);
            GoExprNode[] allArgs = new GoExprNode[args.length + 1];
            allArgs[0] = recv;
            System.arraycopy(args, 0, allArgs, 1, args.length);
            // P1-1 (M6 remediation): the exporter wires the method's
            // signature type id on the methodref node (receiver as
            // params[0]); adapt concrete args passed to interface params
            // exactly as for direct calls. Previously the signature was
            // not on the wire and the root's fallback inferred a bogus
            // descriptor from the Java carrier type, so a later type
            // switch silently took the wrong branch.
            Json.Value sig = ff.opt("type");
            List<Integer> paramTypes = (sig instanceof Json.Null || sig == null) ? null
                    : signatureParamTypeIds(ctx, ff.intValue("type", path + ".fun.fields"), path);
            allArgs = adaptArgsForInterfaceParams(allArgs, paramTypes, ctx, path);
            return new FuncValueCallNode(new FuncConstNode(funcTable, oid), allArgs,
                    callResultKind(node, ctx, path), true, ctx.funcObjId);
        }
        if ("ident".equals(funKind)) {
            // A func-typed variable (or a builtin ident without the builtin
            // marker — the latter is a scope error naming WS G).
            Json.Object ff = fun.object("fields", path + ".fun");
            if (ff.has("builtin")) {
                String name = ff.string("builtin", path + ".fun.fields");
                throw new FrontendException(path + ": call to builtin \"" + name + "\""
                        + " is not implemented in v1 (WS G intrinsic surface)");
            }
            GoExprNode fn = buildIdent(fun, ctx);
            if (fn.kind() != GoKind.FUNC) {
                throw new FrontendException(path + ": call to non-function value of kind "
                        + fn.kind() + " (WS D)");
            }
            Json.Value sig = ff.opt("type");
            List<Integer> paramTypes = (sig instanceof Json.Null || sig == null) ? null
                    : signatureParamTypeIds(ctx, ff.intValue("type", path + ".fun.fields"), path);
            args = adaptArgsForInterfaceParams(args, paramTypes, ctx, path);
            return new FuncValueCallNode(fn, args, callResultKind(node, ctx, path),
                    false, ctx.funcObjId);
        }
        if ("call".equals(funKind)) {
            // P3-M8b: call to a computed func value, e.g.
            // reflect.Swapper(s)(0, 2). The exporter (P3-M8, export.go)
            // emits the inner call as the fun operand; build it and
            // require a func result (mirrors the ident branch above).
            GoExprNode fn = buildExpr(fun, ctx);
            if (fn.kind() != GoKind.FUNC) {
                throw new FrontendException(path + ": call to non-function value of kind "
                        + fn.kind() + " (WS D)");
            }
            Json.Object ff = fun.object("fields", path + ".fun");
            Json.Value sig = ff.opt("type");
            List<Integer> paramTypes = (sig instanceof Json.Null || sig == null) ? null
                    : signatureParamTypeIds(ctx, ff.intValue("type", path + ".fun.fields"), path);
            args = adaptArgsForInterfaceParams(args, paramTypes, ctx, path);
            return new FuncValueCallNode(fn, args, callResultKind(node, ctx, path),
                    false, ctx.funcObjId);
        }
        if ("selector".equals(funKind)) {
            // Function-typed field call, e.g. c.generate(r) where generate
            // is a func-typed struct field (P3-M9: nistCurve function
            // fields). The exporter emits the field access as a selector
            // node; build it and require a func result (mirrors the ident
            // branch above).
            GoExprNode fn = buildExpr(fun, ctx);
            if (fn.kind() != GoKind.FUNC) {
                throw new FrontendException(path + ": call to non-function value of kind "
                        + fn.kind() + " (WS D)");
            }
            Json.Object ff = fun.object("fields", path + ".fun");
            Json.Value sig = ff.opt("type");
            List<Integer> paramTypes = (sig instanceof Json.Null || sig == null) ? null
                    : signatureParamTypeIds(ctx, ff.intValue("type", path + ".fun.fields"), path);
            args = adaptArgsForInterfaceParams(args, paramTypes, ctx, path);
            return new FuncValueCallNode(fn, args, callResultKind(node, ctx, path),
                    false, ctx.funcObjId);
        }
        if ("index".equals(funKind)) {
            // Call to a func value selected out of a composite, e.g.
            // ms.less[k](p, q) where less is []func(...) (Gate-1 sort
            // divergence: the multiSorter pattern), or m.handlers[name](args).
            // The exporter emits the index expression as the fun operand;
            // build it and require a func result (mirrors the call/selector
            // branches above).
            GoExprNode fn = buildExpr(fun, ctx);
            if (fn.kind() != GoKind.FUNC) {
                throw new FrontendException(path + ": call to non-function value of kind "
                        + fn.kind() + " (WS D)");
            }
            Json.Object ff = fun.object("fields", path + ".fun");
            Json.Value sig = ff.opt("type");
            List<Integer> paramTypes = (sig instanceof Json.Null || sig == null) ? null
                    : signatureParamTypeIds(ctx, ff.intValue("type", path + ".fun.fields"), path);
            args = adaptArgsForInterfaceParams(args, paramTypes, ctx, path);
            return new FuncValueCallNode(fn, args, callResultKind(node, ctx, path),
                    false, ctx.funcObjId);
        }
        throw new FrontendException(path + ": call to " + funKind + " is not supported"
                + " in frontend/v1 scope (WS D: methods and qualified calls)");
    }

    /**
     * Implicit concrete-to-interface boxing at call sites (M6, WS H).
     *
     * <p>When a callee parameter has interface type and the argument is a
     * concrete value, the argument is boxed with its exact static-type
     * descriptor. This replaces the old {@code GoFuncRootNode.wrapInterface}
     * inference, which derived the descriptor from the Java carrier type —
     * int8/int16/uint8/uint16/uint32 all looked like int32 (a wrong type
     * switch match, not just a miss), and composite values never matched.
     * The root's {@code wrapInterface} fallback stays only for exotic call
     * shapes whose signature is not on the wire (none remain: ident, funclit
     * and methodref all carry it).
     *
     * @param paramTypeIds the callee's parameter type ids, or null when
     *                     unknown (no adaptation)
     */
    private GoExprNode[] adaptArgsForInterfaceParams(GoExprNode[] args,
            List<Integer> paramTypeIds, BuildCtx ctx, String path) {
        if (paramTypeIds == null) {
            return args;
        }
        for (int i = 0; i < args.length && i < paramTypeIds.size(); i++) {
            if (args[i].kind() == GoKind.INTERFACE) {
                continue; // already an interface value
            }
            // P3-M3 (WS D): named interface types (e.g. main.Writer) box
            // too — the old kind=="interface" check silently skipped them.
            if (!isInterfaceType(ctx, paramTypeIds.get(i), path)) {
                continue;
            }
            // P3-M3 remediation (R1 P1-2): a generic instantiation passed
            // to a non-empty interface parameter fails fast with the named
            // P3-M3 scope error (its wire method table is empty by design).
            checkInstantiationBoxedAsInterface(ctx, args[i].typeId(), paramTypeIds.get(i), path);
            args[i] = boxInterface(args[i], ctx, path);
        }
        return args;
    }

    /** Parameter type ids from a wire signature entry; null when not a (non-variadic) signature. */
    private static List<Integer> signatureParamTypeIds(BuildCtx ctx, int sigId, String path) {
        Json.Object entry = ctx.types.entry(sigId, path);
        if (!"signature".equals(entry.string("kind", path))) {
            return null;
        }
        Json.Object detail = entry.object("detail", path);
        if (detail.has("variadic") && detail.bool("variadic", path)) {
            return null; // v1 has no variadic adaptation
        }
        Json.Array params = detail.array("params", path);
        List<Integer> out = new ArrayList<>(params.size());
        for (int i = 0; i < params.size(); i++) {
            Json.Value pv = params.get(i);
            if (!(pv instanceof Json.Object po)) {
                return null;
            }
            out.add(po.intValue("type", path));
        }
        return out;
    }

    /** Parameter type ids from a wire funclit node; null when unreadable. */
    private static List<Integer> funclitParamTypeIds(Json.Object fun, String path) {
        Json.Object ff = fun.object("fields", path + ".fun");
        Json.Array params = ff.array("params", path + ".fun.fields");
        List<Integer> out = new ArrayList<>(params.size());
        for (int i = 0; i < params.size(); i++) {
            Json.Value pv = params.get(i);
            if (!(pv instanceof Json.Object po)) {
                return null;
            }
            Json.Object pf = po.object("fields", path + ".fun.fields.params[" + i + "]");
            out.add(pf.intValue("type", path + ".fun.fields.params[" + i + "]"));
        }
        return out;
    }

    /**
     * P3-M10 (fix-fmt-funcs): walks a slice/array element type down to the
     * scalar, collecting the kind/name chain below the first element level
     * (e.g. [2][2][2]int -&gt; kinds [ARRAY, INT32]). Factored out of
     * {@link #buildFmtCall} so pointer-to-composite args (e.g. {@code *[]byte}
     * scan destinations) get the same chains. Terminates: each step moves
     * to a strictly smaller type, and go/types rejects recursive array
     * types before export.
     */
    private record ChainWalk(java.util.List<GoKind> kinds, java.util.List<String> names) {}

    private ChainWalk walkElemChain(int startElemId, BuildCtx ctx, String apath) {
        java.util.List<GoKind> kc = new java.util.ArrayList<>();
        java.util.List<String> nc = new java.util.ArrayList<>();
        int curId = startElemId;
        while (true) {
            Json.Object curEntry = underlyingEntry(ctx, curId, apath);
            String ck = curEntry.string("kind", apath);
            if (!"slice".equals(ck) && !"array".equals(ck)) {
                break;
            }
            Json.Object curDetail = curEntry.object("detail", apath);
            int nextId = curDetail.intValue("elem", apath);
            kc.add(kindOf(ctx, nextId, apath));
            nc.add(GoTypeStrings.of(ctx, nextId));
            curId = nextId;
        }
        return new ChainWalk(java.util.List.copyOf(kc), java.util.List.copyOf(nc));
    }

    /**
     * P3-M10 (fix-fmt-funcs): walks a map element type down through nested
     * maps, collecting the (key, elem) kinds per level (e.g.
     * map[string]map[string]int -&gt; keyChain=[STRING], elemChain=[INT]).
     * Factored out of {@link #buildFmtCall}; same termination argument as
     * {@link #walkElemChain}.
     */
    private record MapChainWalk(java.util.List<GoKind> keyKinds, java.util.List<String> keyNames,
            java.util.List<GoKind> elemKinds, java.util.List<String> elemNames) {}

    private MapChainWalk walkMapChain(int startElemId, BuildCtx ctx, String apath) {
        java.util.List<GoKind> kkc = new java.util.ArrayList<>();
        java.util.List<String> knc = new java.util.ArrayList<>();
        java.util.List<GoKind> kec = new java.util.ArrayList<>();
        java.util.List<String> nec = new java.util.ArrayList<>();
        int curId = startElemId;
        while (true) {
            Json.Object curEntry = underlyingEntry(ctx, curId, apath);
            String ck = curEntry.string("kind", apath);
            if (!"map".equals(ck)) {
                break;
            }
            Json.Object curDetail = curEntry.object("detail", apath);
            int ckId = curDetail.intValue("key", apath);
            int ceId = curDetail.intValue("elem", apath);
            kkc.add(kindOf(ctx, ckId, apath));
            knc.add(GoTypeStrings.of(ctx, ckId));
            kec.add(kindOf(ctx, ceId, apath));
            nec.add(GoTypeStrings.of(ctx, ceId));
            curId = ceId;
        }
        // bug-041: if the terminal type (after nested maps) is an array or
        // slice, append its element chain so map values that are arrays or
        // slices carry their element kinds for nested fmt printing.
        // e.g. map[int][2]int: the walk above breaks immediately (elem is
        // array, not map), yielding an empty chain; the nested [2]int value
        // then gets a null elemKind which printSlice defaults to UINT8,
        // crashing with [J->[B on the actual long[] backing (int is 64-bit).
        // Appending walkElemChain gives [INT64], the kind-driven element type.
        Json.Object termEntry = underlyingEntry(ctx, curId, apath);
        String termKind = termEntry.string("kind", apath);
        if ("array".equals(termKind) || "slice".equals(termKind)) {
            ChainWalk ew = walkElemChain(curId, ctx, apath);
            kec.addAll(ew.kinds());
            nec.addAll(ew.names());
        }
        return new MapChainWalk(java.util.List.copyOf(kkc), java.util.List.copyOf(knc),
                java.util.List.copyOf(kec), java.util.List.copyOf(nec));
    }

    /**
     * A call to one of the D4 {@code fmt} stub functions (M6, WS H — the
     * Phase-1 import overlay; the wire {@code fmtcall} node is specified in
     * docs/frontend-v1.md §6). Static per-argument type metadata comes from
     * the wire type table; values are read from the children at runtime.
     */
    private GoExprNode buildFmtCall(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "fmtcall");
        String path = "fmtcall.fields";
        String func = f.string("func", path);
        Json.Array argNodes = f.array("args", path);
        int n = argNodes.size();
        GoExprNode[] args = new GoExprNode[n];
        GoKind[] kinds = new GoKind[n];
        String[] typeNames = new String[n];
        GoKind[] elemKinds = new GoKind[n];
        String[] elemTypeNames = new String[n];
        // M7-remediation (R2 P0-2): element kind/name chains for nested
        // array/slice printing at arbitrary depth. The old single
        // elemElemKind field structurally capped nesting at depth 2;
        // walk the type table down to the scalar instead.
        java.util.List<GoKind>[] elemKindChains = new java.util.List[n];
        java.util.List<String>[] elemTypeNameChains = new java.util.List[n];
        GoKind[] keyKinds = new GoKind[n];
        String[] keyTypeNames = new String[n];
        // M8 closeout: key kind/name chains for nested maps, parallel to the
        // elem chains for nested arrays (see Fmt.Arg).
        java.util.List<GoKind>[] keyKindChains = new java.util.List[n];
        java.util.List<String>[] keyTypeNameChains = new java.util.List[n];
        for (int i = 0; i < n; i++) {
            String apath = path + ".args[" + i + "]";
            args[i] = buildExpr(asObject(argNodes.get(i), apath), ctx);
            int typeId = args[i].typeId();
            if (typeId < 0) {
                throw new FrontendException(apath + ": fmt argument without a type id (WS H)");
            }
            kinds[i] = kindOf(ctx, typeId, apath);
            typeNames[i] = GoTypeStrings.of(ctx, typeId);
            Json.Object entry = underlyingEntry(ctx, typeId, apath);
            String k = entry.string("kind", apath);
            Json.Object detail = entry.object("detail", apath);
            if ("slice".equals(k) || "array".equals(k)) {
                int elemId = detail.intValue("elem", apath);
                elemKinds[i] = kindOf(ctx, elemId, apath);
                elemTypeNames[i] = GoTypeStrings.of(ctx, elemId);
                // M7-remediation (R2 P0-2): element kind/name chains for nested
                // printing at arbitrary depth (see walkElemChain).
                ChainWalk w = walkElemChain(elemId, ctx, apath);
                elemKindChains[i] = w.kinds();
                elemTypeNameChains[i] = w.names();
                keyKindChains[i] = java.util.List.of();
                keyTypeNameChains[i] = java.util.List.of();
            } else if ("map".equals(k)) {
                int keyId = detail.intValue("key", apath);
                int elemId = detail.intValue("elem", apath);
                keyKinds[i] = kindOf(ctx, keyId, apath);
                keyTypeNames[i] = GoTypeStrings.of(ctx, keyId);
                elemKinds[i] = kindOf(ctx, elemId, apath);
                elemTypeNames[i] = GoTypeStrings.of(ctx, elemId);
                // M8 closeout: key kind/name chains for nested maps, parallel to the
                // elem chains for nested arrays (see walkMapChain).
                MapChainWalk w = walkMapChain(elemId, ctx, apath);
                keyKindChains[i] = w.keyKinds();
                keyTypeNameChains[i] = w.keyNames();
                elemKindChains[i] = w.elemKinds();
                elemTypeNameChains[i] = w.elemNames();
            } else if ("pointer".equals(k)) {
                // M8 (fuzzer find): %v of a pointer to struct/array/slice/map
                // prints &value (Go's printValue Pointer case), not the erased
                // address. Thread the referent kind so fmtPointer takes the
                // &-path; other referents keep the erased 0x0.
                int elemId = detail.intValue("elem", apath);
                elemKinds[i] = kindOf(ctx, elemId, apath);
                elemTypeNames[i] = GoTypeStrings.of(ctx, elemId);
                // P3-M10 (fix-fmt-funcs): pointer-to-composite (e.g. *[]byte
                // scan destinations): populate the chains by walking the
                // referent like the slice/map branches above; other
                // referents keep empty chains.
                Json.Object elemEntry = underlyingEntry(ctx, elemId, apath);
                String ek = elemEntry.string("kind", apath);
                if ("slice".equals(ek) || "array".equals(ek)) {
                    ChainWalk w = walkElemChain(elemId, ctx, apath);
                    elemKindChains[i] = w.kinds();
                    elemTypeNameChains[i] = w.names();
                    keyKindChains[i] = java.util.List.of();
                    keyTypeNameChains[i] = java.util.List.of();
                } else if ("map".equals(ek)) {
                    MapChainWalk w = walkMapChain(elemId, ctx, apath);
                    keyKindChains[i] = w.keyKinds();
                    keyTypeNameChains[i] = w.keyNames();
                    elemKindChains[i] = w.elemKinds();
                    elemTypeNameChains[i] = w.elemNames();
                } else {
                    elemKindChains[i] = java.util.List.of();
                    elemTypeNameChains[i] = java.util.List.of();
                    keyKindChains[i] = java.util.List.of();
                    keyTypeNameChains[i] = java.util.List.of();
                }
            } else {
                elemKindChains[i] = java.util.List.of();
                elemTypeNameChains[i] = java.util.List.of();
                keyKindChains[i] = java.util.List.of();
                keyTypeNameChains[i] = java.util.List.of();
            }
        }
        // The wire "type" is null for the Print variants (results discarded,
        // statement position only); a single id for the Sprint variants.
        GoKind resultKind;
        Json.Value t = f.opt("type");
        if (t instanceof Json.Null || t == null) {
            resultKind = GoKind.INTERFACE;
        } else {
            resultKind = kindOf(ctx, f.intValue("type", path), path);
        }
        return new FmtCallNode(func, args, kinds, typeNames, elemKinds, elemTypeNames,
                elemKindChains, elemTypeNameChains, keyKinds, keyTypeNames,
                keyKindChains, keyTypeNameChains, resultKind);
    }

    /**
     * Builtin call wiring (M6, WS H — integration of the WS G intrinsic
     * surface): {@code panic}/{@code recover} (M5, WS F), {@code print}/
     * {@code println}, {@code len}, {@code cap}, {@code make}, {@code new},
     * {@code append}, {@code copy}, {@code delete}, {@code complex},
     * {@code real}, {@code imag}. Remaining builtins ({@code clear},
     * {@code min}, {@code max}) still name WS G.
     *
     * <p>The wire "type" carries the call's result type id (null for void
     * calls like print/println); element types come from the type table via
     * {@link #underlyingEntry}. By the time the factory sees a generic
     * function body, the exporter has monomorphized it (D2), so all types
     * here are concrete.
     */
    private GoExprNode buildBuiltinCall(String builtin, Json.Object f, BuildCtx ctx, String path) {
        Json.Array argNodes = f.array("args", path);
        GoExprNode[] args = new GoExprNode[argNodes.size()];
        for (int i = 0; i < args.length; i++) {
            args[i] = buildExpr(asObject(argNodes.get(i), path + ".args[" + i + "]"), ctx);
        }
        return switch (builtin) {
            case "panic" -> {
                if (args.length != 1) {
                    throw new FrontendException(path + ": panic takes exactly one argument");
                }
                GoExprNode v = args[0];
                int typeId = v.typeId();
                GoKind k = kindOf(ctx, typeId, path);
                boolean isIface = k == GoKind.INTERFACE;
                graalgo.values.GoTypeDescriptor desc =
                        isIface ? null : typeDescriptorFor(ctx, typeId, path);
                String typeName = null;
                if (!isIface && (GoTypeStrings.isNamedBasic(ctx, typeId)
                        || k == GoKind.SLICE || k == GoKind.MAP || k == GoKind.ARRAY
                        || k == GoKind.STRUCT || k == GoKind.POINTER || k == GoKind.CHAN)) {
                    typeName = GoTypeStrings.of(ctx, typeId);
                }
                yield new PanicBuiltinNode(v, desc, typeName, isIface);
            }
            case "recover" -> {
                if (args.length != 0) {
                    throw new FrontendException(path + ": recover takes no arguments");
                }
                yield new RecoverBuiltinNode();
            }
            case "print" -> new PrintNode(false, args);
            case "println" -> new PrintNode(true, args);
            case "len" -> {
                requireArgs(path, builtin, args, 1);
                yield new LenNode(args[0]);
            }
            case "cap" -> {
                requireArgs(path, builtin, args, 1);
                yield new CapNode(args[0]);
            }
            case "make" -> buildMakeBuiltin(args, f, ctx, path);
            case "new" -> buildNewBuiltin(args, f, ctx, path);
            case "append" -> buildAppendBuiltin(args, f, ctx, path);
            case "copy" -> buildCopyBuiltin(args, f, ctx, path);
            case "delete" -> {
                requireArgs(path, builtin, args, 2);
                GoExprNode delKey = args[1];
                int dtid = args[0].typeId();
                if (dtid >= 0 && kindOf(ctx, dtid, path) == GoKind.MAP) {
                    // P3-M9 (bug-028): the key must be converted to the
                    // map's key type (interface boxing for map[any]).
                    delKey = convertMapKey(delKey, dtid, ctx, path);
                }
                yield new DeleteNode(args[0], delKey);
            }
            case "clear" -> {
                // M9 acceptance: clear/min/max are outside the frozen v1
                // builtin surface (see docs/freeze-stub.md "Builtin nodes").
                // The exporter must fail fast with a clean WS G scope error
                // instead of emitting a program that crashes at runtime.
                throw new FrontendException(path + ": builtin \"" + builtin
                        + "\" is not supported in frontend/v1 scope (WS G)");
            }
            case "min", "max" -> {
                // M9 acceptance: see "clear" above — outside v1 scope.
                throw new FrontendException(path + ": builtin \"" + builtin
                        + "\" is not supported in frontend/v1 scope (WS G)");
            }
            case "complex" -> {
                requireArgs(path, builtin, args, 2);
                yield new ComplexNode(ComplexNode.Op.COMPLEX, builtinResultKind(f, ctx, path),
                        args[0], args[1]);
            }
            case "real" -> {
                requireArgs(path, builtin, args, 1);
                yield new ComplexNode(ComplexNode.Op.REAL, builtinResultKind(f, ctx, path),
                        args[0], null);
            }
            case "imag" -> {
                requireArgs(path, builtin, args, 1);
                yield new ComplexNode(ComplexNode.Op.IMAG, builtinResultKind(f, ctx, path),
                        args[0], null);
            }
            default -> throw new FrontendException("call to builtin \"" + builtin + "\""
                    + " is not implemented in NodeFactory v1 (WS G intrinsic surface)");
        };
    }

    /** Arg-count guard for builtin calls (gc already type-checked; this is a wire-shape check). */
    private static void requireArgs(String path, String builtin, GoExprNode[] args, int n) {
        if (args.length != n) {
            throw new FrontendException(path + ": builtin \"" + builtin + "\" takes " + n
                    + " argument(s), wire has " + args.length);
        }
    }

    /** The wire "type" of a builtin call as a GoKind (null only for void calls). */
    private static GoKind builtinResultKind(Json.Object f, BuildCtx ctx, String path) {
        Json.Value t = f.opt("type");
        if (t instanceof Json.Null || t == null) {
            throw new FrontendException(path + ": builtin call without a result type id");
        }
        return kindOf(ctx, f.intValue("type", path), path);
    }

    /** Element kind of the type id's underlying composite (slice/array/map/pointer). */
    private static GoKind builtinElemKind(BuildCtx ctx, int typeId, String path, String what) {
        Json.Object entry = underlyingEntry(ctx, typeId, path);
        Json.Object detail = entry.object("detail", path);
        if (!detail.has("elem")) {
            throw new FrontendException(path + ": " + what + " on non-composite type");
        }
        return kindOf(ctx, detail.intValue("elem", path), path);
    }

    /** {@code make}: slice and map forms (chan is Phase 2). */
    private GoExprNode buildMakeBuiltin(GoExprNode[] args, Json.Object f, BuildCtx ctx,
            String path) {
        Json.Value t = f.opt("type");
        if (t instanceof Json.Null || t == null) {
            throw new FrontendException(path + ": make without a result type id");
        }
        int typeId = f.intValue("type", path);
        Json.Object entry = underlyingEntry(ctx, typeId, path);
        String k = entry.string("kind", path);
        switch (k) {
            case "slice" -> {
                if (args.length < 1 || args.length > 2) {
                    throw new FrontendException(path + ": make(slice) takes 1-2 length args");
                }
                GoKind elemKind = builtinElemKind(ctx, typeId, path, "make");
                // P3-M9: struct elements need their descriptor so makeslice
                // can zero-initialize (Go's make([]T, n) zero-initializes;
                // null slots NPE on s[i].field access).
                graalgo.values.GoStructDescriptor elemDesc = null;
                // NEW-P0-3: the recursive zero template — makeslice
                // instantiates one fresh copy per slot, so nested
                // sync-primitive cells (e.g. atomic.Pointer fields) get
                // valid, distinct backings. The descriptor-only zero
                // leaves them null.
                Object elemZeroTemplate = null;
                if (elemKind == GoKind.STRUCT) {
                    int elemTid = entry.object("detail", path).intValue("elem", path);
                    elemDesc = structDescriptorFor(ctx, elemTid, path);
                    elemZeroTemplate = zeroTemplateFor(GoKind.STRUCT, elemTid, ctx, path);
                }
                return new MakeNode(elemKind, args[0], args.length == 2 ? args[1] : null, elemDesc,
                        elemZeroTemplate);
            }
            case "map" -> {
                // The size hint is semantically invisible (GoMap grows
                // dynamically), so it is not threaded through.
                return new MakeNode(-1);
            }
            default -> throw new FrontendException(path + ": make of " + k
                    + " is not supported in frontend/v1 scope (Phase 2: channels)");
        }
    }

    /** {@code new(T)}: allocate the zero value of T, return a pointer. */
    private GoExprNode buildNewBuiltin(GoExprNode[] args, Json.Object f, BuildCtx ctx,
            String path) {
        // M7: new takes a type, not a value; the exporter sends zero args
        // and the result (pointer) type id carries T.
        requireArgs(path, "new", args, 0);
        Json.Value t = f.opt("type");
        if (t instanceof Json.Null || t == null) {
            throw new FrontendException(path + ": new without a result type id");
        }
        int typeId = f.intValue("type", path);
        Json.Object entry = underlyingEntry(ctx, typeId, path);
        if (!"pointer".equals(entry.string("kind", path))) {
            throw new FrontendException(path + ": new result is not a pointer type");
        }
        int elemId = entry.object("detail", path).intValue("elem", path);
        GoKind elemKind = kindOf(ctx, elemId, path);
        GoStructDescriptor desc =
                elemKind == GoKind.STRUCT ? structDescriptorFor(ctx, elemId, path) : null;
        // P3-M8b: new([N]T) needs the array length and element kind.
        if (elemKind == GoKind.ARRAY) {
            // P3-M9: Go zero-initializes nested arrays recursively —
            // new([N][M]T) must allocate the inner [M]T arrays, not leave
            // null elements (else arr[i][j] faults). Collect the length at
            // each nesting level plus the ultimate (non-array) element kind.
            java.util.List<Integer> lens = new java.util.ArrayList<>();
            int eid = elemId;
            GoKind ek = elemKind;
            for (int steps = 0; ; steps++) {
                if (steps > MAX_TYPE_CHAIN_STEPS) {
                    throw new FrontendException(path + ": nested array type exceeds "
                            + MAX_TYPE_CHAIN_STEPS + " levels (WS E)");
                }
                Json.Object arrayEntry = underlyingEntry(ctx, eid, path);
                Json.Object detail = arrayEntry.object("detail", path);
                lens.add(detail.intValue("len", path));
                eid = detail.intValue("elem", path);
                ek = kindOf(ctx, eid, path);
                if (ek != GoKind.ARRAY) {
                    break;
                }
            }
            int[] arrayLens = lens.stream().mapToInt(Integer::intValue).toArray();
            // P3-M9: if the ultimate element is a struct, new([N]S) must
            // zero-initialize each element (Go zero value), not leave nulls.
            GoStructDescriptor ultimateDesc =
                    ek == GoKind.STRUCT ? structDescriptorFor(ctx, eid, path) : null;
            // P3-M9: the referent zero comes from the recursive zero
            // template (nested struct/array fields materialized), not
            // the shallow descriptor-only GoStruct.zero.
            return new NewNode(elemKind, desc, arrayLens, ek, ultimateDesc,
                    zeroTemplateOrNull(elemKind, elemId, ctx, path));
        }
        return new NewNode(elemKind, desc, null, null, null,
                zeroTemplateOrNull(elemKind, elemId, ctx, path));
    }

    /** {@code append(s, elems...)}: the result type id gives the element kind. */
    private GoExprNode buildAppendBuiltin(GoExprNode[] args, Json.Object f, BuildCtx ctx,
            String path) {
        if (args.length < 1) {
            throw new FrontendException(path + ": append takes at least one argument");
        }
        Json.Value t = f.opt("type");
        if (t instanceof Json.Null || t == null) {
            throw new FrontendException(path + ": append without a result type id");
        }
        int typeId = f.intValue("type", path);
        Json.Object entry = underlyingEntry(ctx, typeId, path);
        Json.Object detail = entry.object("detail", path);
        if (!detail.has("elem")) {
            throw new FrontendException(path + ": append on non-composite type");
        }
        int elemId = detail.intValue("elem", path);
        GoKind elemKind = kindOf(ctx, elemId, path);
        // M7: append(s, x...) with a string x spreads the string's bytes.
        boolean ellipsis = false;
        Json.Value e = f.opt("ellipsis");
        if (e instanceof Json.Bool b) {
            ellipsis = b.value();
        }
        GoExprNode[] elems = Arrays.copyOfRange(args, 1, args.length);
        if (elemKind == GoKind.INTERFACE && !ellipsis) {
            // P3-M8b: appending a concrete value to an interface-element
            // slice must box it (cf. buildComplit's element maybeConvert).
            // Without this, e.g. append([]DirEntry{}, &dirEntry{}) stores
            // the raw GoPointer and interface dispatch fails at runtime.
            for (int i = 0; i < elems.length; i++) {
                elems[i] = maybeConvert(elems[i], GoKind.INTERFACE, elemId, ctx,
                        path + ".args[" + (i + 1) + "]");
            }
        }
        // P3-M4 (WS C): struct elements need their ABI size for the
        // growslice math (elemSizeBytes(STRUCT) is 0 by design).
        // P3-M9: struct elements also need their descriptor so growslice
        // can zero-initialize new slots.
        // P3 bug-042: array elements need their ABI size too —
        // elemSizeBytes(ARRAY) is 0, which made growslice take its
        // zero-size fast path (skipping the old-element copy) for
        // non-zero-size array types, losing appended-to elements.
        long elemSizeBytes = 0;
        graalgo.values.GoStructDescriptor elemDesc = null;
        // NEW-P0-3: recursive zero template for struct elements (same
        // init rule as make() — fresh per new backing slot).
        Object elemZeroTemplate = null;
        if (elemKind == GoKind.STRUCT) {
            elemDesc = structDescriptorFor(ctx, elemId, path);
            elemSizeBytes = graalgo.runtime.Intrinsics.structSizeBytes(elemDesc);
            elemZeroTemplate = zeroTemplateFor(GoKind.STRUCT, elemId, ctx, path);
        } else if (elemKind == GoKind.ARRAY) {
            elemSizeBytes = arraySizeBytes(ctx, elemId, path);
        }
        return new AppendNode(args[0], elemKind, elems, ellipsis, elemSizeBytes, elemDesc,
                elemZeroTemplate);
    }

    /**
     * {@code copy(dst, src)}: slice-to-slice, or {@code copy(dst []byte, src string)}
     * (P3-M8b: string sources are supported by converting the string to []byte).
     */
    private GoExprNode buildCopyBuiltin(GoExprNode[] args, Json.Object f, BuildCtx ctx,
            String path) {
        requireArgs(path, "copy", args, 2);
        GoExprNode src = args[1];
        // P3-M8b: copy(dst []byte, src string) — convert the string to []byte.
        // Go spec: the string's bytes are copied; dst must be []byte.
        if (src.kind() == GoKind.STRING || src.kind() == GoKind.UNTYPED_STRING) {
            src = new ConvertNode(src.kind(), GoKind.SLICE, src, GoKind.UINT8);
        }
        GoKind elemKind = builtinElemKind(ctx, args[0].typeId(), path, "copy");
        return new CopyNode(args[0], src, elemKind);
    }

    /** Result kind of a call node (from the wire "type"; void → null kind handling). */
    private GoKind callResultKind(Json.Object node, BuildCtx ctx, String path) {
        Json.Object f = fieldsOf(node, "call");
        Json.Value t = f.opt("type");
        if (t instanceof Json.Null || t == null) {
            // Void call: use a dummy kind; FuncValueCallNode is only used in
            // expression position via ExprStmtNode which ignores the value.
            return GoKind.INTERFACE;
        }
        // The wire "type" for calls is a type id (or null).
        return kindOf(ctx, f.intValue("type", path), path);
    }

    /**
     * {@code defer} statement (M5, WS F).
     *
     * <p>The wire is kind:"defer" with fields.call = the call node. The call
     * is either a function-literal call ({@code defer func(){...}()}), a
     * call to a func-typed variable, or a builtin call ({@code println},
     * {@code panic}, {@code recover}). Arguments are evaluated at defer
     * time (in DeferNode); the function value too.
     */
    private GoStmtNode buildDefer(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "defer");
        String path = "defer.fields";
        Json.Object call = f.object("call", path);
        String callKind = call.string("kind", path + ".call");
        // Deferred fmt overlay call (M7, e.g. `defer fmt.Println(x)`): build
        // the FmtCallNode; DeferNode evaluates its arguments now and replays
        // the formatting at return/unwind time.
        if ("fmtcall".equals(callKind)) {
            FmtCallNode fmtCall = (FmtCallNode) buildFmtCall(call, ctx);
            fmtCall.setSourceSection(sectionFor(node, "defer"));
            return new DeferNode(fmtCall);
        }
        Json.Object cf = call.object("fields", path + ".call");
        String cpath = path + ".call.fields";
        // Deferred builtin: pass the builtin name through; DeferNode builds
        // the DeferredCall.Builtin at defer-execution time.
        if (cf.has("builtin")) {
            String builtin = cf.string("builtin", cpath);
            if (!("println".equals(builtin) || "print".equals(builtin)
                    || "panic".equals(builtin) || "recover".equals(builtin))) {
                throw new FrontendException(cpath + ": cannot defer builtin \"" + builtin
                        + "\" in v1 (WS G intrinsic surface)");
            }
            Json.Array argNodes = cf.array("args", cpath);
            GoExprNode[] args = new GoExprNode[argNodes.size()];
            GoKind[] kinds = new GoKind[args.length];
            for (int i = 0; i < args.length; i++) {
                args[i] = buildExpr(asObject(argNodes.get(i), cpath + ".args[" + i + "]"), ctx);
                kinds[i] = args[i].kind();
            }
            DeferNode n = new DeferNode(null, args, builtin, kinds);
            n.setSourceSection(sectionFor(node, "defer"));
            return n;
        }
        Json.Object fun = cf.object("fun", cpath);
        String funKind = fun.string("kind", cpath + ".fun");
        Json.Array argNodes = cf.array("args", cpath);
        GoExprNode[] args = new GoExprNode[argNodes.size()];
        GoKind[] kinds = new GoKind[args.length];
        for (int i = 0; i < args.length; i++) {
            args[i] = buildExpr(asObject(argNodes.get(i), cpath + ".args[" + i + "]"), ctx);
            kinds[i] = args[i].kind();
        }
        GoExprNode fn;
        if ("funclit".equals(funKind)) {
            fn = buildFuncLit(fun, ctx);
        } else if ("ident".equals(funKind)) {
            fn = buildIdent(fun, ctx);
            if (fn.kind() != GoKind.FUNC) {
                throw new FrontendException(cpath + ": cannot defer non-function of kind " + fn.kind());
            }
        } else if ("methodref".equals(funKind)) {
            // P3-M9: deferred method call, e.g. defer p.Close(). The
            // receiver is in the fun's fields; prepend it to args (the
            // method's params[0]), mirroring buildCall's methodref branch.
            Json.Object ff = fun.object("fields", cpath + ".fun");
            int oid = ff.intValue("obj", cpath + ".fun.fields");
            GoExprNode recv = buildExpr(ff.object("recv", cpath + ".fun.fields"), ctx);
            GoExprNode[] allArgs = new GoExprNode[args.length + 1];
            allArgs[0] = recv;
            System.arraycopy(args, 0, allArgs, 1, args.length);
            GoKind[] allKinds = new GoKind[kinds.length + 1];
            allKinds[0] = recv.kind();
            System.arraycopy(kinds, 0, allKinds, 1, kinds.length);
            fn = new FuncConstNode(funcTable, oid);
            args = allArgs;
            kinds = allKinds;
        } else {
            throw new FrontendException(cpath + ": cannot defer call to " + funKind + " (WS D)");
        }
        DeferNode n = new DeferNode(fn, args, null, kinds);
        n.setSourceSection(sectionFor(node, "defer"));
        n.setEnclosingFunc(ctx.funcObjId);
        return n;
    }

    /**
     * Function literal (M5, WS F).
     *
     * <p>Builds the body in a child {@link BuildCtx} (parent = enclosing
     * ctx) with its own frame descriptor: slot 0 is the {@code $closure}
     * slot, then params, then locals. Captured variables resolve via the
     * parent chain to {@link CapReadNode}/{@link CapWriteNode}. The
     * {@link GoFuncRootNode} is wrapped in a {@link RootCallTarget}; the
     * {@link FuncLitNode} captures the materialized parent frame iff the
     * body references an ancestor variable.
     */
    private GoExprNode buildFuncLit(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "funclit");
        String path = "funclit.fields";
        FrameDescriptor.Builder frames = FrameDescriptor.newBuilder().defaultValueIllegal();
        BuildCtx fctx = new BuildCtx(source, frames, ctx.types, ctx.objects, ctx);
        fctx.funcName = ctx.funcName + ".func" + (++funclitCounter);
        fctx.funcObjId = ctx.funcObjId;
        // Slot 0: the $closure slot (parent frame or null).
        int closureSlot = frames.addSlot(FrameSlotKind.Object, "$closure", null);
        fctx.closureSlot = closureSlot;
        // Params.
        Json.Array params = f.array("params", path);
        int[] paramSlots = new int[params.size()];
        GoKind[] paramKinds = new GoKind[params.size()];
        List<GoStmtNode> prologue = new ArrayList<>();
        for (int i = 0; i < params.size(); i++) {
            Json.Object p = asObject(params.get(i), path + ".params[" + i + "]");
            Json.Object pf = p.object("fields", path + ".params[" + i + "]");
            String pname = pf.string("name", path);
            int oid = pf.intValue("obj", path);
            int typeId = pf.intValue("type", path);
            GoKind kind = kindOf(ctx, typeId, path);
            int slot = allocParamSlot(pname, oid, typeId, path + ".params[" + i + "]", fctx,
                    prologue);
            paramSlots[i] = slot;
            paramKinds[i] = kind;
        }
        // Results: at most one in v1 (multi-value returns are WS D).
        Json.Array results = f.array("results", path);
        if (results.size() > 1) {
            throw new FrontendException(path + ": function literals with multiple results"
                    + " are not supported in v1 (WS D)");
        }
        if (results.size() == 1) {
            Json.Object r = asObject(results.get(0), path + ".results[0]");
            Json.Object rf = r.object("fields", path + ".results[0]");
            String rname = rf.string("name", path);
            int roid = rf.intValue("obj", path);
            int rtypeId = rf.intValue("type", path);
            GoKind rkind = kindOf(ctx, rtypeId, path);
            // P3-M9 (WS D): an address-taken named result lives in a
            // GoBox cell in the result slot.
            int rslot = addrTaken.contains(roid)
                    ? allocBoxSlot(rname.isEmpty() ? "result" : rname, roid, rtypeId,
                            path + ".results[0]", fctx)
                    : allocSlot(rname.isEmpty() ? "result" : rname, roid, rtypeId,
                            path + ".results[0]", fctx);
            if (addrTaken.contains(roid)) {
                fctx.resultBoxed = true;
            }
            fctx.resultSlot = rslot;
            fctx.resultKind = rkind;
            fctx.resultTypeId = rtypeId;
        }
        prescanAddrTakenDecls(f.object("body", path), fctx, path);
        GoStmtNode body = buildBlock(f.object("body", path), fctx);
        // Entry: box address-taken parameters, then zero the result slot
        // (Go semantics: results start at zero values, so a bare `return`
        // yields zeros; a boxed result's zero goes into its cell).
        List<GoStmtNode> entryStmts = new ArrayList<>(prologue);
        if (fctx.resultSlot >= 0) {
            GoExprNode zeroVal = zeroNode(fctx.resultKind, resultTypeId(f, ctx, path),
                    fctx, path);
            entryStmts.add(fctx.resultBoxed
                    ? new BoxDeclNode(fctx.resultSlot, fctx.resultKind, zeroVal)
                    : new AssignNode(fctx.resultSlot, fctx.resultKind, zeroVal));
        }
        entryStmts.add(body);
        GoStmtNode entry = entryStmts.size() == 1 ? body
                : new BlockNode(entryStmts.toArray(new GoStmtNode[0]));
        GoFuncRootNode root;
        {
            // M6: drain the boxSpec triples into the flat array for
            // FuncLitNode, and pass the snapshot slots (in boxSpec order)
            // to the root so it can install the GoFunc's cells on entry.
            int[] flat = null;
            int[] snapSlots = null;
            if (!fctx.boxSpec.isEmpty()) {
                flat = new int[fctx.boxSpec.size() * 3];
                snapSlots = new int[fctx.boxSpec.size()];
                for (int i = 0; i < fctx.boxSpec.size(); i++) {
                    int[] t = fctx.boxSpec.get(i);
                    flat[i * 3] = t[0];
                    flat[i * 3 + 1] = t[1];
                    flat[i * 3 + 2] = t[2];
                    snapSlots[i] = t[2];
                }
            }
            root = new GoFuncRootNode(frames.build(), entry, closureSlot,
                    paramSlots, paramKinds, fctx.resultSlot, fctx.resultKind, fctx.funcName,
                    snapSlots, fctx.resultBoxed);
            root.setSourceSection(sectionFor(node, "funclit"));
            RootCallTarget target = root.getCallTarget();
            // Does the body capture? The hasCapture flag is set when CapReadNode
            // or CapWriteNode is created (more reliable than AST scanning, which
            // can miss nodes nested in switches).
            boolean captures = fctx.hasCapture || capturesAncestor(entry);
            FuncLitNode n = flat == null
                    ? new FuncLitNode(target, captures)
                    : new FuncLitNode(target, captures, flat, closureSlot);
            n.setSourceSection(sectionFor(node, "funclit"));
            // M7: the exporter wires the literal's signature type id so the
            // value can be boxed into an interface (boxInterface needs the
            // dynamic type descriptor). Desugared funclits omit it; they
            // are never boxed.
            Json.Value tid = f.opt("type");
            if (tid instanceof Json.Num num) {
                n.setTypeId((int) num.asLong(path));
            }
            return n;
        }
    }

    /** Type id of the single result (for zeroNode); -1 if none. */
    private int resultTypeId(Json.Object f, BuildCtx ctx, String path) {
        Json.Array results = f.array("results", path);
        if (results.size() == 0) {
            return -1;
        }
        Json.Object rf = asObject(results.get(0), path + ".results[0]")
                .object("fields", path + ".results[0]");
        return rf.intValue("type", path);
    }

    /** True if the built subtree contains a CapRead/CapWrite (captured variable). */
    private static boolean capturesAncestor(Node n) {
        if (n instanceof CapReadNode || n instanceof CapWriteNode) {
            return true;
        }
        for (Node child : n.getChildren()) {
            if (capturesAncestor(child)) {
                return true;
            }
        }
        return false;
    }

    // ------------------------------------------------------------------
    // Statements.
    // ------------------------------------------------------------------

    /** The type-table id of the variable an identifier's {@code obj} id denotes. */
    private int objTypeOf(BuildCtx ctx, Json.Object identFields, String path) {
        int oid = identFields.intValue("obj", path);
        Json.Value e = ctx.objects.get(oid);
        if (!(e instanceof Json.Object o)) {
            throw new FrontendException(path + ": object id " + oid + " is not an object");
        }
        return o.intValue("type", "objects[" + oid + "]");
    }

    /** Go's zero value for a variable of the given kind, as a constant node. */
    private GoExprNode zeroNode(GoKind kind, int typeId, BuildCtx ctx, String path) {
        return switch (kind.slotKind()) {
            case Int -> ConstNode.ofInt(kind, 0L);
            case Long -> ConstNode.ofInt(kind, 0L);
            case Float -> ConstNode.ofFloat(kind, 0, 0L);
            case Double -> ConstNode.ofFloat(kind, 0, 0L);
            case Boolean -> ConstNode.ofBool(false);
            case Object -> {
                if (kind == GoKind.STRING) {
                    yield ConstNode.ofString(GoString.EMPTY);
                }
                // WS G: a complex zero value is (0+0i), not nil (mirrors
                // Boxing.zeroValue; without this, `var z complex128` reads
                // null and every complex use — conversion included — sees
                // a nil instead of the zero value).
                if (kind == GoKind.COMPLEX64) {
                    yield ConstNode.ofObject(kind, graalgo.values.GoComplex32.ZERO);
                }
                if (kind == GoKind.COMPLEX128 || kind == GoKind.UNTYPED_COMPLEX) {
                    yield ConstNode.ofObject(kind, graalgo.values.GoComplex64.ZERO);
                }
                if (kind == GoKind.STRUCT) {
                    // NEW-P0-3: atomic.Pointer/Value get a fresh AtomicReference
                    // cell per evaluation (the value model; method calls route
                    // to syncop). Other sync primitives (Mutex, Pool, ...) keep
                    // the GoStruct zero — their real methods operate on struct
                    // fields, not opaque cells.
                    String prim = syncPrimitiveName(ctx, typeId, path);
                    if ("atomic.Pointer".equals(prim) || "atomic.Value".equals(prim)) {
                        yield new SyncPrimitiveZeroNode(prim);
                    }
                    // Fresh per evaluation (M4, WS E): a shared template
                    // would alias nested values across executions.
                    yield new StructZeroNode(structZeroTemplate(ctx, typeId, path));
                }
                if (kind == GoKind.ARRAY) {
                    // A Go array's zero value is an array of zero values
                    // (not nil): fresh per evaluation, recursively
                    // initialized for nested arrays/structs.
                    yield ConstNode.ofObject(kind, zeroArrayFor(typeId, ctx, path));
                }
                // Slices, maps, pointers, interfaces, funcs, chans: nil.
                yield ConstNode.ofObject(kind, null);
            }
            case Byte -> throw new FrontendException(
                    path + ": no Go kind maps to a Byte frame slot (WS C/D)");
            case Illegal, Static -> throw new FrontendException(
                    path + ": no Go kind maps to frame slot kind " + kind.slotKind() + " (WS C/D)");
        };
    }

    /**
     * Go-exact assignment lowering: every right-hand side (and, for
     * op-assignments, every read of an old left-hand value) is evaluated
     * before any assignment takes place, then the assignments run
     * left-to-right (Go spec, "Assignments"). A single assignment lowers
     * directly; several go through fresh temporary slots (typed by the
     * value's kind) so {@code a, b = b, a} swaps instead of sequentially
     * overwriting.
     */
    private GoStmtNode assignTwoPhase(List<Integer> slots, List<GoKind> kinds, List<GoExprNode> values,
                                      BuildCtx ctx) {
        if (slots.size() == 1) {
            return new AssignNode(slots.get(0), kinds.get(0), values.get(0));
        }
        List<GoStmtNode> out = new ArrayList<>(2 * slots.size());
        List<Integer> tmps = new ArrayList<>(slots.size());
        List<GoKind> tmpKinds = new ArrayList<>(slots.size());
        for (int i = 0; i < slots.size(); i++) {
            GoKind vk = values.get(i).kind();
            int tmp = ctx.frames.addSlot(vk.slotKind(), "tmp$assign$" + i, null);
            tmps.add(tmp);
            tmpKinds.add(vk);
            out.add(new AssignNode(tmp, vk, values.get(i)));
        }
        for (int i = 0; i < slots.size(); i++) {
            out.add(new AssignNode(slots.get(i), kinds.get(i), new VarReadNode(tmpKinds.get(i), tmps.get(i))));
        }
        return new BlockNode(out.toArray(new GoStmtNode[0]));
    }

    /**
     * Box-aware two-phase assignment (loopcap): like {@link #assignTwoPhase}
     * but each ident write goes through {@link #writeIdentSlot} so boxed
     * variables write their GoBox cell. Used when at least one lhs ident
     * is boxed; the all-plain case keeps the original.
     */
    private GoStmtNode assignTwoPhaseBoxed(List<Lhs> lhss, List<GoExprNode> values, BuildCtx ctx,
                                           String path) {
        if (lhss.size() == 1) {
            Lhs l = lhss.get(0);
            return writeIdentSlot(l, values.get(0), ctx, path);
        }
        List<GoStmtNode> out = new ArrayList<>(2 * lhss.size());
        List<Integer> tmps = new ArrayList<>(lhss.size());
        List<GoKind> tmpKinds = new ArrayList<>(lhss.size());
        for (int i = 0; i < lhss.size(); i++) {
            GoKind vk = values.get(i).kind();
            int tmp = ctx.frames.addSlot(vk.slotKind(), "tmp$assign$box$" + i, null);
            tmps.add(tmp);
            tmpKinds.add(vk);
            out.add(new AssignNode(tmp, vk, values.get(i)));
        }
        for (int i = 0; i < lhss.size(); i++) {
            Lhs l = lhss.get(i);
            out.add(writeIdentSlot(l, new VarReadNode(tmpKinds.get(i), tmps.get(i)), ctx, path));
        }
        return new BlockNode(out.toArray(new GoStmtNode[0]));
    }

    private GoStmtNode buildBlock(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "block");
        Json.Array stmts = f.array("stmts", "block.fields");
        List<GoStmtNode> out = new ArrayList<>(stmts.size());
        for (int i = 0; i < stmts.size(); i++) {
            Json.Value s = stmts.get(i);
            if (!(s instanceof Json.Object o)) {
                throw new FrontendException("block.fields.stmts[" + i + "]: must be an object");
            }
            out.add(buildStmt(o, ctx));
        }
        return new BlockNode(out.toArray(new GoStmtNode[0]));
    }

    /** Allocates a frame slot for one declared object; returns the slot index. */
    private int allocSlot(String varName, int oid, int typeId, String path, BuildCtx ctx) {
        int slot = ctx.frames.addSlot(slotKindFor(typeId, path, ctx), varName, null);
        if (ctx.objSlots.putIfAbsent(oid, slot) != null) {
            throw new FrontendException(path + ": duplicate object id " + oid);
        }
        ctx.slotsByName.put(varName, slot);
        ctx.kindsByName.put(varName, kindOf(ctx, typeId, path));
        return slot;
    }

    /** The frame slot kind a variable of the given type occupies. */
    private FrameSlotKind slotKindFor(int typeId, String path, BuildCtx ctx) {
        // Aliases and named types resolve before decoders run.
        int rid = resolveAlias(ctx, typeId, path);
        Json.Object entry = ctx.types.entry(rid, path);
        while ("named".equals(entry.string("kind", path))) {
            rid = entry.object("detail", path).intValue("underlying", path);
            entry = ctx.types.entry(rid, path);
        }
        for (TypeDecoder d : typeDecoders) {
            FrameSlotKind slotKind = d.decode(rid, entry);
            if (slotKind != null) {
                return slotKind;
            }
        }
        throw new FrontendException(path + ": no TypeDecoder handles type id " + typeId
                + " (WS C/D hook: NodeFactory.registerTypeDecoder)");
    }

    /**
     * Allocates a slot for an incoming parameter value WITHOUT registering
     * it as the variable's slot (P3-M9, WS D): an address-taken parameter
     * lives in a GoBox cell (see {@link #allocParamSlot}); the root writes
     * the argument into this raw slot and the prologue boxes it.
     */
    private int allocRawSlot(String varName, int typeId, String path, BuildCtx ctx) {
        return ctx.frames.addSlot(slotKindFor(typeId, path, ctx), varName, null);
    }

    /**
     * Allocates an Object slot holding the {@link graalgo.values.GoBox}
     * cell of an address-taken variable and registers it as the variable's
     * slot (P3-M9, WS D).
     */
    private int allocBoxSlot(String varName, int oid, int typeId, String path, BuildCtx ctx) {
        int slot = ctx.frames.addSlot(FrameSlotKind.Object, varName, null);
        if (ctx.objSlots.putIfAbsent(oid, slot) != null) {
            throw new FrontendException(path + ": duplicate object id " + oid);
        }
        ctx.slotsByName.put(varName, slot);
        ctx.kindsByName.put(varName, kindOf(ctx, typeId, path));
        ctx.boxed.add(oid);
        return slot;
    }

    /**
     * Allocates a parameter's slot (P3-M9, WS D). A plain parameter uses
     * the standard path. An address-taken parameter lives in a GoBox
     * cell: the returned slot is the raw incoming slot the root writes
     * the argument into, the variable's registered slot is the cell, and
     * a {@link BoxDeclNode} prologue statement (appended to
     * {@code prologue}) moves the argument into the cell before the body
     * runs — so {@code &param} and writes through it alias the parameter.
     */
    private int allocParamSlot(String pname, int oid, int typeId, String path, BuildCtx ctx,
                               List<GoStmtNode> prologue) {
        if (!addrTaken.contains(oid)) {
            return allocSlot(pname, oid, typeId, path, ctx);
        }
        int rawSlot = allocRawSlot(pname, typeId, path, ctx);
        int boxSlot = allocBoxSlot(pname, oid, typeId, path, ctx);
        GoKind kind = kindOf(ctx, typeId, path);
        prologue.add(new BoxDeclNode(boxSlot, kind, new VarReadNode(kind, rawSlot)));
        return rawSlot;
    }

    /**
     * P3-M9 (WS D): boxes every variable declared in a function body
     * whose address is taken anywhere in the program ({@link #addrTaken}):
     * its declaration installs a GoBox cell and every access goes through
     * the cell, so {@code &x} pointers alias the variable. (Loop variables
     * are additionally handled by the loopcap prescans; this covers plain
     * declarations in any function or function literal body.)
     */
    private void prescanAddrTakenDecls(Json.Value body, BuildCtx ctx, String path) {
        Set<Integer> decls = new HashSet<>();
        collectVardeclObjs(body, decls, path);
        for (int oid : decls) {
            if (addrTaken.contains(oid)) {
                ctx.boxed.add(oid);
            }
        }
    }

    /** Frame slot for an object-table id, allocating on first declaration (M4: shared helper). */
    private int declareObj(BuildCtx ctx, int oid, String path) {
        Integer existing = ctx.objSlots.get(oid);
        if (existing != null) {
            // := redeclaration (same block, same type, at least one new
            // variable on the left): go/types records the name in Uses,
            // the exporter maps it to the existing object id, and the
            // declaration reuses the variable's slot.
            return existing;
        }
        Json.Value oe = ctx.objects.get(oid);
        if (!(oe instanceof Json.Object objEnt)) {
            throw new FrontendException(path + ": object id " + oid + " is not an object");
        }
        String varName = objEnt.string("name", "objects[" + oid + "]");
        int typeId = objEnt.intValue("type", "objects[" + oid + "]");
        if (ctx.boxed.contains(oid)) {
            // M6: boxed (per-iteration) variable — the slot holds a GoBox
            // cell (FrameSlotKind.Object), allocated fresh on each
            // execution of the declaration.
            int slot = ctx.frames.addSlot(FrameSlotKind.Object, varName, null);
            if (ctx.objSlots.putIfAbsent(oid, slot) != null) {
                throw new FrontendException(path + ": duplicate object id " + oid);
            }
            ctx.slotsByName.put(varName, slot);
            ctx.kindsByName.put(varName, kindOf(ctx, typeId, path));
            return slot;
        }
        return allocSlot(varName, oid, typeId, "objects[" + oid + "]", ctx);
    }

    /** The declared type id behind an object-table id. */
    private int objTypeId(BuildCtx ctx, int oid, String path) {
        Json.Value oe = ctx.objects.get(oid);
        if (!(oe instanceof Json.Object objEnt)) {
            throw new FrontendException(path + ": object id " + oid + " is not an object");
        }
        return objEnt.intValue("type", "objects[" + oid + "]");
    }

    private GoStmtNode buildVarDecl(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "vardecl");
        String path = "vardecl.fields";
        Json.Array names = f.array("names", path);
        Json.Array objs = f.array("objs", path);
        Json.Array values = f.array("values", path);
        if (names.size() != objs.size()) {
            throw new FrontendException(path + ": names/objs length mismatch");
        }
        if (!values.items().isEmpty() && values.size() != objs.size()
                && !(values.size() == 1 && objs.size() == 2)) {
            throw new FrontendException(path + ": values/objs length mismatch");
        }
        List<Integer> slots = new ArrayList<>(objs.size());
        List<GoKind> kinds = new ArrayList<>(objs.size());
        List<Integer> typeIds = new ArrayList<>(objs.size());
        List<Integer> oids = new ArrayList<>(objs.size());
        for (int i = 0; i < objs.size(); i++) {
            Json.Value o = objs.get(i);
            if (!(o instanceof Json.Num n)) {
                throw new FrontendException(path + ".objs[" + i + "]: must be an integer");
            }
            int oid = (int) n.asLong(path + ".objs[" + i + "]");
            int typeId = objTypeId(ctx, oid, path);
            GoKind kind = kindOf(ctx, typeId, "objects[" + oid + "]");
            slots.add(declareObj(ctx, oid, path));
            kinds.add(kind);
            typeIds.add(typeId);
            oids.add(oid);
        }
        if (values.items().isEmpty()) {
            // No initializers: each slot gets Go's zero value.
            // M6: boxed variables declare a fresh GoBox cell per execution.
            List<GoStmtNode> out = new ArrayList<>(slots.size());
            for (int i = 0; i < slots.size(); i++) {
                GoExprNode zero = zeroNode(kinds.get(i), typeIds.get(i), ctx, path);
                out.add(ctx.boxed.contains(oids.get(i))
                        ? new BoxDeclNode(slots.get(i), kinds.get(i), zero)
                        : new VarDeclNode(slots.get(i), kinds.get(i), zero));
            }
            if (out.size() == 1) {
                return out.get(0);
            }
            return new BlockNode(out.toArray(new GoStmtNode[0]));
        }
        if (values.size() == 1 && slots.size() == 2
                && isCommaOkValue(asObject(values.get(0), path + ".values[0]"))) {
            // v, ok := m[k] / v, ok := x.(T) (M4, WS E).
            List<Lhs> lhss = new ArrayList<>(2);
            for (int i = 0; i < 2; i++) {
                Lhs l = new Lhs();
                if ("_".equals(names.get(i) instanceof Json.Str s ? s.value() : "")) {
                    l.isBlank = true;
                } else {
                    l.isIdent = true;
                    l.oid = oids.get(i);
                    l.slot = slots.get(i);
                    l.kind = kinds.get(i);
                    l.typeId = typeIds.get(i);
                }
                lhss.add(l);
            }
            return buildCommaOk(lhss, asObject(values.get(0), path + ".values[0]"), true, ctx, path, node);
        }
        List<GoExprNode> inits = new ArrayList<>(objs.size());
        for (int i = 0; i < objs.size(); i++) {
            inits.add(maybeConvert(
                    buildExpr(asObject(values.get(i), path + ".values[" + i + "]"), ctx),
                    kinds.get(i), typeIds.get(i), ctx, path));
        }
        if (slots.size() == 1) {
            // M6: boxed variables declare a fresh GoBox cell per execution.
            if (ctx.boxed.contains(oids.get(0))) {
                return new BoxDeclNode(slots.get(0), kinds.get(0), inits.get(0));
            }
            return new VarDeclNode(slots.get(0), kinds.get(0), inits.get(0));
        }
        // Multi-value declaration: same two-phase rule as assignment — every
        // initializer is evaluated before any slot is written, so a
        // redeclared variable's old value is visible to every initializer.
        // M6: boxed targets declare fresh cells (BoxDeclNode), not writes.
        boolean anyBoxed = oids.stream().anyMatch(ctx.boxed::contains);
        if (!anyBoxed) {
            return assignTwoPhase(slots, kinds, inits, ctx);
        }
        List<GoStmtNode> declOut = new ArrayList<>(2 * slots.size());
        List<Integer> declTmps = new ArrayList<>(slots.size());
        List<GoKind> declTmpKinds = new ArrayList<>(slots.size());
        for (int i = 0; i < slots.size(); i++) {
            GoKind vk = inits.get(i).kind();
            int tmp = ctx.frames.addSlot(vk.slotKind(), "tmp$vardecl$box$" + i, null);
            declTmps.add(tmp);
            declTmpKinds.add(vk);
            declOut.add(new AssignNode(tmp, vk, inits.get(i)));
        }
        for (int i = 0; i < slots.size(); i++) {
            GoExprNode v = new VarReadNode(declTmpKinds.get(i), declTmps.get(i));
            if (ctx.boxed.contains(oids.get(i))) {
                declOut.add(new BoxDeclNode(slots.get(i), kinds.get(i), v));
            } else {
                declOut.add(new AssignNode(slots.get(i), kinds.get(i), v));
            }
        }
        return new BlockNode(declOut.toArray(new GoStmtNode[0]));
    }

    private static Json.Object asObject(Json.Value v, String path) {
        if (!(v instanceof Json.Object o)) {
            throw new FrontendException(path + ": must be an object");
        }
        return o;
    }

    private GoStmtNode buildAssign(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "assign");
        String path = "assign.fields";
        String tok = f.string("tok", path);
        Json.Array lhs = f.array("lhs", path);
        Json.Array rhs = f.array("rhs", path);
        boolean commaOk = lhs.size() == 2 && rhs.size() == 1
                && isCommaOkValue(asObject(rhs.get(0), path + ".rhs[0]"));
        if (!commaOk && lhs.size() != rhs.size()) {
            throw new FrontendException(path + ": lhs/rhs length mismatch");
        }
        BinaryOpNode.Op op = BinaryOpNode.Op.fromAssignToken(tok);
        if (op != null && lhs.size() != 1) {
            // Mirrors go/types: "assignment operation += requires single-valued expressions".
            throw new FrontendException(path + ": assignment operation " + tok
                    + " requires single-valued expressions (WS E)");
        }
        List<Lhs> lhss = new ArrayList<>(lhs.size());
        for (int i = 0; i < lhs.size(); i++) {
            lhss.add(classifyLhs(asObject(lhs.get(i), path + ".lhs[" + i + "]"), ctx, path));
        }
        if (commaOk) {
            return buildCommaOk(lhss, asObject(rhs.get(0), path + ".rhs[0]"), false, ctx, path, node);
        }
        boolean hasLvalue = lhss.stream().anyMatch(l -> !l.isIdent);
        boolean hasCapture = lhss.stream().anyMatch(l -> l.captureDepth > 0);
        // M7: globals bypass the slot-based fast path (their slots live in
        // the globals frame, not the current frame); they take the general
        // path through writeIdentSlot → GlobalWriteNode.
        boolean hasGlobal = lhss.stream().anyMatch(l -> l.isGlobal);
        if (!hasLvalue && op == null && !hasCapture && !hasGlobal) {
            // Ident-only plain assignment: the M2 two-phase lowering, unchanged.
            // (Captured variables take the general path below for CapWriteNode.)
            // M6: boxed idents write their GoBox cell via writeIdentSlot.
            List<Integer> slots = new ArrayList<>(lhs.size());
            List<GoKind> kinds = new ArrayList<>(lhs.size());
            List<GoExprNode> values = new ArrayList<>(lhs.size());
            boolean anyBoxed = false;
            for (int i = 0; i < lhs.size(); i++) {
                Lhs l = lhss.get(i);
                slots.add(l.slot);
                kinds.add(l.kind);
                values.add(maybeConvert(
                        buildExpr(asObject(rhs.get(i), path + ".rhs[" + i + "]"), ctx),
                        l.kind, l.typeId, ctx, path));
                anyBoxed |= l.oid >= 0 && isBoxedTransitive(ctx, l.oid);
            }
            if (!anyBoxed) {
                return assignTwoPhase(slots, kinds, values, ctx);
            }
            return assignTwoPhaseBoxed(lhss, values, ctx, path);
        }
        if (!hasLvalue && !hasCapture && !hasGlobal) {
            // Ident-only op-assignment: the M2 lowering, unchanged.
            // (Captured variables take the general path below for CapWriteNode.)
            // M6: boxed idents use the box-aware read/write.
            // M7: globals take the general path (GlobalWriteNode).
            Lhs l = lhss.get(0);
            GoExprNode rhsVal = maybeConvert(
                    buildExpr(asObject(rhs.get(0), path + ".rhs[0]"), ctx), l.kind, l.typeId, ctx, path);
            return writeIdentSlot(l,
                    new BinaryOpNode(op, l.kind, readIdentSlot(l, ctx, path), rhsVal), ctx, path);
        }
        // General lvalue path (M4, WS E): Go's two-phase rule — every lhs
        // index/selector operand and every rhs is evaluated (in lexical
        // order) before any write; writes run left-to-right.
        // P3-M9 (bug-030): gc re-reads a simple variable container/index
        // AFTER the rhs completes — the store lands in the variable's
        // current value (e.g. a slice's new backing array after the rhs
        // appends). Only non-variable expressions are pinned into
        // temporaries; simple variable reads are re-evaluated at store
        // time. (Verified against gc 1.27.1: variable container, index,
        // selector base, and pointer are all re-read; call results pin.)
        List<GoStmtNode> prog = new ArrayList<>();
        for (Lhs l : lhss) {
            if (l.isIndex) {
                if (!isSimpleVarRead(l.container)) {
                    l.tmpC = newTmp(l.container.kind(), ctx);
                    prog.add(new AssignNode(l.tmpC, l.container.kind(), l.container, false));
                    l.container = new VarReadNode(l.container.kind(), l.tmpC);
                }
                if (!isSimpleVarRead(l.key)) {
                    l.tmpK = newTmp(l.key.kind(), ctx);
                    prog.add(new AssignNode(l.tmpK, l.key.kind(), l.key, false));
                    l.key = new VarReadNode(l.key.kind(), l.tmpK);
                }
            } else if (l.isSelector) {
                if (!isSimpleVarRead(l.obj)) {
                    l.tmpO = newTmp(l.obj.kind(), ctx);
                    prog.add(new AssignNode(l.tmpO, l.obj.kind(), l.obj, false));
                    l.obj = new VarReadNode(l.obj.kind(), l.tmpO);
                }
            } else if (l.isDeref) {
                // P3-M9 (WS D): evaluate the pointer once into a temporary
                // (Go's two-phase rule: *p is evaluated before the rhs) —
                // unless it is a simple variable, re-read after (bug-030).
                if (!isSimpleVarRead(l.ptr)) {
                    l.tmpP = newTmp(l.ptr.kind(), ctx);
                    prog.add(new AssignNode(l.tmpP, l.ptr.kind(), l.ptr, false));
                    l.ptr = new VarReadNode(l.ptr.kind(), l.tmpP);
                }
            }
        }
        if (op != null) {
            // Single lvalue op-assignment: old = read; new = old op rhs; write.
            Lhs l = lhss.get(0);
            GoKind tk;
            int tid;
            if (l.isIndex) {
                tk = l.elemKind;
                tid = l.elemTypeId;
            } else if (l.isSelector) {
                tk = l.fieldKind;
                tid = l.fieldTypeId;
            } else if (l.isDeref) {
                // P3-M9 (WS D): *p op= v.
                tk = l.elemKind;
                tid = l.elemTypeId;
            } else {
                // Ident (possibly captured): use the variable's kind/type.
                tk = l.kind;
                tid = l.typeId;
            }
            GoExprNode rhsVal = maybeConvert(
                    buildExpr(asObject(rhs.get(0), path + ".rhs[0]"), ctx), tk, tid, ctx, path);
            GoExprNode newVal = new BinaryOpNode(op, tk, readLvalue(l, ctx, path), rhsVal);
            newVal.setSourceSection(sectionFor(node, "assign"));
            GoStmtNode w = writeLvalue(l, newVal, ctx, path);
            w.setSourceSection(sectionFor(node, "assign"));
            prog.add(w);
            return new BlockNode(prog.toArray(new GoStmtNode[0]));
        }
        List<GoExprNode> rhsVals = new ArrayList<>(rhs.size());
        List<GoKind> rhsKinds = new ArrayList<>(rhs.size());
        for (int i = 0; i < rhs.size(); i++) {
            Lhs l = lhss.get(i);
            GoExprNode v = buildExpr(asObject(rhs.get(i), path + ".rhs[" + i + "]"), ctx);
            if (!l.isBlank) {
                // Blank: evaluate and discard; no target type to convert to.
                GoKind tk = targetKind(l);
                int tid = targetTypeId(l);
                v = maybeConvert(v, tk, tid, ctx, path);
            }
            int tmp = newTmp(v.kind(), ctx);
            prog.add(new AssignNode(tmp, v.kind(), v));
            rhsVals.add(new VarReadNode(v.kind(), tmp));
            rhsKinds.add(v.kind());
        }
        for (int i = 0; i < lhss.size(); i++) {
            Lhs l = lhss.get(i);
            if (l.isIdent) {
                // M5 (WS F): ident write, capture-aware; M6: box-aware.
                prog.add(writeIdentSlot(l, rhsVals.get(i), ctx, path));
            } else if (!l.isBlank) {
                GoStmtNode w = writeLvalue(l, rhsVals.get(i), ctx, path);
                w.setSourceSection(sectionFor(node, "assign"));
                prog.add(w);
            }
            // Blank: the value was already evaluated; nothing to store.
        }
        return new BlockNode(prog.toArray(new GoStmtNode[0]));
    }

    /** One assignment target: identifier, blank, index, or selector (M4, WS E). */

    /**
     * P3-M9 (bug-030): whether an expression node is a simple variable
     * read — local ({@link VarReadNode}), global ({@link GlobalReadNode}/
     * {@link GlobalBoxReadNode}), captured ({@link CapReadNode}), or boxed
     * ({@link BoxReadNode}). gc re-reads such variables AFTER the rhs of
     * an assignment completes, so the store uses the variable's current
     * value; they must NOT be pinned into temporaries before the rhs.
     * Every other expression shape keeps the pinning (single evaluation,
     * lexical order for side effects).
     */
    private static boolean isSimpleVarRead(GoExprNode n) {
        return n instanceof VarReadNode
                || n instanceof GlobalReadNode
                || n instanceof GlobalBoxReadNode
                || n instanceof CapReadNode
                || n instanceof BoxReadNode;
    }

    private static final class Lhs {
        boolean isIdent;
        int slot;
        GoKind kind;
        int typeId;
        /** Object id for ident lvalues (-1 for blank/index/selector). */
        int oid = -1;
        boolean isBlank;
        boolean isIndex;
        GoExprNode container;
        GoExprNode key;
        GoKind containerKind;
        GoKind elemKind;
        int elemTypeId;
        int tmpC = -1;
        int tmpK = -1;
        boolean isSelector;
        GoExprNode obj;
        int[] selpath;
        GoKind fieldKind;
        int fieldTypeId;
        int tmpO = -1;
        /** Dereference lvalue (P3-M9, WS D): *p = v. */
        boolean isDeref;
        GoExprNode ptr;
        int tmpP = -1;
        /** Capture depth for ident lvalues (M5, WS F): 0 = local, >0 = ancestor. */
        int captureDepth;
        /** Package-level variable (M7): the write goes to the globals frame. */
        boolean isGlobal;
    }

    private static GoKind targetKind(Lhs l) {
        if (l.isIndex) {
            return l.elemKind;
        }
        if (l.isSelector) {
            return l.fieldKind;
        }
        if (l.isDeref) {
            return l.elemKind;
        }
        return l.kind;
    }

    private static int targetTypeId(Lhs l) {
        if (l.isIndex) {
            return l.elemTypeId;
        }
        if (l.isSelector) {
            return l.fieldTypeId;
        }
        if (l.isDeref) {
            return l.elemTypeId;
        }
        return l.typeId;
    }

    /** Fresh temporary frame slot. */
    private int newTmp(GoKind kind, BuildCtx ctx) {
        return ctx.frames.addSlot(kind.slotKind(), "tmp$m4", null);
    }

    /** True for a two-valued map-index / type-assertion rhs node. */
    private static boolean isCommaOkValue(Json.Object v) {
        String k = v.string("kind", "value");
        if (!("index".equals(k) || "typeassert".equals(k))) {
            return false;
        }
        Json.Value cv = v.object("fields", "value").opt("commaOk");
        return cv instanceof Json.Bool b && b.value();
    }

    private Lhs classifyLhs(Json.Object l, BuildCtx ctx, String path) {
        String k = l.string("kind", path);
        Lhs lhs = new Lhs();
        if ("blank".equals(k)) {
            // The blank node carries no fields.
            lhs.isBlank = true;
            return lhs;
        }
        Json.Object lf = l.object("fields", path);
        switch (k) {
            case "ident" -> {
                lhs.isIdent = true;
                int oid = lf.intValue("obj", path);
                lhs.oid = oid;
                // M7: package-level variable — resolved against the globals
                // frame, never a function frame (and never captured).
                Integer gslot = globalSlots.get(oid);
                if (gslot != null) {
                    lhs.isGlobal = true;
                    lhs.slot = gslot;
                    lhs.typeId = objTypeOf(ctx, lf, path);
                    lhs.kind = kindOf(ctx, lhs.typeId, path);
                } else {
                    VarResolution r = resolveVar(ctx, oid, "assign.lhs");
                    lhs.slot = r.slot;
                    lhs.captureDepth = r.depth;
                    lhs.typeId = objTypeOf(ctx, lf, path);
                    lhs.kind = kindOf(ctx, lhs.typeId, path);
                }
            }
            case "index" -> {
                lhs.isIndex = true;
                lhs.container = buildExpr(lf.object("x", path), ctx);
                lhs.key = buildExpr(lf.object("index", path), ctx);
                int ctid = lhs.container.typeId();
                if (ctid < 0) {
                    throw new FrontendException(path + ": index container without a type id (WS E)");
                }
                lhs.containerKind = kindOf(ctx, ctid, path);
                if (lhs.containerKind == GoKind.MAP) {
                    // P3-M9 (bug-028): the key must be converted to the
                    // map's key type (interface boxing for map[any]), not
                    // boxed at its static kind.
                    lhs.key = convertMapKey(lhs.key, ctid, ctx, path);
                }
                lhs.elemTypeId = lf.intValue("type", path);
                lhs.elemKind = kindOf(ctx, lhs.elemTypeId, path);
            }
            case "selector" -> {
                lhs.isSelector = true;
                lhs.obj = buildExpr(lf.object("x", path), ctx);
                Json.Array sp = lf.array("selpath", path);
                lhs.selpath = new int[sp.size()];
                for (int i = 0; i < sp.size(); i++) {
                    lhs.selpath[i] = (int) ((Json.Num) sp.get(i)).asLong(path);
                }
                lhs.fieldTypeId = lf.intValue("type", path);
                lhs.fieldKind = kindOf(ctx, lhs.fieldTypeId, path);
            }
            case "deref" -> {
                // P3-M9 (WS D): *p = v. The pointer expression is built
                // here; the element kind/type come from the wire "type".
                lhs.isDeref = true;
                lhs.ptr = buildExpr(lf.object("x", path), ctx);
                lhs.elemTypeId = lf.intValue("type", path);
                lhs.elemKind = kindOf(ctx, lhs.elemTypeId, path);
            }
            default -> throw new FrontendException(path + ": bad assignment lhs kind \"" + k + "\" (WS E)");
        }
        return lhs;
    }

    /**
     * Box-aware ident write (loopcap): a boxed variable writes its
     * {@link graalgo.values.GoBox} cell via {@link BoxWriteNode}
     * (phase-aware slot from {@link #boxWriteSlot}); otherwise the plain
     * M2/M5 path (AssignNode local, CapWriteNode captured).
     */
    private GoStmtNode writeIdentSlot(Lhs l, GoExprNode value, BuildCtx ctx, String path) {
        // M7: package-level variable — write the globals-frame cell.
        // P3-M9 (WS D): an address-taken global writes its GoBox cell.
        if (l.isGlobal) {
            return addrTaken.contains(l.oid)
                    ? new GlobalBoxWriteNode(l.kind, globalsFrame, l.slot, value)
                    : new GlobalWriteNode(l.kind, globalsFrame, l.slot, value);
        }
        if (l.oid >= 0 && isBoxedTransitive(ctx, l.oid)) {
            return new BoxWriteNode(l.kind, boxWriteSlot(ctx, l.oid, path), value);
        }
        if (l.captureDepth == 0) {
            return new AssignNode(l.slot, l.kind, value);
        }
        ctx.hasCapture = true;
        return new CapWriteNode(l.kind, l.captureDepth - 1, l.slot,
                ctx.closureSlot, value);
    }

    /**
     * Box-aware ident read (loopcap): a boxed variable reads its
     * {@link graalgo.values.GoBox} cell via {@link #readBoxedVar};
     * otherwise the plain M2/M5 path.
     */
    private GoExprNode readIdentSlot(Lhs l, BuildCtx ctx, String path) {
        // M7: package-level variable — read the globals-frame cell.
        // P3-M9 (WS D): an address-taken global reads its GoBox cell.
        if (l.isGlobal) {
            return addrTaken.contains(l.oid)
                    ? new GlobalBoxReadNode(l.kind, globalsFrame, l.slot)
                    : new GlobalReadNode(l.kind, globalsFrame, l.slot);
        }
        if (l.oid >= 0 && isBoxedTransitive(ctx, l.oid)) {
            return readBoxedVar(l.kind, ctx, l.oid, path);
        }
        if (l.captureDepth == 0) {
            return new VarReadNode(l.kind, l.slot);
        }
        ctx.hasCapture = true;
        return new CapReadNode(l.kind, l.captureDepth - 1, l.slot, ctx.closureSlot);
    }

    /** Read half of an lvalue (for op-assignments), over evaluated temporaries. */
    private GoExprNode readLvalue(Lhs l, BuildCtx ctx, String path) {
        if (l.isIdent) {
            // M5 (WS F): ident read, capture-aware; M6: box-aware.
            return readIdentSlot(l, ctx, path);
        }
        if (l.isIndex) {
            GoStructDescriptor ed = l.elemKind == GoKind.STRUCT
                    ? structDescriptorFor(ctx, l.elemTypeId, path) : null;
            return new IndexNode(l.container, l.key, l.containerKind, l.elemKind, ed,
                    zeroTemplateOrNull(l.elemKind, l.elemTypeId, ctx, path));
        }
        if (l.isDeref) {
            // P3-M9 (WS D): *p read.
            return new DerefNode(l.elemKind, l.ptr);
        }
        return new SelectorNode(l.obj, l.selpath, l.fieldKind);
    }

    /** Write half of an lvalue, over evaluated temporaries. */
    private GoStmtNode writeLvalue(Lhs l, GoExprNode value, BuildCtx ctx, String path) {
        if (l.isIdent) {
            // M5 (WS F): ident write, capture-aware; M6: box-aware.
            return writeIdentSlot(l, value, ctx, path);
        }
        if (l.isIndex) {
            return new IndexWriteNode(l.container, l.key, value, l.containerKind, l.elemKind);
        }
        if (l.isDeref) {
            // P3-M9 (WS D): *p = v.
            return new DerefWriteNode(l.ptr, value, l.elemKind);
        }
        return new SelectorWriteNode(l.obj, value, l.selpath, l.fieldKind);
    }

    /**
     * Two-valued assignment {@code v, ok := m[k]} / {@code v, ok = m[k]}
     * (and the type-assertion forms), M4 WS E. The container/key (or
     * interface) is evaluated once into temporaries; the value and ok
     * halves read the temporaries.
     */
    private GoStmtNode buildCommaOk(List<Lhs> lhss, Json.Object vnode, boolean isDecl,
                                    BuildCtx ctx, String path, Json.Object stmtNode) {
        Json.Object vf = vnode.object("fields", path);
        String vk = vnode.string("kind", path);
        List<GoStmtNode> prog = new ArrayList<>();
        GoExprNode valueNode;
        GoExprNode okNode;
        if ("index".equals(vk)) {
            GoExprNode container = buildExpr(vf.object("x", path), ctx);
            GoExprNode key = buildExpr(vf.object("index", path), ctx);
            int ctid = container.typeId();
            if (ctid >= 0 && kindOf(ctx, ctid, path) == GoKind.MAP) {
                // P3-M9 (bug-028): convert the key to the map's key type
                // before the tmp slot is allocated, so the slot kind
                // matches the converted key.
                key = convertMapKey(key, ctid, ctx, path);
            }
            int tmpC = newTmp(container.kind(), ctx);
            int tmpK = newTmp(key.kind(), ctx);
            prog.add(new AssignNode(tmpC, container.kind(), container, false));
            prog.add(new AssignNode(tmpK, key.kind(), key, false));
            GoKind containerKind = kindOf(ctx, container.typeId(), path);
            GoKind elemKind = targetKind(lhss.get(0));
            int elemTypeId = targetTypeId(lhss.get(0));
            GoStructDescriptor ed = elemKind == GoKind.STRUCT
                    ? structDescriptorFor(ctx, elemTypeId, path) : null;
            valueNode = new IndexNode(new VarReadNode(container.kind(), tmpC),
                    new VarReadNode(key.kind(), tmpK), containerKind, elemKind, ed,
                    zeroTemplateOrNull(elemKind, elemTypeId, ctx, path));
            okNode = new IndexOkNode(new VarReadNode(container.kind(), tmpC),
                    new VarReadNode(key.kind(), tmpK));
        } else if ("typeassert".equals(vk)) {
            GoExprNode iface = buildExpr(vf.object("x", path), ctx);
            int tmpI = newTmp(iface.kind(), ctx);
            prog.add(new AssignNode(tmpI, iface.kind(), iface, false));
            int assertTypeId = vf.intValue("type", path);
            GoKind ak = kindOf(ctx, assertTypeId, path);
            graalgo.values.GoTypeDescriptor adesc = typeDescriptorFor(ctx, assertTypeId, path);
            // P3-M3 (WS D): assertion to a non-empty interface is allowed;
            // the implements check runs at runtime against the dynamic
            // type's method set (the old "needs method tables" fail-fast
            // is retired — method tables exist now).
            List<String> ifaceMethods = null;
            Map<String, Set<String>> dynMethods = null;
            if (ak == GoKind.INTERFACE && !adesc.isEmptyInterface()) {
                ifaceMethods = new ArrayList<>(interfaceMethodImplKeys(ctx, assertTypeId, path));
                dynMethods = dynamicMethodSets(ctx, path);
            }
            GoStructDescriptor sd = ak == GoKind.STRUCT
                    ? structDescriptorFor(ctx, assertTypeId, path) : null;
            valueNode = new TypeAssertNode(new VarReadNode(iface.kind(), tmpI), ak, adesc, true, sd,
                    GoTypeStrings.of(ctx, iface.typeId()), GoTypeStrings.of(ctx, assertTypeId),
                    ifaceMethods, dynMethods,
                    zeroTemplateOrNull(ak, assertTypeId, ctx, path));
            okNode = new TypeAssertOkNode(new VarReadNode(iface.kind(), tmpI), adesc,
                    ifaceMethods, dynMethods);
        } else {
            throw new FrontendException(path + ": bad comma-ok kind \"" + vk + "\" (WS E)");
        }
        valueNode.setSourceSection(sectionFor(stmtNode, "comma-ok"));
        okNode.setSourceSection(sectionFor(stmtNode, "comma-ok"));
        for (int i = 0; i < 2; i++) {
            Lhs l = lhss.get(i);
            if (l.isBlank) {
                continue; // evaluated already; discarded
            }
            if (!l.isIdent) {
                throw new FrontendException(path + ": comma-ok target must be a variable (WS E)");
            }
            GoExprNode v = i == 0
                    ? maybeConvert(valueNode, l.kind, l.typeId, ctx, path)
                    : okNode; // bool: no conversion
            GoStmtNode w;
            if (l.oid >= 0 && isBoxedTransitive(ctx, l.oid)) {
                // M6: boxed comma-ok target — fresh cell on :=, cell write on =.
                w = isDecl ? new BoxDeclNode(l.slot, l.kind, v)
                        : new BoxWriteNode(l.kind, boxWriteSlot(ctx, l.oid, path), v);
            } else {
                w = isDecl ? new VarDeclNode(l.slot, l.kind, v) : new AssignNode(l.slot, l.kind, v);
            }
            w.setSourceSection(sectionFor(stmtNode, "comma-ok"));
            prog.add(w);
        }
        return new BlockNode(prog.toArray(new GoStmtNode[0]));
    }

    private GoStmtNode buildExprStmt(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "exprstmt");
        return new ExprStmtNode(buildExpr(f.object("x", "exprstmt.fields"), ctx));
    }

    private GoStmtNode buildReturn(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "return");
        Json.Array results = f.array("results", "return.fields");
        if (results.size() > 1) {
            throw new FrontendException("return with multiple results is not supported in v1");
        }
        GoExprNode value = null;
        if (results.size() == 1) {
            Json.Object re = asObject(results.get(0), "return.fields.results[0]");
            value = buildExpr(re, ctx);
            // M6 (WS H): box a concrete value into the declared interface
            // result type. `func() interface{} { var x int; return x }` must
            // return a GoInterface, not the raw Long — mirrors the call-site
            // argument adaptation in buildCall.
            if (ctx.resultTypeId >= 0
                    && kindOf(ctx, ctx.resultTypeId, "return") == GoKind.INTERFACE) {
                int valueTypeId = re.object("fields", "return.fields.results[0]")
                        .intValue("type", "return.fields.results[0]");
                if (kindOf(ctx, valueTypeId, "return") != GoKind.INTERFACE) {
                    value = boxInterface(value, ctx, "return");
                }
            }
        }
        // M5 (WS F): function literals with results assign the result slot
        // before throwing GoReturn (defer runs before the return completes).
        if (ctx.resultSlot >= 0) {
            return new ReturnNode(value, ctx.resultSlot, ctx.resultKind, ctx.resultBoxed);
        }
        return new ReturnNode(value);
    }

    private GoStmtNode buildIncDec(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "incdec");
        String path = "incdec.fields";
        long delta = switch (f.string("tok", path)) {
            case "++" -> 1L;
            case "--" -> -1L;
            default -> throw new FrontendException(path + ": bad incdec tok");
        };
        Json.Object x = f.object("x", path);
        if ("ident".equals(x.string("kind", path + ".x"))) {
            Json.Object xf = x.object("fields", path + ".x");
            int oid = xf.intValue("obj", path);
            Integer gslot = globalSlots.get(oid);
            if (gslot != null) {
                // M7: package-level variable — read-modify-write the
                // globals-frame cell (never captured, never boxed).
                Lhs l = new Lhs();
                l.isIdent = true;
                l.isGlobal = true;
                l.oid = oid;
                l.slot = gslot;
                l.kind = kindOf(ctx, objTypeOf(ctx, xf, path + ".x"), path + ".x");
                GoExprNode newVal = new BinaryOpNode(
                        delta > 0 ? BinaryOpNode.Op.ADD : BinaryOpNode.Op.SUB,
                        l.kind, readIdentSlot(l, ctx, path), oneNode(l.kind));
                newVal.setSourceSection(sectionFor(node, "incdec"));
                GoStmtNode w = writeIdentSlot(l, newVal, ctx, path);
                w.setSourceSection(sectionFor(node, "incdec"));
                return w;
            }
            VarResolution r = resolveVar(ctx, oid, "incdec.x");
            GoKind kind = kindOf(ctx, objTypeOf(ctx, xf, path + ".x"), path + ".x");
            if (isBoxedTransitive(ctx, oid)) {
                // M6: boxed variable — read-modify-write the GoBox cell.
                Lhs l = new Lhs();
                l.isIdent = true;
                l.oid = oid;
                l.slot = r.slot;
                l.kind = kind;
                l.captureDepth = r.depth;
                GoExprNode newVal = new BinaryOpNode(
                        delta > 0 ? BinaryOpNode.Op.ADD : BinaryOpNode.Op.SUB,
                        kind, readIdentSlot(l, ctx, path), oneNode(kind));
                newVal.setSourceSection(sectionFor(node, "incdec"));
                GoStmtNode w = writeIdentSlot(l, newVal, ctx, path);
                w.setSourceSection(sectionFor(node, "incdec"));
                return w;
            }
            if (r.depth == 0) {
                return new IncDecNode(r.slot, kind, delta);
            }
            // Captured variable: read-modify-write through the $closure chain.
            return new CapIncDecNode(kind, r.depth - 1, r.slot, ctx.closureSlot, delta);
        }
        // s[i]++ / s.E++ (M4, WS E): desugar through the lvalue path as
        // old = read; new = old ± 1; write, with the container/key evaluated once.
        Lhs l = classifyLhs(x, ctx, path);
        if (l.isBlank) {
            throw new FrontendException(path + ".x: incdec on blank (WS E)");
        }
        GoKind tk = targetKind(l);
        int tid = targetTypeId(l);
        List<GoStmtNode> prog = new ArrayList<>();
        if (l.isIndex) {
            l.tmpC = newTmp(l.container.kind(), ctx);
            l.tmpK = newTmp(l.key.kind(), ctx);
            prog.add(new AssignNode(l.tmpC, l.container.kind(), l.container, false));
            prog.add(new AssignNode(l.tmpK, l.key.kind(), l.key, false));
            l.container = new VarReadNode(l.container.kind(), l.tmpC);
            l.key = new VarReadNode(l.key.kind(), l.tmpK);
        } else if (l.isSelector) {
            l.tmpO = newTmp(l.obj.kind(), ctx);
            prog.add(new AssignNode(l.tmpO, l.obj.kind(), l.obj, false));
            l.obj = new VarReadNode(l.obj.kind(), l.tmpO);
        }
        GoExprNode newVal = new BinaryOpNode(delta > 0 ? BinaryOpNode.Op.ADD : BinaryOpNode.Op.SUB,
                tk, readLvalue(l, ctx, path), oneNode(tk));
        newVal.setSourceSection(sectionFor(node, "incdec"));
        GoStmtNode w = writeLvalue(l, newVal, ctx, path);
        w.setSourceSection(sectionFor(node, "incdec"));
        prog.add(w);
        return new BlockNode(prog.toArray(new GoStmtNode[0]));
    }

    private static GoExprNode oneNode(GoKind kind) {
        return switch (kind) {
            case FLOAT32 -> ConstNode.ofFloat(kind, Float.floatToIntBits(1f), 0L);
            case FLOAT64, UNTYPED_FLOAT -> ConstNode.ofFloat(kind, 0, Double.doubleToLongBits(1d));
            default -> ConstNode.ofInt(kind, 1L);
        };
    }

    // ------------------------------------------------------------------
    // M4 (WS E): control-flow statement builders.
    // ------------------------------------------------------------------

    private GoStmtNode buildIf(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "if");
        String path = "if.fields";
        GoStmtNode init = null;
        if (f.opt("init") instanceof Json.Object initObj) {
            init = buildStmt(initObj, ctx);
        }
        GoExprNode cond = buildExpr(f.object("cond", path), ctx);
        GoStmtNode then = buildStmt(f.object("then", path), ctx);
        GoStmtNode els = null;
        if (f.opt("else") instanceof Json.Object elseObj) {
            els = buildStmt(elseObj, ctx);
        }
        return new IfNode(init, cond, then, els);
    }

    private GoStmtNode buildFor(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "for");
        String path = "for.fields";
        // Consume the pending label BEFORE building the body, so a nested
        // loop/switch doesn't steal it.
        String label = ctx.pendingLabel;
        ctx.pendingLabel = null;
        // M6: pre-scan for per-iteration (boxed) variables before building.
        int loopId = nextLoopId++;
        List<Integer> headerOids = prescanForBoxes(node, f, path, ctx, loopId);
        for (int oid : headerOids) {
            int iterSlot = ctx.frames.addSlot(FrameSlotKind.Object, "boxiter$" + oid, null);
            ctx.boxIterSlots.put(oid, iterSlot);
            // Record the kind for BoxRefreshNode.
            Json.Value e = ctx.objects.get(oid);
            if (e instanceof Json.Object o) {
                int typeIdx = o.intValue("type", "objects[" + oid + "]");
                ctx.boxKinds.put(oid, kindOf(ctx, typeIdx, "objects[" + oid + "]"));
            }
        }
        GoStmtNode init = null;
        loopStack.push(new LoopPhase(loopId, LP_INIT));
        try {
            if (f.opt("init") instanceof Json.Object initObj) {
                init = buildStmt(initObj, ctx);
            }
        } finally {
            loopStack.pop();
        }
        GoExprNode cond = null;
        loopStack.push(new LoopPhase(loopId, LP_COND));
        try {
            if (f.opt("cond") instanceof Json.Object condObj) {
                cond = buildExpr(condObj, ctx);
            }
        } finally {
            loopStack.pop();
        }
        GoStmtNode post = null;
        loopStack.push(new LoopPhase(loopId, LP_POST));
        try {
            if (f.opt("post") instanceof Json.Object postObj) {
                post = buildStmt(postObj, ctx);
            }
        } finally {
            loopStack.pop();
        }
        GoStmtNode body;
        loopStack.push(new LoopPhase(loopId, LP_BODY));
        try {
            body = buildStmt(f.object("body", path), ctx);
        } finally {
            loopStack.pop();
        }
        if (!headerOids.isEmpty()) {
            // gc loopvar model (verified against cmd/compile/internal/loopvar):
            // init defines the carry cells (z'); each iteration runs the
            // refresh prologue (fresh per-iteration cell z := z'), then —
            // unless it is the first iteration (tmp_first) — the post
            // statement on the fresh cell, then the condition, then the
            // body, then the write-back (z' = z). continue is rewritten to
            // the write-back; break/panic/return/goto-out skip it.
            int firstIterSlot = ctx.frames.addSlot(FrameSlotKind.Boolean, "loopfirst$" + loopId, null);
            List<GoStmtNode> prologue = new ArrayList<>(headerOids.size());
            List<GoStmtNode> writeback = new ArrayList<>(headerOids.size());
            for (int oid : headerOids) {
                Integer carrySlot = ctx.objSlots.get(oid);
                Integer iterSlot = ctx.boxIterSlots.get(oid);
                GoKind kind = ctx.boxKinds.get(oid);
                if (carrySlot == null || iterSlot == null || kind == null) {
                    throw new FrontendException(path + ": boxed header var " + oid
                            + " missing cell slot");
                }
                prologue.add(new BoxRefreshNode(kind, iterSlot, carrySlot));
                writeback.add(new BoxWritebackNode(kind, iterSlot, carrySlot));
            }
            GoStmtNode prologueNode = prologue.size() == 1 ? prologue.get(0)
                    : new BlockNode(prologue.toArray(new GoStmtNode[0]));
            GoStmtNode writebackNode = writeback.size() == 1 ? writeback.get(0)
                    : new BlockNode(writeback.toArray(new GoStmtNode[0]));
            ForStmtNode stmt = new ForStmtNode(init,
                    new GoLoopNode(new ForRepeatingNode(cond, post, body, prologueNode,
                            writebackNode, firstIterSlot)),
                    firstIterSlot);
            if (label != null) {
                stmt.setLabel(label);
            }
            return stmt;
        }
        ForStmtNode stmt = new ForStmtNode(init,
                new GoLoopNode(new ForRepeatingNode(cond, post, body)));
        if (label != null) {
            stmt.setLabel(label);
        }
        return stmt;
    }

    /** Frame slot for an already-declared object id (range `=` form). */
    private int slotForOid(BuildCtx ctx, int oid, String path) {
        if (globalSlots.containsKey(oid)) {
            // M7: ranging with `=` into a package-level variable would
            // need the range repeating nodes to write the globals frame
            // (v1 limitation, like captured variables below).
            throw new FrontendException(path + ": range assignment to a package-level variable"
                    + " is not supported in v1 (M7)");
        }
        VarResolution r = resolveVar(ctx, oid, path);
        if (r.depth > 0) {
            // M5 (WS F): ranging with `=` into a captured variable needs
            // CapWriteNode support in the range nodes (v1 limitation).
            throw new FrontendException(path + ": range assignment to captured variable"
                    + " is not supported in v1 (WS F)");
        }
        return r.slot;
    }

    /**
     * Frame slot for a range `=` target (P0-D4). A boxed for-header
     * variable of an enclosing loop denotes that loop's per-iteration
     * variable inside the loop body (gc's {@code loopvar} desugar), so the
     * assignment must hit the per-iteration cell, not the carry cell. The
     * end-of-iteration write-back propagates it, exactly like a body
     * assignment. Captured (depth &gt; 0) targets keep the v1 limitation.
     */
    private int rangeAssignSlot(BuildCtx ctx, int oid, String path) {
        if (globalSlots.containsKey(oid)) {
            // M7: see slotForOid — ranging with `=` into a package-level
            // variable is not supported in v1.
            throw new FrontendException(path + ": range assignment to a package-level variable"
                    + " is not supported in v1 (M7)");
        }
        VarResolution r = resolveVar(ctx, oid, path);
        if (r.depth > 0) {
            throw new FrontendException(path + ": range assignment to captured variable"
                    + " is not supported in v1 (WS F)");
        }
        int iter = headerIterSlot(ctx, oid);
        if (iter >= 0) {
            return iter;
        }
        return r.slot;
    }

    /** Type-table entry with alias/named layers resolved (for detail access). */
    private static Json.Object underlyingEntry(BuildCtx ctx, int typeId, String path) {
        Json.Object entry = ctx.types.entry(resolveAlias(ctx, typeId, path), path);
        while ("named".equals(entry.string("kind", path))) {
            typeId = entry.object("detail", path).intValue("underlying", path);
            entry = ctx.types.entry(typeId, path);
        }
        return entry;
    }

    private GoStmtNode buildRange(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "range");
        String path = "range.fields";
        // Consume the pending label BEFORE building the body.
        String label = ctx.pendingLabel;
        ctx.pendingLabel = null;
        GoExprNode x = buildExpr(f.object("x", path), ctx);
        int xtid = x.typeId();
        if (xtid < 0) {
            throw new FrontendException(path + ": range operand without a type id (WS E)");
        }
        GoKind xk = kindOf(ctx, xtid, path);
        Json.Object detail = underlyingEntry(ctx, xtid, path).object("detail", path);
        GoKind keyKind;
        GoKind valKind;
        boolean isIntRange = xk.isIntKind();
        // P3-M9 (WS E): range over *[N]T (pointer to array) — dereference
        // the pointer, then range over the array.
        GoExprNode rangeX = x;
        if (xk == GoKind.POINTER) {
            int elemTid = detail.intValue("elem", path);
            GoKind elemKind = kindOf(ctx, elemTid, path);
            if (elemKind == GoKind.ARRAY) {
                rangeX = new DerefNode(GoKind.ARRAY, x);
                // Update xk and detail for the array case below.
                xk = GoKind.ARRAY;
                detail = underlyingEntry(ctx, elemTid, path).object("detail", path);
            }
        }
        switch (xk) {
            case SLICE, ARRAY -> {
                int elemTid = detail.intValue("elem", path);
                keyKind = GoKind.INT64;
                valKind = kindOf(ctx, elemTid, path);
            }
            case STRING -> {
                keyKind = GoKind.INT64;
                valKind = GoKind.INT32; // rune
            }
            case MAP -> {
                keyKind = kindOf(ctx, detail.intValue("key", path), path);
                valKind = kindOf(ctx, detail.intValue("elem", path), path);
            }
            default -> {
                // P3-M9 (WS E): Go 1.22+ integer ranges: for i := range n.
                if (isIntRange) {
                    keyKind = GoKind.INT64;
                    valKind = GoKind.INT64; // unused
                } else {
                    throw new FrontendException(path + ": cannot range over " + xk + " (WS E)");
                }
            }
        }
        boolean define = f.bool("define", path);
        // M6: pre-scan for per-iteration (boxed) range/body variables.
        prescanRangeBoxes(node, f, path, ctx);
        int keySlot = -1;
        int valSlot = -1;
        int keyOid = -1;
        int valOid = -1;
        if (f.opt("key") instanceof Json.Num kn) {
            int koid = (int) kn.asLong(path);
            keyOid = koid;
            keySlot = define ? declareObj(ctx, koid, path) : rangeAssignSlot(ctx, koid, path);
        }
        if (f.opt("value") instanceof Json.Num vn) {
            int void_ = (int) vn.asLong(path);
            valOid = void_;
            valSlot = define ? declareObj(ctx, void_, path) : rangeAssignSlot(ctx, void_, path);
        }
        boolean keyBoxed = keyOid >= 0 && ctx.boxed.contains(keyOid);
        boolean valBoxed = valOid >= 0 && ctx.boxed.contains(valOid);
        GoStmtNode body = buildStmt(f.object("body", path), ctx);
        // M10, F-1: per-invocation range iteration state lives in frame
        // slots, never in node fields (the node object is shared by every
        // goroutine executing the same compiled function). Allocate hidden
        // slots for the state each Range*Node needs.
        int rangeSeq = ctx.frames.addSlot(FrameSlotKind.Boolean, "rangestarted$", null);
        com.oracle.truffle.api.nodes.RepeatingNode repeating;
        if (isIntRange) {
            // P3-M9 (WS E): Go 1.22+ integer range: for i := range n.
            int boundSlot = ctx.frames.addSlot(FrameSlotKind.Long, "rangebound$", null);
            int indexSlot = ctx.frames.addSlot(FrameSlotKind.Long, "rangeindex$", null);
            repeating = new RangeIntNode(rangeX, body, keySlot, keyKind, keyBoxed, define,
                    rangeSeq, boundSlot, indexSlot);
        } else {
            repeating = switch (xk) {
                case SLICE -> {
                    int sliceSlot = ctx.frames.addSlot(FrameSlotKind.Object, "rangeslice$", null);
                    int indexSlot = ctx.frames.addSlot(FrameSlotKind.Int, "rangeindex$", null);
                    yield new RangeSliceNode(rangeX, body, keySlot, keyKind, keyBoxed, define,
                            valSlot, valKind, valBoxed, define, rangeSeq, sliceSlot, indexSlot);
                }
                case ARRAY -> {
                    int arraySlot = ctx.frames.addSlot(FrameSlotKind.Object, "rangearray$", null);
                    int indexSlot = ctx.frames.addSlot(FrameSlotKind.Int, "rangeindex$", null);
                    yield new RangeArrayNode(rangeX, body, keySlot, keyKind, keyBoxed, define,
                            valSlot, valKind, valBoxed, define, rangeSeq, arraySlot, indexSlot);
                }
                case STRING -> {
                    int bytesSlot = ctx.frames.addSlot(FrameSlotKind.Object, "rangebytes$", null);
                    int posSlot = ctx.frames.addSlot(FrameSlotKind.Int, "rangepos$", null);
                    yield new RangeStringNode(rangeX, body, keySlot, keyKind, keyBoxed, define,
                            valSlot, valBoxed, define, rangeSeq, bytesSlot, posSlot);
                }
                case MAP -> {
                    int mapSlot = ctx.frames.addSlot(FrameSlotKind.Object, "rangemap$", null);
                    int keysSlot = ctx.frames.addSlot(FrameSlotKind.Object, "rangekeys$", null);
                    int indexSlot = ctx.frames.addSlot(FrameSlotKind.Int, "rangeindex$", null);
                    yield new RangeMapNode(rangeX, body, keySlot, keyKind, keyBoxed, define,
                            valSlot, valKind, valBoxed, define, rangeSeq, mapSlot, keysSlot, indexSlot);
                }
                default -> throw new FrontendException(path + ": cannot range over " + xk + " (WS E)");
            };
        }
        RangeStmtNode stmt = new RangeStmtNode(null, new GoLoopNode(repeating));
        if (label != null) {
            stmt.setLabel(label);
        }
        return stmt;
    }

    /** Builds a statement list (case-clause bodies) as a block. */
    private GoStmtNode buildStmtList(Json.Array stmts, BuildCtx ctx, String path) {
        List<GoStmtNode> out = new ArrayList<>(stmts.size());
        for (int i = 0; i < stmts.size(); i++) {
            out.add(buildStmt(asObject(stmts.get(i), path + "[" + i + "]"), ctx));
        }
        return new BlockNode(out.toArray(new GoStmtNode[0]));
    }

    private GoStmtNode buildSwitch(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "switch");
        String path = "switch.fields";
        // Consume the pending label BEFORE building the clauses.
        String label = ctx.pendingLabel;
        ctx.pendingLabel = null;
        GoStmtNode init = null;
        if (f.opt("init") instanceof Json.Object initObj) {
            init = buildStmt(initObj, ctx);
        }
        GoExprNode tag = null;
        GoKind tagKind = GoKind.BOOL; // `switch {}`: cases are booleans
        int tagTypeId = -1;
        graalgo.values.GoTypeDescriptor tagDesc = null;
        if (f.opt("tag") instanceof Json.Object tagObj) {
            tag = buildExpr(tagObj, ctx);
            tagTypeId = tag.typeId();
            if (tagTypeId < 0) {
                throw new FrontendException(path + ": switch tag without a type id (WS E)");
            }
            tagKind = kindOf(ctx, tagTypeId, path);
            if (tagKind == GoKind.INTERFACE) {
                tagDesc = typeDescriptorFor(ctx, tagTypeId, path);
            }
        }
        Json.Array clauses = f.array("clauses", path);
        List<GoExprNode[]> caseExprs = new ArrayList<>(clauses.size());
        List<graalgo.values.GoTypeDescriptor[]> caseDescs = new ArrayList<>(clauses.size());
        boolean[] isDefault = new boolean[clauses.size()];
        List<GoStmtNode> bodies = new ArrayList<>(clauses.size());
        for (int i = 0; i < clauses.size(); i++) {
            String cp = path + ".clauses[" + i + "]";
            Json.Object c = asObject(clauses.get(i), cp);
            Json.Object cf = c.object("fields", cp);
            boolean def = cf.bool("default", cp);
            isDefault[i] = def;
            List<GoExprNode> exprs = new ArrayList<>();
            List<graalgo.values.GoTypeDescriptor> descs = new ArrayList<>();
            if (!def) {
                Json.Array cases = cf.array("cases", cp);
                for (int j = 0; j < cases.size(); j++) {
                    String ep = cp + ".cases[" + j + "]";
                    GoExprNode ce = buildExpr(asObject(cases.get(j), ep), ctx);
                    if (tagKind != GoKind.INTERFACE) {
                        // Untyped case constants take the tag's type
                        // (Go == semantics: carriers must match for GoEquality).
                        ce = maybeConvert(ce, tagKind, tagTypeId, ctx, ep);
                    }
                    exprs.add(ce);
                    // Dynamic-type descriptor for interface-tag comparison:
                    // untyped constants take their default type.
                    GoKind ck = ce.kind();
                    graalgo.values.GoTypeDescriptor cd;
                    if (ck.isUntyped()) {
                        // P3-M7R2: same default-type descriptor as boxing —
                        // "int", not "int64" — or case/int-tag comparisons
                        // mint a different instance than the boxed tag.
                        cd = untypedDefaultDescriptor(ctx, ce, defaultKind(ck), ep);
                    } else if (ce.typeId() >= 0) {
                        cd = typeDescriptorFor(ctx, ce.typeId(), ep);
                    } else {
                        cd = null;
                    }
                    descs.add(cd);
                }
            }
            caseExprs.add(exprs.toArray(new GoExprNode[0]));
            caseDescs.add(descs.toArray(new graalgo.values.GoTypeDescriptor[0]));
            bodies.add(buildStmt(cf.object("body", cp), ctx));
        }
        SwitchNode stmt = new SwitchNode(init, tag, tagKind, tagDesc,
                caseExprs.toArray(new GoExprNode[0][]),
                caseDescs.toArray(new graalgo.values.GoTypeDescriptor[0][]),
                isDefault, bodies.toArray(new GoStmtNode[0]));
        if (label != null) {
            stmt.setLabel(label);
        }
        return stmt;
    }

    private GoStmtNode buildTypeSwitch(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "typeswitch");
        String path = "typeswitch.fields";
        GoStmtNode init = null;
        if (f.opt("init") instanceof Json.Object initObj) {
            init = buildStmt(initObj, ctx);
        }
        GoExprNode tag = buildExpr(f.object("tag", path), ctx);
        int tagTypeId = tag.typeId();
        if (tagTypeId < 0) {
            throw new FrontendException(path + ": type switch tag without a type id (WS E)");
        }
        if (kindOf(ctx, tagTypeId, path) != GoKind.INTERFACE) {
            throw new FrontendException(path + ": type switch on non-interface type (WS E)");
        }
        graalgo.values.GoTypeDescriptor ifaceDesc = typeDescriptorFor(ctx, tagTypeId, path);
        Json.Array clauses = f.array("clauses", path);
        List<TypeSwitchNode.TypeCaseClause> out = new ArrayList<>(clauses.size());
        java.util.Map<String, java.util.Set<String>> dynSets = null;
        for (int i = 0; i < clauses.size(); i++) {
            String cp = path + ".clauses[" + i + "]";
            Json.Object c = asObject(clauses.get(i), cp);
            Json.Object cf = c.object("fields", cp);
            boolean isDefault = cf.bool("default", cp);
            boolean nilCase = cf.bool("nilcase", cp);
            List<graalgo.values.GoTypeDescriptor> types = new ArrayList<>();
            List<java.util.List<String>> ifaceMethods = new ArrayList<>();
            boolean hasIface = false;
            Json.Value typesVal = cf.opt("types");
            if (typesVal instanceof Json.Array typeIds) {
                for (int j = 0; j < typeIds.size(); j++) {
                    int tid = (int) ((Json.Num) typeIds.get(j)).asLong(cp);
                    types.add(typeDescriptorFor(ctx, tid, cp));
                    // P3-M8b: interface clause types need their method
                    // implkeys for the implements check (null otherwise).
                    java.util.List<String> im = null;
                    if (kindOf(ctx, tid, cp) == GoKind.INTERFACE) {
                        java.util.Set<String> keys = interfaceMethodImplKeys(ctx, tid, cp);
                        if (!keys.isEmpty()) {
                            im = new java.util.ArrayList<>(keys);
                            hasIface = true;
                        }
                    }
                    ifaceMethods.add(im);
                }
            }
            if (hasIface && dynSets == null) {
                dynSets = dynamicMethodSets(ctx, path);
            }
            // null types array: `case nil:` (no asserted types).
            int bindSlot = -1;
            GoKind bindKind = null;
            if (cf.opt("bindobj") instanceof Json.Num bn) {
                int boid = (int) bn.asLong(cp);
                int btid = cf.intValue("bindtype", cp);
                bindKind = kindOf(ctx, btid, cp);
                bindSlot = declareObj(ctx, boid, cp);
            }
            GoStmtNode body = buildStmt(cf.object("body", cp), ctx);
            out.add(new TypeSwitchNode.TypeCaseClause(types, ifaceMethods, nilCase, isDefault,
                    bindSlot, bindKind, body));
        }
        return new TypeSwitchNode(tag, init, out, ifaceDesc, dynSets);
    }

    private GoStmtNode buildBranch(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "branch");
        String path = "branch.fields";
        String op = f.string("op", path);
        String label = f.opt("label") instanceof Json.Str s ? s.value() : null;
        if ("goto".equals(op) && label == null) {
            throw new FrontendException(path + ": goto without a label (WS E)");
        }
        return new BranchNode(BranchNode.Op.fromWire(op, path), label);
    }

    private GoStmtNode buildLabeled(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "labeled");
        String path = "labeled.fields";
        String label = f.string("label", path);
        // Loops and switches consume the pending label for labeled
        // break/continue; the wrapper still carries it for goto dispatch.
        ctx.pendingLabel = label;
        GoStmtNode inner;
        try {
            inner = buildStmt(f.object("stmt", path), ctx);
        } finally {
            ctx.pendingLabel = null;
        }
        return new LabeledStmtNode(label, inner);
    }

    // ------------------------------------------------------------------
    // M4 (WS E): composite-type expression builders.
    // ------------------------------------------------------------------

    /**
     * Recursive zero template for STRUCT/ARRAY kinds, null for all other
     * kinds (P3-M9): nodes that materialize a zero of the kind at runtime
     * (new(), map-index misses, lenient type asserts, nil) carry this
     * template and deep-copy it per evaluation — the descriptor-only
     * GoStruct.zero leaves nested struct fields null.
     */
    private Object zeroTemplateOrNull(GoKind kind, int typeId, BuildCtx ctx, String path) {
        return (kind == GoKind.STRUCT || kind == GoKind.ARRAY)
                ? zeroTemplateFor(kind, typeId, ctx, path) : null;
    }

    /** Zero-value template for a composite literal element/field type. */
    private Object zeroTemplateFor(GoKind kind, int typeId, BuildCtx ctx, String path) {
        if (kind == GoKind.STRUCT) {
            // fix-linkname: sync primitive fields get usable Java objects,
            // not empty structs (mirrors fieldZeroTemplate).
            String prim = syncPrimitiveName(ctx, typeId, path);
            if (prim != null) {
                return newSyncPrimitive(prim);
            }
            return structZeroTemplate(ctx, typeId, path);
        }
        if (kind == GoKind.ARRAY) {
            return zeroArrayFor(typeId, ctx, path);
        }
        return Boxing.zero(kind, null);
    }

    /**
     * Fresh zero-initialized Java array for a Go array type (M4, WS E):
     * recursively initializes nested array/struct/string elements so no
     * element is null.
     */
    private Object zeroArrayFor(int typeId, BuildCtx ctx, String path) {
        Json.Object e = underlyingEntry(ctx, typeId, path);
        Json.Object d = e.object("detail", path);
        int len = d.intValue("len", path);
        int elemTid = d.intValue("elem", path);
        GoKind ek = kindOf(ctx, elemTid, path);
        Object arr = Boxing.newArray(ek, len);
        if (ek == GoKind.ARRAY || ek == GoKind.STRUCT || ek == GoKind.STRING) {
            Object[] objs = (Object[]) arr;
            for (int i = 0; i < len; i++) {
                objs[i] = switch (ek) {
                    case ARRAY -> zeroArrayFor(elemTid, ctx, path);
                    // P3-M9 (driver-harden, go-cmp lane): sync primitive
                    // slots get fresh instances (never shared templates).
                    case STRUCT -> {
                        String prim = syncPrimitiveName(ctx, elemTid, path);
                        if (prim != null) {
                            yield newSyncPrimitive(prim);
                        }
                        yield Boxing.freshZeroCopy(structZeroTemplate(ctx, elemTid, path));
                    }
                    default -> GoString.EMPTY;
                };
            }
        }
        return arr;
    }

    private static int structFieldIndex(graalgo.values.GoStructDescriptor desc,
                                        String name, String path) {
        List<graalgo.values.GoStructDescriptor.Field> fields = desc.fields();
        for (int i = 0; i < fields.size(); i++) {
            if (fields.get(i).name().equals(name)) {
                return i;
            }
        }
        throw new FrontendException(path + ": no such struct field \"" + name + "\" (WS E)");
    }

    private GoExprNode buildComplit(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "complit");
        String path = "complit.fields";
        int typeId = f.intValue("type", path);
        GoKind kind = kindOf(ctx, typeId, path);
        boolean keyed = f.bool("keyed", path);
        Json.Array values = f.array("values", path);
        Json.Object entry = underlyingEntry(ctx, typeId, path);
        String ek = entry.string("kind", path);
        boolean isPointer = "pointer".equals(ek);
        Json.Object compEntry = entry;
        int compTypeId = typeId;
        if (isPointer) {
            compTypeId = entry.object("detail", path).intValue("elem", path);
            compEntry = underlyingEntry(ctx, compTypeId, path);
            ek = compEntry.string("kind", path);
            if (!"struct".equals(ek)) {
                throw new FrontendException(path + ": pointer composite literal to non-struct (WS E)");
            }
        }
        Json.Object compDetail = compEntry.object("detail", path);
        GoExprNode[] valueNodes;
        // P3-M9 (x509): compact hex encoding for large byte arrays. The
        // gofrontend emits "hexdata" (hex string) instead of "values" for
        // byte literals with >2000 elements (e.g., crypto tables). Decode
        // directly to avoid 88k JSON nodes.
        if (f.has("hexdata")) {
            String hex = f.string("hexdata", path);
            int n = hex.length() / 2;
            valueNodes = new GoExprNode[n];
            for (int i = 0; i < n; i++) {
                int b = Integer.parseInt(hex.substring(i*2, i*2+2), 16);
                valueNodes[i] = ConstNode.ofInt(GoKind.UINT8, b);
            }
        } else {
            valueNodes = new GoExprNode[values.size()];
            for (int i = 0; i < values.size(); i++) {
                valueNodes[i] = buildExpr(asObject(values.get(i), path + ".values[" + i + "]"), ctx);
            }
        }
        // M7: convert literal values to the composite element/field type.
        // Without this, a concrete value stored into an `any` element (e.g.
        // map[string]any{"k": p} with p a nil *int) is never boxed in a
        // GoInterface, so a later read loses the dynamic type (typed nil
        // compared equal to nil). maybeConvert is a no-op for compatible
        // kinds. Nil literals are skipped: nil is already the zero value
        // for every nillable type and needs no conversion.
        if ("map".equals(ek) || "slice".equals(ek) || "array".equals(ek)) {
            int elemTid = compDetail.intValue("elem", path);
            GoKind elemKind = kindOf(ctx, elemTid, path);
            for (int i = 0; i < valueNodes.length; i++) {
                if (!(valueNodes[i] instanceof NilNode)) {
                    valueNodes[i] = maybeConvert(valueNodes[i], elemKind, elemTid, ctx,
                            path + ".values[" + i + "]");
                }
            }
        }
        // Keyed keys: index expressions for map/array/slice, field names for structs.
        GoExprNode[] keyNodes = new GoExprNode[0];
        int[] fieldIdx = null;
        if (keyed) {
            if ("struct".equals(ek)) {
                Json.Array fks = f.array("fieldkeys", path);
                graalgo.values.GoStructDescriptor sd =
                        structDescriptorFor(ctx, compTypeId, path);
                fieldIdx = new int[fks.size()];
                for (int i = 0; i < fks.size(); i++) {
                    String name = ((Json.Str) fks.get(i)).value();
                    fieldIdx[i] = structFieldIndex(sd, name, path);
                }
            } else {
                Json.Array ks = f.array("keys", path);
                keyNodes = new GoExprNode[ks.size()];
                for (int i = 0; i < ks.size(); i++) {
                    keyNodes[i] = buildExpr(asObject(ks.get(i), path + ".keys[" + i + "]"), ctx);
                }
                if ("map".equals(ek)) {
                    // P3-M9 (bug-028): map literal keys must be converted
                    // to the map's key type (interface boxing for
                    // map[any]), like the index-expression paths.
                    int keyTid = compDetail.intValue("key", path);
                    GoKind keyKind = kindOf(ctx, keyTid, path);
                    for (int i = 0; i < keyNodes.length; i++) {
                        keyNodes[i] = maybeConvert(keyNodes[i], keyKind, keyTid, ctx,
                                path + ".keys[" + i + "]");
                    }
                }
            }
        }
        GoExprNode lit = switch (ek) {
            case "slice" -> {
                int elemTid = compDetail.intValue("elem", path);
                GoKind elemKind = kindOf(ctx, elemTid, path);
                yield new CompositeLitNode(kind, keyNodes, fieldIdx, valueNodes, keyed,
                        isPointer, elemKind, null, 0, null, null,
                        zeroTemplateFor(elemKind, elemTid, ctx, path));
            }
            case "array" -> {
                int elemTid = compDetail.intValue("elem", path);
                int len = compDetail.intValue("len", path);
                GoKind elemKind = kindOf(ctx, elemTid, path);
                yield new CompositeLitNode(kind, keyNodes, fieldIdx, valueNodes, keyed,
                        isPointer, elemKind, null, len, null, null,
                        zeroTemplateFor(elemKind, elemTid, ctx, path));
            }
            case "map" -> {
                int keyTid = compDetail.intValue("key", path);
                int elemTid = compDetail.intValue("elem", path);
                yield new CompositeLitNode(kind, keyNodes, fieldIdx, valueNodes, keyed,
                        isPointer, kindOf(ctx, elemTid, path), kindOf(ctx, keyTid, path),
                        0, null, null, null);
            }
            case "struct" -> {
                graalgo.values.GoStructDescriptor sd =
                        structDescriptorFor(ctx, compTypeId, path);
                int[] ftids = structFieldTypeIds(ctx, compTypeId, path);
                // M7: convert each field value to its declared field type
                // (same interface-boxing rationale as map/slice/array above).
                // fieldIdx maps value position -> struct field for keyed
                // literals; unkeyed literals are positional. Nil literals are
                // skipped (nil needs no conversion).
                for (int i = 0; i < valueNodes.length; i++) {
                    if (valueNodes[i] instanceof NilNode) {
                        continue;
                    }
                    int fi = (fieldIdx != null) ? fieldIdx[i] : i;
                    GoKind fkind = sd.fields().get(fi).kind();
                    valueNodes[i] = maybeConvert(valueNodes[i], fkind, ftids[fi], ctx,
                            path + ".values[" + i + "]");
                }
                Object[] fieldZeros = new Object[sd.fields().size()];
                for (int i = 0; i < fieldZeros.length; i++) {
                    fieldZeros[i] = zeroTemplateFor(sd.fields().get(i).kind(), ftids[i], ctx, path);
                }
                yield new CompositeLitNode(kind, keyNodes, fieldIdx, valueNodes, keyed,
                        isPointer, null, null, 0, sd, fieldZeros, null);
            }
            default -> throw new FrontendException(path + ": composite literal of " + ek + " (WS E)");
        };
        // P3-M8b: the literal carries its wire type id (needed to box the
        // value into an interface, e.g. append([]DirEntry{}, &dirEntry{})).
        lit.setTypeId(typeId);
        return lit;
    }

    /**
     * Converts a map index key to the map's key type (P3-M9, bug-028).
     * Go's index expression must be assignable to the key type; for an
     * interface key type this boxes the key into a {@link GoInterface}
     * carrying the dynamic type descriptor. Without this, the key was
     * boxed at its static kind: {@code m[1]} and {@code m[int64(1)]} on a
     * {@code map[any]} both canonicalized to {@code Long(1)} in
     * {@link MapKeys#box} and silently aliased, because Go's rule — the
     * dynamic type is part of an interface key's identity — was lost.
     * {@link #maybeConvert} is a no-op when the kinds already agree, so
     * concrete-keyed maps are unaffected.
     */
    private GoExprNode convertMapKey(GoExprNode key, int mapTypeId, BuildCtx ctx, String path) {
        Json.Object detail = underlyingEntry(ctx, mapTypeId, path).object("detail", path);
        int keyTid = detail.intValue("key", path);
        GoKind keyKind = kindOf(ctx, keyTid, path);
        return maybeConvert(key, keyKind, keyTid, ctx, path);
    }

    private GoExprNode buildIndex(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "index");
        String path = "index.fields";
        GoExprNode container = buildExpr(f.object("x", path), ctx);
        GoExprNode key = buildExpr(f.object("index", path), ctx);
        int ctid = container.typeId();
        if (ctid < 0) {
            throw new FrontendException(path + ": index container without a type id (WS E)");
        }
        GoKind containerKind = kindOf(ctx, ctid, path);
        if (containerKind == GoKind.MAP) {
            key = convertMapKey(key, ctid, ctx, path);
        }
        int elemTid = f.intValue("type", path);
        GoKind elemKind = kindOf(ctx, elemTid, path);
        GoStructDescriptor elemDesc =
                elemKind == GoKind.STRUCT ? structDescriptorFor(ctx, elemTid, path) : null;
        return new IndexNode(container, key, containerKind, elemKind, elemDesc,
                zeroTemplateOrNull(elemKind, elemTid, ctx, path));
    }

    private GoExprNode buildSliceExpr(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "sliceexpr");
        String path = "sliceexpr.fields";
        GoExprNode x = buildExpr(f.object("x", path), ctx);
        int xtid = x.typeId();
        if (xtid < 0) {
            throw new FrontendException(path + ": slice operand without a type id (WS E)");
        }
        GoKind containerKind = kindOf(ctx, xtid, path);
        GoExprNode lo = null;
        GoExprNode hi = null;
        GoExprNode max = null;
        if (f.opt("lo") instanceof Json.Object loObj) {
            lo = buildExpr(loObj, ctx);
        }
        if (f.opt("hi") instanceof Json.Object hiObj) {
            hi = buildExpr(hiObj, ctx);
        }
        if (f.opt("max") instanceof Json.Object maxObj) {
            max = buildExpr(maxObj, ctx);
        }
        int typeId = f.intValue("type", path);
        GoKind nodeKind = kindOf(ctx, typeId, path);
        return new SliceExprNode(x, lo, hi, max, containerKind, nodeKind);
    }

    private GoExprNode buildSelector(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "selector");
        String path = "selector.fields";
        GoExprNode obj = buildExpr(f.object("x", path), ctx);
        Json.Array sp = f.array("selpath", path);
        int[] selpath = new int[sp.size()];
        for (int i = 0; i < sp.size(); i++) {
            selpath[i] = (int) ((Json.Num) sp.get(i)).asLong(path);
        }
        int fieldTid = f.intValue("type", path);
        GoKind fieldKind = kindOf(ctx, fieldTid, path);
        return new SelectorNode(obj, selpath, fieldKind);
    }

    private GoExprNode buildTypeAssert(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "typeassert");
        String path = "typeassert.fields";
        GoExprNode x = buildExpr(f.object("x", path), ctx);
        int assertTid = f.intValue("type", path);
        GoKind ak = kindOf(ctx, assertTid, path);
        graalgo.values.GoTypeDescriptor adesc = typeDescriptorFor(ctx, assertTid, path);
        // P3-M3 (WS D): assertion to a non-empty interface is allowed; the
        // implements check runs at runtime (see buildCommaOk).
        List<String> ifaceMethods = null;
        Map<String, Set<String>> dynMethods = null;
        if (ak == GoKind.INTERFACE && !adesc.isEmptyInterface()) {
            ifaceMethods = new ArrayList<>(interfaceMethodImplKeys(ctx, assertTid, path));
            dynMethods = dynamicMethodSets(ctx, path);
        }
        GoStructDescriptor sd =
                ak == GoKind.STRUCT ? structDescriptorFor(ctx, assertTid, path) : null;
        // Single-valued form: a failed assertion panics (M5, WS F: gc's
        // interface-conversion panic).
        return new TypeAssertNode(x, ak, adesc, false, sd,
                GoTypeStrings.of(ctx, x.typeId()), GoTypeStrings.of(ctx, assertTid),
                ifaceMethods, dynMethods,
                zeroTemplateOrNull(ak, assertTid, ctx, path));
    }

    private GoExprNode buildNil(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "nil");
        String path = "nil.fields";
        int typeId = f.intValue("type", path);
        GoKind kind = kindOf(ctx, typeId, path);
        GoStructDescriptor sd =
                kind == GoKind.STRUCT ? structDescriptorFor(ctx, typeId, path) : null;
        NilNode n = new NilNode(kind, sd, zeroTemplateOrNull(kind, typeId, ctx, path));
        n.setTypeId(typeId);
        return n;
    }
}
