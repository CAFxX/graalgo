package graalgo.frontend;

import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.FrameSlotTypeException;
import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;
import graalgo.runtime.DeferChain;
import graalgo.runtime.DeferredCall;
import graalgo.runtime.GoErrors;
import graalgo.runtime.GoroutineState;
import graalgo.runtime.Panic;
import graalgo.safepoint.Safepoint;
import graalgo.values.GoBox;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import java.util.ArrayList;
import java.util.List;

/**
 * {@code range} over a function — Go 1.23 range-over-func (PKG-039).
 *
 * <p>A range statement whose operand has a function type
 * {@code func(yield func(K[, V]) bool)} is <em>not</em> a loop over a
 * container: per the language spec (and gc's
 * {@code cmd/compile/internal/rangefunc} rewrite, which this mirrors) it
 * is a call of the sequence function with a synthesized yield closure:
 *
 * <pre>
 *   for k, v := range seq { body }  ≡  seq(func(k K, v V) bool { body; return true })
 * </pre>
 *
 * with {@code break} in the body becoming {@code return false} and
 * {@code continue} becoming {@code return true}.
 *
 * <p>Implementation: the backend builds a real {@link GoFunc} for the
 * yield closure (a {@link YieldRoot} root node + {@link FuncLitNode},
 * which materializes the enclosing frame) and calls the sequence value
 * with it via {@link FuncValueCallNode}. The yield body is the
 * already-built loop body, executed <em>against the enclosing
 * function's materialized frame</em> — so iteration variables, locals,
 * and captures resolve exactly as in the source, and control-flow
 * signals behave per Go semantics with no desugaring of the body:
 *
 * <ul>
 *   <li>{@code break}/{@code continue} (labeled or not) targeting this
 *       range are caught by the yield root and become {@code false} /
 *       {@code true}; those targeting outer constructs propagate.</li>
 *   <li>{@code return}, {@code goto}, and labeled {@code break}/{@code
 *       continue} targeting <em>outside</em> the range cannot propagate
 *       through the sequence call directly: the sequence function is a
 *       real compiled function whose root would mistake a {@link GoReturn}
 *       for its own return, and gc runs the sequence function's defers
 *       before any such transfer. Mirroring gc's {@code #next} rewrite,
 *       the yield root stashes the signal in the {@link IterState}, sets
 *       state {@code DONE}, and returns {@code false}; the post-call node
 *       re-throws the stashed signal after the sequence returns, so the
 *       sequence always returns normally (its defers run) and control
 *       resumes exactly as in gc.</li>
 *   <li>{@code defer} in the body is registered on the goroutine's
 *       {@link graalgo.runtime.DeferChain} with no intervening
 *       watermark (the yield root is a plain {@link RootNode}, not a
 *       {@link GoFuncRootNode}), so it runs at the <em>enclosing</em>
 *       function's return — gc's {@code deferrangefunc} semantics.</li>
 * </ul>
 *
 * <p>Iterator-misuse checking mirrors gc's {@code #stateK} machine
 * (states {@code READY/PANIC/DONE/EXHAUSTED}, same panic messages as
 * gc's {@code runtime.panicrangestate}): yielding after the body
 * returned {@code false}, after a body panic, re-entrantly, or after
 * loop exit panics; a sequence function that swallows a body panic and
 * returns normally is reported when the call completes.
 *
 * <p>Per-iteration capture (loopcap): when an iteration variable is
 * boxed, each yield installs a fresh {@link GoBox} cell ({@code :=}
 * form) or writes the existing cell ({@code =} form on an outer boxed
 * variable) — the same rule as the container range nodes.
 *
 * <p>Concurrency (M10, F-1): no per-execution state in node fields.
 * The {@link IterState} lives in a hidden frame slot of the enclosing
 * function; the shared {@link YieldRoot} reads it from the
 * materialized frame, so concurrent goroutines each get their own.
 */
public final class RangeFuncNode extends GoStmtNode {
    @Child private GoStmtNode initState;
    @Child private GoStmtNode doCall;
    @Child private GoStmtNode postCall;

    public RangeFuncNode(GoStmtNode initState, GoStmtNode doCall, GoStmtNode postCall) {
        this.initState = initState;
        this.doCall = doCall;
        this.postCall = postCall;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        initState.executeVoid(frame);
        doCall.executeVoid(frame);
        postCall.executeVoid(frame);
    }

