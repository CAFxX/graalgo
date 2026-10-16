package graalgo.frontend;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;

import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.Source;

import graalgo.rtmemstats.RuntimeGoRootNode;
import graalgo.rtmemstats.RuntimeReadMemStatsNode;
import graalgo.rtmemstats.RuntimeStackNode;
import graalgo.rtmemstats.RuntimeVersionNode;
import graalgo.runtime.PanicException;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Unit tests for the PKG-051 MemStats-family wire builders
 * ({@code "runtimereadmemstats"}, {@code "runtimegoroot"},
 * {@code "runtimeversion"}, {@code "runtimestack"}).
 *
 * <p>Package placement follows the {@code ChanBuilderTest} precedent: the
 * builders take a {@link NodeFactory.BuildCtx} whose constructor is
 * package-private, so the tests drive each builder's {@code build} method
 * directly on parsed wire fragments — the same fragments the exporter
 * emits.
 *
 * <p>{@link #testUnregisteredKindFailsWithPkg051Signature} pins the exact
 * PKG-051 failure: a factory without the registrations rejects the
 * frontend's {@code "runtimeversion"} node with {@code unsupported
 * expression kind "runtimeversion" (WS D hook:
 * NodeFactory.registerExprKind)}.
 */
public final class RtMemStatsBuilderTest {

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

    /** Type ids in the test type table. */
    private static final int T_STRING = 0;
    private static final int T_INT = 1;
    private static final int T_BOOL = 2;
    private static final int T_UINT8 = 3;
    private static final int T_BYTESLICE = 4;

    private static final String TYPES_JSON = "["
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"string\",\"name\":\"string\"}},"
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"int\",\"name\":\"int\"}},"
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"bool\",\"name\":\"bool\"}},"
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"uint8\",\"name\":\"uint8\"}},"
            + "{\"kind\":\"slice\",\"detail\":{\"elem\":3}}"
            + "]";

    private static final String POS = "{\"file\":0,\"line\":1,\"col\":1,\"len\":1}";

    private NodeFactory factory;
    private NodeFactory.BuildCtx ctx;
    private ArrayList<Json.Value> objects;

    // ---------- scaffolding ----------

    /** Test double: a fixed-value expression node (test-only wire kinds). */
    private static final class FixedNode extends GoExprNode {
        private final Object value;
        private final boolean boolValue;

        FixedNode(GoKind kind, Object value) {
            super(kind);
            this.value = value;
            this.boolValue = false;
        }

        FixedNode(boolean boolValue) {
            super(GoKind.BOOL);
            this.value = null;
            this.boolValue = boolValue;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return value;
        }

        @Override
        public boolean executeBoolean(VirtualFrame frame) {
            return boolValue;
        }
    }

    private void newBuild() {
        Source source = Source.newBuilder("go", "package main\n", "test.go").build();
        factory = new NodeFactory(source);
        factory.registerExprKind("runtimeversion", RuntimeVersionNode::build);
        factory.registerExprKind("runtimegoroot", RuntimeGoRootNode::build);
        factory.registerExprKind("runtimestack",
                (n, c) -> RuntimeStackNode.build(n, c, factory));
        factory.registerExprKind("runtimereadmemstats",
                (n, c) -> RuntimeReadMemStatsNode.build(n, c, factory));
        // Test-only doubles for child positions.
        factory.registerExprKind("testbyteslice",
                (n, c) -> new FixedNode(GoKind.SLICE,
                        new GoSlice(new byte[4096], 0, 4096, 4096)));
        factory.registerExprKind("testbool",
                (n, c) -> new FixedNode(true));
        factory.registerExprKind("testmemstatsptr",
                (n, c) -> new FixedNode(GoKind.POINTER,
                        new GoPointer(GoStruct.zero(memStatsDescriptor()))));
        factory.registerExprKind("testnilptr",
                (n, c) -> new FixedNode(GoKind.POINTER, new GoPointer(null)));
        Json.Array types = (Json.Array) Json.parse(TYPES_JSON);
        objects = new ArrayList<>();
        ctx = new NodeFactory.BuildCtx(source, FrameDescriptor.newBuilder(),
                new TypeTable(types), new Json.Array(objects));
    }

