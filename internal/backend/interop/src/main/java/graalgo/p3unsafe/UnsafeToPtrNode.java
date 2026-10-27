package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.p3reflect.ReflectTypes;
import graalgo.values.GoCell;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

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
 * <p>P3-M17: unsafe struct reinterpretation. Go permits
 * {@code (*B)(unsafe.Pointer(p))} where {@code p} is a {@code *A} and
 * {@code B} is a struct whose layout starts with {@code A}'s — the
 * canonical case is a single-field wrapper ({@code reflect.rtype{t abi.Type}}
 * over {@code internal/abi.Type}, via {@code toRType}). The referent
 * carries {@code A}'s descriptor, but field selections use {@code B}'s
 * indices, so a bare pointer would misalign every field. When the target
 * is a single-field struct whose field type is a struct, and the
 * referent's descriptor names that field's struct type, the referent is
 * wrapped in a {@code B}-described struct (the inner value is shared, so
 * gc's aliasing is preserved). All decisions are descriptor-driven; no
 * package or type names are special-cased.
 *
 * <p>P3-M18: the single-field wrapper may also have an interface-typed
 * field (e.g. {@code reflect.rtype{t abi.Type}} where the referent is an
 * {@code abi.Type} interface value). In that case a {@code GoInterface}
 * referent is wrapped directly in the target descriptor.
 *
 * <p>fix-redmaster-r27 (slice-header reinterpretation): gc permits
 * {@code (*SliceHeader)(unsafe.Pointer(&s))} for a slice variable
 * {@code s} — the memory at {@code &s} IS the header (data, len, cap).
 * Graalgo stores a slice variable's value as a {@link GoSlice} object,
 * not as three words, so a bare pointer would hand field selection a
 * GoSlice where it expects a GoStruct (ClassCastException). When the
 * target is a struct and the referent (cell-unwrapped) is a GoSlice,
 * the header words are materialized as a GoStruct in the target
 * descriptor: field i reads header word i (data handle, len, cap),
 * converted per the field's carrier (longs fields take the word bits;
 * UNSAFE_POINTER object fields take the data handle; POINTER object
 * fields at word 0 take an element pointer). Descriptor-driven, not
 * type-name-keyed: any struct layout over a slice variable reinterprets
 * the same way. Targets with more fields than the header has words, or
 * with object fields that cannot hold a header word, fail closed to
 * the previous behavior. Reads through the materialized header are
 * exact; writes to it do not propagate back to the slice variable
 * (the same copy limitation as the P3-M17 wrapper materialization —
 * field writes go through the GoStruct, not through the cell).
 *
 * <p>fix-redmaster-r27 (extended-type reinterpretation): real reflect
 * reads extended type data through unsafe reinterpretation —
 * {@code (*structType)(unsafe.Pointer(v.typ()))} for NumField/Field,
 * {@code (*sliceType)(...)} for Index's Elem, {@code (*arrayType)(...)}
 * for Index's Len/Elem. In gc the type object IS the full extended
 * struct (its first words are the abi.Type); graalgo materializes only
 * the abi.Type prefix, so the P3-M17 single-field wrapper (which needs
 * the referent's descriptor name to equal the field's struct type name)
 * does not fire and field selection walks abi.Type's layout with the
 * extended struct's indices (Size_ read as a struct, then CCE). When
 * the target matches an extended-type shape (struct with a first field
 * of abi.Type layout, plus the shape's remaining fields) and the
 * referent is a materialized abi.Type struct (token recovered from the
 * per-run table), the full extended struct is materialized from the
 * type's TypeInfo: struct fields (names/tags encoded per abi.Name,
 * offsets, field-type abi.Type structs), slice/array element types,
 * array length. Shape recognition is structural (field layouts from
 * descriptors + the type table), never type-name-keyed; anything
 * unrecognized fails closed to the previous behavior. The materialized
 * struct's Type prefix is the shared abi.Type struct, so common-field
 * reads stay exact.
 */
