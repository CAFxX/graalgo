package graalgo.frontend;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.runtime.ChanOps;
import graalgo.runtime.GoChan;
import graalgo.runtime.TimerChan;
import graalgo.sched.SchedulerCore;
import graalgo.values.GoKind;

/**
 * P1-1 (review-r2): {@code len(ch)} / {@code cap(ch)} through the frozen
 * WS G {@link LenNode} / {@link CapNode}.
 *
 * <p>The WS G nodes rejected {@code CHAN} with a stale "channels are
 * Phase 2" {@link FrontendException} although channels are implemented
 * (P2-M2/P3-M5). The fix dispatches {@code CHAN} to
 * {@link ChanOps#len}/{@link ChanOps#cap} — the same call the P3
 * {@code "chanlen"}/{@code "chancap"} wire nodes make — with gc parity for a
 * nil channel (len/cap of nil are 0, not a blocking operation).
 *
 * <p>verifies: GG-0020 (WS G builtin, channel case)
 */
public final class LenCapChanTest {

    /** Minimal CHAN-kind child evaluating to a fixed channel value (null = nil channel). */
    private static final class ChanValueNode extends GoExprNode {
        private final Object value;

        ChanValueNode(Object value) {
            super(GoKind.CHAN);
            this.value = value;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return value;
        }
    }

    /** Minimal child of a kind len/cap must still reject (negative control). */
    private static final class FuncValueNode extends GoExprNode {
        FuncValueNode() {
            super(GoKind.FUNC);
        }
    }

    private interface ThrowingRunnable {
        void run() throws Exception;
    }

    private static void assertEq(long expected, long actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    private static VirtualFrame frame() {
        return Truffle.getRuntime()
                .createVirtualFrame(new Object[0], FrameDescriptor.newBuilder().build());
    }

    /**
     * Installs a scheduler for the body: {@code GoChan.send} (used to
     * fill a buffered channel) reaches {@code SchedulerCore.current()}.
     */
    private static void withScheduler(ThrowingRunnable body) throws Exception {
        SchedulerCore sched = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            body.run();
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }

    /** Regression: the constructor used to throw FrontendException for CHAN. */
    public void testConstructorAcceptsChan() {
        new LenNode(new ChanValueNode(null));
        new CapNode(new ChanValueNode(null));
    }

    public void testLenBufferedChannel() throws Exception {
        withScheduler(() -> {
            GoChan ch = new GoChan(3, null);
            ChanOps.send(ch, 1L);
            ChanOps.send(ch, 2L);
            LenNode len = new LenNode(new ChanValueNode(ch));
            assertEq(2, len.executeLong(frame()), "len(buffered chan, 2 elems)");
            assertEq(2, len.executeInt(frame()), "len via executeInt");
        });
    }

    public void testCapBufferedChannel() throws Exception {
        withScheduler(() -> {
            GoChan ch = new GoChan(3, null);
            ChanOps.send(ch, 1L);
            CapNode cap = new CapNode(new ChanValueNode(ch));
            assertEq(3, cap.executeLong(frame()), "cap(buffered chan, capacity 3)");
            assertEq(3, cap.executeInt(frame()), "cap via executeInt");
        });
    }

    public void testLenCapUnbufferedChannel() {
        GoChan ch = new GoChan(0, null);
        assertEq(0, new LenNode(new ChanValueNode(ch)).executeLong(frame()),
                "len(unbuffered chan)");
        assertEq(0, new CapNode(new ChanValueNode(ch)).executeLong(frame()),
                "cap(unbuffered chan)");
    }

    /** gc parity: len/cap of a nil channel are 0 (not a blocking operation). */
    public void testLenCapNilChannel() {
        assertEq(0, new LenNode(new ChanValueNode(null)).executeLong(frame()),
                "len(nil chan)");
        assertEq(0, new CapNode(new ChanValueNode(null)).executeLong(frame()),
                "cap(nil chan)");
    }

    /** DIV-M3-1: timer channels observe as unbuffered in every state. */
    public void testLenCapTimerChannel() {
        TimerChan ch = new TimerChan();
        assertEq(0, new LenNode(new ChanValueNode(ch)).executeLong(frame()),
                "len(timer chan)");
        assertEq(0, new CapNode(new ChanValueNode(ch)).executeLong(frame()),
                "cap(timer chan)");
    }

    /** Genuinely unhandled kinds must still fail loudly. */
    public void testLenRejectsFunc() {
        try {
            new LenNode(new FuncValueNode());
        } catch (FrontendException expected) {
            return;
        }
        throw new AssertionError("len(func) must throw FrontendException");
    }

    /** Genuinely unhandled kinds must still fail loudly. */
    public void testCapRejectsFunc() {
        try {
            new CapNode(new FuncValueNode());
        } catch (FrontendException expected) {
            return;
        }
        throw new AssertionError("cap(func) must throw FrontendException");
    }
}
