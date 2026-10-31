package graalgo.p3x509;

import java.io.ByteArrayInputStream;
import java.math.BigInteger;
import java.security.AlgorithmParameters;
import java.security.KeyFactory;
import java.security.cert.CertificateException;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;
import java.security.interfaces.ECPrivateKey;
import java.security.interfaces.ECPublicKey;
import java.security.interfaces.EdECPrivateKey;
import java.security.interfaces.EdECPublicKey;
import java.security.interfaces.RSAPrivateCrtKey;
import java.security.interfaces.RSAPublicKey;
import java.security.spec.ECGenParameterSpec;
import java.security.spec.ECPublicKeySpec;
import java.security.spec.PKCS8EncodedKeySpec;
import java.security.spec.RSAPrivateCrtKeySpec;
import java.security.spec.X509EncodedKeySpec;
import java.util.ArrayList;
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
 * <p>Delegation status per operation (remediation of review P1-6,
 * 2026-10-31; the prior revision hand-rolled curve arithmetic and scanned
 * DER bytes for tags instead of parsing structure):
 * <ul>
 *   <li>Certificate parsing, SPKI public keys, PKCS#8 private keys:
 *       fully delegated to {@link CertificateFactory} / {@link KeyFactory}.
 *   <li>PKCS#1 public keys ({@link #parsePkcs1}): structure is validated
 *       by a strict sequential DER reader, then the key is built by the
 *       JDK from a canonical re-encoding wrapped as SPKI; the returned
 *       components are read back from the JDK key object. The JDK has no
 *       PKCS#1 import spec, so the structure check cannot itself be
 *       delegated — but no key material is derived by hand.
 *   <li>PKCS#1 private keys ({@link #parsePrivateKey} PKCS1 path):
 *       same strict reader, plus gc-mirror consistency checks
 *       (product of primes, d&middot;e congruences, CRT consistency —
 *       the deterministic part of gc's {@code Validate}), and the
 *       standard 9-integer form is additionally materialised as a JDK
 *       {@link RSAPrivateCrtKey} whose components are read back.
 *       Short forms (6–8 integers, CRT parameters omitted) cannot be
 *       represented by any JDK key type ({@code RSAPrivateCrtKey}
 *       requires all nine; {@code RSAPrivateKey} drops p and q, which
 *       the descriptor must carry), so their components come from the
 *       strict parse; this is the one documented JDK-representation gap.
 *   <li>SEC1 EC private keys: strict sequential parse (see
 *       {@link #parseSec1}), JDK re-validation (the SEC1 bytes wrapped
 *       as PKCS#8 must parse to an {@link ECPrivateKey} with the same
 *       scalar and curve), and an ECDH cross-check of the derived
 *       public point (below). The embedded [1] public key is ignored,
 *       exactly as gc's {@code ParseECPrivateKey} ignores it.
 *   <li>EC scalar multiplication ({@link #scalarMultiply}): RETAINED
 *       hand-rolled, for a specific, verifiable reason — the public JCA
 *       API exposes no scalar-multiplication operation. {@code
 *       KeyAgreement("ECDH")} returns only the shared secret, i.e. the
 *       x-coordinate; recovering y from x would need a second
 *       hand-rolled primitive (modular square root) plus a
 *       sign-disambiguation test, strictly worse than one audited
 *       double-and-add over JDK-supplied curve parameters. The result
 *       is therefore verified three ways before use: it must lie on
 *       the curve, it must not be infinity, and its x-coordinate must
 *       equal the JDK ECDH shared secret for the same scalar. A
 *       mismatch fails the parse loudly instead of yielding wrong key
 *       material.
 * </ul>
 *
 * <p>gc-parity rules for the strict readers (pinned by differential tests
 * against gc {@code crypto/x509} in {@code GoX509Test}; gc's
 * {@code encoding/asn1} struct decoding is the reference, quirks
 * included): lengths must be canonical DER (no indefinite form, no
 * non-minimal long form); inside a SEQUENCE, elements after the last
 * struct field are ignored (even malformed ones — gc stops parsing the
 * struct there); a SEC1 value ignores trailing bytes after its top
 * SEQUENCE (gc's {@code Unmarshal} returns them as rest, which
 * {@code ParseECPrivateKey} ignores), while PKCS#1 rejects trailing
 * bytes (gc checks rest explicitly). Optional struct fields are skipped
 * on tag mismatch, and parsing continues with the next field.
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
        // KeyIdentifier ::= OCTET STRING). Malformed values yield null
        // (absent) instead of truncated bytes.
        byte[] once = unwrapOctetString(ext);
        if (once == null) {
            return null;
        }
        return toGoBytes(unwrapOctetString(once));
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
        if (unwrapped == null) {
            return null;
        }
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
     * Reconstructed from the JDK {@link EdECPublicKey} point (RFC 8032
     * encoding: little-endian y with the x-odd bit in the top bit) —
     * no DER scanning: the prior revision extracted the SPKI BIT STRING
     * by walking tags by hand.
     */
    public static Object ed25519Raw(Object certArg) {
        X509Certificate cert = unwrapCert(certArg);
        java.security.PublicKey key = cert.getPublicKey();
        if (!"Ed25519".equalsIgnoreCase(key.getAlgorithm())
                && !"EdDSA".equalsIgnoreCase(key.getAlgorithm())) {
            return null;
        }
        byte[] raw = ed25519RawFromKey(key);
        if (raw == null) {
            return null;
        }
        return toGoBytes(raw);
    }

    /**
     * RFC 8032 encoding of an Ed25519 public key from its JDK point.
     * Returns null if the key is not an Ed25519 key or the point does
     * not encode to exactly 32 bytes.
     */
    private static byte[] ed25519RawFromKey(java.security.PublicKey key) {
        if (key instanceof EdECPublicKey) {
            java.security.spec.EdECPoint point =
                    ((EdECPublicKey) key).getPoint();
            BigInteger y = point.getY();
            if (y.signum() < 0) {
                return null;
            }
            byte[] be = stripLeadingZero(y.toByteArray());
            if (be.length > 32) {
                return null;
            }
            byte[] le = new byte[32];
            for (int i = 0; i < be.length; i++) {
                le[i] = be[be.length - 1 - i];
            }
            if (point.isXOdd()) {
                le[31] |= (byte) 0x80;
            }
            return le;
        }
        // Non-EdEC key object (e.g. a provider-specific EdDSA key):
        // re-parse its SPKI encoding through the JDK and retry once.
        try {
            KeyFactory kf = KeyFactory.getInstance("Ed25519");
            java.security.PublicKey parsed = kf.generatePublic(
                    new X509EncodedKeySpec(key.getEncoded()));
            if (parsed != key) {
                return ed25519RawFromKey(parsed);
            }
        } catch (Exception ignored) {
        }
        return null;
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

    // ---- Strict sequential DER reader ----
    //
    // Replaces the prior revision's tag-scanning helpers (extractBitString /
    // skipLength / derLength), which located values by searching for tag
    // bytes anywhere in the input. This reader is positional: it consumes
    // one TLV after another, validates canonical DER lengths, and fails
    // (via DerException, caught by callers) on any structural violation.
    // It deliberately does NOT reimplement gc's asn1 semantics beyond the
    // documented gc-parity rules in the class javadoc; value-level checks
    // (ranges, consistency) live in the per-format parsers below.

    /** Internal failure of strict DER parsing; callers map it to null/"". */
    private static final class DerException extends RuntimeException {
        DerException(String msg) {
            super(msg);
        }
    }

    /** One parsed TLV: tag byte, value slice, and end offset. */
    private static final class Tlv {
        final int tag;
        final byte[] value;
        final int end;

        Tlv(int tag, byte[] value, int end) {
            this.tag = tag;
            this.value = value;
            this.end = end;
        }
    }

    /**
     * Positional reader over a byte range. Lengths must be canonical DER:
     * short form for values under 128, minimal long form otherwise;
     * indefinite form (0x80) is rejected, as gc rejects it.
     */
    private static final class DerReader {
        final byte[] buf;
        int pos;
        final int end;

        DerReader(byte[] buf, int off, int end) {
            this.buf = buf;
            this.pos = off;
            this.end = end;
        }

        boolean hasMore() {
            return pos < end;
        }

        /** Reads one TLV at the current position. */
        Tlv readTlv() {
            if (pos >= end) {
                throw new DerException("unexpected end of input");
            }
            int tag = buf[pos++] & 0xFF;
            int len = readLength();
            if (len < 0 || pos + len > end) {
                throw new DerException("length overruns input");
            }
            byte[] value = new byte[len];
            System.arraycopy(buf, pos, value, 0, len);
            pos += len;
            return new Tlv(tag, value, pos);
        }

        /** Peeks at the next tag byte without consuming it, or -1. */
        int peekTag() {
            return pos < end ? buf[pos] & 0xFF : -1;
        }

        private int readLength() {
            if (pos >= end) {
                throw new DerException("missing length");
            }
            int b = buf[pos++] & 0xFF;
            if ((b & 0x80) == 0) {
                return b;
            }
            int num = b & 0x7F;
            if (num == 0) {
                throw new DerException("indefinite length");
            }
            if (num > 4 || pos + num > end) {
                throw new DerException("bad long-form length");
            }
            if (buf[pos] == 0) {
                throw new DerException("non-minimal long-form length");
            }
            long len = 0;
            for (int i = 0; i < num; i++) {
                len = (len << 8) | (buf[pos++] & 0xFF);
            }
            if (len > Integer.MAX_VALUE) {
                throw new DerException("length overflow");
            }
            // Long form for a value under 128 is non-canonical DER;
            // gc rejects it (differential corpus: sec1-d-longform-len).
            if (len < 128) {
                throw new DerException("non-minimal long-form length");
            }
            return (int) len;
        }
    }

    /** Reads the top-level TLV of a DER value (trailing bytes ignored). */
    private static Tlv topTlv(byte[] der) {
        DerReader r = new DerReader(der, 0, der.length);
        return r.readTlv();
    }

    /**
     * Decodes an INTEGER TLV value (two's complement) to BigInteger.
     * Empty values are rejected, as gc rejects them.
     */
    private static BigInteger integerValue(Tlv tlv) {
        if (tlv.tag != 0x02) {
            throw new DerException("expected INTEGER");
        }
        if (tlv.value.length == 0) {
            throw new DerException("empty INTEGER");
        }
        return new BigInteger(tlv.value);
    }

    /** Decodes DER OID content octets to dotted-decimal (strict base-128). */
    private static String decodeOidStrict(byte[] oid) {
        if (oid.length == 0) {
            throw new DerException("empty OID");
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
        boolean inArc = false;
        for (int i = 1; i < oid.length; i++) {
            int b = oid[i] & 0xFF;
            if (!inArc && b == 0x80) {
                throw new DerException("non-minimal OID arc");
            }
            inArc = true;
            val = (val << 7) | (b & 0x7F);
            if (val > Integer.MAX_VALUE) {
                throw new DerException("OID arc overflow");
            }
            if ((b & 0x80) == 0) {
                sb.append('.').append(val);
                val = 0;
                inArc = false;
            }
        }
        if (inArc) {
            throw new DerException("truncated OID arc");
        }
        return sb.toString();
    }

    // ---- DER encoding helpers (for canonical re-encodings) ----

    private static byte[] derLengthBytes(int len) {
        if (len < 128) {
            return new byte[]{(byte) len};
        }
        if (len < 256) {
            return new byte[]{(byte) 0x81, (byte) len};
        }
        return new byte[]{(byte) 0x82, (byte) (len >> 8), (byte) len};
    }

    private static byte[] derTlv(int tag, byte[] content) {
        byte[] len = derLengthBytes(content.length);
        byte[] out = new byte[1 + len.length + content.length];
        out[0] = (byte) tag;
        System.arraycopy(len, 0, out, 1, len.length);
        System.arraycopy(content, 0, out, 1 + len.length, content.length);
        return out;
    }

    private static byte[] derConcat(byte[]... parts) {
        int total = 0;
        for (byte[] p : parts) {
            total += p.length;
        }
        byte[] out = new byte[total];
        int pos = 0;
        for (byte[] p : parts) {
            System.arraycopy(p, 0, out, pos, p.length);
            pos += p.length;
        }
        return out;
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

    /**
     * Parses a PKCS#1 RSAPublicKey DER into "n_hex:e" (backs gc
     * {@code ParsePKCS1PublicKey}). The JDK has no PKCS#1 import spec,
     * so the structure is validated by the strict reader (top SEQUENCE
     * with exact consumption — gc rejects trailing bytes — two leading
     * INTEGERs, further elements inside ignored as gc ignores them),
     * the values are checked against gc's rules (n &gt; 0,
     * 0 &lt; e &lt; 2^31), and the key is then built by the JDK from a
     * canonical re-encoding wrapped as SPKI; the returned components
     * are read back from the JDK key object and must equal the parsed
     * values. The prior revision scanned INTEGER tags by hand, ignored
     * trailing bytes, and returned e = 0, n = 0, and e &gt;= 2^31 as
     * if valid.
     */
    public static Object parsePkcs1(Object derArg) {
        byte[] der = toBytes(derArg);
        try {
            Tlv top = topTlv(der);
            if (top.tag != 0x30) {
                return null;
            }
            // gc rejects trailing bytes after the SEQUENCE.
            if (top.end != der.length) {
                return null;
            }
            DerReader r = new DerReader(top.value, 0, top.value.length);
            BigInteger n = integerValue(r.readTlv());
            BigInteger eBig = integerValue(r.readTlv());
            if (n.signum() <= 0) {
                return null;
            }
            if (eBig.signum() <= 0
                    || eBig.compareTo(BigInteger.valueOf(0x7FFFFFFFL)) > 0) {
                return null;
            }
            long e = eBig.longValue();
            // Canonical re-encoding wrapped as SPKI, parsed by the JDK.
            byte[] pkcs1 = derTlv(0x30, derConcat(
                    derTlv(0x02, n.toByteArray()),
                    derTlv(0x02, eBig.toByteArray())));
            // rsaEncryption OID 1.2.840.113549.1.1.1 + NULL.
            byte[] algId = derTlv(0x30, derConcat(
                    derTlv(0x06, new byte[]{
                        0x2A, (byte) 0x86, 0x48, (byte) 0x86, (byte) 0xF7,
                        0x0D, 0x01, 0x01, 0x01}),
                    derTlv(0x05, new byte[0])));
            byte[] bitString = derTlv(0x03,
                    derConcat(new byte[]{0x00}, pkcs1));
            byte[] spki = derTlv(0x30, derConcat(algId, bitString));
            KeyFactory kf = KeyFactory.getInstance("RSA");
            RSAPublicKey key = (RSAPublicKey) kf.generatePublic(
                    new X509EncodedKeySpec(spki));
            if (!key.getModulus().equals(n)
                    || !key.getPublicExponent().equals(eBig)) {
                return null;
            }
            return toGoString(toHex(stripLeadingZero(n.toByteArray())) + ":" + e);
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
        if (key instanceof ECPublicKey) {
            ECPublicKey ec = (ECPublicKey) key;
            java.security.spec.ECParameterSpec spec = ec.getParams();
            byte[] x = toFixed(ec.getW().getAffineX(), spec);
            byte[] y = toFixed(ec.getW().getAffineY(), spec);
            long fs = spec.getCurve().getField().getFieldSize();
            return "EC:" + toHex(x) + ":" + toHex(y) + ":" + fs;
        }
        String alg = key.getAlgorithm();
        if ("Ed25519".equalsIgnoreCase(alg) || "EdDSA".equalsIgnoreCase(alg)) {
            byte[] raw = ed25519RawFromKey(key);
            if (raw != null) {
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

    /**
     * Unwraps a DER OCTET STRING, or null if malformed. The prior
     * revision truncated silently on short input and could throw
     * ArrayIndexOutOfBounds on malformed length bytes; callers now
     * propagate null (key identifier absent) instead of wrong bytes.
     * A non-OCTET-STRING input is returned unchanged (legacy fallback
     * for extension values that are already bare content).
     */
    private static byte[] unwrapOctetString(byte[] der) {
        if (der.length < 2 || der[0] != 0x04) {
            return der;
        }
        try {
            Tlv tlv = topTlv(der);
            if (tlv.tag != 0x04) {
                return der;
            }
            return tlv.value;
        } catch (DerException e) {
            return null;
        }
    }

    /**
     * Extracts keyIdentifier [0] from an AuthorityKeyIdentifier SEQUENCE
     * by walking its elements in order (the [0] value is primitive
     * context-specific tag 0x80). The prior revision scanned raw bytes
     * for 0x80 anywhere, which could match a 0x80 byte inside a nested
     * value, and truncated silently on short input. Returns null if
     * the structure is malformed or has no [0] element.
     */
    private static byte[] extractAuthorityKeyId(byte[] seq) {
        // SEQUENCE { [0] keyIdentifier [1] authorityCertIssuer [2] authorityCertSerialNumber }
        try {
            Tlv top = topTlv(seq);
            if (top.tag != 0x30) {
                return null;
            }
            DerReader r = new DerReader(top.value, 0, top.value.length);
            while (r.hasMore()) {
                Tlv el = r.readTlv();
                if (el.tag == 0x80) {
                    return el.value;
                }
            }
            return null;
        } catch (DerException e) {
            return null;
        }
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
            // Canonical minimal-byte hex for every component (the prior
            // revision left the BigInteger sign byte on the modulus and
            // exponents; the Go side tolerates it via SetBytes, but the
            // descriptor should be canonical).
            return "RSA:" + toHex(stripLeadingZero(key.getModulus().toByteArray())) + ":"
                    + toHex(stripLeadingZero(key.getPublicExponent().toByteArray())) + ":"
                    + toHex(stripLeadingZero(key.getPrivateExponent().toByteArray())) + ":"
                    + toHex(stripLeadingZero(key.getPrimeP().toByteArray())) + ":"
                    + toHex(stripLeadingZero(key.getPrimeQ().toByteArray()));
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

    /**
     * PKCS#1 RSAPrivateKey, parsed with the strict reader mirroring gc's
     * struct decoding plus the deterministic part of gc's
     * {@code Validate} (see {@link #validateRsaPrivate}). The JDK cannot
     * import PKCS#1 at all, and the standard 9-integer form is therefore
     * additionally materialised as a JDK {@link RSAPrivateCrtKey} whose
     * components are read back for the descriptor. Short forms (6–8
     * integers: trailing CRT parameters omitted, which gc accepts
     * because those struct fields are optional) and multi-prime keys
     * have no JDK key representation carrying p and q, so their
     * components come from the strict parse. The prior revision
     * required exactly 9 integers (rejecting gc-valid short forms) and
     * accepted version 2, zero/negative components, and internally
     * inconsistent keys (n != p*q, broken d&middot;e congruences).
     */
    private static String tryPkcs1(byte[] der) {
        try {
            Tlv top = topTlv(der);
            if (top.tag != 0x30) {
                return null;
            }
            // gc rejects trailing bytes after the SEQUENCE.
            if (top.end != der.length) {
                return null;
            }
            DerReader r = new DerReader(top.value, 0, top.value.length);
            BigInteger version = integerValue(r.readTlv());
            // gc rejects only Version > 1 (a negative version is accepted).
            if (version.compareTo(BigInteger.ONE) > 0
                    || version.compareTo(BigInteger.valueOf(Long.MIN_VALUE)) < 0
                    || version.compareTo(BigInteger.valueOf(Long.MAX_VALUE)) > 0) {
                return null;
            }
            BigInteger n = integerValue(r.readTlv());
            BigInteger e = integerValue(r.readTlv());
            BigInteger d = integerValue(r.readTlv());
            BigInteger p = integerValue(r.readTlv());
            BigInteger q = integerValue(r.readTlv());
            // Optional trailing CRT parameters: skipped on tag mismatch,
            // exactly as gc skips optional struct fields.
            BigInteger dp = null;
            BigInteger dq = null;
            BigInteger qinv = null;
            if (r.hasMore() && r.peekTag() == 0x02) {
                dp = integerValue(r.readTlv());
            }
            if (r.hasMore() && r.peekTag() == 0x02) {
                dq = integerValue(r.readTlv());
            }
            if (r.hasMore() && r.peekTag() == 0x02) {
                qinv = integerValue(r.readTlv());
            }
            List<BigInteger> primes = new ArrayList<>();
            primes.add(p);
            primes.add(q);
            boolean hasAdditional = false;
            if (r.hasMore() && r.peekTag() == 0x30) {
                // AdditionalPrimes: SEQUENCE OF SEQUENCE { prime, exp, coeff }.
                Tlv add = r.readTlv();
                DerReader ar = new DerReader(add.value, 0, add.value.length);
                while (ar.hasMore()) {
                    Tlv primeSeq = ar.readTlv();
                    if (primeSeq.tag != 0x30) {
                        return null;
                    }
                    DerReader pr = new DerReader(
                            primeSeq.value, 0, primeSeq.value.length);
                    BigInteger prime = integerValue(pr.readTlv());
                    // exp and coeff are required by gc's struct but their
                    // values are ignored (recomputed); only their presence
                    // as INTEGERs is checked, extra elements ignored.
                    integerValue(pr.readTlv());
                    integerValue(pr.readTlv());
                    if (prime.signum() <= 0) {
                        return null;
                    }
                    primes.add(prime);
                }
                hasAdditional = true;
            }
            // Any remaining elements inside the SEQUENCE are ignored,
            // as gc ignores elements after the last struct field.
            if (!validateRsaPrivate(n, e, d, primes, dp, dq, qinv)) {
                return null;
            }
            if (dp != null && dq != null && qinv != null && !hasAdditional) {
                // Standard form: build the JDK key and read back from it.
                try {
                    KeyFactory kf = KeyFactory.getInstance("RSA");
                    RSAPrivateCrtKey key = (RSAPrivateCrtKey) kf.generatePrivate(
                            new RSAPrivateCrtKeySpec(n, e, d, p, q, dp, dq, qinv));
                    if (!key.getModulus().equals(n)
                            || !key.getPublicExponent().equals(e)
                            || !key.getPrivateExponent().equals(d)
                            || !key.getPrimeP().equals(p)
                            || !key.getPrimeQ().equals(q)) {
                        return null;
                    }
                    return "RSA:" + toHex(stripLeadingZero(n.toByteArray())) + ":"
                            + toHex(stripLeadingZero(e.toByteArray())) + ":"
                            + toHex(stripLeadingZero(d.toByteArray())) + ":"
                            + toHex(stripLeadingZero(p.toByteArray())) + ":"
                            + toHex(stripLeadingZero(q.toByteArray()));
                } catch (Exception ignored) {
                    return null;
                }
            }
            return "RSA:" + toHex(stripLeadingZero(n.toByteArray())) + ":"
                    + toHex(stripLeadingZero(e.toByteArray())) + ":"
                    + toHex(stripLeadingZero(d.toByteArray())) + ":"
                    + toHex(stripLeadingZero(p.toByteArray())) + ":"
                    + toHex(stripLeadingZero(q.toByteArray()));
        } catch (DerException | ArithmeticException ignored) {
            return null;
        }
    }

    /**
     * Deterministic part of gc {@code rsa.PrivateKey.Validate} /
     * {@code x509.ParsePKCS1PrivateKey}: positivity, public-exponent
     * range (gc accepts e = 3, rejects e = 1 and e = -1 despite those
     * admitting a congruent d; e fits a Go int), product of primes
     * equals n, d&middot;e &equiv; 1 (mod r-1) for every prime r, and
     * CRT consistency when CRT parameters are present. Primality is
     * NOT checked: gc's parse path accepted a crafted composite
     * "prime" in the differential corpus, so no gap is introduced by
     * omitting it here.
     */
    private static boolean validateRsaPrivate(BigInteger n, BigInteger e,
            BigInteger d, List<BigInteger> primes,
            BigInteger dp, BigInteger dq, BigInteger qinv) {
        if (n.signum() <= 0 || d.signum() <= 0) {
            return false;
        }
        for (BigInteger prime : primes) {
            if (prime.signum() <= 0) {
                return false;
            }
        }
        if (e.compareTo(BigInteger.valueOf(3)) < 0
                || e.compareTo(BigInteger.valueOf(Long.MAX_VALUE)) > 0) {
            return false;
        }
        if (dp != null && dp.signum() <= 0) {
            return false;
        }
        if (dq != null && dq.signum() <= 0) {
            return false;
        }
        if (qinv != null && qinv.signum() <= 0) {
            return false;
        }
        BigInteger product = BigInteger.ONE;
        for (BigInteger prime : primes) {
            product = product.multiply(prime);
        }
        if (!product.equals(n)) {
            return false;
        }
        BigInteger de = d.multiply(e);
        for (BigInteger prime : primes) {
            if (!de.mod(prime.subtract(BigInteger.ONE)).equals(BigInteger.ONE)) {
                return false;
            }
        }
        BigInteger p = primes.get(0);
        BigInteger q = primes.get(1);
        if (dp != null && !dp.equals(d.mod(p.subtract(BigInteger.ONE)))) {
            return false;
        }
        if (dq != null && !dq.equals(d.mod(q.subtract(BigInteger.ONE)))) {
            return false;
        }
        if (qinv != null) {
            try {
                if (!q.modInverse(p).equals(qinv)) {
                    return false;
                }
            } catch (ArithmeticException ex) {
                return false;
            }
        }
        return true;
    }

    /** Parsed SEC1 ECPrivateKey: normalised scalar, curve, [1] BIT STRING value. */
    private static final class Sec1 {
        final byte[] d;
        final String curveName;
        final byte[] pubBits;

        Sec1(byte[] d, String curveName, byte[] pubBits) {
            this.d = d;
            this.curveName = curveName;
            this.pubBits = pubBits;
        }
    }

    /**
     * Strict sequential parse of a SEC1 ECPrivateKey, mirroring gc's
     * struct decoding: SEQUENCE { version INTEGER (= 1), privateKey
     * OCTET STRING, [0] parameters (OID, required — gc reports
     * "unknown elliptic curve" without it), [1] publicKey (BIT STRING,
     * optional and IGNORED, as gc ignores it even when it disagrees
     * with d) }. Elements after the last field, and elements inside
     * the [0]/[1] wrappers after the first value, are ignored, as gc
     * ignores them; trailing bytes after the top SEQUENCE are ignored,
     * as gc's {@code ParseECPrivateKey} ignores {@code Unmarshal}'s
     * rest. The scalar is normalised exactly as gc normalises it:
     * leading zero bytes are stripped while it is longer than the
     * curve size (a non-zero leading byte is an error), shorter
     * scalars are left-padded. Returns null on any violation.
     */
    private static Sec1 parseSec1(byte[] der) {
        Tlv top = topTlv(der);
        if (top.tag != 0x30) {
            throw new DerException("SEC1 is not a SEQUENCE");
        }
        DerReader r = new DerReader(top.value, 0, top.value.length);
        BigInteger version = integerValue(r.readTlv());
        if (!version.equals(BigInteger.ONE)) {
            throw new DerException("SEC1 version is not 1");
        }
        Tlv privTlv = r.readTlv();
        if (privTlv.tag != 0x04) {
            throw new DerException("SEC1 privateKey is not an OCTET STRING");
        }
        byte[] dRaw = privTlv.value;
        // Parameters [0]: required; a non-[0] next element means the
        // optional field is absent (gc: unknown curve), not an error
        // to be skipped over — parsing stops here for the curve.
        String curveName = null;
        if (r.hasMore() && r.peekTag() == 0xA0) {
            Tlv params = r.readTlv();
            DerReader pr = new DerReader(params.value, 0, params.value.length);
            Tlv oidTlv = pr.readTlv();
            if (oidTlv.tag != 0x06) {
                throw new DerException("SEC1 parameters are not an OID");
            }
            // Anything else inside [0] is ignored, as gc ignores it.
            curveName = ecCurveNameByOid(decodeOidStrict(oidTlv.value));
        }
        if (curveName == null) {
            throw new DerException("SEC1 unknown curve");
        }
        // Public key [1]: optional; its content is ignored, but its
        // first value must be a well-formed BIT STRING (gc rejects an
        // unused-bits count above 7).
        byte[] pubBits = null;
        if (r.hasMore() && r.peekTag() == 0xA1) {
            Tlv pub = r.readTlv();
            DerReader br = new DerReader(pub.value, 0, pub.value.length);
            Tlv bits = br.readTlv();
            if (bits.tag != 0x03 || bits.value.length < 1
                    || (bits.value[0] & 0xFF) > 7) {
                throw new DerException("SEC1 publicKey is not a BIT STRING");
            }
            pubBits = bits.value;
            // Anything else inside [1] is ignored, as gc ignores it.
        }
        // Remaining elements inside the SEQUENCE are ignored (gc rule).
        java.security.spec.ECParameterSpec spec = ecSpecForName(curveName);
        if (spec == null) {
            throw new DerException("SEC1 curve has no JDK parameters");
        }
        int size = (spec.getOrder().bitLength() + 7) / 8;
        int off = 0;
        while (dRaw.length - off > size) {
            if (dRaw[off] != 0) {
                throw new DerException("SEC1 private key too long");
            }
            off++;
        }
        byte[] d = new byte[size];
        System.arraycopy(dRaw, off, d, size - (dRaw.length - off),
                dRaw.length - off);
        return new Sec1(d, curveName, pubBits);
    }

    /**
     * SEC1 ECPrivateKey via {@link #parseSec1}, cross-validated by the
     * JDK twice: the SEC1 bytes wrapped as PKCS#8 must parse to an
     * {@link ECPrivateKey} with the same scalar and curve, and the
     * derived public point's x-coordinate must equal the JDK ECDH
     * shared secret for that scalar (see {@link #scalarMultiply}).
     * Any disagreement fails the parse instead of yielding key
     * material. The prior revision scanned for the first 0x04 tag and
     * the first 0xA0/0x06 tags anywhere in the input, accepted any
     * version, ignored the curve/scalar relationship, and capped the
     * scalar at 66 bytes (rejecting gc-valid leading-zero-padded
     * scalars and accepting out-of-range scalars, which it then
     * reduced mod n silently).
     */
    private static String trySec1(byte[] der) {
        try {
            Sec1 sec1 = parseSec1(der);
            java.security.spec.ECParameterSpec spec =
                    ecSpecForName(sec1.curveName);
            if (spec == null) {
                return null;
            }
            BigInteger s = new BigInteger(1, sec1.d);
            if (s.signum() <= 0 || s.compareTo(spec.getOrder()) >= 0) {
                return null;
            }
            // JDK re-validation: the canonical SEC1 (rebuilt from the
            // parsed values, so gc-accepted quirks such as trailing
            // bytes or extra elements inside the SEQUENCE cannot make
            // the JDK parse fail where gc accepts) wrapped as PKCS#8
            // must parse to an ECPrivateKey with the same scalar.
            ECPrivateKey jdkKey = sec1ToJdkPrivateKey(sec1);
            if (jdkKey == null || !jdkKey.getS().equals(s)) {
                return null;
            }
            if (jdkKey.getParams().getCurve().getField().getFieldSize()
                    != spec.getCurve().getField().getFieldSize()) {
                return null;
            }
            java.security.spec.ECPoint w =
                    scalarMultiply(spec, s, jdkKey, sec1.curveName);
            if (w == null) {
                return null;
            }
            return "EC:" + sec1.curveName + ":" + toHex(toFixed(w.getAffineX(), spec))
                    + ":" + toHex(toFixed(w.getAffineY(), spec)) + ":" + toHex(sec1.d);
        } catch (DerException | ArithmeticException ignored) {
            return null;
        }
    }

    /**
     * Builds a canonical SEC1 from parsed values (version 1, normalised
     * scalar, [0] OID, [1] BIT STRING if present), wraps it as a
     * PKCS#8 ECPrivateKey (PrivateKeyInfo with the ecPublicKey
     * algorithm identifier and the curve OID), and parses it with the
     * JDK {@link KeyFactory}. A malformed structure that the strict
     * reader missed is caught here by the JDK's own ASN.1 parser.
     * The canonical rebuild (rather than wrapping the original bytes)
     * is deliberate: gc accepts trailing bytes and extra elements
     * that the JDK's stricter SEC1 parser would reject, and those
     * quirks are already decided by the strict reader above.
     */
    private static ECPrivateKey sec1ToJdkPrivateKey(Sec1 sec1) {
        try {
            byte[] oid = ecOidBytes(sec1.curveName);
            if (oid == null) {
                return null;
            }
            byte[] algId = derTlv(0x30, derConcat(
                    derTlv(0x06, new byte[]{
                        0x2A, (byte) 0x86, 0x48, (byte) 0xCE, 0x3D, 0x02, 0x01}),
                    derTlv(0x06, oid)));
            byte[] sec1Der = derTlv(0x30, sec1.pubBits == null
                    ? derConcat(
                            derTlv(0x02, new byte[]{0x01}),
                            derTlv(0x04, sec1.d),
                            derTlv(0xA0, derTlv(0x06, oid)))
                    : derConcat(
                            derTlv(0x02, new byte[]{0x01}),
                            derTlv(0x04, sec1.d),
                            derTlv(0xA0, derTlv(0x06, oid)),
                            derTlv(0xA1, derTlv(0x03, sec1.pubBits))));
            byte[] pkcs8 = derTlv(0x30, derConcat(
                    derTlv(0x02, new byte[]{0x00}),
                    algId,
                    derTlv(0x04, sec1Der)));
            KeyFactory kf = KeyFactory.getInstance("EC");
            return (ECPrivateKey) kf.generatePrivate(
                    new PKCS8EncodedKeySpec(pkcs8));
        } catch (Exception ignored) {
            return null;
        }
    }

    /** OID content bytes for a JDK curve name (inverse of ecCurveNameByOid). */
    private static byte[] ecOidBytes(String curveName) {
        switch (curveName) {
            case "secp224r1":
                return new byte[]{0x2B, (byte) 0x81, 0x04, 0x00, 0x21};
            case "secp256r1":
                return new byte[]{0x2A, (byte) 0x86, 0x48, (byte) 0xCE,
                    0x3D, 0x03, 0x01, 0x07};
            case "secp384r1":
                return new byte[]{0x2B, (byte) 0x81, 0x04, 0x00, 0x22};
            case "secp521r1":
                return new byte[]{0x2B, (byte) 0x81, 0x04, 0x00, 0x23};
            default:
                return null;
        }
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
     * Scalar multiplication by double-and-add over JDK-supplied curve
     * parameters. RETAINED hand-rolled: the public JCA API exposes no
     * scalar-multiplication operation (see the class javadoc for why
     * the ECDH-only alternative would be worse). It is therefore
     * verified before use, and any failure returns null instead of a
     * point: the scalar must be in [1, n-1] (the prior revision
     * reduced mod n silently, so an out-of-range scalar yielded a
     * point for a different key), the result must not be infinity, it
     * must satisfy the curve equation, and its x-coordinate must equal
     * the JDK ECDH shared secret for the same scalar and the generator
     * — an independent JDK computation of the same value. Only prime
     * fields are supported (all four named curves are Fp); anything
     * else fails loudly. One curve is excepted from the ECDH check:
     * the JDK (SunEC, probed on OpenJDK 25) rejects secp224r1 for
     * ECDH, key generation, and signatures alike ("Curve not
     * supported: secp224r1"), while accepting it for KeyFactory
     * parsing and AlgorithmParameters — so for P-224 the independent
     * checks are the PKCS#8 re-parse, the on-curve check, and the gc
     * differential test, and an ECDH failure on any other curve still
     * fails the parse.
     */
    private static java.security.spec.ECPoint scalarMultiply(
            java.security.spec.ECParameterSpec spec, BigInteger s,
            ECPrivateKey jdkKey, String curveName) {
        if (s.signum() <= 0 || s.compareTo(spec.getOrder()) >= 0) {
            return null;
        }
        java.security.spec.EllipticCurve curve = spec.getCurve();
        if (!(curve.getField() instanceof java.security.spec.ECFieldFp)) {
            return null;
        }
        BigInteger prime = ((java.security.spec.ECFieldFp) curve.getField()).getP();
        java.security.spec.ECPoint g = spec.getGenerator();
        java.security.spec.ECPoint result = java.security.spec.ECPoint.POINT_INFINITY;
        java.security.spec.ECPoint addend = g;
        BigInteger k = s;
        while (k.signum() > 0) {
            if (k.testBit(0)) {
                result = ecAdd(curve, result, addend);
            }
            addend = ecDouble(curve, addend);
            k = k.shiftRight(1);
        }
        if (result.equals(java.security.spec.ECPoint.POINT_INFINITY)) {
            return null;
        }
        // On-curve check: y^2 == x^3 + a*x + b (mod p).
        BigInteger x = result.getAffineX();
        BigInteger y = result.getAffineY();
        BigInteger lhs = y.multiply(y).mod(prime);
        BigInteger rhs = x.multiply(x).multiply(x)
                .add(curve.getA().multiply(x)).add(curve.getB()).mod(prime);
        if (!lhs.equals(rhs)) {
            return null;
        }
        // Independent JDK cross-check: ECDH(d, G) shared secret == x.
        // A mismatch fails the parse; an ECDH failure fails it too,
        // except on secp224r1, where the JDK does not support ECDH at
        // all (see the method javadoc) — the remaining checks still
        // apply there.
        try {
            KeyFactory kf = KeyFactory.getInstance("EC");
            ECPublicKey generator = (ECPublicKey) kf.generatePublic(
                    new ECPublicKeySpec(g, spec));
            KeyAgreement ka = KeyAgreement.getInstance("ECDH");
            ka.init(jdkKey);
            ka.doPhase(generator, true);
            BigInteger ecdhX = new BigInteger(1, ka.generateSecret());
            if (!ecdhX.equals(x)) {
                return null;
            }
        } catch (Exception ignored) {
            if (!"secp224r1".equals(curveName)) {
                return null;
            }
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
            // Doubling a point with y == 0 gives infinity (vertical
            // tangent); the prior revision let modInverse throw here
            // and relied on a distant catch-all.
            if (y1.signum() == 0) {
                return java.security.spec.ECPoint.POINT_INFINITY;
            }
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

}
