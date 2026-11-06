package graalgo.frontend.v2;

import com.oracle.truffle.api.TruffleLanguage;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;
import com.oracle.truffle.api.source.SourceSection;
import graalgo.frontend.Boxing;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoReturn;
import graalgo.frontend.GoStmtNode;
import graalgo.runtime.Goexit;
import graalgo.runtime.GoroutineState;
import graalgo.safepoint.Safepoint;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import graalgo.values.GoTuple;

/**
 * Root node for a compiled v2 function (V12-P1-2).
 *
 * <p>Calling convention (shared with {@code GoFuncRootNode}): the call
 * target is invoked with the {@link GoFunc} itself as {@code args[0]}
 * (its {@code closureFrame()} is installed in the {@code $closure} frame
 * slot), followed by the call arguments, stored into the parameter slots.
 * The parameter install loop is identical to {@code GoFuncRootNode}'s
 * (interface wrap for {@code any} parameters, interface unwrap for
 * concrete parameters, by-value struct handling): the calling convention
 * is shared, and divergence here would be a bug farm.
 *
 * <p>Results: unlike v1 (at most one result slot), a v2 function returns
 * its values as one carrier object. The body signals return by throwing
 * {@link GoReturn}:
 * <ul>
 *   <li>0 results (void): the return value must be null; anything else is
 *       a factory contract violation and fails loudly.
 *   <li>1 result: the carried value (a bare {@code return} or falling off
 *       the end yields the zero value of the result kind, per Go).
 *   <li>N &gt; 1 results: the carried value must be a {@link GoTuple} of
 *       exactly N values; a bare {@code return} or falling off the end
 *       yields the zero tuple. Any other shape fails loudly.
 * </ul>
 * Zero synthesis uses {@link Boxing#zero}: exact for scalars, strings,
 * complex, and nil-kind results. Struct and array results have no zero
 * template in v2 yet (that needs the struct-descriptor plumbing of
 * V12-P0-1), so a bare return / fall-off from a function returning one
 * fails loudly instead of silently producing a null.
 *
 * <p>No defer handling: v2 has no {@code defer} yet, so there is no
 * {@code DeferChain} to unwind. A {@link PanicException} propagates
 * untouched to the caller's root (which owns the chain); when v2 gains
 * defer, this root must adopt the unwind protocol. {@link Goexit} is
 * likewise rethrown.
 */
public final class V2FuncRootNode extends RootNode {
    @Child private GoStmtNode body;
    private final int closureSlot;
    private final int[] paramSlots;
    private final GoKind[] paramKinds;
    private final GoKind[] resultKinds;
    private final String funcName;
    private SourceSection sourceSection;

    public V2FuncRootNode(FrameDescriptor descriptor, GoStmtNode body,
                          int closureSlot, int[] paramSlots, GoKind[] paramKinds,
                          GoKind[] resultKinds, String funcName) {
        super((TruffleLanguage<?>) null, descriptor);
        this.body = body;
        this.closureSlot = closureSlot;
        this.paramSlots = paramSlots;
        this.paramKinds = paramKinds;
        this.resultKinds = resultKinds;
        this.funcName = funcName;
    }

    @Override
    public Object execute(VirtualFrame frame) {
        Safepoint.poll();
        Object[] args = frame.getArguments();
        GoFunc fn = (GoFunc) args[0];
        frame.setObject(closureSlot, fn.closureFrame());
        for (int i = 0; i < paramSlots.length; i++) {
            Object arg = args[i + 1];
            // Interface params wrap concrete values; concrete params
            // unwrap interface-boxed values (GoFuncRootNode's contract,
            // mirrored exactly).
            if (paramKinds[i] == graalgo.values.GoKind.INTERFACE
                    && arg != null && !(arg instanceof graalgo.values.GoInterface)) {
                arg = wrapInterface(arg);
            }
            if (paramKinds[i] != graalgo.values.GoKind.INTERFACE
                    && arg instanceof graalgo.values.GoInterface gi) {
                Object data = gi.data();
                if (data == null) {
                    throw new FrontendException(
                            "linkname: nil interface value for concrete parameter "
                                    + i + " of " + funcName);
                }
                arg = data;
            }
            if (paramKinds[i] != graalgo.values.GoKind.INTERFACE
                    && arg instanceof graalgo.values.GoInterface) {
                throw new AssertionError("V2FuncRootNode(" + funcName
                        + "): concrete param " + i
                        + " still interface-boxed after unwrap (caller contract violated)");
            }
            if (paramKinds[i] == graalgo.values.GoKind.STRUCT) {
                if (arg instanceof graalgo.runtime.sync.GoAtomicInt64 gi64) {
                    arg = new graalgo.runtime.sync.GoAtomicInt64(gi64.get());
                } else if (arg instanceof graalgo.runtime.sync.GoAtomicInt32 gi32) {
                    arg = new graalgo.runtime.sync.GoAtomicInt32(gi32.get());
                } else if (arg instanceof java.util.concurrent.atomic.AtomicLong al) {
                    arg = new java.util.concurrent.atomic.AtomicLong(al.get());
                } else if (arg instanceof java.util.concurrent.atomic.AtomicInteger ai) {
                    arg = new java.util.concurrent.atomic.AtomicInteger(ai.get());
                }
            }
            Boxing.setSlot(frame, paramSlots[i], paramKinds[i], arg);
        }
        GoroutineState state = GoroutineState.current();
        GoStackFrameHead head = null;
        if (state != null) {
            head = new GoStackFrameHead(state.pushGoStack(funcName, getSourceSection()));
        }
        try {
            try {
                body.executeVoid(frame);
            } catch (GoReturn r) {
                return readResult(r.value);
            }
            return readResult(null);
        } catch (Goexit g) {
            throw g;
        } finally {
            if (state != null && head != null) {
                state.popGoStack(head.frame);
            }
        }
    }

