package graalgo.runtime;

/**
 * Compile-time netpoller API contract (WS G, GG-0020; bridge B-8, inventoried
 * in {@code docs/arch/bridge-inventory.md}).
 *
 * <p>This class exists so the truffle module compiles against the netpoller
 * surface despite the truffle&#8596;runtime compile cycle (the runtime module
 * compiles the joint round first; the truffle module then compiles this
 * same-FQN class against it). The real netpoller lives in the runtime module
 * ({@code graalgo.runtime.Netpoll} in {@code internal/backend/runtime}).
 * At launch, {@code internal/backend/graalgo-run} puts
 * {@code runtime/out/classes} <em>before</em> {@code truffle/out/classes} on
 * the classpath, so the real implementation shadows this class — this stub
 * is only ever loaded if the launcher classpath order is violated (a launch
 * bug). Every method throws {@link UnsupportedOperationException}; file
 * descriptors are plain ints and the poller descriptor is an opaque long
 * handle.
 */
public final class Netpoll {
    private Netpoll() {}

    /** STUB — initialize the network poller. */
    public static void init() {
        throw new UnsupportedOperationException(
                "Phase 2/3: the network poller is not implemented in Phase 1 (GG-0020)");
    }

    /** STUB — register {@code fd} for mode {@code mode} ('r'/'w'). */
    public static long open(int fd, int mode) {
        throw new UnsupportedOperationException(
                "Phase 2/3: the network poller is not implemented in Phase 1 (GG-0020)");
    }

    /** STUB — unregister {@code fd}. */
    public static void close(int fd) {
        throw new UnsupportedOperationException(
                "Phase 2/3: the network poller is not implemented in Phase 1 (GG-0020)");
    }

    /**
     * STUB — wait for I/O readiness; returns true when events were
     * delivered. {@code block} selects blocking vs polling.
     */
    public static boolean waitForEvents(boolean block) {
        throw new UnsupportedOperationException(
                "Phase 2/3: the network poller is not implemented in Phase 1 (GG-0020)");
    }

    /** STUB — wake a thread blocked in {@link #waitForEvents}. */
    public static void breakWait() {
        throw new UnsupportedOperationException(
                "Phase 2/3: the network poller is not implemented in Phase 1 (GG-0020)");
    }
}
