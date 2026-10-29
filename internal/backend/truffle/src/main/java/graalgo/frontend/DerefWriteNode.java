package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

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
        if (o instanceof java.util.concurrent.atomic.AtomicReference<?> ar) {
            // fix-redmaster-r39: writing through &x.v of an
            // atomic.Pointer/Value cell (FieldAddrNode yields the cell
            // itself — the DerefNode read path already handles this
            // shape) stores the boxed value in the cell, e.g.
            // StorePointer(&x.v, v) in (*atomic.Pointer[T]).Store.
            @SuppressWarnings("unchecked")
            java.util.concurrent.atomic.AtomicReference<Object> cell =
                    (java.util.concurrent.atomic.AtomicReference<Object>) ar;
            cell.set(Boxing.copyForAssign(elemKind, Boxing.box(elemKind, value, frame)));
            return;
        }
        GoPointer p;
        if (o == null) {
            // Java null is the nil *T (e.g., var p *int).
            Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
            return; // unreachable
        } else if (o instanceof GoPointer gp) {
            p = gp;
        } else {
            throw new FrontendException("deref write: not a pointer: " + o);
        }
        if (p.isNil()) {
            Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
        }
        Object b = Boxing.copyForAssign(elemKind, Boxing.box(elemKind, value, frame));
        if (elemKind == GoKind.INTERFACE && fillEfaceSlot(p, b)) {
            return;
        }
        if (writeOffsetZero(p, elemKind, b)) {
            return;
        }
        p.setReferent(b);
    }

    /**
     * fix-redmaster-r43: offset-0 write through a struct reached
     * via a storage cell or a plain struct pointer (the inverse of
     * DerefNode's descent): in gc flat memory a write through a
     * pointer to a struct's offset-0 field lands in that field,
     * not in the struct's first carrier word and not by replacing
     * the pointer's referent (which loses the write for the
     * temp pointers guest reflect builds per Field call). Returns
     * false when the referent is not a struct, the kind does not
     * match a field-0 chain, or the target is a struct (struct
     * stores keep the replacing store below).
     */
    private static boolean writeOffsetZero(GoPointer p, GoKind kind, Object b) {
        if (kind == GoKind.STRUCT) {
            return false;
        }
        Object unwrapped = GoPointer.unwrapStorage(p.referent());
        if (!(unwrapped instanceof GoStruct gs)) {
            return false;
        }
        GoStruct cur = gs;
        int depth = 0;
        while (true) {
            GoStructDescriptor d = cur.descriptor();
            if (d.fields().size() == 0 || depth++ > 64) {
                return false;
            }
            GoStructDescriptor.Field f0 = d.field(0);
            if (f0.kind() == GoKind.STRUCT) {
                Object next = cur.getObject(0);
                if (!(next instanceof GoStruct ns)) {
                    return false;
                }
                cur = ns;
                continue;
            }
            if (f0.kind() == kind) {
                if (f0.inLongs()) {
                    cur.setLong(0, Boxing.toLongBits(kind, b));
                } else {
                    cur.setObject(0, b);
                }
                return true;
            }
            // fix-redmaster-r43: a word store through a pointer
            // field (reflect Set's *(*unsafe.Pointer)(v.ptr) =
            // x.ptr for direct pointer values): unsafe.Pointer and
            // *T share the GoPointer representation, so a GoPointer
            // word lands in a *T field, and a Long word lands in a
            // word slot. Any other combination falls through (the
            // value cannot live in the field's carrier).
            if (f0.kind() == GoKind.POINTER && kind == GoKind.UNSAFE_POINTER
                    && b instanceof GoPointer) {
                cur.setObject(0, b);
                return true;
            }
            // fix-redmaster-r43: a handle-valued word store into
            // a *T field (reflect Set's x.ptr is a handle, not a
            // GoPointer): resolve the handle and store the pointer
            // it denotes; a zero word stores nil; an unresolvable
            // handle falls through (never a wrong field value).
            if (f0.kind() == GoKind.POINTER && kind == GoKind.UNSAFE_POINTER
                    && b instanceof Long l) {
                if (l.longValue() == 0) {
                    cur.setObject(0, null);
                    return true;
                }
                Object resolved = GoPointer.resolveHandle(l.longValue());
                if (resolved instanceof GoPointer gp) {
                    cur.setObject(0, gp);
                    return true;
                }
                // fix-redmaster-r43: a handle denotes its pointee
                // (not a pointer object), so a handle-valued *T
                // store lands as a fresh pointer to the denoted
                // value — a cell pointer for cell storage (the
                // write-through shape), a plain pointer otherwise.
                // An unresolvable handle falls through (never a
                // wrong field value).
                if (resolved != null) {
                    if (resolved instanceof graalgo.values.GoCell gc) {
                        cur.setObject(0, new GoPointer(gc));
                    } else {
                        cur.setObject(0, new GoPointer(resolved));
                    }
                    return true;
                }
            }
            if (f0.kind() == GoKind.UNSAFE_POINTER && kind == GoKind.POINTER
                    && b instanceof Number n) {
                cur.setLong(0, n.longValue());
                return true;
            }
            return false;
        }
    }

    /**
     * fix-redmaster-r43: an interface store through a pointer to a struct
     * whose first two fields are object-carried (the eface layout: typ,
     * data) fills those two fields instead of replacing the struct.
     *
     * <p>In gc memory is flat: {@code *(*any)(unsafe.Pointer(slot)) = val}
     * overwrites the two words at the slot's address with the interface's
     * (type, data) pair. The slot object itself survives — later code
     * reads {@code slot.typ} and writes {@code slot.val} as struct fields
     * (sync.Pool's poolDequeue pushHead/popTail do exactly this). The old
     * behavior replaced the struct with the GoInterface value, so every
     * later field access on the slot died casting GoInterface to GoStruct
     * (snapshot 46 section 6: popTail's {@code slot.val = nil} and
     * pushHead's {@code slot.typ} read).
     *
     * <p>Scoped to INTERFACE stores whose current referent is such a
     * struct; every other shape falls through to the replacing store.
     * The type word stored is the interface's descriptor object itself
     * (DerefNode's interface read maps it back; pool and atomic.Value
     * readers only nil-check and compare it, never dereference it as an
     * address). Field 0 is filled before field 1 so a concurrent reader
     * never observes a (null, data) pair, which is not a valid interface
     * value; the slot protocol (headTail gating) makes torn reads moot
     * in correct uses, matching gc's own two-word interface stores.
     */
    private static boolean fillEfaceSlot(GoPointer p, Object b) {
        if (b != null && !(b instanceof GoInterface)) {
            return false;
        }
        Object ref = p.referent();
        if (!(ref instanceof GoStruct s)) {
            return false;
        }
        GoStructDescriptor d = s.descriptor();
        if (d.fields().size() < 2 || d.field(0).inLongs() || d.field(1).inLongs()) {
            return false;
        }
        GoInterface gi = (GoInterface) b;
        s.setObject(0, gi == null ? null : gi.descriptor());
        s.setObject(1, gi == null ? null : gi.data());
        return true;
    }
}
