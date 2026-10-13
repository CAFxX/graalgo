package graalgo.launcher;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.Source;
import graalgo.frontend.FrontendException;
import graalgo.frontend.NodeFactory;
import graalgo.values.GoKind;
import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.Map;

/**
 * graalgo-run: the Phase-1 launcher (PLAN.md §5 CLI contract).
 *
 * <pre>
 *   graalgo-run file.go              frontend → AST → execute main; exit 0
 *   graalgo-run --check file.go      frontend only (harness build step; build-failure parity)
 *   graalgo-run --dump-vars file.go  execute, then print name=value per frame slot (debug aid)
 * </pre>
 *
 * <p>The frontend step shells out to {@code gofrontend} (resolved by
 * {@link ToolPaths}): its stdout is the frontend/v1 JSON document, fed to
 * {@link NodeFactory}. Exit codes: 0 = success; 1 = usage error, unreadable
 * input, gofrontend unlaunchable, or an internal/launcher error; 2 = build
 * failure (gofrontend rejected the program — parse, type, or scope error;
 * gofrontend's stderr is passed through verbatim as the build log, so the
 * diffharness records it deterministically).
 *
 * <p>M2: the program's own exit code is always 0 after {@code func main}
 * completes (Go's {@code main} returns nothing; {@code os.Exit} and panics
 * arrive with WS F/G). Computed values are observable in tests via
 * frame-slot reads (the {@code --dump-vars} debug aid prints them); the WS G
 * {@code println} intrinsic becomes the program-observable channel.
 */
public final class Main {
    private Main() {}

    static final int EXIT_OK = 0;
    static final int EXIT_INPUT_ERROR = 1; // usage / unreadable input / launcher-internal
    static final int EXIT_BUILD_FAILURE = 2; // gofrontend rejected the program
    static final int EXIT_PANIC = 2; // uncaught Go panic or Goexit in main (gc: exit 2)

    public static void main(String[] args) {
        System.exit(run(args));
    }

