// P3-M9 tier-5: synthetic `crypto/internal/fips140/edwards25519`.
// go1.27.1 vendored sources. Synthetic-only.
package edwards25519

import (
	_ "crypto/internal/fips140/check"
	"crypto/internal/fips140/edwards25519/field"
	"errors"
	"crypto/internal/fips140deps/byteorder"
	"math/bits"
	"sync"
	"crypto/internal/constanttime"
)


// ---- vendored from crypto/internal/fips140/edwards25519/edwards25519.go ----




// Point types.

type projP1xP1 struct {
	X, Y, Z, T field.Element
}

type projP2 struct {
	X, Y, Z field.Element
}

// Point represents a point on the edwards25519 curve.
//
// This type works similarly to math/big.Int, and all arguments and receivers
// are allowed to alias.
//
// The zero value is NOT valid, and it may be used only as a receiver.
type Point struct {
	// Make the type not comparable (i.e. used with == or as a map key), as
	// equivalent points can be represented by different Go values.
	_ incomparable

	// The point is internally represented in extended coordinates (X, Y, Z, T)
	// where x = X/Z, y = Y/Z, and xy = T/Z per https://eprint.iacr.org/2008/522.
	x, y, z, t field.Element
}

type incomparable [0]func()

func checkInitialized(points ...*Point) {
	for _, p := range points {
		if p.x == (field.Element{}) && p.y == (field.Element{}) {
			panic("edwards25519: use of uninitialized Point")
		}
	}
}

type projCached struct {
	YplusX, YminusX, Z, T2d field.Element
}

type affineCached struct {
	YplusX, YminusX, T2d field.Element
}

// Constructors.

func (v *projP2) Zero() *projP2 {
	v.X.Zero()
	v.Y.One()
	v.Z.One()
	return v
}

// identity is the point at infinity.
var identity, _ = new(Point).SetBytes([]byte{
	1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})

// NewIdentityPoint returns a new Point set to the identity.
func NewIdentityPoint() *Point {
	return new(Point).Set(identity)
}

// generator is the canonical curve basepoint. See TestGenerator for the
// correspondence of this encoding with the values in RFC 8032.
var generator, _ = new(Point).SetBytes([]byte{
	0x58, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66,
	0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66,
	0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66,
	0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66})

// NewGeneratorPoint returns a new Point set to the canonical generator.
func NewGeneratorPoint() *Point {
	return new(Point).Set(generator)
}

func (v *projCached) Zero() *projCached {
	v.YplusX.One()
	v.YminusX.One()
	v.Z.One()
	v.T2d.Zero()
	return v
}

func (v *affineCached) Zero() *affineCached {
	v.YplusX.One()
	v.YminusX.One()
	v.T2d.Zero()
	return v
}

// Assignments.

// Set sets v = u, and returns v.
func (v *Point) Set(u *Point) *Point {
	*v = *u
	return v
}

// Encoding.

// Bytes returns the canonical 32-byte encoding of v, according to RFC 8032,
// Section 5.1.2.
func (v *Point) Bytes() []byte {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var buf [32]byte
	return v.bytes(&buf)
}

func (v *Point) bytes(buf *[32]byte) []byte {
	checkInitialized(v)

	var zInv, x, y field.Element
	zInv.Invert(&v.z)       // zInv = 1 / Z
	x.Multiply(&v.x, &zInv) // x = X / Z
	y.Multiply(&v.y, &zInv) // y = Y / Z

	out := copyFieldElement(buf, &y)
	out[31] |= byte(x.IsNegative() << 7)
	return out
}

var feOne = new(field.Element).One()

// SetBytes sets v = x, where x is a 32-byte encoding of v. If x does not
// represent a valid point on the curve, SetBytes returns nil and an error and
// the receiver is unchanged. Otherwise, SetBytes returns v.
//
// Note that SetBytes accepts all non-canonical encodings of valid points.
// That is, it follows decoding rules that match most implementations in
// the ecosystem rather than RFC 8032.
type __mrPoint_SetBytesPair struct {
	v0 *Point
	v1 error
}

func __mrPoint_SetBytes(v *Point, x []byte) __mrPoint_SetBytesPair {
	// Specifically, the non-canonical encodings that are accepted are
	//   1) the ones where the field element is not reduced (see the
	//      (*field.Element).SetBytes docs) and
	//   2) the ones where the x-coordinate is zero and the sign bit is set.
	//
	// Read more at https://hdevalence.ca/blog/2020-10-04-its-25519am,
	// specifically the "Canonical A, R" section.

	y, err := new(field.Element).SetBytes(x)
	if err != nil {
		return __mrPoint_SetBytesPair{nil, errors.New("edwards25519: invalid point encoding length")}
	}

	// -x² + y² = 1 + dx²y²
	// x² + dx²y² = x²(dy² + 1) = y² - 1
	// x² = (y² - 1) / (dy² + 1)

	// u = y² - 1
	y2 := new(field.Element).Square(y)
	u := new(field.Element).Subtract(y2, feOne)

	// v = dy² + 1
	vv := new(field.Element).Multiply(y2, d)
	vv = vv.Add(vv, feOne)

	// x = +√(u/v)
	xx, wasSquare := new(field.Element).SqrtRatio(u, vv)
	if wasSquare == 0 {
		return __mrPoint_SetBytesPair{nil, errors.New("edwards25519: invalid point encoding")}
	}

	// Select the negative square root if the sign bit is set.
	xxNeg := new(field.Element).Negate(xx)
	xx = xx.Select(xxNeg, xx, int(x[31]>>7))

	v.x.Set(xx)
	v.y.Set(y)
	v.z.One()
	v.t.Multiply(xx, y) // xy = T / Z

	return __mrPoint_SetBytesPair{v, nil}
}

func (v *Point) SetBytes(x []byte) (*Point, error) {
	panic("crypto/internal/fips140/edwards25519: SetBytes: multi-result desugar")
}

func copyFieldElement(buf *[32]byte, v *field.Element) []byte {
	copy(buf[:], v.Bytes())
	return buf[:]
}

// Conversions.

func (v *projP2) FromP1xP1(p *projP1xP1) *projP2 {
	v.X.Multiply(&p.X, &p.T)
	v.Y.Multiply(&p.Y, &p.Z)
	v.Z.Multiply(&p.Z, &p.T)
	return v
}

func (v *projP2) FromP3(p *Point) *projP2 {
	v.X.Set(&p.x)
	v.Y.Set(&p.y)
	v.Z.Set(&p.z)
	return v
}

func (v *Point) fromP1xP1(p *projP1xP1) *Point {
	v.x.Multiply(&p.X, &p.T)
	v.y.Multiply(&p.Y, &p.Z)
	v.z.Multiply(&p.Z, &p.T)
	v.t.Multiply(&p.X, &p.Y)
	return v
}

func (v *Point) fromP2(p *projP2) *Point {
	v.x.Multiply(&p.X, &p.Z)
	v.y.Multiply(&p.Y, &p.Z)
	v.z.Square(&p.Z)
	v.t.Multiply(&p.X, &p.Y)
	return v
}

// d is a constant in the curve equation.
var d, _ = new(field.Element).SetBytes([]byte{
	0xa3, 0x78, 0x59, 0x13, 0xca, 0x4d, 0xeb, 0x75,
	0xab, 0xd8, 0x41, 0x41, 0x4d, 0x0a, 0x70, 0x00,
	0x98, 0xe8, 0x79, 0x77, 0x79, 0x40, 0xc7, 0x8c,
	0x73, 0xfe, 0x6f, 0x2b, 0xee, 0x6c, 0x03, 0x52})
var d2 = new(field.Element).Add(d, d)

func (v *projCached) FromP3(p *Point) *projCached {
	v.YplusX.Add(&p.y, &p.x)
	v.YminusX.Subtract(&p.y, &p.x)
	v.Z.Set(&p.z)
	v.T2d.Multiply(&p.t, d2)
	return v
}

func (v *affineCached) FromP3(p *Point) *affineCached {
	v.YplusX.Add(&p.y, &p.x)
	v.YminusX.Subtract(&p.y, &p.x)
	v.T2d.Multiply(&p.t, d2)

	var invZ field.Element
	invZ.Invert(&p.z)
	v.YplusX.Multiply(&v.YplusX, &invZ)
	v.YminusX.Multiply(&v.YminusX, &invZ)
	v.T2d.Multiply(&v.T2d, &invZ)
	return v
}

// (Re)addition and subtraction.

// Add sets v = p + q, and returns v.
func (v *Point) Add(p, q *Point) *Point {
	checkInitialized(p, q)
	qCached := new(projCached).FromP3(q)
	result := new(projP1xP1).Add(p, qCached)
	return v.fromP1xP1(result)
}

// Subtract sets v = p - q, and returns v.
func (v *Point) Subtract(p, q *Point) *Point {
	checkInitialized(p, q)
	qCached := new(projCached).FromP3(q)
	result := new(projP1xP1).Sub(p, qCached)
	return v.fromP1xP1(result)
}

func (v *projP1xP1) Add(p *Point, q *projCached) *projP1xP1 {
	var YplusX, YminusX, PP, MM, TT2d, ZZ2 field.Element

	YplusX.Add(&p.y, &p.x)
	YminusX.Subtract(&p.y, &p.x)

	PP.Multiply(&YplusX, &q.YplusX)
	MM.Multiply(&YminusX, &q.YminusX)
	TT2d.Multiply(&p.t, &q.T2d)
	ZZ2.Multiply(&p.z, &q.Z)

	ZZ2.Add(&ZZ2, &ZZ2)

	v.X.Subtract(&PP, &MM)
	v.Y.Add(&PP, &MM)
	v.Z.Add(&ZZ2, &TT2d)
	v.T.Subtract(&ZZ2, &TT2d)
	return v
}

func (v *projP1xP1) Sub(p *Point, q *projCached) *projP1xP1 {
	var YplusX, YminusX, PP, MM, TT2d, ZZ2 field.Element

	YplusX.Add(&p.y, &p.x)
	YminusX.Subtract(&p.y, &p.x)

	PP.Multiply(&YplusX, &q.YminusX) // flipped sign
	MM.Multiply(&YminusX, &q.YplusX) // flipped sign
	TT2d.Multiply(&p.t, &q.T2d)
	ZZ2.Multiply(&p.z, &q.Z)

	ZZ2.Add(&ZZ2, &ZZ2)

	v.X.Subtract(&PP, &MM)
	v.Y.Add(&PP, &MM)
	v.Z.Subtract(&ZZ2, &TT2d) // flipped sign
	v.T.Add(&ZZ2, &TT2d)      // flipped sign
	return v
}

func (v *projP1xP1) AddAffine(p *Point, q *affineCached) *projP1xP1 {
	var YplusX, YminusX, PP, MM, TT2d, Z2 field.Element

	YplusX.Add(&p.y, &p.x)
	YminusX.Subtract(&p.y, &p.x)

	PP.Multiply(&YplusX, &q.YplusX)
	MM.Multiply(&YminusX, &q.YminusX)
	TT2d.Multiply(&p.t, &q.T2d)

	Z2.Add(&p.z, &p.z)

	v.X.Subtract(&PP, &MM)
	v.Y.Add(&PP, &MM)
	v.Z.Add(&Z2, &TT2d)
	v.T.Subtract(&Z2, &TT2d)
	return v
}

