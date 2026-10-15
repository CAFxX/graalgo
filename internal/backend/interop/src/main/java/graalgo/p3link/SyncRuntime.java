package graalgo.p3link;

import java.nio.charset.StandardCharsets;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.BitSet;
import java.util.Deque;
import java.util.Iterator;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoFieldCell;
import graalgo.p3builtin.nodes.GoMaxProcsNode;
import graalgo.runtime.GoCalls;
import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;
import graalgo.values.GoCell;
import graalgo.values.GoFunc;
import graalgo.values.GoPointer;
import graalgo.values.GoString;

/**
 * Runtime-provided {@code //go:linkname} surface for the real {@code sync}
 * and {@code sync/atomic} packages (real-sync lane).
 *
 * <p>In gc, package {@code sync} declares a set of bodyless functions that
 * the runtime provides via {@code //go:linkname} (see
 * {@code src/runtime/sema.go}, {@code src/runtime/mgc.go},
 * {@code src/runtime/proc.go}, {@code src/runtime/panic.go} in go1.27.1).
 * The frontend emits each as a linkname call with symbol
 * {@code <pkgpath>.<name>} (e.g. {@code "sync.runtime_registerPoolCleanup"});
 * this class implements the provider side. Registration happens in
 * {@link LinknameIntrinsics}; the semantics below mirror gc's providers,
 * adapted to the JVM where gc's mechanisms have no direct counterpart.
 * The adaptations are documented per facility — none is package- or
 * symbol-name-keyed beyond the gc symbol table itself (which is how gc's
 * own linker resolves these).
 *
 * <p>Facilities (all mirror gc's {@code //go:linkname} providers for the
 * real {@code sync}, {@code sync/atomic}, {@code internal/sync}, and
 * {@code internal/synctest} packages — the {@code internal/sync} surface
 * is included because {@code sync.Mutex} embeds {@code isync.Mutex}):
 * <ul>
 *   <li><b>Pool-cleanup registration</b>
 *       ({@code sync.runtime_registerPoolCleanup}, called once by
 *       {@code sync.init()}): gc stores the func in {@code poolcleanup}
 *       and the runtime invokes it at the start of every GC stop-the-world
 *       ({@code clearpools} in {@code src/runtime/mgc.go}), rotating each
 *       pool's primary cache into the victim generation. There is no STW
 *       hook on the JVM, and running arbitrary Go code on a GC-notification
 *       thread would touch scheduler state off-scheduler (cf.
 *       {@code graalgo.runtime.GcInterop}'s finalizer discipline), so
 *       graalgo stores the registration (mirroring gc's
 *       {@code poolcleanup} variable, last registration wins) without
 *       auto-invoking it. This is contract-safe: {@code sync.Pool}'s
 *       documented contract permits never evicting ("any item stored in
 *       the Pool may be removed automatically at any time" — removal is
 *       allowed, never required). {@link #runPoolCleanups()} runs the
 *       registered cleanup on the <em>calling</em> goroutine for explicit,
 *       deterministic invocation (tests, future GC wiring).</li>
 *   <li><b>P pinning</b> ({@code sync.runtime_procPin} /
 *       {@code sync.runtime_procUnpin}, and the {@code sync/atomic}
 *       spellings which gc's runtime provides from the same
 *       {@code procPin}/{@code procUnpin}): gc pins the goroutine to its
 *       P, disables preemption, and returns the P id in
 *       {@code [0, GOMAXPROCS)} — exclusive per P by construction, so each
 *       {@code poolLocal} shard's {@code private} slot has exactly one
 *       accessor at a time. On the JVM there is no preemption to disable,
 *       and a pin section contains no blocking calls, so the goroutine
 *       cannot migrate carriers mid-section; pin/unpin are therefore id
 *       checkout / id return. graalgo mints ids in {@code [0, GOMAXPROCS)}
 *       and checks them out one per pin section, restoring gc's per-shard
 *       exclusivity: at most one thread holds a given id at a time, so no
 *       two concurrently pinned threads ever share a {@code poolLocal}
 *       shard (the earlier per-thread dense ids reduced mod GOMAXPROCS
 *       allowed congruent ids to be concurrently pinned — a genuine Go
 *       data race once more than GOMAXPROCS threads existed in a run).
 *       Each thread holds at most one checked-out id; nested pins reuse
 *       the outermost id via a per-thread stack, exactly like gc's nested
 *       procPin returning the same P id. If every id is checked out,
 *       procPin waits for one to be returned — deadlock-free because pin
 *       sections are non-blocking by the Go runtime contract. The
 *       {@code pid < GOMAXPROCS(0)} invariant that
 *       {@code sync.Pool.pinSlow} relies on when sizing its per-P array
 *       holds because ids are only ever issued below the current
 *       GOMAXPROCS (the pool reconciles on GOMAXPROCS changes).</li>
 *   <li><b>notifyList</b> ({@code sync.runtime_notifyListAdd} /
 *       {@code Wait} / {@code NotifyOne} / {@code NotifyAll} /
 *       {@code Check}, the ticket lock behind {@code sync.Cond}):
 *       abstractly a ticket dispenser — {@code Add} issues the next ticket
 *       ({@code wait++}), {@code Wait(t)} blocks until
 *       {@code notify > t} (wraparound-safe unsigned comparison, gc's
 *       {@code less}), {@code NotifyOne} consumes one ticket
 *       ({@code notify++}) and wakes the waiter holding exactly that
 *       ticket, {@code NotifyAll} sets {@code notify = wait} and wakes
 *       everyone. Implemented with scheduler-aware parking
 *       ({@link SchedulerCore#parkCurrent},
 *       {@link SchedulerCore#unpark}), following the missed-wakeup
 *       discipline of {@code graalgo.runtime.sync.GoSemaphore}
 *       (enqueue under the lock, re-check, park outside it; the drainer
 *       stashes an UNPARK that arrives before the park). State lives in a
 *       JVM-side table keyed by the notifyList's <em>storage identity</em>
 *       (the {@code *notifyList} arrives as a {@code GoPointer}; a fresh
 *       cell is minted per {@code &} evaluation, so the key is the
 *       (owner struct, field index) pair): Go code
 *       never reads {@code notifyList.wait}/{@code .notify} directly
 *       (verified against go1.27.1 {@code src/sync/*.go} — only the five
 *       runtime_* entry points touch them), so no Go-memory mirroring is
 *       needed. {@code Check} compares the frontend-constant-folded
 *       {@code unsafe.Sizeof} against the gcSizes amd64 layout (32 bytes
 *       for the default non-staticlockranking build the disk importer
 *       uses); a mismatch fails fast exactly like gc's
 *       {@code notifyListCheck}, catching sync/runtime struct skew.</li>
 *   <li><b>{@code sync.throw} / {@code sync.fatal}</b> (and the
 *       {@code internal/sync} spellings, which gc provides from the same
 *       {@code runtime.throw}/{@code runtime.fatal}): gc maps these to
 *       {@code runtime.throw} / {@code runtime.fatal} — print
 *       {@code "fatal error: <s>"} and terminate the process. The JVM
 *       stack is not a Go stack, so no traceback is printed; the
 *       observable contract (message on stderr, nonzero exit) is kept.
 *       Both are unrecoverable from Go code, as in gc.</li>
 *   <li><b>{@code internal/sync} mutex spinning</b>
 *       ({@code runtime_SemacquireMutex}/{@code runtime_Semrelease} —
 *       same wakeup/mutual-exclusion semantics as the {@code sync}
 *       spellings, only the profiling/wait-reason differs in gc; gc's
 *       {@code lifo=true} queueing (most-recent waiter served next) is
 *       NOT replicated — {@code GoSemaphore.acquire} is FIFO, a
 *       correctness-safe fairness approximation, documented in
 *       {@link LinknameIntrinsics};
 *       {@code runtime_canSpin}/{@code runtime_doSpin} — bounded
 *       cooperative spinning, adapted to the JVM's observables;
 *       {@code runtime_nanotime} — the {@code internal/sync} spelling of
 *       the nanotime intrinsic).</li>
 * </ul>
 *
 * <p>Per-run state (the pool-cleanup slot and the notifyList table) is
 * reset by {@link #clear()}, called from {@link LinknameIntrinsics#install}
 * next to {@code Linknames.clear()} — the notifyList table is keyed by Go
 * struct object identity and the pool-cleanup func is compiled code from
 * the previous program build, so neither may leak across builds in one
 * JVM (the P3-M4 remediation rationale). The P-id checkout pool holds no
 * run-specific data (ids are just numbers), but {@code clear()} still
 * reconciles it to the current GOMAXPROCS so a value changed between runs
 * takes effect even before the next pin.
 */
