package graalgo.p3os;

import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.sched.SchedulerCore;
import graalgo.values.GoSlice;

/**
 * P1-25 (lane L28): the blocking-I/O paths of {@link GoOs} must bracket
 * with {@code SchedulerCore.ioEnter}/{@code ioExit} (GG-SCHED-0004).
 *
 * <p>Regression tests for the {@code --gmp=1} starvation hang: a
 * goroutine blocked in a {@code GoOs} syscall must release its
 * in-flight slot so a peer goroutine is still admitted. Before the
 * fix, {@code GoOs.read}/{@code write} never called {@code ioEnter} —
 * the blocked goroutine held its slot and the peer was never admitted
 * (the concrete hang from the brief: a reader on {@code os.Stdin} plus
 * any other goroutine under {@code --gmp=1}).
 *
 * <p>Each test asserts AFTER full cleanup (sink/source closed, peer
 * stopped, quiescence awaited) so a red run fails the assertion
 * instead of hanging the suite.
 */
public final class GoOsIoSlotsTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static GoSlice buf(int n) {
        return new GoSlice(new int[n], 0, n, n);
    }

    private static SchedulerCore withScheduler(int gmp) {
        SchedulerCore sched = new SchedulerCore(gmp, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        return sched;
    }

    /**
     * GOMAXPROCS=1: a goroutine blocked in {@code GoOs.read} on a pipe
     * (sink open, no writer — the read blocks in the channel) must not
     * starve a busy peer. Red before the L28 fix (peer never admitted,
     * counter stays 0); green after (ioEnter releases the slot).
     */
    public void testBlockedPipeReadReleasesSlotAtGmp1() throws Exception {
        SchedulerCore sched = withScheduler(1);
        AtomicLong counter = new AtomicLong();
        AtomicBoolean stop = new AtomicBoolean();
        AtomicBoolean readerEntered = new AtomicBoolean();
        AtomicReference<Throwable> readerError = new AtomicReference<>();
        try {
            GoOs.init(new String[0]);
            long r = GoOs.pipe();
            assertTrue(r > 0, "GoOs.pipe failed: " + r);
            final long w = r + 1;
            sched.spawn(() -> {
                readerEntered.set(true);
                try {
                    long n = GoOs.read(r, buf(64));
                    // The test closes the sink below: EOF must surface as 0.
                    if (n != 0) {
                        throw new AssertionError(
                                "expected EOF (0) after sink close, got " + n);
                    }
                } catch (Throwable t) {
                    readerError.set(t);
                }
            });
            long deadline = System.currentTimeMillis() + 5000;
            while (!readerEntered.get()) {
                if (System.currentTimeMillis() > deadline) {
                    throw new AssertionError("reader goroutine never started");
                }
                Thread.sleep(5);
            }
            // The flag is set immediately before the GoOs.read call; give
            // the reader time to reach the blocking channel read.
            Thread.sleep(200);
            sched.spawn(() -> {
                while (!stop.get()) {
                    counter.incrementAndGet();
                }
            });
            Thread.sleep(1000);
            long c = counter.get();
            // Cleanup first: closing the sink EOFs the blocked read, and
            // stop releases the peer, so quiescence is reachable on both
            // the red and the green path.
            GoOs.close(w);
            stop.set(true);
            sched.awaitQuiescence();
            assertTrue(c > 1000,
                    "P1-25: goroutine blocked in GoOs.read held its in-flight slot "
                    + "at GOMAXPROCS=1; peer starved, counter=" + c);
            assertTrue(readerError.get() == null, "reader failed: " + readerError.get());
            assertTrue(sched.maxInflightObserved() <= 1,
                    "cap violated: maxInflight=" + sched.maxInflightObserved());
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * GOMAXPROCS=1: the write side. A goroutine blocked in a large
     * {@code GoOs.write} to a readerless pipe (pipe buffer fills, the
     * single channel write blocks) must release its slot the same way.
     */
    public void testBlockedPipeWriteReleasesSlotAtGmp1() throws Exception {
        SchedulerCore sched = withScheduler(1);
        AtomicLong counter = new AtomicLong();
        AtomicBoolean stop = new AtomicBoolean();
        AtomicBoolean writerEntered = new AtomicBoolean();
        AtomicReference<Throwable> writerError = new AtomicReference<>();
        try {
            GoOs.init(new String[0]);
            long r = GoOs.pipe();
            assertTrue(r > 0, "GoOs.pipe failed: " + r);
            final long w = r + 1;
            // 256 KiB > any default pipe capacity: the single channel
            // write fills the buffer and then blocks with no reader.
            final GoSlice big = buf(256 * 1024);
            sched.spawn(() -> {
                writerEntered.set(true);
                try {
                    GoOs.write(w, big);
                } catch (Throwable t) {
                    writerError.set(t);
                }
            });
            long deadline = System.currentTimeMillis() + 5000;
            while (!writerEntered.get()) {
                if (System.currentTimeMillis() > deadline) {
                    throw new AssertionError("writer goroutine never started");
                }
                Thread.sleep(5);
            }
            Thread.sleep(200);
            sched.spawn(() -> {
                while (!stop.get()) {
                    counter.incrementAndGet();
                }
            });
            Thread.sleep(1000);
            long c = counter.get();
            // Closing the source breaks the blocked write (EPIPE path);
            // stop releases the peer.
            GoOs.close(r);
            stop.set(true);
            sched.awaitQuiescence();
            assertTrue(c > 1000,
                    "P1-25: goroutine blocked in GoOs.write held its in-flight slot "
                    + "at GOMAXPROCS=1; peer starved, counter=" + c);
            assertTrue(writerError.get() == null, "writer failed: " + writerError.get());
            assertTrue(sched.maxInflightObserved() <= 1,
                    "cap violated: maxInflight=" + sched.maxInflightObserved());
        } finally {
            SchedulerCore.install(null);
        }
    }

    /**
     * No scheduler installed (plain unit-test / Phase-1 context): the
     * bracket is a silent no-op and the syscall still works — the
     * fail-fast {@code current()} path must not leak into {@code GoOs}.
     */
    public void testNoSchedulerBracketIsNoop() throws Exception {
        GoOs.init(new String[0]);
        long r = GoOs.pipe();
        assertTrue(r > 0, "GoOs.pipe failed: " + r);
        long w = r + 1;
        GoSlice payload = new GoSlice(new int[] {1, 2, 3, 4, 5, 6}, 0, 6, 6);
        assertTrue(GoOs.write(w, payload) == 7, "write n+1");
        GoSlice rb = buf(16);
        assertTrue(GoOs.read(r, rb) == 7, "read n+1");
        GoOs.close(w);
        assertTrue(GoOs.read(r, buf(16)) == 0, "EOF after sink close");
        GoOs.close(r);
    }
}
