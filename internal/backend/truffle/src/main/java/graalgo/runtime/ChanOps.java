package graalgo.runtime;

/**
 * Compile-time channel/select intrinsic surface (WS G, GG-0020; bridge B-8,
 * inventoried in {@code docs/arch/bridge-inventory.md}).
 *
 * <p>This class exists so the truffle module compiles against the channel
 * surface despite the truffle&#8596;runtime compile cycle (the runtime module
 * compiles the joint round first; the truffle module then compiles this
 * same-FQN class against it). The real channel/select implementation lives
 * in the runtime module ({@code graalgo.runtime.ChanOps} in
 * {@code internal/backend/runtime}). At launch,
 * {@code internal/backend/graalgo-run} puts {@code runtime/out/classes}
 * <em>before</em> {@code truffle/out/classes} on the classpath, so the real
 * implementation shadows this class — this stub is only ever loaded if the
 * launcher classpath order is violated (a launch bug). Every method throws
 * {@link UnsupportedOperationException}.
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
