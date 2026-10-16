package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.runtime.ChanOps;
import graalgo.runtime.GoChan;
import graalgo.runtime.TimerChan;
import graalgo.values.GoKind;

/**
 * R1-P1-1: {@code len(ch)} / {@code cap(ch)} on channels through the
 * frontend's own {@link LenNode} / {@link CapNode} (WS G builtins,
 * verifies GG-0020).
 *
 * <p>Before the fix, both constructors threw
 * {@code FrontendException("len of CHAN is not implemented (WS G);
 * channels are Phase 2")} for any channel child — a stale Phase-1 scope
 * error. Channels are implemented; the nodes must dispatch to the
 * runtime module's {@link ChanOps#len} / {@link ChanOps#cap}
 * (call-through, never reimplemented) with gc parity for nil and timer
 * channels.
 *
 * <p>Fixtures run scheduler-free: {@code buffered()}/{@code capacity()}
 * are plain synchronized reads, so no {@code SchedulerCore} is installed.
 */
public final class LenCapChanTest {

    /** Minimal CHAN-kind child returning a fixed channel value (null = nil channel). */
    static final class ChanConst extends GoExprNode {
        private final Object ch;

        ChanConst(Object ch) {
            super(GoKind.CHAN);
            this.ch = ch;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return ch;
        }
    }

    private static void assertEq(long want, long got, String what) {
        if (want != got) {
            throw new AssertionError(what + ": want " + want + ", got " + got);
        }
    }

    /** The stale scope error must be gone: CHAN is a legal LenNode argument. */
    public void testLenNodeAcceptsChan() {
        new LenNode(new ChanConst(new GoChan(3, null)));
    }

    /** The stale scope error must be gone: CHAN is a legal CapNode argument. */
    public void testCapNodeAcceptsChan() {
        new CapNode(new ChanConst(new GoChan(3, null)));
    }

    /** Empty buffered channel: len 0, cap = buffer capacity. */
    public void testLenCapEmptyBufferedChannel() {
        GoChan ch = new GoChan(5, null);
        assertEq(0, new LenNode(new ChanConst(ch)).executeLong(null), "len(empty,cap5)");
        assertEq(5, new CapNode(new ChanConst(ch)).executeLong(null), "cap(empty,cap5)");
    }

    /** Unbuffered channel: len 0, cap 0. */
    public void testLenCapUnbufferedChannel() {
        GoChan ch = new GoChan(0, null);
        assertEq(0, new LenNode(new ChanConst(ch)).executeLong(null), "len(unbuffered)");
        assertEq(0, new CapNode(new ChanConst(ch)).executeLong(null), "cap(unbuffered)");
    }

    /** gc parity: len/cap of a nil channel are 0 (not a scope error, not a panic). */
    public void testLenCapNilChannelIsZero() {
        assertEq(0, new LenNode(new ChanConst(null)).executeLong(null), "len(nil)");
        assertEq(0, new CapNode(new ChanConst(null)).executeLong(null), "cap(nil)");
    }

    /**
     * DIV-M3-1: timer channels observe as unbuffered even with a physical
     * buffer — the masking lives in {@link TimerChan}, reached here through
     * the ChanOps call-through (proves the node does not reimplement len).
     */
    public void testLenCapTimerChannelIsZero() {
        TimerChan ch = new TimerChan();
        assertEq(0, new LenNode(new ChanConst(ch)).executeLong(null), "len(timerchan)");
        assertEq(0, new CapNode(new ChanConst(ch)).executeLong(null), "cap(timerchan)");
    }

    /** Non-channel values still fail fast with the scope error (no silent accept). */
    public void testNonChanStillRejected() {
        GoExprNode bad = new GoExprNode(GoKind.INT64) {
            @Override
            public long executeLong(VirtualFrame frame) {
                return 1L;
            }
        };
        try {
            new LenNode(bad);
        } catch (FrontendException e) {
            if (!e.getMessage().contains("len of INT64 is not implemented")) {
                throw new AssertionError("unexpected len message: " + e.getMessage());
            }
            return;
        }
        throw new AssertionError("LenNode must still reject INT64");
    }
}
