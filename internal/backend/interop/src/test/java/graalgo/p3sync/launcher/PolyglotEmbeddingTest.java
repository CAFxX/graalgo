package graalgo.p3sync.launcher;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import org.graalvm.polyglot.Context;
import org.graalvm.polyglot.PolyglotException;
import org.graalvm.polyglot.Source;
import org.graalvm.polyglot.Value;
import org.graalvm.polyglot.io.ByteSequence;

/**
 * TR-1 (truffle-design §2, §10.2): the Go language embedded through the
 * plain polyglot API — {@code Context.newBuilder("go")}, a byte
 * {@link Source} of the frontend/v1 wire MIME type, {@code eval}, and the
 * program's exit status read back as a {@link Value}. No graalgo launcher
 * class is involved after the wire document exists (it is produced once by
 * {@code graalgo-sync-run --emit-wire}, i.e. gofrontend).
 *
 * <p>Falsifiable: each program's status differs (0 / 3 / 2), so a language
 * that ignored the program, or a launcher-only exit path, fails the
 * assertions; the goroutine assertion fails if goroutines do not run as
 * threads of the context (a value computed on a second goroutine must
 * decide the status).
 */
public final class PolyglotEmbeddingTest {
    private static final String WIRE_MIME = "application/x-graalgo-wire+json";

