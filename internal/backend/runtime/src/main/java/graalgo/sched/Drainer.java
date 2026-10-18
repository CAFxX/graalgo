package graalgo.sched;

import java.util.ArrayDeque;
import java.util.Deque;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.locks.LockSupport;

/**
 * The single platform-thread drainer (P2-D2, P2-D3).
 *
 * <p>Loop: blocking take from the MPSC inbox → route the token into its
 * lane → admit while {@code inflight < cap} → hard-assert the invariants.
 * Lane order per GG-WAKE-0003: {@code wakeFifo} &gt; {@code initFifo} &gt;
 * {@code runFifo}. The drainer is non-terminating: a stray interrupt is
 * swallowed and the loop continues (otherwise every parked goroutine would
 * hang forever with no recovery).
 *
 * <p>Admission does <strong>ack-before-unpark</strong> (GG-WAKE-0001):
 * {@code acked.set(generation)} precedes {@code LockSupport.unpark};
 * reversing the order loses wakeups.
 *
 * <p>All lane/inflight mutations happen on the drainer thread under
 * {@link #lock}; vthreads only offer to the inbox. Single-threaded
 * mutation is what makes the cap assert airtight.
 */
public final class Drainer {
    private final SchedulerCore sched;
    private final BlockingQueue<ResumeToken> inbox = new LinkedBlockingQueue<>();
    private final Deque<ResumeToken> wakeFifo = new ArrayDeque<>();
    private final Deque<ResumeToken> initFifo = new ArrayDeque<>();
    private final Deque<ResumeToken> runFifo = new ArrayDeque<>();
    private final Object lock = new Object();

    private int inflight;
    private int maxInflightObserved;
    private long admissions;
    private Thread drainerThread;
    // Test-only admission observer (null in production).
    private volatile java.util.function.Consumer<GoroutineHandle> admissionListener;
    // Park/unpark matching (all under lock, drainer thread only):
    // handle id -> parked generation (PARKED seen, UNPARK not yet).
    private final java.util.Map<Long, Long> parked = new java.util.HashMap<>();
    // handle id -> pending unpark arrived before the PARKED token.
    private final java.util.Set<Long> unparkPending = new java.util.HashSet<>();
    // handle ids currently in blocking I/O (P2-M3, GG-0018): slot
    // released, vthread blocked in the syscall (not parked). IO_EXIT
    // re-admits through the WAKE lane.
    private final java.util.Set<Long> inIo = new java.util.HashSet<>();

    /**
     * Idle inbox-poll quantum for the L31 quiescence watchdog. When no
     * token arrives within this window the drainer re-runs the deadlock
     * check: quiescence can be reached without any drainer token (e.g. a
     * timer callback that fires and unblocks nobody). The tick only
     * schedules evaluations — the verdict itself is purely state-based
     * (see {@link #maybeDeadlock}), so the timeout can never cause a
     * false positive.
     */
    private static final long IDLE_POLL_MILLIS = 100;

    /**
     * Admission provenance counters (P2-M8, M6 watch-item investigation):
     * every WAKE-lane admission is classified by what produced it.
     * {@code wakeStashed} counts PARKED tokens that found a pre-arrived
     * UNPARK in {@code unparkPending} — legitimate, but looks like
     * "re-admission with no UNPARK in flight" to a JFR timeline observer.
     */
    public final java.util.concurrent.atomic.AtomicLong wakeUnparkDirect = new java.util.concurrent.atomic.AtomicLong();
    public final java.util.concurrent.atomic.AtomicLong wakeUnparkStashed = new java.util.concurrent.atomic.AtomicLong();
    public final java.util.concurrent.atomic.AtomicLong wakeIoExit = new java.util.concurrent.atomic.AtomicLong();

    Drainer(SchedulerCore sched) {
        this.sched = sched;
    }

    /** Starts the drainer platform thread. Idempotent. */
    synchronized void start() {
        if (drainerThread != null) {
            return;
        }
        drainerThread = Thread.ofPlatform().daemon(true).name("graalgo-drainer")
                .start(this::drainLoop);
        drainerThread.setPriority(Thread.MAX_PRIORITY);
    }

    /** Package-private for tests: the drainer thread (to interrupt it). */
    Thread drainerThreadForTest() {
        return drainerThread;
    }

    /** MPSC offer from vthreads. Never blocks. */
    void offer(ResumeToken token) {
        inbox.offer(token);
    }

