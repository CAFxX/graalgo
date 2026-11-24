package graalgo.p3sync.launcher;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;


import graalgo.launcher.GoPolyglot;
import graalgo.sched.launcher.GoDebug;

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
 * <p>verifies: GG-0073 (sync slice)
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
        // O5: post-"--" args are the Go program's argv, collected
        // separately from input files. The old code folded the first
        // post-"--" arg into `files`, which made a --wire-input run
        // carrying its program argv[0] after `--` (exactly what `graalgo
        // build` artifacts pass) trip "--wire-input cannot be combined
        // with input files". Splitting after the loop keeps the file-run
        // shape identical while letting a wire run carry its argv.
        java.util.List<String> dashdashArgs = new java.util.ArrayList<>();
        for (String a : args) {
            if (dashdash) {
                dashdashArgs.add(a);
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
        if (wireInput != null) {
            // A wire run has no input files: pre-"--" files are still a
            // genuine combination error, but the post-"--" args are the
            // program's argv (argv[0] first), not files.
            if (!files.isEmpty()) {
                return usageError("--wire-input cannot be combined with input files");
            }
            if (dashdashArgs.isEmpty()) {
                return usageError("--wire-input requires program argv after --");
            }
            goArgs.addAll(dashdashArgs.subList(1, dashdashArgs.size()));
        } else {
            // File-run shape, unchanged: with no pre-"--" file, the
            // first post-"--" arg is the input file (and argv[0]);
            // otherwise all post-"--" args are program args.
            if (files.isEmpty() && !dashdashArgs.isEmpty()) {
                files.add(dashdashArgs.get(0));
                goArgs.addAll(dashdashArgs.subList(1, dashdashArgs.size()));
            } else {
                goArgs.addAll(dashdashArgs);
            }
            if (files.isEmpty()) {
                return usageError("no input file");
            }
        }
        if (gmp == 0) {
            // gc (runtime/proc.go schedinit): a positive GOMAXPROCS
            // environment value sets the initial P count; anything else
            // (unset, zero, negative, unparsable) leaves the default.
            gmp = gomaxprocsFromEnv(System.getenv("GOMAXPROCS"));
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

        // P3-M8 (wave 2, R6): os.Args parity (gc): under `go run`, argv[0]
        // is always an absolute path (the compiled temp binary). Graalgo
        // produces no binary artifact; the honest equivalent is the
        // absolute, normalized path of the program entry point (the first
        // input file). A relative argv[0] matches gc in no scenario, so
        // the launcher resolves it here — the single choke point for all
        // entry paths (`graalgo run`, `graalgo test`, direct
        // graalgo-sync-run, diffharness). For a --wire-input run there is
        // no input file, so argv[0] is the artifact path the caller passed
        // after `--`. The language's context initializes the os surface
        // with it (GoOs, via OsSurfaceInitializer) before Go
        // initialization runs, so os.Args is visible to package-var
        // initializers.
        String argv0Source = wireInput != null ? dashdashArgs.get(0) : files.get(0);
        String[] goArgv = new String[goArgs.size() + 1];
        goArgv[0] = Paths.get(argv0Source).toAbsolutePath().normalize().toString();
        for (int i = 0; i < goArgs.size(); i++) {
            goArgv[i + 1] = goArgs.get(i);
        }

        if (!check) {
            // M10 (self-attack D3): preload all graalgo classes before guest
            // execution, so a guest that exhausts the process's fds cannot
            // break lazy class loading (notably on error-reporting paths).
            graalgo.launcher.ClassPreloader.preload();
            if (dumpPolymorphism) {
                graalgo.values.InterfaceDispatchProbes.reset();
                graalgo.values.InterfaceDispatchProbes.setEnabled(true);
            }
        }

        // TR-1 (truffle-design §2): the program is a source of the "go"
        // language (graalgo.language.GoLanguage) evaluated in a polyglot
        // Context. parse builds the AST (the --check step); eval builds it,
        // runs package init and main as the first goroutine on the
        // scheduler (goroutines are polyglot threads), and returns the
        // exit status (T0.A contract: 0 main returned, 2 uncaught panic or
        // deadlock, n os.Exit(n), 1 internal crash). Other goroutines are
        // abandoned, as gc's exit abandons them — so the context is never
        // closed here: the process exits with the status.
        // The input files back source positions; a --wire-input run has
        // none (the language supplies one placeholder source).
        org.graalvm.polyglot.Context context =
                GoPolyglot.newContext(gmp, goArgv, wireInput != null ? java.util.List.of() : paths);
        org.graalvm.polyglot.Source program = GoPolyglot.wireSource(json,
                wireInput != null ? "wire" : paths.get(0).getFileName().toString());
        final int exitCode;
        try {
            if (check) {
                context.parse(program);
                return EXIT_OK;
            }
            exitCode = context.eval(program).asInt();
        } catch (org.graalvm.polyglot.PolyglotException e) {
            if (e.isGuestException()) {
                // GoParseException: the factory cannot build the document.
                System.err.println("graalgo-sync-run: internal error building AST: "
                        + e.getMessage());
            } else {
                System.err.println("graalgo-sync-run: internal error: " + e.getMessage());
            }
            return EXIT_INPUT_ERROR;
        }
        System.out.flush();
        System.err.flush();
        // M9: dump even on panic (partial traces are marked as such by
        // the harness); the dump goes to stderr, never to the program's
        // stdout.
        if (dumpPolymorphism) {
            dumpPolymorphism();
        }
        return exitCode;
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
     * The startup GOMAXPROCS: gc's {@code schedinit} uses the environment
     * value when it parses as a positive integer ({@code atoi32}), else the
     * CPU count.
     */
    static int gomaxprocsFromEnv(String env) {
        if (env != null) {
            try {
                int n = Integer.parseInt(env);
                if (n > 0) {
                    return n;
                }
            } catch (NumberFormatException ignored) {
                // gc ignores an unparsable value
            }
        }
        return Runtime.getRuntime().availableProcessors();
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
                "  --            program argv follow (os.Args[0] is the absolute input-file path, or the artifact path with --wire-input)");
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
