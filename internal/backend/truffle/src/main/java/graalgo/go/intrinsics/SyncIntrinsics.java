package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.Unsafe;
import graalgo.go.values.Closure;
import graalgo.go.values.Flat;
import graalgo.go.values.Ptr;
import graalgo.go.values.Str;
import graalgo.jfr.ParkReason;
import graalgo.runtime.sync.PinLock;
import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;
import java.util.ArrayDeque;
import java.util.HashMap;
import java.util.Iterator;
import java.util.concurrent.ThreadLocalRandom;

/**
 * The runtime side of packages {@code sync} and {@code internal/sync}: semaphores, notify lists,
 * spinning, {@code sync.Pool}'s pinning and cleanup hook, {@code fatal}/{@code throw}; registered
 * in {@code internal/toolexec/intrinsics/registry.txt}. {@code Mutex}, {@code RWMutex},
 * {@code WaitGroup}, {@code Once}, {@code Cond}, {@code Pool} and {@code Map} are upstream's Go
 * code on top of these.
 *
 * <p>The algorithms are gc's ({@code runtime/sema.go}): a semaphore is a {@code uint32} in Go
 * memory, decremented by compare-and-swap when positive; waiters queue per address in one of
 * {@link #ROOTS} striped roots, each with a waiter count that disables the release's slow path
 * when nobody waits; FIFO queues with gc's LIFO re-queue for mutex waiters that already waited,
 * and the starving mutex's direct handoff (the releaser grants the count to the first waiter and
 * yields to it). A notify list ({@code sync.Cond}) keeps gc's ticket counters in its own Go
 * fields ({@code wait}, {@code notify}); its parked waiters queue in the same roots.
 *
 * <p>Waiting goroutines park through {@link SchedulerCore#parkCurrent} with gc's wait reason
 * ({@code sync.Mutex.Lock}, {@code sync.WaitGroup.Wait}, ...), so they release their scheduler
 * slot, count toward deadlock detection and appear with that reason in the deadlock report; a
 * waker {@link SchedulerCore#unpark}s them through the wake lane. No JVM blocking primitive is
 * involved (GG-SCHED-0006, GG-WAKE-0001). The roots' monitors only guard queue edits — no
 * goroutine parks while holding one.
 */
final class SyncIntrinsics {
    private SyncIntrinsics() {}

    /** A Go address: the referent object and gc's byte offset in it (pointer identity, GG-0062). */
    private record Addr(Flat obj, long off) {
        @Override
        public boolean equals(Object o) {
            return o instanceof Addr a && a.obj == obj && a.off == off;
        }

        @Override
        public int hashCode() {
            return System.identityHashCode(obj) * 31 + Long.hashCode(off);
        }
    }

    /** A parked goroutine (gc's sudog). */
    private static final class Waiter {
        final GoroutineHandle g;
        /** Notify-list ticket ({@code notifyListWait}); unused by semaphores. */
        final long ticket;
        /** Set by a releaser that handed the count over directly (gc's {@code s.ticket = 1}). */
        volatile boolean granted;

        Waiter(GoroutineHandle g, long ticket) {
            this.g = g;
            this.ticket = ticket;
        }
    }

    /** gc's {@code semaRoot}: per-address waiter queues and the root's waiter count. */
    private static final class Root {
        final HashMap<Addr, ArrayDeque<Waiter>> queues = new HashMap<>();
        volatile int nwait;

        void queue(Addr a, Waiter w, boolean lifo) {
            ArrayDeque<Waiter> q = queues.computeIfAbsent(a, k -> new ArrayDeque<>());
            if (lifo) {
                q.addFirst(w);
            } else {
                q.addLast(w);
            }
        }

        Waiter dequeue(Addr a) {
            ArrayDeque<Waiter> q = queues.get(a);
            if (q == null) {
                return null;
            }
            Waiter w = q.pollFirst();
            if (q.isEmpty()) {
                queues.remove(a); // no entry outlives its last waiter
            }
            return w;
        }
    }

    /** gc's {@code semTabSize}. */
    private static final int ROOTS = 251;
    private static final Root[] TABLE = new Root[ROOTS];

    static {
        for (int i = 0; i < ROOTS; i++) {
            TABLE[i] = new Root();
        }
    }

