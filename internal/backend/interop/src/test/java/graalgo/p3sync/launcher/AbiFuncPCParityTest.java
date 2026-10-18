package graalgo.p3sync.launcher;

import com.oracle.truffle.api.source.Source;
import graalgo.frontend.NodeFactory;
import graalgo.frontend.WireKindRegistrar;
import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ServiceLoader;

/**
 * Parity guard for the 2026-10-18 review P1 (launcher divergence): the
 * {@code "abifuncpc"} wire kind must be accepted identically by both
 * launchers — {@code graalgo-run} (via the ServiceLoader-discovered
 * {@link WireKindRegistrar}) and {@code graalgo-sync-run}
 * ({@link P3SyncMain}). Both register through the single shared
 * {@link P3KindRegistrations}; this test fails if either path rejects the
 * kind or their observable behavior diverges.
 *
 * <p>The wire document is a committed fixture
 * ({@code abifuncpc-wire.json}): a minimal frontend/v1 program whose
 * {@code main} prints a marker line, then the result of an
 * {@code "abifuncpc"} node applied to a func value (expected: a non-zero
 * opaque stable func id). gofrontend cannot emit this node from ordinary
 * user Go code (it lowers {@code internal/abi} compile intrinsics, which
 * user code cannot import), so the fixture is hand-built from real
 * gofrontend output with the node injected.
 */
public final class AbiFuncPCParityTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /** The fixture wire JSON exercising the "abifuncpc" node. */
    private static String wireJson() throws Exception {
        try (var in = AbiFuncPCParityTest.class.getResourceAsStream("abifuncpc-wire.json")) {
            assertTrue(in != null, "test resource abifuncpc-wire.json missing from test classpath");
            return new String(in.readAllBytes(), StandardCharsets.UTF_8);
        }
    }

    /**
     * Captures whatever the guest program prints. Go's {@code println}
     * builtin writes to <b>stderr</b> (gc parity — see
     * {@code graalgo.runtime.BuiltinPrint}), so the fixture's
     * {@code println("start")} / {@code println(abifuncpc(...))} lines
     * land on {@code System.err}, not {@code System.out}.
     */
    private static String captureProgramOutput(ThrowingRunnable body) throws Exception {
        PrintStream oldErr = System.err;
        ByteArrayOutputStream buf = new ByteArrayOutputStream();
        System.setErr(new PrintStream(buf, true, StandardCharsets.UTF_8));
        try {
            body.run();
        } finally {
            System.setErr(oldErr);
        }
        return buf.toString(StandardCharsets.UTF_8);
    }

    /**
     * Runs the fixture through graalgo-run's exact registration path:
     * ServiceLoader-discovered WireKindRegistrar providers, then
     * buildProgram — the same four lines as
     * {@code graalgo.launcher.Main.runOne}.
     */
    private static String runServiceLoaderPath(String json) throws Exception {
        Source source = Source.newBuilder("go", "package main\n", "parity.go").build();
        NodeFactory factory = new NodeFactory(source);
        int registrars = 0;
        for (WireKindRegistrar registrar : ServiceLoader.load(WireKindRegistrar.class)) {
            registrar.registerAll(factory);
            registrars++;
        }
        assertTrue(registrars > 0,
                "no WireKindRegistrar discovered via ServiceLoader (graalgo-run path untested)");
        NodeFactory.Program program = factory.buildProgram(json);
        return captureProgramOutput(() -> program.root().getCallTarget().call());
    }

    /** Both launchers must accept the kind and print a non-zero func id. */
    public void testAbifuncpcAcceptedByBothLaunchers() throws Exception {
        String json = wireJson();
        // Warm-up, output discarded: the first Truffle execution in a
        // JVM prints one-time HotSpot/Unsafe warnings to stderr — run it
        // uncaptured so the assertions below see only program output.
        runServiceLoaderPathUncaptured(json);
        String syncOut = runSyncLauncherCapture(json);
        String runOut = runServiceLoaderPath(json);
        assertPrintedFuncId(syncOut, "graalgo-sync-run");
        assertPrintedFuncId(runOut, "graalgo-run (ServiceLoader path)");
    }

    /** Uncaptured throwaway run: triggers one-time JVM/Truffle stderr noise. */
    private static void runServiceLoaderPathUncaptured(String json) throws Exception {
        Source source = Source.newBuilder("go", "package main\n", "parity.go").build();
        NodeFactory factory = new NodeFactory(source);
        for (WireKindRegistrar registrar : ServiceLoader.load(WireKindRegistrar.class)) {
            registrar.registerAll(factory);
        }
        factory.buildProgram(json).root().getCallTarget().call();
    }

    private static String runSyncLauncherCapture(String json) throws Exception {
        Path dir = Files.createTempDirectory("graalgo-abifuncpc-test");
        Path wire = dir.resolve("abifuncpc-wire.json");
        Files.writeString(wire, json, StandardCharsets.UTF_8);
        final int[] code = new int[1];
        String out = captureProgramOutput(
                () -> code[0] = P3SyncMain.run(new String[] {"--wire-input=" + wire}));
        assertTrue(code[0] == 0,
                "graalgo-sync-run --wire-input exited " + code[0] + " (want 0)");
        return out;
    }

    /** The program must print "start" then a non-zero opaque func id. */
    private static void assertPrintedFuncId(String out, String which) {
        String[] lines = out.strip().split("\n");
        assertTrue(lines.length == 2 && lines[0].equals("start"),
                which + ": unexpected output (want 'start' + func id), got: " + out);
        long id;
        try {
            id = Long.parseLong(lines[1].strip());
        } catch (NumberFormatException e) {
            throw new AssertionError(
                    which + ": second line is not a numeric func id: " + out);
        }
        assertTrue(id != 0, which + ": abifuncpc returned a zero func id");
    }

    private interface ThrowingRunnable {
        void run() throws Exception;
    }
}
