package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
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

    private UnsafeToPtrNode(GoExprNode child, GoStructDescriptor wrapperDesc,
            String wrappedTypeName, int wrappedFieldIndex,
            GoStructDescriptor ifaceWrapperDesc, int ifaceWrapperFieldIndex,
            GoStructDescriptor ifaceStructDesc) {
        super(GoKind.POINTER);
        this.child = child;
        this.wrapperDesc = wrapperDesc;
        this.wrappedTypeName = wrappedTypeName;
        this.wrappedFieldIndex = wrappedFieldIndex;
        this.ifaceWrapperDesc = ifaceWrapperDesc;
        this.ifaceWrapperFieldIndex = ifaceWrapperFieldIndex;
        this.ifaceStructDesc = ifaceStructDesc;
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
        }
        return UnsafeTable.pointerFor(referent);
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
        GoExprNode n = new UnsafeToPtrNode(
                UnsafeWire.child(f, "unsafetooptr", "x", factory, ctx),
                wi == null ? null : wi.desc,
                wi == null ? null : wi.wrappedTypeName,
                wi == null ? -1 : wi.wrappedFieldIndex,
                iwi == null ? null : iwi.desc,
                iwi == null ? -1 : iwi.fieldIndex,
                ifaceDesc);
        n.setTypeId(typeId);
        return n;
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
