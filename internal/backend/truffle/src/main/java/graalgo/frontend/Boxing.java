package graalgo.frontend;

import com.oracle.truffle.api.frame.FrameSlotKind;
import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Canonical boxed values and value-copy rules shared by the M4 nodes
 * (WS E).
 *
 * <p>Boxed form per carrier: {@code int}→{@link Integer},
 * {@code long}→{@link Long}, {@code float}→{@link Float},
 * {@code double}→{@link Double}, {@code boolean}→{@link Boolean},
 * {@code Object}→the reference itself. Every producer (literals, map
 * keys, interface data, slot writes) and every consumer uses this form,
 * so {@link graalgo.values.GoEquality} sees consistent types (e.g. all
 * int/uint map keys box as {@link Long}, never {@link Integer}).
 */
public final class Boxing {
    private Boxing() {}

    /** Reads {@code node} and returns its canonical boxed value. */
    static Object box(GoKind kind, GoExprNode node, VirtualFrame frame) {
        // Use the node's own kind to determine how to read it, not the target
        // kind. The node may be an untyped constant (e.g., UNTYPED_INT with
        // Long slot kind) being boxed to a different target kind (e.g.,
        // UINT16 with Int slot kind). Reading via the node's kind is correct;
        // the boxed value is then narrowed/widened by the caller if needed.
        GoKind readKind = node.kind() != null ? node.kind() : kind;
        return switch (readKind.slotKind()) {
            case Int -> Integer.valueOf(node.executeInt(frame));
            case Long -> Long.valueOf(node.executeLong(frame));
            case Float -> Float.valueOf(node.executeFloat(frame));
            case Double -> Double.valueOf(node.executeDouble(frame));
            case Boolean -> Boolean.valueOf(node.executeBoolean(frame));
            case Object -> node.executeObject(frame);
            default -> throw new FrontendException("cannot box kind " + readKind + " (WS E)");
        };
    }

    /** Writes a canonical boxed value to a frame slot of the given kind. */
    static void setSlot(VirtualFrame frame, int slot, GoKind kind, Object boxed) {
        switch (kind.slotKind()) {
            case Int -> frame.setInt(slot, ((Integer) boxed).intValue());
            case Long -> frame.setLong(slot, ((Long) boxed).longValue());
            case Float -> frame.setFloat(slot, ((Float) boxed).floatValue());
            case Double -> frame.setDouble(slot, ((Double) boxed).doubleValue());
            case Boolean -> frame.setBoolean(slot, ((Boolean) boxed).booleanValue());
            case Object -> frame.setObject(slot, boxed);
            default -> throw new FrontendException("cannot write kind " + kind + " (WS E)");
        }
    }

    /** Reads a frame slot as a canonical boxed value (M5, WS F). */
    static Object readSlot(VirtualFrame frame, int slot, GoKind kind) {
        return switch (kind.slotKind()) {
            case Int -> Integer.valueOf(frame.getInt(slot));
            case Long -> Long.valueOf(frame.getLong(slot));
            case Float -> Float.valueOf(frame.getFloat(slot));
            case Double -> Double.valueOf(frame.getDouble(slot));
            case Boolean -> Boolean.valueOf(frame.getBoolean(slot));
            case Object -> frame.getObject(slot);
            default -> throw new FrontendException("cannot read kind " + kind + " (WS E)");
        };
    }

    /**
     * Writes an already-boxed value into a {@link graalgo.values.GoBox}
     * cell (per-iteration capture): stores it in the cell. The cell is
     * fully detached — there is no write-through to any other cell;
     * propagation between the per-iteration cell and the carry cell of a
     * {@code for}-header variable happens only through the explicit
     * {@link BoxRefreshNode} (seed) and {@link BoxWritebackNode}
     * (write-back), matching gc's {@code loopvar} desugar.
     */
    static void setBoxedCell(VirtualFrame frame, int slot, GoKind kind, Object boxed) {
        graalgo.values.GoBox cell = (graalgo.values.GoBox) frame.getObject(slot);
        if (cell == null) {
            throw new IllegalStateException("null GoBox cell in slot " + slot
                    + " (per-iteration cell not installed)");
        }
        cell.value = boxed;
    }

