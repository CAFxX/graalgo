package graalgo.syscall;

import com.oracle.truffle.api.TruffleSafepoint;
import com.oracle.truffle.api.nodes.Node;
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
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.locks.LockSupport;

/**
 * One may-block system call, cancellable (ADR-0019; WP-TR-AUDIT-3 of truffle-audit.md).
 *
 * <p>The downcall runs on a platform thread outside the carrier pool ({@link #OFF_POOL}) while
 * the goroutine waits. Three rules make the wait cancellable without ever freeing memory the
 * kernel may still write:
 *
 * <ol>
 *   <li><b>The call owns its native buffers.</b> They are one {@code malloc} block, freed by
 *       whichever side finishes last: the goroutine after it copied the results out, or — when
 *       the goroutine left first — the off-pool thread once the kernel returned. No arena tied
 *       to the goroutine's thread or lifetime is involved.
 *   <li><b>The wait is Truffle-blocked.</b> Inside a context the goroutine's thread (its
 *       carrier in carrier mode, where the goroutine is pinned from {@code ioEnter} to
 *       {@code ioExit}) waits in {@link TruffleSafepoint#setBlockedThreadInterruptible}: a
 *       thread-local action runs and the wait resumes; the context's cancellation or hard exit
 *       throws out of it, and the goroutine leaves (the call is abandoned).
 *   <li><b>The kernel wait is cancellable where the kernel lets it be.</b> A transfer on a
 *       descriptor ({@code read}/{@code write}/{@code pread64}/{@code pwrite64}: the table's
 *       {@code rd}/{@code wr}) first waits in {@code poll(2)} for the descriptor <em>or</em>
 *       the scheduler's abort descriptor (an {@code eventfd}, {@link Aborter}); an abort makes
 *       it return {@code EINTR} without issuing the transfer, so the off-pool thread ends too.
 *       Other may-block calls ({@code openat} of a FIFO, {@code fsync}) cannot be woken: the
 *       goroutine still leaves at once, the thread finishes when the kernel returns.
 * </ol>
 *
 * <p>An abandoned call aborts every pending call of its scheduler: abandonment only happens when
 * the program is being torn down (context cancel or exit, scheduler termination).
 */
final class BlockingCall implements Runnable {
    private static final Linker LINKER = Linker.nativeLinker();
    private static final MethodHandle MALLOC = LINKER.downcallHandle(LINKER.defaultLookup().find("malloc").orElseThrow(),
            FunctionDescriptor.of(ValueLayout.ADDRESS, ValueLayout.JAVA_LONG));
    private static final MethodHandle FREE = LINKER.downcallHandle(LINKER.defaultLookup().find("free").orElseThrow(),
            FunctionDescriptor.ofVoid(ValueLayout.ADDRESS));
    private static final StructLayout CAPTURE = Linker.Option.captureStateLayout();
    private static final VarHandle ERRNO = CAPTURE.varHandle(MemoryLayout.PathElement.groupElement("errno"));
    /** {@code poll(2)} with {@code errno}. */
    private static final MethodHandle POLL_ERRNO = LINKER.downcallHandle(
            LINKER.defaultLookup().find("poll").orElseThrow(),
            FunctionDescriptor.of(ValueLayout.JAVA_INT, ValueLayout.ADDRESS, ValueLayout.JAVA_LONG, ValueLayout.JAVA_INT),
            Linker.Option.captureCallState("errno"));

    private static final long EINTR = 4;

    private static final AtomicInteger THREADS = new AtomicInteger();
    /**
     * Platform threads for may-block calls: outside the goroutine carrier pool. Unbounded, as
     * gc's thread per goroutine blocked in a system call is (gc stops at {@code
     * debug.SetMaxThreads}, 10,000 by default, with a fatal error; here the JVM's own thread
     * limit is the bound, and a call whose thread cannot be created fails as an internal error
     * of its goroutine — the program is not torn down for it).
     */
    private static final ExecutorService OFF_POOL = Executors.newCachedThreadPool(r -> {
        Thread t = new Thread(r, "graalgo-syscall-" + THREADS.incrementAndGet());
        t.setDaemon(true);
        return t;
    });

