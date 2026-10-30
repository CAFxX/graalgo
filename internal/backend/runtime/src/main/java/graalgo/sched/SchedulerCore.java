package graalgo.sched;

import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.locks.LockSupport;

import graalgo.runtime.GoroutineState;
import graalgo.runtime.GoCalls;
import graalgo.runtime.sync.PinLock;
import graalgo.values.GoFunc;
import graalgo.jfr.JfrEvents;
import graalgo.jfr.ParkReason;

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
    private final graalgo.runtime.TimerHeap timerHeap;
    private final ThreadLocal<GoroutineHandle> currentHandle = new ThreadLocal<>();
    private final AtomicInteger liveGoroutines = new AtomicInteger();
    /**
     * Deadlock detection (review R1-22; see {@link #checkDeadlock}).
     * The launcher's main goroutine, once known: the detector only
     * fires while main is still parked. After main returns, gc exits
     * the process; graalgo's launcher instead awaits all goroutines
     * (documented Phase-2 semantics), so a main-finished + others-parked
     * state must NOT be reported as a deadlock — it is the pre-existing
     * await-all hang, not a new verdict. Until {@link #noteMain} is
     * called the detector stays disabled (safe default for embedders).
     */
    private volatile GoroutineHandle mainHandle;
    /**
     * Goroutines parked in waits whose wakeup comes from outside the
     * scheduler's goroutine set (netpoll readiness via
     * {@code graalgo.p3net.GoNetpoll}, bracketed by
     * {@link #noteExternalWaitEnter}/{@link #noteExternalWaitExit}).
     * Any such waiter vetoes deadlock detection: a network packet can
     * still wake it. Timer waits need no counter — the timer heap's
     * pending/firing state is the veto (see {@link #checkDeadlock}).
     */
    private final AtomicInteger externalWaiters = new AtomicInteger();
    /** Set once by {@link #checkDeadlock}; read by the launcher. */
    private volatile boolean deadlocked;
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
        // P2-M7 (GG-0057): force-initialize the JFR event classes here on
        // the startup thread. Their <clinit> calls FlightRecorder.register,
        // which parks while pinned; doing it lazily on a goroutine carrier
        // emits jdk.VirtualThreadPinned on the conformance corpus.
        JfrEvents.ensureRegistered();
        this.drainer = new Drainer(this);
        this.timerHeap = new graalgo.runtime.TimerHeap(this);
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
        timerHeap.start();
    }

    /** The scheduler's timer heap (P2-M3, WS C). */
    public graalgo.runtime.TimerHeap timerHeap() {
        return timerHeap;
    }

    /**
     * Enter blocking I/O (P2-M3, GG-0018/GG-SCHED-0004 — netpoller
     * deletion): releases the goroutine's in-flight slot synchronously
     * (returns only after the drainer has released it), so the slot is
     * immediately available to other goroutines. The vthread then blocks
     * in the real syscall; Loom unmounts it, freeing the carrier.
     *
     * <p>Every {@code ioEnter} must be paired with {@code ioExit} on the
     * same goroutine (use try/finally); re-entrant ioEnter and unpaired
     * ioExit fail fast on the calling thread, and the drainer
     * fail-fasts on any accounting mismatch.
     */
    public void ioEnter() {
        GoroutineHandle h = requireHandle();
        if (!h.inIo.compareAndSet(false, true)) {
            throw new IllegalStateException(
                    "graalgo.sched: re-entrant ioEnter (goroutine already in blocking I/O)");
        }
        long t = armPark(h);
        h.ioStartNanos = System.nanoTime();
        h.ioParkEvent = JfrEvents.ioParkBegin();
        drainer.offer(new ResumeToken(ResumeToken.Kind.IO_ENTER, h, t));
        awaitAck(h, t);
    }

    /**
     * Leave blocking I/O: re-acquires the in-flight slot through the
     * WAKE lane, blocking until the drainer admits the goroutine (gc
     * exitsyscall parity — if no slot is free, the goroutine waits
     * rather than running unaccounted).
     */
    public void ioExit() {
        GoroutineHandle h = requireHandle();
        if (!h.inIo.compareAndSet(true, false)) {
            throw new IllegalStateException(
                    "graalgo.sched: ioExit without ioEnter");
        }
        long t = armPark(h);
        drainer.offer(new ResumeToken(ResumeToken.Kind.IO_EXIT, h, t));
        awaitAck(h, t);
        // P2-M6: report the full blocking-I/O interval. The slot-release
        // and re-admission handshakes above are deliberately NOT
        // notePark'd: they are scheduler latency, not I/O, and under
        // drainer contention could exceed the park threshold — emitting
        // them would double/triple-count one syscall and mislabel the
        // reason. ioParkEnd is the single authoritative IO event: the
        // interval begun at ioEnter commits spanning the syscall
        // (reviewer2 P2-7); if no interval was begun (recording started
        // mid-syscall) it falls back to emitting the full duration at
        // exit. (ioStartNanos is taken unconditionally in ioEnter so a
        // recording started mid-syscall still observes the interval;
        // ~100ns on a syscall path is negligible.)
        graalgo.jfr.GoroutineParkEvent ioEvent = h.ioParkEvent;
        h.ioParkEvent = null;
        JfrEvents.ioParkEnd(h, ioEvent, System.nanoTime() - h.ioStartNanos);
    }

    Drainer drainer() {
        return drainer;
    }

    /**
     * Admission provenance summary (P2-M8): counts of WAKE-lane admissions
     * by cause. Format: {@code direct=<n> stashed=<n> ioExit=<n>}.
     * {@code stashed} counts PARKED tokens that found a pre-arrived UNPARK
     * (legitimate, but looks like "re-admission with no UNPARK in flight"
     * to a timeline observer — the M6 watch item).
     */
    public String wakeProvenance() {
        return "direct=" + drainer.wakeUnparkDirect.get()
                + " stashed=" + drainer.wakeUnparkStashed.get()
                + " ioExit=" + drainer.wakeIoExit.get();
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
        JfrEvents.notePark(h, ParkReason.QUANTUM);
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
        parkCurrent(onWake, ParkReason.UNKNOWN);
    }

    /**
     * Park the current goroutine indefinitely, with a JFR park reason.
     * See {@link #parkCurrent(Runnable)} for the protocol; {@code reason}
     * is recorded on the handle for the {@code graalgo.GoroutinePark}
     * event committed on resume.
     */
    public void parkCurrent(Runnable onWake, ParkReason reason) {
        GoroutineHandle h = requireHandle();
        long t = armPark(h);
        JfrEvents.notePark(h, reason);
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
     * The calling goroutine's handle. Fail-fast when the current thread
     * is not a scheduler goroutine (a loud error beats silently running
     * unscheduled).
     */
    public GoroutineHandle currentGoroutine() {
        return requireHandle();
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
        // fix-redmaster-r43: never yield inside a pin section. In gc,
        // pinning disables preemption, so a pinned goroutine is not
        // migrated mid-section; here a quantum park inside a section
        // would park while holding PinLock, stalling every other Pool
        // operation (and risking a missed re-admission against the
        // drainer). Pin sections are short and non-blocking, so the
        // yield simply moves to the first poll after unpin — the
        // deadline stays expired, so no quantum is lost.
        if (PinLock.isHeldByCurrentThread()) {
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
        // P2-M6: commit the park event (if long enough) on resume. The
        // initial admission has no parked reason and emits nothing; the
        // goroutine-start event is emitted explicitly by spawn().
        JfrEvents.parkResumed(h);
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
     * {@code RuntimeException}s, stack overflows, and any other Java
     * {@code Error} are recorded loudly (the launcher exits 1).
     * The general {@code Error} catch is load-bearing: without it an
     * {@code AssertionError}/{@code OutOfMemoryError}/other {@code Error}
     * escapes the vthread, the {@code finally} still decrements the live
     * count, and the launcher sees neither panic nor crash — exit 0 for
     * a silently dead goroutine (review R1-21).
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
                // P2-M6: the initial admission is not a park (no reason
                // was noted, so parkResumed emitted nothing); the
                // goroutine genuinely starts here.
                h.startNanos = System.nanoTime();
                JfrEvents.goroutineStart(h);
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
            } catch (Error e) {
                // R1-21: any other Error (AssertionError, OutOfMemoryError,
                // ...) is an internal failure too. Swallowed after
                // recording, like RuntimeException: the goroutine still
                // dies (the finally below runs), but the launcher now
                // sees crashed() and exits non-zero instead of 0.
                recordCrash("internal error", e);
            } finally {
                // P2-M6: every start gets an end, before the EXIT token.
                JfrEvents.goroutineEnd(h, System.nanoTime() - h.startNanos);
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
        // P3-M4 (WS C, ADR-0004 D3): full gc-style traceback, sharing the
        // phase-1 rendering (header via PanicRendering, frames via
        // Intrinsics.renderGoStack). Previously only the raw value was
        // printed (no trace; error values rendered via toString, diverging
        // from gc and from the phase-1 launcher). The goroutine id is the
        // scheduler handle id (main is 1, like gc).
        Object v = pe.goValue();
        if (v == null && graalgo.runtime.Panic.isPanicNilOldBehavior()) {
            // P2-M5: with GODEBUG=panicnil=1, an uncaught panic(nil) prints
            // "panic: nil" like pre-1.21 gc (instead of "panic: null").
            System.err.println("panic: nil");
        } else {
            System.err.println(graalgo.runtime.PanicRendering.renderPanicLine(pe));
        }
        if (v instanceof graalgo.runtime.GoErrorValue e && e.signalSegv()) {
            System.err.println("[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0xADDR]");
        }
        System.err.println();
        com.oracle.truffle.api.source.SourceSection throwSec = null;
        if (pe.throwSite() != null && pe.throwSite().getSourceSection() != null) {
            throwSec = pe.throwSite().getSourceSection();
        }
        graalgo.sched.GoroutineHandle h = currentHandle.get();
        long gid = h != null ? h.id : 1;
        System.err.print(graalgo.runtime.Intrinsics.renderGoStack(gid, pe.stackTop(), throwSec));
    }

    private void recordCrash(String what, Throwable t) {
        crashCount.incrementAndGet();
        // M10 (self-attack D3): a class-loading failure while fds are
        // exhausted is resource exhaustion, not a logic bug. The JVM loads
        // node classes lazily; a guest that consumed every fd breaks that
        // load, surfacing as NoClassDefFoundError. Reporting "internal
        // error" would send operators hunting a phantom graalgo bug —
        // name the resource instead.
        if (isClassLoadingFailure(t) && fdExhausted()) {
            System.err.println("graalgo-run: fatal fd exhaustion in goroutine: " + t
                    + " (too many open files)");
            return;
        }
        System.err.println("graalgo-run: fatal " + what + " in goroutine: " + t);
    }

    private static boolean isClassLoadingFailure(Throwable t) {
        return t instanceof NoClassDefFoundError
                || t instanceof ExceptionInInitializerError
                || t instanceof ClassNotFoundException;
    }

    /**
     * Direct fd-exhaustion probe: can this process still open a file?
     * Used to distinguish resource exhaustion from genuine internal
     * errors at crash-report time (M10 self-attack D3).
     */
    private static boolean fdExhausted() {
        try (java.io.InputStream in =
                java.nio.file.Files.newInputStream(java.nio.file.Path.of("/dev/null"))) {
            return false;
        } catch (java.io.IOException e) {
            String m = e.getMessage();
            return m != null
                    && m.toLowerCase(java.util.Locale.ROOT).contains("too many open files");
        }
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
     * Records the launcher's main goroutine. The deadlock detector
     * requires it (see the {@code mainHandle} field): call once, with
     * the handle {@link #spawn} returned for main, before awaiting
     * quiescence. Re-checks for deadlock immediately, covering the
     * race where every goroutine parked before this call.
     */
    public void noteMain(GoroutineHandle main) {
        if (main == null) {
            throw new IllegalArgumentException("main handle must not be null");
        }
        if (mainHandle != null && mainHandle != main) {
            throw new IllegalStateException("main handle already recorded");
        }
        mainHandle = main;
        checkDeadlock();
    }

    /**
     * Brackets a park whose wakeup can arrive from outside the
     * scheduler's goroutine set (currently only netpoll readiness:
     * {@code GoNetpoll.pollWaitRaw} parks here). Call immediately
     * before {@link #parkCurrent} and in a {@code finally} right after
     * it returns. While the count is non-zero, deadlock detection
     * abstains: an external event may still wake the waiter.
     */
    public void noteExternalWaitEnter() {
        externalWaiters.incrementAndGet();
    }

    /** Paired with {@link #noteExternalWaitEnter}; see its contract. */
    public void noteExternalWaitExit() {
        externalWaiters.decrementAndGet();
    }

    /** True if {@link #checkDeadlock} has fired (fatal deadlock). */
    public boolean deadlocked() {
        return deadlocked;
    }

    /**
     * All-goroutines-parked deadlock detection (review R1-22, gc
     * parity: {@code fatal error: all goroutines are asleep -
     * deadlock!}, exit 2).
     *
     * <p>Called by the drainer after every transition that can move
     * the scheduler INTO the all-parked state (PARKED, EXIT,
     * IO_ENTER tokens) and by {@link #noteMain}. Fires only when the
     * scheduler can PROVE no wakeup is possible:
     * <ul>
     *   <li>at least one goroutine is live, and every live goroutine
     *       is in the drainer's parked set — including main (a
     *       main-finished state is not a deadlock, see
     *       {@code mainHandle});
     *   <li>no goroutine is in flight (admitted, running, or
     *       quantum/yield-parked — quantum parks re-enter through
     *       the run lane, so they are runnable work, not sleep);
     *   <li>no drainer lane or inbox token is unprocessed, no UNPARK
     *       is stashed, and no goroutine is in blocking I/O
     *       ({@code ioEnter} without {@code ioExit});
     *   <li>no timer is pending and no timer fire is in progress
     *       (a pending timer will wake its sleeper, run its callback,
     *       or deliver to its channel; the firing flag closes the
     *       pop-then-unpark race, where the entry is already gone
     *       from the heap but the UNPARK is not yet offered);
     *   <li>no externally-woken waiter exists
     *       ({@link #noteExternalWaitEnter}).
     * </ul>
     *
     * <p>Why this cannot false-positive on a legitimately waiting
     * program: every wakeup source in this codebase is one of (a) a
     * live goroutine (channel/mutex/cond/waitgroup/select sends and
     * unlocks all require a scheduler handle, so a non-goroutine thread
     * cannot perform them — a Cleaner-thread finalizer invoking Go
     * code fail-fasts in {@code currentGoroutine()} before it could
     * unpark anyone), (b) the timer thread (vetoed by pending/firing),
     * (c) the netpoll poller thread (vetoed by the external-waiter
     * count), or (d) the drainer itself (vetoed by lane/inbox
     * emptiness). A goroutine blocked in a real syscall without
     * {@code ioEnter} stays in flight (veto); with {@code ioEnter} it
     * is in the in-I/O set (veto) — both conservative, matching gc,
     * which never declares deadlock over a syscall-blocked goroutine.
     * Goroutines blocked on JVM monitors/sleeps outside the scheduler
     * protocol likewise stay in flight and veto (a missed detection,
     * never a false positive).
     *
     * <p>Precondition (shared with {@link #awaitQuiescence}): no
     * outside thread spawns concurrently with detection. Spawns from
     * inside goroutines are safe: the spawning goroutine is in flight
     * for the whole spawn call, so the in-flight veto covers any
     * check overlapping it. The launcher spawns main before
     * {@link #noteMain}, so production has no outside spawner during
     * detection; a test harness that spawns from the test thread
     * after calling {@code noteMain} could observe a false positive
     * and must not do so.
     *
     * <p>Known limitation (conservative miss, not a false positive):
     * a pending timer whose fire cannot actually wake anyone (e.g. a
     * ticker nobody receives from) still vetoes, so such programs
     * keep hanging instead of being reported. Refining the veto to
     * sleep/callback timers only is future work.
     *
     * <p>On firing, prints gc's fatal line once and releases
     * {@link #awaitQuiescence} (the parked goroutines never resume,
     * so without the release the launcher would hang). The launcher
     * maps {@link #deadlocked()} to exit 2. Lock order: drainer lock
     * (held by the drainer caller) → timer-heap lock; no path takes
     * them in the reverse order (the timer thread never touches the
     * drainer lock — it only offers to the inbox queue).
     */
    void checkDeadlock() {
        if (deadlocked) {
            return;
        }
        GoroutineHandle main = mainHandle;
        if (main == null) {
            return;
        }
        if (externalWaiters.get() != 0) {
            return;
        }
        if (timerHeap.hasPendingOrFiring()) {
            return;
        }
        if (!drainer.isAllParked(liveGoroutines.get(), main.id)) {
            return;
        }
        deadlocked = true;
        System.err.println("fatal error: all goroutines are asleep - deadlock!");
        synchronized (quiescenceLock) {
            quiescenceLock.notifyAll();
        }
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

    /**
     * Number of goroutines that currently exist, for
     * {@code runtime.NumGoroutine} (gc: "returns the number of goroutines
     * that currently exist", {@code src/runtime/debug.go}).
     *
     * <p>Definition (graalgo): a goroutine exists from the moment
     * {@link #spawn} returns (the count increments immediately after the
     * vthread starts) until its body completes (the count decrements in
     * the vthread's {@code finally} block, before the EXIT token is
     * offered). Blocked/parked goroutines — channel waits, mutexes,
     * timers, scheduler parks — count, exactly like gc, which counts
     * waiting goroutines. The main goroutine counts: launchers run main
     * via {@code spawn}, so a fresh program observes 1, like gc.
     *
     * <p>The only window the count misses is the instant between
     * {@code vthread.start()} and the increment; no "created but not
     * started" goroutine is observable to Go code, matching gc where a
     * goroutine exists from {@code newproc} on.
     */
    public int numGoroutines() {
        return liveGoroutines.get();
    }

    /** Highest in-flight count observed by the drainer. */
    public int maxInflightObserved() {
        return drainer.maxInflightObserved();
    }

    /**
     * Blocks until every spawned goroutine has finished, or until
     * {@link #checkDeadlock} fires (then returns early with
     * {@link #deadlocked()} true; the launcher maps that to exit 2).
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
            while (liveGoroutines.get() != 0 && !deadlocked) {
                quiescenceLock.wait();
            }
        }
    }
}
