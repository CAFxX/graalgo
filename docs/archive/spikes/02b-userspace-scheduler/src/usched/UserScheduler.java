package usched;

import java.util.ArrayDeque;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.LongAdder;
import java.util.concurrent.locks.LockSupport;

/**
 * Spike 2b: user-space scheduler for 1:1 virtual-thread goroutines.
 *
 * Design (Carlo's option C): keep 1:1 vthreads as the execution vehicle, demote
 * Loom's ForkJoinPool carrier to a dumb thread pool, and move ALL scheduling
 * discipline to user space:
 *
 * - A goroutine runs until its time quantum expires (checked at loop
 *   back-edge polls, see Safepoint) or it blocks on a channel/timer/select.
 * - It then parks INDEFINITELY via LockSupport.park() (not timed: the spike-2
 *   PARK mode was parkNanos, which resubmits to the FJP on expiry — a
 *   different, falsified mechanism) and enqueues a resume token.
 * - A dedicated platform-thread drainer takes tokens and unparks vthreads in
 *   FIFO order, with a priority lane (wakeFifo) for woken/channel-ready
 *   goroutines — the runnext slot of Go's scheduler.
 * - GOMAXPROCS is enforced as an in-flight cap: at most cap vthreads are
 *   unparked-but-not-yet-reparked at any time. The drainer hard-asserts
 *   inflight <= cap on every mutation.
 * - P1-1: effective cap = min(GOMAXPROCS, carrier parallelism). The
 *   bounded-submissions argument ("no FJP discipline assumed") requires
 *   in-flight <= carriers; a user-set GOMAXPROCS above the carrier count
 *   must not reintroduce dependence on FJP queue discipline.
 * - P0-2: scheduler-transparent blocking I/O (e.g. NIO reads) releases its
 *   in-flight slot via ioEnter() and re-acquires it via ioExit() through
 *   the wakeFifo lane — Go's netpoller P-release. Without this, N blocked
 *   I/O goroutines at cap=N starve every runnable goroutine.
 *
 * Park/unpark pairing safety: every park reserves a generation ticket via
 * armPark() BEFORE the waiter becomes visible to wakers. A waker wins the
 * wakeup for generation t only via armed.compareAndSet(t, -2); the drainer
 * matches WAKEUP events against IDLE events by generation, tolerating both
 * orders (wakeup-before-park via wakePending, park-before-wakeup via
 * idleSet). AwaitAck spins on acked >= t with LockSupport.park(); unpark
 * arriving early is absorbed by the LockSupport permit, spurious wakeups
 * re-park. Every unpark is preceded by acked.set(t), so no wakeup is lost
 * or duplicated.
 *
 * Verdict-scope: this is a scheduling-discipline prototype, not a Go
 * runtime. Channel/select/timer are models wired to this scheduler.
 */
public final class UserScheduler {
    enum Kind { INIT, RESUME, IDLE, EXIT, WAKEUP, BARRIER, IOBLOCK }

    static final class ParkEvent {
        final Goroutine g;
        final Kind kind;
        final long gen;
        final CountDownLatch latch;
        ParkEvent(Goroutine g, Kind kind, long gen, CountDownLatch latch) {
            this.g = g; this.kind = kind; this.gen = gen; this.latch = latch;
        }
    }

    static final class RunnableReq {
        final Goroutine g;
        final long gen;
        RunnableReq(Goroutine g, long gen) { this.g = g; this.gen = gen; }
    }

    private static final UserScheduler INSTANCE = new UserScheduler();
    public static UserScheduler instance() { return INSTANCE; }

    private final LinkedBlockingQueue<ParkEvent> parkQ = new LinkedBlockingQueue<>();
    private final LinkedBlockingQueue<ParkEvent> initQ = new LinkedBlockingQueue<>();
    private final MpscQueue<ParkEvent> mpscQ = new MpscQueue<>();
    private final MpscQueue<ParkEvent> mpscInitQ = new MpscQueue<>();
    private final boolean useMpsc = Boolean.getBoolean("usched.mpsc");
    private volatile Thread drainerThread;

    private void qOffer(ParkEvent e) {
        if (e.kind == Kind.INIT) {
            if (useMpsc) mpscInitQ.offer(e); else initQ.offer(e);
            // wake drainer if it's blocked in parkQ.take()/mpscQ.take()
            Thread d = drainerThread;
            if (d != null) LockSupport.unpark(d);
        } else {
            if (useMpsc) mpscQ.offer(e); else parkQ.offer(e);
        }
    }