func (v *projP1xP1) SubAffine(p *Point, q *affineCached) *projP1xP1 {
	var YplusX, YminusX, PP, MM, TT2d, Z2 field.Element

	YplusX.Add(&p.y, &p.x)
	YminusX.Subtract(&p.y, &p.x)

	PP.Multiply(&YplusX, &q.YminusX) // flipped sign
	MM.Multiply(&YminusX, &q.YplusX) // flipped sign
	TT2d.Multiply(&p.t, &q.T2d)

	Z2.Add(&p.z, &p.z)

	v.X.Subtract(&PP, &MM)
	v.Y.Add(&PP, &MM)
	v.Z.Subtract(&Z2, &TT2d) // flipped sign
	v.T.Add(&Z2, &TT2d)      // flipped sign
	return v
}

// Doubling.

func (v *projP1xP1) Double(p *projP2) *projP1xP1 {
	var XX, YY, ZZ2, XplusYsq field.Element

	XX.Square(&p.X)
	YY.Square(&p.Y)
	ZZ2.Square(&p.Z)
	ZZ2.Add(&ZZ2, &ZZ2)
	XplusYsq.Add(&p.X, &p.Y)
	XplusYsq.Square(&XplusYsq)

	v.Y.Add(&YY, &XX)
	v.Z.Subtract(&YY, &XX)

	v.X.Subtract(&XplusYsq, &v.Y)
	v.T.Subtract(&ZZ2, &v.Z)
	return v
}

// Negation.

// Negate sets v = -p, and returns v.
func (v *Point) Negate(p *Point) *Point {
	checkInitialized(p)
	v.x.Negate(&p.x)
	v.y.Set(&p.y)
	v.z.Set(&p.z)
	v.t.Negate(&p.t)
	return v
}

// Equal returns 1 if v is equivalent to u, and 0 otherwise.
func (v *Point) Equal(u *Point) int {
	checkInitialized(v, u)

	var t1, t2, t3, t4 field.Element
	t1.Multiply(&v.x, &u.z)
	t2.Multiply(&u.x, &v.z)
	t3.Multiply(&v.y, &u.z)
	t4.Multiply(&u.y, &v.z)

	return t1.Equal(&t2) & t3.Equal(&t4)
}

// Constant-time operations

// Select sets v to a if cond == 1 and to b if cond == 0.
func (v *projCached) Select(a, b *projCached, cond int) *projCached {
	v.YplusX.Select(&a.YplusX, &b.YplusX, cond)
	v.YminusX.Select(&a.YminusX, &b.YminusX, cond)
	v.Z.Select(&a.Z, &b.Z, cond)
	v.T2d.Select(&a.T2d, &b.T2d, cond)
	return v
}

// Select sets v to a if cond == 1 and to b if cond == 0.
func (v *affineCached) Select(a, b *affineCached, cond int) *affineCached {
	v.YplusX.Select(&a.YplusX, &b.YplusX, cond)
	v.YminusX.Select(&a.YminusX, &b.YminusX, cond)
	v.T2d.Select(&a.T2d, &b.T2d, cond)
	return v
}

// CondNeg negates v if cond == 1 and leaves it unchanged if cond == 0.
func (v *projCached) CondNeg(cond int) *projCached {
	v.YplusX.Swap(&v.YminusX, cond)
	v.T2d.Select(new(field.Element).Negate(&v.T2d), &v.T2d, cond)
	return v
}

// CondNeg negates v if cond == 1 and leaves it unchanged if cond == 0.
func (v *affineCached) CondNeg(cond int) *affineCached {
	v.YplusX.Swap(&v.YminusX, cond)
	v.T2d.Select(new(field.Element).Negate(&v.T2d), &v.T2d, cond)
	return v
}


// ---- vendored from crypto/internal/fips140/edwards25519/scalar.go ----




// A Scalar is an integer modulo
//
//	l = 2^252 + 27742317777372353535851937790883648493
//
// which is the prime order of the edwards25519 group.
//
// This type works similarly to math/big.Int, and all arguments and
// receivers are allowed to alias.
//
// The zero value is a valid zero element.
type Scalar struct {
	// s is the scalar in the Montgomery domain, in the format of the
	// fiat-crypto implementation.
	s fiatScalarMontgomeryDomainFieldElement
}

// The field implementation in scalar_fiat.go is generated by the fiat-crypto
// project (https://github.com/mit-plv/fiat-crypto) at version v0.0.9 (23d2dbc)
// from a formally verified model.
//
// fiat-crypto code comes under the following license.
//
//     Copyright (c) 2015-2020 The fiat-crypto Authors. All rights reserved.
//
//     Redistribution and use in source and binary forms, with or without
//     modification, are permitted provided that the following conditions are
//     met:
//
//         1. Redistributions of source code must retain the above copyright
//         notice, this list of conditions and the following disclaimer.
//
//     THIS SOFTWARE IS PROVIDED BY the fiat-crypto authors "AS IS"
//     AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO,
//     THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR
//     PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL Berkeley Software Design,
//     Inc. BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL,
//     EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO,
//     PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR
//     PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF
//     LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING
//     NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
//     SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
//

// NewScalar returns a new zero Scalar.
func NewScalar() *Scalar {
	return &Scalar{}
}

// MultiplyAdd sets s = x * y + z mod l, and returns s. It is equivalent to
// using Multiply and then Add.
func (s *Scalar) MultiplyAdd(x, y, z *Scalar) *Scalar {
	// Make a copy of z in case it aliases s.
	zCopy := new(Scalar).Set(z)
	return s.Multiply(x, y).Add(s, zCopy)
}

// Add sets s = x + y mod l, and returns s.
func (s *Scalar) Add(x, y *Scalar) *Scalar {
	// s = 1 * x + y mod l
	fiatScalarAdd(&s.s, &x.s, &y.s)
	return s
}

// Subtract sets s = x - y mod l, and returns s.
func (s *Scalar) Subtract(x, y *Scalar) *Scalar {
	// s = -1 * y + x mod l
	fiatScalarSub(&s.s, &x.s, &y.s)
	return s
}

// Negate sets s = -x mod l, and returns s.
func (s *Scalar) Negate(x *Scalar) *Scalar {
	// s = -1 * x + 0 mod l
	fiatScalarOpp(&s.s, &x.s)
	return s
}

// Multiply sets s = x * y mod l, and returns s.
func (s *Scalar) Multiply(x, y *Scalar) *Scalar {
	// s = x * y + 0 mod l
	fiatScalarMul(&s.s, &x.s, &y.s)
	return s
}

// Set sets s = x, and returns s.
func (s *Scalar) Set(x *Scalar) *Scalar {
	*s = *x
	return s
}

// SetUniformBytes sets s = x mod l, where x is a 64-byte little-endian integer.
// If x is not of the right length, SetUniformBytes returns nil and an error,
// and the receiver is unchanged.
//
// SetUniformBytes can be used to set s to a uniformly distributed value given
// 64 uniformly distributed random bytes.
type __mrScalar_SetUniformBytesPair struct {
	v0 *Scalar
	v1 error
}

func __mrScalar_SetUniformBytes(s *Scalar, x []byte) __mrScalar_SetUniformBytesPair {
	if len(x) != 64 {
		return __mrScalar_SetUniformBytesPair{nil, errors.New("edwards25519: invalid SetUniformBytes input length")}
	}

	// We have a value x of 512 bits, but our fiatScalarFromBytes function
	// expects an input lower than l, which is a little over 252 bits.
	//
	// Instead of writing a reduction function that operates on wider inputs, we
	// can interpret x as the sum of three shorter values a, b, and c.
	//
	//    x = a + b * 2^168 + c * 2^336  mod l
	//
	// We then precompute 2^168 and 2^336 modulo l, and perform the reduction
	// with two multiplications and two additions.

	s.setShortBytes(x[:21])
	t := new(Scalar).setShortBytes(x[21:42])
	s.Add(s, t.Multiply(t, scalarTwo168))
	t.setShortBytes(x[42:])
	s.Add(s, t.Multiply(t, scalarTwo336))

	return __mrScalar_SetUniformBytesPair{s, nil}
}

func (s *Scalar) SetUniformBytes(x []byte) (*Scalar, error) {
	panic("crypto/internal/fips140/edwards25519: SetUniformBytes: multi-result desugar")
}

// scalarTwo168 and scalarTwo336 are 2^168 and 2^336 modulo l, encoded as a
// fiatScalarMontgomeryDomainFieldElement, which is a little-endian 4-limb value
// in the 2^256 Montgomery domain.
var scalarTwo168 = &Scalar{s: [4]uint64{0x5b8ab432eac74798, 0x38afddd6de59d5d7,
	0xa2c131b399411b7c, 0x6329a7ed9ce5a30}}
var scalarTwo336 = &Scalar{s: [4]uint64{0xbd3d108e2b35ecc5, 0x5c3a3718bdf9c90b,
	0x63aa97a331b4f2ee, 0x3d217f5be65cb5c}}

// setShortBytes sets s = x mod l, where x is a little-endian integer shorter
// than 32 bytes.
func (s *Scalar) setShortBytes(x []byte) *Scalar {
	if len(x) >= 32 {
		panic("edwards25519: internal error: setShortBytes called with a long string")
	}
	var buf [32]byte
	copy(buf[:], x)
	fiatScalarFromBytes((*[4]uint64)(&s.s), &buf)
	fiatScalarToMontgomery(&s.s, (*fiatScalarNonMontgomeryDomainFieldElement)(&s.s))
	return s
}

// SetCanonicalBytes sets s = x, where x is a 32-byte little-endian encoding of
// s, and returns s. If x is not a canonical encoding of s, SetCanonicalBytes
// returns nil and an error, and the receiver is unchanged.
type __mrScalar_SetCanonicalBytesPair struct {
	v0 *Scalar
	v1 error
}

func __mrScalar_SetCanonicalBytes(s *Scalar, x []byte) __mrScalar_SetCanonicalBytesPair {
	if len(x) != 32 {
		return __mrScalar_SetCanonicalBytesPair{nil, errors.New("invalid scalar length")}
	}
	if !isReduced(x) {
		return __mrScalar_SetCanonicalBytesPair{nil, errors.New("invalid scalar encoding")}
	}

	fiatScalarFromBytes((*[4]uint64)(&s.s), (*[32]byte)(x))
	fiatScalarToMontgomery(&s.s, (*fiatScalarNonMontgomeryDomainFieldElement)(&s.s))

	return __mrScalar_SetCanonicalBytesPair{s, nil}
}

func (s *Scalar) SetCanonicalBytes(x []byte) (*Scalar, error) {
	panic("crypto/internal/fips140/edwards25519: SetCanonicalBytes: multi-result desugar")
}

// scalarMinusOneBytes is l - 1 in little endian.
var scalarMinusOneBytes = [32]byte{236, 211, 245, 92, 26, 99, 18, 88, 214, 156, 247, 162, 222, 249, 222, 20, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 16}

// isReduced returns whether the given scalar in 32-byte little endian encoded
// form is reduced modulo l.
func isReduced(s []byte) bool {
	if len(s) != 32 {
		return false
	}

	s0 := byteorder.LEUint64(s[:8])
	s1 := byteorder.LEUint64(s[8:16])
	s2 := byteorder.LEUint64(s[16:24])
	s3 := byteorder.LEUint64(s[24:])

	l0 := byteorder.LEUint64(scalarMinusOneBytes[:8])
	l1 := byteorder.LEUint64(scalarMinusOneBytes[8:16])
	l2 := byteorder.LEUint64(scalarMinusOneBytes[16:24])
	l3 := byteorder.LEUint64(scalarMinusOneBytes[24:])

	// Do a constant time subtraction chain scalarMinusOneBytes - s. If there is
	// a borrow at the end, then s > scalarMinusOneBytes.
	_, b := bits.Sub64(l0, s0, 0)
	_, b = bits.Sub64(l1, s1, b)
	_, b = bits.Sub64(l2, s2, b)
	_, b = bits.Sub64(l3, s3, b)
	return b == 0
}

