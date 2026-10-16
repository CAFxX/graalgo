package graalgo.frontend;

import java.util.ArrayList;

import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.Source;

import graalgo.p3abi.AbiFuncPCNode;
import graalgo.p3sync.launcher.P3KindRegistrations;
import graalgo.values.GoKind;

/**
 * Unit tests for the {@code "abifuncpc"} wire kind (real-reflect Phase 1:
 * the {@code internal/abi} {@code FuncPCABI0}/{@code FuncPCABIInternal}
 * compile intrinsics, built by {@link AbiFuncPCNode}).
 *
 * <p>Package placement follows the {@code RtMemStatsBuilderTest} precedent:
 * the builder takes a {@link NodeFactory.BuildCtx} whose constructor is
 * package-private, so the tests drive the factory's {@code buildExpr} on
 * parsed wire fragments — the same fragments the exporter emits.
 *
 * <p>{@link #testBareFactoryRejectsAbifuncpcWithWsDSignature} pins the exact
 * conformance failure: a {@code graalgo-run} launcher whose factory missed
 * the registration rejected the frontend's {@code "abifuncpc"} node with
 * {@code unsupported expression kind "abifuncpc" (WS D hook:
 * NodeFactory.registerExprKind)}. The root cause was that the registration
 * lived only in {@code P3SyncMain}'s inline table while {@code graalgo-run}
 * discovers kinds through the shared {@link P3KindRegistrations} provider;
 * {@link #testSharedRegistrationsBuildAbifuncpc} fails on any tree where
 * the shared provider drops the kind again.
 */
public final class AbiFuncPCBuilderTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long e, long a, String msg) {
        if (e != a) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    /** Type ids in the test type table: 0 = uintptr, 1 = string. */
    private static final String TYPES_JSON = "["
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"uint\",\"name\":\"uintptr\"}},"
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"string\",\"name\":\"string\"}}"
            + "]";

    private static final String POS = "{\"file\":0,\"line\":1,\"col\":1,\"len\":1}";

    private NodeFactory factory;
    private NodeFactory.BuildCtx ctx;

    /** Test double: an expression node evaluating to null (test-only wire kind). */
    private static final class NullNode extends GoExprNode {
        NullNode() {
            super(GoKind.UINT64);
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return null;
        }
    }

    private void newBuild() {
        Source source = Source.newBuilder("go", "package main\n", "test.go").build();
        factory = new NodeFactory(source);
        // The exact production path graalgo-run takes: the shared
        // ServiceLoader-discovered WireKindRegistrar provider.
        new P3KindRegistrations().registerAll(factory);
        // Test-only double for the "arg" child position.
        factory.registerExprKind("testnil", (n, c) -> new NullNode());
        Json.Array types = (Json.Array) Json.parse(TYPES_JSON);
        ctx = new NodeFactory.BuildCtx(source, FrameDescriptor.newBuilder(),
                new TypeTable(types), new Json.Array(new ArrayList<Json.Value>()));
    }

    private GoExprNode buildExpr(String json) {
        return factory.buildExpr((Json.Object) Json.parse(json), ctx);
    }

    private String abifuncpcNode(String intrinsicKind, String argKind, int typeId) {
        return "{\"kind\":\"abifuncpc\",\"pos\":" + POS + ",\"fields\":{"
                + "\"kind\":\"" + intrinsicKind + "\","
                + "\"arg\":{\"kind\":\"" + argKind + "\",\"pos\":" + POS + ",\"fields\":{}},"
                + "\"type\":" + typeId + "}}";
    }

    // ---------- tests ----------

    /**
     * The regression test: the shared provider must map the
     * {@code "abifuncpc"} wire kind. Fails with {@code unsupported
     * expression kind "abifuncpc"} on any tree where the registration is
     * missing from {@link P3KindRegistrations} (the conformance red-master
     * failure).
     */
    public void testSharedRegistrationsBuildAbifuncpc() {
        newBuild();
        GoExprNode n = buildExpr(abifuncpcNode("FuncPCABIInternal", "testnil", 0));
        assertTrue(n instanceof AbiFuncPCNode,
                "abifuncpc must build an AbiFuncPCNode, got: " + n.getClass().getName());
        assertEq(0L, n.executeLong(null),
                "abifuncpc of a nil func value must be 0");
    }

    /** Both intrinsics lower through the same registration. */
    public void testSharedRegistrationsBuildFuncPCABI0() {
        newBuild();
        GoExprNode n = buildExpr(abifuncpcNode("FuncPCABI0", "testnil", 0));
        assertTrue(n instanceof AbiFuncPCNode,
                "abifuncpc/FuncPCABI0 must build an AbiFuncPCNode, got: "
                        + n.getClass().getName());
    }

    /** Pins the exact conformance failure on a factory missing the registration. */
    public void testBareFactoryRejectsAbifuncpcWithWsDSignature() {
        Source source = Source.newBuilder("go", "package main\n", "test.go").build();
        NodeFactory bare = new NodeFactory(source);
        Json.Array types = (Json.Array) Json.parse(TYPES_JSON);
        NodeFactory.BuildCtx bareCtx = new NodeFactory.BuildCtx(source,
                FrameDescriptor.newBuilder(), new TypeTable(types),
                new Json.Array(new ArrayList<Json.Value>()));
        try {
            bare.buildExpr(
                    (Json.Object) Json.parse(abifuncpcNode("FuncPCABIInternal", "testnil", 0)),
                    bareCtx);
            throw new AssertionError("expected FrontendException for unregistered kind");
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("unsupported expression kind \"abifuncpc\""),
                    "must carry the conformance failure signature, got: " + e.getMessage());
            assertTrue(e.getMessage().contains("NodeFactory.registerExprKind"),
                    "must name the WS D hook, got: " + e.getMessage());
        }
    }

    public void testAbifuncpcRejectsUnknownIntrinsicKind() {
        newBuild();
        try {
            buildExpr(abifuncpcNode("BogusIntrinsic", "testnil", 0));
            throw new AssertionError("expected FrontendException for unknown abifuncpc kind");
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("abifuncpc"),
                    "error must name the node, got: " + e.getMessage());
        }
    }

    public void testAbifuncpcRejectsMissingArg() {
        newBuild();
        try {
            buildExpr("{\"kind\":\"abifuncpc\",\"pos\":" + POS
                    + ",\"fields\":{\"kind\":\"FuncPCABI0\",\"type\":0}}");
            throw new AssertionError("expected FrontendException for missing arg");
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("abifuncpc"),
                    "error must name the node, got: " + e.getMessage());
        }
    }

    public void testAbifuncpcRejectsNonUintptrType() {
        newBuild();
        try {
            buildExpr(abifuncpcNode("FuncPCABI0", "testnil", 1));
            throw new AssertionError("expected FrontendException for non-uintptr result type");
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("abifuncpc"),
                    "error must name the node, got: " + e.getMessage());
        }
    }
}
