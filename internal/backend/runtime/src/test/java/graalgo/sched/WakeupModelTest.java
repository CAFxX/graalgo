package graalgo.sched;

import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Deque;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * P2-M9: exhaustive interleaving explorer for the park/unpark protocol
 * (done-gate 4; verifies: GG-WAKE-0002).
 *
 * <h2>Why a bespoke explorer (tool-spike verdict)</h2>
 * The protocol's entire concurrency surface collapses to two things:
 * (1) the ORDER of token arrival at the drainer's MPSC inbox — the
 * drainer itself is single-threaded, every lane/inflight/parked mutation
 * happens under one lock; (2) the vthread-side armPark/acked generation
 * protocol (15 lines). TLA+ was rejected: it would re-encode the protocol
 * by hand, so a green TLC run verifies the encoding, not the shipped
 * code. Java PathFinder was rejected: it cannot execute JDK 25 bytecode
 * with virtual threads, and full-JVM interleaving exploration buys nothing
 * over token-order exploration here. This explorer drives the REAL
 * {@link Drainer} class through the existing {@code processOneForTest}
 * hook (route+admit+check = exactly the production drain step), so there
 * is zero abstraction gap on the drainer; the vthread side is modeled 1:1
 * from the production protocol. Re-runnable by a second reviewer via
 * {@code make test}.
 *
 * <h2>How it works</h2>
 * Each goroutine (plus a driver actor) runs a script of ops. The explorer
 * DFS-interleaves goroutine offers, driver offers, and drainer steps, with
 * a visited set over the full concrete state (program counters, inbox
 * contents, wait generations, acked values, drainer snapshot with
 * normalized handle ids). At every quiescent terminal state it asserts
 * the expected outcome per goroutine: DONE (woken and exited), WAIT
 * (legitimately parked forever — its unpark was never offered), or
 * RUNNING. A goroutine stuck in WAIT although its paired UNPARK was
 * offered is a LOST WAKEUP and fails the test with the full path.
 * A drainer invariant violation throws out of {@code processOneForTest}
 * and fails with the path.
 *
 * <h2>Soundness notes</h2>
 * <ul>
 *   <li>A DRAIN action = route+admit+check of exactly one inbox token,
 *       atomic. In production an offer can land between the drainer's
 *       take/route/admit; admit() only reads the lanes (never the inbox),
 *       so those interleavings are observationally equivalent to
 *       offer-before-DRAIN or offer-after-DRAIN, both of which the
 *       explorer generates.</li>
 *   <li>After every action, WAIT→RUNNING is settled for acked&gt;=gen.
 *       Production may delay the vthread's observation arbitrarily, but
 *       never act before it (the awaitAck loop); delayed observation is
 *       covered because the explorer can always schedule other actions
 *       first.</li>
 *   <li>Concurrent offers linearize through the LinkedBlockingQueue; the
 *       explorer covers every total order.</li>
 * </ul>
 *
 * <h2>Abstraction gap (what is NOT modeled)</h2>
 * Real LockSupport permit semantics are modeled as "awaitAck returns iff
 * acked&gt;=gen": GG-WAKE-0001 (ack-before-unpark) plus the loop re-check
 * means a permit can only ever cause an early return from park(), never a
 * missed wakeup — early unpark is absorbed by the permit, spurious wakeups
 * re-park. Real vthread/carrier scheduling, GC, and JIT are out of scope.
 * The channel/select/timer logic that DECIDES when to unpark is out of
 * scope: pairing is given by the scripts. {@code armed} is write-only
 * dead state (the spike-2b external-waker protocol was never implemented;
 * every wakeup routes through the drainer).
 */
public final class WakeupModelTest {

    // ================= script language =================

    enum Op { INIT, PARK, RESUME, IOENTER, IOEXIT, UNPARK, EXIT }

    /** One scripted action; {@code target} is the goroutine index for UNPARK. */
    static final class Step {
        final Op op;
        final int target;
        Step(Op op, int target) { this.op = op; this.target = target; }
        static Step s(Op op) { return new Step(op, -1); }
        static Step u(int target) { return new Step(Op.UNPARK, target); }
        @Override public String toString() {
            return op + (op == Op.UNPARK ? "(g" + target + ")" : "");
        }
    }

