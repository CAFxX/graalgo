package graalgo.p3x509;

import java.io.ByteArrayInputStream;
import java.security.AlgorithmParameters;
import java.security.KeyFactory;
import java.security.cert.CertificateException;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;
import java.security.interfaces.ECPrivateKey;
import java.security.interfaces.EdECPrivateKey;
import java.security.interfaces.RSAPrivateCrtKey;
import java.security.spec.PKCS8EncodedKeySpec;
import java.util.ArrayList;
import java.util.Collection;
import java.util.List;

import graalgo.values.GoInterface;
import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * X.509 certificate handling delegated to the JDK (P3-M9).
 *
 * <p>Per the stdlib strategy (MEMORY.md 2026-10-10): prefer delegating to JDK
 * libraries when semantically compatible, over reimplementing in Go.
 * Certificate parsing uses {@link CertificateFactory}; field extraction
 * exposes the X509Certificate fields needed by the synthetic crypto/x509
 * package via {@code graalgo.x509.*} linkname intrinsics.
 *
 * <p>D5 types: []byte arrives as GoSlice (int[] backing); strings return as
 * GoString; ints as Long; the X509Certificate passes through as Object.
 */
public final class GoX509 {
    private GoX509() {}

    private static final CertificateFactory FACTORY;

    static {
        try {
            FACTORY = CertificateFactory.getInstance("X.509");
        } catch (CertificateException e) {
            throw new ExceptionInInitializerError(e);
        }
    }

    /** Parses DER bytes into an X509Certificate, or null on failure. */
    public static Object parse(Object derArg) {
        byte[] der = toBytes(derArg);
        try {
            return (X509Certificate) FACTORY.generateCertificate(
                    new ByteArrayInputStream(der));
        } catch (Exception e) {
            return null;
        }
    }

    /**
     * Unwraps a Go {@code any} holding an X509Certificate. The Go side passes
     * the parse result as {@code any}, which arrives as a GoInterface wrapper;
     * the raw X509Certificate is in its data slot.
     */
    private static X509Certificate unwrapCert(Object certArg) {
        if (certArg instanceof GoInterface) {
            certArg = ((GoInterface) certArg).data();
        }
        return (X509Certificate) certArg;
    }

    /** Serial number as big-endian bytes (Go builds big.Int from this). */
    public static Object serialNumber(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        return toGoBytes(cert.getSerialNumber().toByteArray());
    }

    /** Subject DN as RFC 2253 string. */
    public static Object subject(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        return toGoString(cert.getSubjectX500Principal().getName());
    }

    /** Issuer DN as RFC 2253 string. */
    public static Object issuer(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        return toGoString(cert.getIssuerX500Principal().getName());
    }

    /** NotBefore as Unix millis. */
    public static Object notBefore(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        return cert.getNotBefore().getTime();
    }

    /** NotAfter as Unix millis. */
    public static Object notAfter(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        return cert.getNotAfter().getTime();
    }

    /** Version (1, 2, or 3). */
    public static Object version(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        return (long) cert.getVersion();
    }

    /** Signature algorithm OID as dotted string. */
    public static Object sigAlgOid(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        return toGoString(cert.getSigAlgOID());
    }

    /** Signature bytes. */
    public static Object signature(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        return toGoBytes(cert.getSignature());
    }

    /** TBS certificate bytes (for RawTBSCertificate). */
    public static Object tbsCertificate(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        try {
            return toGoBytes(cert.getTBSCertificate());
        } catch (CertificateException e) {
            return null;
        }
    }

    /** Key usage as bitmask, or -1 if absent. */
    public static Object keyUsage(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        boolean[] ku = cert.getKeyUsage();
        if (ku == null) {
            return -1L;
        }
        long bits = 0;
        for (int i = 0; i < ku.length && i < 9; i++) {
            if (ku[i]) {
                bits |= (1L << i);
            }
        }
        return bits;
    }

    /**
     * Basic constraints path length, or -1 if not a CA.
     * Returns Integer.MAX_VALUE if CA with no path length constraint.
     */
    public static Object basicConstraints(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        int bc = cert.getBasicConstraints();
        return (long) bc;
    }

