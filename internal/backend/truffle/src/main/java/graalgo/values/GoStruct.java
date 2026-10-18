package graalgo.values;

/**
 * A Go struct value: descriptor + {@code long[]} + {@code Object[]} (D5).
 *
 * <p>Structs are <em>value</em> types: assignment and argument passing copy
 * the value. {@link #deepCopy()} is the Go assignment semantics — nested
 * struct and array values are copied recursively, while slices, maps,
 * strings, pointers, and interfaces stay shared references. {@link #copy()}
 * is a shallow carrier copy (fields alias); it is <em>not</em> Go assignment
 * semantics for nested value fields.
 */
public final class GoStruct {
    private final GoStructDescriptor descriptor;
    private final long[] longs;
    private final Object[] objects;

    public GoStruct(GoStructDescriptor descriptor, long[] longs, Object[] objects) {
        if (longs.length != descriptor.longsCount() || objects.length != descriptor.objectsCount()) {
            throw new IllegalArgumentException("carrier length mismatch for " + descriptor.typeName());
        }
        this.descriptor = descriptor;
        this.longs = longs.clone();
        this.objects = objects.clone();
    }

    /** The Go zero value for the described struct type. */
    public static GoStruct zero(GoStructDescriptor descriptor) {
        Object[] objects = new Object[descriptor.objectsCount()];
        for (int i = 0; i < descriptor.fields().size(); i++) {
            GoStructDescriptor.Field f = descriptor.field(i);
            if (!f.inLongs() && f.kind() == GoKind.STRING) {
                objects[f.index()] = GoString.EMPTY;
            } else if (!f.inLongs() && f.kind() == GoKind.ARRAY && f.arrayLen() >= 0 && f.arrayElemKind() != null) {
                // P3-M8b: Go new(T) gives zero-valued arrays, not null.
                // Pre-allocate so d.h[0]=x doesn't NPE. P3-M9: struct array
                // elements are zero-filled via the element descriptor (same
                // root cause as the make([]T,n) slice fix in Intrinsics).
                objects[f.index()] = graalgo.frontend.Boxing.newArray(
                        f.arrayElemKind(), f.arrayElemDesc(), f.arrayLen());
            } else if (!f.inLongs() && f.kind().isComplexKind()) {
                // WS G: a complex field's zero value is (0+0i), not nil.
                objects[f.index()] = f.kind() == GoKind.COMPLEX64
                        ? GoComplex32.ZERO : GoComplex64.ZERO;
            }
        }
        return new GoStruct(descriptor, new long[descriptor.longsCount()], objects);
    }

    public GoStructDescriptor descriptor() {
        return descriptor;
    }

    public long getLong(int fieldIndex) {
        GoStructDescriptor.Field f = descriptor.field(fieldIndex);
        if (!f.inLongs()) {
            throw new IllegalArgumentException("field " + f.name() + " is not a longs-carried field");
        }
        return longs[f.index()];
    }

    public void setLong(int fieldIndex, long value) {
        GoStructDescriptor.Field f = descriptor.field(fieldIndex);
        if (!f.inLongs()) {
            throw new IllegalArgumentException("field " + f.name() + " is not a longs-carried field");
        }
        longs[f.index()] = value;
    }

    public Object getObject(int fieldIndex) {
        GoStructDescriptor.Field f = descriptor.field(fieldIndex);
        if (f.inLongs()) {
            throw new IllegalArgumentException("field " + f.name() + " is not an objects-carried field");
        }
        return objects[f.index()];
    }

    public void setObject(int fieldIndex, Object value) {
        GoStructDescriptor.Field f = descriptor.field(fieldIndex);
        if (f.inLongs()) {
            throw new IllegalArgumentException("field " + f.name() + " is not an objects-carried field");
        }
        objects[f.index()] = value;
    }

    /** Shallow carrier copy (fields alias) — not Go assignment semantics. */
    public GoStruct copy() {
        return new GoStruct(descriptor, longs, objects);
    }

    /**
     * Deep value copy: Go assignment semantics for structs. Nested struct
     * values and array values are copied recursively; slices, maps,
     * strings, pointers, and interfaces are references and stay shared.
     * Every struct-typed assignment, interface box/unbox, and container
     * store of a struct value goes through this, so {@code b := a; b.E.X
     * = 1} never observably mutates {@code a} (M4, WS E).
     */
    public GoStruct deepCopy() {
        Object[] objs = new Object[objects.length];
        for (int i = 0; i < descriptor.fields().size(); i++) {
            GoStructDescriptor.Field f = descriptor.field(i);
            if (f.inLongs()) {
                continue;
            }
            Object v = objects[f.index()];
            if (v instanceof GoStruct gs) {
                v = gs.deepCopy();
            } else if (v != null && v.getClass().isArray()) {
                v = deepCopyArray(v);
            }
            objs[f.index()] = v;
        }
        return new GoStruct(descriptor, longs, objs);
    }

