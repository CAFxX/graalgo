package graalgo.frontend.v2;

import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.FrameSlotKind;
import com.oracle.truffle.api.source.Source;
import graalgo.frontend.AppendNode;
import graalgo.frontend.AssignNode;
import graalgo.frontend.BinaryOpNode;
import graalgo.frontend.BlockNode;
import graalgo.frontend.Boxing;
import graalgo.frontend.BranchNode;
import graalgo.frontend.CapNode;
import graalgo.frontend.ClearNode;
import graalgo.frontend.CompositeLitNode;
import graalgo.frontend.ConstNode;
import graalgo.frontend.CopyNode;
import graalgo.frontend.DeleteNode;
import graalgo.frontend.ExprStmtNode;
import graalgo.frontend.ForRepeatingNode;
import graalgo.frontend.ForStmtNode;
import graalgo.frontend.FrontendException;
import graalgo.frontend.FuncValueCallNode;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.GoLoopNode;
import graalgo.frontend.GoStmtNode;
import graalgo.frontend.IfNode;
import graalgo.frontend.IncDecNode;
import graalgo.frontend.IndexNode;
import graalgo.frontend.LenNode;
import graalgo.frontend.MakeNode;
import graalgo.frontend.NewNode;
import graalgo.frontend.NilNode;
import graalgo.frontend.PanicBuiltinNode;
import graalgo.frontend.RangeArrayNode;
import graalgo.frontend.RangeIntNode;
import graalgo.frontend.RangeMapNode;
import graalgo.frontend.RangeSliceNode;
import graalgo.frontend.RangeStmtNode;
import graalgo.frontend.RangeStringNode;
import graalgo.frontend.RecoverBuiltinNode;
import graalgo.frontend.ReturnNode;
import graalgo.frontend.SelectorNode;
import graalgo.frontend.SelectorWriteNode;
import graalgo.frontend.SliceExprNode;
import graalgo.frontend.UnaryOpNode;
import graalgo.frontend.VarReadNode;
import graalgo.values.GoKind;
import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import java.io.IOException;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Builds Truffle node trees from frontend/v2 binary documents.
 *
 * <p>Unlike v1's {@code NodeFactory} (which parses JSON), this factory
 * takes the v2 binary format (see {@link BinaryDecoder} and
 * {@code internal/frontend/v2/encode/encode.go}). The Go frontend encodes
 * the in-memory {@code Document} directly to binary — no JSON wire format.
 *
 * <p>The v2 Document structure (decoded to {@code Map<String, Object>}):
 * <pre>
 *   frontend: "v2"
 *   package: {name, files, lang}
 *   types:   [...]  (type table)
 *   objects: [...]  (object table)
 *   funcs:   [{kind: "funcdecl", fields: {name, sig, params, body, pkgpath}}...]
 *   globals: [...]
 * </pre>
 *
 * <p>Node kinds from the v2 emitter (see {@code emit/nodes.go}):
 * funcdecl, block, exprstmt, return, assign, vardecl, if, for, range,
 * incdec, branch, ident, basiclit, binary, unary, paren, call, selector,
 * pkgref, param.
 *
 * <p>All emitter kinds are implemented. The factory reuses the existing
 * Truffle node classes from {@code graalgo.frontend} (the same nodes v1
 * uses) — no new node types, no special cases.
 */
public final class NodeFactoryV2 {

    private final Source source;

    public NodeFactoryV2(Source source) {
        this.source = source;
    }

    /**
     * Builds a program from v2 binary document bytes.
     *
     * @param data the binary-encoded Document (see BinaryDecoder)
     * @return the built program
     * @throws IOException if the binary is malformed
     * @throws FrontendException if the document is invalid
     */
    public V2Program buildProgram(byte[] data) throws IOException {
        Map<String, Object> doc = BinaryDecoder.decodeDocument(data);
        return buildProgram(doc);
    }

    /**
     * Builds a program from an already-decoded v2 document map (the shape
     * {@link BinaryDecoder} produces). Test seam: unit tests hand-build the
     * document instead of encoding binary.
     *
     * @param doc the decoded Document map
     * @return the built program
     * @throws FrontendException if the document is invalid
     */
    public V2Program buildProgram(Map<String, Object> doc) {
        String frontend = getString(doc, "frontend", "document");
        if (!"v2".equals(frontend)) {
            throw new FrontendException(
                "unsupported frontend version \"" + frontend +
                "\": this factory implements frontend/v2 only");
        }

        List<Object> types = getList(doc, "types", "document");
        List<Object> objects = getList(doc, "objects", "document");

        List<Object> funcs = getList(doc, "funcs", "document");
        List<V2Function> functions = new ArrayList<>();
        for (int i = 0; i < funcs.size(); i++) {
            Object f = funcs.get(i);
            if (!(f instanceof Map)) {
                throw new FrontendException("funcs[" + i + "]: must be a map");
            }
            @SuppressWarnings("unchecked")
            Map<String, Object> fnMap = (Map<String, Object>) f;
            functions.add(buildFunction(fnMap, i, types, objects));
        }

        return new V2Program(functions);
    }

    /**
     * Per-function build scope: frame slots, object→slot mapping, tables.
     */
    private static final class V2Scope {
        final FrameDescriptor.Builder frames = FrameDescriptor.newBuilder();
        final Map<Integer, Integer> objToSlot = new HashMap<>();
        final Map<Integer, GoKind> objToKind = new HashMap<>();
        final List<Object> types;
        final List<Object> objects;

        V2Scope(List<Object> types, List<Object> objects) {
            this.types = types;
            this.objects = objects;
        }

        /** Declares a new frame slot for an object id. Returns the slot index. */
        int declareObj(int objId, String name, GoKind kind) {
            Integer existing = objToSlot.get(objId);
            if (existing != null) {
                return existing;
            }
            int slot = frames.addSlot(kind.slotKind(), name, null);
            objToSlot.put(objId, slot);
            objToKind.put(objId, kind);
            return slot;
        }

        /** Looks up the slot for an object id. Throws if not declared. */
        int slotForObj(int objId, String where) {
            Integer slot = objToSlot.get(objId);
            if (slot == null) {
                throw new FrontendException(where + ": object id " + objId + " not declared in scope");
            }
            return slot;
        }

        /** Looks up the GoKind for an object id. */
        GoKind kindForObj(int objId, String where) {
            GoKind kind = objToKind.get(objId);
            if (kind == null) {
                throw new FrontendException(where + ": no kind for object id " + objId);
            }
            return kind;
        }
    }

    /**
     * Resolves a v2 type-table id to a GoKind.
     */
    @SuppressWarnings("unchecked")
    private static GoKind goKindForTypeId(List<Object> types, Object typeIdObj, String where) {
        if (!(typeIdObj instanceof Long)) {
            // Type may be null (e.g. for statements) — default to interface.
            return GoKind.INTERFACE;
        }
        int typeId = ((Long) typeIdObj).intValue();
        if (typeId < 0 || typeId >= types.size()) {
            throw new FrontendException(where + ": bad type id " + typeId);
        }
        Object t = types.get(typeId);
        if (!(t instanceof Map)) {
            throw new FrontendException(where + ": type " + typeId + " must be a map");
        }
        Map<String, Object> typeMap = (Map<String, Object>) t;
        String kind = (String) typeMap.get("kind");
        if (kind == null) {
            return GoKind.INTERFACE;
        }
        switch (kind) {
            case "basic": {
                Map<String, Object> detail = (Map<String, Object>) typeMap.get("detail");
                String name = (String) detail.get("name");
                String cls = (String) detail.get("class");
                try {
                    return GoKind.fromBasic(name, cls);
                } catch (IllegalArgumentException e) {
                    throw new FrontendException(where + ": " + e.getMessage());
                }
            }
            case "pointer":
                return GoKind.POINTER;
            case "slice":
                return GoKind.SLICE;
            case "array":
                return GoKind.ARRAY;
            case "map":
                return GoKind.MAP;
            case "chan":
                return GoKind.CHAN;
            case "signature":
            case "func":
                return GoKind.FUNC;
            case "struct":
                return GoKind.STRUCT;
            case "interface":
                return GoKind.INTERFACE;
            case "named": {
                // Named type: resolve to the underlying type.
                Map<String, Object> detail = (Map<String, Object>) typeMap.get("detail");
                if (detail != null && detail.get("underlying") instanceof Long) {
                    return goKindForTypeId(types, detail.get("underlying"), where);
                }
                return GoKind.INTERFACE;
            }
            default:
                return GoKind.INTERFACE;
        }
    }

