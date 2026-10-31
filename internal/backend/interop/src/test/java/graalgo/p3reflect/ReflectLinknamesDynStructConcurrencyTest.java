package graalgo.p3reflect;

import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

/**
 * R-01 (independent codebase review 2026-10-31-19) regression tests:
 * dynamic-struct synthesis ({@code ReflectLinknames.synthesizeDynamicStruct},
 * reached from ordinary guest reflect calls via {@code tokForTypeArg}) must
 * be safe under concurrent guest goroutines, which run on separate virtual
 * threads. Two goroutines concurrently constructing/using the same
 * guest-built {@code StructOf} type must receive the same token (gc's
 * StructOf identity); distinct structures must receive distinct tokens;
 * and no {@code byTok} entry may be lost to a concurrent append.
 *
 * <p>Fixtures are hand-built guest values, not wire types: a header
 * ({@code internal/abi.Type}, Kind_ 25) whose field-0 prefix parent is an
 * {@code internal/abi.StructType} carrying the Fields slice — exactly the
 * fix-redmaster-r43 shape {@code synthesizeDynamicToken} walks. Field
 * types are the installed table's {@code int} token, passed as a
 * {@link Number} so no nested dynamic synthesis is involved.
 *
 * <p>Negative control: against the pre-fix code (plain
 * {@code IdentityHashMap} memo, plain {@code ArrayList} table, no lock)
 * the same-struct hammer below fails — threads that both miss the memo
 * mint distinct tokens for one structure, or the table grows by more
 * than one entry for it (a lost/orphaned entry). The multi-field shape
 * (16 fields) widens the memo-miss-to-memo-put window so the race is
 * observable without timing luck; the distinct-struct hammer additionally
 * catches lost {@code byTok} appends.
 */
public final class ReflectLinknamesDynStructConcurrencyTest {

    /** Threads racing each hammer round. */
    private static final int THREADS = 16;
    /** Fields per hammer struct: widens the synthesis window. */
    private static final int FIELDS = 16;
    /** Hammer rounds per test (fresh struct object per round). */
    private static final int ROUNDS = 200;

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

