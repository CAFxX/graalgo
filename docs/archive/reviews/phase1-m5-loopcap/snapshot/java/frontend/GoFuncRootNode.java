package graalgo.frontend;

import com.oracle.truffle.api.TruffleLanguage;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RootNode;
import com.oracle.truffle.api.source.SourceSection;
import graalgo.runtime.DeferChain;
import graalgo.runtime.Goexit;
import graalgo.runtime.GoroutineState;
import graalgo.runtime.PanicException;
import graalgo.safepoint.Safepoint;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;

/**
 * Root node for a compiled function literal (M5, WS F).
 *
 * <p>Calling convention: {@code args[0]} is the {@link GoFunc} (its
 * {@code closureFrame()} is installed in the {@code $closure} frame slot
 * for {@link CapReadNode}/{@link CapWriteNode}); {@code args[1..]} are the
 * call arguments, stored into the parameter slots.
 *
 * <p>Per-iteration capture (M6): when the literal captures boxed (loop)
 * variables, {@code args[0]}'s {@code boxCells()} are installed into the
 * {@code snapSlots} frame slots on entry (parallel arrays; null when the
 * literal captures no boxed variables). Nested literals resolve boxed
 * captures to those snapshot slots and re-snapshot them at their own
 * creation.
 *
 * <p>On entry the node's defer watermark is recorded; on exit the
 * {@link DeferChain} drains to the watermark:
 * <ul>
 *   <li>normal return ({@link GoReturn}) → run defers, then read the
 *       result slot (defer runs before the return completes, so deferred
 *       calls see and may modify named results);
 *   <li>panic → run defers via unwind; rethrow unless recovered (recovery
 *       resumes after the chain, returning the result slot);
 *   <li>goexit → run defers, rethrow.
 * </ul>
 */
public final class GoFuncRootNode extends RootNode {
    @Child private GoStmtNode body;
    private final int closureSlot;
    private final int[] paramSlots;
    private final GoKind[] paramKinds;
    private final int resultSlot;
    private final GoKind resultKind;
    private final String funcName;
    private SourceSection sourceSection;
    /**
     * Snapshot slots for captured boxed variables (M6); parallel to
     * {@link GoFunc#boxCells()}. Null when the literal captures none.
     */
    private final int[] snapSlots;

    public GoFuncRootNode(FrameDescriptor descriptor, GoStmtNode body,
                          int closureSlot, int[] paramSlots, GoKind[] paramKinds,
                          int resultSlot, GoKind resultKind, String funcName) {
        this(descriptor, body, closureSlot, paramSlots, paramKinds, resultSlot, resultKind, funcName, null);
    }

    public GoFuncRootNode(FrameDescriptor descriptor, GoStmtNode body,
                          int closureSlot, int[] paramSlots, GoKind[] paramKinds,
                          int resultSlot, GoKind resultKind, String funcName, int[] snapSlots) {
        super((TruffleLanguage<?>) null, descriptor);
        this.body = body;
        this.closureSlot = closureSlot;
        this.paramSlots = paramSlots;
        this.paramKinds = paramKinds;
        this.resultSlot = resultSlot;
        this.resultKind = resultKind;
        this.funcName = funcName;
        this.snapSlots = snapSlots;
    }

