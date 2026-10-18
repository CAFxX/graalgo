package graalgo.p3builtin.nodes;

import java.lang.reflect.Constructor;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.sched.SchedulerCore;
import graalgo.values.GoKind;

/**
 * P1-29: {@code runtime.GOMAXPROCS(n)} propagates the new value to the
 * scheduler's admission cap — gc changes the thread limit, not just the
 * reported number.
 *
 * <p>White-box: the node constructor is private (nodes are built via the
 * {@code "runtimegomaxprocs"} wire {@code build}); the test reflects it
 * with a fixed-int child. The {@code build} field parsing is unchanged by
 * this fix (and exercised by the launcher end-to-end path); the new
 * behavior lives entirely in {@code executeLong}.
 *
 * <p>Red-green: pre-fix, {@code executeLong} only touched the static
 * cell, so {@code sched.cap()} stayed at the construction value and
 * {@link #testSetPropagatesToSchedulerCap} failed.
 */
public final class GoMaxProcsNodeTest {

    /** Test double: a fixed-value int expression. */
    private static final class FixedInt extends GoExprNode {
        private final long v;

        FixedInt(long v) {
            super(GoKind.INT64);
            this.v = v;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return v;
        }
    }

    private static GoExprNode newNode(long want) throws Exception {
        Constructor<GoMaxProcsNode> c = GoMaxProcsNode.class
                .getDeclaredConstructor(GoKind.class, GoExprNode.class);
        c.setAccessible(true);
        return c.newInstance(GoKind.INT64, new FixedInt(want));
    }

    private static void assertEq(long expected, long actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /**
     * GOMAXPROCS(1) on an installed scheduler: returns the previous value
     * (gc round-trip) AND drives the scheduler cap down to 1.
     */
    public void testSetPropagatesToSchedulerCap() throws Exception {
        SchedulerCore sched = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        int carriers = sched.carrierParallelism();
        int initialCap = Math.min(4, carriers);
        assertEq(initialCap, sched.cap(), "precondition: construction cap");
        int prevReported = GoMaxProcsNode.current();
        SchedulerCore prev = SchedulerCore.install(sched);
        VirtualFrame frame = Truffle.getRuntime()
                .createVirtualFrame(new Object[0], new FrameDescriptor());
        try {
            long prev = newNode(1).executeLong(frame);
            assertEq(prevReported, prev, "returns the previous GOMAXPROCS value");
            assertEq(1, GoMaxProcsNode.current(), "reported value follows GOMAXPROCS(1)");
            assertEq(1, sched.requestedCap(), "scheduler requestedCap follows GOMAXPROCS(1)");
            assertEq(1, sched.cap(), "scheduler cap follows GOMAXPROCS(1)");

            // Growth is honored too (clamped by the carrier parallelism).
            newNode(4 * carriers + 7).executeLong(frame);
            assertEq(4 * carriers + 7, sched.requestedCap(), "requestedCap keeps the ask");
            assertEq(carriers, sched.cap(), "cap clamped to carriers");
        } finally {
            SchedulerCore.install(prev);
            // Restore the process-wide cell for other tests in this JVM
            // (scheduler uninstalled: only the cell is touched).
            newNode(prevReported).executeLong(frame);
            assertEq(prevReported, GoMaxProcsNode.current(), "cell restored");
        }
    }

    /**
     * Query and non-positive forms never touch the scheduler: GOMAXPROCS(0)
     * reports, GOMAXPROCS(-n) is a no-op (gc: "If n <= 0, it does not
     * change the current setting").
     */
    public void testQueryAndNonPositiveLeaveSchedulerAlone() throws Exception {
        SchedulerCore sched = new SchedulerCore(2, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        int prevReported = GoMaxProcsNode.current();
        SchedulerCore prev = SchedulerCore.install(sched);
        VirtualFrame frame = Truffle.getRuntime()
                .createVirtualFrame(new Object[0], new FrameDescriptor());
        try {
            int before = sched.requestedCap();
            long q = newNode(0).executeLong(frame);
            assertEq(GoMaxProcsNode.current(), q, "GOMAXPROCS(0) reports the value");
            assertEq(before, sched.requestedCap(), "GOMAXPROCS(0) leaves the cap alone");

            long n = newNode(-5).executeLong(frame);
            assertEq(GoMaxProcsNode.current(), n, "GOMAXPROCS(-5) reports the value");
            assertEq(before, sched.requestedCap(), "GOMAXPROCS(-5) leaves the cap alone");
        } finally {
            SchedulerCore.install(prev);
            newNode(prevReported).executeLong(frame);
        }
    }

    /**
     * Scheduler-less Phase-1 runs: the node still round-trips the value
     * (no scheduler entry points are reached — {@code installedOrNull},
     * never throws).
     */
    public void testWorksWithoutScheduler() throws Exception {
        SchedulerCore prev = SchedulerCore.install(null);
        int prevReported = GoMaxProcsNode.current();
        VirtualFrame frame = Truffle.getRuntime()
                .createVirtualFrame(new Object[0], new FrameDescriptor());
        try {
            long p = newNode(prevReported + 3).executeLong(frame);
            assertEq(prevReported, p, "returns the previous value");
            assertEq(prevReported + 3, GoMaxProcsNode.current(), "value round-trips");
        } finally {
            SchedulerCore.install(prev);
            newNode(prevReported).executeLong(frame);
        }
    }
}
