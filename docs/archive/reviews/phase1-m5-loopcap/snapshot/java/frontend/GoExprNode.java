package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.source.SourceSection;
import graalgo.values.GoKind;

/**
 * Base class for expression nodes built by {@link NodeFactory}.
 *
 * <p>Every node carries its {@link GoKind} and implements the typed execute
 * methods for the carriers its kind uses (D5/D6: typed execute methods,
 * unboxed primitives, no PICs outside interface dispatch):
 * <ul>
 *   <li>int/uint kinds (8/16/32-bit) → {@link #executeInt}; the 64-bit
 *       kinds and untyped int → {@link #executeLong}.</li>
 *   <li>float32 → {@link #executeFloat}; float64 → {@link #executeDouble}.</li>
 *   <li>bool → {@link #executeBoolean}; string and reference kinds →
 *       {@link #executeObject}.</li>
 * </ul>
 * Numeric nodes implement <em>both</em> {@code executeInt} and
 * {@code executeLong} (with correct sign/zero extension), so parents and
 * shift-count readers need no wrapper nodes. Untyped-constant nodes
 * implement every method of their family (untyped transparency — see
 * {@link GoKind#transparent}).
 *
 * <p>Source sections: Truffle 25.x removed {@code Node.setSourceSection}; the
 * supported path is storing the section and overriding
 * {@link #getSourceSection()}.
 */
public abstract class GoExprNode extends Node {
    private SourceSection sourceSection;
    private final GoKind kind;
    /**
     * Frontend type-table id of this node's static type (-1 when built
     * synthetically rather than from a wire node). Set by
     * {@link NodeFactory#buildExpr} from the node's {@code "type"} field;
     * used for interface boxing and dynamic-type descriptors (M4, WS E).
     */
    private int typeId = -1;

    protected GoExprNode(GoKind kind) {
        this.kind = kind;
    }

    /** This node's Go kind (its static value type). */
    public final GoKind kind() {
        return kind;
    }

    /** Frontend type-table id of this node's static type. */
    public final int typeId() {
        return typeId;
    }

    /** Sets the type-table id (called by the factory for wire-built nodes). */
    public final void setTypeId(int typeId) {
        this.typeId = typeId;
    }

    /**
     * Exact decimal value of an untyped-constant subtree. The exporter folds
     * binary/paren constant expressions but not unary ones, so a
     * {@code UnaryOpNode} can sit atop an untyped constant (e.g.
     * {@code string(-(1<<70))}); conversions needing the exact value
     * (float rounding, rune range checks) call this instead of the
     * low-64-bit executes. Nodes outside untyped-constant subtrees throw.
     */
    public String exactDecimal() {
        throw new FrontendException(
                "exactDecimal() is only defined on untyped-constant subtrees, not on " + kind()
                        + " (WS D)");
    }

    public long executeLong(VirtualFrame frame) {
        throw new UnsupportedOperationException(
                getClass().getSimpleName() + " (kind " + kind + ") has no long value");
    }

    public int executeInt(VirtualFrame frame) {
        throw new UnsupportedOperationException(
                getClass().getSimpleName() + " (kind " + kind + ") has no int value");
    }

    public float executeFloat(VirtualFrame frame) {
        throw new UnsupportedOperationException(
                getClass().getSimpleName() + " (kind " + kind + ") has no float value");
    }

    public double executeDouble(VirtualFrame frame) {
        throw new UnsupportedOperationException(
                getClass().getSimpleName() + " (kind " + kind + ") has no double value");
    }

    public boolean executeBoolean(VirtualFrame frame) {
        throw new UnsupportedOperationException(
                getClass().getSimpleName() + " (kind " + kind + ") has no boolean value");
    }

    public Object executeObject(VirtualFrame frame) {
        throw new UnsupportedOperationException(
                getClass().getSimpleName() + " (kind " + kind + ") has no object value");
    }

    /** Sets this node's source section (replaces the removed Node API). */
    public final void setSourceSection(SourceSection section) {
        this.sourceSection = section;
    }

    @Override
    public SourceSection getSourceSection() {
        return sourceSection;
    }
}
