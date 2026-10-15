package graalgo.iface;

import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;

import graalgo.frontend.FrontendException;
import graalgo.runtime.Fmt;
import graalgo.runtime.FmtScan;
import graalgo.runtime.GoCalls;
import graalgo.runtime.GoErrors;
import graalgo.runtime.GoroutineState;
import graalgo.runtime.Panic;
import graalgo.values.GoFunc;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import graalgo.values.GoTypeDescriptor;

/**
 * The {@link Fmt.GoMethodDispatch} implementation (P3-M10, fix-fmt-funcs):
 * Go method dispatch for the {@code fmt} scan and format families, installed
 * by the phase-3 launcher next to {@link FmtHandleMethods#install}.
 * (The F* print family is desugared at the frontend and needs no dispatch.)
 *
 * <p>Also installs the Java-implemented {@code fmt.ScanState} methods for
 * the synthetic {@code internal/graalgo/fmtscan.ScanStateImpl} descriptor
 * (the {@link FmtHandleMethods} PrinterState pattern): custom
 * {@code fmt.Scanner} implementations receive a ScanState value whose
 * methods delegate to the thread-local {@link FmtScan#currentScan}.
 */
public final class FmtMethodDispatch implements Fmt.GoMethodDispatch {
    /** Descriptor name of the synthetic ScanState implementation. */
    private static final String SCAN_STATE_IMPL_DESC = "internal/graalgo/fmtscan.ScanStateImpl";
    /** Descriptor name of the fmt.ScanState interface. */
    private static final String FMT_SCANSTATE_DESC = "fmt.ScanState";

    /** Installs the dispatcher and the ScanState methods. */
    public static void install() {
        Fmt.setGoMethodDispatch(new FmtMethodDispatch());
        installScanStateMethods();
    }

    // ------------------------------------------------------------------
    // Fmt.GoMethodDispatch.
    // ------------------------------------------------------------------

    @Override
    public Object call(Fmt.Arg arg, String method, Object[] args) {
        Object[] resolved = resolveReceiver(arg);
        String descName = (String) resolved[0];
        Object receiver = resolved[1];
        IfaceMethodTarget target = IfaceMethodTable.lookup(descName, method);
        if (target == null || !target.supported()) {
            throw new FrontendException("fmt: method \"" + method
                    + "\" not found for dynamic type \"" + descName + "\"");
        }
        Object[] callArgs = adaptReceiver(target, receiver, args);
        if (callArgs == null) {
            throw new FrontendException("fmt: cannot adapt receiver for \"" + method
                    + "\" on \"" + descName + "\" (promoted method)");
        }
        // IfaceDispatchNode's recoverAllowed dance: recover() is only
        // non-nil when called directly by a deferred function.
        GoroutineState state = GoroutineState.current();
        boolean prev = state != null && state.isRecoverAllowed();
        if (state != null) {
            state.setRecoverAllowed(false);
        }
        try {
            return GoCalls.invoke(target.func(), callArgs);
        } finally {
            if (state != null) {
                state.setRecoverAllowed(prev);
            }
        }
    }

    @Override
    public boolean hasMethod(Fmt.Arg arg, String method) {
        // resolveReceiver panics on a nil interface (gc's nil dereference);
        // that propagates — only a genuinely absent method returns false.
        Object[] resolved = resolveReceiver(arg);
        IfaceMethodTarget target = IfaceMethodTable.lookup((String) resolved[0], method);
        return target != null && target.supported();
    }

    @Override
    public Object scanStateValue() {
        GoStructDescriptor desc = new GoStructDescriptor(SCAN_STATE_IMPL_DESC, java.util.List.of());
        GoStruct data = new GoStruct(desc, new long[0], new Object[0]);
        GoTypeDescriptor typeDesc = new GoTypeDescriptor(SCAN_STATE_IMPL_DESC, GoKind.STRUCT, false);
        Object itab = null;
        try {
            itab = IfaceMethodTable.itabForIntrinsic(SCAN_STATE_IMPL_DESC, FMT_SCANSTATE_DESC);
        } catch (Exception e) {
            // Leave null: the Scan call still dispatches by method name.
        }
        return new GoInterface(typeDesc, data, itab);
    }

    @Override
    public boolean isScanStateValue(Object value) {
        if (value instanceof GoInterface gi) {
            Object desc = gi.descriptor();
            if (desc instanceof GoTypeDescriptor td) {
                return SCAN_STATE_IMPL_DESC.equals(td.name());
            }
        }
        return false;
    }

    /**
     * Resolves (descriptor name, receiver value) from a {@link Fmt.Arg},
     * mirroring {@link FmtHandleMethods#handle}: interfaces unwrap to the
     * dynamic type; concrete values use the static type name. A nil
     * interface is gc's nil-dereference panic.
     */
    private static Object[] resolveReceiver(Fmt.Arg arg) {
        Object v = arg.value();
        GoKind k = arg.kind();
        if (k == GoKind.INTERFACE && v instanceof GoInterface gi) {
            if (gi.isNil()) {
                Panic.throwPanic(GoErrors.nilDereference(), null, null, null);
                throw new AssertionError("unreachable: Panic.throwPanic returned");
            }
            Object data = gi.data();
            if (data == null) {
                // Typed nil: the method runs with a null receiver, like gc.
                return new Object[]{((GoTypeDescriptor) gi.descriptor()).name(), null};
            }
            return new Object[]{((GoTypeDescriptor) gi.descriptor()).name(), data};
        }
        if (k == GoKind.INTERFACE) {
            // Java null: nil interface value.
            Panic.throwPanic(GoErrors.nilDereference(), null, null, null);
            throw new AssertionError("unreachable: Panic.throwPanic returned");
        }
        String descName = arg.typeName();
        if (descName == null || v == null) {
            Panic.throwPanic(GoErrors.nilDereference(), null, null, null);
            throw new AssertionError("unreachable: Panic.throwPanic returned");
        }
        return new Object[]{descName, v};
    }

