package graalgo.frontend;

import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.FrameSlotKind;
import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.source.SourceSection;
import com.oracle.truffle.api.source.Source;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
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
        /** Canonical dynamic-type descriptors per type-table id (M4, WS E). */
        public final Map<Integer, graalgo.values.GoTypeDescriptor> typeDescs = new HashMap<>();
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

        /** Declared result type-table id (-1 if the function has no result). */
        public int resultTypeId = -1;
        /** Function name for Go stack traces (M5, WS F). */
        public String funcName = "main";
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
    private final Map<String, ExprBuilder> exprBuilders = new LinkedHashMap<>();
    private final Map<String, StmtBuilder> stmtBuilders = new LinkedHashMap<>();
    private final List<TypeDecoder> typeDecoders = new ArrayList<>();
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
        this.source = source;
        registerExprKind("ident", this::buildIdent);
        registerExprKind("basiclit", this::buildBasicLit);
        registerExprKind("binary", this::buildBinary);
        registerExprKind("unary", this::buildUnary);
        registerExprKind("paren", this::buildParen);
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
            int slot = allocGlobalSlot(gctx, typeId, varName, gpath);
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
            GlobalWriteNode w = new GlobalWriteNode(kind, globalsFrame, slot, init);
            w.setSourceSection(sectionFor(e, gpath));
            out.add(w);
        }
        return out;
    }

    public Program buildProgram(String jsonText) {
        Json.Value v = Json.parse(jsonText);
        if (!(v instanceof Json.Object doc)) {
            throw new FrontendException("document: must be a JSON object");
        }
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
                GoFuncRootNode root = buildNamedFunc(fn, fields, ctx);
                RootCallTarget target = root.getCallTarget();
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
            int slot = allocSlot(pname, oid, typeId, path + ".params[" + i + "]", ctx);
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
            resultSlot = allocSlot(rname.isEmpty() ? "result" : rname, roid, resultTypeId,
                    path + ".results[0]", ctx);
            ctx.resultSlot = resultSlot;
            ctx.resultKind = resultKind;
            ctx.resultTypeId = resultTypeId;
        }
        GoStmtNode body = buildStmt(f.object("body", path), ctx);
        // Zero the result slot on entry (Go semantics: results start at zero
        // values, so a bare `return` yields zeros).
        GoStmtNode entry = body;
        if (resultSlot >= 0) {
            GoStmtNode zero = new AssignNode(resultSlot, resultKind,
                    zeroNode(resultKind, resultTypeId, ctx, path));
            entry = new BlockNode(new GoStmtNode[]{zero, body});
        }
        GoFuncRootNode root = new GoFuncRootNode(ctx.frames.build(), entry, closureSlot,
                paramSlots, paramKinds, resultSlot, resultKind, funcName);
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
        return source.createSection(pos.intValue("line", p), pos.intValue("col", p), pos.intValue("len", p));
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
    static GoKind kindOf(BuildCtx ctx, int typeId, String path) {
        Json.Object entry = ctx.types.entry(typeId, path);
        while (true) {
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
        Json.Object entry = ctx.types.entry(typeId, path);
        while ("alias".equals(entry.string("kind", path))) {
            typeId = entry.object("detail", path).intValue("rhs", path);
            entry = ctx.types.entry(typeId, path);
        }
        return typeId;
    }

    /**
     * Canonical {@link GoStructDescriptor} for a struct type-table id
     * (M4, WS E). Descriptor identity is type identity: two values of the
     * same struct type always share the instance (map keys and struct
     * equality hash by it).
     */
    static GoStructDescriptor structDescriptorFor(BuildCtx ctx, int typeId, String path) {
        typeId = resolveAlias(ctx, typeId, path);
        GoStructDescriptor d = ctx.structDescs.get(typeId);
        if (d != null) {
            return d;
        }
        Json.Object entry = ctx.types.entry(typeId, path);
        while ("named".equals(entry.string("kind", path))) {
            typeId = entry.object("detail", path).intValue("underlying", path);
            entry = ctx.types.entry(typeId, path);
        }
        if (!"struct".equals(entry.string("kind", path))) {
            throw new FrontendException(path + ": not a struct type");
        }
        Json.Array fields = entry.object("detail", path).array("fields", path);
        List<String> names = new ArrayList<>(fields.size());
        List<GoKind> kinds = new ArrayList<>(fields.size());
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
        }
        d = GoStructDescriptor.layout("struct#" + typeId, names, kinds);
        ctx.structDescs.put(typeId, d);
        ctx.structFieldTypeIds.put(typeId, fieldTypeIds);
        return d;
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
        while ("named".equals(entry.string("kind", path))) {
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
        Json.Object entry = ctx.types.entry(typeId, path);
        String k = entry.string("kind", path);
        if ("basic".equals(k)) {
            return basicDescriptorFor(kindOf(ctx, typeId, path));
        }
        if (isStructuralComposite(k)) {
            String key = structuralKey(ctx, typeId, path);
            graalgo.values.GoTypeDescriptor d = STRUCTURAL_DESCS.get(key);
            if (d == null) {
                d = new graalgo.values.GoTypeDescriptor(k + "#" + Integer.toHexString(key.hashCode()),
                        kindOf(ctx, typeId, path), false);
                graalgo.values.GoTypeDescriptor prev =
                        STRUCTURAL_DESCS.putIfAbsent(key, d);
                if (prev != null) {
                    d = prev;
                }
            }
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
            name = entry.object("detail", path).string("name", path);
            graalgo.values.GoTypeDescriptor nd = NAMED_DESCS.get(name);
            if (nd == null) {
                nd = new graalgo.values.GoTypeDescriptor(name,
                        kindOf(ctx, typeId, path), false);
                graalgo.values.GoTypeDescriptor prev = NAMED_DESCS.putIfAbsent(name, nd);
                if (prev != null) {
                    nd = prev;
                }
            }
            ctx.typeDescs.put(typeId, nd);
            return nd;
        } else if ("interface".equals(k)) {
            emptyIface = entry.object("detail", path).bool("empty", path);
            name = emptyIface ? "any" : "interface#" + typeId;
        } else {
            name = k + "#" + typeId;
        }
        d = new graalgo.values.GoTypeDescriptor(name, kindOf(ctx, typeId, path), emptyIface);
        ctx.typeDescs.put(typeId, d);
        return d;
    }

    /** Unnamed composite kinds canonicalized by structure (M6, WS H). */
    private static boolean isStructuralComposite(String k) {
        return "struct".equals(k) || "array".equals(k) || "slice".equals(k)
                || "map".equals(k) || "pointer".equals(k);
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
            default:
                // interface, signature, invalid: program-local identity.
                return k + "#" + typeId;
        }
    }

    /** Canonical dynamic-type descriptor per basic kind (M4, WS E). */
    private static final Map<GoKind, graalgo.values.GoTypeDescriptor> BASIC_DESCS = new HashMap<>();

    public static graalgo.values.GoTypeDescriptor basicDescriptorFor(GoKind kind) {
        return BASIC_DESCS.computeIfAbsent(kind,
                k -> new graalgo.values.GoTypeDescriptor(k.name().toLowerCase(), k, false));
    }

    /**
     * True if the descriptor is one of the canonical predeclared-basic
     * descriptors (M5, WS F): used by panic rendering to distinguish
     * {@code 5} (bare) from {@code main.MyInt(5)} (named).
     */
    public static boolean isPredeclaredBasic(graalgo.values.GoTypeDescriptor d) {
        return BASIC_DESCS.containsValue(d);
    }

    /** Default kind of an untyped constant kind (for interface boxing). */
    static GoKind defaultKind(GoKind untyped) {
        return switch (untyped) {
            case UNTYPED_INT -> GoKind.INT64;
            case UNTYPED_FLOAT -> GoKind.FLOAT64;
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
    }

    /** Zero template for one struct field / array element (null when trivially zero). */
    private static Object fieldZeroTemplate(BuildCtx ctx, GoKind kind, int fieldTypeId, String path) {
        return switch (kind) {
            case STRING -> GoString.EMPTY;
            case STRUCT -> structZeroTemplate(ctx, fieldTypeId, path);
            case ARRAY -> arrayZeroTemplate(ctx, fieldTypeId, path);
            default -> null;
        };
    }

    private static Object arrayZeroTemplate(BuildCtx ctx, int arrayTypeId, String path) {
        int rid = resolveAlias(ctx, arrayTypeId, path);
        Json.Object entry = ctx.types.entry(rid, path);
        while ("named".equals(entry.string("kind", path))) {
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
            // evaluation its own copy.
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
     * types passes only for the empty interface, and concrete→non-empty-
     * interface is a factory error (needs method tables, WS D).
     */
    private GoExprNode maybeConvert(GoExprNode child, GoKind to, int toTypeId, BuildCtx ctx, String path) {
        return maybeConvert(child, to, toTypeId, ctx, path, null);
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
        if (child instanceof NilNode && isNillable(to)) {
            return child;
        }
        if (GoKind.transparent(from, to)) {
            return child;
        }
        if (to == GoKind.INTERFACE) {
            if (from == GoKind.INTERFACE) {
                if (isEmptyInterface(ctx, toTypeId, path)) {
                    return child;
                }
                throw new FrontendException(path + ": interface-to-interface conversion needs"
                        + " method tables (WS D)");
            }
            if (!isEmptyInterface(ctx, toTypeId, path)) {
                throw new FrontendException(path + ": conversion to non-empty interface needs"
                        + " method tables (WS D)");
            }
            return boxInterface(child, ctx, path);
        }
        ConvertNode n = new ConvertNode(from, to, child, sliceElemKind);
        n.setSourceSection(child.getSourceSection());
        return n;
    }

    /**
     * Boxes a concrete value into an interface (M4, WS E). The dynamic type
     * is the value's static type id (named types stay distinct); an untyped
     * constant takes its default type.
     */
    private GoExprNode boxInterface(GoExprNode child, BuildCtx ctx, String path) {
        GoKind from = child.kind();
        GoKind dataKind;
        graalgo.values.GoTypeDescriptor descriptor;
        if (from.isUntyped()) {
            dataKind = defaultKind(from);
            descriptor = basicDescriptorFor(dataKind);
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
        Integer gslot = globalSlots.get(oid);
        if (gslot != null) {
            return new GlobalReadNode(kind, globalsFrame, gslot);
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
            if (captured.contains(oid)) {
                ctx.boxed.add(oid);
                headerLoopIds.put(oid, loopId);
                headerOids.add(oid);
            }
        }
        for (int oid : bodyDecls) {
            if (captured.contains(oid)) {
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
            if (captured.contains(oid)) {
                ctx.boxed.add(oid);
            }
        }
        for (int oid : bodyDecls) {
            if (captured.contains(oid)) {
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
        return switch (kind) {
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
        return new UnaryOpNode(op, kind, maybeConvert(buildExpr(f.object("x", path), ctx), kind, path));
    }

    private GoExprNode buildParen(Json.Object node, BuildCtx ctx) {
        return buildExpr(fieldsOf(node, "paren").object("x", "paren.fields"), ctx);
    }

    /** Builds a Go conversion {@code T(x)} (frontend {@code convert} node). */
    private GoExprNode buildConvert(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "convert");
        String path = "convert.fields";
        int toId = f.intValue("to", path);
        GoKind to = kindOf(ctx, toId, path);
        GoExprNode x = buildExpr(f.object("x", path), ctx);
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
            return new FuncValueCallNode(lit, args, callResultKind(node, ctx, path));
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
                    callResultKind(node, ctx, path));
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
            return new FuncValueCallNode(fn, args, callResultKind(node, ctx, path));
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
            int paramTid = resolveAlias(ctx, paramTypeIds.get(i), path);
            Json.Object pentry = ctx.types.entry(paramTid, path);
            if (!"interface".equals(pentry.string("kind", path))) {
                continue;
            }
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
        // M7: element kind of a nested array/slice element (for correct
        // recursive printing); null unless elemKind is ARRAY or SLICE.
        GoKind[] elemElemKinds = new GoKind[n];
        GoKind[] keyKinds = new GoKind[n];
        String[] keyTypeNames = new String[n];
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
                // M7: if the element is itself an array/slice, capture its
                // element kind for nested printing.
                if (elemKinds[i] == GoKind.ARRAY || elemKinds[i] == GoKind.SLICE) {
                    Json.Object elemEntry = underlyingEntry(ctx, elemId, apath);
                    Json.Object elemDetail = elemEntry.object("detail", apath);
                    int elemElemId = elemDetail.intValue("elem", apath);
                    elemElemKinds[i] = kindOf(ctx, elemElemId, apath);
                }
            } else if ("map".equals(k)) {
                int keyId = detail.intValue("key", apath);
                int elemId = detail.intValue("elem", apath);
                keyKinds[i] = kindOf(ctx, keyId, apath);
                keyTypeNames[i] = GoTypeStrings.of(ctx, keyId);
                elemKinds[i] = kindOf(ctx, elemId, apath);
                elemTypeNames[i] = GoTypeStrings.of(ctx, elemId);
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
                elemElemKinds, keyKinds, keyTypeNames, resultKind);
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
                yield new DeleteNode(args[0], args[1]);
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
                return new MakeNode(elemKind, args[0], args.length == 2 ? args[1] : null);
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
        return new NewNode(elemKind, desc);
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
        GoKind elemKind = builtinElemKind(ctx, f.intValue("type", path), path, "append");
        // M7: append(s, x...) with a string x spreads the string's bytes.
        boolean ellipsis = false;
        Json.Value e = f.opt("ellipsis");
        if (e instanceof Json.Bool b) {
            ellipsis = b.value();
        }
        return new AppendNode(args[0], elemKind, Arrays.copyOfRange(args, 1, args.length), ellipsis);
    }

    /** {@code copy(dst, src)}: slice-to-slice only (string sources are WS D). */
    private GoExprNode buildCopyBuiltin(GoExprNode[] args, Json.Object f, BuildCtx ctx,
            String path) {
        requireArgs(path, "copy", args, 2);
        if (args[1].kind() == GoKind.STRING) {
            throw new FrontendException(path + ": copy from a string source is not supported"
                    + " in frontend/v1 scope (WS D: string<->[]byte conversions)");
        }
        GoKind elemKind = builtinElemKind(ctx, args[0].typeId(), path, "copy");
        return new CopyNode(args[0], args[1], elemKind);
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
        } else {
            throw new FrontendException(cpath + ": cannot defer call to " + funKind + " (WS D)");
        }
        DeferNode n = new DeferNode(fn, args, null, kinds);
        n.setSourceSection(sectionFor(node, "defer"));
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
        // Slot 0: the $closure slot (parent frame or null).
        int closureSlot = frames.addSlot(FrameSlotKind.Object, "$closure", null);
        fctx.closureSlot = closureSlot;
        // Params.
        Json.Array params = f.array("params", path);
        int[] paramSlots = new int[params.size()];
        GoKind[] paramKinds = new GoKind[params.size()];
        for (int i = 0; i < params.size(); i++) {
            Json.Object p = asObject(params.get(i), path + ".params[" + i + "]");
            Json.Object pf = p.object("fields", path + ".params[" + i + "]");
            String pname = pf.string("name", path);
            int oid = pf.intValue("obj", path);
            int typeId = pf.intValue("type", path);
            GoKind kind = kindOf(ctx, typeId, path);
            int slot = allocSlot(pname, oid, typeId, path + ".params[" + i + "]", fctx);
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
            int rslot = allocSlot(rname.isEmpty() ? "result" : rname, roid, rtypeId,
                    path + ".results[0]", fctx);
            fctx.resultSlot = rslot;
            fctx.resultKind = rkind;
            fctx.resultTypeId = rtypeId;
        }
        GoStmtNode body = buildBlock(f.object("body", path), fctx);
        // Zero the result slot on entry (Go semantics: results start at zero
        // values, so a bare `return` yields zeros).
        GoStmtNode entry = body;
        if (fctx.resultSlot >= 0) {
            GoStmtNode zero = new AssignNode(fctx.resultSlot, fctx.resultKind,
                    zeroNode(fctx.resultKind, resultTypeId(f, ctx, path), fctx, path));
            entry = new BlockNode(new GoStmtNode[]{zero, body});
        }
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
                    snapSlots);
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
                if (kind == GoKind.STRUCT) {
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
        // Aliases and named types resolve before decoders run.
        int rid = resolveAlias(ctx, typeId, path);
        Json.Object entry = ctx.types.entry(rid, path);
        while ("named".equals(entry.string("kind", path))) {
            rid = entry.object("detail", path).intValue("underlying", path);
            entry = ctx.types.entry(rid, path);
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
                    + " (WS C/D hook: NodeFactory.registerTypeDecoder)");
        }
        int slot = ctx.frames.addSlot(slotKind, varName, null);
        if (ctx.objSlots.putIfAbsent(oid, slot) != null) {
            throw new FrontendException(path + ": duplicate object id " + oid);
        }
        ctx.slotsByName.put(varName, slot);
        ctx.kindsByName.put(varName, kindOf(ctx, typeId, path));
        return slot;
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
        List<GoStmtNode> prog = new ArrayList<>();
        for (Lhs l : lhss) {
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
        return l.kind;
    }

    private static int targetTypeId(Lhs l) {
        if (l.isIndex) {
            return l.elemTypeId;
        }
        if (l.isSelector) {
            return l.fieldTypeId;
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
        if (l.isGlobal) {
            return new GlobalWriteNode(l.kind, globalsFrame, l.slot, value);
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
        if (l.isGlobal) {
            return new GlobalReadNode(l.kind, globalsFrame, l.slot);
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
            return new IndexNode(l.container, l.key, l.containerKind, l.elemKind, ed);
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
                    new VarReadNode(key.kind(), tmpK), containerKind, elemKind, ed);
            okNode = new IndexOkNode(new VarReadNode(container.kind(), tmpC),
                    new VarReadNode(key.kind(), tmpK));
        } else if ("typeassert".equals(vk)) {
            GoExprNode iface = buildExpr(vf.object("x", path), ctx);
            int tmpI = newTmp(iface.kind(), ctx);
            prog.add(new AssignNode(tmpI, iface.kind(), iface, false));
            int assertTypeId = vf.intValue("type", path);
            GoKind ak = kindOf(ctx, assertTypeId, path);
            graalgo.values.GoTypeDescriptor adesc = typeDescriptorFor(ctx, assertTypeId, path);
            if (ak == GoKind.INTERFACE && !adesc.isEmptyInterface()) {
                throw new FrontendException(path + ": assertion to non-empty interface needs"
                        + " method tables (WS D)");
            }
            GoStructDescriptor sd = ak == GoKind.STRUCT
                    ? structDescriptorFor(ctx, assertTypeId, path) : null;
            valueNode = new TypeAssertNode(new VarReadNode(iface.kind(), tmpI), ak, adesc, true, sd,
                    GoTypeStrings.of(ctx, iface.typeId()), GoTypeStrings.of(ctx, assertTypeId));
            okNode = new TypeAssertOkNode(new VarReadNode(iface.kind(), tmpI), adesc);
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
            return new ReturnNode(value, ctx.resultSlot, ctx.resultKind);
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
            default -> throw new FrontendException(path + ": cannot range over " + xk + " (WS E)");
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
        com.oracle.truffle.api.nodes.RepeatingNode repeating = switch (xk) {
            case SLICE -> new RangeSliceNode(x, body, keySlot, keyKind, keyBoxed, define,
                    valSlot, valKind, valBoxed, define);
            case ARRAY -> new RangeArrayNode(x, body, keySlot, keyKind, keyBoxed, define,
                    valSlot, valKind, valBoxed, define);
            case STRING -> new RangeStringNode(x, body, keySlot, keyKind, keyBoxed, define,
                    valSlot, valBoxed, define);
            case MAP -> new RangeMapNode(x, body, keySlot, keyKind, keyBoxed, define,
                    valSlot, valKind, valBoxed, define);
            default -> throw new FrontendException(path + ": cannot range over " + xk + " (WS E)");
        };
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
                    if (ck == GoKind.UNTYPED_INT) {
                        cd = basicDescriptorFor(GoKind.INT64);
                    } else if (ck == GoKind.UNTYPED_FLOAT) {
                        cd = basicDescriptorFor(GoKind.FLOAT64);
                    } else if (ck == GoKind.UNTYPED_STRING) {
                        cd = basicDescriptorFor(GoKind.STRING);
                    } else if (ck == GoKind.UNTYPED_BOOL) {
                        cd = basicDescriptorFor(GoKind.BOOL);
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
        for (int i = 0; i < clauses.size(); i++) {
            String cp = path + ".clauses[" + i + "]";
            Json.Object c = asObject(clauses.get(i), cp);
            Json.Object cf = c.object("fields", cp);
            boolean isDefault = cf.bool("default", cp);
            boolean nilCase = cf.bool("nilcase", cp);
            List<graalgo.values.GoTypeDescriptor> types = new ArrayList<>();
            Json.Value typesVal = cf.opt("types");
            if (typesVal instanceof Json.Array typeIds) {
                for (int j = 0; j < typeIds.size(); j++) {
                    int tid = (int) ((Json.Num) typeIds.get(j)).asLong(cp);
                    types.add(typeDescriptorFor(ctx, tid, cp));
                }
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
            out.add(new TypeSwitchNode.TypeCaseClause(types, nilCase, isDefault,
                    bindSlot, bindKind, body));
        }
        return new TypeSwitchNode(tag, init, out, ifaceDesc);
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

    /** Zero-value template for a composite literal element/field type. */
    private Object zeroTemplateFor(GoKind kind, int typeId, BuildCtx ctx, String path) {
        if (kind == GoKind.STRUCT) {
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
                    case STRUCT -> Boxing.freshZeroCopy(structZeroTemplate(ctx, elemTid, path));
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
        GoExprNode[] valueNodes = new GoExprNode[values.size()];
        for (int i = 0; i < values.size(); i++) {
            valueNodes[i] = buildExpr(asObject(values.get(i), path + ".values[" + i + "]"), ctx);
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
            }
        }
        return switch (ek) {
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
        int elemTid = f.intValue("type", path);
        GoKind elemKind = kindOf(ctx, elemTid, path);
        GoStructDescriptor elemDesc =
                elemKind == GoKind.STRUCT ? structDescriptorFor(ctx, elemTid, path) : null;
        return new IndexNode(container, key, containerKind, elemKind, elemDesc);
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
        if (ak == GoKind.INTERFACE && !adesc.isEmptyInterface()) {
            throw new FrontendException(path + ": assertion to non-empty interface needs"
                    + " method tables (WS D)");
        }
        GoStructDescriptor sd =
                ak == GoKind.STRUCT ? structDescriptorFor(ctx, assertTid, path) : null;
        // Single-valued form: a failed assertion panics (M5, WS F: gc's
        // interface-conversion panic).
        return new TypeAssertNode(x, ak, adesc, false, sd,
                GoTypeStrings.of(ctx, x.typeId()), GoTypeStrings.of(ctx, assertTid));
    }

    private GoExprNode buildNil(Json.Object node, BuildCtx ctx) {
        Json.Object f = fieldsOf(node, "nil");
        String path = "nil.fields";
        int typeId = f.intValue("type", path);
        GoKind kind = kindOf(ctx, typeId, path);
        GoStructDescriptor sd =
                kind == GoKind.STRUCT ? structDescriptorFor(ctx, typeId, path) : null;
        return new NilNode(kind, sd);
    }
}
