package graalgo.language;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.TruffleContext;
import com.oracle.truffle.api.TruffleSafepoint;
import com.oracle.truffle.api.TruffleLanguage.ContextReference;
import com.oracle.truffle.api.TruffleLanguage.Env;
import com.oracle.truffle.api.nodes.Node;
import graalgo.frontend.NodeFactory;
import graalgo.launcher.OsSurfaceInitializer;
import graalgo.sched.SchedulerCore;
import java.io.File;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.ServiceLoader;
import java.util.function.Function;

/**
 * One Go program run inside a polyglot context (truffle-design §2.3–2.6).
 *
 * <p>Owns the {@link Env} and runs the program: os surface, scheduler with
 * goroutine threads built through the polyglot thread builder, {@code main}
 * as the first goroutine, the exit status. The runtime's process-global
 * statics (the installed scheduler, type/itab tables, GoOs) stay where they
 * are until TR-5 (§2.4); {@link #activate} records the one live context for
 * the static helpers that need to know whether they run inside it.
 */
public final class GoContext {
    private static final ContextReference<GoContext> REFERENCE =
            ContextReference.create(GoLanguage.class);

    /** The live context (policy EXCLUSIVE: at most one per process in practice). */
    private static volatile GoContext active;

    /** The scheduler of the last run, torn down when the context closes. */
    private volatile SchedulerCore scheduler;

    /**
     * The context whose {@link #run} executes a program right now, or
     * null. Claimed and released under the class lock ({@link #claimRun}),
     * so two contexts (or two threads evaluating one context) can never
     * run programs concurrently on the process-global runtime state.
     */
    private static GoContext runningContext;

    final GoLanguage language;
    final Env env;
    private final TruffleContext truffleContext;
    private final boolean virtualThreads;
    /** Goroutines are continuations on carriers (ADR-0018), not one thread each. */
    private final boolean carrierMode;

    GoContext(GoLanguage language, Env env) {
        this.language = language;
        this.env = env;
        this.truffleContext = env.getContext();
        // Unset: carrier mode (GG-0074), or -Dgraalgo.sched.goroutines=thread (the opt-in thread mode).
        String threads = env.getOptions().hasBeenSet(GoLanguage.GoroutineThreads)
                ? env.getOptions().get(GoLanguage.GoroutineThreads)
                : SchedulerCore.carrierModeDefault() ? "carrier" : "virtual";
        switch (threads) {
            case "carrier" -> {
                carrierMode = true;
                virtualThreads = false;
            }
            case "thread", "virtual" -> {
                carrierMode = false;
                virtualThreads = true;
            }
            case "platform" -> {
                carrierMode = false;
                virtualThreads = false;
            }
            default -> throw new IllegalArgumentException("go.GoroutineThreads must be 'carrier', "
                    + "'thread', 'virtual' or 'platform', got '" + threads + "'");
        }
    }

    public static GoContext get(Node node) {
        return REFERENCE.get(node);
    }

    /**
     * Makes {@code context} the live one. Until TR-5 the scheduler, type
     * tables and os surface are process-global, so a second context may not
     * start while another runs a program (a finished one is replaced: the
     * launchers never close theirs, see P3SyncMain).
     */
    static synchronized void activate(GoContext context) {
        if (runningContext != null && runningContext != context) {
            throw oneProgramAtATime();
        }
        active = context;
    }

    private static IllegalStateException oneProgramAtATime() {
        return new IllegalStateException("graalgo runs one Go program per process at a "
                + "time until its runtime state is per-context (truffle-design TR-5); "
                + "another Go program is running");
    }

    /**
     * Atomically claims the single program slot for {@code context} and
     * makes it the live one; fails if any program (another context's, or
     * a concurrent eval of this one) is running. {@link #activate} alone
     * is not enough: it runs once, at context initialization, so two
     * contexts initialized before either evaluates would both pass it.
     */
    private static synchronized void claimRun(GoContext context) {
        if (runningContext != null) {
            throw oneProgramAtATime();
        }
        runningContext = context;
        active = context;
    }

    private static synchronized void releaseRun(GoContext context) {
        if (runningContext == context) {
            runningContext = null;
        }
    }

    static synchronized void deactivate(GoContext context) {
        if (active == context) {
            active = null;
        }
    }

    /** The live Go context, or {@code null} outside any (unit tests, legacy launchers). */
    public static GoContext activeOrNull() {
        return active;
    }

    /** Is a Go context entered on the current thread? */
    public static boolean isEnteredOnCurrentThread() {
        GoContext c = active;
        return c != null && c.isEntered();
    }

    /**
     * Context close (truffle-design §2.3): goroutines the program left
     * behind — frozen by exit, blocked forever — are torn down so the
     * polyglot layer finds every goroutine thread finished. A goroutine
     * that cannot be stopped (in a blocking system call) is reported.
     */
    void finalizeContext() {
        terminatePrevious();
    }

