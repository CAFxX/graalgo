package graalgo.p3reflect;

import graalgo.runtime.ChanOps;
import graalgo.runtime.Linknames;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

import java.util.List;

/**
 * The runtime-provided half of {@code reflect} (and
 * {@code internal/reflectlite}): the bodyless helper declarations gc's
 * runtime pushes into the reflect packages via {@code //go:linkname}
 * (runtime/malloc.go, mbarrier.go, runtime1.go, chan.go, map.go).
 *
 * <p>graalgo compiles no runtime package, so these symbols dispatch
 * through the {@link Linknames} registry like every other
 * runtime-provided function (the sync lane's {@code runtime_Semacquire}
 * is the same shape). Until fix-redmaster-r41 the frontend exported
 * the declarations as "unimplemented" stubs, so live calls died with
 * the degraded panic {@code (?) 0xADDR}: {@code packEfaceData} (every
 * {@code Value.Interface()} of an addressable element, hence fmt's
 * {@code %v} of slices) at {@code unsafe_New}/{@code typedmemmove},
 * and {@code Type.String()} at {@code resolveNameOff}.
 *
 * <p>D5 calling conventions (as in {@code LinknameIntrinsics}):
 * pointer arguments arrive as Long UnsafeTable handles or as
 * {@link GoPointer} values; {@code *abi.Type} arguments denote a type
 * whose token is recovered via {@link #tokForTypeArg}; results that
 * are Go pointers are returned in the same shapes (handles for fresh
 * allocations, the registered value as-is for offset resolution).
 *
 * <p>verifies: GG-0020
 */
public final class ReflectLinknames {

    private ReflectLinknames() {
    }

    /** Registers the reflect + reflectlite runtime-helper tables. */
    public static void registerAll() {
        // fix-redmaster-r42: reflect's //go:linknamestd
        // compiledTypelinks, served through its linkname-bound
        // __-helper (see mr_disk.go): the pair type is derived from
        // the symbol by the MR pair rule (helper "__"+name, pair
        // "__"+name+"Pair", qualified with the symbol's package).
        // graalgo keeps no pre-compiled typelinks table, so both
        // slices are empty: ArrayOf/ChanOf/MapOf/SliceOf then take
        // their construct-fresh path, with canonicalization still
        // holding through their lookup caches. The one observable gap
        // this leaves is ArrayOf(n, T) identity against a statically
        // compiled [n]T type (a fresh type is constructed instead of
        // reusing the static one); direct typesByString callers
        // likewise see no pre-compiled types.
        Linknames.register("reflect.compiledTypelinks", args -> {
            GoStructDescriptor pairDesc =
                    ReflectTypes.structDescriptorFor("reflect.__compiledTypelinksPair");
            GoStruct pair = GoStruct.zero(pairDesc);
            pair.setObject(0, new GoSlice(new Object[0], 0, 0, 0));
            pair.setObject(1, new GoSlice(new Object[0], 0, 0, 0));
            return pair;
        }, 0);
        for (String pkg : new String[]{"reflect", "internal/reflectlite"}) {
            boolean full = pkg.equals("reflect");
            Linknames.register(pkg + ".unsafe_New",
                    args -> unsafeNew(tokForTypeArg(args[0], pkg + ".unsafe_New")), 1);
            Linknames.register(pkg + ".typedmemmove", args -> {
                typedmemmove(tokForTypeArg(args[0], pkg + ".typedmemmove"), args[1], args[2]);
                return null;
            }, 3);
            Linknames.register(pkg + ".resolveNameOff",
                    args -> resolveNameOff(args[1]), 2);
            Linknames.register(pkg + ".resolveTypeOff",
                    args -> resolveTypeOff(args[1]), 2);
            Linknames.register(pkg + ".chanlen",
                    args -> (long) ChanOps.len(asChan(args[0], pkg + ".chanlen")), 1);
            Linknames.register(pkg + ".maplen",
                    args -> (long) asMap(args[0], pkg + ".maplen").size(), 1);
            Linknames.register(pkg + ".ifaceE2I", args -> {
                ifaceE2I(tokForTypeArg(args[0], pkg + ".ifaceE2I"), args[1], args[2]);
                return null;
            }, 3);
            if (!full) {
                continue;
            }
            Linknames.register("reflect.unsafe_NewArray", args -> unsafeNewArray(
                    tokForTypeArg(args[0], "reflect.unsafe_NewArray"),
                    (int) num(args[1])), 2);
            Linknames.register("reflect.resolveTextOff",
                    args -> resolveTextOff(args[1]), 2);
            Linknames.register("reflect.addReflectOff",
                    args -> AbiTypes.addReflectOff(args[0]), 1);
            Linknames.register("reflect.chancap",
                    args -> (long) ChanOps.cap(asChan(args[0], "reflect.chancap")), 1);
            Linknames.register("reflect.chanclose", args -> {
                ChanOps.close(asChan(args[0], "reflect.chanclose"));
                return null;
            }, 1);
            Linknames.register("reflect.chansend0", args -> chanSend0(args[0], args[1], args[2]), 3);
            Linknames.register("reflect.memmove", args -> {
                memmove(args[0], args[1], (int) num(args[2]));
                return null;
            }, 3);
            Linknames.register("reflect.typedmemclr", args -> {
                long tok = tokForTypeArg(args[0], "reflect.typedmemclr");
                storePointerValue(args[1], tok, ReflectOpNode.zeroFor(tok),
                        "reflect.typedmemclr");
                return null;
            }, 2);
            Linknames.register("reflect.typedarrayclear", args -> {
                typedArrayClear(tokForTypeArg(args[0], "reflect.typedarrayclear"),
                        args[1], (int) num(args[2]));
                return null;
            }, 3);
            // fix-redmaster-r53: reflect.Value.Clear's map branch calls
            // mapclear(t, m) (gc runtime/map.go's reflect_mapclear,
            // linked as reflect.mapclear). Until now no implementation
            // was registered, so the first map Clear through real
            // reflect died binding the symbol — masked until r53 fixed
            // the map Kind read, which had panicked earlier with
            // ValueError{Invalid} instead of reaching the map branch.
            // gc: clearing a nil map is a no-op, so null / null-handle
            // map words clear to nothing instead of failing the
            // "not a map" check in asMap.
            Linknames.register("reflect.mapclear", args -> {
                tokForTypeArg(args[0], "reflect.mapclear");
                Object ptr = args[1];
                if (ptr == null) {
                    return null;
                }
                if (ptr instanceof Number n
                        && ReflectRefs.denotedValue(n.longValue(), GoKind.MAP) == null) {
                    return null;
                }
                asMap(ptr, "reflect.mapclear").clear();
                return null;
            }, 2);
            Linknames.register("reflect.typedslicecopy", args -> typedSliceCopy(
                    tokForTypeArg(args[0], "reflect.typedslicecopy"),
                    (GoStruct) args[1], (GoStruct) args[2]), 3);
            Linknames.register("reflect.growslice", args -> growSlice(
                    tokForTypeArg(args[0], "reflect.growslice"),
                    (GoStruct) args[1], (int) num(args[2])), 2);
            Linknames.register("reflect.makechan", args -> makeChan(
                    tokForTypeArg(args[0], "reflect.makechan"), (int) num(args[1])), 1);
            Linknames.register("reflect.makemap", args -> {
                tokForTypeArg(args[0], "reflect.makemap");
                return ReflectRefs.intern(new GoMap());
            }, 1);
        }
    }

    // ------------------------------------------------------------------
    // Allocation.
    // ------------------------------------------------------------------

