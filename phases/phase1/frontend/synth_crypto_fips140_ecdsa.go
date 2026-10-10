package main

// P3-M9 tier-5: synthetic 'crypto/internal/fips140/ecdsa' package.
const syntheticCryptoFips140EcdsaSource = `// P3-M9 tier-5: synthetic 'crypto/internal/fips140/ecdsa'.
// go1.27.1 vendored ecdsa.go + hmacdrbg.go (cast.go excluded).
// Synthetic-only.
package ecdsa

import (
	"bytes"
	"crypto/internal/fips140"
	"crypto/internal/fips140/bigmod"
	"crypto/internal/fips140/drbg"
	"crypto/internal/fips140/nistec"
	"errors"
	"hash"
	"io"
	"math/bits"
	"crypto/internal/fips140/hmac"
)


// ---- vendored from crypto/internal/fips140/ecdsa/ecdsa.go ----




// PrivateKey and PublicKey are not generic to make it possible to use them
// in other types without instantiating them with a specific point type.
// They are tied to one of the Curve types below through the curveID field.

type PrivateKey struct {
	pub PublicKey
	d   []byte // bigmod.(*Nat).Bytes output (same length as the curve order)
}

func (priv *PrivateKey) Bytes() []byte {
	return priv.d
}

func (priv *PrivateKey) PublicKey() *PublicKey {
	return &priv.pub
}

type PublicKey struct {
	curve curveID
	q     []byte // uncompressed nistec Point.Bytes output
}

func (pub *PublicKey) Bytes() []byte {
	return pub.q
}

type curveID string

const (
	p224 curveID = "P-224"
	p256 curveID = "P-256"
	p384 curveID = "P-384"
	p521 curveID = "P-521"
)

type Curve[P Point[P]] struct {
	curve      curveID
	newPoint   func() P
	ordInverse func(*[4]uint64)
	N          *bigmod.Modulus
	nMinus2    []byte
}

// Point is a generic constraint for the [nistec] Point types.
type Point[P any] interface {
	*nistec.P224Point | *nistec.P256Point | *nistec.P384Point | *nistec.P521Point
	Bytes() []byte
	BytesX() ([]byte, error)
	SetBytes([]byte) (P, error)
	ScalarMult(P, []byte) (P, error)
	ScalarBaseMult([]byte) (P, error)
	Add(p1, p2 P) P
}

// P3-M9 (WS H + WS D, graalgo-wsh-wsd-interaction): pair types for
// Point[P] interface multi-result methods dispatched via the WS H
// ifacemethod fallback. The v0 field is interface{} (not P) to keep the
// wire type non-generic; at runtime the itab dispatches to the concrete
// nistec helper which returns its own pair -- field extraction is by
// name (v0, v1), so the concrete type flows through.
type __mrPointIfaceSetBytesPair struct {
	v0 interface{}
	v1 error
}
type __mrPointIfaceBytesXPair struct {
	v0 []byte
	v1 error
}
type __mrPointIfaceScalarMultPair struct {
	v0 interface{}
	v1 error
}
type __mrPointIfaceScalarBaseMultPair struct {
	v0 interface{}
	v1 error
}

func P224() *Curve[*nistec.P224Point] { return _P224 }

var _P224 = func() *Curve[*nistec.P224Point] {
	c := &Curve[*nistec.P224Point]{
		curve:    p224,
		newPoint: nistec.NewP224Point,
	}
	{
	__n, __err := bigmod.NewModulus(p224Order)
	if __err != nil {
		panic(__err)
	}
	c.N = __n
	two, _ := bigmod.NewNat().SetBytes([]byte{2}, c.N)
	c.nMinus2 = bigmod.NewNat().ExpandFor(c.N).Sub(two, c.N).Bytes(c.N)}
	return c
}()

var p224Order = []byte{
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x16, 0xa2,
	0xe0, 0xb8, 0xf0, 0x3e, 0x13, 0xdd, 0x29, 0x45,
	0x5c, 0x5c, 0x2a, 0x3d,
}

func P256() *Curve[*nistec.P256Point] { return _P256 }

var _P256 = func() *Curve[*nistec.P256Point] {
	c := &Curve[*nistec.P256Point]{
		curve:      p256,
		newPoint:   nistec.NewP256Point,
		ordInverse: nistec.P256OrdInverse,
	}
	{
	__n, __err := bigmod.NewModulus(p256Order)
	if __err != nil {
		panic(__err)
	}
	c.N = __n
	two, _ := bigmod.NewNat().SetBytes([]byte{2}, c.N)
	c.nMinus2 = bigmod.NewNat().ExpandFor(c.N).Sub(two, c.N).Bytes(c.N)}
	return c
}()

var p256Order = []byte{
	0xff, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x00,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xbc, 0xe6, 0xfa, 0xad, 0xa7, 0x17, 0x9e, 0x84,
	0xf3, 0xb9, 0xca, 0xc2, 0xfc, 0x63, 0x25, 0x51}

func P384() *Curve[*nistec.P384Point] { return _P384 }

var _P384 = func() *Curve[*nistec.P384Point] {
	c := &Curve[*nistec.P384Point]{
		curve:    p384,
		newPoint: nistec.NewP384Point,
	}
	{
	__n, __err := bigmod.NewModulus(p384Order)
	if __err != nil {
		panic(__err)
	}
	c.N = __n
	two, _ := bigmod.NewNat().SetBytes([]byte{2}, c.N)
	c.nMinus2 = bigmod.NewNat().ExpandFor(c.N).Sub(two, c.N).Bytes(c.N)}
	return c
}()

var p384Order = []byte{
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xc7, 0x63, 0x4d, 0x81, 0xf4, 0x37, 0x2d, 0xdf,
	0x58, 0x1a, 0x0d, 0xb2, 0x48, 0xb0, 0xa7, 0x7a,
	0xec, 0xec, 0x19, 0x6a, 0xcc, 0xc5, 0x29, 0x73}

func P521() *Curve[*nistec.P521Point] { return _P521 }

var _P521 = func() *Curve[*nistec.P521Point] {
	c := &Curve[*nistec.P521Point]{
		curve:    p521,
		newPoint: nistec.NewP521Point,
	}
	{
	__n, __err := bigmod.NewModulus(p521Order)
	if __err != nil {
		panic(__err)
	}
	c.N = __n
	two, _ := bigmod.NewNat().SetBytes([]byte{2}, c.N)
	c.nMinus2 = bigmod.NewNat().ExpandFor(c.N).Sub(two, c.N).Bytes(c.N)}
	return c
}()

var p521Order = []byte{0x01, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xfa,
	0x51, 0x86, 0x87, 0x83, 0xbf, 0x2f, 0x96, 0x6b,
	0x7f, 0xcc, 0x01, 0x48, 0xf7, 0x09, 0xa5, 0xd0,
	0x3b, 0xb5, 0xc9, 0xb8, 0x89, 0x9c, 0x47, 0xae,
	0xbb, 0x6f, 0xb7, 0x1e, 0x91, 0x38, 0x64, 0x09}

// NewPrivateKey creates a new ECDSA private key from the given D and Q byte
// slices. D must be the fixed-length big-endian encoding of the private scalar,
// and Q must be the compressed or uncompressed encoding of the public point.
type __mrNewPrivateKeyPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrNewPrivateKeyP224(c *Curve[*nistec.P224Point], D, Q []byte) __mrNewPrivateKeyPair {
	fips140.RecordApproved()
	pub, err := NewPublicKeyP224(c, Q)
	if err != nil {
		return __mrNewPrivateKeyPair{nil, err}
	}
	if len(D) != c.N.Size() {
		return __mrNewPrivateKeyPair{nil, errors.New("ecdsa: invalid private key length")}
	}
	d, err := bigmod.NewNat().SetBytes(D, c.N)
	if err != nil {
		return __mrNewPrivateKeyPair{nil, err}
	}
	if d.IsZero() == 1 {
		return __mrNewPrivateKeyPair{nil, errors.New("ecdsa: private key is zero")}
	}
	priv := &PrivateKey{pub: *pub, d: d.Bytes(c.N)}
	return __mrNewPrivateKeyPair{priv, nil}
}
func __mrNewPrivateKeyP256(c *Curve[*nistec.P256Point], D, Q []byte) __mrNewPrivateKeyPair {
	fips140.RecordApproved()
	pub, err := NewPublicKeyP256(c, Q)
	if err != nil {
		return __mrNewPrivateKeyPair{nil, err}
	}
	if len(D) != c.N.Size() {
		return __mrNewPrivateKeyPair{nil, errors.New("ecdsa: invalid private key length")}
	}
	d, err := bigmod.NewNat().SetBytes(D, c.N)
	if err != nil {
		return __mrNewPrivateKeyPair{nil, err}
	}
	if d.IsZero() == 1 {
		return __mrNewPrivateKeyPair{nil, errors.New("ecdsa: private key is zero")}
	}
	priv := &PrivateKey{pub: *pub, d: d.Bytes(c.N)}
	return __mrNewPrivateKeyPair{priv, nil}
}
func __mrNewPrivateKeyP384(c *Curve[*nistec.P384Point], D, Q []byte) __mrNewPrivateKeyPair {
	fips140.RecordApproved()
	pub, err := NewPublicKeyP384(c, Q)
	if err != nil {
		return __mrNewPrivateKeyPair{nil, err}
	}
	if len(D) != c.N.Size() {
		return __mrNewPrivateKeyPair{nil, errors.New("ecdsa: invalid private key length")}
	}
	d, err := bigmod.NewNat().SetBytes(D, c.N)
	if err != nil {
		return __mrNewPrivateKeyPair{nil, err}
	}
	if d.IsZero() == 1 {
		return __mrNewPrivateKeyPair{nil, errors.New("ecdsa: private key is zero")}
	}
	priv := &PrivateKey{pub: *pub, d: d.Bytes(c.N)}
	return __mrNewPrivateKeyPair{priv, nil}
}
func __mrNewPrivateKeyP521(c *Curve[*nistec.P521Point], D, Q []byte) __mrNewPrivateKeyPair {
	fips140.RecordApproved()
	pub, err := NewPublicKeyP521(c, Q)
	if err != nil {
		return __mrNewPrivateKeyPair{nil, err}
	}
	if len(D) != c.N.Size() {
		return __mrNewPrivateKeyPair{nil, errors.New("ecdsa: invalid private key length")}
	}
	d, err := bigmod.NewNat().SetBytes(D, c.N)
	if err != nil {
		return __mrNewPrivateKeyPair{nil, err}
	}
	if d.IsZero() == 1 {
		return __mrNewPrivateKeyPair{nil, errors.New("ecdsa: private key is zero")}
	}
	priv := &PrivateKey{pub: *pub, d: d.Bytes(c.N)}
	return __mrNewPrivateKeyPair{priv, nil}
}

func NewPrivateKeyP224(c *Curve[*nistec.P224Point], D, Q []byte) (*PrivateKey, error) {
	panic("crypto/internal/fips140/ecdsa: NewPrivateKey: multi-result desugar")
}
func NewPrivateKeyP256(c *Curve[*nistec.P256Point], D, Q []byte) (*PrivateKey, error) {
	panic("crypto/internal/fips140/ecdsa: NewPrivateKey: multi-result desugar")
}
func NewPrivateKeyP384(c *Curve[*nistec.P384Point], D, Q []byte) (*PrivateKey, error) {
	panic("crypto/internal/fips140/ecdsa: NewPrivateKey: multi-result desugar")
}
func NewPrivateKeyP521(c *Curve[*nistec.P521Point], D, Q []byte) (*PrivateKey, error) {
	panic("crypto/internal/fips140/ecdsa: NewPrivateKey: multi-result desugar")
}

// NewPublicKey creates a new ECDSA public key from the given Q byte slice.
// Q must be the compressed or uncompressed encoding of the public point.
type __mrNewPublicKeyPair struct {
	v0 *PublicKey
	v1 error
}

func __mrNewPublicKeyP224(c *Curve[*nistec.P224Point], Q []byte) __mrNewPublicKeyPair {
	// SetBytes checks that Q is a valid point on the curve, and that its
	// coordinates are reduced modulo p, fulfilling the requirements of SP
	// 800-89, Section 5.3.2.
	if len(Q) < 1 || Q[0] == 0 {
		return __mrNewPublicKeyPair{nil, errors.New("ecdsa: invalid public key encoding")}
	}
	_, err := c.newPoint().SetBytes(Q)
	if err != nil {
		return __mrNewPublicKeyPair{nil, err}
	}
	return __mrNewPublicKeyPair{&PublicKey{curve: c.curve, q: Q}, nil}
}
func __mrNewPublicKeyP256(c *Curve[*nistec.P256Point], Q []byte) __mrNewPublicKeyPair {
	// SetBytes checks that Q is a valid point on the curve, and that its
	// coordinates are reduced modulo p, fulfilling the requirements of SP
	// 800-89, Section 5.3.2.
	if len(Q) < 1 || Q[0] == 0 {
		return __mrNewPublicKeyPair{nil, errors.New("ecdsa: invalid public key encoding")}
	}
	_, err := c.newPoint().SetBytes(Q)
	if err != nil {
		return __mrNewPublicKeyPair{nil, err}
	}
	return __mrNewPublicKeyPair{&PublicKey{curve: c.curve, q: Q}, nil}
}
func __mrNewPublicKeyP384(c *Curve[*nistec.P384Point], Q []byte) __mrNewPublicKeyPair {
	// SetBytes checks that Q is a valid point on the curve, and that its
	// coordinates are reduced modulo p, fulfilling the requirements of SP
	// 800-89, Section 5.3.2.
	if len(Q) < 1 || Q[0] == 0 {
		return __mrNewPublicKeyPair{nil, errors.New("ecdsa: invalid public key encoding")}
	}
	_, err := c.newPoint().SetBytes(Q)
	if err != nil {
		return __mrNewPublicKeyPair{nil, err}
	}
	return __mrNewPublicKeyPair{&PublicKey{curve: c.curve, q: Q}, nil}
}
func __mrNewPublicKeyP521(c *Curve[*nistec.P521Point], Q []byte) __mrNewPublicKeyPair {
	// SetBytes checks that Q is a valid point on the curve, and that its
	// coordinates are reduced modulo p, fulfilling the requirements of SP
	// 800-89, Section 5.3.2.
	if len(Q) < 1 || Q[0] == 0 {
		return __mrNewPublicKeyPair{nil, errors.New("ecdsa: invalid public key encoding")}
	}
	_, err := c.newPoint().SetBytes(Q)
	if err != nil {
		return __mrNewPublicKeyPair{nil, err}
	}
	return __mrNewPublicKeyPair{&PublicKey{curve: c.curve, q: Q}, nil}
}

func NewPublicKeyP224(c *Curve[*nistec.P224Point], Q []byte) (*PublicKey, error) {
	panic("crypto/internal/fips140/ecdsa: NewPublicKey: multi-result desugar")
}
func NewPublicKeyP256(c *Curve[*nistec.P256Point], Q []byte) (*PublicKey, error) {
	panic("crypto/internal/fips140/ecdsa: NewPublicKey: multi-result desugar")
}
func NewPublicKeyP384(c *Curve[*nistec.P384Point], Q []byte) (*PublicKey, error) {
	panic("crypto/internal/fips140/ecdsa: NewPublicKey: multi-result desugar")
}
func NewPublicKeyP521(c *Curve[*nistec.P521Point], Q []byte) (*PublicKey, error) {
	panic("crypto/internal/fips140/ecdsa: NewPublicKey: multi-result desugar")
}

// GenerateKey generates a new ECDSA private key pair for the specified curve.
type __mrGenerateKeyPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrGenerateKeyP224(c *Curve[*nistec.P224Point], rand io.Reader) __mrGenerateKeyPair {
	fips140.RecordApproved()

	k, Q, err := randomPointP224(c, func(b []byte) error {
		return drbg.ReadWithReader(rand, b)
	})
	if err != nil {
		return __mrGenerateKeyPair{nil, err}
	}

	priv := &PrivateKey{
		pub: PublicKey{
			curve: c.curve,
			q:     Q.Bytes(),
		},
		d: k.Bytes(c.N),
	}
	fipsPCTP224(c, priv)
	return __mrGenerateKeyPair{priv, nil}
}
func __mrGenerateKeyP256(c *Curve[*nistec.P256Point], rand io.Reader) __mrGenerateKeyPair {
	fips140.RecordApproved()

	k, Q, err := randomPointP256(c, func(b []byte) error {
		return drbg.ReadWithReader(rand, b)
	})
	if err != nil {
		return __mrGenerateKeyPair{nil, err}
	}

	priv := &PrivateKey{
		pub: PublicKey{
			curve: c.curve,
			q:     Q.Bytes(),
		},
		d: k.Bytes(c.N),
	}
	fipsPCTP256(c, priv)
	return __mrGenerateKeyPair{priv, nil}
}
func __mrGenerateKeyP384(c *Curve[*nistec.P384Point], rand io.Reader) __mrGenerateKeyPair {
	fips140.RecordApproved()

	k, Q, err := randomPointP384(c, func(b []byte) error {
		return drbg.ReadWithReader(rand, b)
	})
	if err != nil {
		return __mrGenerateKeyPair{nil, err}
	}

	priv := &PrivateKey{
		pub: PublicKey{
			curve: c.curve,
			q:     Q.Bytes(),
		},
		d: k.Bytes(c.N),
	}
	fipsPCTP384(c, priv)
	return __mrGenerateKeyPair{priv, nil}
}
func __mrGenerateKeyP521(c *Curve[*nistec.P521Point], rand io.Reader) __mrGenerateKeyPair {
	fips140.RecordApproved()

	k, Q, err := randomPointP521(c, func(b []byte) error {
		return drbg.ReadWithReader(rand, b)
	})
	if err != nil {
		return __mrGenerateKeyPair{nil, err}
	}

	priv := &PrivateKey{
		pub: PublicKey{
			curve: c.curve,
			q:     Q.Bytes(),
		},
		d: k.Bytes(c.N),
	}
	fipsPCTP521(c, priv)
	return __mrGenerateKeyPair{priv, nil}
}

func GenerateKeyP224(c *Curve[*nistec.P224Point], rand io.Reader) (*PrivateKey, error) {
	panic("crypto/internal/fips140/ecdsa: GenerateKey: multi-result desugar")
}
func GenerateKeyP256(c *Curve[*nistec.P256Point], rand io.Reader) (*PrivateKey, error) {
	panic("crypto/internal/fips140/ecdsa: GenerateKey: multi-result desugar")
}
func GenerateKeyP384(c *Curve[*nistec.P384Point], rand io.Reader) (*PrivateKey, error) {
	panic("crypto/internal/fips140/ecdsa: GenerateKey: multi-result desugar")
}
func GenerateKeyP521(c *Curve[*nistec.P521Point], rand io.Reader) (*PrivateKey, error) {
	panic("crypto/internal/fips140/ecdsa: GenerateKey: multi-result desugar")
}

// randomPoint returns a random scalar and the corresponding point using a
// procedure equivalent to FIPS 186-5, Appendix A.2.2 (ECDSA Key Pair Generation
// by Rejection Sampling) and to Appendix A.3.2 (Per-Message Secret Number
// Generation of Private Keys by Rejection Sampling) or Appendix A.3.3
// (Per-Message Secret Number Generation for Deterministic ECDSA) followed by
// Step 5 of Section 6.4.1.
type __mrrandomPointPair struct {
	k *bigmod.Nat
	p interface{}
	err error
}

func __mrrandomPointP224(c *Curve[*nistec.P224Point], generate func([]byte) error) __mrrandomPointPair {
	for {
		b := make([]byte, c.N.Size())
		if err := generate(b); err != nil {
			return __mrrandomPointPair{nil, nil, err}
		}

		// Take only the leftmost bits of the generated random value. This is
		// both necessary to increase the chance of the random value being in
		// the correct range and to match the specification. It's unfortunate
		// that we need to do a shift instead of a mask, but see the comment on
		// rightShift.
		//
		// These are the most dangerous lines in the package and maybe in the
		// library: a single bit of bias in the selection of nonces would likely
		// lead to key recovery, but no tests would fail. Look but DO NOT TOUCH.
		if excess := len(b)*8 - c.N.BitLen(); excess > 0 {
			// Just to be safe, assert that this only happens for the one curve that
			// doesn't have a round number of bits.
			if c.curve != p521 {
				panic("ecdsa: internal error: unexpectedly masking off bits")
			}
			b = rightShift(b, excess)
		}

		// FIPS 186-5, Appendix A.4.2 makes us check x <= N - 2 and then return
		// x + 1. Note that it follows that 0 < x + 1 < N. Instead, SetBytes
		// checks that k < N, and we explicitly check 0 != k. Since k can't be
		// negative, this is strictly equivalent. None of this matters anyway
		// because the chance of selecting zero is cryptographically negligible.
		if k, err := bigmod.NewNat().SetBytes(b, c.N); err == nil && k.IsZero() == 0 {
			p, err := c.newPoint().ScalarBaseMult(k.Bytes(c.N))
			return __mrrandomPointPair{k, p, err}
		}

		if testingOnlyRejectionSamplingLooped != nil {
			testingOnlyRejectionSamplingLooped()
		}
	}
}
func __mrrandomPointP256(c *Curve[*nistec.P256Point], generate func([]byte) error) __mrrandomPointPair {
	for {
		b := make([]byte, c.N.Size())
		if err := generate(b); err != nil {
			return __mrrandomPointPair{nil, nil, err}
		}

		// Take only the leftmost bits of the generated random value. This is
		// both necessary to increase the chance of the random value being in
		// the correct range and to match the specification. It's unfortunate
		// that we need to do a shift instead of a mask, but see the comment on
		// rightShift.
		//
		// These are the most dangerous lines in the package and maybe in the
		// library: a single bit of bias in the selection of nonces would likely
		// lead to key recovery, but no tests would fail. Look but DO NOT TOUCH.
		if excess := len(b)*8 - c.N.BitLen(); excess > 0 {
			// Just to be safe, assert that this only happens for the one curve that
			// doesn't have a round number of bits.
			if c.curve != p521 {
				panic("ecdsa: internal error: unexpectedly masking off bits")
			}
			b = rightShift(b, excess)
		}

		// FIPS 186-5, Appendix A.4.2 makes us check x <= N - 2 and then return
		// x + 1. Note that it follows that 0 < x + 1 < N. Instead, SetBytes
		// checks that k < N, and we explicitly check 0 != k. Since k can't be
		// negative, this is strictly equivalent. None of this matters anyway
		// because the chance of selecting zero is cryptographically negligible.
		if k, err := bigmod.NewNat().SetBytes(b, c.N); err == nil && k.IsZero() == 0 {
			p, err := c.newPoint().ScalarBaseMult(k.Bytes(c.N))
			return __mrrandomPointPair{k, p, err}
		}

		if testingOnlyRejectionSamplingLooped != nil {
			testingOnlyRejectionSamplingLooped()
		}
	}
}
func __mrrandomPointP384(c *Curve[*nistec.P384Point], generate func([]byte) error) __mrrandomPointPair {
	for {
		b := make([]byte, c.N.Size())
		if err := generate(b); err != nil {
			return __mrrandomPointPair{nil, nil, err}
		}

		// Take only the leftmost bits of the generated random value. This is
		// both necessary to increase the chance of the random value being in
		// the correct range and to match the specification. It's unfortunate
		// that we need to do a shift instead of a mask, but see the comment on
		// rightShift.
		//
		// These are the most dangerous lines in the package and maybe in the
		// library: a single bit of bias in the selection of nonces would likely
		// lead to key recovery, but no tests would fail. Look but DO NOT TOUCH.
		if excess := len(b)*8 - c.N.BitLen(); excess > 0 {
			// Just to be safe, assert that this only happens for the one curve that
			// doesn't have a round number of bits.
			if c.curve != p521 {
				panic("ecdsa: internal error: unexpectedly masking off bits")
			}
			b = rightShift(b, excess)
		}

		// FIPS 186-5, Appendix A.4.2 makes us check x <= N - 2 and then return
		// x + 1. Note that it follows that 0 < x + 1 < N. Instead, SetBytes
		// checks that k < N, and we explicitly check 0 != k. Since k can't be
		// negative, this is strictly equivalent. None of this matters anyway
		// because the chance of selecting zero is cryptographically negligible.
		if k, err := bigmod.NewNat().SetBytes(b, c.N); err == nil && k.IsZero() == 0 {
			p, err := c.newPoint().ScalarBaseMult(k.Bytes(c.N))
			return __mrrandomPointPair{k, p, err}
		}

		if testingOnlyRejectionSamplingLooped != nil {
			testingOnlyRejectionSamplingLooped()
		}
	}
}
func __mrrandomPointP521(c *Curve[*nistec.P521Point], generate func([]byte) error) __mrrandomPointPair {
	for {
		b := make([]byte, c.N.Size())
		if err := generate(b); err != nil {
			return __mrrandomPointPair{nil, nil, err}
		}

		// Take only the leftmost bits of the generated random value. This is
		// both necessary to increase the chance of the random value being in
		// the correct range and to match the specification. It's unfortunate
		// that we need to do a shift instead of a mask, but see the comment on
		// rightShift.
		//
		// These are the most dangerous lines in the package and maybe in the
		// library: a single bit of bias in the selection of nonces would likely
		// lead to key recovery, but no tests would fail. Look but DO NOT TOUCH.
		if excess := len(b)*8 - c.N.BitLen(); excess > 0 {
			// Just to be safe, assert that this only happens for the one curve that
			// doesn't have a round number of bits.
			if c.curve != p521 {
				panic("ecdsa: internal error: unexpectedly masking off bits")
			}
			b = rightShift(b, excess)
		}

		// FIPS 186-5, Appendix A.4.2 makes us check x <= N - 2 and then return
		// x + 1. Note that it follows that 0 < x + 1 < N. Instead, SetBytes
		// checks that k < N, and we explicitly check 0 != k. Since k can't be
		// negative, this is strictly equivalent. None of this matters anyway
		// because the chance of selecting zero is cryptographically negligible.
		if k, err := bigmod.NewNat().SetBytes(b, c.N); err == nil && k.IsZero() == 0 {
			p, err := c.newPoint().ScalarBaseMult(k.Bytes(c.N))
			return __mrrandomPointPair{k, p, err}
		}

		if testingOnlyRejectionSamplingLooped != nil {
			testingOnlyRejectionSamplingLooped()
		}
	}
}

func randomPointP224(c *Curve[*nistec.P224Point], generate func([]byte) error) (k *bigmod.Nat, p *nistec.P224Point, err error) {
	panic("crypto/internal/fips140/ecdsa: randomPoint: multi-result desugar")
}
func randomPointP256(c *Curve[*nistec.P256Point], generate func([]byte) error) (k *bigmod.Nat, p *nistec.P256Point, err error) {
	panic("crypto/internal/fips140/ecdsa: randomPoint: multi-result desugar")
}
func randomPointP384(c *Curve[*nistec.P384Point], generate func([]byte) error) (k *bigmod.Nat, p *nistec.P384Point, err error) {
	panic("crypto/internal/fips140/ecdsa: randomPoint: multi-result desugar")
}
func randomPointP521(c *Curve[*nistec.P521Point], generate func([]byte) error) (k *bigmod.Nat, p *nistec.P521Point, err error) {
	panic("crypto/internal/fips140/ecdsa: randomPoint: multi-result desugar")
}

// testingOnlyRejectionSamplingLooped is called when rejection sampling in
// randomPoint rejects a candidate for being higher than the modulus.
var testingOnlyRejectionSamplingLooped func()

// Signature is an ECDSA signature, where r and s are represented as big-endian
// byte slices of the same length as the curve order.
type Signature struct {
	R, S []byte
}

// Sign signs a hash (which should be the result of hashing a larger message with
// the hash function H) using the private key, priv. If the hash is longer than
// the bit-length of the private key's curve order, the hash will be truncated
// to that length.
type __mrSignPair struct {
	v0 *Signature
	v1 error
}

func __mrSignP224(c *Curve[*nistec.P224Point], h func() hash.Hash, priv *PrivateKey, rand io.Reader, hash []byte) __mrSignPair {
	if priv.pub.curve != c.curve {
		return __mrSignPair{nil, errors.New("ecdsa: private key does not match curve")}
	}
	if len(hash) == 0 {
		return __mrSignPair{nil, errors.New("ecdsa: hash cannot be empty")}
	}
	fips140.RecordApproved()
	fipsSelfTest()

	// Random ECDSA is dangerous, because a failure of the RNG would immediately
	// leak the private key. Instead, we use a "hedged" approach, as specified
	// in draft-irtf-cfrg-det-sigs-with-noise-04, Section 4. This has also the
	// advantage of closely resembling Deterministic ECDSA.

	Z := make([]byte, len(priv.d))
	if err := drbg.ReadWithReader(rand, Z); err != nil {
		return __mrSignPair{nil, err}
	}

	// See https://github.com/cfrg/draft-irtf-cfrg-det-sigs-with-noise/issues/6
	// for the FIPS compliance of this method. In short Z is entropy from the
	// main DRBG, of length 3/2 of security_strength, so the nonce is optional
	// per SP 800-90Ar1, Section 8.6.7, and the rest is a personalization
	// string, which per SP 800-90Ar1, Section 8.7.1 may contain secret
	// information.
	drbg := newDRBG(h, Z, nil, blockAlignedPersonalizationString{priv.d, bits2octetsP224(c, hash)})

	__fw0, __fw1 := signP224(c, priv, drbg, hash)
	return __mrSignPair{__fw0, __fw1}
}
func __mrSignP256(c *Curve[*nistec.P256Point], h func() hash.Hash, priv *PrivateKey, rand io.Reader, hash []byte) __mrSignPair {
	if priv.pub.curve != c.curve {
		return __mrSignPair{nil, errors.New("ecdsa: private key does not match curve")}
	}
	if len(hash) == 0 {
		return __mrSignPair{nil, errors.New("ecdsa: hash cannot be empty")}
	}
	fips140.RecordApproved()
	fipsSelfTest()

	// Random ECDSA is dangerous, because a failure of the RNG would immediately
	// leak the private key. Instead, we use a "hedged" approach, as specified
	// in draft-irtf-cfrg-det-sigs-with-noise-04, Section 4. This has also the
	// advantage of closely resembling Deterministic ECDSA.

	Z := make([]byte, len(priv.d))
	if err := drbg.ReadWithReader(rand, Z); err != nil {
		return __mrSignPair{nil, err}
	}

	// See https://github.com/cfrg/draft-irtf-cfrg-det-sigs-with-noise/issues/6
	// for the FIPS compliance of this method. In short Z is entropy from the
	// main DRBG, of length 3/2 of security_strength, so the nonce is optional
	// per SP 800-90Ar1, Section 8.6.7, and the rest is a personalization
	// string, which per SP 800-90Ar1, Section 8.7.1 may contain secret
	// information.
	drbg := newDRBG(h, Z, nil, blockAlignedPersonalizationString{priv.d, bits2octetsP256(c, hash)})

	__fw0, __fw1 := signP256(c, priv, drbg, hash)
	return __mrSignPair{__fw0, __fw1}
}
func __mrSignP384(c *Curve[*nistec.P384Point], h func() hash.Hash, priv *PrivateKey, rand io.Reader, hash []byte) __mrSignPair {
	if priv.pub.curve != c.curve {
		return __mrSignPair{nil, errors.New("ecdsa: private key does not match curve")}
	}
	if len(hash) == 0 {
		return __mrSignPair{nil, errors.New("ecdsa: hash cannot be empty")}
	}
	fips140.RecordApproved()
	fipsSelfTest()

	// Random ECDSA is dangerous, because a failure of the RNG would immediately
	// leak the private key. Instead, we use a "hedged" approach, as specified
	// in draft-irtf-cfrg-det-sigs-with-noise-04, Section 4. This has also the
	// advantage of closely resembling Deterministic ECDSA.

	Z := make([]byte, len(priv.d))
	if err := drbg.ReadWithReader(rand, Z); err != nil {
		return __mrSignPair{nil, err}
	}

	// See https://github.com/cfrg/draft-irtf-cfrg-det-sigs-with-noise/issues/6
	// for the FIPS compliance of this method. In short Z is entropy from the
	// main DRBG, of length 3/2 of security_strength, so the nonce is optional
	// per SP 800-90Ar1, Section 8.6.7, and the rest is a personalization
	// string, which per SP 800-90Ar1, Section 8.7.1 may contain secret
	// information.
	drbg := newDRBG(h, Z, nil, blockAlignedPersonalizationString{priv.d, bits2octetsP384(c, hash)})

	__fw0, __fw1 := signP384(c, priv, drbg, hash)
	return __mrSignPair{__fw0, __fw1}
}
func __mrSignP521(c *Curve[*nistec.P521Point], h func() hash.Hash, priv *PrivateKey, rand io.Reader, hash []byte) __mrSignPair {
	if priv.pub.curve != c.curve {
		return __mrSignPair{nil, errors.New("ecdsa: private key does not match curve")}
	}
	if len(hash) == 0 {
		return __mrSignPair{nil, errors.New("ecdsa: hash cannot be empty")}
	}
	fips140.RecordApproved()
	fipsSelfTest()

	// Random ECDSA is dangerous, because a failure of the RNG would immediately
	// leak the private key. Instead, we use a "hedged" approach, as specified
	// in draft-irtf-cfrg-det-sigs-with-noise-04, Section 4. This has also the
	// advantage of closely resembling Deterministic ECDSA.

	Z := make([]byte, len(priv.d))
	if err := drbg.ReadWithReader(rand, Z); err != nil {
		return __mrSignPair{nil, err}
	}

	// See https://github.com/cfrg/draft-irtf-cfrg-det-sigs-with-noise/issues/6
	// for the FIPS compliance of this method. In short Z is entropy from the
	// main DRBG, of length 3/2 of security_strength, so the nonce is optional
	// per SP 800-90Ar1, Section 8.6.7, and the rest is a personalization
	// string, which per SP 800-90Ar1, Section 8.7.1 may contain secret
	// information.
	drbg := newDRBG(h, Z, nil, blockAlignedPersonalizationString{priv.d, bits2octetsP521(c, hash)})

	__fw0, __fw1 := signP521(c, priv, drbg, hash)
	return __mrSignPair{__fw0, __fw1}
}

func SignP224(c *Curve[*nistec.P224Point], h func() hash.Hash, priv *PrivateKey, rand io.Reader, hash []byte) (*Signature, error) {
	panic("crypto/internal/fips140/ecdsa: Sign: multi-result desugar")
}
func SignP256(c *Curve[*nistec.P256Point], h func() hash.Hash, priv *PrivateKey, rand io.Reader, hash []byte) (*Signature, error) {
	panic("crypto/internal/fips140/ecdsa: Sign: multi-result desugar")
}
func SignP384(c *Curve[*nistec.P384Point], h func() hash.Hash, priv *PrivateKey, rand io.Reader, hash []byte) (*Signature, error) {
	panic("crypto/internal/fips140/ecdsa: Sign: multi-result desugar")
}
func SignP521(c *Curve[*nistec.P521Point], h func() hash.Hash, priv *PrivateKey, rand io.Reader, hash []byte) (*Signature, error) {
	panic("crypto/internal/fips140/ecdsa: Sign: multi-result desugar")
}

// SignDeterministic signs a hash (which shall be the result of hashing a
// larger message with the hash function H) using the private key, priv. If the
// hash is longer than the bit-length of the private key's curve order, the hash
// will be truncated to that length. This applies Deterministic ECDSA as
// specified in FIPS 186-5 and RFC 6979.
type __mrSignDeterministicPair struct {
	v0 *Signature
	v1 error
}

func __mrSignDeterministicP224(c *Curve[*nistec.P224Point], h func() hash.Hash, priv *PrivateKey, hash []byte) __mrSignDeterministicPair {
	if priv.pub.curve != c.curve {
		return __mrSignDeterministicPair{nil, errors.New("ecdsa: private key does not match curve")}
	}
	if len(hash) == 0 {
		return __mrSignDeterministicPair{nil, errors.New("ecdsa: hash cannot be empty")}
	}
	fips140.RecordApproved()
	fipsSelfTestDeterministic()
	drbg := newDRBG(h, priv.d, bits2octetsP224(c, hash), nil) // RFC 6979, Section 3.3
	__fw0, __fw1 := signP224(c, priv, drbg, hash)
	return __mrSignDeterministicPair{__fw0, __fw1}
}
func __mrSignDeterministicP256(c *Curve[*nistec.P256Point], h func() hash.Hash, priv *PrivateKey, hash []byte) __mrSignDeterministicPair {
	if priv.pub.curve != c.curve {
		return __mrSignDeterministicPair{nil, errors.New("ecdsa: private key does not match curve")}
	}
	if len(hash) == 0 {
		return __mrSignDeterministicPair{nil, errors.New("ecdsa: hash cannot be empty")}
	}
	fips140.RecordApproved()
	fipsSelfTestDeterministic()
	drbg := newDRBG(h, priv.d, bits2octetsP256(c, hash), nil) // RFC 6979, Section 3.3
	__fw0, __fw1 := signP256(c, priv, drbg, hash)
	return __mrSignDeterministicPair{__fw0, __fw1}
}
func __mrSignDeterministicP384(c *Curve[*nistec.P384Point], h func() hash.Hash, priv *PrivateKey, hash []byte) __mrSignDeterministicPair {
	if priv.pub.curve != c.curve {
		return __mrSignDeterministicPair{nil, errors.New("ecdsa: private key does not match curve")}
	}
	if len(hash) == 0 {
		return __mrSignDeterministicPair{nil, errors.New("ecdsa: hash cannot be empty")}
	}
	fips140.RecordApproved()
	fipsSelfTestDeterministic()
	drbg := newDRBG(h, priv.d, bits2octetsP384(c, hash), nil) // RFC 6979, Section 3.3
	__fw0, __fw1 := signP384(c, priv, drbg, hash)
	return __mrSignDeterministicPair{__fw0, __fw1}
}
func __mrSignDeterministicP521(c *Curve[*nistec.P521Point], h func() hash.Hash, priv *PrivateKey, hash []byte) __mrSignDeterministicPair {
	if priv.pub.curve != c.curve {
		return __mrSignDeterministicPair{nil, errors.New("ecdsa: private key does not match curve")}
	}
	if len(hash) == 0 {
		return __mrSignDeterministicPair{nil, errors.New("ecdsa: hash cannot be empty")}
	}
	fips140.RecordApproved()
	fipsSelfTestDeterministic()
	drbg := newDRBG(h, priv.d, bits2octetsP521(c, hash), nil) // RFC 6979, Section 3.3
	__fw0, __fw1 := signP521(c, priv, drbg, hash)
	return __mrSignDeterministicPair{__fw0, __fw1}
}

func SignDeterministicP224(c *Curve[*nistec.P224Point], h func() hash.Hash, priv *PrivateKey, hash []byte) (*Signature, error) {
	panic("crypto/internal/fips140/ecdsa: SignDeterministic: multi-result desugar")
}
func SignDeterministicP256(c *Curve[*nistec.P256Point], h func() hash.Hash, priv *PrivateKey, hash []byte) (*Signature, error) {
	panic("crypto/internal/fips140/ecdsa: SignDeterministic: multi-result desugar")
}
func SignDeterministicP384(c *Curve[*nistec.P384Point], h func() hash.Hash, priv *PrivateKey, hash []byte) (*Signature, error) {
	panic("crypto/internal/fips140/ecdsa: SignDeterministic: multi-result desugar")
}
func SignDeterministicP521(c *Curve[*nistec.P521Point], h func() hash.Hash, priv *PrivateKey, hash []byte) (*Signature, error) {
	panic("crypto/internal/fips140/ecdsa: SignDeterministic: multi-result desugar")
}

// bits2octets as specified in FIPS 186-5, Appendix B.2.4 or RFC 6979,
// Section 2.3.4. See RFC 6979, Section 3.5 for the rationale.
func bits2octetsP224(c *Curve[*nistec.P224Point], hash []byte) []byte {
	e := bigmod.NewNat()
	hashToNatP224(c, e, hash)
	return e.Bytes(c.N)
}
func bits2octetsP256(c *Curve[*nistec.P256Point], hash []byte) []byte {
	e := bigmod.NewNat()
	hashToNatP256(c, e, hash)
	return e.Bytes(c.N)
}
func bits2octetsP384(c *Curve[*nistec.P384Point], hash []byte) []byte {
	e := bigmod.NewNat()
	hashToNatP384(c, e, hash)
	return e.Bytes(c.N)
}
func bits2octetsP521(c *Curve[*nistec.P521Point], hash []byte) []byte {
	e := bigmod.NewNat()
	hashToNatP521(c, e, hash)
	return e.Bytes(c.N)
}

type __mrsignGenericPair struct {
	v0 *Signature
	v1 error
}

func __mrsignGenericP224(c *Curve[*nistec.P224Point], priv *PrivateKey, drbg *hmacDRBG, hash []byte) __mrsignGenericPair {
	// FIPS 186-5, Section 6.4.1

	k, R, err := randomPointP224(c, func(b []byte) error {
		drbg.Generate(b)
		return nil
	})
	if err != nil {
		return __mrsignGenericPair{nil, err}
	}

	// kInv = k⁻¹
	kInv := bigmod.NewNat()
	inverseP224(c, kInv, k)

	Rx, err := R.BytesX()
	if err != nil {
		return __mrsignGenericPair{nil, err}
	}
	r, err := bigmod.NewNat().SetOverflowingBytes(Rx, c.N)
	if err != nil {
		return __mrsignGenericPair{nil, err}
	}

	// The spec wants us to retry here, but the chance of hitting this condition
	// on a large prime-order group like the NIST curves we support is
	// cryptographically negligible. If we hit it, something is awfully wrong.
	if r.IsZero() == 1 {
		return __mrsignGenericPair{nil, errors.New("ecdsa: internal error: r is zero")}
	}

	e := bigmod.NewNat()
	hashToNatP224(c, e, hash)

	s, err := bigmod.NewNat().SetBytes(priv.d, c.N)
	if err != nil {
		return __mrsignGenericPair{nil, err}
	}
	s.Mul(r, c.N)
	s.Add(e, c.N)
	s.Mul(kInv, c.N)

	// Again, the chance of this happening is cryptographically negligible.
	if s.IsZero() == 1 {
		return __mrsignGenericPair{nil, errors.New("ecdsa: internal error: s is zero")}
	}

	return __mrsignGenericPair{&Signature{r.Bytes(c.N), s.Bytes(c.N)}, nil}
}
func __mrsignGenericP256(c *Curve[*nistec.P256Point], priv *PrivateKey, drbg *hmacDRBG, hash []byte) __mrsignGenericPair {
	// FIPS 186-5, Section 6.4.1

	k, R, err := randomPointP256(c, func(b []byte) error {
		drbg.Generate(b)
		return nil
	})
	if err != nil {
		return __mrsignGenericPair{nil, err}
	}

	// kInv = k⁻¹
	kInv := bigmod.NewNat()
	inverseP256(c, kInv, k)

	Rx, err := R.BytesX()
	if err != nil {
		return __mrsignGenericPair{nil, err}
	}
	r, err := bigmod.NewNat().SetOverflowingBytes(Rx, c.N)
	if err != nil {
		return __mrsignGenericPair{nil, err}
	}

	// The spec wants us to retry here, but the chance of hitting this condition
	// on a large prime-order group like the NIST curves we support is
	// cryptographically negligible. If we hit it, something is awfully wrong.
	if r.IsZero() == 1 {
		return __mrsignGenericPair{nil, errors.New("ecdsa: internal error: r is zero")}
	}

	e := bigmod.NewNat()
	hashToNatP256(c, e, hash)

	s, err := bigmod.NewNat().SetBytes(priv.d, c.N)
	if err != nil {
		return __mrsignGenericPair{nil, err}
	}
	s.Mul(r, c.N)
	s.Add(e, c.N)
	s.Mul(kInv, c.N)

	// Again, the chance of this happening is cryptographically negligible.
	if s.IsZero() == 1 {
		return __mrsignGenericPair{nil, errors.New("ecdsa: internal error: s is zero")}
	}

	return __mrsignGenericPair{&Signature{r.Bytes(c.N), s.Bytes(c.N)}, nil}
}
func __mrsignGenericP384(c *Curve[*nistec.P384Point], priv *PrivateKey, drbg *hmacDRBG, hash []byte) __mrsignGenericPair {
	// FIPS 186-5, Section 6.4.1

	k, R, err := randomPointP384(c, func(b []byte) error {
		drbg.Generate(b)
		return nil
	})
	if err != nil {
		return __mrsignGenericPair{nil, err}
	}

	// kInv = k⁻¹
	kInv := bigmod.NewNat()
	inverseP384(c, kInv, k)

	Rx, err := R.BytesX()
	if err != nil {
		return __mrsignGenericPair{nil, err}
	}
	r, err := bigmod.NewNat().SetOverflowingBytes(Rx, c.N)
	if err != nil {
		return __mrsignGenericPair{nil, err}
	}

	// The spec wants us to retry here, but the chance of hitting this condition
	// on a large prime-order group like the NIST curves we support is
	// cryptographically negligible. If we hit it, something is awfully wrong.
	if r.IsZero() == 1 {
		return __mrsignGenericPair{nil, errors.New("ecdsa: internal error: r is zero")}
	}

	e := bigmod.NewNat()
	hashToNatP384(c, e, hash)

	s, err := bigmod.NewNat().SetBytes(priv.d, c.N)
	if err != nil {
		return __mrsignGenericPair{nil, err}
	}
	s.Mul(r, c.N)
	s.Add(e, c.N)
	s.Mul(kInv, c.N)

	// Again, the chance of this happening is cryptographically negligible.
	if s.IsZero() == 1 {
		return __mrsignGenericPair{nil, errors.New("ecdsa: internal error: s is zero")}
	}

	return __mrsignGenericPair{&Signature{r.Bytes(c.N), s.Bytes(c.N)}, nil}
}
func __mrsignGenericP521(c *Curve[*nistec.P521Point], priv *PrivateKey, drbg *hmacDRBG, hash []byte) __mrsignGenericPair {
	// FIPS 186-5, Section 6.4.1

	k, R, err := randomPointP521(c, func(b []byte) error {
		drbg.Generate(b)
		return nil
	})
	if err != nil {
		return __mrsignGenericPair{nil, err}
	}

	// kInv = k⁻¹
	kInv := bigmod.NewNat()
	inverseP521(c, kInv, k)

	Rx, err := R.BytesX()
	if err != nil {
		return __mrsignGenericPair{nil, err}
	}
	r, err := bigmod.NewNat().SetOverflowingBytes(Rx, c.N)
	if err != nil {
		return __mrsignGenericPair{nil, err}
	}

	// The spec wants us to retry here, but the chance of hitting this condition
	// on a large prime-order group like the NIST curves we support is
	// cryptographically negligible. If we hit it, something is awfully wrong.
	if r.IsZero() == 1 {
		return __mrsignGenericPair{nil, errors.New("ecdsa: internal error: r is zero")}
	}

	e := bigmod.NewNat()
	hashToNatP521(c, e, hash)

	s, err := bigmod.NewNat().SetBytes(priv.d, c.N)
	if err != nil {
		return __mrsignGenericPair{nil, err}
	}
	s.Mul(r, c.N)
	s.Add(e, c.N)
	s.Mul(kInv, c.N)

	// Again, the chance of this happening is cryptographically negligible.
	if s.IsZero() == 1 {
		return __mrsignGenericPair{nil, errors.New("ecdsa: internal error: s is zero")}
	}

	return __mrsignGenericPair{&Signature{r.Bytes(c.N), s.Bytes(c.N)}, nil}
}

func signGenericP224(c *Curve[*nistec.P224Point], priv *PrivateKey, drbg *hmacDRBG, hash []byte) (*Signature, error) {
	panic("crypto/internal/fips140/ecdsa: signGeneric: multi-result desugar")
}
func signGenericP256(c *Curve[*nistec.P256Point], priv *PrivateKey, drbg *hmacDRBG, hash []byte) (*Signature, error) {
	panic("crypto/internal/fips140/ecdsa: signGeneric: multi-result desugar")
}
func signGenericP384(c *Curve[*nistec.P384Point], priv *PrivateKey, drbg *hmacDRBG, hash []byte) (*Signature, error) {
	panic("crypto/internal/fips140/ecdsa: signGeneric: multi-result desugar")
}
func signGenericP521(c *Curve[*nistec.P521Point], priv *PrivateKey, drbg *hmacDRBG, hash []byte) (*Signature, error) {
	panic("crypto/internal/fips140/ecdsa: signGeneric: multi-result desugar")
}

// inverse sets kInv to the inverse of k modulo the order of the curve.
func inverseP224(c *Curve[*nistec.P224Point], kInv, k *bigmod.Nat) {
	if c.ordInverse != nil && bits.UintSize == 64 {
		if kb := k.Bits(); len(kb) == 4 {
			k64 := [4]uint64{uint64(kb[0]), uint64(kb[1]), uint64(kb[2]), uint64(kb[3])}
			c.ordInverse(&k64)
			kInv.SetBits([]uint{uint(k64[0]), uint(k64[1]), uint(k64[2]), uint(k64[3])})
			return
		}
	}

	// Calculate the inverse of s in GF(N) using Fermat's method
	// (exponentiation modulo *nistec.P224Point - 2, per Euler's theorem)
	kInv.Exp(k, c.nMinus2, c.N)
}
func inverseP256(c *Curve[*nistec.P256Point], kInv, k *bigmod.Nat) {
	if c.ordInverse != nil && bits.UintSize == 64 {
		if kb := k.Bits(); len(kb) == 4 {
			k64 := [4]uint64{uint64(kb[0]), uint64(kb[1]), uint64(kb[2]), uint64(kb[3])}
			c.ordInverse(&k64)
			kInv.SetBits([]uint{uint(k64[0]), uint(k64[1]), uint(k64[2]), uint(k64[3])})
			return
		}
	}

	// Calculate the inverse of s in GF(N) using Fermat's method
	// (exponentiation modulo *nistec.P256Point - 2, per Euler's theorem)
	kInv.Exp(k, c.nMinus2, c.N)
}
func inverseP384(c *Curve[*nistec.P384Point], kInv, k *bigmod.Nat) {
	if c.ordInverse != nil && bits.UintSize == 64 {
		if kb := k.Bits(); len(kb) == 4 {
			k64 := [4]uint64{uint64(kb[0]), uint64(kb[1]), uint64(kb[2]), uint64(kb[3])}
			c.ordInverse(&k64)
			kInv.SetBits([]uint{uint(k64[0]), uint(k64[1]), uint(k64[2]), uint(k64[3])})
			return
		}
	}

	// Calculate the inverse of s in GF(N) using Fermat's method
	// (exponentiation modulo *nistec.P384Point - 2, per Euler's theorem)
	kInv.Exp(k, c.nMinus2, c.N)
}
func inverseP521(c *Curve[*nistec.P521Point], kInv, k *bigmod.Nat) {
	if c.ordInverse != nil && bits.UintSize == 64 {
		if kb := k.Bits(); len(kb) == 4 {
			k64 := [4]uint64{uint64(kb[0]), uint64(kb[1]), uint64(kb[2]), uint64(kb[3])}
			c.ordInverse(&k64)
			kInv.SetBits([]uint{uint(k64[0]), uint(k64[1]), uint(k64[2]), uint(k64[3])})
			return
		}
	}

	// Calculate the inverse of s in GF(N) using Fermat's method
	// (exponentiation modulo *nistec.P521Point - 2, per Euler's theorem)
	kInv.Exp(k, c.nMinus2, c.N)
}

// hashToNat sets e to the left-most bits of hash, according to
// FIPS 186-5, Section 6.4.1, point 2 and Section 6.4.2, point 3.
func hashToNatP224(c *Curve[*nistec.P224Point], e *bigmod.Nat, hash []byte) {
	// ECDSA asks us to take the left-most log2(N) bits of hash, and use them as
	// an integer modulo N. This is the absolute worst of all worlds: we still
	// have to reduce, because the result might still overflow N, but to take
	// the left-most bits for *nistec.P224Point-521 we have to do a right shift.
	if size := c.N.Size(); len(hash) >= size {
		hash = hash[:size]
		if excess := len(hash)*8 - c.N.BitLen(); excess > 0 {
			hash = rightShift(hash, excess)
		}
	}
	_, err := e.SetOverflowingBytes(hash, c.N)
	if err != nil {
		panic("ecdsa: internal error: truncated hash is too long")
	}
}
func hashToNatP256(c *Curve[*nistec.P256Point], e *bigmod.Nat, hash []byte) {
	// ECDSA asks us to take the left-most log2(N) bits of hash, and use them as
	// an integer modulo N. This is the absolute worst of all worlds: we still
	// have to reduce, because the result might still overflow N, but to take
	// the left-most bits for *nistec.P256Point-521 we have to do a right shift.
	if size := c.N.Size(); len(hash) >= size {
		hash = hash[:size]
		if excess := len(hash)*8 - c.N.BitLen(); excess > 0 {
			hash = rightShift(hash, excess)
		}
	}
	_, err := e.SetOverflowingBytes(hash, c.N)
	if err != nil {
		panic("ecdsa: internal error: truncated hash is too long")
	}
}
func hashToNatP384(c *Curve[*nistec.P384Point], e *bigmod.Nat, hash []byte) {
	// ECDSA asks us to take the left-most log2(N) bits of hash, and use them as
	// an integer modulo N. This is the absolute worst of all worlds: we still
	// have to reduce, because the result might still overflow N, but to take
	// the left-most bits for *nistec.P384Point-521 we have to do a right shift.
	if size := c.N.Size(); len(hash) >= size {
		hash = hash[:size]
		if excess := len(hash)*8 - c.N.BitLen(); excess > 0 {
			hash = rightShift(hash, excess)
		}
	}
	_, err := e.SetOverflowingBytes(hash, c.N)
	if err != nil {
		panic("ecdsa: internal error: truncated hash is too long")
	}
}
func hashToNatP521(c *Curve[*nistec.P521Point], e *bigmod.Nat, hash []byte) {
	// ECDSA asks us to take the left-most log2(N) bits of hash, and use them as
	// an integer modulo N. This is the absolute worst of all worlds: we still
	// have to reduce, because the result might still overflow N, but to take
	// the left-most bits for *nistec.P521Point-521 we have to do a right shift.
	if size := c.N.Size(); len(hash) >= size {
		hash = hash[:size]
		if excess := len(hash)*8 - c.N.BitLen(); excess > 0 {
			hash = rightShift(hash, excess)
		}
	}
	_, err := e.SetOverflowingBytes(hash, c.N)
	if err != nil {
		panic("ecdsa: internal error: truncated hash is too long")
	}
}

// rightShift implements the right shift necessary for bits2int, which takes the
// leftmost bits of either the hash or HMAC_DRBG output.
//
// Note how taking the rightmost bits would have been as easy as masking the
// first byte, but we can't have nice things.
func rightShift(b []byte, shift int) []byte {
	if shift <= 0 || shift >= 8 {
		panic("ecdsa: internal error: shift can only be by 1 to 7 bits")
	}
	b = bytes.Clone(b)
	for i := len(b) - 1; i >= 0; i-- {
		b[i] >>= shift
		if i > 0 {
			b[i] |= b[i-1] << (8 - shift)
		}
	}
	return b
}

// Verify verifies the signature, sig, of hash (which should be the result of
// hashing a larger message) using the public key, pub. If the hash is longer
// than the bit-length of the private key's curve order, the hash will be
// truncated to that length.
//
// The inputs are not considered confidential, and may leak through timing side
// channels, or if an attacker has control of part of the inputs.
func VerifyP224(c *Curve[*nistec.P224Point], pub *PublicKey, hash []byte, sig *Signature) error {
	if pub.curve != c.curve {
		return errors.New("ecdsa: public key does not match curve")
	}
	if len(hash) == 0 {
		return errors.New("ecdsa: hash cannot be empty")
	}
	fips140.RecordApproved()
	fipsSelfTest()
	return verifyP224(c, pub, hash, sig)
}
func VerifyP256(c *Curve[*nistec.P256Point], pub *PublicKey, hash []byte, sig *Signature) error {
	if pub.curve != c.curve {
		return errors.New("ecdsa: public key does not match curve")
	}
	if len(hash) == 0 {
		return errors.New("ecdsa: hash cannot be empty")
	}
	fips140.RecordApproved()
	fipsSelfTest()
	return verifyP256(c, pub, hash, sig)
}
func VerifyP384(c *Curve[*nistec.P384Point], pub *PublicKey, hash []byte, sig *Signature) error {
	if pub.curve != c.curve {
		return errors.New("ecdsa: public key does not match curve")
	}
	if len(hash) == 0 {
		return errors.New("ecdsa: hash cannot be empty")
	}
	fips140.RecordApproved()
	fipsSelfTest()
	return verifyP384(c, pub, hash, sig)
}
func VerifyP521(c *Curve[*nistec.P521Point], pub *PublicKey, hash []byte, sig *Signature) error {
	if pub.curve != c.curve {
		return errors.New("ecdsa: public key does not match curve")
	}
	if len(hash) == 0 {
		return errors.New("ecdsa: hash cannot be empty")
	}
	fips140.RecordApproved()
	fipsSelfTest()
	return verifyP521(c, pub, hash, sig)
}

func verifyGenericP224(c *Curve[*nistec.P224Point], pub *PublicKey, hash []byte, sig *Signature) error {
	// FIPS 186-5, Section 6.4.2

	Q, err := c.newPoint().SetBytes(pub.q)
	if err != nil {
		return err
	}

	r, err := bigmod.NewNat().SetBytes(sig.R, c.N)
	if err != nil {
		return err
	}
	if r.IsZero() == 1 {
		return errors.New("ecdsa: invalid signature: r is zero")
	}
	s, err := bigmod.NewNat().SetBytes(sig.S, c.N)
	if err != nil {
		return err
	}
	if s.IsZero() == 1 {
		return errors.New("ecdsa: invalid signature: s is zero")
	}

	e := bigmod.NewNat()
	hashToNatP224(c, e, hash)

	// w = s⁻¹
	w := bigmod.NewNat()
	inverseP224(c, w, s)

	// p₁ = [e * s⁻¹]G
	p1, err := c.newPoint().ScalarBaseMult(e.Mul(w, c.N).Bytes(c.N))
	if err != nil {
		return err
	}
	// p₂ = [r * s⁻¹]Q
	p2, err := Q.ScalarMult(Q, w.Mul(r, c.N).Bytes(c.N))
	if err != nil {
		return err
	}
	// BytesX returns an error for the point at infinity.
	Rx, err := p1.Add(p1, p2).BytesX()
	if err != nil {
		return err
	}

	v, err := bigmod.NewNat().SetOverflowingBytes(Rx, c.N)
	if err != nil {
		return err
	}

	if v.Equal(r) != 1 {
		return errors.New("ecdsa: signature did not verify")
	}
	return nil
}
func verifyGenericP256(c *Curve[*nistec.P256Point], pub *PublicKey, hash []byte, sig *Signature) error {
	// FIPS 186-5, Section 6.4.2

	Q, err := c.newPoint().SetBytes(pub.q)
	if err != nil {
		return err
	}

	r, err := bigmod.NewNat().SetBytes(sig.R, c.N)
	if err != nil {
		return err
	}
	if r.IsZero() == 1 {
		return errors.New("ecdsa: invalid signature: r is zero")
	}
	s, err := bigmod.NewNat().SetBytes(sig.S, c.N)
	if err != nil {
		return err
	}
	if s.IsZero() == 1 {
		return errors.New("ecdsa: invalid signature: s is zero")
	}

	e := bigmod.NewNat()
	hashToNatP256(c, e, hash)

	// w = s⁻¹
	w := bigmod.NewNat()
	inverseP256(c, w, s)

	// p₁ = [e * s⁻¹]G
	p1, err := c.newPoint().ScalarBaseMult(e.Mul(w, c.N).Bytes(c.N))
	if err != nil {
		return err
	}
	// p₂ = [r * s⁻¹]Q
	p2, err := Q.ScalarMult(Q, w.Mul(r, c.N).Bytes(c.N))
	if err != nil {
		return err
	}
	// BytesX returns an error for the point at infinity.
	Rx, err := p1.Add(p1, p2).BytesX()
	if err != nil {
		return err
	}

	v, err := bigmod.NewNat().SetOverflowingBytes(Rx, c.N)
	if err != nil {
		return err
	}

	if v.Equal(r) != 1 {
		return errors.New("ecdsa: signature did not verify")
	}
	return nil
}
func verifyGenericP384(c *Curve[*nistec.P384Point], pub *PublicKey, hash []byte, sig *Signature) error {
	// FIPS 186-5, Section 6.4.2

	Q, err := c.newPoint().SetBytes(pub.q)
	if err != nil {
		return err
	}

	r, err := bigmod.NewNat().SetBytes(sig.R, c.N)
	if err != nil {
		return err
	}
	if r.IsZero() == 1 {
		return errors.New("ecdsa: invalid signature: r is zero")
	}
	s, err := bigmod.NewNat().SetBytes(sig.S, c.N)
	if err != nil {
		return err
	}
	if s.IsZero() == 1 {
		return errors.New("ecdsa: invalid signature: s is zero")
	}

	e := bigmod.NewNat()
	hashToNatP384(c, e, hash)

	// w = s⁻¹
	w := bigmod.NewNat()
	inverseP384(c, w, s)

	// p₁ = [e * s⁻¹]G
	p1, err := c.newPoint().ScalarBaseMult(e.Mul(w, c.N).Bytes(c.N))
	if err != nil {
		return err
	}
	// p₂ = [r * s⁻¹]Q
	p2, err := Q.ScalarMult(Q, w.Mul(r, c.N).Bytes(c.N))
	if err != nil {
		return err
	}
	// BytesX returns an error for the point at infinity.
	Rx, err := p1.Add(p1, p2).BytesX()
	if err != nil {
		return err
	}

	v, err := bigmod.NewNat().SetOverflowingBytes(Rx, c.N)
	if err != nil {
		return err
	}

	if v.Equal(r) != 1 {
		return errors.New("ecdsa: signature did not verify")
	}
	return nil
}
func verifyGenericP521(c *Curve[*nistec.P521Point], pub *PublicKey, hash []byte, sig *Signature) error {
	// FIPS 186-5, Section 6.4.2

	Q, err := c.newPoint().SetBytes(pub.q)
	if err != nil {
		return err
	}

	r, err := bigmod.NewNat().SetBytes(sig.R, c.N)
	if err != nil {
		return err
	}
	if r.IsZero() == 1 {
		return errors.New("ecdsa: invalid signature: r is zero")
	}
	s, err := bigmod.NewNat().SetBytes(sig.S, c.N)
	if err != nil {
		return err
	}
	if s.IsZero() == 1 {
		return errors.New("ecdsa: invalid signature: s is zero")
	}

	e := bigmod.NewNat()
	hashToNatP521(c, e, hash)

	// w = s⁻¹
	w := bigmod.NewNat()
	inverseP521(c, w, s)

	// p₁ = [e * s⁻¹]G
	p1, err := c.newPoint().ScalarBaseMult(e.Mul(w, c.N).Bytes(c.N))
	if err != nil {
		return err
	}
	// p₂ = [r * s⁻¹]Q
	p2, err := Q.ScalarMult(Q, w.Mul(r, c.N).Bytes(c.N))
	if err != nil {
		return err
	}
	// BytesX returns an error for the point at infinity.
	Rx, err := p1.Add(p1, p2).BytesX()
	if err != nil {
		return err
	}

	v, err := bigmod.NewNat().SetOverflowingBytes(Rx, c.N)
	if err != nil {
		return err
	}

	if v.Equal(r) != 1 {
		return errors.New("ecdsa: signature did not verify")
	}
	return nil
}


// ---- vendored from crypto/internal/fips140/ecdsa/hmacdrbg.go ----




// hmacDRBG is an SP 800-90A Rev. 1 HMAC_DRBG.
//
// It is only intended to be used to generate ECDSA nonces. Since it will be
// instantiated ex-novo for each signature, its Generate function will only be
// invoked once or twice (only for P-256, with probability 2⁻³²).
//
// Per Table 2, it has a reseed interval of 2^48 requests, and a maximum request
// size of 2^19 bits (2^16 bytes, 64 KiB).
type hmacDRBG struct {
	newHMAC func(key []byte) *hmac.HMAC

	hK *hmac.HMAC
	V  []byte

	reseedCounter uint64
}

const (
	reseedInterval = 1 << 48
	maxRequestSize = (1 << 19) / 8
)

// plainPersonalizationString is used by HMAC_DRBG as-is.
type plainPersonalizationString []byte

func (plainPersonalizationString) isPersonalizationString() {}

// Each entry in blockAlignedPersonalizationString is written to the HMAC at a
// block boundary, as specified in draft-irtf-cfrg-det-sigs-with-noise-04,
// Section 4.
type blockAlignedPersonalizationString [][]byte

func (blockAlignedPersonalizationString) isPersonalizationString() {}

type personalizationString interface {
	isPersonalizationString()
}

func newDRBG(hash func() hash.Hash, entropy, nonce []byte, s personalizationString) *hmacDRBG {
	// HMAC_DRBG_Instantiate_algorithm, per Section 10.1.2.3.
	fips140.RecordApproved()

	d := &hmacDRBG{
		newHMAC: func(key []byte) *hmac.HMAC {
			return hmac.New(hash, key)
		},
	}
	size := hash().Size()

	// K = 0x00 0x00 0x00 ... 0x00
	K := make([]byte, size)

	// V = 0x01 0x01 0x01 ... 0x01
	d.V = bytes.Repeat([]byte{0x01}, size)

	// HMAC_DRBG_Update, per Section 10.1.2.2.
	// K = HMAC (K, V || 0x00 || provided_data)
	h := hmac.New(hash, K)
	h.Write(d.V)
	h.Write([]byte{0x00})
	h.Write(entropy)
	h.Write(nonce)
	switch s := s.(type) {
	case plainPersonalizationString:
		h.Write(s)
	case blockAlignedPersonalizationString:
		l := len(d.V) + 1 + len(entropy) + len(nonce)
		for _, b := range s {
			pad000(h, l)
			h.Write(b)
			l = len(b)
		}
	}
	K = h.Sum(K[:0])
	// V = HMAC (K, V)
	h = hmac.New(hash, K)
	h.Write(d.V)
	d.V = h.Sum(d.V[:0])
	// K = HMAC (K, V || 0x01 || provided_data).
	h.Reset()
	h.Write(d.V)
	h.Write([]byte{0x01})
	h.Write(entropy)
	h.Write(nonce)
	switch s := s.(type) {
	case plainPersonalizationString:
		h.Write(s)
	case blockAlignedPersonalizationString:
		l := len(d.V) + 1 + len(entropy) + len(nonce)
		for _, b := range s {
			pad000(h, l)
			h.Write(b)
			l = len(b)
		}
	}
	K = h.Sum(K[:0])
	// V = HMAC (K, V)
	h = hmac.New(hash, K)
	h.Write(d.V)
	d.V = h.Sum(d.V[:0])

	d.hK = h
	d.reseedCounter = 1
	return d
}

// TestingOnlyNewDRBG creates an SP 800-90A Rev. 1 HMAC_DRBG with a plain
// personalization string.
//
// This should only be used for ACVP testing. hmacDRBG is not intended to be
// used directly.
func TestingOnlyNewDRBG(hash func() hash.Hash, entropy, nonce []byte, s []byte) *hmacDRBG {
	return newDRBG(hash, entropy, nonce, plainPersonalizationString(s))
}

func pad000(h *hmac.HMAC, writtenSoFar int) {
	blockSize := h.BlockSize()
	if rem := writtenSoFar % blockSize; rem != 0 {
		h.Write(make([]byte, blockSize-rem))
	}
}

// Generate produces at most maxRequestSize bytes of random data in out.
func (d *hmacDRBG) Generate(out []byte) {
	// HMAC_DRBG_Generate_algorithm, per Section 10.1.2.5.
	fips140.RecordApproved()

	if len(out) > maxRequestSize {
		panic("ecdsa: internal error: request size exceeds maximum")
	}

	if d.reseedCounter > reseedInterval {
		panic("ecdsa: reseed interval exceeded")
	}

	tlen := 0
	for tlen < len(out) {
		// V = HMAC_K(V)
		// T = T || V
		d.hK.Reset()
		d.hK.Write(d.V)
		d.V = d.hK.Sum(d.V[:0])
		tlen += copy(out[tlen:], d.V)
	}

	// Note that if this function shows up on ECDSA-level profiles, this can be
	// optimized in the common case by deferring the rest to the next Generate
	// call, which will never come in nearly all cases.

	// HMAC_DRBG_Update, per Section 10.1.2.2, without provided_data.
	// K = HMAC (K, V || 0x00)
	d.hK.Reset()
	d.hK.Write(d.V)
	d.hK.Write([]byte{0x00})
	K := d.hK.Sum(nil)
	// V = HMAC (K, V)
	d.hK = d.newHMAC(K)
	d.hK.Write(d.V)
	d.V = d.hK.Sum(d.V[:0])

	d.reseedCounter++
}


// ---- vendored from crypto/internal/fips140/ecdsa/ecdsa_noasm.go ----


type __mrsignPair struct {
	v0 *Signature
	v1 error
}

func __mrsignP224(c *Curve[*nistec.P224Point], priv *PrivateKey, drbg *hmacDRBG, hash []byte) __mrsignPair {
	__fw0, __fw1 := signGenericP224(c, priv, drbg, hash)
	return __mrsignPair{__fw0, __fw1}
}
func __mrsignP256(c *Curve[*nistec.P256Point], priv *PrivateKey, drbg *hmacDRBG, hash []byte) __mrsignPair {
	__fw0, __fw1 := signGenericP256(c, priv, drbg, hash)
	return __mrsignPair{__fw0, __fw1}
}
func __mrsignP384(c *Curve[*nistec.P384Point], priv *PrivateKey, drbg *hmacDRBG, hash []byte) __mrsignPair {
	__fw0, __fw1 := signGenericP384(c, priv, drbg, hash)
	return __mrsignPair{__fw0, __fw1}
}
func __mrsignP521(c *Curve[*nistec.P521Point], priv *PrivateKey, drbg *hmacDRBG, hash []byte) __mrsignPair {
	__fw0, __fw1 := signGenericP521(c, priv, drbg, hash)
	return __mrsignPair{__fw0, __fw1}
}

func signP224(c *Curve[*nistec.P224Point], priv *PrivateKey, drbg *hmacDRBG, hash []byte) (*Signature, error) {
	panic("crypto/internal/fips140/ecdsa: sign: multi-result desugar")
}
func signP256(c *Curve[*nistec.P256Point], priv *PrivateKey, drbg *hmacDRBG, hash []byte) (*Signature, error) {
	panic("crypto/internal/fips140/ecdsa: sign: multi-result desugar")
}
func signP384(c *Curve[*nistec.P384Point], priv *PrivateKey, drbg *hmacDRBG, hash []byte) (*Signature, error) {
	panic("crypto/internal/fips140/ecdsa: sign: multi-result desugar")
}
func signP521(c *Curve[*nistec.P521Point], priv *PrivateKey, drbg *hmacDRBG, hash []byte) (*Signature, error) {
	panic("crypto/internal/fips140/ecdsa: sign: multi-result desugar")
}

func verifyP224(c *Curve[*nistec.P224Point], pub *PublicKey, hash []byte, sig *Signature) error {
	return verifyGenericP224(c, pub, hash, sig)
}
func verifyP256(c *Curve[*nistec.P256Point], pub *PublicKey, hash []byte, sig *Signature) error {
	return verifyGenericP256(c, pub, hash, sig)
}
func verifyP384(c *Curve[*nistec.P384Point], pub *PublicKey, hash []byte, sig *Signature) error {
	return verifyGenericP384(c, pub, hash, sig)
}
func verifyP521(c *Curve[*nistec.P521Point], pub *PublicKey, hash []byte, sig *Signature) error {
	return verifyGenericP521(c, pub, hash, sig)
}


// ---- synthetic additions ----

// P3-M9: FIPS self-tests are no-ops in default (non-FIPS) mode.
var fipsSelfTest = func() {}
var fipsSelfTestDeterministic = func() {}

// P3-M9: fipsPCT is a no-op (FIPS mode disabled).
func fipsPCTP224(c *Curve[*nistec.P224Point], _ *PrivateKey) { _ = c }
func fipsPCTP256(c *Curve[*nistec.P256Point], _ *PrivateKey) { _ = c }
func fipsPCTP384(c *Curve[*nistec.P384Point], _ *PrivateKey) { _ = c }
func fipsPCTP521(c *Curve[*nistec.P521Point], _ *PrivateKey) { _ = c }

`
