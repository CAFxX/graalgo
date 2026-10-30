package graalgo.p3os;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.PrintStream;
import java.nio.channels.ClosedChannelException;
import java.nio.channels.NonReadableChannelException;
import java.nio.charset.StandardCharsets;
import java.nio.file.AccessDeniedException;
import java.nio.file.DirectoryNotEmptyException;
import java.nio.file.FileAlreadyExistsException;
import java.nio.file.FileSystemException;
import java.nio.file.FileSystemLoopException;
import java.nio.file.Files;
import java.nio.file.InvalidPathException;
import java.nio.file.NoSuchFileException;
import java.nio.file.NotDirectoryException;
import java.nio.file.NotLinkException;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Unit tests for {@link GoOs} (P3-M8, wave 2, R1): the single
 * {@code errno(Throwable)} mapper is tested per failure mode with
 * constructed exceptions, and the main intrinsics are exercised
 * behaviorally through a hermetic temp directory (never /tmp).
 *
 * <p>Each test calls {@code GoOs.init} first to reset process-wide
 * state (virtual cwd, env overlay, handle table).
 */
public final class GoOsErrnoTest {
    // Linux open(2) flags, mirroring the synthetic os package.
    private static final int O_RDONLY = 0;
    private static final int O_WRONLY = 1;
    private static final int O_RDWR = 2;
    private static final int O_CREAT = 64;
    private static final int O_EXCL = 128;
    private static final int O_TRUNC = 512;

    private static Path scratch;

    private static void initScratch() throws IOException {
        GoOs.init(new String[0]);
        // Hermetic scratch: the sanctioned $TMPDIR (never /tmp), falling
        // back to a directory under the working directory.
        String tmpdir = System.getenv("TMPDIR");
        Path base = tmpdir != null
                ? Path.of(tmpdir, "junit")
                : Path.of(System.getProperty("user.dir"), "junit-scratch");
        Files.createDirectories(base);
        scratch = Files.createTempDirectory(base, "goos-");
    }

    private static void cleanScratch() throws IOException {
        if (scratch == null) {
            return;
        }
        List<Path> paths = new ArrayList<>();
        try (var stream = Files.walk(scratch)) {
            stream.forEach(paths::add);
        }
        paths.sort((a, b) -> b.compareTo(a));
        for (Path p : paths) {
            Files.deleteIfExists(p);
        }
        scratch = null;
    }

    private static Object gs(String s) {
        return GoString.fromUtf8(s);
    }

    private static GoSlice buf(int n) {
        return new GoSlice(new int[n], 0, n, n);
    }

    private static GoSlice bytesBuf(byte[] data) {
        int[] arr = new int[data.length];
        for (int i = 0; i < data.length; i++) {
            arr[i] = data[i] & 0xFF;
        }
        return new GoSlice(arr, 0, arr.length, arr.length);
    }

    private static byte[] sliceBytes(GoSlice gs) {
        int[] arr = (int[]) gs.array();
        byte[] out = new byte[gs.len()];
        for (int i = 0; i < gs.len(); i++) {
            out[i] = (byte) arr[gs.offset() + i];
        }
        return out;
    }

    private static String str(Object o) {
        return new String(((GoString) o).bytes(), StandardCharsets.UTF_8);
    }

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void checkEq(long want, long got, String msg) {
        if (want != got) {
            throw new AssertionError(msg + ": want " + want + ", got " + got);
        }
    }

    // ------------------------------------------------------------------
    // R1: the errno mapper, one failure mode per case.
    // ------------------------------------------------------------------

    public void testErrnoNulPath() {
        checkEq(GoOs.EINVAL,
                GoOs.errno(new InvalidPathException("a\0b", "Nul character not allowed")),
                "NUL path");
    }

    public void testErrnoMissing() {
        checkEq(GoOs.ENOENT, GoOs.errno(new NoSuchFileException("x")), "missing file");
    }

    public void testErrnoDenied() {
        checkEq(GoOs.EACCES, GoOs.errno(new AccessDeniedException("x")), "access denied");
    }

    public void testErrnoExists() {
        checkEq(GoOs.EEXIST, GoOs.errno(new FileAlreadyExistsException("x")), "exists");
    }

