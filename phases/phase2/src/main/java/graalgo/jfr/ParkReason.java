package graalgo.jfr;

/**
 * Why a goroutine parked (P2-M6, WS F; GG-0008 JFR slice).
 *
 * <p>Recorded on {@code graalgo.GoroutinePark}; the reason is fixed at
 * park time by the parking site:
 * <ul>
 *   <li>{@code CHANNEL} — blocked channel send/recv ({@code GoChan}).
 *   <li>{@code TIMER} — {@code Timers.sleep}.
 *   <li>{@code SELECT} — blocked {@code select} ({@code SelectOp}).
 *   <li>{@code QUANTUM} — quantum expiry or voluntary yield
 *       ({@code SchedulerCore.quantumPark}; a yield uses the same
 *       mechanism).
 *   <li>{@code IO} — blocking I/O slot release ({@code ioEnter}/{@code ioExit}).
 *   <li>{@code UNKNOWN} — generic {@code parkCurrent} callers (tests).
 * </ul>
 */
public enum ParkReason {
    CHANNEL,
    TIMER,
    SELECT,
    QUANTUM,
    IO,
    UNKNOWN
}
