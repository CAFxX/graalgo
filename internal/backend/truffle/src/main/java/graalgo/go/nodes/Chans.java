package graalgo.go.nodes;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.ExplodeLoop;
import graalgo.go.nodes.N.Bool;
import graalgo.go.nodes.N.Expr;
import graalgo.go.nodes.N.GoNode;
import graalgo.go.nodes.N.I64;
import graalgo.go.nodes.N.Ref;
import graalgo.go.nodes.N.Stmt;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.Panics;
import graalgo.go.types.GoType;
import graalgo.runtime.ChanOps;
import graalgo.runtime.GoChan;
import graalgo.runtime.GoErrorValue;
import graalgo.runtime.PanicException;

/**
 * Channel operations: one node each, the runtime entry behind a boundary (truffle-design §4.2).
 * This is the adapter onto the existing channel implementation ({@link GoChan},
 * {@link ChanOps#selectGo}): elements cross it boxed in carrier form, a nil channel is Java null, and
 * the runtime's own panics are re-raised as panics of this node package.
 */
public final class Chans {
    private Chans() {}

    @TruffleBoundary
    static RuntimeException convert(GoNode n, PanicException e) {
        return Panics.of(n, (GoErrorValue) e.goValue());
    }

    public static final class MakeChan extends Ref {
        private final GoType elem;
        @Child private I64 cap;

        public MakeChan(GoType elem, I64 cap) {
            this.elem = elem;
            this.cap = cap;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            long c = cap == null ? 0 : cap.executeLong(frame);
            if (c < 0 || c > Integer.MAX_VALUE) {
                throw Panics.plain(this, "makechan: size out of range");
            }
            return create((int) c);
        }

        @TruffleBoundary
        private GoChan create(int c) {
            return new graalgo.go.values.TypedChan(elem, c);
        }
    }

    public static final class Send extends Stmt {
        @Child private Expr ch;
        @Child private Expr value;

        public Send(Expr ch, Expr value) {
            this.ch = ch;
            this.value = value;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            GoChan c = (GoChan) ch.executeGeneric(frame);
            Object v = value.executeGeneric(frame);
            if (c == null) {
                GoProcess.get(this).blockForever("chan send (nil chan)");
            }
            try {
                send(c, v);
            } catch (PanicException e) {
                throw convert(this, e);
            }
        }

        @TruffleBoundary
        private static void send(GoChan c, Object v) {
            c.send(v);
        }
    }

    /** {@code <-ch}: the element, boxed. */
    public static final class Recv extends Ref {
        @Child private Expr ch;

        public Recv(Expr ch) {
            this.ch = ch;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            GoChan c = (GoChan) ch.executeGeneric(frame);
            if (c == null) {
                GoProcess.get(this).blockForever("chan receive (nil chan)");
            }
            return recv(c).value();
        }
    }

    @TruffleBoundary
    static GoChan.RecvResult recv(GoChan c) {
        return c.recv();
    }

    /** {@code v, ok := <-ch} as a tuple. */
    public static final class RecvOk extends Ref {
        @Child private Expr ch;

        public RecvOk(Expr ch) {
            this.ch = ch;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            GoChan c = (GoChan) ch.executeGeneric(frame);
            if (c == null) {
                GoProcess.get(this).blockForever("chan receive (nil chan)");
            }
            GoChan.RecvResult r = recv(c);
            return new Object[] {r.value(), r.ok()};
        }
    }

    /** Range over a channel: receives into an object slot, false once closed and drained. */
    public static final class RecvNext extends Bool {
        @Child private Expr ch;
        private final int slot;

        public RecvNext(Expr ch, int slot) {
            this.ch = ch;
            this.slot = slot;
        }

        @Override
        public boolean executeBoolean(VirtualFrame frame) {
            GoChan c = (GoChan) ch.executeGeneric(frame);
            if (c == null) {
                GoProcess.get(this).blockForever("chan receive (nil chan)");
            }
            GoChan.RecvResult r = recv(c);
            frame.setObject(slot, r.value());
            return r.ok();
        }
    }

    public static final class Close extends Stmt {
        @Child private Expr ch;

        public Close(Expr ch) {
            this.ch = ch;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            GoChan c = (GoChan) ch.executeGeneric(frame);
            if (c == null) {
                throw Panics.plain(this, "close of nil channel");
            }
            try {
                close(c);
            } catch (PanicException e) {
                throw convert(this, e);
            }
        }

        @TruffleBoundary
        private static void close(GoChan c) {
            c.close();
        }
    }

    public static final class ChanLen extends I64 {
        @Child private Expr ch;
        private final boolean cap;

        public ChanLen(Expr ch, boolean cap) {
            this.ch = ch;
            this.cap = cap;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            GoChan c = (GoChan) ch.executeGeneric(frame);
            return c == null ? 0 : query(c);
        }

        @TruffleBoundary
        private long query(GoChan c) {
            return cap ? c.capacity() : c.buffered();
        }
    }

    /**
     * {@code select}: evaluates every channel and send operand in source order, runs the
     * runtime's select, stores a received value and its ok flag into two frame slots, and
     * yields the index of the chosen case (the default case has index {@code chans.length}).
     */
    public static final class Select extends I64 {
        @Children private final Expr[] chans;
        @Children private final Expr[] sends;
        private final boolean hasDefault;
        private final int valueSlot;
        private final int okSlot;

        public Select(Expr[] chans, Expr[] sends, boolean hasDefault, int valueSlot, int okSlot) {
            this.chans = chans;
            this.sends = sends;
            this.hasDefault = hasDefault;
            this.valueSlot = valueSlot;
            this.okSlot = okSlot;
        }

        @Override
        @ExplodeLoop
        public long executeLong(VirtualFrame frame) {
            Object[] c = new Object[chans.length];
            Object[] v = new Object[chans.length];
            for (int i = 0; i < chans.length; i++) {
                c[i] = chans[i].executeGeneric(frame);
                if (sends[i] != null) {
                    v[i] = sends[i].executeGeneric(frame);
                }
            }
            Object[] result;
            try {
                result = run(c, v);
            } catch (PanicException e) {
                throw convert(this, e);
            }
            frame.setObject(valueSlot, result[1]);
            frame.setBoolean(okSlot, (boolean) result[2]);
            return (int) result[0];
        }

        @TruffleBoundary
        private Object[] run(Object[] c, Object[] v) {
            int n = c.length;
            if (n == 0 && !hasDefault) {
                GoProcess.get(this).blockForever("select (no cases)");
            }
            ChanOps.SelectCase[] cases = new ChanOps.SelectCase[n + (hasDefault ? 1 : 0)];
            for (int i = 0; i < n; i++) {
                // A nil channel is never ready: a private channel nobody else holds.
                GoChan ch = c[i] == null ? new GoChan(0, null) : (GoChan) c[i];
                cases[i] = new ChanOps.SelectCase(ch, sends[i] != null, v[i]);
            }
            if (hasDefault) {
                cases[n] = new ChanOps.SelectCase(null, false, null);
            }
            int index = ChanOps.selectGo(cases);
            return new Object[] {index, ChanOps.selectRecvValue(), ChanOps.selectRecvOk()};
        }
    }
}