    public void testErrnoNotDir() {
        checkEq(GoOs.ENOTDIR, GoOs.errno(new NotDirectoryException("x")), "not a directory");
    }

    public void testErrnoNotLink() {
        checkEq(GoOs.EINVAL, GoOs.errno(new NotLinkException("x")), "readlink non-link");
    }

    public void testErrnoFileParent() throws IOException {
        initScratch();
        try {
            String f = scratch.resolve("f").toString();
            Files.writeString(Path.of(f), "x");
            // A regular file in a parent position is ENOTDIR (gc parity);
            // the JDK reports both cases as NoSuchFileException.
            checkEq(GoOs.ENOTDIR, GoOs.errno(new NoSuchFileException(f + "/x")),
                    "file parent -> ENOTDIR");
            checkEq(GoOs.ENOENT,
                    GoOs.errno(new NoSuchFileException(scratch.resolve("nope/x").toString())),
                    "missing parent -> ENOENT");
            checkEq(GoOs.ENOTDIR,
                    GoOs.errno(new FileSystemException("x", null, "Not a directory")),
                    "reason not a directory");
        } finally {
            cleanScratch();
        }
    }

    public void testErrnoNotEmpty() {
        checkEq(GoOs.ENOTEMPTY, GoOs.errno(new DirectoryNotEmptyException("x")),
                "directory not empty");
    }

    public void testErrnoLoop() {
        checkEq(GoOs.ELOOP, GoOs.errno(new FileSystemLoopException("x")), "symlink loop");
    }

    public void testErrnoClosedChannel() {
        checkEq(GoOs.EBADF, GoOs.errno(new ClosedChannelException()), "closed channel");
    }

    public void testErrnoNonReadableChannel() {
        checkEq(GoOs.EBADF, GoOs.errno(new NonReadableChannelException()),
                "read on write-only channel");
    }

    public void testErrnoNameTooLong() {
        // The JDK's reason text is "File name too long" (verified 2026-10-07).
        checkEq(GoOs.ENAMETOOLONG,
                GoOs.errno(new FileSystemException("x", null, "File name too long")),
                "name too long");
    }

    public void testErrnoGenericFs() {
        checkEq(GoOs.EIO,
                GoOs.errno(new FileSystemException("x", null, "some other reason")),
                "generic fs error");
    }

    public void testErrnoBrokenPipe() {
        // The JDK's message is "Broken pipe" (verified 2026-10-07).
        checkEq(GoOs.EPIPE, GoOs.errno(new IOException("Broken pipe")), "broken pipe");
    }

    public void testErrnoTooManyOpenFiles() {
        // M10 (P3-M10 WS J, self-attack D3): fd exhaustion must map to
        // EMFILE (per-process, e.g. ulimit) / ENFILE (system-wide), not
        // EIO — gc surfaces these distinctly and Go code branches on
        // them (e.g. syscall retry predicates).
        checkEq(GoOs.EMFILE, GoOs.errno(new IOException("Too many open files")),
                "per-process fd exhaustion");
        checkEq(GoOs.ENFILE, GoOs.errno(new IOException("Too many open files in system")),
                "system-wide fd exhaustion");
        // Case-insensitive like the broken-pipe check.
        checkEq(GoOs.EMFILE, GoOs.errno(new IOException("too many open files")),
                "lowercase variant");
        // FileChannel.open reports EMFILE as FileSystemException (with
        // the path and reason), not plain IOException — the branch that
        // actually fires in production.
        checkEq(GoOs.EMFILE,
                GoOs.errno(new FileSystemException("/tmp/x", null, "Too many open files")),
                "FileSystemException variant");
    }

    public void testErrnoGenericIo() {
        checkEq(GoOs.EIO, GoOs.errno(new IOException("disk exploded")), "generic io error");
    }

    public void testErrnoSecurity() {
        checkEq(GoOs.EACCES, GoOs.errno(new SecurityException("denied")), "security");
    }

    public void testErrnoUnknown() {
        checkEq(GoOs.EIO, GoOs.errno(new RuntimeException("weird")), "unknown throwable");
    }

    // ------------------------------------------------------------------
    // Behavioral: files.
    // ------------------------------------------------------------------