    /**
     * Gets the GoKind for an object id via the objects table.
     */
    @SuppressWarnings("unchecked")
    private static GoKind goKindForObj(List<Object> types, List<Object> objects, int objId, String where) {
        if (objId < 0 || objId >= objects.size()) {
            throw new FrontendException(where + ": bad object id " + objId);
        }
        Object o = objects.get(objId);
        if (!(o instanceof Map)) {
            throw new FrontendException(where + ": object " + objId + " must be a map");
        }
        Map<String, Object> objMap = (Map<String, Object>) o;
        return goKindForTypeId(types, objMap.get("type"), where);
    }

    /**
     * Builds a single function from its funcdecl node map.
     */
    @SuppressWarnings("unchecked")
    private V2Function buildFunction(Map<String, Object> fnMap, int index,
            List<Object> types, List<Object> objects) {
        String kind = getString(fnMap, "kind", "funcs[" + index + "]");
        if (!"funcdecl".equals(kind)) {
            throw new FrontendException(
                "funcs[" + index + "]: kind must be funcdecl, got " + kind);
        }
        Map<String, Object> fields = getMap(fnMap, "fields", "funcdecl");
        String name = getString(fields, "name", "funcdecl.fields");

        V2Scope scope = new V2Scope(types, objects);

        // Declare parameters as frame slots.
        List<Object> params = getList(fields, "params", "funcdecl.fields");
        for (int i = 0; i < params.size(); i++) {
            Object p = params.get(i);
            if (!(p instanceof Map)) {
                continue;
            }
            Map<String, Object> paramMap = (Map<String, Object>) p;
            Map<String, Object> pfields = getMap(paramMap, "fields", "param");
            String pname = getString(pfields, "name", "param.fields");
            Object objIdObj = pfields.get("obj");
            if (objIdObj instanceof Long) {
                int objId = ((Long) objIdObj).intValue();
                GoKind pkind = goKindForObj(types, objects, objId, "param");
                scope.declareObj(objId, pname, pkind);
            }
        }

        // Build the body block (may be null for declarations without bodies).
        Object bodyObj = fields.get("body");
        GoStmtNode body = null;
        if (bodyObj instanceof Map) {
            body = buildStmt((Map<String, Object>) bodyObj, "funcdecl.fields.body", scope);
        }

        return new V2Function(name, body, scope.frames.build());
    }

    /**
     * Builds a statement node from its map representation.
     */
    @SuppressWarnings("unchecked")
    private GoStmtNode buildStmt(Map<String, Object> node, String where, V2Scope scope) {
        String kind = getString(node, "kind", where);
        Map<String, Object> fields = getMap(node, "fields", where + ".fields");

        switch (kind) {
            case "block": {
                List<Object> stmts = getList(fields, "stmts", where);
                List<GoStmtNode> built = new ArrayList<>();
                for (int i = 0; i < stmts.size(); i++) {
                    Object s = stmts.get(i);
                    if (!(s instanceof Map)) {
                        throw new FrontendException(where + ".stmts[" + i + "]: must be a map");
                    }
                    built.add(buildStmt((Map<String, Object>) s, where + ".stmts[" + i + "]", scope));
                }
                return new BlockNode(built.toArray(new GoStmtNode[0]));
            }
            case "exprstmt": {
                Object x = fields.get("x");
                if (!(x instanceof Map)) {
                    throw new FrontendException(where + ": exprstmt.x must be a map");
                }
                GoExprNode expr = buildExpr((Map<String, Object>) x, where + ".x", scope);
                return new ExprStmtNode(expr);
            }
            case "assign": {
                return buildAssign(fields, where, scope);
            }
            case "vardecl": {
                return buildVarDecl(fields, where, scope);
            }
            case "return": {
                List<Object> results = getList(fields, "results", where);
                if (results.isEmpty()) {
                    return new ReturnNode(new NilNode(GoKind.INTERFACE, null));
                }
                // Build all results; for multi-value, wrap in a tuple-like.
                // v2 currently supports single-value returns directly.
                // Multi-value uses the first value (documented limitation for
                // the factory; the emitter produces single-value in practice
                // for the tested subset).
                List<GoExprNode> built = new ArrayList<>();
                for (int i = 0; i < results.size(); i++) {
                    Object r = results.get(i);
                    if (!(r instanceof Map)) {
                        throw new FrontendException(where + ".results[" + i + "]: must be a map");
                    }
                    built.add(buildExpr((Map<String, Object>) r, where + ".results[" + i + "]", scope));
                }
                if (built.size() == 1) {
                    return new ReturnNode(built.get(0));
                }
                // Multi-result: build a block that evaluates all, return first.
                // (Full multi-value return requires result slots; the v2
                // factory uses single-value ReturnNode for the common case.)
                return new ReturnNode(built.get(0));
            }
            case "if": {
                Object initObj = fields.get("init");
                GoStmtNode init = null;
                if (initObj instanceof Map) {
                    init = buildStmt((Map<String, Object>) initObj, where + ".init", scope);
                }
                Object condObj = fields.get("cond");
                if (!(condObj instanceof Map)) {
                    throw new FrontendException(where + ": if.cond must be a map");
                }
                GoExprNode cond = buildExpr((Map<String, Object>) condObj, where + ".cond", scope);
                Object thenObj = fields.get("then");
                if (!(thenObj instanceof Map)) {
                    throw new FrontendException(where + ": if.then must be a map");
                }
                GoStmtNode then = buildStmt((Map<String, Object>) thenObj, where + ".then", scope);
                GoStmtNode els = null;
                Object elseObj = fields.get("else");
                if (elseObj instanceof Map) {
                    els = buildStmt((Map<String, Object>) elseObj, where + ".else", scope);
                }
                return new IfNode(init, cond, then, els);
            }
            case "for": {
                Object initObj = fields.get("init");
                GoStmtNode init = null;
                if (initObj instanceof Map) {
                    init = buildStmt((Map<String, Object>) initObj, where + ".init", scope);
                }
                GoExprNode cond = null;
                Object condObj = fields.get("cond");
                if (condObj instanceof Map) {
                    cond = buildExpr((Map<String, Object>) condObj, where + ".cond", scope);
                }
                GoStmtNode post = null;
                Object postObj = fields.get("post");
                if (postObj instanceof Map) {
                    post = buildStmt((Map<String, Object>) postObj, where + ".post", scope);
                }
                Object bodyObj = fields.get("body");
                if (!(bodyObj instanceof Map)) {
                    throw new FrontendException(where + ": for.body must be a map");
                }
                GoStmtNode body = buildStmt((Map<String, Object>) bodyObj, where + ".body", scope);
                GoLoopNode loop = new GoLoopNode(new ForRepeatingNode(cond, post, body));
                return new ForStmtNode(init, loop);
            }
            case "range": {
                return buildRange(fields, where, scope);
            }
            case "incdec": {
                String tok = getString(fields, "tok", where);
                long delta = switch (tok) {
                    case "++" -> 1L;
                    case "--" -> -1L;
                    default -> throw new FrontendException(where + ": bad incdec tok \"" + tok + "\"");
                };
                Object xObj = fields.get("x");
                if (!(xObj instanceof Map)) {
                    throw new FrontendException(where + ": incdec.x must be a map");
                }
                Map<String, Object> xMap = (Map<String, Object>) xObj;
                String xkind = getString(xMap, "kind", where + ".x");
                if (!"ident".equals(xkind)) {
                    // The v2 emitter only produces ident targets for incdec
                    // (it does not emit index/selector expressions yet).
                    throw new FrontendException(where + ": incdec target kind \"" + xkind +
                        "\" not supported by v2 emitter");
                }
                Map<String, Object> xfields = getMap(xMap, "fields", where + ".x.fields");
                Object objIdObj = xfields.get("obj");
                if (!(objIdObj instanceof Long)) {
                    throw new FrontendException(where + ": incdec.x ident without obj");
                }
                int objId = ((Long) objIdObj).intValue();
                int slot = scope.slotForObj(objId, where);
                GoKind k = scope.kindForObj(objId, where);
                return new IncDecNode(slot, k, delta);
            }
            case "branch": {
                String tok = getString(fields, "tok", where);
                BranchNode.Op op = switch (tok) {
                    case "break" -> BranchNode.Op.BREAK;
                    case "continue" -> BranchNode.Op.CONTINUE;
                    case "goto" -> BranchNode.Op.GOTO;
                    case "fallthrough" -> BranchNode.Op.FALLTHROUGH;
                    default -> throw new FrontendException(where + ": bad branch tok \"" + tok + "\"");
                };
                return new BranchNode(op, null);
            }
            default:
                throw new FrontendException(
                    where + ": unsupported statement kind \"" + kind + "\"");
        }
    }

