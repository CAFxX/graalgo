package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

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
        return readFieldPath(o, selpath, derefResult, site, false);
    }

    /**
     * Scalar-result overload (fix-redmaster-r40): when the compiled
     * read's final step expects a longs-carried scalar (int / uint /
     * float / bool), the final step additionally applies the
     * prefix-mismatch descent in {@link #readFieldEmbedded}: a value
     * whose runtime struct is a WIDER type than the static type the
     * path was compiled against (an abi kind shell or {@code rtype},
     * with the static type embedded at field 0) answers a scalar field
     * read from its embedded prefix, exactly as gc's flat layout does
     * (the prefix type's field sits at the same index inside the
     * header at offset 0). Non-final steps and non-scalar reads keep
     * the exact prior behavior.
     */
    public static Object readFieldPath(Object o, int[] selpath, boolean derefResult, Node site,
            boolean scalarResult) {
        for (int i = 0; i < selpath.length; i++) {
            o = deref(o, site);
            GoStruct s = (GoStruct) o;
            o = readFieldEmbedded(s, selpath[i],
                    scalarResult && i == selpath.length - 1);
        }
        return derefResult ? deref(o, site) : o;
    }

    /**
     * Reads field {@code idx} from struct {@code s}, with embedding-prefix
     * descent (r37, tightened r38).
     *
     * <p>In gc, embedding is a prefix: a pointer to a struct embedding
     * {@code T} reads as a pointer to {@code T}, so a method compiled
     * for receiver type {@code T} (direct field indices into {@code T})
     * runs unchanged on a value of a type embedding {@code T}. In
     * graalgo's nested-struct model the embedding struct is a separate
     * object, so a direct index into {@code T} is out of range for the
     * embedding struct's descriptor — e.g. {@code abi.Type.Kind} (field
     * 6 of {@code Type}) called on a {@code PtrType} value (fields
     * {@code Type}, {@code Elem}) via a {@code *Type}-typed pointer,
     * which is how every {@code reflect} {@code Kind}/{@code Elem} call
     * reaches the abi structs. When the index is out of range, this
     * descends into the first field (the embedding prefix) and reads
     * from there; when no such field exists it preserves the original
     * out-of-range failure. In-range reads are untouched, so there is
     * no hot-path cost. Only the first field is descended into (r38
     * tightening of r37's first-fitting-field rule): a valid gc
     * reinterpretation cast requires the embedded type at byte offset 0,
     * which is the first field in declaration order (modulo zero-size
     * fields); descending into a later struct field would read a
     * non-prefix field as the embedded type, which gc never does for
     * a valid cast. Only value-embedded (struct-kind) fields are
     * descended into; pointer-embedded fields keep the prior behavior.
     *
     * <p>The descent uses the live nested value's own descriptor, not
     * the parent descriptor's {@code structDesc} metadata (r38 fix for
     * r37's descent never firing): descriptors synthesized by
     * {@code ReflectTypes.structDescriptorFor} — exactly the abi shells
     * this descent exists for — are built with the legacy layout
     * overload, which leaves every {@code structDesc} null, so a
     * metadata-based descent could never fire on them (diagnosed:
     * {@code PtrType} field 0 {@code Type:STRUCT:structDesc=null}).
     * The nested value's descriptor is authoritative for that value,
     * so this also covers ordinary frontend structs.
     */
    private static Object readFieldEmbedded(GoStruct s, int idx) {
        return readFieldEmbedded(s, idx, false);
    }

    /**
     * Field read with embedding-prefix descent. The {@code scalarResult}
     * variant (fix-redmaster-r40) adds one trigger to the r37/r38
     * descent: the index is IN range for the wider runtime struct, but
     * the field there is object-carried while the compiled read expects
     * a longs-carried scalar — the direct field is the embedded prefix
     * struct itself (or another wider-type field), not the scalar the
     * static type has at this index. The read then descends into field
     * 0 and retries the same index, but only when the prefix's own
     * field at that index IS longs-carried (kind evidence from the live
     * descriptor, so a coincidental object field never reinterprets).
     * When no such prefix exists the direct read is preserved.
     *
     * <p>N2 position (r38 review): the descent still trusts field-0
     * position, as does the whole prefix model — the frontend emits
     * struct fields in declaration order and gc places an embedded
     * value type at byte offset 0, so field 0 is the only possible
     * prefix position; the guards here (struct-kind field 0 only, live
     * descriptor coverage, and now scalar kind evidence for the new
     * trigger) are the checks the model supports. A descriptor-level
     * embedded flag does not exist in the wire format; adding one is a
     * frontend change outside this lane's evidence.
     */
    private static Object readFieldEmbedded(GoStruct s, int idx, boolean scalarResult) {
        for (int depth = 0; depth < 32; depth++) {
            GoStructDescriptor desc = s.descriptor();
            if (idx < desc.fields().size()) {
                if (scalarResult && !desc.field(idx).inLongs()) {
                    GoStruct next = prefixCandidate(s, idx, true);
                    if (next != null) {
                        s = next;
                        continue;
                    }
                }
                if (desc.field(idx).inLongs()) {
                    return Long.valueOf(s.getLong(idx));
                }
                return s.getObject(idx);
            }
            GoStruct next = prefixCandidate(s, idx, false);
            if (next == null) {
                // No embedding descent (or the candidate field is null):
                // preserve the original out-of-range failure.
                desc.field(idx);
            }
            s = next;
        }
        throw new IllegalStateException("embedding descent too deep");
    }

    /**
     * The embedding-prefix candidate for {@code idx}: field 0's live
     * struct value when field 0 is a value-embedded (struct-kind) field
     * whose descriptor covers {@code idx} — and, when
     * {@code needScalar}, whose field at {@code idx} is longs-carried.
     * Null when there is no valid prefix.
     */
    private static GoStruct prefixCandidate(GoStruct s, int idx, boolean needScalar) {
        GoStructDescriptor desc = s.descriptor();
        if (desc.fields().isEmpty()
                || desc.field(0).kind() != GoKind.STRUCT) {
            return null;
        }
        Object v = s.getObject(0);
        if (!(v instanceof GoStruct gs)
                || idx >= gs.descriptor().fields().size()) {
            return null;
        }
        if (needScalar && !gs.descriptor().field(idx).inLongs()) {
            return null;
        }
        return gs;
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
        Object rawObj = obj.executeObject(frame);
        Object o = readFieldPath(rawObj, selpath, false, this,
                kind().isIntKind() || kind().isFloatKind() || kind() == GoKind.BOOL);
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
