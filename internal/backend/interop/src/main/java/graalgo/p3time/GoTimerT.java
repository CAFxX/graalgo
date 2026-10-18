package graalgo.p3time;

import java.nio.charset.StandardCharsets;
import java.util.concurrent.atomic.AtomicBoolean;

import graalgo.runtime.ChanOps;
import graalgo.runtime.GoChan;
import graalgo.runtime.GoTimer;
import graalgo.runtime.Panic;
import graalgo.runtime.Scheduler;
import graalgo.runtime.TimerChan;
import graalgo.sched.SchedulerCore;
import graalgo.values.GoFunc;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Opaque {@code *time.Timer} Go value (P3-M5, WS E, ADR-0005 D1): wraps a
 * Phase-2 {@link GoTimer} (one-shot) plus its fire-value translator.
 *
 * <p>Scheduling, Stop/Reset truth, ticker re-arm, and drop semantics are
 * the Phase-2 timer heap's verbatim (GG-SCHED-0004, verified
 * differentially vs gc 1.27.1): this class only bridges the value domain.
 * The heap delivers the fire nanotime as a {@link Long} on the timer's
 * own {@link TimerChan}; Go code must observe {@code time.Time} structs
 * on {@code t.C} / {@code time.After} channels, so a translator
 * goroutine pumps {@link Long} → {@code Time{unixnano}} from the heap
 * channel to the user-visible {@link TimeChan}:
 * <ul>
 *   <li>Delivery is non-blocking ({@code select} send-case +
 *       {@code default}): a receiver or buffer space takes the value;
 *       otherwise the tick drops — gc ticker parity for slow receivers.
 *       For one-shots the buffer is always free at fire time.
 *   <li>Stop/Reset drain the user channel through the same non-blocking
 *       select, so a fired-undrained value counts as active (Go 1.23+
 *       {@code timerchandrain} parity) — the heap drains its own channel,
 *       this class drains the translated one, and the return value is the
 *       OR.
 *   <li>The translator parks in {@code select} on {heap channel, done
 *       channel}, scheduler-aware. {@link #stop()} and program-end
 *       shutdown close {@code done}; the generation protocol lets a
 *       {@link #reset} re-arming reuse the parked translator instead of
 *       spawning a second one.
 * </ul>
 *
 * <p>{@code AfterFunc} rides the same machinery: on fire the translator
 * additionally spawns the func value as a new goroutine through
 * {@link Scheduler#spawn} — the exact path {@code go} statements use.
 * The {@code fCommitted} CAS gives AfterFunc Stop its gc meaning
 * ("stopped before f started"): whichever of stop/fire wins the CAS
 * decides whether f runs.
 *
 * <p>Racy-window note: the translator moves the fire value outside the
 * heap lock, so a Stop/Reset landing between the heap's injection and the
 * translator's delivery may observe either side of the race. Delivery and
 * stop/reset's drain are mutually exclusive under the translator lock,
 * narrowing the window to that single handoff; gc documents the same
 * race for concurrent Stop/Reset.
 *
 * <p>Copying copies the reference (the ADR-0002 {@code sync.Mutex}
 * pattern): this is not a {@code GoStruct}, so assignment, argument
 * passing, and interface boxing carry the reference.
 *
 * <p>verifies: GG-0017 (time slice)
 */
public class GoTimerT {
    private final GoTimer timer;
    /** The heap's channel: {@link Long} fire values. Never exposed to Go code. */
    private final TimerChan internal;
    /** The user-visible channel: {@code time.Time} values, {@link TimeChan} flavor. */
    private final TimeChan user;
    private final boolean ticker;
    /** Non-null only for {@code AfterFunc} timers. */
    private final GoFunc afterFunc;
    private final GoStructDescriptor timeDesc;
    private final SchedulerCore sched;
    /**
     * P1-27: the (wall, monotonic) clock base pair for converting the
     * heap's monotonic fire nanotime to wall time (see
     * {@link #fireNanosToWallNanos}). Captured in the constructor — the
     * GoTimer is already armed then, so every future fire postdates
     * {@code monoBaseNanos}.
     */
    private final long wallBaseMillis;
    private final long monoBaseNanos;

    // ---- translator lifecycle (all under lock) ----
    private final Object lock = new Object();
    private long generation;
    /** False after stop()/shutdown: the heap timer is disarmed. */
    private boolean armed = true;
    /**
     * Closed to wake the parked translator (stop/reset/shutdown).
     * Replaced (after closing) only by reset(), so a translator parked in
     * selectGo is always parked on either the current done or a closed
     * one it is about to wake from — never on an open, abandoned done.
     */
    private GoChan done = new GoChan(0, null);
    private final AtomicBoolean doneClosed = new AtomicBoolean(false);
    private boolean translatorAlive;
    /** AfterFunc: set (once per arming) when f's spawn is committed. */
    private final AtomicBoolean fCommitted = new AtomicBoolean(false);

    /**
     * @param timer     the Phase-2 one-shot timer (already armed)
     * @param ticker    true for {@code *time.Ticker} (the translator loops)
     * @param afterFunc non-null for {@code AfterFunc}: spawned on fire
     * @param timeDesc  canonical {@code time.Time} descriptor
     */
    public GoTimerT(GoTimer timer, boolean ticker, GoFunc afterFunc,
                    GoStructDescriptor timeDesc) {
        if (timer == null || timeDesc == null) {
            throw new IllegalArgumentException("GoTimerT needs a timer and a time.Time descriptor");
        }
        if (!(timer.channel() instanceof TimerChan tc)) {
            throw new IllegalStateException(
                    "GoTimerT: phase-2 timer channel is not a TimerChan");
        }
        this.timer = timer;
        this.ticker = ticker;
        this.afterFunc = afterFunc;
        this.timeDesc = timeDesc;
        this.sched = SchedulerCore.current();
        // P1-27: capture the clock base pair before the translator can
        // deliver (mono first, then wall; either order leaves only ~100ns
        // of residual skew, negligible against the ms base granularity).
        this.monoBaseNanos = System.nanoTime();
        this.wallBaseMillis = System.currentTimeMillis();
        this.internal = tc;
        this.user = new TimeChan();
        ensureTranslator();
    }

    /** The user-visible channel ({@code t.C}, {@code After}/{@code Tick} results). */
    public TimeChan userChannel() {
        return user;
    }

    /**
     * {@code (*Timer).Stop} / {@code (*Ticker).Stop}: gc semantics via
     * the Phase-2 heap (pending → true; fired-undrained counts as active
     * and is drained), OR-ed with the translated-channel drain.
     */
    public boolean stop() {
        // AfterFunc: whoever wins the fCommitted CAS decides whether f
        // runs. Stop()->true must imply f never runs (the documented
        // AfterFunc+Stop contract); if the fire already committed f,
        // Stop reports false.
        boolean fCommittedAlready =
                afterFunc != null && !fCommitted.compareAndSet(false, true);
        final boolean heapSays;
        synchronized (lock) {
            if (armed) {
                armed = false;
                generation++;
                // Close (do NOT replace): the parked translator wakes from
                // this very channel. A later reset() installs a fresh one.
                closeDoneLocked();
            }
            // Delivery and this drain are mutually exclusive under lock
            // (see class javadoc): the translator cannot slip a value in
            // between the heap drain and ours.
            heapSays = timer.stop();
            boolean drained = drainUserLocked();
            if (fCommittedAlready) {
                return false;
            }
            return heapSays || drained;
        }
    }

    /**
     * {@code (*Timer).Reset} / {@code (*Ticker).Reset}: re-arms for
     * {@code ns} from now; true iff the timer had been active
     * (fired-undrained counts as active). Non-positive on a ticker panics
     * with gc's message (via {@link GoTimer#reset}).
     */
    public boolean reset(long ns) {
        final boolean was;
        synchronized (lock) {
            // timer.reset first: it may panic (ticker, non-positive); on
            // panic our translator state is untouched.
            was = timer.reset(ns);
            generation++;
            armed = true;
            // Wake any translator parked on the old done, then install a
            // fresh one for the new arming (close-then-replace is atomic
            // under lock, so the translator re-reads a consistent pair).
            closeDoneLocked();
            done = new GoChan(0, null);
            doneClosed.set(false);
            boolean drained = drainUserLocked();
            fCommitted.set(false);
            ensureTranslatorLocked();
            return was || drained;
        }
    }

    /** Program-end shutdown: disarm and wake the translator (idempotent). */
    void shutdownTranslator() {
        synchronized (lock) {
            if (!armed) {
                return;
            }
            armed = false;
            generation++;
            closeDoneLocked();
        }
    }

    /** Test probe: is the translator goroutine currently live? */
    boolean translatorAliveForTest() {
        synchronized (lock) {
            return translatorAlive;
        }
    }

    /**
     * P1-27: converts a monotonic fire nanotime ({@code System.nanoTime}
     * basis, as delivered by the Phase-2 heap on the timer channel) to
     * wall-clock nanoseconds via the creation-time base pair:
     * {@code wall = wallBase + (fire - monoBase)}.
     *
     * <p>Invariant: {@code fireNanos >= monoBaseNanos}. The heap samples
     * {@code System.nanoTime()} at fire time
     * ({@code TimerHeap.fireChanLocked}), strictly after this object's
     * construction captured {@code monoBaseNanos}; {@code nanoTime} is
     * monotonic, so the delta is never negative and no clamp is needed.
     * {@code reset()} re-arms with a fresh heap deadline, likewise
     * postdating the base pair.
     */
    static long fireNanosToWallNanos(long wallBaseMillis, long monoBaseNanos, long fireNanos) {
        return wallBaseMillis * 1_000_000L + (fireNanos - monoBaseNanos);
    }

    /**
     * Test probe (P1-27): delivers a synthetic fire nanotime on the
     * heap channel, bypassing the heap. Must be called on a scheduler
     * goroutine. Lets tests verify the translator honors the fire value
     * deterministically, without timing.
     */
    void injectFireNanosForTest(long fireNanos) {
        internal.send(fireNanos);
    }

    /** Test probe (P1-27): the wall half of the clock base pair. */
    long wallBaseMillisForTest() {
        return wallBaseMillis;
    }

    /** Test probe (P1-27): the monotonic half of the clock base pair. */
    long monoBaseNanosForTest() {
        return monoBaseNanos;
    }

    private void closeDoneLocked() {
        if (doneClosed.compareAndSet(false, true)) {
            done.close();
        }
    }

    private void ensureTranslator() {
        synchronized (lock) {
            ensureTranslatorLocked();
        }
    }

    private void ensureTranslatorLocked() {
        if (translatorAlive) {
            return;
        }
        translatorAlive = true;
        try {
            P3TimeRuntime.spawnTranslator(this, sched, this::translatorLoop);
        } catch (RuntimeException e) {
            translatorAlive = false;
            throw e;
        }
    }

    /**
     * Non-blocking drain of the user channel (monitor-held select with
     * {@code default}); caller holds {@link #lock}. Returns true iff a
     * stale {@code Time} value was discarded.
     */
    private boolean drainUserLocked() {
        boolean drained = false;
        for (;;) {
            int idx = ChanOps.selectGo(new ChanOps.SelectCase[]{
                    new ChanOps.SelectCase(user, false, null),
                    new ChanOps.SelectCase(null, false, null),
            });
            if (idx != 0 || !ChanOps.selectRecvOk()) {
                return drained;
            }
            drained = true;
        }
    }

    private void translatorLoop() {
        try {
            for (;;) {
                final long myGen;
                final GoChan myDone;
                synchronized (lock) {
                    myGen = generation;
                    myDone = done;
                }
                int idx = ChanOps.selectGo(new ChanOps.SelectCase[]{
                        new ChanOps.SelectCase(internal, false, null),
                        new ChanOps.SelectCase(myDone, false, null),
                });
                if (idx == 1) {
                    // done closed: stop() or program-end shutdown — unless a
                    // reset() re-armed meanwhile (generation moved, armed).
                    synchronized (lock) {
                        if (!armed) {
                            translatorAlive = false;
                            return;
                        }
                    }
                    continue;
                }
                if (!ChanOps.selectRecvOk()) {
                    // internal is never closed; a missing ok is an internal bug.
                    throw new IllegalStateException("p3time: timer heap channel closed");
                }
                Object fireVal = ChanOps.selectRecvValue();
                if (!(fireVal instanceof Long)) {
                    throw new IllegalStateException(
                            "p3time: timer heap channel delivered non-Long " + fireVal);
                }
                if (user.isClosed()) {
                    // gc: send on a closed channel panics (even the
                    // runtime's non-blocking timer send).
                    Panic.throwPanic(new GoString(
                            "send on closed channel".getBytes(StandardCharsets.UTF_8)));
                }
                // P1-27: stamp the *heap's fire nanotime* (monotonic), converted
                // to wall time via the creation-time base pair — never the
                // wall clock at delivery time. fireVal is a boxed Long per
                // the check above.
                GoStruct tv = new GoStruct(timeDesc,
                        new long[]{fireNanosToWallNanos(
                                wallBaseMillis, monoBaseNanos, (Long) fireVal), 1},
                        new Object[0]);
                // Non-blocking delivery, mutually exclusive with stop()/
                // reset()'s drain under lock: for one-shots the buffer is
                // always free here; for tickers a slow receiver drops the
                // tick (gc parity).
                boolean runF = false;
                synchronized (lock) {
                    // P1-1 (reviewer2): if reset() intervened between the
                    // claim (above, outside lock) and this delivery, the
                    // generation moved — drop the stale pre-reset fire value
                    // (gc timerchandrain parity) instead of delivering it.
                    // (If stop() intervened, !armed — deliver as today: gc
                    // Stop→false implies the value is in the channel.)
                    if (armed && generation != myGen) {
                        continue;
                    }
                    // AfterFunc: claim f BEFORE delivering, so a concurrent
                    // stop()->true (which claims first) guarantees f never runs.
                    // (Claimed here, after the generation check, so a dropped
                    // stale fire does not consume the one-shot f.)
                    runF = afterFunc != null && fCommitted.compareAndSet(false, true);
                    ChanOps.selectGo(new ChanOps.SelectCase[]{
                            new ChanOps.SelectCase(user, true, tv),
                            new ChanOps.SelectCase(null, false, null),
                    });
                }
                if (runF) {
                    // AfterFunc: f runs in its own goroutine — the exact
                    // path `go` statements use.
                    Scheduler.spawn(afterFunc);
                }
                synchronized (lock) {
                    if (!armed) {
                        translatorAlive = false;
                        return;
                    }
                    if (!ticker && generation == myGen) {
                        // One-shot fired with no intervening reset.
                        translatorAlive = false;
                        return;
                    }
                    // Ticker, or a one-shot reset during delivery: adopt
                    // the new generation and keep pumping.
                }
            }
        } finally {
            synchronized (lock) {
                translatorAlive = false;
            }
            P3TimeRuntime.deregister(this);
        }
    }
}