    /** Highest in-flight count observed; for tests and stats. */
    public int maxInflightObserved() {
        synchronized (lock) {
            return maxInflightObserved;
        }
    }

    /** Total admissions; for quiescence/lost-wakeup accounting. */
    public long admissions() {
        synchronized (lock) {
            return admissions;
        }
    }

    private void drainLoop() {
        for (;;) {
            final ResumeToken token;
            try {
                // L31: poll (not take) so the quiescence watchdog below
                // re-runs even when no token ever arrives. poll returns
                // immediately when a token is available — admission
                // latency is unchanged.
                token = inbox.poll(IDLE_POLL_MILLIS, TimeUnit.MILLISECONDS);
            } catch (InterruptedException ie) {
                // Non-terminating drainer: a stray interrupt must not kill
                // the scheduler. Swallow and continue.
                continue;
            }
            try {
                synchronized (lock) {
                    if (token == null) {
                        // Watchdog tick: the inbox has been quiet;
                        // re-run the quiescence check (see maybeDeadlock).
                        maybeDeadlock();
                        continue;
                    }
                    route(token);
                    admit();
                    checkInvariants();
                    maybeDeadlock();
                }
            } catch (Throwable t) {
                // Fail LOUD, never silent: a violated invariant (or any
                // drainer bug) means scheduler state is corrupt. Letting
                // the drainer thread die here would hang every parked
                // goroutine forever with no diagnostic — the exact
                // permanent silent hang the non-terminating contract
                // exists to prevent. Halt immediately.
                System.err.println(
                        "FATAL: scheduler drainer invariant violated, halting: " + t);
                t.printStackTrace();
                Runtime.getRuntime().halt(1);
            }
        }
    }

    private void route(ResumeToken token) {
        switch (token.kind) {
            case INIT -> initFifo.addLast(token);
            case RESUME -> {
                inflight--;
                runFifo.addLast(token);
            }
            case WAKE -> wakeFifo.addLast(token);
            case EXIT -> {
                // A goroutine that dies while in blocking I/O without
                // ioExit would double-release its slot (it was already
                // released at ioEnter). Every ioEnter must be paired —
                // fail fast, never silently corrupt the cap.
                if (!inIo.remove(token.handle.id)) {
                    inflight--;
                } else {
                    throw new AssertionError(
                            "goroutine g" + token.handle.id
                                    + " exited while in blocking I/O (unpaired ioEnter)");
                }
            }
            case PARKED -> {
                if (inIo.contains(token.handle.id)) {
                    throw new AssertionError(
                            "goroutine g" + token.handle.id
                                    + " parked while in blocking I/O");
                }
                inflight--;
                if (unparkPending.remove(token.handle.id)) {
                    // Wakeup arrived first: admit immediately through the
                    // priority lane (no wakeup lost).
                    wakeUnparkStashed.incrementAndGet();
                    wakeFifo.addLast(new ResumeToken(
                            ResumeToken.Kind.WAKE, token.handle, token.generation));
                } else {
                    parked.put(token.handle.id, token.generation);
                }
            }
            case UNPARK -> {
                Long gen = parked.remove(token.handle.id);
                if (gen != null) {
                    wakeUnparkDirect.incrementAndGet();
                    wakeFifo.addLast(new ResumeToken(
                            ResumeToken.Kind.WAKE, token.handle, gen));
                } else {
                    // Park hasn't been processed yet: remember the wakeup.
                    unparkPending.add(token.handle.id);
                }
            }
            case IO_ENTER -> {
                // Slot-releasing blocking I/O (P2-M3, GG-0018,
                // GG-SCHED-0004): the goroutine no longer counts against
                // the in-flight cap while blocked in the syscall.
                if (!inIo.add(token.handle.id)) {
                    throw new AssertionError(
                            "duplicate ioEnter for g" + token.handle.id);
                }
                if (parked.containsKey(token.handle.id)) {
                    throw new AssertionError(
                            "ioEnter while parked for g" + token.handle.id);
                }
                inflight--;
                // Synchronous release: ack (+unpark in case the vthread
                // already parked in awaitAck) so ioEnter() returns only
                // after the slot is actually released.
                token.handle.acked.set(token.generation);
                LockSupport.unpark(token.handle.thread);
            }
            case IO_EXIT -> {
                // Re-acquire the slot through the priority lane (gc
                // exitsyscall parity); the vthread waits for admission.
                if (!inIo.remove(token.handle.id)) {
                    throw new AssertionError(
                            "ioExit without ioEnter for g" + token.handle.id);
                }
                wakeIoExit.incrementAndGet();
                wakeFifo.addLast(new ResumeToken(
                        ResumeToken.Kind.WAKE, token.handle, token.generation));
            }
        }
    }

