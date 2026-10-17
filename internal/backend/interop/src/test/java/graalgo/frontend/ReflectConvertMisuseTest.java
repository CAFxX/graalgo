package graalgo.frontend;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.Source;

import graalgo.p3reflect.ReflectMisuseException;
import graalgo.p3reflect.ReflectOpNode;
import graalgo.p3reflect.ReflectRefs;
import graalgo.p3reflect.ReflectTypes;
import graalgo.p3reflect.ReflectValues;
import graalgo.p3unsafe.UnsafeTable;
import graalgo.runtime.PanicException;
import graalgo.values.GoKind;
import graalgo.values.GoString;
import graalgo.values.GoStruct;

/**
 * R2 (review P1-8), remediated per the REJECT verdict: the reflect node's
 * boundary translates ONLY {@link ReflectMisuseException} — genuine guest
 * misuse, where gc would panic (ADR-0002 D4) — into a Go panic. Plain
 * {@code IllegalStateException}s (frontend/backend contract violations,
 * unimplemented-feature markers) must escape as internal errors, never as
 * guest-recoverable panics.
 *
 * <p>A program hitting an unsupported {@code reflect.Value.Convert} got
 * exit 1; gc panics (exit 2) — a differential-corpus-visible divergence.
 *
 * <p>Package placement is deliberate: the builders take a
 * {@link NodeFactory.BuildCtx} whose constructor is package-private, so
 * the tests live in {@code graalgo.frontend} (the
 * {@code DescriptorNamesTest} precedent) and drive
 * {@link ReflectOpNode#build} directly on parsed wire fragments.
 *
 * <p>Registered in {@code graalgo.iface.IfaceTestRunner}.
 */
public final class ReflectConvertMisuseTest {

    /** Wire type ids in the test type table. */
    private static final int T_INT = 0;
    private static final int T_STRING = 1;
    private static final int T_UNSAFEPTR = 2;
    private static final int T_VALUE_STRUCT = 3;

    private static final String TYPES_JSON = "["
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"int\",\"name\":\"int\",\"untyped\":false}},"
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"string\",\"name\":\"string\",\"untyped\":false}},"
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"pointer\",\"name\":\"Pointer\",\"untyped\":false}},"
            + "{\"kind\":\"struct\",\"detail\":{\"fields\":["
            + "{\"name\":\"typ\",\"tag\":\"\",\"offset\":0,\"type\":" + T_UNSAFEPTR + "},"
            + "{\"name\":\"ptr\",\"tag\":\"\",\"offset\":8,\"type\":" + T_UNSAFEPTR + "},"
            + "{\"name\":\"flag\",\"tag\":\"\",\"offset\":16,\"type\":" + T_INT + "}]}},"
            + "{\"kind\":\"named\",\"detail\":{\"name\":\"reflect.Value\",\"underlying\":"
            + T_VALUE_STRUCT + ",\"methods\":[]}}"
            + "]";

    private static final String POS = "{\"file\":0,\"line\":1,\"col\":1,\"len\":1}";

    private NodeFactory factory;
    private NodeFactory.BuildCtx ctx;
    private final List<GoExprNode> constNodes = new ArrayList<>();

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    /** Constant node (test scaffolding): evaluates to a fixed D5 value. */
    private static final class ConstNode extends GoExprNode {
        private final Object value;

        ConstNode(Object value, GoKind kind) {
            super(kind);
            this.value = value;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return value;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return ((Number) value).longValue();
        }
    }

    private void newBuild() {
        Source source = Source.newBuilder("go", "package main\n", "test.go").build();
        factory = new NodeFactory(source);
        // Test-only constant args, resolved by index from the wire fragment.
        factory.registerExprKind("testconst", (n, c) -> {
            Json.Object f = n.object("fields", "testconst");
            int idx = (int) f.longInt("idx", "testconst.fields");
            return constNodes.get(idx);
        });
        Json.Array types = (Json.Array) Json.parse(TYPES_JSON);
        ctx = new NodeFactory.BuildCtx(source, FrameDescriptor.newBuilder(),
                new TypeTable(types), new Json.Array(new ArrayList<>()));
        // The launcher's per-run reflect table install, plus the unsafe
        // table the reflect bindings intern through (UnsafeTable.current()
        // fail-fasts without it — valueOf/denotedValue need it installed).
        ReflectTypes.install((Json.Object) Json.parse("{\"types\":" + TYPES_JSON + "}"), Map.of());
        UnsafeTable.install();
    }

