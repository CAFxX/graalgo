package graalgo.p3x509;

import java.lang.reflect.Method;
import java.math.BigInteger;
import java.security.AlgorithmParameters;
import java.security.KeyFactory;
import java.security.KeyPair;
import java.security.KeyPairGenerator;
import java.security.spec.ECGenParameterSpec;
import java.security.spec.ECParameterSpec;
import java.security.spec.ECPoint;
import java.security.spec.ECPrivateKeySpec;
import java.security.spec.ECPublicKeySpec;
import java.security.spec.PKCS8EncodedKeySpec;
import java.security.spec.X509EncodedKeySpec;
import java.util.Arrays;

import javax.crypto.KeyAgreement;
import java.util.HashSet;
import java.util.Set;

import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Unit tests for {@link GoX509} (R-03 remediation, 2026-10-31).
 *
 * <p>This is the highest-risk file in the R-03 finding: it contains
 * hand-rolled EC arithmetic ({@code scalarMultiply}, used only for
 * SEC1 private-key derivation) and hand-rolled DER parsing, previously
 * covered only by end-to-end corpus programs. The tests pin it three
 * ways, in decreasing order of independence from the code under test:
 * <ol>
 *   <li>known vectors and a real certificate fixture whose fields were
 *       read with gc's {@code crypto/x509} (see {@code CERT_DER} and
 *       the gc-oracle values in the assertions);</li>
 *   <li>JDK oracles that do not share code with GoX509 (ECDH
 *       {@code KeyAgreement} for scalar-multiplication x-coordinates,
 *       {@code KeyFactory} for key components);</li>
 *   <li>internal-consistency checks (roundtrips, malformed-input
 *       rejections).</li>
 * </ol>
 *
 * <p>Documented encoding differences, pinned as-is (the Go synthetic
 * package consumes these values through its own mapping, so they are
 * wire values, not final Go values): {@code subject}/{@code issuer}
 * return the JDK RFC 2253 string, whose RDN order is the reverse of
 * gc's {@code pkix.Name.String()} (the Go side re-parses the string
 * with {@code parseName} into a {@code pkix.Name}); {@code keyUsage}
 * returns -1 when the extension is absent (gc reports 0);
 * {@code basicConstraints} returns {@code Integer.MAX_VALUE} for a CA
 * with no path-length constraint (gc reports {@code MaxPathLen -1}
 * plus {@code IsCA}). Changing any of these needs a Go-side audit
 * first, so the tests pin them rather than silently redefining them.
 */
public final class GoX509Test {

