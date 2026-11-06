package graalgo.p3reflect;

import graalgo.values.GoKind;

import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ConcurrentLinkedQueue;
import java.util.concurrent.CyclicBarrier;

/**
 * Concurrent regression tests for NEW-P1-2 (REVIEW-2026-11-06-01):
 * the direct {@code ReflectTypes} token-mint family
 * ({@code ptrTo}/{@code arrayOf}/{@code sliceOf}/{@code mapOf}/
 * {@code chanOf}/{@code funcOf}/{@code structOf}) was unsynchronized
 * check-then-mint on plain {@code ArrayList}/{@code HashMap} table
 * state — two threads minting different types could both read the
 * same {@code byTok.size()} and return the same token, so one
 * caller's type silently resolved to the other's {@code TypeInfo};
 * same-type concurrent mints could mint two tokens for one type,
 * breaking canonicalization. The fix serializes the whole mint path
 * (and the readers) on the Table instance monitor.
 *
 * <p>Race tests are probabilistic by nature: on the unfixed code each
 * test fails only when two threads overlap in the mint window, which
 * 16 barrier-released threads x many mints x many fresh rounds makes
 * overwhelmingly likely (the dynamic-struct precedent failed in round
 * 0 with the same shape). On the fixed code all three are
 * deterministic: the monitor serializes the accesses, so every round
 * mints exactly the expected tokens and every reader sees exactly the
 * minted type. Falsifiability: reverting the synchronization re-opens
 * the race the tests exercise (verified red before the fix landed:
 * distinct-array and distinct-struct fail in round 0–3 with a token
 * resolving to another thread's type). The same-type canonicalization
 * facet did not manifest in the two red runs (its check-then-mint
 * window is narrower than the barrier wakeup stagger); it is kept as
 * the guard for that property, with the distinct-type tests as the
 * sensitive detectors.
 *
 * <p>Registered in {@code graalgo.iface.IfaceTestRunner}.
 */
public final class ReflectMintRaceTest {

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
     * Installs a table carrying the basic {@code int} type (plus the
     * wire named types the abi shell machinery expects). Copied from
     * the dynamic-synthesis race test's fixture: the shape is proven
     * to install cleanly.
     */
    private static long installIntTable() {
        String doc = "{\"types\": ["
                + "{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"int\", \"class\": \"int\", \"untyped\": false}},"
                + "{\"kind\": \"interface\", \"detail\": {\"methods\": []}},"
                + "{\"kind\": \"struct\", \"detail\": {\"fields\": ["
                + "{\"name\": \"Size_\", \"type\": 0, \"tag\": \"\", \"offset\": 0},"
                + "{\"name\": \"PtrBytes\", \"type\": 0, \"tag\": \"\", \"offset\": 8},"
                + "{\"name\": \"Hash\", \"type\": 0, \"tag\": \"\", \"offset\": 16},"
                + "{\"name\": \"TFlag\", \"type\": 0, \"tag\": \"\", \"offset\": 24},"
                + "{\"name\": \"Align_\", \"type\": 0, \"tag\": \"\", \"offset\": 32},"
                + "{\"name\": \"FieldAlign_\", \"type\": 0, \"tag\": \"\", \"offset\": 40},"
                + "{\"name\": \"Kind_\", \"type\": 0, \"tag\": \"\", \"offset\": 48},"
                + "{\"name\": \"Equal\", \"type\": 1, \"tag\": \"\", \"offset\": 56},"
                + "{\"name\": \"GCData\", \"type\": 1, \"tag\": \"\", \"offset\": 64},"
                + "{\"name\": \"Str\", \"type\": 0, \"tag\": \"\", \"offset\": 72},"
                + "{\"name\": \"PtrToThis\", \"type\": 0, \"tag\": \"\", \"offset\": 80}]}},"
                + "{\"kind\": \"named\", \"detail\": "
                + "{\"name\": \"internal/abi.Type\", \"pkgpath\": \"internal/abi\", "
                + "\"underlying\": 2, \"methods\": []}},"
                + "{\"kind\": \"struct\", \"detail\": {\"fields\": ["
                + "{\"name\": \"Type\", \"type\": 3, \"tag\": \"\", \"offset\": 0},"
                + "{\"name\": \"Elem\", \"type\": 1, \"tag\": \"\", \"offset\": 8},"
                + "{\"name\": \"Len\", \"type\": 0, \"tag\": \"\", \"offset\": 16}]}},"
                + "{\"kind\": \"named\", \"detail\": "
                + "{\"name\": \"internal/abi.ArrayType\", \"pkgpath\": \"internal/abi\", "
                + "\"underlying\": 4, \"methods\": []}}]}";
        ReflectTypes.install(parseDoc(doc), Map.of());
        return ReflectTypes.table().tokForKind(GoKind.INT64);
    }

