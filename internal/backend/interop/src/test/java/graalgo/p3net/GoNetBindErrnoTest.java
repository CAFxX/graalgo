package graalgo.p3net;

import java.net.BindException;
import java.net.InetSocketAddress;
import java.nio.channels.ServerSocketChannel;

import graalgo.p3os.GoOs;

/**
 * Regression tests for {@link GoNet#sockErrno}'s {@link BindException}
 * mapping (REVIEW-2026-11-04 P1, error-identity divergence): every
 * BindException used to collapse to EADDRINUSE, so a program binding a
 * non-local address or a privileged port saw "address already in use"
 * instead of EADDRNOTAVAIL / EACCES and branched wrong.
 *
 * <p>Live binds pin the JVM's real messages; synthetic exceptions cover
 * the branch the sandbox cannot produce live: this sandbox runs as
 * root, so a privileged-port bind succeeds and never throws, hence
 * {@link #testLivePrivilegedPortBind} skips explicitly when the bind
 * succeeds (the EACCES mapping itself is still asserted, with the
 * JVM's exact message, in
 * {@link #testSyntheticPermissionDeniedMapsToEacces}).
 */
public final class GoNetBindErrnoTest {

    private static void assertEq(long want, long got, String msg) {
        if (want != got) {
            throw new AssertionError(msg + ": want " + want + " got " + got);
        }
    }

    /** Live: binding an address the host does not own must map to EADDRNOTAVAIL. */
    public void testLiveNonLocalBindMapsToEaddrnotavail() throws Exception {
        try (ServerSocketChannel ch = ServerSocketChannel.open()) {
            ch.bind(new InetSocketAddress("192.0.2.1", 0));
            throw new AssertionError("expected BindException binding non-local address 192.0.2.1");
        } catch (BindException be) {
            assertEq(GoNet.EADDRNOTAVAIL, GoNet.sockErrno(be),
                    "non-local bind [" + be.getMessage() + "]");
        }
    }

    /** Live: binding an already-bound loopback port must map to EADDRINUSE. */
    public void testLiveDoubleBindMapsToEaddrinuse() throws Exception {
        try (ServerSocketChannel first = ServerSocketChannel.open()) {
            first.bind(new InetSocketAddress("127.0.0.1", 0));
            int port = ((InetSocketAddress) first.getLocalAddress()).getPort();
            try (ServerSocketChannel second = ServerSocketChannel.open()) {
                second.bind(new InetSocketAddress("127.0.0.1", port));
                throw new AssertionError("expected BindException on double bind of port " + port);
            } catch (BindException be) {
                assertEq(GoNet.EADDRINUSE, GoNet.sockErrno(be),
                        "double bind [" + be.getMessage() + "]");
            }
        }
    }

    /**
     * Live privileged-port bind, skipped when the sandbox cannot produce
     * it: as root the bind succeeds (no exception at all). On a non-root
     * machine the bind throws and the EACCES mapping is asserted for real.
     */
    public void testLivePrivilegedPortBind() throws Exception {
        try (ServerSocketChannel ch = ServerSocketChannel.open()) {
            ch.bind(new InetSocketAddress("127.0.0.1", 1));
            System.out.println("SKIP testLivePrivilegedPortBind: privileged bind succeeded "
                    + "(running as root); EACCES branch covered synthetically");
        } catch (BindException be) {
            if (be.getMessage() != null
                    && be.getMessage().toLowerCase(java.util.Locale.ROOT).contains("permission denied")) {
                assertEq(GoOs.EACCES, GoNet.sockErrno(be),
                        "privileged bind [" + be.getMessage() + "]");
            } else {
                System.out.println("SKIP testLivePrivilegedPortBind: port 1 unavailable for the probe "
                        + "([" + be.getMessage() + "]); EACCES branch covered synthetically");
            }
        }
    }

    /** Synthetic: the JVM's privileged-port message must map to EACCES. */
    public void testSyntheticPermissionDeniedMapsToEacces() {
        assertEq(GoOs.EACCES, GoNet.sockErrno(new BindException("Permission denied")),
                "synthetic permission-denied bind");
    }

    /** Synthetic: the JVM's already-in-use message must map to EADDRINUSE. */
    public void testSyntheticAddressInUseMapsToEaddrinuse() {
        assertEq(GoNet.EADDRINUSE, GoNet.sockErrno(new BindException("Address already in use")),
                "synthetic address-in-use bind");
    }

    /** Synthetic: the JVM's non-local message must map to EADDRNOTAVAIL (case-insensitive). */
    public void testSyntheticCannotAssignMapsToEaddrnotavail() {
        assertEq(GoNet.EADDRNOTAVAIL,
                GoNet.sockErrno(new BindException("Cannot assign requested address")),
                "synthetic cannot-assign bind");
        assertEq(GoNet.EADDRNOTAVAIL,
                GoNet.sockErrno(new BindException("CANNOT ASSIGN REQUESTED ADDRESS")),
                "synthetic cannot-assign bind, upper case");
    }

    /** Synthetic: unknown or null messages keep the historical EADDRINUSE default. */
    public void testSyntheticUnknownBindMessageKeepsEaddrinuse() {
        assertEq(GoNet.EADDRINUSE, GoNet.sockErrno(new BindException("something unexpected")),
                "synthetic unknown-message bind");
        assertEq(GoNet.EADDRINUSE, GoNet.sockErrno(new BindException()),
                "synthetic null-message bind");
    }
}
