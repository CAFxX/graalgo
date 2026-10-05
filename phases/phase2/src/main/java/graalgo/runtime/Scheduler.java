package graalgo.runtime;

import graalgo.values.GoFunc;

/**
 * Scheduler-glue intrinsic surface (WS G, GG-0020).
 *
 * <p><strong>Phase-2 implementation</strong> (P2-D8): this class
 * <em>replaces</em> the Phase-1 stub of the same fully-qualified name
 * (whose methods threw {@code UnsupportedOperationException}). The
 * Phase-2 classpath puts {@code phase2/out/classes} first, so every
 * call compiled against the frozen signatures resolves here.
 * Signatures are byte-identical to the frozen ones (bodies are INTERNAL
 * per freeze §3.5); {@code FreezeSignatureTest} keeps passing.
 *
 * <p>If the Phase-1 stub is loaded instead (phase2 classes not first on
 * the classpath), every method throws the stub's loud
 * {@code UnsupportedOperationException} — a fail-fast, never silent
 * misbehavior.
 */
public final class Scheduler {
    private Scheduler() {}

    /**
     * Phase-2-only diagnostic: true when THIS class (the Phase-2
     * replacement) is loaded, absent on the Phase-1 stub. The launcher
     * calls it at startup to fail fast if the classpath order is wrong
     * (phase2 classes must precede phase1 classes). NOT part of the
     * frozen contract — intentionally not covered by
     * FreezeSignatureTest.
     */
    public static boolean isPhase2() {
        return true;
    }

    /**
     * Create a new goroutine running {@code fn} (no-arg functions in
     * P2-M1; argument binding arrives with the call protocol in P2-M2+).
     */
    public static void spawn(GoFunc fn) {
        graalgo.sched.SchedulerCore.current().spawnGoFunc(fn);
    }

    /** Voluntarily yield the processor (quantum park). */
    public static void yield() {
        graalgo.sched.SchedulerCore.current().yieldCurrent();
    }

    /**
     * Park the current goroutine indefinitely; {@code wake} runs on the
     * waker thread before re-admission. Wake via
     * {@code SchedulerCore.unpark} (channels in P2-M2).
     */
    public static void park(Runnable wake) {
        graalgo.sched.SchedulerCore.current().parkCurrent(wake);
    }

    /** Number of live goroutines (for the netpoller contract). */
    public static int runnableCount() {
        return graalgo.sched.SchedulerCore.current().runnableCount();
    }
}