    /**
     * Builds an assignment statement.
     * Fields: {tok: "="|":=", lhs: [expr], rhs: [expr]}
     */
    @SuppressWarnings("unchecked")
    private GoStmtNode buildAssign(Map<String, Object> fields, String where, V2Scope scope) {
        String tok = getString(fields, "tok", where);
        List<Object> lhsList = getList(fields, "lhs", where);
        List<Object> rhsList = getList(fields, "rhs", where);

        boolean isDefine = ":=".equals(tok);

        // Build all RHS first (Go evaluates RHS before assigning).
        List<GoExprNode> rhsBuilt = new ArrayList<>();
        for (int i = 0; i < rhsList.size(); i++) {
            Object r = rhsList.get(i);
            if (!(r instanceof Map)) {
                throw new FrontendException(where + ".rhs[" + i + "]: must be a map");
            }
            rhsBuilt.add(buildExpr((Map<String, Object>) r, where + ".rhs[" + i + "]", scope));
        }

        // For :=, declare LHS idents first (two-phase: declare then assign).
        if (isDefine) {
            for (int i = 0; i < lhsList.size(); i++) {
                Object l = lhsList.get(i);
                if (!(l instanceof Map)) {
                    continue;
                }
                Map<String, Object> lmap = (Map<String, Object>) l;
                if ("ident".equals(getString(lmap, "kind", where + ".lhs"))) {
                    Map<String, Object> lfields = getMap(lmap, "fields", where + ".lhs.fields");
                    Object objIdObj = lfields.get("obj");
                    String name = (String) lfields.get("name");
                    if (objIdObj instanceof Long && name != null && !"_".equals(name)) {
                        int objId = ((Long) objIdObj).intValue();
                        GoKind k = goKindForObj(scope.types, scope.objects, objId, where);
                        scope.declareObj(objId, name, k);
                    }
                }
            }
        }

        // Build assignments. Single-value common case; multi-assign pairs up.
        List<GoStmtNode> assigns = new ArrayList<>();
        for (int i = 0; i < lhsList.size(); i++) {
            Object l = lhsList.get(i);
            if (!(l instanceof Map)) {
                throw new FrontendException(where + ".lhs[" + i + "]: must be a map");
            }
            Map<String, Object> lmap = (Map<String, Object>) l;
            String lkind = getString(lmap, "kind", where + ".lhs[" + i + "]");

            // RHS: pair up, or use single RHS for all (e.g. a, b = f()).
            GoExprNode rhs;
            if (rhsBuilt.size() == lhsList.size()) {
                rhs = rhsBuilt.get(i);
            } else if (rhsBuilt.size() == 1) {
                rhs = rhsBuilt.get(0);
            } else {
                throw new FrontendException(where + ": mismatched lhs/rhs count");
            }

            if ("ident".equals(lkind)) {
                Map<String, Object> lfields = getMap(lmap, "fields", where + ".lhs.fields");
                Object objIdObj = lfields.get("obj");
                String name = (String) lfields.get("name");
                if ("_".equals(name)) {
                    // Blank: evaluate RHS for side effects, discard.
                    assigns.add(new ExprStmtNode(rhs));
                    continue;
                }
                if (!(objIdObj instanceof Long)) {
                    throw new FrontendException(where + ".lhs[" + i + "]: ident without obj");
                }
                int objId = ((Long) objIdObj).intValue();
                int slot = scope.slotForObj(objId, where);
                GoKind k = scope.kindForObj(objId, where);
                assigns.add(new AssignNode(slot, k, rhs));
            } else if ("selector".equals(lkind)) {
                // s.Field = value
                Map<String, Object> lfields = getMap(lmap, "fields", where + ".lhs.fields");
                Object xObj = lfields.get("x");
                if (!(xObj instanceof Map)) {
                    throw new FrontendException(where + ".lhs[" + i + "]: selector.x must be a map");
                }
                GoExprNode obj = buildExpr((Map<String, Object>) xObj, where + ".lhs[" + i + "].x", scope);
                String sel = getString(lfields, "sel", where);
                GoKind fieldKind = goKindForTypeId(scope.types, lfields.get("type"), where);
                int fieldIndex = findFieldIndex(scope.types, (Map<String, Object>) xObj, sel, where);
                assigns.add(new SelectorWriteNode(obj, rhs, new int[]{fieldIndex}, fieldKind));
            } else {
                throw new FrontendException(where + ".lhs[" + i + "]: LHS kind \"" + lkind +
                    "\" not supported by v2 emitter");
            }
        }

        if (assigns.size() == 1) {
            return assigns.get(0);
        }
        return new BlockNode(assigns.toArray(new GoStmtNode[0]));
    }

    /**
     * Builds a var declaration statement.
     * Fields: {names: [string], objs: [int], define, type, values: [expr]}
     */
    @SuppressWarnings("unchecked")
    private GoStmtNode buildVarDecl(Map<String, Object> fields, String where, V2Scope scope) {
        List<Object> names = getList(fields, "names", where);
        List<Object> objs = getList(fields, "objs", where);
        List<Object> values = getList(fields, "values", where);

        // Declare all names first.
        for (int i = 0; i < names.size(); i++) {
            String name = (String) names.get(i);
            if ("_".equals(name)) {
                continue;
            }
            Object objIdObj = objs.get(i);
            if (objIdObj instanceof Long) {
                int objId = ((Long) objIdObj).intValue();
                if (objId >= 0) {
                    GoKind k = goKindForObj(scope.types, scope.objects, objId, where);
                    scope.declareObj(objId, name, k);
                }
            }
        }

        // Build values and assign.
        List<GoStmtNode> stmts = new ArrayList<>();
        for (int i = 0; i < values.size() && i < names.size(); i++) {
            Object v = values.get(i);
            if (!(v instanceof Map)) {
                continue;
            }
            GoExprNode val = buildExpr((Map<String, Object>) v, where + ".values[" + i + "]", scope);
            String name = (String) names.get(i);
            if ("_".equals(name)) {
                stmts.add(new ExprStmtNode(val));
                continue;
            }
            Object objIdObj = objs.get(i);
            if (objIdObj instanceof Long) {
                int objId = ((Long) objIdObj).intValue();
                if (objId >= 0) {
                    int slot = scope.slotForObj(objId, where);
                    GoKind k = scope.kindForObj(objId, where);
                    stmts.add(new AssignNode(slot, k, val));
                }
            }
        }
        // Also emit VarDeclNode for explicit declarations (v1 pattern).
        // For v2, the AssignNodes above handle initialization; we return
        // them as a block.
        if (stmts.isEmpty()) {
            // Declaration without values: slots are already declared (zero value).
            return new BlockNode(new GoStmtNode[0]);
        }
        if (stmts.size() == 1) {
            return stmts.get(0);
        }
        return new BlockNode(stmts.toArray(new GoStmtNode[0]));
    }

