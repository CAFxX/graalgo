package graalgo.sched;

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
    /**
     * Quiescence monitor (reviewer2 P0). A one-shot CountDownLatch cannot
     * work here: the live count hits zero transiently mid-spawn-stream
     * (early trivial goroutines drain before later spawns are issued),
     * firing the latch early with no way to un-fire. The monitor
     * re-checks the condition on every wakeup, so a transient zero
     * before the await call cannot cause an early return.
     */
    private final Object quiescenceLock = new Object();

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
     * Park the current goroutine indefinitely. When the goroutine is
     * re-admitted via {@link #unpark(GoroutineHandle)}, {@code onWake}
     * runs on the goroutine's own vthread immediately after resume
     * (before {@code parkCurrent} returns). Wake via
     * {@link #unpark(GoroutineHandle)}; the drainer matches PARKED/UNPARK
     * by handle, tolerating both orders — no wakeup (and no wake action)
     * is lost.
     */
    public void parkCurrent(Runnable onWake) {
        GoroutineHandle h = requireHandle();
        long t = armPark(h);
        h.wakeAction.set(onWake);
        drainer.offer(new ResumeToken(ResumeToken.Kind.PARKED, h, t));
        awaitAck(h, t);
        Runnable action = h.wakeAction.getAndSet(null);
        if (action != null) {
            action.run();
        }
    }

    /**
     * Wake a parked goroutine: enqueues an UNPARK token; the drainer
     * matches it against the parked record (or stashes it if the park
     * hasn't been processed yet) and re-admits through the WAKE lane.
     * The goroutine's wake action runs on its own vthread after resume.
     * Safe to call before the target parks.
     */
    public void unpark(GoroutineHandle h) {
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
     *
     * <p>Failure discipline (Go parity): an uncaught Go panic in
     * <em>any</em> goroutine is recorded and printed (the launcher exits
     * 2); {@code Goexit} kills just that goroutine; internal
     * {@code RuntimeException}s and stack overflows are recorded loudly
     * (the launcher exits 1).
     */
    public GoroutineHandle spawn(Runnable body) {
        GoroutineHandle h = new GoroutineHandle(body);
        long t = armPark(h);
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
            } catch (graalgo.runtime.PanicException pe) {
                recordPanic(pe);
            } catch (graalgo.runtime.Goexit g) {
                // Non-main goroutine Goexit: just dies. (Main's Goexit
                // is handled by the launcher's own catch, inside.)
            } catch (StackOverflowError soe) {
                recordCrash("stack overflow", soe);
            } catch (NoClassDefFoundError | ExceptionInInitializerError e) {
                if (isStackOverflowCascade(e)) {
                    recordCrash("stack overflow", e);
                } else {
                    recordCrash("internal error", e);
                }
            } catch (RuntimeException e) {
                recordCrash("internal error", e);
            } finally {
                GoroutineState.restore(prev);
                currentHandle.remove();
                synchronized (quiescenceLock) {
                    if (liveGoroutines.decrementAndGet() == 0) {
                        quiescenceLock.notifyAll();
                    }
                }
                drainer.offer(new ResumeToken(ResumeToken.Kind.EXIT, h, -1));
            }
        });
        h.thread = vthread;
        // Start BEFORE accounting: if start() throws, no slot is leaked
        // and the live count is untouched. The vthread blocks in awaitAck
        // until the drainer admits it, so early start is safe.
        vthread.start();
        synchronized (quiescenceLock) {
            liveGoroutines.incrementAndGet();
        }
        drainer.offer(new ResumeToken(ResumeToken.Kind.INIT, h, t));
        return h;
    }

    private final AtomicInteger panicCount = new AtomicInteger();
    private final AtomicInteger crashCount = new AtomicInteger();
    private volatile String firstPanicMessage;

    private void recordPanic(graalgo.runtime.PanicException pe) {
        if (panicCount.getAndIncrement() == 0) {
            firstPanicMessage = String.valueOf(pe.goValue());
        }
        // gc prints "panic: <value>" for an uncaught panic. Full gc-style
        // stack rendering is Phase-1's reportPanic; M1 prints the value
        // (PanicRendering is Phase-1-internal, not in the freeze).
        System.err.println("panic: " + pe.goValue());
    }

    private void recordCrash(String what, Throwable t) {
        crashCount.incrementAndGet();
        System.err.println("graalgo-run: fatal " + what + " in goroutine: " + t);
    }

    /** True if any goroutine died from an uncaught Go panic. */
    public boolean panicked() {
        return panicCount.get() > 0;
    }

    /** True if any goroutine died from an internal error. */
    public boolean crashed() {
        return crashCount.get() > 0;
    }

    /**
     * The M10 self-attack's stack-overflow cascade: Truffle's exception
     * profiler can blow the exhausted stack while handling a
     * StackOverflowError, masking it as a class-init failure.
     */
    private static boolean isStackOverflowCascade(Throwable t) {
        Throwable c = t;
        while (c != null) {
            if (c instanceof StackOverflowError) {
                return true;
            }
            c = c.getCause();
        }
        return false;
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
     * Blocks until every spawned goroutine has finished.
     *
     * <p><strong>No-concurrent-spawn contract:</strong> {@code spawn()}
     * must not be called concurrently with this method from another
     * thread. The spawn set must be closed before awaiting; a spawn that
     * starts after this method has observed zero and returned will not
     * be waited for. (The canonical spawn-loop-then-await from a single
     * thread is safe: the monitor re-checks the live count at await
     * time, so a transient zero mid-loop cannot cause an early return —
     * reviewer2 P0.)
     */
    public void awaitQuiescence() throws InterruptedException {
        synchronized (quiescenceLock) {
            while (liveGoroutines.get() != 0) {
                quiescenceLock.wait();
            }
        }
    }
}
