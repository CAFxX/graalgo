package graalgo.p3net;

import java.io.IOException;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.NetworkInterface;
import java.net.ProtocolFamily;
import java.net.SocketAddress;
import java.net.SocketException;
import java.net.StandardProtocolFamily;
import java.net.StandardSocketOptions;
import java.nio.ByteBuffer;
import java.nio.channels.ClosedChannelException;
import java.nio.channels.DatagramChannel;
import java.nio.channels.SelectableChannel;
import java.nio.channels.ServerSocketChannel;
import java.nio.channels.SocketChannel;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;

import graalgo.p3os.GoOs;
import graalgo.values.GoSlice;

/**
 * Host-side implementation of the {@code graalgo.net.*} linkname
 * intrinsics (P3-M9, RISK-M9-2): the socket syscall surface.
 *
 * <p>Calling conventions (mirroring {@code graalgo.os.*}, os.md §2.3):
 * <ul>
 *   <li>Every {@code long}-returning intrinsic returns negative =
 *       {@code -errno} (Linux errno number), non-negative = value.
 *   <li>{@code sendto}/{@code recvfrom} return {@code n+1} for
 *       {@code n >= 0} bytes transferred, negative = {@code -errno}.
 *       A zero-length datagram returns 1, never confused with an error.
 *       Stream read/write go through {@code graalgo.os.read/write} on
 *       the same cookie (fd-domain unification): a 0-byte read on a
 *       nonblocking socket surfaces as return value 1, which the fork
 *       maps to EAGAIN when {@code len(buf) > 0} (GoOs returns 0 only
 *       on EOF, -errno on error — the three cases stay distinct).
 *   <li>{@code getsockname}/{@code getpeername} write the wire-format
 *       address into the supplied buffer and return {@code n+1} bytes
 *       written.
 *   <li>Arguments arrive D5-boxed ({@code Long} for integers,
 *       {@code GoSlice} for {@code []byte}); each target adapts
 *       explicitly via {@link #num} ({@code Number} accepted for every
 *       integer shape).
 * </ul>
 *
 * <p>Socket address wire format (a {@code []byte}): byte 0 is the family,
 * then big-endian fields —
 * <ul>
 *   <li>AF_INET (2): {@code [2, portHi, portLo, a0, a1, a2, a3]} (7 bytes)
 *   <li>AF_INET6 (10): {@code [10, portHi, portLo, a0..a15, scopeHi..scopeLo]}
 *       (23 bytes; scope id is a big-endian u32)
 *   <li>AF_UNIX (1): not supported in M9 — {@code socket()} returns
 *       {@code -EAFNOSUPPORT} (documented scope limit; loopback TCP/UDP
 *       is the M9 target).
 * </ul>
 * {@code recvfrom} writes the peer address in this format into its third
 * argument (which must have room for 23 bytes); byte 0 = 0 means "no
 * address" (should not happen for datagrams; defensive).
 *
 * <p>Handle model: sockets are cookies in {@link GoOs}'s unified handle
 * table ({@code KIND_SOCKET}), allocated from the same
 * {@code NEXT_COOKIE} space as files — {@code os.NewFile} cookies,
 * {@code net} FDs and {@code os.Pipe} fds share one integer namespace,
 * gc parity. Per-socket control state (bind/connect progress, recorded
 * socket options, the pending connect error) lives in {@link SockState}
 * here, keyed by cookie.
 *
 * <p>Channel lifecycle (eager): {@code socket()} creates the NIO channel
 * immediately (SOCK_STREAM → {@link SocketChannel}, SOCK_DGRAM →
 * {@link DatagramChannel}, always nonblocking). {@code bind()} records
 * the address and binds datagram channels at once; stream channels bind
 * lazily at {@code connect()} time so that {@code listen()} can swap in
 * a {@link ServerSocketChannel} (a bound {@code SocketChannel} cannot
 * listen). Recorded socket options are re-applied across the swap.
 * {@code connect()} is nonblocking: 0 = connected, {@code -EINPROGRESS}
 * = completion pending (the netpoller drives {@code OP_CONNECT};
 * failure surfaces through {@code getsockopt(SO_ERROR)}, gc parity).
 *
 * <p>State is per-program-run like {@link GoOs}: {@link #resetForTest}
 * clears it (tests); the launcher path relies on one program per JVM.
 */
public final class GoNet {
    private GoNet() {}

    // Linux errno numbers (amd64 ABI) beyond GoOs's file set.
    public static final int EAGAIN = 11;
    public static final int ENOMEM = 12;
    public static final int EADDRINUSE = 98;
    public static final int EADDRNOTAVAIL = 99;
    public static final int ENETUNREACH = 101;
    public static final int ECONNRESET = 104;
    public static final int ENOTCONN = 107;
    public static final int ETIMEDOUT = 110;
    public static final int ECONNREFUSED = 111;
    public static final int EHOSTUNREACH = 113;
    public static final int EALREADY = 114;
    public static final int EINPROGRESS = 115;
    public static final int EMSGSIZE = 90;
    public static final int EDESTADDRREQ = 89;
    public static final int EISCONN = 106;
    public static final int ENOTSOCK = 88;
    public static final int EAFNOSUPPORT = 97;
    public static final int ENOPROTOOPT = 92;
    public static final int EOPNOTSUPP = 95;

