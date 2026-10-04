package graalgo.frontend;

import graalgo.launcher.ToolPaths;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.nio.file.StandardCopyOption;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.TimeUnit;

/**
 * M6 acceptance: the test/typeparam differential vs stock gc.
 *
 * <p>Go's {@code test/typeparam} tree (266 files at GOROOT/test/typeparam)
 * is the pathological-generics corpus the D2 monomorphization design must
 * survive. This test drives it through the diffharness {@code typeparam}
 * adapter:
 *
 * <ul>
 *   <li>{@code testExclusionListCoversAllFiles} — every one of the 266
 *       files is either included by the adapter or carried on the
 *       versioned, reason-coded exclusion list
 *       ({@code tools/diffharness/typeparam-exclusions.yaml}); no stale
 *       entries, no unknown reason codes.
 *   <li>{@code testZeroUnexplainedDivergences} — the included files run
 *       differential gc-a vs graalgo (build parity for {@code // compile},
 *       build-failure parity for {@code // errorcheck}, full run parity
 *       for {@code // run}); any divergence fails the test with the
 *       adapter's JSON report copied to
 *       {@code phases/phase1/hidden_files/m6_typeparam_divergences.json}.
 * </ul>
 *
 * <p>Red-green-first (SPEC §5.2): the oracle is stock gc at test time —
 * the test cannot pass by construction.
 */
// verifies: GG-0033 (versioned exclusion list: every excluded test/typeparam
// file carries a reason code; the list is versioned and the test enforces
// coverage, freshness, and code validity).
// verifies: GG-0014 (M6: test/typeparam differential — zero unexplained
// divergences graalgo vs gc on the included files).
public final class TypeparamDifferentialTest {

    private static final Set<String> VALID_REASONS = Set.of(
            "backend-specific", "unspecified-behavior", "deferred-parity");

    private static Path phase1Dir() {
        return resolvePhase1Dir(
                System.getProperty("graalgo.phase1.dir"),
                Path.of(System.getProperty("user.dir")));
    }

    /**
     * Resolves the phases/phase1 directory robustly. The old code stripped
     * two levels off the raw property-or-cwd with no validation, so any
     * nonstandard working directory (e.g. phases/phase1/frontend) silently
     * produced a wrong repo root and a confusing "No such file or
     * directory" from the diffharness build. This version validates every
     * candidate against the real layout and fails fast with a clear message.
     */
    static Path resolvePhase1Dir(String prop, Path cwd) {
        if (prop != null && !prop.isBlank()) {
            Path p = Path.of(prop);
            if (!p.isAbsolute()) {
                p = cwd.resolve(p);
            }
            p = p.toAbsolutePath().normalize();
            if (!isPhase1Dir(p)) {
                throw new IllegalStateException(
                        "graalgo.phase1.dir=" + prop + " does not resolve to a phases/phase1 "
                                + "directory (resolved: " + p + ")");
            }
            return p;
        }
        Path base = cwd.toAbsolutePath().normalize();
        for (Path c = base; c != null; c = c.getParent()) {
            if (isPhase1Dir(c)) {
                return c;
            }
            Path cand = c.resolve("phases").resolve("phase1");
            if (isPhase1Dir(cand)) {
                return cand;
            }
        }
        throw new IllegalStateException(
                "cannot locate phases/phase1 from working directory " + base
                        + "; set -Dgraalgo.phase1.dir=<dir>");
    }

    /** True when p is a real phases/phase1 checkout (layout markers). */
    private static boolean isPhase1Dir(Path p) {
        Path name = p.getFileName();
        Path parent = p.getParent();
        return name != null && name.toString().equals("phase1")
                && parent != null && parent.getFileName().toString().equals("phases")
                && Files.isDirectory(p.resolve("src"))
                && Files.isDirectory(p.resolve("frontend"));
    }

