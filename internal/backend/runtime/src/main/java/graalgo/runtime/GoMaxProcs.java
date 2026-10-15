package graalgo.runtime;

import java.util.concurrent.atomic.AtomicInteger;

/**
 * Process-wide GOMAXPROCS value (graalgo).
 *
 * <p>Holds the value manipulated by {@code runtime.GOMAXPROCS(n)} so both
 * the Truffle node ({@code graalgo.p3builtin.nodes.GoMaxProcsNode} in the
 * interop module) and runtime-module consumers (e.g.
 * {@code graalgo.runtime.sync.GoProcPin}, which sizes pool shards with
 * the same value {@code sync.Pool.pinSlow} uses) observe a single source
 * of truth. Lives in the runtime module because interop builds against
 * runtime, not the reverse.
 */
public final class GoMaxProcs {
    private GoMaxProcs() {}

    /** The current GOMAXPROCS value; initialized to the host CPU count. */
    private static final AtomicInteger CURRENT =
            new AtomicInteger(java.lang.Runtime.getRuntime().availableProcessors());

    /** Returns the current GOMAXPROCS value. */
    public static int current() {
        return CURRENT.get();
    }

    /**
     * Sets GOMAXPROCS, returning the previous value (gc's
     * {@code runtime.GOMAXPROCS} contract).
     */
    public static int set(int want) {
        return CURRENT.getAndSet(want);
    }
}