    public void testOpenWriteReadClose() throws IOException {
        initScratch();
        try {
            String name = scratch.resolve("f").toString();
            long h = GoOs.open(gs(name), O_WRONLY | O_CREAT | O_TRUNC, 0644);
            check(h >= 16, "open cookie, got " + h);
            checkEq(GoOs.KIND_REGULAR, GoOs.handleKind(h), "kind");
            byte[] data = "hello".getBytes(StandardCharsets.UTF_8);
            checkEq(6, GoOs.write(h, bytesBuf(data)),
                    "write n+1");
            checkEq(0, GoOs.close(h), "close");
            checkEq(-GoOs.EBADF, GoOs.close(h), "double close");

            long r = GoOs.open(gs(name), O_RDONLY, 0);
            GoSlice b = buf(16);
            checkEq(6, GoOs.read(r, b), "read n+1");
            byte[] got = new byte[5];
            byte[] read = sliceBytes(b);
            System.arraycopy(read, 0, got, 0, 5);
            checkEq(0, "hello".compareTo(new String(got, StandardCharsets.UTF_8)),
                    "read content");
            checkEq(0, GoOs.read(r, buf(16)), "EOF is 0");
            checkEq(-GoOs.EBADF, GoOs.write(r, buf(1)), "write on read-only");
            checkEq(0, GoOs.close(r), "close read handle");
        } finally {
            cleanScratch();
        }
    }

    public void testOpenErrors() throws IOException {
        initScratch();
        try {
            String missing = scratch.resolve("nope").toString();
            checkEq(-GoOs.ENOENT, GoOs.open(gs(missing), O_RDONLY, 0), "open missing");
            checkEq(-GoOs.EINVAL, GoOs.open(gs("a\0b"), O_RDONLY, 0), "open NUL path");

            String name = scratch.resolve("e").toString();
            long h = GoOs.open(gs(name), O_WRONLY | O_CREAT, 0644);
            checkEq(0, GoOs.close(h), "create");
            checkEq(-GoOs.EEXIST,
                    GoOs.open(gs(name), O_WRONLY | O_CREAT | O_EXCL, 0644), "O_EXCL");
            // Read-only O_EXCL on an existing file is a no-op success.
            long h2 = GoOs.open(gs(name), O_RDONLY | O_EXCL, 0);
            check(h2 >= 16, "O_EXCL without O_CREATE");
            checkEq(0, GoOs.close(h2), "close");

            long d = GoOs.open(gs(scratch.toString()), O_RDONLY, 0);
            checkEq(GoOs.KIND_DIR, GoOs.handleKind(d), "dir kind");
            checkEq(-GoOs.EISDIR, GoOs.read(d, buf(8)), "read on dir");
            checkEq(-GoOs.EISDIR,
                    GoOs.open(gs(scratch.toString()), O_WRONLY, 0), "write-open on dir");
            checkEq(0, GoOs.close(d), "close dir");

            checkEq(-GoOs.EBADF, GoOs.read(999999L, buf(8)), "read bad cookie");
            checkEq(-GoOs.EBADF, GoOs.handleKind(999999L), "kind bad cookie");
        } finally {
            cleanScratch();
        }
    }

