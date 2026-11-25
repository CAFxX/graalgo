package graalgo.go.nodes;

import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;
import com.oracle.truffle.api.profiles.BranchProfile;
import com.oracle.truffle.api.source.SourceSection;
import graalgo.go.nodes.N.Expr;
import graalgo.go.nodes.N.Jump;
import graalgo.go.nodes.N.Stmt;
import graalgo.go.runtime.G;
import graalgo.go.runtime.Panics;
import graalgo.go.runtime.Positions;
import graalgo.go.values.Closure;
import graalgo.go.values.Env;
import graalgo.language.GoLanguage;
import graalgo.safepoint.Safepoint;

/**
 * {@code for k, v := range seq { body }} over a function (Go spec, "For statements with range
 * clause"; gc's {@code cmd/compile/internal/rangefunc} rewrite): a call of {@code seq} with a
 * yield function whose body is the loop body.
 *
 * <p>The yield function is a {@link YieldRoot} that runs the already built body <em>against
 * the enclosing function's materialized frame</em>, so locals, results and iteration variables
 * resolve exactly as in the source and no desugaring of the body is needed. Leaving the body:
 * {@code continue} of this loop returns true; {@code break} of this loop returns false;
 * anything else — {@code return}, {@code goto}, a labeled break or continue of an outer
 * statement — is stashed in the loop's {@link State} and returns false, and this node rethrows
 * it once {@code seq} has returned (its own defers have run, as in gc). A panic propagates
 * through {@code seq}.
 *
 * <p>Misuse of yield panics with gc's {@code runtime.panicrangestate} errors, from the same
 * state machine as gc's {@code #stateN} ({@code abi.RF_*}): yield in any state but READY;
 * {@code seq} returning normally while the body was still marked running (it recovered a body
 * panic and did not repanic).
 *
 * <p>{@code defer} in the body belongs to the enclosing function: the outermost running
 * range-over-func loop of a frame pushes a {@link G.RangeDefers} record onto the goroutine's
 * chain before calling {@code seq} (so {@code seq}'s own defer mark lies above it), and the
 * body's defer statements ({@link Defer}) add to that record; the enclosing function — always
 * a chain function (GG-0071) — runs it when it unwinds to it (gc's {@code deferrangefunc}).
 */
public final class RangeFunc extends Stmt {
    static final int DONE = 0;
    static final int READY = 1;
    static final int PANIC = 2;
    static final int EXHAUSTED = 3;

    /** One execution of the loop: gc's {@code #stateN} plus the stashed exit. */
    static final class State {
        final MaterializedFrame outer;
        int state = READY;
        Jump pending;

        State(MaterializedFrame outer) {
            this.outer = outer;
        }
    }

    @Child private Expr seq;
    @Child private Calls.Dispatch dispatch = CallsFactory.DispatchNodeGen.create();
    private final RootCallTarget yield;
    /** The frame slot of the running loop's {@link G.RangeDefers}, or -1 when the body has no defer. */
    private final int anchorSlot;
    private final BranchProfile misuse = BranchProfile.create();

    public RangeFunc(Expr seq, RootCallTarget yield, int anchorSlot) {
        this.seq = seq;
        this.yield = yield;
        this.anchorSlot = anchorSlot;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        Closure f = (Closure) seq.executeGeneric(frame);
        State st = new State(frame.materialize());
        boolean owner = false;
        if (anchorSlot >= 0 && frame.getObjectStatic(anchorSlot) == null) {
            G g = G.current(this);
            G.RangeDefers anchor = new G.RangeDefers(g.defers);
            g.defers = anchor;
            frame.setObjectStatic(anchorSlot, anchor);
            owner = true;
        }
        try {
            if (f == null) {
                throw Panics.nilDeref(this);
            }
            dispatch.execute(f.target(), new Object[] {f.env(), new Closure(yield, st)});
        } finally {
            if (owner) {
                frame.setObjectStatic(anchorSlot, null);
            }
        }
        if (st.state == PANIC) {
            misuse.enter();
            throw Panics.error(this, "range function recovered a loop body panic and did not resume panicking");
        }
        st.state = EXHAUSTED;
        Jump p = st.pending;
        if (p != null) {
            throw p;
        }
    }

    /**
     * The yield function: binds the iteration values into the enclosing frame's slots and runs
     * the body there. Arguments: {@code [env, k?, v?]}, env the loop's {@link State} (a one-value {@link Env}).
     */
    public static final class YieldRoot extends RootNode implements Positions.Root {
        private final String name;
        /** The enclosing function's table: the body's statements are that function's. */
        private final Positions positions;
        /** The range statement's position in that table. */
        private final int pos;
        @Child private Stmt body;
        private final int keySlot;
        private final int valueSlot;
        private final Jump brk;
        private final Jump cont;
        private final BranchProfile exited = BranchProfile.create();
        private final BranchProfile misuse = BranchProfile.create();

        /** {@code brk}/{@code cont}: this loop's jumps (null when the body has none). */
        public YieldRoot(GoLanguage language, String name, Positions positions, int pos, Stmt body, int keySlot,
                int valueSlot, Jump brk, Jump cont) {
            super(language, FrameDescriptor.newBuilder().build());
            this.name = name;
            this.positions = positions;
            this.pos = pos;
            this.body = body;
            this.keySlot = keySlot;
            this.valueSlot = valueSlot;
            this.brk = brk;
            this.cont = cont;
        }

        @Override
        public Object execute(VirtualFrame frame) {
            Safepoint.poll(this);
            Object[] args = frame.getArguments();
            State st = (State) Env.get(args[0], 0, 1);
            if (st.state != READY) {
                misuse.enter();
                throw Panics.error(this, message(st.state));
            }
            st.state = PANIC;
            MaterializedFrame outer = st.outer;
            if (keySlot >= 0) {
                outer.setObjectStatic(keySlot, args[1]);
            }
            if (valueSlot >= 0) {
                outer.setObjectStatic(valueSlot, args[2]);
            }
            try {
                body.executeVoid(outer);
            } catch (Jump j) {
                if (j != cont) {
                    exited.enter();
                    st.state = DONE;
                    st.pending = j == brk ? null : j;
                    return Boolean.FALSE;
                }
            }
            st.state = READY;
            return Boolean.TRUE;
        }

        private static String message(int state) {
            switch (state) {
                case DONE:
                    return "range function continued iteration after function for loop body returned false";
                case PANIC:
                    return "range function continued iteration after loop body panic";
                default:
                    return "range function continued iteration after whole loop exit";
            }
        }

        @Override
        public Positions positions() {
            return positions;
        }

        @Override
        public String getName() {
            return name;
        }

        @Override
        public String getQualifiedName() {
            return name;
        }

        /** The range statement. */
        @Override
        public SourceSection getSourceSection() {
            return positions.section(pos);
        }

        @Override
        public String toString() {
            return name;
        }
    }

    /** {@code defer} in a range-over-func body: added to the running loop's record. */
    public static final class Defer extends Stmt {
        @Child private Calls.Prepared call;
        private final int anchorSlot;

        public Defer(Calls.Prepared call, int anchorSlot) {
            this.call = call;
            this.anchorSlot = anchorSlot;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            Object[] a = call.prepare(frame);
            G.RangeDefers r = (G.RangeDefers) frame.getObjectStatic(anchorSlot);
            r.list = new G.Defer(frame.getObjectStatic(call.targetSlot), a, r.list);
        }
    }
}
