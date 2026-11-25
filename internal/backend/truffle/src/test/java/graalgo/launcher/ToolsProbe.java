package graalgo.launcher;

import com.oracle.truffle.api.source.SourceSection;
import com.oracle.truffle.tools.profiler.CPUSampler;
import com.oracle.truffle.tools.profiler.CPUSamplerData;
import com.oracle.truffle.tools.profiler.ProfilerNode;
import graalgo.language.GoLanguage;
import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.TreeSet;
import java.util.concurrent.atomic.AtomicInteger;
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
 *   <li>{@code spin}: a collecting {@link CPUSampler} (10 ms period) over the whole run records
 *       {@code main.fib}, with its section in {@code main.go}, called from
 *       {@code main.main.func1}: Go names and file:line on the sampled stacks, in both modes (in
 *       thread mode a sampled program used to never finish: a sampler interrupt lost a
 *       goroutine's wake, BUG-0050, fixed).
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
            try {
                count(bundle, args[0], mode, problems);
                spin(bundle, args[0], mode, problems);
            } catch (Throwable t) {
                problems.add(mode + ": " + t);
            }
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

    private static void spin(byte[] bundle, String exe, String mode, List<String> problems) {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        String got = null;
        long samples = 0;
        try (Context c = context(exe, "spin", out)) {
            CPUSampler sampler = CPUSampler.find(c.getEngine());
            sampler.setPeriod(10);
            sampler.setCollecting(true);
            c.eval(source(bundle, exe));
            sampler.setCollecting(false);
            for (CPUSamplerData d : sampler.getDataList()) {
                samples += d.getSamples();
                for (Collection<ProfilerNode<CPUSampler.Payload>> roots : d.getThreadData().values()) {
                    for (ProfilerNode<CPUSampler.Payload> n : roots) {
                        got = got != null ? got : fibUnder(n);
                    }
                }
            }
        }
        System.out.println(mode + " spin: " + samples + " samples, " + got);
        if (got == null) {
            problems.add(mode + " spin: no sample of main.fib (main.go section) called from main.main.func1 in "
                    + samples + " samples");
        }
    }

    /** "main.fib main.go:LINE under CALLER" for the first main.fib node under main.main.func1 in n's tree. */
    private static String fibUnder(ProfilerNode<CPUSampler.Payload> n) {
        SourceSection s = n.getSourceSection();
        ProfilerNode<CPUSampler.Payload> p = n.getParent();
        if (n.getRootName().equals("main.fib") && s != null && s.getSource().getName().endsWith("/main.go") && p != null
                && p.getRootName().equals("main.main.func1")) {
            return "main.fib main.go:" + s.getStartLine() + " under " + p.getRootName();
        }
        for (ProfilerNode<CPUSampler.Payload> c : n.getChildren()) {
            String r = fibUnder(c);
            if (r != null) {
                return r;
            }
        }
        return null;
    }
}
