package graalgo.sched;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

import graalgo.runtime.ChanOps;
import graalgo.runtime.GoTimer;
import graalgo.runtime.Timers;

/**
 * P2-M3 timer differential tests (WS C; GG-SCHED-0004).
 *
 * <p>Each test is a mechanical Java translation of the Go program in
 * {@code phases/phase2/corpus/timers/<name>.go} (translation convention
 * documented below), executed on the user-space scheduler. Output is
 * diffed byte-for-byte against the gc-recorded {@code <name>.expected}
 * file. Only event ORDER is significant — never wall-clock values
 * (sleep margins are ≥300ms; 50ms separations sat inside the measured
 * fire-lateness noise band and flaked — P1-2).
 *
 * <p>Translation convention (1:1 syntactic; timing semantics come from
 * the runtime under test, not the translation):
 * <ul>
 *   <li>{@code time.Sleep(d)} → {@code Timers.sleep(d.toNanos())}
 *       (nanos literals below)
 *   <li>{@code time.After(d)} → {@code Timers.after(ns)}
 *   <li>{@code t := time.NewTimer(d)} → {@code GoTimer.newTimer(ns)};
 *       {@code t.Stop()} → {@code t.stop()};
 *       {@code t.Reset(d)} → {@code t.reset(ns)};
 *       {@code <-t.C} → {@code ChanOps.recv(t.channel())}
 *   <li>{@code tk := time.NewTicker(d)} → {@code GoTimer.newTicker(ns)}
 *   <li>{@code go f()} → {@code sched.spawn(() -> ...)}
 *   <li>{@code fmt.Println(a, b)} → out.append(a).append(' ').append(b)...
 *   <li>{@code select} → {@code ChanOps.selectGo} + selectRecvValue
 *       ({@code default:} is a null-chan case, chosen index checked)
 * </ul>
 *
 * <p>These .go files double as the future end-to-end corpus: once the
 * exporter lowers the {@code time} package (parent dependency), the
 * same programs run through {@code graalgo-run} and must produce
 * identical output.
 *
 * <p>verifies: GG-SCHED-0004 (differential slice)
 */
public final class TimerDifferentialTest {

    private SchedulerCore sched;
    private static final Path CORPUS = Paths.get("corpus/timers");

