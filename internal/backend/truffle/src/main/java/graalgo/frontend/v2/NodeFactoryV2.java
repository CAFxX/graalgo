package graalgo.frontend.v2;

import com.oracle.truffle.api.source.Source;
import graalgo.frontend.BlockNode;
import graalgo.frontend.ConstNode;
import graalgo.frontend.ExprStmtNode;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.GoStmtNode;
import graalgo.values.GoKind;
import java.io.IOException;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

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
 * <p>MVP status: This factory implements the binary interface end-to-end
 * and constructs Truffle nodes for the core expression/statement kinds.
 * Complex kinds (generics, channels, type switches) are future work —
 * they fail loudly with {@link FrontendException}, never silently.
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
     * @throws FrontendException if the document is invalid or uses
     *         unsupported node kinds
     */
    public V2Program buildProgram(byte[] data) throws IOException {
        Map<String, Object> doc = BinaryDecoder.decodeDocument(data);

        String frontend = getString(doc, "frontend", "document");
        if (!"v2".equals(frontend)) {
            throw new FrontendException(
                "unsupported frontend version \"" + frontend +
                "\": this factory implements frontend/v2 only");
        }

        List<Object> funcs = getList(doc, "funcs", "document");
        List<Object> types = getList(doc, "types", "document");
        List<V2Function> functions = new ArrayList<>();
        for (int i = 0; i < funcs.size(); i++) {
            Object f = funcs.get(i);
            if (!(f instanceof Map)) {
                throw new FrontendException("funcs[" + i + "]: must be a map");
            }
            @SuppressWarnings("unchecked")
            Map<String, Object> fnMap = (Map<String, Object>) f;
            functions.add(buildFunction(fnMap, i, types));
        }

        return new V2Program(functions);
    }

    /**
     * Builds a single function from its funcdecl node map.
     */
    @SuppressWarnings("unchecked")
    private V2Function buildFunction(Map<String, Object> fnMap, int index, List<Object> types) {
        String kind = getString(fnMap, "kind", "funcs[" + index + "]");
        if (!"funcdecl".equals(kind)) {
            throw new FrontendException(
                "funcs[" + index + "]: kind must be funcdecl, got " + kind);
        }
        Map<String, Object> fields = getMap(fnMap, "fields", "funcdecl");
        String name = getString(fields, "name", "funcdecl.fields");

        // Build the body block (may be null for declarations without bodies).
        Object bodyObj = fields.get("body");
        GoStmtNode body = null;
        if (bodyObj instanceof Map) {
            body = buildStmt((Map<String, Object>) bodyObj, "funcdecl.fields.body", types);
        }

        return new V2Function(name, body);
    }

    /**
     * Builds a statement node from its map representation.
     */
    @SuppressWarnings("unchecked")
    private GoStmtNode buildStmt(Map<String, Object> node, String where, List<Object> types) {
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
                    built.add(buildStmt((Map<String, Object>) s, where + ".stmts[" + i + "]", types));
                }
                return new BlockNode(built.toArray(new GoStmtNode[0]));
            }
            case "exprstmt": {
                Object x = fields.get("x");
                if (!(x instanceof Map)) {
                    throw new FrontendException(where + ": exprstmt.x must be a map");
                }
                GoExprNode expr = buildExpr((Map<String, Object>) x, where + ".x", types);
                return new ExprStmtNode(expr);
            }
            case "assign": {
                // MVP: assignments require frame slots and two-phase lowering.
                // For now, fail loudly — full implementation is next.
                throw new FrontendException(
                    where + ": assign not yet implemented in v2 factory (MVP)");
            }
            case "return": {
                throw new FrontendException(
                    where + ": return not yet implemented in v2 factory (MVP)");
            }
            default:
                throw new FrontendException(
                    where + ": unsupported statement kind \"" + kind + "\"");
        }
    }

    /**
     * Builds an expression node from its map representation.
     */
    @SuppressWarnings("unchecked")
    private GoExprNode buildExpr(Map<String, Object> node, String where, List<Object> types) {
        String kind = getString(node, "kind", where);
        Map<String, Object> fields = getMap(node, "fields", where + ".fields");

        switch (kind) {
            case "basiclit": {
                String value = getString(fields, "value", where);
                // The emitter (internal/frontend/v2/emit/nodes.go: emitBasicLit)
                // records the go/types-computed type of every literal, so the
                // GoKind comes from the document's type table — the same
                // general mechanism v1 uses (NodeFactory.buildBasicLit +
                // kindOf → GoKind.fromBasic), never a hard-coded guess.
                GoKind goKind = kindOf(types, getInt(fields, "type", where), where);
                // Prefer the emitter's go/types-computed exact constant when
                // present (canonical decimal text, like v1's "const" field);
                // fall back to the source literal text (MVP).
                Object constObj = fields.get("const");
                String digits = (constObj instanceof String s) ? s : value;
                try {
                    long bits = Long.parseLong(digits);
                    return switch (goKind) {
                        case INT8, INT16, INT32, INT64, UINT8, UINT16, UINT32, UINT64 ->
                            ConstNode.ofInt(goKind, bits);
                        // An untyped constant stays transparent: it assumes the
                        // context type at the parent, exactly like v1's
                        // UNTYPED_INT and go/types itself (GoKind javadoc).
                        case UNTYPED_INT -> ConstNode.ofUntypedInt(bits, digits);
                        default -> throw new FrontendException(
                            where + ": basiclit of GoKind " + goKind
                                + " not yet supported in v2 factory (MVP)");
                    };
                } catch (NumberFormatException e) {
                    throw new FrontendException(
                        where + ": non-integer literal \"" + digits + "\" not yet supported (MVP)");
                }
            }
            case "ident": {
                throw new FrontendException(
                    where + ": ident not yet implemented in v2 factory (MVP)");
            }
            case "call": {
                throw new FrontendException(
                    where + ": call not yet implemented in v2 factory (MVP)");
            }
            case "binary": {
                throw new FrontendException(
                    where + ": binary not yet implemented in v2 factory (MVP)");
            }
            default:
                throw new FrontendException(
                    where + ": unsupported expression kind \"" + kind + "\"");
        }
    }

    // --- Helpers for safe map access ---

    private static int getInt(Map<String, Object> m, String key, String where) {
        Object v = m.get(key);
        if (!(v instanceof Long)) {
            throw new FrontendException(where + ": \"" + key + "\" must be an int");
        }
        long l = (Long) v;
        if (l < 0 || l > Integer.MAX_VALUE) {
            throw new FrontendException(where + ": \"" + key + "\" out of int range");
        }
        return (int) l;
    }

    /**
     * Resolves a type-table id to a {@link GoKind}. Mirrors v1's
     * {@code NodeFactory.kindOf}: named types are resolved to their
     * underlying type (bounded walk — a cycle fails fast, never hangs);
     * basic entries go through {@link GoKind#fromBasic}, everything else
     * through {@link GoKind#fromCompositeKind}. A general mechanism — no
     * type-name special cases at the call sites.
     */
    @SuppressWarnings("unchecked")
    private static GoKind kindOf(List<Object> types, int typeId, String where) {
        if (typeId < 0 || typeId >= types.size()) {
            throw new FrontendException(where + ": type id " + typeId + " out of range");
        }
        String path = where + ".types[" + typeId + "]";
        // M10 (P1-3) analog: bounded chain walk; a cycle fails fast, never hangs.
        for (int steps = 0; ; steps++) {
            if (steps > 64) {
                throw new FrontendException(path + ": type chain exceeds 64 steps (cyclic type graph?)");
            }
            Object entryObj = types.get(typeId);
            if (!(entryObj instanceof Map)) {
                throw new FrontendException(path + ": type table entry must be a map");
            }
            Map<String, Object> entry = (Map<String, Object>) entryObj;
            String ek = getString(entry, "kind", path);
            if ("named".equals(ek)) {
                Map<String, Object> detail = getMap(entry, "detail", path);
                typeId = getInt(detail, "underlying", path);
                if (typeId < 0 || typeId >= types.size()) {
                    throw new FrontendException(path + ": underlying type id " + typeId + " out of range");
                }
            } else {
                break;
            }
        }
        Map<String, Object> entry = (Map<String, Object>) types.get(typeId);
        String k = getString(entry, "kind", path);
        try {
            if ("basic".equals(k)) {
                Map<String, Object> detail = getMap(entry, "detail", path);
                return GoKind.fromBasic(getString(detail, "name", path), getString(detail, "class", path));
            }
            return GoKind.fromCompositeKind(k);
        } catch (IllegalArgumentException e) {
            throw new FrontendException(path + ": " + e.getMessage());
        }
    }

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
     * A single built function: name and optional Truffle body.
     */
    public static final class V2Function {
        private final String name;
        private final GoStmtNode body;

        V2Function(String name, GoStmtNode body) {
            this.name = name;
            this.body = body;
        }

        public String name() {
            return name;
        }

        public GoStmtNode body() {
            return body;
        }

        public boolean hasBody() {
            return body != null;
        }
    }
}