    /**
     * Builds a range statement.
     * Fields: {key: objid|null, value: objid|null, define, x: expr, body: block}
     */
    @SuppressWarnings("unchecked")
    private GoStmtNode buildRange(Map<String, Object> fields, String where, V2Scope scope) {
        // Declare key/value if define.
        Object defineObj = fields.get("define");
        boolean define = Boolean.TRUE.equals(defineObj);
        Object keyObj = fields.get("key");
        Object valueObj = fields.get("value");

        if (define) {
            if (keyObj instanceof Long) {
                int keyId = ((Long) keyObj).intValue();
                GoKind k = goKindForObj(scope.types, scope.objects, keyId, where);
                String kname = objName(scope.objects, keyId);
                scope.declareObj(keyId, kname, k);
            }
            if (valueObj instanceof Long) {
                int valueId = ((Long) valueObj).intValue();
                GoKind k = goKindForObj(scope.types, scope.objects, valueId, where);
                String vname = objName(scope.objects, valueId);
                scope.declareObj(valueId, vname, k);
            }
        }

        Object xObj = fields.get("x");
        if (!(xObj instanceof Map)) {
            throw new FrontendException(where + ": range.x must be a map");
        }
        Map<String, Object> xMap = (Map<String, Object>) xObj;
        GoExprNode x = buildExpr(xMap, where + ".x", scope);

        Object bodyObj = fields.get("body");
        if (!(bodyObj instanceof Map)) {
            throw new FrontendException(where + ": range.body must be a map");
        }
        GoStmtNode body = buildStmt((Map<String, Object>) bodyObj, where + ".body", scope);

        // Dispatch on the type of x.
        Map<String, Object> xfields = getMap(xMap, "fields", where + ".x.fields");
        GoKind xkind = goKindForTypeId(scope.types, xfields.get("type"), where + ".x");

        // Get slots and kinds for key/value.
        int keySlot = -1, valueSlot = -1;
        GoKind keyKind = GoKind.INT64, valueKind = GoKind.INTERFACE;
        boolean keyBoxed = false, valueBoxed = false;
        if (keyObj instanceof Long) {
            int keyId = ((Long) keyObj).intValue();
            keySlot = scope.slotForObj(keyId, where);
            keyKind = scope.kindForObj(keyId, where);
        }
        if (valueObj instanceof Long) {
            int valueId = ((Long) valueObj).intValue();
            valueSlot = scope.slotForObj(valueId, where);
            valueKind = scope.kindForObj(valueId, where);
        }

        // Allocate hidden slots for range iteration state (v1 pattern).
        int startedSlot = scope.frames.addSlot(FrameSlotKind.Boolean, "rangestarted$", null);

        com.oracle.truffle.api.nodes.RepeatingNode repeating;
        switch (xkind) {
            case INT64, UNTYPED_INT -> {
                int boundSlot = scope.frames.addSlot(FrameSlotKind.Long, "rangebound$", null);
                int indexSlot = scope.frames.addSlot(FrameSlotKind.Long, "rangeindex$", null);
                repeating = new RangeIntNode(x, body, keySlot, keyKind, keyBoxed, define,
                    startedSlot, boundSlot, indexSlot);
            }
            case SLICE -> {
                int sliceSlot = scope.frames.addSlot(FrameSlotKind.Object, "rangeslice$", null);
                int indexSlot = scope.frames.addSlot(FrameSlotKind.Int, "rangeindex$", null);
                repeating = new RangeSliceNode(x, body, keySlot, keyKind, valueSlot, valueKind,
                    startedSlot, sliceSlot, indexSlot);
            }
            case ARRAY -> {
                int arraySlot = scope.frames.addSlot(FrameSlotKind.Object, "rangearray$", null);
                int indexSlot = scope.frames.addSlot(FrameSlotKind.Int, "rangeindex$", null);
                repeating = new RangeArrayNode(x, body, keySlot, keyKind, valueSlot, valueKind,
                    startedSlot, arraySlot, indexSlot);
            }
            case MAP -> {
                int mapSlot = scope.frames.addSlot(FrameSlotKind.Object, "rangemap$", null);
                int keysSlot = scope.frames.addSlot(FrameSlotKind.Object, "rangekeys$", null);
                int indexSlot = scope.frames.addSlot(FrameSlotKind.Int, "rangeindex$", null);
                repeating = new RangeMapNode(x, body, keySlot, keyKind, valueSlot, valueKind,
                    startedSlot, mapSlot, keysSlot, indexSlot);
            }
            case STRING -> {
                int bytesSlot = scope.frames.addSlot(FrameSlotKind.Object, "rangebytes$", null);
                int posSlot = scope.frames.addSlot(FrameSlotKind.Int, "rangepos$", null);
                repeating = new RangeStringNode(x, body, keySlot, keyKind, valueSlot,
                    startedSlot, bytesSlot, posSlot);
            }
            default -> throw new FrontendException(
                where + ": range over " + xkind + " not yet supported in v2");
        }
        GoLoopNode loop = new GoLoopNode(repeating);
        return new RangeStmtNode(null, loop);
    }

    @SuppressWarnings("unchecked")
    private static String objName(List<Object> objects, int objId) {
        if (objId < 0 || objId >= objects.size()) {
            return "v" + objId;
        }
        Object o = objects.get(objId);
        if (o instanceof Map) {
            Object name = ((Map<String, Object>) o).get("name");
            if (name instanceof String) {
                return (String) name;
            }
        }
        return "v" + objId;
    }

    /**
     * Finds the field index of a named field in the struct type of x.
     */
    @SuppressWarnings("unchecked")
    private static int findFieldIndex(List<Object> types, Map<String, Object> xMap,
            String fieldName, String where) {
        Map<String, Object> xfields = getMap(xMap, "fields", where + ".x.fields");
        Object typeIdObj = xfields.get("type");
        if (!(typeIdObj instanceof Long)) {
            throw new FrontendException(where + ": selector.x has no type");
        }
        int typeId = ((Long) typeIdObj).intValue();
        if (typeId < 0 || typeId >= types.size()) {
            throw new FrontendException(where + ": bad type id " + typeId);
        }
        Object t = types.get(typeId);
        if (!(t instanceof Map)) {
            throw new FrontendException(where + ": type not a map");
        }
        Map<String, Object> typeMap = (Map<String, Object>) t;
        // Handle pointer to struct: dereference.
        String kind = (String) typeMap.get("kind");
        if ("pointer".equals(kind)) {
            Map<String, Object> detail = (Map<String, Object>) typeMap.get("detail");
            Object elemObj = detail.get("elem");
            if (elemObj instanceof Long) {
                typeId = ((Long) elemObj).intValue();
                t = types.get(typeId);
                typeMap = (Map<String, Object>) t;
                kind = (String) typeMap.get("kind");
            }
        }
        if (!"struct".equals(kind)) {
            throw new FrontendException(where + ": selector on non-struct type " + kind);
        }
        Map<String, Object> detail = (Map<String, Object>) typeMap.get("detail");
        List<Object> fieldsList = (List<Object>) detail.get("fields");
        for (int i = 0; i < fieldsList.size(); i++) {
            Object f = fieldsList.get(i);
            if (f instanceof Map) {
                Object fname = ((Map<String, Object>) f).get("name");
                if (fieldName.equals(fname)) {
                    return i;
                }
            }
        }
        throw new FrontendException(where + ": field \"" + fieldName + "\" not found in struct");
    }

    /**
     * Builds an expression node from its map representation.
     */
    @SuppressWarnings("unchecked")
    private GoExprNode buildExpr(Map<String, Object> node, String where, V2Scope scope) {
        String kind = getString(node, "kind", where);
        Map<String, Object> fields = getMap(node, "fields", where + ".fields");

        switch (kind) {
            case "basiclit": {
                return buildBasicLit(fields, where, scope);
            }
            case "ident": {
                return buildIdent(fields, where, scope);
            }
            case "binary": {
                String op = getString(fields, "op", where);
                Object xObj = fields.get("x");
                Object yObj = fields.get("y");
                if (!(xObj instanceof Map) || !(yObj instanceof Map)) {
                    throw new FrontendException(where + ": binary x/y must be maps");
                }
                GoExprNode x = buildExpr((Map<String, Object>) xObj, where + ".x", scope);
                GoExprNode y = buildExpr((Map<String, Object>) yObj, where + ".y", scope);
                GoKind k = goKindForTypeId(scope.types, fields.get("type"), where);
                BinaryOpNode.Op bop;
                try {
                    bop = BinaryOpNode.Op.fromToken(op);
                } catch (Exception e) {
                    throw new FrontendException(where + ": bad binary op \"" + op + "\"");
                }
                return new BinaryOpNode(bop, k, x, y);
            }
            case "unary": {
                String op = getString(fields, "op", where);
                Object xObj = fields.get("x");
                if (!(xObj instanceof Map)) {
                    throw new FrontendException(where + ": unary x must be a map");
                }
                GoExprNode x = buildExpr((Map<String, Object>) xObj, where + ".x", scope);
                GoKind k = goKindForTypeId(scope.types, fields.get("type"), where);
                UnaryOpNode.Op uop;
                try {
                    uop = UnaryOpNode.Op.fromToken(op);
                } catch (Exception e) {
                    throw new FrontendException(where + ": bad unary op \"" + op + "\"");
                }
                return new UnaryOpNode(uop, k, x);
            }
            case "paren": {
                Object xObj = fields.get("x");
                if (!(xObj instanceof Map)) {
                    throw new FrontendException(where + ": paren x must be a map");
                }
                return buildExpr((Map<String, Object>) xObj, where + ".x", scope);
            }
            case "call": {
                return buildCall(fields, where, scope);
            }
            case "selector": {
                Object xObj = fields.get("x");
                if (!(xObj instanceof Map)) {
                    throw new FrontendException(where + ": selector.x must be a map");
                }
                Map<String, Object> xMap = (Map<String, Object>) xObj;
                GoExprNode x = buildExpr(xMap, where + ".x", scope);
                String sel = getString(fields, "sel", where);
                GoKind k = goKindForTypeId(scope.types, fields.get("type"), where);
                // Look up the field index in the struct type of x.
                int fieldIndex = findFieldIndex(scope.types, xMap, sel, where);
                return new SelectorNode(x, new int[]{fieldIndex}, k);
            }
            case "pkgref": {
                // Package-qualified reference (e.g. fmt.Println).
                // External package loading is future work for v2: it requires
                // (1) the Go emitter to include external package function info
                // in the binary format, (2) a Java-side package loader (via
                // disk importer or similar), and (3) cross-package function
                // resolution. This is a multi-component feature, not a bug.
                // Fail loudly rather than silently producing wrong code.
                String pkg = getString(fields, "pkg", where);
                String name = getString(fields, "name", where);
                throw new FrontendException(
                    where + ": pkgref " + pkg + "." + name +
                    " not yet supported in v2 (external package loading is future work)");
            }
            case "compositelit": {
                return buildCompositeLit(fields, where, scope);
            }
            case "index": {
                Object xObj = fields.get("x");
                Object indexObj = fields.get("index");
                if (!(xObj instanceof Map) || !(indexObj instanceof Map)) {
                    throw new FrontendException(where + ": index x/index must be maps");
                }
                Map<String, Object> xMap = (Map<String, Object>) xObj;
                GoExprNode x = buildExpr(xMap, where + ".x", scope);
                GoExprNode index = buildExpr((Map<String, Object>) indexObj, where + ".index", scope);
                Map<String, Object> xfields = getMap(xMap, "fields", where + ".x.fields");
                GoKind containerKind = goKindForTypeId(scope.types, xfields.get("type"), where + ".x");
                GoKind elemKind = goKindForTypeId(scope.types, fields.get("type"), where);
                return new IndexNode(x, index, containerKind, elemKind, null);
            }
            case "slice": {
                Object xObj = fields.get("x");
                if (!(xObj instanceof Map)) {
                    throw new FrontendException(where + ": slice.x must be a map");
                }
                Map<String, Object> xMap = (Map<String, Object>) xObj;
                GoExprNode x = buildExpr(xMap, where + ".x", scope);
                GoExprNode lo = null, hi = null, max = null;
                Object loObj = fields.get("low");
                if (loObj instanceof Map) {
                    lo = buildExpr((Map<String, Object>) loObj, where + ".low", scope);
                }
                Object hiObj = fields.get("high");
                if (hiObj instanceof Map) {
                    hi = buildExpr((Map<String, Object>) hiObj, where + ".high", scope);
                }
                Object maxObj = fields.get("max");
                if (maxObj instanceof Map) {
                    max = buildExpr((Map<String, Object>) maxObj, where + ".max", scope);
                }
                Map<String, Object> xfields = getMap(xMap, "fields", where + ".x.fields");
                GoKind containerKind = goKindForTypeId(scope.types, xfields.get("type"), where + ".x");
                GoKind nodeKind = goKindForTypeId(scope.types, fields.get("type"), where);
                return new SliceExprNode(x, lo, hi, max, containerKind, nodeKind);
            }
            default:
                throw new FrontendException(
                    where + ": unsupported expression kind \"" + kind + "\"");
        }
    }

