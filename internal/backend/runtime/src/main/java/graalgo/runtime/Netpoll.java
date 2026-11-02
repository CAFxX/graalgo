package graalgo.runtime;

/**
 * Netpoller contract — DELETED surface, frozen (P2-M3, WS C; GG-0018).
 *
 * <p><strong>Phase-2 replacement</strong> (P2-D8) for the Phase-1 stub
 * of the same fully-qualified name. Signatures are byte-identical to
 * the frozen ones ({@code FreezeSignatureTest} keeps passing); every
 * method now throws documenting the deletion rather than "not
 * implemented".
 *
 * <p><strong>Superseded as a description of the live system (review
 * 2026-11-02, R08).</strong> The GG-0018 deletion claim below no
 * longer holds: P3-M9 re-integrated a real netpoller as
 * {@code graalgo.p3net.GoNetpoll} (interop module), implementing the
 * {@code internal/poll.runtime_poll*} linknames that real guest
 * {@code net} code uses. This class is NOT that poller: it is the
 * frozen Phase-2 surface of the same fully-qualified name, kept
 * throwing so that a wrong-classpath load fails loudly instead of
 * silently running a second poller model, and so the signature-freeze
 * tests keep passing. It has no production callers. The live polling
 * model is documented on {@code GoNetpoll} and in
 * {@code docs/arch/SPEC.md} §3.5. Do not un-delete this surface
 * (two live pollers would be worse than one dead surface) and do
 * not delete {@code GoNetpoll} to re-assert GG-0018 — the P3-M9
 * decision, with its GMP/drainer integration and differentials,
 * is the standing model.
 *
 * <p>Phase-2 history (no longer the live model): blocking I/O ran
 * directly on virtual threads, with the calling goroutine bracketing
 * the blocking call with {@code SchedulerCore.ioEnter()} /
 * {@code ioExit()}, which releases and re-acquires the in-flight
 * scheduler slot (gc netpoller parity for GOMAXPROCS accounting — a
 * goroutine blocked in I/O does not count against the cap). Loom
 * unmounts the vthread on the blocking call, freeing the carrier.
 *
 * <p>Falsifier (GG-0018, historical): any reachable netpoller code
 * path, or any NIO bypassing the slot discipline, fails
 * {@code make check}'s netpoller-deletion gate. The gate itself was
 * updated in the P3-M9 era to guard only code references inside the
 * runtime module, since the premise no longer holds for the interop
 * poller.
 *
 * <p>verifies: GG-0018 (Phase-2 history; live path superseded by P3-M9)
 */
public final class Netpoll {
    private Netpoll() {}

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
