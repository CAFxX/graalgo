package graalgo.p3reflect;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * The {@code reflect.unpackEface} intrinsic (fix-redmaster-unpackeface RC-4a):
 * the {@code "reflectunpack"} wire node.
 *
 * <p>gc implements {@code unpackEface} via unsafe interface reinterpretation
 * ({@code *(*abi.EmptyInterface)(unsafe.Pointer(&i))}), which relies on the
 * memory layout of interfaces. graalgo represents interfaces as high-level
 * {@link GoInterface} objects, so the reinterpretation produces a broken
 * value and the subsequent field selection ({@code e.Type}) fails with
 * {@code ClassCastException}. This intrinsic implements {@code unpackEface}
 * directly: given an {@code any} value, it builds the corresponding
 * {@code reflect.Value} struct from the {@link GoInterface}'s type and data.
 *
 * <p>Fields: {@code "arg"} (the {@code any} value), {@code "type"} (result
 * {@code reflect.Value} type-table id).
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1 change.
 */
public final class ReflectUnpackEfaceNode extends GoExprNode {
    @Child private GoExprNode arg;

    private ReflectUnpackEfaceNode(GoExprNode arg) {
        super(GoKind.STRUCT);
        this.arg = arg;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object v = arg.executeObject(frame);
        GoStructDescriptor desc = ReflectTypes.valueDescriptor();

        // Unwrap the interface to get the dynamic type and data.
        GoInterface gi;
        if (v instanceof GoInterface g) {
            gi = g;
        } else {
            // Non-interface value: shouldn't happen (unpackEface takes any),
            // but handle gracefully by returning zero Value.
            return ReflectValues.zero(desc);
        }
        if (gi.isNil()) {
            // Nil interface: unpackEface returns zero Value.
            return ReflectValues.zero(desc);
        }

        // Get the type token and materialize the *abi.Type for the typ_ field.
        long tok = ReflectValues.typeof(gi);
        if (tok == 0) {
            return ReflectValues.zero(desc);
        }
        GoStruct abiType = ReflectTypes.materialize(tok);
        GoPointer typPtr = new GoPointer(abiType);

        // Get the data for the ptr field. Intern it to get an unsafe.Pointer
        // handle (long).
        Object data = gi.data();
        long ptrHandle = graalgo.p3unsafe.UnsafeTable.current().intern(data);

        // Compute the flag: kind in low 5 bits from the materialized abi.Type's
        // Kind_ field, flagIndir if not direct.
        // Note: we read Kind from the materialized struct rather than mapping
        // from GoKind, because GoKind doesn't distinguish int vs int64 (both
        // are INT64, but reflect.Int=2 vs reflect.Int64=6).
        int kindBits = readAbiKind(abiType);
        long flag = kindBits;
        ReflectTypes.TypeInfo ti = ReflectTypes.table().info(tok);
        if (!isDirectIface(ti)) {
            flag |= ReflectValues.FLAG_INDIR;
        }

        // Build the reflect.Value struct: {typ_, ptr, flag}.
        return buildValueStruct(desc, typPtr, ptrHandle, flag);
    }

    /**
     * Reads the Kind value from a materialized abi.Type GoStruct.
     * The Kind_ field is in the longs carrier.
     */
    private static int readAbiKind(GoStruct abiType) {
        try {
            GoStructDescriptor desc = abiType.descriptor();
            for (GoStructDescriptor.Field f : desc.fields()) {
                if (f.name().equals("Kind_") && f.inLongs()) {
                    return (int) abiType.getLong(f.index());
                }
            }
        } catch (Exception e) {
            // Fall through to default
        }
        return 0;
    }

    /**
     * Maps GoKind to abi Kind value (mirrors ReflectTypes.abiKind).
     * @deprecated Use readAbiKind instead for accuracy.
     */
    @Deprecated
    private static int abiKind(GoKind kind) {
        return switch (kind) {
            case BOOL -> 1;
            case INT8 -> 3;
            case INT16 -> 4;
            case INT32 -> 5;
            case INT64 -> 6;
            case UINT8 -> 8;
            case UINT16 -> 9;
            case UINT32 -> 10;
            case UINT64 -> 11;
            case FLOAT32 -> 13;
            case FLOAT64 -> 14;
            case COMPLEX64 -> 15;
            case COMPLEX128 -> 16;
            case ARRAY -> 17;
            case CHAN -> 18;
            case FUNC -> 19;
            case INTERFACE -> 20;
            case MAP -> 21;
            case POINTER -> 22;
            case SLICE -> 23;
            case STRING -> 24;
            case STRUCT -> 25;
            case UNSAFE_POINTER -> 26;
            default -> 0;
        };
    }

    /**
     * Reports whether values of this type are stored directly in the
     * interface data word (no indirection). Reference types (pointer, map,
     * chan, func, unsafe.Pointer) are direct; value types are indirect.
     */
    private static boolean isDirectIface(ReflectTypes.TypeInfo ti) {
        return switch (ti.kind) {
            case POINTER, MAP, CHAN, FUNC, UNSAFE_POINTER -> true;
            default -> false;
        };
    }

    /**
     * Builds the reflect.Value GoStruct with the given field values.
     * Uses the descriptor to place fields correctly.
     *
     * <p>reflect.Value has fields: typ_ (*abi.Type, pointer -> objects),
     * ptr (unsafe.Pointer -> objects, NOT longs!), flag (uintptr -> longs).
     * This matches the layout used by ReflectValues.pack.
     */
    private static GoStruct buildValueStruct(GoStructDescriptor desc, GoPointer typPtr,
            long ptrHandle, long flag) {
        // ptr is unsafe.Pointer which goes in objects carrier (not longs),
        // boxed as Long. typ_ is a GoPointer in objects. flag is in longs.
        return new GoStruct(desc, new long[]{flag}, new Object[]{typPtr, ptrHandle});
    }

    /**
     * Builds the node from the {@code "reflectunpack"} wire node. Fields:
     * {@code arg} (expr node), {@code type} (result type-table id).
     */
    public static GoExprNode build(Json.Object node, NodeFactory factory,
            NodeFactory.BuildCtx ctx) {
        Json.Object f = node.object("fields", "reflectunpack");
        String path = "reflectunpack.fields";
        Json.Value av = f.opt("arg");
        if (!(av instanceof Json.Object ao)) {
            throw new FrontendException(path + ": reflectunpack needs \"arg\"");
        }
        GoExprNode argNode = factory.buildExpr(ao, ctx);
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": reflectunpack needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        GoExprNode n = new ReflectUnpackEfaceNode(argNode);
        n.setTypeId(typeId);
        return n;
    }
}
