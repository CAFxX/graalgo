package graalgo.frontend;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;

import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.source.Source;

import graalgo.rtstack.RuntimeStackMethodValueNode;
import graalgo.runtime.GoFuncInfo;
import graalgo.runtime.PanicException;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoString;

/**
 * Unit tests for the P1-3 {@code "rtstackmethodvalue"} wire builder
 * (method values {@code fn.Name} / {@code fn.Entry} of the synthetic
 * {@code runtime} stack-inspection methods).
 *
 * <p>Package placement follows the {@code ChanBuilderTest} precedent:
 * the builder takes a {@link NodeFactory.BuildCtx} whose constructor
 * is package-private, so the tests drive the builder's {@code build}
 * method directly on parsed wire fragments — the same fragments the
 * exporter emits. Invocation of the returned {@link GoFunc} goes
 * through its {@code RootCallTarget} directly, with the calling
 * convention of {@link graalgo.runtime.GoCalls#callArgs}:
 * {@code args[0]} is the {@code GoFunc} itself, {@code args[1]} the
 * bound receiver.
 */
public final class RtStackMethodValueBuilderTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    /** Type ids in the test type table. */
    private static final int T_FUNC = 0;
    private static final int T_STRING = 1;

    private static final String TYPES_JSON = "["
            + "{\"kind\":\"signature\",\"detail\":{\"params\":[],"
            + "\"results\":[{\"name\":\"\",\"type\":1}],\"variadic\":false}},"
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"string\",\"name\":\"string\"}}"
            + "]";

    private static final String POS = "{\"file\":0,\"line\":1,\"col\":1,\"len\":1}";

    private NodeFactory factory;
    private NodeFactory.BuildCtx ctx;

    /** Test double: a fixed-value expression node (test-only wire kinds). */
    private static final class FixedNode extends GoExprNode {
        private final Object value;

        FixedNode(GoKind kind, Object value) {
            super(kind);
            this.value = value;
        }

        @Override
        public Object executeObject(com.oracle.truffle.api.frame.VirtualFrame frame) {
            return value;
        }
    }

    private void newBuild() {
        Source source = Source.newBuilder("go", "package main\n", "test.go").build();
        factory = new NodeFactory(source);
        factory.registerExprKind("rtstackmethodvalue",
                (n, c) -> RuntimeStackMethodValueNode.build(n, c, factory));
        factory.registerExprKind("testfuncptr",
                (n, c) -> new FixedNode(GoKind.POINTER,
                        new GoPointer(new GoFuncInfo("main.main", 42L))));
        factory.registerExprKind("testnilptr",
                (n, c) -> new FixedNode(GoKind.POINTER, new GoPointer(null)));
        Json.Array types = (Json.Array) Json.parse(TYPES_JSON);
        ctx = new NodeFactory.BuildCtx(source, FrameDescriptor.newBuilder(),
                new TypeTable(types), new Json.Array(new ArrayList<>()));
    }

    private GoExprNode buildExpr(String json) {
        return factory.buildExpr((Json.Object) Json.parse(json), ctx);
    }

    private String methodValueNode(String op, String recvKind) {
        return "{\"kind\":\"rtstackmethodvalue\",\"pos\":" + POS
                + ",\"fields\":{\"op\":\"" + op + "\",\"recv\":"
                + "{\"kind\":\"" + recvKind + "\",\"pos\":" + POS
                + ",\"fields\":{}},\"type\":" + T_FUNC + "}}";
    }

    private static String goStringText(Object v) {
        assertTrue(v instanceof GoString,
                "expected a GoString, got: " + v);
        return new String(((GoString) v).bytes(), StandardCharsets.UTF_8);
    }

    /** Pins the exact P1-3 signature on a factory missing the registration. */
    public void testUnregisteredKindFails() {
        Source source = Source.newBuilder("go", "package main\n", "test.go").build();
        NodeFactory bare = new NodeFactory(source);
        Json.Array types = (Json.Array) Json.parse(TYPES_JSON);
        NodeFactory.BuildCtx bareCtx = new NodeFactory.BuildCtx(source,
                FrameDescriptor.newBuilder(), new TypeTable(types),
                new Json.Array(new ArrayList<>()));
        try {
            bare.buildExpr((Json.Object) Json.parse(methodValueNode("Name", "testfuncptr")),
                    bareCtx);
            throw new AssertionError("expected FrontendException for unregistered kind");
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("unsupported expression kind \"rtstackmethodvalue\""),
                    "must carry the unregistered-kind signature, got: " + e.getMessage());
        }
    }

    public void testNameBuildsAndInvokes() {
        newBuild();
        GoExprNode n = buildExpr(methodValueNode("Name", "testfuncptr"));
        Object v = n.executeObject(null);
        assertTrue(v instanceof GoFunc,
                "rtstackmethodvalue must evaluate to a GoFunc, got: " + v);
        GoFunc fn = (GoFunc) v;
        assertTrue(fn.boundArgs().length == 1,
                "bound receiver must be a single argument, got: " + fn.boundArgs().length);
        Object out = fn.target().call(new Object[] {fn, fn.boundArgs()[0]});
        assertTrue("main.main".equals(goStringText(out)),
                "fn.Name must return the qualified name, got: " + out);
    }

    public void testEntryBuildsAndInvokes() {
        newBuild();
        GoExprNode n = buildExpr(methodValueNode("Entry", "testfuncptr"));
        Object v = n.executeObject(null);
        assertTrue(v instanceof GoFunc,
                "rtstackmethodvalue must evaluate to a GoFunc, got: " + v);
        GoFunc fn = (GoFunc) v;
        Object out = fn.target().call(new Object[] {fn, fn.boundArgs()[0]});
        assertTrue(out instanceof Long && ((Long) out) == 42L,
                "fn.Entry must return the interned entry PC, got: " + out);
    }

    public void testNilReceiverPanicsAtInvocationNotFormation() {
        newBuild();
        GoExprNode n = buildExpr(methodValueNode("Name", "testnilptr"));
        // Formation on a nil receiver is legal (like an ordinary method
        // value on a nil pointer); the panic happens at invocation.
        Object v = n.executeObject(null);
        assertTrue(v instanceof GoFunc,
                "nil-receiver method value must still form a GoFunc, got: " + v);
        GoFunc fn = (GoFunc) v;
        try {
            fn.target().call(new Object[] {fn, fn.boundArgs()[0]});
            throw new AssertionError("expected PanicException for nil receiver");
        } catch (PanicException e) {
            // Expected: gc's nil-dereference panic.
        }
    }

    public void testBadOpFails() {
        newBuild();
        try {
            buildExpr(methodValueNode("FileLine", "testfuncptr"));
            throw new AssertionError("expected FrontendException for bad op");
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("rtstackmethodvalue"),
                    "error must name the node, got: " + e.getMessage());
        }
    }

    public void testNonFuncTypeFails() {
        newBuild();
        String node = "{\"kind\":\"rtstackmethodvalue\",\"pos\":" + POS
                + ",\"fields\":{\"op\":\"Name\",\"recv\":"
                + "{\"kind\":\"testfuncptr\",\"pos\":" + POS + ",\"fields\":{}},"
                + "\"type\":" + T_STRING + "}}";
        try {
            buildExpr(node);
            throw new AssertionError("expected FrontendException for non-func type");
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("rtstackmethodvalue"),
                    "error must name the node, got: " + e.getMessage());
        }
    }

    /** Standalone runner mirroring IfaceTestRunner semantics. */
    public static void main(String[] args) throws Exception {
        RtStackMethodValueBuilderTest t = new RtStackMethodValueBuilderTest();
        int failures = 0;
        for (java.lang.reflect.Method m : RtStackMethodValueBuilderTest.class.getMethods()) {
            if (!m.getName().startsWith("test") || m.getParameterCount() != 0) {
                continue;
            }
            try {
                m.invoke(t);
                System.out.println("PASS " + m.getName());
            } catch (Exception e) {
                failures++;
                System.out.println("FAIL " + m.getName() + ": " + e.getCause());
            }
        }
        if (failures > 0) {
            System.exit(1);
        }
    }
}