    private static final int RUNNING = 0;
    private static final int DONE = 1;
    private static final int ABANDONED = 2;

    /** The goroutine's wait as a Truffle {@code Interruptible} (rule 2). */
    private static final TruffleSafepoint.Interruptible<BlockingCall> AWAIT = BlockingCall::awaitInterruptibly;

    private final Entry e;
    private final long nr;
    private final long[] w;
    /** The native buffers (rule 1); freed exactly once, by {@link #release}. */
    private final MemorySegment block;
    private final Aborter aborter;
    private final Thread waiter;
    private final AtomicInteger state = new AtomicInteger(RUNNING);
    /** The abort descriptor read by the off-pool thread (open while this call is in flight). */
    private int abortFd = -1;
    private boolean submitted;
    private volatile Executor.Raw raw;
    private volatile Throwable failure;

    private BlockingCall(Entry e, long nr, long[] w, MemorySegment block, Aborter aborter) {
        this.e = e;
        this.nr = nr;
        this.w = w;
        this.block = block;
        this.aborter = aborter;
        this.waiter = Thread.currentThread();
    }

    /**
     * Runs a may-block call for the calling goroutine of {@code sched}: buffers in, the
     * downcall off the pool with the scheduler slot released, buffers out. Returns
     * {@code {r1, 0, errno}} as {@link Executor#syscall6}.
     */
    static Object[] run(Node node, SchedulerCore sched, Entry e, long nr, long[] w, Role[] roles, Flat[] objs,
            long[] offs, byte[][] data, long total) {
        return run(node, sched, e, nr, w, roles, objs, offs, data, total, OFF_POOL);
    }

    /** {@link #run} on {@code offPool} (tests: a pool that refuses the call). */
    static Object[] run(Node node, SchedulerCore sched, Entry e, long nr, long[] w, Role[] roles, Flat[] objs,
            long[] offs, byte[][] data, long total, java.util.concurrent.Executor offPool) {
        MemorySegment block = malloc(Math.max(total, 16));
        MemorySegment[] segs = new MemorySegment[roles.length];
        long at = 0;
        for (int i = 0; i < roles.length; i++) {
            if (data[i] != null) {
                MemorySegment seg = block.asSlice(at, Math.max(data[i].length, 1));
                MemorySegment.copy(data[i], 0, seg, ValueLayout.JAVA_BYTE, 0, data[i].length);
                segs[i] = seg;
                w[i] = seg.address();
                at += Executor.align(Math.max(data[i].length, 1));
            }
        }
        BlockingCall c = new BlockingCall(e, nr, w, block, Aborter.of(sched));
        boolean done = false;
        boolean notStarted = false;
        try {
            sched.ioEnter();
            if (c.aborter.enter(c)) {
                try {
                    offPool.execute(c);
                } catch (RuntimeException | Error x) {
                    // The call never started (no thread for it: RejectedExecutionException,
                    // OutOfMemoryError): the program is live, so nothing is aborted — this
                    // goroutine still owns the buffers, the call leaves the in-flight set (or the
                    // abort descriptor could never close), the slot is re-acquired, and the
                    // failure propagates as an internal error (BUG-0057).
                    notStarted = true;
                    c.aborter.leave(c);
                    free(block);
                    sched.ioExit();
                    throw x;
                }
                c.submitted = true;
                c.await(sched);
                done = c.state.get() == DONE;
            }
        } finally {
            if (!done && !notStarted) {
                // Leaving early: a cancellation is in flight (rethrown), or the scheduler is
                // torn down. The program is over; the off-pool thread owns the buffers now.
                c.abandon();
                sched.ioAbandon();
            }
        }
        if (!done) {
            sched.freezeForExit(); // throws: the scheduler is torn down (or parks until it is)
            throw new IllegalStateException("graalgo: a torn-down goroutine resumed after a system call");
        }
        try {
            sched.ioExit();
            Throwable f = c.failure;
            if (f != null) {
                throw new IllegalStateException("syscall " + nr + " failed off the carrier pool", f);
            }
            Executor.Raw r = c.raw;
            Executor.copyOut(node, r, roles, objs, offs, data, segs);
            return new Object[] {r.r1(), 0L, r.errno()};
        } finally {
            free(block);
        }
    }