// SetBytesWithClamping applies the buffer pruning described in RFC 8032,
// Section 5.1.5 (also known as clamping) and sets s to the result. The input
// must be 32 bytes, and it is not modified. If x is not of the right length,
// SetBytesWithClamping returns nil and an error, and the receiver is unchanged.
//
// Note that since Scalar values are always reduced modulo the prime order of
// the curve, the resulting value will not preserve any of the cofactor-clearing
// properties that clamping is meant to provide. It will however work as
// expected as long as it is applied to points on the prime order subgroup, like
// in Ed25519. In fact, it is lost to history why RFC 8032 adopted the
// irrelevant RFC 7748 clamping, but it is now required for compatibility.
type __mrScalar_SetBytesWithClampingPair struct {
	v0 *Scalar
	v1 error
}

func __mrScalar_SetBytesWithClamping(s *Scalar, x []byte) __mrScalar_SetBytesWithClampingPair {
	// The description above omits the purpose of the high bits of the clamping
	// for brevity, but those are also lost to reductions, and are also
	// irrelevant to edwards25519 as they protect against a specific
	// implementation bug that was once observed in a generic Montgomery ladder.
	if len(x) != 32 {
		return __mrScalar_SetBytesWithClampingPair{nil, errors.New("edwards25519: invalid SetBytesWithClamping input length")}
	}

	// We need to use the wide reduction from SetUniformBytes, since clamping
	// sets the 2^254 bit, making the value higher than the order.
	var wideBytes [64]byte
	copy(wideBytes[:], x[:])
	wideBytes[0] &= 248
	wideBytes[31] &= 63
	wideBytes[31] |= 64
	__fw0, __fw1 := s.SetUniformBytes(wideBytes[:])
	return __mrScalar_SetBytesWithClampingPair{__fw0, __fw1}
}

func (s *Scalar) SetBytesWithClamping(x []byte) (*Scalar, error) {
	panic("crypto/internal/fips140/edwards25519: SetBytesWithClamping: multi-result desugar")
}

// Bytes returns the canonical 32-byte little-endian encoding of s.
func (s *Scalar) Bytes() []byte {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var encoded [32]byte
	return s.bytes(&encoded)
}

func (s *Scalar) bytes(out *[32]byte) []byte {
	var ss fiatScalarNonMontgomeryDomainFieldElement
	fiatScalarFromMontgomery(&ss, &s.s)
	fiatScalarToBytes(out, (*[4]uint64)(&ss))
	return out[:]
}

// Equal returns 1 if s and t are equal, and 0 otherwise.
func (s *Scalar) Equal(t *Scalar) int {
	var diff fiatScalarMontgomeryDomainFieldElement
	fiatScalarSub(&diff, &s.s, &t.s)
	var nonzero uint64
	fiatScalarNonzero(&nonzero, (*[4]uint64)(&diff))
	nonzero |= nonzero >> 32
	nonzero |= nonzero >> 16
	nonzero |= nonzero >> 8
	nonzero |= nonzero >> 4
	nonzero |= nonzero >> 2
	nonzero |= nonzero >> 1
	return int(^nonzero) & 1
}

// nonAdjacentForm computes a width-w non-adjacent form for this scalar.
//
// w must be between 2 and 8, or nonAdjacentForm will panic.
func (s *Scalar) nonAdjacentForm(w uint) [256]int8 {
	// This implementation is adapted from the one
	// in curve25519-dalek and is documented there:
	// https://github.com/dalek-cryptography/curve25519-dalek/blob/f630041af28e9a405255f98a8a93adca18e4315b/src/scalar.rs#L800-L871
	b := s.Bytes()
	if b[31] > 127 {
		panic("scalar has high bit set illegally")
	}
	if w < 2 {
		panic("w must be at least 2 by the definition of NAF")
	} else if w > 8 {
		panic("NAF digits must fit in int8")
	}

	var naf [256]int8
	var digits [5]uint64

	for i := 0; i < 4; i++ {
		digits[i] = byteorder.LEUint64(b[i*8:])
	}

	width := uint64(1 << w)
	windowMask := uint64(width - 1)

	pos := uint(0)
	carry := uint64(0)
	for pos < 256 {
		indexU64 := pos / 64
		indexBit := pos % 64
		var bitBuf uint64
		if indexBit < 64-w {
			// This window's bits are contained in a single u64
			bitBuf = digits[indexU64] >> indexBit
		} else {
			// Combine the current 64 bits with bits from the next 64
			bitBuf = (digits[indexU64] >> indexBit) | (digits[1+indexU64] << (64 - indexBit))
		}

		// Add carry into the current window
		window := carry + (bitBuf & windowMask)

		if window&1 == 0 {
			// If the window value is even, preserve the carry and continue.
			// Why is the carry preserved?
			// If carry == 0 and window & 1 == 0,
			//    then the next carry should be 0
			// If carry == 1 and window & 1 == 0,
			//    then bit_buf & 1 == 1 so the next carry should be 1
			pos += 1
			continue
		}

		if window < width/2 {
			carry = 0
			naf[pos] = int8(window)
		} else {
			carry = 1
			naf[pos] = int8(window) - int8(width)
		}

		pos += w
	}
	return naf
}

func (s *Scalar) signedRadix16() [64]int8 {
	b := s.Bytes()
	if b[31] > 127 {
		panic("scalar has high bit set illegally")
	}

	var digits [64]int8

	// Compute unsigned radix-16 digits:
	for i := 0; i < 32; i++ {
		digits[2*i] = int8(b[i] & 15)
		digits[2*i+1] = int8((b[i] >> 4) & 15)
	}

	// Recenter coefficients:
	for i := 0; i < 63; i++ {
		carry := (digits[i] + 8) >> 4
		digits[i] -= carry << 4
		digits[i+1] += carry
	}

	return digits
}


// ---- vendored from crypto/internal/fips140/edwards25519/scalar_fiat.go ----



type fiatScalarUint1 uint64 // We use uint64 instead of a more narrow type for performance reasons; see https://github.com/mit-plv/fiat-crypto/pull/1006#issuecomment-892625927
type fiatScalarInt1 int64   // We use uint64 instead of a more narrow type for performance reasons; see https://github.com/mit-plv/fiat-crypto/pull/1006#issuecomment-892625927

// The type fiatScalarMontgomeryDomainFieldElement is a field element in the Montgomery domain.
//
// Bounds: [[0x0 ~> 0xffffffffffffffff], [0x0 ~> 0xffffffffffffffff], [0x0 ~> 0xffffffffffffffff], [0x0 ~> 0xffffffffffffffff]]
type fiatScalarMontgomeryDomainFieldElement [4]uint64

// The type fiatScalarNonMontgomeryDomainFieldElement is a field element NOT in the Montgomery domain.
//
// Bounds: [[0x0 ~> 0xffffffffffffffff], [0x0 ~> 0xffffffffffffffff], [0x0 ~> 0xffffffffffffffff], [0x0 ~> 0xffffffffffffffff]]
type fiatScalarNonMontgomeryDomainFieldElement [4]uint64

// fiatScalarCmovznzU64 is a single-word conditional move.
//
// Postconditions:
//
//	out1 = (if arg1 = 0 then arg2 else arg3)
//
// Input Bounds:
//
//	arg1: [0x0 ~> 0x1]
//	arg2: [0x0 ~> 0xffffffffffffffff]
//	arg3: [0x0 ~> 0xffffffffffffffff]
//
// Output Bounds:
//
//	out1: [0x0 ~> 0xffffffffffffffff]
func fiatScalarCmovznzU64(out1 *uint64, arg1 fiatScalarUint1, arg2 uint64, arg3 uint64) {
	x1 := (uint64(arg1) * 0xffffffffffffffff)
	x2 := ((x1 & arg3) | ((^x1) & arg2))
	*out1 = x2
}

