package graalgo.runtime;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ThreadLocalRandom;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;
import graalgo.jfr.ChannelOp;
import graalgo.jfr.JfrEvents;
import graalgo.jfr.ParkReason;

/**
 * A single {@code select} operation (P2-M2, WS B; GG-0017 chan slice).
 *
 * <p>Go select semantics:
 * <ol>
 *   <li>Poll every case under each channel's monitor: a send case is
 *       ready if a receiver waits or the buffer has space; a send on a
 *       closed channel is poison, which is itself a ready case —
 *       choosing it panics, exactly as gc chooses uniformly among all
 *       ready cases (poison included). A recv case is ready if a sender
 *       waits, the buffer is non-empty, or the channel is closed.
 *   <li>If any case is ready, complete one chosen uniformly at random
 *       (Go fairness: no case starves under contention) and return its
 *       index; a chosen poison case panics instead of completing.
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
    /** Self-claim sentinel: the selecting goroutine won in its sweep. */
    private static final Object SELF_CLAIM = new Object();

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

        /**
         * Non-mutating check: is the claim held transiently by the
         * selecting goroutine itself (block-phase sweep)? No wakeup is
         * in flight for such a waiter: wakers must preserve it, never
         * drop it — dropping it strands the selector, whose sweep never
         * revisits this channel and which then parks with no future
         * event to wake it (CI run 41943236460 lost wakeup).
         */
        boolean isSelfClaimed() {
            return op.claim.get() == SELF_CLAIM;
        }

        /**
         * Non-mutating check: has a waker on another channel won the
         * claim? Only then is a wakeup in flight and the waiter safe
         * for a waker to drop.
         */
        boolean isClaimedByWaker() {
            return op.claim.get() instanceof SelectCaseWaiter;
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

    /**
     * Tries to claim the SelectOp for the selecting goroutine itself
     * (block-phase sweep). Returns true if the claim was won; the caller
     * must then complete under the monitor or release.
     */
    private boolean tryClaimSelf() {
        return claim.compareAndSet(UNCLAIMED, SELF_CLAIM);
    }

    /** Releases a self-claim after a failed immediate completion. */
    private void releaseSelfClaim() {
        claim.compareAndSet(SELF_CLAIM, UNCLAIMED);
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
                            // Poison is a ready case: gc chooses uniformly
                            // among all ready cases, poison included, and
                            // choosing it panics. Excluding it from the
                            // ready set made the select return another case
                            // 100% of the time where gc panics ~50% (two
                            // ready cases).
                            ready.add(i);
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
            // Poison-before-default still holds: a poison case is in the
            // ready set above, so with no other ready case it is chosen
            // and tryCompleteReady panics — before default is considered
            // (gc panics for `select { case closedCh <- v: default: }`,
            // verified p1/p4).
            if (defaultIndex >= 0) {
                return defaultIndex;
            }

            // ---- Phase 2: block ----
            // Sweep in random order (P2-4: fairness — no systematic
            // low-index preference in the poll→block gap).
            List<SelectCaseWaiter> mine = new ArrayList<>();
            Integer immediate = null;
            boolean claimedByOther = false;
            int[] order = new int[cases.length];
            for (int i = 0; i < order.length; i++) {
                order[i] = i;
            }
            ThreadLocalRandom rng = ThreadLocalRandom.current();
            for (int i = order.length - 1; i > 0; i--) {
                int j = rng.nextInt(i + 1);
                int t = order[i];
                order[i] = order[j];
                order[j] = t;
            }
            for (int oi = 0; oi < order.length && immediate == null && !claimedByOther
                    && !poison; oi++) {
                int i = order[oi];
                ChanOps.SelectCase c = cases[i];
                if (c.chan() == null) {
                    continue;
                }
                GoChan ch = requireGoChan(c.chan());
                // Per-channel retry: a RETRY means another select holds
                // its transient self-claim on this channel's queue.
                // Release our own self-claim and re-attempt the channel
                // (yielding outside the monitor). NEVER wait while
                // holding SELF: a ring of selects each waiting on the
                // next one's self-claim would deadlock. Releasing lets
                // the other select finish (claiming our waiter, or
                // completing elsewhere and cleaning up), after which
                // this channel re-resolves.
                for (;;) {
                    boolean retry = false;
                    synchronized (ch) {
                        // Re-check under the monitor: a case that became
                        // ready between poll and enqueue completes
                        // immediately. (Never enqueue on a poisoned
                        // channel.) A closed-send found here is a
                        // completable candidate like any ready case: it
                        // wins the sweep in random order (the loop exits
                        // on poison and panics below, after removing the
                        // waiters enqueued so far), so a ready case
                        // earlier in the random order wins and a poison
                        // case earlier in the order panics — gc's uniform
                        // choice among ready cases.
                        if (c.send() && ch.isClosed()) {
                            poison = true;
                        } else if (tryClaimSelf()) {
                            // P0-1: claim the SelectOp BEFORE completing.
                            // A concurrent waker on another channel may
                            // have already won the shared claim;
                            // completing without it causes a lost claim
                            // (stale unpark → spurious wakeup, or a
                            // resultIndex overwrite).
                            CompOutcome doneNow = completeUnderLock(ch, i, sched);
                            if (doneNow.retry()) {
                                releaseSelfClaim();
                                retry = true;
                            } else if (doneNow.index() >= 0) {
                                immediate = doneNow.index();
                            } else {
                                // Lost the race under the monitor:
                                // release so a waker can win, then
                                // enqueue normally.
                                releaseSelfClaim();
                                SelectCaseWaiter w = new SelectCaseWaiter(this, i, ch,
                                        c.send(), c.sendValue(), self);
                                ch.enqueueSelectWaiter(w, c.send());
                                mine.add(w);
                            }
                        } else {
                            // A waker won the claim; its unpark is in
                            // flight. Do not complete — park and read
                            // its result.
                            claimedByOther = true;
                        }
                    }
                    if (!retry) {
                        break;
                    }
                    Thread.yield();
                }
            }
            if (poison) {
                // Sweep-phase poison: a send case closed between the poll
                // phase (which saw it open) and this sweep. No case has
                // already won here (poison exits the sweep loop, so
                // immediate is null and no waker claimed us), so the
                // select panics rather than parking on the remaining
                // enqueued cases — parking would hang (or return another
                // case) where gc panics. The enqueued waiters are removed
                // first: a panicking select must not leave stale waiters
                // on other channels.
                cleanup(mine);
                Panic.throwPanic(GoStrings.of("send on closed channel"));
            }
            if (immediate != null) {
                cleanup(mine);
                return immediate;
            }
            if (claimedByOther) {
                // Winner's unpark is in flight: this park returns
                // immediately. (mine holds earlier enqueues; cleaned up.)
                // P2-M6: no ChannelBlock — the select did not block; the
                // result is already decided. The park is still
                // reason-tagged but filtered by duration on resume.
                sched.parkCurrent(null, ParkReason.SELECT);
                try {
                    if (resultPoison) {
                        Panic.throwPanic(GoStrings.of("send on closed channel"));
                    }
                    return resultIndex;
                } finally {
                    cleanup(mine);
                }
            }
            if (mine.isEmpty()) {
                // Zero cases: `select {}` blocks forever (gc deadlock).
                // Park indefinitely; the deadlock detector (P2-M10) owns it.
                // P2-M6: it genuinely blocks — record with channelId -1
                // (multi-channel select has no single channel).
                JfrEvents.channelBlock(sched.currentGoroutine(), -1, ChannelOp.SELECT);
                sched.parkCurrent(null, ParkReason.SELECT);
                throw new IllegalStateException("unreachable: empty select woke");
            }
            // Scheduler-aware park. A waker that claimed us before we park
            // has its UNPARK stashed by the drainer — no wakeup lost.
            // P2-M6: the select genuinely blocks — record it before parking
            // (channelId -1: multi-channel select has no single channel).
            JfrEvents.channelBlock(sched.currentGoroutine(), -1, ChannelOp.SELECT);
            sched.parkCurrent(null, ParkReason.SELECT);
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
     * A chosen closed-send case panics instead of returning null.
     * A RETRY from the channel (another select's transient self-claim
     * on its queue) is retried outside the monitor until it resolves;
     * this thread holds no self-claim here, so the retry cannot
     * deadlock a ring of completing selects.
     */
    private Integer tryCompleteReady(int chosen, SchedulerCore sched) {
        ChanOps.SelectCase c = cases[chosen];
        GoChan ch = requireGoChan(c.chan());
        for (;;) {
            boolean retry = false;
            synchronized (ch) {
                if (c.send()) {
                    if (ch.isClosed()) {
                        // The chosen ready case is poison: choosing it
                        // panics (gc). Returning null here would re-poll
                        // forever — closed is terminal, so the case would
                        // be chosen again on every retry.
                        Panic.throwPanic(GoStrings.of("send on closed channel"));
                    }
                    GoChan.SendAttempt sa = ch.trySend(c.sendValue(), sched);
                    if (sa == GoChan.SendAttempt.SENT) {
                        resultIndex = chosen;
                        resultOk = true;
                        return chosen;
                    }
                    retry = sa == GoChan.SendAttempt.RETRY;
                } else {
                    GoChan.RecvAttempt a = ch.tryRecv(sched);
                    if (a.retry()) {
                        retry = true;
                    } else if (a.result() != null) {
                        resultIndex = chosen;
                        resultValue = a.result().value();
                        resultOk = a.result().ok();
                        return chosen;
                    }
                }
            }
            if (!retry) {
                return null;
            }
            Thread.yield();
        }
    }

    /** Outcome of {@link #completeUnderLock}: a won case index, a block, or a retry. */
    private record CompOutcome(int index, boolean retry) {}

    private static final CompOutcome COMP_BLOCK = new CompOutcome(-1, false);
    private static final CompOutcome COMP_RETRY = new CompOutcome(-1, true);

    /**
     * Caller holds ch's monitor and the self-claim. Completes the case
     * if still possible (won index); COMP_BLOCK if the race was lost;
     * COMP_RETRY if another select's transient self-claim blocks this
     * channel's queue. A closed-send case panics (it never reaches here
     * from the sweep, which takes the poison branch first). COMP_RETRY
     * must NOT be waited on while holding the self-claim (see the
     * sweep's retry loop): the caller releases the claim, yields, and
     * re-attempts.
     */
    private CompOutcome completeUnderLock(GoChan ch, int chosen, SchedulerCore sched) {
        ChanOps.SelectCase c = cases[chosen];
        if (c.send()) {
            if (ch.isClosed()) {
                // Defense in depth: the sweep never calls this for a
                // closed-send case (it takes the poison branch first),
                // but completing one here must panic, never COMP_BLOCK:
                // trySend on a closed channel would deliver/buffer the
                // value (it does not check closed), and COMP_BLOCK would
                // silently drop the poison. gc panics.
                Panic.throwPanic(GoStrings.of("send on closed channel"));
            }
            GoChan.SendAttempt sa = ch.trySend(c.sendValue(), sched);
            if (sa == GoChan.SendAttempt.SENT) {
                resultIndex = chosen;
                resultOk = true;
                return new CompOutcome(chosen, false);
            }
            return sa == GoChan.SendAttempt.RETRY ? COMP_RETRY : COMP_BLOCK;
        }
        GoChan.RecvAttempt a = ch.tryRecv(sched);
        if (a.retry()) {
            return COMP_RETRY;
        }
        if (a.result() == null) {
            return COMP_BLOCK;
        }
        resultIndex = chosen;
        resultValue = a.result().value();
        resultOk = a.result().ok();
        return new CompOutcome(chosen, false);
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
