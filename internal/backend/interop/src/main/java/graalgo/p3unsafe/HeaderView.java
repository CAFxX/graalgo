package graalgo.p3unsafe;

import graalgo.values.GoCell;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Header views for {@code unsafe.Pointer -> *Header} reinterpretations
 * over string and slice values (fix-redmaster-r40).
 *
 * <p>In gc, a Go string value <em>is</em> its header
 * ({@code unsafeheader.String} / {@code reflect.StringHeader}:
 * {@code {Data, Len}}) and a slice value <em>is</em> its header
 * ({@code {Data, Len, Cap}}), so code like reflect's
 * {@code (*unsafeheader.String)(v.ptr).Len} (the {@code Len}/
 * {@code lenNonSlice} path for the String and Slice kinds) simply reads
 * words out of the value's own storage. In this backend's value model
 * the referent behind {@code v.ptr} is a {@link GoString} (resp.
 * {@link GoSlice}) object — there is no header struct in memory — so
 * the selector dereferenced the string itself and died with a
 * {@code ClassCastException} ({@code GoString} cast to
 * {@code GoStruct} in {@code SelectorNode.readFieldPath}), which is
 * what kept every corpus program at exit 1 after the r39 stack.
 *
 * <p>This class materializes the header struct from the value, at the
 * one consumption site that knows the target struct type
 * ({@link UnsafeToPtrNode}, which captures the header layout from the
 * wire by field <em>type</em> — a data word followed by one or two
 * integer words at word offsets 0/8/16 — never by struct or field
 * name; the same type-directed view pattern as the r38 interface
 * view in {@code graalgo.p3reflect.AbiTypes}):
 *
 * <ul>
 *   <li>{@code Data} is the value's established data handle
 *       ({@link UnsafeOps#stringData} / {@link UnsafeOps#sliceData}),
 *       so {@code unsafe.String}/{@code unsafe.Slice} over the header's
 *       Data word round-trips to the same bytes/elements gc would
 *       read. For a pointer-typed Data field the value is the
 *       corresponding {@link GoPointer} instead of the handle.</li>
 *   <li>{@code Len} (and {@code Cap} for slices) come from the value.
 *       A three-word header over a string value sets {@code Cap = Len}:
 *       gc reads out-of-header memory there (undefined), and Len is
 *       the least surprising defined value.</li>
 *   <li>A null referent yields the zero header: the storage behind a
 *       nil slice's reflect Value is a zero header in gc, so
 *       {@code Len()} on it is 0, not a nil dereference.</li>
 *   <li>Any other referent shape yields no view ({@code null}) and the
 *       caller keeps the exact pre-view behavior.</li>
 * </ul>
 *
 * <p>Deliberate V1 limit (the r38 interface view's N3 class): the view
 * is a <em>read snapshot</em>. Writes through the header pointer —
 * e.g. reflect {@code SetLen}'s {@code (*Slice)(v.ptr).Len = n} —
 * land in the snapshot, not in the source string/slice value. A
 * write-through view needs a derived-pointer kind that does not exist
 * yet.
 */
public final class HeaderView {
    private HeaderView() {}

    /**
     * Builds the header struct view of {@code referent} under the
     * descriptor {@code desc} (whose shape {@link UnsafeToPtrNode} has
     * already classified as a header layout), or {@code null} when the
     * referent is not a string/slice value (after unwrapping a
     * {@link GoCell}) and not null — the caller then falls back to the
     * pre-view behavior exactly.
     */
    public static GoStruct build(Object referent, GoStructDescriptor desc) {
        if (desc.fields().size() < 2 || desc.fields().size() > 3) {
            return null;
        }
        Object v = referent;
        if (v instanceof GoCell cell) {
            v = cell.get();
        }
        if (v instanceof UnsafeTable.ArrayRef ref) {
            // &arr[i] storage: the header is over the element's value.
            v = java.lang.reflect.Array.get(ref.array(), ref.offset());
            if (v instanceof GoCell cell) {
                v = cell.get();
            }
        }
        if (v == null) {
            // Zero header (see class javadoc): Data word 0, Len/Cap 0.
            return GoStruct.zero(desc);
        }
        long dataHandle;
        Object dataReferent;
        long len;
        long cap;
        if (v instanceof GoString gs) {
            dataHandle = UnsafeOps.stringData(gs);
            dataReferent = gs;
            len = gs.length();
            cap = len;
        } else if (v instanceof GoSlice gs) {
            dataHandle = UnsafeOps.sliceData(gs);
            dataReferent = new UnsafeTable.ArrayRef(gs.array(), gs.offset());
            len = gs.len();
            cap = gs.cap();
        } else {
            return null;
        }
        GoStruct view = GoStruct.zero(desc);
        GoStructDescriptor.Field dataField = desc.field(0);
        if (dataField.inLongs()) {
            // uintptr-carried Data word (reflect.StringHeader shape).
            view.setLong(0, dataHandle);
        } else if (dataField.kind() == GoKind.POINTER) {
            view.setObject(0, UnsafeTable.pointerFor(dataReferent));
        } else {
            // unsafe.Pointer-carried Data word: the handle as a Long
            // (the model's objects-carried unsafe.Pointer encoding,
            // cf. the interface view's DATA word in AbiTypes).
            view.setObject(0, Long.valueOf(dataHandle));
        }
        if (!desc.field(1).inLongs()) {
            return null;
        }
        view.setLong(1, len);
        if (desc.fields().size() == 3) {
            if (!desc.field(2).inLongs()) {
                return null;
            }
            view.setLong(2, cap);
        }
        return view;
    }
}
