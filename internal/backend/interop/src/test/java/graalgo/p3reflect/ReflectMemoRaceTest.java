package graalgo.p3reflect;

import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

/**
 * R-1108-90: concurrent-reflect torture tests for the shared memo maps
 * in the reflect/ABI layer.
 *
 * <p>Two races, both on plain (unsynchronized) maps mutated under one
 * lock and read without it:
 *
 * <ul>
 * <li>{@code AbiTypes} — {@code SHELL_TOKS} (an {@code IdentityHashMap})
 * is cleared and filled by the class-locked {@code abiPointerForTok},
 * but {@code tokForShell} and the {@code unpackInterfaceView} lookup
 * read it with no lock. A {@code get} racing a {@code put}/resize (or
 * the table-change {@code clear}) can return a stale null, so a minted
 * shell reads as "unknown type".
 * <li>{@code ReflectLinknames} — {@code DYN_STRUCT_TOKS} (also an
 * {@code IdentityHashMap}) is checked, cleared and filled by the
 * unsynchronized {@code synthesizeDynamicStruct}. Two threads
 * synthesizing the same structural type concurrently can both miss
 * the memo and mint two distinct tokens for it, breaking type
 * identity ({@code ==}, map keys, type switches).
 * </ul>
 *
 * <p>Falsifiability: on the pre-fix code the dynamic-struct test mints
 * duplicate tokens (the memo-miss window spans the whole field
 * decoding and {@code TypeInfo} allocation), and the shell round-trip
 * test observes a {@code tokForShell} miss for a minted shell. On the
 * fixed code both tests pass deterministically: readers take the same
 * lock as writers, and synthesis is serialized so one structural type
 * mints exactly one token.
 *
 * <p>Registered in {@code graalgo.iface.IfaceTestRunner}.
 */
public final class ReflectMemoRaceTest {

    private static final int THREADS = 8;

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static graalgo.frontend.Json.Object parseDoc(String text) {
        graalgo.frontend.Json.Value v = graalgo.frontend.Json.parse(text);
        if (!(v instanceof graalgo.frontend.Json.Object doc)) {
            throw new AssertionError("test doc must be a JSON object");
        }
        return doc;
    }

    /**
     * AbiTypes shell round-trip under concurrency: every minted shell
     * must resolve back to its token, and {@code abiPointerForTok}
     * must return the same canonical {@code GoPointer} on every call.
     * The table holds {@code structCount} structurally distinct struct
     * types (varying field counts), so concurrent first-mints resize
     * the shell maps while other threads read them. The round is
     * repeated with a freshly installed table, so every round re-runs
     * the table-change {@code clear} and the full re-mint: the
     * unsynchronized reader races both the {@code clear} and the
     * {@code put}/resize, not just a single warm-up pass.
     */
    public void testAbiPointerShellRoundTripUnderConcurrency() throws Exception {
        for (int round = 0; round < 5; round++) {
            roundShellRoundTrip(64, round);
        }
    }