    private ParkEvent qTake() throws InterruptedException {
        // INIT priority: new goroutines must not starve behind RESUME flood
        drainerThread = Thread.currentThread();
        try {
            if (useMpsc) {
                for (;;) {
                    ParkEvent e = mpscInitQ.poll();
                    if (e != null) return e;
                    e = mpscQ.poll();
                    if (e != null) return e;
                    Thread me = Thread.currentThread();
                    mpscQ.taker = me;
                    mpscInitQ.taker = me;
                    // re-check after setting taker (avoid lost wakeup)
                    if (mpscInitQ.poll() != null || mpscQ.poll() != null) {
                        mpscQ.taker = null; mpscInitQ.taker = null;
                        continue;
                    }
                    LockSupport.park();
                    mpscQ.taker = null; mpscInitQ.taker = null;
                    if (Thread.interrupted()) throw new InterruptedException();
                }
            } else {
                for (;;) {
                    ParkEvent e = initQ.poll();
                    if (e != null) return e;
                    // timed poll so INITs are checked promptly
                    e = parkQ.poll(100, TimeUnit.MICROSECONDS);
                    if (e != null) return e;
                }
            }
        } finally {
            drainerThread = null;
        }
    }
    /** Guards runFifo/wakeFifo/idleSet/wakePending/inflight. Only the drainer mutates. */
    private final Object lock = new Object();
    private final ArrayDeque<RunnableReq> runFifo = new ArrayDeque<>();
    private final ArrayDeque<RunnableReq> wakeFifo = new ArrayDeque<>();
    private final ArrayDeque<RunnableReq> initFifo = new ArrayDeque<>();
    private final Map<Goroutine, Long> idleSet = new HashMap<>();
    private final Map<Goroutine, Long> wakePending = new HashMap<>();
    /**
     * Goroutines blocked in scheduler-transparent I/O (see ioEnter): they
     * hold no in-flight slot (released at ioEnter) and re-acquire one via
     * the WAKEUP/wakeFifo path at ioExit. Drainer-only.
     */
    private final Map<Goroutine, Long> ioBlocked = new HashMap<>();

    private int inflight = 0;
    private final int cap;
    /** Requested GOMAXPROCS and carrier parallelism, for the clamp audit. */
    private final int requestedCap;
    private final int carrierParallelism;

    // ---- stats (thread-safe; read by tests) ----
    public final LongAdder parkEvents = new LongAdder();  // INIT+RESUME+IDLE processed
    public final LongAdder admissions = new LongAdder();  // unparks issued
    /**
     * Distinct goroutines admitted at least once. Sampled by fairness probes
     * (e.g. Quiescence) to distinguish "never admitted during the run"
     * (scheduler starvation) from "admitted but not yet drained after stop"
     * (test drain-window artifact).
     */
    public final LongAdder firstAdmissions = new LongAdder();
    private final java.util.Set<Long> admittedIds =
            java.util.Collections.newSetFromMap(new java.util.concurrent.ConcurrentHashMap<>());
    public final LongAdder wakeupCalls = new LongAdder();
    public final LongAdder wakeupWon = new LongAdder();
    public final AtomicLong unparkedNotResumed = new AtomicLong();
    public final AtomicLong maxUnparkedNotResumed = new AtomicLong();
    public final AtomicLong currentlyExecuting = new AtomicLong();
    public final AtomicLong maxTrueParallel = new AtomicLong();
    public final LongAdder drainerEvents = new LongAdder();
    public final AtomicLong drainerBusyNanos = new AtomicLong();
    public volatile long minInflightObserved = Long.MAX_VALUE;
    public final long wallStartNanos = System.nanoTime();

    private UserScheduler() {
        requestedCap = Integer.getInteger("usched.gomaxprocs", Runtime.getRuntime().availableProcessors());
        if (requestedCap < 1) throw new IllegalArgumentException("usched.gomaxprocs must be >= 1");
        // P1-1: the bounded-submissions argument ("no FJP discipline assumed")
        // requires in-flight <= carrier count. GOMAXPROCS is user-controlled
        // and independent of the carrier pool size, so clamp: oversubscription
        // becomes drainer-FIFO time-slicing (more Go-faithful than FJP
        // queueing), never untrusted FJP queue discipline.
        int par = Integer.getInteger("jdk.virtualThreadScheduler.parallelism",
                Runtime.getRuntime().availableProcessors());
        if (par < 1) par = Runtime.getRuntime().availableProcessors();
        carrierParallelism = par;
        cap = Math.min(requestedCap, carrierParallelism);
        Thread drainer = Thread.ofPlatform().daemon(true).name("usched-drainer").start(this::drainLoop);
        drainer.setPriority(Thread.MAX_PRIORITY);
    }

    public int cap() { return cap; }
    public int requestedCap() { return requestedCap; }
    public int carrierParallelism() { return carrierParallelism; }

