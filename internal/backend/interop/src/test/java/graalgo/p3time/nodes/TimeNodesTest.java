package graalgo.p3time.nodes;

import java.lang.reflect.Constructor;
import java.util.List;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Unit tests for the directly testable {@code p3time} node
 * (R-03 remediation, 2026-10-31): {@code TimeNowNode}.
 *
 * <p>{@code TimeNowNode} reads only the wall clock, so it executes
 * without a scheduler or a frontend type table: the test builds the
 * two-long descriptor the node expects and pins that the returned
 * struct carries a current wall time (unix nanos) and the
 * {@code isSet} marker, exactly as the node's documented gc parity
 * requires. A node returning a stale, zero, or millisecond-scale
 * time fails.
 *
 * <p>Not covered here, deliberately: {@code TimeSleepNode} parks
 * through the runtime timer heap, and {@code TimeAfterNode} /
 * {@code TimeTickNode} / {@code TimeAfterFuncNode} /
 * {@code TimeCNode} / {@code TimeNewNode} produce or drive timer
 * channels — all need live timer/scheduler machinery. Their
 * behaviour is covered differentially by the timers corpus
 * ({@code test/corpus/phase2/timers/tm01_sleep_order.go} through
 * {@code tm10_ticker_drop.go}).
 */
public final class TimeNodesTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long e, long a, String msg) {
        if (e != a) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    private static VirtualFrame frame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0],
                FrameDescriptor.newBuilder().build());
    }

    public void testTimeNowReturnsCurrentWallTimeAndIsSet() throws Exception {
        // NB: List.of rejects null elements, so the element-kind list is
        // built as an ArrayList (the RtMemStatsTest pattern).
        java.util.List<GoKind> elemKinds = new java.util.ArrayList<>();
        elemKinds.add(null);
        elemKinds.add(null);
        GoStructDescriptor desc = GoStructDescriptor.layout("time.Time",
                List.of("wall", "ext"),
                List.of(GoKind.UINT64, GoKind.UINT64),
                List.of(-1, -1),
                elemKinds);
        Constructor<TimeNowNode> c = TimeNowNode.class.getDeclaredConstructor(
                GoStructDescriptor.class);
        c.setAccessible(true);
        GoExprNode now = c.newInstance(desc);
        long beforeMs = System.currentTimeMillis();
        Object got = now.executeObject(frame());
        long afterMs = System.currentTimeMillis();
        assertTrue(got instanceof GoStruct, "TimeNow must return a GoStruct");
        GoStruct t = (GoStruct) got;
        long wallNanos = t.getLong(0);
        assertTrue(wallNanos >= beforeMs * 1_000_000L
                        && wallNanos <= (afterMs + 1000) * 1_000_000L,
                "wall must be the current time in unix nanos, got " + wallNanos
                        + " for window [" + beforeMs + ", " + afterMs + "] ms");
        assertEq(1, t.getLong(1), "ext/isSet marker must be 1 (never the zero Time)");
    }
}
