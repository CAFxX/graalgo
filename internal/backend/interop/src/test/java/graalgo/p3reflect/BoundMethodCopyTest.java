package graalgo.p3reflect;

import java.util.ArrayList;
import java.util.List;

import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.nodes.RootNode;

import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * R22-F-4 (reflect family): a reflect bound method value
 * ({@code v.Method(i)}) must receive a fresh copy of a value receiver on
 * every {@code Call} — callee mutations must never alias the stored
 * receiver, and repeated calls must be independent.
 *
 * <p>RED (pre-fix): {@link ReflectCalls#invokeCallable} prepends the
 * stored receiver verbatim on every call.
 * Registered in {@code graalgo.iface.IfaceTestRunner}.
 */
public final class BoundMethodCopyTest {

    /** Descriptor for struct{F int64}: one long field at index 0. */
    private static GoStructDescriptor structDesc() {
        return new GoStructDescriptor("S", List.of(
                new GoStructDescriptor.Field("F", GoKind.INT64, true, 0, -1, null, null, null)));
    }

    private static GoStruct structWith(long f) {
        return new GoStruct(structDesc(), new long[]{f}, new Object[0]);
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /** A RootNode capturing its call arguments and returning a constant. */
    static final class CaptureRoot extends RootNode {
        final List<Object[]> seen = new ArrayList<>();

        CaptureRoot() {
            super(null);
        }

        @Override
        public Object execute(com.oracle.truffle.api.frame.VirtualFrame frame) {
            seen.add(frame.getArguments().clone());
            return 0L;
        }
    }

    private static ReflectTypes.TypeInfo structType() {
        return new ReflectTypes.TypeInfo(-1, GoKind.STRUCT, "main.S", "", 8, 8,
                null, null, 0, null, List.of(), List.of(), List.of(), List.of(),
                false, List.of(), "", -1);
    }

    private static ReflectTypes.MethodInfo methodInfo(CaptureRoot root, boolean pointerReceiver) {
        ReflectTypes.TypeInfo recv = structType();
        ReflectTypes.TypeInfo funcType = new ReflectTypes.TypeInfo(-1, GoKind.FUNC, "", "", 0, 0,
                null, null, 0, null, List.of(), List.of(), List.of(recv), List.of(),
                false, List.of(), "", -1);
        RootCallTarget target = root.getCallTarget();
        GoFunc func = new GoFunc(target, null, null);
        return new ReflectTypes.MethodInfo("M", "", funcType, func, pointerReceiver, 0, "M");
    }

    /**
     * Each reflect Call of a bound method value gets a fresh receiver
     * copy; mutating one call's receiver touches neither the stored
     * receiver nor the next call's.
     */
    public void testBoundMethodCallGetsFreshReceiverPerCall() {
        CaptureRoot root = new CaptureRoot();
        ReflectTypes.MethodInfo mi = methodInfo(root, false);
        GoStruct s = structWith(1);
        ReflectCalls.BoundMethodVal bmv = new ReflectCalls.BoundMethodVal(mi, s);
        ReflectCalls.invokeCallable(bmv, new Object[0]);
        ReflectCalls.invokeCallable(bmv, new Object[0]);
        assertTrue(root.seen.size() == 2, "method invoked twice, got " + root.seen.size());
        Object r1 = root.seen.get(0)[1];
        Object r2 = root.seen.get(1)[1];
        assertTrue(r1 != s && r2 != s,
                "reflect bound-method call passed the stored receiver verbatim (R22-F-4)");
        assertTrue(r1 != r2,
                "two reflect calls shared one receiver copy (R22-F-4)");
        ((GoStruct) r1).setLong(0, 99);
        assertTrue(s.getLong(0) == 1,
                "mutating the call receiver mutated the stored receiver (R22-F-4)");
        assertTrue(((GoStruct) r2).getLong(0) == 1,
                "mutating one call's receiver leaked into the next call (R22-F-4)");
    }

    /**
     * Negative control: a pointer-receiver bound method keeps sharing the
     * stored receiver, so callee mutations stay visible like gc.
     */
    public void testBoundMethodPointerReceiverStaysShared() {
        CaptureRoot root = new CaptureRoot();
        ReflectTypes.MethodInfo mi = methodInfo(root, true);
        GoStruct s = structWith(1);
        ReflectCalls.BoundMethodVal bmv = new ReflectCalls.BoundMethodVal(mi, s);
        ReflectCalls.invokeCallable(bmv, new Object[0]);
        assertTrue(root.seen.size() == 1, "method invoked once");
        assertTrue(root.seen.get(0)[1] == s,
                "pointer receiver was copied — mutations would stop being visible");
    }
}