    // ================= vthread side =================

    /**
     * Reserve a park generation. Call BEFORE the waiter becomes visible to any
     * waker (i.e., while still holding the registration lock).
     */
    public long armPark(Goroutine g) {
        long t = g.parkSeq.getAndIncrement();
        g.armed.set(t);
        return t;
    }

    /** Quantum-expiry park: no external waker exists. */
    public void quantumPark(Goroutine g) {
        long t = armPark(g);
        noteParking();
        if (LatencyProbe.ENABLED) LatencyProbe.parking(g);
        qOffer(new ParkEvent(g, Kind.RESUME, t, null));
        awaitAck(g, t);
        noteRunning();
    }

    /** Park on a blocking op; the holder of ticket t will wake us. */
    public void parkIdle(Goroutine g, long t) {
        noteParking();
        if (LatencyProbe.ENABLED) LatencyProbe.parking(g);
        qOffer(new ParkEvent(g, Kind.IDLE, t, null));
        awaitAck(g, t);
        noteRunning();
    }

    /**
     * P0-2: enter scheduler-transparent blocking I/O (e.g. a blocking NIO
     * read). The vthread is about to block in the kernel WITHOUT parking on
     * the user-space scheduler (Loom unmounts it, freeing the carrier), so
     * without this call it would hold its in-flight slot for the whole
     * block — with cap=N and N such blocks, runnable goroutines starve
     * (the standard Go server shape). ioEnter releases the slot: the
     * drainer records the goroutine in ioBlocked and decrements inflight,
     * WITHOUT enqueueing any resume token (there is nothing to resume yet).
     * Mirrors Go's netpoller P-release.
     *
     * Returns the ticket to pass to ioExit. Every ioEnter MUST be paired
     * with ioExit (even on exception); an unpaired ioEnter leaks nothing
     * from the scheduler's point of view (the slot stays released, which is
     * the safe direction), but the goroutine can never run again.
     */
    public long ioEnter(Goroutine g) {
        long t = armPark(g);
        noteParking();
        qOffer(new ParkEvent(g, Kind.IOBLOCK, t, null));
        return t;
    }

    /**
     * P0-2: blocking I/O completed. Re-acquires an in-flight slot through
     * the NORMAL token queue: the WAKEUP event is matched against ioBlocked
     * and the goroutine is admitted via the wakeFifo priority lane, then
     * this parks until admission (like any wakeup). Transient carrier
     * oversubscription between I/O completion and re-admission is bounded
     * and self-draining: every such vthread immediately re-parks here.
     */
    public void ioExit(Goroutine g, long t) {
        wakeup(g, t);
        awaitAck(g, t);
        noteRunning();
    }

    /** First park of a fresh vthread: admitted by the drainer like any quantum. */
    void parkInitial(Goroutine g) {
        long t = armPark(g);
        qOffer(new ParkEvent(g, Kind.INIT, t, null));
        awaitAck(g, t);
        noteRunning();
    }

    void noteExit(Goroutine g) {
        noteParking();
        qOffer(new ParkEvent(g, Kind.EXIT, -1, null));
    }

    private static final boolean SPIN_AWAIT = false;

    private void awaitAck(Goroutine g, long t) {
        if (SPIN_AWAIT) {
            while (g.acked.get() < t) Thread.onSpinWait();
        } else {
            while (g.acked.get() < t) LockSupport.park();
        }
        g.armed.set(-1);
        unparkedNotResumed.decrementAndGet();
        if (LatencyProbe.ENABLED) LatencyProbe.resumed(g);
    }

    /**
     * Wakeup for generation t. Returns true iff this call won the race.
     * Must be called with the waiter's registration still held/dequeued under
     * the same lock that guarded registration (callers guarantee at-most-once
     * dequeue; the CAS makes it at-most-once wakeup).
     */
    public boolean wakeup(Goroutine g, long t) {
        wakeupCalls.increment();
        if (g.armed.compareAndSet(t, -2)) {
            wakeupWon.increment();
            qOffer(new ParkEvent(g, Kind.WAKEUP, t, null));
            return true;
        }
        return false;
    }

    /** Test helper: returns after the drainer has processed all prior events. */
    public void awaitQuiescent() throws InterruptedException {
        CountDownLatch latch = new CountDownLatch(1);
        qOffer(new ParkEvent(null, Kind.BARRIER, -1, latch));
        latch.await();
    }

    // ================= drainer (single thread) =================