    private static Addr addr(Node node, Object p) {
        if (p == null) {
            throw graalgo.go.runtime.Panics.nilDeref(node);
        }
        return new Addr(Unsafe.referent(p), Unsafe.offset(p));
    }

    private static Root rootFor(Addr a) {
        return TABLE[Math.floorMod(a.hashCode(), ROOTS)];
    }

    /** gc's {@code cansemacquire}: decrement {@code *addr} if it is positive. */
    private static boolean canAcquire(Node node, Object addr) {
        for (;;) {
            long v = AtomicIntrinsics.sync_atomic_LoadUint32(node, addr);
            if (v == 0) {
                return false;
            }
            if (AtomicIntrinsics.sync_atomic_CompareAndSwapUint32(node, addr, v, v - 1)) {
                return true;
            }
        }
    }

    /** gc's {@code semacquire1}. */
    @TruffleBoundary
    private static void acquire(Node node, Object addr, boolean lifo, String reason) {
        if (canAcquire(node, addr)) {
            return;
        }
        SchedulerCore sched = SchedulerCore.current();
        Addr a = addr(node, addr);
        Root root = rootFor(a);
        Waiter w = new Waiter(sched.currentGoroutine(), 0);
        for (;;) {
            synchronized (root) {
                // Count ourselves first, so a release after the check below takes its slow path.
                root.nwait++;
                if (canAcquire(node, addr)) {
                    root.nwait--;
                    return;
                }
                root.queue(a, w, lifo);
            }
            // The release may unpark before this parks: the drainer keeps the early UNPARK.
            sched.parkCurrent(null, ParkReason.UNKNOWN, reason);
            if (w.granted || canAcquire(node, addr)) {
                return;
            }
        }
    }

    /** gc's {@code semrelease1}. */
    @TruffleBoundary
    private static void release(Node node, Object addr, boolean handoff) {
        Addr a = addr(node, addr);
        Root root = rootFor(a);
        AtomicIntrinsics.sync_atomic_AddUint32(node, addr, 1);
        if (root.nwait == 0) {
            return; // nobody waits anywhere in this root (checked after the add: no lost wakeup)
        }
        Waiter w;
        synchronized (root) {
            if (root.nwait == 0) {
                return;
            }
            w = root.dequeue(a);
            if (w != null) {
                root.nwait--;
            }
        }
        if (w == null) {
            return;
        }
        if (handoff && canAcquire(node, addr)) {
            w.granted = true;
        }
        SchedulerCore sched = SchedulerCore.current();
        sched.unpark(w.g);
        if (w.granted) {
            // Direct handoff (starving mutex): the waiter inherits the rest of the time slice.
            sched.yieldCurrent();
        }
    }

    static void sync_runtime_Semacquire(Node node, Object addr) {
        acquire(node, addr, false, "semacquire");
    }

    /** No synctest bubbles (see {@link #internal_synctest_IsInBubble}): never durable. */
    static void sync_runtime_SemacquireWaitGroup(Node node, Object addr, boolean synctestDurable) {
        acquire(node, addr, false, "sync.WaitGroup.Wait");
    }

    static void sync_runtime_SemacquireRWMutexR(Node node, Object addr, boolean lifo, long skipframes) {
        acquire(node, addr, lifo, "sync.RWMutex.RLock");
    }

    static void sync_runtime_SemacquireRWMutex(Node node, Object addr, boolean lifo, long skipframes) {
        acquire(node, addr, lifo, "sync.RWMutex.Lock");
    }

    static void internal_sync_runtime_SemacquireMutex(Node node, Object addr, boolean lifo, long skipframes) {
        acquire(node, addr, lifo, "sync.Mutex.Lock");
    }

    static void sync_runtime_Semrelease(Node node, Object addr, boolean handoff, long skipframes) {
        release(node, addr, handoff);
    }

    static void internal_sync_runtime_Semrelease(Node node, Object addr, boolean handoff, long skipframes) {
        release(node, addr, handoff);
    }

    // ------------------------------------------------------------------ notify lists

