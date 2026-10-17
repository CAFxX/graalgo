package graalgo.frontend;

import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.source.Source;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Map;

/**
 * RV-P1-03: negative tests pinning the eight "not implemented/supported in v1"
 * scope exclusions in {@code NodeFactory.java}.
 *
 * <p>Each exclusion gets two tests:
 * <ol>
 *   <li>an <em>abort-fires</em> test: feeding a wire document (or, for the two
 *       dead-code exclusions, a direct method invocation) that hits the
 *       exclusion's exact scope must throw {@code FrontendException} with the
 *       exclusion's named message. Removing the abort without implementing the
 *       feature makes this test fail;</li>
 *   <li>a <em>scope-guard</em> test: a nearby in-scope document must build
 *       without throwing. Widening the abort beyond its scope makes this test
 *       fail.</li>
 * </ol>
 *
 * <p>Wire resources: {@code src/test/resources/frontend/scope-excl-*.json}.
 * The {@code *-abort} / {@code e8} documents are gofrontend exports of small
 * Go probes with one minimal, asserted structural surgery each (duplicate a
 * results entry, drop a "const" key, inject/rename a builtin marker); the
 * {@code *-control} documents are the unmodified gofrontend exports. The
 * surgeries are asserted in the generator
 * ({@code make_resources.py}, kept with the lane scratch, not committed) so a
 * wire-format change fails loudly at generation time instead of silently
 * producing a vacuous resource.
 *
 * <p>Retirement: each test names its work-registry item
 * ({@code rv-p1-03-excl-eN-*}); when the exclusion's feature lands and the
 * abort is removed, the abort-fires test is replaced by a positive test and
 * the item is marked done.
 */
public final class ScopeExclusionAbortsTest {

    private static String loadResource(String name) throws Exception {
        return Files.readString(
                Path.of("src/test/resources/frontend", name),
                StandardCharsets.UTF_8);
    }

    private static Source newSource(String fileName) {
        return Source.newBuilder("go", "package main", fileName).build();
    }

    /** Builds the wire document; returns null when it builds cleanly. */
    private static FrontendException buildOutcome(String resource) throws Exception {
        String json = loadResource(resource);
        try {
            new NodeFactory(newSource("scope-excl.go")).buildProgram(json);
        } catch (FrontendException e) {
            return e;
        }
        return null;
    }

    private static void expectAbort(String testName, String resource, String... markers)
            throws Exception {
        FrontendException e = buildOutcome(resource);
        if (e == null) {
            throw new AssertionError(testName + ": " + resource
                    + " must abort with FrontendException, but built cleanly;"
                    + " the scope exclusion may have been removed without a"
                    + " replacement test");
        }
        for (String marker : markers) {
            if (!e.getMessage().contains(marker)) {
                throw new AssertionError(testName + ": expected <" + marker
                        + "> in FrontendException, got <" + e.getMessage() + ">");
            }
        }
    }

    private static void expectBuilds(String testName, String resource) throws Exception {
        FrontendException e = buildOutcome(resource);
        if (e != null) {
            throw new AssertionError(testName + ": " + resource
                    + " is in scope and must build cleanly, but aborted with <"
                    + e.getMessage() + ">");
        }
    }

    // ---- E1: WS D — function with multiple results (NodeFactory.java:747) ----

    public void testMultiResultFuncdeclAbortsWSD() throws Exception {
        expectAbort("E1", "scope-excl-e1-multiresult-funcdecl.json",
                "function with multiple results", "(WS D)");
    }

    public void testSingleResultFuncdeclBuilds() throws Exception {
        // Scope guard: the abort must fire only for >1 results.
        expectBuilds("E1-guard", "scope-excl-control-single-result.json");
    }

    // ---- E2: WS D — non-constant basiclit (NodeFactory.java:2907) ----

    public void testNonConstantBasiclitAbortsWSD() throws Exception {
        expectAbort("E2", "scope-excl-e2-nonconst-basiclit.json",
                "non-constant basiclit", "(WS D: typed constant evaluation)");
    }

    public void testConstantBasiclitBuilds() throws Exception {
        // Scope guard: an ordinary folded constant literal is in scope.
        expectBuilds("E2-guard", "scope-excl-control-single-result.json");
    }

    // ---- E3: WS G — ident fun carrying a builtin marker without call-level
    // dispatch (NodeFactory.java:3372) ----

    public void testIdentBuiltinMarkerAbortsWSG() throws Exception {
        expectAbort("E3", "scope-excl-e3-ident-builtin.json",
                "call to builtin", "is not implemented in v1 (WS G intrinsic surface)");
    }

    public void testFuncVarCallBuilds() throws Exception {
        // Scope guard: a call through a func-typed variable is in scope.
        expectBuilds("E3-guard", "scope-excl-control-funvar-call.json");
    }

    // ---- E4: WS G — unlisted builtin name (NodeFactory.java:3704) ----

    public void testUnlistedBuiltinAbortsWSG() throws Exception {
        expectAbort("E4", "scope-excl-e4-unlisted-builtin.json",
                "call to builtin \"close\"",
                "is not implemented in NodeFactory v1 (WS G intrinsic surface)");
    }

    public void testListedBuiltinCallBuilds() throws Exception {
        // Scope guard: a listed builtin (len) dispatches without the abort.
        expectBuilds("E4-guard", "scope-excl-control-len.json");
    }

    // ---- E5: return with multiple results (NodeFactory.java:5037) ----

    public void testMultiResultReturnAborts() throws Exception {
        expectAbort("E5", "scope-excl-e5-multiresult-return.json",
                "return with multiple results is not supported in v1");
    }

