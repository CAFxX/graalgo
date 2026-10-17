package graalgo.p3reflect;

import graalgo.values.GoFunc;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;
import graalgo.runtime.GoCalls;
import java.lang.ref.Reference;

/**
 * Callable wrappers and trampolines for the reflect package (P3-M8, wave 2).
 *
 * <p>A func-typed {@code reflect.Value}'s handle resolves to one of:
 * <ul>
 *   <li>{@link GoFunc} — from {@code ValueOf(f)}; invoked directly.</li>
 *   <li>{@link MethodVal} — from {@code Type.Method(i)} (method expression);
 *       the receiver is the first call argument.</li>
 *   <li>{@link BoundMethodVal} — from {@code Value.Method(i)}; the receiver
 *       is bound at Method() time and prepended at call time.</li>
 *   <li>{@link MakeFuncVal} — from {@code MakeFunc}; the trampoline
 *       converts D5 args to {@code []Value}, invokes the Go callback, and
 *       converts results back. Panics in the callback propagate as
 *       PanicException across the boundary.</li>
 * </ul>
 */
public final class ReflectCalls {
    private ReflectCalls() {}

    /** Method expression: (receiver, args...) → results. */
    public static final class MethodVal {
        public final ReflectTypes.MethodInfo method;

        public MethodVal(ReflectTypes.MethodInfo method) {
            this.method = method;
        }
    }

    /** Bound method value: receiver fixed, (args...) → results. */
    public static final class BoundMethodVal {
        public final ReflectTypes.MethodInfo method;
        /** The receiver as a D5 value (unboxed). */
        public final Object receiver;

        public BoundMethodVal(ReflectTypes.MethodInfo method, Object receiver) {
            this.method = method;
            this.receiver = receiver;
        }
    }

    /** MakeFunc trampoline target. */
    public static final class MakeFuncVal {
        public final long funcTok;
        /** The callback as a func-typed Value (GoStruct). */
        public final GoStruct callback;

        public MakeFuncVal(long funcTok, GoStruct callback) {
            this.funcTok = funcTok;
            this.callback = callback;
        }
    }

    /**
     * Invokes a func-typed Value's callable with D5 arguments.
     * Returns the D5 result (single result; v1 has no multi-result funcs).
     */
    public static Object invokeCallable(Object callable, Object[] d5Args) {
        if (callable instanceof GoFunc gf) {
            return GoCalls.invoke(gf, d5Args);
        }
        if (callable instanceof MethodVal mv) {
            return GoCalls.invoke(mv.method.func, d5Args);
        }
        if (callable instanceof BoundMethodVal bmv) {
            Object[] withRecv = new Object[d5Args.length + 1];
            withRecv[0] = bmv.receiver;
            System.arraycopy(d5Args, 0, withRecv, 1, d5Args.length);
            return GoCalls.invoke(bmv.method.func, withRecv);
        }
        if (callable instanceof MakeFuncVal mfv) {
            return invokeMakeFunc(mfv, d5Args);
        }
        throw new IllegalStateException(
                "reflect: not a callable: " + callable);
    }

    /**
     * The MakeFunc trampoline: D5 args → []Value → callback → D5 result.
     * The callback is a synthetic {@code func([]Value) []Value} Value.
     */
    private static Object invokeMakeFunc(MakeFuncVal mfv, Object[] d5Args) {
        ReflectTypes.Table table = ReflectTypes.table();
        ReflectTypes.TypeInfo funcType = table.info(mfv.funcTok);
        // Build the []Value argument.
        GoStruct[] values = new GoStruct[d5Args.length];
        for (int i = 0; i < d5Args.length; i++) {
            long argTok = funcType.ins.get(i).tok;
            values[i] = ReflectRefs.packCounted(ReflectTypes.valueDescriptor(),
                    argTok, d5Args[i], 0);
        }
        Object valuesSlice = new graalgo.values.GoSlice(
                values, 0, values.length, values.length);
        // Invoke the callback: __reflect_call on the callback Value.
        Object callbackFn = ReflectRefs.load(unpackHandle(mfv.callback));
        // RV-P1-23 §6f: keep the callback Value reachable past the last
        // handle-derived use.
        Reference.reachabilityFence(mfv.callback);
        Object result = invokeCallable(callbackFn, new Object[]{valuesSlice});
        // Convert the []Value result back to D5. Single-result (v1).
        GoSlice resultSlice = (GoSlice) result;
        if (resultSlice.len() == 0) {
            return null;
        }
        // The callback is a real Go func value; its []Value result slice may
        // be backed by Object[] rather than GoStruct[] — read via Array.get.
        GoStruct resultValue = (GoStruct) java.lang.reflect.Array.get(
                resultSlice.array(), resultSlice.offset());
        // Unbox the single Value to D5.
        return ReflectValues.unbox(resultValue);
    }

    private static long unpackHandle(GoStruct v) {
        return ReflectValues.parts(v).ptr();
    }
}
