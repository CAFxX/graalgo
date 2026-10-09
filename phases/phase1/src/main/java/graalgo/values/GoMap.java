package graalgo.values;

/**
 * A Go map value: open-addressing hash table with linear probing (D5).
 *
 * <p>Keys use {@link GoEquality} (Go {@code ==} semantics — a NaN key never
 * matches, matching Go). {@link #get} returns {@code null} for absent keys;
 * map-access nodes substitute the element type's zero value (Go's
 * comma-ok form arrives with WS D). Tombstones keep probe chains intact
 * across deletes; the table doubles when occupancy (including tombstones)
 * exceeds 70%.
 */
public final class GoMap {
    private static final byte EMPTY = 0;
    private static final byte OCCUPIED = 1;
    private static final byte TOMBSTONE = 2;
    private static final double MAX_LOAD = 0.7;

    private Object[] keys;
    private Object[] values;
    private byte[] states;
    private int mask;
    private int size;
    private int tombstones;

    public GoMap() {
        this(8);
    }

    public GoMap(int hint) {
        int n = 8;
        while (n < Math.max(8, hint * 2)) {
            n *= 2;
        }
        keys = new Object[n];
        values = new Object[n];
        states = new byte[n];
        mask = n - 1;
    }

    public int size() {
        return size;
    }

    /** Returns the value for {@code key}, or {@code null} if absent. */
    public Object get(Object key) {
        int i = findSlot(key);
        return i >= 0 ? values[i] : null;
    }

    /** Inserts or replaces {@code key → value}. */
    public void put(Object key, Object value) {
        if (key == null) {
            throw new IllegalArgumentException("Go map keys are never null in Phase 1 (WS D refines)");
        }
        if ((size + tombstones + 1) > (int) (keys.length * MAX_LOAD)) {
            resize(keys.length * 2);
        }
        int h = GoEquality.hash(key) & mask;
        int firstTombstone = -1;
        for (int probe = 0; probe < keys.length; probe++) {
            int i = (h + probe) & mask;
            byte s = states[i];
            if (s == EMPTY) {
                int at = firstTombstone >= 0 ? firstTombstone : i;
                if (firstTombstone >= 0) {
                    tombstones--;
                }
                keys[at] = key;
                values[at] = value;
                states[at] = OCCUPIED;
                size++;
                return;
            }
            if (s == TOMBSTONE) {
                if (firstTombstone < 0) {
                    firstTombstone = i;
                }
            } else if (GoEquality.equal(keys[i], key)) {
                values[i] = value;
                return;
            }
        }
        // Table full of tombstones (should not happen given the load check):
        // resize and retry.
        resize(keys.length * 2);
        put(key, value);
    }

    /** Removes {@code key}; no-op if absent. */
    public void delete(Object key) {
        int i = findSlot(key);
        if (i >= 0) {
            keys[i] = null;
            values[i] = null;
            states[i] = TOMBSTONE;
            size--;
            tombstones++;
        }
    }

    /** Removes all entries (the {@code clear} builtin). */
    public void clear() {
        java.util.Arrays.fill(keys, null);
        java.util.Arrays.fill(values, null);
        java.util.Arrays.fill(states, EMPTY);
        size = 0;
        tombstones = 0;
    }

    /** True when {@code key} is present (even with a null value). */
    public boolean containsKey(Object key) {
        return findSlot(key) >= 0;
    }

    /**
     * Snapshot of the present keys, in table order. Used by range-over-map:
     * entries deleted mid-iteration are skipped by re-checking
     * {@link #containsKey} at production time (Go spec: a not-yet-reached
     * entry removed during iteration is not produced); entries created
     * mid-iteration are never produced (the spec permits skipping them).
     */
    public Object[] keysSnapshot() {
        Object[] out = new Object[size];
        int j = 0;
        for (int i = 0; i < keys.length; i++) {
            if (states[i] == OCCUPIED) {
                out[j++] = keys[i];
            }
        }
        return out;
    }

    /** Slot index holding {@code key}, or -1. */
    private int findSlot(Object key) {
        int h = GoEquality.hash(key) & mask;
        for (int probe = 0; probe < keys.length; probe++) {
            int i = (h + probe) & mask;
            byte s = states[i];
            if (s == EMPTY) {
                return -1;
            }
            if (s == OCCUPIED && GoEquality.equal(keys[i], key)) {
                return i;
            }
        }
        return -1;
    }

    private void resize(int n) {
        Object[] oldKeys = keys;
        Object[] oldValues = values;
        byte[] oldStates = states;
        keys = new Object[n];
        values = new Object[n];
        states = new byte[n];
        mask = n - 1;
        size = 0;
        tombstones = 0;
        for (int i = 0; i < oldKeys.length; i++) {
            if (oldStates[i] == OCCUPIED) {
                put(oldKeys[i], oldValues[i]);
            }
        }
    }
}