    /** Waits until the call is done or the scheduler aborted its calls (rule 2). */
    private void await(SchedulerCore sched) {
        if (sched.inContext()) {
            TruffleSafepoint.setBlockedThreadInterruptible(null, AWAIT, this);
            return;
        }
        while (state.get() == RUNNING && !aborter.aborted) {
            LockSupport.park(this);
        }
    }

    private void awaitInterruptibly() throws InterruptedException {
        while (state.get() == RUNNING && !aborter.aborted) {
            LockSupport.park(this);
            if (Thread.interrupted()) {
                throw new InterruptedException();
            }
        }
    }

    /** The goroutine leaves before the call is done (rule 1: the buffers go to whoever is last). */
    private void abandon() {
        if (!submitted) {
            free(block);
            return;
        }
        aborter.abort();
        if (!state.compareAndSet(RUNNING, ABANDONED)) {
            free(block); // the call finished meanwhile; the goroutine was last
        }
    }

    @Override
    public void run() {
        try {
            raw = e.ready != 0 ? gated() : Executor.invoke(nr, w);
        } catch (Throwable t) {
            failure = t;
        } finally {
            // Off the in-flight set (and done with the abort descriptor) BEFORE the goroutine can
            // see DONE: a goroutine that returned from its call never finds it still in flight.
            aborter.leave(this);
            if (state.compareAndSet(RUNNING, DONE)) {
                LockSupport.unpark(waiter);
            } else {
                free(block); // abandoned: this thread is last
            }
        }
    }

    /** Rule 3: wait for the descriptor or the abort, then transfer (or report EINTR). */
    private Executor.Raw gated() throws Throwable {
        short events = e.ready == 'r' ? Executor.POLLIN : Executor.POLLOUT;
        for (;;) {
            try (Arena a = Arena.ofConfined()) {
                // struct pollfd[2] { int fd; short events; short revents; }
                MemorySegment p = a.allocate(16, 4);
                p.set(ValueLayout.JAVA_INT, 0, (int) w[0]);
                p.set(ValueLayout.JAVA_SHORT, 4, events);
                p.set(ValueLayout.JAVA_INT, 8, abortFd);
                p.set(ValueLayout.JAVA_SHORT, 12, Executor.POLLIN);
                MemorySegment st = a.allocate(CAPTURE);
                int n = (int) POLL_ERRNO.invokeExact(st, p, 2L, -1);
                if (n < 0) {
                    int errno = (int) ERRNO.get(st, 0L);
                    if (errno == EINTR) {
                        continue;
                    }
                    break; // poll itself failed: fall back to the plain (blocking) transfer
                }
                if (p.get(ValueLayout.JAVA_SHORT, 14) != 0) {
                    return new Executor.Raw(-1, EINTR); // aborted: the transfer never happens
                }
                if (p.get(ValueLayout.JAVA_SHORT, 6) != 0) {
                    break; // ready (or an error condition the transfer reports)
                }
            }
        }
        return Executor.invoke(nr, w);
    }

    private static MemorySegment malloc(long n) {
        MemorySegment p;
        try {
            p = (MemorySegment) MALLOC.invokeExact(n);
        } catch (Throwable t) {
            throw new IllegalStateException("malloc downcall failed", t);
        }
        if (p.address() == 0) {
            throw new OutOfMemoryError("graalgo: no native memory for a " + n + "-byte system call buffer");
        }
        return p.reinterpret(n);
    }

    private static void free(MemorySegment p) {
        try {
            FREE.invokeExact(p);
        } catch (Throwable t) {
            throw new IllegalStateException("free downcall failed", t);
        }
    }

