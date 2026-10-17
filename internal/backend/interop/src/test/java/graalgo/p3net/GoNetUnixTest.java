package graalgo.p3net;

import graalgo.p3os.GoOs;
import graalgo.sched.SchedulerCore;
import graalgo.values.GoSlice;

/**
 * Unit tests for AF_UNIX (Unix domain) sockets (review3-traps P1-I7).
 *
 * <p>Red-green discipline: {@code socket(AF_UNIX)} returned
 * {@code -EAFNOSUPPORT} before the fix; the bind/listen/connect/accept
 * flow exercises the {@link java.net.UnixDomainSocketAddress} backend
 * end to end. Uses {@code byte[]}-backed slices (the D5 carrier).
 */
public final class GoNetUnixTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long expected, long actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    private static SchedulerCore setup() {
        SchedulerCore sched = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        SchedulerCore prev = SchedulerCore.install(sched);
        sched.start();
        GoOs.init(new String[0]);
        GoNet.resetForTest();
        return prev;
    }

    private static void teardown(SchedulerCore prev) {
        GoNet.resetForTest();
        SchedulerCore.install(prev);
    }

    /** Wire-format AF_UNIX address: [1, path bytes...]. */
    private static GoSlice unixAddr(String path) {
        byte[] pb = path.getBytes(java.nio.charset.StandardCharsets.UTF_8);
        byte[] wire = new byte[1 + pb.length];
        wire[0] = 1; // AF_UNIX
        System.arraycopy(pb, 0, wire, 1, pb.length);
        return new GoSlice(wire, 0, wire.length, wire.length);
    }

    private static GoSlice byteBuf(int n) {
        byte[] b = new byte[n];
        return new GoSlice(b, 0, n, n);
    }

    private static String sockPath() throws Exception {
        java.nio.file.Path dir = java.nio.file.Files.createTempDirectory("gonet-unix-test");
        dir.toFile().deleteOnExit();
        java.nio.file.Path sock = dir.resolve("test.sock");
        sock.toFile().deleteOnExit();
        return sock.toString();
    }

    /**
     * socket(AF_UNIX, SOCK_STREAM) yields a cookie — not -EAFNOSUPPORT.
     */
    public void testUnixSocketCreates() {
        SchedulerCore prev = setup();
        try {
            long c = GoNet.socket(1, 1, 0);
            assertTrue(c >= 0, "socket(AF_UNIX) cookie, got errno=" + c);
            assertEq(0, GoOs.close(c), "close");
        } finally {
            teardown(prev);
        }
    }

    /**
     * Full Unix stream flow: bind, listen, connect, accept, and
     * getsockname round-trips the path through the wire format.
     */
    public void testUnixBindListenAcceptConnect() throws Exception {
        SchedulerCore prev = setup();
        String path = sockPath();
        long ls = -1;
        long cs = -1;
        try {
            ls = GoNet.socket(1, 1, 0);
            assertTrue(ls >= 0, "listen socket");
            assertEq(0, GoNet.bind(ls, unixAddr(path)), "bind");
            assertEq(0, GoNet.listen(ls, 128), "listen");

            // getsockname round-trips the bound path.
            GoSlice buf = byteBuf(128);
            long r = GoNet.getsockname(ls, buf);
            assertTrue(r > 0, "getsockname: " + r);
            byte[] wire = (byte[]) buf.array();
            assertTrue(wire[0] == 1, "getsockname family is AF_UNIX");
            int n = (int) r - 1;
            String got = new String(wire, 1, n - 1, java.nio.charset.StandardCharsets.UTF_8);
            assertTrue(path.equals(got), "getsockname path round-trip: " + got);

            // Connect (unix connects usually complete immediately).
            cs = GoNet.socket(1, 1, 0);
            assertTrue(cs >= 0, "client socket");
            long rc = GoNet.connect(cs, unixAddr(path));
            assertTrue(rc == 0 || rc == -GoNet.EINPROGRESS,
                    "connect immediate or in-progress: " + rc);
            // Let an async connect complete before accepting.
            Thread.sleep(100);

            // Accept the pending connection.
            long acc = -GoNet.EAGAIN;
            for (int i = 0; i < 100 && acc == -GoNet.EAGAIN; i++) {
                acc = GoNet.accept(ls);
                if (acc == -GoNet.EAGAIN) {
                    Thread.sleep(10);
                }
            }
            assertTrue(acc >= 0, "accept: " + acc);
            assertEq(0, GoOs.close(acc), "close accepted");

            assertEq(0, GoOs.close(cs), "close client");
            cs = -1;
            assertEq(0, GoOs.close(ls), "close listener");
            ls = -1;
        } finally {
            if (cs >= 0) {
                GoOs.close(cs);
            }
            if (ls >= 0) {
                GoOs.close(ls);
            }
            teardown(prev);
            new java.io.File(path).delete();
        }
    }
}
