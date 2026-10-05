package graalgo.sched.audit;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.concurrent.TimeUnit;
import java.util.stream.Stream;

import jdk.jfr.consumer.RecordedEvent;
import jdk.jfr.consumer.RecordedFrame;
import jdk.jfr.consumer.RecordingFile;

/**
 * P2-M7 pinning audit (WS G, GG-0057): runs the conformance corpus with the
 * {@code jdk.VirtualThreadPinned} JFR event enabled and classifies every pin.
 *
 * <p>Background: under option C a pinned carrier only transiently consumes
 * dumb-pool capacity (fairness is drainer-owned), so this audit is the
 * cgo-budget check — it proves no goroutine path pins its carrier outside
 * the budgeted set. On JDK 25 (JEP 491) {@code synchronized} no longer pins;
 * the remaining pinning class is park-while-in-JNI-critical (or FFM/JNI
 * native). graalgo has no JNI/FFM on goroutine paths (verified by grep gate
 * in {@code make check}), so the expected set is currently empty: any pin
 * on a {@code goroutine-*} thread is UNEXPECTED and fails the audit.
 *
 * <p>Usage:
 * <pre>
 *   PinAudit --corpus &lt;dir&gt;... [--gmp N] [--tests] [--allow &lt;file&gt;] [--out &lt;report.md&gt;]
 * </pre>
 * <ul>
 *   <li>{@code --corpus}: Go source dirs; every {@code *.go} is run through
 *       {@code graalgo.sched.launcher.SchedMain} in a child JVM with the pin
 *       event enabled. Programs that fail to build/run are reported as
 *       SKIPPED (their pin count is still collected — a crash before any
 *       pin is 0 pins, not a pass).</li>
 *   <li>{@code --tests}: additionally run the phase-2 unit/differential
 *       suite ({@code graalgo.sched.SchedTestRunner}) in a child JVM with
 *       the pin event enabled. This covers the channel/select/timer/GC
 *       paths that have no Go-source form yet (Java-translated corpus).</li>
 *   <li>{@code --allow}: allowlist file, one {@code &lt;thread-prefix&gt;|&lt;frame-substring&gt;}
 *       per line. A pin matching an entry is EXPECTED (budgeted). The
 *       default (no file) allows nothing.</li>
 * </ul>
 * Exit 0 iff zero unexpected pins; exit 1 otherwise (or on tool error).
 * The JFR plumbing uses inline {@code -XX:StartFlightRecording} event
 * settings — a bare custom .jfc silently enables nothing (P2-M7 spike).
 */
public final class PinAudit {

    /** One classified pin event. */
    static final class Pin {
        final String program;
        final String thread;
        final List<String> frames;
        final boolean expected;
        final String allowMatch;

        Pin(String program, String thread, List<String> frames,
            boolean expected, String allowMatch) {
            this.program = program;
            this.thread = thread;
            this.frames = frames;
            this.expected = expected;
            this.allowMatch = allowMatch;
        }
    }