public final class UnsafeToPtrNode extends GoExprNode {
    @Child private GoExprNode child;
    /**
     * Non-null when the target {@code *T} is a single-field struct wrapper:
     * the wrapper's descriptor, the wrapped struct's type name, and the
     * wrapper field's index in the objects array.
     */
    private final GoStructDescriptor wrapperDesc;
    private final String wrappedTypeName;
    private final int wrappedFieldIndex;
    /**
     * Non-null when the target {@code *T} is a single-field struct wrapper
     * whose field is an interface (e.g. {@code *reflect.rtype} over
     * {@code *abi.Type} via {@code toRType}): a {@code GoInterface} referent
     * is wrapped in the target descriptor. The interface field's index in
     * the objects array.
     */
    private final GoStructDescriptor ifaceWrapperDesc;
    private final int ifaceWrapperFieldIndex;
    /**
     * Non-null when the target {@code *T} is a two-field
     * (POINTER, UNSAFE_POINTER) struct (e.g. {@code *abi.EmptyInterface}):
     * a {@code GoInterface} referent is materialized as the struct.
     */
    private final GoStructDescriptor ifaceStructDesc;
    /**
     * The target struct's descriptor when {@code *T}'s element is a
     * struct (null otherwise): used for slice-header materialization.
     */
    private final GoStructDescriptor targetDesc;
    /** Extended-type shape for fix-redmaster-r27 (NONE when inapplicable). */
    private final ExtShape extShape;
    /** True when the target is a single-field wrapper around the extended struct. */
    private final boolean extWrapper;
    /** The extended struct's descriptor (structType/sliceType/arrayType shape). */
    private final GoStructDescriptor extDesc;
    /** The Name struct descriptor for the extended struct's PkgPath field (struct shape). */
    private final GoStructDescriptor extNameDesc;
    /** The StructField struct descriptor for the extended struct's Fields slice (struct shape). */
    private final GoStructDescriptor extFieldDesc;

    private enum ExtShape { NONE, STRUCT, SLICE, ARRAY }

    private record ExtInfo(ExtShape shape, boolean wrapper, GoStructDescriptor eDesc,
            GoStructDescriptor nameDesc, GoStructDescriptor fieldDesc) {
        static final ExtInfo NONE = new ExtInfo(ExtShape.NONE, false, null, null, null);
    }

    private UnsafeToPtrNode(GoExprNode child, GoStructDescriptor wrapperDesc,
            String wrappedTypeName, int wrappedFieldIndex,
            GoStructDescriptor ifaceWrapperDesc, int ifaceWrapperFieldIndex,
            GoStructDescriptor ifaceStructDesc, GoStructDescriptor targetDesc,
            ExtInfo extInfo) {
        super(GoKind.POINTER);
        this.child = child;
        this.wrapperDesc = wrapperDesc;
        this.wrappedTypeName = wrappedTypeName;
        this.wrappedFieldIndex = wrappedFieldIndex;
        this.ifaceWrapperDesc = ifaceWrapperDesc;
        this.ifaceWrapperFieldIndex = ifaceWrapperFieldIndex;
        this.ifaceStructDesc = ifaceStructDesc;
        this.targetDesc = targetDesc;
        this.extShape = extInfo.shape();
        this.extWrapper = extInfo.wrapper();
        this.extDesc = extInfo.eDesc();
        this.extNameDesc = extInfo.nameDesc();
        this.extFieldDesc = extInfo.fieldDesc();
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        long handle = child.executeLong(frame);
        Object referent = UnsafeTable.current().resolve(handle);
        if (wrapperDesc != null && referent instanceof GoStruct gs
                && wrappedTypeName.equals(gs.descriptor().typeName())) {
            // P3-M17: reinterpretation (*B)(unsafe.Pointer(p)) with p a *A:
            // present the referent through B's descriptor so B's field
            // indices select correctly. The inner struct is shared, not
            // copied, matching gc's memory aliasing.
            Object[] objects = new Object[wrapperDesc.objectsCount()];
            objects[wrappedFieldIndex] = gs;
            referent = new GoStruct(wrapperDesc,
                    new long[wrapperDesc.longsCount()], objects);
        } else if (ifaceWrapperDesc != null) {
            // P3-M18: reinterpretation (*B)(unsafe.Pointer(p)) where B is a
            // single-field struct with an interface field (e.g. *reflect.rtype
            // over *abi.Type via toRType) and p points to an interface value:
            // wrap the interface in B's descriptor. The interface field holds
            // the GoInterface directly; layout matches gc (2 words).
            Object r = referent instanceof graalgo.values.GoBox gb ? gb.get() : referent;
            if (r instanceof graalgo.values.GoInterface gi && !gi.isNil()) {
                Object[] objects = new Object[ifaceWrapperDesc.objectsCount()];
                objects[ifaceWrapperFieldIndex] = gi;
                referent = new GoStruct(ifaceWrapperDesc,
                        new long[ifaceWrapperDesc.longsCount()], objects);
            }
        } else if (ifaceStructDesc != null) {
            // P3-M17: (*S)(unsafe.Pointer(&i)) where S is a two-field
            // (pointer, unsafe-pointer) struct and i is an interface value
            // (e.g. abi.EmptyInterface over any). The handle may resolve to
            // the variable's GoBox cell; unwrap to the interface value,
            // then materialize S from (descriptor, data).
            Object r = referent instanceof graalgo.values.GoBox gb ? gb.get() : referent;
            if (r instanceof graalgo.values.GoInterface gi && !gi.isNil()) {
                referent = graalgo.frontend.DerefNode.materializeInterfaceAsStruct(gi, ifaceStructDesc);
            }
        } else if (targetDesc != null) {
            Object raw = referent instanceof GoCell cell ? cell.get() : referent;
            // fix-redmaster-r27: extended-type reinterpretation first
            // (referent is a materialized abi.Type struct); slice-header
            // reinterpretation otherwise.
            GoStruct ext = tryExtendedType(raw);
            if (ext != null) {
                referent = ext;
            } else if (raw instanceof GoSlice || raw == null && referent instanceof GoCell) {
                // (*T)(unsafe.Pointer(&s)) where s is a slice variable
                // (or a nil slice): reinterpret the GoSlice as a header
                // struct in the target descriptor.
                GoStruct header = materializeSliceHeader((GoSlice) raw, targetDesc);
                if (header != null) {
                    referent = header;
                }
            }
        }
        return UnsafeTable.pointerFor(referent);
    }

