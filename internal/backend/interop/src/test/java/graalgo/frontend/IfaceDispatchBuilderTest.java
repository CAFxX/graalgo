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
import com.oracle.truffle.api.source.Source;

import graalgo.iface.IfaceDispatchBuilder;
import graalgo.iface.IfaceMethodTable;
import graalgo.iface.IfaceMethodTarget;
import graalgo.values.GoFunc;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;
import graalgo.values.GoTypeDescriptor;

/**
 * Unit tests for {@link IfaceDispatchBuilder} (R1-11 remediation).
 *
 * <p>Package placement is deliberate: the builder takes a
 * {@link NodeFactory.BuildCtx} whose constructor is package-private, so
 * the test lives in {@code graalgo.frontend} (the
 * {@code DescriptorNamesTest} / {@code ChanBuilderTest} precedent) and
 * drives the builder directly on a wire fragment shaped like the
 * exporter's {@code ifacemethod} node.
 *
 * <p>Red-green discipline: the variadic test fails against the old
 * builder (variadic signatures returned null param ids, so the fixed
 * interface-typed parameter was passed unboxed and the callee saw a raw
 * {@code Long}) and passes against the fixed builder (the fixed param
 * is boxed into a {@code GoInterface} carrying the {@code int}
 * descriptor, exactly like the factory's fix-redmaster-r40 path).
 */
public final class IfaceDispatchBuilderTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(Object expected, Object actual, String msg) {
        boolean eq = expected == null ? actual == null : expected.equals(actual);
        if (!eq) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
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

    // Type ids in the test type table.
    private static final int T_INT = 0;
    private static final int T_IFACE = 1;
    private static final int T_SIG = 2;
    private static final int T_SLICE_STRING = 3;
    private static final int T_STRING = 4;

    private static final String TYPES_JSON = "["
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"int\",\"name\":\"int\"}},"
            + "{\"kind\":\"interface\",\"detail\":{\"empty\":false,\"methods\":[{\"name\":\"M\"}]}},"
            + "{\"kind\":\"signature\",\"detail\":{\"params\":[{\"name\":\"w\",\"type\":1},"
            + "{\"name\":\"parts\",\"type\":3}],\"variadic\":true}},"
            + "{\"kind\":\"slice\",\"detail\":{\"elem\":4}},"
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"string\",\"name\":\"string\"}}"
            + "]";

    // Expression nodes build Truffle source sections from pos, which
    // requires "file" (unlike the statement builders ChanBuilderTest
    // drives, whose nodes skip section creation).
    private static final String POS = "{\"file\":0,\"line\":1,\"col\":1,\"len\":1}";

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

    private String ident(int obj, String name, int typeId) {
        return "{\"kind\":\"ident\",\"pos\":" + POS
                + ",\"fields\":{\"name\":\"" + name + "\",\"obj\":" + obj + ",\"type\":" + typeId + "}}";
    }

    /**
     * Builds an ifacemethod node for {@code M(w, parts...)} on a receiver
     * of the test interface, with a packed variadic call: the wire args
     * are the fixed arg plus one slice arg, aligning 1:1 with the
     * signature params (the exporter's {@code exportCallArgsPacked}
     * shape). The fixed arg is an int variable passed to the
     * interface-typed first parameter — it must arrive boxed.
     */
    public void testVariadicFixedInterfaceParamIsBoxed() {
        Source source = Source.newBuilder("go", "package main\n", "test.go").build();
        factory = new NodeFactory(source);
        Json.Array types = (Json.Array) Json.parse(TYPES_JSON);
        objects = new ArrayList<>();
        ctx = new NodeFactory.BuildCtx(source, FrameDescriptor.newBuilder(),
                new TypeTable(types), new Json.Array(objects));
        int recvObj = declareObj("recv", T_IFACE, FrameSlotKind.Object);
        int argObj = declareObj("w", T_INT, FrameSlotKind.Long);
        int sliceObj = declareObj("parts", T_SLICE_STRING, FrameSlotKind.Object);
        int recvSlot = ctx.objSlots.get(recvObj);
        int argSlot = ctx.objSlots.get(argObj);
        int sliceSlot = ctx.objSlots.get(sliceObj);

        CaptureRoot root = new CaptureRoot(99L);
        RootCallTarget target = root.getCallTarget();
        GoFunc func = new GoFunc(target, null, null);
        IfaceMethodTable.clear();
        try {
            IfaceMethodTable.install(Map.of("main.T", Map.of("M",
                    new IfaceMethodTarget(func, new int[0], true, false, -1, null))));
            String wire = "{\"kind\":\"ifacemethod\",\"pos\":" + POS + ",\"fields\":{"
                    + "\"recv\":" + ident(recvObj, "recv", T_IFACE)
                    + ",\"method\":\"M\",\"iface\":" + T_IFACE
                    + ",\"args\":[" + ident(argObj, "w", T_INT) + ","
                    + ident(sliceObj, "parts", T_SLICE_STRING) + "]"
                    + ",\"sig\":" + T_SIG + ",\"type\":null}}";
            GoExprNode node = new IfaceDispatchBuilder(factory)
                    .build((Json.Object) Json.parse(wire), ctx);
            VirtualFrame frame = Truffle.getRuntime().createVirtualFrame(
                    new Object[0], ctx.frames.build());
            frame.setObject(recvSlot,
                    new GoInterface(new GoTypeDescriptor("main.T", GoKind.STRUCT, false),
                            "DATA", null));
            frame.setLong(argSlot, 42L);
            GoSlice slice = new GoSlice(new Object[0], 0, 0, 0);
            frame.setObject(sliceSlot, slice);
            assertEquals(99L, node.executeObject(frame), "dispatch result");
            assertEquals(1, root.seen.size(), "method invoked once");
            // GoCalls prepends the GoFunc itself at callArgs[0]; the
            // receiver data is params[0] at index 1.
            assertEquals("DATA", root.seen.get(0)[1], "params[0] is the receiver data");
            Object boxed = root.seen.get(0)[2];
            assertTrue(boxed instanceof GoInterface,
                    "fixed interface param arrives boxed, got "
                            + (boxed == null ? "null" : boxed.getClass().getName()));
            GoInterface gi = (GoInterface) boxed;
            assertEquals(42L, gi.data(), "boxed value is the int argument");
            assertEquals("int", ((GoTypeDescriptor) gi.descriptor()).name(),
                    "boxed descriptor is the int type");
            assertTrue(root.seen.get(0)[3] == slice, "packed slice arg passes through");
        } finally {
            IfaceMethodTable.clear();
        }
    }
}
