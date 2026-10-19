package graalgo.p3reflect;

import graalgo.values.GoFunc;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;
import graalgo.runtime.GoCalls;

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
        return invokeCallableMulti(callable, d5Args, 1)[0];
    }

    /**
     * Multi-result variant of {@link #invokeCallable}: returns one D5 value
     * per declared out (I-6). A {@code MakeFunc} type may declare multiple
     * outs via {@code FuncOf}; v1 Go funcs declare at most one result
     * (frontend WS D), so a multi-out {@link GoFunc}/{@link MethodVal}/
     * {@link BoundMethodVal} is unreachable — it fails loud here rather
     * than silently truncating to the first result.
     *
     * <p>Package-visible for unit tests.
     */
    static Object[] invokeCallableMulti(Object callable, Object[] d5Args, int numOuts) {
        if (callable instanceof MakeFuncVal mfv) {
            return invokeMakeFuncMulti(mfv, d5Args, numOuts);
        }
        if (numOuts > 1) {
            throw new IllegalStateException(
                    "reflect: Call of func with " + numOuts
                            + " results is not supported in v1 (WS D)");
        }
        if (callable instanceof GoFunc gf) {
            return new Object[]{GoCalls.invoke(gf, d5Args)};
        }
        if (callable instanceof MethodVal mv) {
            return new Object[]{GoCalls.invoke(mv.method.func, d5Args)};
        }
        if (callable instanceof BoundMethodVal bmv) {
            Object[] withRecv = new Object[d5Args.length + 1];
            withRecv[0] = bmv.receiver;
            System.arraycopy(d5Args, 0, withRecv, 1, d5Args.length);
            return new Object[]{GoCalls.invoke(bmv.method.func, withRecv)};
        }
        throw new IllegalStateException(
                "reflect: not a callable: " + callable);
    }

    /**
     * The MakeFunc trampoline: D5 args → []Value → callback → D5 results.
     * The callback is a synthetic {@code func([]Value) []Value} Value.
     * Unboxes one D5 value per declared out (I-6).
     */
    private static Object[] invokeMakeFuncMulti(MakeFuncVal mfv, Object[] d5Args, int numOuts) {
        ReflectTypes.Table table = ReflectTypes.table();
        ReflectTypes.TypeInfo funcType = table.info(mfv.funcTok);
        // Build the []Value argument.
        GoStruct[] values = new GoStruct[d5Args.length];
        for (int i = 0; i < d5Args.length; i++) {
            long argTok = funcType.ins.get(i).tok;
            long handle = ReflectRefs.intern(d5Args[i]);
            values[i] = ReflectValues.pack(ReflectTypes.valueDescriptor(),
                    argTok, handle, 0);
        }
        Object valuesSlice = new graalgo.values.GoSlice(
                values, 0, values.length, values.length);
        // Invoke the callback: __reflect_call on the callback Value.
        Object result = invokeCallable(
                ReflectRefs.load(unpackHandle(mfv.callback)), new Object[]{valuesSlice});
        // Convert the []Value result back to D5, one per declared out.
        return unboxMakeFuncResults((GoSlice) result, numOuts);
    }

    /**
     * Unboxes a MakeFunc callback's []Value result slice to D5, one value
     * per declared out. A result count that disagrees with the declared
     * type fails loud (the callback violated the MakeFunc contract), except
     * the legacy single-out empty result which maps to a nil D5 value.
     *
     * <p>Package-visible for unit tests.
     */
    static Object[] unboxMakeFuncResults(GoSlice resultSlice, int numOuts) {
        if (numOuts == 1 && resultSlice.len() == 0) {
            // Legacy single-out behavior: an empty []Value maps to nil D5.
            return new Object[]{null};
        }
        if (resultSlice.len() != numOuts) {
            throw new IllegalStateException(
                    "reflect: MakeFunc callback returned " + resultSlice.len()
                            + " values, want " + numOuts);
        }
        Object[] d5 = new Object[numOuts];
        for (int i = 0; i < numOuts; i++) {
            // The callback is a real Go func value; its []Value result slice
            // may be backed by Object[] rather than GoStruct[] — read via
            // Array.get.
            GoStruct resultValue = (GoStruct) java.lang.reflect.Array.get(
                    resultSlice.array(), resultSlice.offset() + i);
            d5[i] = ReflectValues.unbox(resultValue);
        }
        return d5;
    }

    private static long unpackHandle(GoStruct v) {
        return ReflectValues.parts(v).ptr();
    }
}
