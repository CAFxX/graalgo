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
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.atomic.AtomicInteger;

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
 * carrier pool while the goroutine's virtual thread waits unmounted, so a blocked read pins no
 * carrier; the slot is re-acquired through the wake lane after the call. Other calls run
 * inline: they return without waiting on anything.
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

    private static final AtomicInteger THREADS = new AtomicInteger();
    /** Platform threads for may-block calls: outside the goroutine carrier pool. */
    private static final ExecutorService OFF_POOL = Executors.newCachedThreadPool(r -> {
        Thread t = new Thread(r, "graalgo-syscall-" + THREADS.incrementAndGet());
        t.setDaemon(true);
        return t;
    });

    /** The raw result: {@code r1} and {@code errno} (0 on success). */
    private record Raw(long r1, long errno) {}

    private static Raw invoke(long nr, long[] w) {
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
        MemorySegment[] segs = new MemorySegment[roles.length];
        Raw raw;
        try (Arena arena = Arena.ofConfined()) {
            for (int i = 0; i < roles.length; i++) {
                Role r = roles[i];
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
                    long n = a[r.lenArg];
                    if (n < 0 || n > room) {
                        throw fail(node, e, i, "is a buffer of " + n + " bytes in a Go object with " + room
                                + " bytes left");
                    }
                    b = new byte[(int) n];
                    // Out buffers are copied in as well: an unfilled tail reads back unchanged.
                    Unsafe.readBytes(node, obj, off, b, b.length);
                }
                MemorySegment seg = arena.allocate(Math.max(b.length, 1));
                MemorySegment.copy(b, 0, seg, ValueLayout.JAVA_BYTE, 0, b.length);
                objs[i] = obj;
                offs[i] = off;
                data[i] = b;
                segs[i] = seg;
                w[i] = seg.address();
            }
            raw = call(e, nr, w);
            if (raw.errno == 0) {
                for (int i = 0; i < roles.length; i++) {
                    Role r = roles[i];
                    if (segs[i] == null || r.kind != Role.K_BYTES || !r.out) {
                        continue;
                    }
                    int n = data[i].length;
                    int filled = r.filledResult ? (int) Math.max(0, Math.min(raw.r1, n)) : n;
                    MemorySegment.copy(segs[i], ValueLayout.JAVA_BYTE, 0, data[i], 0, filled);
                    Unsafe.writeBytes(node, objs[i], offs[i], data[i], filled);
                }
            }
        }
        return new Object[] {raw.r1, 0L, raw.errno};
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

    /** The downcall, with the slot released and off the carrier pool when the call may block. */
    private static Raw call(Entry e, long nr, long[] w) {
        SchedulerCore sched = SchedulerCore.installedOrNull();
        if (!e.mayBlock || sched == null || !sched.onGoroutine()) {
            return invoke(nr, w);
        }
        sched.ioEnter();
        try {
            Future<Raw> f = OFF_POOL.submit(() -> invoke(nr, w));
            boolean interrupted = false;
            try {
                for (;;) {
                    try {
                        // The virtual thread unmounts while it waits: the carrier is free.
                        return f.get();
                    } catch (InterruptedException ie) {
                        interrupted = true; // the native call cannot be cancelled; its buffers stay live
                    } catch (ExecutionException ee) {
                        throw new IllegalStateException(ee.getCause());
                    }
                }
            } finally {
                if (interrupted) {
                    Thread.currentThread().interrupt();
                }
            }
        } finally {
            sched.ioExit();
        }
    }
}
