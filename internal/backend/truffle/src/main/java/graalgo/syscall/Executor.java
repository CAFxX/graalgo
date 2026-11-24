package graalgo.syscall;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.Unsafe;
import graalgo.go.values.Flat;
import graalgo.sched.SchedulerCore;
import java.lang.foreign.Arena;
import java.lang.foreign.FunctionDescriptor;
import java.lang.foreign.Linker;
import java.lang.foreign.MemoryLayout;
import java.lang.foreign.MemorySegment;
import java.lang.foreign.StructLayout;
import java.lang.foreign.ValueLayout;
import java.lang.invoke.MethodHandle;
import java.lang.invoke.VarHandle;

/**
 * The syscall executor (toolexec-design §5.6, W8a; abi-spike.md §2): the one place where a Go
 * program's system calls reach the kernel. {@code internal/runtime/syscall/linux.Syscall6} —
 * under {@code syscall.Syscall}/{@code RawSyscall} and everything above them — arrives here
 * with the number and six words; the role table ({@link SyscallTable}, generated from and
 * checked against the standard library's source) says which words are pointers. A pointer word
 * is a {@code uintptr} handle of the pointer model (GG-0062): it resolves to a Go object and a
 * byte offset, the bytes are copied into a call-scoped native arena, the call is one Panama
 * downcall to libc {@code syscall(2)} with {@code errno} captured, and written buffers are
 * copied back. No Go object is pinned and no native address outlives the call.
 *
 * <p>Loud failures, never a guess: a number missing from the table, a pointer word that is not
 * inside a live Go object, a buffer longer than its object, a C string without its NUL — each
 * is a fatal error.
 *
 * <p>Scheduling (GG-SCHED-0006): a call whose entry may block releases the goroutine's
 * scheduler slot ({@link SchedulerCore#ioEnter}) and runs on a platform thread outside the
 * carrier pool ({@link BlockingCall}); the slot is re-acquired through the wake lane after the
 * call. Other calls run inline: they return without waiting on anything. A may-block call is
 * cancellable (ADR-0019): the goroutine's wait is Truffle-blocked, and its native buffers belong
 * to the call, so a context cancel, hard exit or scheduler teardown ends the wait at once while
 * the buffers stay valid until the kernel is done with them.
 */
public final class Executor {
    private Executor() {}

    private static final Linker LINKER = Linker.nativeLinker();
    private static final StructLayout CAPTURE = Linker.Option.captureStateLayout();
    private static final VarHandle ERRNO = CAPTURE.varHandle(MemoryLayout.PathElement.groupElement("errno"));
    private static final MethodHandle SYSCALL;

    static {
        ValueLayout.OfLong j = ValueLayout.JAVA_LONG;
        SYSCALL = LINKER.downcallHandle(LINKER.defaultLookup().find("syscall").orElseThrow(),
                FunctionDescriptor.of(j, j, j, j, j, j, j, j),
                Linker.Option.firstVariadicArg(1), Linker.Option.captureCallState("errno"));
    }

    /** A write of at most this many bytes to a writable pipe does not wait (POSIX PIPE_BUF on linux). */
    private static final long PIPE_BUF = 4096;
    static final short POLLIN = 0x1;
    static final short POLLOUT = 0x4;
    static final MethodHandle POLL = LINKER.downcallHandle(LINKER.defaultLookup().find("poll").orElseThrow(),
            FunctionDescriptor.of(ValueLayout.JAVA_INT, ValueLayout.ADDRESS, ValueLayout.JAVA_LONG, ValueLayout.JAVA_INT));