    /** {@code unsafe_New(t)}: a handle to fresh zeroed storage for one t. */
    private static Object unsafeNew(long tok) {
        return ReflectRefs.intern(new ReflectRefs.Cell(ReflectOpNode.zeroFor(tok)));
    }

    /**
     * {@code unsafe_NewArray(t, n)}: a handle to fresh zeroed storage
     * for n values of t (the backing array of the slice reflect builds
     * over it — see reflect/value.go's makeSlice path).
     */
    private static Object unsafeNewArray(long tok, int n) {
        ReflectTypes.TypeInfo t = ReflectTypes.table().info(tok);
        Object arr = ReflectValues.newArray(t.kind, n);
        if (t.kind == GoKind.STRUCT && arr instanceof Object[] oa) {
            for (int i = 0; i < oa.length; i++) {
                oa[i] = ReflectOpNode.zeroFor(tok);
            }
        }
        return ReflectRefs.intern(arr);
    }

    /** {@code makechan(t, size)}: the new channel's handle. */
    private static Object makeChan(long tok, int size) {
        ReflectTypes.TypeInfo t = ReflectTypes.table().info(tok);
        long elemSize = t.elem.size < 0 ? 8 : t.elem.size;
        return ReflectRefs.intern(ChanOps.makeChan(elemSize, size));
    }

    // ------------------------------------------------------------------
    // Memory moves.
    // ------------------------------------------------------------------

    /** {@code typedmemmove(t, dst, src)}: copies one t from src to dst. */
    private static void typedmemmove(long tok, Object dst, Object src) {
        Object value = loadPointerValue(src, tok, "reflect.typedmemmove");
        storePointerValue(dst, tok, value, "reflect.typedmemmove");
    }

    /** {@code memmove(dst, src, n)}: byte copy (overlap-safe, as gc). */
    private static void memmove(Object dst, Object src, int n) {
        Object[] d = byteBacking(dst, "reflect.memmove dst");
        Object[] s = byteBacking(src, "reflect.memmove src");
        byte[] da = (byte[]) d[0];
        byte[] sa = (byte[]) s[0];
        int doff = (Integer) d[1];
        int soff = (Integer) s[1];
        if (da == sa) {
            byte[] tmp = new byte[n];
            System.arraycopy(sa, soff, tmp, 0, n);
            System.arraycopy(tmp, 0, da, doff, n);
        } else {
            System.arraycopy(sa, soff, da, doff, n);
        }
    }

    /** {@code typedarrayclear(t, ptr, len)}: zeroes len t values at ptr. */
    private static void typedArrayClear(long elemTok, Object ptr, int len) {
        ReflectTypes.TypeInfo t = ReflectTypes.table().info(elemTok);
        Object[] backing = arrayBacking(ptr, "reflect.typedarrayclear");
        Object arr = backing[0];
        int start = (Integer) backing[1];
        Object zero = ReflectOpNode.zeroFor(elemTok);
        for (int i = 0; i < len; i++) {
            ReflectValues.arraySet(arr, t.kind, start + i, zero);
        }
    }

    /**
     * {@code typedslicecopy(t, dst, src)}: copies min(len) elements of
     * the slice type t between two unsafeheader.Slice values; returns
     * the element count.
     */
    private static Object typedSliceCopy(long sliceTok, GoStruct dst, GoStruct src) {
        ReflectTypes.TypeInfo t = ReflectTypes.table().info(sliceTok);
        Object[] d = sliceBacking(dst);
        Object[] s = sliceBacking(src);
        int n = Math.min((Integer) d[2], (Integer) s[2]);
        for (int i = 0; i < n; i++) {
            Object v = ReflectValues.arrayGet(s[0], t.elem.kind, (Integer) s[1] + i);
            ReflectValues.arraySet(d[0], t.elem.kind, (Integer) d[1] + i, v);
        }
        return (long) n;
    }

    /**
     * {@code growslice(t, old, num)}: growslice semantics — the result
     * has len old.len+num; the backing array is reused when capacity
     * suffices, otherwise a fresh array with the same growth policy as
     * the reflect Append path ({@code ReflectOpNode.reflectAppend}).
     * The result is an unsafeheader.Slice struct with old's descriptor.
     */
    private static Object growSlice(long sliceTok, GoStruct old, int num) {
        ReflectTypes.TypeInfo t = ReflectTypes.table().info(sliceTok);
        Object[] s = sliceBacking(old);
        Object arr = s[0];
        int off = (Integer) s[1];
        int len = (Integer) s[2];
        int cap = (Integer) s[3];
        int newLen = len + num;
        Object dataField = sliceHeaderField(old, "Data");
        Object newArr = arr;
        int newOff = off;
        Object newData = dataField;
        int newCap = cap;
        if (newLen > cap) {
            newCap = Math.max(newLen, cap * 2);
            newArr = ReflectValues.newArray(t.elem.kind, newCap);
            for (int i = 0; i < len; i++) {
                Object v = ReflectValues.arrayGet(arr, t.elem.kind, off + i);
                ReflectValues.arraySet(newArr, t.elem.kind, i, v);
            }
            newOff = 0;
            newData = dataField instanceof GoPointer
                    ? new GoPointer(newArr, 0) : ReflectRefs.intern(newArr);
        }
        GoStruct out = GoStruct.zero(old.descriptor());
        setSliceHeader(out, newData, newLen, newCap);
        return out;
    }

    // ------------------------------------------------------------------
    // Offset resolution (gc runtime/type.go + runtime1.go).
    // ------------------------------------------------------------------

    /** {@code resolveNameOff(ptr, off)}: off 0 → nil; else the name pointer. */
    private static Object resolveNameOff(Object offArg) {
        int off = (int) num(offArg);
        if (off == 0) {
            return 0L;
        }
        Object p = AbiTypes.nameOffPointer(off);
        if (p == null) {
            throw new IllegalStateException(
                    "reflect.resolveNameOff: name offset " + off + " not in ranges");
        }
        return asGuestPointer(p);
    }

    /** {@code resolveTypeOff(ptr, off)}: off 0 or -1 → nil; else the type pointer. */
    private static Object resolveTypeOff(Object offArg) {
        int off = (int) num(offArg);
        if (off == 0 || off == -1) {
            return 0L;
        }
        Object p = AbiTypes.typeOffPointer(off);
        if (p == null) {
            throw new IllegalStateException(
                    "reflect.resolveTypeOff: type offset " + off + " not in ranges");
        }
        return asGuestPointer(p);
    }

    /**
     * Normalizes a stored pointer value to the shape a linkname
     * RESULT must take: pointer-typed results cross back into guest
     * code as Long UnsafeTable handles (the call machinery converts
     * handles to typed pointers; a raw GoPointer result is rejected
     * by that conversion). Handles and non-pointer values pass
     * through; a GoPointer is interned by its storage (element
     * pointers into byte arrays become derived byte-offset handles).
     */
    private static Object asGuestPointer(Object v) {
        if (v == null) {
            return 0L;
        }
        if (v instanceof Number) {
            return v;
        }
        if (v instanceof GoPointer p) {
            if (p.isNil()) {
                return 0L;
            }
            if (p.isElemPointer() && p.elemArray() instanceof byte[] arr) {
                long base = ReflectRefs.intern(arr);
                return graalgo.p3unsafe.UnsafeTable.current()
                        .derive(base, p.elemIndex());
            }
            if (p.isCellPointer()) {
                return ReflectRefs.intern(p.cell());
            }
            Object ref = p.referent();
            return ref == null ? 0L : ReflectRefs.intern(ref);
        }
        return v;
    }