    /** Subject key identifier, or null if absent. */
    public static Object subjectKeyId(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        byte[] ext = cert.getExtensionValue("2.5.29.14");
        if (ext == null) {
            return null;
        }
        // Unwrap OCTET STRING twice: getExtensionValue returns the outer
        // extnValue OCTET STRING, and SubjectKeyIdentifier itself is an
        // OCTET STRING (RFC 5280: SubjectKeyIdentifier ::= KeyIdentifier,
        // KeyIdentifier ::= OCTET STRING).
        return toGoBytes(unwrapOctetString(unwrapOctetString(ext)));
    }

    /** Authority key identifier, or null if absent. */
    public static Object authorityKeyId(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        byte[] ext = cert.getExtensionValue("2.5.29.35");
        if (ext == null) {
            return null;
        }
        // AuthorityKeyIdentifier is a SEQUENCE; extract keyIdentifier [0] if present
        byte[] unwrapped = unwrapOctetString(ext);
        return toGoBytes(extractAuthorityKeyId(unwrapped));
    }

    /** Public key algorithm name (e.g., "RSA", "EC", "Ed25519"). */
    public static Object publicKeyAlgOid(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        return toGoString(cert.getPublicKey().getAlgorithm());
    }

    /** Public key in X.509 SubjectPublicKeyInfo DER encoding. */
    public static Object publicKeyEncoded(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        return toGoBytes(cert.getPublicKey().getEncoded());
    }

    /** Raw issuer DER bytes (for RawIssuer). */
    public static Object rawIssuer(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        return toGoBytes(cert.getIssuerX500Principal().getEncoded());
    }

    /** Raw subject DER bytes (for RawSubject). */
    public static Object rawSubject(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        return toGoBytes(cert.getSubjectX500Principal().getEncoded());
    }

    // ---- Public key components (for ParsePKIXPublicKey) ----
    //
    // The synthetic crypto/x509 cannot use asn1.Unmarshal (pointer-store
    // WS D limitation), so public key parsing delegates to the JDK.
    // The Go side switches on publicKeyAlg and calls the matching
    // intrinsics below.

    /** RSA modulus N as big-endian bytes, or null if not RSA. */
    public static Object rsaModulus(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        java.security.PublicKey key = cert.getPublicKey();
        if (!(key instanceof java.security.interfaces.RSAPublicKey)) {
            return null;
        }
        java.security.interfaces.RSAPublicKey rsa =
                (java.security.interfaces.RSAPublicKey) key;
        return toGoBytes(stripLeadingZero(rsa.getModulus().toByteArray()));
    }

    /** RSA public exponent E, or -1 if not RSA. */
    public static Object rsaExponent(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        java.security.PublicKey key = cert.getPublicKey();
        if (!(key instanceof java.security.interfaces.RSAPublicKey)) {
            return -1L;
        }
        java.security.interfaces.RSAPublicKey rsa =
                (java.security.interfaces.RSAPublicKey) key;
        return rsa.getPublicExponent().longValue();
    }

    /** EC affine X as big-endian bytes, or null if not EC. */
    public static Object ecX(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        java.security.PublicKey key = cert.getPublicKey();
        if (!(key instanceof java.security.interfaces.ECPublicKey)) {
            return null;
        }
        java.security.interfaces.ECPublicKey ec =
                (java.security.interfaces.ECPublicKey) key;
        return toGoBytes(stripLeadingZero(ec.getW().getAffineX().toByteArray()));
    }

    /** EC affine Y as big-endian bytes, or null if not EC. */
    public static Object ecY(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        java.security.PublicKey key = cert.getPublicKey();
        if (!(key instanceof java.security.interfaces.ECPublicKey)) {
            return null;
        }
        java.security.interfaces.ECPublicKey ec =
                (java.security.interfaces.ECPublicKey) key;
        return toGoBytes(stripLeadingZero(ec.getW().getAffineY().toByteArray()));
    }

