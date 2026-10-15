package graalgo.runtime.sync;

import graalgo.runtime.GoCalls;
import graalgo.values.GoFunc;

import java.lang.management.GarbageCollectorMXBean;
import java.lang.management.ManagementFactory;
import java.util.concurrent.atomic.AtomicBoolean;

import javax.management.Notification;
import javax.management.NotificationEmitter;
import javax.management.NotificationListener;
import javax.management.openmbean.CompositeData;

import com.sun.management.GarbageCollectionNotificationInfo;

/**
 * {@code sync.runtime_registerPoolCleanup} (graalgo).
 *
 * <p>Go's {@code sync} package (go1.27.1, sync/pool.go) registers its
 * {@code poolCleanup} func with the runtime at init time:
 * {@code runtime_registerPoolCleanup(poolCleanup)}. gc stores it and the
 * GC invokes it at the start of every stop-the-world pause
 * ({@code clearpools} in runtime/mgc.go): victim caches are dropped and
 * primary caches rotate to victim.
 *
 * <p>Mapping to the JVM: the JVM GC <em>is</em> the Go GC (ADR-0002 D4),
 * so the registered cleanup is invoked from a GC notification listener
 * (the {@link GarbageCollectorMXBean} notification path, same mechanism
 * as {@link graalgo.runtime.GcInterop}'s pause ring). Invoking Go code
 * from the notification thread follows the
 * {@code runtime.SetFinalizer} precedent ({@code GcInterop}'s
 * {@code FinalizerAction} invokes its {@link GoFunc} from the Cleaner
 * thread via {@link GoCalls#invoke}).
 *
 * <p>Why this is safe here: the cleanup only touches Go-level pool state
 * ({@code allPools}/{@code oldPools} in sync/pool.go). A panicking
 * cleanup is reported loudly on stderr and never breaks the notification
 * thread (finalizer precedent). Reentrancy (cleanup triggering another
 * GC notification) is guarded: at most one invocation runs at a time.
 */
public final class GoPoolCleanup {
    private GoPoolCleanup() {}

    /** The func registered by {@code sync.init()}, or null. */
    private static volatile GoFunc cleanup;

    /** Reentrancy guard: at most one cleanup invocation at a time. */
    private static final AtomicBoolean NOTIFYING = new AtomicBoolean();

    static {
        NotificationListener gcListener = (Notification notification, Object handback) -> {
            if (!GarbageCollectionNotificationInfo.GARBAGE_COLLECTION_NOTIFICATION
                    .equals(notification.getType())) {
                return;
            }
            GoFunc f = cleanup;
            if (f == null) {
                return;
            }
            if (!NOTIFYING.compareAndSet(false, true)) {
                return;
            }
            try {
                GoCalls.invoke(f, new Object[0]);
            } catch (Throwable t) {
                // gc: poolCleanup must not allocate or fail. We cannot
                // crash the host from here; be loud instead (finalizer
                // precedent in GcInterop).
                System.err.println("graalgo: pool cleanup panicked: " + t);
                t.printStackTrace(System.err);
            } finally {
                NOTIFYING.set(false);
            }
        };
        for (GarbageCollectorMXBean b : ManagementFactory.getGarbageCollectorMXBeans()) {
            if (b instanceof NotificationEmitter emitter) {
                try {
                    emitter.addNotificationListener(gcListener, null, null);
                } catch (Exception e) {
                    // Best effort: without notifications the cleanup never
                    // runs, matching a GC that never happens.
                }
            }
        }
    }

    /**
     * {@code sync.runtime_registerPoolCleanup(cleanup func())}: stores the
     * cleanup func; it is invoked on every JVM GC notification thereafter.
     *
     * @param cleanupFn the Go func value (D5 {@link GoFunc}, possibly boxed)
     */
    public static void register(Object cleanupFn) {
        Object f = cleanupFn;
        if (f instanceof graalgo.values.GoInterface gi) {
            f = gi.data();
        }
        if (!(f instanceof GoFunc g)) {
            throw new IllegalArgumentException(
                    "GoPoolCleanup.register: expected GoFunc, got "
                            + (f == null ? "null" : f.getClass().getSimpleName()));
        }
        cleanup = g;
    }

    /** Clears the registration (program reinstall isolation). */
    public static void clear() {
        cleanup = null;
    }
}
