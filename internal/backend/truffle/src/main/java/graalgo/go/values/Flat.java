package graalgo.go.values;

import graalgo.go.types.GoType;
import java.util.Arrays;

/**
 * A flat Go memory object (SPEC §3.4, GG-0072): scalar leaves wider than a byte in
 * {@code prims} (integers sign/zero-extended, floats as raw {@code double} bits), reference
 * leaves in {@code refs}, and every byte-sized leaf — {@code uint8}, {@code int8}, {@code bool}
 * (0/1), as fields and as array elements alike — in {@code bytes} (ADR-0015 decision 3). Nested
 * structs and arrays are inlined; the owning {@link GoType}'s layout maps a field or element to
 * its (prim, ref, byte) slot offsets, and its leaf table maps gc byte offsets to slots.
 *
 * <p>One class serves every addressable object: struct and array values, the backing store of a
 * slice, and the heap box of an escaping (captured or address-taken) variable. A {@code Flat}
 * reference in a pointer-typed position is the pointer to the object's start; interior pointers
 * are {@link Ptr}. Value semantics are the factory's job: a {@code Flat} held by a variable is
 * never shared with another variable.
 *
 * <p>The region of a leaf is static (the leaf kind of the static type), so typed loads and
 * stores index one array at a constant or frame-held offset and test no shape. A {@code []byte}
 * over a {@code make}d array and one over a struct's inlined {@code [N]byte} field both address
 * {@code bytes}.
 */
public final class Flat {
    public static final long[] NO_PRIMS = new long[0];
    public static final Object[] NO_REFS = new Object[0];
    public static final byte[] NO_BYTES = new byte[0];

    /** The type whose layout describes the object; null for slice backing stores. */
    public final GoType type;
    public final long[] prims;
    public final Object[] refs;
    public final byte[] bytes;

    public Flat(GoType type, long[] prims, Object[] refs, byte[] bytes) {
        this.type = type;
        this.prims = prims;
        this.refs = refs;
        this.bytes = bytes;
    }

    /** A copy of the whole object (value assignment). */
    public Flat copy() {
        return new Flat(type, prims.length == 0 ? NO_PRIMS : prims.clone(), refs.length == 0 ? NO_REFS : refs.clone(),
                bytes.length == 0 ? NO_BYTES : bytes.clone());
    }

    /** A copy of the sub-object of the given slot counts at the given offsets. */
    public Flat sub(GoType subType, int primOff, int nPrim, int refOff, int nRef, int byteOff, int nByte) {
        return new Flat(subType,
                nPrim == 0 ? NO_PRIMS : Arrays.copyOfRange(prims, primOff, primOff + nPrim),
                nRef == 0 ? NO_REFS : Arrays.copyOfRange(refs, refOff, refOff + nRef),
                nByte == 0 ? NO_BYTES : Arrays.copyOfRange(bytes, byteOff, byteOff + nByte));
    }

    /** Overwrites the sub-object at the given offsets with the whole of {@code src}. */
    public void store(int primOff, int refOff, int byteOff, Flat src) {
        System.arraycopy(src.prims, 0, prims, primOff, src.prims.length);
        System.arraycopy(src.refs, 0, refs, refOff, src.refs.length);
        System.arraycopy(src.bytes, 0, bytes, byteOff, src.bytes.length);
    }
}