    /**
     * EC field size in bits (224, 256, 384, 521), or -1 if not EC.
     * The Go side maps this to the elliptic.Curve (P-224, P-256, P-384,
     * P-521). Field size alone cannot distinguish curves sharing a field
     * (e.g. secp256r1 vs secp256k1); the OID mechanism used by the
     * private-key path ({@link #ecCurveName}) is the authoritative curve
     * identifier. This entry point keeps the field-size wire format for
     * Go-side compatibility; a 256-bit non-P-256 certificate is therefore
     * still reported as 256 here (known limitation, same bug family as the
     * fixed private-key path).
     */
    public static Object ecFieldSize(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        java.security.PublicKey key = cert.getPublicKey();
        if (!(key instanceof java.security.interfaces.ECPublicKey)) {
            return -1L;
        }
        java.security.interfaces.ECPublicKey ec =
                (java.security.interfaces.ECPublicKey) key;
        return (long) ec.getParams().getCurve().getField().getFieldSize();
    }

    /**
     * Ed25519 raw 32-byte public key, or null if not Ed25519.
     * Extracted from the SPKI BIT STRING (the JDK EdECPublicKey API
     * does not expose raw bytes directly).
     */
    public static Object ed25519Raw(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        java.security.PublicKey key = cert.getPublicKey();
        if (!"Ed25519".equalsIgnoreCase(key.getAlgorithm())
                && !"EdDSA".equalsIgnoreCase(key.getAlgorithm())) {
            return null;
        }
        byte[] spki = key.getEncoded();
        byte[] raw = extractBitString(spki);
        if (raw == null || raw.length != 32) {
            return null;
        }
        return toGoBytes(raw);
    }

    /** Strips a single leading zero byte (BigInteger sign byte). */
    private static byte[] stripLeadingZero(byte[] b) {
        if (b.length > 1 && b[0] == 0) {
            byte[] out = new byte[b.length - 1];
            System.arraycopy(b, 1, out, 0, out.length);
            return out;
        }
        return b;
    }

    /**
     * Extracts the BIT STRING content from SPKI DER (skipping the
     * unused-bits byte). Returns null if the structure is unexpected.
     */
    private static byte[] extractBitString(byte[] spki) {
        int pos = 0;
        if (spki.length < 2 || spki[pos++] != 0x30) {
            return null;
        }
        pos = skipLength(spki, pos);
        if (pos < 0 || pos >= spki.length || spki[pos] != 0x30) {
            return null;
        }
        pos++;
        int algLenPos = skipLength(spki, pos);
        if (algLenPos < 0) {
            return null;
        }
        int algLen = derLength(spki, pos);
        pos = algLenPos + algLen;
        if (pos >= spki.length || spki[pos] != 0x03) {
            return null;
        }
        pos++;
        int bitStrLenPos = skipLength(spki, pos);
        if (bitStrLenPos < 0) {
            return null;
        }
        int bitStrLen = derLength(spki, pos);
        pos = bitStrLenPos;
        if (pos >= spki.length || spki[pos] != 0x00) {
            return null;
        }
        pos++;
        int contentLen = bitStrLen - 1;
        if (contentLen < 0 || pos + contentLen > spki.length) {
            return null;
        }
        byte[] out = new byte[contentLen];
        System.arraycopy(spki, pos, out, 0, contentLen);
        return out;
    }

    /** Returns the position after the DER length bytes at pos, or -1. */
    private static int skipLength(byte[] der, int pos) {
        if (pos >= der.length) {
            return -1;
        }
        int b = der[pos] & 0xFF;
        if ((b & 0x80) == 0) {
            return pos + 1;
        }
        int num = b & 0x7F;
        if (num > 4 || pos + 1 + num > der.length) {
            return -1;
        }
        return pos + 1 + num;
    }

    /** Returns the DER length value at pos (assumes valid). */
    private static int derLength(byte[] der, int pos) {
        int b = der[pos] & 0xFF;
        if ((b & 0x80) == 0) {
            return b;
        }
        int num = b & 0x7F;
        int len = 0;
        for (int i = 0; i < num; i++) {
            len = (len << 8) | (der[pos + 1 + i] & 0xFF);
        }
        return len;
    }

    // ---- Standalone DER parsers (for ParsePKIXPublicKey/ParsePKCS1PublicKey) ----

