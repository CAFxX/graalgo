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
    }

    public final Kind kind;
    public final GoroutineHandle handle;
    public final long generation;

    public ResumeToken(Kind kind, GoroutineHandle handle, long generation) {
        if (kind == null || handle == null) {
            throw new IllegalArgumentException("token kind and handle must not be null");
        }
        this.kind = kind;
        this.handle = handle;
        this.generation = generation;
    }

    @Override
    public String toString() {
        return "ResumeToken(" + kind + ", g" + handle.id + ", gen=" + generation + ")";
    }
}
