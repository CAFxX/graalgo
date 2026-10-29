package graalgo.p3unsafe;

import graalgo.values.GoCell;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Prefix (embedding) reinterpretation views for
 * {@code unsafe.Pointer -> *T} conversions (fix-redmaster-r40).
 *
 * <p>In gc, embedding is a memory prefix: a struct type {@code W} that
 * embeds {@code E} begins with {@code E}'s words, so
 * {@code (*W)(unsafe.Pointer(ePtr))} and
 * {@code (*E)(unsafe.Pointer(wPtr))} are free reinterpretations of the
 * same storage. In this backend's nested-struct model the two shapes
 * are different {@link GoStruct} objects — the wrapper holds the
 * embedded struct as field 0 — so the bare conversion handed the
 * embedded value to code compiled for the wrapper (or vice versa) and
 * the first promoted-field read walked the wrong descriptor:
 * reflect's {@code sliceType}/{@code structType}/
 * {@code ptrType} embed their {@code internal/abi} counterparts, so
 * {@code (*sliceType)(unsafe.Pointer(v.typ())).Elem} (field path
 * {@code [0, 1]}) read {@code PtrBytes} (a zero long) off the abi
 * shell's nested {@code Type} header instead of the shell's
 * {@code Elem} field, and the next method call on that "type" died
 * casting the long to a struct. ({@code arrayType} is a type
 * <em>alias</em> for {@code abi.ArrayType}, which is why the array
 * path never hit this.)
 *
 * <p>The view is built at the one site that knows the target type
 * ({@link UnsafeToPtrNode}, which captures the target's field-0
 * embedded type name from the wire by resolved type — never by name
 * keying on the conversion site):
 *
 * <ul>
 *   <li><b>Wrap</b>: the referent is a struct of the target's embedded
 *       type {@code E} — return a wrapper struct of the target type
 *       whose field 0 is the referent itself (the remaining fields
 *       are zero: gc reads whatever follows the prefix there, which
 *       is undefined for a bare embedded value). Reads and writes
 *       through the promoted path reach the referent's own fields,
 *       because field 0 holds the referent object, not a copy.</li>
 *   <li><b>Unwrap</b>: the referent is a wrapper whose field 0 is a
 *       struct of the target type — return that nested struct itself
 *       (fully live: it is the same object the wrapper holds).</li>
 *   <li>Anything else — including a referent already of the target
 *       type — yields no view ({@code null}) and the caller keeps the
 *       exact pre-view behavior.</li>
 *   <li><b>Field-cell prefix identity</b>: a {@code GoFieldCell} over
 *       field 0 (the storage behind {@code &owner.field0}) converted
 *       to the owner's own type yields the owner struct itself — the
 *       addressed storage is the owner's, and the field's value alone
 *       has lost the owner's remaining fields.</li>
 *   <li><b>Sibling transfer</b>: the referent is a wrapper whose
 *       field-0 prefix is canonically owned by a different wider
 *       struct {@code W} of the target's embedded type (the header's
 *       {@link GoStruct#prefixParent()}, first-store-wins so it names
 *       the canonical allocation) — the target view wraps {@code W}.
 *       This is the {@code (*interfaceType)(unsafe.Pointer(rtypePtr))}
 *       shape: {@code rtype} and {@code interfaceType} are sibling
 *       views of one abi allocation.</li>
 * </ul>
 *
 * <p>Matching is by descriptor type name on both sides (the frontend's
 * qualified type names are the model's type identity), and the
 * embedded field must be a value-embedded struct field at position 0 —
 * the only position gc's prefix layout guarantees. Pointer-embedded
 * fields are not prefix storage and never match.
 */
public final class PrefixView {
    private PrefixView() {}

    /**
     * Canonical fabricated wrappers (fix-redmaster-r43). A Wrap view is
     * a struct whose field 0 is the referent — and in gc the
     * conversion it implements ({@code (*rtype)(unsafe.Pointer(t))},
     * i.e. toRType) is a pointer reinterpretation: the same address
     * yields the same pointer, so two conversions of one *abi.Type
     * must yield views of one storage (TypeOf(0) == TypeOf(0), FuncOf
     * cache hits returning toRType of the cached type, ...). The
     * parent-return path already canonicalizes sequentially (the
     * first Wrap links the referent to its wrapper via prefixParent,
     * so later conversions return that wrapper); the cache makes
     * Wrap itself idempotent — same (referent, target) always yields
     * the same wrapper, including under concurrent conversion, where
     * two threads could otherwise mint two wrappers before either
     * links the parent. The wrapper's field 0 is the referent itself
     * (live, shared), and its tail fields are zero — sharing one
     * wrapper per (referent, target) is exactly gc's same-address
     * semantics, not an approximation of it. The parent-return and
     * unwrap paths already return canonical objects and bypass the
     * cache. Guarded by a lock: concurrent Wrap of one referent must
     * not mint two wrappers, one of which escapes uncached. Lifetime
     * is the JVM (one guest program per launcher JVM; identity keys
     * can never collide with a later run's objects while strongly
     * held). Companion fix: {@link graalgo.values.GoEquality} now
     * compares GoPointers by storage, without which even identical
     * referents compared unequal through fresh GoPointer wrappers.
     */
    private static final java.util.Map<GoStruct, java.util.Map<String, GoStruct>> WRAP_CACHE =
            new java.util.IdentityHashMap<>();

    private static synchronized GoStruct cachedWrap(GoStruct prefix, GoStructDescriptor targetDesc) {
        java.util.Map<String, GoStruct> byTarget =
                WRAP_CACHE.computeIfAbsent(prefix, k -> new java.util.HashMap<>());
        GoStruct w = byTarget.get(targetDesc.typeName());
        if (w == null) {
            w = GoStruct.zero(targetDesc);
            w.setObject(0, prefix);
            byTarget.put(targetDesc.typeName(), w);
        }
        return w;
    }

    /**
     * Builds the prefix view of {@code referent} for the target type
     * described by {@code targetDesc}, or {@code null} when no prefix
     * reinterpretation applies.
     *
     * @param embeddedQName the qualified name of the target's field-0
     *        embedded struct type, captured at build time, or
     *        {@code null} when field 0 is not a named struct type
     *        (the wrap direction then cannot match; unwrap still can,
     *        by comparing the nested value's own descriptor name
     *        against the target's)
     */
    public static GoStruct viewFor(Object referent, GoStructDescriptor targetDesc,
            String embeddedQName) {
        // Prefix identity for field cells (fix-redmaster-r40): the
        // address of a struct field at index 0 IS the address of its
        // owner (gc: the embedded field is the owner's prefix), so a
        // conversion of &owner.field0 to the owner's own type must
        // yield the OWNER, not a view of the field's value. The
        // canonical case is reflect's rtype.common() — &t.t, a
        // GoFieldCell over the abi kind shell at field 0 — converted
        // back to the kind type ((*SliceType)(unsafe.Pointer(t)) in
        // abi.Type.Elem): viewing the field value (the bare Type
        // header) instead would wrap it with the extension fields
        // zeroed, orphaning the shell's real Elem field.
        if (referent instanceof graalgo.frontend.GoFieldCell fc
                && fc.fieldIndex() == 0
                && fc.get() instanceof GoStruct
                && fc.ownerStruct().descriptor().typeName().equals(targetDesc.typeName())) {
            return fc.ownerStruct();
        }
        Object v = referent;
        if (v instanceof GoCell cell) {
            v = cell.get();
        }
        if (v instanceof UnsafeTable.ArrayRef ref) {
            // &arr[i] storage: the prefix relation is over the element.
            v = java.lang.reflect.Array.get(ref.array(), ref.offset());
            if (v instanceof GoCell cell) {
                v = cell.get();
            }
        }
        if (!(v instanceof GoStruct gs)) {
            return null;
        }
        if (gs.descriptor().typeName().equals(targetDesc.typeName())) {
            // Already the target shape: no reinterpretation.
            return null;
        }
        // Recover the embedded struct value from its own prefix
        // header first: a conversion to an intermediate prefix type
        // (e.g. *abi.Type, via v.typ()) unwraps a kind shell to its
        // bare Type header, and a later conversion to a wrapper of
        // the shell (e.g. *reflect.sliceType over abi.SliceType) must
        // start from the shell, not from the header. The header's
        // prefix parent is the shell it was stored into.
        if (embeddedQName != null
                && !embeddedQName.equals(gs.descriptor().typeName())) {
            GoStruct parent = gs.prefixParent();
            if (parent != null
                    && parent.descriptor().typeName().equals(embeddedQName)) {
                gs = parent;
            }
        }
        // Sibling transfer (fix-redmaster-r40): the referent is a
        // wrapper whose field-0 prefix H is canonically owned by a
        // DIFFERENT wider struct W — the reflect mirror types wear
        // the same allocation in gc: an rtype (wrapping the bare Type
        // header H) and an interfaceType (wrapping the InterfaceType
        // shell W, which itself wraps H) are two views of one _type.
        // Converting the rtype to *interfaceType must therefore start
        // from W, not from the rtype: the target view wraps W (or IS
        // W, when the target is W's own type).
        if (embeddedQName != null
                && !gs.descriptor().fields().isEmpty()
                && gs.descriptor().field(0).kind() == GoKind.STRUCT
                && !gs.descriptor().field(0).inLongs()
                && gs.getObject(0) instanceof GoStruct h) {
            GoStruct w = h.prefixParent();
            if (w != null && w != gs) {
                if (w.descriptor().typeName().equals(targetDesc.typeName())) {
                    return w;
                }
                if (w.descriptor().typeName().equals(embeddedQName)
                        && targetDesc.field(0).kind() == GoKind.STRUCT
                        && !targetDesc.field(0).inLongs()) {
                    GoStruct parent = w.prefixParent();
                    if (parent != null
                            && parent.descriptor().typeName().equals(targetDesc.typeName())) {
                        return parent;
                    }
                    return cachedWrap(w, targetDesc);
                }
            }
        }
        // Wrap: the referent IS the target's embedded prefix type.
        if (embeddedQName != null
                && embeddedQName.equals(gs.descriptor().typeName())
                && !targetDesc.fields().isEmpty()
                && targetDesc.field(0).kind() == GoKind.STRUCT
                && !targetDesc.field(0).inLongs()) {
            // If this value is the field-0 storage of a live struct of
            // the target type, that owner IS the addressed storage —
            // recovering it preserves the extension fields a fabricated
            // wrapper would zero (the unwrap→wrap round trip:
            // (*Type)(ptrToShell) followed by (*PtrType)(that)). Only
            // a bare prefix value with no known owner gets a fabricated
            // wrapper.
            GoStruct parent = gs.prefixParent();
            if (parent != null
                    && parent.descriptor().typeName().equals(targetDesc.typeName())) {
                return parent;
            }
            return cachedWrap(gs, targetDesc);
        }
        // Unwrap: the referent's field 0 IS the target type.
        if (!gs.descriptor().fields().isEmpty()
                && gs.descriptor().field(0).kind() == GoKind.STRUCT
                && !gs.descriptor().field(0).inLongs()) {
            Object nested = gs.getObject(0);
            if (nested instanceof GoStruct ns
                    && ns.descriptor().typeName().equals(targetDesc.typeName())) {
                return ns;
            }
        }
        return null;
    }
}