    private static void assertEquals(Object want, Object got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": want " + want + ", got " + got);
        }
    }

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what);
        }
    }

    /** gofrontend's wire document for one Go program (via --emit-wire). */
    private static byte[] wire(String name, String goSource) throws Exception {
        Path dir = Files.createTempDirectory("graalgo-embed");
        Path go = dir.resolve(name + ".go");
        Files.writeString(go, goSource, StandardCharsets.UTF_8);
        Path out = dir.resolve(name + ".json");
        int rc = P3SyncMain.run(new String[] {"--emit-wire=" + out, go.toString()});
        assertEquals(0, rc, "--emit-wire exit status for " + name);
        return Files.readAllBytes(out);
    }

    private static Context newContext() {
        return Context.newBuilder("go")
                .allowCreateThread(true)
                .allowExperimentalOptions(true)
                .option("engine.WarnVirtualThreadSupport", "false")
                .option("go.Gomaxprocs", "2")
                .arguments("go", new String[] {"/embedded/prog"})
                .build();
    }

    private static Source source(byte[] wire, String name) {
        return Source.newBuilder("go", ByteSequence.create(wire), name)
                .mimeType(WIRE_MIME)
                .buildLiteral();
    }

    /** verifies: GG-0005 (embedding) — main returning yields status 0. */
    public void testEvalReturnsZeroWhenMainReturns() throws Exception {
        byte[] w = wire("ok", "package main\n\nfunc main() {\n\tx := 0\n"
                + "\tfor i := 0; i < 10; i++ {\n\t\tx += i\n\t}\n\tif x != 45 {\n"
                + "\t\tpanic(x)\n\t}\n}\n");
        try (Context ctx = newContext()) {
            Value v = ctx.eval(source(w, "ok"));
            assertTrue(v.isNumber() && v.fitsInInt(), "eval result is an int: " + v);
            assertEquals(0, v.asInt(), "exit status");
        }
    }

    /**
     * verifies: GG-0005 — os.Exit(n) from a second goroutine is the status
     * read through Value (goroutines run inside the context).
     */
    public void testOsExitStatusFromGoroutine() throws Exception {
        byte[] w = wire("exit3", "package main\n\nimport \"os\"\n\n"
                + "func main() {\n\tdone := make(chan int)\n"
                + "\tgo func() { done <- 3 }()\n\tos.Exit(<-done)\n}\n");
        try (Context ctx = newContext()) {
            assertEquals(3, ctx.eval(source(w, "exit3")).asInt(), "os.Exit status");
        }
    }

    /** verifies: GG-0005 — an uncaught panic yields gc's status 2. */
    public void testPanicStatus() throws Exception {
        byte[] w = wire("boom", "package main\n\nfunc main() {\n\tpanic(\"boom\")\n}\n");
        try (Context ctx = newContext()) {
            assertEquals(2, ctx.eval(source(w, "boom")).asInt(), "panic status");
        }
    }

    /**
     * Go source text is refused (the language registers byte MIME types
     * only), and a wire document under the bundle MIME type is rejected by
     * the bundle loader (toolexec W4) instead of being misread.
     */
    public void testUnsupportedSourcesRejected() throws Exception {
        try (Context ctx = newContext()) {
            try {
                ctx.eval(Source.create("go", "package main\nfunc main() {}\n"));
                throw new AssertionError("eval of Go text did not fail");
            } catch (IllegalArgumentException e) {
                assertTrue(e.getMessage().contains("binary"), "binary-only message: "
                        + e.getMessage());
            }
            byte[] w = wire("bundle", "package main\n\nfunc main() {}\n");
            try {
                ctx.eval(Source.newBuilder("go", ByteSequence.create(w), "b")
                        .mimeType("application/x-graalgo-bundle").buildLiteral());
                throw new AssertionError("eval of a bundle did not fail");
            } catch (PolyglotException e) {
                assertTrue(e.isGuestException(), "guest exception: " + e);
                assertTrue(e.getMessage().contains("malformed bundle"),
                        "bundle message: " + e.getMessage());
            }
        }
    }

    /**
     * Closing a context whose program exited while another goroutine was
     * still blocked tears that goroutine down (no "did not complete all
     * polyglot threads").
     */
    public void testCloseAfterExitWithBlockedGoroutine() throws Exception {
        byte[] w = wire("leftover", "package main\n\nfunc main() {\n"
                + "\tblock := make(chan int)\n\tgo func() { <-block }()\n}\n");
        Context ctx = newContext();
        assertEquals(0, ctx.eval(source(w, "leftover")).asInt(), "exit status");
        ctx.close();
    }

    /** Runs {@code body} on a daemon thread; fails if it does not finish in time. */
    private static void within(long millis, String what, ThrowingRunnable body) throws Exception {
        java.util.concurrent.atomic.AtomicReference<Throwable> failure =
                new java.util.concurrent.atomic.AtomicReference<>();
        Thread t = Thread.ofPlatform().daemon(true).start(() -> {
            try {
                body.run();
            } catch (Throwable e) {
                failure.set(e);
            }
        });
        t.join(millis);
        if (t.isAlive()) {
            throw new AssertionError(what + " did not finish within " + millis + " ms (hang)");
        }
        if (failure.get() instanceof Exception e) {
            throw e;
        }
        if (failure.get() instanceof Error e) {
            throw e;
        }
    }

    private interface ThrowingRunnable {
        void run() throws Exception;
    }

    /**
     * A second eval in the same context: the first run's leftover
     * goroutines (one blocked on a channel, one spinning) are torn down
     * before the next run, and close() still completes. Pre-fix the first
     * run's scheduler was overwritten without teardown: its goroutine
     * threads stayed alive and close() hung/failed on them.
     */
    public void testSecondEvalTearsDownFirstRunsGoroutines() throws Exception {
        byte[] w = wire("leftover2", "package main\n\nvar n int\n\nfunc main() {\n"
                + "\tblock := make(chan int)\n\tgo func() { <-block }()\n"
                + "\tgo func() {\n\t\tfor {\n\t\t\tn++\n\t\t}\n\t}()\n}\n");
        within(60_000, "two evals + close", () -> {
            Context ctx = newContext();
            assertEquals(0, ctx.eval(source(w, "leftover2a")).asInt(), "first exit status");
            assertEquals(0, ctx.eval(source(w, "leftover2b")).asInt(), "second exit status");
            ctx.close();
        });
    }

    /**
     * One Go program per process at a time (until TR-5): with two
     * contexts both initialized, evaluating the second while the first
     * still runs fails instead of sharing the process-global scheduler.
     * Pre-fix the guard ran only at context initialization, so the second
     * eval ran concurrently on the first's runtime state.
     */
    public void testConcurrentProgramsInTwoContextsRejected() throws Exception {
        byte[] slow = wire("slow", "package main\n\nimport \"time\"\n\n"
                + "func main() {\n\ttime.Sleep(3 * time.Second)\n}\n");
        byte[] quick = wire("quick", "package main\n\nfunc main() {}\n");
        Context a = newContext();
        Context b = newContext();
        a.initialize("go");
        b.initialize("go");
        java.util.concurrent.atomic.AtomicReference<Object> first =
                new java.util.concurrent.atomic.AtomicReference<>();
        Thread runner = Thread.ofPlatform().daemon(true).start(() -> {
            try {
                first.set(a.eval(source(slow, "slow")).asInt());
            } catch (Throwable e) {
                first.set(e);
            }
        });
        try {
            Thread.sleep(1000); // the first program is sleeping in main
            try {
                b.eval(source(quick, "quick"));
                throw new AssertionError("second context ran a program concurrently");
            } catch (PolyglotException e) {
                assertTrue(String.valueOf(e.getMessage()).contains("one Go program per process"),
                        "rejection message: " + e.getMessage());
            }
            runner.join(30_000);
            assertEquals(0, first.get(), "first program unaffected");
            // Once the first program finished, the second context may run.
            assertEquals(0, b.eval(source(quick, "quick2")).asInt(), "sequential run");
        } finally {
            runner.join(30_000); // a context closes only once its eval returned
            a.close();
            b.close();
        }
    }
}
