package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.GoTypeStrings;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.p3reflect.ReflectTypes;
import graalgo.values.AbiTypeRef;
import graalgo.values.EfaceRef;
import graalgo.values.GoCell;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;

import java.util.HashMap;
import java.util.Map;

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
 * <p>P3-M10 (eface): two unsafe-reinterpretation idioms from
 * {@code internal/abi} (real GOROOT, used by {@code internal/reflectlite}
 * and hence by every {@code import "os"} via the synthetic errors
 * package's {@code errorType} initializer) need structural support:
 * <ul>
 *   <li>{@code *(*abi.EmptyInterface)(unsafe.Pointer(&x))} where {@code x}
 *       is an interface value — synthesizes the eface view
 *       ({@link EfaceRef}): field 0 ({@code Type}) is the {@code *abi.Type}
 *       for the interface's dynamic type, field 1 ({@code Data}) the data
 *       word as {@code unsafe.Pointer}.</li>
 *   <li>{@code (*abi.X)(unsafe.Pointer(p))} for {@code X} in
 *       {Type, PtrType} where {@code p} is a type token — wraps the
 *       token as an {@link AbiTypeRef}, so {@code unsafe.Pointer} of it
 *       yields the token back and struct-field selection ({@code Kind_},
 *       {@code Elem}) resolves from the type table.</li>
 * </ul>
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 */
public final class UnsafeToPtrNode extends GoExprNode {
    @Child private GoExprNode child;

    /** How to interpret the handle operand. */
    private enum Mode {
        /** Normal: resolve the handle, wrap the referent. */
        NORMAL,
        /** Target is *abi.EmptyInterface: synthesize the eface view. */
        EFACE,
        /** Target is *abi.X: the operand is a type token. */
        ABI_VIEW,
    }

    private final Mode mode;
    /** For ABI_VIEW: the abi struct name, e.g. "internal/abi.PtrType". */
    private final String abiView;

    private UnsafeToPtrNode(GoExprNode child, Mode mode, String abiView) {
        super(GoKind.POINTER);
        this.child = child;
        this.mode = mode;
        this.abiView = abiView;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        long handle = child.executeLong(frame);
        if (mode == Mode.EFACE) {
            return efaceView(handle);
        }
        if (mode == Mode.ABI_VIEW) {
            return abiView(handle);
        }
        return UnsafeTable.pointerFor(UnsafeTable.current().resolve(handle));
    }

    /**
     * Synthesizes {@code *abi.EmptyInterface} over an interface value's
     * storage. The handle was interned from {@code &x}; resolve it and,
     * when it addresses an interface, build the view. Anything else falls
     * back to the normal wrap (a non-interface reinterpreted as
     * EmptyInterface is outside the supported idiom — fail-loud downstream
     * rather than silently).
     */
    private static Object efaceView(long handle) {
        if (handle == 0) {
            return null; // nil *EmptyInterface
        }
        Object referent = UnsafeTable.current().resolve(handle);
        Object iface = referent;
        if (iface instanceof GoCell cell) {
            iface = cell.get();
        }
        if (!(iface instanceof GoInterface gi)) {
            return UnsafeTable.pointerFor(referent);
        }
        if (gi.isNil()) {
            // Nil interface viewed as EmptyInterface: Type is nil *Type.
            return new GoPointer(new EfaceRef(null, 0L));
        }
        long tok = ReflectTypes.tokForDescriptor(gi.descriptor());
        Object data = gi.data();
        long dataHandle = data == null ? 0L : UnsafeTable.current().intern(data);
        AbiTypeRef typeRef = abiTypeRef(tok, "internal/abi.Type");
        return new GoPointer(new EfaceRef(typeRef, dataHandle));
    }

    /**
     * Wraps a type token as {@code *abi.X}. The operand of
     * {@code (*abi.X)(unsafe.Pointer(p))} is the canonical token (see
     * {@link UnsafeFromPtrNode}); re-wrap it so field selection and
     * {@code unsafe.Pointer} round-trip.
     */
    private Object abiView(long handle) {
        if (handle == 0) {
            return null; // nil *abi.X
        }
        return new GoPointer(abiTypeRef(handle, abiView));
    }

    /**
     * Builds the {@link AbiTypeRef} for a token under an abi struct view,
     * precomputing the struct fields the abi code reads:
     * <ul>
     *   <li>{@code internal/abi.Type}: field 6 ({@code Kind_}) → Kind ordinal.</li>
     *   <li>{@code internal/abi.PtrType}: field 1 ({@code Elem}) → *abi.Type of the element.</li>
     * </ul>
     */
    static AbiTypeRef abiTypeRef(long tok, String view) {
        Map<Integer, Object> fields = new HashMap<>();
        ReflectTypes.TypeInfo info = ReflectTypes.table().info(tok);
        switch (view) {
            case "internal/abi.Type" -> {
                // abi.Type.Kind_ is field index 6 (Size_, PtrBytes, Hash,
                // TFlag, Align_, FieldAlign_, Kind_, ...).
                fields.put(6, (long) ReflectTypes.kindOrdinal(info));
            }
            case "internal/abi.PtrType" -> {
                // abi.PtrType{Type, Elem}: Elem is field index 1.
                ReflectTypes.TypeInfo elem = info.elem;
                if (elem == null) {
                    throw new IllegalStateException(
                            "abi.PtrType view of non-pointer token " + tok);
                }
                fields.put(1, new GoPointer(abiTypeRef(elem.tok, "internal/abi.Type")));
            }
            default -> throw new IllegalStateException(
                    "unsupported abi view: " + view);
        }
        return new AbiTypeRef(tok, view, fields);
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
                modeFor(ctx, typeId),
                viewFor(ctx, typeId));
        n.setTypeId(typeId);
        return n;
    }

    private static Mode modeFor(NodeFactory.BuildCtx ctx, int typeId) {
        String name = GoTypeStrings.of(ctx, typeId);
        if ("*internal/abi.EmptyInterface".equals(name)) {
            return Mode.EFACE;
        }
        if (name.startsWith("*internal/abi.")) {
            return Mode.ABI_VIEW;
        }
        return Mode.NORMAL;
    }

    private static String viewFor(NodeFactory.BuildCtx ctx, int typeId) {
        String name = GoTypeStrings.of(ctx, typeId);
        if (name.startsWith("*internal/abi.")) {
            return name.substring(1); // "*internal/abi.PtrType" -> "internal/abi.PtrType"
        }
        return "";
    }
}
