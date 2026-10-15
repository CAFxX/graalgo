package graalgo.p3link;

import graalgo.runtime.Panic;
import graalgo.values.GoString;

/**
 * Runtime provider for {@code internal/synctest}'s {@code //go:linkname}
 * stubs (real-sync lane, PKG-012 follow-up).
 *
 * <p>In gc, the bodyless declarations in
 * {@code src/internal/synctest/synctest.go} are provided by the runtime
 * ({@code src/runtime/synctest.go}) via {@code //go:linkname}. The
 * frontend emits each as a linkname call with symbol
 * {@code "internal/synctest.<name>"}; they dispatch here, registered by
 * {@link LinknameIntrinsics}.
 *
 * <p>graalgo does not implement synctest bubbles (consistent with the
 * existing {@code runtime_SemacquireWaitGroup} handling, which ignores
 * the synctest-durability flag). Every intrinsic below is therefore the
 * honest no-bubble answer:
 * <ul>
 *   <li>{@code IsInBubble} → false: no goroutine is ever in a bubble.
 *       This is also what makes the real {@code sync.WaitGroup}'s
 *       bubble-tracking branches dead code under graalgo, exactly as if
 *       the program never ran under {@code synctest.Run}.</li>
 *   <li>{@code associate} → {@code Unbubbled} (0),
 *       {@code isAssociated} → false, {@code disassociate} /
 *       {@code release} → no-ops,
 *       {@code acquire} → nil: with no bubbles there is nothing to
 *       associate with. ({@code release} diverges from gc here: gc's
 *       {@code synctest_release} is
 *       {@code bubble.(*synctestBubble).decActive()}, whose type assertion
 *       panics on a nil argument; the no-op is the honest answer when
 *       {@link #acquire} can only yield nil.)</li>
 *   <li>{@code inBubble} → Go panic: gc installs the bubble on the
 *       goroutine and then runs {@code f}; with no bubbles to install,
 *       running {@code f} would silently drop the bubble contract (fake
 *       time, deadlock detection), so failing loud — like {@code Run} —
 *       is the honest choice. Never a silent drop of {@code f}.</li>
 *   <li>{@code Wait} → Go panic {@code "goroutine is not in a bubble"}:
 *       this is exactly gc's behavior when called outside a bubble, and
 *       under graalgo every call is outside a bubble.</li>
 *   <li>{@code Run} → Go panic: executing the function without a bubble
 *       would silently drop synctest's fake-time and deadlock-detection
 *       semantics, producing confusing test results. Failing loud is the
 *       honest contract; bubble support is future work.</li>
 * </ul>
 */
public final class Synctest {
    private Synctest() {}

    /** {@code internal/synctest.IsInBubble() bool} — never in a bubble. */
    public static boolean isInBubble() {
        return false;
    }

    /**
     * {@code internal/synctest.associate(p unsafe.Pointer) int} — with no
     * bubbles, {@code p} is never associated: {@code Unbubbled} (0).
     */
    public static long associate(long addr) {
        return 0L;
    }

    /** {@code internal/synctest.disassociate} — no-op: nothing is associated. */
    public static void disassociate(long addr) {
    }

    /** {@code internal/synctest.isAssociated} — false: no bubbles. */
    public static boolean isAssociated(long addr) {
        return false;
    }

    /** {@code internal/synctest.acquire() any} — nil: no bubble to acquire. */
    public static Object acquire() {
        return null;
    }

    /**
     * {@code internal/synctest.release} — no-op: {@link #acquire} yields nil,
     * so there is never a bubble to release. Divergence from gc, documented:
     * gc's {@code synctest_release} is
     * {@code bubble.(*synctestBubble).decActive()}
     * ({@code src/runtime/synctest.go}), whose type assertion panics when
     * called with nil; we accept nil silently because a nil release is the
     * only reachable outcome of our {@link #acquire}.
     */
    public static void release(Object bubble) {
    }

    /**
     * {@code internal/synctest.inBubble(bubble any, f func())} — loud Go
     * panic: gc installs {@code bubble} on the current goroutine and then
     * runs {@code f}; with no bubbles to install, running {@code f} would
     * silently drop synctest's fake-time and deadlock-detection semantics.
     * Failing loud is the honest contract (same rationale as
     * {@link #run}); bubble support is future work. Never a silent drop
     * of {@code f}.
     */
    public static void inBubble(Object bubble, Object f) {
        Panic.throwPanic(GoString.fromUtf8("synctest.inBubble: not implemented by graalgo"));
        throw new AssertionError("unreachable: Panic.throwPanic returned");
    }

    /**
     * {@code internal/synctest.Run(f func())} — loud Go panic: bubble
     * semantics (fake time, deadlock detection) are not implemented.
     */
    public static void run(Object f) {
        Panic.throwPanic(GoString.fromUtf8("synctest.Run: not implemented by graalgo"));
        throw new AssertionError("unreachable: Panic.throwPanic returned");
    }

    /**
     * {@code internal/synctest.Wait()} — gc's unbubbled behavior: Go panic
     * {@code "goroutine is not in a bubble"}. Every graalgo call site is
     * unbubbled, so this is exactly faithful.
     */
    public static void waitForBubble() {
        Panic.throwPanic(GoString.fromUtf8("goroutine is not in a bubble"));
        throw new AssertionError("unreachable: Panic.throwPanic returned");
    }
}
