package graalgo.p3tls;

import java.lang.reflect.Field;
import java.nio.ByteBuffer;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicLong;

import javax.net.ssl.SSLEngine;
import javax.net.ssl.SSLEngineResult;
import javax.net.ssl.SSLException;
import javax.net.ssl.SSLSession;

import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Unit tests for the I-8 wrap errno side-channel ({@link TlsRuntime#wrap},
 * {@link TlsRuntime#wrapError}, {@code graalgo.tls.wrapError}).
 *
 * <p>Covered: (a) take semantics — each recorded failure is observed at
 * most once; (b) the shared 0/1/2 code namespace (0=none, 1=bad handle,
 * 2=engine threw); {@link TlsRuntime#close} cleanup of the side-channel.
 * Not covered here: (c) the Go-side 10k-iteration staleness bound in
 * {@code Conn.Handshake} — it lives in synthetic Go driven through the
 * linkname traps and cannot be unit-tested without a stuck SSLEngine;
 * it is covered by the TLS differential legs in {@code make
 * snapshot-check} (no happy-path drift) plus the fail-fast path tested
 * here, which is what fires on real wrap failures.
 *
 * <p>White-box: engine handles are injected reflectively into
 * TlsRuntime's private table so the engine-threw path is exercised with
 * a real {@link SSLEngine#wrap} throwing {@link SSLException}, the
 * actual production failure mode.
 */
public final class TlsWrapErrorTest {
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

    private static GoSlice emptyBytes() {
        return new GoSlice(new byte[0], 0, 0, 0);
    }

    /** Real client engine handle (public API, handshake begun). */
    private static long newBegunClient() {
        long h = TlsRuntime.newClientEngine(
                GoString.fromUtf8(""), 0L, 0L, 0L, emptyBytes());
        checkEq(0, TlsRuntime.beginHandshake(h), "beginHandshake");
        return h;
    }

    /**
     * SSLEngine whose wrap() always throws — the production failure mode
     * (e.g. the engine dies mid-handshake). All other abstract methods
     * are inert stubs; only wrap is ever called in these tests.
     *
     * <p>Note the exact abstract signatures (verified with javap): the
     * throwing entry points are the 4-arg wrap/unwrap forms; the 2-arg
     * forms are concrete in SSLEngine and delegate to them.
     */
    private static final class ThrowingEngine extends SSLEngine {
        @Override
        public SSLEngineResult wrap(ByteBuffer src, ByteBuffer dst)
                throws SSLException {
            throw new SSLException("injected wrap failure");
        }

        @Override
        public SSLEngineResult wrap(ByteBuffer[] srcs, int offset, int len,
                                    ByteBuffer dst)
                throws SSLException {
            throw new SSLException("injected wrap failure");
        }

        @Override
        public SSLEngineResult unwrap(ByteBuffer src, ByteBuffer dst)
                throws SSLException {
            throw new SSLException("injected unwrap failure");
        }

        @Override
        public SSLEngineResult unwrap(ByteBuffer src, ByteBuffer[] dsts,
                                      int offset, int len)
                throws SSLException {
            throw new SSLException("injected unwrap failure");
        }

        @Override
        public void beginHandshake() throws SSLException {
        }

        @Override
        public void closeInbound() throws SSLException {
        }

        @Override
        public void closeOutbound() {
        }

        @Override
        public Runnable getDelegatedTask() {
            return null;
        }

        @Override
        public boolean isInboundDone() {
            return false;
        }

        @Override
        public boolean isOutboundDone() {
            return false;
        }

        @Override
        public String[] getEnabledCipherSuites() {
            return new String[0];
        }

        @Override
        public void setEnabledCipherSuites(String[] suites) {
        }

        @Override
        public String[] getSupportedCipherSuites() {
            return new String[0];
        }

        @Override
        public String[] getEnabledProtocols() {
            return new String[0];
        }

        @Override
        public void setEnabledProtocols(String[] protocols) {
        }

        @Override
        public String[] getSupportedProtocols() {
            return new String[0];
        }

        @Override
        public SSLSession getSession() {
            return null;
        }

        @Override
        public String getPeerHost() {
            return "";
        }

        @Override
        public int getPeerPort() {
            return 0;
        }

        @Override
        public void setUseClientMode(boolean mode) {
        }

        @Override
        public boolean getUseClientMode() {
            return true;
        }

        @Override
        public void setNeedClientAuth(boolean need) {
        }

        @Override
        public boolean getNeedClientAuth() {
            return false;
        }

        @Override
        public void setWantClientAuth(boolean want) {
        }

        @Override
        public boolean getWantClientAuth() {
            return false;
        }

        @Override
        public void setEnableSessionCreation(boolean flag) {
        }

        @Override
        public boolean getEnableSessionCreation() {
            return true;
        }

        @Override
        public SSLEngineResult.HandshakeStatus getHandshakeStatus() {
            return SSLEngineResult.HandshakeStatus.NEED_WRAP;
        }
    }

    @SuppressWarnings("unchecked")
    private static ConcurrentHashMap<Long, SSLEngine> engines()
            throws Exception {
        Field f = TlsRuntime.class.getDeclaredField("ENGINES");
        f.setAccessible(true);
        return (ConcurrentHashMap<Long, SSLEngine>) f.get(null);
    }

    private static long nextHandle() throws Exception {
        Field f = TlsRuntime.class.getDeclaredField("NEXT_ENGINE_ID");
        f.setAccessible(true);
        return ((AtomicLong) f.get(null)).getAndIncrement();
    }

    /** Injects an engine, returning its fresh handle. */
    private static long injectEngine(SSLEngine engine) throws Exception {
        long h = nextHandle();
        engines().put(h, engine);
        return h;
    }

    // (a)+(b): bad handle records code 1; take clears; unknown reads 0.
    public void testBadHandleRecordsAndTakes() {
        long bogus = 0x7FFFFFFFFFFFFFFFL;
        check(TlsRuntime.wrap(bogus, emptyBytes()) == null,
                "wrap on bad handle returns null");
        checkEq(TlsRuntime.TLS_ERR_BAD_HANDLE, TlsRuntime.wrapError(bogus),
                "first take yields BAD_HANDLE");
        checkEq(TlsRuntime.TLS_ERR_NONE, TlsRuntime.wrapError(bogus),
                "second take yields NONE (take semantics)");
        checkEq(TlsRuntime.TLS_ERR_NONE,
                TlsRuntime.wrapError(0x7FFFFFFFFFFFFFFEL),
                "wrapError on never-failed handle yields NONE");
    }

    // (b): engine-threw records code 2 via the real SSLEngine.wrap path.
    public void testEngineThrowRecordsCode2() throws Exception {
        long h = injectEngine(new ThrowingEngine());
        try {
            check(TlsRuntime.wrap(h, emptyBytes()) == null,
                    "wrap on throwing engine returns null");
            checkEq(TlsRuntime.TLS_ERR_ENGINE_THREW, TlsRuntime.wrapError(h),
                    "first take yields ENGINE_THREW");
            checkEq(TlsRuntime.TLS_ERR_NONE, TlsRuntime.wrapError(h),
                    "second take yields NONE (take semantics)");
        } finally {
            TlsRuntime.close(h);
        }
    }

    // Success records nothing and clears a stale recorded error.
    public void testSuccessClearsStaleError() throws Exception {
        long h = injectEngine(new ThrowingEngine());
        try {
            check(TlsRuntime.wrap(h, emptyBytes()) == null,
                    "wrap on throwing engine returns null");
            // Do NOT take: the stale ENGINE_THREW stays recorded.
            // Swap in a real begun engine under the same handle, then
            // succeed: the stale entry must be gone.
            long real = newBegunClient();
            ConcurrentHashMap<Long, SSLEngine> table = engines();
            SSLEngine good = table.remove(real);
            check(good != null, "real engine present");
            table.put(h, good);
            Object out = TlsRuntime.wrap(h, emptyBytes());
            check(out != null, "wrap on real engine returns non-null");
            checkEq(TlsRuntime.TLS_ERR_NONE, TlsRuntime.wrapError(h),
                    "success clears the stale recorded error");
        } finally {
            TlsRuntime.close(h);
        }
    }

    // close() drops the side-channel entry: no stale error survives.
    public void testCloseCleansWrapError() throws Exception {
        long h = injectEngine(new ThrowingEngine());
        check(TlsRuntime.wrap(h, emptyBytes()) == null,
                "wrap on throwing engine returns null");
        TlsRuntime.close(h);
        checkEq(TlsRuntime.TLS_ERR_NONE, TlsRuntime.wrapError(h),
                "close removes the recorded error (no stale take)");
        // The handle is now genuinely bad: fresh failures still record.
        check(TlsRuntime.wrap(h, emptyBytes()) == null,
                "wrap after close returns null");
        checkEq(TlsRuntime.TLS_ERR_BAD_HANDLE, TlsRuntime.wrapError(h),
                "wrap after close records BAD_HANDLE");
    }

    // Happy path: real engine wrap succeeds, side-channel stays empty.
    public void testSuccessfulWrapRecordsNothing() {
        long h = newBegunClient();
        try {
            Object out = TlsRuntime.wrap(h, emptyBytes());
            check(out instanceof GoSlice, "wrap returns GoSlice");
            check(((GoSlice) out).len() > 0,
                    "handshake wrap produces ClientHello bytes");
            checkEq(TlsRuntime.TLS_ERR_NONE, TlsRuntime.wrapError(h),
                    "successful wrap records no error");
        } finally {
            TlsRuntime.close(h);
        }
    }
}