    public void testSeekReadAtWriteAt() throws IOException {
        initScratch();
        try {
            String name = scratch.resolve("s").toString();
            long h = GoOs.open(gs(name), O_RDWR | O_CREAT | O_TRUNC, 0644);
            byte[] data = "0123456789".getBytes(StandardCharsets.UTF_8);
            GoOs.write(h, bytesBuf(data));

            checkEq(4, GoOs.seek(h, 4, 0), "seek set");
            GoSlice b = buf(4);
            checkEq(5, GoOs.read(h, b), "read after seek");
            checkEq(0, "4567".compareTo(
                    new String(sliceBytes(b), 0, 4, StandardCharsets.UTF_8)),
                    "seek content");

            checkEq(6, GoOs.seek(h, -2, 1), "seek cur negative"); // pos was 8 after the read
            checkEq(10, GoOs.seek(h, 0, 2), "seek end");
            checkEq(-GoOs.EINVAL, GoOs.seek(h, -100, 0), "seek before start");
            checkEq(-GoOs.EINVAL, GoOs.seek(h, 0, 99), "bad whence");

            GoSlice rb = buf(3);
            checkEq(4, GoOs.readAt(h, rb, 2L), "readAt n+1");
            checkEq(0, "234".compareTo(
                    new String(sliceBytes(rb), 0, 3, StandardCharsets.UTF_8)),
                    "readAt content");
            checkEq(0, GoOs.readAt(h, buf(3), 100L), "readAt EOF");

            byte[] zz = "ZZ".getBytes(StandardCharsets.UTF_8);
            checkEq(3, GoOs.writeAt(h, bytesBuf(zz), 0L), "writeAt n+1");
            checkEq(0, GoOs.seek(h, 0, 0), "rewind");
            GoSlice all = buf(10);
            GoOs.read(h, all);
            checkEq(0, "ZZ23456789".compareTo(
                    new String(sliceBytes(all), 0, 10, StandardCharsets.UTF_8)),
                    "writeAt content");

            checkEq(0, GoOs.truncate(h, 4L), "truncate");
            checkEq(4, GoOs.seek(h, 0, 2), "size after truncate");
            checkEq(-GoOs.EINVAL, GoOs.truncate(h, -1L), "negative truncate");
            checkEq(0, GoOs.sync(h), "sync");
            checkEq(0, GoOs.close(h), "close");
            checkEq(-GoOs.ESPIPE, GoOs.readAt(h, buf(1), 0L), "readAt closed");
        } finally {
            cleanScratch();
        }
    }

    public void testDirNext() throws IOException {
        initScratch();
        try {
            Files.writeString(scratch.resolve("a"), "a");
            Files.writeString(scratch.resolve("b"), "b");
            Files.createDirectory(scratch.resolve("sub"));
            long d = GoOs.open(gs(scratch.toString()), O_RDONLY, 0);
            String recs = str(GoOs.dirNext(d, 100));
            check(recs.contains("a\0f"), "file a: " + recs.replace("\0", "<0>"));
            check(recs.contains("b\0f"), "file b");
            check(recs.contains("sub\0d"), "dir sub");
            checkEq(0, str(GoOs.dirNext(d, 100)).length(), "exhausted stream");
            checkEq(0, GoOs.close(d), "close");
        } finally {
            cleanScratch();
        }
    }

    public void testMkdirRemoveRename() throws IOException {
        initScratch();
        try {
            String d = scratch.resolve("d1").toString();
            checkEq(0, GoOs.mkdir(gs(d), 0755), "mkdir");
            checkEq(-GoOs.EEXIST, GoOs.mkdir(gs(d), 0755), "mkdir existing");
            checkEq(-GoOs.ENOENT,
                    GoOs.mkdir(gs(scratch.resolve("nope/x").toString()), 0755),
                    "mkdir missing parent");

            String d2 = scratch.resolve("d2").toString();
            checkEq(0, GoOs.rename(gs(d), gs(d2)), "rename");
            checkEq(-GoOs.ENOENT, GoOs.rename(gs(d), gs(d2)), "rename missing src");

            checkEq(0, GoOs.remove(gs(d2)), "rmdir");
            checkEq(-GoOs.ENOENT, GoOs.remove(gs(d2)), "remove missing");

            String f = scratch.resolve("f").toString();
            long h = GoOs.open(gs(f), O_WRONLY | O_CREAT, 0644);
            GoOs.close(h);
            checkEq(0, GoOs.truncatePath(gs(f), 0L), "truncatePath");
            checkEq(-GoOs.EINVAL, GoOs.truncatePath(gs(f), -1L), "truncatePath negative");
        } finally {
            cleanScratch();
        }
    }

