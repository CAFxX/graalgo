package work;

import usched.*;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ThreadLocalRandom;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;

/**
 * Lost-wakeup stress (falsifier 6): 64 channel ping-pong pairs (blocking
 * send/recv -> exercises the armed-ticket wakeup path), 32 spinners
 * (quantum parks), 32 sleepers (timer wakeups). Deterministic termination;
 * joins with timeout -> any hang is FAIL. Ends with awaitQuiescent and the
 * scheduler accounting invariant: admissions == parks(INIT+RESUME+IDLE)
 * once every parked goroutine has been woken.
 */
public final class LostWakeupStress {
    public static void main(String[] args) throws Exception {
        int nPairs = 64, nSpin = 32, nSleep = 32, nPings = 300;
        GoTimer.start();
        UserScheduler us = UserScheduler.instance();
        AtomicLong errors = new AtomicLong(0);
        List<Thread> all = new ArrayList<>();
        CountDownLatch done = new CountDownLatch(nPairs * 2 + nSpin + nSleep);

        for (int i = 0; i < nPairs; i++) {
            GoChannel<Long> ping = new GoChannel<>();
            GoChannel<Long> pong = new GoChannel<>();
            GoChannel<Long> dch = new GoChannel<>();
            final int id = i;
            all.add(Go.go("pairA-" + id, () -> {
                try {
                    for (;;) {
                        GoSelect.SelResult<Long> sr = GoSelect.selectRecv(List.of(ping, dch));
                        if (sr.index() == 1) break;
                        pong.send(sr.value() == null ? -1 : sr.value() + 1);
                    }
                } catch (Throwable th) { errors.incrementAndGet(); th.printStackTrace(); }
                done.countDown();
            }));
            all.add(Go.go("pairB-" + id, () -> {
                try {
                    for (int k = 0; k < nPings; k++) {
                        long t0 = System.nanoTime();
                        ping.send(t0);
                        long rt = pong.recv();
                        if (rt <= t0) { errors.incrementAndGet(); }
                        if ((k & 31) == 0) Safepoint.poll();
                    }
                    dch.send(0L);
                } catch (Throwable th) { errors.incrementAndGet(); th.printStackTrace(); }
                done.countDown();
            }));
        }
        for (int i = 0; i < nSpin; i++) {
            all.add(Go.go("spin-" + i, () -> {
                try {
                    long sink = 0, c = 0;
                    long end = System.nanoTime() + 15_000_000_000L;
                    while (System.nanoTime() < end) {
                        sink += c * 0x9E3779B97F4A7C15L; sink ^= sink >>> 29; c++;
                        Safepoint.poll();
                    }
                    Blackhole.consume(sink);
                } catch (Throwable th) { errors.incrementAndGet(); th.printStackTrace(); }
                done.countDown();
            }));
        }
        for (int i = 0; i < nSleep; i++) {
            all.add(Go.go("sleep-" + i, () -> {
                try {
                    ThreadLocalRandom r = ThreadLocalRandom.current();
                    for (int k = 0; k < 60; k++) GoTimer.sleep(r.nextInt(1_000_000, 5_000_000));
                } catch (Throwable th) { errors.incrementAndGet(); th.printStackTrace(); }
                done.countDown();
            }));
        }

        boolean finished = done.await(120, TimeUnit.SECONDS);
        long parks = us.parkEvents.sum();
        long admissions = us.admissions.sum();
        us.awaitQuiescent();
        // invariant: every INIT/RESUME/IDLE park was admitted exactly once.
        // (All IDLE parks were woken by construction: channels drained via
        //  done, timers fired.)
        boolean invariant = admissions == parks;
        System.out.printf("stress finished=%s errors=%d parks=%d admissions=%d wakeupCalls=%d wakeupWon=%d invariant=%s%n",
                finished, errors.get(), parks, admissions, us.wakeupCalls.sum(), us.wakeupWon.sum(),
                invariant ? "OK" : "VIOLATED");
        System.out.println(us.statsLine());
        boolean ok = finished && errors.get() == 0 && invariant;
        System.out.printf("RESULT=%s%n", ok ? "PASS" : "FAIL");
        System.out.flush();
        System.exit(ok ? 0 : 2);
    }
}
