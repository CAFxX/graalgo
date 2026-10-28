package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.GoFieldCell;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.p3reflect.AbiTypes;
import graalgo.p3reflect.ReflectTypes;
import graalgo.values.GoCell;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.UnsafeHandles;

import java.util.ArrayList;
import java.util.List;

/**
 * {@code unsafe.Pointer → *T} (P3-M8): the {@code "unsafetooptr"} wire
 * node. Resolves the handle and wraps the referent via
 * {@link UnsafeTable#pointerFor}: a cell handle becomes a cell pointer
 * (reads/writes hit the cell), an element handle an element pointer, any
 * other handle a plain pointer — so the round trip
 * {@code (*T)(unsafe.Pointer(p))} addresses the same storage {@code p}
 * does (P3-M10; previously the resolved carrier/value leaked through
 * dereference).
 *
 * <p>r37 (interface-view hook): when the resolved referent is a cell
 * holding an interface value and the target struct's field layout fits
 * the interface-word shape (see {@link AbiTypes#buildInterfaceView}),
 * the node returns a pointer to the synthesized struct view instead of
 * a pointer to the cell. Without this, {@code *(*EmptyInterface)
 * (unsafe.Pointer(&a))} — the reinterpretation {@code abi.TypeOf}
 * performs — collapses to the interface value itself, and the
 * {@code eface.Type} selector dereferences the interface's (possibly
 * null) data word instead of a struct field (the errors-init NPE:
 * {@code reflectlite.TypeOf((*error)(nil))} during {@code errors}
 * init). The layout is captured from the wire at build time (field
 * names, offsets, and type-directed roles); the view itself is built
 * at runtime from the live interface words. Targets whose layout does
 * not fit keep the pre-view behavior exactly.
 *
 * <p>fix-redmaster-r39 (byte-derived pointers): a handle produced by
 * {@link UnsafeTable#derive} resolves to an
 * {@link UnsafeTable.ByteRef}; this node unwraps it via
 * {@link UnsafeTable#resolveView} and converts the byte offset to an
 * element index using the wire's {@code elemSize} — see
 * {@link #derivedPointer}.
 *
 * <p>fix-redmaster-r40 (header views): when the target struct's field
 * layout fits the string/slice header shape (see
 * {@link #captureHeaderTarget}) and the resolved referent is a string
 * or slice value (or a cell holding one, or null), the node returns a
 * pointer to the header struct materialized by {@link HeaderView}
 * instead of a pointer to the value itself. Without this,
 * {@code (*unsafeheader.String)(v.ptr).Len} — reflect's {@code Len}
 * path for the String and Slice kinds — dereferenced the
 * {@code GoString} itself and died casting it to {@code GoStruct}.
 * Targets whose layout does not fit keep the pre-view behavior
 * exactly.
 *
 * <p>fix-redmaster-r40 (prefix views): when the target is a named
 * struct and the resolved referent is a struct of the target's
 * embedded field-0 type (or a wrapper whose field 0 is the target
 * type), the node returns a pointer to the prefix view built by
 * {@link PrefixView} — gc's embedding-is-a-prefix reinterpretation,
 * which reflect's {@code sliceType}/{@code structType} (embedding
 * their {@code internal/abi} counterparts) rely on.
 *
 * <p>fix-redmaster-r40 (struct-field storage): when the referent is a
 * struct and the handle's byte offset lands exactly on a direct field
 * whose type is the conversion target, the conversion addresses that
 * field's storage — see {@link #structFieldAt}. This is what
 * reflect's {@code Value.Field} + typed dereference requires
 * ({@code add(v.ptr, field.Offset)} followed by a conversion to the
 * field's type).
 */
public final class UnsafeToPtrNode extends GoExprNode {
    @Child private GoExprNode child;

    /** Qualified name of the target struct, or null when no view applies. */
    private final String targetQName;

    /** Field layout for the view, or null when no view applies. */
    private final List<AbiTypes.ViewField> viewLayout;

    /** Qualified name of the header-view target struct, or null when no header view applies. */
    private final String headerQName;

    /** Prefix-view target (target qname + field-0 embedded qname), or null. */
    private final PrefixTarget prefixTarget;

    /**
     * Interface-unpack layout (fix-redmaster-r40): when the target
     * {@code T} is an interface type, the field layout of the abi
     * interface-word struct a guest may have stored at the referent
     * ({@code EmptyInterface} for an empty target interface,
     * {@code NonEmptyInterface} otherwise), or null.
     */
    private final List<AbiTypes.ViewField> ifaceLayout;

