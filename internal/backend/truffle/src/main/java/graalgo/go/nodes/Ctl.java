package graalgo.go.nodes;

import com.oracle.truffle.api.CompilerAsserts;
import com.oracle.truffle.api.CompilerDirectives;
import com.oracle.truffle.api.CompilerDirectives.CompilationFinal;
import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.ExplodeLoop;
import com.oracle.truffle.api.nodes.LoopNode;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.NodeUtil;
import com.oracle.truffle.api.nodes.RepeatingNode;
import com.oracle.truffle.api.profiles.CountingConditionProfile;
import graalgo.go.nodes.N.Bool;
import graalgo.go.nodes.N.I64;
import graalgo.go.nodes.N.Jump;
import graalgo.go.nodes.N.Stmt;
import graalgo.safepoint.Safepoint;

/**
 * Statements and control flow (truffle-design §4.8). {@code break}, {@code continue},
 * {@code goto} and {@code return} are {@link Jump}s resolved to their target statement by the
 * factory; {@code switch}, {@code range} and type switches are lowered by the factory onto
 * these nodes, with {@code fallthrough} chained statically.
 */
public final class Ctl {
    private Ctl() {}

    public static final class Block extends Stmt {
        @Children private final Stmt[] stmts;

        public Block(Stmt[] stmts) {
            this.stmts = stmts;
        }

        @Override
        @ExplodeLoop
        public void executeVoid(VirtualFrame frame) {
            for (Stmt s : stmts) {
                s.executeVoid(frame);
            }
        }
    }

    public static final class If extends Stmt {
        @Child private Bool cond;
        @Child private Stmt then;
        @Child private Stmt otherwise;
        private final CountingConditionProfile profile = CountingConditionProfile.create();

        public If(Bool cond, Stmt then, Stmt otherwise) {
            this.cond = cond;
            this.then = then;
            this.otherwise = otherwise;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            if (profile.profile(cond.executeBoolean(frame))) {
                then.executeVoid(frame);
            } else if (otherwise != null) {
                otherwise.executeVoid(frame);
            }
        }
    }

    public static final class Throw extends Stmt {
        private final Jump jump;

        public Throw(Jump jump) {
            this.jump = jump;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            throw jump;
        }
    }

    /** A statement that is the target of {@code break} (switch, select, labeled block). */
    public static final class BreakTarget extends Stmt {
        @Child private Stmt body;
        private final Jump jump;

        public BreakTarget(Stmt body, Jump jump) {
            this.body = body;
            this.jump = jump;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            try {
                body.executeVoid(frame);
            } catch (Jump j) {
                if (j != jump) {
                    throw j;
                }
            }
        }
    }

    /**
     * Every Go loop: a Truffle {@link LoopNode} (OSR, back-edge profiling) whose repeating node
     * evaluates condition, body and post statement and polls the scheduler safepoint at the
     * back-edge (SPEC §3.5, GG-SCHED-0001). A counted loop polls once per strip in compiled
     * code ({@link Repeat#strip}).
     */
    public static final class For extends Stmt {
        @Child private LoopNode loop;
        private final Jump brk;

        public For(Bool cond, Stmt body, Stmt post, Jump brk, Jump cont) {
            this(cond, body, post, brk, cont, -1, null);
        }

        /**
         * A counted loop when {@code limit} is not null: {@code cond} is {@code idx < limit} on
         * the integer slot {@code idx}, {@code post} ends with {@code idx++}, and neither the
         * body nor the post statement assigns {@code idx} or anything {@code limit} reads (the
         * factory proves it; {@code limit} is a second copy of the bound expression).
         */
        public For(Bool cond, Stmt body, Stmt post, Jump brk, Jump cont, int idx, I64 limit) {
            this.loop = Truffle.getRuntime().createLoopNode(new Repeat(cond, body, post, cont, idx, limit));
            this.brk = brk;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            try {
                loop.execute(frame);
            } catch (Jump j) {
                if (j != brk) {
                    throw j;
                }
            }
        }
    }

    static final class Repeat extends Node implements RepeatingNode {
        /** Upper bound of the iterations of one strip. */
        static final int STRIP_MAX = 1024;
        /** Node executions of the body and post statement one strip may run without a poll. */
        static final int STRIP_NODES = 1 << 15;

