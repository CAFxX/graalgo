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
 *       ({@code test/differential/typeparam-exclusions.yaml}); no stale
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

    /**
     * P1-7 (M6 remediation): mechanical guard against silent exclusion-list
     * growth. The acceptance gate ("zero unexplained divergences") is only
     * as strong as the exclusion list — adding an entry with any valid
     * reason silently shrinks the compared set. This bound is the honest
     * count after the M6 re-triage (222); growing the list beyond it fails
     * the test, forcing the worker to bump this constant *with a reason*
     * in the same change. Shrinking the list (pruning stale exclusions)
     * stays free.
     *
     * M7 remediation (R1 P1-1/R2 P1-3): +2 for the generic variadic-packing
     * compile tests issue51840.go and issue51925.go, now loud WS D scope
     * errors (v1 has no packing adaptation); they previously built by
     * accident.
     */
    private static final int MAX_EXCLUSIONS = 224;

    /** Expected version of the exclusion list (bumped on material changes). */
    private static final String EXPECTED_EXCLUSIONS_VERSION = "4";

    private static Path repoDir() {
        return resolveRepoDir(
                System.getProperty("graalgo.repo.dir"),
                Path.of(System.getProperty("user.dir")));
    }

    /**
     * Resolves the repo root robustly by looking for the test/differential
     * directory marker.
     */
    static Path resolveRepoDir(String prop, Path cwd) {
        if (prop != null && !prop.isBlank()) {
            Path p = Path.of(prop);
            if (!p.isAbsolute()) {
                p = cwd.resolve(p);
            }
            p = p.toAbsolutePath().normalize();
            if (!isRepoDir(p)) {
                throw new IllegalStateException(
                        "graalgo.repo.dir=" + prop + " does not resolve to the repo root "
                                + "(resolved: " + p + ")");
            }
            return p;
        }
        Path base = cwd.toAbsolutePath().normalize();
        for (Path c = base; c != null; c = c.getParent()) {
            if (isRepoDir(c)) {
                return c;
            }
        }
        throw new IllegalStateException(
                "cannot locate repo root from working directory " + base
                        + "; set -Dgraalgo.repo.dir=<dir>");
    }

    /** True when p is the repo root (layout markers). */
    private static boolean isRepoDir(Path p) {
        return Files.isDirectory(p.resolve("test").resolve("differential"))
                && Files.isDirectory(p.resolve("internal").resolve("backend"));
    }

    private static Path repoRoot() {
        Path root = repoDir();
        Path dh = root.resolve("test").resolve("differential");
        if (!Files.isDirectory(dh)) {
            throw new IllegalStateException(
                    "diffharness directory missing at " + dh
                            + " (repoDir=" + repoDir() + "); repo layout assumption broken");
        }
        return root;
    }

    private static Path diffharnessDir() {
        return repoRoot().resolve("test").resolve("differential");
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

    /**
     * Minimal YAML reader for the flat exclusion-list format (no deps).
     * Mirrors the Go {@code LoadExclusions} parser's semantics (P2-8):
     * field order is insignificant, {@code detail:} is optional (defaults
     * to ""), entries flush at the next {@code - file:} or EOF. The two
     * readers are conformance-tested against the same fixture (see
     * {@code test/differential/testdata/exclusions-edge.yaml}).
     */
    static Map<String, String[]> readExclusions(Path yaml) throws IOException {
        Map<String, String[]> out = new HashMap<>();
        String file = null, reason = null, detail = null;
        for (String line : Files.readAllLines(yaml, StandardCharsets.UTF_8)) {
            String t = line.trim();
            if (t.startsWith("- file:")) {
                if (file != null) {
                    out.put(file, new String[]{reason, detail == null ? "" : detail});
                }
                file = t.substring("- file:".length()).trim();
                reason = null;
                detail = null;
            } else if (t.startsWith("reason:") && file != null) {
                reason = t.substring("reason:".length()).trim();
            } else if (t.startsWith("detail:") && file != null) {
                detail = t.substring("detail:".length()).trim();
            }
        }
        if (file != null) {
            out.put(file, new String[]{reason, detail == null ? "" : detail});
        }
        return out;
    }

    /** Reads the {@code version:} line of the exclusion list (GG-0033). */
    static String readExclusionsVersion(Path yaml) throws IOException {
        for (String line : Files.readAllLines(yaml, StandardCharsets.UTF_8)) {
            String t = line.trim();
            if (t.startsWith("version:")) {
                return t.substring("version:".length()).trim();
            }
        }
        return "";
    }

    /** Strips one pair of surrounding double quotes, if present. */
    private static String unquote(String s) {
        if (s != null && s.length() >= 2 && s.startsWith("\"") && s.endsWith("\"")) {
            return s.substring(1, s.length() - 1);
        }
        return s;
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
                // P1-7: every exclusion carries a human-readable
                // justification — an empty detail is unauditable.
                if (unquote(entry[1]).trim().isEmpty()) {
                    problems.add(name + ": empty exclusion detail (GG-0033: every exclusion must say why)");
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
        // P1-7: guard against silent exclusion-list growth (see MAX_EXCLUSIONS).
        int excludedCount = 0;
        for (int i = 0; i < files.size(); i++) {
            Json.Object f = (Json.Object) files.get(i);
            if (f.string("class", "scan.files").startsWith("exclude")) {
                excludedCount++;
            }
        }
        if (excludedCount > MAX_EXCLUSIONS) {
            problems.add("exclusion list grew to " + excludedCount
                    + " (bound " + MAX_EXCLUSIONS + "): bump MAX_EXCLUSIONS with a reason, not silently");
        }
        String version = readExclusionsVersion(exclusionsFile());
        if (!EXPECTED_EXCLUSIONS_VERSION.equals(version)) {
            problems.add("exclusion list version is " + version + ", expected "
                    + EXPECTED_EXCLUSIONS_VERSION + " (bump on material list changes)");
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
        pb.environment().put("REPO_DIR", repoDir().toAbsolutePath().toString());
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
        // P2-14: if the adapter died before writing the report, say so with
        // its log — don't let a NoSuchFileException on the missing JSON
        // mask the real failure.
        if (p.exitValue() != 0) {
            throw new AssertionError("typeparam run exited " + p.exitValue()
                    + " (adapter log above)");
        }
        if (!Files.isRegularFile(reportOut)) {
            throw new AssertionError("typeparam run produced no report JSON at "
                    + reportOut + " (adapter log above)");
        }
        Json.Object report = (Json.Object) Json.parse(
                Files.readString(reportOut, StandardCharsets.UTF_8));
        Json.Object summary = report.object("summary", "report");
        long diverged = summary.longInt("diverged", "report.summary");
        if (diverged > 0 || p.exitValue() != 0) {
            Path dst = repoDir().resolve(".out").resolve("typeparam-divergences.json");
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

    // -- YAML parser conformance (P2-8) -----------------------------------
    // readExclusions must agree with Go LoadExclusions on the shared
    // fixture test/differential/testdata/exclusions-edge.yaml. Neither
    // parser unquotes detail values; both accept any field order and a
    // missing detail (defaults to "").

    public void testReadExclusionsEdgeCases() throws IOException {
        Path fixture = repoRoot().resolve("tools").resolve("diffharness")
                .resolve("testdata").resolve("exclusions-edge.yaml");
        Map<String, String[]> got = readExclusions(fixture);
        Map<String, String[]> want = Map.of(
                "a.go", new String[]{"deferred-parity", "\"canonical order\""},
                "b.go", new String[]{"backend-specific", "\"out of order\""},
                "c.go", new String[]{"deferred-parity", ""},
                "d.go", new String[]{"unspecified-behavior", "\"\""});
        if (!got.keySet().equals(want.keySet())) {
            throw new AssertionError("parsed files " + got.keySet()
                    + ", want " + want.keySet());
        }
        for (Map.Entry<String, String[]> e : want.entrySet()) {
            String[] g = got.get(e.getKey());
            String[] w = e.getValue();
            if (!g[0].equals(w[0]) || !g[1].equals(w[1])) {
                throw new AssertionError(e.getKey() + " parsed as ["
                        + g[0] + ", " + g[1] + "], want [" + w[0] + ", " + w[1] + "]");
            }
        }
        if (!"99".equals(readExclusionsVersion(fixture))) {
            throw new AssertionError("fixture version not parsed");
        }
    }

    public void testResolvePhase1DirFailsFast() throws IOException {
        Path p1 = fakePhase1();
        // The garbage cwd must not be under a phase1 tree; java.io.tmpdir is
        // routed to out/test-tmp (under the repo) by the Makefile, so use an
        // explicit system-temp path here.
        Path garbage = Files.createTempDirectory(Path.of("/tmp"), "garbage-cwd-");
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
