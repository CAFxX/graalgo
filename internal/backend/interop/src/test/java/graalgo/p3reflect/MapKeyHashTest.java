package graalgo.p3reflect;

import graalgo.frontend.FrontendException;
import graalgo.values.GoBox;
import graalgo.values.GoPointer;
import graalgo.values.GoString;

import java.nio.charset.StandardCharsets;

/**
 * fix-redmaster-r42: unit tests for {@code MapKeyHash} — the 64-bit,
 * type-agnostic hash behind {@code HashTrieMap.keyHash} for
 * {@code sync.Map} (and hence reflect's type caches).
 *
 * <p>The hash values are deliberately NOT gc's memhash output: only trie
 * navigation uses them, never observation. So no test asserts a specific
 * hash value — only determinism (same key + seed), seed sensitivity,
 * key discrimination, and loud failure on nil keys. A test that pinned
 * exact hash values would be tautological here (the values are an
 * implementation choice, not a contract).
 *
 * <p>Falsifiability: replacing {@code mix64} with the identity, or
 * dropping the seed from the mix, fails the seed-sensitivity test;
 * hashing only the first byte of a string fails the discrimination
 * test. Pointer-key identity (same storage -&gt; same handle) cannot be
 * unit-tested without an installed run table
 * ({@code UnsafeTable.handleForPointer} requires {@code current()}), so
 * it is covered by construction plus the {@code sm.go} end-to-end probe
 * instead — see DESIGN.md.
 */
public final class MapKeyHashTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static GoPointer strKey(String s) {
        return new GoPointer(new GoString(s.getBytes(StandardCharsets.UTF_8)));
    }

    public void testStringKeyDeterministic() {
        GoPointer k = strKey("hello");
        long h1 = MapKeyHash.hash(k, 12345L);
        long h2 = MapKeyHash.hash(k, 12345L);
        assertTrue(h1 == h2, "same key + seed hashes identically, got " + h1 + " vs " + h2);
    }

    public void testSeedChangesHash() {
        GoPointer k = strKey("hello");
        long h1 = MapKeyHash.hash(k, 1L);
        long h2 = MapKeyHash.hash(k, 2L);
        assertTrue(h1 != h2, "different seeds must mix differently (hash collision is 2^-64)");
    }

    public void testDistinctStringKeysDiffer() {
        long ha = MapKeyHash.hash(strKey("a"), 0L);
        long hb = MapKeyHash.hash(strKey("b"), 0L);
        assertTrue(ha != hb, "distinct keys must hash distinctly");
        // Longer keys sharing a prefix must also differ: a first-byte-only
        // hash would collide here.
        long h1 = MapKeyHash.hash(strKey("abc"), 0L);
        long h2 = MapKeyHash.hash(strKey("abd"), 0L);
        assertTrue(h1 != h2, "keys differing only in the last byte must hash distinctly");
    }

    public void testIntKeyViaBox() {
        long h42a = MapKeyHash.hash(new GoPointer(new GoBox(42L)), 0L);
        long h42b = MapKeyHash.hash(new GoPointer(new GoBox(42L)), 0L);
        long h43 = MapKeyHash.hash(new GoPointer(new GoBox(43L)), 0L);
        assertTrue(h42a == h42b, "same int key hashes identically");
        assertTrue(h42a != h43, "distinct int keys hash distinctly");
    }

    public void testNilKeyFailsLoudly() {
        try {
            MapKeyHash.hash(new GoPointer((Object) null), 0L);
        } catch (FrontendException e) {
            return;
        }
        throw new AssertionError("nil key pointer must fail loudly, not hash silently");
    }
}