public final class SyncRuntime {
    private SyncRuntime() {}

    // ------------------------------------------------------------------
    // Pool-cleanup registration (sync.runtime_registerPoolCleanup).
    // ------------------------------------------------------------------

    /** Mirrors gc's {@code var poolcleanup func()} (src/runtime/mgc.go). */
    private static final AtomicReference<GoFunc> POOL_CLEANUP = new AtomicReference<>();

    /** Stores the pool-cleanup func (last registration wins, as in gc). */
    public static void registerPoolCleanup(GoFunc f) {
        POOL_CLEANUP.set(f);
    }

    /**
     * Runs the registered pool cleanup on the calling goroutine, if any.
     * Explicit, deterministic invocation — the automatic gc trigger (GC
     * stop-the-world) has no JVM counterpart; see the class javadoc.
     */
    public static void runPoolCleanups() {
        GoFunc f = POOL_CLEANUP.get();
        if (f != null) {
            GoCalls.invoke(f, new Object[0]);
        }
    }

    // ------------------------------------------------------------------
    // P pinning (sync/sync/atomic .runtime_procPin/.runtime_procUnpin).
    //
    // Id checkout pool. gc pins the goroutine to its P and returns the P
    // id, which is exclusive per P by construction: no two goroutines are
    // ever pinned to the same P concurrently, so each poolLocal shard's
    // private slot has exactly one accessor at a time. The JVM has no Ps,
    // so graalgo mints ids in [0, GOMAXPROCS) and checks them out: at most
    // one thread holds a given id at a time, which restores gc's
    // per-shard exclusivity. (The previous design handed out per-thread
    // dense ids reduced mod GOMAXPROCS; with more than GOMAXPROCS threads
    // in a run, congruent ids were concurrently pinned and shared one
    // poolLocal shard's private slot — a genuine Go data race.)
    //
    // Checkout rules:
    // - Each thread holds at most one checked-out id; nested pins on the
    //   same thread reuse the outermost id (per-thread stack), exactly
    //   like gc's nested procPin returning the same P id. Unpin returns
    //   the id to the pool only when the outermost section ends.
    // - If every id is checked out, procPin waits for one to be returned.
    //   This cannot deadlock: pin sections contain no blocking calls (the
    //   Go runtime contract — a pinned section that blocked would wedge
    //   gc as well), so every holder releases promptly.
    // - A procUnpin with no matching procPin on this thread is a no-op
    //   (robustness; the old no-op unpin was equally tolerant).
    // - GOMAXPROCS changes reconcile the pool: ids are only ever issued
    //   below the current GOMAXPROCS, keeping the pid < GOMAXPROCS(0)
    //   invariant sync.Pool.pinSlow relies on when sizing its per-P
    //   array. Ids in flight across a shrink drain out and are dropped on
    //   return (re-minted if GOMAXPROCS grows again); pinSlow's own
    //   pid < size slow-path check covers the shrink race, exactly as in
    //   gc.
    // ------------------------------------------------------------------