    private static Path repoRoot() {
        Path root = phase1Dir().getParent().getParent();
        Path dh = root.resolve("tools").resolve("diffharness");
        if (!Files.isDirectory(dh)) {
            throw new IllegalStateException(
                    "diffharness directory missing at " + dh
                            + " (phase1Dir=" + phase1Dir() + "); repo layout assumption broken");
        }
        return root;
    }

    private static Path diffharnessDir() {
        return repoRoot().resolve("tools").resolve("diffharness");
    }

    private static Path exclusionsFile() {
        Path p = diffharnessDir().resolve("typeparam-exclusions.yaml");
        if (!Files.isRegularFile(p)) {
            throw new IllegalStateException(
                    "typeparam exclusions file missing: " + p + " (GG-0033 list not generated?)");
        }
        return p;
    }

    private static Path typeparamDir() throws Exception {
        Path go = ToolPaths.stockGo();
        Process p = new ProcessBuilder(go.toString(), "env", "GOROOT")
                .redirectErrorStream(true).start();
        String out = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
        if (!p.waitFor(60, TimeUnit.SECONDS) || p.exitValue() != 0) {
            throw new AssertionError("go env GOROOT failed: " + out);
        }
        Path tp = Paths.get(out.trim()).resolve("test").resolve("typeparam");
        if (!Files.isDirectory(tp)) {
            throw new AssertionError("no test/typeparam dir at " + tp);
        }
        return tp;
    }

    /** Builds the diffharness CLI once per JVM (to a temp dir) and returns it. */
    private static Path diffharnessBin() throws Exception {
        Path dir = Files.createTempDirectory("diffharness-bin-");
        Path bin = dir.resolve("diffharness");
        ProcessBuilder pb = new ProcessBuilder(
                ToolPaths.stockGo().toString(), "build", "-o", bin.toString(),
                "./cmd/diffharness");
        pb.directory(diffharnessDir().toFile());
        pb.environment().put("GOTOOLCHAIN", "local");
        pb.redirectErrorStream(true);
        Process p = pb.start();
        String out = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
        if (!p.waitFor(300, TimeUnit.SECONDS) || p.exitValue() != 0) {
            throw new AssertionError("building diffharness failed: " + out);
        }
        return bin;
    }

    private static Path holderBin;
    private static Path bin() throws Exception {
        if (holderBin == null) {
            holderBin = diffharnessBin();
        }
        return holderBin;
    }

    /** Minimal YAML reader for the flat exclusion-list format (no deps). */
    static Map<String, String[]> readExclusions(Path yaml) throws IOException {
        Map<String, String[]> out = new HashMap<>();
        String file = null, reason = null;
        for (String line : Files.readAllLines(yaml, StandardCharsets.UTF_8)) {
            String t = line.trim();
            if (t.startsWith("- file:")) {
                file = t.substring("- file:".length()).trim();
                reason = null;
            } else if (t.startsWith("reason:") && file != null) {
                reason = t.substring("reason:".length()).trim();
            } else if (t.startsWith("detail:") && file != null && reason != null) {
                out.put(file, new String[]{reason, t.substring("detail:".length()).trim()});
                file = null;
                reason = null;
            }
        }
        return out;
    }

