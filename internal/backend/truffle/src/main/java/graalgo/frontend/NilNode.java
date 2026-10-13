package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;
import graalgo.values.GoStructDescriptor;

/**
 * The {@code nil} identifier in expression position (M4, WS E): currently
 * only reachable as an expression-switch {@code case nil} (the exporter
 * types it with the switch tag's type). Evaluates to the kind's zero
 * value — the nil reference for every nilable kind.
 */
public final class NilNode extends GoExprNode {
    private final GoStructDescriptor structDesc;
    /**
     * Recursive zero template for STRUCT/ARRAY kinds (null otherwise):
     * the zero of a struct kind must be fully materialized — GoStruct.zero
     * would leave nested struct fields null (P3-M9).
     */
    private final Object zeroTemplate;

    public NilNode(GoKind kind, GoStructDescriptor structDesc) {
        this(kind, structDesc, null);
    }

    public NilNode(GoKind kind, GoStructDescriptor structDesc, Object zeroTemplate) {
        super(kind);
        this.structDesc = structDesc;
        this.zeroTemplate = zeroTemplate;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return zeroTemplate != null
                ? Boxing.freshZeroCopy(zeroTemplate)
                : Boxing.zero(kind(), structDesc);
    }
}