    // Self-signed EC P-256 certificate, generated with openssl for this
    // test (CN=graalgo-test.example.com, O=GRAALGO Test; SANs
    // graalgo-test.example.com, alt.example.com, 127.0.0.1; EKU
    // serverAuth+clientAuth; SKID=AKID). gc-oracle field values for
    // this exact DER (from crypto/x509.ParseCertificate):
    // serial 19d474f9c7c87353b9d97ed33de4c5492a55d55f,
    // NotBefore 1793465920, NotAfter 2108825920, Version 3,
    // ECDSA-SHA256, SubjectKeyId 64d2dfeee066ea890b0ed69f80db5a8bd0c36d25.
    private static final String CERT_DER_HEX =
            "30820226308201cca003020102021419d474f9c7c87353b9d97ed33de4c5492a55d55f"
            + "300a06082a8648ce3d040302303a3121301f06035504030c18677261616c676f2d746573742e6578616d706c652e636f6d31153013060355040a0c0c475241414c474f2054657374"
            + "301e170d3236313033313136353834305a170d3336313032383136353834305a303a3121301f06035504030c18677261616c676f2d746573742e6578616d706c652e636f6d31153013060355040a0c0c475241414c474f2054657374"
            + "3059301306072a8648ce3d020106082a8648ce3d0301070342000487dd389ac4e45481179a8343dc801a6290146b8c78ff34fadc043947334a82ee7d5dcd3a5c5b3ed25c4013f1699dd9e7f3fe96791a0fc83a41492693722181bca381af3081ac300f0603551d130101ff040530030101ff303a0603551d11043330318218677261616c676f2d746573742e6578616d706c652e636f6d820f616c742e6578616d706c652e636f6d87047f000001301d0603551d250416301406082b0601050507030106082b06010505070302301d0603551d0e0416041464d2dfeee066ea890b0ed69f80db5a8bd0c36d25301f0603551d2304183016801464d2dfeee066ea890b0ed69f80db5a8bd0c36d25300a06082a8648ce3d0403020348003045022005ca90958173a719db1f49753fabc8a43a86d5995b292c9d483d01be9e932d49022100b2e52e974aaf72a7e5eab304c1dbc95a4b07284d522bc3b8923dd85986f04bb4";

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long e, long a, String msg) {
        if (e != a) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    private static void assertEq(String e, String a, String msg) {
        if (!e.equals(a)) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    private static void assertNull(Object o, String msg) {
        if (o != null) {
            throw new AssertionError(msg + " expected null, got " + o);
        }
    }

    private static void assertNotNull(Object o, String msg) {
        if (o == null) {
            throw new AssertionError(msg + " expected non-null");
        }
    }

    private static GoSlice slice(byte[] b) {
        return new GoSlice(b.clone(), 0, b.length, b.length);
    }

    private static byte[] bytesOf(Object o) {
        GoSlice gs = (GoSlice) o;
        Object arr = gs.array();
        byte[] out = new byte[gs.len()];
        if (arr instanceof byte[] ba) {
            System.arraycopy(ba, gs.offset(), out, 0, out.length);
        } else if (arr instanceof int[] ia) {
            for (int i = 0; i < out.length; i++) {
                out[i] = (byte) ia[gs.offset() + i];
            }
        } else {
            throw new AssertionError("unexpected backing " + arr.getClass());
        }
        return out;
    }

    private static String strOf(Object o) {
        return new String(((GoString) o).bytes(), java.nio.charset.StandardCharsets.UTF_8);
    }

    private static String hex(byte[] b) {
        StringBuilder sb = new StringBuilder(b.length * 2);
        for (byte v : b) {
            sb.append(Character.forDigit((v >> 4) & 0xF, 16));
            sb.append(Character.forDigit(v & 0xF, 16));
        }
        return sb.toString();
    }

    private static byte[] hexBytes(String h) {
        byte[] out = new byte[h.length() / 2];
        for (int i = 0; i < out.length; i++) {
            out[i] = (byte) Integer.parseInt(h.substring(2 * i, 2 * i + 2), 16);
        }
        return out;
    }

    private static byte[] stripLeadingZero(byte[] b) {
        if (b.length > 1 && b[0] == 0) {
            return Arrays.copyOfRange(b, 1, b.length);
        }
        return b;
    }

    private static Object parseCert() {
        Object cert = GoX509.parse(slice(hexBytes(CERT_DER_HEX)));
        assertNotNull(cert, "fixture certificate must parse");
        return cert;
    }

    // ---------- certificate field accessors (gc-oracle values) ----------

    public void testParseFixtureFieldsAgainstGcOracle() {
        Object cert = parseCert();
        assertEq("19d474f9c7c87353b9d97ed33de4c5492a55d55f",
                hex(bytesOf(GoX509.serialNumber(cert))), "serial (gc oracle)");
        assertEq("O=GRAALGO Test,CN=graalgo-test.example.com",
                strOf(GoX509.subject(cert)), "subject wire string (JDK RFC 2253 order)");
        assertEq("O=GRAALGO Test,CN=graalgo-test.example.com",
                strOf(GoX509.issuer(cert)), "issuer wire string (self-signed)");
        assertEq(1793465920000L, (Long) GoX509.notBefore(cert), "NotBefore millis (gc oracle)");
        assertEq(2108825920000L, (Long) GoX509.notAfter(cert), "NotAfter millis (gc oracle)");
        assertEq(3, (Long) GoX509.version(cert), "version (gc oracle)");
        assertEq("1.2.840.10045.4.3.2", strOf(GoX509.sigAlgOid(cert)),
                "ECDSA-SHA256 OID");
        assertTrue(bytesOf(GoX509.signature(cert)).length > 0, "signature non-empty");
        assertTrue(bytesOf(GoX509.tbsCertificate(cert)).length > 0, "TBS non-empty");
        assertEq("303a3121301f06035504030c18677261616c676f2d746573742e6578616d706c652e636f6d31153013060355040a0c0c475241414c474f2054657374",
                hex(bytesOf(GoX509.rawSubject(cert))), "RawSubject (gc oracle)");
        assertEq("303a3121301f06035504030c18677261616c676f2d746573742e6578616d706c652e636f6d31153013060355040a0c0c475241414c474f2054657374",
                hex(bytesOf(GoX509.rawIssuer(cert))), "RawIssuer (gc oracle)");
    }

    public void testParseFixtureExtensions() {
        Object cert = parseCert();
        // Absent KeyUsage extension: pinned -1 wire value (see class note).
        assertEq(-1, (Long) GoX509.keyUsage(cert), "absent KeyUsage must be -1");
        // CA with no path-length constraint: pinned MAX_VALUE wire value.
        assertEq(2147483647L, (Long) GoX509.basicConstraints(cert),
                "CA without pathlen must be MAX_VALUE");
        assertEq("64d2dfeee066ea890b0ed69f80db5a8bd0c36d25",
                hex(bytesOf(GoX509.subjectKeyId(cert))), "SubjectKeyId (gc oracle)");
        assertEq("64d2dfeee066ea890b0ed69f80db5a8bd0c36d25",
                hex(bytesOf(GoX509.authorityKeyId(cert))), "AuthorityKeyId (gc oracle)");
        assertEq("1.3.6.1.5.5.7.3.1,1.3.6.1.5.5.7.3.2",
                strOf(GoX509.extKeyUsage(cert)), "serverAuth,clientAuth OIDs");
        Set<String> sans = new HashSet<>(Arrays.asList(
                strOf(GoX509.subjectAltNames(cert)).split(";")));
        assertTrue(sans.equals(Set.of("DNS:graalgo-test.example.com",
                        "DNS:alt.example.com", "IP:127.0.0.1")),
                "SAN set must match the fixture, got " + sans);
    }

    public void testParseFixturePublicKey() {
        Object cert = parseCert();
        // Method name says Oid but returns the JDK algorithm name; pinned.
        assertEq("EC", strOf(GoX509.publicKeyAlgOid(cert)), "EC public key algorithm");
        assertEq(256, (Long) GoX509.ecFieldSize(cert), "P-256 field size");
        assertEq("87dd389ac4e45481179a8343dc801a6290146b8c78ff34fadc043947334a82ee",
                hex(bytesOf(GoX509.ecX(cert))), "EC public X (fixture DER)");
        assertEq("7d5dcd3a5c5b3ed25c4013f1699dd9e7f3fe96791a0fc83a41492693722181bc",
                hex(bytesOf(GoX509.ecY(cert))), "EC public Y (fixture DER)");
        assertTrue(bytesOf(GoX509.publicKeyEncoded(cert)).length > 0, "SPKI non-empty");
        assertNull(GoX509.rsaModulus(cert), "EC cert must have no RSA modulus");
        assertEq(-1, (Long) GoX509.rsaExponent(cert), "EC cert must have no RSA exponent");
    }

    public void testParseRejectsGarbage() {
        assertNull(GoX509.parse(slice(new byte[]{0x30, 0x03, 0x02, 0x01, 0x05})),
                "garbage DER must not parse");
        assertNull(GoX509.parse(slice(new byte[0])), "empty input must not parse");
        assertNull(GoX509.parse(slice("not a certificate"
                .getBytes(java.nio.charset.StandardCharsets.US_ASCII))),
                "text input must not parse");
    }

    // ---------- SPKI / PKCS#1 parsing against JDK key components ----------

    public void testParseSpkiRsaAgainstJdk() throws Exception {
        KeyPair kp = KeyPairGenerator.getInstance("RSA").generateKeyPair();
        java.security.interfaces.RSAPublicKey pub =
                (java.security.interfaces.RSAPublicKey) kp.getPublic();
        String got = strOf(GoX509.parseSpki(slice(pub.getEncoded())));
        String want = "RSA:" + hex(stripLeadingZero(pub.getModulus().toByteArray()))
                + ":" + pub.getPublicExponent().longValue();
        assertEq(want, got, "RSA SPKI descriptor must match JDK components");
    }

    public void testParseSpkiEcAgainstJdk() throws Exception {
        KeyPairGenerator gen = KeyPairGenerator.getInstance("EC");
        gen.initialize(new ECGenParameterSpec("secp256r1"));
        KeyPair kp = gen.generateKeyPair();
        java.security.interfaces.ECPublicKey pub =
                (java.security.interfaces.ECPublicKey) kp.getPublic();
        String got = strOf(GoX509.parseSpki(slice(pub.getEncoded())));
        String want = "EC:" + hex(stripLeadingZero(pub.getW().getAffineX().toByteArray()))
                + ":" + hex(stripLeadingZero(pub.getW().getAffineY().toByteArray())) + ":256";
        assertEq(want, got, "EC SPKI descriptor must match JDK components");
    }

    public void testParseSpkiEd25519ShapeAndRejection() throws Exception {
        KeyPair kp = KeyPairGenerator.getInstance("Ed25519").generateKeyPair();
        String got = strOf(GoX509.parseSpki(slice(kp.getPublic().getEncoded())));
        assertTrue(got.startsWith("Ed25519:") && got.length() == 8 + 64,
                "Ed25519 SPKI must yield 'Ed25519:' + 32-byte hex, got " + got);
        assertNull(GoX509.parseSpki(slice(new byte[]{1, 2, 3, 4})),
                "garbage SPKI must not parse");
    }

    /** Minimal DER writer (short form only; all test values are small). */
    private static byte[] derInteger(byte[] content) {
        byte[] out = new byte[2 + content.length];
        out[0] = 0x02;
        out[1] = (byte) content.length;
        System.arraycopy(content, 0, out, 2, content.length);
        return out;
    }

    private static byte[] derSequence(byte[]... parts) {
        int total = 0;
        for (byte[] p : parts) {
            total += p.length;
        }
        byte[] out = new byte[2 + total];
        out[0] = 0x30;
        out[1] = (byte) total;
        int pos = 2;
        for (byte[] p : parts) {
            System.arraycopy(p, 0, out, pos, p.length);
            pos += p.length;
        }
        return out;
    }

    public void testParsePkcs1RoundtripAndMalformed() {
        byte[] n = hexBytes("00c0ffee1234");
        // Leading-zero INTEGER content must be stripped by the parser.
        byte[] der = derSequence(derInteger(n), derInteger(new byte[]{0x01, 0x00, 0x01}));
        assertEq("c0ffee1234:65537", strOf(GoX509.parsePkcs1(slice(der))),
                "PKCS#1 (n, e) must roundtrip with stripped leading zero");
        assertNull(GoX509.parsePkcs1(slice(new byte[0])), "empty PKCS#1 must be null");
        assertNull(GoX509.parsePkcs1(slice(new byte[]{0x31, 0x00})),
                "non-SEQUENCE PKCS#1 must be null");
        byte[] zeroE = derSequence(derInteger(new byte[]{0x05}),
                derInteger(new byte[]{0x00}));
        assertNull(GoX509.parsePkcs1(slice(zeroE)), "zero exponent must be null");
    }

    // ---------- private-key parsing against JDK components ----------

    public void testParsePrivateKeyPkcs8RsaAgainstJdk() throws Exception {
        KeyPair kp = KeyPairGenerator.getInstance("RSA").generateKeyPair();
        java.security.interfaces.RSAPrivateCrtKey priv =
                (java.security.interfaces.RSAPrivateCrtKey) kp.getPrivate();
        String got = strOf(GoX509.parsePrivateKey(slice(priv.getEncoded())));
        String[] parts = got.split(":");
        assertTrue(parts.length == 7 && "PKCS8".equals(parts[0]) && "RSA".equals(parts[1]),
                "PKCS#8 RSA descriptor shape, got " + got);
        assertTrue(new BigInteger(parts[2], 16).equals(priv.getModulus()), "n");
        assertTrue(new BigInteger(parts[3], 16).equals(priv.getPublicExponent()), "e");
        assertTrue(new BigInteger(parts[4], 16).equals(priv.getPrivateExponent()), "d");
        assertTrue(new BigInteger(parts[5], 16).equals(priv.getPrimeP()), "p");
        assertTrue(new BigInteger(parts[6], 16).equals(priv.getPrimeQ()), "q");
    }

    public void testParsePrivateKeyPkcs8EcAndEd25519AgainstJdk() throws Exception {
        KeyPairGenerator gen = KeyPairGenerator.getInstance("EC");
        gen.initialize(new ECGenParameterSpec("secp256r1"));
        KeyPair kp = gen.generateKeyPair();
        java.security.interfaces.ECPrivateKey priv =
                (java.security.interfaces.ECPrivateKey) kp.getPrivate();
        String got = strOf(GoX509.parsePrivateKey(slice(priv.getEncoded())));
        String[] parts = got.split(":");
        assertTrue(parts.length == 4 && "PKCS8".equals(parts[0]) && "EC".equals(parts[1])
                        && "secp256r1".equals(parts[2]),
                "PKCS#8 EC descriptor shape, got " + got);
        assertTrue(new BigInteger(parts[3], 16).equals(priv.getS()), "EC private scalar");

        KeyPair ed = KeyPairGenerator.getInstance("Ed25519").generateKeyPair();
        java.security.interfaces.EdECPrivateKey edPriv =
                (java.security.interfaces.EdECPrivateKey) ed.getPrivate();
        String edGot = strOf(GoX509.parsePrivateKey(slice(edPriv.getEncoded())));
        assertEq("PKCS8:Ed25519:" + hex(edPriv.getBytes().orElseThrow()), edGot,
                "PKCS#8 Ed25519 descriptor must carry the seed");
        assertEq("", strOf(GoX509.parsePrivateKey(slice(new byte[]{9, 9, 9}))),
                "garbage private key must yield empty descriptor");
    }

    // ---------- SEC1 derivation: the hand-rolled scalarMultiply path ----------

    private static ECParameterSpec specFor(String name) throws Exception {
        AlgorithmParameters p = AlgorithmParameters.getInstance("EC");
        p.init(new ECGenParameterSpec(name));
        return p.getParameterSpec(ECParameterSpec.class);
    }

    private static byte[] fixed(BigInteger v, ECParameterSpec spec) {
        int size = (spec.getCurve().getField().getFieldSize() + 7) / 8;
        byte[] raw = stripLeadingZero(v.toByteArray());
        byte[] out = new byte[size];
        System.arraycopy(raw, 0, out, size - raw.length, Math.min(raw.length, size));
        return out;
    }

    private static byte[] derOctetString(byte[] content) {
        byte[] out = new byte[2 + content.length];
        out[0] = 0x04;
        out[1] = (byte) content.length;
        System.arraycopy(content, 0, out, 2, content.length);
        return out;
    }

    private static byte[] derOid(byte[] content) {
        byte[] out = new byte[2 + content.length];
        out[0] = 0x06;
        out[1] = (byte) content.length;
        System.arraycopy(content, 0, out, 2, content.length);
        return out;
    }

    private static byte[] derExplicit0(byte[] inner) {
        byte[] out = new byte[2 + inner.length];
        out[0] = (byte) 0xA0;
        out[1] = (byte) inner.length;
        System.arraycopy(inner, 0, out, 2, inner.length);
        return out;
    }

    private static byte[] derExplicit1(byte[] inner) {
        byte[] out = new byte[2 + inner.length];
        out[0] = (byte) 0xA1;
        out[1] = (byte) inner.length;
        System.arraycopy(inner, 0, out, 2, inner.length);
        return out;
    }

    private static byte[] derBitString(byte[] content) {
        byte[] out = new byte[3 + content.length];
        out[0] = 0x03;
        out[1] = (byte) (content.length + 1);
        out[2] = 0x00;
        System.arraycopy(content, 0, out, 3, content.length);
        return out;
    }

    /** SEC1 ECPrivateKey DER for P-256: SEQUENCE { INTEGER 1, OCTET d, [0] OID, [1] BITSTRING }. */
    private static byte[] sec1Der(byte[] d, byte[] pub) {
        byte[] oid = hexBytes("2a8648ce3d030107"); // 1.2.840.10045.3.1.7
        return derSequence(derInteger(new byte[]{0x01}), derOctetString(d),
                derExplicit0(derOid(oid)), derExplicit1(derBitString(pub)));
    }

    public void testSec1DerivationMatchesJdkKeypair() throws Exception {
        ECParameterSpec spec = specFor("secp256r1");
        KeyPairGenerator gen = KeyPairGenerator.getInstance("EC");
        gen.initialize(new ECGenParameterSpec("secp256r1"));
        KeyPair kp = gen.generateKeyPair();
        java.security.interfaces.ECPrivateKey priv =
                (java.security.interfaces.ECPrivateKey) kp.getPrivate();
        java.security.interfaces.ECPublicKey pub =
                (java.security.interfaces.ECPublicKey) kp.getPublic();
        byte[] d = fixed(priv.getS(), spec);
        byte[] pubBytes = new byte[65];
        pubBytes[0] = 0x04;
        System.arraycopy(fixed(pub.getW().getAffineX(), spec), 0, pubBytes, 1, 32);
        System.arraycopy(fixed(pub.getW().getAffineY(), spec), 0, pubBytes, 33, 32);
        String got = strOf(GoX509.parsePrivateKey(slice(sec1Der(d, pubBytes))));
        String want = "SEC1:EC:secp256r1:" + hex(fixed(pub.getW().getAffineX(), spec))
                + ":" + hex(fixed(pub.getW().getAffineY(), spec)) + ":" + hex(d);
        assertEq(want, got, "SEC1 derivation must reproduce the JDK keypair public point");
    }

    public void testSec1UnitScalarYieldsGenerator() throws Exception {
        ECParameterSpec spec = specFor("secp256r1");
        byte[] d = new byte[32];
        d[31] = 0x01;
        ECPoint g = spec.getGenerator();
        byte[] pubBytes = new byte[65];
        pubBytes[0] = 0x04;
        System.arraycopy(fixed(g.getAffineX(), spec), 0, pubBytes, 1, 32);
        System.arraycopy(fixed(g.getAffineY(), spec), 0, pubBytes, 33, 32);
        String got = strOf(GoX509.parsePrivateKey(slice(sec1Der(d, pubBytes))));
        String want = "SEC1:EC:secp256r1:" + hex(fixed(g.getAffineX(), spec))
                + ":" + hex(fixed(g.getAffineY(), spec)) + ":" + hex(d);
        assertEq(want, got, "1 * G must be the generator (known vector)");
    }

    // ---------- scalarMultiply itself, via reflection (private method) ----------

    private static ECPoint scalarMultiply(ECParameterSpec spec, BigInteger s) throws Exception {
        Method m = GoX509.class.getDeclaredMethod("scalarMultiply",
                ECParameterSpec.class, BigInteger.class);
        m.setAccessible(true);
        return (ECPoint) m.invoke(null, spec, s);
    }

    /** ECDH oracle: x-coordinate of s*G from the JDK, independent of GoX509. */
    private static BigInteger ecdhX(ECParameterSpec spec, BigInteger s) throws Exception {
        KeyFactory kf = KeyFactory.getInstance("EC");
        java.security.PrivateKey priv = kf.generatePrivate(new ECPrivateKeySpec(s, spec));
        java.security.PublicKey peer = kf.generatePublic(
                new ECPublicKeySpec(spec.getGenerator(), spec));
        KeyAgreement ka = KeyAgreement.getInstance("ECDH");
        ka.init(priv);
        ka.doPhase(peer, true);
        return new BigInteger(1, ka.generateSecret());
    }

    private static void assertOnCurve(ECParameterSpec spec, ECPoint p, String msg) {
        BigInteger prime = ((java.security.spec.ECFieldFp) spec.getCurve().getField()).getP();
        BigInteger lhs = p.getAffineY().modPow(BigInteger.TWO, prime);
        BigInteger rhs = p.getAffineX().modPow(BigInteger.valueOf(3), prime)
                .add(spec.getCurve().getA().multiply(p.getAffineX()))
                .add(spec.getCurve().getB()).mod(prime);
        assertTrue(lhs.equals(rhs), msg + ": point must satisfy the curve equation");
    }

    public void testScalarMultiplyAgainstEcdhOracle() throws Exception {
        ECParameterSpec spec = specFor("secp256r1");
        for (BigInteger s : new BigInteger[]{
                BigInteger.ONE, BigInteger.TWO, BigInteger.valueOf(12345),
                new BigInteger("98765432109876543210987654321")}) {
            ECPoint p = scalarMultiply(spec, s);
            assertTrue(!p.equals(ECPoint.POINT_INFINITY), "s=" + s + " must not be infinity");
            assertTrue(p.getAffineX().equals(ecdhX(spec, s)),
                    "s=" + s + " x-coordinate must match the ECDH oracle");
            assertOnCurve(spec, p, "s=" + s);
        }
    }

    public void testScalarMultiplyEdgeCases() throws Exception {
        ECParameterSpec spec = specFor("secp256r1");
        BigInteger order = spec.getOrder();
        assertTrue(scalarMultiply(spec, BigInteger.ZERO).equals(ECPoint.POINT_INFINITY),
                "0 * G must be infinity");
        assertTrue(scalarMultiply(spec, order).equals(ECPoint.POINT_INFINITY),
                "order * G must be infinity");
        ECPoint g = spec.getGenerator();
        assertTrue(scalarMultiply(spec, BigInteger.ONE).equals(g), "1 * G must be G");
        ECPoint neg = scalarMultiply(spec, order.subtract(BigInteger.ONE));
        BigInteger prime = ((java.security.spec.ECFieldFp) spec.getCurve().getField()).getP();
        assertTrue(neg.getAffineX().equals(g.getAffineX()), "(order-1)*G x must equal G x");
        assertTrue(neg.getAffineY().equals(prime.subtract(g.getAffineY())),
                "(order-1)*G must be -G");
        // P-384 spot check: the arithmetic is curve-generic, not P-256-only.
        ECParameterSpec spec384 = specFor("secp384r1");
        ECPoint p384 = scalarMultiply(spec384, BigInteger.valueOf(7));
        assertTrue(p384.getAffineX().equals(ecdhX(spec384, BigInteger.valueOf(7))),
                "P-384 7*G x must match the ECDH oracle");
        assertOnCurve(spec384, p384, "P-384 7*G");
    }

    // ---------- PKCS#8 / SEC1 rejection paths ----------

    public void testParsePrivateKeyRejectsPkcs1TwoIntAndGarbage() {
        byte[] twoInt = derSequence(derInteger(new byte[]{0x05}),
                derInteger(new byte[]{0x03}));
        // 2 INTEGERs: not a 9-int PKCS#1 key, not SEC1 either.
        assertEq("", strOf(GoX509.parsePrivateKey(slice(twoInt))),
                "2-int input must yield an empty descriptor");
    }
}
