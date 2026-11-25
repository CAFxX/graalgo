package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.Panics;
import graalgo.go.runtime.Stacks;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;
import graalgo.runtime.GoErrors;
import graalgo.sched.SchedulerCore;
import java.nio.charset.StandardCharsets;
import java.util.List;

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
            graalgo.runtime.sync.PinLock.lockForGo(node);
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
     * {@code runtime.callers}: the return pcs of the calling goroutine's Go frames after
     * {@code skip} of them, into {@code pcbuf} (WP-TR-AUDIT-6, BUG-0047; {@link Stacks}).
     */
    @TruffleBoundary
    static long runtime_callers(Node node, long skip, Object pcbuf) {
        Slice buf = (Slice) pcbuf;
        if (buf.len() == 0) {
            return 0;
        }
        List<Stacks.Frame> frames = GoProcess.get(node).stacks.current((int) Math.max(0, Math.min(skip, Integer.MAX_VALUE)),
                buf.len());
        long[] prims = buf.base().prims;
        for (int i = 0; i < frames.size(); i++) {
            prims[buf.prim() + i] = frames.get(i).pc() + 1;
        }
        return frames.size();
    }

    /** {@code runtime.funcInfo}: the name and entry pc of the function containing {@code pc}. */
    @TruffleBoundary
    static Object runtime_funcInfo(Node node, long pc) {
        Stacks.Fn f = GoProcess.get(node).stacks.fnAt(GoProcess.get(node), pc);
        return f == null ? new Object[] {Str.EMPTY, 0L} : new Object[] {Str.of(f.name()), f.entry()};
    }

    /** {@code runtime.pcFileLine}: the file and line of {@code pc}. */
    @TruffleBoundary
    static Object runtime_pcFileLine(Node node, long pc) {
        GoProcess p = GoProcess.get(node);
        Stacks.Frame f = p.stacks.frameAt(p, pc);
        return f == null ? new Object[] {Str.EMPTY, 0L} : new Object[] {Str.of(f.file()), (long) f.line()};
    }

    /**
     * {@code runtime.Stack}: the calling goroutine's traceback, truncated to {@code buf}. With
     * {@code all}, gc appends every other goroutine's; their stacks are not walkable from here,
     * so only the caller's is reported.
     */
    @TruffleBoundary
    static long runtime_Stack(Node node, Object buf, boolean all) {
        Slice s = (Slice) buf;
        byte[] text = GoProcess.get(node).currentStack().getBytes(StandardCharsets.UTF_8);
        int n = Math.min(text.length, s.len());
        if (n > 0) {
            System.arraycopy(text, 0, s.base().bytes, s.byt(), n);
        }
        return n;
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
