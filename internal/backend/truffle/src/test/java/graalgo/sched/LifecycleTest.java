package graalgo.sched;

import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.jfr.ParkReason;
import graalgo.runtime.Goexit;
import graalgo.runtime.Panic;
import graalgo.values.GoString;

/**
 * T0.A: Go program lifecycle at the scheduler level — exit when main
 * returns, fatal exit on a panic in any goroutine, gc {@code checkdead}
 * deadlock detection (and its absence while a timer, blocking I/O or the
 * netpoller can still wake someone), and run-time GOMAXPROCS.
 *
 * <p>Red-green: each test pins one behavior the pre-T0.A scheduler lacked
 * (it awaited every goroutine, recorded panics without exiting, had no
 * deadlock check and a fixed cap); a regression makes {@link #exitOf}
 * time out or return the wrong code, or the stderr assertion fail.
 */
public final class LifecycleTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(long expected, long actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /** Result of one program run: exit code + captured stderr. */
    private record Run(int code, String stderr) {}

    /**
     * Runs {@code main} as the main goroutine of a fresh scheduler and
     * returns the requested exit code (fails after 20 s: a hang is the
     * pre-fix behavior for most of these tests). Captures System.err.
     */
    private static Run run(int gomaxprocs, java.util.function.Consumer<SchedulerCore> main)
            throws Exception {
        SchedulerCore sched = new SchedulerCore(gomaxprocs, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore prev = SchedulerCore.install(sched);
        PrintStream oldErr = System.err;
        ByteArrayOutputStream err = new ByteArrayOutputStream();
        System.setErr(new PrintStream(err, true, StandardCharsets.UTF_8));
        try {
            sched.spawnMain(() -> main.accept(sched));
            AtomicReference<Integer> code = new AtomicReference<>();
            Thread waiter = Thread.ofPlatform().daemon(true).start(() -> {
                try {
                    code.set(sched.awaitExit());
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                }
            });
            waiter.join(TimeUnit.SECONDS.toMillis(20));
            if (code.get() == null) {
                throw new AssertionError("program did not exit within 20s (hang)");
            }
            return new Run(code.get(), err.toString(StandardCharsets.UTF_8));
        } finally {
            System.setErr(oldErr);
            SchedulerCore.install(prev);
        }
    }

    private static int exitOf(Run r) {
        return r.code();
    }

    /** Parks the calling goroutine until something unparks it. */
    private static void parkForever(SchedulerCore s, String reason) {
        s.parkCurrent(null, ParkReason.CHANNEL, reason);
    }

    // ---------- main returning ----------

    /**
     * Main returning exits 0 even while other goroutines are blocked
     * forever or spinning (gc runtime.main calls exit(0)).
     */
    public void testMainReturnExitsDespiteLiveGoroutines() throws Exception {
        Run r = run(2, s -> {
            s.spawn(() -> parkForever(s, "chan receive"));
            s.spawn(() -> {
                for (;;) {
                    s.pollCurrentThread(); // a spinning goroutine
                }
            });
            s.yieldCurrent();
        });
        assertEquals(0, exitOf(r), "main return exit code");
        assertTrue(r.stderr().isEmpty(), "no output on clean exit, got: " + r.stderr());
    }

    /**
     * Exit freezes a running goroutine at its very next
     * {@code Safepoint.poll()}, even mid-stride: the JIT fast path skips
     * the full quantum check for up to STRIDE-1 polls, and must not skip
     * the exit freeze with it (integration of the lifecycle model with
     * the strided poll). The spinner positions its countdown mid-stride
     * (one full-path poll, then 10 fast polls), waits for main to request
     * exit, then polls in a loop counting completed polls: with the freeze
     * honored it completes none; a strided exit check lets ~117 through.
     */
    public void testExitFreezesAtNextStridedPoll() throws Exception {
        java.util.concurrent.atomic.AtomicLong after = new java.util.concurrent.atomic.AtomicLong();
        CountDownLatch ready = new CountDownLatch(1);
        AtomicBoolean go = new AtomicBoolean();
        Run r = run(2, s -> {
            s.spawn(() -> {
                for (int i = 0; i < 11; i++) {
                    graalgo.safepoint.Safepoint.poll();
                }
                ready.countDown();
                while (!go.get()) {
                    Thread.onSpinWait(); // no polls while waiting
                }
                for (;;) {
                    graalgo.safepoint.Safepoint.poll();
                    after.incrementAndGet();
                }
            });
            try {
                ready.await();
            } catch (InterruptedException e) {
                throw new AssertionError(e);
            }
            s.requestExit(0);
            go.set(true);
        });
        assertEquals(0, exitOf(r), "exit code");
        Thread.sleep(200); // give a non-frozen spinner time to run on
        assertEquals(0, after.get(), "polls completed after exit was requested");
    }

    // ---------- fatal panic ----------

    /** An uncaught panic in a non-main goroutine prints and exits 2 at once. */
    public void testGoroutinePanicIsFatal() throws Exception {
        Run r = run(2, s -> {
            s.spawn(() -> Panic.throwPanic(GoString.fromUtf8("boom")));
            parkForever(s, "chan receive"); // main never returns by itself
        });
        assertEquals(2, exitOf(r), "panic exit code");
        assertTrue(r.stderr().startsWith("panic: boom\n\ngoroutine "),
                "panic header, got: " + r.stderr());
        assertTrue(!r.stderr().contains("deadlock"), "panic, not deadlock: " + r.stderr());
    }

    /**
     * Runs {@code main} like {@link #run}, then tears the scheduler down
     * (joining every goroutine carrier) before reading stderr, so output a
     * goroutine prints after the exit decision is captured too.
     */
    private static Run runAndTerminate(int gomaxprocs,
            java.util.function.Consumer<SchedulerCore> main) throws Exception {
        SchedulerCore sched = new SchedulerCore(gomaxprocs, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore prev = SchedulerCore.install(sched);
        PrintStream oldErr = System.err;
        ByteArrayOutputStream err = new ByteArrayOutputStream();
        System.setErr(new PrintStream(err, true, StandardCharsets.UTF_8));
        try {
            sched.spawnMain(() -> main.accept(sched));
            java.util.concurrent.CompletableFuture<Integer> code =
                    java.util.concurrent.CompletableFuture.supplyAsync(() -> {
                        try {
                            return sched.awaitExit();
                        } catch (InterruptedException e) {
                            throw new IllegalStateException(e);
                        }
                    });
            int c = code.get(20, TimeUnit.SECONDS);
            java.util.List<Thread> stuck = sched.terminate(10_000);
            assertTrue(stuck.isEmpty(), "goroutines did not stop: " + stuck);
            return new Run(c, err.toString(StandardCharsets.UTF_8));
        } catch (java.util.concurrent.TimeoutException e) {
            throw new AssertionError("program did not exit within 20s (hang)");
        } finally {
            System.setErr(oldErr);
            SchedulerCore.install(prev);
        }
    }

    /** A panic value whose Error()/String() text comes from {@code text}. */
    private static void panicWithText(java.util.function.Supplier<String> text) throws Exception {
        graalgo.values.GoTypeDescriptor d =
                new graalgo.values.GoTypeDescriptor("main.E", graalgo.values.GoKind.STRUCT, false);
        Object marker = new Object();
        java.util.function.BiFunction<Object, Object, String> prev =
                graalgo.runtime.PanicRendering.methodTextResolver();
        graalgo.runtime.PanicRendering.setMethodTextResolver(
                (desc, v) -> v == marker ? text.get() : null);
        lastPanicRun = null;
        try {
            lastPanicRun = runAndTerminate(2, s -> Panic.throwPanic(marker, d, "main.E", null));
        } finally {
            graalgo.runtime.PanicRendering.setMethodTextResolver(prev);
        }
    }

    private static Run lastPanicRun;

    /**
     * gc preprintpanics: Error() runs with the world running, so it may
     * sleep (park on a timer) and the report still completes. Before the
     * fix the report claimed the process first (admissions stopped), the
     * sleeping goroutine was never re-admitted, and the process hung.
     */
    public void testPanicErrorMayBlock() throws Exception {
        panicWithText(() -> {
            graalgo.runtime.Timers.sleep(20_000_000L);
            return "slept";
        });
        assertEquals(2, exitOf(lastPanicRun), "exit code");
        assertTrue(lastPanicRun.stderr().startsWith("panic: slept\n\ngoroutine "),
                "panic header, got: " + lastPanicRun.stderr());
    }

    /** Error() panicking: gc's "panic while printing panic value" throw, exit 2. */
    public void testPanicErrorPanics() throws Exception {
        panicWithText(() -> {
            Panic.throwPanic(GoString.fromUtf8("inner"));
            return "unreachable";
        });
        assertEquals(2, exitOf(lastPanicRun), "exit code");
        assertTrue(lastPanicRun.stderr().startsWith(
                "fatal error: panic while printing panic value: inner\n\ngoroutine "),
                "throw header, got: " + lastPanicRun.stderr());
    }

    /**
     * Error() calling runtime.Goexit: still a fatal error with exit 2, no
     * hang (gc itself crashes with "fatal error: unknown pc" here — a gc
     * runtime defect graalgo does not reproduce; the cause is named).
     */
    public void testPanicErrorGoexits() throws Exception {
        panicWithText(() -> {
            Goexit.goexit();
            return "unreachable";
        });
        assertEquals(2, exitOf(lastPanicRun), "exit code");
        assertTrue(lastPanicRun.stderr().startsWith("fatal error: panic while printing panic value"),
                "throw header, got: " + lastPanicRun.stderr());
    }

    /**
     * A goroutine panic racing main's return has exactly two gc outcomes:
     * exit 0 with nothing printed, or the panic report with exit 2 — never
     * the report with exit 0 (the pre-fix race: requestExit(0) won while
     * the panicking goroutine still printed).
     */
    public void testPanicRacingMainReturnNeverExitsZeroWithReport() throws Exception {
        int printedZero = 0;
        int reports = 0;
        for (int i = 0; i < 200; i++) {
            AtomicBoolean go = new AtomicBoolean();
            CountDownLatch ready = new CountDownLatch(1);
            Run r = runAndTerminate(2, s -> {
                s.spawn(() -> {
                    ready.countDown();
                    while (!go.get()) {
                        Thread.onSpinWait();
                    }
                    Panic.throwPanic(GoString.fromUtf8("racing"));
                });
                try {
                    ready.await();
                } catch (InterruptedException e) {
                    throw new AssertionError(e);
                }
                go.set(true);
            });
            boolean printed = r.stderr().contains("panic: racing");
            if (r.code() == 0 && printed) {
                printedZero++;
            } else if (r.code() == 2) {
                assertTrue(printed, "exit 2 without the report: " + r.stderr());
                reports++;
            } else {
                assertEquals(0, r.code(), "exit code");
                assertTrue(r.stderr().isEmpty(), "exit 0 with output: " + r.stderr());
            }
        }
        assertEquals(0, printedZero, "runs printing the panic yet exiting 0 (reports=" + reports + ")");
    }

    // ---------- several schedulers in one process (embedding) ----------

    /**
     * Tearing down a scheduler that is not the installed one (an
     * embedder's finished context) must not set the fast-path exit flag
     * the RUNNING program's polls read: pre-fix terminate() set the static
     * flag unconditionally, sending every poll of the other program down
     * the slow path for good.
     */
    public void testTerminatingOtherSchedulerLeavesFastPathClear() throws Exception {
        SchedulerCore running = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        running.start();
        SchedulerCore prev = SchedulerCore.install(running);
        try {
            SchedulerCore old = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS);
            old.start();
            old.requestExit(0);
            assertTrue(old.terminate(1000).isEmpty(), "old scheduler torn down");
            assertTrue(!SchedulerCore.exitPending(), "fast-path exit flag set by another scheduler");
            running.requestExit(0);
            assertTrue(SchedulerCore.exitPending(), "installed scheduler's exit sets the flag");
        } finally {
            SchedulerCore.install(prev);
            running.terminate(1000);
        }
        assertTrue(!SchedulerCore.exitPending() || prev != null && prev.isExiting(),
                "install mirrors the newly installed scheduler");
    }

    /**
     * A goroutine left running by an earlier program whose scheduler is no
     * longer installed still freezes at its safepoint once its own
     * scheduler exits, and unwinds on teardown: pre-fix the poll consulted
     * the INSTALLED scheduler (where the goroutine has no handle), so a
     * spinning leftover never stopped and teardown reported it stuck.
     */
    public void testLeftoverGoroutineOfUninstalledSchedulerUnwinds() throws Exception {
        SchedulerCore old = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        old.start();
        SchedulerCore prev = SchedulerCore.install(old);
        CountDownLatch spinning = new CountDownLatch(1);
        old.spawn(() -> {
            spinning.countDown();
            for (;;) {
                graalgo.safepoint.Safepoint.poll();
            }
        });
        assertTrue(spinning.await(10, TimeUnit.SECONDS), "spinner admitted");
        SchedulerCore next = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        next.start();
        SchedulerCore.install(next); // a later program is now installed
        try {
            old.requestExit(0);
            java.util.List<Thread> stuck = old.terminate(3000);
            assertTrue(stuck.isEmpty(), "leftover spinner did not unwind: " + stuck);
        } finally {
            SchedulerCore.install(prev);
            next.terminate(1000);
        }
    }

    // ---------- deadlock detection ----------

    /** Main blocked with no possible waker: gc's fatal deadlock, exit 2. */
    public void testDeadlockMainOnly() throws Exception {
        Run r = run(1, s -> parkForever(s, "chan receive"));
        assertEquals(2, exitOf(r), "deadlock exit code");
        assertTrue(r.stderr().startsWith(
                "fatal error: all goroutines are asleep - deadlock!\n\ngoroutine "),
                "deadlock header, got: " + r.stderr());
        assertTrue(r.stderr().contains(" [chan receive]:\n"), "wait reason: " + r.stderr());
    }

    /** Every goroutine (main and others) blocked: all are dumped. */
    public void testDeadlockDumpsEveryUserGoroutine() throws Exception {
        Run r = run(2, s -> {
            s.spawn(() -> parkForever(s, "chan send"));
            s.spawnSystem(() -> parkForever(s, "internal"));
            parkForever(s, "sync.WaitGroup.Wait");
        });
        assertEquals(2, exitOf(r), "deadlock exit code");
        assertTrue(r.stderr().contains("[sync.WaitGroup.Wait]:"), "main dumped: " + r.stderr());
        assertTrue(r.stderr().contains("[chan send]:"), "worker dumped: " + r.stderr());
        assertTrue(!r.stderr().contains("[internal]"), "system goroutine hidden: " + r.stderr());
    }

    /** A pending sleep timer is a future waker: no deadlock (gc checkdead). */
    public void testNoDeadlockWhileTimerPending() throws Exception {
        Run r = run(1, s -> graalgo.runtime.Timers.sleep(300_000_000L));
        assertEquals(0, exitOf(r), "sleeping main is not deadlocked");
    }

    /**
     * After the last timer fires without waking anyone, the deadlock is
     * reported (the timer heap pokes the drainer when it empties).
     */
    public void testDeadlockAfterLastTimerRetires() throws Exception {
        AtomicBoolean fired = new AtomicBoolean();
        Run r = run(1, s -> {
            s.timerHeap().addCallback(System.nanoTime() + 100_000_000L, () -> fired.set(true));
            parkForever(s, "chan receive");
        });
        assertTrue(fired.get(), "timer fired before the deadlock report");
        assertEquals(2, exitOf(r), "deadlock after timer");
    }

    /** A goroutine in blocking I/O can wake main later: no deadlock. */
    public void testNoDeadlockWhileGoroutineInIo() throws Exception {
        Run r = run(1, s -> {
            GoroutineHandle main = s.currentGoroutine();
            s.spawn(() -> {
                s.ioEnter();
                try {
                    Thread.sleep(300); // the "syscall"
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                } finally {
                    s.ioExit();
                }
                s.unpark(main);
            });
            parkForever(s, "chan receive");
        });
        assertEquals(0, exitOf(r), "I/O-blocked waker is not a deadlock");
    }

    /** A netpoll park is woken from outside the scheduler: no deadlock. */
    public void testNoDeadlockWhileNetpollParked() throws Exception {
        Run r = run(1, s -> {
            GoroutineHandle main = s.currentGoroutine();
            Thread.ofPlatform().daemon(true).start(() -> {
                try {
                    Thread.sleep(300); // the selector thread seeing readiness
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                }
                s.unpark(main);
            });
            s.parkCurrent(() -> { }, ParkReason.NETPOLL);
        });
        assertEquals(0, exitOf(r), "netpoll park is not a deadlock");
    }

    /** Main's Goexit with nothing left: gc's "no goroutines" fatal. */
    public void testMainGoexitNoGoroutines() throws Exception {
        Run r = run(1, s -> {
            Goexit.goexit();
        });
        assertEquals(2, exitOf(r), "goexit exit code");
        assertEquals(0, r.stderr().compareTo(
                "fatal error: no goroutines (main called runtime.Goexit) - deadlock!\n"),
                "goexit message, got: " + r.stderr());
    }

    /** Main's Goexit while another goroutine still runs: it finishes first. */
    public void testMainGoexitWaitsForOthers() throws Exception {
        CountDownLatch otherDone = new CountDownLatch(1);
        Run r = run(1, s -> {
            s.spawn(() -> {
                graalgo.runtime.Timers.sleep(100_000_000L);
                otherDone.countDown();
            });
            Goexit.goexit();
        });
        assertEquals(0, otherDone.getCount(), "other goroutine ran to completion");
        assertEquals(2, exitOf(r), "then no goroutines");
    }

    // ---------- GOMAXPROCS ----------

    /**
     * runtime.GOMAXPROCS(n) returns the previous value and changes the
     * in-flight cap at run time (SCHED-0003): raised to 2, two spinners
     * run concurrently; lowered again, the invariant holds.
     */
    public void testGomaxprocsChangesCap() throws Exception {
        if (Runtime.getRuntime().availableProcessors() < 2) {
            return; // cap is clamped to carriers; nothing to observe
        }
        AtomicReference<String> failure = new AtomicReference<>();
        Run r = run(1, s -> {
            if (s.cap() != 1) {
                failure.set("initial cap " + s.cap());
            }
            int prev = s.setGomaxprocs(2);
            if (prev != 1 || s.cap() != 2 || s.setGomaxprocs(0) != 2) {
                failure.set("prev=" + prev + " cap=" + s.cap());
            }
            CountDownLatch both = new CountDownLatch(2);
            AtomicBoolean stop = new AtomicBoolean();
            for (int i = 0; i < 2; i++) {
                s.spawn(() -> {
                    both.countDown();
                    while (!stop.get()) {
                        s.pollCurrentThread();
                    }
                });
            }
            // Main parks (sleep) so both spinners can be admitted at once.
            graalgo.runtime.Timers.sleep(200_000_000L);
            if (s.maxInflightObserved() < 2) {
                failure.set("cap raise not applied: maxInflight=" + s.maxInflightObserved());
            }
            if (s.setGomaxprocs(1) != 2) {
                failure.set("lowering did not return the previous value");
            }
            graalgo.runtime.Timers.sleep(50_000_000L);
            stop.set(true);
        });
        assertEquals(0, exitOf(r), "exit code (an invariant violation halts the JVM)");
        assertTrue(failure.get() == null, String.valueOf(failure.get()));
    }
}