    private void admit() {
        while (inflight < sched.cap()) {
            ResumeToken token = pollLane();
            if (token == null) {
                break;
            }
            inflight++;
            admissions++;
            if (inflight > maxInflightObserved) {
                maxInflightObserved = inflight;
            }
            // GG-WAKE-0001: ack BEFORE unpark. Reversing loses wakeups.
            token.handle.acked.set(token.generation);
            LockSupport.unpark(token.handle.thread);
            java.util.function.Consumer<GoroutineHandle> listener = admissionListener;
            if (listener != null) {
                listener.accept(token.handle);
            }
        }
    }

    /** Lane order per GG-WAKE-0003: wake &gt; init &gt; run. */
    private ResumeToken pollLane() {
        ResumeToken token = wakeFifo.pollFirst();
        if (token == null) {
            token = initFifo.pollFirst();
        }
        if (token == null) {
            token = runFifo.pollFirst();
        }
        return token;
    }

    /**
     * Hard invariant check (GG-SCHED-0003): the in-flight count never
     * exceeds the cap and never goes negative. Called by the drainer
     * after every route+admit; a violation is a loud
     * {@link AssertionError}, never silent corruption.
     */
    void checkInvariants() {
        if (inflight > sched.cap()) {
            throw new AssertionError(
                    "inflight=" + inflight + " > cap=" + sched.cap());
        }
        if (inflight < 0) {
            throw new AssertionError("inflight negative: " + inflight);
        }
    }

    /**
     * Drainer-side quiescence watchdog (L31, P1-28; gc
     * {@code checkdead} parity). Called with the drainer lock held
     * whenever the drainer has nothing left to do: after each token's
     * route+admit, and on every idle inbox-poll timeout.
     *
     * <p>Verdict — the program is deadlocked — iff ALL of:
     * <ol>
     *   <li>a goroutine is still live ({@code numGoroutines() > 0}).
     *       The last EXIT means normal termination: {@code
     *       awaitQuiescence} is about to return (the vthread decrements
     *       the live count <em>before</em> offering EXIT, and the
     *       no-concurrent-spawn contract makes live==0 terminal), so it
     *       is never a deadlock;
     *   <li>{@code inflight == 0} — nothing admitted-but-not-parked;
     *   <li>all three lanes empty — nothing queued for admission;
     *   <li>{@code inIo} empty — nobody blocked in a syscall (gc's
     *       {@code _Gsyscall} keeps its M non-idle, so it is not
     *       "asleep"; the syscall completing is external progress);
     *   <li>the timer heap quiescent — no pending entries and no fire
     *       in progress (gc: any P with timers suppresses checkdead,
     *       even far-future ones, even ones whose callback unblocks
     *       nobody).
     * </ol>
     *
     * <p>No false positives on legitimate parks: a {@code time.Sleep} /
     * {@code time.After} / ticker waiter fails (5); a goroutine in
     * blocking I/O fails (4); quantum/yield parks leave RESUME tokens
     * behind, failing (2)/(3).
     *
     * <p>Race-freedom (the subtle part): the timer thread is the only
     * party that can offer a token while (2)–(4) hold — a SLEEP unpark
     * (or a channel-callback unpark) can be sitting in the inbox while
     * the heap already looks quiescent. So the verdict is a two-phase
     * commit under the heap lock ({@code TimerHeap.runIfQuiescent}):
     * first drain the inbox and route everything, then re-verify
     * (2)–(4). Any fire whose unpark lands after that drain still holds
     * {@code active > 0} (or the heap lock, for channel fires) at the
     * quiescence check, so it fails (5). New timers can only be armed by
     * a running goroutine (fails 2) or by a firing callback (fails 5).
     * Lock order is drainer → heap; the timer thread never takes the
     * drainer lock while holding the heap lock (its wakeups go through
     * the lock-free inbox), so this cannot deadlock.
     */
    private void maybeDeadlock() {
        if (sched.numGoroutines() == 0) {
            return;
        }
        if (!looksQuiescent()) {
            return; // fast path: obviously busy; skip the heap lock
        }
        sched.timerHeap().runIfQuiescent(() -> {
            // Definitive phase: a wakeup may have been offered after the
            // fast-path check (e.g. a timer unpark still in the inbox).
            // Route everything first, then re-verify before verdicting.
            drainInboxLocked();
            if (!looksQuiescent()) {
                return;
            }
            sched.fireDeadlockVerdict();
        });
    }

