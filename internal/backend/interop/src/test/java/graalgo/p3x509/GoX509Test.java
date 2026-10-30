package graalgo.p3x509;

import java.io.ByteArrayOutputStream;
import java.math.BigInteger;
import java.security.KeyPair;
import java.security.KeyPairGenerator;
import java.security.interfaces.ECPrivateKey;
import java.security.interfaces.RSAPrivateCrtKey;
import java.security.spec.ECGenParameterSpec;

/**
 * Unit tests for {@link GoX509#parsePrivateKey} (R1-12 remediation).
 *
 * <p>Red-green discipline: the SEC1 scanner-trap test fails against the
 * old byte-scanning extractors (the embedded {@code 0xA0 0x06} pattern
 * inside the private scalar hijacks the curve-OID scan) and passes
 * against the DER walker; the trailing-garbage, unknown-curve and
 * zero-scalar tests fail against the old code's collapse of every
 * failure to {@code ""}. Key material for the round-trips comes from
 * the JDK generators, and expected descriptors are computed from the
 * JDK-parsed key objects — never from graalgo's own output.
 */
public final class GoX509Test {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(Object expected, Object actual, String msg) {
        boolean eq = expected == null ? actual == null : expected.equals(actual);
        if (!eq) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    /** parsePrivateKey's GoString result as a Java string. */
    private static String parse(byte[] der) {
        Object out = GoX509.parsePrivateKey(der);
        assertTrue(out instanceof graalgo.values.GoString,
                "parsePrivateKey returns a GoString, got " + out);
        return out.toString();
    }

    private static String hex(byte[] b) {
        StringBuilder sb = new StringBuilder(b.length * 2);
        for (byte v : b) {
            sb.append(Character.forDigit((v >> 4) & 0xF, 16));
            sb.append(Character.forDigit(v & 0xF, 16));
        }
        return sb.toString();
    }

    /** Strips a single leading zero byte (BigInteger sign byte). */
    private static byte[] stripZero(byte[] b) {
        if (b.length > 1 && b[0] == 0) {
            byte[] out = new byte[b.length - 1];
            System.arraycopy(b, 1, out, 0, out.length);
            return out;
        }
        return b;
    }

    // ---------- minimal DER writer (test-only) ----------

    private static byte[] lenBytes(int len) {
        if (len < 0x80) {
            return new byte[]{(byte) len};
        }
        if (len < 0x100) {
            return new byte[]{(byte) 0x81, (byte) len};
        }
        return new byte[]{(byte) 0x82, (byte) (len >> 8), (byte) len};
    }

    private static byte[] tlv(int tag, byte[] content) {
        byte[] len = lenBytes(content.length);
        byte[] out = new byte[1 + len.length + content.length];
        out[0] = (byte) tag;
        System.arraycopy(len, 0, out, 1, len.length);
        System.arraycopy(content, 0, out, 1 + len.length, content.length);
        return out;
    }

    private static byte[] concat(byte[]... parts) {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        for (byte[] p : parts) {
            out.write(p, 0, p.length);
        }
        return out.toByteArray();
    }

    private static byte[] integer(BigInteger v) {
        return tlv(0x02, v.toByteArray());
    }

    private static byte[] integerBytes(byte[] raw) {
        return tlv(0x02, raw);
    }

    private static byte[] oid(byte[] content) {
        return tlv(0x06, content);
    }

    /** OID 1.2.840.10045.3.1.7 (secp256r1) content octets. */
    private static final byte[] OID_P256 =
            {0x2A, (byte) 0x86, 0x48, (byte) 0xCE, 0x3D, 0x03, 0x01, 0x07};
    /** OID 1.3.132.0.10 (secp256k1) content octets. */
    private static final byte[] OID_P256K1 =
            {0x2B, (byte) 0x81, 0x04, 0x00, 0x0A};
    /** OID 1.2.840.10046.2.1 (DSA) content octets. */
    private static final byte[] OID_DSA =
            {0x2A, (byte) 0x86, 0x48, (byte) 0xCE, 0x38, 0x04, 0x01};

    /** SEC1 ECPrivateKey DER for scalar {@code d} and curve OID, no [1]. */
    private static byte[] sec1(byte[] d, byte[] curveOid) {
        return tlv(0x30, concat(
                integer(BigInteger.ONE),
                tlv(0x04, d),
                tlv(0xA0, oid(curveOid))));
    }

    // ---------- tests ----------

    /** JDK-generated EC key, PKCS#8 form: descriptor carries curve + scalar. */
    public void testPkcs8EcRoundTrip() throws Exception {
        KeyPairGenerator gen = KeyPairGenerator.getInstance("EC");
        gen.initialize(new ECGenParameterSpec("secp256r1"));
        KeyPair kp = gen.generateKeyPair();
        ECPrivateKey priv = (ECPrivateKey) kp.getPrivate();
        String got = parse(kp.getPrivate().getEncoded());
        assertEquals("PKCS8:EC:secp256r1:" + hex(stripZero(priv.getS().toByteArray())),
                got, "PKCS#8 EC descriptor");
    }