    // Address families.
    public static final int AF_UNIX = 1;
    public static final int AF_INET = 2;
    public static final int AF_INET6 = 10;
    // Socket types (low 4 bits; Go ORs SOCK_CLOEXEC|SOCK_NONBLOCK).
    public static final int SOCK_STREAM = 1;
    public static final int SOCK_DGRAM = 2;
    // Socket levels / option names (Linux).
    public static final int SOL_SOCKET = 1;
    public static final int SO_REUSEADDR = 2;
    public static final int SO_TYPE = 3;
    public static final int SO_ERROR = 4;
    public static final int SO_SNDBUF = 7;
    public static final int SO_RCVBUF = 8;
    public static final int SO_KEEPALIVE = 9;
    public static final int IPPROTO_TCP = 6;
    public static final int TCP_NODELAY = 1;
    public static final int IPPROTO_IPV6 = 41;
    public static final int IPV6_V6ONLY = 26;
    // shutdown(2) how.
    public static final int SHUT_RD = 0;
    public static final int SHUT_WR = 1;
    public static final int SHUT_RDWR = 2;

    /** Socket lifecycle states. */
    enum SockStateKind {
        /** socket() done, nothing else. */
        IDLE,
        /** bind() recorded (stream: channel not yet bound). */
        BOUND,
        /** listen() done (ServerSocketChannel). */
        LISTENING,
        /** connect() in progress (nonblocking). */
        CONNECTING,
        /** connect() failed; error in connectErrno. */
        CONNECT_ERROR,
        /** connected (stream) or ready (dgram). */
        CONNECTED,
    }

    /** Per-socket control state, keyed by cookie. */
    static final class SockState {
        final int domain;
        final int sockType;
        final int proto;
        volatile SockStateKind kind = SockStateKind.IDLE;
        /** Address recorded by bind(); null until bound. */
        volatile InetSocketAddress boundAddr;
        /** Stashed async connect errno (SO_ERROR); 0 = none. */
        volatile int connectErrno;
        /** Recorded (level, opt, value) setsockopt calls, re-applied on channel swap. */
        final List<long[]> sockOpts = new ArrayList<>();
        /** setnonblock flag (channels are always nonblocking underneath). */
        volatile boolean nonblocking = true;

        SockState(int domain, int sockType, int proto) {
            this.domain = domain;
            this.sockType = sockType;
            this.proto = proto;
        }
    }

    private static final ConcurrentHashMap<Long, SockState> STATES = new ConcurrentHashMap<>();

    /** Clears per-run socket state (tests). The GoOs handle table is reset separately. */
    public static void resetForTest() {
        STATES.clear();
    }

    // ------------------------------------------------------------------
    // D5 adaptation + buffer helpers (mirroring GoOs's private ones;
    // cross-package, so duplicated here rather than exposed).
    // ------------------------------------------------------------------

    private static long num(Object o) {
        return ((Number) o).longValue();
    }

    private static int num32(Object o) {
        return ((Number) o).intValue();
    }

    /** A Go []byte as a ByteBuffer honoring the GoSlice offset/len. */
    private static ByteBuffer goBuf(Object o) {
        if (o == null) {
            return ByteBuffer.allocate(0);
        }
        GoSlice gs = (GoSlice) o;
        // []byte is byte[]-backed (D5).
        byte[] arr = (byte[]) gs.array();
        int off = gs.offset();
        int len = gs.len();
        byte[] bytes = new byte[len];
        System.arraycopy(arr, off, bytes, 0, len);
        return ByteBuffer.wrap(bytes);
    }

    /** Copies bytes read into a {@link #goBuf} ByteBuffer back into the Go []byte. */
    private static void goBufBack(Object o, ByteBuffer bb, int n) {
        if (o == null || n <= 0) {
            return;
        }
        GoSlice gs = (GoSlice) o;
        byte[] arr = (byte[]) gs.array();
        byte[] bytes = bb.array();
        System.arraycopy(bytes, 0, arr, gs.offset(), n);
    }

    /** Writes raw bytes into a Go []byte at its offset; returns bytes written or -1 if too small. */
    private static int goBufPut(Object o, byte[] src) {
        if (o == null) {
            return -1;
        }
        GoSlice gs = (GoSlice) o;
        if (gs.len() < src.length) {
            return -1;
        }
        byte[] arr = (byte[]) gs.array();
        System.arraycopy(src, 0, arr, gs.offset(), src.length);
        return src.length;
    }

