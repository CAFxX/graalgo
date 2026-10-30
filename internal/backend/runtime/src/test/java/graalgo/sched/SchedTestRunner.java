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
 *
 * <h2>Test-writing rules (P2-M9 process patch)</h2>
 * A spawned goroutine holds a scheduler in-flight slot until it parks via
 * the scheduler ({@code parkCurrent}/{@code yieldCurrent}/{@code ioEnter}).
 * JVM-blocking inside a goroutine body WITHOUT the scheduler's knowledge
 * leaks the slot: with GOMAXPROCS=cap, an indefinite block (e.g.
 * {@code CountDownLatch.await()}, {@code BlockingQueue.take()}) deadlocks
 * admission — every other goroutine starves and the test hangs. Rules:
 * <ul>
 *   <li>NEVER JVM-block indefinitely inside a spawned goroutine. Use
 *       scheduler-aware parking ({@code parkCurrent}, channel ops,
 *       {@code GoWaitGroup}, {@code Timers.sleep}) or spin +
 *       {@code yieldCurrent()} (which releases the slot).</li>
 *   <li>Real blocking syscalls go inside {@code ioEnter()}/{@code ioExit()}
 *       (see {@code TortureBattery.legF5b}: the slot is released first).</li>
 *   <li>Bounded {@code Thread.sleep} for test pacing is tolerated
 *       (transient slot hold) but prefer deterministic probes
 *       ({@code recvWaitersCount()}, admission listeners).</li>
 *   <li>{@code CountDownLatch.await()} etc. belong on the TEST thread
 *       (which holds no slot), counted down by goroutines — never inverted.</li>
 * </ul>
 */
public final class SchedTestRunner {
    private static final List<Class<?>> TESTS = new ArrayList<>();

    static {
        // JfrRegistrationTest FIRST: it asserts ensureRegistered() initializes
        // the event classes, which is only meaningful before any scheduler
        // activity (see the class javadoc).
        TESTS.add(graalgo.jfr.JfrRegistrationTest.class);
        TESTS.add(graalgo.sched.audit.PinAuditTest.class);
        TESTS.add(graalgo.sched.SchedulerCoreTest.class);
        TESTS.add(graalgo.sched.ChannelTest.class);
        TESTS.add(graalgo.sched.ChannelDifferentialTest.class);
        TESTS.add(graalgo.sched.IoSlotsTest.class);
        TESTS.add(graalgo.sched.TimerDifferentialTest.class);
        TESTS.add(graalgo.sched.launcher.GoDebugTest.class);
        TESTS.add(graalgo.runtime.TimerTest.class);
        TESTS.add(graalgo.runtime.StackTraceTest.class);
        TESTS.add(graalgo.runtime.GuestExitTest.class);
        TESTS.add(graalgo.launcher.BatchTrapTest.class);
        TESTS.add(graalgo.runtime.FreezeGapTest.class);
        TESTS.add(graalgo.runtime.ZeroSizeSliceTest.class);
        TESTS.add(graalgo.runtime.StructSizeNestedTest.class);
        TESTS.add(graalgo.runtime.PanicOverlayDriftTest.class);
        TESTS.add(graalgo.runtime.GcInteropTest.class);
        TESTS.add(graalgo.runtime.GcDifferentialTest.class);
        TESTS.add(graalgo.runtime.GcTortureTest.class);
        TESTS.add(graalgo.runtime.GcVisibilityTest.class);
        TESTS.add(graalgo.jfr.JfrEventsTest.class);
        TESTS.add(graalgo.runtime.sync.SyncStressTest.class);
        TESTS.add(graalgo.runtime.sync.GoPoolTest.class);
        TESTS.add(graalgo.sched.WakeupModelTest.class);
        TESTS.add(graalgo.sched.WakeupRaceStressTest.class);
        TESTS.add(graalgo.sched.JfrParkCostTest.class);
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