    /** Parses SPKI DER into string-encoded public key components. */
    public static Object parseSpki(Object spkiArg) {
        byte[] spki = toBytes(spkiArg);
        java.security.PublicKey key = null;
        String[] algs = {"RSA", "EC", "Ed25519", "EdDSA", "ECDSA"};
        for (String a : algs) {
            try {
                java.security.KeyFactory kf =
                        java.security.KeyFactory.getInstance(a);
                key = kf.generatePublic(
                        new java.security.spec.X509EncodedKeySpec(spki));
                break;
            } catch (Exception e) {
                // try next algorithm
            }
        }
        if (key == null) {
            return null;
        }
        String encoded = encodePublicKey(key);
        return encoded == null ? null : toGoString(encoded);
    }

    /** Parses PKCS1 DER into "n_hex:e". */
    public static Object parsePkcs1(Object derArg) {
        byte[] der = toBytes(derArg);
        try {
            int pos = 0;
            if (der.length < 2 || der[pos++] != 0x30) {
                return null;
            }
            pos = skipLength(der, pos);
            if (pos < 0) {
                return null;
            }
            if (pos >= der.length || der[pos++] != 0x02) {
                return null;
            }
            int nLenPos = skipLength(der, pos);
            if (nLenPos < 0) {
                return null;
            }
            int nLen = derLength(der, pos);
            pos = nLenPos;
            if (pos + nLen > der.length) {
                return null;
            }
            byte[] nBytes = new byte[nLen];
            System.arraycopy(der, pos, nBytes, 0, nLen);
            pos += nLen;
            nBytes = stripLeadingZero(nBytes);
            if (pos >= der.length || der[pos++] != 0x02) {
                return null;
            }
            int eLenPos = skipLength(der, pos);
            if (eLenPos < 0) {
                return null;
            }
            int eLen = derLength(der, pos);
            pos = eLenPos;
            if (pos + eLen > der.length) {
                return null;
            }
            long e = 0;
            for (int i = 0; i < eLen; i++) {
                e = (e << 8) | (der[pos + i] & 0xFF);
            }
            if (e <= 0) {
                return null;
            }
            return toGoString(toHex(nBytes) + ":" + e);
        } catch (Exception ex) {
            return null;
        }
    }

    /** Encodes a PublicKey into the string format parseSpki returns. */
    private static String encodePublicKey(java.security.PublicKey key) {
        if (key instanceof java.security.interfaces.RSAPublicKey) {
            java.security.interfaces.RSAPublicKey rsa =
                    (java.security.interfaces.RSAPublicKey) key;
            byte[] n = stripLeadingZero(rsa.getModulus().toByteArray());
            long e = rsa.getPublicExponent().longValue();
            return "RSA:" + toHex(n) + ":" + e;
        }
        if (key instanceof java.security.interfaces.ECPublicKey) {
            java.security.interfaces.ECPublicKey ec =
                    (java.security.interfaces.ECPublicKey) key;
            byte[] x = stripLeadingZero(ec.getW().getAffineX().toByteArray());
            byte[] y = stripLeadingZero(ec.getW().getAffineY().toByteArray());
            long fs = ec.getParams().getCurve().getField().getFieldSize();
            return "EC:" + toHex(x) + ":" + toHex(y) + ":" + fs;
        }
        String alg = key.getAlgorithm();
        if ("Ed25519".equalsIgnoreCase(alg) || "EdDSA".equalsIgnoreCase(alg)) {
            byte[] raw = extractBitString(key.getEncoded());
            if (raw != null && raw.length == 32) {
                return "Ed25519:" + toHex(raw);
            }
        }
        return null;
    }

    /** Hex-encodes bytes (lowercase). */
    private static String toHex(byte[] b) {
        StringBuilder sb = new StringBuilder(b.length * 2);
        for (byte v : b) {
            sb.append(Character.forDigit((v >> 4) & 0xF, 16));
            sb.append(Character.forDigit(v & 0xF, 16));
        }
        return sb.toString();
    }

    // ---- D5 boxing helpers ----

    /** Converts Java byte[] to Go []byte (GoSlice with byte[] backing). */
    private static Object toGoBytes(byte[] bytes) {
        if (bytes == null) {
            return null;
        }
        // []byte is byte[]-backed (D5).
        byte[] arr = bytes.clone();
        return new GoSlice(arr, 0, arr.length, arr.length);
    }

    /** Converts Java String to Go string (GoString with UTF-8 bytes). */
    private static Object toGoString(String s) {
        if (s == null) {
            return null;
        }
        return new GoString(s.getBytes(java.nio.charset.StandardCharsets.UTF_8));
    }

