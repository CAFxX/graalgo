package graalgo.runtime;

import graalgo.sched.SchedulerCore;

/**
 * Channel/select intrinsic surface (P2-M2, WS B; GG-0017 chan slice).
 *
 * <p>Phase-2 same-FQN replacement for the Phase-1 stub
 * (overlay mechanism, P2-D8): every FROZEN signature is byte-identical
 * to {@code internal/backend/truffle/src/main/java/graalgo/runtime/ChanOps.java} — only the
 * method bodies are real now (bodies are INTERNAL per freeze §3.5, so no
 * ADR/version bump). {@code FreezeSignatureTest} keeps passing.
 *
 * <p>All blocking operations park scheduler-aware via
 * {@link SchedulerCore#parkCurrent} — a blocked send/recv/select never
 * pins a carrier thread (P2-D4).
 *
 * <p>Conventions for the future exporter (documented here because the
 * frozen {@link SelectCase} record cannot carry them):
 * <ul>
 *   <li>{@code default:} is a {@code SelectCase} with {@code chan == null}.
 *   <li>After {@link #selectGo} returns the chosen index, a chosen recv
 *       case's value/ok are read via {@link #selectRecvValue} /
 *       {@link #selectRecvOk} (per-thread stash, set by the select).
 * </ul>
 */
public final class ChanOps {
    private ChanOps() {}

    /** Opaque channel handle (frozen type). */
    public interface Chan {}

    /**
     * {@code make(chan T, n)} (frozen signature). The element zero value
     * defaults to {@code null}; prefer
     * {@link #makeChan(long, int, Object)} with the type-directed zero
     * when the program can observe a closed-channel receive.
     */
    public static Chan makeChan(long elemSizeBytes, int capacity) {
        return makeChan(elemSizeBytes, capacity, null);
    }

    /**
     * {@code make(chan T, n)} with an explicit element zero value
     * (additive; Phase 2). The exporter should pass the type-directed
     * zero so {@code <-ch} on a closed, drained channel observes Go's
     * zero value.
     */
    public static Chan makeChan(long elemSizeBytes, int capacity, Object zeroValue) {
        if (capacity < 0) {
            Panic.throwPanic(GoStrings.of("makechan: negative capacity"));
        }
        return new GoChan(capacity, zeroValue);
    }

    /** {@code ch <- v} (frozen signature). */
    public static void send(Chan ch, Object value) {
        requireGoChan(ch).send(value);
    }

    /**
     * {@code v = <-ch} (frozen signature). Returns the element zero
     * value once the channel is closed and drained (Go's
     * {@code (zero, false)}; the ok bit is not observable through this
     * signature — the two-value receive form is a P2-M3 exporter item).
     */
    public static Object recv(Chan ch) {
        return requireGoChan(ch).recv().value();
    }

    /**
     * Two-value receive {@code v, ok = <-ch} (additive; Phase 2). The
     * future exporter lowers the comma-ok form to this.
     */
    public static GoChan.RecvResult recvOk(Chan ch) {
        return requireGoChan(ch).recv();
    }

    /** {@code close(ch)} (frozen signature). */
    public static void close(Chan ch) {
        requireGoChan(ch).close();
    }

    /** {@code len(ch)}: buffered element count (frozen signature). */
    public static int len(Chan ch) {
        return requireGoChan(ch).buffered();
    }

    /** {@code cap(ch)}: buffer capacity (frozen signature). */
    public static int cap(Chan ch) {
        return requireGoChan(ch).capacity();
    }

    /**
     * {@code select} over cases (frozen signature). Returns the chosen
     * case index. A {@code SelectCase} with {@code chan == null}
     * denotes {@code default:}.
     */
    public static int selectGo(SelectCase[] cases) {
        if (cases == null) {
            throw new IllegalArgumentException("selectGo: null cases");
        }
        SelectOp op = new SelectOp(cases);
        int idx = op.execute();
        SELECT_RESULT.set(new SelectResult(op.resultValue(), op.resultOk()));
        return idx;
    }

    /** Value received by the last {@link #selectGo} on this thread. */
    public static Object selectRecvValue() {
        SelectResult r = SELECT_RESULT.get();
        if (r == null) {
            throw new IllegalStateException("selectRecvValue with no preceding selectGo");
        }
        return r.value;
    }

    /** Ok bit received by the last {@link #selectGo} on this thread. */
    public static boolean selectRecvOk() {
        SelectResult r = SELECT_RESULT.get();
        if (r == null) {
            throw new IllegalStateException("selectRecvOk with no preceding selectGo");
        }
        return r.ok;
    }

    private record SelectResult(Object value, boolean ok) {}

    private static final ThreadLocal<SelectResult> SELECT_RESULT = new ThreadLocal<>();

    /** One select case (channel + direction); frozen record. */
    public record SelectCase(Chan chan, boolean send, Object sendValue) {}

    private static GoChan requireGoChan(Chan ch) {
        if (!(ch instanceof GoChan g)) {
            throw new IllegalArgumentException(
                    "channel operation on non-GoChan: "
                            + (ch == null ? "null" : ch.getClass().getName()));
        }
        return g;
    }
}