    // ------------------------------------------------------------------
    // Iterator state (mirrors gc's abi.RF_* values).
    // ------------------------------------------------------------------

    /** Per-execution iterator state, held in a hidden Object frame slot. */
    static final class IterState {
        int state;
        /**
         * Control signal stashed by the yield root when the body exits via
         * {@code return}, {@code goto}, or a labeled {@code break} /
         * {@code continue} targeting outside this range (gc's
         * {@code #next}); re-thrown by the post-call node after the
         * sequence returns. Null on the normal path.
         */
        RuntimeException pending;

        IterState(int state) {
            this.state = state;
        }
    }

    static final int RF_DONE = 0;
    static final int RF_READY = 1;
    static final int RF_PANIC = 2;
    static final int RF_EXHAUSTED = 3;

    /** Panic message for a yield entry in a non-ready state (gc's panicrangestate texts). */
    static String rangeStateMessage(int state) {
        return switch (state) {
            case RF_DONE -> "range function continued iteration after function for loop body returned false";
            case RF_PANIC -> "range function continued iteration after loop body panic";
            case RF_EXHAUSTED -> "range function continued iteration after whole loop exit";
            default -> "range function yielded in an unexpected state";
        };
    }

    /** Installs a fresh {@link IterState}{@code (READY)} before the sequence call. */
    static final class InitState extends GoStmtNode {
        private final int stateSlot;

        InitState(int stateSlot) {
            this.stateSlot = stateSlot;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            frame.setObject(stateSlot, new IterState(RF_READY));
        }
    }

    /**
     * After the sequence call returns normally: a {@code PANIC} state
     * means the sequence swallowed a body panic without re-panicking
     * (gc's {@code RF_MISSING_PANIC}); then the sequence is exhausted.
     * A signal stashed by the yield root (body {@code return} /
     * {@code goto} / labeled break-or-continue outward — gc's
     * {@code #next}) is re-thrown now that the sequence has returned
     * (so its defers have run). Exceptions (body panic, goexit) bypass
     * this node — the call never returned.
     */
    static final class PostCall extends GoStmtNode {
        private final int stateSlot;

        PostCall(int stateSlot) {
            this.stateSlot = stateSlot;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            IterState st = (IterState) frame.getObject(stateSlot);
            if (st.state == RF_PANIC) {
                Panic.throwPanic(
                        GoErrors.rangeState(
                                "range function recovered a loop body panic and did not resume panicking"),
                        null, null, this);
            }
            st.state = RF_EXHAUSTED;
            RuntimeException pending = st.pending;
            if (pending != null) {
                st.pending = null;
                throw pending;
            }
        }
    }

    // ------------------------------------------------------------------
    // The yield closure's root node.
    // ------------------------------------------------------------------

    /**
     * Root node of the synthesized yield closure
     * {@code func(k K, v V) bool}.
     *
     * <p>Calling convention: {@code args[0]} is the yield {@link GoFunc}
     * itself (its {@code closureFrame()} is the materialized enclosing
     * function frame, installed by {@link FuncLitNode}); {@code
     * args[1..]} are the iteration values in D5 boxing. The body runs
     * against the enclosing frame; this node deliberately installs no
     * defer watermark (see class doc).
     */
    public static final class YieldRoot extends RootNode {
        @Child private GoStmtNode body;
        private final int stateSlot;
        private final int keySlot;
        private final GoKind keyKind;
        private final boolean keyBoxed;
        private final boolean keyDefine;
        private final int valSlot;
        private final GoKind valKind;
        private final boolean valBoxed;
        private final boolean valDefine;
        /** This range statement's label (null when unlabeled). */
        private final String label;
        /**
         * PKG-039: enclosing frame slot holding the range-over-func defer
         * stash (gc's {@code deferrangefunc}).
         */
        private final int rangeStashSlot;

        public YieldRoot(FrameDescriptor descriptor, GoStmtNode body, int stateSlot,
                         int keySlot, GoKind keyKind, boolean keyBoxed, boolean keyDefine,
                         int valSlot, GoKind valKind, boolean valBoxed, boolean valDefine,
                         String label, int rangeStashSlot) {
            super(null, descriptor);
            this.body = body;
            this.stateSlot = stateSlot;
            this.keySlot = keySlot;
            this.keyKind = keyKind;
            this.keyBoxed = keyBoxed;
            this.keyDefine = keyDefine;
            this.valSlot = valSlot;
            this.valKind = valKind;
            this.valBoxed = valBoxed;
            this.valDefine = valDefine;
            this.label = label;
            this.rangeStashSlot = rangeStashSlot;
        }

