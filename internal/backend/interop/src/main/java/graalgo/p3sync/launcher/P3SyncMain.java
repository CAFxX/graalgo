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
import graalgo.p3builtin.nodes.ClearNode;
import graalgo.p3builtin.nodes.GoMaxProcsNode;
import graalgo.p3builtin.nodes.MinMaxNode;
import graalgo.p3builtin.nodes.NumCPUNode;
import graalgo.p3chan.nodes.ChanCloseNode;
import graalgo.p3chan.nodes.ChanLenCapNode;
import graalgo.p3chan.nodes.ChanRecvNode;
import graalgo.p3chan.nodes.ChanSendNode;
import graalgo.p3chan.nodes.MakeChanNode;
import graalgo.p3chan.nodes.SelectNode;
import graalgo.rtstack.RuntimeCallerNode;
import graalgo.rtstack.RuntimeCallersFramesNode;
import graalgo.rtstack.RuntimeCallersNode;
import graalgo.rtstack.RuntimeFramesNextNode;
import graalgo.rtstack.RuntimeFuncEntryNode;
import graalgo.rtstack.RuntimeFuncFileLineNode;
import graalgo.rtstack.RuntimeFuncForPCNode;
import graalgo.rtstack.RuntimeFuncNameNode;
import graalgo.p3link.LinknameIntrinsics;
import graalgo.p3os.GoOs;
import graalgo.p3sync.nodes.AtomicAddrNode;
import graalgo.p3sync.nodes.AtomicOpNode;
import graalgo.p3sync.nodes.SyncNewNode;
import graalgo.p3sync.nodes.SyncOpNode;
import graalgo.p3unsafe.UnsafeAddNode;
import graalgo.p3unsafe.UnsafeFromPtrNode;
import graalgo.p3unsafe.UnsafeFromUintNode;
import graalgo.p3unsafe.UnsafeSliceDataNode;
import graalgo.p3unsafe.UnsafeSliceNode;
import graalgo.p3unsafe.UnsafeStringDataNode;
import graalgo.p3unsafe.UnsafeStringNode;
import graalgo.p3unsafe.UnsafeTable;
import graalgo.p3unsafe.UnsafeToPtrNode;
import graalgo.p3math.nodes.MathF32BitsNode;
import graalgo.p3math.nodes.MathF32FromBitsNode;
import graalgo.p3math.nodes.MathF64BitsNode;
import graalgo.p3math.nodes.MathF64FromBitsNode;
import graalgo.p3math.nodes.MathFmaNode;
import graalgo.p3math.nodes.MathSqrtNode;
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
 * {@code "timetick"}/{@code "timeafterfunc"} kinds (P3-M5) and the
 * {@code "makechan"}/{@code "send"}/{@code "recv"}/{@code "chanclose"}/
 * {@code "select"} channel kinds (P3-M5, ADR-0005 Appendix C) on the
 * factory via the public {@code registerExprKind}/{@code registerStmtKind}
 * hooks (P2-D8 precedent). No Phase-1 or Phase-2 source is modified; the
 * classpath order phase3 &gt; phase2 &gt; phase1 (see the phase-3 Makefile)
 * keeps every overlay in force.
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
        // PKG-001: package dir whose internal-visibility privileges a
        // synthesized `graalgo test` driver compiles with. Explicit argv
        // flag (per-invocation, non-inheritable), never an environment
        // variable: a user-exported value must not confer privilege on
        // commands that never asked for it.
        String testPackageDir = null;
        // Phase 4 (build-artifact): path to a file containing pre-generated
        // frontend/v1 JSON. When present, the JSON is read directly instead
        // of running gofrontend on source files. This is how `graalgo build`
        // artifacts avoid re-running the frontend on every execution: the
        // frontend runs once at build time, and the artifact embeds its
        // output.
        String wireInput = null;
        // Phase 4 (build-artifact): when set, write the frontend JSON to
        // this file and exit (implies --check). This is how `graalgo build`
        // captures the frontend output at build time for embedding in the
        // artifact.
        String emitWire = null;
        java.util.List<String> files = new java.util.ArrayList<>();
        // P3-M8 (wave 2, R6): program argv. Everything after a bare
        // "--" is the Go program's argv (os.Args); the first element is
        // always the absolute input-file path (gc parity: argv[0] is
        // absolute under `go run`). Without "--", os.Args is just
        // [input-file].
        boolean dashdash = false;
        java.util.List<String> goArgs = new java.util.ArrayList<>();
        for (String a : args) {
            if (dashdash) {
                if (files.isEmpty()) {
                    files.add(a);
                } else {
                    goArgs.add(a);
                }
                continue;
            }
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
            if (a.startsWith("--test-package-dir=")) {
                testPackageDir = a.substring("--test-package-dir=".length());
                if (testPackageDir.isEmpty()) {
                    return usageError("--test-package-dir must not be empty");
                }
                continue;
            }
            if (a.startsWith("--wire-input=")) {
                wireInput = a.substring("--wire-input=".length());
                if (wireInput.isEmpty()) {
                    return usageError("--wire-input must not be empty");
                }
                continue;
            }
            if (a.startsWith("--emit-wire=")) {
                emitWire = a.substring("--emit-wire=".length());
                if (emitWire.isEmpty()) {
                    return usageError("--emit-wire must not be empty");
                }
                continue;
            }
            switch (a) {
                case "--" -> dashdash = true;
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
                    files.add(a);
                }
            }
        }
        if (files.isEmpty() && wireInput == null) {
            return usageError("no input file");
        }
        if (!files.isEmpty() && wireInput != null) {
            return usageError("--wire-input cannot be combined with input files");
        }
        if (gmp == 0) {
            gmp = Runtime.getRuntime().availableProcessors();
        }
        java.util.List<Path> paths = new java.util.ArrayList<>();
        for (String f : files) {
            Path p = Paths.get(f);
            if (!Files.isReadable(p)) {
                System.err.println("graalgo-sync-run: cannot read input file: " + f);
                return EXIT_INPUT_ERROR;
            }
            paths.add(p);
        }

        final String json;
        if (wireInput != null) {
            // Pre-generated frontend output: read directly, skipping gofrontend.
            try {
                json = Files.readString(Paths.get(wireInput), StandardCharsets.UTF_8);
            } catch (IOException e) {
                System.err.println("graalgo-sync-run: cannot read --wire-input file: " + e.getMessage());
                return EXIT_INPUT_ERROR;
            }
        } else try {
            json = runGofrontend(paths, testPackageDir);
        } catch (GofrontendException e) {
            // gofrontend's stderr is the deterministic build log the harness records.
            // M10 (self-attack F11): a gofrontend killed by a signal leaves an
            // empty build log — the exit status is the only signal. Never fail
            // silently: report the exit code when there is no log.
            if (e.buildLog.isEmpty()) {
                System.err.println("graalgo-sync-run: gofrontend exited " + e.exit
                        + " with no output");
            } else {
                System.err.print(e.buildLog);
            }
            return e.exit == 2 ? EXIT_BUILD_FAILURE : EXIT_INPUT_ERROR;
        } catch (IOException e) {
            System.err.println("graalgo-sync-run: cannot launch gofrontend: " + e.getMessage());
            return EXIT_INPUT_ERROR;
        }

        // Phase 4 (build-artifact): dump the frontend JSON and exit.
        // Used by `graalgo build` to capture the frontend output at build
        // time for embedding in the artifact.
        if (emitWire != null) {
            try {
                Files.writeString(Paths.get(emitWire), json, StandardCharsets.UTF_8);
            } catch (IOException e) {
                System.err.println("graalgo-sync-run: cannot write --emit-wire file: " + e.getMessage());
                return EXIT_INPUT_ERROR;
            }
            return EXIT_OK;
        }

        // Build one Truffle Source per input file; the frontend's position
        // "file" index selects among them (frontend/v1 wire format).
        Source[] sources = new Source[paths.size()];
        for (int i = 0; i < paths.size(); i++) {
            final String srcText;
            try {
                srcText = Files.readString(paths.get(i), StandardCharsets.UTF_8);
            } catch (IOException e) {
                System.err.println("graalgo-sync-run: cannot read input file: " + files.get(i));
                return EXIT_INPUT_ERROR;
            }
            sources[i] = Source.newBuilder("go", srcText,
                    paths.get(i).getFileName().toString()).build();
        }
        final NodeFactory.Program program;
        try {
            NodeFactory factory = new NodeFactory(sources);
            // P3-M2b (WS D, ADR-0002): the sync wire kinds. Public
            // extension hook — no Phase-1 NodeFactory change.
            factory.registerExprKind("syncnew", (node, ctx) -> SyncNewNode.build(node, ctx, factory));
            // fix-linkname (Caddy/godebug lane): the atomicaddr wire node
            // (package-level sync/atomic word ops).
            factory.registerExprKind("atomicaddr",
                    (node, ctx) -> AtomicAddrNode.build(node, ctx, factory));
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
            // P3-M5 (WS E, ADR-0005 Appendix C): the channel wire kinds.
            // Public extension hooks — no Phase-1/Phase-2 change.
            factory.registerExprKind("makechan",
                    (node, ctx) -> MakeChanNode.build(node, ctx, factory));
            factory.registerStmtKind("send",
                    (node, ctx) -> ChanSendNode.build(node, ctx, factory));
            factory.registerExprKind("recv",
                    (node, ctx) -> ChanRecvNode.build(node, ctx, factory));
            // P3-M5 (WS E, ADR-0005 Appendix C): len/cap on channels (the
            // frozen LenNode rejects CHAN, so the exporter emits additive
            // chanlen/chancap nodes). Public extension hooks — no
            // Phase-1/Phase-2 change.
            factory.registerExprKind("chanlen",
                    (node, ctx) -> ChanLenCapNode.build(node, ctx, factory, false));
            factory.registerExprKind("chancap",
                    (node, ctx) -> ChanLenCapNode.build(node, ctx, factory, true));
            factory.registerStmtKind("chanclose",
                    (node, ctx) -> ChanCloseNode.build(node, ctx, factory));
            factory.registerStmtKind("select",
                    (node, ctx) -> SelectNode.build(node, ctx, factory));
            // P3-M7 (WS G, ADR-0007 D5): the synthetic math wire kinds
            // (bit conversions, Sqrt, FMA). Public extension hooks — no
            // Phase-1/Phase-2 change.
            factory.registerExprKind("mathf64bits",
                    (node, ctx) -> MathF64BitsNode.build(node, factory, ctx));
            factory.registerExprKind("mathf64frombits",
                    (node, ctx) -> MathF64FromBitsNode.build(node, factory, ctx));
            factory.registerExprKind("mathf32bits",
                    (node, ctx) -> MathF32BitsNode.build(node, factory, ctx));
            factory.registerExprKind("mathf32frombits",
                    (node, ctx) -> MathF32FromBitsNode.build(node, factory, ctx));
            factory.registerExprKind("mathsqrt",
                    (node, ctx) -> MathSqrtNode.build(node, factory, ctx));
            factory.registerExprKind("mathfma",
                    (node, ctx) -> MathFmaNode.build(node, factory, ctx));
            // P4 (driver-harden): the min/max builtin wire kind. The frozen
            // v1 builtin path rejects min/max (WS G), but real modules use
            // them — the frontend lowers them to this additive node. Public
            // extension hook — no Phase-1/Phase-2 change.
            factory.registerExprKind("minmax",
                    (node, ctx) -> MinMaxNode.build(node, ctx, factory));
            // P4 (driver-harden): the clear builtin wire kind (same
            // rationale as minmax above).
            factory.registerExprKind("clear",
                    (node, ctx) -> ClearNode.build(node, ctx, factory));
            // P4 (driver-harden): runtime.NumCPU() (same rationale).
            factory.registerExprKind("runtimenumcpu", NumCPUNode::build);
            // P4 (driver-harden): runtime.GOMAXPROCS(n) (same rationale).
            factory.registerExprKind("runtimegomaxprocs",
                    (node, ctx) -> GoMaxProcsNode.build(node, ctx, factory));
            // Stack-inspection family (synthetic runtime package):
            // runtime.Callers/Caller/CallersFrames/FuncForPC and the
            // Frames/Func methods. Public extension hooks — no Phase-1/
            // Phase-2 change.
            factory.registerExprKind("runtimecallers",
                    (node, ctx) -> RuntimeCallersNode.build(node, ctx, factory));
            factory.registerExprKind("runtimecaller",
                    (node, ctx) -> RuntimeCallerNode.build(node, ctx, factory));
            factory.registerExprKind("runtimecallersframes",
                    (node, ctx) -> RuntimeCallersFramesNode.build(node, ctx, factory));
            factory.registerExprKind("runtimeframesnext",
                    (node, ctx) -> RuntimeFramesNextNode.build(node, ctx, factory));
            factory.registerExprKind("runtimefuncforpc",
                    (node, ctx) -> RuntimeFuncForPCNode.build(node, ctx, factory));
            factory.registerExprKind("runtimefuncname",
                    (node, ctx) -> RuntimeFuncNameNode.build(node, ctx, factory));
            factory.registerExprKind("runtimefuncentry",
                    (node, ctx) -> RuntimeFuncEntryNode.build(node, ctx, factory));
            factory.registerExprKind("runtimefuncfileline",
                    (node, ctx) -> RuntimeFuncFileLineNode.build(node, ctx, factory));
            // P3-M8: the synthetic unsafe wire kinds (Pointer conversions,
            // Slice/SliceData/String/StringData, Add trap). Public extension
            // hooks — no Phase-1/Phase-2 change. The per-run handle table is
            // installed here (same pattern as LinknameIntrinsics.install
            // below: static registries are re-installed on every build so a
            // previous program's handles can never leak into this run).
            factory.registerExprKind("unsafefromptr",
                    (node, ctx) -> UnsafeFromPtrNode.build(node, factory, ctx));
            factory.registerExprKind("unsafetooptr",
                    (node, ctx) -> UnsafeToPtrNode.build(node, factory, ctx));
            factory.registerExprKind("unsafefromuint",
                    (node, ctx) -> UnsafeFromUintNode.build(node, factory, ctx));
            factory.registerExprKind("unsafeslice",
                    (node, ctx) -> UnsafeSliceNode.build(node, factory, ctx));
            factory.registerExprKind("unsafeslicedata",
                    (node, ctx) -> UnsafeSliceDataNode.build(node, factory, ctx));
            factory.registerExprKind("unsafestring",
                    (node, ctx) -> UnsafeStringNode.build(node, factory, ctx));
            factory.registerExprKind("unsafestringdata",
                    (node, ctx) -> UnsafeStringDataNode.build(node, factory, ctx));
            factory.registerExprKind("unsafeadd",
                    (node, ctx) -> UnsafeAddNode.build(node, factory, ctx));
            UnsafeTable.install();
            // P3-M8 (wave 2, reflect): the "reflectop" wire node (single
            // kind dispatching on "op"). Public extension hook — no
            // Phase-1/Phase-2 change.
            factory.registerExprKind("reflectop",
                    (node, ctx) -> graalgo.p3reflect.ReflectOpNode.build(node, ctx, factory));
            // real-reflect (Phase 1): the "abifuncpc" wire node for the
            // internal/abi FuncPCABI0/FuncPCABIInternal compile intrinsics
            // (opaque stable func ids). Public extension hook — no
            // Phase-1/Phase-2 change.
            factory.registerExprKind("abifuncpc",
                    (node, ctx) -> graalgo.p3abi.AbiFuncPCNode.build(node, factory, ctx));
            // P2-M1 (WS A): the go statement, reused from Phase 2 via the
            // same public hook (no Phase-2 change — GoStatementNode is a
            // public class).
            factory.registerStmtKind("gostmt",
                    (node, ctx) -> GoStatementNode.build(node, ctx, factory));
            program = factory.buildProgram(json);
            // P3-M9: parse-once — the downstream installers reuse the parsed
            // document instead of re-parsing the 200MB JSON text (was 5x).
            graalgo.frontend.Json.Object doc = factory.document();
            // P3-M3 (WS D, ADR-0003): install the interface method tables.
            // Method objIDs resolve through Program.functions(); a missing
            // objID fails fast here. Installed on every build (even empty)
            // so a previous program's tables can never leak into this run.
            IfaceMethodTable.install(IfaceMethodTables.resolve(doc, program.functions()));
            // P3-M9 (RC-6): install the fmt.handleMethods handler (Formatter/
            // Stringer/error dispatch for fmt verbs) and the Java-implemented
            // fmt.State methods.
            graalgo.iface.FmtHandleMethods.install();
            // P3-M10 (fix-fmt-funcs): install the Go method dispatcher for
            // the fmt F*/scan/format families (Writer/Reader/ScanState
            // method dispatch) and the Java-implemented fmt.ScanState
            // methods.
            graalgo.iface.FmtMethodDispatch.install();
            // P3-M10 (fix-fmt-funcs): install the Go method dispatcher for
            // the fmt F*/scan/format families (writer/reader/ScanState
            // dispatch) and the Java-implemented fmt.ScanState methods.
            graalgo.iface.FmtMethodDispatch.install();
            // P3-M8 (wave 2, reflect): install the GoRType table (canonical
            // per structural identity) and the unsafe handle table. Installed
            // on every build (even empty) so a previous program's tables can
            // never leak into this run.
            graalgo.p3unsafe.UnsafeTable.install();
            graalgo.p3reflect.ReflectTypes.install(doc, program.functions());
            // P3-M4 (WS C, ADR-0004): linkname symbol table (both
            // directions), itabFor provider, interface method lists, and
            // Go function bindings for callGo. Installed on every build
            // (even empty) so a previous program's bindings can never
            // leak into this run (registries are static).
            LinknameIntrinsics.install(doc, program);
        } catch (FrontendException e) {
            System.err.println("graalgo-sync-run: internal error building AST: " + e.getMessage());
            return EXIT_INPUT_ERROR;
        }
        if (check) {
            return EXIT_OK;
        }
        // M10 (self-attack D3): preload all graalgo classes before guest
        // execution, so a guest that exhausts the process's fds cannot
        // break lazy class loading (notably on error-reporting paths).
        graalgo.launcher.ClassPreloader.preload();
        if (dumpPolymorphism) {
            graalgo.values.InterfaceDispatchProbes.reset();
            graalgo.values.InterfaceDispatchProbes.setEnabled(true);
        }

        // P3-M8 (wave 2, R6): initialize the os surface's process-wide
        // state (virtual cwd, env overlay, stdio cookies, argv) BEFORE
        // Go initialization runs — os.Args[i] must already be visible
        // to package-var initializers.
        //
        // os.Args[0] parity (gc): under `go run`, argv[0] is always an
        // absolute path (the compiled temp binary). Graalgo produces no
        // binary artifact; the honest equivalent is the absolute,
        // normalized path of the program entry point (the first input
        // file). A relative argv[0] matches gc in no scenario, so the
        // launcher resolves it here — the single choke point for all
        // entry paths (`graalgo run`, `graalgo test`, direct
        // graalgo-sync-run, diffharness).
        String[] goArgv = new String[goArgs.size() + 1];
        goArgv[0] = Paths.get(files.get(0)).toAbsolutePath().normalize().toString();
        for (int i = 0; i < goArgs.size(); i++) {
            goArgv[i + 1] = goArgs.get(i);
        }
        GoOs.init(goArgv);

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
     *
     * @param testPackageDir package dir for gofrontend's
     *     {@code --test-package-dir} flag (PKG-001), or null for an
     *     ordinary compile with no test-driver privilege.
     */
    private static String runGofrontend(java.util.List<Path> goFiles, String testPackageDir) throws IOException, GofrontendException {
        // P3-M9: run gofrontend with stdout to a temp file, not a pipe.
        // Piping 200MB+ through Process.getInputStream() hangs (gofrontend
        // blocks on write while the reader blocks on read — pipe buffer
        // deadlock for large outputs). A temp file avoids the pipe entirely.
        // General fix: benefits any program with a large AST export.
        Path tmpOut = Files.createTempFile("gofrontend-", ".json");
        Path tmpErr = Files.createTempFile("gofrontend-", ".err");
        try {
            java.util.List<String> cmd = new java.util.ArrayList<>();
            cmd.add(graalgo.launcher.ToolPaths.gofrontend().toString());
            if (testPackageDir != null) {
                cmd.add("--test-package-dir=" + testPackageDir);
            }
            for (Path p : goFiles) {
                cmd.add(p.toString());
            }
            ProcessBuilder pb = new ProcessBuilder(cmd);
            // P2-M5: gofrontend is a gc-built Go binary; it would inherit
            // GODEBUG and print its own schedtrace/gctrace lines into the
            // build log (documented as deterministic), or fatal on knobs like
            // cgocheck=2. The build log must reflect only gofrontend's
            // deterministic output.
            pb.environment().remove("GODEBUG");
            pb.redirectOutput(tmpOut.toFile());
            pb.redirectError(tmpErr.toFile());
            Process p;
            try {
                p = pb.start();
            } catch (IOException e) {
                throw new IOException("failed to start: " + e.getMessage(), e);
            }
            final int exit;
            try {
                exit = p.waitFor();
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new IOException("interrupted while waiting for gofrontend", e);
            }
            if (exit != 0) {
                String errLog = Files.readString(tmpErr, StandardCharsets.UTF_8);
                throw new GofrontendException(exit, errLog);
            }
            return Files.readString(tmpOut, StandardCharsets.UTF_8);
        } finally {
            try { Files.deleteIfExists(tmpOut); } catch (IOException ignored) {}
            try { Files.deleteIfExists(tmpErr); } catch (IOException ignored) {}
        }
    }

    private static void usage() {
        System.err.println(
                "usage: graalgo-sync-run [--gmp=N] [--check] [--dump-polymorphism] [--test-package-dir=DIR] [--wire-input=FILE] [--emit-wire=FILE] file.go [file.go...] [-- args...]");
        System.err.println(
                "  --            program argv follow (os.Args[0] is the absolute input-file path)");
        System.err.println("  --gmp=N   in-flight cap (default: carriers); effective cap = min(gmp, carriers)");
        System.err.println("  --check   build only, do not execute");
        System.err.println("  --dump-polymorphism  M9: dump per-site itab-identity histograms to stderr");
        System.err.println("  --test-package-dir=DIR  PKG-001: package dir whose internal-visibility");
        System.err.println("      privileges a synthesized `graalgo test` driver compiles with");
        System.err.println("  --wire-input=FILE  read pre-generated frontend/v1 JSON instead of running");
        System.err.println("      gofrontend (used by `graalgo build` artifacts; cannot combine with files)");
        System.err.println("  --emit-wire=FILE  write the frontend/v1 JSON to FILE and exit (implies --check;");
        System.err.println("      used by `graalgo build` to capture the frontend output at build time)");
    }
}