    /**
     * Materializes a slice header struct from a slice value (null =
     * a nil slice variable's contents). Header words in order are
     * data, len, cap; field i of the target reads word i. Returns
     * null (fail closed) when the target has more fields than the
     * header has words, or a field that cannot hold its word.
     */
    private static GoStruct materializeSliceHeader(GoSlice slice, GoStructDescriptor desc) {
        if (desc.fields().size() > 3) {
            return null;
        }
        UnsafeTable table = UnsafeTable.current();
        long dataHandle = slice == null ? 0
                : table.intern(new UnsafeTable.ArrayRef(slice.array(), slice.offset()));
        long[] words = {dataHandle, slice == null ? 0 : slice.len(), slice == null ? 0 : slice.cap()};
        long[] longs = new long[desc.longsCount()];
        Object[] objects = new Object[desc.objectsCount()];
        for (int i = 0; i < desc.fields().size(); i++) {
            GoStructDescriptor.Field f = desc.fields().get(i);
            long word = words[i];
            if (f.inLongs()) {
                longs[f.index()] = word;
            } else if (f.kind() == GoKind.UNSAFE_POINTER) {
                // Unsafe.Pointer struct fields hold the handle as a Long
                // (null = nil, decoded to 0 on read).
                objects[f.index()] = word == 0 ? null : Long.valueOf(word);
            } else if (f.kind() == GoKind.POINTER && i == 0) {
                // *T data field: an element pointer at the slice's start.
                objects[f.index()] = slice == null ? new GoPointer((Object) null)
                        : UnsafeTable.pointerFor(new UnsafeTable.ArrayRef(slice.array(), slice.offset()));
            } else {
                return null; // cannot hold a header word: fail closed
            }
        }
        return new GoStruct(desc, longs, objects);
    }

    /**
     * Tries extended-type materialization (fix-redmaster-r27): when the
     * target matches an extended-type shape and the referent is a
     * materialized abi.Type struct, builds the full extended struct from
     * the type's TypeInfo. Returns null (fail closed) on any mismatch.
     */
    private GoStruct tryExtendedType(Object raw) {
        if (extShape == ExtShape.NONE || !(raw instanceof GoStruct gs)) {
            return null;
        }
        try {
            ReflectTypes.Table table = ReflectTypes.table();
            long tok = table.abiTypeTok(gs);
            if (tok <= 0) {
                return null;
            }
            ReflectTypes.TypeInfo ti = table.info(tok);
            GoStruct e;
            switch (extShape) {
                case STRUCT -> {
                    if (ti.kind != GoKind.STRUCT || ti.fields == null) {
                        return null;
                    }
                    e = buildStructTypeE(gs, ti);
                }
                case SLICE -> {
                    if (ti.kind != GoKind.SLICE || ti.elem == null) {
                        return null;
                    }
                    e = buildSliceTypeE(gs, ti);
                }
                case ARRAY -> {
                    if (ti.kind != GoKind.ARRAY || ti.elem == null) {
                        return null;
                    }
                    e = buildArrayTypeE(gs, ti);
                }
                default -> {
                    return null;
                }
            }
            if (e == null) {
                return null;
            }
            if (!extWrapper) {
                return e;
            }
            long[] longs = new long[targetDesc.longsCount()];
            Object[] objects = new Object[targetDesc.objectsCount()];
            for (var f : targetDesc.fields()) {
                if (!f.inLongs() && f.kind() == GoKind.STRUCT) {
                    objects[f.index()] = e;
                }
            }
            return new GoStruct(targetDesc, longs, objects);
        } catch (RuntimeException ex) {
            return null;
        }
    }

