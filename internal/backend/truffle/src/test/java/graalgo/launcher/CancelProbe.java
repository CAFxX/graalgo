package graalgo.launcher;

import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

import org.graalvm.polyglot.Context;
import org.graalvm.polyglot.PolyglotException;
import org.graalvm.polyglot.Source;
import org.graalvm.polyglot.io.ByteSequence;

import com.oracle.truffle.tools.profiler.CPUSampler;
import com.oracle.truffle.tools.profiler.CPUSamplerData;
import com.oracle.truffle.tools.profiler.StackTraceEntry;

import graalgo.language.GoLanguage;

/**
 * Cancellation, hard exit and thread-local actions of a running Go program (playbook §10;
 * WP-TR-AUDIT-3 of docs/arch/truffle-audit.md; GG-0073: the same in both goroutine modes).
 * Runs the linked program test/m1r/cancel/probe.go (built by test/m1r/cancel.sh), whose
 * goroutines block in every way a Go program blocks — channels, select, Mutex, WaitGroup,
 * timers, sleep, read(2) on a pipe and on stdin, inside a deferred call mid-panic — while
 * others panic and recover in a loop and others spin (plain and backward-goto loops). Once it
 * creates its ready file:
 *
 * <ul>
 *   <li>{@code park}: a {@code ThreadLocalAction} ({@link CPUSampler#takeSample}) must reach
 *       every thread of the run — carriers idle, running or blocked under a system call; in
 *       thread mode every parked goroutine thread — and a collecting sampler must record
 *       samples, most of them on time; then {@code Context.close(true)} from the embedder thread must return within
 *       {@link #CLOSE_BUDGET_MILLIS}, the eval must end cancelled, nothing may be printed by
 *       the cancelled program (no panic report), and no thread the run owns may stay alive —
 *       including a system-call thread still inside read(2).
 *   <li>{@code exit}: the program calls {@code os.Exit(3)}: the eval must end in a
 *       {@code PolyglotException} with {@code isExit()} and status 3, promptly, with the same
 *       no-leak check.
 *   <li>{@code panic} (carrier mode only): an uncaught panic is a hard exit with status 2 and
 *       gc's report on the context's error stream.
 * </ul>
 *
 * <p>Usage: {@code CancelProbe <linked executable> <mode,...>} with modes {@code carrier} and
 * {@code thread} ({@code graalgo.sched.goroutines}). Exit 0 when every case passes.
 */
public final class CancelProbe {
    static final long CLOSE_BUDGET_MILLIS = 1_000;
    /** From the program's exit point (its READY.exit file) to the end of the eval. */
    static final long EXIT_BUDGET_MILLIS = 2_000;
    /** Goroutines parked on one channel: 10k with carriers; thread mode creates a thread each. */
    static final int PARKED_CARRIER = 10_000;
    static final int PARKED_THREAD = 1_000;

    private CancelProbe() {
    }

    public static void main(String[] args) throws Exception {
        if (args.length != 2) {
            throw new IllegalArgumentException("usage: CancelProbe <linked executable> <mode,...>");
        }
        byte[] bundle = BundleMain.readBundle(args[0]);
        int failed = 0;
        for (String mode : args[1].split(",")) {
            for (String scenario : mode.equals("carrier") ? List.of("park", "exit", "panic") : List.of("park", "exit")) {
                long t0 = System.nanoTime();
                Thread watchdog = Thread.ofPlatform().daemon(true).start(() -> dumpAfter(mode + " " + scenario));
                String verdict = probe(bundle, args[0], mode, scenario);
                watchdog.interrupt();
                System.err.println("cancel-probe " + mode + " " + scenario + ": " + verdict + " ["
                        + (System.nanoTime() - t0) / 1_000_000 + " ms]");
                if (!verdict.startsWith("ok")) {
                    failed++;
                }
            }
        }
        System.err.flush();
        Runtime.getRuntime().halt(failed == 0 ? 0 : 1);
    }

    /** A case still running after {@link #HANG_DUMP_SECONDS}: print every thread's stack (diagnosis). */
    private static void dumpAfter(String what) {
        try {
            Thread.sleep(TimeUnit.SECONDS.toMillis(HANG_DUMP_SECONDS));
        } catch (InterruptedException e) {
            return;
        }
        StringBuilder sb = new StringBuilder("cancel-probe " + what + ": still running after " + HANG_DUMP_SECONDS
                + " s; threads:\n");
        for (Map.Entry<Thread, StackTraceElement[]> e : Thread.getAllStackTraces().entrySet()) {
            sb.append('"').append(e.getKey().getName()).append("\" ").append(e.getKey().getState()).append('\n');
            for (StackTraceElement f : e.getValue()) {
                sb.append("    at ").append(f).append('\n');
            }
        }
        System.err.print(sb);
        System.err.flush();
    }

