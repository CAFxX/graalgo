package graalgo.p3reflect;

import graalgo.frontend.Json;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import graalgo.values.GoTypeDescriptor;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

/**
 * R03-B1 (REVIEW-2026-11-09-03): concurrent-access torture tests for the
 * {@code AbiTypes} shell memo.
 *
 * <p>{@code SHELL_TOKS} is a plain {@code IdentityHashMap} cleared and
 * filled under the class lock by {@code abiPointerForTok}, but read with
 * no lock by {@code tokForShell} and by the {@code unpackInterfaceView}
 * lookup. A {@code get} racing a {@code put}/resize (or the table-change
 * {@code clear}) can return a stale/wrong token — a minted shell reads as
 * "unknown type" — or loop/corrupt the table. Both readers are reachable
 * from concurrent guest goroutines ({@code ReflectLinknames} via
 * {@code tokForShell}; {@code UnsafeToPtrNode} via
 * {@code unpackInterfaceView}).
 *
 * <p>Falsifiability: on the pre-fix code the round-trip test observes a
 * {@code tokForShell} miss for a minted shell and the unpack test
 * observes a null/wrong-descriptor unpack for a minted shell (both are
 * timing-dependent — a missed race on a given run is not proof of
 * safety, since unsynchronized access to a non-thread-safe map is a
 * latent race regardless). On the fixed code both tests pass
 * deterministically: readers take the same lock as writers.
 *
 * <p>Registered in {@code graalgo.iface.IfaceTestRunner}.
 */
public final class AbiShellRaceTest {

