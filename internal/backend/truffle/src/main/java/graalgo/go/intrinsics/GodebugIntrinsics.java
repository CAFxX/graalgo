package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.Unsafe;
import graalgo.go.values.Closure;
import graalgo.go.values.Str;

/**
 * What gc's runtime provides to {@code internal/godebug} and, for the environment, to
 * {@code syscall} by {@code //go:linkname} (upstream runtime/runtime.go, runtime1.go,
 * metrics.go); registered in {@code internal/toolexec/intrinsics/registry.txt}.
 * {@code internal/godebug} itself is upstream's source: it parses the setting and keeps the
 * per-key state; the runtime's part is to hand it the GODEBUG value at registration and on
 * every change of the environment variable.
 */
final class GodebugIntrinsics {
    private GodebugIntrinsics() {}

    private static final Str GODEBUG = Str.of("GODEBUG");

    /** gc {@code godebugNotify}: {@code update(godebugDefault, env)}. */
    private static void notify(Node node, Str env) {
        GoProcess process = GoProcess.get(node);
        Closure update = process.godebugUpdate;
        if (update != null) {
            update.target().call(node, update.env(), Str.of(process.godebugDefault), env);
        }
    }

    /** {@code setUpdate(update)}: registers the hook and calls it with the current setting. */
    @TruffleBoundary
    static void internal_godebug_setUpdate(Node node, Object update) {
        GoProcess.get(node).godebugUpdate = (Closure) update;
        String env = System.getenv("GODEBUG");
        notify(node, env == null || env.isEmpty() ? Str.EMPTY : Str.of(env));
    }

    /**
     * {@code registerMetric(name, read)}: gc exports the counter as a runtime/metrics sample.
     * runtime/metrics has no samples here (docs/arch/limitations.md); the counters themselves
     * are internal/godebug's.
     */
    static void internal_godebug_registerMetric(Node node, Str name, Object read) {
    }

    /**
     * {@code setNewIncNonDefault(f)}: the hook through which the runtime's own settings count
     * non-default uses; the runtime has no such setting here.
     */
    static void internal_godebug_setNewIncNonDefault(Node node, Object f) {
    }

    /** {@code runtime.write(fd, p, n) int32}, which internal/godebug prints its errors with. */
    @TruffleBoundary
    static long runtime_write(Node node, long fd, Object p, long n) {
        Object[] r = graalgo.syscall.Executor.syscall6(node, 1, fd, Unsafe.toInt(node, p), n, 0, 0, 0);
        return (long) r[2] != 0 ? -(long) r[2] : (long) (int) (long) r[0];
    }

    /** {@code syscall.runtimeSetenv(k, v)}: the runtime learns of an environment change. */
    @TruffleBoundary
    static void syscall_runtimeSetenv(Node node, Str k, Str v) {
        if (k.equals(GODEBUG)) {
            GoProcess.get(node).godebugChanged(v.toString());
            notify(node, v);
        }
    }

    /**
     * {@code syscall.runtimeClearenv(env map[string]int)}: every variable is gone; gc forgets a
     * GODEBUG setting that was among them and notifies {@code internal/godebug}.
     */
    @TruffleBoundary
    static void syscall_runtimeClearenv(Node node, Object env) {
        if (env instanceof graalgo.go.values.GoMap m && m.find(GODEBUG) >= 0) {
            GoProcess.get(node).godebugChanged("");
            notify(node, Str.EMPTY);
        }
    }

    /** {@code syscall.runtimeUnsetenv(k)}. */
    @TruffleBoundary
    static void syscall_runtimeUnsetenv(Node node, Str k) {
        if (k.equals(GODEBUG)) {
            GoProcess.get(node).godebugChanged("");
            notify(node, Str.EMPTY);
        }
    }
}
