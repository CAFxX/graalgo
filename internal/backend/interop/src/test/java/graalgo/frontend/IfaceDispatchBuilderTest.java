package graalgo.frontend;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.FrameSlotKind;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;

import graalgo.iface.IfaceDispatchBuilder;
import graalgo.iface.IfaceMethodTable;
import graalgo.iface.IfaceMethodTarget;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import graalgo.values.GoTypeDescriptor;

/**
 * Unit tests for {@link IfaceDispatchBuilder} (P3-M3, WS D): the
 * {@code "ifacemethod"} wire node is built directly on parsed wire
 * fragments, following the ChanBuilderTest precedent (the builder takes
 * a {@link NodeFactory.BuildCtx} whose constructor is package-private,
 * so this test lives in {@code graalgo.frontend}).
 *
 * <p>verifies: GG-0014
 */
public final class IfaceDispatchBuilderTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(long e, long a, String msg) {
        if (e != a) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    /** A RootNode capturing its call arguments and returning a constant. */
    static final class CaptureRoot extends RootNode {
        final List<Object[]> seen = new ArrayList<>();
        private final Object ret;

        CaptureRoot(Object ret) {
            super(null);
            this.ret = ret;
        }

        @Override
        public Object execute(VirtualFrame frame) {
            seen.add(frame.getArguments().clone());
            return ret;
        }
    }

    private static final String POS = "{\"file\":0,\"line\":1,\"col\":1,\"len\":1}";

    /** Type ids in the test type table. */
    private static final int T_INT = 0;
    private static final int T_IFACE = 1;
    private static final int T_STRUCT = 2;
    private static final int T_SLICE = 3;
    private static final int T_SIG = 4;

    private static final String TYPES_JSON = "["
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"int\",\"name\":\"int\"}},"
            + "{\"kind\":\"interface\",\"detail\":{\"empty\":false,"
            + "\"methods\":[{\"name\":\"M\"}]}},"
            + "{\"kind\":\"struct\",\"detail\":{\"fields\":["
            + "{\"name\":\"x\",\"type\":0,\"embedded\":false}]}},"
            + "{\"kind\":\"slice\",\"detail\":{\"elem\":0}},"
            + "{\"kind\":\"signature\",\"detail\":{\"params\":["
            + "{\"type\":1},{\"type\":3}],\"results\":[],\"variadic\":true}}"
            + "]";

    private NodeFactory factory;
    private NodeFactory.BuildCtx ctx;
    private ArrayList<Json.Value> objects;

    private int declareObj(String name, int typeId, FrameSlotKind slotKind) {
        int slot = ctx.frames.addSlot(slotKind, name, null);
        int obj = objects.size();
        ctx.objSlots.put(obj, slot);
        objects.add(Json.parse("{\"kind\":\"var\",\"name\":\"" + name + "\",\"type\":" + typeId + "}"));
        return obj;
    }

    private void newBuild() {
        com.oracle.truffle.api.source.Source source =
                com.oracle.truffle.api.source.Source.newBuilder("go", "package main\n", "test.go").build();
        factory = new NodeFactory(source);
        factory.registerExprKind("ifacemethod", new IfaceDispatchBuilder(factory));
        Json.Array types = (Json.Array) Json.parse(TYPES_JSON);
        objects = new ArrayList<>();
        ctx = new NodeFactory.BuildCtx(source, FrameDescriptor.newBuilder(),
                new TypeTable(types), new Json.Array(objects));
    }

    private String ident(int obj, String name, int typeId) {
        return "{\"kind\":\"ident\",\"pos\":" + POS
                + ",\"fields\":{\"name\":\"" + name + "\",\"obj\":" + obj + ",\"type\":" + typeId + "}}";
    }

    /**
     * N-P1-1: a variadic interface method's FIXED interface-typed
     * parameter is still adapted (boxed) — the old variadic bail-out
     * skipped adaptation for fixed params too, so the callee received
     * the raw struct instead of an interface value. The trailing
     * variadic slice parameter is never an interface and passes
     * through untouched. Red: args[0] reaches the method as a raw
     * GoStruct, not a GoInterface.
     */
    public void testVariadicFixedInterfaceParamIsBoxed() {
        CaptureRoot root = new CaptureRoot(42L);
        IfaceMethodTable.clear();
        try {
            IfaceMethodTable.install(Map.of("main.T", Map.of("M",
                    new IfaceMethodTarget(new graalgo.values.GoFunc(root.getCallTarget(), null, null),
                            new int[0], true, false, -1, null))));
            newBuild();
            int recvObj = declareObj("w", T_IFACE, FrameSlotKind.Object);
            int structObj = declareObj("s", T_STRUCT, FrameSlotKind.Object);
            int sliceObj = declareObj("rest", T_SLICE, FrameSlotKind.Object);
            String nodeJson = "{\"kind\":\"ifacemethod\",\"pos\":" + POS + ",\"fields\":{"
                    + "\"recv\":" + ident(recvObj, "w", T_IFACE) + ","
                    + "\"method\":\"M\",\"iface\":" + T_IFACE + ","
                    + "\"args\":[" + ident(structObj, "s", T_STRUCT) + ","
                    + ident(sliceObj, "rest", T_SLICE) + "],"
                    + "\"sig\":" + T_SIG + ",\"type\":" + T_INT + "}}";
            GoExprNode n = factory.buildExpr((Json.Object) Json.parse(nodeJson), ctx);

            GoStructDescriptor desc = GoStructDescriptor.layout("S",
                    List.of("x"), List.of(GoKind.INT64));
            GoStruct recvData = new GoStruct(desc, new long[]{1}, new Object[0]);
            GoStruct argData = new GoStruct(desc, new long[]{7}, new Object[0]);
            int[] sliceData = new int[]{1, 2};
            VirtualFrame f = Truffle.getRuntime().createVirtualFrame(new Object[0], ctx.frames.build());
            f.setObject(ctx.objSlots.get(recvObj),
                    new GoInterface(new GoTypeDescriptor("main.T", GoKind.STRUCT, false),
                            recvData, null));
            f.setObject(ctx.objSlots.get(structObj), argData);
            f.setObject(ctx.objSlots.get(sliceObj), sliceData);

            assertEquals(42L, n.executeLong(f), "dispatch result");
            assertEquals(1, root.seen.size(), "method invoked once");
            Object[] callArgs = root.seen.get(0);
            // [GoFunc, receiver data, fixed arg, variadic slice arg].
            assertTrue(callArgs[1] == recvData, "params[0] is the receiver data");
            assertTrue(callArgs[2] instanceof GoInterface,
                    "fixed interface param is boxed; got "
                            + (callArgs[2] == null ? "null" : callArgs[2].getClass().getName()));
            GoInterface boxed = (GoInterface) callArgs[2];
            assertTrue(boxed.data() instanceof GoStruct, "boxed payload is the struct value");
            assertEquals(7L, ((GoStruct) boxed.data()).getLong(0), "boxed payload value");
            assertTrue(callArgs[3] == sliceData, "variadic slice arg passes through unboxed");
        } finally {
            IfaceMethodTable.clear();
        }
    }
}
