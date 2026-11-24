package graalgo.launcher;

import java.util.ArrayList;
import java.util.List;
import java.util.Set;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import java.util.stream.Collectors;

import org.graalvm.polyglot.Context;
import org.graalvm.polyglot.PolyglotException;
import org.graalvm.polyglot.Source;
import org.graalvm.polyglot.io.ByteSequence;

import graalgo.language.GoLanguage;

/**
 * Cancellation of a running Go program (playbook §10: cancel/close must be bounded; GG-0073:
 * the same in both goroutine modes). Runs a linked program (test/m1r/cancel/probe.go, built by
 * test/m1r/cancel.sh) whose goroutines are parked on channels and timers, and spinning; once it
 * creates its ready file, {@code Context.close(true)} from the embedder
 * thread must return within {@link #CLOSE_BUDGET_MILLIS}, the program's eval must end with a
 * cancelled {@link PolyglotException}, and no thread the run owns (carriers, drainer, goroutine
 * threads) may stay alive.
 *
 * <p>Usage: {@code CancelProbe <linked executable> <mode,...>} with modes {@code carrier} and
 * {@code thread} ({@code graalgo.sched.goroutines}). Exit 0 when every mode passes.
 */
public final class CancelProbe {
    static final long CLOSE_BUDGET_MILLIS = 2_000;

    private CancelProbe() {
    }

    public static void main(String[] args) throws Exception {
        if (args.length != 2) {
            throw new IllegalArgumentException("usage: CancelProbe <linked executable> <mode,...>");
        }
        byte[] bundle = BundleMain.readBundle(args[0]);
        int failed = 0;
        for (String mode : args[1].split(",")) {
            String verdict = probe(bundle, args[0], mode);
            System.err.println("cancel-probe " + mode + ": " + verdict);
            if (!verdict.startsWith("ok")) {
                failed++;
            }
        }
        System.err.flush();
        Runtime.getRuntime().halt(failed == 0 ? 0 : 1);
    }

    /**
     * The threads a run owns: carriers, the drainer, goroutine threads. The poll ticker, the
     * system-call pool and the network poller are process-wide daemons shared by every context
     * (started once, never per run), so they are not a run's leak.
     */
    private static Set<Thread> runtimeThreads() {
        return Thread.getAllStackTraces().keySet().stream()
                .filter(t -> t.getName().startsWith("graalgo-carrier-") || t.getName().equals("graalgo-drainer")
                        || t.getName().startsWith("goroutine-"))
                .collect(Collectors.toSet());
    }

    private static String probe(byte[] bundle, String exe, String mode) throws Exception {
        System.setProperty("graalgo.sched.goroutines", mode);
        Set<Thread> before = runtimeThreads();
        java.nio.file.Path ready = java.nio.file.Files.createTempFile("cancel-probe", ".ready");
        java.nio.file.Files.delete(ready);
        Context context = GoPolyglot.newContext(4, new String[] {exe, ready.toString()}, List.of());
        Source source = Source.newBuilder(GoLanguage.ID, ByteSequence.create(bundle), exe)
                .mimeType(GoLanguage.BUNDLE_MIME).buildLiteral();
        AtomicReference<Throwable> ended = new AtomicReference<>();
        Thread runner = Thread.ofPlatform().name("cancel-probe-eval").start(() -> {
            try {
                context.eval(source);
                ended.set(new AssertionError("eval returned normally"));
            } catch (Throwable t) {
                ended.set(t);
            }
        });
        long readyBy = System.nanoTime() + TimeUnit.SECONDS.toNanos(60);
        while (!java.nio.file.Files.exists(ready)) {
            if (System.nanoTime() > readyBy || !runner.isAlive()) {
                context.close(true);
                return "FAIL: the program never got ready (" + ended.get() + ")";
            }
            Thread.sleep(10);
        }
        java.nio.file.Files.delete(ready);
        Thread.sleep(200); // let the spinners run
        long t0 = System.nanoTime();
        context.close(true);
        long closeMillis = (System.nanoTime() - t0) / 1_000_000;
        runner.join(CLOSE_BUDGET_MILLIS);
        List<String> problems = new ArrayList<>();
        if (closeMillis > CLOSE_BUDGET_MILLIS) {
            problems.add("close(true) took " + closeMillis + " ms");
        }
        if (runner.isAlive()) {
            problems.add("eval still running after close");
        } else if (!(ended.get() instanceof PolyglotException pe && pe.isCancelled())) {
            problems.add("eval ended with " + ended.get() + ", not a cancellation");
        }
        long deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(CLOSE_BUDGET_MILLIS);
        List<Thread> leaked;
        do {
            leaked = runtimeThreads().stream().filter(t -> !before.contains(t) && t.isAlive()).toList();
            if (leaked.isEmpty()) {
                break;
            }
            Thread.sleep(10);
        } while (System.nanoTime() < deadline);
        if (!leaked.isEmpty()) {
            problems.add("threads alive after close: " + leaked.stream().map(Thread::getName).sorted().toList());
        }
        return problems.isEmpty() ? "ok, close(true) " + closeMillis + " ms" : "FAIL: " + problems;
    }
}
