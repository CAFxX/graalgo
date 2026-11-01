package graalgo.p3reflect;

import graalgo.frontend.Json;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

import java.util.List;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;
import java.util.concurrent.locks.LockSupport;

/**
 * Concurrent-reflect reproducer for review G1 (REVIEW-2026-11-01-02):
 * {@code AbiTypes.tokForShell} and {@code AbiTypes.unpackInterfaceView}
 * read the static {@code SHELL_TOKS} IdentityHashMap without holding the
 * class lock that every writer ({@code abiPointerForTok}) holds, while
 * guest virtual threads can call both readers concurrently with minting.
 *
 * <p>Isolation design (deliberately free of happens-before masking and
 * of cross-round artifacts):
 * <ul>
 *   <li>Each repetition installs a fresh wire table with new named
 *       types, so every repetition re-mints over a thousand shells and
 *       re-sizes {@code SHELL_TOKS} from empty.</li>
 *   <li>Shells are published to slots through plain (non-volatile)
 *       array writes only, after {@code abiPointerForTok} returns, so
 *       no synchronization edge masks the map race. Once a reader
 *       observes a non-null shell in a slot, that shell was minted
 *       (its map put precedes the slot write in program order on the
 *       writer), so {@code tokForShell} returning 0, a different
 *       token, or {@code unpackInterfaceView} returning null for its
 *       view is an observable wrong resolution, not an ordering
 *       artifact.</li>
 *   <li>Repetitions are fully sequential: every worker of a repetition
 *       is joined before the next table is installed, so no reader can
 *       straddle a table install (a straddling unpack would resolve
 *       its token through the new table and corrupt that table's
 *       descriptor memo — a real adjacent race in
 *       {@code ReflectTypes.descriptorForTok}, observed during
 *       harness development, but not the SHELL_TOKS race measured
 *       here).</li>
 *   <li>Descriptor lookup is pre-warmed single-threaded by the
 *       installer before workers start, so unpack's descriptor lookup
 *       is a get-only read during the hammer and cannot confound the
 *       SHELL_TOKS measurement.</li>
 *   <li>Readers park 1us per scan: with few CPUs, tight reader spins
 *       otherwise starve the writer virtual threads (carrier
 *       starvation, a harness artifact, not a target-code hang).</li>
 * </ul>
 *
 * <p>Registered in {@code graalgo.iface.IfaceTestRunner} as
 * {@code testConcurrentAbiShellResolution}; {@code main} runs the same
 * hammer standalone and exits non-zero on any failure.
 */
public final class AbiTypesConcurrencyTest {

    /** Signatures minted per repetition (1024 shells force several resizes). */
    private static final int SIGS_PER_ROUND = 1024;
    /** Sequential repetitions of install + mint + resolve. */
    private static final int REPS = 12;
    private static final int WRITERS = 8;
    private static final int READERS = 8;

    /** One repetition's publication state. */
    private static final class RoundState {
        final int round;
        final GoPointer[] slots = new GoPointer[SIGS_PER_ROUND];
        final long[] toks = new long[SIGS_PER_ROUND];
        final graalgo.values.GoTypeDescriptor[] descs = new graalgo.values.GoTypeDescriptor[SIGS_PER_ROUND];

        RoundState(int round) {
            this.round = round;
        }
    }

    private static volatile RoundState currentRound;
    private static final AtomicReference<String> firstFailure = new AtomicReference<>();
    private static final java.util.concurrent.ConcurrentLinkedQueue<String> allFailures =
            new java.util.concurrent.ConcurrentLinkedQueue<>();
    private static final AtomicLong failureCount = new AtomicLong();
    private static final AtomicLong tokChecks = new AtomicLong();
    private static final AtomicLong unpackChecks = new AtomicLong();

