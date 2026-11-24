package graalgo.sched;

import java.util.ArrayDeque;
import java.util.Deque;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.LinkedBlockingQueue;

/**
 * The single platform-thread drainer (P2-D2, P2-D3).
 *
 * <p>Loop: take (spin briefly, then block) from the MPSC inbox → route the token into its
 * lane → admit while {@code inflight < cap} → hard-assert the invariants.
 * Lane order per GG-WAKE-0003: {@code wakeFifo} &gt; {@code initFifo} &gt;
 * {@code runFifo}. The drainer is non-terminating: a stray interrupt is
 * swallowed and the loop continues (otherwise every parked goroutine would
 * hang forever with no recovery).
 *
 * <p>Admission does <strong>ack-before-unpark</strong> (GG-WAKE-0001):
 * {@code acked.set(generation)} precedes the unpark ({@link SchedulerCore#wake});
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
    // handle ids a HANDOFF already admitted (on the waker's slot) before
    // their PARKED token arrived: that PARKED only releases the old slot.
    private final java.util.Set<Long> handedOff = new java.util.HashSet<>();
    // handle ids currently in blocking I/O (P2-M3, GG-0063): slot
    // released, vthread blocked in the syscall (not parked). IO_EXIT
    // re-admits through the WAKE lane.
    private final java.util.Set<Long> inIo = new java.util.HashSet<>();
    // handle id -> handle, for every entry in `parked` (deadlock dump).
    private final java.util.Map<Long, GoroutineHandle> parkedHandles = new java.util.HashMap<>();
    // parked handle ids woken from outside the scheduler (netpoller).
    private final java.util.Set<Long> externalParked = new java.util.HashSet<>();
    /**
     * Upper bound the in-flight count may legitimately reach: the cap,
     * except right after runtime.GOMAXPROCS lowered it while more
     * goroutines were in flight — then the ceiling is the in-flight count
     * at the change and shrinks to the cap as they park (no admission
     * happens above the cap; the assert still catches any over-admission).
     */
    private int capCeiling;

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
    /** Direct handoffs booked (HANDOFF tokens): admissions on the waker's slot, not a lane's. */
    public final java.util.concurrent.atomic.AtomicLong wakeHandoff = new java.util.concurrent.atomic.AtomicLong();

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

    /** Set by {@link #stop}: the drain loop ends at its next token. */
    private volatile boolean stopped;

    /**
     * Ends the drain loop (scheduler teardown, SchedulerCore#terminate):
     * goroutines unwinding after teardown offer EXIT tokens for goroutines
     * that were never admitted, which the accounting must not see.
     */
    void stop() {
        stopped = true;
        inbox.offer(ResumeToken.poke());
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

    /**
     * How long the drainer polls an empty inbox before it blocks (WP-POLLCOST follow-up,
     * performance.md): a goroutine hop (unpark, then park) offers two tokens a few microseconds
     * apart, and a blocked drainer pays a futex wake-up (tens of µs on a loaded host) for each.
     * Spinning this long after the last token keeps a steady park/unpark stream off the futex;
     * an idle scheduler spins once per burst and then blocks as before. Only the waiting
     * changes: tokens are processed in the same order by the same thread.
     */
    static final long SPIN_NANOS = 50_000L;

    private ResumeToken next() throws InterruptedException {
        ResumeToken t = inbox.poll();
        if (t != null) {
            return t;
        }
        long deadline = System.nanoTime() + SPIN_NANOS;
        do {
            Thread.onSpinWait();
            t = inbox.poll();
            if (t != null) {
                return t;
            }
        } while (System.nanoTime() < deadline);
        return inbox.take();
    }

    private void drainLoop() {
        for (;;) {
            final ResumeToken token;
            try {
                token = next();
            } catch (InterruptedException ie) {
                // Non-terminating drainer: a stray interrupt must not kill
                // the scheduler. Swallow and continue.
                continue;
            }
            if (stopped) {
                return;
            }
            try {
                synchronized (lock) {
                    step(token);
                    detectDeadlock();
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
            case POKE -> {
                // Global re-evaluation only (cap change, timer retired):
                // admit() and detectDeadlock() run after every token.
            }
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
                bookPark(token.handle, token.generation);
            }
            case HANDOFF -> {
                // The parker's slot passes to the target: no inflight change.
                GoroutineHandle target = token.target;
                if (inIo.contains(token.handle.id) || inIo.contains(target.id)) {
                    throw new AssertionError("handoff g" + token.handle.id + " -> g" + target.id
                            + " involves a goroutine in blocking I/O");
                }
                if (parked.remove(target.id) != null) {
                    parkedHandles.remove(target.id);
                    externalParked.remove(target.id);
                } else if (!handedOff.add(target.id)) {
                    throw new AssertionError("duplicate handoff to g" + target.id);
                }
                if (unparkPending.contains(target.id)) {
                    throw new AssertionError("handoff to g" + target.id + " with an UNPARK pending");
                }
                wakeHandoff.incrementAndGet();
                admissions++;
                bookPark(token.handle, token.generation);
            }
            case UNPARK -> {
                Long gen = parked.remove(token.handle.id);
                if (gen != null) {
                    parkedHandles.remove(token.handle.id);
                    externalParked.remove(token.handle.id);
                    wakeUnparkDirect.incrementAndGet();
                    wakeFifo.addLast(new ResumeToken(
                            ResumeToken.Kind.WAKE, token.handle, gen));
                } else {
                    // Park hasn't been processed yet: remember the wakeup.
                    unparkPending.add(token.handle.id);
                }
            }
            case IO_ENTER -> {
                // Slot-releasing blocking I/O (P2-M3, GG-0063,
                // GG-SCHED-0006): the goroutine no longer counts against
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
                sched.wake(token.handle, false);
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

    /**
     * Books a park (the goroutine of a PARKED token, or a HANDOFF's parker,
     * whose slot the caller has already accounted): already admitted when a
     * HANDOFF to it came first (it runs on that waker's slot, and the slot
     * it held until now was released or passed on by the caller); woken at
     * once through the priority lane when its UNPARK came first; else
     * recorded as parked.
     */
    private void bookPark(GoroutineHandle h, long generation) {
        if (handedOff.remove(h.id)) {
            return;
        }
        if (unparkPending.remove(h.id)) {
            // Wakeup arrived first: admit immediately through the
            // priority lane (no wakeup lost).
            wakeUnparkStashed.incrementAndGet();
            wakeFifo.addLast(new ResumeToken(ResumeToken.Kind.WAKE, h, generation));
        } else {
            parked.put(h.id, generation);
            parkedHandles.put(h.id, h);
            if (h.externalWake) {
                externalParked.add(h.id);
            }
        }
    }

    /**
     * One drainer iteration: route, admit, check. The cap is read ONCE
     * and both admission and the invariant check use that value: a
     * concurrent {@code runtime.GOMAXPROCS} change lands between
     * iterations, never between admitting and judging the admissions
     * (re-reading it there let a raise-then-lower race fail the check on
     * a correct admission sequence — a halt(1) of a correct program).
     */
    private void step(ResumeToken token) {
        int cap = sched.cap();
        route(token);
        admit(cap);
        checkInvariants(cap);
        int free = cap - inflight;
        if (free != freeSlots) {
            freeSlots = free;
        }
    }

    /**
     * Idle admission slots as of the drainer's last step ({@code cap - inflight}; lanes
     * are empty whenever it is positive). A hint for {@code SchedulerCore#spinForHandoff},
     * never for admission.
     */
    private volatile int freeSlots;

    int freeSlots() {
        return freeSlots;
    }

    private void admit(int cap) {
        if (sched.isExiting()) {
            return; // process exit: the world is stopped, nothing runs again
        }
        while (inflight < cap) {
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
            sched.wake(token.handle, false);
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
        checkInvariants(sched.cap());
    }

    /** {@link #checkInvariants()} against the cap the iteration admitted under. */
    private void checkInvariants(int cap) {
        if (capCeiling < cap) {
            capCeiling = cap; // cap raised (or first check)
        } else if (capCeiling > cap) {
            // cap lowered: the excess in flight drains, never refills
            capCeiling = Math.max(cap, Math.min(capCeiling, inflight));
        }
        if (inflight > capCeiling) {
            throw new AssertionError(
                    "inflight=" + inflight + " > cap=" + cap);
        }
        if (inflight < 0) {
            throw new AssertionError("inflight negative: " + inflight);
        }
    }

    /**
     * gc {@code checkdead}: when no goroutine is in flight or queued for
     * admission, none is in blocking I/O, none waits on the netpoller,
     * and no token is pending, every goroutine is parked on another
     * goroutine — and if no timer can fire either, none will ever run
     * again. The scheduler then reports the deadlock (or, after main's
     * Goexit, "no goroutines") and exits. Called after every token with
     * the lock held; the cheap conditions short-circuit first.
     *
     * <p>Race-freedom: every waker that is a goroutine offers its UNPARK
     * before it parks or exits itself (one FIFO inbox), so its wakee is
     * never seen parked while it is not; the timer heap offers its wakeups
     * before retiring the entry ({@code TimerHeap.hasPending} counts
     * entries being fired), so the inbox is re-checked after the timer
     * probe.
     */
    private void detectDeadlock() {
        if (!sched.deadlockArmed() || inflight != 0 || !wakeFifo.isEmpty() || !initFifo.isEmpty()
                || !runFifo.isEmpty() || !inIo.isEmpty() || !externalParked.isEmpty()
                || !unparkPending.isEmpty() || !handedOff.isEmpty() || !inbox.isEmpty()
                || sched.isExiting()) {
            return;
        }
        boolean timers = sched.timerHeap().hasPending();
        if (!inbox.isEmpty() || (timers && !sched.mainGoexited())) {
            return;
        }
        sched.onAllParked(new java.util.ArrayList<>(parkedHandles.values()), timers);
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
            java.util.TreeSet<String> handedSorted = new java.util.TreeSet<>();
            for (Long id : handedOff) {
                handedSorted.add(name.apply(id));
            }
            StringBuilder sb = new StringBuilder();
            sb.append("inflight=").append(inflight)
              .append(";admissions=").append(admissions)
              .append(";max=").append(maxInflightObserved)
              .append(";parked=").append(parkedSorted)
              .append(";pending=").append(pendingSorted)
              .append(";io=").append(ioSorted)
              .append(";handedOff=").append(handedSorted);
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
              .append(wakeUnparkStashed.get()).append("/").append(wakeIoExit.get())
              .append("/").append(wakeHandoff.get());
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
            step(token);
        }
    }
}