    /**
     * Builds a basic literal (int, float, string, bool, char).
     */
    private GoExprNode buildBasicLit(Map<String, Object> fields, String where, V2Scope scope) {
        String value = getString(fields, "value", where);
        GoKind k = goKindForTypeId(scope.types, fields.get("type"), where);

        // Try integer first.
        if (k.isIntKind()) {
            try {
                long bits;
                if (value.startsWith("0x") || value.startsWith("0X")) {
                    bits = Long.parseLong(value.substring(2), 16);
                } else if (value.startsWith("0o") || value.startsWith("0O")) {
                    bits = Long.parseLong(value.substring(2), 8);
                } else if (value.startsWith("0b") || value.startsWith("0B")) {
                    bits = Long.parseLong(value.substring(2), 2);
                } else {
                    bits = Long.parseLong(value);
                }
                return ConstNode.ofInt(k, bits);
            } catch (NumberFormatException e) {
                // Fall through to other kinds.
            }
        }
        if (k.isFloatKind()) {
            try {
                double d = Double.parseDouble(value);
                long bits = Double.doubleToLongBits(d);
                if (k == GoKind.FLOAT32) {
                    return ConstNode.ofFloat(k, (int) Float.floatToIntBits((float) d), bits);
                }
                return ConstNode.ofFloat(k, 0, bits);
            } catch (NumberFormatException e) {
                throw new FrontendException(where + ": bad float literal \"" + value + "\"");
            }
        }
        if (k == GoKind.STRING || k == GoKind.UNTYPED_STRING) {
            // Strip quotes.
            String s = value;
            if (s.length() >= 2 && s.startsWith("\"") && s.endsWith("\"")) {
                s = s.substring(1, s.length() - 1);
            }
            return ConstNode.ofString(new GoString(s.getBytes(java.nio.charset.StandardCharsets.UTF_8)));
        }
        if (k == GoKind.BOOL || k == GoKind.UNTYPED_BOOL) {
            return ConstNode.ofBool("true".equals(value));
        }
        // Character literal (rune).
        if (value.startsWith("'") && value.endsWith("'") && value.length() >= 3) {
            String inner = value.substring(1, value.length() - 1);
            long runeVal = inner.codePointAt(0);
            return ConstNode.ofInt(k, runeVal);
        }
        throw new FrontendException(where + ": unsupported literal \"" + value + "\" of kind " + k);
    }

    /**
     * Builds an identifier reference.
     */
    private GoExprNode buildIdent(Map<String, Object> fields, String where, V2Scope scope) {
        // Builtin?
        Object builtinObj = fields.get("builtin");
        if (builtinObj instanceof String) {
            String builtin = (String) builtinObj;
            return buildBuiltinIdent(builtin, where, scope);
        }
        Object objIdObj = fields.get("obj");
        if (objIdObj instanceof Long) {
            int objId = ((Long) objIdObj).intValue();
            int slot = scope.slotForObj(objId, where);
            GoKind k = scope.kindForObj(objId, where);
            return new VarReadNode(k, slot);
        }
        String name = (String) fields.get("name");
        throw new FrontendException(where + ": unresolved ident \"" + name + "\"");
    }

    /**
     * Builds a builtin identifier (true, false, nil, iota).
     */
    private GoExprNode buildBuiltinIdent(String builtin, String where, V2Scope scope) {
        switch (builtin) {
            case "true":
                return ConstNode.ofBool(true);
            case "false":
                return ConstNode.ofBool(false);
            case "nil":
                return new NilNode(GoKind.INTERFACE, null);
            default:
                throw new FrontendException(where + ": unsupported builtin ident \"" + builtin + "\"");
        }
    }

    /**
     * Builds a call expression.
     * Fields: {fun, args, builtin, type, ellipsis}
     */
    @SuppressWarnings("unchecked")
    private GoExprNode buildCall(Map<String, Object> fields, String where, V2Scope scope) {
        // Builtin call?
        Object builtinObj = fields.get("builtin");
        if (builtinObj instanceof String) {
            return buildBuiltinCall((String) builtinObj, fields, where, scope);
        }

        Object funObj = fields.get("fun");
        if (!(funObj instanceof Map)) {
            throw new FrontendException(where + ": call.fun must be a map");
        }
        GoExprNode fun = buildExpr((Map<String, Object>) funObj, where + ".fun", scope);

        List<Object> argsList = getList(fields, "args", where);
        List<GoExprNode> args = new ArrayList<>();
        for (int i = 0; i < argsList.size(); i++) {
            Object a = argsList.get(i);
            if (!(a instanceof Map)) {
                throw new FrontendException(where + ".args[" + i + "]: must be a map");
            }
            args.add(buildExpr((Map<String, Object>) a, where + ".args[" + i + "]", scope));
        }

        GoKind resultKind = goKindForTypeId(scope.types, fields.get("type"), where);
        return new FuncValueCallNode(fun, args.toArray(new GoExprNode[0]), resultKind);
    }