    /**
     * Go's value-copy rule for assignments, container stores, and
     * interface box/unbox (M4): struct values deep-copy, array values
     * deep-copy (struct elements are copied, not aliased — a shallow
     * array clone would let {@code b := a; b[i].f = x} observably mutate
     * {@code a}), everything else is already a reference or immutable.
     */
    static Object copyForAssign(GoKind kind, Object value) {
        if (value instanceof GoStruct gs && kind == GoKind.STRUCT) {
            return gs.deepCopy();
        }
        // P3-M4 (WS C, ADR-0004 D2; remediated P3-M4 P0-1): atomic cells
        // are values in Go (the struct copy copies the integer; copying
        // is a vet error but defined), so a by-value copy clones the
        // cell — `b := a; b.Store(7)` leaves a alone, like gc. The clone
        // applies ONLY to struct-kind copies: the D5 carrier of a
        // `*atomic.Int64` (POINTER kind) IS the same cell object, and
        // cloning it would sever pointer aliasing (`p:=new(atomic.Int64);
        // q:=p; q.Store(5)` must be visible through p). Mirrors the
        // GoStruct branch directly above.
        if (kind == GoKind.STRUCT) {
            if (value instanceof java.util.concurrent.atomic.AtomicLong al) {
                return new java.util.concurrent.atomic.AtomicLong(al.get());
            }
            if (value instanceof java.util.concurrent.atomic.AtomicInteger ai) {
                return new java.util.concurrent.atomic.AtomicInteger(ai.get());
            }
        }
        if (value instanceof GoInterface gi && kind == GoKind.INTERFACE) {
            // Boxing a struct/array into an interface copies the value.
            Object d = gi.data();
            Object dc = d instanceof GoStruct gs ? gs.deepCopy()
                    : (d != null && d.getClass().isArray() ? GoStruct.deepCopyArray(d) : d);
            return new GoInterface(gi.descriptor(), dc, gi.itab());
        }
        if (value != null && kind == GoKind.ARRAY && value.getClass().isArray()) {
            return GoStruct.deepCopyArray(value);
        }
        return value;
    }

    /** Go's zero value in canonical boxed form (nil → null, except strings/structs). */
    static Object zero(GoKind kind, GoStructDescriptor structDesc) {
        return switch (kind.slotKind()) {
            case Int -> Integer.valueOf(0);
            case Long -> Long.valueOf(0L);
            case Float -> Float.valueOf(0f);
            case Double -> Double.valueOf(0d);
            case Boolean -> Boolean.FALSE;
            case Object -> {
                if (kind == GoKind.STRING) {
                    yield GoString.EMPTY;
                }
                if (kind == GoKind.COMPLEX64) {
                    yield graalgo.values.GoComplex32.ZERO;
                }
                if (kind == GoKind.COMPLEX128 || kind == GoKind.UNTYPED_COMPLEX) {
                    yield graalgo.values.GoComplex64.ZERO;
                }
                if (kind == GoKind.STRUCT) {
                    if (structDesc == null) {
                        throw new FrontendException("struct zero needs a descriptor (WS E)");
                    }
                    yield GoStruct.zero(structDesc);
                }
                yield null;
            }
            default -> throw new FrontendException("no zero for kind " + kind + " (WS E)");
        };
    }

    /**
     * Allocates a fresh typed Java array for {@code len} elements of the
     * given element kind (D5 array mapping): int/uint ≤32→{@code int[]},
     * 64-bit→{@code long[]}, {@code float32}→{@code float[]},
     * {@code float64}→{@code double[]}, {@code bool}→{@code boolean[]},
     * {@code complex64}→{@code GoComplex32[]},
     * {@code complex128}→{@code GoComplex64[]}, everything
     * else→{@code Object[]}. For STRUCT elements, each slot is
     * pre-initialized to the zero-valued GoStruct (Go's array zero value
     * has zero-valued elements; a null slot would NPE on
     * {@code a[i].field} access). Requires the element descriptor for
     * struct kinds; may be null for non-struct kinds.
     */
    public static Object newArray(GoKind elemKind, GoStructDescriptor elemDesc, int len) {
        return switch (elemKind) {
            case INT8, INT16, INT32, UINT8, UINT16, UINT32 -> new int[len];
            case INT64, UINT64, UNTYPED_INT -> new long[len];
            case FLOAT32 -> new float[len];
            case FLOAT64, UNTYPED_FLOAT -> new double[len];
            case BOOL, UNTYPED_BOOL -> new boolean[len];
            case COMPLEX64 -> new graalgo.values.GoComplex32[len];
            case COMPLEX128, UNTYPED_COMPLEX -> new graalgo.values.GoComplex64[len];
            case STRUCT -> {
                Object[] arr = new Object[len];
                if (elemDesc != null) {
                    for (int i = 0; i < len; i++) {
                        arr[i] = GoStruct.zero(elemDesc);
                    }
                }
                yield arr;
            }
            default -> new Object[len];
        };
    }

    /**
     * Allocates a fresh typed Java array without struct zero-filling
     * (legacy): struct element slots stay null. Prefer
     * {@link #newArray(GoKind, GoStructDescriptor, int)} when the element
     * descriptor is available.
     */
    public static Object newArray(GoKind elemKind, int len) {
        return newArray(elemKind, null, len);
    }