    static final long HANG_DUMP_SECONDS = 30;

    /**
     * The threads a run owns: carriers, the drainer, goroutine threads, and system-call threads
     * inside a call. The poll ticker and the network poller are process-wide daemons shared by
     * every context (started once, never per run), and an idle system-call pool thread is a
     * cached pool member, so none of them is a run's leak.
     */
    private static Set<Thread> runtimeThreads() {
        Set<Thread> out = new HashSet<>();
        for (Map.Entry<Thread, StackTraceElement[]> e : Thread.getAllStackTraces().entrySet()) {
            String n = e.getKey().getName();
            if (n.startsWith("graalgo-carrier-") || n.equals("graalgo-drainer") || n.startsWith("goroutine-")) {
                out.add(e.getKey());
            } else if (n.startsWith("graalgo-syscall-")) {
                for (StackTraceElement f : e.getValue()) {
                    if (f.getClassName().equals("graalgo.syscall.BlockingCall") && f.getMethodName().equals("run")) {
                        out.add(e.getKey());
                        break;
                    }
                }
            }
        }
        return out;
    }

    /** The run's polyglot threads: carriers or goroutine threads (not the drainer). */
    private static Set<Thread> polyglotThreads() {
        Set<Thread> out = new HashSet<>();
        for (Thread t : runtimeThreads()) {
            String n = t.getName();
            if (n.startsWith("graalgo-carrier-") || n.startsWith("goroutine-")) {
                out.add(t);
            }
        }
        return out;
    }

    private static String probe(byte[] bundle, String exe, String mode, String scenario) throws Exception {
        System.setProperty("graalgo.sched.goroutines", mode);
        Set<Thread> before = runtimeThreads();
        Path ready = Files.createTempFile("cancel-probe", ".ready");
        Files.delete(ready);
        int parked = Integer.getInteger("cancelprobe.parked", mode.equals("carrier") ? PARKED_CARRIER : PARKED_THREAD);
        ByteArrayOutputStream errBytes = new ByteArrayOutputStream();
        PrintStream err = new PrintStream(errBytes, true, StandardCharsets.UTF_8);
        Context context = GoPolyglot.newContextBuilder(4,
                new String[] {exe, ready.toString(), scenario, Integer.toString(parked)}, List.of())
                .err(err).build();
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
        while (!Files.exists(ready)) {
            if (System.nanoTime() > readyBy || !runner.isAlive()) {
                context.close(true);
                return "FAIL: the program never got ready (" + ended.get() + "; stderr: " + errBytes + ")";
            }
            Thread.sleep(5);
        }
        Files.delete(ready);
        List<String> problems = new ArrayList<>();
        String evidence;
        if (scenario.equals("park")) {
            Thread.sleep(100); // let the spinners run
            evidence = threadLocalActions(context, runner, mode.equals("thread") ? parked : 0, problems);
            long t0 = System.nanoTime();
            context.close(true);
            long closeMillis = (System.nanoTime() - t0) / 1_000_000;
            evidence += ", close(true) " + closeMillis + " ms";
            runner.join(CLOSE_BUDGET_MILLIS);
            if (closeMillis > CLOSE_BUDGET_MILLIS) {
                problems.add("close(true) took " + closeMillis + " ms");
            }
            if (runner.isAlive()) {
                problems.add("eval still running after close");
            } else if (!(ended.get() instanceof PolyglotException pe && pe.isCancelled())) {
                problems.add("eval ended with " + ended.get() + ", not a cancellation");
            }
            if (errBytes.size() != 0) {
                problems.add("the cancelled program printed: " + errBytes);
            }
        } else {
            int want = scenario.equals("exit") ? 3 : 2;
            Path exiting = Path.of(ready + ".exit");
            while (!Files.exists(exiting) && runner.isAlive() && System.nanoTime() < readyBy) {
                Thread.sleep(1);
            }
            long exitAt = System.nanoTime();
            Files.deleteIfExists(exiting);
            runner.join(10 * EXIT_BUDGET_MILLIS);
            long exitMillis = (System.nanoTime() - exitAt) / 1_000_000;
            evidence = "eval ended " + exitMillis + " ms after the exit point";
            if (runner.isAlive()) {
                problems.add("eval still running " + exitMillis + " ms after the exit point");
                context.close(true);
            } else if (exitMillis > EXIT_BUDGET_MILLIS) {
                problems.add("the exit took " + exitMillis + " ms (budget " + EXIT_BUDGET_MILLIS + ")");
            } else if (!(ended.get() instanceof PolyglotException pe && pe.isExit() && pe.getExitStatus() == want)) {
                problems.add("eval ended with " + describe(ended.get()) + ", not a hard exit with status " + want);
            }
            if (scenario.equals("panic") && !errBytes.toString(StandardCharsets.UTF_8).contains("panic: boom")) {
                problems.add("no panic report on stderr: " + errBytes);
            }
            try {
                context.close();
            } catch (PolyglotException pe) {
                if (!pe.isExit() && !pe.isCancelled()) {
                    throw pe; // closing an exited (cancelled) context reports the exit (cancel) again
                }
            }
        }
        long deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(CLOSE_BUDGET_MILLIS);
        List<Thread> leaked;
        do {
            leaked = runtimeThreads().stream().filter(t -> !before.contains(t) && t.isAlive()).toList();
            if (leaked.isEmpty()) {
                break;
            }
            Thread.sleep(5);
        } while (System.nanoTime() < deadline);
        if (!leaked.isEmpty()) {
            problems.add("threads alive after the run: " + leaked.stream().map(Thread::getName).sorted().toList());
        }
        return problems.isEmpty() ? "ok, " + evidence : "FAIL: " + problems + " (" + evidence + ")";
    }