        @Child private Bool cond;
        @Child private Stmt body;
        @Child private Stmt post;
        private final Jump cont;
        private final CountingConditionProfile profile = CountingConditionProfile.create();
        private final int idx;
        @Child private I64 limit;
        /** Iterations per strip (a power of two); the loop is strip-mined iff {@code limit != null}. */
        private final int strip;

        Repeat(Bool cond, Stmt body, Stmt post, Jump cont, int idx, I64 limit) {
            this.cond = cond;
            this.body = body;
            this.post = post;
            this.cont = cont;
            this.idx = idx;
            this.strip = limit == null ? 1 : stripLength(body, post);
            this.limit = strip > 1 ? limit : null;
        }

        /**
         * {@link #STRIP_NODES} divided by the size of one iteration, as a power of two at most
         * {@link #STRIP_MAX}: what an iteration runs beyond its own nodes is inner loops and
         * calls, and those poll themselves (back-edges, strip ends, function entries).
         */
        private static int stripLength(Stmt body, Stmt post) {
            int nodes = NodeUtil.countNodes(body) + (post == null ? 0 : NodeUtil.countNodes(post));
            int n = Math.min(STRIP_MAX, STRIP_NODES / Math.max(1, nodes));
            return n <= 1 ? 1 : Integer.highestOneBit(n);
        }

        @Override
        public boolean executeRepeating(VirtualFrame frame) {
            if (limit != null && CompilerDirectives.inCompiledCode()) {
                return strip(frame);
            }
            return once(frame);
        }

        /**
         * One strip of a counted loop in compiled code: at most {@link #strip} iterations of an
         * inner loop whose only test is {@code idx < end} at its head, then one poll. The inner
         * loop has no call and no volatile load, so it is a counted loop the compiler optimizes
         * like a Java one (invariant motion, guard hoisting, vectorization) — the per-iteration
         * poll's slow-path call blocked all of that. {@code idx < limit} is exactly
         * {@code cond} ({@link For}). The preemption bound of GG-SCHED-0001 stays: a strip runs
         * at most {@link #STRIP_NODES} node executions plus inner loops and calls, which poll.
         * The interpreter keeps a poll per iteration, so OSR still counts every iteration.
         */
        private boolean strip(VirtualFrame frame) {
            long i = frame.getLong(idx);
            long n = limit.executeLong(frame);
            if (!profile.profile(i < n)) {
                return false;
            }
            // i < n, so n - i is the exact distance as an unsigned number.
            long end = Long.compareUnsigned(n - i, strip) > 0 ? i + strip : n;
            // The test at the head (not a do-while): the compiler recognizes only that shape as
            // a counted loop.
            while (profile.profile(frame.getLong(idx) < end)) {
                try {
                    body.executeVoid(frame);
                } catch (Jump j) {
                    if (j != cont) {
                        throw j;
                    }
                }
                post.executeVoid(frame);
            }
            Safepoint.poll(this);
            return true;
        }

        private boolean once(VirtualFrame frame) {
            if (cond != null && !profile.profile(cond.executeBoolean(frame))) {
                return false;
            }
            try {
                body.executeVoid(frame);
            } catch (Jump j) {
                if (j != cont) {
                    throw j;
                }
            }
            if (post != null) {
                post.executeVoid(frame);
            }
            Safepoint.poll(this);
            return true;
        }
    }

    /**
     * A block containing {@code goto} targets: statements run from a program counter that a
     * caught label jump resets (labels are indices; merge-exploded like a bytecode loop).
     */
    public static final class GotoBlock extends Stmt {
        @Children private final Stmt[] stmts;
        @CompilationFinal(dimensions = 1) private final Jump[] labels;
        @CompilationFinal(dimensions = 1) private final int[] targets;

        public GotoBlock(Stmt[] stmts, Jump[] labels, int[] targets) {
            this.stmts = stmts;
            this.labels = labels;
            this.targets = targets;
        }

        @Override
        @ExplodeLoop(kind = ExplodeLoop.LoopExplosionKind.MERGE_EXPLODE)
        public void executeVoid(VirtualFrame frame) {
            int pc = 0;
            while (pc < stmts.length) {
                CompilerAsserts.partialEvaluationConstant(pc);
                try {
                    stmts[pc].executeVoid(frame);
                    pc++;
                } catch (Jump j) {
                    pc = target(j);
                    Safepoint.poll(this);
                }
            }
        }

        @ExplodeLoop
        private int target(Jump j) {
            for (int i = 0; i < labels.length; i++) {
                if (labels[i] == j) {
                    return targets[i];
                }
            }
            throw j;
        }
    }
}
