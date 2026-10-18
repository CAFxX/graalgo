package graalgo.runtime;

import java.lang.ref.WeakReference;
import java.lang.reflect.Field;
import java.util.Arrays;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;

import graalgo.frontend.GoArrayCell;
import graalgo.frontend.GoFieldCell;
import graalgo.values.GoBox;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * P2-M4 unit tests for {@link GcInterop} (WS D; ADR-0002 D4).
 *
 * <p>verifies: WS D (GcInterop unit behavior)
 */
public final class GcInteropTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(Object expected, Object actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /** weak.Pointer vehicle: referent visible while live, null after collection. */
    public void testWeakRefLifecycle() throws Exception {
        Object target = new Object();
        WeakReference<Object> ref = GcInterop.newWeakRef(target);
        assertTrue(ref != null, "newWeakRef of non-null must be non-null");
        assertTrue(GcInterop.weakRefValue(ref) == target, "weak value must be the referent");
        assertTrue(GcInterop.newWeakRef(null) == null, "newWeakRef(nil) must be nil");
        assertTrue(GcInterop.weakRefValue(null) == null, "weak value of nil ref must be nil");

        // Drop the strong ref; force collection; the weak must clear.
        WeakReference<Object> ref2 = GcInterop.newWeakRef(new Object());
        for (int i = 0; i < 50 && GcInterop.weakRefValue(ref2) != null; i++) {
            GcInterop.gc();
            @SuppressWarnings("unused")
            byte[] pressure = new byte[1 << 20];
        }
        assertTrue(GcInterop.weakRefValue(ref2) == null,
                "weak reference must clear after its referent is collected");
    }

    /** memStats: mapped fields present with plausible values. */
    public void testMemStatsFields() {
        Map<String, Long> m = GcInterop.memStats();
        for (String f : new String[] {
                "HeapAlloc", "HeapSys", "Sys", "NumGC", "PauseTotalNs",
                "NumForcedGC", "PauseNsLast" }) {
            assertTrue(m.containsKey(f), "memStats must contain " + f);
            assertTrue(m.get(f) >= 0, "memStats." + f + " must be non-negative");
        }
        assertTrue(m.get("HeapAlloc") <= m.get("HeapSys"),
                "HeapAlloc (used) must not exceed HeapSys (committed)");
        assertTrue(m.get("HeapSys") <= m.get("Sys"),
                "HeapSys must not exceed Sys (heap+non-heap committed)");
    }

    /** memStats: NumForcedGC tracks explicit gc() calls. */
    public void testMemStatsForcedGcCount() {
        long before = GcInterop.memStats().get("NumForcedGC");
        GcInterop.gc();
        long after = GcInterop.memStats().get("NumForcedGC");
        assertEquals(before + 1, after, "NumForcedGC must increment per gc() call");
    }

    /** keepAlive: callable, no-op observably (must not throw). */
    public void testKeepAliveNoop() {
        Object x = new Object();
        GcInterop.keepAlive(x);
        GcInterop.keepAlive(null); // gc allows nil
    }

    /** setFinalizer: argument validation matches gc's panics. */
    public void testSetFinalizerValidation() {
        boolean threw = false;
        try {
            GcInterop.setFinalizer(null, null);
        } catch (IllegalArgumentException e) {
            threw = true;
        }
        assertTrue(threw, "SetFinalizer(nil, nil) must throw");

        threw = false;
        try {
            GcInterop.setFinalizer("not-a-pointer", null);
        } catch (IllegalArgumentException e) {
            threw = true;
        }
        assertTrue(threw, "SetFinalizer(non-pointer, nil) must throw");

        threw = false;
        try {
            GoPointer p = new GoPointer(new Object());
            GcInterop.setFinalizer(p, "not-a-func");
        } catch (IllegalArgumentException e) {
            threw = true;
        }
        assertTrue(threw, "SetFinalizer(ptr, non-func) must throw");
    }

    /** setFinalizer(ptr, nil) on an object with no finalizer is a silent no-op. */
    public void testSetFinalizerRemoveAbsent() {
        GoPointer p = new GoPointer(new Object());
        GcInterop.setFinalizer(p, null); // must not throw
    }

    /** gc(): returns promptly (does not hang) on the normal path. */
    public void testGcReturnsPromptly() throws Exception {
        // Note: this exercises the fast path where MXBean counts advance.
        // The 30s-timeout path (explicit GC disabled via -XX:+DisableExplicitGC)
        // cannot be tested in-process; it is covered by code inspection
        // (bounded poll loop with deadline).
        long start = System.nanoTime();
        GcInterop.gc();
        long ms = (System.nanoTime() - start) / 1_000_000L;
        assertTrue(ms < 60_000L, "gc() must return within the documented bound, took " + ms + "ms");
    }

    // ------------------------------------------------------------------
    // R15-P1-3: double-set detection is keyed by storage identity, not by
    // the GoPointer wrapper object. Every &x evaluation mints a fresh
    // wrapper over the same storage; the registry must collide them.
    // ------------------------------------------------------------------

    /** A GoFunc whose body only counts down the latch (never touches the scheduler). */
    private static GoFunc countingFinalizer(CountDownLatch latch) {
        RootNode root = new RootNode(null) {
            @Override
            public Object execute(VirtualFrame frame) {
                latch.countDown();
                return null;
            }
        };
        RootCallTarget target = root.getCallTarget();
        return new GoFunc(target, null, null);
    }

    private static GoFunc quietFinalizer() {
        return countingFinalizer(new CountDownLatch(1));
    }

    private static void expectAlreadySet(Runnable r, String what) {
        try {
            r.run();
        } catch (IllegalStateException e) {
            assertTrue(e.getMessage() != null && e.getMessage().contains("already set"),
                    what + ": signal must say 'already set', got: " + e.getMessage());
            return;
        }
        throw new AssertionError(what + ": expected the double-set signal");
    }

    private static GoStructDescriptor twoPtrDesc() {
        return GoStructDescriptor.layout("T",
                List.of("a", "b"),
                List.of(GoKind.POINTER, GoKind.POINTER),
                List.of(-1, -1),
                Arrays.asList(null, null));
    }

    private static GoStruct newStruct(GoStructDescriptor d) {
        return new GoStruct(d, new long[0], new Object[2]);
    }

    /** R15-P1-3 case 1: two evaluations of &x over the same variable collide. */
    public void testSetFinalizerDoubleSetSameStorage() {
        GoBox box = new GoBox(null);
        GoFunc f = quietFinalizer();
        GoFunc g = quietFinalizer();
        try {
            GcInterop.setFinalizer(new GoPointer(box), f);
            expectAlreadySet(() -> GcInterop.setFinalizer(new GoPointer(box), g),
                    "SetFinalizer(&x, f) then SetFinalizer(&x, g)");
        } finally {
            GcInterop.setFinalizer(new GoPointer(box), null);
        }
    }

    /** R15-P1-3 case 2: same wrapper twice (regression guard of the old path). */
    public void testSetFinalizerDoubleSetSameWrapper() {
        GoPointer p = new GoPointer(new GoBox(null));
        GoFunc f = quietFinalizer();
        GoFunc g = quietFinalizer();
        try {
            GcInterop.setFinalizer(p, f);
            expectAlreadySet(() -> GcInterop.setFinalizer(p, g),
                    "SetFinalizer(p, f) then SetFinalizer(p, g)");
        } finally {
            GcInterop.setFinalizer(p, null);
        }
    }

    /**
     * R15-P1-3 case 3: two evaluations of &s.f allocate distinct GoFieldCell
     * objects over the same (struct, field) — they must still collide.
     */
    public void testSetFinalizerDoubleSetFieldCell() {
        GoStruct s = newStruct(twoPtrDesc());
        GoFunc f = quietFinalizer();
        GoFunc g = quietFinalizer();
        try {
            GcInterop.setFinalizer(new GoPointer(new GoFieldCell(s, 0, GoKind.POINTER)), f);
            expectAlreadySet(
                    () -> GcInterop.setFinalizer(
                            new GoPointer(new GoFieldCell(s, 0, GoKind.POINTER)), g),
                    "SetFinalizer(&s.f, f) then SetFinalizer(&s.f, g)");
        } finally {
            GcInterop.setFinalizer(new GoPointer(new GoFieldCell(s, 0, GoKind.POINTER)), null);
        }
    }

    /** R15-P1-3 case 4: two evaluations of &a[i] over the same element collide. */
    public void testSetFinalizerDoubleSetElem() {
        Object arr = new Object[4];
        GoFunc f = quietFinalizer();
        GoFunc g = quietFinalizer();
        try {
            GcInterop.setFinalizer(new GoPointer(arr, 2), f);
            expectAlreadySet(() -> GcInterop.setFinalizer(new GoPointer(arr, 2), g),
                    "SetFinalizer(&a[i], f) then SetFinalizer(&a[i], g)");
        } finally {
            GcInterop.setFinalizer(new GoPointer(arr, 2), null);
        }
    }

    /** R15-P1-3 case 5: p := &x, then a fresh &x wrapper, must collide (aliases). */
    public void testSetFinalizerDoubleSetAlias() {
        GoBox box = new GoBox(null);
        GoPointer p = new GoPointer(box);
        GoFunc f = quietFinalizer();
        GoFunc g = quietFinalizer();
        try {
            GcInterop.setFinalizer(p, f);
            expectAlreadySet(() -> GcInterop.setFinalizer(new GoPointer(box), g),
                    "SetFinalizer(p, f) then SetFinalizer(&x, g)");
        } finally {
            GcInterop.setFinalizer(p, null);
        }
    }

    /** R15-P1-3 case 6: two distinct new(T) allocations stay distinct. */
    public void testSetFinalizerDistinctAllocations() {
        GoBox b1 = new GoBox(null);
        GoBox b2 = new GoBox(null);
        GoFunc f = quietFinalizer();
        GoFunc g = quietFinalizer();
        GoFunc h = quietFinalizer();
        try {
            GcInterop.setFinalizer(new GoPointer(b1), f);
            GcInterop.setFinalizer(new GoPointer(b2), g); // must not throw
            // ...but each allocation still detects its own double-set.
            expectAlreadySet(() -> GcInterop.setFinalizer(new GoPointer(b1), h),
                    "second SetFinalizer on the first allocation");
        } finally {
            GcInterop.setFinalizer(new GoPointer(b1), null);
            GcInterop.setFinalizer(new GoPointer(b2), null);
        }
    }

    /** R15-P1-3 case 7: set, remove with nil, re-arm — all clean. */
    public void testSetFinalizerRemoveThenRearm() {
        GoBox box = new GoBox(null);
        GoFunc f = quietFinalizer();
        GoFunc g = quietFinalizer();
        GoFunc h = quietFinalizer();
        try {
            GcInterop.setFinalizer(new GoPointer(box), f);
            GcInterop.setFinalizer(new GoPointer(box), null);
            GcInterop.setFinalizer(new GoPointer(box), g); // must not throw
            // The re-armed registration detects double-set again.
            expectAlreadySet(() -> GcInterop.setFinalizer(new GoPointer(box), h),
                    "double-set after re-arm");
        } finally {
            GcInterop.setFinalizer(new GoPointer(box), null);
        }
    }

    /**
     * R15-P1-3 case 8: &s.f and &s.g are distinct storages — no signal.
     * Documents the deliberate finer-than-gc granularity (gc keys the
     * finalizer per containing object; graalgo keys per field — see
     * DIV-M4-12).
     */
    public void testSetFinalizerDifferentFields() {
        GoStruct s = newStruct(twoPtrDesc());
        GoFunc f = quietFinalizer();
        GoFunc g = quietFinalizer();
        GoFunc h = quietFinalizer();
        try {
            GcInterop.setFinalizer(new GoPointer(new GoFieldCell(s, 0, GoKind.POINTER)), f);
            // must not throw: distinct fields are distinct storages
            GcInterop.setFinalizer(new GoPointer(new GoFieldCell(s, 1, GoKind.POINTER)), g);
            // ...but the first field still detects its own double-set.
            expectAlreadySet(
                    () -> GcInterop.setFinalizer(
                            new GoPointer(new GoFieldCell(s, 0, GoKind.POINTER)), h),
                    "second SetFinalizer on &s.a");
        } finally {
            GcInterop.setFinalizer(new GoPointer(new GoFieldCell(s, 0, GoKind.POINTER)), null);
            GcInterop.setFinalizer(new GoPointer(new GoFieldCell(s, 1, GoKind.POINTER)), null);
        }
    }

    /** R15-P1-3 case 9: setting the same function value twice still signals. */
    public void testSetFinalizerSameFuncTwice() {
        GoBox box = new GoBox(null);
        GoFunc f = quietFinalizer();
        try {
            GcInterop.setFinalizer(new GoPointer(box), f);
            expectAlreadySet(() -> GcInterop.setFinalizer(new GoPointer(box), f),
                    "SetFinalizer(&x, f) then SetFinalizer(&x, f)");
        } finally {
            GcInterop.setFinalizer(new GoPointer(box), null);
        }
    }

    // -- reflection helpers: the design allows a test hook or reflection
    // into GcInterop for the no-leak check; reflection needs no production
    // change, so no production accessor was added.

    @SuppressWarnings("unchecked")
    private static Map<Object, Object> byObj() throws Exception {
        Field f = GcInterop.class.getDeclaredField("BY_OBJ");
        f.setAccessible(true);
        return (Map<Object, Object>) f.get(null);
    }

    private static Object finalizerLock() throws Exception {
        Field f = GcInterop.class.getDeclaredField("FINALIZER_LOCK");
        f.setAccessible(true);
        return f.get(null);
    }

    private static int byObjSize() throws Exception {
        Object lock = finalizerLock();
        synchronized (lock) {
            return byObj().size();
        }
    }

    /**
     * Registers a finalizer on a fresh box and drops every guest reference
     * (the wrapper dies with this method's frame).
     */
    private static void registerShortLived(GoFunc fn) {
        GcInterop.setFinalizer(new GoPointer(new GoBox(null)), fn);
    }

    /**
     * R15-P1-3 case 10: no interning leak. Registrations on short-lived
     * storages must not accumulate: once each finalizer has fired (which
     * drops its own registry entry), BY_OBJ is empty again.
     */
    public void testFinalizerRegistryNoLeak() throws Exception {
        int n = 8;
        CountDownLatch allFired = new CountDownLatch(n);
        GoFunc fn = countingFinalizer(allFired);
        for (int i = 0; i < n; i++) {
            registerShortLived(fn);
        }
        for (int i = 0; i < 10 && allFired.getCount() > 0; i++) {
            GcInterop.gc(); // releases PINNED; lets the wrappers die
        }
        assertTrue(allFired.await(30, TimeUnit.SECONDS),
                "all " + n + " finalizers must fire, remaining=" + allFired.getCount());
        assertEquals(0, byObjSize(), "BY_OBJ must be empty after every finalizer fired");
    }

    /**
     * Registers on a fresh box, keeps the box (storage) alive, drops the
     * wrapper. Returns the box.
     */
    private static GoBox registerAndDropWrapper(GoFunc fn) {
        GoBox box = new GoBox(null);
        GcInterop.setFinalizer(new GoPointer(box), fn);
        return box;
    }

    /**
     * R15-P1-3 case 11: post-fire re-arm. After the finalizer has run
     * (one-shot), setting a new finalizer on the same storage is legal
     * again — without the run() self-removal companion change this wrongly
     * reports "already set".
     */
    public void testSetFinalizerRearmAfterFire() throws Exception {
        CountDownLatch fired1 = new CountDownLatch(1);
        GoBox box = registerAndDropWrapper(countingFinalizer(fired1));
        GcInterop.gc();
        assertTrue(fired1.await(30, TimeUnit.SECONDS), "first finalizer must fire");

        // Re-arm on the same storage with a fresh wrapper: must be clean.
        CountDownLatch fired2 = new CountDownLatch(1);
        GoFunc g = countingFinalizer(fired2);
        GcInterop.setFinalizer(new GoPointer(box), g); // must not throw
        // The re-armed registration is live and functional: it fires too.
        GcInterop.gc();
        assertTrue(fired2.await(30, TimeUnit.SECONDS), "re-armed finalizer must fire");
        assertEquals(0, byObjSize(), "BY_OBJ must be empty after the re-armed finalizer fired");
    }

    /**
     * R15-P1-3 case 12: concurrent SetFinalizer on aliases of one storage
     * resolves to exactly one winner and N-1 double-set signals.
     */
    public void testSetFinalizerConcurrentAliases() throws Exception {
        final int n = 16;
        final GoBox box = new GoBox(null);
        final GoFunc fn = quietFinalizer();
        final AtomicInteger won = new AtomicInteger();
        final AtomicInteger alreadySet = new AtomicInteger();
        final CountDownLatch start = new CountDownLatch(1);
        final CountDownLatch done = new CountDownLatch(n);
        for (int i = 0; i < n; i++) {
            Thread t = new Thread(() -> {
                try {
                    start.await();
                    GcInterop.setFinalizer(new GoPointer(box), fn);
                    won.incrementAndGet();
                } catch (IllegalStateException e) {
                    alreadySet.incrementAndGet();
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                } finally {
                    done.countDown();
                }
            });
            t.setDaemon(true);
            t.start();
        }
        try {
            start.countDown();
            assertTrue(done.await(30, TimeUnit.SECONDS), "all racing threads must finish");
            assertEquals(1, won.get(), "exactly one registration must win");
            assertEquals(n - 1, alreadySet.get(), "every loser must see the double-set signal");
            assertEquals(1, byObjSize(), "exactly one registry entry afterwards");
        } finally {
            GcInterop.setFinalizer(new GoPointer(box), null);
        }
    }
}
