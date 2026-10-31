package graalgo.frontend;

import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.source.Source;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoTypeDescriptor;
import java.lang.reflect.Method;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * P1-B downgrade (2026-10-31): pins the wording of the three
 * template-methods-skipped fail-fasts (boxing, type assertion, type
 * switch) and the reachability condition behind them. The old messages
 * named a shipped phase ("(P3-M3 scope)"); the new ones name the
 * condition (template methods skipped because the type parameters are
 * unbound). The sentinel can only arise on template entries, so these
 * checks are defense-in-depth — but if they ever fire, the message must
 * say what is actually wrong.
 *
 * <p>Each test is red-green: it fails if the sentinel branch is removed
 * (no throw where one is expected) or if the old phase label returns.
 */
public final class TemplateMethodsSkippedTest {

    private static final String NEW_LABEL =
            "are incomplete (template methods skipped: unbound type parameters)";
    private static final String OLD_LABEL = "(P3-M3 scope)";

    private static final String DYN_NAME = "*sync/atomic.Pointer[T]";

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertContains(String haystack, String needle, String msg) {
        if (haystack == null || !haystack.contains(needle)) {
            throw new AssertionError(msg + " — message was: " + haystack);
        }
    }

    private static void assertNotContains(String haystack, String needle, String msg) {
        if (haystack != null && haystack.contains(needle)) {
            throw new AssertionError(msg + " — message was: " + haystack);
        }
    }

    private static GoTypeDescriptor dynDescriptor() {
        return new GoTypeDescriptor(DYN_NAME, GoKind.STRUCT, false);
    }

    private static GoTypeDescriptor ifaceDescriptor() {
        return new GoTypeDescriptor("main.Loader", GoKind.INTERFACE, false);
    }

    private static GoInterface dynValue() {
        return new GoInterface(dynDescriptor(), new Object(), null);
    }

    public void testAssertSentinelMessage() {
        Map<String, Set<String>> sets = Map.of(
                DYN_NAME, Set.of(NodeFactory.TEMPLATE_METHODS_SKIPPED));
        try {
            TypeAssertNode.holds(dynValue(), ifaceDescriptor(),
                    List.of("Load"), sets);
            throw new AssertionError("expected FrontendException for sentinel dynamic type");
        } catch (FrontendException e) {
            assertContains(e.getMessage(), "cannot assert generic instantiation",
                    "assert message must name the failed assertion");
            assertContains(e.getMessage(), NEW_LABEL,
                    "assert message must carry the new label");
            assertNotContains(e.getMessage(), OLD_LABEL,
                    "assert message must not carry the stale phase label");
        }
    }

    public void testAssertSentinelAbsentControls() {
        // Without the sentinel, holds() is a plain implkey-cover check:
        // covering set holds, empty set does not. This proves the throw
        // above comes from the sentinel branch, not from holds() itself.
        Map<String, Set<String>> covering = Map.of(DYN_NAME, Set.of("Load"));
        assertTrue(TypeAssertNode.holds(dynValue(), ifaceDescriptor(),
                List.of("Load"), covering), "covering implkeys must hold");
        Map<String, Set<String>> empty = Map.of(DYN_NAME, Set.of());
        assertTrue(!TypeAssertNode.holds(dynValue(), ifaceDescriptor(),
                List.of("Load"), empty), "empty implkeys must not hold");
    }

    public void testSwitchSentinelMessage() throws Exception {
        Map<String, Set<String>> sets = Map.of(
                DYN_NAME, Set.of(NodeFactory.TEMPLATE_METHODS_SKIPPED));
        TypeSwitchNode node = new TypeSwitchNode(
                null, null, List.of(), ifaceDescriptor(), sets);
        Method m = TypeSwitchNode.class.getDeclaredMethod(
                "implementsDyn", GoTypeDescriptor.class, List.class);
        m.setAccessible(true);
        try {
            m.invoke(node, dynDescriptor(), List.of("Load"));
            throw new AssertionError("expected FrontendException for sentinel dynamic type");
        } catch (java.lang.reflect.InvocationTargetException e) {
            Throwable cause = e.getCause();
            assertTrue(cause instanceof FrontendException,
                    "switch sentinel must throw FrontendException, got: " + cause);
            assertContains(cause.getMessage(), "cannot switch generic instantiation",
                    "switch message must name the failed switch");
            assertContains(cause.getMessage(), NEW_LABEL,
                    "switch message must carry the new label");
            assertNotContains(cause.getMessage(), OLD_LABEL,
                    "switch message must not carry the stale phase label");
        }
    }

    public void testBoxingSentinelMessage() {
        NodeFactory.BuildCtx ctx = boxingCtx(true);
        try {
            NodeFactory.checkInstantiationBoxedAsInterface(ctx, 0, 2, "test");
            throw new AssertionError("expected FrontendException for marked boxed type");
        } catch (FrontendException e) {
            assertContains(e.getMessage(), "cannot use generic instantiation *sync/atomic.Pointer[T]",
                    "boxing message must name the boxed type");
            assertContains(e.getMessage(), NEW_LABEL,
                    "boxing message must carry the new label");
            assertNotContains(e.getMessage(), OLD_LABEL,
                    "boxing message must not carry the stale phase label");
        }
        // Negative control: the same table without the marker boxes fine.
        NodeFactory.checkInstantiationBoxedAsInterface(boxingCtx(false), 0, 2, "test");
    }

    /** Minimal type table: marked (or unmarked) pointer + its named elem + a non-empty interface. */
    private static NodeFactory.BuildCtx boxingCtx(boolean marked) {
        String marker = marked ? ", \"templateMethodsSkipped\": true" : "";
        String json = "{ \"types\": ["
                + "{ \"kind\": \"pointer\", \"detail\": { \"elem\": 1, \"methods\": []" + marker + " } },"
                + "{ \"kind\": \"named\", \"detail\": { \"name\": \"sync/atomic.Pointer[T]\", \"methods\": [] } },"
                + "{ \"kind\": \"interface\", \"detail\": { \"empty\": false, \"methods\": [] } }"
                + "], \"objects\": [] }";
        Json.Object doc = (Json.Object) Json.parse(json);
        TypeTable types = new TypeTable(doc.array("types", "test"));
        Source source = Source.newBuilder("go", "", "test").build();
        return new NodeFactory.BuildCtx(source, FrameDescriptor.newBuilder(), types,
                doc.array("objects", "test"));
    }
}
