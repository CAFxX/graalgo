package graalgo.runtime;

/**
 * Netpoller contract — DELETED (P2-M3, WS C; GG-0018).
 *
 * <p><strong>Phase-2 replacement</strong> (P2-D8) for the Phase-1 stub
 * of the same fully-qualified name. Signatures are byte-identical to
 * the frozen ones ({@code FreezeSignatureTest} keeps passing); every
 * method now throws documenting the deletion rather than "not
 * implemented".
 *
 * <p>There is no readiness-based network poller in graalgo. Blocking
 * I/O runs directly on virtual threads: the calling goroutine brackets
 * the blocking call with {@code SchedulerCore.ioEnter()} /
 * {@code ioExit()}, which releases and re-acquires the in-flight
 * scheduler slot (gc netpoller parity for GOMAXPROCS accounting — a
 * goroutine blocked in I/O does not count against the cap). Loom
 * unmounts the vthread on the blocking call, freeing the carrier.
 *
 * <p>Falsifier (GG-0018): any reachable netpoller code path, or any
 * NIO bypassing the slot discipline, fails {@code make check}'s
 * netpoller-deletion gate.
 *
 * <p>verifies: GG-0018
 */
public final class Netpoll {
    private Netpoll() {}

    /**
     * Phase-2-only diagnostic: true when THIS class (the Phase-2
     * replacement, documenting the netpoller deletion) is loaded,
     * absent on the Phase-1 stub. The launcher calls it at startup
     * (R-02 overlay check) to fail fast if the classpath order is
     * wrong. NOT part of the frozen contract — intentionally not
     * covered by FreezeSignatureTest.
     */
    public static boolean isPhase2() {
        return true;
    }

    private static UnsupportedOperationException deleted(String what) {
        return new UnsupportedOperationException(
                "netpoller deleted per GG-0018 (P2-M3): " + what
                        + " does not exist; blocking I/O uses "
                        + "SchedulerCore.ioEnter/ioExit on virtual threads");
    }

    /** DELETED — the network poller does not exist (GG-0018). */
    public static void init() {
        throw deleted("init");
    }

    /** DELETED — the network poller does not exist (GG-0018). */
    public static long open(int fd, int mode) {
        throw deleted("open");
    }

    /** DELETED — the network poller does not exist (GG-0018). */
    public static void close(int fd) {
        throw deleted("close");
    }

    /** DELETED — the network poller does not exist (GG-0018). */
    public static boolean waitForEvents(boolean block) {
        throw deleted("waitForEvents");
    }

    /** DELETED — the network poller does not exist (GG-0018). */
    public static void breakWait() {
        throw deleted("breakWait");
    }
}
