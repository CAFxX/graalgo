package graalgo.go.values;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import graalgo.go.runtime.Values;
import graalgo.go.types.GoType;
import java.util.Arrays;
import java.util.concurrent.ThreadLocalRandom;

/**
 * A Go map: a hand-written open-addressing index over insertion-ordered entries (SPEC §3.4 —
 * the runtime owns the iterator). Keys of integer and boolean types are stored unboxed in
 * {@code kprims} and found by {@link #find(long)}, a plain probe loop that partial evaluation
 * can inline; every other key kind is stored in carrier form in {@code krefs} and compared with
 * Go's {@code ==}: by the site's key-kind node ({@code MapKeys.KeyOps}, PE-visible) through
 * {@link #findRaw}/{@link #putRaw}, or by the generic {@link Values} code behind a boundary
 * (reflection). Values are stored flat, {@code vp} scalar, {@code vr}
 * reference and {@code vb} byte slots per entry, so no value is boxed. The nil map is Java {@code null}.
 *
 * <p>Deleted entries leave a tombstone that is squeezed out when the table next grows; a
 * {@link Epoch} chain lets live iterators translate their positions across compactions, which
 * gives Go's guarantees (an entry is produced at most once, a deleted one never).
 */
public final class GoMap {
    /**
     * Go {@code ==} on keys in carrier form, consistent with the hash the caller passes to
     * {@link #findRaw}: a site's key-kind node; {@code null} means the generic {@link Values#equal}.
     */
    public interface KeyEq {
        boolean eq(Object stored, Object k);
    }

    /** One compaction: where each old entry index went. */
    static final class Epoch {
        int[] remap;
        Epoch next;
    }

    public final GoType keyType;
    public final GoType valType;
    public final int vp;
    public final int vr;
    public final int vb;
    private final boolean primKeys;
    private final Object[] zeroVal;

    private int[] index;
    private int[] hashes;
    private boolean[] live;
    public long[] kprims;
    public Object[] krefs;
    public long[] vprims;
    public Object[] vrefs;
    public byte[] vbytes;
    /** The value slots as one object: entry {@code e}'s value is at ({@code e*vp}, {@code e*vr}, {@code e*vb}). */
    public Flat vals;
    private int used;
    private int count;
    private Epoch epoch = new Epoch();
    /**
     * The per-map hash seed (gc's {@code h.hash0}, F-9.3): a random odd multiplier. The slot of
     * a key is the top bits of {@code key * seed} (Dietzfelbinger's multiply-shift, a universal
     * family: keys found colliding in one map do not collide in another), and, like Fibonacci
     * hashing, it spreads arithmetic key sequences without clustering the linear probes.
     */
    private final long seed;
    /** {@code 32 - log2(index.length)}: a slot is the top bits of the 32-bit stored hash. */
    private int shift;
    /** The state of the range-start generator: benign races (concurrent ranges only read entries). */
    private long rng;

    public GoMap(GoType keyType, GoType valType, int hint) {
        this(keyType, valType, hint, ThreadLocalRandom.current().nextLong());
    }

    private GoMap(GoType keyType, GoType valType, int hint, long seed) {
        this.seed = seed | 1;
        this.rng = seed * 0xD1342543DE82EF95L + 1;
        this.keyType = keyType;
        this.valType = valType;
        this.vp = valType.nPrim();
        this.vr = valType.nRef();
        this.vb = valType.nByte();
        int c = keyType.carrier();
        this.primKeys = c == GoType.C_I64 || c == GoType.C_BOOL;
        this.zeroVal = valType.zeroRefsAreNull() ? null : valType.zeroRefs();
        int cap = 8;
        while (cap < hint && cap < (1 << 28)) {
            cap <<= 1;
        }
        allocate(cap);
    }

    private void allocate(int cap) {
        index = new int[cap * 2];
        shift = Integer.numberOfLeadingZeros(cap * 2) + 1;
        hashes = new int[cap];
        live = new boolean[cap];
        kprims = primKeys ? new long[cap] : Flat.NO_PRIMS;
        krefs = primKeys ? Flat.NO_REFS : new Object[cap];
        vprims = vp == 0 ? Flat.NO_PRIMS : new long[cap * vp];
        vrefs = vr == 0 ? Flat.NO_REFS : new Object[cap * vr];
        vbytes = vb == 0 ? Flat.NO_BYTES : new byte[cap * vb];
        vals = new Flat(null, vprims, vrefs, vbytes);
    }