    public static void main(String[] args) throws Exception {
        List<Path> corpusDirs = new ArrayList<>();
        boolean runTests = false;
        Path allowFile = null;
        Path outFile = null;
        int gmp = 2;
        for (int i = 0; i < args.length; i++) {
            switch (args[i]) {
                case "--corpus" -> corpusDirs.add(Paths.get(args[++i]));
                case "--tests" -> runTests = true;
                case "--allow" -> allowFile = Paths.get(args[++i]);
                case "--out" -> outFile = Paths.get(args[++i]);
                case "--gmp" -> gmp = Integer.parseInt(args[++i]);
                default -> throw new IllegalArgumentException("unknown arg: " + args[i]);
            }
        }
        if (corpusDirs.isEmpty() && !runTests) {
            throw new IllegalArgumentException("nothing to audit: pass --corpus and/or --tests");
        }
        List<String> allow = allowFile == null ? List.of()
                : Files.readAllLines(allowFile, StandardCharsets.UTF_8).stream()
                        .map(String::trim).filter(s -> !s.isEmpty() && !s.startsWith("#"))
                        .toList();

        Path work = Files.createTempDirectory("pinaudit");
        List<Pin> pins = new ArrayList<>();
        List<String> skipped = new ArrayList<>();
        List<String> unreadable = new ArrayList<>();
        int programs = 0;

        List<Path> programsList = new ArrayList<>();
        for (Path dir : corpusDirs) {
            try (Stream<Path> s = Files.walk(dir)) {
                s.filter(p -> p.toString().endsWith(".go")).sorted().forEach(programsList::add);
            }
        }
        for (Path prog : programsList) {
            programs++;
            Path jfr = work.resolve("p" + programs + ".jfr");
            int exit = runChild(jfr, gmp,
                    new String[]{"--gmp=" + gmp, prog.toString()});
            if (exit != 0) {
                skipped.add(prog + " (exit " + exit + ")");
            }
            try {
                pins.addAll(readPins(jfr, prog.toString(), allow));
            } catch (IOException e) {
                // Corrupt/unreadable recording: do not fail the whole audit,
                // but record it separately (pin count unknown, not zero).
                unreadable.add(prog + " (unreadable JFR: " + e.getMessage() + ")");
            }
            Files.deleteIfExists(jfr);
        }
        boolean testsUnreadable = false;
        boolean testsFailed = false;
        if (runTests) {
            programs++;
            Path jfr = work.resolve("tests.jfr");
            int exit = runChild(jfr, gmp, new String[]{"__TESTS__"});
            if (exit != 0) {
                skipped.add("SchedTestRunner (exit " + exit + ")");
                // Reviewer1 P1-1: the --tests leg is the pin-relevant
                // coverage; a non-zero exit means the suite did not run
                // clean, so PASS would be vacuous.
                testsFailed = true;
            }
            try {
                pins.addAll(readPins(jfr, "<SchedTestRunner>", allow));
            } catch (IOException e) {
                unreadable.add("SchedTestRunner (unreadable JFR: " + e.getMessage() + ")");
                testsUnreadable = true;
            }
            Files.deleteIfExists(jfr);
        }

        long unexpected = pins.stream().filter(p -> !p.expected).count();
        long expected = pins.size() - unexpected;

        // P2-M7 reviewer P2-2 / reviewer1 P1-1: fail closed on systemic
        // recording failure AND on a failed tests leg. Unreadable JFR means
        // "pin count unknown", not "0 pins"; a failed suite means the
        // pin-relevant paths were not exercised.
        boolean infraFail = testsUnreadable || testsFailed
                || (!unreadable.isEmpty()
                        && (double) unreadable.size() / programs > 0.05);

        StringBuilder md = new StringBuilder();
        md.append("# P2-M7 pinning audit report\n\n");
        md.append("- Programs run: ").append(programs).append("\n");
        md.append("- Skipped (non-zero exit, pin count still collected): ")
                .append(skipped.size()).append("\n");
        for (String s : skipped) {
            md.append("  - ").append(s).append("\n");
        }
        md.append("- Unreadable recordings (pin count UNKNOWN, not zero): ")
                .append(unreadable.size()).append("\n");
        for (String s : unreadable) {
            md.append("  - ").append(s).append("\n");
        }
        md.append("- Pin events total: ").append(pins.size())
                .append(" (expected ").append(expected)
                .append(", UNEXPECTED ").append(unexpected).append(")\n\n");
        md.append("## Classification\n\n");
        md.append("Expected = matches an allowlist entry (budgeted off-carrier ")
                .append("FFM/file-I/O per GG-0057). The default allowlist is empty: ")
                .append("graalgo has no JNI/FFM on goroutine paths.\n\n");
        for (Pin p : pins) {
            md.append("### ").append(p.expected ? "EXPECTED" : "UNEXPECTED")
                    .append(" pin — ").append(p.program).append("\n");
            md.append("- thread: `").append(p.thread).append("`\n");
            if (p.allowMatch != null) {
                md.append("- allowlist match: `").append(p.allowMatch).append("`\n");
            }
            md.append("- stack (top 12 frames):\n");
            p.frames.stream().limit(12).forEach(f -> md.append("  - `").append(f).append("`\n"));
            md.append("\n");
        }
        if (pins.isEmpty()) {
            md.append("No `jdk.VirtualThreadPinned` events on the conformance corpus.\n");
        }
        String report = md.toString();
        System.out.print(report);
        String verdict = "AUDIT_RESULT=" + (unexpected == 0 && !infraFail ? "PASS" : "FAIL")
                + " unexpected=" + unexpected + " unreadable=" + unreadable.size();
        System.out.println(verdict);
        // Append the verdict to the archived report (reviewer1 P2-6).
        if (outFile != null) {
            Files.writeString(outFile, report + "\n" + verdict + "\n",
                    StandardCharsets.UTF_8);
            System.out.println("report written to " + outFile);
        }
        if (unexpected > 0 || infraFail) {
            if (infraFail) {
                System.out.println("INFRA_FAIL: testsFailed=" + testsFailed
                        + " testsUnreadable=" + testsUnreadable
                        + " unreadable=" + unreadable.size() + "/" + programs);
            }
            System.exit(1);
        }
        // Clean up the temp dir (reviewer1 nit: was littered).
        try (Stream<Path> s = Files.walk(work)) {
            s.sorted(Comparator.reverseOrder()).forEach(p -> {
                try {
                    Files.deleteIfExists(p);
                } catch (IOException ignored) {
                }
            });
        } catch (IOException ignored) {
        }
    }