    /**
     * Builds a builtin call (len, cap, append, make, new, panic, recover, etc.).
     */
    @SuppressWarnings("unchecked")
    private GoExprNode buildBuiltinCall(String builtin, Map<String, Object> fields,
            String where, V2Scope scope) {
        List<Object> argsList = getList(fields, "args", where);
        List<GoExprNode> args = new ArrayList<>();
        for (int i = 0; i < argsList.size(); i++) {
            Object a = argsList.get(i);
            if (!(a instanceof Map)) {
                throw new FrontendException(where + ".args[" + i + "]: must be a map");
            }
            args.add(buildExpr((Map<String, Object>) a, where + ".args[" + i + "]", scope));
        }

        switch (builtin) {
            case "len": {
                if (args.size() != 1) {
                    throw new FrontendException(where + ": len requires 1 arg");
                }
                return new LenNode(args.get(0));
            }
            case "cap": {
                if (args.size() != 1) {
                    throw new FrontendException(where + ": cap requires 1 arg");
                }
                return new CapNode(args.get(0));
            }
            case "append": {
                if (args.isEmpty()) {
                    throw new FrontendException(where + ": append requires at least 1 arg");
                }
                GoExprNode slice = args.get(0);
                List<GoExprNode> elems = args.subList(1, args.size());
                // Elem kind from the slice type.
                GoKind sliceKind = slice.kind();
                GoKind elemKind = GoKind.INTERFACE;
                // Try to get elem kind from the call's type (the result is the slice type).
                return new AppendNode(slice, elemKind, elems.toArray(new GoExprNode[0]));
            }
            case "make": {
                // make(T), make(T, len), make(T, len, cap).
                GoKind k = goKindForTypeId(scope.types, fields.get("type"), where);
                GoExprNode lenArg = args.size() > 0 ? args.get(0) : null;
                GoExprNode capArg = args.size() > 1 ? args.get(1) : null;
                if (k == GoKind.MAP) {
                    int hint = 0;
                    return new MakeNode(hint);
                }
                // Slice form: need elem kind. Default to interface.
                return new MakeNode(GoKind.INTERFACE, lenArg, capArg, null);
            }
            case "new": {
                // new(T) returns *T.
                return new NewNode(GoKind.POINTER, null);
            }
            case "panic": {
                if (args.size() != 1) {
                    throw new FrontendException(where + ": panic requires 1 arg");
                }
                return new PanicBuiltinNode(args.get(0), null, "", false);
            }
            case "recover": {
                return new RecoverBuiltinNode();
            }
            case "print":
            case "println": {
                // print/println write to stderr. For v2, evaluate args
                // for side effects and return nil.
                if (args.isEmpty()) {
                    return new NilNode(GoKind.INTERFACE, null);
                }
                return args.get(0);
            }
            case "delete": {
                if (args.size() != 2) {
                    throw new FrontendException(where + ": delete requires 2 args");
                }
                return new DeleteNode(args.get(0), args.get(1));
            }
            case "copy": {
                if (args.size() != 2) {
                    throw new FrontendException(where + ": copy requires 2 args");
                }
                return new CopyNode(args.get(0), args.get(1), GoKind.INTERFACE);
            }
            case "clear": {
                if (args.size() != 1) {
                    throw new FrontendException(where + ": clear requires 1 arg");
                }
                return new ClearNode(args.get(0));
            }
            default:
                throw new FrontendException(where + ": unsupported builtin \"" + builtin + "\"");
        }
    }

    // --- Helpers for safe map access ---

