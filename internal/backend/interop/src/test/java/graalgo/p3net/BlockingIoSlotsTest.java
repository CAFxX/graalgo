package graalgo.p3net;

import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;

import graalgo.p3os.GoOs;
import graalgo.sched.SchedulerCore;
import graalgo.values.GoSlice;

/**
 * GG-0018 production-call-site tests (review-1105-io-slots B-P1-6):
 * the blocking-I/O entry points graalgo actually exposes —
 * {@link GoHttp#clientDo} and {@link GoOs#read}/{@link GoOs#write} on
 * pipes — must release the goroutine's in-flight slot while blocked,
 * or GOMAXPROCS=1 programs stall: the drainer admits nobody until the
 * blocking call returns. The mechanism itself is pinned by
 * {@code IoSlotsTest}; these tests pin the wiring: a goroutine blocked
 * inside the real intrinsic must not starve a busy peer.
 *
 * <p>verifies: GG-0018, GG-SCHED-0004 (io slice, production call sites)
 */
public final class BlockingIoSlotsTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long want, long got, String msg) {
        if (want != got) {
            throw new AssertionError(msg + ": want " + want + " got " + got);
        }
    }

    private SchedulerCore sched;

    private void withScheduler(int gmp, ThrowingRunnable body) throws Exception {
        sched = new SchedulerCore(gmp, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            body.run();
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }

    private interface ThrowingRunnable {
        void run() throws Exception;
    }

    // ---- minimal loopback HTTP/1.1 server: one slow response ----

    /**
     * Serves a single request on loopback: reads the request head,
     * signals arrival, waits for the test to release it, then answers
     * {@code 200 hello}. Runs on a plain platform thread (never a
     * goroutine), so it cannot hold a scheduler slot.
     */
    private static final class SlowHttpServer implements AutoCloseable {
        final ServerSocket ss;
        final int port;
        final CountDownLatch arrived = new CountDownLatch(1);
        final CountDownLatch release = new CountDownLatch(1);
        final AtomicReference<Throwable> error = new AtomicReference<>();
        private final Thread thread;

        SlowHttpServer() throws Exception {
            ss = new ServerSocket(0, 128, InetAddress.getLoopbackAddress());
            port = ss.getLocalPort();
            thread = Thread.ofPlatform().daemon(true).name("slow-http-server").start(() -> {
                try (Socket sock = ss.accept()) {
                    sock.setSoTimeout(15000);
                    readHead(sock.getInputStream());
                    arrived.countDown();
                    if (!release.await(15, TimeUnit.SECONDS)) {
                        error.set(new AssertionError("server never released"));
                        return;
                    }
                    byte[] body = "hello".getBytes(StandardCharsets.UTF_8);
                    String head = "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n"
                            + "Connection: close\r\n\r\n";
                    OutputStream out = sock.getOutputStream();
                    out.write(head.getBytes(StandardCharsets.UTF_8));
                    out.write(body);
                    out.flush();
                } catch (Throwable t) {
                    error.set(t);
                    arrived.countDown();
                }
            });
        }

        /** Reads bytes until the blank line ending the request head. */
        private static void readHead(InputStream in) throws Exception {
            int b;
            int matched = 0;
            byte[] tail = "\r\n\r\n".getBytes(StandardCharsets.UTF_8);
            int total = 0;
            while ((b = in.read()) != -1) {
                total++;
                if (total > 8192) {
                    throw new AssertionError("request head over 8KB");
                }
                if (b == (tail[matched] & 0xFF)) {
                    matched++;
                    if (matched == tail.length) {
                        return;
                    }
                } else {
                    matched = 0;
                }
            }
            throw new AssertionError("client closed before sending a request head");
        }

        @Override
        public void close() throws Exception {
            ss.close();
            thread.join(5000);
        }
    }

    /** Decodes the GoHttp.clientDo wire format: int64 BE status first. */
    private static long statusOf(Object result) {
        GoSlice gs = (GoSlice) result;
        byte[] arr = (byte[]) gs.array();
        return ByteBuffer.wrap(arr, gs.offset(), 8).getLong();
    }

    /** Decodes the response body from the GoHttp.clientDo wire format. */
    private static String bodyOf(Object result) {
        GoSlice gs = (GoSlice) result;
        byte[] arr = (byte[]) gs.array();
        ByteBuffer bb = ByteBuffer.wrap(arr, gs.offset(), gs.len());
        bb.getLong(); // status
        long headerCount = bb.getLong();
        for (long i = 0; i < headerCount; i++) {
            int klen = (int) bb.getLong();
            bb.position(bb.position() + klen);
            int vlen = (int) bb.getLong();
            bb.position(bb.position() + vlen);
        }
        int bodyLen = (int) bb.getLong();
        byte[] body = new byte[bodyLen];
        bb.get(body);
        return new String(body, StandardCharsets.UTF_8);
    }

    /**
     * GOMAXPROCS=1: a goroutine blocked in {@code http.Get} (real
     * {@link GoHttp#clientDo}, 30 s timeouts) must not starve a busy
     * peer. Pre-fix the peer counter stays 0 (the HTTP goroutine holds
     * the only slot for the whole request) — red. Also asserts the hard
     * cap was never exceeded.
     */
    public void testHttpClientDoReleasesSlot() throws Exception {
        SlowHttpServer server = new SlowHttpServer();
        AtomicLong counter = new AtomicLong();
        AtomicBoolean stop = new AtomicBoolean();
        AtomicReference<Object> result = new AtomicReference<>();
        AtomicReference<Throwable> httpError = new AtomicReference<>();
        try {
            withScheduler(1, () -> {
                String url = "http://127.0.0.1:" + server.port + "/slow";
                sched.spawn(() -> {
                    try {
                        result.set(GoHttp.clientDo("GET", url, null, null));
                    } catch (Throwable t) {
                        httpError.set(t);
                    }
                });
                // Wait until the request arrived: the goroutine is now
                // blocked inside CLIENT.send.
                assertTrue(server.arrived.await(10, TimeUnit.SECONDS),
                        "HTTP request never arrived at the server");
                sched.spawn(() -> {
                    while (!stop.get()) {
                        counter.incrementAndGet();
                    }
                });
                Thread.sleep(1500);
                long c = counter.get();
                try {
                    assertTrue(c > 1000,
                            "busy goroutine starved at GOMAXPROCS=1 while peer in http.Get: " + c);
                } finally {
                    stop.set(true);
                    server.release.countDown();
                }
            });
        } finally {
            server.close();
        }
        assertTrue(server.error.get() == null, "server failed: " + server.error.get());
        assertTrue(httpError.get() == null, "clientDo failed: " + httpError.get());
        assertTrue(result.get() != null, "clientDo returned no response");
        assertEq(200, statusOf(result.get()), "HTTP status");
        assertTrue("hello".equals(bodyOf(result.get())),
                "HTTP body: " + bodyOf(result.get()));
        assertTrue(sched.maxInflightObserved() <= 1,
                "cap violated: maxInflight=" + sched.maxInflightObserved());
    }

    /**
     * GOMAXPROCS=1: a goroutine blocked in {@link GoOs#read} on a pipe
     * (os.Pipe FIFO — the non-socket blocking file case) must not starve
     * a busy peer. Pre-fix the peer counter stays 0 — red. The release
     * write runs on the test thread, which is not a scheduler goroutine:
     * it exercises the unbracketed foreign-thread path of the same
     * intrinsic.
     */
    public void testPipeReadReleasesSlot() throws Exception {
        GoOs.init(new String[0]);
        long r = GoOs.pipe();
        assertTrue(r >= 16, "pipe cookies: " + r);
        long readEnd = r;
        long writeEnd = r + 1;
        AtomicLong counter = new AtomicLong();
        AtomicBoolean stop = new AtomicBoolean();
        AtomicReference<Long> readResult = new AtomicReference<>();
        AtomicReference<Throwable> readError = new AtomicReference<>();
        byte[] backing = new byte[64];
        GoSlice buf = new GoSlice(backing, 0, 64, 64);
        withScheduler(1, () -> {
            sched.spawn(() -> {
                try {
                    readResult.set(GoOs.read(readEnd, buf));
                } catch (Throwable t) {
                    readError.set(t);
                }
            });
            // Let the reader enter the blocking read (it is admitted
            // immediately: first spawn, cap free).
            Thread.sleep(300);
            sched.spawn(() -> {
                while (!stop.get()) {
                    counter.incrementAndGet();
                }
            });
            Thread.sleep(1500);
            long c = counter.get();
            try {
                assertTrue(c > 1000,
                        "busy goroutine starved at GOMAXPROCS=1 while peer in pipe read: " + c);
            } finally {
                stop.set(true);
                // Release the reader: one byte through the pipe (test
                // thread — not a goroutine — so this write is unbracketed).
                GoSlice one = new GoSlice(new byte[]{7}, 0, 1, 1);
                long w = GoOs.write(writeEnd, one);
                assertEq(2, w, "pipe write n+1");
            }
        });
        assertTrue(readError.get() == null, "pipe read failed: " + readError.get());
        assertTrue(readResult.get() != null, "pipe read returned nothing");
        assertEq(2, readResult.get(), "pipe read n+1");
        assertTrue(backing[0] == 7, "pipe byte: " + backing[0]);
        assertTrue(sched.maxInflightObserved() <= 1,
                "cap violated: maxInflight=" + sched.maxInflightObserved());
    }
}
