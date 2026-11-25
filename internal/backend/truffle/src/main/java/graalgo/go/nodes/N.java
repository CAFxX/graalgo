package graalgo.go.nodes;

import com.oracle.truffle.api.CompilerDirectives;
import com.oracle.truffle.api.CompilerDirectives.CompilationFinal;
import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.instrumentation.GenerateWrapper;
import com.oracle.truffle.api.instrumentation.InstrumentableNode;
import com.oracle.truffle.api.instrumentation.ProbeNode;
import com.oracle.truffle.api.instrumentation.StandardTags;
import com.oracle.truffle.api.instrumentation.Tag;
import com.oracle.truffle.api.nodes.ControlFlowException;
import com.oracle.truffle.api.nodes.ExplodeLoop;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RootNode;
import com.oracle.truffle.api.source.SourceSection;
import graalgo.go.runtime.Positions;

/**
 * The node families of the Go AST (SPEC §3.3, truffle-design §4.1): statically typed expression
 * nodes and void statement nodes. The node factory picks a node class from the static Go type,
 * so a child field's family is the type check; there is no type system, no implicit cast and no
 * {@code UnexpectedResultException}.
 *
 * <p>Carriers (deviation from truffle-design §3.1, recorded in its change log): every integer
 * type travels as a {@code long} in canonical extended form (sign- or zero-extended to 64 bits)
 * and both float types as a {@code double} (a float32 is a float-representable double); narrow
 * types are re-normalized by a wrap node after an operation that can leave canonical form. That
 * halves the arithmetic node surface; partial evaluation fuses the wrap into its consumer.
 */
public final class N {
    private N() {}

    public abstract static class GoNode extends Node {
        /**
         * The section of position {@code pos} of the function this node belongs to (null if it
         * belongs to none, e.g. a generated wrapper). Off the hot path: tools and stack walks.
         */
        @TruffleBoundary
        protected final SourceSection sectionAt(int pos) {
            if (pos <= 0) {
                return null;
            }
            Positions ps = Positions.of(getRootNode());
            return ps == null ? null : ps.section(pos);
        }
    }

    /**
     * Any expression; {@code executeGeneric} is the boxing boundary (calls, interfaces). Not
     * instrumentable: expressions are typed by carrier family ({@link I64}, {@link F64}, ...), and
     * Go's steppable unit is the statement; calls are, through {@link Ref}.
     */
    public abstract static class Expr extends GoNode {
        public abstract Object executeGeneric(VirtualFrame frame);
    }

    /** Integer types of every width and signedness. */
    public abstract static class I64 extends Expr {
        public abstract long executeLong(VirtualFrame frame);

        @Override
        public final Object executeGeneric(VirtualFrame frame) {
            return executeLong(frame);
        }
    }

    /** float32 and float64. */
    public abstract static class F64 extends Expr {
        public abstract double executeDouble(VirtualFrame frame);

        @Override
        public final Object executeGeneric(VirtualFrame frame) {
            return executeDouble(frame);
        }
    }

    public abstract static class Bool extends Expr {
        public abstract boolean executeBoolean(VirtualFrame frame);

        @Override
        public final Object executeGeneric(VirtualFrame frame) {
            return executeBoolean(frame);
        }
    }

    /**
     * Reference carriers: strings, slices, maps, pointers, interfaces, funcs, flat values. Every
     * call returns a boxed result and is a {@code Ref}, so this is where the {@code CallTag}
     * wrapper goes (WP-TR-AUDIT-6); only a positioned call is instrumentable.
     */
    @GenerateWrapper
    public abstract static class Ref extends Expr implements InstrumentableNode {
        @Override
        public boolean isInstrumentable() {
            return false;
        }

        @Override
        public WrapperNode createWrapper(ProbeNode probe) {
            return new RefWrapper(this, probe);
        }
    }

    /**
     * A positioned call ({@link Calls.Call}, {@link Calls.CallClosure}, {@link Calls.IfaceCall}):
     * instrumentable with {@code CallTag} when the factory gave it a position.
     */
    public abstract static class CallRef extends Ref implements Positions.At {
        /** Set by the factory before the tree is adopted, never after: a constant. */
        @CompilationFinal private int pos;

        /** Set once by the factory, before the tree is adopted. */
        public final void setPos(int pos) {
            this.pos = pos;
        }

        @Override
        public final int pos() {
            return pos;
        }

        @Override
        public final boolean isInstrumentable() {
            return pos > 0;
        }

