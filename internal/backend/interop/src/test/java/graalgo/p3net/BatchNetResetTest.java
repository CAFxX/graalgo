package graalgo.p3net;

import java.nio.channels.Channel;

import graalgo.p3os.GoOs;
import graalgo.sched.SchedulerCore;
import graalgo.values.GoSlice;

/**
 * Tests for R2315-14-P1-2: batch-mode per-entry reset of net state.
 *
 * <p>In {@code graalgo-run --batch} mode each entry calls
 * {@link GoOs#init} (via {@code GoOsSurfaceInitializer}) before Go
 * initialization runs, and cookies recycle from 16 — but {@code init}
 * only cleared the {@code GoOs} handle table while {@link GoNet#STATES}
 * and the {@link GoNetpoll} descs survived. A listener left open by
 * entry N therefore kept its port bound into entry N+1, whose recycled
 * cookie 16 aliased the stale socket state ({@code EADDRINUSE} on the
 * second entry's {@code listen}), and abandoned poll descs leaked
 * selector registrations and channels.
 *
 * <p>Both tests drive the production path only — {@code GoOs.init}
 * twice, never {@code resetForTest} between entries — so they fail
 * while the reset is missing and pass once the init-time eviction
 * sweep (plus per-close {@code SockState} discard) lands.
 *
 * <p>All socket tests are loopback-only (127.0.0.1, ephemeral ports).
 */
public final class BatchNetResetTest {

    // ---- small helpers (mirroring GoNetpollTest's) ----

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

    /** Wire-format AF_INET 127.0.0.1:port. */
    private static GoSlice v4(int port) {
        // D5: []byte is byte[]-backed — GoNet.goBuf casts to byte[].
        byte[] a = new byte[7];
        a[0] = 2;
        a[1] = (byte) ((port >>> 8) & 0xFF);
        a[2] = (byte) (port & 0xFF);
        a[3] = 127;
        a[4] = 0;
        a[5] = 0;
        a[6] = 1;
        return new GoSlice(a, 0, 7, 7);
    }

    private static GoSlice buf(int n) {
        // D5: []byte is byte[]-backed.
        return new GoSlice(new byte[n], 0, n, n);
    }

    private static int boundPort(long cookie) {
        GoSlice b = buf(23);
        long r = GoNet.getsockname(cookie, b);
        assertTrue(r > 0, "getsockname ok: " + r);
        byte[] a = (byte[]) b.array();
        return ((a[1] & 0xFF) << 8) | (a[2] & 0xFF);
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

    // ---- tests ----

    /**
     * Entry 1 opens a listener and never closes it; entry 2 is just
     * {@code GoOs.init}. The init must evict the previous entry's
     * handles through the normal close path: the socket state is gone,
     * the poll desc is gone, the listener channel is closed — so entry
     * 2 rebinds the same port on the recycled cookie 16.
     *
     * <p>Pre-fix: the {@code sockState(16) == null} assertion fails
     * (stale LISTENING state survives {@code init}); the desc count
     * stays 1, the channel stays open, and the second {@code listen}
     * returns {@code -EADDRINUSE}.
     */
    public void testBatchInitEvictsStaleListener() throws Exception {
        SchedulerCore prev = setup();
        Channel entry1Channel = null;
        Channel entry2Channel = null;
        try {
            // Entry 1: socket + pollOpen (fork order) + bind + listen.
            long ls = GoNet.socket(2, 1, 0);
            assertEq(16, ls, "entry1 socket cookie");
            long ctx = GoNetpoll.pollOpen(ls);
            assertTrue(ctx != 0, "entry1 pollOpen, errno=" + GoNetpoll.pollOpenErrno());
            assertEq(0, GoNet.bind(ls, v4(0)), "entry1 bind ephemeral");
            assertEq(0, GoNet.listen(ls, 128), "entry1 listen");
            int port = boundPort(ls);
            assertTrue(port != 0, "listener got an ephemeral port");
            entry1Channel = GoOs.socketChannel(ls);
            assertTrue(entry1Channel != null && entry1Channel.isOpen(),
                    "entry1 listener channel open before entry2 init");
            assertTrue(GoNet.sockState(ls) != null, "entry1 sockState present");
            assertEq(1, GoNetpoll.descCount(), "entry1 one poll desc");

            // Entry 2: production path only — GoOs.init, NO resetForTest.
            GoOs.init(new String[] {"entry2"});
            assertTrue(GoNet.sockState(16) == null,
                    "entry2 init drops stale SockState for recycled cookie 16");
            assertEq(0, GoNetpoll.descCount(),
                    "entry2 init evicts the abandoned poll desc");
            assertTrue(!entry1Channel.isOpen(),
                    "entry2 init closes the previous entry's listener channel");

            // Entry 2 rebinds the same port on the recycled cookie.
            long ls2 = GoNet.socket(2, 1, 0);
            assertEq(16, ls2, "entry2 socket recycles cookie 16");
            assertEq(0, GoNet.bind(ls2, v4(port)), "entry2 bind same port");
            assertEq(0, GoNet.listen(ls2, 128), "entry2 listen same port");
            entry2Channel = GoOs.socketChannel(ls2);
            assertTrue(entry2Channel != null && entry2Channel.isOpen(),
                    "entry2 listener channel open");
            // Leave the entry-2 listener open on purpose: the NEXT
            // entry's init is what must evict it (covered by re-running
            // this test's entry-2 init above in the next run); close it
            // here via the normal path so the port is freed for the
            // rest of the suite.
            assertEq(0, GoOs.close(ls2), "entry2 close listener");
            entry2Channel = null;
        } finally {
            // Best effort: never leak a bound port into the next test,
            // whichever assertion failed (pre-fix the entry-1 channel is
            // still open here — close it directly).
            try {
                if (entry1Channel != null && entry1Channel.isOpen()) {
                    entry1Channel.close();
                }
            } catch (Exception ignored) {
                // Best effort.
            }
            try {
                if (entry2Channel != null && entry2Channel.isOpen()) {
                    entry2Channel.close();
                }
            } catch (Exception ignored) {
                // Best effort.
            }
            teardown(prev);
        }
    }

    /**
     * Closing a socket through {@code GoOs.close} drops its
     * {@code SockState}: a later trap on the recycled cookie must see
     * no state (EBADF via the {@code st == null} guard), never the
     * previous socket's bind/connect progress.
     *
     * <p>Pre-fix: {@code STATES} is never removed on close, so
     * {@code sockState} stays non-null after close.
     */
    public void testCloseDropsSockState() {
        SchedulerCore prev = setup();
        try {
            long s = GoNet.socket(2, 1, 0);
            assertEq(16, s, "socket cookie");
            assertTrue(GoNet.sockState(s) != null, "sockState present after socket()");
            assertEq(0, GoNet.bind(s, v4(0)), "bind");
            assertTrue(GoNet.sockState(s) != null, "sockState present after bind()");
            assertEq(0, GoOs.close(s), "os.close socket");
            assertTrue(GoNet.sockState(s) == null,
                    "close drops SockState for the cookie");
            assertTrue(GoOs.socketChannel(s) == null,
                    "close drops the handle too");
        } finally {
            teardown(prev);
        }
    }
}