    public void testSymlinkReadlink() throws IOException {
        initScratch();
        try {
            String target = scratch.resolve("t").toString();
            Files.writeString(Path.of(target), "x");
            String link = scratch.resolve("l").toString();
            checkEq(0, GoOs.symlink(gs(target), gs(link)), "symlink");
            // readlink with a generous buffer.
            GoSlice b = buf(256);
            long r = GoOs.readlink(gs(link), b);
            check(r > 1, "readlink n+1, got " + r);
            String got = new String(sliceBytes(b), 0, (int) (r - 1),
                    StandardCharsets.UTF_8);
            checkEq(0, target.compareTo(got), "readlink content");
            // Stat with follow=0 sees the link itself (S_IFLNK).
            long mode = GoOs.statMode(gs(link), 0L);
            checkEq(0120000, mode & 0170000, "lstat mode type");
            // Stat with follow=1 sees the target (S_IFREG).
            checkEq(0100000, GoOs.statMode(gs(link), 1L) & 0170000, "stat mode type");
            // readlink on a non-link fails.
            checkEq(-GoOs.EINVAL, GoOs.readlink(gs(target), buf(8)), "readlink non-link");
            checkEq(-GoOs.ENOENT,
                    GoOs.readlink(gs(scratch.resolve("nope").toString()), buf(8)),
                    "readlink missing");
        } finally {
            cleanScratch();
        }
    }

    public void testStat() throws IOException {
        initScratch();
        try {
            String name = scratch.resolve("st").toString();
            Files.writeString(Path.of(name), "12345");
            long size = GoOs.statSize(gs(name), 1L);
            checkEq(5, size, "stat size");
            long mode = GoOs.statMode(gs(name), 1L);
            checkEq(0100000, mode & 0170000, "stat type");
            check((mode & 0777) != 0, "stat perm bits, got " + Long.toOctalString(mode));
            long mtime = GoOs.statMtimeNs(gs(name), 1L);
            check(mtime > 0, "stat mtime");
            long ino = GoOs.statIno(gs(name), 1L);
            check(ino != 0, "stat ino");
            long dev = GoOs.statDev(gs(name), 1L);
            check(dev != 0, "stat dev");
            checkEq(-GoOs.ENOENT,
                    GoOs.statSize(gs(scratch.resolve("nope").toString()), 1L),
                    "stat missing");
        } finally {
            cleanScratch();
        }
    }

    public void testChmodChtimes() throws IOException {
        initScratch();
        try {
            String name = scratch.resolve("c").toString();
            Files.writeString(Path.of(name), "x");
            checkEq(0, GoOs.chmod(gs(name), 0600), "chmod");
            long mode = GoOs.statMode(gs(name), 1L);
            checkEq(0600, mode & 0777, "chmod bits, got " + Long.toOctalString(mode & 0777));
            long t = 1_700_000_000_000_000_000L;
            checkEq(0, GoOs.chtimes(gs(name), t, t), "chtimes");
            // Second-resolution check: nanos may be truncated by the FS,
            // but the second must match exactly.
            checkEq(t / 1_000_000_000,
                    GoOs.statMtimeNs(gs(name), 1L) / 1_000_000_000, "chtimes mtime");
            checkEq(-GoOs.ENOENT,
                    GoOs.chmod(gs(scratch.resolve("nope").toString()), 0600),
                    "chmod missing");
        } finally {
            cleanScratch();
        }
    }

    public void testChdirGetcwd() throws IOException {
        initScratch();
        try {
            String sub = scratch.resolve("sub").toString();
            Files.createDirectory(Path.of(sub));
            checkEq(0, GoOs.chdir(gs(sub)), "chdir");
            String cwd = str(GoOs.getcwd());
            checkEq(0, Path.of(sub).toRealPath().toString().compareTo(cwd), "getcwd");
            // A relative open now resolves against the virtual cwd.
            long h = GoOs.open(gs("rel"), O_WRONLY | O_CREAT, 0644);
            check(h >= 16, "relative open after chdir");
            GoOs.close(h);
            check(Files.exists(Path.of(sub, "rel")), "file landed in virtual cwd");
            checkEq(-GoOs.ENOTDIR,
                    GoOs.chdir(gs(Path.of(sub, "rel").toString())), "chdir to file");
            checkEq(-GoOs.ENOENT,
                    GoOs.chdir(gs(scratch.resolve("nope").toString())), "chdir missing");
            // The failed chdirs left the cwd unchanged.
            checkEq(0, Path.of(sub).toRealPath().toString().compareTo(str(GoOs.getcwd())),
                    "cwd unchanged after failed chdir");
        } finally {
            cleanScratch();
        }
    }

