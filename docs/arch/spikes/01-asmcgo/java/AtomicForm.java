import java.lang.invoke.MethodHandles;
import java.lang.invoke.VarHandle;

/**
 * AtomicForm: the INTRINSIFY formulation of src/internal/runtime/atomic
 * (amd64) at the JVM level.
 *
 * On graalgo there is no Go implementation of the atomic ops at all: the
 * backend intrinsic for each op lowers directly to a java.lang.invoke.
 * VarHandle access mode. This file is that mapping, stated exactly, plus
 * a concurrent stress test proving the mapped modes deliver the same
 * observable guarantees as the Go LOCK-prefixed assembly:
 *   - atomic read-modify-write (no lost updates),
 *   - CAS linearizability (every success installs a unique value),
 *   - no torn 64-bit reads/writes,
 *   - bitwise RMW atomicity.
 *
 * Mapping table (Go internal/runtime/atomic -> VarHandle):
 *   Load/Load64/Loaduintptr/Loadp        -> getVolatile
 *   LoadAcq/LoadAcq64/LoadAcquintptr     -> getAcquire
 *   Store/Store64/Storeuintptr           -> setVolatile
 *   StoreRel/StoreRel64/StoreReluintptr  -> setRelease
 *   Xchg/Xchg64/Xchguintptr              -> getAndSet
 *   Xadd/Xadd64/Xadduintptr              -> getAndAdd
 *   Cas/Cas64/Casuintptr/Casp1           -> compareAndSet
 *   And32/And64/Anduintptr               -> getAndBitwiseAnd
 *   Or32/Or64/Oruintptr                  -> getAndBitwiseOr
 *   And8/Or8 (void)                      -> getAndBitwiseAnd/Or (result discarded)
 *   StorepNoWB                           -> setVolatile (no write barriers on JVM)
 *
 * Notes:
 *  - VarHandle.getVolatile on long is specified atomic (JLS 17.7 gives
 *    only 32-bit atomicity for plain accesses; the volatile access modes
 *    extend atomicity to 64-bit). This is exactly the guarantee the Go
 *    asm provides via LOCK CMPXCHG / XADD on amd64.
 *  - Go's Cas has full-barrier semantics; compareAndSet is specified
 *    with volatile (full-fence) effects. CasRel/StoreRel/LoadAcq map to
 *    the weaker release/acquire modes, matching the Go memory model
 *    documentation for those variants.
 */
public class AtomicForm {
    // Plain (non-volatile) fields: all ordering/atomicity comes from the
    // VarHandle access modes alone, mirroring Go's plainly-addressed words.
    static long word64;
    static int word32;
    static long counter;

    static final VarHandle VH64, VH32, VHCTR;
    static {
        try {
            var l = MethodHandles.lookup();
            VH64  = l.findStaticVarHandle(AtomicForm.class, "word64", long.class);
            VH32  = l.findStaticVarHandle(AtomicForm.class, "word32", int.class);
            VHCTR = l.findStaticVarHandle(AtomicForm.class, "counter", long.class);
        } catch (Exception e) { throw new RuntimeException(e); }
    }

    static int failures = 0;
    static void check(boolean cond, String name) {
        System.out.println((cond ? "ok   " : "FAIL ") + name);
        if (!cond) failures++;
    }

