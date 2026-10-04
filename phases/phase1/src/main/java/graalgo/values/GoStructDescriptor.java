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
    /** One field's placement. */
    public record Field(String name, GoKind kind, boolean inLongs, int index) {}

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
     * {@code objects}.
     */
    public static GoStructDescriptor layout(String typeName, List<String> names, List<GoKind> kinds) {
        if (names.size() != kinds.size()) {
            throw new IllegalArgumentException("names/kinds length mismatch");
        }
        java.util.ArrayList<Field> fields = new java.util.ArrayList<>(names.size());
        int longs = 0;
        int objects = 0;
        for (int i = 0; i < names.size(); i++) {
            GoKind k = kinds.get(i);
            boolean inLongs = k.isIntKind() || k.isFloatKind() || k == GoKind.BOOL;
            fields.add(new Field(names.get(i), k, inLongs, inLongs ? longs++ : objects++));
        }
        return new GoStructDescriptor(typeName, fields);
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