    enum Expect { DONE, WAIT, RUNNING }

    static final class Script {
        final String name;
        final int cap;
        /** Per-goroutine programs (INIT is auto-prepended by the harness). */
        final List<List<Step>> progs;
        /** Driver program (UNPARKs offered from outside any goroutine). */
        final List<Step> driver;
        final List<Expect> expect;
        Script(String name, int cap, List<List<Step>> progs,
               List<Step> driver, List<Expect> expect) {
            this.name = name;
            this.cap = cap;
            this.progs = progs;
            this.driver = driver;
            this.expect = expect;
        }
    }

    // ================= execution state =================

    /** Inbox token descriptor (handle referenced by goroutine index). */
    static final class Tok {
        final ResumeToken.Kind kind;
        final int g;
        final long gen;
        Tok(ResumeToken.Kind kind, int g, long gen) {
            this.kind = kind; this.g = g; this.gen = gen;
        }
        @Override public String toString() { return kind + "(g" + g + "," + gen + ")"; }
    }

    static final int RUNNING = 0, WAIT = 1, DONE = 2;

    static final class Exec {
        final SchedulerCore sched;
        final List<GoroutineHandle> handles;
        final Deque<Tok> inbox = new ArrayDeque<>();
        final List<List<Step>> fullProgs;
        final List<Step> driver;
        final int[] pc;
        final int[] st;
        final long[] waitGen;
        int driverPc;

        Exec(Script sc) {
            // Cheap and thread-free: Drainer/TimerHeap threads start only
            // via start(), which the explorer never calls.
            sched = new SchedulerCore(sc.cap, SchedulerCore.DEFAULT_QUANTUM_NANOS);
            handles = new ArrayList<>();
            for (int i = 0; i < sc.progs.size(); i++) {
                GoroutineHandle h = new GoroutineHandle(() -> {});
                h.thread = Thread.currentThread(); // model only; never really parked
                handles.add(h);
            }
            fullProgs = new ArrayList<>();
            for (List<Step> p : sc.progs) {
                List<Step> full = new ArrayList<>();
                full.add(Step.s(Op.INIT));
                full.addAll(p);
                fullProgs.add(full);
            }
            driver = sc.driver;
            int n = handles.size();
            pc = new int[n];
            st = new int[n]; // all RUNNING; first step (INIT) moves to WAIT
            waitGen = new long[n];
        }
    }

    /** Applies one action code: 0..n-1 goroutine, n driver, n+1 drain. */
    static void apply(Exec e, int code) {
        int n = e.handles.size();
        if (code == n + 1) {
            Tok t = e.inbox.pollFirst();
            e.sched.drainer().processOneForTest(
                    new ResumeToken(t.kind, e.handles.get(t.g), t.gen));
        } else if (code == n) {
            Step s = e.driver.get(e.driverPc++);
            offer(e, -1, s);
        } else {
            Step s = e.fullProgs.get(code).get(e.pc[code]++);
            offer(e, code, s);
        }
        // awaitAck spin, modeled: a waiter observes acked>=gen.
        for (int i = 0; i < n; i++) {
            if (e.st[i] == WAIT && e.handles.get(i).acked.get() >= e.waitGen[i]) {
                e.st[i] = RUNNING;
            }
        }
    }

    static void offer(Exec e, int actor, Step s) {
        int g = (s.op == Op.UNPARK) ? s.target : actor;
        GoroutineHandle h = e.handles.get(g);
        switch (s.op) {
            case INIT, PARK, RESUME, IOENTER, IOEXIT -> {
                long t = e.sched.armPark(h);
                ResumeToken.Kind k = switch (s.op) {
                    case INIT -> ResumeToken.Kind.INIT;
                    case PARK -> ResumeToken.Kind.PARKED;
                    case RESUME -> ResumeToken.Kind.RESUME;
                    case IOENTER -> ResumeToken.Kind.IO_ENTER;
                    case IOEXIT -> ResumeToken.Kind.IO_EXIT;
                    default -> throw new AssertionError("unreachable");
                };
                e.inbox.addLast(new Tok(k, g, t));
                e.waitGen[g] = t;
                e.st[g] = WAIT;
            }
            case UNPARK -> e.inbox.addLast(new Tok(ResumeToken.Kind.UNPARK, g, -1));
            case EXIT -> {
                e.inbox.addLast(new Tok(ResumeToken.Kind.EXIT, g, -1));
                e.st[g] = DONE;
            }
        }
    }