    /** {@code resolveTextOff(ptr, off)}: off 0 → nil; else the registered pointer. */
    private static Object resolveTextOff(Object offArg) {
        int off = (int) num(offArg);
        if (off == 0) {
            return 0L;
        }
        Object p = AbiTypes.typeOffPointer(off);
        if (p == null) {
            throw new IllegalStateException(
                    "reflect.resolveTextOff: text offset " + off + " not in ranges");
        }
        return p;
    }

    // ------------------------------------------------------------------
    // Interface conversion.
    // ------------------------------------------------------------------

    /**
     * {@code ifaceE2I(t, src, dst)}: writes the iface value of interface
     * type t holding src's dynamic value into *dst. The D5 interface
     * value carries its dynamic descriptor; the target type travels
     * with the guest Value built over dst, exactly as the boxing in
     * {@code InterfaceBoxNode} leaves it.
     */
    private static void ifaceE2I(long targetTok, Object src, Object dst) {
        Object iface;
        if (src == null || src == GoInterface.NIL) {
            iface = null;
        } else if (src instanceof GoInterface gi) {
            iface = gi.isNil() ? null
                    : new GoInterface(gi.descriptor(), gi.data(), null);
        } else {
            long srcTok = ReflectValues.typeof(src);
            iface = new GoInterface(ReflectTypes.descriptorForTok(srcTok), src, null);
        }
        storePointerValue(dst, targetTok, iface, "reflect.ifaceE2I");
    }

    /** {@code chansend0(ch, val, nb)}: send the value at val on ch. */
    private static Object chanSend0(Object chPtr, Object valPtr, Object nbArg) {
        ChanOps.Chan ch = asChan(chPtr, "reflect.chansend0");
        Object val = loadRawPointer(valPtr, "reflect.chansend0");
        boolean nb = nbArg instanceof Boolean b ? b : num(nbArg) != 0;
        if (!nb) {
            ChanOps.send(ch, val);
            return Boolean.TRUE;
        }
        ChanOps.SelectCase[] cases = {
                new ChanOps.SelectCase(ch, true, val),
                new ChanOps.SelectCase(null, false, null),
        };
        return ChanOps.selectGo(cases) == 0;
    }

    // ------------------------------------------------------------------
    // Argument adaptation.
    // ------------------------------------------------------------------

    private static long num(Object o) {
        if (o instanceof Number n) {
            return n.longValue();
        }
        throw new IllegalStateException("reflect linkname: expected a number, got "
                + (o == null ? "null" : o.getClass().getName()));
    }

    /**
     * Resolves a by-value copy of a canonical shell's {@code internal/abi.Type}
     * header (fix-redmaster-r42) to its token via the copy's Str ID, with
     * Kind_ and Size_ cross-checked against the candidate. Returns 0 when
     * the copy names no minted shell or fails the cross-check — the caller
     * then fails loudly rather than resolving to a wrong type.
     */
    private static long tokForHeaderCopy(GoStruct header) {
        Long strId = headerLong(header, "Str");
        Long kind = headerLong(header, "Kind_");
        Long size = headerLong(header, "Size_");
        if (strId == null || kind == null || size == null) {
            return 0;
        }
        long tok = AbiTypes.tokForStrId(strId.intValue());
        if (tok == 0) {
            return 0;
        }
        ReflectTypes.TypeInfo info = ReflectTypes.table().info(tok);
        if (ReflectTypes.kindOrdinal(info) != kind || info.size != size) {
            return 0;
        }
        return tok;
    }

    /** The Long value of a header field, or null when absent/not longs-carried. */
    private static Long headerLong(GoStruct header, String name) {
        List<GoStructDescriptor.Field> fields = header.descriptor().fields();
        for (int i = 0; i < fields.size(); i++) {
            GoStructDescriptor.Field f = fields.get(i);
            if (name.equals(f.name()) && f.inLongs()) {
                return header.getLong(i);
            }
        }
        return null;
    }

    /**
     * Recovers the type token denoted by a {@code *abi.Type} argument:
     * a shell pointer (GoPointer to the canonical shell, or a Long
     * handle resolving to it) via {@link AbiTypes#tokForShell}, the raw
     * token itself (the shape stored in a Value's typ word), or — for a
     * header copy — the token its Str ID names (fix-redmaster-r42, see
     * below). Anything else fails loudly: an unresolvable type argument
     * is a guest-visible error, never a guess.
     */
    static long tokForTypeArg(Object arg, String symbol) {
        if (arg instanceof GoPointer p) {
            Object ref = p.referent();
            if (ref instanceof GoStruct shell) {
                long tok = AbiTypes.tokForShell(shell);
                if (tok != 0) {
                    return tok;
                }
                // Header copy (fix-redmaster-r42): guest code reinterprets
                // a *abi.Type as a *rtype (reflect's toType), and the
                // rtype's embedded t field is a by-value copy of the
                // shell's header — same fields, different object, so the
                // shell-identity lookup above misses. The copy's Str ID
                // still names its shell's token (Str IDs are minted fresh
                // per shell fill, never reused in a run); Kind_ and Size_
                // are cross-checked so a fabricated or stale header does
                // not silently resolve to the wrong type.
                if ("internal/abi.Type".equals(shell.descriptor().typeName())) {
                    long copyTok = tokForHeaderCopy(shell);
                    if (copyTok != 0) {
                        return copyTok;
                    }
                    // Guest-constructed (dynamic) type: no shell, and
                    // Str is a negative addReflectOff id, so the
                    // header-copy path cannot resolve it. Synthesize a
                    // token from the kind-extension struct reachable via
                    // the prefix-parent chain (fix-redmaster-r43; 0
                    // when no such link exists, and the original error
                    // below is preserved for truly unknown types).
                    long dynTok = synthesizeDynamicToken(shell);
                    if (dynTok != 0) {
                        return dynTok;
                    }
                }
            }
            throw new IllegalStateException(symbol
                    + ": type argument pointer does not denote a known type shell: "
                    + describe(arg));
        }
        if (arg instanceof Number n) {
            long v = n.longValue();
            if (v == 0) {
                throw new IllegalStateException(symbol + ": nil type argument");
            }
            Object ref = tryResolve(v);
            if (ref instanceof GoStruct shell) {
                long tok = AbiTypes.tokForShell(shell);
                if (tok != 0) {
                    return tok;
                }
            }
            // Not a shell handle: the raw token shape.
            ReflectTypes.table().info(v); // validates; throws on unknown
            return v;
        }
        if (arg instanceof GoStruct shell) {
            long tok = AbiTypes.tokForShell(shell);
            if (tok != 0) {
                return tok;
            }
        }
        throw new IllegalStateException(symbol + ": cannot recover a type from "
                + describe(arg));
    }

