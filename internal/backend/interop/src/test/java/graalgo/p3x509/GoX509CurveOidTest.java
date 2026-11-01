package graalgo.p3x509;

import java.io.ByteArrayInputStream;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;

import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Regression tests for F5 (review P1-6 follow-up, 2026-11-01): EC curve
 * identity must come from the key's actual curve OID, never from its
 * field size. secp256k1 shares P-256's 256-bit field, so the old
 * field-size mapping silently relabeled a secp256k1 key as secp256r1
 * and derived P-256 key material from it; gc rejects all three
 * mislabeled forms below.
 *
 * <p>Vectors: a P-256 key generated once by gc 1.27.1, plus the same
 * key material re-encoded with the secp256k1 OID (1.3.132.0.10) in
 * place of the P-256 OID in both OID positions (PKCS#8) / the SPKI
 * algorithm identifier / the certificate SPKI. Expected values are
 * exactly what gc returns for them (verified with a gc harness:
 * ParsePKCS8PrivateKey rejects the mislabeled PKCS#8 with "unknown
 * elliptic curve"; ParsePKIXPublicKey and ParseCertificate reject
 * the mislabeled SPKI/cert with "unsupported elliptic curve"; the
 * P-384-OID PKCS#8 variant is accepted by gc as P-384, proving the
 * identification is OID-driven). The P-224/P-256/P-384/P-521 SPKI
 * vectors are the review P1-6 corpus cases, with the curve name in
 * place of the old field-size component. NOTE on the P-384 case: on
 * this base (master) GoX509 emits minimal-length coordinates, so x
 * is "ad09..."; the approved commit 16dbc12a pads SPKI coordinates
 * to field size (toFixed), so on rebase onto it this expectation
 * becomes "00ad09..." — which is exactly what GoX509Test already
 * expects there. Only the P-384 case differs between the two.
 *
 * <p>Each rejection case is demonstrably capable of failing: the
 * pre-fix implementation accepted all three mislabeled forms (as
 * PKCS8:EC:secp256r1, EC SPKI with field size 256, and a secp256r1
 * certificate respectively).
 */
public final class GoX509CurveOidTest {

    // {name, derHex, expectedDescriptorOrEmpty}
    private static final String[][] PRIV_CASES = {
        {"pkcs8-P256-valid",
            "308187020100301306072a8648ce3d020106082a8648ce3d030107046d306b020101042044d1e4ee075273d9f56de58a69aa31a39cef2cb9b0b511d2372bbbd5039f1868a144034200040740d3aa332b37d0bcc9b7c1fca9acd3ab4656a221ba3c67e7a0a2cde2436a3ee179946af7005779a3baaced311fb23cb967bd39369c5ce119a75381350bc383",
            "PKCS8:EC:secp256r1:44d1e4ee075273d9f56de58a69aa31a39cef2cb9b0b511d2372bbbd5039f1868"},
        {"pkcs8-P256-secp256k1",
            "308184020100301006072a8648ce3d020106052b8104000a046d306b020101042044d1e4ee075273d9f56de58a69aa31a39cef2cb9b0b511d2372bbbd5039f1868a144034200040740d3aa332b37d0bcc9b7c1fca9acd3ab4656a221ba3c67e7a0a2cde2436a3ee179946af7005779a3baaced311fb23cb967bd39369c5ce119a75381350bc383",
            ""},
        {"pkcs8-P256-P384-oid",
            "308184020100301006072a8648ce3d020106052b81040022046d306b020101042044d1e4ee075273d9f56de58a69aa31a39cef2cb9b0b511d2372bbbd5039f1868a144034200040740d3aa332b37d0bcc9b7c1fca9acd3ab4656a221ba3c67e7a0a2cde2436a3ee179946af7005779a3baaced311fb23cb967bd39369c5ce119a75381350bc383",
            "PKCS8:EC:secp384r1:44d1e4ee075273d9f56de58a69aa31a39cef2cb9b0b511d2372bbbd5039f1868"},
    };

    // {name, derHex, expectedSpkiStringOrNull}
    private static final String[][] SPKI_CASES = {
        {"spki-P256-valid",
            "3059301306072a8648ce3d020106082a8648ce3d030107034200040740d3aa332b37d0bcc9b7c1fca9acd3ab4656a221ba3c67e7a0a2cde2436a3ee179946af7005779a3baaced311fb23cb967bd39369c5ce119a75381350bc383",
            "EC:0740d3aa332b37d0bcc9b7c1fca9acd3ab4656a221ba3c67e7a0a2cde2436a3e:e179946af7005779a3baaced311fb23cb967bd39369c5ce119a75381350bc383:secp256r1"},
        {"spki-P256-secp256k1",
            "3056301006072a8648ce3d020106052b8104000a034200040740d3aa332b37d0bcc9b7c1fca9acd3ab4656a221ba3c67e7a0a2cde2436a3ee179946af7005779a3baaced311fb23cb967bd39369c5ce119a75381350bc383",
            null},
        {"spki-P224",
            "304e301006072a8648ce3d020106052b81040021033a000459c1c73576d1dc485e54561fb56199f2a4380a187fc68fc17b7ec3409612fca6aa99426ad07ed2e5720e5d8c91c93420cfa0d5879d1315cf",
            "EC:59c1c73576d1dc485e54561fb56199f2a4380a187fc68fc17b7ec340:9612fca6aa99426ad07ed2e5720e5d8c91c93420cfa0d5879d1315cf:secp224r1"},
        {"spki-P256-corpus",
            "3059301306072a8648ce3d020106082a8648ce3d030107034200048a96b625224247485be947d067f29ba16447f9e713c26991a26fc460a8f9a39d0289caf7f267b697d33e7672790ffe8dde9dd6a1af0a69de00672c3cf39d8390",
            "EC:8a96b625224247485be947d067f29ba16447f9e713c26991a26fc460a8f9a39d:0289caf7f267b697d33e7672790ffe8dde9dd6a1af0a69de00672c3cf39d8390:secp256r1"},
        {"spki-P384",
            "3076301006072a8648ce3d020106052b810400220362000400ad0988457c0b31801d5714c932b3cf6040e6ada1685ba64810f4f8b547b54ae78341f39b534dae1d065b4474863446adf5e1922d629b1739514e6a37405421cd1a9b1ae96684473bf93eac22d0002dd40de4f669b9f17cad52b9c64ed0e3f7",
            "EC:ad0988457c0b31801d5714c932b3cf6040e6ada1685ba64810f4f8b547b54ae78341f39b534dae1d065b4474863446:adf5e1922d629b1739514e6a37405421cd1a9b1ae96684473bf93eac22d0002dd40de4f669b9f17cad52b9c64ed0e3f7:secp384r1"},
        {"spki-P521",
            "30819b301006072a8648ce3d020106052b8104002303818600040107449fab040afb892ac8b594f237f50f7afe8ad261085195a203a565f8afedf6a4edf7431e1d4513359970037cd861ae9b4267c2f380ec9b7182fca116d67a871101806a1ba4c02747af7bc471a75901d096646aaf8f320237c016e01651fe64b6d3645cd344b58b8d2aeceb092c0d8605d47dcb781a35c3b1633ef00eb05290433236",
            "EC:0107449fab040afb892ac8b594f237f50f7afe8ad261085195a203a565f8afedf6a4edf7431e1d4513359970037cd861ae9b4267c2f380ec9b7182fca116d67a8711:01806a1ba4c02747af7bc471a75901d096646aaf8f320237c016e01651fe64b6d3645cd344b58b8d2aeceb092c0d8605d47dcb781a35c3b1633ef00eb05290433236:secp521r1"},
    };

    // {name, derHex, expectedCurveNameOrEmpty, expectedX, expectedY}
    private static final String[][] CERT_CASES = {
        {"cert-P256-valid",
            "308201123081b8a003020102020204d2300a06082a8648ce3d04030230123110300e0603550403130754657374204543301e170d3236313130313033343631395a170d3236313130313035343631395a30123110300e06035504031307546573742045433059301306072a8648ce3d020106082a8648ce3d030107034200040740d3aa332b37d0bcc9b7c1fca9acd3ab4656a221ba3c67e7a0a2cde2436a3ee179946af7005779a3baaced311fb23cb967bd39369c5ce119a75381350bc383300a06082a8648ce3d0403020349003046022100b720f646ece6c1daad5f0ca65313ea1eb2e0e3165484d11643d0179ffaa0c17902210093e379563a8f3232b2007ab58ef302c4a563e108e2efe125a5cb037421605dff",
            "secp256r1",
            "0740d3aa332b37d0bcc9b7c1fca9acd3ab4656a221ba3c67e7a0a2cde2436a3e",
            "e179946af7005779a3baaced311fb23cb967bd39369c5ce119a75381350bc383"},
        {"cert-P256-secp256k1",
            "3082010f3081b5a003020102020204d2300a06082a8648ce3d04030230123110300e0603550403130754657374204543301e170d3236313130313033343631395a170d3236313130313035343631395a30123110300e06035504031307546573742045433056301006072a8648ce3d020106052b8104000a034200040740d3aa332b37d0bcc9b7c1fca9acd3ab4656a221ba3c67e7a0a2cde2436a3ee179946af7005779a3baaced311fb23cb967bd39369c5ce119a75381350bc383300a06082a8648ce3d0403020349003046022100b720f646ece6c1daad5f0ca65313ea1eb2e0e3165484d11643d0179ffaa0c17902210093e379563a8f3232b2007ab58ef302c4a563e108e2efe125a5cb037421605dff",
            "",
            "0740d3aa332b37d0bcc9b7c1fca9acd3ab4656a221ba3c67e7a0a2cde2436a3e",
            "e179946af7005779a3baaced311fb23cb967bd39369c5ce119a75381350bc383"},
    };

    public void testPkcs8CurveOidMatchesGc() {
        for (String[] c : PRIV_CASES) {
            Object r = GoX509.parsePrivateKey(goSlice(hex(c[1])));
            String got = r == null ? "<null>" : ((GoString) r).toString();
            if (c[2].isEmpty()) {
                check(got.isEmpty(), c[0] + ": gc rejects, GoX509 accepted: " + got);
            } else {
                check(got.equals(c[2]), c[0] + ": expected " + c[2] + " got " + got);
            }
        }
    }

    public void testSpkiCurveNameMatchesGc() {
        for (String[] c : SPKI_CASES) {
            Object r = GoX509.parseSpki(goSlice(hex(c[1])));
            if (c[2] == null) {
                check(r == null, c[0] + ": gc rejects, GoX509 accepted: "
                        + (r == null ? "<null>" : ((GoString) r).toString()));
            } else {
                check(r != null, c[0] + ": GoX509 rejected, gc accepts");
                check(((GoString) r).toString().equals(c[2]),
                        c[0] + ": expected " + c[2] + " got " + ((GoString) r).toString());
            }
        }
    }

    public void testCertCurveNameMatchesGc() throws Exception {
        CertificateFactory f = CertificateFactory.getInstance("X.509");
        for (String[] c : CERT_CASES) {
            X509Certificate cert = (X509Certificate) f.generateCertificate(
                    new ByteArrayInputStream(hex(c[1])));
            String name = ((GoString) GoX509.ecCurveName(cert)).toString();
            check(name.equals(c[2]),
                    c[0] + ": expected curve " + c[2] + " got " + name);
            String x = hexOf(sliceBytes((GoSlice) GoX509.ecX(cert)));
            String y = hexOf(sliceBytes((GoSlice) GoX509.ecY(cert)));
            check(x.equals(c[3]) && y.equals(c[4]),
                    c[0] + ": expected " + c[3] + ":" + c[4] + " got " + x + ":" + y);
        }
    }

    private static byte[] hex(String s) {
        byte[] out = new byte[s.length() / 2];
        for (int i = 0; i < out.length; i++) {
            out[i] = (byte) Integer.parseInt(s.substring(2 * i, 2 * i + 2), 16);
        }
        return out;
    }

    private static String hexOf(byte[] b) {
        StringBuilder sb = new StringBuilder(b.length * 2);
        for (byte v : b) {
            sb.append(Character.forDigit((v >> 4) & 0xF, 16));
            sb.append(Character.forDigit(v & 0xF, 16));
        }
        return sb.toString();
    }

    private static byte[] sliceBytes(GoSlice s) {
        byte[] arr = (byte[]) s.array();
        byte[] out = new byte[s.len()];
        System.arraycopy(arr, s.offset(), out, 0, out.length);
        return out;
    }

    private static GoSlice goSlice(byte[] der) {
        return new GoSlice(der, 0, der.length, der.length);
    }

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }
}
