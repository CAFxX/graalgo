package graalgo.go.values;

import graalgo.go.types.GoType;
import java.util.Arrays;

/**
 * A flat Go memory object (SPEC §3.4, GG-0061): scalar leaves in {@code prims} (integers
 * sign/zero-extended, floats as raw {@code double} bits, booleans 0/1), reference leaves in
 * {@code refs}. Nested structs and arrays are inlined; the owning {@link GoType}'s layout maps a
 * field or element to its (prim, ref) slot offsets.
 *
 * <p>One class serves every addressable object: struct and array values, the backing store of a
 * slice, and the heap box of an escaping (captured or address-taken) variable. A {@code Flat}
 * reference in a pointer-typed position is the pointer to the object's start; interior pointers
 * are {@link Ptr}. Value semantics are the factory's job: a {@code Flat} held by a variable is
 * never shared with another variable.
 *
 * <p>Not yet: toolexec work package W13 (the pointer model, milestone M1r, GG-0062) adds the gc
 * byte offset — {@code type} will name the leaf table that maps a byte offset to a slot pair.
 */
public final class Flat {
    public static final long[] NO_PRIMS = new long[0];
    public static final Object[] NO_REFS = new Object[0];

    /** The type whose layout describes the object; null for slice backing stores. */
    public final GoType type;
    public final long[] prims;
    public final Object[] refs;

    public Flat(GoType type, long[] prims, Object[] refs) {
        this.type = type;
        this.prims = prims;
        this.refs = refs;
    }

    /** A zeroed object with the given slot counts. */
    public static Flat zero(GoType type, int nPrim, int nRef) {
        return new Flat(type, nPrim == 0 ? NO_PRIMS : new long[nPrim], nRef == 0 ? NO_REFS : new Object[nRef]);
    }

    /** A copy of the whole object (value assignment). */
    public Flat copy() {
        return new Flat(type, prims.length == 0 ? NO_PRIMS : prims.clone(), refs.length == 0 ? NO_REFS : refs.clone());
    }

    /** A copy of the sub-object of {@code nPrim}/{@code nRef} slots at the given offsets. */
    public Flat sub(GoType subType, int primOff, int nPrim, int refOff, int nRef) {
        return new Flat(subType,
                nPrim == 0 ? NO_PRIMS : Arrays.copyOfRange(prims, primOff, primOff + nPrim),
                nRef == 0 ? NO_REFS : Arrays.copyOfRange(refs, refOff, refOff + nRef));
    }

    /** Overwrites the sub-object at the given offsets with the whole of {@code src}. */
    public void store(int primOff, int refOff, Flat src) {
        System.arraycopy(src.prims, 0, prims, primOff, src.prims.length);
        System.arraycopy(src.refs, 0, refs, refOff, src.refs.length);
    }
}
