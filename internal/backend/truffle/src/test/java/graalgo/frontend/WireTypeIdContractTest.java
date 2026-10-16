package graalgo.frontend;

import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.source.Source;
import graalgo.values.GoKind;

/**
 * Wire-contract regression tests for the {@code "type"} field on the
 * {@code addrof} and {@code sliceoverlap} wire nodes (P1-2 follow-up,
 * 2026-10-16).
 *
 * <p>Design decision (pinned here and in the commit message): the
 * {@code "type"} field is NOT dead wire weight. The per-kind builders
 * ({@code buildAddrOf}, {@code buildSliceOverlap}) don't read it, but the
 * central {@link NodeFactory#buildExpr} dispatcher stamps every expression
 * node's {@code fields.type} via {@code setTypeId} — interface boxing and
 * dynamic-type descriptors need the static type-table id. These tests pin
 * the builder side of the contract: a node built from wire carrying
 * {@code "type"} reports it via {@code typeId()}, with the result kind the
 * builder assigns (POINTER for {@code addrof}, BOOL for
 * {@code sliceoverlap}).
 *
 * <p>Red-green: the {@code *MissingTypeId} tests prove the suite observes
 * the field — wire without {@code "type"} leaves {@code typeId()} at its
 * -1 default, so silently dropping the emission (or the central stamp)
 * fails these tests.
 *
 * <p>Operands are constant literals: the tests build but never execute the
 * nodes, so no frame or runtime state is needed beyond a minimal type
 * table (0=int, 1=bool, 2=*int).
 */
public final class WireTypeIdContractTest {

    private static final String TYPES_JSON = "["
            + "{\"kind\":\"basic\",\"detail\":{\"name\":\"int\",\"class\":\"int\",\"untyped\":false}},"
            + "{\"kind\":\"basic\",\"detail\":{\"name\":\"bool\",\"class\":\"bool\",\"untyped\":false}},"
            + "{\"kind\":\"pointer\",\"detail\":{\"elem\":0,\"methods\":[]}}"
            + "]";

    private static final String POS_JSON =
            "{\"file\":0,\"line\":1,\"col\":1,\"len\":1}";

    private static NodeFactory newFactory() {
        Source source = Source.newBuilder("go", "package main", "wire.go").build();
        return new NodeFactory(source);
    }

    private static NodeFactory.BuildCtx newCtx(Source source) {
        TypeTable types = new TypeTable((Json.Array) Json.parse(TYPES_JSON));
        return new NodeFactory.BuildCtx(
                source, FrameDescriptor.newBuilder(), types, (Json.Array) Json.parse("[]"));
    }

    private static GoExprNode buildExpr(String nodeJson) {
        Source source = Source.newBuilder("go", "package main", "wire.go").build();
        NodeFactory factory = new NodeFactory(source);
        Json.Object node = (Json.Object) Json.parse(nodeJson);
        return factory.buildExpr(node, newCtx(source));
    }

    private static String basiclitJson(int typeId, String value, String constant) {
        return "{\"kind\":\"basiclit\",\"pos\":" + POS_JSON
                + ",\"fields\":{\"value\":\"" + value + "\",\"const\":\"" + constant
                + "\",\"type\":" + typeId + "}}";
    }

    public void testAddrofXSchemaStampsTypeId() {
        String operand = basiclitJson(0, "42", "42");
        String nodeJson = "{\"kind\":\"addrof\",\"pos\":" + POS_JSON
                + ",\"fields\":{\"x\":" + operand + ",\"type\":2}}";
        GoExprNode n = buildExpr(nodeJson);
        if (n.kind() != GoKind.POINTER) {
            throw new AssertionError("addrof kind = " + n.kind() + ", want POINTER");
        }
        if (n.typeId() != 2) {
            throw new AssertionError("addrof typeId = " + n.typeId() + ", want 2 (*int)");
        }
    }

    public void testAddrofArrayIndexSchemaStampsTypeId() {
        String arr = basiclitJson(0, "0", "0");
        String idx = basiclitJson(0, "1", "1");
        String nodeJson = "{\"kind\":\"addrof\",\"pos\":" + POS_JSON
                + ",\"fields\":{\"array\":" + arr + ",\"index\":" + idx + ",\"type\":2}}";
        GoExprNode n = buildExpr(nodeJson);
        if (n.kind() != GoKind.POINTER) {
            throw new AssertionError("addrof[array/index] kind = " + n.kind() + ", want POINTER");
        }
        if (n.typeId() != 2) {
            throw new AssertionError("addrof[array/index] typeId = " + n.typeId() + ", want 2 (*int)");
        }
    }

    public void testSliceOverlapStampsTypeId() {
        String x = basiclitJson(0, "1", "1");
        String y = basiclitJson(0, "2", "2");
        String nodeJson = "{\"kind\":\"sliceoverlap\",\"pos\":" + POS_JSON
                + ",\"fields\":{\"x\":" + x + ",\"y\":" + y + ",\"inexact\":true,\"type\":1}}";
        GoExprNode n = buildExpr(nodeJson);
        if (n.kind() != GoKind.BOOL) {
            throw new AssertionError("sliceoverlap kind = " + n.kind() + ", want BOOL");
        }
        if (n.typeId() != 1) {
            throw new AssertionError("sliceoverlap typeId = " + n.typeId() + ", want 1 (bool)");
        }
        if (!(n instanceof SliceOverlapNode)) {
            throw new AssertionError("sliceoverlap built " + n.getClass().getSimpleName()
                    + ", want SliceOverlapNode");
        }
    }

    public void testAddrofMissingTypeIdLeavesDefault() {
        String operand = basiclitJson(0, "42", "42");
        String nodeJson = "{\"kind\":\"addrof\",\"pos\":" + POS_JSON
                + ",\"fields\":{\"x\":" + operand + "}}";
        GoExprNode n = buildExpr(nodeJson);
        if (n.typeId() != -1) {
            throw new AssertionError("addrof without fields.type: typeId = " + n.typeId()
                    + ", want -1 (unset default)");
        }
    }

    public void testSliceOverlapMissingTypeIdLeavesDefault() {
        String x = basiclitJson(0, "1", "1");
        String y = basiclitJson(0, "2", "2");
        String nodeJson = "{\"kind\":\"sliceoverlap\",\"pos\":" + POS_JSON
                + ",\"fields\":{\"x\":" + x + ",\"y\":" + y + ",\"inexact\":false}}";
        GoExprNode n = buildExpr(nodeJson);
        if (n.typeId() != -1) {
            throw new AssertionError("sliceoverlap without fields.type: typeId = " + n.typeId()
                    + ", want -1 (unset default)");
        }
    }
}
