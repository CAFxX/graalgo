package graalgo.p3os;

import java.lang.foreign.FunctionDescriptor;
import java.lang.foreign.Linker;
import java.lang.foreign.SymbolLookup;
import java.lang.foreign.ValueLayout;
import java.lang.invoke.MethodHandle;
import java.util.Locale;

/**
 * The process umask, read once from libc via the Panama Foreign Function
 * &amp; Memory API (JDK 25, final API).
 *
 * <p>gc's {@code open(2)}/{@code mkdir(2)} create paths apply
 * {@code perm &^ umask}, but the {@code java.nio.file} APIs cannot read
 * the umask — so {@link GoOs}'s create paths cache the process mask here
 * and apply {@code perm &^ mask} themselves (P1-R5-5).
 *
 * <p>The read uses the classic set/restore trick: {@code old = umask(0)},
 * then immediately {@code umask(old)} to restore. It runs exactly once in
 * this class's static initializer, which {@link GoOs} forces from its own
 * static initializer — i.e. while the JVM is still single-threaded, before
 * any guest code runs — so the trick's tiny window (mask transiently 0)
 * cannot collide with a concurrent {@code umask(2)}. Guest code has no
 * umask syscall, and nothing else in the process calls {@code umask(2)}
 * during that window.
 *
 * <p>Non-POSIX platforms (Windows) have no umask: the cached mask is 0,
 * preserving the previous exact-perm behavior where umask is meaningless.
 * If the native read fails for any reason, the mask degrades to 0
 * (exact perms) rather than breaking file creation.
 *
 * <p>Staleness: graalgo has no synthetic {@code syscall.Umask} today, so
 * nothing can change the process mask underneath this cache. If a future
 * synthetic {@code Umask} lands, it must update this cache (recompute or
 * invalidate) — not merely call the native {@code umask(2)} — or creations
 * will keep applying a stale mask.
 */
final class Umask {
    private Umask() {}

    /**
     * Forces class initialization (which reads and caches the mask).
     * Called once from {@link GoOs}'s static initializer.
     */
    static void preload() {
        // no-op: the static field read below is the actual trigger
        int ignored = MASK;
        if (ignored == Integer.MIN_VALUE) {
            throw new AssertionError("unreachable");
        }
    }

    /** Cached process umask: 0 on non-POSIX or if the read failed. */
    static final int MASK = readOnce();

    private static int readOnce() {
        if (!isPosix()) {
            return 0;
        }
        try {
            Linker linker = Linker.nativeLinker();
            SymbolLookup libc = linker.defaultLookup();
            MethodHandle umask = linker.downcallHandle(
                    libc.find("umask").orElseThrow(UnsatisfiedLinkError::new),
                    FunctionDescriptor.of(ValueLayout.JAVA_INT, ValueLayout.JAVA_INT));
            int old = (int) umask.invokeExact(0);
            try {
                return old;
            } finally {
                // Restore immediately: the mask is 0 only inside this call.
                // The int result must be bound — an unbound invokeExact in
                // statement position is mislinked as (int)void at runtime
                // (signature-polymorphic pitfall).
                int restored = (int) umask.invokeExact(old);
                // Restoring must return the transient 0 we had set; anything
                // else means the process mask changed under us.
                if (restored != 0) {
                    throw new IllegalStateException("umask restore failed");
                }
            }
        } catch (Throwable t) {
            // Degrade to exact-perm behavior rather than breaking creation.
            return 0;
        }
    }

    private static boolean isPosix() {
        String os = System.getProperty("os.name", "").toLowerCase(Locale.ROOT);
        return !os.startsWith("windows");
    }
}