    /** Builds the extended struct for a struct type (fields 0,1,2 = Type, PkgPath, Fields). */
    private GoStruct buildStructTypeE(GoStruct abiType, ReflectTypes.TypeInfo ti) {
        GoStruct pkgName = graalgo.frontend.DerefNode.newAbiNameStruct(extNameDesc,
                ti.pkgPath == null || ti.pkgPath.isEmpty() ? null
                        : graalgo.frontend.DerefNode.encodeAbiName(ti.pkgPath, null, false, false));
        Object[] elems = new Object[ti.fields.size()];
        for (int i = 0; i < ti.fields.size(); i++) {
            ReflectTypes.FieldInfo fi = ti.fields.get(i);
            if (fi.type == null) {
                return null;
            }
            GoStruct nameStruct = graalgo.frontend.DerefNode.newAbiNameStruct(
                    extFieldDesc.fields().get(0).structDesc(),
                    graalgo.frontend.DerefNode.encodeAbiName(
                            fi.name, fi.tag, fi.isExported(), fi.anonymous));
            GoStruct fieldTyp = graalgo.frontend.DerefNode.materializeAbiTypeForTok(fi.type.tok);
            long[] flongs = new long[extFieldDesc.longsCount()];
            Object[] fobjects = new Object[extFieldDesc.objectsCount()];
            for (var f : extFieldDesc.fields()) {
                if (f.inLongs()) {
                    flongs[f.index()] = fi.offset;
                } else if (f.kind() == GoKind.STRUCT) {
                    fobjects[f.index()] = nameStruct;
                } else if (f.kind() == GoKind.POINTER) {
                    fobjects[f.index()] = new GoPointer(fieldTyp);
                } else {
                    return null;
                }
            }
            elems[i] = new GoStruct(extFieldDesc, flongs, fobjects);
        }
        GoSlice fieldsSlice = new GoSlice(elems, 0, elems.length, elems.length);
        long[] longs = new long[extDesc.longsCount()];
        Object[] objects = new Object[extDesc.objectsCount()];
        var ef = extDesc.fields();
        if (ef.size() != 3) {
            return null;
        }
        for (int i = 0; i < 3; i++) {
            var f = ef.get(i);
            if (f.inLongs()) {
                return null;
            }
            if (i == 0 && f.kind() == GoKind.STRUCT) {
                objects[f.index()] = abiType;
            } else if (i == 1 && f.kind() == GoKind.STRUCT) {
                objects[f.index()] = pkgName;
            } else if (i == 2 && f.kind() == GoKind.SLICE) {
                objects[f.index()] = fieldsSlice;
            } else {
                return null;
            }
        }
        return new GoStruct(extDesc, longs, objects);
    }

    /** Builds the extended struct for a slice type (fields 0,1 = Type, Elem). */
    private GoStruct buildSliceTypeE(GoStruct abiType, ReflectTypes.TypeInfo ti) {
        GoStruct elemTyp = graalgo.frontend.DerefNode.materializeAbiTypeForTok(ti.elem.tok);
        long[] longs = new long[extDesc.longsCount()];
        Object[] objects = new Object[extDesc.objectsCount()];
        var ef = extDesc.fields();
        if (ef.size() != 2) {
            return null;
        }
        for (int i = 0; i < 2; i++) {
            var f = ef.get(i);
            if (f.inLongs()) {
                return null;
            }
            if (i == 0 && f.kind() == GoKind.STRUCT) {
                objects[f.index()] = abiType;
            } else if (i == 1 && f.kind() == GoKind.POINTER) {
                objects[f.index()] = new GoPointer(elemTyp);
            } else {
                return null;
            }
        }
        return new GoStruct(extDesc, longs, objects);
    }