    private static void assertEquals(String expected, String actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + "\nexpected:\n" + expected + "\nactual:\n" + actual);
        }
    }

    private String expected(String name) throws Exception {
        return Files.readString(CORPUS.resolve(name + ".expected"), StandardCharsets.UTF_8);
    }

    private void withScheduler(int gmp, ThrowingRunnable body) throws Exception {
        sched = new SchedulerCore(gmp, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            body.run();
            long deadline = System.currentTimeMillis() + 30000;
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

    // tm01: sleep completion order.
    public void testTm01SleepOrder() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                ChanOps.Chan done = ChanOps.makeChan(8, 3, "");
                // P1-2: separations ≥300ms; 50ms sat inside the measured
                // fire-lateness noise band (up to ~69ms) and flaked.
                sched.spawn(() -> {
                    Timers.sleep(900_000_000L);
                    ChanOps.send(done, "slow");
                });
                sched.spawn(() -> {
                    Timers.sleep(300_000_000L);
                    ChanOps.send(done, "fast");
                });
                sched.spawn(() -> {
                    Timers.sleep(600_000_000L);
                    ChanOps.send(done, "mid");
                });
                for (int i = 0; i < 3; i++) {
                    out.append(ChanOps.recv(done)).append('\n');
                }
            });
        });
        assertEquals(expected("tm01_sleep_order"), out.toString(), "tm01");
    }

    // tm02: select with time.After → timeout branch.
    public void testTm02AfterTimeout() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                ChanOps.Chan ch = ChanOps.makeChan(8, 0, 0L);
                int idx = ChanOps.selectGo(new ChanOps.SelectCase[]{
                        new ChanOps.SelectCase(ch, false, null),
                        new ChanOps.SelectCase(Timers.after(100_000_000L), false, null),
                });
                if (idx == 0) {
                    out.append("value ").append(ChanOps.selectRecvValue()).append('\n');
                } else {
                    out.append("timeout\n");
                }
            });
        });
        assertEquals(expected("tm02_after_timeout"), out.toString(), "tm02");
    }

    // tm03: <-time.After fires.
    public void testTm03AfterValue() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                ChanOps.recv(Timers.after(50_000_000L));
                out.append("fired\n");
            });
        });
        assertEquals(expected("tm03_after_value"), out.toString(), "tm03");
    }

    // tm04: NewTimer + Stop before fire → no fire.
    public void testTm04TimerStop() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                GoTimer t = GoTimer.newTimer(200_000_000L);
                out.append("stop: ").append(t.stop()).append('\n');
                int idx = ChanOps.selectGo(new ChanOps.SelectCase[]{
                        new ChanOps.SelectCase(t.channel(), false, null),
                        new ChanOps.SelectCase(Timers.after(300_000_000L), false, null),
                });
                out.append(idx == 0 ? "FIRED (bug)" : "no fire").append('\n');
            });
        });
        assertEquals(expected("tm04_timer_stop"), out.toString(), "tm04");
    }

    // tm05: Stop on expired-undrained → true, pending value drained
    // (Go 1.23+ parity).
    public void testTm05TimerStopAfterFire() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                GoTimer t = GoTimer.newTimer(20_000_000L);
                Timers.sleep(100_000_000L); // fired, not drained
                out.append("stop: ").append(t.stop()).append('\n');
                int idx = ChanOps.selectGo(new ChanOps.SelectCase[]{
                        new ChanOps.SelectCase(t.channel(), false, null),
                        new ChanOps.SelectCase(null, false, null),
                });
                out.append(idx == 0 ? "value leaked (bug)" : "drained").append('\n');
            });
        });
        assertEquals(expected("tm05_timer_stop_after_fire"), out.toString(), "tm05");
    }

    // tm06: Reset on active timer → true, fires at new deadline.
    public void testTm06TimerResetActive() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                GoTimer t = GoTimer.newTimer(500_000_000L);
                out.append("reset: ").append(t.reset(20_000_000L)).append('\n');
                ChanOps.recv(t.channel());
                out.append("fired\n");
            });
        });
        assertEquals(expected("tm06_timer_reset_active"), out.toString(), "tm06");
    }

    // tm07: Reset on expired+drained → false, fires at new deadline.
    public void testTm07TimerResetExpired() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                GoTimer t = GoTimer.newTimer(20_000_000L);
                Timers.sleep(100_000_000L);
                ChanOps.recv(t.channel()); // drain
                out.append("reset: ").append(t.reset(50_000_000L)).append('\n');
                ChanOps.recv(t.channel());
                out.append("fired\n");
            });
        });
        assertEquals(expected("tm07_timer_reset_expired"), out.toString(), "tm07");
    }

    // tm08: Reset on expired+undrained → true (Go 1.23+), stale value
    // dropped, exactly one new fire.
    public void testTm08TimerResetUndrained() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                GoTimer t = GoTimer.newTimer(20_000_000L);
                Timers.sleep(100_000_000L); // fired, not drained
                out.append("reset: ").append(t.reset(50_000_000L)).append('\n');
                ChanOps.recv(t.channel());
                out.append("fired\n");
                int idx = ChanOps.selectGo(new ChanOps.SelectCase[]{
                        new ChanOps.SelectCase(t.channel(), false, null),
                        new ChanOps.SelectCase(null, false, null),
                });
                out.append(idx == 0 ? "stale value (bug)" : "no stale value").append('\n');
            });
        });
        assertEquals(expected("tm08_timer_reset_undrained"), out.toString(), "tm08");
    }

    // tm09: NewTicker, 3 ticks, Stop.
    public void testTm09Ticker() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                GoTimer tk = GoTimer.newTicker(50_000_000L);
                for (int i = 0; i < 3; i++) {
                    ChanOps.recv(tk.channel());
                    out.append("tick\n");
                }
                tk.stop();
                out.append("stopped\n");
            });
        });
        assertEquals(expected("tm09_ticker"), out.toString(), "tm09");
    }

    // tm10: slow ticker receiver → ticks dropped.
    public void testTm10TickerDrop() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                GoTimer tk = GoTimer.newTicker(50_000_000L);
                Timers.sleep(300_000_000L); // slow receiver
                int n = 0;
                for (;;) {
                    int idx = ChanOps.selectGo(new ChanOps.SelectCase[]{
                            new ChanOps.SelectCase(tk.channel(), false, null),
                            new ChanOps.SelectCase(null, false, null),
                    });
                    if (idx == 1) {
                        break;
                    }
                    ChanOps.selectRecvValue();
                    n++;
                }
                tk.stop();
                out.append(n < 6 ? "dropped" : "no drop (bug)").append('\n');
            });
        });
        assertEquals(expected("tm10_ticker_drop"), out.toString(), "tm10");
    }

    // tm11: select+After — value branch first, then timeout branch.
    public void testTm11SelectAfterBoth() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                ChanOps.Chan ch = ChanOps.makeChan(8, 1, "");
                ChanOps.send(ch, "fast");
                int idx = ChanOps.selectGo(new ChanOps.SelectCase[]{
                        new ChanOps.SelectCase(ch, false, null),
                        new ChanOps.SelectCase(Timers.after(100_000_000L), false, null),
                });
                if (idx == 0) {
                    out.append("value ").append(ChanOps.selectRecvValue()).append('\n');
                } else {
                    out.append("timeout (bug)\n");
                }
                int idx2 = ChanOps.selectGo(new ChanOps.SelectCase[]{
                        new ChanOps.SelectCase(ch, false, null),
                        new ChanOps.SelectCase(Timers.after(100_000_000L), false, null),
                });
                if (idx2 == 0) {
                    out.append("value (bug) ").append(ChanOps.selectRecvValue()).append('\n');
                } else {
                    out.append("timeout\n");
                }
            });
        });
        assertEquals(expected("tm11_select_after_both"), out.toString(), "tm11");
    }

    // tm12: Sleep(0)/Sleep(-1) return immediately.
    public void testTm12SleepZero() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                Timers.sleep(0);
                Timers.sleep(-1);
                out.append("ok\n");
            });
        });
        assertEquals(expected("tm12_sleep_zero"), out.toString(), "tm12");
    }

    // tm13: Stop on a live ticker with a buffered tick drains it (Go
    // 1.23+); no stale tick is observable after Stop.
    public void testTm13TickerStopBuffered() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                GoTimer tk = GoTimer.newTicker(20_000_000L);
                try {
                    Thread.sleep(300);
                } catch (InterruptedException ie) {
                    // ignore
                }
                tk.stop();
                out.append("stopped\n");
                out.append("buffered after stop: ")
                        .append(ChanOps.len(tk.channel())).append("\n");
                out.append("done\n");
            });
        });
        assertEquals(expected("tm13_ticker_stop_buffered"), out.toString(), "tm13");
    }

    // tm14: Reset on a live ticker with a buffered tick drains the stale
    // tick; the next receive is a fresh post-reset tick.
    public void testTm14TickerResetBuffered() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                GoTimer tk = GoTimer.newTicker(20_000_000L);
                try {
                    Thread.sleep(300);
                } catch (InterruptedException ie) {
                    // ignore
                }
                tk.reset(150_000_000L);
                out.append("reset\n");
                long t0 = System.nanoTime();
                ChanOps.recv(tk.channel());
                long dt = System.nanoTime() - t0;
                out.append(dt > 75_000_000L ? "fresh tick\n" : "STALE tick\n");
                tk.stop();
            });
        });
        assertEquals(expected("tm14_ticker_reset_buffered"), out.toString(), "tm14");
    }

    // tm15: len/cap of timer channels report 0 in every state (gc presents
    // timer channels as unbuffered). Red-green target for DIV-M3-1:
    // fails while GoTimer uses a plain 1-buffered GoChan (len/cap = 1/1
    // after the fire), passes with the TimerChan flavor.
    //
    // <p>verifies: GG-0048 (timer slice)
    public void testTm15TimerChanLenCap() throws Exception {
        StringBuilder out = new StringBuilder();
        withScheduler(2, () -> {
            sched.spawn(() -> {
                GoTimer t = GoTimer.newTimer(100_000_000L);
                out.append("timer pre-fire: ")
                        .append(ChanOps.len(t.channel())).append(' ')
                        .append(ChanOps.cap(t.channel())).append('\n');
                Timers.sleep(400_000_000L); // fired, undrained
                out.append("timer fired undrained: ")
                        .append(ChanOps.len(t.channel())).append(' ')
                        .append(ChanOps.cap(t.channel())).append('\n');
                ChanOps.recv(t.channel());
                out.append("timer drained: ")
                        .append(ChanOps.len(t.channel())).append(' ')
                        .append(ChanOps.cap(t.channel())).append('\n');
                GoTimer tk = GoTimer.newTicker(20_000_000L);
                Timers.sleep(400_000_000L); // ticks collapse to 1 buffered
                out.append("ticker fired: ")
                        .append(ChanOps.len(tk.channel())).append(' ')
                        .append(ChanOps.cap(tk.channel())).append('\n');
                tk.stop();
                out.append("ticker stopped: ")
                        .append(ChanOps.len(tk.channel())).append(' ')
                        .append(ChanOps.cap(tk.channel())).append('\n');
                out.append("done\n");
            });
        });
        assertEquals(expected("tm15_timer_chan_len_cap"), out.toString(), "tm15");
    }
}
