package graalgo.p3reflect;

import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentLinkedQueue;
import java.util.concurrent.CyclicBarrier;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Concurrent regression tests for review NEW-P1-1 / NEW-P1-2
 * (2026-11-04): the p3reflect shared memo tables were plain,
 * unsynchronized maps read from guest reflect paths that can run on
 * multiple carrier threads.
 *
 * <ul>
 *   <li>NEW-P1-1: {@code AbiTypes.SHELL_TOKS} was a plain
 *       {@code IdentityHashMap} whose writer ({@code abiPointerForTok})
 *       was synchronized but whose readers ({@code tokForShell},
 *       {@code unpackInterfaceView}) were not.</li>
 *   <li>NEW-P1-2: {@code ReflectLinknames.DYN_STRUCT_TOKS} was a plain
 *       {@code IdentityHashMap}, {@code dynStructTable} plain
 *       non-volatile, and the struct synthesizer minted
 *       {@code table.byTok.size()} before appending — so concurrent
 *       {@code StructOf} calls could mint duplicate tokens.</li>
 * </ul>
 *
 * <p>Race tests are probabilistic by nature: on the unfixed code the
 * struct test fails only when two threads overlap in the mint window,
 * which 16 threads x 50 fresh rounds makes overwhelmingly likely, and
 * the shell test only corrupts under concurrent put/get pressure.
 * On the fixed code both are deterministic: the locks serialize the
 * accesses, so every round mints exactly one token and every reader
 * sees exactly the minted token. Falsifiability: deleting either lock
 * re-opens the race the corresponding test exercises.
 */