    /**
     * Runs one child JVM with the pin event enabled. {@code mainArgs[0] ==
     * "__TESTS__"} selects the test runner instead of SchedMain.
     */
    private static int runChild(Path jfr, int gmp, String[] mainArgs)
            throws IOException, InterruptedException {
        String java = System.getProperty("java.home") + "/bin/java";
        String cp = System.getProperty("java.class.path");
        List<String> cmd = new ArrayList<>();
        cmd.add(java);
        cmd.add("--enable-native-access=ALL-UNNAMED");
        cmd.add("-Xss32m");
        cmd.add("-XX:StartFlightRecording:filename=" + jfr.toAbsolutePath()
                + ",jdk.VirtualThreadPinned#enabled=true"
                + ",jdk.VirtualThreadPinned#stackTrace=true");
        cmd.add("-cp");
        if (mainArgs[0].equals("__TESTS__")) {
            // The test runner lives in out/test-classes, which is not on
            // PinAudit's own classpath. The caller passes it via
            // -Dpinaudit.test.cp (see the pin-audit Makefile target).
            String testCp = System.getProperty("pinaudit.test.cp");
            if (testCp == null || testCp.isEmpty()) {
                throw new IllegalStateException(
                        "pinaudit.test.cp not set (required for --tests)");
            }
            cmd.add(testCp);
            cmd.add("graalgo.sched.SchedTestRunner");
        } else {
            cmd.add(cp);
            cmd.add("graalgo.sched.launcher.SchedMain");
            cmd.addAll(List.of(mainArgs));
        }
        ProcessBuilder pb = new ProcessBuilder(cmd);
        pb.redirectOutput(ProcessBuilder.Redirect.DISCARD);
        pb.redirectError(ProcessBuilder.Redirect.DISCARD);
        Process p = pb.start();
        boolean done = p.waitFor(5, TimeUnit.MINUTES);
        if (!done) {
            p.destroyForcibly();
            return -1;
        }
        return p.exitValue();
    }

    /** Reads pin events from one recording and classifies them. Package-visible for tests. */
    static List<Pin> readPins(Path jfr, String program, List<String> allow)
            throws IOException {
        List<Pin> out = new ArrayList<>();
        if (!Files.exists(jfr)) {
            return out;
        }
        try (RecordingFile rf = new RecordingFile(jfr)) {
            while (rf.hasMoreEvents()) {
                RecordedEvent e = rf.readEvent();
                if (!e.getEventType().getName().equals("jdk.VirtualThreadPinned")) {
                    continue;
                }
                String thread = e.getThread() == null ? "<no-thread>"
                        : String.valueOf(e.getThread().getJavaName());
                List<String> frames = new ArrayList<>();
                if (e.getStackTrace() != null) {
                    for (RecordedFrame f : e.getStackTrace().getFrames()) {
                        frames.add(f.getMethod().getType().getName().replace('/', '.')
                                + "." + f.getMethod().getName());
                    }
                }
                String match = allowMatch(thread, frames, allow);
                // Only goroutine carrier pins matter for the budget; the
                // allowlist gates the expected set. Anything else is
                // unexpected by default (fail closed).
                boolean expected = match != null;
                out.add(new Pin(program, thread, frames, expected, match));
            }
        }
        return out;
    }

    private static String allowMatch(String thread, List<String> frames,
            List<String> allow) {
        for (String entry : allow) {
            int bar = entry.indexOf('|');
            String threadPrefix = bar < 0 ? entry : entry.substring(0, bar);
            String frameSub = bar < 0 ? "" : entry.substring(bar + 1);
            if (!thread.startsWith(threadPrefix)) {
                continue;
            }
            if (frameSub.isEmpty()
                    || frames.stream().anyMatch(f -> f.contains(frameSub))) {
                return entry;
            }
        }
        return null;
    }
}