    /**
     * Byte size of the target element type {@code T}, emitted by the
     * frontend from its go/types {@code Sizes} (the same oracle that folds
     * the program's own {@code unsafe.Sizeof} constants), or {@code -1}
     * when the wire carries none. Only consulted for byte-derived
     * pointers (fix-redmaster-r39): the byte offset of an
     * {@link UnsafeTable.ByteRef} is divided by this size to recover the
     * element index.
     */
    private final long elemSize;

    /**
     * Wire type-table id of the target element type {@code T} (the
     * {@code elem} of the {@code *T} entry), or -1 when the target is
     * not a pointer entry. Used by {@link #structFieldAt} to match a
     * conversion target against struct field layouts.
     */
    private final int targetElemWireId;

    /** True when the target is a pointer to a pointer type
     * ({@code **U}): the interface type-word rule below applies. */
    private final boolean ptrToPtr;

    /** Prefix-view target for a {@code **U} conversion (the {@code U}
     * struct's qname plus its field-0 embedded qname), or null when
     * {@code U} is not a named struct — the type-word cell then holds
     * the raw shell pointer. */
    private final PrefixTarget ptrToPtrTarget;

    private UnsafeToPtrNode(GoExprNode child, String targetQName,
            List<AbiTypes.ViewField> viewLayout, String headerQName,
            PrefixTarget prefixTarget, long elemSize, int targetElemWireId,
            List<AbiTypes.ViewField> ifaceLayout, boolean ptrToPtr,
            PrefixTarget ptrToPtrTarget) {
        super(GoKind.POINTER);
        this.child = child;
        this.targetQName = targetQName;
        this.viewLayout = viewLayout;
        this.headerQName = headerQName;
        this.prefixTarget = prefixTarget;
        this.elemSize = elemSize;
        this.targetElemWireId = targetElemWireId;
        this.ifaceLayout = ifaceLayout;
        this.ptrToPtr = ptrToPtr;
        this.ptrToPtrTarget = ptrToPtrTarget;
    }

    /** A prefix-view target: the struct's qname plus its field-0 embedded struct qname. */
    private record PrefixTarget(String qname, String embeddedQName) {}

