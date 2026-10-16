package graalgo.p3reflect;

import com.oracle.truffle.api.frame.FrameSlotKind;
import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.frontend.TypeTable;
import graalgo.p3reflect.ReflectRefs.Cell;
import graalgo.p3reflect.ReflectTypes.TypeInfo;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import java.util.ArrayList;
import java.util.List;

/**
 * The {@code "reflectop"} wire node (P3-M8, wave 2, reflect package).
 *
 * <p>One node kind dispatches on {@code "op"} (the intrinsic name without
 * the {@code __reflect_} prefix) instead of ~60 node classes. Fields:
 * {@code "op"} (string), {@code "args"} (array of expr nodes),
 * {@code "type"} (result type-table id, for boxing).
 *
 * <p>Value results are packed as {@link GoStruct}s using the descriptors
 * captured at install (built from the wire via
 * {@link GoStructDescriptor#layout}, identical to NodeFactory's).
 */
public final class ReflectOpNode extends GoExprNode {
    private final String op;
    @Children
    private final GoExprNode[] args;

    private ReflectOpNode(String op, GoExprNode[] args, GoKind kind) {
        super(kind);
        this.op = op;
        this.args = args;
    }

    /**
     * Builds the node from the {@code "reflectop"} wire node. Fields:
     * {@code op}: string, {@code args}: array of expr nodes,
     * {@code type}: result type-table id.
     *
     * <p>P3-M8b: the node's kind is computed from the result type id —
     * different intrinsics return different kinds (typeof → unsafe.Pointer,
     * valueof → Value struct, etc.). The old hardcoded INTERFACE kind broke
     * any use in a non-interface context.
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "reflectop");
        String path = "reflectop.fields";
        String op = f.string("op", path);
        Json.Array argsArr = f.array("args", path);
        GoExprNode[] args = new GoExprNode[argsArr.size()];
        for (int i = 0; i < argsArr.size(); i++) {
            Json.Value av = argsArr.get(i);
            if (!(av instanceof Json.Object ao)) {
                throw new FrontendException(path + ": arg " + i + " must be an object");
            }
            args[i] = factory.buildExpr(ao, ctx);
        }
        // P3-M8b: the type field may be null for void intrinsics (the Go
        // exporter sets it to nil when callType has no result). Default to
        // INTERFACE for those — the result is discarded.
        GoKind kind = GoKind.INTERFACE;
        int typeId = -1;
        Json.Value t = f.opt("type");
        if (t instanceof Json.Num n) {
            typeId = (int) n.longValue();
            kind = NodeFactory.kindOf(ctx, typeId, path);
        }
        GoExprNode n = new ReflectOpNode(op, args, kind);
        if (typeId >= 0) {
            n.setTypeId(typeId);
        }
        return n;
    }

    /**
     * Evaluates args and dispatches. Each argument is read at its own
     * kind's slot kind and boxed (Long for long kinds, etc.) — args of
     * primitive kind (e.g. {@code unsafe.Pointer} tokens are Long) live
     * in primitive frame slots, so a blind {@code executeObject} throws
     * {@code FrameSlotTypeException}.
     */
    private Object dispatchBoxed(VirtualFrame frame) {
        Object[] argv = new Object[args.length];
        for (int i = 0; i < args.length; i++) {
            argv[i] = boxArg(args[i], frame);
        }
        return dispatch(op, argv);
    }

    /**
     * Reads an argument at its kind's slot kind (replica of the
     * package-private {@code Boxing.box}, same shape as
     * {@code IfaceDispatchNode.boxArg}: canonical boxed values, e.g.
     * long slot kinds as {@link Long}).
     */
    private static Object boxArg(GoExprNode arg, VirtualFrame frame) {
        FrameSlotKind slot = arg.kind().slotKind();
        if (slot == FrameSlotKind.Int) {
            return Integer.valueOf(arg.executeInt(frame));
        }
        if (slot == FrameSlotKind.Long) {
            return Long.valueOf(arg.executeLong(frame));
        }
        if (slot == FrameSlotKind.Float) {
            return Float.valueOf(arg.executeFloat(frame));
        }
        if (slot == FrameSlotKind.Double) {
            return Double.valueOf(arg.executeDouble(frame));
        }
        if (slot == FrameSlotKind.Boolean) {
            return Boolean.valueOf(arg.executeBoolean(frame));
        }
        if (slot == FrameSlotKind.Object) {
            return arg.executeObject(frame);
        }
        throw new FrontendException("cannot box kind " + arg.kind() + " (WS E)");
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return dispatchBoxed(frame);
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        return ((Number) dispatchBoxed(frame)).longValue();
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        return ((Number) dispatchBoxed(frame)).intValue();
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        return ((Number) dispatchBoxed(frame)).doubleValue();
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        return ((Number) dispatchBoxed(frame)).floatValue();
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return (Boolean) dispatchBoxed(frame);
    }

    // ------------------------------------------------------------------
    // Argument coercions.
    // ------------------------------------------------------------------

    private static long asTok(Object o) {
        if (o instanceof Long l) {
            return l;
        }
        if (o instanceof Integer n) {
            return n.longValue();
        }
        throw new IllegalStateException("reflect: expected type token, got " + o);
    }

    private static GoStruct asValue(Object o) {
        if (!(o instanceof GoStruct s)) {
            throw new IllegalStateException("reflect: expected Value, got " + o);
        }
        return s;
    }

    private static long asLong(Object o) {
        return ((Number) o).longValue();
    }

    private static int asInt(Object o) {
        return ((Number) o).intValue();
    }

    private static boolean asBool(Object o) {
        return (Boolean) o;
    }

    private static String asString(Object o) {
        if (o instanceof GoString gs) {
            return gs.toString();
        }
        return (String) o;
    }

    private static GoSlice asSlice(Object o) {
        return (GoSlice) o;
    }

    // ------------------------------------------------------------------
    // Slice-argument coercions (FuncOf/StructOf take []Type-style args).
    // A nil Go slice arrives as Java null (see unboxArgs); it coerces to
    // an empty array.
    // ------------------------------------------------------------------

    /** Raw elements of a GoSlice argument (boxed). */
    private static Object[] sliceElements(GoSlice s) {
        if (s == null) {
            return new Object[0];
        }
        int n = s.len();
        Object arr = s.array();
        int off = s.offset();
        Object[] out = new Object[n];
        // The backing array is typed by element kind (long[] for
        // []unsafe.Pointer, boolean[] for []bool, Object[] for refs);
        // Array.get boxes primitives uniformly.
        for (int i = 0; i < n; i++) {
            out[i] = java.lang.reflect.Array.get(arr, off + i);
        }
        return out;
    }

    /** []unsafe.Pointer (type tokens) argument as long[]. */
    private static long[] toTokArray(GoSlice s) {
        Object[] els = sliceElements(s);
        long[] out = new long[els.length];
        for (int i = 0; i < els.length; i++) {
            out[i] = ((Number) els[i]).longValue();
        }
        return out;
    }

    /** []string argument as String[]. */
    private static String[] toStringArray(GoSlice s) {
        Object[] els = sliceElements(s);
        String[] out = new String[els.length];
        for (int i = 0; i < els.length; i++) {
            out[i] = asString(els[i]);
        }
        return out;
    }

    /** []bool argument as boolean[]. */
    private static boolean[] toBoolArray(GoSlice s) {
        Object[] els = sliceElements(s);
        boolean[] out = new boolean[els.length];
        for (int i = 0; i < els.length; i++) {
            out[i] = (Boolean) els[i];
        }
        return out;
    }

    // ------------------------------------------------------------------
    // Dispatch.
    // ------------------------------------------------------------------

