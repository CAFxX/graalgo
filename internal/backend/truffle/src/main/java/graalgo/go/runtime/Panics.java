package graalgo.go.runtime;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.types.GoType;
import graalgo.go.values.Iface;
import graalgo.language.GoLanguage;
import graalgo.runtime.GoErrorValue;
import graalgo.runtime.GoErrors;
import graalgo.runtime.GoErrors.BoundsCode;
import graalgo.runtime.PanicNilError;

/**
 * Factories for run-time panics (truffle-design §11 B5): every method is a boundary and is
 * called behind a branch profile. The values are the runtime error values of the existing panic
 * model ({@link GoErrors}), boxed as interface values of the runtime's error types
 * ({@link RuntimeTypes}).
 */
public final class Panics {
    private Panics() {}

    @TruffleBoundary
    public static GoPanic of(Node n, GoErrorValue e) {
        return new GoPanic(n, GoLanguage.get(n).runtimeTypes().box(e));
    }

    @TruffleBoundary
    public static GoPanic nilDeref(Node n) {
        return of(n, GoErrors.nilDereference());
    }

    @TruffleBoundary
    public static GoPanic index(Node n, long i, long len, boolean signed) {
        return of(n, GoErrors.bounds(BoundsCode.INDEX, i, len, signed));
    }

    /** {@code signed}: the type of the index {@code x} reported (gc prints an unsigned one ≥ 2^63 as is). */
    @TruffleBoundary
    public static GoPanic bounds(Node n, BoundsCode code, long x, long y, boolean signed) {
        return of(n, GoErrors.bounds(code, x, y, signed));
    }

    @TruffleBoundary
    public static GoPanic divideByZero(Node n) {
        return of(n, GoErrors.integerDivideByZero());
    }

    @TruffleBoundary
    public static GoPanic negativeShift(Node n) {
        return of(n, GoErrors.negativeShift());
    }

    @TruffleBoundary
    public static GoPanic nilMap(Node n) {
        return of(n, GoErrors.assignmentToNilMap());
    }

    @TruffleBoundary
    public static GoPanic plain(Node n, String msg) {
        return of(n, GoErrors.plain(msg));
    }

    @TruffleBoundary
    public static GoPanic error(Node n, String msg) {
        return of(n, GoErrors.rangeState(msg));
    }

    @TruffleBoundary
    public static GoPanic uncomparable(Node n, GoType t) {
        return of(n, new GoErrorValue("runtime error: comparing uncomparable type " + t.str(), false,
                "runtime.errorString", null, null));
    }

    @TruffleBoundary
    public static GoPanic unhashable(Node n, GoType t) {
        return of(n, new GoErrorValue("runtime error: hash of unhashable type " + t.str(), false,
                "runtime.errorString", null, null));
    }

    /** Failed {@code x.(T)}: {@code dyn} null for a nil interface. */
    @TruffleBoundary
    public static GoPanic typeAssert(Node n, GoType iface, GoType dyn, GoType target) {
        String ifaceStr = iface.isNamed() ? iface.str() : iface.u().imethods.length == 0 ? "interface {}" : "interface";
        if (target.isInterface()) {
            String missing = dyn == null ? null : dyn.missingMethod(target);
            return of(n, GoErrors.interfaceConversionMissing(dyn == null ? null : dyn.str(), target.str(), missing));
        }
        return of(n, GoErrors.interfaceConversion(ifaceStr, dyn == null ? "nil" : dyn.str(), target.str()));
    }

    /** {@code panic(v)} for an {@code any} value (null: {@code panic(nil)}). */
    @TruffleBoundary
    public static GoPanic user(Node n, Object any) {
        if (any == null) {
            return of(n, PanicNilError.INSTANCE);
        }
        return new GoPanic(n, any);
    }

    /** Boxes a runtime error as the interface value user code recovers. */
    @TruffleBoundary
    public static Iface box(Node n, GoErrorValue e) {
        return GoLanguage.get(n).runtimeTypes().box(e);
    }
}