    /**
     * Dynamic type synthesis (fix-redmaster-r43).
     *
     * <p>Guest-constructed types ({@code StructOf}, {@code FuncOf},
     * {@code ArrayOf}, {@code PtrTo}, ...) have no wire entry, no
     * shell, and no token: their header's Str is a negative
     * addReflectOff id naming name bytes the guest registered, not a
     * shell. But the header is never alone — in gc a kind-extension
     * type's memory STARTS with its header, and in this model the
     * prefix-parent chain records exactly that: header →
     * {@code internal/abi.StructType} (Fields, PkgPath) →
     * {@code reflect.structType} → ..., or header →
     * {@code internal/abi.FuncType} → the {@code initFuncTypes}
     * struct value holding the args array, and so on for the other
     * kind extensions. The extension link carries everything
     * synthesis needs.
     *
     * <p>Returns 0 when no known extension link is found (the caller
     * keeps its original loud failure — an interface dynamic type,
     * or a header with no extension, is not synthesizable here).
     * Throws when the link IS found but broken (malformed metadata,
     * an unresolvable component type, a Kind_ mismatch, a synthesis
     * cycle): a broken dynamic type is a real defect, never a silent
     * fallback to the original error. Struct tokens are memoized by
     * StructType object identity (the same guest type object always
     * yields the same token; distinct objects get distinct tokens,
     * matching gc's cache identity); the composite kinds delegate to
     * the existing token families ({@code ptrTo}/{@code arrayOf}/
     * {@code sliceOf}/{@code chanOf}/{@code mapOf}/{@code funcOf}),
     * which canonicalize by structure themselves. The memo is per
     * installed table (cleared when the table changes, like every
     * other per-run table); a synthesis cycle — impossible through
     * the guest constructors, whose component types pre-exist the
     * type — fails loudly via the guard set.
     *
     * <p>Synthesized struct TypeInfos are deliberately UNNAMED
     * (name ""): gc's StructOf types have no name; their String() is
     * the structural rendering, which typeString reproduces from the
     * fields. Field pkgPaths follow the guest rule exactly
     * (structType.Field: unexported fields carry the struct type's
     * PkgPath, exported fields carry none).
     */
    private static final java.util.Map<GoStruct, Long> DYN_STRUCT_TOKS =
            new java.util.IdentityHashMap<>();
    private static ReflectTypes.Table dynStructTable;
    private static final ThreadLocal<java.util.IdentityHashMap<GoStruct, Boolean>> SYNTHESIZING =
            ThreadLocal.withInitial(java.util.IdentityHashMap::new);

    private static long synthesizeDynamicToken(GoStruct header) {
        GoStruct ext = null;
        String extName = null;
        GoStruct cur = header.prefixParent();
        for (int depth = 0; depth < 8 && cur != null; depth++) {
            String tn = cur.descriptor().typeName();
            switch (tn) {
                case "internal/abi.StructType", "internal/abi.PtrType",
                        "internal/abi.ArrayType", "internal/abi.SliceType",
                        "internal/abi.ChanType", "internal/abi.MapType",
                        "internal/abi.FuncType" -> {
                    ext = cur;
                    extName = tn;
                }
                default -> {
                    // Not an extension link; keep walking (a structType
                    // wrapper or uncommon struct may sit above it).
                }
            }
            if (ext != null) {
                break;
            }
            cur = cur.prefixParent();
        }
        if (ext == null) {
            return 0;
        }
        java.util.IdentityHashMap<GoStruct, Boolean> guard = SYNTHESIZING.get();
        if (guard.containsKey(ext)) {
            throw new IllegalStateException(
                    "reflect: cyclic dynamic type (already synthesizing " + extName + ")");
        }
        guard.put(ext, Boolean.TRUE);
        try {
            return switch (extName) {
                case "internal/abi.StructType" -> synthesizeDynamicStruct(header, ext);
                case "internal/abi.PtrType" -> ReflectTypes.ptrTo(
                        tokForTypeArg(fieldObj(ext, "Elem"), "reflect: dynamic pointer element type"));
                case "internal/abi.ArrayType" -> {
                    long elemTok = tokForTypeArg(fieldObj(ext, "Elem"),
                            "reflect: dynamic array element type");
                    long len = fieldLong(ext, "Len");
                    if (len < 0 || len > Integer.MAX_VALUE) {
                        throw new IllegalStateException(
                                "reflect: dynamic array length out of range: " + len);
                    }
                    yield ReflectTypes.arrayOf(elemTok, (int) len);
                }
                case "internal/abi.SliceType" -> ReflectTypes.sliceOf(
                        tokForTypeArg(fieldObj(ext, "Elem"), "reflect: dynamic slice element type"));
                case "internal/abi.ChanType" -> {
                    long elemTok = tokForTypeArg(fieldObj(ext, "Elem"),
                            "reflect: dynamic chan element type");
                    long dir = fieldLong(ext, "Dir");
                    // gc ChanDir: RecvDir=1, SendDir=2, BothDir=3; the
                    // Java enum order is RECV, SEND, BOTH.
                    int dirOrd = switch ((int) dir) {
                        case 1 -> 0;
                        case 2 -> 1;
                        case 3 -> 2;
                        default -> throw new IllegalStateException(
                                "reflect: dynamic chan direction out of range: " + dir);
                    };
                    yield ReflectTypes.chanOf(elemTok, dirOrd);
                }
                case "internal/abi.MapType" -> ReflectTypes.mapOf(
                        tokForTypeArg(fieldObj(ext, "Key"), "reflect: dynamic map key type"),
                        tokForTypeArg(fieldObj(ext, "Elem"), "reflect: dynamic map element type"));
                case "internal/abi.FuncType" -> synthesizeDynamicFunc(header, ext, ext.prefixParent());
                default -> 0;
            };
        } finally {
            guard.remove(ext);
        }
    }

    private static long synthesizeDynamicStruct(GoStruct header, GoStruct structType) {
        ReflectTypes.Table table = ReflectTypes.table();
        if (table != dynStructTable) {
            DYN_STRUCT_TOKS.clear();
            dynStructTable = table;
        }
        Long memo = DYN_STRUCT_TOKS.get(structType);
        if (memo != null) {
            return memo;
        }
        long kind = fieldLong(header, "Kind_");
        if (kind != 25) {
            throw new IllegalStateException(
                    "reflect: dynamic struct header has Kind_ " + kind + ", not Struct (25)");
        }
        long size = fieldLong(header, "Size_");
        long align = fieldLong(header, "Align_");
        Object fieldsObj = fieldObj(structType, "Fields");
        if (!(fieldsObj instanceof GoSlice fieldsSlice)) {
            throw new IllegalStateException(
                    "reflect: dynamic structType Fields is not a slice: " + fieldsObj);
        }
        Object pkgPathObj = fieldObj(structType, "PkgPath");
        String structPkgPath = "";
        if (pkgPathObj instanceof GoStruct pkgPathName) {
            structPkgPath = decodeAbiName(pkgPathName).name();
        } else if (pkgPathObj != null) {
            throw new IllegalStateException(
                    "reflect: dynamic structType PkgPath is not a Name struct: " + pkgPathObj);
        }
        Object arrObj = fieldsSlice.array();
        if (!(arrObj instanceof Object[] arr)) {
            throw new IllegalStateException(
                    "reflect: dynamic structType Fields backing is not Object[]: " + arrObj);
        }
        java.util.List<ReflectTypes.FieldInfo> fields = new java.util.ArrayList<>();
        for (int i = 0; i < fieldsSlice.len(); i++) {
            Object el = arr[fieldsSlice.offset() + i];
            if (!(el instanceof GoStruct sf)
                    || !"internal/abi.StructField".equals(sf.descriptor().typeName())) {
                throw new IllegalStateException(
                        "reflect: dynamic struct field " + i + " is not an abi.StructField: " + el);
            }
            Object nameObj = fieldObj(sf, "Name");
            if (!(nameObj instanceof GoStruct nameStruct)) {
                throw new IllegalStateException(
                        "reflect: dynamic struct field " + i + " Name is not a Name struct");
            }
            NameParts np = decodeAbiName(nameStruct);
            Object typObj = fieldObj(sf, "Typ");
            long fieldTok = tokForTypeArg(typObj, "reflect: dynamic struct field type");
            long offset = fieldLong(sf, "Offset");
            String fpkg = np.exported() ? "" : structPkgPath;
            fields.add(new ReflectTypes.FieldInfo(np.name(), fpkg,
                    ReflectTypes.table().info(fieldTok), np.tag(), offset,
                    new int[]{i}, np.embedded()));
        }
        long newTok = table.byTok.size();
        ReflectTypes.TypeInfo info = new ReflectTypes.TypeInfo(newTok, GoKind.STRUCT,
                "", "", size, (int) align,
                null, null, 0, null, java.util.List.copyOf(fields), java.util.List.of(),
                java.util.List.of(), java.util.List.of(), false, java.util.List.of(), "", -1);
        table.byTok.add(info);
        DYN_STRUCT_TOKS.put(structType, newTok);
        return newTok;
    }

