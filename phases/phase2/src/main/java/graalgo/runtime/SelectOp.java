package graalgo.runtime;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ThreadLocalRandom;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;

/**
 * A single {@code select} operation (P2-M2, WS B; GG-0017 chan slice).
 *
 * <p>Go select semantics:
 * <ol>
 *   <li>Poll every case under each channel's monitor: a send case is
 *       ready if a receiver waits or the buffer has space (send on a
 *       closed channel is poison, never ready); a recv case is ready if
 *       a sender waits, the buffer is non-empty, or the channel is
 *       closed.
 *   <li>If any case is ready, complete one chosen uniformly at random
 *       (Go fairness: no case starves under contention) and return its
 *       index.
 *   <li>If none is ready and a {@code default} case is present (a
 *       {@code SelectCase} with {@code chan == null} — documented
 *       convention for the future exporter), return its index.
 *   <li>Otherwise block: enqueue one {@link SelectCaseWaiter} per
 *       (channel, case) — re-checking under each monitor so no wakeup is
 *       missed — and park scheduler-aware. Exactly one waker wins the
 *       shared CAS claim; the resumed goroutine removes itself from all
 *       other channels (one monitor at a time: no lock-ordering issue).
 * </ol>
 *
 * <p>The chosen case completes atomically under the winning channel's
 * monitor. A received value/ok is stashed here and read via
 * {@link ChanOps#selectRecvValue} / {@link ChanOps#selectRecvOk}.
 */
public final class SelectOp {
    /** Unclaimed sentinel for the shared claim. */
    private static final Object UNCLAIMED = new Object();

    private final ChanOps.SelectCase[] cases;
    /** Shared claim: UNCLAIMED, or the winning SelectCaseWaiter. */
    private final AtomicReference<Object> claim = new AtomicReference<>(UNCLAIMED);

    private volatile int resultIndex = -1;
    private volatile Object resultValue;
    private volatile boolean resultOk;
    private volatile boolean resultPoison;

    public SelectOp(ChanOps.SelectCase[] cases) {
        this.cases = cases.clone();
    }

    /** One (channel, case) waiter entry enqueued on a channel's queue. */
    static final class SelectCaseWaiter extends GoChan.ChanWaiter {
        final SelectOp op;
        final int caseIndex;
        final boolean isSend;
        final Object sendValue;
        final GoChan chan;

        SelectCaseWaiter(SelectOp op, int caseIndex, GoChan chan,
                boolean isSend, Object sendValue, GoroutineHandle handle) {
            super(handle);
            this.op = op;
            this.caseIndex = caseIndex;
            this.chan = chan;
            this.isSend = isSend;
            this.sendValue = sendValue;
        }

        /**
         * Tries to claim this select operation for the waking channel.
         * Exactly one waker across all channels wins.
         */
        boolean claim() {
            return op.claim.compareAndSet(UNCLAIMED, this);
        }

        /** Non-mutating check: has another channel already claimed us? */
        boolean isClaimed() {
            return op.claim.get() != UNCLAIMED;
        }

        @Override
        boolean accept() {
            return claim();
        }
    }

    /** Called by the winning waker under the channel monitor, pre-unpark. */
    void setResult(int caseIndex, Object value, boolean ok) {
        this.resultIndex = caseIndex;
        this.resultValue = value;
        this.resultOk = ok;
    }

    /** Called by close() when it poisons a blocked select send case. */
    void setPoison(int caseIndex) {
        this.resultIndex = caseIndex;
        this.resultPoison = true;
    }

