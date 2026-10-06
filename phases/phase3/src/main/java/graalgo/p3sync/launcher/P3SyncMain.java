package graalgo.p3sync.launcher;

import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.Source;

import graalgo.frontend.FrontendException;
import graalgo.frontend.NodeFactory;
import graalgo.iface.IfaceDispatchBuilder;
import graalgo.iface.IfaceMethodTable;
import graalgo.iface.IfaceMethodTables;
import graalgo.p3link.LinknameIntrinsics;
import graalgo.p3sync.nodes.AtomicOpNode;
import graalgo.p3sync.nodes.SyncNewNode;
import graalgo.p3sync.nodes.SyncOpNode;
import graalgo.p3time.P3TimeRuntime;
import graalgo.p3time.nodes.TimeAfterFuncNode;
import graalgo.p3time.nodes.TimeAfterNode;
import graalgo.p3time.nodes.TimeCNode;
import graalgo.p3time.nodes.TimeNewNode;
import graalgo.p3time.nodes.TimeNowNode;
import graalgo.p3time.nodes.TimeOpNode;
import graalgo.p3time.nodes.TimeSleepNode;
import graalgo.p3time.nodes.TimeTickNode;
import graalgo.sched.SchedulerCore;
import graalgo.sched.launcher.GoDebug;
import graalgo.sched.nodes.GoStatementNode;

/**
 * Phase-3 launcher for {@code sync}-binding programs (P3-M2b, WS D,
 * ADR-0002) with interface method dispatch (P3-M3, WS D, ADR-0003) and
 * the synthetic {@code time} package (P3-M5, WS E, ADR-0005):
 * {@code graalgo-sync-run --gmp=N file.go}.
 *
 * <p>Mirrors Phase-2 {@code SchedMain}'s flow (frontend, exit-code
 * contract) but additionally registers the {@code "syncnew"} /
 * {@code "syncop"} wire kinds (P3-M2b), the {@code "ifacemethod"} kind
 * (P3-M3), and the {@code "timenew"}/{@code "timeop"}/{@code "timec"}/
 * {@code "timenow"}/{@code "timesleep"}/{@code "timeafter"}/
 * {@code "timetick"}/{@code "timeafterfunc"} kinds (P3-M5) on the factory
 * via the public {@code registerExprKind} hook (P2-D8 precedent). No
 * Phase-1 or Phase-2 source is modified; the classpath order phase3 &gt;
 * phase2 &gt; phase1 (see the phase-3 Makefile) keeps every overlay in
 * force.
 *
 * <p>Exit codes match Phase 1/2: 0 ok, 1 usage/input, 2 build failure or
 * uncaught Go panic (gc parity).
 *
 * <p>verifies: GG-0017 (sync slice)
 */
public final class P3SyncMain {
    private P3SyncMain() {}

    static final int EXIT_OK = 0;
    static final int EXIT_INPUT_ERROR = 1;
    static final int EXIT_BUILD_FAILURE = 2;
    static final int EXIT_PANIC = 2;

    public static void main(String[] args) {
        System.exit(run(args));
    }

    /** Entry point without System.exit, for tests. */
    static int run(String[] args) {
        // P2-M5 (WS E): GODEBUG surface triage — same as SchedMain.
        try {
            GoDebug.apply();
        } catch (GoDebug.GoDebugException e) {
            System.err.println("graalgo-sync-run: fatal error: " + e.getMessage());
            return 2;
        }
        int gmp = 0; // 0 = default (carriers)
        boolean check = false;
        boolean dumpPolymorphism = false;
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
                // P3-M3 (M9 re-measure): enable the InterfaceDispatchProbes
                // (WS D hooks) and dump per-site itab-identity histograms
                // to stderr after the run. The program's stdout is
                // untouched, so differential comparisons still apply.
                case "--dump-polymorphism" -> dumpPolymorphism = true;
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
            System.err.println("graalgo-sync-run: cannot read input file: " + file);
            return EXIT_INPUT_ERROR;
        }

        final String json;
        try {
            json = runGofrontend(p);
        } catch (GofrontendException e) {
            // gofrontend's stderr is the deterministic build log the harness records.
            System.err.print(e.buildLog);
            return e.exit == 2 ? EXIT_BUILD_FAILURE : EXIT_INPUT_ERROR;
        } catch (IOException e) {
            System.err.println("graalgo-sync-run: cannot launch gofrontend: " + e.getMessage());
            return EXIT_INPUT_ERROR;
        }