public final class ReflectRaceTest {

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
     * Installs a table carrying the basic {@code int} type plus the
     * wire named types {@code abiPointerForTok} needs to mint shells:
     * {@code internal/abi.Type} (header fields) and
     * {@code internal/abi.ArrayType} (for the distinct array tokens
     * the mint-race test uses). Object-carried abi fields point at an
     * empty interface wire type; long-carried fields at {@code int}.
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
     * NEW-P1-1: readers of the canonical shell must see exactly the
     * minted token while other threads mint shells for distinct types
     * (each mint puts into the same map the readers read).
     */
    public void testTokForShellConcurrentWithMint() throws Exception {
        long intTok = installIntTable();
        GoPointer intPtr = AbiTypes.abiPointerForTok(intTok);
        GoStruct intShell = (GoStruct) intPtr.referent();
        assertTrue(AbiTypes.tokForShell(intShell) == intTok,
                "minted int shell must resolve to its token");

        // Distinct array tokens, minted single-threaded so the
        // concurrent phase exercises only the AbiTypes lock discipline
        // (not the separately unsynchronized ReflectTypes token family).
        List<Long> toks = new ArrayList<>();
        for (int n = 1; n <= 64; n++) {
            toks.add(ReflectTypes.arrayOf(intTok, n));
        }

        ConcurrentLinkedQueue<String> failures = new ConcurrentLinkedQueue<>();
        CyclicBarrier start = new CyclicBarrier(8);
        List<Thread> threads = new ArrayList<>();
        for (int w = 0; w < 4; w++) {
            int wi = w;
            threads.add(new Thread(() -> {
                try {
                    start.await();
                    for (int i = wi; i < toks.size(); i += 4) {
                        long tok = toks.get(i);
                        GoPointer p = AbiTypes.abiPointerForTok(tok);
                        long back = AbiTypes.tokForShell((GoStruct) p.referent());
                        if (back != tok) {
                            failures.add("minted token " + tok + " read back as " + back);
                        }
                    }
                } catch (Exception e) {
                    failures.add("writer failed: " + e);
                }
            }));
        }
        for (int r = 0; r < 4; r++) {
            threads.add(new Thread(() -> {
                try {
                    start.await();
                    for (int i = 0; i < 20_000; i++) {
                        long back = AbiTypes.tokForShell(intShell);
                        if (back != intTok) {
                            failures.add("int shell read back as " + back
                                    + " instead of " + intTok);
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
        assertTrue(failures.isEmpty(), "concurrent shell reads failed: " + failures);
        for (long tok : toks) {
            GoPointer p = AbiTypes.abiPointerForTok(tok);
            assertTrue(AbiTypes.tokForShell((GoStruct) p.referent()) == tok,
                    "token " + tok + " must resolve after the concurrent phase");
        }
    }

    // ------------------------------------------------------------------
    // Dynamic-struct synthesis fixture (NEW-P1-2).
    //
    // synthesizeDynamicToken(header) walks header.prefixParent() for a
    // struct named "internal/abi.StructType". The guest shape is:
    // header (abi.Type, field 0 of an rtype) -> rtype (field 0 of the
    // structType) -> structType (Fields slice + PkgPath). Field types
    // are raw tokens (the Number branch of tokForTypeArg), so no shell
    // minting is involved.
    // ------------------------------------------------------------------

    private static GoStructDescriptor headerDesc() {
        return GoStructDescriptor.layout("internal/abi.Type",
                List.of("Kind_", "Size_", "Align_"),
                List.of(GoKind.INT64, GoKind.INT64, GoKind.INT64));
    }

    private static GoStructDescriptor nameDesc() {
        return GoStructDescriptor.layout("internal/abi.Name",
                List.of("Bytes"),
                List.of(GoKind.POINTER));
    }

    private static GoStructDescriptor fieldDesc() {
        return GoStructDescriptor.layout("internal/abi.StructField",
                List.of("Name", "Typ", "Offset"),
                List.of(GoKind.STRUCT, GoKind.INTERFACE, GoKind.INT64),
                List.of(-1, -1, -1),
                Arrays.asList(null, null, null),
                Arrays.asList(null, null, null),
                Arrays.asList(nameDesc(), null, null));
    }

    private static GoStructDescriptor rtypeDesc() {
        return GoStructDescriptor.layout("reflect.rtype",
                List.of("T"),
                List.of(GoKind.STRUCT),
                List.of(-1),
                Collections.singletonList(null),
                Collections.singletonList(null),
                List.of(headerDesc()));
    }

    private static GoStructDescriptor structTypeDesc() {
        return GoStructDescriptor.layout("internal/abi.StructType",
                List.of("T", "Fields", "PkgPath"),
                List.of(GoKind.STRUCT, GoKind.SLICE, GoKind.STRUCT),
                List.of(-1, -1, -1),
                Arrays.asList(null, null, null),
                Arrays.asList(null, null, null),
                Arrays.asList(rtypeDesc(), null, null));
    }

    /** One fresh dynamic struct (one int field) + its header. */
    private static GoStruct[] dynamicStructFixture(long intTok) {
        GoStruct header = GoStruct.zero(headerDesc());
        header.setLong(0, 25); // Kind_ = Struct
        header.setLong(1, 8); // Size_
        header.setLong(2, 8); // Align_
        GoStruct sf = GoStruct.zero(fieldDesc());
        sf.setObject(0, GoStruct.zero(nameDesc())); // Bytes null -> "" exported
        sf.setObject(1, Long.valueOf(intTok));
        sf.setLong(2, 0);
        GoStruct rtype = GoStruct.zero(rtypeDesc());
        rtype.setObject(0, header);
        GoStruct structType = GoStruct.zero(structTypeDesc());
        structType.setObject(0, rtype);
        structType.setObject(1, new GoSlice(new Object[]{sf}, 0, 1, 1));
        return new GoStruct[]{header, structType};
    }

    private static long synthesizeDynamicToken(GoStruct header) throws Exception {
        Method m = ReflectLinknames.class.getDeclaredMethod(
                "synthesizeDynamicToken", GoStruct.class);
        m.setAccessible(true);
        return (Long) m.invoke(null, header);
    }

    /**
     * NEW-P1-2: 16 threads synthesizing the SAME dynamic struct at once
     * must all get the same token and mint exactly one table entry.
     * Without the lock, threads that both miss the memo mint the same
     * {@code byTok.size()} and append twice: same returned token, but
     * the table grows by two and the second entry is orphaned.
     */
    public void testConcurrentDynamicStructSynthesisReturnsSingleToken() throws Exception {
        long intTok = installIntTable();
        for (int round = 0; round < 50; round++) {
            GoStruct[] fixture = dynamicStructFixture(intTok);
            GoStruct header = fixture[0];
            ReflectTypes.Table table = ReflectTypes.table();
            int before = table.byTok.size();
            ConcurrentLinkedQueue<String> failures = new ConcurrentLinkedQueue<>();
            AtomicReference<Long> first = new AtomicReference<>();
            CyclicBarrier start = new CyclicBarrier(16);
            List<Thread> threads = new ArrayList<>();
            for (int i = 0; i < 16; i++) {
                threads.add(new Thread(() -> {
                    try {
                        start.await();
                        long tok = synthesizeDynamicToken(header);
                        if (!first.compareAndSet(null, tok) && first.get() != tok) {
                            failures.add("divergent token: " + tok + " vs " + first.get());
                        }
                    } catch (Exception e) {
                        failures.add("synthesis failed: " + e);
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
                    "round " + round + " concurrent synthesis failed: " + failures);
            assertTrue(first.get() != null && first.get() != 0,
                    "round " + round + " must synthesize a nonzero token");
            int grew = table.byTok.size() - before;
            assertTrue(grew == 1,
                    "round " + round + " must mint exactly one table entry, grew by " + grew);
            ReflectTypes.TypeInfo info = table.info(first.get());
            assertTrue(info.kind == GoKind.STRUCT && info.fields.size() == 1,
                    "round " + round + " synthesized info must be a 1-field struct, got " + info);
        }
    }
}
