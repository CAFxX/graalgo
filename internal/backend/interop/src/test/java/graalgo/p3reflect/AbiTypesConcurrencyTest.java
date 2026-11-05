package graalgo.p3reflect;

import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;

import java.util.Map;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Regression tests for NEW-P1-1 (independent review 2026-11-05-22):
 * {@code AbiTypes.tokForShell} read the {@code SHELL_TOKS}
 * {@code IdentityHashMap} with no synchronization while
 * {@code abiPointerForTok} mutated it ({@code put} on first mint,
 * {@code clear} on a table change) under the class lock. Guest
 * goroutines run on concurrent virtual threads, so a {@code get}
 * racing a {@code put}/{@code clear} could return 0 (or a wrong
 * token) for a minted shell — silent wrong behavior in
 * {@code ReflectLinknames.tokForTypeArg}. The fix synchronizes
 * {@code tokForShell} (and {@code unpackInterfaceView}, the file's
 * other unsynchronized {@code SHELL_TOKS} reader, called from
 * {@code UnsafeToPtrNode}) on the same class lock as every sibling
 * accessor.
 *
 * <p>Registered in {@code graalgo.iface.IfaceTestRunner}.
 */
public final class AbiTypesConcurrencyTest {

    /** Distinct named types per round (main.T0..main.T191, underlying int). */
    private static final int N_TYPES = 192;
    /** Table reinstalls, each re-minting every shell from empty. */
    private static final int ROUNDS = 20;

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
     * Wire document: basic {@code int}, a signature and a pointer
     * (for the {@code Equal}/{@code GCData} fields of the abi header),
     * the named {@code internal/abi.Type} header struct
     * {@code abiPointerForTok} fills (its fields are the ones
     * {@code fillTypeFields} sets by name), and {@code N_TYPES} named
     * {@code main.T<i>} types over {@code int} — one distinct token
     * each, all minted through the plain-{@code Type} header path, so
     * no kind-specific abi layout is needed.
     */
    private static String docText() {
        StringBuilder sb = new StringBuilder("{\"types\": [");
        // 0: basic int.
        sb.append("{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"int\", \"class\": \"int\", \"untyped\": false}}");
        // 1: signature (Equal func field).
        sb.append(",{\"kind\": \"signature\", \"detail\": "
                + "{\"params\": [], \"results\": [], \"variadic\": false}}");
        // 2: pointer to int (GCData field).
        sb.append(",{\"kind\": \"pointer\", \"detail\": {\"elem\": 0, \"methods\": []}}");
        // 3: struct underlying of internal/abi.Type (8-byte stride;
        //    only names + kinds matter to the descriptor).
        sb.append(",{\"kind\": \"struct\", \"detail\": {\"fields\": ["
                + "{\"name\": \"Size_\", \"type\": 0, \"tag\": \"\", \"offset\": 0},"
                + "{\"name\": \"PtrBytes\", \"type\": 0, \"tag\": \"\", \"offset\": 8},"
                + "{\"name\": \"Hash\", \"type\": 0, \"tag\": \"\", \"offset\": 16},"
                + "{\"name\": \"TFlag\", \"type\": 0, \"tag\": \"\", \"offset\": 24},"
                + "{\"name\": \"Align_\", \"type\": 0, \"tag\": \"\", \"offset\": 32},"
                + "{\"name\": \"FieldAlign_\", \"type\": 0, \"tag\": \"\", \"offset\": 40},"
                + "{\"name\": \"Kind_\", \"type\": 0, \"tag\": \"\", \"offset\": 48},"
                + "{\"name\": \"Equal\", \"type\": 1, \"tag\": \"\", \"offset\": 56},"
                + "{\"name\": \"GCData\", \"type\": 2, \"tag\": \"\", \"offset\": 64},"
                + "{\"name\": \"Str\", \"type\": 0, \"tag\": \"\", \"offset\": 72},"
                + "{\"name\": \"PtrToThis\", \"type\": 0, \"tag\": \"\", \"offset\": 80}"
                + "]}}");
        // 4: named internal/abi.Type.
        sb.append(",{\"kind\": \"named\", \"detail\": "
                + "{\"name\": \"internal/abi.Type\", \"pkgpath\": \"internal/abi\", "
                + "\"underlying\": 3, \"methods\": []}}");
        // 5..: named main.T<i> over int.
        for (int i = 0; i < N_TYPES; i++) {
            sb.append(",{\"kind\": \"named\", \"detail\": "
                    + "{\"name\": \"main.T").append(i).append("\", "
                    + "\"pkgpath\": \"main\", \"underlying\": 0, \"methods\": []}}");
        }
        sb.append("]}");
        return sb.toString();
    }