    /** Builds the extended struct for an array type (fields 0..3 = Type, Elem, Slice, Len). */
    private GoStruct buildArrayTypeE(GoStruct abiType, ReflectTypes.TypeInfo ti) {
        GoStruct elemTyp = graalgo.frontend.DerefNode.materializeAbiTypeForTok(ti.elem.tok);
        long sliceTok = ReflectTypes.sliceOf(ti.elem.tok);
        GoStruct sliceTyp = graalgo.frontend.DerefNode.materializeAbiTypeForTok(sliceTok);
        long[] longs = new long[extDesc.longsCount()];
        Object[] objects = new Object[extDesc.objectsCount()];
        var ef = extDesc.fields();
        if (ef.size() != 4) {
            return null;
        }
        for (int i = 0; i < 4; i++) {
            var f = ef.get(i);
            if (i == 3) {
                if (!f.inLongs()) {
                    return null;
                }
                longs[f.index()] = ti.arrayLen;
            } else if (f.inLongs()) {
                return null;
            } else if (i == 0 && f.kind() == GoKind.STRUCT) {
                objects[f.index()] = abiType;
            } else if (i == 1 && f.kind() == GoKind.POINTER) {
                objects[f.index()] = new GoPointer(elemTyp);
            } else if (i == 2 && f.kind() == GoKind.POINTER) {
                objects[f.index()] = new GoPointer(sliceTyp);
            } else {
                return null;
            }
        }
        return new GoStruct(extDesc, longs, objects);
    }

    /**
     * Builds the node from the {@code "unsafetooptr"} wire node. Fields:
     * {@code x} (the handle operand), {@code type} (result type-table id —
     * the {@code *T} type).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafetooptr");
        int typeId = UnsafeWire.requireTypeId(f, "unsafetooptr");
        WrapperInfo wi = wrapperInfo(ctx, typeId, "unsafetooptr.fields");
        IfaceWrapperInfo iwi = ifaceWrapperInfo(ctx, typeId, "unsafetooptr.fields");
        GoStructDescriptor ifaceDesc = ifaceStructInfo(ctx, typeId, "unsafetooptr.fields");
        GoStructDescriptor targetDesc = null;
        int targetElemId = -1;
        try {
            Json.Object entry = ctx.types.entry(typeId, "unsafetooptr.fields");
            if ("pointer".equals(entry.string("kind", "unsafetooptr.fields"))) {
                targetElemId = entry.object("detail", "unsafetooptr.fields")
                        .intValue("elem", "unsafetooptr.fields");
                targetDesc = NodeFactory.structDescriptorFor(ctx, targetElemId, "unsafetooptr.fields");
            }
        } catch (RuntimeException e) {
            targetDesc = null; // target is not a struct: no materialization
        }
        ExtInfo extInfo = targetDesc == null ? ExtInfo.NONE
                : analyzeExtShape(ctx, typeId, targetElemId, targetDesc, "unsafetooptr.fields");
        GoExprNode n = new UnsafeToPtrNode(
                UnsafeWire.child(f, "unsafetooptr", "x", factory, ctx),
                wi == null ? null : wi.desc,
                wi == null ? null : wi.wrappedTypeName,
                wi == null ? -1 : wi.wrappedFieldIndex,
                iwi == null ? null : iwi.desc,
                iwi == null ? -1 : iwi.fieldIndex,
                ifaceDesc,
                targetDesc,
                extInfo);
        n.setTypeId(typeId);
        return n;
    }

    /** Resolves named/alias chains to the underlying type-table id. */
    private static int resolveTypeId(NodeFactory.BuildCtx ctx, int typeId, String path) {
        for (int steps = 0; steps < 64; steps++) {
            Json.Object entry = ctx.types.entry(typeId, path);
            String k = entry.string("kind", path);
            if ("named".equals(k)) {
                typeId = entry.object("detail", path).intValue("underlying", path);
            } else if ("alias".equals(k)) {
                typeId = entry.object("detail", path).intValue("rhs", path);
            } else {
                return typeId;
            }
        }
        throw new FrontendException(path + ": type chain too long");
    }

