package graalgo.go.values;

/**
 * An interior Go pointer: the referent object plus the slot offsets of the pointee inside it
 * (SPEC §3.4: "Java reference to the referent plus an offset", GG-0062). A pointer to the start
 * of an object is the {@link Flat} itself ({@link #make} never builds a {@code Ptr} with both
 * offsets zero), so every location has exactly one representation per static pointee type and
 * pointer equality is {@link #same}.
 *
 * <p>W13 adds the gc byte offset next to the slot offsets (unsafe arithmetic, {@code uintptr}).
 */
public final class Ptr {
    public final Flat obj;
    public final int prim;
    public final int ref;

    private Ptr(Flat obj, int prim, int ref) {
        this.obj = obj;
        this.prim = prim;
        this.ref = ref;
    }

    /** The pointer to the location ({@code obj}, {@code prim}, {@code ref}). */
    public static Object make(Flat obj, int prim, int ref) {
        return (prim | ref) == 0 ? obj : new Ptr(obj, prim, ref);
    }

    /** Go pointer equality over the two representations (null is nil). */
    public static boolean same(Object a, Object b) {
        if (a == b) {
            return true;
        }
        return a instanceof Ptr p && b instanceof Ptr q && p.obj == q.obj && p.prim == q.prim && p.ref == q.ref;
    }

    @Override
    public boolean equals(Object o) {
        return same(this, o);
    }

    @Override
    public int hashCode() {
        return System.identityHashCode(obj) * 31 + prim * 7 + ref;
    }
}
