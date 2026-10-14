package graalgo.iface;

import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;

import graalgo.runtime.Fmt;
import graalgo.runtime.GoCalls;
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
 * Go's {@code fmt.handleMethods} (P3-M9, RC-6).
 *
 * <p>Implements Go's method-dispatch for fmt verbs, installed as the
 * {@code Fmt.FmtMethodHandler} by the phase-3 launcher:
 * <ul>
 *   <li>{@code Formatter} (method {@code Format(fmt.State, rune)}): for all
 *       verbs except {@code %T} and {@code %p}.</li>
 *   <li>{@code GoStringer} (method {@code GoString() string}): for {@code %#v}.</li>
 *   <li>{@code Stringer} (method {@code String() string}): for
 *       {@code %v}, {@code %s}, {@code %q}, {@code %x}, {@code %X}.</li>
 *   <li>{@code error} (method {@code Error() string}): same verbs as Stringer.</li>
 * </ul>
 *
 * <p>The {@code fmt.State} passed to {@code Format} is a
 * {@code internal/graalgo/fmtstate.PrinterState} value whose methods are
 * implemented in Java (below) and installed into the method table at
 * class-load. The methods delegate to the thread-local current
 * {@code Fmt.Printer}.
 */
public final class FmtHandleMethods implements Fmt.FmtMethodHandler {
    /** Descriptor name of the synthetic State implementation. */
    private static final String PRINTER_STATE_DESC = "internal/graalgo/fmtstate.PrinterState";
    /** Descriptor name of the fmt.State interface. */
    private static final String FMT_STATE_DESC = "fmt.State";

    static {
        installStateMethods();
    }

    /** Installs this handler with Fmt (called by the launcher). */
    public static void install() {
        Fmt.setFmtMethodHandler(new FmtHandleMethods());
    }

    /**
     * The printers owning the currently-active {@code Formatter.Format}
     * dispatches, innermost last (P3-M10, fix-fmt-funcs).
     *
     * <p>A {@code fmt.State} value is bound to the {@code Fmt.Printer} that
     * was formatting when the {@code Format} dispatch began (gc: the
     * {@code *pp} the state refers to) — <em>not</em> to whatever printer
     * happens to be {@code Fmt.currentPrinter} when a State method runs.
     * The two differ as soon as the {@code Format} method itself calls back
     * into fmt (e.g. {@code fmt.Fprintf(s, ...)} or {@code fmt.Sprintf}
     * inside {@code Format}): those set their own inner printer, so the
     * State methods must resolve through this stack. The fallback to
     * {@code Fmt.currentPrinter} covers direct calls outside a dispatch.
     */
    private static final ThreadLocal<java.util.ArrayDeque<Fmt.Printer>> formatPrinterStack =
            ThreadLocal.withInitial(java.util.ArrayDeque::new);

    /** The printer the current {@code fmt.State} method call belongs to. */
    private static Fmt.Printer statePrinter() {
        Fmt.Printer p = formatPrinterStack.get().peekLast();
        if (p != null) {
            return p;
        }
        return Fmt.currentPrinter.get();
    }

    /** Installs the Java-implemented fmt.State methods. */
    private static void installStateMethods() {
        // Write(b []byte) (int, error)
        installStateMethod("Write", args -> {
            Fmt.Printer p = statePrinter();
            if (p == null) {
                throw new IllegalStateException("fmtstate.Write: no current printer");
            }
            // args[0] is receiver (ignored), args[1] is b.
            GoSlice s = (GoSlice) args[1];
            byte[] b = sliceToBytes(s);
            StringBuilder hex = new StringBuilder();
            for (byte by : b) hex.append(String.format("%02x", by));
            p.writeBytes(b);
            return makePair((long) b.length, null);
        });
        // Width() (int, bool)
        installStateMethod("Width", args -> {
            Fmt.Printer p = statePrinter();
            if (p == null) {
                throw new IllegalStateException("fmtstate.Width: no current printer");
            }
            return makePair((long) (p.widPresent ? p.wid : 0), p.widPresent);
        });
        // Precision() (int, bool)
        installStateMethod("Precision", args -> {
            Fmt.Printer p = statePrinter();
            if (p == null) {
                throw new IllegalStateException("fmtstate.Precision: no current printer");
            }
            return makePair((long) (p.precPresent ? p.prec : 0), p.precPresent);
        });
        // Flag(c int) bool
        installStateMethod("Flag", args -> {
            Fmt.Printer p = statePrinter();
            if (p == null) {
                throw new IllegalStateException("fmtstate.Flag: no current printer");
            }
            int c = ((Number) args[1]).intValue();
            return switch (c) {
                case '-' -> p.minus;
                case '+' -> p.plus;
                case '#' -> p.sharp;
                case ' ' -> p.space;
                case '0' -> p.zero;
                default -> false;
            };
        });
    }

    /** Functional interface for State method implementations. */
    @FunctionalInterface
    private interface StateMethod {
        Object invoke(Object[] args);
    }

    /** Installs one State method. */
    private static void installStateMethod(String name, StateMethod impl) {
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
        IfaceMethodTable.installMethod(PRINTER_STATE_DESC, name, methodTarget);
    }

    /** Converts a GoSlice of bytes to a Java byte[]. */
    private static byte[] sliceToBytes(GoSlice s) {
        if (s == null) {
            return new byte[0];
        }
        Object arr = s.array();
        int off = s.offset();
        int len = s.len();
        byte[] out = new byte[len];
        if (arr instanceof byte[] ba) {
            System.arraycopy(ba, off, out, 0, len);
        } else if (arr instanceof int[] ia) {
            // []byte (uint8) may be backed by int[] in the D5 representation.
            for (int i = 0; i < len; i++) {
                out[i] = (byte) ia[off + i];
            }
        }
        return out;
    }

    /** Makes a pair struct (f0, f1) for two-result returns. */
    private static Object makePair(Object f0, Object f1) {
        GoStructDescriptor desc = new GoStructDescriptor(
                "internal/graalgo/fmtstate.pair",
                java.util.List.of(
                        new GoStructDescriptor.Field("f0", GoKind.INT64, false, 0, 0, null, null),
                        new GoStructDescriptor.Field("f1", GoKind.INTERFACE, false, 1, 0, null, null)));
        // f0 goes in longs if it's a Long, else objects. Simplified: put in objects.
        // Actually, for the pair to work with the desugar, we need correct layout.
        // For now, put both in objects; the desugar reads by field index.
        return new GoStruct(desc, new long[0], new Object[]{f0, f1});
    }

    @Override
    public boolean handle(Fmt.Printer printer, Fmt.Arg arg, int verb) {
        if (verb == 'T' || verb == 'p') {
            return false;
        }
        Object v = arg.value();
        GoKind k = arg.kind();
        // Normalize kind (like Printer.normalize).
        if (k == GoKind.INTERFACE && v instanceof GoInterface gi && !gi.isNil()) {
            String descName = ((GoTypeDescriptor) gi.descriptor()).name();
            Object receiver = gi.data();
            return handleForType(printer, descName, receiver, verb, arg);
        } else if (k != GoKind.INTERFACE) {
            String descName = arg.typeName();
            if (descName == null) {
                return false;
            }
            return handleForType(printer, descName, v, verb, arg);
        }
        return false;
    }

    private boolean handleForType(Fmt.Printer printer, String descName, Object receiver, int verb, Fmt.Arg arg) {
        if (descName == null || receiver == null) {
            return false;
        }

        // 1. Formatter: Format(fmt.State, rune)
        IfaceMethodTarget formatTarget = IfaceMethodTable.lookup(descName, "Format");
        if (formatTarget != null && formatTarget.supported()) {
            GoStructDescriptor stateStructDesc = new GoStructDescriptor(PRINTER_STATE_DESC, java.util.List.of());
            GoStruct stateData = new GoStruct(stateStructDesc, new long[0], new Object[0]);
            // The GoInterface descriptor is the DYNAMIC TYPE. Use a
            // GoTypeDescriptor (the method table is keyed by name).
            GoTypeDescriptor stateTypeDesc = new GoTypeDescriptor(PRINTER_STATE_DESC, GoKind.STRUCT, false);
            // Build the itab for (PrinterState, fmt.State).
            Object itab = null;
            try {
                itab = IfaceMethodTable.itabForIntrinsic(PRINTER_STATE_DESC, FMT_STATE_DESC);
            } catch (Exception e) {
            }
            GoInterface stateIface = new GoInterface(stateTypeDesc, stateData, itab);
            Object[] callArgs = adaptReceiver(formatTarget, receiver, new Object[]{stateIface, (int) verb});
            if (callArgs == null) {
                return false;
            }
            // P3-M10 (fix-fmt-funcs): bind the State methods (Write/Width/
            // Precision/Flag) to the printer being formatted — the Format
            // method may call back into fmt (Fprintf(s, ...), Sprintf),
            // which installs its own inner printer.
            java.util.ArrayDeque<Fmt.Printer> stack = formatPrinterStack.get();
            Fmt.Printer outer = Fmt.currentPrinter.get();
            if (outer != null) {
                stack.addLast(outer);
            }
            try {
                GoCalls.invoke(formatTarget.func(), callArgs);
            } catch (Exception e) {
                e.printStackTrace(System.err);
                return false;
            } finally {
                if (outer != null) {
                    stack.removeLast();
                }
            }
            return true;
        }

        // 2. GoStringer for %#v (sharpV is printer.sharpV, verb == 'v').
        if (printer.sharpV && verb == 'v') {
            IfaceMethodTarget goStringTarget = IfaceMethodTable.lookup(descName, "GoString");
            if (goStringTarget != null && goStringTarget.supported()) {
                Object result = invokeNoArg(goStringTarget, receiver);
                if (result instanceof GoString gs) {
                    printer.writeUtf8(gs.toString());
                    return true;
                }
            }
            return false;
        }

        // 3. Stringer and error for %v, %s, %q, %x, %X.
        // P3-M9 BUG-MODPROBE-011: the verb applies to the method's return
        // value (e.g. %q quotes the String() result), so route through
        // formatMethodResult rather than writing the raw string.
        if (verb == 'v' || verb == 's' || verb == 'q' || verb == 'x' || verb == 'X') {
            IfaceMethodTarget stringTarget = IfaceMethodTable.lookup(descName, "String");
            if (stringTarget != null && stringTarget.supported()) {
                Object result = invokeNoArg(stringTarget, receiver);
                if (result instanceof GoString gs) {
                    printer.formatMethodResult(gs.bytes(), verb);
                    return true;
                } else if (result instanceof String s) {
                    printer.formatMethodResult(s.getBytes(java.nio.charset.StandardCharsets.UTF_8), verb);
                    return true;
                }
            }
            IfaceMethodTarget errorTarget = IfaceMethodTable.lookup(descName, "Error");
            if (errorTarget != null && errorTarget.supported()) {
                Object result = invokeNoArg(errorTarget, receiver);
                if (result instanceof GoString gs) {
                    printer.formatMethodResult(gs.bytes(), verb);
                    return true;
                } else if (result instanceof String s) {
                    printer.formatMethodResult(s.getBytes(java.nio.charset.StandardCharsets.UTF_8), verb);
                    return true;
                }
            }
        }
        return false;
    }

    /** Adapts the receiver for the target (handles pointerReceiver, addrOf). */
    private static Object[] adaptReceiver(IfaceMethodTarget target, Object receiver, Object[] args) {
        Object recv = receiver;
        if (!target.isDirect()) {
            // Promoted methods: not implemented for now.
            return null;
        }
        if (recv instanceof GoPointer gp && !target.pointerReceiver()) {
            // P3-M9 BUG-MODPROBE-011: gc's implicit (*p) — a value-receiver
            // method dispatched on a pointer dereferences first. Without
            // this the callee receives a GoPointer where it expects the
            // value, throws, and fmt falls through to structural printing.
            recv = gp.referent();
            if (recv == null) {
                return null; // nil pointer: cannot dereference; structural path handles it
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

    /** Invokes a no-arg method (String, Error, GoString). */
    private static Object invokeNoArg(IfaceMethodTarget target, Object receiver) {
        Object[] args = adaptReceiver(target, receiver, new Object[0]);
        if (args == null) {
            return null;
        }
        try {
            return GoCalls.invoke(target.func(), args);
        } catch (Exception e) {
            return null;
        }
    }
}
