package graalgo.sched;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.runtime.ChanOps;
import graalgo.runtime.GoChan;
import graalgo.runtime.PanicException;

/**
 * P2-M2 channel differential tests (WS B; GG-0017 chan slice).
 *
 * <p>Each test is a mechanical Java translation of the Go program in
 * {@code phases/phase2/corpus/channels/<name>.go} (translation
 * convention documented below), executed on the user-space scheduler.
 * Output is diffed byte-for-byte against the gc-recorded
 * {@code <name>.expected} file.
 *
 * <p>Translation convention (1:1 syntactic; concurrency semantics come
 * from the runtime under test, not the translation):
 * <ul>
 *   <li>{@code make(chan int, n)} → {@code ChanOps.makeChan(8, n, 0L)}
 *   <li>{@code ch <- v} → {@code ChanOps.send(ch, v)}
 *   <li>{@code <-ch} → {@code ChanOps.recv(ch)};
 *       {@code v, ok := <-ch} → {@code ChanOps.recvOk(ch)}
 *   <li>{@code close(ch)} → {@code ChanOps.close(ch)}
 *   <li>{@code go f()} → {@code sched.spawn(() -> ...)}
 *   <li>{@code fmt.Println(a, b)} → out.append(a).append(' ').append(b)...
 *   <li>{@code select} → {@code ChanOps.selectGo} + selectRecvValue/Ok
 *   <li>{@code for v := range ch} → recvOk loop until !ok
 * </ul>
 *
 * <p>These .go files double as the future end-to-end corpus: once the
 * exporter lowering lands (parent dependency), the same programs run
 * through {@code graalgo-run} and must produce identical output.
 *
 * <p>verifies: GG-0017 (differential slice)
 */
public final class ChannelDifferentialTest {

    private SchedulerCore sched;
    private static final Path CORPUS =
            Paths.get("../../../test/corpus/phase2/channels");

