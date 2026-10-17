package graalgo.values;

import java.util.List;

/**
 * Descriptor for a Go struct type (D5): the field layout used by
 * {@link GoStruct}.
 *
 * <p>Fields of primitive kinds ({@code long} carriers: all int/uint kinds;
 * {@code int} carriers: none yet — sub-64 ints live in {@code long[]}
 * too until WS D specializes; {@code float}/{@code double}/
 * {@code boolean} go to {@code longs[]} as bit patterns… ) — the exact
 * split is: every field whose value fits the {@code long[]} carrier by the
 * D5 mapping's primitive set goes to {@code longs} (ints as raw bits,
 * floats as {@code Float.floatToIntBits}/{@code Double.doubleToLongBits},
 * bools as 0/1); everything else (strings, slices, maps, nested structs,
 * pointers, interfaces, funcs) goes to {@code objects}. The descriptor is
 * built once per struct type from the frontend type table; field access
 * nodes (WS D) index through it.
 */
public final class GoStructDescriptor {
    /**
     * One field's placement. arrayLen is the array length for ARRAY-kind
     * fields (-1 for non-arrays); arrayElemKind is the element kind for
     * ARRAY-kind fields (null for non-arrays); arrayElemDesc is the element
     * struct descriptor for ARRAY-kind fields whose element kind is STRUCT
     * (null otherwise) — used by GoStruct.zero to pre-allocate struct array
     * elements (Go: new(T) gives zero-valued arrays, not null). Used by
     * GoStruct.zero to pre-allocate array fields (Go: new(T) gives
     * zero-valued arrays, not null). structDesc is the nested struct
     * descriptor for STRUCT-kind fields (null otherwise) — used by
     * Intrinsics.structSizeBytes to compute ABI sizes of nested structs
     * (P3-M4 scope extension: nested structs are no longer fail-fast),
     * and by GoStruct.zero to recursively materialize nested struct
     * fields (RV-P0 runtime-npe; opaque sync primitives are carved out
     * and stay null).
     */
    public record Field(String name, GoKind kind, boolean inLongs, int index, int arrayLen,
                        GoKind arrayElemKind, GoStructDescriptor arrayElemDesc,
                        GoStructDescriptor structDesc) {}

    private final String typeName;
    private final List<Field> fields;
    private final int longsCount;
    private final int objectsCount;

    public GoStructDescriptor(String typeName, List<Field> fields) {
        this.typeName = typeName;
        this.fields = List.copyOf(fields);
        int longs = 0;
        int objects = 0;
        for (Field f : fields) {
            if (f.inLongs()) {
                if (f.index() != longs) {
                    throw new IllegalArgumentException("longs index out of order for field " + f.name());
                }
                longs++;
            } else {
                if (f.index() != objects) {
                    throw new IllegalArgumentException("objects index out of order for field " + f.name());
                }
                objects++;
            }
        }
        this.longsCount = longs;
        this.objectsCount = objects;
    }

    /**
     * Assigns each field of the given kinds to the longs/objects carriers:
     * int/uint/float/bool kinds → {@code longs}, everything else →
     * {@code objects}. arrayLens[i] is the array length for ARRAY-kind
     * fields (-1 for non-arrays); arrayElemKinds[i] is the element kind
     * (null for non-arrays); arrayElemDescs[i] is the element struct
     * descriptor for ARRAY-kind fields whose element kind is STRUCT
     * (null otherwise); structDescs[i] is the nested struct descriptor for
     * STRUCT-kind fields (null otherwise).
     */
    public static GoStructDescriptor layout(String typeName, List<String> names, List<GoKind> kinds,
            List<Integer> arrayLens, List<GoKind> arrayElemKinds, List<GoStructDescriptor> arrayElemDescs,
            List<GoStructDescriptor> structDescs) {
        if (names.size() != kinds.size() || names.size() != arrayLens.size()
                || names.size() != arrayElemKinds.size() || names.size() != arrayElemDescs.size()
                || names.size() != structDescs.size()) {
            throw new IllegalArgumentException(
                    "names/kinds/arrayLens/arrayElemKinds/arrayElemDescs/structDescs length mismatch");
        }
        java.util.ArrayList<Field> fields = new java.util.ArrayList<>(names.size());
        int longs = 0;
        int objects = 0;
        for (int i = 0; i < names.size(); i++) {
            GoKind k = kinds.get(i);
            boolean inLongs = k.isIntKind() || k.isFloatKind() || k == GoKind.BOOL;
            fields.add(new Field(names.get(i), k, inLongs, inLongs ? longs++ : objects++,
                    arrayLens.get(i), arrayElemKinds.get(i), arrayElemDescs.get(i),
                    structDescs.get(i)));
        }
        return new GoStructDescriptor(typeName, fields);
    }

    /**
     * Layout without nested struct descriptors (all null): STRUCT-kind
     * fields keep the legacy fail-fast in Intrinsics.structSizeBytes.
     * Prefer the seven-argument overload when nested descriptors are
     * available.
     */
    public static GoStructDescriptor layout(String typeName, List<String> names, List<GoKind> kinds,
            List<Integer> arrayLens, List<GoKind> arrayElemKinds, List<GoStructDescriptor> arrayElemDescs) {
        java.util.ArrayList<GoStructDescriptor> descs = new java.util.ArrayList<>(names.size());
        for (int i = 0; i < names.size(); i++) {
            descs.add(null);
        }
        return layout(typeName, names, kinds, arrayLens, arrayElemKinds, arrayElemDescs, descs);
    }

    /**
     * Layout without array element descriptors (all null): struct array
     * elements are left uninitialized (legacy behavior). Prefer the
     * six-argument overload when element descriptors are available.
     */
    public static GoStructDescriptor layout(String typeName, List<String> names, List<GoKind> kinds,
            List<Integer> arrayLens, List<GoKind> arrayElemKinds) {
        java.util.ArrayList<GoStructDescriptor> descs = new java.util.ArrayList<>(names.size());
        for (int i = 0; i < names.size(); i++) {
            descs.add(null);
        }
        return layout(typeName, names, kinds, arrayLens, arrayElemKinds, descs);
    }

    /**
     * Legacy layout without array lengths (all -1/null). For call sites that
     * don't have array length info.
     */
    public static GoStructDescriptor layout(String typeName, List<String> names, List<GoKind> kinds) {
        java.util.ArrayList<Integer> lens = new java.util.ArrayList<>(names.size());
        java.util.ArrayList<GoKind> elemKinds = new java.util.ArrayList<>(names.size());
        for (int i = 0; i < names.size(); i++) {
            lens.add(-1);
            elemKinds.add(null);
        }
        return layout(typeName, names, kinds, lens, elemKinds);
    }

    public String typeName() {
        return typeName;
    }

    public List<Field> fields() {
        return fields;
    }

    public int longsCount() {
        return longsCount;
    }

    public int objectsCount() {
        return objectsCount;
    }

    public Field field(int i) {
        return fields.get(i);
    }
}