    public void testEnv() {
        GoOs.init(new String[0]);
        String key = "GRAALGO_OS_TEST_KEY_XYZ";
        checkEq(0, GoOs.getenvFound(gs(key)), "absent initially");
        checkEq(0, str(GoOs.getenv(gs(key))).length(), "absent value");
        GoOs.setenv(gs(key), gs("v1"));
        checkEq(1, GoOs.getenvFound(gs(key)), "found after set");
        checkEq(0, "v1".compareTo(str(GoOs.getenv(gs(key)))), "value after set");
        GoOs.unsetenv(gs(key));
        checkEq(0, GoOs.getenvFound(gs(key)), "gone after unset");
        // Real environment is visible until clearenv.
        String pathVal = System.getenv("PATH");
        if (pathVal != null) {
            checkEq(1, GoOs.getenvFound(gs("PATH")), "real env visible");
            GoOs.clearenv();
            checkEq(0, GoOs.getenvFound(gs("PATH")), "real env hidden after clearenv");
        }
        checkEq(0, GoOs.getenvFound(gs(key)), "overlay cleared");
        // environ() is NUL-joined k=v.
        GoOs.setenv(gs("A"), gs("1"));
        GoOs.setenv(gs("B"), gs("2"));
        String env = str(GoOs.environ());
        check(env.contains("A=1\0") && env.contains("B=2\0"), "environ: " + env.replace("\0", "<0>"));
    }

    public void testPipe() throws IOException {
        GoOs.init(new String[0]);
        long r = GoOs.pipe();
        check(r >= 16, "pipe read cookie");
        checkEq(GoOs.KIND_PIPE, GoOs.handleKind(r), "pipe read kind");
        checkEq(GoOs.KIND_PIPE, GoOs.handleKind(r + 1), "pipe write kind");
        byte[] data = "pipedata".getBytes(StandardCharsets.UTF_8);
        checkEq(9, GoOs.write(r + 1, bytesBuf(data)),
                "pipe write n+1");
        GoSlice b = buf(16);
        checkEq(9, GoOs.read(r, b), "pipe read n+1");
        checkEq(0, "pipedata".compareTo(
                new String(sliceBytes(b), 0, 8, StandardCharsets.UTF_8)),
                "pipe content");
        checkEq(-GoOs.EBADF, GoOs.read(r + 1, buf(4)), "read on pipe write end");
        checkEq(-GoOs.EBADF, GoOs.write(r, buf(4)), "write on pipe read end");
        checkEq(-GoOs.ESPIPE, GoOs.seek(r, 0, 0), "seek on pipe");
        checkEq(0, GoOs.close(r), "close read");
        checkEq(0, GoOs.close(r + 1), "close write");
    }

    public void testStdio() {
        GoOs.init(new String[0]);
        checkEq(1, GoOs.stdio(0L), "stdin cookie");
        checkEq(2, GoOs.stdio(1L), "stdout cookie");
        checkEq(3, GoOs.stdio(2L), "stderr cookie");
        checkEq(-GoOs.EBADF, GoOs.stdio(3L), "bad stdio fd");
        checkEq(GoOs.KIND_STDIO, GoOs.handleKind(1L), "stdio kind");
        // Closing stdio removes the cookie but must not close System.out.
        checkEq(0, GoOs.close(2L), "close stdout cookie");
        checkEq(-GoOs.EBADF, GoOs.handleKind(2L), "stdout cookie gone");
        System.out.println("stdio-still-open");
    }

    // fix-redmaster-r44: stdio writes must follow System.out/err rebinds.
    // Batch mode (graalgo-run --batch) rebinds the standard streams per
    // entry to capture each entry's output, but the old channels captured
    // the stream object at init time, so every entry after the first lost
    // its fmt/os.Stdout output (exit 0, empty stdout — the r43 CI
    // mergemul/strength divergence). This test mirrors the batch shape:
    // init first (channels bound), then rebind, write, rebind, write.
    // It FAILS on the old code (both writes land on the real stdout and
    // both buffers stay empty) and passes on the dynamic delegates.
    // NOTE: uses a byte[]-backed slice, not the shared bytesBuf helper:
    // D5 stores []byte in byte[] backing arrays (goBuf casts to byte[]),
    // while bytesBuf still builds int[]-backed slices.
    private static GoSlice byteSlice(byte[] data) {
        return new GoSlice(data.clone(), 0, data.length, data.length);
    }