    /**
     * Dynamic func-type synthesis (fix-redmaster-r43). A FuncOf
     * product's ins/outs are NOT in the funcType struct itself: in gc
     * the {@code [n]*rtype} args array directly follows the funcType
     * in the {@code initFuncTypes} allocation, and in this model that
     * allocation is the parent struct (the funcType struct is its
     * field 0, so the parent is the funcType's prefix parent) whose
     * {@code Args} field holds the array. InCount/OutCount come from
     * the funcType struct (OutCount's top bit is the variadic flag);
     * the token delegates to the {@code funcOf} family, which
     * canonicalizes by (ins, outs, variadic) like gc's func cache.
     */
    private static long synthesizeDynamicFunc(GoStruct header, GoStruct funcType, GoStruct parent) {
        long kind = fieldLong(header, "Kind_");
        if (kind != 19) {
            throw new IllegalStateException(
                    "reflect: dynamic func header has Kind_ " + kind + ", not Func (19)");
        }
        long inCount = fieldLong(funcType, "InCount");
        long outCountRaw = fieldLong(funcType, "OutCount");
        boolean variadic = (outCountRaw & 0x8000) != 0;
        long outCount = outCountRaw & 0x7FFF;
        long n = inCount + outCount;
        if (parent == null) {
            throw new IllegalStateException(
                    "reflect: dynamic func type has no initFunc parent (no Args array)");
        }
        Object argsObj = fieldObj(parent, "Args");
        // The Args field is an ARRAY ([n]*rtype), stored as a raw
        // Object[]; a GoSlice over it is accepted too (same elements).
        Object[] arr;
        int arrOff;
        if (argsObj instanceof Object[] oa) {
            arr = oa;
            arrOff = 0;
        } else if (argsObj instanceof GoSlice argsSlice) {
            if (!(argsSlice.array() instanceof Object[] oa)) {
                throw new IllegalStateException(
                        "reflect: dynamic func Args backing is not Object[]: " + argsSlice.array());
            }
            arr = oa;
            arrOff = argsSlice.offset();
        } else {
            throw new IllegalStateException(
                    "reflect: dynamic func parent Args is neither array nor slice: " + argsObj);
        }
        if (arr.length - arrOff < n) {
            throw new IllegalStateException(
                    "reflect: dynamic func Args len " + (arr.length - arrOff) + " < " + n);
        }
        long[] inToks = new long[(int) inCount];
        long[] outToks = new long[(int) outCount];
        for (int i = 0; i < n; i++) {
            Object el = arr[arrOff + i];
            // fix-redmaster-r43: Args elements are *rtype Type
            // values, not header pointers — resolve them through
            // the Type-value walk (rtype t field), not the
            // header-only tokForTypeArg (which cannot resolve an
            // rtype view struct and failed loudly here).
            long tok = ReflectOpNode.resolveTypeValue(el, "reflect: dynamic func arg type");
            if (i < inCount) {
                inToks[i] = tok;
            } else {
                outToks[(int) (i - inCount)] = tok;
            }
        }
        return ReflectTypes.funcOf(inToks, outToks, variadic);
    }

    /** A struct field by name (ordinal lookup; fails loudly when absent). */
    private static Object fieldObj(GoStruct s, String name) {
        GoStructDescriptor d = s.descriptor();
        for (int i = 0; i < d.fields().size(); i++) {
            if (name.equals(d.field(i).name())) {
                if (d.field(i).inLongs()) {
                    throw new IllegalStateException(
                            "reflect: field " + name + " of " + d.typeName() + " is longs-carried");
                }
                return s.getObject(i);
            }
        }
        throw new IllegalStateException(
                "reflect: no field " + name + " on " + d.typeName());
    }

    /** A longs-carried struct field by name (fails loudly when absent). */
    private static long fieldLong(GoStruct s, String name) {
        GoStructDescriptor d = s.descriptor();
        for (int i = 0; i < d.fields().size(); i++) {
            if (name.equals(d.field(i).name())) {
                if (!d.field(i).inLongs()) {
                    throw new IllegalStateException(
                            "reflect: field " + name + " of " + d.typeName() + " is objects-carried");
                }
                return s.getLong(i);
            }
        }
        throw new IllegalStateException(
                "reflect: no field " + name + " on " + d.typeName());
    }

    /** Decoded {@code internal/abi.Name} bytes (see abi/type.go Name). */
    private record NameParts(String name, String tag, boolean exported, boolean embedded) {}

    /**
     * Decodes an abi.Name struct's inline parts (fix-redmaster-r43).
     * Encoding (internal/abi/type.go): byte 0 flags (bit 0 exported,
     * bit 1 has-tag, bit 3 embedded), then a varint name length and
     * the name bytes, then — when the tag bit is set — a varint tag
     * length and the tag bytes. The Bytes pointer is an element
     * pointer into a fresh byte[] in practice (newName's {@code &b[0]});
     * a plain byte[] referent is accepted too. Truncation or an
     * unhandled pointer shape fails loudly: a mis-decoded name would
     * silently corrupt field identity. The pkgPath offset form (bit 2)
     * is NOT decoded here — struct field pkgPaths come from the
     * struct type's PkgPath Name, per the guest rule, never from the
     * field Name's trailing offset.
     */
    private static NameParts decodeAbiName(GoStruct nameStruct) {
        Object bytesObj = fieldObj(nameStruct, "Bytes");
        if (bytesObj == null) {
            return new NameParts("", "", true, false);
        }
        if (!(bytesObj instanceof GoPointer gp)) {
            throw new IllegalStateException(
                    "reflect: abi.Name Bytes is not a pointer: " + bytesObj);
        }
        byte[] bytes;
        int base;
        if (gp.isElemPointer() && gp.elemArray() instanceof byte[] ba) {
            bytes = ba;
            base = gp.elemIndex();
        } else if (gp.isCellPointer() && gp.cell().get() instanceof byte[] ba) {
            bytes = ba;
            base = 0;
        } else if (gp.referent() instanceof byte[] ba) {
            bytes = ba;
            base = 0;
        } else {
            throw new IllegalStateException(
                    "reflect: abi.Name Bytes pointer shape not decodable: " + gp);
        }
        if (base < 0 || base >= bytes.length) {
            throw new IllegalStateException("reflect: abi.Name Bytes base out of range");
        }
        int flags = bytes[base] & 0xFF;
        boolean exported = (flags & 1) != 0;
        boolean hasTag = (flags & 2) != 0;
        boolean embedded = (flags & 8) != 0;
        int[] nameVarint = readNameVarint(bytes, base + 1);
        int nameLen = nameVarint[1];
        int nameOff = base + 1 + nameVarint[0];
        if (nameOff < 0 || nameOff + nameLen > bytes.length) {
            throw new IllegalStateException("reflect: abi.Name name bytes truncated");
        }
        String name = new String(bytes, nameOff, nameLen, java.nio.charset.StandardCharsets.UTF_8);
        String tag = "";
        if (hasTag) {
            int[] tagVarint = readNameVarint(bytes, nameOff + nameLen);
            int tagLen = tagVarint[1];
            int tagOff = nameOff + nameLen + tagVarint[0];
            if (tagOff < 0 || tagOff + tagLen > bytes.length) {
                throw new IllegalStateException("reflect: abi.Name tag bytes truncated");
            }
            tag = new String(bytes, tagOff, tagLen, java.nio.charset.StandardCharsets.UTF_8);
        }
        return new NameParts(name, tag, exported, embedded);
    }

