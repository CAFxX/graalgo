package graalgo.frontend;

import com.oracle.truffle.api.source.Source;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * RV18: negative tests pinning the ninth "not supported in v1" scope
 * exclusion in {@code NodeFactory.java} — range {@code =} into a
 * package-level variable ({@code rangeAssignSlot}, M7, line 5308).
 *
 * <p>Each exclusion gets two tests, mirroring
 * {@code ScopeExclusionAbortsTest} (RV-P1-03):
 * <ol>
 *   <li>an <em>abort-fires</em> test: feeding the wire document must throw
 *       {@code FrontendException} with the exclusion's named message.
 *       Removing the abort without implementing the feature makes this test
 *       fail;</li>
 *   <li>a <em>scope-guard</em> test: a nearby in-scope document must build
 *       without throwing. Widening the abort beyond its scope makes this test
 *       fail.</li>
 * </ol>
 *
 * <p>Wire resources: {@code src/test/resources/frontend/scope-excl-*.json}.
 * Both documents are unmodified gofrontend exports of small Go probes:
 * {@code e9_range_global.go} ranges with {@code =} into a package-level
 * variable, {@code control_range_assign_local.go} ranges with {@code =} into
 * a local.
 *
 * <p>Note on the triggering construct: the abort fires for a range
 * <em>header</em> {@code =} target that is a package-level variable
 * ({@code for _, g = range xs}). A body assignment
 * ({@code for x := range xs { g = x }}) is a plain assignment to a global —
 * supported in v1 — and must keep building; it is checked as scratch
 * verification, not committed here.
 *
 * <p>Retirement: this test names its work-registry item
 * ({@code rv18-rangeassign-abort-test}); when the M7 feature lands and the
 * abort is removed, the abort-fires test is replaced by a positive test and
 * the item is marked done.
 */
public final class RangeAssignGlobalAbortTest {

    private static String loadResource(String name) throws Exception {
        return Files.readString(
                Path.of("src/test/resources/frontend", name),
                StandardCharsets.UTF_8);
    }

    private static Source newSource(String fileName) {
        return Source.newBuilder("go", "package main", fileName).build();
    }

    /** Builds the wire document; returns null when it builds cleanly. */
    private static FrontendException buildOutcome(String resource) throws Exception {
        String json = loadResource(resource);
        try {
            new NodeFactory(newSource("scope-excl.go")).buildProgram(json);
        } catch (FrontendException e) {
            return e;
        }
        return null;
    }

    private static void expectAbort(String testName, String resource, String... markers)
            throws Exception {
        FrontendException e = buildOutcome(resource);
        if (e == null) {
            throw new AssertionError(testName + ": " + resource
                    + " must abort with FrontendException, but built cleanly;"
                    + " the scope exclusion may have been removed without a"
                    + " replacement test");
        }
        for (String marker : markers) {
            if (!e.getMessage().contains(marker)) {
                throw new AssertionError(testName + ": expected <" + marker
                        + "> in FrontendException, got <" + e.getMessage() + ">");
            }
        }
    }

    private static void expectBuilds(String testName, String resource) throws Exception {
        FrontendException e = buildOutcome(resource);
        if (e != null) {
            throw new AssertionError(testName + ": " + resource
                    + " is in scope and must build cleanly, but aborted with <"
                    + e.getMessage() + ">");
        }
    }

    // ---- E9: M7 — range `=` into a package-level variable, live path
    // (NodeFactory.java:5308, rangeAssignSlot) ----

    public void testRangeAssignGlobalAbortsM7() throws Exception {
        expectAbort("E9", "scope-excl-e9-range-global.json",
                "range assignment to a package-level variable", "(M7)");
    }

    public void testRangeAssignLocalBuilds() throws Exception {
        // Scope guard: range `=` into a local, non-captured variable is in
        // scope and takes the plain-slot path.
        expectBuilds("E9-guard", "scope-excl-control-range-assign-local.json");
    }
}
