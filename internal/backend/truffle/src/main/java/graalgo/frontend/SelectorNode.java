package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.AbiTypeRef;
import graalgo.values.EfaceRef;

/**
 * Field selector {@code x.f} (M4, WS E).
 *
 * <p>{@code selpath} is go/types' field-index path through (possibly
 * embedded) structs, so no promotion logic lives here. Pointers
 * auto-deref at every level (a nil pointer raises gc's nil-dereference panic).
 * The node's kind is the field's kind; struct-typed fields read by
 * reference (writes through them mutate the owning struct in place —
 * sound because every struct assignment deep-copies).
 */
public final class SelectorNode extends GoExprNode {
    @Child private GoExprNode obj;
    private final int[] selpath;

    public SelectorNode(GoExprNode obj, int[] selpath, GoKind fieldKind) {
        super(fieldKind);
        this.obj = obj;
        this.selpath = selpath;
    }

    public GoExprNode obj() {
        return obj;
    }

    public int[] selpath() {
        return selpath;
    }

    /**
     * Navigates a field-index path from a runtime value (P3-M3, WS D):
     * promoted-method receiver adaptation in interface dispatch walks the
     * exporter's {@code "index"} path exactly like a field selection.
     * Empty path returns the value unchanged (up to {@code derefResult}).
     *
     * @param derefResult when true, dereference the final value once more
     *        (nil → gc's nil-dereference panic): a value-receiver method
     *        promoted via a pointer-embedded field receives the pointed-to
     *        struct, mirroring gc's implicit {@code (*p)}.
     */
    public static Object readFieldPath(Object o, int[] selpath, boolean derefResult) {
        return readFieldPath(o, selpath, derefResult, null);
    }

    /**
     * Site-carrying overload (P3-M4 remediation P1-2): nil-dereference
     * panics while walking the path are attributed to {@code site}.
     */
    public static Object readFieldPath(Object o, int[] selpath, boolean derefResult, Node site) {
        for (int idx : selpath) {
            o = deref(o, site);
            if (o instanceof EfaceRef er) {
                // P3-M10: *(*abi.EmptyInterface)(unsafe.Pointer(&iface)) —
                // the synthesized eface view (see UnsafeToPtrNode). Field
                // 0 is Type (*abi.Type), field 1 is Data (unsafe.Pointer).
                // Checked after deref like AbiTypeRef: the view is usually
                // behind the *abi.EmptyInterface GoPointer (p.Type must
                // work as well as (*p).Type).
                o = er.field(idx);
                continue;
            }
            if (o instanceof AbiTypeRef ar) {
                // P3-M10: field selection on a *abi.Type (or *abi.PtrType,
                // ...) produced by unsafe reinterpretation. The fields are
                // precomputed from the type table at ref creation. (Checked
                // after deref: the ref is usually behind a GoPointer.)
                o = ar.field(idx);
                continue;
            }
            if (o == null) {
                // A typed-nil interface's data (or any other null) reached
                // a struct field selection: gc's nil-dereference panic,
                // never a Java NPE.
                Panic.throwPanic(GoErrors.nilDereference(), null, null, site);
            }
            GoStruct s = (GoStruct) o;
            if (s.descriptor().field(idx).inLongs()) {
                o = Long.valueOf(s.getLong(idx));
            } else {
                o = s.getObject(idx);
            }
        }
        return derefResult ? deref(o, site) : o;
    }

    static Object deref(Object o, Node site) {
        if (o == null) {
            // Nil pointer (Java null): gc's nil-dereference panic.
            Panic.throwPanic(GoErrors.nilDereference(), null, null, site);
        }
        if (o instanceof GoPointer p) {
            if (p.isNil()) {
                Panic.throwPanic(GoErrors.nilDereference(), null, null, site);
            }
            return p.referent();
        }
        if (o instanceof GoInterface gi) {
            // PKG-074: a GoInterface wrapping a struct (e.g. from real
            // reflect or interface-typed values) must be unwrapped to the
            // underlying data before field selection. The data is the
            // dynamic value in its D5 Java carrier.
            if (gi.isNil()) {
                Panic.throwPanic(GoErrors.nilDereference(), null, null, site);
            }
            return gi.data();
        }
        return o;
    }

    private Object read(VirtualFrame frame) {
        // P3-M4 remediation (P1-2): site-carrying path — a nil pointer in
        // the selection chain panics at this selector's line.
        Object o = readFieldPath(obj.executeObject(frame), selpath, false, this);
        if (o instanceof Long l) {
            // Longs-carried field: decode per the node's kind.
            return Boxing.fromLongBits(kind(), l.longValue());
        }
        if (o == null && kind() == graalgo.values.GoKind.UNSAFE_POINTER) {
            // Nil unsafe.Pointer field: the zero value is address 0, not
            // Java null. (UNSAFE_POINTER has Long slot kind but struct
            // fields live in the objects array; a nil field reads as null
            // and must decode to 0L for executeLong/executeInt callers.)
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
