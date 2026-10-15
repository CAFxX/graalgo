package graalgo.runtime.sync;

import graalgo.sched.GoroutineHandle;
import graalgo.sched.SchedulerCore;

import java.util.ArrayDeque;
import java.util.Deque;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Ticket-based notification list for {@code sync.Cond} (graalgo).
 *
 * <p>Go's {@code sync.Cond} (go1.27.1, sync/cond.go) is real Go code that
 * calls the {@code runtime_notifyList*} intrinsics (declared bodyless in
 * sync/runtime.go, implemented in runtime/sema.go). This class implements
 * those intrinsics with gc's exact semantics:
 *
 * <ul>
 *   <li>{@code notifyListAdd(l *notifyList) uint32}: atomically issues the
 *       next ticket ({@code wait++}), returning the ticket.
 *   <li>{@code notifyListWait(l *notifyList, t uint32)}: returns immediately
 *       if ticket {@code t} was already notified ({@code less(t, notify)});
 *       otherwise parks until {@code notify} passes {@code t}.
 *   <li>{@code notifyListNotifyOne(l *notifyList)}: notifies the waiter
 *       holding the {@code notify} ticket (incrementing {@code notify});
 *       a waiter that has taken a ticket but not yet parked observes the
 *       new {@code notify} and never parks (no lost wakeup).
 *   <li>{@code notifyListNotifyAll(l *notifyList)}: sets
 *       {@code notify = wait} and wakes every parked waiter.
 * </ul>
 *
 * <p>State layout: the Go {@code notifyList} struct's {@code wait}/
 * {@code notify} fields are never touched by Go code directly — cond.go
 * only reaches them through these intrinsics — so the ticket counters
 * and the waiter queue live here, keyed by the struct's address (the
 * {@code *notifyList} arrives as a D5-boxed long, like
 * {@link AddressIntCell}'s {@code *uint32}). The Go-side struct memory
 * is unused.
 *
 * <p>Ticket comparison uses gc's wraparound-safe {@code less}:
 * {@code int32(a-b) < 0}.
 *
 * <p>Blocking is scheduler-aware, following {@link GoSemaphore}: waiters
 * park via {@link SchedulerCore#parkCurrent} and are re-admitted with
 * {@link SchedulerCore#unpark}. The check-then-enqueue runs under a
 * single lock, so a notify that lands between ticket issue and enqueue
 * is observed (the waiter never parks on an already-notified ticket).
 */
public final class GoNotifyList {
    private GoNotifyList() {}

    /** One parked waiter: the goroutine handle plus its ticket. */
    private static final class Waiter {
        final GoroutineHandle handle;
        final int ticket;

        Waiter(GoroutineHandle handle, int ticket) {
            this.handle = handle;
            this.ticket = ticket;
        }
    }

    /** Per-notifyList state. Guarded by {@link #LOCK}. */
    private static final class State {
        /** Next ticket to issue (gc's {@code wait}, atomically incremented). */
        int wait;
        /** Next ticket to notify (gc's {@code notify}). */
        int notify;
        /** Parked waiters in enqueue order. */
        final Deque<Waiter> queue = new ArrayDeque<>();
    }

    /** Per-notifyList state, keyed by the Go {@code *notifyList} address. */
    private static final Map<Long, State> STATES = new ConcurrentHashMap<>();

    /** Global lock for atomic ticket + waiter-queue updates. */
    private static final Object LOCK = new Object();

    /** gc's wraparound-safe ticket comparison: {@code int32(a-b) < 0}. */
    private static boolean less(int a, int b) {
        return (a - b) < 0;
    }

    private static State stateFor(long addr) {
        State s = STATES.get(addr);
        if (s == null) {
            throw new IllegalArgumentException(
                    "GoNotifyList: no state for notifyList address " + addr);
        }
        return s;
    }

    /**
     * Ensures state exists for the notifyList at {@code addr}, creating it
     * on first use. Separate from {@link #stateFor} so misuse (operating on
     * a never-added list) still fails fast.
     */
    private static State ensureState(long addr) {
        return STATES.computeIfAbsent(addr, k -> new State());
    }

    /**
     * {@code sync.runtime_notifyListAdd(l *notifyList) uint32}.
     *
     * @param addr the Go {@code *notifyList} address (D5 long)
     * @return the issued ticket, as an int (D5 uint32)
     */
    public static int add(long addr) {
        if (addr == 0) {
            throw new IllegalArgumentException("GoNotifyList.add: nil address");
        }
        synchronized (LOCK) {
            State s = ensureState(addr);
            int t = s.wait;
            s.wait = t + 1;
            return t;
        }
    }

    /**
     * {@code sync.runtime_notifyListWait(l *notifyList, t uint32)}.
     *
     * @param addr the Go {@code *notifyList} address (D5 long)
     * @param ticket the ticket issued by {@link #add} (D5 uint32 as int)
     */
    public static void waitForNotify(long addr, int ticket) {
        if (addr == 0) {
            throw new IllegalArgumentException("GoNotifyList.wait: nil address");
        }
        SchedulerCore sched = SchedulerCore.current();
        GoroutineHandle h;
        synchronized (LOCK) {
            State s = stateFor(addr);
            // Already notified (notify ran after our Add but before we
            // enqueued): never park.
            if (less(ticket, s.notify)) {
                return;
            }
            h = sched.currentGoroutine();
            s.queue.addLast(new Waiter(h, ticket));
            // Re-check under the same lock: a notify that incremented
            // s.notify after our first check but before enqueue is now
            // visible; dequeue instead of parking on a dead ticket.
            // (notify only changes under LOCK, so this is the complete
            // check — no ABA is possible here.)
            if (less(ticket, s.notify)) {
                s.queue.removeLastOccurrence(h);
                return;
            }
        }
        // Park until notifyOne/notifyAll unparks us. The drainer matches
        // UNPARK by handle even if it arrives before we park (stashed) —
        // no lost wakeup (GoSemaphore precedent).
        sched.parkCurrent(null);
    }

    /**
     * {@code sync.runtime_notifyListNotifyOne(l *notifyList)}: wakes the
     * waiter holding the current {@code notify} ticket, if it has parked;
     * a ticket holder that has not yet parked observes the incremented
     * {@code notify} and skips parking.
     *
     * @param addr the Go {@code *notifyList} address (D5 long)
     */
    public static void notifyOne(long addr) {
        if (addr == 0) {
            throw new IllegalArgumentException("GoNotifyList.notifyOne: nil address");
        }
        SchedulerCore sched = SchedulerCore.current();
        GoroutineHandle next = null;
        synchronized (LOCK) {
            State s = stateFor(addr);
            // Fast path (gc): no new waiters since the last notification.
            if (s.wait == s.notify) {
                return;
            }
            int t = s.notify;
            s.notify = t + 1;
            // Find the waiter holding ticket t. It may not have parked
            // yet (lost the race to us); then it observes the new notify
            // in waitForNotify and never parks — no wakeup needed.
            for (Waiter w : s.queue) {
                if (w.ticket == t) {
                    s.queue.remove(w);
                    next = w.handle;
                    break;
                }
            }
        }
        if (next != null) {
            sched.unpark(next);
        }
    }

    /**
     * {@code sync.runtime_notifyListNotifyAll(l *notifyList)}: wakes every
     * parked waiter.
     *
     * @param addr the Go {@code *notifyList} address (D5 long)
     */
    public static void notifyAll(long addr) {
        if (addr == 0) {
            throw new IllegalArgumentException("GoNotifyList.notifyAll: nil address");
        }
        SchedulerCore sched = SchedulerCore.current();
        Deque<GoroutineHandle> toWake;
        synchronized (LOCK) {
            State s = stateFor(addr);
            // Fast path (gc): no new waiters since the last notification.
            if (s.wait == s.notify) {
                return;
            }
            // gc sets notify = wait: every ticket issued so far is notified.
            // Waiters that took a ticket but have not parked yet observe
            // this and skip parking.
            s.notify = s.wait;
            toWake = new ArrayDeque<>(s.queue.size());
            for (Waiter w : s.queue) {
                toWake.addLast(w.handle);
            }
            s.queue.clear();
        }
        for (GoroutineHandle h : toWake) {
            sched.unpark(h);
        }
    }

    /**
     * {@code sync.runtime_notifyListCheck(size uintptr)}: verifies the Go
     * {@code notifyList} struct layout both sides agree on.
     *
     * <p>gc fatals when {@code Sizeof(sync.notifyList) !=
     * Sizeof(runtime.notifyList)} — the two structs must stay in sync.
     * Here the ticket state lives in Java (keyed by address), so there is
     * no runtime-side layout to drift; the check validates that the
     * frontend's {@code unsafe.Sizeof} produced gc's layout:
     * {@code wait(4) + notify(4) + lock(uintptr=8) + head(8) + tail(8) = 32}.
     * A different size means the frontend's struct layout diverged — fail
     * fast, never silently.
     *
     * @param size the D5-boxed uintptr size
     */
    public static void check(int size) {
        if (size != 32) {
            throw new IllegalStateException(
                    "GoNotifyList.check: unexpected notifyList size " + size
                            + " (want 32: wait(4)+notify(4)+lock(8)+head(8)+tail(8))");
        }
    }

    /** Clears all notifyList state (program reinstall isolation). */
    public static void clear() {
        synchronized (LOCK) {
            STATES.clear();
        }
    }
}
