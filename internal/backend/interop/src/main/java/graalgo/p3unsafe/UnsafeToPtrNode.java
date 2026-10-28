package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.p3reflect.AbiTypes;
import graalgo.values.GoCell;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;

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
 */
public final class UnsafeToPtrNode extends GoExprNode {
    @Child private GoExprNode child;

    /** Qualified name of the target struct, or null when no view applies. */
    private final String targetQName;

    /** Field layout for the view, or null when no view applies. */
    private final List<AbiTypes.ViewField> viewLayout;

    private UnsafeToPtrNode(GoExprNode child, String targetQName,
            List<AbiTypes.ViewField> viewLayout) {
        super(GoKind.POINTER);
        this.child = child;
        this.targetQName = targetQName;
        this.viewLayout = viewLayout;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        long handle = child.executeLong(frame);
        Object resolved = UnsafeTable.current().resolve(handle);
        if (viewLayout != null && targetQName != null
                && resolved instanceof GoCell cell
                && cell.get() instanceof GoInterface gi) {
            GoStruct view = AbiTypes.buildInterfaceView(gi, targetQName, viewLayout);
            if (view != null) {
                return new GoPointer(view);
            }
        }
        return UnsafeTable.pointerFor(resolved);
    }

    /**
     * Builds the node from the {@code "unsafetooptr"} wire node. Fields:
     * {@code x} (the handle operand), {@code type} (result type-table id —
     * the {@code *T} type).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafetooptr");
        int typeId = UnsafeWire.requireTypeId(f, "unsafetooptr");
        ViewTarget target = captureViewTarget(ctx, typeId);
        GoExprNode n = new UnsafeToPtrNode(
                UnsafeWire.child(f, "unsafetooptr", "x", factory, ctx),
                target == null ? null : target.qname(),
                target == null ? null : target.layout());
        n.setTypeId(typeId);
        return n;
    }

    /** A viewable target: its qualified name plus its field layout. */
    private record ViewTarget(String qname, List<AbiTypes.ViewField> layout) {}

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
