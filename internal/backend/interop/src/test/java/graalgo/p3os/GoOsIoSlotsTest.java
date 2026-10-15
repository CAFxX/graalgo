package graalgo.p3os;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;

import graalgo.sched.SchedulerCore;
import graalgo.values.GoSlice;

/**
 * P1-R4-2 (review 2026-10-15): blocking file/pipe I/O must bracket the
 * syscall with {@code ioEnter}/{@code ioExit} so the goroutine's
 * in-flight slot is released across the block. With {@code --gmp=1}, an
 * un-bracketed goroutine blocked in {@code read(2)} keeps its slot and
 * starves the whole program (gc releases the M via entersyscall).
 *
 * <p>Red-green: a goroutine blocked in {@link GoOs#read} on a pipe must
 * not prevent a peer's admission at gmp=1. Pre-fix the peer never runs
 * (slot held across the block) and the latch below times out instead
 * of hanging the suite.
 */
public final class GoOsIoSlotsTest {

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static GoSlice buf(int n) {
        return new GoSlice(new int[n], 0, n, n);
    }

    /**
     * gmp=1: a peer makes progress while another goroutine is blocked
     * in a pipe read.
     */
    public void testBlockedPipeReadReleasesSlot() throws Exception {
        SchedulerCore sched = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            GoOs.init(new String[0]);
            long r = GoOs.pipe();
            check(r >= 0, "pipe() failed: " + r);
            long w = r + 1;
            CountDownLatch peerRan = new CountDownLatch(1);
            CountDownLatch readerDone = new CountDownLatch(1);
            AtomicLong readResult = new AtomicLong(-999);
            // Reader first: admitted, blocks in read(2) on the empty pipe.
            sched.spawn(() -> {
                long n = GoOs.read(r, buf(16));
                readResult.set(n);
                readerDone.countDown();
            });
            // Peer: must be admitted while the reader is blocked in I/O.
            sched.spawn(peerRan::countDown);
            check(peerRan.await(15, TimeUnit.SECONDS),
                    "gmp=1 starved: peer not admitted while reader blocked in read (P1-R4-2)");
            // Unblock the reader: one byte; a 1-byte read returns n+1 = 2.
            GoSlice wb = buf(1);
            ((int[]) wb.array())[0] = 7;
            check(GoOs.write(w, wb) == 2, "pipe write failed");
            check(readerDone.await(15, TimeUnit.SECONDS), "reader never finished");
            check(readResult.get() == 2, "read returned " + readResult.get());
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }
}
