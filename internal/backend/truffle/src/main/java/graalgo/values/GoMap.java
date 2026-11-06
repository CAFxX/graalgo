package graalgo.values;

/**
 * A Go map value: open-addressing hash table with linear probing (D5).
 *
 * <p>Keys use {@link GoEquality} (Go {@code ==} semantics — a NaN key never
 * matches, matching Go). {@link #get} returns {@code null} for absent keys;
 * map-access nodes substitute the element type's zero value (Go's
 * comma-ok form arrives with WS D). Tombstones keep probe chains intact
 * across deletes; the table doubles when occupancy (including tombstones)
 * exceeds 70%.
 *
 * <p>Concurrency (review 2026-11-05, B-P1-4): concurrent use of one map
 * from two goroutines is a data race (undefined behavior per the Go memory
 * model) and gc fails it loudly instead of corrupting silently. This class
 * does the same — every mutating operation holds the write guard, and a
 * second concurrent writer, a read that observes a write in progress, or
 * a write that observes an iteration in progress raises
 * {@link graalgo.runtime.GoFatalError} with gc's exact message
 * ({@code concurrent map writes},
 * {@code concurrent map read and map write},
 * {@code concurrent map iteration and map write}). The fatal is an
 * {@code Error}, never recoverable by Go code, and the process dies with
 * exit 2 (see {@link graalgo.runtime.GoFatal}).
 */
public final class GoMap {
    private static final byte EMPTY = 0;
    private static final byte OCCUPIED = 1;
    private static final byte TOMBSTONE = 2;
    private static final double MAX_LOAD = 0.7;

    private Object[] keys;
    private Object[] values;
    private byte[] states;
    private int mask;
    private int size;
    private int tombstones;

    /**
     * Write guard: held across every mutating operation. A second
     * concurrent writer CAS-fails and raises
     * {@code GoFatalError("concurrent map writes")} (gc's exact message).
     *
     * <p>CAS rather than gc's check-then-set is a deliberate, documented
     * strengthening (see {@code DIV-RV1105-1}): gc's check-then-set has a
     * hole where two racing writers both slip through and silently corrupt
     * — the exact failure this guard exists to eliminate. One CAS per write
     * is noise next to hashing + probing, and no allocation or lock is added
     * to any path.
     *
     * <p>Detection is best-effort like gc: a read that <em>starts</em>
     * before the writer's CAS cannot be detected — inherent, identical in
     * kind to gc's entry check (gc relies on {@code -race} for the rest).
     * Programs that share state through {@code sync.Mutex}/
     * {@code sync.RWMutex}, channels, {@code sync.WaitGroup},
     * {@code sync/atomic}, {@code sync.Once}, or goroutine start observe
     * full visibility via the JVM happens-before edges of those primitives;
     * happens-before-ordered access can never observe the flag set (volatile
     * semantics of the {@code AtomicBoolean}), so no correct program trips
     * the guard.
     */
    private final java.util.concurrent.atomic.AtomicBoolean writing =
            new java.util.concurrent.atomic.AtomicBoolean();

    /**
     * Iteration count: held across {@link #keysSnapshot}. A writer that
     * observes it nonzero raises
     * {@code GoFatalError("concurrent map iteration and map write")}.
     *
     * <p>This is a strengthening beyond gc's entry check (see
     * {@code DIV-RV1105-2}): without it, a write starting mid-snapshot
     * would grow {@code size} past the snapshot array ({@code
     * ArrayIndexOutOfBoundsException} surfacing as an "internal error") or
     * tear the snapshot. The snapshot holds the count only for the array
     * build itself — range-over-map iterates the detached snapshot, so a
     * legal single-goroutine write from the loop body (which completes
     * before the next per-key {@link #get}) never trips it.
     *
     * <p>Ordering protocol (the count only closes the window if taken
     * before the check): {@link #keysSnapshot} increments the count
     * <em>first</em> and then checks the write guard
     * ({@link #beginIterationChecked}), pairing with {@link #beginWrite}'s
     * CAS-then-check-count. A check-then-increment order would leave a
     * hole where a writer CASes after the reader's check but before its
     * increment and both proceed over the mutating table.
     */
    private final java.util.concurrent.atomic.AtomicInteger iterating =
            new java.util.concurrent.atomic.AtomicInteger();

