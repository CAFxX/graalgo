package graalgo.launcher;

import graalgo.language.GoLanguage;
import java.io.ByteArrayOutputStream;
import java.io.OutputStream;
import java.io.PrintStream;
import java.lang.management.ManagementFactory;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;
import org.graalvm.polyglot.Context;
import org.graalvm.polyglot.PolyglotException;
import org.graalvm.polyglot.Source;
import org.graalvm.polyglot.io.ByteSequence;

/**
 * Context lifecycle leak gate (playbook §10, §15 "create, enter, cancel, close and recreate
 * loops checked for leaks"; BUG-0052). Runs the linked program test/m1r/leak/cycle.go (built by
 * test/m1r/leak.sh) in {@link #CYCLES} successive contexts of this JVM, in each goroutine mode,
 * and fails when
 *
 * <ul>
 *   <li>a run does not exit 0 (the program checks its own results);
 *   <li>a runtime thread of a closed context (carrier, drainer, timer, finalizer watcher,
 *       goroutine) is still alive;
 *   <li>the live heap after a full GC grows by more than {@link #BUDGET_BYTES_PER_CYCLE} per
 *       cycle on average after {@link #WARMUP} warm-up cycles: everything a context owns
 *       (its scheduler, goroutines, timers, heap of Go values, AST, compiled code) must become
 *       unreachable when it closes, so a long-lived embedder (a server creating a context per
 *       tenant or per job) runs in bounded memory.
 * </ul>
 *
 * <p>Usage: {@code ContextLeakProbe <linked executable> <mode,...>} with modes {@code carrier}
 * and {@code thread}.
 */
public final class ContextLeakProbe {
    private ContextLeakProbe() {}

    static final int CYCLES = Integer.getInteger("leakprobe.cycles", 12);
    static final int WARMUP = 3;
    static final long BUDGET_BYTES_PER_CYCLE = 2L << 20;
    static final long THREAD_EXIT_BUDGET_MILLIS = 5_000;
    static final boolean VERBOSE = Boolean.getBoolean("leakprobe.verbose");

    public static void main(String[] args) throws Exception {
        if (args.length != 2) {
            throw new IllegalArgumentException("usage: ContextLeakProbe <linked executable> <mode,...>");
        }
        byte[] bundle = BundleMain.readBundle(args[0]);
        Source source = Source.newBuilder(GoLanguage.ID, ByteSequence.create(bundle), args[0])
                .mimeType(GoLanguage.BUNDLE_MIME).cached(false).buildLiteral();
        boolean failed = false;
        for (String mode : args[1].split(",")) {
            String verdict = probe(source, args[0], mode);
            System.err.println("leak-probe " + mode + ": " + verdict);
            failed |= verdict.startsWith("FAIL");
        }
        System.exit(failed ? 1 : 0);
    }

    private static String probe(Source source, String exe, String mode) throws Exception {
        System.setProperty("graalgo.sched.goroutines", mode);
        List<String> problems = new ArrayList<>();
        long[] live = new long[CYCLES];
        long t0 = System.nanoTime();
        for (int i = 0; i < CYCLES; i++) {
            long tc = System.nanoTime();
            List<Thread> before = runtimeThreads();
            ByteArrayOutputStream err = new ByteArrayOutputStream();
            int status;
            try (Context context = GoPolyglot.newContextBuilder(4, new String[] {exe}, List.of())
                    .out(OutputStream.nullOutputStream())
                    .err(new PrintStream(err, true, StandardCharsets.UTF_8)).build()) {
                status = context.eval(source).asInt();
            } catch (PolyglotException e) {
                status = e.isExit() ? e.getExitStatus() : -1;
                if (!e.isExit()) {
                    problems.add("cycle " + i + ": " + e);
                }
            }
            if (status != 0) {
                problems.add("cycle " + i + ": exit status " + status + ", stderr: " + err.toString(StandardCharsets.UTF_8));
                break;
            }
            List<Thread> leaked = awaitThreads(before);
            if (!leaked.isEmpty()) {
                problems.add("cycle " + i + ": threads alive after close: "
                        + leaked.stream().map(Thread::getName).sorted().toList());
                break;
            }
            long tg = System.nanoTime();
            live[i] = liveHeap();
            if (VERBOSE) {
                System.err.println("leak-probe " + mode + " cycle " + i + ": live " + (live[i] >> 10) + " KB, run "
                        + (tg - tc) / 1_000_000 + " ms, gc " + (System.nanoTime() - tg) / 1_000_000 + " ms");
            }
        }
        long ms = (System.nanoTime() - t0) / 1_000_000;
        if (!problems.isEmpty()) {
            return "FAIL: " + problems;
        }
        // Minima over three cycles at each end: a closed context's last goroutine threads may
        // still be leaving Truffle's weak thread tables at one measurement (transient, not a
        // leak), while a retained context stays in every later one.
        long start = Math.min(live[WARMUP - 1], Math.min(live[WARMUP], live[WARMUP + 1]));
        long end = Math.min(live[CYCLES - 1], Math.min(live[CYCLES - 2], live[CYCLES - 3]));
        long perCycle = (end - start) / (CYCLES - 2 - WARMUP);
        String evidence = String.format("%d cycles in %d ms, live heap %d KB after cycles %d-%d, %d KB after"
                + " cycles %d-%d, %+d KB/cycle (budget %d KB)", CYCLES, ms, start >> 10, WARMUP, WARMUP + 2,
                end >> 10, CYCLES - 2, CYCLES, perCycle >> 10, BUDGET_BYTES_PER_CYCLE >> 10);
        return perCycle > BUDGET_BYTES_PER_CYCLE ? "FAIL: live heap grows, " + evidence : "ok, " + evidence;
    }

    /** Runtime threads a context may start; the process-wide daemons are not among them. */
    private static List<Thread> runtimeThreads() {
        List<Thread> out = new ArrayList<>();
        for (Thread t : Thread.getAllStackTraces().keySet()) {
            String n = t.getName();
            if (n.startsWith("graalgo-carrier-") || n.equals("graalgo-drainer") || n.equals("graalgo-timer")
                    || n.equals("graalgo-finalizers") || n.startsWith("goroutine-")) {
                out.add(t);
            }
        }
        return out;
    }

    private static List<Thread> awaitThreads(List<Thread> before) throws InterruptedException {
        long deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(THREAD_EXIT_BUDGET_MILLIS);
        List<Thread> leaked;
        do {
            leaked = runtimeThreads().stream().filter(t -> !before.contains(t) && t.isAlive()).toList();
            if (leaked.isEmpty()) {
                break;
            }
            Thread.sleep(2);
        } while (System.nanoTime() < deadline);
        return leaked;
    }

    /** Used heap after full collections (G1 System.gc() is a full, compacting GC). */
    private static long liveHeap() {
        var mem = ManagementFactory.getMemoryMXBean();
        long used = Long.MAX_VALUE;
        for (int i = 0; i < 2; i++) {
            System.gc();
            used = Math.min(used, mem.getHeapMemoryUsage().getUsed());
        }
        return used;
    }
}
