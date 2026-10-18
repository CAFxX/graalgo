package graalgo.frontend.v2;

import graalgo.frontend.FrontendException;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * Contract tests for v2 selector decoding (L13).
 *
 * <p>The Go emitter attaches the selected object's id to every "selector"
 * node. The factory must use it: a selector whose object is a func is a
 * method selection and must fail loudly (v2 has no receiver emission or
 * method dispatch), while a selector whose object is a var is a field
 * selection resolved by field index as before.
 *
 * <p>{@code buildFunction} is private, so these tests reach it via
 * reflection with hand-built document fragments shaped exactly like the
 * emitter's output (pinned on the Go side by
 * {@code TestEmitSelectorCarriesObjectID}).
 */
public final class NodeFactoryV2SelectorTest {

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /** Types table: 0 = int, 1 = struct{X int}, 2 = opaque func signature. */
    private static List<Object> types() {
        List<Object> types = new ArrayList<>();
        types.add(Map.of("kind", "basic", "detail", Map.of("name", "int", "class", "int")));
        types.add(Map.of("kind", "struct", "detail",
            Map.of("fields", List.of(Map.of("name", "X", "type", 0L)))));
        types.add(Map.of("kind", "signature", "detail", Map.of()));
        return types;
    }

    /**
     * Objects table: 0 = method M (func), 1 = param p (var of struct),
     * 2 = field X (var).
     */
    private static List<Object> objects() {
        List<Object> objects = new ArrayList<>();
        objects.add(Map.of("kind", "func", "name", "M", "type", 2L));
        objects.add(Map.of("kind", "var", "name", "p", "type", 1L));
        objects.add(Map.of("kind", "var", "name", "X", "type", 0L));
        return objects;
    }

    private static Map<String, Object> node(String kind, Map<String, Object> fields) {
        Map<String, Object> n = new HashMap<>();
        n.put("kind", kind);
        n.put("fields", fields);
        return n;
    }

    private static Map<String, Object> identP() {
        Map<String, Object> f = new HashMap<>();
        f.put("obj", 1L);
        f.put("type", 1L);
        return node("ident", f);
    }

    private static Map<String, Object> selectorFor(long objId, String sel) {
        Map<String, Object> f = new HashMap<>();
        f.put("x", identP());
        f.put("sel", sel);
        f.put("obj", objId);
        f.put("type", 0L);
        return node("selector", f);
    }

    /** Builds a funcdecl whose body is a single exprstmt of the given expr. */
    private static Map<String, Object> funcdeclWithExpr(Map<String, Object> expr) {
        Map<String, Object> stmt = node("exprstmt", Map.of("x", expr));
        Map<String, Object> body = node("block", Map.of("stmts", List.of(stmt)));
        Map<String, Object> param = node("param",
            Map.of("name", "p", "obj", 1L, "type", 1L));
        Map<String, Object> f = new HashMap<>();
        f.put("name", "F");
        f.put("sig", 2L);
        f.put("params", List.of(param));
        f.put("body", body);
        return node("funcdecl", f);
    }

    private static void buildFunction(Map<String, Object> fn) throws Exception {
        // The factory's Source is unused by the statement/expression
        // builders exercised here; null is safe for these tests.
        NodeFactoryV2 factory = new NodeFactoryV2(null);
        Method m = NodeFactoryV2.class.getDeclaredMethod(
            "buildFunction", Map.class, int.class, List.class, List.class);
        m.setAccessible(true);
        try {
            m.invoke(factory, fn, 0, types(), objects());
        } catch (InvocationTargetException e) {
            Throwable cause = e.getCause();
            if (cause instanceof Exception) {
                throw (Exception) cause;
            }
            throw new AssertionError("unexpected throwable", cause);
        }
    }

    public void testMethodSelectorFailsLoudly() throws Exception {
        // p.M() where M is the method (object 0): v2 cannot dispatch it,
        // so the factory must reject it explicitly — not name-match "M"
        // against the struct's fields.
        try {
            buildFunction(funcdeclWithExpr(selectorFor(0L, "M")));
        } catch (FrontendException e) {
            check(e.getMessage().contains("method selection"),
                "expected method-selection error, got: " + e.getMessage());
            return;
        }
        throw new AssertionError("expected FrontendException for method selector");
    }

    public void testFieldSelectorStillResolves() throws Exception {
        // p.X where X is the field (object 2): resolves by field index.
        buildFunction(funcdeclWithExpr(selectorFor(2L, "X")));
    }

    public void testSelectorWithoutObjFallsBackToNameLookup() throws Exception {
        // Old wire (no "obj" key): field resolution by name still works.
        Map<String, Object> f = new HashMap<>();
        f.put("x", identP());
        f.put("sel", "X");
        f.put("type", 0L);
        buildFunction(funcdeclWithExpr(node("selector", f)));
    }
}
