package main

// L11 (REVIEW-2026-11-18-05): crypto/rsa — THIN, not zero: exactly one
// importer before this program (test/corpus/wave3/crypto/x509/01_parse.go).
// GenerateKey with a seeded math/rand reader (deterministic), PKCS#1 v1.5
// sign/verify, OAEP encrypt/decrypt, PKCS#1 marshal/parse round-trip.
// Only deterministic facts are printed (key bytes are never printed).

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"fmt"
	"math/big"
	mrand "math/rand"
)

// detReader is a deterministic xorshift64* stream; identical in both legs.
type detReader struct{ s uint64 }

func (r *detReader) Read(b []byte) (int, error) {
	for i := range b {
		r.s ^= r.s >> 12
		r.s ^= r.s << 25
		r.s ^= r.s >> 27
		b[i] = byte((r.s * 0x2545F4914F6CDD1D) >> 56)
	}
	return len(b), nil
}

func main() {
	// math/rand seeded reader as the deterministic randomness source.
	rng := mrand.New(mrand.NewSource(1805))
	priv, err := rsa.GenerateKey(rng, 1024)
	if err != nil {
		fmt.Printf("gen-err=%v\n", err)
		return
	}
	fmt.Printf("n-bits=%d e=%d valid=%v\n",
		priv.N.BitLen(), priv.E, priv.Validate() == nil)
	fmt.Printf("primes=%d\n", len(priv.Primes))

	// PKCS#1 v1.5 sign/verify (deterministic for a fixed key+hash).
	h := sha256.Sum256([]byte("rsa differential message"))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, h[:])
	if err != nil {
		fmt.Printf("sign-err=%v\n", err)
		return
	}
	fmt.Printf("sig-len=%d\n", len(sig))
	fmt.Printf("verify=%v\n",
		rsa.VerifyPKCS1v15(&priv.PublicKey, crypto.SHA256, h[:], sig) == nil)
	badSig := make([]byte, len(sig))
	copy(badSig, sig)
	badSig[0] ^= 0xff
	fmt.Printf("verify-tampered=%v\n",
		rsa.VerifyPKCS1v15(&priv.PublicKey, crypto.SHA256, h[:], badSig) == nil)

	// OAEP encrypt/decrypt with the deterministic reader.
	label := []byte("oaep-label")
	ct, err := rsa.EncryptOAEP(sha256.New(), &detReader{s: 0x5eed}, &priv.PublicKey,
		[]byte("secret-plaintext"), label)
	if err != nil {
		fmt.Printf("oaep-enc-err=%v\n", err)
		return
	}
	pt, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, priv, ct, label)
	fmt.Printf("oaep-decrypt=%q err=%v\n", string(pt), err)

	// PKCS#1 marshal/parse round-trip preserves D.
	der := x509.MarshalPKCS1PrivateKey(priv)
	priv2, err := x509.ParsePKCS1PrivateKey(der)
	if err != nil {
		fmt.Printf("parse-err=%v\n", err)
		return
	}
	fmt.Printf("roundtrip-d=%v\n", priv2.D.Cmp(priv.D) == 0)
	fmt.Printf("roundtrip-n=%v\n", priv2.N.Cmp(priv.N) == 0)
	fmt.Printf("pub-equal=%v\n", priv2.PublicKey.N.Cmp(priv.PublicKey.N) == 0 &&
		priv2.PublicKey.E == priv.PublicKey.E)

	// Small-exponent sanity: E is odd and >= 3.
	fmt.Printf("e-odd=%v e-big=%v\n",
		big.NewInt(int64(priv.E)).Bit(0) == 1, priv.E >= 3)
}