    /**
     * Result value from a {@link GoReturn} payload (or null for falling off
     * the end): null means "no value given", which Go defines as the zero
     * values; a tuple payload must match the result arity exactly.
     */
    private Object readResult(Object returnValue) {
        if (resultKinds.length == 0) {
            if (returnValue != null) {
                throw new FrontendException("V2FuncRootNode(" + funcName
                        + "): void function returned a value");
            }
            return null;
        }
        if (returnValue == null) {
            return zeroResult();
        }
        if (resultKinds.length == 1) {
            if (returnValue instanceof GoTuple) {
                throw new FrontendException("V2FuncRootNode(" + funcName
                        + "): single-result function returned a tuple");
            }
            return returnValue;
        }
        if (!(returnValue instanceof GoTuple t)) {
            throw new FrontendException("V2FuncRootNode(" + funcName
                    + "): multi-result function returned a non-tuple "
                    + returnValue.getClass().getSimpleName());
        }
        if (t.size() != resultKinds.length) {
            throw new FrontendException("V2FuncRootNode(" + funcName
                    + "): multi-result function returned a tuple of "
                    + t.size() + " values, want " + resultKinds.length);
        }
        return t;
    }

    /** Zero values for every result kind (Go's bare-return / fall-off rule). */
    private Object zeroResult() {
        if (resultKinds.length == 1) {
            return zeroOf(resultKinds[0]);
        }
        Object[] zeros = new Object[resultKinds.length];
        for (int i = 0; i < resultKinds.length; i++) {
            zeros[i] = zeroOf(resultKinds[i]);
        }
        return new GoTuple(zeros);
    }

    private Object zeroOf(GoKind kind) {
        if (kind == GoKind.STRUCT || kind == GoKind.ARRAY) {
            throw new FrontendException("V2FuncRootNode(" + funcName
                    + "): cannot synthesize the zero value for a " + kind
                    + " result (needs the struct-descriptor plumbing of V12-P0-1); "
                    + "return an explicit value instead");
        }
        return Boxing.zero(kind, null);
    }

    private static final class GoStackFrameHead {
        final graalgo.runtime.GoStackFrame frame;
        GoStackFrameHead(graalgo.runtime.GoStackFrame frame) {
            this.frame = frame;
        }
    }

    /** Test/introspection aid: lets a test walk the built tree. */
    public GoStmtNode getBody() {
        return body;
    }

    /** Sets this node's source section (Truffle 25.x removed Node.setSourceSection). */
    public void setSourceSection(SourceSection section) {
        this.sourceSection = section;
    }

    @Override
    public SourceSection getSourceSection() {
        return sourceSection;
    }

    /** Wraps a concrete value in a GoInterface (GoFuncRootNode's helper, mirrored). */
    static graalgo.values.GoInterface wrapInterface(Object arg) {
        graalgo.values.GoKind kind;
        String goName;
        if (arg instanceof Long) {
            kind = graalgo.values.GoKind.INT64;
            goName = "int64";
        } else if (arg instanceof Integer) {
            kind = graalgo.values.GoKind.INT32;
            goName = "int32";
        } else if (arg instanceof Double) {
            kind = graalgo.values.GoKind.FLOAT64;
            goName = "float64";
        } else if (arg instanceof Float) {
            kind = graalgo.values.GoKind.FLOAT32;
            goName = "float32";
        } else if (arg instanceof Boolean) {
            kind = graalgo.values.GoKind.BOOL;
            goName = "bool";
        } else if (arg instanceof graalgo.values.GoString) {
            kind = graalgo.values.GoKind.STRING;
            goName = "string";
        } else {
            kind = graalgo.values.GoKind.INTERFACE;
            goName = "interface";
        }
        graalgo.values.GoTypeDescriptor desc =
                graalgo.frontend.NodeFactory.basicDescriptorForName(goName, kind);
        return new graalgo.values.GoInterface(desc, arg, null);
    }
}
