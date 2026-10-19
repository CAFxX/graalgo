package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;

/**
 * Assignment through a pointer dereference: {@code *p = v} (P3-M9, WS D).
 *
 * <p>Evaluates the pointer expression; a nil pointer raises gc's
 * nil-dereference panic. Otherwise sets the pointer's referent to the
 * boxed value (via Boxing.box + copyForAssign, matching the value-copy
 * rule used by SelectorWriteNode).
 */
public final class DerefWriteNode extends GoStmtNode {
    @Child private GoExprNode ptr;
    @Child private GoExprNode value;
    private final GoKind elemKind;

    public DerefWriteNode(GoExprNode ptr, GoExprNode value, GoKind elemKind) {
        this.ptr = ptr;
        this.value = value;
        this.elemKind = elemKind;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        Object o = ptr.executeObject(frame);
        GoPointer p;
        if (o == null) {
            // Java null is the nil *T (e.g., var p *int).
            Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
            return; // unreachable
        } else if (o instanceof GoPointer gp) {
            p = gp;
        } else if (o instanceof java.util.concurrent.atomic.AtomicReference<?> ar) {
            // fix-redmaster-r12: *addr = val where addr is &x.v of an
            // atomic.Pointer/Value (FieldAddrNode's cell transparency yields
            // the cell itself, e.g. StorePointer(&x.v, ...) in the generic
            // (*Pointer[T]).Store body). Write the cell, mirroring
            // DerefNode's read path for the same shape.
            @SuppressWarnings("unchecked")
            java.util.concurrent.atomic.AtomicReference<Object> cell =
                    (java.util.concurrent.atomic.AtomicReference<Object>) ar;
            cell.set(Boxing.copyForAssign(elemKind, Boxing.box(elemKind, value, frame)));
            return;
        } else {
            throw new FrontendException("deref write: not a pointer: " + o);
        }
        if (p.isNil()) {
            Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
        }
        Object b = Boxing.copyForAssign(elemKind, Boxing.box(elemKind, value, frame));
        p.setReferent(b);
    }
}
