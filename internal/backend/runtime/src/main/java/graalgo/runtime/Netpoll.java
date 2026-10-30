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
 * <p>The bracket lives at the interop choke points, via the off-scheduler
 * no-op pair {@code SchedulerCore.ioBracketEnter()} /
 * {@code ioBracketExit()} (review-pool-io, R1-24): {@code GoOs.read} /
 * {@code write} / {@code readAt} / {@code writeAt} / {@code sync}
 * (skipping nonblocking {@code SelectableChannel}s, which never block),
 * {@code GoHttp.clientDo}'s {@code HttpClient.send}, and
 * {@code GoToolchain.resolveGoroot}'s subprocess wait. Network sockets
 * are nonblocking and wait through {@code GoNetpoll.pollWait}, which
 * parks via the scheduler instead of blocking — they need no bracket,
 * and bracketing them would pay a scheduler round-trip per read/write.
 * Unbracketed sites remaining (see the lane report): {@code GoOs.open}
 * (a FIFO open can block until a peer opens the other end; regular
 * path opens do not block indefinitely), {@code GoNetpoll.pollOpen}'s
 * brief selector-registration handoff (its unbounded-wait gap is a
 * separately tracked P2), and metadata syscalls (stat/mkdir/rename),
 * which do not block indefinitely in practice. There is no guest
 * {@code os/exec} path to bracket: {@code os.StartProcess} fail-fasts
 * in the current synthetic {@code os} scope.
 *
 * <p>Falsifier (GG-0018): any reachable netpoller code path, or any
 * NIO bypassing the slot discipline, fails {@code make check}'s
 * netpoller-deletion gate.
 *
 * <p>verifies: GG-0018
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