// fiatScalarMul multiplies two field elements in the Montgomery domain.
//
// Preconditions:
//
//	0 ≤ eval arg1 < m
//	0 ≤ eval arg2 < m
//
// Postconditions:
//
//	eval (from_montgomery out1) mod m = (eval (from_montgomery arg1) * eval (from_montgomery arg2)) mod m
//	0 ≤ eval out1 < m
func fiatScalarMul(out1 *fiatScalarMontgomeryDomainFieldElement, arg1 *fiatScalarMontgomeryDomainFieldElement, arg2 *fiatScalarMontgomeryDomainFieldElement) {
	x1 := arg1[1]
	x2 := arg1[2]
	x3 := arg1[3]
	x4 := arg1[0]
	var x5 uint64
	var x6 uint64
	x6, x5 = bits.Mul64(x4, arg2[3])
	var x7 uint64
	var x8 uint64
	x8, x7 = bits.Mul64(x4, arg2[2])
	var x9 uint64
	var x10 uint64
	x10, x9 = bits.Mul64(x4, arg2[1])
	var x11 uint64
	var x12 uint64
	x12, x11 = bits.Mul64(x4, arg2[0])
	var x13 uint64
	var x14 uint64
	x13, x14 = bits.Add64(x12, x9, uint64(0x0))
	var x15 uint64
	var x16 uint64
	x15, x16 = bits.Add64(x10, x7, uint64(fiatScalarUint1(x14)))
	var x17 uint64
	var x18 uint64
	x17, x18 = bits.Add64(x8, x5, uint64(fiatScalarUint1(x16)))
	x19 := (uint64(fiatScalarUint1(x18)) + x6)
	var x20 uint64
	_, x20 = bits.Mul64(x11, 0xd2b51da312547e1b)
	var x22 uint64
	var x23 uint64
	x23, x22 = bits.Mul64(x20, 0x1000000000000000)
	var x24 uint64
	var x25 uint64
	x25, x24 = bits.Mul64(x20, 0x14def9dea2f79cd6)
	var x26 uint64
	var x27 uint64
	x27, x26 = bits.Mul64(x20, 0x5812631a5cf5d3ed)
	var x28 uint64
	var x29 uint64
	x28, x29 = bits.Add64(x27, x24, uint64(0x0))
	x30 := (uint64(fiatScalarUint1(x29)) + x25)
	var x32 uint64
	_, x32 = bits.Add64(x11, x26, uint64(0x0))
	var x33 uint64
	var x34 uint64
	x33, x34 = bits.Add64(x13, x28, uint64(fiatScalarUint1(x32)))
	var x35 uint64
	var x36 uint64
	x35, x36 = bits.Add64(x15, x30, uint64(fiatScalarUint1(x34)))
	var x37 uint64
	var x38 uint64
	x37, x38 = bits.Add64(x17, x22, uint64(fiatScalarUint1(x36)))
	var x39 uint64
	var x40 uint64
	x39, x40 = bits.Add64(x19, x23, uint64(fiatScalarUint1(x38)))
	var x41 uint64
	var x42 uint64
	x42, x41 = bits.Mul64(x1, arg2[3])
	var x43 uint64
	var x44 uint64
	x44, x43 = bits.Mul64(x1, arg2[2])
	var x45 uint64
	var x46 uint64
	x46, x45 = bits.Mul64(x1, arg2[1])
	var x47 uint64
	var x48 uint64
	x48, x47 = bits.Mul64(x1, arg2[0])
	var x49 uint64
	var x50 uint64
	x49, x50 = bits.Add64(x48, x45, uint64(0x0))
	var x51 uint64
	var x52 uint64
	x51, x52 = bits.Add64(x46, x43, uint64(fiatScalarUint1(x50)))
	var x53 uint64
	var x54 uint64
	x53, x54 = bits.Add64(x44, x41, uint64(fiatScalarUint1(x52)))
	x55 := (uint64(fiatScalarUint1(x54)) + x42)
	var x56 uint64
	var x57 uint64
	x56, x57 = bits.Add64(x33, x47, uint64(0x0))
	var x58 uint64
	var x59 uint64
	x58, x59 = bits.Add64(x35, x49, uint64(fiatScalarUint1(x57)))
	var x60 uint64
	var x61 uint64
	x60, x61 = bits.Add64(x37, x51, uint64(fiatScalarUint1(x59)))
	var x62 uint64
	var x63 uint64
	x62, x63 = bits.Add64(x39, x53, uint64(fiatScalarUint1(x61)))
	var x64 uint64
	var x65 uint64
	x64, x65 = bits.Add64(uint64(fiatScalarUint1(x40)), x55, uint64(fiatScalarUint1(x63)))
	var x66 uint64
	_, x66 = bits.Mul64(x56, 0xd2b51da312547e1b)
	var x68 uint64
	var x69 uint64
	x69, x68 = bits.Mul64(x66, 0x1000000000000000)
	var x70 uint64
	var x71 uint64
	x71, x70 = bits.Mul64(x66, 0x14def9dea2f79cd6)
	var x72 uint64
	var x73 uint64
	x73, x72 = bits.Mul64(x66, 0x5812631a5cf5d3ed)
	var x74 uint64
	var x75 uint64
	x74, x75 = bits.Add64(x73, x70, uint64(0x0))
	x76 := (uint64(fiatScalarUint1(x75)) + x71)
	var x78 uint64
	_, x78 = bits.Add64(x56, x72, uint64(0x0))
	var x79 uint64
	var x80 uint64
	x79, x80 = bits.Add64(x58, x74, uint64(fiatScalarUint1(x78)))
	var x81 uint64
	var x82 uint64
	x81, x82 = bits.Add64(x60, x76, uint64(fiatScalarUint1(x80)))
	var x83 uint64
	var x84 uint64
	x83, x84 = bits.Add64(x62, x68, uint64(fiatScalarUint1(x82)))
	var x85 uint64
	var x86 uint64
	x85, x86 = bits.Add64(x64, x69, uint64(fiatScalarUint1(x84)))
	x87 := (uint64(fiatScalarUint1(x86)) + uint64(fiatScalarUint1(x65)))
	var x88 uint64
	var x89 uint64
	x89, x88 = bits.Mul64(x2, arg2[3])
	var x90 uint64
	var x91 uint64
	x91, x90 = bits.Mul64(x2, arg2[2])
	var x92 uint64
	var x93 uint64
	x93, x92 = bits.Mul64(x2, arg2[1])
	var x94 uint64
	var x95 uint64
	x95, x94 = bits.Mul64(x2, arg2[0])
	var x96 uint64
	var x97 uint64
	x96, x97 = bits.Add64(x95, x92, uint64(0x0))
	var x98 uint64
	var x99 uint64
	x98, x99 = bits.Add64(x93, x90, uint64(fiatScalarUint1(x97)))
	var x100 uint64
	var x101 uint64
	x100, x101 = bits.Add64(x91, x88, uint64(fiatScalarUint1(x99)))
	x102 := (uint64(fiatScalarUint1(x101)) + x89)
	var x103 uint64
	var x104 uint64
	x103, x104 = bits.Add64(x79, x94, uint64(0x0))
	var x105 uint64
	var x106 uint64
	x105, x106 = bits.Add64(x81, x96, uint64(fiatScalarUint1(x104)))
	var x107 uint64
	var x108 uint64
	x107, x108 = bits.Add64(x83, x98, uint64(fiatScalarUint1(x106)))
	var x109 uint64
	var x110 uint64
	x109, x110 = bits.Add64(x85, x100, uint64(fiatScalarUint1(x108)))
	var x111 uint64
	var x112 uint64
	x111, x112 = bits.Add64(x87, x102, uint64(fiatScalarUint1(x110)))
	var x113 uint64
	_, x113 = bits.Mul64(x103, 0xd2b51da312547e1b)
	var x115 uint64
	var x116 uint64
	x116, x115 = bits.Mul64(x113, 0x1000000000000000)
	var x117 uint64
	var x118 uint64
	x118, x117 = bits.Mul64(x113, 0x14def9dea2f79cd6)
	var x119 uint64
	var x120 uint64
	x120, x119 = bits.Mul64(x113, 0x5812631a5cf5d3ed)
	var x121 uint64
	var x122 uint64
	x121, x122 = bits.Add64(x120, x117, uint64(0x0))
	x123 := (uint64(fiatScalarUint1(x122)) + x118)
	var x125 uint64
	_, x125 = bits.Add64(x103, x119, uint64(0x0))
	var x126 uint64
	var x127 uint64
	x126, x127 = bits.Add64(x105, x121, uint64(fiatScalarUint1(x125)))
	var x128 uint64
	var x129 uint64
	x128, x129 = bits.Add64(x107, x123, uint64(fiatScalarUint1(x127)))
	var x130 uint64
	var x131 uint64
	x130, x131 = bits.Add64(x109, x115, uint64(fiatScalarUint1(x129)))
	var x132 uint64
	var x133 uint64
	x132, x133 = bits.Add64(x111, x116, uint64(fiatScalarUint1(x131)))
	x134 := (uint64(fiatScalarUint1(x133)) + uint64(fiatScalarUint1(x112)))
	var x135 uint64
	var x136 uint64
	x136, x135 = bits.Mul64(x3, arg2[3])
	var x137 uint64
	var x138 uint64
	x138, x137 = bits.Mul64(x3, arg2[2])
	var x139 uint64
	var x140 uint64
	x140, x139 = bits.Mul64(x3, arg2[1])
	var x141 uint64
	var x142 uint64
	x142, x141 = bits.Mul64(x3, arg2[0])
	var x143 uint64
	var x144 uint64
	x143, x144 = bits.Add64(x142, x139, uint64(0x0))
	var x145 uint64
	var x146 uint64
	x145, x146 = bits.Add64(x140, x137, uint64(fiatScalarUint1(x144)))
	var x147 uint64
	var x148 uint64
	x147, x148 = bits.Add64(x138, x135, uint64(fiatScalarUint1(x146)))
	x149 := (uint64(fiatScalarUint1(x148)) + x136)
	var x150 uint64
	var x151 uint64
	x150, x151 = bits.Add64(x126, x141, uint64(0x0))
	var x152 uint64
	var x153 uint64
	x152, x153 = bits.Add64(x128, x143, uint64(fiatScalarUint1(x151)))
	var x154 uint64
	var x155 uint64
	x154, x155 = bits.Add64(x130, x145, uint64(fiatScalarUint1(x153)))
	var x156 uint64
	var x157 uint64
	x156, x157 = bits.Add64(x132, x147, uint64(fiatScalarUint1(x155)))
	var x158 uint64
	var x159 uint64
	x158, x159 = bits.Add64(x134, x149, uint64(fiatScalarUint1(x157)))
	var x160 uint64
	_, x160 = bits.Mul64(x150, 0xd2b51da312547e1b)
	var x162 uint64
	var x163 uint64
	x163, x162 = bits.Mul64(x160, 0x1000000000000000)
	var x164 uint64
	var x165 uint64
	x165, x164 = bits.Mul64(x160, 0x14def9dea2f79cd6)
	var x166 uint64
	var x167 uint64
	x167, x166 = bits.Mul64(x160, 0x5812631a5cf5d3ed)
	var x168 uint64
	var x169 uint64
	x168, x169 = bits.Add64(x167, x164, uint64(0x0))
	x170 := (uint64(fiatScalarUint1(x169)) + x165)
	var x172 uint64
	_, x172 = bits.Add64(x150, x166, uint64(0x0))
	var x173 uint64
	var x174 uint64
	x173, x174 = bits.Add64(x152, x168, uint64(fiatScalarUint1(x172)))
	var x175 uint64
	var x176 uint64
	x175, x176 = bits.Add64(x154, x170, uint64(fiatScalarUint1(x174)))
	var x177 uint64
	var x178 uint64
	x177, x178 = bits.Add64(x156, x162, uint64(fiatScalarUint1(x176)))
	var x179 uint64
	var x180 uint64
	x179, x180 = bits.Add64(x158, x163, uint64(fiatScalarUint1(x178)))
	x181 := (uint64(fiatScalarUint1(x180)) + uint64(fiatScalarUint1(x159)))
	var x182 uint64
	var x183 uint64
	x182, x183 = bits.Sub64(x173, 0x5812631a5cf5d3ed, uint64(0x0))
	var x184 uint64
	var x185 uint64
	x184, x185 = bits.Sub64(x175, 0x14def9dea2f79cd6, uint64(fiatScalarUint1(x183)))
	var x186 uint64
	var x187 uint64
	x186, x187 = bits.Sub64(x177, uint64(0x0), uint64(fiatScalarUint1(x185)))
	var x188 uint64
	var x189 uint64
	x188, x189 = bits.Sub64(x179, 0x1000000000000000, uint64(fiatScalarUint1(x187)))
	var x191 uint64
	_, x191 = bits.Sub64(x181, uint64(0x0), uint64(fiatScalarUint1(x189)))
	var x192 uint64
	fiatScalarCmovznzU64(&x192, fiatScalarUint1(x191), x182, x173)
	var x193 uint64
	fiatScalarCmovznzU64(&x193, fiatScalarUint1(x191), x184, x175)
	var x194 uint64
	fiatScalarCmovznzU64(&x194, fiatScalarUint1(x191), x186, x177)
	var x195 uint64
	fiatScalarCmovznzU64(&x195, fiatScalarUint1(x191), x188, x179)
	out1[0] = x192
	out1[1] = x193
	out1[2] = x194
	out1[3] = x195
}

// fiatScalarAdd adds two field elements in the Montgomery domain.
//
// Preconditions:
//
//	0 ≤ eval arg1 < m
//	0 ≤ eval arg2 < m
//
// Postconditions:
//
//	eval (from_montgomery out1) mod m = (eval (from_montgomery arg1) + eval (from_montgomery arg2)) mod m
//	0 ≤ eval out1 < m
func fiatScalarAdd(out1 *fiatScalarMontgomeryDomainFieldElement, arg1 *fiatScalarMontgomeryDomainFieldElement, arg2 *fiatScalarMontgomeryDomainFieldElement) {
	var x1 uint64
	var x2 uint64
	x1, x2 = bits.Add64(arg1[0], arg2[0], uint64(0x0))
	var x3 uint64
	var x4 uint64
	x3, x4 = bits.Add64(arg1[1], arg2[1], uint64(fiatScalarUint1(x2)))
	var x5 uint64
	var x6 uint64
	x5, x6 = bits.Add64(arg1[2], arg2[2], uint64(fiatScalarUint1(x4)))
	var x7 uint64
	var x8 uint64
	x7, x8 = bits.Add64(arg1[3], arg2[3], uint64(fiatScalarUint1(x6)))
	var x9 uint64
	var x10 uint64
	x9, x10 = bits.Sub64(x1, 0x5812631a5cf5d3ed, uint64(0x0))
	var x11 uint64
	var x12 uint64
	x11, x12 = bits.Sub64(x3, 0x14def9dea2f79cd6, uint64(fiatScalarUint1(x10)))
	var x13 uint64
	var x14 uint64
	x13, x14 = bits.Sub64(x5, uint64(0x0), uint64(fiatScalarUint1(x12)))
	var x15 uint64
	var x16 uint64
	x15, x16 = bits.Sub64(x7, 0x1000000000000000, uint64(fiatScalarUint1(x14)))
	var x18 uint64
	_, x18 = bits.Sub64(uint64(fiatScalarUint1(x8)), uint64(0x0), uint64(fiatScalarUint1(x16)))
	var x19 uint64
	fiatScalarCmovznzU64(&x19, fiatScalarUint1(x18), x9, x1)
	var x20 uint64
	fiatScalarCmovznzU64(&x20, fiatScalarUint1(x18), x11, x3)
	var x21 uint64
	fiatScalarCmovznzU64(&x21, fiatScalarUint1(x18), x13, x5)
	var x22 uint64
	fiatScalarCmovznzU64(&x22, fiatScalarUint1(x18), x15, x7)
	out1[0] = x19
	out1[1] = x20
	out1[2] = x21
	out1[3] = x22
}