        @Override
        public final boolean hasTag(Class<? extends Tag> tag) {
            return tag == StandardTags.CallTag.class;
        }

        @Override
        public final SourceSection getSourceSection() {
            return sectionAt(pos);
        }
    }

    /**
     * A statement. The factory gives each statement of the Go source its position (truffle-design
     * §6.2); a positioned statement carries {@code StatementTag}, a function body
     * ({@link Calls.Body}) {@code RootTag} and {@code RootBodyTag}. Wrappers are inserted only
     * while an instrument is attached: an uninstrumented tree is unchanged (playbook §12).
     */
    @GenerateWrapper
    public abstract static class Stmt extends GoNode implements InstrumentableNode, Positions.At {
        /** Set by the factory before the tree is adopted, never after: a constant. */
        @CompilationFinal private int pos;

        public abstract void executeVoid(VirtualFrame frame);

        /** Set once by the factory, before the tree is adopted. */
        public final void setPos(int pos) {
            this.pos = pos;
        }

        @Override
        public final int pos() {
            return pos;
        }

        @Override
        public boolean isInstrumentable() {
            return pos > 0;
        }

        @Override
        public boolean hasTag(Class<? extends Tag> tag) {
            return tag == StandardTags.StatementTag.class;
        }

        @Override
        public SourceSection getSourceSection() {
            return sectionAt(pos);
        }

        @Override
        public WrapperNode createWrapper(ProbeNode probe) {
            return new StmtWrapper(this, probe);
        }
    }

    /**
     * A structured jump (break, continue, goto, return): one preallocated instance per target
     * statement, compared by identity (truffle-design §4.8, rule N6).
     */
    public static final class Jump extends ControlFlowException {
        private static final long serialVersionUID = 1L;
        public static final Jump RETURN = new Jump();
    }

    // ---------------------------------------------------------------- carrier adapters

    /** Unboxes an integer from a generic expression (call results, tuple elements). */
    public static final class AsI64 extends I64 {
        @Child private Expr x;

        public AsI64(Expr x) {
            this.x = x;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return (long) x.executeGeneric(frame);
        }
    }

    public static final class AsF64 extends F64 {
        @Child private Expr x;

        public AsF64(Expr x) {
            this.x = x;
        }

        @Override
        public double executeDouble(VirtualFrame frame) {
            return (double) x.executeGeneric(frame);
        }
    }

    public static final class AsBool extends Bool {
        @Child private Expr x;

        public AsBool(Expr x) {
            this.x = x;
        }

        @Override
        public boolean executeBoolean(VirtualFrame frame) {
            return (boolean) x.executeGeneric(frame);
        }
    }

    public static final class AsRef extends Ref {
        @Child private Expr x;

        public AsRef(Expr x) {
            this.x = x;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return x.executeGeneric(frame);
        }
    }

    /** The scalar-slot encoding of a float: raw double bits. */
    public static final class F64Raw extends I64 {
        @Child private F64 x;

        public F64Raw(F64 x) {
            this.x = x;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return Double.doubleToRawLongBits(x.executeDouble(frame));
        }
    }

    /** The scalar-slot encoding of a bool: 0 or 1. */
    public static final class BoolRaw extends I64 {
        @Child private Bool x;

        public BoolRaw(Bool x) {
            this.x = x;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return x.executeBoolean(frame) ? 1 : 0;
        }
    }

    public static final class RawF64 extends F64 {
        @Child private I64 x;

        public RawF64(I64 x) {
            this.x = x;
        }

        @Override
        public double executeDouble(VirtualFrame frame) {
            return Double.longBitsToDouble(x.executeLong(frame));
        }
    }

    public static final class RawBool extends Bool {
        @Child private I64 x;

        public RawBool(I64 x) {
            this.x = x;
        }

        @Override
        public boolean executeBoolean(VirtualFrame frame) {
            return x.executeLong(frame) != 0;
        }
    }

    // ---------------------------------------------------------------- constants

    public static final class ConstI64 extends I64 {
        private final long v;

        public ConstI64(long v) {
            this.v = v;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return v;
        }
    }

    public static final class ConstF64 extends F64 {
        private final double v;

        public ConstF64(double v) {
            this.v = v;
        }

        @Override
        public double executeDouble(VirtualFrame frame) {
            return v;
        }
    }

    public static final class ConstBool extends Bool {
        private final boolean v;

        public ConstBool(boolean v) {
            this.v = v;
        }

