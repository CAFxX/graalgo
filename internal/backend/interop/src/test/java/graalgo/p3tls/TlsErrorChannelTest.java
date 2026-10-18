package graalgo.p3tls;

import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;

import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Unit tests for the R23-P1-3 TLS error channel: no host {@code Exception}
 * may escape a {@code graalgo.tls.*} trap. Failures travel as negative
 * error-slot keys (long-returning traps) or handle-scoped errors
 * (Object-returning traps), retrievable via {@link TlsRuntime#lastError}.
 *
 * <p>Style follows {@code p3os/GoOsErrnoTest}: plain {@code test*} methods
 * run by {@code IfaceTestRunner}, {@code check}/{@code checkEq} assertions.
 *
 * <p>Protocol hygiene: every test that triggers a negative return consumes
 * the slot via {@code lastError} (slots are consume-on-read).
 */
public final class TlsErrorChannelTest {

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

    private static GoString gs(String s) {
        return GoString.fromUtf8(s);
    }

    private static GoSlice slice(byte[] b) {
        return new GoSlice(b, 0, b.length, b.length);
    }

    private static GoSlice emptySlice() {
        return slice(new byte[0]);
    }

    /** Reads (consuming) the error detail for a slot key or handle. */
    private static String errMsg(long key) {
        Object o = TlsRuntime.lastError(Long.valueOf(key));
        return new String(((GoString) o).bytes(), StandardCharsets.UTF_8);
    }

    /** Builds a valid client engine (no I/O) for handle-scoped tests. */
    private static long validEngine() {
        long h = TlsRuntime.newClientEngine(gs(""), Long.valueOf(0),
                Long.valueOf(0), Long.valueOf(0), emptySlice());
        check(h >= 1, "validEngine handle, got " + h);
        return h;
    }

    // 1: constructor failure -> slot + detail + consume-on-read; no throw.
    public void testNewClientEngineBadSni() {
        long r = TlsRuntime.newClientEngine(gs("not a hostname!"),
                Long.valueOf(0), Long.valueOf(0), Long.valueOf(0),
                emptySlice());
        check(r < 0, "bad SNI must return a negative slot, got " + r);
        String m = errMsg(r);
        check(!m.isEmpty(), "slot message must be non-empty");
        check(m.contains("IllegalArgumentException"),
                "slot message must name the cause, got: " + m);
        checkEq(0, errMsg(r).length(), "second lastError must be empty");
    }

    // 2: cert-material failure channeled.
    public void testNewServerEngineGarbageChain() {
        byte[] garbage = new byte[]{0, 0, 0, 5, 1, 2, 3, 4, 5};
        long r = TlsRuntime.newServerEngine(slice(garbage), gs("RSA"),
                emptySlice(), Long.valueOf(0), Long.valueOf(0),
                emptySlice());
        check(r < 0, "garbage chainDER must return a negative slot, got " + r);
        String m = errMsg(r);
        check(m.contains("CertificateException"),
                "slot message must mention certificate parsing, got: " + m);
    }

    // 3: the old line-404 throw becomes a channeled error.
    public void testNewServerEngineUnsupportedKeyAlg() {
        long r = TlsRuntime.newServerEngine(emptySlice(), gs("DSA"),
                emptySlice(), Long.valueOf(0), Long.valueOf(0),
                emptySlice());
        check(r < 0, "unsupported keyAlg must return a negative slot, got " + r);
        String m = errMsg(r);
        check(m.contains("unsupported keyAlg"),
                "slot message must name the keyAlg failure, got: " + m);
    }

    // 4: str() consolidation — wrong-typed serverName.
    public void testNewClientEngineWrongServerNameType() {
        long r = TlsRuntime.newClientEngine(new Object(), Long.valueOf(0),
                Long.valueOf(0), Long.valueOf(0), emptySlice());
        check(r < 0, "wrong-typed serverName must not throw, got " + r);
        String m = errMsg(r);
        check(m.contains("expected GoString"),
                "slot message must preserve the old text, got: " + m);
    }

    // 5: num() consolidation in constructors — wrong-typed minVersion.
    public void testNewClientEngineWrongVersionType() {
        long r = TlsRuntime.newClientEngine(gs(""), Long.valueOf(0),
                "nope", Long.valueOf(0), emptySlice());
        check(r < 0, "wrong-typed minVersion must not throw, got " + r);
        String m = errMsg(r);
        check(m.contains("expected Number"),
                "slot message must preserve the old text, got: " + m);
    }

    // 6: num() consolidation in long-returning traps — none throws.
    public void testLongTrapsWrongHandleType() {
        long[] slots = new long[]{
            TlsRuntime.beginHandshake("nope"),
            TlsRuntime.engineStatus(new Object()),
            TlsRuntime.hasPending(new Object()),
            TlsRuntime.runTasks(new Object()),
            TlsRuntime.connectionState(new Object()),
            TlsRuntime.close(new Object()),
        };
        String[] names = {"beginHandshake", "engineStatus", "hasPending",
            "runTasks", "connectionState", "close"};
        for (int i = 0; i < slots.length; i++) {
            check(slots[i] < 0, names[i] + " with bad handle must not throw");
            // Consume each slot (protocol hygiene); each must carry detail.
            String m = errMsg(slots[i]);
            check(m.contains("expected Number"),
                    names[i] + " slot must preserve the old text, got: " + m);
        }
    }

    // 7: num() consolidation in Object-returning traps ("no key" limitation).
    public void testObjectTrapsWrongHandleType() {
        check(TlsRuntime.wrap(new Object(), emptySlice()) == null,
                "wrap with bad handle must return null, not throw");
        check(TlsRuntime.unwrap(new Object(), emptySlice()) == null,
                "unwrap with bad handle must return null, not throw");
        check(TlsRuntime.peerCertificates(new Object()) != null,
                "peerCertificates with bad handle must return empty, not throw");
    }

    // 8: handle-scoped error for a data-arg violation.
    public void testWrapBadPlaintextType() {
        long h = validEngine();
        try {
            check(TlsRuntime.wrap(h, "not-a-slice") == null,
                    "wrap with bad plaintext must return null, not throw");
            String m = errMsg(h);
            check(m.contains("expected GoSlice"),
                    "handle error must name the violation, got: " + m);
            checkEq(0, errMsg(h).length(),
                    "second lastError(handle) must be empty");
        } finally {
            checkEq(0, TlsRuntime.close(Long.valueOf(h)), "close");
        }
    }

    // 9: runTasks on an unknown handle -> detailed slot, no throw.
    public void testRunTasksUnknownHandle() {
        long r = TlsRuntime.runTasks(Long.valueOf(0x7FFFFFFFFFFFFFFFL));
        check(r < 0, "runTasks on unknown handle must fail, got " + r);
        String m = errMsg(r);
        check(m.contains("unknown engine handle"),
                "slot message must name the unknown handle, got: " + m);
    }

    // 10: slot/handle namespace separation — no cross-talk, either order.
    public void testSlotHandleNamespaceSeparation() {
        long s = TlsRuntime.newClientEngine(gs("not a hostname!"),
                Long.valueOf(0), Long.valueOf(0), Long.valueOf(0),
                emptySlice());
        check(s < 0, "need a failing construction slot");
        long h = validEngine();
        try {
            check(TlsRuntime.wrap(h, "bad") == null, "wrap must fail");
            // Slot first, then handle.
            String ms = errMsg(s);
            String mh = errMsg(h);
            check(ms.contains("newClientEngine"),
                    "slot message must be the construction error, got: " + ms);
            check(mh.contains("wrap.plaintext"),
                    "handle message must be the wrap error, got: " + mh);
        } finally {
            checkEq(0, TlsRuntime.close(Long.valueOf(h)), "close");
        }
        // And the reverse order on a fresh pair.
        long s2 = TlsRuntime.newClientEngine(gs("not a hostname!"),
                Long.valueOf(0), Long.valueOf(0), Long.valueOf(0),
                emptySlice());
        long h2 = validEngine();
        try {
            check(TlsRuntime.wrap(h2, "bad") == null, "wrap must fail");
            String mh2 = errMsg(h2);
            String ms2 = errMsg(s2);
            check(mh2.contains("wrap.plaintext"),
                    "handle message must be the wrap error, got: " + mh2);
            check(ms2.contains("newClientEngine"),
                    "slot message must be the construction error, got: " + ms2);
        } finally {
            checkEq(0, TlsRuntime.close(Long.valueOf(h2)), "close");
        }
    }

    // 11: handle-scoped errors die with the handle.
    public void testCloseClearsHandleError() {
        long h = validEngine();
        check(TlsRuntime.wrap(h, "bad") == null, "wrap must fail");
        checkEq(0, TlsRuntime.close(Long.valueOf(h)), "close returns 0");
        checkEq(0, errMsg(h).length(),
                "lastError(handle) after close must be empty");
    }

    // 12: no-throw invariant sweep — every public static trap with hostile
    // args must return a sentinel, never throw. Pins the P1's core guarantee
    // against future regressions.
    public void testNoThrowSweep() throws Exception {
        List<String> failures = new ArrayList<>();
        for (Method m : TlsRuntime.class.getDeclaredMethods()) {
            if (!Modifier.isPublic(m.getModifiers())
                    || !Modifier.isStatic(m.getModifiers())) {
                continue;
            }
            Object[] args = new Object[m.getParameterCount()];
            for (int i = 0; i < args.length; i++) {
                args[i] = new Object();
            }
            try {
                Object r = m.invoke(null, args);
                // Consume any error slot (protocol hygiene).
                if (r instanceof Long && ((Long) r).longValue() < 0) {
                    TlsRuntime.lastError(r);
                }
            } catch (Exception e) {
                Throwable cause = e.getCause() != null ? e.getCause() : e;
                failures.add(m.getName() + " threw " + cause);
            }
        }
        check(failures.isEmpty(),
                "traps must never throw; failures: " + failures);
    }

    // 13: the accessor never throws — wrong-typed and unknown keys.
    public void testLastErrorNeverThrows() {
        checkEq(0, ((GoString) TlsRuntime.lastError(new Object())).bytes().length,
                "lastError(wrong type) must be empty");
        checkEq(0, ((GoString) TlsRuntime.lastError(Long.valueOf(-424242L)))
                .bytes().length, "lastError(unknown slot) must be empty");
        checkEq(0, ((GoString) TlsRuntime.lastError(Long.valueOf(424242L)))
                .bytes().length, "lastError(unknown handle) must be empty");
    }
}