    /** Installs a minimal table (one basic {@code int}) and returns its token. */
    private static long installIntTable() {
        String doc = "{\"types\": [{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"int\", \"class\": \"int\", \"untyped\": false}}]}";
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            // Basic types carry name "" and the wire name in basicName.
            if (ti != null && "int".equals(ti.basicName)) {
                return ti.tok;
            }
        }
        throw new AssertionError("installed table has no int TypeInfo");
    }

    private static GoStructDescriptor headerDesc() {
        return GoStructDescriptor.layout("internal/abi.Type",
                List.of("Kind_", "Size_", "Align_"),
                List.of(GoKind.INT64, GoKind.INT64, GoKind.INT64));
    }

    private static GoStructDescriptor structTypeDesc() {
        return GoStructDescriptor.layout("internal/abi.StructType",
                List.of("Type", "Fields", "PkgPath"),
                List.of(GoKind.STRUCT, GoKind.SLICE, GoKind.POINTER));
    }

    private static GoStructDescriptor structFieldDesc() {
        return GoStructDescriptor.layout("internal/abi.StructField",
                List.of("Name", "Typ", "Offset"),
                List.of(GoKind.STRUCT, GoKind.POINTER, GoKind.INT64));
    }

    private static GoStructDescriptor nameDesc() {
        return GoStructDescriptor.layout("internal/abi.Name",
                List.of("Bytes"), List.of(GoKind.POINTER));
    }

    /** abi.Name bytes for an exported field name (flags=1, varint len, name). */
    private static GoStruct nameStruct(String name) {
        byte[] raw = name.getBytes(java.nio.charset.StandardCharsets.UTF_8);
        byte[] bytes = new byte[2 + raw.length];
        bytes[0] = 1; // exported, no tag, not embedded
        bytes[1] = (byte) raw.length;
        System.arraycopy(raw, 0, bytes, 2, raw.length);
        return new GoStruct(nameDesc(), new long[0],
                new Object[]{new GoPointer(bytes, 0)});
    }

    /**
     * One guest-built dynamic struct type: a fresh header plus the
     * StructType that owns it as field 0 (so {@code header.prefixParent()}
     * is the StructType, the r43 link). Returns the header; the caller
     * passes a {@link GoPointer} to it into {@code tokForTypeArg}.
     */
    private static GoStruct newDynamicStruct(long intTok, int fieldCount) {
        GoStruct header = new GoStruct(headerDesc(),
                new long[]{25, 0, 1}, new Object[0]);
        Object[] fields = new Object[fieldCount];
        for (int i = 0; i < fieldCount; i++) {
            fields[i] = new GoStruct(structFieldDesc(),
                    new long[]{i * 8L},
                    new Object[]{nameStruct("F" + i), Long.valueOf(intTok)});
        }
        GoSlice slice = new GoSlice(fields, 0, fieldCount, fieldCount);
        // Constructing the owner registers header.prefixParent = owner.
        new GoStruct(structTypeDesc(), new long[0],
                new Object[]{header, slice, null});
        return header;
    }

    private static long synthesize(GoStruct header) {
        return ReflectLinknames.tokForTypeArg(new GoPointer(header), "test: dynamic struct");
    }

    /**
     * Fixture sanity (single-threaded): synthesis yields one unnamed
     * STRUCT token with the expected fields, and a second call for the
     * same StructType object returns the identical token (memo hit).
     */
    public void testDynamicStructSynthesisFixture() {
        long intTok = installIntTable();
        GoStruct header = newDynamicStruct(intTok, FIELDS);
        long tok = synthesize(header);
        ReflectTypes.TypeInfo info = ReflectTypes.table().info(tok);
        assertTrue(info.kind == GoKind.STRUCT, "synthesized kind should be STRUCT, got " + info.kind);
        assertTrue(info.name.isEmpty(), "synthesized struct must be unnamed, got " + info.name);
        assertTrue(info.fields.size() == FIELDS,
                "synthesized struct should have " + FIELDS + " fields, got " + info.fields.size());
        assertTrue(synthesize(header) == tok, "second synthesis of the same object must return the memo token");
    }

    /**
     * Same-StructType hammer: THREADS threads synthesizing the same fresh
     * dynamic struct type per round must all receive the same token, and
     * the table must grow by exactly one entry per round (no duplicate
     * mint, no lost/orphaned append).
     */
    public void testConcurrentSameDynamicStructYieldsOneToken() throws Exception {
        long intTok = installIntTable();
        ExecutorService pool = Executors.newFixedThreadPool(THREADS);
        try {
            for (int round = 0; round < ROUNDS; round++) {
                GoStruct header = newDynamicStruct(intTok, FIELDS);
                int before = ReflectTypes.table().byTok.size();
                CountDownLatch start = new CountDownLatch(1);
                CountDownLatch done = new CountDownLatch(THREADS);
                long[] toks = new long[THREADS];
                AtomicReference<Throwable> failure = new AtomicReference<>();
                for (int t = 0; t < THREADS; t++) {
                    int idx = t;
                    pool.execute(() -> {
                        try {
                            start.await();
                            toks[idx] = synthesize(header);
                        } catch (Throwable e) {
                            failure.set(e);
                        } finally {
                            done.countDown();
                        }
                    });
                }
                start.countDown();
                assertTrue(done.await(120, TimeUnit.SECONDS),
                        "same-struct hammer round " + round + " timed out (possible memo corruption hang)");
                assertTrue(failure.get() == null,
                        "same-struct hammer round " + round + " threw: " + failure.get());
                for (int t = 1; t < THREADS; t++) {
                    assertTrue(toks[t] == toks[0],
                            "same-struct hammer round " + round + ": thread " + t + " got token "
                                    + toks[t] + " but thread 0 got " + toks[0]);
                }
                int after = ReflectTypes.table().byTok.size();
                assertTrue(after == before + 1,
                        "same-struct hammer round " + round + ": table grew by " + (after - before)
                                + ", expected exactly 1 (duplicate mint or lost append)");
                assertTrue(ReflectTypes.table().byTok.get((int) toks[0]) != null,
                        "same-struct hammer round " + round + ": token " + toks[0] + " has no byTok entry");
            }
        } finally {
            pool.shutdownNow();
        }
    }

    /**
     * Distinct-struct hammer: THREADS threads each synthesizing their own
     * fresh dynamic struct type per round must receive THREADS distinct
     * tokens, and the table must grow by exactly THREADS entries with no
     * null hole in the new range.
     */
    public void testConcurrentDistinctDynamicStructsYieldDistinctTokens() throws Exception {
        long intTok = installIntTable();
        ExecutorService pool = Executors.newFixedThreadPool(THREADS);
        try {
            for (int round = 0; round < ROUNDS; round++) {
                GoStruct[] headers = new GoStruct[THREADS];
                for (int t = 0; t < THREADS; t++) {
                    headers[t] = newDynamicStruct(intTok, FIELDS);
                }
                int before = ReflectTypes.table().byTok.size();
                CountDownLatch start = new CountDownLatch(1);
                CountDownLatch done = new CountDownLatch(THREADS);
                long[] toks = new long[THREADS];
                AtomicReference<Throwable> failure = new AtomicReference<>();
                for (int t = 0; t < THREADS; t++) {
                    int idx = t;
                    pool.execute(() -> {
                        try {
                            start.await();
                            toks[idx] = synthesize(headers[idx]);
                        } catch (Throwable e) {
                            failure.set(e);
                        } finally {
                            done.countDown();
                        }
                    });
                }
                start.countDown();
                assertTrue(done.await(120, TimeUnit.SECONDS),
                        "distinct-struct hammer round " + round + " timed out (possible memo corruption hang)");
                assertTrue(failure.get() == null,
                        "distinct-struct hammer round " + round + " threw: " + failure.get());
                java.util.Set<Long> seen = new java.util.HashSet<>();
                for (int t = 0; t < THREADS; t++) {
                    assertTrue(seen.add(toks[t]),
                            "distinct-struct hammer round " + round + ": token " + toks[t] + " minted twice");
                    assertTrue(ReflectTypes.table().info(toks[t]).kind == GoKind.STRUCT,
                            "distinct-struct hammer round " + round + ": token " + toks[t] + " is not STRUCT");
                }
                int after = ReflectTypes.table().byTok.size();
                assertTrue(after == before + THREADS,
                        "distinct-struct hammer round " + round + ": table grew by " + (after - before)
                                + ", expected " + THREADS + " (lost append)");
                for (int i = before; i < after; i++) {
                    assertTrue(ReflectTypes.table().byTok.get(i) != null,
                            "distinct-struct hammer round " + round + ": byTok hole at index " + i);
                }
            }
        } finally {
            pool.shutdownNow();
        }
    }
}