    /**
     * The {@code notify} field of {@code sync.notifyList} ({@code wait atomic.Uint32; notify
     * uint32; lock uintptr; head, tail unsafe.Pointer}): the second scalar leaf, gc offset 4.
     * {@code wait} is the list's first leaf, so the list's own pointer addresses it.
     */
    private static Object notifyField(Object l) {
        if (l instanceof Flat f) {
            return Ptr.make(f, 1, 0, 0);
        } else if (l instanceof Ptr p) {
            return Ptr.make(p.obj(), p.prim() + 1, p.ref(), p.byt());
        }
        return Unsafe.pointer(Unsafe.referent(l), Unsafe.offset(l) + 4);
    }

    /** gc's {@code less}: ticket order with wraparound. */
    private static boolean less(long a, long b) {
        return (int) (a - b) < 0;
    }

    @TruffleBoundary
    static long sync_runtime_notifyListAdd(Node node, Object l) {
        if (l == null) {
            throw graalgo.go.runtime.Panics.nilDeref(node);
        }
        return (AtomicIntrinsics.sync_atomic_AddUint32(node, l, 1) - 1) & 0xFFFFFFFFL;
    }

    @TruffleBoundary
    static void sync_runtime_notifyListWait(Node node, Object l, long t) {
        Addr a = addr(node, l);
        Root root = rootFor(a);
        SchedulerCore sched = SchedulerCore.current();
        synchronized (root) {
            if (less(t, AtomicIntrinsics.sync_atomic_LoadUint32(node, notifyField(l)))) {
                return; // already notified
            }
            root.queue(a, new Waiter(sched.currentGoroutine(), t), false);
        }
        sched.parkCurrent(null, ParkReason.UNKNOWN, "sync.Cond.Wait");
    }

    @TruffleBoundary
    static void sync_runtime_notifyListNotifyAll(Node node, Object l) {
        Object notify = notifyField(l);
        if (AtomicIntrinsics.sync_atomic_LoadUint32(node, l) == AtomicIntrinsics.sync_atomic_LoadUint32(node, notify)) {
            return;
        }
        Addr a = addr(node, l);
        Root root = rootFor(a);
        ArrayDeque<Waiter> q;
        synchronized (root) {
            q = root.queues.remove(a);
            AtomicIntrinsics.sync_atomic_StoreUint32(node, notify, AtomicIntrinsics.sync_atomic_LoadUint32(node, l));
        }
        if (q != null) {
            SchedulerCore sched = SchedulerCore.current();
            for (Waiter w : q) {
                sched.unpark(w.g);
            }
        }
    }

    @TruffleBoundary
    static void sync_runtime_notifyListNotifyOne(Node node, Object l) {
        Object notify = notifyField(l);
        if (AtomicIntrinsics.sync_atomic_LoadUint32(node, l) == AtomicIntrinsics.sync_atomic_LoadUint32(node, notify)) {
            return;
        }
        Addr a = addr(node, l);
        Root root = rootFor(a);
        Waiter found = null;
        synchronized (root) {
            long t = AtomicIntrinsics.sync_atomic_LoadUint32(node, notify);
            if (t == AtomicIntrinsics.sync_atomic_LoadUint32(node, l)) {
                return;
            }
            AtomicIntrinsics.sync_atomic_StoreUint32(node, notify, (t + 1) & 0xFFFFFFFFL);
            // The waiter holding ticket t may not have queued yet; then it sees the new notify
            // number and does not park.
            ArrayDeque<Waiter> q = root.queues.get(a);
            if (q != null) {
                for (Iterator<Waiter> it = q.iterator(); it.hasNext();) {
                    Waiter w = it.next();
                    if (w.ticket == t) {
                        it.remove();
                        found = w;
                        break;
                    }
                }
                if (q.isEmpty()) {
                    root.queues.remove(a);
                }
            }
        }
        if (found != null) {
            SchedulerCore.current().unpark(found.g);
        }
    }

    /** gc's {@code unsafe.Sizeof(notifyList{})} on 64-bit platforms. */
    private static final long NOTIFY_LIST_SIZE = 32;