    /**
     * Guards (2)–(4) of the deadlock verdict: no admitted goroutine,
     * nothing queued for admission, nobody in blocking I/O. Caller holds
     * the drainer lock.
     */
    private boolean looksQuiescent() {
        return inflight == 0 && wakeFifo.isEmpty() && initFifo.isEmpty()
                && runFifo.isEmpty() && inIo.isEmpty();
    }

    /**
     * Routes every token currently in the inbox, then admits and checks
     * invariants — the same work the drain loop does per token, applied
     * to the backlog. Caller holds the drainer lock.
     */
    private void drainInboxLocked() {
        ResumeToken token;
        while ((token = inbox.poll()) != null) {
            route(token);
        }
        admit();
        checkInvariants();
    }

    // ---- test hooks (package-visible; production never calls these) ----

    /**
     * Read-only state snapshot for the P2-M9 interleaving explorer's
     * visited-set. Pure observer: no mutation, no behavior change.
     *
     * @param idNames maps handle ids to stable per-exploration names
     *        (goroutine indices); null keeps raw ids for human debugging.
     */
    String snapshotForTest(java.util.Map<Long, String> idNames) {
        java.util.function.LongFunction<String> name = id ->
                idNames == null ? "g" + id
                        : idNames.getOrDefault(id, "g?" + id);
        synchronized (lock) {
            // P1-2: entries sorted by normalized name — HashMap/HashSet
            // iteration order is run-dependent (ids come from a global
            // counter), which made explorer state counts irreproducible.
            java.util.TreeMap<String, Long> parkedSorted = new java.util.TreeMap<>();
            for (java.util.Map.Entry<Long, Long> e : parked.entrySet()) {
                parkedSorted.put(name.apply(e.getKey()), e.getValue());
            }
            java.util.TreeSet<String> pendingSorted = new java.util.TreeSet<>();
            for (Long id : unparkPending) {
                pendingSorted.add(name.apply(id));
            }
            java.util.TreeSet<String> ioSorted = new java.util.TreeSet<>();
            for (Long id : inIo) {
                ioSorted.add(name.apply(id));
            }
            StringBuilder sb = new StringBuilder();
            sb.append("inflight=").append(inflight)
              .append(";admissions=").append(admissions)
              .append(";max=").append(maxInflightObserved)
              .append(";parked=").append(parkedSorted)
              .append(";pending=").append(pendingSorted)
              .append(";io=").append(ioSorted);
            sb.append(";wake=");
            for (ResumeToken t : wakeFifo) {
                sb.append(t.kind).append(":").append(name.apply(t.handle.id))
                  .append(":").append(t.generation).append(",");
            }
            // P2-4: lanes render uniformly as kind:id:gen (init/run lanes
            // are kind-homogeneous by construction, but uniform rendering
            // keeps a future lane-kind bug visible in snapshots).
            sb.append(";init=");
            for (ResumeToken t : initFifo) {
                sb.append(t.kind).append(":").append(name.apply(t.handle.id))
                  .append(":").append(t.generation).append(",");
            }
            sb.append(";run=");
            for (ResumeToken t : runFifo) {
                sb.append(t.kind).append(":").append(name.apply(t.handle.id))
                  .append(":").append(t.generation).append(",");
            }
            sb.append(";prov=").append(wakeUnparkDirect.get()).append("/")
              .append(wakeUnparkStashed.get()).append("/").append(wakeIoExit.get());
            return sb.toString();
        }
    }

    /** Directly sets the in-flight count; white-box invariant tests only. */
    void setInflightForTest(int n) {
        synchronized (lock) {
            inflight = n;
        }
    }

    /** Reads the in-flight count; white-box invariant tests only. */
    int inflightForTest() {
        synchronized (lock) {
            return inflight;
        }
    }

    /** Observes admissions in order; lane-order tests only. */
    void setAdmissionListener(java.util.function.Consumer<GoroutineHandle> listener) {
        admissionListener = listener;
    }

    /**
     * Synchronous route+admit+check on the calling thread; deterministic
     * lane-order tests only. The production drainer is never driven this
     * way (its drainLoop owns the lock there).
     */
    void processOneForTest(ResumeToken token) {
        synchronized (lock) {
            route(token);
            admit();
            checkInvariants();
        }
    }
}