    /** Guards the checkout-pool state below. */
    private static final Object PIN_LOCK = new Object();

    /** Free ids, all in {@code [0, pinCapacity)}, none checked out. Guarded by {@link #PIN_LOCK}. */
    private static final Deque<Integer> PIN_FREE = new ArrayDeque<>();

    /** Ids currently checked out. Guarded by {@link #PIN_LOCK}. */
    private static final BitSet PIN_CHECKED_OUT = new BitSet();

    /** The GOMAXPROCS value the pool was last reconciled to. Guarded by {@link #PIN_LOCK}. */
    private static int pinCapacity = 0;

    /**
     * Per-thread stack of held ids. A thread holds at most one checked-out
     * id; nested pins push nothing and reuse the top, so the stack depth
     * is the pin-nesting depth and only the outermost unpin returns the id.
     */
    private static final ThreadLocal<ArrayDeque<Integer>> PIN_HELD =
            ThreadLocal.withInitial(ArrayDeque::new);

    private static int pinGomaxprocs() {
        return Math.max(1, GoMaxProcsNode.current());
    }

    /**
     * Rebuild the free list for the current GOMAXPROCS: every id in
     * {@code [0, gmp)} that is not checked out becomes available.
     * Checked-out ids are never reissued, so no two threads can hold the
     * same id even across a GOMAXPROCS shrink/grow cycle. Call with
     * {@link #PIN_LOCK} held.
     */
    private static void reconcilePinPoolLocked(int gmp) {
        if (gmp == pinCapacity) {
            return;
        }
        PIN_FREE.clear();
        for (int id = 0; id < gmp; id++) {
            if (!PIN_CHECKED_OUT.get(id)) {
                PIN_FREE.addLast(id);
            }
        }
        pinCapacity = gmp;
    }