    /** Tears down the last run's scheduler, if any (see {@link #finalizeContext}). */
    private void terminatePrevious() {
        SchedulerCore sched = scheduler;
        if (sched == null) {
            return;
        }
        scheduler = null;
        List<Thread> stuck;
        try {
            stuck = sched.terminate(10_000);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            return;
        }
        if (!stuck.isEmpty()) {
            throw new IllegalStateException("cannot tear down the previous Go program: "
                    + "goroutine thread(s) " + stuck
                    + " did not stop (blocked outside the scheduler)");
        }
    }

    /** Is this context entered on the current thread? */
    public boolean isEntered() {
        return truffleContext.isEntered();
    }

    /**
     * Runs {@code body} entered in this context. For runtime threads that
     * are not polyglot threads but run Go code (the finalizer thread): the
     * code of language-bound roots reads the thread's context state.
     */
    @TruffleBoundary
    public void runEntered(Runnable body) {
        if (truffleContext.isEntered()) {
            body.run();
            return;
        }
        Object prev = truffleContext.enter(null);
        try {
            body.run();
        } finally {
            truffleContext.leave(null, prev);
        }
    }

    /** This thread's slot; the context must be entered on the current thread. */
    public GoThreadSlot threadSlot() {
        return language.threadSlot.get();
    }

    List<Path> sourceFiles() {
        String spec = env.getOptions().get(GoLanguage.SourceFiles);
        List<Path> files = new ArrayList<>();
        if (!spec.isEmpty()) {
            for (String f : spec.split(File.pathSeparator, -1)) {
                files.add(Path.of(f));
            }
        }
        return files;
    }

    /**
     * Goroutine carrier factory for the scheduler: polyglot threads, entered
     * in this context for their whole life (§2.6 item 2): a few platform
     * carriers (carrier mode, the default), else one virtual thread per
     * goroutine unless {@code go.GoroutineThreads=platform}.
     */
    private Function<Runnable, Thread> goroutineThreads() {
        if (carrierMode) {
            // Carriers: platform polyglot threads, a handful per program (Carriers).
            return body -> env.newTruffleThreadBuilder(body).virtual(false).build();
        }
        return body -> env.newTruffleThreadBuilder(body).virtual(virtualThreads).build();
    }

    /** A new scheduler for this context's goroutines. */
    private SchedulerCore newScheduler(int gomaxprocs) {
        SchedulerCore s = new SchedulerCore(gomaxprocs, SchedulerCore.DEFAULT_QUANTUM_NANOS, goroutineThreads(),
                carrierMode ? language.threadSlot::get : null, carrierMode);
        s.bindContext(this::closing);
        return s;
    }

    /**
     * Runs the program: os surface, scheduler, {@code main} as the first
     * goroutine; returns the exit status (T0.A contract: 0 main returned,
     * 2 uncaught panic / deadlock, n os.Exit(n), 1 internal crash). Other
     * goroutines are abandoned, as gc's exit abandons them.
     */
    @TruffleBoundary
    int run(NodeFactory.Program program) {
        claimRun(this);
        try {
            return runProgram(program);
        } finally {
            releaseRun(this);
        }
    }

    /** The bundle run of this context (package variables, scheduler adapter), or null. */
    private graalgo.go.runtime.GoProcess process;

    /** The program's argv ({@code os.Args}): {@code argv[0]} as invoked, then its arguments. */
    public String[] applicationArguments() {
        return env.getApplicationArguments();
    }

    /** The running bundle's context-level state; only valid while a bundle runs. */
    public graalgo.go.runtime.GoProcess process() {
        return process;
    }

    /**
     * Runs a linked bundle (toolexec-design §4.2) on the new node package: package
     * initialization in order, then {@code main.main} as the first goroutine; returns the exit
     * status with the same contract as {@link #run}. The os surface of v1 is not initialized:
     * a bundle reaches the OS only through its own packages.
     */
    @TruffleBoundary
    int runBundle(graalgo.go.loader.Program program) {
        if (!env.isNativeAccessAllowed()) {
            // Every system call of a bundle is a Panama downcall (graalgo.syscall.Executor).
            throw new IllegalStateException("the go language makes system calls through native "
                    + "downcalls: build the context with allowNativeAccess(true)");
        }
        claimRun(this);
        try {
            // As runProgram: tear a previous run's leftover goroutines down first.
            terminatePrevious();
            int gmp = env.getOptions().get(GoLanguage.Gomaxprocs);
            if (gmp <= 0) {
                gmp = Runtime.getRuntime().availableProcessors();
            }
            SchedulerCore sched = newScheduler(gmp);
            scheduler = sched;
            graalgo.go.runtime.GoProcess p =
                    new graalgo.go.runtime.GoProcess(program.newGlobals(), sched, env.err(), program.types);
            process = p;
            SchedulerCore prev = SchedulerCore.install(sched);
            sched.start();
            try {
                sched.spawnMain(() -> p.run(() -> program.run(p)));
                int status = awaitExit(sched);
                if (!sched.exitedNaturally()) {
                    hardExit(status);
                }
                return status;
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new IllegalStateException("interrupted while awaiting program exit", e);
            } finally {
                SchedulerCore.install(prev);
            }
        } finally {
            releaseRun(this);
        }
    }

