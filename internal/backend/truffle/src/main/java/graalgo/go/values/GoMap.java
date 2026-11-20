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
 * Go's {@code ==} behind a boundary. Values are stored flat, {@code vp} scalar, {@code vr}
 * reference and {@code vb} byte slots per entry, so no value is boxed. The nil map is Java {@code null}.
 *
 * <p>Deleted entries leave a tombstone that is squeezed out when the table next grows; a
 * {@link Epoch} chain lets live iterators translate their positions across compactions, which
 * gives Go's guarantees (an entry is produced at most once, a deleted one never).
 */
public final class GoMap {
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

    public GoMap(GoType keyType, GoType valType, int hint) {
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

    private static int mix(long k) {
        return (int) ((k * 0x9E3779B97F4A7C15L) >>> 32);
    }

    /** The entry of an integer/boolean key, or -1. */
    public int find(long k) {
        int[] idx = index;
        int mask = idx.length - 1;
        for (int i = mix(k) & mask;; i = (i + 1) & mask) {
            int e = idx[i];
            if (e == 0) {
                return -1;
            }
            if (e > 0 && kprims[e - 1] == k) {
                return e - 1;
            }
        }
    }

    /** The entry of a key in carrier form, or -1. Panics for an uncomparable dynamic type. */
    @TruffleBoundary
    public int find(Object k) {
        int h = Values.hash(keyType, k);
        int[] idx = index;
        int mask = idx.length - 1;
        for (int i = h & mask;; i = (i + 1) & mask) {
            int e = idx[i];
            if (e == 0) {
                return -1;
            }
            if (e > 0 && hashes[e - 1] == h && Values.equal(keyType, krefs[e - 1], k)) {
                return e - 1;
            }
        }
    }

    /** The entry of {@code k}, inserted with a zero value if absent. */
    public int put(long k) {
        int e = find(k);
        return e >= 0 ? e : insert(mix(k), k, null);
    }

    /** As {@link #put(long)} for a key in carrier form (a struct/array key must be a private copy). */
    @TruffleBoundary
    public int put(Object k) {
        int e = find(k);
        if (e >= 0) {
            // gc's NeedKeyUpdate: assigning to an existing entry stores the new key, which an
            // equal key can tell apart (+0/-0, and through interfaces and struct fields). For
            // every other key type the two are identical, so storing it unconditionally is gc.
            krefs[e] = k;
            return e;
        }
        return insert(Values.hash(keyType, k), 0, k);
    }

    @TruffleBoundary
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
        int i = h & mask;
        while (index[i] > 0) {
            i = (i + 1) & mask;
        }
        index[i] = e + 1;
    }

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
    @TruffleBoundary
    public void delete(int e) {
        if (e < 0) {
            return;
        }
        int mask = index.length - 1;
        int i = hashes[e] & mask;
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
        GoMap c = new GoMap(keyType, valType, count);
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

    /** A range iterator: random start, each live entry at most once. */
    public static final class Iter {
        private final GoMap m;
        private Epoch epoch;
        private int start;
        private int pos;
        private boolean wrapped;

        @TruffleBoundary
        public Iter(GoMap m) {
            this.m = m;
            if (m != null) {
                epoch = m.epoch;
                start = m.used == 0 ? 0 : ThreadLocalRandom.current().nextInt(m.used);
                pos = start;
            }
        }

        /** The next live entry, or -1 at the end. */
        @TruffleBoundary
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