    /**
     * gc's {@code procPin}: pin to the current P and return its id in
     * {@code [0, GOMAXPROCS)}. The JVM has no preemption to disable and a
     * pin section cannot block, so this is id checkout: the returned id is
     * held exclusively by this thread until the matching
     * {@link #procUnpin}.
     */
    public static int procPin() {
        ArrayDeque<Integer> held = PIN_HELD.get();
        if (!held.isEmpty()) {
            return held.peek(); // nested pin: same id, as in gc
        }
        synchronized (PIN_LOCK) {
            for (;;) {
                int gmp = pinGomaxprocs();
                reconcilePinPoolLocked(gmp);
                Integer id = PIN_FREE.pollFirst();
                if (id != null) {
                    PIN_CHECKED_OUT.set(id);
                    held.push(id);
                    return id;
                }
                // Every id is checked out. Pin sections are non-blocking by
                // the Go runtime contract, so a holder releases promptly;
                // waiting here cannot deadlock.
                try {
                    PIN_LOCK.wait();
                } catch (InterruptedException ie) {
                    // procPin is infallible from Go's perspective; keep the
                    // interrupt visible and keep waiting for the id.
                    Thread.currentThread().interrupt();
                }
            }
        }
    }

    /**
     * gc's {@code procUnpin}: re-enable preemption. On the JVM this
     * returns the thread's checked-out id to the pool (outermost unpin
     * only; nested unpins just pop the stack).
     */
    public static void procUnpin() {
        ArrayDeque<Integer> held = PIN_HELD.get();
        if (held.isEmpty()) {
            return; // unbalanced unpin: no-op
        }
        int id = held.pop();
        synchronized (PIN_LOCK) {
            PIN_CHECKED_OUT.clear(id);
            if (id < pinCapacity) {
                PIN_FREE.addLast(id);
            }
            // Else GOMAXPROCS shrank while pinned: the id is dropped here
            // and re-minted by reconcile if GOMAXPROCS grows again; it is
            // never reissued while checked out.
            PIN_LOCK.notifyAll();
        }
    }

    // ------------------------------------------------------------------
    // notifyList (sync.runtime_notifyList*): the sync.Cond ticket lock.
    // ------------------------------------------------------------------

    /**
     * gcSizes amd64 size of {@code sync.notifyList} as compiled by the
     * frontend (go/types constant-folds the {@code unsafe.Sizeof} in
     * {@code sync.init}): {@code wait+notify} (2×uint32) + {@code lock}
     * (uintptr) + {@code head}/{@code tail} (2×unsafe.Pointer) = 32.
     * This is the default non-staticlockranking layout — the build the
     * disk importer uses. If the frontend ever folds a different size
     * (e.g. staticlockranking's 40), {@link #notifyListCheck} fails fast,
     * which is the check working as designed.
     */
    private static final long NOTIFY_LIST_SIZE = 32;

    /** One parked Cond waiter: the scheduler handle plus its ticket. */
    private record Waiter(GoroutineHandle handle, int ticket) {}

    /** Canonical notifyList state (gc's wait/notify counters + sudog queue). */
    private static final class NotifyState {
        /** Tickets issued (unsigned 32-bit, wraps like gc's uint32). */
        int wait;
        /** Tickets notified (unsigned 32-bit). */
        int notify;
        /** Parked waiters in enqueue order. */
        final Deque<Waiter> waiters = new ArrayDeque<>();
    }

    /** NotifyList state by storage identity (per-run: see clear()). */
    private static final ConcurrentHashMap<NotifyKey, NotifyState> NOTIFY =
            new ConcurrentHashMap<>();

