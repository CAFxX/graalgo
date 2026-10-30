package graalgo.p3x509;

import java.io.ByteArrayInputStream;
import java.math.BigInteger;
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
     * P-521) — the JDK does not expose the curve OID directly.
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
     * n, e, d, p, q. EC components: curveName, d (x, y are derived
     * Go-side via {@code ScalarBaseMult}, the same path PKCS#8 EC keys
     * already used). Ed25519: seed. Backs
     * {@code graalgo.x509.parsePrivateKey}.
     *
     * <p>Failure is distinguishable, never bare {@code ""} (R1-12):
     * {@code ERROR:&lt;kind&gt;} where kind is one of {@code empty-input},
     * {@code unparseable}, {@code malformed-pkcs8}, {@code malformed-pkcs1},
     * {@code malformed-sec1}, {@code missing-curve-parameters},
     * {@code unsupported-algorithm}, {@code unsupported-curve} or
     * {@code invalid-key}. The Go side (synth_crypto_x509.go) turns
     * {@code ERROR:*} into a descriptive {@code x509} error; inputs
     * matching no known format stay {@code unparseable} and keep the
     * previous generic Go error text. The three format attempts return
     * either a descriptor, {@code null} (not this format), or an
     * {@code ERROR:*} sentinel (this format, invalid) — the formats are
     * disjoint by their second SEQUENCE child (SEQUENCE for PKCS#8,
     * INTEGER for PKCS#1, OCTET STRING for SEC1), so the first format
     * that positively identifies the input wins and later attempts never
     * relabel its failure.
     */
    public static Object parsePrivateKey(Object derArg) {
        byte[] der = toBytes(derArg);
        try {
            if (der.length == 0) {
                return toGoString("ERROR:empty-input");
            }
            // PKCS#8 first: the JDK attempts run before the
            // algorithm-OID sniff, so a valid key is never mislabeled;
            // only when every JDK attempt fails does the sniff decide
            // whether the input was PKCS#8-shaped (malformed /
            // unsupported) or some other format entirely.
            String parsed = tryPkcs8(der);
            if (parsed != null) {
                if (parsed.startsWith("ERROR:")) {
                    return toGoString(parsed);
                }
                return toGoString("PKCS8:" + parsed);
            }
            String pkcs8Error = pkcs8ErrorKind(der);
            if (pkcs8Error != null) {
                return toGoString("ERROR:" + pkcs8Error);
            }
            parsed = tryPkcs1(der);
            if (parsed != null) {
                if (parsed.startsWith("ERROR:")) {
                    return toGoString(parsed);
                }
                return toGoString("PKCS1:" + parsed);
            }
            parsed = trySec1(der);
            if (parsed != null) {
                if (parsed.startsWith("ERROR:")) {
                    return toGoString(parsed);
                }
                return toGoString("SEC1:" + parsed);
            }
            return toGoString("ERROR:unparseable");
        } catch (Exception e) {
            return toGoString("ERROR:unparseable");
        }
    }

    /**
     * PKCS#8 format sniff: walks the top SEQUENCE expecting
     * {@code INTEGER version, SEQUENCE { OID algorithm, ... }}. Returns
     * null when the input is not PKCS#8-shaped (the other formats try
     * next), {@code malformed-pkcs8} when the shape holds but the JDK
     * rejects every known algorithm for it, or
     * {@code unsupported-algorithm} when the algorithm OID is present
     * but unknown. Only called when the JDK attempts have already
     * failed, so a non-null result means the input really is PKCS#8.
     */
    private static String pkcs8ErrorKind(byte[] der) {
        DerElem seq = readElem(der, 0, der.length);
        if (seq == null || seq.tag != 0x30) {
            return null;
        }
        DerElem version = readElem(der, seq.content, seq.end);
        if (version == null || version.tag != 0x02) {
            return null;
        }
        DerElem alg = readElem(der, version.end, seq.end);
        if (alg == null || alg.tag != 0x30) {
            return null;
        }
        DerElem oid = readElem(der, alg.content, alg.end);
        if (oid == null || oid.tag != 0x06) {
            return "malformed-pkcs8";
        }
        String algOid = decodeOid(copyOfRange(der, oid.content, oid.end));
        if (algOid == null) {
            return "malformed-pkcs8";
        }
        if ("1.2.840.113549.1.1.1".equals(algOid)
                || "1.2.840.10045.2.1".equals(algOid)
                || "1.3.101.112".equals(algOid)) {
            return "malformed-pkcs8";
        }
        return "unsupported-algorithm";
    }

    /** One DER TLV element, bounds-checked against {@code limit}. */
    private static final class DerElem {
        final int tag;
        final int content;
        final int len;
        final int end;

        DerElem(int tag, int content, int len, int end) {
            this.tag = tag;
            this.content = content;
            this.len = len;
            this.end = end;
        }
    }

    /**
     * Reads one DER TLV element at {@code pos}, requiring its content to
     * lie within {@code limit}. Returns null on any structural defect:
     * truncated tag/length, indefinite or over-long length form, or
     * content running past {@code limit}. Lengths accumulate in a long
     * so a 4-byte length can never wrap negative past the bounds check.
     */
    private static DerElem readElem(byte[] der, int pos, int limit) {
        if (pos < 0 || pos + 2 > limit || pos + 2 > der.length) {
            return null;
        }
        int tag = der[pos] & 0xFF;
        int lb = der[pos + 1] & 0xFF;
        int content;
        long len;
        if ((lb & 0x80) == 0) {
            content = pos + 2;
            len = lb;
        } else {
            int num = lb & 0x7F;
            if (num == 0 || num > 4 || pos + 2 + num > limit) {
                return null;
            }
            content = pos + 2 + num;
            len = 0;
            for (int i = 0; i < num; i++) {
                len = (len << 8) | (der[pos + 2 + i] & 0xFF);
            }
        }
        if (len > limit - content) {
            return null;
        }
        return new DerElem(tag, content, (int) len, (int) (content + len));
    }

    private static byte[] copyOfRange(byte[] der, int from, int to) {
        byte[] out = new byte[to - from];
        System.arraycopy(der, from, out, 0, out.length);
        return out;
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
            int fieldSize = key.getParams().getCurve().getField().getFieldSize();
            String curveName = ecCurveName(fieldSize);
            if (curveName == null) {
                // The JDK parsed a PKCS#8 EC key on a curve graalgo does
                // not name: positively identified, unsupported.
                return "ERROR:unsupported-curve";
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

    /**
     * PKCS#1 RSAPrivateKey: SEQUENCE { version, n, e, d, p, q, dp, dq,
     * qinv }. Returns the descriptor, null when the input is not an
     * integer SEQUENCE (another format's turn), or
     * {@code ERROR:malformed-pkcs1} when it is an integer SEQUENCE of
     * the wrong arity — at least two INTEGERs must have been read so a
     * PKCS#8/SEC1 input (whose walk breaks at the second child) is
     * never mislabeled.
     */
    private static String tryPkcs1(byte[] der) {
        try {
            if (der.length < 2 || der[0] != 0x30) {
                return null;
            }
            List<byte[]> ints = parsePkcs1Integers(der);
            if (ints.size() == 9) {
                return "RSA:" + toHex(ints.get(1)) + ":" + toHex(ints.get(2)) + ":"
                        + toHex(ints.get(3)) + ":" + toHex(ints.get(4)) + ":"
                        + toHex(ints.get(5));
            }
            if (ints.size() >= 2) {
                return "ERROR:malformed-pkcs1";
            }
            return null;
        } catch (Exception ignored) {
            return null;
        }
    }

    /**
     * SEC1 ECPrivateKey, parsed with a real DER walk (R1-12) — never by
     * scanning for tag bytes: the old scanners took the first
     * {@code 0x04}/{@code 0xA0}/{@code 0x06} anywhere in the input, so a
     * stray byte inside an earlier value (or inside the public key)
     * silently selected the wrong private key or curve. Returns the
     * 3-part descriptor {@code EC:curveName:d} (x, y are derived Go-side
     * via {@code ScalarBaseMult}), null when the input is not a SEC1
     * SEQUENCE, or an {@code ERROR:*} sentinel when it is SEC1-shaped
     * but invalid.
     *
     * <p>No scalar multiplication happens here at all: the previous
     * textbook double-and-add (secret-dependent branch per bit, plus a
     * {@code modInverse} that throws on 0 and was swallowed to
     * {@code ""}) is deleted. The Go side already derives x, y from d
     * for PKCS#8 EC keys; SEC1 now uses that same path, so no
     * private-key arithmetic remains in this class.
     */
    private static String trySec1(byte[] der) {
        try {
            DerElem seq = readElem(der, 0, der.length);
            if (seq == null || seq.tag != 0x30) {
                return null;
            }
            DerElem version = readElem(der, seq.content, seq.end);
            if (version == null || version.tag != 0x02 || version.len < 1) {
                return null;
            }
            DerElem priv = readElem(der, version.end, seq.end);
            if (priv == null || priv.tag != 0x04) {
                return null;
            }
            if (priv.len < 1 || priv.len > 66) {
                return "ERROR:malformed-sec1";
            }
            byte[] d = copyOfRange(der, priv.content, priv.end);
            int pos = priv.end;
            String oid = null;
            if (pos < seq.end && (der[pos] & 0xFF) == 0xA0) {
                DerElem params = readElem(der, pos, seq.end);
                if (params == null) {
                    return "ERROR:malformed-sec1";
                }
                DerElem oidElem = readElem(der, params.content, params.end);
                if (oidElem == null || oidElem.tag != 0x06 || oidElem.end != params.end) {
                    return "ERROR:malformed-sec1";
                }
                oid = decodeOid(copyOfRange(der, oidElem.content, oidElem.end));
                if (oid == null) {
                    return "ERROR:malformed-sec1";
                }
                pos = params.end;
            }
            if (pos < seq.end && (der[pos] & 0xFF) == 0xA1) {
                DerElem pub = readElem(der, pos, seq.end);
                if (pub == null) {
                    return "ERROR:malformed-sec1";
                }
                pos = pub.end;
            }
            if (pos != seq.end) {
                return "ERROR:malformed-sec1";
            }
            if (oid == null) {
                return "ERROR:missing-curve-parameters";
            }
            String curveName = ecCurveNameByOid(oid);
            if (curveName == null) {
                return "ERROR:unsupported-curve";
            }
            if (new BigInteger(1, d).signum() == 0) {
                return "ERROR:invalid-key";
            }
            return "EC:" + curveName + ":" + toHex(d);
        } catch (Exception ignored) {
            return null;
        }
    }


    private static String ecCurveNameByOid(String oid) {
        if ("1.2.840.10045.3.1.7".equals(oid)) {
            return "secp256r1";
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

    /** Maps an EC field size in bits to a JDK curve name. */
    private static String ecCurveName(int fieldSize) {
        if (fieldSize == 224) {
            return "secp224r1";
        }
        if (fieldSize == 256) {
            return "secp256r1";
        }
        if (fieldSize == 384) {
            return "secp384r1";
        }
        if (fieldSize == 521) {
            return "secp521r1";
        }
        return null;
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