        final String srcText;
        try {
            srcText = Files.readString(p, StandardCharsets.UTF_8);
        } catch (IOException e) {
            System.err.println("graalgo-sync-run: cannot read input file: " + file);
            return EXIT_INPUT_ERROR;
        }
        Source source = Source.newBuilder("go", srcText, p.getFileName().toString()).build();
        final NodeFactory.Program program;
        try {
            NodeFactory factory = new NodeFactory(source);
            // P3-M2b (WS D, ADR-0002): the sync wire kinds. Public
            // extension hook — no Phase-1 NodeFactory change.
            factory.registerExprKind("syncnew", SyncNewNode::build);
            // P3-M4 (WS C, ADR-0004 D2): "syncop" with an "atomic.*"
            // synctyp dispatches to AtomicOpNode (real result kinds);
            // the sync.* namespace keeps SyncOpNode.
            factory.registerExprKind("syncop", (node, ctx) -> {
                String synctyp = node.object("fields", "syncop")
                        .string("synctyp", "syncop.fields");
                if (synctyp.startsWith("atomic.")) {
                    return AtomicOpNode.build(node, ctx, factory);
                }
                return SyncOpNode.build(node, ctx, factory);
            });
            // P3-M3 (WS D, ADR-0003): interface method dispatch. Public
            // extension hook — no Phase-1 NodeFactory change.
            factory.registerExprKind("ifacemethod", new IfaceDispatchBuilder(factory));
            // P3-M5 (WS E, ADR-0005 D3): the synthetic time wire kinds.
            // Public extension hook — no Phase-1 NodeFactory change.
            factory.registerExprKind("timenew", (node, ctx) -> TimeNewNode.build(node, ctx, factory));
            factory.registerExprKind("timeop", (node, ctx) -> TimeOpNode.build(node, ctx, factory));
            factory.registerExprKind("timec", (node, ctx) -> TimeCNode.build(node, ctx, factory));
            factory.registerExprKind("timenow", TimeNowNode::build);
            factory.registerExprKind("timesleep", (node, ctx) -> TimeSleepNode.build(node, ctx, factory));
            factory.registerExprKind("timeafter", (node, ctx) -> TimeAfterNode.build(node, ctx, factory));
            factory.registerExprKind("timetick", (node, ctx) -> TimeTickNode.build(node, ctx, factory));
            factory.registerExprKind("timeafterfunc",
                    (node, ctx) -> TimeAfterFuncNode.build(node, ctx, factory));
            // P2-M1 (WS A): the go statement, reused from Phase 2 via the
            // same public hook (no Phase-2 change — GoStatementNode is a
            // public class).
            factory.registerStmtKind("gostmt",
                    (node, ctx) -> GoStatementNode.build(node, ctx, factory));
            program = factory.buildProgram(json);
            // P3-M3 (WS D, ADR-0003): install the interface method tables.
            // Method objIDs resolve through Program.functions(); a missing
            // objID fails fast here. Installed on every build (even empty)
            // so a previous program's tables can never leak into this run.
            IfaceMethodTable.install(IfaceMethodTables.resolve(json, program.functions()));
            // P3-M4 (WS C, ADR-0004): linkname symbol table (both
            // directions), itabFor provider, interface method lists, and
            // Go function bindings for callGo. Installed on every build
            // (even empty) so a previous program's bindings can never
            // leak into this run (registries are static).
            LinknameIntrinsics.install(json, program);
        } catch (FrontendException e) {
            System.err.println("graalgo-sync-run: internal error building AST: " + e.getMessage());
            return EXIT_INPUT_ERROR;
        }
        if (check) {
            return EXIT_OK;
        }
        if (dumpPolymorphism) {
            graalgo.values.InterfaceDispatchProbes.reset();
            graalgo.values.InterfaceDispatchProbes.setEnabled(true);
        }

        // Install the scheduler and run main as the first goroutine
        // (SchedMain's flow, verbatim).
        assertSchedulerOverlay();
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
            // P3-M5 (WS E, ADR-0005): timer translators are background
            // goroutines that may outlive the Go program's own goroutines
            // (gc: process exit abandons pending timers; time.Tick's ticker
            // leaks by design). awaitQuiescence waits for EVERY goroutine,
            // so first wait until only translators remain, shut them down,
            // then await full quiescence. With no timers this is just the
            // ordinary quiescence wait at 1ms poll granularity.
            P3TimeRuntime.awaitBackgroundDrain(sched);
            sched.awaitQuiescence();
        } catch (InterruptedException ie) {
            Thread.currentThread().interrupt();
            System.err.println("graalgo-sync-run: interrupted while awaiting quiescence");
            return EXIT_INPUT_ERROR;
        } finally {
            SchedulerCore.install(prev);
        }
        // M9: dump even on panic (partial traces are marked as such by
        // the harness); the dump goes to stderr, never to the program's
        // stdout.
        if (dumpPolymorphism) {
            dumpPolymorphism();
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
     * M9 re-measurement (P3-M3): per-site itab-identity histograms from
     * {@link graalgo.values.InterfaceDispatchProbes}, one line per site on
     * stderr. A site's itab count is its dynamic-type cardinality:
     * 1 = monomorphic, 2 = bimorphic, &ge;3 = megamorphic.
     */
    private static void dumpPolymorphism() {
        var sites = graalgo.values.InterfaceDispatchProbes.sites();
        System.err.println("POLYMORPHISM-BEGIN sites=" + sites.size());
        var names = new java.util.ArrayList<>(sites.keySet());
        java.util.Collections.sort(names);
        for (String name : names) {
            var site = sites.get(name);
            System.err.println("POLYMORPHISM-SITE name=\"" + name + "\" itabs="
                    + site.histogram().size() + " dispatches=" + site.total());
        }
        System.err.println("POLYMORPHISM-END");
    }

    /**
     * Fail-fast that the Phase-2 overlay is active (P2-D8, same as
     * SchedMain): the {@code graalgo.runtime.Scheduler} on the classpath
     * must be the Phase-2 replacement, i.e. phase2/out/classes precedes
     * phase1/out/classes. Never silent.
     */
    private static void assertSchedulerOverlay() {
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
    }

    private static int usageError(String msg) {
        System.err.println("graalgo-sync-run: " + msg);
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
     * Phase-2 {@code SchedMain.runGofrontend}; duplicated because that
     * method is private.)
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

    private static byte[] readAll(InputStream in) {
        try {
            return in.readAllBytes();
        } catch (IOException e) {
            throw new RuntimeException(e);
        }
    }

    private static void usage() {
        System.err.println("usage: graalgo-sync-run [--gmp=N] [--check] [--dump-polymorphism] file.go");
        System.err.println("  --gmp=N   in-flight cap (default: carriers); effective cap = min(gmp, carriers)");
        System.err.println("  --check   build only, do not execute");
        System.err.println("  --dump-polymorphism  M9: dump per-site itab-identity histograms to stderr");
    }
}
