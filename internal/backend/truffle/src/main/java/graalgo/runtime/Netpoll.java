package graalgo.runtime;

/**
 * Netpoller contract — DELETED in v1 (P2-M3, WS C). The superseding
 * requirement GG-0063 replaces this stub with a runtime epoll poller over
 * raw fds (toolexec-design W8); until then v1 has no readiness poller.
 *
 * <p>Signatures are the frozen ones ({@code FreezeGapTest}); every
 * method throws documenting the deletion rather than "not
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
 * <p>v1 falsifier (P2-M3): any reachable netpoller code path, or any
 * NIO bypassing the slot discipline, fails {@code make check}'s
 * netpoller-deletion gate.
 */
public final class Netpoll {
    private Netpoll() {}

    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    private static UnsupportedOperationException deleted(String what) {
        return new UnsupportedOperationException(
                "v1 has no netpoller (P2-M3; replaced by the GG-0063 runtime poller in W8): " + what
                        + " does not exist; blocking I/O uses "
                        + "SchedulerCore.ioEnter/ioExit on virtual threads");
    }

    /** DELETED — v1 has no network poller (GG-0063 replaces this stub). */
    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    public static void init() {
        throw deleted("init");
    }

    /** DELETED — v1 has no network poller (GG-0063 replaces this stub). */
    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    public static long open(int fd, int mode) {
        throw deleted("open");
    }

    /** DELETED — v1 has no network poller (GG-0063 replaces this stub). */
    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    public static void close(int fd) {
        throw deleted("close");
    }

    /** DELETED — v1 has no network poller (GG-0063 replaces this stub). */
    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    public static boolean waitForEvents(boolean block) {
        throw deleted("waitForEvents");
    }

    /** DELETED — v1 has no network poller (GG-0063 replaces this stub). */
    @com.oracle.truffle.api.CompilerDirectives.TruffleBoundary
    public static void breakWait() {
        throw deleted("breakWait");
    }
}