    /** Enabled action codes in canonical order. */
    static List<Integer> enabled(Exec e) {
        List<Integer> out = new ArrayList<>();
        int n = e.handles.size();
        for (int i = 0; i < n; i++) {
            if (e.st[i] == RUNNING && e.pc[i] < e.fullProgs.get(i).size()) {
                out.add(i);
            }
        }
        if (e.driverPc < e.driver.size()) {
            out.add(n);
        }
        if (!e.inbox.isEmpty()) {
            out.add(n + 1);
        }
        return out;
    }

    /** Full concrete-state signature for the visited set. */
    static String signature(Exec e) {
        StringBuilder sb = new StringBuilder();
        int n = e.handles.size();
        for (int i = 0; i < n; i++) {
            sb.append(e.pc[i]).append(',').append(e.st[i]).append(',')
              .append(e.waitGen[i]).append(';');
        }
        sb.append('d').append(e.driverPc).append(';');
        for (Tok t : e.inbox) {
            sb.append(t.kind).append(':').append(t.g).append(':').append(t.gen).append(';');
        }
        for (int i = 0; i < n; i++) {
            sb.append('a').append(e.handles.get(i).acked.get())
              .append('q').append(e.handles.get(i).parkSeq.get()).append(';');
        }
        Map<Long, String> names = new HashMap<>();
        for (int i = 0; i < n; i++) {
            names.put(e.handles.get(i).id, "g" + i);
        }
        sb.append(e.sched.drainer().snapshotForTest(names));
        return sb.toString();
    }

    static String renderPath(Script sc, List<Integer> path, List<Step> steps) {
        StringBuilder sb = new StringBuilder();
        int n = sc.progs.size();
        for (int i = 0; i < path.size(); i++) {
            int code = path.get(i);
            String who = code == n + 1 ? "DRAIN"
                    : code == n ? "drv:" + steps.get(i)
                    : "g" + code + ":" + steps.get(i);
            sb.append(who).append("  ");
        }
        return sb.toString();
    }

    // ================= DFS =================

    static final class Explorer {
        final Script sc;
        final Set<String> visited = new HashSet<>();
        final List<Integer> path = new ArrayList<>();
        final List<Step> pathSteps = new ArrayList<>();
        long states;
        long terminals;

        Explorer(Script sc) { this.sc = sc; }

        /** Replays the action path on a fresh scheduler (cheap, thread-free). */
        Exec execute() {
            Exec e = new Exec(sc);
            for (int code : path) {
                try {
                    apply(e, code);
                } catch (AssertionError ae) {
                    throw new AssertionError(
                            "drainer invariant violated at path: "
                            + renderPath(sc, path, pathSteps) + " :: " + ae.getMessage(), ae);
                } catch (RuntimeException re) {
                    throw new RuntimeException(
                            "drainer threw at path: "
                            + renderPath(sc, path, pathSteps), re);
                }
            }
            return e;
        }

        void dfs() {
            Exec e = execute();
            if (!visited.add(signature(e))) {
                return;
            }
            if (++states > 2_000_000) {
                throw new AssertionError(
                        "state explosion in script " + sc.name + " (>2M states); investigate");
            }
            List<Integer> en = enabled(e);
            if (en.isEmpty()) {
                terminals++;
                checkTerminal(e);
                return;
            }
            int n = sc.progs.size();
            for (int code : en) {
                Step step;
                if (code == n + 1) {
                    step = new Step(Op.INIT, -2); // marker for DRAIN
                } else if (code == n) {
                    step = sc.driver.get(e.driverPc);
                } else {
                    step = e.fullProgs.get(code).get(e.pc[code]);
                }
                path.add(code);
                pathSteps.add(step);
                dfs();
                path.remove(path.size() - 1);
                pathSteps.remove(pathSteps.size() - 1);
            }
        }

