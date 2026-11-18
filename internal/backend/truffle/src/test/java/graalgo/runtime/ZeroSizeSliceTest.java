package graalgo.runtime;

import graalgo.values.GoKind;
import graalgo.values.GoSlice;
import graalgo.values.GoStructDescriptor;
import java.util.List;

/**
 * bug-037: appending to a zero-size-element slice crashed with
 * {@code IllegalArgumentException: slice bounds out of range}.
 *
 * <p>The zero-size path in {@link Intrinsics#growslice} built a
 * {@link GoSlice} with a 0-length backing array but {@code len=cap=newLen>0},
 * violating GoSlice's bounds invariant. The fix allocates a real backing
 * (sized geometrically, reported cap exactly newLen per gc) and reuses it
 * across appends.
 *
 * <p>Each test would have thrown before the fix (demonstrably capable of
 * failing); all assert gc-parity observable behavior.
 */
public final class ZeroSizeSliceTest {

    private static final GoStructDescriptor EMPTY_DESC =
            new GoStructDescriptor("Empty", List.of());

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /** Growing from nil: len=1 cap=1, no crash (the original repro). */
    public void testGrowFromNil() {
        GoSlice s = Intrinsics.growslice(null, GoKind.STRUCT, EMPTY_DESC, 1, 0, false);
        check(s.len() == 1, "len: expected 1 got " + s.len());
        check(s.cap() == 1, "cap: expected 1 (gc parity) got " + s.cap());
    }

    /** gc reports cap == newLen exactly for zero-size growth (never doubled). */
    public void testCapEqualsNewLen() {
        GoSlice s = null;
        for (int i = 1; i <= 10; i++) {
            s = Intrinsics.growslice(s, GoKind.STRUCT, EMPTY_DESC, i, 0, false);
            check(s.len() == i, "len: expected " + i + " got " + s.len());
            check(s.cap() == i, "cap: expected " + i + " (gc parity) got " + s.cap());
        }
    }

    /** Growth from a non-zero cap (make + append): no crash, exact cap. */
    public void testGrowFromMake() {
        GoSlice s = Intrinsics.makeslice(GoKind.STRUCT, EMPTY_DESC, 2, 2);
        check(s.len() == 2 && s.cap() == 2, "make: expected 2/2");
        GoSlice grown = Intrinsics.growslice(s, GoKind.STRUCT, EMPTY_DESC, 3, 0, false);
        check(grown.len() == 3, "len: expected 3 got " + grown.len());
        check(grown.cap() == 3, "cap: expected 3 (gc parity) got " + grown.cap());
    }

    /** Multi-append in one growslice call: no crash, exact cap. */
    public void testMultiAppend() {
        GoSlice s = Intrinsics.growslice(null, GoKind.STRUCT, EMPTY_DESC, 3, 0, false);
        check(s.len() == 3, "len: expected 3 got " + s.len());
        check(s.cap() == 3, "cap: expected 3 (gc parity) got " + s.cap());
    }

    /** Repeated appends reuse the backing (amortized O(1), not O(n^2)). */
    public void testRepeatedAppendsFast() {
        GoSlice s = null;
        long start = System.nanoTime();
        for (int i = 1; i <= 20000; i++) {
            s = Intrinsics.growslice(s, GoKind.STRUCT, EMPTY_DESC, i, 0, false);
        }
        long ms = (System.nanoTime() - start) / 1_000_000;
        check(s.len() == 20000, "len: expected 20000 got " + s.len());
        check(s.cap() == 20000, "cap: expected 20000 got " + s.cap());
        // 20k appends must be fast; the pre-fix-perf path (fresh geometric
        // alloc + full zero-init per append) took minutes.
        check(ms < 5000, "20k appends took " + ms + "ms (expected < 5000ms)");
    }
}
