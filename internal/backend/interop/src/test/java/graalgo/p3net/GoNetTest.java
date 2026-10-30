package graalgo.p3net;

import graalgo.p3os.GoOs;
import graalgo.values.GoSlice;

/**
 * Unit tests for the socket traps ({@link GoNet}) that need no scheduler
 * and no poller: socket lifecycle, bind/listen/accept state machine,
 * sockopt round-trips, and errno contracts. Poller-driven paths
 * (connect completion, deadlines, wakeups) live in
 * {@link GoNetpollTest}.
 *
 * <p>Backing note: {@code []byte} is byte[]-backed in canonical D5 form
 * (Boxing.newArray: UINT8 -&gt; byte[]). These tests therefore build every
 * address/buffer slice byte[]-backed. The sibling GoNetpollTest builds
 * int[]-backed slices and currently fails with ClassCastException on
 * exactly this boundary; that pre-existing test bug is reported
 * separately and is deliberately NOT repeated here.
 *
 * <p>All sockets are loopback-only (127.0.0.1, ephemeral ports).
 * Each test starts from a fresh GoOs handle table
 * ({@link GoOs#init}) plus {@link GoNet#resetForTest}, so cookies
 * never leak across tests.
 */
public final class GoNetTest {

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

    private static void setup() {
        GoOs.init(new String[0]);
        GoNet.resetForTest();
        GoNetpoll.resetForTest();
    }

    private static void teardown() {
        GoNetpoll.resetForTest();
        GoNet.resetForTest();
        GoOs.init(new String[0]);
    }

    /** Wire-format AF_INET 127.0.0.1:port, byte[]-backed. */
    private static GoSlice v4(int port) {
        byte[] a = new byte[7];
        a[0] = 2;
        a[1] = (byte) (port >>> 8);
        a[2] = (byte) port;
        a[3] = 127;
        a[4] = 0;
        a[5] = 0;
        a[6] = 1;
        return new GoSlice(a, 0, 7, 7);
    }

    private static GoSlice buf(int n) {
        return new GoSlice(new byte[n], 0, n, n);
    }

    private static byte[] bytesOf(GoSlice gs, int n) {
        byte[] arr = (byte[]) gs.array();
        byte[] out = new byte[n];
        System.arraycopy(arr, gs.offset(), out, 0, n);
        return out;
    }

    private static int boundPort(long cookie) {
        GoSlice b = buf(23);
        long r = GoNet.getsockname(cookie, b);
        assertTrue(r > 0, "getsockname ok: " + r);
        byte[] a = bytesOf(b, (int) (r - 1));
        return ((a[1] & 0xFF) << 8) | (a[2] & 0xFF);
    }

    private static int closedPort() throws Exception {
        java.net.ServerSocket ss = new java.net.ServerSocket(0);
        int port = ss.getLocalPort();
        ss.close();
        return port;
    }

    private static long listenLoopback() {
        long ls = GoNet.socket(2L, 1L, 0L);
        assertTrue(ls >= 16, "socket cookie: " + ls);
        assertEq(0, GoNet.setsockopt(ls, 1L, 2L, 1L), "SO_REUSEADDR");
        assertEq(0, GoNet.bind(ls, v4(0)), "bind");
        assertEq(0, GoNet.listen(ls, 128L), "listen");
        return ls;
    }

    public void testSocketBindListenAcceptLifecycle() {
        setup();
        try {
            long ls = listenLoopback();
            int port = boundPort(ls);
            assertTrue(port > 0, "ephemeral port assigned: " + port);
            // No connection pending: nonblocking accept reports EAGAIN.
            assertEq(-GoNet.EAGAIN, GoNet.accept(ls), "accept with none pending");
            assertEq(0, GoOs.close(ls), "close listener");
        } finally {
            teardown();
        }
    }

    public void testSocketRejectsUnsupportedFamilyAndType() {
        setup();
        try {
            assertEq(-GoNet.EAFNOSUPPORT, GoNet.socket(1L, 1L, 0L),
                    "AF_UNIX deferred");
            assertEq(-GoNet.EAFNOSUPPORT, GoNet.socket(2L, 3L, 0L),
                    "SOCK_RAW out of scope");
            assertEq(-GoNet.EAFNOSUPPORT, GoNet.socket(99L, 1L, 0L),
                    "unknown family");
            long s = GoNet.socket(2L, 1L, 0L);
            assertTrue(s >= 16, "AF_INET stream ok");
            assertEq(0, GoOs.close(s), "close");
        } finally {
            teardown();
        }
    }

