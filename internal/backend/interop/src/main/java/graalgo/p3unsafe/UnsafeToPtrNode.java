package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;

/**
 * {@code unsafe.Pointer → *T} (P3-M8): the {@code "unsafetooptr"} wire
 * node. Resolves the handle and wraps the referent via
 * {@link UnsafeTable#pointerFor}: a cell handle becomes a cell pointer
 * (reads/writes hit the cell), an element handle an element pointer, any
 * other handle a plain pointer — so the round trip
 * {@code (*T)(unsafe.Pointer(p))} addresses the same storage {@code p}
 * does (P3-M10; previously the resolved carrier/value leaked through
 * dereference).
 */
public final class UnsafeToPtrNode extends GoExprNode {
    @Child private GoExprNode child;
    /**
     * Struct descriptor for the pointee type T (when {@code *T} points to a
     * struct). Used to synthesize header structs for unsafe reinterpretation
     * (e.g. {@code (*unsafeheader.String)(v.ptr)} where the handle resolves
     * to a GoString). Null if T is not a struct or descriptor unavailable.
     */
    private final graalgo.values.GoStructDescriptor pointeeDesc;

    private UnsafeToPtrNode(GoExprNode child) {
        this(child, null);
    }

    private UnsafeToPtrNode(GoExprNode child,
            graalgo.values.GoStructDescriptor pointeeDesc) {
        super(GoKind.POINTER);
        this.child = child;
        this.pointeeDesc = pointeeDesc;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        long handle = child.executeLong(frame);
        Object referent = UnsafeTable.current().resolve(handle);
        // Unsafe reinterpretation: if the handle resolves to a GoString or
        // GoSlice and the target is a header-like struct, synthesize the
        // header from the value's shape (e.g. (*unsafeheader.String)(v.ptr)).
        // This is shape-based, not name-keyed: any 2-field [ptr,int] or
        // 3-field [ptr,int,int] struct qualifies.
        if (pointeeDesc != null
                && (referent instanceof graalgo.values.GoString
                    || referent instanceof graalgo.values.GoSlice)) {
            graalgo.values.GoStruct synthesized =
                    trySynthesizeHeader(pointeeDesc, referent);
            if (synthesized != null) {
                return new GoPointer(synthesized);
            }
        }
        return UnsafeTable.pointerFor(referent);
    }

    /**
     * Attempts to synthesize a header struct from a GoString or GoSlice.
     * Returns null if the descriptor shape doesn't match a known header
     * pattern (caller falls back to pointerFor).
     */
    private static graalgo.values.GoStruct trySynthesizeHeader(
            graalgo.values.GoStructDescriptor desc, Object value) {
        var fields = desc.fields();
        if (value instanceof graalgo.values.GoString gs) {
            byte[] bytes = gs.bytes();
            int len = gs.length();
            long dataHandle = UnsafeTable.current().intern(bytes);
            if (fields.size() == 2
                    && isPtrLike(fields.get(0).kind())
                    && fields.get(1).kind().isIntKind()) {
                return buildHeaderStruct(desc, dataHandle, len, len);
            }
            if (fields.size() == 3
                    && isPtrLike(fields.get(0).kind())
                    && fields.get(1).kind().isIntKind()
                    && fields.get(2).kind().isIntKind()) {
                return buildHeaderStruct(desc, dataHandle, len, len);
            }
        }
        if (value instanceof graalgo.values.GoSlice gsl) {
            if (fields.size() == 3
                    && isPtrLike(fields.get(0).kind())
                    && fields.get(1).kind().isIntKind()
                    && fields.get(2).kind().isIntKind()) {
                long dataHandle = UnsafeTable.current().intern(gsl.array());
                return buildHeaderStruct(desc, dataHandle, gsl.len(), gsl.cap());
            }
        }
        return null;
    }

    /** Returns true for pointer-like kinds (POINTER, UNSAFE_POINTER). */
    private static boolean isPtrLike(GoKind kind) {
        return kind == GoKind.POINTER || kind == GoKind.UNSAFE_POINTER;
    }

    /**
     * Builds a header struct with the given Data handle, Len, and Cap.
     * Fields are placed per the descriptor.
     */
    private static graalgo.values.GoStruct buildHeaderStruct(
            graalgo.values.GoStructDescriptor desc,
            long dataHandle, long len, long cap) {
        long[] longs = new long[desc.longsCount()];
        Object[] objects = new Object[desc.objectsCount()];
        var fields = desc.fields();
        for (int i = 0; i < fields.size(); i++) {
            var f = fields.get(i);
            long lv = (i == 0) ? dataHandle : (i == 1) ? len : cap;
            Object v = Long.valueOf(lv);
            if (f.inLongs()) {
                longs[f.index()] = lv;
            } else {
                objects[f.index()] = v;
            }
        }
        return new graalgo.values.GoStruct(desc, longs, objects);
    }

    /**
     * Builds the node from the {@code "unsafetooptr"} wire node. Fields:
     * {@code x} (the handle operand), {@code type} (result type-table id —
     * the {@code *T} type).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory, NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "unsafetooptr");
        int typeId = UnsafeWire.requireTypeId(f, "unsafetooptr");
        GoExprNode n = new UnsafeToPtrNode(
                UnsafeWire.child(f, "unsafetooptr", "x", factory, ctx),
                pointeeDescriptorFor(ctx, typeId, "unsafetooptr"));
        n.setTypeId(typeId);
        return n;
    }

    /**
     * Looks up the struct descriptor for the pointee type T of {@code *T}.
     * Returns null if T is not a struct or the descriptor is unavailable
     * (caller falls back to plain pointer wrapping).
     */
    private static graalgo.values.GoStructDescriptor pointeeDescriptorFor(
            NodeFactory.BuildCtx ctx, int ptrTypeId, String path) {
        try {
            Json.Object entry = ctx.types.entry(ptrTypeId, path);
            String kind = entry.string("kind", path);
            if (!"pointer".equals(kind)) {
                return null;
            }
            Json.Object detail = (Json.Object) entry.object("detail", path);
            int elemTypeId = detail.intValue("elem", path);
            return NodeFactory.structDescriptorFor(ctx, elemTypeId, path);
        } catch (Exception e) {
            // Not a struct pointee or descriptor unavailable; the runtime
            // synthesis is skipped and plain pointer wrapping is used.
            return null;
        }
    }
}
