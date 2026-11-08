package graalgo.p3net;

import java.io.File;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

import graalgo.p3os.GoOs;
import graalgo.values.GoString;

/**
 * B-04 (REVIEW-2026-11-07-07): {@code GoOs.init} must be a complete
 * per-run reset — open handles actually closed (no fd leak) and net /
 * poller state cleared, so a cookie reissued after the reset cannot
 * alias stale socket state.
 *
 * <p>Proven with OS fds, not just table state: /proc/self/fd symlink
 * targets are counted before/after the reset.
 */
public final class GoOsResetTest {

    // Local copy: GoOs's O_* flags are private (mirrors GoOsErrnoTest).
    private static final int O_RDONLY = 0;

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

    private static Object gs(String s) {
        return GoString.fromUtf8(s);
    }

    /**
     * Number of open fds whose /proc/self/fd target is the given path,
     * or -1 when /proc/self/fd is unavailable (non-Linux: fd assertions
     * are skipped). Fds vanishing mid-scan are skipped, not counted.
     */
    private static int countFileFds(String realPath) {
        File dir = new File("/proc/self/fd");
        String[] names = dir.list();
        if (names == null) {
            return -1;
        }
        int n = 0;
        for (String name : names) {
            try {
                if (Files.readSymbolicLink(Path.of("/proc/self/fd", name)).toString().equals(realPath)) {
                    n++;
                }
            } catch (IOException | RuntimeException ignored) {
                // Fd closed mid-scan (or unreadable): skip, don't count.
            }
        }
        return n;
    }

    /** Number of open socket fds, or -1 when /proc/self/fd is unavailable. */
    private static int countSocketFds() {
        File dir = new File("/proc/self/fd");
        String[] names = dir.list();
        if (names == null) {
            return -1;
        }
        int n = 0;
        for (String name : names) {
            try {
                if (Files.readSymbolicLink(Path.of("/proc/self/fd", name)).toString().startsWith("socket:[")) {
                    n++;
                }
            } catch (IOException | RuntimeException ignored) {
                // Fd closed mid-scan (or unreadable): skip, don't count.
            }
        }
        return n;
    }

    public void testResetClosesHandlesAndClearsNetState() throws Exception {
        Path tmpFile = Files.createTempFile("goos-reset-", ".tmp");
        // The kernel resolves symlinks when opening; match its view.
        String realPath = tmpFile.toRealPath().toString();
        try {
            // Warm-up cycle: triggers lazy JVM/selector fd allocation
            // (first Selector.open, poller thread) so the measured
            // cycle is balanced.
            GoOs.init(new String[0]);
            long wFile = GoOs.open(gs(realPath), O_RDONLY, 0);
            assertEq(16, wFile, "warm-up file cookie");
            long wSock = GoNet.socket(GoNet.AF_INET, GoNet.SOCK_STREAM, 0);
            assertEq(17, wSock, "warm-up socket cookie");
            long wCtx = GoNetpoll.pollOpen(wSock);
            assertTrue(wCtx != 0, "warm-up pollOpen, errno=" + GoNetpoll.pollOpenErrno());
            GoOs.init(new String[0]); // the reset under test (warm-up)

            int baseSockets = countSocketFds();

            long fileCookie = GoOs.open(gs(realPath), O_RDONLY, 0);
            assertEq(16, fileCookie, "file cookie after init");
            long sockCookie = GoNet.socket(GoNet.AF_INET, GoNet.SOCK_STREAM, 0);
            assertEq(17, sockCookie, "socket cookie after init");
            long ctx = GoNetpoll.pollOpen(sockCookie);
            assertTrue(ctx != 0, "pollOpen, errno=" + GoNetpoll.pollOpenErrno());
            assertTrue(GoNet.sockState(sockCookie) != null, "SockState present before reset");
            assertTrue(GoNetpoll.descForTest(ctx) != null, "PollDesc present before reset");
            int fileFds = countFileFds(realPath);
            if (fileFds >= 0) {
                assertEq(1, fileFds, "fds referencing the temp file after open");
            }
            if (baseSockets >= 0) {
                assertEq(baseSockets + 1, countSocketFds(), "socket fds after open");
            }

            // THE RESET.
            GoOs.init(new String[0]);

            // No fd leak: the file channel and the socket channel are
            // really closed, not just dropped from the handle table.
            int fileFdsAfter = countFileFds(realPath);
            if (fileFdsAfter >= 0) {
                assertEq(0, fileFdsAfter, "fds referencing the temp file after reset");
            }
            if (baseSockets >= 0) {
                assertEq(baseSockets, countSocketFds(), "socket fds after reset");
            }
            // No aliasing: cookie 16 is reissued, but no stale net
            // state may survive under it.
            long reused = GoOs.open(gs(realPath), O_RDONLY, 0);
            assertEq(16, reused, "cookie 16 reissued to a new file handle");
            assertTrue(GoNet.sockState(16L) == null, "stale SockState must not survive reset");
            assertTrue(GoNetpoll.descForTest(ctx) == null, "stale PollDesc must not survive reset");
            // Behavioral: the net layer must not treat the new file
            // handle as a socket.
            assertEq(0, GoNetpoll.pollOpen(16L), "pollOpen on a file handle must fail");
            assertEq(GoNet.ENOTSOCK, GoNetpoll.pollOpenErrno(), "pollOpen errno on a file handle");
            // And the reissued handle is a live file handle: closeable
            // exactly once.
            assertEq(0, GoOs.close(16L), "close reissued handle");
            assertEq(-GoOs.EBADF, GoOs.close(16L), "re-close must be EBADF");
        } finally {
            GoOs.init(new String[0]); // leave the JVM clean for the next test class
            Files.deleteIfExists(tmpFile);
        }
    }

    public void testNetpollerUsableAfterReset() {
        GoOs.init(new String[0]);
        long sock = GoNet.socket(GoNet.AF_INET, GoNet.SOCK_STREAM, 0);
        assertEq(16, sock, "socket cookie after init");
        long ctx = GoNetpoll.pollOpen(sock);
        assertTrue(ctx != 0, "pollOpen after reset, errno=" + GoNetpoll.pollOpenErrno());
        GoNetpoll.pollUnblock(ctx);
        GoNetpoll.pollClose(ctx);
        assertTrue(GoNetpoll.descForTest(ctx) == null, "desc dropped after pollClose");
        assertEq(0, GoOs.close(sock), "close socket after pollClose");
        GoOs.init(new String[0]);
    }
}