    /**
     * Acquires the write guard. Throws {@code GoFatalError} on a second
     * concurrent writer, or on a write racing an in-progress iteration.
     * Package-private: the deterministic wiring tests hold the guard
     * directly.
     */
    void beginWrite() {
        if (!writing.compareAndSet(false, true)) {
            throw new graalgo.runtime.GoFatalError("concurrent map writes");
        }
        if (iterating.get() > 0) {
            // Release before throwing: the flag must never stay set after
            // a failed acquisition, or every later op would self-fatal.
            writing.set(false);
            throw new graalgo.runtime.GoFatalError("concurrent map iteration and map write");
        }
    }

    /** Releases the write guard. Package-private for tests. */
    void endWrite() {
        writing.set(false);
    }

    /**
     * Throws {@code GoFatalError} if a write is in progress.
     * Package-private for tests.
     */
    void checkRead() {
        if (writing.get()) {
            throw new graalgo.runtime.GoFatalError("concurrent map read and map write");
        }
    }

    /**
     * Iteration guard with gc's exact iteration message.
     * Package-private for tests.
     */
    void checkIteration() {
        if (writing.get()) {
            throw new graalgo.runtime.GoFatalError("concurrent map iteration and map write");
        }
    }

    /**
     * Marks an iteration in progress. Held across {@link #keysSnapshot};
     * a writer starting in that window fatals via {@link #beginWrite}.
     * Package-private for tests.
     */
    void beginIteration() {
        iterating.incrementAndGet();
    }

    /**
     * Increments the iteration count and then checks the write guard,
     * throwing {@code GoFatalError("concurrent map iteration and map
     * write")} if a write is in progress. The increment-before-check
     * order is load-bearing (see the {@code iterating} field doc): a
     * writer that CASes after this increment observes the count and
     * fatals in {@link #beginWrite}, and a writer already in progress is
     * observed here — so no write can be in flight undetected while the
     * snapshot builds. On failure the count is released before throwing,
     * so a failed snapshot never strands the count nonzero.
     * Package-private for tests.
     */
    void beginIterationChecked() {
        iterating.incrementAndGet();
        if (writing.get()) {
            iterating.decrementAndGet();
            throw new graalgo.runtime.GoFatalError("concurrent map iteration and map write");
        }
    }

    /** Marks an iteration finished. Package-private for tests. */
    void endIteration() {
        iterating.decrementAndGet();
    }

    public GoMap() {
        this(8);
    }

    public GoMap(int hint) {
        int n = 8;
        while (n < Math.max(8, hint * 2)) {
            n *= 2;
        }
        keys = new Object[n];
        values = new Object[n];
        states = new byte[n];
        mask = n - 1;
    }

    /**
     * Number of entries. Deliberately unguarded: gc's {@code len} performs
     * no concurrent-access check either, and a plain int read can corrupt
     * nothing (a stale value during a concurrent write is the same
     * race gc leaves to {@code -race}).
     */
    public int size() {
        return size;
    }

    /** Returns the value for {@code key}, or {@code null} if absent. */
    public Object get(Object key) {
        checkRead();
        int i = findSlot(key);
        return i >= 0 ? values[i] : null;
    }

    /** Inserts or replaces {@code key → value}. */
    public void put(Object key, Object value) {
        if (key == null) {
            throw new IllegalArgumentException("Go map keys are never null in Phase 1 (WS D refines)");
        }
        beginWrite();
        try {
            putInternal(key, value);
        } finally {
            endWrite();
        }
    }