    /**
     * Waits for the program's exit as a Truffle-blocked thread (playbook §10): an embedder's
     * {@code Context.close(true)} (or a hard exit) interrupts the wait and throws its
     * cancellation here. The scheduler is then torn down before the cancellation propagates —
     * parked goroutines unwind, running ones end at their next Truffle safepoint, and the
     * carriers (polyglot threads entered for their whole life) stop — because the polyglot
     * layer waits for every thread of the context to leave before it can finish closing.
     */
    private int awaitExit(SchedulerCore sched) throws InterruptedException {
        int[] status = new int[1];
        try {
            TruffleSafepoint.setBlockedThreadInterruptible(null, s -> status[0] = s.awaitExit(), sched);
        } catch (RuntimeException | Error e) {
            if (closing()) {
                terminatePrevious();
            }
            throw e;
        }
        return status[0];
    }

    /**
     * The program ended by a process exit other than {@code main} returning — {@code os.Exit(n)},
     * an uncaught panic or a fatal error (status 2) — which gc implements as {@code exit(2)}
     * of the process: in a polyglot context that is Truffle's hard exit (playbook §10). The
     * goroutines are already frozen by the exit (no further output); the scheduler is torn
     * down so the context's threads are gone, and {@link TruffleContext#closeExited} closes the
     * context: {@link GoLanguage#exitContext} runs on this thread, and the embedder's
     * {@code eval} ends in a {@code PolyglotException} whose {@code isExit()} holds and whose
     * {@code getExitStatus()} is {@code status}; the standalone launcher exits with it.
     */
    private void hardExit(int status) {
        terminatePrevious();
        truffleContext.closeExited(null, status);
        throw new IllegalStateException("TruffleContext.closeExited returned");
    }

    /**
     * {@link GoLanguage#exitContext}: a hard exit (ours, or one another language or the
     * embedder started) freezes the program first — no goroutine runs or prints after the
     * exit point, as in gc — then, in both modes, the context's output streams are flushed
     * (the runtime's own reports go through {@link Env#err()}). gc has no exit hooks; deferred
     * calls do not run on exit.
     */
    void exitContext(boolean hard, int status) {
        SchedulerCore sched = scheduler;
        if (hard && sched != null && !sched.isExiting()) {
            sched.requestExit(status);
        }
        try {
            env.out().flush();
            env.err().flush();
        } catch (java.io.IOException e) {
            // Nothing to report to: the streams are the embedder's and are closing.
        }
    }

    /** Whether the polyglot layer is cancelling or hard-exiting this context. */
    private boolean closing() {
        return truffleContext.isCancelling() || truffleContext.isExiting();
    }

    private int runProgram(NodeFactory.Program program) {
        // A previous program of this context exited (gc: its process is
        // gone): tear its leftover goroutines down before the next run,
        // or they stay parked forever and the context cannot close.
        terminatePrevious();
        initializeOsSurface(env.getApplicationArguments());
        int gmp = env.getOptions().get(GoLanguage.Gomaxprocs);
        if (gmp <= 0) {
            // gc's default: one P per CPU.
            gmp = Runtime.getRuntime().availableProcessors();
        }
        SchedulerCore sched = newScheduler(gmp);
        scheduler = sched;
        SchedulerCore prev = SchedulerCore.install(sched);
        sched.start();
        try {
            sched.spawnMain(() -> program.root().getCallTarget().call((com.oracle.truffle.api.nodes.Node) null));
            return awaitExit(sched);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("interrupted while awaiting program exit", e);
        } finally {
            SchedulerCore.install(prev);
        }
    }

    /**
     * (Re)initializes the Go os surface through the {@link OsSurfaceInitializer}
     * providers on the class path (interop's GoOs) before package init runs:
     * {@code os.Args} must already be visible to package-var initializers.
     */
    private static void initializeOsSurface(String[] argv) {
        boolean found = false;
        for (OsSurfaceInitializer initializer : ServiceLoader.load(OsSurfaceInitializer.class)) {
            found = true;
            initializer.initialize(argv);
        }
        if (!found) {
            throw new IllegalStateException("no OsSurfaceInitializer provider on the class "
                    + "path (the interop module): os.Args and the os surface would be empty");
        }
    }
}
