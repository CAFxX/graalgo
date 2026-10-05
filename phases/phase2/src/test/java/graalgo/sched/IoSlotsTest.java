package graalgo.sched;

import java.io.PipedInputStream;
import java.io.PipedOutputStream;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;

/**
 * P2-M3 blocking-I/O slot tests (WS C; GG-0018, GG-SCHED-0004).
 *
 * <p>Netpoller deletion: blocking I/O runs on the virtual thread
 * bracketed by {@code ioEnter} (releases the in-flight slot) /
 * {@code ioExit} (re-acquires via the WAKE lane). A goroutine blocked
 * in I/O must not count against GOMAXPROCS.
 *
 * <p>verifies: GG-0018, GG-SCHED-0004 (io slice)
 */
public final class IoSlotsTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private SchedulerCore sched;

    private void withScheduler(int gmp, ThrowingRunnable body) throws Exception {
        sched = new SchedulerCore(gmp, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            body.run();
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }

    private interface ThrowingRunnable {
        void run() throws Exception;
    }

    /**
     * GOMAXPROCS=1: a goroutine blocked in real blocking I/O must not
     * starve a busy peer. Mutation (ioEnter without slot release, or
     * ioExit without re-acquire): the busy goroutine is never admitted
     * (inflight stuck at 1) → counter stays 0 → red. Also asserts the
     * hard cap was never exceeded.
     */
    public void testIoEnterReleasesSlot() throws Exception {
        AtomicLong counter = new AtomicLong();
        AtomicBoolean stop = new AtomicBoolean();
        AtomicBoolean inIo = new AtomicBoolean();
        AtomicBoolean readerDone = new AtomicBoolean();
        AtomicReference<Throwable> readerError = new AtomicReference<>();
        withScheduler(1, () -> {
            PipedInputStream in = new PipedInputStream();
            PipedOutputStream out = new PipedOutputStream(in);
            sched.spawn(() -> {
                sched.ioEnter();
                inIo.set(true);
                try {
                    int b = in.read(); // real blocking syscall
                    if (b != 7) {
                        throw new AssertionError("short read: " + b);
                    }
                } catch (java.io.IOException e) {
                    readerError.set(e);
                } finally {
                    sched.ioExit();
                }
                readerDone.set(true);
            });
            // Wait until the reader is inside blocking I/O (slot released
            // synchronously by ioEnter).
            long deadline = System.currentTimeMillis() + 5000;
            while (!inIo.get()) {
                if (System.currentTimeMillis() > deadline) {
                    throw new AssertionError("reader never entered blocking I/O");
                }
                Thread.sleep(5);
            }
            // Busy peer must now be admittable at cap=1.
            sched.spawn(() -> {
                while (!stop.get()) {
                    counter.incrementAndGet();
                }
            });
            Thread.sleep(300);
            long c = counter.get();
            assertTrue(c > 1000,
                    "busy goroutine starved at GOMAXPROCS=1 while peer in I/O: " + c);
            out.write(7);
            out.flush();
            stop.set(true);
            out.close();
        });
        assertTrue(readerError.get() == null,
                "reader failed: " + readerError.get());
        assertTrue(readerDone.get(), "reader did not complete after ioExit");
        assertTrue(sched.maxInflightObserved() <= 1,
                "cap violated: maxInflight=" + sched.maxInflightObserved());
    }

    // ioExit re-acquires the slot: the goroutine continues and the cap
    // still holds when both run.
    public void testIoExitReacquiresSlot() throws Exception {
        AtomicBoolean continued = new AtomicBoolean();
        withScheduler(1, () -> {
            sched.spawn(() -> {
                sched.ioEnter();
                // No actual blocking: immediate re-acquire.
                sched.ioExit();
                continued.set(true);
            });
        });
        assertTrue(continued.get(), "goroutine did not continue after ioExit");
        assertTrue(sched.maxInflightObserved() <= 1,
                "cap violated: maxInflight=" + sched.maxInflightObserved());
    }

    // Re-entrant ioEnter fails fast on the calling thread.
    public void testReentrantIoEnterFails() throws Exception {
        AtomicReference<String> err = new AtomicReference<>();
        withScheduler(1, () -> {
            sched.spawn(() -> {
                sched.ioEnter();
                try {
                    sched.ioEnter();
                } catch (IllegalStateException e) {
                    err.set(e.getMessage());
                } finally {
                    sched.ioExit();
                }
            });
        });
        assertTrue(err.get() != null && err.get().contains("re-entrant ioEnter"),
                "expected re-entrant ioEnter failure, got: " + err.get());
    }

    // Unpaired ioExit fails fast on the calling thread (no drainer
    // involvement — testable without halting the JVM).
    public void testUnpairedIoExitFails() throws Exception {
        AtomicReference<String> err = new AtomicReference<>();
        withScheduler(1, () -> {
            sched.spawn(() -> {
                try {
                    sched.ioExit();
                } catch (IllegalStateException e) {
                    err.set(e.getMessage());
                }
            });
        });
        assertTrue(err.get() != null && err.get().contains("ioExit without ioEnter"),
                "expected unpaired ioExit failure, got: " + err.get());
    }

    // White-box: the drainer fail-fasts on an IO_EXIT token with no
    // matching IO_ENTER (defense-in-depth behind the calling-thread
    // check). Driven synchronously so the AssertionError is observable
    // (the production drainer would halt).
    public void testDrainerRejectsUnpairedIoExit() {
        SchedulerCore sc = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        GoroutineHandle h = new GoroutineHandle(() -> {});
        h.thread = Thread.currentThread();
        try {
            sc.drainer().processOneForTest(
                    new ResumeToken(ResumeToken.Kind.IO_EXIT, h, 0));
            throw new AssertionError("expected AssertionError for unpaired IO_EXIT");
        } catch (AssertionError e) {
            assertTrue(e.getMessage().contains("ioExit without ioEnter"),
                    "wrong message: " + e.getMessage());
        }
    }

    // White-box: duplicate IO_ENTER fail-fasts in the drainer.
    public void testDrainerRejectsDuplicateIoEnter() {
        SchedulerCore sc = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        GoroutineHandle h = new GoroutineHandle(() -> {});
        h.thread = Thread.currentThread();
        sc.drainer().setInflightForTest(1);
        sc.drainer().processOneForTest(
                new ResumeToken(ResumeToken.Kind.IO_ENTER, h, 0));
        try {
            sc.drainer().processOneForTest(
                    new ResumeToken(ResumeToken.Kind.IO_ENTER, h, 1));
            throw new AssertionError("expected AssertionError for duplicate IO_ENTER");
        } catch (AssertionError e) {
            assertTrue(e.getMessage().contains("duplicate ioEnter"),
                    "wrong message: " + e.getMessage());
        }
    }

    // P1-4: EXIT while in blocking I/O fail-fasts (no double-decrement —
    // the slot was already released at IO_ENTER). The inflight count
    // must be untouched by the failed EXIT.
    public void testDrainerRejectsExitWhileInIo() {
        SchedulerCore sc = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        GoroutineHandle h = new GoroutineHandle(() -> {});
        h.thread = Thread.currentThread();
        sc.drainer().setInflightForTest(1);
        sc.drainer().processOneForTest(
                new ResumeToken(ResumeToken.Kind.IO_ENTER, h, 0));
        int before = sc.drainer().inflightForTest();
        try {
            sc.drainer().processOneForTest(
                    new ResumeToken(ResumeToken.Kind.EXIT, h, 0));
            throw new AssertionError("expected AssertionError for EXIT while inIo");
        } catch (AssertionError e) {
            assertTrue(e.getMessage().contains("exited while in blocking I/O"),
                    "wrong message: " + e.getMessage());
        }
        assertTrue(before == sc.drainer().inflightForTest(),
                "failed EXIT must not decrement inflight");
    }

    // P1-4: PARKED while in blocking I/O fail-fasts (a goroutine in
    // ioEnter must ioExit before parking — otherwise its slot accounting
    // diverges from its parked state).
    public void testDrainerRejectsParkedWhileInIo() {
        SchedulerCore sc = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        GoroutineHandle h = new GoroutineHandle(() -> {});
        h.thread = Thread.currentThread();
        sc.drainer().setInflightForTest(1);
        sc.drainer().processOneForTest(
                new ResumeToken(ResumeToken.Kind.IO_ENTER, h, 0));
        try {
            sc.drainer().processOneForTest(
                    new ResumeToken(ResumeToken.Kind.PARKED, h, 0));
            throw new AssertionError("expected AssertionError for PARKED while inIo");
        } catch (AssertionError e) {
            assertTrue(e.getMessage().contains("parked while in blocking I/O"),
                    "wrong message: " + e.getMessage());
        }
    }
}
