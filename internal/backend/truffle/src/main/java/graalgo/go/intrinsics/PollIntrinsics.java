package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.syscall.Poller;

/**
 * What gc's runtime provides to {@code internal/poll} by {@code //go:linkname}: the semaphores
 * of {@code fdMutex} (runtime/sema.go) and the runtime poller (runtime/netpoll.go), here
 * {@link Poller} (GG-0063); registered in {@code internal/toolexec/intrinsics/registry.txt}.
 * A poll descriptor ({@code uintptr}) is the poller's id of the registration.
 */
final class PollIntrinsics {
    private PollIntrinsics() {}

    static void internal_poll_runtime_Semacquire(Node node, Object addr) {
        SyncIntrinsics.sync_runtime_Semacquire(node, addr);
    }

    static void internal_poll_runtime_Semrelease(Node node, Object addr) {
        SyncIntrinsics.sync_runtime_Semrelease(node, addr, false, 0);
    }

    @TruffleBoundary
    static void internal_poll_runtime_pollServerInit(Node node) {
        Poller.init();
    }

    /** {@code runtime_pollOpen(fd) (pd uintptr, errno int)}. */
    @TruffleBoundary
    static Object internal_poll_runtime_pollOpen(Node node, long fd) {
        long[] r = Poller.open(fd);
        return new Object[] {r[0], r[1]};
    }

    @TruffleBoundary
    static void internal_poll_runtime_pollClose(Node node, long pd) {
        Poller.close(pd);
    }

    @TruffleBoundary
    static long internal_poll_runtime_pollReset(Node node, long pd, long mode) {
        return Poller.reset(pd, mode);
    }

    @TruffleBoundary
    static long internal_poll_runtime_pollWait(Node node, long pd, long mode) {
        return Poller.await(pd, mode, false);
    }

    @TruffleBoundary
    static void internal_poll_runtime_pollWaitCanceled(Node node, long pd, long mode) {
        Poller.await(pd, mode, true);
    }

    @TruffleBoundary
    static void internal_poll_runtime_pollSetDeadline(Node node, long pd, long d, long mode) {
        Poller.setDeadline(pd, d, mode);
    }

    @TruffleBoundary
    static void internal_poll_runtime_pollUnblock(Node node, long pd) {
        Poller.unblock(pd);
    }

    @TruffleBoundary
    static boolean internal_poll_runtime_isPollServerDescriptor(Node node, long fd) {
        return Poller.isPollDescriptor(fd);
    }
}
