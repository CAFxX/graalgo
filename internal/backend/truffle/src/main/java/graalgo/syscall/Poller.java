package graalgo.syscall;

import graalgo.sched.GoroutineHandle;
import graalgo.jfr.ParkReason;
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
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicLong;

/**
 * The runtime poller (SPEC §3.5, GG-0063): gc's netpoller for raw descriptors. One epoll
 * instance, one daemon platform thread in {@code epoll_wait}; {@code internal/poll}'s
 * {@code runtime_poll*} functions are its operations (upstream runtime/netpoll.go, whose
 * contract this follows: edge-triggered registration for read and write, one waiter per
 * direction, deadlines, the error codes of {@code pollWait}).
 *
 * <p>A goroutine that waits for readiness parks through the scheduler
 * ({@link ParkReason#NETPOLL}: no carrier and no in-flight slot while blocked, GG-SCHED-0006)
 * and is unparked by the poller thread, by a deadline on the scheduler's timer heap, or by
 * {@link #unblock}. A waiter is unparked exactly once per registration: every wake clears the
 * registration under the descriptor's monitor.
 */
public final class Poller {
    private Poller() {}

    /** {@code pollWait}/{@code pollReset} results (upstream's pollNoError .. pollErrNotPollable). */
    public static final int OK = 0;
    public static final int ERR_CLOSING = 1;
    public static final int ERR_TIMEOUT = 2;
    public static final int ERR_NOT_POLLABLE = 3;

    private static final int EPOLLIN = 0x1;
    private static final int EPOLLOUT = 0x4;
    private static final int EPOLLERR = 0x8;
    private static final int EPOLLHUP = 0x10;
    private static final int EPOLLRDHUP = 0x2000;
    private static final int EPOLLET = 1 << 31;
    private static final int EPOLL_CLOEXEC = 0x80000;
    private static final int EPOLL_CTL_ADD = 1;
    private static final int EPOLL_CTL_DEL = 2;
    private static final int EINTR = 4;
    /** {@code struct epoll_event} on amd64: packed {@code uint32 events; uint64 data}. */
    private static final int EVENT_SIZE = 12;
    private static final int MAX_EVENTS = 128;

    private static final Linker LINKER = Linker.nativeLinker();
    private static final StructLayout CAPTURE = Linker.Option.captureStateLayout();
    private static final VarHandle ERRNO = CAPTURE.varHandle(MemoryLayout.PathElement.groupElement("errno"));
    private static final MethodHandle CREATE1;
    private static final MethodHandle CTL;
    private static final MethodHandle WAIT;

    static {
        ValueLayout.OfInt i = ValueLayout.JAVA_INT;
        Linker.Option errno = Linker.Option.captureCallState("errno");
        CREATE1 = LINKER.downcallHandle(LINKER.defaultLookup().find("epoll_create1").orElseThrow(),
                FunctionDescriptor.of(i, i), errno);
        CTL = LINKER.downcallHandle(LINKER.defaultLookup().find("epoll_ctl").orElseThrow(),
                FunctionDescriptor.of(i, i, i, i, ValueLayout.ADDRESS), errno);
        WAIT = LINKER.downcallHandle(LINKER.defaultLookup().find("epoll_wait").orElseThrow(),
                FunctionDescriptor.of(i, i, ValueLayout.ADDRESS, i, i), errno);
    }

    /** gc's pollDesc. Guarded by its own monitor. */
    private static final class Desc {
        final long id;
        final int fd;
        boolean closing;
        final boolean[] ready = new boolean[2];
        final GoroutineHandle[] waiter = new GoroutineHandle[2];
        /** 0: none; -1: expired; else the deadline in {@code System.nanoTime} units. */
        final long[] deadline = new long[2];
        final long[] timer = {-1, -1};
        /** Bumped by every setDeadline: a timer of an older sequence does nothing. */
        final long[] seq = new long[2];

        Desc(long id, int fd) {
            this.id = id;
            this.fd = fd;
        }
    }

    private static final int R = 0;
    private static final int W = 1;

    private static final ConcurrentHashMap<Long, Desc> DESCS = new ConcurrentHashMap<>();
    private static final AtomicLong NEXT = new AtomicLong(1);
    private static volatile int epfd = -1;

