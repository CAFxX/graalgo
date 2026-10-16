package graalgo.test;

import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.util.ArrayList;
import java.util.List;

/**
 * Minimal red-green test runner: instantiates each registered test class and
 * invokes every public no-arg method whose name starts with "test".
 *
 * <p>Exits 0 on all-pass, 1 on any failure. Dependency-free (no JUnit in the
 * pinned toolchain); new test classes register in TESTS below.
 *
 * <p>Optional args filter by test class (binary or simple name); a filter
 * that runs zero tests exits 1 — a smoke target must never pass silently.
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
        TESTS.add(graalgo.runtime.FmtVerbMatrixTest.class);
        TESTS.add(graalgo.runtime.FreezeSignatureTest.class);
        TESTS.add(graalgo.frontend.SectionClampTest.class);
        TESTS.add(graalgo.frontend.WrapInterfaceTest.class);
        TESTS.add(graalgo.frontend.BoxingArrayStorageTest.class);
        TESTS.add(graalgo.frontend.ArrayValueSemanticsTest.class);
        TESTS.add(graalgo.frontend.AtomicCellFieldAddrTest.class);
        TESTS.add(graalgo.frontend.v2.BinaryDecoderTest.class);
    }

    /**
     * Optional class filter: with no args every registered test runs (the
     * differential {@code make test} behavior); with args, only test classes
     * whose binary or simple name matches one of the args run. This lets the
     * fast smoke path run a subset (e.g. the v2 wire-contract tests) through
     * the same red-green runner instead of a second runner.
     */
    private static boolean selected(Class<?> c, String[] args) {
        if (args.length == 0) {
            return true;
        }
        for (String a : args) {
            if (c.getName().equals(a) || c.getSimpleName().equals(a)) {
                return true;
            }
        }
        return false;
    }

    public static void main(String[] args) {
        int run = 0;
        int failed = 0;
        for (Class<?> c : TESTS) {
            if (!selected(c, args)) {
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
        // A filter that runs zero tests is a wiring bug, not a pass: fail
        // loudly instead of reporting "0 tests, 0 failures" and letting a
        // broken smoke target look green.
        if (args.length > 0 && run == 0) {
            System.out.println("FAIL: filter ran 0 tests: "
                + String.join(" ", args));
            System.exit(1);
        }
        System.out.println(run + " tests, " + failed + " failures");
        if (failed > 0) {
            System.exit(1);
        }
    }
}