    /**
     * NEW-P1-2, distinct types: 16 threads minting distinct array types
     * at once must get distinct tokens, each resolving (via
     * {@code info()}) to the type it minted. Without the lock, two
     * threads that both read the same {@code byTok.size()} return the
     * same token — the duplicate shows up as a set-size shortfall and
     * as an {@code info()} round-trip with the wrong array length.
     * Four reader threads hammer {@code info()} on pre-minted tokens
     * throughout, so a torn read (stale size / null placeholder slot)
     * fails loudly instead of silently.
     */
    public void testConcurrentDistinctArrayOfReturnsDistinctTokens() throws Exception {
        long intTok = installIntTable();
        final int writers = 16;
        final int perWriter = 32;
        final int readers = 4;
        final int rounds = 30;
        for (int round = 0; round < rounds; round++) {
            if (round > 0) {
                intTok = installIntTable();
            }
            final long elemTok = intTok;
            // Pre-minted tokens for the readers (single-threaded, so the
            // concurrent phase exercises only the mint/read discipline).
            List<Long> readerToks = new ArrayList<>();
            for (int n = 1000; n < 1004; n++) {
                readerToks.add(ReflectTypes.arrayOf(elemTok, n));
            }
            ConcurrentLinkedQueue<String> failures = new ConcurrentLinkedQueue<>();
            ConcurrentLinkedQueue<long[]> minted = new ConcurrentLinkedQueue<>();
            CyclicBarrier start = new CyclicBarrier(writers + readers);
            List<Thread> threads = new ArrayList<>();
            for (int w = 0; w < writers; w++) {
                int wi = w;
                threads.add(new Thread(() -> {
                    try {
                        start.await();
                        for (int k = 0; k < perWriter; k++) {
                            int n = wi * perWriter + k + 1;
                            long tok = ReflectTypes.arrayOf(elemTok, n);
                            minted.add(new long[]{tok, n});
                        }
                    } catch (Exception e) {
                        failures.add("writer failed: " + e);
                    }
                }));
            }
            for (int r = 0; r < readers; r++) {
                threads.add(new Thread(() -> {
                    try {
                        start.await();
                        for (int i = 0; i < 20_000; i++) {
                            long tok = readerToks.get(i % readerToks.size());
                            ReflectTypes.TypeInfo info = ReflectTypes.table().info(tok);
                            if (info == null || info.kind != GoKind.ARRAY) {
                                failures.add("reader saw wrong info for token " + tok
                                        + ": " + info);
                                return;
                            }
                        }
                    } catch (Exception e) {
                        failures.add("reader failed: " + e);
                    }
                }));
            }
            for (Thread t : threads) {
                t.start();
            }
            for (Thread t : threads) {
                t.join();
            }
            assertTrue(failures.isEmpty(),
                    "round " + round + " concurrent mint/read failed: " + failures);
            int expected = writers * perWriter;
            assertTrue(minted.size() == expected,
                    "round " + round + ": minted " + minted.size() + " expected " + expected);
            Set<Long> distinct = new HashSet<>();
            for (long[] m : minted) {
                distinct.add(m[0]);
                ReflectTypes.TypeInfo info = ReflectTypes.table().info(m[0]);
                assertTrue(info != null && info.kind == GoKind.ARRAY
                                && info.arrayLen == (int) m[1],
                        "round " + round + ": token " + m[0] + " round-trips to "
                                + (info == null ? "null" : info.kind + "[" + info.arrayLen + "]")
                                + " instead of array[" + m[1] + "]");
            }
            assertTrue(distinct.size() == expected,
                    "round " + round + ": only " + distinct.size()
                            + " distinct tokens for " + expected + " distinct types"
                            + " (two threads minted the same token)");
        }
    }

