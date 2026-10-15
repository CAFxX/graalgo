package graalgo.runtime.sync;

import graalgo.runtime.GoMaxProcs;

/**
 * {@code sync.runtime_procPin}/{@code sync.runtime_procUnpin} (graalgo).
 *
 * <p>Go's {@code sync.Pool} (go1.27.1, sync/pool.go) pins the calling
 * goroutine to its P around the pool-local shard access and uses the P's
 * id to index the per-P {@code local} array (sized
 * {@code GOMAXPROCS(0)}). gc's {@code procPin} disables preemption and
 * returns the P id; {@code procUnpin} re-enables it.
 *
 * <p>Mapping to the JVM: goroutines run on virtual threads
 * (ADR-0002 D1 — there is no P to pin to). The pin contract the pool
 * needs is narrower than gc's:
 *
 * <ul>
 *   <li>the returned pid is in {@code [0, GOMAXPROCS)} — required by
 *       {@code pinSlow}'s {@code &local[pid]} indexing;
 *   <li>the pid is stable for the calling thread across the pin/unpin
 *       window — the shard is only a locality hint (the pool is
 *       explicitly lossy), so sharing a shard across threads is
 *       semantically fine.
 * </ul>
 *
 * <p>Both hold with {@code pid = threadId % GOMAXPROCS}: a virtual
 * thread's id is stable, and the modulus keeps it in range. gc's
 * preemption disable exists so that no STW GC (which runs
 * {@code poolCleanup}, mutating {@code p.local}) can interleave the
 * pinned window; here pool cleanup runs on JVM GC notifications which
 * only reassign the {@code p.local} slice header — a pinned mutator
 * already holds its shard pointer and keeps using the old array,
 * which stays valid. No lock or preemption control is needed in the
 * pinned window itself (it never blocks).
 */
public final class GoProcPin {
    private GoProcPin() {}

    /**
     * {@code sync.runtime_procPin() int}: pins the calling goroutine's
     * thread and returns its shard id.
     *
     * @return pid in {@code [0, GOMAXPROCS)}
     */
    public static int pin() {
        int maxProcs = GoMaxProcs.current();
        if (maxProcs <= 0) {
            maxProcs = 1;
        }
        long tid = Thread.currentThread().threadId();
        return (int) Long.remainderUnsigned(tid, maxProcs);
    }

    /**
     * {@code sync.runtime_procUnpin()}: releases the pin. No-op — there
     * is no preemption state to restore on the JVM; see the class
     * javadoc for why the pin needs no release action.
     */
    public static void unpin() {
        // Intentionally empty.
    }
}