    /**
     * Adapts the receiver for the target (handles pointerReceiver,
     * addrOf). Mirrors {@link FmtHandleMethods} (private there): pointer
     * receivers keep the pointer; value receivers dereference; addrOf
     * takes the address. Returns null for promoted (non-direct) methods.
     */
    private static Object[] adaptReceiver(IfaceMethodTarget target, Object receiver, Object[] args) {
        Object recv = receiver;
        if (!target.isDirect()) {
            return null;
        }
        if (recv instanceof GoPointer gp && !target.pointerReceiver()) {
            recv = gp.referent();
            if (recv == null) {
                return null;
            }
        }
        if (target.addrOf() && !(recv instanceof GoPointer)) {
            recv = new GoPointer(recv);
        }
        Object[] callArgs = new Object[args.length + 1];
        callArgs[0] = recv;
        System.arraycopy(args, 0, callArgs, 1, args.length);
        return callArgs;
    }

    // ------------------------------------------------------------------
    // Java-implemented fmt.ScanState methods.
    // ------------------------------------------------------------------

    /** Functional interface for ScanState method implementations. */
    @FunctionalInterface
    private interface ScanStateMethod {
        Object invoke(Object[] args);
    }

    private static void installScanStateMethod(String name, ScanStateMethod impl) {
        RootNode root = new RootNode(null) {
            @Override
            public Object execute(VirtualFrame frame) {
                Object[] args = frame.getArguments();
                // args[0] is the GoFunc, args[1] is receiver, args[2..] are params.
                Object[] callArgs = new Object[args.length - 1];
                System.arraycopy(args, 1, callArgs, 0, callArgs.length);
                return impl.invoke(callArgs);
            }
        };
        RootCallTarget target = root.getCallTarget();
        GoFunc func = new GoFunc(target, null);
        IfaceMethodTarget methodTarget = new IfaceMethodTarget(func, new int[0], false, false, -1, null);
        IfaceMethodTable.installMethod(SCAN_STATE_IMPL_DESC, name, methodTarget);
    }

    private static FmtScan.ScanState current() {
        FmtScan.ScanState s = FmtScan.currentScan.get();
        if (s == null) {
            throw new IllegalStateException("fmtscan: no current scan state");
        }
        return s;
    }

    private static void installScanStateMethods() {
        // ReadRune() (rune, size, error)
        installScanStateMethod("ReadRune", args -> {
            FmtScan.ScanState s = current();
            int r = s.readRuneChecked();
            if (r < 0) {
                return makeTriple(-1L, 0L, Fmt.eofError());
            }
            int size = r < 0x80 ? 1 : r < 0x800 ? 2 : r < 0x10000 ? 3 : 4;
            return makeTriple((long) r, (long) size, null);
        });
        // UnreadRune() error
        installScanStateMethod("UnreadRune", args -> {
            current().unreadRune();
            return null;
        });
        // SkipSpace()
        installScanStateMethod("SkipSpace", args -> {
            current().skipSpace();
            return null;
        });
        // Token(skipSpace bool, f func(rune) bool) ([]byte, error)
        installScanStateMethod("Token", args -> {
            FmtScan.ScanState s = current();
            boolean skipSpace = (Boolean) args[1];
            Object f = args[2]; // GoFunc or null
            byte[] tok = s.token(skipSpace, r -> {
                if (f == null) {
                    return !FmtScan.isSpace(r);
                }
                Object res = GoCalls.invoke((GoFunc) f, new Object[]{Integer.valueOf(r)});
                return Boolean.TRUE.equals(res);
            });
            // byte[]-backed: the D5 representation of []byte.
            byte[] arr = tok.clone();
            return makePair(new GoSlice(arr, 0, arr.length, arr.length), null);
        });
        // Width() (wid int, ok bool)
        installScanStateMethod("Width", args -> {
            FmtScan.ScanState s = current();
            return makePair(s.scanWidth(), s.scanWidthOk());
        });
        // Read(buf []byte) (n int, err error): never called by the scanner.
        installScanStateMethod("Read", args ->
                makePair(0L, Fmt.newError("ScanState's Read should not be called. Use ReadRune")));
    }

    /** Makes a pair struct (f0, f1) for two-result returns. */
    private static Object makePair(Object f0, Object f1) {
        GoStructDescriptor desc = new GoStructDescriptor(
                "internal/graalgo/fmtscan.pair",
                java.util.List.of(
                        new GoStructDescriptor.Field("f0", GoKind.INT64, false, 0, 0, null, null),
                        new GoStructDescriptor.Field("f1", GoKind.INTERFACE, false, 1, 0, null, null)));
        return new GoStruct(desc, new long[0], new Object[]{f0, f1});
    }

    /** Makes a triple struct (f0, f1, f2) for three-result returns. */
    private static Object makeTriple(Object f0, Object f1, Object f2) {
        GoStructDescriptor desc = new GoStructDescriptor(
                "internal/graalgo/fmtscan.triple",
                java.util.List.of(
                        new GoStructDescriptor.Field("f0", GoKind.INT64, false, 0, 0, null, null),
                        new GoStructDescriptor.Field("f1", GoKind.INT64, false, 1, 0, null, null),
                        new GoStructDescriptor.Field("f2", GoKind.INTERFACE, false, 2, 0, null, null)));
        return new GoStruct(desc, new long[0], new Object[]{f0, f1, f2});
    }
}
