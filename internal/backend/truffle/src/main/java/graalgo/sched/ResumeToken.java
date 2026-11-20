package graalgo.sched;

/**
 * A scheduler token: a parked goroutine's request for (re-)admission,
 * processed by the drainer (P2-D2, P2-D3).
 *
 * <p>Lanes (GG-WAKE-0003): {@link Kind#WAKE} (woken/channel-ready — Go's
 * runnext analog) first, then {@link Kind#INIT} (new goroutines — FIFO
 * alone starved 2,643/10,000 of them behind RESUME flood in spike 2b
 * §3.2), then {@link Kind#RESUME} (quantum-expired spinners).
 * {@link Kind#EXIT} is not a lane: it only decrements the in-flight
 * count.
 */
public final class ResumeToken {
    public enum Kind {
        /** New goroutine awaiting first admission. */
        INIT,
        /** Quantum-expired goroutine awaiting re-admission. */
        RESUME,
        /** Woken goroutine (channel/timer/sleep — P2-M2/M3). */
        WAKE,
        /** Goroutine finished; releases its in-flight slot. */
        EXIT,
        /**
         * Goroutine parked itself indefinitely (blocking op). The
         * drainer records it and releases its slot; re-admission needs
         * an UNPARK token for the same handle.
         */
        PARKED,
        /**
         * Wake request for a PARKED goroutine. The drainer matches it
         * against the parked record by handle, tolerating both orders
         * (unpark-before-parked via a pending flag, parked-before-unpark
         * via the parked record) — no wakeup is lost.
         */
        UNPARK,
        /**
         * Goroutine entering blocking I/O (P2-M3, GG-0063/GG-SCHED-0006).
         * The drainer releases its in-flight slot (it no longer counts
         * against GOMAXPROCS while blocked, gc netpoller parity) and
         * records the handle in the in-I/O set. The vthread then blocks
         * in the real syscall (Loom unmounts the carrier). Re-admission
         * needs an IO_EXIT token for the same handle.
         */
        IO_ENTER,
        /**
         * Goroutine leaving blocking I/O. The drainer removes it from
         * the in-I/O set (fail-fast when absent: unpaired ioExit) and
         * re-admits through the WAKE lane; the vthread waits for
         * re-admission (gc exitsyscall parity).
         */
        IO_EXIT,
        /**
         * Direct handoff (WP-SCHED, gc's runnext): {@link #handle} parks
         * (generation {@link #generation}, like PARKED) and gives its
         * in-flight slot to {@link #target}, whose pending park it wakes
         * itself (the target's {@code handoff} flag, then
         * {@code LockSupport.unpark}) right after offering this token. The
         * drainer keeps the slot count unchanged and books the target as
         * admitted: removed from the parked record, or — when the target's
         * PARKED token is still to come — remembered so that PARKED only
         * releases the target's old slot.
         */
        HANDOFF,
        /**
         * No goroutine: re-evaluate scheduler-global conditions (a
         * GOMAXPROCS change may allow admissions; the last external waker
         * — a timer — retired, so deadlock must be re-checked). Carries
         * no handle and never touches the lanes.
         */
        POKE,
    }

    /** The handle-less POKE token (shared: it carries no state). */
    private static final ResumeToken POKE_TOKEN = new ResumeToken();

    private ResumeToken() {
        this.kind = Kind.POKE;
        this.handle = null;
        this.generation = -1;
        this.target = null;
    }

    /** A {@link Kind#POKE} token. */
    public static ResumeToken poke() {
        return POKE_TOKEN;
    }

    public final Kind kind;
    public final GoroutineHandle handle;
    public final long generation;
    /** {@link Kind#HANDOFF} only: the goroutine receiving the slot (else null). */
    public final GoroutineHandle target;

    public ResumeToken(Kind kind, GoroutineHandle handle, long generation) {
        if (kind == null || handle == null || kind == Kind.HANDOFF) {
            throw new IllegalArgumentException(
                    "token kind and handle must not be null (HANDOFF tokens: handoff())");
        }
        this.kind = kind;
        this.handle = handle;
        this.generation = generation;
        this.target = null;
    }

    private ResumeToken(GoroutineHandle parker, long generation, GoroutineHandle target) {
        this.kind = Kind.HANDOFF;
        this.handle = parker;
        this.generation = generation;
        this.target = target;
    }

    /**
     * A {@link Kind#HANDOFF} token: {@code parker} parks at {@code generation}
     * and {@code target} receives its in-flight slot.
     */
    public static ResumeToken handoff(GoroutineHandle parker, long generation, GoroutineHandle target) {
        if (parker == null || target == null || parker == target) {
            throw new IllegalArgumentException("handoff needs two distinct handles");
        }
        return new ResumeToken(parker, generation, target);
    }

    @Override
    public String toString() {
        return "ResumeToken(" + kind + ", g" + (handle == null ? "-" : handle.id) + ", gen=" + generation
                + (target == null ? "" : ", to g" + target.id) + ")";
    }
}