    private static Object dispatch(String op, Object[] argv) {
        return switch (op) {
            case "tKind" -> (long) ReflectTypes.kindOf(asTok(argv[0]));
            case "tString" -> GoString.fromUtf8(ReflectTypes.typeString(asTok(argv[0])));
            case "tName" -> GoString.fromUtf8(simpleName(asTok(argv[0])));
            case "tPkgPath" -> GoString.fromUtf8(ReflectTypes.table().info(asTok(argv[0])).pkgPath);
            case "tSize" -> ReflectTypes.table().info(asTok(argv[0])).size;
            case "tAlign" -> (long) ReflectTypes.table().info(asTok(argv[0])).align;
            case "tFieldAlign" -> (long) ReflectTypes.table().info(asTok(argv[0])).align;
            case "tBits" -> (long) bitsOf(asTok(argv[0]));
            case "tChanDir" -> (long) chanDirOrdinal(asTok(argv[0]));
            case "tLen" -> (long) ReflectTypes.table().info(asTok(argv[0])).arrayLen;
            case "tNumField" -> (long) ReflectTypes.table().info(asTok(argv[0])).fields.size();
            case "tNumMethod" -> (long) ReflectTypes.table().info(asTok(argv[0])).methods.size();
            case "tNumIn" -> (long) ReflectTypes.table().info(asTok(argv[0])).ins.size();
            case "tNumOut" -> (long) ReflectTypes.table().info(asTok(argv[0])).outs.size();
            case "tIsVariadic" -> ReflectTypes.table().info(asTok(argv[0])).variadic;
            case "tComparable" -> comparable(asTok(argv[0]));
            case "tField" -> structField(asTok(argv[0]), asInt(argv[1]));
            case "tMethod" -> method(asTok(argv[0]), asInt(argv[1]));
            case "tElem" -> ReflectTypes.table().info(asTok(argv[0])).elem.tok;
            case "tKey" -> ReflectTypes.table().info(asTok(argv[0])).key.tok;
            case "tIn" -> ReflectTypes.table().info(asTok(argv[0])).ins.get(asInt(argv[1])).tok;
            case "tOut" -> ReflectTypes.table().info(asTok(argv[0])).outs.get(asInt(argv[1])).tok;
            case "tImplements" -> ReflectTypes.implementsType(asTok(argv[0]), asTok(argv[1]));
            case "tAssignableTo" -> ReflectTypes.assignableTo(asTok(argv[0]), asTok(argv[1]));
            case "tConvertibleTo" -> ReflectTypes.convertibleTo(asTok(argv[0]), asTok(argv[1]));
            case "tPtrTo" -> ReflectTypes.ptrTo(asTok(argv[0]));
            case "tArrayOf" -> ReflectTypes.arrayOf(asTok(argv[1]), asInt(argv[0]));
            case "tSliceOf" -> ReflectTypes.sliceOf(asTok(argv[0]));
            case "tMapOf" -> ReflectTypes.mapOf(asTok(argv[0]), asTok(argv[1]));
            case "tChanOf" -> ReflectTypes.chanOf(asTok(argv[1]), asInt(argv[0]));
            case "tFuncOf" -> ReflectTypes.funcOf(toTokArray(asSlice(argv[0])),
                    toTokArray(asSlice(argv[1])), asBool(argv[2]));
            case "tStructOf" -> ReflectTypes.structOf(toStringArray(asSlice(argv[0])),
                    toTokArray(asSlice(argv[1])), toStringArray(asSlice(argv[2])),
                    toStringArray(asSlice(argv[3])), toBoolArray(asSlice(argv[4])));
            default -> dispatchValue(op, argv);
        };
    }

    private static String simpleName(long tok) {
        TypeInfo t = ReflectTypes.table().info(tok);
        if (!t.isNamed()) {
            return "";
        }
        String qname = t.name;
        int dot = qname.lastIndexOf('.');
        return dot >= 0 ? qname.substring(dot + 1) : qname;
    }

    private static int bitsOf(long tok) {
        TypeInfo t = ReflectTypes.table().info(tok);
        return switch (t.kind) {
            case INT8, UINT8 -> 8;
            case INT16, UINT16 -> 16;
            case INT32, UINT32, FLOAT32 -> 32;
            case INT64, UINT64, FLOAT64 -> 64;
            case COMPLEX64 -> 64;
            case COMPLEX128 -> 128;
            default -> throw new IllegalStateException("reflect: bits of " + t.kind);
        };
    }

    private static int chanDirOrdinal(long tok) {
        return switch (ReflectTypes.table().info(tok).chanDir) {
            case RECV -> 1;
            case SEND -> 2;
            case BOTH -> 3;
        };
    }

    private static boolean comparable(long tok) {
        TypeInfo t = ReflectTypes.table().info(tok);
        return switch (t.kind) {
            case BOOL, INT8, INT16, INT32, INT64, UINT8, UINT16,
                 UINT32, UINT64, FLOAT32, FLOAT64, COMPLEX64,
                 COMPLEX128, STRING, POINTER, CHAN, UNSAFE_POINTER -> true;
            case INTERFACE -> true; // interfaces are comparable (may panic at runtime)
            case ARRAY -> comparable(t.elem.tok);
            case STRUCT -> t.fields.stream().allMatch(f -> comparable(f.type.tok));
            default -> false;
        };
    }

    private static Object dispatchValue(String op, Object[] argv) {
        return switch (op) {
            case "typeof" -> ReflectValues.typeof(argv[0]);
            case "valueof" -> valueOf(argv[0]);
            case "new" -> reflectNew(asTok(argv[0]));
            case "newat" -> reflectNewAt(asTok(argv[0]), asTok(argv[1]));
            case "field" -> reflectField(asValue(argv[0]), asInt(argv[1]));
            case "index" -> reflectIndex(asValue(argv[0]), asInt(argv[1]));
            case "elem" -> reflectElem(asValue(argv[0]));
            case "len" -> (long) reflectLen(asValue(argv[0]));
            case "cap" -> (long) reflectCap(asValue(argv[0]));
            case "isnil" -> reflectIsNil(asValue(argv[0]));
            case "iszero" -> reflectIsZero(asValue(argv[0]));
            case "getint" -> ReflectValues.getInt(unboxValue(asValue(argv[0])));
            case "getuint" -> ReflectValues.getUint(unboxValue(asValue(argv[0])));
            case "getfloat" -> ReflectValues.getFloat(unboxValue(asValue(argv[0])));
            case "getcomplex" -> unboxValue(asValue(argv[0]));
            case "getbool" -> unboxValue(asValue(argv[0]));
            case "getstring" -> unboxValue(asValue(argv[0]));
            case "getbytes" -> getBytes(asValue(argv[0]));
            case "setint" -> {
                setScalar(asValue(argv[0]), argv[1]);
                yield null;
            }
            case "setuint" -> {
                setScalar(asValue(argv[0]), argv[1]);
                yield null;
            }
            case "setfloat" -> {
                setScalar(asValue(argv[0]), argv[1]);
                yield null;
            }
            case "setcomplex" -> {
                setScalar(asValue(argv[0]), argv[1]);
                yield null;
            }
            case "setbool" -> {
                setScalar(asValue(argv[0]), argv[1]);
                yield null;
            }
            case "setstring" -> {
                setScalar(asValue(argv[0]), argv[1]);
                yield null;
            }
            case "setbytes" -> {
                setBytes(asValue(argv[0]), argv[1]);
                yield null;
            }
            case "set" -> {
                reflectSet(asValue(argv[0]), asValue(argv[1]));
                yield null;
            }
            case "setzero" -> {
                setZero(asValue(argv[0]));
                yield null;
            }
            case "setmapindex" -> {
                setMapIndex(asValue(argv[0]), asValue(argv[1]), asValue(argv[2]));
                yield null;
            }
            case "mapindex" -> mapIndex(asValue(argv[0]), asValue(argv[1]));
            case "interface" -> reflectInterface(asValue(argv[0]));
            case "rawinterface" -> reflectInterface(asValue(argv[0]));
            case "convert" -> reflectConvert(asValue(argv[0]), asTok(argv[1]));
            case "slice" -> reflectSlice(asValue(argv[0]), asInt(argv[1]), asInt(argv[2]));
            case "slice3" -> reflectSlice3(asValue(argv[0]), asInt(argv[1]), asInt(argv[2]), asInt(argv[3]));
            case "grow" -> {
                reflectGrow(asValue(argv[0]), asInt(argv[1]));
                yield null;
            }
            case "setlen" -> {
                setLen(asValue(argv[0]), asInt(argv[1]));
                yield null;
            }
            case "setcap" -> {
                setCap(asValue(argv[0]), asInt(argv[1]));
                yield null;
            }
            case "clear" -> {
                reflectClear(asValue(argv[0]));
                yield null;
            }
            case "call" -> reflectCall(asValue(argv[0]), asSlice(argv[1]), false);
            case "callslice" -> reflectCall(asValue(argv[0]), asSlice(argv[1]), true);
            case "methodval" -> methodVal(asValue(argv[0]), asInt(argv[1]));
            case "makefunc" -> makeFunc(asTok(argv[0]), asValue(argv[1]));
            case "append" -> reflectAppend(asValue(argv[0]), asSlice(argv[1]));
            case "makeslice" -> makeSlice(asTok(argv[0]), asInt(argv[1]), asInt(argv[2]));
            case "makemap" -> makeMap(asTok(argv[0]));
            case "makechan" -> makeChan(asTok(argv[0]), asInt(argv[1]));
            case "sliceat" -> throw new IllegalStateException("reflect: SliceAt needs type synthesis");
            case "send" -> {
                chanSend(asValue(argv[0]), asValue(argv[1]));
                yield null;
            }
            case "recv" -> chanRecv(asValue(argv[0]));
            case "trysend" -> chanTrySend(asValue(argv[0]), asValue(argv[1]));
            case "close" -> {
                chanClose(asValue(argv[0]));
                yield null;
            }
            case "swap" -> {
                reflectSwap(asValue(argv[0]), asInt(argv[1]), asInt(argv[2]));
                yield null;
            }
            case "selectimpl" -> reflectSelect(asSlice(argv[0]));
            case "mapiterinit" -> mapIterInit(asValue(argv[0]));
            case "mapiternext" -> mapIterNext(asTok(argv[0]));
            case "mapiterkey" -> mapIterKey(asTok(argv[0]));
            case "mapiterval" -> mapIterVal(asTok(argv[0]));
            case "mapiterreset" -> {
                mapIterReset(asTok(argv[0]), asValue(argv[1]));
                yield null;
            }
            default -> throw new IllegalStateException("reflect: unknown op " + op);
        };
    }

    // ------------------------------------------------------------------
    // Value construction.
    // ------------------------------------------------------------------