    private VirtualFrame newFrame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0], ctx.frames.build());
    }

    /** Packs a reflect.Value for the D5 value {@code d5} of type {@code tok}. */
    private static GoStruct valueOf(Object d5, long tok) {
        long h = ReflectRefs.intern(d5);
        return ReflectValues.pack(ReflectTypes.valueDescriptor(), tok, h, 0);
    }

    /** Builds a {@code reflectop/convert} node over the given constants. */
    private GoExprNode convertNode(Object value, GoKind valueKind, long toTok) {
        constNodes.clear();
        constNodes.add(new ConstNode(value, valueKind));
        constNodes.add(new ConstNode(toTok, GoKind.INT64));
        String wire = "{\"kind\":\"reflectop\",\"pos\":" + POS + ",\"fields\":{\"op\":\"convert\","
                + "\"args\":[{\"kind\":\"testconst\",\"pos\":" + POS + ",\"fields\":{\"idx\":0}},"
                + "{\"kind\":\"testconst\",\"pos\":" + POS + ",\"fields\":{\"idx\":1}}]}}";
        return ReflectOpNode.build((Json.Object) Json.parse(wire), ctx, factory);
    }

    /** Builds a {@code reflectop} node for a raw op string (test scaffolding). */
    private GoExprNode rawOpNode(String op) {
        String wire = "{\"kind\":\"reflectop\",\"pos\":" + POS
                + ",\"fields\":{\"op\":\"" + op + "\",\"args\":[]}}";
        return ReflectOpNode.build((Json.Object) Json.parse(wire), ctx, factory);
    }

    private static String panicMessage(PanicException pe) {
        Object gv = pe.goValue();
        assertTrue(gv instanceof GoString,
                "panic value must be a D5 GoString (P3-M4), got: "
                        + (gv == null ? "null" : gv.getClass().getName()));
        return gv.toString();
    }

    /**
     * R2 positive: string -&gt; int is not a valid reflect conversion (gc
     * panics). The node must raise a Go panic carrying gc's VERBATIM
     * message (gc 1.27.1 reflect/value.go:
     * {@code "reflect.Value.Convert: value of type string cannot be
     * converted to type int"}) — never let the misuse exception escape as
     * an internal error (exit 1).
     *
     * <p>Non-tautological: fails if the translation is removed (the
     * {@code ReflectMisuseException} would escape and hit the
     * {@code IllegalStateException} catch-all below), and fails if the
     * message regresses from gc's text (exact equality, not
     * {@code contains}).
     */
    public void testUnsupportedConvertPanicsAsGoPanic() {
        newBuild();
        long strTok = ReflectTypes.table().tokForKind(GoKind.STRING);
        long intTok = ReflectTypes.table().tokForKind(GoKind.INT64);
        GoStruct strVal = valueOf(new GoString("s".getBytes(StandardCharsets.UTF_8)), strTok);
        GoExprNode node = convertNode(strVal, GoKind.INTERFACE, intTok);
        try {
            node.executeObject(newFrame());
            throw new AssertionError("unsupported reflect conversion must panic");
        } catch (PanicException pe) {
            String msg = panicMessage(pe);
            assertTrue(
                    msg.equals("reflect.Value.Convert: value of type string cannot be converted to type int"),
                    "panic must carry gc's verbatim Convert message, got: " + msg);
        } catch (IllegalStateException ise) {
            throw new AssertionError(
                    "R2: misuse exception escaped the reflect node boundary: " + ise.getMessage());
        }
    }

    /**
     * R2 negative: int -&gt; int64 is a valid conversion — no panic, and a
     * well-formed Value comes back.
     *
     * <p>Non-tautological: fails if the boundary over-translates (a valid
     * conversion raising any panic) or if the conversion corrupts the
     * value.
     */
    public void testSupportedConvertReturnsValue() {
        newBuild();
        long intTok = ReflectTypes.table().tokForKind(GoKind.INT64);
        GoStruct intVal = valueOf(42L, intTok);
        GoExprNode node = convertNode(intVal, GoKind.INTERFACE, intTok);
        Object out;
        try {
            out = node.executeObject(newFrame());
        } catch (PanicException pe) {
            throw new AssertionError("supported conversion must not panic, got: " + pe.goValue());
        }
        assertTrue(out instanceof GoStruct, "convert must return a Value struct");
        // The converted Value denotes the int64 42.
        Object d5 = ReflectRefs.denotedValue(ReflectValues.parts((GoStruct) out).ptr(), GoKind.INT64);
        assertTrue(d5 instanceof Long l && l == 42L, "converted value must denote 42, got: " + d5);
    }

    /**
     * F1 (REJECT remediation), inverted boundary: an unknown reflect op is
     * a frontend/backend contract violation (op strings are build-time
     * constants), NOT guest misuse. The plain {@code IllegalStateException}
     * must escape as an internal error — it must NOT become a Go panic,
     * and it must NOT be classified as {@link ReflectMisuseException}.
     *
     * <p>Non-tautological: fails if the catch is widened back to
     * {@code IllegalStateException} (the panic branch fires), and fails if
     * the site is ever reclassified as misuse (the misuse branch fires).
     */
    public void testUnknownOpEscapesAsInternalError() {
        newBuild();
        GoExprNode node = rawOpNode("bogus-op");
        try {
            node.executeObject(newFrame());
            throw new AssertionError("unknown reflect op must fail");
        } catch (PanicException pe) {
            throw new AssertionError(
                    "F1: internal contract violation must not become a Go panic, got: "
                            + panicMessage(pe));
        } catch (ReflectMisuseException rme) {
            throw new AssertionError(
                    "F1: unknown op is not guest misuse: " + rme.getMessage());
        } catch (IllegalStateException ise) {
            assertTrue(ise.getMessage() != null && ise.getMessage().contains("reflect: unknown op"),
                    "internal error must carry the unknown-op text, got: " + ise.getMessage());
        }
    }

    /**
     * F1 negative: a misshaped Value argument (a raw Long where the
     * frontend always builds a Value struct) breaches the backend's
     * argument contract. The plain {@code IllegalStateException} from
     * {@code asValue} must escape as an internal error — never a Go panic.
     *
     * <p>Non-tautological: fails if the boundary catches broad
     * {@code IllegalStateException} again (the panic branch fires).
     */
    public void testMisshapedValueArgEscapesAsInternalError() {
        newBuild();
        long intTok = ReflectTypes.table().tokForKind(GoKind.INT64);
        // Not a Value struct: the frontend/backend boxing contract breach.
        GoExprNode node = convertNode(42L, GoKind.INT64, intTok);
        try {
            node.executeObject(newFrame());
            throw new AssertionError("misshaped Value argument must fail");
        } catch (PanicException pe) {
            throw new AssertionError(
                    "F1: backend contract breach must not become a Go panic, got: "
                            + panicMessage(pe));
        } catch (ReflectMisuseException rme) {
            throw new AssertionError(
                    "F1: contract breach is not guest misuse: " + rme.getMessage());
        } catch (IllegalStateException ise) {
            assertTrue(ise.getMessage() != null && ise.getMessage().contains("reflect: expected Value"),
                    "internal error must carry the asValue text, got: " + ise.getMessage());
        }
    }

    /**
     * F1 negative: {@code sliceat} is an unimplemented backend feature
     * ("needs type synthesis"). A missing feature must surface as an
     * honest internal error — it must NEVER become a guest-recoverable Go
     * panic.
     *
     * <p>Non-tautological: fails if the site is ever reclassified as
     * misuse (either the panic or the misuse branch fires).
     */
    public void testSliceAtEscapesAsInternalError() {
        newBuild();
        GoExprNode node = rawOpNode("sliceat");
        try {
            node.executeObject(newFrame());
            throw new AssertionError("unimplemented sliceat must fail");
        } catch (PanicException pe) {
            throw new AssertionError(
                    "F1: unimplemented feature must not become a Go panic, got: "
                            + panicMessage(pe));
        } catch (ReflectMisuseException rme) {
            throw new AssertionError(
                    "F1: unimplemented feature is not guest misuse: " + rme.getMessage());
        } catch (IllegalStateException ise) {
            assertTrue(ise.getMessage() != null && ise.getMessage().contains("needs type synthesis"),
                    "internal error must carry the sliceat text, got: " + ise.getMessage());
        }
    }
}