// fiatScalarSub subtracts two field elements in the Montgomery domain.
//
// Preconditions:
//
//	0 ≤ eval arg1 < m
//	0 ≤ eval arg2 < m
//
// Postconditions:
//
//	eval (from_montgomery out1) mod m = (eval (from_montgomery arg1) - eval (from_montgomery arg2)) mod m
//	0 ≤ eval out1 < m
func fiatScalarSub(out1 *fiatScalarMontgomeryDomainFieldElement, arg1 *fiatScalarMontgomeryDomainFieldElement, arg2 *fiatScalarMontgomeryDomainFieldElement) {
	var x1 uint64
	var x2 uint64
	x1, x2 = bits.Sub64(arg1[0], arg2[0], uint64(0x0))
	var x3 uint64
	var x4 uint64
	x3, x4 = bits.Sub64(arg1[1], arg2[1], uint64(fiatScalarUint1(x2)))
	var x5 uint64
	var x6 uint64
	x5, x6 = bits.Sub64(arg1[2], arg2[2], uint64(fiatScalarUint1(x4)))
	var x7 uint64
	var x8 uint64
	x7, x8 = bits.Sub64(arg1[3], arg2[3], uint64(fiatScalarUint1(x6)))
	var x9 uint64
	fiatScalarCmovznzU64(&x9, fiatScalarUint1(x8), uint64(0x0), 0xffffffffffffffff)
	var x10 uint64
	var x11 uint64
	x10, x11 = bits.Add64(x1, (x9 & 0x5812631a5cf5d3ed), uint64(0x0))
	var x12 uint64
	var x13 uint64
	x12, x13 = bits.Add64(x3, (x9 & 0x14def9dea2f79cd6), uint64(fiatScalarUint1(x11)))
	var x14 uint64
	var x15 uint64
	x14, x15 = bits.Add64(x5, uint64(0x0), uint64(fiatScalarUint1(x13)))
	var x16 uint64
	x16, _ = bits.Add64(x7, (x9 & 0x1000000000000000), uint64(fiatScalarUint1(x15)))
	out1[0] = x10
	out1[1] = x12
	out1[2] = x14
	out1[3] = x16
}

// fiatScalarOpp negates a field element in the Montgomery domain.
//
// Preconditions:
//
//	0 ≤ eval arg1 < m
//
// Postconditions:
//
//	eval (from_montgomery out1) mod m = -eval (from_montgomery arg1) mod m
//	0 ≤ eval out1 < m
func fiatScalarOpp(out1 *fiatScalarMontgomeryDomainFieldElement, arg1 *fiatScalarMontgomeryDomainFieldElement) {
	var x1 uint64
	var x2 uint64
	x1, x2 = bits.Sub64(uint64(0x0), arg1[0], uint64(0x0))
	var x3 uint64
	var x4 uint64
	x3, x4 = bits.Sub64(uint64(0x0), arg1[1], uint64(fiatScalarUint1(x2)))
	var x5 uint64
	var x6 uint64
	x5, x6 = bits.Sub64(uint64(0x0), arg1[2], uint64(fiatScalarUint1(x4)))
	var x7 uint64
	var x8 uint64
	x7, x8 = bits.Sub64(uint64(0x0), arg1[3], uint64(fiatScalarUint1(x6)))
	var x9 uint64
	fiatScalarCmovznzU64(&x9, fiatScalarUint1(x8), uint64(0x0), 0xffffffffffffffff)
	var x10 uint64
	var x11 uint64
	x10, x11 = bits.Add64(x1, (x9 & 0x5812631a5cf5d3ed), uint64(0x0))
	var x12 uint64
	var x13 uint64
	x12, x13 = bits.Add64(x3, (x9 & 0x14def9dea2f79cd6), uint64(fiatScalarUint1(x11)))
	var x14 uint64
	var x15 uint64
	x14, x15 = bits.Add64(x5, uint64(0x0), uint64(fiatScalarUint1(x13)))
	var x16 uint64
	x16, _ = bits.Add64(x7, (x9 & 0x1000000000000000), uint64(fiatScalarUint1(x15)))
	out1[0] = x10
	out1[1] = x12
	out1[2] = x14
	out1[3] = x16
}

// fiatScalarNonzero outputs a single non-zero word if the input is non-zero and zero otherwise.
//
// Preconditions:
//
//	0 ≤ eval arg1 < m
//
// Postconditions:
//
//	out1 = 0 ↔ eval (from_montgomery arg1) mod m = 0
//
// Input Bounds:
//
//	arg1: [[0x0 ~> 0xffffffffffffffff], [0x0 ~> 0xffffffffffffffff], [0x0 ~> 0xffffffffffffffff], [0x0 ~> 0xffffffffffffffff]]
//
// Output Bounds:
//
//	out1: [0x0 ~> 0xffffffffffffffff]
func fiatScalarNonzero(out1 *uint64, arg1 *[4]uint64) {
	x1 := (arg1[0] | (arg1[1] | (arg1[2] | arg1[3])))
	*out1 = x1
}

// fiatScalarFromMontgomery translates a field element out of the Montgomery domain.
//
// Preconditions:
//
//	0 ≤ eval arg1 < m
//
// Postconditions:
//
//	eval out1 mod m = (eval arg1 * ((2^64)⁻¹ mod m)^4) mod m
//	0 ≤ eval out1 < m
func fiatScalarFromMontgomery(out1 *fiatScalarNonMontgomeryDomainFieldElement, arg1 *fiatScalarMontgomeryDomainFieldElement) {
	x1 := arg1[0]
	var x2 uint64
	_, x2 = bits.Mul64(x1, 0xd2b51da312547e1b)
	var x4 uint64
	var x5 uint64
	x5, x4 = bits.Mul64(x2, 0x1000000000000000)
	var x6 uint64
	var x7 uint64
	x7, x6 = bits.Mul64(x2, 0x14def9dea2f79cd6)
	var x8 uint64
	var x9 uint64
	x9, x8 = bits.Mul64(x2, 0x5812631a5cf5d3ed)
	var x10 uint64
	var x11 uint64
	x10, x11 = bits.Add64(x9, x6, uint64(0x0))
	var x13 uint64
	_, x13 = bits.Add64(x1, x8, uint64(0x0))
	var x14 uint64
	var x15 uint64
	x14, x15 = bits.Add64(uint64(0x0), x10, uint64(fiatScalarUint1(x13)))
	var x16 uint64
	var x17 uint64
	x16, x17 = bits.Add64(x14, arg1[1], uint64(0x0))
	var x18 uint64
	_, x18 = bits.Mul64(x16, 0xd2b51da312547e1b)
	var x20 uint64
	var x21 uint64
	x21, x20 = bits.Mul64(x18, 0x1000000000000000)
	var x22 uint64
	var x23 uint64
	x23, x22 = bits.Mul64(x18, 0x14def9dea2f79cd6)
	var x24 uint64
	var x25 uint64
	x25, x24 = bits.Mul64(x18, 0x5812631a5cf5d3ed)
	var x26 uint64
	var x27 uint64
	x26, x27 = bits.Add64(x25, x22, uint64(0x0))
	var x29 uint64
	_, x29 = bits.Add64(x16, x24, uint64(0x0))
	var x30 uint64
	var x31 uint64
	x30, x31 = bits.Add64((uint64(fiatScalarUint1(x17)) + (uint64(fiatScalarUint1(x15)) + (uint64(fiatScalarUint1(x11)) + x7))), x26, uint64(fiatScalarUint1(x29)))
	var x32 uint64
	var x33 uint64
	x32, x33 = bits.Add64(x4, (uint64(fiatScalarUint1(x27)) + x23), uint64(fiatScalarUint1(x31)))
	var x34 uint64
	var x35 uint64
	x34, x35 = bits.Add64(x5, x20, uint64(fiatScalarUint1(x33)))
	var x36 uint64
	var x37 uint64
	x36, x37 = bits.Add64(x30, arg1[2], uint64(0x0))
	var x38 uint64
	var x39 uint64
	x38, x39 = bits.Add64(x32, uint64(0x0), uint64(fiatScalarUint1(x37)))
	var x40 uint64
	var x41 uint64
	x40, x41 = bits.Add64(x34, uint64(0x0), uint64(fiatScalarUint1(x39)))
	var x42 uint64
	_, x42 = bits.Mul64(x36, 0xd2b51da312547e1b)
	var x44 uint64
	var x45 uint64
	x45, x44 = bits.Mul64(x42, 0x1000000000000000)
	var x46 uint64
	var x47 uint64
	x47, x46 = bits.Mul64(x42, 0x14def9dea2f79cd6)
	var x48 uint64
	var x49 uint64
	x49, x48 = bits.Mul64(x42, 0x5812631a5cf5d3ed)
	var x50 uint64
	var x51 uint64
	x50, x51 = bits.Add64(x49, x46, uint64(0x0))
	var x53 uint64
	_, x53 = bits.Add64(x36, x48, uint64(0x0))
	var x54 uint64
	var x55 uint64
	x54, x55 = bits.Add64(x38, x50, uint64(fiatScalarUint1(x53)))
	var x56 uint64
	var x57 uint64
	x56, x57 = bits.Add64(x40, (uint64(fiatScalarUint1(x51)) + x47), uint64(fiatScalarUint1(x55)))
	var x58 uint64
	var x59 uint64
	x58, x59 = bits.Add64((uint64(fiatScalarUint1(x41)) + (uint64(fiatScalarUint1(x35)) + x21)), x44, uint64(fiatScalarUint1(x57)))
	var x60 uint64
	var x61 uint64
	x60, x61 = bits.Add64(x54, arg1[3], uint64(0x0))
	var x62 uint64
	var x63 uint64
	x62, x63 = bits.Add64(x56, uint64(0x0), uint64(fiatScalarUint1(x61)))
	var x64 uint64
	var x65 uint64
	x64, x65 = bits.Add64(x58, uint64(0x0), uint64(fiatScalarUint1(x63)))
	var x66 uint64
	_, x66 = bits.Mul64(x60, 0xd2b51da312547e1b)
	var x68 uint64
	var x69 uint64
	x69, x68 = bits.Mul64(x66, 0x1000000000000000)
	var x70 uint64
	var x71 uint64
	x71, x70 = bits.Mul64(x66, 0x14def9dea2f79cd6)
	var x72 uint64
	var x73 uint64
	x73, x72 = bits.Mul64(x66, 0x5812631a5cf5d3ed)
	var x74 uint64
	var x75 uint64
	x74, x75 = bits.Add64(x73, x70, uint64(0x0))
	var x77 uint64
	_, x77 = bits.Add64(x60, x72, uint64(0x0))
	var x78 uint64
	var x79 uint64
	x78, x79 = bits.Add64(x62, x74, uint64(fiatScalarUint1(x77)))
	var x80 uint64
	var x81 uint64
	x80, x81 = bits.Add64(x64, (uint64(fiatScalarUint1(x75)) + x71), uint64(fiatScalarUint1(x79)))
	var x82 uint64
	var x83 uint64
	x82, x83 = bits.Add64((uint64(fiatScalarUint1(x65)) + (uint64(fiatScalarUint1(x59)) + x45)), x68, uint64(fiatScalarUint1(x81)))
	x84 := (uint64(fiatScalarUint1(x83)) + x69)
	var x85 uint64
	var x86 uint64
	x85, x86 = bits.Sub64(x78, 0x5812631a5cf5d3ed, uint64(0x0))
	var x87 uint64
	var x88 uint64
	x87, x88 = bits.Sub64(x80, 0x14def9dea2f79cd6, uint64(fiatScalarUint1(x86)))
	var x89 uint64
	var x90 uint64
	x89, x90 = bits.Sub64(x82, uint64(0x0), uint64(fiatScalarUint1(x88)))
	var x91 uint64
	var x92 uint64
	x91, x92 = bits.Sub64(x84, 0x1000000000000000, uint64(fiatScalarUint1(x90)))
	var x94 uint64
	_, x94 = bits.Sub64(uint64(0x0), uint64(0x0), uint64(fiatScalarUint1(x92)))
	var x95 uint64
	fiatScalarCmovznzU64(&x95, fiatScalarUint1(x94), x85, x78)
	var x96 uint64
	fiatScalarCmovznzU64(&x96, fiatScalarUint1(x94), x87, x80)
	var x97 uint64
	fiatScalarCmovznzU64(&x97, fiatScalarUint1(x94), x89, x82)
	var x98 uint64
	fiatScalarCmovznzU64(&x98, fiatScalarUint1(x94), x91, x84)
	out1[0] = x95
	out1[1] = x96
	out1[2] = x97
	out1[3] = x98
}

