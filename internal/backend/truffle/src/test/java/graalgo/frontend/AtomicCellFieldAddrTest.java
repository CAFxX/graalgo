package graalgo.frontend;

import java.util.List;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.runtime.sync.GoAtomicInt32;
import graalgo.runtime.sync.GoAtomicInt64;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Red-green for PKG-053: {@code &x.v} on an atomic wrapper cell must
 * yield the cell itself, not throw {@code ClassCastException}.
 *
 * <p>Root cause: the real {@code sync/atomic} method bodies (compiled
 * from disk since 503608ea) address the atomic word as {@code &x.v},
 * but atomic-typed values materialize as opaque cells
 * ({@link GoAtomicInt32}/{@link GoAtomicInt64} — never {@link GoStruct})
 * at several construction sites (struct fields, {@code new}, array
 * elements). {@link FieldAddrNode} unconditionally cast the base to
 * {@link GoStruct}, so every atomic wrapper method call on a
 * cell-typed value died with
 * {@code ClassCastException: GoAtomicInt32 cannot be cast to GoStruct}
 * (hit in the wild by {@code io.Pipe}'s {@code sync.Once.done} field
 * via {@code (*sync.Once).Do} → {@code (*atomic.Bool).Load}).
 *
 * <p>RED (pre-fix): the cell tests throw
 * {@code ClassCastException: class graalgo.runtime.sync.GoAtomicInt32
 * cannot be cast to class graalgo.values.GoStruct}.
 * GREEN (post-fix): the node returns the cell object itself — the
 * address of an atomic wrapper's value field IS the wrapper cell.
 */
public final class AtomicCellFieldAddrTest {

    /** A base node evaluating to a fixed value (frame is unused). */
    private static GoExprNode constBase(Object value, GoKind kind) {
        return new GoExprNode(kind) {
            @Override
            public Object executeObject(VirtualFrame frame) {
                return value;
            }
        };
    }

    private static void assertSame(Object want, Object got, String msg) {
        if (want != got) {
            throw new AssertionError(msg + " — want identical cell, got "
                    + (got == null ? "null" : got.getClass().getName()));
        }
    }

    /**
     * {@code &x.v} where {@code x} is a 32-bit atomic wrapper cell
     * (e.g. the {@code done} field of {@code sync.Once}, or a
     * {@code var u atomic.Uint32}): yields the cell itself.
     */
    public void testFieldAddrOnInt32CellYieldsCell() {
        GoAtomicInt32 cell = new GoAtomicInt32(7);
        FieldAddrNode n = new FieldAddrNode(
                constBase(cell, GoKind.STRUCT), new int[]{0}, GoKind.INT32);
        assertSame(cell, n.executeObject(null),
                "FieldAddrNode on GoAtomicInt32");
    }

    /**
     * Same for a 64-bit cell ({@code sync.WaitGroup.state} is
     * {@code atomic.Uint64}; {@code (*Uint64).Add} does
     * {@code AddUint64(&x.v, delta)}).
     */
    public void testFieldAddrOnInt64CellYieldsCell() {
        GoAtomicInt64 cell = new GoAtomicInt64(11L);
        FieldAddrNode n = new FieldAddrNode(
                constBase(cell, GoKind.STRUCT), new int[]{0}, GoKind.INT64);
        assertSame(cell, n.executeObject(null),
                "FieldAddrNode on GoAtomicInt64");
    }

    /**
     * {@code &x.v} where {@code x} is a {@code *atomic.Uint32} (the
     * receiver inside the real {@code (*Uint32).Load} body): the base
     * pointer is dereferenced to the cell, which is yielded.
     */
    public void testFieldAddrOnPointerToCellYieldsCell() {
        GoAtomicInt32 cell = new GoAtomicInt32(13);
        GoPointer p = new GoPointer(cell);
        FieldAddrNode n = new FieldAddrNode(
                constBase(p, GoKind.POINTER), new int[]{0}, GoKind.INT32);
        assertSame(cell, n.executeObject(null),
                "FieldAddrNode on *GoAtomicInt32");
    }

    /**
     * Control: an ordinary struct base keeps the old behavior — the
     * node yields a {@link GoPointer} to the field's storage (so
     * {@code *p = v} writes the field itself). Guards the fix against
     * over-triggering on non-cell bases.
     */
    public void testFieldAddrOnStructStillYieldsFieldPointer() {
        GoStructDescriptor desc = new GoStructDescriptor("S", List.of(
                new GoStructDescriptor.Field("v", GoKind.INT32,
                        false, 0, 0, null, null, null, false)));
        GoStruct s = new GoStruct(desc, new long[0], new Object[]{null});
        FieldAddrNode n = new FieldAddrNode(
                constBase(s, GoKind.STRUCT), new int[]{0}, GoKind.INT32);
        Object r = n.executeObject(null);
        if (!(r instanceof GoPointer gp) || gp.isNil()) {
            throw new AssertionError("FieldAddrNode on GoStruct must yield "
                    + "a non-nil GoPointer, got "
                    + (r == null ? "null" : r.getClass().getName()));
        }
    }
}
