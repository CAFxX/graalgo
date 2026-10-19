package graalgo.p3x509;

import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;
import java.security.KeyFactory;
import java.security.KeyPair;
import java.security.KeyPairGenerator;
import java.security.interfaces.ECPrivateKey;
import java.security.interfaces.ECPublicKey;
import java.security.spec.ECGenParameterSpec;
import java.security.spec.PKCS8EncodedKeySpec;

import graalgo.values.GoString;

/**
 * Unit tests for the EC curve identification in {@link GoX509}
 * (codebase-review I-4, 2026-10-19): a 256-bit field does NOT imply
 * secp256r1. The private-key path must identify the curve by its
 * named-curve OID, and SEC1 descriptors must carry {@code curveName:d}
 * only (the Go side derives the public point itself via ScalarBaseMult).
 *
 * <p>JDK limitation (verified on both the system OpenJDK 25 and the pinned
 * GraalVM JDK 25): SunEC supports only secp256r1/secp384r1/secp521r1, so no
 * live secp256k1 <em>key</em> can be generated or PKCS#8-parsed here — the
 * old field-size misidentification is latent on these JDKs (KeyFactory
 * rejects such keys before any naming happens) and becomes live on any
 * provider that parses them. The secp256k1 coverage is therefore via
 * synthetic SEC1 DERs carrying the real secp256k1 OID (1.3.132.0.10), which
 * exercise the OID table and the descriptor format without needing JDK
 * curve support.
 */
public final class GoX509CurveTest {

    /** DER content octets of the secp256k1 named-curve OID (1.3.132.0.10). */
    private static final byte[] OID_SECP256K1 =
            {(byte) 0x2b, (byte) 0x81, 0x04, 0x00, 0x0a};
    /** DER content octets of the secp256r1 named-curve OID (1.2.840.10045.3.1.7). */
    private static final byte[] OID_SECP256R1 =
            {0x2a, (byte) 0x86, 0x48, (byte) 0xce, 0x3d, 0x03, 0x01, 0x07};

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void checkEq(String want, String got, String msg) {
        if (!want.equals(got)) {
            throw new AssertionError(msg + ": want <" + want + ">, got <" + got + ">");
        }
    }

    /** Runs {@code GoX509.parsePrivateKey} on raw DER, returning the descriptor. */
    private static String parsePrivateKey(byte[] der) {
        Object r = GoX509.parsePrivateKey(der);
        check(r instanceof GoString, "parsePrivateKey did not return a GoString");
        return new String(((GoString) r).bytes(), StandardCharsets.UTF_8);
    }

    private static KeyPair genKey(String curve) throws Exception {
        KeyPairGenerator kpg = KeyPairGenerator.getInstance("EC");
        kpg.initialize(new ECGenParameterSpec(curve));
        return kpg.generateKeyPair();
    }

    /** Pads/strips the scalar to exactly {@code size} bytes. */
    private static byte[] scalarFixed(ECPrivateKey key, int size) {
        byte[] raw = key.getS().toByteArray();
        if (raw.length > 1 && raw[0] == 0) {
            byte[] stripped = new byte[raw.length - 1];
            System.arraycopy(raw, 1, stripped, 0, stripped.length);
            raw = stripped;
        }
        check(raw.length <= size, "scalar longer than " + size + " bytes");
        byte[] out = new byte[size];
        System.arraycopy(raw, 0, out, size - raw.length, raw.length);
        return out;
    }

    private static String hex(byte[] b) {
        StringBuilder sb = new StringBuilder(b.length * 2);
        for (byte v : b) {
            sb.append(Character.forDigit((v >> 4) & 0xF, 16));
            sb.append(Character.forDigit(v & 0xF, 16));
        }
        return sb.toString();
    }

    /** Writes a DER length (short or long form) to the stream. */
    private static void writeLen(ByteArrayOutputStream out, int len) {
        if (len < 128) {
            out.write(len);
        } else if (len < 256) {
            out.write(0x81);
            out.write(len);
        } else {
            out.write(0x82);
            out.write((len >> 8) & 0xFF);
            out.write(len & 0xFF);
        }
    }

    /**
     * Builds a minimal SEC1 ECPrivateKey DER:
     * SEQUENCE { INTEGER 1, OCTET STRING d, [0] { OID }, [1] { BIT STRING 0x00||point } }.
     * The public point is content the parser must ignore (B-1).
     */
    private static byte[] sec1Der(byte[] d, byte[] oid, byte[] point) throws Exception {
        ByteArrayOutputStream body = new ByteArrayOutputStream();
        body.write(new byte[]{0x02, 0x01, 0x01}); // version
        body.write(0x04);
        writeLen(body, d.length);
        body.write(d);
        body.write(0xA0); // [0] parameters
        writeLen(body, oid.length + 2);
        body.write(0x06);
        writeLen(body, oid.length);
        body.write(oid);
        ByteArrayOutputStream bs = new ByteArrayOutputStream();
        bs.write(0x00); // unused bits
        bs.write(point);
        byte[] bsv = bs.toByteArray();
        body.write(0xA1); // [1] publicKey
        writeLen(body, bsv.length + 2);
        body.write(0x03);
        writeLen(body, bsv.length);
        body.write(bsv);
        byte[] b = body.toByteArray();
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        out.write(0x30);
        writeLen(out, b.length);
        out.write(b);
        return out.toByteArray();
    }

