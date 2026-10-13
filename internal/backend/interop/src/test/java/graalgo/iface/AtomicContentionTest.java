package graalgo.iface;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicLong;

import graalgo.runtime.Atomics;

/**
 * Java-level contention tests for the P3-M4 atomic completions
 * (GG-0020): the raw {@link Atomics} ops are linearizable under
 * concurrent hammering.
 * verifies: GG-0020
 */
public final class AtomicContentionTest {

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

    /**
     * verifies: GG-0020 — 8 threads × 1000 addInt64 = 8000, no lost
     * updates.
     */
    public void testConcurrentAddInt64() throws Exception {
        AtomicLong cell = new AtomicLong();
        int threads = 8, perThread = 1000;
        CountDownLatch start = new CountDownLatch(1);
        CountDownLatch done = new CountDownLatch(threads);
        for (int t = 0; t < threads; t++) {
            new Thread(() -> {
                try {
                    start.await();
                    for (int i = 0; i < perThread; i++) {
                        Atomics.addInt64(cell, 1);
                    }
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                } finally {
                    done.countDown();
                }
            }).start();
        }
        start.countDown();
        done.await();
        assertEquals((long) threads * perThread, Atomics.loadInt64(cell),
                "concurrent addInt64 lost updates");
    }

    /**
     * verifies: GG-0020 — CAS-spin increments from 4 threads converge to
     * the exact total (CAS is the linearization point).
     */
    public void testCasSpinConverges() throws Exception {
        AtomicLong cell = new AtomicLong();
        int threads = 4, perThread = 1000;
        CountDownLatch start = new CountDownLatch(1);
        CountDownLatch done = new CountDownLatch(threads);
        for (int t = 0; t < threads; t++) {
            new Thread(() -> {
                try {
                    start.await();
                    for (int i = 0; i < perThread; i++) {
                        long old;
                        do {
                            old = Atomics.loadInt64(cell);
                        } while (!Atomics.compareAndSwapInt64(cell, old, old + 1));
                    }
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                } finally {
                    done.countDown();
                }
            }).start();
        }
        start.countDown();
        done.await();
        assertEquals((long) threads * perThread, Atomics.loadInt64(cell),
                "CAS-spin increments lost updates");
    }

    /**
     * verifies: GG-0020 — swap returns the previous value atomically
     * (sequential sanity for the M4 swapUintptr/swapBool completions'
     * sibling).
     */
    public void testSwapInt64ReturnsOld() {
        AtomicLong cell = new AtomicLong(41);
        long old = Atomics.swapInt64(cell, 42);
        assertEquals(41, old, "swap must return the previous value");
        assertEquals(42, Atomics.loadInt64(cell), "swap must install the new value");
    }
}
