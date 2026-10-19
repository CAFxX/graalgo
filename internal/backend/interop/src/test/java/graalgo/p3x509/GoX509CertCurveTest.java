package graalgo.p3x509;

import java.nio.charset.StandardCharsets;
import java.security.cert.X509Certificate;

import graalgo.values.GoString;

/**
 * Unit tests for the certificate-path curve identification
 * (codebase-review 2026-10-19, SPKI follow-up to I-4): {@link GoX509#ecCurveName}
 * must report the certificate's EC curve by NAME ("secp256r1"), never by
 * field size. The old {@code ecFieldSize} entry point mapped the field size
 * to the curve Go-side, so a 256-bit-field non-P-256 certificate was
 * silently treated as P-256 (the I-4 bug family).
 */
public final class GoX509CertCurveTest {

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

    /** Runs {@code GoX509.ecCurveName} on a parsed certificate. */
    private static String certCurveName(byte[] certDer) {
        Object cert = GoX509.parse(certDer);
        check(cert instanceof X509Certificate, "test certificate did not parse");
        Object r = GoX509.ecCurveName(cert);
        check(r instanceof GoString, "ecCurveName did not return a GoString");
        return new String(((GoString) r).bytes(), StandardCharsets.UTF_8);
    }

    public void testCertSecp256r1NamedCorrectly() throws Exception {
        checkEq("secp256r1", certCurveName(EC_CERT_DER), "P-256 cert curve name");
    }