    /** __reflect_valueof: Value for an interface payload. */
    private static GoStruct valueOf(Object iface) {
        GoStructDescriptor desc = ReflectTypes.valueDescriptor();
        if (iface == null || iface == graalgo.values.GoInterface.NIL) {
            return ReflectValues.zero(desc);
        }
        long tok = ReflectValues.typeof(iface);
        Object data = iface instanceof graalgo.values.GoInterface gi ? gi.data() : iface;
        long handle = ReflectRefs.intern(data);
        return ReflectValues.pack(desc, tok, handle, 0);
    }

    /** __reflect_new: *T pointing at a fresh mutable cell. */
    private static GoStruct reflectNew(long tok) {
        GoStructDescriptor desc = ReflectTypes.valueDescriptor();
        Object zero = zeroFor(tok);
        long handle = ReflectRefs.intern(new Cell(zero));
        long ptrTok = ReflectTypes.ptrTo(tok);
        return ReflectValues.pack(desc, ptrTok, handle, 0);
    }

    /** __reflect_newat: *T pointing at an existing handle. */
    private static GoStruct reflectNewAt(long tok, long p) {
        GoStructDescriptor desc = ReflectTypes.valueDescriptor();
        // resolve validates the handle (unknown → UnsafeEscapeException).
        ReflectRefs.resolve(p);
        long ptrTok = ReflectTypes.ptrTo(tok);
        return ReflectValues.pack(desc, ptrTok, p, ReflectValues.FLAG_ADDR);
    }

    /** Zero D5 value for a token. */
    private static Object zeroFor(long tok) {
        TypeInfo t = ReflectTypes.table().info(tok);
        return switch (t.kind) {
            case BOOL -> Boolean.FALSE;
            case INT32, INT16, INT8 -> Integer.valueOf(0);
            case UINT32, UINT16, UINT8 -> Integer.valueOf(0);
            case INT64, UINT64 -> Long.valueOf(0L);
            case FLOAT32 -> Float.valueOf(0f);
            case FLOAT64 -> Double.valueOf(0d);
            case COMPLEX64 -> graalgo.values.GoComplex32.ZERO;
            case COMPLEX128 -> graalgo.values.GoComplex64.ZERO;
            case STRING -> GoString.EMPTY;
            case SLICE, MAP, POINTER, CHAN, FUNC, UNSAFE_POINTER, INTERFACE -> null;
            case ARRAY -> ReflectValues.newArray(t.elem.kind, t.arrayLen);
            case STRUCT -> zeroStruct(t);
            default -> throw new IllegalStateException("reflect: zero of " + t.kind);
        };
    }

    private static graalgo.values.GoStructDescriptor structDescFor(TypeInfo t) {
        // The struct's GoStructDescriptor: built from the wire via layout.
        // (Captured per-type at install in a full implementation; here we
        // build on demand from the field list.)
        List<String> names = new ArrayList<>();
        List<GoKind> kinds = new ArrayList<>();
        for (ReflectTypes.FieldInfo f : t.fields) {
            names.add(f.name);
            kinds.add(f.type.kind);
        }
        return GoStructDescriptor.layout(ReflectTypes.typeString(t.tok), names, kinds);
    }

    /**
     * Zero GoStruct for a (possibly synthesized) struct type. Nested
     * aggregates are zero-filled recursively: struct fields get zero
     * structs, string fields get GoString.EMPTY, array fields get fresh
     * arrays with struct elements zeroed. (The flat new-Object[] left
     * nested struct fields null, breaking Field() chains on New(T).)
     */
    private static GoStruct zeroStruct(TypeInfo t) {
        graalgo.values.GoStructDescriptor desc = structDescFor(t);
        GoStruct s = new GoStruct(desc, new long[desc.longsCount()],
                new Object[desc.objectsCount()]);
        for (int i = 0; i < t.fields.size(); i++) {
            ReflectTypes.FieldInfo f = t.fields.get(i);
            if (desc.field(i).inLongs()) {
                continue; // numeric/bool fields are already zero
            }
            Object zv = switch (f.type.kind) {
                case STRUCT -> zeroStruct(f.type);
                case STRING -> GoString.EMPTY;
                case ARRAY -> zeroArray(f.type);
                default -> null; // slices, maps, pointers, funcs, chans, interfaces
            };
            if (zv != null) {
                s.setObject(i, zv);
            }
        }
        return s;
    }

    /** Zero array with struct elements recursively zeroed. */
    private static Object zeroArray(TypeInfo t) {
        Object arr = ReflectValues.newArray(t.elem.kind, t.arrayLen);
        if (t.elem.kind == GoKind.STRUCT && arr instanceof Object[] oa) {
            for (int i = 0; i < oa.length; i++) {
                oa[i] = zeroStruct(t.elem);
            }
        }
        return arr;
    }

    // ------------------------------------------------------------------
    // Navigation.
    // ------------------------------------------------------------------