    @TruffleBoundary
    static void sync_runtime_notifyListCheck(Node node, long size) {
        if (size != NOTIFY_LIST_SIZE) {
            GoProcess process = GoProcess.get(node);
            process.writeErr(("runtime: bad notifyList size - sync=" + size + " runtime=" + NOTIFY_LIST_SIZE + "\n")
                    .getBytes(java.nio.charset.StandardCharsets.UTF_8));
            throw process.fatal("bad notifyList size");
        }
    }

    // ------------------------------------------------------------------ spinning, clock, fatal

    /** gc's {@code active_spin}. */
    private static final int ACTIVE_SPIN = 4;
    /** gc's {@code active_spin_cnt} (PAUSE instructions per spin). */
    private static final int ACTIVE_SPIN_CNT = 30;
    private static final int NCPU = Runtime.getRuntime().availableProcessors();

    /**
     * gc's {@code canSpin}: a few rounds, only on a multicore machine with GOMAXPROCS &gt; 1 and
     * another goroutine running (gc: another P not idle) and nothing waiting to run (gc: the
     * local run queue is empty).
     */
    @TruffleBoundary
    static boolean internal_sync_runtime_canSpin(Node node, long i) {
        if (i >= ACTIVE_SPIN || NCPU <= 1) {
            return false;
        }
        SchedulerCore sched = SchedulerCore.current();
        return sched.cap() > 1 && sched.runnableCount() == 0;
    }

    static void internal_sync_runtime_doSpin(Node node) {
        for (int i = 0; i < ACTIVE_SPIN_CNT; i++) {
            Thread.onSpinWait();
        }
    }

    static long internal_sync_runtime_nanotime(Node node) {
        return System.nanoTime();
    }

    /** gc's {@code fatal}: {@code fatal error: text}, exit status 2, no deferred call runs. */
    @TruffleBoundary
    static void internal_sync_fatal(Node node, Str s) {
        throw GoProcess.get(node).fatal(s.toString());
    }

    @TruffleBoundary
    static void sync_fatal(Node node, Str s) {
        throw GoProcess.get(node).fatal(s.toString());
    }

    /**
     * gc's {@code throw}. gc also dumps every goroutine (a runtime throw raises the traceback
     * level); the report here is {@code fatal}'s. Only reached on a corrupted lock word.
     */
    @TruffleBoundary
    static void internal_sync_throw(Node node, Str s) {
        throw GoProcess.get(node).fatal(s.toString());
    }

    @TruffleBoundary
    static void sync_throw(Node node, Str s) {
        throw GoProcess.get(node).fatal(s.toString());
    }

    // ------------------------------------------------------------------ sync.Pool

    /**
     * gc's {@code procPin}: no preemption until {@link #sync_runtime_procUnpin}, and the P's id
     * indexes the pool's per-P array. Pin sections serialize on the {@link PinLock} (a pinned
     * goroutine never quantum-parks, SchedulerCore), so one per-P slot — id 0 — is never used by
     * two goroutines at once. The sections are short and never park.
     */
    @TruffleBoundary
    static long sync_runtime_procPin(Node node) {
        PinLock.lockForGo(node);
        return 0;
    }

    @TruffleBoundary
    static void sync_runtime_procUnpin(Node node) {
        PinLock.LOCK.unlock();
    }

    /** gc calls the cleanup at the start of every collection; here {@code runtime.GC} does. */
    @TruffleBoundary
    static void sync_runtime_registerPoolCleanup(Node node, Object cleanup) {
        GoProcess.get(node).poolCleanup = (Closure) cleanup;
    }

    /** gc's {@code randn}: a uniform value in [0, n). */
    static long runtime_randn(Node node, long n) {
        return ((ThreadLocalRandom.current().nextLong() & 0xFFFFFFFFL) * n) >>> 32;
    }

    static long runtime_rand(Node node) {
        return ThreadLocalRandom.current().nextLong();
    }

    // ------------------------------------------------------------------ internal/synctest

    /** No goroutine is in a synctest bubble: {@code testing/synctest} is not supported. */
    static boolean internal_synctest_IsInBubble(Node node) {
        return false;
    }

    /** {@code synctest.Unbubbled}. */
    static long internal_synctest_associate(Node node, Object p) {
        return 0;
    }

    static void internal_synctest_disassociate(Node node, Object p) {
    }

    static boolean internal_synctest_isAssociated(Node node, Object p) {
        return false;
    }
}