        void checkTerminal(Exec e) {
            for (int i = 0; i < sc.progs.size(); i++) {
                Expect actual = switch (e.st[i]) {
                    case DONE -> Expect.DONE;
                    case WAIT -> Expect.WAIT;
                    default -> Expect.RUNNING;
                };
                Expect want = sc.expect.get(i);
                if (actual != want) {
                    String kind = (actual == Expect.WAIT && want == Expect.DONE)
                            ? "LOST WAKEUP" : "unexpected terminal state";
                    throw new AssertionError(
                            kind + " in script " + sc.name + ": g" + i
                            + " terminal=" + actual + " expected=" + want
                            + "\npath: " + renderPath(sc, path, pathSteps)
                            + "\ndrainer: " + e.sched.drainer().snapshotForTest(null));
                }
            }
        }
    }

    static void explore(Script sc) {
        Explorer ex = new Explorer(sc);
        ex.dfs();
        System.out.println("  [model] " + sc.name + ": " + ex.states
                + " states, " + ex.terminals + " terminals, no lost wakeup");
    }

    // ================= scripts =================

    static Script script(String name, int cap, List<List<Step>> progs,
                         List<Step> driver, List<Expect> expect) {
        return new Script(name, cap, progs, driver, expect);
    }

    /** Basic park/unpark pairing (both token orders explored). */
    public void testModelBasicParkUnpark() {
        explore(script("basic", 2,
                List.of(List.of(Step.s(Op.PARK), Step.s(Op.EXIT))),
                List.of(Step.u(0)),
                List.of(Expect.DONE)));
    }

    /** Two goroutines, cross wakeups. */
    public void testModelCrossWake() {
        explore(script("cross", 2,
                List.of(List.of(Step.s(Op.PARK), Step.s(Op.EXIT)),
                        List.of(Step.s(Op.PARK), Step.s(Op.EXIT))),
                List.of(Step.u(0), Step.u(1)),
                List.of(Expect.DONE, Expect.DONE)));
    }

    /** Three goroutines, all orders. */
    public void testModelThreeWay() {
        explore(script("threeway", 2,
                List.of(List.of(Step.s(Op.PARK), Step.s(Op.EXIT)),
                        List.of(Step.s(Op.PARK), Step.s(Op.EXIT)),
                        List.of(Step.s(Op.PARK), Step.s(Op.EXIT))),
                List.of(Step.u(0), Step.u(1), Step.u(2)),
                List.of(Expect.DONE, Expect.DONE, Expect.DONE)));
    }

    /** Quantum park needs no unpark: the drainer re-admits via the run lane. */
    public void testModelQuantumNoUnpark() {
        explore(script("quantum", 1,
                List.of(List.of(Step.s(Op.RESUME), Step.s(Op.EXIT))),
                List.of(),
                List.of(Expect.DONE)));
    }

    /** Blocking-I/O pair: IO_ENTER acks synchronously, IO_EXIT re-admits. */
    public void testModelIoPair() {
        explore(script("io", 1,
                List.of(List.of(Step.s(Op.IOENTER), Step.s(Op.IOEXIT), Step.s(Op.EXIT))),
                List.of(),
                List.of(Expect.DONE)));
    }

    /**
     * One unpark, two parks: the unpark is consumed by the first park;
     * the second park waits legitimately (spurious-wakeup tolerance —
     * a stale unpark must not be duplicated, and the second park must
     * not be lost either: it is WAIT, not DONE, on every path).
     */
    public void testModelSingleUnparkDoublePark() {
        explore(script("unpark-consumed-once", 1,
                List.of(List.of(Step.s(Op.PARK), Step.s(Op.PARK))),
                List.of(Step.u(0)),
                List.of(Expect.WAIT)));
    }

