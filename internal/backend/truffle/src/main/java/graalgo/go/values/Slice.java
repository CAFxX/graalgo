package graalgo.go.values;

/**
 * A Go slice header: backing object, the slot offsets of element 0 (one per region of
 * {@link Flat}), length and capacity (in elements). Immutable; the nil slice has a null
 * {@code base}. Element {@code i} of a slice whose element type has {@code np} scalar,
 * {@code nr} reference and {@code nb} byte slots lives at {@code base.prims[prim + i*np ..]},
 * {@code base.refs[ref + i*nr ..]}, {@code base.bytes[byt + i*nb ..]}; the element layout is
 * static, so it is a constant of the accessing node, not a field here. A slice of an array
 * shares the array's {@link Flat} (GG-0072: arrays are inlined in their enclosing object), and
 * this is the one slice shape: a {@code []byte} addresses {@code base.bytes} whether the backing
 * object was made for it or is a struct with an inlined {@code [N]byte} (ADR-0015 decision 3).
 */
public final class Slice {
    public static final Slice NIL = new Slice(null, 0, 0, 0, 0, 0);

    public final Flat base;
    public final int prim;
    public final int ref;
    public final int byt;
    public final int len;
    public final int cap;

    public Slice(Flat base, int prim, int ref, int byt, int len, int cap) {
        this.base = base;
        this.prim = prim;
        this.ref = ref;
        this.byt = byt;
        this.len = len;
        this.cap = cap;
    }
}