    /**
     * Whether {@code fd} is readable ('r') or writable ('w') now: {@code poll(2)} with a zero
     * timeout. An error condition on the descriptor counts as ready — the call reports it at once.
     */
    private static boolean readyNow(long fd, int mode) {
        try (Arena a = Arena.ofConfined()) {
            // struct pollfd { int fd; short events; short revents; }
            MemorySegment p = a.allocate(8, 4);
            p.set(ValueLayout.JAVA_INT, 0, (int) fd);
            p.set(ValueLayout.JAVA_SHORT, 4, mode == 'r' ? POLLIN : POLLOUT);
            int n = (int) POLL.invokeExact(p, 1L, 0);
            return n > 0 && p.get(ValueLayout.JAVA_SHORT, 6) != 0;
        } catch (Throwable t) {
            throw new IllegalStateException("poll downcall failed", t);
        }
    }

    /** The raw result: {@code r1} and {@code errno} (0 on success). */
    record Raw(long r1, long errno) {}

    static Raw invoke(long nr, long[] w) {
        try (Arena a = Arena.ofConfined()) {
            MemorySegment state = a.allocate(CAPTURE);
            long r = (long) SYSCALL.invokeExact(state, nr, w[0], w[1], w[2], w[3], w[4], w[5]);
            if (r == -1) {
                return new Raw(-1, (int) ERRNO.get(state, 0L));
            }
            return new Raw(r, 0);
        } catch (Throwable t) {
            throw new IllegalStateException("syscall " + nr + " downcall failed", t);
        }
    }

    /**
     * {@code linux.Syscall6(nr, a1..a6)}: returns {@code {r1, r2, errno}} as gc's assembly does —
     * on failure {@code r1 = -1} (all ones as a uintptr), {@code r2 = 0}, {@code errno > 0}.
     */
    @TruffleBoundary
    public static Object[] syscall6(Node node, long nr, long a1, long a2, long a3, long a4, long a5, long a6) {
        Entry e = SyscallTable.get(nr);
        if (e == null) {
            throw GoProcess.get(node).fatal("graalgo: system call " + nr
                    + " is not in the syscall role table (internal/toolexec/syscalls/table.txt)");
        }
        long[] a = {a1, a2, a3, a4, a5, a6};
        long[] w = a.clone();
        Role[] roles = e.roles;
        Flat[] objs = new Flat[roles.length];
        long[] offs = new long[roles.length];
        byte[][] data = new byte[roles.length][];
        long total = 0;
        for (int i = 0; i < roles.length; i++) {
            Role r = roles[i];
            if (r.allowed != null && !listed(r.allowed, a[i])) {
                throw fail(node, e, i, "is " + a[i] + ", a selector the role table does not list");
            }
            if (r.kind == Role.K_VAL || a[i] == 0) {
                continue; // a value, or a nil pointer (the kernel answers EFAULT where it matters)
            }
            Object p = Unsafe.fromInt(node, a[i]);
            if (!Unsafe.isFlatPointer(p)) {
                throw fail(node, e, i, "is not a pointer into Go memory");
            }
            Flat obj = Unsafe.referent(p);
            long off = Unsafe.offset(p);
            long room = Unsafe.extent(obj) - off;
            byte[] b;
            if (r.kind == Role.K_CSTR) {
                b = cString(node, e, i, obj, off, room);
            } else {
                long n = r.kind == Role.K_STRUCT ? r.size : a[r.lenArg];
                if (n < 0 || n > room) {
                    throw fail(node, e, i, "is a buffer of " + n + " bytes in a Go object with " + room
                            + " bytes left");
                }
                b = new byte[(int) n];
                // Out buffers are copied in as well: an unfilled tail reads back unchanged.
                Unsafe.readBytes(node, obj, off, b, b.length);
            }
            objs[i] = obj;
            offs[i] = off;
            data[i] = b;
            total += align(Math.max(b.length, 1));
        }
        SchedulerCore sched = offPool(e, w);
        if (sched != null) {
            // May block: the native buffers belong to the call, not to this goroutine, so a
            // cancelled wait can leave while the kernel may still write them (BlockingCall).
            return BlockingCall.run(node, sched, e, nr, w, roles, objs, offs, data, total);
        }
        Raw raw;
        try (Arena arena = Arena.ofConfined()) {
            MemorySegment[] segs = new MemorySegment[roles.length];
            for (int i = 0; i < roles.length; i++) {
                if (data[i] != null) {
                    MemorySegment seg = arena.allocate(Math.max(data[i].length, 1));
                    MemorySegment.copy(data[i], 0, seg, ValueLayout.JAVA_BYTE, 0, data[i].length);
                    segs[i] = seg;
                    w[i] = seg.address();
                }
            }
            raw = invoke(nr, w);
            copyOut(node, raw, roles, objs, offs, data, segs);
        }
        return new Object[] {raw.r1, 0L, raw.errno};
    }