    /** Field type ids of a struct type id, in declaration order (null when not a struct). */
    private static int[] structFieldTypeIdsOf(NodeFactory.BuildCtx ctx, int typeId, String path) {
        int resolved = resolveTypeId(ctx, typeId, path);
        Json.Object entry = ctx.types.entry(resolved, path);
        if (!"struct".equals(entry.string("kind", path))) {
            return null;
        }
        Json.Array fields = entry.object("detail", path).array("fields", path);
        int[] out = new int[fields.size()];
        for (int i = 0; i < fields.size(); i++) {
            Json.Value fv = fields.get(i);
            if (!(fv instanceof Json.Object fo)) {
                return null;
            }
            out[i] = fo.intValue("type", path);
        }
        return out;
    }

    /** True when a descriptor has exactly the abi.Type field layout (by field names/kinds). */
    private static boolean isAbiTypeLayout(GoStructDescriptor desc) {
        if (desc == null) {
            return false;
        }
        var fields = desc.fields();
        String[] names = {"Size_", "PtrBytes", "Hash", "TFlag", "Align_", "FieldAlign_",
                "Kind_", "Equal", "GCData", "Str", "PtrToThis"};
        if (fields.size() != names.length) {
            return false;
        }
        for (int i = 0; i < names.length; i++) {
            if (!names[i].equals(fields.get(i).name())) {
                return false;
            }
        }
        return true;
    }

    /** True when a descriptor is a Name shape: one POINTER field. */
    private static boolean isNameShape(GoStructDescriptor desc) {
        return desc != null && desc.fields().size() == 1
                && desc.fields().get(0).kind() == GoKind.POINTER;
    }

    /**
     * Analyzes the target for extended-type shapes (fix-redmaster-r27).
     * All decisions are structural (descriptor field layouts + type-table
     * walks); anything unrecognized yields ExtInfo.NONE (fail closed).
     */
    private static ExtInfo analyzeExtShape(NodeFactory.BuildCtx ctx, int pointerTypeId,
            int targetElemId, GoStructDescriptor targetDesc, String path) {
        try {
            // The extended struct E: either the target itself, or the
            // target's single STRUCT field (reflect's structType/sliceType/
            // arrayType wrappers around abi.StructType/SliceType/ArrayType).
            GoStructDescriptor eDesc;
            int eTypeId;
            boolean wrapper;
            if (targetDesc.fields().size() == 1
                    && targetDesc.fields().get(0).kind() == GoKind.STRUCT
                    && targetDesc.fields().get(0).structDesc() != null) {
                eDesc = targetDesc.fields().get(0).structDesc();
                int[] ftids = structFieldTypeIdsOf(ctx, targetElemId, path);
                if (ftids == null || ftids.length != 1) {
                    return ExtInfo.NONE;
                }
                eTypeId = ftids[0];
                wrapper = true;
            } else {
                eDesc = targetDesc;
                eTypeId = targetElemId;
                wrapper = false;
            }
            var ef = eDesc.fields();
            if (ef.isEmpty() || ef.get(0).kind() != GoKind.STRUCT
                    || !isAbiTypeLayout(ef.get(0).structDesc())) {
                return ExtInfo.NONE;
            }
            if (ef.size() == 3 && ef.get(1).kind() == GoKind.STRUCT
                    && isNameShape(ef.get(1).structDesc())
                    && ef.get(2).kind() == GoKind.SLICE) {
                // structType-shape: resolve the StructField descriptor
                // from the Fields slice's element type.
                int[] eFtids = structFieldTypeIdsOf(ctx, eTypeId, path);
                if (eFtids == null || eFtids.length != 3) {
                    return ExtInfo.NONE;
                }
                int sliceResolved = resolveTypeId(ctx, eFtids[2], path);
                Json.Object sliceEntry = ctx.types.entry(sliceResolved, path);
                if (!"slice".equals(sliceEntry.string("kind", path))) {
                    return ExtInfo.NONE;
                }
                int fieldElemId = sliceEntry.object("detail", path).intValue("elem", path);
                GoStructDescriptor fieldDesc =
                        NodeFactory.structDescriptorFor(ctx, fieldElemId, path);
                var ff = fieldDesc.fields();
                if (ff.size() != 3 || ff.get(0).kind() != GoKind.STRUCT
                        || !isNameShape(ff.get(0).structDesc())
                        || ff.get(1).kind() != GoKind.POINTER || !ff.get(2).inLongs()) {
                    return ExtInfo.NONE;
                }
                return new ExtInfo(ExtShape.STRUCT, wrapper, eDesc,
                        ef.get(1).structDesc(), fieldDesc);
            }
            if (ef.size() == 2 && ef.get(1).kind() == GoKind.POINTER) {
                return new ExtInfo(ExtShape.SLICE, wrapper, eDesc, null, null);
            }
            if (ef.size() == 4 && ef.get(1).kind() == GoKind.POINTER
                    && ef.get(2).kind() == GoKind.POINTER && ef.get(3).inLongs()) {
                return new ExtInfo(ExtShape.ARRAY, wrapper, eDesc, null, null);
            }
            return ExtInfo.NONE;
        } catch (RuntimeException e) {
            return ExtInfo.NONE;
        }
    }

