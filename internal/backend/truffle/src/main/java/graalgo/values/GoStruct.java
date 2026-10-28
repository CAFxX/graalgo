package graalgo.values;

import java.util.Arrays;

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

    /**
     * The struct value this value is the field-0 (prefix) storage of,
     * or null (fix-redmaster-r40). In gc a value-embedded struct field
     * at offset 0 shares its owner's address, so a pointer to the
     * field is a pointer to the owner under another type; this link
     * records that provenance when a struct becomes another struct's
     * field-0 value (construction or {@link #setObject}), letting the
     * unsafe prefix-reinterpretation view recover the true owner
     * instead of fabricating a wrapper with the extension fields
     * zeroed. Only field 0 is registered: it is the only field
     * guaranteed to sit at byte offset 0. If the same value is stored
     * at field 0 of several owners, the FIRST store wins
     * (fix-redmaster-r40, reversing the earlier most-recent-store
     * rule): the first owner is the canonical wider allocation — for
     * abi type shells, the shell minted by the reflect machinery
     * before any transient reinterpretation wrapper is fabricated
     * around the same prefix — and prefix conversions must recover
     * that canonical owner, not whichever wrapper touched the value
     * last. A documented corner of the view mechanism in
     * {@code graalgo.p3unsafe.PrefixView}.
     */
    private GoStruct prefixParent;

    public GoStruct(GoStructDescriptor descriptor, long[] longs, Object[] objects) {
        if (longs.length != descriptor.longsCount() || objects.length != descriptor.objectsCount()) {
            throw new IllegalArgumentException("carrier length mismatch for " + descriptor.typeName());
        }
        this.descriptor = descriptor;
        this.longs = longs.clone();
        this.objects = objects.clone();
        registerPrefixChild();
    }

    /** The field-0 prefix owner of this value, or null if none is known. */
    public GoStruct prefixParent() {
        return prefixParent;
    }

    /** Records this struct as the prefix owner of its field-0 struct value. */
    private void registerPrefixChild() {
        if (descriptor.fields().isEmpty()) {
            return;
        }
        GoStructDescriptor.Field f0 = descriptor.field(0);
        if (f0.kind() == GoKind.STRUCT && !f0.inLongs()
                && objects[f0.index()] instanceof GoStruct child
                && child.prefixParent == null) {
            child.prefixParent = this;
        }
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
            } else if (!f.inLongs() && f.kind() == GoKind.STRUCT
                    && f.structDesc() != null) {
                // r38: a struct-typed field's zero value is a zero struct,
                // never null. Previously this slot stayed null, so reading
                // through a value-embedded field of a zero struct panicked
                // with a nil dereference instead of yielding the zero value
                // — e.g. make([]sync.poolLocal, n)[i].private, which is how
                // every sync.Pool.Put/Get died (the poolLocal element's
                // embedded poolLocalInternal was null). Recursion
                // terminates: value-embedding cycles are illegal in Go,
                // and descriptor construction already fails fast on cyclic
                // type graphs; descriptors without nested metadata (the
                // ReflectTypes-synthesized abi shells) keep the old null
                // slot, since those shells are always filled explicitly
                // by their builder.
                objects[f.index()] = GoStruct.zero(f.structDesc());
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
        if (fieldIndex == 0 && f.kind() == GoKind.STRUCT && value instanceof GoStruct child
                && child.prefixParent == null) {
            child.prefixParent = this;
        }
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
                && Arrays.equals(longs, gs.longs) && structObjectsEqual(objects, gs.objects);
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
        h = 31 * h + Arrays.hashCode(longs);
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
