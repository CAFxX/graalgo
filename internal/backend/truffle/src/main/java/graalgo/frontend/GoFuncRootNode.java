package graalgo.frontend;

import com.oracle.truffle.api.TruffleLanguage;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.FrameSlotTypeException;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RootNode;
import com.oracle.truffle.api.source.SourceSection;
import graalgo.runtime.DeferChain;
import graalgo.runtime.DeferredCall;
import graalgo.runtime.Goexit;
import graalgo.runtime.GoroutineState;
import graalgo.runtime.PanicException;
import graalgo.safepoint.Safepoint;
import java.util.List;
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
 * <p>Per-iteration capture (loopcap): when the literal captures boxed (loop)
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
    // P3-M9 (WS D): an address-taken named result's slot holds a GoBox
    // cell; readResult unwraps it.
    private final boolean resultBoxed;
    private final String funcName;
    private SourceSection sourceSection;
    /**
     * Snapshot slots for captured boxed variables (loopcap); parallel to
     * {@link GoFunc#boxCells()}. Null when the literal captures none.
     */
    private final int[] snapSlots;
    /**
     * PKG-039: frame slot holding this function's range-over-func defer
     * stash, or -1 when the function contains no range-over-func (see
     * {@link RangeFuncNode}).
     */
    private final int rangeStashSlot;

    public GoFuncRootNode(FrameDescriptor descriptor, GoStmtNode body,
                          int closureSlot, int[] paramSlots, GoKind[] paramKinds,
                          int resultSlot, GoKind resultKind, String funcName) {
        this(descriptor, body, closureSlot, paramSlots, paramKinds, resultSlot, resultKind, funcName, null);
    }

    public GoFuncRootNode(FrameDescriptor descriptor, GoStmtNode body,
                          int closureSlot, int[] paramSlots, GoKind[] paramKinds,
                          int resultSlot, GoKind resultKind, String funcName, int[] snapSlots) {
        this(descriptor, body, closureSlot, paramSlots, paramKinds, resultSlot, resultKind,
                funcName, snapSlots, false);
    }

    public GoFuncRootNode(FrameDescriptor descriptor, GoStmtNode body,
                          int closureSlot, int[] paramSlots, GoKind[] paramKinds,
                          int resultSlot, GoKind resultKind, String funcName, int[] snapSlots,
                          boolean resultBoxed) {
        this(descriptor, body, closureSlot, paramSlots, paramKinds, resultSlot, resultKind,
                funcName, snapSlots, resultBoxed, -1);
    }

    /**
     * Full constructor.
     *
     * @param rangeStashSlot PKG-039: frame slot holding this function's
     *                       range-over-func defer stash (a
     *                       {@code List<DeferredCall>}), or -1 when the
     *                       function contains no range-over-func.
     */
    public GoFuncRootNode(FrameDescriptor descriptor, GoStmtNode body,
                          int closureSlot, int[] paramSlots, GoKind[] paramKinds,
                          int resultSlot, GoKind resultKind, String funcName, int[] snapSlots,
                          boolean resultBoxed, int rangeStashSlot) {
        super((TruffleLanguage<?>) null, descriptor);
        this.body = body;
        this.closureSlot = closureSlot;
        this.paramSlots = paramSlots;
        this.paramKinds = paramKinds;
        this.resultSlot = resultSlot;
        this.resultKind = resultKind;
        this.resultBoxed = resultBoxed;
        this.funcName = funcName;
        this.snapSlots = snapSlots;
        this.rangeStashSlot = rangeStashSlot;
    }

    /**
     * The parameter kinds, parallel to the call argument array (P3-M4, WS
     * C): documents the per-parameter boxing contract used by
     * {@link #execute} — interface parameters keep {@code GoInterface}
     * values, concrete parameters carry D5 values (unwrapped from any
     * interface boxing by the caller path). The returned array is a copy.
     */
    public GoKind[] paramKinds() {
        return paramKinds.clone();
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
            // P3-M4 (WS C): concrete params unwrap interface-boxed values.
            // The linkname intrinsic→Go bridge (Linknames.callGo) forwards
            // call-site arguments, which the call adaptation boxes for
            // any-typed parameters; a direct Go call would pass D5 values,
            // so the root normalizes here, next to the interface-param
            // wrap above. A nil interface for a concrete parameter is a
            // caller type error and fails fast (never a silent null slot).
            if (paramKinds[i] != graalgo.values.GoKind.INTERFACE
                    && arg instanceof graalgo.values.GoInterface gi) {
                Object data = gi.data();
                if (data == null) {
                    throw new graalgo.frontend.FrontendException(
                            "linkname: nil interface value for concrete parameter "
                                    + i + " of " + funcName);
                }
                arg = data;
            }
            // P3-M4 remediation (P2-5): assert the caller contract. After
            // the unwrap above, a concrete parameter must carry a D5
            // value, never an interface box: the linkname intrinsic→Go
            // bridge boxes any-typed args as GoInterface (unwrapped
            // here), while a direct Go call passes D5 values. If this
            // fires, the caller path violated the contract — fail fast,
            // never silently misbox. (Checked-in coverage:
            // LinknameEndToEndTest.testCallGoEndToEndPrints42 drives the
            // bridge with an any-typed arg for a concrete param.)
            if (paramKinds[i] != graalgo.values.GoKind.INTERFACE
                    && arg instanceof graalgo.values.GoInterface) {
                throw new AssertionError("GoFuncRootNode(" + funcName
                        + "): concrete param " + i
                        + " still interface-boxed after unwrap (caller contract violated)");
            }
            // N-P1-3 (rev30-iface-valuecopy): THE value-copy choke
            // point. By-value STRUCT/ARRAY parameters are deep-copied
            // here, on every invocation, for every call path — direct
            // calls (which also copy at the call site; the second copy
            // is redundant but harmless), interface dispatch (the
            // receiver travels as params[0] and struct/array args as
            // params[1..]), method values (bound args are prepended
            // per invocation by GoCalls, so a bind-time copy would
            // still leak across repeated invocations — copying here
            // gives each invocation its own value, like gc), and
            // reflect/linkname calls into Go roots. INTERFACE params
            // are deliberately NOT copied here: passing an interface
            // value shares its dynamic data in gc (the copy happens at
            // conversion/box time, in Boxing.copyForAssign); only a
            // value-receiver method's STRUCT params[0] needs the fresh
            // copy, and it gets one by this rule. Pointer-kind params
            // (`*atomic.T`, `*T`) carry the same object and must NOT
            // be cloned, or pointer aliasing breaks (`setIt(p)`
            // mutating through p). Atomic cells are Go values: a
            // by-value (STRUCT-kind) parameter gets its own cell
            // (clone), so `f(a)` cannot observe or affect the
            // caller's cell — like gc (P3-M4 P0-1).
            if (paramKinds[i] == graalgo.values.GoKind.STRUCT) {
                if (arg instanceof graalgo.values.GoStruct gs) {
                    arg = gs.deepCopy();
                } else if (arg instanceof graalgo.runtime.sync.GoAtomicInt64 gi64) {
                    // fix-linkname: match the GoAtomic wrappers before
                    // the raw AtomicLong/Integer (they extend them) so
                    // the clone keeps the LongCell/IntCell interface.
                    arg = new graalgo.runtime.sync.GoAtomicInt64(gi64.get());
                } else if (arg instanceof graalgo.runtime.sync.GoAtomicInt32 gi32) {
                    arg = new graalgo.runtime.sync.GoAtomicInt32(gi32.get());
                } else if (arg instanceof java.util.concurrent.atomic.AtomicLong al) {
                    arg = new java.util.concurrent.atomic.AtomicLong(al.get());
                } else if (arg instanceof java.util.concurrent.atomic.AtomicInteger ai) {
                    arg = new java.util.concurrent.atomic.AtomicInteger(ai.get());
                }
            } else if (paramKinds[i] == graalgo.values.GoKind.ARRAY
                    && arg != null && arg.getClass().isArray()) {
                arg = graalgo.values.GoStruct.deepCopyArray(arg);
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
                    adoptRangeDefers(chain, frame, mark);
                    chain.unwindReturning(mark);
                }
                return readResult(frame, resultSlot, resultKind, r.value, resultBoxed);
            }
            if (chain != null) {
                adoptRangeDefers(chain, frame, mark);
                chain.unwindReturning(mark);
            }
            return readResult(frame, resultSlot, resultKind, null, resultBoxed);
        } catch (PanicException p) {
            PanicException rest;
            if (chain != null) {
                adoptRangeDefers(chain, frame, mark);
                rest = chain.unwindPanicking(mark, p);
            } else {
                rest = p;
            }
            if (rest != null) {
                throw rest;
            }
            // Recovered: resume after the chain with the result slot.
            return readResult(frame, resultSlot, resultKind, null, resultBoxed);
        } catch (Goexit g) {
            if (chain != null) {
                adoptRangeDefers(chain, frame, mark);
                chain.unwindGoexiting(mark);
            }
            throw g;
        } finally {
            if (state != null && head != null) {
                state.popGoStack(head.frame);
            }
        }
    }

    /**
     * PKG-039: merges this function's range-over-func defer stash back
     * into the chain before unwinding (gc's {@code deferrangefunc}).
     * No-op when the function contains no range-over-func or no body
     * defer was stashed.
     */
    private void adoptRangeDefers(DeferChain chain, VirtualFrame frame, int mark) {
        if (rangeStashSlot < 0) {
            return;
        }
        Object cur;
        try {
            cur = frame.getObject(rangeStashSlot);
        } catch (FrameSlotTypeException e) {
            return; // stash slot never initialized: no range executed
        }
        if (cur instanceof List<?> stash) {
            @SuppressWarnings("unchecked")
            List<DeferredCall> calls = (List<DeferredCall>) stash;
            chain.adoptRangeDefers(mark, calls);
        }
    }

    /** Result value: the result slot if present, else the GoReturn value, else null. */
    private static Object readResult(VirtualFrame frame, int resultSlot, GoKind resultKind,
                                     Object returnValue, boolean resultBoxed) {
        if (resultSlot >= 0) {
            if (resultBoxed) {
                // The slot holds the named result's GoBox cell; its value
                // is already in canonical boxed form.
                return ((graalgo.values.GoBox) frame.getObject(resultSlot)).value;
            }
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
     * Infers the basic descriptor from the Java type. Package-visible for
     * {@link LinknameRootNode} (P3-M4): a linkname target returning a
     * declared interface type boxes its D5 result the same way.
     *
     * <p>P3-M7R2: uses the name-keyed canonical descriptors (see
     * {@code NodeFactory.basicDescriptorForName}) — the kind-keyed cache
     * is gone, and every path must mint the identical instance for one
     * Go type or identity comparisons fail ("is int64, not int64").
     * The Java carrier -> Go name mapping is conventional: a D5
     * {@code Long} is the 64-bit carrier shared by Go {@code int} and
     * {@code int64}; "int64" is used (matching the pre-P3-M7 observable
     * behavior on the linkname path). Callers that know the exact Go
     * type must box via the type table instead.
     */
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
            // Unknown: use the arg's class name; the type switch will not
            // match, which is acceptable for v1.
            kind = graalgo.values.GoKind.INTERFACE;
            goName = "interface";
        }
        graalgo.values.GoTypeDescriptor desc =
                graalgo.frontend.NodeFactory.basicDescriptorForName(goName, kind);
        return new graalgo.values.GoInterface(desc, arg, null);
    }
}