    /** Uncompressed point encoding (0x04 || x || y), each coordinate {@code coordSize} bytes. */
    private static byte[] uncompressedPoint(ECPublicKey pub, int coordSize) {
        byte[] x = pub.getW().getAffineX().toByteArray();
        byte[] y = pub.getW().getAffineY().toByteArray();
        byte[] out = new byte[1 + 2 * coordSize];
        out[0] = 0x04;
        System.arraycopy(x, Math.max(0, x.length - coordSize), out,
                1 + Math.max(0, coordSize - x.length), Math.min(coordSize, x.length));
        System.arraycopy(y, Math.max(0, y.length - coordSize), out,
                1 + coordSize + Math.max(0, coordSize - y.length), Math.min(coordSize, y.length));
        return out;
    }

    public void testPkcs8Secp256r1NamedCorrectly() throws Exception {
        KeyPair kp = genKey("secp256r1");
        byte[] der = kp.getPrivate().getEncoded(); // PKCS#8
        ECPrivateKey parsed = (ECPrivateKey) KeyFactory.getInstance("EC")
                .generatePrivate(new PKCS8EncodedKeySpec(der));
        String want = "PKCS8:EC:secp256r1:" + hex(scalarFixed(parsed, 32));
        checkEq(want, parsePrivateKey(der), "secp256r1 PKCS#8 descriptor");
    }

    public void testPkcs8Secp384r1NamedCorrectly() throws Exception {
        KeyPair kp = genKey("secp384r1");
        byte[] der = kp.getPrivate().getEncoded(); // PKCS#8
        ECPrivateKey parsed = (ECPrivateKey) KeyFactory.getInstance("EC")
                .generatePrivate(new PKCS8EncodedKeySpec(der));
        String want = "PKCS8:EC:secp384r1:" + hex(scalarFixed(parsed, 48));
        checkEq(want, parsePrivateKey(der), "secp384r1 PKCS#8 descriptor");
    }

    public void testSec1Secp256k1IdentifiedByOid() throws Exception {
        // I-4: the secp256k1 OID must resolve to secp256k1, never to
        // secp256r1 via the field size. Red-green on the OID table: before
        // 1.3.132.0.10 was added, this returned "" (unknown curve).
        byte[] d = new byte[32];
        for (int i = 0; i < d.length; i++) {
            d[i] = (byte) (i + 1);
        }
        byte[] point = new byte[65];
        point[0] = 0x04; // content ignored by the parser
        byte[] der = sec1Der(d, OID_SECP256K1, point);
        String want = "SEC1:EC:secp256k1:" + hex(d);
        checkEq(want, parsePrivateKey(der), "secp256k1 SEC1 descriptor");
    }

    public void testSec1Secp256r1IdentifiedByOid() throws Exception {
        byte[] d = new byte[32];
        for (int i = 0; i < d.length; i++) {
            d[i] = (byte) (0xA0 + i);
        }
        byte[] point = new byte[65];
        point[0] = 0x04; // content ignored by the parser
        byte[] der = sec1Der(d, OID_SECP256R1, point);
        String want = "SEC1:EC:secp256r1:" + hex(d);
        checkEq(want, parsePrivateKey(der), "secp256r1 SEC1 descriptor");
    }

    public void testSec1DescriptorCarriesNoPublicPoint() throws Exception {
        // B-1: trySec1 must return EC:curveName:d only — the Go side derives
        // (x, y) itself via ScalarBaseMult(d). A real public point is
        // embedded in the DER; if any x:y leaked into the descriptor the
        // hand-rolled scalar multiplication would still be wired in.
        KeyPair kp = genKey("secp256r1");
        ECPrivateKey priv = (ECPrivateKey) kp.getPrivate();
        byte[] d = scalarFixed(priv, 32);
        byte[] der = sec1Der(d, OID_SECP256R1,
                uncompressedPoint((ECPublicKey) kp.getPublic(), 32));
        String got = parsePrivateKey(der);
        checkEq("SEC1:EC:secp256r1:" + hex(d), got, "SEC1 descriptor format");
        check(got.split(":").length == 4, "descriptor must have exactly 4 parts: " + got);
    }

    public void testSec1UnknownOidFailsLoudly() throws Exception {
        // An unrecognized curve OID must fail the parse ("" -> the Go side
        // reports an error), never silently map to a wrong curve.
        byte[] d = new byte[32];
        byte[] point = new byte[65];
        point[0] = 0x04;
        byte[] der = sec1Der(d, new byte[]{0x2a, 0x03}, point); // OID 1.2.3
        checkEq("", parsePrivateKey(der), "unknown curve OID must fail loudly");
    }
}
