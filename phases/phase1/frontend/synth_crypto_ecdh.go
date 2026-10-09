package main

// P3-M9 tier-5: synthetic 'crypto/ecdh' package.
const syntheticCryptoEcdhSource = `// P3-M9 tier-5: synthetic 'crypto/ecdh' package.
// go1.27.1 vendored sources (ecdh.go, nist.go, x25519.go).
package ecdh

import (
	"crypto"
	"crypto/internal/boring"
	"crypto/internal/fips140/ecdh"
	"crypto/subtle"
	"errors"
	"io"
	"bytes"
	"crypto/internal/fips140only"
	"crypto/internal/rand"
	"crypto/internal/fips140/edwards25519/field"
)


// ---- vendored from crypto/ecdh/ecdh.go ----




type Curve interface {
	// GenerateKey generates a random PrivateKey.
	//
	// Since Go 1.26, a secure source of random bytes is always used, and rand
	// is ignored unless GODEBUG=cryptocustomrand=1 is set. This setting will be
	// removed in a future Go release. Instead, use [testing/cryptotest.SetGlobalRandom].
	GenerateKey(rand io.Reader) (*PrivateKey, error)

	// NewPrivateKey checks that key is valid and returns a PrivateKey.
	//
	// For NIST curves, this follows SEC 1, Version 2.0, Section 2.3.6, which
	// amounts to decoding the bytes as a fixed length big endian integer and
	// checking that the result is lower than the order of the curve. The zero
	// private key is also rejected, as the encoding of the corresponding public
	// key would be irregular.
	//
	// For X25519, this only checks the scalar length.
	NewPrivateKey(key []byte) (*PrivateKey, error)

	// NewPublicKey checks that key is valid and returns a PublicKey.
	//
	// For NIST curves, this decodes an uncompressed point according to SEC 1,
	// Version 2.0, Section 2.3.4. Compressed encodings and the point at
	// infinity are rejected.
	//
	// For X25519, this only checks the u-coordinate length. Adversarially
	// selected public keys can cause ECDH to return an error.
	NewPublicKey(key []byte) (*PublicKey, error)

	// ecdh performs an ECDH exchange and returns the shared secret. It's exposed
	// as the PrivateKey.ECDH method.
	//
	// The private method also allow us to expand the ECDH interface with more
	// methods in the future without breaking backwards compatibility.
	ecdh(local *PrivateKey, remote *PublicKey) ([]byte, error)
}

// PublicKey is an ECDH public key, usually a peer's ECDH share sent over the wire.
//
// These keys can be parsed with [crypto/x509.ParsePKIXPublicKey] and encoded
// with [crypto/x509.MarshalPKIXPublicKey]. For NIST curves, they then need to
// be converted with [crypto/ecdsa.PublicKey.ECDH] after parsing.
type PublicKey struct {
	curve     Curve
	publicKey []byte
	boring    *boring.PublicKeyECDH
	fips      *ecdh.PublicKey
}

// Bytes returns a copy of the encoding of the public key.
func (k *PublicKey) Bytes() []byte {
	// Copy the public key to a fixed size buffer that can get allocated on the
	// caller's stack after inlining.
	var buf [133]byte
	return append(buf[:0], k.publicKey...)
}

// Equal returns whether x represents the same public key as k.
//
// Note that there can be equivalent public keys with different encodings which
// would return false from this check but behave the same way as inputs to ECDH.
//
// This check is performed in constant time as long as the key types and their
// curve match.
func (k *PublicKey) Equal(x crypto.PublicKey) bool {
	xx, ok := x.(*PublicKey)
	if !ok {
		return false
	}
	return k.curve == xx.curve &&
		subtle.ConstantTimeCompare(k.publicKey, xx.publicKey) == 1
}

func (k *PublicKey) Curve() Curve {
	return k.curve
}

// KeyExchanger is an interface for an opaque private key that can be used for
// key exchange operations. For example, an ECDH key kept in a hardware module.
//
// It is implemented by [PrivateKey].
type KeyExchanger interface {
	PublicKey() *PublicKey
	Curve() Curve
	ECDH(*PublicKey) ([]byte, error)
}

var _ KeyExchanger = (*PrivateKey)(nil)

// PrivateKey is an ECDH private key, usually kept secret.
//
// These keys can be parsed with [crypto/x509.ParsePKCS8PrivateKey] and encoded
// with [crypto/x509.MarshalPKCS8PrivateKey]. For NIST curves, they then need to
// be converted with [crypto/ecdsa.PrivateKey.ECDH] after parsing.
type PrivateKey struct {
	curve      Curve
	privateKey []byte
	publicKey  *PublicKey
	boring     *boring.PrivateKeyECDH
	fips       *ecdh.PrivateKey
}

// ECDH performs an ECDH exchange and returns the shared secret. The [PrivateKey]
// and [PublicKey] must use the same curve.
//
// For NIST curves, this performs ECDH as specified in SEC 1, Version 2.0,
// Section 3.3.1, and returns the x-coordinate encoded according to SEC 1,
// Version 2.0, Section 2.3.5. The result is never the point at infinity.
// This is also known as the Shared Secret Computation of the Ephemeral Unified
// Model scheme specified in NIST SP 800-56A Rev. 3, Section 6.1.2.2.
//
// For [X25519], this performs ECDH as specified in RFC 7748, Section 6.1. If
// the result is the all-zero value, ECDH returns an error.
type __mrPrivateKey_ECDHPair struct {
	v0 []byte
	v1 error
}

func __mrPrivateKey_ECDH(k *PrivateKey, remote *PublicKey) __mrPrivateKey_ECDHPair {
	if k.curve != remote.curve {
		return __mrPrivateKey_ECDHPair{nil, errors.New("crypto/ecdh: private key and public key curves do not match")}
	}
	__fw0, __fw1 := k.curve.ecdh(k, remote)
	return __mrPrivateKey_ECDHPair{__fw0, __fw1}
}

func (k *PrivateKey) ECDH(remote *PublicKey) ([]byte, error) {
	panic("crypto/ecdh: ECDH: multi-result desugar")
}

// Bytes returns a copy of the encoding of the private key.
func (k *PrivateKey) Bytes() []byte {
	// Copy the private key to a fixed size buffer that can get allocated on the
	// caller's stack after inlining.
	var buf [66]byte
	return append(buf[:0], k.privateKey...)
}

// Equal returns whether x represents the same private key as k.
//
// Note that there can be equivalent private keys with different encodings which
// would return false from this check but behave the same way as inputs to [ECDH].
//
// This check is performed in constant time as long as the key types and their
// curve match.
func (k *PrivateKey) Equal(x crypto.PrivateKey) bool {
	xx, ok := x.(*PrivateKey)
	if !ok {
		return false
	}
	return k.curve == xx.curve &&
		subtle.ConstantTimeCompare(k.privateKey, xx.privateKey) == 1
}

func (k *PrivateKey) Curve() Curve {
	return k.curve
}

func (k *PrivateKey) PublicKey() *PublicKey {
	return k.publicKey
}

// Public implements the implicit interface of all standard library private
// keys. See the docs of [crypto.PrivateKey].
func (k *PrivateKey) Public() crypto.PublicKey {
	return k.PublicKey()
}


// ---- vendored from crypto/ecdh/nist.go ----




type nistCurve struct {
	name          string
	generate      func(io.Reader) (*ecdh.PrivateKey, error)
	newPrivateKey func([]byte) (*ecdh.PrivateKey, error)
	newPublicKey  func(publicKey []byte) (*ecdh.PublicKey, error)
	sharedSecret  func(*ecdh.PrivateKey, *ecdh.PublicKey) (sharedSecret []byte, err error)
}

func (c *nistCurve) String() string {
	return c.name
}

type __mrnistCurve_GenerateKeyPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrnistCurve_GenerateKey(c *nistCurve, r io.Reader) __mrnistCurve_GenerateKeyPair {
	if boring.Enabled && rand.IsDefaultReader(r) {
		key, bytes, err := boring.GenerateKeyECDH(c.name)
		if err != nil {
			return __mrnistCurve_GenerateKeyPair{nil, err}
		}
		pub, err := key.PublicKey()
		if err != nil {
			return __mrnistCurve_GenerateKeyPair{nil, err}
		}
		k := &PrivateKey{
			curve:      c,
			privateKey: bytes,
			publicKey:  &PublicKey{curve: c, publicKey: pub.Bytes(), boring: pub},
			boring:     key,
		}
		return __mrnistCurve_GenerateKeyPair{k, nil}
	}

	r = rand.CustomReader(r)

	if fips140only.Enforced() && !fips140only.ApprovedRandomReader(r) {
		return __mrnistCurve_GenerateKeyPair{nil, errors.New("crypto/ecdh: only crypto/rand.Reader is allowed in FIPS 140-only mode")}
	}

	privateKey, err := c.generate(r)
	if err != nil {
		return __mrnistCurve_GenerateKeyPair{nil, err}
	}

	k := &PrivateKey{
		curve:      c,
		privateKey: privateKey.Bytes(),
		fips:       privateKey,
		publicKey: &PublicKey{
			curve:     c,
			publicKey: privateKey.PublicKey().Bytes(),
			fips:      privateKey.PublicKey(),
		},
	}
	if boring.Enabled {
		bk, err := boring.NewPrivateKeyECDH(c.name, k.privateKey)
		if err != nil {
			return __mrnistCurve_GenerateKeyPair{nil, err}
		}
		pub, err := bk.PublicKey()
		if err != nil {
			return __mrnistCurve_GenerateKeyPair{nil, err}
		}
		k.boring = bk
		k.publicKey.boring = pub
	}
	return __mrnistCurve_GenerateKeyPair{k, nil}
}

func (c *nistCurve) GenerateKey(r io.Reader) (*PrivateKey, error) {
	panic("crypto/ecdh: GenerateKey: multi-result desugar")
}

type __mrnistCurve_NewPrivateKeyPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrnistCurve_NewPrivateKey(c *nistCurve, key []byte) __mrnistCurve_NewPrivateKeyPair {
	if boring.Enabled {
		bk, err := boring.NewPrivateKeyECDH(c.name, key)
		if err != nil {
			return __mrnistCurve_NewPrivateKeyPair{nil, errors.New("crypto/ecdh: invalid private key")}
		}
		pub, err := bk.PublicKey()
		if err != nil {
			return __mrnistCurve_NewPrivateKeyPair{nil, errors.New("crypto/ecdh: invalid private key")}
		}
		k := &PrivateKey{
			curve:      c,
			privateKey: bytes.Clone(key),
			publicKey:  &PublicKey{curve: c, publicKey: pub.Bytes(), boring: pub},
			boring:     bk,
		}
		return __mrnistCurve_NewPrivateKeyPair{k, nil}
	}

	fk, err := c.newPrivateKey(key)
	if err != nil {
		return __mrnistCurve_NewPrivateKeyPair{nil, err}
	}
	k := &PrivateKey{
		curve:      c,
		privateKey: bytes.Clone(key),
		fips:       fk,
		publicKey: &PublicKey{
			curve:     c,
			publicKey: fk.PublicKey().Bytes(),
			fips:      fk.PublicKey(),
		},
	}
	return __mrnistCurve_NewPrivateKeyPair{k, nil}
}

func (c *nistCurve) NewPrivateKey(key []byte) (*PrivateKey, error) {
	panic("crypto/ecdh: NewPrivateKey: multi-result desugar")
}

type __mrnistCurve_NewPublicKeyPair struct {
	v0 *PublicKey
	v1 error
}

func __mrnistCurve_NewPublicKey(c *nistCurve, key []byte) __mrnistCurve_NewPublicKeyPair {
	// Reject the point at infinity and compressed encodings.
	// Note that boring.NewPublicKeyECDH would accept them.
	if len(key) == 0 || key[0] != 4 {
		return __mrnistCurve_NewPublicKeyPair{nil, errors.New("crypto/ecdh: invalid public key")}
	}
	k := &PublicKey{
		curve:     c,
		publicKey: bytes.Clone(key),
	}
	if boring.Enabled {
		bk, err := boring.NewPublicKeyECDH(c.name, k.publicKey)
		if err != nil {
			return __mrnistCurve_NewPublicKeyPair{nil, errors.New("crypto/ecdh: invalid public key")}
		}
		k.boring = bk
	} else {
		fk, err := c.newPublicKey(key)
		if err != nil {
			return __mrnistCurve_NewPublicKeyPair{nil, err}
		}
		k.fips = fk
	}
	return __mrnistCurve_NewPublicKeyPair{k, nil}
}

func (c *nistCurve) NewPublicKey(key []byte) (*PublicKey, error) {
	panic("crypto/ecdh: NewPublicKey: multi-result desugar")
}

type __mrnistCurve_ecdhPair struct {
	v0 []byte
	v1 error
}

func __mrnistCurve_ecdh(c *nistCurve, local *PrivateKey, remote *PublicKey) __mrnistCurve_ecdhPair {
	// Note that this function can't return an error, as NewPublicKey rejects
	// invalid points and the point at infinity, and NewPrivateKey rejects
	// invalid scalars and the zero value. BytesX returns an error for the point
	// at infinity, but in a prime order group such as the NIST curves that can
	// only be the result of a scalar multiplication if one of the inputs is the
	// zero scalar or the point at infinity.

	if boring.Enabled {
		__fw0, __fw1 := boring.ECDH(local.boring, remote.boring)
		return __mrnistCurve_ecdhPair{__fw0, __fw1}
	}
	__fw0, __fw1 := c.sharedSecret(local.fips, remote.fips)
	return __mrnistCurve_ecdhPair{__fw0, __fw1}
}

func (c *nistCurve) ecdh(local *PrivateKey, remote *PublicKey) ([]byte, error) {
	panic("crypto/ecdh: ecdh: multi-result desugar")
}

// P256 returns a [Curve] which implements NIST P-256 (FIPS 186-3, section D.2.3),
// also known as secp256r1 or prime256v1.
//
// Multiple invocations of this function will return the same value, which can
// be used for equality checks and switch statements.
func P256() Curve { return p256 }

var p256 = &nistCurve{
	name: "P-256",
	generate: func(r io.Reader) (*ecdh.PrivateKey, error) {
		k, err := ecdh.GenerateKey(ecdh.P256(), r)
		return k, err
	},
	newPrivateKey: func(b []byte) (*ecdh.PrivateKey, error) {
		k, err := ecdh.NewPrivateKey(ecdh.P256(), b)
		return k, err
	},
	newPublicKey: func(publicKey []byte) (*ecdh.PublicKey, error) {
		k, err := ecdh.NewPublicKey(ecdh.P256(), publicKey)
		return k, err
	},
	sharedSecret: func(priv *ecdh.PrivateKey, pub *ecdh.PublicKey) (sharedSecret []byte, err error) {
		ss, err := ecdh.ECDH(ecdh.P256(), priv, pub)
		return ss, err
	},
}

// P384 returns a [Curve] which implements NIST P-384 (FIPS 186-3, section D.2.4),
// also known as secp384r1.
//
// Multiple invocations of this function will return the same value, which can
// be used for equality checks and switch statements.
func P384() Curve { return p384 }

var p384 = &nistCurve{
	name: "P-384",
	generate: func(r io.Reader) (*ecdh.PrivateKey, error) {
		k, err := ecdh.GenerateKey(ecdh.P384(), r)
		return k, err
	},
	newPrivateKey: func(b []byte) (*ecdh.PrivateKey, error) {
		k, err := ecdh.NewPrivateKey(ecdh.P384(), b)
		return k, err
	},
	newPublicKey: func(publicKey []byte) (*ecdh.PublicKey, error) {
		k, err := ecdh.NewPublicKey(ecdh.P384(), publicKey)
		return k, err
	},
	sharedSecret: func(priv *ecdh.PrivateKey, pub *ecdh.PublicKey) (sharedSecret []byte, err error) {
		ss, err := ecdh.ECDH(ecdh.P384(), priv, pub)
		return ss, err
	},
}

// P521 returns a [Curve] which implements NIST P-521 (FIPS 186-3, section D.2.5),
// also known as secp521r1.
//
// Multiple invocations of this function will return the same value, which can
// be used for equality checks and switch statements.
func P521() Curve { return p521 }

var p521 = &nistCurve{
	name: "P-521",
	generate: func(r io.Reader) (*ecdh.PrivateKey, error) {
		k, err := ecdh.GenerateKey(ecdh.P521(), r)
		return k, err
	},
	newPrivateKey: func(b []byte) (*ecdh.PrivateKey, error) {
		k, err := ecdh.NewPrivateKey(ecdh.P521(), b)
		return k, err
	},
	newPublicKey: func(publicKey []byte) (*ecdh.PublicKey, error) {
		k, err := ecdh.NewPublicKey(ecdh.P521(), publicKey)
		return k, err
	},
	sharedSecret: func(priv *ecdh.PrivateKey, pub *ecdh.PublicKey) (sharedSecret []byte, err error) {
		ss, err := ecdh.ECDH(ecdh.P521(), priv, pub)
		return ss, err
	},
}


// ---- vendored from crypto/ecdh/x25519.go ----




var (
	x25519PublicKeySize    = 32
	x25519PrivateKeySize   = 32
	x25519SharedSecretSize = 32
)

// X25519 returns a [Curve] which implements the X25519 function over Curve25519
// (RFC 7748, Section 5).
//
// Multiple invocations of this function will return the same value, so it can
// be used for equality checks and switch statements.
func X25519() Curve { return x25519 }

var x25519 = &x25519Curve{}

type x25519Curve struct{}

func (c *x25519Curve) String() string {
	return "X25519"
}

type __mrx25519Curve_GenerateKeyPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrx25519Curve_GenerateKey(c *x25519Curve, r io.Reader) __mrx25519Curve_GenerateKeyPair {
	if fips140only.Enforced() {
		return __mrx25519Curve_GenerateKeyPair{nil, errors.New("crypto/ecdh: use of X25519 is not allowed in FIPS 140-only mode")}
	}
	r = rand.CustomReader(r)
	key := make([]byte, x25519PrivateKeySize)
	if _, err := io.ReadFull(r, key); err != nil {
		return __mrx25519Curve_GenerateKeyPair{nil, err}
	}
	__fw0, __fw1 := c.NewPrivateKey(key)
	return __mrx25519Curve_GenerateKeyPair{__fw0, __fw1}
}

func (c *x25519Curve) GenerateKey(r io.Reader) (*PrivateKey, error) {
	panic("crypto/ecdh: GenerateKey: multi-result desugar")
}

type __mrx25519Curve_NewPrivateKeyPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrx25519Curve_NewPrivateKey(c *x25519Curve, key []byte) __mrx25519Curve_NewPrivateKeyPair {
	if fips140only.Enforced() {
		return __mrx25519Curve_NewPrivateKeyPair{nil, errors.New("crypto/ecdh: use of X25519 is not allowed in FIPS 140-only mode")}
	}
	if len(key) != x25519PrivateKeySize {
		return __mrx25519Curve_NewPrivateKeyPair{nil, errors.New("crypto/ecdh: invalid private key size")}
	}
	publicKey := make([]byte, x25519PublicKeySize)
	x25519Basepoint := [32]byte{9}
	x25519ScalarMult(publicKey, key, x25519Basepoint[:])
	// We don't check for the all-zero public key here because the scalar is
	// never zero because of clamping, and the basepoint is not the identity in
	// the prime-order subgroup(s).
	return __mrx25519Curve_NewPrivateKeyPair{&PrivateKey{
			curve:      c,
			privateKey: bytes.Clone(key),
			publicKey:  &PublicKey{curve: c, publicKey: publicKey},
		}, nil}
}

func (c *x25519Curve) NewPrivateKey(key []byte) (*PrivateKey, error) {
	panic("crypto/ecdh: NewPrivateKey: multi-result desugar")
}

type __mrx25519Curve_NewPublicKeyPair struct {
	v0 *PublicKey
	v1 error
}

func __mrx25519Curve_NewPublicKey(c *x25519Curve, key []byte) __mrx25519Curve_NewPublicKeyPair {
	if fips140only.Enforced() {
		return __mrx25519Curve_NewPublicKeyPair{nil, errors.New("crypto/ecdh: use of X25519 is not allowed in FIPS 140-only mode")}
	}
	if len(key) != x25519PublicKeySize {
		return __mrx25519Curve_NewPublicKeyPair{nil, errors.New("crypto/ecdh: invalid public key")}
	}
	return __mrx25519Curve_NewPublicKeyPair{&PublicKey{
			curve:     c,
			publicKey: bytes.Clone(key),
		}, nil}
}

func (c *x25519Curve) NewPublicKey(key []byte) (*PublicKey, error) {
	panic("crypto/ecdh: NewPublicKey: multi-result desugar")
}

type __mrx25519Curve_ecdhPair struct {
	v0 []byte
	v1 error
}

func __mrx25519Curve_ecdh(c *x25519Curve, local *PrivateKey, remote *PublicKey) __mrx25519Curve_ecdhPair {
	out := make([]byte, x25519SharedSecretSize)
	x25519ScalarMult(out, local.privateKey, remote.publicKey)
	if isZero(out) {
		return __mrx25519Curve_ecdhPair{nil, errors.New("crypto/ecdh: bad X25519 remote ECDH input: low order point")}
	}
	return __mrx25519Curve_ecdhPair{out, nil}
}

func (c *x25519Curve) ecdh(local *PrivateKey, remote *PublicKey) ([]byte, error) {
	panic("crypto/ecdh: ecdh: multi-result desugar")
}

func x25519ScalarMult(dst, scalar, point []byte) {
	var e [32]byte

	copy(e[:], scalar[:])
	e[0] &= 248
	e[31] &= 127
	e[31] |= 64

	var x1, x2, z2, x3, z3, tmp0, tmp1 field.Element
	x1.SetBytes(point[:])
	x2.One()
	x3.Set(&x1)
	z3.One()

	swap := 0
	for pos := 254; pos >= 0; pos-- {
		b := e[pos/8] >> uint(pos&7)
		b &= 1
		swap ^= int(b)
		x2.Swap(&x3, swap)
		z2.Swap(&z3, swap)
		swap = int(b)

		tmp0.Subtract(&x3, &z3)
		tmp1.Subtract(&x2, &z2)
		x2.Add(&x2, &z2)
		z2.Add(&x3, &z3)
		z3.Multiply(&tmp0, &x2)
		z2.Multiply(&z2, &tmp1)
		tmp0.Square(&tmp1)
		tmp1.Square(&x2)
		x3.Add(&z3, &z2)
		z2.Subtract(&z3, &z2)
		x2.Multiply(&tmp1, &tmp0)
		tmp1.Subtract(&tmp1, &tmp0)
		z2.Square(&z2)

		z3.Mult32(&tmp1, 121666)
		x3.Square(&x3)
		tmp0.Add(&tmp0, &z3)
		z3.Multiply(&x1, &z2)
		z2.Multiply(&tmp1, &tmp0)
	}

	x2.Swap(&x3, swap)
	z2.Swap(&z3, swap)

	z2.Invert(&z2)
	x2.Multiply(&x2, &z2)
	copy(dst[:], x2.Bytes())
}

// isZero reports whether x is all zeroes in constant time.
func isZero(x []byte) bool {
	var acc byte
	for _, b := range x {
		acc |= b
	}
	return acc == 0
}

`
