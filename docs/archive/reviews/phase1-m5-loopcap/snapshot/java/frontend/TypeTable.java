package graalgo.frontend;

/**
 * Read view over the frontend/v1 type table ({@code "types"} array).
 *
 * <p>Every go/types type reachable from the program is serialized exactly
 * once; expressions reference entries by index. Bounds-checked access turns a
 * dangling id into {@link FrontendException}, never an
 * {@link IndexOutOfBoundsException}.
 */
public final class TypeTable {
    private final Json.Array entries;

    public TypeTable(Json.Array entries) {
        this.entries = entries;
    }

    public int size() {
        return entries.size();
    }

    public Json.Object entry(int id, String path) {
        if (id < 0 || id >= entries.size()) {
            throw new FrontendException(path + ": dangling type id " + id
                    + " (table has " + entries.size() + " entries)");
        }
        Json.Value v = entries.get(id);
        if (!(v instanceof Json.Object o)) {
            throw new FrontendException(path + ": type entry " + id + " is not an object");
        }
        return o;
    }
}