    public int len() {
        return count;
    }

    /**
     * The seeded hash of a 64-bit key or of a key's raw hash: the high half of the product with
     * the map's multiplier; its top bits pick the slot ({@link #shift}).
     */
    private int scramble(long k) {
        return (int) ((k * seed) >>> 32);
    }

    /** The entry of an integer/boolean key, or -1. */
    public int find(long k) {
        int[] idx = index;
        int mask = idx.length - 1;
        for (int i = scramble(k) >>> shift;; i = (i + 1) & mask) {
            int e = idx[i];
            if (e == 0) {
                return -1;
            }
            if (e > 0 && kprims[e - 1] == k) {
                return e - 1;
            }
        }
    }

    /**
     * The entry of a key in carrier form, or -1: the generic path (reflection), hashing and
     * comparing with {@link Values}. Throws {@link Values.Uncomparable} for an unhashable key.
     */
    @TruffleBoundary
    public int find(Object k) {
        return findRaw(Values.hash(keyType, k), k, null);
    }

    /**
     * The entry of {@code k} whose raw hash ({@code Values.hash} of the key type, computed by the
     * caller's key node) is {@code raw}, or -1. PE-visible: with {@code eq} a constant node, the
     * probe loop compiles to hash compares and the inlined key equality.
     */
    public int findRaw(int raw, Object k, KeyEq eq) {
        int h = scramble(raw);
        int[] idx = index;
        int[] hs = hashes;
        Object[] ks = krefs;
        int mask = idx.length - 1;
        for (int i = h >>> shift;; i = (i + 1) & mask) {
            int e = idx[i];
            if (e == 0) {
                return -1;
            }
            if (e > 0 && hs[e - 1] == h && (eq == null ? Values.equal(keyType, ks[e - 1], k) : eq.eq(ks[e - 1], k))) {
                return e - 1;
            }
        }
    }

    /** The entry of {@code k}, inserted with a zero value if absent. */
    public int put(long k) {
        int e = find(k);
        return e >= 0 ? e : insert(scramble(k), k, null);
    }

    /** As {@link #put(long)} for a key in carrier form (a struct/array key must be a private copy): the generic path. */
    @TruffleBoundary
    public int put(Object k) {
        return putRaw(Values.hash(keyType, k), k, null);
    }

    /** As {@link #put(Object)} with the caller's raw hash and equality ({@link #findRaw}). */
    public int putRaw(int raw, Object k, KeyEq eq) {
        int e = findRaw(raw, k, eq);
        if (e >= 0) {
            // gc's NeedKeyUpdate: assigning to an existing entry stores the new key, which an
            // equal key can tell apart (+0/-0, and through interfaces and struct fields). For
            // every other key type the two are identical, so storing it unconditionally is gc.
            krefs[e] = k;
            return e;
        }
        return insert(scramble(raw), 0, k);
    }

    private int insert(int h, long pk, Object rk) {
        if (used == hashes.length) {
            grow();
        }
        int e = used++;
        hashes[e] = h;
        live[e] = true;
        if (primKeys) {
            kprims[e] = pk;
        } else {
            krefs[e] = rk;
        }
        if (zeroVal != null) {
            System.arraycopy(zeroVal, 0, vrefs, e * vr, vr);
        }
        link(e, h);
        count++;
        return e;
    }

    private void link(int e, int h) {
        int mask = index.length - 1;
        int i = h >>> shift;
        while (index[i] > 0) {
            i = (i + 1) & mask;
        }
        index[i] = e + 1;
    }

