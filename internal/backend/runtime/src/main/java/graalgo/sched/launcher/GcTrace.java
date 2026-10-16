package graalgo.sched.launcher;

import java.lang.management.GarbageCollectorMXBean;
import java.lang.management.ManagementFactory;
import java.lang.management.MemoryUsage;
import java.util.Locale;
import java.util.Map;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;

import javax.management.Notification;
import javax.management.NotificationEmitter;
import javax.management.NotificationListener;
import javax.management.openmbean.CompositeData;

import com.sun.management.GarbageCollectionNotificationInfo;

/**
 * P2-M5 (WS E): GODEBUG=gctrace emulation.
 *
 * <p>When {@code GODEBUG=gctrace} is set to a non-zero value, a line is
 * printed to stderr for every JVM garbage-collection cycle:
 *
 * <pre>
 * gc 3 @1.234s G1 Young Generation: 12ms, heap 45-&gt;38 MB
 * </pre>
 *
 * <p>The shape mirrors gc's {@code gctrace} ("a line per GC cycle on
 * stderr"); the numbers are JVM-heap sourced (documented in
 * {@code docs/arch/phases/phase2/GODEBUG.md}), not Go-heap: gc prints
 * allocator-level detail ({@code 4-&gt;4-&gt;3 MB, 5 MB goal, ...}) that
 * has no counterpart when the collector is the JVM's.
 *
 * <p>Registration is idempotent (guarded): repeated {@code GoDebug.apply()}
 * calls cannot stack listeners.
 */
public final class GcTrace {
    private GcTrace() {}

    private static final AtomicBoolean started = new AtomicBoolean(false);
    private static final AtomicLong count = new AtomicLong(0);
    private static final long T0_MS = System.currentTimeMillis();
    private static volatile NotificationListener listener;

    /** Starts per-cycle GC trace lines. Idempotent. */
    public static void start() {
        if (!started.compareAndSet(false, true)) {
            return;
        }
        NotificationListener l = (Notification notification, Object handback) -> {
            if (!GarbageCollectionNotificationInfo.GARBAGE_COLLECTION_NOTIFICATION
                    .equals(notification.getType())) {
                return;
            }
            CompositeData cd = (CompositeData) notification.getUserData();
            GarbageCollectionNotificationInfo info =
                    GarbageCollectionNotificationInfo.from(cd);
            long n = count.incrementAndGet();
            double secs = (System.currentTimeMillis() - T0_MS) / 1000.0;
            long durMs = info.getGcInfo().getDuration();
            Map<String, MemoryUsage> before = info.getGcInfo().getMemoryUsageBeforeGc();
            Map<String, MemoryUsage> after = info.getGcInfo().getMemoryUsageAfterGc();
            long b = totalUsed(before);
            long a = totalUsed(after);
            // Locale.ROOT: the documented line shape must not vary with
            // the ambient locale (e.g. "1,234s" under it_IT).
            System.err.println("gc " + n + " @"
                    + String.format(Locale.ROOT, "%.3f", secs) + "s "
                    + info.getGcName() + ": " + durMs + "ms, heap "
                    + (b / (1024 * 1024)) + "->" + (a / (1024 * 1024)) + " MB");
        };
        listener = l;
        for (GarbageCollectorMXBean b : ManagementFactory.getGarbageCollectorMXBeans()) {
            if (b instanceof NotificationEmitter emitter) {
                try {
                    emitter.addNotificationListener(l, null, null);
                } catch (Exception e) {
                    // Best effort: without notifications no trace lines.
                }
            }
        }
    }

    private static long totalUsed(Map<String, MemoryUsage> m) {
        long t = 0;
        for (MemoryUsage u : m.values()) {
            if (u.getUsed() >= 0) {
                t += u.getUsed();
            }
        }
        return t;
    }

    /** Whether tracing was started (tests). */
    public static boolean isStarted() {
        return started.get();
    }

    /**
     * Removes the listener and clears the started flag (tests only).
     * Production code never calls this: listeners are for the life of
     * the JVM.
     */
    static void resetForTests() {
        NotificationListener l = listener;
        listener = null;
        if (l != null) {
            for (GarbageCollectorMXBean b : ManagementFactory.getGarbageCollectorMXBeans()) {
                if (b instanceof NotificationEmitter emitter) {
                    try {
                        emitter.removeNotificationListener(l);
                    } catch (Exception e) {
                        // Best effort.
                    }
                }
            }
        }
        started.set(false);
    }
}