    // ---- Helpers ----

    private static byte[] toBytes(Object arg) {
        if (arg instanceof GoSlice) {
            GoSlice gs = (GoSlice) arg;
            // []byte is byte[]-backed (D5).
            byte[] arr = (byte[]) gs.array();
            int off = gs.offset();
            int len = gs.len();
            byte[] bytes = new byte[len];
            System.arraycopy(arr, off, bytes, 0, len);
            return bytes;
        }
        if (arg instanceof byte[]) {
            return (byte[]) arg;
        }
        throw new IllegalArgumentException("expected byte slice, got " + arg.getClass());
    }

    /** Unwraps a DER OCTET STRING (assumes short or long form length). */
    private static byte[] unwrapOctetString(byte[] der) {
        if (der.length < 2 || der[0] != 0x04) {
            return der;
        }
        int len = der[1] & 0xFF;
        int offset = 2;
        if ((len & 0x80) != 0) {
            int numBytes = len & 0x7F;
            len = 0;
            for (int i = 0; i < numBytes; i++) {
                len = (len << 8) | (der[offset++] & 0xFF);
            }
        }
        byte[] out = new byte[len];
        System.arraycopy(der, offset, out, 0, Math.min(len, der.length - offset));
        return out;
    }

    /** Extracts keyIdentifier [0] from AuthorityKeyIdentifier SEQUENCE. */
    private static byte[] extractAuthorityKeyId(byte[] seq) {
        // SEQUENCE { [0] keyIdentifier [1] authorityCertIssuer [2] authorityCertSerialNumber }
        // Look for context-specific tag 0 (0x80)
        for (int i = 0; i < seq.length - 2; i++) {
            if ((seq[i] & 0xFF) == 0x80) {
                int len = seq[i + 1] & 0xFF;
                byte[] kid = new byte[len];
                System.arraycopy(seq, i + 2, kid, 0, Math.min(len, seq.length - i - 2));
                return kid;
            }
        }
        return null;
    }

    /**
     * Extended key usage OIDs as a comma-separated list (empty if none).
     * Backs {@code graalgo.x509.extKeyUsage}.
     */
    public static Object extKeyUsage(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        try {
            List<String> oids = cert.getExtendedKeyUsage();
            if (oids == null || oids.isEmpty()) {
                return toGoString("");
            }
            return toGoString(String.join(",", oids));
        } catch (Exception e) {
            return toGoString("");
        }
    }

    /**
     * Subject Alternative Names encoded as a semicolon-separated list of
     * {@code TYPE:value} pairs (TYPE is DNS, IP, EMAIL or URI). Used by the
     * Go-side Verify hostname check. Empty string if absent/unparseable.
     * Backs {@code graalgo.x509.subjectAltNames}.
     */
    public static Object subjectAltNames(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        try {
            Collection<List<?>> sans = cert.getSubjectAlternativeNames();
            if (sans == null) {
                return toGoString("");
            }
            StringBuilder sb = new StringBuilder();
            for (List<?> san : sans) {
                Integer type = (Integer) san.get(0);
                String tag;
                if (type == 1) {
                    tag = "EMAIL";
                } else if (type == 2) {
                    tag = "DNS";
                } else if (type == 7) {
                    tag = "IP";
                } else if (type == 8) {
                    tag = "URI";
                } else {
                    continue;
                }
                if (sb.length() > 0) {
                    sb.append(';');
                }
                sb.append(tag).append(':').append(san.get(1).toString());
            }
            return toGoString(sb.toString());
        } catch (Exception e) {
            return toGoString("");
        }
    }

    /**
     * Parses a private key (PKCS#8, PKCS#1 or SEC1 EC) and returns a
     * colon-separated descriptor {@code FMT:ALG:hex:hex:...} where FMT is
     * PKCS8, PKCS1 or SEC1 and ALG is RSA, EC or Ed25519. RSA components:
     * n, e, d, p, q. EC components: curveName, x, y, d (SEC1) or
     * curveName, d (PKCS#8; x,y derived). Ed25519: seed. Empty string on
     * failure. Backs {@code graalgo.x509.parsePrivateKey}.
     */
    public static Object parsePrivateKey(Object derArg) {
        byte[] der = toBytes(derArg);
        try {
            // Try PKCS#8 with each known algorithm.
            String parsed = tryPkcs8(der);
            String fmt = "PKCS8";
            if (parsed == null) {
                parsed = tryPkcs1(der);
                fmt = "PKCS1";
            }
            if (parsed == null) {
                parsed = trySec1(der);
                fmt = "SEC1";
            }
            if (parsed == null) {
                return toGoString("");
            }
            return toGoString(fmt + ":" + parsed);
        } catch (Exception e) {
            return toGoString("");
        }
    }

