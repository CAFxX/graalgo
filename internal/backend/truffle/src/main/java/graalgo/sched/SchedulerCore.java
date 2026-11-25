package graalgo.sched;

import java.util.function.Function;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.locks.LockSupport;
import com.oracle.truffle.api.TruffleSafepoint;

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
 *   <li>Direct handoff (WP-SCHED, ADR-0016; gc's runnext): a goroutine's
 *       {@link #unpark} of another is deferred as its
 *       {@link GoroutineHandle#runnext}; if it parks within
 *       {@link #HANDOFF_WINDOW_NANOS} it hands the woken goroutine its
 *       in-flight slot and quantum (a HANDOFF token books it, the waker
 *       admits it itself), else the wake goes through the drainer. The
 *       drainer's slot count stays exact and never below the goroutines
 *       actually running on slots.
 * </ul>
 *
 * <p>Threading: vthreads only offer to the drainer's inbox and park;
 * all lane/inflight mutation happens on the drainer thread. The static
 * {@link #install}/{@link #current} holds the launcher-installed
 * scheduler; unit tests use instances directly.
 */
public final class SchedulerCore {
    /**
     * Builds one goroutine's unstarted thread (thread mode), or one unstarted carrier thread
     * (carrier mode: {@link #carriers} is non-null).
     */
    private final Function<Runnable, Thread> goroutineThreads;

    /**
     * Carrier mode (ADR-0018, the default): goroutines are continuations on a few carrier
     * threads, so spawning one costs the same however many are alive. Null in thread mode
     * (one thread per goroutine: {@code -Dgraalgo.sched.goroutines=thread}, or a JVM that does
     * not export {@code jdk.internal.vm} to the runtime).
     */
    private final Carriers carriers;

    /**
     * Live goroutines in carrier mode ({@link #terminate} unwinds them): an intrusive list
     * through {@link GoroutineHandle#liveNext}/{@code livePrev}, guarded by
     * {@link #quiescenceLock} (taken by spawn and exit anyway), so tracking a goroutine costs
     * neither an allocation nor a hash-set resize.
     */
    private GoroutineHandle liveList;

    /**
     * Carrier mode is available: {@code java.base} exports {@code jdk.internal.vm} to the
     * runtime ({@code --add-exports java.base/jdk.internal.vm=ALL-UNNAMED}, which every graalgo
     * launcher passes).
     */
    public static final boolean CARRIER_MODE_AVAILABLE =
            Object.class.getModule().isExported("jdk.internal.vm", SchedulerCore.class.getModule());

    /**
     * Whether new schedulers run in carrier mode (GG-0073, ADR-0018: the default). The system
     * property {@code graalgo.sched.goroutines} selects the mode when a context does not set
     * {@code go.GoroutineThreads}: unset or {@code carrier}, carrier mode; {@code thread}, one
     * thread per goroutine (GG-0073's fallback). Read per scheduler, so a test can switch it. A
     * JVM without the {@code jdk.internal.vm} export fails loudly at scheduler creation rather
     * than falling back to the quadratic mode unnoticed.
     */
    public static boolean carrierModeDefault() {
        String mode = System.getProperty("graalgo.sched.goroutines", "carrier");
        return switch (mode) {
            case "carrier" -> true;
            case "thread" -> false;
            default -> throw new IllegalArgumentException(
                    "graalgo.sched.goroutines must be 'carrier' or 'thread', got '" + mode + "'");
        };
    }

    /**
     * Whether the embedding is tearing the program down (a polyglot context being cancelled or
     * hard-exited): an {@link Error} that ends a goroutine then is that teardown, not a crash.
     * Set once, before {@link #start}, by the context; never outside one.
     */
    private java.util.function.BooleanSupplier tornDown = () -> false;

    /**
     * Goroutine threads (thread mode) are polyglot threads of a context: their parks are
     * Truffle-blocked ({@link TruffleSafepoint#setBlockedThreadInterruptible}), so thread-local
     * actions — cancellation, hard exit, samplers, the debugger — reach parked goroutines
     * (playbook §10). Carrier mode decides the same per carrier ({@link Carriers}).
     */
    private boolean inContext;

    /**
     * Binds the scheduler to the polyglot context that owns its threads (before {@link #start}):
     * {@code tornDown} tells whether the context is being cancelled or hard-exited.
     */
    public void bindContext(java.util.function.BooleanSupplier tornDown) {
        this.tornDown = tornDown;
        this.inContext = true;
    }

    /** Whether the owning context is being cancelled or hard-exited ({@link #bindContext}). */
    boolean isTornDown() {
        return tornDown.getAsBoolean();
    }

    /** True when goroutines run as continuations on carrier threads. */
    public boolean carrierMode() {
        return carriers != null;
    }

    /** The carrier pool (carrier mode), or null; tests only. */
    Carriers carriersForTest() {
        return carriers;
    }

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

    private static Thread plainPlatformThread(Runnable body) {
        return Thread.ofPlatform().unstarted(body);
    }

    /** Default quantum: 10 ms (P2-D5, measured in spike 2b). */
    public static final long DEFAULT_QUANTUM_NANOS = 10_000_000L;

    private static volatile SchedulerCore installed;

    /**
     * Installs the launcher-wide scheduler. Returns the previous one.
     */
    public static SchedulerCore install(SchedulerCore next) {
        synchronized (SchedulerCore.class) {
            SchedulerCore prev = installed;
            installed = next;
            return prev;
        }
    }

    /**
     * The scheduler whose goroutine runs on the calling thread (null off
     * goroutine threads). Safepoints poll their OWN scheduler, not the
     * installed one: a goroutine left behind by an earlier program (whose
     * scheduler is no longer installed) must still freeze and unwind when
     * that scheduler exits or is torn down.
     */
    private static final ThreadLocal<SchedulerCore> GOROUTINE_SCHEDULER = new ThreadLocal<>();

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
     *
     * @return true when the calling goroutine's own scheduler is exiting
     *         and the poll returned without freezing (pin section, fatal
     *         reporter), or when the goroutine still holds a deferred wake
     *         ({@link GoroutineHandle#runnext}, released by the first full
     *         poll past {@link #HANDOFF_WINDOW_NANOS}): the caller's next
     *         poll must be a full one again ({@link #exitEpoch}).
     */
    public static boolean pollCurrentThreadStatic() {
        // Common case first: a goroutine of the installed scheduler (one
        // ThreadLocal lookup, as before). Only a thread that is not one
        // pays the second lookup for its own scheduler.
        SchedulerCore sc = installed;
        GoroutineHandle h;
        if (sc != null && (h = sc.pollIfGoroutine()) != null) {
            return sc.exiting || h.runnext != null;
        }
        SchedulerCore own = GOROUTINE_SCHEDULER.get();
        if (own != null && own != sc) {
            h = own.pollIfGoroutine();
            return own.exiting || (h != null && h.runnext != null);
        }
        return false;
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
        this(gomaxprocs, quantumNanos, carrierModeDefault());
    }

    private SchedulerCore(int gomaxprocs, long quantumNanos, boolean carrierMode) {
        this(gomaxprocs, quantumNanos,
                carrierMode ? SchedulerCore::plainPlatformThread : SchedulerCore::plainVirtualThread,
                null, carrierMode);
    }

    /**
     * Thread mode with an explicit thread factory (tests, tools).
     *
     * @param goroutineThreads builds the (unstarted) thread of one goroutine
     *     around its body. Inside a polyglot context these are polyglot
     *     threads (graalgo.language.GoContext); the two-argument constructor,
     *     for embedders and tests without a context, follows
     *     {@link #carrierModeDefault()} with bare platform carriers or bare
     *     virtual threads.
     */
    public SchedulerCore(int gomaxprocs, long quantumNanos,
            Function<Runnable, Thread> goroutineThreads) {
        this(gomaxprocs, quantumNanos, goroutineThreads, null, false);
    }

    /**
     * @param threads builds unstarted threads: one per goroutine in thread mode, the carriers
     *     in carrier mode
     * @param carrierSlots in carrier mode inside a context: the calling carrier's context
     *     thread-local slot (null outside a context)
     * @param carrierMode run goroutines as continuations on carriers ({@link #carriers})
     */
    public SchedulerCore(int gomaxprocs, long quantumNanos, Function<Runnable, Thread> threads,
            java.util.function.Supplier<graalgo.language.GoThreadSlot> carrierSlots, boolean carrierMode) {
        this.goroutineThreads = threads;
        this.requestedCap = Math.max(1, gomaxprocs);
        this.carrierParallelism = Math.max(1, Runtime.getRuntime().availableProcessors());
        if (carrierMode && !CARRIER_MODE_AVAILABLE) {
            throw new IllegalStateException("graalgo.sched: carrier mode (the default) needs the JVM "
                    + "option --add-exports java.base/jdk.internal.vm=ALL-UNNAMED; "
                    + "-Dgraalgo.sched.goroutines=thread selects one thread per goroutine instead");
        }
        this.carriers = carrierMode ? new Carriers(this, carrierParallelism, threads, carrierSlots) : null;
        // P2-D6: effective cap = min(GOMAXPROCS, carrier parallelism).
        this.cap = Math.min(requestedCap, carrierParallelism);
        this.quantumNanos = quantumNanos;
        startPollTicker(quantumNanos);
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
        drainer.start();
        timerHeap.start();
    }

    /** The scheduler's timer heap (P2-M3, WS C). */
    public graalgo.runtime.TimerHeap timerHeap() {
        return timerHeap;
    }

    /**
     * Enter blocking I/O (P2-M3, GG-0063/GG-SCHED-0006 — v1 netpoller
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
        if (carriers != null) {
            // The goroutine stays on its carrier from here to ioExit (callers hold
            // thread-confined resources around the system call: confined native memory).
            Carriers.pinCurrent();
        }
        flushRunnext(h);
        long t = armPark(h);
        h.ioStartNanos = System.nanoTime();
        h.ioParkEvent = JfrEvents.ioParkBegin();
        drainer.offer(new ResumeToken(ResumeToken.Kind.IO_ENTER, h, t));
        awaitAck(h, t);
        if (carriers != null) {
            // The goroutine blocks its carrier from here to ioExit: a spare takes its place.
            carriers.beginBlocking();
            h.ioBlocking = true;
        }
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
        if (carriers != null) {
            carriers.endBlocking();
            h.ioBlocking = false;
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
        if (carriers != null) {
            Carriers.unpinCurrent();
        }
        graalgo.jfr.GoroutineParkEvent ioEvent = h.ioParkEvent;
        h.ioParkEvent = null;
        JfrEvents.ioParkEnd(h, ioEvent, System.nanoTime() - h.ioStartNanos);
    }

    /**
     * Ends an {@link #ioEnter} (complete or interrupted part-way) that no {@link #ioExit} will
     * follow: the goroutine is leaving a cancelled blocking call because the program is being
     * torn down (context cancel or exit, {@link #terminate}). Undoes the carrier-side state —
     * the pin and the spare-carrier count — without the admission handshake: nothing is
     * admitted any more. No-op when no ioEnter is pending.
     */
    public void ioAbandon() {
        GoroutineHandle h = currentHandle.get();
        if (h == null || !h.inIo.compareAndSet(true, false)) {
            return;
        }
        if (carriers != null) {
            if (h.ioBlocking) {
                h.ioBlocking = false;
                carriers.endBlocking();
            }
            Carriers.unpinCurrent();
        }
    }

    /**
     * The system-call layer's abort hook of this scheduler (graalgo.syscall's cancellable
     * blocking calls, ADR-0019), created by {@code create} on first use; {@link #terminate}
     * runs it (at once, if this scheduler is already terminated).
     */
    public Runnable syscallAborter(java.util.function.Supplier<? extends Runnable> create) {
        Runnable a = syscallAborter.get();
        if (a == null) {
            Runnable n = create.get();
            if (syscallAborter.compareAndSet(null, n)) {
                a = n;
                if (terminated) {
                    n.run();
                }
            } else {
                a = syscallAborter.get();
            }
        }
        return a;
    }

    private final java.util.concurrent.atomic.AtomicReference<Runnable> syscallAborter =
            new java.util.concurrent.atomic.AtomicReference<>();

    /** Whether the goroutine threads are polyglot threads of a context ({@link #bindContext}). */
    public boolean inContext() {
        return inContext;
    }

    Drainer drainer() {
        return drainer;
    }

    /**
     * Admission provenance summary (P2-M8): counts of WAKE-lane admissions
     * by cause, plus direct handoffs (admissions on the waker's slot).
     * Format: {@code direct=<n> stashed=<n> ioExit=<n> handoff=<n>}.
     * {@code stashed} counts PARKED tokens that found a pre-arrived UNPARK
     * (legitimate, but looks like "re-admission with no UNPARK in flight"
     * to a timeline observer — the M6 watch item).
     */
    public String wakeProvenance() {
        return "direct=" + drainer.wakeUnparkDirect.get()
                + " stashed=" + drainer.wakeUnparkStashed.get()
                + " ioExit=" + drainer.wakeIoExit.get()
                + " handoff=" + drainer.wakeHandoff.get();
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
        // The slot's quantum is spent: a deferred wake goes to the drainer
        // (its WAKE lane outranks this RESUME), never a handoff.
        flushRunnext(h);
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
        GoroutineHandle next = h.runnext;
        if (next != null && exiting) {
            // Process exit stops the world: no admission, handoffs included.
            flushRunnext(h);
            next = null;
        }
        if (next == null) {
            drainer.offer(new ResumeToken(ResumeToken.Kind.PARKED, h, t));
        } else {
            // Direct handoff (gc runnext): the goroutine this one just woke
            // runs next on this slot. Token first (the drainer then sees it
            // before any later token of next's), then next's admission flag,
            // then the unpark (ack-before-unpark, GG-WAKE-0001). next inherits
            // the slot's quantum (gc: runnext inherits the time slice), so a
            // ping-ponging pair yields the slot at the quantum like one
            // goroutine would (GG-SCHED-0001, starvation freedom).
            h.runnext = null;
            drainer.offer(ResumeToken.handoff(h, t, next));
            next.quantumDeadlineNanos = h.quantumDeadlineNanos;
            next.handoff.set(true);
            wake(next, true);
        }
        awaitAck(h, t, true, next != null);
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
        GoroutineHandle w = currentHandle.get();
        if (w == null || w == h || !HANDOFF) {
            // Not a goroutine of this scheduler (timer heap, netpoller,
            // cleaner, a test's platform thread): through the drainer.
            drainer.offer(new ResumeToken(ResumeToken.Kind.UNPARK, h, -1));
            return;
        }
        // A goroutine woke h: defer the wake as its runnext (gc ready():
        // the newest wake takes runnext, an older one goes to the queue).
        // If w parks before the handoff window closes it hands h its slot
        // (parkCurrent); otherwise flushRunnext releases h through the
        // drainer as before.
        GoroutineHandle prev = w.runnext;
        w.runnext = h;
        if (prev != null) {
            drainer.offer(new ResumeToken(ResumeToken.Kind.UNPARK, prev, -1));
        } else {
            w.runnextSinceNanos = System.nanoTime();
        }
    }

    /**
     * How long a goroutine may keep a deferred wake ({@link GoroutineHandle#runnext})
     * while it keeps running: its polls stay full meanwhile, and the first one past this
     * window releases the wake through the drainer. gc protects a P's runnext from
     * stealing for ~3 µs for the same reason (the waker usually blocks within
     * nanoseconds — a channel ping-pong, a request/response); the window is wider here
     * because the poll, not a timer, closes it. It only delays the woken goroutine,
     * never the waker, and only while the waker runs on.
     */
    static final long HANDOFF_WINDOW_NANOS = 20_000L;

    /**
     * Direct handoff on/off ({@code -Dgraalgo.sched.handoff=false} turns it off: every
     * wake goes through the drainer, as before WP-SCHED's handoff). For A/B measurement
     * on one build (performance.md) and as a diagnostic switch; on by default.
     */
    static final boolean HANDOFF = !"false".equals(System.getProperty("graalgo.sched.handoff"));

    /**
     * Releases a deferred wake through the drainer (ordinary UNPARK): on every
     * scheduler interaction of {@code h} other than a handoff park, so that every
     * waker offers its wakee's UNPARK before its own park/exit token (the deadlock
     * check's race-freedom argument, Drainer#detectDeadlock).
     */
    private void flushRunnext(GoroutineHandle h) {
        GoroutineHandle g = h.runnext;
        if (g != null) {
            h.runnext = null;
            drainer.offer(new ResumeToken(ResumeToken.Kind.UNPARK, g, -1));
        }
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
     * Starts a section in which the calling goroutine is not preempted (gc's {@code acquirem}:
     * a goroutine holding a runtime lock runs with preemption off). For runtime code that holds
     * a Java monitor across guest code: in carrier mode a quantum park is a continuation yield,
     * and the JDK freezes a continuation together with the monitors it holds, so every carrier
     * that then contends for the monitor blocks (BLOCKED, not parked: it is never given back)
     * while the frozen owner waits for one of them to run it again — BUG-0054. The section's
     * code must not park in any other way; the deferred quantum park happens at the first poll
     * after {@link #enablePreemption} (the deadline stays expired). No-op off goroutines.
     */
    public void disablePreemption() {
        GoroutineHandle h = currentHandle.get();
        if (h != null) {
            h.preemptOff++;
        }
    }

    /** Ends a {@link #disablePreemption} section. */
    public void enablePreemption() {
        GoroutineHandle h = currentHandle.get();
        if (h != null) {
            h.preemptOff--;
        }
    }

    /**
     * Quantum check for the calling thread. No handle (not a scheduler
     * vthread) &rarr; no-op, so scheduler-less Phase-1 runs are
     * unaffected even with the replacement Safepoint on the classpath.
     */
    public void pollCurrentThread() {
        pollIfGoroutine();
    }

    /**
     * {@link #pollCurrentThread}; null when the caller is not one of this
     * scheduler's goroutines, else its handle.
     */
    private GoroutineHandle pollIfGoroutine() {
        GoroutineHandle h = currentHandle.get();
        if (h == null) {
            return null;
        }
        // fix-redmaster-r43: never yield inside a pin section. In gc,
        // pinning disables preemption, so a pinned goroutine is not
        // migrated mid-section; here a quantum park inside a section
        // would park while holding PinLock, stalling every other Pool
        // operation (and risking a missed re-admission against the
        // drainer). Pin sections are short and non-blocking, so the
        // yield simply moves to the first poll after unpin — the
        // deadline stays expired, so no quantum is lost.
        if (h.preemptOff > 0 || PinLock.isHeldByCurrentThread()) {
            return h;
        }
        if (exiting) {
            if (h == fatalOwner) {
                // The fatal reporter prints with the world stopped (Go
                // Error()/String() already ran, world running, before it
                // claimed the report): it must never quantum-park (no
                // admission happens any more), so it runs to completion.
                return h;
            }
            // Process exit in progress (main returned, os.Exit, fatal
            // panic/deadlock): freeze this goroutine at its safepoint so
            // it produces no further output while the launcher exits
            // (gc: exit/fatalpanic stop the world first). After the pin
            // check, so a frozen goroutine never holds PinLock (the
            // reporter's fmt calls pin sync.Pool).
            freezeForExit();
        }
        long now = System.nanoTime();
        if (h.runnext != null && now - h.runnextSinceNanos >= HANDOFF_WINDOW_NANOS) {
            // The waker kept running past the handoff window: release the
            // deferred wake through the drainer (it may run elsewhere now).
            flushRunnext(h);
        }
        if (now >= h.quantumDeadlineNanos) {
            quantumPark(h);
        }
        return h;
    }

    private void awaitAck(GoroutineHandle h, long t) {
        awaitAck(h, t, false, false);
    }

    /** Upper bound of one {@link #spinForHandoff}. */
    static final long HANDOFF_SPIN_NANOS = 5_000L;

    /** Goroutines spinning in {@link #spinForHandoff} right now. */
    private final AtomicInteger handoffSpinners = new AtomicInteger();

    /**
     * After handing its slot to the goroutine it woke, a goroutine waits briefly on CPU for
     * the answer before it unmounts: in a request/response exchange (channel ping-pong) the
     * partner answers within a few µs with a handoff back, and a mounted waiter takes it
     * without a virtual-thread park and remount (measured: 2000 Java-level round trips
     * 9–16 ms → 2.2–2.9 ms median on a loaded host). The waiter holds no slot meanwhile (it
     * gave it away) but does hold a CPU, so — like gc, whose Ms spin only while Ps are idle —
     * it spins only while the drainer last saw idle slots, never more spinners than idle
     * slots, and never longer than {@link #HANDOFF_SPIN_NANOS}.
     */
    private void spinForHandoff(GoroutineHandle h, long t) {
        int n = handoffSpinners.incrementAndGet();
        try {
            if (n > drainer.freeSlots()) {
                return;
            }
            long start = System.nanoTime();
            int i = 0;
            while (h.acked.get() < t && !h.handoff.get()) {
                Thread.onSpinWait();
                if ((++i & 15) == 0 && System.nanoTime() - start >= HANDOFF_SPIN_NANOS) {
                    return;
                }
            }
        } finally {
            handoffSpinners.decrementAndGet();
        }
    }

    /**
     * Waits for admission of park generation {@code t}: the drainer's ack, or —
     * for a {@link #parkCurrent} ({@code handoffOk}) — a waker's direct handoff
     * ({@link GoroutineHandle#handoff}). A handoff targets the park its waker
     * dequeued, which is always this goroutine's pending parkCurrent (wakers
     * reach only goroutines that enqueued themselves and then park), so only
     * parkCurrent consumes the flag.
     */
    private void awaitAck(GoroutineHandle h, long t, boolean handoffOk, boolean spinForHandoff) {
        // Early unpark is absorbed by the LockSupport permit; spurious
        // wakeups re-park on the acked < t condition. No wakeup is lost
        // because the drainer (or the handing-off waker) sets acked (the
        // handoff flag) BEFORE unparking (GG-WAKE-0001).
        if (spinForHandoff && carriers == null && h.acked.get() < t && !h.handoff.get()) {
            // Carrier mode never spins: the goroutine handed off to runs next on this carrier
            // once this one yields.
            spinForHandoff(h, t);
        }
        while (h.acked.get() < t) {
            if (handoffOk && h.handoff.get() && h.handoff.compareAndSet(true, false)) {
                break;
            }
            if (terminated) {
                throw TERMINATED;
            }
            parkSelf(h);
        }
        // P2-M6: commit the park event (if long enough) on resume. The
        // initial admission has no parked reason and emits nothing; the
        // goroutine-start event is emitted explicitly by spawn().
        JfrEvents.parkResumed(h);
    }

    /** Parks the calling goroutine until {@link #wake} (spurious returns possible). */
    private void parkSelf(GoroutineHandle h) {
        if (carriers != null) {
            carriers.park(h);
        } else if (inContext) {
            TruffleSafepoint.setBlockedThreadInterruptible(null, Carriers.PARK_INTERRUPTIBLY, this);
        } else {
            LockSupport.park(this);
        }
    }

    /**
     * Unparks goroutine {@code h} (after its admission was published: GG-WAKE-0001).
     * {@code handoff}: the caller is a goroutine handing {@code h} its slot right before it
     * parks.
     */
    void wake(GoroutineHandle h, boolean handoff) {
        if (carriers != null) {
            carriers.unpark(h, handoff);
        } else {
            LockSupport.unpark(h.thread);
        }
    }

    /** Carrier mount: the goroutine's thread-local view (Carriers#mount). */
    void mountLocals(GoroutineHandle h) {
        currentHandle.set(h);
        GOROUTINE_SCHEDULER.set(this);
    }

    /** Carrier unmount. */
    void unmountLocals() {
        currentHandle.set(null);
        GOROUTINE_SCHEDULER.set(null);
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
        Runnable run = () -> {
            if (carriers == null) {
                currentHandle.set(h);
                GOROUTINE_SCHEDULER.set(this);
                h.thread = Thread.currentThread();
            }
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
                    mainReturned();
                }
            } catch (GoroutineTerminated gt) {
                // Torn down by terminate(): the program is over.
            } catch (graalgo.runtime.PanicException pe) {
                try {
                    recordPanic(pe);
                } catch (GoroutineTerminated gt) {
                    // Torn down while rendering the report.
                }
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
                if (tornDown.getAsBoolean()) {
                    // The polyglot layer cancels or hard-exits the context (Context.close(true)):
                    // its exception ends this goroutine; the context's teardown stops the rest.
                    // Not a crash, and it must not report one or request an exit. It can end a
                    // goroutine anywhere — parked, slot released (a Truffle-blocked park) — so
                    // admission accounting is over: the drainer stops before this EXIT.
                    markExiting();
                    drainer.stop();
                } else {
                    // Any other JVM error (AssertionError, OutOfMemoryError,
                    // ...): the program can no longer be trusted — loud exit,
                    // never a silently dead goroutine and a hung process.
                    recordCrash("internal error", e);
                }
            } finally {
                // Unwound by a teardown from inside a pin section (no deferred procUnpin ran):
                // the lock belongs to this thread and must not outlive the goroutine.
                PinLock.releaseAllHeldByCurrentThread();
                // A wake this goroutine deferred is released before its EXIT
                // (the drainer must see the wakee's UNPARK first).
                flushRunnext(h);
                // P2-M6: every start gets an end, before the EXIT token.
                JfrEvents.goroutineEnd(h, System.nanoTime() - h.startNanos);
                GoroutineState.restore(prev);
                if (carriers == null) {
                    currentHandle.remove();
                    GOROUTINE_SCHEDULER.remove();
                }
                if (!h.system) {
                    liveUser.decrementAndGet();
                }
                synchronized (quiescenceLock) {
                    if (carriers != null) {
                        unlinkLive(h);
                    }
                    if (liveGoroutines.decrementAndGet() == 0) {
                        quiescenceLock.notifyAll();
                    }
                }
                drainer.offer(new ResumeToken(ResumeToken.Kind.EXIT, h, -1));
            }
        };
        if (carriers != null) {
            // A continuation, parked until the drainer's first admission unparks it: no
            // thread is created, so the cost does not grow with the live goroutine count.
            carriers.prepare(h, run);
            if (!system) {
                liveUser.incrementAndGet();
            }
        } else {
            Thread vthread = goroutineThreads.apply(run);
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
        }
        synchronized (quiescenceLock) {
            if (carriers != null) {
                linkLive(h);
            }
            liveGoroutines.incrementAndGet();
        }
        drainer.offer(new ResumeToken(ResumeToken.Kind.INIT, h, t));
        return h;
    }

    /** Adds {@code h} to {@link #liveList}; quiescenceLock held. */
    private void linkLive(GoroutineHandle h) {
        GoroutineHandle head = liveList;
        h.liveNext = head;
        if (head != null) {
            head.livePrev = h;
        }
        liveList = h;
    }

    /** Removes {@code h} from {@link #liveList}; quiescenceLock held. */
    private void unlinkLive(GoroutineHandle h) {
        GoroutineHandle prev = h.livePrev;
        GoroutineHandle next = h.liveNext;
        if (prev != null) {
            prev.liveNext = next;
        } else {
            liveList = next;
        }
        if (next != null) {
            next.livePrev = prev;
        }
        h.livePrev = null;
        h.liveNext = null;
    }

    private final AtomicInteger panicCount = new AtomicInteger();
    private final AtomicInteger crashCount = new AtomicInteger();

    /**
     * An uncaught Go panic is fatal to the whole process, in any
     * goroutine (gc {@code gopanic} → {@code preprintpanics} →
     * {@code fatalpanic}): render the panic chain, then print it and this
     * goroutine's traceback and exit 2 — other goroutines are frozen,
     * never awaited.
     *
     * <p>Rendering runs Go {@code Error()}/{@code String()} methods with
     * the world still running, exactly like gc's {@code preprintpanics}:
     * they may sleep, lock, use channels and other goroutines. Only once
     * the text exists does the fatal report claim the process
     * ({@link #beginFatal}) and stop the world. A method that panics
     * yields gc's {@code fatal error: panic while printing panic value};
     * every path ends in {@link #requestExit} (no hang).
     */
    private void recordPanic(graalgo.runtime.PanicException pe) {
        panicCount.incrementAndGet();
        synchronized (lifecycleLock) {
            if (exiting) {
                return; // exit/another fatal already owns the process
            }
            panicsRendering++;
        }
        String header = null;
        String throwText = null;
        Throwable crash = null;
        try {
            header = graalgo.runtime.PanicRendering.renderPanicChain(pe);
        } catch (graalgo.runtime.PanicException inner) {
            throwText = "panic while printing panic value: " + panicWhilePrintingValue(inner);
        } catch (graalgo.runtime.Goexit g) {
            // gc's preprintpanics recovers nothing from a Goexit and its
            // runtime then crashes ("fatal error: unknown pc"); graalgo
            // reports the cause instead (still a fatal error, exit 2).
            throwText = "panic while printing panic value: runtime.Goexit called";
        } catch (GoroutineTerminated gt) {
            throw gt;
        } catch (RuntimeException | StackOverflowError e) {
            crash = e;
        } finally {
            synchronized (lifecycleLock) {
                panicsRendering--;
            }
        }
        if (crash != null) {
            recordCrash("internal error", crash);
            return;
        }
        if (!beginFatal()) {
            return; // exit/another fatal won while rendering: nothing prints
        }
        StringBuilder sb = new StringBuilder();
        if (throwText != null) {
            // gc throw(): "fatal error: <text>", then the traceback.
            sb.append("fatal error: ").append(throwText).append('\n');
        } else {
            sb.append(header);
            Object v = pe.goValue();
            if (v instanceof graalgo.runtime.GoErrorValue e && e.signalSegv()) {
                sb.append("[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x0]\n");
            }
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

    /**
     * gc preprintpanics' recover: a {@code string} value prints as is,
     * anything else as {@code type <dynamic type>}.
     */
    private static String panicWhilePrintingValue(graalgo.runtime.PanicException inner) {
        Object v = inner.goValue();
        Object d = inner.descriptor();
        graalgo.values.GoKind kind = d instanceof graalgo.values.GoTypeDescriptor td ? td.kind() : null;
        boolean plainString = inner.goTypeName() == null
                && (kind == graalgo.values.GoKind.STRING || kind == graalgo.values.GoKind.UNTYPED_STRING
                    || (kind == null && v instanceof graalgo.values.GoString));
        if (plainString && v instanceof graalgo.values.GoString gs) {
            return new String(gs.bytes(), java.nio.charset.StandardCharsets.UTF_8);
        }
        String type = null;
        if (v instanceof graalgo.runtime.GoErrorValue e) {
            type = e.typeName();
        }
        if (type == null) {
            type = inner.goTypeName();
        }
        if (type == null && d instanceof graalgo.values.GoTypeDescriptor td) {
            type = td.name();
        }
        return "type " + (type != null ? type : "?");
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
     * Exit epoch for the strided {@code Safepoint.poll()} fast path (one
     * static volatile read per poll): bumped whenever ANY scheduler begins
     * process exit. A poll skips the full quantum check for up to
     * {@code STRIDE - 1} polls only while the epoch equals the one its
     * thread saw at its last full poll, so a pending exit freezes a
     * running goroutine at its very next safepoint (no extra output after
     * exit/fatal begins) — whichever scheduler it belongs to and whether
     * or not that scheduler is still installed. Pre-fix this was a flag
     * mirroring only the INSTALLED scheduler's {@link #exiting}, cleared
     * by {@link #install}: a goroutine whose scheduler exited and was then
     * uninstalled (a test harness, an embedder's finished context) ran up
     * to {@code STRIDE - 1} more polls (LifecycleTest
     * testExitFreezesAfterUninstall). Another scheduler's exit costs a
     * running program's threads one full poll each, never the slow path
     * for good; a thread whose own scheduler is exiting but whose poll
     * returned (pin section, fatal reporter) re-polls in full every time
     * ({@link #pollCurrentThreadStatic}'s result). Written under the class
     * lock, so concurrent exits never lose a bump.
     */
    private static volatile int exitEpoch;

    /** The current exit epoch (see the field). */
    public static int exitEpoch() {
        return exitEpoch;
    }

    /**
     * The poll epoch (WP-POLLCOST): the only shared word {@code Safepoint.poll}
     * reads on its fast path. A poll takes the full quantum check
     * ({@link #pollCurrentThreadStatic}) only when the epoch differs from the one its thread saw
     * at its last full poll, so the fast path is one load and one compare — no store, no
     * per-poll countdown. The epoch moves (a) every {@link #pollTickNanos} on the
     * {@code graalgo-poll-ticker} daemon thread, so a running goroutine checks its quantum
     * deadline at its first poll after each tick: quantum expiry is noticed at most one tick
     * late (the tick is at most a quarter of the smallest quantum, GG-SCHED-0001); and (b) in
     * {@link #markExiting}, together with {@link #exitEpoch}, so a pending exit still freezes a
     * running goroutine at its very next poll. Volatile so that a compiled loop re-reads it on
     * every back-edge (a plain read could be hoisted out of a loop whose full-poll branch was
     * never taken) and so a tick is seen promptly; on x86 the read is a plain load.
     */
    private static volatile int pollEpoch;

    /** The current poll epoch (see the field). */
    public static int pollEpoch() {
        return pollEpoch;
    }

    /** The upper bound of the poll tick: 1 ms. */
    static final long MAX_POLL_TICK_NANOS = 1_000_000L;
    /** The lower bound of the poll tick: 50 µs. */
    static final long MIN_POLL_TICK_NANOS = 50_000L;
    /** The tick period: a quarter of the smallest quantum seen, clamped (guarded by the class). */
    private static long pollTickNanos = MAX_POLL_TICK_NANOS;
    private static Thread pollTicker;

    /** The current tick period, nanoseconds. */
    public static synchronized long pollTickNanos() {
        return pollTickNanos;
    }

    /**
     * Starts the poll ticker on first use and shortens its period to a quarter of
     * {@code quantumNanos} (clamped to [{@link #MIN_POLL_TICK_NANOS}, {@link
     * #MAX_POLL_TICK_NANOS}]) when that is shorter. The ticker is a daemon platform thread
     * that lives as long as the JVM: one wake-up per tick (≈1,000/s at the default quantum)
     * whether or not goroutines run, which is the price of a store-free poll.
     */
    private static synchronized void startPollTicker(long quantumNanos) {
        long tick = Math.max(MIN_POLL_TICK_NANOS, Math.min(MAX_POLL_TICK_NANOS, quantumNanos / 4));
        if (tick < pollTickNanos) {
            pollTickNanos = tick;
        }
        if (pollTicker != null) {
            return;
        }
        pollTicker = Thread.ofPlatform().daemon(true).name("graalgo-poll-ticker").unstarted(() -> {
            while (true) {
                java.util.concurrent.locks.LockSupport.parkNanos(pollTickNanos());
                bumpPollEpoch();
            }
        });
        pollTicker.start();
    }

    private static synchronized void bumpPollEpoch() {
        pollEpoch++;
    }

    /**
     * Sets {@link #exiting}, then bumps the fast-path {@link #exitEpoch} —
     * once per scheduler: later calls (teardown of an exited scheduler)
     * leave the epoch, and so every other thread's fast path, alone.
     */
    private void markExiting() {
        if (exiting) {
            return;
        }
        synchronized (SchedulerCore.class) {
            if (!exiting) {
                exiting = true;
                exitEpoch++;
                pollEpoch++;
            }
        }
    }
    /** First fatal reporter wins the right to print (gc: one fatal trace). */
    private final java.util.concurrent.atomic.AtomicBoolean fatal =
            new java.util.concurrent.atomic.AtomicBoolean();
    /** The goroutine printing the fatal report (exempt from the freeze). */
    private volatile GoroutineHandle fatalOwner;
    /**
     * Guards the exit decision: {@link #exiting}, the fatal claim and
     * {@link #panicsRendering} change together under it, so a goroutine
     * panic and main returning (or os.Exit) agree on one outcome. A leaf
     * lock (taken with the drainer lock held, never the reverse).
     */
    private final Object lifecycleLock = new Object();
    /**
     * Uncaught panics whose report is being rendered (gc
     * {@code runningPanicDefers} for the preprintpanics phase).
     */
    private int panicsRendering;
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

    // ---- finalizers as possible wakers (gc checkdead vs. the finalizer queue) ----

    /**
     * Finalizers registered with this program and neither cancelled nor
     * dispatched: their object may be (or become) collected, and the JDK
     * Cleaner thread — outside the scheduler — then spawns the finalizer
     * goroutine, which may wake a parked goroutine. gc queues finalizers
     * synchronously during the GC that finds them; here the Cleaner runs
     * shortly after, so a deadlock check must give it that moment.
     */
    private final AtomicInteger pendingFinalizers = new AtomicInteger();
    /** Finalizers dispatched so far (monotonic); guarded by its monitor for waits. */
    private final java.util.concurrent.atomic.AtomicLong finalizerEpoch =
            new java.util.concurrent.atomic.AtomicLong();
    /** Epoch for which a settle wait already ran (drainer thread only). */
    private long settledFinalizerEpoch = -1;
    /** Bound on one settle wait for the Cleaner (deadlock report delay). */
    private static final long FINALIZER_SETTLE_MILLIS = 1000;

    /** A finalizer was registered (runtime.SetFinalizer). */
    public void finalizerRegistered() {
        pendingFinalizers.incrementAndGet();
    }

    /** A pending finalizer was removed (SetFinalizer(x, nil)). */
    public void finalizerCancelled() {
        pendingFinalizers.decrementAndGet();
    }

    /** The Cleaner dispatched a finalizer (its goroutine is spawned, if any). */
    public void finalizerDispatched() {
        pendingFinalizers.decrementAndGet();
        synchronized (finalizerEpoch) {
            finalizerEpoch.incrementAndGet();
            finalizerEpoch.notifyAll();
        }
        pokeScheduler();
    }

    /**
     * Drainer side of the finalizer waker (lock held): when finalizers are
     * pending and no settle wait ran for the current dispatch epoch, start
     * one — a bounded wait for the Cleaner to dispatch, then a drainer
     * poke — and suppress this report. A dispatch spawns a goroutine (no
     * longer all parked); if none comes, the re-check reports the
     * deadlock. A genuine deadlock with live finalizable objects is thus
     * reported, at most {@link #FINALIZER_SETTLE_MILLIS} late.
     */
    private boolean deferForFinalizers() {
        if (pendingFinalizers.get() <= 0) {
            return false;
        }
        long epoch = finalizerEpoch.get();
        if (settledFinalizerEpoch == epoch) {
            return false;
        }
        settledFinalizerEpoch = epoch;
        Thread.ofPlatform().daemon(true).name("graalgo-finalizer-settle").start(() -> {
            long deadline = System.nanoTime() + FINALIZER_SETTLE_MILLIS * 1_000_000L;
            synchronized (finalizerEpoch) {
                while (finalizerEpoch.get() == epoch && !exiting) {
                    long left = (deadline - System.nanoTime()) / 1_000_000L;
                    if (left <= 0) {
                        break;
                    }
                    try {
                        finalizerEpoch.wait(left);
                    } catch (InterruptedException e) {
                        return;
                    }
                }
            }
            pokeScheduler();
        });
        return true;
    }

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

    /**
     * Claims the single fatal-report slot and stops the world; false if
     * another fatal won or process exit already began (gc: the first of
     * exit/fatalpanic owns the process).
     */
    private boolean beginFatal() {
        synchronized (lifecycleLock) {
            if (exiting || !fatal.compareAndSet(false, true)) {
                return false;
            }
            // The reporting goroutine keeps running to print; everyone
            // else freezes at its next safepoint.
            fatalOwner = currentHandle.get();
            markExiting();
            return true;
        }
    }

    /**
     * Main returned (gc {@code runtime.main}): if another goroutine is
     * rendering a panic report, yield to it a bounded number of times
     * (gc: up to 1000 {@code Gosched} while {@code runningPanicDefers});
     * if a fatal report then owns the process, park forever (gc
     * {@code waitReasonPanicWait}) so the exit status is the report's 2,
     * never 0 with panic output; otherwise exit 0.
     */
    private void mainReturned() {
        for (int i = 0; i < 1000; i++) {
            synchronized (lifecycleLock) {
                if (panicsRendering == 0 || exiting) {
                    break;
                }
            }
            yieldCurrent();
        }
        synchronized (lifecycleLock) {
            if (!fatal.get()) {
                if (!exiting) {
                    naturalExit = true;
                }
                requestExit(0);
                return;
            }
        }
        freezeForExit();
    }

    /**
     * Requests process exit with {@code code} (main returned: 0; uncaught
     * panic / deadlock: 2; {@code os.Exit(n)}: n). The first request
     * wins; the launcher's {@link #awaitExit} returns it. Admissions stop
     * immediately and running goroutines freeze at their next safepoint.
     */
    public void requestExit(int code) {
        synchronized (lifecycleLock) {
            markExiting();
            exitCode.complete(code);
        }
    }

    /** True when the calling thread is a scheduler goroutine. */
    public boolean onGoroutine() {
        return currentHandle.get() != null;
    }

    /** Set when the first exit request is main returning (not os.Exit, a panic or a fatal). */
    private volatile boolean naturalExit;

    /**
     * Whether the program's exit was {@code main} returning — a natural end, as opposed to a
     * process exit ({@code os.Exit}, an uncaught panic, a fatal error). Valid once
     * {@link #awaitExit} returned.
     */
    public boolean exitedNaturally() {
        return naturalExit;
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
        markExiting();
        drainer.stop();
        timerHeap.stop();
        Runnable aborter = syscallAborter.get();
        if (aborter != null) {
            // Goroutines waiting for a blocking system call leave now (ADR-0019).
            aborter.run();
        }
        long deadline = System.nanoTime() + timeoutMillis * 1_000_000L;
        if (carriers != null) {
            return terminateCarriers(deadline);
        }
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
     * {@link #terminate} in carrier mode: every live goroutine is unparked and unwinds at its
     * park; then the carriers stop. Returns the carriers still alive at {@code deadline} (one
     * blocked under a goroutine in a system call).
     */
    private java.util.List<Thread> terminateCarriers(long deadline) throws InterruptedException {
        java.util.List<GoroutineHandle> live = new java.util.ArrayList<>();
        synchronized (quiescenceLock) {
            for (GoroutineHandle h = liveList; h != null; h = h.liveNext) {
                live.add(h);
            }
        }
        for (GoroutineHandle h : live) {
            carriers.unpark(h, false);
        }
        synchronized (quiescenceLock) {
            long left;
            while (liveGoroutines.get() != 0 && (left = (deadline - System.nanoTime()) / 1_000_000L) > 0) {
                quiescenceLock.wait(left);
            }
        }
        java.util.List<Thread> stuck = new java.util.ArrayList<>();
        for (Thread t : carriers.stop()) {
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
        GoroutineHandle h = currentHandle.get();
        if (h == null) {
            return;
        }
        for (;;) {
            if (terminated) {
                throw TERMINATED;
            }
            parkSelf(h);
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
            if (deferForFinalizers() || !beginFatal()) {
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
        if (deferForFinalizers() || !beginFatal()) {
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