    private static void assertEquals(String expected, String actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + "\nexpected:\n" + expected + "\nactual:\n" + actual);
        }
    }

    private String expected(String name) throws Exception {
        return Files.readString(CORPUS.resolve(name + ".expected"), StandardCharsets.UTF_8);
    }

    /** Expected first panic line, from gc's stderr. */
    private String expectedPanic(String name) throws Exception {
        return Files.readString(CORPUS.resolve(name + ".panic"), StandardCharsets.UTF_8);
    }

    private void withScheduler(int gmp, ThrowingRunnable body) throws Exception {
        sched = new SchedulerCore(gmp, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            body.run();
            long deadline = System.currentTimeMillis() + 15000;
            while (sched.runnableCount() != 0) {
                if (System.currentTimeMillis() > deadline) {
                    throw new AssertionError("quiescence timeout (lost wakeup?)");
                }
                Thread.sleep(10);
            }
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }

    private interface ThrowingRunnable {
        void run() throws Exception;
    }

    // ch01: unbuffered ping-pong via spawned goroutine.
    public void testCh01Unbuffered() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            ChanOps.Chan ch = ChanOps.makeChan(8, 0, 0L);
            sched.spawn(() -> ChanOps.send(ch, 42L));
            sched.spawn(() -> out.append(ChanOps.recv(ch)).append('\n'));
        });
        assertEquals(expected("ch01_unbuffered"), out.toString(), "ch01");
    }

    // ch02: buffered FIFO.
    public void testCh02Buffered() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            ChanOps.Chan ch = ChanOps.makeChan(8, 3, 0L);
            sched.spawn(() -> {
                ChanOps.send(ch, 1L);
                ChanOps.send(ch, 2L);
                ChanOps.send(ch, 3L);
                out.append(ChanOps.recv(ch)).append('\n');
                out.append(ChanOps.recv(ch)).append('\n');
                out.append(ChanOps.recv(ch)).append('\n');
            });
        });
        assertEquals(expected("ch02_buffered"), out.toString(), "ch02");
    }

    // ch03: close, drain, then (zero, false).
    public void testCh03CloseDrain() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            ChanOps.Chan ch = ChanOps.makeChan(8, 2, 0L);
            sched.spawn(() -> {
                ChanOps.send(ch, 10L);
                ChanOps.send(ch, 20L);
                ChanOps.close(ch);
                for (int i = 0; i < 4; i++) {
                    GoChan.RecvResult r = ChanOps.recvOk(ch);
                    out.append(r.value()).append(' ').append(r.ok()).append('\n');
                }
            });
        });
        assertEquals(expected("ch03_close_drain"), out.toString(), "ch03");
    }

    // ch04: producer goroutine, range-equivalent drain, sum.
    public void testCh04ProducerConsumer() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            ChanOps.Chan ch = ChanOps.makeChan(8, 5, 0L);
            sched.spawn(() -> {
                for (long i = 1; i <= 5; i++) {
                    ChanOps.send(ch, i * i);
                }
                ChanOps.close(ch);
            });
            sched.spawn(() -> {
                long sum = 0;
                for (;;) {
                    GoChan.RecvResult r = ChanOps.recvOk(ch);
                    if (!r.ok()) {
                        break;
                    }
                    sum += (Long) r.value();
                }
                out.append(sum).append('\n');
            });
        });
        assertEquals(expected("ch04_producer_consumer"), out.toString(), "ch04");
    }

    // ch05: select over two recv cases, one ready.
    public void testCh05SelectRecv() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            ChanOps.Chan a = ChanOps.makeChan(8, 1, 0L);
            ChanOps.Chan b = ChanOps.makeChan(8, 1, 0L);
            ChanOps.send(a, 7L);
            sched.spawn(() -> {
                int idx = ChanOps.selectGo(new ChanOps.SelectCase[]{
                        new ChanOps.SelectCase(a, false, null),
                        new ChanOps.SelectCase(b, false, null),
                });
                Object v = ChanOps.selectRecvValue();
                out.append(idx == 0 ? "a" : "b").append(' ').append(v).append('\n');
            });
        });
        assertEquals(expected("ch05_select_recv"), out.toString(), "ch05");
    }

    // ch06: select with default, nothing ready → default branch.
    public void testCh06SelectDefault() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            ChanOps.Chan ch = ChanOps.makeChan(8, 0, 0L);
            sched.spawn(() -> {
                int idx = ChanOps.selectGo(new ChanOps.SelectCase[]{
                        new ChanOps.SelectCase(ch, false, null),
                        new ChanOps.SelectCase(null, false, null),
                });
                // The recv case is never ready here (unbuffered, no
                // sender); the translation mirrors the Go program's two
                // branches by index.
                out.append(idx == 0 ? "recv" : "default").append('\n');
            });
        });
        // gc prints just "default"; our translation prints the branch name.
        // Normalize: the Go program prints "recv <v>" or "default".
        assertEquals(expected("ch06_select_default"), out.toString(), "ch06");
    }

    // ch07: select send case.
    public void testCh07SelectSend() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            ChanOps.Chan ch = ChanOps.makeChan(8, 1, 0L);
            sched.spawn(() -> {
                int idx = ChanOps.selectGo(new ChanOps.SelectCase[]{
                        new ChanOps.SelectCase(ch, true, 99L),
                        new ChanOps.SelectCase(null, false, null),
                });
                out.append(idx == 0 ? "sent" : "default").append('\n');
                out.append(ChanOps.recv(ch)).append('\n');
            });
        });
        assertEquals(expected("ch07_select_send"), out.toString(), "ch07");
    }

    // ch08: blocked receiver woken by close with (0, false).
    public void testCh08CloseWhileBlocked() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            ChanOps.Chan ch = ChanOps.makeChan(8, 0, 0L);
            ChanOps.Chan done = ChanOps.makeChan(8, 1, false);
            sched.spawn(() -> {
                GoChan.RecvResult r = ChanOps.recvOk(ch);
                out.append(r.value()).append(' ').append(r.ok()).append('\n');
                ChanOps.send(done, true);
            });
            sched.spawn(() -> {
                // Races the receiver's park; both orders observe
                // (0, false): parked → woken by close; not yet parked →
                // tryRecv sees the closed channel directly.
                ChanOps.close(ch);
                ChanOps.recv(done);
            });
        });
        assertEquals(expected("ch08_close_while_blocked"), out.toString(), "ch08");
    }

    // ch09: send on closed channel panics (message parity with gc).
    public void testCh09SendOnClosedPanic() throws Exception {
        String want = expectedPanic("ch09_send_on_closed");
        AtomicReference<String> panicLine = new AtomicReference<>();
        withScheduler(2, () -> {
            ChanOps.Chan ch = ChanOps.makeChan(8, 0, 0L);
            ChanOps.close(ch);
            sched.spawn(() -> {
                try {
                    ChanOps.send(ch, 1L);
                } catch (PanicException pe) {
                    panicLine.set("panic: " + pe.goValue());
                }
            });
        });
        assertEquals(want, panicLine.get() + "\n", "ch09");
    }

    // ch10: close of closed channel panics (message parity with gc).
    public void testCh10CloseOfClosedPanic() throws Exception {
        String want = expectedPanic("ch10_close_of_closed");
        AtomicReference<String> panicLine = new AtomicReference<>();
        withScheduler(2, () -> {
            ChanOps.Chan ch = ChanOps.makeChan(8, 0, 0L);
            ChanOps.close(ch);
            sched.spawn(() -> {
                try {
                    ChanOps.close(ch);
                } catch (PanicException pe) {
                    panicLine.set("panic: " + pe.goValue());
                }
            });
        });
        assertEquals(want, panicLine.get() + "\n", "ch10");
    }

    // ch11: select send-on-closed + default panics (gc: poison > default).
    public void testCh11SelectPoisonDefault() throws Exception {
        String want = expectedPanic("ch11_select_poison_default");
        AtomicReference<String> panicLine = new AtomicReference<>();
        withScheduler(2, () -> {
            ChanOps.Chan ch = ChanOps.makeChan(8, 0, 0L);
            ChanOps.close(ch);
            sched.spawn(() -> {
                try {
                    ChanOps.selectGo(new ChanOps.SelectCase[]{
                            new ChanOps.SelectCase(ch, true, 1L),
                            new ChanOps.SelectCase(null, false, null),
                    });
                } catch (PanicException pe) {
                    panicLine.set("panic: " + pe.goValue());
                }
            });
        });
        assertEquals(want, panicLine.get() + "\n", "ch11");
    }

    // ch12: poison ignored when another case is ready (gc parity).
    public void testCh12SelectPoisonReady() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            ChanOps.Chan ch1 = ChanOps.makeChan(8, 0, 0L);
            ChanOps.close(ch1);
            ChanOps.Chan ch2 = ChanOps.makeChan(8, 1, 0L);
            ChanOps.send(ch2, 99L);
            sched.spawn(() -> {
                int idx = ChanOps.selectGo(new ChanOps.SelectCase[]{
                        new ChanOps.SelectCase(ch1, true, 1L),
                        new ChanOps.SelectCase(ch2, false, null),
                });
                if (idx == 0) {
                    out.append("sent ch1\n");
                } else {
                    out.append("recv ch2 ").append(ChanOps.selectRecvValue()).append('\n');
                }
            });
        });
        assertEquals(expected("ch12_select_poison_ready"), out.toString(), "ch12");
    }
}