    private static final int THREADS = 8;

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
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
     * Builds the fixture table: the {@code internal/abi} named types
     * ({@code abiPointerForTok} builds each shell from the wire
     * descriptor of {@code internal/abi.Type} / {@code StructType} /
     * {@code StructField} / {@code Name} and {@code fillAbiStruct}
     * touches every field listed here) plus {@code structCount}
     * structurally distinct struct types (varying field counts). Wire
     * ids: 0 int, 1 byte, 2 *int, 3 *byte, 4/5 Name, 6/7 Type, 8/9
     * StructField, 10 *Type, 11 []StructField, 12/13 StructType; the
     * test structs follow at 14...
     */
    private static String fixtureDoc(int structCount) {
        StringBuilder sb = new StringBuilder("{\"types\": [");
        sb.append("{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"int\", \"class\": \"int\", \"untyped\": false}},");
        sb.append("{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"byte\", \"class\": \"int\", \"untyped\": false}},");
        sb.append("{\"kind\": \"pointer\", \"detail\": {\"elem\": 0, \"methods\": []}},");
        sb.append("{\"kind\": \"pointer\", \"detail\": {\"elem\": 1, \"methods\": []}},");
        sb.append("{\"kind\": \"named\", \"detail\": {\"name\": \"internal/abi.Name\", "
                + "\"pkgpath\": \"internal/abi\", \"underlying\": 5, \"methods\": []}},");
        sb.append("{\"kind\": \"struct\", \"detail\": {\"fields\": ["
                + "{\"name\": \"Bytes\", \"type\": 3, \"tag\": \"\", \"offset\": 0}]}},");
        sb.append("{\"kind\": \"named\", \"detail\": {\"name\": \"internal/abi.Type\", "
                + "\"pkgpath\": \"internal/abi\", \"underlying\": 7, \"methods\": []}},");
        sb.append("{\"kind\": \"struct\", \"detail\": {\"fields\": ["
                + "{\"name\": \"Size_\", \"type\": 0, \"tag\": \"\", \"offset\": 0},"
                + "{\"name\": \"PtrBytes\", \"type\": 0, \"tag\": \"\", \"offset\": 8},"
                + "{\"name\": \"Hash\", \"type\": 0, \"tag\": \"\", \"offset\": 16},"
                + "{\"name\": \"TFlag\", \"type\": 0, \"tag\": \"\", \"offset\": 24},"
                + "{\"name\": \"Align_\", \"type\": 0, \"tag\": \"\", \"offset\": 32},"
                + "{\"name\": \"FieldAlign_\", \"type\": 0, \"tag\": \"\", \"offset\": 40},"
                + "{\"name\": \"Kind_\", \"type\": 0, \"tag\": \"\", \"offset\": 48},"
                + "{\"name\": \"Equal\", \"type\": 2, \"tag\": \"\", \"offset\": 56},"
                + "{\"name\": \"GCData\", \"type\": 3, \"tag\": \"\", \"offset\": 64},"
                + "{\"name\": \"Str\", \"type\": 0, \"tag\": \"\", \"offset\": 72},"
                + "{\"name\": \"PtrToThis\", \"type\": 0, \"tag\": \"\", \"offset\": 80}]}},");
        sb.append("{\"kind\": \"named\", \"detail\": {\"name\": \"internal/abi.StructField\", "
                + "\"pkgpath\": \"internal/abi\", \"underlying\": 9, \"methods\": []}},");
        sb.append("{\"kind\": \"struct\", \"detail\": {\"fields\": ["
                + "{\"name\": \"Name\", \"type\": 4, \"tag\": \"\", \"offset\": 0},"
                + "{\"name\": \"Typ\", \"type\": 10, \"tag\": \"\", \"offset\": 8},"
                + "{\"name\": \"Offset\", \"type\": 0, \"tag\": \"\", \"offset\": 16}]}},");
        sb.append("{\"kind\": \"pointer\", \"detail\": {\"elem\": 6, \"methods\": []}},");
        sb.append("{\"kind\": \"slice\", \"detail\": {\"elem\": 8}},");
        sb.append("{\"kind\": \"named\", \"detail\": {\"name\": \"internal/abi.StructType\", "
                + "\"pkgpath\": \"internal/abi\", \"underlying\": 13, \"methods\": []}},");
        sb.append("{\"kind\": \"struct\", \"detail\": {\"fields\": ["
                + "{\"name\": \"Type\", \"type\": 6, \"tag\": \"\", \"offset\": 0},"
                + "{\"name\": \"PkgPath\", \"type\": 4, \"tag\": \"\", \"offset\": 8},"
                + "{\"name\": \"Fields\", \"type\": 11, \"tag\": \"\", \"offset\": 16}]}}");
        for (int i = 1; i <= structCount; i++) {
            // One field each, named f<i>: distinct by name, so each
            // struct is a distinct token, and the fixture doc stays
            // small even for large structCount. The mint storm below
            // needs many distinct first-mints per round: with only a
            // handful of tokens, every iteration after the first burst
            // is a memo hit and the put/get race window never recurs.
            sb.append(",{\"kind\": \"struct\", \"detail\": {\"fields\": ["
                    + "{\"name\": \"f").append(i)
                    .append("\", \"type\": 0, \"tag\": \"\", \"offset\": 0}]}}");
        }
        sb.append("]}");
        return sb.toString();
    }