    /**
     * The abort side of one scheduler's may-block calls: an {@code eventfd} that every gated
     * call polls next to its descriptor, created on first use and signalled once, when the
     * scheduler is torn down ({@link SchedulerCore#terminate}) or a call is abandoned. It is
     * closed once aborted and no call that may still poll it is in flight.
     */
    static final class Aborter implements Runnable {
        private static final MethodHandle EVENTFD = LINKER.downcallHandle(
                LINKER.defaultLookup().find("eventfd").orElseThrow(),
                FunctionDescriptor.of(ValueLayout.JAVA_INT, ValueLayout.JAVA_INT, ValueLayout.JAVA_INT));
        private static final MethodHandle WRITE = LINKER.downcallHandle(
                LINKER.defaultLookup().find("write").orElseThrow(),
                FunctionDescriptor.of(ValueLayout.JAVA_LONG, ValueLayout.JAVA_INT, ValueLayout.ADDRESS,
                        ValueLayout.JAVA_LONG));
        private static final MethodHandle CLOSE = LINKER.downcallHandle(
                LINKER.defaultLookup().find("close").orElseThrow(),
                FunctionDescriptor.of(ValueLayout.JAVA_INT, ValueLayout.JAVA_INT));
        private static final int EFD_CLOEXEC = 0x80000;
        private static final int EFD_NONBLOCK = 0x800;

        /** Set once; then no call starts and every pending one ends. */
        volatile boolean aborted;
        /** The eventfd, or -1 (none yet / closed); guarded by this. */
        private int fd = -1;
        /** Calls that may still poll {@link #fd} or whose goroutine waits; guarded by this. */
        private final List<BlockingCall> inflight = new ArrayList<>();

        /** Calls in flight now (tests). */
        synchronized int inflightCount() {
            return inflight.size();
        }

        /** The scheduler's aborter, created (and hooked into its teardown) on first use. */
        static Aborter of(SchedulerCore sched) {
            return (Aborter) sched.syscallAborter(Aborter::new);
        }

        /** Registers a call about to be submitted; false once aborted (the call never runs). */
        synchronized boolean enter(BlockingCall c) {
            if (aborted) {
                return false;
            }
            if (c.e.ready != 0) {
                if (fd < 0) {
                    fd = eventfd();
                }
                c.abortFd = fd;
            }
            inflight.add(c);
            return true;
        }

        /** A call's off-pool thread is done with it (and with {@link #fd}). */
        synchronized void leave(BlockingCall c) {
            inflight.remove(c);
            if (aborted && inflight.isEmpty()) {
                closeFd();
            }
        }

        /** Aborts every pending and future call of the scheduler (idempotent). */
        @Override
        public synchronized void run() {
            abort();
        }

        synchronized void abort() {
            if (aborted) {
                return;
            }
            aborted = true;
            if (fd >= 0) {
                signal(fd);
            }
            for (BlockingCall c : inflight) {
                LockSupport.unpark(c.waiter);
            }
            if (inflight.isEmpty()) {
                closeFd();
            }
        }

        private void closeFd() {
            if (fd >= 0) {
                try {
                    int r = (int) CLOSE.invokeExact(fd);
                    assert r == 0 : "close(eventfd) failed";
                } catch (Throwable t) {
                    throw new IllegalStateException("close downcall failed", t);
                }
                fd = -1;
            }
        }

        private static int eventfd() {
            int fd;
            try {
                fd = (int) EVENTFD.invokeExact(0, EFD_CLOEXEC | EFD_NONBLOCK);
            } catch (Throwable t) {
                throw new IllegalStateException("eventfd downcall failed", t);
            }
            if (fd < 0) {
                throw new IllegalStateException("graalgo: eventfd failed: cannot make blocking system calls cancellable");
            }
            return fd;
        }

        private static void signal(int fd) {
            try (Arena a = Arena.ofConfined()) {
                MemorySegment one = a.allocate(ValueLayout.JAVA_LONG);
                one.set(ValueLayout.JAVA_LONG, 0, 1L);
                long n = (long) WRITE.invokeExact(fd, one, 8L);
                if (n != 8) {
                    throw new IllegalStateException("graalgo: cannot signal the system-call abort eventfd");
                }
            } catch (RuntimeException | Error e) {
                throw e;
            } catch (Throwable t) {
                throw new IllegalStateException("write downcall failed", t);
            }
        }
    }
}
