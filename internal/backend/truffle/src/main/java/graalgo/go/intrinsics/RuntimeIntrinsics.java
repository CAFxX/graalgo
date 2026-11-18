package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.Panics;
import graalgo.runtime.GoErrors;
import graalgo.sched.SchedulerCore;

/**
 * Intrinsics of the facade {@code runtime} package ({@code stdlib/fork/runtime}); registered in
 * {@code internal/toolexec/intrinsics/registry.txt} (toolexec-design §5.3, §5.6).
 */
final class RuntimeIntrinsics {
    private RuntimeIntrinsics() {}

    @TruffleBoundary
    static void runtime_Gosched(Node node) {
        SchedulerCore.current().yieldCurrent();
    }

    @TruffleBoundary
    static void runtime_GC(Node node) {
        System.gc();
    }

    /** Sets the cap and returns the previous setting; n &lt; 1 only queries. */
    @TruffleBoundary
    static long runtime_GOMAXPROCS(Node node, long n) {
        return SchedulerCore.current().setGomaxprocs((int) Math.max(Math.min(n, Integer.MAX_VALUE), 0));
    }

    @TruffleBoundary
    static long runtime_NumCPU(Node node) {
        return Runtime.getRuntime().availableProcessors();
    }

    static long runtime_nanotime(Node node) {
        return System.nanoTime();
    }

    /**
     * The backend's value for a run-time error a Go package raises itself (the facade's
     * {@code divideError} and {@code overflowError}): the same {@code runtime.Error} a
     * backend-raised panic of that kind carries.
     */
    @TruffleBoundary
    static Object runtime_runtimeError(Node node, long code) {
        switch ((int) code) {
            case 0:
                return Panics.box(node, GoErrors.integerDivideByZero());
            case 1:
                // An errorString like the other; GoErrors names its factory after its first
                // user.
                return Panics.box(node, GoErrors.rangeState("integer overflow"));
            default:
                throw new IllegalStateException("runtime.runtimeError: unknown code " + code);
        }
    }
}