    /** {@code runtime_pollServerInit}: the epoll instance and its thread, once per process. */
    public static synchronized void init() {
        if (epfd >= 0) {
            return;
        }
        try (Arena a = Arena.ofConfined()) {
            MemorySegment state = a.allocate(CAPTURE);
            int fd = (int) CREATE1.invokeExact(state, EPOLL_CLOEXEC);
            if (fd < 0) {
                throw new IllegalStateException("graalgo: epoll_create1 failed, errno " + (int) ERRNO.get(state, 0L));
            }
            epfd = fd;
        } catch (RuntimeException e) {
            throw e;
        } catch (Throwable t) {
            throw new IllegalStateException("epoll_create1 downcall failed", t);
        }
        Thread t = new Thread(Poller::loop, "graalgo-netpoll");
        t.setDaemon(true);
        t.start();
    }

    /** Whether {@code fd} is the poller's own descriptor ({@code runtime_isPollServerDescriptor}). */
    public static boolean isPollDescriptor(long fd) {
        return epfd >= 0 && fd == epfd;
    }

    private static int ctl(int op, int fd, long id) {
        try (Arena a = Arena.ofConfined()) {
            MemorySegment state = a.allocate(CAPTURE);
            MemorySegment ev = a.allocate(EVENT_SIZE);
            ev.set(ValueLayout.JAVA_INT_UNALIGNED, 0, EPOLLIN | EPOLLOUT | EPOLLRDHUP | EPOLLET);
            ev.set(ValueLayout.JAVA_LONG_UNALIGNED, 4, id);
            int r = (int) CTL.invokeExact(state, epfd, op, fd, ev);
            return r < 0 ? (int) ERRNO.get(state, 0L) : 0;
        } catch (Throwable t) {
            throw new IllegalStateException("epoll_ctl downcall failed", t);
        }
    }

    /** {@code runtime_pollOpen(fd)}: {@code {handle, errno}}; the handle is 0 on failure. */
    public static long[] open(long fd) {
        long id = NEXT.getAndIncrement();
        Desc d = new Desc(id, (int) fd);
        DESCS.put(id, d);
        int errno = ctl(EPOLL_CTL_ADD, (int) fd, id);
        if (errno != 0) {
            DESCS.remove(id);
            return new long[] {0, errno};
        }
        return new long[] {id, 0};
    }

    private static Desc desc(long id) {
        Desc d = DESCS.get(id);
        if (d == null) {
            throw new IllegalStateException("graalgo: poll descriptor " + id + " is not open");
        }
        return d;
    }

    /** {@code runtime_pollClose}: the descriptor must have been unblocked and have no waiter. */
    public static void close(long id) {
        Desc d = desc(id);
        synchronized (d) {
            if (!d.closing) {
                throw new IllegalStateException("runtime: close polldesc w/o unblock");
            }
            if (d.waiter[R] != null || d.waiter[W] != null) {
                throw new IllegalStateException("runtime: blocked read or write on closing polldesc");
            }
            cancelTimer(d, R);
            cancelTimer(d, W);
        }
        ctl(EPOLL_CTL_DEL, d.fd, id);
        DESCS.remove(id);
    }

    private static int check(Desc d, int dir) {
        if (d.closing) {
            return ERR_CLOSING;
        }
        if (d.deadline[dir] < 0) {
            return ERR_TIMEOUT;
        }
        return OK;
    }

    private static int dir(long mode) {
        if (mode == 'r') {
            return R;
        } else if (mode == 'w') {
            return W;
        }
        throw new IllegalStateException("graalgo: poll mode " + mode);
    }

    /** {@code runtime_pollReset(pd, mode)}: forget readiness seen so far, unless in error. */
    public static int reset(long id, long mode) {
        Desc d = desc(id);
        int dir = dir(mode);
        synchronized (d) {
            int err = check(d, dir);
            if (err == OK) {
                d.ready[dir] = false;
            }
            return err;
        }
    }

    /**
     * {@code runtime_pollWait(pd, mode)}: parks the calling goroutine until the descriptor is
     * ready in that direction, is unblocked, or its deadline passes. With {@code canceled}
     * ({@code runtime_pollWaitCanceled}) only readiness ends the wait.
     */
    public static int await(long id, long mode, boolean canceled) {
        Desc d = desc(id);
        int dir = dir(mode);
        SchedulerCore sched = SchedulerCore.current();
        for (;;) {
            synchronized (d) {
                if (!canceled) {
                    int err = check(d, dir);
                    if (err != OK) {
                        return err;
                    }
                }
                if (d.ready[dir]) {
                    d.ready[dir] = false;
                    return OK;
                }
                if (d.waiter[dir] != null) {
                    throw new IllegalStateException("runtime: double wait");
                }
                d.waiter[dir] = sched.currentGoroutine();
            }
            // A wake between the registration and the park is kept by the scheduler.
            sched.parkCurrent(null, ParkReason.NETPOLL, "IO wait");
        }
    }

