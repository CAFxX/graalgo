package graalgo.p3unsafe;

import graalgo.p3reflect.ReflectRefs;
import graalgo.p3reflect.ReflectValues;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import graalgo.values.UnsafeHandles;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * Falsification tests for RV-P1-23 (split-domain counted handles).
 *
 * <p>Before the fix, every {@code reflect.Value} handle was interned
 * strongly and never released ({@code UnsafeTable.free} had zero
 * production callers): {@code liveCount()} grew without bound within a
 * run. After the fix, counted-domain entries are removed when their last
 * owning Value dies (Cleaner-driven), while the unsafe domain keeps its
 * strong gc-parity anchor.
 *
 * <p>Repo convention: public {@code test*} methods, no JUnit; registered
 * in {@code graalgo.iface.IfaceTestRunner}.
 */
public final class UnsafeTableLeakTest {

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what);
        }
    }

    /** A minimal reflect.Value-shaped descriptor ({typ, ptr, flag}). */
    private static GoStructDescriptor valueDescriptor() {
        GoStructDescriptor d = GoStructDescriptor.layout(
                "reflect.Value",
                List.of("typ", "ptr", "flag"),
                List.of(GoKind.UNSAFE_POINTER, GoKind.UNSAFE_POINTER, GoKind.UINT64));
        d.setCopyHook(ReflectRefs.valueCopyHook());
        return d;
    }

    /** Allocates garbage and suggests collection (Cleaner latency). */
    private static void gcPressure() {
        for (int i = 0; i < 4; i++) {
            byte[] junk = new byte[8 * 1024 * 1024];
            for (int j = 0; j < junk.length; j += 4096) {
                junk[j] = (byte) j;
            }
        }
        System.gc();
    }

    /** Polls until liveCounted() reaches want (Cleaner latency; 15s budget). */
    private static void awaitCounted(UnsafeTable t, int want, String what) {
        long deadline = System.currentTimeMillis() + 15000;
        for (;;) {
            int got = t.liveCounted();
            if (got == want) {
                return;
            }
            if (System.currentTimeMillis() >= deadline) {
                throw new AssertionError(
                        what + ": liveCounted()=" + got + ", want " + want);
            }
            gcPressure();
            try {
                Thread.sleep(50);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new AssertionError(what + ": interrupted");
            }
        }
    }

    /**
     * T1 — leak falsifier. 50k counted Values are created (held, so no
     * release can fire mid-loop — a GC during the loop must not move the
     * count), then all dropped; the counted domain must quiesce to zero.
     * Pre-fix (old intern+pack API) this stays stuck at 50000 — the
     * demonstrated leak.
     */
    public void testCountedHandlesAreReleased() {
        UnsafeTable.install();
        UnsafeTable t = UnsafeTable.current();
        GoStructDescriptor desc = valueDescriptor();
        GoStruct[] held = new GoStruct[50000];
        for (int i = 0; i < held.length; i++) {
            // Held: every Value is a live owner, so the count is exact.
            held[i] = ReflectRefs.packCounted(desc, 1, new ReflectRefs.Cell(i), 0);
        }
        assertTrue(t.liveCounted() == 50000,
                "T1: expected 50000 live counted handles, got " + t.liveCounted());
        held = null; // drop all owners at once
        awaitCounted(t, 0, "T1");
    }

    /** T2 — unsafe parity: the uncounted domain keeps its strong anchor. */
    public void testUncountedHandlesStayStrong() {
        UnsafeTable.install();
        UnsafeTable t = UnsafeTable.current();
        Object obj = new Object();
        long h = t.intern(obj);
        gcPressure();
        assertTrue(t.resolve(h) == obj, "T2: strong handle must resolve after GC");
        // Even with the local dropped, the table anchors the referent.
        obj = null;
        gcPressure();
        assertTrue(t.resolve(h) != null, "T2: uncounted entry must stay pinned");
        assertTrue(t.liveCounted() == 0, "T2: no counted entries expected");
    }

    /** T3 — fail-loud counted: resolve after the last owner's death throws. */
    public void testCountedReleaseIsFailLoud() {
        UnsafeTable.install();
        UnsafeTable t = UnsafeTable.current();
        GoStructDescriptor desc = valueDescriptor();
        GoStruct v = ReflectRefs.packCounted(desc, 1, new ReflectRefs.Cell(42), 0);
        long h = ReflectValues.parts(v).ptr();
        v = null;
        awaitCounted(t, 0, "T3");
        try {
            t.resolve(h);
            throw new AssertionError("T3: resolve after release must throw");
        } catch (UnsafeHandles.UnsafeEscapeException expected) {
            // specified post-release semantics
        }
    }

    /**
     * T4 — transitive base: dropping the parent Value while a child
     * (FieldRef) Value lives must keep the whole base chain resolvable.
     */
    public void testChildValueKeepsBaseChainAlive() {
        UnsafeTable.install();
        UnsafeTable t = UnsafeTable.current();
        GoStructDescriptor desc = valueDescriptor();
        GoStruct parent = ReflectRefs.packCounted(desc, 1, new ReflectRefs.Cell("base"), 0);
        long parentH = ReflectValues.parts(parent).ptr();
        GoStruct child = ReflectRefs.packCounted(
                desc, 1, new ReflectRefs.FieldRef(parentH, 0), 0);
        long childH = ReflectValues.parts(child).ptr();
        parent = null;
        gcPressure();
        // The child owns the parent handle transitively: both entries live.
        assertTrue(t.liveCounted() == 2,
                "T4: child must keep base chain alive, got " + t.liveCounted());
        assertTrue(t.resolve(childH) instanceof ReflectRefs.FieldRef, "T4: child resolves");
        assertTrue(t.resolve(parentH) != null, "T4: base resolves through child ownership");
        child = null;
        awaitCounted(t, 0, "T4");
    }

    /**
     * T5 — NewAt: a Value packed around an unsafe-domain handle takes no
     * counted ownership; the unsafe entry outlives the Value.
     */
    public void testSharedHandleSurvivesValueDeath() {
        UnsafeTable.install();
        UnsafeTable t = UnsafeTable.current();
        GoStructDescriptor desc = valueDescriptor();
        Object obj = new Object();
        long h = t.intern(obj);
        GoStruct v = ReflectRefs.packShared(desc, 1, h, 0);
        v = null;
        obj = null;
        gcPressure();
        assertTrue(t.resolve(h) != null, "T5: shared unsafe handle must survive");
        assertTrue(t.liveCounted() == 0, "T5: no counted entries expected");
    }

    /**
     * T6 — shared handle: packCountedExisting registers an additional
     * owner; the entry dies only when the last owner goes.
     */
    public void testExistingHandleOwnership() {
        UnsafeTable.install();
        UnsafeTable t = UnsafeTable.current();
        GoStructDescriptor desc = valueDescriptor();
        GoStruct v1 = ReflectRefs.packCounted(desc, 1, new ReflectRefs.Cell(7), 0);
        long h = ReflectValues.parts(v1).ptr();
        GoStruct v2 = ReflectRefs.packCountedExisting(desc, 1, h, 0);
        v1 = null;
        gcPressure();
        assertTrue(t.resolve(h) != null, "T6: one owner left, must resolve");
        assertTrue(t.liveCounted() == 1, "T6: expected 1 live counted handle");
        v2 = null;
        awaitCounted(t, 0, "T6");
        try {
            t.resolve(h);
            throw new AssertionError("T6: resolve after all owners released must throw");
        } catch (UnsafeHandles.UnsafeEscapeException expected) {
            // specified post-release semantics
        }
    }

    /**
     * T7 — stress: 8 threads × 5k packCounted/resolve/drop with a
     * concurrent GC-churn thread; no spurious throws, no deadlock, and
     * quiescence to zero counted handles.
     */
    public void testConcurrentPackResolveDrop() throws Exception {
        UnsafeTable.install();
        final UnsafeTable t = UnsafeTable.current();
        final GoStructDescriptor desc = valueDescriptor();
        final int threads = 8;
        final int perThread = 5000;
        final AtomicInteger errors = new AtomicInteger();
        final CountDownLatch start = new CountDownLatch(1);
        final CountDownLatch done = new CountDownLatch(threads);
        final AtomicBoolean stop = new AtomicBoolean();
        Thread gcThread = new Thread(() -> {
            while (!stop.get()) {
                byte[] junk = new byte[1024 * 1024];
                for (int i = 0; i < junk.length; i += 4096) {
                    junk[i] = 1;
                }
            }
        });
        gcThread.setDaemon(true);
        gcThread.start();
        for (int ti = 0; ti < threads; ti++) {
            new Thread(() -> {
                try {
                    start.await();
                    for (int i = 0; i < perThread; i++) {
                        Object referent = new Object();
                        GoStruct v = ReflectRefs.packCounted(desc, 1, referent, 0);
                        long h = ReflectValues.parts(v).ptr();
                        if (t.resolve(h) != referent) {
                            errors.incrementAndGet();
                        }
                        // v dropped here: one owner, released on collection.
                    }
                } catch (Throwable th) {
                    errors.incrementAndGet();
                } finally {
                    done.countDown();
                }
            }).start();
        }
        start.countDown();
        if (!done.await(120, TimeUnit.SECONDS)) {
            throw new AssertionError("T7: worker threads did not finish (deadlock?)");
        }
        stop.set(true);
        gcThread.join(5000);
        awaitCounted(t, 0, "T7");
        assertTrue(errors.get() == 0, "T7: errors=" + errors.get());
    }

    /** T8 — stability: re-interning a live referent yields the same handle. */
    public void testCountedInterningIsStable() {
        UnsafeTable.install();
        UnsafeTable t = UnsafeTable.current();
        GoStructDescriptor desc = valueDescriptor();
        Object x = new Object();
        GoStruct v1 = ReflectRefs.packCounted(desc, 1, x, 0);
        GoStruct v2 = ReflectRefs.packCounted(desc, 1, x, 0);
        long h1 = ReflectValues.parts(v1).ptr();
        long h2 = ReflectValues.parts(v2).ptr();
        assertTrue(h1 == h2, "T8: same referent must yield the same counted handle");
        GoStruct w1 = ReflectRefs.packCounted(desc, 1, new ReflectRefs.FieldRef(h1, 3), 0);
        GoStruct w2 = ReflectRefs.packCounted(desc, 1, new ReflectRefs.FieldRef(h1, 3), 0);
        long f1 = ReflectValues.parts(w1).ptr();
        long f2 = ReflectValues.parts(w2).ptr();
        assertTrue(f1 == f2, "T8: equal Ref keys must yield the same counted handle");
        assertTrue(t.liveCounted() == 2, "T8: expected 2 live counted handles");
    }

    /**
     * Copy hook: a deep-copied Value is a new counted owner — dropping the
     * original must not release the entry while the copy lives.
     */
    public void testCopiedValueIsNewOwner() {
        UnsafeTable.install();
        UnsafeTable t = UnsafeTable.current();
        GoStructDescriptor desc = valueDescriptor();
        GoStruct v1 = ReflectRefs.packCounted(desc, 1, new ReflectRefs.Cell(1), 0);
        long h = ReflectValues.parts(v1).ptr();
        GoStruct v2 = v1.deepCopy();
        assertTrue(ReflectValues.parts(v2).ptr() == h, "copy must carry the same handle");
        v1 = null;
        gcPressure();
        assertTrue(t.resolve(h) != null, "copy: entry must survive the original's death");
        assertTrue(t.liveCounted() == 1, "copy: expected 1 live counted handle");
        v2 = null;
        awaitCounted(t, 0, "copy");
    }

    /**
     * Domain violation is fail-loud: addCountedOwner on an uncounted
     * handle throws.
     */
    public void testCountedOwnerOnUncountedHandleThrows() {
        UnsafeTable.install();
        UnsafeTable t = UnsafeTable.current();
        long h = t.intern(new Object());
        try {
            t.addCountedOwner(h);
            throw new AssertionError("addCountedOwner on uncounted handle must throw");
        } catch (UnsafeHandles.UnsafeEscapeException expected) {
            // fail loud on domain violation
        }
    }
}
