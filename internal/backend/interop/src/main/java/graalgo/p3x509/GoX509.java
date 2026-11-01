package graalgo.p3x509;

import java.io.ByteArrayInputStream;
import java.math.BigInteger;
import java.security.AlgorithmParameters;
import java.security.KeyFactory;
import java.security.Signature;
import java.security.cert.CertificateException;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;
import java.security.interfaces.ECPrivateKey;
import java.security.interfaces.EdECPrivateKey;
import java.security.interfaces.RSAPrivateCrtKey;
import java.security.spec.ECGenParameterSpec;
import java.security.spec.PKCS8EncodedKeySpec;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collection;
import java.util.List;

import javax.crypto.KeyAgreement;

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
 * <p>Private-key parsing (F-06 remediation, 2026-11-01) is likewise
 * JDK-delegated as far as the public JCA APIs reach, and differentially
 * tested against gc {@code crypto/x509} (see
 * {@code graalgo.p3x509.GoX509Test}):
 * <ul>
 *   <li>PKCS#8 and PKCS#1 structures are parsed with {@link KeyFactory}
 *       where the JDK accepts them; SEC1/PKCS#1/SPKI <em>framing</em>
 *       (tag/length/value walk) uses the structured {@link DerCursor}
 *       below — never byte pattern-matching over key material, which
 *       previously mistook scalar bytes for ASN.1 tags.</li>
 *   <li>SEC1 public points for P-256/P-384/P-521 are derived via
 *       {@code KeyAgreement("ECDH")} against the generator (giving X),
 *       a field square root (giving the two candidate Y values), and
 *       {@code Signature} verification to pick the right one. The group
 *       law itself is never reimplemented for these curves.</li>
 *   <li>P-224 is the documented exception: SunEC on this JDK rejects
 *       secp224r1 for ECDH, ECDSA and key generation alike
 *       ("Curve not supported"), so no public JDK API can derive a
 *       P-224 public point. P-224 SEC1 derivation keeps the
 *       {@link #scalarMultiply} fallback, fed only by the structured
 *       parser and explicit D-range validation. Re-test trigger: a JDK
 *       whose SunEC (or a future EC scalar-multiplication API) supports
 *       secp224r1 — then delete the fallback and route P-224 through
 *       the ECDH path.</li>
 * </ul>
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
     * Structured walk: SEQUENCE { AlgorithmIdentifier SEQUENCE, BIT STRING }.
     */
    private static byte[] extractBitString(byte[] spki) {
        DerCursor.Tlv top = DerCursor.parse(spki, 0);
        if (top == null || top.tag != 0x30) {
            return null;
        }
        List<DerCursor.Tlv> children = DerCursor.children(spki, top);
        if (children == null || children.size() != 2) {
            return null;
        }
        DerCursor.Tlv bitString = children.get(1);
        if (bitString.tag != 0x03 || bitString.length < 1) {
            return null;
        }
        byte[] content = DerCursor.content(spki, bitString);
        if (content[0] != 0x00) {
            return null;
        }
        byte[] out = new byte[content.length - 1];
        System.arraycopy(content, 1, out, 0, out.length);
        return out;
    }

    /**
     * Minimal structured DER reader: tag/length/value walk with bounds
     * checks, replacing the byte pattern-matching scans this file used
     * to have (which could mistake key-material bytes for ASN.1 tags).
     * Only single-byte tags are supported (all tags this file needs are);
     * lengths may be short or long form, never indefinite; every returned
     * value lies inside the input. Malformed input yields null, never an
     * exception and never a slice past the input end.
     *
     * <p>Trailing bytes <em>after</em> a top-level SEQUENCE are ignored by
     * callers, matching gc's {@code asn1.Unmarshal} (which returns trailing
     * bytes unchecked); bytes <em>inside</em> a SEQUENCE must be exactly
     * consumed by well-formed children.
     */
    private static final class DerCursor {
        /** One TLV: tag byte, content offset/length, total encoded length. */
        private static final class Tlv {
            private final int tag;
            private final int contentOff;
            private final int length;
            private final int totalLen;

            private Tlv(int tag, int contentOff, int length, int totalLen) {
                this.tag = tag;
                this.contentOff = contentOff;
                this.length = length;
                this.totalLen = totalLen;
            }
        }

        private DerCursor() {}

        /** Parses one TLV at {@code pos}, or null if malformed/truncated. */
        private static Tlv parse(byte[] der, int pos) {
            if (der == null || pos < 0 || pos + 2 > der.length) {
                return null;
            }
            int tag = der[pos] & 0xFF;
            if ((tag & 0x1F) == 0x1F) {
                return null; // multi-byte tag: not needed here
            }
            int lenByte = der[pos + 1] & 0xFF;
            int contentOff;
            int length;
            if ((lenByte & 0x80) == 0) {
                contentOff = pos + 2;
                length = lenByte;
            } else {
                int num = lenByte & 0x7F;
                if (num == 0 || num > 4 || pos + 2 + num > der.length) {
                    return null; // indefinite or oversized length
                }
                length = 0;
                for (int i = 0; i < num; i++) {
                    length = (length << 8) | (der[pos + 2 + i] & 0xFF);
                }
                contentOff = pos + 2 + num;
            }
            if (length < 0 || contentOff + length > der.length) {
                return null;
            }
            return new Tlv(tag, contentOff, length, contentOff + length - pos);
        }

        /**
         * Parses the children of a constructed TLV: they must exactly fill
         * its content, each well-formed. Returns null otherwise.
         */
        private static List<Tlv> children(byte[] der, Tlv parent) {
            List<Tlv> out = new ArrayList<>();
            int pos = parent.contentOff;
            int end = parent.contentOff + parent.length;
            while (pos < end) {
                Tlv child = parse(der, pos);
                if (child == null || pos + child.totalLen > end) {
                    return null;
                }
                out.add(child);
                pos += child.totalLen;
            }
            if (pos != end) {
                return null;
            }
            return out;
        }

        /** Copies a TLV's content bytes. */
        private static byte[] content(byte[] der, Tlv tlv) {
            byte[] out = new byte[tlv.length];
            System.arraycopy(der, tlv.contentOff, out, 0, out.length);
            return out;
        }

        /** Content bytes as an unsigned integer (leading zeros allowed). */
        private static BigInteger unsigned(byte[] der, Tlv tlv) {
            return new BigInteger(1, content(der, tlv));
        }
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

    /** Parses PKCS1 DER into "n_hex:e" (SEQUENCE { n, e, ... }). */
    public static Object parsePkcs1(Object derArg) {
        byte[] der = toBytes(derArg);
        try {
            DerCursor.Tlv top = DerCursor.parse(der, 0);
            if (top == null || top.tag != 0x30) {
                return null;
            }
            List<DerCursor.Tlv> children = DerCursor.children(der, top);
            if (children == null || children.size() < 2) {
                return null;
            }
            DerCursor.Tlv nTlv = children.get(0);
            DerCursor.Tlv eTlv = children.get(1);
            if (nTlv.tag != 0x02 || eTlv.tag != 0x02) {
                return null;
            }
            byte[] nBytes = stripLeadingZero(DerCursor.content(der, nTlv));
            BigInteger e = DerCursor.unsigned(der, eTlv);
            if (e.signum() <= 0) {
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
        // EC: identified by the AlgorithmIdentifier parameters OID, never
        // by field size — secp256k1 and secp256r1 share field size 256,
        // and the old field-size mapping silently misidentified a k1 key
        // as P-256 (GoX509Test.testPkcs8EcEdges pins the rejection).
        String ec = pkcs8EcDescriptor(der);
        if (ec != null) {
            return ec;
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

    /**
     * PKCS#8 EC descriptor ("EC:curve:d"), or null if {@code der} is not a
     * usable PKCS#8 EC key. Curve identification is by OID (see above);
     * D is range-validated explicitly because SunEC's KeyFactory accepts
     * out-of-range scalars (S == 0, S == order) that gc rejects.
     * A parameters-less EC key (legal PKCS#8, rejected by SunEC's
     * KeyFactory) falls back to the embedded SEC1 key, as gc does.
     */
    private static String pkcs8EcDescriptor(byte[] der) {
        try {
            DerCursor.Tlv top = DerCursor.parse(der, 0);
            if (top == null || top.tag != 0x30) {
                return null;
            }
            List<DerCursor.Tlv> fields = DerCursor.children(der, top);
            if (fields == null || fields.size() < 3) {
                return null;
            }
            if (fields.get(0).tag != 0x02 || fields.get(1).tag != 0x30
                    || fields.get(2).tag != 0x04) {
                return null;
            }
            List<DerCursor.Tlv> algId = DerCursor.children(der, fields.get(1));
            if (algId == null || algId.isEmpty() || algId.get(0).tag != 0x06) {
                return null;
            }
            String algOid = decodeOid(DerCursor.content(der, algId.get(0)));
            if (!"1.2.840.10045.2.1".equals(algOid)) {
                return null; // not an EC key (RSA/Ed branches handle theirs)
            }
            if (algId.size() == 1) {
                // No parameters: gc derives the curve from the embedded
                // SEC1 key. The descriptor keeps the PKCS#8 format.
                Sec1Key inner = parseSec1(DerCursor.content(der, fields.get(2)));
                if (inner == null) {
                    return null;
                }
                return "EC:" + inner.curveName + ":"
                        + toHex(stripLeadingZero(inner.d.toByteArray()));
            }
            if (algId.get(1).tag != 0x06) {
                return null; // non-OID EC parameters: not a named curve
            }
            String curveName = ecCurveNameByOid(
                    decodeOid(DerCursor.content(der, algId.get(1))));
            if (curveName == null) {
                return null; // e.g. secp256k1: gc rejects, so do we
            }
            KeyFactory kf = KeyFactory.getInstance("EC");
            ECPrivateKey key =
                    (ECPrivateKey) kf.generatePrivate(new PKCS8EncodedKeySpec(der));
            BigInteger s = key.getS();
            java.security.spec.ECParameterSpec params = key.getParams();
            if (s.signum() <= 0 || s.compareTo(params.getOrder()) >= 0) {
                return null; // gc rejects D == 0 and D >= order
            }
            return "EC:" + curveName + ":"
                    + toHex(stripLeadingZero(s.toByteArray()));
        } catch (Exception ignored) {
            return null;
        }
    }

    /** A parsed SEC1 key: curve, scalar, derived public point, raw D bytes. */
    private static final class Sec1Key {
        private final String curveName;
        private final BigInteger d;
        private final BigInteger x;
        private final BigInteger y;
        private final byte[] dRaw;

        private Sec1Key(String curveName, BigInteger d, BigInteger x,
                BigInteger y, byte[] dRaw) {
            this.curveName = curveName;
            this.d = d;
            this.x = x;
            this.y = y;
            this.dRaw = dRaw;
        }
    }

    /**
     * Parses a SEC1 ECPrivateKey (RFC 5915) with a structured DER walk and
     * gc-compatible verdicts (see GoX509Test): version must be 1;
     * parameters [0] must be present and name a known curve; the [1]
     * public key, if present, is ignored and the point is derived from D;
     * further children are ignored; trailing bytes after the SEQUENCE are
     * ignored; D must satisfy 1 &lt;= D &lt; order after stripping leading
     * zero padding (over-long D with a non-zero leading byte is rejected).
     * Returns null on any deviation.
     */
    private static Sec1Key parseSec1(byte[] der) {
        try {
            DerCursor.Tlv top = DerCursor.parse(der, 0);
            if (top == null || top.tag != 0x30) {
                return null;
            }
            List<DerCursor.Tlv> ch = DerCursor.children(der, top);
            if (ch == null || ch.size() < 2) {
                return null;
            }
            if (ch.get(0).tag != 0x02
                    || !DerCursor.unsigned(der, ch.get(0)).equals(BigInteger.ONE)) {
                return null; // version != 1: gc rejects
            }
            if (ch.get(1).tag != 0x04) {
                return null;
            }
            byte[] dRaw = DerCursor.content(der, ch.get(1));
            if (ch.size() < 3 || ch.get(2).tag != 0xA0) {
                return null; // gc: unknown elliptic curve without parameters
            }
            List<DerCursor.Tlv> params = DerCursor.children(der, ch.get(2));
            if (params == null || params.size() != 1 || params.get(0).tag != 0x06) {
                return null;
            }
            String curveName = ecCurveNameByOid(
                    decodeOid(DerCursor.content(der, params.get(0))));
            if (curveName == null) {
                return null;
            }
            java.security.spec.ECParameterSpec spec = ecSpecForName(curveName);
            if (spec == null) {
                return null;
            }
            int size = (spec.getCurve().getField().getFieldSize() + 7) / 8;
            int first = 0;
            while (first < dRaw.length && dRaw[first] == 0) {
                first++;
            }
            if (first == dRaw.length) {
                return null; // D == 0
            }
            if (dRaw.length - first > size) {
                return null; // invalid private key length
            }
            byte[] stripped = Arrays.copyOfRange(dRaw, first, dRaw.length);
            BigInteger d = new BigInteger(1, stripped);
            if (d.compareTo(spec.getOrder()) >= 0) {
                return null; // D >= order
            }
            java.security.spec.ECPoint w = derivePublic(spec, curveName, d);
            if (w == null) {
                return null;
            }
            return new Sec1Key(curveName, d, w.getAffineX(), w.getAffineY(), dRaw);
        } catch (Exception ignored) {
            return null;
        }
    }

    private static String trySec1(byte[] der) {
        Sec1Key key = parseSec1(der);
        if (key == null) {
            return null;
        }
        java.security.spec.ECParameterSpec spec = ecSpecForName(key.curveName);
        if (spec == null) {
            return null;
        }
        return "EC:" + key.curveName + ":" + toHex(toFixed(key.x, spec))
                + ":" + toHex(toFixed(key.y, spec)) + ":" + toHex(key.dRaw);
    }

    /**
     * Derives the public point for scalar {@code d} (already validated:
     * 1 &lt;= d &lt; order). P-256/P-384/P-521 go through the JDK:
     * ECDH against the generator yields X, a field square root yields
     * the two candidate Y values, and an ECDSA sign/verify oracle picks
     * the right one. P-224 keeps the {@link #scalarMultiply} fallback:
     * SunEC rejects secp224r1 outright (see class javadoc).
     */
    private static java.security.spec.ECPoint derivePublic(
            java.security.spec.ECParameterSpec spec, String curveName,
            BigInteger d) {
        if ("secp224r1".equals(curveName)) {
            return scalarMultiply(spec, d);
        }
        try {
            KeyFactory kf = KeyFactory.getInstance("EC");
            ECPrivateKey priv = (ECPrivateKey) kf.generatePrivate(
                    new java.security.spec.ECPrivateKeySpec(d, spec));
            java.security.interfaces.ECPublicKey genPub =
                    (java.security.interfaces.ECPublicKey) kf.generatePublic(
                            new java.security.spec.ECPublicKeySpec(
                                    spec.getGenerator(), spec));
            KeyAgreement ka = KeyAgreement.getInstance("ECDH");
            ka.init(priv);
            ka.doPhase(genPub, true);
            BigInteger x = new BigInteger(1, ka.generateSecret());
            return derivePointFromX(spec, curveName, priv, x);
        } catch (Exception ignored) {
            return null;
        }
    }

    /**
     * Recovers the full point for a known X. The Y candidates come from a
     * field square root (all ECDH-covered curves here have p = 3 mod 4);
     * the sign is resolved by verifying a fresh ECDSA signature against
     * each candidate point. Fails closed if neither or both verify, or
     * if X is not on the curve. Note the sign bit is unrecoverable from
     * ECDH output alone (ECDH secrets are X-coordinates by design), so
     * the verify oracle is load-bearing, not decorative.
     */
    private static java.security.spec.ECPoint derivePointFromX(
            java.security.spec.ECParameterSpec spec, String curveName,
            ECPrivateKey priv, BigInteger x) throws Exception {
        java.security.spec.ECFieldFp field =
                (java.security.spec.ECFieldFp) spec.getCurve().getField();
        BigInteger p = field.getP();
        if (!p.mod(BigInteger.valueOf(4)).equals(BigInteger.valueOf(3))) {
            return null;
        }
        BigInteger a = spec.getCurve().getA();
        BigInteger b = spec.getCurve().getB();
        BigInteger rhs = x.modPow(BigInteger.valueOf(3), p)
                .add(a.multiply(x)).add(b).mod(p);
        BigInteger y0 = rhs.modPow(
                p.add(BigInteger.ONE).divide(BigInteger.valueOf(4)), p);
        if (!y0.modPow(BigInteger.TWO, p).equals(rhs)) {
            return null; // X is not on the curve
        }
        String sigAlg;
        if ("secp384r1".equals(curveName)) {
            sigAlg = "SHA384withECDSA";
        } else if ("secp521r1".equals(curveName)) {
            sigAlg = "SHA512withECDSA";
        } else {
            sigAlg = "SHA256withECDSA";
        }
        Signature signer = Signature.getInstance(sigAlg);
        signer.initSign(priv);
        byte[] msg = new byte[]{0x47, 0x6F, 0x58, 0x35, 0x30, 0x39};
        signer.update(msg);
        byte[] sig = signer.sign();
        KeyFactory kf = KeyFactory.getInstance("EC");
        BigInteger[] candidates = y0.signum() == 0
                ? new BigInteger[]{y0}
                : new BigInteger[]{y0, p.subtract(y0)};
        java.security.spec.ECPoint found = null;
        for (BigInteger y : candidates) {
            java.security.interfaces.ECPublicKey cand =
                    (java.security.interfaces.ECPublicKey) kf.generatePublic(
                            new java.security.spec.ECPublicKeySpec(
                                    new java.security.spec.ECPoint(x, y), spec));
            Signature verifier = Signature.getInstance(sigAlg);
            verifier.initVerify(cand);
            verifier.update(msg);
            if (verifier.verify(sig)) {
                if (found != null) {
                    return null; // both verify: fail closed
                }
                found = new java.security.spec.ECPoint(x, y);
            }
        }
        return found;
    }

    private static byte[] toFixed(BigInteger v, java.security.spec.ECParameterSpec spec) {
        int size = (spec.getCurve().getField().getFieldSize() + 7) / 8;
        byte[] raw = stripLeadingZero(v.toByteArray());
        if (raw.length == size) {
            return raw;
        }
        byte[] out = new byte[size];
        System.arraycopy(raw, 0, out, size - raw.length,
                Math.min(raw.length, size));
        return out;
    }

    private static java.security.spec.ECParameterSpec ecSpecForName(String name) {
        try {
            AlgorithmParameters p = AlgorithmParameters.getInstance("EC");
            p.init(new ECGenParameterSpec(name));
            return p.getParameterSpec(
                    java.security.spec.ECParameterSpec.class);
        } catch (Exception e) {
            return null;
        }
    }

    /**
     * P-224-only fallback (see class javadoc): SunEC rejects secp224r1
     * for every operation that could derive a public point, so this
     * double-and-add is the only route for that curve. It is fed only
     * validated scalars (1 &lt;= s &lt; order) from {@link #parseSec1}.
     * Delete when the JDK covers P-224; do not extend to other curves.
     */
    private static java.security.spec.ECPoint scalarMultiply(
            java.security.spec.ECParameterSpec spec, BigInteger s) {
        // Double-and-add on the curve.
        java.security.spec.EllipticCurve curve = spec.getCurve();
        java.security.spec.ECPoint g = spec.getGenerator();
        java.security.spec.ECPoint result = java.security.spec.ECPoint.POINT_INFINITY;
        java.security.spec.ECPoint addend = g;
        BigInteger k = s.mod(spec.getOrder());
        while (k.signum() > 0) {
            if (k.testBit(0)) {
                result = ecAdd(curve, result, addend);
            }
            addend = ecDouble(curve, addend);
            k = k.shiftRight(1);
        }
        return result;
    }

    private static java.security.spec.ECPoint ecAdd(
            java.security.spec.EllipticCurve curve,
            java.security.spec.ECPoint p, java.security.spec.ECPoint q) {
        if (p.equals(java.security.spec.ECPoint.POINT_INFINITY)) {
            return q;
        }
        if (q.equals(java.security.spec.ECPoint.POINT_INFINITY)) {
            return p;
        }
        BigInteger prime = ((java.security.spec.ECFieldFp) curve.getField()).getP();
        BigInteger a = curve.getA();
        BigInteger x1 = p.getAffineX(), y1 = p.getAffineY();
        BigInteger x2 = q.getAffineX(), y2 = q.getAffineY();
        BigInteger lambda;
        if (x1.equals(x2)) {
            if (!y1.equals(y2)) {
                return java.security.spec.ECPoint.POINT_INFINITY;
            }
            // Doubling.
            lambda = x1.multiply(x1).multiply(BigInteger.valueOf(3)).add(a)
                    .multiply(y1.shiftLeft(1).modInverse(prime)).mod(prime);
        } else {
            lambda = y2.subtract(y1).multiply(x2.subtract(x1).modInverse(prime))
                    .mod(prime);
        }
        BigInteger x3 = lambda.multiply(lambda).subtract(x1).subtract(x2).mod(prime);
        BigInteger y3 = lambda.multiply(x1.subtract(x3)).subtract(y1).mod(prime);
        return new java.security.spec.ECPoint(x3, y3);
    }

    private static java.security.spec.ECPoint ecDouble(
            java.security.spec.EllipticCurve curve, java.security.spec.ECPoint p) {
        return ecAdd(curve, p, p);
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

    /**
     * Parses all INTEGERs from a PKCS#1 SEQUENCE. Returns an empty list
     * unless the SEQUENCE holds exactly well-formed INTEGER children;
     * trailing bytes after the SEQUENCE are ignored (gc-compatible).
     */
    private static List<byte[]> parsePkcs1Integers(byte[] der) {
        List<byte[]> out = new ArrayList<>();
        DerCursor.Tlv top = DerCursor.parse(der, 0);
        if (top == null || top.tag != 0x30) {
            return out;
        }
        List<DerCursor.Tlv> children = DerCursor.children(der, top);
        if (children == null) {
            return out;
        }
        for (DerCursor.Tlv child : children) {
            if (child.tag != 0x02) {
                return new ArrayList<>();
            }
            out.add(stripLeadingZero(DerCursor.content(der, child)));
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
        if (first < 40) {
            sb.append(0).append('.').append(first);
        } else if (first < 80) {
            sb.append(1).append('.').append(first - 40);
        } else {
            sb.append(2).append('.').append(first - 80);
        }
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