    /** Unparks the registered waiter of a direction, if any. Call with {@code d}'s monitor held. */
    private static void wake(Desc d, int dir) {
        GoroutineHandle g = d.waiter[dir];
        if (g != null) {
            d.waiter[dir] = null;
            SchedulerCore.current().unpark(g);
        }
    }

    private static void cancelTimer(Desc d, int dir) {
        if (d.timer[dir] >= 0) {
            SchedulerCore.current().timerHeap().cancel(d.timer[dir]);
            d.timer[dir] = -1;
        }
        d.seq[dir]++;
    }

    /**
     * {@code runtime_pollSetDeadline(pd, d, mode)}: {@code d} is an absolute time of the
     * runtime's monotonic clock, 0 for none; {@code mode} is 'r', 'w' or 'r'+'w'.
     */
    public static void setDeadline(long id, long when, long mode) {
        Desc d = desc(id);
        SchedulerCore sched = SchedulerCore.current();
        synchronized (d) {
            if (d.closing) {
                return;
            }
            long v = when != 0 && when <= System.nanoTime() ? -1 : when;
            for (int dir = R; dir <= W; dir++) {
                if (mode != 'r' + 'w' && dir != dir(mode)) {
                    continue;
                }
                cancelTimer(d, dir);
                d.deadline[dir] = v;
                if (v < 0) {
                    wake(d, dir);
                } else if (v > 0) {
                    final int fdir = dir;
                    final long seq = d.seq[dir];
                    d.timer[dir] = sched.timerHeap().addCallback(v, () -> expire(d, fdir, seq));
                }
            }
        }
    }

    private static void expire(Desc d, int dir, long seq) {
        synchronized (d) {
            if (d.seq[dir] != seq || d.closing) {
                return;
            }
            d.timer[dir] = -1;
            d.deadline[dir] = -1;
            wake(d, dir);
        }
    }

    /** {@code runtime_pollUnblock}: the descriptor is closing; every waiter returns ERR_CLOSING. */
    public static void unblock(long id) {
        Desc d = desc(id);
        synchronized (d) {
            if (d.closing) {
                throw new IllegalStateException("runtime: unblock on closing polldesc");
            }
            d.closing = true;
            cancelTimer(d, R);
            cancelTimer(d, W);
            wake(d, R);
            wake(d, W);
        }
    }

    private static void loop() {
        try (Arena a = Arena.ofConfined()) {
            MemorySegment state = a.allocate(CAPTURE);
            MemorySegment events = a.allocate((long) EVENT_SIZE * MAX_EVENTS);
            for (;;) {
                int n = (int) WAIT.invokeExact(state, epfd, events, MAX_EVENTS, -1);
                if (n < 0) {
                    int errno = (int) ERRNO.get(state, 0L);
                    if (errno == EINTR) {
                        continue;
                    }
                    throw new IllegalStateException("graalgo: epoll_wait failed, errno " + errno);
                }
                for (int k = 0; k < n; k++) {
                    int ev = events.get(ValueLayout.JAVA_INT_UNALIGNED, (long) k * EVENT_SIZE);
                    long id = events.get(ValueLayout.JAVA_LONG_UNALIGNED, (long) k * EVENT_SIZE + 4);
                    Desc d = DESCS.get(id);
                    if (d == null) {
                        continue; // closed since the event was queued
                    }
                    synchronized (d) {
                        if ((ev & (EPOLLIN | EPOLLRDHUP | EPOLLHUP | EPOLLERR)) != 0) {
                            d.ready[R] = true;
                            wake(d, R);
                        }
                        if ((ev & (EPOLLOUT | EPOLLHUP | EPOLLERR)) != 0) {
                            d.ready[W] = true;
                            wake(d, W);
                        }
                    }
                }
            }
        } catch (RuntimeException e) {
            throw e;
        } catch (Throwable t) {
            throw new IllegalStateException("epoll_wait downcall failed", t);
        }
    }
}