    /**
     * cap=1 handoff: g0 parks, g1 is admitted, unparks g0 (WAKE waits for
     * the freed slot), parks itself, and is woken by the driver. Exercises
     * WAKE &gt; INIT lane priority under cap pressure.
     */
    public void testModelCapOneHandoff() {
        explore(script("cap1-handoff", 1,
                List.of(List.of(Step.s(Op.PARK), Step.s(Op.EXIT)),
                        List.of(Step.u(0), Step.s(Op.PARK), Step.s(Op.EXIT))),
                List.of(Step.u(1)),
                List.of(Expect.DONE, Expect.DONE)));
    }

    /**
     * Unpark for an exited goroutine: retained in unparkPending (bounded:
     * handle ids are unique, never reused), no hang, EXIT accounting
     * stays exact.
     */
    public void testModelExitWithPendingUnpark() {
        explore(script("exit-pending-unpark", 1,
                List.of(List.of(Step.s(Op.EXIT))),
                List.of(Step.u(0)),
                List.of(Expect.DONE)));
    }

    /**
     * Unpark racing a quantum park: the unpark is stashed; the RESUME is
     * admitted via the run lane; the stale unpark is retained (a later
     * park would wake spuriously — tolerated, never lost).
     */
    public void testModelUnparkDuringQuantum() {
        explore(script("unpark-during-quantum", 1,
                List.of(List.of(Step.s(Op.RESUME))),
                List.of(Step.u(0)),
                List.of(Expect.RUNNING)));
    }

    /**
     * GG-WAKE-0002(d): INIT lane under WAKE flood, cap=1. Two goroutines
     * park while a third is still awaiting first admission; WAKE tokens
     * must not starve or lose the queued INIT (WAKE &gt; INIT priority is
     * about order, not loss — every INIT is eventually admitted).
     *
     * <p>Boundary (reviewer1 P2-3): two unparks are pressure, not a flood;
     * fairness/starvation under an <em>infinite</em> WAKE flood and
     * RESUME-vs-WAKE/INIT lane contention under cap are out of
     * model-checker scope (unbounded scripts don't terminate) — covered
     * empirically by the P2-M8 torture battery instead.
     */
    public void testModelInitUnderWakeFlood() {
        explore(script("init-under-wake-flood", 1,
                List.of(List.of(Step.s(Op.PARK), Step.s(Op.EXIT)),
                        List.of(Step.s(Op.PARK), Step.s(Op.EXIT)),
                        List.of(Step.s(Op.EXIT))),
                List.of(Step.u(0), Step.u(1)),
                List.of(Expect.DONE, Expect.DONE, Expect.DONE)));
    }

    /**
     * Mixed: one goroutine parked on a channel while another does a
     * blocking-I/O round trip. Exercises the inIo set against the parked
     * map on all interleavings (the drainer fail-fasts on
     * ioEnter-while-parked / parked-while-in-io — any reachable
     * violation throws out of processOneForTest).
     */
    public void testModelMixedIoAndPark() {
        explore(script("mixed-io-park", 2,
                List.of(List.of(Step.s(Op.PARK), Step.s(Op.EXIT)),
                        List.of(Step.s(Op.IOENTER), Step.s(Op.IOEXIT), Step.s(Op.EXIT))),
                List.of(Step.u(0)),
                List.of(Expect.DONE, Expect.DONE)));
    }

    /**
     * Unpark arrives while the target is running, then the target does an
     * I/O round trip before parking: the stale unpark is retained through
     * IO_ENTER/IO_EXIT (which don't consult unparkPending) and consumed
     * by the later park — spurious wakeup, never lost.
     */
    public void testModelUnparkThenIoThenPark() {
        explore(script("unpark-io-park", 1,
                List.of(List.of(Step.s(Op.IOENTER), Step.s(Op.IOEXIT),
                        Step.s(Op.PARK), Step.s(Op.EXIT))),
                List.of(Step.u(0)),
                List.of(Expect.DONE)));
    }

    // ================= M6 watch-item: directed re-test =================

