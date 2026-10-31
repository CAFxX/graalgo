package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import graalgo.values.GoTypeDescriptor;

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
        // fix-redmaster-r43: offset-0 descent through a struct
        // reached via a storage cell (reflect.New cells) or any
        // plain struct pointer. In gc flat memory a pointer to a
        // struct and a pointer to its offset-0 field are the same
        // address, so a read through such a pointer descends field
        // 0s until the node's kind matches (a struct read returns
        // the struct itself). Unmatched shapes fall through to the
        // existing behavior below (loud failure downstream, never
        // a silent value); later branches must see the unwrapped
        // struct, not the cell, notably the INTERFACE eface
        // reconstruction.
        if (o != null) {
            Object unwrapped = GoPointer.unwrapStorage(o);
            if (unwrapped instanceof GoStruct gs) {
                o = unwrapped;
                Object descended = descendOffsetZero(gs, kind());
                if (descended != NOT_FOUND) {
                    return descended;
                }
            } else if (unwrapped != o) {
                // A cell holding a non-struct value: the value
                // itself is the dereference result.
                o = unwrapped;
            }
        }
        if (kind() == GoKind.UNSAFE_POINTER && o instanceof GoTypeDescriptor td) {
            // fix-redmaster-r43: a type word stored by DerefWriteNode's
            // eface fill (the interface's descriptor object) read as an
            // unsafe.Pointer — atomic.LoadPointer lowers to a Long read,
            // and Boxing.toLong requires a Number. The descriptor has no
            // address in this model, so synthesize a stable non-zero word
            // from its identity: non-zero (an occupied slot, not nil),
            // stable per descriptor (equal type words compare equal),
            // distinct across types with high probability. It is NOT
            // dereferenceable as an address — reflect reads types via
            // abi shells, never via eface typ words — and pool and
            // atomic.Value readers only nil-check and compare it, for
            // which the word is exact.
            return Long.valueOf((System.identityHashCode(td) & 0xFFFFFFFFL) | 1L);
        }
        // fix-redmaster-r43: a scalar dereference of array storage reads
        // the array's first element, recursively. In gc memory is flat:
        // a pointer to an array value and a pointer to its element 0
        // are the same address, so reflect's Value.Index (add(v.ptr, 0))
        // followed by Value.Int (*(*int)(p)) reads element 0 — and for a
        // nested array the recursively-first scalar. This backend
        // materializes array values as Java arrays, so a bare
        // array-root pointer dereferenced as a scalar must peel element
        // 0 until a scalar is reached; without the peel the whole array
        // object reaches Boxing.toLong and dies casting it to Number
        // (snapshot 36, fmt printing of array values). Scoped to scalar
        // kinds plus UNSAFE_POINTER: reflect's packEfaceData reads a
        // direct value through *(*unsafe.Pointer)(v.ptr), a word-sized
        // read whose value is element 0's word (an Index value packed
        // with Interface() dies in fmt's type-switch binding without
        // this). Aggregate dereferences (*p for array/struct pointers)
        // and **T dereferences (POINTER kind) still return the
        // referent unchanged.
        if ((kind().isIntKind() || kind().isFloatKind() || kind().isComplexKind()
                || kind() == GoKind.BOOL || kind() == GoKind.UNTYPED_BOOL
                || kind() == GoKind.UNSAFE_POINTER)
                && o != null && o.getClass().isArray()) {
            while (o != null && o.getClass().isArray()
                    && java.lang.reflect.Array.getLength(o) > 0) {
                o = java.lang.reflect.Array.get(o, 0);
            }
        }
        // fix-redmaster-r43: an interface read from an eface-layout struct
        // reconstructs the interface (the inverse of DerefWriteNode's
        // fill). In gc, *(*any)(unsafe.Pointer(slot)) reads the slot's
        // two words as (type, data); here the slot is a struct whose
        // fields 0/1 hold the descriptor object and the data object (or
        // nulls for an empty slot). Only a null or descriptor-valued
        // field 0 converts — a struct whose field 0 holds a real pointer
        // is a genuine struct value being reinterpreted, not an eface
        // slot, and falls through to the existing behavior.
        if (kind() == GoKind.INTERFACE && o instanceof GoStruct s) {
            GoStructDescriptor d = s.descriptor();
            if (d.fields().size() >= 2 && !d.field(0).inLongs() && !d.field(1).inLongs()) {
                Object typ = s.getObject(0);
                if (typ instanceof GoPointer gp && gp.referent() instanceof GoTypeDescriptor td) {
                    typ = td;
                }
                if (typ == null && s.getObject(1) == null) {
                    return GoInterface.NIL;
                }
                if (typ instanceof GoTypeDescriptor td) {
                    return new GoInterface(td, s.getObject(1), null);
                }
            }
        }
        if (o instanceof java.util.concurrent.atomic.AtomicReference<?> ar) {
            // R-04: dereferencing an atomic.Pointer/Value cell reads the
            // stored reference, e.g. LoadPointer(&x.v) in
            // (*atomic.Pointer[T]).Load. Cells no longer come from
            // FieldAddrNode (wrappers are plain GoStructs since R-04);
            // this branch serves cells produced through the retained
            // syncnew wire kind only.
            Object v = ar.get();
            if (v != null) {
                return v;
            }
            // fix-redmaster-r39: a zero cell holds Java null, which for
            // an unsafe.Pointer word is the handle 0, not a missing
            // value — fall through to the kind-based zero handling
            // below instead of returning null into Boxing.toLong.
            o = null;
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

    /** Sentinel for "no offset-0 field matches" (a null value is a legitimate result). */
    private static final Object NOT_FOUND = new Object();

    /**
     * Reads the value at byte offset 0 of struct gs as kind: the
     * struct itself for STRUCT reads, else the field-0 chain
     * (struct field 0s descended, then the first non-struct field
     * 0 read with the target kind's carrier decoding). Returns
     * {@link #NOT_FOUND} for a zero-sized struct field 0, a kind
     * mismatch, or a chain deeper than 64 (a value-struct cycle
     * is impossible in Go, so depth is only a hang guard).
     */
    private static Object descendOffsetZero(GoStruct gs, GoKind kind) {
        if (kind == GoKind.STRUCT) {
            return gs;
        }
        GoStruct cur = gs;
        int depth = 0;
        while (true) {
            GoStructDescriptor d = cur.descriptor();
            if (d.fields().size() == 0 || depth++ > 64) {
                return NOT_FOUND;
            }
            GoStructDescriptor.Field f0 = d.field(0);
            if (f0.kind() == GoKind.STRUCT) {
                Object next = cur.getObject(0);
                if (!(next instanceof GoStruct ns)) {
                    return NOT_FOUND;
                }
                cur = ns;
                continue;
            }
            if (f0.kind() == kind) {
                if (f0.inLongs()) {
                    return Boxing.fromLongBits(kind, cur.getLong(0));
                }
                return cur.getObject(0);
            }
            // fix-redmaster-r43: a word read through a pointer
            // field: unsafe.Pointer and *T share the GoPointer
            // representation, so *(*unsafe.Pointer)(p) where p
            // addresses a *T field reads that field. The reverse
            // (a word slot read as *T) is not matched: a longs
            // word is not a GoPointer, and returning it would be a
            // silent wrong value.
            if (f0.kind() == GoKind.POINTER && kind == GoKind.UNSAFE_POINTER) {
                return cur.getObject(0);
            }
            return NOT_FOUND;
        }
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