    /**
     * Every test/typeparam/*.go file is either included by the adapter or
     * present on the versioned exclusion list with a valid GG-0033 reason
     * code; the list has no stale entries.
     */
    public void testExclusionListCoversAllFiles() throws Exception {
        Path tp = typeparamDir();
        Path scanOut = Files.createTempFile("typeparam-scan-", ".json");
        ProcessBuilder pb = new ProcessBuilder(bin().toString(), "typeparam", "scan",
                "--dir", tp.toString(),
                "--exclusions", exclusionsFile().toString(),
                "--out", scanOut.toString());
        pb.environment().put("GOTOOLCHAIN", "local");
        pb.redirectErrorStream(true);
        Process p = pb.start();
        String log = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
        if (!p.waitFor(120, TimeUnit.SECONDS) || p.exitValue() != 0) {
            throw new AssertionError("typeparam scan failed: " + log);
        }
        Json.Object scan = (Json.Object) Json.parse(
                Files.readString(scanOut, StandardCharsets.UTF_8));
        Json.Array files = scan.array("files", "scan");

        Map<String, String[]> exclusions = readExclusions(exclusionsFile());
        Set<String> seen = new HashSet<>();
        Map<String, Integer> byReason = new HashMap<>();
        Map<String, Integer> byClass = new HashMap<>();
        List<String> problems = new ArrayList<>();
        for (int i = 0; i < files.size(); i++) {
            Json.Object f = (Json.Object) files.get(i);
            String name = f.string("file", "scan.files");
            String cls = f.string("class", "scan.files");
            seen.add(name);
            byClass.merge(cls, 1, Integer::sum);
            boolean excluded = cls.startsWith("exclude");
            String[] entry = exclusions.get(name);
            if (excluded && entry == null) {
                problems.add(name + ": excluded by adapter (" + f.string("detail", "scan.files")
                        + ") but missing from " + exclusionsFile().getFileName());
            } else if (!excluded && entry != null) {
                problems.add(name + ": included by adapter but present on exclusion list");
            } else if (excluded) {
                if (!VALID_REASONS.contains(entry[0])) {
                    problems.add(name + ": invalid reason code " + entry[0]);
                }
                byReason.merge(entry[0] + ":" + entry[1], 1, Integer::sum);
            }
        }
        for (String name : exclusions.keySet()) {
            if (!seen.contains(name)) {
                problems.add(name + ": stale exclusion-list entry (file not in test/typeparam)");
            }
        }
        // The tree has 266 files; guard against silent shrinkage of the scan.
        if (seen.size() < 260) {
            problems.add("scan covered only " + seen.size() + " files; expected >= 260");
        }
        System.out.println("typeparam scan: classes=" + byClass);
        System.out.println("typeparam exclusions by reason: " + byReason);
        if (!problems.isEmpty()) {
            throw new AssertionError("exclusion list problems:\n  - "
                    + String.join("\n  - ", problems));
        }
    }

    /**
     * Zero unexplained divergences graalgo vs gc on the included
     * test/typeparam files.
     */
    public void testZeroUnexplainedDivergences() throws Exception {
        Path tp = typeparamDir();
        Path reportOut = Files.createTempFile("typeparam-report-", ".json");
        List<String> cmd = new ArrayList<>(List.of(bin().toString(), "typeparam", "run",
                "--dir", tp.toString(),
                "--exclusions", exclusionsFile().toString(),
                "--out", reportOut.toString(),
                "--jobs", "4"));
        ProcessBuilder pb = new ProcessBuilder(cmd);
        pb.environment().put("GOTOOLCHAIN", "local");
        pb.environment().put("DH_GO", ToolPaths.stockGo().getParent().getParent().toString());
        pb.environment().put("PHASE1_DIR", phase1Dir().toAbsolutePath().toString());
        pb.redirectErrorStream(true);
        Process p = pb.start();
        // Stream the adapter's progress to the test log.
        String log = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
        boolean done = p.waitFor(40, TimeUnit.MINUTES);
        System.out.println(log);
        if (!done) {
            p.destroyForcibly();
            throw new AssertionError("typeparam run timed out after 40 minutes");
        }
        Json.Object report = (Json.Object) Json.parse(
                Files.readString(reportOut, StandardCharsets.UTF_8));
        Json.Object summary = report.object("summary", "report");
        long diverged = summary.longInt("diverged", "report.summary");
        if (diverged > 0 || p.exitValue() != 0) {
            Path dst = phase1Dir().resolve("hidden_files")
                    .resolve("m6_typeparam_divergences.json");
            Files.createDirectories(dst.getParent());
            Files.copy(reportOut, dst, StandardCopyOption.REPLACE_EXISTING);
            Json.Array comps = report.array("comparisons", "report");
            List<String> bad = new ArrayList<>();
            for (int i = 0; i < comps.size(); i++) {
                Json.Object c = (Json.Object) comps.get(i);
                if (!c.bool("same", "report.comparisons")) {
                    Json.Array rs = c.array("reasons", "report.comparisons");
                    List<String> rlist = new ArrayList<>();
                    for (int j = 0; j < rs.size(); j++) {
                        rlist.add(((Json.Str) rs.get(j)).value());
                    }
                    bad.add(c.string("program", "report.comparisons") + " " + rlist);
                }
            }
            throw new AssertionError(diverged + " divergences (report -> "
                    + dst.getFileName() + "):\n  - " + String.join("\n  - ", bad));
        }
        System.out.println("typeparam run: " + summary.longInt("total", "report.summary")
                + " comparisons, 0 diverged, "
                + summary.longInt("skipped", "report.summary") + " excluded");
    }

