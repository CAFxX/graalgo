package main

// P3-M9 tier-5: synthetic 'crypto/elliptic' package.
const syntheticCryptoEllipticSource = `// P3-M9 tier-5: synthetic 'crypto/elliptic' package.
// go1.27.1 vendored sources (elliptic.go, nistec.go, params.go).
// Pure Go; no adaptations needed (imports io, math/big, sync).
package elliptic

import (
	"io"
	"math/big"
	"crypto/internal/fips140/nistec"
	"errors"
)


// ---- vendored from crypto/elliptic/elliptic.go ----




// A Curve represents a short-form Weierstrass curve with a=-3.
//
// The behavior of Add, Double, and ScalarMult when the input is not a point on
// the curve is undefined.
//
// Note that the conventional point at infinity (0, 0) is not considered on the
// curve, although it can be returned by Add, Double, ScalarMult, or
// ScalarBaseMult (but not the [Unmarshal] or [UnmarshalCompressed] functions).
//
// Using Curve implementations besides those returned by [P224], [P256], [P384],
// and [P521] is deprecated.
type Curve interface {
	// Params returns the parameters for the curve.
	Params() *CurveParams

	// IsOnCurve reports whether the given (x,y) lies on the curve.
	//
	// Deprecated: this is a low-level unsafe API. For ECDH, use the crypto/ecdh
	// package. The NewPublicKey methods of NIST curves in crypto/ecdh accept
	// the same encoding as the Unmarshal function, and perform on-curve checks.
	IsOnCurve(x, y *big.Int) bool

	// Add returns the sum of (x1,y1) and (x2,y2).
	//
	// Deprecated: this is a low-level unsafe API.
	Add(x1, y1, x2, y2 *big.Int) (x, y *big.Int)

	// Double returns 2*(x,y).
	//
	// Deprecated: this is a low-level unsafe API.
	Double(x1, y1 *big.Int) (x, y *big.Int)

	// ScalarMult returns k*(x,y) where k is an integer in big-endian form.
	//
	// Deprecated: this is a low-level unsafe API. For ECDH, use the crypto/ecdh
	// package. Most uses of ScalarMult can be replaced by a call to the ECDH
	// methods of NIST curves in crypto/ecdh.
	ScalarMult(x1, y1 *big.Int, k []byte) (x, y *big.Int)

	// ScalarBaseMult returns k*G, where G is the base point of the group
	// and k is an integer in big-endian form.
	//
	// Deprecated: this is a low-level unsafe API. For ECDH, use the crypto/ecdh
	// package. Most uses of ScalarBaseMult can be replaced by a call to the
	// PrivateKey.PublicKey method in crypto/ecdh.
	ScalarBaseMult(k []byte) (x, y *big.Int)
}

var mask = []byte{0xff, 0x1, 0x3, 0x7, 0xf, 0x1f, 0x3f, 0x7f}

// GenerateKey returns a public/private key pair. The private key is
// generated using the given reader, which must return random data.
//
// Deprecated: for ECDH, use the GenerateKey methods of the [crypto/ecdh] package;
// for ECDSA, use the GenerateKey function of the crypto/ecdsa package.
type __mrGenerateKeyPair struct {
	priv []byte
	x *big.Int
	y *big.Int
	err error
}

func __mrGenerateKey(curve Curve, rand io.Reader) __mrGenerateKeyPair {
	var priv []byte
	var x *big.Int
	var y *big.Int
	var err error

	N := curve.Params().N
	bitSize := N.BitLen()
	byteLen := (bitSize + 7) / 8
	priv = make([]byte, byteLen)

	for x == nil {
		_, err = io.ReadFull(rand, priv)
		if err != nil {
			return __mrGenerateKeyPair{priv, x, y, err}
		}
		// We have to mask off any excess bits in the case that the size of the
		// underlying field is not a whole number of bytes.
		priv[0] &= mask[bitSize%8]
		// This is because, in tests, rand will return all zeros and we don't
		// want to get the point at infinity and loop forever.
		priv[1] ^= 0x42

		// If the scalar is out of range, sample another random number.
		if new(big.Int).SetBytes(priv).Cmp(N) >= 0 {
			continue
		}

		x, y = curve.ScalarBaseMult(priv)
	}
	return __mrGenerateKeyPair{priv, x, y, err}
}

func GenerateKey(curve Curve, rand io.Reader) (priv []byte, x, y *big.Int, err error) {
	panic("crypto/elliptic: GenerateKey: multi-result desugar")
}

// Marshal converts a point on the curve into the uncompressed form specified in
// SEC 1, Version 2.0, Section 2.3.3. If the point is not on the curve (or is
// the conventional point at infinity), the behavior is undefined.
//
// Deprecated: for ECDH, use the crypto/ecdh package. This function returns an
// encoding equivalent to that of PublicKey.Bytes in crypto/ecdh.
func Marshal(curve Curve, x, y *big.Int) []byte {
	panicIfNotOnCurve(curve, x, y)

	byteLen := (curve.Params().BitSize + 7) / 8

	ret := make([]byte, 1+2*byteLen)
	ret[0] = 4 // uncompressed point

	x.FillBytes(ret[1 : 1+byteLen])
	y.FillBytes(ret[1+byteLen : 1+2*byteLen])

	return ret
}

// MarshalCompressed converts a point on the curve into the compressed form
// specified in SEC 1, Version 2.0, Section 2.3.3. If the point is not on the
// curve (or is the conventional point at infinity), the behavior is undefined.
func MarshalCompressed(curve Curve, x, y *big.Int) []byte {
	panicIfNotOnCurve(curve, x, y)
	byteLen := (curve.Params().BitSize + 7) / 8
	compressed := make([]byte, 1+byteLen)
	compressed[0] = byte(y.Bit(0)) | 2
	x.FillBytes(compressed[1:])
	return compressed
}

// unmarshaler is implemented by curves with their own constant-time Unmarshal.
//
// There isn't an equivalent interface for Marshal/MarshalCompressed because
// that doesn't involve any mathematical operations, only FillBytes and Bit.
type unmarshaler interface {
	Unmarshal([]byte) (x, y *big.Int)
	UnmarshalCompressed([]byte) (x, y *big.Int)
}

// Assert that the known curves implement unmarshaler.
var _ = []unmarshaler{p224, p256, p384, p521}

// Unmarshal converts a point, serialized by [Marshal], into an x, y pair. It is
// an error if the point is not in uncompressed form, is not on the curve, or is
// the point at infinity. On error, x = nil.
//
// Deprecated: for ECDH, use the crypto/ecdh package. This function accepts an
// encoding equivalent to that of the NewPublicKey methods in crypto/ecdh.
type __mrUnmarshalPair struct {
	x *big.Int
	y *big.Int
}

func __mrUnmarshal(curve Curve, data []byte) __mrUnmarshalPair {
	var x *big.Int
	var y *big.Int

	if c, ok := curve.(unmarshaler); ok {
		__fw0, __fw1 := c.Unmarshal(data)
		return __mrUnmarshalPair{__fw0, __fw1}
	}

	byteLen := (curve.Params().BitSize + 7) / 8
	if len(data) != 1+2*byteLen {
		return __mrUnmarshalPair{nil, nil}
	}
	if data[0] != 4 { // uncompressed form
		return __mrUnmarshalPair{nil, nil}
	}
	p := curve.Params().P
	x = new(big.Int).SetBytes(data[1 : 1+byteLen])
	y = new(big.Int).SetBytes(data[1+byteLen:])
	if x.Cmp(p) >= 0 || y.Cmp(p) >= 0 {
		return __mrUnmarshalPair{nil, nil}
	}
	if !curve.IsOnCurve(x, y) {
		return __mrUnmarshalPair{nil, nil}
	}
	return __mrUnmarshalPair{x, y}
}

func Unmarshal(curve Curve, data []byte) (x, y *big.Int) {
	panic("crypto/elliptic: Unmarshal: multi-result desugar")
}

// UnmarshalCompressed converts a point, serialized by [MarshalCompressed], into
// an x, y pair. It is an error if the point is not in compressed form, is not
// on the curve, or is the point at infinity. On error, x = nil.
type __mrUnmarshalCompressedPair struct {
	x *big.Int
	y *big.Int
}

func __mrUnmarshalCompressed(curve Curve, data []byte) __mrUnmarshalCompressedPair {
	var x *big.Int
	var y *big.Int

	if c, ok := curve.(unmarshaler); ok {
		__fw0, __fw1 := c.UnmarshalCompressed(data)
		return __mrUnmarshalCompressedPair{__fw0, __fw1}
	}

	byteLen := (curve.Params().BitSize + 7) / 8
	if len(data) != 1+byteLen {
		return __mrUnmarshalCompressedPair{nil, nil}
	}
	if data[0] != 2 && data[0] != 3 { // compressed form
		return __mrUnmarshalCompressedPair{nil, nil}
	}
	p := curve.Params().P
	x = new(big.Int).SetBytes(data[1:])
	if x.Cmp(p) >= 0 {
		return __mrUnmarshalCompressedPair{nil, nil}
	}
	// y² = x³ - 3x + b
	y = curve.Params().polynomial(x)
	y = y.ModSqrt(y, p)
	if y == nil {
		return __mrUnmarshalCompressedPair{nil, nil}
	}
	if byte(y.Bit(0)) != data[0]&1 {
		y.Neg(y).Mod(y, p)
	}
	if !curve.IsOnCurve(x, y) {
		return __mrUnmarshalCompressedPair{nil, nil}
	}
	return __mrUnmarshalCompressedPair{x, y}
}

func UnmarshalCompressed(curve Curve, data []byte) (x, y *big.Int) {
	panic("crypto/elliptic: UnmarshalCompressed: multi-result desugar")
}

func panicIfNotOnCurve(curve Curve, x, y *big.Int) {
	// (0, 0) is the point at infinity by convention. It's ok to operate on it,
	// although IsOnCurve is documented to return false for it. See Issue 37294.
	if x.Sign() == 0 && y.Sign() == 0 {
		return
	}

	if !curve.IsOnCurve(x, y) {
		panic("crypto/elliptic: attempted operation on invalid point")
	}
}

var initonce bool

func initAll() {
	initP224()
	initP256()
	initP384()
	initP521()
}

// P224 returns a [Curve] which implements NIST P-224 (FIPS 186-3, section D.2.2),
// also known as secp224r1. The CurveParams.Name of this [Curve] is "P-224".
//
// Multiple invocations of this function will return the same value, so it can
// be used for equality checks and switch statements.
//
// The cryptographic operations are implemented using constant-time algorithms.
func P224() Curve {
	if !initonce { initAll(); initonce = true }
	return p224
}

// P256 returns a [Curve] which implements NIST P-256 (FIPS 186-3, section D.2.3),
// also known as secp256r1 or prime256v1. The CurveParams.Name of this [Curve] is
// "P-256".
//
// Multiple invocations of this function will return the same value, so it can
// be used for equality checks and switch statements.
//
// The cryptographic operations are implemented using constant-time algorithms.
func P256() Curve {
	if !initonce { initAll(); initonce = true }
	return p256
}

// P384 returns a [Curve] which implements NIST P-384 (FIPS 186-3, section D.2.4),
// also known as secp384r1. The CurveParams.Name of this [Curve] is "P-384".
//
// Multiple invocations of this function will return the same value, so it can
// be used for equality checks and switch statements.
//
// The cryptographic operations are implemented using constant-time algorithms.
func P384() Curve {
	if !initonce { initAll(); initonce = true }
	return p384
}

// P521 returns a [Curve] which implements NIST P-521 (FIPS 186-3, section D.2.5),
// also known as secp521r1. The CurveParams.Name of this [Curve] is "P-521".
//
// Multiple invocations of this function will return the same value, so it can
// be used for equality checks and switch statements.
//
// The cryptographic operations are implemented using constant-time algorithms.
func P521() Curve {
	if !initonce { initAll(); initonce = true }
	return p521
}


// ---- vendored from crypto/elliptic/nistec.go ----






// P3-M9: local aliases for nistec point types (frontend/v1 type exporter
// limitation with package-qualified types in generic instantiations).
type p224Point = nistec.P224Point
type p256Point = nistec.P256Point
type p384Point = nistec.P384Point
type p521Point = nistec.P521Point

// P3-M9: local wrappers for nistec constructors (frontend/v1 does not
// support package-qualified function values).
func newP224Point() *p224Point { return nistec.NewP224Point() }
func newP256Point() *p256Point { return nistec.NewP256Point() }
func newP384Point() *p384Point { return nistec.NewP384Point() }
func newP521Point() *p521Point { return nistec.NewP521Point() }

// P3-M9: concrete wrapper to avoid generic instantiation as Curve
// interface (P3-M3 scope: method tables for instantiated methods not
// supported). Delegates to the embedded generic nistCurve.
type p224Curve struct {
	nistCurve *nistCurve[*p224Point]
}

func (c *p224Curve) Params() *CurveParams { return c.nistCurve.Params() }
func (c *p224Curve) IsOnCurve(x, y *big.Int) bool { return c.nistCurve.IsOnCurve(x, y) }
func (c *p224Curve) Add(x1, y1, x2, y2 *big.Int) (*big.Int, *big.Int) { return c.nistCurve.Add(x1, y1, x2, y2) }
func (c *p224Curve) Double(x1, y1 *big.Int) (*big.Int, *big.Int) { return c.nistCurve.Double(x1, y1) }
func (c *p224Curve) ScalarMult(x1, y1 *big.Int, k []byte) (*big.Int, *big.Int) { return c.nistCurve.ScalarMult(x1, y1, k) }
func (c *p224Curve) ScalarBaseMult(k []byte) (*big.Int, *big.Int) { return c.nistCurve.ScalarBaseMult(k) }
func (c *p224Curve) Unmarshal(data []byte) (*big.Int, *big.Int) { return c.nistCurve.Unmarshal(data) }
func (c *p224Curve) UnmarshalCompressed(data []byte) (*big.Int, *big.Int) { return c.nistCurve.UnmarshalCompressed(data) }

var p224 = &p224Curve{
	nistCurve: &nistCurve[*p224Point]{
		newPoint: newP224Point,
	},
}

func initP224() {
	p224.nistCurve.params = &CurveParams{
		Name:    "P-224",
		BitSize: 224,
		// SP 800-186, Section 3.2.1.2
		P:  bigFromDecimal("26959946667150639794667015087019630673557916260026308143510066298881"),
		N:  bigFromDecimal("26959946667150639794667015087019625940457807714424391721682722368061"),
		B:  bigFromHex("b4050a850c04b3abf54132565044b0b7d7bfd8ba270b39432355ffb4"),
		Gx: bigFromHex("b70e0cbd6bb4bf7f321390b94a03c1d356c21122343280d6115c1d21"),
		Gy: bigFromHex("bd376388b5f723fb4c22dfe6cd4375a05a07476444d5819985007e34"),
	}
}

type p256Curve struct {
	nistCurve *nistCurve[*p256Point]
}

func (c *p256Curve) Params() *CurveParams { return c.nistCurve.Params() }
func (c *p256Curve) IsOnCurve(x, y *big.Int) bool { return c.nistCurve.IsOnCurve(x, y) }
func (c *p256Curve) Add(x1, y1, x2, y2 *big.Int) (*big.Int, *big.Int) { return c.nistCurve.Add(x1, y1, x2, y2) }
func (c *p256Curve) Double(x1, y1 *big.Int) (*big.Int, *big.Int) { return c.nistCurve.Double(x1, y1) }
func (c *p256Curve) ScalarMult(x1, y1 *big.Int, k []byte) (*big.Int, *big.Int) { return c.nistCurve.ScalarMult(x1, y1, k) }
func (c *p256Curve) ScalarBaseMult(k []byte) (*big.Int, *big.Int) { return c.nistCurve.ScalarBaseMult(k) }
func (c *p256Curve) Unmarshal(data []byte) (*big.Int, *big.Int) { return c.nistCurve.Unmarshal(data) }
func (c *p256Curve) UnmarshalCompressed(data []byte) (*big.Int, *big.Int) { return c.nistCurve.UnmarshalCompressed(data) }

var p256 = &p256Curve{
	nistCurve: &nistCurve[*p256Point]{
		newPoint: newP256Point,
	},
}

func initP256() {
	p256.nistCurve.params = &CurveParams{
		Name:    "P-256",
		BitSize: 256,
		// SP 800-186, Section 3.2.1.3
		P:  bigFromDecimal("115792089210356248762697446949407573530086143415290314195533631308867097853951"),
		N:  bigFromDecimal("115792089210356248762697446949407573529996955224135760342422259061068512044369"),
		B:  bigFromHex("5ac635d8aa3a93e7b3ebbd55769886bc651d06b0cc53b0f63bce3c3e27d2604b"),
		Gx: bigFromHex("6b17d1f2e12c4247f8bce6e563a440f277037d812deb33a0f4a13945d898c296"),
		Gy: bigFromHex("4fe342e2fe1a7f9b8ee7eb4a7c0f9e162bce33576b315ececbb6406837bf51f5"),
	}
}

type p384Curve struct {
	nistCurve *nistCurve[*p384Point]
}

func (c *p384Curve) Params() *CurveParams { return c.nistCurve.Params() }
func (c *p384Curve) IsOnCurve(x, y *big.Int) bool { return c.nistCurve.IsOnCurve(x, y) }
func (c *p384Curve) Add(x1, y1, x2, y2 *big.Int) (*big.Int, *big.Int) { return c.nistCurve.Add(x1, y1, x2, y2) }
func (c *p384Curve) Double(x1, y1 *big.Int) (*big.Int, *big.Int) { return c.nistCurve.Double(x1, y1) }
func (c *p384Curve) ScalarMult(x1, y1 *big.Int, k []byte) (*big.Int, *big.Int) { return c.nistCurve.ScalarMult(x1, y1, k) }
func (c *p384Curve) ScalarBaseMult(k []byte) (*big.Int, *big.Int) { return c.nistCurve.ScalarBaseMult(k) }
func (c *p384Curve) Unmarshal(data []byte) (*big.Int, *big.Int) { return c.nistCurve.Unmarshal(data) }
func (c *p384Curve) UnmarshalCompressed(data []byte) (*big.Int, *big.Int) { return c.nistCurve.UnmarshalCompressed(data) }

var p384 = &p384Curve{
	nistCurve: &nistCurve[*p384Point]{
		newPoint: newP384Point,
	},
}

func initP384() {
	p384.nistCurve.params = &CurveParams{
		Name:    "P-384",
		BitSize: 384,
		// SP 800-186, Section 3.2.1.4
		P: bigFromDecimal("394020061963944792122790401001436138050797392704654" +
			"46667948293404245721771496870329047266088258938001861606973112319"),
		N: bigFromDecimal("394020061963944792122790401001436138050797392704654" +
			"46667946905279627659399113263569398956308152294913554433653942643"),
		B: bigFromHex("b3312fa7e23ee7e4988e056be3f82d19181d9c6efe8141120314088" +
			"f5013875ac656398d8a2ed19d2a85c8edd3ec2aef"),
		Gx: bigFromHex("aa87ca22be8b05378eb1c71ef320ad746e1d3b628ba79b9859f741" +
			"e082542a385502f25dbf55296c3a545e3872760ab7"),
		Gy: bigFromHex("3617de4a96262c6f5d9e98bf9292dc29f8f41dbd289a147ce9da31" +
			"13b5f0b8c00a60b1ce1d7e819d7a431d7c90ea0e5f"),
	}
}

type p521Curve struct {
	nistCurve *nistCurve[*p521Point]
}

func (c *p521Curve) Params() *CurveParams { return c.nistCurve.Params() }
func (c *p521Curve) IsOnCurve(x, y *big.Int) bool { return c.nistCurve.IsOnCurve(x, y) }
func (c *p521Curve) Add(x1, y1, x2, y2 *big.Int) (*big.Int, *big.Int) { return c.nistCurve.Add(x1, y1, x2, y2) }
func (c *p521Curve) Double(x1, y1 *big.Int) (*big.Int, *big.Int) { return c.nistCurve.Double(x1, y1) }
func (c *p521Curve) ScalarMult(x1, y1 *big.Int, k []byte) (*big.Int, *big.Int) { return c.nistCurve.ScalarMult(x1, y1, k) }
func (c *p521Curve) ScalarBaseMult(k []byte) (*big.Int, *big.Int) { return c.nistCurve.ScalarBaseMult(k) }
func (c *p521Curve) Unmarshal(data []byte) (*big.Int, *big.Int) { return c.nistCurve.Unmarshal(data) }
func (c *p521Curve) UnmarshalCompressed(data []byte) (*big.Int, *big.Int) { return c.nistCurve.UnmarshalCompressed(data) }

var p521 = &p521Curve{
	nistCurve: &nistCurve[*p521Point]{
		newPoint: newP521Point,
	},
}

func initP521() {
	p521.nistCurve.params = &CurveParams{
		Name:    "P-521",
		BitSize: 521,
		// SP 800-186, Section 3.2.1.5
		P: bigFromDecimal("68647976601306097149819007990813932172694353001433" +
			"0540939446345918554318339765605212255964066145455497729631139148" +
			"0858037121987999716643812574028291115057151"),
		N: bigFromDecimal("68647976601306097149819007990813932172694353001433" +
			"0540939446345918554318339765539424505774633321719753296399637136" +
			"3321113864768612440380340372808892707005449"),
		B: bigFromHex("0051953eb9618e1c9a1f929a21a0b68540eea2da725b99b315f3b8" +
			"b489918ef109e156193951ec7e937b1652c0bd3bb1bf073573df883d2c34f1ef" +
			"451fd46b503f00"),
		Gx: bigFromHex("00c6858e06b70404e9cd9e3ecb662395b4429c648139053fb521f8" +
			"28af606b4d3dbaa14b5e77efe75928fe1dc127a2ffa8de3348b3c1856a429bf9" +
			"7e7e31c2e5bd66"),
		Gy: bigFromHex("011839296a789a3bc0045c8a5fb42c7d1bd998f54449579b446817" +
			"afbd17273e662c97ee72995ef42640c550b9013fad0761353c7086a272c24088" +
			"be94769fd16650"),
	}
}

// nistCurve is a Curve implementation based on a nistec Point.
//
// It's a wrapper that exposes the big.Int-based Curve interface and encodes the
// legacy idiosyncrasies it requires, such as invalid and infinity point
// handling.
//
// To interact with the nistec package, points are encoded into and decoded from
// properly formatted byte slices. All big.Int use is limited to this package.
// Encoding and decoding is 1/1000th of the runtime of a scalar multiplication,
// so the overhead is acceptable.
type nistCurve[Point nistPoint[Point]] struct {
	newPoint func() Point
	params   *CurveParams
}

// nistPoint is a generic constraint for the nistec Point types.
type nistPoint[T any] interface {
	Bytes() []byte
	SetBytes([]byte) (T, error)
	Add(T, T) T
	Double(T) T
	ScalarMult(T, []byte) (T, error)
	ScalarBaseMult([]byte) (T, error)
}

func (curve *nistCurve[Point]) Params() *CurveParams {
	return curve.params
}

func (curve *nistCurve[Point]) IsOnCurve(x, y *big.Int) bool {
	// IsOnCurve is documented to reject (0, 0), the conventional point at
	// infinity, which however is accepted by pointFromAffine.
	if x.Sign() == 0 && y.Sign() == 0 {
		return false
	}
	_, err := curve.pointFromAffine(x, y)
	return err == nil
}

type __mrnistCurve_pointFromAffinePair[Point nistPoint[Point]] struct {
	p Point
	err error
}

func __mrnistCurve_pointFromAffine[Point nistPoint[Point]](curve *nistCurve[Point], x, y *big.Int) __mrnistCurve_pointFromAffinePair[Point] {
	var p Point

	// (0, 0) is by convention the point at infinity, which can't be represented
	// in affine coordinates. See Issue 37294.
	if x.Sign() == 0 && y.Sign() == 0 {
		return __mrnistCurve_pointFromAffinePair[Point]{curve.newPoint(), nil}
	}
	// Reject values that would not get correctly encoded.
	if x.Sign() < 0 || y.Sign() < 0 {
		return __mrnistCurve_pointFromAffinePair[Point]{p, errors.New("negative coordinate")}
	}
	if x.BitLen() > curve.params.BitSize || y.BitLen() > curve.params.BitSize {
		return __mrnistCurve_pointFromAffinePair[Point]{p, errors.New("overflowing coordinate")}
	}
	// Encode the coordinates and let SetBytes reject invalid points.
	byteLen := (curve.params.BitSize + 7) / 8
	buf := make([]byte, 1+2*byteLen)
	buf[0] = 4 // uncompressed point
	x.FillBytes(buf[1 : 1+byteLen])
	y.FillBytes(buf[1+byteLen : 1+2*byteLen])
	__fw0, __fw1 := curve.newPoint().SetBytes(buf)
	return __mrnistCurve_pointFromAffinePair[Point]{__fw0, __fw1}
}

func (curve *nistCurve[Point]) pointFromAffine(x, y *big.Int) (p Point, err error) {
	panic("crypto/elliptic: pointFromAffine: multi-result desugar")
}

type __mrnistCurve_pointToAffinePair struct {
	x *big.Int
	y *big.Int
}

func __mrnistCurve_pointToAffine[Point nistPoint[Point]](curve *nistCurve[Point], p Point) __mrnistCurve_pointToAffinePair {
	var x *big.Int
	var y *big.Int

	out := p.Bytes()
	if len(out) == 1 && out[0] == 0 {
		// This is the encoding of the point at infinity, which the affine
		// coordinates API represents as (0, 0) by convention.
		return __mrnistCurve_pointToAffinePair{new(big.Int), new(big.Int)}
	}
	byteLen := (curve.params.BitSize + 7) / 8
	x = new(big.Int).SetBytes(out[1 : 1+byteLen])
	y = new(big.Int).SetBytes(out[1+byteLen:])
	return __mrnistCurve_pointToAffinePair{x, y}
}

func (curve *nistCurve[Point]) pointToAffine(p Point) (x, y *big.Int) {
	panic("crypto/elliptic: pointToAffine: multi-result desugar")
}

type __mrnistCurve_AddPair struct {
	v0 *big.Int
	v1 *big.Int
}

func __mrnistCurve_Add[Point nistPoint[Point]](curve *nistCurve[Point], x1, y1, x2, y2 *big.Int) __mrnistCurve_AddPair {
	p1, err := curve.pointFromAffine(x1, y1)
	if err != nil {
		panic("crypto/elliptic: Add was called on an invalid point")
	}
	p2, err := curve.pointFromAffine(x2, y2)
	if err != nil {
		panic("crypto/elliptic: Add was called on an invalid point")
	}
	__fw0, __fw1 := curve.pointToAffine(p1.Add(p1, p2))
	return __mrnistCurve_AddPair{__fw0, __fw1}
}

func (curve *nistCurve[Point]) Add(x1, y1, x2, y2 *big.Int) (*big.Int, *big.Int) {
	panic("crypto/elliptic: Add: multi-result desugar")
}

type __mrnistCurve_DoublePair struct {
	v0 *big.Int
	v1 *big.Int
}

func __mrnistCurve_Double[Point nistPoint[Point]](curve *nistCurve[Point], x1, y1 *big.Int) __mrnistCurve_DoublePair {
	p, err := curve.pointFromAffine(x1, y1)
	if err != nil {
		panic("crypto/elliptic: Double was called on an invalid point")
	}
	__fw0, __fw1 := curve.pointToAffine(p.Double(p))
	return __mrnistCurve_DoublePair{__fw0, __fw1}
}

func (curve *nistCurve[Point]) Double(x1, y1 *big.Int) (*big.Int, *big.Int) {
	panic("crypto/elliptic: Double: multi-result desugar")
}

// normalizeScalar brings the scalar within the byte size of the order of the
// curve, as expected by the nistec scalar multiplication functions.
func (curve *nistCurve[Point]) normalizeScalar(scalar []byte) []byte {
	byteSize := (curve.params.N.BitLen() + 7) / 8
	if len(scalar) == byteSize {
		return scalar
	}
	s := new(big.Int).SetBytes(scalar)
	if len(scalar) > byteSize {
		s.Mod(s, curve.params.N)
	}
	out := make([]byte, byteSize)
	return s.FillBytes(out)
}

type __mrnistCurve_ScalarMultPair struct {
	v0 *big.Int
	v1 *big.Int
}

func __mrnistCurve_ScalarMult[Point nistPoint[Point]](curve *nistCurve[Point], Bx, By *big.Int, scalar []byte) __mrnistCurve_ScalarMultPair {
	p, err := curve.pointFromAffine(Bx, By)
	if err != nil {
		panic("crypto/elliptic: ScalarMult was called on an invalid point")
	}
	scalar = curve.normalizeScalar(scalar)
	p, err = p.ScalarMult(p, scalar)
	if err != nil {
		panic("crypto/elliptic: nistec rejected normalized scalar")
	}
	__fw0, __fw1 := curve.pointToAffine(p)
	return __mrnistCurve_ScalarMultPair{__fw0, __fw1}
}

func (curve *nistCurve[Point]) ScalarMult(Bx, By *big.Int, scalar []byte) (*big.Int, *big.Int) {
	panic("crypto/elliptic: ScalarMult: multi-result desugar")
}

type __mrnistCurve_ScalarBaseMultPair struct {
	v0 *big.Int
	v1 *big.Int
}

func __mrnistCurve_ScalarBaseMult[Point nistPoint[Point]](curve *nistCurve[Point], scalar []byte) __mrnistCurve_ScalarBaseMultPair {
	scalar = curve.normalizeScalar(scalar)
	p, err := curve.newPoint().ScalarBaseMult(scalar)
	if err != nil {
		panic("crypto/elliptic: nistec rejected normalized scalar")
	}
	__fw0, __fw1 := curve.pointToAffine(p)
	return __mrnistCurve_ScalarBaseMultPair{__fw0, __fw1}
}

func (curve *nistCurve[Point]) ScalarBaseMult(scalar []byte) (*big.Int, *big.Int) {
	panic("crypto/elliptic: ScalarBaseMult: multi-result desugar")
}

type __mrnistCurve_UnmarshalPair struct {
	x *big.Int
	y *big.Int
}

func __mrnistCurve_Unmarshal[Point nistPoint[Point]](curve *nistCurve[Point], data []byte) __mrnistCurve_UnmarshalPair {
	var x *big.Int
	var y *big.Int

	if len(data) == 0 || data[0] != 4 {
		return __mrnistCurve_UnmarshalPair{nil, nil}
	}
	// Use SetBytes to check that data encodes a valid point.
	_, err := curve.newPoint().SetBytes(data)
	if err != nil {
		return __mrnistCurve_UnmarshalPair{nil, nil}
	}
	// We don't use pointToAffine because it involves an expensive field
	// inversion to convert from Jacobian to affine coordinates, which we
	// already have.
	byteLen := (curve.params.BitSize + 7) / 8
	x = new(big.Int).SetBytes(data[1 : 1+byteLen])
	y = new(big.Int).SetBytes(data[1+byteLen:])
	return __mrnistCurve_UnmarshalPair{x, y}
}

func (curve *nistCurve[Point]) Unmarshal(data []byte) (x, y *big.Int) {
	panic("crypto/elliptic: Unmarshal: multi-result desugar")
}

type __mrnistCurve_UnmarshalCompressedPair struct {
	x *big.Int
	y *big.Int
}

func __mrnistCurve_UnmarshalCompressed[Point nistPoint[Point]](curve *nistCurve[Point], data []byte) __mrnistCurve_UnmarshalCompressedPair {
	if len(data) == 0 || (data[0] != 2 && data[0] != 3) {
		return __mrnistCurve_UnmarshalCompressedPair{nil, nil}
	}
	p, err := curve.newPoint().SetBytes(data)
	if err != nil {
		return __mrnistCurve_UnmarshalCompressedPair{nil, nil}
	}
	__fw0, __fw1 := curve.pointToAffine(p)
	return __mrnistCurve_UnmarshalCompressedPair{__fw0, __fw1}
}

func (curve *nistCurve[Point]) UnmarshalCompressed(data []byte) (x, y *big.Int) {
	panic("crypto/elliptic: UnmarshalCompressed: multi-result desugar")
}

func bigFromDecimal(s string) *big.Int {
	b, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("crypto/elliptic: internal error: invalid encoding")
	}
	return b
}

func bigFromHex(s string) *big.Int {
	b, ok := new(big.Int).SetString(s, 16)
	if !ok {
		panic("crypto/elliptic: internal error: invalid encoding")
	}
	return b
}


// ---- vendored from crypto/elliptic/params.go ----



// CurveParams contains the parameters of an elliptic curve and also provides
// a generic, non-constant time implementation of [Curve].
//
// The generic Curve implementation is deprecated, and using custom curves
// (those not returned by [P224], [P256], [P384], and [P521]) is not guaranteed
// to provide any security property.
type CurveParams struct {
	P       *big.Int // the order of the underlying field
	N       *big.Int // the order of the base point
	B       *big.Int // the constant of the curve equation
	Gx, Gy  *big.Int // (x,y) of the base point
	BitSize int      // the size of the underlying field
	Name    string   // the canonical name of the curve
}

func (curve *CurveParams) Params() *CurveParams {
	return curve
}

// CurveParams operates, internally, on Jacobian coordinates. For a given
// (x, y) position on the curve, the Jacobian coordinates are (x1, y1, z1)
// where x = x1/z1² and y = y1/z1³. The greatest speedups come when the whole
// calculation can be performed within the transform (as in ScalarMult and
// ScalarBaseMult). But even for Add and Double, it's faster to apply and
// reverse the transform than to operate in affine coordinates.

// polynomial returns x³ - 3x + b.
func (curve *CurveParams) polynomial(x *big.Int) *big.Int {
	x3 := new(big.Int).Mul(x, x)
	x3.Mul(x3, x)

	threeX := new(big.Int).Lsh(x, 1)
	threeX.Add(threeX, x)

	x3.Sub(x3, threeX)
	x3.Add(x3, curve.B)
	x3.Mod(x3, curve.P)

	return x3
}

// IsOnCurve implements [Curve.IsOnCurve].
//
// Deprecated: the [CurveParams] methods are deprecated and are not guaranteed to
// provide any security property. For ECDH, use the [crypto/ecdh] package.
// For ECDSA, use the [crypto/ecdsa] package with a [Curve] value returned directly
// from [P224], [P256], [P384], or [P521].
func (curve *CurveParams) IsOnCurve(x, y *big.Int) bool {
	// If there is a dedicated constant-time implementation for this curve operation,
	// use that instead of the generic one.
	if specific, ok := matchesSpecificCurve(curve); ok {
		return specific.IsOnCurve(x, y)
	}

	if x.Sign() < 0 || x.Cmp(curve.P) >= 0 ||
		y.Sign() < 0 || y.Cmp(curve.P) >= 0 {
		return false
	}

	// y² = x³ - 3x + b
	y2 := new(big.Int).Mul(y, y)
	y2.Mod(y2, curve.P)

	return curve.polynomial(x).Cmp(y2) == 0
}

// zForAffine returns a Jacobian Z value for the affine point (x, y). If x and
// y are zero, it assumes that they represent the point at infinity because (0,
// 0) is not on the any of the curves handled here.
func zForAffine(x, y *big.Int) *big.Int {
	z := new(big.Int)
	if x.Sign() != 0 || y.Sign() != 0 {
		z.SetInt64(1)
	}
	return z
}

// affineFromJacobian reverses the Jacobian transform. See the comment at the
// top of the file. If the point is ∞ it returns 0, 0.
type __mrCurveParams_affineFromJacobianPair struct {
	xOut *big.Int
	yOut *big.Int
}

func __mrCurveParams_affineFromJacobian(curve *CurveParams, x, y, z *big.Int) __mrCurveParams_affineFromJacobianPair {
	var xOut *big.Int
	var yOut *big.Int

	if z.Sign() == 0 {
		return __mrCurveParams_affineFromJacobianPair{new(big.Int), new(big.Int)}
	}

	zinv := new(big.Int).ModInverse(z, curve.P)
	zinvsq := new(big.Int).Mul(zinv, zinv)

	xOut = new(big.Int).Mul(x, zinvsq)
	xOut.Mod(xOut, curve.P)
	zinvsq.Mul(zinvsq, zinv)
	yOut = new(big.Int).Mul(y, zinvsq)
	yOut.Mod(yOut, curve.P)
	return __mrCurveParams_affineFromJacobianPair{xOut, yOut}
}

func (curve *CurveParams) affineFromJacobian(x, y, z *big.Int) (xOut, yOut *big.Int) {
	panic("crypto/elliptic: affineFromJacobian: multi-result desugar")
}

// Add implements [Curve.Add].
//
// Deprecated: the [CurveParams] methods are deprecated and are not guaranteed to
// provide any security property. For ECDH, use the [crypto/ecdh] package.
// For ECDSA, use the [crypto/ecdsa] package with a [Curve] value returned directly
// from [P224], [P256], [P384], or [P521].
type __mrCurveParams_AddPair struct {
	v0 *big.Int
	v1 *big.Int
}

func __mrCurveParams_Add(curve *CurveParams, x1, y1, x2, y2 *big.Int) __mrCurveParams_AddPair {
	// If there is a dedicated constant-time implementation for this curve operation,
	// use that instead of the generic one.
	if specific, ok := matchesSpecificCurve(curve); ok {
		__fw0, __fw1 := specific.Add(x1, y1, x2, y2)
		return __mrCurveParams_AddPair{__fw0, __fw1}
	}
	panicIfNotOnCurve(curve, x1, y1)
	panicIfNotOnCurve(curve, x2, y2)

	z1 := zForAffine(x1, y1)
	z2 := zForAffine(x2, y2)
	__fw0, __fw1 := curve.affineFromJacobian(curve.addJacobian(x1, y1, z1, x2, y2, z2))
	return __mrCurveParams_AddPair{__fw0, __fw1}
}

func (curve *CurveParams) Add(x1, y1, x2, y2 *big.Int) (*big.Int, *big.Int) {
	panic("crypto/elliptic: Add: multi-result desugar")
}

// addJacobian takes two points in Jacobian coordinates, (x1, y1, z1) and
// (x2, y2, z2) and returns their sum, also in Jacobian form.
type __mrCurveParams_addJacobianPair struct {
	v0 *big.Int
	v1 *big.Int
	v2 *big.Int
}

func __mrCurveParams_addJacobian(curve *CurveParams, x1, y1, z1, x2, y2, z2 *big.Int) __mrCurveParams_addJacobianPair {
	// See https://hyperelliptic.org/EFD/g1p/auto-shortw-jacobian-3.html#addition-add-2007-bl
	x3, y3, z3 := new(big.Int), new(big.Int), new(big.Int)
	if z1.Sign() == 0 {
		x3.Set(x2)
		y3.Set(y2)
		z3.Set(z2)
		return __mrCurveParams_addJacobianPair{x3, y3, z3}
	}
	if z2.Sign() == 0 {
		x3.Set(x1)
		y3.Set(y1)
		z3.Set(z1)
		return __mrCurveParams_addJacobianPair{x3, y3, z3}
	}

	z1z1 := new(big.Int).Mul(z1, z1)
	z1z1.Mod(z1z1, curve.P)
	z2z2 := new(big.Int).Mul(z2, z2)
	z2z2.Mod(z2z2, curve.P)

	u1 := new(big.Int).Mul(x1, z2z2)
	u1.Mod(u1, curve.P)
	u2 := new(big.Int).Mul(x2, z1z1)
	u2.Mod(u2, curve.P)
	h := new(big.Int).Sub(u2, u1)
	xEqual := h.Sign() == 0
	if h.Sign() == -1 {
		h.Add(h, curve.P)
	}
	i := new(big.Int).Lsh(h, 1)
	i.Mul(i, i)
	j := new(big.Int).Mul(h, i)

	s1 := new(big.Int).Mul(y1, z2)
	s1.Mul(s1, z2z2)
	s1.Mod(s1, curve.P)
	s2 := new(big.Int).Mul(y2, z1)
	s2.Mul(s2, z1z1)
	s2.Mod(s2, curve.P)
	r := new(big.Int).Sub(s2, s1)
	if r.Sign() == -1 {
		r.Add(r, curve.P)
	}
	yEqual := r.Sign() == 0
	if xEqual && yEqual {
		__fw0, __fw1, __fw2 := curve.doubleJacobian(x1, y1, z1)
		return __mrCurveParams_addJacobianPair{__fw0, __fw1, __fw2}
	}
	r.Lsh(r, 1)
	v := new(big.Int).Mul(u1, i)

	x3.Set(r)
	x3.Mul(x3, x3)
	x3.Sub(x3, j)
	x3.Sub(x3, v)
	x3.Sub(x3, v)
	x3.Mod(x3, curve.P)

	y3.Set(r)
	v.Sub(v, x3)
	y3.Mul(y3, v)
	s1.Mul(s1, j)
	s1.Lsh(s1, 1)
	y3.Sub(y3, s1)
	y3.Mod(y3, curve.P)

	z3.Add(z1, z2)
	z3.Mul(z3, z3)
	z3.Sub(z3, z1z1)
	z3.Sub(z3, z2z2)
	z3.Mul(z3, h)
	z3.Mod(z3, curve.P)

	return __mrCurveParams_addJacobianPair{x3, y3, z3}
}

func (curve *CurveParams) addJacobian(x1, y1, z1, x2, y2, z2 *big.Int) (*big.Int, *big.Int, *big.Int) {
	panic("crypto/elliptic: addJacobian: multi-result desugar")
}

// Double implements [Curve.Double].
//
// Deprecated: the [CurveParams] methods are deprecated and are not guaranteed to
// provide any security property. For ECDH, use the [crypto/ecdh] package.
// For ECDSA, use the [crypto/ecdsa] package with a [Curve] value returned directly
// from [P224], [P256], [P384], or [P521].
type __mrCurveParams_DoublePair struct {
	v0 *big.Int
	v1 *big.Int
}

func __mrCurveParams_Double(curve *CurveParams, x1, y1 *big.Int) __mrCurveParams_DoublePair {
	// If there is a dedicated constant-time implementation for this curve operation,
	// use that instead of the generic one.
	if specific, ok := matchesSpecificCurve(curve); ok {
		__fw0, __fw1 := specific.Double(x1, y1)
		return __mrCurveParams_DoublePair{__fw0, __fw1}
	}
	panicIfNotOnCurve(curve, x1, y1)

	z1 := zForAffine(x1, y1)
	__fw0, __fw1 := curve.affineFromJacobian(curve.doubleJacobian(x1, y1, z1))
	return __mrCurveParams_DoublePair{__fw0, __fw1}
}

func (curve *CurveParams) Double(x1, y1 *big.Int) (*big.Int, *big.Int) {
	panic("crypto/elliptic: Double: multi-result desugar")
}

// doubleJacobian takes a point in Jacobian coordinates, (x, y, z), and
// returns its double, also in Jacobian form.
type __mrCurveParams_doubleJacobianPair struct {
	v0 *big.Int
	v1 *big.Int
	v2 *big.Int
}

func __mrCurveParams_doubleJacobian(curve *CurveParams, x, y, z *big.Int) __mrCurveParams_doubleJacobianPair {
	// See https://hyperelliptic.org/EFD/g1p/auto-shortw-jacobian-3.html#doubling-dbl-2001-b
	delta := new(big.Int).Mul(z, z)
	delta.Mod(delta, curve.P)
	gamma := new(big.Int).Mul(y, y)
	gamma.Mod(gamma, curve.P)
	alpha := new(big.Int).Sub(x, delta)
	if alpha.Sign() == -1 {
		alpha.Add(alpha, curve.P)
	}
	alpha2 := new(big.Int).Add(x, delta)
	alpha.Mul(alpha, alpha2)
	alpha2.Set(alpha)
	alpha.Lsh(alpha, 1)
	alpha.Add(alpha, alpha2)

	beta := alpha2.Mul(x, gamma)

	x3 := new(big.Int).Mul(alpha, alpha)
	beta8 := new(big.Int).Lsh(beta, 3)
	beta8.Mod(beta8, curve.P)
	x3.Sub(x3, beta8)
	if x3.Sign() == -1 {
		x3.Add(x3, curve.P)
	}
	x3.Mod(x3, curve.P)

	z3 := new(big.Int).Add(y, z)
	z3.Mul(z3, z3)
	z3.Sub(z3, gamma)
	if z3.Sign() == -1 {
		z3.Add(z3, curve.P)
	}
	z3.Sub(z3, delta)
	if z3.Sign() == -1 {
		z3.Add(z3, curve.P)
	}
	z3.Mod(z3, curve.P)

	beta.Lsh(beta, 2)
	beta.Sub(beta, x3)
	if beta.Sign() == -1 {
		beta.Add(beta, curve.P)
	}
	y3 := alpha.Mul(alpha, beta)

	gamma.Mul(gamma, gamma)
	gamma.Lsh(gamma, 3)
	gamma.Mod(gamma, curve.P)

	y3.Sub(y3, gamma)
	if y3.Sign() == -1 {
		y3.Add(y3, curve.P)
	}
	y3.Mod(y3, curve.P)

	return __mrCurveParams_doubleJacobianPair{x3, y3, z3}
}

func (curve *CurveParams) doubleJacobian(x, y, z *big.Int) (*big.Int, *big.Int, *big.Int) {
	panic("crypto/elliptic: doubleJacobian: multi-result desugar")
}

// ScalarMult implements [Curve.ScalarMult].
//
// Deprecated: the [CurveParams] methods are deprecated and are not guaranteed to
// provide any security property. For ECDH, use the [crypto/ecdh] package.
// For ECDSA, use the [crypto/ecdsa] package with a [Curve] value returned directly
// from [P224], [P256], [P384], or [P521].
type __mrCurveParams_ScalarMultPair struct {
	v0 *big.Int
	v1 *big.Int
}

func __mrCurveParams_ScalarMult(curve *CurveParams, Bx, By *big.Int, k []byte) __mrCurveParams_ScalarMultPair {
	// If there is a dedicated constant-time implementation for this curve operation,
	// use that instead of the generic one.
	if specific, ok := matchesSpecificCurve(curve); ok {
		__fw0, __fw1 := specific.ScalarMult(Bx, By, k)
		return __mrCurveParams_ScalarMultPair{__fw0, __fw1}
	}
	panicIfNotOnCurve(curve, Bx, By)

	Bz := new(big.Int).SetInt64(1)
	x, y, z := new(big.Int), new(big.Int), new(big.Int)

	for _, b := range k {
		for range 8 {
			x, y, z = curve.doubleJacobian(x, y, z)
			if b&0x80 == 0x80 {
				x, y, z = curve.addJacobian(Bx, By, Bz, x, y, z)
			}
			b <<= 1
		}
	}

	__fw0, __fw1 := curve.affineFromJacobian(x, y, z)
	return __mrCurveParams_ScalarMultPair{__fw0, __fw1}
}

func (curve *CurveParams) ScalarMult(Bx, By *big.Int, k []byte) (*big.Int, *big.Int) {
	panic("crypto/elliptic: ScalarMult: multi-result desugar")
}

// ScalarBaseMult implements [Curve.ScalarBaseMult].
//
// Deprecated: the [CurveParams] methods are deprecated and are not guaranteed to
// provide any security property. For ECDH, use the [crypto/ecdh] package.
// For ECDSA, use the [crypto/ecdsa] package with a [Curve] value returned directly
// from [P224], [P256], [P384], or [P521].
type __mrCurveParams_ScalarBaseMultPair struct {
	v0 *big.Int
	v1 *big.Int
}

func __mrCurveParams_ScalarBaseMult(curve *CurveParams, k []byte) __mrCurveParams_ScalarBaseMultPair {
	// If there is a dedicated constant-time implementation for this curve operation,
	// use that instead of the generic one.
	if specific, ok := matchesSpecificCurve(curve); ok {
		__fw0, __fw1 := specific.ScalarBaseMult(k)
		return __mrCurveParams_ScalarBaseMultPair{__fw0, __fw1}
	}

	__fw0, __fw1 := curve.ScalarMult(curve.Gx, curve.Gy, k)
	return __mrCurveParams_ScalarBaseMultPair{__fw0, __fw1}
}

func (curve *CurveParams) ScalarBaseMult(k []byte) (*big.Int, *big.Int) {
	panic("crypto/elliptic: ScalarBaseMult: multi-result desugar")
}

type __mrmatchesSpecificCurvePair struct {
	v0 Curve
	v1 bool
}

func __mrmatchesSpecificCurve(params *CurveParams) __mrmatchesSpecificCurvePair {
	for _, c := range []Curve{p224, p256, p384, p521} {
		if params == c.Params() {
			return __mrmatchesSpecificCurvePair{c, true}
		}
	}
	return __mrmatchesSpecificCurvePair{nil, false}
}

func matchesSpecificCurve(params *CurveParams) (Curve, bool) {
	panic("crypto/elliptic: matchesSpecificCurve: multi-result desugar")
}

`