// fiatScalarToMontgomery translates a field element into the Montgomery domain.
//
// Preconditions:
//
//	0 ≤ eval arg1 < m
//
// Postconditions:
//
//	eval (from_montgomery out1) mod m = eval arg1 mod m
//	0 ≤ eval out1 < m
func fiatScalarToMontgomery(out1 *fiatScalarMontgomeryDomainFieldElement, arg1 *fiatScalarNonMontgomeryDomainFieldElement) {
	x1 := arg1[1]
	x2 := arg1[2]
	x3 := arg1[3]
	x4 := arg1[0]
	var x5 uint64
	var x6 uint64
	x6, x5 = bits.Mul64(x4, 0x399411b7c309a3d)
	var x7 uint64
	var x8 uint64
	x8, x7 = bits.Mul64(x4, 0xceec73d217f5be65)
	var x9 uint64
	var x10 uint64
	x10, x9 = bits.Mul64(x4, 0xd00e1ba768859347)
	var x11 uint64
	var x12 uint64
	x12, x11 = bits.Mul64(x4, 0xa40611e3449c0f01)
	var x13 uint64
	var x14 uint64
	x13, x14 = bits.Add64(x12, x9, uint64(0x0))
	var x15 uint64
	var x16 uint64
	x15, x16 = bits.Add64(x10, x7, uint64(fiatScalarUint1(x14)))
	var x17 uint64
	var x18 uint64
	x17, x18 = bits.Add64(x8, x5, uint64(fiatScalarUint1(x16)))
	var x19 uint64
	_, x19 = bits.Mul64(x11, 0xd2b51da312547e1b)
	var x21 uint64
	var x22 uint64
	x22, x21 = bits.Mul64(x19, 0x1000000000000000)
	var x23 uint64
	var x24 uint64
	x24, x23 = bits.Mul64(x19, 0x14def9dea2f79cd6)
	var x25 uint64
	var x26 uint64
	x26, x25 = bits.Mul64(x19, 0x5812631a5cf5d3ed)
	var x27 uint64
	var x28 uint64
	x27, x28 = bits.Add64(x26, x23, uint64(0x0))
	var x30 uint64
	_, x30 = bits.Add64(x11, x25, uint64(0x0))
	var x31 uint64
	var x32 uint64
	x31, x32 = bits.Add64(x13, x27, uint64(fiatScalarUint1(x30)))
	var x33 uint64
	var x34 uint64
	x33, x34 = bits.Add64(x15, (uint64(fiatScalarUint1(x28)) + x24), uint64(fiatScalarUint1(x32)))
	var x35 uint64
	var x36 uint64
	x35, x36 = bits.Add64(x17, x21, uint64(fiatScalarUint1(x34)))
	var x37 uint64
	var x38 uint64
	x38, x37 = bits.Mul64(x1, 0x399411b7c309a3d)
	var x39 uint64
	var x40 uint64
	x40, x39 = bits.Mul64(x1, 0xceec73d217f5be65)
	var x41 uint64
	var x42 uint64
	x42, x41 = bits.Mul64(x1, 0xd00e1ba768859347)
	var x43 uint64
	var x44 uint64
	x44, x43 = bits.Mul64(x1, 0xa40611e3449c0f01)
	var x45 uint64
	var x46 uint64
	x45, x46 = bits.Add64(x44, x41, uint64(0x0))
	var x47 uint64
	var x48 uint64
	x47, x48 = bits.Add64(x42, x39, uint64(fiatScalarUint1(x46)))
	var x49 uint64
	var x50 uint64
	x49, x50 = bits.Add64(x40, x37, uint64(fiatScalarUint1(x48)))
	var x51 uint64
	var x52 uint64
	x51, x52 = bits.Add64(x31, x43, uint64(0x0))
	var x53 uint64
	var x54 uint64
	x53, x54 = bits.Add64(x33, x45, uint64(fiatScalarUint1(x52)))
	var x55 uint64
	var x56 uint64
	x55, x56 = bits.Add64(x35, x47, uint64(fiatScalarUint1(x54)))
	var x57 uint64
	var x58 uint64
	x57, x58 = bits.Add64(((uint64(fiatScalarUint1(x36)) + (uint64(fiatScalarUint1(x18)) + x6)) + x22), x49, uint64(fiatScalarUint1(x56)))
	var x59 uint64
	_, x59 = bits.Mul64(x51, 0xd2b51da312547e1b)
	var x61 uint64
	var x62 uint64
	x62, x61 = bits.Mul64(x59, 0x1000000000000000)
	var x63 uint64
	var x64 uint64
	x64, x63 = bits.Mul64(x59, 0x14def9dea2f79cd6)
	var x65 uint64
	var x66 uint64
	x66, x65 = bits.Mul64(x59, 0x5812631a5cf5d3ed)
	var x67 uint64
	var x68 uint64
	x67, x68 = bits.Add64(x66, x63, uint64(0x0))
	var x70 uint64
	_, x70 = bits.Add64(x51, x65, uint64(0x0))
	var x71 uint64
	var x72 uint64
	x71, x72 = bits.Add64(x53, x67, uint64(fiatScalarUint1(x70)))
	var x73 uint64
	var x74 uint64
	x73, x74 = bits.Add64(x55, (uint64(fiatScalarUint1(x68)) + x64), uint64(fiatScalarUint1(x72)))
	var x75 uint64
	var x76 uint64
	x75, x76 = bits.Add64(x57, x61, uint64(fiatScalarUint1(x74)))
	var x77 uint64
	var x78 uint64
	x78, x77 = bits.Mul64(x2, 0x399411b7c309a3d)
	var x79 uint64
	var x80 uint64
	x80, x79 = bits.Mul64(x2, 0xceec73d217f5be65)
	var x81 uint64
	var x82 uint64
	x82, x81 = bits.Mul64(x2, 0xd00e1ba768859347)
	var x83 uint64
	var x84 uint64
	x84, x83 = bits.Mul64(x2, 0xa40611e3449c0f01)
	var x85 uint64
	var x86 uint64
	x85, x86 = bits.Add64(x84, x81, uint64(0x0))
	var x87 uint64
	var x88 uint64
	x87, x88 = bits.Add64(x82, x79, uint64(fiatScalarUint1(x86)))
	var x89 uint64
	var x90 uint64
	x89, x90 = bits.Add64(x80, x77, uint64(fiatScalarUint1(x88)))
	var x91 uint64
	var x92 uint64
	x91, x92 = bits.Add64(x71, x83, uint64(0x0))
	var x93 uint64
	var x94 uint64
	x93, x94 = bits.Add64(x73, x85, uint64(fiatScalarUint1(x92)))
	var x95 uint64
	var x96 uint64
	x95, x96 = bits.Add64(x75, x87, uint64(fiatScalarUint1(x94)))
	var x97 uint64
	var x98 uint64
	x97, x98 = bits.Add64(((uint64(fiatScalarUint1(x76)) + (uint64(fiatScalarUint1(x58)) + (uint64(fiatScalarUint1(x50)) + x38))) + x62), x89, uint64(fiatScalarUint1(x96)))
	var x99 uint64
	_, x99 = bits.Mul64(x91, 0xd2b51da312547e1b)
	var x101 uint64
	var x102 uint64
	x102, x101 = bits.Mul64(x99, 0x1000000000000000)
	var x103 uint64
	var x104 uint64
	x104, x103 = bits.Mul64(x99, 0x14def9dea2f79cd6)
	var x105 uint64
	var x106 uint64
	x106, x105 = bits.Mul64(x99, 0x5812631a5cf5d3ed)
	var x107 uint64
	var x108 uint64
	x107, x108 = bits.Add64(x106, x103, uint64(0x0))
	var x110 uint64
	_, x110 = bits.Add64(x91, x105, uint64(0x0))
	var x111 uint64
	var x112 uint64
	x111, x112 = bits.Add64(x93, x107, uint64(fiatScalarUint1(x110)))
	var x113 uint64
	var x114 uint64
	x113, x114 = bits.Add64(x95, (uint64(fiatScalarUint1(x108)) + x104), uint64(fiatScalarUint1(x112)))
	var x115 uint64
	var x116 uint64
	x115, x116 = bits.Add64(x97, x101, uint64(fiatScalarUint1(x114)))
	var x117 uint64
	var x118 uint64
	x118, x117 = bits.Mul64(x3, 0x399411b7c309a3d)
	var x119 uint64
	var x120 uint64
	x120, x119 = bits.Mul64(x3, 0xceec73d217f5be65)
	var x121 uint64
	var x122 uint64
	x122, x121 = bits.Mul64(x3, 0xd00e1ba768859347)
	var x123 uint64
	var x124 uint64
	x124, x123 = bits.Mul64(x3, 0xa40611e3449c0f01)
	var x125 uint64
	var x126 uint64
	x125, x126 = bits.Add64(x124, x121, uint64(0x0))
	var x127 uint64
	var x128 uint64
	x127, x128 = bits.Add64(x122, x119, uint64(fiatScalarUint1(x126)))
	var x129 uint64
	var x130 uint64
	x129, x130 = bits.Add64(x120, x117, uint64(fiatScalarUint1(x128)))
	var x131 uint64
	var x132 uint64
	x131, x132 = bits.Add64(x111, x123, uint64(0x0))
	var x133 uint64
	var x134 uint64
	x133, x134 = bits.Add64(x113, x125, uint64(fiatScalarUint1(x132)))
	var x135 uint64
	var x136 uint64
	x135, x136 = bits.Add64(x115, x127, uint64(fiatScalarUint1(x134)))
	var x137 uint64
	var x138 uint64
	x137, x138 = bits.Add64(((uint64(fiatScalarUint1(x116)) + (uint64(fiatScalarUint1(x98)) + (uint64(fiatScalarUint1(x90)) + x78))) + x102), x129, uint64(fiatScalarUint1(x136)))
	var x139 uint64
	_, x139 = bits.Mul64(x131, 0xd2b51da312547e1b)
	var x141 uint64
	var x142 uint64
	x142, x141 = bits.Mul64(x139, 0x1000000000000000)
	var x143 uint64
	var x144 uint64
	x144, x143 = bits.Mul64(x139, 0x14def9dea2f79cd6)
	var x145 uint64
	var x146 uint64
	x146, x145 = bits.Mul64(x139, 0x5812631a5cf5d3ed)
	var x147 uint64
	var x148 uint64
	x147, x148 = bits.Add64(x146, x143, uint64(0x0))
	var x150 uint64
	_, x150 = bits.Add64(x131, x145, uint64(0x0))
	var x151 uint64
	var x152 uint64
	x151, x152 = bits.Add64(x133, x147, uint64(fiatScalarUint1(x150)))
	var x153 uint64
	var x154 uint64
	x153, x154 = bits.Add64(x135, (uint64(fiatScalarUint1(x148)) + x144), uint64(fiatScalarUint1(x152)))
	var x155 uint64
	var x156 uint64
	x155, x156 = bits.Add64(x137, x141, uint64(fiatScalarUint1(x154)))
	x157 := ((uint64(fiatScalarUint1(x156)) + (uint64(fiatScalarUint1(x138)) + (uint64(fiatScalarUint1(x130)) + x118))) + x142)
	var x158 uint64
	var x159 uint64
	x158, x159 = bits.Sub64(x151, 0x5812631a5cf5d3ed, uint64(0x0))
	var x160 uint64
	var x161 uint64
	x160, x161 = bits.Sub64(x153, 0x14def9dea2f79cd6, uint64(fiatScalarUint1(x159)))
	var x162 uint64
	var x163 uint64
	x162, x163 = bits.Sub64(x155, uint64(0x0), uint64(fiatScalarUint1(x161)))
	var x164 uint64
	var x165 uint64
	x164, x165 = bits.Sub64(x157, 0x1000000000000000, uint64(fiatScalarUint1(x163)))
	var x167 uint64
	_, x167 = bits.Sub64(uint64(0x0), uint64(0x0), uint64(fiatScalarUint1(x165)))
	var x168 uint64
	fiatScalarCmovznzU64(&x168, fiatScalarUint1(x167), x158, x151)
	var x169 uint64
	fiatScalarCmovznzU64(&x169, fiatScalarUint1(x167), x160, x153)
	var x170 uint64
	fiatScalarCmovznzU64(&x170, fiatScalarUint1(x167), x162, x155)
	var x171 uint64
	fiatScalarCmovznzU64(&x171, fiatScalarUint1(x167), x164, x157)
	out1[0] = x168
	out1[1] = x169
	out1[2] = x170
	out1[3] = x171
}

