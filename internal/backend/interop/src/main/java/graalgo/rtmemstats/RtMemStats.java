package graalgo.rtmemstats;

import java.lang.management.GarbageCollectorMXBean;
import java.lang.management.ManagementFactory;
import java.lang.management.MemoryMXBean;
import java.lang.management.MemoryUsage;
import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;

import graalgo.frontend.FrontendException;
import graalgo.p3log.P3Log;
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

    /**
     * HotSpot thread-allocation accounting, when available: the
     * {@code com.sun.management} extension reports, per live JVM thread,
     * the cumulative bytes that thread has allocated. Guest goroutines
     * run on carrier threads, so a process-wide sum over live threads
     * captures guest allocation volume (verified empirically on the
     * pinned GraalVM JDK: a virtual thread's own counter reads 0 while
     * the live-thread sum grows by the allocated amount and survives
     * the virtual thread's death, because the carrier thread persists).
     * Null when the extension is unavailable or allocation accounting
     * cannot be enabled — then {@code TotalAlloc} has no measured
     * source (see {@link #currentTotalAlloc}).
     */
    private static final com.sun.management.ThreadMXBean ALLOC_MX = allocMXBean();

    /**
     * Monotonic high-water mark over the live-thread allocation sum.
     * The sum itself can dip when a short-lived platform thread exits
     * (its counter leaves the sum with it), so the high-water mark —
     * not the raw sum — is what {@code TotalAlloc} reports: gc defines
     * {@code TotalAlloc} as monotonic, and the mark preserves that
     * while remaining a measured quantity (a lower bound on process
     * allocation volume), not a synthetic counter. The earlier code
     * deliberately rejected a max-seen accumulator over <em>live
     * heap</em> because live heap is not allocation volume; here the
     * underlying quantity IS cumulative allocation, and the mark only
     * repairs thread-exit loss.
     */
    private static final AtomicLong TOTAL_ALLOC_HIGH_WATER = new AtomicLong(0);

    /** Whether the unmeasured-counts warning has been logged. */
    private static final AtomicBoolean COUNTS_WARNING_LOGGED = new AtomicBoolean(false);

    private static com.sun.management.ThreadMXBean allocMXBean() {
        try {
            java.lang.management.ThreadMXBean base =
                    ManagementFactory.getThreadMXBean();
            if (!(base instanceof com.sun.management.ThreadMXBean tmx)) {
                return null;
            }
            if (!tmx.isThreadAllocatedMemorySupported()) {
                return null;
            }
            if (!tmx.isThreadAllocatedMemoryEnabled()) {
                tmx.setThreadAllocatedMemoryEnabled(true);
            }
            return tmx;
        } catch (RuntimeException e) {
            return null;
        }
    }

    /**
     * Current {@code TotalAlloc}: the monotonic high-water mark over the
     * process-wide live-thread allocation sum, or 0 when allocation
     * accounting is unavailable (with the counts warning noting that).
     * Saturating: a sum overflowing {@code long} pins at
     * {@code Long.MAX_VALUE} rather than wrapping negative.
     */
    static long currentTotalAlloc() {
        if (ALLOC_MX == null) {
            return TOTAL_ALLOC_HIGH_WATER.get();
        }
        long sum = 0;
        for (long id : ALLOC_MX.getAllThreadIds()) {
            long v = ALLOC_MX.getThreadAllocatedBytes(id);
            if (v < 0) {
                continue;
            }
            if (sum > Long.MAX_VALUE - v) {
                sum = Long.MAX_VALUE;
                break;
            }
            sum += v;
        }
        return TOTAL_ALLOC_HIGH_WATER.accumulateAndGet(sum, Math::max);
    }

    /**
     * Logs, once per process, that the allocation-count fields are
     * unmeasured (see the {@link #fillMemStats} mapping). This is the
     * loud signal available for plain struct fields: a constant
     * sentinel cancels in the {@code m2 - m1} delta idioms (so it can
     * never fail those loudly by itself), and any varying non-count
     * would be fabricated data, so the warning — routed via
     * {@link P3Log} to stderr by default — is what makes the gap
     * visible in every affected program's output instead of living
     * only in a doc.
     */
    static void warnCountsUnmeasured() {
        if (COUNTS_WARNING_LOGGED.compareAndSet(false, true)) {
            String extra = ALLOC_MX == null
                    ? " Thread allocation accounting is also unavailable on this JVM,"
                            + " so TotalAlloc is reported as 0 and is likewise unmeasured."
                    : "";
            P3Log.warn(RtMemStats.class,
                    "graalgo: runtime.ReadMemStats: Mallocs, Frees, HeapObjects,"
                            + " HeapReleased and Lookups are not measurable on the JVM"
                            + " and are reported as 0 (unmeasured, not a measurement);"
                            + " do not use them for allocation assertions —"
                            + " testing.AllocsPerRun-style deltas over them are meaningless"
                            + " on this backend." + extra);
        }
    }

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
     *   <li>{@code TotalAlloc}: cumulative JVM allocation volume —
     *       the monotonic high-water mark over the process-wide sum of
     *       per-thread allocated bytes (see {@link #currentTotalAlloc}).
     *       This is <strong>not</strong> gc's "bytes allocated for heap
     *       objects" exactly: it counts every JVM heap allocation on
     *       every live thread (guest allocations via carrier threads,
     *       plus JVM-internal allocations), and it can undercount
     *       allocations made by short-lived platform threads that exit
     *       between samples (their counters leave the sum; the
     *       high-water mark keeps the value monotonic but cannot
     *       recover bytes it never sampled). Within those bounds the
     *       standard gc idiom {@code m2.TotalAlloc - m1.TotalAlloc}
     *       ("bytes allocated between the two calls") is meaningful as
     *       JVM allocation volume, and it never decreases. (Review
     *       R1-6 fix; documented user-facing in
     *       docs/arch/phases/phase2/DIVERGENCES.md, DIV-M4-11; asserted
     *       by {@code RtMemStatsTest.testTotalAllocIsMonotonic} and
     *       {@code testTotalAllocGrowsWithAllocation}.)
     *   <li>{@code Lookups}/{@code Mallocs}/{@code Frees}/
     *       {@code HeapObjects}/{@code HeapReleased}: reported as 0,
     *       which is an <strong>unmeasured sentinel, not a
     *       measurement</strong> — the JVM exposes no allocation-count,
     *       live-object-count, or returned-to-OS counters, and no
     *       backend counter can stand in for gc's object counts without
     *       fabricating them (guest-value constructor counts would
     *       count backend temporaries and miss Java-delegated
     *       allocations). Consequence: <strong>{@code testing.AllocsPerRun}
     *       over these fields is meaningless on this backend</strong>
     *       (gc computes it as {@code m2.Mallocs - m1.Mallocs}); an
     *       assertion over it proves nothing. Because plain struct
     *       fields cannot fail per-field (any constant sentinel cancels
     *       in delta idioms), the loud signal is a once-per-process
     *       warning via {@link #warnCountsUnmeasured} instead of a
     *       panic that would also destroy the measurable fields
     *       ({@code Alloc}, {@code HeapAlloc}, {@code NumGC}). The
     *       sentinel contract is pinned by
     *       {@code RtMemStatsTest.testMallocsFreesAreUnmeasuredZero}.
     *       (Documented user-facing in DIV-M4-11.)
     *   <li>{@code Sys}: heap committed + non-heap committed.
     *   <li>{@code HeapSys}: heap committed; {@code HeapIdle}: committed
     *       minus used.
     *   <li>{@code HeapReleased}/{@code HeapObjects}: the JVM does not
     *       report memory returned to the OS or the live object count;
     *       0 as the unmeasured sentinel (see the counts bullet above).
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

        warnCountsUnmeasured();
        ms.setLong(longIndex(d, "Alloc", what), heapUsed);
        ms.setLong(longIndex(d, "TotalAlloc", what), currentTotalAlloc());
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