    /**
     * Deep copy of a Go array value: a fresh array whose struct elements
     * are deep-copied and whose nested arrays are copied recursively.
     * Slices, maps, pointers, interfaces, and strings are references and
     * stay shared (Go value semantics — only structs and arrays are
     * copied by assignment). A shallow {@code System.arraycopy} here
     * would leave struct element slots aliased across copies, so that
     * {@code a[i].field = x} on one copy observably mutates the other.
     */
    public static Object deepCopyArray(Object array) {
        Class<?> ct = array.getClass().getComponentType();
        int n = java.lang.reflect.Array.getLength(array);
        Object out = java.lang.reflect.Array.newInstance(ct, n);
        if (out instanceof Object[] oa) {
            Object[] src = (Object[]) array;
            for (int i = 0; i < n; i++) {
                Object e = src[i];
                if (e instanceof GoStruct gs) {
                    e = gs.deepCopy();
                } else if (e instanceof graalgo.runtime.sync.GoMutex) {
                    // P3-M9 (driver-harden, go-cmp lane): copied sync
                    // primitives get fresh instances per slot — sharing
                    // would let operations through one copy be observed
                    // through another, which gc's value copy does not do.
                    e = new graalgo.runtime.sync.GoMutex();
                } else if (e instanceof graalgo.runtime.sync.GoRWMutex) {
                    e = new graalgo.runtime.sync.GoRWMutex();
                } else if (e instanceof graalgo.runtime.sync.GoWaitGroup) {
                    e = new graalgo.runtime.sync.GoWaitGroup();
                } else if (e instanceof graalgo.runtime.sync.GoOnce) {
                    e = new graalgo.runtime.sync.GoOnce();
                } else if (e instanceof graalgo.runtime.sync.GoPool) {
                    e = new graalgo.runtime.sync.GoPool();
                } else if (e != null && e.getClass().isArray()) {
                    e = deepCopyArray(e);
                }
                oa[i] = e;
            }
        } else {
            System.arraycopy(array, 0, out, 0, n);
        }
        return out;
    }

    @Override
    public boolean equals(Object o) {
        return o instanceof GoStruct gs && descriptor == gs.descriptor
                && longsEqual(gs.longs) && structObjectsEqual(objects, gs.objects);
    }

    /**
     * Field-wise comparison of the {@code longs[]} carrier against another
     * struct of the same (identical) descriptor. Integer and bool slots
     * compare bit-exact (their boxing is canonical); float slots compare
     * with IEEE {@code ==} semantics decoded from the stored bit patterns
     * — {@code +0.0 == -0.0}, and NaN never equals anything, including a
     * bit-identical NaN or the struct itself — matching gc struct
     * {@code ==}. (Before: a single bit-exact {@code Arrays.equals} here
     * equated bit-identical NaN fields and distinguished signed zeros;
     * review 2026-10-18.)
     */
    private boolean longsEqual(long[] other) {
        for (GoStructDescriptor.Field f : descriptor.fields()) {
            if (!f.inLongs()) {
                continue;
            }
            long x = longs[f.index()];
            long y = other[f.index()];
            if (f.kind().isFloatKind()) {
                if (!floatBitsEqual(f.kind(), x, y)) {
                    return false;
                }
            } else if (x != y) {
                return false;
            }
        }
        return true;
    }

    /**
     * IEEE {@code ==} on the float values encoded by
     * {@link graalgo.frontend.Boxing#toLongBits} (float32 slots hold
     * {@code Float.floatToIntBits} zero-extended into the long; float64
     * slots hold {@code Double.doubleToLongBits}). {@code x == y} is
     * already false for NaN, so no explicit NaN check is needed.
     */
    private static boolean floatBitsEqual(GoKind kind, long x, long y) {
        if (kind == GoKind.FLOAT32) {
            return Float.intBitsToFloat((int) x) == Float.intBitsToFloat((int) y);
        }
        // FLOAT64 (and any other float kind, per GoEquality's "double for
        // both float kinds" carrier convention).
        return Double.longBitsToDouble(x) == Double.longBitsToDouble(y);
    }

    private static boolean structObjectsEqual(Object[] a, Object[] b) {
        if (a.length != b.length) {
            return false;
        }
        for (int i = 0; i < a.length; i++) {
            if (!GoEquality.equal(a[i], b[i])) {
                return false;
            }
        }
        return true;
    }

    @Override
    public int hashCode() {
        int h = System.identityHashCode(descriptor);
        // Mirrors Arrays.hashCode(longs) element-wise (31*h + Long.hashCode
        // per slot, from 1 — so non-float structs hash exactly as before),
        // except float slots are funneled through GoEquality's signed-zero
        // normalizer: equal (+0.0/-0.0) structs must hash equal (review
        // 2026-10-18). NaN needs no normalization — it never equals
        // anything, so its canonical hash is a mere collision.
        int lh = 1;
        for (GoStructDescriptor.Field f : descriptor.fields()) {
            if (!f.inLongs()) {
                continue;
            }
            long bits = longs[f.index()];
            int c;
            if (f.kind() == GoKind.FLOAT32) {
                c = GoEquality.hashFloat(Float.intBitsToFloat((int) bits));
            } else if (f.kind().isFloatKind()) {
                c = GoEquality.hashDouble(Double.longBitsToDouble(bits));
            } else {
                c = Long.hashCode(bits);
            }
            lh = 31 * lh + c;
        }
        h = 31 * h + lh;
        for (Object o : objects) {
            h = 31 * h + GoEquality.hash(o);
        }
        return h;
    }

    @Override
    public String toString() {
        return "GoStruct{" + descriptor.typeName() + "}";
    }
}