    // -- Path-resolution unit tests --------------------------------------
    // The old repoRoot() stripped two levels off the raw cwd with no
    // validation; running the suite from any nonstandard directory
    // (e.g. phases/phase1/frontend) silently produced a wrong root and a
    // confusing "No such file or directory" from the diffharness build.
    // These pin the robust resolution (red: the old code fails
    // testResolvePhase1DirFromSubdir).

    private static Path fakePhase1() throws IOException {
        Path root = Files.createTempDirectory("fake-repo-");
        Path p1 = root.resolve("phases").resolve("phase1");
        Files.createDirectories(p1.resolve("src"));
        Files.createDirectories(p1.resolve("frontend"));
        return p1;
    }

    public void testResolvePhase1DirFromPhase1Cwd() throws IOException {
        Path p1 = fakePhase1();
        if (!resolvePhase1Dir(null, p1).equals(p1)) {
            throw new AssertionError("cwd=phase1 not resolved");
        }
    }

    public void testResolvePhase1DirFromSubdir() throws IOException {
        // The dead worker's case: cwd one level too deep must still find it.
        Path p1 = fakePhase1();
        Path deep = p1.resolve("frontend");
        if (!resolvePhase1Dir(null, deep).equals(p1)) {
            throw new AssertionError("cwd=phase1/frontend not resolved to " + p1);
        }
    }

    public void testResolvePhase1DirFromRepoRoot() throws IOException {
        Path p1 = fakePhase1();
        if (!resolvePhase1Dir(null, p1.getParent().getParent()).equals(p1)) {
            throw new AssertionError("cwd=repo root not resolved");
        }
    }

    public void testResolvePhase1DirProperty() throws IOException {
        Path p1 = fakePhase1();
        if (!resolvePhase1Dir(p1.toString(), Path.of("/nonexistent")).equals(p1)) {
            throw new AssertionError("absolute property not honored");
        }
        Path rel = p1.getParent().getParent().relativize(p1);
        Path got = resolvePhase1Dir(rel.toString(), p1.getParent().getParent());
        if (!got.equals(p1)) {
            throw new AssertionError("relative property not resolved against cwd: " + got);
        }
    }

    public void testResolvePhase1DirFailsFast() throws IOException {
        Path p1 = fakePhase1();
        Path garbage = Files.createTempDirectory("garbage-cwd-");
        try {
            resolvePhase1Dir(null, garbage);
            throw new AssertionError("expected IllegalStateException for garbage cwd");
        } catch (IllegalStateException expected) {
            if (!expected.getMessage().contains("graalgo.phase1.dir")) {
                throw new AssertionError("unhelpful message: " + expected.getMessage());
            }
        }
        try {
            resolvePhase1Dir("/nonexistent-phase1", p1);
            throw new AssertionError("expected IllegalStateException for bad property");
        } catch (IllegalStateException expected) {
            // expected
        }
    }
}
