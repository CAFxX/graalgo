package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.GoProcess;
import graalgo.go.types.GoType;
import graalgo.go.values.Flat;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;
import graalgo.language.GoContext;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.ThreadLocalRandom;

/**
 * What gc's runtime provides to package {@code os} by {@code //go:linkname} (toolexec-design
 * §5.6; upstream runtime/runtime.go, proc.go, os_linux.go, rand.go); registered in
 * {@code internal/toolexec/intrinsics/registry.txt}. {@code os} itself is upstream's source
 * over {@code internal/poll} and {@code syscall}.
 */
final class OsIntrinsics {
    private OsIntrinsics() {}

    /** {@code os.runtime_args}: the program's argv, {@code argv[0]} as the process was invoked. */
    @TruffleBoundary
    static Object os_runtime_args(Node node) {
        String[] argv = GoContext.get(node).applicationArguments();
        GoType st = GoProcess.get(node).types.basic(GoType.STRING);
        Object[] refs = new Object[argv.length];
        for (int i = 0; i < argv.length; i++) {
            refs[i] = argv[i].isEmpty() ? Str.EMPTY : new Str(argv[i].getBytes(StandardCharsets.UTF_8));
        }
        return new Slice(new Flat(st.backing(), Flat.NO_PRIMS, refs, Flat.NO_BYTES), 0, 0, 0, argv.length,
                argv.length);
    }

    /**
     * {@code os.runtime_beforeExit(exitCode)}: gc's hook flushes the race detector and coverage
     * counters, neither of which exists here (ADR-0010 DV6).
     */
    static void os_runtime_beforeExit(Node node, long exitCode) {
    }

    /** {@code os.runtime_rand}: a random 64-bit number (temporary file names). */
    @TruffleBoundary
    static long os_runtime_rand(Node node) {
        return ThreadLocalRandom.current().nextLong();
    }

    /**
     * {@code os.sigpipe}: gc raises SIGPIPE at itself after a write to a broken stdout or
     * stderr pipe, and the process dies of the signal. A JVM cannot die of SIGPIPE (HotSpot
     * owns the handler), so the process exits with the status a shell reports for that death,
     * 128 + SIGPIPE (docs/arch/limitations.md).
     */
    @TruffleBoundary
    static void os_sigpipe(Node node) {
        RuntimeIntrinsics.syscall_Exit(node, 128 + 13);
    }

    /**
     * {@code os.ignoreSIGSYS}/{@code restoreSIGSYS} bracket the pidfd probe of
     * {@code os.checkPidfd}, whose clone(2) fails here before any signal could be raised
     * ({@code syscall.rawVforkSyscall}).
     */
    static void os_ignoreSIGSYS(Node node) {
    }

    static void os_restoreSIGSYS(Node node) {
    }
}