    @TruffleBoundary
    private void grow() {
        int oldUsed = used;
        int[] oh = hashes;
        boolean[] ol = live;
        long[] okp = kprims;
        Object[] okr = krefs;
        long[] ovp = vprims;
        Object[] ovr = vrefs;
        byte[] ovb = vbytes;
        allocate(count * 2 > oh.length ? oh.length * 2 : oh.length);
        int[] remap = new int[oldUsed + 1];
        int n = 0;
        for (int e = 0; e < oldUsed; e++) {
            remap[e] = n;
            if (!ol[e]) {
                continue;
            }
            hashes[n] = oh[e];
            live[n] = true;
            if (primKeys) {
                kprims[n] = okp[e];
            } else {
                krefs[n] = okr[e];
            }
            System.arraycopy(ovp, e * vp, vprims, n * vp, vp);
            System.arraycopy(ovr, e * vr, vrefs, n * vr, vr);
            System.arraycopy(ovb, e * vb, vbytes, n * vb, vb);
            link(n, oh[e]);
            n++;
        }
        remap[oldUsed] = n;
        used = n;
        advanceEpoch(remap);
    }

    private void advanceEpoch(int[] remap) {
        Epoch next = new Epoch();
        epoch.remap = remap;
        epoch.next = next;
        epoch = next;
    }

    /** Removes entry {@code e} (from {@link #find}); a negative entry is a no-op. */
    public void delete(int e) {
        if (e < 0) {
            return;
        }
        int mask = index.length - 1;
        int i = hashes[e] >>> shift;
        while (index[i] != e + 1) {
            i = (i + 1) & mask;
        }
        index[i] = -1;
        live[e] = false;
        if (!primKeys) {
            krefs[e] = null;
        }
        Arrays.fill(vprims, e * vp, e * vp + vp, 0);
        Arrays.fill(vrefs, e * vr, e * vr + vr, null);
        Arrays.fill(vbytes, e * vb, e * vb + vb, (byte) 0);
        count--;
    }

    @TruffleBoundary
    public void clear() {
        int[] remap = new int[used + 1];
        allocate(8);
        used = 0;
        count = 0;
        advanceEpoch(remap);
    }

    /**
     * A new map with the same entries: each key and value copied as by assignment (the
     * runtime's map clone, reached from {@code maps.Clone}).
     */
    @TruffleBoundary
    public GoMap copy() {
        GoMap c = new GoMap(keyType, valType, count, seed);
        for (int e = 0; e < used; e++) {
            if (!live[e]) {
                continue;
            }
            int n = primKeys ? c.insert(hashes[e], kprims[e], null) : c.insert(hashes[e], 0, krefs[e]);
            System.arraycopy(vprims, e * vp, c.vprims, n * vp, vp);
            System.arraycopy(vrefs, e * vr, c.vrefs, n * vr, vr);
            System.arraycopy(vbytes, e * vb, c.vbytes, n * vb, vb);
        }
        return c;
    }

    /** The key of entry {@code e} in carrier form, boxed. */
    public Object keyAt(int e) {
        return primKeys ? (Object) kprims[e] : krefs[e];
    }

    /**
     * A random entry index below {@link #used} (> 0) where a range starts: an LCG step per
     * range (the high bits), so iteration order stays unpredictable without a boundary call.
     */
    int rangeStart() {
        long r = rng * 0x5851F42D4C957F2DL + 0x14057B7EF767814FL;
        rng = r;
        return (int) (((r >>> 33) * used) >>> 31);
    }

    /** A range iterator: random start, each live entry at most once. */
    public static final class Iter {
        private final GoMap m;
        private Epoch epoch;
        private int start;
        private int pos;
        private boolean wrapped;

        public Iter(GoMap m) {
            this.m = m;
            if (m != null) {
                epoch = m.epoch;
                start = m.used == 0 ? 0 : m.rangeStart();
                pos = start;
            }
        }

        /** The next live entry, or -1 at the end. */
        public int next() {
            if (m == null) {
                return -1;
            }
            while (epoch.next != null) {
                start = epoch.remap[Math.min(start, epoch.remap.length - 1)];
                pos = epoch.remap[Math.min(pos, epoch.remap.length - 1)];
                epoch = epoch.next;
            }
            for (;;) {
                if (!wrapped) {
                    if (pos >= m.used) {
                        wrapped = true;
                        pos = 0;
                        continue;
                    }
                } else if (pos >= start || pos >= m.used) {
                    return -1;
                }
                int e = pos++;
                if (m.live[e]) {
                    return e;
                }
            }
        }
    }
}