    /** JDK-generated EC key, SEC1 form built by the test DER writer. */
    public void testSec1RoundTrip() throws Exception {
        KeyPairGenerator gen = KeyPairGenerator.getInstance("EC");
        gen.initialize(new ECGenParameterSpec("secp256r1"));
        KeyPair kp = gen.generateKeyPair();
        ECPrivateKey priv = (ECPrivateKey) kp.getPrivate();
        byte[] d = stripZero(priv.getS().toByteArray());
        // Fixed 32-byte scalar form, as SEC1 encoders emit it.
        byte[] d32 = new byte[32];
        System.arraycopy(d, 0, d32, 32 - d.length, d.length);
        assertEquals("SEC1:EC:secp256r1:" + hex(d32), parse(sec1(d32, OID_P256)),
                "SEC1 descriptor (x, y derived Go-side, not in descriptor)");
    }

    /**
     * R1-12 scanner trap: the scalar itself contains {@code 0xA0 0x06}
     * bytes. The old curve-OID scanner took the first {@code 0xA0} then
     * the first {@code 0x06} after it — both inside the scalar — and
     * decoded a bogus OID (or no known curve). The DER walker reads the
     * scalar by length and the OID from the [0] parameters only.
     */
    public void testSec1ScalarContainingTagBytes() {
        byte[] d = new byte[32];
        d[0] = (byte) 0xA0;
        d[1] = 0x06;
        d[2] = 0x08;
        d[3] = 0x2A;
        d[31] = 0x01;
        assertEquals("SEC1:EC:secp256r1:" + hex(d), parse(sec1(d, OID_P256)),
                "scalar-embedded tag bytes must not hijack the OID walk");
    }

    /** Trailing element inside the SEC1 SEQUENCE is malformed, not ignored. */
    public void testSec1TrailingElementMalformed() {
        byte[] d = new byte[32];
        d[31] = 0x01;
        byte[] der = tlv(0x30, concat(
                integer(BigInteger.ONE),
                tlv(0x04, d),
                tlv(0xA0, oid(OID_P256)),
                integer(BigInteger.ZERO)));
        assertEquals("ERROR:malformed-sec1", parse(der), "trailing SEQUENCE element");
    }

    /** SEC1 with an unknown curve OID is unsupported, not unparseable. */
    public void testSec1UnknownCurve() {
        byte[] d = new byte[32];
        d[31] = 0x01;
        assertEquals("ERROR:unsupported-curve", parse(sec1(d, OID_P256K1)),
                "secp256k1 is not a named graalgo curve");
    }

    /** SEC1 without [0] parameters has no curve to name. */
    public void testSec1MissingCurveParameters() {
        byte[] d = new byte[32];
        d[31] = 0x01;
        byte[] der = tlv(0x30, concat(integer(BigInteger.ONE), tlv(0x04, d)));
        assertEquals("ERROR:missing-curve-parameters", parse(der), "no [0] parameters");
    }

    /** Zero scalar is not a private key (old code: infinity + swallowed NPE). */
    public void testSec1ZeroScalar() {
        assertEquals("ERROR:invalid-key", parse(sec1(new byte[32], OID_P256)),
                "zero scalar");
    }

    /** JDK-generated RSA key, PKCS#1 form built by the test DER writer. */
    public void testPkcs1RoundTrip() throws Exception {
        KeyPairGenerator gen = KeyPairGenerator.getInstance("RSA");
        gen.initialize(2048);
        RSAPrivateCrtKey priv = (RSAPrivateCrtKey) gen.generateKeyPair().getPrivate();
        byte[] der = tlv(0x30, concat(
                integer(BigInteger.ZERO),
                integer(priv.getModulus()),
                integer(priv.getPublicExponent()),
                integer(priv.getPrivateExponent()),
                integer(priv.getPrimeP()),
                integer(priv.getPrimeQ()),
                integer(priv.getPrimeExponentP()),
                integer(priv.getPrimeExponentQ()),
                integer(priv.getCrtCoefficient())));
        String want = "PKCS1:RSA:"
                + hex(stripZero(priv.getModulus().toByteArray())) + ":"
                + hex(stripZero(priv.getPublicExponent().toByteArray())) + ":"
                + hex(stripZero(priv.getPrivateExponent().toByteArray())) + ":"
                + hex(stripZero(priv.getPrimeP().toByteArray())) + ":"
                + hex(stripZero(priv.getPrimeQ().toByteArray()));
        assertEquals(want, parse(der), "PKCS#1 descriptor");
    }

    /** Integer SEQUENCE of the wrong arity is malformed PKCS#1. */
    public void testPkcs1WrongArity() {
        byte[] der = tlv(0x30, concat(
                integer(BigInteger.ZERO),
                integer(BigInteger.valueOf(3233)),
                integer(BigInteger.valueOf(17))));
        assertEquals("ERROR:malformed-pkcs1", parse(der), "3-integer SEQUENCE");
    }

    /** PKCS#8-shaped input with an unknown algorithm OID. */
    public void testPkcs8UnsupportedAlgorithm() {
        byte[] der = tlv(0x30, concat(
                integer(BigInteger.ZERO),
                tlv(0x30, oid(OID_DSA)),
                tlv(0x04, new byte[]{0x01, 0x02, 0x03})));
        assertEquals("ERROR:unsupported-algorithm", parse(der), "DSA PKCS#8 shape");
    }

    /** Empty input and unstructured garbage are distinguishable failures. */
    public void testEmptyAndGarbage() {
        assertEquals("ERROR:empty-input", parse(new byte[0]), "empty input");
        assertEquals("ERROR:unparseable", parse(new byte[]{0x01, 0x02, 0x03}),
                "garbage input");
    }
}
