package graalgo.go.values;

/**
 * An interior Go pointer: the referent object plus the slot offsets of the pointee inside it
 * (SPEC §3.4: "Java reference to the referent plus an offset", GG-0062) — one offset per region
 * of {@link Flat}. A pointer to the start of an object is the {@link Flat} itself ({@link #make}
 * never builds a {@code Ptr} with all offsets zero), so every location has exactly one
 * representation per static pointee type and pointer equality is {@link #same}.
 *
 * <p>The gc byte offset of the location is not stored: it is derived on demand from the
 * referent type's inverse leaf table (the offsets sum to a leaf index), so pointer values do
 * not grow for {@code unsafe} arithmetic (ADR-0015 decision 1).
 */
public record Ptr(Flat obj, int prim, int ref, int byt) {
    /** Use {@link #make}: a pointer to an object's start is the object itself. */
    public Ptr {
        assert (prim | ref | byt) != 0 : "Ptr with zero offsets: use Ptr.make";
    }

    /** The pointer to the location ({@code obj}, {@code prim}, {@code ref}, {@code byt}). */
    public static Object make(Flat obj, int prim, int ref, int byt) {
        return (prim | ref | byt) == 0 ? obj : new Ptr(obj, prim, ref, byt);
    }

    /** Go pointer equality over the two representations (null is nil). */
    public static boolean same(Object a, Object b) {
        if (a == b) {
            return true;
        }
        if (a instanceof Ptr p && b instanceof Ptr q) {
            return p.obj == q.obj && p.prim == q.prim && p.ref == q.ref && p.byt == q.byt;
        }
        // A view names its location by gc byte offset: compare locations, not shapes.
        return (a instanceof View || b instanceof View) && graalgo.go.runtime.Unsafe.sameLocation(a, b);
    }

    @Override
    public boolean equals(Object o) {
        return same(this, o);
    }

    @Override
    public int hashCode() {
        return graalgo.go.runtime.Unsafe.hashPointer(this);
    }
}
