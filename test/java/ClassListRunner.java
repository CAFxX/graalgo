import java.lang.reflect.Method;
import java.lang.reflect.Modifier;

/**
 * Runs the named backend unit-test classes with the same contract as the
 * per-module runners (graalgo.test.Runner, graalgo.sched.SchedTestRunner,
 * graalgo.iface.IfaceTestRunner): every public no-arg method whose name
 * starts with "test" is invoked on a fresh instance. Used by the root
 * Makefile's test-java-fast / test-java-slow targets to split the suites by
 * cost (see the class lists there), with per-test wall-clock timing so the
 * split stays evidence-based.
 *
 * <p>Exit status: 0 all pass, 1 any failure, 3 a named class cannot be
 * loaded (infrastructure error, never reported as a pass).
 */
public final class ClassListRunner {
    public static void main(String[] args) {
        int run = 0;
        int failed = 0;
        long suiteStart = System.nanoTime();
        for (String name : args) {
            Class<?> c;
            try {
                c = Class.forName(name);
            } catch (ClassNotFoundException | LinkageError e) {
                System.out.println("INFRA cannot load test class " + name + ": " + e);
                System.exit(3);
                return;
            }
            long classStart = System.nanoTime();
            int methods = 0;
            for (Method m : c.getMethods()) {
                if (!m.getName().startsWith("test") || m.getParameterCount() != 0
                        || !Modifier.isPublic(m.getModifiers())
                        || Modifier.isStatic(m.getModifiers())) {
                    continue;
                }
                run++;
                methods++;
                long t0 = System.nanoTime();
                try {
                    m.invoke(c.getDeclaredConstructor().newInstance());
                    System.out.printf("PASS %s.%s (%d ms)%n", c.getSimpleName(), m.getName(),
                            (System.nanoTime() - t0) / 1_000_000);
                } catch (Exception e) {
                    failed++;
                    Throwable cause = e.getCause() != null ? e.getCause() : e;
                    System.out.printf("FAIL %s.%s (%d ms): %s%n", c.getSimpleName(), m.getName(),
                            (System.nanoTime() - t0) / 1_000_000, cause);
                }
            }
            if (methods == 0) {
                System.out.println("INFRA " + name + " has no test methods");
                System.exit(3);
            }
            System.out.printf("CLASS %s %d ms%n", name, (System.nanoTime() - classStart) / 1_000_000);
        }
        System.out.printf("%d tests, %d failures, %d ms%n", run, failed,
                (System.nanoTime() - suiteStart) / 1_000_000);
        System.exit(failed > 0 ? 1 : 0);
    }
}