    private static String describe(Throwable t) {
        if (t instanceof PolyglotException pe) {
            return "PolyglotException(exit=" + pe.isExit() + (pe.isExit() ? " status=" + pe.getExitStatus() : "")
                    + ", cancelled=" + pe.isCancelled() + ", " + pe.getMessage() + ")";
        }
        return String.valueOf(t);
    }

    /**
     * Thread-local actions reach every polyglot thread of the run: one synchronous sample
     * ({@link CPUSampler#takeSample}) must hear back from each carrier (carrier mode) or each
     * goroutine thread, parked ones included (thread mode), and from the eval thread blocked in
     * its exit wait; a collecting sampler then runs for a while and must record samples.
     */
    private static String threadLocalActions(Context context, Thread runner, int minThreads, List<String> problems)
            throws Exception {
        CPUSampler sampler = CPUSampler.find(context.getEngine());
        Set<Thread> want = polyglotThreads();
        want.add(runner);
        Map<Thread, List<StackTraceEntry>> sample = sampler.takeSample(5, TimeUnit.SECONDS);
        Set<Thread> missing = new HashSet<>(want);
        missing.removeAll(sample.keySet());
        missing.removeIf(t -> !t.isAlive()); // a spare carrier may retire meanwhile
        // Thread mode: goroutine threads are virtual, invisible to getAllStackTraces, so count
        // them instead — every parked goroutine is a thread the action must reach.
        if (minThreads > 0 && sample.size() < minThreads) {
            problems.add("a thread-local action reached " + sample.size() + " threads, fewer than the "
                    + minThreads + " parked goroutine threads");
        }
        if (!missing.isEmpty()) {
            problems.add("a thread-local action did not reach " + missing.size() + " of " + want.size()
                    + " threads, e.g. " + missing.stream().map(Thread::getName).sorted().limit(5).toList());
        }
        sampler.setPeriod(10);
        sampler.setCollecting(true);
        Thread.sleep(300);
        sampler.setCollecting(false);
        long samples = 0;
        long missed = 0;
        for (CPUSamplerData d : sampler.getDataList()) {
            samples += d.getSamples();
            missed += d.missedSamples();
        }
        // A sample is missed when some thread answers later than the period: a timing metric on
        // a shared host. Before WP-TR-AUDIT-3 every sample was missed (no thread answered).
        if (samples == 0 || missed >= samples) {
            problems.add("the CPU sampler recorded " + samples + " samples and missed " + missed);
        }
        return "action reached " + sample.size() + " threads, sampler " + samples + " samples / " + missed
                + " missed";
    }
}