    private static void recordFailure(String detail) {
        failureCount.incrementAndGet();
        firstFailure.compareAndSet(null, detail);
        if (allFailures.size() < 10) {
            allFailures.add(detail);
        }
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /** Builds the wire document for one repetition: abi descriptors + N named structs + N signatures. */
    private static String buildDoc(int round) {
        StringBuilder sb = new StringBuilder();
        sb.append("{\"types\": [");
        // 0: basic int. 1: basic uint8. 2: pointer to uint8. 3: empty signature (Equal field).
        sb.append("{\"kind\": \"basic\", \"detail\": {\"name\": \"int\", \"class\": \"int\", \"untyped\": false}},");
        sb.append("{\"kind\": \"basic\", \"detail\": {\"name\": \"uint8\", \"class\": \"int\", \"untyped\": false}},");
        sb.append("{\"kind\": \"pointer\", \"detail\": {\"elem\": 1}},");
        sb.append("{\"kind\": \"signature\", \"detail\": {\"params\": [], \"results\": [], \"variadic\": false}},");
        // 4: struct underlying internal/abi.Type.
        sb.append("{\"kind\": \"struct\", \"detail\": {\"fields\": [");
        String[] abiTypeFields = {"Size_", "PtrBytes", "Hash", "TFlag", "Align_", "FieldAlign_", "Kind_"};
        int off = 0;
        for (int i = 0; i < abiTypeFields.length; i++) {
            if (i > 0) {
                sb.append(',');
            }
            sb.append("{\"name\": \"").append(abiTypeFields[i])
                    .append("\", \"type\": 0, \"tag\": \"\", \"offset\": ").append(off).append('}');
            off += 8;
        }
        sb.append(",{\"name\": \"Equal\", \"type\": 3, \"tag\": \"\", \"offset\": ").append(off).append('}');
        off += 8;
        sb.append(",{\"name\": \"GCData\", \"type\": 2, \"tag\": \"\", \"offset\": ").append(off).append('}');
        off += 8;
        sb.append(",{\"name\": \"Str\", \"type\": 0, \"tag\": \"\", \"offset\": ").append(off).append('}');
        off += 8;
        sb.append(",{\"name\": \"PtrToThis\", \"type\": 0, \"tag\": \"\", \"offset\": ").append(off).append('}');
        sb.append("]}},");
        // 5: named internal/abi.Type.
        sb.append("{\"kind\": \"named\", \"detail\": {\"name\": \"internal/abi.Type\", "
                + "\"pkgpath\": \"internal/abi\", \"underlying\": 4, \"methods\": []}},");
        // 6: struct underlying internal/abi.FuncType (embeds Type, then counts).
        sb.append("{\"kind\": \"struct\", \"detail\": {\"fields\": ["
                + "{\"name\": \"Type\", \"type\": 5, \"tag\": \"\", \"offset\": 0},"
                + "{\"name\": \"InCount\", \"type\": 0, \"tag\": \"\", \"offset\": 88},"
                + "{\"name\": \"OutCount\", \"type\": 0, \"tag\": \"\", \"offset\": 96}]}},");
        // 7: named internal/abi.FuncType.
        sb.append("{\"kind\": \"named\", \"detail\": {\"name\": \"internal/abi.FuncType\", "
                + "\"pkgpath\": \"internal/abi\", \"underlying\": 6, \"methods\": []}}");
        int nextId = 8;
        for (int i = 0; i < SIGS_PER_ROUND; i++) {
            int structId = nextId++;
            int namedId = nextId++;
            nextId++; // signature id, recorded by the caller via position math
            sb.append(",{\"kind\": \"struct\", \"detail\": {\"fields\": ["
                    + "{\"name\": \"X\", \"type\": 0, \"tag\": \"\", \"offset\": 0}]}},");
            sb.append("{\"kind\": \"named\", \"detail\": {\"name\": \"main.R").append(round)
                    .append(".T").append(i).append("\", \"pkgpath\": \"main\", \"underlying\": ")
                    .append(structId).append(", \"methods\": []}},");
            sb.append("{\"kind\": \"signature\", \"detail\": {\"params\": [{\"name\": \"v\", \"type\": ")
                    .append(namedId).append("}], \"results\": [], \"variadic\": false}}");
        }
        sb.append("]}");
        return sb.toString();
    }

    /** Wire id of signature i in a repetition document (layout fixed by buildDoc). */
    private static int sigWireId(int i) {
        return 8 + i * 3 + 2;
    }

    private static Json.Object parseDoc(String text) {
        Json.Value v = Json.parse(text);
        if (!(v instanceof Json.Object doc)) {
            throw new AssertionError("test doc must be a JSON object");
        }
        return doc;
    }

    /** The EmptyInterface view layout used by UnsafeToPtrNode for empty interfaces. */
    private static List<AbiTypes.ViewField> emptyIfaceLayout() {
        return List.of(
                new AbiTypes.ViewField("Type", 0, AbiTypes.ViewRole.ABI_TYPE),
                new AbiTypes.ViewField("Data", 8, AbiTypes.ViewRole.DATA));
    }

    private static GoStructDescriptor viewDesc() {
        return GoStructDescriptor.layout("test.EmptyInterface",
                List.of("Type", "Data"),
                List.of(GoKind.UNSAFE_POINTER, GoKind.INT64));
    }

    /** One repetition: install, pre-warm, mint on writers, resolve on readers, join all workers. */
    private static void runRep(int rep, List<AbiTypes.ViewField> layout, GoStructDescriptor vd) throws Exception {
        ReflectTypes.install(parseDoc(buildDoc(rep)), Map.of());
        RoundState st = new RoundState(rep);
        for (int i = 0; i < SIGS_PER_ROUND; i++) {
            long tok = ReflectTypes.tokForWireId(sigWireId(i));
            assertTrue(tok > 0, "no token for signature wire id " + sigWireId(i) + " rep " + rep);
            st.toks[i] = tok;
            // Single-threaded pre-warm, before any worker starts: descriptorForTok
            // memoizes into the table's tokToDesc HashMap, which is itself
            // unsynchronized, so worker pre-warming would confound this experiment.
            st.descs[i] = ReflectTypes.descriptorForTok(tok);
        }
        AtomicBoolean repDone = new AtomicBoolean(false);
        CountDownLatch done = new CountDownLatch(WRITERS + READERS);
        AtomicReference<Throwable> harnessError = new AtomicReference<>();
        currentRound = st; // announced before workers start

        for (int w = 0; w < WRITERS; w++) {
            final int writer = w;
            Thread.ofVirtual().start(() -> {
                try {
                    for (int i = writer; i < SIGS_PER_ROUND; i += WRITERS) {
                        GoPointer p = AbiTypes.abiPointerForTok(st.toks[i]);
                        st.slots[i] = p; // plain publication, no happens-before edge
                    }
                } catch (Throwable t) {
                    harnessError.compareAndSet(null, t);
                    System.err.println("WRITER " + writer + " rep " + rep + " FAILED: " + t);
                    t.printStackTrace(System.err);
                } finally {
                    done.countDown();
                }
            });
        }
        for (int r = 0; r < READERS; r++) {
            Thread.ofVirtual().start(() -> {
                try {
                    while (!repDone.get()) {
                        RoundState cur = currentRound;
                        if (cur == null) {
                            Thread.sleep(1);
                            continue;
                        }
                        for (int i = 0; i < SIGS_PER_ROUND; i++) {
                            GoPointer p = cur.slots[i];
                            if (p == null) {
                                continue;
                            }
                            GoStruct shell = (GoStruct) p.referent();
                            long got = AbiTypes.tokForShell(shell);
                            tokChecks.incrementAndGet();
                            if (currentRound != cur) {
                                continue; // raced rep end; discard
                            }
                            if (got != cur.toks[i]) {
                                recordFailure("tokForShell rep=" + cur.round + " slot=" + i
                                        + " expected=" + cur.toks[i] + " got=" + got);
                            }
                            if ((tokChecks.get() & 15) == 0 && cur.descs[i] != null) {
                                GoStruct view = GoStruct.zero(vd);
                                view.setObject(0, p);
                                view.setLong(1, 0);
                                GoInterface gi = AbiTypes.unpackInterfaceView(view, layout);
                                unpackChecks.incrementAndGet();
                                if (currentRound != cur) {
                                    continue;
                                }
                                if (gi == null) {
                                    recordFailure("unpackInterfaceView null rep=" + cur.round
                                            + " slot=" + i + " tok=" + cur.toks[i]);
                                } else if (gi.descriptor() != cur.descs[i] || gi.data() != null) {
                                    recordFailure("unpackInterfaceView wrong rep=" + cur.round
                                            + " slot=" + i + " tok=" + cur.toks[i]
                                            + " descriptor=" + gi.descriptor()
                                            + " expected=" + cur.descs[i] + " data=" + gi.data());
                                }
                            }
                        }
                        // Yield the carrier: with few CPUs, tight reader spins otherwise
                        // starve the writer virtual threads (carrier starvation, not a
                        // target-code hang). A 1us park unmounts the virtual thread.
                        LockSupport.parkNanos(1_000);
                    }
                } catch (Throwable t) {
                    harnessError.compareAndSet(null, t);
                    System.err.println("READER rep " + rep + " FAILED: " + t);
                    t.printStackTrace(System.err);
                } finally {
                    done.countDown();
                }
            });
        }

        // Installer (this thread) waits for every slot to be minted, then ends the rep.
        // No next install happens until all workers below are joined.
        long deadline = System.nanoTime() + 120_000_000_000L;
        for (;;) {
            int minted = 0;
            for (int i = 0; i < SIGS_PER_ROUND; i++) {
                if (st.slots[i] != null) {
                    minted++;
                }
            }
            if (minted == SIGS_PER_ROUND) {
                break;
            }
            if (System.nanoTime() > deadline) {
                throw new AssertionError("rep " + rep + " did not finish minting in 120s: "
                        + minted + "/" + SIGS_PER_ROUND + " harnessError=" + harnessError.get());
            }
            Thread.sleep(5);
        }
        repDone.set(true);
        currentRound = null;
        boolean finished = done.await(120, java.util.concurrent.TimeUnit.SECONDS);
        if (!finished) {
            throw new AssertionError("rep " + rep + " HANG: workers did not finish in 120s"
                    + " tokChecks=" + tokChecks.get() + " unpackChecks=" + unpackChecks.get());
        }
        if (harnessError.get() != null) {
            throw new AssertionError("rep " + rep + " harness error", harnessError.get());
        }
    }

    /** Runs the full hammer; returns a one-line summary. Throws on internal harness error. */
    public static String runHammer() throws Exception {
        firstFailure.set(null);
        allFailures.clear();
        failureCount.set(0);
        tokChecks.set(0);
        unpackChecks.set(0);
        currentRound = null;
        List<AbiTypes.ViewField> layout = emptyIfaceLayout();
        GoStructDescriptor vd = viewDesc();
        for (int rep = 0; rep < REPS; rep++) {
            runRep(rep, layout, vd);
        }
        return "reps=" + REPS + " tokChecks=" + tokChecks.get() + " unpackChecks=" + unpackChecks.get()
                + " failures=" + failureCount.get()
                + (firstFailure.get() == null ? "" : " all=" + allFailures);
    }

    public void testConcurrentAbiShellResolution() throws Exception {
        String summary = runHammer();
        System.out.println("AbiTypesConcurrency: " + summary);
        assertTrue(failureCount.get() == 0,
                "concurrent reflect resolution failures: " + summary);
    }

    public static void main(String[] args) throws Exception {
        String summary = runHammer();
        System.out.println("AbiTypesConcurrency: " + summary);
        if (failureCount.get() > 0) {
            System.out.println("REPRODUCED: concurrent wrong resolution observed");
            System.exit(1);
        }
        System.out.println("NOT REPRODUCED: no corruption or wrong resolution observed");
    }
}
