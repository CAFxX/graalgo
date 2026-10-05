package graalgo.sched;

import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.runtime.ChanOps;
import graalgo.runtime.GoChan;
import graalgo.runtime.PanicException;

/**
 * P2-M2 channel + select tests (WS B; GG-0017 chan slice).
 *
 * <p>Red-green discipline: every test fails against a broken channel
 * implementation (stubbed ops, missing wakeups, carrier-blocking park)
 * and passes against the real one. Tests marked "verifies: GG-0017"
 * activate the GG ID per the plan's activation policy.
 */
public final class ChannelTest {

    private SchedulerCore sched;

    // ---------- harness ----------

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

    private static void assertEquals(Object expected, Object actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /** Runs body with an installed scheduler, then awaits quiescence. */
    private void withScheduler(int gmp, ThrowingRunnable body) throws Exception {
        sched = new SchedulerCore(gmp, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            body.run();
            awaitQuiescence(15000);
        } finally {
            SchedulerCore.install(null);
        }
    }

    /** Quiescence with a hang diagnosis (lost wakeup => loud failure). */
    private void awaitQuiescence(long timeoutMs) throws Exception {
        long deadline = System.currentTimeMillis() + timeoutMs;
        while (sched.runnableCount() != 0) {
            if (System.currentTimeMillis() > deadline) {
                throw new AssertionError(
                        "quiescence timeout: " + sched.runnableCount()
                        + " goroutines still live — lost wakeup?");
            }
            Thread.sleep(10);
        }
        sched.awaitQuiescence();
    }

    /** Polls cond until true or timeout (for cross-goroutine assertions). */
    private static void waitFor(java.util.function.BooleanSupplier cond, String what)
            throws Exception {
        long deadline = System.currentTimeMillis() + 10000;
        while (!cond.getAsBoolean()) {
            if (System.currentTimeMillis() > deadline) {
                throw new AssertionError("timeout waiting for: " + what);
            }
            Thread.sleep(5);
        }
    }

    private interface ThrowingRunnable {
        void run() throws Exception;
    }

    // ---------- unbuffered ----------

    /**
     * Unbuffered ping-pong: sender blocks until receiver arrives; values
     * transfer exactly. verifies: GG-0017
     */
    public void testUnbufferedSendRecv() throws Exception {
        AtomicReference<Object> got = new AtomicReference<>();
        withScheduler(2, () -> {
            GoChan ch = new GoChan(0, 0L);
            sched.spawn(() -> ChanOps.send(ch, 42L));
            sched.spawn(() -> got.set(ChanOps.recv(ch)));
        });
        assertEquals(42L, got.get(), "unbuffered value transferred");
    }

    // ---------- buffered ----------

    /**
     * Buffered FIFO order: two sends without a receiver, then two recvs.
     * verifies: GG-0017
     */
    public void testBufferedFifo() throws Exception {
        AtomicReference<Object> r1 = new AtomicReference<>();
        AtomicReference<Object> r2 = new AtomicReference<>();
        withScheduler(2, () -> {
            GoChan ch = new GoChan(2, 0L);
            sched.spawn(() -> {
                ChanOps.send(ch, 1L);
                ChanOps.send(ch, 2L);
                r1.set(ChanOps.recv(ch));
                r2.set(ChanOps.recv(ch));
            });
        });
        assertEquals(1L, r1.get(), "fifo first");
        assertEquals(2L, r2.get(), "fifo second");
    }

    /**
     * Sender blocks when the buffer is full; a recv unblocks it and the
     * buffered values keep FIFO order. verifies: GG-0017
     */
    public void testBufferedBlocksWhenFull() throws Exception {
        AtomicBoolean thirdSent = new AtomicBoolean();
        AtomicReference<Object> firstRecv = new AtomicReference<>();
        withScheduler(2, () -> {
            GoChan ch = new GoChan(2, 0L);
            sched.spawn(() -> {
                ChanOps.send(ch, 1L);
                ChanOps.send(ch, 2L);
                ChanOps.send(ch, 3L); // blocks: buffer full
                thirdSent.set(true);
            });
            // Let the sender fill the buffer and park.
            waitFor(() -> ch.buffered() == 2, "buffer full");
            Thread.sleep(200);
            assertTrue(!thirdSent.get(), "sender must be parked on full buffer");
            assertEquals(2, ch.buffered(), "buffer still full while sender parked");
            sched.spawn(() -> firstRecv.set(ChanOps.recv(ch)));
            waitFor(thirdSent::get, "blocked sender to complete after recv");
        });
        assertEquals(1L, firstRecv.get(), "receiver got first buffered value");
        assertTrue(thirdSent.get(), "third send completed");
    }

    /**
     * A blocked send does NOT pin its carrier (P2-D4): at GOMAXPROCS=1 a
     * sender parked on an unbuffered send must release its in-flight
     * slot so a peer goroutine still runs. A raw Java block here would
     * deadlock the test (carrier pinned, cap=1).
     * verifies: GG-0017
     */
    public void testBlockedSendDoesNotPinCarrier() throws Exception {
        AtomicBoolean peerRan = new AtomicBoolean();
        AtomicBoolean senderDone = new AtomicBoolean();
        withScheduler(1, () -> {
            GoChan ch = new GoChan(0, 0L);
            sched.spawn(() -> {
                ChanOps.send(ch, 1L); // parks: no receiver, unbuffered
                senderDone.set(true);
            });
            sched.spawn(() -> peerRan.set(true));
            // The peer must run while the sender is parked. If the send
            // blocked the carrier thread, the peer could never be
            // admitted at cap=1 and this would time out.
            waitFor(peerRan::get, "peer progress while sender parked");
            assertTrue(sched.maxInflightObserved() <= 1, "cap=1 honored");
            // Unblock the sender so quiescence is reachable.
            sched.spawn(() -> ChanOps.recv(ch));
        });
        assertTrue(peerRan.get(), "peer ran");
        assertTrue(senderDone.get(), "sender completed after recv");
    }

    // ---------- close semantics ----------

    /**
     * close() wakes a parked receiver with (zero, ok=false).
     * verifies: GG-0017
     */
    public void testCloseWakesReceiver() throws Exception {
        AtomicReference<GoChan.RecvResult> rr = new AtomicReference<>();
        withScheduler(2, () -> {
            GoChan ch = new GoChan(0, 0L);
            sched.spawn(() -> rr.set(ChanOps.recvOk(ch)));
            Thread.sleep(200); // let the receiver park
            ChanOps.close(ch); // close is non-blocking: safe on any thread
        });
        assertEquals(0L, rr.get().value(), "closed recv gives zero");
        assertTrue(!rr.get().ok(), "closed recv ok=false");
    }

    /**
     * close() wakes a parked sender; the sender panics with
     * "send on closed channel" (Go semantics). verifies: GG-0017
     */
    public void testCloseWakesSenderToPanic() throws Exception {
        AtomicReference<Object> panicVal = new AtomicReference<>();
        withScheduler(2, () -> {
            GoChan ch = new GoChan(1, 0L);
            sched.spawn(() -> {
                ChanOps.send(ch, 1L); // fills the buffer
                try {
                    ChanOps.send(ch, 2L); // parks: buffer full
                    panicVal.set("NO_PANIC");
                } catch (PanicException pe) {
                    panicVal.set(pe.goValue().toString());
                }
            });
            waitFor(() -> ch.buffered() == 1, "buffer full");
            Thread.sleep(200); // let the second send park
            ChanOps.close(ch); // main thread: close is non-blocking
        });
        assertEquals("send on closed channel", panicVal.get(),
                "parked sender panics on close");
    }

    /**
     * Direct send on a closed channel panics. verifies: GG-0017
     */
    public void testSendOnClosedPanics() throws Exception {
        AtomicReference<Object> panicVal = new AtomicReference<>();
        withScheduler(2, () -> {
            GoChan ch = new GoChan(1, 0L);
            ChanOps.close(ch);
            sched.spawn(() -> {
                try {
                    ChanOps.send(ch, 1L);
                    panicVal.set("NO_PANIC");
                } catch (PanicException pe) {
                    panicVal.set(pe.goValue().toString());
                }
            });
        });
        assertEquals("send on closed channel", panicVal.get(), "send-on-closed panics");
    }

    /**
     * Closing twice panics ("close of closed channel"). verifies: GG-0017
     */
    public void testCloseOfClosedPanics() throws Exception {
        AtomicReference<Object> panicVal = new AtomicReference<>();
        withScheduler(2, () -> {
            GoChan ch = new GoChan(0, 0L);
            ChanOps.close(ch);
            sched.spawn(() -> {
                try {
                    ChanOps.close(ch);
                    panicVal.set("NO_PANIC");
                } catch (PanicException pe) {
                    panicVal.set(pe.goValue().toString());
                }
            });
        });
        assertEquals("close of closed channel", panicVal.get(), "double close panics");
    }

    /**
     * Recv on a closed channel drains the buffer, then returns
     * (zero, false) forever. verifies: GG-0017
     */
    public void testRecvOnClosedDrainsThenZero() throws Exception {
        AtomicReference<GoChan.RecvResult> r1 = new AtomicReference<>();
        AtomicReference<GoChan.RecvResult> r2 = new AtomicReference<>();
        AtomicReference<GoChan.RecvResult> r3 = new AtomicReference<>();
        AtomicReference<GoChan.RecvResult> r4 = new AtomicReference<>();
        withScheduler(2, () -> {
            GoChan ch = new GoChan(2, 0L);
            sched.spawn(() -> {
                ChanOps.send(ch, 10L);
                ChanOps.send(ch, 20L);
                ChanOps.close(ch);
                r1.set(ChanOps.recvOk(ch));
                r2.set(ChanOps.recvOk(ch));
                r3.set(ChanOps.recvOk(ch));
                r4.set(ChanOps.recvOk(ch));
            });
        });
        assertEquals(10L, r1.get().value(), "drain 1");
        assertTrue(r1.get().ok(), "drain 1 ok");
        assertEquals(20L, r2.get().value(), "drain 2");
        assertTrue(r2.get().ok(), "drain 2 ok");
        assertEquals(0L, r3.get().value(), "drained zero");
        assertTrue(!r3.get().ok(), "drained ok=false");
        assertEquals(0L, r4.get().value(), "zero repeats");
        assertTrue(!r4.get().ok(), "ok=false repeats");
    }

    /**
     * len/cap: buffered count and capacity. verifies: GG-0017
     */
    public void testLenCap() throws Exception {
        withScheduler(2, () -> {
            GoChan ch = new GoChan(3, 0L);
            assertEquals(0, ChanOps.len(ch), "len empty");
            assertEquals(3, ChanOps.cap(ch), "cap");
            ChanOps.send(ch, 1L);
            ChanOps.send(ch, 2L);
            assertEquals(2, ChanOps.len(ch), "len after sends");
            ChanOps.recv(ch);
            assertEquals(1, ChanOps.len(ch), "len after recv");
            GoChan unbuf = new GoChan(0, 0L);
            assertEquals(0, ChanOps.cap(unbuf), "unbuffered cap 0");
        });
    }

    // ---------- select ----------

    /**
     * Select over two recv cases picks the ready one and delivers its
     * value. verifies: GG-0017
     */
    public void testSelectRecvChoosesReady() throws Exception {
        AtomicInteger idx = new AtomicInteger(-1);
        AtomicReference<Object> val = new AtomicReference<>();
        AtomicBoolean ok = new AtomicBoolean(true);
        withScheduler(2, () -> {
            GoChan a = new GoChan(1, 0L);
            GoChan b = new GoChan(1, 0L);
            ChanOps.send(a, 111L); // only a is ready
            sched.spawn(() -> {
                int i = ChanOps.selectGo(new ChanOps.SelectCase[]{
                        new ChanOps.SelectCase(a, false, null),
                        new ChanOps.SelectCase(b, false, null),
                });
                idx.set(i);
                val.set(ChanOps.selectRecvValue());
                ok.set(ChanOps.selectRecvOk());
            });
        });
        assertEquals(0, idx.get(), "ready case chosen");
        assertEquals(111L, val.get(), "value delivered");
        assertTrue(ok.get(), "ok=true");
    }

    /**
     * Select with default: no case ready → default (chan==null) wins.
     * verifies: GG-0017
     */
    public void testSelectDefault() throws Exception {
        AtomicInteger idx = new AtomicInteger(-1);
        withScheduler(2, () -> {
            GoChan a = new GoChan(0, 0L);
            sched.spawn(() -> idx.set(ChanOps.selectGo(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(a, false, null),
                    new ChanOps.SelectCase(null, false, null), // default
            })));
        });
        assertEquals(1, idx.get(), "default chosen when nothing ready");
    }

