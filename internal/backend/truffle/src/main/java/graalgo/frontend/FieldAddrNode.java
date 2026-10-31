package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;

/**
 * Address of a struct field: {@code &s.f} (P3-M9, WS D), including
 * promoted-field chains ({@code &s.a.b.f}).
 *
 * <p>The base expression evaluates to the outermost struct value (a
 * {@link GoStruct}, or a {@link GoPointer} to one, which is auto-derefed
 * — a nil pointer is gc's nil-dereference panic). The flattened
 * field-index path then walks nested struct fields to the field's owner;
 * the returned pointer addresses a {@link GoFieldCell} over that owner
 * and field, so {@code *p = v} writes the field itself (gc aliasing)
 * rather than a snapshot of its current value.
 */
public final class FieldAddrNode extends GoExprNode {
    @Child private GoExprNode base;
    private final int[] hops; // field indices from the base struct to the field
    private final GoKind fieldKind;

    public FieldAddrNode(GoExprNode base, int[] hops, GoKind fieldKind) {
        super(GoKind.POINTER);
        this.base = base;
        this.hops = hops;
        this.fieldKind = fieldKind;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object b = base.executeObject(frame);
        b = unwrap(b);
        // R-04: no cell transparency remains. sync/atomic wrappers are
        // plain GoStructs, so &x.v (as addressed by the real sync/atomic
        // method bodies) is an ordinary field address: a GoPointer over
        // a GoFieldCell. Atomicity comes from the atomicaddr wire node,
        // which wraps that pointer in a delegating cell for AtomicOpNode
        // — the address itself carries no special case.
        GoStruct cur = (GoStruct) b;
        for (int i = 0; i < hops.length - 1; i++) {
            Object next = cur.getObject(hops[i]);
            next = unwrap(next);
            cur = (GoStruct) next;
        }
        return new GoPointer(new GoFieldCell(cur, hops[hops.length - 1], fieldKind));
    }

    /**
     * PKG-074: unwrap GoPointer (auto-deref) and GoInterface (extract
     * underlying data) before struct field navigation. A GoInterface
     * wrapping a struct must be unwrapped to the data, otherwise the
     * GoStruct cast throws ClassCastException.
     */
    private Object unwrap(Object o) {
        if (o instanceof GoPointer gp) {
            if (gp.isNil()) {
                Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
            }
            return gp.referent();
        }
        if (o instanceof GoInterface gi) {
            if (gi.isNil()) {
                Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
            }
            return gi.data();
        }
        return o;
    }

}