    @Override
    public Object executeObject(VirtualFrame frame) {
        long handle = child.executeLong(frame);
        UnsafeTable.ResolvedView view = UnsafeTable.current().resolveView(handle);
        Object resolved = view.root();
        // Set when a byte-offset field normalization produced a cell
        // whose field type is NOT the conversion target: the cell may
        // only be consumed by one of the views below (e.g. the header
        // view over a string field); if no view fires, the conversion
        // is a reinterpretation D5 forbids and traps like the
        // array-derivation path would have.
        boolean strictField = false;
        if (view.byteOffset() != 0) {
            // A byte offset into a struct referent that lands exactly
            // on a direct field is that field's storage (reflect's
            // Field machinery) — normalize to the field cell and let
            // the view chain below convert it; anything else keeps
            // the array-derivation semantics.
            FieldHit hit = structFieldAt(resolved, view.byteOffset());
            if (hit == null) {
                return derivedPointer(resolved, view.byteOffset());
            }
            if (hit.fieldTok() != hit.targetTok()
                    && headerQName == null && prefixTarget == null
                    && viewLayout == null && ifaceLayout == null) {
                return derivedPointer(resolved, view.byteOffset());
            }
            strictField = hit.fieldTok() != hit.targetTok();
            resolved = hit.cell();
        } else {
            // Offset 0: a conversion whose target is a direct field's
            // type (not the referent struct's own type) addresses that
            // field's storage — gc's prefix identity for field 0.
            FieldHit hit = structFieldAt(resolved, 0);
            if (hit != null && hit.fieldTok() == hit.targetTok()) {
                resolved = hit.cell();
            }
        }
        if (headerQName != null) {
            GoStruct header = buildHeaderView(resolved);
            if (header != null) {
                return new GoPointer(header);
            }
        }
        if (prefixTarget != null) {
            GoStruct prefixed = buildPrefixView(resolved);
            if (prefixed != null) {
                return new GoPointer(prefixed);
            }
        }
        if (viewLayout != null && targetQName != null
                && storedValue(resolved) instanceof GoInterface gi) {
            GoStruct viewStruct = AbiTypes.buildInterfaceView(gi, targetQName, viewLayout);
            if (viewStruct != null) {
                return new GoPointer(viewStruct);
            }
        }
        if (ifaceLayout != null
                && storedValue(resolved) instanceof GoStruct gs) {
            // Interface-word struct → interface value (the inverse
            // view): guest code that manufactures an interface by
            // writing an abi.EmptyInterface/NonEmptyInterface struct
            // and dereferencing it as the interface type (reflect's
            // packEface) must yield a real GoInterface, not the raw
            // struct. The unpack is a snapshot (the r38 N3 class): a
            // fresh cell holds the unpacked value, so writes through
            // the converted pointer do not reach the original struct.
            GoInterface gi = AbiTypes.unpackInterfaceView(gs, ifaceLayout);
            if (gi != null) {
                return UnsafeTable.pointerFor(new graalgo.values.GoBox(gi));
            }
        }
        if (strictField) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "unsafe: byte-offset derivation from a non-array referent traps "
                            + "(D5: the handle model cannot reinterpret memory)");
        }
        if (ptrToPtr && storedValue(resolved) instanceof GoInterface gi) {
            // Interface type-word pointer (fix-redmaster-r42): in gc
            // an interface value in memory is (type word, data word),
            // so a pointer to an interface value reinterpreted as
            // **U points at the TYPE word — reflect's ArrayOf/FuncOf
            // prototype trick reads the dynamic type through it
            // ((**arrayType)(unsafe.Pointer(&iarray)) derefs to the
            // [1]T array type's rtype). The fallthrough below would
            // yield a pointer to the interface storage instead, whose
            // deref is the DATA word (the array value) — the observed
            // Object[]-as-struct failure. Return a pointer to a fresh
            // cell holding the dynamic type's shell pointer instead:
            // one deref yields the type pointer, two yield the type
            // struct (copied by assignment, so the canonical shell is
            // never mutated). Writes through it are snapshot
            // semantics (the r38 N3 class): they land in the fresh
            // cell, not in the interface's type word — the read path
            // is exact, and rewriting an interface's dynamic type
            // through its header is deliberate unsafe abuse, not a
            // shape real code relies on. A nil interface's type word
            // is nil, so its cell holds null and dereferencing it
            // panics with the usual nil dereference.
            Object typePtr = null;
            if (!gi.isNil()) {
                long tok = ReflectTypes.tokForDescriptor(gi.descriptor());
                typePtr = AbiTypes.abiPointerForTok(tok);
                if (ptrToPtrTarget != null && typePtr instanceof GoPointer gp
                        && gp.referent() instanceof GoStruct shell) {
                    // The guest views the type through the target
                    // wrapper, not through the bare abi shell:
                    // reflect's sliceType/arrayType/etc. embed their
                    // abi counterparts, so a *sliceType pointer must
                    // address a 3-level wrapper (sliceType{SliceType{
                    // Type}}), not the 2-level shell — otherwise the
                    // guest's promoted-field paths (e.g. [0,0,3] for
                    // slice.TFlag) walk off the shell's layout. The
                    // prefix view builds exactly that wrapper (or
                    // returns null when the shell already is the
                    // target shape, in which case the raw pointer
                    // stands). The wrapper shares the shell until
                    // assignment copies it, as for every view.
                    try {
                        graalgo.values.GoStructDescriptor desc =
                                ReflectTypes.structDescriptorFor(ptrToPtrTarget.qname());
                        GoStruct wrapped = PrefixView.viewFor(shell, desc,
                                ptrToPtrTarget.embeddedQName());
                        if (wrapped != null) {
                            typePtr = new GoPointer(wrapped);
                        }
                    } catch (RuntimeException e) {
                        // Unresolvable target descriptor: keep the raw
                        // shell pointer (the pre-view behavior).
                    }
                }
            }
            return UnsafeTable.pointerFor(new graalgo.values.GoBox(typePtr));
        }
        return UnsafeTable.pointerFor(resolved);
    }

    /**
     * Resolves a byte-derived pointer (fix-redmaster-r39): the root must
     * be array storage (an {@link UnsafeTable.ArrayRef} or a bare Java
     * array), the byte offset must be an exact multiple of the target
     * element size, and the resulting element index must lie in
     * {@code [0, length]} (one-past-the-end may be formed, as in gc;
     * dereferencing it fails loud through the element-pointer
     * machinery). The result is the canonical element pointer — the same
     * {@code GoPointer} shape {@code &arr[i]} produces — so reads and
     * writes through it alias the array slot. Anything else (a non-array
     * root, a misaligned offset, an out-of-range index, a missing
     * element size) traps: D5 cannot reinterpret memory.
     */
    private Object derivedPointer(Object root, long byteOffset) {
        Object array;
        long baseIndex;
        if (root instanceof UnsafeTable.ArrayRef ref) {
            array = ref.array();
            baseIndex = ref.offset();
        } else if (root != null && root.getClass().isArray()) {
            array = root;
            baseIndex = 0;
        } else {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "unsafe: byte-offset derivation from a non-array referent traps "
                            + "(D5: the handle model cannot reinterpret memory)");
        }
        if (elemSize <= 0) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "unsafe: byte-derived pointer conversion lacks a target element size "
                            + "(D5: cannot index the referent without it)");
        }
        if (byteOffset % elemSize != 0) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "unsafe: byte offset " + byteOffset + " is not a multiple of the target "
                            + "element size " + elemSize + " (D5: misaligned derivation traps)");
        }
        long index = baseIndex + byteOffset / elemSize;
        int length = UnsafeOps.arrayLength(array);
        if (index < 0 || index > length) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "unsafe: byte-derived element index " + index + " out of range [0, "
                            + length + "] (D5: derivation cannot leave its referent)");
        }
        return UnsafeTable.pointerFor(new UnsafeTable.ArrayRef(array, (int) index));
    }

    /** A struct-field storage hit: the field cell plus both type tokens. */
    private record FieldHit(GoCell cell, long fieldTok, long targetTok) {}

    /**
     * Struct-field storage resolution (fix-redmaster-r40): when the
     * referent is a struct value and {@code byteOffset} lands exactly
     * on a DIRECT field of the referent's own layout, that field's
     * storage is the addressed storage — return a field cell over it
     * (with the field's and the target's type tokens for the caller's
     * consumption checks), or null when no field boundary matches.
     *
     * <p>This is the consumption half of reflect's field machinery:
     * {@code Value.Field} forms {@code add(v.ptr, field.Offset)} and
     * {@code Value.Int}/{@code Len}/... convert the result onward
     * ({@code (*int)(v.ptr)}, {@code (*unsafeheader.String)(v.ptr)}).
     * In gc those addresses ARE the field's bytes; in the value model
     * the field cell is the aliasing storage with exactly that
     * identity, so reads and writes through the converted pointer hit
     * the field. Offsets that land mid-field or between fields do not
     * resolve here — the caller's array-derivation path traps them
     * (D5 preserved). Matching is by layout facts only (the referent
     * type's own field offsets from the reflect type table, plus a
     * descriptor name cross-check) — never by type or field names.
     * The target type must differ from the referent struct's own type
     * (a same-type conversion is the identity, not a field access).
     */
    private FieldHit structFieldAt(Object referent, long byteOffset) {
        // Unwrap storage cells to the stored value (fix-redmaster-r42):
        // reflect-allocated storage arrives as a ReflectRefs.Cell (the
        // unsafe_New handle's referent), not a GoCell — without this the
        // field match below misses and the conversion falls back to a
        // pointer to the cell object itself, so a typed dereference
        // reads the cell instead of the field.
        Object root = referent;
        while (true) {
            if (root instanceof GoCell cell) {
                root = cell.get();
            } else if (root instanceof graalgo.p3reflect.ReflectRefs.Cell rc) {
                root = rc.value;
            } else {
                break;
            }
        }
        if (!(root instanceof GoStruct gs) || targetElemWireId < 0) {
            return null;
        }
        ReflectTypes.TypeInfo rootInfo;
        long targetTok;
        try {
            rootInfo = ReflectTypes.infoForName(gs.descriptor().typeName());
            targetTok = ReflectTypes.tokForWireId(targetElemWireId);
        } catch (RuntimeException e) {
            return null;
        }
        if (rootInfo == null || targetTok < 0
                || rootInfo.tok == targetTok || rootInfo.fields == null
                || rootInfo.kind != GoKind.STRUCT) {
            return null;
        }
        for (ReflectTypes.FieldInfo f : rootInfo.fields) {
            if (f.offset != byteOffset || f.type == null || f.index.length != 1) {
                continue;
            }
            int ord = f.index[0];
            if (ord < 0 || ord >= gs.descriptor().fields().size()
                    || !gs.descriptor().field(ord).name().equals(f.name)) {
                return null;
            }
            return new FieldHit(
                    new GoFieldCell(gs, ord, gs.descriptor().field(ord).kind()),
                    f.type.tok, targetTok);
        }
        return null;
    }

    /**
     * Builds the node from the {@code "unsafetooptr"} wire node. Fields:
     * {@code x} (the handle operand), {@code type} (result type-table id —
     * the {@code *T} type), {@code elemSize} (optional; the byte size of
     * {@code T} from the frontend's go/types Sizes).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafetooptr");
        int typeId = UnsafeWire.requireTypeId(f, "unsafetooptr");
        long elemSize = -1;
        Json.Value es = f.opt("elemSize");
        if (es instanceof Json.Num num) {
            elemSize = num.asLong("unsafetooptr.fields.elemSize");
        }
        ViewTarget target = captureViewTarget(ctx, typeId);
        String headerQName = captureHeaderTarget(ctx, typeId);
        PrefixTarget prefixTarget = capturePrefixTarget(ctx, typeId);
        int targetElemWireId = -1;
        boolean ptrToPtr = false;
        PrefixTarget ptrToPtrTarget = capturePtrToPtrTarget(ctx, typeId);
        Json.Object typeEntry = ctx.types.entry(typeId, "unsafetooptr.fields.type");
        if ("pointer".equals(typeEntry.string("kind", "unsafetooptr.fields.type"))) {
            targetElemWireId = typeEntry.object("detail", "unsafetooptr.fields.type")
                    .intValue("elem", "unsafetooptr.fields.type");
            Json.Object elemEntry = ctx.types.entry(targetElemWireId, "unsafetooptr.fields.type");
            ptrToPtr = "pointer".equals(elemEntry.string("kind", "unsafetooptr.fields.type"));
        }
        GoExprNode n = new UnsafeToPtrNode(
                UnsafeWire.child(f, "unsafetooptr", "x", factory, ctx),
                target == null ? null : target.qname(),
                target == null ? null : target.layout(),
                headerQName,
                prefixTarget,
                elemSize,
                targetElemWireId,
                captureIfaceTarget(ctx, typeId),
                ptrToPtr,
                ptrToPtrTarget);
        n.setTypeId(typeId);
        return n;
    }

    /** A viewable target: its qualified name plus its field layout. */
    private record ViewTarget(String qname, List<AbiTypes.ViewField> layout) {}

    /**
     * Captures the interface-unpack layout for a {@code *T} target
     * whose {@code T} is an interface type (fix-redmaster-r40), or null.
     * The layout is the abi interface-word struct shape for the target:
     * {@code EmptyInterface} (Type word + Data word) when the interface
     * has no methods, {@code NonEmptyInterface} (Itab word + Data word)
     * otherwise — determined by the target interface's own method list
     * from the wire, never by a type name.
     */
    private static List<AbiTypes.ViewField> captureIfaceTarget(NodeFactory.BuildCtx ctx, int typeId) {
        String path = "unsafetooptr.fields.type";
        Json.Object entry = ctx.types.entry(typeId, path);
        if (!"pointer".equals(entry.string("kind", path))) {
            return null;
        }
        int elemId = entry.object("detail", path).intValue("elem", path);
        Json.Object e = ctx.types.entry(elemId, path);
        for (int step = 0; step < 32; step++) {
            if (!"named".equals(e.string("kind", path))) {
                break;
            }
            int underlying = e.object("detail", path).intValue("underlying", path);
            e = ctx.types.entry(underlying, path);
        }
        if (!"interface".equals(e.string("kind", path))) {
            return null;
        }
        Json.Array methods = e.object("detail", path).array("methods", path);
        if (methods.size() == 0) {
            return List.of(
                    new AbiTypes.ViewField("Type", 0, AbiTypes.ViewRole.ABI_TYPE),
                    new AbiTypes.ViewField("Data", 8, AbiTypes.ViewRole.DATA));
        }
        return List.of(
                new AbiTypes.ViewField("Itab", 0, AbiTypes.ViewRole.ITAB),
                new AbiTypes.ViewField("Data", 8, AbiTypes.ViewRole.DATA));
    }

    /**
     * Captures the view layout for a {@code *T} target, or null when the
     * target is not a named struct whose fields are all classifiable as
     * interface words. Classification is type-directed (a field is an
     * abi-type word iff its type resolves to a pointer to
     * {@code internal/abi.Type}, an itab word iff to a pointer to
     * {@code internal/abi.Itab}, a data word iff to {@code uintptr} or
     * {@code unsafe.Pointer}) — never keyed on the target struct's name.
     */
    private static ViewTarget captureViewTarget(NodeFactory.BuildCtx ctx, int typeId) {
        String path = "unsafetooptr.fields.type";
        Json.Object entry = ctx.types.entry(typeId, path);
        if (!"pointer".equals(entry.string("kind", path))) {
            return null;
        }
        int elemId = entry.object("detail", path).intValue("elem", path);
        Json.Object e = ctx.types.entry(elemId, path);
        String qname = null;
        for (int step = 0; step < 32; step++) {
            if (!"named".equals(e.string("kind", path))) {
                break;
            }
            qname = e.object("detail", path).string("name", path);
            int underlying = e.object("detail", path).intValue("underlying", path);
            e = ctx.types.entry(underlying, path);
        }
        if (!"struct".equals(e.string("kind", path)) || qname == null) {
            return null;
        }
        Json.Array fields = e.object("detail", path).array("fields", path);
        List<AbiTypes.ViewField> layout = new ArrayList<>(fields.size());
        for (int i = 0; i < fields.size(); i++) {
            Json.Value fv = fields.get(i);
            if (!(fv instanceof Json.Object fo)) {
                return null;
            }
            String fname = fo.string("name", path);
            long offset = fo.longInt("offset", path);
            int ftype = fo.intValue("type", path);
            AbiTypes.ViewRole role = classifyViewField(ctx, ftype, path);
            if (role == null) {
                return null;
            }
            layout.add(new AbiTypes.ViewField(fname, offset, role));
        }
        return new ViewTarget(qname, layout);
    }

    /**
     * The value stored at the addressed storage (fix-redmaster-r40):
     * a cell's current value, an array element's value (for
     * {@code &arr[i]} referents — e.g. an interface value sitting in a
     * variadic-args array, whose {@code EmptyInterface} reinterpretation
     * in {@code unpackEface} the interface view must serve just like
     * the cell case), or the referent itself.
     */
    private static Object storedValue(Object resolved) {
        Object v = resolved;
        if (v instanceof GoCell cell) {
            v = cell.get();
        }
        if (v instanceof UnsafeTable.ArrayRef ref) {
            v = java.lang.reflect.Array.get(ref.array(), ref.offset());
            if (v instanceof GoCell cell) {
                v = cell.get();
            }
        }
        return v;
    }

    /**
     * Builds the header view of {@code resolved} for this node's
     * header target, or null when the referent is not a string/slice
     * value (or the target's descriptor cannot be resolved) — the
     * caller then falls back to the pre-view behavior exactly.
     */
    private GoStruct buildHeaderView(Object resolved) {
        graalgo.values.GoStructDescriptor desc;
        try {
            desc = graalgo.p3reflect.ReflectTypes.structDescriptorFor(headerQName);
        } catch (RuntimeException e) {
            return null;
        }
        return HeaderView.build(resolved, desc);
    }

    /**
     * Builds the prefix view of {@code resolved} for this node's
     * prefix target, or null when no prefix reinterpretation applies
     * (or the target's descriptor cannot be resolved) — the caller
     * then falls back to the pre-view behavior exactly.
     */
    private GoStruct buildPrefixView(Object resolved) {
        graalgo.values.GoStructDescriptor desc;
        try {
            desc = graalgo.p3reflect.ReflectTypes.structDescriptorFor(prefixTarget.qname());
        } catch (RuntimeException e) {
            return null;
        }
        return PrefixView.viewFor(resolved, desc, prefixTarget.embeddedQName());
    }

    /**
     * Captures the prefix-view target for a {@code **U} conversion:
     * the same shape as {@link #capturePrefixTarget} one pointer level
     * deeper — {@code U}'s qualified name plus its field-0 embedded
     * type's qualified name. Returns null when the target is not a
     * pointer to a pointer to a named struct (the type-word cell then
     * holds the raw shell pointer, as before).
     */
    private static PrefixTarget capturePtrToPtrTarget(NodeFactory.BuildCtx ctx, int typeId) {
        String path = "unsafetooptr.fields.type";
        Json.Object entry = ctx.types.entry(typeId, path);
        if (!"pointer".equals(entry.string("kind", path))) {
            return null;
        }
        int elemId = entry.object("detail", path).intValue("elem", path);
        Json.Object elemEntry = ctx.types.entry(elemId, path);
        if (!"pointer".equals(elemEntry.string("kind", path))) {
            return null;
        }
        int elemElemId = elemEntry.object("detail", path).intValue("elem", path);
        // Reuse the *T capture on the elem-elem id: it walks the named
        // chain and returns the struct qname + field-0 embedded qname.
        return captureNamedPrefixTarget(ctx, elemElemId, path);
    }

    /**
     * Captures the prefix-view target for a {@code *T} conversion: the
     * target struct's qualified name plus the qualified name of its
     * field-0 type when that type is a named struct (the embedding
     * prefix candidate). Returns null when the target is not a named
     * struct at all. The runtime match itself lives in
     * {@link PrefixView} and is purely descriptor-name-directed.
     */
    private static PrefixTarget capturePrefixTarget(NodeFactory.BuildCtx ctx, int typeId) {
        String path = "unsafetooptr.fields.type";
        Json.Object entry = ctx.types.entry(typeId, path);
        if (!"pointer".equals(entry.string("kind", path))) {
            return null;
        }
        int elemId = entry.object("detail", path).intValue("elem", path);
        return captureNamedPrefixTarget(ctx, elemId, path);
    }

    /** The named-struct half of the prefix-target capture: walks the
     * named chain to the underlying struct and reads its field-0
     * embedded type's qualified name. */
    private static PrefixTarget captureNamedPrefixTarget(NodeFactory.BuildCtx ctx, int namedId,
            String path) {
        Json.Object e = ctx.types.entry(namedId, path);
        String qname = null;
        for (int step = 0; step < 32; step++) {
            if (!"named".equals(e.string("kind", path))) {
                break;
            }
            qname = e.object("detail", path).string("name", path);
            int underlying = e.object("detail", path).intValue("underlying", path);
            e = ctx.types.entry(underlying, path);
        }
        if (!"struct".equals(e.string("kind", path)) || qname == null) {
            return null;
        }
        Json.Array fields = e.object("detail", path).array("fields", path);
        if (fields.size() == 0 || !(fields.get(0) instanceof Json.Object f0)) {
            return new PrefixTarget(qname, null);
        }
        int f0type = f0.intValue("type", path);
        Json.Object fe = ctx.types.entry(f0type, path);
        String embeddedQName = null;
        for (int step = 0; step < 32; step++) {
            if (!"named".equals(fe.string("kind", path))) {
                break;
            }
            embeddedQName = fe.object("detail", path).string("name", path);
            int underlying = fe.object("detail", path).intValue("underlying", path);
            fe = ctx.types.entry(underlying, path);
        }
        if (!"struct".equals(fe.string("kind", path))) {
            embeddedQName = null;
        }
        return new PrefixTarget(qname, embeddedQName);
    }

    /**
     * Captures the header-view target for a {@code *T} conversion, or
     * null when {@code T} is not a header-shaped struct. The shape is
     * classified by field TYPE alone (never by struct or field name):
     * exactly 2 or 3 fields at word offsets 0, 8, (16), where field 0
     * is a data word ({@code unsafe.Pointer}, {@code uintptr}, or any
     * pointer type) and fields 1..2 are integer words (basic int/uint
     * class). This is the layout of {@code unsafeheader.String} /
     * {@code unsafeheader.Slice} and of {@code reflect.StringHeader} /
     * {@code reflect.SliceHeader} — and of any user struct with the
     * same shape, which gc likewise treats as raw header words.
     * Interface-view targets ({@code EmptyInterface} and kin) do NOT
     * fit: their second word is a data word, not an integer word, so
     * the two captures are disjoint.
     */
    private static String captureHeaderTarget(NodeFactory.BuildCtx ctx, int typeId) {
        String path = "unsafetooptr.fields.type";
        Json.Object entry = ctx.types.entry(typeId, path);
        if (!"pointer".equals(entry.string("kind", path))) {
            return null;
        }
        int elemId = entry.object("detail", path).intValue("elem", path);
        Json.Object e = ctx.types.entry(elemId, path);
        String qname = null;
        for (int step = 0; step < 32; step++) {
            if (!"named".equals(e.string("kind", path))) {
                break;
            }
            qname = e.object("detail", path).string("name", path);
            int underlying = e.object("detail", path).intValue("underlying", path);
            e = ctx.types.entry(underlying, path);
        }
        if (!"struct".equals(e.string("kind", path)) || qname == null) {
            return null;
        }
        Json.Array fields = e.object("detail", path).array("fields", path);
        if (fields.size() < 2 || fields.size() > 3) {
            return null;
        }
        for (int i = 0; i < fields.size(); i++) {
            Json.Value fv = fields.get(i);
            if (!(fv instanceof Json.Object fo)) {
                return null;
            }
            long offset = fo.longInt("offset", path);
            if (offset != 8L * i) {
                return null;
            }
            int ftype = fo.intValue("type", path);
            boolean fits = i == 0
                    ? isDataWordField(ctx, ftype, path)
                    : isIntWordField(ctx, ftype, path);
            if (!fits) {
                return null;
            }
        }
        return qname;
    }

    /**
     * Whether a wire type is a header data word: {@code unsafe.Pointer}
     * (basic, pointer class), {@code uintptr} (basic, uint class), or
     * any pointer type. Named wrappers are unwrapped first.
     */
    private static boolean isDataWordField(NodeFactory.BuildCtx ctx, int typeId, String path) {
        Json.Object e = ctx.types.entry(typeId, path);
        for (int step = 0; step < 32; step++) {
            if (!"named".equals(e.string("kind", path))) {
                break;
            }
            int underlying = e.object("detail", path).intValue("underlying", path);
            e = ctx.types.entry(underlying, path);
        }
        String kind = e.string("kind", path);
        if ("pointer".equals(kind)) {
            return true;
        }
        if ("basic".equals(kind)) {
            Json.Object d = e.object("detail", path);
            return "pointer".equals(d.string("class", path))
                    || "uintptr".equals(d.string("name", path));
        }
        return false;
    }

    /**
     * Whether a wire type is a header integer word: a basic int/uint
     * class type (named wrappers unwrapped first).
     */
    private static boolean isIntWordField(NodeFactory.BuildCtx ctx, int typeId, String path) {
        Json.Object e = ctx.types.entry(typeId, path);
        for (int step = 0; step < 32; step++) {
            if (!"named".equals(e.string("kind", path))) {
                break;
            }
            int underlying = e.object("detail", path).intValue("underlying", path);
            e = ctx.types.entry(underlying, path);
        }
        if (!"basic".equals(e.string("kind", path))) {
            return false;
        }
        String bclass = e.object("detail", path).string("class", path);
        return "int".equals(bclass) || "uint".equals(bclass);
    }

    /** Classifies one target field as an interface word, or null if it is not one. */
    private static AbiTypes.ViewRole classifyViewField(NodeFactory.BuildCtx ctx, int typeId, String path) {
        Json.Object e = ctx.types.entry(typeId, path);
        for (int step = 0; step < 32; step++) {
            if (!"named".equals(e.string("kind", path))) {
                break;
            }
            int underlying = e.object("detail", path).intValue("underlying", path);
            e = ctx.types.entry(underlying, path);
        }
        String kind = e.string("kind", path);
        Json.Object d = e.object("detail", path);
        if ("pointer".equals(kind)) {
            int elemId = d.intValue("elem", path);
            Json.Object ee = ctx.types.entry(elemId, path);
            for (int step = 0; step < 32; step++) {
                if (!"named".equals(ee.string("kind", path))) {
                    break;
                }
                String ename = ee.object("detail", path).string("name", path);
                if ("internal/abi.Type".equals(ename)) {
                    return AbiTypes.ViewRole.ABI_TYPE;
                }
                if ("internal/abi.Itab".equals(ename)) {
                    return AbiTypes.ViewRole.ITAB;
                }
                int underlying = ee.object("detail", path).intValue("underlying", path);
                ee = ctx.types.entry(underlying, path);
            }
            return null;
        }
        if ("basic".equals(kind)) {
            String bname = d.string("name", path);
            String bclass = d.string("class", path);
            if ("pointer".equals(bclass) || "uintptr".equals(bname)) {
                return AbiTypes.ViewRole.DATA;
            }
            return null;
        }
        return null;
    }
}
