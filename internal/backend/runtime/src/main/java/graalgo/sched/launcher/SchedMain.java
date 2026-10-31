package graalgo.sched.launcher;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.List;
import java.util.ServiceLoader;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.Source;

import graalgo.frontend.FrontendException;
import graalgo.frontend.NodeFactory;
import graalgo.frontend.WireKindRegistrar;
import graalgo.sched.SchedulerCore;
import graalgo.sched.nodes.GoStatementNode;

/**
 * Phase-2 launcher: {@code graalgo-run --gmp=N file.go}.
 *
 * <p>Mirrors Phase-1 {@code graalgo.launcher.Main} (frontend, exit-code
 * contract) but executes {@code main} on the user-space scheduler
 * (P2-D1): a {@link SchedulerCore} is installed, the drainer started,
 * {@code main} spawned as the first goroutine, and the launcher thread
 * blocks until quiescence. The {@code "gostmt"} wire kind is registered
 * on the factory via the public {@code registerStmtKind} hook (as the
 * fallback when no provider is discovered), and the Phase-3 wire kinds
 * via the discovered {@link WireKindRegistrar} providers, exactly as
 * {@code graalgo.launcher.Main} does (see the fix-redmaster-r52 note at
 * the factory construction site).
 *
 * <p>Exit codes match Phase 1: 0 ok, 1 usage/input, 2 build failure or
 * uncaught Go panic (gc parity).
 */
public final class SchedMain {
    private SchedMain() {}

    static final int EXIT_OK = 0;
    static final int EXIT_INPUT_ERROR = 1;
    static final int EXIT_BUILD_FAILURE = 2;
    static final int EXIT_PANIC = 2;

    public static void main(String[] args) {
        System.exit(run(args));
    }

