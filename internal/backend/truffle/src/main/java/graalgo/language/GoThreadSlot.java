package graalgo.language;

import graalgo.runtime.GoroutineState;

/**
 * Per-(context, thread) slot — the {@code M} of truffle-design §2.5: the
 * state of the goroutine currently running on this thread. Reached through
 * {@link GoLanguage}'s {@code ContextThreadLocal}, which partial evaluation
 * reads without a host {@code ThreadLocal} lookup or a boundary call.
 * Written only by {@link GoroutineState#install}/{@link GoroutineState#restore}
 * on the owning thread.
 */
public final class GoThreadSlot {
    public GoroutineState state;
    /** SchedulerCore.pollEpoch() at this thread's last full poll (Safepoint.poll(Node)). */
    public int pollEpoch = -1;
    /** The goroutine state of the bundle node package (graalgo.go), created on first use. */
    public graalgo.go.runtime.G g;
}
