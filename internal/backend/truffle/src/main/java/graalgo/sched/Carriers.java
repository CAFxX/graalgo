package graalgo.sched;

import java.lang.invoke.MethodHandles;
import java.lang.invoke.VarHandle;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.LinkedTransferQueue;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.locks.LockSupport;
import java.util.function.Function;
import java.util.function.Supplier;

import graalgo.language.GoThreadSlot;
import graalgo.runtime.GoroutineState;
import jdk.internal.vm.Continuation;
import jdk.internal.vm.ContinuationScope;

/**
 * Goroutines as continuations on a small pool of carrier threads (ADR-0018).
 *
 * <p>A goroutine is a {@link Continuation}, not a thread: spawning one allocates a handle and a
 * continuation and touches no thread registry, so its cost does not depend on how many
 * goroutines are alive (a polyglot thread per goroutine made every spawn walk all of them:
 * {@code PolyglotContextImpl.checkAllThreadAccesses}). The carriers are the only threads that run
 * Go code: inside a context they are polyglot threads, entered once for their whole life, so the
 * language's context thread-local belongs to the carrier and the per-goroutine values in it are
 * swapped on every mount ({@link #mount}/{@link #unmount}).
 *
 * <p>Park/unpark is {@link java.util.concurrent.locks.LockSupport}'s permit protocol, per
 * goroutine: {@link #park} yields the continuation unless a permit is pending; {@link #unpark}
 * grants the permit and queues the goroutine if it is parked. A park that cannot yield (a
 * monitor or a native frame on the goroutine's stack pins it) blocks its carrier instead, and a
 * spare carrier replaces it while it blocks, as for a goroutine in a blocking system call
 * ({@link #beginBlocking}): the pool keeps {@link #target} carriers able to run goroutines.
 *
 * <p>The admission protocol above this class is unchanged: the drainer decides which goroutine
 * may run and in-flight never exceeds the cap, so the pool needs no fairness of its own: one
 * FIFO run queue. A direct handoff (a goroutine waking the one that runs next on its slot) goes
 * to the waker's carrier ({@link Carrier#next}), which runs it as soon as the waker yields.
 */
final class Carriers {
    /** The scope every goroutine continuation runs in. */
    static final ContinuationScope SCOPE = new ContinuationScope("graalgo-goroutine");

    /** Goroutine run states ({@link GoroutineHandle#runState}). */
    static final int RUNNING = 0;
    static final int PARKING = 1;
    static final int PARKED = 2;
    static final int QUEUED = 3;
    static final int PINNED = 4;

    private static final VarHandle RUN_STATE;
    private static final VarHandle PERMIT;

    static {
        try {
            MethodHandles.Lookup l = MethodHandles.lookup();
            RUN_STATE = l.findVarHandle(GoroutineHandle.class, "runState", int.class);
            PERMIT = l.findVarHandle(GoroutineHandle.class, "permit", boolean.class);
        } catch (ReflectiveOperationException e) {
            throw new ExceptionInInitializerError(e);
        }
    }

    /** How long a spare carrier (above {@link #target}) idles before it retires. */
    private static final long SPARE_IDLE_NANOS = TimeUnit.SECONDS.toNanos(10);

    /** One goroutine's continuation: a park that cannot yield returns false instead of throwing. */
    private static final class GoContinuation extends Continuation {
        GoContinuation(Runnable body) {
            super(SCOPE, body);
        }

        @Override
        protected void onPinned(Continuation.Pinned reason) {
            // yield() returns false; park() blocks the carrier instead.
        }
    }

    /** One carrier thread's state. */
    static final class Carrier {
        final Thread thread;
        /** The language's context thread-local of this carrier, or null outside a context. */
        final GoThreadSlot slot;
        /** A handed-off goroutine to run as soon as the current one yields (owner thread only). */
        GoroutineHandle next;

        Carrier(Thread thread, GoThreadSlot slot) {
            this.thread = thread;
            this.slot = slot;
        }
    }

    private static final ThreadLocal<Carrier> CURRENT = new ThreadLocal<>();