    public void testBadCookieReturnsEbadf() {
        setup();
        try {
            long bad = 999999L;
            assertEq(-GoOs.EBADF, GoNet.bind(bad, v4(0)), "bind bad cookie");
            assertEq(-GoOs.EBADF, GoNet.getsockname(bad, buf(23)), "getsockname bad");
            assertEq(-GoOs.EBADF, GoNet.getpeername(bad, buf(23)), "getpeername bad");
            assertEq(-GoOs.EBADF, GoNet.getsockopt(bad, 1L, 2L), "getsockopt bad");
            assertEq(-GoOs.EBADF, GoNet.setsockopt(bad, 1L, 2L, 1L), "setsockopt bad");
            assertEq(-GoOs.EBADF, GoNet.listen(bad, 128L), "listen bad");
            assertEq(-GoOs.EBADF, GoNet.accept(bad), "accept bad");
            assertEq(-GoOs.EBADF, GoNet.connect(bad, v4(closedPort())), "connect bad");
            assertEq(-GoOs.EBADF, GoNet.shutdown(bad, 0L), "shutdown bad");
            assertEq(-GoOs.EBADF, GoNet.sendto(bad, buf(8), buf(23)), "sendto bad");
            assertEq(-GoOs.EBADF, GoNet.recvfrom(bad, buf(8), buf(23)), "recvfrom bad");
            assertEq(-GoOs.EBADF, GoOs.close(bad), "os.close bad");
        } catch (Exception e) {
            throw new AssertionError(e);
        } finally {
            teardown();
        }
    }

    public void testDoubleBindFailsAndDoubleCloseFails() {
        setup();
        try {
            long s = GoNet.socket(2L, 1L, 0L);
            assertEq(0, GoNet.bind(s, v4(0)), "first bind");
            assertEq(-GoOs.EINVAL, GoNet.bind(s, v4(0)), "second bind -> EINVAL");
            assertEq(0, GoOs.close(s), "first close");
            assertEq(-GoOs.EBADF, GoOs.close(s), "second close -> EBADF");
        } finally {
            teardown();
        }
    }

    public void testSecondListenFails() {
        setup();
        try {
            long ls = listenLoopback();
            // After listen() the channel is a ServerSocketChannel, so a
            // second listen() is EOPNOTSUPP, not a silent no-op.
            assertEq(-GoNet.EOPNOTSUPP, GoNet.listen(ls, 128L), "second listen");
            assertEq(0, GoOs.close(ls), "close");
        } finally {
            teardown();
        }
    }

    public void testSockoptRoundTrip() {
        setup();
        try {
            long s = GoNet.socket(2L, 1L, 0L);
            assertEq(0, GoNet.setsockopt(s, 1L, 2L, 1L), "set REUSEADDR 1");
            assertEq(1, GoNet.getsockopt(s, 1L, 2L), "get REUSEADDR 1");
            assertEq(0, GoNet.setsockopt(s, 1L, 2L, 0L), "set REUSEADDR 0");
            assertEq(0, GoNet.getsockopt(s, 1L, 2L), "get REUSEADDR 0");
            assertEq(1, GoNet.getsockopt(s, 1L, 3L), "SO_TYPE stream == 1");
            assertEq(0, GoNet.setsockopt(s, 6L, 1L, 1L), "set TCP_NODELAY");
            assertEq(1, GoNet.getsockopt(s, 6L, 1L), "get TCP_NODELAY");
            assertEq(-GoNet.ENOPROTOOPT, GoNet.getsockopt(s, 1L, 999L),
                    "unknown opt -> ENOPROTOOPT");
            assertEq(-GoNet.ENOPROTOOPT, GoNet.setsockopt(s, 99L, 1L, 1L),
                    "unknown level -> ENOPROTOOPT");
            assertEq(0, GoOs.close(s), "close stream");
            long d = GoNet.socket(2L, 2L, 0L);
            assertEq(2, GoNet.getsockopt(d, 1L, 3L), "SO_TYPE dgram == 2");
            assertEq(0, GoOs.close(d), "close dgram");
        } finally {
            teardown();
        }
    }

    public void testGetsocknameUnboundIsZeroAddr() {
        setup();
        try {
            long s = GoNet.socket(2L, 1L, 0L);
            GoSlice b = buf(23);
            long r = GoNet.getsockname(s, b);
            assertEq(8, r, "unbound getsockname returns 7+1");
            byte[] a = bytesOf(b, 7);
            assertEq(2, a[0] & 0xFF, "family AF_INET");
            assertEq(0, ((a[1] & 0xFF) << 8) | (a[2] & 0xFF), "port 0");
            assertEq(0, a[3] & 0xFF, "addr 0.0.0.0");
            assertEq(0, GoOs.close(s), "close");
        } finally {
            teardown();
        }
    }

    public void testGetpeernameUnconnectedIsEnotconn() {
        setup();
        try {
            long s = GoNet.socket(2L, 1L, 0L);
            assertEq(-GoNet.ENOTCONN, GoNet.getpeername(s, buf(23)),
                    "unconnected peer");
            assertEq(0, GoOs.close(s), "close");
        } finally {
            teardown();
        }
    }

