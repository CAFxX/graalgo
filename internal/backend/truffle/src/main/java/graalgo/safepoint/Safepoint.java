package graalgo.safepoint;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;

/**
 * GC safepoint seam (PLAN.md D8 [DECIDED]; GG-0017 seam slice).
 *
 * <p>Frozen signature: {@code graalgo.safepoint.poll()}. The AST inserts
 * calls at function entries ({@code GoRootNode}) and loop back-edges
 * (WS E).
 *
 * <p>The signature is the frozen one (bodies are INTERNAL per freeze
 * §3.5); {@code FreezeSignatureTest} checks it.
 *
 * <p>Semantics: quantum check for the calling goroutine
 * (GG-SCHED-0001). When no scheduler is installed (plain Phase-1 runs),
 * this is a no-op — Phase-1 parity.
 *
 * <p><strong>JIT shape</strong> (lane T0.B): {@code poll()} sits on every
 * function entry and loop back-edge, so it must be cheap and must not be
 * partially evaluated into JDK code. The full quantum check
 * ({@code SchedulerCore.pollCurrentThread}: ThreadLocal handle lookup,
 * pin-section check, {@code System.nanoTime()} ≈ 19 ns on the reference
 * box) runs once every {@link #STRIDE} polls of the calling thread; the
 * polls in between only decrement a per-thread countdown. Everything that
 * touches a ThreadLocal lives behind a {@link TruffleBoundary}, so a
 * compiled Go function sees one opaque call per poll instead of the
 * ThreadLocal/nanoTime graph. Semantics: a goroutine whose quantum
 * expired parks at most {@code STRIDE - 1} polls later than before —
 * a few microseconds of extra preemption latency in compiled code, well
 * inside the GG-SCHED-0001 quantum (exact preemption timing is not an
 * observable, SPEC §7.2). Process exit is not strided: a poll takes
 * the full path whenever {@code SchedulerCore.exitEpoch()} moved since the
 * thread's last full poll, and every poll does while the thread's own
 * scheduler is exiting, so the exit freeze happens at the next safepoint.
 */
public final class Safepoint {
    private Safepoint() {}

    /**
     * Polls per full quantum check. 128
     * keeps the amortized nanoTime cost under 0.2 ns/poll while bounding
     * the added latency to 127 polls (≲1 µs compiled, ≲100 µs
     * interpreted — both far below the 10 ms quantum).
     */
    static final int STRIDE = 128;

    /** Per-thread poll countdown (one per goroutine vthread). */
    private static final class Countdown {
        int remaining;
        /** {@code SchedulerCore.exitEpoch()} at this thread's last full poll. */
        int exitEpoch;
    }

    private static final ThreadLocal<Countdown> COUNTDOWN = ThreadLocal.withInitial(Countdown::new);

    /** Phase 2: quantum-expiry check; no-op without a scheduler. */
    public static void poll() {
        pollBoundary();
    }

    /**
     * The poll as seen from {@code node}'s code (TR-1). For a root built
     * inside a Go context the countdown lives in the thread's
     * {@code GoThreadSlot} (the language's {@code ContextThreadLocal}):
     * partial evaluation folds the language check and reads the slot
     * inline, so compiled code pays a decrement and a branch per poll and
     * calls the boundary once every {@link #STRIDE} polls. Measured
     * reason: on polyglot threads the host {@code ThreadLocal} lookup of
     * {@link #poll()} misses its first probe (more thread-locals per
     * thread) and cost ~20% on the {@code loop} benchmark. Language-less
     * roots (no context: unit tests, graalgo-run) keep {@link #poll()}.
     * Same semantics: a pending exit bypasses the stride.
     */
    public static void poll(com.oracle.truffle.api.nodes.Node node) {
        graalgo.language.GoLanguage lang =
                node.getRootNode().getLanguage(graalgo.language.GoLanguage.class);
        if (lang == null) {
            pollBoundary();
            return;
        }
        graalgo.language.GoThreadSlot slot = lang.threadSlot();
        int epoch = graalgo.sched.SchedulerCore.exitEpoch();
        if (--slot.pollCountdown > 0 && slot.exitEpoch == epoch) {
            return;
        }
        // The epoch is read BEFORE the full check: an exit that begins
        // during it bumps the epoch again, so the next poll is full too.
        slot.exitEpoch = epoch;
        slot.pollCountdown = pollFull() ? 1 : STRIDE;
    }

    @TruffleBoundary(allowInlining = false)
    private static boolean pollFull() {
        return graalgo.sched.SchedulerCore.pollCurrentThreadStatic();
    }

    @TruffleBoundary(allowInlining = false)
    private static void pollBoundary() {
        Countdown c = COUNTDOWN.get();
        // A pending process exit bypasses the stride: the exit/fatal
        // freeze must happen at the next safepoint (SchedulerCore
        // #freezeForExit), not up to STRIDE-1 polls later. The epoch is
        // read before the full check (an exit beginning during it bumps
        // the epoch again); while the thread's own scheduler is exiting
        // and the poll returned (pin section, fatal reporter), every poll
        // stays full.
        int epoch = graalgo.sched.SchedulerCore.exitEpoch();
        if (--c.remaining > 0 && c.exitEpoch == epoch) {
            return;
        }
        c.exitEpoch = epoch;
        c.remaining = graalgo.sched.SchedulerCore.pollCurrentThreadStatic() ? 1 : STRIDE;
    }
}
