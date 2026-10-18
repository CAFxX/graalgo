package graalgo.p3net;

import java.lang.reflect.Field;
import java.util.concurrent.ConcurrentHashMap;

import graalgo.p3os.GoOs;
import graalgo.values.GoSlice;

/**
 * L34 (P1-31) regression: {@code GoNet.STATES} entries must be evicted when
 * a socket handle closes. Before the fix, {@code GoOs.close} removed the
 * handle and evicted netpoller state but left the {@code SockState} in the
 * static map, so long-running accept loops grew it without bound.
 *
 * <p>Loopback-only; no scheduler required (close with no registered
 * pollDesc is a no-op in the netpoller).
 */
public final class GoNetStatesEvictTest {

    private static final int AF_INET = 2;
    private static final int SOCK_STREAM = 1;
    private static final int SOCK_DGRAM = 2;
    private static final int EAGAIN = 11;

    @SuppressWarnings("unchecked")
    private static ConcurrentHashMap<Long, ?> states() {
        try {
            Field f = GoNet.class.getDeclaredField("STATES");
            f.setAccessible(true);
            return (ConcurrentHashMap<Long, ?>) f.get(null);
        } catch (ReflectiveOperationException e) {
            throw new RuntimeException(e);
        }
    }

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

    private static void reset() {
        GoOs.init(new String[0]);
        GoNet.resetForTest();
        GoNetpoll.resetForTest();
    }

    /** AF_INET loopback address wire form: [2, portHi, portLo, 127,0,0,1]. */
    private static byte[] loopback(int port) {
        return new byte[] {2, (byte) (port >>> 8), (byte) port, 127, 0, 0, 1};
    }

    /** Wire address as a byte[]-backed GoSlice (the net API's D5 []byte form). */
    private static GoSlice loopbackSlice(int port) {
        byte[] b = loopback(port);
        return new GoSlice(b, 0, b.length, b.length);
    }

    /** testStatesEvictedOnSocketClose: plain + datagram sockets. */
    public void testStatesEvictedOnSocketClose() {
        reset();
        long[] cookies = new long[8];
        for (int i = 0; i < cookies.length; i++) {
            long c = GoNet.socket((long) AF_INET, (long) (i % 2 == 0 ? SOCK_STREAM : SOCK_DGRAM), 0L);
            assertTrue(c >= 0, "socket failed: " + c);
            cookies[i] = c;
        }
        assertEq(cookies.length, states().size(), "STATES should hold one entry per socket");
        for (long c : cookies) {
            assertEq(0, GoOs.close(c), "close failed");
        }
        assertEq(0, states().size(), "STATES entries leaked after close");
        reset();
    }

    /** testAcceptedSocketEvictedOnClose: the accept() path inserts STATES too. */
    public void testAcceptedSocketEvictedOnClose() throws Exception {
        reset();
        long listener = GoNet.socket((long) AF_INET, (long) SOCK_STREAM, 0L);
        assertTrue(listener >= 0, "socket failed: " + listener);
        assertEq(0, GoNet.bind(listener, loopbackSlice(0)), "bind failed");
        assertEq(0, GoNet.listen(listener, 8L), "listen failed");

        GoSlice nameBuf = new GoSlice(new byte[32], 0, 32, 32);
        long n = GoNet.getsockname(listener, nameBuf);
        assertTrue(n > 1, "getsockname failed: " + n);
        byte[] name = (byte[]) nameBuf.array();
        int port = ((name[1] & 0xFF) << 8) | (name[2] & 0xFF);
        assertTrue(port != 0, "ephemeral port not assigned");

        long client = GoNet.socket((long) AF_INET, (long) SOCK_STREAM, 0L);
        assertTrue(client >= 0, "client socket failed: " + client);
        long rc = GoNet.connect(client, loopbackSlice(port));
        assertTrue(rc == 0 || rc == -115, "connect failed: " + rc); // -115 = EINPROGRESS

        long accepted = -EAGAIN;
        for (int i = 0; i < 400 && accepted < 0; i++) {
            accepted = GoNet.accept(listener);
            if (accepted == -EAGAIN) {
                Thread.sleep(5);
            }
        }
        assertTrue(accepted >= 0, "accept failed: " + accepted);
        assertEq(3, states().size(), "STATES should hold listener+client+accepted");

        assertEq(0, GoOs.close(client), "close client failed");
        assertEq(0, GoOs.close(accepted), "close accepted failed");
        assertEq(0, GoOs.close(listener), "close listener failed");
        assertEq(0, states().size(), "STATES entries leaked after accept-close");
        reset();
    }

    /** testDoubleCloseDoesNotRecreateState: eviction is idempotent. */
    public void testDoubleCloseDoesNotRecreateState() {
        reset();
        long c = GoNet.socket((long) AF_INET, (long) SOCK_STREAM, 0L);
        assertTrue(c >= 0, "socket failed: " + c);
        assertEq(0, GoOs.close(c), "close failed");
        long rc = GoOs.close(c);
        assertTrue(rc != 0, "second close should fail, got " + rc);
        assertEq(0, states().size(), "STATES must stay empty after double close");
        reset();
    }
}