    public void testAcceptOnNonListenerIsEinval() {
        setup();
        try {
            long s = GoNet.socket(2L, 1L, 0L);
            assertEq(-GoOs.EINVAL, GoNet.accept(s), "accept on plain socket");
            assertEq(0, GoOs.close(s), "close");
        } finally {
            teardown();
        }
    }

    public void testDatagramBindConnectAndSecondConnectIsEisconn() {
        setup();
        try {
            long a = GoNet.socket(2L, 2L, 0L);
            assertEq(0, GoNet.bind(a, v4(0)), "dgram bind");
            int portA = boundPort(a);
            assertTrue(portA > 0, "dgram port: " + portA);
            long b = GoNet.socket(2L, 2L, 0L);
            assertEq(0, GoNet.connect(b, v4(portA)), "dgram connect instant");
            assertEq(-GoNet.EISCONN, GoNet.connect(b, v4(portA)),
                    "second dgram connect -> EISCONN");
            assertEq(0, GoOs.close(a), "close a");
            assertEq(0, GoOs.close(b), "close b");
        } finally {
            teardown();
        }
    }

    public void testShutdownValidation() {
        setup();
        try {
            long s = GoNet.socket(2L, 1L, 0L);
            assertEq(-GoOs.EINVAL, GoNet.shutdown(s, 99L), "how=99 -> EINVAL");
            assertEq(-GoOs.EINVAL, GoNet.shutdown(s, -1L), "how=-1 -> EINVAL");
            long ls = listenLoopback();
            assertEq(-GoNet.ENOTCONN, GoNet.shutdown(ls, 2L),
                    "shutdown listener -> ENOTCONN");
            assertEq(0, GoOs.close(s), "close stream");
            assertEq(0, GoOs.close(ls), "close listener");
            long d = GoNet.socket(2L, 2L, 0L);
            assertEq(-GoNet.ENOTCONN, GoNet.shutdown(d, 0L),
                    "unconnected dgram -> ENOTCONN");
            assertEq(0, GoNet.connect(d, v4(closedPort())), "dgram connect instant");
            assertEq(-GoNet.EOPNOTSUPP, GoNet.shutdown(d, 0L),
                    "connected dgram -> EOPNOTSUPP");
            assertEq(0, GoOs.close(d), "close dgram");
        } catch (Exception e) {
            throw new AssertionError(e);
        } finally {
            teardown();
        }
    }

    public void testSetnonblock() {
        setup();
        try {
            long s = GoNet.socket(2L, 1L, 0L);
            assertEq(0, GoNet.setnonblock(s, 1L), "setnonblock ok");
            assertEq(-GoOs.EBADF, GoNet.setnonblock(999999L, 1L),
                    "setnonblock bad cookie");
            assertEq(0, GoOs.close(s), "close");
        } finally {
            teardown();
        }
    }

    public void testConnectToClosedPortIsEinprogress() {
        setup();
        try {
            int port = closedPort();
            long s = GoNet.socket(2L, 1L, 0L);
            // Nonblocking connect: the refusal surfaces only after the
            // poller runs finishConnect, so the synchronous result is
            // EINPROGRESS and no error is stashed yet.
            assertEq(-GoNet.EINPROGRESS, GoNet.connect(s, v4(port)),
                    "refused connect is async");
            assertEq(0, GoNet.getsockopt(s, 1L, 4L), "SO_ERROR still 0 pre-wait");
            assertEq(0, GoOs.close(s), "close");
        } catch (Exception e) {
            throw new AssertionError(e);
        } finally {
            teardown();
        }
    }

    public void testResetForTestInvalidatesSockets() {
        setup();
        try {
            long s = GoNet.socket(2L, 1L, 0L);
            GoNet.resetForTest();
            assertEq(-GoOs.EBADF, GoNet.getsockname(s, buf(23)),
                    "getsockname after reset -> EBADF");
            // The GoOs handle survives a GoNet-only reset; the lane
            // teardown (GoOs.init) is what drops it.
            assertEq(0, GoOs.close(s), "os.close still works after GoNet reset");
        } finally {
            teardown();
        }
    }

    public void testInterfacesListsLoopback() {
        setup();
        try {
            GoSlice b = buf(8192);
            long r = GoNet.interfaces(b);
            assertTrue(r > 0, "interfaces ok: " + r);
            byte[] raw = bytesOf(b, (int) (r - 1));
            int count = (raw[0] & 0xFF) | ((raw[1] & 0xFF) << 8)
                    | ((raw[2] & 0xFF) << 16) | ((raw[3] & 0xFF) << 24);
            assertTrue(count >= 1, "at least one interface, count=" + count);
            // Too-small buffer reports ENOMEM so the caller can grow it.
            assertEq(-GoNet.ENOMEM, GoNet.interfaces(buf(4)), "small buf -> ENOMEM");
        } finally {
            teardown();
        }
    }
}