    private void drainLoop() {
        for (;;) {
            ParkEvent e;
            try {
                e = qTake();
            } catch (InterruptedException ie) {
                // P1-5: the drainer is non-terminating. A stray interrupt
                // must not kill the entire scheduler (every parked goroutine
                // would hang forever with no recovery). Swallow-and-continue;
                // production hardens this with a supervisor-restart.
                continue;
            }
            if (LatencyProbe.ENABLED) LatencyProbe.drainerTook(e.g);
            long t0 = System.nanoTime();
            synchronized (lock) {
                drainerEvents.increment();
                process(e);
                admit();
                if (inflight < minInflightObserved) minInflightObserved = inflight;
                if (inflight > cap)
                    throw new AssertionError("inflight=" + inflight + " > cap=" + cap);
                if (inflight < 0)
                    throw new AssertionError("inflight negative: " + inflight);
            }
            drainerBusyNanos.addAndGet(System.nanoTime() - t0);
        }
    }

    private void process(ParkEvent e) {
        switch (e.kind) {
            case INIT -> {
                parkEvents.increment();
                initFifo.addLast(new RunnableReq(e.g, e.gen));
            }
            case RESUME -> {
                parkEvents.increment();
                inflight--;
                runFifo.addLast(new RunnableReq(e.g, e.gen));
            }
            case IDLE -> {
                parkEvents.increment();
                inflight--;
                Long wp = wakePending.remove(e.g);
                if (wp != null && wp.longValue() == e.gen) {
                    wakeFifo.addLast(new RunnableReq(e.g, e.gen));
                } else {
                    idleSet.put(e.g, e.gen);
                }
            }
            case EXIT -> {
                inflight--;
            }
            case IOBLOCK -> {
                // P0-2: vthread entered scheduler-transparent blocking I/O.
                // Release the in-flight slot WITHOUT enqueueing a resume
                // token. If ioExit already ran (WAKEUP won the race before
                // this event arrived), admit via wakeFifo immediately.
                parkEvents.increment();
                inflight--;
                Long wp = wakePending.remove(e.g);
                if (wp != null && wp.longValue() == e.gen) {
                    wakeFifo.addLast(new RunnableReq(e.g, e.gen));
                } else {
                    ioBlocked.put(e.g, e.gen);
                }
            }
            case WAKEUP -> {
                Long idl = idleSet.remove(e.g);
                if (idl == null) idl = ioBlocked.remove(e.g);
                if (idl != null && idl.longValue() == e.gen) {
                    wakeFifo.addLast(new RunnableReq(e.g, e.gen));
                } else {
                    // Not parked yet (wakeup won the race before the IDLE
                    // or IOBLOCK event arrived), or already re-parked: hold
                    // pending. Stale generations self-clean on the next
                    // IDLE/IOBLOCK.
                    wakePending.put(e.g, e.gen);
                }
            }
            case BARRIER -> {
                if (e.latch != null) e.latch.countDown();
            }
        }
    }

    private void admit() {
        while (inflight < cap) {
            RunnableReq r = wakeFifo.pollFirst();
            if (r == null) r = initFifo.pollFirst();
            if (r == null) r = runFifo.pollFirst();
            if (r == null) break;
            inflight++;
            admissions.increment();
            if (admittedIds.add(r.g.id)) firstAdmissions.increment();
            long u = unparkedNotResumed.incrementAndGet();
            maxUnparkedNotResumed.accumulateAndGet(u, Math::max);
            // ack BEFORE unpark: reversing this order loses wakeups.
            r.g.acked.set(r.gen);
            if (LatencyProbe.ENABLED) LatencyProbe.unparking(r.g);
            LockSupport.unpark(r.g.thread);
        }
    }

    private void noteParking() {
        currentlyExecuting.decrementAndGet();
    }

    private void noteRunning() {
        long c = currentlyExecuting.incrementAndGet();
        maxTrueParallel.accumulateAndGet(c, Math::max);
    }

    /** One-line stats for logs. */
    public String statsLine() {
        double wallS = (System.nanoTime() - wallStartNanos) / 1e9;
        double drainerUtil = drainerBusyNanos.get() / 1e9 / wallS * 100.0;
        return String.format(
                "sched{cap=%d(req=%d,par=%d) inflight_now~na parkEvents=%d admissions=%d firstAdmissions=%d wakeupCalls=%d wakeupWon=%d "
                + "maxUnparkedNotResumed=%d maxTrueParallel=%d minInflightObserved=%d "
                + "drainerEvents=%d drainerUtilPct=%.2f}",
                cap, requestedCap, carrierParallelism, parkEvents.sum(), admissions.sum(),
                firstAdmissions.sum(), wakeupCalls.sum(), wakeupWon.sum(),
                maxUnparkedNotResumed.get(), maxTrueParallel.get(), minInflightObserved,
                drainerEvents.sum(), drainerUtil);
    }
}