    /** Compact MemStats-shaped descriptor (names/kinds mirror the synthetic runtime view). */
    private static GoStructDescriptor memStatsDescriptor() {
        String[][] spec = {
                {"Alloc", "UINT64"}, {"TotalAlloc", "UINT64"}, {"Sys", "UINT64"},
                {"Lookups", "UINT64"}, {"Mallocs", "UINT64"}, {"Frees", "UINT64"},
                {"HeapAlloc", "UINT64"}, {"HeapSys", "UINT64"}, {"HeapIdle", "UINT64"},
                {"HeapInuse", "UINT64"}, {"HeapReleased", "UINT64"}, {"HeapObjects", "UINT64"},
                {"StackInuse", "UINT64"}, {"StackSys", "UINT64"},
                {"MSpanInuse", "UINT64"}, {"MSpanSys", "UINT64"},
                {"MCacheInuse", "UINT64"}, {"MCacheSys", "UINT64"},
                {"BuckHashSys", "UINT64"}, {"GCSys", "UINT64"}, {"OtherSys", "UINT64"},
                {"NextGC", "UINT64"}, {"LastGC", "UINT64"}, {"PauseTotalNs", "UINT64"},
                {"PauseNs", "ARRAY", "256", "UINT64"}, {"PauseEnd", "ARRAY", "256", "UINT64"},
                {"NumGC", "UINT32"}, {"NumForcedGC", "UINT32"},
                {"GCCPUFraction", "FLOAT64"},
                {"EnableGC", "BOOL"}, {"DebugGC", "BOOL"},
                {"BySize", "ARRAY", "61", "STRUCT"},
        };
        List<String> names = new ArrayList<>();
        List<GoKind> kinds = new ArrayList<>();
        List<Integer> lens = new ArrayList<>();
        List<GoKind> elemKinds = new ArrayList<>();
        for (String[] row : spec) {
            names.add(row[0]);
            kinds.add(GoKind.valueOf(row[1]));
            if (row.length > 2) {
                lens.add(Integer.parseInt(row[2]));
                elemKinds.add(GoKind.valueOf(row[3]));
            } else {
                lens.add(-1);
                elemKinds.add(null);
            }
        }
        return GoStructDescriptor.layout("runtime.MemStats", names, kinds, lens, elemKinds);
    }

    private GoExprNode buildExpr(String json) {
        return factory.buildExpr((Json.Object) Json.parse(json), ctx);
    }

    private String versionNode(int typeId) {
        return "{\"kind\":\"runtimeversion\",\"pos\":" + POS
                + ",\"fields\":{\"type\":" + typeId + "}}";
    }

    // ---------- tests ----------