    /** Unboxes a Value to its D5 value (resolving derived references). */
    private static Object unboxValue(GoStruct v) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        if (p.typ() == 0) {
            // Invalid (zero) Value: old load path (resolve(0) is null).
            return ReflectRefs.load(p.ptr());
        }
        GoKind kind = ReflectTypes.table().info(p.typ()).kind;
        return ReflectRefs.denotedValue(p.ptr(), kind);
    }

    /**
     * __reflect_interface / __reflect_rawinterface: boxes the Value's D5
     * value as a proper {@code any} (GoInterface). The safe/raw distinction
     * (flagRO check) is handled by the synthetic callers; both intrinsics
     * return a real interface value, like gc's valueInterface.
     */
    private static Object reflectInterface(GoStruct v) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        Object d5 = ReflectRefs.denotedValue(p.ptr(), t.kind);
        if (d5 == null) {
            return null;
        }
        if (d5 instanceof graalgo.values.GoInterface gi) {
            // Already an interface value: gc unwraps one level into the
            // empty interface (the dynamic type travels with the value).
            return gi;
        }
        graalgo.values.GoTypeDescriptor d = ReflectTypes.descriptorForTok(p.typ());
        return new graalgo.values.GoInterface(d, d5, null);
    }

    /** __reflect_field: struct field with flagRO propagation. */
    private static GoStruct reflectField(GoStruct v, int i) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        ReflectTypes.FieldInfo f = t.fields.get(i);
        Object base = ReflectRefs.denotedValue(p.ptr(), GoKind.STRUCT);
        if (!(base instanceof GoStruct s)) {
            throw new IllegalStateException("reflect: field base is not a struct");
        }
        long flag = 0;
        // Addressability: fields of an addressable struct are addressable.
        if ((p.flag() & ReflectValues.FLAG_ADDR) != 0) {
            flag |= ReflectValues.FLAG_ADDR;
        }
        // flagRO propagation (gc-exact): unexported fields get stickyRO;
        // fields of an unexported embedded struct field get embedRO.
        if (!f.isExported()) {
            flag |= ReflectValues.FLAG_STICKY_RO;
        } else if ((p.flag() & ReflectValues.FLAG_RO) != 0) {
            flag |= ReflectValues.FLAG_EMBED_RO;
        }
        long fieldHandle = ReflectRefs.internRef(new ReflectRefs.FieldRef(p.ptr(), i));
        return ReflectValues.pack(ReflectTypes.valueDescriptor(), f.type.tok, fieldHandle, flag);
    }

    /** __reflect_index: array/slice/string element. */
    private static GoStruct reflectIndex(GoStruct v, int i) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        GoStructDescriptor desc = ReflectTypes.valueDescriptor();
        switch (t.kind) {
            case ARRAY -> {
                Object arr = ReflectRefs.denotedValue(p.ptr(), GoKind.ARRAY);
                long elemTok = t.elem.tok;
                long flag = 0;
                if ((p.flag() & ReflectValues.FLAG_ADDR) != 0) {
                    flag |= ReflectValues.FLAG_ADDR;
                }
                if ((p.flag() & ReflectValues.FLAG_RO) != 0) {
                    flag |= ReflectValues.FLAG_EMBED_RO;
                }
                long h = ReflectRefs.internRef(new ReflectRefs.ArrayElemRef(p.ptr(), i));
                return ReflectValues.pack(desc, elemTok, h, flag);
            }
            case SLICE -> {
                long elemTok = t.elem.tok;
                // Slice elements are always addressable; RO propagates as embedRO.
                long flag = ReflectValues.FLAG_ADDR;
                if ((p.flag() & ReflectValues.FLAG_RO) != 0) {
                    flag |= ReflectValues.FLAG_EMBED_RO;
                }
                long h = ReflectRefs.internRef(new ReflectRefs.SliceElemRef(p.ptr(), i));
                return ReflectValues.pack(desc, elemTok, h, flag);
            }
            case STRING -> {
                GoString gs = (GoString) ReflectRefs.denotedValue(p.ptr(), GoKind.STRING);
                byte b = gs.bytes()[i];
                long h = ReflectRefs.intern(b);
                long uint8Tok = ReflectTypes.table().tokForKind(GoKind.UINT8);
                return ReflectValues.pack(desc, uint8Tok, h, 0);
            }
            default -> throw new IllegalStateException("reflect: index of " + t.kind);
        }
    }

    /** __reflect_elem: interface unwrap or pointer dereference. */
    private static GoStruct reflectElem(GoStruct v) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        GoStructDescriptor desc = ReflectTypes.valueDescriptor();
        Object d5 = ReflectRefs.denotedValue(p.ptr(), t.kind);
        switch (t.kind) {
            case INTERFACE -> {
                if (d5 == null) {
                    return ReflectValues.zero(desc);
                }
                // The interface data: find its dynamic type token.
                long elemTok = ReflectValues.tokForDescriptor(
                        ((graalgo.values.GoInterface) d5).descriptor());
                Object data = ((graalgo.values.GoInterface) d5).data();
                long h = ReflectRefs.intern(data);
                // gc: Elem sets flagIndir on the result (both branches).
                long flag = ReflectValues.FLAG_INDIR | (p.flag() & ReflectValues.FLAG_RO);
                return ReflectValues.pack(desc, elemTok, h, flag);
            }
            case POINTER -> {
                // The Go pointer P that v represents is nil iff:
                // - the handle addresses a ReflectRefs.Cell: provenance
                //   decides. A Cell handle from reflectNew/reflectNewAt
                //   (no FLAG_INDIR — gc's flagIndir) denotes &cell, a
                //   non-nil pointer, so P is never nil. A Cell handle on an
                //   Elem RESULT (FLAG_INDIR set) reuses the parent's handle
                //   and denotes the cell's CONTENT (the pointee *T), which
                //   may itself be nil. Without the INDIR bit the two
                //   provenances are indistinguishable on the shared handle:
                //   New(*T).Elem().Elem() returned a bogus valid Value
                //   instead of gc's zero Value (PKG-022 VETO-1);
                // - the handle addresses a GoCell: P is the cell's content
                //   (nil content = nil pointer; from Elem on a cell pointer);
                // - otherwise P is the denoted D5 value: nil iff d5 is null
                //   or a nil GoPointer.
                Object storage = ReflectRefs.resolve(p.ptr());
                boolean pIsNil;
                if (storage instanceof Cell) {
                    pIsNil = ((p.flag() & ReflectValues.FLAG_INDIR) != 0)
                            && (d5 == null
                                || (d5 instanceof GoPointer gp2 && gp2.isNil()));
                } else if (storage instanceof graalgo.values.GoCell gc) {
                    pIsNil = gc.get() == null;
                } else if (d5 instanceof GoPointer gp) {
                    pIsNil = gp.isNil();
                } else {
                    pIsNil = d5 == null;
                }
                if (pIsNil) {
                    // The pointer itself is nil: Elem() is the zero Value (gc).
                    return ReflectValues.zero(desc);
                }
                // P is non-nil: Elem() is the pointee Value (valid even when
                // the pointee itself is nil — e.g. **T pointing to nil *T).
                long h;
                if (d5 instanceof GoPointer gp) {
                    Object ref = gp.referent();
                    if (ref == null && gp.isCellPointer()) {
                        // Non-nil cell pointer to a nil value (e.g. &ppp where
                        // ppp is a nil **int): Elem() is a VALID nil *T Value
                        // (gc), not the zero Value. Address the cell storage
                        // so Set() writes through; interning the null value
                        // would lose the storage identity. PKG-022:
                        // Value.Elem() on **T pointing to nil *T returned the
                        // zero Value, breaking reflect-driven Set in errors.As
                        // test setup (and any pointer-to-nil-pointer Elem).
                        h = ReflectRefs.intern(gp.cell());
                    } else if (ref == null) {
                        return ReflectValues.zero(desc);
                    } else {
                        h = ReflectRefs.intern(ref);
                    }
                } else {
                    // d5 is the raw pointee value (or null for a nil pointee
                    // behind Cell storage): address the storage itself so
                    // Set() writes through.
                    h = p.ptr();
                }
                // gc: Elem sets flagIndir on the result (provenance consumed
                // by the Cell-handle nilness rule above).
                long flag = ReflectValues.FLAG_INDIR | ReflectValues.FLAG_ADDR
                        | (p.flag() & ReflectValues.FLAG_RO);
                return ReflectValues.pack(desc, t.elem.tok, h, flag);
            }
            default -> throw new IllegalStateException("reflect: elem of " + t.kind);
        }
    }

    private static int reflectLen(GoStruct v) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        Object d5 = ReflectRefs.denotedValue(p.ptr(), t.kind);
        return switch (t.kind) {
            case ARRAY -> t.arrayLen;
            case SLICE -> ((GoSlice) d5).len();
            case STRING -> ((GoString) d5).bytes().length;
            case MAP -> graalgo.runtime.Intrinsics.mapLen((GoMap) d5);
            case CHAN -> graalgo.runtime.ChanOps.len((graalgo.runtime.ChanOps.Chan) d5);
            case POINTER -> {
                // Pointer to array.
                if (t.elem.kind != GoKind.ARRAY) {
                    throw new IllegalStateException(
                            "reflect: call of reflect.Value.Len on ptr to non-array Value");
                }
                yield t.elem.arrayLen;
            }
            default -> throw new IllegalStateException("reflect: len of " + t.kind);
        };
    }

    private static int reflectCap(GoStruct v) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        Object d5 = ReflectRefs.denotedValue(p.ptr(), t.kind);
        return switch (t.kind) {
            case ARRAY -> t.arrayLen;
            case SLICE -> ((GoSlice) d5).cap();
            case CHAN -> graalgo.runtime.ChanOps.cap((graalgo.runtime.ChanOps.Chan) d5);
            case POINTER -> {
                if (t.elem.kind != GoKind.ARRAY) {
                    throw new IllegalStateException(
                            "reflect: call of reflect.Value.Cap on ptr to non-array Value");
                }
                yield t.elem.arrayLen;
            }
            default -> throw new IllegalStateException("reflect: cap of " + t.kind);
        };
    }

    private static boolean reflectIsNil(GoStruct v) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        Object d5 = ReflectRefs.denotedValue(p.ptr(), t.kind);
        return d5 == null;
    }

    private static boolean reflectIsZero(GoStruct v) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        Object d5 = ReflectRefs.denotedValue(p.ptr(), t.kind);
        return isZeroValue(t, d5);
    }

    private static boolean isZeroValue(TypeInfo t, Object d5) {
        if (d5 == null) {
            return true;
        }
        return switch (t.kind) {
            case BOOL -> !(Boolean) d5;
            case INT32, INT16, INT8 -> ((Integer) d5) == 0;
            case UINT32, UINT16, UINT8 -> ((Integer) d5) == 0;
            case INT64, UINT64 -> ((Long) d5) == 0L;
            case FLOAT32 -> ((Float) d5) == 0f;
            case FLOAT64 -> ((Double) d5) == 0d;
            case COMPLEX64 -> graalgo.values.GoComplex32.ZERO.equals(d5);
            case COMPLEX128 -> graalgo.values.GoComplex64.ZERO.equals(d5);
            case STRING -> ((GoString) d5).bytes().length == 0;
            case ARRAY -> {
                for (int i = 0; i < t.arrayLen; i++) {
                    if (!isZeroValue(t.elem,
                            ReflectValues.arrayGet(d5, t.elem.kind, i))) {
                        yield false;
                    }
                }
                yield true;
            }
            case STRUCT -> {
                GoStruct s = (GoStruct) d5;
                for (int i = 0; i < t.fields.size(); i++) {
                    ReflectTypes.FieldInfo f = t.fields.get(i);
                    Object fv = ReflectValues.structFieldGet(s, i, f.type.kind);
                    if (!isZeroValue(f.type, fv)) {
                        yield false;
                    }
                }
                yield true;
            }
            default -> false;
        };
    }

    // ------------------------------------------------------------------
    // Stores.
    // ------------------------------------------------------------------

    /**
     * Stores a canonical D5 scalar through a Value's reference. The
     * synthetic layer has already checked assignability and kind. The
     * Value's own type token identifies the element kind.
     */
    private static void setScalar(GoStruct v, Object boxed) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        GoKind kind = ReflectTypes.table().info(p.typ()).kind;
        Object ref = ReflectRefs.resolve(p.ptr());
        if (ref instanceof Cell c) {
            c.value = boxed;
            return;
        }
        storeThroughRef(ref, kind, boxed);
    }

    /**
     * Write-through store for element/field references (shared by setScalar
     * and reflectSet). P3-M8b: extracted from setScalar so whole-value Set
     * into slice/array elements and struct fields works too (Copy into
     * []int elements hit "Set of INT64 through non-cell").
     */
    private static void storeThroughRef(Object ref, GoKind kind, Object boxed) {
        if (ref instanceof ReflectRefs.FieldRef fr) {
            GoStruct s = (GoStruct) ReflectRefs.denotedValue(fr.base(), GoKind.STRUCT);
            ReflectValues.structFieldSet(s, fr.field(), kind, boxed);
            return;
        }
        if (ref instanceof ReflectRefs.ArrayElemRef ar) {
            Object arr = ReflectRefs.denotedValue(ar.base(), GoKind.ARRAY);
            ReflectValues.arraySet(arr, kind, ar.index(), boxed);
            return;
        }
        if (ref instanceof ReflectRefs.SliceElemRef sr) {
            GoSlice slice = (GoSlice) ReflectRefs.denotedValue(sr.base(), GoKind.SLICE);
            Object arr = slice.array();
            ReflectValues.arraySet(arr, kind, slice.offset() + sr.index(), boxed);
            return;
        }
        throw new IllegalStateException("reflect: cannot store through " + ref);
    }

    private static void setBytes(GoStruct v, Object bytesObj) {
        // SetBytes: v is []byte, x is []byte. Replace the slice header.
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        Object ref = ReflectRefs.resolve(p.ptr());
        byte[] bytes = (byte[]) bytesObj;
        GoSlice newSlice = new GoSlice(bytes, 0, bytes.length, bytes.length);
        if (ref instanceof Cell c) {
            c.value = newSlice;
            return;
        }
        throw new IllegalStateException("reflect: SetBytes through non-cell");
    }

    /** __reflect_set: whole-value assignment (assignability checked in Go). */
    private static void reflectSet(GoStruct dst, GoStruct src) {
        ReflectValues.ValueParts dp = ReflectValues.parts(dst);
        Object d5src = unboxValue(src);
        Object ref = ReflectRefs.resolve(dp.ptr());
        if (ref instanceof Cell c) {
            c.value = d5src;
            return;
        }
        // PKG-022: Elem() on a pointer to a nil pointee addresses the
        // pointee's GoCell storage. Write through the cell.
        if (ref instanceof graalgo.values.GoCell gc) {
            gc.set(d5src);
            return;
        }
        TypeInfo t = ReflectTypes.table().info(dp.typ());
        if (t.kind != GoKind.STRUCT && t.kind != GoKind.ARRAY) {
            // Scalar (or other leaf) through an element/field ref: write
            // through like setScalar (P3-M8b: Copy into slice elements).
            storeThroughRef(ref, t.kind, d5src);
            return;
        }
        // Whole-aggregate store: copy field-wise / element-wise.
        Object d5dst = ReflectRefs.denotedValue(dp.ptr(), t.kind);
        copyAggregate(t, d5dst, d5src);
    }

    /** Copies one aggregate into another (for whole-value Set). */
    private static void copyAggregate(TypeInfo t, Object dst, Object src) {
        switch (t.kind) {
            case STRUCT -> {
                GoStruct ds = (GoStruct) dst;
                GoStruct ss = (GoStruct) src;
                for (int i = 0; i < t.fields.size(); i++) {
                    ReflectTypes.FieldInfo f = t.fields.get(i);
                    Object fv = ReflectValues.structFieldGet(ss, i, f.type.kind);
                    ReflectValues.structFieldSet(ds, i, f.type.kind, fv);
                }
            }
            case ARRAY -> {
                int n = t.arrayLen;
                for (int i = 0; i < n; i++) {
                    Object ev = ReflectValues.arrayGet(src, t.elem.kind, i);
                    ReflectValues.arraySet(dst, t.elem.kind, i, ev);
                }
            }
            default -> throw new IllegalStateException(
                    "reflect: Set of " + t.kind + " through non-cell");
        }
    }

    private static void setZero(GoStruct v) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        Object ref = ReflectRefs.resolve(p.ptr());
        Object zero = zeroFor(p.typ());
        if (ref instanceof Cell c) {
            c.value = zero;
            return;
        }
        // PKG-022: see reflectSet — Elem() on a pointer to a nil pointee
        // addresses the pointee's GoCell storage.
        if (ref instanceof graalgo.values.GoCell gc) {
            gc.set(zero);
            return;
        }
        TypeInfo t = ReflectTypes.table().info(p.typ());
        if (t.kind != GoKind.STRUCT && t.kind != GoKind.ARRAY) {
            // Scalar (or other leaf) through an element/field ref: write
            // through like setScalar (same P3-M8b class as reflectSet).
            storeThroughRef(ref, t.kind, zero);
            return;
        }
        copyAggregate(t, ReflectRefs.denotedValue(p.ptr(), t.kind), zero);
    }

    private static void setMapIndex(GoStruct v, GoStruct key, GoStruct elem) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        GoMap m = (GoMap) ReflectRefs.denotedValue(p.ptr(), GoKind.MAP);
        Object d5key = unboxValue(key);
        if (!ReflectValues.isValid(elem)) {
            graalgo.runtime.Intrinsics.mapDelete(m, d5key);
        } else {
            Object d5elem = unboxValue(elem);
            graalgo.runtime.Intrinsics.mapAssign(m, d5key, d5elem);
        }
    }

    private static GoStruct mapIndex(GoStruct v, GoStruct key) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        GoMap m = (GoMap) ReflectRefs.denotedValue(p.ptr(), t.kind);
        Object d5key = unboxValue(key);
        Object d5val = graalgo.runtime.Intrinsics.mapAccess(m, d5key);
        GoStructDescriptor desc = ReflectTypes.valueDescriptor();
        if (d5val == null) {
            return ReflectValues.zero(desc);
        }
        long h = ReflectRefs.intern(d5val);
        long flag = p.flag() & ReflectValues.FLAG_RO;
        return ReflectValues.pack(desc, t.elem.tok, h, flag);
    }

    private static Object getBytes(GoStruct v) {
        // Returns the []byte as a Java byte[] for the synthetic Bytes().
        Object d5 = unboxValue(v);
        if (d5 instanceof GoSlice s) {
            Object arr = s.array();
            int off = s.offset();
            int len = s.len();
            byte[] out = new byte[len];
            System.arraycopy((byte[]) arr, off, out, 0, len);
            return out;
        }
        if (d5 != null && d5.getClass() == byte[].class) {
            return d5;
        }
        throw new IllegalStateException("reflect: Bytes of non-byte slice");
    }

    // ------------------------------------------------------------------
    // Conversion, slices, calls.
    // ------------------------------------------------------------------

    /** __reflect_convert: mirrors ConvertNode's combos. */
    private static GoStruct reflectConvert(GoStruct v, long toTok) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo from = ReflectTypes.table().info(p.typ());
        TypeInfo to = ReflectTypes.table().info(toTok);
        Object d5 = unboxValue(v);
        Object converted = convertValue(from, to, d5);
        long h = ReflectRefs.intern(converted);
        return ReflectValues.pack(ReflectTypes.valueDescriptor(), toTok, h, 0);
    }

    private static Object convertValue(TypeInfo from, TypeInfo to, Object d5) {
        if (from.tok == to.tok) {
            return d5;
        }
        GoKind fk = from.kind;
        GoKind tk = to.kind;
        // Numeric conversions.
        if (isNumericKind(fk) && isNumericKind(tk)) {
            return convertNumeric(d5, fk, tk);
        }
        // String <-> []byte / []rune.
        if (fk == GoKind.STRING && tk == GoKind.SLICE) {
            GoString gs = (GoString) d5;
            if (to.elem.kind == GoKind.UINT8) {
                return new GoSlice(gs.bytes().clone(), 0, gs.bytes().length, gs.bytes().length);
            }
            // []rune: decode UTF-8.
            String s = new String(gs.bytes(), java.nio.charset.StandardCharsets.UTF_8);
            int[] runes = s.codePoints().toArray();
            return new GoSlice(runes, 0, runes.length, runes.length);
        }
        if (tk == GoKind.STRING && fk == GoKind.SLICE) {
            GoSlice sl = (GoSlice) d5;
            if (from.elem.kind == GoKind.UINT8) {
                byte[] bytes = new byte[sl.len()];
                System.arraycopy((byte[]) sl.array(), sl.offset(), bytes, 0, sl.len());
                return new GoString(bytes);
            }
            int[] runes = new int[sl.len()];
            System.arraycopy((int[]) sl.array(), sl.offset(), runes, 0, sl.len());
            String s = new String(runes, 0, runes.length);
            return GoString.fromUtf8(s);
        }
        // Integer -> string (rune conversion).
        if (isIntKind(fk) && tk == GoKind.STRING) {
            long cp = ((Number) d5).longValue();
            return GoString.fromRune((int) cp);
        }
        throw new IllegalStateException(
                "reflect: convert " + fk + " to " + tk + " not supported");
    }

    private static boolean isNumericKind(GoKind k) {
        return isIntKind(k) || isFloatKind(k) || k == GoKind.COMPLEX64 || k == GoKind.COMPLEX128;
    }

    private static boolean isIntKind(GoKind k) {
        return switch (k) {
            case INT8, INT16, INT32, INT64, UINT8, UINT16, UINT32, UINT64 -> true;
            default -> false;
        };
    }

    private static boolean isFloatKind(GoKind k) {
        return k == GoKind.FLOAT32 || k == GoKind.FLOAT64;
    }

    private static Object convertNumeric(Object d5, GoKind fk, GoKind tk) {
        // Via double/long intermediates (v1 range; matches ConvertNode).
        if (tk == GoKind.COMPLEX64 || tk == GoKind.COMPLEX128) {
            double re = toDouble(d5, fk);
            if (tk == GoKind.COMPLEX64) {
                return new graalgo.values.GoComplex32((float) re, 0f);
            }
            return new graalgo.values.GoComplex64(re, 0);
        }
        if (fk == GoKind.COMPLEX64 || fk == GoKind.COMPLEX128) {
            double re = d5 instanceof graalgo.values.GoComplex32 c32 ? c32.re()
                    : ((graalgo.values.GoComplex64) d5).re();
            return fromDouble(re, tk);
        }
        if (isFloatKind(tk)) {
            double dv = toDouble(d5, fk);
            return tk == GoKind.FLOAT32 ? Float.valueOf((float) dv) : Double.valueOf(dv);
        }
        long lv;
        if (isFloatKind(fk)) {
            lv = (long) toDouble(d5, fk);
        } else {
            lv = ((Number) d5).longValue();
        }
        return switch (tk) {
            case INT8, INT16, INT32, UINT8, UINT16, UINT32 -> Integer.valueOf((int) lv);
            case INT64, UINT64 -> Long.valueOf(lv);
            default -> throw new IllegalStateException("reflect: numeric convert to " + tk);
        };
    }

    private static double toDouble(Object d5, GoKind fk) {
        if (d5 instanceof Double dd) {
            return dd;
        }
        if (d5 instanceof Float ff) {
            return ff;
        }
        return ((Number) d5).longValue();
    }

    private static Object fromDouble(double dv, GoKind tk) {
        return switch (tk) {
            case FLOAT32 -> Float.valueOf((float) dv);
            case FLOAT64 -> Double.valueOf(dv);
            case INT8, INT16, INT32, UINT8, UINT16, UINT32 -> Integer.valueOf((int) dv);
            case INT64, UINT64 -> Long.valueOf((long) dv);
            default -> throw new IllegalStateException("reflect: fromDouble " + tk);
        };
    }

    private static GoStruct reflectSlice(GoStruct v, int i, int j) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        Object d5 = ReflectRefs.denotedValue(p.ptr(), t.kind);
        GoStructDescriptor desc = ReflectTypes.valueDescriptor();
        long flag = p.flag() & ReflectValues.FLAG_RO;
        switch (t.kind) {
            case ARRAY -> {
                Object arr = d5;
                GoSlice sl = new GoSlice(arr, i, j - i, t.arrayLen - i);
                long h = ReflectRefs.intern(sl);
                long sliceTok = sliceTokFor(t.elem.tok);
                return ReflectValues.pack(desc, sliceTok, h, flag);
            }
            case SLICE -> {
                GoSlice s = (GoSlice) d5;
                GoSlice sl = s.slice(i, j);
                long h = ReflectRefs.intern(sl);
                return ReflectValues.pack(desc, p.typ(), h, flag);
            }
            case STRING -> {
                GoString gs = (GoString) d5;
                byte[] bytes = gs.bytes();
                byte[] sub = new byte[j - i];
                System.arraycopy(bytes, i, sub, 0, j - i);
                long h = ReflectRefs.intern(new GoString(sub));
                return ReflectValues.pack(desc, p.typ(), h, flag);
            }
            default -> throw new IllegalStateException("reflect: slice of " + t.kind);
        }
    }

    private static GoStruct reflectSlice3(GoStruct v, int i, int j, int k) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        Object d5 = ReflectRefs.denotedValue(p.ptr(), t.kind);
        GoStructDescriptor desc = ReflectTypes.valueDescriptor();
        long flag = p.flag() & ReflectValues.FLAG_RO;
        if (t.kind == GoKind.ARRAY) {
            Object arr = d5;
            GoSlice sl = new GoSlice(arr, i, j - i, k - i);
            long h = ReflectRefs.intern(sl);
            long sliceTok = sliceTokFor(t.elem.tok);
            return ReflectValues.pack(desc, sliceTok, h, flag);
        }
        GoSlice s = (GoSlice) d5;
        GoSlice sl = s.slice3(i, j, k);
        long h = ReflectRefs.intern(sl);
        return ReflectValues.pack(desc, p.typ(), h, flag);
    }

    /** Token for []E (synthesized if absent). */
    private static long sliceTokFor(long elemTok) {
        ReflectTypes.Table table = ReflectTypes.table();
        TypeInfo elem = table.info(elemTok);
        String key = "slice:" + elemKey(elem);
        Long existing = table.keyToTok.get(key);
        if (existing != null) {
            return existing;
        }
        long tok = table.byTok.size();
        table.keyToTok.put(key, tok);
        TypeInfo info = new TypeInfo(tok, GoKind.SLICE, "", "", 24, 8,
                elem, null, 0, null, List.of(), List.of(),
                List.of(), List.of(), false, List.of(), "", -1);
        table.byTok.add(info);
        return tok;
    }

    private static String elemKey(TypeInfo t) {
        if (t.isNamed()) {
            return "named:" + t.pkgPath + "." + t.name;
        }
        return "basic:" + t.kind;
    }

    // ------------------------------------------------------------------
    // Calls, methods, MakeFunc.
    // ------------------------------------------------------------------

    /** __reflect_call / __reflect_callslice. */
    private static GoSlice reflectCall(GoStruct fn, GoSlice args, boolean sliceCall) {
        ReflectValues.ValueParts p = ReflectValues.parts(fn);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        Object callable = ReflectRefs.denotedValue(p.ptr(), t.kind);
        // Unbox the []Value args to D5.
        Object[] d5Args = unboxArgs(args, t, sliceCall);
        Object result = ReflectCalls.invokeCallable(callable, d5Args);
        // Box the results as a []Value (the synthetic __reflect_call
        // declares []Value; v1 has no multi-result funcs).
        GoStructDescriptor desc = ReflectTypes.valueDescriptor();
        if (t.outs.isEmpty()) {
            return new GoSlice(new GoStruct[0], 0, 0, 0);
        }
        long resultTok = t.outs.get(0).tok;
        long h = ReflectRefs.intern(result);
        GoStruct v = ReflectValues.pack(desc, resultTok, h, 0);
        return new GoSlice(new GoStruct[]{v}, 0, 1, 1);
    }

    private static Object[] unboxArgs(GoSlice args, TypeInfo funcType, boolean sliceCall) {
        // A nil []Value (e.g. Call(nil)) has length 0 — GoSlice is Java null.
        if (args == null) {
            return new Object[0];
        }
        int n = args.len();
        Object arr = args.array();
        int off = args.offset();
        List<Object> out = new ArrayList<>();
        int numIn = funcType.ins.size();
        for (int i = 0; i < n; i++) {
            GoStruct v = (GoStruct) ((Object[]) arr)[off + i];
            if (sliceCall && funcType.variadic && i == n - 1) {
                // Spread the final slice argument.
                Object d5slice = unboxValue(v);
                GoSlice sl = (GoSlice) d5slice;
                Object sarr = sl.array();
                for (int j = 0; j < sl.len(); j++) {
                    out.add(((Object[]) sarr)[sl.offset() + j]);
                }
            } else {
                out.add(unboxValue(v));
            }
        }
        return out.toArray();
    }

    /** __reflect_methodval: bound method value. */
    private static GoStruct methodVal(GoStruct v, int i) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        ReflectTypes.MethodInfo mi = t.methods.get(i);
        Object receiver = unboxValue(v);
        // Pointer-receiver adaptation: if the method needs a pointer but
        // the value is addressable, wrap it.
        long handle = ReflectRefs.intern(
                new ReflectCalls.BoundMethodVal(mi, receiver));
        // The bound method's type: func without receiver.
        long boundTok = boundMethodTok(mi);
        long flag = p.flag() & ReflectValues.FLAG_RO;
        return ReflectValues.pack(ReflectTypes.valueDescriptor(), boundTok, handle, flag);
    }

    /** Token for a bound method's func type (receiver stripped). */
    private static long boundMethodTok(ReflectTypes.MethodInfo mi) {
        ReflectTypes.Table table = ReflectTypes.table();
        TypeInfo ft = mi.funcType;
        StringBuilder sb = new StringBuilder("func(");
        for (int i = 1; i < ft.ins.size(); i++) {
            if (i > 1) {
                sb.append(',');
            }
            sb.append(elemKey(ft.ins.get(i)));
        }
        sb.append(")(");
        for (int i = 0; i < ft.outs.size(); i++) {
            if (i > 0) {
                sb.append(',');
            }
            sb.append(elemKey(ft.outs.get(i)));
        }
        sb.append(')');
        if (ft.variadic) {
            sb.append("...");
        }
        String key = sb.toString();
        Long existing = table.keyToTok.get(key);
        if (existing != null) {
            return existing;
        }
        long tok = table.byTok.size();
        table.keyToTok.put(key, tok);
        List<TypeInfo> ins = new ArrayList<>(ft.ins.subList(1, ft.ins.size()));
        TypeInfo info = new TypeInfo(tok, GoKind.FUNC, "", "", 8, 8,
                null, null, 0, null, List.of(), List.of(),
                List.copyOf(ins), List.copyOf(ft.outs), ft.variadic, List.of(), "", -1);
        table.byTok.add(info);
        return tok;
    }

    /** __reflect_makefunc. */
    private static GoStruct makeFunc(long tok, GoStruct callback) {
        long handle = ReflectRefs.intern(new ReflectCalls.MakeFuncVal(tok, callback));
        return ReflectValues.pack(ReflectTypes.valueDescriptor(), tok, handle, 0);
    }

    // ------------------------------------------------------------------
    // Slice/map/chan constructors and mutation.
    // ------------------------------------------------------------------

    private static GoStruct makeSlice(long tok, int len, int cap) {
        TypeInfo t = ReflectTypes.table().info(tok);
        Object arr = ReflectValues.newArray(t.elem.kind, cap);
        GoSlice sl = new GoSlice(arr, 0, len, cap);
        long h = ReflectRefs.intern(sl);
        return ReflectValues.pack(ReflectTypes.valueDescriptor(), tok, h, 0);
    }

    private static GoStruct makeMap(long tok) {
        GoMap m = new GoMap();
        long h = ReflectRefs.intern(m);
        return ReflectValues.pack(ReflectTypes.valueDescriptor(), tok, h, 0);
    }

    private static GoStruct makeChan(long tok, int buffer) {
        TypeInfo t = ReflectTypes.table().info(tok);
        long elemSize = t.elem.size < 0 ? 8 : t.elem.size;
        graalgo.runtime.ChanOps.Chan ch =
                graalgo.runtime.ChanOps.makeChan(elemSize, buffer);
        long h = ReflectRefs.intern(ch);
        return ReflectValues.pack(ReflectTypes.valueDescriptor(), tok, h, 0);
    }

    private static GoStruct reflectAppend(GoStruct v, GoSlice xs) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        GoSlice s = (GoSlice) ReflectRefs.denotedValue(p.ptr(), t.kind);
        int n = xs.len();
        Object arr = xs.array();
        int off = xs.offset();
        // Grow if needed.
        GoSlice out = s;
        int need = s.len() + n;
        if (need > s.cap()) {
            int newCap = Math.max(need, s.cap() * 2);
            if (newCap == 0) {
                newCap = need;
            }
            Object newArr = ReflectValues.newArray(t.elem.kind, newCap);
            copySliceElements(s, newArr, t.elem.kind);
            out = new GoSlice(newArr, 0, s.len(), newCap);
        }
        Object outArr = out.array();
        for (int i = 0; i < n; i++) {
            GoStruct xv = (GoStruct) ((Object[]) arr)[off + i];
            Object d5x = unboxValue(xv);
            ReflectValues.arraySet(outArr, t.elem.kind, out.offset() + out.len() + i, d5x);
        }
        GoSlice grown = new GoSlice(outArr, out.offset(), out.len() + n, out.cap());
        long h = ReflectRefs.intern(grown);
        long flag = p.flag() & ReflectValues.FLAG_RO;
        return ReflectValues.pack(ReflectTypes.valueDescriptor(), p.typ(), h, flag);
    }

    private static void copySliceElements(GoSlice src, Object dstArr, GoKind elemKind) {
        Object srcArr = src.array();
        for (int i = 0; i < src.len(); i++) {
            Object ev = ReflectValues.arrayGet(srcArr, elemKind, src.offset() + i);
            ReflectValues.arraySet(dstArr, elemKind, i, ev);
        }
    }

    private static void reflectGrow(GoStruct v, int n) {
        // Grow(n): ensure capacity for n more elements (in place via Cell).
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        Object ref = ReflectRefs.resolve(p.ptr());
        if (!(ref instanceof Cell c)) {
            throw new IllegalStateException("reflect: Grow of non-cell slice");
        }
        GoSlice s = (GoSlice) c.value;
        if (n < 0) {
            throw new IllegalStateException("reflect.Value.Grow: negative len");
        }
        if (s.len() + n > s.cap()) {
            int newCap = Math.max(s.len() + n, s.cap() * 2);
            Object newArr = ReflectValues.newArray(t.elem.kind, newCap);
            copySliceElements(s, newArr, t.elem.kind);
            c.value = new GoSlice(newArr, 0, s.len(), newCap);
        }
    }

    private static void setLen(GoStruct v, int n) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        Object ref = ReflectRefs.resolve(p.ptr());
        if (!(ref instanceof Cell c)) {
            throw new IllegalStateException("reflect: SetLen of non-cell slice");
        }
        GoSlice s = (GoSlice) c.value;
        if (n < 0 || n > s.cap()) {
            throw new IllegalStateException("reflect: slice length out of range in SetLen");
        }
        c.value = new GoSlice(s.array(), s.offset(), n, s.cap());
    }

    private static void setCap(GoStruct v, int n) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        Object ref = ReflectRefs.resolve(p.ptr());
        if (!(ref instanceof Cell c)) {
            throw new IllegalStateException("reflect: SetCap of non-cell slice");
        }
        GoSlice s = (GoSlice) c.value;
        if (n < s.len() || n > s.cap()) {
            throw new IllegalStateException("reflect: slice capacity out of range in SetCap");
        }
        c.value = new GoSlice(s.array(), s.offset(), s.len(), n);
    }

    private static void reflectClear(GoStruct v) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        Object d5 = ReflectRefs.denotedValue(p.ptr(), t.kind);
        if (t.kind == GoKind.MAP) {
            GoMap m = (GoMap) d5;
            m.clear();
            return;
        }
        // Slice: zero the elements.
        GoSlice s = (GoSlice) d5;
        Object arr = s.array();
        Object zero = zeroFor(t.elem.tok);
        for (int i = 0; i < s.len(); i++) {
            ReflectValues.arraySet(arr, t.elem.kind, s.offset() + i, zero);
        }
    }

    private static void reflectSwap(GoStruct v, int i, int j) {
        // Raw swap (bypasses flagRO like gc's typedmemmove).
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        GoSlice s = (GoSlice) ReflectRefs.denotedValue(p.ptr(), t.kind);
        Object arr = s.array();
        int oi = s.offset() + i;
        int oj = s.offset() + j;
        Object tmp = ReflectValues.arrayGet(arr, t.elem.kind, oi);
        ReflectValues.arraySet(arr, t.elem.kind, oi,
                ReflectValues.arrayGet(arr, t.elem.kind, oj));
        ReflectValues.arraySet(arr, t.elem.kind, oj, tmp);
    }

    // ------------------------------------------------------------------
    // Channels.
    // ------------------------------------------------------------------

    private static graalgo.runtime.ChanOps.Chan asChan(GoStruct v) {
        Object d5 = ReflectRefs.denotedValue(ReflectValues.parts(v).ptr(), GoKind.CHAN);
        return (graalgo.runtime.ChanOps.Chan) d5;
    }

    private static void chanSend(GoStruct v, GoStruct x) {
        graalgo.runtime.ChanOps.Chan ch = asChan(v);
        Object d5x = unboxValue(x);
        graalgo.runtime.ChanOps.send(ch, d5x);
    }

    /** __reflect_recv: returns a reflectValueBoolPair struct. */
    private static GoStruct chanRecv(GoStruct v) {
        graalgo.runtime.ChanOps.Chan ch = asChan(v);
        graalgo.runtime.GoChan.RecvResult r =
                graalgo.runtime.ChanOps.recvOk(ch);
        return packValueBoolPair(r.value(), r.ok(), v);
    }

    private static boolean chanTrySend(GoStruct v, GoStruct x) {
        graalgo.runtime.ChanOps.Chan ch = asChan(v);
        Object d5x = unboxValue(x);
        // Non-blocking send via select with a default case.
        graalgo.runtime.ChanOps.SelectCase[] cases = {
                new graalgo.runtime.ChanOps.SelectCase(ch, true, d5x),
                new graalgo.runtime.ChanOps.SelectCase(null, false, null),
        };
        int chosen = graalgo.runtime.ChanOps.selectGo(cases);
        return chosen == 0;
    }

    private static void chanClose(GoStruct v) {
        graalgo.runtime.ChanOps.close(asChan(v));
    }

    /**
     * Packs a reflectValueBoolPair struct {x Value, ok bool}. The field
     * layout comes from the synthetic source: x (Value struct) then ok.
     */
    private static GoStruct packValueBoolPair(Object d5val, boolean ok, GoStruct chanValue) {
        // The element type for the received value.
        TypeInfo t = ReflectTypes.table().info(ReflectValues.parts(chanValue).typ());
        GoStructDescriptor desc = pairDescriptor();
        long[] longs = new long[desc.longsCount()];
        Object[] objects = new Object[desc.objectsCount()];
        GoStruct xv;
        if (d5val == null && !ok) {
            xv = ReflectValues.zero(ReflectTypes.valueDescriptor());
        } else {
            long h = ReflectRefs.intern(d5val);
            xv = ReflectValues.pack(ReflectTypes.valueDescriptor(), t.elem.tok, h, 0);
        }
        setPairField(desc, longs, objects, 0, xv);
        setPairField(desc, longs, objects, 1, ok ? 1L : 0L);
        return new GoStruct(desc, longs, objects);
    }

    private static GoStructDescriptor pairDescriptor() {
        // reflectValueBoolPair{ x Value; ok bool } — built from the wire.
        return ReflectTypes.structDescriptorFor("reflect.reflectValueBoolPair");
    }

    private static GoStructDescriptor pairDescFor(String name) {
        return ReflectTypes.structDescriptorFor(name);
    }

    private static void setPairField(GoStructDescriptor desc, long[] longs,
            Object[] objects, int fieldIdx, Object val) {
        GoStructDescriptor.Field f = desc.field(fieldIdx);
        if (f.inLongs()) {
            longs[f.index()] = ((Number) val).longValue();
        } else {
            objects[f.index()] = val;
        }
    }

    /** __reflect_selectimpl: returns a reflectSelectTriple struct. */
    private static GoStruct reflectSelect(GoSlice cases) {
        Object arr = cases.array();
        int off = cases.offset();
        int n = cases.len();
        // Each case is a SelectCase struct {Dir, Chan, Send}.
        List<graalgo.runtime.ChanOps.SelectCase> wire = new ArrayList<>();
        List<Integer> origIndex = new ArrayList<>();
        for (int i = 0; i < n; i++) {
            GoStruct cs = (GoStruct) ((Object[]) arr)[off + i];
            long dir = cs.getLong(0);
            // SelectCase{Dir, Chan, Send}: getObject takes FIELD indices
            // (1=Chan, 2=Send), not objects-array indices (P3-M8b fix —
            // getObject(0) threw "field Dir is not an objects-carried field").
            GoStruct chanVal = (GoStruct) cs.getObject(1);
            GoStruct sendVal = (GoStruct) cs.getObject(2);
            if (dir == 2) { // SelectDefault
                wire.add(new graalgo.runtime.ChanOps.SelectCase(null, false, null));
                origIndex.add(i);
            } else if (dir == 0) { // SelectSend
                graalgo.runtime.ChanOps.Chan ch = asChan(chanVal);
                if (ch == null) {
                    continue; // nil channel: never ready (gc-exact)
                }
                wire.add(new graalgo.runtime.ChanOps.SelectCase(
                        ch, true, unboxValue(sendVal)));
                origIndex.add(i);
            } else { // SelectRecv
                graalgo.runtime.ChanOps.Chan ch = asChan(chanVal);
                if (ch == null) {
                    continue;
                }
                wire.add(new graalgo.runtime.ChanOps.SelectCase(ch, false, null));
                origIndex.add(i);
            }
        }
        int chosen = graalgo.runtime.ChanOps.selectGo(
                wire.toArray(new graalgo.runtime.ChanOps.SelectCase[0]));
        int origChosen = origIndex.get(chosen);
        // Determine if the chosen case was a receive.
        GoStruct chosenCs = (GoStruct) ((Object[]) arr)[off + origChosen];
        long dir = chosenCs.getLong(0);
        Object recvVal = null;
        boolean recvOK = false;
        if (dir == 1) { // SelectRecv
            recvVal = graalgo.runtime.ChanOps.selectRecvValue();
            recvOK = graalgo.runtime.ChanOps.selectRecvOk();
        }
        return packSelectTriple(origChosen, recvVal, recvOK, chosenCs);
    }

    private static GoStruct packSelectTriple(int chosen, Object recvVal,
            boolean recvOK, GoStruct chosenCase) {
        GoStructDescriptor desc = pairDescFor("reflect.reflectSelectTriple");
        long[] longs = new long[desc.longsCount()];
        Object[] objects = new Object[desc.objectsCount()];
        // Fields: chosen int, recv Value, recvOK bool.
        setPairField(desc, longs, objects, 0, (long) chosen);
        GoStruct recv;
        if (recvVal == null) {
            recv = ReflectValues.zero(ReflectTypes.valueDescriptor());
        } else {
            GoStruct chanVal = (GoStruct) chosenCase.getObject(1); // Chan field
            TypeInfo t = ReflectTypes.table().info(ReflectValues.parts(chanVal).typ());
            long h = ReflectRefs.intern(recvVal);
            recv = ReflectValues.pack(ReflectTypes.valueDescriptor(), t.elem.tok, h, 0);
        }
        setPairField(desc, longs, objects, 1, recv);
        setPairField(desc, longs, objects, 2, recvOK ? 1L : 0L);
        return new GoStruct(desc, longs, objects);
    }

    // ------------------------------------------------------------------
    // Map iteration.
    // ------------------------------------------------------------------

    /** Iterator state for MapIter. */
    private static final class MapIterState {
        GoMap map;
        long keyTok;
        long elemTok;
        Object[] snapshot;
        int index;
        Object key;
        Object val;

        MapIterState(GoMap map, long keyTok, long elemTok) {
            reset(map, keyTok, elemTok);
        }

        void reset(GoMap map, long keyTok, long elemTok) {
            this.map = map;
            this.keyTok = keyTok;
            this.elemTok = elemTok;
            this.snapshot = graalgo.runtime.Intrinsics.mapIterInit(map);
            this.index = 0;
        }
    }

    private static long mapIterInit(GoStruct v) {
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        Object d5 = ReflectRefs.denotedValue(p.ptr(), t.kind);
        GoMap m = (GoMap) d5;
        return ReflectRefs.intern(new MapIterState(m, t.key.tok, t.elem.tok));
    }

    private static boolean mapIterNext(long h) {
        MapIterState st = (MapIterState) ReflectRefs.resolve(h);
        if (st.index >= st.snapshot.length) {
            return false;
        }
        // The snapshot holds keys (Intrinsics.mapIterInit returns
        // m.keysSnapshot()); the value is looked up from the live map.
        Object key = st.snapshot[st.index++];
        st.key = key;
        st.val = st.map.get(key);
        return true;
    }

    private static GoStruct mapIterKey(long h) {
        MapIterState st = (MapIterState) ReflectRefs.resolve(h);
        long handle = ReflectRefs.intern(st.key);
        return ReflectValues.pack(ReflectTypes.valueDescriptor(), st.keyTok, handle, 0);
    }

    private static GoStruct mapIterVal(long h) {
        MapIterState st = (MapIterState) ReflectRefs.resolve(h);
        long handle = ReflectRefs.intern(st.val);
        return ReflectValues.pack(ReflectTypes.valueDescriptor(), st.elemTok, handle, 0);
    }

    private static void mapIterReset(long h, GoStruct v) {
        MapIterState st = (MapIterState) ReflectRefs.resolve(h);
        ReflectValues.ValueParts p = ReflectValues.parts(v);
        TypeInfo t = ReflectTypes.table().info(p.typ());
        Object d5 = ReflectRefs.denotedValue(p.ptr(), t.kind);
        st.reset((GoMap) d5, t.key.tok, t.elem.tok);
    }

    // ------------------------------------------------------------------
    // StructField and Method packing.
    // ------------------------------------------------------------------

    /** __reflect_tField: packs a StructField struct. */
    private static GoStruct structField(long tok, int i) {
        TypeInfo t = ReflectTypes.table().info(tok);
        ReflectTypes.FieldInfo f = t.fields.get(i);
        GoStructDescriptor desc = ReflectTypes.structFieldDescriptor();
        long[] longs = new long[desc.longsCount()];
        Object[] objects = new Object[desc.objectsCount()];
        // Synthetic field order: Name, PkgPath, Type, Tag, Offset, Index, Anonymous.
        setNamedField(desc, longs, objects, "Name", GoString.fromUtf8(f.name));
        setNamedField(desc, longs, objects, "PkgPath", GoString.fromUtf8(f.pkgPath));
        setNamedField(desc, longs, objects, "Type", ReflectTypes.boxType(f.type.tok));
        setNamedField(desc, longs, objects, "Tag", GoString.fromUtf8(f.tag));
        setNamedField(desc, longs, objects, "Offset", f.offset);
        setNamedField(desc, longs, objects, "Index", intSlice(f.index));
        setNamedField(desc, longs, objects, "Anonymous", f.anonymous ? 1L : 0L);
        return new GoStruct(desc, longs, objects);
    }

    /** __reflect_tMethod: packs a Method struct. */
    private static GoStruct method(long tok, int i) {
        TypeInfo t = ReflectTypes.table().info(tok);
        ReflectTypes.MethodInfo m = t.methods.get(i);
        GoStructDescriptor desc = ReflectTypes.methodDescriptor();
        long[] longs = new long[desc.longsCount()];
        Object[] objects = new Object[desc.objectsCount()];
        // Synthetic field order: Name, PkgPath, Type, Func, Index.
        setNamedField(desc, longs, objects, "Name", GoString.fromUtf8(m.name));
        setNamedField(desc, longs, objects, "PkgPath", GoString.fromUtf8(m.pkgPath));
        setNamedField(desc, longs, objects, "Type", ReflectTypes.boxType(m.funcType.tok));
        // The method expression value: a func Value wrapping the MethodVal.
        long handle = ReflectRefs.intern(new ReflectCalls.MethodVal(m));
        GoStruct funcVal = ReflectValues.pack(ReflectTypes.valueDescriptor(),
                m.funcType.tok, handle, 0);
        setNamedField(desc, longs, objects, "Func", funcVal);
        setNamedField(desc, longs, objects, "Index", (long) m.index);
        return new GoStruct(desc, longs, objects);
    }

    /** Sets a struct field by name (using the descriptor's layout). */
    private static void setNamedField(GoStructDescriptor desc, long[] longs,
            Object[] objects, String name, Object val) {
        GoStructDescriptor.Field f = findField(desc, name);
        if (f.inLongs()) {
            longs[f.index()] = ((Number) val).longValue();
        } else {
            objects[f.index()] = val;
        }
    }

    private static GoStructDescriptor.Field findField(GoStructDescriptor desc, String name) {
        for (int i = 0; i < desc.fields().size(); i++) {
            GoStructDescriptor.Field f = desc.field(i);
            if (f.name().equals(name)) {
                return f;
            }
        }
        throw new IllegalStateException("reflect: no field " + name);
    }

    /** Boxes an int[] as a GoSlice of int. */
    private static GoSlice intSlice(int[] vals) {
        Object arr = new int[vals.length];
        System.arraycopy(vals, 0, (int[]) arr, 0, vals.length);
        return new GoSlice(arr, 0, vals.length, vals.length);
    }
}