    /**
     * Socket-aware errno mapper: socket-specific exception types first,
     * then {@link GoOs#errno} for the rest. The only exception→errno
     * translation on the socket path (mirrors GoOs R1 discipline).
     *
     * <p>I-9 (2026-10-19): TCP RST ("Connection reset") maps to
     * {@link #ECONNRESET} via the {@code GoOs.errno} fallthrough below
     * (the branch lives there because stream I/O goes through
     * {@code graalgo.os.read/write}); "Broken pipe" likewise falls
     * through to {@code EPIPE}. The subclass branches above keep
     * priority, so e.g. a refused connect still reports
     * {@code ECONNREFUSED}, never {@code ECONNRESET}.
     */
    public static int sockErrno(Throwable t) {
        if (t instanceof java.net.SocketException se) {
            // "Operation not permitted" (sandbox EPERM on unconnected
            // sendto, verified 2026-10-09) must not collapse to EIO.
            String m = se.getMessage();
            if (m != null && m.toLowerCase(java.util.Locale.ROOT).contains("operation not permitted")) {
                return GoOs.EPERM;
            }
        }
        if (t instanceof java.net.BindException) {
            return EADDRINUSE;
        }        if (t instanceof java.net.NoRouteToHostException) {
            return EHOSTUNREACH;
        }
        if (t instanceof java.net.PortUnreachableException) {
            return ECONNREFUSED;
        }
        if (t instanceof java.net.ConnectException) {
            return ECONNREFUSED;
        }
        if (t instanceof java.nio.channels.UnresolvedAddressException) {
            return GoOs.EINVAL;
        }
        if (t instanceof java.nio.channels.UnsupportedAddressTypeException) {
            return EAFNOSUPPORT;
        }
        if (t instanceof java.nio.channels.AlreadyConnectedException) {
            return EISCONN;
        }
        if (t instanceof java.nio.channels.ConnectionPendingException) {
            return EALREADY;
        }
        if (t instanceof java.nio.channels.NotYetConnectedException) {
            return ENOTCONN;
        }
        return GoOs.errno(t);
    }

    // ------------------------------------------------------------------
    // Package-visible internals for GoNetpoll.
    // ------------------------------------------------------------------

    /** The NIO selectable channel for a socket cookie, or null. */
    static SelectableChannel selectableChannel(long cookie) {
        java.nio.channels.Channel ch = GoOs.socketChannel(cookie);
        return ch instanceof SelectableChannel sc ? sc : null;
    }

    /** Per-socket control state, or null for a non-socket cookie. */
    static SockState sockState(long cookie) {
        return STATES.get(cookie);
    }

    /**
     * Called by the netpoller when a nonblocking connect completes.
     * errno 0 = success; otherwise the async failure is stashed for
     * getsockopt(SO_ERROR) (gc parity) and the state goes CONNECT_ERROR.
     */
    static void onConnectComplete(long cookie, int errno) {
        SockState st = STATES.get(cookie);
        if (st == null) {
            return;
        }
        if (errno == 0) {
            st.kind = SockStateKind.CONNECTED;
        } else {
            st.connectErrno = errno;
            st.kind = SockStateKind.CONNECT_ERROR;
        }
    }

    // ------------------------------------------------------------------
    // Address wire format.
    // ------------------------------------------------------------------

    /** Parses a wire-format address into an InetSocketAddress. */
    private static InetSocketAddress parseAddr(Object addrArg) throws IllegalArgumentException {
        return parseAddrBuf(goBuf(addrArg));
    }

    /** Parses a wire-format address from an already-copied buffer (P2-6: no double copy). */
    private static InetSocketAddress parseAddrBuf(ByteBuffer bb) throws IllegalArgumentException {
        if (!bb.hasRemaining()) {
            throw new IllegalArgumentException("empty sockaddr");
        }
        int fam = bb.get() & 0xFF;
        if (fam == AF_INET) {
            if (bb.remaining() < 6) {
                throw new IllegalArgumentException("short AF_INET sockaddr");
            }
            int port = bb.getShort() & 0xFFFF;
            byte[] a = new byte[4];
            bb.get(a);
            try {
                return new InetSocketAddress(InetAddress.getByAddress(a), port);
            } catch (java.net.UnknownHostException e) {
                throw new IllegalArgumentException(e);
            }
        }
        if (fam == AF_INET6) {
            if (bb.remaining() < 22) {
                throw new IllegalArgumentException("short AF_INET6 sockaddr");
            }
            int port = bb.getShort() & 0xFFFF;
            byte[] a = new byte[16];
            bb.get(a);
            int scope = bb.getInt();
            try {
                // No public InetSocketAddress(InetAddress, port, scope);
                // build via Inet6Address (scope 0 = default).
                java.net.Inet6Address ia6 = scope == 0
                        ? java.net.Inet6Address.getByAddress(null, a, null)
                        : java.net.Inet6Address.getByAddress(null, a, scope);
                return new InetSocketAddress(ia6, port);
            } catch (java.net.UnknownHostException e) {
                throw new IllegalArgumentException(e);
            }
        }
        throw new IllegalArgumentException("unsupported sockaddr family " + fam);
    }