    /**
     * Stable storage-identity key for a {@code *notifyList} argument.
     *
     * <p>A {@code *notifyList} arrives as a {@link GoPointer}. It is
     * almost always a cell pointer from {@code &c.notify}, and
     * {@code FieldAddrNode} mints a fresh {@code GoFieldCell} per
     * {@code &} evaluation — so the cell object itself is not a stable
     * key. The stable identity is (owner struct, field index) for field
     * cells, the box for {@code GoBox} cells (one box per variable),
     * (array, index) for element pointers, and the referent otherwise.
     * This matches gc: two evaluations of {@code &c.notify} address the
     * same notifyList.
     *
     * <p>The key compares by Java identity ({@code ==} on the storage
     * object), never by structural equality: {@code GoStruct.equals} is
     * structural (descriptor + field contents), so two distinct
     * {@code sync.Cond} values with equal contents — the canonical
     * {@code sync.NewCond(&mu)} sharing one {@code Mutex} — would
     * otherwise map to the same {@link NotifyState}, and a
     * {@code Signal} on one would wake the other's waiters (silent
     * deterministic corruption). Copying a Cond after first use is
     * forbidden by Go, so identity keys are strictly more correct than
     * structural keys here.
     */
    private static final class NotifyKey {
        private final Object storage;
        private final int index;

        private NotifyKey(Object storage, int index) {
            this.storage = storage;
            this.index = index;
        }

        @Override
        public boolean equals(Object o) {
            return o instanceof NotifyKey k && storage == k.storage && index == k.index;
        }

        @Override
        public int hashCode() {
            return System.identityHashCode(storage) * 31 + index;
        }
    }

    private static NotifyKey notifyKey(Object arg, String symbol) {
        if (!(arg instanceof GoPointer gp) || gp.isNil()) {
            throw new FrontendException(symbol + ": expected a *notifyList argument");
        }
        if (gp.isCellPointer()) {
            GoCell cell = gp.cell();
            if (cell instanceof GoFieldCell fc) {
                return new NotifyKey(fc.ownerStruct(), fc.fieldIndex());
            }
            return new NotifyKey(cell, 0);
        }
        if (gp.isElemPointer()) {
            return new NotifyKey(gp.elemArray(), gp.elemIndex());
        }
        return new NotifyKey(gp.referent(), 0);
    }

    private static NotifyState notifyState(Object arg, String symbol) {
        return NOTIFY.computeIfAbsent(notifyKey(arg, symbol), k -> new NotifyState());
    }

    /** gc's {@code less(a, b)}: wraparound-safe unsigned ticket comparison. */
    private static boolean ticketNotified(int ticket, int notify) {
        return Integer.compareUnsigned(ticket, notify) < 0;
    }

    /**
     * {@code sync.runtime_notifyListAdd(l *notifyList) uint32}: issue the
     * next ticket (gc: {@code l.wait.Add(1) - 1}).
     */
    public static int notifyListAdd(Object arg) {
        NotifyState s = notifyState(arg, "sync.runtime_notifyListAdd");
        synchronized (s) {
            int t = s.wait;
            s.wait = t + 1;
            return t;
        }
    }

    /**
     * {@code sync.runtime_notifyListWait(l *notifyList, t uint32)}: block
     * until ticket {@code t} is notified; return immediately if it already
     * was (gc's fast path). Scheduler-aware parking — never blocks a
     * carrier thread.
     */
    public static void notifyListWait(Object arg, int ticket) {
        SchedulerCore sched = SchedulerCore.current();
        NotifyState s = notifyState(arg, "sync.runtime_notifyListWait");
        GoroutineHandle h;
        synchronized (s) {
            if (ticketNotified(ticket, s.notify)) {
                return;
            }
            h = sched.currentGoroutine();
            s.waiters.addLast(new Waiter(h, ticket));
            // Missed-wakeup guard (cf. GoSemaphore.acquire): a NotifyOne/
            // NotifyAll that landed between the fast-path check and the
            // enqueue either consumed our ticket (then it is notified now)
            // or it holds this lock — it cannot have, we hold it.
            if (ticketNotified(ticket, s.notify)) {
                s.waiters.removeLast();
                return;
            }
        }
        // Park outside the lock; the drainer stashes an UNPARK that wins
        // the race (no lost wakeup), same discipline as GoSemaphore.
        sched.parkCurrent(null);
    }

    /**
     * {@code sync.runtime_notifyListNotifyOne(l *notifyList)}: consume one
     * ticket and wake the waiter holding exactly that ticket, if it has
     * enqueued yet (gc scans for the ticket; a waiter that has not parked
     * observes the consumed ticket on its fast path).
     */
    public static void notifyListNotifyOne(Object arg) {
        NotifyState s = notifyState(arg, "sync.runtime_notifyListNotifyOne");
        Waiter w = null;
        synchronized (s) {
            if (s.wait == s.notify) {
                return; // fast path: no new waiters since last notification
            }
            int t = s.notify;
            if (t == s.wait) {
                return; // re-check under the lock
            }
            s.notify = t + 1;
            for (Iterator<Waiter> it = s.waiters.iterator(); it.hasNext();) {
                Waiter c = it.next();
                if (c.ticket() == t) {
                    it.remove();
                    w = c;
                    break;
                }
            }
        }
        if (w != null) {
            SchedulerCore.current().unpark(w.handle());
        }
    }

