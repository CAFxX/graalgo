package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.Panics;
import graalgo.go.values.Str;
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
        // gc clears sync.Pool's caches at the start of a collection, with the world stopped:
        // every pool field is touched only inside pin sections, so holding the pin lock is
        // the stopped world for them.
        graalgo.go.values.Closure cleanup = graalgo.go.runtime.GoProcess.get(node).poolCleanup;
        if (cleanup != null) {
            graalgo.runtime.sync.PinLock.LOCK.lock();
            try {
                cleanup.target().call(node, cleanup.env());
            } finally {
                graalgo.runtime.sync.PinLock.LOCK.unlock();
            }
        }
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

    /** gc's gcount: the goroutines that exist, blocked ones included, the runtime's own not. */
    @TruffleBoundary
    static long runtime_NumGoroutine(Node node) {
        return SchedulerCore.current().numGoroutines();
    }

    /** Terminates the calling goroutine after running its deferred calls (GG-0070). */
    static void runtime_Goexit(Node node) {
        graalgo.runtime.Goexit.goexit();
    }

    /**
     * {@code syscall.Exit}, which upstream's runtime provides by {@code //go:linkname} and
     * {@code os.Exit} ends in: the scheduler's exit request; the calling goroutine never returns.
     */
    @TruffleBoundary
    static void syscall_Exit(Node node, long code) {
        SchedulerCore sc = SchedulerCore.installedOrNull();
        if (sc == null || !sc.onGoroutine()) {
            throw new IllegalStateException("syscall.Exit outside a goroutine");
        }
        sc.requestExit((int) code);
        sc.freezeForExit();
        throw new IllegalStateException("syscall.Exit returned");
    }

    /**
     * {@code runtime.Caller}: the node package records no call positions yet, so no frame can
     * be attributed — {@code ok == false}, which is gc's answer when it cannot recover the
     * information (docs/arch/limitations.md).
     */
    static Object runtime_Caller(Node node, long skip) {
        return new Object[] {0L, Str.EMPTY, 0L, false};
    }

    /** {@code runtime.FuncForPC}: nil, gc's answer for a pc inside no known function. */
    static Object runtime_FuncForPC(Node node, long pc) {
        return null;
    }

    /** {@code (*runtime.Func).Name}: upstream returns "" for a nil receiver, the only one there is. */
    static Str runtime___Func__Name(Node node, Object f) {
        return Str.EMPTY;
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