    /**
     * A system call whose three argument words are integers by the Go signature of its caller
     * (the runtime's own {@code fcntl}); never blocks. Returns {@code {r1, errno}}.
     */
    @TruffleBoundary
    public static long[] values(long nr, long a1, long a2, long a3) {
        Raw raw = invoke(nr, new long[] {a1, a2, a3, 0, 0, 0});
        return new long[] {raw.r1, raw.errno};
    }

    private static boolean listed(long[] allowed, long v) {
        for (long x : allowed) {
            if (x == v) {
                return true;
            }
        }
        return false;
    }

    /** The bytes of the C string at {@code off} with its NUL, which must lie inside the object. */
    private static byte[] cString(Node node, Entry e, int i, Flat obj, long off, long room) {
        int max = (int) Math.min(room, Integer.MAX_VALUE);
        byte[] all = new byte[max];
        Unsafe.readBytes(node, obj, off, all, max);
        for (int k = 0; k < max; k++) {
            if (all[k] == 0) {
                return java.util.Arrays.copyOf(all, k + 1);
            }
        }
        throw fail(node, e, i, "is a C string without a NUL inside its Go object");
    }

    private static RuntimeException fail(Node node, Entry e, int i, String what) {
        return GoProcess.get(node).fatal("graalgo: system call " + e.name + ": argument " + (i + 1) + " " + what);
    }

    /**
     * Copies the out buffers back into their Go objects after a successful call ({@code segs}:
     * the native copies, by argument).
     */
    static void copyOut(Node node, Raw raw, Role[] roles, Flat[] objs, long[] offs, byte[][] data,
            MemorySegment[] segs) {
        if (raw.errno != 0) {
            return;
        }
        for (int i = 0; i < roles.length; i++) {
            Role r = roles[i];
            if (segs[i] == null || r.kind == Role.K_VAL || r.kind == Role.K_CSTR || !r.out) {
                continue;
            }
            int n = data[i].length;
            int filled = r.filledResult ? (int) Math.max(0, Math.min(raw.r1, n)) : n;
            MemorySegment.copy(segs[i], ValueLayout.JAVA_BYTE, 0, data[i], 0, filled);
            Unsafe.writeBytes(node, objs[i], offs[i], data[i], filled);
        }
    }

    /** Native buffers are 16-byte aligned inside one call's block. */
    static long align(long n) {
        return (n + 15) & ~15L;
    }

    /**
     * The scheduler of the calling goroutine when the call must leave the carrier pool (its
     * entry may block and the descriptor is not ready now), else null: the call runs inline.
     */
    private static SchedulerCore offPool(Entry e, long[] w) {
        SchedulerCore sched = SchedulerCore.installedOrNull();
        if (!e.mayBlock || sched == null || !sched.onGoroutine()) {
            return null;
        }
        if (e.ready != 0 && (e.ready == 'r' || w[e.lenArg] <= PIPE_BUF) && readyNow(w[0], e.ready)) {
            // The descriptor is ready: the transfer returns without waiting (a regular file,
            // a terminal, a pipe with data or room), so it needs neither the slot release nor
            // the thread hand-off. Another process may take the readiness between the poll and
            // the call; the call then waits on the carrier for that one transfer.
            return null;
        }
        return sched;
    }
}