    private void roundShellRoundTrip(int structCount, int round) throws Exception {
        // The table must carry the internal/abi named types:
        // abiPointerForTok builds each shell from the wire descriptor
        // of internal/abi.Type / StructType / StructField / Name
        // (structDescriptorFor), and fillAbiStruct touches every
        // field listed here. Wire ids: 0 int, 1 byte, 2 *int, 3 *byte,
        // 4/5 Name, 6/7 Type, 8/9 StructField, 10 *Type, 11 []StructField,
        // 12/13 StructType; the test structs follow at 14...
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
            sb.append(",{\"kind\": \"struct\", \"detail\": {\"fields\": [");
            for (int j = 0; j < i; j++) {
                if (j > 0) {
                    sb.append(',');
                }
                sb.append("{\"name\": \"f").append(j)
                        .append("\", \"type\": 0, \"tag\": \"\", \"offset\": ")
                        .append(8L * j).append('}');
            }
            sb.append("]}}");
        }
        sb.append("]}");
        ReflectTypes.install(parseDoc(sb.toString()), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();

        List<Long> toks = new ArrayList<>();
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            // Unnamed test structs only: the internal/abi named types
            // are STRUCT kind too, and their underlying structs are
            // unnamed as well — tell them apart by first field name
            // (every test struct starts with field f0; the abi
            // underlying structs start with Size_/Bytes/Name/Type).
            if (ti != null && ti.kind == GoKind.STRUCT && !ti.isNamed()
                    && ti.fields != null && !ti.fields.isEmpty()
                    && "f0".equals(ti.fields.get(0).name)) {
                toks.add(ti.tok);
            }
        }
        assertTrue(toks.size() == structCount,
                "round " + round + ": expected " + structCount
                        + " distinct struct tokens, got " + toks.size());

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
                            long tok = toks.get((i + threadIdx) % toks.size());
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
    // Dynamic-struct fixture (fix-redmaster-r43 shape).
    //
    // header ("internal/abi.Type", longs Str/Kind_/Size_/Align_) is the
    // field-0 child of structType ("internal/abi.StructType"), so it is
    // the header's prefix parent; structType carries one StructField
    // whose Typ is the raw int token and whose Name/PkgPath carry no
    // bytes (empty name, exported). tokForTypeArg on a GoPointer to the
    // header therefore reaches synthesizeDynamicToken, which mints the
    // struct token through synthesizeDynamicStruct.
    // ------------------------------------------------------------------

    private record DynFixture(GoStruct header) {}

    private static int ordinal(GoStructDescriptor desc, String name) {
        for (int i = 0; i < desc.fields().size(); i++) {
            if (name.equals(desc.field(i).name())) {
                return i;
            }
        }
        throw new AssertionError("no field " + name + " on " + desc.typeName());
    }

    private static DynFixture buildDynFixture(long intTok) {
        GoStructDescriptor nameDesc = GoStructDescriptor.layout("internal/abi.Name",
                List.of("Bytes"), List.of(GoKind.POINTER));
        GoStructDescriptor headerDesc = GoStructDescriptor.layout("internal/abi.Type",
                List.of("Str", "Kind_", "Size_", "Align_"),
                List.of(GoKind.INT64, GoKind.INT64, GoKind.INT64, GoKind.INT64));
        GoStruct header = GoStruct.zero(headerDesc);
        header.setLong(ordinal(headerDesc, "Str"), 0);
        header.setLong(ordinal(headerDesc, "Kind_"), 25);
        header.setLong(ordinal(headerDesc, "Size_"), 8);
        header.setLong(ordinal(headerDesc, "Align_"), 8);

        GoStructDescriptor fieldDesc = GoStructDescriptor.layout("internal/abi.StructField",
                List.of("Name", "Typ", "Offset"),
                List.of(GoKind.STRUCT, GoKind.POINTER, GoKind.INT64),
                List.of(-1, -1, -1),
                Arrays.asList(null, null, null),
                Arrays.asList(null, null, null),
                Arrays.asList(nameDesc, null, null));
        GoStruct fieldStruct = GoStruct.zero(fieldDesc);
        fieldStruct.setObject(ordinal(fieldDesc, "Name"), GoStruct.zero(nameDesc));
        fieldStruct.setObject(ordinal(fieldDesc, "Typ"), Long.valueOf(intTok));
        fieldStruct.setLong(ordinal(fieldDesc, "Offset"), 0);

        GoStructDescriptor structTypeDesc = GoStructDescriptor.layout("internal/abi.StructType",
                List.of("Head", "Fields", "PkgPath"),
                List.of(GoKind.STRUCT, GoKind.SLICE, GoKind.STRUCT),
                List.of(-1, -1, -1),
                Arrays.asList(null, null, null),
                Arrays.asList(null, null, null),
                Arrays.asList(headerDesc, null, nameDesc));
        GoStruct structType = GoStruct.zero(structTypeDesc);
        structType.setObject(ordinal(structTypeDesc, "Head"), header);
        structType.setObject(ordinal(structTypeDesc, "Fields"),
                new GoSlice(new Object[]{fieldStruct}, 0, 1, 1));
        structType.setObject(ordinal(structTypeDesc, "PkgPath"), GoStruct.zero(nameDesc));
        assertTrue(header.prefixParent() == structType,
                "fixture header must have the structType as prefix parent");
        return new DynFixture(header);
    }

