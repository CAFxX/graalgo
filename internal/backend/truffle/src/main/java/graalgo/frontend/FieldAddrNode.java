package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
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
        if (b instanceof GoPointer gp) {
            if (gp.isNil()) {
                Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
            }
            b = gp.referent();
        }
        GoStruct cur = (GoStruct) b;
        for (int i = 0; i < hops.length - 1; i++) {
            Object next = cur.getObject(hops[i]);
            if (next instanceof GoPointer gp) {
                // Promoted field through an embedded *struct field.
                if (gp.isNil()) {
                    Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
                }
                next = gp.referent();
            }
            cur = (GoStruct) next;
        }
        return new GoPointer(new GoFieldCell(cur, hops[hops.length - 1], fieldKind));
    }
}
