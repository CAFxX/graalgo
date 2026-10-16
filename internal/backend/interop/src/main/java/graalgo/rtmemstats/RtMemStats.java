package graalgo.rtmemstats;

import java.lang.management.GarbageCollectorMXBean;
import java.lang.management.ManagementFactory;
import java.lang.management.MemoryMXBean;
import java.lang.management.MemoryUsage;
import java.nio.charset.StandardCharsets;
import java.util.List;

import graalgo.frontend.FrontendException;
import graalgo.runtime.StackTrace;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Shared helpers for the {@code runtime} MemStats-family wire nodes
 * ({@code runtimereadmemstats}, {@code runtimegoroot},
 * {@code runtimeversion}, {@code runtimestack}).
 *
 * <p>The frontend lowers calls to {@code runtime.ReadMemStats},
 * {@code runtime.GOROOT}, {@code runtime.Version} and
 * {@code runtime.Stack} to these additive wire nodes (the same pattern as
 * {@code "runtimenumcpu"}). Each node below implements the real gc
 * semantics against JVM telemetry — no stubs.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook — no Phase-1
 * {@code NodeFactory} change.
 */
final class RtMemStats {
    private RtMemStats() {}

    static GoString goString(String s) {
        return new GoString(s.getBytes(StandardCharsets.UTF_8));
    }

    /**
     * Descriptor ordinal of a named longs-carried field. Note: this is the
     * field's position in {@link GoStructDescriptor#fields()}, which is
     * what {@link GoStruct#setLong}/{@link GoStruct#getLong} take — NOT
     * the {@code longs[]} carrier index.
     */
    static int longIndex(GoStructDescriptor d, String field, String what) {
        List<GoStructDescriptor.Field> fields = d.fields();
        for (int i = 0; i < fields.size(); i++) {
            GoStructDescriptor.Field f = fields.get(i);
            if (f.name().equals(field)) {
                if (!f.inLongs()) {
                    throw new FrontendException(
                            what + ": field " + field + " is not longs-carried");
                }
                return i;
            }
        }
        throw new FrontendException(what + ": no field " + field);
    }

    /**
     * Descriptor ordinal of a named objects-carried field (see
     * {@link #longIndex} for the ordinal-vs-carrier-index distinction).
     */
    static int objIndex(GoStructDescriptor d, String field, String what) {
        List<GoStructDescriptor.Field> fields = d.fields();
        for (int i = 0; i < fields.size(); i++) {
            GoStructDescriptor.Field f = fields.get(i);
            if (f.name().equals(field)) {
                if (f.inLongs()) {
                    throw new FrontendException(
                            what + ": field " + field + " is not objects-carried");
                }
                return i;
            }
        }
        throw new FrontendException(what + ": no field " + field);
    }

