package graalgo.runtime.sync;

import java.util.ArrayDeque;
import java.util.Deque;

import graalgo.runtime.GoCalls;
import graalgo.values.GoFunc;

/**
 * Scheduler-agnostic object pool ({@code sync.Pool} semantics) for the
 * Phase-2 runtime.
 *
 * <p>A {@code sync.Pool} Go value <em>is</em> the {@link GoPool} Java
 * object (opaque, like {@code GoMutex} — ADR-0002 D1). Copying copies the
 * reference.
 *
 * <p>Go semantics: {@code Get} returns an arbitrary previously
 * {@code Put} item; if the pool is empty it falls back to the
 * {@code New} field (when set, via {@code sync.Pool{New: f}} or
 * {@code p.New = f}), and then to nil. {@code Put(nil)} is a no-op
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
    // P3-driver-harden (jsonlib lane): the New func() any field, set via
    // sync.Pool{New: f} literals (newFn child) or p.New = f (setnew op).
    private GoFunc newFunc;

    /**
     * Pops an item, or calls {@code New}, or returns {@code null}.
     */
    public Object get() {
        Object x;
        GoFunc nf;
        synchronized (this) {
            x = items.pollFirst();
            nf = newFunc;
        }
        if (x != null) {
            return x;
        }
        if (nf == null) {
            return null;
        }
        return GoCalls.invoke(nf, new Object[0]);
    }

    /**
     * Returns an item to the pool; {@code null} is dropped (gc parity).
     */
    public void put(Object x) {
        if (x == null) {
            return;
        }
        synchronized (this) {
            items.addLast(x);
        }
    }

    /**
     * Reads the {@code New} field (may be {@code null}).
     */
    public synchronized Object getNew() {
        return newFunc;
    }

    /**
     * Writes the {@code New} field.
     */
    public synchronized void setNew(Object f) {
        newFunc = (GoFunc) f;
    }
}