        @Override
        public Object execute(VirtualFrame frame) {
            Safepoint.poll();
            Object[] args = frame.getArguments();
            GoFunc self = (GoFunc) args[0];
            MaterializedFrame outer = self.closureFrame();
            IterState st = (IterState) outer.getObject(stateSlot);
            if (st.state != RF_READY) {
                Panic.throwPanic(GoErrors.rangeState(rangeStateMessage(st.state)), null, null, this);
            }
            st.state = RF_PANIC;
            if (keySlot >= 0) {
                // Value-copy rule: the sequence may reuse its buffers;
                // each iteration binds a copy (same as container ranges).
                Object k = Boxing.copyForAssign(keyKind, args[1]);
                if (keyBoxed) {
                    if (keyDefine) {
                        outer.setObject(keySlot, new GoBox(k));
                    } else {
                        Boxing.setBoxedCell(outer, keySlot, keyKind, k);
                    }
                } else {
                    Boxing.setSlot(outer, keySlot, keyKind, k);
                }
            }
            if (valSlot >= 0) {
                Object v = Boxing.copyForAssign(valKind, args[2]);
                if (valBoxed) {
                    if (valDefine) {
                        outer.setObject(valSlot, new GoBox(v));
                    } else {
                        Boxing.setBoxedCell(outer, valSlot, valKind, v);
                    }
                } else {
                    Boxing.setSlot(outer, valSlot, valKind, v);
                }
            }
            boolean cont;
            // PKG-039 (gc's deferrangefunc): defers pushed by the body
            // belong to the enclosing function, not to the sequence
            // function (whose root would otherwise drain them on return).
            // Extract them into the enclosing frame's stash; the enclosing
            // root merges them back at unwind.
            GoroutineState gs = GoroutineState.current();
            DeferChain chain = gs != null ? gs.deferChain() : null;
            int deferBase = chain != null ? chain.depth() : -1;
            try {
                try {
                    body.executeVoid(outer);
                    cont = true;
                } catch (BreakSignal b) {
                    if (!b.matches(label)) {
                        // Labeled break targeting an outer construct: stash
                        // (gc's #next), stop the sequence, re-throw after it
                        // returns so the sequence's defers run first.
                        st.pending = b;
                        st.state = RF_DONE;
                        return Boolean.FALSE;
                    }
                    cont = false;
                } catch (ContinueSignal c) {
                    if (!c.matches(label)) {
                        st.pending = c;
                        st.state = RF_DONE;
                        return Boolean.FALSE;
                    }
                    cont = true;
                } catch (GoReturn r) {
                    // `return` in the body returns from the enclosing
                    // function, not from the sequence (gc's #next = -1).
                    st.pending = r;
                    st.state = RF_DONE;
                    return Boolean.FALSE;
                } catch (GotoSignal g) {
                    // `goto` out of the body (gc's #next): same treatment.
                    st.pending = g;
                    st.state = RF_DONE;
                    return Boolean.FALSE;
                }
                st.state = cont ? RF_READY : RF_DONE;
                return Boolean.valueOf(cont);
            } finally {
                if (chain != null && deferBase >= 0) {
                    rehomeBodyDefers(chain, outer, deferBase);
                }
            }
        }

        /**
         * Moves defers pushed by this yield's body execution from the
         * chain into the enclosing frame's range-defer stash (in push
         * order). Nested calls drain their own defers via their roots,
         * so everything above {@code base} is the body's own.
         */
        private void rehomeBodyDefers(DeferChain chain, MaterializedFrame outer, int base) {
            List<DeferredCall> taken = chain.takeAbove(base);
            if (taken.isEmpty()) {
                return;
            }
            Object cur;
            try {
                cur = outer.getObject(rangeStashSlot);
            } catch (FrameSlotTypeException e) {
                cur = null;
            }
            List<DeferredCall> stash;
            if (cur instanceof List<?> l) {
                @SuppressWarnings("unchecked")
                List<DeferredCall> cast = (List<DeferredCall>) l;
                stash = cast;
            } else {
                stash = new ArrayList<>();
                outer.setObject(rangeStashSlot, stash);
            }
            stash.addAll(taken);
        }

        @Override
        public String toString() {
            return "RangeFuncYield()";
        }
    }
}
