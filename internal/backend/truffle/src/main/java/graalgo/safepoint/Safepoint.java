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
 * <p><strong>JIT shape</strong> (WP-POLLCOST): {@code poll()} sits on every
 * function entry and loop back-edge, so its fast path is one volatile load
 * of {@code SchedulerCore.pollEpoch()} (a plain load on x86) compared with
 * the epoch the calling thread saw at its last full poll — no store, no
 * countdown. The full quantum check ({@code
 * SchedulerCore.pollCurrentThreadStatic}: ThreadLocal handle lookup,
 * pin-section check, {@code System.nanoTime()}) runs behind a
 * {@link TruffleBoundary} at the first poll after the epoch moved: every
 * poll tick (a quarter of the smallest quantum, at most 1 ms, on the
 * scheduler's ticker thread) and whenever a scheduler begins process exit.
 * Semantics: a goroutine whose quantum expired parks at its first poll
 * after the next tick — at most one tick late, inside the GG-SCHED-0001
 * quantum (exact preemption timing is not an observable, SPEC §7.2).
 * Process exit is immediate: the exit bumps the epoch, so the next poll of
 * every thread is full, and while the thread's own scheduler is exiting
 * (the full check returned: pin section, fatal reporter) every poll stays
 * full, so the exit freeze happens at the next safepoint.
 */
public final class Safepoint {
    private Safepoint() {}

    /** Per-thread poll state of language-less roots (one per goroutine vthread). */
    private static final class Seen {
        /** {@code SchedulerCore.pollEpoch()} at this thread's last full poll. */
        int epoch = -1;
    }

    private static final ThreadLocal<Seen> SEEN = ThreadLocal.withInitial(Seen::new);

    /** Phase 2: quantum-expiry check; no-op without a scheduler. */
    public static void poll() {
        pollBoundary();
    }

    /**
     * The poll as seen from {@code node}'s code (TR-1, WP-POLLCOST). For a root built inside a
     * Go context the thread's last-seen epoch lives in its {@code GoThreadSlot} (the language's
     * {@code ContextThreadLocal}): partial evaluation folds the language check and reads the
     * slot inline, so compiled code pays two loads and a compare per poll and calls the
     * boundary once per epoch move. Language-less roots (no context: unit tests, graalgo-run)
     * keep {@link #poll()}.
     */
    public static void poll(com.oracle.truffle.api.nodes.Node node) {
        graalgo.language.GoLanguage lang =
                node.getRootNode().getLanguage(graalgo.language.GoLanguage.class);
        if (lang == null) {
            pollBoundary();
            return;
        }
        graalgo.language.GoThreadSlot slot = lang.threadSlot();
        int epoch = graalgo.sched.SchedulerCore.pollEpoch();
        if (com.oracle.truffle.api.CompilerDirectives.injectBranchProbability(
                com.oracle.truffle.api.CompilerDirectives.FASTPATH_PROBABILITY, slot.pollEpoch == epoch)) {
            return;
        }
        slot.pollEpoch = full(epoch);
    }

    /**
     * The full poll: the quantum check, then the epoch the thread has now seen. The epoch was
     * read BEFORE the check, so an exit or tick that happens during it moves the epoch again
     * and the next poll is full too; when the thread's own scheduler is exiting and the check
     * returned, the result differs from every current and future epoch (it only increases), so
     * every poll stays full.
     */
    @TruffleBoundary(allowInlining = false)
    private static int full(int epoch) {
        return graalgo.sched.SchedulerCore.pollCurrentThreadStatic() ? epoch - 1 : epoch;
    }

    @TruffleBoundary(allowInlining = false)
    private static void pollBoundary() {
        Seen s = SEEN.get();
        int epoch = graalgo.sched.SchedulerCore.pollEpoch();
        if (s.epoch == epoch) {
            return;
        }
        s.epoch = full(epoch);
    }
}