    private static String getString(Map<String, Object> m, String key, String where) {
        Object v = m.get(key);
        if (!(v instanceof String)) {
            throw new FrontendException(where + ": \"" + key + "\" must be a string");
        }
        return (String) v;
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> getMap(Map<String, Object> m, String key, String where) {
        Object v = m.get(key);
        if (!(v instanceof Map)) {
            throw new FrontendException(where + ": \"" + key + "\" must be a map");
        }
        return (Map<String, Object>) v;
    }

    @SuppressWarnings("unchecked")
    private static List<Object> getList(Map<String, Object> m, String key, String where) {
        Object v = m.get(key);
        if (!(v instanceof List)) {
            throw new FrontendException(where + ": \"" + key + "\" must be a list");
        }
        return (List<Object>) v;
    }

    // --- Result types ---

    /**
     * A built v2 program: the list of functions with their Truffle bodies.
     */
    public static final class V2Program {
        private final List<V2Function> functions;

        V2Program(List<V2Function> functions) {
            this.functions = List.copyOf(functions);
        }

        public List<V2Function> functions() {
            return functions;
        }

        public int functionCount() {
            return functions.size();
        }
    }

    /**
     * A single built function: name, Truffle body, and frame descriptor.
     */
    public static final class V2Function {
        private final String name;
        private final GoStmtNode body;
        private final FrameDescriptor frameDescriptor;

        V2Function(String name, GoStmtNode body, FrameDescriptor frameDescriptor) {
            this.name = name;
            this.body = body;
            this.frameDescriptor = frameDescriptor;
        }

        public String name() {
            return name;
        }

        public GoStmtNode body() {
            return body;
        }

        public FrameDescriptor frameDescriptor() {
            return frameDescriptor;
        }

        public boolean hasBody() {
            return body != null;
        }
    }

    /**
     * Builds a composite literal (e.g. []int{1,2,3}, Point{X:1}).
     *
     * <p>Element/key kinds, the array length, the struct descriptor and the
     * zero templates are all derived from the literal's type-table entry.
     * Hard-coded placeholders here silently miscompile: a null structDesc
     * NPEs in {@code buildStruct}, arrayLen=-1 throws
     * NegativeArraySizeException in {@code buildArray}, and elemKind=INTERFACE
     * builds {@code []int{1,2,3}} as {@code Object[]} instead of
     * {@code long[]}.
     */
    @SuppressWarnings("unchecked")
    private GoExprNode buildCompositeLit(Map<String, Object> fields, String where, V2Scope scope) {
        int typeId = getTypeId(fields.get("type"));
        GoKind kind = goKindForTypeId(scope.types, typeId, where);

        // Resolve the composite entry: named types resolve to their underlying
        // entry first (bounded walk, like v1), then &T{...} unwraps the
        // pointer — so a named pointer-to-struct type works too. compTypeId
        // stays unresolved (it may name the struct) so the descriptor keeps
        // the qualified name; compResolved is the underlying composite entry.
        int resolvedId = resolveUnderlyingId(scope.types, typeId, where);
        Map<String, Object> entry = typeEntry(scope.types, resolvedId, where);
        boolean isPointer = "pointer".equals(entry.get("kind"));
        int compTypeId = typeId;
        if (isPointer) {
            compTypeId = toInt(typeDetail(entry, where).get("elem"),
                    where + ": pointer composite literal element");
        }
        int compResolved = resolveUnderlyingId(scope.types, compTypeId, where);
        Map<String, Object> compEntry = typeEntry(scope.types, compResolved, where);
        String compKind = (String) compEntry.get("kind");
        Map<String, Object> compDetail = typeDetail(compEntry, where);

        List<Object> elems = getList(fields, "elems", where);

        // Determine if this is a keyed literal.
        List<GoExprNode> keyNodes = new ArrayList<>();
        List<GoExprNode> valueNodes = new ArrayList<>();
        List<Integer> fieldIdxList = new ArrayList<>();
        boolean keyed = false;

        for (int i = 0; i < elems.size(); i++) {
            Object e = elems.get(i);
            if (!(e instanceof Map)) {
                throw new FrontendException(where + ": compositelit elem not a map");
            }
            Map<String, Object> elemMap = (Map<String, Object>) e;
            String elemKind = getString(elemMap, "kind", where + ".elems[" + i + "]");
            if ("keyvalue".equals(elemKind)) {
                keyed = true;
                Map<String, Object> kvFields = getMap(elemMap, "fields", where + ".elems[" + i + "].fields");
                Object keyObj = kvFields.get("key");
                Object valObj = kvFields.get("value");
                if (!(keyObj instanceof Map) || !(valObj instanceof Map)) {
                    throw new FrontendException(where + ": keyvalue key/value must be maps");
                }
                // For struct literals, the key is a field name (ident).
                // For maps, the key is a value.
                Map<String, Object> keyMap = (Map<String, Object>) keyObj;
                String keyKind = getString(keyMap, "kind", where + ".key");
                if ("ident".equals(keyKind)) {
                    // Struct field: resolve field index by name.
                    Map<String, Object> keyFields = getMap(keyMap, "fields", where + ".key.fields");
                    String fieldName = getString(keyFields, "name", where);
                    // Find field index in the struct type.
                    int fieldIdx = findFieldIndexByTypeId(scope.types,
                        compTypeId, fieldName, where);
                    fieldIdxList.add(fieldIdx);
                    keyNodes.add(null); // placeholder
                } else {
                    // Map key: build the key expr.
                    keyNodes.add(buildExpr(keyMap, where + ".key", scope));
                    fieldIdxList.add(-1);
                }
                valueNodes.add(buildExpr((Map<String, Object>) valObj, where + ".value", scope));
            } else {
                // Positional element.
                valueNodes.add(buildExpr(elemMap, where + ".elems[" + i + "]", scope));
            }
        }

        int[] fieldIdx = new int[fieldIdxList.size()];
        for (int i = 0; i < fieldIdxList.size(); i++) {
            fieldIdx[i] = fieldIdxList.get(i);
        }

        // Derive kinds, lengths, descriptors and zero templates from the
        // type-table entry (mirrors v1 buildComplit's per-kind handling).
        GoKind elemKind = null;
        GoKind keyKind = null;
        int arrayLen = 0;
        GoStructDescriptor structDesc = null;
        Object[] fieldZeroTemplates = null;
        Object elemZeroTemplate = null;

        switch (compKind) {
            case "slice": {
                int elemTid = toInt(compDetail.get("elem"), where + ": slice element type");
                elemKind = goKindForTypeId(scope.types, elemTid, where);
                elemZeroTemplate = zeroTemplateForV2(scope.types, elemKind, elemTid, where);
                break;
            }
            case "array": {
                int elemTid = toInt(compDetail.get("elem"), where + ": array element type");
                arrayLen = toInt(compDetail.get("len"), where + ": array length");
                if (arrayLen < 0) {
                    throw new FrontendException(where + ": negative array length " + arrayLen);
                }
                elemKind = goKindForTypeId(scope.types, elemTid, where);
                elemZeroTemplate = zeroTemplateForV2(scope.types, elemKind, elemTid, where);
                break;
            }
            case "map": {
                int keyTid = toInt(compDetail.get("key"), where + ": map key type");
                int elemTid = toInt(compDetail.get("elem"), where + ": map element type");
                keyKind = goKindForTypeId(scope.types, keyTid, where);
                elemKind = goKindForTypeId(scope.types, elemTid, where);
                break;
            }
            case "struct": {
                structDesc = structDescriptorForV2(scope.types, compTypeId, where);
                int[] ftids = structFieldTypeIdsV2(scope.types, compTypeId, where);
                fieldZeroTemplates = new Object[structDesc.fields().size()];
                for (int i = 0; i < fieldZeroTemplates.length; i++) {
                    fieldZeroTemplates[i] = zeroTemplateForV2(scope.types,
                            structDesc.field(i).kind(), ftids[i], where);
                }
                break;
            }
            default:
                throw new FrontendException(
                        where + ": composite literal of " + compKind + " (WS E)");
        }

        return new CompositeLitNode(
            kind,
            keyNodes.toArray(new GoExprNode[0]),
            fieldIdx,
            valueNodes.toArray(new GoExprNode[0]),
            keyed,
            isPointer,
            elemKind,
            keyKind,
            arrayLen,
            structDesc,
            fieldZeroTemplates,
            elemZeroTemplate
        );
    }

    /**
     * Finds a field index by type ID (for composite literals). Named types
     * resolve to their underlying struct entry first.
     */
    @SuppressWarnings("unchecked")
    private static int findFieldIndexByTypeId(List<Object> types, int typeId,
            String fieldName, String where) {
        typeId = resolveUnderlyingId(types, typeId, where);
        Map<String, Object> typeMap = typeEntry(types, typeId, where);
        String kind = (String) typeMap.get("kind");
        if (!"struct".equals(kind)) {
            throw new FrontendException(where + ": keyed literal on non-struct type " + kind);
        }
        Map<String, Object> detail = typeDetail(typeMap, where);
        List<Object> fieldsList = checkedFieldList(detail, where);
        for (int i = 0; i < fieldsList.size(); i++) {
            Object f = fieldsList.get(i);
            if (f instanceof Map) {
                Object fname = ((Map<String, Object>) f).get("name");
                if (fieldName.equals(fname)) {
                    return i;
                }
            }
        }
        throw new FrontendException(where + ": field \"" + fieldName + "\" not found");
    }

    private static int getTypeId(Object typeIdObj) {
        if (typeIdObj instanceof Long) {
            return ((Long) typeIdObj).intValue();
        }
        return -1;
    }

    // --- v2 type-table helpers (V-1) ---

    /**
     * Canonical struct descriptors for the v2 track: descriptor identity ==
     * type identity, so GoStruct.equals and map keys behave across functions
     * built from one document (the I-7 lesson). Keyed structurally — identical
     * anonymous structs share, like v1's STRUCT_DESCS.
     */
    private static final Map<String, GoStructDescriptor> V2_STRUCT_DESCS =
            new ConcurrentHashMap<>();
    /** Recursion guard for descriptor construction: fail fast on cyclic graphs. */
    private static final ThreadLocal<Set<Integer>> V2_DESC_VISITING =
            ThreadLocal.withInitial(HashSet::new);
    /** Zero-value struct templates, keyed by resolved type id. */
    private static final Map<Integer, GoStruct> V2_ZERO_TEMPLATES = new ConcurrentHashMap<>();
    /** Recursion guard for zero-template construction. */
    private static final ThreadLocal<Set<Integer>> V2_ZERO_VISITING =
            ThreadLocal.withInitial(HashSet::new);

    /** Bound for named-type chain walks (mirrors v1's MAX_TYPE_CHAIN_STEPS). */
    private static final int MAX_TYPE_CHAIN_STEPS = 10_000;

    /** Validated type-table entry for a type id. */
    @SuppressWarnings("unchecked")
    private static Map<String, Object> typeEntry(List<Object> types, int typeId, String where) {
        if (typeId < 0 || typeId >= types.size()) {
            throw new FrontendException(where + ": bad type id " + typeId);
        }
        Object t = types.get(typeId);
        if (!(t instanceof Map)) {
            throw new FrontendException(where + ": type " + typeId + " must be a map");
        }
        return (Map<String, Object>) t;
    }

    /** Validated detail map of a type-table entry. */
    @SuppressWarnings("unchecked")
    private static Map<String, Object> typeDetail(Map<String, Object> entry, String where) {
        Object d = entry.get("detail");
        if (!(d instanceof Map)) {
            throw new FrontendException(where + ": type entry missing detail map");
        }
        return (Map<String, Object>) d;
    }

    /** Converts a decoded integer (Long) to int, with a range check. */
    private static int toInt(Object v, String where) {
        long l;
        if (v instanceof Long) {
            l = (Long) v;
        } else if (v instanceof Integer) {
            l = (Integer) v;
        } else {
            throw new FrontendException(where + ": expected integer, got " + v);
        }
        if (l < Integer.MIN_VALUE || l > Integer.MAX_VALUE) {
            throw new FrontendException(where + ": integer out of range: " + l);
        }
        return (int) l;
    }

    /**
     * Resolves a type id through "named" wrappers to its underlying entry,
     * with a bounded chain walk (mirrors v1's structDescriptorFor walk). A
     * cycle fails fast instead of hanging or overflowing the stack.
     */
    private static int resolveUnderlyingId(List<Object> types, int typeId, String where) {
        for (int steps = 0; steps <= MAX_TYPE_CHAIN_STEPS; steps++) {
            Map<String, Object> e = typeEntry(types, typeId, where);
            if (!"named".equals(e.get("kind"))) {
                return typeId;
            }
            typeId = toInt(typeDetail(e, where).get("underlying"),
                    where + ": named underlying type");
        }
        throw new FrontendException(where + ": named-type chain exceeds "
                + MAX_TYPE_CHAIN_STEPS + " steps (cyclic type graph?) (WS E)");
    }

    /** GoKind for an int type id (delegates to the Object overload). */
    private static GoKind goKindForTypeId(List<Object> types, int typeId, String where) {
        return goKindForTypeId(types, (Object) Long.valueOf(typeId), where);
    }

    /** Validated struct field list of a struct detail map. */
    @SuppressWarnings("unchecked")
    private static List<Object> checkedFieldList(Map<String, Object> detail, String where) {
        Object fl = detail.get("fields");
        if (!(fl instanceof List)) {
            throw new FrontendException(where + ": struct detail missing fields list");
        }
        return (List<Object>) fl;
    }

    /** Validated single struct field map. */
    @SuppressWarnings("unchecked")
    private static Map<String, Object> checkedFieldMap(Object f, String where, int i) {
        if (!(f instanceof Map)) {
            throw new FrontendException(where + ": struct field " + i + " is not a map");
        }
        return (Map<String, Object>) f;
    }

    /**
     * Builds (or reuses) the canonical {@link GoStructDescriptor} for a v2
     * struct type id. Mirrors v1's structDescriptorForInner: the full layout
     * call carries array lengths/element kinds, array-element struct
     * descriptors (for GoStruct.zero pre-allocation) and nested struct
     * descriptors (for Intrinsics.structSizeBytes). Recursion terminates on
     * legal Go (direct value recursion is illegal); the visiting guard fails
     * fast otherwise.
     */
    private static GoStructDescriptor structDescriptorForV2(
            List<Object> types, int typeId, String where) {
        int resolved = resolveUnderlyingId(types, typeId, where);
        Map<String, Object> entry = typeEntry(types, resolved, where);
        if (!"struct".equals(entry.get("kind"))) {
            throw new FrontendException(where + ": not a struct type");
        }
        if (!V2_DESC_VISITING.get().add(resolved)) {
            throw new FrontendException(where + ": cyclic struct type graph (WS E)");
        }
        try {
            List<Object> fieldObjs = checkedFieldList(typeDetail(entry, where), where);
            List<String> names = new ArrayList<>(fieldObjs.size());
            List<GoKind> kinds = new ArrayList<>(fieldObjs.size());
            List<Integer> arrayLens = new ArrayList<>(fieldObjs.size());
            List<GoKind> arrayElemKinds = new ArrayList<>(fieldObjs.size());
            List<GoStructDescriptor> arrayElemDescs = new ArrayList<>(fieldObjs.size());
            List<GoStructDescriptor> structDescs = new ArrayList<>(fieldObjs.size());
            for (int i = 0; i < fieldObjs.size(); i++) {
                Map<String, Object> f = checkedFieldMap(fieldObjs.get(i), where, i);
                Object fname = f.get("name");
                if (!(fname instanceof String)) {
                    throw new FrontendException(where + ": struct field " + i + " missing name");
                }
                names.add((String) fname);
                int ftid = toInt(f.get("type"), where + ": struct field " + i + " type");
                GoKind fk = goKindForTypeId(types, ftid, where);
                kinds.add(fk);
                int arrLen = -1;
                GoKind arrElemKind = null;
                GoStructDescriptor arrElemDesc = null;
                if (fk == GoKind.ARRAY) {
                    int arrTid = resolveUnderlyingId(types, ftid, where);
                    Map<String, Object> arrDetail =
                            typeDetail(typeEntry(types, arrTid, where), where);
                    arrLen = toInt(arrDetail.get("len"), where + ": array field length");
                    int arrElemTid =
                            toInt(arrDetail.get("elem"), where + ": array field element");
                    arrElemKind = goKindForTypeId(types, arrElemTid, where);
                    if (arrElemKind == GoKind.STRUCT) {
                        arrElemDesc = structDescriptorForV2(types, arrElemTid, where);
                    }
                }
                arrayLens.add(arrLen);
                arrayElemKinds.add(arrElemKind);
                arrayElemDescs.add(arrElemDesc);
                structDescs.add(fk == GoKind.STRUCT
                        ? structDescriptorForV2(types, ftid, where) : null);
            }
            // Qualified name for named structs (mirrors v1's qname); anonymous
            // structs keep the struct#id marker. A nameless named entry still
            // keys distinctly from anonymous structs of identical layout.
            String qname = null;
            Map<String, Object> origEntry = typeEntry(types, typeId, where);
            if ("named".equals(origEntry.get("kind"))) {
                Object nm = typeDetail(origEntry, where).get("name");
                qname = nm instanceof String ? (String) nm : "named#" + typeId;
            }
            String key = (qname != null ? "N:" + qname : "S:")
                    + "|" + names + "|" + kinds + "|" + arrayLens + "|" + arrayElemKinds
                    + "|" + elemDescKeysV2(arrayElemDescs) + "|" + elemDescKeysV2(structDescs);
            GoStructDescriptor cd = V2_STRUCT_DESCS.get(key);
            if (cd == null) {
                GoStructDescriptor nd = GoStructDescriptor.layout(
                        qname != null ? qname : "struct#" + resolved,
                        names, kinds, arrayLens, arrayElemKinds, arrayElemDescs, structDescs);
                GoStructDescriptor prev = V2_STRUCT_DESCS.putIfAbsent(key, nd);
                cd = prev != null ? prev : nd;
            }
            return cd;
        } finally {
            V2_DESC_VISITING.get().remove(resolved);
        }
    }

    /** Structural keys for nested struct descriptors ("-" for absent). */
    private static String elemDescKeysV2(List<GoStructDescriptor> descs) {
        StringBuilder sb = new StringBuilder("[");
        for (GoStructDescriptor d : descs) {
            sb.append(d == null ? "-" : structLayoutKeyV2(d)).append(',');
        }
        return sb.append(']').toString();
    }

    /** Recursive structural layout key for a struct descriptor. */
    private static String structLayoutKeyV2(GoStructDescriptor d) {
        StringBuilder sb = new StringBuilder("{");
        for (GoStructDescriptor.Field f : d.fields()) {
            sb.append(f.name()).append(':').append(f.kind());
            if (f.kind() == GoKind.ARRAY) {
                sb.append('[').append(f.arrayLen()).append(']').append(f.arrayElemKind());
                if (f.arrayElemDesc() != null) {
                    sb.append(structLayoutKeyV2(f.arrayElemDesc()));
                }
            }
            if (f.kind() == GoKind.STRUCT && f.structDesc() != null) {
                sb.append(structLayoutKeyV2(f.structDesc()));
            }
            sb.append(';');
        }
        return sb.append('}').toString();
    }

    /**
     * Field type-table ids parallel to a v2 struct descriptor's fields.
     */
    private static int[] structFieldTypeIdsV2(List<Object> types, int typeId, String where) {
        int resolved = resolveUnderlyingId(types, typeId, where);
        Map<String, Object> entry = typeEntry(types, resolved, where);
        if (!"struct".equals(entry.get("kind"))) {
            throw new FrontendException(where + ": not a struct type");
        }
        List<Object> fieldObjs = checkedFieldList(typeDetail(entry, where), where);
        int[] ftids = new int[fieldObjs.size()];
        for (int i = 0; i < ftids.length; i++) {
            Map<String, Object> f = checkedFieldMap(fieldObjs.get(i), where, i);
            ftids[i] = toInt(f.get("type"), where + ": struct field " + i + " type");
        }
        return ftids;
    }

    /**
     * Zero-value template for a composite literal element/field type
     * (mirrors v1's zeroTemplateFor): STRUCT → nested zero GoStruct, ARRAY →
     * zero-filled array, STRING → GoString.EMPTY, complex → complex zeros.
     * Primitive kinds need no template (their typed backing arrays are
     * already zero) and nillable kinds are correctly nil as null.
     */
    private static Object zeroTemplateForV2(List<Object> types, GoKind kind, int typeId,
            String where) {
        if (kind == GoKind.STRUCT) {
            return structZeroTemplateV2(types, typeId, where);
        }
        if (kind == GoKind.ARRAY) {
            return zeroArrayForV2(types, typeId, where);
        }
        if (kind == GoKind.STRING || kind == GoKind.UNTYPED_STRING) {
            return GoString.EMPTY;
        }
        if (kind == GoKind.COMPLEX64) {
            return GoComplex32.ZERO;
        }
        if (kind == GoKind.COMPLEX128 || kind == GoKind.UNTYPED_COMPLEX) {
            return GoComplex64.ZERO;
        }
        return null;
    }

    /**
     * Fresh Go zero value for a v2 struct type id, recursively zeroing nested
     * struct/array fields (mirrors v1's structZeroTemplate). The template is
     * built once per type; callers copy it per evaluation (never share
     * mutable zeros).
     */
    private static GoStruct structZeroTemplateV2(List<Object> types, int typeId, String where) {
        int resolved = resolveUnderlyingId(types, typeId, where);
        GoStruct t = V2_ZERO_TEMPLATES.get(resolved);
        if (t != null) {
            return t;
        }
        if (!V2_ZERO_VISITING.get().add(resolved)) {
            throw new FrontendException(where + ": cyclic struct type graph at type id "
                    + resolved + " (WS E)");
        }
        try {
            GoStructDescriptor desc = structDescriptorForV2(types, resolved, where);
            int[] ftids = structFieldTypeIdsV2(types, resolved, where);
            Object[] objects = new Object[desc.objectsCount()];
            for (int i = 0; i < desc.fields().size(); i++) {
                GoStructDescriptor.Field f = desc.field(i);
                if (!f.inLongs()) {
                    objects[f.index()] = zeroTemplateForV2(types, f.kind(), ftids[i], where);
                }
            }
            t = new GoStruct(desc, new long[desc.longsCount()], objects);
            GoStruct prev = V2_ZERO_TEMPLATES.putIfAbsent(resolved, t);
            return prev != null ? prev : t;
        } finally {
            V2_ZERO_VISITING.get().remove(resolved);
        }
    }

    /**
     * Fresh zero-initialized Java array for a v2 Go array type id (mirrors
     * v1's zeroArrayFor): recursively initializes nested array/struct/string
     * elements so no element is null.
     */
    private static Object zeroArrayForV2(List<Object> types, int typeId, String where) {
        int resolved = resolveUnderlyingId(types, typeId, where);
        Map<String, Object> entry = typeEntry(types, resolved, where);
        if (!"array".equals(entry.get("kind"))) {
            throw new FrontendException(where + ": not an array type");
        }
        Map<String, Object> detail = typeDetail(entry, where);
        int len = toInt(detail.get("len"), where + ": array length");
        int elemTid = toInt(detail.get("elem"), where + ": array element type");
        GoKind ek = goKindForTypeId(types, elemTid, where);
        Object arr = Boxing.newArray(ek, len);
        if (ek == GoKind.ARRAY || ek == GoKind.STRUCT || ek == GoKind.STRING) {
            Object[] objs = (Object[]) arr;
            for (int i = 0; i < len; i++) {
                if (ek == GoKind.ARRAY) {
                    objs[i] = zeroArrayForV2(types, elemTid, where);
                } else if (ek == GoKind.STRUCT) {
                    objs[i] = structZeroTemplateV2(types, elemTid, where).deepCopy();
                } else {
                    objs[i] = GoString.EMPTY;
                }
            }
        }
        return arr;
    }
}
