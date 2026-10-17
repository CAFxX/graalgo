package graalgo.p3os;

import java.io.IOException;
import java.net.InetAddress;
import java.nio.ByteBuffer;
import java.nio.channels.Channel;
import java.nio.channels.Channels;
import java.nio.channels.ClosedChannelException;
import java.nio.channels.FileChannel;
import java.nio.channels.Pipe;
import java.nio.channels.ReadableByteChannel;
import java.nio.channels.SelectableChannel;
import java.nio.channels.WritableByteChannel;
import java.nio.charset.StandardCharsets;
import java.nio.file.AccessDeniedException;
import java.nio.file.DirectoryIteratorException;
import java.nio.file.DirectoryNotEmptyException;
import java.nio.file.DirectoryStream;
import java.nio.file.FileAlreadyExistsException;
import java.nio.file.FileSystemException;
import java.nio.file.FileSystemLoopException;
import java.nio.file.Files;
import java.nio.file.InvalidPathException;
import java.nio.file.LinkOption;
import java.nio.file.NoSuchFileException;
import java.nio.file.NotDirectoryException;
import java.nio.file.NotLinkException;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.nio.file.StandardCopyOption;
import java.nio.file.StandardOpenOption;
import java.nio.file.attribute.BasicFileAttributeView;
import java.nio.file.attribute.BasicFileAttributes;
import java.nio.file.attribute.FileTime;
import java.nio.file.attribute.PosixFilePermission;
import java.util.EnumSet;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;

