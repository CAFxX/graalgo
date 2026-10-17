package graalgo.p3net;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.frontend.FrontendException;
import graalgo.p3os.GoOs;
import graalgo.runtime.Linknames;
import graalgo.sched.SchedulerCore;
import graalgo.values.GoSlice;

/**
 * Tests for the P3-M9 netpoller ({@link GoNetpoll}) and socket traps
 * ({@link GoNet}) — RISK-M9-1 + RISK-M9-2.
 *
 * <p>All socket tests are loopback-only (127.0.0.1, ephemeral ports).
 * Waits run on real scheduler goroutines (installed
 * {@link SchedulerCore}), so park/unpark/wakeup paths are exercised
 * for real, not mocked.
 *
 * <p>verifies: RISK-M9-1 (wakeup path + deadlines), RISK-M9-2 (socket
 * surface + fd unification)
 */
public final class GoNetpollTest {

    // ---- small helpers ----

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long want, long got, String msg) {
        if (want != got) {
            throw new AssertionError(msg + ": want " + want + " got " + got);
        }
    }

    /** A fresh scheduler + clean net/os state; returns the previous scheduler. */
    private static SchedulerCore setup() {
        SchedulerCore sched = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        SchedulerCore prev = SchedulerCore.install(sched);
        sched.start();
        GoOs.init(new String[0]);
        GoNet.resetForTest();
        GoNetpoll.resetForTest();
        return prev;
    }

    private static void teardown(SchedulerCore prev) {
        GoNetpoll.resetForTest();
        GoNet.resetForTest();
        GoOs.init(new String[0]);
        SchedulerCore.install(prev);
    }

    /** Runs work on one goroutine, propagating failures; waits for quiescence. */
    private interface ThrowingSupplier<T> {
        T get() throws Throwable;
    }

    private static <T> T onGoroutine(SchedulerCore sched, ThrowingSupplier<T> work)
            throws Exception {
        AtomicReference<T> out = new AtomicReference<>();
        AtomicReference<Throwable> err = new AtomicReference<>();
        sched.spawn(() -> {
            try {
                out.set(work.get());
            } catch (Throwable t) {
                err.set(t);
            }
        });
        sched.awaitQuiescence();
        if (err.get() != null) {
            Throwable t = err.get();
            if (t instanceof Exception e) {
                throw e;
            }
            throw new RuntimeException(t);
        }
        return out.get();
    }

    /** Wire-format AF_INET 127.0.0.1:port. */
    private static GoSlice v4(int port) {
        int[] a = new int[7];
        a[0] = 2;
        a[1] = (port >>> 8) & 0xFF;
        a[2] = port & 0xFF;
        a[3] = 127;
        a[4] = 0;
        a[5] = 0;
        a[6] = 1;
        return new GoSlice(a, 0, 7, 7);
    }

    private static GoSlice bytes(String s) {
        byte[] b = s.getBytes(java.nio.charset.StandardCharsets.UTF_8);
        int[] a = new int[b.length];
        for (int i = 0; i < b.length; i++) {
            a[i] = b[i] & 0xFF;
        }
        return new GoSlice(a, 0, a.length, a.length);
    }

    private static GoSlice buf(int n) {
        return new GoSlice(new int[n], 0, n, n);
    }

    private static String str(GoSlice gs, int n) {
        int[] a = (int[]) gs.array();
        byte[] b = new byte[n];
        for (int i = 0; i < n; i++) {
            b[i] = (byte) a[gs.offset() + i];
        }
        return new String(b, java.nio.charset.StandardCharsets.UTF_8);
    }

    private static int boundPort(long cookie) {
        GoSlice b = buf(23);
        long r = GoNet.getsockname(cookie, b);
        assertTrue(r > 0, "getsockname ok: " + r);
        int[] a = (int[]) b.array();
        return ((a[1] & 0xFF) << 8) | (a[2] & 0xFF);
    }

    /** A loopback port with nothing listening (bind ephemeral, read, close). */
    private static int closedPort() throws Exception {
        java.net.ServerSocket ss = new java.net.ServerSocket(0);
        int port = ss.getLocalPort();
        ss.close();
        return port;
    }

    /** Sets up a loopback listener; returns {cookie, ctx, port}.
     *
     * <p>P1-2: drives the FORK's call order — {@code pollOpen} BEFORE
     * {@code bind}/{@code listen} ({@code fd_poll_runtime.go} is
     * unmodified, so {@code runtime_pollOpen} runs in {@code FD.Init}).
     * The convenient reverse order ({@code listen} then {@code pollOpen})
     * masks P0-1 (the listen channel swap orphaning the pollDesc) and
     * must never be used in test setups. */
    private static long[] listenLoopback() {
        long ls = GoNet.socket(2, 1, 0);
        assertTrue(ls >= 16, "socket cookie: " + ls);
        long ctx = openPollFirst(ls);
        assertEq(0, GoNet.setsockopt(ls, 1, 2, 1), "SO_REUSEADDR");
        assertEq(0, GoNet.bind(ls, v4(0)), "bind");
        assertEq(0, GoNet.listen(ls, 128), "listen");
        return new long[]{ls, ctx, boundPort(ls)};
    }

    /**
     * P1-2: {@code pollOpen} in the fork's position — immediately after
     * {@code socket()}, before {@code bind}/{@code listen}/{@code connect}.
     */
    private static long openPollFirst(long cookie) {
        long ctx = GoNetpoll.pollOpen(cookie);
        assertTrue(ctx != 0, "pollOpen (fork order), errno=" + GoNetpoll.pollOpenErrno());
        return ctx;
    }

    /** Dials 127.0.0.1:port; returns {cookie, ctx}. Drives EINPROGRESS via the poller.
     *
     * <p>P1-2: fork order — {@code pollOpen} BEFORE {@code connect}
     * (masks P0-2's stale {@code connecting} snapshot in reverse order).
     * On this machine loopback connects always take the async path, so
     * the OP_CONNECT arm is exercised every time. */
    private static long[] dialLoopback(int port) {
        long cs = GoNet.socket(2, 1, 0);
        assertTrue(cs >= 16, "socket cookie: " + cs);
        long ctx = openPollFirst(cs);
        long rc = GoNet.connect(cs, v4(port));
        if (rc == -GoNet.EINPROGRESS) {
            long w = GoNetpoll.pollWait(ctx, (long) 'w');
            assertEq(0, w, "connect wait");
            assertEq(0, GoNet.getsockopt(cs, 1, 4), "SO_ERROR after connect");
        } else {
            assertEq(0, rc, "connect immediate");
        }
        return new long[]{cs, ctx};
    }

    /** Accepts one connection, driving EAGAIN via the poller. */
    private static long acceptOne(long ls, long lctx) {
        for (;;) {
            long c = GoNet.accept(ls);
            if (c == -GoNet.EAGAIN) {
                long w = GoNetpoll.pollWait(lctx, (long) 'r');
                assertEq(0, w, "accept wait");
                continue;
            }
            assertTrue(c >= 16, "accept cookie: " + c);
            return c;
        }
    }

    /** Full close sequence for a poll-registered socket: unblock, close, os.close. */
    private static void closeSock(long cookie, long ctx) {
        GoNetpoll.pollUnblock(ctx);
        GoNetpoll.pollClose(ctx);
        assertEq(0, GoOs.close(cookie), "os.close socket");
    }

    // ---- tests ----

    public void testPollServerInitIdempotent() {
        SchedulerCore prev = setup();
        try {
            GoNetpoll.pollServerInit();
            GoNetpoll.pollServerInit();
            assertEq(0, GoNetpoll.descCount(), "no descs");
            assertTrue(GoNetpoll.lastPollerError() == null, "poller healthy");
        } finally {
            teardown(prev);
        }
    }

    /**
     * RV-P1-19 (predicate clause 5, gc p6 quirk): {@code pollServerInit}
     * wires the deadlock suppressor onto the installed scheduler once the
     * poller thread exists — the detector then suppresses while the
     * poller lives. {@code resetForTest} tears the poller down and the
     * suppression deactivates with it.
     */
    public void testPollServerInitWiresDeadlockSuppressor() {
        SchedulerCore prev = setup();
        try {
            SchedulerCore sched = SchedulerCore.current();
            assertTrue(!sched.deadlockSuppressed(), "no suppression before init");
            GoNetpoll.pollServerInit();
            assertTrue(GoNetpoll.pollerThreadStarted(), "poller thread exists");
            assertTrue(sched.deadlockSuppressed(), "suppressor wired and active");
        } finally {
            teardown(prev);
        }
    }

    public void testSocketBindListenAcceptConnect() throws Exception {
        SchedulerCore prev = setup();
        try {
            SchedulerCore sched = SchedulerCore.current();
            long[] srv = listenLoopback();
            long[] cli = onGoroutine(sched, () -> dialLoopback((int) srv[2]));
            long ss = onGoroutine(sched, () -> acceptOne(srv[0], srv[1]));
            long sctx = GoNetpoll.pollOpen(ss);
            assertTrue(sctx != 0, "pollOpen accepted");
            // Peer addresses agree (fd-domain unification: cookies resolve).
            GoSlice pb = buf(23);
            assertTrue(GoNet.getpeername(ss, pb) > 0, "getpeername accepted");
            int[] a = (int[]) pb.array();
            assertEq(2, a[0], "peer family");
            closeSock(ss, sctx);
            closeSock(cli[0], cli[1]);
            closeSock(srv[0], srv[1]);
        } finally {
            teardown(prev);
        }
    }

    /**
     * P0-1 red-green: the fork's order is {@code pollOpen} BEFORE
     * {@code listen} ({@code FD.Init} runs {@code runtime_pollOpen}).
     * {@code listen()} swaps the SocketChannel for a ServerSocketChannel
     * and closes the old one; without pollDesc migration the accept wait
     * parks forever on the dead key. Watchdogged: a hang fails the test
     * instead of hanging the suite.
     */
    public void testListenForkOrderAcceptWakes() throws Exception {
        SchedulerCore prev = setup();
        try {
            SchedulerCore sched = SchedulerCore.current();
            long ls = GoNet.socket(2, 1, 0);
            assertTrue(ls >= 16, "socket");
            long lctx = openPollFirst(ls); // fork order: BEFORE bind/listen
            assertEq(0, GoNet.setsockopt(ls, 1, 2, 1), "SO_REUSEADDR");
            assertEq(0, GoNet.bind(ls, v4(0)), "bind");
            assertEq(0, GoNet.listen(ls, 128), "listen");
            int port = boundPort(ls);
            // Park the accept waiter. NOTE: the dial below is spawned
            // manually, NOT via onGoroutine: onGoroutine awaits quiescence
            // (liveGoroutines == 0), which a never-waking accept waiter
            // would block forever — the P0-1 failure must surface at the
            // watchdog below, not inside the helper.
            AtomicLong wres = new AtomicLong(-99);
            sched.spawn(() -> wres.set(GoNetpoll.pollWait(lctx, (long) 'r')));
            Thread.sleep(150); // let it park
            AtomicReference<long[]> cliBox = new AtomicReference<>();
            AtomicReference<Throwable> cliErr = new AtomicReference<>();
            sched.spawn(() -> {
                try {
                    cliBox.set(dialLoopback(port));
                } catch (Throwable t) {
                    cliErr.set(t);
                }
            });
            long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(10);
            while (wres.get() == -99 && System.nanoTime() < deadline) {
                Thread.sleep(20);
            }
            assertEq(0, wres.get(), "accept wait wakes (P0-1: channel migration)");
            long d2 = System.nanoTime() + TimeUnit.SECONDS.toNanos(10);
            while (cliBox.get() == null && cliErr.get() == null && System.nanoTime() < d2) {
                Thread.sleep(20);
            }
            if (cliErr.get() != null) {
                throw new AssertionError("dial failed", cliErr.get());
            }
            assertTrue(cliBox.get() != null, "dial completed");
            long[] cli = cliBox.get();
            long ss = GoNet.accept(ls);
            assertTrue(ss >= 16, "accept cookie: " + ss);
            long sctx = GoNetpoll.pollOpen(ss);
            assertTrue(sctx != 0, "pollOpen accepted");
            closeSock(ss, sctx);
            closeSock(cli[0], cli[1]);
            closeSock(ls, lctx);
        } finally {
            teardown(prev);
        }
    }

    /**
     * P0-2 red-green: the fork's order is {@code pollOpen} BEFORE
     * {@code connect}. A refused connect takes the async path
     * ({@code -EINPROGRESS}); the waiter must arm OP_CONNECT so
     * {@code finishConnect()} runs and stashes the error. With the stale
     * {@code pollOpen} snapshot the poller armed OP_WRITE instead: the
     * waiter woke with NO_ERROR but SO_ERROR stayed 0 and the state
     * stayed CONNECTING (and on success the first I/O would throw
     * NotYetConnectedException).
     */
    public void testDialForkOrderRefusedConnectReportsError() throws Exception {
        SchedulerCore prev = setup();
        try {
            SchedulerCore sched = SchedulerCore.current();
            int port = closedPort();
            long cs = GoNet.socket(2, 1, 0);
            assertTrue(cs >= 16, "socket");
            long cctx = openPollFirst(cs); // fork order: BEFORE connect
            long rc = GoNet.connect(cs, v4(port));
            assertEq(-GoNet.EINPROGRESS, rc, "refused connect is async");
            AtomicLong wres = new AtomicLong(-99);
            sched.spawn(() -> wres.set(GoNetpoll.pollWait(cctx, (long) 'w')));
            long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(10);
            while (wres.get() == -99 && System.nanoTime() < deadline) {
                Thread.sleep(20);
            }
            assertEq(0, wres.get(), "connect wait wakes");
            // finishConnect() must have run: error stashed, state moved.
            assertEq(GoNet.ECONNREFUSED, GoNet.getsockopt(cs, 1, 4),
                    "SO_ERROR ECONNREFUSED");
            assertTrue(GoNet.sockState(cs).kind == GoNet.SockStateKind.CONNECT_ERROR,
                    "state CONNECT_ERROR, was " + GoNet.sockState(cs).kind);
            closeSock(cs, cctx);
        } finally {
            teardown(prev);
        }
    }

    /**
     * P0-3 red-green: the fork calls {@code runtime_pollUnblock} then
     * {@code runtime_pollClose} back-to-back. A reader woken by unblock
     * but not yet resumed by the drainer (gc's {@code pdReady}) must not
     * make {@code pollClose} throw — the waiter later resumes and
     * observes CLOSING itself.
     */
    public void testUnblockThenCloseOverParkedReader() throws Exception {
        SchedulerCore prev = setup();
        try {
            SchedulerCore sched = SchedulerCore.current();
            long[] srv = listenLoopback();
            long[] cli = onGoroutine(sched, () -> dialLoopback((int) srv[2]));
            long ss = onGoroutine(sched, () -> acceptOne(srv[0], srv[1]));
            long sctx = GoNetpoll.pollOpen(ss);
            AtomicLong res = new AtomicLong(-99);
            sched.spawn(() -> res.set(GoNetpoll.pollWait(sctx, (long) 'r')));
            Thread.sleep(200); // let it park; NO awaitQuiescence (slot must stay claimed)
            GoNetpoll.pollUnblock(sctx);
            GoNetpoll.pollClose(sctx); // pre-fix: throws IllegalStateException
            long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(10);
            while (res.get() == -99 && System.nanoTime() < deadline) {
                Thread.sleep(20);
            }
            assertEq(GoNetpoll.POLL_ERR_CLOSING, res.get(), "woken reader sees CLOSING");
            assertEq(0, GoOs.close(ss), "os.close");
            assertEq(0, GoOs.close(cli[0]), "close client");
            closeSock(srv[0], srv[1]);
        } finally {
            teardown(prev);
        }
    }

    /**
     * P1-1 red-green for the R1 stale-timer fix: a stale deadline
     * callback (id A) firing after the deadline was re-armed (id B) must
     * neither clobber B's id nor wake a parked waiter; the live callback
     * (id B) must deliver exactly one TIMEOUT and clear the id.
     *
     * <p>The waiter parks on a connected (but data-less) socket: an
     * unconnected socket is poll-readable by kernel semantics, so it
     * would wake immediately with NO_ERROR and prove nothing.
     */
    public void testStaleTimerCallbackIgnored() throws Exception {
        SchedulerCore prev = setup();
        try {
            SchedulerCore sched = SchedulerCore.current();
            long[] srv = listenLoopback();
            long[] cli = onGoroutine(sched, () -> dialLoopback((int) srv[2]));
            long ss = onGoroutine(sched, () -> acceptOne(srv[0], srv[1]));
            long ctx = GoNetpoll.pollOpen(ss);
            assertTrue(ctx != 0, "pollOpen accepted");
            // Arm a far-future read deadline (timer id A), then re-arm
            // (timer id B; A is cancelled lazily).
            GoNetpoll.pollSetDeadline(ctx, 60_000_000_000L, (long) 'r');
            long idA = GoNetpoll.readTimerIdForTest(ctx);
            assertTrue(idA >= 0, "timer A armed, id=" + idA);
            GoNetpoll.pollSetDeadline(ctx, 60_000_000_000L, (long) 'r');
            long idB = GoNetpoll.readTimerIdForTest(ctx);
            assertTrue(idB >= 0 && idB != idA, "timer B rearmed, id=" + idB);
            // Park a reader under deadline B (connected, no data: stays parked).
            AtomicLong res = new AtomicLong(-99);
            CountDownLatch done = new CountDownLatch(1);
            sched.spawn(() -> {
                res.set(GoNetpoll.pollWait(ctx, (long) 'r'));
                done.countDown();
            });
            Thread.sleep(200); // let it park
            // Stale callback A fires late (its TimerHeap.cancel lost the race).
            GoNetpoll.fireDeadlineForTest(ctx, 'r', idA);
            assertEq(idB, GoNetpoll.readTimerIdForTest(ctx),
                    "stale callback must not clobber rearmed id");
            assertTrue(!done.await(150, TimeUnit.MILLISECONDS),
                    "stale callback must not wake the waiter");
            // Live callback B fires: exactly one TIMEOUT, id cleared.
            GoNetpoll.fireDeadlineForTest(ctx, 'r', idB);
            assertTrue(done.await(10, TimeUnit.SECONDS), "live deadline wakes the waiter");
            assertEq(GoNetpoll.POLL_ERR_TIMEOUT, res.get(), "waiter sees TIMEOUT");
            assertEq(-1, GoNetpoll.readTimerIdForTest(ctx), "id cleared after live fire");
            closeSock(ss, ctx);
            closeSock(cli[0], cli[1]);
            closeSock(srv[0], srv[1]);
        } finally {
            teardown(prev);
        }
    }

    /**
     * P2-4: {@code shutdown} with an invalid {@code how} must return
     * {@code -EINVAL} (previously fell through to 0); shutdown on a
     * connected datagram is {@code -EOPNOTSUPP} (legal on Linux, no JDK
     * API), not the previous wrong {@code -ENOTCONN}.
     */
    public void testShutdownValidatesHow() throws Exception {
        SchedulerCore prev = setup();
        try {
            long s = GoNet.socket(2, 1, 0);
            assertTrue(s >= 16, "socket");
            assertEq(-GoOs.EINVAL, GoNet.shutdown(s, 99), "how=99 -> EINVAL");
            assertEq(-GoOs.EINVAL, GoNet.shutdown(s, -1), "how=-1 -> EINVAL");
            assertEq(0, GoOs.close(s), "close");
            long d = GoNet.socket(2, 2, 0);
            assertTrue(d >= 16, "dgram socket");
            assertEq(-GoOs.EINVAL, GoNet.shutdown(d, 7), "how=7 on dgram -> EINVAL");
            assertEq(-GoNet.ENOTCONN, GoNet.shutdown(d, 0), "unconnected dgram -> ENOTCONN");
            assertEq(0, GoNet.connect(d, v4(closedPort())), "dgram connect instant");
            assertEq(-GoNet.EOPNOTSUPP, GoNet.shutdown(d, 0),
                    "connected dgram -> EOPNOTSUPP");
            assertEq(0, GoOs.close(d), "close dgram");
        } finally {
            teardown(prev);
        }
    }

    /**
     * E1 prototype: single-socket loopback echo. Measures the
     * <em>poller-side</em> wakeup path (epoll event → wakeup CAS-win, via
     * {@link GoNetpoll#lastWakeNanosForTest}) — the component this worker
     * owns — against the 38µs cold-drainer-wakeup budget. Waiter-side
     * carrier scheduling is the scheduler's domain and is load-noisy;
     * the committed gate here is a hang/regression catcher, while the
     * quiet-machine E1 number is recorded in the worker checkpoint.
     */
    public void testEchoWakeupLatency() throws Exception {
        SchedulerCore prev = setup();
        try {
            SchedulerCore sched = SchedulerCore.current();
            long[] pollerLat = new long[3];
            for (int iter = 0; iter < 3; iter++) {
                long[] srv = listenLoopback();
                long[] cli = onGoroutine(sched, () -> dialLoopback((int) srv[2]));
                long ss = onGoroutine(sched, () -> acceptOne(srv[0], srv[1]));
                long sctx = GoNetpoll.pollOpen(ss);
                AtomicLong tWriteDone = new AtomicLong();
                AtomicReference<String> got = new AtomicReference<>();
                sched.spawn(() -> {
                    long w = GoNetpoll.pollWait(sctx, (long) 'r');
                    if (w != 0) {
                        got.set("WAIT=" + w);
                        return;
                    }
                    GoSlice b = buf(64);
                    long r = GoOs.read(ss, b);
                    got.set("n=" + (r - 1) + ":" + str(b, (int) (r - 1)));
                });
                Thread.sleep(100); // let the waiter park (cold path)
                GoSlice msg = bytes("hello");
                long w = GoOs.write(cli[0], msg);
                assertEq((long) msg.len() + 1, w, "write n+1");
                tWriteDone.set(System.nanoTime());
                sched.awaitQuiescence();
                assertTrue(("n=5:hello").equals(got.get()), "echo data: " + got.get());
                long wakeNanos = GoNetpoll.lastWakeNanosForTest(sctx);
                assertTrue(wakeNanos > 0, "wakeNanos recorded");
                pollerLat[iter] = wakeNanos - tWriteDone.get();
                closeSock(ss, sctx);
                closeSock(cli[0], cli[1]);
                closeSock(srv[0], srv[1]);
            }
            java.util.Arrays.sort(pollerLat);
            System.out.println("NOTE GoNetpollTest.testEchoWakeupLatency: poller-side wakeup ns="
                    + pollerLat[0] + "/" + pollerLat[1] + "/" + pollerLat[2]
                    + " (min/med/max; budget 38000ns; VM load-noisy)");
            assertTrue(pollerLat[2] < 100_000_000L,
                    "poller-side wakeup insanely slow: " + pollerLat[2] + "ns");
        } finally {
            teardown(prev);
        }
    }

    public void testDeadlineExpires() throws Exception {
        SchedulerCore prev = setup();
        try {
            SchedulerCore sched = SchedulerCore.current();
            long[] srv = listenLoopback();
            long[] cli = onGoroutine(sched, () -> dialLoopback((int) srv[2]));
            long ss = onGoroutine(sched, () -> acceptOne(srv[0], srv[1]));
            long sctx = GoNetpoll.pollOpen(ss);
            // Arm a 100ms read deadline; nothing will arrive.
            long t0 = System.nanoTime();
            Long res = onGoroutine(sched, () -> {
                GoNetpoll.pollSetDeadline(sctx, 100_000_000L, (long) 'r');
                return GoNetpoll.pollWait(sctx, (long) 'r');
            });
            long dt = System.nanoTime() - t0;
            assertEq(GoNetpoll.POLL_ERR_TIMEOUT, res, "wait result");
            assertTrue(dt >= 80_000_000L, "fired after deadline, dt=" + dt);
            assertTrue(dt < 5_000_000_000L, "no huge overshoot, dt=" + dt);
            // A second wait with no deadline re-armed still sees the
            // expired state (gc: pd.rd persists).
            Long res2 = onGoroutine(sched, () -> GoNetpoll.pollWait(sctx, (long) 'r'));
            assertEq(GoNetpoll.POLL_ERR_TIMEOUT, res2, "sticky expiry");
            // Disarm and verify a live wait works again.
            Long res3 = onGoroutine(sched, () -> {
                GoNetpoll.pollSetDeadline(sctx, 0L, (long) 'r');
                GoSlice msg = bytes("x");
                // Write from the test thread is racy; instead verify the
                // wait parks and we unblock it from here via close path.
                return GoNetpoll.pollReset(sctx, (long) 'r');
            });
            assertEq(0, res3, "reset after disarm");
            closeSock(ss, sctx);
            closeSock(cli[0], cli[1]);
            closeSock(srv[0], srv[1]);
        } finally {
            teardown(prev);
        }
    }

    public void testPastDeadlineUnblocksImmediately() throws Exception {
        SchedulerCore prev = setup();
        try {
            SchedulerCore sched = SchedulerCore.current();
            long[] srv = listenLoopback();
            long[] cli = onGoroutine(sched, () -> dialLoopback((int) srv[2]));
            long ss = onGoroutine(sched, () -> acceptOne(srv[0], srv[1]));
            long sctx = GoNetpoll.pollOpen(ss);
            long t0 = System.nanoTime();
            Long res = onGoroutine(sched, () -> {
                // d < 0: deadline in the past → immediate timeout, no park.
                GoNetpoll.pollSetDeadline(sctx, -1L, (long) 'r');
                return GoNetpoll.pollWait(sctx, (long) 'r');
            });
            long dt = System.nanoTime() - t0;
            assertEq(GoNetpoll.POLL_ERR_TIMEOUT, res, "past deadline");
            assertTrue(dt < 1_000_000_000L, "no park on past deadline, dt=" + dt);
            closeSock(ss, sctx);
            closeSock(cli[0], cli[1]);
            closeSock(srv[0], srv[1]);
        } finally {
            teardown(prev);
        }
    }

    public void testUnblockWakesWaiterWithClosing() throws Exception {
        SchedulerCore prev = setup();
        try {
            SchedulerCore sched = SchedulerCore.current();
            long[] srv = listenLoopback();
            long[] cli = onGoroutine(sched, () -> dialLoopback((int) srv[2]));
            long ss = onGoroutine(sched, () -> acceptOne(srv[0], srv[1]));
            long sctx = GoNetpoll.pollOpen(ss);
            AtomicReference<Long> res = new AtomicReference<>();
            sched.spawn(() -> res.set(GoNetpoll.pollWait(sctx, (long) 'r')));
            Thread.sleep(100); // let it park
            GoNetpoll.pollUnblock(sctx);
            sched.awaitQuiescence();
            assertEq(GoNetpoll.POLL_ERR_CLOSING, res.get(), "unblock result");
            // Double unblock fails fast (gc: throw).
            try {
                GoNetpoll.pollUnblock(sctx);
                throw new AssertionError("expected throw on double unblock");
            } catch (IllegalStateException e) {
                assertTrue(e.getMessage().contains("unblock on closing"),
                        "message: " + e.getMessage());
            }
            GoNetpoll.pollClose(sctx);
            assertEq(0, GoOs.close(ss), "close");
            assertEq(0, GoOs.close(cli[0]), "close client");
            GoNetpoll.pollUnblock(srv[1]);
            GoNetpoll.pollClose(srv[1]);
            assertEq(0, GoOs.close(srv[0]), "close listener");
        } finally {
            teardown(prev);
        }
    }

    /**
     * Red-green for the close-vs-wait race: if {@code pollUnblock} lands
     * after the pre-wait check but before the waiter claims its slot, the
     * waiter must return CLOSING, not park forever (no waker will ever
     * fire — the slot was empty when unblock ran). The pre-claim hook
     * forces this interleaving deterministically.
     */
    public void testUnblockBeforeClaimReturnsClosing() throws Exception {
        SchedulerCore prev = setup();
        try {
            SchedulerCore sched = SchedulerCore.current();
            long[] srv = listenLoopback();
            long[] cli = onGoroutine(sched, () -> dialLoopback((int) srv[2]));
            long ss = onGoroutine(sched, () -> acceptOne(srv[0], srv[1]));
            long sctx = GoNetpoll.pollOpen(ss);
            CountDownLatch inHook = new CountDownLatch(1);
            CountDownLatch release = new CountDownLatch(1);
            GoNetpoll.preClaimHookForTest = () -> {
                inHook.countDown();
                try {
                    if (!release.await(30, TimeUnit.SECONDS)) {
                        throw new AssertionError("hook release timed out");
                    }
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                    throw new AssertionError(e);
                }
            };
            AtomicLong res = new AtomicLong(-99);
            AtomicReference<Throwable> err = new AtomicReference<>();
            sched.spawn(() -> {
                try {
                    res.set(GoNetpoll.pollWait(sctx, (long) 'r'));
                } catch (Throwable t) {
                    err.set(t);
                }
            });
            assertTrue(inHook.await(30, TimeUnit.SECONDS), "waiter reached pre-claim hook");
            // Unblock while the waiter is held before the slot claim: the
            // slot is empty, so no wakeup can fire for this wait.
            GoNetpoll.pollUnblock(sctx);
            release.countDown();
            long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(30);
            while (res.get() == -99 && err.get() == null
                    && System.nanoTime() < deadline) {
                Thread.sleep(20);
            }
            GoNetpoll.preClaimHookForTest = null;
            if (err.get() != null) {
                throw new AssertionError("waiter threw", err.get());
            }
            assertTrue(res.get() != -99, "waiter parked forever: close-vs-wait race");
            assertEq(GoNetpoll.POLL_ERR_CLOSING, res.get(), "unblock-before-claim result");
            GoNetpoll.pollClose(sctx);
            assertEq(0, GoOs.close(ss), "close");
            assertEq(0, GoOs.close(cli[0]), "close client");
            GoNetpoll.pollUnblock(srv[1]);
            GoNetpoll.pollClose(srv[1]);
            assertEq(0, GoOs.close(srv[0]), "close listener");
        } finally {
            GoNetpoll.preClaimHookForTest = null;
            teardown(prev);
        }
    }

    public void testCloseWithoutUnblockFailsFast() {
        SchedulerCore prev = setup();
        try {
            long[] srv = listenLoopback();
            try {
                GoNetpoll.pollClose(srv[1]);
                throw new AssertionError("expected throw on close w/o unblock");
            } catch (IllegalStateException e) {
                assertTrue(e.getMessage().contains("w/o unblock"), "message: " + e.getMessage());
            }
            closeSock(srv[0], srv[1]);
        } finally {
            teardown(prev);
        }
    }

    public void testConcurrentWaitFailsFast() throws Exception {
        SchedulerCore prev = setup();
        try {
            SchedulerCore sched = SchedulerCore.current();
            long[] srv = listenLoopback();
            long[] cli = onGoroutine(sched, () -> dialLoopback((int) srv[2]));
            long ss = onGoroutine(sched, () -> acceptOne(srv[0], srv[1]));
            long sctx = GoNetpoll.pollOpen(ss);
            sched.spawn(() -> GoNetpoll.pollWait(sctx, (long) 'r')); // parks
            Thread.sleep(100);
            AtomicReference<Throwable> err = new AtomicReference<>();
            sched.spawn(() -> {
                try {
                    GoNetpoll.pollWait(sctx, (long) 'r');
                } catch (Throwable t) {
                    err.set(t);
                }
            });
            Thread.sleep(100);
            assertTrue(err.get() instanceof IllegalStateException,
                    "second waiter fails fast: " + err.get());
            // The first waiter is still parked; unblock to release it.
            GoNetpoll.pollUnblock(sctx);
            sched.awaitQuiescence();
            GoNetpoll.pollClose(sctx);
            assertEq(0, GoOs.close(ss), "close");
            assertEq(0, GoOs.close(cli[0]), "close client");
            closeSock(srv[0], srv[1]);
        } finally {
            teardown(prev);
        }
    }

    /**
     * UDP over connected datagrams (the fork's DialUDP path). Note: this
     * sandbox blocks unconnected sendto(2) with EPERM (verified at the
     * syscall level 2026-10-09), so the positive datagram path is
     * exercised connected; sendto is covered on its validation paths.
     */
    public void testUdpSendRecv() throws Exception {
        SchedulerCore prev = setup();
        try {
            SchedulerCore sched = SchedulerCore.current();
            long a = GoNet.socket(2, 2, 0);
            assertEq(0, GoNet.bind(a, v4(0)), "bind a");
            int portA = boundPort(a);
            long actx = GoNetpoll.pollOpen(a);
            long b = GoNet.socket(2, 2, 0);
            assertEq(0, GoNet.bind(b, v4(0)), "bind b");
            assertEq(0, GoNet.connect(b, v4(portA)), "dgram connect");
            long bctx = GoNetpoll.pollOpen(b);

            // Connected send goes through the unified os.write.
            GoSlice payload = bytes("udp-hello");
            long s = GoOs.write(b, payload);
            assertEq((long) payload.len() + 1, s, "dgram write n+1");

            AtomicReference<String> got = new AtomicReference<>();
            AtomicReference<String> peer = new AtomicReference<>();
            onGoroutine(sched, () -> {
                long w = GoNetpoll.pollWait(actx, (long) 'r');
                if (w != 0) {
                    return "WAIT=" + w;
                }
                GoSlice rb = buf(64);
                GoSlice ab = buf(23);
                long r = GoNet.recvfrom(a, rb, ab);
                int[] aa = (int[]) ab.array();
                peer.set("fam=" + aa[0]);
                got.set("n=" + (r - 1) + ":" + str(rb, (int) (r - 1)));
                return null;
            });
            assertTrue(("n=9:udp-hello").equals(got.get()), "udp data: " + got.get());
            assertTrue("fam=2".equals(peer.get()), "peer family: " + peer.get());

            // Nothing queued now: recvfrom → EAGAIN.
            assertEq(-GoNet.EAGAIN, GoNet.recvfrom(a, buf(64), buf(23)), "recvfrom EAGAIN");
            // sendto validation paths (no network use).
            assertEq(-GoOs.EBADF, GoNet.sendto(999999L, buf(8), buf(23)), "sendto bad cookie");
            long tcp = GoNet.socket(2, 1, 0);
            assertEq(-GoNet.EOPNOTSUPP, GoNet.sendto(tcp, buf(8), buf(23)),
                    "sendto on stream");
            assertEq(0, GoOs.close(tcp), "close tcp");
            closeSock(a, actx);
            closeSock(b, bctx);
        } finally {
            teardown(prev);
        }
    }

    /**
     * E3: wakeup latency at 1k connections. 1000 loopback pairs, one
     * parked reader each; the server writes 1 byte to all, then we
     * measure per-waiter wakeup latency (write-completion → wait-return).
     */
    /**
     * E3: wakeup latency at 1k connections. 1000 loopback pairs, one
     * parked reader each; the server writes 1 byte to all. Per-waiter
     * latency is poller-side (write-completion -> wakeup CAS-win, via
     * {@link GoNetpoll#lastWakeNanosForTest}), immune to waiter
     * carrier-scheduling noise. Dialing and accepting run concurrently
     * so the listen backlog (128) never overflows.
     */
    public void testWakeupLatency1k() throws Exception {
        SchedulerCore prev = setup();
        try {
            SchedulerCore sched = SchedulerCore.current();
            final int N = 1000;
            long[] srv = listenLoopback();
            int port = (int) srv[2];
            long[] cliCookie = new long[N];
            long[] cliCtx = new long[N];
            long[] srvCookie = new long[N];
            long[] srvCtx = new long[N];
            long[] writeNanos = new long[N];
            long[] enterNanos = new long[N];
            AtomicReference<Throwable> err = new AtomicReference<>();
            // Acceptor and dialer run concurrently (both on goroutines:
            // acceptOne/dialLoopback drive pollWait).
            sched.spawn(() -> {
                try {
                    for (int i = 0; i < N; i++) {
                        long ss = acceptOne(srv[0], srv[1]);
                        srvCookie[i] = ss;
                        srvCtx[i] = GoNetpoll.pollOpen(ss);
                    }
                } catch (Throwable t) {
                    err.compareAndSet(null, t);
                }
            });
            sched.spawn(() -> {
                try {
                    for (int i = 0; i < N; i++) {
                        long[] cli = dialLoopback(port);
                        cliCookie[i] = cli[0];
                        cliCtx[i] = cli[1];
                    }
                } catch (Throwable t) {
                    err.compareAndSet(null, t);
                }
            });
            sched.awaitQuiescence();
            if (err.get() != null) {
                throw new RuntimeException(err.get());
            }
            for (int i = 0; i < N; i++) {
                final int k = i;
                sched.spawn(() -> {
                    try {
                        enterNanos[k] = System.nanoTime();
                        long w = GoNetpoll.pollWait(cliCtx[k], (long) 'r');
                        if (w != 0) {
                            throw new AssertionError("wait=" + w);
                        }
                        GoSlice rb = buf(8);
                        long r = GoOs.read(cliCookie[k], rb);
                        if (r != 2) {
                            throw new AssertionError("read=" + r);
                        }
                    } catch (Throwable t) {
                        err.compareAndSet(null, t);
                    }
                });
            }
            // Barrier: every reader must have ENTERED its wait (about to
            // park) before the server writes; otherwise goroutine startup
            // latency (drainer admission backlog under load) would be
            // misattributed to the wakeup path.
            long barrier0 = System.nanoTime();
            for (;;) {
                boolean all = true;
                for (int i = 0; i < N; i++) {
                    if (enterNanos[i] == 0) {
                        all = false;
                        break;
                    }
                }
                if (all) {
                    break;
                }
                if (System.nanoTime() - barrier0 > 60_000_000_000L) {
                    throw new AssertionError("readers did not all enter wait");
                }
                Thread.sleep(10);
            }
            // Server writes 1 byte to every accepted socket, stamping each.
            GoSlice one = bytes("x");
            for (int i = 0; i < N; i++) {
                long w = GoOs.write(srvCookie[i], one);
                if (w != 2) {
                    throw new AssertionError("server write=" + w);
                }
                writeNanos[i] = System.nanoTime();
            }
            sched.awaitQuiescence();
            if (err.get() != null) {
                throw new RuntimeException(err.get());
            }
            long[] lat = new long[N];
            for (int i = 0; i < N; i++) {
                long wake = GoNetpoll.lastWakeNanosForTest(cliCtx[i]);
                assertTrue(wake > 0, "wakeNanos recorded for waiter " + i);
                // Wakeup latency = CAS-win minus the later of (write,
                // wait-entry): the epoll event is level-triggered, so a
                // write that lands before the park still wakes promptly.
                lat[i] = wake - Math.max(writeNanos[i], enterNanos[i]);
            }
            java.util.Arrays.sort(lat);
            long p50 = lat[N / 2];
            long p99 = lat[(int) (N * 0.99)];
            long max = lat[N - 1];
            System.out.println("NOTE GoNetpollTest.testWakeupLatency1k: n=1000 poller-side"
                    + " p50=" + p50 + "ns p99=" + p99 + "ns max=" + max + "ns");
            assertTrue(GoNetpoll.lastPollerError() == null,
                    "poller healthy: " + GoNetpoll.lastPollerError());
            assertTrue(p99 < 1_000_000_000L, "p99 insanely slow: " + p99 + "ns");
            for (int i = 0; i < N; i++) {
                GoNetpoll.pollUnblock(cliCtx[i]);
                GoNetpoll.pollClose(cliCtx[i]);
                GoOs.close(cliCookie[i]);
                GoNetpoll.pollUnblock(srvCtx[i]);
                GoNetpoll.pollClose(srvCtx[i]);
                GoOs.close(srvCookie[i]);
            }
            closeSock(srv[0], srv[1]);
            assertTrue(GoNetpoll.descCount() == 0, "all descs dropped");
        } finally {
            teardown(prev);
        }
    }

    /** Linkname table: the net symbols dispatch with the right arity. */
    public void testLinknamesRegistered() {
        SchedulerCore prev = setup();
        try {
            // pollServerInit dispatches (void → null).
            Object r = Linknames.dispatch("internal/poll.runtime_pollServerInit",
                    new Object[0]);
            assertTrue(r == null, "void dispatch → null");
            // Arity mismatch fails fast (never silently misroutes).
            try {
                Linknames.dispatch("internal/poll.runtime_pollWait", new Object[0]);
                throw new AssertionError("expected arity failure");
            } catch (FrontendException e) {
                assertTrue(e.getMessage().contains("takes 2 arguments"),
                        "arity message: " + e.getMessage());
            }
            try {
                Linknames.dispatch("graalgo.net.socket",
                        new Object[]{1L, 2L});
                throw new AssertionError("expected arity failure");
            } catch (FrontendException e) {
                assertTrue(e.getMessage().contains("takes 3 arguments"),
                        "arity message: " + e.getMessage());
            }
            try {
                Linknames.dispatch("internal/poll.noSuchSymbol", new Object[0]);
                throw new AssertionError("expected unknown-symbol failure");
            } catch (FrontendException e) {
                assertTrue(e.getMessage().contains("noSuchSymbol"),
                        "unknown message: " + e.getMessage());
            }
            // A full socket() round-trip through the linkname table.
            Object cookie = Linknames.dispatch("graalgo.net.socket",
                    new Object[]{2L, 1L, 0L});
            assertTrue(((Long) cookie) >= 16, "socket via table: " + cookie);
            Object rc = Linknames.dispatch("graalgo.net.bind",
                    new Object[]{cookie, v4(0)});
            assertEq(0, (Long) rc, "bind via table");
            Object closed = Linknames.dispatch("graalgo.os.close",
                    new Object[]{cookie});
            assertEq(0, (Long) closed, "close via table");
        } finally {
            teardown(prev);
        }
    }
}