// Retirement: see ../../docs/arch/synth-retirement-tracker.md#cryptoecdsa for this package's root cause and retires-when trigger (crypto/ecdsa).
package main

// P3-M9 tier-5: synthetic 'crypto/ecdsa' package.
const syntheticCryptoEcdsaSource = `// P3-M9 tier-5: synthetic 'crypto/ecdsa' package.
// go1.27.1 vendored sources (ecdsa.go, ecdsa_legacy.go, notboring.go).
// boring.go (boringcrypto build tag) excluded.
package ecdsa

import (
	"crypto"
	"crypto/ecdh"
	"crypto/elliptic"
	"crypto/internal/boring"
	"crypto/internal/boring/bbig"
	"crypto/internal/fips140/ecdsa"
	"crypto/internal/fips140/nistec"
	"crypto/internal/fips140cache"
	"crypto/internal/fips140hash"
	"crypto/internal/fips140only"
	"crypto/internal/rand"
	"crypto/sha512"
	"crypto/subtle"
	"errors"
	"io"
	"math/big"
	"golang.org/x/crypto/cryptobyte"
	"golang.org/x/crypto/cryptobyte/asn1"
	randv2 "math/rand/v2"
)


// ---- vendored from crypto/ecdsa/ecdsa.go ----




// PublicKey represents an ECDSA public key.
type PublicKey struct {
	elliptic.Curve

	// X, Y are the coordinates of the public key point.
	//
	// Deprecated: modifying the raw coordinates can produce invalid keys, and may
	// invalidate internal optimizations; moreover, [big.Int] methods are not
	// suitable for operating on cryptographic values. To encode and decode
	// PublicKey values, use [PublicKey.Bytes] and [ParseUncompressedPublicKey]
	// or [crypto/x509.MarshalPKIXPublicKey] and [crypto/x509.ParsePKIXPublicKey].
	// For ECDH, use [crypto/ecdh]. For lower-level elliptic curve operations,
	// use a third-party module like filippo.io/nistec.
	X, Y *big.Int
}

// Any methods implemented on PublicKey might need to also be implemented on
// PrivateKey, as the latter embeds the former and will expose its methods.

// ECDH returns k as a [ecdh.PublicKey]. It returns an error if the key is
// invalid according to the definition of [ecdh.Curve.NewPublicKey], or if the
// Curve is not supported by crypto/ecdh.
type __mrPublicKey_ECDHPair struct {
	v0 *ecdh.PublicKey
	v1 error
}

func __mrPublicKey_ECDH(pub *PublicKey) __mrPublicKey_ECDHPair {
	c := curveToECDH(pub.Curve)
	if c == nil {
		return __mrPublicKey_ECDHPair{nil, errors.New("ecdsa: unsupported curve by crypto/ecdh")}
	}
	k, err := pub.Bytes()
	if err != nil {
		return __mrPublicKey_ECDHPair{nil, err}
	}
	__fw0, __fw1 := c.NewPublicKey(k)
	return __mrPublicKey_ECDHPair{__fw0, __fw1}
}

func (pub *PublicKey) ECDH() (*ecdh.PublicKey, error) {
	panic("crypto/ecdsa: ECDH: multi-result desugar")
}

// Equal reports whether pub and x have the same value.
//
// Two keys are only considered to have the same value if they have the same Curve value.
// Note that for example [elliptic.P256] and elliptic.P256().Params() are different
// values, as the latter is a generic not constant time implementation.
func (pub *PublicKey) Equal(x crypto.PublicKey) bool {
	xx, ok := x.(*PublicKey)
	if !ok {
		return false
	}
	return bigIntEqual(pub.X, xx.X) && bigIntEqual(pub.Y, xx.Y) &&
		// Standard library Curve implementations are singletons, so this check
		// will work for those. Other Curves might be equivalent even if not
		// singletons, but there is no definitive way to check for that, and
		// better to err on the side of safety.
		pub.Curve == xx.Curve
}

// ParseUncompressedPublicKey parses a public key encoded as an uncompressed
// point according to SEC 1, Version 2.0, Section 2.3.3 (also known as the X9.62
// uncompressed format). It returns an error if the point is not in uncompressed
// form, is not on the curve, or is the point at infinity.
//
// curve must be one of [elliptic.P224], [elliptic.P256], [elliptic.P384], or
// [elliptic.P521], or ParseUncompressedPublicKey returns an error.
//
// ParseUncompressedPublicKey accepts the same format as
// [ecdh.Curve.NewPublicKey] does for NIST curves, but returns a [PublicKey]
// instead of an [ecdh.PublicKey].
//
// Note that public keys are more commonly encoded in DER (or PEM) format, which
// can be parsed with [crypto/x509.ParsePKIXPublicKey] (and [encoding/pem]).
type __mrParseUncompressedPublicKeyPair struct {
	v0 *PublicKey
	v1 error
}

func __mrParseUncompressedPublicKey(curve elliptic.Curve, data []byte) __mrParseUncompressedPublicKeyPair {
	if len(data) < 1 || data[0] != 4 {
		return __mrParseUncompressedPublicKeyPair{nil, errors.New("ecdsa: invalid uncompressed public key")}
	}
	switch curve {
	case elliptic.P224():
		__fw0, __fw1 := parseUncompressedPublicKey(ecdsa.P224(), curve, data)
		return __mrParseUncompressedPublicKeyPair{__fw0, __fw1}
	case elliptic.P256():
		__fw0, __fw1 := parseUncompressedPublicKey(ecdsa.P256(), curve, data)
		return __mrParseUncompressedPublicKeyPair{__fw0, __fw1}
	case elliptic.P384():
		__fw0, __fw1 := parseUncompressedPublicKey(ecdsa.P384(), curve, data)
		return __mrParseUncompressedPublicKeyPair{__fw0, __fw1}
	case elliptic.P521():
		__fw0, __fw1 := parseUncompressedPublicKey(ecdsa.P521(), curve, data)
		return __mrParseUncompressedPublicKeyPair{__fw0, __fw1}
	default:
		return __mrParseUncompressedPublicKeyPair{nil, errors.New("ecdsa: curve not supported by ParseUncompressedPublicKey")}
	}
}

func ParseUncompressedPublicKey(curve elliptic.Curve, data []byte) (*PublicKey, error) {
	panic("crypto/ecdsa: ParseUncompressedPublicKey: multi-result desugar")
}

type __mrparseUncompressedPublicKeyPair struct {
	v0 *PublicKey
	v1 error
}

func __mrparseUncompressedPublicKey[P ecdsa.Point[P]](c *ecdsa.Curve[P], curve elliptic.Curve, data []byte) __mrparseUncompressedPublicKeyPair {
	k, err := ecdsa.NewPublicKey(c, data)
	if err != nil {
		return __mrparseUncompressedPublicKeyPair{nil, err}
	}
	__fw0, __fw1 := publicKeyFromFIPS(curve, k)
	return __mrparseUncompressedPublicKeyPair{__fw0, __fw1}
}

func parseUncompressedPublicKey[P ecdsa.Point[P]](c *ecdsa.Curve[P], curve elliptic.Curve, data []byte) (*PublicKey, error) {
	panic("crypto/ecdsa: parseUncompressedPublicKey: multi-result desugar")
}

// Bytes encodes the public key as an uncompressed point according to SEC 1,
// Version 2.0, Section 2.3.3 (also known as the X9.62 uncompressed format).
// It returns an error if the public key is invalid.
//
// PublicKey.Curve must be one of [elliptic.P224], [elliptic.P256],
// [elliptic.P384], or [elliptic.P521], or Bytes returns an error.
//
// Bytes returns the same format as [ecdh.PublicKey.Bytes] does for NIST curves.
//
// Note that public keys are more commonly encoded in DER (or PEM) format, which
// can be generated with [crypto/x509.MarshalPKIXPublicKey] (and [encoding/pem]).
type __mrPublicKey_BytesPair struct {
	v0 []byte
	v1 error
}

func __mrPublicKey_Bytes(pub *PublicKey) __mrPublicKey_BytesPair {
	switch pub.Curve {
	case elliptic.P224():
		__fw0, __fw1 := publicKeyBytes(ecdsa.P224(), pub)
		return __mrPublicKey_BytesPair{__fw0, __fw1}
	case elliptic.P256():
		__fw0, __fw1 := publicKeyBytes(ecdsa.P256(), pub)
		return __mrPublicKey_BytesPair{__fw0, __fw1}
	case elliptic.P384():
		__fw0, __fw1 := publicKeyBytes(ecdsa.P384(), pub)
		return __mrPublicKey_BytesPair{__fw0, __fw1}
	case elliptic.P521():
		__fw0, __fw1 := publicKeyBytes(ecdsa.P521(), pub)
		return __mrPublicKey_BytesPair{__fw0, __fw1}
	default:
		return __mrPublicKey_BytesPair{nil, errors.New("ecdsa: curve not supported by PublicKey.Bytes")}
	}
}

func (pub *PublicKey) Bytes() ([]byte, error) {
	panic("crypto/ecdsa: Bytes: multi-result desugar")
}

type __mrpublicKeyBytesPair struct {
	v0 []byte
	v1 error
}

func __mrpublicKeyBytes[P ecdsa.Point[P]](c *ecdsa.Curve[P], pub *PublicKey) __mrpublicKeyBytesPair {
	k, err := publicKeyToFIPS(c, pub)
	if err != nil {
		return __mrpublicKeyBytesPair{nil, err}
	}
	return __mrpublicKeyBytesPair{k.Bytes(), nil}
}

func publicKeyBytes[P ecdsa.Point[P]](c *ecdsa.Curve[P], pub *PublicKey) ([]byte, error) {
	panic("crypto/ecdsa: publicKeyBytes: multi-result desugar")
}

// PrivateKey represents an ECDSA private key.
type PrivateKey struct {
	PublicKey

	// D is the private scalar value.
	//
	// Deprecated: modifying the raw value can produce invalid keys, and may
	// invalidate internal optimizations; moreover, [big.Int] methods are not
	// suitable for operating on cryptographic values. To encode and decode
	// PrivateKey values, use [PrivateKey.Bytes] and [ParseRawPrivateKey] or
	// [crypto/x509.MarshalPKCS8PrivateKey] and [crypto/x509.ParsePKCS8PrivateKey].
	// For ECDH, use [crypto/ecdh].
	D *big.Int
}

// ECDH returns k as a [ecdh.PrivateKey]. It returns an error if the key is
// invalid according to the definition of [ecdh.Curve.NewPrivateKey], or if the
// Curve is not supported by [crypto/ecdh].
type __mrPrivateKey_ECDHPair struct {
	v0 *ecdh.PrivateKey
	v1 error
}

func __mrPrivateKey_ECDH(priv *PrivateKey) __mrPrivateKey_ECDHPair {
	c := curveToECDH(priv.Curve)
	if c == nil {
		return __mrPrivateKey_ECDHPair{nil, errors.New("ecdsa: unsupported curve by crypto/ecdh")}
	}
	k, err := priv.Bytes()
	if err != nil {
		return __mrPrivateKey_ECDHPair{nil, err}
	}
	__fw0, __fw1 := c.NewPrivateKey(k)
	return __mrPrivateKey_ECDHPair{__fw0, __fw1}
}

func (priv *PrivateKey) ECDH() (*ecdh.PrivateKey, error) {
	panic("crypto/ecdsa: ECDH: multi-result desugar")
}

func curveToECDH(c elliptic.Curve) ecdh.Curve {
	switch c {
	case elliptic.P256():
		return ecdh.P256()
	case elliptic.P384():
		return ecdh.P384()
	case elliptic.P521():
		return ecdh.P521()
	default:
		return nil
	}
}

// Public returns the public key corresponding to priv.
func (priv *PrivateKey) Public() crypto.PublicKey {
	return &priv.PublicKey
}

// Equal reports whether priv and x have the same value.
//
// See [PublicKey.Equal] for details on how Curve is compared.
func (priv *PrivateKey) Equal(x crypto.PrivateKey) bool {
	xx, ok := x.(*PrivateKey)
	if !ok {
		return false
	}
	return priv.PublicKey.Equal(&xx.PublicKey) && bigIntEqual(priv.D, xx.D)
}

// bigIntEqual reports whether a and b are equal leaking only their bit length
// through timing side-channels.
func bigIntEqual(a, b *big.Int) bool {
	return subtle.ConstantTimeCompare(a.Bytes(), b.Bytes()) == 1
}

// ParseRawPrivateKey parses a private key encoded as a fixed-length big-endian
// integer, according to SEC 1, Version 2.0, Section 2.3.6 (sometimes referred
// to as the raw format). It returns an error if the value is not reduced modulo
// the curve's order, or if it's zero.
//
// curve must be one of [elliptic.P224], [elliptic.P256], [elliptic.P384], or
// [elliptic.P521], or ParseRawPrivateKey returns an error.
//
// ParseRawPrivateKey accepts the same format as [ecdh.Curve.NewPrivateKey] does
// for NIST curves, but returns a [PrivateKey] instead of an [ecdh.PrivateKey].
//
// Note that private keys are more commonly encoded in ASN.1 or PKCS#8 format,
// which can be parsed with [crypto/x509.ParseECPrivateKey] or
// [crypto/x509.ParsePKCS8PrivateKey] (and [encoding/pem]).
type __mrParseRawPrivateKeyPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrParseRawPrivateKey(curve elliptic.Curve, data []byte) __mrParseRawPrivateKeyPair {
	switch curve {
	case elliptic.P224():
		__fw0, __fw1 := parseRawPrivateKey(ecdsa.P224(), nistec.NewP224Point, curve, data)
		return __mrParseRawPrivateKeyPair{__fw0, __fw1}
	case elliptic.P256():
		__fw0, __fw1 := parseRawPrivateKey(ecdsa.P256(), nistec.NewP256Point, curve, data)
		return __mrParseRawPrivateKeyPair{__fw0, __fw1}
	case elliptic.P384():
		__fw0, __fw1 := parseRawPrivateKey(ecdsa.P384(), nistec.NewP384Point, curve, data)
		return __mrParseRawPrivateKeyPair{__fw0, __fw1}
	case elliptic.P521():
		__fw0, __fw1 := parseRawPrivateKey(ecdsa.P521(), nistec.NewP521Point, curve, data)
		return __mrParseRawPrivateKeyPair{__fw0, __fw1}
	default:
		return __mrParseRawPrivateKeyPair{nil, errors.New("ecdsa: curve not supported by ParseRawPrivateKey")}
	}
}

func ParseRawPrivateKey(curve elliptic.Curve, data []byte) (*PrivateKey, error) {
	panic("crypto/ecdsa: ParseRawPrivateKey: multi-result desugar")
}

type __mrparseRawPrivateKeyPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrparseRawPrivateKey[P ecdsa.Point[P]](c *ecdsa.Curve[P], newPoint func() P, curve elliptic.Curve, data []byte) __mrparseRawPrivateKeyPair {
	q, err := newPoint().ScalarBaseMult(data)
	if err != nil {
		return __mrparseRawPrivateKeyPair{nil, err}
	}
	k, err := ecdsa.NewPrivateKey(c, data, q.Bytes())
	if err != nil {
		return __mrparseRawPrivateKeyPair{nil, err}
	}
	__fw0, __fw1 := privateKeyFromFIPS(curve, k)
	return __mrparseRawPrivateKeyPair{__fw0, __fw1}
}

func parseRawPrivateKey[P ecdsa.Point[P]](c *ecdsa.Curve[P], newPoint func() P, curve elliptic.Curve, data []byte) (*PrivateKey, error) {
	panic("crypto/ecdsa: parseRawPrivateKey: multi-result desugar")
}

// Bytes encodes the private key as a fixed-length big-endian integer according
// to SEC 1, Version 2.0, Section 2.3.6 (sometimes referred to as the raw
// format). It returns an error if the private key is invalid.
//
// PrivateKey.Curve must be one of [elliptic.P224], [elliptic.P256],
// [elliptic.P384], or [elliptic.P521], or Bytes returns an error.
//
// Bytes returns the same format as [ecdh.PrivateKey.Bytes] does for NIST curves.
//
// Note that private keys are more commonly encoded in ASN.1 or PKCS#8 format,
// which can be generated with [crypto/x509.MarshalECPrivateKey] or
// [crypto/x509.MarshalPKCS8PrivateKey] (and [encoding/pem]).
type __mrPrivateKey_BytesPair struct {
	v0 []byte
	v1 error
}

func __mrPrivateKey_Bytes(priv *PrivateKey) __mrPrivateKey_BytesPair {
	switch priv.Curve {
	case elliptic.P224():
		__fw0, __fw1 := privateKeyBytes(ecdsa.P224(), priv)
		return __mrPrivateKey_BytesPair{__fw0, __fw1}
	case elliptic.P256():
		__fw0, __fw1 := privateKeyBytes(ecdsa.P256(), priv)
		return __mrPrivateKey_BytesPair{__fw0, __fw1}
	case elliptic.P384():
		__fw0, __fw1 := privateKeyBytes(ecdsa.P384(), priv)
		return __mrPrivateKey_BytesPair{__fw0, __fw1}
	case elliptic.P521():
		__fw0, __fw1 := privateKeyBytes(ecdsa.P521(), priv)
		return __mrPrivateKey_BytesPair{__fw0, __fw1}
	default:
		return __mrPrivateKey_BytesPair{nil, errors.New("ecdsa: curve not supported by PrivateKey.Bytes")}
	}
}

func (priv *PrivateKey) Bytes() ([]byte, error) {
	panic("crypto/ecdsa: Bytes: multi-result desugar")
}

type __mrprivateKeyBytesPair struct {
	v0 []byte
	v1 error
}

func __mrprivateKeyBytes[P ecdsa.Point[P]](c *ecdsa.Curve[P], priv *PrivateKey) __mrprivateKeyBytesPair {
	k, err := privateKeyToFIPS(c, priv)
	if err != nil {
		return __mrprivateKeyBytesPair{nil, err}
	}
	return __mrprivateKeyBytesPair{k.Bytes(), nil}
}

func privateKeyBytes[P ecdsa.Point[P]](c *ecdsa.Curve[P], priv *PrivateKey) ([]byte, error) {
	panic("crypto/ecdsa: privateKeyBytes: multi-result desugar")
}

// Sign signs a hash (which should be the result of hashing a larger message
// with opts.HashFunc()) using the private key, priv. If the hash is longer than
// the bit-length of the private key's curve order, the hash will be truncated
// to that length. It returns the ASN.1 encoded signature, like [SignASN1].
//
// If random is not nil, the signature is randomized. Most applications should use
// [crypto/rand.Reader] as random, but unless GODEBUG=cryptocustomrand=1 is set, a
// secure source of random bytes is always used, and the actual Reader is ignored.
// The GODEBUG setting will be removed in a future Go release. Instead, use
// [testing/cryptotest.SetGlobalRandom].
//
// If random is nil, Sign will produce a deterministic signature according to RFC
// 6979. When producing a deterministic signature, opts.HashFunc() must be the
// function used to produce digest and priv.Curve must be one of
// [elliptic.P224], [elliptic.P256], [elliptic.P384], or [elliptic.P521].
type __mrPrivateKey_SignPair struct {
	v0 []byte
	v1 error
}

func __mrPrivateKey_Sign(priv *PrivateKey, random io.Reader, digest []byte, opts crypto.SignerOpts) __mrPrivateKey_SignPair {
	if opts != nil {
		h := opts.HashFunc()
		if h == 0 {
			return __mrPrivateKey_SignPair{nil, errors.New("ecdsa: Sign must be called with a hash, not with crypto.Hash(0)")}
		}
		if h.Size() != len(digest) {
			return __mrPrivateKey_SignPair{nil, errors.New("ecdsa: hash length does not match hash function")}
		}
	}
	if random == nil {
		__fw0, __fw1 := signRFC6979(priv, digest, opts)
		return __mrPrivateKey_SignPair{__fw0, __fw1}
	}
	random = rand.CustomReader(random)
	__fw0, __fw1 := SignASN1(random, priv, digest)
	return __mrPrivateKey_SignPair{__fw0, __fw1}
}

func (priv *PrivateKey) Sign(random io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	panic("crypto/ecdsa: Sign: multi-result desugar")
}

// GenerateKey generates a new ECDSA private key for the specified curve.
//
// Since Go 1.26, a secure source of random bytes is always used, and the Reader is
// ignored unless GODEBUG=cryptocustomrand=1 is set. This setting will be removed
// in a future Go release. Instead, use [testing/cryptotest.SetGlobalRandom].
type __mrGenerateKeyPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrGenerateKey(c elliptic.Curve, r io.Reader) __mrGenerateKeyPair {
	if boring.Enabled && rand.IsDefaultReader(r) {
		x, y, d, err := boring.GenerateKeyECDSA(c.Params().Name)
		if err != nil {
			return __mrGenerateKeyPair{nil, err}
		}
		return __mrGenerateKeyPair{&PrivateKey{PublicKey: PublicKey{Curve: c, X: bbig.Dec(x), Y: bbig.Dec(y)}, D: bbig.Dec(d)}, nil}
	}
	boring.UnreachableExceptTests()

	r = rand.CustomReader(r)

	switch c.Params() {
	case elliptic.P224().Params():
		__fw0, __fw1 := generateFIPS(c, ecdsa.P224(), r)
		return __mrGenerateKeyPair{__fw0, __fw1}
	case elliptic.P256().Params():
		__fw0, __fw1 := generateFIPS(c, ecdsa.P256(), r)
		return __mrGenerateKeyPair{__fw0, __fw1}
	case elliptic.P384().Params():
		__fw0, __fw1 := generateFIPS(c, ecdsa.P384(), r)
		return __mrGenerateKeyPair{__fw0, __fw1}
	case elliptic.P521().Params():
		__fw0, __fw1 := generateFIPS(c, ecdsa.P521(), r)
		return __mrGenerateKeyPair{__fw0, __fw1}
	default:
		__fw0, __fw1 := generateLegacy(c, r)
		return __mrGenerateKeyPair{__fw0, __fw1}
	}
}

func GenerateKey(c elliptic.Curve, r io.Reader) (*PrivateKey, error) {
	panic("crypto/ecdsa: GenerateKey: multi-result desugar")
}

type __mrgenerateFIPSPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrgenerateFIPS[P ecdsa.Point[P]](curve elliptic.Curve, c *ecdsa.Curve[P], rand io.Reader) __mrgenerateFIPSPair {
	if fips140only.Enforced() && !fips140only.ApprovedRandomReader(rand) {
		return __mrgenerateFIPSPair{nil, errors.New("crypto/ecdsa: only crypto/rand.Reader is allowed in FIPS 140-only mode")}
	}
	privateKey, err := ecdsa.GenerateKey(c, rand)
	if err != nil {
		return __mrgenerateFIPSPair{nil, err}
	}
	__fw0, __fw1 := privateKeyFromFIPS(curve, privateKey)
	return __mrgenerateFIPSPair{__fw0, __fw1}
}

func generateFIPS[P ecdsa.Point[P]](curve elliptic.Curve, c *ecdsa.Curve[P], rand io.Reader) (*PrivateKey, error) {
	panic("crypto/ecdsa: generateFIPS: multi-result desugar")
}

// SignASN1 signs a hash (which should be the result of hashing a larger message)
// using the private key, priv. If the hash is longer than the bit-length of the
// private key's curve order, the hash will be truncated to that length. It
// returns the ASN.1 encoded signature.
//
// The signature is randomized. Since Go 1.26, a secure source of random bytes
// is always used, and the Reader is ignored unless GODEBUG=cryptocustomrand=1
// is set. This setting will be removed in a future Go release. Instead, use
// [testing/cryptotest.SetGlobalRandom].
type __mrSignASN1Pair struct {
	v0 []byte
	v1 error
}

func __mrSignASN1(r io.Reader, priv *PrivateKey, hash []byte) __mrSignASN1Pair {
	if len(hash) == 0 {
		return __mrSignASN1Pair{nil, errors.New("ecdsa: hash cannot be empty")}
	}

	if boring.Enabled && rand.IsDefaultReader(r) {
		b, err := boringPrivateKey(priv)
		if err != nil {
			return __mrSignASN1Pair{nil, err}
		}
		__fw0, __fw1 := boring.SignMarshalECDSA(b, hash)
		return __mrSignASN1Pair{__fw0, __fw1}
	}
	boring.UnreachableExceptTests()

	r = rand.CustomReader(r)

	switch priv.Curve.Params() {
	case elliptic.P224().Params():
		__fw0, __fw1 := signFIPS(ecdsa.P224(), priv, r, hash)
		return __mrSignASN1Pair{__fw0, __fw1}
	case elliptic.P256().Params():
		__fw0, __fw1 := signFIPS(ecdsa.P256(), priv, r, hash)
		return __mrSignASN1Pair{__fw0, __fw1}
	case elliptic.P384().Params():
		__fw0, __fw1 := signFIPS(ecdsa.P384(), priv, r, hash)
		return __mrSignASN1Pair{__fw0, __fw1}
	case elliptic.P521().Params():
		__fw0, __fw1 := signFIPS(ecdsa.P521(), priv, r, hash)
		return __mrSignASN1Pair{__fw0, __fw1}
	default:
		__fw0, __fw1 := signLegacy(priv, r, hash)
		return __mrSignASN1Pair{__fw0, __fw1}
	}
}

func SignASN1(r io.Reader, priv *PrivateKey, hash []byte) ([]byte, error) {
	panic("crypto/ecdsa: SignASN1: multi-result desugar")
}

type __mrsignFIPSPair struct {
	v0 []byte
	v1 error
}

func __mrsignFIPS[P ecdsa.Point[P]](c *ecdsa.Curve[P], priv *PrivateKey, rand io.Reader, hash []byte) __mrsignFIPSPair {
	if fips140only.Enforced() && !fips140only.ApprovedRandomReader(rand) {
		return __mrsignFIPSPair{nil, errors.New("crypto/ecdsa: only crypto/rand.Reader is allowed in FIPS 140-only mode")}
	}
	k, err := privateKeyToFIPS(c, priv)
	if err != nil {
		return __mrsignFIPSPair{nil, err}
	}
	// Always using SHA-512 instead of the hash that computed hash is
	// technically a violation of draft-irtf-cfrg-det-sigs-with-noise-04 but in
	// our API we don't get to know what it was, and this has no security impact.
	sig, err := ecdsa.Sign(c, sha512.New, k, rand, hash)
	if err != nil {
		return __mrsignFIPSPair{nil, err}
	}
	__fw0, __fw1 := encodeSignature(sig.R, sig.S)
	return __mrsignFIPSPair{__fw0, __fw1}
}

func signFIPS[P ecdsa.Point[P]](c *ecdsa.Curve[P], priv *PrivateKey, rand io.Reader, hash []byte) ([]byte, error) {
	panic("crypto/ecdsa: signFIPS: multi-result desugar")
}

type __mrsignRFC6979Pair struct {
	v0 []byte
	v1 error
}

func __mrsignRFC6979(priv *PrivateKey, hash []byte, opts crypto.SignerOpts) __mrsignRFC6979Pair {
	if opts == nil {
		return __mrsignRFC6979Pair{nil, errors.New("ecdsa: Sign called with nil random and nil opts")}
	}
	h := opts.HashFunc()
	switch priv.Curve.Params() {
	case elliptic.P224().Params():
		__fw0, __fw1 := signFIPSDeterministic(ecdsa.P224(), h, priv, hash)
		return __mrsignRFC6979Pair{__fw0, __fw1}
	case elliptic.P256().Params():
		__fw0, __fw1 := signFIPSDeterministic(ecdsa.P256(), h, priv, hash)
		return __mrsignRFC6979Pair{__fw0, __fw1}
	case elliptic.P384().Params():
		__fw0, __fw1 := signFIPSDeterministic(ecdsa.P384(), h, priv, hash)
		return __mrsignRFC6979Pair{__fw0, __fw1}
	case elliptic.P521().Params():
		__fw0, __fw1 := signFIPSDeterministic(ecdsa.P521(), h, priv, hash)
		return __mrsignRFC6979Pair{__fw0, __fw1}
	default:
		return __mrsignRFC6979Pair{nil, errors.New("ecdsa: curve not supported by deterministic signatures")}
	}
}

func signRFC6979(priv *PrivateKey, hash []byte, opts crypto.SignerOpts) ([]byte, error) {
	panic("crypto/ecdsa: signRFC6979: multi-result desugar")
}

type __mrsignFIPSDeterministicPair struct {
	v0 []byte
	v1 error
}

func __mrsignFIPSDeterministic[P ecdsa.Point[P]](c *ecdsa.Curve[P], hashFunc crypto.Hash, priv *PrivateKey, hash []byte) __mrsignFIPSDeterministicPair {
	k, err := privateKeyToFIPS(c, priv)
	if err != nil {
		return __mrsignFIPSDeterministicPair{nil, err}
	}
	if !hashFunc.Available() {
		return __mrsignFIPSDeterministicPair{nil, errors.New("ecdsa: requested hash function unavailable: " + hashFunc.String())}
	}
	h := fips140hash.UnwrapNew(hashFunc.New)
	if fips140only.Enforced() && !fips140only.ApprovedHash(h()) {
		return __mrsignFIPSDeterministicPair{nil, errors.New("crypto/ecdsa: use of hash functions other than SHA-2 or SHA-3 is not allowed in FIPS 140-only mode")}
	}
	sig, err := ecdsa.SignDeterministic(c, h, k, hash)
	if err != nil {
		return __mrsignFIPSDeterministicPair{nil, err}
	}
	__fw0, __fw1 := encodeSignature(sig.R, sig.S)
	return __mrsignFIPSDeterministicPair{__fw0, __fw1}
}

func signFIPSDeterministic[P ecdsa.Point[P]](c *ecdsa.Curve[P], hashFunc crypto.Hash, priv *PrivateKey, hash []byte) ([]byte, error) {
	panic("crypto/ecdsa: signFIPSDeterministic: multi-result desugar")
}

type __mrencodeSignaturePair struct {
	v0 []byte
	v1 error
}

func __mrencodeSignature(r, s []byte) __mrencodeSignaturePair {
	var b cryptobyte.Builder
	b.AddASN1(asn1.SEQUENCE, func(b *cryptobyte.Builder) {
		addASN1IntBytes(b, r)
		addASN1IntBytes(b, s)
	})
	__fw0, __fw1 := b.Bytes()
	return __mrencodeSignaturePair{__fw0, __fw1}
}

func encodeSignature(r, s []byte) ([]byte, error) {
	panic("crypto/ecdsa: encodeSignature: multi-result desugar")
}

// addASN1IntBytes encodes in ASN.1 a positive integer represented as
// a big-endian byte slice with zero or more leading zeroes.
func addASN1IntBytes(b *cryptobyte.Builder, bytes []byte) {
	for len(bytes) > 0 && bytes[0] == 0 {
		bytes = bytes[1:]
	}
	if len(bytes) == 0 {
		b.SetError(errors.New("invalid integer"))
		return
	}
	b.AddASN1(asn1.INTEGER, func(c *cryptobyte.Builder) {
		if bytes[0]&0x80 != 0 {
			c.AddUint8(0)
		}
		c.AddBytes(bytes)
	})
}

// VerifyASN1 verifies the ASN.1 encoded signature, sig, of hash using the
// public key, pub. Its return value records whether the signature is valid.
//
// The inputs are not considered confidential, and may leak through timing side
// channels, or if an attacker has control of part of the inputs.
func VerifyASN1(pub *PublicKey, hash, sig []byte) bool {
	if len(hash) == 0 {
		return false
	}

	if boring.Enabled {
		key, err := boringPublicKey(pub)
		if err != nil {
			return false
		}
		return boring.VerifyECDSA(key, hash, sig)
	}
	boring.UnreachableExceptTests()

	switch pub.Curve.Params() {
	case elliptic.P224().Params():
		return verifyFIPS(ecdsa.P224(), pub, hash, sig)
	case elliptic.P256().Params():
		return verifyFIPS(ecdsa.P256(), pub, hash, sig)
	case elliptic.P384().Params():
		return verifyFIPS(ecdsa.P384(), pub, hash, sig)
	case elliptic.P521().Params():
		return verifyFIPS(ecdsa.P521(), pub, hash, sig)
	default:
		return verifyLegacy(pub, hash, sig)
	}
}

func verifyFIPS[P ecdsa.Point[P]](c *ecdsa.Curve[P], pub *PublicKey, hash, sig []byte) bool {
	r, s, err := parseSignature(sig)
	if err != nil {
		return false
	}
	k, err := publicKeyToFIPS(c, pub)
	if err != nil {
		return false
	}
	if err := ecdsa.Verify(c, k, hash, &ecdsa.Signature{R: r, S: s}); err != nil {
		return false
	}
	return true
}

type __mrparseSignaturePair struct {
	r []byte
	s []byte
	err error
}

func __mrparseSignature(sig []byte) __mrparseSignaturePair {
	var r []byte
	var s []byte

	var inner cryptobyte.String
	input := cryptobyte.String(sig)
	if !input.ReadASN1(&inner, asn1.SEQUENCE) ||
		!input.Empty() ||
		!inner.ReadASN1Integer(&r) ||
		!inner.ReadASN1Integer(&s) ||
		!inner.Empty() {
		return __mrparseSignaturePair{nil, nil, errors.New("invalid ASN.1")}
	}
	return __mrparseSignaturePair{r, s, nil}
}

func parseSignature(sig []byte) (r, s []byte, err error) {
	panic("crypto/ecdsa: parseSignature: multi-result desugar")
}

type __mrpublicKeyFromFIPSPair struct {
	v0 *PublicKey
	v1 error
}

func __mrpublicKeyFromFIPS(curve elliptic.Curve, pub *ecdsa.PublicKey) __mrpublicKeyFromFIPSPair {
	x, y, err := pointToAffine(curve, pub.Bytes())
	if err != nil {
		return __mrpublicKeyFromFIPSPair{nil, err}
	}
	return __mrpublicKeyFromFIPSPair{&PublicKey{Curve: curve, X: x, Y: y}, nil}
}

func publicKeyFromFIPS(curve elliptic.Curve, pub *ecdsa.PublicKey) (*PublicKey, error) {
	panic("crypto/ecdsa: publicKeyFromFIPS: multi-result desugar")
}

type __mrprivateKeyFromFIPSPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrprivateKeyFromFIPS(curve elliptic.Curve, priv *ecdsa.PrivateKey) __mrprivateKeyFromFIPSPair {
	pub, err := publicKeyFromFIPS(curve, priv.PublicKey())
	if err != nil {
		return __mrprivateKeyFromFIPSPair{nil, err}
	}
	return __mrprivateKeyFromFIPSPair{&PrivateKey{PublicKey: *pub, D: new(big.Int).SetBytes(priv.Bytes())}, nil}
}

func privateKeyFromFIPS(curve elliptic.Curve, priv *ecdsa.PrivateKey) (*PrivateKey, error) {
	panic("crypto/ecdsa: privateKeyFromFIPS: multi-result desugar")
}

type __mrpublicKeyToFIPSPair struct {
	v0 *ecdsa.PublicKey
	v1 error
}

func __mrpublicKeyToFIPS[P ecdsa.Point[P]](c *ecdsa.Curve[P], pub *PublicKey) __mrpublicKeyToFIPSPair {
	Q, err := pointFromAffine(pub.Curve, pub.X, pub.Y)
	if err != nil {
		return __mrpublicKeyToFIPSPair{nil, err}
	}
	__fw0, __fw1 := ecdsa.NewPublicKey(c, Q)
	return __mrpublicKeyToFIPSPair{__fw0, __fw1}
}

func publicKeyToFIPS[P ecdsa.Point[P]](c *ecdsa.Curve[P], pub *PublicKey) (*ecdsa.PublicKey, error) {
	panic("crypto/ecdsa: publicKeyToFIPS: multi-result desugar")
}

var privateKeyCache fips140cache.Cache[PrivateKey, ecdsa.PrivateKey]

type __mrprivateKeyToFIPSPair struct {
	v0 *ecdsa.PrivateKey
	v1 error
}

func __mrprivateKeyToFIPS[P ecdsa.Point[P]](c *ecdsa.Curve[P], priv *PrivateKey) __mrprivateKeyToFIPSPair {
	Q, err := pointFromAffine(priv.Curve, priv.X, priv.Y)
	if err != nil {
		return __mrprivateKeyToFIPSPair{nil, err}
	}

	// Reject values that would not get correctly encoded.
	if priv.D.BitLen() > priv.Curve.Params().N.BitLen() {
		return __mrprivateKeyToFIPSPair{nil, errors.New("ecdsa: private key scalar too large")}
	}
	if priv.D.Sign() <= 0 {
		return __mrprivateKeyToFIPSPair{nil, errors.New("ecdsa: private key scalar is zero or negative")}
	}

	size := (priv.Curve.Params().N.BitLen() + 7) / 8
	const maxScalarSize = 66 // enough for a P-521 private key
	if size > maxScalarSize {
		return __mrprivateKeyToFIPSPair{nil, errors.New("ecdsa: internal error: curve size too large")}
	}
	D := priv.D.FillBytes(make([]byte, size, maxScalarSize))

	k, err := ecdsa.NewPrivateKey(c, D, Q)
	return __mrprivateKeyToFIPSPair{k, err}
}

func privateKeyToFIPS[P ecdsa.Point[P]](c *ecdsa.Curve[P], priv *PrivateKey) (*ecdsa.PrivateKey, error) {
	panic("crypto/ecdsa: privateKeyToFIPS: multi-result desugar")
}

// pointFromAffine is used to convert the PublicKey to a nistec SetBytes input.
type __mrpointFromAffinePair struct {
	v0 []byte
	v1 error
}

func __mrpointFromAffine(curve elliptic.Curve, x, y *big.Int) __mrpointFromAffinePair {
	bitSize := curve.Params().BitSize
	// Reject values that would not get correctly encoded.
	if x.Sign() < 0 || y.Sign() < 0 {
		return __mrpointFromAffinePair{nil, errors.New("negative coordinate")}
	}
	if x.BitLen() > bitSize || y.BitLen() > bitSize {
		return __mrpointFromAffinePair{nil, errors.New("overflowing coordinate")}
	}
	// Encode the coordinates and let [ecdsa.NewPublicKey] reject invalid points.
	byteLen := (bitSize + 7) / 8
	buf := make([]byte, 1+2*byteLen)
	buf[0] = 4 // uncompressed point
	x.FillBytes(buf[1 : 1+byteLen])
	y.FillBytes(buf[1+byteLen : 1+2*byteLen])
	return __mrpointFromAffinePair{buf, nil}
}

func pointFromAffine(curve elliptic.Curve, x, y *big.Int) ([]byte, error) {
	panic("crypto/ecdsa: pointFromAffine: multi-result desugar")
}

// pointToAffine is used to convert a nistec Bytes encoding to a PublicKey.
type __mrpointToAffinePair struct {
	x *big.Int
	y *big.Int
	err error
}

func __mrpointToAffine(curve elliptic.Curve, p []byte) __mrpointToAffinePair {
	var x *big.Int
	var y *big.Int

	if len(p) == 1 && p[0] == 0 {
		// This is the encoding of the point at infinity.
		return __mrpointToAffinePair{nil, nil, errors.New("ecdsa: public key point is the infinity")}
	}
	byteLen := (curve.Params().BitSize + 7) / 8
	x = new(big.Int).SetBytes(p[1 : 1+byteLen])
	y = new(big.Int).SetBytes(p[1+byteLen:])
	return __mrpointToAffinePair{x, y, nil}
}

func pointToAffine(curve elliptic.Curve, p []byte) (x, y *big.Int, err error) {
	panic("crypto/ecdsa: pointToAffine: multi-result desugar")
}


// ---- vendored from crypto/ecdsa/ecdsa_legacy.go ----




// This file contains a math/big implementation of ECDSA that is only used for
// deprecated custom curves.

type __mrgenerateLegacyPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrgenerateLegacy(c elliptic.Curve, rand io.Reader) __mrgenerateLegacyPair {
	if fips140only.Enforced() {
		return __mrgenerateLegacyPair{nil, errors.New("crypto/ecdsa: use of custom curves is not allowed in FIPS 140-only mode")}
	}

	k, err := randFieldElement(c, rand)
	if err != nil {
		return __mrgenerateLegacyPair{nil, err}
	}

	priv := new(PrivateKey)
	priv.PublicKey.Curve = c
	priv.D = k
	priv.PublicKey.X, priv.PublicKey.Y = c.ScalarBaseMult(k.Bytes())
	return __mrgenerateLegacyPair{priv, nil}
}

func generateLegacy(c elliptic.Curve, rand io.Reader) (*PrivateKey, error) {
	panic("crypto/ecdsa: generateLegacy: multi-result desugar")
}

// hashToInt converts a hash value to an integer. Per FIPS 186-4, Section 6.4,
// we use the left-most bits of the hash to match the bit-length of the order of
// the curve. This also performs Step 5 of SEC 1, Version 2.0, Section 4.1.3.
func hashToInt(hash []byte, c elliptic.Curve) *big.Int {
	orderBits := c.Params().N.BitLen()
	orderBytes := (orderBits + 7) / 8
	if len(hash) > orderBytes {
		hash = hash[:orderBytes]
	}

	ret := new(big.Int).SetBytes(hash)
	excess := len(hash)*8 - orderBits
	if excess > 0 {
		ret.Rsh(ret, uint(excess))
	}
	return ret
}

var errZeroParam = errors.New("zero parameter")

// Sign signs a hash (which should be the result of hashing a larger message)
// using the private key, priv. If the hash is longer than the bit-length of the
// private key's curve order, the hash will be truncated to that length. It
// returns the signature as a pair of integers. Most applications should use
// [SignASN1] instead of dealing directly with r, s.
//
// The signature is randomized. Since Go 1.26, a secure source of random bytes
// is always used, and the Reader is ignored unless GODEBUG=cryptocustomrand=1
// is set. This setting will be removed in a future Go release. Instead, use
// [testing/cryptotest.SetGlobalRandom].
type __mrSignPair struct {
	r *big.Int
	s *big.Int
	err error
}

func __mrSign(rand io.Reader, priv *PrivateKey, hash []byte) __mrSignPair {
	var r *big.Int
	var s *big.Int
	var err error

	sig, err := SignASN1(rand, priv, hash)
	if err != nil {
		return __mrSignPair{nil, nil, err}
	}

	r, s = new(big.Int), new(big.Int)
	var inner cryptobyte.String
	input := cryptobyte.String(sig)
	if !input.ReadASN1(&inner, asn1.SEQUENCE) ||
		!input.Empty() ||
		!inner.ReadASN1Integer(r) ||
		!inner.ReadASN1Integer(s) ||
		!inner.Empty() {
		return __mrSignPair{nil, nil, errors.New("invalid ASN.1 from SignASN1")}
	}
	return __mrSignPair{r, s, nil}
}

func Sign(rand io.Reader, priv *PrivateKey, hash []byte) (r, s *big.Int, err error) {
	panic("crypto/ecdsa: Sign: multi-result desugar")
}

type __mrsignLegacyPair struct {
	sig []byte
	err error
}

func __mrsignLegacy(priv *PrivateKey, csprng io.Reader, hash []byte) __mrsignLegacyPair {
	var err error

	if fips140only.Enforced() {
		return __mrsignLegacyPair{nil, errors.New("crypto/ecdsa: use of custom curves is not allowed in FIPS 140-only mode")}
	}

	c := priv.Curve

	// A cheap version of hedged signatures, for the deprecated path.
	var seed [32]byte
	if _, err := io.ReadFull(csprng, seed[:]); err != nil {
		return __mrsignLegacyPair{nil, err}
	}
	for i, b := range priv.D.Bytes() {
		seed[i%32] ^= b
	}
	for i, b := range hash {
		seed[i%32] ^= b
	}
	csprng = randv2.NewChaCha8(seed)

	// SEC 1, Version 2.0, Section 4.1.3
	N := c.Params().N
	if N.Sign() == 0 {
		return __mrsignLegacyPair{nil, errZeroParam}
	}
	var k, kInv, r, s *big.Int
	for {
		for {
			k, err = randFieldElement(c, csprng)
			if err != nil {
				return __mrsignLegacyPair{nil, err}
			}

			kInv = new(big.Int).ModInverse(k, N)

			r, _ = c.ScalarBaseMult(k.Bytes())
			r.Mod(r, N)
			if r.Sign() != 0 {
				break
			}
		}

		e := hashToInt(hash, c)
		s = new(big.Int).Mul(priv.D, r)
		s.Add(s, e)
		s.Mul(s, kInv)
		s.Mod(s, N) // N != 0
		if s.Sign() != 0 {
			break
		}
	}

	__fw0, __fw1 := encodeSignature(r.Bytes(), s.Bytes())
	return __mrsignLegacyPair{__fw0, __fw1}
}

func signLegacy(priv *PrivateKey, csprng io.Reader, hash []byte) (sig []byte, err error) {
	panic("crypto/ecdsa: signLegacy: multi-result desugar")
}

// Verify verifies the signature in r, s of hash using the public key, pub. Its
// return value records whether the signature is valid. Most applications should
// use VerifyASN1 instead of dealing directly with r, s.
//
// The inputs are not considered confidential, and may leak through timing side
// channels, or if an attacker has control of part of the inputs.
func Verify(pub *PublicKey, hash []byte, r, s *big.Int) bool {
	if r.Sign() <= 0 || s.Sign() <= 0 {
		return false
	}
	sig, err := encodeSignature(r.Bytes(), s.Bytes())
	if err != nil {
		return false
	}
	return VerifyASN1(pub, hash, sig)
}

func verifyLegacy(pub *PublicKey, hash []byte, sig []byte) bool {
	if fips140only.Enforced() {
		panic("crypto/ecdsa: use of custom curves is not allowed in FIPS 140-only mode")
	}

	rBytes, sBytes, err := parseSignature(sig)
	if err != nil {
		return false
	}
	r, s := new(big.Int).SetBytes(rBytes), new(big.Int).SetBytes(sBytes)

	c := pub.Curve
	N := c.Params().N

	if r.Sign() <= 0 || s.Sign() <= 0 {
		return false
	}
	if r.Cmp(N) >= 0 || s.Cmp(N) >= 0 {
		return false
	}

	// SEC 1, Version 2.0, Section 4.1.4
	e := hashToInt(hash, c)
	w := new(big.Int).ModInverse(s, N)

	u1 := e.Mul(e, w)
	u1.Mod(u1, N)
	u2 := w.Mul(r, w)
	u2.Mod(u2, N)

	x1, y1 := c.ScalarBaseMult(u1.Bytes())
	x2, y2 := c.ScalarMult(pub.X, pub.Y, u2.Bytes())
	x, y := c.Add(x1, y1, x2, y2)

	if x.Sign() == 0 && y.Sign() == 0 {
		return false
	}
	x.Mod(x, N)
	return x.Cmp(r) == 0
}

var one = new(big.Int).SetInt64(1)

// randFieldElement returns a random element of the order of the given
// curve using the procedure given in FIPS 186-4, Appendix B.5.2.
type __mrrandFieldElementPair struct {
	k *big.Int
	err error
}

func __mrrandFieldElement(c elliptic.Curve, rand io.Reader) __mrrandFieldElementPair {
	var k *big.Int
	var err error

	for {
		N := c.Params().N
		b := make([]byte, (N.BitLen()+7)/8)
		if _, err = io.ReadFull(rand, b); err != nil {
			return __mrrandFieldElementPair{k, err}
		}
		if excess := len(b)*8 - N.BitLen(); excess > 0 {
			b[0] >>= excess
		}
		k = new(big.Int).SetBytes(b)
		if k.Sign() != 0 && k.Cmp(N) < 0 {
			return __mrrandFieldElementPair{k, err}
		}
	}
}

func randFieldElement(c elliptic.Curve, rand io.Reader) (k *big.Int, err error) {
	panic("crypto/ecdsa: randFieldElement: multi-result desugar")
}


// ---- vendored from crypto/ecdsa/notboring.go ----



type __mrboringPublicKeyPair struct {
	v0 *boring.PublicKeyECDSA
	v1 error
}

func __mrboringPublicKey(*PublicKey) __mrboringPublicKeyPair {
	panic("boringcrypto: not available")
}

func boringPublicKey(*PublicKey) (*boring.PublicKeyECDSA, error) {
	panic("crypto/ecdsa: boringPublicKey: multi-result desugar")
}
type __mrboringPrivateKeyPair struct {
	v0 *boring.PrivateKeyECDSA
	v1 error
}

func __mrboringPrivateKey(*PrivateKey) __mrboringPrivateKeyPair {
	panic("boringcrypto: not available")
}

func boringPrivateKey(*PrivateKey) (*boring.PrivateKeyECDSA, error) {
	panic("crypto/ecdsa: boringPrivateKey: multi-result desugar")
}

`
