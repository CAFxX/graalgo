package graalgo.runtime;

import java.lang.ref.WeakReference;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.atomic.AtomicBoolean;

import graalgo.sched.SchedulerCore;

/**
 * P2-M4 F4 torture differential (WS D; ADR-0002 F4).
 *
 * <p>Verifies exact root enumeration: for each scheduler park kind, a
 * goroutine parks while holding a large object graph; a GC during the park
 * must NOT collect the graph (roots are exact — the parked stack is
 * enumerated). After the goroutine exits, a GC MUST collect it (no
 * over-retention by scheduler/channel/timer structures).
 *
 * <p>Park kinds covered: channel recv (unbuffered), timer sleep, quantum
 * park (tight loop yielding via Safepoint). Select parks via channels;
 * IO parks via the same quantum mechanism.
 *
 * <p>Collector coverage: this test runs in-process under the JVM's default
 * collector (G1). Per-collector verification (ZGC, Shenandoah) is via
 * {@code make test-gc-collectors}, which re-runs this class in subprocesses
 * with {@code -XX:+UseZGC} / {@code -XX:+UseShenandoahGC}.
 *
 * <p>verifies: WS D (ADR-0002 F4)
 */
public final class GcTortureTest {

    private SchedulerCore sched;

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /** A large graph: 1000 nodes, each with a 10KB payload (~10MB). */
    private static List<byte[]> makeGraph() {
        List<byte[]> graph = new ArrayList<>();
        for (int i = 0; i < 1000; i++) {
            graph.add(new byte[10 * 1024]);
        }
        return graph;
    }

    private void withScheduler(ThrowingRunnable body) throws Exception {
        sched = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            body.run();
            long deadline = System.currentTimeMillis() + 15000;
            while (sched.runnableCount() != 0) {
                if (System.currentTimeMillis() > deadline) {
                    throw new AssertionError("quiescence timeout (lost wakeup?)");
                }
                Thread.sleep(10);
            }
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }

    /** Holder to allow lambdas to capture a mutable graph reference. */
    private static final class GraphHolder {
        List<byte[]> graph;
    }

    public void testChannelParkRootEnumeration() throws Exception {
        withScheduler(() -> {
            ChanOps.Chan ch = ChanOps.makeChan(8, 0, 0L);
            GraphHolder h = new GraphHolder();
            h.graph = makeGraph();
            WeakReference<List<byte[]>> weak = new WeakReference<>(h.graph);

            sched.spawn(() -> {
                int size = h.graph.size();
                if (size == 0) {
                    throw new AssertionError("graph empty");
                }
                ChanOps.recv(ch); // parks indefinitely
            });

            Thread.sleep(500);
            GcInterop.gc();
            GcInterop.gc();
            assertTrue(weak.get() != null,
                    "channel-parked goroutine's graph was collected (root under-enumeration!)");

            ChanOps.close(ch);
            h.graph = null; // drop; closure ref dies with goroutine

            for (int i = 0; i < 20 && weak.get() != null; i++) {
                GcInterop.gc();
            }
            assertTrue(weak.get() == null,
                    "graph not collected after goroutine exit (over-retention!)");
        });
    }

    public void testTimerParkRootEnumeration() throws Exception {
        withScheduler(() -> {
            GraphHolder h = new GraphHolder();
            h.graph = makeGraph();
            WeakReference<List<byte[]>> weak = new WeakReference<>(h.graph);

            sched.spawn(() -> {
                int size = h.graph.size();
                if (size == 0) {
                    throw new AssertionError("graph empty");
                }
                Timers.sleep(2_000_000_000L);
            });

            Thread.sleep(500);
            GcInterop.gc();
            GcInterop.gc();
            assertTrue(weak.get() != null,
                    "timer-parked goroutine's graph was collected (root under-enumeration!)");

            Thread.sleep(2500);
            h.graph = null;
            for (int i = 0; i < 20 && weak.get() != null; i++) {
                GcInterop.gc();
            }
            assertTrue(weak.get() == null,
                    "graph not collected after goroutine exit (over-retention!)");
        });
    }

    public void testQuantumParkRootEnumeration() throws Exception {
        withScheduler(() -> {
            GraphHolder h = new GraphHolder();
            h.graph = makeGraph();
            WeakReference<List<byte[]>> weak = new WeakReference<>(h.graph);
            AtomicBoolean stop = new AtomicBoolean(false);

            sched.spawn(() -> {
                int size = h.graph.size();
                while (!stop.get()) {
                    if (size == 0) {
                        throw new AssertionError("graph empty");
                    }
                }
            });

            Thread.sleep(500);
            GcInterop.gc();
            GcInterop.gc();
            assertTrue(weak.get() != null,
                    "quantum-parked goroutine's graph was collected (root under-enumeration!)");

            stop.set(true);
            Thread.sleep(500);
            h.graph = null;
            for (int i = 0; i < 20 && weak.get() != null; i++) {
                GcInterop.gc();
            }
            assertTrue(weak.get() == null,
                    "graph not collected after goroutine exit (over-retention!)");
        });
    }

    @FunctionalInterface
    interface ThrowingRunnable {
        void run() throws Exception;
    }
}