    /** Serializes an InetSocketAddress into the wire format. */
    private static byte[] renderAddr(InetSocketAddress sa) {
        InetAddress ia = sa.getAddress();
        byte[] raw = ia.getAddress();
        if (raw.length == 4) {
            byte[] out = new byte[7];
            out[0] = (byte) AF_INET;
            out[1] = (byte) (sa.getPort() >>> 8);
            out[2] = (byte) sa.getPort();
            System.arraycopy(raw, 0, out, 3, 4);
            return out;
        }
        byte[] out = new byte[23];
        out[0] = (byte) AF_INET6;
        out[1] = (byte) (sa.getPort() >>> 8);
        out[2] = (byte) sa.getPort();
        System.arraycopy(raw, 0, out, 3, 16);
        // Scope id: not recoverable from InetSocketAddress portably; zero.
        return out;
    }

    /** The zero address for a family (getsockname on an unbound socket, gc parity). */
    private static byte[] zeroAddr(int domain) {
        if (domain == AF_INET6) {
            byte[] out = new byte[23];
            out[0] = (byte) AF_INET6;
            return out;
        }
        byte[] out = new byte[7];
        out[0] = (byte) AF_INET;
        return out;
    }

    private static ProtocolFamily protoFamily(int domain) {
        return domain == AF_INET6 ? StandardProtocolFamily.INET6 : StandardProtocolFamily.INET;
    }

    // ------------------------------------------------------------------
    // Traps.
    // ------------------------------------------------------------------

    /** graalgo.net.socket — cookie or -errno. */
    public static long socket(Object domainArg, Object typeArg, Object protoArg) {
        int domain = num32(domainArg);
        int type = num32(typeArg) & 0xF; // mask off SOCK_CLOEXEC|SOCK_NONBLOCK
        int proto = num32(protoArg);
        if (domain != AF_INET && domain != AF_INET6) {
            return -EAFNOSUPPORT; // AF_UNIX deferred (documented scope limit)
        }
        try {
            final java.nio.channels.Channel ch;
            final boolean readable;
            final boolean writable;
            if (type == SOCK_STREAM) {
                SocketChannel sc = SocketChannel.open(protoFamily(domain));
                sc.configureBlocking(false);
                ch = sc;
                readable = true;
                writable = true;
            } else if (type == SOCK_DGRAM) {
                DatagramChannel dc = DatagramChannel.open(protoFamily(domain));
                dc.configureBlocking(false);
                ch = dc;
                readable = true;
                writable = true;
            } else {
                return -EAFNOSUPPORT; // SOCK_RAW etc. out of scope
            }
            long cookie = GoOs.registerSocketChannel(ch, readable, writable);
            STATES.put(cookie, new SockState(domain, type, proto));
            return cookie;
        } catch (IOException e) {
            return -sockErrno(e);
        } catch (UnsupportedOperationException e) {
            return -EAFNOSUPPORT;
        }
    }

    /** graalgo.net.bind — 0 or -errno. Records the address; binds datagram channels eagerly. */
    public static long bind(Object cookieArg, Object addrArg) {
        long cookie = num(cookieArg);
        SockState st = STATES.get(cookie);
        java.nio.channels.Channel ch = GoOs.socketChannel(cookie);
        if (st == null || ch == null) {
            return -GoOs.EBADF;
        }
        final InetSocketAddress sa;
        try {
            sa = parseAddr(addrArg);
        } catch (IllegalArgumentException e) {
            return -GoOs.EINVAL;
        }
        synchronized (st) {
            if (st.kind != SockStateKind.IDLE) {
                return -GoOs.EINVAL; // already bound (gc: EINVAL)
            }
            try {
                applySockOpts(ch, st);
                if (ch instanceof DatagramChannel dc) {
                    dc.bind(sa);
                }
                // SOCK_STREAM: channel binds lazily at connect()/listen()
                // so listen() can swap in a ServerSocketChannel.
                st.boundAddr = sa;
                st.kind = SockStateKind.BOUND;
                return 0;
            } catch (IOException e) {
                return -sockErrno(e);
            }
        }
    }

