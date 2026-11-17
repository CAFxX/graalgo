package graalgo.p3unsafe;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.frontend.GoArrayCell;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.GoFieldCell;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.p3reflect.AbiTypes;
import graalgo.p3reflect.MapHeaderView;
import graalgo.p3reflect.ReflectTypes;
import graalgo.p3reflect.ReflectValues;
import graalgo.runtime.Intrinsics;
import graalgo.values.GoByteCell;
import graalgo.values.GoCell;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
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
 * <p>fix-redmaster-r59 (maps.Map header view): when the target struct's
 * field layout fits the {@code internal/runtime/maps.Map} header shape
 * (see {@link #captureMapHeaderTarget} — nine fields at fixed offsets
 * with header classes, type-directed only) and the resolved referent
 * is a live {@link GoMap}, the node returns a pointer to the header
 * struct materialized by {@link MapHeaderView} instead of a pointer to
 * the map itself. Without this, {@code (*maps.Map)(mptr)} in real
 * {@code reflect/map.go} dereferenced the {@code GoMap} and died
 * casting it to {@code GoStruct}. Targets whose layout does not fit,
 * and non-map referents, keep the pre-view behavior exactly.
 *
 * <p>fix-redmaster-r59 (sub-word reads): when a byte offset lands
 * strictly inside a Long-carried scalar field and the target is one
 * byte, the node mints a {@link GoByteCell} over the field cell, so
 * {@code *(*ctrl)(unsafe.Add(unsafe.Pointer(g), k))} reads byte
 * {@code k} of the ctrlGroup word (the maps iteration fast path).
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

    /**
     * Qualified name of the maps.Map header-view target struct, or null
     * when the target does not have the header layout (fix-redmaster-r59).
     */
    private final String mapHeaderQName;

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
            PrefixTarget ptrToPtrTarget, String mapHeaderQName) {
        super(GoKind.POINTER);
        this.child = child;
        this.targetQName = targetQName;
        this.viewLayout = viewLayout;
        this.headerQName = headerQName;
        this.mapHeaderQName = mapHeaderQName;
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
                Object fieldPtr = continuedFieldPointer(resolved, view.byteOffset());
                if (fieldPtr != null) {
                    return fieldPtr;
                }
                // fix-redmaster-r59: a byte offset into a Long-carried
                // scalar field cell (e.g. byte k of a ctrlGroup word)
                // with a one-byte target denotes that byte: mint a
                // GoByteCell over the field cell. Wider targets and
                // non-Long cells keep the existing loud derivation
                // failure below.
                Object bytePtr = byteCellPointer(resolved, view.byteOffset());
                if (bytePtr != null) {
                    return bytePtr;
                }
                // fix-redmaster-r59 (b): the same byte-of-a-word read
                // with a GoStruct root (e.g. byte k of a group image's
                // ctrl word) — byteCellPointer only handles GoCell
                // roots. Mint the byte cell over the containing field.
                Object structBytePtr = structByteCellPointer(resolved, view.byteOffset());
                if (structBytePtr != null) {
                    return structBytePtr;
                }
                return derivedPointer(resolved, view.byteOffset());
            }
            if (hit.fieldTok() != hit.targetTok()
                    && headerQName == null && prefixTarget == null
                    && viewLayout == null && ifaceLayout == null) {
                Object fieldPtr = continuedFieldPointer(resolved, view.byteOffset());
                if (fieldPtr != null) {
                    return fieldPtr;
                }
                // fix-redmaster-r59 (c): a same-size reinterpretation
                // of a field cell IS the field's storage, not a
                // reinterpretation of memory — reflect.Value.copyVal's
                // direct branch reads a map slot as unsafe.Pointer
                // (both 8-byte words), so the cell pointer is exact.
                // Differently-sized reinterpretations keep the loud
                // derivation failure below.
                ReflectTypes.TypeInfo fieldInfo = ReflectTypes.table().info(hit.fieldTok());
                if (fieldInfo != null && fieldInfo.size > 0 && elemSize > 0
                        && fieldInfo.size == elemSize) {
                    return UnsafeTable.pointerFor(hit.cell());
                }
                return derivedPointer(resolved, view.byteOffset());
            }
            strictField = hit.fieldTok() != hit.targetTok();
            resolved = hit.cell();
        } else {
            // fix-redmaster-r64 (C2): an offset-0 conversion whose
            // referent is a Java array holding a struct at element 0
            // (the maps header view's groups data: Object[]{group})
            // addresses the element, not the array — in gc flat memory
            // an array and its element 0 share an address, and a
            // pointer to the array IS a pointer to its first element.
            // Without this, (*ctrlGroup)(g.data) returned a pointer to
            // the Object[] itself, and every ctrl read died casting
            // the group struct (or the array) to a scalar. The descent
            // fires only for a struct element and a known non-array
            // target; anything else keeps the pre-existing behavior.
            resolved = arrayElementZero(resolved);
            // Offset 0: a conversion whose target is a direct field's
            // type (not the referent struct's own type) addresses that
            // field's storage — gc's prefix identity for field 0.
            FieldHit hit = structFieldAt(resolved, 0);
            if (hit != null && hit.fieldTok() == hit.targetTok()) {
                resolved = hit.cell();
            } else if (hit != null) {
                // Field 0 is an array and the target is a pointer to
                // its element type: the pointer addresses element 0
                // (same rule as the nonzero-offset branch below).
                Object fieldPtr = continuedFieldPointer(resolved, 0);
                if (fieldPtr != null) {
                    return fieldPtr;
                }
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
        if (mapHeaderQName != null && storedValue(resolved) instanceof GoMap gm) {
            // fix-redmaster-r59: the target struct has the maps.Map
            // header layout (captured at build time) and the referent is
            // a live GoMap — return a pointer to the materialized header
            // so the guest's own header-field reads land on a real
            // struct. Other referents keep the pre-view behavior exactly
            // (a genuine header struct converts by identity below).
            return new GoPointer(MapHeaderView.build(gm));
        }
        if (strictField) {
            throw new UnsafeHandles.UnsafeEscapeException(
                    "unsafe: byte-offset derivation from a non-array referent traps "
                            + "(D5: the handle model cannot reinterpret memory)");
        }
        // fix-redmaster-r64 (C2, directory): an offset-0 conversion
        // whose referent is a Java array addresses element 0, not the
        // array as a whole — in gc flat memory an array and its element
        // 0 share an address (the same rule derivedPointer applies for
        // nonzero offsets). Without this, directoryAt(dirIdx == 0)
        // returned a pointer to the directory array itself, and the
        // first newTab.index read died casting it to a struct. The
        // struct-element case is already normalized above (a pointer
        // to the element's field-0 cell); this covers the remaining
        // shapes (e.g. an array of pointers). Array targets keep the
        // pre-existing behavior (the pointer addresses the array).
        if (resolved != null && resolved.getClass().isArray()
                && java.lang.reflect.Array.getLength(resolved) > 0
                && !isArrayTarget()) {
            return UnsafeTable.pointerFor(
                    new UnsafeTable.ArrayRef(resolved, 0));
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
     * Whether the conversion target's element type is itself an array
     * type (fix-redmaster-r64, C2): a (*[N]T) conversion addresses the
     * array as a whole, so the element-0 rules do not apply. Unresolvable
     * targets report true (fail closed: keep the pre-existing behavior).
     */
    private boolean isArrayTarget() {
        if (targetElemWireId < 0) {
            return true;
        }
        try {
            long targetTok = ReflectTypes.tokForWireId(targetElemWireId);
            if (targetTok < 0) {
                return true;
            }
            ReflectTypes.TypeInfo targetInfo = ReflectTypes.table().info(targetTok);
            return targetInfo == null || targetInfo.kind == null
                    || targetInfo.kind == GoKind.ARRAY;
        } catch (RuntimeException e) {
            return true;
        }
    }

    /**
     * The element-0 referent for an offset-0 conversion over a Java
     * array (fix-redmaster-r64, C2): when the resolved referent is a
     * non-empty Java array whose element 0 is a struct, the conversion
     * addresses that element (gc flat memory: an array and its element
     * 0 share an address). Returns the original referent when the
     * shape does not match (non-array root, empty array, non-struct
     * element), when the conversion target is itself an array type (a
     * (*[N]T) conversion addresses the array as a whole), or when the
     * target type does not resolve — the caller keeps its pre-existing
     * behavior in all of those cases.
     */
    private Object arrayElementZero(Object referent) {
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
        if (root == null || !root.getClass().isArray()) {
            return referent;
        }
        if (java.lang.reflect.Array.getLength(root) == 0) {
            return referent;
        }
        Object first = java.lang.reflect.Array.get(root, 0);
        if (!(first instanceof GoStruct)) {
            return referent;
        }
        if (targetElemWireId < 0) {
            return referent;
        }
        try {
            long targetTok = ReflectTypes.tokForWireId(targetElemWireId);
            if (targetTok < 0) {
                return referent;
            }
            ReflectTypes.TypeInfo targetInfo = ReflectTypes.table().info(targetTok);
            if (targetInfo == null || targetInfo.kind == null
                    || targetInfo.kind == GoKind.ARRAY) {
                return referent;
            }
        } catch (RuntimeException e) {
            return referent;
        }
        return first;
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
        // Unwrap storage cells to the stored value (fix-redmaster-r43,
        // mirrors structFieldAt's unwrap): addressable array storage
        // arrives cell-wrapped (ValueOf(&a).Elem() then Index), and the
        // array check below must see the array, not the cell.
        Object unwrapped = root;
        while (true) {
            if (unwrapped instanceof GoCell cell) {
                unwrapped = cell.get();
            } else if (unwrapped instanceof graalgo.p3reflect.ReflectRefs.Cell rc) {
                unwrapped = rc.value;
            } else {
                break;
            }
        }
        Object array;
        long baseIndex;
        if (unwrapped instanceof UnsafeTable.ArrayRef ref) {
            array = ref.array();
            baseIndex = ref.offset();
        } else if (unwrapped != null && unwrapped.getClass().isArray()) {
            array = unwrapped;
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
        // Nested-array walk (fix-redmaster-r43): the pre-r43 flat
        // division (byteOffset / elemSize against the ROOT array) is
        // only valid when the root's elements ARE the target type. For
        // a nested array it miscomputes: byte 16 into a [2][2]int
        // (root elements 16 bytes) divided by the int size (8) yields
        // index 2 into a length-2 root — a bogus one-past-end element
        // pointer that dies dereferencing, instead of outer[1][0].
        // When the root's element size is derivable from its shape
        // (primitive arrays and rectangular arrays thereof), walk down
        // the levels: at each level divide by THAT level's element
        // size, descending on the remainder while the level's elements
        // are larger than the target; a level whose elements are the
        // target type indexes flat (misalignment traps, as before).
        // Shape-opaque roots (struct elements, empty arrays) keep the
        // exact pre-r43 flat arithmetic, so their behavior is unchanged.
        long rootElemSize = goArrayElemSize(array);
        if (rootElemSize > 0) {
            long offset = baseIndex * rootElemSize + byteOffset;
            Object level = array;
            while (true) {
                long es = goArrayElemSize(level);
                if (es <= 0) {
                    throw new UnsafeHandles.UnsafeEscapeException(
                            "unsafe: cannot resolve a nested-array byte offset "
                                    + "through a shape-opaque level (D5)");
                }
                if (offset < 0) {
                    throw new UnsafeHandles.UnsafeEscapeException(
                            "unsafe: byte-derived element index out of range (D5)");
                }
                int len = java.lang.reflect.Array.getLength(level);
                if (es == elemSize) {
                    if (offset % es != 0) {
                        throw new UnsafeHandles.UnsafeEscapeException(
                                "unsafe: byte offset " + offset + " is not a multiple of the target "
                                        + "element size " + es + " (D5: misaligned derivation traps)");
                    }
                    long index = offset / es;
                    if (index < 0 || index > len) {
                        throw new UnsafeHandles.UnsafeEscapeException(
                                "unsafe: byte-derived element index " + index + " out of range [0, "
                                        + len + "] (D5: derivation cannot leave its referent)");
                    }
                    return UnsafeTable.pointerFor(new UnsafeTable.ArrayRef(level, (int) index));
                }
                if (es < elemSize) {
                    throw new UnsafeHandles.UnsafeEscapeException(
                            "unsafe: byte-derived pointer conversion cannot descend into "
                                    + "elements smaller than the target (D5)");
                }
                long index = offset / es;
                long rem = offset % es;
                if (index < 0 || index > len) {
                    throw new UnsafeHandles.UnsafeEscapeException(
                            "unsafe: byte-derived element index " + index + " out of range [0, "
                                    + len + "] (D5: derivation cannot leave its referent)");
                }
                if (rem == 0) {
                    // Element boundary: the element itself is the
                    // addressed storage (a scalar deref of it peels
                    // through DerefNode; an aggregate deref returns it).
                    return UnsafeTable.pointerFor(new UnsafeTable.ArrayRef(level, (int) index));
                }
                if (index >= len) {
                    throw new UnsafeHandles.UnsafeEscapeException(
                            "unsafe: byte-derived element index " + index + " out of range [0, "
                                    + len + "] (D5: derivation cannot leave its referent)");
                }
                Object next = java.lang.reflect.Array.get(level, (int) index);
                if (next instanceof GoStruct elemStruct) {
                    // fix-redmaster-r59 (gap (c)): a byte offset into
                    // an array-of-struct element (e.g. a maps group
                    // inside the groups data array) denotes a field or
                    // byte of that struct — resolve the remainder
                    // through the struct-field machinery, the same
                    // resolution executeObject applies to a struct root.
                    Object elemPtr = structOffsetPointer(elemStruct, rem);
                    if (elemPtr != null) {
                        return elemPtr;
                    }
                    throw new UnsafeHandles.UnsafeEscapeException(
                            "unsafe: byte-derived pointer conversion cannot resolve a byte offset "
                                    + "inside a struct array element (D5)");
                }
                if (next == null || !next.getClass().isArray()) {
                    throw new UnsafeHandles.UnsafeEscapeException(
                            "unsafe: byte-derived pointer conversion cannot descend into "
                                    + "a non-array element (D5)");
                }
                level = next;
                offset = rem;
            }
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

    /**
     * Resolves a byte offset into a struct value to a pointer
     * (fix-redmaster-r59 gap (c)): the same resolution executeObject
     * applies to a struct root — exact field hit (or its same-size
     * reinterpretation), array-element continuation, or a byte cell —
     * for use when derivedPointer's walk reaches a struct array element
     * with a nonzero remainder. View-owned mismatches return null (the
     * views fire only for the top-level referent in executeObject); the
     * caller keeps its loud failure. Returns null when nothing resolves.
     */
    private Object structOffsetPointer(GoStruct gs, long byteOffset) {
        FieldHit hit = structFieldAt(gs, byteOffset);
        if (hit == null) {
            Object fieldPtr = continuedFieldPointer(gs, byteOffset);
            if (fieldPtr != null) {
                return fieldPtr;
            }
            Object bytePtr = byteCellPointer(gs, byteOffset);
            if (bytePtr != null) {
                return bytePtr;
            }
            Object structBytePtr = structByteCellPointer(gs, byteOffset);
            if (structBytePtr != null) {
                return structBytePtr;
            }
            return null;
        }
        if (hit.fieldTok() == hit.targetTok()) {
            return UnsafeTable.pointerFor(hit.cell());
        }
        if (headerQName == null && prefixTarget == null
                && viewLayout == null && ifaceLayout == null) {
            Object fieldPtr = continuedFieldPointer(gs, byteOffset);
            if (fieldPtr != null) {
                return fieldPtr;
            }
            ReflectTypes.TypeInfo fieldInfo = ReflectTypes.table().info(hit.fieldTok());
            if (fieldInfo != null && fieldInfo.size > 0 && elemSize > 0
                    && fieldInfo.size == elemSize) {
                return UnsafeTable.pointerFor(hit.cell());
            }
        }
        return null;
    }

    /**
     * The Go byte size of one element of a Java array, derived from the
     * array's shape alone (fix-redmaster-r43): primitive component
     * types by width; nested arrays by rectangularity (every element
     * has the first element's length — true of every Go array value).
     * Returns -1 when the size is not shape-derivable (struct elements,
     * empty arrays, null first element): callers then keep the
     * pre-r43 flat arithmetic instead of guessing.
     */
    private static long goArrayElemSize(Object array) {
        Class<?> comp = array.getClass().getComponentType();
        if (comp == null) {
            return -1;
        }
        if (comp == long.class || comp == double.class) {
            return 8;
        }
        if (comp == int.class || comp == float.class) {
            return 4;
        }
        if (comp == short.class || comp == char.class) {
            return 2;
        }
        if (comp == byte.class || comp == boolean.class) {
            return 1;
        }
        if (comp.isArray()) {
            int len = java.lang.reflect.Array.getLength(array);
            if (len == 0) {
                return -1;
            }
            Object first = java.lang.reflect.Array.get(array, 0);
            if (first == null || !first.getClass().isArray()) {
                return -1;
            }
            long inner = goArrayElemSize(first);
            if (inner <= 0) {
                return -1;
            }
            return inner * java.lang.reflect.Array.getLength(first);
        }
        // Object[] (the shape nested Go arrays take: the component type
        // is Object even when every element is itself an array): nest
        // through the first element, as above.
        if (comp == Object.class) {
            int len = java.lang.reflect.Array.getLength(array);
            if (len == 0) {
                return -1;
            }
            Object first = java.lang.reflect.Array.get(array, 0);
            if (first instanceof GoStruct firstStruct) {
                // fix-redmaster-r59 (gap (c)): an array of structs
                // (the maps groups data: one group per table) — the
                // element size is the struct's Go size.
                return goStructElemSize(firstStruct);
            }
            if (first == null || !first.getClass().isArray()) {
                return -1;
            }
            long inner = goArrayElemSize(first);
            if (inner <= 0) {
                return -1;
            }
            return inner * java.lang.reflect.Array.getLength(first);
        }
        return -1;
    }

    /**
     * The Go byte size of a struct value (fix-redmaster-r59 gap (c)):
     * the TypeInfo size when the descriptor resolves to one (exact —
     * structOf lays synthesized types out from the wire sizes, and named
     * structs carry their ABI size), else the descriptor walk. Returns
     * -1 when neither yields a size (the caller keeps the pre-r43 flat
     * arithmetic).
     */
    private static long goStructElemSize(GoStruct gs) {
        try {
            ReflectTypes.TypeInfo info = ReflectTypes.infoForName(gs.descriptor().typeName());
            if (info == null) {
                info = ReflectTypes.infoForStructString(gs.descriptor().typeName());
            }
            if (info != null && info.size > 0) {
                return info.size;
            }
        } catch (RuntimeException e) {
            // Fall through to the descriptor walk.
        }
        try {
            return Intrinsics.structSizeBytes(gs.descriptor());
        } catch (RuntimeException e) {
            return -1;
        }
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
            if (rootInfo == null) {
                // Unnamed structs (including guest-constructed ones):
                // no name to index by, but the descriptor name IS the
                // structural rendering — recover via typeString match
                // (fix-redmaster-r43; see infoForStructString).
                rootInfo = ReflectTypes.infoForStructString(gs.descriptor().typeName());
            }
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
            if (f.type == null || f.index.length != 1) {
                continue;
            }
            if (f.offset == byteOffset) {
                int ord = f.index[0];
                if (ord < 0 || ord >= gs.descriptor().fields().size()
                        || !gs.descriptor().field(ord).name().equals(f.name)) {
                    return null;
                }
                // fix-redmaster-r59 (d): an exact field-boundary match
                // whose kind differs from the conversion target is NOT
                // the addressed storage when the field is a container:
                // offset 8 into a group image is the slots array, but a
                // STRING read there addresses slot 0's key, not the
                // array — descend into element 0 (array-of-struct) or
                // field 0 (struct), falling back to the field hit when
                // the descent does not resolve.
                ReflectTypes.TypeInfo targetInfo = ReflectTypes.table().info(targetTok);
                if (targetInfo != null && targetInfo.kind != null
                        && f.type.kind != null && f.type.kind != targetInfo.kind) {
                    FieldHit descended = descendExactMatch(gs, ord, f);
                    if (descended != null) {
                        return descended;
                    }
                }
                return new FieldHit(
                        new GoFieldCell(gs, ord, gs.descriptor().field(ord).kind()),
                        f.type.tok, targetTok);
            }
            // fix-redmaster-r51: an offset strictly inside a
            // struct-typed direct field descends into that field with
            // the remainder (gc flat memory; mirrors
            // ReflectRefs.readStructFieldAt). Without this, reflect's
            // Field chain on a nested struct (add(structPtr,
            // outerOffset + innerOffset), then a typed deref — e.g.
            // fmt printing a nested struct's string field through
            // Value.String()'s *(*string)(v.ptr)) finds no direct
            // field at the composite offset and falls through to the
            // array-derivation trap. Array-typed fields stay with
            // continuedFieldPointer (the array-element path below).
            if (f.type.kind == GoKind.STRUCT && f.type.size > 0
                    && byteOffset > f.offset && byteOffset < f.offset + f.type.size) {
                int ord = f.index[0];
                if (ord < 0 || ord >= gs.descriptor().fields().size()
                        || !gs.descriptor().field(ord).name().equals(f.name)) {
                    return null;
                }
                Object nested = ReflectValues.structFieldGet(gs, ord, f.type.kind);
                if (!(nested instanceof GoStruct nestedStruct)) {
                    return null;
                }
                return structFieldAt(nestedStruct, byteOffset - f.offset);
            }
            // fix-redmaster-r59: an offset strictly inside an
            // array-of-struct field descends into the addressed element
            // (element index = rel / elemSize, remainder recursed) —
            // gc flat memory again, for the group image's slots array:
            // group.key(i) / group.elem(i) form
            // add(groupPtr, slotsOff + i*slotSize + fieldOff).
            if (f.type.kind == GoKind.ARRAY && f.type.elem != null
                    && f.type.elem.kind == GoKind.STRUCT && f.type.elem.size > 0
                    && byteOffset > f.offset
                    && byteOffset < f.offset + f.type.size) {
                int ord = f.index[0];
                if (ord < 0 || ord >= gs.descriptor().fields().size()
                        || !gs.descriptor().field(ord).name().equals(f.name)) {
                    return null;
                }
                Object arr = ReflectValues.structFieldGet(gs, ord, GoKind.ARRAY);
                if (arr == null) {
                    return null;
                }
                long rel = byteOffset - f.offset;
                long elemSize = f.type.elem.size;
                long idx = rel / elemSize;
                long rem = rel % elemSize;
                if (idx < 0 || idx >= f.type.arrayLen) {
                    return null;
                }
                Object elem = java.lang.reflect.Array.get(arr, (int) idx);
                if (!(elem instanceof GoStruct elemStruct)) {
                    return null;
                }
                return structFieldAt(elemStruct, rem);
            }
        }
        return null;
    }

    /**
     * Descends an exact field-boundary match into the container's first
     * element (fix-redmaster-r59 (d)): an ARRAY-of-STRUCT field's element
     * 0, or a STRUCT field's field 0. Returns null when the field is not
     * such a container or the descent does not resolve (the caller keeps
     * the field hit).
     */
    private FieldHit descendExactMatch(GoStruct gs, int ord, ReflectTypes.FieldInfo f) {
        if (f.type.kind == GoKind.ARRAY && f.type.elem != null
                && f.type.elem.kind == GoKind.STRUCT) {
            Object arr = ReflectValues.structFieldGet(gs, ord, GoKind.ARRAY);
            if (arr == null) {
                return null;
            }
            Object elem = java.lang.reflect.Array.get(arr, 0);
            if (!(elem instanceof GoStruct elemStruct)) {
                return null;
            }
            return structFieldAt(elemStruct, 0);
        }
        if (f.type.kind == GoKind.STRUCT) {
            Object nested = ReflectValues.structFieldGet(gs, ord, GoKind.STRUCT);
            if (!(nested instanceof GoStruct nestedStruct)) {
                return null;
            }
            return structFieldAt(nestedStruct, 0);
        }
        return null;
    }

    /**
     * Pointer into an array field (fix-redmaster-r43). When the byte
     * offset addresses a struct field whose type is an ARRAY and the
     * conversion target is a pointer to that array's element type,
     * the pointer denotes an array element, not the field as a whole:
     * in gc an array and its first element share an address, and
     * reflect's own code relies on it
     * ({@code unsafe.Slice((**rtype)(unsafe.Pointer(o.Field(1).Addr().Pointer())), n)}
     * in FuncOf, where field 1 is the {@code [n]*rtype} args array).
     * Returns an element pointer ({@code GoPointer(array, index)},
     * the same form {@code &arr[i]} produces, so downstream
     * consumers — notably unsafe.Slice — see array storage) or null
     * when the offset does not address such an element, in which case
     * the caller keeps its existing loud derivation failure. The
     * element index comes from the offset within the field divided
     * by the target element size (the wire elemSize, which equals
     * the array's element size because the element token matches);
     * misaligned or out-of-range offsets are not elements and return
     * null rather than building an invalid pointer.
     */
    private Object arrayElemPointer(Object referent, long byteOffset) {
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
        if (!(root instanceof GoStruct gs) || targetElemWireId < 0 || elemSize <= 0) {
            return null;
        }
        ReflectTypes.TypeInfo rootInfo;
        long targetTok;
        try {
            rootInfo = ReflectTypes.infoForName(gs.descriptor().typeName());
            if (rootInfo == null) {
                rootInfo = ReflectTypes.infoForStructString(gs.descriptor().typeName());
            }
            targetTok = ReflectTypes.tokForWireId(targetElemWireId);
        } catch (RuntimeException e) {
            return null;
        }
        if (rootInfo == null || targetTok < 0
                || rootInfo.fields == null || rootInfo.kind != GoKind.STRUCT) {
            return null;
        }
        for (ReflectTypes.FieldInfo f : rootInfo.fields) {
            if (f.type == null || f.index.length != 1
                    || f.type.kind != GoKind.ARRAY || f.type.elem == null
                    || f.type.elem.tok != targetTok) {
                continue;
            }
            if (byteOffset < f.offset) {
                continue;
            }
            long rel = byteOffset - f.offset;
            if (rel % elemSize != 0) {
                continue;
            }
            long idx = rel / elemSize;
            // One past the end is a valid POINTER (gc pointer
            // arithmetic may form it; only dereference is UB, and
            // that then fails loudly at the access): the empty-array
            // case (FuncOf with no args: Args is [0]*rtype, and
            // unsafe.Slice(ptr, 0) over it must succeed) has no
            // element index at all, yet its storage address is
            // well-defined. Beyond one past the end is not an
            // address in the field and stays rejected.
            if (idx < 0 || idx > f.type.arrayLen) {
                continue;
            }
            int ord = f.index[0];
            if (ord < 0 || ord >= gs.descriptor().fields().size()
                    || !gs.descriptor().field(ord).name().equals(f.name)) {
                return null;
            }
            Object arr = new GoFieldCell(gs, ord, gs.descriptor().field(ord).kind()).get();
            long base = 0;
            if (arr instanceof UnsafeTable.ArrayRef ref) {
                arr = ref.array();
                base = ref.offset();
            }
            while (arr instanceof GoCell cell) {
                arr = cell.get();
            }
            if (arr == null || !arr.getClass().isArray()) {
                return null;
            }
            long total = base + idx;
            if (total < 0 || total > Integer.MAX_VALUE) {
                return null;
            }
            return new GoPointer(arr, (int) total);
        }
        return null;
    }

    /**
     * Pointer to an array field (fix-redmaster-r43). When the byte
     * offset is exactly a struct field whose type is an ARRAY and the
     * conversion target is a pointer to an ARRAY type with a
     * compatible element type, the pointer denotes the field's array
     * itself — not an element of it. This is gc's InSlice/OutSlice
     * shape: {@code (*[1<<16]*Type)(ptr+uadd)[:n]} over a funcType
     * struct, where the target array is deliberately oversized (the
     * real bounds come from the slice expression, which reads the
     * field array's own length) and the element types differ only by
     * prefix identity ({@code *Type} vs {@code *rtype}: an rtype's
     * memory starts with its abi.Type). Returns a plain
     * {@code GoPointer} to the field's array (so a pointer-slice
     * consumer's {@code referent()} sees the array) or null. Element
     * compatibility is token equality, or — for pointer elements
     * only — the field element's pointee struct having the target
     * element's pointee struct as an offset-0 prefix (the direction
     * matters: reading {@code *rtype} slots as {@code *Type} is a
     * prefix view; the reverse would read past the smaller struct).
     * Mid-field offsets are not array starts and return null; the
     * caller keeps its loud derivation failure for those.
     */
    private Object arrayFieldPointer(Object referent, long byteOffset) {
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
            if (rootInfo == null) {
                rootInfo = ReflectTypes.infoForStructString(gs.descriptor().typeName());
            }
            targetTok = ReflectTypes.tokForWireId(targetElemWireId);
        } catch (RuntimeException e) {
            return null;
        }
        if (rootInfo == null || targetTok < 0
                || rootInfo.fields == null || rootInfo.kind != GoKind.STRUCT) {
            return null;
        }
        ReflectTypes.TypeInfo targetInfo = ReflectTypes.table().info(targetTok);
        if (targetInfo == null || targetInfo.kind != GoKind.ARRAY || targetInfo.elem == null) {
            return null;
        }
        for (ReflectTypes.FieldInfo f : rootInfo.fields) {
            if (f.type == null || f.index.length != 1
                    || f.type.kind != GoKind.ARRAY || f.type.elem == null
                    || byteOffset != f.offset) {
                continue;
            }
            ReflectTypes.TypeInfo fieldElem = f.type.elem;
            ReflectTypes.TypeInfo targetElem = targetInfo.elem;
            boolean elemMatch = fieldElem.tok == targetElem.tok;
            if (!elemMatch && fieldElem.kind == GoKind.POINTER && targetElem.kind == GoKind.POINTER
                    && fieldElem.elem != null && targetElem.elem != null) {
                // Pointee-prefix comparison (not pointer-token
                // comparison): the field element's pointee struct
                // must have the target element's pointee struct as
                // an offset-0 prefix.
                elemMatch = hasOffsetZeroPrefix(fieldElem.elem, targetElem.elem.tok, 4);
            }
            if (!elemMatch) {
                continue;
            }
            int ord = f.index[0];
            if (ord < 0 || ord >= gs.descriptor().fields().size()
                    || !gs.descriptor().field(ord).name().equals(f.name)) {
                return null;
            }
            Object arr = new GoFieldCell(gs, ord, gs.descriptor().field(ord).kind()).get();
            if (arr instanceof UnsafeTable.ArrayRef ref) {
                arr = ref.array();
            }
            while (arr instanceof GoCell cell) {
                arr = cell.get();
            }
            if (arr == null || !arr.getClass().isArray()) {
                return null;
            }
            return new GoPointer(arr);
        }
        return null;
    }

    /**
     * Whether struct info {@code s} has the struct named by
     * {@code prefixTok} as an offset-0 prefix: its field at offset 0
     * is (transitively) that struct. This is the layout fact behind
     * gc's prefix identity (an rtype's field 0 is its abi.Type, a
     * structType's field 0 is its abi.StructType, and so on).
     */
    private static boolean hasOffsetZeroPrefix(ReflectTypes.TypeInfo s, long prefixTok, int depth) {
        if (s == null || depth < 0) {
            return false;
        }
        if (s.tok == prefixTok) {
            return true;
        }
        if (s.kind != GoKind.STRUCT || s.fields == null) {
            return false;
        }
        for (ReflectTypes.FieldInfo f : s.fields) {
            if (f.offset == 0 && f.type != null) {
                return hasOffsetZeroPrefix(f.type, prefixTok, depth - 1);
            }
        }
        return false;
    }

    /**
     * Field-pointer resolution across prefix parents (fix-redmaster-r43).
     * A byte-offset derivation may start from a struct that is itself
     * at offset 0 of a larger allocation — the canonical case is
     * InSlice's {@code ptr+Sizeof(funcType)}: the funcType struct is
     * field 0 of the initFunc allocation, so offset 56 past the
     * funcType struct is the Args array field of the initFunc parent,
     * not memory past the funcType struct. Prefix parents are always
     * offset-0 containers (embedding chains and field-0 values alike),
     * so the offset carries unchanged up the chain. Tries the
     * element-pointer and array-pointer rules at each level, nearest
     * first; returns null when no level resolves, leaving the caller's
     * loud derivation failure intact.
     */
    private Object continuedFieldPointer(Object referent, long byteOffset) {
        Object cur = referent;
        while (true) {
            if (cur instanceof GoCell cell) {
                cur = cell.get();
            } else if (cur instanceof graalgo.p3reflect.ReflectRefs.Cell rc) {
                cur = rc.value;
            } else {
                break;
            }
        }
        for (int depth = 0; depth < 8 && cur instanceof GoStruct gs; depth++) {
            Object p = arrayElemPointer(cur, byteOffset);
            if (p != null) {
                return p;
            }
            p = arrayFieldPointer(cur, byteOffset);
            if (p != null) {
                return p;
            }
            GoStruct parent = gs.prefixParent();
            if (parent == null || parent == gs) {
                break;
            }
            cur = parent;
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
        String mapHeaderQName = captureMapHeaderTarget(ctx, typeId);
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
                ptrToPtrTarget,
                mapHeaderQName);
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

    /**
     * Captures the maps.Map header-view target (fix-redmaster-r59), or
     * null when the target is not a header-shaped struct. The target
     * must be a pointer to a named struct with exactly nine fields at
     * offsets 0, 8, 16, 24, 32, 33, 34, 35, 40 with classes uint, uint,
     * data, int, uint, uint, uint, bool, uint — the
     * {@code internal/runtime/maps.Map} header layout (used, seed,
     * dirPtr, dirLen, globalDepth, globalShift, writing,
     * tombstonePossible, clearSeq). Classification is by layout only
     * (the r40 header-view precedent): no package name is consulted,
     * so an identically-shaped user struct converts identically — in
     * gc it would read the same header bytes. The qualified name is
     * returned only so the header view's descriptor resolves via the
     * real type.
     */
    private static String captureMapHeaderTarget(NodeFactory.BuildCtx ctx, int typeId) {
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
        if (fields.size() != 9) {
            return null;
        }
        long[] wantOffsets = {0, 8, 16, 24, 32, 33, 34, 35, 40};
        for (int i = 0; i < 9; i++) {
            Json.Value fv = fields.get(i);
            if (!(fv instanceof Json.Object fo)) {
                return null;
            }
            if (fo.longInt("offset", path) != wantOffsets[i]) {
                return null;
            }
            int ftype = fo.intValue("type", path);
            boolean fits = switch (i) {
                case 0, 1, 4, 5, 6, 8 -> isUintWordField(ctx, ftype, path);
                case 2 -> isDataWordField(ctx, ftype, path);
                case 3 -> isIntWordField(ctx, ftype, path);
                case 7 -> isBoolWordField(ctx, ftype, path);
                default -> false;
            };
            if (!fits) {
                return null;
            }
        }
        return qname;
    }

    /**
     * Whether a wire type is a uint word: a basic uint-class type
     * (named wrappers unwrapped first). Covers uint64/uint8/uintptr
     * (uintptr's class is uint).
     */
    private static boolean isUintWordField(NodeFactory.BuildCtx ctx, int typeId, String path) {
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
        return "uint".equals(e.object("detail", path).string("class", path));
    }

    /**
     * Whether a wire type is a bool word: a basic bool-class type
     * (named wrappers unwrapped first).
     */
    private static boolean isBoolWordField(NodeFactory.BuildCtx ctx, int typeId, String path) {
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
        return "bool".equals(e.object("detail", path).string("class", path));
    }

    /**
     * A byte pointer into a scalar field cell (fix-redmaster-r59, r69
     * C1/F1): when the resolved referent (after unwrapping storage
     * cells) is a {@link GoCell} holding an integral value and the
     * target element is one byte, byte {@code byteOffset} of that
     * storage is the addressed storage — return a {@link GoByteCell}
     * pointer over it. Long-carried storage takes the Long-only cell;
     * sub-word integral storage (an Integer-boxed uint32/int32 local,
     * an int[]/short[] array element) takes the width-aware cell, so
     * {@code *(*byte)(unsafe.Add(unsafe.Pointer(&x), k))} reads the
     * variable's own byte k instead of trapping in derivedPointer.
     * Returns null when the shape does not match (the caller keeps its
     * existing loud derivation failure), the target is wider than one
     * byte (sub-word multi-byte reads are not minted), or the offset
     * lies outside the storage: offsets at/above the width are NOT
     * minted, so cross-element reads stay with derivedPointer (which
     * indexes the backing array correctly) and out-of-variable reads
     * keep trapping instead of reading extension bytes.
     */
    private Object byteCellPointer(Object referent, long byteOffset) {
        if (elemSize != 1 || byteOffset <= 0 || byteOffset >= 8) {
            return null;
        }
        Object root = referent;
        while (true) {
            if (root instanceof GoCell cell) {
                Object v = cell.get();
                // r64 (C1): the Long-only cell over Long-carrying storage.
                if (v instanceof Long) {
                    return new GoPointer(new GoByteCell(cell, (int) byteOffset));
                }
                // r69 (C1/F1): sub-word integral storage takes the
                // width-aware cell, minted only inside the storage.
                int width = byteStorageWidth(cell, v);
                if (width > 0 && byteOffset < width) {
                    return new GoPointer(new GoByteCell(cell, (int) byteOffset, width));
                }
                return null;
            } else if (root instanceof UnsafeTable.ArrayRef ref) {
                // r69 (C1/F1): a byte offset into a primitive array
                // element. &arr[i] handles resolve to ArrayRef (not a
                // GoArrayCell), so the GoCell branch above never sees
                // them; without this, every in-element byte offset fell
                // through to derivedPointer, which cannot descend into
                // a primitive element and traps.
                return arrayElementBytePointer(ref.array(), ref.offset(), byteOffset);
            } else if (root != null && root.getClass().isArray()) {
                return arrayElementBytePointer(root, 0, byteOffset);
            } else if (root instanceof graalgo.p3reflect.ReflectRefs.Cell rc) {
                root = rc.value;
            } else {
                return null;
            }
        }
    }

    /**
     * A byte pointer into one element of a primitive Java array (r69
     * C1/F1): {@code byteOffset} is relative to element {@code baseIndex};
     * decompose it into whole elements plus a within-element byte. A zero
     * remainder is an element boundary — derivedPointer owns it (null).
     * A nonzero remainder inside a primitive integral element mints a
     * {@link GoByteCell} over that element's {@link GoArrayCell}, so
     * in-element bytes read exactly and writes alias the element.
     * Anything else returns null so derivedPointer keeps its existing
     * behavior: non-integral elements (float/bool/object/nested arrays),
     * and out-of-range elements (which derivedPointer reports loudly).
     * Signedness of the element kind is immaterial here: byte reads are
     * masked to the element width and writes narrow to the low bits, so
     * the signed kinds are used throughout.
     */
    private static Object arrayElementBytePointer(Object array, int baseIndex, long byteOffset) {
        long es = goArrayElemSize(array);
        if (es <= 0 || es > 8) {
            return null;
        }
        long skip = byteOffset / es;
        long within = byteOffset % es;
        if (within == 0) {
            return null;
        }
        GoKind ek = integralArrayElemKind(array);
        if (ek == null) {
            return null;
        }
        long index = (long) baseIndex + skip;
        if (index < 0 || index >= java.lang.reflect.Array.getLength(array)) {
            return null;
        }
        return new GoPointer(new GoByteCell(
                new GoArrayCell(array, ek, (int) index), (int) within, (int) es));
    }

    /** The signed integral {@link GoKind} for a primitive array's component type, or null. */
    private static GoKind integralArrayElemKind(Object array) {
        Class<?> comp = array.getClass().getComponentType();
        if (comp == byte.class) {
            return GoKind.INT8;
        }
        if (comp == short.class) {
            return GoKind.INT16;
        }
        if (comp == int.class) {
            return GoKind.INT32;
        }
        if (comp == long.class) {
            return GoKind.INT64;
        }
        return null;
    }

    /**
     * Storage width in bytes of the integral value a byte-cell mint
     * would address (r69 C1/F1), or -1 when no byte cell may be minted
     * over this cell: the value is not an integral {@link Number}
     * (floating-point, boolean, String, ...), or the width is not
     * derivable. Array elements use the backing array's element size —
     * the element's true storage width (byte[] 1, short[] 2, int[] 4,
     * long[] 8), which is narrower than the Integer box narrow elements
     * arrive in; other cells use the boxed value's width
     * ({@link GoByteCell#boxedWidth}). The mint site additionally
     * requires the offset to lie inside the width, so a byte cell
     * never addresses past its storage.
     */
    private static int byteStorageWidth(GoCell cell, Object v) {
        if (GoByteCell.boxedWidth(v) <= 0) {
            return -1;
        }
        if (cell instanceof GoArrayCell a) {
            long es = goArrayElemSize(a.array());
            return es > 0 && es <= 8 ? (int) es : -1;
        }
        return GoByteCell.boxedWidth(v);
    }

    /**
     * A byte pointer into a Long-carried scalar field of a struct
     * referent (fix-redmaster-r59 (b)): when the resolved referent is a
     * {@link GoStruct} and the target element is one byte, byte
     * {@code byteOffset} of the containing Long-carried field is the
     * addressed storage — return a {@link GoByteCell} pointer over a
     * {@link GoFieldCell} for it. The containing field is found directly
     * or by descending into a STRUCT field or an array-of-struct element
     * (gc flat memory: a group image's ctrl word is read as
     * {@code *(*ctrl)(unsafe.Add(unsafe.Pointer(g), k))} with a group
     * struct root). Returns null when the shape does not match (the
     * caller keeps its existing loud derivation failure) or the target
     * is wider than one byte.
     */
    private Object structByteCellPointer(Object referent, long byteOffset) {
        if (elemSize != 1) {
            return null;
        }
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
        if (!(root instanceof GoStruct gs)) {
            return null;
        }
        return structByteCellIn(gs, byteOffset);
    }

    /** Recursive worker for {@link #structByteCellPointer}: the byte cell for a byte offset into one struct. */
    private Object structByteCellIn(GoStruct gs, long byteOffset) {
        ReflectTypes.TypeInfo info;
        try {
            info = ReflectTypes.infoForName(gs.descriptor().typeName());
            if (info == null) {
                info = ReflectTypes.infoForStructString(gs.descriptor().typeName());
            }
        } catch (RuntimeException e) {
            return null;
        }
        if (info == null || info.fields == null) {
            return null;
        }
        for (ReflectTypes.FieldInfo f : info.fields) {
            if (f.type == null || f.index.length != 1 || f.type.size <= 0) {
                continue;
            }
            if (byteOffset < f.offset || byteOffset >= f.offset + f.type.size) {
                continue;
            }
            int ord = f.index[0];
            if (ord < 0 || ord >= gs.descriptor().fields().size()
                    || !gs.descriptor().field(ord).name().equals(f.name)) {
                return null;
            }
            long rel = byteOffset - f.offset;
            if (f.type.kind == GoKind.STRUCT) {
                Object nested = ReflectValues.structFieldGet(gs, ord, GoKind.STRUCT);
                if (!(nested instanceof GoStruct nestedStruct)) {
                    return null;
                }
                return structByteCellIn(nestedStruct, rel);
            }
            if (f.type.kind == GoKind.ARRAY && f.type.elem != null
                    && f.type.elem.kind == GoKind.STRUCT && f.type.elem.size > 0) {
                Object arr = ReflectValues.structFieldGet(gs, ord, GoKind.ARRAY);
                if (arr == null) {
                    return null;
                }
                long idx = rel / f.type.elem.size;
                long rem = rel % f.type.elem.size;
                if (idx < 0 || idx >= f.type.arrayLen) {
                    return null;
                }
                Object elem = java.lang.reflect.Array.get(arr, (int) idx);
                if (!(elem instanceof GoStruct elemStruct)) {
                    return null;
                }
                return structByteCellIn(elemStruct, rem);
            }
            // A Long-carried scalar field: byte rel of its word.
            if (gs.descriptor().field(ord).inLongs() && rel >= 0 && rel < 8) {
                return new GoPointer(new GoByteCell(
                        new GoFieldCell(gs, ord, gs.descriptor().field(ord).kind()), (int) rel));
            }
            return null;
        }
        return null;
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
