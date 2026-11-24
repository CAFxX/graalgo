package graalgo.conventions;

import graalgo.conventions.NodeConventions.Violation;
import java.io.IOException;
import java.net.URISyntaxException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.TreeSet;
import java.util.function.Predicate;

/**
 * The Truffle coding conventions (truffle-design.md §16) over the compiled new node package
 * ({@code graalgo.go.*}), the shared runtime ({@code graalgo.sched.*}, {@code graalgo.syscall.*},
 * {@code graalgo.safepoint.*}) and the classes of {@code graalgo.runtime}, {@code graalgo.values}
 * and {@code graalgo.language} they reference directly.
 *
 * <p>Every finding must be in {@link #ALLOWLIST} (rule, location, reason), and every allowlist
 * entry must still be a finding: an entry the code no longer needs fails as stale, so the list
 * only shrinks (check_ratchets.py's {@code java-conventions} counter holds it to its base).
 * The fixture package {@code graalgo.conventions.fixtures} plants one violation of every rule
 * and proves each can fail. Class files only, one pass, no JVM per case: well under a second.
 */
public final class NodeConventionsTest {
    static final String ALLOWLIST = "internal/backend/truffle/src/test/java/graalgo/conventions/allowlist.txt";

    /** graalgo.sched.nodes is v1's {@code go} statement node (deleted at M3, toolexec-design §9.1). */
    private static final Predicate<String> SCOPE = n -> n.startsWith("graalgo.go.")
        || (n.startsWith("graalgo.sched.") && !n.startsWith("graalgo.sched.nodes."))
        || n.startsWith("graalgo.syscall.") || n.startsWith("graalgo.safepoint.");
    private static final Predicate<String> SHARED = n -> n.startsWith("graalgo.runtime.") || n.startsWith("graalgo.values.")
        || n.startsWith("graalgo.language.");

    private static Path root(Class<?> c) {
        try {
            return Path.of(c.getProtectionDomain().getCodeSource().getLocation().toURI());
        } catch (URISyntaxException e) {
            throw new AssertionError(e);
        }
    }

    private static Path repo() {
        String repo = System.getProperty("graalgo.repo.dir");
        if (repo == null) {
            throw new AssertionError("graalgo.repo.dir is not set");
        }
        return Path.of(repo);
    }

    static List<Violation> tree() {
        List<Path> roots = new ArrayList<>();
        roots.add(root(graalgo.go.nodes.N.class));
        Path interop = repo().resolve("internal/backend/interop/out/classes");
        if (Files.isDirectory(interop)) {
            roots.add(interop);
        }
        return NodeConventions.check(roots, SCOPE, SHARED);
    }

    static List<Violation> fixtures() {
        return NodeConventions.check(List.of(root(NodeConventionsTest.class)),
            n -> n.startsWith("graalgo.conventions.fixtures."), n -> false);
    }

    /** rule + " " + location → reason. */
    static Map<String, String> allowlist() throws IOException {
        Map<String, String> r = new TreeMap<>();
        for (String line : Files.readAllLines(repo().resolve(ALLOWLIST))) {
            String l = line.strip();
            if (l.isEmpty() || l.startsWith("#")) {
                continue;
            }
            int bar = l.indexOf(" | ");
            if (bar < 0 || l.substring(bar + 3).isBlank()) {
                throw new AssertionError("allowlist entry without a reason: " + l);
            }
            String key = l.substring(0, bar).strip();
            if (!NodeConventions.RULES.containsKey(key.split(" ")[0])) {
                throw new AssertionError("allowlist entry names an unknown rule: " + l);
            }
            if (r.put(key, l.substring(bar + 3).strip()) != null) {
                throw new AssertionError("duplicate allowlist entry: " + key);
            }
        }
        return r;
    }

