package graalgo.runtime;

/**
 * Frozen netpoller API contract (WS G, GG-0020, STUB).
 *
 * <p>Phase 2/3 own the network poller; these signatures freeze the
 * contract now so later phases build against it. Every method throws
 * {@link UnsupportedOperationException} with a Phase-2/3 message until
 * implemented. File descriptors are plain ints; the poller descriptor
 * is an opaque long handle.
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
