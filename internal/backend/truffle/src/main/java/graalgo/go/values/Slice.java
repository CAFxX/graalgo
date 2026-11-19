package graalgo.go.values;

/**
 * A Go slice header: backing object, the slot offsets of element 0, length and capacity (in
 * elements). Immutable; the nil slice has a null {@code base}. Element {@code i} of a slice
 * whose element type has {@code np} prim and {@code nr} ref slots lives at
 * {@code base.prims[prim + i*np ..]} / {@code base.refs[ref + i*nr ..]}; the element layout is
 * static, so it is a constant of the accessing node, not a field here. A slice of an array
 * shares the array's {@link Flat} (GG-0072: arrays are inlined in their enclosing object).
 */
public final class Slice {
    public static final Slice NIL = new Slice(null, 0, 0, 0, 0);

    public final Flat base;
    public final int prim;
    public final int ref;
    public final int len;
    public final int cap;

    public Slice(Flat base, int prim, int ref, int len, int cap) {
        this.base = base;
        this.prim = prim;
        this.ref = ref;
        this.len = len;
        this.cap = cap;
    }
}