    /**
     * {@code sync.runtime_notifyListNotifyAll(l *notifyList)}: notify every
     * outstanding ticket and wake all waiters (gc: {@code notify = wait}).
     */
    public static void notifyListNotifyAll(Object arg) {
        NotifyState s = notifyState(arg, "sync.runtime_notifyListNotifyAll");
        List<Waiter> wake;
        synchronized (s) {
            if (s.wait == s.notify) {
                return; // fast path
            }
            s.notify = s.wait;
            wake = new ArrayList<>(s.waiters);
            s.waiters.clear();
        }
        // The scheduler entry point is only needed when there is someone to
        // wake; with no waiters this stays scheduler-free (and matches the
        // fast path above, which also avoids scheduler entry points).
        if (!wake.isEmpty()) {
            SchedulerCore sched = SchedulerCore.current();
            for (Waiter w : wake) {
                sched.unpark(w.handle());
            }
        }
    }

    /**
     * {@code sync.runtime_notifyListCheck(size uintptr)} (called by
     * {@code sync.init()}): fail fast if the frontend's
     * {@code unsafe.Sizeof(notifyList)} disagrees with the runtime layout.
     */
    public static void notifyListCheck(long size) {
        if (size != NOTIFY_LIST_SIZE) {
            throw new FrontendException("sync.runtime_notifyListCheck: bad notifyList size"
                    + " (sync=" + size + ", runtime=" + NOTIFY_LIST_SIZE + ")");
        }
    }

    // ------------------------------------------------------------------
    // internal/sync surface (sync.Mutex embeds isync.Mutex).
    // ------------------------------------------------------------------

    /**
     * gc's {@code internal_sync_runtime_canSpin} (src/runtime/proc.go):
     * spin only a few times ({@code active_spin}, 4) and only on
     * multicore with GOMAXPROCS &gt; 1. gc additionally requires an idle
     * P and an empty local run queue; the graalgo scheduler exposes no
     * such observables, so those conjuncts are dropped (documented
     * approximation — spinning remains conservative and bounded).
     */
    public static boolean canSpin(int i) {
        return i < 4
                && Runtime.getRuntime().availableProcessors() > 1
                && GoMaxProcsNode.current() > 1;
    }

    /**
     * gc's {@code internal_sync_runtime_doSpin} (src/runtime/proc.go):
     * {@code procyield(active_spin_cnt)} — an OS yield. On the JVM,
     * {@link Thread#yield}.
     */
    public static void doSpin() {
        Thread.yield();
    }

    // ------------------------------------------------------------------
    // sync.throw / sync.fatal (also reached as internal/sync.throw/fatal).
    // ------------------------------------------------------------------

    private static String goString(Object arg, String symbol) {
        if (!(arg instanceof GoString gs)) {
            throw new FrontendException(symbol + ": expected a string argument");
        }
        return new String(gs.bytes(), StandardCharsets.UTF_8);
    }

    /**
     * gc's {@code runtime.throw} via {@code sync.throw}: fatal error,
     * unrecoverable from Go code.
     */
    public static void syncThrow(Object arg) {
        System.err.println("fatal error: " + goString(arg, "sync.throw"));
        System.err.flush();
        System.exit(2);
    }

    /**
     * gc's {@code runtime.fatal} via {@code sync.fatal}: fatal error,
     * unrecoverable from Go code.
     */
    public static void syncFatal(Object arg) {
        System.err.println("fatal error: " + goString(arg, "sync.fatal"));
        System.err.flush();
        System.exit(2);
    }

    /**
     * Resets per-run state (pool-cleanup registration, notifyList table)
     * and reconciles the P-id checkout pool to the current GOMAXPROCS.
     * Called from {@link LinknameIntrinsics#install} next to
     * {@code Linknames.clear()}: the notifyList table is keyed by Go
     * struct object identity and the pool-cleanup func is compiled code
     * from the previous program build — neither may leak across builds
     * in one JVM.
     */
    public static void clear() {
        POOL_CLEANUP.set(null);
        NOTIFY.clear();
        synchronized (PIN_LOCK) {
            reconcilePinPoolLocked(pinGomaxprocs());
        }
    }
}
