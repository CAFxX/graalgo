package graalgo.launcher;

import graalgo.language.GoLanguage;
import graalgo.syscall.ProcessStdio;
import java.io.BufferedReader;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.PrintStream;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import org.graalvm.polyglot.Context;
import org.graalvm.polyglot.PolyglotException;
import org.graalvm.polyglot.Source;
import org.graalvm.polyglot.io.ByteSequence;

/**
 * The test gates' batch runner (docs/dev/test-profile.md, round 3; test/lib/batchrun.sh): runs
 * many programs linked by {@code graalgo-tool} one after another in this JVM, each in a fresh
 * {@link Context} built exactly as {@link BundleMain} builds it (production runs one context per
 * program; the JVM start, class loading and HotSpot warm-up are what a gate saves). Each program
 * gets what a shell gives a child: its stdin, stdout and stderr files on descriptors 0, 1 and 2,
 * its working directory, its {@code os.Args} ({@link ProcessStdio}); the environment is the
 * batch's (programs that need another one are not batched). Its exit status is the one
 * {@link BundleMain} would exit with: the program's status, the hard exit's, 2 with a
 * {@code graalgo:} line for an internal error, and 124 (coreutils {@code timeout}) when its
 * timeout cancels it.
 *
 * <p>Usage: {@code BundleBatchMain <manifest> <results>}. The manifest has one program per line,
 * tab-separated: {@code id cwd argv0 stdin stdout stderr timeout-seconds [arg...]} ({@code argv0}
 * as the program is invoked, relative to {@code cwd} or absolute). The results file gets
 * {@code START id} before a program runs and {@code END id status} after; a program without its
 * {@code END} line (the JVM died, or its cancel did not finish within {@link #CLOSE_GRACE_S}:
 * this JVM halts with status 3) is the caller's to rerun in its own process, together with every
 * program after it. A program never takes the batch down silently.
 */
public final class BundleBatchMain {
    private BundleBatchMain() {}

    /** How long a cancelled or exited context may take to close before the batch gives up. */
    static final int CLOSE_GRACE_S = 20;
    /** {@code timeout(1)}'s status for a command it timed out. */
    static final int TIMED_OUT = 124;

    record Entry(String id, String cwd, String argv0, String stdin, String stdout, String stderr,
            int timeoutSeconds, List<String> args) {
        static Entry parse(String line) {
            String[] f = line.split("\t", -1);
            if (f.length < 7) {
                throw new IllegalArgumentException("manifest line has " + f.length + " fields: " + line);
            }
            return new Entry(f[0], f[1], f[2], f[3], f[4], f[5], Integer.parseInt(f[6]),
                    List.of(Arrays.copyOfRange(f, 7, f.length)));
        }

        String exe() {
            Path p = Path.of(argv0);
            Path abs = p.isAbsolute() ? p : Path.of(cwd).resolve(p);
            try {
                // as the launcher stub: os.Executable(), symbolic links resolved
                return abs.toRealPath().toString();
            } catch (java.io.IOException e) {
                return abs.toString();
            }
        }
    }

    public static void main(String[] args) throws IOException {
        if (args.length != 2) {
            System.err.println("usage: BundleBatchMain <manifest> <results>");
            Runtime.getRuntime().halt(2);
        }
        List<Entry> entries = new ArrayList<>();
        try (BufferedReader r = Files.newBufferedReader(Path.of(args[0]), StandardCharsets.UTF_8)) {
            for (String line; (line = r.readLine()) != null;) {
                if (!line.isEmpty()) {
                    entries.add(Entry.parse(line));
                }
            }
        }
        // The batch's own descriptors, restored after every program.
        int[] saved = {ProcessStdio.dup(0), ProcessStdio.dup(1), ProcessStdio.dup(2)};
        try (PrintStream results = new PrintStream(new FileOutputStream(args[1], true), true,
                StandardCharsets.UTF_8)) {
            for (Entry e : entries) {
                results.println("START " + e.id());
                int status = runOne(e, saved);
                results.println("END " + e.id() + " " + status);
            }
        }
        Runtime.getRuntime().halt(0);
    }

