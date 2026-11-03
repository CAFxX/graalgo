package graalgo.p3reflect;

import graalgo.frontend.FrontendException;
import graalgo.runtime.GoChan;
import graalgo.values.GoBox;
import graalgo.values.GoPointer;
import graalgo.values.GoString;

import java.nio.charset.StandardCharsets;
import java.util.HashMap;
import java.util.HashSet;

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
 * instead — see DESIGN.md. Channel keys, by contrast, ARE unit-testable
 * here: {@code resolveKeyValue}'s {@code GoPointer} branch returns the
 * referent without touching {@code UnsafeTable}, so a channel wrapped
 * in a {@code GoPointer} hashes exactly as the sync.Map path hashes it.
 *
 * <p>Channel falsifiability (REVIEW-2026-11-03-22 NEW-P1-1): the
 * channel test below uses 200,000 distinct channels. Under a 32-bit
 * hash (the old {@code System.identityHashCode} branch) the birthday
 * collision probability at that sample size is
 * 1 - exp(-200000^2 / 2^33) &approx; 99%, so reverting the fix fails
 * the test with ~99% probability on any run; under the 64-bit
 * sequence the collision probability is 200000^2 / 2^65 &approx;
 * 10^-9, so the test does not flake on the fixed code. The
 * hash-keyed-store test asserts the exact invariant HashTrieMap
 * needs: distinct keys must never share a full hash, or the trie
 * panics with "ran out of hash bits".
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

    private static GoPointer chanKey(GoChan chan) {
        return new GoPointer(chan);
    }

    /**
     * REVIEW-2026-11-03-22 NEW-P1-1: distinct channels hash distinctly
     * and stably. A channel's identity is its whole value, so this is
     * the case the old 32-bit {@code System.identityHashCode} branch
     * got wrong: two live channels sharing an identity hash shared a
     * full sync.Map hash, and HashTrieMap panics on that. The sample
     * size is chosen from the birthday bound (see the class doc):
     * 200,000 channels collide with ~99% probability under any 32-bit
     * hash, so this test catches a revert, and with ~10^-9 probability
     * under the 64-bit sequence, so it does not flake on fixed code.
     * Re-hashing every channel asserts stability (the sequence map
     * must return the minted value, never a fresh one), and the second
     * seed asserts that seed mixing preserves distinctness — the seed
     * is a bijection on the 64-bit hash, so it cannot create or cure
     * a collision, only relocate it.
     */
    public void testChannelKeysDistinctAndStable() {
        final int n = 200_000;
        GoChan[] chans = new GoChan[n];
        long[] first = new long[n];
        HashSet<Long> seen = new HashSet<>(n * 2);
        for (int i = 0; i < n; i++) {
            chans[i] = new GoChan(0, null);
            long h = MapKeyHash.hash(chanKey(chans[i]), 0L);
            first[i] = h;
            assertTrue(seen.add(h), "distinct channels must hash distinctly (collision at index " + i + ")");
        }
        assertTrue(seen.size() == n, "all channel hashes distinct, got " + seen.size() + " of " + n);
        HashSet<Long> seenSeeded = new HashSet<>(n * 2);
        for (int i = 0; i < n; i++) {
            long h = MapKeyHash.hash(chanKey(chans[i]), 0L);
            assertTrue(h == first[i], "same channel + seed hashes identically on re-hash (index " + i + ")");
            long hs = MapKeyHash.hash(chanKey(chans[i]), 1L);
            assertTrue(hs != h, "different seed must mix differently for a channel key (index " + i + ")");
            assertTrue(seenSeeded.add(hs), "distinct channels must stay distinct under a second seed (index " + i + ")");
        }
        assertTrue(seenSeeded.size() == n, "all seeded channel hashes distinct, got " + seenSeeded.size() + " of " + n);
    }

    /**
     * REVIEW-2026-11-03-22 NEW-P1-1, sync.Map side: many distinct
     * channel keys stored in a store keyed by their full
     * {@code MapKeyHash} hash — the exact shape HashTrieMap consumes.
     * (The guest sync.Map engine itself lives in guest Go code and
     * cannot be driven from this JVM unit test; what this test pins
     * is the invariant its panic depends on: no two distinct channel
     * keys share a full hash, and re-hashing a stored key finds its
     * own entry, never another channel's.) A full-hash collision here
     * is the trie panic, not a longer bucket chain.
     */
    public void testManyChannelKeysInHashKeyedStore() {
        final int n = 20_000;
        HashMap<Long, GoChan> store = new HashMap<>(n * 2);
        GoChan[] chans = new GoChan[n];
        for (int i = 0; i < n; i++) {
            chans[i] = new GoChan(0, null);
            long h = MapKeyHash.hash(chanKey(chans[i]), 0L);
            GoChan prev = store.put(h, chans[i]);
            assertTrue(prev == null, "storing a distinct channel key must not overwrite another key (index " + i + ")");
        }
        assertTrue(store.size() == n, "store holds all channel keys, got " + store.size() + " of " + n);
        for (int i = 0; i < n; i++) {
            long h = MapKeyHash.hash(chanKey(chans[i]), 0L);
            assertTrue(store.get(h) == chans[i], "re-hashed channel key finds its own entry (index " + i + ")");
        }
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
