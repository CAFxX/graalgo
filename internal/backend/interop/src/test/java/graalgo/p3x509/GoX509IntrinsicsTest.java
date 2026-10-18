package graalgo.p3x509;

import java.io.ByteArrayInputStream;
import java.nio.charset.StandardCharsets;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;

import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Direct unit tests for the P0-3/P0-4 x509 intrinsics (2026-10-18):
 * {@code graalgo.x509.systemRoots} and {@code graalgo.x509.nameConstraints}.
 * The Go-side parsing of the wire format is covered differentially against
 * gc in the frontend (synth_x509_verify_regression_test.go); these tests pin
 * the Java side: truststore loading, DER framing, and the DER-to-wire
 * name-constraints encoding.
 */
public final class GoX509IntrinsicsTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static X509Certificate loadCert(String pem) {
        try {
            CertificateFactory cf = CertificateFactory.getInstance("X.509");
            return (X509Certificate) cf.generateCertificate(
                    new ByteArrayInputStream(pem.getBytes(StandardCharsets.UTF_8)));
        } catch (Exception e) {
            throw new AssertionError("cannot load test cert: " + e, e);
        }
    }

    /**
     * P0-3: systemRoots returns the JDK default truststore as 4-byte
     * big-endian length-prefixed DER blobs, each parsing as X.509.
     */
    public void testSystemRootsLoads() {
        Object result = GoX509.systemRoots(null);
        assertTrue(result instanceof GoSlice,
                "systemRoots returns a GoSlice, got " + result.getClass());
        byte[] raw = (byte[]) ((GoSlice) result).array();
        assertTrue(raw.length > 4, "systemRoots is non-empty");
        int count = 0;
        try {
            CertificateFactory cf = CertificateFactory.getInstance("X.509");
            int off = 0;
            while (off < raw.length) {
                assertTrue(off + 4 <= raw.length, "length prefix truncated");
                int len = ((raw[off] & 0xFF) << 24) | ((raw[off + 1] & 0xFF) << 16)
                        | ((raw[off + 2] & 0xFF) << 8) | (raw[off + 3] & 0xFF);
                assertTrue(len > 0 && off + 4 + len <= raw.length,
                        "length prefix valid at offset " + off);
                X509Certificate c = (X509Certificate) cf.generateCertificate(
                        new ByteArrayInputStream(raw, off + 4, len));
                assertTrue(c != null, "blob parses as X.509 at offset " + off);
                count++;
                off += 4 + len;
            }
        } catch (Exception e) {
            throw new AssertionError("systemRoots framing/parse failed: " + e, e);
        }
        assertTrue(count > 0, "at least one system root loaded");
    }

    /**
     * P0-4: nameConstraints encodes the 2.5.29.30 extension as the
     * P:/X:/CRIT:1 wire format the Go parser consumes.
     */
    public void testNameConstraintsWireFormat() {
        X509Certificate cert = loadCert(NC_CA_PEM);
        Object result = GoX509.nameConstraints(cert);
        assertTrue(result instanceof GoString,
                "nameConstraints returns a GoString, got " + result.getClass());
        String wire = result.toString();
        assertTrue(wire.contains("P:DNS:.example.com"),
                "permitted DNS (leading-dot preserved): " + wire);
        assertTrue(wire.contains("P:IPH:0a000000:ff000000"),
                "permitted IP subnet (hex addr:hex mask): " + wire);
        assertTrue(wire.contains("X:DNS:bad.example.com"),
                "excluded DNS: " + wire);
        assertTrue(wire.contains("CRIT:1"), "critical flag: " + wire);
    }

    /** P0-4: a cert without the extension yields the empty wire string. */
    public void testNameConstraintsAbsent() {
        X509Certificate cert = loadCert(PLAIN_CA_PEM);
        Object result = GoX509.nameConstraints(cert);
        assertTrue(result instanceof GoString, "nameConstraints returns a GoString");
        assertTrue("".equals(result.toString()),
                "absent extension -> empty string, got: " + result);
    }

    // Self-signed CA with critical nameConstraints:
    // permitted DNS:.example.com, permitted IP:10.0.0.0/255.0.0.0,
    // excluded DNS:bad.example.com. Generated 2026-10-18 via openssl;
    // checked in so the test needs no external tooling.
    private static final String NC_CA_PEM =
        """
        -----BEGIN CERTIFICATE-----
        MIIDXjCCAkagAwIBAgIUOOyE568jQOp2I5p8uW41OD/kbq0wDQYJKoZIhvcNAQEL
        BQAwFTETMBEGA1UEAwwKbmMtdGVzdC1jYTAeFw0yNjEwMTgwNzMxMzFaFw0yNjEw
        MjAwNzMxMzFaMBUxEzARBgNVBAMMCm5jLXRlc3QtY2EwggEiMA0GCSqGSIb3DQEB
        AQUAA4IBDwAwggEKAoIBAQDo7TNARo/sQgqrZDYaiu24NdCl1CF7KAlXG2t3QgqL
        1++dTpObCbMOQQ/y3m2+Q+Qzb8Qr3hvMmRdvVanHcX7Pm6ADBQBHAuEzysEU8aop
        rzcNI3c5xd02/D/jkRND8iPO4HMrh161AMqaohTPI3oxFi3Mcx0vDFOdRIk68dJz
        ktqV1Ca56+YkH21MUXqlYjAY+WZHidw2U5UjygwskD91SI2DMuFaSYqPLDdnBC2U
        jbSDGbEbDj561mSDdmFRcZloc5BiYdzxWz6AQT2o9PmbiKmGv18NCA9l61+vUC0C
        wAFDxD4nLe5NF6InJB7FXRbypQW77ESBVHxLHSWr7j9DAgMBAAGjgaUwgaIwDwYD
        VR0TAQH/BAUwAwEB/zAOBgNVHQ8BAf8EBAMCAoQwHQYDVR0OBBYEFBWDxcktO/7z
        1KAr+OmnXX5dry2RMB8GA1UdIwQYMBaAFBWDxcktO/7z1KAr+OmnXX5dry2RMD8G
        A1UdHgEB/wQ1MDOgHDAOggwuZXhhbXBsZS5jb20wCocICgAAAP8AAAChEzARgg9i
        YWQuZXhhbXBsZS5jb20wDQYJKoZIhvcNAQELBQADggEBALyhr96zjsKE0x4QBJxS
        vlWRgCqFezZaS3FOonj5P2MyM3INbXG1/3AWW5Gh/aZ7SZCAbo+VeZV1YVpt4wVm
        Pn3WdWFTqw5DGE80W4HRV4aBXsJKUXRMiQu1C7KXfShP10ZpIbb2iN2eJxTWNvdl
        L90dHedTYumi7ez36Ht+8ZXRgLU5T/p6uF2eGUmMGGmQHzRsIgIUQqkbxxkAcnWI
        +eUofffVgz5NXLXxLIFyJ0CKqrW4BdErmh1IYLce8mcII17yRtT1JK6MF5cMP4f3
        2YJeZXiISmKfAJm1JQnpl5xo9U1a0yv8XZJfSRqjp0W78X54EjR75pZxCSHK8wUV
        8oo=
        -----END CERTIFICATE-----
        """;

    // Self-signed CA without a nameConstraints extension.
    private static final String PLAIN_CA_PEM =
        """
        -----BEGIN CERTIFICATE-----
        MIIDADCCAeigAwIBAgIUHEoV8PLFHe9XsoruSV5W6rp0PTUwDQYJKoZIhvcNAQEL
        BQAwGDEWMBQGA1UEAwwNcGxhaW4tdGVzdC1jYTAeFw0yNjEwMTgwNzMxMjVaFw0y
        NjEwMjAwNzMxMjVaMBgxFjAUBgNVBAMMDXBsYWluLXRlc3QtY2EwggEiMA0GCSqG
        SIb3DQEBAQUAA4IBDwAwggEKAoIBAQCiivXA634bWuMZWW0UzVpjIxGCC//ARY39
        uZ/lnGIzJiGk/gKlnf4cP6HbUNX9q4Wxt43QJOJ1hP4V5cdpdqO8QtG5HVi5m+/V
        ST8uX6ZOMf9SC8aOo7pn0GowZW6C8Uo9YNPcHI+hlJC5rtsO8O7QdIa5D4bz6DIJ
        LlyhHVMCeLI9dYzCSVz4JdHqvL4kx4AO4XA5sE+h11N7Nv71BhyvRsdAwLA6VIGH
        rzUPei6RdDdc7Z6xL7+KuTXJWzAUkLNipn+ROryoGFDWIW3sTQghPtuK5j3GAZYr
        6FF91QyKk3lftx1A05LvWoMMAqgn9GGlZEcEIb++UehbrR6IfOM9AgMBAAGjQjBA
        MA8GA1UdEwEB/wQFMAMBAf8wDgYDVR0PAQH/BAQDAgKEMB0GA1UdDgQWBBQTF7rW
        5wHZ0VfBhPcM/Cw+3iS7rjANBgkqhkiG9w0BAQsFAAOCAQEAIoAzASwaij8IgKVC
        GkZP8bHTWnKCaAfruxHrOdoeqfgRyWd9+eqEHKj+QqM+8+fwqMLYe2UHctXqYVCi
        5jc8KASqlFdpLHN+DioKO8LCdFEEHN+S1n6U0e4k93je61x9gj279PHV2scSYw4Y
        KcKZ7llkYFUgsxSneQGjHbs7WUaPMkHzPHgo/JOiavNhqR/w0soL282B2bVTN5mo
        x69JIaBQSwhHx9ORSo4wj+0tPcFHmdtKrLou54iz29HDE7HGg8Cz0Ewm73qB+VWK
        9Q1KVXi+PCjvacGhGULgqQjPf0UsYHSkWetqqyDg+5ZSqYXT/6puPZ9ZHYIjMpvS
        zXRT7Q==
        -----END CERTIFICATE-----
        """;
}