    /**
     * Dynamic-struct synthesis under concurrency: {@code THREADS}
     * threads synthesizing the same guest-constructed struct type at
     * once (start-gated, fresh fixture per round so every round races
     * a fresh memo miss) must all observe the same single token, and
     * the table must grow by exactly one token per round.
     */
    public void testDynamicStructSynthesisMintsOneToken() throws Exception {
        String doc = "{\"types\": [{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"int\", \"class\": \"int\", \"untyped\": false}}]}";
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();
        long intTok = table.tokForKind(GoKind.INT64);

        int rounds = 20;
        for (int round = 0; round < rounds; round++) {
            DynFixture fixture = buildDynFixture(intTok);
            int sizeBefore = table.byTok.size();
            Set<Long> seen = ConcurrentHashMap.newKeySet();
            AtomicReference<String> failure = new AtomicReference<>();
            CountDownLatch start = new CountDownLatch(1);
            ExecutorService pool = Executors.newFixedThreadPool(THREADS);
            try {
                for (int t = 0; t < THREADS; t++) {
                    pool.submit(() -> {
                        try {
                            start.await();
                            for (int i = 0; i < 200 && failure.get() == null; i++) {
                                long tok = ReflectLinknames.tokForTypeArg(
                                        new GoPointer(fixture.header()), "test");
                                seen.add(tok);
                            }
                        } catch (Throwable e) {
                            failure.compareAndSet(null, "thread failed: " + e);
                        }
                    });
                }
                start.countDown();
                pool.shutdown();
                assertTrue(pool.awaitTermination(120, TimeUnit.SECONDS),
                        "round " + round + ": workers did not finish in 120s");
            } finally {
                pool.shutdownNow();
            }
            assertTrue(failure.get() == null, "round " + round + ": " + failure.get());
            assertTrue(seen.size() == 1,
                    "round " + round + ": concurrent synthesis minted " + seen.size()
                            + " distinct tokens for one structural type: " + seen);
            assertTrue(table.byTok.size() == sizeBefore + 1,
                    "round " + round + ": table grew by "
                            + (table.byTok.size() - sizeBefore) + " tokens, expected 1");
        }
    }

    /** Sanity: the fixture alone (single thread) synthesizes one token. */
    public void testDynamicStructFixtureSynthesizesOnce() {
        String doc = "{\"types\": [{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"int\", \"class\": \"int\", \"untyped\": false}}]}";
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();
        long intTok = table.tokForKind(GoKind.INT64);
        DynFixture fixture = buildDynFixture(intTok);
        int sizeBefore = table.byTok.size();
        long tok = ReflectLinknames.tokForTypeArg(new GoPointer(fixture.header()), "test");
        assertTrue(tok != 0, "fixture synthesis returned token 0");
        assertTrue(table.byTok.size() == sizeBefore + 1,
                "single synthesis grew the table by "
                        + (table.byTok.size() - sizeBefore) + ", expected 1");
        long again = ReflectLinknames.tokForTypeArg(new GoPointer(fixture.header()), "test");
        assertTrue(again == tok, "memoized synthesis returned " + again + ", expected " + tok);
        assertTrue(table.byTok.size() == sizeBefore + 1,
                "memoized synthesis must not mint a second token");
        ReflectTypes.TypeInfo info = table.info(tok);
        assertTrue(info.kind == GoKind.STRUCT, "synthesized kind should be STRUCT");
        assertTrue(info.fields.size() == 1, "synthesized struct should have 1 field");
    }
}