    /** Entry point without System.exit, for tests. */
    static int run(String[] args) {
        // P2-M5 (WS E): GODEBUG surface triage — parse and validate before
        // anything else. Unknown knobs, malformed values, and non-default
        // values of unsupported knobs are hard errors (never silently
        // ignored); gc fatals with exit status 2 on invalid GODEBUG.
        try {
            GoDebug.apply();
        } catch (GoDebug.GoDebugException e) {
            System.err.println("graalgo-run: fatal error: " + e.getMessage());
            return 2;
        }
        int gmp = 0; // 0 = default (carriers)
        boolean check = false;
        String file = null;
        for (String a : args) {
            if (a.startsWith("--gmp=")) {
                try {
                    gmp = Integer.parseInt(a.substring("--gmp=".length()));
                } catch (NumberFormatException e) {
                    return usageError("bad --gmp value: " + a);
                }
                if (gmp < 1) {
                    return usageError("--gmp must be >= 1");
                }
                continue;
            }
            switch (a) {
                case "--check" -> check = true;
                case "--help", "-h" -> {
                    usage();
                    return EXIT_OK;
                }
                default -> {
                    if (a.startsWith("-")) {
                        return usageError("unknown flag: " + a);
                    }
                    if (file != null) {
                        return usageError("too many input files");
                    }
                    file = a;
                }
            }
        }
        if (file == null) {
            return usageError("no input file");
        }
        if (gmp == 0) {
            gmp = Runtime.getRuntime().availableProcessors();
        }
        Path p = Paths.get(file);
        if (!Files.isReadable(p)) {
            System.err.println("graalgo-run: cannot read input file: " + file);
            return EXIT_INPUT_ERROR;
        }

        final String json;
        try {
            json = runGofrontend(p);
        } catch (GofrontendException e) {
            // gofrontend's stderr is the deterministic build log the harness records.
            // M10 (self-attack F11): a gofrontend killed by a signal leaves an
            // empty build log — the exit status is the only signal. Never fail
            // silently: report the exit code when there is no log.
            if (e.buildLog.isEmpty()) {
                System.err.println("graalgo-run: gofrontend exited " + e.exit
                        + " with no output");
            } else {
                System.err.print(e.buildLog);
            }
            return e.exit == 2 ? EXIT_BUILD_FAILURE : EXIT_INPUT_ERROR;
        } catch (IOException e) {
            System.err.println("graalgo-run: cannot launch gofrontend: " + e.getMessage());
            return EXIT_INPUT_ERROR;
        }

        final String srcText;
        try {
            srcText = Files.readString(p, StandardCharsets.UTF_8);
        } catch (IOException e) {
            System.err.println("graalgo-run: cannot read input file: " + file);
            return EXIT_INPUT_ERROR;
        }
        Source source = Source.newBuilder("go", srcText, p.getFileName().toString()).build();
        final NodeFactory.Program program;
        try {
            NodeFactory factory = new NodeFactory(source);
            // fix-redmaster-r52: Phase-3 wire kinds (ifacemethod, syncop,
            // ...) must be registered here exactly as in
            // graalgo.launcher.Main: real stdlib packages emit them, and
            // SchedMain is the launcher the runtime subprocess tests run
            // through (the snapshot diffharness instead uses
            // graalgo-sync-run/P3SyncMain). Without this, any program
            // touching fmt/runtime interfaces dies with "unsupported
            // expression kind ifacemethod". Discovered via ServiceLoader
            // — the same circular-dependency break as Main (runtime must
            // not compile against interop); the interop classes are on
            // the runtime test/product classpath.
            //
            // One-kind-one-registration: the provider table already
            // registers "gostmt" (P2-M1, same GoStatementNode builder), and
            // NodeFactory rejects duplicates. So the launcher's own
            // gostmt registration is only the fallback for a classpath
            // with no provider discovered (bare runtime classes) — never
            // registered alongside a provider.
            List<WireKindRegistrar> registrars = new ArrayList<>();
            for (WireKindRegistrar registrar
                    : ServiceLoader.load(WireKindRegistrar.class)) {
                registrars.add(registrar);
                registrar.registerAll(factory);
            }
            if (registrars.isEmpty()) {
                // P2-M1 (WS A): the go statement. No Phase-1 NodeFactory
                // change needed — public extension hook.
                factory.registerStmtKind("gostmt",
                        (node, ctx) -> GoStatementNode.build(node, ctx, factory));
            }
            program = factory.buildProgram(json);
            for (WireKindRegistrar registrar : registrars) {
                registrar.afterBuild(factory, program);
            }
        } catch (FrontendException e) {
            System.err.println("graalgo-run: internal error building AST: " + e.getMessage());
            return EXIT_INPUT_ERROR;
        }
        if (check) {
            return EXIT_OK;
        }

        // M10 (self-attack D3): preload all graalgo classes before guest
        // execution, so a guest that exhausts the process's fds cannot
        // break lazy class loading (notably on error-reporting paths).
        graalgo.launcher.ClassPreloader.preload();

        // Install the scheduler and run main as the first goroutine.
        assertOverlay();
        SchedulerCore sched = new SchedulerCore(gmp, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        SchedulerCore prev = SchedulerCore.install(sched);
        sched.start();
        final int[] exitCode = {EXIT_OK};
        try {
            sched.spawn(() -> {
                VirtualFrame frame = Truffle.getRuntime()
                        .createVirtualFrame(new Object[0], program.descriptor());
                try {
                    program.root().execute(frame);
                } catch (graalgo.runtime.Goexit g) {
                    // Main called Goexit: Go fatal deadlock iff no other
                    // goroutine is live; otherwise main just dies.
                    if (sched.runnableCount() > 1) {
                        return;
                    }
                    System.err.println(
                            "fatal error: all goroutines are asleep - deadlock!");
                    exitCode[0] = EXIT_PANIC;
                }
            });
            sched.awaitQuiescence();
        } catch (InterruptedException ie) {
            Thread.currentThread().interrupt();
            System.err.println("graalgo-run: interrupted while awaiting quiescence");
            return EXIT_INPUT_ERROR;
        } finally {
            SchedulerCore.install(prev);
        }
        if (sched.panicked()) {
            return EXIT_PANIC; // uncaught Go panic in some goroutine (gc: exit 2)
        }
        if (sched.crashed()) {
            return EXIT_INPUT_ERROR; // internal error, already reported on stderr
        }
        return exitCode[0];
    }

    /**
     * Fail-fast that the Phase-2 overlay is active (P2-D8, extended to
     * all eight shadows by R-02; same shape as Main and P3SyncMain):
     * every same-FQN shadow in {@code STUB_REL} (runtime Makefile) on
     * the classpath must be the Phase-2 replacement, i.e.
     * phase2/out/classes precedes phase1/out/classes. Each class
     * carries its own {@code isPhase2()} marker, absent on the Phase-1
     * copy, so each probe is a direct static call in its own try
     * block: a stub loaded first throws {@code NoSuchMethodError}
     * naming that class. With the wrong order the failure modes differ
     * per class and not all are loud: the Phase-1 {@code Safepoint}
     * is a silent no-op, the Phase-1 static {@code GoroutineState}
     * holder would be silently shared across goroutines, and
     * {@code ChanOps}/{@code GcInterop}/{@code Netpoll} throw UOE only
     * on first use of those paths. Never silent.
     */
    private static void assertOverlay() {
        try {
            if (!graalgo.runtime.ChanOps.isPhase2()) {
                throw new IllegalStateException(
                        "graalgo.runtime.ChanOps.isPhase2() returned false");
            }
        } catch (NoSuchMethodError e) {
            throw new IllegalStateException(
                    "FATAL: Phase-1 graalgo.runtime.ChanOps stub loaded instead of the Phase-2 "
                    + "overlay; phase2/out/classes must precede phase1/out/classes "
                    + "on the classpath",
                    e);
        }
        try {
            if (!graalgo.runtime.GcInterop.isPhase2()) {
                throw new IllegalStateException(
                        "graalgo.runtime.GcInterop.isPhase2() returned false");
            }
        } catch (NoSuchMethodError e) {
            throw new IllegalStateException(
                    "FATAL: Phase-1 graalgo.runtime.GcInterop stub loaded instead of the Phase-2 "
                    + "overlay; phase2/out/classes must precede phase1/out/classes "
                    + "on the classpath",
                    e);
        }
        try {
            if (!graalgo.runtime.GoroutineState.isPhase2()) {
                throw new IllegalStateException(
                        "graalgo.runtime.GoroutineState.isPhase2() returned false");
            }
        } catch (NoSuchMethodError e) {
            throw new IllegalStateException(
                    "FATAL: Phase-1 graalgo.runtime.GoroutineState stub loaded instead of the Phase-2 "
                    + "overlay; phase2/out/classes must precede phase1/out/classes "
                    + "on the classpath",
                    e);
        }
        try {
            if (!graalgo.runtime.Netpoll.isPhase2()) {
                throw new IllegalStateException(
                        "graalgo.runtime.Netpoll.isPhase2() returned false");
            }
        } catch (NoSuchMethodError e) {
            throw new IllegalStateException(
                    "FATAL: Phase-1 graalgo.runtime.Netpoll stub loaded instead of the Phase-2 "
                    + "overlay; phase2/out/classes must precede phase1/out/classes "
                    + "on the classpath",
                    e);
        }
        try {
            if (!graalgo.runtime.Panic.isPhase2()) {
                throw new IllegalStateException(
                        "graalgo.runtime.Panic.isPhase2() returned false");
            }
        } catch (NoSuchMethodError e) {
            throw new IllegalStateException(
                    "FATAL: Phase-1 graalgo.runtime.Panic stub loaded instead of the Phase-2 "
                    + "overlay; phase2/out/classes must precede phase1/out/classes "
                    + "on the classpath",
                    e);
        }
        try {
            if (!graalgo.runtime.Scheduler.isPhase2()) {
                throw new IllegalStateException(
                        "graalgo.runtime.Scheduler.isPhase2() returned false");
            }
        } catch (NoSuchMethodError e) {
            throw new IllegalStateException(
                    "FATAL: Phase-1 Scheduler stub loaded instead of the Phase-2 "
                    + "overlay; phase2/out/classes must precede phase1/out/classes "
                    + "on the classpath",
                    e);
        }
        try {
            if (!graalgo.runtime.Timers.isPhase2()) {
                throw new IllegalStateException(
                        "graalgo.runtime.Timers.isPhase2() returned false");
            }
        } catch (NoSuchMethodError e) {
            throw new IllegalStateException(
                    "FATAL: Phase-1 graalgo.runtime.Timers stub loaded instead of the Phase-2 "
                    + "overlay; phase2/out/classes must precede phase1/out/classes "
                    + "on the classpath",
                    e);
        }
        try {
            if (!graalgo.safepoint.Safepoint.isPhase2()) {
                throw new IllegalStateException(
                        "graalgo.safepoint.Safepoint.isPhase2() returned false");
            }
        } catch (NoSuchMethodError e) {
            throw new IllegalStateException(
                    "FATAL: Phase-1 graalgo.safepoint.Safepoint stub loaded instead of the Phase-2 "
                    + "overlay; phase2/out/classes must precede phase1/out/classes "
                    + "on the classpath",
                    e);
        }
    }

    private static int usageError(String msg) {
        System.err.println("graalgo-run: " + msg);
        usage();
        return EXIT_INPUT_ERROR;
    }

    /** gofrontend failure: exit code + stderr build log. */
    private static final class GofrontendException extends Exception {
        final int exit;
        final String buildLog;

        GofrontendException(int exit, String buildLog) {
            super("gofrontend exited " + exit);
            this.exit = exit;
            this.buildLog = buildLog;
        }
    }

    /**
     * Runs {@code gofrontend <file>} and returns its stdout (the
     * frontend/v1 JSON). Both streams are drained concurrently so a large
     * document can never deadlock against a buffered stderr. (Mirrors
     * Phase-1 {@code Main.runGofrontend}; duplicated here because that
     * method's exception fields are package-private.)
     */
    private static String runGofrontend(Path goFile) throws IOException, GofrontendException {
        ProcessBuilder pb = new ProcessBuilder(
                graalgo.launcher.ToolPaths.gofrontend().toString(), goFile.toString());
        // P2-M5: gofrontend is a gc-built Go binary; it would inherit
        // GODEBUG and print its own schedtrace/gctrace lines into the
        // build log (documented as deterministic), or fatal on knobs like
        // cgocheck=2. The build log must reflect only gofrontend's
        // deterministic output.
        pb.environment().remove("GODEBUG");
        Process p;
        try {
            p = pb.start();
        } catch (IOException e) {
            throw new IOException("failed to start: " + e.getMessage(), e);
        }
        final byte[][] bufs = new byte[2][];
        Thread errDrain = Thread.ofVirtual().start(() -> bufs[1] = readAll(p.getErrorStream()));
        bufs[0] = readAll(p.getInputStream());
        try {
            errDrain.join();
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new IOException("interrupted while reading gofrontend output", e);
        }
        final int exit;
        try {
            exit = p.waitFor();
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new IOException("interrupted while waiting for gofrontend", e);
        }
        if (exit != 0) {
            throw new GofrontendException(exit, new String(bufs[1], StandardCharsets.UTF_8));
        }
        return new String(bufs[0], StandardCharsets.UTF_8);
    }

    private static byte[] readAll(java.io.InputStream in) {
        try {
            return in.readAllBytes();
        } catch (IOException e) {
            return new byte[0];
        }
    }

    private static void usage() {
        System.err.println("usage: graalgo-run [--gmp=N] [--check] file.go");
        System.err.println("  --gmp=N   in-flight cap (default: carriers); effective cap = min(gmp, carriers)");
        System.err.println("  --check   build only, do not execute");
    }
}
