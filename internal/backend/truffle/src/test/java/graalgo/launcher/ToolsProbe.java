package graalgo.launcher;

import com.oracle.truffle.tools.profiler.CPUSampler;
import com.oracle.truffle.tools.profiler.StackTraceEntry;
import graalgo.language.GoLanguage;
import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.TreeSet;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;
import org.graalvm.polyglot.Context;
import org.graalvm.polyglot.Source;
import org.graalvm.polyglot.io.ByteSequence;
import org.graalvm.polyglot.management.ExecutionListener;

/**
 * Truffle tools on a running Go program (WP-TR-AUDIT-6, playbook §12), driven by
 * test/m1r/tools.sh over test/m1r/tools/probe.go in each goroutine mode:
 * <ul>
 *   <li>{@code count}: an {@link ExecutionListener} on statements and roots sees exactly the
 *       177 calls of {@code main.fib} (roots, located at its declaration, line 13) and their
 *       354 statements (the {@code if} on line 14, a {@code return} on line 15 or 17), with the
 *       statement wrappers in place (the program writes its result, 55, to the process's stdout);
 *   <li>{@code spin}: {@link CPUSampler#takeSample} on the running program finds a goroutine
 *       inside {@code main.fib} with its section in {@code main.go}, called from
 *       {@code main.main.func1}: Go names and file:line on the sampled stack.
 * </ul>
 * Usage: {@code ToolsProbe <linked executable> <modes>}, modes a comma-separated subset of
 * {@code carrier,thread}. Exit 0 when every check passes.
 */
public final class ToolsProbe {
    private ToolsProbe() {}

    public static void main(String[] args) throws Exception {
        byte[] bundle = BundleMain.readBundle(args[0]);
        List<String> problems = new ArrayList<>();
        for (String mode : args[1].split(",")) {
            System.setProperty("graalgo.sched.goroutines", mode);
            count(bundle, args[0], mode, problems);
            spin(bundle, args[0], mode, problems);
        }
        for (String p : problems) {
            System.out.println("FAIL " + p);
        }
        System.out.flush();
        Runtime.getRuntime().halt(problems.isEmpty() ? 0 : 1);
    }

    private static Context context(String exe, String what, ByteArrayOutputStream out) {
        return GoPolyglot.newContextBuilder(4, new String[] {exe, what}, List.of())
                .out(new PrintStream(out, true, StandardCharsets.UTF_8)).build();
    }

    private static Source source(byte[] bundle, String exe) {
        return Source.newBuilder(GoLanguage.ID, ByteSequence.create(bundle), exe).mimeType(GoLanguage.BUNDLE_MIME)
                .buildLiteral();
    }

    private static void count(byte[] bundle, String exe, String mode, List<String> problems) {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        AtomicInteger roots = new AtomicInteger();
        AtomicInteger statements = new AtomicInteger();
        TreeSet<String> where = new TreeSet<>();
        try (Context c = context(exe, "count", out)) {
            ExecutionListener l = ExecutionListener.newBuilder().statements(true).roots(true)
                    .rootNameFilter("main.fib"::equals).onEnter(e -> {
                        String at = e.getLocation().getSource().getName().replaceAll(".*/", "") + ":"
                                + e.getLocation().getStartLine();
                        synchronized (where) {
                            where.add((e.isRoot() ? "root " : "stmt ") + at);
                        }
                        (e.isRoot() ? roots : statements).incrementAndGet();
                    }).attach(c.getEngine());
            c.eval(source(bundle, exe));
            l.close();
        }
        String got = "roots=" + roots + " statements=" + statements + " at=" + where;
        String want = "roots=177 statements=354 at=[root main.go:13, stmt main.go:14, stmt main.go:15, stmt main.go:17]";
        System.out.println(mode + " count: " + got);
        if (!got.equals(want)) {
            problems.add(mode + " count: got " + got + ", want " + want);
        }
    }

    private static void spin(byte[] bundle, String exe, String mode, List<String> problems) throws Exception {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        AtomicReference<String> found = new AtomicReference<>();
        try (Context c = context(exe, "spin", out)) {
            CPUSampler sampler = CPUSampler.find(c.getEngine());
            AtomicReference<Throwable> ended = new AtomicReference<>();
            Thread runner = Thread.ofPlatform().name("tools-probe-eval").start(() -> {
                try {
                    c.eval(source(bundle, exe));
                } catch (Throwable t) {
                    ended.set(t);
                }
            });
            long until = System.nanoTime() + TimeUnit.SECONDS.toNanos(60);
            while (found.get() == null && runner.isAlive() && System.nanoTime() < until) {
                for (Map.Entry<Thread, List<StackTraceEntry>> e : sampler.takeSample().entrySet()) {
                    List<StackTraceEntry> stack = e.getValue();
                    for (int i = 0; i + 1 < stack.size(); i++) {
                        StackTraceEntry top = stack.get(i);
                        if (top.getRootName().equals("main.fib") && top.getSourceSection() != null
                                && top.getSourceSection().getSource().getName().endsWith("/main.go")) {
                            int j = i;
                            while (j < stack.size() - 1 && stack.get(j).getRootName().equals("main.fib")) {
                                j++;
                            }
                            found.set(top.getRootName() + " main.go:" + top.getSourceSection().getStartLine()
                                    + " under " + stack.get(j).getRootName());
                            break;
                        }
                    }
                }
                Thread.onSpinWait();
            }
            runner.join(TimeUnit.SECONDS.toMillis(60));
            if (ended.get() != null) {
                problems.add(mode + " spin: eval ended with " + ended.get());
            }
        }
        String got = found.get();
        System.out.println(mode + " spin: sampled " + got);
        if (got == null || !got.startsWith("main.fib main.go:") || !got.endsWith(" under main.main.func1")) {
            problems.add(mode + " spin: no sample of main.fib under main.main.func1 with a main.go section (" + got + ")");
        }
    }
}
