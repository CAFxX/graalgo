package graalgo.frontend;

import java.util.List;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * R-04: {@code &x.v} on a sync/atomic wrapper is an ordinary field
 * address. Wrappers materialise as plain {@link GoStruct}s (the
 * name-keyed cell lowering is deleted), so the real sync/atomic method
 * bodies address the word through a {@link GoPointer} over a
 * {@link GoFieldCell} — writes through that pointer update the field
 * itself (gc aliasing), and the atomicaddr wire node (not this node)
 * supplies atomicity. This replaces AtomicCellFieldAddrTest, which
 * asserted the deleted cell-transparent behaviour (PKG-053): a cell
 * base no longer exists, so there is nothing to yield through.
 */
public final class AtomicFieldAddrTest {

    /** A base node evaluating to a fixed value (frame is unused). */
    private static GoExprNode constBase(Object value, GoKind kind) {
        return new GoExprNode(kind) {
            @Override
            public Object executeObject(VirtualFrame frame) {
                return value;
            }
        };
    }

    private static GoStructDescriptor oneIntFieldDesc() {
        // INT64 is a longs-carried kind: inLongs=true, longs index 0.
        return new GoStructDescriptor("S", List.of(
                new GoStructDescriptor.Field("v", GoKind.INT64,
                        true, 0, 0, null, null, null)));
    }

    /**
     * {@code &s.v} on a struct value yields a non-nil cell
     * {@link GoPointer} — the shape the real (*atomic.Int64).Load body
     * relies on when it evaluates {@code LoadInt64(&x.v)} — whose
     * referent is the field's current value.
     */
    public void testFieldAddrOnStructYieldsFieldPointer() {
        GoStructDescriptor desc = oneIntFieldDesc();
        GoStruct s = new GoStruct(desc, new long[]{0}, new Object[0]);
        FieldAddrNode n = new FieldAddrNode(
                constBase(s, GoKind.STRUCT), new int[]{0}, GoKind.INT64);
        Object r = n.executeObject(null);
        if (!(r instanceof GoPointer gp) || gp.isNil()
                || !gp.isCellPointer()) {
            throw new AssertionError("FieldAddrNode on GoStruct must yield "
                    + "a non-nil cell GoPointer, got "
                    + (r == null ? "null" : r.toString()));
        }
        if (!Long.valueOf(0L).equals(gp.referent())) {
            throw new AssertionError("cell pointer referent must be the "
                    + "field's value 0, got " + gp.referent());
        }
    }

    /**
     * Writes through the {@code &s.v} pointer update the field itself
     * (aliasing, not a snapshot): the atomic word read after a
     * cell write is the written value.
     */
    public void testFieldAddrPointerWritesThroughToField() {
        GoStructDescriptor desc = oneIntFieldDesc();
        GoStruct s = new GoStruct(desc, new long[]{0}, new Object[0]);
        FieldAddrNode n = new FieldAddrNode(
                constBase(s, GoKind.STRUCT), new int[]{0}, GoKind.INT64);
        GoPointer p = (GoPointer) n.executeObject(null);
        p.setReferent(Long.valueOf(42L));
        if (s.getLong(0) != 42L) {
            throw new AssertionError("write through &s.v must update the "
                    + "field, got " + s.getLong(0));
        }
        if (!Long.valueOf(42L).equals(p.referent())) {
            throw new AssertionError("read through &s.v must see the "
                    + "written value, got " + p.referent());
        }
    }

    /**
     * {@code &x.v} where {@code x} is a {@code *atomic.Int64} (the
     * receiver inside the real method bodies): the base pointer is
     * dereferenced to the struct, which then takes the ordinary field
     * path — never a cell passthrough — and writes through it land
     * in the same struct.
     */
    public void testFieldAddrOnPointerToStructYieldsFieldPointer() {
        GoStructDescriptor desc = oneIntFieldDesc();
        GoStruct s = new GoStruct(desc, new long[]{0}, new Object[0]);
        GoPointer p = new GoPointer(s);
        FieldAddrNode n = new FieldAddrNode(
                constBase(p, GoKind.POINTER), new int[]{0}, GoKind.INT64);
        Object r = n.executeObject(null);
        if (!(r instanceof GoPointer gp) || gp.isNil()
                || !gp.isCellPointer()) {
            throw new AssertionError("FieldAddrNode on *GoStruct must yield "
                    + "a non-nil cell GoPointer, got "
                    + (r == null ? "null" : r.toString()));
        }
        gp.setReferent(Long.valueOf(7L));
        if (s.getLong(0) != 7L) {
            throw new AssertionError("write through &(*x).v must update "
                    + "the struct's field, got " + s.getLong(0));
        }
    }
}
