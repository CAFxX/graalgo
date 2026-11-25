package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.Unsafe;
import graalgo.go.types.GoType;
import graalgo.go.values.Closure;
import graalgo.go.values.Flat;
import graalgo.jfr.ParkReason;
import graalgo.runtime.ChanOps;
import graalgo.runtime.TimerHeap;
import graalgo.sched.SchedulerCore;
import java.time.Instant;

/**
 * The runtime side of package {@code time} (upstream {@code runtime/time.go}): the clocks, sleep,
 * and the timers behind {@code Timer}, {@code Ticker}, {@code After} and {@code AfterFunc};
 * registered in {@code internal/toolexec/intrinsics/registry.txt}. Everything else of
 * {@code time} — {@code Now}'s encoding, {@code Duration}, formatting, zones — is upstream's Go.
 *
 * <p>Timers live on the scheduler's existing timer heap ({@link TimerHeap}, GG-SCHED/WAKE):
 * {@code Sleep} parks the goroutine with a sleep entry, which the heap's thread unparks through
 * the wake lane; a {@code Timer}'s entry is a callback that starts a runtime goroutine
 * ({@link SchedulerCore#spawnSystem}) to run the timer's Go function ({@code sendTime} or
 * {@code goFunc}) — gc runs it on the timer-running M. The {@code *Timer} {@code newTimer}
 * returns is a {@code time.Timer} value (gc returns its runtime timer, whose prefix has that
 * layout); its runtime state is kept beside it, keyed by identity.
 *
 * <p>Go 1.23 timer channels: a stale value never survives {@code Stop}/{@code Reset} (their
 * drain and the sequence number, under the timer's lock, as gc's {@code sendLock}), and
 * {@code Stop}/{@code Reset} report a fired but unreceived value as pending. Not reproduced:
 * gc reports {@code len}/{@code cap} 0 for a timer channel and sends lazily when a receiver
 * arrives; here the buffered value is visible to {@code len} (docs/arch/limitations.md).
 */
final class TimeIntrinsics {
    private TimeIntrinsics() {}

    // ------------------------------------------------------------------ clocks

    /** {@code nanotime()}: CLOCK_MONOTONIC, as {@link System#nanoTime} on Linux. */
    static long time_runtimeNano(Node node) {
        return System.nanoTime();
    }

    /**
     * {@code time.now} (runtime/timestub.go): wall clock (CLOCK_REALTIME) and {@code nanotime()}
     * itself — the same clock as {@link #time_runtimeNano}; package time subtracts its own
     * {@code startNano} from both.
     */
    @TruffleBoundary
    static Object time_runtimeNow(Node node) {
        Instant now = Instant.now();
        return new Object[] {now.getEpochSecond(), (long) now.getNano(), System.nanoTime()};
    }

    /**
     * {@code crypto/internal/fips140deps/time.monoTime} (gc: {@code time_now}'s monotonic
     * reading, {@code nanotime()}): the clock of {@link #time_runtimeNano}.
     */
    static long crypto_internal_fips140deps_time_monoTime(Node node) {
        return System.nanoTime();
    }

    /** No synctest bubbles (see SyncIntrinsics). */
    static boolean time_runtimeIsBubbled(Node node) {
        return false;
    }

    // ------------------------------------------------------------------ sleep

    /** gc's {@code timeSleep}: park on a sleep timer; d &lt;= 0 returns at once. */
    @TruffleBoundary
    static void time_Sleep(Node node, Object duration) {
        // A time.Duration: the generated table passes named types in their carrier, a Long.
        long d = (Long) duration;
        if (d <= 0) {
            return;
        }
        SchedulerCore sched = SchedulerCore.current();
        long deadline = System.nanoTime() + d;
        if (deadline < 0) {
            deadline = Long.MAX_VALUE;
        }
        // Registered before the park: an early fire's UNPARK is kept by the drainer.
        sched.timerHeap().addSleep(deadline, sched.currentGoroutine());
        sched.parkCurrent(null, ParkReason.TIMER, "sleep");
    }

    // ------------------------------------------------------------------ timers

    /** The runtime half of a timer (gc's {@code timer}). Guarded by its own monitor. */
    private static final class State {
        final GoProcess process;
        final SchedulerCore sched;
        final Closure f;
        final Object arg;
        /** The timer channel ({@code cp}), or null for a func timer. */
        final Object chan;
        long when;
        long period;
        /** Bumped by stop/reset: a fire scheduled for an older sequence does nothing. */
        long seq;
        /** The heap entry, or -1 when none is armed. */
        long heapId = -1;

        State(GoProcess process, SchedulerCore sched, Closure f, Object arg, Object chan) {
            this.process = process;
            this.sched = sched;
            this.f = f;
            this.arg = arg;
            this.chan = chan;
        }
    }

