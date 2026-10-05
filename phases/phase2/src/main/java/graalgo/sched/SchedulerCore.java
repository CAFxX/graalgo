package graalgo.sched;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.locks.LockSupport;

import graalgo.runtime.GoroutineState;
import graalgo.runtime.GoCalls;
import graalgo.values.GoFunc;

/**
 * The user-space scheduler core (WS A, P2-M1; P2-D1…P2-D7).
 *
 * <p>Goroutine = virtual thread + {@link GoroutineHandle}. Loom's
 * ForkJoinPool is demoted to a dumb thread pool; scheduling discipline
 * lives here:
 * <ul>
 *   <li>Quantum-based preemption (P2-D5: 10&nbsp;ms default): the vthread
 *       checks its deadline in {@link #pollCurrentThread()} (called from
 *       the replacement {@code graalgo.safepoint.Safepoint.poll()} at
 *       function entries and loop back-edges). On expiry it parks
 *       <em>indefinitely</em> via {@link LockSupport#park()} (P2-D2) and
 *       enqueues a RESUME token; the drainer re-admits it.
 *   <li>Single platform-thread {@link Drainer} (P2-D3): three lanes
 *       (wake &gt; init &gt; run per GG-WAKE-0003), blocking take,
 *       non-terminating, hard in-flight cap assert.
 *   <li>In-flight cap = {@code min(GOMAXPROCS, carrier parallelism)}
 *       (P2-D6, GG-SCHED-0003): at most {@code cap} vthreads are
 *       admitted-but-not-parked at any time; the drainer hard-asserts.
 *   <li>Armed-ticket discipline (P2-D7, GG-WAKE-0001): every park
 *       reserves a generation via {@link #armPark} BEFORE becoming
 *       visible to wakers; every admission does ack-before-unpark.
 * </ul>
 *
 * <p>Threading: vthreads only offer to the drainer's inbox and park;
 * all lane/inflight mutation happens on the drainer thread. The static
 * {@link #install}/{@link #current} holds the launcher-installed
 * scheduler; unit tests use instances directly.
 */
public final class SchedulerCore {
    /** Default quantum: 10 ms (P2-D5, measured in spike 2b). */
    public static final long DEFAULT_QUANTUM_NANOS = 10_000_000L;

    private static volatile SchedulerCore installed;

    /**
     * Installs the launcher-wide scheduler. Returns the previous one.
     */
    public static SchedulerCore install(SchedulerCore next) {
        SchedulerCore prev = installed;
        installed = next;
        return prev;
    }

    /**
     * The installed scheduler; fail-fast when none (a loud error beats
     * silently running unscheduled).
     */
    public static SchedulerCore current() {
        SchedulerCore sc = installed;
        if (sc == null) {
            throw new IllegalStateException(
                    "graalgo.sched: no SchedulerCore installed (SchedMain installs one; "
                    + "plain Phase-1 runs must not reach scheduler entry points)");
        }
        return sc;
    }

    /**
     * Static entry for the replacement {@code Safepoint.poll()}: no-op
     * when no scheduler is installed (Phase-1 parity for scheduler-less
     * runs), otherwise the quantum check for the current vthread.
     */
    public static void pollCurrentThreadStatic() {
        SchedulerCore sc = installed;
        if (sc == null) {
            return;
        }
        sc.pollCurrentThread();
    }

    private final int cap;
    private final int requestedCap;
    private final int carrierParallelism;
    private final long quantumNanos;
    private final Drainer drainer;
    private final ThreadLocal<GoroutineHandle> currentHandle = new ThreadLocal<>();
    private final AtomicInteger liveGoroutines = new AtomicInteger();
    private final CountDownLatch quiescenceLatch = new CountDownLatch(1);

    /**
     * @param gomaxprocs requested GOMAXPROCS (clamped to &ge;1)
     * @param quantumNanos quantum per admission, nanoseconds
     */
    public SchedulerCore(int gomaxprocs, long quantumNanos) {
        this.requestedCap = Math.max(1, gomaxprocs);
        this.carrierParallelism = Math.max(1, Runtime.getRuntime().availableProcessors());
        // P2-D6: effective cap = min(GOMAXPROCS, carrier parallelism).
        this.cap = Math.min(requestedCap, carrierParallelism);
        this.quantumNanos = quantumNanos;
        this.drainer = new Drainer(this);
    }

    /** Effective in-flight cap. */
    public int cap() {
        return cap;
    }

    public int requestedCap() {
        return requestedCap;
    }

    public int carrierParallelism() {
        return carrierParallelism;
    }

    /** Starts the drainer. Must precede the first spawn. */
    public void start() {
        drainer.start();
    }

    Drainer drainer() {
        return drainer;
    }

    // ================= vthread side =================

    /**
     * Reserves a park generation. Must be called BEFORE the waiter
     * becomes visible to any waker (P2-D7).
     */
    long armPark(GoroutineHandle h) {
        long t = h.parkSeq.getAndIncrement();
        h.armed.set(t);
        return t;
    }

