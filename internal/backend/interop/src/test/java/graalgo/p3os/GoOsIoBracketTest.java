package graalgo.p3os;

import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.sched.SchedulerCore;
import graalgo.values.GoSlice;

/**
 * End-to-end proof for the GG-0018 slot bracket at the {@code GoOs}
 * choke point (review-pool-io, R1-24): at GOMAXPROCS=1, a goroutine
 * blocked in {@code GoOs.read} on a pipe must release its in-flight
 * slot, so a busy peer goroutine still runs. Fails if the bracket is
 * missing (the reader holds the only slot and the peer's counter stays
 * 0). The write that unblocks the reader runs on the test thread
 * (off-scheduler, where the bracket no-ops), and it runs whether or
 * not the counter advanced, so the test always terminates.
 */
public final class GoOsIoBracketTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long want, long got, String msg) {
        if (want != got) {
            throw new AssertionError(msg + ": want " + want + " got " + got);
        }
    }

    private static GoSlice bytesBuf(byte[] data) {
        return new GoSlice(data.clone(), 0, data.length, data.length);
    }

    public void testPipeReadReleasesSlotAtGmp1() throws Exception {
        SchedulerCore sched = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        SchedulerCore prev = SchedulerCore.install(sched);
        sched.start();
        GoOs.init(new String[0]);
        try {
            long r = GoOs.pipe();
            long w = r + 1;
            AtomicBoolean readerBlocked = new AtomicBoolean();
            AtomicBoolean stop = new AtomicBoolean();
            AtomicLong counter = new AtomicLong();
            AtomicReference<Throwable> readerError = new AtomicReference<>();
            AtomicLong readResult = new AtomicLong(-1);
            sched.spawn(() -> {
                // Signal "about to block" before the read: the pipe is
                // empty, so GoOs.read blocks until the test thread
                // writes. The flag is set before the bracketed call, so
                // the test thread may write while the reader is still
                // in ioEnter handshaking — harmless: the byte waits in
                // the pipe until the read runs.
                readerBlocked.set(true);
                try {
                    GoSlice b = new GoSlice(new byte[16], 0, 16, 16);
                    readResult.set(GoOs.read(r, b));
                    byte[] arr = (byte[]) b.array();
                    if (arr[0] != 7) {
                        throw new AssertionError("pipe byte mismatch: " + arr[0]);
                    }
                } catch (Throwable t) {
                    readerError.set(t);
                }
            });
            sched.spawn(() -> {
                while (!stop.get()) {
                    counter.incrementAndGet();
                }
            });
            long deadline = System.currentTimeMillis() + 5000;
            while (!readerBlocked.get()) {
                if (System.currentTimeMillis() > deadline) {
                    throw new AssertionError("reader never started");
                }
                Thread.sleep(5);
            }
            // Give the reader time to block inside GoOs.read and the
            // peer time to be admitted (only possible if the slot was
            // released), then unblock the reader regardless of outcome.
            Thread.sleep(500);
            long c = counter.get();
            assertEq(2, GoOs.write(w, bytesBuf(new byte[]{7})),
                    "pipe write n+1 (1 byte)");
            stop.set(true);
            sched.awaitQuiescence();
            assertTrue(readerError.get() == null,
                    "reader failed: " + readerError.get());
            assertEq(2, readResult.get(), "pipe read n+1");
            assertTrue(c > 1000,
                    "busy goroutine starved at GOMAXPROCS=1 while peer blocked in GoOs.read: " + c);
            assertEq(0, GoOs.close(r), "close read");
            assertEq(0, GoOs.close(w), "close write");
        } finally {
            GoOs.init(new String[0]);
            SchedulerCore.install(prev);
        }
    }
}