    /** Reads one array element in canonical boxed form. */
    static Object arrayGet(Object array, GoKind elemKind, int i) {
        return switch (elemKind) {
            case INT8, INT16, INT32, UINT8, UINT16, UINT32 -> Integer.valueOf(((int[]) array)[i]);
            case INT64, UINT64, UNTYPED_INT -> Long.valueOf(((long[]) array)[i]);
            case FLOAT32 -> Float.valueOf(((float[]) array)[i]);
            case FLOAT64, UNTYPED_FLOAT -> Double.valueOf(((double[]) array)[i]);
            case BOOL, UNTYPED_BOOL -> Boolean.valueOf(((boolean[]) array)[i]);
            default -> ((Object[]) array)[i];
        };
    }

    /** Writes one canonical-boxed element into a typed Java array. */
    static void arraySet(Object array, GoKind elemKind, int i, Object boxed) {
        switch (elemKind) {
            case INT8, INT16, INT32, UINT8, UINT16, UINT32 -> ((int[]) array)[i] = ((Integer) boxed).intValue();
            case INT64, UINT64, UNTYPED_INT -> ((long[]) array)[i] = ((Long) boxed).longValue();
            case FLOAT32 -> ((float[]) array)[i] = ((Float) boxed).floatValue();
            case FLOAT64, UNTYPED_FLOAT -> ((double[]) array)[i] = ((Double) boxed).doubleValue();
            case BOOL, UNTYPED_BOOL -> ((boolean[]) array)[i] = ((Boolean) boxed).booleanValue();
            default -> ((Object[]) array)[i] = boxed;
        }
    }

    /**
     * Encodes a canonical boxed value into the {@code long[]} struct carrier
     * (ints as raw bits, floats as IEEE bits, bools as 0/1).
     *
     * <p>P3-M7: sub-64-bit int kinds (INT8/16/32, UINT8/16/32) are
     * canonically boxed as {@link Integer} (see {@link #box}), not
     * {@link Long} — accept any {@link Number} here. (Before: a blind
     * {@code (Long)} cast threw ClassCastException for struct literals
     * like {@code runePair{rune(c0), 1}}.)
     */
    static long toLongBits(GoKind kind, Object boxed) {
        return switch (kind) {
            case FLOAT32 -> (long) Float.floatToIntBits((Float) boxed);
            case FLOAT64 -> Double.doubleToLongBits((Double) boxed);
            case BOOL, UNTYPED_BOOL -> ((Boolean) boxed) ? 1L : 0L;
            default -> ((Number) boxed).longValue();
        };
    }

    /**
     * Decodes the {@code long[]} struct carrier back to canonical boxed form.
     *
     * <p>P3-M7: must return the canonical boxing per kind ({@link #box}):
     * sub-64-bit int kinds come back as {@link Integer} (sign/zero-extended
     * per {@code extendInt}), not {@link Long} — {@code SelectorNode}
     * callers cast to {@code Integer} for these kinds.
     */
    static Object fromLongBits(GoKind kind, long bits) {
        return switch (kind) {
            case FLOAT32 -> Float.valueOf(Float.intBitsToFloat((int) bits));
            case FLOAT64 -> Double.valueOf(Double.longBitsToDouble(bits));
            case BOOL, UNTYPED_BOOL -> bits != 0 ? Boolean.TRUE : Boolean.FALSE;
            case INT8 -> Integer.valueOf((int) (byte) bits);
            case INT16 -> Integer.valueOf((int) (short) bits);
            case INT32, UINT32 -> Integer.valueOf((int) bits);
            case UINT8 -> Integer.valueOf((int) bits & 0xFF);
            case UINT16 -> Integer.valueOf((int) bits & 0xFFFF);
            default -> Long.valueOf(bits);
        };
    }

    /**
     * Converts a canonically-boxed integer to {@code long} with the correct
     * extension for the kind (P3-M9 uint32 fix).
     *
     * <p>Sub-64-bit kinds box as {@link Integer}; for unsigned kinds the
     * {@code int} bits must be <em>zero</em>-extended (not sign-extended) so
     * that e.g. uint32(0xFFFFFFFF) becomes 4294967295L, not -1L. This keeps
     * {@code executeLong()} consistent across all producer paths (struct
     * fields, locals, constants, array elements), which the
     * {@link BinaryOpNode} unsigned comparisons rely on.
     */
    public static long toLong(Object boxed, graalgo.values.GoKind kind) {
        if (boxed instanceof Long l) {
            return l.longValue();
        }
        int i = ((Number) boxed).intValue();
        return kind.isUintKind() ? (i & 0xFFFFFFFFL) : (long) i;
    }

    /**
     * Fresh copy of a zero-value template (struct/array): templates are
     * built once per type at build time and must never be shared between
     * evaluations. Array copying goes through
     * {@link graalgo.values.GoStruct#deepCopyArray} — the single canonical
     * recursive array copier (struct elements deep-copied, nested arrays
     * recursed).
     */
    static Object freshZeroCopy(Object template) {
        if (template instanceof GoStruct gs) {
            return gs.deepCopy();
        }
        if (template != null && template.getClass().isArray()) {
            return GoStruct.deepCopyArray(template);
        }
        return template; // immutable (GoString.EMPTY) or null
    }
}