    public void testTreeMatchesAllowlist() throws IOException {
        long t0 = System.nanoTime();
        List<Violation> found = tree();
        Map<String, String> allowed = allowlist();
        List<String> unlisted = new ArrayList<>();
        Set<String> keys = new TreeSet<>();
        for (Violation v : found) {
            keys.add(v.key());
            if (!allowed.containsKey(v.key())) {
                unlisted.add(v.toString());
            }
        }
        List<String> stale = new ArrayList<>();
        for (String k : allowed.keySet()) {
            if (!keys.contains(k)) {
                stale.add(k);
            }
        }
        if (!unlisted.isEmpty() || !stale.isEmpty()) {
            StringBuilder sb = new StringBuilder("Truffle conventions (truffle-design.md §16):");
            for (String u : unlisted) {
                sb.append("\n  VIOLATION ").append(u).append(" — ").append(NodeConventions.RULES.get(u.split(" ")[0]));
            }
            for (String s : stale) {
                sb.append("\n  STALE allowlist entry (fixed: delete it from ").append(ALLOWLIST).append("): ").append(s);
            }
            throw new AssertionError(sb.toString());
        }
        System.out.printf("  conventions: %d findings, all allowlisted, %.0f ms%n", found.size(), (System.nanoTime() - t0) / 1e6);
    }

    /** Every planted violation, and nothing else: each rule can fail, none fires on the conforming plants. */
    private static final List<String> PLANTED = List.of(
        "B2-frame Fixtures$FrameBoundary#slow",
        "B3-boundary Fixtures$Hostile#execute",
        "B6-invalidate Fixtures$WritesFinal#execute",
        "C1-final Fixtures$NotFinal",
        "D2-limit Fixtures$Dsl#cached",
        "D4-idempotent Fixtures$Dsl#isSmall",
        "D6-neverdefault Fixtures$Dsl#make",
        "G1-calltarget Fixtures$LocationlessCall#enter",
        "N1-cfdims Fixtures$BadArrays#table",
        "N1-children Fixtures$BadArrays#kids",
        "N1-field Fixtures$BadFields#count",
        "N1-field Fixtures$BadFields#flag",
        "N1-sync Fixtures$Synchronized#touch",
        "N1-volatile Fixtures$BadFields#flag",
        "N2-switch Fixtures$SwitchOnKind#execute",
        "N4-explode Fixtures$ExplodePlain#execute",
        "N4-unexploded Fixtures$LoopChildren#execute",
        "N8-typecase Fixtures$TypeCase#execute",
        "R1-record Fixtures$Pair",
        "S1-static Fixtures$MutableStatic#counter",
        "S2-static-read Fixtures$ReadsStatic#execute");

    public void testEveryRuleFailsOnItsPlant() {
        Set<String> got = new TreeSet<>();
        for (Violation v : fixtures()) {
            got.add(v.key().replace("graalgo.conventions.fixtures.", ""));
        }
        Set<String> want = new TreeSet<>(PLANTED);
        if (!got.equals(want)) {
            Set<String> missing = new TreeSet<>(want);
            missing.removeAll(got);
            Set<String> extra = new TreeSet<>(got);
            extra.removeAll(want);
            throw new AssertionError("fixture findings differ: missing " + missing + ", unexpected " + extra);
        }
        Set<String> rules = new TreeSet<>();
        for (String p : PLANTED) {
            rules.add(p.split(" ")[0]);
        }
        if (!rules.equals(NodeConventions.RULES.keySet())) {
            Set<String> unplanted = new TreeSet<>(NodeConventions.RULES.keySet());
            unplanted.removeAll(rules);
            throw new AssertionError("rules without a planted violation: " + unplanted);
        }
    }

    /** The allowlist parser rejects entries without a reason or with an unknown rule. */
    public void testAllowlistEntriesCarryReasons() throws IOException {
        for (Map.Entry<String, String> e : allowlist().entrySet()) {
            if (e.getValue().length() < 10) {
                throw new AssertionError("allowlist reason too short for " + e.getKey() + ": " + e.getValue());
            }
        }
    }

    /** Main for the sweep: prints every finding (optionally only unlisted ones). */
    public static void main(String[] args) throws IOException {
        Map<String, String> allowed = args.length > 0 && args[0].equals("--unlisted") ? allowlist() : Map.of();
        for (Violation v : args.length > 0 && args[0].equals("--fixtures") ? fixtures() : tree()) {
            if (!allowed.containsKey(v.key())) {
                System.out.println(v);
            }
        }
    }
}
