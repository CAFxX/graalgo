package graalgo.go.values;

/**
 * A view pointer (SPEC §3.4, GG-0062; ADR-0015 decision 1): the referent object plus a gc byte
 * offset. It is what a conversion from {@code unsafe.Pointer} to {@code *U} yields when
 * {@code U}'s leaves do not coincide with the referent's leaves at that location, and what
 * {@code unsafe.Pointer} arithmetic yields. Loads and stores through it go by byte offset
 * through the referent's leaf table ({@code runtime.Unsafe}); typed code holds a {@link Flat} or
 * a {@link Ptr} and never sees one.
 */
public final class View {
    public final Flat obj;
    public final long off;

    public View(Flat obj, long off) {
        this.obj = obj;
        this.off = off;
    }
}
