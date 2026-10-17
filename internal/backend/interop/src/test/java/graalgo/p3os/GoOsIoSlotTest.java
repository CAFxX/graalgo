package graalgo.p3os;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;

import graalgo.sched.SchedulerCore;
import graalgo.values.GoSlice;

/**
 * R2 P1-5: the slot-releasing I/O discipline is wired at the
 * blocking-syscall sites (SPEC §3.5 / GG-SCHED-0004: "All blocking in
 * the runtime/stdlib adaptation layer SHALL go through scheduler-aware
 * primitives").
 *
 * <p>Red-green discipline:
 * <ul>
 *   <li>{@code testBlockingReadReleasesSlot} fails when the bracket is
 *   removed (the peer starves at GOMAXPROCS=1);</li>
 *   <li>{@code testNonblockingSocketSkipsBracket} fails if the socket
 *   fast path ever brackets (ioExit count moves);</li>
 *   <li>{@code testBlockingReadBrackets} fails if the pipe path stops
 *   bracketing (ioExit count does not move);</li>
 *   <li>{@code testOffGoroutinePlainPath} fails if the scheduler-less
 *   path regresses (it must keep working exactly as before).</li>
 * </ul>
 */
public final class GoOsIoSlotTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(long expected, long actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    private static SchedulerCore newScheduler(int gmp) {
        SchedulerCore sched = new SchedulerCore(gmp, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        return sched;
    }

    private static GoSlice buf(int n) {
        return new GoSlice(new byte[n], 0, n, n);
    }

    private static GoSlice bytes(byte... b) {
        return new GoSlice(b, 0, b.length, b.length);
    }

    /** Parses the ioExit=<n> component of {@link SchedulerCore#wakeProvenance()}. */
    private static long ioExitCount(SchedulerCore sched) {
        String p = sched.wakeProvenance();
        int i = p.indexOf("ioExit=");
        assertTrue(i >= 0, "wakeProvenance format: " + p);
        return Long.parseLong(p.substring(i + 7).trim());
    }

    /**
     * A goroutine blocked in a pipe read releases its in-flight slot:
     * at GOMAXPROCS=1 a runnable peer must still run (gc entersyscall
     * parity). Without the bracket, the reader holds the only slot and
     * the peer starves.
     */
    public void testBlockingReadReleasesSlot() throws Exception {
        SchedulerCore sched = newScheduler(1);
        try {
            long r = GoOs.pipe();
            long w = r + 1;
            AtomicLong readResult = new AtomicLong(-999);
            CountDownLatch peerDone = new CountDownLatch(1);
            AtomicBoolean peerRan = new AtomicBoolean(false);
            sched.spawn(() -> {
                long n = GoOs.read(r, buf(16)); // blocks: no writer yet
                readResult.set(n);
            });
            sched.spawn(() -> {
                peerRan.set(true);
                peerDone.countDown();
            });
            assertTrue(peerDone.await(30, TimeUnit.SECONDS),
                    "peer starved: blocking pipe read holds the only slot");
            // Release the reader from the (scheduler-less) test thread.
            assertEquals(2, GoOs.write(w, bytes((byte) 'x')), "pipe write");
            sched.awaitQuiescence();
            assertTrue(peerRan.get(), "peer must have run");
            assertEquals(2, readResult.get(), "read result (n+1)");
            assertEquals(0, GoOs.close(r), "close read end");
            assertEquals(0, GoOs.close(w), "close write end");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * A read on a non-blocking channel must NOT bracket: the channel
     * cannot block (socket readiness waits park via the netpoller
     * instead), so bracketing would add a drainer round-trip per read
     * for zero benefit. Observed via the ioExit admission count.
     * (A non-blocking pipe source stands in for a socket: the same
     * non-blocking SelectableChannel property, without TCP setup.)
     */
    public void testNonblockingSocketSkipsBracket() throws Exception {
        SchedulerCore sched = newScheduler(2);
        java.nio.channels.Pipe pipe = java.nio.channels.Pipe.open();
        try {
            java.nio.channels.Pipe.SourceChannel src = pipe.source();
            src.configureBlocking(false);
            long cookie = GoOs.registerSocketChannel(src, true, false);
            long before = ioExitCount(sched);
            CountDownLatch done = new CountDownLatch(1);
            AtomicLong readResult = new AtomicLong(-999);
            sched.spawn(() -> {
                // No data: non-blocking read returns 0 -> GoOs.read returns 1.
                readResult.set(GoOs.read(cookie, buf(16)));
                done.countDown();
            });
            assertTrue(done.await(30, TimeUnit.SECONDS), "nonblocking read timed out");
            sched.awaitQuiescence();
            assertEquals(1, readResult.get(), "nonblocking empty read");
            assertEquals(before, ioExitCount(sched),
                    "nonblocking channel read must not bracket ioEnter/ioExit");
            assertEquals(0, GoOs.close(cookie), "close channel");
        } finally {
            pipe.sink().close();
            SchedulerCore.install(null);
        }
    }

    /**
     * A read on a blocking pipe (data available, so it returns
     * immediately) DOES bracket: exactly one ioExit admission per call.
     */
    public void testBlockingReadBrackets() throws Exception {
        SchedulerCore sched = newScheduler(2);
        try {
            long r = GoOs.pipe();
            long w = r + 1;
            assertEquals(2, GoOs.write(w, bytes((byte) 'y')), "pipe write");
            long before = ioExitCount(sched);
            CountDownLatch done = new CountDownLatch(1);
            AtomicLong readResult = new AtomicLong(-999);
            sched.spawn(() -> {
                readResult.set(GoOs.read(r, buf(16)));
                done.countDown();
            });
            assertTrue(done.await(30, TimeUnit.SECONDS), "pipe read timed out");
            sched.awaitQuiescence();
            assertEquals(2, readResult.get(), "read result (n+1)");
            assertEquals(before + 1, ioExitCount(sched),
                    "blocking pipe read must bracket ioEnter/ioExit exactly once");
            assertEquals(0, GoOs.close(r), "close read end");
            assertEquals(0, GoOs.close(w), "close write end");
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * Scheduler-less callers (tests, launchers, Phase-1 runs) keep the
     * plain blocking path: no fail-fast, no behavior change.
     */
    public void testOffGoroutinePlainPath() throws Exception {
        SchedulerCore.install(null);
        long r = GoOs.pipe();
        long w = r + 1;
        try {
            assertEquals(2, GoOs.write(w, bytes((byte) 'z')), "pipe write");
            GoSlice b = buf(16);
            assertEquals(2, GoOs.read(r, b), "pipe read");
            assertEquals((byte) 'z', ((byte[]) b.array())[b.offset()], "byte round-trip");
        } finally {
            GoOs.close(r);
            GoOs.close(w);
        }
    }
}