    /** Arms the heap entry for {@code s.when} (lock held). */
    private static void arm(State s) {
        final long seq = s.seq;
        final long when = s.when;
        s.heapId = s.sched.timerHeap().addCallback(when, () -> fire(s, seq, when));
    }

    /** On the timer thread: hand the timer's function to a runtime goroutine. */
    private static void fire(State s, long seq, long when) {
        s.sched.spawnSystem(() -> s.process.run(() -> runTimer(s, seq, when)));
    }

    /** gc's {@code unlockAndRun}: the timer's function, unless stop/reset superseded this fire. */
    private static void runTimer(State s, long seq, long when) {
        long delta;
        synchronized (s) {
            if (s.seq != seq) {
                return; // stopped or reset since this fire was armed
            }
            delta = System.nanoTime() - when;
            if (s.period > 0) {
                // gc: when += period * (1 + (now-when)/period), and the timer stays armed.
                long next = when + s.period * (1 + Math.max(0, delta) / s.period);
                s.when = next < 0 ? Long.MAX_VALUE : next;
                arm(s);
            } else {
                s.when = 0;
                s.heapId = -1;
            }
            if (s.chan != null) {
                // Chan timers send under the lock (gc's sendLock): stop/reset either precede the
                // send and drain nothing, or follow it and drain the value. The send (sendTime's
                // non-blocking select) runs with preemption off, as under gc's runtime lock: a
                // quantum park here would freeze the goroutine holding this monitor (BUG-0054).
                s.sched.disablePreemption();
                try {
                    call(s, seq, delta);
                } finally {
                    s.sched.enablePreemption();
                }
                return;
            }
        }
        call(s, seq, delta);
    }

    private static void call(State s, long seq, long delta) {
        s.f.target().call((com.oracle.truffle.api.nodes.Node) null, s.f.env(), s.arg, seq, delta);
    }

    /** Removes a buffered value from the timer channel; true if there was one. */
    private static boolean drain(State s) {
        if (s.chan == null) {
            return false;
        }
        ChanOps.SelectCase[] cases = {
            new ChanOps.SelectCase((ChanOps.Chan) s.chan, false, null), new ChanOps.SelectCase(null, false, null)
        };
        return ChanOps.selectGo(cases) == 0;
    }

    /** gc's {@code timer.stop}/{@code modify} common part (lock held): disarm; was it pending? */
    private static boolean disarm(State s) {
        boolean pending = s.when > 0;
        if (s.heapId >= 0) {
            s.sched.timerHeap().cancel(s.heapId);
            s.heapId = -1;
        }
        s.when = 0;
        s.seq++;
        return pending;
    }

    /**
     * {@code time.newTimer}: a {@code *Timer} whose {@code initTimer} is set, armed for
     * {@code when} (gc's {@code newTimer} + {@code modify}).
     */
    @TruffleBoundary
    static Object time_newTimer(Node node, long when, long period, Object f, Object arg, Object cp) {
        GoProcess process = GoProcess.get(node);
        GoType timerType = process.types.lookup("time.Timer");
        if (timerType == null) {
            throw new IllegalStateException("time.newTimer: the bundle has no type time.Timer");
        }
        Flat t = timerType.zeroFlat();
        // initTimer: the field after C (gc offset 8).
        Unsafe.storeScalar(node, t, 8, process.types.basic(GoType.BOOL), 1);
        State s = new State(process, SchedulerCore.current(), (Closure) f, arg, cp);
        process.timers.put(t, s);
        synchronized (s) {
            s.when = when;
            s.period = period;
            if (when > 0) {
                arm(s);
            }
        }
        return t;
    }

    private static State state(Node node, Object t) {
        if (t == null) {
            throw graalgo.go.runtime.Panics.nilDeref(node);
        }
        if (!(GoProcess.get(node).timers.get(Unsafe.referent(t)) instanceof State s)) {
            throw new IllegalStateException("time: a Timer that newTimer did not make");
        }
        return s;
    }

    @TruffleBoundary
    static boolean time_stopTimer(Node node, Object t) {
        State s = state(node, t);
        boolean pending;
        synchronized (s) {
            pending = disarm(s);
        }
        if (drain(s)) {
            pending = true; // gc: a value the program has not received counts as pending
        }
        return pending;
    }

    @TruffleBoundary
    static boolean time_resetTimer(Node node, Object t, long when, long period) {
        State s = state(node, t);
        boolean pending;
        synchronized (s) {
            pending = disarm(s);
            s.period = period;
        }
        if (drain(s)) {
            pending = true;
        }
        synchronized (s) {
            s.when = when;
            if (when > 0) {
                arm(s);
            }
        }
        return pending;
    }
}