    public void testStdioFollowsReboundStreams() {
        GoOs.init(new String[0]);
        PrintStream realOut = System.out;
        PrintStream realErr = System.err;
        try {
            ByteArrayOutputStream first = new ByteArrayOutputStream();
            ByteArrayOutputStream second = new ByteArrayOutputStream();
            System.setOut(new PrintStream(first, true, StandardCharsets.UTF_8));
            byte[] one = "one".getBytes(StandardCharsets.UTF_8);
            checkEq(4, GoOs.write(2L, byteSlice(one)), "write stdout n+1");
            System.setOut(new PrintStream(second, true, StandardCharsets.UTF_8));
            byte[] two = "two".getBytes(StandardCharsets.UTF_8);
            checkEq(4, GoOs.write(2L, byteSlice(two)), "write stdout after rebind n+1");
            checkEq(0, "one".compareTo(first.toString(StandardCharsets.UTF_8)),
                    "first capture");
            checkEq(0, "two".compareTo(second.toString(StandardCharsets.UTF_8)),
                    "second capture after rebind");

            ByteArrayOutputStream errFirst = new ByteArrayOutputStream();
            ByteArrayOutputStream errSecond = new ByteArrayOutputStream();
            System.setErr(new PrintStream(errFirst, true, StandardCharsets.UTF_8));
            byte[] e1 = "e1".getBytes(StandardCharsets.UTF_8);
            checkEq(3, GoOs.write(3L, byteSlice(e1)), "write stderr n+1");
            System.setErr(new PrintStream(errSecond, true, StandardCharsets.UTF_8));
            byte[] e2 = "e2".getBytes(StandardCharsets.UTF_8);
            checkEq(3, GoOs.write(3L, byteSlice(e2)), "write stderr after rebind n+1");
            checkEq(0, "e1".compareTo(errFirst.toString(StandardCharsets.UTF_8)),
                    "first stderr capture");
            checkEq(0, "e2".compareTo(errSecond.toString(StandardCharsets.UTF_8)),
                    "second stderr capture after rebind");
        } finally {
            System.setOut(realOut);
            System.setErr(realErr);
        }
    }

    // N-P1-10: stdio failure detection. A PrintStream over a stream
    // that throws on write/flush (the broken-pipe shape: the kernel
    // write fails, PrintStream swallows it) must be reported by
    // stdioError, while a healthy capture stream must not. This tests
    // the detection mechanism only, not the death dance: calling
    // GoOs.write here would run onEpipeStdio in this JVM and kill the
    // test runner (see testStdioBrokenPipeDiesWithSigpipe, which puts
    // the whole path, death included, in a child JVM).
    private static final class FailingOutputStream extends java.io.OutputStream {
        @Override
        public void write(int b) throws IOException {
            throw new IOException("Broken pipe");
        }

        @Override
        public void write(byte[] b, int off, int len) throws IOException {
            throw new IOException("Broken pipe");
        }

        @Override
        public void flush() throws IOException {
            throw new IOException("Broken pipe");
        }
    }

    public void testStdioErrorDetection() {
        GoOs.init(new String[0]);
        PrintStream realOut = System.out;
        PrintStream realErr = System.err;
        try {
            System.setOut(new PrintStream(new FailingOutputStream(), true,
                    StandardCharsets.UTF_8));
            check(GoOs.stdioError(2L), "failing stdout must report error");
            System.setErr(new PrintStream(new FailingOutputStream(), true,
                    StandardCharsets.UTF_8));
            check(GoOs.stdioError(3L), "failing stderr must report error");
            check(!GoOs.stdioError(1L), "stdin cookie has no PrintStream");
            check(!GoOs.stdioError(99L), "unknown cookie has no PrintStream");

            ByteArrayOutputStream ok = new ByteArrayOutputStream();
            System.setOut(new PrintStream(ok, true, StandardCharsets.UTF_8));
            check(!GoOs.stdioError(2L), "healthy stdout must not report error");
            System.setErr(new PrintStream(ok, true, StandardCharsets.UTF_8));
            check(!GoOs.stdioError(3L), "healthy stderr must not report error");
        } finally {
            System.setOut(realOut);
            System.setErr(realErr);
        }
    }