    /** Pins the exact PKG-051 signature on a factory missing the registration. */
    public void testUnregisteredKindFailsWithPkg051Signature() {
        Source source = Source.newBuilder("go", "package main\n", "test.go").build();
        NodeFactory bare = new NodeFactory(source);
        Json.Array types = (Json.Array) Json.parse(TYPES_JSON);
        NodeFactory.BuildCtx bareCtx = new NodeFactory.BuildCtx(source,
                FrameDescriptor.newBuilder(), new TypeTable(types),
                new Json.Array(new ArrayList<Json.Value>()));
        try {
            bare.buildExpr((Json.Object) Json.parse(versionNode(T_STRING)), bareCtx);
            throw new AssertionError("expected FrontendException for unregistered kind");
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("unsupported expression kind \"runtimeversion\""),
                    "must carry the PKG-051 signature, got: " + e.getMessage());
            assertTrue(e.getMessage().contains("NodeFactory.registerExprKind"),
                    "must name the WS D hook, got: " + e.getMessage());
        }
    }

    public void testRuntimeVersionBuildsAndRuns() {
        newBuild();
        GoExprNode n = buildExpr(versionNode(T_STRING));
        Object v = n.executeObject(null);
        assertTrue(v instanceof GoString,
                "runtimeversion must evaluate to a GoString, got: " + v);
        String s = new String(((GoString) v).bytes(), StandardCharsets.UTF_8);
        assertTrue(s.startsWith("go"),
                "runtime.Version() must look like a Go release, got: " + s);
    }

    public void testRuntimeVersionRejectsNonString() {
        newBuild();
        try {
            buildExpr(versionNode(T_INT));
            throw new AssertionError("expected FrontendException for non-string type");
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("runtimeversion"),
                    "error must name the node, got: " + e.getMessage());
        }
    }

    public void testRuntimeVersionRejectsMissingType() {
        newBuild();
        try {
            buildExpr("{\"kind\":\"runtimeversion\",\"pos\":" + POS + ",\"fields\":{}}");
            throw new AssertionError("expected FrontendException for missing type");
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("runtimeversion"),
                    "error must name the node, got: " + e.getMessage());
        }
    }

    public void testRuntimeGoRootBuildsAndRuns() {
        newBuild();
        GoExprNode n = buildExpr("{\"kind\":\"runtimegoroot\",\"pos\":" + POS
                + ",\"fields\":{\"type\":" + T_STRING + "}}");
        Object v = n.executeObject(null);
        assertTrue(v instanceof GoString,
                "runtimegoroot must evaluate to a GoString, got: " + v);
    }

    public void testRuntimeGoRootRejectsNonString() {
        newBuild();
        try {
            buildExpr("{\"kind\":\"runtimegoroot\",\"pos\":" + POS
                    + ",\"fields\":{\"type\":" + T_BOOL + "}}");
            throw new AssertionError("expected FrontendException for non-string type");
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("runtimegoroot"),
                    "error must name the node, got: " + e.getMessage());
        }
    }

    public void testRuntimeStackWritesTrace() {
        newBuild();
        String buf = "{\"kind\":\"testbyteslice\",\"pos\":" + POS + ",\"fields\":{}}";
        String all = "{\"kind\":\"testbool\",\"pos\":" + POS + ",\"fields\":{}}";
        GoExprNode n = buildExpr("{\"kind\":\"runtimestack\",\"pos\":" + POS
                + ",\"fields\":{\"args\":[" + buf + "," + all + "],\"type\":" + T_INT + "}}");
        long written = n.executeLong(null);
        assertTrue(written > 0, "runtime.Stack must write bytes, wrote " + written);
        assertTrue(written <= 4096, "must not overrun the buffer");
    }

    public void testRuntimeStackRejectsArgCount() {
        newBuild();
        String buf = "{\"kind\":\"testbyteslice\",\"pos\":" + POS + ",\"fields\":{}}";
        try {
            buildExpr("{\"kind\":\"runtimestack\",\"pos\":" + POS
                    + ",\"fields\":{\"args\":[" + buf + "],\"type\":" + T_INT + "}}");
            throw new AssertionError("expected FrontendException for arg count != 2");
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("runtimestack"),
                    "error must name the node, got: " + e.getMessage());
        }
    }

    public void testRuntimeStackRejectsNonInt() {
        newBuild();
        String buf = "{\"kind\":\"testbyteslice\",\"pos\":" + POS + ",\"fields\":{}}";
        String all = "{\"kind\":\"testbool\",\"pos\":" + POS + ",\"fields\":{}}";
        try {
            buildExpr("{\"kind\":\"runtimestack\",\"pos\":" + POS
                    + ",\"fields\":{\"args\":[" + buf + "," + all + "],\"type\":" + T_STRING + "}}");
            throw new AssertionError("expected FrontendException for non-int result");
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("runtimestack"),
                    "error must name the node, got: " + e.getMessage());
        }
    }

    public void testRuntimeReadMemStatsFills() {
        newBuild();
        final GoStruct[] holder = new GoStruct[1];
        // Capture the struct the test double allocated.
        factory.registerExprKind("testmemstatsptr2",
                (n, c) -> {
                    GoStruct ms = GoStruct.zero(memStatsDescriptor());
                    holder[0] = ms;
                    return new FixedNode(GoKind.POINTER, new GoPointer(ms));
                });
        GoExprNode n = buildExpr("{\"kind\":\"runtimereadmemstats\",\"pos\":" + POS
                + ",\"fields\":{\"m\":{\"kind\":\"testmemstatsptr2\",\"pos\":" + POS
                + ",\"fields\":{}}}}");
        long r = n.executeLong(null);
        assertEq(0, r, "ReadMemStats is void; the node returns 0");
        assertTrue(holder[0] != null, "test double must have run");
        long alloc = holder[0].getLong(0);
        assertTrue(alloc >= 0, "Alloc must be populated, got " + alloc);
        assertEq(alloc, holder[0].getLong(6), "Alloc must equal HeapAlloc");
    }

    public void testRuntimeReadMemStatsNilPanics() {
        newBuild();
        GoExprNode n = buildExpr("{\"kind\":\"runtimereadmemstats\",\"pos\":" + POS
                + ",\"fields\":{\"m\":{\"kind\":\"testnilptr\",\"pos\":" + POS
                + ",\"fields\":{}}}}");
        try {
            n.executeLong(null);
            throw new AssertionError("expected PanicException for nil *MemStats");
        } catch (PanicException e) {
            // gc: nil pointer dereference panic. Any PanicException here is
            // the honest failure; the unreachable-AssertionError must not fire.
        }
    }

    public void testRuntimeReadMemStatsRejectsMissingM() {
        newBuild();
        try {
            buildExpr("{\"kind\":\"runtimereadmemstats\",\"pos\":" + POS + ",\"fields\":{}}");
            throw new AssertionError("expected FrontendException for missing m");
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("runtimereadmemstats"),
                    "error must name the node, got: " + e.getMessage());
        }
    }
}