    /** graalgo.net.listen — 0 or -errno. Swaps the stream channel for a ServerSocketChannel. */
    public static long listen(Object cookieArg, Object backlogArg) {
        long cookie = num(cookieArg);
        int backlog = num32(backlogArg);
        SockState st = STATES.get(cookie);
        java.nio.channels.Channel ch = GoOs.socketChannel(cookie);
        if (st == null || ch == null) {
            return -GoOs.EBADF;
        }
        if (!(ch instanceof SocketChannel)) {
            return -EOPNOTSUPP; // listen on datagram/already-listening
        }
        synchronized (st) {
            if (st.kind != SockStateKind.IDLE && st.kind != SockStateKind.BOUND) {
                return -GoOs.EINVAL;
            }
            try {
                ServerSocketChannel ssc = ServerSocketChannel.open(protoFamily(st.domain));
                ssc.configureBlocking(false);
                applySockOpts(ssc, st);
                ssc.bind(st.boundAddr, backlog <= 0 ? 128 : backlog);
                // Swap the handle's channel (closes the idle SocketChannel).
                GoOs.replaceSocketChannel(cookie, ssc, false, false);
                // The fork ran runtime_pollOpen BEFORE listen (FD.Init
                // order): migrate the pollDesc's selector registration to
                // the new ServerSocketChannel, or the accept wait parks
                // forever on the dead key (P0-1).
                long mig = GoNetpoll.migrateSocketChannel(cookie);
                if (mig != 0) {
                    return mig;
                }
                st.kind = SockStateKind.LISTENING;
                return 0;
            } catch (IOException e) {
                return -sockErrno(e);
            } catch (UnsupportedOperationException e) {
                return -EAFNOSUPPORT;
            }
        }
    }

    /** graalgo.net.accept — new cookie or -errno (incl. -EAGAIN). */
    public static long accept(Object cookieArg) {
        long cookie = num(cookieArg);
        SockState st = STATES.get(cookie);
        java.nio.channels.Channel ch = GoOs.socketChannel(cookie);
        if (st == null || ch == null) {
            return -GoOs.EBADF;
        }
        if (!(ch instanceof ServerSocketChannel ssc)) {
            return -GoOs.EINVAL;
        }
        try {
            SocketChannel sc = ssc.accept();
            if (sc == null) {
                return -EAGAIN; // nonblocking, none pending (gc parity)
            }
            sc.configureBlocking(false);
            long nc = GoOs.registerSocketChannel(sc, true, true);
            SockState nst = new SockState(st.domain, SOCK_STREAM, st.proto);
            nst.kind = SockStateKind.CONNECTED;
            STATES.put(nc, nst);
            return nc;
        } catch (IOException e) {
            return -sockErrno(e);
        }
    }

    /** graalgo.net.connect — 0, -EINPROGRESS, or -errno. */
    public static long connect(Object cookieArg, Object addrArg) {
        long cookie = num(cookieArg);
        SockState st = STATES.get(cookie);
        java.nio.channels.Channel ch = GoOs.socketChannel(cookie);
        if (st == null || ch == null) {
            return -GoOs.EBADF;
        }
        if (!(ch instanceof SocketChannel) && !(ch instanceof DatagramChannel)) {
            return -EOPNOTSUPP;
        }
        final InetSocketAddress sa;
        try {
            sa = parseAddr(addrArg);
        } catch (IllegalArgumentException e) {
            return -GoOs.EINVAL;
        }
        // Connected datagrams: connect() is instant (no EINPROGRESS).
        if (ch instanceof DatagramChannel dc) {
            synchronized (st) {
                if (st.kind == SockStateKind.CONNECTED) {
                    return -EISCONN;
                }
                try {
                    dc.connect(sa);
                    st.kind = SockStateKind.CONNECTED;
                    return 0;
                } catch (IOException e) {
                    return -sockErrno(e);
                }
            }
        }
        synchronized (st) {
            if (st.kind == SockStateKind.CONNECTED) {
                return -EISCONN;
            }
            if (st.kind == SockStateKind.CONNECTING) {
                return -EALREADY;
            }
            try {
                SocketChannel conn = (SocketChannel) ch;
                if (st.kind == SockStateKind.CONNECT_ERROR) {
                    // A failed nonblocking connect leaves the channel
                    // unusable; retry on a fresh one (the fork closes on
                    // connect error anyway, so this is a safety net).
                    // Same staleness class as listen() (P0-1): the fork's
                    // early pollOpen registered the old channel.
                    conn = SocketChannel.open(protoFamily(st.domain));
                    conn.configureBlocking(false);
                    GoOs.replaceSocketChannel(cookie, conn, true, true);
                    long mig = GoNetpoll.migrateSocketChannel(cookie);
                    if (mig != 0) {
                        return mig;
                    }
                }
                applySockOpts(conn, st);
                if (st.boundAddr != null
                        && (st.kind == SockStateKind.BOUND
                            || st.kind == SockStateKind.CONNECT_ERROR)) {
                    conn.bind(st.boundAddr); // explicit bind before connect (gc parity)
                }
                boolean done = conn.connect(sa);
                if (done) {
                    st.kind = SockStateKind.CONNECTED;
                    return 0;
                }
                st.kind = SockStateKind.CONNECTING;
                return -EINPROGRESS;
            } catch (IOException e) {
                return -sockErrno(e);
            } catch (UnsupportedOperationException e) {
                return -EAFNOSUPPORT;
            }
        }
    }