    private final SchedulerCore sched;
    private final Function<Runnable, Thread> carrierThreads;
    private final Supplier<GoThreadSlot> slots;
    private final LinkedTransferQueue<Object> runq = new LinkedTransferQueue<>();
    /** Carriers that should be able to run goroutines: the scheduler's carrier parallelism. */
    private final int target;
    /** Carrier threads alive (started, not retired). */
    private final AtomicInteger alive = new AtomicInteger();
    /** Carriers blocked under a goroutine (blocking system call, pinned park). */
    private final AtomicInteger blocked = new AtomicInteger();
    /** Every carrier thread started and not yet retired (terminate joins them). */
    private final java.util.Set<Thread> threads = java.util.concurrent.ConcurrentHashMap.newKeySet();
    private volatile boolean stopped;
    /** Parks that could not yield and blocked their carrier (diagnostics, tests). */
    final AtomicInteger pinnedParks = new AtomicInteger();

    /**
     * @param carrierThreads builds an unstarted carrier thread around its loop (polyglot threads
     *     inside a context)
     * @param slots the calling carrier's context thread-local slot, or null outside a context
     */
    Carriers(SchedulerCore sched, int target, Function<Runnable, Thread> carrierThreads,
            Supplier<GoThreadSlot> slots) {
        this.sched = sched;
        this.target = Math.max(1, target);
        this.carrierThreads = carrierThreads;
        this.slots = slots;
    }

    /** A new goroutine's continuation, parked until its first {@link #unpark}. */
    void prepare(GoroutineHandle h, Runnable body) {
        h.cont = new GoContinuation(body);
        h.runState = PARKED;
    }

    // ---------------------------------------------------------------- goroutine side

    /**
     * Parks the calling goroutine until {@link #unpark}: returns at once when a permit is
     * pending; may return spuriously (callers re-check their condition).
     */
    void park(GoroutineHandle h) {
        if ((boolean) PERMIT.getAndSet(h, false)) {
            return;
        }
        Carrier c = h.carrier;
        RUN_STATE.setVolatile(h, PARKING);
        if (Continuation.yield(SCOPE)) {
            // Resumed by a carrier (possibly another one) after an unpark.
            PERMIT.setVolatile(h, false);
            return;
        }
        // Pinned: the continuation cannot unmount. Block the carrier, with a spare in its place.
        pinnedParks.incrementAndGet();
        GoroutineHandle n = c.next;
        if (n != null) {
            c.next = null;
            submit(n);
        }
        RUN_STATE.setVolatile(h, PINNED);
        beginBlocking();
        try {
            while (!(boolean) PERMIT.getAndSet(h, false)) {
                LockSupport.park(this);
            }
        } finally {
            RUN_STATE.setVolatile(h, RUNNING);
            endBlocking();
        }
    }

    /**
     * Grants {@code h} a permit; queues it when it is parked. {@code handoff}: the caller is a
     * goroutine handing its slot to {@code h} and about to park, so {@code h} runs next on the
     * caller's carrier.
     */
    void unpark(GoroutineHandle h, boolean handoff) {
        if ((boolean) PERMIT.getAndSet(h, true)) {
            return; // a permit is already pending: the goroutine will see it
        }
        int s = (int) RUN_STATE.getVolatile(h);
        if (s == PARKED) {
            if (RUN_STATE.compareAndSet(h, PARKED, QUEUED)) {
                Carrier c;
                if (handoff && (c = CURRENT.get()) != null && c.next == null) {
                    c.next = h;
                } else {
                    submit(h);
                }
            }
        } else if (s == PINNED) {
            LockSupport.unpark(h.carrier.thread);
        }
        // RUNNING / PARKING / QUEUED: the goroutine (or its carrier, after the yield) sees the
        // permit.
    }

    /**
     * The calling goroutine's carrier is about to block outside the scheduler (a system call):
     * a spare carrier keeps {@link #target} carriers available meanwhile.
     */
    void beginBlocking() {
        blocked.incrementAndGet();
        ensureCarriers();
    }

    /** Ends {@link #beginBlocking}. */
    void endBlocking() {
        blocked.decrementAndGet();
    }

    /**
     * Binds the calling goroutine to its carrier until {@link #unpinCurrent}: its parks block the
     * carrier instead of yielding (a spare carrier takes over meanwhile). For regions that hold
     * carrier-confined resources across a park (thread-confined native memory around a system
     * call).
     */
    static void pinCurrent() {
        Continuation.pin();
    }

    /** Ends {@link #pinCurrent}. */
    static void unpinCurrent() {
        Continuation.unpin();
    }

    // ---------------------------------------------------------------- carriers

    private void submit(GoroutineHandle h) {
        runq.offer(h);
        if (!runq.hasWaitingConsumer()) {
            ensureCarriers();
        }
    }