    /** abi varint (7 bits per byte, little-endian groups; max 10 bytes). */
    private static int[] readNameVarint(byte[] bytes, int off) {
        int v = 0;
        for (int i = 0; i < 10; i++) {
            if (off + i >= bytes.length) {
                throw new IllegalStateException("reflect: abi.Name varint truncated");
            }
            int x = bytes[off + i] & 0xFF;
            v += (x & 0x7F) << (7 * i);
            if ((x & 0x80) == 0) {
                return new int[]{i + 1, v};
            }
        }
        throw new IllegalStateException("reflect: abi.Name varint too long");
    }

    /** UnsafeTable resolve that yields null instead of throwing. */
    private static Object tryResolve(long handle) {
        try {
            return ReflectRefs.resolve(handle);
        } catch (RuntimeException e) {
            return null;
        }
    }

    /** The value denoted by a guest pointer for type tok (a copy source). */
    static Object loadPointerValue(Object ptr, long tok, String symbol) {
        ReflectTypes.TypeInfo t = ReflectTypes.table().info(tok);
        if (ptr instanceof Number n) {
            long h = n.longValue();
            if (h == 0) {
                throw new IllegalStateException(symbol + ": load through nil pointer");
            }
            // A handle may carry a byte-offset view of an array root
            // (UnsafeTable.ByteRef — the shape of &slice[i] after
            // unsafe.Pointer arithmetic, e.g. the source pointer of
            // packEfaceData's typedmemmove): read the element there.
            // Offset-0 handles go through the offset-0 loader below
            // (cells, field/element refs, bare referents, plus the
            // prefix descent for struct/array referents).
            graalgo.p3unsafe.UnsafeTable.ResolvedView view =
                    graalgo.p3unsafe.UnsafeTable.current().resolveView(h);
            if (view.byteOffset() == 0) {
                return loadOffsetZero(h, t, symbol);
            }
            return ReflectValues.arrayGet(arrayRoot(view, symbol), t.kind,
                    elementIndex(view, t, symbol));
        }
        if (ptr instanceof GoPointer p) {
            if (p.isNil()) {
                throw new IllegalStateException(symbol + ": load through nil pointer");
            }
            return p.referent();
        }
        throw new IllegalStateException(symbol + ": cannot load through " + describe(ptr));
    }

    /**
     * Loads a value of type {@code target} through an offset-0 handle
     * (fix-redmaster-r42): the ordinary denoted value, plus offset-0
     * prefix descent for struct/array referents. A handle denoting a
     * struct read as a different type denotes that struct's first data
     * field (gc: offset 0 is the first field's first byte, recursively);
     * a handle denoting an array read as a different type denotes its
     * element 0. This is the shape reflect's {@code Field} produces:
     * {@code add(ptr, 0)} collapses to the base handle in
     * {@code UnsafeTable.derive}, so the field provenance is only
     * recoverable from the referent's shape — without the descent, a
     * typedmemmove of a struct field's value loads the whole struct
     * (the 47_reflect_funcof_structof {@code Interface()} CCE: an int
     * interface bound to the {@code main.E} struct). A handle denoting
     * the target type itself, and any non-container value (slices,
     * strings, interfaces — whose offset 0 is a header word, not an
     * element), loads unchanged. A container with no data at offset 0
     * (empty struct/array) or an unidentifiable struct fails loudly
     * rather than returning the container as the value.
     */
    static Object loadOffsetZero(long handle, ReflectTypes.TypeInfo target, String symbol) {
        Object v = ReflectRefs.denotedValue(handle, target.kind);
        while (true) {
            if (v instanceof GoStruct gs) {
                ReflectTypes.TypeInfo info = ReflectTypes.infoForName(gs.descriptor().typeName());
                if (info == null) {
                    if (target.kind == GoKind.STRUCT) {
                        return v;
                    }
                    // Unnamed struct (guest-built StructOf products
                    // have no name index): descend offset-0 fields
                    // via the value descriptor instead of TypeInfo
                    // field offsets.
                    return loadOffsetZeroUnnamed(gs, target, symbol);
                }
                if (info.tok == target.tok) {
                    return v;
                }
                if (info.fields == null) {
                    throw new IllegalStateException(symbol
                            + ": cannot load " + target.name + " at offset 0 of struct "
                            + info.name + " (no field layout)");
                }
                ReflectTypes.FieldInfo f = firstDataFieldAtZero(info);
                if (f == null || f.type == null || f.index.length == 0) {
                    throw new IllegalStateException(symbol
                            + ": cannot load " + target.name + " at offset 0 of struct "
                            + info.name + " (no data field at offset 0)");
                }
                v = ReflectValues.structFieldGet(gs, f.index[0], f.type.kind);
            } else if (isArray(v)) {
                if (target.kind == GoKind.ARRAY) {
                    return v;
                }
                int len = java.lang.reflect.Array.getLength(v);
                if (len == 0) {
                    throw new IllegalStateException(symbol
                            + ": cannot load " + target.name + " at offset 0 of empty array");
                }
                v = arrayElemAtZero(v, target, symbol);
            } else {
                return v;
            }
        }
    }

    /**
     * Offset-0 descent for unnamed structs (see {@link #loadOffsetZero}):
     * walks field 0 while it is a struct, then reads field 0 with the
     * target kind's carrier decoding. A zero-sized struct at field 0
     * (or any other shape that cannot yield the target kind) fails
     * loudly rather than returning a wrong value.
     */
    private static Object loadOffsetZeroUnnamed(GoStruct gs, ReflectTypes.TypeInfo target, String symbol) {
        Object cur = gs;
        while (true) {
            if (cur instanceof GoStruct s) {
                graalgo.values.GoStructDescriptor d = s.descriptor();
                if (d.fields().size() == 0) {
                    throw new IllegalStateException(symbol
                            + ": cannot load " + target.name + " at offset 0 of empty struct "
                            + d.typeName());
                }
                graalgo.values.GoStructDescriptor.Field f0 = d.field(0);
                if (f0.kind() == GoKind.STRUCT) {
                    cur = ReflectValues.structFieldGet(s, 0, GoKind.STRUCT);
                    continue;
                }
                if (f0.kind() == target.kind) {
                    return ReflectValues.structFieldGet(s, 0, target.kind);
                }
                throw new IllegalStateException(symbol
                        + ": cannot load " + target.name + " at offset 0 of unnamed struct "
                        + d.typeName() + " (field 0 kind " + f0.kind() + ")");
            } else if (isArray(cur)) {
                if (target.kind == GoKind.ARRAY) {
                    return cur;
                }
                int len = java.lang.reflect.Array.getLength(cur);
                if (len == 0) {
                    throw new IllegalStateException(symbol
                            + ": cannot load " + target.name + " at offset 0 of empty array");
                }
                cur = arrayElemAtZero(cur, target, symbol);
            } else {
                return cur;
            }
        }
    }

