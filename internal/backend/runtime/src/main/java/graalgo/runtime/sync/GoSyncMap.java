package graalgo.runtime.sync;

import graalgo.values.GoEquality;
import graalgo.values.GoInterface;
import graalgo.values.GoMap;

/**
 * A {@code sync.Map} Go value (fix-linkname, Caddy/godebug lane).
 *
 * <p>A {@code sync.Map} Go value <em>is</em> the {@link GoSyncMap} Java
 * object (opaque, ADR-0002 D1). Backed by {@link GoMap}, so keys use Go
 * {@code ==} semantics ({@link GoEquality} — a NaN key never matches,
 * like Go). All methods are synchronized: gc shards by key hash for
 * throughput, but coarse locking is observationally equivalent (the race
 * detector aside, which graalgo does not implement).
 *
 * <p>Keys and values arrive from the syncop node boxed as
 * {@link GoInterface} (the {@code any}-typed API), so interface keys
 * compare by (descriptor, data) exactly like Go map interface keys. A
 * Java-null key (a nil interface key, legal in gc's sync.Map) maps to a
 * private sentinel; a Java-null value normalizes to
 * {@link GoInterface#NIL}.
 */
public final class GoSyncMap {
    /**
     * Sentinel for nil interface keys: a Java-null key (a nil interface
     * key, legal in gc's sync.Map) maps to this private marker via
     * {@link #normKey}, keeping sync.Map's nil-key representation local
     * to this class.
     */
    private static final Object NIL_KEY = new Object();

    private final GoMap map = new GoMap();

    /** The (value, ok) result of the loading operations. */
    public record LoadResult(Object value, boolean ok) {}

    private static Object normKey(Object k) {
        return k == null ? NIL_KEY : k;
    }

    private static Object normValue(Object v) {
        return v == null ? GoInterface.NIL : v;
    }

    public synchronized LoadResult load(Object key) {
        Object k = normKey(key);
        if (!map.containsKey(k)) {
            return new LoadResult(null, false);
        }
        return new LoadResult(map.get(k), true);
    }

    public synchronized void store(Object key, Object value) {
        map.put(normKey(key), normValue(value));
    }

    public synchronized LoadResult loadOrStore(Object key, Object value) {
        Object k = normKey(key);
        if (map.containsKey(k)) {
            return new LoadResult(map.get(k), true);
        }
        Object v = normValue(value);
        map.put(k, v);
        return new LoadResult(v, false);
    }

    public synchronized LoadResult loadAndDelete(Object key) {
        LoadResult r = load(key);
        if (r.ok()) {
            map.delete(normKey(key));
        }
        return r;
    }

    public synchronized void delete(Object key) {
        map.delete(normKey(key));
    }

    public synchronized LoadResult swap(Object key, Object value) {
        Object k = normKey(key);
        LoadResult prev = load(k);
        map.put(k, normValue(value));
        return prev;
    }

    public synchronized boolean compareAndSwap(Object key, Object old, Object newValue) {
        Object k = normKey(key);
        if (!map.containsKey(k)) {
            return false;
        }
        if (!GoEquality.equal(map.get(k), normValue(old))) {
            return false;
        }
        map.put(k, normValue(newValue));
        return true;
    }

    public synchronized boolean compareAndDelete(Object key, Object old) {
        Object k = normKey(key);
        if (!map.containsKey(k)) {
            return false;
        }
        if (!GoEquality.equal(map.get(k), normValue(old))) {
            return false;
        }
        map.delete(k);
        return true;
    }

    public synchronized void clear() {
        map.clear();
    }

    /** Snapshot of the present keys, in table order (for Range). */
    public synchronized Object[] keysSnapshot() {
        return map.keysSnapshot();
    }

    public synchronized boolean containsKey(Object key) {
        return map.containsKey(normKey(key));
    }

    public synchronized Object get(Object key) {
        return map.get(normKey(key));
    }
}