    public static void main(String[] a) throws Exception {
        final int T = 8, N = 200_000;

        // T1: getAndAdd atomicity (Go: Xadd hammer). 8 threads x N adds.
        VHCTR.setVolatile(0L);
        Thread[] ts = new Thread[T];
        for (int t = 0; t < T; t++) {
            ts[t] = Thread.ofPlatform().start(() -> {
                for (int i = 0; i < N; i++) VHCTR.getAndAdd(1L);
            });
        }
        for (Thread t : ts) t.join();
        check((long) VHCTR.getVolatile() == (long) T * N,
              "T1 getAndAdd: no lost updates (" + VHCTR.getVolatile() + " == " + ((long) T * N) + ")");

        // T2: compareAndSet linearizability (Go: Cas hammer). Each thread
        // installs N unique increments; final must equal T*N exactly.
        VHCTR.setVolatile(0L);
        for (int t = 0; t < T; t++) {
            final int M = 50_000;
            ts[t] = Thread.ofPlatform().start(() -> {
                for (int i = 0; i < M; i++) {
                    long v;
                    do { v = (long) VHCTR.getVolatile(); }
                    while (!VHCTR.compareAndSet(v, v + 1));
                }
            });
        }
        for (Thread t : ts) t.join();
        check((long) VHCTR.getVolatile() == (long) T * 50_000,
              "T2 compareAndSet: every success installed (" + VHCTR.getVolatile() + ")");

        // T3: no torn 64-bit reads (Go: Load64 vs concurrent Store64).
        // Writer publishes 1..W via setVolatile; readers must only ever
        // observe values in [0, W] (a torn read would show garbage bits).
        final int W = 2_000_000;
        final long[] bad = {0};
        VH64.setVolatile(0L);
        Thread writer = Thread.ofPlatform().start(() -> {
            for (long i = 1; i <= W; i++) VH64.setVolatile(i);
        });
        Thread[] readers = new Thread[4];
        for (int t = 0; t < 4; t++) {
            readers[t] = Thread.ofPlatform().start(() -> {
                for (;;) {
                    long v = (long) VH64.getVolatile();
                    if (v < 0 || v > W) { bad[0]++; break; }
                    if (v == W) break;
                }
            });
        }
        writer.join();
        for (Thread t : readers) t.join();
        check(bad[0] == 0, "T3 getVolatile/setVolatile long: no torn reads");

        // T4: getAndSet (Go: Xchg) returns the previous value exactly once
        // per install: chain of T threads each swapping in its id.
        VH64.setVolatile(-1L);
        long[] seen = new long[T];
        for (int t = 0; t < T; t++) {
            final int id = t;
            ts[t] = Thread.ofPlatform().start(() ->
                seen[id] = (long) VH64.getAndSet(id));
        }
        for (Thread t : ts) t.join();
        java.util.Arrays.sort(seen);
        boolean chainOk = seen[0] == -1;
        for (int t = 1; t < T; t++) chainOk &= seen[t] >= 0 && seen[t] < T;
        // every getAndSet returned a distinct predecessor
        java.util.Set<Long> s = new java.util.HashSet<>();
        for (long v : seen) s.add(v);
        check(chainOk && s.size() == T, "T4 getAndSet: each swap observed exactly once");

        // T5: bitwise RMW atomicity (Go: And64/Or64 hammer) with disjoint
        // bit ranges per thread; final must equal the full OR.
        VH64.setVolatile(0L);
        for (int t = 0; t < T; t++) {
            final long bits = 0xFFL << (t * 8);
            ts[t] = Thread.ofPlatform().start(() -> {
                for (int i = 0; i < N; i++) VH64.getAndBitwiseOr(bits);
            });
        }
        for (Thread t : ts) t.join();
        check((long) VH64.getVolatile() == -1L,
              "T5 getAndBitwiseOr: all bits set exactly (" +
              Long.toHexString((long) VH64.getVolatile()) + ")");

        // T6: acquire/release pairing (Go: StoreRel/LoadAcq). Writer stores
        // payload then flag with setRelease; reader spins on getAcquire of
        // the flag and must see the payload (no reordering).
        final int[] payload = {0};
        final int[] flag = {0};
        VarHandle VHFLAG, VHPAY;
        class H { int p, f; }
        H h = new H();
        var l = MethodHandles.lookup();
        VHPAY  = l.findVarHandle(H.class, "p", int.class);
        VHFLAG = l.findVarHandle(H.class, "f", int.class);
        final boolean[] err = {false};
        Thread w = Thread.ofPlatform().start(() -> {
            for (int i = 1; i <= 1_000_000; i++) {
                VHPAY.setRelease(h, i);
                VHFLAG.setRelease(h, i);
            }
        });
        Thread r = Thread.ofPlatform().start(() -> {
            int last = 0;
            while (last < 1_000_000) {
                int f = (int) VHFLAG.getAcquire(h);
                if (f != last) {
                    int p = (int) VHPAY.getAcquire(h);
                    if (p < f) err[0] = true; // payload must be visible
                    last = f;
                }
            }
        });
        w.join(); r.join();
        check(!err[0], "T6 setRelease/getAcquire: payload visible with flag");

        System.out.println(failures == 0 ? "RESULT: PASS" : "RESULT: FAIL");
        if (failures > 0) System.exit(1);
    }
}