    /**
     * P2-M6 watch item re-test: the phantom "drainer re-admission with no
     * UNPARK in flight" was root-caused in P2-M8 as the unparkPending
     * fast-path (UNPARK offered before the PARKED token is processed).
     * This directed execution reproduces exactly that order and asserts
     * the admission is classified stashed — i.e. the model exhibits the
     * mechanism, and no OTHER path produces an admission without UNPARK.
     */
    public void testUnparkBeforeParkFastPath() {
        SchedulerCore sched = new SchedulerCore(2, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        GoroutineHandle h = new GoroutineHandle(() -> {});
        h.thread = Thread.currentThread();
        Drainer d = sched.drainer();
        Deque<ResumeToken> inbox = new ArrayDeque<>();
        // INIT + first admission.
        long t0 = sched.armPark(h);
        inbox.addLast(new ResumeToken(ResumeToken.Kind.INIT, h, t0));
        // The M6 order: UNPARK offered BEFORE the goroutine parks.
        inbox.addLast(new ResumeToken(ResumeToken.Kind.UNPARK, h, -1));
        long t1 = sched.armPark(h);
        inbox.addLast(new ResumeToken(ResumeToken.Kind.PARKED, h, t1));
        while (!inbox.isEmpty()) {
            d.processOneForTest(inbox.pollFirst());
        }
        assertTrue(h.acked.get() >= t1, "park must be admitted via stashed unpark");
        assertEquals(1, d.wakeUnparkStashed.get(), "exactly one stashed admission");
        assertEquals(0, d.wakeUnparkDirect.get(), "no direct admission on this path");
        assertEquals(0, d.wakeIoExit.get(), "no io admission on this path");
    }

    /**
     * Complement: PARKED processed before UNPARK takes the direct path.
     * Together the two directed tests prove the provenance counters
     * partition every WAKE admission, and the explorer above proves no
     * admission happens without an UNPARK/IO_EXIT behind it.
     */
    public void testParkBeforeUnparkDirectPath() {
        SchedulerCore sched = new SchedulerCore(2, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        GoroutineHandle h = new GoroutineHandle(() -> {});
        h.thread = Thread.currentThread();
        Drainer d = sched.drainer();
        Deque<ResumeToken> inbox = new ArrayDeque<>();
        long t0 = sched.armPark(h);
        inbox.addLast(new ResumeToken(ResumeToken.Kind.INIT, h, t0));
        long t1 = sched.armPark(h);
        inbox.addLast(new ResumeToken(ResumeToken.Kind.PARKED, h, t1));
        inbox.addLast(new ResumeToken(ResumeToken.Kind.UNPARK, h, -1));
        while (!inbox.isEmpty()) {
            d.processOneForTest(inbox.pollFirst());
        }
        assertTrue(h.acked.get() >= t1, "park must be admitted via direct unpark");
        assertEquals(1, d.wakeUnparkDirect.get(), "exactly one direct admission");
        assertEquals(0, d.wakeUnparkStashed.get(), "no stashed admission on this path");
    }

    /**
     * Duplicate unpark: TWO unparks for ONE park must produce exactly ONE
     * admission — the second is stashed, never duplicated. (Reviewer2
     * P1-1: the "never duplicated" half of the stale-wakeup property.)
     */
    public void testDuplicateUnparkAdmittedOnce() {
        SchedulerCore sched = new SchedulerCore(2, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        GoroutineHandle h = new GoroutineHandle(() -> {});
        h.thread = Thread.currentThread();
        Drainer d = sched.drainer();
        Deque<ResumeToken> inbox = new ArrayDeque<>();
        long t0 = sched.armPark(h);
        inbox.addLast(new ResumeToken(ResumeToken.Kind.INIT, h, t0));
        long t1 = sched.armPark(h);
        inbox.addLast(new ResumeToken(ResumeToken.Kind.PARKED, h, t1));
        inbox.addLast(new ResumeToken(ResumeToken.Kind.UNPARK, h, -1));
        inbox.addLast(new ResumeToken(ResumeToken.Kind.UNPARK, h, -1));
        while (!inbox.isEmpty()) {
            d.processOneForTest(inbox.pollFirst());
        }
        assertTrue(h.acked.get() >= t1, "park admitted");
        long wakeAdmissions = d.wakeUnparkDirect.get() + d.wakeUnparkStashed.get();
        assertEquals(1, wakeAdmissions, "exactly one WAKE admission for one park");
        assertEquals(1, d.wakeUnparkDirect.get(), "the matched unpark went direct");
        // The duplicate is retained (bounded: ids unique), not duplicated.
    }

    /**
     * L2: EXIT purges the handle's park bookkeeping. A goroutine that
     * was admitted, received a spurious UNPARK (stashed, never
     * consumed — it never parked), and then exits must not leave its
     * id in unparkPending forever: over a long run that set grows with
     * every such goroutine and pollutes snapshots. The same purge
     * covers the parked map (a PARKED record for a dead handle).
     */
    public void testExitPurgesUnparkPending() {
        SchedulerCore sched = new SchedulerCore(2, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        GoroutineHandle h = new GoroutineHandle(() -> {});
        h.thread = Thread.currentThread();
        Drainer d = sched.drainer();
        long t0 = sched.armPark(h);
        d.processOneForTest(new ResumeToken(ResumeToken.Kind.INIT, h, t0));
        d.processOneForTest(new ResumeToken(ResumeToken.Kind.UNPARK, h, -1));
        String before = d.snapshotForTest(null);
        assertTrue(before.contains("pending=[g" + h.id + "]"),
                "stashed unpark must be present before EXIT: " + before);
        d.processOneForTest(new ResumeToken(ResumeToken.Kind.EXIT, h, -1));
        String after = d.snapshotForTest(null);
        assertTrue(after.contains("pending=[]"),
                "EXIT must purge unparkPending: " + after);
        assertTrue(after.contains("parked={}"),
                "EXIT must purge parked: " + after);
    }

    /**
     * Stale unpark after re-arm: the goroutine completes a park/wake
     * cycle, then a STALE unpark arrives while it is RUNNING, then it
     * parks again. The stale unpark stashes and is consumed exactly once
     * by the re-armed park (spurious wakeup — tolerated, never lost,
     * never duplicated).
     */
    public void testStaleUnparkAfterRearm() {
        SchedulerCore sched = new SchedulerCore(2, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        GoroutineHandle h = new GoroutineHandle(() -> {});
        h.thread = Thread.currentThread();
        Drainer d = sched.drainer();
        Deque<ResumeToken> inbox = new ArrayDeque<>();
        long t0 = sched.armPark(h);
        inbox.addLast(new ResumeToken(ResumeToken.Kind.INIT, h, t0));
        // First park/wake cycle (re-arm).
        long t1 = sched.armPark(h);
        inbox.addLast(new ResumeToken(ResumeToken.Kind.PARKED, h, t1));
        inbox.addLast(new ResumeToken(ResumeToken.Kind.UNPARK, h, -1));
        // Stale unpark while RUNNING.
        inbox.addLast(new ResumeToken(ResumeToken.Kind.UNPARK, h, -1));
        // Re-armed park consumes the stash.
        long t2 = sched.armPark(h);
        inbox.addLast(new ResumeToken(ResumeToken.Kind.PARKED, h, t2));
        while (!inbox.isEmpty()) {
            d.processOneForTest(inbox.pollFirst());
        }
        assertTrue(h.acked.get() >= t2, "re-armed park admitted via stashed unpark");
        assertEquals(1, d.wakeUnparkDirect.get(), "first cycle went direct");
        assertEquals(1, d.wakeUnparkStashed.get(), "stale unpark consumed once via stash");
    }

    // ================= red-green guard =================

    /**
     * Mutation guard: the explorer must be capable of FAILING. A script
     * whose paired unpark is deliberately withheld must end WAIT, so
     * demanding DONE fails. (If this ever passes, the explorer is
     * vacuous.)
     */
    public void testModelDetectsWithheldUnpark() {
        Script sc = script("withheld", 1,
                List.of(List.of(Step.s(Op.PARK), Step.s(Op.EXIT))),
                List.of(), // unpark never offered
                List.of(Expect.DONE)); // wrong on purpose
        boolean failed = false;
        try {
            explore(sc);
        } catch (AssertionError ae) {
            failed = ae.getMessage().contains("LOST WAKEUP");
        }
        assertTrue(failed, "explorer must flag a park whose unpark never arrives as LOST WAKEUP");
    }

    // ================= helpers =================

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(long expected, long actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }
}