    /**
     * Default does not preempt a ready case. verifies: GG-0017
     */
    public void testSelectDefaultLosesToReady() throws Exception {
        AtomicInteger idx = new AtomicInteger(-1);
        withScheduler(2, () -> {
            GoChan a = new GoChan(1, 0L);
            ChanOps.send(a, 7L);
            sched.spawn(() -> idx.set(ChanOps.selectGo(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(a, false, null),
                    new ChanOps.SelectCase(null, false, null), // default
            })));
        });
        assertEquals(0, idx.get(), "ready case beats default");
    }

    /**
     * Select blocks until a case becomes ready (scheduler-aware park).
     * verifies: GG-0017
     */
    public void testSelectBlocksUntilReady() throws Exception {
        AtomicInteger idx = new AtomicInteger(-1);
        AtomicReference<Object> val = new AtomicReference<>();
        AtomicBoolean selected = new AtomicBoolean();
        withScheduler(2, () -> {
            GoChan ch = new GoChan(0, 0L);
            sched.spawn(() -> {
                int i = ChanOps.selectGo(new ChanOps.SelectCase[]{
                        new ChanOps.SelectCase(ch, false, null),
                });
                idx.set(i);
                val.set(ChanOps.selectRecvValue());
                selected.set(true);
            });
            Thread.sleep(200);
            assertTrue(!selected.get(), "select must block with no ready case");
            sched.spawn(() -> ChanOps.send(ch, 42L));
            waitFor(selected::get, "select to complete after send");
        });
        assertEquals(0, idx.get(), "case index");
        assertEquals(42L, val.get(), "value delivered to blocked select");
    }

    /**
     * Select fairness: two always-ready recv cases; over many iterations
     * neither starves (Go's uniform random choice). verifies: GG-0017
     */
    public void testSelectFairness() throws Exception {
        int[] counts = new int[2];
        withScheduler(2, () -> {
            GoChan a = new GoChan(1, 0L);
            GoChan b = new GoChan(1, 0L);
            sched.spawn(() -> {
                int n = 2000;
                for (int i = 0; i < n; i++) {
                    // Keep both ready: refill whichever was consumed.
                    if (ChanOps.len(a) == 0) {
                        ChanOps.send(a, 1L);
                    }
                    if (ChanOps.len(b) == 0) {
                        ChanOps.send(b, 2L);
                    }
                    int idx = ChanOps.selectGo(new ChanOps.SelectCase[]{
                            new ChanOps.SelectCase(a, false, null),
                            new ChanOps.SelectCase(b, false, null),
                    });
                    counts[idx]++;
                    // The chosen case's value was consumed by the select;
                    // the next iteration refills that channel.
                }
            });
        });
        assertTrue(counts[0] > 600, "case 0 not starved: " + counts[0]);
        assertTrue(counts[1] > 600, "case 1 not starved: " + counts[1]);
        assertEquals(2000, counts[0] + counts[1], "all iterations counted");
    }

    /**
     * Select send case completes via direct handoff to a parked
     * receiver. verifies: GG-0017
     */
    public void testSelectSendHandoff() throws Exception {
        AtomicInteger idx = new AtomicInteger(-1);
        AtomicReference<Object> got = new AtomicReference<>();
        withScheduler(2, () -> {
            GoChan ch = new GoChan(0, 0L);
            // Park a plain receiver first.
            sched.spawn(() -> got.set(ChanOps.recv(ch)));
            Thread.sleep(200);
            // Select with a send case: must hand off to the receiver.
            sched.spawn(() -> idx.set(ChanOps.selectGo(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(ch, true, 99L),
            })));
        });
        assertEquals(0, idx.get(), "send case chosen");
        assertEquals(99L, got.get(), "receiver got the select-sent value");
    }

    /**
     * Select blocked on recv wakes when the channel closes, observing
     * (zero, ok=false). verifies: GG-0017
     */
    public void testSelectWakesOnClose() throws Exception {
        AtomicInteger idx = new AtomicInteger(-1);
        AtomicReference<Object> val = new AtomicReference<>();
        AtomicBoolean ok = new AtomicBoolean(true);
        withScheduler(2, () -> {
            GoChan ch = new GoChan(0, 0L);
            sched.spawn(() -> {
                int i = ChanOps.selectGo(new ChanOps.SelectCase[]{
                        new ChanOps.SelectCase(ch, false, null),
                });
                idx.set(i);
                val.set(ChanOps.selectRecvValue());
                ok.set(ChanOps.selectRecvOk());
            });
            Thread.sleep(200);
            ChanOps.close(ch);
        });
        assertEquals(0, idx.get(), "closed case chosen");
        assertEquals(0L, val.get(), "zero value");
        assertTrue(!ok.get(), "ok=false on close");
    }

    /**
     * Select over two channels, one closed while parked: the close wins
     * with (zero, false). verifies: GG-0017
     */
    public void testSelectCloseWhileParked() throws Exception {
        AtomicInteger idx = new AtomicInteger(-1);
        AtomicBoolean ok = new AtomicBoolean(true);
        withScheduler(2, () -> {
            GoChan a = new GoChan(0, 0L);
            GoChan b = new GoChan(0, 0L);
            sched.spawn(() -> {
                int i = ChanOps.selectGo(new ChanOps.SelectCase[]{
                        new ChanOps.SelectCase(a, false, null),
                        new ChanOps.SelectCase(b, false, null),
                });
                idx.set(i);
                ok.set(ChanOps.selectRecvOk());
            });
            Thread.sleep(200);
            ChanOps.close(b);
        });
        assertEquals(1, idx.get(), "closed channel's case chosen");
        assertTrue(!ok.get(), "ok=false");
    }

    /**
     * Select with only a send-on-closed case and no default panics.
     * verifies: GG-0017
     */
    public void testSelectSendOnClosedPanics() throws Exception {
        AtomicReference<Object> panicVal = new AtomicReference<>();
        withScheduler(2, () -> {
            GoChan ch = new GoChan(0, 0L);
            ChanOps.close(ch);
            sched.spawn(() -> {
                try {
                    ChanOps.selectGo(new ChanOps.SelectCase[]{
                            new ChanOps.SelectCase(ch, true, 1L),
                    });
                    panicVal.set("NO_PANIC");
                } catch (PanicException pe) {
                    panicVal.set(pe.goValue().toString());
                }
            });
        });
        assertEquals("send on closed channel", panicVal.get(),
                "select send-on-closed panics");
    }

    // ---------- stress ----------

    /**
     * Ping-pong stress: many goroutines × many round trips on one
     * unbuffered channel; every value must arrive exactly once (wakeup
     * accounting under contention). verifies: GG-0017
     */
    public void testPingPongStress() throws Exception {
        int goroutines = 20;
        int trips = 50;
        AtomicInteger received = new AtomicInteger();
        AtomicInteger sum = new AtomicInteger();
        withScheduler(4, () -> {
            GoChan ch = new GoChan(0, 0L);
            for (int g = 0; g < goroutines; g++) {
                final long base = (long) g * trips;
                sched.spawn(() -> {
                    for (long i = 0; i < trips; i++) {
                        ChanOps.send(ch, base + i);
                    }
                });
            }
            sched.spawn(() -> {
                for (int i = 0; i < goroutines * trips; i++) {
                    Object v = ChanOps.recv(ch);
                    sum.addAndGet(((Long) v).intValue());
                    received.incrementAndGet();
                }
            });
        });
        assertEquals(goroutines * trips, received.get(), "all values received");
        int expected = 0;
        for (int g = 0; g < goroutines; g++) {
            for (int i = 0; i < trips; i++) {
                expected += g * trips + i;
            }
        }
        assertEquals(expected, sum.get(), "sum matches (no lost/duplicated values)");
    }
}