    /** Starts carriers until {@code target} of them are not blocked. */
    private void ensureCarriers() {
        for (;;) {
            int a = alive.get();
            if (stopped || a - blocked.get() >= target) {
                return;
            }
            if (alive.compareAndSet(a, a + 1)) {
                startCarrier();
            }
        }
    }

    private void startCarrier() {
        Thread[] self = new Thread[1];
        Thread t;
        try {
            t = carrierThreads.apply(() -> loop(self[0]));
        } catch (RuntimeException | Error e) {
            alive.decrementAndGet();
            throw e;
        }
        self[0] = t;
        t.setName("graalgo-carrier-" + t.threadId());
        t.setDaemon(true);
        t.start();
    }

    private void loop(Thread self) {
        // Registered by the carrier itself (stop() joins the members), removed when it ends.
        threads.add(self);
        Carrier c = new Carrier(self, slots == null ? null : slots.get());
        CURRENT.set(c);
        try {
            for (;;) {
                Object h = c.next;
                if (h != null) {
                    c.next = null;
                } else {
                    h = runq.poll();
                    if (h == null) {
                        if (stopped) {
                            return;
                        }
                        try {
                            h = runq.poll(SPARE_IDLE_NANOS, TimeUnit.NANOSECONDS);
                        } catch (InterruptedException ie) {
                            continue; // carriers ignore interrupts; stop() wakes them
                        }
                        if (h == null) {
                            if (stopped || retireSpare()) {
                                return;
                            }
                            continue;
                        }
                    }
                }
                if (h == STOP) {
                    return;
                }
                try {
                    run(c, (GoroutineHandle) h);
                } catch (Throwable t) {
                    // The goroutine body catches everything and offers its EXIT token in a
                    // finally block; a throwable here means the scheduler's accounting is
                    // corrupt. Fail loud, as the drainer does: never a silently lost carrier.
                    System.err.println("FATAL: goroutine carrier failed, halting: " + t);
                    t.printStackTrace();
                    Runtime.getRuntime().halt(1);
                }
            }
        } finally {
            CURRENT.remove();
            threads.remove(self);
        }
    }

    /** An idle carrier above {@link #target} retires. */
    private boolean retireSpare() {
        for (;;) {
            int a = alive.get();
            if (a - blocked.get() <= target) {
                return false;
            }
            if (alive.compareAndSet(a, a - 1)) {
                return true;
            }
        }
    }

    private void run(Carrier c, GoroutineHandle h) {
        h.carrier = c;
        mount(c, h);
        RUN_STATE.setVolatile(h, RUNNING);
        Continuation cont = (Continuation) h.cont;
        try {
            cont.run();
        } finally {
            unmount(c, h);
        }
        if (cont.isDone()) {
            h.cont = null;
            h.carrier = null;
            return;
        }
        // Yielded in park(): parked unless a permit arrived meanwhile.
        RUN_STATE.setVolatile(h, PARKED);
        if ((boolean) PERMIT.getVolatile(h) && RUN_STATE.compareAndSet(h, PARKED, QUEUED)) {
            submit(h);
        }
    }

    /** Installs {@code h}'s goroutine-local values on the carrier. */
    private void mount(Carrier c, GoroutineHandle h) {
        sched.mountLocals(h);
        GoroutineState.swapHolder(h.savedState);
        GoThreadSlot s = c.slot;
        if (s != null) {
            s.state = h.savedState;
            s.g = h.savedG;
        }
    }

    /** Saves the carrier's goroutine-local values into {@code h}. */
    private void unmount(Carrier c, GoroutineHandle h) {
        h.savedState = GoroutineState.swapHolder(null);
        GoThreadSlot s = c.slot;
        if (s != null) {
            h.savedG = s.g;
            s.state = null;
            s.g = null;
        }
        sched.unmountLocals();
    }

    /** Sentinel that stops one carrier. */
    private static final Object STOP = new Object();

    /**
     * Stops the pool (scheduler teardown): idle carriers exit, a carrier running or blocked under
     * a goroutine exits once that goroutine yields or ends. Returns the carrier threads.
     */
    List<Thread> stop() {
        stopped = true;
        List<Thread> all = new ArrayList<>(threads);
        for (int i = 0; i < all.size(); i++) {
            runq.offer(STOP);
        }
        return all;
    }

    /** Carriers alive now (tests). */
    int aliveCarriers() {
        return alive.get();
    }
}