    /** Applies recorded socket options to a (possibly fresh) channel. */
    private static void applySockOpts(java.nio.channels.Channel ch, SockState st) throws IOException {
        for (long[] so : st.sockOpts) {
            setOneOpt(ch, (int) so[0], (int) so[1], so[2]);
        }
    }

    @SuppressWarnings({"unchecked", "rawtypes"})
    private static void setOneOpt(java.nio.channels.Channel ch, int level, int opt, long value)
            throws IOException {
        if (!(ch instanceof java.nio.channels.NetworkChannel nc)) {
            throw new IOException("not a network channel");
        }
        boolean b = value != 0;
        try {
            if (level == SOL_SOCKET) {
                switch (opt) {
                    case SO_REUSEADDR -> nc.setOption(StandardSocketOptions.SO_REUSEADDR, b);
                    case SO_KEEPALIVE -> nc.setOption(StandardSocketOptions.SO_KEEPALIVE, b);
                    case SO_SNDBUF -> nc.setOption(StandardSocketOptions.SO_SNDBUF, (int) value);
                    case SO_RCVBUF -> nc.setOption(StandardSocketOptions.SO_RCVBUF, (int) value);
                    default -> throw new IOException("ENOPROTOOPT");
                }
                return;
            }
            if (level == IPPROTO_TCP && opt == TCP_NODELAY) {
                nc.setOption(StandardSocketOptions.TCP_NODELAY, b);
                return;
            }
            if (level == IPPROTO_IPV6 && opt == IPV6_V6ONLY) {
                // No JDK API for IPV6_V6ONLY. Our INET6 channels are
                // dual-stack (v6only=0) by construction, so v=0 is a
                // no-op success and v=1 is a loud -ENOPROTOOPT rather
                // than a silent behavioral divergence.
                if (!b) {
                    return;
                }
                throw new IOException("ENOPROTOOPT");
            }
            throw new IOException("ENOPROTOOPT");
        } catch (UnsupportedOperationException e) {
            throw new IOException("ENOPROTOOPT", e);
        } catch (IllegalArgumentException e) {
            throw new IOException("ENOPROTOOPT", e);
        }
    }

    /** Maps the ENOPROTOOPT sentinel IOException to the errno. */
    private static int optErrno(IOException e) {
        return "ENOPROTOOPT".equals(e.getMessage()) ? ENOPROTOOPT : sockErrno(e);
    }

    /** graalgo.net.setsockopt — 0 or -errno. */
    public static long setsockopt(Object cookieArg, Object levelArg, Object optArg, Object valArg) {
        long cookie = num(cookieArg);
        int level = num32(levelArg);
        int opt = num32(optArg);
        long value = num(valArg);
        SockState st = STATES.get(cookie);
        java.nio.channels.Channel ch = GoOs.socketChannel(cookie);
        if (st == null || ch == null) {
            return -GoOs.EBADF;
        }
        synchronized (st) {
            try {
                setOneOpt(ch, level, opt, value);
            } catch (IOException e) {
                return -optErrno(e);
            }
            // Record for re-application across the listen() channel swap.
            st.sockOpts.removeIf(so -> so[0] == level && so[1] == opt);
            st.sockOpts.add(new long[]{level, opt, value});
            return 0;
        }
    }

    /** graalgo.net.getsockopt — option value or -errno. */
    @SuppressWarnings({"unchecked", "rawtypes"})
    public static long getsockopt(Object cookieArg, Object levelArg, Object optArg) {
        long cookie = num(cookieArg);
        int level = num32(levelArg);
        int opt = num32(optArg);
        SockState st = STATES.get(cookie);
        java.nio.channels.Channel ch = GoOs.socketChannel(cookie);
        if (st == null || ch == null) {
            return -GoOs.EBADF;
        }
        // SO_ERROR: the stashed async connect error (gc parity: queried after waitWrite).
        if (level == SOL_SOCKET && opt == SO_ERROR) {
            int e;
            synchronized (st) {
                e = st.connectErrno;
                st.connectErrno = 0;
            }
            return e;
        }
        if (level == SOL_SOCKET && opt == SO_TYPE) {
            return st.sockType;
        }
        if (!(ch instanceof java.nio.channels.NetworkChannel nc)) {
            return -ENOPROTOOPT;
        }
        try {
            if (level == SOL_SOCKET) {
                return switch (opt) {
                    case SO_REUSEADDR -> nc.getOption(StandardSocketOptions.SO_REUSEADDR) ? 1 : 0;
                    case SO_KEEPALIVE -> nc.getOption(StandardSocketOptions.SO_KEEPALIVE) ? 1 : 0;
                    case SO_SNDBUF -> nc.getOption(StandardSocketOptions.SO_SNDBUF);
                    case SO_RCVBUF -> nc.getOption(StandardSocketOptions.SO_RCVBUF);
                    default -> -ENOPROTOOPT;
                };
            }
            if (level == IPPROTO_TCP && opt == TCP_NODELAY) {
                return nc.getOption(StandardSocketOptions.TCP_NODELAY) ? 1 : 0;
            }
            if (level == IPPROTO_IPV6 && opt == IPV6_V6ONLY) {
                return 0; // dual-stack by construction (see setOneOpt)
            }
            return -ENOPROTOOPT;
        } catch (IOException e) {
            return -optErrno(e);
        } catch (UnsupportedOperationException | IllegalArgumentException e) {
            return -ENOPROTOOPT;
        }
    }