    /**
     * Executes the select on the current goroutine. Returns the chosen
     * case index; for a recv case the value/ok are stashed for
     * {@link ChanOps#selectRecvValue}/{@link ChanOps#selectRecvOk}.
     */
    public int execute() {
        SchedulerCore sched = SchedulerCore.current();
        GoroutineHandle self = sched.currentGoroutine();

        for (;;) {
            // ---- Phase 1: poll ----
            List<Integer> ready = new ArrayList<>();
            int defaultIndex = -1;
            boolean poison = false;
            for (int i = 0; i < cases.length; i++) {
                ChanOps.SelectCase c = cases[i];
                if (c.chan() == null) {
                    defaultIndex = i;
                    continue;
                }
                GoChan ch = requireGoChan(c.chan());
                synchronized (ch) {
                    if (c.send()) {
                        if (ch.isClosed()) {
                            poison = true;
                        } else if (ch.canSend()) {
                            ready.add(i);
                        }
                    } else if (ch.canRecv()) {
                        ready.add(i);
                    }
                }
            }
            if (!ready.isEmpty()) {
                int chosen = ready.get(ThreadLocalRandom.current().nextInt(ready.size()));
                Integer done = tryCompleteReady(chosen, sched);
                if (done != null) {
                    return done;
                }
                // Lost the race between poll and completion: re-poll.
                // Each retry consumes a genuine contention event.
                continue;
            }
            if (defaultIndex >= 0) {
                return defaultIndex;
            }

            // ---- Phase 2: block ----
            List<SelectCaseWaiter> mine = new ArrayList<>();
            Integer immediate = null;
            for (int i = 0; i < cases.length && immediate == null; i++) {
                ChanOps.SelectCase c = cases[i];
                if (c.chan() == null) {
                    continue;
                }
                GoChan ch = requireGoChan(c.chan());
                synchronized (ch) {
                    // Re-check under the monitor: a case that became ready
                    // between poll and enqueue completes immediately.
                    // (Never enqueue on a poisoned channel.)
                    if (!(c.send() && ch.isClosed())) {
                        Integer doneNow = completeUnderLock(ch, i, sched);
                        if (doneNow != null) {
                            immediate = doneNow;
                        } else {
                            SelectCaseWaiter w = new SelectCaseWaiter(this, i, ch,
                                    c.send(), c.sendValue(), self);
                            ch.enqueueSelectWaiter(w, c.send());
                            mine.add(w);
                        }
                    } else {
                        poison = true;
                    }
                }
            }
            if (immediate != null) {
                cleanup(mine);
                return immediate;
            }
            if (mine.isEmpty()) {
                // All send cases poisoned (or zero cases): nothing to wait on.
                Panic.throwPanic(GoStrings.of("send on closed channel"));
            }
            // Scheduler-aware park. A waker that claimed us before we park
            // has its UNPARK stashed by the drainer — no wakeup lost.
            sched.parkCurrent(null);
            try {
                if (resultPoison) {
                    Panic.throwPanic(GoStrings.of("send on closed channel"));
                }
                return resultIndex;
            } finally {
                cleanup(mine);
            }
        }
    }

    /**
     * Tries to complete a poll-ready case atomically. Returns the case
     * index on success, null if the race was lost (caller re-polls).
     */
    private Integer tryCompleteReady(int chosen, SchedulerCore sched) {
        ChanOps.SelectCase c = cases[chosen];
        GoChan ch = requireGoChan(c.chan());
        synchronized (ch) {
            if (c.send()) {
                if (ch.isClosed() || !ch.trySend(c.sendValue(), sched)) {
                    return null;
                }
                resultIndex = chosen;
                resultOk = true;
                return chosen;
            }
            GoChan.RecvResult rr = ch.tryRecv(sched);
            if (rr == null) {
                return null;
            }
            resultIndex = chosen;
            resultValue = rr.value();
            resultOk = rr.ok();
            return chosen;
        }
    }

    /**
     * Caller holds ch's monitor. Completes the case if still possible;
     * returns the case index, or null if the race was lost.
     */
    private Integer completeUnderLock(GoChan ch, int chosen, SchedulerCore sched) {
        ChanOps.SelectCase c = cases[chosen];
        if (c.send()) {
            if (ch.isClosed() || !ch.trySend(c.sendValue(), sched)) {
                return null;
            }
            resultIndex = chosen;
            resultOk = true;
            return chosen;
        }
        GoChan.RecvResult rr = ch.tryRecv(sched);
        if (rr == null) {
            return null;
        }
        resultIndex = chosen;
        resultValue = rr.value();
        resultOk = rr.ok();
        return chosen;
    }

    /** Removes our waiter entries from all channels (post-resume). */
    private void cleanup(List<SelectCaseWaiter> mine) {
        for (SelectCaseWaiter w : mine) {
            w.chan.removeSelectWaiter(w, w.isSend);
        }
    }

    private static GoChan requireGoChan(ChanOps.Chan ch) {
        if (!(ch instanceof GoChan g)) {
            throw new IllegalArgumentException(
                    "select on non-GoChan channel: " + ch.getClass());
        }
        return g;
    }

    // ---- result accessors (read by ChanOps after execute) ----

    Object resultValue() {
        return resultValue;
    }

    boolean resultOk() {
        return resultOk;
    }
}
