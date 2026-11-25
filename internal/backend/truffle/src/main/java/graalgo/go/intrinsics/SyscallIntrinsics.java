package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.GoProcess;
import graalgo.go.types.GoType;
import graalgo.go.values.Flat;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/**
 * The runtime side of package {@code syscall}: the raw entry {@code linux.Syscall6} under every
 * {@code syscall.Syscall}/{@code RawSyscall} (executed by {@link graalgo.syscall.Executor}),
 * the {@code entersyscall}/{@code exitsyscall} bracket, the environment and the page size;
 * registered in {@code internal/toolexec/intrinsics/registry.txt}. {@code syscall} itself is
 * upstream's source.
 */
final class SyscallIntrinsics {
    private SyscallIntrinsics() {}

    /** {@code internal/runtime/syscall/linux.Syscall6}: see {@link graalgo.syscall.Executor}. */
    static Object internal_runtime_syscall_linux_Syscall6(Node node, long nr, long a1, long a2, long a3, long a4,
            long a5, long a6) {
        return graalgo.syscall.Executor.syscall6(node, nr, a1, a2, a3, a4, a5, a6);
    }

    /**
     * {@code syscall.rawSyscallNoError(trap, a1, a2, a3) (r1, r2)}: upstream's assembly entry for
     * the calls that cannot fail (getpid, getuid, ...); through the role table like every other.
     */
    static Object syscall_rawSyscallNoError(Node node, long trap, long a1, long a2, long a3) {
        Object[] r = graalgo.syscall.Executor.syscall6(node, trap, a1, a2, a3, 0, 0, 0);
        return new Object[] {r[0], r[1]};
    }

    /**
     * {@code syscall.rawVforkSyscall(trap, a1, a2, a3) (r1, err)}: the clone(2) of fork/exec.
     * A JVM process cannot fork; GG-0064 replaces the fork/exec glue by a posix_spawn helper.
     * Until then the call fails with ENOSYS, so {@code os.StartProcess} returns an error
     * (docs/arch/limitations.md).
     */
    static Object syscall_rawVforkSyscall(Node node, long trap, long a1, long a2, long a3) {
        return new Object[] {0L, 38L};
    }

    /**
     * {@code runtime.fcntl(fd, cmd, arg) (ret, errno)}, which internal/syscall/unix reaches by
     * {@code //go:linkname}: three integers by its signature, so no word is a pointer.
     */
    static Object runtime_fcntl(Node node, long fd, long cmd, long arg) {
        long[] r = graalgo.syscall.Executor.values(72, fd, cmd, arg);
        return r[1] != 0 ? new Object[] {-1L, r[1]} : new Object[] {(long) (int) r[0], 0L};
    }

    /**
     * gc's {@code entersyscall}/{@code exitsyscall} release and re-acquire the P around every
     * {@code syscall.Syscall}. Here the executor brackets by the role table's may-block bit
     * (slot released, carrier left; GG-SCHED-0006), which covers the calls that can block and
     * spares the ones that cannot, so the bracket itself has nothing to do.
     */
    static void runtime_entersyscall(Node node) {
    }

    static void runtime_exitsyscall(Node node) {
    }

    /** {@code syscall.Getpagesize}: linux/amd64's page size. */
    static long syscall_Getpagesize(Node node) {
        return 4096;
    }

    /**
     * {@code syscall.runtime_envs}: the process environment in the order the kernel gave it
     * (gc reads it from the initial stack; here from {@code /proc/self/environ}).
     */
    @TruffleBoundary
    static Object syscall_runtime_envs(Node node) {
        List<byte[]> envs = new ArrayList<>();
        try {
            byte[] all = Files.readAllBytes(Path.of("/proc/self/environ"));
            int start = 0;
            for (int i = 0; i < all.length; i++) {
                if (all[i] == 0) {
                    if (i > start) {
                        envs.add(java.util.Arrays.copyOfRange(all, start, i));
                    }
                    start = i + 1;
                }
            }
        } catch (IOException e) {
            for (Map.Entry<String, String> kv : System.getenv().entrySet()) {
                envs.add((kv.getKey() + "=" + kv.getValue()).getBytes(java.nio.charset.StandardCharsets.UTF_8));
            }
        }
        return stringSlice(node, envs);
    }

    private static Slice stringSlice(Node node, List<byte[]> elems) {
        GoType st = GoProcess.get(node).types.basic(GoType.STRING);
        if (st.nRef() != 1 || st.nPrim() != 0 || st.nByte() != 0) {
            throw new IllegalStateException("string is not one reference slot");
        }
        int n = elems.size();
        Object[] refs = new Object[n];
        for (int i = 0; i < n; i++) {
            refs[i] = elems.get(i).length == 0 ? Str.EMPTY : new Str(elems.get(i));
        }
        return new Slice(new Flat(st.backing(), Flat.NO_PRIMS, refs, Flat.NO_BYTES), 0, 0, 0, n, n);
    }

    /**
     * {@code runtime.gogetenv(key)}: the variable as the process started with it (gc reads its
     * startup copy of the environment, which {@code os.Setenv} does not change); "" if unset.
     */
    @TruffleBoundary
    static Str runtime_gogetenv(Node node, Str key) {
        String s = System.getenv(key.toString());
        return s == null || s.isEmpty() ? Str.EMPTY : Str.of(s);
    }

    /**
     * {@code runtime.vgetrandom(p []byte, flags uint32) (ret int, supported bool)}, which
     * {@code internal/syscall/unix.GetRandom} tries before the system call: there is no vDSO
     * state here (gc's answer without the vDSO function), so callers make the
     * {@code getrandom} system call.
     */
    static Object runtime_vgetrandom(Node node, Object p, long flags) {
        return new Object[] {-1L, false};
    }
}
