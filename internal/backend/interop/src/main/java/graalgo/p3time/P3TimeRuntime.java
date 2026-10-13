package graalgo.p3time;

import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicInteger;

import graalgo.sched.SchedulerCore;

/**
 * Phase-3 time runtime (P3-M5, WS E, ADR-0005): the background fire-value
 * translators owned by {@link GoTimerT}.
 *
 * <p>Why background: a translator goroutine pumps fire values from the
 * Phase-2 heap channel (which carries the fire nanotime as a
 * {@link Long}) to the user-visible {@link TimeChan} (which must carry
 * {@code time.Time} structs). For tickers the translator loops until the
 * program ends — gc's {@code time.Tick} leaks the ticker by design, and
 * process exit abandons it. But {@link SchedulerCore#awaitQuiescence}
 * waits for <em>every</em> spawned goroutine, so a parked translator
 * would hang program exit. The launcher therefore ends programs in two
 * phases ({@link #awaitBackgroundDrain}): wait until only translators
 * remain live, shut their {@code done} channels, then await full
 * quiescence. This also gives gc parity for the early-return case: a
 * pending {@code time.After(time.Hour)} must not delay exit, and a
 * pending {@code AfterFunc} must not run after main returns.
 *
 * <p>Registration and the translator spawn are atomic under
 * {@link #bgLock}: the drain loop's {@code live <= background} check
 * must never observe "registered but not yet spawned" (that would shut
 * down a timer whose Go owner is still alive and about to use it).
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class P3TimeRuntime {
    private P3TimeRuntime() {}

    /** Guards background registration vs the drain check (see above). */
    private static final Object bgLock = new Object();

    /** Live background translators (a subset of the scheduler's live goroutines). */
    private static final Set<GoTimerT> background = ConcurrentHashMap.newKeySet();

    /** Mirrors {@link #background}'s size; decremented before the goroutine's live count. */
    private static final AtomicInteger backgroundLive = new AtomicInteger(0);

    /**
     * Registers {@code timer} and spawns its translator, atomically w.r.t.
     * {@link #awaitBackgroundDrain}'s check.
     */
    static void spawnTranslator(GoTimerT timer, SchedulerCore sched, Runnable body) {
        synchronized (bgLock) {
            background.add(timer);
            backgroundLive.incrementAndGet();
            try {
                sched.spawn(body);
            } catch (RuntimeException e) {
                background.remove(timer);
                backgroundLive.decrementAndGet();
                throw e;
            }
            bgLock.notifyAll();
        }
    }

    /** Deregisters a finished translator. */
    static void deregister(GoTimerT timer) {
        synchronized (bgLock) {
            if (background.remove(timer)) {
                backgroundLive.decrementAndGet();
            }
            bgLock.notifyAll();
        }
    }

    /** Current background-translator count (test probe). */
    public static int backgroundLive() {
        return backgroundLive.get();
    }

    /**
     * Idempotent shutdown of every background translator: closes each
     * {@code done} channel so parked translators wake and exit. Safe to
     * call with none registered.
     */
    public static void shutdownBackground() {
        for (GoTimerT t : background) {
            t.shutdownTranslator();
        }
    }

    /**
     * Program-end phase 1 (called by the launcher instead of awaiting
     * quiescence directly): waits until every live goroutine is a
     * background timer translator, then shuts them down. The caller then
     * runs the normal {@link SchedulerCore#awaitQuiescence}.
     *
     * <p>With no timer translators registered this reduces to waiting for
     * ordinary quiescence (1ms poll granularity at exit).
     */
    public static void awaitBackgroundDrain(SchedulerCore sched) throws InterruptedException {
        for (;;) {
            final int live;
            final int bg;
            synchronized (bgLock) {
                live = sched.runnableCount();
                bg = backgroundLive.get();
            }
            // bg <= live always: deregister runs before the goroutine's
            // live-count decrement, and register+spawn are atomic above —
            // so live <= bg means every live goroutine is background.
            if (live <= bg) {
                break;
            }
            Thread.sleep(1);
        }
        shutdownBackground();
    }
}
