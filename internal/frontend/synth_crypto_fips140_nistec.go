// SYNTHETIC (Principle 4) — crypto/internal/fips140/nistec: real go1.27.1 sources use amd64 asm kernels and //go:linkname runtime hooks the frontend cannot compile; software-only pure-Go port.
// RETIRES WHEN: graalgo compiles amd64 asm kernels and resolves //go:linkname in the real sources — then delete this file.
// RE-TEST TRIGGER: asm/linkname support for the real stdlib sources lands (B-4 phased removal: docs/arch/bridge-inventory.md).
package main

// P3-M9 tier-5: synthetic 'crypto/internal/fips140/nistec' package.
const syntheticCryptoFips140NistecSource = `// P3-M9 tier-5: synthetic 'crypto/internal/fips140/nistec'.
// go1.27.1 vendored pure-Go sources (p256.go selected for purego;
// p256_asm.go excluded). Synthetic-only.
// DEVIATIONS (documented):
// (1) init() recomputes p256GeneratorTables with pure-Go point
// arithmetic instead of reinterpreting the embedded byte array via
// unsafe.Pointer (not in frontend/v1 scope). table[i][j] =
// (j+1)*2^(6i)*G, matching the documented layout.
// (2) crypto/internal/fips140deps/cpu import dropped (amd64 is
// little-endian; big-endian path not emulated).
package nistec

import (
	_ "crypto/internal/fips140/check"
	"crypto/internal/constanttime"
	"crypto/internal/fips140/nistec/fiat"
	"errors"
	"crypto/internal/fips140deps/byteorder"
	"math/bits"
)


// ---- vendored from crypto/internal/fips140/nistec/nistec.go ----



//go:generate go run generate.go


// ---- vendored from crypto/internal/fips140/nistec/p224.go ----




// p224ElementLength is the length of an element of the base or scalar field,
// which have the same bytes length for all NIST P curves.
const p224ElementLength = 28

// P224Point is a P224 point. The zero value is NOT valid.
type P224Point struct {
	// The point is represented in projective coordinates (X:Y:Z),
	// where x = X/Z and y = Y/Z.
	x, y, z *fiat.P224Element
}

// NewP224Point returns a new P224Point representing the point at infinity point.
func NewP224Point() *P224Point {
	return &P224Point{
		x: new(fiat.P224Element),
		y: new(fiat.P224Element).One(),
		z: new(fiat.P224Element),
	}
}

// SetGenerator sets p to the canonical generator and returns p.
func (p *P224Point) SetGenerator() *P224Point {
	p.x.SetBytes([]byte{0xb7, 0xe, 0xc, 0xbd, 0x6b, 0xb4, 0xbf, 0x7f, 0x32, 0x13, 0x90, 0xb9, 0x4a, 0x3, 0xc1, 0xd3, 0x56, 0xc2, 0x11, 0x22, 0x34, 0x32, 0x80, 0xd6, 0x11, 0x5c, 0x1d, 0x21})
	p.y.SetBytes([]byte{0xbd, 0x37, 0x63, 0x88, 0xb5, 0xf7, 0x23, 0xfb, 0x4c, 0x22, 0xdf, 0xe6, 0xcd, 0x43, 0x75, 0xa0, 0x5a, 0x7, 0x47, 0x64, 0x44, 0xd5, 0x81, 0x99, 0x85, 0x0, 0x7e, 0x34})
	p.z.One()
	return p
}

// Set sets p = q and returns p.
func (p *P224Point) Set(q *P224Point) *P224Point {
	p.x.Set(q.x)
	p.y.Set(q.y)
	p.z.Set(q.z)
	return p
}

// SetBytes sets p to the compressed, uncompressed, or infinity value encoded in
// b, as specified in SEC 1, Version 2.0, Section 2.3.4. If the point is not on
// the curve, it returns nil and an error, and the receiver is unchanged.
// Otherwise, it returns p.
type __mrP224Point_SetBytesPair struct {
	v0 *P224Point
	v1 error
}

func __mrP224Point_SetBytes(p *P224Point, b []byte) __mrP224Point_SetBytesPair {
	switch {
	// Point at infinity.
	case len(b) == 1 && b[0] == 0:
		return __mrP224Point_SetBytesPair{p.Set(NewP224Point()), nil}

	// Uncompressed form.
	case len(b) == 1+2*p224ElementLength && b[0] == 4:
		x, err := new(fiat.P224Element).SetBytes(b[1 : 1+p224ElementLength])
		if err != nil {
			return __mrP224Point_SetBytesPair{nil, err}
		}
		y, err := new(fiat.P224Element).SetBytes(b[1+p224ElementLength:])
		if err != nil {
			return __mrP224Point_SetBytesPair{nil, err}
		}
		if err := p224CheckOnCurve(x, y); err != nil {
			return __mrP224Point_SetBytesPair{nil, err}
		}
		p.x.Set(x)
		p.y.Set(y)
		p.z.One()
		return __mrP224Point_SetBytesPair{p, nil}

	// Compressed form.
	case len(b) == 1+p224ElementLength && (b[0] == 2 || b[0] == 3):
		x, err := new(fiat.P224Element).SetBytes(b[1:])
		if err != nil {
			return __mrP224Point_SetBytesPair{nil, err}
		}

		// y² = x³ - 3x + b
		y := p224Polynomial(new(fiat.P224Element), x)
		if !p224Sqrt(y, y) {
			return __mrP224Point_SetBytesPair{nil, errors.New("invalid P224 compressed point encoding")}
		}

		// Select the positive or negative root, as indicated by the least
		// significant bit, based on the encoding type byte.
		otherRoot := new(fiat.P224Element)
		otherRoot.Sub(otherRoot, y)
		cond := y.Bytes()[p224ElementLength-1]&1 ^ b[0]&1
		y.Select(otherRoot, y, int(cond))

		p.x.Set(x)
		p.y.Set(y)
		p.z.One()
		return __mrP224Point_SetBytesPair{p, nil}

	default:
		return __mrP224Point_SetBytesPair{nil, errors.New("invalid P224 point encoding")}
	}
}

func (p *P224Point) SetBytes(b []byte) (*P224Point, error) {
	panic("crypto/internal/fips140/nistec: SetBytes: multi-result desugar")
}

var _p224B *fiat.P224Element
var _p224BOnce bool

func p224B() *fiat.P224Element {
	if !_p224BOnce {
		_p224B, _ = new(fiat.P224Element).SetBytes([]byte{0xb4, 0x5, 0xa, 0x85, 0xc, 0x4, 0xb3, 0xab, 0xf5, 0x41, 0x32, 0x56, 0x50, 0x44, 0xb0, 0xb7, 0xd7, 0xbf, 0xd8, 0xba, 0x27, 0xb, 0x39, 0x43, 0x23, 0x55, 0xff, 0xb4})
	
	_p224BOnce = true
}
	return _p224B
}

// p224Polynomial sets y2 to x³ - 3x + b, and returns y2.
func p224Polynomial(y2, x *fiat.P224Element) *fiat.P224Element {
	y2.Square(x)
	y2.Mul(y2, x)

	threeX := new(fiat.P224Element).Add(x, x)
	threeX.Add(threeX, x)
	y2.Sub(y2, threeX)

	return y2.Add(y2, p224B())
}

func p224CheckOnCurve(x, y *fiat.P224Element) error {
	// y² = x³ - 3x + b
	rhs := p224Polynomial(new(fiat.P224Element), x)
	lhs := new(fiat.P224Element).Square(y)
	if rhs.Equal(lhs) != 1 {
		return errors.New("P224 point not on curve")
	}
	return nil
}

// Bytes returns the uncompressed or infinity encoding of p, as specified in
// SEC 1, Version 2.0, Section 2.3.3. Note that the encoding of the point at
// infinity is shorter than all other encodings.
func (p *P224Point) Bytes() []byte {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var out [1 + 2*p224ElementLength]byte
	return p.bytes(&out)
}

func (p *P224Point) bytes(out *[1 + 2*p224ElementLength]byte) []byte {
	if p.z.IsZero() == 1 {
		return append(out[:0], 0)
	}

	zinv := new(fiat.P224Element).Invert(p.z)
	x := new(fiat.P224Element).Mul(p.x, zinv)
	y := new(fiat.P224Element).Mul(p.y, zinv)

	buf := append(out[:0], 4)
	buf = append(buf, x.Bytes()...)
	buf = append(buf, y.Bytes()...)
	return buf
}

// BytesX returns the encoding of the x-coordinate of p, as specified in SEC 1,
// Version 2.0, Section 2.3.5, or an error if p is the point at infinity.
type __mrP224Point_BytesXPair struct {
	v0 []byte
	v1 error
}

func __mrP224Point_BytesX(p *P224Point) __mrP224Point_BytesXPair {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var out [p224ElementLength]byte
	__fw0, __fw1 := p.bytesX(&out)
	return __mrP224Point_BytesXPair{__fw0, __fw1}
}

func (p *P224Point) BytesX() ([]byte, error) {
	panic("crypto/internal/fips140/nistec: BytesX: multi-result desugar")
}

type __mrP224Point_bytesXPair struct {
	v0 []byte
	v1 error
}

func __mrP224Point_bytesX(p *P224Point, out *[p224ElementLength]byte) __mrP224Point_bytesXPair {
	if p.z.IsZero() == 1 {
		return __mrP224Point_bytesXPair{nil, errors.New("P224 point is the point at infinity")}
	}

	zinv := new(fiat.P224Element).Invert(p.z)
	x := new(fiat.P224Element).Mul(p.x, zinv)

	return __mrP224Point_bytesXPair{append(out[:0], x.Bytes()...), nil}
}

func (p *P224Point) bytesX(out *[p224ElementLength]byte) ([]byte, error) {
	panic("crypto/internal/fips140/nistec: bytesX: multi-result desugar")
}

// BytesCompressed returns the compressed or infinity encoding of p, as
// specified in SEC 1, Version 2.0, Section 2.3.3. Note that the encoding of the
// point at infinity is shorter than all other encodings.
func (p *P224Point) BytesCompressed() []byte {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var out [1 + p224ElementLength]byte
	return p.bytesCompressed(&out)
}

func (p *P224Point) bytesCompressed(out *[1 + p224ElementLength]byte) []byte {
	if p.z.IsZero() == 1 {
		return append(out[:0], 0)
	}

	zinv := new(fiat.P224Element).Invert(p.z)
	x := new(fiat.P224Element).Mul(p.x, zinv)
	y := new(fiat.P224Element).Mul(p.y, zinv)

	// Encode the sign of the y coordinate (indicated by the least significant
	// bit) as the encoding type (2 or 3).
	buf := append(out[:0], 2)
	buf[0] |= y.Bytes()[p224ElementLength-1] & 1
	buf = append(buf, x.Bytes()...)
	return buf
}

// Add sets q = p1 + p2, and returns q. The points may overlap.
func (q *P224Point) Add(p1, p2 *P224Point) *P224Point {
	// Complete addition formula for a = -3 from "Complete addition formulas for
	// prime order elliptic curves" (https://eprint.iacr.org/2015/1060), §A.2.

	t0 := new(fiat.P224Element).Mul(p1.x, p2.x)  // t0 := X1 * X2
	t1 := new(fiat.P224Element).Mul(p1.y, p2.y)  // t1 := Y1 * Y2
	t2 := new(fiat.P224Element).Mul(p1.z, p2.z)  // t2 := Z1 * Z2
	t3 := new(fiat.P224Element).Add(p1.x, p1.y)  // t3 := X1 + Y1
	t4 := new(fiat.P224Element).Add(p2.x, p2.y)  // t4 := X2 + Y2
	t3.Mul(t3, t4)                               // t3 := t3 * t4
	t4.Add(t0, t1)                               // t4 := t0 + t1
	t3.Sub(t3, t4)                               // t3 := t3 - t4
	t4.Add(p1.y, p1.z)                           // t4 := Y1 + Z1
	x3 := new(fiat.P224Element).Add(p2.y, p2.z)  // X3 := Y2 + Z2
	t4.Mul(t4, x3)                               // t4 := t4 * X3
	x3.Add(t1, t2)                               // X3 := t1 + t2
	t4.Sub(t4, x3)                               // t4 := t4 - X3
	x3.Add(p1.x, p1.z)                           // X3 := X1 + Z1
	y3 := new(fiat.P224Element).Add(p2.x, p2.z)  // Y3 := X2 + Z2
	x3.Mul(x3, y3)                               // X3 := X3 * Y3
	y3.Add(t0, t2)                               // Y3 := t0 + t2
	y3.Sub(x3, y3)                               // Y3 := X3 - Y3
	z3 := new(fiat.P224Element).Mul(p224B(), t2) // Z3 := b * t2
	x3.Sub(y3, z3)                               // X3 := Y3 - Z3
	z3.Add(x3, x3)                               // Z3 := X3 + X3
	x3.Add(x3, z3)                               // X3 := X3 + Z3
	z3.Sub(t1, x3)                               // Z3 := t1 - X3
	x3.Add(t1, x3)                               // X3 := t1 + X3
	y3.Mul(p224B(), y3)                          // Y3 := b * Y3
	t1.Add(t2, t2)                               // t1 := t2 + t2
	t2.Add(t1, t2)                               // t2 := t1 + t2
	y3.Sub(y3, t2)                               // Y3 := Y3 - t2
	y3.Sub(y3, t0)                               // Y3 := Y3 - t0
	t1.Add(y3, y3)                               // t1 := Y3 + Y3
	y3.Add(t1, y3)                               // Y3 := t1 + Y3
	t1.Add(t0, t0)                               // t1 := t0 + t0
	t0.Add(t1, t0)                               // t0 := t1 + t0
	t0.Sub(t0, t2)                               // t0 := t0 - t2
	t1.Mul(t4, y3)                               // t1 := t4 * Y3
	t2.Mul(t0, y3)                               // t2 := t0 * Y3
	y3.Mul(x3, z3)                               // Y3 := X3 * Z3
	y3.Add(y3, t2)                               // Y3 := Y3 + t2
	x3.Mul(t3, x3)                               // X3 := t3 * X3
	x3.Sub(x3, t1)                               // X3 := X3 - t1
	z3.Mul(t4, z3)                               // Z3 := t4 * Z3
	t1.Mul(t3, t0)                               // t1 := t3 * t0
	z3.Add(z3, t1)                               // Z3 := Z3 + t1

	q.x.Set(x3)
	q.y.Set(y3)
	q.z.Set(z3)
	return q
}

// Double sets q = p + p, and returns q. The points may overlap.
func (q *P224Point) Double(p *P224Point) *P224Point {
	// Complete addition formula for a = -3 from "Complete addition formulas for
	// prime order elliptic curves" (https://eprint.iacr.org/2015/1060), §A.2.

	t0 := new(fiat.P224Element).Square(p.x)      // t0 := X ^ 2
	t1 := new(fiat.P224Element).Square(p.y)      // t1 := Y ^ 2
	t2 := new(fiat.P224Element).Square(p.z)      // t2 := Z ^ 2
	t3 := new(fiat.P224Element).Mul(p.x, p.y)    // t3 := X * Y
	t3.Add(t3, t3)                               // t3 := t3 + t3
	z3 := new(fiat.P224Element).Mul(p.x, p.z)    // Z3 := X * Z
	z3.Add(z3, z3)                               // Z3 := Z3 + Z3
	y3 := new(fiat.P224Element).Mul(p224B(), t2) // Y3 := b * t2
	y3.Sub(y3, z3)                               // Y3 := Y3 - Z3
	x3 := new(fiat.P224Element).Add(y3, y3)      // X3 := Y3 + Y3
	y3.Add(x3, y3)                               // Y3 := X3 + Y3
	x3.Sub(t1, y3)                               // X3 := t1 - Y3
	y3.Add(t1, y3)                               // Y3 := t1 + Y3
	y3.Mul(x3, y3)                               // Y3 := X3 * Y3
	x3.Mul(x3, t3)                               // X3 := X3 * t3
	t3.Add(t2, t2)                               // t3 := t2 + t2
	t2.Add(t2, t3)                               // t2 := t2 + t3
	z3.Mul(p224B(), z3)                          // Z3 := b * Z3
	z3.Sub(z3, t2)                               // Z3 := Z3 - t2
	z3.Sub(z3, t0)                               // Z3 := Z3 - t0
	t3.Add(z3, z3)                               // t3 := Z3 + Z3
	z3.Add(z3, t3)                               // Z3 := Z3 + t3
	t3.Add(t0, t0)                               // t3 := t0 + t0
	t0.Add(t3, t0)                               // t0 := t3 + t0
	t0.Sub(t0, t2)                               // t0 := t0 - t2
	t0.Mul(t0, z3)                               // t0 := t0 * Z3
	y3.Add(y3, t0)                               // Y3 := Y3 + t0
	t0.Mul(p.y, p.z)                             // t0 := Y * Z
	t0.Add(t0, t0)                               // t0 := t0 + t0
	z3.Mul(t0, z3)                               // Z3 := t0 * Z3
	x3.Sub(x3, z3)                               // X3 := X3 - Z3
	z3.Mul(t0, t1)                               // Z3 := t0 * t1
	z3.Add(z3, z3)                               // Z3 := Z3 + Z3
	z3.Add(z3, z3)                               // Z3 := Z3 + Z3

	q.x.Set(x3)
	q.y.Set(y3)
	q.z.Set(z3)
	return q
}

// Select sets q to p1 if cond == 1, and to p2 if cond == 0.
func (q *P224Point) Select(p1, p2 *P224Point, cond int) *P224Point {
	q.x.Select(p1.x, p2.x, cond)
	q.y.Select(p1.y, p2.y, cond)
	q.z.Select(p1.z, p2.z, cond)
	return q
}

// A p224Table holds the first 15 multiples of a point at offset -1, so [1]P
// is at table[0], [15]P is at table[14], and [0]P is implicitly the identity
// point.
type p224Table [15]*P224Point

// Select selects the n-th multiple of the table base point into p. It works in
// constant time by iterating over every entry of the table. n must be in [0, 15].
func (table *p224Table) Select(p *P224Point, n uint8) {
	if n >= 16 {
		panic("nistec: internal error: p224Table called with out-of-bounds value")
	}
	p.Set(NewP224Point())
	for i := uint8(1); i < 16; i++ {
		cond := constanttime.ByteEq(i, n)
		p.Select(table[i-1], p, cond)
	}
}

// ScalarMult sets p = scalar * q, and returns p.
type __mrP224Point_ScalarMultPair struct {
	v0 *P224Point
	v1 error
}

func __mrP224Point_ScalarMult(p *P224Point, q *P224Point, scalar []byte) __mrP224Point_ScalarMultPair {
	// Compute a p224Table for the base point q. The explicit NewP224Point
	// calls get inlined, letting the allocations live on the stack.
	var table = p224Table{NewP224Point(), NewP224Point(), NewP224Point(),
		NewP224Point(), NewP224Point(), NewP224Point(), NewP224Point(),
		NewP224Point(), NewP224Point(), NewP224Point(), NewP224Point(),
		NewP224Point(), NewP224Point(), NewP224Point(), NewP224Point()}
	table[0].Set(q)
	for i := 1; i < 15; i += 2 {
		table[i].Double(table[i/2])
		table[i+1].Add(table[i], q)
	}

	// Instead of doing the classic double-and-add chain, we do it with a
	// four-bit window: we double four times, and then add [0-15]P.
	t := NewP224Point()
	p.Set(NewP224Point())
	for i, byte := range scalar {
		// No need to double on the first iteration, as p is the identity at
		// this point, and [N]∞ = ∞.
		if i != 0 {
			p.Double(p)
			p.Double(p)
			p.Double(p)
			p.Double(p)
		}

		windowValue := byte >> 4
		table.Select(t, windowValue)
		p.Add(p, t)

		p.Double(p)
		p.Double(p)
		p.Double(p)
		p.Double(p)

		windowValue = byte & 0b1111
		table.Select(t, windowValue)
		p.Add(p, t)
	}

	return __mrP224Point_ScalarMultPair{p, nil}
}

func (p *P224Point) ScalarMult(q *P224Point, scalar []byte) (*P224Point, error) {
	panic("crypto/internal/fips140/nistec: ScalarMult: multi-result desugar")
}

var p224GeneratorTable *[p224ElementLength * 2]p224Table
var p224GeneratorTableOnce bool

// generatorTable returns a sequence of p224Tables. The first table contains
// multiples of G. Each successive table is the previous table doubled four
// times.
func (p *P224Point) generatorTable() *[p224ElementLength * 2]p224Table {
	if !p224GeneratorTableOnce {
		p224GeneratorTable = new([p224ElementLength * 2]p224Table)
		base := NewP224Point().SetGenerator()
		for i := 0; i < p224ElementLength*2; i++ {
			p224GeneratorTable[i][0] = NewP224Point().Set(base)
			for j := 1; j < 15; j++ {
				p224GeneratorTable[i][j] = NewP224Point().Add(p224GeneratorTable[i][j-1], base)
			}
			base.Double(base)
			base.Double(base)
			base.Double(base)
			base.Double(base)
		}
	
	p224GeneratorTableOnce = true
}
	return p224GeneratorTable
}

// ScalarBaseMult sets p = scalar * B, where B is the canonical generator, and
// returns p.
type __mrP224Point_ScalarBaseMultPair struct {
	v0 *P224Point
	v1 error
}

func __mrP224Point_ScalarBaseMult(p *P224Point, scalar []byte) __mrP224Point_ScalarBaseMultPair {
	if len(scalar) != p224ElementLength {
		return __mrP224Point_ScalarBaseMultPair{nil, errors.New("invalid scalar length")}
	}
	tables := p.generatorTable()

	// This is also a scalar multiplication with a four-bit window like in
	// ScalarMult, but in this case the doublings are precomputed. The value
	// [windowValue]G added at iteration k would normally get doubled
	// (totIterations-k)×4 times, but with a larger precomputation we can
	// instead add [2^((totIterations-k)×4)][windowValue]G and avoid the
	// doublings between iterations.
	t := NewP224Point()
	p.Set(NewP224Point())
	tableIndex := len(tables) - 1
	for _, byte := range scalar {
		windowValue := byte >> 4
		tables[tableIndex].Select(t, windowValue)
		p.Add(p, t)
		tableIndex--

		windowValue = byte & 0b1111
		tables[tableIndex].Select(t, windowValue)
		p.Add(p, t)
		tableIndex--
	}

	return __mrP224Point_ScalarBaseMultPair{p, nil}
}

func (p *P224Point) ScalarBaseMult(scalar []byte) (*P224Point, error) {
	panic("crypto/internal/fips140/nistec: ScalarBaseMult: multi-result desugar")
}

// p224Sqrt sets e to a square root of x. If x is not a square, p224Sqrt returns
// false and e is unchanged. e and x can overlap.
func p224Sqrt(e, x *fiat.P224Element) (isSquare bool) {
	candidate := new(fiat.P224Element)
	p224SqrtCandidate(candidate, x)
	square := new(fiat.P224Element).Square(candidate)
	if square.Equal(x) != 1 {
		return false
	}
	e.Set(candidate)
	return true
}


// ---- vendored from crypto/internal/fips140/nistec/p224_sqrt.go ----




var p224GG *[96]fiat.P224Element
var p224GGOnce bool

// p224SqrtCandidate sets r to a square root candidate for x. r and x must not overlap.
func p224SqrtCandidate(r, x *fiat.P224Element) {
	// Since p = 1 mod 4, we can't use the exponentiation by (p + 1) / 4 like
	// for the other primes. Instead, implement a variation of Tonelli–Shanks.
	// The constant-time implementation is adapted from Thomas Pornin's ecGFp5.
	//
	// https://github.com/pornin/ecgfp5/blob/82325b965/rust/src/field.rs#L337-L385

	// p = q*2^n + 1 with q odd -> q = 2^128 - 1 and n = 96
	// g^(2^n) = 1 -> g = 11 ^ q (where 11 is the smallest non-square)
	// GG[j] = g^(2^j) for j = 0 to n-1

	if !p224GGOnce {
		p224GG = new([96]fiat.P224Element)
		for i := range p224GG {
			if i == 0 {
				p224GG[i].SetBytes([]byte{0x6a, 0x0f, 0xec, 0x67,
					0x85, 0x98, 0xa7, 0x92, 0x0c, 0x55, 0xb2, 0xd4,
					0x0b, 0x2d, 0x6f, 0xfb, 0xbe, 0xa3, 0xd8, 0xce,
					0xf3, 0xfb, 0x36, 0x32, 0xdc, 0x69, 0x1b, 0x74})
			} else {
				p224GG[i].Square(&p224GG[i-1])
			}
		}
	
	p224GGOnce = true
}

	// r <- x^((q+1)/2) = x^(2^127)
	// v <- x^q = x^(2^128-1)

	// Compute x^(2^127-1) first.
	//
	// The sequence of 10 multiplications and 126 squarings is derived from the
	// following addition chain generated with github.com/mmcloughlin/addchain v0.4.0.
	//
	//	_10      = 2*1
	//	_11      = 1 + _10
	//	_110     = 2*_11
	//	_111     = 1 + _110
	//	_111000  = _111 << 3
	//	_111111  = _111 + _111000
	//	_1111110 = 2*_111111
	//	_1111111 = 1 + _1111110
	//	x12      = _1111110 << 5 + _111111
	//	x24      = x12 << 12 + x12
	//	i36      = x24 << 7
	//	x31      = _1111111 + i36
	//	x48      = i36 << 17 + x24
	//	x96      = x48 << 48 + x48
	//	return     x96 << 31 + x31
	//
	var t0 = new(fiat.P224Element)
	var t1 = new(fiat.P224Element)

	r.Square(x)
	r.Mul(x, r)
	r.Square(r)
	r.Mul(x, r)
	t0.Square(r)
	for s := 1; s < 3; s++ {
		t0.Square(t0)
	}
	t0.Mul(r, t0)
	t1.Square(t0)
	r.Mul(x, t1)
	for s := 0; s < 5; s++ {
		t1.Square(t1)
	}
	t0.Mul(t0, t1)
	t1.Square(t0)
	for s := 1; s < 12; s++ {
		t1.Square(t1)
	}
	t0.Mul(t0, t1)
	t1.Square(t0)
	for s := 1; s < 7; s++ {
		t1.Square(t1)
	}
	r.Mul(r, t1)
	for s := 0; s < 17; s++ {
		t1.Square(t1)
	}
	t0.Mul(t0, t1)
	t1.Square(t0)
	for s := 1; s < 48; s++ {
		t1.Square(t1)
	}
	t0.Mul(t0, t1)
	for s := 0; s < 31; s++ {
		t0.Square(t0)
	}
	r.Mul(r, t0)

	// v = x^(2^127-1)^2 * x
	v := new(fiat.P224Element).Square(r)
	v.Mul(v, x)

	// r = x^(2^127-1) * x
	r.Mul(r, x)

	// for i = n-1 down to 1:
	//     w = v^(2^(i-1))
	//     if w == -1 then:
	//         v <- v*GG[n-i]
	//         r <- r*GG[n-i-1]

	var p224MinusOne = new(fiat.P224Element).Sub(
		new(fiat.P224Element), new(fiat.P224Element).One())

	for i := 96 - 1; i >= 1; i-- {
		w := new(fiat.P224Element).Set(v)
		for j := 0; j < i-1; j++ {
			w.Square(w)
		}
		cond := w.Equal(p224MinusOne)
		v.Select(t0.Mul(v, &p224GG[96-i]), v, cond)
		r.Select(t0.Mul(r, &p224GG[96-i-1]), r, cond)
	}
}


// ---- vendored from crypto/internal/fips140/nistec/p256.go ----




// P256Point is a P-256 point. The zero value is NOT valid.
type P256Point struct {
	// The point is represented in projective coordinates (X:Y:Z), where x = X/Z
	// and y = Y/Z. Infinity is (0:1:0).
	//
	// fiat.P256Element is a base field element in [0, P-1] in the Montgomery
	// domain (with R 2²⁵⁶ and P 2²⁵⁶ - 2²²⁴ + 2¹⁹² + 2⁹⁶ - 1) as four limbs in
	// little-endian order value.
	x, y, z fiat.P256Element
}

// NewP256Point returns a new P256Point representing the point at infinity point.
func NewP256Point() *P256Point {
	p := &P256Point{}
	p.y.One()
	return p
}

// SetGenerator sets p to the canonical generator and returns p.
func (p *P256Point) SetGenerator() *P256Point {
	p.x.SetBytes([]byte{0x6b, 0x17, 0xd1, 0xf2, 0xe1, 0x2c, 0x42, 0x47, 0xf8, 0xbc, 0xe6, 0xe5, 0x63, 0xa4, 0x40, 0xf2, 0x77, 0x3, 0x7d, 0x81, 0x2d, 0xeb, 0x33, 0xa0, 0xf4, 0xa1, 0x39, 0x45, 0xd8, 0x98, 0xc2, 0x96})
	p.y.SetBytes([]byte{0x4f, 0xe3, 0x42, 0xe2, 0xfe, 0x1a, 0x7f, 0x9b, 0x8e, 0xe7, 0xeb, 0x4a, 0x7c, 0xf, 0x9e, 0x16, 0x2b, 0xce, 0x33, 0x57, 0x6b, 0x31, 0x5e, 0xce, 0xcb, 0xb6, 0x40, 0x68, 0x37, 0xbf, 0x51, 0xf5})
	p.z.One()
	return p
}

// Set sets p = q and returns p.
func (p *P256Point) Set(q *P256Point) *P256Point {
	p.x.Set(&q.x)
	p.y.Set(&q.y)
	p.z.Set(&q.z)
	return p
}

const p256ElementLength = 32
const p256UncompressedLength = 1 + 2*p256ElementLength
const p256CompressedLength = 1 + p256ElementLength

// SetBytes sets p to the compressed, uncompressed, or infinity value encoded in
// b, as specified in SEC 1, Version 2.0, Section 2.3.4. If the point is not on
// the curve, it returns nil and an error, and the receiver is unchanged.
// Otherwise, it returns p.
type __mrP256Point_SetBytesPair struct {
	v0 *P256Point
	v1 error
}

func __mrP256Point_SetBytes(p *P256Point, b []byte) __mrP256Point_SetBytesPair {
	switch {
	// Point at infinity.
	case len(b) == 1 && b[0] == 0:
		return __mrP256Point_SetBytesPair{p.Set(NewP256Point()), nil}

	// Uncompressed form.
	case len(b) == p256UncompressedLength && b[0] == 4:
		x, err := new(fiat.P256Element).SetBytes(b[1 : 1+p256ElementLength])
		if err != nil {
			return __mrP256Point_SetBytesPair{nil, err}
		}
		y, err := new(fiat.P256Element).SetBytes(b[1+p256ElementLength:])
		if err != nil {
			return __mrP256Point_SetBytesPair{nil, err}
		}
		if err := p256CheckOnCurve(x, y); err != nil {
			return __mrP256Point_SetBytesPair{nil, err}
		}
		p.x.Set(x)
		p.y.Set(y)
		p.z.One()
		return __mrP256Point_SetBytesPair{p, nil}

	// Compressed form.
	case len(b) == p256CompressedLength && (b[0] == 2 || b[0] == 3):
		x, err := new(fiat.P256Element).SetBytes(b[1:])
		if err != nil {
			return __mrP256Point_SetBytesPair{nil, err}
		}

		// y² = x³ - 3x + b
		y := p256Polynomial(new(fiat.P256Element), x)
		if !p256Sqrt(y, y) {
			return __mrP256Point_SetBytesPair{nil, errors.New("invalid P256 compressed point encoding")}
		}

		// Select the positive or negative root, as indicated by the least
		// significant bit, based on the encoding type byte.
		otherRoot := new(fiat.P256Element)
		otherRoot.Sub(otherRoot, y)
		cond := y.Bytes()[p256ElementLength-1]&1 ^ b[0]&1
		y.Select(otherRoot, y, int(cond))

		p.x.Set(x)
		p.y.Set(y)
		p.z.One()
		return __mrP256Point_SetBytesPair{p, nil}

	default:
		return __mrP256Point_SetBytesPair{nil, errors.New("invalid P256 point encoding")}
	}
}

func (p *P256Point) SetBytes(b []byte) (*P256Point, error) {
	panic("crypto/internal/fips140/nistec: SetBytes: multi-result desugar")
}

var _p256B *fiat.P256Element
var _p256BOnce bool

func p256B() *fiat.P256Element {
	if !_p256BOnce {
		_p256B, _ = new(fiat.P256Element).SetBytes([]byte{0x5a, 0xc6, 0x35, 0xd8, 0xaa, 0x3a, 0x93, 0xe7, 0xb3, 0xeb, 0xbd, 0x55, 0x76, 0x98, 0x86, 0xbc, 0x65, 0x1d, 0x6, 0xb0, 0xcc, 0x53, 0xb0, 0xf6, 0x3b, 0xce, 0x3c, 0x3e, 0x27, 0xd2, 0x60, 0x4b})
	
	_p256BOnce = true
}
	return _p256B
}

// p256Polynomial sets y2 to x³ - 3x + b, and returns y2.
func p256Polynomial(y2, x *fiat.P256Element) *fiat.P256Element {
	y2.Square(x)
	y2.Mul(y2, x)

	threeX := new(fiat.P256Element).Add(x, x)
	threeX.Add(threeX, x)
	y2.Sub(y2, threeX)

	return y2.Add(y2, p256B())
}

func p256CheckOnCurve(x, y *fiat.P256Element) error {
	// y² = x³ - 3x + b
	rhs := p256Polynomial(new(fiat.P256Element), x)
	lhs := new(fiat.P256Element).Square(y)
	if rhs.Equal(lhs) != 1 {
		return errors.New("P256 point not on curve")
	}
	return nil
}

// Bytes returns the uncompressed or infinity encoding of p, as specified in
// SEC 1, Version 2.0, Section 2.3.3. Note that the encoding of the point at
// infinity is shorter than all other encodings.
func (p *P256Point) Bytes() []byte {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var out [p256UncompressedLength]byte
	return p.bytes(&out)
}

func (p *P256Point) bytes(out *[p256UncompressedLength]byte) []byte {
	// The SEC 1 representation of the point at infinity is a single zero byte,
	// and only infinity has z = 0.
	if p.z.IsZero() == 1 {
		return append(out[:0], 0)
	}

	zinv := new(fiat.P256Element).Invert(&p.z)
	x := new(fiat.P256Element).Mul(&p.x, zinv)
	y := new(fiat.P256Element).Mul(&p.y, zinv)

	buf := append(out[:0], 4)
	buf = append(buf, x.Bytes()...)
	buf = append(buf, y.Bytes()...)
	return buf
}

// BytesX returns the encoding of the x-coordinate of p, as specified in SEC 1,
// Version 2.0, Section 2.3.5, or an error if p is the point at infinity.
type __mrP256Point_BytesXPair struct {
	v0 []byte
	v1 error
}

func __mrP256Point_BytesX(p *P256Point) __mrP256Point_BytesXPair {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var out [p256ElementLength]byte
	__fw0, __fw1 := p.bytesX(&out)
	return __mrP256Point_BytesXPair{__fw0, __fw1}
}

func (p *P256Point) BytesX() ([]byte, error) {
	panic("crypto/internal/fips140/nistec: BytesX: multi-result desugar")
}

type __mrP256Point_bytesXPair struct {
	v0 []byte
	v1 error
}

func __mrP256Point_bytesX(p *P256Point, out *[p256ElementLength]byte) __mrP256Point_bytesXPair {
	if p.z.IsZero() == 1 {
		return __mrP256Point_bytesXPair{nil, errors.New("P256 point is the point at infinity")}
	}

	zinv := new(fiat.P256Element).Invert(&p.z)
	x := new(fiat.P256Element).Mul(&p.x, zinv)

	return __mrP256Point_bytesXPair{append(out[:0], x.Bytes()...), nil}
}

func (p *P256Point) bytesX(out *[p256ElementLength]byte) ([]byte, error) {
	panic("crypto/internal/fips140/nistec: bytesX: multi-result desugar")
}

// BytesCompressed returns the compressed or infinity encoding of p, as
// specified in SEC 1, Version 2.0, Section 2.3.3. Note that the encoding of the
// point at infinity is shorter than all other encodings.
func (p *P256Point) BytesCompressed() []byte {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var out [p256CompressedLength]byte
	return p.bytesCompressed(&out)
}

func (p *P256Point) bytesCompressed(out *[p256CompressedLength]byte) []byte {
	if p.z.IsZero() == 1 {
		return append(out[:0], 0)
	}

	zinv := new(fiat.P256Element).Invert(&p.z)
	x := new(fiat.P256Element).Mul(&p.x, zinv)
	y := new(fiat.P256Element).Mul(&p.y, zinv)

	// Encode the sign of the y coordinate (indicated by the least significant
	// bit) as the encoding type (2 or 3).
	buf := append(out[:0], 2)
	buf[0] |= y.Bytes()[p256ElementLength-1] & 1
	buf = append(buf, x.Bytes()...)
	return buf
}

// Add sets q = p1 + p2, and returns q. The points may overlap.
func (q *P256Point) Add(p1, p2 *P256Point) *P256Point {
	// Complete addition formula for a = -3 from "Complete addition formulas for
	// prime order elliptic curves" (https://eprint.iacr.org/2015/1060), §A.2.

	t0 := new(fiat.P256Element).Mul(&p1.x, &p2.x) // t0 := X1 * X2
	t1 := new(fiat.P256Element).Mul(&p1.y, &p2.y) // t1 := Y1 * Y2
	t2 := new(fiat.P256Element).Mul(&p1.z, &p2.z) // t2 := Z1 * Z2
	t3 := new(fiat.P256Element).Add(&p1.x, &p1.y) // t3 := X1 + Y1
	t4 := new(fiat.P256Element).Add(&p2.x, &p2.y) // t4 := X2 + Y2
	t3.Mul(t3, t4)                                // t3 := t3 * t4
	t4.Add(t0, t1)                                // t4 := t0 + t1
	t3.Sub(t3, t4)                                // t3 := t3 - t4
	t4.Add(&p1.y, &p1.z)                          // t4 := Y1 + Z1
	x3 := new(fiat.P256Element).Add(&p2.y, &p2.z) // X3 := Y2 + Z2
	t4.Mul(t4, x3)                                // t4 := t4 * X3
	x3.Add(t1, t2)                                // X3 := t1 + t2
	t4.Sub(t4, x3)                                // t4 := t4 - X3
	x3.Add(&p1.x, &p1.z)                          // X3 := X1 + Z1
	y3 := new(fiat.P256Element).Add(&p2.x, &p2.z) // Y3 := X2 + Z2
	x3.Mul(x3, y3)                                // X3 := X3 * Y3
	y3.Add(t0, t2)                                // Y3 := t0 + t2
	y3.Sub(x3, y3)                                // Y3 := X3 - Y3
	z3 := new(fiat.P256Element).Mul(p256B(), t2)  // Z3 := b * t2
	x3.Sub(y3, z3)                                // X3 := Y3 - Z3
	z3.Add(x3, x3)                                // Z3 := X3 + X3
	x3.Add(x3, z3)                                // X3 := X3 + Z3
	z3.Sub(t1, x3)                                // Z3 := t1 - X3
	x3.Add(t1, x3)                                // X3 := t1 + X3
	y3.Mul(p256B(), y3)                           // Y3 := b * Y3
	t1.Add(t2, t2)                                // t1 := t2 + t2
	t2.Add(t1, t2)                                // t2 := t1 + t2
	y3.Sub(y3, t2)                                // Y3 := Y3 - t2
	y3.Sub(y3, t0)                                // Y3 := Y3 - t0
	t1.Add(y3, y3)                                // t1 := Y3 + Y3
	y3.Add(t1, y3)                                // Y3 := t1 + Y3
	t1.Add(t0, t0)                                // t1 := t0 + t0
	t0.Add(t1, t0)                                // t0 := t1 + t0
	t0.Sub(t0, t2)                                // t0 := t0 - t2
	t1.Mul(t4, y3)                                // t1 := t4 * Y3
	t2.Mul(t0, y3)                                // t2 := t0 * Y3
	y3.Mul(x3, z3)                                // Y3 := X3 * Z3
	y3.Add(y3, t2)                                // Y3 := Y3 + t2
	x3.Mul(t3, x3)                                // X3 := t3 * X3
	x3.Sub(x3, t1)                                // X3 := X3 - t1
	z3.Mul(t4, z3)                                // Z3 := t4 * Z3
	t1.Mul(t3, t0)                                // t1 := t3 * t0
	z3.Add(z3, t1)                                // Z3 := Z3 + t1

	q.x.Set(x3)
	q.y.Set(y3)
	q.z.Set(z3)
	return q
}

// Double sets q = p + p, and returns q. The points may overlap.
func (q *P256Point) Double(p *P256Point) *P256Point {
	// Complete addition formula for a = -3 from "Complete addition formulas for
	// prime order elliptic curves" (https://eprint.iacr.org/2015/1060), §A.2.

	t0 := new(fiat.P256Element).Square(&p.x)     // t0 := X ^ 2
	t1 := new(fiat.P256Element).Square(&p.y)     // t1 := Y ^ 2
	t2 := new(fiat.P256Element).Square(&p.z)     // t2 := Z ^ 2
	t3 := new(fiat.P256Element).Mul(&p.x, &p.y)  // t3 := X * Y
	t3.Add(t3, t3)                               // t3 := t3 + t3
	z3 := new(fiat.P256Element).Mul(&p.x, &p.z)  // Z3 := X * Z
	z3.Add(z3, z3)                               // Z3 := Z3 + Z3
	y3 := new(fiat.P256Element).Mul(p256B(), t2) // Y3 := b * t2
	y3.Sub(y3, z3)                               // Y3 := Y3 - Z3
	x3 := new(fiat.P256Element).Add(y3, y3)      // X3 := Y3 + Y3
	y3.Add(x3, y3)                               // Y3 := X3 + Y3
	x3.Sub(t1, y3)                               // X3 := t1 - Y3
	y3.Add(t1, y3)                               // Y3 := t1 + Y3
	y3.Mul(x3, y3)                               // Y3 := X3 * Y3
	x3.Mul(x3, t3)                               // X3 := X3 * t3
	t3.Add(t2, t2)                               // t3 := t2 + t2
	t2.Add(t2, t3)                               // t2 := t2 + t3
	z3.Mul(p256B(), z3)                          // Z3 := b * Z3
	z3.Sub(z3, t2)                               // Z3 := Z3 - t2
	z3.Sub(z3, t0)                               // Z3 := Z3 - t0
	t3.Add(z3, z3)                               // t3 := Z3 + Z3
	z3.Add(z3, t3)                               // Z3 := Z3 + t3
	t3.Add(t0, t0)                               // t3 := t0 + t0
	t0.Add(t3, t0)                               // t0 := t3 + t0
	t0.Sub(t0, t2)                               // t0 := t0 - t2
	t0.Mul(t0, z3)                               // t0 := t0 * Z3
	y3.Add(y3, t0)                               // Y3 := Y3 + t0
	t0.Mul(&p.y, &p.z)                           // t0 := Y * Z
	t0.Add(t0, t0)                               // t0 := t0 + t0
	z3.Mul(t0, z3)                               // Z3 := t0 * Z3
	x3.Sub(x3, z3)                               // X3 := X3 - Z3
	z3.Mul(t0, t1)                               // Z3 := t0 * t1
	z3.Add(z3, z3)                               // Z3 := Z3 + Z3
	z3.Add(z3, z3)                               // Z3 := Z3 + Z3

	q.x.Set(x3)
	q.y.Set(y3)
	q.z.Set(z3)
	return q
}

// p256AffinePoint is a point in affine coordinates (x, y). x and y are still
// Montgomery domain elements. The point can't be the point at infinity.
type p256AffinePoint struct {
	x, y fiat.P256Element
}

func (p *p256AffinePoint) Projective() *P256Point {
	pp := &P256Point{x: p.x, y: p.y}
	pp.z.One()
	return pp
}

// AddAffine sets q = p1 + p2, if infinity == 0, and to p1 if infinity == 1.
// p2 can't be the point at infinity as it can't be represented in affine
// coordinates, instead callers can set p2 to an arbitrary point and set
// infinity to 1.
func (q *P256Point) AddAffine(p1 *P256Point, p2 *p256AffinePoint, infinity int) *P256Point {
	// Complete mixed addition formula for a = -3 from "Complete addition
	// formulas for prime order elliptic curves"
	// (https://eprint.iacr.org/2015/1060), Algorithm 5.

	t0 := new(fiat.P256Element).Mul(&p1.x, &p2.x)   // t0 ← X1 · X2
	t1 := new(fiat.P256Element).Mul(&p1.y, &p2.y)   // t1 ← Y1 · Y2
	t3 := new(fiat.P256Element).Add(&p2.x, &p2.y)   // t3 ← X2 + Y2
	t4 := new(fiat.P256Element).Add(&p1.x, &p1.y)   // t4 ← X1 + Y1
	t3.Mul(t3, t4)                                  // t3 ← t3 · t4
	t4.Add(t0, t1)                                  // t4 ← t0 + t1
	t3.Sub(t3, t4)                                  // t3 ← t3 − t4
	t4.Mul(&p2.y, &p1.z)                            // t4 ← Y2 · Z1
	t4.Add(t4, &p1.y)                               // t4 ← t4 + Y1
	y3 := new(fiat.P256Element).Mul(&p2.x, &p1.z)   // Y3 ← X2 · Z1
	y3.Add(y3, &p1.x)                               // Y3 ← Y3 + X1
	z3 := new(fiat.P256Element).Mul(p256B(), &p1.z) // Z3 ← b  · Z1
	x3 := new(fiat.P256Element).Sub(y3, z3)         // X3 ← Y3 − Z3
	z3.Add(x3, x3)                                  // Z3 ← X3 + X3
	x3.Add(x3, z3)                                  // X3 ← X3 + Z3
	z3.Sub(t1, x3)                                  // Z3 ← t1 − X3
	x3.Add(t1, x3)                                  // X3 ← t1 + X3
	y3.Mul(p256B(), y3)                             // Y3 ← b  · Y3
	t1.Add(&p1.z, &p1.z)                            // t1 ← Z1 + Z1
	t2 := new(fiat.P256Element).Add(t1, &p1.z)      // t2 ← t1 + Z1
	y3.Sub(y3, t2)                                  // Y3 ← Y3 − t2
	y3.Sub(y3, t0)                                  // Y3 ← Y3 − t0
	t1.Add(y3, y3)                                  // t1 ← Y3 + Y3
	y3.Add(t1, y3)                                  // Y3 ← t1 + Y3
	t1.Add(t0, t0)                                  // t1 ← t0 + t0
	t0.Add(t1, t0)                                  // t0 ← t1 + t0
	t0.Sub(t0, t2)                                  // t0 ← t0 − t2
	t1.Mul(t4, y3)                                  // t1 ← t4 · Y3
	t2.Mul(t0, y3)                                  // t2 ← t0 · Y3
	y3.Mul(x3, z3)                                  // Y3 ← X3 · Z3
	y3.Add(y3, t2)                                  // Y3 ← Y3 + t2
	x3.Mul(t3, x3)                                  // X3 ← t3 · X3
	x3.Sub(x3, t1)                                  // X3 ← X3 − t1
	z3.Mul(t4, z3)                                  // Z3 ← t4 · Z3
	t1.Mul(t3, t0)                                  // t1 ← t3 · t0
	z3.Add(z3, t1)                                  // Z3 ← Z3 + t1

	q.x.Select(&p1.x, x3, infinity)
	q.y.Select(&p1.y, y3, infinity)
	q.z.Select(&p1.z, z3, infinity)
	return q
}

// Select sets q to p1 if cond == 1, and to p2 if cond == 0.
func (q *P256Point) Select(p1, p2 *P256Point, cond int) *P256Point {
	q.x.Select(&p1.x, &p2.x, cond)
	q.y.Select(&p1.y, &p2.y, cond)
	q.z.Select(&p1.z, &p2.z, cond)
	return q
}

// p256OrdElement is a P-256 scalar field element in [0, ord(G)-1]
// as four uint64 limbs in little-endian order.
type p256OrdElement [4]uint64

// SetBytes sets s to the big-endian value of x, reducing it as necessary.
type __mrp256OrdElement_SetBytesPair struct {
	v0 *p256OrdElement
	v1 error
}

func __mrp256OrdElement_SetBytes(s *p256OrdElement, x []byte) __mrp256OrdElement_SetBytesPair {
	if len(x) != 32 {
		return __mrp256OrdElement_SetBytesPair{nil, errors.New("invalid scalar length")}
	}

	s[0] = byteorder.BEUint64(x[24:])
	s[1] = byteorder.BEUint64(x[16:])
	s[2] = byteorder.BEUint64(x[8:])
	s[3] = byteorder.BEUint64(x[:])

	// Ensure s is in the range [0, ord(G)-1]. Since 2 * ord(G) > 2²⁵⁶, we can
	// just conditionally subtract ord(G), keeping the result if it doesn't
	// underflow.
	t0, b := bits.Sub64(s[0], 0xf3b9cac2fc632551, 0)
	t1, b := bits.Sub64(s[1], 0xbce6faada7179e84, b)
	t2, b := bits.Sub64(s[2], 0xffffffffffffffff, b)
	t3, b := bits.Sub64(s[3], 0xffffffff00000000, b)
	tMask := b - 1 // zero if subtraction underflowed
	s[0] ^= (t0 ^ s[0]) & tMask
	s[1] ^= (t1 ^ s[1]) & tMask
	s[2] ^= (t2 ^ s[2]) & tMask
	s[3] ^= (t3 ^ s[3]) & tMask

	return __mrp256OrdElement_SetBytesPair{s, nil}
}

func (s *p256OrdElement) SetBytes(x []byte) (*p256OrdElement, error) {
	panic("crypto/internal/fips140/nistec: SetBytes: multi-result desugar")
}

func (s *p256OrdElement) Bytes() []byte {
	var out [32]byte
	byteorder.BEPutUint64(out[24:], s[0])
	byteorder.BEPutUint64(out[16:], s[1])
	byteorder.BEPutUint64(out[8:], s[2])
	byteorder.BEPutUint64(out[:], s[3])
	return out[:]
}

// Rsh returns the 64 least significant bits of x >> n. n must be lower
// than 256. The value of n leaks through timing side-channels.
func (s *p256OrdElement) Rsh(n int) uint64 {
	i := n / 64
	n = n % 64
	res := s[i] >> n
	// Shift in the more significant limb, if present.
	if i := i + 1; i < len(s) {
		res |= s[i] << (64 - n)
	}
	return res
}

// p256Table is a table of the first 16 multiples of a point. Points are stored
// at an index offset of -1 so [8]P is at index 7, P is at 0, and [16]P is at 15.
// [0]P is the point at infinity and it's not stored.
type p256Table [16]P256Point

// Select selects the n-th multiple of the table base point into p. It works in
// constant time. n must be in [0, 16]. If n is 0, p is set to the identity point.
func (table *p256Table) Select(p *P256Point, n uint8) {
	if n > 16 {
		panic("nistec: internal error: p256Table called with out-of-bounds value")
	}
	p.Set(NewP256Point())
	for i := uint8(1); i <= 16; i++ {
		cond := constanttime.ByteEq(i, n)
		p.Select(&table[i-1], p, cond)
	}
}

// Compute populates the table to the first 16 multiples of q.
func (table *p256Table) Compute(q *P256Point) *p256Table {
	table[0].Set(q)
	for i := 1; i < 16; i += 2 {
		table[i].Double(&table[i/2])
		if i+1 < 16 {
			table[i+1].Add(&table[i], q)
		}
	}
	return table
}

type __mrboothW5Pair struct {
	v0 uint8
	v1 int
}

func __mrboothW5(in uint64) __mrboothW5Pair {
	s := ^((in >> 5) - 1)
	d := (1 << 6) - in - 1
	d = (d & s) | (in & (^s))
	d = (d >> 1) + (d & 1)
	return __mrboothW5Pair{uint8(d), int(s & 1)}
}

func boothW5(in uint64) (uint8, int) {
	panic("crypto/internal/fips140/nistec: boothW5: multi-result desugar")
}

// ScalarMult sets r = scalar * q, where scalar is a 32-byte big endian value,
// and returns r. If scalar is not 32 bytes long, ScalarMult returns an error
// and the receiver is unchanged.
type __mrP256Point_ScalarMultPair struct {
	v0 *P256Point
	v1 error
}

func __mrP256Point_ScalarMult(p *P256Point, q *P256Point, scalar []byte) __mrP256Point_ScalarMultPair {
	s, err := new(p256OrdElement).SetBytes(scalar)
	if err != nil {
		return __mrP256Point_ScalarMultPair{nil, err}
	}

	// Start scanning the window from the most significant bits. We move by
	// 5 bits at a time and need to finish at -1, so -1 + 5 * 51 = 254.
	index := 254

	sel, sign := boothW5(s.Rsh(index))
	// sign is always zero because the boothW5 input here is at
	// most two bits long, so the top bit is never set.
	_ = sign

	// Neither Select nor Add have exceptions for the point at infinity /
	// selector zero, so we don't need to check for it here or in the loop.
	table := new(p256Table).Compute(q)
	table.Select(p, sel)

	t := NewP256Point()
	for index >= 4 {
		index -= 5

		p.Double(p)
		p.Double(p)
		p.Double(p)
		p.Double(p)
		p.Double(p)

		if index >= 0 {
			sel, sign = boothW5(s.Rsh(index) & 0b111111)
		} else {
			// Booth encoding considers a virtual zero bit at index -1,
			// so we shift left the least significant limb.
			wvalue := (s[0] << 1) & 0b111111
			sel, sign = boothW5(wvalue)
		}

		table.Select(t, sel)
		t.Negate(sign)
		p.Add(p, t)
	}

	return __mrP256Point_ScalarMultPair{p, nil}
}

func (p *P256Point) ScalarMult(q *P256Point, scalar []byte) (*P256Point, error) {
	panic("crypto/internal/fips140/nistec: ScalarMult: multi-result desugar")
}

// Negate sets p to -p, if cond == 1, and to p if cond == 0.
func (p *P256Point) Negate(cond int) *P256Point {
	negY := new(fiat.P256Element)
	negY.Sub(negY, &p.y)
	p.y.Select(negY, &p.y, cond)
	return p
}

// p256AffineTable is a table of the first 32 multiples of a point. Points are
// stored at an index offset of -1 like in p256Table, and [0]P is not stored.
type p256AffineTable [32]p256AffinePoint

// Select selects the n-th multiple of the table base point into p. It works in
// constant time. n can be in [0, 32], but (unlike p256Table.Select) if n is 0,
// p is set to an undefined value.
func (table *p256AffineTable) Select(p *p256AffinePoint, n uint8) {
	if n > 32 {
		panic("nistec: internal error: p256AffineTable.Select called with out-of-bounds value")
	}
	for i := uint8(1); i <= 32; i++ {
		cond := constanttime.ByteEq(i, n)
		p.x.Select(&table[i-1].x, &p.x, cond)
		p.y.Select(&table[i-1].y, &p.y, cond)
	}
}

// p256GeneratorTables is a series of precomputed multiples of G, the canonical
// generator. The first p256AffineTable contains multiples of G. The second one
// multiples of [2⁶]G, the third one of [2¹²]G, and so on, where each successive
// table is the previous table doubled six times. Six is the width of the
// sliding window used in ScalarBaseMult, and having each table already
// pre-doubled lets us avoid the doublings between windows entirely.
// P3-M9: the table is recomputed in init() (the embedded p256PrecomputedEmbed
// bytes were removed as dead data).
var p256GeneratorTables *[43]p256AffineTable

func init() {
	// P3-M9: recompute the table (see header deviation 1).
	t := new([43]p256AffineTable)
	base := NewP256Point().SetGenerator()
	acc := NewP256Point()
	zi := new(fiat.P256Element)
	for i := range t {
		acc.Set(base)
		for j := range t[i] {
			zi.Invert(&acc.z)
			t[i][j].x.Mul(&acc.x, zi)
			t[i][j].y.Mul(&acc.y, zi)
			acc.Add(acc, base)
		}
		for k := 0; k < 6; k++ {
			base.Double(base)
		}
	}
	p256GeneratorTables = t
}

type __mrboothW6Pair struct {
	v0 uint8
	v1 int
}

func __mrboothW6(in uint64) __mrboothW6Pair {
	s := ^((in >> 6) - 1)
	d := (1 << 7) - in - 1
	d = (d & s) | (in & (^s))
	d = (d >> 1) + (d & 1)
	return __mrboothW6Pair{uint8(d), int(s & 1)}
}

func boothW6(in uint64) (uint8, int) {
	panic("crypto/internal/fips140/nistec: boothW6: multi-result desugar")
}

// ScalarBaseMult sets p = scalar * generator, where scalar is a 32-byte big
// endian value, and returns r. If scalar is not 32 bytes long, ScalarBaseMult
// returns an error and the receiver is unchanged.
type __mrP256Point_ScalarBaseMultPair struct {
	v0 *P256Point
	v1 error
}

func __mrP256Point_ScalarBaseMult(p *P256Point, scalar []byte) __mrP256Point_ScalarBaseMultPair {
	// This function works like ScalarMult above, but the table is fixed and
	// "pre-doubled" for each iteration, so instead of doubling we move to the
	// next table at each iteration.

	s, err := new(p256OrdElement).SetBytes(scalar)
	if err != nil {
		return __mrP256Point_ScalarBaseMultPair{nil, err}
	}

	// Start scanning the window from the most significant bits. We move by
	// 6 bits at a time and need to finish at -1, so -1 + 6 * 42 = 251.
	index := 251

	sel, sign := boothW6(s.Rsh(index))
	// sign is always zero because the boothW6 input here is at
	// most five bits long, so the top bit is never set.
	_ = sign

	t := &p256AffinePoint{}
	table := &p256GeneratorTables[(index+1)/6]
	table.Select(t, sel)

	// Select's output is undefined if the selector is zero, when it should be
	// the point at infinity (because infinity can't be represented in affine
	// coordinates). Here we conditionally set p to the infinity if sel is zero.
	// In the loop, that's handled by AddAffine.
	selIsZero := constanttime.ByteEq(sel, 0)
	p.Select(NewP256Point(), t.Projective(), selIsZero)

	for index >= 5 {
		index -= 6

		if index >= 0 {
			sel, sign = boothW6(s.Rsh(index) & 0b1111111)
		} else {
			// Booth encoding considers a virtual zero bit at index -1,
			// so we shift left the least significant limb.
			wvalue := (s[0] << 1) & 0b1111111
			sel, sign = boothW6(wvalue)
		}

		table := &p256GeneratorTables[(index+1)/6]
		table.Select(t, sel)
		t.Negate(sign)
		selIsZero := constanttime.ByteEq(sel, 0)
		p.AddAffine(p, t, selIsZero)
	}

	return __mrP256Point_ScalarBaseMultPair{p, nil}
}

func (p *P256Point) ScalarBaseMult(scalar []byte) (*P256Point, error) {
	panic("crypto/internal/fips140/nistec: ScalarBaseMult: multi-result desugar")
}

// Negate sets p to -p, if cond == 1, and to p if cond == 0.
func (p *p256AffinePoint) Negate(cond int) *p256AffinePoint {
	negY := new(fiat.P256Element)
	negY.Sub(negY, &p.y)
	p.y.Select(negY, &p.y, cond)
	return p
}

// p256Sqrt sets e to a square root of x. If x is not a square, p256Sqrt returns
// false and e is unchanged. e and x can overlap.
func p256Sqrt(e, x *fiat.P256Element) (isSquare bool) {
	t0, t1 := new(fiat.P256Element), new(fiat.P256Element)

	// Since p = 3 mod 4, exponentiation by (p + 1) / 4 yields a square root candidate.
	//
	// The sequence of 7 multiplications and 253 squarings is derived from the
	// following addition chain generated with github.com/mmcloughlin/addchain v0.4.0.
	//
	//	_10       = 2*1
	//	_11       = 1 + _10
	//	_1100     = _11 << 2
	//	_1111     = _11 + _1100
	//	_11110000 = _1111 << 4
	//	_11111111 = _1111 + _11110000
	//	x16       = _11111111 << 8 + _11111111
	//	x32       = x16 << 16 + x16
	//	return      ((x32 << 32 + 1) << 96 + 1) << 94
	//
	p256Square(t0, x, 1)
	t0.Mul(x, t0)
	p256Square(t1, t0, 2)
	t0.Mul(t0, t1)
	p256Square(t1, t0, 4)
	t0.Mul(t0, t1)
	p256Square(t1, t0, 8)
	t0.Mul(t0, t1)
	p256Square(t1, t0, 16)
	t0.Mul(t0, t1)
	p256Square(t0, t0, 32)
	t0.Mul(x, t0)
	p256Square(t0, t0, 96)
	t0.Mul(x, t0)
	p256Square(t0, t0, 94)

	// Check if the candidate t0 is indeed a square root of x.
	t1.Square(t0)
	if t1.Equal(x) != 1 {
		return false
	}
	e.Set(t0)
	return true
}

// p256Square sets e to the square of x, repeated n times > 1.
func p256Square(e, x *fiat.P256Element, n int) {
	e.Square(x)
	for i := 1; i < n; i++ {
		e.Square(e)
	}
}


// ---- vendored from crypto/internal/fips140/nistec/p256_ordinv.go ----



// P256OrdInverse sets k to the inverse of k modulo ord(G). k is encoded as four
// uint64 limbs in little-endian order, and must be reduced. If k is zero, the
// result is zero.
func P256OrdInverse(k *[4]uint64) {
	// Inversion is implemented as exponentiation by n - 2, per Fermat's little theorem.
	//
	// The sequence of 38 multiplications and 254 squarings is derived from
	// https://briansmith.org/ecc-inversion-addition-chains-01#p256_scalar_inversion
	_1 := new(p256OrdMontElement)
	_11 := new(p256OrdMontElement)
	_101 := new(p256OrdMontElement)
	_111 := new(p256OrdMontElement)
	_1111 := new(p256OrdMontElement)
	_10101 := new(p256OrdMontElement)
	_101111 := new(p256OrdMontElement)
	x := new(p256OrdMontElement)
	t := new(p256OrdMontElement)

	j := (*p256OrdElement)(k)
	p256OrdToMontgomery(_1, j) // _1
	p256OrdSqr(x, _1, 1)       // _10
	p256OrdMul(_11, x, _1)     // _11
	p256OrdMul(_101, x, _11)   // _101
	p256OrdMul(_111, x, _101)  // _111
	p256OrdSqr(x, _101, 1)     // _1010
	p256OrdMul(_1111, _101, x) // _1111

	p256OrdSqr(t, x, 1)          // _10100
	p256OrdMul(_10101, t, _1)    // _10101
	p256OrdSqr(x, _10101, 1)     // _101010
	p256OrdMul(_101111, _101, x) // _101111
	p256OrdMul(x, _10101, x)     // _111111 = x6
	p256OrdSqr(t, x, 2)          // _11111100
	p256OrdMul(t, t, _11)        // _11111111 = x8
	p256OrdSqr(x, t, 8)          // _ff00
	p256OrdMul(x, x, t)          // _ffff = x16
	p256OrdSqr(t, x, 16)         // _ffff0000
	p256OrdMul(t, t, x)          // _ffffffff = x32

	p256OrdSqr(x, t, 64)
	p256OrdMul(x, x, t)
	p256OrdSqr(x, x, 32)
	p256OrdMul(x, x, t)

	sqrs := []int{
		6, 5, 4, 5, 5,
		4, 3, 3, 5, 9,
		6, 2, 5, 6, 5,
		4, 5, 5, 3, 10,
		2, 5, 5, 3, 7, 6}
	muls := []*p256OrdMontElement{
		_101111, _111, _11, _1111, _10101,
		_101, _101, _101, _111, _101111,
		_1111, _1, _1, _1111, _111,
		_111, _111, _101, _11, _101111,
		_11, _11, _11, _1, _10101, _1111}

	for i, s := range sqrs {
		p256OrdSqr(x, x, s)
		p256OrdMul(x, x, muls[i])
	}

	p256OrdFromMontgomery(j, x)
}

// The code below was generated by Fiat Cryptography v0.1.6-63-g92ee794c2, and
// then manually formatted and optimized.
//
//	word-by-word-montgomery --lang Go --no-wide-int
//	--relax-primitive-carry-to-bitwidth 32,64 --cmovznz-by-mul --static
//	--package-case flatcase --private-function-case camelCase --private-type-case
//	camelCase --no-prefix-fiat --package-name nistec p256Ord 64
//	2^256-2^224+2^192-89188191075325690597107910205041859247
//  mul square from_montgomery to_montgomery
//
// https://mit-plv.github.io/fiat-crypto/
//
//	 Copyright (c) 2015-2020 the fiat-crypto authors (see the AUTHORS file)
//	 All rights reserved.
//
//	 Redistribution and use in source and binary forms, with or without
//	 modification, are permitted provided that the following conditions are
//	 met:
//
//	 	1. Redistributions of source code must retain the above copyright
//	 	notice, this list of conditions and the following disclaimer.
//
//	 THIS SOFTWARE IS PROVIDED BY the fiat-crypto authors "AS IS"
//	 AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO,
//	 THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR
//	 PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL Berkeley Software Design,
//	 Inc. BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL,
//	 EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO,
//	 PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR
//	 PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF
//	 LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING
//	 NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
//	 SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
//

// p256OrdMontElement is a scalar field element in the Montgomery domain, as
// four uint64 limbs in little-endian order. It must be strictly less than
// ord(G) and in Montgomery form (with R 2²⁵⁶).
type p256OrdMontElement [4]uint64

// p256OrdMul multiplies two field elements in the Montgomery domain.
func p256OrdMul(out1 *p256OrdMontElement, arg1 *p256OrdMontElement, arg2 *p256OrdMontElement) {
	x1 := arg1[1]
	x2 := arg1[2]
	x3 := arg1[3]
	x4 := arg1[0]
	x6, x5 := bits.Mul64(x4, arg2[3])
	x8, x7 := bits.Mul64(x4, arg2[2])
	x10, x9 := bits.Mul64(x4, arg2[1])
	x12, x11 := bits.Mul64(x4, arg2[0])
	x13, x14 := bits.Add64(x12, x9, 0)
	x15, x16 := bits.Add64(x10, x7, x14)
	x17, x18 := bits.Add64(x8, x5, x16)
	x19 := x18 + x6
	_, x20 := bits.Mul64(x11, 0xccd1c8aaee00bc4f)
	// x20 * 0xffffffff00000000 = x20 * (2^64 - 2^32)
	x22, b22 := bits.Sub64(0, x20<<32, 0)
	x23 := x20 - (x20 >> 32) - b22
	// x20 * 0xffffffffffffffff = x20 * (2^64 - 1)
	x24, b24 := bits.Sub64(0, x20, 0)
	x25 := x20 - b24
	x27, x26 := bits.Mul64(x20, 0xbce6faada7179e84)
	x29, x28 := bits.Mul64(x20, 0xf3b9cac2fc632551)
	x30, x31 := bits.Add64(x29, x26, 0)
	x32, x33 := bits.Add64(x27, x24, x31)
	x34, x35 := bits.Add64(x25, x22, x33)
	x36 := x35 + x23
	_, x38 := bits.Add64(x11, x28, 0)
	x39, x40 := bits.Add64(x13, x30, x38)
	x41, x42 := bits.Add64(x15, x32, x40)
	x43, x44 := bits.Add64(x17, x34, x42)
	x45, x46 := bits.Add64(x19, x36, x44)
	x48, x47 := bits.Mul64(x1, arg2[3])
	x50, x49 := bits.Mul64(x1, arg2[2])
	x52, x51 := bits.Mul64(x1, arg2[1])
	x54, x53 := bits.Mul64(x1, arg2[0])
	x55, x56 := bits.Add64(x54, x51, 0)
	x57, x58 := bits.Add64(x52, x49, x56)
	x59, x60 := bits.Add64(x50, x47, x58)
	x61 := x60 + x48
	x62, x63 := bits.Add64(x39, x53, 0)
	x64, x65 := bits.Add64(x41, x55, x63)
	x66, x67 := bits.Add64(x43, x57, x65)
	x68, x69 := bits.Add64(x45, x59, x67)
	x70, x71 := bits.Add64(x46, x61, x69)
	_, x72 := bits.Mul64(x62, 0xccd1c8aaee00bc4f)
	x74, b74 := bits.Sub64(0, x72<<32, 0)
	x75 := x72 - (x72 >> 32) - b74
	x76, b76 := bits.Sub64(0, x72, 0)
	x77 := x72 - b76
	x79, x78 := bits.Mul64(x72, 0xbce6faada7179e84)
	x81, x80 := bits.Mul64(x72, 0xf3b9cac2fc632551)
	x82, x83 := bits.Add64(x81, x78, 0)
	x84, x85 := bits.Add64(x79, x76, x83)
	x86, x87 := bits.Add64(x77, x74, x85)
	x88 := x87 + x75
	_, x90 := bits.Add64(x62, x80, 0)
	x91, x92 := bits.Add64(x64, x82, x90)
	x93, x94 := bits.Add64(x66, x84, x92)
	x95, x96 := bits.Add64(x68, x86, x94)
	x97, x98 := bits.Add64(x70, x88, x96)
	x99 := x98 + x71
	x101, x100 := bits.Mul64(x2, arg2[3])
	x103, x102 := bits.Mul64(x2, arg2[2])
	x105, x104 := bits.Mul64(x2, arg2[1])
	x107, x106 := bits.Mul64(x2, arg2[0])
	x108, x109 := bits.Add64(x107, x104, 0)
	x110, x111 := bits.Add64(x105, x102, x109)
	x112, x113 := bits.Add64(x103, x100, x111)
	x114 := x113 + x101
	x115, x116 := bits.Add64(x91, x106, 0)
	x117, x118 := bits.Add64(x93, x108, x116)
	x119, x120 := bits.Add64(x95, x110, x118)
	x121, x122 := bits.Add64(x97, x112, x120)
	x123, x124 := bits.Add64(x99, x114, x122)
	_, x125 := bits.Mul64(x115, 0xccd1c8aaee00bc4f)
	x127, b127 := bits.Sub64(0, x125<<32, 0)
	x128 := x125 - (x125 >> 32) - b127
	x129, b129 := bits.Sub64(0, x125, 0)
	x130 := x125 - b129
	x132, x131 := bits.Mul64(x125, 0xbce6faada7179e84)
	x134, x133 := bits.Mul64(x125, 0xf3b9cac2fc632551)
	x135, x136 := bits.Add64(x134, x131, 0)
	x137, x138 := bits.Add64(x132, x129, x136)
	x139, x140 := bits.Add64(x130, x127, x138)
	x141 := x140 + x128
	_, x143 := bits.Add64(x115, x133, 0)
	x144, x145 := bits.Add64(x117, x135, x143)
	x146, x147 := bits.Add64(x119, x137, x145)
	x148, x149 := bits.Add64(x121, x139, x147)
	x150, x151 := bits.Add64(x123, x141, x149)
	x152 := x151 + x124
	x154, x153 := bits.Mul64(x3, arg2[3])
	x156, x155 := bits.Mul64(x3, arg2[2])
	x158, x157 := bits.Mul64(x3, arg2[1])
	x160, x159 := bits.Mul64(x3, arg2[0])
	x161, x162 := bits.Add64(x160, x157, 0)
	x163, x164 := bits.Add64(x158, x155, x162)
	x165, x166 := bits.Add64(x156, x153, x164)
	x167 := x166 + x154
	x168, x169 := bits.Add64(x144, x159, 0)
	x170, x171 := bits.Add64(x146, x161, x169)
	x172, x173 := bits.Add64(x148, x163, x171)
	x174, x175 := bits.Add64(x150, x165, x173)
	x176, x177 := bits.Add64(x152, x167, x175)
	_, x178 := bits.Mul64(x168, 0xccd1c8aaee00bc4f)
	x180, b180 := bits.Sub64(0, x178<<32, 0)
	x181 := x178 - (x178 >> 32) - b180
	x182, b182 := bits.Sub64(0, x178, 0)
	x183 := x178 - b182
	x185, x184 := bits.Mul64(x178, 0xbce6faada7179e84)
	x187, x186 := bits.Mul64(x178, 0xf3b9cac2fc632551)
	x188, x189 := bits.Add64(x187, x184, 0)
	x190, x191 := bits.Add64(x185, x182, x189)
	x192, x193 := bits.Add64(x183, x180, x191)
	x194 := x193 + x181
	_, x196 := bits.Add64(x168, x186, 0)
	x197, x198 := bits.Add64(x170, x188, x196)
	x199, x200 := bits.Add64(x172, x190, x198)
	x201, x202 := bits.Add64(x174, x192, x200)
	x203, x204 := bits.Add64(x176, x194, x202)
	x205 := x204 + x177
	x206, x207 := bits.Sub64(x197, 0xf3b9cac2fc632551, 0)
	x208, x209 := bits.Sub64(x199, 0xbce6faada7179e84, x207)
	x210, x211 := bits.Sub64(x201, 0xffffffffffffffff, x209)
	x212, x213 := bits.Sub64(x203, 0xffffffff00000000, x211)
	_, x215 := bits.Sub64(x205, 0, x213)
	mask, _ := bits.Sub64(0, 0, x215)
	out1[0] = x206&^mask | x197&mask
	out1[1] = x208&^mask | x199&mask
	out1[2] = x210&^mask | x201&mask
	out1[3] = x212&^mask | x203&mask
}

// p256OrdSqr squares n times a field element in the Montgomery domain.
func p256OrdSqr(out1 *p256OrdMontElement, arg1 *p256OrdMontElement, n int) {
	x1 := arg1[1]
	x2 := arg1[2]
	x3 := arg1[3]
	x4 := arg1[0]
	for range n {
		x6, x5 := bits.Mul64(x4, x3)
		x8, x7 := bits.Mul64(x4, x2)
		x10, x9 := bits.Mul64(x4, x1)
		x12, x11 := bits.Mul64(x4, x4)
		x13, x14 := bits.Add64(x12, x9, 0)
		x15, x16 := bits.Add64(x10, x7, x14)
		x17, x18 := bits.Add64(x8, x5, x16)
		x19 := x18 + x6
		_, x20 := bits.Mul64(x11, 0xccd1c8aaee00bc4f)
		x22, b22 := bits.Sub64(0, x20<<32, 0)
		x23 := x20 - (x20 >> 32) - b22
		x24, b24 := bits.Sub64(0, x20, 0)
		x25 := x20 - b24
		x27, x26 := bits.Mul64(x20, 0xbce6faada7179e84)
		x29, x28 := bits.Mul64(x20, 0xf3b9cac2fc632551)
		x30, x31 := bits.Add64(x29, x26, 0)
		x32, x33 := bits.Add64(x27, x24, x31)
		x34, x35 := bits.Add64(x25, x22, x33)
		x36 := x35 + x23
		_, x38 := bits.Add64(x11, x28, 0)
		x39, x40 := bits.Add64(x13, x30, x38)
		x41, x42 := bits.Add64(x15, x32, x40)
		x43, x44 := bits.Add64(x17, x34, x42)
		x45, x46 := bits.Add64(x19, x36, x44)
		x48, x47 := bits.Mul64(x1, x3)
		x50, x49 := bits.Mul64(x1, x2)
		x52, x51 := bits.Mul64(x1, x1)
		x54, x53 := bits.Mul64(x1, x4)
		x55, x56 := bits.Add64(x54, x51, 0)
		x57, x58 := bits.Add64(x52, x49, x56)
		x59, x60 := bits.Add64(x50, x47, x58)
		x61 := x60 + x48
		x62, x63 := bits.Add64(x39, x53, 0)
		x64, x65 := bits.Add64(x41, x55, x63)
		x66, x67 := bits.Add64(x43, x57, x65)
		x68, x69 := bits.Add64(x45, x59, x67)
		x70, x71 := bits.Add64(x46, x61, x69)
		_, x72 := bits.Mul64(x62, 0xccd1c8aaee00bc4f)
		x74, b74 := bits.Sub64(0, x72<<32, 0)
		x75 := x72 - (x72 >> 32) - b74
		x76, b76 := bits.Sub64(0, x72, 0)
		x77 := x72 - b76
		x79, x78 := bits.Mul64(x72, 0xbce6faada7179e84)
		x81, x80 := bits.Mul64(x72, 0xf3b9cac2fc632551)
		x82, x83 := bits.Add64(x81, x78, 0)
		x84, x85 := bits.Add64(x79, x76, x83)
		x86, x87 := bits.Add64(x77, x74, x85)
		x88 := x87 + x75
		_, x90 := bits.Add64(x62, x80, 0)
		x91, x92 := bits.Add64(x64, x82, x90)
		x93, x94 := bits.Add64(x66, x84, x92)
		x95, x96 := bits.Add64(x68, x86, x94)
		x97, x98 := bits.Add64(x70, x88, x96)
		x99 := x98 + x71
		x101, x100 := bits.Mul64(x2, x3)
		x103, x102 := bits.Mul64(x2, x2)
		x105, x104 := bits.Mul64(x2, x1)
		x107, x106 := bits.Mul64(x2, x4)
		x108, x109 := bits.Add64(x107, x104, 0)
		x110, x111 := bits.Add64(x105, x102, x109)
		x112, x113 := bits.Add64(x103, x100, x111)
		x114 := x113 + x101
		x115, x116 := bits.Add64(x91, x106, 0)
		x117, x118 := bits.Add64(x93, x108, x116)
		x119, x120 := bits.Add64(x95, x110, x118)
		x121, x122 := bits.Add64(x97, x112, x120)
		x123, x124 := bits.Add64(x99, x114, x122)
		_, x125 := bits.Mul64(x115, 0xccd1c8aaee00bc4f)
		x127, b127 := bits.Sub64(0, x125<<32, 0)
		x128 := x125 - (x125 >> 32) - b127
		x129, b129 := bits.Sub64(0, x125, 0)
		x130 := x125 - b129
		x132, x131 := bits.Mul64(x125, 0xbce6faada7179e84)
		x134, x133 := bits.Mul64(x125, 0xf3b9cac2fc632551)
		x135, x136 := bits.Add64(x134, x131, 0)
		x137, x138 := bits.Add64(x132, x129, x136)
		x139, x140 := bits.Add64(x130, x127, x138)
		x141 := x140 + x128
		_, x143 := bits.Add64(x115, x133, 0)
		x144, x145 := bits.Add64(x117, x135, x143)
		x146, x147 := bits.Add64(x119, x137, x145)
		x148, x149 := bits.Add64(x121, x139, x147)
		x150, x151 := bits.Add64(x123, x141, x149)
		x152 := x151 + x124
		x154, x153 := bits.Mul64(x3, x3)
		x156, x155 := bits.Mul64(x3, x2)
		x158, x157 := bits.Mul64(x3, x1)
		x160, x159 := bits.Mul64(x3, x4)
		x161, x162 := bits.Add64(x160, x157, 0)
		x163, x164 := bits.Add64(x158, x155, x162)
		x165, x166 := bits.Add64(x156, x153, x164)
		x167 := x166 + x154
		x168, x169 := bits.Add64(x144, x159, 0)
		x170, x171 := bits.Add64(x146, x161, x169)
		x172, x173 := bits.Add64(x148, x163, x171)
		x174, x175 := bits.Add64(x150, x165, x173)
		x176, x177 := bits.Add64(x152, x167, x175)
		_, x178 := bits.Mul64(x168, 0xccd1c8aaee00bc4f)
		x180, b180 := bits.Sub64(0, x178<<32, 0)
		x181 := x178 - (x178 >> 32) - b180
		x182, b182 := bits.Sub64(0, x178, 0)
		x183 := x178 - b182
		x185, x184 := bits.Mul64(x178, 0xbce6faada7179e84)
		x187, x186 := bits.Mul64(x178, 0xf3b9cac2fc632551)
		x188, x189 := bits.Add64(x187, x184, 0)
		x190, x191 := bits.Add64(x185, x182, x189)
		x192, x193 := bits.Add64(x183, x180, x191)
		x194 := x193 + x181
		_, x196 := bits.Add64(x168, x186, 0)
		x197, x198 := bits.Add64(x170, x188, x196)
		x199, x200 := bits.Add64(x172, x190, x198)
		x201, x202 := bits.Add64(x174, x192, x200)
		x203, x204 := bits.Add64(x176, x194, x202)
		x205 := x204 + x177
		x206, x207 := bits.Sub64(x197, 0xf3b9cac2fc632551, 0)
		x208, x209 := bits.Sub64(x199, 0xbce6faada7179e84, x207)
		x210, x211 := bits.Sub64(x201, 0xffffffffffffffff, x209)
		x212, x213 := bits.Sub64(x203, 0xffffffff00000000, x211)
		_, x215 := bits.Sub64(x205, 0, x213)
		mask, _ := bits.Sub64(0, 0, x215)
		x4 = x206&^mask | x197&mask
		x1 = x208&^mask | x199&mask
		x2 = x210&^mask | x201&mask
		x3 = x212&^mask | x203&mask
	}
	out1[0] = x4
	out1[1] = x1
	out1[2] = x2
	out1[3] = x3
}

// p256OrdFromMontgomery translates a field element out of the Montgomery domain.
func p256OrdFromMontgomery(out1 *p256OrdElement, arg1 *p256OrdMontElement) {
	x1 := arg1[0]
	_, x2 := bits.Mul64(x1, 0xccd1c8aaee00bc4f)
	x5, x4 := bits.Mul64(x2, 0xffffffff00000000)
	x7, x6 := bits.Mul64(x2, 0xffffffffffffffff)
	x9, x8 := bits.Mul64(x2, 0xbce6faada7179e84)
	x11, x10 := bits.Mul64(x2, 0xf3b9cac2fc632551)
	x12, x13 := bits.Add64(x11, x8, 0)
	x14, x15 := bits.Add64(x9, x6, x13)
	x16, x17 := bits.Add64(x7, x4, x15)
	_, x19 := bits.Add64(x1, x10, 0)
	x20, x21 := bits.Add64(0, x12, x19)
	x22, x23 := bits.Add64(0, x14, x21)
	x24, x25 := bits.Add64(0, x16, x23)
	x26, x27 := bits.Add64(x20, arg1[1], 0)
	x28, x29 := bits.Add64(x22, 0, x27)
	x30, x31 := bits.Add64(x24, 0, x29)
	_, x32 := bits.Mul64(x26, 0xccd1c8aaee00bc4f)
	x35, x34 := bits.Mul64(x32, 0xffffffff00000000)
	x37, x36 := bits.Mul64(x32, 0xffffffffffffffff)
	x39, x38 := bits.Mul64(x32, 0xbce6faada7179e84)
	x41, x40 := bits.Mul64(x32, 0xf3b9cac2fc632551)
	x42, x43 := bits.Add64(x41, x38, 0)
	x44, x45 := bits.Add64(x39, x36, x43)
	x46, x47 := bits.Add64(x37, x34, x45)
	_, x49 := bits.Add64(x26, x40, 0)
	x50, x51 := bits.Add64(x28, x42, x49)
	x52, x53 := bits.Add64(x30, x44, x51)
	x54, x55 := bits.Add64(x31+(x25+(x17+x5)), x46, x53)
	x56, x57 := bits.Add64(x50, arg1[2], 0)
	x58, x59 := bits.Add64(x52, 0, x57)
	x60, x61 := bits.Add64(x54, 0, x59)
	_, x62 := bits.Mul64(x56, 0xccd1c8aaee00bc4f)
	x65, x64 := bits.Mul64(x62, 0xffffffff00000000)
	x67, x66 := bits.Mul64(x62, 0xffffffffffffffff)
	x69, x68 := bits.Mul64(x62, 0xbce6faada7179e84)
	x71, x70 := bits.Mul64(x62, 0xf3b9cac2fc632551)
	x72, x73 := bits.Add64(x71, x68, 0)
	x74, x75 := bits.Add64(x69, x66, x73)
	x76, x77 := bits.Add64(x67, x64, x75)
	_, x79 := bits.Add64(x56, x70, 0)
	x80, x81 := bits.Add64(x58, x72, x79)
	x82, x83 := bits.Add64(x60, x74, x81)
	x84, x85 := bits.Add64(x61+(x55+(x47+x35)), x76, x83)
	x86, x87 := bits.Add64(x80, arg1[3], 0)
	x88, x89 := bits.Add64(x82, 0, x87)
	x90, x91 := bits.Add64(x84, 0, x89)
	_, x92 := bits.Mul64(x86, 0xccd1c8aaee00bc4f)
	x95, x94 := bits.Mul64(x92, 0xffffffff00000000)
	x97, x96 := bits.Mul64(x92, 0xffffffffffffffff)
	x99, x98 := bits.Mul64(x92, 0xbce6faada7179e84)
	x101, x100 := bits.Mul64(x92, 0xf3b9cac2fc632551)
	x102, x103 := bits.Add64(x101, x98, 0)
	x104, x105 := bits.Add64(x99, x96, x103)
	x106, x107 := bits.Add64(x97, x94, x105)
	_, x109 := bits.Add64(x86, x100, 0)
	x110, x111 := bits.Add64(x88, x102, x109)
	x112, x113 := bits.Add64(x90, x104, x111)
	x114, x115 := bits.Add64(x91+(x85+(x77+x65)), x106, x113)
	x116 := x115 + (x107 + x95)
	x117, x118 := bits.Sub64(x110, 0xf3b9cac2fc632551, 0)
	x119, x120 := bits.Sub64(x112, 0xbce6faada7179e84, x118)
	x121, x122 := bits.Sub64(x114, 0xffffffffffffffff, x120)
	x123, x124 := bits.Sub64(x116, 0xffffffff00000000, x122)
	mask, _ := bits.Sub64(0, 0, x124)
	out1[0] = x117&^mask | x110&mask
	out1[1] = x119&^mask | x112&mask
	out1[2] = x121&^mask | x114&mask
	out1[3] = x123&^mask | x116&mask
}

// p256OrdToMontgomery translates a field element into the Montgomery domain.
func p256OrdToMontgomery(out1 *p256OrdMontElement, arg1 *p256OrdElement) {
	x1 := arg1[1]
	x2 := arg1[2]
	x3 := arg1[3]
	x4 := arg1[0]
	x6, x5 := bits.Mul64(x4, 0x66e12d94f3d95620)
	x8, x7 := bits.Mul64(x4, 0x2845b2392b6bec59)
	x10, x9 := bits.Mul64(x4, 0x4699799c49bd6fa6)
	x12, x11 := bits.Mul64(x4, 0x83244c95be79eea2)
	x13, x14 := bits.Add64(x12, x9, 0)
	x15, x16 := bits.Add64(x10, x7, x14)
	x17, x18 := bits.Add64(x8, x5, x16)
	_, x19 := bits.Mul64(x11, 0xccd1c8aaee00bc4f)
	x22, x21 := bits.Mul64(x19, 0xffffffff00000000)
	x24, x23 := bits.Mul64(x19, 0xffffffffffffffff)
	x26, x25 := bits.Mul64(x19, 0xbce6faada7179e84)
	x28, x27 := bits.Mul64(x19, 0xf3b9cac2fc632551)
	x29, x30 := bits.Add64(x28, x25, 0)
	x31, x32 := bits.Add64(x26, x23, x30)
	x33, x34 := bits.Add64(x24, x21, x32)
	_, x36 := bits.Add64(x11, x27, 0)
	x37, x38 := bits.Add64(x13, x29, x36)
	x39, x40 := bits.Add64(x15, x31, x38)
	x41, x42 := bits.Add64(x17, x33, x40)
	x43, x44 := bits.Add64(x18+x6, x34+x22, x42)
	x46, x45 := bits.Mul64(x1, 0x66e12d94f3d95620)
	x48, x47 := bits.Mul64(x1, 0x2845b2392b6bec59)
	x50, x49 := bits.Mul64(x1, 0x4699799c49bd6fa6)
	x52, x51 := bits.Mul64(x1, 0x83244c95be79eea2)
	x53, x54 := bits.Add64(x52, x49, 0)
	x55, x56 := bits.Add64(x50, x47, x54)
	x57, x58 := bits.Add64(x48, x45, x56)
	x59, x60 := bits.Add64(x37, x51, 0)
	x61, x62 := bits.Add64(x39, x53, x60)
	x63, x64 := bits.Add64(x41, x55, x62)
	x65, x66 := bits.Add64(x43, x57, x64)
	_, x67 := bits.Mul64(x59, 0xccd1c8aaee00bc4f)
	x70, x69 := bits.Mul64(x67, 0xffffffff00000000)
	x72, x71 := bits.Mul64(x67, 0xffffffffffffffff)
	x74, x73 := bits.Mul64(x67, 0xbce6faada7179e84)
	x76, x75 := bits.Mul64(x67, 0xf3b9cac2fc632551)
	x77, x78 := bits.Add64(x76, x73, 0)
	x79, x80 := bits.Add64(x74, x71, x78)
	x81, x82 := bits.Add64(x72, x69, x80)
	_, x84 := bits.Add64(x59, x75, 0)
	x85, x86 := bits.Add64(x61, x77, x84)
	x87, x88 := bits.Add64(x63, x79, x86)
	x89, x90 := bits.Add64(x65, x81, x88)
	x91, x92 := bits.Add64(x66+x44+(x58+x46), x82+x70, x90)
	x94, x93 := bits.Mul64(x2, 0x66e12d94f3d95620)
	x96, x95 := bits.Mul64(x2, 0x2845b2392b6bec59)
	x98, x97 := bits.Mul64(x2, 0x4699799c49bd6fa6)
	x100, x99 := bits.Mul64(x2, 0x83244c95be79eea2)
	x101, x102 := bits.Add64(x100, x97, 0)
	x103, x104 := bits.Add64(x98, x95, x102)
	x105, x106 := bits.Add64(x96, x93, x104)
	x107, x108 := bits.Add64(x85, x99, 0)
	x109, x110 := bits.Add64(x87, x101, x108)
	x111, x112 := bits.Add64(x89, x103, x110)
	x113, x114 := bits.Add64(x91, x105, x112)
	_, x115 := bits.Mul64(x107, 0xccd1c8aaee00bc4f)
	x118, x117 := bits.Mul64(x115, 0xffffffff00000000)
	x120, x119 := bits.Mul64(x115, 0xffffffffffffffff)
	x122, x121 := bits.Mul64(x115, 0xbce6faada7179e84)
	x124, x123 := bits.Mul64(x115, 0xf3b9cac2fc632551)
	x125, x126 := bits.Add64(x124, x121, 0)
	x127, x128 := bits.Add64(x122, x119, x126)
	x129, x130 := bits.Add64(x120, x117, x128)
	_, x132 := bits.Add64(x107, x123, 0)
	x133, x134 := bits.Add64(x109, x125, x132)
	x135, x136 := bits.Add64(x111, x127, x134)
	x137, x138 := bits.Add64(x113, x129, x136)
	x139, x140 := bits.Add64(x114+x92+(x106+x94), x130+x118, x138)
	x142, x141 := bits.Mul64(x3, 0x66e12d94f3d95620)
	x144, x143 := bits.Mul64(x3, 0x2845b2392b6bec59)
	x146, x145 := bits.Mul64(x3, 0x4699799c49bd6fa6)
	x148, x147 := bits.Mul64(x3, 0x83244c95be79eea2)
	x149, x150 := bits.Add64(x148, x145, 0)
	x151, x152 := bits.Add64(x146, x143, x150)
	x153, x154 := bits.Add64(x144, x141, x152)
	x155, x156 := bits.Add64(x133, x147, 0)
	x157, x158 := bits.Add64(x135, x149, x156)
	x159, x160 := bits.Add64(x137, x151, x158)
	x161, x162 := bits.Add64(x139, x153, x160)
	_, x163 := bits.Mul64(x155, 0xccd1c8aaee00bc4f)
	x166, x165 := bits.Mul64(x163, 0xffffffff00000000)
	x168, x167 := bits.Mul64(x163, 0xffffffffffffffff)
	x170, x169 := bits.Mul64(x163, 0xbce6faada7179e84)
	x172, x171 := bits.Mul64(x163, 0xf3b9cac2fc632551)
	x173, x174 := bits.Add64(x172, x169, 0)
	x175, x176 := bits.Add64(x170, x167, x174)
	x177, x178 := bits.Add64(x168, x165, x176)
	_, x180 := bits.Add64(x155, x171, 0)
	x181, x182 := bits.Add64(x157, x173, x180)
	x183, x184 := bits.Add64(x159, x175, x182)
	x185, x186 := bits.Add64(x161, x177, x184)
	x187, x188 := bits.Add64(x162+x140+(x154+x142), x178+x166, x186)
	x189, x190 := bits.Sub64(x181, 0xf3b9cac2fc632551, 0)
	x191, x192 := bits.Sub64(x183, 0xbce6faada7179e84, x190)
	x193, x194 := bits.Sub64(x185, 0xffffffffffffffff, x192)
	x195, x196 := bits.Sub64(x187, 0xffffffff00000000, x194)
	_, x198 := bits.Sub64(x188, 0, x196)
	mask, _ := bits.Sub64(0, 0, x198)
	out1[0] = x189&^mask | x181&mask
	out1[1] = x191&^mask | x183&mask
	out1[2] = x193&^mask | x185&mask
	out1[3] = x195&^mask | x187&mask
}


// ---- vendored from crypto/internal/fips140/nistec/p256_table.go ----




// ---- vendored from crypto/internal/fips140/nistec/p384.go ----




// p384ElementLength is the length of an element of the base or scalar field,
// which have the same bytes length for all NIST P curves.
const p384ElementLength = 48

// P384Point is a P384 point. The zero value is NOT valid.
type P384Point struct {
	// The point is represented in projective coordinates (X:Y:Z),
	// where x = X/Z and y = Y/Z.
	x, y, z *fiat.P384Element
}

// NewP384Point returns a new P384Point representing the point at infinity point.
func NewP384Point() *P384Point {
	return &P384Point{
		x: new(fiat.P384Element),
		y: new(fiat.P384Element).One(),
		z: new(fiat.P384Element),
	}
}

// SetGenerator sets p to the canonical generator and returns p.
func (p *P384Point) SetGenerator() *P384Point {
	p.x.SetBytes([]byte{0xaa, 0x87, 0xca, 0x22, 0xbe, 0x8b, 0x5, 0x37, 0x8e, 0xb1, 0xc7, 0x1e, 0xf3, 0x20, 0xad, 0x74, 0x6e, 0x1d, 0x3b, 0x62, 0x8b, 0xa7, 0x9b, 0x98, 0x59, 0xf7, 0x41, 0xe0, 0x82, 0x54, 0x2a, 0x38, 0x55, 0x2, 0xf2, 0x5d, 0xbf, 0x55, 0x29, 0x6c, 0x3a, 0x54, 0x5e, 0x38, 0x72, 0x76, 0xa, 0xb7})
	p.y.SetBytes([]byte{0x36, 0x17, 0xde, 0x4a, 0x96, 0x26, 0x2c, 0x6f, 0x5d, 0x9e, 0x98, 0xbf, 0x92, 0x92, 0xdc, 0x29, 0xf8, 0xf4, 0x1d, 0xbd, 0x28, 0x9a, 0x14, 0x7c, 0xe9, 0xda, 0x31, 0x13, 0xb5, 0xf0, 0xb8, 0xc0, 0xa, 0x60, 0xb1, 0xce, 0x1d, 0x7e, 0x81, 0x9d, 0x7a, 0x43, 0x1d, 0x7c, 0x90, 0xea, 0xe, 0x5f})
	p.z.One()
	return p
}

// Set sets p = q and returns p.
func (p *P384Point) Set(q *P384Point) *P384Point {
	p.x.Set(q.x)
	p.y.Set(q.y)
	p.z.Set(q.z)
	return p
}

// SetBytes sets p to the compressed, uncompressed, or infinity value encoded in
// b, as specified in SEC 1, Version 2.0, Section 2.3.4. If the point is not on
// the curve, it returns nil and an error, and the receiver is unchanged.
// Otherwise, it returns p.
type __mrP384Point_SetBytesPair struct {
	v0 *P384Point
	v1 error
}

func __mrP384Point_SetBytes(p *P384Point, b []byte) __mrP384Point_SetBytesPair {
	switch {
	// Point at infinity.
	case len(b) == 1 && b[0] == 0:
		return __mrP384Point_SetBytesPair{p.Set(NewP384Point()), nil}

	// Uncompressed form.
	case len(b) == 1+2*p384ElementLength && b[0] == 4:
		x, err := new(fiat.P384Element).SetBytes(b[1 : 1+p384ElementLength])
		if err != nil {
			return __mrP384Point_SetBytesPair{nil, err}
		}
		y, err := new(fiat.P384Element).SetBytes(b[1+p384ElementLength:])
		if err != nil {
			return __mrP384Point_SetBytesPair{nil, err}
		}
		if err := p384CheckOnCurve(x, y); err != nil {
			return __mrP384Point_SetBytesPair{nil, err}
		}
		p.x.Set(x)
		p.y.Set(y)
		p.z.One()
		return __mrP384Point_SetBytesPair{p, nil}

	// Compressed form.
	case len(b) == 1+p384ElementLength && (b[0] == 2 || b[0] == 3):
		x, err := new(fiat.P384Element).SetBytes(b[1:])
		if err != nil {
			return __mrP384Point_SetBytesPair{nil, err}
		}

		// y² = x³ - 3x + b
		y := p384Polynomial(new(fiat.P384Element), x)
		if !p384Sqrt(y, y) {
			return __mrP384Point_SetBytesPair{nil, errors.New("invalid P384 compressed point encoding")}
		}

		// Select the positive or negative root, as indicated by the least
		// significant bit, based on the encoding type byte.
		otherRoot := new(fiat.P384Element)
		otherRoot.Sub(otherRoot, y)
		cond := y.Bytes()[p384ElementLength-1]&1 ^ b[0]&1
		y.Select(otherRoot, y, int(cond))

		p.x.Set(x)
		p.y.Set(y)
		p.z.One()
		return __mrP384Point_SetBytesPair{p, nil}

	default:
		return __mrP384Point_SetBytesPair{nil, errors.New("invalid P384 point encoding")}
	}
}

func (p *P384Point) SetBytes(b []byte) (*P384Point, error) {
	panic("crypto/internal/fips140/nistec: SetBytes: multi-result desugar")
}

var _p384B *fiat.P384Element
var _p384BOnce bool

func p384B() *fiat.P384Element {
	if !_p384BOnce {
		_p384B, _ = new(fiat.P384Element).SetBytes([]byte{0xb3, 0x31, 0x2f, 0xa7, 0xe2, 0x3e, 0xe7, 0xe4, 0x98, 0x8e, 0x5, 0x6b, 0xe3, 0xf8, 0x2d, 0x19, 0x18, 0x1d, 0x9c, 0x6e, 0xfe, 0x81, 0x41, 0x12, 0x3, 0x14, 0x8, 0x8f, 0x50, 0x13, 0x87, 0x5a, 0xc6, 0x56, 0x39, 0x8d, 0x8a, 0x2e, 0xd1, 0x9d, 0x2a, 0x85, 0xc8, 0xed, 0xd3, 0xec, 0x2a, 0xef})
	
	_p384BOnce = true
}
	return _p384B
}

// p384Polynomial sets y2 to x³ - 3x + b, and returns y2.
func p384Polynomial(y2, x *fiat.P384Element) *fiat.P384Element {
	y2.Square(x)
	y2.Mul(y2, x)

	threeX := new(fiat.P384Element).Add(x, x)
	threeX.Add(threeX, x)
	y2.Sub(y2, threeX)

	return y2.Add(y2, p384B())
}

func p384CheckOnCurve(x, y *fiat.P384Element) error {
	// y² = x³ - 3x + b
	rhs := p384Polynomial(new(fiat.P384Element), x)
	lhs := new(fiat.P384Element).Square(y)
	if rhs.Equal(lhs) != 1 {
		return errors.New("P384 point not on curve")
	}
	return nil
}

// Bytes returns the uncompressed or infinity encoding of p, as specified in
// SEC 1, Version 2.0, Section 2.3.3. Note that the encoding of the point at
// infinity is shorter than all other encodings.
func (p *P384Point) Bytes() []byte {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var out [1 + 2*p384ElementLength]byte
	return p.bytes(&out)
}

func (p *P384Point) bytes(out *[1 + 2*p384ElementLength]byte) []byte {
	if p.z.IsZero() == 1 {
		return append(out[:0], 0)
	}

	zinv := new(fiat.P384Element).Invert(p.z)
	x := new(fiat.P384Element).Mul(p.x, zinv)
	y := new(fiat.P384Element).Mul(p.y, zinv)

	buf := append(out[:0], 4)
	buf = append(buf, x.Bytes()...)
	buf = append(buf, y.Bytes()...)
	return buf
}

// BytesX returns the encoding of the x-coordinate of p, as specified in SEC 1,
// Version 2.0, Section 2.3.5, or an error if p is the point at infinity.
type __mrP384Point_BytesXPair struct {
	v0 []byte
	v1 error
}

func __mrP384Point_BytesX(p *P384Point) __mrP384Point_BytesXPair {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var out [p384ElementLength]byte
	__fw0, __fw1 := p.bytesX(&out)
	return __mrP384Point_BytesXPair{__fw0, __fw1}
}

func (p *P384Point) BytesX() ([]byte, error) {
	panic("crypto/internal/fips140/nistec: BytesX: multi-result desugar")
}

type __mrP384Point_bytesXPair struct {
	v0 []byte
	v1 error
}

func __mrP384Point_bytesX(p *P384Point, out *[p384ElementLength]byte) __mrP384Point_bytesXPair {
	if p.z.IsZero() == 1 {
		return __mrP384Point_bytesXPair{nil, errors.New("P384 point is the point at infinity")}
	}

	zinv := new(fiat.P384Element).Invert(p.z)
	x := new(fiat.P384Element).Mul(p.x, zinv)

	return __mrP384Point_bytesXPair{append(out[:0], x.Bytes()...), nil}
}

func (p *P384Point) bytesX(out *[p384ElementLength]byte) ([]byte, error) {
	panic("crypto/internal/fips140/nistec: bytesX: multi-result desugar")
}

// BytesCompressed returns the compressed or infinity encoding of p, as
// specified in SEC 1, Version 2.0, Section 2.3.3. Note that the encoding of the
// point at infinity is shorter than all other encodings.
func (p *P384Point) BytesCompressed() []byte {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var out [1 + p384ElementLength]byte
	return p.bytesCompressed(&out)
}

func (p *P384Point) bytesCompressed(out *[1 + p384ElementLength]byte) []byte {
	if p.z.IsZero() == 1 {
		return append(out[:0], 0)
	}

	zinv := new(fiat.P384Element).Invert(p.z)
	x := new(fiat.P384Element).Mul(p.x, zinv)
	y := new(fiat.P384Element).Mul(p.y, zinv)

	// Encode the sign of the y coordinate (indicated by the least significant
	// bit) as the encoding type (2 or 3).
	buf := append(out[:0], 2)
	buf[0] |= y.Bytes()[p384ElementLength-1] & 1
	buf = append(buf, x.Bytes()...)
	return buf
}

// Add sets q = p1 + p2, and returns q. The points may overlap.
func (q *P384Point) Add(p1, p2 *P384Point) *P384Point {
	// Complete addition formula for a = -3 from "Complete addition formulas for
	// prime order elliptic curves" (https://eprint.iacr.org/2015/1060), §A.2.

	t0 := new(fiat.P384Element).Mul(p1.x, p2.x)  // t0 := X1 * X2
	t1 := new(fiat.P384Element).Mul(p1.y, p2.y)  // t1 := Y1 * Y2
	t2 := new(fiat.P384Element).Mul(p1.z, p2.z)  // t2 := Z1 * Z2
	t3 := new(fiat.P384Element).Add(p1.x, p1.y)  // t3 := X1 + Y1
	t4 := new(fiat.P384Element).Add(p2.x, p2.y)  // t4 := X2 + Y2
	t3.Mul(t3, t4)                               // t3 := t3 * t4
	t4.Add(t0, t1)                               // t4 := t0 + t1
	t3.Sub(t3, t4)                               // t3 := t3 - t4
	t4.Add(p1.y, p1.z)                           // t4 := Y1 + Z1
	x3 := new(fiat.P384Element).Add(p2.y, p2.z)  // X3 := Y2 + Z2
	t4.Mul(t4, x3)                               // t4 := t4 * X3
	x3.Add(t1, t2)                               // X3 := t1 + t2
	t4.Sub(t4, x3)                               // t4 := t4 - X3
	x3.Add(p1.x, p1.z)                           // X3 := X1 + Z1
	y3 := new(fiat.P384Element).Add(p2.x, p2.z)  // Y3 := X2 + Z2
	x3.Mul(x3, y3)                               // X3 := X3 * Y3
	y3.Add(t0, t2)                               // Y3 := t0 + t2
	y3.Sub(x3, y3)                               // Y3 := X3 - Y3
	z3 := new(fiat.P384Element).Mul(p384B(), t2) // Z3 := b * t2
	x3.Sub(y3, z3)                               // X3 := Y3 - Z3
	z3.Add(x3, x3)                               // Z3 := X3 + X3
	x3.Add(x3, z3)                               // X3 := X3 + Z3
	z3.Sub(t1, x3)                               // Z3 := t1 - X3
	x3.Add(t1, x3)                               // X3 := t1 + X3
	y3.Mul(p384B(), y3)                          // Y3 := b * Y3
	t1.Add(t2, t2)                               // t1 := t2 + t2
	t2.Add(t1, t2)                               // t2 := t1 + t2
	y3.Sub(y3, t2)                               // Y3 := Y3 - t2
	y3.Sub(y3, t0)                               // Y3 := Y3 - t0
	t1.Add(y3, y3)                               // t1 := Y3 + Y3
	y3.Add(t1, y3)                               // Y3 := t1 + Y3
	t1.Add(t0, t0)                               // t1 := t0 + t0
	t0.Add(t1, t0)                               // t0 := t1 + t0
	t0.Sub(t0, t2)                               // t0 := t0 - t2
	t1.Mul(t4, y3)                               // t1 := t4 * Y3
	t2.Mul(t0, y3)                               // t2 := t0 * Y3
	y3.Mul(x3, z3)                               // Y3 := X3 * Z3
	y3.Add(y3, t2)                               // Y3 := Y3 + t2
	x3.Mul(t3, x3)                               // X3 := t3 * X3
	x3.Sub(x3, t1)                               // X3 := X3 - t1
	z3.Mul(t4, z3)                               // Z3 := t4 * Z3
	t1.Mul(t3, t0)                               // t1 := t3 * t0
	z3.Add(z3, t1)                               // Z3 := Z3 + t1

	q.x.Set(x3)
	q.y.Set(y3)
	q.z.Set(z3)
	return q
}

// Double sets q = p + p, and returns q. The points may overlap.
func (q *P384Point) Double(p *P384Point) *P384Point {
	// Complete addition formula for a = -3 from "Complete addition formulas for
	// prime order elliptic curves" (https://eprint.iacr.org/2015/1060), §A.2.

	t0 := new(fiat.P384Element).Square(p.x)      // t0 := X ^ 2
	t1 := new(fiat.P384Element).Square(p.y)      // t1 := Y ^ 2
	t2 := new(fiat.P384Element).Square(p.z)      // t2 := Z ^ 2
	t3 := new(fiat.P384Element).Mul(p.x, p.y)    // t3 := X * Y
	t3.Add(t3, t3)                               // t3 := t3 + t3
	z3 := new(fiat.P384Element).Mul(p.x, p.z)    // Z3 := X * Z
	z3.Add(z3, z3)                               // Z3 := Z3 + Z3
	y3 := new(fiat.P384Element).Mul(p384B(), t2) // Y3 := b * t2
	y3.Sub(y3, z3)                               // Y3 := Y3 - Z3
	x3 := new(fiat.P384Element).Add(y3, y3)      // X3 := Y3 + Y3
	y3.Add(x3, y3)                               // Y3 := X3 + Y3
	x3.Sub(t1, y3)                               // X3 := t1 - Y3
	y3.Add(t1, y3)                               // Y3 := t1 + Y3
	y3.Mul(x3, y3)                               // Y3 := X3 * Y3
	x3.Mul(x3, t3)                               // X3 := X3 * t3
	t3.Add(t2, t2)                               // t3 := t2 + t2
	t2.Add(t2, t3)                               // t2 := t2 + t3
	z3.Mul(p384B(), z3)                          // Z3 := b * Z3
	z3.Sub(z3, t2)                               // Z3 := Z3 - t2
	z3.Sub(z3, t0)                               // Z3 := Z3 - t0
	t3.Add(z3, z3)                               // t3 := Z3 + Z3
	z3.Add(z3, t3)                               // Z3 := Z3 + t3
	t3.Add(t0, t0)                               // t3 := t0 + t0
	t0.Add(t3, t0)                               // t0 := t3 + t0
	t0.Sub(t0, t2)                               // t0 := t0 - t2
	t0.Mul(t0, z3)                               // t0 := t0 * Z3
	y3.Add(y3, t0)                               // Y3 := Y3 + t0
	t0.Mul(p.y, p.z)                             // t0 := Y * Z
	t0.Add(t0, t0)                               // t0 := t0 + t0
	z3.Mul(t0, z3)                               // Z3 := t0 * Z3
	x3.Sub(x3, z3)                               // X3 := X3 - Z3
	z3.Mul(t0, t1)                               // Z3 := t0 * t1
	z3.Add(z3, z3)                               // Z3 := Z3 + Z3
	z3.Add(z3, z3)                               // Z3 := Z3 + Z3

	q.x.Set(x3)
	q.y.Set(y3)
	q.z.Set(z3)
	return q
}

// Select sets q to p1 if cond == 1, and to p2 if cond == 0.
func (q *P384Point) Select(p1, p2 *P384Point, cond int) *P384Point {
	q.x.Select(p1.x, p2.x, cond)
	q.y.Select(p1.y, p2.y, cond)
	q.z.Select(p1.z, p2.z, cond)
	return q
}

// A p384Table holds the first 15 multiples of a point at offset -1, so [1]P
// is at table[0], [15]P is at table[14], and [0]P is implicitly the identity
// point.
type p384Table [15]*P384Point

// Select selects the n-th multiple of the table base point into p. It works in
// constant time by iterating over every entry of the table. n must be in [0, 15].
func (table *p384Table) Select(p *P384Point, n uint8) {
	if n >= 16 {
		panic("nistec: internal error: p384Table called with out-of-bounds value")
	}
	p.Set(NewP384Point())
	for i := uint8(1); i < 16; i++ {
		cond := constanttime.ByteEq(i, n)
		p.Select(table[i-1], p, cond)
	}
}

// ScalarMult sets p = scalar * q, and returns p.
type __mrP384Point_ScalarMultPair struct {
	v0 *P384Point
	v1 error
}

func __mrP384Point_ScalarMult(p *P384Point, q *P384Point, scalar []byte) __mrP384Point_ScalarMultPair {
	// Compute a p384Table for the base point q. The explicit NewP384Point
	// calls get inlined, letting the allocations live on the stack.
	var table = p384Table{NewP384Point(), NewP384Point(), NewP384Point(),
		NewP384Point(), NewP384Point(), NewP384Point(), NewP384Point(),
		NewP384Point(), NewP384Point(), NewP384Point(), NewP384Point(),
		NewP384Point(), NewP384Point(), NewP384Point(), NewP384Point()}
	table[0].Set(q)
	for i := 1; i < 15; i += 2 {
		table[i].Double(table[i/2])
		table[i+1].Add(table[i], q)
	}

	// Instead of doing the classic double-and-add chain, we do it with a
	// four-bit window: we double four times, and then add [0-15]P.
	t := NewP384Point()
	p.Set(NewP384Point())
	for i, byte := range scalar {
		// No need to double on the first iteration, as p is the identity at
		// this point, and [N]∞ = ∞.
		if i != 0 {
			p.Double(p)
			p.Double(p)
			p.Double(p)
			p.Double(p)
		}

		windowValue := byte >> 4
		table.Select(t, windowValue)
		p.Add(p, t)

		p.Double(p)
		p.Double(p)
		p.Double(p)
		p.Double(p)

		windowValue = byte & 0b1111
		table.Select(t, windowValue)
		p.Add(p, t)
	}

	return __mrP384Point_ScalarMultPair{p, nil}
}

func (p *P384Point) ScalarMult(q *P384Point, scalar []byte) (*P384Point, error) {
	panic("crypto/internal/fips140/nistec: ScalarMult: multi-result desugar")
}

var p384GeneratorTable *[p384ElementLength * 2]p384Table
var p384GeneratorTableOnce bool

// generatorTable returns a sequence of p384Tables. The first table contains
// multiples of G. Each successive table is the previous table doubled four
// times.
func (p *P384Point) generatorTable() *[p384ElementLength * 2]p384Table {
	if !p384GeneratorTableOnce {
		p384GeneratorTable = new([p384ElementLength * 2]p384Table)
		base := NewP384Point().SetGenerator()
		for i := 0; i < p384ElementLength*2; i++ {
			p384GeneratorTable[i][0] = NewP384Point().Set(base)
			for j := 1; j < 15; j++ {
				p384GeneratorTable[i][j] = NewP384Point().Add(p384GeneratorTable[i][j-1], base)
			}
			base.Double(base)
			base.Double(base)
			base.Double(base)
			base.Double(base)
		}
	
	p384GeneratorTableOnce = true
}
	return p384GeneratorTable
}

// ScalarBaseMult sets p = scalar * B, where B is the canonical generator, and
// returns p.
type __mrP384Point_ScalarBaseMultPair struct {
	v0 *P384Point
	v1 error
}

func __mrP384Point_ScalarBaseMult(p *P384Point, scalar []byte) __mrP384Point_ScalarBaseMultPair {
	if len(scalar) != p384ElementLength {
		return __mrP384Point_ScalarBaseMultPair{nil, errors.New("invalid scalar length")}
	}
	tables := p.generatorTable()

	// This is also a scalar multiplication with a four-bit window like in
	// ScalarMult, but in this case the doublings are precomputed. The value
	// [windowValue]G added at iteration k would normally get doubled
	// (totIterations-k)×4 times, but with a larger precomputation we can
	// instead add [2^((totIterations-k)×4)][windowValue]G and avoid the
	// doublings between iterations.
	t := NewP384Point()
	p.Set(NewP384Point())
	tableIndex := len(tables) - 1
	for _, byte := range scalar {
		windowValue := byte >> 4
		tables[tableIndex].Select(t, windowValue)
		p.Add(p, t)
		tableIndex--

		windowValue = byte & 0b1111
		tables[tableIndex].Select(t, windowValue)
		p.Add(p, t)
		tableIndex--
	}

	return __mrP384Point_ScalarBaseMultPair{p, nil}
}

func (p *P384Point) ScalarBaseMult(scalar []byte) (*P384Point, error) {
	panic("crypto/internal/fips140/nistec: ScalarBaseMult: multi-result desugar")
}

// p384Sqrt sets e to a square root of x. If x is not a square, p384Sqrt returns
// false and e is unchanged. e and x can overlap.
func p384Sqrt(e, x *fiat.P384Element) (isSquare bool) {
	candidate := new(fiat.P384Element)
	p384SqrtCandidate(candidate, x)
	square := new(fiat.P384Element).Square(candidate)
	if square.Equal(x) != 1 {
		return false
	}
	e.Set(candidate)
	return true
}

// p384SqrtCandidate sets z to a square root candidate for x. z and x must not overlap.
func p384SqrtCandidate(z, x *fiat.P384Element) {
	// Since p = 3 mod 4, exponentiation by (p + 1) / 4 yields a square root candidate.
	//
	// The sequence of 14 multiplications and 381 squarings is derived from the
	// following addition chain generated with github.com/mmcloughlin/addchain v0.4.0.
	//
	//	_10      = 2*1
	//	_11      = 1 + _10
	//	_110     = 2*_11
	//	_111     = 1 + _110
	//	_111000  = _111 << 3
	//	_111111  = _111 + _111000
	//	_1111110 = 2*_111111
	//	_1111111 = 1 + _1111110
	//	x12      = _1111110 << 5 + _111111
	//	x24      = x12 << 12 + x12
	//	x31      = x24 << 7 + _1111111
	//	x32      = 2*x31 + 1
	//	x63      = x32 << 31 + x31
	//	x126     = x63 << 63 + x63
	//	x252     = x126 << 126 + x126
	//	x255     = x252 << 3 + _111
	//	return     ((x255 << 33 + x32) << 64 + 1) << 30
	//
	var t0 = new(fiat.P384Element)
	var t1 = new(fiat.P384Element)
	var t2 = new(fiat.P384Element)

	z.Square(x)
	z.Mul(x, z)
	z.Square(z)
	t0.Mul(x, z)
	z.Square(t0)
	for s := 1; s < 3; s++ {
		z.Square(z)
	}
	t1.Mul(t0, z)
	t2.Square(t1)
	z.Mul(x, t2)
	for s := 0; s < 5; s++ {
		t2.Square(t2)
	}
	t1.Mul(t1, t2)
	t2.Square(t1)
	for s := 1; s < 12; s++ {
		t2.Square(t2)
	}
	t1.Mul(t1, t2)
	for s := 0; s < 7; s++ {
		t1.Square(t1)
	}
	t1.Mul(z, t1)
	z.Square(t1)
	z.Mul(x, z)
	t2.Square(z)
	for s := 1; s < 31; s++ {
		t2.Square(t2)
	}
	t1.Mul(t1, t2)
	t2.Square(t1)
	for s := 1; s < 63; s++ {
		t2.Square(t2)
	}
	t1.Mul(t1, t2)
	t2.Square(t1)
	for s := 1; s < 126; s++ {
		t2.Square(t2)
	}
	t1.Mul(t1, t2)
	for s := 0; s < 3; s++ {
		t1.Square(t1)
	}
	t0.Mul(t0, t1)
	for s := 0; s < 33; s++ {
		t0.Square(t0)
	}
	z.Mul(z, t0)
	for s := 0; s < 64; s++ {
		z.Square(z)
	}
	z.Mul(x, z)
	for s := 0; s < 30; s++ {
		z.Square(z)
	}
}


// ---- vendored from crypto/internal/fips140/nistec/p521.go ----




// p521ElementLength is the length of an element of the base or scalar field,
// which have the same bytes length for all NIST P curves.
const p521ElementLength = 66

// P521Point is a P521 point. The zero value is NOT valid.
type P521Point struct {
	// The point is represented in projective coordinates (X:Y:Z),
	// where x = X/Z and y = Y/Z.
	x, y, z *fiat.P521Element
}

// NewP521Point returns a new P521Point representing the point at infinity point.
func NewP521Point() *P521Point {
	return &P521Point{
		x: new(fiat.P521Element),
		y: new(fiat.P521Element).One(),
		z: new(fiat.P521Element),
	}
}

// SetGenerator sets p to the canonical generator and returns p.
func (p *P521Point) SetGenerator() *P521Point {
	p.x.SetBytes([]byte{0x0, 0xc6, 0x85, 0x8e, 0x6, 0xb7, 0x4, 0x4, 0xe9, 0xcd, 0x9e, 0x3e, 0xcb, 0x66, 0x23, 0x95, 0xb4, 0x42, 0x9c, 0x64, 0x81, 0x39, 0x5, 0x3f, 0xb5, 0x21, 0xf8, 0x28, 0xaf, 0x60, 0x6b, 0x4d, 0x3d, 0xba, 0xa1, 0x4b, 0x5e, 0x77, 0xef, 0xe7, 0x59, 0x28, 0xfe, 0x1d, 0xc1, 0x27, 0xa2, 0xff, 0xa8, 0xde, 0x33, 0x48, 0xb3, 0xc1, 0x85, 0x6a, 0x42, 0x9b, 0xf9, 0x7e, 0x7e, 0x31, 0xc2, 0xe5, 0xbd, 0x66})
	p.y.SetBytes([]byte{0x1, 0x18, 0x39, 0x29, 0x6a, 0x78, 0x9a, 0x3b, 0xc0, 0x4, 0x5c, 0x8a, 0x5f, 0xb4, 0x2c, 0x7d, 0x1b, 0xd9, 0x98, 0xf5, 0x44, 0x49, 0x57, 0x9b, 0x44, 0x68, 0x17, 0xaf, 0xbd, 0x17, 0x27, 0x3e, 0x66, 0x2c, 0x97, 0xee, 0x72, 0x99, 0x5e, 0xf4, 0x26, 0x40, 0xc5, 0x50, 0xb9, 0x1, 0x3f, 0xad, 0x7, 0x61, 0x35, 0x3c, 0x70, 0x86, 0xa2, 0x72, 0xc2, 0x40, 0x88, 0xbe, 0x94, 0x76, 0x9f, 0xd1, 0x66, 0x50})
	p.z.One()
	return p
}

// Set sets p = q and returns p.
func (p *P521Point) Set(q *P521Point) *P521Point {
	p.x.Set(q.x)
	p.y.Set(q.y)
	p.z.Set(q.z)
	return p
}

// SetBytes sets p to the compressed, uncompressed, or infinity value encoded in
// b, as specified in SEC 1, Version 2.0, Section 2.3.4. If the point is not on
// the curve, it returns nil and an error, and the receiver is unchanged.
// Otherwise, it returns p.
type __mrP521Point_SetBytesPair struct {
	v0 *P521Point
	v1 error
}

func __mrP521Point_SetBytes(p *P521Point, b []byte) __mrP521Point_SetBytesPair {
	switch {
	// Point at infinity.
	case len(b) == 1 && b[0] == 0:
		return __mrP521Point_SetBytesPair{p.Set(NewP521Point()), nil}

	// Uncompressed form.
	case len(b) == 1+2*p521ElementLength && b[0] == 4:
		x, err := new(fiat.P521Element).SetBytes(b[1 : 1+p521ElementLength])
		if err != nil {
			return __mrP521Point_SetBytesPair{nil, err}
		}
		y, err := new(fiat.P521Element).SetBytes(b[1+p521ElementLength:])
		if err != nil {
			return __mrP521Point_SetBytesPair{nil, err}
		}
		if err := p521CheckOnCurve(x, y); err != nil {
			return __mrP521Point_SetBytesPair{nil, err}
		}
		p.x.Set(x)
		p.y.Set(y)
		p.z.One()
		return __mrP521Point_SetBytesPair{p, nil}

	// Compressed form.
	case len(b) == 1+p521ElementLength && (b[0] == 2 || b[0] == 3):
		x, err := new(fiat.P521Element).SetBytes(b[1:])
		if err != nil {
			return __mrP521Point_SetBytesPair{nil, err}
		}

		// y² = x³ - 3x + b
		y := p521Polynomial(new(fiat.P521Element), x)
		if !p521Sqrt(y, y) {
			return __mrP521Point_SetBytesPair{nil, errors.New("invalid P521 compressed point encoding")}
		}

		// Select the positive or negative root, as indicated by the least
		// significant bit, based on the encoding type byte.
		otherRoot := new(fiat.P521Element)
		otherRoot.Sub(otherRoot, y)
		cond := y.Bytes()[p521ElementLength-1]&1 ^ b[0]&1
		y.Select(otherRoot, y, int(cond))

		p.x.Set(x)
		p.y.Set(y)
		p.z.One()
		return __mrP521Point_SetBytesPair{p, nil}

	default:
		return __mrP521Point_SetBytesPair{nil, errors.New("invalid P521 point encoding")}
	}
}

func (p *P521Point) SetBytes(b []byte) (*P521Point, error) {
	panic("crypto/internal/fips140/nistec: SetBytes: multi-result desugar")
}

var _p521B *fiat.P521Element
var _p521BOnce bool

func p521B() *fiat.P521Element {
	if !_p521BOnce {
		_p521B, _ = new(fiat.P521Element).SetBytes([]byte{0x0, 0x51, 0x95, 0x3e, 0xb9, 0x61, 0x8e, 0x1c, 0x9a, 0x1f, 0x92, 0x9a, 0x21, 0xa0, 0xb6, 0x85, 0x40, 0xee, 0xa2, 0xda, 0x72, 0x5b, 0x99, 0xb3, 0x15, 0xf3, 0xb8, 0xb4, 0x89, 0x91, 0x8e, 0xf1, 0x9, 0xe1, 0x56, 0x19, 0x39, 0x51, 0xec, 0x7e, 0x93, 0x7b, 0x16, 0x52, 0xc0, 0xbd, 0x3b, 0xb1, 0xbf, 0x7, 0x35, 0x73, 0xdf, 0x88, 0x3d, 0x2c, 0x34, 0xf1, 0xef, 0x45, 0x1f, 0xd4, 0x6b, 0x50, 0x3f, 0x0})
	
	_p521BOnce = true
}
	return _p521B
}

// p521Polynomial sets y2 to x³ - 3x + b, and returns y2.
func p521Polynomial(y2, x *fiat.P521Element) *fiat.P521Element {
	y2.Square(x)
	y2.Mul(y2, x)

	threeX := new(fiat.P521Element).Add(x, x)
	threeX.Add(threeX, x)
	y2.Sub(y2, threeX)

	return y2.Add(y2, p521B())
}

func p521CheckOnCurve(x, y *fiat.P521Element) error {
	// y² = x³ - 3x + b
	rhs := p521Polynomial(new(fiat.P521Element), x)
	lhs := new(fiat.P521Element).Square(y)
	if rhs.Equal(lhs) != 1 {
		return errors.New("P521 point not on curve")
	}
	return nil
}

// Bytes returns the uncompressed or infinity encoding of p, as specified in
// SEC 1, Version 2.0, Section 2.3.3. Note that the encoding of the point at
// infinity is shorter than all other encodings.
func (p *P521Point) Bytes() []byte {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var out [1 + 2*p521ElementLength]byte
	return p.bytes(&out)
}

func (p *P521Point) bytes(out *[1 + 2*p521ElementLength]byte) []byte {
	if p.z.IsZero() == 1 {
		return append(out[:0], 0)
	}

	zinv := new(fiat.P521Element).Invert(p.z)
	x := new(fiat.P521Element).Mul(p.x, zinv)
	y := new(fiat.P521Element).Mul(p.y, zinv)

	buf := append(out[:0], 4)
	buf = append(buf, x.Bytes()...)
	buf = append(buf, y.Bytes()...)
	return buf
}

// BytesX returns the encoding of the x-coordinate of p, as specified in SEC 1,
// Version 2.0, Section 2.3.5, or an error if p is the point at infinity.
type __mrP521Point_BytesXPair struct {
	v0 []byte
	v1 error
}

func __mrP521Point_BytesX(p *P521Point) __mrP521Point_BytesXPair {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var out [p521ElementLength]byte
	__fw0, __fw1 := p.bytesX(&out)
	return __mrP521Point_BytesXPair{__fw0, __fw1}
}

func (p *P521Point) BytesX() ([]byte, error) {
	panic("crypto/internal/fips140/nistec: BytesX: multi-result desugar")
}

type __mrP521Point_bytesXPair struct {
	v0 []byte
	v1 error
}

func __mrP521Point_bytesX(p *P521Point, out *[p521ElementLength]byte) __mrP521Point_bytesXPair {
	if p.z.IsZero() == 1 {
		return __mrP521Point_bytesXPair{nil, errors.New("P521 point is the point at infinity")}
	}

	zinv := new(fiat.P521Element).Invert(p.z)
	x := new(fiat.P521Element).Mul(p.x, zinv)

	return __mrP521Point_bytesXPair{append(out[:0], x.Bytes()...), nil}
}

func (p *P521Point) bytesX(out *[p521ElementLength]byte) ([]byte, error) {
	panic("crypto/internal/fips140/nistec: bytesX: multi-result desugar")
}

// BytesCompressed returns the compressed or infinity encoding of p, as
// specified in SEC 1, Version 2.0, Section 2.3.3. Note that the encoding of the
// point at infinity is shorter than all other encodings.
func (p *P521Point) BytesCompressed() []byte {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var out [1 + p521ElementLength]byte
	return p.bytesCompressed(&out)
}

func (p *P521Point) bytesCompressed(out *[1 + p521ElementLength]byte) []byte {
	if p.z.IsZero() == 1 {
		return append(out[:0], 0)
	}

	zinv := new(fiat.P521Element).Invert(p.z)
	x := new(fiat.P521Element).Mul(p.x, zinv)
	y := new(fiat.P521Element).Mul(p.y, zinv)

	// Encode the sign of the y coordinate (indicated by the least significant
	// bit) as the encoding type (2 or 3).
	buf := append(out[:0], 2)
	buf[0] |= y.Bytes()[p521ElementLength-1] & 1
	buf = append(buf, x.Bytes()...)
	return buf
}

// Add sets q = p1 + p2, and returns q. The points may overlap.
func (q *P521Point) Add(p1, p2 *P521Point) *P521Point {
	// Complete addition formula for a = -3 from "Complete addition formulas for
	// prime order elliptic curves" (https://eprint.iacr.org/2015/1060), §A.2.

	t0 := new(fiat.P521Element).Mul(p1.x, p2.x)  // t0 := X1 * X2
	t1 := new(fiat.P521Element).Mul(p1.y, p2.y)  // t1 := Y1 * Y2
	t2 := new(fiat.P521Element).Mul(p1.z, p2.z)  // t2 := Z1 * Z2
	t3 := new(fiat.P521Element).Add(p1.x, p1.y)  // t3 := X1 + Y1
	t4 := new(fiat.P521Element).Add(p2.x, p2.y)  // t4 := X2 + Y2
	t3.Mul(t3, t4)                               // t3 := t3 * t4
	t4.Add(t0, t1)                               // t4 := t0 + t1
	t3.Sub(t3, t4)                               // t3 := t3 - t4
	t4.Add(p1.y, p1.z)                           // t4 := Y1 + Z1
	x3 := new(fiat.P521Element).Add(p2.y, p2.z)  // X3 := Y2 + Z2
	t4.Mul(t4, x3)                               // t4 := t4 * X3
	x3.Add(t1, t2)                               // X3 := t1 + t2
	t4.Sub(t4, x3)                               // t4 := t4 - X3
	x3.Add(p1.x, p1.z)                           // X3 := X1 + Z1
	y3 := new(fiat.P521Element).Add(p2.x, p2.z)  // Y3 := X2 + Z2
	x3.Mul(x3, y3)                               // X3 := X3 * Y3
	y3.Add(t0, t2)                               // Y3 := t0 + t2
	y3.Sub(x3, y3)                               // Y3 := X3 - Y3
	z3 := new(fiat.P521Element).Mul(p521B(), t2) // Z3 := b * t2
	x3.Sub(y3, z3)                               // X3 := Y3 - Z3
	z3.Add(x3, x3)                               // Z3 := X3 + X3
	x3.Add(x3, z3)                               // X3 := X3 + Z3
	z3.Sub(t1, x3)                               // Z3 := t1 - X3
	x3.Add(t1, x3)                               // X3 := t1 + X3
	y3.Mul(p521B(), y3)                          // Y3 := b * Y3
	t1.Add(t2, t2)                               // t1 := t2 + t2
	t2.Add(t1, t2)                               // t2 := t1 + t2
	y3.Sub(y3, t2)                               // Y3 := Y3 - t2
	y3.Sub(y3, t0)                               // Y3 := Y3 - t0
	t1.Add(y3, y3)                               // t1 := Y3 + Y3
	y3.Add(t1, y3)                               // Y3 := t1 + Y3
	t1.Add(t0, t0)                               // t1 := t0 + t0
	t0.Add(t1, t0)                               // t0 := t1 + t0
	t0.Sub(t0, t2)                               // t0 := t0 - t2
	t1.Mul(t4, y3)                               // t1 := t4 * Y3
	t2.Mul(t0, y3)                               // t2 := t0 * Y3
	y3.Mul(x3, z3)                               // Y3 := X3 * Z3
	y3.Add(y3, t2)                               // Y3 := Y3 + t2
	x3.Mul(t3, x3)                               // X3 := t3 * X3
	x3.Sub(x3, t1)                               // X3 := X3 - t1
	z3.Mul(t4, z3)                               // Z3 := t4 * Z3
	t1.Mul(t3, t0)                               // t1 := t3 * t0
	z3.Add(z3, t1)                               // Z3 := Z3 + t1

	q.x.Set(x3)
	q.y.Set(y3)
	q.z.Set(z3)
	return q
}

// Double sets q = p + p, and returns q. The points may overlap.
func (q *P521Point) Double(p *P521Point) *P521Point {
	// Complete addition formula for a = -3 from "Complete addition formulas for
	// prime order elliptic curves" (https://eprint.iacr.org/2015/1060), §A.2.

	t0 := new(fiat.P521Element).Square(p.x)      // t0 := X ^ 2
	t1 := new(fiat.P521Element).Square(p.y)      // t1 := Y ^ 2
	t2 := new(fiat.P521Element).Square(p.z)      // t2 := Z ^ 2
	t3 := new(fiat.P521Element).Mul(p.x, p.y)    // t3 := X * Y
	t3.Add(t3, t3)                               // t3 := t3 + t3
	z3 := new(fiat.P521Element).Mul(p.x, p.z)    // Z3 := X * Z
	z3.Add(z3, z3)                               // Z3 := Z3 + Z3
	y3 := new(fiat.P521Element).Mul(p521B(), t2) // Y3 := b * t2
	y3.Sub(y3, z3)                               // Y3 := Y3 - Z3
	x3 := new(fiat.P521Element).Add(y3, y3)      // X3 := Y3 + Y3
	y3.Add(x3, y3)                               // Y3 := X3 + Y3
	x3.Sub(t1, y3)                               // X3 := t1 - Y3
	y3.Add(t1, y3)                               // Y3 := t1 + Y3
	y3.Mul(x3, y3)                               // Y3 := X3 * Y3
	x3.Mul(x3, t3)                               // X3 := X3 * t3
	t3.Add(t2, t2)                               // t3 := t2 + t2
	t2.Add(t2, t3)                               // t2 := t2 + t3
	z3.Mul(p521B(), z3)                          // Z3 := b * Z3
	z3.Sub(z3, t2)                               // Z3 := Z3 - t2
	z3.Sub(z3, t0)                               // Z3 := Z3 - t0
	t3.Add(z3, z3)                               // t3 := Z3 + Z3
	z3.Add(z3, t3)                               // Z3 := Z3 + t3
	t3.Add(t0, t0)                               // t3 := t0 + t0
	t0.Add(t3, t0)                               // t0 := t3 + t0
	t0.Sub(t0, t2)                               // t0 := t0 - t2
	t0.Mul(t0, z3)                               // t0 := t0 * Z3
	y3.Add(y3, t0)                               // Y3 := Y3 + t0
	t0.Mul(p.y, p.z)                             // t0 := Y * Z
	t0.Add(t0, t0)                               // t0 := t0 + t0
	z3.Mul(t0, z3)                               // Z3 := t0 * Z3
	x3.Sub(x3, z3)                               // X3 := X3 - Z3
	z3.Mul(t0, t1)                               // Z3 := t0 * t1
	z3.Add(z3, z3)                               // Z3 := Z3 + Z3
	z3.Add(z3, z3)                               // Z3 := Z3 + Z3

	q.x.Set(x3)
	q.y.Set(y3)
	q.z.Set(z3)
	return q
}

// Select sets q to p1 if cond == 1, and to p2 if cond == 0.
func (q *P521Point) Select(p1, p2 *P521Point, cond int) *P521Point {
	q.x.Select(p1.x, p2.x, cond)
	q.y.Select(p1.y, p2.y, cond)
	q.z.Select(p1.z, p2.z, cond)
	return q
}

// A p521Table holds the first 15 multiples of a point at offset -1, so [1]P
// is at table[0], [15]P is at table[14], and [0]P is implicitly the identity
// point.
type p521Table [15]*P521Point

// Select selects the n-th multiple of the table base point into p. It works in
// constant time by iterating over every entry of the table. n must be in [0, 15].
func (table *p521Table) Select(p *P521Point, n uint8) {
	if n >= 16 {
		panic("nistec: internal error: p521Table called with out-of-bounds value")
	}
	p.Set(NewP521Point())
	for i := uint8(1); i < 16; i++ {
		cond := constanttime.ByteEq(i, n)
		p.Select(table[i-1], p, cond)
	}
}

// ScalarMult sets p = scalar * q, and returns p.
type __mrP521Point_ScalarMultPair struct {
	v0 *P521Point
	v1 error
}

func __mrP521Point_ScalarMult(p *P521Point, q *P521Point, scalar []byte) __mrP521Point_ScalarMultPair {
	// Compute a p521Table for the base point q. The explicit NewP521Point
	// calls get inlined, letting the allocations live on the stack.
	var table = p521Table{NewP521Point(), NewP521Point(), NewP521Point(),
		NewP521Point(), NewP521Point(), NewP521Point(), NewP521Point(),
		NewP521Point(), NewP521Point(), NewP521Point(), NewP521Point(),
		NewP521Point(), NewP521Point(), NewP521Point(), NewP521Point()}
	table[0].Set(q)
	for i := 1; i < 15; i += 2 {
		table[i].Double(table[i/2])
		table[i+1].Add(table[i], q)
	}

	// Instead of doing the classic double-and-add chain, we do it with a
	// four-bit window: we double four times, and then add [0-15]P.
	t := NewP521Point()
	p.Set(NewP521Point())
	for i, byte := range scalar {
		// No need to double on the first iteration, as p is the identity at
		// this point, and [N]∞ = ∞.
		if i != 0 {
			p.Double(p)
			p.Double(p)
			p.Double(p)
			p.Double(p)
		}

		windowValue := byte >> 4
		table.Select(t, windowValue)
		p.Add(p, t)

		p.Double(p)
		p.Double(p)
		p.Double(p)
		p.Double(p)

		windowValue = byte & 0b1111
		table.Select(t, windowValue)
		p.Add(p, t)
	}

	return __mrP521Point_ScalarMultPair{p, nil}
}

func (p *P521Point) ScalarMult(q *P521Point, scalar []byte) (*P521Point, error) {
	panic("crypto/internal/fips140/nistec: ScalarMult: multi-result desugar")
}

var p521GeneratorTable *[p521ElementLength * 2]p521Table
var p521GeneratorTableOnce bool

// generatorTable returns a sequence of p521Tables. The first table contains
// multiples of G. Each successive table is the previous table doubled four
// times.
func (p *P521Point) generatorTable() *[p521ElementLength * 2]p521Table {
	if !p521GeneratorTableOnce {
		p521GeneratorTable = new([p521ElementLength * 2]p521Table)
		base := NewP521Point().SetGenerator()
		for i := 0; i < p521ElementLength*2; i++ {
			p521GeneratorTable[i][0] = NewP521Point().Set(base)
			for j := 1; j < 15; j++ {
				p521GeneratorTable[i][j] = NewP521Point().Add(p521GeneratorTable[i][j-1], base)
			}
			base.Double(base)
			base.Double(base)
			base.Double(base)
			base.Double(base)
		}
	
	p521GeneratorTableOnce = true
}
	return p521GeneratorTable
}

// ScalarBaseMult sets p = scalar * B, where B is the canonical generator, and
// returns p.
type __mrP521Point_ScalarBaseMultPair struct {
	v0 *P521Point
	v1 error
}

func __mrP521Point_ScalarBaseMult(p *P521Point, scalar []byte) __mrP521Point_ScalarBaseMultPair {
	if len(scalar) != p521ElementLength {
		return __mrP521Point_ScalarBaseMultPair{nil, errors.New("invalid scalar length")}
	}
	tables := p.generatorTable()

	// This is also a scalar multiplication with a four-bit window like in
	// ScalarMult, but in this case the doublings are precomputed. The value
	// [windowValue]G added at iteration k would normally get doubled
	// (totIterations-k)×4 times, but with a larger precomputation we can
	// instead add [2^((totIterations-k)×4)][windowValue]G and avoid the
	// doublings between iterations.
	t := NewP521Point()
	p.Set(NewP521Point())
	tableIndex := len(tables) - 1
	for _, byte := range scalar {
		windowValue := byte >> 4
		tables[tableIndex].Select(t, windowValue)
		p.Add(p, t)
		tableIndex--

		windowValue = byte & 0b1111
		tables[tableIndex].Select(t, windowValue)
		p.Add(p, t)
		tableIndex--
	}

	return __mrP521Point_ScalarBaseMultPair{p, nil}
}

func (p *P521Point) ScalarBaseMult(scalar []byte) (*P521Point, error) {
	panic("crypto/internal/fips140/nistec: ScalarBaseMult: multi-result desugar")
}

// p521Sqrt sets e to a square root of x. If x is not a square, p521Sqrt returns
// false and e is unchanged. e and x can overlap.
func p521Sqrt(e, x *fiat.P521Element) (isSquare bool) {
	candidate := new(fiat.P521Element)
	p521SqrtCandidate(candidate, x)
	square := new(fiat.P521Element).Square(candidate)
	if square.Equal(x) != 1 {
		return false
	}
	e.Set(candidate)
	return true
}

// p521SqrtCandidate sets z to a square root candidate for x. z and x must not overlap.
func p521SqrtCandidate(z, x *fiat.P521Element) {
	// Since p = 3 mod 4, exponentiation by (p + 1) / 4 yields a square root candidate.
	//
	// The sequence of 0 multiplications and 519 squarings is derived from the
	// following addition chain generated with github.com/mmcloughlin/addchain v0.4.0.
	//
	//	return  1 << 519
	//

	z.Square(x)
	for s := 1; s < 519; s++ {
		z.Square(z)
	}
}

`
