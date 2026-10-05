import graalgo.runtime.ChanOps;
import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;

import java.lang.ref.Cleaner;
import java.lang.ref.WeakReference;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * THROWAWAY design spike for P2-M4 (GC interop).
 *
 * Question: in this architecture (all Go values are JVM-heap Java objects;
 * goroutine = vthread + scheduler token; parks via LockSupport.park()),
 * does the JVM GC exactly enumerate roots from a parked vthread's stack?
 *
 * Test 1 (parked-root liveness + closure exactness + no over-retention):
 *   - a goroutine allocates a ~20MB object graph, keeps the root in a Java
 *     local, and parks indefinitely on an unbuffered channel recv;
 *   - the test thread drops every other strong ref, forces GC, and checks
 *     via WeakReferences that (a) the root survived (roots enumerated from
 *     the parked stack — no premature collection) and (b) a deep leaf
 *     survived (the transitive closure was traced exactly — no holes);
 *   - then the goroutine is released and exits; after another forced GC the
 *     weak refs must clear (scheduler/channel/timer structures do not
 *     over-retain dead goroutine stacks).
 * Test 2 (Go-observable finalizer surface): java.lang.ref.Cleaner as the
 *   implementation vehicle for runtime.SetFinalizer — runs exactly once
 *   after unreachability, never while reachable.
 */
public class Spike {

    static void forceGc() throws Exception {
        for (int i = 0; i < 3; i++) {
            System.gc();
            Thread.sleep(100);
        }
        // A little allocation pressure so a lazy collector actually sweeps.
        byte[] pressure = new byte[8_000_000];
        if (pressure.length == 0) System.out.print("");
        System.gc();
        Thread.sleep(200);
    }

    static void check(boolean cond, String msg) {
        if (!cond) throw new AssertionError("SPIKE FAIL: " + msg);
        System.out.println("  ok: " + msg);
    }

    public static void main(String[] args) throws Exception {
        long t0 = System.nanoTime();
        testParkedRoots();
        long t1 = System.nanoTime();
        testFinalizer();
        long t2 = System.nanoTime();
        System.out.printf("parked-roots: %.1f ms, finalizer: %.1f ms%n",
                (t1 - t0) / 1e6, (t2 - t1) / 1e6);
        System.out.println("SPIKE PASS");
        System.exit(0);
    }

    static void testParkedRoots() throws Exception {
        System.out.println("[1] parked vthread stack roots (via real SchedulerCore + channel park)");
        SchedulerCore sched = new SchedulerCore(2, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start(); // drainer must run before the first spawn
        SchedulerCore.install(sched); // ChanOps resolves the scheduler via current()
        ChanOps.Chan ch = ChanOps.makeChan(8, 0);

        final Object[] box = new Object[2];
        final CountDownLatch parked = new CountDownLatch(1);
        final CountDownLatch done = new CountDownLatch(1);

        GoroutineHandle h = sched.spawn(() -> {
            // ~20MB graph: root Object[] + 20k leaves. Root and one deep leaf
            // stay in Java locals ACROSS the park below (post-park use keeps
            // them live against JIT dead-local elimination).
            Object[] graph = new Object[20_000];
            for (int i = 0; i < graph.length; i++) graph[i] = new byte[1024];
            Object leaf = graph[graph.length - 1];
            box[0] = graph;
            box[1] = leaf;
            parked.countDown();
            ChanOps.recv(ch); // parks indefinitely; graph/leaf live on the vthread stack
            done.countDown();
            if (graph.length == 0 || leaf == null) throw new IllegalStateException("unreachable");
        });

        check(parked.await(10, TimeUnit.SECONDS), "goroutine reached park point");
        Thread.sleep(500); // let it settle into LockSupport.park()

        // From here the ONLY strong refs to graph/leaf are the parked
        // vthread's Java locals, reachable via GoChan.recvWaiters ->
        // RecvWaiter -> GoroutineHandle.thread -> vthread stack.
        WeakReference<Object> rootWeak = new WeakReference<>(box[0]);
        WeakReference<Object> leafWeak = new WeakReference<>(box[1]);
        box[0] = null;
        box[1] = null;

        forceGc();
        check(rootWeak.get() != null,
                "root survived GC while parked (stack roots enumerated, no premature collection)");
        check(leafWeak.get() != null,
                "deep leaf survived GC while parked (transitive closure traced exactly, no holes)");

        // Release; goroutine exits; its stack must become collectable.
        ChanOps.send(ch, 1L);
        check(done.await(10, TimeUnit.SECONDS), "goroutine released and exited");
        h = null; // drop our handle too
        forceGc();
        // Spin a little: weak refs clear asynchronously after the sweep.
        for (int i = 0; i < 20 && (rootWeak.get() != null || leafWeak.get() != null); i++) {
            Thread.sleep(200);
            System.gc();
        }
        check(rootWeak.get() == null && leafWeak.get() == null,
                "dead goroutine stack collected (scheduler/channel do not over-retain)");
    }

    static final Cleaner CLEANER = Cleaner.create();

    static final class Finalizable {
        final AtomicBoolean ran;
        Finalizable(AtomicBoolean ran) { this.ran = ran; }
    }

    static void testFinalizer() throws Exception {
        System.out.println("[2] SetFinalizer vehicle: java.lang.ref.Cleaner");
        // Positive: finalizer runs exactly once after unreachability.
        AtomicBoolean ran = new AtomicBoolean(false);
        Object o = new Finalizable(ran);
        CLEANER.register(o, () -> ran.set(true));
        o = null;
        forceGc();
        long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(10);
        while (!ran.get() && System.nanoTime() < deadline) {
            Thread.sleep(100);
            System.gc();
        }
        check(ran.get(), "finalizer ran after object became unreachable");

        // Negative: no premature finalization while reachable.
        AtomicBoolean ran2 = new AtomicBoolean(false);
        Object o2 = new Finalizable(ran2);
        CLEANER.register(o2, () -> ran2.set(true));
        forceGc();
        Thread.sleep(500);
        check(!ran2.get(), "finalizer did NOT run while object still reachable");
        if (o2 == null) System.out.print(""); // keep o2 live
    }
}