    private static int runOne(Entry e, int[] saved) throws IOException {
        System.out.flush();
        System.err.flush();
        int in = ProcessStdio.open(e.stdin(), ProcessStdio.O_RDONLY);
        int out = ProcessStdio.open(e.stdout(), ProcessStdio.O_WRITE_TRUNC);
        int err = ProcessStdio.open(e.stderr(), ProcessStdio.O_WRITE_TRUNC);
        ProcessStdio.dup2(in, 0);
        ProcessStdio.dup2(out, 1);
        ProcessStdio.dup2(err, 2);
        ProcessStdio.close(in);
        ProcessStdio.close(out);
        ProcessStdio.close(err);
        ProcessStdio.chdir(e.cwd());
        int status;
        try {
            status = run(e);
        } finally {
            System.out.flush();
            System.err.flush();
            for (int fd = 0; fd < 3; fd++) {
                ProcessStdio.dup2(saved[fd], fd);
            }
        }
        return status;
    }

    /** One program, as {@link BundleMain#main} runs it. */
    private static int run(Entry e) {
        byte[] bundle;
        try {
            bundle = BundleMain.readBundle(e.exe());
        } catch (IOException ex) {
            return internalError(ex);
        }
        String[] argv = new String[e.args().size() + 1];
        argv[0] = e.argv0();
        for (int i = 0; i < e.args().size(); i++) {
            argv[i + 1] = e.args().get(i);
        }
        Context context = GoPolyglot.newContext(BundleMain.gomaxprocs(), argv, List.of());
        AtomicBoolean timedOut = new AtomicBoolean();
        CountDownLatch done = new CountDownLatch(1);
        Thread watchdog = Thread.ofPlatform().daemon().name("batch-watchdog").start(() -> {
            try {
                if (!done.await(e.timeoutSeconds(), TimeUnit.SECONDS)) {
                    timedOut.set(true);
                    closeOrHalt(context, true);
                }
            } catch (InterruptedException ignored) {
                // the program finished
            }
        });
        int status;
        try {
            Source source = Source.newBuilder(GoLanguage.ID, ByteSequence.create(bundle), e.exe())
                    .mimeType(GoLanguage.BUNDLE_MIME).buildLiteral();
            status = context.eval(source).asInt();
        } catch (PolyglotException ex) {
            if (timedOut.get()) {
                status = TIMED_OUT;
            } else if (ex.isExit()) {
                // os.Exit(n), an uncaught panic or a fatal error: the context's hard exit.
                status = ex.getExitStatus();
            } else {
                System.err.println("graalgo: " + ex.getMessage());
                status = 2;
            }
        } catch (RuntimeException | Error ex) {
            status = timedOut.get() ? TIMED_OUT : internalError(ex);
        } finally {
            done.countDown();
        }
        // As BundleMain's try-with-resources: the context closes after the run.
        if (!timedOut.get()) {
            closeOrHalt(context, false);
        }
        try {
            watchdog.join();
        } catch (InterruptedException ex) {
            Thread.currentThread().interrupt();
        }
        return timedOut.get() ? TIMED_OUT : status;
    }

    /**
     * Closes the context (cancelling it when {@code cancel}); if that does not finish within
     * {@link #CLOSE_GRACE_S} the process is no longer trustworthy for the next program: halt, and
     * the caller reruns this program and the rest in their own processes.
     */
    private static void closeOrHalt(Context context, boolean cancel) {
        Thread closer = Thread.ofPlatform().daemon().name("batch-close").start(() -> {
            try {
                context.close(cancel);
            } catch (IllegalStateException ex) {
                // still executing in another thread: cancel it
                context.close(true);
            } catch (PolyglotException ex) {
                // an exit or cancel raised while closing: the context is closed
            }
        });
        try {
            closer.join(TimeUnit.SECONDS.toMillis(CLOSE_GRACE_S));
        } catch (InterruptedException ex) {
            Thread.currentThread().interrupt();
        }
        if (closer.isAlive()) {
            Runtime.getRuntime().halt(3);
        }
    }

    /** An exception out of the runner, reported as the JVM reports one out of main (status 1). */
    private static int internalError(Throwable t) {
        StringWriter w = new StringWriter();
        t.printStackTrace(new PrintWriter(w));
        System.err.print("Exception in thread \"main\" " + w);
        return 1;
    }
}