    /**
     * Quantum-expiry park (GG-SCHED-0001): indefinite park + RESUME
     * token; the drainer re-admits through the run lane.
     */
    void quantumPark(GoroutineHandle h) {
        long t = armPark(h);
        drainer.offer(new ResumeToken(ResumeToken.Kind.RESUME, h, t));
        awaitAck(h, t);
        h.quantumDeadlineNanos = System.nanoTime() + quantumNanos;
    }

    /**
     * Voluntary yield: same mechanism as quantum expiry (P2-D2).
     */
    public void yieldCurrent() {
        GoroutineHandle h = requireHandle();
        quantumPark(h);
    }

    /**
     * Park the current goroutine indefinitely; {@code onWake} runs on
     * the waker thread before the WAKE token is enqueued. Wake via
     * {@link #unpark(GoroutineHandle)}. Generation matching is done by
     * the drainer, tolerating both orders (wakeup-before-park and
     * park-before-wakeup) — no wakeup is lost.
     */
    public void parkCurrent(Runnable onWake) {
        GoroutineHandle h = requireHandle();
        long t = armPark(h);
        h.parkWakeAction = onWake;
        drainer.offer(new ResumeToken(ResumeToken.Kind.PARKED, h, t));
        awaitAck(h, t);
    }

    /**
     * Wake a parked goroutine: runs its wake action, then enqueues a
     * WAKE token (priority lane). Safe to call before the target parks
     * (the drainer matches by handle).
     */
    public void unpark(GoroutineHandle h) {
        Runnable action = h.parkWakeAction;
        if (action != null) {
            h.parkWakeAction = null;
            action.run();
        }
        drainer.offer(new ResumeToken(ResumeToken.Kind.UNPARK, h, -1));
    }

    /**
     * Quantum check for the calling thread. No handle (not a scheduler
     * vthread) &rarr; no-op, so scheduler-less Phase-1 runs are
     * unaffected even with the replacement Safepoint on the classpath.
     */
    public void pollCurrentThread() {
        GoroutineHandle h = currentHandle.get();
        if (h == null) {
            return;
        }
        if (System.nanoTime() >= h.quantumDeadlineNanos) {
            quantumPark(h);
        }
    }

    private void awaitAck(GoroutineHandle h, long t) {
        // Early unpark is absorbed by the LockSupport permit; spurious
        // wakeups re-park on the acked < t condition. No wakeup is lost
        // because the drainer sets acked BEFORE unparking (GG-WAKE-0001).
        while (h.acked.get() < t) {
            LockSupport.park();
        }
    }

    private GoroutineHandle requireHandle() {
        GoroutineHandle h = currentHandle.get();
        if (h == null) {
            throw new IllegalStateException(
                    "graalgo.sched: current thread is not a scheduler goroutine");
        }
        return h;
    }

    // ================= spawn =================

    /**
     * Spawns a goroutine running {@code body}. The vthread starts
     * parked; the drainer admits it through the init lane (GG-WAKE-0003).
     * Returns the handle (for tests and M2+ wakeups).
     */
    public GoroutineHandle spawn(Runnable body) {
        GoroutineHandle h = new GoroutineHandle(body);
        long t = armPark(h);
        liveGoroutines.incrementAndGet();
        Thread vthread = Thread.ofVirtual().name("goroutine-" + h.id).unstarted(() -> {
            currentHandle.set(h);
            h.thread = Thread.currentThread();
            GoroutineState state = new GoroutineState();
            GoroutineState prev = GoroutineState.install(state);
            try {
                // Wait for the drainer's first admission.
                awaitAck(h, t);
                h.quantumDeadlineNanos = System.nanoTime() + quantumNanos;
                body.run();
            } finally {
                GoroutineState.restore(prev);
                currentHandle.remove();
                if (liveGoroutines.decrementAndGet() == 0) {
                    quiescenceLatch.countDown();
                }
                drainer.offer(new ResumeToken(ResumeToken.Kind.EXIT, h, -1));
            }
        });
        h.thread = vthread;
        drainer.offer(new ResumeToken(ResumeToken.Kind.INIT, h, t));
        vthread.start();
        return h;
    }

    /**
     * Spawns a goroutine invoking a Go function value (the {@code go}
     * statement path; frozen {@code Scheduler.spawn(GoFunc)} delegates
     * here). Installs the per-goroutine state the function root expects.
     */
    public GoroutineHandle spawnGoFunc(GoFunc fn) {
        if (fn == null) {
            throw new IllegalArgumentException("cannot go-call nil function value");
        }
        return spawn(() -> GoCalls.invoke(fn, new Object[0]));
    }

    /** Number of live goroutines (for tests and the netpoller contract). */
    public int runnableCount() {
        return liveGoroutines.get();
    }

    /** Highest in-flight count observed by the drainer. */
    public int maxInflightObserved() {
        return drainer.maxInflightObserved();
    }

    /**
     * Blocks until every spawned goroutine has finished. Only valid
     * after at least one spawn (otherwise returns immediately).
     */
    public void awaitQuiescence() throws InterruptedException {
        quiescenceLatch.await();
    }
}
