package abispike;

import abispike.Mem.Addr;
import abispike.Mem.Blob;
import abispike.Syscalls.Result;
import abispike.Values.Flat;
import abispike.Values.Layout;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Set;
import java.util.TreeSet;

/**
 * P2 prototype checks. Every call below has the shape Go source has:
 * {@code Syscall6(trap, a1..a6 uintptr)} with pointers passed as
 * {@code uintptr(unsafe.Pointer(p))}. Expected values come from the JDK's
 * own file APIs or from data the test wrote, never from the executor.
 *
 * <pre>Main: SysMain types.tab workdir [bench]</pre>
 */
public final class SysMain {
    static final long SYS_READ = 0, SYS_WRITE = 1, SYS_CLOSE = 3, SYS_FSTAT = 5, SYS_READV = 19, SYS_WRITEV = 20, SYS_GETPID = 39,
            SYS_SENDMSG = 46, SYS_RECVMSG = 47, SYS_SOCKETPAIR = 53, SYS_GETDENTS64 = 217, SYS_CLOCK_GETTIME = 228,
            SYS_EPOLL_WAIT = 232, SYS_EPOLL_CTL = 233, SYS_OPENAT = 257, SYS_EPOLL_CREATE1 = 291, SYS_PIPE2 = 293;
    static final long AT_FDCWD = -100, O_RDONLY = 0, O_DIRECTORY = 0x10000, O_CLOEXEC = 0x80000, EPOLLIN = 1, EPOLL_CTL_ADD = 1,
            EBADF = 9, ENOENT = 2, S_IFMT = 0xf000, S_IFREG = 0x8000, AF_UNIX = 1, SOCK_STREAM = 1;

    private final TypeTab tab;
    private final HandleTable ht = new HandleTable();
    private final Syscalls sys;
    private int passed;
    private int failed;

    private SysMain(TypeTab tab) {
        this.tab = tab;
        this.sys = new Syscalls(tab, SyscallTable.linuxAmd64(tab), ht);
    }

    public static void main(String[] args) throws Exception {
        SysMain m = new SysMain(TypeTab.read(Path.of(args[0])));
        Path work = Path.of(args[1]);
        if (args.length > 2 && args[2].equals("bench")) {
            m.bench();
            return;
        }
        m.run(work);
        System.out.println("sysexec: " + m.passed + "/" + (m.passed + m.failed) + " checks passed");
        if (m.failed != 0) {
            System.exit(1);
        }
    }

    private void check(String name, boolean ok) {
        if (ok) {
            passed++;
        } else {
            failed++;
            System.out.println("sysexec: FAIL " + name);
        }
    }

    // ---- Go-side values --------------------------------------------------------------------------

    private static Blob bytes(String s) {
        byte[] b = s.getBytes(StandardCharsets.UTF_8);
        Blob blob = new Blob("[]byte", b.length);
        System.arraycopy(b, 0, blob.data, 0, b.length);
        return blob;
    }

    /** uintptr(unsafe.Pointer(&b[0])) */
    private long up(Blob b) {
        return ht.uintptr(new Addr(b, 0), b.data.length);
    }

    private Flat make(String type) {
        return new Flat(new Layout(tab, SyscallTable.type(tab, type)));
    }

    /** uintptr(unsafe.Pointer(&v)) */
    private long up(Flat v) {
        return ht.uintptr(new Addr(v, 0), v.layout.size);
    }

    private long off(String type, String field) {
        return SyscallTable.fieldOff(tab, type, field);
    }

    private Result call(long trap, long... a) {
        long[] w = new long[6];
        System.arraycopy(a, 0, w, 0, a.length);
        return sys.syscall6(trap, w[0], w[1], w[2], w[3], w[4], w[5]);
    }

    // ---- checks ----------------------------------------------------------------------------------

