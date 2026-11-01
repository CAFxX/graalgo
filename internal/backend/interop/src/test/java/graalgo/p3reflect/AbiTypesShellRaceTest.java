package graalgo.p3reflect;

import graalgo.frontend.Json;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;

/**
 * R-02 (2026-11-01): {@code AbiTypes.unpackInterfaceView} and
 * {@code tokForShell} read the {@code IdentityHashMap SHELL_TOKS}
 * without the {@code AbiTypes} class monitor every other accessor
 * uses, while {@code abiPointerForTok} {@code clear()}s and
 * {@code put()}s it (including a table-change memo drop and
 * resize-inducing bulk mints). Both readers run at guest runtime
 * (unsafe conversions via {@code UnsafeToPtrNode}, reflect
 * intrinsics via {@code ReflectLinknames.tokForTypeArg}) on scheduler
 * vthreads, so the race is reachable from concurrent guest goroutines,
 * not just a static-analysis smell — a concurrent read during a
 * {@code clear()}/resize could return a stale or wrong token (silent
 * wrong dynamic type for an interface value).
 *
 * <p>Tests:
 * <ul>
 *   <li>functional: an interface-word struct built over a minted shell
 *       unpacks to the token's descriptor;</li>
 *   <li>monitor: {@code unpackInterfaceView} and {@code tokForShell}
 *       block while another thread holds the {@code AbiTypes} monitor
 *       — on the unfixed code the worker completes immediately, so
 *       this test fails pre-fix (it is the deterministic proof the
 *       readers share the writer's monitor);</li>
 *   <li>stress: one thread repeatedly installs a fresh table and
 *       re-mints ~65 function-type shells (forcing
 *       {@code IdentityHashMap} growth), another unpacks the latest
 *       shell in a loop — every non-null result must carry exactly
 *       the holder's token descriptor, never a wrong one, and no
 *       thread may throw.</li>
 * </ul>
 *
 * <p>Registered in {@code graalgo.iface.IfaceTestRunner}.
 */
public final class AbiTypesShellRaceTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(Object expected, Object actual, String msg) {
        if (expected == null ? actual != null : !expected.equals(actual)) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    private static Json.Object parseDoc(String text) {
        Json.Value v = Json.parse(text);
        if (!(v instanceof Json.Object doc)) {
            throw new AssertionError("test doc must be a JSON object");
        }
        return doc;
    }

    /**
     * Minimal wire document: a basic {@code int}, one 1-param function
     * signature (the primary token), the {@code internal/abi.Type}
     * header struct and the {@code internal/abi.FuncType} extension
     * struct that {@code abiPointerForTok} needs to mint a function
     * shell, plus 64 further signatures (0..63 int params) so the
     * stress thread mints enough shells to grow {@code SHELL_TOKS}.
     * Function types are used deliberately: they are not comparable,
     * so shell filling never calls {@code equalFunc()} (which builds
     * a Truffle {@code RootNode}), keeping this a pure unit test.
     */
    private static String buildDoc() {
        StringBuilder sb = new StringBuilder("{\"types\": [");
        sb.append("{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"int\", \"class\": \"int\", \"untyped\": false}},");
        sb.append("{\"kind\": \"signature\", \"detail\": {\"params\": "
                + "[{\"name\": \"v\", \"type\": 0}], \"results\": [], \"variadic\": false}},");
        sb.append("{\"kind\": \"struct\", \"detail\": {\"fields\": [");
        String[] headerLongs = {"Size_", "PtrBytes", "Hash", "TFlag", "Align_", "FieldAlign_", "Kind_"};
        int off = 0;
        for (String name : headerLongs) {
            sb.append("{\"name\": \"").append(name).append("\", \"type\": 0, \"tag\": \"\", \"offset\": ")
                    .append(off).append("},");
            off += 8;
        }
        sb.append("{\"name\": \"Equal\", \"type\": 1, \"tag\": \"\", \"offset\": ").append(off).append("},");
        off += 8;
        sb.append("{\"name\": \"GCData\", \"type\": 3, \"tag\": \"\", \"offset\": ").append(off).append("},");
        off += 8;
        sb.append("{\"name\": \"Str\", \"type\": 0, \"tag\": \"\", \"offset\": ").append(off).append("},");
        off += 8;
        sb.append("{\"name\": \"PtrToThis\", \"type\": 0, \"tag\": \"\", \"offset\": ").append(off).append("}");
        sb.append("]}},");
        sb.append("{\"kind\": \"pointer\", \"detail\": {\"elem\": 0, \"methods\": []}},");
        sb.append("{\"kind\": \"named\", \"detail\": {\"name\": \"internal/abi.Type\", "
                + "\"pkgpath\": \"internal/abi\", \"underlying\": 2, \"methods\": []}},");
        sb.append("{\"kind\": \"struct\", \"detail\": {\"fields\": ["
                + "{\"name\": \"Type\", \"type\": 2, \"tag\": \"\", \"offset\": 0},"
                + "{\"name\": \"InCount\", \"type\": 0, \"tag\": \"\", \"offset\": 8},"
                + "{\"name\": \"OutCount\", \"type\": 0, \"tag\": \"\", \"offset\": 16}]}},");
        sb.append("{\"kind\": \"named\", \"detail\": {\"name\": \"internal/abi.FuncType\", "
                + "\"pkgpath\": \"internal/abi\", \"underlying\": 5, \"methods\": []}}");
        for (int k = 0; k < 64; k++) {
            sb.append(",{\"kind\": \"signature\", \"detail\": {\"params\": [");
            for (int i = 0; i < k; i++) {
                if (i > 0) {
                    sb.append(",");
                }
                sb.append("{\"name\": \"p").append(i).append("\", \"type\": 0}");
            }
            sb.append("], \"results\": [], \"variadic\": false}}");
        }
        sb.append("]}");
        return sb.toString();
    }

