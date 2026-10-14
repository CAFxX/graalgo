package graalgo.iface;

import graalgo.runtime.sync.GoSyncMap;
import graalgo.values.GoInterface;

/**
 * Unit tests for {@link GoSyncMap} (fix-linkname, Caddy/godebug lane):
 * the Go-semantics sync.Map backing store.
 */
public final class SyncMapTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(Object expected, Object actual, String msg) {
        if (expected == null ? actual != null : !expected.equals(actual)) {
            throw new AssertionError(msg + ": expected " + expected + ", got " + actual);
        }
    }

    public void testLoadStore() {
        GoSyncMap m = new GoSyncMap();
        GoSyncMap.LoadResult r = m.load("k");
        assertTrue(!r.ok(), "load of missing key");
        assertTrue(r.value() == null, "missing value is null");
        m.store("k", "v");
        r = m.load("k");
        assertTrue(r.ok(), "load of present key");
        assertEquals("v", r.value(), "stored value");
    }

    public void testLoadOrStore() {
        GoSyncMap m = new GoSyncMap();
        GoSyncMap.LoadResult r = m.loadOrStore("k", "v1");
        assertTrue(!r.ok(), "first loadOrStore not loaded");
        assertEquals("v1", r.value(), "first loadOrStore returns stored value");
        r = m.loadOrStore("k", "v2");
        assertTrue(r.ok(), "second loadOrStore loaded");
        assertEquals("v1", r.value(), "second loadOrStore returns existing");
    }

    public void testSwap() {
        GoSyncMap m = new GoSyncMap();
        GoSyncMap.LoadResult r = m.swap("k", "v1");
        assertTrue(!r.ok(), "swap of missing key");
        assertTrue(r.value() == null, "swap missing value null");
        r = m.swap("k", "v2");
        assertTrue(r.ok(), "swap of present key");
        assertEquals("v1", r.value(), "swap returns previous");
        assertEquals("v2", m.load("k").value(), "swap stores new");
    }

    public void testCompareAndSwap() {
        GoSyncMap m = new GoSyncMap();
        assertTrue(!m.compareAndSwap("k", "a", "b"), "CAS on missing key");
        m.store("k", "a");
        assertTrue(!m.compareAndSwap("k", "x", "b"), "CAS on wrong old");
        assertEquals("a", m.load("k").value(), "failed CAS keeps old");
        assertTrue(m.compareAndSwap("k", "a", "b"), "CAS on matching old");
        assertEquals("b", m.load("k").value(), "successful CAS stores new");
    }

    public void testCompareAndDelete() {
        GoSyncMap m = new GoSyncMap();
        assertTrue(!m.compareAndDelete("k", "a"), "CAD on missing key");
        m.store("k", "a");
        assertTrue(!m.compareAndDelete("k", "x"), "CAD on wrong old");
        assertTrue(m.load("k").ok(), "failed CAD keeps entry");
        assertTrue(m.compareAndDelete("k", "a"), "CAD on matching old");
        assertTrue(!m.load("k").ok(), "successful CAD removes entry");
    }

    public void testDeleteAndClear() {
        GoSyncMap m = new GoSyncMap();
        m.store("a", 1);
        m.store("b", 2);
        m.delete("a");
        assertTrue(!m.load("a").ok(), "deleted key gone");
        assertTrue(m.load("b").ok(), "other key kept");
        m.clear();
        assertTrue(!m.load("b").ok(), "clear removes all");
    }

    public void testNilKeyAndNilValue() {
        GoSyncMap m = new GoSyncMap();
        // A nil interface key is legal in gc's sync.Map.
        m.store(null, "v");
        assertTrue(m.load(null).ok(), "nil key stored");
        assertEquals("v", m.load(null).value(), "nil key value");
        // A nil value normalizes to the nil interface (never Java null).
        m.store("k", null);
        GoSyncMap.LoadResult r = m.load("k");
        assertTrue(r.ok(), "nil value stored");
        assertTrue(r.value() == GoInterface.NIL, "nil value is GoInterface.NIL");
    }

    public void testLoadAndDelete() {
        GoSyncMap m = new GoSyncMap();
        GoSyncMap.LoadResult r = m.loadAndDelete("k");
        assertTrue(!r.ok(), "loadAndDelete missing");
        m.store("k", "v");
        r = m.loadAndDelete("k");
        assertTrue(r.ok(), "loadAndDelete present");
        assertEquals("v", r.value(), "loadAndDelete returns value");
        assertTrue(!m.load("k").ok(), "loadAndDelete removes");
    }
}
