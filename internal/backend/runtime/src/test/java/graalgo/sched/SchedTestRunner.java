package graalgo.sched;

import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

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
        TESTS.add(graalgo.runtime.SelectSelfClaimTest.class);
        TESTS.add(graalgo.sched.ChannelDifferentialTest.class);
        TESTS.add(graalgo.sched.IoSlotsTest.class);
        TESTS.add(graalgo.sched.TimerDifferentialTest.class);
        TESTS.add(graalgo.sched.launcher.GoDebugTest.class);
        TESTS.add(graalgo.launcher.LauncherOverlayTest.class);
        TESTS.add(graalgo.runtime.TimerTest.class);
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

    /**
     * Per-test timeout, in seconds (NEW-P1-5). Each test method runs on its
     * own worker thread and is bounded by {@code Future.get}; on timeout
     * the runner records {@code FAIL <test>: timed out} and continues with
     * the next test instead of dying with the suite.
     *
     * <p>The value is deliberately larger than the subprocess bound: the
     * subprocess harnesses ({@code GcDifferentialTest},
     * {@code GoDebugTest}) kill a stalled grandchild after 120 s and drain
     * its output for at most 10 s more, so a stalled subprocess always
     * fails cleanly on its own timeout -- with {@code destroyForcibly} run
     * and no orphaned grandchild -- before this timeout can race it. And
     * two in-process stalls (2 x 150 s) plus the measured ~230 s healthy
     * suite still fit inside PinAudit's 600 s child watchdog, so the suite
     * now degrades to per-test FAILs instead of a child kill that discards
     * all later results.
     */
    private static final long PER_TEST_TIMEOUT_SECONDS = 150;

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
                String name = c.getSimpleName() + "." + m.getName();
                // One fresh worker per test, never a shared pool: a test
                // that hangs past its timeout must not queue-block the
                // tests after it. The worker is a daemon thread and is
                // abandoned on timeout, so a stuck test leaks at most one
                // thread and cannot hold the JVM open. Instantiation runs
                // on the worker too, so a hanging constructor is bounded
                // as well.
                ExecutorService worker = Executors.newSingleThreadExecutor(r -> {
                    Thread t = new Thread(r, "sched-test-" + name);
                    t.setDaemon(true);
                    return t;
                });
                Future<?> f = worker.submit(() -> {
                    m.invoke(c.getDeclaredConstructor().newInstance());
                    return null;
                });
                try {
                    f.get(PER_TEST_TIMEOUT_SECONDS, TimeUnit.SECONDS);
                    System.out.println("PASS " + name);
                } catch (TimeoutException e) {
                    f.cancel(true);
                    failed++;
                    System.out.println("FAIL " + name + ": timed out after "
                            + PER_TEST_TIMEOUT_SECONDS + "s");
                } catch (ExecutionException e) {
                    failed++;
                    // Unwrap the two layers Future.get adds
                    // (ExecutionException -> InvocationTargetException) to
                    // reach the test's own failure, exactly as the direct
                    // m.invoke call used to report it.
                    Throwable cause = e.getCause();
                    if (cause instanceof InvocationTargetException
                            && cause.getCause() != null) {
                        cause = cause.getCause();
                    }
                    System.out.println("FAIL " + name + ": " + cause);
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                    f.cancel(true);
                    failed++;
                    System.out.println("FAIL " + name + ": runner interrupted");
                } finally {
                    worker.shutdownNow();
                }
            }
        }
        System.out.println(run + " tests, " + failed + " failures");
        if (failed > 0) {
            System.exit(1);
        }
    }
}
