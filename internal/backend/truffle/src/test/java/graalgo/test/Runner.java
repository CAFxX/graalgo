package graalgo.test;

import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

/**
 * Minimal red-green test runner: instantiates each registered test class and
 * invokes every public no-arg method whose name starts with "test".
 *
 * <p>Exits 0 on all-pass, 1 on any failure. Dependency-free (no JUnit in the
 * pinned toolchain); new test classes register in TESTS below.
 *
 * <p>Unit gate (review P1-4): {@code --skip=ClassA,ClassB} runs every
 * registered class except the named ones (simple names). The per-push
 * {@code test-unit} target skips the corpus/differential classes, which
 * stay covered by the weekly full suite ({@code make test}). An unknown
 * skip name fails loudly (exit 2): a typo must never silently run zero
 * tests. No args means all classes, as before.
 */
public final class Runner {
    private static final List<Class<?>> TESTS = new ArrayList<>();

    static {
        TESTS.add(graalgo.launcher.MainTest.class);
        TESTS.add(graalgo.frontend.StraightLineCorpusTest.class);
        TESTS.add(graalgo.frontend.ArithmeticCorpusTest.class);
        TESTS.add(graalgo.frontend.ComplexBinaryOpTest.class);
        TESTS.add(graalgo.frontend.ControlFlowCorpusTest.class);
        TESTS.add(graalgo.frontend.CompositeCorpusTest.class);
        TESTS.add(graalgo.frontend.PanicCorpusTest.class);
        TESTS.add(graalgo.frontend.GenericsCorpusTest.class);
        TESTS.add(graalgo.frontend.FrontendRobustnessTest.class);
        TESTS.add(graalgo.frontend.VarsCorpusTest.class);
        TESTS.add(graalgo.runtime.BuiltinsCorpusTest.class);
        TESTS.add(graalgo.runtime.SliceGrowthTest.class);
        TESTS.add(graalgo.frontend.TypeparamDifferentialTest.class);
        TESTS.add(graalgo.values.ValuesTest.class);
        TESTS.add(graalgo.values.CallSiteProbesTest.class);
        TESTS.add(graalgo.values.CellIdentityTest.class);
        TESTS.add(graalgo.runtime.FreezeSignatureTest.class);
        TESTS.add(graalgo.frontend.SectionClampTest.class);
        TESTS.add(graalgo.frontend.WrapInterfaceTest.class);
        TESTS.add(graalgo.frontend.BoxingArrayStorageTest.class);
        TESTS.add(graalgo.frontend.ArrayValueSemanticsTest.class);
        TESTS.add(graalgo.frontend.AtomicCellFieldAddrTest.class);
        TESTS.add(graalgo.frontend.CommaOkLvalueTest.class);
        TESTS.add(graalgo.frontend.v2.BinaryDecoderTest.class);
    }

    public static void main(String[] args) {
        Set<String> skip = parseSkip(args);
        int run = 0;
        int failed = 0;
        for (Class<?> c : TESTS) {
            if (skip.contains(c.getSimpleName())) {
                continue;
            }
            for (Method m : c.getMethods()) {
                if (!m.getName().startsWith("test") || m.getParameterCount() != 0
                        || !Modifier.isPublic(m.getModifiers())) {
                    continue;
                }
                run++;
                try {
                    m.invoke(c.getDeclaredConstructor().newInstance());
                    System.out.println("PASS " + c.getSimpleName() + "." + m.getName());
                } catch (Exception e) {
                    failed++;
                    Throwable cause = e.getCause() != null ? e.getCause() : e;
                    System.out.println("FAIL " + c.getSimpleName() + "." + m.getName()
                        + ": " + cause);
                }
            }
        }
        System.out.println(run + " tests, " + failed + " failures");
        if (failed > 0) {
            System.exit(1);
        }
    }

    private static Set<String> parseSkip(String[] args) {
        Set<String> skip = new HashSet<>();
        for (String arg : args) {
            if (!arg.startsWith("--skip=")) {
                System.err.println("usage: Runner [--skip=ClassA,ClassB]");
                System.exit(2);
            }
            for (String name : arg.substring("--skip=".length()).split(",")) {
                if (!name.isEmpty()) {
                    skip.add(name);
                }
            }
        }
        Set<String> known = new HashSet<>();
        for (Class<?> c : TESTS) {
            known.add(c.getSimpleName());
        }
        for (String name : skip) {
            if (!known.contains(name)) {
                System.err.println("unknown --skip class: " + name);
                System.exit(2);
            }
        }
        return skip;
    }
}
