package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;

/**
 * Pointer dereference read {@code *p} (P3-M9, WS D).
 *
 * <p>Evaluates the pointer expression; a nil pointer raises gc's
 * nil-dereference panic. Otherwise returns the referent, following the
 * SelectorNode read pattern (Long-carried values decoded via
 * Boxing.fromLongBits; executeLong via Boxing.toLong for correct
 * unsigned extension).
 */
public final class DerefNode extends GoExprNode {
    @Child private GoExprNode ptr;

    public DerefNode(GoKind kind, GoExprNode ptr) {
        super(kind);
        this.ptr = ptr;
    }

    private Object read(VirtualFrame frame) {
        Object o = ptr.executeObject(frame);
        if (o == null) {
            // Java null is the nil *T (e.g., var p *int).
            Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
        }
        if (o instanceof GoPointer p) {
            if (p.isNil()) {
                Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
            }
            o = p.referent();
        }
        if (o instanceof java.util.concurrent.atomic.AtomicReference<?> ar) {
            // PKG-053: dereferencing an atomic.Pointer/Value cell (&x.v
            // yields the cell itself — see FieldAddrNode) reads the
            // stored reference, e.g. LoadPointer(&x.v) in
            // (*atomic.Pointer[T]).Load.
            return ar.get();
        }
        if (o instanceof Long l) {
            // Long-carried value: decode per the node's kind.
            return Boxing.fromLongBits(kind(), l.longValue());
        }
        if (o == null && kind() == GoKind.UNSAFE_POINTER) {
            // Nil unsafe.Pointer referent: the zero value is address 0,
            // not Java null (cf. SelectorNode.read).
            return Long.valueOf(0);
        }
        return o;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        // P3-M9: use Boxing.toLong for correct zero-extension of unsigned
        // kinds (uint32(0xFFFFFFFF) -> 4294967295L, not -1L).
        return Boxing.toLong(read(frame), kind());
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return ((Number) read(frame)).intValue();
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        return ((Float) read(frame)).floatValue();
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        return ((Double) read(frame)).doubleValue();
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return ((Boolean) read(frame)).booleanValue();
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return read(frame);
    }
}