    /**
     * Populates a {@code runtime.MemStats} struct from JVM telemetry.
     * Field names match the synthetic {@code runtime} view's MemStats
     * declaration (gc 1.27.1 field layout, {@code src/runtime/mstats.go}).
     *
     * <p>Mapping (documented approximations where the JVM exposes no
     * equivalent):
     * <ul>
     *   <li>{@code Alloc}/{@code HeapAlloc}/{@code HeapInuse}: heap used.
     *   <li>{@code TotalAlloc}: the JVM exposes no cumulative allocation
     *       counter; heap used is the best available live figure.
     *   <li>{@code Sys}: heap committed + non-heap committed.
     *   <li>{@code HeapSys}: heap committed; {@code HeapIdle}: committed
     *       minus used.
     *   <li>{@code HeapReleased}/{@code HeapObjects}: the JVM does not
     *       report memory returned to the OS or the live object count; 0.
     *   <li>{@code StackInuse}/{@code StackSys}/{@code MSpanInuse}/
     *       {@code MSpanSys}/{@code MCacheInuse}/{@code MCacheSys}/
     *       {@code BuckHashSys}/{@code OtherSys}: Go-runtime-internal
     *       accounting with no JVM equivalent; 0.
     *   <li>{@code NumGC}/{@code PauseTotalNs}: summed over the GC MXBeans
     *       (collection time is millisecond-granular, converted to ns);
     *       {@code GCCPUFraction}: pause time over process uptime.
     *   <li>{@code PauseNs}/{@code PauseEnd}: the JVM exposes no per-pause
     *       history; left zeroed.
     *   <li>{@code BySize}: no per-size-class accounting on the JVM; left
     *       zeroed.
     *   <li>{@code EnableGC}: true; {@code DebugGC}: false.
     * </ul>
     */
    static void fillMemStats(GoStruct ms) {
        GoStructDescriptor d = ms.descriptor();
        String what = "runtimereadmemstats";
        MemoryMXBean mem = ManagementFactory.getMemoryMXBean();
        MemoryUsage heap = mem.getHeapMemoryUsage();
        MemoryUsage nonHeap = mem.getNonHeapMemoryUsage();
        // getUsed/getCommitted return -1 when the value is undefined; gc
        // never reports negative counters, so clamp.
        long heapUsed = Math.max(heap.getUsed(), 0);
        long heapCommitted = Math.max(heap.getCommitted(), 0);
        long nonHeapCommitted = Math.max(nonHeap.getCommitted(), 0);
        long sys = heapCommitted + nonHeapCommitted;

        ms.setLong(longIndex(d, "Alloc", what), heapUsed);
        ms.setLong(longIndex(d, "TotalAlloc", what), heapUsed);
        ms.setLong(longIndex(d, "Sys", what), sys);
        ms.setLong(longIndex(d, "Lookups", what), 0);
        ms.setLong(longIndex(d, "Mallocs", what), 0);
        ms.setLong(longIndex(d, "Frees", what), 0);
        ms.setLong(longIndex(d, "HeapAlloc", what), heapUsed);
        ms.setLong(longIndex(d, "HeapSys", what), heapCommitted);
        ms.setLong(longIndex(d, "HeapIdle", what),
                Math.max(heapCommitted - heapUsed, 0));
        ms.setLong(longIndex(d, "HeapInuse", what), heapUsed);
        ms.setLong(longIndex(d, "HeapReleased", what), 0);
        ms.setLong(longIndex(d, "HeapObjects", what), 0);
        ms.setLong(longIndex(d, "StackInuse", what), 0);
        ms.setLong(longIndex(d, "StackSys", what), 0);
        ms.setLong(longIndex(d, "MSpanInuse", what), 0);
        ms.setLong(longIndex(d, "MSpanSys", what), 0);
        ms.setLong(longIndex(d, "MCacheInuse", what), 0);
        ms.setLong(longIndex(d, "MCacheSys", what), 0);
        ms.setLong(longIndex(d, "BuckHashSys", what), 0);
        ms.setLong(longIndex(d, "GCSys", what), 0);
        ms.setLong(longIndex(d, "OtherSys", what), 0);
        ms.setLong(longIndex(d, "NextGC", what), 0);
        ms.setLong(longIndex(d, "LastGC", what), 0);

        long numGC = 0;
        long pauseTotalNs = 0;
        for (GarbageCollectorMXBean gc : ManagementFactory.getGarbageCollectorMXBeans()) {
            long count = gc.getCollectionCount();
            long millis = gc.getCollectionTime();
            if (count > 0) {
                numGC += count;
            }
            if (millis > 0) {
                pauseTotalNs += millis * 1_000_000L;
            }
        }
        ms.setLong(longIndex(d, "PauseTotalNs", what), pauseTotalNs);
        ms.setLong(longIndex(d, "NumGC", what), numGC);
        ms.setLong(longIndex(d, "NumForcedGC", what), 0);
        long uptimeNs = Math.max(
                ManagementFactory.getRuntimeMXBean().getUptime(), 1) * 1_000_000L;
        ms.setLong(longIndex(d, "GCCPUFraction", what),
                Double.doubleToLongBits((double) pauseTotalNs / (double) uptimeNs));
        ms.setLong(longIndex(d, "EnableGC", what), 1);
        ms.setLong(longIndex(d, "DebugGC", what), 0);

        // PauseNs / PauseEnd rings and BySize: no JVM equivalent; the
        // zero value they already carry is the honest report. Verify the
        // shape so a frontend/backend layout drift fails loud instead of
        // silently misreporting.
        Object pauseNs = ms.getObject(objIndex(d, "PauseNs", what));
        if (!(pauseNs instanceof long[] pa) || pa.length != 256) {
            throw new FrontendException(
                    what + ": PauseNs is not a 256-element long[]");
        }
        Object pauseEnd = ms.getObject(objIndex(d, "PauseEnd", what));
        if (!(pauseEnd instanceof long[] pe) || pe.length != 256) {
            throw new FrontendException(
                    what + ": PauseEnd is not a 256-element long[]");
        }
        Object bySize = ms.getObject(objIndex(d, "BySize", what));
        if (!(bySize instanceof Object[] bs) || bs.length != 61) {
            throw new FrontendException(
                    what + ": BySize is not a 61-element Object[]");
        }
    }

    /**
     * Formats the current goroutine's Go stack in gc's
     * {@code runtime.Stack} rendering ({@code "goroutine N [running]:\n"}
     * header, one {@code "func()\n\tfile:line\n"} block per frame).
     *
     * <p>The header id is the backing JVM thread's id — a stable
     * per-thread number, not gc's goroutine numbering (documented
     * deviation; no Go-visible goroutine id exists on this backend).
     */
    static byte[] formatCurrentStack(com.oracle.truffle.api.source.SourceSection callSite) {
        List<StackTrace.FrameData> frames = StackTrace.captureAt(callSite);
        StringBuilder sb = new StringBuilder();
        sb.append("goroutine ").append(Thread.currentThread().threadId())
                .append(" [running]:\n");
        for (StackTrace.FrameData f : frames) {
            sb.append(StackTrace.qualifyFuncName(f.funcName())).append("()\n");
            sb.append('\t').append(f.file()).append(':').append(f.line()).append('\n');
        }
        sb.append('\n');
        return sb.toString().getBytes(StandardCharsets.UTF_8);
    }
}