    /** Entry point without the System.exit, for tests. Returns the process exit code. */
    static int run(String[] args) {
        boolean check = false;
        boolean dumpVars = false;
        String dumpPoly = null;
        String file = null;
        for (String a : args) {
            if (a.equals("--dump-polymorphism")) {
                dumpPoly = "-"; // stdout
                continue;
            }
            if (a.startsWith("--dump-polymorphism=")) {
                dumpPoly = a.substring("--dump-polymorphism=".length());
                continue;
            }
            switch (a) {
                case "--check":
                    check = true;
                    break;
                case "--dump-vars":
                    dumpVars = true;
                    break;
                case "--help":
                case "-h":
                    usage();
                    return EXIT_OK;
                default:
                    if (a.startsWith("-")) {
                        return usageError("unknown flag: " + a);
                    }
                    if (file != null) {
                        return usageError("too many input files");
                    }
                    file = a;
            }
        }
        if (file == null) {
            return usageError("no input file");
        }
        Path p = Paths.get(file);
        if (!Files.isReadable(p)) {
            System.err.println("graalgo-run: cannot read input file: " + file);
            return EXIT_INPUT_ERROR;
        }

        // Frontend: gofrontend type-checks the file and emits frontend/v1 JSON.
        final String json;
        try {
            json = runGofrontend(p);
        } catch (FrontendBuildException e) {
            // gofrontend's stderr is the deterministic build log the harness records.
            System.err.print(e.buildLog);
            return e.gofrontendExit == 2 ? EXIT_BUILD_FAILURE : EXIT_INPUT_ERROR;
        } catch (IOException e) {
            System.err.println("graalgo-run: cannot launch gofrontend ("
                    + ToolPaths.gofrontend() + "): " + e.getMessage());
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
            program = new NodeFactory(source).buildProgram(json);
        } catch (FrontendException e) {
            // M9 acceptance: scope errors (program uses constructs outside
            // frontend/v1 scope) are the program's fault → EXIT_BUILD_FAILURE.
            // Genuine internal errors (factory bugs) → EXIT_INPUT_ERROR.
            String msg = e.getMessage();
            if (msg != null && (msg.contains("not supported in frontend/v1 scope")
                    || msg.contains("not supported in v1"))) {
                System.err.println("graalgo-run: " + msg);
                return EXIT_BUILD_FAILURE;
            }
            // gofrontend accepted the program but the factory cannot build it:
            // a launcher-internal inconsistency, never the program's fault.
            System.err.println("graalgo-run: internal error building AST: " + msg);
            return EXIT_INPUT_ERROR;
        }
        if (check) {
            return EXIT_OK;
        }

        if (dumpPoly != null) {
            // M9 (WS L): enable call-site polymorphism recording for this run.
            graalgo.values.CallSiteProbes.setEnabled(true);
        }
        int exitCode = EXIT_OK;
        try {
            if (dumpVars) {
                VirtualFrame frame = Truffle.getRuntime()
                        .createVirtualFrame(new Object[0], program.descriptor());
                program.root().execute(frame);
                Map<String, GoKind> kinds = program.kindsByName();
                program.slotsByName().entrySet().stream()
                        .sorted(Map.Entry.comparingByKey())
                        .forEach(e -> {
                            int slot = e.getValue();
                            Object v = switch (kinds.getOrDefault(e.getKey(), GoKind.INT64).slotKind()) {
                                case Int -> frame.getInt(slot);
                                case Long -> frame.getLong(slot);
                                case Float -> frame.getFloat(slot);
                                case Double -> frame.getDouble(slot);
                                case Boolean -> frame.getBoolean(slot);
                                case Byte -> frame.getByte(slot);
                                case Object -> frame.getObject(slot);
                                case Illegal, Static -> throw new FrontendException(
                                        "--dump-vars: variable " + e.getKey() + " has no readable slot kind");
                            };
                            System.out.println(e.getKey() + "=" + v);
                        });
            } else {
                program.root().getCallTarget().call();
            }
            exitCode = EXIT_OK;
        } catch (graalgo.runtime.PanicException pe) {
            // M5 (WS F): uncaught Go panic → gc-comparable output, exit 2.
            reportPanic(pe);
            exitCode = EXIT_PANIC;
        } catch (graalgo.runtime.Goexit g) {
            // M5 (WS F): Goexit in main = gc's fatal deadlock (exit 2).
            // (Exit 0 applies to non-main goroutines, Phase 2.)
            System.err.println("fatal error: no goroutines (main called runtime.Goexit) - deadlock!");
            System.err.println();
            System.err.println("goroutine 1 [running]:");
            System.err.println("main.main()");
            exitCode = EXIT_PANIC;
        } catch (RuntimeException e) {
            // M2 stand-in: Go panics (incl. divide-by-zero) surface here until
            // WS F maps them to Go panic values.
            System.err.println("graalgo-run: execution failed: " + e);
            exitCode = EXIT_INPUT_ERROR;
        } catch (StackOverflowError soe) {
            // M10 (self-attack): guest recursion burns host stack (-Xss32m,
            // M5 stance). Catching here — after the stack unwound — reports
            // a clean stack-overflow instead of the JVM's raw trace plus the
            // Truffle exception-profiling cascade. gc also aborts (exit 2)
            // on runaway recursion; the depth limit itself is documented.
            reportStackOverflow();
            exitCode = EXIT_PANIC;
        } catch (NoClassDefFoundError | ExceptionInInitializerError e) {
            // M10 (self-attack): Truffle's exception profiler can itself
            // blow the exhausted stack while handling a StackOverflowError,
            // masking it as a class-init failure. Unwrap the cascade;
            // rethrow anything else.
            if (isStackOverflowCascade(e)) {
                reportStackOverflow();
                exitCode = EXIT_PANIC;
            } else {
                throw e;
            }
        }
        if (dumpPoly != null) {
            // M9 (WS L): report the call-site polymorphism snapshot. The
            // program's own output already went to stdout; the snapshot goes
            // to the requested path (or stdout with the bare flag) so
            // measurement runs stay machine-readable.
            String polyJson = graalgo.values.CallSiteProbes.snapshotJson();
            if ("-".equals(dumpPoly)) {
                System.out.println(polyJson);
            } else {
                try {
                    Files.writeString(Paths.get(dumpPoly), polyJson, StandardCharsets.UTF_8);
                } catch (IOException e) {
                    System.err.println("graalgo-run: cannot write polymorphism snapshot: " + e);
                    return EXIT_INPUT_ERROR;
                }
            }
        }
        return exitCode;
    }

    /** gofrontend's side of a failed build: its exit code and captured stderr. */
    public static final class FrontendBuildException extends Exception {
        final int gofrontendExit;
        final String buildLog;