    /**
     * NEW-P1-2, distinct structs: 16 threads minting distinct
     * single-field structs at once must get distinct tokens, each
     * resolving to the struct it minted (checked by field name).
     */
    public void testConcurrentDistinctStructOfReturnsDistinctTokens() throws Exception {
        long intTok = installIntTable();
        final int writers = 16;
        final int perWriter = 8;
        final int rounds = 20;
        for (int round = 0; round < rounds; round++) {
            if (round > 0) {
                intTok = installIntTable();
            }
            final long elemTok = intTok;
            ConcurrentLinkedQueue<String> failures = new ConcurrentLinkedQueue<>();
            ConcurrentLinkedQueue<Object[]> minted = new ConcurrentLinkedQueue<>();
            CyclicBarrier start = new CyclicBarrier(writers);
            List<Thread> threads = new ArrayList<>();
            for (int w = 0; w < writers; w++) {
                int wi = w;
                threads.add(new Thread(() -> {
                    try {
                        start.await();
                        for (int k = 0; k < perWriter; k++) {
                            String field = "f" + wi + "_" + k;
                            long tok = ReflectTypes.structOf(
                                    new String[]{field},
                                    new long[]{elemTok},
                                    new String[]{""},
                                    new String[]{""},
                                    new boolean[]{false});
                            minted.add(new Object[]{tok, field});
                        }
                    } catch (Exception e) {
                        failures.add("writer failed: " + e);
                    }
                }));
            }
            for (Thread t : threads) {
                t.start();
            }
            for (Thread t : threads) {
                t.join();
            }
            assertTrue(failures.isEmpty(),
                    "round " + round + " concurrent structOf failed: " + failures);
            int expected = writers * perWriter;
            assertTrue(minted.size() == expected,
                    "round " + round + ": minted " + minted.size() + " expected " + expected);
            Set<Long> distinct = new HashSet<>();
            for (Object[] m : minted) {
                long tok = (Long) m[0];
                distinct.add(tok);
                ReflectTypes.TypeInfo info = ReflectTypes.table().info(tok);
                assertTrue(info != null && info.kind == GoKind.STRUCT
                                && info.fields.size() == 1
                                && m[1].equals(info.fields.get(0).name),
                        "round " + round + ": token " + tok + " round-trips to "
                                + (info == null ? "null"
                                        : info.kind + " fields=" + info.fields)
                                + " instead of struct{" + m[1] + " int}");
            }
            assertTrue(distinct.size() == expected,
                    "round " + round + ": only " + distinct.size()
                            + " distinct tokens for " + expected + " distinct structs"
                            + " (two threads minted the same token)");
        }
    }

    /**
     * NEW-P1-2, canonicalization: 16 threads minting the SAME pointer
     * type at once must all get the same token, and the table must grow
     * by exactly one entry. Without the lock, two threads that both
     * miss the memo mint two tokens for one type, so
     * {@code PtrTo(T) == PtrTo(T)} can fail.
     */
    public void testConcurrentSamePtrToReturnsSingleToken() throws Exception {
        long intTok = installIntTable();
        final int threads = 16;
        final int rounds = 200;
        for (int round = 0; round < rounds; round++) {
            if (round > 0) {
                intTok = installIntTable();
            }
            final long elemTok = intTok;
            ReflectTypes.Table table = ReflectTypes.table();
            int before = table.byTok.size();
            ConcurrentLinkedQueue<String> failures = new ConcurrentLinkedQueue<>();
            ConcurrentLinkedQueue<Long> minted = new ConcurrentLinkedQueue<>();
            CyclicBarrier start = new CyclicBarrier(threads);
            List<Thread> ts = new ArrayList<>();
            for (int i = 0; i < threads; i++) {
                ts.add(new Thread(() -> {
                    try {
                        start.await();
                        minted.add(ReflectTypes.ptrTo(elemTok));
                    } catch (Exception e) {
                        failures.add("ptrTo failed: " + e);
                    }
                }));
            }
            for (Thread t : ts) {
                t.start();
            }
            for (Thread t : ts) {
                t.join();
            }
            assertTrue(failures.isEmpty(),
                    "round " + round + " concurrent ptrTo failed: " + failures);
            Set<Long> distinct = new HashSet<>(minted);
            assertTrue(distinct.size() == 1,
                    "round " + round + ": same-type concurrent ptrTo minted "
                            + distinct.size() + " distinct tokens " + distinct
                            + " (canonicalization broken)");
            long tok = distinct.iterator().next();
            ReflectTypes.TypeInfo info = ReflectTypes.table().info(tok);
            assertTrue(info != null && info.kind == GoKind.POINTER
                            && info.elem != null && info.elem.tok == elemTok,
                    "round " + round + ": minted token " + tok + " is not *int: " + info);
            assertTrue(ReflectTypes.table().byTok.size() == before + 1,
                    "round " + round + ": table grew by "
                            + (ReflectTypes.table().byTok.size() - before)
                            + " instead of 1 (duplicate mint left an orphan entry)");
        }
    }
}