    @Override
    public Object execute(VirtualFrame frame) {
        Safepoint.poll();
        Object[] args = frame.getArguments();
        GoFunc fn = (GoFunc) args[0];
        frame.setObject(closureSlot, fn.closureFrame());
        if (snapSlots != null) {
            graalgo.values.GoBox[] cells = fn.boxCells();
            if (cells == null || cells.length != snapSlots.length) {
                throw new IllegalStateException("GoFuncRootNode(" + funcName
                        + "): snapshot mismatch: root expects " + snapSlots.length
                        + " boxed cells, GoFunc has " + (cells == null ? "none" : cells.length));
            }
            for (int i = 0; i < snapSlots.length; i++) {
                frame.setObject(snapSlots[i], cells[i]);
            }
        }
        for (int i = 0; i < paramSlots.length; i++) {
            Object arg = args[i + 1];
            // M5 (WS F): interface params wrap concrete values. If the arg
            // is not already a GoInterface, wrap it with a basic descriptor
            // inferred from the Java type.
            if (paramKinds[i] == graalgo.values.GoKind.INTERFACE
                    && arg != null && !(arg instanceof graalgo.values.GoInterface)) {
                arg = wrapInterface(arg);
            }
            Boxing.setSlot(frame, paramSlots[i], paramKinds[i], arg);
        }
        GoroutineState state = GoroutineState.current();
        DeferChain chain = state != null ? state.deferChain() : null;
        int mark = chain != null ? chain.depth() : 0;
        GoStackFrameHead head = null;
        if (state != null) {
            head = new GoStackFrameHead(state.pushGoStack(funcName, getSourceSection()));
        }
        try {
            try {
                body.executeVoid(frame);
            } catch (GoReturn r) {
                if (chain != null) {
                    chain.unwindReturning(mark);
                }
                return readResult(frame, resultSlot, resultKind, r.value);
            }
            if (chain != null) {
                chain.unwindReturning(mark);
            }
            return readResult(frame, resultSlot, resultKind, null);
        } catch (PanicException p) {
            PanicException rest = chain != null ? chain.unwindPanicking(mark, p) : p;
            if (rest != null) {
                throw rest;
            }
            // Recovered: resume after the chain with the result slot.
            return readResult(frame, resultSlot, resultKind, null);
        } catch (Goexit g) {
            if (chain != null) {
                chain.unwindGoexiting(mark);
            }
            throw g;
        } finally {
            if (state != null && head != null) {
                state.popGoStack(head.frame);
            }
        }
    }

    /** Result value: the result slot if present, else the GoReturn value, else null. */
    private static Object readResult(VirtualFrame frame, int resultSlot, GoKind resultKind,
                                     Object returnValue) {
        if (resultSlot >= 0) {
            return Boxing.readSlot(frame, resultSlot, resultKind);
        }
        return returnValue;
    }

    private static final class GoStackFrameHead {
        final graalgo.runtime.GoStackFrame frame;
        GoStackFrameHead(graalgo.runtime.GoStackFrame frame) {
            this.frame = frame;
        }
    }

    public GoStmtNode getBody() {
        return body;
    }

    public void setSourceSection(SourceSection section) {
        this.sourceSection = section;
    }

    @Override
    public SourceSection getSourceSection() {
        return sourceSection;
    }

    @Override
    public String toString() {
        return "GoFuncRootNode(" + funcName + ")";
    }

    /**
     * Wraps a concrete value in a GoInterface for interface params (M5, WS F).
     * Infers the basic descriptor from the Java type.
     */
    private static graalgo.values.GoInterface wrapInterface(Object arg) {
        graalgo.values.GoKind kind;
        if (arg instanceof Long) {
            kind = graalgo.values.GoKind.INT64;
        } else if (arg instanceof Integer) {
            kind = graalgo.values.GoKind.INT32;
        } else if (arg instanceof Double) {
            kind = graalgo.values.GoKind.FLOAT64;
        } else if (arg instanceof Float) {
            kind = graalgo.values.GoKind.FLOAT32;
        } else if (arg instanceof Boolean) {
            kind = graalgo.values.GoKind.BOOL;
        } else if (arg instanceof graalgo.values.GoString) {
            kind = graalgo.values.GoKind.STRING;
        } else {
            // Unknown: use the arg's class name; the type switch will not
            // match, which is acceptable for v1.
            kind = graalgo.values.GoKind.INTERFACE;
        }
        graalgo.values.GoTypeDescriptor desc =
                graalgo.frontend.NodeFactory.basicDescriptorFor(kind);
        return new graalgo.values.GoInterface(desc, arg, null);
    }
}