    // N-P1-10, child entry point for testStdioBrokenPipeDiesWithSigpipe:
    // never invoked by the test runner (it only calls test* methods).
    // Sleeps so the parent can close the read end of this JVM's stdout
    // pipe, then writes to stdout through GoOs. Before the fix the
    // PrintStream swallowed the broken-pipe failure and this exited 0
    // (or 42/43 below); with the fix the SIGPIPE dance kills it (141).
    public static void main(String[] args) throws Exception {
        if (args.length == 0 || !args[0].equals("stdio-epipe-child")) {
            return;
        }
        Thread.sleep(800);
        GoOs.init(new String[0]);
        long r = GoOs.write(2L, byteSlice("hello-broken-pipe\n".getBytes(StandardCharsets.UTF_8)));
        System.err.println("CHILD-SURVIVED r=" + r);
        System.exit(r == -GoOs.EPIPE ? 42 : 43);
    }

    // N-P1-10, end to end in a child JVM: the child's stdout is a pipe
    // whose read end the parent closes before the child writes, so the
    // child's stdout write hits a broken pipe exactly like
    // `program | head -c 0`. gc dies with SIGPIPE (exit 141); before
    // this fix graalgo exited 0 having "written" successfully.
    public void testStdioBrokenPipeDiesWithSigpipe() throws Exception {
        String javaBin = Path.of(System.getProperty("java.home"), "bin", "java").toString();
        ProcessBuilder pb = new ProcessBuilder(javaBin, "-cp",
                System.getProperty("java.class.path"),
                GoOsErrnoTest.class.getName(), "stdio-epipe-child");
        pb.redirectError(ProcessBuilder.Redirect.INHERIT);
        pb.redirectInput(ProcessBuilder.Redirect.PIPE);
        pb.redirectOutput(ProcessBuilder.Redirect.PIPE);
        Process child = pb.start();
        // Close the child's stdin and, critically, the read end of its
        // stdout pipe: from here on the child writes to a broken pipe.
        child.getOutputStream().close();
        child.getInputStream().close();
        boolean finished = child.waitFor(30, java.util.concurrent.TimeUnit.SECONDS);
        check(finished, "stdio-epipe child did not exit in 30s");
        int exit = child.exitValue();
        checkEq(141, exit, "broken-pipe stdout write must die with SIGPIPE (141)");
    }

    public void testIdentity() {
        GoOs.init(new String[0]);
        check(GoOs.getpid() > 0, "getpid");
        check(GoOs.getppid() >= 0, "getppid");
        check(GoOs.getuid() >= 0, "getuid");
        check(GoOs.getgid() >= 0, "getgid");
        checkEq(GoOs.getuid(), GoOs.geteuid(), "euid==uid absent setuid");
        checkEq(GoOs.getgid(), GoOs.getegid(), "egid==gid absent setuid");
    }

    public void testArgv() {
        GoOs.init(new String[] {"prog.go", "a", "b"});
        checkEq(3, GoOs.argc(), "argc");
        checkEq(0, "prog.go".compareTo(str(GoOs.argv(0L))), "argv0");
        checkEq(0, "a".compareTo(str(GoOs.argv(1L))), "argv1");
        checkEq(0, "b".compareTo(str(GoOs.argv(2L))), "argv2");
        checkEq(0, str(GoOs.argv(99L)).length(), "argv out of range");
        GoOs.init(new String[0]);
        checkEq(0, GoOs.argc(), "argc after reset");
    }

    public void testHostname() {
        GoOs.init(new String[0]);
        GoSlice b = buf(256);
        long r = GoOs.hostname(b);
        // -EIO when the host name is unresolvable (Go side falls back
        // to /proc/sys/kernel/hostname); otherwise n+1.
        if (r != -GoOs.EIO) {
            check(r > 1, "hostname n+1, got " + r);
            String hn = new String(sliceBytes(b), 0, (int) (r - 1),
                    StandardCharsets.UTF_8);
            check(!hn.isEmpty(), "hostname non-empty");
        }
    }
}
