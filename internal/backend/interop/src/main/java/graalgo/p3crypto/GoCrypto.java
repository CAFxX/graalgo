package graalgo.p3crypto;

import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicLong;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

import graalgo.values.GoSlice;

/**
 * JDK-delegating cryptographic intrinsics (P3-M9, user directive 2026-10-10:
 * "prefer delegating to Java libraries when possible").
 *
 * <p>Since graalgo runs on GraalVM/Truffle with full JVM access, hash
 * functions, HMAC, and other crypto primitives delegate to the JDK's
 * battle-tested implementations ({@link java.security.MessageDigest},
 * {@link javax.crypto.Mac}) instead of pure-Go reimplementations. This
 * gives us correctness and performance for free.
 *
 * <p>Two APIs:
 * <ul>
 *   <li><b>One-shot:</b> {@code sum(algorithm, data)} — for Go's
 *       {@code sha256.Sum256} etc.
 *   <li><b>Stateful:</b> handle-based ({@code new_}, {@code write},
 *       {@code sum}, {@code reset}, {@code free}) — for Go's
 *       {@code hash.Hash} interface. Handles are long IDs into a
 *       {@link ConcurrentHashMap}; the Go side holds the handle in the
 *       digest struct.
 * </ul>
 *
 * <p>D5 boxing: Go {@code []byte} arrives as {@link GoSlice} with
 * {@code int[]} backing (bytes as ints 0-255). Results are returned as
 * new {@link GoSlice} with {@code int[]} backing.
 *
 * <p>verifies: GG-0020 (intrinsic surface completion)
 */
public final class GoCrypto {
    private GoCrypto() {}

    // ------------------------------------------------------------------
    // Stateful digest handles.
    // ------------------------------------------------------------------

    private static final ConcurrentHashMap<Long, MessageDigest> DIGESTS =
            new ConcurrentHashMap<>();
    private static final AtomicLong NEXT_DIGEST_ID = new AtomicLong(1);

    private static final ConcurrentHashMap<Long, Mac> MACS =
            new ConcurrentHashMap<>();
    private static final AtomicLong NEXT_MAC_ID = new AtomicLong(1);

    /**
     * Creates a new MessageDigest for the given JDK algorithm name
     * (e.g. "SHA-256", "SHA-512", "SHA-1", "MD5", "SHA3-256").
     * Returns a handle (long) for use with digestWrite/digestSum/etc.
     */
    public static long digestNew(String algorithm) {
        try {
            MessageDigest md = MessageDigest.getInstance(algorithm);
            long id = NEXT_DIGEST_ID.getAndIncrement();
            DIGESTS.put(id, md);
            return id;
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalArgumentException(
                    "GoCrypto.digestNew: unknown algorithm " + algorithm, e);
        }
    }

    /** Writes Go []byte data into the digest. Returns bytes written. */
    public static long digestWrite(long id, Object dataArg) {
        MessageDigest md = DIGESTS.get(id);
        if (md == null) {
            throw new IllegalArgumentException(
                    "GoCrypto.digestWrite: unknown digest handle " + id);
        }
        byte[] data = goBytes(dataArg);
        md.update(data);
        return data.length;
    }

    /** Returns the current digest as a new Go []byte (does not reset). */
    public static Object digestSum(long id) {
        MessageDigest md = DIGESTS.get(id);
        if (md == null) {
            throw new IllegalArgumentException(
                    "GoCrypto.digestSum: unknown digest handle " + id);
        }
        try {
            MessageDigest clone = (MessageDigest) md.clone();
            return goSlice(clone.digest());
        } catch (CloneNotSupportedException e) {
            throw new IllegalStateException(
                    "GoCrypto.digestSum: MessageDigest not cloneable", e);
        }
    }

    /** Resets the digest to its initial state. */
    public static void digestReset(long id) {
        MessageDigest md = DIGESTS.get(id);
        if (md == null) {
            throw new IllegalArgumentException(
                    "GoCrypto.digestReset: unknown digest handle " + id);
        }
        md.reset();
    }

    /** Releases the digest handle. */
    public static void digestFree(long id) {
        DIGESTS.remove(id);
    }

    /** Returns the digest size in bytes for the algorithm. */
    public static long digestSize(String algorithm) {
        try {
            return MessageDigest.getInstance(algorithm).getDigestLength();
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalArgumentException(
                    "GoCrypto.digestSize: unknown algorithm " + algorithm, e);
        }
    }