    private static String tryPkcs8(byte[] der) {
        // RSA
        try {
            KeyFactory kf = KeyFactory.getInstance("RSA");
            RSAPrivateCrtKey key =
                    (RSAPrivateCrtKey) kf.generatePrivate(new PKCS8EncodedKeySpec(der));
            return "RSA:" + toHex(key.getModulus().toByteArray()) + ":"
                    + toHex(key.getPublicExponent().toByteArray()) + ":"
                    + toHex(key.getPrivateExponent().toByteArray()) + ":"
                    + toHex(key.getPrimeP().toByteArray()) + ":"
                    + toHex(key.getPrimeQ().toByteArray());
        } catch (Exception ignored) {
        }
        // EC
        try {
            KeyFactory kf = KeyFactory.getInstance("EC");
            ECPrivateKey key =
                    (ECPrivateKey) kf.generatePrivate(new PKCS8EncodedKeySpec(der));
            String curveName = ecCurveName(key);
            if (curveName == null) {
                return null;
            }
            return "EC:" + curveName + ":"
                    + toHex(stripLeadingZero(key.getS().toByteArray()));
        } catch (Exception ignored) {
        }
        // Ed25519
        try {
            KeyFactory kf = KeyFactory.getInstance("Ed25519");
            EdECPrivateKey key =
                    (EdECPrivateKey) kf.generatePrivate(new PKCS8EncodedKeySpec(der));
            byte[] seed = key.getBytes().orElse(null);
            if (seed == null) {
                return null;
            }
            return "Ed25519:" + toHex(seed);
        } catch (Exception ignored) {
        }
        return null;
    }

    private static String tryPkcs1(byte[] der) {
        // PKCS#1 RSAPrivateKey: SEQUENCE { version, n, e, d, p, q, dp, dq, qinv }
        try {
            List<byte[]> ints = parsePkcs1Integers(der);
            if (ints.size() != 9) {
                return null;
            }
            return "RSA:" + toHex(ints.get(1)) + ":" + toHex(ints.get(2)) + ":"
                    + toHex(ints.get(3)) + ":" + toHex(ints.get(4)) + ":"
                    + toHex(ints.get(5));
        } catch (Exception ignored) {
            return null;
        }
    }

    private static String trySec1(byte[] der) {
        // SEC1 ECPrivateKey: SEQUENCE { version, privateKey OCTET STRING,
        //   [0] parameters, [1] publicKey }. Extract d and the curve OID.
        // The public point (x, y) is deliberately NOT derived here: the Go
        // side derives it itself from d via ScalarBaseMult (the synthetic
        // elliptic package already owns big-integer EC arithmetic), so no EC
        // arithmetic is needed on the Java side at all.
        try {
            byte[] d = extractSec1PrivateKey(der);
            String oid = extractSec1CurveOid(der);
            if (d == null || oid == null) {
                return null;
            }
            String curveName = ecCurveNameByOid(oid);
            if (curveName == null) {
                return null;
            }
            return "EC:" + curveName + ":" + toHex(d);
        } catch (Exception ignored) {
            return null;
        }
    }

    private static byte[] extractSec1PrivateKey(byte[] der) {
        // Find the first OCTET STRING (tag 0x04) after the version INTEGER.
        for (int i = 0; i < der.length - 2; i++) {
            if ((der[i] & 0xFF) == 0x04) {
                int len = der[i + 1] & 0xFF;
                if (len > 0 && len <= 66 && i + 2 + len <= der.length) {
                    byte[] d = new byte[len];
                    System.arraycopy(der, i + 2, d, 0, len);
                    return d;
                }
            }
        }
        return null;
    }