    /** graalgo.net.getsockname — n+1 bytes written or -errno. */
    public static long getsockname(Object cookieArg, Object bufArg) {
        long cookie = num(cookieArg);
        SockState st = STATES.get(cookie);
        java.nio.channels.Channel ch = GoOs.socketChannel(cookie);
        if (st == null || ch == null) {
            return -GoOs.EBADF;
        }
        try {
            SocketAddress la = null;
            if (ch instanceof SocketChannel sc) {
                la = sc.getLocalAddress();
            } else if (ch instanceof ServerSocketChannel ssc) {
                la = ssc.getLocalAddress();
            } else if (ch instanceof DatagramChannel dc) {
                la = dc.getLocalAddress();
            }
            byte[] wire;
            if (la instanceof InetSocketAddress isa) {
                wire = renderAddr(isa);
            } else if (st.boundAddr != null) {
                wire = renderAddr(st.boundAddr);
            } else {
                wire = zeroAddr(st.domain);
            }
            int n = goBufPut(bufArg, wire);
            return n < 0 ? -GoOs.EINVAL : (long) n + 1;
        } catch (IOException e) {
            return -sockErrno(e);
        }
    }

    /** graalgo.net.getpeername — n+1 bytes written or -errno. */
    public static long getpeername(Object cookieArg, Object bufArg) {
        long cookie = num(cookieArg);
        SockState st = STATES.get(cookie);
        java.nio.channels.Channel ch = GoOs.socketChannel(cookie);
        if (st == null || ch == null) {
            return -GoOs.EBADF;
        }
        try {
            SocketAddress ra = null;
            if (ch instanceof SocketChannel sc) {
                ra = sc.getRemoteAddress();
            } else if (ch instanceof DatagramChannel dc) {
                ra = dc.getRemoteAddress();
            }
            if (!(ra instanceof InetSocketAddress isa)) {
                return -ENOTCONN;
            }
            int n = goBufPut(bufArg, renderAddr(isa));
            return n < 0 ? -GoOs.EINVAL : (long) n + 1;
        } catch (IOException e) {
            return -sockErrno(e);
        }
    }

    /**
     * graalgo.net.interfaces — writes the serialized interface list into
     * buf and returns n+1, or -errno. Returns -ENOMEM when the blob does
     * not fit (the Go side grows the buffer and retries). Blob format
     * (all int32 little-endian): count, then per interface index, mtu,
     * nameLen + name bytes, hwLen + hw bytes, flags (bitmask: 1=up,
     * 4=loopback, 8=pointopoint, 16=multicast; broadcast has no Java API
     * and is never set).
     *
     * <p>P3-M9 (module-probe lane): backs net.Interface/Interfaces
     * (github.com/google/uuid v1 node IDs).
     */
    public static long interfaces(Object bufArg) {
        final java.util.List<NetworkInterface> ifs;
        try {
            java.util.Enumeration<NetworkInterface> en =
                    NetworkInterface.getNetworkInterfaces();
            ifs = (en == null) ? java.util.List.of()
                    : java.util.Collections.list(en);
        } catch (SocketException e) {
            return -sockErrno(e);
        }
        java.io.ByteArrayOutputStream baos =
                new java.io.ByteArrayOutputStream();
        putI32(baos, ifs.size());
            for (NetworkInterface ni : ifs) {
                putI32(baos, ni.getIndex());
                int mtu;
                try {
                    mtu = ni.getMTU();
                } catch (SocketException e) {
                    mtu = 0;
                }
                putI32(baos, mtu);
                byte[] name = ni.getName()
                        .getBytes(java.nio.charset.StandardCharsets.UTF_8);
                putI32(baos, name.length);
                baos.write(name, 0, name.length);
                byte[] hw;
                try {
                    hw = ni.getHardwareAddress();
                } catch (SocketException e) {
                    hw = null;
                }
                if (hw == null) {
                    hw = new byte[0];
                }
                putI32(baos, hw.length);
                baos.write(hw, 0, hw.length);
                int flags = 0;
                try {
                    if (ni.isUp()) {
                        flags |= 1;
                    }
                    if (ni.isLoopback()) {
                        flags |= 4;
                    }
                    if (ni.isPointToPoint()) {
                        flags |= 8;
                    }
                    if (ni.supportsMulticast()) {
                        flags |= 16;
                    }
                } catch (SocketException e) {
                    // Keep whatever flags were collected.
                }
                putI32(baos, flags);
            }
            byte[] data = baos.toByteArray();
            if (!(bufArg instanceof GoSlice gs) || gs.len() < data.length) {
                return -ENOMEM;
            }
            int n = goBufPut(bufArg, data);
            return n < 0 ? -GoOs.EINVAL : (long) n + 1;
    }

