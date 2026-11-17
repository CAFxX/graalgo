package graalgo.safepoint;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;

/**
 * GC safepoint seam (PLAN.md D8 [DECIDED]; GG-0017 seam slice).
 *
 * <p>Frozen signature: {@code graalgo.safepoint.poll()}. The AST inserts
 * calls at function entries ({@code GoRootNode}) and loop back-edges
 * (WS E).
 *
 * <p><strong>Phase-2 implementation</strong> (P2-D8): this class
 * <em>replaces</em> the Phase-1 no-op of the same fully-qualified name.
 * The Phase-2 classpath puts {@code phase2/out/classes} before
 * {@code phase1/out/classes}, so every {@code poll()} call compiled into
 * the Phase-1 AST resolves here. The signature is byte-identical to the
 * frozen one (bodies are INTERNAL per freeze §3.5 — no ADR/version bump
 * needed); {@code FreezeSignatureTest} keeps passing.
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
 * observable, SPEC §7.2).
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
    }

    private static final ThreadLocal<Countdown> COUNTDOWN = ThreadLocal.withInitial(Countdown::new);

    /** Phase 2: quantum-expiry check; no-op without a scheduler. */
    public static void poll() {
        pollBoundary();
    }

    @TruffleBoundary(allowInlining = false)
    private static void pollBoundary() {
        Countdown c = COUNTDOWN.get();
        if (--c.remaining > 0) {
            return;
        }
        c.remaining = STRIDE;
        graalgo.sched.SchedulerCore.pollCurrentThreadStatic();
    }
}
