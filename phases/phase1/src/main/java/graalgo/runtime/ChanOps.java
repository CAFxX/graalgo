package graalgo.runtime;

/**
 * Frozen channel/select intrinsic surface (WS G, GG-0020, STUB).
 *
 * <p>Channels are Phase 2 (no goroutines in Phase 1); these signatures
 * freeze the contract now. Every method throws
 * {@link UnsupportedOperationException} with a Phase-2 message until
 * implemented. The {@code len} builtin on channels is a factory scope
 * error naming Phase 2 (it type-checks but has no Phase-1 lowering).
 */
public final class ChanOps {
    private ChanOps() {}

    /** Opaque channel handle (Phase 2 fills in the representation). */
    public interface Chan {}

    /** STUB — {@code make(chan T, n)}. Phase 2. */
    public static Chan makeChan(long elemSizeBytes, int capacity) {
        throw new UnsupportedOperationException(
                "Phase 2: channels need the scheduler (GG-0020)");
    }

    /** STUB — {@code ch <- v} (send). Phase 2. */
    public static void send(Chan ch, Object value) {
        throw new UnsupportedOperationException(
                "Phase 2: channels need the scheduler (GG-0020)");
    }

    /** STUB — {@code v = <-ch} (receive). Phase 2. */
    public static Object recv(Chan ch) {
        throw new UnsupportedOperationException(
                "Phase 2: channels need the scheduler (GG-0020)");
    }

    /** STUB — {@code close(ch)}. Phase 2. */
    public static void close(Chan ch) {
        throw new UnsupportedOperationException(
                "Phase 2: channels need the scheduler (GG-0020)");
    }

    /** STUB — {@code len(ch)}. Phase 2. */
    public static int len(Chan ch) {
        throw new UnsupportedOperationException(
                "Phase 2: channels need the scheduler (GG-0020)");
    }

    /** STUB — {@code cap(ch)}. Phase 2. */
    public static int cap(Chan ch) {
        throw new UnsupportedOperationException(
                "Phase 2: channels need the scheduler (GG-0020)");
    }

    /** STUB — {@code select} over cases. Phase 2. */
    public static int selectGo(SelectCase[] cases) {
        throw new UnsupportedOperationException(
                "Phase 2: select needs the scheduler (GG-0020)");
    }

    /** One select case (channel + direction); interpreted by Phase 2. */
    public record SelectCase(Chan chan, boolean send, Object sendValue) {}
}
