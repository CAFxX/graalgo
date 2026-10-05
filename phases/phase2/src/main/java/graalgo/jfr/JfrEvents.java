package graalgo.jfr;

import graalgo.sched.GoroutineHandle;

/**
 * Central JFR emission point for scheduler observability (P2-M6, WS F;
 * GG-0008 JFR slice).
 *
 * <p>Overhead contract: when no recording has our events enabled, the
 * park path costs tens of nanoseconds (two volatile field accesses and
 * one {@code Event.isEnabled()} at ~8ns — measured on GraalVM 25.0.4.1.1,
 * not the ~600ns an early flawed probe reported; the caching layer built
 * on that number was removed in review) — no allocation, no
 * {@code System.nanoTime()}, no JFR call. Start/end/block emissions check
 * the live JFR state directly — they are not hot-path.
 *
 * <p>Rate contract: {@code graalgo.GoroutinePark} is committed for
 * non-quantum parks lasting at least {@link #PARK_THRESHOLD_NANOS}, and
 * for 1-in-1024 sampled quantum parks (the hot loop — a duration
 * threshold cannot filter it: measured 26% of quantum parks exceed
 * 100µs). {@code graalgo.ChannelBlock} fires only when an operation
 * actually blocks; non-blocking fast-path channel ops emit nothing.
 *
 * <p>Opt-in: all graalgo events are {@code @Enabled(false)} — a bare
 * {@code new Recording()} does <em>not</em> capture them (JFR would
 * otherwise enable custom events by default for any active recording).
 * Enable explicitly: {@code rec.enable("graalgo.GoroutineStart")} etc.
 * For a live JVM: {@code jcmd <pid> JFR.start name=graalgo
 * '+graalgo.GoroutineStart#enabled=true' '+graalgo.GoroutineEnd#enabled=true'
 * '+graalgo.GoroutinePark#enabled=true' '+graalgo.ChannelBlock#enabled=true'}.
 */
public final class JfrEvents {
    private JfrEvents() {}

    /**
     * Parks shorter than this are not committed as
     * {@code graalgo.GoroutinePark} events. Default 100&nbsp;µs:
     * quantum-park round-trips measure single-digit microseconds, so the
     * scheduler hot loop stays silent, while channel/timer/select blocks
     * (milliseconds) are recorded. Override with
     * {@code -Dgraalgo.jfr.parkThresholdNanos=<nanos>}.
     */
    public static final long PARK_THRESHOLD_NANOS =
            Long.getLong("graalgo.jfr.parkThresholdNanos", 100_000L);

    /**
     * Emitted-but-dropped count (observability must never break the
     * scheduler: emission failures are swallowed and counted, never
     * propagated). Test-visible.
     */
    private static final java.util.concurrent.atomic.AtomicLong droppedEmits =
            new java.util.concurrent.atomic.AtomicLong();

    /** Number of swallowed emission failures since VM start. */
    public static long droppedEmits() {
        return droppedEmits.get();
    }

    /**
     * Runs an emission, swallowing failures. JFR commit is designed not
     * to throw, but these call sites are scheduler-critical (awaitAck,
     * spawn finally, channel ops): a throwing observability hook must
     * never break scheduling or mask the goroutine's real exception.
     */
    private static void emitQuietly(Runnable emit) {
        try {
            emit.run();
        } catch (RuntimeException e) {
            droppedEmits.incrementAndGet();
        }
    }

    /**
     * Force-loads the event classes so {@code Recording.enable(name)}
     * resolves them. Referencing the class literals triggers loading,
     * which registers the events with JFR.
     */
    public static void ensureRegistered() {
        Class<?>[] events = {
            GoroutineStartEvent.class,
            GoroutineEndEvent.class,
            GoroutineParkEvent.class,
            ChannelBlockEvent.class,
        };
        if (events.length != 4) {
            throw new AssertionError("event registration list changed");
        }
    }

    /** True if any graalgo event is enabled in any active recording. */
    public static boolean anyEnabled() {
        return GoroutineStartEvent.enabled()
                || GoroutineEndEvent.enabled()
                || GoroutineParkEvent.enabled()
                || ChannelBlockEvent.enabled();
    }

    /** Goroutine started executing (call after first admission). */
    public static void goroutineStart(GoroutineHandle h) {
        if (GoroutineStartEvent.enabled()) {
            Thread t = h.thread;
            String name = t == null ? "" : t.getName();
            emitQuietly(() -> GoroutineStartEvent.emit(h.id, name));
        }
    }

    /** Goroutine finished (call in the vthread finally block). */
    public static void goroutineEnd(GoroutineHandle h, long lifetimeNanos) {
        if (GoroutineEndEvent.enabled()) {
            emitQuietly(() -> GoroutineEndEvent.emit(h.id, lifetimeNanos));
        }
    }

    /**
     * Quantum-park sampling (P2-M6 rate control): quantum parks are the
     * scheduler hot loop (tens of microseconds each — a 100µs duration
     * threshold does NOT filter them; measured 26% exceed it). Committing
     * every one would drown the recording, so 1-in-1024 is sampled. The
     * sampled events carry true durations, so the distribution stays
     * observable. Non-quantum parks use the duration threshold instead.
     */
    private static final long QUANTUM_SAMPLE_MASK = 1023L;

    /**
     * Records that {@code h} is parking for {@code reason}. Call before
     * the park becomes visible (same place as {@code armPark}).
     * The start timestamp is only taken when a recording might want the
     * event, keeping the disabled path to a few nanoseconds.
     */
    public static void notePark(GoroutineHandle h, ParkReason reason) {
        h.parkReason = reason;
        h.parkStartNanos = GoroutineParkEvent.enabled() ? System.nanoTime() : 0L;
    }

    /**
     * Called when a park's {@code awaitAck} returns. Commits
     * {@code graalgo.GoroutinePark} for non-quantum parks lasting at
     * least {@link #PARK_THRESHOLD_NANOS}, and for 1-in-1024 quantum
     * parks (sampled). Consumes the parked reason (idempotent per park).
     * A zero start timestamp means the park began while disabled —
     * duration unknown, nothing committed.
     */
    public static void parkResumed(GoroutineHandle h) {
        ParkReason reason = h.parkReason;
        if (reason == null) {
            return;
        }
        h.parkReason = null;
        long start = h.parkStartNanos;
        if (start == 0L || !GoroutineParkEvent.enabled()) {
            return;
        }
        long durationNanos = System.nanoTime() - start;
        if (reason == ParkReason.QUANTUM) {
            if ((h.parkSeq.get() & QUANTUM_SAMPLE_MASK) != 0) {
                return;
            }
        } else if (durationNanos < PARK_THRESHOLD_NANOS) {
            return;
        }
        emitQuietly(() -> GoroutineParkEvent.emit(h.id, reason, durationNanos));
    }

    /** A channel operation blocked (call before parking). */
    public static void channelBlock(GoroutineHandle h, long channelId, ChannelOp op) {
        if (ChannelBlockEvent.enabled()) {
            emitQuietly(() -> ChannelBlockEvent.emit(h.id, channelId, op));
        }
    }

    /**
     * A blocking-I/O interval finished (call after {@code ioExit}'s
     * re-admission). The full syscall duration is reported, unlike the
     * microsecond slot-release handshake which {@link #parkResumed}
     * filters.
     */
    public static void ioBlocked(GoroutineHandle h, long ioNanos) {
        if (GoroutineParkEvent.enabled() && ioNanos >= PARK_THRESHOLD_NANOS) {
            emitQuietly(() -> GoroutineParkEvent.emit(h.id, ParkReason.IO, ioNanos));
        }
    }
}