    private void run(Path work) throws Exception {
        Path file = work.resolve("sysexec-data.txt");
        String content = "graalgo syscall prototype\nline two\n";
        Files.writeString(file, content);

        // write(1, buf, len): the line run.sh looks for on stdout.
        Blob hello = bytes("sysexec: hello from write(2)\n");
        System.out.flush();
        Result r = call(SYS_WRITE, 1, up(hello), hello.data.length);
        check("write(1) returns the length", r.r1() == hello.data.length && r.errno() == 0);

        // openat + read + close
        Blob path = bytes(file + "\0");
        r = call(SYS_OPENAT, AT_FDCWD, up(path), O_RDONLY | O_CLOEXEC, 0);
        long fd = r.r1();
        check("openat returns a descriptor", fd > 2 && r.errno() == 0);
        Blob buf = new Blob("[]byte", 64);
        java.util.Arrays.fill(buf.data, (byte) '#');
        r = call(SYS_READ, fd, up(buf), buf.data.length);
        check("read returns the file size", r.r1() == content.length());
        check("read fills exactly n bytes", new String(buf.data, 0, content.length(), StandardCharsets.UTF_8).equals(content));
        check("read leaves the buffer tail untouched", buf.data[content.length()] == '#' && buf.data[63] == '#');
        // A pointer into the middle of the buffer: uintptr(unsafe.Pointer(&buf[40])) is base+40.
        r = call(SYS_READ, fd, up(buf) + 40, 8);
        check("read at EOF returns 0", r.r1() == 0 && r.errno() == 0);

        // fstat into a syscall.Stat_t held as a flat Go value (no byte image anywhere on the Go side).
        Flat st = make("syscall.Stat_t");
        r = call(SYS_FSTAT, fd, up(st));
        check("fstat succeeds", r.r1() == 0);
        check("Stat_t.Size", st.loadBits(off("syscall.Stat_t", "Size"), 8) == Files.size(file));
        check("Stat_t.Mode is a regular file", (st.loadBits(off("syscall.Stat_t", "Mode"), 4) & S_IFMT) == S_IFREG);
        check("Stat_t.Ino", st.loadBits(off("syscall.Stat_t", "Ino"), 8) == (Long) Files.getAttribute(file, "unix:ino"));
        check("Stat_t.Nlink", st.loadBits(off("syscall.Stat_t", "Nlink"), 8) == ((Number) Files.getAttribute(file, "unix:nlink")).longValue());
        long mtimeSec = st.loadBits(off("syscall.Stat_t", "Mtim"), 8); // Mtim.Sec is the first leaf of the nested Timespec
        check("Stat_t.Mtim.Sec", mtimeSec == Files.getLastModifiedTime(file).toMillis() / 1000);
        check("close", call(SYS_CLOSE, fd).r1() == 0);

        // errno paths
        r = call(SYS_CLOSE, fd);
        check("close of a closed fd: r1 = -1, EBADF", r.r1() == -1 && r.errno() == EBADF);
        Blob missing = bytes(work.resolve("no-such-file") + "\0");
        r = call(SYS_OPENAT, AT_FDCWD, up(missing), O_RDONLY, 0);
        check("openat of a missing file: ENOENT", r.r1() == -1 && r.errno() == ENOENT);
        Flat stKeep = make("syscall.Stat_t");
        stKeep.storeBits(off("syscall.Stat_t", "Size"), 8, 12345);
        r = call(SYS_FSTAT, -1, up(stKeep));
        check("failed fstat does not write the struct", r.errno() == EBADF && stKeep.loadBits(off("syscall.Stat_t", "Size"), 8) == 12345);

        // getdents64 on a directory
        Path dir = work.resolve("sysexec-dir");
        Files.createDirectories(dir);
        for (String n : new String[] {"alpha", "beta", "gamma.txt"}) {
            Files.writeString(dir.resolve(n), n);
        }
        Blob dpath = bytes(dir + "\0");
        long dfd = call(SYS_OPENAT, AT_FDCWD, up(dpath), O_RDONLY | O_DIRECTORY | O_CLOEXEC, 0).r1();
        Blob dents = new Blob("[]byte", 4096);
        r = call(SYS_GETDENTS64, dfd, up(dents), dents.data.length);
        Set<String> names = new TreeSet<>();
        for (int p = 0; p < r.r1();) { // linux_dirent64: ino u64, off i64, reclen u16, type u8, name
            int reclen = (int) dents.loadBits(p + 16, 2);
            int e = p + 19;
            while (dents.data[e] != 0) {
                e++;
            }
            names.add(new String(dents.data, p + 19, e - p - 19, StandardCharsets.UTF_8));
            p += reclen;
        }
        check("getdents64 lists the directory", names.equals(Set.of(".", "..", "alpha", "beta", "gamma.txt")));
        call(SYS_CLOSE, dfd);

        // pipe2 into a [2]int32, then write/read through it
        Flat fds = make("[2]int32");
        check("pipe2", call(SYS_PIPE2, up(fds), O_CLOEXEC).r1() == 0);
        long rfd = (int) fds.loadBits(0, 4), wfd = (int) fds.loadBits(4, 4);
        check("pipe2 fills both descriptors", rfd > 2 && wfd > 2 && rfd != wfd);

        // epoll: create, add the read end, wait (empty), write, wait (ready)
        long ep = call(SYS_EPOLL_CREATE1, O_CLOEXEC).r1();
        Flat ev = make("syscall.EpollEvent"); // 12 bytes: Events uint32, Fd int32, Pad int32
        ev.storeBits(off("syscall.EpollEvent", "Events"), 4, EPOLLIN);
        ev.storeBits(off("syscall.EpollEvent", "Fd"), 4, rfd);
        check("epoll_ctl ADD", call(SYS_EPOLL_CTL, ep, EPOLL_CTL_ADD, rfd, up(ev)).r1() == 0);
        Flat events = make("[4]syscall.EpollEvent");
        events.storeBits(12 + off("syscall.EpollEvent", "Fd"), 4, 77); // sentinel in events[1]
        check("epoll_wait with nothing ready returns 0", call(SYS_EPOLL_WAIT, ep, up(events), 4, 0).r1() == 0);
        Blob ping = bytes("ping");
        check("write to the pipe", call(SYS_WRITE, wfd, up(ping), 4).r1() == 4);
        r = call(SYS_EPOLL_WAIT, ep, up(events), 4, 1000);
        check("epoll_wait reports one event", r.r1() == 1);
        check("events[0].Fd is the read end", (int) events.loadBits(off("syscall.EpollEvent", "Fd"), 4) == rfd);
        check("events[0].Events has EPOLLIN", (events.loadBits(off("syscall.EpollEvent", "Events"), 4) & EPOLLIN) != 0);
        check("events[1] is not overwritten", events.loadBits(12 + off("syscall.EpollEvent", "Fd"), 4) == 77);
        Blob pong = new Blob("[]byte", 4);
        check("read from the pipe", call(SYS_READ, rfd, up(pong), 4).r1() == 4 && new String(pong.data, StandardCharsets.UTF_8).equals("ping"));

        // writev: [2]Iovec whose Base fields are Go pointers (nested, one level)
        Blob a = bytes("hello, "), b = bytes("world");
        Flat iov = make("[2]syscall.Iovec");
        long base = off("syscall.Iovec", "Base"), len = off("syscall.Iovec", "Len");
        iov.storeRef(base, new Addr(a, 0));
        iov.storeBits(len, 8, a.data.length);
        iov.storeRef(16 + base, new Addr(b, 0));
        iov.storeBits(16 + len, 8, b.data.length);
        check("writev returns the total", call(SYS_WRITEV, wfd, up(iov), 2).r1() == 12);
        // readv into two buffers of 5 and 20 bytes: the kernel fills 5 + 7
        Blob x = new Blob("[]byte", 5), y = new Blob("[]byte", 20);
        java.util.Arrays.fill(y.data, (byte) '.');
        Flat riov = make("[2]syscall.Iovec");
        riov.storeRef(base, new Addr(x, 0));
        riov.storeBits(len, 8, 5);
        riov.storeRef(16 + base, new Addr(y, 0));
        riov.storeBits(16 + len, 8, 20);
        check("readv returns the total", call(SYS_READV, rfd, up(riov), 2).r1() == 12);
        check("readv scatters into both Go buffers",
                new String(x.data, StandardCharsets.UTF_8).equals("hello") && new String(y.data, 0, 7, StandardCharsets.UTF_8).equals(", world"));
        check("readv leaves the unfilled tail unchanged", y.data[7] == '.' && y.data[19] == '.');

        // sendmsg/recvmsg over a socketpair: Msghdr -> []Iovec -> bytes (nested, two levels)
        Flat sv = make("[2]int32");
        check("socketpair", call(SYS_SOCKETPAIR, AF_UNIX, SOCK_STREAM, 0, up(sv)).r1() == 0);
        long s0 = (int) sv.loadBits(0, 4), s1 = (int) sv.loadBits(4, 4);
        Flat msg = make("syscall.Msghdr");
        msg.storeRef(off("syscall.Msghdr", "Iov"), new Addr(iov, 0));
        msg.storeBits(off("syscall.Msghdr", "Iovlen"), 8, 2);
        check("sendmsg sends both segments", call(SYS_SENDMSG, s0, up(msg), 0).r1() == 12);
        Blob in = new Blob("[]byte", 32);
        Flat iov1 = make("syscall.Iovec");
        iov1.storeRef(base, new Addr(in, 0));
        iov1.storeBits(len, 8, 32);
        Flat rmsg = make("syscall.Msghdr");
        rmsg.storeRef(off("syscall.Msghdr", "Iov"), new Addr(iov1, 0));
        rmsg.storeBits(off("syscall.Msghdr", "Iovlen"), 8, 1);
        rmsg.storeBits(off("syscall.Msghdr", "Flags"), 4, 0x7fff);
        check("recvmsg receives 12 bytes", call(SYS_RECVMSG, s1, up(rmsg), 0).r1() == 12);
        check("recvmsg fills the Go buffer two pointers away", new String(in.data, 0, 12, StandardCharsets.UTF_8).equals("hello, world"));
        check("recvmsg writes Msghdr.Flags back", rmsg.loadBits(off("syscall.Msghdr", "Flags"), 4) == 0);
        check("recvmsg leaves the Go pointer field alone", ((Addr) rmsg.loadRef(off("syscall.Msghdr", "Iov"))).base() == iov1);

        // clock_gettime into a Timespec
        Flat ts = make("syscall.Timespec");
        long before = System.currentTimeMillis() / 1000;
        check("clock_gettime", call(SYS_CLOCK_GETTIME, 0, up(ts)).r1() == 0);
        long sec = ts.loadBits(off("syscall.Timespec", "Sec"), 8);
        check("Timespec.Sec is now", sec >= before && sec <= before + 2);
        check("Timespec.Nsec in range", ts.loadBits(off("syscall.Timespec", "Nsec"), 8) < 1_000_000_000L);
        check("getpid", call(SYS_GETPID).r1() == ProcessHandle.current().pid());

        // Loud failures (never a silent wrong call)
        check("unknown syscall number is rejected", throwsUnsupported(() -> call(9999)));
        check("syscall outside the prototype table is rejected", throwsUnsupported(() -> call(9 /* mmap */, 0, 4096, 3, 0x22, -1, 0)));
        check("integer that is not a pointer traps", traps(() -> call(SYS_WRITE, 1, 0x1234, 4)));
        check("length beyond the Go buffer traps", traps(() -> call(SYS_WRITE, wfd, up(ping), 4096)));
        check("offset pointer beyond the Go buffer traps", traps(() -> call(SYS_READ, rfd, up(pong) + 3, 2)));
        check("nil where the kernel requires a struct traps", traps(() -> call(SYS_FSTAT, rfd, 0)));
        check("blocking calls were announced to the scheduler hook", sys.blockingCalls >= 10);
        for (long d : new long[] {rfd, wfd, ep, s0, s1}) {
            call(SYS_CLOSE, d);
        }
    }