    public void testCertNonEcYieldsEmptyName() throws Exception {
        // A non-EC certificate must yield "" (loud "invalid ECDSA public
        // key" Go-side), never a guessed curve.
        checkEq("", certCurveName(RSA_CERT_DER), "RSA cert curve name");
    }
    /** P-256 self-signed cert DER (bytes copied from test/corpus/wave3/crypto/x509/01_parse.go). */
    private static final byte[] EC_CERT_DER = {
        48, (byte) 130, 1, 44, 48, (byte) 129, (byte) 211, (byte) 160, 3, 2, 1, 2,
        2, 3, 1, 9, 50, 48, 10, 6, 8, 42, (byte) 134, 72,
        (byte) 206, 61, 4, 3, 2, 48, 21, 49, 19, 48, 17, 6,
        3, 85, 4, 3, 19, 10, 84, 101, 115, 116, 32, 69,
        67, 68, 83, 65, 48, 30, 23, 13, 50, 52, 48, 54,
        48, 49, 48, 48, 48, 48, 48, 48, 90, 23, 13, 50,
        54, 48, 54, 48, 49, 48, 48, 48, 48, 48, 48, 90,
        48, 21, 49, 19, 48, 17, 6, 3, 85, 4, 3, 19,
        10, 84, 101, 115, 116, 32, 69, 67, 68, 83, 65, 48,
        89, 48, 19, 6, 7, 42, (byte) 134, 72, (byte) 206, 61, 2, 1,
        6, 8, 42, (byte) 134, 72, (byte) 206, 61, 3, 1, 7, 3, 66,
        0, 4, 23, 120, 68, (byte) 150, 126, 9, (byte) 196, 71, 112, (byte) 190,
        (byte) 235, 16, (byte) 200, (byte) 230, 127, 1, (byte) 217, (byte) 240, 52, 43, 91, (byte) 209,
        (byte) 239, (byte) 137, (byte) 235, 112, (byte) 207, (byte) 225, (byte) 215, (byte) 178, (byte) 195, 17, 123, 25,
        61, (byte) 189, 75, 105, (byte) 147, (byte) 236, (byte) 250, (byte) 196, (byte) 252, 6, (byte) 212, 37,
        (byte) 179, 105, 72, 45, 125, 26, 90, 5, 123, 127, (byte) 207, (byte) 180,
        118, 0, (byte) 200, 62, (byte) 246, 106, (byte) 163, 18, 48, 16, 48, 14,
        6, 3, 85, 29, 15, 1, 1, (byte) 255, 4, 4, 3, 2,
        7, (byte) 128, 48, 10, 6, 8, 42, (byte) 134, 72, (byte) 206, 61, 4,
        3, 2, 3, 72, 0, 48, 69, 2, 32, 99, 91, 123,
        80, 81, 100, (byte) 236, (byte) 186, (byte) 142, (byte) 238, (byte) 139, (byte) 143, (byte) 159, 25, 4,
        (byte) 171, (byte) 214, (byte) 168, 81, 103, 100, (byte) 232, (byte) 192, (byte) 175, (byte) 166, (byte) 233, 37,
        103, (byte) 221, (byte) 176, 24, 70, 2, 33, 0, (byte) 172, 92, 79, (byte) 142,
        (byte) 202, (byte) 228, (byte) 238, (byte) 172, (byte) 164, (byte) 209, 38, 112, (byte) 149, 54, (byte) 136, (byte) 211,
        (byte) 214, 91, (byte) 200, 112, (byte) 138, 5, (byte) 167, (byte) 160, (byte) 150, (byte) 247, (byte) 231, 105,
        3, 110, (byte) 253, 102,
    };
    /** RSA self-signed cert DER (bytes copied from test/corpus/wave3/crypto/x509/01_parse.go). */
    private static final byte[] RSA_CERT_DER = {
        48, (byte) 130, 2, (byte) 180, 48, (byte) 130, 1, (byte) 156, (byte) 160, 3, 2, 1,
        2, 2, 2, 48, 57, 48, 13, 6, 9, 42, (byte) 134, 72,
        (byte) 134, (byte) 247, 13, 1, 1, 11, 5, 0, 48, 19, 49, 17,
        48, 15, 6, 3, 85, 4, 3, 19, 8, 84, 101, 115,
        116, 32, 82, 83, 65, 48, 30, 23, 13, 50, 52, 48,
        49, 49, 53, 49, 48, 51, 48, 48, 48, 90, 23, 13,
        50, 53, 48, 49, 49, 53, 49, 48, 51, 48, 48, 48,
        90, 48, 19, 49, 17, 48, 15, 6, 3, 85, 4, 3,
        19, 8, 84, 101, 115, 116, 32, 82, 83, 65, 48, (byte) 130,
        1, 34, 48, 13, 6, 9, 42, (byte) 134, 72, (byte) 134, (byte) 247, 13,
        1, 1, 1, 5, 0, 3, (byte) 130, 1, 15, 0, 48, (byte) 130,
        1, 10, 2, (byte) 130, 1, 1, 0, (byte) 178, (byte) 163, (byte) 129, 63, (byte) 156,
        116, (byte) 167, 13, (byte) 143, 27, (byte) 128, (byte) 141, 26, 7, 52, (byte) 247, 59,
        (byte) 181, 31, (byte) 226, (byte) 159, (byte) 186, (byte) 203, 107, (byte) 236, (byte) 218, 28, 75, (byte) 198,
        (byte) 128, (byte) 174, 2, 71, 105, (byte) 132, 51, 117, 98, 109, 73, 65,
        71, 37, 45, (byte) 239, 86, (byte) 163, (byte) 146, (byte) 205, (byte) 211, (byte) 235, (byte) 242, (byte) 254,
        20, 1, 33, (byte) 211, 44, (byte) 179, (byte) 238, (byte) 215, (byte) 188, 122, (byte) 158, (byte) 246,
        92, (byte) 200, (byte) 132, (byte) 245, 126, 35, 1, 25, (byte) 143, (byte) 180, (byte) 250, 119,
        (byte) 238, 57, 42, 34, (byte) 248, 90, (byte) 189, 10, 100, 21, 45, (byte) 155,
        7, 75, (byte) 206, 68, 21, (byte) 180, (byte) 143, (byte) 250, (byte) 208, 58, (byte) 214, (byte) 223,
        (byte) 151, (byte) 226, (byte) 183, (byte) 164, (byte) 141, 4, (byte) 210, (byte) 187, 46, 83, (byte) 145, 103,
        (byte) 190, (byte) 208, (byte) 174, 127, (byte) 230, 51, (byte) 215, 127, 28, 44, 32, (byte) 128,
        (byte) 250, 76, 127, 15, 24, (byte) 250, 72, 24, 67, 61, 79, 74,
        110, (byte) 198, 64, 55, (byte) 153, 86, 97, (byte) 136, (byte) 242, 3, (byte) 227, 87,
        93, 31, 106, 24, 93, (byte) 201, 70, 61, 24, 1, 75, (byte) 139,
        (byte) 134, (byte) 237, 56, (byte) 224, (byte) 159, 97, 31, 26, (byte) 185, (byte) 178, 122, 32,
        42, (byte) 153, 99, (byte) 176, (byte) 197, 25, 9, (byte) 135, 10, (byte) 143, (byte) 225, 65,
        (byte) 205, (byte) 130, (byte) 130, 51, 111, 50, 98, 45, 68, 118, (byte) 235, (byte) 170,
        35, (byte) 206, (byte) 147, (byte) 193, (byte) 179, (byte) 156, 73, 92, (byte) 193, (byte) 223, (byte) 240, 70,
        28, 60, 27, 31, (byte) 199, (byte) 178, 12, 101, (byte) 192, (byte) 246, (byte) 157, (byte) 131,
        (byte) 201, (byte) 245, (byte) 139, (byte) 200, 94, 101, 11, 62, (byte) 137, 33, 64, (byte) 144,
        24, (byte) 215, 85, (byte) 132, 87, 112, (byte) 156, 13, 56, (byte) 190, (byte) 183, 20,
        107, (byte) 211, 30, (byte) 149, (byte) 210, (byte) 140, (byte) 190, (byte) 221, 70, (byte) 233, (byte) 209, 2,
        3, 1, 0, 1, (byte) 163, 18, 48, 16, 48, 14, 6, 3,
        85, 29, 15, 1, 1, (byte) 255, 4, 4, 3, 2, 7, (byte) 128,
        48, 13, 6, 9, 42, (byte) 134, 72, (byte) 134, (byte) 247, 13, 1, 1,
        11, 5, 0, 3, (byte) 130, 1, 1, 0, 55, 123, (byte) 220, 29,
        (byte) 207, 39, (byte) 132, 67, (byte) 226, 116, (byte) 210, (byte) 132, 19, (byte) 231, 3, 12,
        10, (byte) 237, (byte) 223, 48, (byte) 134, 32, 71, (byte) 156, 4, (byte) 138, 90, 22,
        91, 23, (byte) 255, 19, (byte) 220, (byte) 255, (byte) 155, 31, (byte) 243, (byte) 208, 71, (byte) 232,
        (byte) 232, 114, (byte) 155, (byte) 253, 24, (byte) 193, 88, (byte) 154, 113, (byte) 205, (byte) 199, 90,
        106, 125, 15, 61, (byte) 149, (byte) 207, (byte) 133, 101, 121, (byte) 128, (byte) 255, (byte) 173,
        (byte) 210, (byte) 220, (byte) 168, (byte) 190, 28, 68, 109, (byte) 135, (byte) 196, 33, (byte) 229, 93,
        (byte) 135, 34, (byte) 205, (byte) 230, (byte) 155, (byte) 213, (byte) 218, 76, (byte) 254, (byte) 167, (byte) 235, 123,
        (byte) 233, (byte) 197, 36, 112, (byte) 143, (byte) 209, 10, 77, (byte) 157, 10, 99, 45,
        61, 102, 18, (byte) 208, 62, (byte) 162, 2, 125, (byte) 181, 107, (byte) 186, 108,
        9, 43, (byte) 241, 84, (byte) 146, (byte) 221, (byte) 238, 50, (byte) 160, 72, 75, (byte) 180,
        (byte) 242, (byte) 196, 75, (byte) 150, 16, (byte) 239, 51, 90, (byte) 141, (byte) 141, 95, (byte) 162,
        124, 18, 97, (byte) 237, (byte) 228, (byte) 169, 28, 23, 90, (byte) 246, 84, (byte) 146,
        96, (byte) 199, (byte) 192, (byte) 180, (byte) 241, 66, 10, (byte) 139, (byte) 176, (byte) 162, (byte) 226, 2,
        (byte) 246, (byte) 250, (byte) 213, 64, (byte) 216, (byte) 210, 36, (byte) 225, (byte) 150, 95, (byte) 134, 53,
        27, 107, 92, 21, (byte) 156, 99, (byte) 217, (byte) 246, 5, 7, 43, 98,
        (byte) 179, 88, (byte) 161, (byte) 196, (byte) 210, 5, 107, (byte) 253, 111, (byte) 159, (byte) 227, 17,
        96, (byte) 254, 51, 88, 92, (byte) 144, (byte) 227, (byte) 146, (byte) 231, 35, 80, (byte) 137,
        (byte) 139, 81, (byte) 198, 117, 89, 118, 55, 90, 123, (byte) 176, (byte) 163, 37,
        62, (byte) 216, (byte) 135, 7, 19, 19, (byte) 141, (byte) 160, (byte) 135, (byte) 163, 49, 34,
        (byte) 222, 110, 121, 90, (byte) 181, (byte) 164, 73, (byte) 144, (byte) 207, (byte) 175, (byte) 169, 66,
        (byte) 246, 125, (byte) 137, 69, 114, 28, (byte) 189, 77, (byte) 168, 61, 22, 81,
    };
}