        FrontendBuildException(int gofrontendExit, String buildLog) {
            super("gofrontend exited " + gofrontendExit);
            this.gofrontendExit = gofrontendExit;
            this.buildLog = buildLog;
        }
    }

    /**
     * Runs {@code gofrontend <file>} and returns its stdout (the frontend/v1
     * JSON). Both streams are drained concurrently so a large document can
     * never deadlock against a buffered stderr.
     *
     * <p>Public as a test seam (the corpus differential test drives the real
     * frontend without going through CLI arg parsing).
     */
    public static String runGofrontend(Path goFile) throws IOException, FrontendBuildException {
        ProcessBuilder pb = new ProcessBuilder(ToolPaths.gofrontend().toString(), goFile.toString());
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
            throw new FrontendBuildException(exit, new String(bufs[1], StandardCharsets.UTF_8));
        }
        return new String(bufs[0], StandardCharsets.UTF_8);
    }

    private static byte[] readAll(InputStream in) {
        try {
            return in.readAllBytes();
        } catch (IOException e) {
            return new byte[0];
        }
    }

    private static int usageError(String msg) {
        System.err.println("graalgo-run: " + msg);
        usage();
        return EXIT_INPUT_ERROR;
    }

    private static void usage() {
        System.out.println("usage: graalgo-run [--check] [--dump-vars] [--dump-polymorphism[=path]] file.go");
        System.out.println("       graalgo-run --help");
        System.out.println("  --check      frontend only (diffharness build step)");
        System.out.println("  --dump-vars  after executing, print name=value per frame slot (debug aid)");
        System.out.println("  --dump-polymorphism[=path]");
        System.out.println("               M9 (WS L): record call-site polymorphism during execution,");
        System.out.println("               then print the JSON snapshot (bare flag) or write it to path");
    }

    /**
     * Reports an uncaught Go panic with gc-comparable output (M5, WS F):
     * {@code panic: <value>} plus the goroutine header and Go stack frames.
     * The value rendering is gc-exact ({@code printpanicval}); frame bodies
     * are graalgo-shaped (documented divergence — gc prints pc offsets and
     * inlined frames).
     */
    /** M10 (self-attack): clean report for host-stack exhaustion. */
    private static void reportStackOverflow() {
        System.err.println("graalgo-run: stack overflow: host stack exhausted "
                + "(see phases/phase1/docs/limitations.md)");
    }

    /**
     * M10 (self-attack): is this the Truffle profiler cascade masking a
     * {@link StackOverflowError}? The profiler can blow the exhausted stack
     * while handling the real SOE, surfacing as {@code NoClassDefFoundError:
     * Could not initialize ...ReturnProfile} (or an
     * {@code ExceptionInInitializerError}) <em>caused by</em> the SOE.
     * Package-visible for deterministic unit testing (the live branch
     * selection is racy across Truffle compilation states).
     */
    static boolean isStackOverflowCascade(Throwable t) {
        return (t instanceof NoClassDefFoundError
                        || t instanceof ExceptionInInitializerError)
                && causedBy(t, StackOverflowError.class);
    }

    /** Walks the cause chain looking for an instance of {@code kind}. */
    private static boolean causedBy(Throwable t, Class<? extends Throwable> kind) {
        for (Throwable c = t; c != null; c = c.getCause()) {
            if (kind.isInstance(c)) {
                return true;
            }
        }
        return false;
    }

    private static void reportPanic(graalgo.runtime.PanicException p) {
        // P3-M4 (WS C, ADR-0004): header via PanicRendering.renderPanicLine
        // and frames via Intrinsics.renderGoStack — the same rendering the
        // phase-2/3 scheduler panic path now shares. Byte-identical to the
        // previous inline version (verified by diff).
        System.err.println(graalgo.runtime.PanicRendering.renderPanicLine(p));
        if (p.goValue() instanceof graalgo.runtime.GoErrorValue e && e.signalSegv()) {
            System.err.println("[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0xADDR]");
        }
        System.err.println();
        com.oracle.truffle.api.source.SourceSection throwSec = null;
        if (p.throwSite() != null && p.throwSite().getSourceSection() != null) {
            throwSec = p.throwSite().getSourceSection();
        }
        System.err.print(graalgo.runtime.Intrinsics.renderGoStack(1, p.stackTop(), throwSec));
    }
}