// fiatScalarToBytes serializes a field element NOT in the Montgomery domain to bytes in little-endian order.
//
// Preconditions:
//
//	0 ≤ eval arg1 < m
//
// Postconditions:
//
//	out1 = map (λ x, ⌊((eval arg1 mod m) mod 2^(8 * (x + 1))) / 2^(8 * x)⌋) [0..31]
//
// Input Bounds:
//
//	arg1: [[0x0 ~> 0xffffffffffffffff], [0x0 ~> 0xffffffffffffffff], [0x0 ~> 0xffffffffffffffff], [0x0 ~> 0x1fffffffffffffff]]
//
// Output Bounds:
//
//	out1: [[0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0x1f]]
func fiatScalarToBytes(out1 *[32]uint8, arg1 *[4]uint64) {
	x1 := arg1[3]
	x2 := arg1[2]
	x3 := arg1[1]
	x4 := arg1[0]
	x5 := (uint8(x4) & 0xff)
	x6 := (x4 >> 8)
	x7 := (uint8(x6) & 0xff)
	x8 := (x6 >> 8)
	x9 := (uint8(x8) & 0xff)
	x10 := (x8 >> 8)
	x11 := (uint8(x10) & 0xff)
	x12 := (x10 >> 8)
	x13 := (uint8(x12) & 0xff)
	x14 := (x12 >> 8)
	x15 := (uint8(x14) & 0xff)
	x16 := (x14 >> 8)
	x17 := (uint8(x16) & 0xff)
	x18 := uint8((x16 >> 8))
	x19 := (uint8(x3) & 0xff)
	x20 := (x3 >> 8)
	x21 := (uint8(x20) & 0xff)
	x22 := (x20 >> 8)
	x23 := (uint8(x22) & 0xff)
	x24 := (x22 >> 8)
	x25 := (uint8(x24) & 0xff)
	x26 := (x24 >> 8)
	x27 := (uint8(x26) & 0xff)
	x28 := (x26 >> 8)
	x29 := (uint8(x28) & 0xff)
	x30 := (x28 >> 8)
	x31 := (uint8(x30) & 0xff)
	x32 := uint8((x30 >> 8))
	x33 := (uint8(x2) & 0xff)
	x34 := (x2 >> 8)
	x35 := (uint8(x34) & 0xff)
	x36 := (x34 >> 8)
	x37 := (uint8(x36) & 0xff)
	x38 := (x36 >> 8)
	x39 := (uint8(x38) & 0xff)
	x40 := (x38 >> 8)
	x41 := (uint8(x40) & 0xff)
	x42 := (x40 >> 8)
	x43 := (uint8(x42) & 0xff)
	x44 := (x42 >> 8)
	x45 := (uint8(x44) & 0xff)
	x46 := uint8((x44 >> 8))
	x47 := (uint8(x1) & 0xff)
	x48 := (x1 >> 8)
	x49 := (uint8(x48) & 0xff)
	x50 := (x48 >> 8)
	x51 := (uint8(x50) & 0xff)
	x52 := (x50 >> 8)
	x53 := (uint8(x52) & 0xff)
	x54 := (x52 >> 8)
	x55 := (uint8(x54) & 0xff)
	x56 := (x54 >> 8)
	x57 := (uint8(x56) & 0xff)
	x58 := (x56 >> 8)
	x59 := (uint8(x58) & 0xff)
	x60 := uint8((x58 >> 8))
	out1[0] = x5
	out1[1] = x7
	out1[2] = x9
	out1[3] = x11
	out1[4] = x13
	out1[5] = x15
	out1[6] = x17
	out1[7] = x18
	out1[8] = x19
	out1[9] = x21
	out1[10] = x23
	out1[11] = x25
	out1[12] = x27
	out1[13] = x29
	out1[14] = x31
	out1[15] = x32
	out1[16] = x33
	out1[17] = x35
	out1[18] = x37
	out1[19] = x39
	out1[20] = x41
	out1[21] = x43
	out1[22] = x45
	out1[23] = x46
	out1[24] = x47
	out1[25] = x49
	out1[26] = x51
	out1[27] = x53
	out1[28] = x55
	out1[29] = x57
	out1[30] = x59
	out1[31] = x60
}

// fiatScalarFromBytes deserializes a field element NOT in the Montgomery domain from bytes in little-endian order.
//
// Preconditions:
//
//	0 ≤ bytes_eval arg1 < m
//
// Postconditions:
//
//	eval out1 mod m = bytes_eval arg1 mod m
//	0 ≤ eval out1 < m
//
// Input Bounds:
//
//	arg1: [[0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0xff], [0x0 ~> 0x1f]]
//
// Output Bounds:
//
//	out1: [[0x0 ~> 0xffffffffffffffff], [0x0 ~> 0xffffffffffffffff], [0x0 ~> 0xffffffffffffffff], [0x0 ~> 0x1fffffffffffffff]]
func fiatScalarFromBytes(out1 *[4]uint64, arg1 *[32]uint8) {
	x1 := (uint64(arg1[31]) << 56)
	x2 := (uint64(arg1[30]) << 48)
	x3 := (uint64(arg1[29]) << 40)
	x4 := (uint64(arg1[28]) << 32)
	x5 := (uint64(arg1[27]) << 24)
	x6 := (uint64(arg1[26]) << 16)
	x7 := (uint64(arg1[25]) << 8)
	x8 := arg1[24]
	x9 := (uint64(arg1[23]) << 56)
	x10 := (uint64(arg1[22]) << 48)
	x11 := (uint64(arg1[21]) << 40)
	x12 := (uint64(arg1[20]) << 32)
	x13 := (uint64(arg1[19]) << 24)
	x14 := (uint64(arg1[18]) << 16)
	x15 := (uint64(arg1[17]) << 8)
	x16 := arg1[16]
	x17 := (uint64(arg1[15]) << 56)
	x18 := (uint64(arg1[14]) << 48)
	x19 := (uint64(arg1[13]) << 40)
	x20 := (uint64(arg1[12]) << 32)
	x21 := (uint64(arg1[11]) << 24)
	x22 := (uint64(arg1[10]) << 16)
	x23 := (uint64(arg1[9]) << 8)
	x24 := arg1[8]
	x25 := (uint64(arg1[7]) << 56)
	x26 := (uint64(arg1[6]) << 48)
	x27 := (uint64(arg1[5]) << 40)
	x28 := (uint64(arg1[4]) << 32)
	x29 := (uint64(arg1[3]) << 24)
	x30 := (uint64(arg1[2]) << 16)
	x31 := (uint64(arg1[1]) << 8)
	x32 := arg1[0]
	x33 := (x31 + uint64(x32))
	x34 := (x30 + x33)
	x35 := (x29 + x34)
	x36 := (x28 + x35)
	x37 := (x27 + x36)
	x38 := (x26 + x37)
	x39 := (x25 + x38)
	x40 := (x23 + uint64(x24))
	x41 := (x22 + x40)
	x42 := (x21 + x41)
	x43 := (x20 + x42)
	x44 := (x19 + x43)
	x45 := (x18 + x44)
	x46 := (x17 + x45)
	x47 := (x15 + uint64(x16))
	x48 := (x14 + x47)
	x49 := (x13 + x48)
	x50 := (x12 + x49)
	x51 := (x11 + x50)
	x52 := (x10 + x51)
	x53 := (x9 + x52)
	x54 := (x7 + uint64(x8))
	x55 := (x6 + x54)
	x56 := (x5 + x55)
	x57 := (x4 + x56)
	x58 := (x3 + x57)
	x59 := (x2 + x58)
	x60 := (x1 + x59)
	out1[0] = x39
	out1[1] = x46
	out1[2] = x53
	out1[3] = x60
}


// ---- vendored from crypto/internal/fips140/edwards25519/scalarmult.go ----



// basepointTable is a set of 32 affineLookupTables, where table i is generated
// from 256i * basepoint. It is precomputed the first time it's used.
func basepointTable() *[32]affineLookupTable {
	basepointTablePrecomp.initOnce.Do(func() {
		p := NewGeneratorPoint()
		for i := 0; i < 32; i++ {
			basepointTablePrecomp.table[i].FromP3(p)
			for j := 0; j < 8; j++ {
				p.Add(p, p)
			}
		}
	})
	return &basepointTablePrecomp.table
}

var basepointTablePrecomp struct {
	table    [32]affineLookupTable
	initOnce sync.Once
}

