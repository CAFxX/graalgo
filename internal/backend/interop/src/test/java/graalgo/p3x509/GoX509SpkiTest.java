package graalgo.p3x509;

import java.nio.charset.StandardCharsets;
import java.security.KeyPair;
import java.security.KeyPairGenerator;
import java.security.interfaces.ECPublicKey;
import java.security.spec.ECGenParameterSpec;

import graalgo.values.GoString;

/**
 * Unit tests for the SPKI public-key path curve identification
 * (codebase-review 2026-10-19, SPKI follow-up to I-4): the SPKI descriptor
 * emitted by {@link GoX509#parseSpki} must carry the curve NAME
 * ("EC:secp256r1:x:y"), never the field size. A 256-bit field does NOT
 * imply secp256r1: the old "EC:x:y:256" wire format let the Go side
 * silently build P-256 for secp256k1 keys (the I-4 bug family).
 *
 * <p>secp256k1 key <em>generation</em> is unsupported by this JDK's SunEC,
 * but secp256k1 SPKI public keys <em>parse</em> (verified on the pinned
 * GraalVM JDK 25 against a real openssl-generated encoding), so the
 * secp256k1 misidentification is exercised live, not synthetically.
 */
public final class GoX509SpkiTest {

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

    /** Runs {@code GoX509.parseSpki} on raw DER, returning the descriptor. */
    private static String parseSpki(byte[] der) {
        Object r = GoX509.parseSpki(der);
        check(r != null, "parseSpki returned null");
        check(r instanceof GoString, "parseSpki did not return a GoString");
        return new String(((GoString) r).bytes(), StandardCharsets.UTF_8);
    }

    private static KeyPair genKey(String curve) throws Exception {
        KeyPairGenerator kpg = KeyPairGenerator.getInstance("EC");
        kpg.initialize(new ECGenParameterSpec(curve));
        return kpg.generateKeyPair();
    }

    /** Strips a single leading zero byte (mirrors GoX509.stripLeadingZero). */
    private static byte[] strip(byte[] b) {
        if (b.length > 1 && b[0] == 0) {
            byte[] out = new byte[b.length - 1];
            System.arraycopy(b, 1, out, 0, out.length);
            return out;
        }
        return b;
    }

    private static String hex(byte[] b) {
        StringBuilder sb = new StringBuilder(b.length * 2);
        for (byte v : b) {
            sb.append(Character.forDigit((v >> 4) & 0xF, 16));
            sb.append(Character.forDigit(v & 0xF, 16));
        }
        return sb.toString();
    }

    /** Expected "EC:name:x:y" descriptor for a live-generated public key. */
    private static String wantSpki(String name, ECPublicKey pub) {
        return "EC:" + name + ":"
                + hex(strip(pub.getW().getAffineX().toByteArray())) + ":"
                + hex(strip(pub.getW().getAffineY().toByteArray()));
    }

    public void testSpkiSecp256r1NamedCorrectly() throws Exception {
        ECPublicKey pub = (ECPublicKey) genKey("secp256r1").getPublic();
        checkEq(wantSpki("secp256r1", pub), parseSpki(pub.getEncoded()),
                "secp256r1 SPKI descriptor");
    }

    public void testSpkiSecp384r1NamedCorrectly() throws Exception {
        ECPublicKey pub = (ECPublicKey) genKey("secp384r1").getPublic();
        checkEq(wantSpki("secp384r1", pub), parseSpki(pub.getEncoded()),
                "secp384r1 SPKI descriptor");
    }

    public void testSpkiSecp521r1NamedCorrectly() throws Exception {
        ECPublicKey pub = (ECPublicKey) genKey("secp521r1").getPublic();
        checkEq(wantSpki("secp521r1", pub), parseSpki(pub.getEncoded()),
                "secp521r1 SPKI descriptor");
    }

    public void testSpkiSecp256k1NamedNotMisidentified() throws Exception {
        // Red-green anchor: pre-fix this descriptor was "EC:<x>:<y>:256"
        // (field size) and the Go side silently built P-256 for a secp256k1
        // key. Post-fix the curve name travels and the Go side resolves it
        // by name, failing loudly on unknown names instead of guessing.
        String got = parseSpki(SECP256K1_SPKI);
        check(got.startsWith("EC:secp256k1:"),
                "secp256k1 SPKI must carry the curve name, got: " + got);
        check(!got.endsWith(":256"),
                "field size must not leak into the descriptor, got: " + got);
        check(got.split(":").length == 4,
                "SPKI descriptor must be EC:name:x:y (4 parts), got: " + got);
    }
    /** Real openssl-generated secp256k1 SPKI (SunEC parses it but cannot generate secp256k1 keys). */
    private static final byte[] SECP256K1_SPKI = {
        48, 86, 48, 16, 6, 7, 42, (byte) 134, 72, (byte) 206, 61, 2,
        1, 6, 5, 43, (byte) 129, 4, 0, 10, 3, 66, 0, 4,
        110, 104, 34, 119, 110, 35, (byte) 246, 118, (byte) 196, (byte) 209, 5, 8,
        12, (byte) 156, (byte) 181, (byte) 161, (byte) 151, 27, (byte) 199, 118, (byte) 224, (byte) 165, 86, (byte) 207,
        69, 30, 49, (byte) 146, (byte) 175, 97, (byte) 211, (byte) 165, (byte) 136, 95, 18, (byte) 218,
        33, (byte) 151, 86, 35, 47, (byte) 200, 13, 105, (byte) 252, 115, 24, (byte) 169,
        52, (byte) 128, (byte) 220, 9, (byte) 163, 22, (byte) 150, 98, 105, (byte) 171, (byte) 152, 53,
        105, (byte) 139, (byte) 130, (byte) 181,
    };
}
