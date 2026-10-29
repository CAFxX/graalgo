package graalgo.p3reflect;

import com.oracle.truffle.api.RootCallTarget;
import graalgo.frontend.FrontendException;
import graalgo.frontend.LinknameRootNode;
import graalgo.p3unsafe.UnsafeTable;
import graalgo.runtime.GoChan;
import graalgo.values.GoBox;
import graalgo.values.GoCell;
import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoFunc;
import graalgo.values.GoInterface;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * The {@code internal/abi.MapType.Hasher} function value
 * (fix-redmaster-r42): gc generates one hash function per map key type
 * and stores it in the map type; {@code internal/sync.HashTrieMap}
 * (which backs {@code sync.Map}) calls it unconditionally on every
 * Load/Store/Swap/Delete. graalgo's {@code AbiTypes} map extension left
 * Hasher zero, so the first sync.Map operation died calling a nil func
 * value — a guest nil-dereference panic, not a HashTrieMap bug.
 *
 * <p>This fills that gap with one shared, type-agnostic hash function:
 * a {@link GoFunc} over the {@code graalgo.mapkeyhash} linkname
 * intrinsic (registered by {@code LinknameIntrinsics}), callable
 * through the ordinary func-value path. The hash is a pure function
 * of the key's <em>value</em> (never of a Java object's identity,
 * except where the value itself is a reference — channels — or a
 * pointer, where storage identity <em>is</em> the value):
 *
 * <ul>
 *   <li>scalars hash by their bits, strings by their bytes
 *       (FNV-1a 64), structs and arrays by combining their fields /
 *       elements, interfaces by (canonical descriptor, data hash);</li>
 *   <li>pointer keys hash by a storage-keyed 64-bit sequence
 *       (see {@link #pointerHash}), which canonicalizes storage
 *       identity — the same storage always yields the same hash,
 *       distinct storages distinct hashes;</li>
 *   <li>the per-map seed is mixed in with a SplitMix64 finalizer, so
 *       distinct maps distribute independently, as in gc.</li>
 * </ul>
 *
 * <p>64 bits matter here, unlike for native maps: HashTrieMap
 * navigates by hash chunks and panics ("ran out of hash bits") when
 * two distinct keys share a full hash, so a 32-bit hash (as in
 * {@code GoEquality}) would collide after ~65k keys. The hash values
 * themselves are never observable — only trie navigation uses them —
 * so they need not match gc's memhash output, only be deterministic
 * per (key, seed) and well-distributed.
 *
 * <p>Unhashable shapes (slices, maps as keys) are rejected by
 * go/types's comparable constraint and cannot reach this function
 * through typed code; they fail loudly here rather than hashing
 * silently wrong.
 */
public final class MapKeyHash {
    private MapKeyHash() {}

    /** Symbol of the linkname intrinsic implementing the hash. */
    public static final String SYMBOL = "graalgo.mapkeyhash";

    /** The shared Hasher func value for every map type's shell. */
    private static volatile GoFunc func;

    /** Returns the shared Hasher func value, creating it on first use. */
    public static GoFunc func() {
        GoFunc f = func;
        if (f == null) {
            synchronized (MapKeyHash.class) {
                f = func;
                if (f == null) {
                    RootCallTarget target =
                            new LinknameRootNode(SYMBOL, SYMBOL, false).getCallTarget();
                    f = new GoFunc(target, null, null);
                    func = f;
                }
            }
        }
        return f;
    }

    /**
     * The intrinsic body: {@code hash(unsafe.Pointer(&key), seed)}.
     * The pointer arrives as a D5 unsafe.Pointer value — a Long handle
     * (or, defensively, a GoPointer) addressing the key's storage.
     * Exactly one storage level is unwrapped to obtain the key value;
     * a pointer key's value is the pointer itself, never its pointee.
     */
    public static long hash(Object keyPtr, long seed) {
        Object value = resolveKeyValue(keyPtr);
        return mix64(hash64(value, 0) ^ seed);
    }

    /**
     * Storage-stable 64-bit hashes for pointer keys
     * (fix-redmaster-r43): the same storage always yields the same
     * sequence, distinct storages distinct sequences. Entries are
     * never removed, matching intern. Concurrent: sync.Map is a
     * concurrent structure, so the table is a ConcurrentHashMap and
     * sequences come from an AtomicLong (computeIfAbsent may skip
     * values under contention; uniqueness, not contiguity, matters).
     */
    private static final java.util.concurrent.ConcurrentHashMap<Object, Long> POINTER_HASHES =
            new java.util.concurrent.ConcurrentHashMap<>();
    private static final java.util.concurrent.atomic.AtomicLong NEXT_POINTER_HASH =
            new java.util.concurrent.atomic.AtomicLong(1);

    private static long pointerHash(GoPointer gp) {
        if (gp.isNil()) {
            return 0;
        }
        return POINTER_HASHES.computeIfAbsent(gp.storageKey(),
                k -> NEXT_POINTER_HASH.getAndIncrement());
    }

    /** Resolves the key value from the pointer addressing its storage. */
    private static Object resolveKeyValue(Object keyPtr) {
        Object storage;
        if (keyPtr instanceof Number n) {
            long handle = n.longValue();
            if (handle == 0) {
                throw new FrontendException("mapkeyhash: nil key pointer");
            }
            storage = UnsafeTable.current().resolve(handle);
            if (storage == null) {
                throw new FrontendException("mapkeyhash: unknown key handle " + handle);
            }
        } else if (keyPtr instanceof GoPointer gp) {
            if (gp.isNil()) {
                throw new FrontendException("mapkeyhash: nil key pointer");
            }
            storage = gp.referent();
        } else {
            throw new FrontendException("mapkeyhash: key pointer is neither a handle nor a pointer: "
                    + (keyPtr == null ? "null" : keyPtr.getClass().getName()));
        }
        // Unwrap storage wrappers (cells, array elements, zero-offset
        // byte views) to the stored value. A ByteRef with a nonzero
        // offset would need the element size to resolve, which this
        // type-agnostic path does not have; HashTrieMap always passes
        // &key at offset 0, so that shape fails loudly instead of
        // hashing the wrong bytes.
        while (true) {
            if (storage instanceof GoCell cell) {
                storage = cell.get();
            } else if (storage instanceof UnsafeTable.ArrayRef ref) {
                storage = java.lang.reflect.Array.get(ref.array(), ref.offset());
            } else if (storage instanceof UnsafeTable.ByteRef br) {
                if (br.byteOffset() != 0) {
                    throw new FrontendException(
                            "mapkeyhash: nonzero byte-offset key pointer is unresolvable");
                }
                storage = br.base();
            } else {
                return storage;
            }
        }
    }

    /** 64-bit value hash; depth guards against pathological value cycles. */
    private static long hash64(Object v, int depth) {
        if (depth > 256) {
            throw new FrontendException("mapkeyhash: value cycle while hashing");
        }
        if (v == null) {
            return 0;
        }
        if (v instanceof Long l) {
            return mix64(l.longValue());
        }
        if (v instanceof Integer i) {
            return mix64(i.longValue());
        }
        if (v instanceof Double d) {
            return mix64(Double.doubleToRawLongBits(d.doubleValue()));
        }
        if (v instanceof Float f) {
            return mix64(Float.floatToRawIntBits(f.floatValue()));
        }
        if (v instanceof Boolean b) {
            return b.booleanValue() ? 0x9E3779B97F4A7C15L : 0xC2B2AE3D27D4EB4FL;
        }
        if (v instanceof GoString s) {
            // FNV-1a 64 over the string bytes.
            long h = 0xCBF29CE484222325L;
            for (byte b : s.bytes()) {
                h ^= (b & 0xFFL);
                h *= 0x100000001B3L;
            }
            return mix64(h);
        }
        if (v instanceof GoStruct gs) {
            // The descriptor is memoized per type within a run, so its
            // identity is a stable type tag; fields are combined in
            // declaration order (longs by bits, objects recursively).
            long h = mix64(System.identityHashCode(gs.descriptor()));
            GoStructDescriptor desc = gs.descriptor();
            for (int i = 0; i < desc.fields().size(); i++) {
                GoStructDescriptor.Field f = desc.field(i);
                long fh = f.inLongs() ? mix64(gs.getLong(i)) : hash64(gs.getObject(i), depth + 1);
                h = combine(h, fh);
            }
            return h;
        }
        if (v instanceof GoInterface gi) {
            if (gi.isNil()) {
                return 0;
            }
            return combine(mix64(System.identityHashCode(gi.descriptor())),
                    hash64(gi.data(), depth + 1));
        }
        if (v instanceof GoPointer gp) {
            // Pointer keys: storage identity is the value. The
            // hash is a storage-keyed 64-bit sequence (stable
            // across re-evaluations of the same storage, unique
            // per storage) — NOT the intern handle, which differs
            // per fresh cell object (so a sync.Map Load after
            // Store on &s.X missed), and NOT a 32-bit storage
            // hash, whose collisions would exhaust the trie's hash
            // bits and panic. Entries are never removed, matching
            // intern. Agrees with GoEquality's pointer ==, which
            // is what entry lookup compares with.
            return mix64(pointerHash(gp));
        }
        if (v instanceof GoChan chan) {
            // Channels are references: the object is the value.
            return mix64(System.identityHashCode(chan));
        }
        if (v instanceof GoBox box) {
            return hash64(box.get(), depth + 1);
        }
        if (v instanceof GoCell cell) {
            return hash64(cell.get(), depth + 1);
        }
        if (v instanceof java.util.concurrent.atomic.AtomicReference<?> ar) {
            return hash64(ar.get(), depth + 1);
        }
        if (v instanceof graalgo.runtime.sync.GoAtomicInt64 gi64) {
            return mix64(gi64.get());
        }
        if (v instanceof graalgo.runtime.sync.GoAtomicInt32 gi32) {
            return mix64(gi32.get());
        }
        if (v instanceof GoComplex32 c) {
            return combine(mix64(Float.floatToRawIntBits(c.re())),
                    mix64(Float.floatToRawIntBits(c.im())));
        }
        if (v instanceof GoComplex64 c) {
            return combine(mix64(Double.doubleToRawLongBits(c.re())),
                    mix64(Double.doubleToRawLongBits(c.im())));
        }
        if (v instanceof GoSlice || v instanceof graalgo.values.GoMap) {
            throw new FrontendException(
                    "mapkeyhash: unhashable key shape " + v.getClass().getName());
        }
        if (v != null && v.getClass().isArray()) {
            int n = java.lang.reflect.Array.getLength(v);
            long h = mix64(n);
            for (int i = 0; i < n; i++) {
                h = combine(h, hash64(java.lang.reflect.Array.get(v, i), depth + 1));
            }
            return h;
        }
        // Anything else (defensive): identity is the only available
        // tag, and reaching here means an uncovered comparable shape.
        throw new FrontendException(
                "mapkeyhash: uncovered key shape " + v.getClass().getName());
    }

    /** Boost hash_combine, 64-bit. */
    private static long combine(long h, long v) {
        return h ^ (v + 0x9E3779B97F4A7C15L + (h << 6) + (h >>> 2));
    }

    /** SplitMix64 finalizer (Stafford mix13). */
    private static long mix64(long x) {
        long z = x + 0x9E3779B97F4A7C15L;
        z = (z ^ (z >>> 30)) * 0xBF58476D1CE4E5B9L;
        z = (z ^ (z >>> 27)) * 0x94D049BB133111EBL;
        return z ^ (z >>> 31);
    }
}