    /** The first field with data at byte offset 0 (skipping zero-sized fields). */
    private static ReflectTypes.FieldInfo firstDataFieldAtZero(ReflectTypes.TypeInfo info) {
        for (ReflectTypes.FieldInfo f : info.fields) {
            if (f.offset == 0 && f.type != null && f.type.size > 0) {
                return f;
            }
        }
        return null;
    }

    /** Element 0 of a Java array, read as the target kind's carrier. */
    private static Object arrayElemAtZero(Object array, ReflectTypes.TypeInfo target, String symbol) {
        if (array instanceof Object[] oa) {
            return oa[0];
        }
        GoKind k = target.kind;
        boolean ok = (array instanceof byte[] && (k == GoKind.INT8 || k == GoKind.UINT8))
                || (array instanceof short[] && (k == GoKind.INT16 || k == GoKind.UINT16))
                || (array instanceof int[] && (k == GoKind.INT32 || k == GoKind.UINT32))
                || (array instanceof long[] && (k == GoKind.INT64 || k == GoKind.UINT64))
                || (array instanceof float[] && k == GoKind.FLOAT32)
                || (array instanceof double[] && k == GoKind.FLOAT64)
                || (array instanceof boolean[] && k == GoKind.BOOL);
        if (!ok) {
            throw new IllegalStateException(symbol
                    + ": cannot load " + target.name + " at offset 0 of array "
                    + array.getClass().getSimpleName());
        }
        return ReflectValues.arrayGet(array, k, 0);
    }

    /** The Java array at a resolved view's root (unwrapping ArrayRef/GoSlice). */
    private static Object arrayRoot(
            graalgo.p3unsafe.UnsafeTable.ResolvedView view, String symbol) {
        Object root = view.root();
        if (root instanceof graalgo.p3unsafe.UnsafeTable.ArrayRef ar) {
            return ar.array();
        }
        if (root instanceof GoSlice sl) {
            return sl.array();
        }
        if (isArray(root)) {
            return root;
        }
        throw new IllegalStateException(symbol
                + ": byte-offset view root is not an array: " + describe(root));
    }

    /** The element index a resolved view denotes for values of type t. */
    private static int elementIndex(
            graalgo.p3unsafe.UnsafeTable.ResolvedView view,
            ReflectTypes.TypeInfo t, String symbol) {
        Object root = view.root();
        int base = 0;
        if (root instanceof graalgo.p3unsafe.UnsafeTable.ArrayRef ar) {
            base = ar.offset();
        } else if (root instanceof GoSlice sl) {
            base = sl.offset();
        }
        long size = t.size;
        if (size <= 0) {
            return base;
        }
        if (view.byteOffset() % size != 0) {
            throw new IllegalStateException(symbol + ": byte offset "
                    + view.byteOffset() + " is not a multiple of the element size "
                    + size);
        }
        return base + (int) (view.byteOffset() / size);
    }

    /** A kind-free load (the referent is already the canonical D5 value). */
    private static Object loadRawPointer(Object ptr, String symbol) {
        if (ptr instanceof Number n) {
            long h = n.longValue();
            if (h == 0) {
                throw new IllegalStateException(symbol + ": load through nil pointer");
            }
            return ReflectRefs.load(h);
        }
        if (ptr instanceof GoPointer p) {
            return p.referent();
        }
        // A direct D5 value (the shape of Value.pointer() for
        // direct-iface kinds): the value itself.
        return ptr;
    }

    /**
     * Stores a value of type tok through a guest pointer (a copy
     * destination). Aggregates are copied into the destination's
     * existing storage where it has any (gc copies memory; aliases of
     * the destination observe the copy), and into fresh storage for
     * cell destinations.
     */
    static void storePointerValue(Object ptr, long tok, Object value, String symbol) {
        ReflectTypes.TypeInfo t = ReflectTypes.table().info(tok);
        boolean aggregate = t.kind == GoKind.STRUCT || t.kind == GoKind.ARRAY;
        if (ptr instanceof Number n) {
            long h = n.longValue();
            if (h == 0) {
                throw new IllegalStateException(symbol + ": store through nil pointer");
            }
            // Byte-offset view of an array root: write the element
            // (the mirror of loadPointerValue's read).
            graalgo.p3unsafe.UnsafeTable.ResolvedView view =
                    graalgo.p3unsafe.UnsafeTable.current().resolveView(h);
            if (view.byteOffset() != 0) {
                if (aggregate) {
                    throw new IllegalStateException(symbol
                            + ": aggregate store through a byte-offset view");
                }
                ReflectValues.arraySet(arrayRoot(view, symbol), t.kind,
                        elementIndex(view, t, symbol), value);
                return;
            }
            Object ref = ReflectRefs.resolve(h);
            if (ref instanceof ReflectRefs.Cell c) {
                // fix-redmaster-r43: a store through a collapsed
                // handle (reflect Field's add(ptr, 0)) whose cell
                // holds a struct lands in the struct's offset-0
                // field chain (the mirror of loadOffsetZero's read
                // descent), not by replacing the cell's struct —
                // replacing it orphaned every other alias of the
                // struct (47's ptr-embed Set). Best-effort: a
                // non-matching shape keeps the replacing store.
                if (!aggregate && c.value instanceof GoStruct gs
                        && storeOffsetZeroField(gs, t.kind, value)) {
                    return;
                }
                c.value = aggregate ? copyOfAggregate(t, value) : value;
                return;
            }
            if (ref instanceof graalgo.values.GoCell gc) {
                gc.set(aggregate ? copyOfAggregate(t, value) : value);
                return;
            }
            if (ref instanceof ReflectRefs.FieldRef
                    || ref instanceof ReflectRefs.ArrayElemRef
                    || ref instanceof ReflectRefs.SliceElemRef) {
                if (aggregate) {
                    Object dstAgg = ReflectRefs.denotedValue(h, t.kind);
                    ReflectOpNode.copyAggregate(t, dstAgg, value);
                } else {
                    ReflectOpNode.storeThroughRef(ref, t.kind, value);
                }
                return;
            }
            if (aggregate && (ref instanceof GoStruct || isArray(ref))) {
                ReflectOpNode.copyAggregate(t, ref, value);
                return;
            }
            throw new IllegalStateException(symbol + ": cannot store through "
                    + describe(ref));
        }
        if (ptr instanceof GoPointer p) {
            if (p.isNil()) {
                throw new IllegalStateException(symbol + ": store through nil pointer");
            }
            Object referent = p.referent();
            if (aggregate && (referent instanceof GoStruct || isArray(referent))) {
                ReflectOpNode.copyAggregate(t, referent, value);
            } else if (!aggregate && referent instanceof ReflectRefs.Cell c
                    && c.value instanceof GoStruct gs
                    && storeOffsetZeroField(gs, t.kind, value)) {
                // Offset-0 field store through a collapsed cell
                // pointer (mirror of the Cell-handle branch above).
            } else if (!aggregate && referent instanceof GoStruct gs
                    && storeOffsetZeroField(gs, t.kind, value)) {
                // Offset-0 field store through a plain struct pointer.
            } else {
                p.setReferent(aggregate ? copyOfAggregate(t, value) : value);
            }
            return;
        }
        throw new IllegalStateException(symbol + ": cannot store through " + describe(ptr));
    }

