package graalgo.p3unsafe;

import graalgo.values.UnsafeHandles;

/**
 * fix-redmaster-r39: unit tests for {@link UnsafeTable#derive} — the
 * provenance-preserving byte derivation behind the "unsafederive"
 * wire node and {@code unsafe.Add}.
 *
 * <p>The soundness contract under test: a derivation is computed from
 * a LIVE base handle and can only ever view the base's own referent.
 * Handles are dense sequential integers, so any scheme that resolved
 * {@code base + offset} arithmetically in handle space could alias an
 * unrelated live object (the exact-handle design r38 rejected);
 * {@code derive} instead interns a (root, byteOffset) view, and these
 * tests pin that no derivation ever resolves to another referent's
 * handle or root.
 *
 * <p>Element-boundary validation at the consumption site
 * ({@code UnsafeToPtrNode}) is covered end-to-end by the runnable
 * probes recorded in hidden_files/red-master-r39 (aligned derivation
 * reads the right element; misaligned and out-of-range derivations
 * trap). These tests fail on the unfixed backend: {@code derive} and
 * {@code resolveView} do not exist there.
 */
public final class UnsafeDeriveTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(long expected, long actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /** A fresh table per test: handles restart from 1, like a new run. */
    private static UnsafeTable freshTable() {
        UnsafeTable.install();
        return UnsafeTable.current();
    }

    /** The canonical element-0 handle for an array, as &arr[0] interns. */
    private static long baseOf(UnsafeTable t, Object array) {
        return t.internEqual(new UnsafeTable.ArrayRef(array, 0));
    }

    public void testZeroOffsetIsIdentity() {
        UnsafeTable t = freshTable();
        long base = baseOf(t, new long[4]);
        assertEquals(base, t.derive(base, 0), "offset-0 derivation must return the base handle");
        assertEquals(0, t.derive(0, 0), "nil derived by 0 stays nil");
    }

    public void testDerivationIsCanonical() {
        UnsafeTable t = freshTable();
        long base = baseOf(t, new long[4]);
        long d1 = t.derive(base, 16);
        long d2 = t.derive(base, 16);
        assertEquals(d1, d2, "the same derivation must yield the same handle (aliasing parity)");
        assertTrue(d1 != base, "a nonzero derivation is a distinct handle from its base");
        UnsafeTable.ResolvedView v = t.resolveView(d1);
        assertEquals(16, v.byteOffset(), "resolved view carries the byte offset");
        assertTrue(v.root() instanceof UnsafeTable.ArrayRef ref && ref.offset() == 0,
                "resolved root is the base ArrayRef");
    }

    public void testDerivationNeverAliasesAnotherReferent() {
        UnsafeTable t = freshTable();
        long[] a = new long[4];
        long[] b = new long[4];
        long baseA = baseOf(t, a);
        long baseB = baseOf(t, b);
        // Handles are dense: baseB is some small integer near baseA.
        // Deriving A by ANY offset must never produce B's handle,
        // B's element handles, or a view rooted at B.
        for (long off = -16; off <= 256; off += 8) {
            if (off == 0) {
                continue;
            }
            long d = t.derive(baseA, off);
            assertTrue(d != baseB, "derivation of A aliased B's base handle at offset " + off);
            for (int i = 0; i < 4; i++) {
                long elemB = t.internEqual(new UnsafeTable.ArrayRef(b, i));
                assertTrue(d != elemB, "derivation of A aliased B's element " + i + " at offset " + off);
            }
            Object root = t.resolveView(d).root();
            assertTrue(root instanceof UnsafeTable.ArrayRef ref && ref.array() == a,
                    "derivation of A resolved to a foreign root at offset " + off);
        }
    }

    public void testNilBaseNonzeroOffsetTraps() {
        UnsafeTable t = freshTable();
        boolean trapped = false;
        try {
            t.derive(0, 8);
        } catch (UnsafeHandles.UnsafeEscapeException e) {
            trapped = true;
        }
        assertTrue(trapped, "deriving from a nil pointer with a nonzero offset must trap");
    }

    public void testNestedDerivationsFlatten() {
        UnsafeTable t = freshTable();
        long base = baseOf(t, new long[8]);
        long step = t.derive(base, 16);
        long two = t.derive(step, 16);
        UnsafeTable.ResolvedView v = t.resolveView(two);
        assertEquals(32, v.byteOffset(), "nested derivations accumulate into one view");
        assertEquals(t.derive(base, 32), two, "flattened derivation is the canonical handle");
        assertEquals(base, t.derive(step, -16),
                "deriving back to offset 0 returns the canonical base handle");
    }

    public void testDeriveFromPlainReferent() {
        UnsafeTable t = freshTable();
        Object referent = new Object();
        long base = t.intern(referent);
        long d = t.derive(base, 24);
        UnsafeTable.ResolvedView v = t.resolveView(d);
        assertTrue(v.root() == referent, "view root is the base referent itself");
        assertEquals(24, v.byteOffset(), "view offset");
        // The plain (non-derived) handle still resolves with offset 0.
        assertEquals(0, t.resolveView(base).byteOffset(), "underived handle has offset 0");
    }
}