    private static final List<AbiTypes.ViewField> EFACE_LAYOUT = List.of(
            new AbiTypes.ViewField("Type", 0, AbiTypes.ViewRole.ABI_TYPE),
            new AbiTypes.ViewField("Data", 8, AbiTypes.ViewRole.DATA));

    private static final GoStructDescriptor EFACE_DESC = GoStructDescriptor.layout(
            "internal/abi.EmptyInterface",
            List.of("Type", "Data"), List.of(GoKind.POINTER, GoKind.INT64));

    /** An {@code EmptyInterface} word struct over {@code shellPtr} with a zero Data word. */
    private static GoStruct efaceStruct(GoPointer shellPtr) {
        GoStruct s = GoStruct.zero(EFACE_DESC);
        s.setObject(0, shellPtr);
        s.setLong(1, 0);
        return s;
    }

    /** Installs the test table and returns every FUNC token it built. */
    private static List<Long> installAndFuncToks(Json.Object doc) {
        ReflectTypes.install(doc, Map.of());
        ReflectTypes.Table table = ReflectTypes.table();
        List<Long> toks = new ArrayList<>();
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti != null && ti.kind == GoKind.FUNC) {
                toks.add(ti.tok);
            }
        }
        assertTrue(!toks.isEmpty(), "no FUNC token built from test doc");
        return toks;
    }

    public void testUnpackRecoversMintedShell() {
        Json.Object doc = parseDoc(buildDoc());
        List<Long> toks = installAndFuncToks(doc);
        long tok = toks.get(0);
        GoPointer ptr = AbiTypes.abiPointerForTok(tok);
        assertTrue(ptr.referent() instanceof GoStruct, "minted shell is not a GoStruct");
        GoInterface gi = AbiTypes.unpackInterfaceView(efaceStruct(ptr), EFACE_LAYOUT);
        assertTrue(gi != null, "unpack of a minted shell must not return null");
        assertEquals(ReflectTypes.descriptorForTok(tok), gi.descriptor(),
                "unpack recovered the wrong dynamic type");
        assertTrue(gi.data() == null, "zero Data word must unpack to null data");
        assertEquals(tok, AbiTypes.tokForShell((GoStruct) ptr.referent()),
                "tokForShell must recover the minted token");
        assertEquals(0L, AbiTypes.tokForShell(GoStruct.zero(EFACE_DESC)),
                "tokForShell of a non-shell struct must be 0");
    }

    public void testUnpackAndTokForShellBlockOnClassMonitor() throws Exception {
        Json.Object doc = parseDoc(buildDoc());
        List<Long> toks = installAndFuncToks(doc);
        GoPointer ptr = AbiTypes.abiPointerForTok(toks.get(0));
        GoStruct eface = efaceStruct(ptr);
        GoStruct shell = (GoStruct) ptr.referent();

        CountDownLatch unpackDone = new CountDownLatch(1);
        AtomicReference<GoInterface> unpackResult = new AtomicReference<>();
        Thread unpacker = new Thread(() -> {
            unpackResult.set(AbiTypes.unpackInterfaceView(eface, EFACE_LAYOUT));
            unpackDone.countDown();
        });
        synchronized (AbiTypes.class) {
            unpacker.start();
            assertTrue(!unpackDone.await(1, TimeUnit.SECONDS),
                    "unpackInterfaceView completed while the AbiTypes monitor was held"
                            + " — it is not synchronized on the writer's monitor (R-02 unfixed)");
        }
        assertTrue(unpackDone.await(5, TimeUnit.SECONDS),
                "unpackInterfaceView did not complete after the monitor was released");
        assertTrue(unpackResult.get() != null, "unpack after monitor release must succeed");

        CountDownLatch tokDone = new CountDownLatch(1);
        AtomicLong tokResult = new AtomicLong(-1);
        Thread toker = new Thread(() -> {
            tokResult.set(AbiTypes.tokForShell(shell));
            tokDone.countDown();
        });
        synchronized (AbiTypes.class) {
            toker.start();
            assertTrue(!tokDone.await(1, TimeUnit.SECONDS),
                    "tokForShell completed while the AbiTypes monitor was held"
                            + " — it is not synchronized on the writer's monitor (R-02 unfixed)");
        }
        assertTrue(tokDone.await(5, TimeUnit.SECONDS),
                "tokForShell did not complete after the monitor was released");
        assertEquals(toks.get(0), tokResult.get(), "tokForShell after monitor release is wrong");
    }

    private record Holder(GoStruct shell, GoPointer ptr, long tok) {}

    public void testConcurrentMintAndUnpackNeverCorrupts() throws Exception {
        Json.Object doc = parseDoc(buildDoc());
        List<Long> toks = installAndFuncToks(doc);
        long primaryTok = toks.get(0);
        AtomicReference<Holder> holder = new AtomicReference<>();
        AtomicReference<Throwable> failure = new AtomicReference<>();
        AtomicLong unpacks = new AtomicLong();
        AtomicLong recovered = new AtomicLong();
        long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(3);

        Thread writer = new Thread(() -> {
            try {
                while (System.nanoTime() < deadline && failure.get() == null) {
                    ReflectTypes.install(doc, Map.of());
                    for (long tok : toks) {
                        GoPointer ptr = AbiTypes.abiPointerForTok(tok);
                        if (tok == primaryTok) {
                            holder.set(new Holder((GoStruct) ptr.referent(), ptr, tok));
                        }
                    }
                }
            } catch (Throwable t) {
                failure.compareAndSet(null, t);
            }
        });
        Thread reader = new Thread(() -> {
            try {
                while (System.nanoTime() < deadline && failure.get() == null) {
                    Holder h = holder.get();
                    if (h == null) {
                        Thread.yield();
                        continue;
                    }
                    GoInterface gi = AbiTypes.unpackInterfaceView(efaceStruct(h.ptr()), EFACE_LAYOUT);
                    unpacks.incrementAndGet();
                    if (gi != null) {
                        recovered.incrementAndGet();
                        if (!ReflectTypes.descriptorForTok(h.tok()).equals(gi.descriptor())) {
                            throw new AssertionError("unpack recovered a descriptor for the wrong token"
                                    + " (corrupt SHELL_TOKS read): holder tok=" + h.tok());
                        }
                        if (gi.data() != null) {
                            throw new AssertionError("unpack with zero Data word yielded non-null data");
                        }
                    }
                    long t = AbiTypes.tokForShell(h.shell());
                    if (t != 0 && t != h.tok()) {
                        throw new AssertionError("tokForShell returned the wrong token: " + t
                                + " for holder tok " + h.tok());
                    }
                }
            } catch (Throwable t) {
                failure.compareAndSet(null, t);
            }
        });
        writer.start();
        reader.start();
        // Retry-loop join, not a single bounded join: the VM's load
        // shedder may SIGSTOP this JVM mid-test (a frozen worker is
        // not a hang — see the repo's load-shed notes). A single
        // join(30000) can expire exactly at a freeze-thaw boundary
        // while the workers still need a scheduling quantum to exit,
        // which reads as a false "did not finish". The workers'
        // loops are nanoTime-deadline-bounded and the code under test
        // holds exactly one monitor (the AbiTypes class monitor, never
        // acquired while holding another lock), so there is no
        // deadlock cycle for a retry loop to mask: a genuinely stuck
        // pair stays alive through all 120s of running time.
        boolean done = false;
        for (int i = 0; i < 12 && !done; i++) {
            writer.join(10000);
            reader.join(10000);
            done = !writer.isAlive() && !reader.isAlive();
        }
        assertTrue(done, "stress threads did not finish (writer alive=" + writer.isAlive()
                + ", reader alive=" + reader.isAlive() + ")");
        assertTrue(failure.get() == null, "concurrent mint/unpack failed: " + failure.get());
        assertTrue(unpacks.get() > 0, "stress loop performed no unpacks");
        // Deterministic tail: the current holder shell must unpack right now.
        Holder h = holder.get();
        assertTrue(h != null, "writer never published a holder shell");
        ReflectTypes.install(doc, Map.of());
        GoPointer ptr = AbiTypes.abiPointerForTok(h.tok());
        GoInterface gi = AbiTypes.unpackInterfaceView(efaceStruct(ptr), EFACE_LAYOUT);
        assertTrue(gi != null, "final single-threaded unpack of the minted shell must succeed");
        assertEquals(ReflectTypes.descriptorForTok(h.tok()), gi.descriptor(),
                "final unpack recovered the wrong dynamic type");
    }
}