    private static boolean throwsUnsupported(Runnable r) {
        try {
            r.run();
            return false;
        } catch (UnsupportedOperationException e) {
            return true;
        }
    }

    private static boolean traps(Runnable r) {
        try {
            r.run();
            return false;
        } catch (Mem.UnsafeTrap e) {
            return true;
        }
    }

    /** Indicative ns/op, best of 5: marshalling + Panama downcall per call. */
    private void bench() {
        Blob devnull = bytes("/dev/null\0");
        long fd = call(SYS_OPENAT, AT_FDCWD, up(devnull), 1 /* O_WRONLY */, 0).r1();
        Blob buf = new Blob("[]byte", 64);
        Flat st = make("syscall.Stat_t");
        long ubuf = up(buf), ust = up(st);
        int n = 1_000_000;
        long bw = Long.MAX_VALUE, bf = Long.MAX_VALUE, bp = Long.MAX_VALUE, sink = 0;
        for (int round = 0; round < 5; round++) {
            long t0 = System.nanoTime();
            for (int i = 0; i < n; i++) {
                sink += sys.syscall6(SYS_WRITE, fd, ubuf, 64, 0, 0, 0).r1();
            }
            long t1 = System.nanoTime();
            for (int i = 0; i < n; i++) {
                sink += sys.syscall6(SYS_FSTAT, fd, ust, 0, 0, 0, 0).r1();
            }
            long t2 = System.nanoTime();
            for (int i = 0; i < n; i++) {
                sink += sys.syscall6(SYS_GETPID, 0, 0, 0, 0, 0, 0).r1();
            }
            long t3 = System.nanoTime();
            bw = Math.min(bw, t1 - t0);
            bf = Math.min(bf, t2 - t1);
            bp = Math.min(bp, t3 - t2);
        }
        System.out.printf("bench java write(/dev/null, 64B) %.0f ns/op%n", (double) bw / n);
        System.out.printf("bench java fstat                 %.0f ns/op%n", (double) bf / n);
        System.out.printf("bench java getpid                %.0f ns/op%n", (double) bp / n);
        if (sink == 42) {
            System.out.println();
        }
        call(SYS_CLOSE, fd);
    }
}
