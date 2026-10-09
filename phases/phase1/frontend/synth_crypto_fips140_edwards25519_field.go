package main

// P3-M9 tier-5: synthetic 'crypto/internal/fips140/edwards25519/field' package.
const syntheticCryptoFips140Edwards25519FieldSource = `// P3-M9 tier-5: synthetic edwards25519 field arithmetic.
// go1.27.1 vendored pure-Go sources (fe.go, fe_generic.go,
// fe_amd64_noasm.go; fe_amd64.go excluded by its !purego tag).
// Synthetic-only.
package field

import (
	_ "crypto/internal/fips140/check"
	"crypto/internal/fips140/subtle"
	"crypto/internal/fips140deps/byteorder"
	"errors"
	"math/bits"
)


// ---- vendored from crypto/internal/fips140/edwards25519/field/fe.go ----




// Element represents an element of the field GF(2^255-19). Note that this
// is not a cryptographically secure group, and should only be used to interact
// with edwards25519.Point coordinates.
//
// This type works similarly to math/big.Int, and all arguments and receivers
// are allowed to alias.
//
// The zero value is a valid zero element.
type Element struct {
	// An element t represents the integer
	//     t.l0 + t.l1*2^51 + t.l2*2^102 + t.l3*2^153 + t.l4*2^204
	//
	// Between operations, all limbs are expected to be lower than 2^52.
	l0 uint64
	l1 uint64
	l2 uint64
	l3 uint64
	l4 uint64
}

const maskLow51Bits uint64 = (1 << 51) - 1

var feZero = &Element{0, 0, 0, 0, 0}

// Zero sets v = 0, and returns v.
func (v *Element) Zero() *Element {
	*v = *feZero
	return v
}

var feOne = &Element{1, 0, 0, 0, 0}

// One sets v = 1, and returns v.
func (v *Element) One() *Element {
	*v = *feOne
	return v
}

// reduce reduces v modulo 2^255 - 19 and returns it.
func (v *Element) reduce() *Element {
	v.carryPropagate()

	// After the light reduction we now have a field element representation
	// v < 2^255 + 2^13 * 19, but need v < 2^255 - 19.

	// If v >= 2^255 - 19, then v + 19 >= 2^255, which would overflow 2^255 - 1,
	// generating a carry. That is, c will be 0 if v < 2^255 - 19, and 1 otherwise.
	c := (v.l0 + 19) >> 51
	c = (v.l1 + c) >> 51
	c = (v.l2 + c) >> 51
	c = (v.l3 + c) >> 51
	c = (v.l4 + c) >> 51

	// If v < 2^255 - 19 and c = 0, this will be a no-op. Otherwise, it's
	// effectively applying the reduction identity to the carry.
	v.l0 += 19 * c

	v.l1 += v.l0 >> 51
	v.l0 = v.l0 & maskLow51Bits
	v.l2 += v.l1 >> 51
	v.l1 = v.l1 & maskLow51Bits
	v.l3 += v.l2 >> 51
	v.l2 = v.l2 & maskLow51Bits
	v.l4 += v.l3 >> 51
	v.l3 = v.l3 & maskLow51Bits
	// no additional carry
	v.l4 = v.l4 & maskLow51Bits

	return v
}

// Add sets v = a + b, and returns v.
func (v *Element) Add(a, b *Element) *Element {
	v.l0 = a.l0 + b.l0
	v.l1 = a.l1 + b.l1
	v.l2 = a.l2 + b.l2
	v.l3 = a.l3 + b.l3
	v.l4 = a.l4 + b.l4
	return v.carryPropagate()
}

// Subtract sets v = a - b, and returns v.
func (v *Element) Subtract(a, b *Element) *Element {
	// We first add 2 * p, to guarantee the subtraction won't underflow, and
	// then subtract b (which can be up to 2^255 + 2^13 * 19).
	v.l0 = (a.l0 + 0xFFFFFFFFFFFDA) - b.l0
	v.l1 = (a.l1 + 0xFFFFFFFFFFFFE) - b.l1
	v.l2 = (a.l2 + 0xFFFFFFFFFFFFE) - b.l2
	v.l3 = (a.l3 + 0xFFFFFFFFFFFFE) - b.l3
	v.l4 = (a.l4 + 0xFFFFFFFFFFFFE) - b.l4
	return v.carryPropagate()
}

// Negate sets v = -a, and returns v.
func (v *Element) Negate(a *Element) *Element {
	return v.Subtract(feZero, a)
}

// Invert sets v = 1/z mod p, and returns v.
//
// If z == 0, Invert returns v = 0.
func (v *Element) Invert(z *Element) *Element {
	// Inversion is implemented as exponentiation with exponent p − 2.
	// It uses 254 squarings and 11 multiplications, grouping squarings to use SquareN.
	var z11, t0, t1, t2, t Element

	t1.Square(z)           // 2
	t.Square(&t1)          // 4
	t.Square(&t)           // 8
	t2.Multiply(&t, z)     // 9
	z11.Multiply(&t2, &t1) // 11
	t.Square(&z11)         // 22
	t0.Multiply(&t, &t2)   // 31 = 2^5 - 2^0

	t.SquareN(&t0, 5)    // 2^10 - 2^5
	t2.Multiply(&t, &t0) // 2^10 - 1

	t.SquareN(&t2, 5)    // 2^15 - 2^5
	t0.Multiply(&t, &t0) // 2^15 - 1

	t.SquareN(&t0, 15)   // 2^30 - 2^15
	t1.Multiply(&t, &t0) // 2^30 - 1

	t.SquareN(&t1, 30)   // 2^60 - 2^30
	t0.Multiply(&t, &t1) // 2^60 - 1

	t.SquareN(&t0, 60)   // 2^120 - 2^60
	t1.Multiply(&t, &t0) // 2^120 - 1

	t.SquareN(&t1, 120) // 2^240 - 2^120
	t.Multiply(&t, &t1) // 2^240 - 1

	t.SquareN(&t, 10)   // 2^250 - 2^10
	t.Multiply(&t, &t2) // 2^250 - 1

	t.SquareN(&t, 5) // 2^255 - 2^5

	return v.Multiply(&t, &z11) // 2^255 - 21
}

// Set sets v = a, and returns v.
func (v *Element) Set(a *Element) *Element {
	*v = *a
	return v
}

// SetBytes sets v to x, where x is a 32-byte little-endian encoding. If x is
// not of the right length, SetBytes returns nil and an error, and the
// receiver is unchanged.
//
// Consistent with RFC 7748, the most significant bit (the high bit of the
// last byte) is ignored, and non-canonical values (2^255-19 through 2^255-1)
// are accepted. Note that this is laxer than specified by RFC 8032, but
// consistent with most Ed25519 implementations.
type __mrElement_SetBytesPair struct {
	v0 *Element
	v1 error
}

func __mrElement_SetBytes(v *Element, x []byte) __mrElement_SetBytesPair {
	if len(x) != 32 {
		return __mrElement_SetBytesPair{nil, errors.New("edwards25519: invalid field element input size")}
	}

	// Bits 0:51 (bytes 0:8, bits 0:64, shift 0, mask 51).
	v.l0 = byteorder.LEUint64(x[0:8])
	v.l0 &= maskLow51Bits
	// Bits 51:102 (bytes 6:14, bits 48:112, shift 3, mask 51).
	v.l1 = byteorder.LEUint64(x[6:14]) >> 3
	v.l1 &= maskLow51Bits
	// Bits 102:153 (bytes 12:20, bits 96:160, shift 6, mask 51).
	v.l2 = byteorder.LEUint64(x[12:20]) >> 6
	v.l2 &= maskLow51Bits
	// Bits 153:204 (bytes 19:27, bits 152:216, shift 1, mask 51).
	v.l3 = byteorder.LEUint64(x[19:27]) >> 1
	v.l3 &= maskLow51Bits
	// Bits 204:255 (bytes 24:32, bits 192:256, shift 12, mask 51).
	// Note: not bytes 25:33, shift 4, to avoid overread.
	v.l4 = byteorder.LEUint64(x[24:32]) >> 12
	v.l4 &= maskLow51Bits

	return __mrElement_SetBytesPair{v, nil}
}

func (v *Element) SetBytes(x []byte) (*Element, error) {
	panic("crypto/internal/fips140/edwards25519/field: SetBytes: multi-result desugar")
}

// Bytes returns the canonical 32-byte little-endian encoding of v.
func (v *Element) Bytes() []byte {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var out [32]byte
	return v.bytes(&out)
}

func (v *Element) bytes(out *[32]byte) []byte {
	t := *v
	t.reduce()

	// Pack five 51-bit limbs into four 64-bit words:
	//
	//  255    204    153    102     51      0
	//    ├──l4──┼──l3──┼──l2──┼──l1──┼──l0──┤
	//   ├───u3───┼───u2───┼───u1───┼───u0───┤
	// 256      192      128       64        0

	u0 := t.l1<<51 | t.l0
	u1 := t.l2<<(102-64) | t.l1>>(64-51)
	u2 := t.l3<<(153-128) | t.l2>>(128-102)
	u3 := t.l4<<(204-192) | t.l3>>(192-153)

	byteorder.LEPutUint64(out[0*8:], u0)
	byteorder.LEPutUint64(out[1*8:], u1)
	byteorder.LEPutUint64(out[2*8:], u2)
	byteorder.LEPutUint64(out[3*8:], u3)

	return out[:]
}

// Equal returns 1 if v and u are equal, and 0 otherwise.
func (v *Element) Equal(u *Element) int {
	sa, sv := u.Bytes(), v.Bytes()
	return subtle.ConstantTimeCompare(sa, sv)
}

// mask64Bits returns 0xffffffff if cond is 1, and 0 otherwise.
func mask64Bits(cond int) uint64 { return ^(uint64(cond) - 1) }

// Select sets v to a if cond == 1, and to b if cond == 0.
func (v *Element) Select(a, b *Element, cond int) *Element {
	m := mask64Bits(cond)
	v.l0 = (m & a.l0) | (^m & b.l0)
	v.l1 = (m & a.l1) | (^m & b.l1)
	v.l2 = (m & a.l2) | (^m & b.l2)
	v.l3 = (m & a.l3) | (^m & b.l3)
	v.l4 = (m & a.l4) | (^m & b.l4)
	return v
}

// Swap swaps v and u if cond == 1 or leaves them unchanged if cond == 0, and returns v.
func (v *Element) Swap(u *Element, cond int) {
	m := mask64Bits(cond)
	t := m & (v.l0 ^ u.l0)
	v.l0 ^= t
	u.l0 ^= t
	t = m & (v.l1 ^ u.l1)
	v.l1 ^= t
	u.l1 ^= t
	t = m & (v.l2 ^ u.l2)
	v.l2 ^= t
	u.l2 ^= t
	t = m & (v.l3 ^ u.l3)
	v.l3 ^= t
	u.l3 ^= t
	t = m & (v.l4 ^ u.l4)
	v.l4 ^= t
	u.l4 ^= t
}

// IsNegative returns 1 if v is negative, and 0 otherwise.
func (v *Element) IsNegative() int {
	return int(v.Bytes()[0] & 1)
}

// Absolute sets v to |u|, and returns v.
func (v *Element) Absolute(u *Element) *Element {
	return v.Select(new(Element).Negate(u), u, u.IsNegative())
}

// Multiply sets v = x * y, and returns v.
func (v *Element) Multiply(x, y *Element) *Element {
	feMul(v, x, y)
	return v
}

// Square sets v = x * x, and returns v.
func (v *Element) Square(x *Element) *Element {
	feSquare(v, x)
	return v
}

// SquareN sets v = x^(2^n), and returns v. n must be positive.
func (v *Element) SquareN(x *Element, n int) *Element {
	feSquareN(v, x, n)
	return v
}

// Mult32 sets v = x * y, and returns v.
func (v *Element) Mult32(x *Element, y uint32) *Element {
	x0lo, x0hi := mul51(x.l0, y)
	x1lo, x1hi := mul51(x.l1, y)
	x2lo, x2hi := mul51(x.l2, y)
	x3lo, x3hi := mul51(x.l3, y)
	x4lo, x4hi := mul51(x.l4, y)
	v.l0 = x0lo + 19*x4hi // carried over per the reduction identity
	v.l1 = x1lo + x0hi
	v.l2 = x2lo + x1hi
	v.l3 = x3lo + x2hi
	v.l4 = x4lo + x3hi
	// The hi portions are going to be only 32 bits, plus any previous excess,
	// so we can skip the carry propagation.
	return v
}

// mul51 returns lo + hi * 2⁵¹ = a * b.
type __mrmul51Pair struct {
	lo uint64
	hi uint64
}

func __mrmul51(a uint64, b uint32) __mrmul51Pair {
	var lo uint64
	var hi uint64

	mh, ml := bits.Mul64(a, uint64(b))
	lo = ml & maskLow51Bits
	hi = (mh << 13) | (ml >> 51)
	return __mrmul51Pair{lo, hi}
}

func mul51(a uint64, b uint32) (lo uint64, hi uint64) {
	panic("crypto/internal/fips140/edwards25519/field: mul51: multi-result desugar")
}

// Pow22523 set v = x^((p-5)/8), and returns v. (p-5)/8 is 2^252-3.
func (v *Element) Pow22523(x *Element) *Element {
	var t0, t1, t2 Element

	t0.Square(x)          // x^2
	t1.Multiply(x, &t0)   // x^3
	t0.Square(&t1)        // x^6
	t0.Square(&t0)        // x^12
	t0.Multiply(&t1, &t0) // x^15
	t0.Square(&t0)        // x^30
	t0.Multiply(x, &t0)   // x^31 = 2^5 - 1

	t1.SquareN(&t0, 5)    // 2^10 - 2^5
	t1.Multiply(&t1, &t0) // 2^10 - 1

	t2.SquareN(&t1, 5)    // 2^15 - 2^5
	t0.Multiply(&t2, &t0) // 2^15 - 1

	t2.SquareN(&t0, 15)   // 2^30 - 2^15
	t2.Multiply(&t2, &t0) // 2^30 - 1

	t0.SquareN(&t2, 30)   // 2^60 - 2^30
	t0.Multiply(&t0, &t2) // 2^60 - 1

	t2.SquareN(&t0, 60)   // 2^120 - 2^60
	t2.Multiply(&t2, &t0) // 2^120 - 1

	t0.SquareN(&t2, 120)  // 2^240 - 2^120
	t0.Multiply(&t0, &t2) // 2^240 - 1

	t0.SquareN(&t0, 10)   // 2^250 - 2^10
	t0.Multiply(&t0, &t1) // 2^250 - 1

	t0.SquareN(&t0, 2)        // 2^252 - 4
	return v.Multiply(&t0, x) // 2^252 - 3
}

// sqrtM1 is 2^((p-1)/4), which squared is equal to -1 by Euler's Criterion.
var sqrtM1 = &Element{1718705420411056, 234908883556509,
	2233514472574048, 2117202627021982, 765476049583133}

// SqrtRatio sets r to the non-negative square root of the ratio of u and v.
//
// If u/v is square, SqrtRatio returns r and 1. If u/v is not square, SqrtRatio
// sets r according to Section 4.3 of draft-irtf-cfrg-ristretto255-decaf448-00,
// and returns r and 0.
type __mrElement_SqrtRatioPair struct {
	R *Element
	wasSquare int
}

func __mrElement_SqrtRatio(r *Element, u, v *Element) __mrElement_SqrtRatioPair {
	t0 := new(Element)

	// r = (u * v3) * (u * v7)^((p-5)/8)
	v2 := new(Element).Square(v)
	uv3 := new(Element).Multiply(u, t0.Multiply(v2, v))
	uv7 := new(Element).Multiply(uv3, t0.Square(v2))
	rr := new(Element).Multiply(uv3, t0.Pow22523(uv7))

	check := new(Element).Multiply(v, t0.Square(rr)) // check = v * r^2

	uNeg := new(Element).Negate(u)
	correctSignSqrt := check.Equal(u)
	flippedSignSqrt := check.Equal(uNeg)
	flippedSignSqrtI := check.Equal(t0.Multiply(uNeg, sqrtM1))

	rPrime := new(Element).Multiply(rr, sqrtM1) // r_prime = SQRT_M1 * r
	// r = CT_SELECT(r_prime IF flipped_sign_sqrt | flipped_sign_sqrt_i ELSE r)
	rr.Select(rPrime, rr, flippedSignSqrt|flippedSignSqrtI)

	r.Absolute(rr) // Choose the nonnegative square root.
	return __mrElement_SqrtRatioPair{r, correctSignSqrt | flippedSignSqrt}
}

func (r *Element) SqrtRatio(u, v *Element) (R *Element, wasSquare int) {
	panic("crypto/internal/fips140/edwards25519/field: SqrtRatio: multi-result desugar")
}


// ---- vendored from crypto/internal/fips140/edwards25519/field/fe_generic.go ----



// uint128 holds a 128-bit number as two 64-bit limbs, for use with the
// bits.Mul64 and bits.Add64 intrinsics.
type uint128 struct {
	lo, hi uint64
}

// mul returns a * b.
func mul(a, b uint64) uint128 {
	hi, lo := bits.Mul64(a, b)
	return uint128{lo, hi}
}

// addMul returns v + a * b.
func addMul(v uint128, a, b uint64) uint128 {
	hi, lo := bits.Mul64(a, b)
	lo, c := bits.Add64(lo, v.lo, 0)
	hi, _ = bits.Add64(hi, v.hi, c)
	return uint128{lo, hi}
}

// mul19 returns v * 19.
func mul19(v uint64) uint64 {
	// Using this approach seems to yield better optimizations than *19.
	return v + (v+v<<3)<<1
}

// addMul19 returns v + 19 * a * b, where a and b are at most 52 bits.
func addMul19(v uint128, a, b uint64) uint128 {
	hi, lo := bits.Mul64(mul19(a), b)
	lo, c := bits.Add64(lo, v.lo, 0)
	hi, _ = bits.Add64(hi, v.hi, c)
	return uint128{lo, hi}
}

// addMul38 returns v + 38 * a * b, where a and b are at most 52 bits.
func addMul38(v uint128, a, b uint64) uint128 {
	hi, lo := bits.Mul64(mul19(a), b*2)
	lo, c := bits.Add64(lo, v.lo, 0)
	hi, _ = bits.Add64(hi, v.hi, c)
	return uint128{lo, hi}
}

// shiftRightBy51 returns a >> 51. a is assumed to be at most 115 bits.
func shiftRightBy51(a uint128) uint64 {
	return (a.hi << (64 - 51)) | (a.lo >> 51)
}

func feMulGeneric(v, a, b *Element) {
	a0 := a.l0
	a1 := a.l1
	a2 := a.l2
	a3 := a.l3
	a4 := a.l4

	b0 := b.l0
	b1 := b.l1
	b2 := b.l2
	b3 := b.l3
	b4 := b.l4

	// Limb multiplication works like pen-and-paper columnar multiplication, but
	// with 51-bit limbs instead of digits.
	//
	//                          a4   a3   a2   a1   a0  x
	//                          b4   b3   b2   b1   b0  =
	//                         ------------------------
	//                        a4b0 a3b0 a2b0 a1b0 a0b0  +
	//                   a4b1 a3b1 a2b1 a1b1 a0b1       +
	//              a4b2 a3b2 a2b2 a1b2 a0b2            +
	//         a4b3 a3b3 a2b3 a1b3 a0b3                 +
	//    a4b4 a3b4 a2b4 a1b4 a0b4                      =
	//   ----------------------------------------------
	//      r8   r7   r6   r5   r4   r3   r2   r1   r0
	//
	// We can then use the reduction identity (a * 2²⁵⁵ + b = a * 19 + b) to
	// reduce the limbs that would overflow 255 bits. r5 * 2²⁵⁵ becomes 19 * r5,
	// r6 * 2³⁰⁶ becomes 19 * r6 * 2⁵¹, etc.
	//
	// Reduction can be carried out simultaneously to multiplication. For
	// example, we do not compute r5: whenever the result of a multiplication
	// belongs to r5, like a1b4, we multiply it by 19 and add the result to r0.
	//
	//            a4b0    a3b0    a2b0    a1b0    a0b0  +
	//            a3b1    a2b1    a1b1    a0b1 19×a4b1  +
	//            a2b2    a1b2    a0b2 19×a4b2 19×a3b2  +
	//            a1b3    a0b3 19×a4b3 19×a3b3 19×a2b3  +
	//            a0b4 19×a4b4 19×a3b4 19×a2b4 19×a1b4  =
	//           --------------------------------------
	//              r4      r3      r2      r1      r0
	//
	// Finally we add up the columns into wide, overlapping limbs.

	// r0 = a0×b0 + 19×(a1×b4 + a2×b3 + a3×b2 + a4×b1)
	r0 := mul(a0, b0)
	r0 = addMul19(r0, a1, b4)
	r0 = addMul19(r0, a2, b3)
	r0 = addMul19(r0, a3, b2)
	r0 = addMul19(r0, a4, b1)

	// r1 = a0×b1 + a1×b0 + 19×(a2×b4 + a3×b3 + a4×b2)
	r1 := mul(a0, b1)
	r1 = addMul(r1, a1, b0)
	r1 = addMul19(r1, a2, b4)
	r1 = addMul19(r1, a3, b3)
	r1 = addMul19(r1, a4, b2)

	// r2 = a0×b2 + a1×b1 + a2×b0 + 19×(a3×b4 + a4×b3)
	r2 := mul(a0, b2)
	r2 = addMul(r2, a1, b1)
	r2 = addMul(r2, a2, b0)
	r2 = addMul19(r2, a3, b4)
	r2 = addMul19(r2, a4, b3)

	// r3 = a0×b3 + a1×b2 + a2×b1 + a3×b0 + 19×a4×b4
	r3 := mul(a0, b3)
	r3 = addMul(r3, a1, b2)
	r3 = addMul(r3, a2, b1)
	r3 = addMul(r3, a3, b0)
	r3 = addMul19(r3, a4, b4)

	// r4 = a0×b4 + a1×b3 + a2×b2 + a3×b1 + a4×b0
	r4 := mul(a0, b4)
	r4 = addMul(r4, a1, b3)
	r4 = addMul(r4, a2, b2)
	r4 = addMul(r4, a3, b1)
	r4 = addMul(r4, a4, b0)

	// After the multiplication, we need to reduce (carry) the five coefficients
	// to obtain a result with limbs that are at most slightly larger than 2⁵¹,
	// to respect the Element invariant.
	//
	// Overall, the reduction works the same as carryPropagate, except with
	// wider inputs: we take the carry for each coefficient by shifting it right
	// by 51, and add it to the limb above it. The top carry is multiplied by 19
	// according to the reduction identity and added to the lowest limb.
	//
	// The largest coefficient (r0) will be at most 111 bits, which guarantees
	// that all carries are at most 111 - 51 = 60 bits, which fits in a uint64.
	//
	//     r0 = a0×b0 + 19×(a1×b4 + a2×b3 + a3×b2 + a4×b1)
	//     r0 < 2⁵²×2⁵² + 19×(2⁵²×2⁵² + 2⁵²×2⁵² + 2⁵²×2⁵² + 2⁵²×2⁵²)
	//     r0 < (1 + 19 × 4) × 2⁵² × 2⁵²
	//     r0 < 2⁷ × 2⁵² × 2⁵²
	//     r0 < 2¹¹¹
	//
	// Moreover, the top coefficient (r4) is at most 107 bits, so c4 is at most
	// 56 bits, and c4 * 19 is at most 61 bits, which again fits in a uint64 and
	// allows us to easily apply the reduction identity.
	//
	//     r4 = a0×b4 + a1×b3 + a2×b2 + a3×b1 + a4×b0
	//     r4 < 5 × 2⁵² × 2⁵²
	//     r4 < 2¹⁰⁷
	//

	c0 := shiftRightBy51(r0)
	c1 := shiftRightBy51(r1)
	c2 := shiftRightBy51(r2)
	c3 := shiftRightBy51(r3)
	c4 := shiftRightBy51(r4)

	rr0 := r0.lo&maskLow51Bits + mul19(c4)
	rr1 := r1.lo&maskLow51Bits + c0
	rr2 := r2.lo&maskLow51Bits + c1
	rr3 := r3.lo&maskLow51Bits + c2
	rr4 := r4.lo&maskLow51Bits + c3

	// Now all coefficients fit into 64-bit registers but are still too large to
	// be passed around as an Element. We therefore do one last carry chain,
	// where the carries will be small enough to fit in the wiggle room above 2⁵¹.

	v.l0 = rr0&maskLow51Bits + mul19(rr4>>51)
	v.l1 = rr1&maskLow51Bits + rr0>>51
	v.l2 = rr2&maskLow51Bits + rr1>>51
	v.l3 = rr3&maskLow51Bits + rr2>>51
	v.l4 = rr4&maskLow51Bits + rr3>>51
}

func feSquare(v, a *Element) {
	l0 := a.l0
	l1 := a.l1
	l2 := a.l2
	l3 := a.l3
	l4 := a.l4

	// Squaring works precisely like multiplication above, but thanks to its
	// symmetry we get to group a few terms together.
	//
	//                          l4   l3   l2   l1   l0  x
	//                          l4   l3   l2   l1   l0  =
	//                         ------------------------
	//                        l4l0 l3l0 l2l0 l1l0 l0l0  +
	//                   l4l1 l3l1 l2l1 l1l1 l0l1       +
	//              l4l2 l3l2 l2l2 l1l2 l0l2            +
	//         l4l3 l3l3 l2l3 l1l3 l0l3                 +
	//    l4l4 l3l4 l2l4 l1l4 l0l4                      =
	//   ----------------------------------------------
	//      r8   r7   r6   r5   r4   r3   r2   r1   r0
	//
	//            l4l0    l3l0    l2l0    l1l0    l0l0  +
	//            l3l1    l2l1    l1l1    l0l1 19×l4l1  +
	//            l2l2    l1l2    l0l2 19×l4l2 19×l3l2  +
	//            l1l3    l0l3 19×l4l3 19×l3l3 19×l2l3  +
	//            l0l4 19×l4l4 19×l3l4 19×l2l4 19×l1l4  =
	//           --------------------------------------
	//              r4      r3      r2      r1      r0

	// r0 = l0×l0 + 19×(l1×l4 + l2×l3 + l3×l2 + l4×l1) = l0×l0 + 19×2×(l1×l4 + l2×l3)
	r0 := mul(l0, l0)
	r0 = addMul38(r0, l1, l4)
	r0 = addMul38(r0, l2, l3)

	// r1 = l0×l1 + l1×l0 + 19×(l2×l4 + l3×l3 + l4×l2) = 2×l0×l1 + 19×2×l2×l4 + 19×l3×l3
	r1 := mul(l0*2, l1)
	r1 = addMul38(r1, l2, l4)
	r1 = addMul19(r1, l3, l3)

	// r2 = l0×l2 + l1×l1 + l2×l0 + 19×(l3×l4 + l4×l3) = 2×l0×l2 + l1×l1 + 19×2×l3×l4
	r2 := mul(l0*2, l2)
	r2 = addMul(r2, l1, l1)
	r2 = addMul38(r2, l3, l4)

	// r3 = l0×l3 + l1×l2 + l2×l1 + l3×l0 + 19×l4×l4 = 2×l0×l3 + 2×l1×l2 + 19×l4×l4
	r3 := mul(l0*2, l3)
	r3 = addMul(r3, l1*2, l2)
	r3 = addMul19(r3, l4, l4)

	// r4 = l0×l4 + l1×l3 + l2×l2 + l3×l1 + l4×l0 = 2×l0×l4 + 2×l1×l3 + l2×l2
	r4 := mul(l0*2, l4)
	r4 = addMul(r4, l1*2, l3)
	r4 = addMul(r4, l2, l2)

	c0 := shiftRightBy51(r0)
	c1 := shiftRightBy51(r1)
	c2 := shiftRightBy51(r2)
	c3 := shiftRightBy51(r3)
	c4 := shiftRightBy51(r4)

	rr0 := r0.lo&maskLow51Bits + mul19(c4)
	rr1 := r1.lo&maskLow51Bits + c0
	rr2 := r2.lo&maskLow51Bits + c1
	rr3 := r3.lo&maskLow51Bits + c2
	rr4 := r4.lo&maskLow51Bits + c3

	v.l0 = rr0&maskLow51Bits + mul19(rr4>>51)
	v.l1 = rr1&maskLow51Bits + rr0>>51
	v.l2 = rr2&maskLow51Bits + rr1>>51
	v.l3 = rr3&maskLow51Bits + rr2>>51
	v.l4 = rr4&maskLow51Bits + rr3>>51
}

// feSquareN squares a n times and writes the result to v.
// It uses local variables to keep limbs in registers.
func feSquareN(v, a *Element, n int) {
	l0 := a.l0
	l1 := a.l1
	l2 := a.l2
	l3 := a.l3
	l4 := a.l4

	for range n {
		r0 := mul(l0, l0)
		r0 = addMul38(r0, l1, l4)
		r0 = addMul38(r0, l2, l3)

		r1 := mul(l0*2, l1)
		r1 = addMul38(r1, l2, l4)
		r1 = addMul19(r1, l3, l3)

		r2 := mul(l0*2, l2)
		r2 = addMul(r2, l1, l1)
		r2 = addMul38(r2, l3, l4)

		r3 := mul(l0*2, l3)
		r3 = addMul(r3, l1*2, l2)
		r3 = addMul19(r3, l4, l4)

		r4 := mul(l0*2, l4)
		r4 = addMul(r4, l1*2, l3)
		r4 = addMul(r4, l2, l2)

		c0 := shiftRightBy51(r0)
		c1 := shiftRightBy51(r1)
		c2 := shiftRightBy51(r2)
		c3 := shiftRightBy51(r3)
		c4 := shiftRightBy51(r4)

		rr0 := r0.lo&maskLow51Bits + mul19(c4)
		rr1 := r1.lo&maskLow51Bits + c0
		rr2 := r2.lo&maskLow51Bits + c1
		rr3 := r3.lo&maskLow51Bits + c2
		rr4 := r4.lo&maskLow51Bits + c3

		l0 = rr0&maskLow51Bits + mul19(rr4>>51)
		l1 = rr1&maskLow51Bits + rr0>>51
		l2 = rr2&maskLow51Bits + rr1>>51
		l3 = rr3&maskLow51Bits + rr2>>51
		l4 = rr4&maskLow51Bits + rr3>>51
	}

	v.l0 = l0
	v.l1 = l1
	v.l2 = l2
	v.l3 = l3
	v.l4 = l4
}

// carryPropagate brings the limbs below 52 bits by applying the reduction
// identity (a * 2²⁵⁵ + b = a * 19 + b) to the l4 carry.
func (v *Element) carryPropagate() *Element {
	// (l4>>51) is at most 64 - 51 = 13 bits, so (l4>>51)*19 is at most 18 bits, and
	// the final l0 will be at most 52 bits. Similarly for the rest.
	l0 := v.l0
	v.l0 = v.l0&maskLow51Bits + mul19(v.l4>>51)
	v.l4 = v.l4&maskLow51Bits + v.l3>>51
	v.l3 = v.l3&maskLow51Bits + v.l2>>51
	v.l2 = v.l2&maskLow51Bits + v.l1>>51
	v.l1 = v.l1&maskLow51Bits + l0>>51

	return v
}


// ---- vendored from crypto/internal/fips140/edwards25519/field/fe_amd64_noasm.go ----


func feMul(v, x, y *Element) { feMulGeneric(v, x, y) }

`
