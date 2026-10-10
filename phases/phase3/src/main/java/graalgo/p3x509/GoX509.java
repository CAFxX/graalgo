package graalgo.p3x509;

import java.io.ByteArrayInputStream;
import java.math.BigInteger;
import java.security.cert.CertificateException;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;
import java.util.List;

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

    /** Serial number as big-endian bytes (Go builds big.Int from this). */
    public static Object serialNumber(Object certArg) {
        X509Certificate cert = (X509Certificate) certArg;
        return toGoBytes(cert.getSerialNumber().toByteArray());
    }

    /** Subject DN as RFC 2253 string. */
    public static Object subject(Object certArg) {
        X509Certificate cert = (X509Certificate) certArg;
        return toGoString(cert.getSubjectX500Principal().getName());
    }

    /** Issuer DN as RFC 2253 string. */
    public static Object issuer(Object certArg) {
        X509Certificate cert = (X509Certificate) certArg;
        return toGoString(cert.getIssuerX500Principal().getName());
    }

    /** NotBefore as Unix millis. */
    public static Object notBefore(Object certArg) {
        X509Certificate cert = (X509Certificate) certArg;
        return cert.getNotBefore().getTime();
    }

    /** NotAfter as Unix millis. */
    public static Object notAfter(Object certArg) {
        X509Certificate cert = (X509Certificate) certArg;
        return cert.getNotAfter().getTime();
    }

    /** Version (1, 2, or 3). */
    public static Object version(Object certArg) {
        X509Certificate cert = (X509Certificate) certArg;
        return (long) cert.getVersion();
    }

    /** Signature algorithm OID as dotted string. */
    public static Object sigAlgOid(Object certArg) {
        X509Certificate cert = (X509Certificate) certArg;
        return toGoString(cert.getSigAlgOID());
    }

    /** Signature bytes. */
    public static Object signature(Object certArg) {
        X509Certificate cert = (X509Certificate) certArg;
        return toGoBytes(cert.getSignature());
    }

    /** TBS certificate bytes (for RawTBSCertificate). */
    public static Object tbsCertificate(Object certArg) {
        X509Certificate cert = (X509Certificate) certArg;
        try {
            return toGoBytes(cert.getTBSCertificate());
        } catch (CertificateException e) {
            return null;
        }
    }

    /** Key usage as bitmask, or -1 if absent. */
    public static Object keyUsage(Object certArg) {
        X509Certificate cert = (X509Certificate) certArg;
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
        X509Certificate cert = (X509Certificate) certArg;
        int bc = cert.getBasicConstraints();
        return (long) bc;
    }

    /** Subject key identifier, or null if absent. */
    public static Object subjectKeyId(Object certArg) {
        X509Certificate cert = (X509Certificate) certArg;
        byte[] ext = cert.getExtensionValue("2.5.29.14");
        if (ext == null) {
            return null;
        }
        // Unwrap OCTET STRING
        return toGoBytes(unwrapOctetString(ext));
    }

    /** Authority key identifier, or null if absent. */
    public static Object authorityKeyId(Object certArg) {
        X509Certificate cert = (X509Certificate) certArg;
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
        X509Certificate cert = (X509Certificate) certArg;
        return toGoString(cert.getPublicKey().getAlgorithm());
    }

    /** Public key in X.509 SubjectPublicKeyInfo DER encoding. */
    public static Object publicKeyEncoded(Object certArg) {
        X509Certificate cert = (X509Certificate) certArg;
        return toGoBytes(cert.getPublicKey().getEncoded());
    }

    /** Raw issuer DER bytes (for RawIssuer). */
    public static Object rawIssuer(Object certArg) {
        X509Certificate cert = (X509Certificate) certArg;
        return toGoBytes(cert.getIssuerX500Principal().getEncoded());
    }

    /** Raw subject DER bytes (for RawSubject). */
    public static Object rawSubject(Object certArg) {
        X509Certificate cert = (X509Certificate) certArg;
        return toGoBytes(cert.getSubjectX500Principal().getEncoded());
    }

    // ---- D5 boxing helpers ----

    /** Converts Java byte[] to Go []byte (GoSlice with int[] backing). */
    private static Object toGoBytes(byte[] bytes) {
        if (bytes == null) {
            return null;
        }
        int[] arr = new int[bytes.length];
        for (int i = 0; i < bytes.length; i++) {
            arr[i] = bytes[i] & 0xFF;
        }
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
            int[] arr = (int[]) gs.array();
            int off = gs.offset();
            int len = gs.len();
            byte[] bytes = new byte[len];
            for (int i = 0; i < len; i++) {
                bytes[i] = (byte) arr[off + i];
            }
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
}
