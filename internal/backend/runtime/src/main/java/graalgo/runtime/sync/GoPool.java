package graalgo.runtime.sync;

import java.util.ArrayDeque;
import java.util.Deque;

/**
 * Scheduler-agnostic object pool ({@code sync.Pool} semantics) for the
 * Phase-2 runtime.
 *
 * <p>A {@code sync.Pool} Go value <em>is</em> the {@link GoPool} Java
 * object (opaque, like {@code GoMutex} — ADR-0002 D1). Copying copies the
 * reference.
 *
 * <p>Go semantics: {@code Get} returns an arbitrary previously
 * {@code Put} item, or nil if the pool is empty (the {@code New} field is
 * not in M2 scope, so the {@code New}-fallback never triggers — see the
 * frontend's {@code syntheticSyncPackage}). {@code Put(nil)} is a no-op
 * (a nil item would be indistinguishable from empty on {@code Get}).
 *
 * <p>gc's victim-collection and per-P sharding are performance details,
 * not semantics: a single deque is observationally equivalent for
 * single-threaded and racy programs alike (the race detector aside,
 * which graalgo does not implement).
 *
 * <p>Array-copy rule: each slot of a copied {@code [N]sync.Pool} gets a
 * fresh empty pool (cf. {@code GoStruct.deepCopyArray}) — sharing one
 * pool across copies would let {@code Put} through one copy be observed
 * through another, which gc's value copy does not do.
 */
public final class GoPool {
    private final Deque<Object> items = new ArrayDeque<>();

    /** Removes and returns an arbitrary pooled item, or null if empty. */
    public synchronized Object get() {
        return items.pollFirst();
    }

    /** Adds x to the pool; a null x is a no-op. */
    public synchronized void put(Object x) {
        if (x != null) {
            items.offerFirst(x);
        }
    }
}
