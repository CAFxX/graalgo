package graalgo.sched;

import java.util.function.Function;
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
    /** Builds one goroutine's unstarted carrier thread (see the constructor). */
    private final Function<Runnable, Thread> goroutineThreads;

    /**
     * Thrown out of a park into a goroutine when its scheduler is torn down
     * ({@link #terminate}): unwinds the goroutine without running defers
     * (gc runs none at exit) and is swallowed by the goroutine wrapper.
     */
    public static final class GoroutineTerminated extends Error {
        private static final long serialVersionUID = 1L;

        GoroutineTerminated() {
            super("goroutine terminated: scheduler torn down", null, false, false);
        }
    }

    private static final GoroutineTerminated TERMINATED = new GoroutineTerminated();

    /** Set by {@link #terminate}: every park throws {@link GoroutineTerminated}. */
    private volatile boolean terminated;

    /**
     * Carrier threads that may still be alive. A carrier is removed only
     * once its thread has terminated (pruned lazily on spawn), so that
     * {@link #terminate} also joins carriers whose goroutine body already
     * returned but whose thread is still finishing.
     */
    private final java.util.Set<Thread> goroutineCarriers =
            java.util.concurrent.ConcurrentHashMap.newKeySet();

    private void trackCarrier(Thread t) {
        goroutineCarriers.add(t);
        // Amortized pruning: the set stays within a constant factor of
        // the live goroutine count.
        if (goroutineCarriers.size() > 2 * liveGoroutines.get() + 64) {
            goroutineCarriers.removeIf(c -> c.getState() == Thread.State.TERMINATED);
        }
    }

    private static Thread plainVirtualThread(Runnable body) {
        return Thread.ofVirtual().unstarted(body);
    }

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

    /** The installed scheduler, or null when none is installed. */
    public static SchedulerCore installedOrNull() {
        return installed;
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

    /**
     * Effective in-flight cap = min(GOMAXPROCS, carrier parallelism).
     * Volatile: {@code runtime.GOMAXPROCS(n)} changes it at run time
     * (SCHED-0003); the drainer reads it on every admission.
     */
    private volatile int cap;
    private volatile int requestedCap;
    private final Object capLock = new Object();
    private final int carrierParallelism;
    private final long quantumNanos;
    private final Drainer drainer;
    private final graalgo.runtime.TimerHeap timerHeap;
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
        this(gomaxprocs, quantumNanos, SchedulerCore::plainVirtualThread);
    }

    /**
     * @param goroutineThreads builds the (unstarted) carrier thread of one
     *     goroutine around its body. Inside a polyglot context these are
     *     polyglot threads (graalgo.language.GoContext); the default, for
     *     embedders and tests without a context, is a bare virtual thread.
     */
    public SchedulerCore(int gomaxprocs, long quantumNanos,
            Function<Runnable, Thread> goroutineThreads) {
        this.goroutineThreads = goroutineThreads;
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
        // Same reason: GoroutineState.install consults the Go context
        // holder on every goroutine start; initialize its class here, not
        // first on a goroutine carrier (class-init waits pin).
        graalgo.language.GoContext.activeOrNull();
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

    /**
     * {@code runtime.GOMAXPROCS(n)} (gc parity): returns the previous
     * setting; {@code n < 1} only queries. The change takes effect for
     * admissions immediately: raising the cap admits queued goroutines at
     * once (the drainer is poked); lowering it lets the goroutines already
     * in flight run to their next park/quantum boundary — no admission
     * happens until in-flight drops below the new cap (gc stops the world
     * and resizes the P set; here the excess drains at the next quantum
     * boundary, where gc's preempted Gs would also lose their P). The
     * effective cap stays clamped to the carrier parallelism (SPEC §3.5).
     */
    public int setGomaxprocs(int n) {
        synchronized (capLock) {
            int prev = requestedCap;
            if (n >= 1) {
                requestedCap = n;
                cap = Math.min(n, carrierParallelism);
                drainer.offer(ResumeToken.poke());
            }
            return prev;
        }
    }

    public int carrierParallelism() {
        return carrierParallelism;
    }

    /** Starts the drainer. Must precede the first spawn. */
    public void start() {
        // A new program run in this process (an embedder's next context):
        // the previous run's exit no longer applies.
        exitPending = false;
        drainer.start();
        timerHeap.start();
    }

    /** The scheduler's timer heap (P2-M3, WS C). */
    public graalgo.runtime.TimerHeap timerHeap() {
        return timerHeap;
    }

    /**
     * Enter blocking I/O (P2-M3, GG-0063/GG-SCHED-0005 — v1 netpoller
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
        parkCurrent(onWake, reason, defaultWaitReason(reason));
    }

    /**
     * Park with gc's wait-reason string ({@code runtime/runtime2.go}
     * {@code waitReasonStrings}: {@code "chan receive"},
     * {@code "sync.Mutex.Lock"}, ...), rendered in the
     * {@code goroutine N [reason]:} header of a deadlock dump.
     *
     * <p>{@link ParkReason#NETPOLL} parks are woken by the netpoller's
     * selector thread, not by a goroutine: they mark the goroutine
     * externally wakeable, so they never count toward deadlock.
     */
    public void parkCurrent(Runnable onWake, ParkReason reason, String waitReason) {
        GoroutineHandle h = requireHandle();
        long t = armPark(h);
        JfrEvents.notePark(h, reason);
        h.wakeAction.set(onWake);
        h.waitReason = waitReason;
        h.externalWake = reason == ParkReason.NETPOLL;
        // The Go stack a deadlock dump renders: the state live NOW (the
        // program root installs its own GoroutineState for main).
        h.state = GoroutineState.current();
        drainer.offer(new ResumeToken(ResumeToken.Kind.PARKED, h, t));
        awaitAck(h, t);
        h.externalWake = false;
        Runnable action = h.wakeAction.getAndSet(null);
        if (action != null) {
            action.run();
        }
    }

    /** gc wait reason for a park that did not name one explicitly. */
    static String defaultWaitReason(ParkReason reason) {
        return switch (reason) {
            case CHANNEL -> "chan receive";
            case SELECT -> "select";
            case TIMER -> "sleep";
            case NETPOLL, IO -> "IO wait";
            default -> "semacquire";
        };
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
        if (exiting) {
            if (h == fatalOwner) {
                // The fatal reporter runs Error()/String() with the world
                // stopped: it must never quantum-park (no admission
                // happens any more), so it runs to completion.
                return;
            }
            // Process exit in progress (main returned, os.Exit, fatal
            // panic/deadlock): freeze this goroutine at its safepoint so
            // it produces no further output while the launcher exits
            // (gc: exit/fatalpanic stop the world first). After the pin
            // check, so a frozen goroutine never holds PinLock (the
            // reporter's fmt calls pin sync.Pool).
            freezeForExit();
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
            if (terminated) {
                throw TERMINATED;
            }
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
     * {@code RuntimeException}s and stack overflows are recorded loudly
     * (the launcher exits 1).
     */
    public GoroutineHandle spawn(Runnable body) {
        return spawn(body, false, false);
    }

    /**
     * Spawns the program's main goroutine: when its body returns, the
     * process exits 0 (gc: {@code main} returning calls {@code exit(0)}
     * without waiting for other goroutines); if it calls
     * {@code runtime.Goexit}, the program continues until the remaining
     * goroutines finish or deadlock (gc {@code checkdead}).
     */
    public GoroutineHandle spawnMain(Runnable body) {
        return spawn(body, true, false);
    }

    /**
     * Spawns a runtime-internal goroutine (gc {@code isSystemGoroutine}):
     * it never appears in deadlock dumps and does not keep a deadlock
     * from being reported.
     */
    public GoroutineHandle spawnSystem(Runnable body) {
        return spawn(body, false, true);
    }

    private GoroutineHandle spawn(Runnable body, boolean main, boolean system) {
        GoroutineHandle h = new GoroutineHandle(body);
        h.isMain = main;
        h.system = system;
        if (main) {
            // Deadlock detection is a property of a Go PROGRAM (gc
            // checkdead): armed only once a main goroutine exists, so
            // embedders and unit tests that drive goroutines from outside
            // (unparking them from a platform thread) never trip it.
            mainSpawned = true;
        }
        // "created by" provenance for tracebacks: the spawning goroutine
        // and its innermost Go frame (the go statement's function).
        GoroutineHandle parent = currentHandle.get();
        if (parent != null) {
            h.creatorId = parent.id;
            GoroutineState ps = GoroutineState.current();
            if (ps != null && ps.goStack() != null && !ps.goStack().isEmpty()) {
                h.creatorFunc = ps.goStack().funcName();
                h.creatorSection = ps.goStack().section();
            }
        }
        long t = armPark(h);
        Thread vthread = goroutineThreads.apply(() -> {
            currentHandle.set(h);
            h.thread = Thread.currentThread();
            GoroutineState state = new GoroutineState();
            h.state = state;
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
                if (h.isMain) {
                    // gc: main returning exits the process with status 0,
                    // abandoning every other goroutine (runtime.main).
                    requestExit(0);
                }
            } catch (GoroutineTerminated gt) {
                // Torn down by terminate(): the program is over.
            } catch (graalgo.runtime.PanicException pe) {
                recordPanic(pe);
            } catch (graalgo.runtime.Goexit g) {
                // Goexit: this goroutine just dies. For main, the program
                // keeps running until the other goroutines finish or
                // block (gc: "no goroutines (main called
                // runtime.Goexit) - deadlock!" via the drainer's check).
                if (h.isMain) {
                    mainGoexited = true;
                }
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
                // Any other JVM error (AssertionError, OutOfMemoryError,
                // ...): the program can no longer be trusted — loud exit,
                // never a silently dead goroutine and a hung process.
                recordCrash("internal error", e);
            } finally {
                // P2-M6: every start gets an end, before the EXIT token.
                JfrEvents.goroutineEnd(h, System.nanoTime() - h.startNanos);
                GoroutineState.restore(prev);
                currentHandle.remove();
                if (!h.system) {
                    liveUser.decrementAndGet();
                }
                synchronized (quiescenceLock) {
                    if (liveGoroutines.decrementAndGet() == 0) {
                        quiescenceLock.notifyAll();
                    }
                }
                drainer.offer(new ResumeToken(ResumeToken.Kind.EXIT, h, -1));
            }
        });
        vthread.setName("goroutine-" + h.id);
        trackCarrier(vthread);
        h.thread = vthread;
        if (!system) {
            liveUser.incrementAndGet();
        }
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

    /**
     * An uncaught Go panic is fatal to the whole process, in any
     * goroutine (gc {@code fatalpanic}): print the panic chain and this
     * goroutine's traceback, then exit 2 immediately — other goroutines
     * are frozen, never awaited.
     */
    private void recordPanic(graalgo.runtime.PanicException pe) {
        panicCount.incrementAndGet();
        if (!beginFatal()) {
            return; // another fatal error owns the report; process is exiting
        }
        // gc preprintpanics: Error()/String() run on the panicking
        // goroutine, before the world is considered dead.
        String header = graalgo.runtime.PanicRendering.renderPanicChain(pe);
        StringBuilder sb = new StringBuilder(header);
        Object v = pe.goValue();
        if (v instanceof graalgo.runtime.GoErrorValue e && e.signalSegv()) {
            sb.append("[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x0]\n");
        }
        sb.append('\n');
        com.oracle.truffle.api.source.SourceSection throwSec = null;
        if (pe.throwSite() != null && pe.throwSite().getSourceSection() != null) {
            throwSec = pe.throwSite().getSourceSection();
        }
        GoroutineHandle h = currentHandle.get();
        long gid = h != null ? h.id : 1;
        sb.append(graalgo.runtime.Intrinsics.renderGoStack(gid, "running", pe.stackTop(), throwSec));
        if (h != null) {
            sb.append(createdBy(h));
        }
        System.out.flush();
        System.err.print(sb);
        System.err.flush();
        requestExit(2);
    }

    private void recordCrash(String what, Throwable t) {
        crashCount.incrementAndGet();
        if (!beginFatal()) {
            return;
        }
        // M10 (self-attack D3): a class-loading failure while fds are
        // exhausted is resource exhaustion, not a logic bug. The JVM loads
        // node classes lazily; a guest that consumed every fd breaks that
        // load, surfacing as NoClassDefFoundError. Reporting "internal
        // error" would send operators hunting a phantom graalgo bug —
        // name the resource instead.
        if (isClassLoadingFailure(t) && fdExhausted()) {
            System.err.println("graalgo-run: fatal fd exhaustion in goroutine: " + t
                    + " (too many open files)");
        } else {
            System.err.println("graalgo-run: fatal " + what + " in goroutine: " + t);
        }
        System.err.flush();
        requestExit(1);
    }

    // ================= process lifecycle =================

    private final java.util.concurrent.CompletableFuture<Integer> exitCode =
            new java.util.concurrent.CompletableFuture<>();
    /** Set once the process is exiting: admissions stop, safepoints freeze. */
    private volatile boolean exiting;
    /**
     * Process-wide mirror of {@link #exiting} for the strided
     * {@code Safepoint.poll()} fast path: the poll countdown skips the
     * full quantum check for up to {@code STRIDE - 1} polls, but a
     * pending exit must still freeze a running goroutine at its very next
     * safepoint (no extra output after exit/fatal begins), so the fast
     * path reads this flag on every poll. Set together with
     * {@link #exiting}; cleared when the next scheduler starts (a later
     * program run in the same process, e.g. an embedder's next context).
     */
    private static volatile boolean exitPending;

    /** True once any installed scheduler began process exit. */
    public static boolean exitPending() {
        return exitPending;
    }
    /** First fatal reporter wins the right to print (gc: one fatal trace). */
    private final java.util.concurrent.atomic.AtomicBoolean fatal =
            new java.util.concurrent.atomic.AtomicBoolean();
    /** The goroutine printing the fatal report (exempt from the freeze). */
    private volatile GoroutineHandle fatalOwner;
    /** Live non-system goroutines (gc checkdead's user goroutine count). */
    private final AtomicInteger liveUser = new AtomicInteger();
    /** A main goroutine was spawned: deadlock detection is armed. */
    private volatile boolean mainSpawned;

    /** True when deadlock detection applies (a Go program is running). */
    boolean deadlockArmed() {
        return mainSpawned;
    }

    /** Main goroutine called runtime.Goexit. */
    private volatile boolean mainGoexited;

    /**
     * Asks the drainer to re-evaluate scheduler-global conditions (the
     * timer heap calls this when its last timer retires, so a deadlock
     * that a pending timer was masking gets reported).
     */
    public void pokeScheduler() {
        drainer.offer(ResumeToken.poke());
    }

    /** True once the main goroutine called runtime.Goexit. */
    boolean mainGoexited() {
        return mainGoexited;
    }

    /** Claims the single fatal-report slot; false if another fatal won. */
    private boolean beginFatal() {
        if (!fatal.compareAndSet(false, true)) {
            return false;
        }
        // The reporting goroutine keeps running (it may call Go
        // Error()/String() methods while rendering); everyone else freezes.
        fatalOwner = currentHandle.get();
        exiting = true;
        exitPending = true;
        return true;
    }

    /**
     * Requests process exit with {@code code} (main returned: 0; uncaught
     * panic / deadlock: 2; {@code os.Exit(n)}: n). The first request
     * wins; the launcher's {@link #awaitExit} returns it. Admissions stop
     * immediately and running goroutines freeze at their next safepoint.
     */
    public void requestExit(int code) {
        exiting = true;
        exitPending = true;
        exitCode.complete(code);
    }

    /** True when the calling thread is a scheduler goroutine. */
    public boolean onGoroutine() {
        return currentHandle.get() != null;
    }

    /** True once process exit has been requested. */
    public boolean isExiting() {
        return exiting;
    }

    /**
     * Blocks until the program requests process exit; returns the exit
     * code. This replaces waiting for quiescence: gc never waits for
     * goroutines other than main.
     */
    public int awaitExit() throws InterruptedException {
        try {
            return exitCode.get();
        } catch (java.util.concurrent.ExecutionException e) {
            throw new IllegalStateException(e.getCause());
        }
    }

    /**
     * Tears the scheduler down after the program has exited (an embedder
     * closing its context, truffle-design §2.3): every goroutine still
     * parked — frozen by exit, blocked on a channel, waiting for
     * admission — unwinds with {@link GoroutineTerminated} at its park;
     * no defers run. Waits up to {@code timeoutMillis} for the carriers to
     * finish and returns the ones that did not (a goroutine inside a
     * blocking system call, or spinning without a park, cannot be
     * stopped this way).
     */
    public java.util.List<Thread> terminate(long timeoutMillis) throws InterruptedException {
        terminated = true;
        exiting = true;
        exitPending = true;
        drainer.stop();
        timerHeap.stop();
        long deadline = System.nanoTime() + timeoutMillis * 1_000_000L;
        for (Thread t : goroutineCarriers) {
            LockSupport.unpark(t);
        }
        java.util.List<Thread> stuck = new java.util.ArrayList<>();
        for (Thread t : goroutineCarriers) {
            long left = (deadline - System.nanoTime()) / 1_000_000L;
            if (left <= 0 || !t.join(java.time.Duration.ofMillis(left))) {
                if (t.isAlive()) {
                    stuck.add(t);
                }
            }
        }
        return stuck;
    }

    /**
     * Stops the calling goroutine for good during process exit: it parks
     * forever (no defers run — gc's exit runs none either), so it can
     * produce no further output while the launcher exits the process.
     * No-op off scheduler goroutines.
     */
    public void freezeForExit() {
        if (currentHandle.get() == null) {
            return;
        }
        for (;;) {
            if (terminated) {
                throw TERMINATED;
            }
            LockSupport.park(this);
        }
    }

    /**
     * Drainer callback: no goroutine can run and nothing outside the
     * scheduler (timer, I/O, netpoller) can wake one (gc
     * {@code checkdead}). {@code parked} are the parked goroutines.
     * Called on the drainer thread with the drainer lock held.
     *
     * @return true if a fatal deadlock was reported
     */
    boolean onAllParked(java.util.List<GoroutineHandle> parked, boolean timersPending) {
        if (exiting) {
            return false;
        }
        java.util.List<GoroutineHandle> user = new java.util.ArrayList<>();
        for (GoroutineHandle h : parked) {
            if (!h.system) {
                user.add(h);
            }
        }
        if (user.isEmpty() && liveUser.get() == 0) {
            if (!mainGoexited) {
                return false; // main not started yet / already exiting
            }
            if (!beginFatal()) {
                return false;
            }
            System.out.flush();
            System.err.print("fatal error: no goroutines (main called runtime.Goexit) - deadlock!\n");
            System.err.flush();
            requestExit(2);
            return true;
        }
        if (timersPending || user.isEmpty() || user.size() != liveUser.get()) {
            // A timer may still wake someone (gc returns from checkdead
            // while any P has timers), or a user goroutine is not parked
            // (still starting/exiting): not a deadlock.
            return false;
        }
        if (!beginFatal()) {
            return false;
        }
        user.sort(java.util.Comparator.comparingLong(h -> h.id));
        StringBuilder sb = new StringBuilder();
        sb.append("fatal error: all goroutines are asleep - deadlock!\n");
        for (GoroutineHandle h : user) {
            sb.append('\n');
            String reason = h.waitReason != null ? h.waitReason : "semacquire";
            graalgo.runtime.GoStackFrame top = h.state != null ? h.state.goStack() : null;
            sb.append(graalgo.runtime.Intrinsics.renderGoStack(h.id, reason, top, null));
            sb.append(createdBy(h));
        }
        System.out.flush();
        System.err.print(sb);
        System.err.flush();
        requestExit(2);
        return true;
    }

    /** gc's {@code created by F in goroutine N} trailer (empty for main). */
    private static String createdBy(GoroutineHandle h) {
        if (h.isMain || h.creatorId == 0 || h.creatorFunc == null) {
            return "";
        }
        StringBuilder sb = new StringBuilder();
        sb.append("created by ").append(graalgo.runtime.Intrinsics.goFrameName(h.creatorFunc))
          .append(" in goroutine ").append(h.creatorId).append('\n');
        if (h.creatorSection != null) {
            sb.append('\t').append(graalgo.runtime.Intrinsics.sourcePath(h.creatorSection))
              .append(':').append(h.creatorSection.getStartLine()).append('\n');
        }
        return sb.toString();
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