    private static void putI32(java.io.ByteArrayOutputStream out, int v) {
        out.write(v & 0xFF);
        out.write((v >>> 8) & 0xFF);
        out.write((v >>> 16) & 0xFF);
        out.write((v >>> 24) & 0xFF);
    }


    /** graalgo.net.shutdown — 0 or -errno. */
    public static long shutdown(Object cookieArg, Object howArg) {        long cookie = num(cookieArg);
        int how = num32(howArg);
        // Validate how first (P2-4): an invalid how is EINVAL regardless
        // of socket type (gc/kernel parity; previously fell through to 0).
        if (how != SHUT_RD && how != SHUT_WR && how != SHUT_RDWR) {
            return -GoOs.EINVAL;
        }
        SockState st = STATES.get(cookie);
        java.nio.channels.Channel ch = GoOs.socketChannel(cookie);
        if (st == null || ch == null) {
            return -GoOs.EBADF;
        }
        if (ch instanceof SocketChannel sc) {
            try {
                if (how == SHUT_RD || how == SHUT_RDWR) {
                    sc.shutdownInput();
                }
                if (how == SHUT_WR || how == SHUT_RDWR) {
                    sc.shutdownOutput();
                }
                return 0;
            } catch (IOException e) {
                return -sockErrno(e);
            } catch (IllegalArgumentException e) {
                return -GoOs.EINVAL;
            }
        }
        if (ch instanceof DatagramChannel) {
            // shutdown(2) is legal on a connected datagram on Linux, but
            // the JDK exposes no API for it: loud EOPNOTSUPP when
            // connected instead of the previous wrong ENOTCONN (P2-4).
            return st.kind == SockStateKind.CONNECTED ? -EOPNOTSUPP : -ENOTCONN;
        }
        return -ENOTCONN; // listener (gc: ENOTCONN/EOPNOTSUPP)
    }

    /** graalgo.net.setnonblock — records the flag; channels stay nonblocking. 0 or -errno. */
    public static long setnonblock(Object cookieArg, Object nbArg) {
        long cookie = num(cookieArg);
        SockState st = STATES.get(cookie);
        if (st == null || GoOs.socketChannel(cookie) == null) {
            return -GoOs.EBADF;
        }
        st.nonblocking = num(nbArg) != 0;
        return 0;
    }

    /** graalgo.net.sendto — n+1 or -errno. Datagrams only (streams use graalgo.os.write). */
    public static long sendto(Object cookieArg, Object bufArg, Object addrArg) {
        long cookie = num(cookieArg);
        java.nio.channels.Channel ch = GoOs.socketChannel(cookie);
        if (ch == null || STATES.get(cookie) == null) {
            return -GoOs.EBADF;
        }
        if (!(ch instanceof DatagramChannel dc)) {
            return -EOPNOTSUPP;
        }
        ByteBuffer bb = goBuf(bufArg);
        int want = bb.remaining();
        try {
            final int n;
            ByteBuffer ab = goBuf(addrArg);
            if (!ab.hasRemaining()) {
                n = dc.write(bb); // connected
            } else {
                n = dc.send(bb, parseAddrBuf(ab)); // P2-6: single addr copy
            }
            // Nonblocking: 0 = would block (fork maps to EAGAIN when want > 0).
            return (long) n + 1;
        } catch (IllegalArgumentException e) {
            return -GoOs.EINVAL;
        } catch (IOException e) {
            return -sockErrno(e);
        }
    }

    /** graalgo.net.recvfrom — n+1 or -errno; peer wire address written into addrArg. */
    public static long recvfrom(Object cookieArg, Object bufArg, Object addrArg) {
        long cookie = num(cookieArg);
        java.nio.channels.Channel ch = GoOs.socketChannel(cookie);
        if (ch == null || STATES.get(cookie) == null) {
            return -GoOs.EBADF;
        }
        if (!(ch instanceof DatagramChannel dc)) {
            return -EOPNOTSUPP;
        }
        ByteBuffer bb = goBuf(bufArg);
        try {
            SocketAddress src = dc.receive(bb);
            if (src == null) {
                return -EAGAIN; // nonblocking, nothing queued (gc parity)
            }
            int n = bb.position();
            goBufBack(bufArg, bb, n);
            byte[] wire = src instanceof InetSocketAddress isa
                    ? renderAddr(isa) : new byte[]{(byte) 0};
            if (goBufPut(addrArg, wire) < 0) {
                return -EMSGSIZE; // peer buffer too small for the wire address
            }
            return (long) n + 1;
        } catch (IOException e) {
            return -sockErrno(e);
        }
    }
}