// ScalarBaseMult sets v = x * B, where B is the canonical generator, and
// returns v.
//
// The scalar multiplication is done in constant time.
func (v *Point) ScalarBaseMult(x *Scalar) *Point {
	basepointTable := basepointTable()

	// Write x = sum(x_i * 16^i) so  x*B = sum( B*x_i*16^i )
	// as described in the Ed25519 paper
	//
	// Group even and odd coefficients
	// x*B     = x_0*16^0*B + x_2*16^2*B + ... + x_62*16^62*B
	//         + x_1*16^1*B + x_3*16^3*B + ... + x_63*16^63*B
	// x*B     = x_0*16^0*B + x_2*16^2*B + ... + x_62*16^62*B
	//    + 16*( x_1*16^0*B + x_3*16^2*B + ... + x_63*16^62*B)
	//
	// We use a lookup table for each i to get x_i*16^(2*i)*B
	// and do four doublings to multiply by 16.
	digits := x.signedRadix16()

	multiple := &affineCached{}
	tmp1 := &projP1xP1{}
	tmp2 := &projP2{}

	// Accumulate the odd components first
	v.Set(NewIdentityPoint())
	for i := 1; i < 64; i += 2 {
		basepointTable[i/2].SelectInto(multiple, digits[i])
		tmp1.AddAffine(v, multiple)
		v.fromP1xP1(tmp1)
	}

	// Multiply by 16
	tmp2.FromP3(v)       // tmp2 =    v in P2 coords
	tmp1.Double(tmp2)    // tmp1 =  2*v in P1xP1 coords
	tmp2.FromP1xP1(tmp1) // tmp2 =  2*v in P2 coords
	tmp1.Double(tmp2)    // tmp1 =  4*v in P1xP1 coords
	tmp2.FromP1xP1(tmp1) // tmp2 =  4*v in P2 coords
	tmp1.Double(tmp2)    // tmp1 =  8*v in P1xP1 coords
	tmp2.FromP1xP1(tmp1) // tmp2 =  8*v in P2 coords
	tmp1.Double(tmp2)    // tmp1 = 16*v in P1xP1 coords
	v.fromP1xP1(tmp1)    // now v = 16*(odd components)

	// Accumulate the even components
	for i := 0; i < 64; i += 2 {
		basepointTable[i/2].SelectInto(multiple, digits[i])
		tmp1.AddAffine(v, multiple)
		v.fromP1xP1(tmp1)
	}

	return v
}

// ScalarMult sets v = x * q, and returns v.
//
// The scalar multiplication is done in constant time.
func (v *Point) ScalarMult(x *Scalar, q *Point) *Point {
	checkInitialized(q)

	var table projLookupTable
	table.FromP3(q)

	// Write x = sum(x_i * 16^i)
	// so  x*Q = sum( Q*x_i*16^i )
	//         = Q*x_0 + 16*(Q*x_1 + 16*( ... + Q*x_63) ... )
	//           <------compute inside out---------
	//
	// We use the lookup table to get the x_i*Q values
	// and do four doublings to compute 16*Q
	digits := x.signedRadix16()

	// Unwrap first loop iteration to save computing 16*identity
	multiple := &projCached{}
	tmp1 := &projP1xP1{}
	tmp2 := &projP2{}
	table.SelectInto(multiple, digits[63])

	v.Set(NewIdentityPoint())
	tmp1.Add(v, multiple) // tmp1 = x_63*Q in P1xP1 coords
	for i := 62; i >= 0; i-- {
		tmp2.FromP1xP1(tmp1) // tmp2 =    (prev) in P2 coords
		tmp1.Double(tmp2)    // tmp1 =  2*(prev) in P1xP1 coords
		tmp2.FromP1xP1(tmp1) // tmp2 =  2*(prev) in P2 coords
		tmp1.Double(tmp2)    // tmp1 =  4*(prev) in P1xP1 coords
		tmp2.FromP1xP1(tmp1) // tmp2 =  4*(prev) in P2 coords
		tmp1.Double(tmp2)    // tmp1 =  8*(prev) in P1xP1 coords
		tmp2.FromP1xP1(tmp1) // tmp2 =  8*(prev) in P2 coords
		tmp1.Double(tmp2)    // tmp1 = 16*(prev) in P1xP1 coords
		v.fromP1xP1(tmp1)    //    v = 16*(prev) in P3 coords
		table.SelectInto(multiple, digits[i])
		tmp1.Add(v, multiple) // tmp1 = x_i*Q + 16*(prev) in P1xP1 coords
	}
	v.fromP1xP1(tmp1)
	return v
}

// basepointNafTable is the nafLookupTable8 for the basepoint.
// It is precomputed the first time it's used.
func basepointNafTable() *nafLookupTable8 {
	basepointNafTablePrecomp.initOnce.Do(func() {
		basepointNafTablePrecomp.table.FromP3(NewGeneratorPoint())
	})
	return &basepointNafTablePrecomp.table
}

var basepointNafTablePrecomp struct {
	table    nafLookupTable8
	initOnce sync.Once
}

// VarTimeDoubleScalarBaseMult sets v = a * A + b * B, where B is the canonical
// generator, and returns v.
//
// Execution time depends on the inputs.
func (v *Point) VarTimeDoubleScalarBaseMult(a *Scalar, A *Point, b *Scalar) *Point {
	checkInitialized(A)

	// Similarly to the single variable-base approach, we compute
	// digits and use them with a lookup table.  However, because
	// we are allowed to do variable-time operations, we don't
	// need constant-time lookups or constant-time digit
	// computations.
	//
	// So we use a non-adjacent form of some width w instead of
	// radix 16.  This is like a binary representation (one digit
	// for each binary place) but we allow the digits to grow in
	// magnitude up to 2^{w-1} so that the nonzero digits are as
	// sparse as possible.  Intuitively, this "condenses" the
	// "mass" of the scalar onto sparse coefficients (meaning
	// fewer additions).

	basepointNafTable := basepointNafTable()
	var aTable nafLookupTable5
	aTable.FromP3(A)
	// Because the basepoint is fixed, we can use a wider NAF
	// corresponding to a bigger table.
	aNaf := a.nonAdjacentForm(5)
	bNaf := b.nonAdjacentForm(8)

	// Find the first nonzero coefficient.
	i := 255
	for i >= 0 {
		if aNaf[i] != 0 || bNaf[i] != 0 {
			break
		}
		i--
	}

	multA := &projCached{}
	multB := &affineCached{}
	tmp1 := &projP1xP1{}
	tmp2 := &projP2{}
	tmp2.Zero()

	// Move from high to low bits, doubling the accumulator
	// at each iteration and checking whether there is a nonzero
	// coefficient to look up a multiple of.
	for ; i >= 0; i-- {
		tmp1.Double(tmp2)

		// Only update v if we have a nonzero coeff to add in.
		if aNaf[i] > 0 {
			v.fromP1xP1(tmp1)
			aTable.SelectInto(multA, aNaf[i])
			tmp1.Add(v, multA)
		} else if aNaf[i] < 0 {
			v.fromP1xP1(tmp1)
			aTable.SelectInto(multA, -aNaf[i])
			tmp1.Sub(v, multA)
		}

		if bNaf[i] > 0 {
			v.fromP1xP1(tmp1)
			basepointNafTable.SelectInto(multB, bNaf[i])
			tmp1.AddAffine(v, multB)
		} else if bNaf[i] < 0 {
			v.fromP1xP1(tmp1)
			basepointNafTable.SelectInto(multB, -bNaf[i])
			tmp1.SubAffine(v, multB)
		}

		tmp2.FromP1xP1(tmp1)
	}

	v.fromP2(tmp2)
	return v
}


// ---- vendored from crypto/internal/fips140/edwards25519/tables.go ----



// A dynamic lookup table for variable-base, constant-time scalar muls.
type projLookupTable struct {
	points [8]projCached
}

// A precomputed lookup table for fixed-base, constant-time scalar muls.
type affineLookupTable struct {
	points [8]affineCached
}

// A dynamic lookup table for variable-base, variable-time scalar muls.
type nafLookupTable5 struct {
	points [8]projCached
}

// A precomputed lookup table for fixed-base, variable-time scalar muls.
type nafLookupTable8 struct {
	points [64]affineCached
}

// Constructors.

// Builds a lookup table at runtime. Fast.
func (v *projLookupTable) FromP3(q *Point) {
	// Goal: v.points[i] = (i+1)*Q, i.e., Q, 2Q, ..., 8Q
	// This allows lookup of -8Q, ..., -Q, 0, Q, ..., 8Q
	v.points[0].FromP3(q)
	tmpP3 := Point{}
	tmpP1xP1 := projP1xP1{}
	for i := 0; i < 7; i++ {
		// Compute (i+1)*Q as Q + i*Q and convert to a projCached
		// This is needlessly complicated because the API has explicit
		// receivers instead of creating stack objects and relying on RVO
		v.points[i+1].FromP3(tmpP3.fromP1xP1(tmpP1xP1.Add(q, &v.points[i])))
	}
}

// This is not optimised for speed; fixed-base tables should be precomputed.
func (v *affineLookupTable) FromP3(q *Point) {
	// Goal: v.points[i] = (i+1)*Q, i.e., Q, 2Q, ..., 8Q
	// This allows lookup of -8Q, ..., -Q, 0, Q, ..., 8Q
	v.points[0].FromP3(q)
	tmpP3 := Point{}
	tmpP1xP1 := projP1xP1{}
	for i := 0; i < 7; i++ {
		// Compute (i+1)*Q as Q + i*Q and convert to affineCached
		v.points[i+1].FromP3(tmpP3.fromP1xP1(tmpP1xP1.AddAffine(q, &v.points[i])))
	}
}

// Builds a lookup table at runtime. Fast.
func (v *nafLookupTable5) FromP3(q *Point) {
	// Goal: v.points[i] = (2*i+1)*Q, i.e., Q, 3Q, 5Q, ..., 15Q
	// This allows lookup of -15Q, ..., -3Q, -Q, 0, Q, 3Q, ..., 15Q
	v.points[0].FromP3(q)
	q2 := Point{}
	q2.Add(q, q)
	tmpP3 := Point{}
	tmpP1xP1 := projP1xP1{}
	for i := 0; i < 7; i++ {
		v.points[i+1].FromP3(tmpP3.fromP1xP1(tmpP1xP1.Add(&q2, &v.points[i])))
	}
}

// This is not optimised for speed; fixed-base tables should be precomputed.
func (v *nafLookupTable8) FromP3(q *Point) {
	v.points[0].FromP3(q)
	q2 := Point{}
	q2.Add(q, q)
	tmpP3 := Point{}
	tmpP1xP1 := projP1xP1{}
	for i := 0; i < 63; i++ {
		v.points[i+1].FromP3(tmpP3.fromP1xP1(tmpP1xP1.AddAffine(&q2, &v.points[i])))
	}
}

// Selectors.

// Set dest to x*Q, where -8 <= x <= 8, in constant time.
func (v *projLookupTable) SelectInto(dest *projCached, x int8) {
	// Compute xabs = |x|
	xmask := x >> 7
	xabs := uint8((x + xmask) ^ xmask)

	dest.Zero()
	for j := 1; j <= 8; j++ {
		// Set dest = j*Q if |x| = j
		cond := constanttime.ByteEq(xabs, uint8(j))
		dest.Select(&v.points[j-1], dest, cond)
	}
	// Now dest = |x|*Q, conditionally negate to get x*Q
	dest.CondNeg(int(xmask & 1))
}

// Set dest to x*Q, where -8 <= x <= 8, in constant time.
func (v *affineLookupTable) SelectInto(dest *affineCached, x int8) {
	// Compute xabs = |x|
	xmask := x >> 7
	xabs := uint8((x + xmask) ^ xmask)

	dest.Zero()
	for j := 1; j <= 8; j++ {
		// Set dest = j*Q if |x| = j
		cond := constanttime.ByteEq(xabs, uint8(j))
		dest.Select(&v.points[j-1], dest, cond)
	}
	// Now dest = |x|*Q, conditionally negate to get x*Q
	dest.CondNeg(int(xmask & 1))
}

// Given odd x with 0 < x < 2^4, return x*Q (in variable time).
func (v *nafLookupTable5) SelectInto(dest *projCached, x int8) {
	*dest = v.points[x/2]
}

// Given odd x with 0 < x < 2^7, return x*Q (in variable time).
func (v *nafLookupTable8) SelectInto(dest *affineCached, x int8) {
	*dest = v.points[x/2]
}