        @Override
        public boolean executeBoolean(VirtualFrame frame) {
            return v;
        }
    }

    /** An immutable reference constant (string, nil, complex, top-level func value). */
    public static final class ConstRef extends Ref {
        private final Object v;

        public ConstRef(Object v) {
            this.v = v;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return v;
        }
    }

    // ---------------------------------------------------------------- locals

    public static final class ReadI64 extends I64 {
        private final int slot;

        public ReadI64(int slot) {
            this.slot = slot;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return frame.getLongStatic(slot);
        }
    }

    public static final class ReadF64 extends F64 {
        private final int slot;

        public ReadF64(int slot) {
            this.slot = slot;
        }

        @Override
        public double executeDouble(VirtualFrame frame) {
            return frame.getDoubleStatic(slot);
        }
    }

    public static final class ReadBool extends Bool {
        private final int slot;

        public ReadBool(int slot) {
            this.slot = slot;
        }

        @Override
        public boolean executeBoolean(VirtualFrame frame) {
            return frame.getBooleanStatic(slot);
        }
    }

    public static final class ReadRef extends Ref {
        private final int slot;

        public ReadRef(int slot) {
            this.slot = slot;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return frame.getObjectStatic(slot);
        }
    }

    public static final class WriteI64 extends Stmt {
        private final int slot;
        @Child private I64 v;

        public WriteI64(int slot, I64 v) {
            this.slot = slot;
            this.v = v;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            frame.setLongStatic(slot, v.executeLong(frame));
        }
    }

    public static final class WriteF64 extends Stmt {
        private final int slot;
        @Child private F64 v;

        public WriteF64(int slot, F64 v) {
            this.slot = slot;
            this.v = v;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            frame.setDoubleStatic(slot, v.executeDouble(frame));
        }
    }

    public static final class WriteBool extends Stmt {
        private final int slot;
        @Child private Bool v;

        public WriteBool(int slot, Bool v) {
            this.slot = slot;
            this.v = v;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            frame.setBooleanStatic(slot, v.executeBoolean(frame));
        }
    }

    public static final class WriteRef extends Stmt {
        private final int slot;
        @Child private Expr v;

        public WriteRef(int slot, Expr v) {
            this.slot = slot;
            this.v = v;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            frame.setObjectStatic(slot, v.executeGeneric(frame));
        }
    }

    /**
     * Clears the reference slots of locals that are dead from here on, so a long-lived frame
     * does not keep their objects alive (gc's liveness maps; the factory's dead-slot analysis).
     * In compiled code the stores fold into the virtual frame.
     */
    public static final class ClearRefs extends Stmt {
        @CompilationFinal(dimensions = 1) private final int[] slots;

        public ClearRefs(int[] slots) {
            this.slots = slots;
        }

        @Override
        @ExplodeLoop
        public void executeVoid(VirtualFrame frame) {
            for (int slot : slots) {
                frame.clearObjectStatic(slot);
            }
        }
    }

    /** Writes a value to a long slot and yields it (loop conditions that advance an iterator). */
    public static final class TeeI64 extends I64 {
        private final int slot;
        @Child private I64 v;

        public TeeI64(int slot, I64 v) {
            this.slot = slot;
            this.v = v;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            long x = v.executeLong(frame);
            frame.setLongStatic(slot, x);
            return x;
        }
    }

    /** The i-th element of the frame's argument array (0 is the closure environment). */
    public static final class Arg extends Ref {
        private final int index;

        public Arg(int index) {
            this.index = index;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return frame.getArguments()[index];
        }
    }

    /** An expression evaluated for its effects. */
    public static final class ExprStmt extends Stmt {
        @Child private Expr x;

        public ExprStmt(Expr x) {
            this.x = x;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            x.executeGeneric(frame);
        }
    }

    /** Statements, then a value: multi-value spreads and other spills inside expressions. */
    public static final class Seq extends Ref {
        @Children private final Stmt[] pre;
        @Child private Expr value;

        public Seq(Stmt[] pre, Expr value) {
            this.pre = pre;
            this.value = value;
        }

        @Override
        @com.oracle.truffle.api.nodes.ExplodeLoop
        public Object executeGeneric(VirtualFrame frame) {
            for (Stmt s : pre) {
                s.executeVoid(frame);
            }
            return value.executeGeneric(frame);
        }
    }

    static RuntimeException unreachable() {
        throw CompilerDirectives.shouldNotReachHere();
    }
}