    public void testSingleResultReturnBuilds() throws Exception {
        // Scope guard: an ordinary single-result return is in scope.
        expectBuilds("E5-guard", "scope-excl-control-single-result.json");
    }

    // ---- E8: WS F — range `=` into a captured variable, live path
    // (NodeFactory.java:5314, rangeAssignSlot) ----

    public void testRangeAssignCapturedAbortsWSF() throws Exception {
        expectAbort("E8", "scope-excl-e8-range-captured.json",
                "range assignment to captured variable", "(WS F)");
    }

    public void testRangeAssignLocalBuilds() throws Exception {
        // Scope guard: range `=` into a local, non-captured variable is in
        // scope and takes the plain-slot path.
        expectBuilds("E8-guard", "scope-excl-control-range-local.json");
    }

    // ---- E6/E7: dead-code exclusions in slotForOid (NodeFactory.java:5284,
    // 5291). slotForOid has no callers; the aborts are pinned by direct
    // invocation. If slotForOid is ever deleted (the documented retirement),
    // these tests fail with an explanatory message pointing at the registry
    // items instead of a raw reflection error. ----

    private static Method slotForOidMethod() {
        try {
            Method m = NodeFactory.class.getDeclaredMethod("slotForOid",
                    NodeFactory.BuildCtx.class, int.class, String.class);
            m.setAccessible(true);
            return m;
        } catch (NoSuchMethodException e) {
            throw new AssertionError("slotForOid was removed: delete the E6/E7 tests in"
                    + " ScopeExclusionAbortsTest and mark work-registry items"
                    + " rv-p1-03-excl-e6-m7-range-global-dead and"
                    + " rv-p1-03-excl-e7-wsf-range-captured-dead done", e);
        }
    }

    @SuppressWarnings("unchecked")
    private static Map<Integer, Integer> globalSlotsOf(NodeFactory factory) throws Exception {
        Field f = NodeFactory.class.getDeclaredField("globalSlots");
        f.setAccessible(true);
        return (Map<Integer, Integer>) f.get(factory);
    }

    private static NodeFactory.BuildCtx newCtx(Source source,
            NodeFactory.BuildCtx parent) {
        return new NodeFactory.BuildCtx(source, FrameDescriptor.newBuilder(),
                new TypeTable(new Json.Array(new ArrayList<>())),
                new Json.Array(new ArrayList<>()), parent);
    }

    private static FrontendException invokeSlotForOid(NodeFactory factory,
            NodeFactory.BuildCtx ctx, int oid) {
        try {
            slotForOidMethod().invoke(factory, ctx, oid, "test-range");
        } catch (java.lang.reflect.InvocationTargetException e) {
            // The abort surfaces wrapped; anything else is a real failure.
            if (e.getCause() instanceof FrontendException fe) {
                return fe;
            }
            throw new AssertionError("slotForOid invocation failed unexpectedly: "
                    + e.getCause(), e.getCause());
        } catch (Exception e) {
            throw new AssertionError("slotForOid invocation failed unexpectedly: "
                    + e, e);
        }
        return null;
    }

    public void testRangeAssignGlobalAbortsM7() throws Exception {
        // E6: NodeFactory.java:5284 — range `=` into a package-level variable.
        NodeFactory factory = new NodeFactory(newSource("slot.go"));
        globalSlotsOf(factory).put(7, 3);
        NodeFactory.BuildCtx ctx = newCtx(newSource("slot.go"), null);
        FrontendException e = invokeSlotForOid(factory, ctx, 7);
        if (e == null) {
            throw new AssertionError("E6: slotForOid on a package-level variable must"
                    + " abort with the M7 exclusion; the abort may have been removed"
                    + " without a replacement test");
        }
        if (!e.getMessage().contains("range assignment to a package-level variable")
                || !e.getMessage().contains("(M7)")) {
            throw new AssertionError("E6: expected the M7 exclusion message, got <"
                    + e.getMessage() + ">");
        }
    }

    public void testRangeAssignCapturedAbortsWSFDead() throws Exception {
        // E7: NodeFactory.java:5291 — range `=` into a captured variable
        // (depth > 0).
        NodeFactory factory = new NodeFactory(newSource("slot.go"));
        NodeFactory.BuildCtx parent = newCtx(newSource("slot.go"), null);
        parent.objSlots.put(9, 5);
        NodeFactory.BuildCtx child = newCtx(newSource("slot.go"), parent);
        FrontendException e = invokeSlotForOid(factory, child, 9);
        if (e == null) {
            throw new AssertionError("E7: slotForOid on a captured variable must"
                    + " abort with the WS F exclusion; the abort may have been removed"
                    + " without a replacement test");
        }
        if (!e.getMessage().contains("range assignment to captured variable")
                || !e.getMessage().contains("(WS F)")) {
            throw new AssertionError("E7: expected the WS F exclusion message, got <"
                    + e.getMessage() + ">");
        }
    }

    public void testSlotForOidLocalReturnsSlot() throws Exception {
        // Scope guard: a local, non-captured variable takes the plain-slot
        // path — neither abort may fire.
        NodeFactory factory = new NodeFactory(newSource("slot.go"));
        NodeFactory.BuildCtx ctx = newCtx(newSource("slot.go"), null);
        ctx.objSlots.put(10, 2);
        FrontendException e = invokeSlotForOid(factory, ctx, 10);
        if (e != null) {
            throw new AssertionError("E6/E7-guard: slotForOid on a local variable must"
                    + " not abort, but got <" + e.getMessage() + ">");
        }
    }
}