    /** The tokens of the fixture's test structs (unnamed, first field f0). */
    private static List<Long> testStructToks(ReflectTypes.Table table, int structCount, int round) {
        List<Long> toks = new ArrayList<>();
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            // Unnamed test structs only: the internal/abi named types
            // are STRUCT kind too, and their underlying structs are
            // unnamed as well — tell them apart by first field name
            // (every test struct has the single field f<i>; the abi
            // underlying structs start with Size_/Bytes/Name/Type).
            if (ti != null && ti.kind == GoKind.STRUCT && !ti.isNamed()
                    && ti.fields != null && !ti.fields.isEmpty()
                    && ti.fields.get(0).name.startsWith("f")) {
                toks.add(ti.tok);
            }
        }
        assertTrue(toks.size() == structCount,
                "round " + round + ": expected " + structCount
                        + " distinct struct tokens, got " + toks.size());
        return toks;
    }

    /**
     * AbiTypes shell round-trip under concurrency: every minted shell
     * must resolve back to its token, and {@code abiPointerForTok}
     * must return the same canonical {@code GoPointer} on every call.
     * Concurrent first-mints resize the shell maps while other threads
     * read them. The round is repeated with a freshly installed table,
     * so every round re-runs the table-change {@code clear} and the
     * full re-mint: the unsynchronized reader races both the
     * {@code clear} and the {@code put}/resize, not just a single
     * warm-up pass.
     */
    public void testShellRoundTripUnderConcurrency() throws Exception {
        for (int round = 0; round < 10; round++) {
            roundShellRoundTrip(512, round);
        }
    }

    private void roundShellRoundTrip(int structCount, int round) throws Exception {
        ReflectTypes.install(parseDoc(fixtureDoc(structCount)), java.util.Map.of());
        ReflectTypes.Table table = ReflectTypes.table();
        List<Long> toks = testStructToks(table, structCount, round);

        // Shared claim counter: each iteration mints a fresh token
        // until the fixture is exhausted, so the put/resize storm
        // (not just the first burst) races the concurrent gets.
        java.util.concurrent.atomic.AtomicInteger next = new java.util.concurrent.atomic.AtomicInteger();
        AtomicReference<String> failure = new AtomicReference<>();
        CountDownLatch start = new CountDownLatch(1);
        ExecutorService pool = Executors.newFixedThreadPool(THREADS);
        try {
            for (int t = 0; t < THREADS; t++) {
                pool.submit(() -> {
                    try {
                        start.await();
                        for (int i = 0; i < 1000 && failure.get() == null; i++) {
                            long tok = toks.get(
                                    Math.floorMod(next.getAndIncrement(), toks.size()));
                            GoPointer ptr = AbiTypes.abiPointerForTok(tok);
                            if (!(ptr.referent() instanceof GoStruct shell)) {
                                failure.compareAndSet(null, "round " + round
                                        + ": abiPointerForTok(" + tok
                                        + ") referent is not a shell: " + ptr.referent());
                                return;
                            }
                            long back = AbiTypes.tokForShell(shell);
                            if (back != tok) {
                                failure.compareAndSet(null, "round " + round
                                        + ": tokForShell returned " + back
                                        + " for a shell minted for tok " + tok);
                                return;
                            }
                            GoPointer again = AbiTypes.abiPointerForTok(tok);
                            if (again != ptr) {
                                failure.compareAndSet(null, "round " + round
                                        + ": abiPointerForTok(" + tok
                                        + ") minted a second shell for one token");
                                return;
                            }
                        }
                    } catch (Throwable e) {
                        failure.compareAndSet(null, "round " + round + ": thread failed: " + e);
                    }
                });
            }
            start.countDown();
            pool.shutdown();
            assertTrue(pool.awaitTermination(300, TimeUnit.SECONDS),
                    "round " + round + ": shell round-trip workers did not finish in 300s");
        } finally {
            pool.shutdownNow();
        }
        assertTrue(failure.get() == null, failure.get());
    }

    // ------------------------------------------------------------------
    // unpackInterfaceView leg.
    //
    // An empty-interface view struct ({Type: *abi.Type, Data: uintptr})
    // whose Type word is a canonical shell pointer must unpack to the
    // interface of that dynamic type. The Data word is the null handle
    // (0), so the unpack exercises the SHELL_TOKS lookup without
    // involving ReflectRefs handles: a racy read that misses the shell
    // yields null (or, after a corrupt resize, a wrong descriptor).
    // Expected descriptors are resolved single-threaded before the
    // workers start (descriptorForTok populates the tokToDesc memo, so
    // the workers' unpacks only read the cached descriptor).
    // ------------------------------------------------------------------

    private static final GoStructDescriptor EFACE_DESC = GoStructDescriptor.layout(
            "test.Eface",
            List.of("Type", "Data"),
            List.of(GoKind.POINTER, GoKind.INT64));

    private static final List<AbiTypes.ViewField> EFACE_LAYOUT = List.of(
            new AbiTypes.ViewField("Type", 0, AbiTypes.ViewRole.ABI_TYPE),
            new AbiTypes.ViewField("Data", 8, AbiTypes.ViewRole.DATA));

    public void testUnpackInterfaceViewUnderConcurrency() throws Exception {
        for (int round = 0; round < 3; round++) {
            roundUnpackUnderConcurrency(512, round);
        }
    }

    private void roundUnpackUnderConcurrency(int structCount, int round) throws Exception {
        ReflectTypes.install(parseDoc(fixtureDoc(structCount)), java.util.Map.of());
        ReflectTypes.Table table = ReflectTypes.table();
        List<Long> toks = testStructToks(table, structCount, round);

        // Anchor views: the first 8 structs, minted and built
        // single-threaded so the workers race only the lookups.
        int anchors = 8;
        List<GoStruct> views = new ArrayList<>(anchors);
        List<GoTypeDescriptor> expected = new ArrayList<>(anchors);
        for (int i = 0; i < anchors; i++) {
            long tok = toks.get(i);
            GoPointer ptr = AbiTypes.abiPointerForTok(tok);
            GoStruct view = GoStruct.zero(EFACE_DESC);
            view.setObject(0, ptr);
            view.setLong(1, 0);
            views.add(view);
            expected.add(ReflectTypes.descriptorForTok(tok));
        }

        // Shared claim counter over the unminted remainder: each mint
        // leg iteration first-mints a fresh token (put/resize) while
        // the unpack threads read SHELL_TOKS.
        java.util.concurrent.atomic.AtomicInteger next = new java.util.concurrent.atomic.AtomicInteger();
        AtomicReference<String> failure = new AtomicReference<>();
        CountDownLatch start = new CountDownLatch(1);
        ExecutorService pool = Executors.newFixedThreadPool(THREADS);
        try {
            for (int t = 0; t < THREADS; t++) {
                int threadIdx = t;
                pool.submit(() -> {
                    try {
                        start.await();
                        for (int i = 0; i < 1000 && failure.get() == null; i++) {
                            if (threadIdx % 2 == 0) {
                                // Unpack leg: the anchor view must
                                // round-trip to its dynamic type.
                                int a = (i + threadIdx) % anchors;
                                GoInterface gi = AbiTypes.unpackInterfaceView(
                                        views.get(a), EFACE_LAYOUT);
                                if (gi == null) {
                                    failure.compareAndSet(null, "round " + round
                                            + ": unpackInterfaceView returned null for the "
                                            + "minted shell of tok " + toks.get(a));
                                    return;
                                }
                                if (gi.descriptor() != expected.get(a)) {
                                    failure.compareAndSet(null, "round " + round
                                            + ": unpackInterfaceView resolved the wrong "
                                            + "descriptor for tok " + toks.get(a)
                                            + ": " + gi.descriptor());
                                    return;
                                }
                                if (gi.data() != null) {
                                    failure.compareAndSet(null, "round " + round
                                            + ": unpackInterfaceView data should be null "
                                            + "for a zero data handle, got " + gi.data());
                                    return;
                                }
                            } else {
                                // Mint leg: first-mints of the remaining
                                // structs put/resize SHELL_TOKS while the
                                // unpack threads read it.
                                long tok = toks.get(anchors + Math.floorMod(
                                        next.getAndIncrement(), toks.size() - anchors));
                                GoPointer ptr = AbiTypes.abiPointerForTok(tok);
                                if (!(ptr.referent() instanceof GoStruct shell)) {
                                    failure.compareAndSet(null, "round " + round
                                            + ": abiPointerForTok(" + tok
                                            + ") referent is not a shell");
                                    return;
                                }
                                long back = AbiTypes.tokForShell(shell);
                                if (back != tok) {
                                    failure.compareAndSet(null, "round " + round
                                            + ": tokForShell returned " + back
                                            + " for a shell minted for tok " + tok);
                                    return;
                                }
                            }
                        }
                    } catch (Throwable e) {
                        failure.compareAndSet(null, "round " + round + ": thread failed: " + e);
                    }
                });
            }
            start.countDown();
            pool.shutdown();
            assertTrue(pool.awaitTermination(300, TimeUnit.SECONDS),
                    "round " + round + ": unpack workers did not finish in 300s");
        } finally {
            pool.shutdownNow();
        }
        assertTrue(failure.get() == null, failure.get());
    }
}