import graalgo.sched.SchedulerCore;
import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Host-side implementation of the {@code graalgo.os.*} linkname
 * intrinsics (P3-M8, wave 2; os.md §2). One class, no real syscalls, no
 * JNI/JNA: everything is built on host JVM APIs ({@code java.nio.file},
 * {@code java.nio.channels}, {@code ProcessHandle},
 * {@code com.sun.security.auth.module.UnixSystem}).
 *
 * <p>Calling conventions (os.md §2.3; the synthetic os package's header
 * documents the Go side):
 *
 * <ul>
 *   <li>Every {@code long}-returning intrinsic returns negative =
 *       {@code -errno} (Linux errno number), non-negative = value.
 *   <li>{@code read}/{@code write}/{@code readAt}/{@code writeAt}/
 *       {@code readlink}/{@code hostname} return {@code n+1} for
 *       {@code n >= 0} bytes, {@code 0} = EOF (reads), negative =
 *       {@code -errno}. A zero-byte transfer returns 1, never confused
 *       with EOF.
 *   <li>String-returning intrinsics return {@link GoString} (raw
 *       {@code Object} flows through — {@code LinknameRootNode},
 *       boxResult=false).
 *   <li>Arguments arrive D5-boxed ({@code GoString} for strings,
 *       {@code Long} for int64, {@code GoSlice} for {@code []byte});
 *       each target adapts explicitly ({@code Number} is accepted for
 *       every integer shape, so int32-vs-int64 carrier differences can
 *       never misroute).
 *   <li>Exactly one channel op per read/write intrinsic call (R2): no
 *       helpful looping here; {@code O_APPEND} is a real
 *       {@code O_APPEND} on the fd, so each single write appends
 *       atomically — the Go side never splits one Write into two
 *       intrinsic calls.
 * </ul>
 *
 * <p>Handle model (os.md §2.2): Go's {@code *poll.FD} is replaced by a
 * Java-managed cookie ({@code long}) in a process-wide table. Cookies
 * 1/2/3 are reserved for stdin/stdout/stderr (registered by
 * {@link #init}); allocation starts at 16. {@code File.Fd()} returns the
 * cookie — it is <em>not</em> an OS fd.
 *
 * <p>Virtual cwd (os.md §2.5): the JVM cannot change the OS cwd, so
 * {@link #resolve(String)} — the single choke point every path-taking
 * intrinsic goes through — resolves relative paths against a
 * process-wide virtual cwd. {@code chdir} validates and stores the
 * physical (symlink-resolved) path, matching gc's {@code Getwd}.
 *
 * <p>Environment (os.md §2.4): the JVM has no {@code setenv(2)}, so
 * mutations live in an overlay (+ a removal set + a cleared flag) over
 * {@code System.getenv}. Documented divergence D3: invisible to native
 * code and exec'd children (no {@code os/exec} in M8).
 *
 * <p>EPIPE/SIGPIPE (R7): on EPIPE the JVM's SIGPIPE disposition is
 * restored to default and one native write is re-driven, so the kernel
 * delivers a real SIGPIPE — the process dies like gc (exit 141). If the
 * signal path is unavailable, {@code -EPIPE} is returned and the
 * SIGPIPE corpus program fails loudly instead of drifting.
 *
 * <p>State is reset by {@link #init(String[])}, called once per program
 * run by the launcher (staged P3SyncMain patch) <em>before</em> Go
 * package-var initialization runs.
 */
public final class GoOs {
    private GoOs() {}

    // Linux errno numbers (amd64 ABI) surfaced through the -errno convention.
    public static final int EPERM = 1;
    public static final int ENOENT = 2;
    public static final int EIO = 5;
    public static final int EBADF = 9;
    public static final int EACCES = 13;
    public static final int EEXIST = 17;
    public static final int EXDEV = 18;
    public static final int ENOTDIR = 20;
    public static final int EISDIR = 21;
    public static final int EINVAL = 22;
    public static final int ENFILE = 23;
    public static final int EMFILE = 24;
    public static final int EFBIG = 27;
    public static final int ENOSPC = 28;
    public static final int ESPIPE = 29;
    public static final int EROFS = 30;
    public static final int EPIPE = 32;
    public static final int ENAMETOOLONG = 36;
    public static final int ENOTEMPTY = 39;
    public static final int ELOOP = 40;
    public static final int EOPNOTSUPP = 95;

    // Handle kinds, cached in the synthetic File.kind (os.md §2.2).
    public static final int KIND_REGULAR = 1;
    public static final int KIND_DIR = 2;
    public static final int KIND_PIPE = 3;
    public static final int KIND_STDIO = 4;
    /**
     * P3-M9 (RISK-M9-2): sockets share the cookie namespace (fd-domain
     * unification) — allocated from the same NEXT_COOKIE space, closed
     * through the same close() path.
     */
    public static final int KIND_SOCKET = 5;

    // Linux open(2) flags used by the open intrinsic.
    private static final int O_RDONLY = 0;
    private static final int O_WRONLY = 1;
    private static final int O_RDWR = 2;
    private static final int O_APPEND = 1024;
    private static final int O_CREAT = 64;
    private static final int O_EXCL = 128;
    private static final int O_SYNC = 1052672;
    private static final int O_TRUNC = 512;

    // st_mode type bits (for dirNext's type chars).
    private static final int S_IFMT = 0170000;
    private static final int S_IFDIR = 0040000;
    private static final int S_IFREG = 0100000;
    private static final int S_IFLNK = 0120000;
    private static final int S_IFIFO = 0010000;
    private static final int S_IFSOCK = 0140000;
    private static final int S_IFCHR = 0060000;
    private static final int S_IFBLK = 0020000;

    // ------------------------------------------------------------------
    // Process-wide state (reset by init).
    // ------------------------------------------------------------------

    private abstract static class Handle {
        final int kind;

        Handle(int kind) {
            this.kind = kind;
        }

        abstract void close() throws IOException;
    }

    private static final class ChannelHandle extends Handle {
        final Channel ch;
        final boolean readable;
        final boolean writable;
        final Path path; // null for pipe/stdio
        final boolean closeUnderlying; // false for stdio (never close System.in/out/err)

        ChannelHandle(Channel ch, boolean readable, boolean writable,
                Path path, int kind, boolean closeUnderlying) {
            super(kind);
            this.ch = ch;
            this.readable = readable;
            this.writable = writable;
            this.path = path;
            this.closeUnderlying = closeUnderlying;
        }

        @Override
        void close() throws IOException {
            if (closeUnderlying) {
                ch.close();
            }
        }
    }

    private static final class DirHandle extends Handle {
        final Path path;
        private DirectoryStream<Path> stream;
        private Iterator<Path> it;
        // P1-5: track the logical position for SEEK_CUR.
        private long position;

        DirHandle(Path path) throws IOException {
            super(KIND_DIR);
            this.path = path;
            open();
        }

        private synchronized void open() throws IOException {
            stream = Files.newDirectoryStream(path);
            it = stream.iterator();
            position = 0;
        }

        /** Rewind iteration (dir Seek(0, SEEK_SET)). */
        synchronized void reopen() throws IOException {
            try {
                stream.close();
            } catch (IOException ignored) {
                // Best effort; reopening proceeds regardless.
            }
            open();
        }

        synchronized Iterator<Path> iterator() {
            return it;
        }

        synchronized long position() {
            return position;
        }

        synchronized void position(long p) {
            position = p;
        }

        @Override
        synchronized void close() throws IOException {
            stream.close();
        }
    }

    private static final ConcurrentHashMap<Long, Handle> HANDLES = new ConcurrentHashMap<>();
    private static final AtomicLong NEXT_COOKIE = new AtomicLong(16);
    private static volatile Path VCWD;
    private static final ConcurrentHashMap<String, String> ENV_OVERLAY = new ConcurrentHashMap<>();
    private static final Set<String> ENV_REMOVED = ConcurrentHashMap.newKeySet();
    private static volatile boolean ENV_CLEARED;
    private static volatile String[] ARGV = new String[0];
    /** Cached com.sun.security.auth.module.UnixSystem (null if unavailable). */
    private static volatile Object UNIX_SYSTEM;

    /**
     * (Re)initializes all GoOs state for one program run. Called by the
     * launcher after {@code LinknameIntrinsics.install} and before Go
     * initialization. {@code argv[0]} is the absolute, normalized path of
     * the program entry point (gc parity: argv[0] is absolute under
     * `go run`; graalgo has no binary artifact, so the entry-point file
     * path is the honest equivalent).
     *
     * <p>A static initializer seeds the same state with an empty argv so
     * the intrinsics are usable even if a caller builds (but never runs
     * through the launcher) a program.
     */
    public static void init(String[] argv) {
        resetState(argv);
    }

    static {
        resetState(new String[0]);
    }

    private static void resetState(String[] argv) {
        HANDLES.clear();
        NEXT_COOKIE.set(16);
        ENV_OVERLAY.clear();
        ENV_REMOVED.clear();
        ENV_CLEARED = false;
        ARGV = argv == null ? new String[0] : argv.clone();
        VCWD = Paths.get(System.getProperty("user.dir")).toAbsolutePath().normalize();
        // Cookies 1/2/3: stdio, never closed underneath.
        HANDLES.put(1L, new ChannelHandle(Channels.newChannel(System.in),
                true, false, null, KIND_STDIO, false));
        HANDLES.put(2L, new ChannelHandle(Channels.newChannel(System.out),
                false, true, null, KIND_STDIO, false));
        HANDLES.put(3L, new ChannelHandle(Channels.newChannel(System.err),
                false, true, null, KIND_STDIO, false));
        try {
            Class<?> uc = Class.forName("com.sun.security.auth.module.UnixSystem");
            UNIX_SYSTEM = uc.getDeclaredConstructor().newInstance();
        } catch (Throwable t) {
            UNIX_SYSTEM = null;
        }
    }

    // ------------------------------------------------------------------
    // The single errno mapper (R1): every intrinsic funnels its
    // Throwable through here. Public for the per-failure-mode unit
    // test (GoOsErrnoTest). Ordering is load-bearing: specific NIO
    // subclasses before their FileSystemException parent; the NUL-path
    // InvalidPathException (not an IOException) first.
    // ------------------------------------------------------------------

    /**
     * Maps a {@link Throwable} from a host API call to a Linux errno
     * number. This is the <em>only</em> exception→errno translation in
     * the os surface (R1); unit-tested per failure mode.
     */
    public static int errno(Throwable t) {
        if (t instanceof InvalidPathException) {
            return EINVAL; // NUL byte in path, like gc
        }
        if (t instanceof NoSuchFileException nsfe) {
            // A missing leaf is ENOENT, but a regular file (or other
            // non-directory) in a parent position is ENOTDIR, like gc.
            // The JDK reports both as NoSuchFileException, so walk up on
            // the error path only.
            return hasNonDirParent(nsfe.getFile()) ? ENOTDIR : ENOENT;
        }
        if (t instanceof AccessDeniedException) {
            return EACCES;
        }
        if (t instanceof FileAlreadyExistsException) {
            return EEXIST;
        }
        if (t instanceof NotDirectoryException) {
            return ENOTDIR;
        }
        if (t instanceof NotLinkException) {
            return EINVAL; // readlink(2) on a non-symlink (verified on this JDK)
        }
        if (t instanceof DirectoryNotEmptyException) {
            return ENOTEMPTY;
        }
        if (t instanceof FileSystemLoopException) {
            return ELOOP;
        }
        if (t instanceof ClosedChannelException) {
            return EBADF;
        }
        if (t instanceof FileSystemException fse) {
            String reason = fse.getReason();
            if (reason != null) {
                String rl = reason.toLowerCase(Locale.ROOT);
                // M10 (self-attack D3): fd exhaustion must surface as
                // EMFILE/ENFILE (gc parity), not EIO. FileChannel.open
                // reports it as FileSystemException with reason
                // "Too many open files".
                if (rl.contains("too many open files")) {
                    return rl.contains("in system") ? ENFILE : EMFILE;
                }
                if (rl.contains("too long")) {
                    return ENAMETOOLONG; // "File name too long" (verified on this JDK)
                }
                if (rl.contains("not a directory")) {
                    return ENOTDIR; // e.g. open("file/x") (verified on this JDK)
                }
            }
            return EIO;
        }
        if (t instanceof java.nio.channels.NonReadableChannelException
                || t instanceof java.nio.channels.NonWritableChannelException) {
            return EBADF;
        }
        if (t instanceof IOException ioe) {
            String m = ioe.getMessage();
            if (m != null) {
                String ml = m.toLowerCase(Locale.ROOT);
                if (ml.contains("broken pipe")) {
                    return EPIPE; // verified: "Broken pipe" on this JDK
                }
                // M10 (self-attack D3): fd exhaustion must surface as
                // EMFILE/ENFILE (gc parity), not EIO — Go programs
                // distinguish "too many open files" (e.g. netpoll
                // backoff, os.IsTimeout-style checks). The JDK reports
                // both as "Too many open files[...]".
                if (ml.contains("too many open files")) {
                    return ml.contains("in system") ? ENFILE : EMFILE;
                }
            }
            return EIO;
        }
        if (t instanceof SecurityException) {
            return EACCES;
        }
        return EIO;
    }

    /**
     * Error-path helper for the {@link #errno} mapper: reports whether
     * {@code file} has a non-directory in a parent position (gc's
     * ENOTDIR) as opposed to a genuinely missing component (ENOENT).
     * Symlinks are followed, matching path resolution.
     */
    private static boolean hasNonDirParent(String file) {
        if (file == null) {
            return false;
        }
        try {
            Path q = Paths.get(file).getParent();
            while (q != null) {
                // P2-4: use NOFOLLOW_LINKS — a dangling symlink in parent
                // position exists (as a link) but is not a directory,
                // so gc returns ENOTDIR, not ENOENT.
                if (!Files.exists(q, LinkOption.NOFOLLOW_LINKS)) {
                    return false; // genuinely missing component → ENOENT
                }
                if (!Files.isDirectory(q)) {
                    return true; // non-directory parent → ENOTDIR
                }
                q = q.getParent();
            }
        } catch (Exception e) {
            return false;
        }
        return false;
    }

    // ------------------------------------------------------------------
    // D5 adaptation helpers.
    // ------------------------------------------------------------------

    private static String goStr(Object o) {
        return new String(((GoString) o).bytes(), StandardCharsets.UTF_8);
    }

    private static long num(Object o) {
        return ((Number) o).longValue();
    }

    private static int num32(Object o) {
        return ((Number) o).intValue();
    }

    /** A Go []byte as a ByteBuffer honoring the GoSlice offset/len (§2.1). */
    /**
     * Wraps a Go []byte slice as a ByteBuffer for channel I/O. D5 stores
     * []byte in byte[] backing arrays, so the bytes are copied to a fresh
     * byte[]; for reads the caller must copy back via {@link #goBufBack}.
     */
    private static ByteBuffer goBuf(Object o) {
        if (o == null) {
            return ByteBuffer.allocate(0);
        }
        GoSlice gs = (GoSlice) o;
        byte[] arr = (byte[]) gs.array();
        int off = gs.offset();
        int len = gs.len();
        byte[] bytes = new byte[len];
        System.arraycopy(arr, off, bytes, 0, len);
        return ByteBuffer.wrap(bytes);
    }

    /**
     * Copies bytes read into a {@link #goBuf} ByteBuffer back into the Go
     * []byte's byte[] backing array. n is the number of bytes read.
     */
    private static void goBufBack(Object o, ByteBuffer bb, int n) {
        if (o == null || n <= 0) {
            return;
        }
        GoSlice gs = (GoSlice) o;
        byte[] arr = (byte[]) gs.array();
        byte[] bytes = bb.array();
        System.arraycopy(bytes, 0, arr, gs.offset(), n);
    }

    /**
     * The single choke point for the virtual cwd (os.md §2.5): every
     * path-taking intrinsic resolves through here. Package-visible for
     * the coherence test.
     */
    static Path resolve(String path) {
        Path p = Paths.get(path); // throws InvalidPathException on NUL bytes
        if (!p.isAbsolute()) {
            p = VCWD.resolve(p);
        }
        return p.normalize();
    }

    private static Set<PosixFilePermission> toPerms(int perm) {
        Set<PosixFilePermission> s = EnumSet.noneOf(PosixFilePermission.class);
        if ((perm & 0400) != 0) {
            s.add(PosixFilePermission.OWNER_READ);
        }
        if ((perm & 0200) != 0) {
            s.add(PosixFilePermission.OWNER_WRITE);
        }
        if ((perm & 0100) != 0) {
            s.add(PosixFilePermission.OWNER_EXECUTE);
        }
        if ((perm & 040) != 0) {
            s.add(PosixFilePermission.GROUP_READ);
        }
        if ((perm & 020) != 0) {
            s.add(PosixFilePermission.GROUP_WRITE);
        }
        if ((perm & 010) != 0) {
            s.add(PosixFilePermission.GROUP_EXECUTE);
        }
        if ((perm & 04) != 0) {
            s.add(PosixFilePermission.OTHERS_READ);
        }
        if ((perm & 02) != 0) {
            s.add(PosixFilePermission.OTHERS_WRITE);
        }
        if ((perm & 01) != 0) {
            s.add(PosixFilePermission.OTHERS_EXECUTE);
        }
        return s;
    }

    private static ChannelHandle channelHandle(long h) {
        Handle handle = HANDLES.get(h);
        return handle instanceof ChannelHandle ch ? ch : null;
    }

    // ------------------------------------------------------------------
    // Socket-handle interop (P3-M9, RISK-M9-2): the p3net package owns
    // socket semantics; the cookies live in this unified table.
    // ------------------------------------------------------------------

    /**
     * Registers a socket channel, returning its cookie. Sockets share
     * the file cookie namespace (fd-domain unification): {@code read},
     * {@code write} and {@code close} below operate on them, and the
     * channel must be a {@link java.nio.channels.SelectableChannel}
     * (the netpoller registers it).
     */
    public static long registerSocketChannel(java.nio.channels.Channel ch,
            boolean readable, boolean writable) {
        long h = NEXT_COOKIE.getAndIncrement();
        HANDLES.put(h, new ChannelHandle(ch, readable, writable, null, KIND_SOCKET, true));
        return h;
    }

    /**
     * Resolves a socket cookie to its channel, or null when absent or
     * not a channel handle. Package-visible to p3net via this public
     * accessor (GoNet lives in another package).
     */
    public static java.nio.channels.Channel socketChannel(long cookie) {
        ChannelHandle ch = channelHandle(cookie);
        return (ch != null && ch.kind == KIND_SOCKET) ? ch.ch : null;
    }

    /**
     * Swaps a socket handle's channel (the {@code listen()} path swaps
     * an idle {@code SocketChannel} for a {@code ServerSocketChannel}).
     * The previous channel is closed; the cookie is unchanged.
     */
    public static void replaceSocketChannel(long cookie, java.nio.channels.Channel ch,
            boolean readable, boolean writable) {
        Handle prev = HANDLES.replace(cookie,
                new ChannelHandle(ch, readable, writable, null, KIND_SOCKET, true));
        if (prev instanceof ChannelHandle oldCh) {
            try {
                oldCh.ch.close();
            } catch (java.io.IOException ignored) {
                // Best effort; the replacement is what matters.
            }
        }
    }

    // ------------------------------------------------------------------
    // Files: open/close/read/write/readAt/writeAt/seek/truncate/sync.
    // ------------------------------------------------------------------

    /** graalgo.os.open */
    public static long open(Object pathArg, Object flagsArg, Object permArg) {
        final Path p;
        try {
            p = resolve(goStr(pathArg));
        } catch (InvalidPathException e) {
            return -EINVAL;
        }
        int flags = num32(flagsArg);
        long perm = num(permArg);
        if ((perm & ~0777L) != 0) {
            return -EINVAL; // setuid/setgid/sticky: fail-fast, never mask (Go side also guards)
        }
        boolean create = (flags & O_CREAT) != 0;
        boolean excl = (flags & O_EXCL) != 0;
        boolean trunc = (flags & O_TRUNC) != 0;
        boolean append = (flags & O_APPEND) != 0;
        boolean sync = (flags & O_SYNC) != 0;
        int accmode = flags & 3;
        boolean write = accmode != O_RDONLY;
        try {
            // Stat-kind first: directories get a DirectoryStream handle.
            // P2-1: follow symlinks — a symlink to a directory opens as a
            // directory (gc returns EISDIR on read, not EIO).
            boolean isDir = Files.isDirectory(p);
            if (isDir) {
                if (write || create || trunc || append) {
                    return -EISDIR;
                }
                DirHandle dh = new DirHandle(p);
                long h = NEXT_COOKIE.getAndIncrement();
                HANDLES.put(h, dh);
                return h;
            }
            Set<StandardOpenOption> opts = EnumSet.noneOf(StandardOpenOption.class);
            if (accmode == O_RDONLY) {
                opts.add(StandardOpenOption.READ);
            } else if (accmode == O_WRONLY) {
                opts.add(StandardOpenOption.WRITE);
            } else {
                opts.add(StandardOpenOption.READ);
                opts.add(StandardOpenOption.WRITE);
            }
            if (append) {
                opts.add(StandardOpenOption.APPEND);
            }
            if (create) {
                opts.add(excl ? StandardOpenOption.CREATE_NEW : StandardOpenOption.CREATE);
            }
            if (trunc) {
                opts.add(StandardOpenOption.TRUNCATE_EXISTING);
            }
            if (sync) {
                opts.add(StandardOpenOption.SYNC);
            }
            FileChannel ch = FileChannel.open(p, opts);
            if (create) {
                // D1: apply the requested 0777 bits exactly (the JVM
                // cannot read umask, so gc's perm&~umask is approximated
                // by the exact perm — documented in the synthetic os).
                try {
                    Files.setPosixFilePermissions(p, toPerms((int) (perm & 0777)));
                } catch (UnsupportedOperationException uoe) {
                    // Non-POSIX filesystem: keep the created defaults.
                }
            }
            long h = NEXT_COOKIE.getAndIncrement();
            HANDLES.put(h, new ChannelHandle(ch, accmode != O_WRONLY, accmode != O_RDONLY,
                    p, KIND_REGULAR, true));
            return h;
        } catch (IOException | InvalidPathException e) {
            return -errno(e);
        } catch (SecurityException e) {
            return -EACCES;
        }
    }

    /** graalgo.os.close */
    public static long close(Object hArg) {
        long cookie = num(hArg);
        Handle handle = HANDLES.remove(cookie);
        if (handle == null) {
            return -EBADF;
        }
        if (handle.kind == KIND_SOCKET) {
            // P3-M9 (RISK-M9-2): evict netpoller state first — a close
            // that bypasses runtime_pollClose must still wake parked
            // waiters (with pollErrClosing) and deregister the channel.
            // Idempotent: the normal FD.Close path already unblocked.
            graalgo.p3net.GoNetpoll.evictForClose(cookie);
        }
        try {
            handle.close();
        } catch (IOException e) {
            return -errno(e);
        }
        return 0;
    }

    /**
     * Slot-releasing bracket for blocking syscalls (R2 P1-5).
     *
     * <p>SPEC §3.5 / GG-SCHED-0004 `[DECIDED]`: "All blocking in the
     * runtime/stdlib adaptation layer SHALL go through scheduler-aware
     * primitives" — blocking I/O uses the slot-releasing wrapper
     * ({@code ioEnter} releases the in-flight slot, {@code ioExit}
     * re-acquires it), so a goroutine blocked in file/pipe I/O does not
     * count against the GOMAXPROCS cap and cannot starve runnable
     * goroutines (gc entersyscall parity).
     *
     * <p>Design decision (this lane's deliverable): wire the discipline
     * at the blocking-syscall sites ({@link #read}, {@link #write},
     * {@link #readAt}, {@link #writeAt}, {@link #truncate},
     * {@link #sync}) rather than scoping the docs to netpoller paths —
     * the machinery (drainer IO lanes, {@code GoroutineHandle.inIo},
     * JFR I/O events, {@code IoSlotsTest}) exists precisely for this,
     * and leaving it with zero consumers would be dead machinery.
     * Metadata syscalls (stat/open/close/mkdir/...) are excluded: they
     * cannot block indefinitely (the same cut gc makes).
     *
     * <p>Use via try-with-resources so the pairing is exception-safe
     * (the drainer fail-fasts on an unpaired {@code ioEnter} at goroutine
     * exit — a missed {@code ioExit} would be a loud halt, never silent
     * corruption).
     */
    private static final class IoBracket implements AutoCloseable {
        private final SchedulerCore sched;

        private IoBracket(SchedulerCore s) {
            sched = s;
            if (s != null) {
                s.ioEnter();
            }
        }

        @Override
        public void close() {
            if (sched != null) {
                sched.ioExit();
            }
        }

        /**
         * Opens the bracket for a blocking syscall on {@code ch}, or a
         * no-op bracket when bracketing cannot apply:
         *
         * <ul>
         *   <li>non-blocking {@link SelectableChannel} (sockets are
         *   always non-blocking underneath — readiness waits park via
         *   the netpoller instead; bracketing would add a drainer
         *   round-trip to every non-blocking read for zero benefit).
         *   Blocking mode is fixed at registration and never toggled
         *   afterwards, so the check is stable;</li>
         *   <li>caller not on a scheduler goroutine (tests, launchers,
         *   scheduler-less Phase-1 runs): the plain blocking call, as
         *   before — no fail-fast, no behavior change off-goroutine.</li>
         * </ul>
         */
        static IoBracket enter(ChannelHandle ch) {
            return new IoBracket(ioSchedulerFor(ch));
        }

        private static SchedulerCore ioSchedulerFor(ChannelHandle ch) {
            if (ch.ch instanceof SelectableChannel sc && !sc.isBlocking()) {
                return null;
            }
            SchedulerCore s = SchedulerCore.installedOrNull();
            if (s == null || SchedulerCore.currentGoroutineOrNull() == null) {
                return null;
            }
            return s;
        }
    }

    /** graalgo.os.read — exactly one channel read (R2). */
    public static long read(Object hArg, Object bArg) {
        ChannelHandle ch = channelHandle(num(hArg));
        if (ch == null) {
            Handle handle = HANDLES.get(num(hArg));
            return handle instanceof DirHandle ? -EISDIR : -EBADF;
        }
        if (!ch.readable) {
            return -EBADF;
        }
        ByteBuffer bb = goBuf(bArg);
        if (!(ch.ch instanceof ReadableByteChannel)) {
            return -EBADF;
        }
        // R2 P1-5: release the in-flight slot across the blocking read.
        try (IoBracket io = IoBracket.enter(ch)) {
            try {
                int n = ((ReadableByteChannel) ch.ch).read(bb);
                if (n < 0) {
                    return 0; // EOF
                }
                goBufBack(bArg, bb, n);
                return (long) n + 1;
            } catch (IOException e) {
                return -errno(e);
            }
        }
    }

    /** graalgo.os.write — exactly one channel write (R2); EPIPE → R7. */
    public static long write(Object hArg, Object bArg) {
        ChannelHandle ch = channelHandle(num(hArg));
        if (ch == null) {
            return -EBADF;
        }
        if (!ch.writable) {
            return -EBADF;
        }
        ByteBuffer bb = goBuf(bArg);
        if (!(ch.ch instanceof WritableByteChannel)) {
            return -EBADF;
        }
        WritableByteChannel wch = (WritableByteChannel) ch.ch;
        // R2 P1-5: release the in-flight slot across the blocking write
        // (the EPIPE/SIGPIPE retry below stays inside the bracket).
        try (IoBracket io = IoBracket.enter(ch)) {
            try {
                int n = wch.write(bb);
                return (long) n + 1;
            } catch (IOException e) {
                if (errno(e) == EPIPE) {
                    // R7: gc raises SIGPIPE (death) only for fd 1/2; other
                    // fds get EPIPE as an error. Match: SIGPIPE dance only
                    // for stdio handles, error return otherwise.
                    if (ch.kind == KIND_STDIO) {
                        onEpipe(wch, bb);
                        // Still alive: the signal path was unavailable — loud
                        // fallback (R7): surface EPIPE; the SIGPIPE corpus
                        // program then fails loudly instead of drifting.
                    }
                    return -EPIPE;
                }
                return -errno(e);
            }
        }
    }

    /**
     * EPIPE path (R7): gc raises SIGPIPE on the first EPIPE. The JVM
     * ignores SIGPIPE, so restore the default disposition and re-drive
     * one native write — the kernel then delivers a real SIGPIPE and
     * the process dies like gc (exit 141). Guarded: if the signal API
     * is unavailable this returns normally and the caller surfaces
     * {@code -EPIPE}.
     */
    private static void onEpipe(WritableByteChannel ch, ByteBuffer bb) {
        try {
            Class<?> sigCls = Class.forName("sun.misc.Signal");
            Class<?> handlerCls = Class.forName("sun.misc.SignalHandler");
            Object sigPipe = sigCls.getConstructor(String.class).newInstance("PIPE");
            Object sigDfl = handlerCls.getField("SIG_DFL").get(null);
            sigCls.getMethod("handle", sigCls, handlerCls).invoke(null, sigPipe, sigDfl);
        } catch (Throwable t) {
            return; // signal path unavailable — caller returns EPIPE
        }
        try {
            ch.write(bb);
        } catch (IOException retry) {
            // The retry should have died via SIGPIPE; if it threw
            // instead, fall through to the EPIPE return.
        }
    }

    /** graalgo.os.readAt */
    public static long readAt(Object hArg, Object bArg, Object posArg) {
        ChannelHandle ch = channelHandle(num(hArg));
        if (ch == null || !(ch.ch instanceof FileChannel fc)) {
            return -ESPIPE;
        }
        if (!ch.readable) {
            return -EBADF;
        }
        ByteBuffer bb = goBuf(bArg);
        // R2 P1-5: release the in-flight slot across the blocking read.
        try (IoBracket io = IoBracket.enter(ch)) {
            try {
                int n = fc.read(bb, num(posArg));
                if (n < 0) {
                    return 0; // EOF
                }
                goBufBack(bArg, bb, n);
                return (long) n + 1;
            } catch (IOException e) {
                return -errno(e);
            } catch (IllegalArgumentException e) {
                return -EINVAL;
            }
        }
    }

    /** graalgo.os.writeAt — EPIPE → R7 like write. */
    public static long writeAt(Object hArg, Object bArg, Object posArg) {
        ChannelHandle ch = channelHandle(num(hArg));
        if (ch == null || !(ch.ch instanceof FileChannel fc)) {
            return -ESPIPE;
        }
        if (!ch.writable) {
            return -EBADF;
        }
        ByteBuffer bb = goBuf(bArg);
        long pos = num(posArg);
        // R2 P1-5: release the in-flight slot across the blocking write.
        try (IoBracket io = IoBracket.enter(ch)) {
            try {
                int n = fc.write(bb, pos);
                return (long) n + 1;
            } catch (IOException e) {
                if (errno(e) == EPIPE) {
                    onEpipe(fc, bb);
                    return -EPIPE;
                }
                return -errno(e);
            } catch (IllegalArgumentException e) {
                return -EINVAL;
            }
        }
    }

    /** graalgo.os.seek */
    public static long seek(Object hArg, Object offArg, Object whenceArg) {
        long h = num(hArg);
        Handle handle = HANDLES.get(h);
        if (handle == null) {
            return -EBADF;
        }
        long off = num(offArg);
        long whence = num(whenceArg);
        if (handle instanceof DirHandle dh) {
            try {
                if (off == 0 && whence == 0) {
                    dh.reopen();
                    return 0;
                }
                // P1-5: gc semantics for dir seek (probed go1.27.1):
                // - SEEK_END on a directory → EINVAL (only SEEK_SET to 0
                //   is meaningful; gc does not stat the dir).
                // - Negative offset → EINVAL (at the Java boundary, before
                //   the value crosses to Go where it would misread as
                //   -EPERM).
                // - SEEK_CUR uses the tracked position.
                if (whence == 2) {
                    return -EINVAL;
                }
                long newPos;
                if (whence == 0) {
                    if (off < 0) {
                        return -EINVAL;
                    }
                    newPos = off;
                } else if (whence == 1) {
                    newPos = dh.position() + off;
                    if (newPos < 0) {
                        return -EINVAL;
                    }
                } else {
                    return -EINVAL;
                }
                dh.position(newPos);
                return newPos;
            } catch (IOException e) {
                return -errno(e);
            }
        }
        ChannelHandle ch = (ChannelHandle) handle;
        if (!(ch.ch instanceof FileChannel fc)) {
            return -ESPIPE;
        }
        try {
            long newpos;
            if (whence == 0) {
                newpos = off;
            } else if (whence == 1) {
                newpos = fc.position() + off;
            } else if (whence == 2) {
                newpos = fc.size() + off;
            } else {
                return -EINVAL;
            }
            if (newpos < 0) {
                return -EINVAL;
            }
            fc.position(newpos);
            return newpos;
        } catch (IOException e) {
            return -errno(e);
        } catch (IllegalArgumentException e) {
            return -EINVAL;
        }
    }

    /** graalgo.os.truncate */
    public static long truncate(Object hArg, Object sizeArg) {
        ChannelHandle ch = channelHandle(num(hArg));
        if (ch == null || !(ch.ch instanceof FileChannel fc)) {
            return -EINVAL;
        }
        long size = num(sizeArg);
        if (size < 0) {
            return -EINVAL;
        }
        // R2 P1-5: the extension path does a real (blocking) write.
        try (IoBracket io = IoBracket.enter(ch)) {
            try {
                ftruncate(fc, size);
                return 0;
            } catch (IOException e) {
                return -errno(e);
            } catch (IllegalArgumentException e) {
                return -EINVAL;
            }
        }
    }

    /**
     * ftruncate(2) semantics on a FileChannel. FileChannel.truncate()
     * only shrinks — its contract is "if the given size is greater than
     * or equal to the file's current size then the file size is
     * unchanged" — while ftruncate extends sparsely. Extension is done
     * with a positional single-zero write at size-1, which the
     * filesystem materializes exactly like ftruncate (the corpus
     * sparse-extension checks read the gap as zeros). The channel
     * position is preserved: ftruncate does not move the file offset.
     */
    private static void ftruncate(FileChannel fc, long size) throws IOException {
        long cur = fc.size();
        if (size == cur) {
            return;
        }
        if (size < cur) {
            fc.truncate(size);
            return;
        }
        // Extend: size > cur >= 0, so size >= 1 and size-1 is a valid
        // position. A positional write does not move the channel's own
        // position, but save/restore anyway for safety.
        long pos = fc.position();
        try {
            fc.write(ByteBuffer.allocate(1), size - 1);
        } finally {
            fc.position(pos);
        }
    }

    /** graalgo.os.sync */
    public static long sync(Object hArg) {
        ChannelHandle ch = channelHandle(num(hArg));
        if (ch == null || !(ch.ch instanceof FileChannel fc)) {
            return -EINVAL;
        }
        // R2 P1-5: fsync blocks; release the in-flight slot across it.
        try (IoBracket io = IoBracket.enter(ch)) {
            try {
                fc.force(true);
                return 0;
            } catch (IOException e) {
                return -errno(e);
            }
        }
    }

    /** graalgo.os.pipe — consecutive cookies (r, r+1), one intrinsic. */
    public static long pipe() {
        try {
            Pipe p = Pipe.open();
            long r = NEXT_COOKIE.getAndAdd(2);
            HANDLES.put(r, new ChannelHandle(p.source(), true, false, null, KIND_PIPE, true));
            HANDLES.put(r + 1, new ChannelHandle(p.sink(), false, true, null, KIND_PIPE, true));
            return r;
        } catch (IOException e) {
            return -errno(e);
        }
    }

    /** graalgo.os.stdio — cookies 1/2/3, pre-registered by init(). */
    public static long stdio(Object fdArg) {
        long fd = num(fdArg);
        if (fd < 0 || fd > 2) {
            return -EBADF;
        }
        return fd + 1;
    }

    /** graalgo.os.handleKind */
    public static long handleKind(Object hArg) {
        Handle handle = HANDLES.get(num(hArg));
        if (handle == null) {
            return -EBADF;
        }
        return handle.kind;
    }

    // ------------------------------------------------------------------
    // Stat.
    // ------------------------------------------------------------------

    private static LinkOption[] statOpts(Object followArg) {
        return num(followArg) == 1
                ? new LinkOption[0]
                : new LinkOption[] {LinkOption.NOFOLLOW_LINKS};
    }

    /** graalgo.os.statMode — full st_mode (type+perm bits). */
    public static long statMode(Object pathArg, Object followArg) {
        final Path p;
        try {
            p = resolve(goStr(pathArg));
        } catch (InvalidPathException e) {
            return -EINVAL;
        }
        try {
            int mode = (Integer) Files.getAttribute(p, "unix:mode", statOpts(followArg));
            return mode & 0xFFFFFFFFL;
        } catch (IOException | InvalidPathException e) {
            return -errno(e);
        } catch (SecurityException e) {
            return -EACCES;
        } catch (UnsupportedOperationException e) {
            return -EOPNOTSUPP;
        }
    }

    /** graalgo.os.statSize */
    public static long statSize(Object pathArg, Object followArg) {
        final Path p;
        try {
            p = resolve(goStr(pathArg));
        } catch (InvalidPathException e) {
            return -EINVAL;
        }
        try {
            BasicFileAttributes a =
                    Files.readAttributes(p, BasicFileAttributes.class, statOpts(followArg));
            return a.size();
        } catch (IOException | InvalidPathException e) {
            return -errno(e);
        } catch (SecurityException e) {
            return -EACCES;
        }
    }

    /** graalgo.os.statMtimeNs */
    public static long statMtimeNs(Object pathArg, Object followArg) {
        final Path p;
        try {
            p = resolve(goStr(pathArg));
        } catch (InvalidPathException e) {
            return -EINVAL;
        }
        try {
            BasicFileAttributes a =
                    Files.readAttributes(p, BasicFileAttributes.class, statOpts(followArg));
            return a.lastModifiedTime().to(TimeUnit.NANOSECONDS);
        } catch (IOException | InvalidPathException e) {
            return -errno(e);
        } catch (SecurityException e) {
            return -EACCES;
        }
    }

    /** graalgo.os.statDev */
    public static long statDev(Object pathArg, Object followArg) {
        final Path p;
        try {
            p = resolve(goStr(pathArg));
        } catch (InvalidPathException e) {
            return -EINVAL;
        }
        try {
            Object dev = Files.getAttribute(p, "unix:dev", statOpts(followArg));
            return ((Number) dev).longValue();
        } catch (IOException | InvalidPathException e) {
            return -errno(e);
        } catch (SecurityException e) {
            return -EACCES;
        } catch (UnsupportedOperationException e) {
            return -EOPNOTSUPP;
        }
    }

    /** graalgo.os.statIno */
    public static long statIno(Object pathArg, Object followArg) {
        final Path p;
        try {
            p = resolve(goStr(pathArg));
        } catch (InvalidPathException e) {
            return -EINVAL;
        }
        try {
            Object ino = Files.getAttribute(p, "unix:ino", statOpts(followArg));
            return ((Number) ino).longValue();
        } catch (IOException | InvalidPathException e) {
            return -errno(e);
        } catch (SecurityException e) {
            return -EACCES;
        } catch (UnsupportedOperationException e) {
            return -EOPNOTSUPP;
        }
    }

    // ------------------------------------------------------------------
    // Directory iteration.
    // ------------------------------------------------------------------

    private static char typeChar(Path e) {
        try {
            int mode =
                    (Integer) Files.getAttribute(e, "unix:mode", LinkOption.NOFOLLOW_LINKS);
            switch (mode & S_IFMT) {
                case S_IFDIR:
                    return 'd';
                case S_IFREG:
                    return 'f';
                case S_IFLNK:
                    return 'l';
                case S_IFIFO:
                    return 'p';
                case S_IFSOCK:
                    return 's';
                case S_IFCHR:
                    return 'c';
                case S_IFBLK:
                    return 'b';
                default:
                    return '?';
            }
        } catch (IOException | RuntimeException ex) {
            return '?';
        }
    }

    /**
     * graalgo.os.dirNext — reads up to {@code max} entries from the
     * dir handle's stream as {@code name\0type} records ({@code d f l p
     * s c b}, {@code ?} when the type is unreadable). Returns an empty
     * string at end-of-stream; a two-NUL string when iteration itself
     * failed (the Go side maps it to EIO). Never throws.
     */
    public static Object dirNext(Object hArg, Object maxArg) {
        Handle handle = HANDLES.get(num(hArg));
        if (!(handle instanceof DirHandle dh)) {
            return GoString.EMPTY;
        }
        int max = num32(maxArg);
        StringBuilder sb = new StringBuilder();
        int count = 0;
        try {
            synchronized (dh) {
                Iterator<Path> it = dh.iterator();
                while (count < max && it.hasNext()) {
                    Path e = it.next();
                    Path fn = e.getFileName();
                    sb.append(fn == null ? e.toString() : fn.toString())
                            .append('\0')
                            .append(typeChar(e));
                    count++;
                }
            }
        } catch (DirectoryIteratorException e) {
            return new GoString(new byte[] {0, 0});
        }
        return GoString.fromUtf8(sb.toString());
    }

    // ------------------------------------------------------------------
    // Path operations.
    // ------------------------------------------------------------------

    /** graalgo.os.mkdir */
    public static long mkdir(Object pathArg, Object permArg) {
        final Path p;
        try {
            p = resolve(goStr(pathArg));
        } catch (InvalidPathException e) {
            return -EINVAL;
        }
        try {
            Files.createDirectory(p);
            try {
                Files.setPosixFilePermissions(p, toPerms(num32(permArg) & 0777));
            } catch (UnsupportedOperationException uoe) {
                // Non-POSIX filesystem: keep the created defaults.
            }
            return 0;
        } catch (IOException e) {
            return -errno(e);
        } catch (SecurityException e) {
            return -EACCES;
        }
    }

    /** graalgo.os.remove */
    public static long remove(Object pathArg) {
        final Path p;
        try {
            p = resolve(goStr(pathArg));
        } catch (InvalidPathException e) {
            return -EINVAL;
        }
        try {
            Files.delete(p);
            return 0;
        } catch (IOException e) {
            return -errno(e);
        } catch (SecurityException e) {
            return -EACCES;
        }
    }

    /** graalgo.os.rename — EXDEV pre-check before attempting the move. */
    public static long rename(Object oldArg, Object newArg) {
        final Path o;
        final Path n;
        try {
            o = resolve(goStr(oldArg));
            n = resolve(goStr(newArg));
        } catch (InvalidPathException e) {
            return -EINVAL;
        }
        try {
            // EXDEV pre-check on the destination's parent (the
            // destination itself usually does not exist yet).
            Path dstParent = n.getParent();
            if (dstParent == null) {
                dstParent = n;
            }
            if (!Files.getFileStore(o).equals(Files.getFileStore(dstParent))) {
                return -EXDEV;
            }
        } catch (IOException e) {
            return -errno(e); // e.g. source missing → ENOENT
        }
        try {
            Files.move(o, n, StandardCopyOption.REPLACE_EXISTING);
            return 0;
        } catch (IOException e) {
            return -errno(e);
        } catch (SecurityException e) {
            return -EACCES;
        }
    }

    /** graalgo.os.symlink — the target is stored verbatim, never resolved. */
    public static long symlink(Object oldArg, Object newArg) {
        final Path n;
        final String target;
        try {
            target = goStr(oldArg);
            n = resolve(goStr(newArg));
        } catch (InvalidPathException e) {
            return -EINVAL;
        }
        try {
            Files.createSymbolicLink(n, Paths.get(target));
            return 0;
        } catch (IOException e) {
            return -errno(e);
        } catch (SecurityException e) {
            return -EACCES;
        }
    }

    /** graalgo.os.readlink — n+1 encoding; buf too small → partial fill. */
    public static long readlink(Object pathArg, Object bArg) {
        final Path p;
        try {
            p = resolve(goStr(pathArg));
        } catch (InvalidPathException e) {
            return -EINVAL;
        }
        try {
            byte[] bytes = Files.readSymbolicLink(p).toString().getBytes(StandardCharsets.UTF_8);
            ByteBuffer bb = goBuf(bArg);
            int n = Math.min(bb.remaining(), bytes.length);
            bb.put(bytes, 0, n);
            goBufBack(bArg, bb, n);
            return (long) n + 1;
        } catch (IOException e) {
            return -errno(e);
        } catch (SecurityException e) {
            return -EACCES;
        }
    }

    /** graalgo.os.link */
    public static long link(Object oldArg, Object newArg) {
        final Path o;
        final Path n;
        try {
            o = resolve(goStr(oldArg));
            n = resolve(goStr(newArg));
        } catch (InvalidPathException e) {
            return -EINVAL;
        }
        try {
            Files.createLink(n, o);
            return 0;
        } catch (IOException e) {
            return -errno(e);
        } catch (SecurityException e) {
            return -EACCES;
        }
    }

    /** graalgo.os.chmod — 0777 subset only (Go side pre-checks). */
    public static long chmod(Object pathArg, Object permArg) {
        final Path p;
        try {
            p = resolve(goStr(pathArg));
        } catch (InvalidPathException e) {
            return -EINVAL;
        }
        try {
            Files.setPosixFilePermissions(p, toPerms(num32(permArg) & 0777));
            return 0;
        } catch (IOException e) {
            return -errno(e);
        } catch (SecurityException e) {
            return -EACCES;
        } catch (UnsupportedOperationException e) {
            return -EOPNOTSUPP;
        }
    }

    /** graalgo.os.chtimes — nanosecond atime/mtime, follows symlinks like gc. */
    public static long chtimes(Object pathArg, Object atimeArg, Object mtimeArg) {
        final Path p;
        try {
            p = resolve(goStr(pathArg));
        } catch (InvalidPathException e) {
            return -EINVAL;
        }
        try {
            BasicFileAttributeView view =
                    Files.getFileAttributeView(p, BasicFileAttributeView.class);
            view.setTimes(
                    FileTime.from(num(mtimeArg), TimeUnit.NANOSECONDS),
                    FileTime.from(num(atimeArg), TimeUnit.NANOSECONDS),
                    null);
            return 0;
        } catch (IOException e) {
            return -errno(e);
        } catch (SecurityException e) {
            return -EACCES;
        } catch (IllegalArgumentException e) {
            return -EINVAL;
        }
    }

    /** graalgo.os.truncatePath */
    public static long truncatePath(Object pathArg, Object sizeArg) {
        final Path p;
        try {
            p = resolve(goStr(pathArg));
        } catch (InvalidPathException e) {
            return -EINVAL;
        }
        long size = num(sizeArg);
        if (size < 0) {
            return -EINVAL;
        }
        try (FileChannel ch = FileChannel.open(p, StandardOpenOption.WRITE)) {
            ftruncate(ch, size);
            return 0;
        } catch (IOException e) {
            return -errno(e);
        } catch (SecurityException e) {
            return -EACCES;
        } catch (IllegalArgumentException e) {
            return -EINVAL;
        }
    }

    // ------------------------------------------------------------------
    // Virtual cwd.
    // ------------------------------------------------------------------

    /** graalgo.os.getcwd — the virtual cwd. */
    public static Object getcwd() {
        return GoString.fromUtf8(VCWD.toString());
    }

    /** graalgo.os.chdir — validates the target, stores the physical path. */
    public static long chdir(Object pathArg) {
        final Path p;
        try {
            p = resolve(goStr(pathArg));
        } catch (InvalidPathException e) {
            return -EINVAL;
        }
        try {
            // isDirectory follows symlinks, like gc's chdir.
            if (!Files.isDirectory(p)) {
                if (!Files.exists(p)) {
                    return -ENOENT;
                }
                return -ENOTDIR;
            }
            VCWD = p.toRealPath();
            return 0;
        } catch (IOException e) {
            return -errno(e);
        } catch (SecurityException e) {
            return -EACCES;
        }
    }

    // ------------------------------------------------------------------
    // Environment (D3: overlay over System.getenv — invisible to native
    // code and exec'd children; no os/exec in M8).
    // ------------------------------------------------------------------

    /** graalgo.os.getenvFound — 1 if the key is set in the overlay view. */
    public static long getenvFound(Object keyArg) {
        String k = goStr(keyArg);
        if (ENV_REMOVED.contains(k)) {
            return 0;
        }
        if (ENV_OVERLAY.containsKey(k)) {
            return 1;
        }
        if (!ENV_CLEARED && System.getenv(k) != null) {
            return 1;
        }
        return 0;
    }

    /** graalgo.os.getenv */
    public static Object getenv(Object keyArg) {
        String k = goStr(keyArg);
        if (!ENV_REMOVED.contains(k)) {
            String v = ENV_OVERLAY.get(k);
            if (v != null) {
                return GoString.fromUtf8(v);
            }
            if (!ENV_CLEARED) {
                v = System.getenv(k);
                if (v != null) {
                    return GoString.fromUtf8(v);
                }
            }
        }
        return GoString.EMPTY;
    }

    /** graalgo.os.setenv */
    public static long setenv(Object keyArg, Object valArg) {
        String k = goStr(keyArg);
        ENV_REMOVED.remove(k);
        ENV_OVERLAY.put(k, goStr(valArg));
        // fix-linkname: GODEBUG changes re-invoke internal/godebug's
        // update callback (gc: runtime calls update(def, env) on change).
        if (k.equals("GODEBUG")) {
            graalgo.runtime.GoDebug.onGodebugChange(goStr(valArg));
        }
        return 0;
    }

    /** graalgo.os.unsetenv */
    public static long unsetenv(Object keyArg) {
        String k = goStr(keyArg);
        ENV_OVERLAY.remove(k);
        ENV_REMOVED.add(k);
        // fix-linkname: see setenv.
        if (k.equals("GODEBUG")) {
            graalgo.runtime.GoDebug.onGodebugChange("");
        }
        return 0;
    }

    /** graalgo.os.clearenv */
    public static long clearenv() {
        ENV_OVERLAY.clear();
        ENV_REMOVED.clear();
        ENV_CLEARED = true;
        // fix-linkname: see setenv.
        graalgo.runtime.GoDebug.onGodebugChange("");
        return 0;
    }

    /** graalgo.os.environ — NUL-joined k=v pairs, deterministic key order. */
    public static Object environ() {
        // P2-5: gc preserves OS environ order; we use LinkedHashMap to
        // preserve System.getenv() order, with overlay entries appended.
        // This is a documented divergence: the exact order may differ
        // from the OS (JVM-dependent), and overlay mutations append.
        Map<String, String> merged = new LinkedHashMap<>();
        if (!ENV_CLEARED) {
            merged.putAll(System.getenv());
        }
        merged.putAll(ENV_OVERLAY);
        for (String k : ENV_REMOVED) {
            merged.remove(k);
        }
        StringBuilder sb = new StringBuilder();
        for (Map.Entry<String, String> e : merged.entrySet()) {
            sb.append(e.getKey()).append('=').append(e.getValue()).append('\0');
        }
        return GoString.fromUtf8(sb.toString());
    }

    // ------------------------------------------------------------------
    // Identity, hostname, exit, argv.
    // ------------------------------------------------------------------

    private static long unixLong(String method) {
        Object us = UNIX_SYSTEM;
        if (us == null) {
            return -1;
        }
        try {
            return (Long) us.getClass().getMethod(method).invoke(us);
        } catch (Throwable t) {
            return -1;
        }
    }

    /** graalgo.os.getpid */
    public static long getpid() {
        return ProcessHandle.current().pid();
    }

    /** graalgo.os.getppid */
    public static long getppid() {
        return ProcessHandle.current().parent().map(ProcessHandle::pid).orElse(0L);
    }

    /** graalgo.os.getuid */
    public static long getuid() {
        return unixLong("getUid");
    }

    /**
     * graalgo.os.geteuid — no public JVM API exposes the effective uid;
     * returns the real uid, which is equal absent setuid (no setuid
     * possible under the JVM).
     */
    public static long geteuid() {
        return unixLong("getUid");
    }

    /** graalgo.os.getgid */
    public static long getgid() {
        return unixLong("getGid");
    }

    /**
     * graalgo.os.getegid — same approximation as {@link #geteuid}.
     */
    public static long getegid() {
        return unixLong("getGid");
    }

    /** graalgo.os.getgroups — NUL-joined decimal gids. */
    public static Object getgroups() {
        StringBuilder sb = new StringBuilder();
        Object us = UNIX_SYSTEM;
        if (us != null) {
            try {
                long[] gs = (long[]) us.getClass().getMethod("getGroups").invoke(us);
                for (int i = 0; i < gs.length; i++) {
                    if (i > 0) {
                        sb.append('\0');
                    }
                    sb.append(gs[i]);
                }
            } catch (Throwable t) {
                // Leave the list empty.
            }
        }
        return GoString.fromUtf8(sb.toString());
    }

    /**
     * graalgo.os.hostname — fills the buffer; the Go side falls back to
     * {@code /proc/sys/kernel/hostname} if this returns -EIO (e.g. the
     * host name is unresolvable on this machine).
     */
    public static long hostname(Object bArg) {
        // P2-6: gc reads the kernel hostname without DNS. InetAddress.getLocalHost()
        // can trigger a blocking DNS lookup; read /proc/sys/kernel/hostname
        // (Linux) directly, with InetAddress as fallback.
        String hn = null;
        try {
            hn = Files.readString(Path.of("/proc/sys/kernel/hostname")).trim();
        } catch (Exception ignored) {
            // Fall through to InetAddress.
        }
        if (hn == null || hn.isEmpty()) {
            try {
                hn = InetAddress.getLocalHost().getHostName();
            } catch (Exception e) {
                return -EIO;
            }
        }
        try {
            byte[] bytes = hn.getBytes(StandardCharsets.UTF_8);
            ByteBuffer bb = goBuf(bArg);
            int n = Math.min(bb.remaining(), bytes.length);
            bb.put(bytes, 0, n);
            goBufBack(bArg, bb, n);
            return (long) n + 1;
        } catch (Exception e) {
            return -EIO;
        }
    }

    /** graalgo.os.exit */
    public static long exit(Object codeArg) {
        System.exit(num32(codeArg));
        return 0; // unreachable
    }

    /** graalgo.os.argc */
    public static long argc() {
        return ARGV.length;
    }

    /** graalgo.os.argv */
    public static Object argv(Object iArg) {
        int i = num32(iArg);
        if (i < 0 || i >= ARGV.length) {
            return GoString.EMPTY;
        }
        return GoString.fromUtf8(ARGV[i]);
    }

    private static final java.security.SecureRandom SECURE_RANDOM =
        new java.security.SecureRandom();

    /** graalgo.os.getrandom — fill the GoSlice with cryptographically
     * secure random bytes (P3-M8: crypto/rand entropy). Returns the
     * number of bytes written. */
    public static long getrandom(Object bArg) {
        ByteBuffer bb = goBuf(bArg);
        int n = bb.remaining();
        if (n == 0) {
            return 0;
        }
        byte[] tmp = new byte[n];
        SECURE_RANDOM.nextBytes(tmp);
        bb.put(tmp);
        goBufBack(bArg, bb, n);
        return n;
    }
}