    /** Build-time interface-wrapper detection; fail-closed (null) on any mismatch. */
    private record IfaceWrapperInfo(GoStructDescriptor desc, int fieldIndex) {}

    /** Build-time wrapper detection; fail-closed (null) on any mismatch. */
    private record WrapperInfo(GoStructDescriptor desc, String wrappedTypeName,
            int wrappedFieldIndex) {}

    private static WrapperInfo wrapperInfo(NodeFactory.BuildCtx ctx, int typeId, String path) {
        try {
            Json.Object entry = ctx.types.entry(typeId, path);
            if (!"pointer".equals(entry.string("kind", path))) {
                return null;
            }
            int elemId = entry.object("detail", path).intValue("elem", path);
            GoStructDescriptor desc;
            try {
                desc = NodeFactory.structDescriptorFor(ctx, elemId, path);
            } catch (FrontendException e) {
                return null; // target is not a struct
            }
            if (desc.fields().size() != 1) {
                return null;
            }
            var field = desc.fields().get(0);
            if (field.kind() != GoKind.STRUCT || field.inLongs()
                    || field.structDesc() == null) {
                return null;
            }
            return new WrapperInfo(desc, field.structDesc().typeName(),
                    field.index());
        } catch (RuntimeException e) {
            return null; // fail-closed: no wrapping
        }
    }

    /**
     * Detects a single-field struct target whose field is an interface
     * (e.g. {@code *reflect.rtype} with field {@code t abi.Type}).
     * Returns the descriptor and field index, or null fail-closed.
     */
    private static IfaceWrapperInfo ifaceWrapperInfo(NodeFactory.BuildCtx ctx, int typeId, String path) {
        try {
            Json.Object entry = ctx.types.entry(typeId, path);
            if (!"pointer".equals(entry.string("kind", path))) {
                return null;
            }
            int elemId = entry.object("detail", path).intValue("elem", path);
            GoStructDescriptor desc;
            try {
                desc = NodeFactory.structDescriptorFor(ctx, elemId, path);
            } catch (FrontendException e) {
                return null; // target is not a struct
            }
            if (desc.fields().size() != 1) {
                return null;
            }
            var field = desc.fields().get(0);
            // Interface-kind field lives in objects array (not longs).
            if (field.kind() != GoKind.INTERFACE || field.inLongs()) {
                return null;
            }
            return new IfaceWrapperInfo(desc, field.index());
        } catch (RuntimeException e) {
            return null; // fail-closed: no wrapping
        }
    }

    /**
     * Detects a two-field (POINTER, UNSAFE_POINTER) struct target (e.g.
     * {@code *abi.EmptyInterface}). Returns the struct descriptor, or null.
     * Fail-closed on any mismatch.
     */
    private static GoStructDescriptor ifaceStructInfo(NodeFactory.BuildCtx ctx, int typeId, String path) {
        try {
            Json.Object entry = ctx.types.entry(typeId, path);
            if (!"pointer".equals(entry.string("kind", path))) {
                return null;
            }
            int elemId = entry.object("detail", path).intValue("elem", path);
            GoStructDescriptor desc;
            try {
                desc = NodeFactory.structDescriptorFor(ctx, elemId, path);
            } catch (FrontendException e) {
                return null; // target is not a struct
            }
            if (desc.fields().size() != 2) {
                return null;
            }
            var f0 = desc.fields().get(0);
            var f1 = desc.fields().get(1);
            if (f0.kind() != GoKind.POINTER || f1.kind() != GoKind.UNSAFE_POINTER) {
                return null;
            }
            return desc;
        } catch (RuntimeException e) {
            return null; // fail-closed
        }
    }
}
