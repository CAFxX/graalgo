package graalgo.jfr;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.atomic.AtomicBoolean;

import jdk.jfr.Recording;
import jdk.jfr.consumer.RecordedEvent;
import jdk.jfr.consumer.RecordingFile;

import graalgo.runtime.ChanOps;
import graalgo.runtime.Timers;
import graalgo.sched.SchedulerCore;

/**
 * P2-M6 JFR event tests (WS F; GG-0008 JFR slice).
 *
 * <p>Red-green discipline:
 * <ul>
 *   <li>{@code testEventsFlow} fails if any hookup is missing (no start/end,
 *       no ChannelBlock, no park events) or if an event is malformed.
 *   <li>{@code testNoEventsWhenDisabled} fails if the isEnabled() guards
 *       leak events when no recording enables them.
 *   <li>{@code testYieldHotLoopOverheadWithinNoise} fails if the event
 *       machinery measurably perturbs the scheduler fast path.
 * </ul>
 */
public final class JfrEventsTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static Recording newRecording() {
        JfrEvents.ensureRegistered();
        Recording rec = new Recording();
        enable(rec, "graalgo.GoroutineStart");
        enable(rec, "graalgo.GoroutineEnd");
        enable(rec, "graalgo.GoroutinePark");
        enable(rec, "graalgo.ChannelBlock");
        return rec;
    }

    private static void enable(Recording rec, String name) {
        // No stack traces: these are high-rate scheduler events; stacks
        // would dominate the recording and the test runtime.
        rec.enable(name).with("stackTrace", "false");
    }

    private static Path dumpToTmp(Recording rec) throws Exception {
        Path dir = Path.of(System.getProperty("java.io.tmpdir"));
        Path p = Files.createTempFile(dir, "graalgo-jfr-", ".jfr");
        rec.dump(p);
        return p;
    }

    private static Map<String, List<RecordedEvent>> readAll(Path p) throws Exception {
        Map<String, List<RecordedEvent>> byName = new HashMap<>();
        for (RecordedEvent e : RecordingFile.readAllEvents(p)) {
            byName.computeIfAbsent(e.getEventType().getName(), k -> new ArrayList<>()).add(e);
        }
        return byName;
    }

    private String getString(RecordedEvent e, String field) {
        Object v = e.getValue(field);
        return v == null ? null : String.valueOf(v);
    }

    private long getLong(RecordedEvent e, String field) {
        Object v = e.getValue(field);
        if (v instanceof Number n) {
            return n.longValue();
        }
        throw new AssertionError("event field " + field + " is not numeric: " + v);
    }

    /**
     * Runs a channel+timer workload under a JFR recording and asserts the
     * expected graalgo events flow with well-formed fields.
     */
    public void testEventsFlow() throws Exception {
        SchedulerCore sched = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        Path recFile;
        try (Recording rec = newRecording()) {
            rec.enable("jdk.VirtualThreadStart").with("stackTrace", "false");
            rec.start();
            try {
                ChanOps.Chan ch = ChanOps.makeChan(0, 0, null);
                // g1 blocks in recv until g2's delayed send.
                sched.spawn(() -> {
                    Object v = ChanOps.recv(ch);
                    if (!"ping".equals(String.valueOf(v))) {
                        throw new AssertionError("unexpected value: " + v);
                    }
                });
                // g2 waits for g1 to be parked (deterministic probe), holds
                // the block long enough to clear the park threshold, sends.
                // The probe is sleep-paced (1ms): a yield-spin hammers the
                // channel monitor and pins carriers via LockSupport.park
                // in awaitAck, starving on a 2-core VM (reviewer2 P1-1).
                sched.spawn(() -> {
                    long deadline = System.currentTimeMillis() + 10000;
                    while (((graalgo.runtime.GoChan) ch).recvWaitersCount() == 0) {
                        if (System.currentTimeMillis() > deadline) {
                            throw new AssertionError("g1 never parked on the channel");
                        }
                        try {
                            Thread.sleep(1);
                        } catch (InterruptedException ie) {
                            Thread.currentThread().interrupt();
                        }
                    }
                    try {
                        Thread.sleep(250);
                    } catch (InterruptedException ie) {
                        Thread.currentThread().interrupt();
                    }
                    ChanOps.send(ch, "ping");
                });
                // g3 sleeps: TIMER park.
                sched.spawn(() -> Timers.sleep(250_000_000L));
                awaitQuiescence(sched, 15000);
            } finally {
                rec.stop();
            }
            recFile = dumpToTmp(rec);
        } finally {
            SchedulerCore.install(null);
        }

        Map<String, List<RecordedEvent>> byName = readAll(recFile);

        List<RecordedEvent> starts = byName.getOrDefault("graalgo.GoroutineStart", List.of());
        List<RecordedEvent> ends = byName.getOrDefault("graalgo.GoroutineEnd", List.of());
        assertTrue(starts.size() >= 3, "expected >=3 GoroutineStart, got " + starts.size());
        assertTrue(ends.size() >= 3, "expected >=3 GoroutineEnd, got " + ends.size());

        // Every end matches a start (no orphan ends).
        Set<Long> startIds = new HashSet<>();
        for (RecordedEvent e : starts) {
            long id = getLong(e, "goroutineId");
            assertTrue(id > 0, "goroutineId must be positive");
            String tn = getString(e, "threadName");
            assertTrue(tn != null && tn.equals("goroutine-" + id),
                    "threadName must be goroutine-<id>, got " + tn);
            startIds.add(id);
        }
        for (RecordedEvent e : ends) {
            long id = getLong(e, "goroutineId");
            assertTrue(startIds.contains(id), "orphan GoroutineEnd for id " + id);
            assertTrue(getLong(e, "lifetimeNanos") > 0, "lifetime must be positive");
        }

        // ChannelBlock: exactly one RECV (g1); the send completed via
        // direct handoff and must NOT emit.
        List<RecordedEvent> blocks = byName.getOrDefault("graalgo.ChannelBlock", List.of());
        long recvBlocks = blocks.stream()
                .filter(e -> "RECV".equals(getString(e, "operation"))).count();
        long sendBlocks = blocks.stream()
                .filter(e -> "SEND".equals(getString(e, "operation"))).count();
        assertTrue(recvBlocks == 1, "expected exactly 1 RECV ChannelBlock, got " + recvBlocks);
        assertTrue(sendBlocks == 0, "send must not block (direct handoff), got " + sendBlocks);
        for (RecordedEvent e : blocks) {
            assertTrue(getLong(e, "channelId") > 0, "channelId must be positive");
            assertTrue(startIds.contains(getLong(e, "goroutineId")), "block has unknown goroutine");
        }

        // Park events: CHANNEL (g1, ~250ms) and TIMER (g3, ~250ms) must be
        // present; quantum parks (~us) must be filtered. The long-park
        // bound is a loose magnitude check (reviewer2 P1-1): the workload
        // holds each block ~250ms, and 50ms tolerates VM timing distortion
        // while still catching unit bugs (ms-vs-ns confusion). At least
        // one long park per reason is required — individual parks may be
        // cut short by early re-admission under carrier starvation (open
        // scheduler question, not a JFR defect: the hook measures the
        // true notePark-to-resume interval).
        List<RecordedEvent> parks = byName.getOrDefault("graalgo.GoroutinePark", List.of());
        boolean sawLongChannel = false;
        boolean sawLongTimer = false;
        for (RecordedEvent e : parks) {
            String reason = getString(e, "reason");
            long dur = getLong(e, "durationNanos");
            assertTrue(dur >= JfrEvents.PARK_THRESHOLD_NANOS,
                    "park event below threshold slipped through: " + dur);
            assertTrue(startIds.contains(getLong(e, "goroutineId")), "park has unknown goroutine");
            if ("CHANNEL".equals(reason) && dur >= 50_000_000L) {
                sawLongChannel = true;
            } else if ("TIMER".equals(reason) && dur >= 50_000_000L) {
                sawLongTimer = true;
            }
        }
        assertTrue(sawLongChannel, "no long CHANNEL park event");
        assertTrue(sawLongTimer, "no long TIMER park event");

        // JDK mapping: jdk.VirtualThreadStart fires for our vthreads and
        // correlates via the thread name.
        List<RecordedEvent> vtStarts = byName.getOrDefault("jdk.VirtualThreadStart", List.of());
        assertTrue(!vtStarts.isEmpty(), "no jdk.VirtualThreadStart events");
    }

    /**
     * Blocking-I/O park renders as an interval spanning the syscall
     * (reviewer2 P2-7): begun at ioEnter, committed at ioExit. Asserts an
     * IO park event exists whose JFR start/end span the sleep (not a
     * point at the exit instant), and whose durationNanos field matches.
     * Red-green for P2-7: the old emit-at-exit code committed the event
     * at ioExit with a ~0 JFR interval.
     */
    public void testIoParkSpansSyscall() throws Exception {
        SchedulerCore sched = new SchedulerCore(2, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        Path recFile;
        try (Recording rec = newRecording()) {
            rec.start();
            try {
                AtomicBoolean entered = new AtomicBoolean();
                sched.spawn(() -> {
                    sched.ioEnter();
                    entered.set(true);
                    try {
                        Thread.sleep(150);
                    } catch (InterruptedException ie) {
                        Thread.currentThread().interrupt();
                    } finally {
                        sched.ioExit();
                    }
                });
                long deadline = System.currentTimeMillis() + 10000;
                while (!entered.get()) {
                    if (System.currentTimeMillis() > deadline) {
                        throw new AssertionError("goroutine never entered blocking I/O");
                    }
                    Thread.sleep(5);
                }
                awaitQuiescence(sched, 15000);
            } finally {
                rec.stop();
            }
            recFile = dumpToTmp(rec);
        } finally {
            SchedulerCore.install(null);
        }

        Map<String, List<RecordedEvent>> byName = readAll(recFile);
        List<RecordedEvent> parks = byName.getOrDefault("graalgo.GoroutinePark", List.of());
        List<RecordedEvent> ioParks = new ArrayList<>();
        for (RecordedEvent e : parks) {
            if ("IO".equals(getString(e, "reason"))) {
                ioParks.add(e);
            }
        }
        assertTrue(!ioParks.isEmpty(), "no IO park event");
        for (RecordedEvent e : ioParks) {
            long durField = getLong(e, "durationNanos");
            assertTrue(durField >= 100_000_000L, "IO durationNanos too short: " + durField);
            long jfrNanos = e.getDuration().toNanos();
            assertTrue(jfrNanos >= 100_000_000L,
                    "IO JFR interval does not span the syscall: " + jfrNanos + "ns");
        }
    }

    /**
     * With no recording enabling our events, the workload must leave no
     * graalgo events anywhere — the isEnabled() guards must hold.
     * (ensureRegistered() is called before start so the test does not
     * depend on the JDK picking up lazily-registered types into a
     * running recording — reviewer2 P2-5. Red-green is preserved: without
     * @Enabled(false), the bare recording would capture the events.)
     */
    public void testNoEventsWhenDisabled() throws Exception {
        JfrEvents.ensureRegistered();
        SchedulerCore sched = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        Path recFile;
        try (Recording rec = new Recording()) {
            // Deliberately NOT enabling graalgo.* — but the recording is
            // active, so a missing guard would still emit.
            rec.start();
            try {
                ChanOps.Chan ch = ChanOps.makeChan(0, 0, null);
                sched.spawn(() -> {
                    ChanOps.recv(ch);
                });
                sched.spawn(() -> {
                    long deadline = System.currentTimeMillis() + 10000;
                    while (((graalgo.runtime.GoChan) ch).recvWaitersCount() == 0) {
                        if (System.currentTimeMillis() > deadline) {
                            throw new AssertionError("g1 never parked on the channel");
                        }
                        try {
                            Thread.sleep(1);
                        } catch (InterruptedException ie) {
                            Thread.currentThread().interrupt();
                        }
                    }
                    ChanOps.send(ch, "x");
                });
                awaitQuiescence(sched, 15000);
            } finally {
                rec.stop();
            }
            recFile = dumpToTmp(rec);
        } finally {
            SchedulerCore.install(null);
        }
        Map<String, List<RecordedEvent>> byName = readAll(recFile);
        for (String name : byName.keySet()) {
            assertTrue(!name.startsWith("graalgo."),
                    "graalgo event emitted while disabled: " + name);
        }
    }

    /**
     * Quantum parks are the scheduler hot loop: a duration threshold
     * cannot filter them (measured 26% exceed 100µs), so they are
     * sampled 1-in-1024. Under an active recording with graalgo events
     * enabled, a 20000-park yield hot loop must produce a trickle of
     * sampled QUANTUM events — never a flood, never zero.
     */
    public void testHotLoopParkFilter() throws Exception {
        int goroutines = 4;
        int yields = 5000;
        Path recFile;
        SchedulerCore sched = new SchedulerCore(1, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try (Recording rec = newRecording()) {
            rec.start();
            try {
                runYielders(sched, goroutines, yields);
            } finally {
                rec.stop();
            }
            recFile = dumpToTmp(rec);
        } finally {
            SchedulerCore.install(null);
        }
        Map<String, List<RecordedEvent>> byName = readAll(recFile);
        List<RecordedEvent> parks = byName.getOrDefault("graalgo.GoroutinePark", List.of());
        // Deterministic sampling: spawn does one armPark, then each yield
        // does one quantumPark; notePark sees seq 2..5001, and seq
        // 1024/2048/3072/4096 hit the 1-in-1024 mask → 4 per goroutine =
        // 16 total. Bound generously for stray scheduler parks.
        assertTrue(parks.size() >= 12 && parks.size() <= 20,
                "expected ~16 sampled quantum parks, got " + parks.size());
        for (RecordedEvent e : parks) {
            assertTrue("QUANTUM".equals(getString(e, "reason")),
                    "hot-loop park has wrong reason: " + getString(e, "reason"));
            assertTrue(getLong(e, "durationNanos") > 0, "park duration must be positive");
        }
        // Sanity: the recording actually captured scheduler activity
        // (starts/ends are not sampled).
        assertTrue(!byName.getOrDefault("graalgo.GoroutineStart", List.of()).isEmpty(),
                "recording captured no GoroutineStart events");
    }

    /**
     * Disabled-path cost, measured directly: notePark + parkResumed on a
     * real handle, no recording active. Asserts the guard costs less than
     * two {@code System.nanoTime()} calls — a machine-independent
     * definition of "near-zero" for instrumentation whose job is
     * timestamping parks. (Absolute budgets don't port: this VM's clock
     * runs ~5x slow. {@code Event.isEnabled()} is ~8ns on the pinned
     * GraalVM 25 toolchain — called directly, no caching layer.)
     */
    public void testDisabledGuardCostNegligible() throws Exception {
        assertTrue(!JfrEvents.anyEnabled(), "test requires no active recording");
        // P2-M7: PinAudit runs this suite with a JFR recording active (for
        // jdk.VirtualThreadPinned). Any active recording perturbs the guard
        // timing; the measurement is only valid with JFR fully idle.
        if (!jdk.jfr.FlightRecorder.getFlightRecorder().getRecordings().isEmpty()) {
            System.out.println("SKIP testDisabledGuardCostNegligible: JFR recording active");
            return;
        }
        java.lang.reflect.Constructor<graalgo.sched.GoroutineHandle> ctor =
                graalgo.sched.GoroutineHandle.class.getDeclaredConstructor(Runnable.class);
        ctor.setAccessible(true);
        graalgo.sched.GoroutineHandle h = ctor.newInstance((Runnable) () -> {
        });
        int n = 1_000_000;
        // Cost of one clock read on this machine (reference unit).
        for (int i = 0; i < 100_000; i++) {
            System.nanoTime();
        }
        long t0 = System.nanoTime();
        long sink = 0;
        for (int i = 0; i < n; i++) {
            sink += System.nanoTime();
        }
        double nanoCost = (double) (System.nanoTime() - t0) / n;
        if (sink == 0) {
            throw new AssertionError("unreachable");
        }
        // Warmup: settle JIT/code-cache on the park path.
        for (int i = 0; i < 100_000; i++) {
            h.parkSeq.incrementAndGet(); // simulate armPark
            JfrEvents.notePark(h, ParkReason.QUANTUM);
            JfrEvents.parkResumed(h);
        }
        t0 = System.nanoTime();
        for (int i = 0; i < n; i++) {
            h.parkSeq.incrementAndGet(); // simulate armPark (scheduler bookkeeping)
            JfrEvents.notePark(h, ParkReason.QUANTUM);
            JfrEvents.parkResumed(h);
        }
        long fullNanos = System.nanoTime() - t0;
        // Subtract the scheduler's own bookkeeping (the atomic inc is not JFR's).
        t0 = System.nanoTime();
        for (int i = 0; i < n; i++) {
            h.parkSeq.incrementAndGet();
        }
        long incNanos = System.nanoTime() - t0;
        double guardCost = (double) (fullNanos - incNanos) / n;
        System.out.println("JFR disabled guard cost: " + guardCost + " ns/park"
                + " (nanoTime: " + nanoCost + " ns)");
        assertTrue(guardCost < 2 * nanoCost,
                "disabled guard too slow: " + guardCost + " ns/park vs nanoTime " + nanoCost + " ns");
    }

    /**
     * Loose end-to-end smoke: yield throughput with a recording active
     * (events enabled) vs no recording. The numbers are noisy (drainer
     * round-trip dominates, and this VM is shared with heavy compute);
     * median-of-3 rounds with adjacent disabled/enabled pairing keeps the
     * ratio stable against load drift, and the bound only catches gross
     * regressions. The precise claims are in testHotLoopParkFilter
     * (filter exactness) and testDisabledGuardCostNegligible (guard cost).
     */
    // NOTE (P2-M9): testYieldThroughputSmoke was REMOVED. A throughput
    // ratio cannot measure a ~0.5% JFR effect under ±10% scheduling noise:
    // it flaked ~40% in-suite (ratios 0.50–1.65) and neither 10x work nor
    // interleaved D/E legs made it deterministic. Replaced by
    // graalgo.sched.JfrParkCostTest, which measures the hook cost directly
    // (stable) and is strictly more sensitive (500ns budget vs a 0.80
    // ratio gate that could never catch a realistic regression).

    /** Spawns n yielders and waits for quiescence. */
    private void runYielders(SchedulerCore sched, int n, int yields) throws Exception {
        for (int i = 0; i < n; i++) {
            sched.spawn(() -> {
                for (int j = 0; j < yields; j++) {
                    graalgo.runtime.Scheduler.yield();
                }
            });
        }
        awaitQuiescence(sched, 120000);
    }

    private static void awaitQuiescence(SchedulerCore sched, long timeoutMs) throws Exception {
        long deadline = System.currentTimeMillis() + timeoutMs;
        while (sched.runnableCount() != 0) {
            if (System.currentTimeMillis() > deadline) {
                throw new AssertionError("quiescence timeout (lost wakeup?)");
            }
            Thread.sleep(5);
        }
    }
}