    /** Tokens of the named main.T<i> types, in wire order. */
    private static long[] mainTypeTokens(ReflectTypes.Table table) {
        long[] toks = new long[N_TYPES];
        int n = 0;
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti != null && ti.name != null && ti.name.startsWith("main.T")) {
                toks[n++] = ti.tok;
            }
        }
        assertTrue(n == N_TYPES,
                "expected " + N_TYPES + " main.T tokens, got " + n);
        return toks;
    }

    /**
     * Single-threaded pin: a minted shell resolves to exactly its token,
     * and a structurally identical but never-minted shell resolves to 0.
     */
    public void testTokForShellReturnsMintedTokenAndZeroForUnminted() {
        ReflectTypes.install(parseDoc(docText()), Map.of());
        long[] toks = mainTypeTokens(ReflectTypes.table());
        GoPointer ptr = AbiTypes.abiPointerForTok(toks[0]);
        assertTrue(ptr.referent() instanceof GoStruct,
                "abiPointerForTok did not return a shell pointer");
        GoStruct shell = (GoStruct) ptr.referent();
        assertTrue(AbiTypes.tokForShell(shell) == toks[0],
                "tokForShell(minted shell) = " + AbiTypes.tokForShell(shell)
                        + ", expected " + toks[0]);
        GoStruct unminted = GoStruct.zero(
                ReflectTypes.structDescriptorFor("internal/abi.Type"));
        assertTrue(AbiTypes.tokForShell(unminted) == 0,
                "tokForShell(unminted shell) = " + AbiTypes.tokForShell(unminted)
                        + ", expected 0");
    }

    /**
     * NEW-P1-1: while one thread mints fresh shells via
     * {@code abiPointerForTok} (putting into {@code SHELL_TOKS},
     * resizing it repeatedly), another thread loops
     * {@code tokForShell} over every shell published so far. A
     * published shell is already in the map, so its lookup must
     * return exactly its token — never 0, never another token.
     * Each round reinstalls the table, clearing the memo, so the
     * minting (and the resizes) happen again from empty.
     *
     * <p>This is a probabilistic detector for a data race: it can
     * only fail while the race is present, and may pass on any
     * single run even then. The structural guarantee is the
     * {@code synchronized} modifier on both accessors; this test
     * pins the observable invariant the race violated.
     */
    public void testConcurrentMintAndTokForShellNeverReturnsWrongToken() throws Exception {
        String doc = docText();
        for (int round = 0; round < ROUNDS; round++) {
            ReflectTypes.install(parseDoc(doc), Map.of());
            final long[] expected = mainTypeTokens(ReflectTypes.table());
            final int total = expected.length;
            final GoStruct[] shells = new GoStruct[total];
            final AtomicInteger published = new AtomicInteger(0);
            final AtomicBoolean writerDone = new AtomicBoolean(false);
            final AtomicReference<String> failure = new AtomicReference<>();
            final int roundNo = round;

            Thread writer = new Thread(() -> {
                try {
                    for (int i = 0; i < total; i++) {
                        GoPointer p = AbiTypes.abiPointerForTok(expected[i]);
                        shells[i] = (GoStruct) p.referent();
                        // Volatile publish: the shell store happens-before
                        // the reader's read of this count.
                        published.incrementAndGet();
                    }
                } catch (Throwable t) {
                    failure.compareAndSet(null,
                            "round " + roundNo + ": writer failed: " + t);
                } finally {
                    writerDone.set(true);
                }
            }, "tokForShell-race-writer");
            Thread reader = new Thread(() -> {
                long checks = 0;
                while (true) {
                    int avail = published.get();
                    for (int i = 0; i < avail; i++) {
                        long got = AbiTypes.tokForShell(shells[i]);
                        checks++;
                        if (got != expected[i]) {
                            failure.compareAndSet(null, "round " + roundNo
                                    + ": tokForShell(shell[" + i + "]) = " + got
                                    + ", expected " + expected[i]
                                    + " after " + checks + " checks");
                            return;
                        }
                    }
                    if (failure.get() != null) {
                        return;
                    }
                    if (writerDone.get() && published.get() >= total) {
                        return;
                    }
                }
            }, "tokForShell-race-reader");
            writer.start();
            reader.start();
            writer.join();
            reader.join();
            assertTrue(failure.get() == null,
                    "concurrent tokForShell returned a wrong token: " + failure.get());
        }
    }
}