    /**
     * Stores a non-aggregate value in the offset-0 field chain of
     * struct gs (fix-redmaster-r43, the write mirror of
     * {@link #loadOffsetZero}'s read descent): walks struct field 0s
     * until the first non-struct field 0, which must have the stored
     * kind. Returns false for a zero-sized struct field 0, a kind
     * mismatch, or a chain deeper than 64 (caller keeps its previous
     * behavior); never stores into a mismatched field.
     */
    private static boolean storeOffsetZeroField(GoStruct gs, GoKind kind, Object value) {
        if (kind == GoKind.STRUCT) {
            return false;
        }
        GoStruct cur = gs;
        int depth = 0;
        while (true) {
            GoStructDescriptor d = cur.descriptor();
            if (d.fields().size() == 0 || depth++ > 64) {
                return false;
            }
            GoStructDescriptor.Field f0 = d.field(0);
            if (f0.kind() == GoKind.STRUCT) {
                Object next = cur.getObject(0);
                if (!(next instanceof GoStruct ns)) {
                    return false;
                }
                cur = ns;
                continue;
            }
            if (f0.kind() == kind) {
                ReflectValues.structFieldSet(cur, 0, kind, value);
                return true;
            }
            return false;
        }
    }

    /** A fresh aggregate holding a shallow copy of {@code value} (gc copy semantics). */
    private static Object copyOfAggregate(ReflectTypes.TypeInfo t, Object value) {
        Object fresh = ReflectOpNode.zeroFor(t.tok);
        ReflectOpNode.copyAggregate(t, fresh, value);
        return fresh;
    }

    private static boolean isArray(Object o) {
        return o != null && o.getClass().isArray();
    }

    private static ChanOps.Chan asChan(Object ptr, String symbol) {
        // The guest's Value.pointer() for a direct (non-indir) chan
        // value is the channel object itself; addressable values pass
        // a handle or GoPointer to their storage.
        Object ref = ptr instanceof Number n
                ? ReflectRefs.denotedValue(n.longValue(), GoKind.CHAN)
                : ptr instanceof GoPointer p ? p.referent() : ptr;
        if (ref instanceof ChanOps.Chan ch) {
            return ch;
        }
        throw new IllegalStateException(symbol + ": not a channel: " + describe(ptr));
    }

    private static GoMap asMap(Object ptr, String symbol) {
        Object ref = ptr instanceof Number n
                ? ReflectRefs.denotedValue(n.longValue(), GoKind.MAP)
                : ptr instanceof GoPointer p ? p.referent() : ptr;
        if (ref instanceof GoMap m) {
            return m;
        }
        throw new IllegalStateException(symbol + ": not a map: " + describe(ptr));
    }

    // ------------------------------------------------------------------
    // Slice-header and byte-backing adaptation.
    // ------------------------------------------------------------------

    /** Reads an unsafeheader.Slice struct field by name. */
    private static Object sliceHeaderField(GoStruct s, String name) {
        GoStructDescriptor desc = s.descriptor();
        for (int i = 0; i < desc.fields().size(); i++) {
            GoStructDescriptor.Field f = desc.field(i);
            if (f.name().equals(name)) {
                return ReflectValues.structFieldGet(s, i, f.kind());
            }
        }
        throw new IllegalStateException("reflect: slice header has no field " + name);
    }

    /** Writes an unsafeheader.Slice struct's Data/Len/Cap fields. */
    private static void setSliceHeader(GoStruct s, Object data, long len, long cap) {
        GoStructDescriptor desc = s.descriptor();
        for (int i = 0; i < desc.fields().size(); i++) {
            GoStructDescriptor.Field f = desc.field(i);
            switch (f.name()) {
                case "Data" -> ReflectValues.structFieldSet(s, i, f.kind(), data);
                case "Len" -> ReflectValues.structFieldSet(s, i, f.kind(), len);
                case "Cap" -> ReflectValues.structFieldSet(s, i, f.kind(), cap);
                default -> {
                }
            }
        }
    }

    /** {array, offset, len, cap} for an unsafeheader.Slice struct value. */
    private static Object[] sliceBacking(GoStruct s) {
        Object data = sliceHeaderField(s, "Data");
        int len = (int) num(sliceHeaderField(s, "Len"));
        int cap = (int) num(sliceHeaderField(s, "Cap"));
        Object[] backing = arrayBacking(data, "reflect slice header");
        return new Object[]{backing[0], backing[1], len, cap};
    }

    /**
     * {array, offset} for a pointer denoting an array's storage: an
     * element pointer into the array, a plain pointer to it, or a
     * handle resolving to it (directly or through a cell).
     *
     * <p>fix-redmaster-r53: a slice header's Data word is an
     * {@code ArrayRef} handle (array + offset, minted by
     * {@code UnsafeOps.sliceData} for the header view), not a direct
     * array handle — so a {@code typedarrayclear} over a reflect
     * slice value (whose Data word comes from that view) died here
     * with "pointer does not denote an array". An {@code ArrayRef}
     * resolves to its backing array at its offset, in both the
     * GoPointer and the handle shapes, exactly as the byte-offset
     * view helpers ({@code arrayRoot}) already unwrap it.
     */
    private static Object[] arrayBacking(Object ptr, String symbol) {
        if (ptr instanceof GoPointer p) {
            if (p.isElemPointer()) {
                return new Object[]{p.elemArray(), p.elemIndex()};
            }
            Object ref = p.referent();
            if (isArray(ref)) {
                return new Object[]{ref, 0};
            }
            if (ref instanceof GoSlice sl) {
                return new Object[]{sl.array(), sl.offset()};
            }
            if (ref instanceof graalgo.p3unsafe.UnsafeTable.ArrayRef ar) {
                return new Object[]{ar.array(), ar.offset()};
            }
        }
        if (ptr instanceof Number n) {
            Object ref = ReflectRefs.resolve(n.longValue());
            Object v = ReflectRefs.deref(ref);
            if (isArray(v)) {
                return new Object[]{v, 0};
            }
            if (v instanceof GoSlice sl) {
                return new Object[]{sl.array(), sl.offset()};
            }
            if (v instanceof graalgo.p3unsafe.UnsafeTable.ArrayRef ar) {
                return new Object[]{ar.array(), ar.offset()};
            }
        }
        throw new IllegalStateException(symbol + ": pointer does not denote an array: "
                + describe(ptr));
    }

    /** {byte[], offset} for a pointer denoting byte storage. */
    private static Object[] byteBacking(Object ptr, String symbol) {
        Object[] b = arrayBackingOrNull(ptr);
        if (b != null && b[0] instanceof byte[]) {
            return b;
        }
        // A GoString's bytes are a valid memmove source.
        Object ref = ptr instanceof GoPointer p ? p.referent()
                : ptr instanceof Number n ? tryResolve(n.longValue()) : null;
        Object v = ref == null ? null : ReflectRefs.deref(ref);
        if (v instanceof graalgo.values.GoString gs) {
            return new Object[]{gs.bytes(), 0};
        }
        throw new IllegalStateException(symbol + ": pointer does not denote bytes: "
                + describe(ptr));
    }

    private static Object[] arrayBackingOrNull(Object ptr) {
        try {
            return arrayBacking(ptr, "reflect");
        } catch (RuntimeException e) {
            return null;
        }
    }

    /** Short shape description for fail-fast messages. */
    private static String describe(Object o) {
        if (o == null) {
            return "null";
        }
        return o.getClass().getName() + "(" + o + ")";
    }

}
