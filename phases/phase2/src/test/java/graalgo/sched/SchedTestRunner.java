package graalgo.sched;

import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.util.ArrayList;
import java.util.List;

/**
 * Minimal red-green test runner (mirrors Phase-1 {@code graalgo.test.Runner}):
 * instantiates each registered test class and invokes every public no-arg
 * method whose name starts with "test". Exits 0 on all-pass, 1 on any
 * failure. New test classes register in TESTS below.
 */
public final class SchedTestRunner {
    private static final List<Class<?>> TESTS = new ArrayList<>();

    static {
        TESTS.add(graalgo.sched.SchedulerCoreTest.class);
        TESTS.add(graalgo.sched.ChannelTest.class);
        TESTS.add(graalgo.sched.ChannelDifferentialTest.class);
        TESTS.add(graalgo.sched.IoSlotsTest.class);
        TESTS.add(graalgo.sched.TimerDifferentialTest.class);
        TESTS.add(graalgo.runtime.TimerTest.class);
        TESTS.add(graalgo.runtime.FreezeGapTest.class);
        TESTS.add(graalgo.runtime.GcInteropTest.class);
        TESTS.add(graalgo.runtime.GcDifferentialTest.class);
        TESTS.add(graalgo.runtime.GcTortureTest.class);
        TESTS.add(graalgo.runtime.GcVisibilityTest.class);
    }

    public static void main(String[] args) {
        int run = 0;
        int failed = 0;
        for (Class<?> c : TESTS) {
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
}