    /**
     * Guarded put body. Called only with the write guard held; the
     * {@code resize}→retry path recurses here (not through {@link #put})
     * so reentrancy never self-fatals.
     */
    private void putInternal(Object key, Object value) {
        if ((size + tombstones + 1) > (int) (keys.length * MAX_LOAD)) {
            resize(keys.length * 2);
        }
        int h = GoEquality.hash(key) & mask;
        int firstTombstone = -1;
        for (int probe = 0; probe < keys.length; probe++) {
            int i = (h + probe) & mask;
            byte s = states[i];
            if (s == EMPTY) {
                int at = firstTombstone >= 0 ? firstTombstone : i;
                if (firstTombstone >= 0) {
                    tombstones--;
                }
                keys[at] = key;
                values[at] = value;
                states[at] = OCCUPIED;
                size++;
                return;
            }
            if (s == TOMBSTONE) {
                if (firstTombstone < 0) {
                    firstTombstone = i;
                }
            } else if (GoEquality.equal(keys[i], key)) {
                values[i] = value;
                return;
            }
        }
        // Table full of tombstones (should not happen given the load check):
        // resize and retry.
        resize(keys.length * 2);
        putInternal(key, value);
    }

    /** Removes {@code key}; no-op if absent. */
    public void delete(Object key) {
        beginWrite();
        try {
            int i = findSlot(key);
            if (i >= 0) {
                keys[i] = null;
                values[i] = null;
                states[i] = TOMBSTONE;
                size--;
                tombstones++;
            }
        } finally {
            endWrite();
        }
    }

    /** Removes all entries (the {@code clear} builtin). */
    public void clear() {
        beginWrite();
        try {
            java.util.Arrays.fill(keys, null);
            java.util.Arrays.fill(values, null);
            java.util.Arrays.fill(states, EMPTY);
            size = 0;
            tombstones = 0;
        } finally {
            endWrite();
        }
    }

    /** True when {@code key} is present (even with a null value). */
    public boolean containsKey(Object key) {
        checkRead();
        return findSlot(key) >= 0;
    }

    /**
     * Snapshot of the present keys, in table order. Used by range-over-map:
     * entries deleted mid-iteration are skipped by re-checking
     * {@link #containsKey} at production time (Go spec: a not-yet-reached
     * entry removed during iteration is not produced); entries created
     * mid-iteration are never produced (the spec permits skipping them).
     *
     * <p>Iteration is a guarded read (gc parity — gc's {@code mapiterinit}
     * fatals on concurrent map iteration and map write): throws
     * {@code GoFatalError("concurrent map iteration and map write")} if a
     * write is in progress, and holds the iteration count across the array
     * build so a write starting mid-snapshot fatals instead of growing
     * {@code size} past the snapshot array or tearing it. The count is
     * taken before the write-guard check ({@link #beginIterationChecked}),
     * so the check-then-increment window — a writer CASing between the
     * two — cannot occur.
     */
    public Object[] keysSnapshot() {
        beginIterationChecked();
        try {
            Object[] out = new Object[size];
            int j = 0;
            for (int i = 0; i < keys.length; i++) {
                if (states[i] == OCCUPIED) {
                    out[j++] = keys[i];
                }
            }
            return out;
        } finally {
            endIteration();
        }
    }

    /** Slot index holding {@code key}, or -1. */
    private int findSlot(Object key) {
        int h = GoEquality.hash(key) & mask;
        for (int probe = 0; probe < keys.length; probe++) {
            int i = (h + probe) & mask;
            byte s = states[i];
            if (s == EMPTY) {
                return -1;
            }
            if (s == OCCUPIED && GoEquality.equal(keys[i], key)) {
                return i;
            }
        }
        return -1;
    }

    /**
     * Grows the table and rehashes. Called only with the write guard held
     * (from {@link #putInternal}); never through {@link #put}, which would
     * self-fatal on reentrancy.
     */
    private void resize(int n) {
        Object[] oldKeys = keys;
        Object[] oldValues = values;
        byte[] oldStates = states;
        keys = new Object[n];
        values = new Object[n];
        states = new byte[n];
        mask = n - 1;
        size = 0;
        tombstones = 0;
        for (int i = 0; i < oldKeys.length; i++) {
            if (oldStates[i] == OCCUPIED) {
                putInternal(oldKeys[i], oldValues[i]);
            }
        }
    }
}