    private static String extractSec1CurveOid(byte[] der) {
        // [0] EXPLICIT parameters containing an OID (tag 0x06).
        for (int i = 0; i < der.length - 2; i++) {
            if ((der[i] & 0xFF) == 0xA0) {
                for (int j = i + 1; j < der.length - 2; j++) {
                    if ((der[j] & 0xFF) == 0x06) {
                        int len = der[j + 1] & 0xFF;
                        if (len > 0 && j + 2 + len <= der.length) {
                            byte[] oid = new byte[len];
                            System.arraycopy(der, j + 2, oid, 0, len);
                            return decodeOid(oid);
                        }
                    }
                }
            }
        }
        return null;
    }

    private static String ecCurveNameByOid(String oid) {
        if ("1.2.840.10045.3.1.7".equals(oid)) {
            return "secp256r1";
        }
        if ("1.3.132.0.10".equals(oid)) {
            return "secp256k1";
        }
        if ("1.3.132.0.34".equals(oid)) {
            return "secp384r1";
        }
        if ("1.3.132.0.35".equals(oid)) {
            return "secp521r1";
        }
        if ("1.3.132.0.33".equals(oid)) {
            return "secp224r1";
        }
        return null;
    }

    /**
     * Maps an EC private key to its JDK curve name via the real named-curve
     * OID. Field size alone cannot identify the curve: secp256r1, secp256k1
     * and brainpoolP256r1 all have 256-bit fields, so a field-size table
     * misidentifies secp256k1/brainpool keys as secp256r1 and the Go side
     * then builds the wrong curve (ECDSA verification fails where gc
     * succeeds). The JDK exposes the OID through
     * {@code AlgorithmParameters("EC")} initialized with the key's params:
     * for provider-known named curves the encoding is the bare named-curve
     * OID DER (verified empirically for secp256r1/secp384r1, both direct
     * and PKCS#8-parsed keys; secp256k1 is not supported by this JDK's
     * SunEC, so its table entry is covered by synthetic SEC1 tests).
     * Returns null when the OID is absent or not in
     * {@link #ecCurveNameByOid}.
     */
    private static String ecCurveName(ECPrivateKey key) {
        try {
            AlgorithmParameters p = AlgorithmParameters.getInstance("EC");
            p.init(key.getParams());
            byte[] enc = p.getEncoded();
            if (enc == null || enc.length < 4 || (enc[0] & 0xFF) != 0x06) {
                return null;
            }
            int len = enc[1] & 0xFF;
            if (len <= 0 || len + 2 != enc.length) {
                // Long-form length or trailing bytes: not a bare OID.
                return null;
            }
            byte[] oid = new byte[len];
            System.arraycopy(enc, 2, oid, 0, len);
            return ecCurveNameByOid(decodeOid(oid));
        } catch (Exception e) {
            return null;
        }
    }

    /** Parses all INTEGERs from a PKCS#1 SEQUENCE. */
    private static List<byte[]> parsePkcs1Integers(byte[] der) {
        List<byte[]> out = new ArrayList<>();
        int pos = 0;
        if (der.length < 2 || der[pos++] != 0x30) {
            return out;
        }
        pos = skipLength(der, pos);
        if (pos < 0) {
            return out;
        }
        while (pos < der.length) {
            if (der[pos++] != 0x02) {
                break;
            }
            int lenPos = skipLength(der, pos);
            if (lenPos < 0) {
                break;
            }
            int len = derLength(der, pos);
            pos = lenPos;
            if (len < 0 || pos + len > der.length) {
                break;
            }
            byte[] val = new byte[len];
            System.arraycopy(der, pos, val, 0, len);
            out.add(stripLeadingZero(val));
            pos += len;
        }
        return out;
    }

    /** Decodes a DER OID content octets to dotted-decimal. */
    private static String decodeOid(byte[] oid) {
        if (oid.length == 0) {
            return null;
        }
        StringBuilder sb = new StringBuilder();
        int first = oid[0] & 0xFF;
        sb.append(first / 40).append('.').append(first % 40);
        long val = 0;
        for (int i = 1; i < oid.length; i++) {
            int b = oid[i] & 0xFF;
            val = (val << 7) | (b & 0x7F);
            if ((b & 0x80) == 0) {
                sb.append('.').append(val);
                val = 0;
            }
        }
        return sb.toString();
    }

}
