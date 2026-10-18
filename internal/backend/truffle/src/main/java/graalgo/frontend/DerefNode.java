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
    /**
     * Struct descriptor for {@code *S} dereference when the referent is a
     * GoInterface (unsafe reinterpretation pattern). Null for non-struct
     * kinds. Used to synthesize the struct from the interface's
     * (descriptor, data) when Go code does
     * {@code *(*S)(unsafe.Pointer(&iface))}.
     */
    private final graalgo.values.GoStructDescriptor structDesc;

    public DerefNode(GoKind kind, GoExprNode ptr) {
        this(kind, ptr, null);
    }

    public DerefNode(GoKind kind, GoExprNode ptr, graalgo.values.GoStructDescriptor structDesc) {
        super(kind);
        this.ptr = ptr;
        this.structDesc = structDesc;
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
        if (o instanceof graalgo.values.GoInterface gi && structDesc != null) {
            // Unsafe reinterpretation: *(*S)(unsafe.Pointer(&iface)).
            // The GoInterface's (descriptor, data) pair is the memory
            // representation that S mirrors (e.g. abi.EmptyInterface
            // mirrors the any layout). Synthesize the struct.
            return synthesizeStructFromInterface(gi);
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

    /**
     * Synthesizes a struct value from a GoInterface's (descriptor, data).
     * Used for the unsafe reinterpretation pattern
     * {@code *(*S)(unsafe.Pointer(&iface))} where S is a struct mirroring
     * the interface memory layout (e.g. {@code abi.EmptyInterface} mirrors
     * {@code any}: field 0 is the type, field 1 is the data).
     *
     * <p>Currently supports 2-field structs where field 0 receives the
     * descriptor and field 1 receives the data. Other shapes throw
     * FrontendException (unsupported reinterpretation).
     *
     * <p>Values are placed per the field's GoKind: a POINTER-kind field
     * holds a {@code *T}-valued {@link GoPointer} (null for nil), never the
     * bare referent — in Go's type system the interface's descriptor word
     * <em>is</em> a {@code *Type}, so the synthesized field must be
     * pointer-valued for downstream {@code *T} operations (deref,
     * {@code unsafe.Pointer}, {@code ==}) to see a well-typed value.
     */
    static graalgo.values.GoStruct synthesizeStructFromInterface(
            graalgo.values.GoStructDescriptor desc, graalgo.values.GoInterface gi) {
        var fields = desc.fields();
        if (fields.size() != 2) {
            throw new FrontendException("deref: cannot reinterpret GoInterface as struct " +
                desc.typeName() + " with " + fields.size() + " fields (expected 2)");
        }
        long[] longs = new long[desc.longsCount()];
        Object[] objects = new Object[desc.objectsCount()];
        // Field 0 <- descriptor, field 1 <- data.
        for (int i = 0; i < 2; i++) {
            var f = fields.get(i);
            Object v = (i == 0) ? gi.descriptor() : gi.data();
            if (f.kind() == GoKind.POINTER && v != null && !(v instanceof GoPointer)) {
                // *T-typed field: wrap the referent so the field holds a
                // genuine *T value. Null stays null (nil *T); an existing
                // GoPointer is already well-typed.
                v = new GoPointer(v);
            }
            if (f.inLongs()) {
                // Longs-carried field: encode the value as long bits.
                if (v instanceof Long l) {
                    longs[f.index()] = l.longValue();
                } else if (v == null) {
                    longs[f.index()] = 0L;
                } else {
                    throw new FrontendException("deref: cannot map " +
                        v.getClass().getSimpleName() + " to longs-carried field " + f.name());
                }
            } else {
                objects[f.index()] = v;
            }
        }
        return new graalgo.values.GoStruct(desc, longs, objects);
    }

    private Object synthesizeStructFromInterface(graalgo.values.GoInterface gi) {
        return synthesizeStructFromInterface(structDesc, gi);
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
