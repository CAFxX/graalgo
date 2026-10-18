package graalgo.p3reflect;

import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import graalgo.runtime.GoCalls;
import graalgo.runtime.Panic;

import java.nio.charset.StandardCharsets;

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
     *
     * <p>The callback's argument slice holds exactly one {@code Value} per
     * formal parameter (gc's makeFuncImpl contract). For a variadic target
     * the trailing D5 args pack into the final {@code []T} parameter — the
     * mirror image of the CallSlice spread in {@code unboxArgs}. Arity
     * violations raise a Go panic (gc's messages), never a raw Java
     * exception.
     */
    private static Object invokeMakeFunc(MakeFuncVal mfv, Object[] d5Args) {
        ReflectTypes.Table table = ReflectTypes.table();
        ReflectTypes.TypeInfo funcType = table.info(mfv.funcTok);
        // Build the []Value argument.
        GoStruct[] values = packMakeFuncArgs(funcType, d5Args);
        Object valuesSlice = new graalgo.values.GoSlice(
                values, 0, values.length, values.length);
        // Invoke the callback: __reflect_call on the callback Value.
        Object result = invokeCallable(
                ReflectRefs.load(unpackHandle(mfv.callback)), new Object[]{valuesSlice});
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

    /**
     * Builds the MakeFunc callback's {@code []Value}: one Value per formal
     * parameter. A variadic target packs the trailing D5 args into the
     * final {@code []T} slice parameter; a non-variadic target requires an
     * exact arity match.
     */
    private static GoStruct[] packMakeFuncArgs(
            ReflectTypes.TypeInfo funcType, Object[] d5Args) {
        GoStructDescriptor desc = ReflectTypes.valueDescriptor();
        int numIn = funcType.ins.size();
        if (funcType.variadic) {
            return packVariadicArgs(funcType, numIn, d5Args, desc);
        }
        if (d5Args.length < numIn) {
            goPanic("reflect: Call with too few input arguments");
        }
        if (d5Args.length > numIn) {
            goPanic("reflect: Call with too many input arguments");
        }
        GoStruct[] values = new GoStruct[numIn];
        for (int i = 0; i < numIn; i++) {
            values[i] = packValue(desc, funcType.ins.get(i).tok, d5Args[i]);
        }
        return values;
    }

    /**
     * Variadic packing for the MakeFunc trampoline: the fixed parameters
     * map 1:1; the trailing D5 args pack into a fresh {@code []T} slice
     * Value for the final formal parameter (gc packs the same way in
     * makeFuncImpl.call).
     */
    private static GoStruct[] packVariadicArgs(ReflectTypes.TypeInfo funcType,
            int numIn, Object[] d5Args, GoStructDescriptor desc) {
        ReflectTypes.TypeInfo tail =
                numIn == 0 ? null : funcType.ins.get(numIn - 1);
        if (tail == null || tail.kind != GoKind.SLICE) {
            // Malformed variadic type (gc's FuncOf rejects these at
            // construction). Still a Go panic — never a Java exception.
            goPanic("reflect: MakeFunc of malformed variadic type");
        }
        int numFixed = numIn - 1;
        if (d5Args.length < numFixed) {
            goPanic("reflect: Call with too few input arguments");
        }
        GoStruct[] values = new GoStruct[numIn];
        for (int i = 0; i < numFixed; i++) {
            values[i] = packValue(desc, funcType.ins.get(i).tok, d5Args[i]);
        }
        // Pack the trailing args into the variadic []T parameter, using
        // the canonical D5 backing store for the element kind.
        GoKind elemKind = tail.elem.kind;
        int m = d5Args.length - numFixed;
        Object backing = ReflectValues.newArray(elemKind, m);
        for (int i = 0; i < m; i++) {
            ReflectValues.arraySet(backing, elemKind, i, d5Args[numFixed + i]);
        }
        values[numFixed] = packValue(desc, tail.tok,
                new GoSlice(backing, 0, m, m));
        return values;
    }

    /** Packs one D5 argument as a {@code reflect.Value} of the given type. */
    private static GoStruct packValue(
            GoStructDescriptor desc, long argTok, Object d5) {
        long handle = ReflectRefs.intern(d5);
        return ReflectValues.pack(desc, argTok, handle, 0);
    }

    /**
     * Raises a Go panic carrying a D5 GoString (the p3sync misuse-panic
     * convention: PanicRendering renders a raw Java String as
     * "(?) 0xADDR", so the message must be a D5 GoString).
     */
    private static void goPanic(String message) {
        Panic.throwPanic(
                new GoString(message.getBytes(StandardCharsets.UTF_8)),
                null, null, null);
    }
}