    // ------------------------------------------------------------------
    // One-shot hashing.
    // ------------------------------------------------------------------

    /**
     * One-shot hash: {@code MessageDigest(algorithm).digest(data)}.
     * Returns the digest as a new Go []byte.
     */
    public static Object sum(String algorithm, Object dataArg) {
        try {
            MessageDigest md = MessageDigest.getInstance(algorithm);
            byte[] result = md.digest(goBytes(dataArg));
            return goSlice(result);
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalArgumentException(
                    "GoCrypto.sum: unknown algorithm " + algorithm, e);
        }
    }

    // ------------------------------------------------------------------
    // HMAC (javax.crypto.Mac).
    // ------------------------------------------------------------------

    /**
     * Creates a new HMAC with the given JDK algorithm (e.g. "HmacSHA256")
     * and key bytes. Returns a handle for use with macWrite/macSum/etc.
     */
    public static long macNew(String algorithm, Object keyArg) {
        try {
            Mac mac = Mac.getInstance(algorithm);
            byte[] key = goBytes(keyArg);
            mac.init(new SecretKeySpec(key, algorithm));
            long id = NEXT_MAC_ID.getAndIncrement();
            MACS.put(id, mac);
            return id;
        } catch (Exception e) {
            throw new IllegalArgumentException(
                    "GoCrypto.macNew: failed for algorithm " + algorithm, e);
        }
    }

    /** Writes Go []byte data into the MAC. Returns bytes written. */
    public static long macWrite(long id, Object dataArg) {
        Mac mac = MACS.get(id);
        if (mac == null) {
            throw new IllegalArgumentException(
                    "GoCrypto.macWrite: unknown MAC handle " + id);
        }
        byte[] data = goBytes(dataArg);
        mac.update(data);
        return data.length;
    }

    /** Returns the current MAC as a new Go []byte (does not reset). */
    public static Object macSum(long id) {
        Mac mac = MACS.get(id);
        if (mac == null) {
            throw new IllegalArgumentException(
                    "GoCrypto.macSum: unknown MAC handle " + id);
        }
        try {
            Mac clone = (Mac) mac.clone();
            return goSlice(clone.doFinal());
        } catch (CloneNotSupportedException e) {
            throw new IllegalStateException(
                    "GoCrypto.macSum: Mac not cloneable", e);
        }
    }

    /** Resets the MAC to its initial state. */
    public static void macReset(long id) {
        Mac mac = MACS.get(id);
        if (mac == null) {
            throw new IllegalArgumentException(
                    "GoCrypto.macReset: unknown MAC handle " + id);
        }
        mac.reset();
    }

    /** Releases the MAC handle. */
    public static void macFree(long id) {
        MACS.remove(id);
    }

    /** Returns the MAC size in bytes for the algorithm. */
    public static long macSize(String algorithm) {
        try {
            return Mac.getInstance(algorithm).getMacLength();
        } catch (Exception e) {
            throw new IllegalArgumentException(
                    "GoCrypto.macSize: unknown algorithm " + algorithm, e);
        }
    }

    // ------------------------------------------------------------------
    // D5 boxing helpers.
    // ------------------------------------------------------------------

    /**
     * Converts a Go []byte (GoSlice with int[] backing) to a Java byte[].
     * D5 stores []byte in int[] backing arrays (bytes as ints 0-255).
     */
    static byte[] goBytes(Object o) {
        if (o == null) {
            return new byte[0];
        }
        GoSlice gs = (GoSlice) o;
        Object arr = gs.array();
        int off = gs.offset();
        int len = gs.len();
        byte[] out = new byte[len];
        if (arr instanceof int[] ia) {
            for (int i = 0; i < len; i++) {
                out[i] = (byte) ia[off + i];
            }
        } else if (arr instanceof byte[] ba) {
            System.arraycopy(ba, off, out, 0, len);
        } else {
            throw new IllegalArgumentException(
                    "GoCrypto.goBytes: unexpected backing array type "
                    + arr.getClass());
        }
        return out;
    }

    /**
     * Converts a Java byte[] to a Go []byte (GoSlice with byte[] backing,
     * D5 canonical form).
     */
    static GoSlice goSlice(byte[] bytes) {
        byte[] arr = bytes.clone();
        return new GoSlice(arr, 0, arr.length, arr.length);
    }
}
