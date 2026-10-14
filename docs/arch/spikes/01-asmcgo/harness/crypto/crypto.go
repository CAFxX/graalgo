// Package cryptoform holds pure-Go EMULATE formulations of amd64 assembly
// crypto kernels: AES-128 block encrypt (crypto/aes aes_asm.go /
// aesni path) and SHA-256 compression (crypto/sha256, sha256_asm.go).
//
// Strategy: EMULATE-THEN-INTRINSIFY. Correctness first in portable Go,
// differential-tested against the asm-backed stdlib; intrinsification
// (AES-NI/SHA-NI via JVM crypto provider or explicit intrinsics) is a
// later performance step that must not change these semantics.
//
// To eliminate S-box/constant transcription errors, the AES S-box and the
// SHA-256 round constants are COMPUTED at init from their mathematical
// definitions (GF(2^8) inverse + affine transform; fractional parts of
// prime cube/square roots), not copied from tables.
package cryptoform

import "math"

// ---------- AES-128 ----------

var sbox [256]byte
var rcon [10]byte

func xtime(a byte) byte {
	if a&0x80 != 0 {
		return (a << 1) ^ 0x1b
	}
	return a << 1
}

func gmul(a, b byte) byte {
	var p byte
	for b != 0 {
		if b&1 != 0 {
			p ^= a
		}
		a = xtime(a)
		b >>= 1
	}
	return p
}

// ginv returns the multiplicative inverse in GF(2^8)/0x11b.
func ginv(a byte) byte {
	if a == 0 {
		return 0
	}
	// a^254 by square-and-multiply.
	var r byte = 1
	e := 254
	base := a
	for e > 0 {
		if e&1 != 0 {
			r = gmul(r, base)
		}
		base = gmul(base, base)
		e >>= 1
	}
	return r
}

func init() {
	// AES S-box: sbox[a] = affine(ginv(a)).
	for a := 0; a < 256; a++ {
		inv := ginv(byte(a))
		var s byte
		for i := 0; i < 8; i++ {
			bit := (inv >> i & 1) ^ (inv >> ((i+4)%8) & 1) ^ (inv >> ((i+5)%8) & 1) ^
				(inv >> ((i+6)%8) & 1) ^ (inv >> ((i+7)%8) & 1) ^ (0x63 >> i & 1)
			s |= bit << i
		}
		sbox[a] = s
	}
	// Rcon[i] = x^(i) in GF(2^8).
	rcon[0] = 1
	for i := 1; i < 10; i++ {
		rcon[i] = xtime(rcon[i-1])
	}
}

// ExpandKey128 expands a 16-byte key into 11 round keys.
func ExpandKey128(key [16]byte) (rk [11][16]byte) {
	copy(rk[0][:], key[:])
	for r := 1; r <= 10; r++ {
		prev := rk[r-1]
		var cur [16]byte
		// RotWord + SubWord + Rcon on first word, XORed with w[i-4].
		cur[0] = prev[0] ^ sbox[prev[13]] ^ rcon[r-1]
		cur[1] = prev[1] ^ sbox[prev[14]]
		cur[2] = prev[2] ^ sbox[prev[15]]
		cur[3] = prev[3] ^ sbox[prev[12]]
		for i := 4; i < 16; i++ {
			cur[i] = prev[i] ^ cur[i-4]
		}
		rk[r] = cur
	}
	return rk
}

// EncryptBlock encrypts one 16-byte block with the expanded key.
func EncryptBlock(rk [11][16]byte, in [16]byte) (out [16]byte) {
	var s [16]byte
	for i := 0; i < 16; i++ {
		s[i] = in[i] ^ rk[0][i]
	}
	for r := 1; r <= 9; r++ {
		// SubBytes + ShiftRows fused.
		var t [16]byte
		t[0] = sbox[s[0]]
		t[1] = sbox[s[5]]
		t[2] = sbox[s[10]]
		t[3] = sbox[s[15]]
		t[4] = sbox[s[4]]
		t[5] = sbox[s[9]]
		t[6] = sbox[s[14]]
		t[7] = sbox[s[3]]
		t[8] = sbox[s[8]]
		t[9] = sbox[s[13]]
		t[10] = sbox[s[2]]
		t[11] = sbox[s[7]]
		t[12] = sbox[s[12]]
		t[13] = sbox[s[1]]
		t[14] = sbox[s[6]]
		t[15] = sbox[s[11]]
		// MixColumns + AddRoundKey.
		for c := 0; c < 4; c++ {
			a0, a1, a2, a3 := t[4*c], t[4*c+1], t[4*c+2], t[4*c+3]
			s[4*c+0] = gmul(a0, 2) ^ gmul(a1, 3) ^ a2 ^ a3 ^ rk[r][4*c+0]
			s[4*c+1] = a0 ^ gmul(a1, 2) ^ gmul(a2, 3) ^ a3 ^ rk[r][4*c+1]
			s[4*c+2] = a0 ^ a1 ^ gmul(a2, 2) ^ gmul(a3, 3) ^ rk[r][4*c+2]
			s[4*c+3] = gmul(a0, 3) ^ a1 ^ a2 ^ gmul(a3, 2) ^ rk[r][4*c+3]
		}
	}
	// Final round (no MixColumns).
	out[0] = sbox[s[0]] ^ rk[10][0]
	out[1] = sbox[s[5]] ^ rk[10][1]
	out[2] = sbox[s[10]] ^ rk[10][2]
	out[3] = sbox[s[15]] ^ rk[10][3]
	out[4] = sbox[s[4]] ^ rk[10][4]
	out[5] = sbox[s[9]] ^ rk[10][5]
	out[6] = sbox[s[14]] ^ rk[10][6]
	out[7] = sbox[s[3]] ^ rk[10][7]
	out[8] = sbox[s[8]] ^ rk[10][8]
	out[9] = sbox[s[13]] ^ rk[10][9]
	out[10] = sbox[s[2]] ^ rk[10][10]
	out[11] = sbox[s[7]] ^ rk[10][11]
	out[12] = sbox[s[12]] ^ rk[10][12]
	out[13] = sbox[s[1]] ^ rk[10][13]
	out[14] = sbox[s[6]] ^ rk[10][14]
	out[15] = sbox[s[11]] ^ rk[10][15]
	return out
}

// ---------- SHA-256 ----------

var k256 [64]uint32
var hinit [8]uint32

func init() {
	// First 64 primes; k[i] = frac(cbrt(p_i)) * 2^32.
	primes := firstPrimes(64)
	for i, p := range primes {
		f := math.Cbrt(float64(p))
		k256[i] = uint32((f - math.Floor(f)) * 4294967296)
	}
	// hinit[i] = frac(sqrt(p_i)) * 2^32 for the first 8 primes.
	for i := 0; i < 8; i++ {
		f := math.Sqrt(float64(primes[i]))
		hinit[i] = uint32((f - math.Floor(f)) * 4294967296)
	}
}

func firstPrimes(n int) []int {
	var out []int
	for c := 2; len(out) < n; c++ {
		prime := true
		for _, p := range out {
			if p*p > c {
				break
			}
			if c%p == 0 {
				prime = false
				break
			}
		}
		if prime {
			out = append(out, c)
		}
	}
	return out
}

func rotr(x uint32, n uint) uint32 { return x>>n | x<<(32-n) }

// compress256 runs the SHA-256 compression function on one 64-byte block.
func compress256(h [8]uint32, block []byte) [8]uint32 {
	var w [64]uint32
	for i := 0; i < 16; i++ {
		w[i] = uint32(block[4*i])<<24 | uint32(block[4*i+1])<<16 |
			uint32(block[4*i+2])<<8 | uint32(block[4*i+3])
	}
	for i := 16; i < 64; i++ {
		s0 := rotr(w[i-15], 7) ^ rotr(w[i-15], 18) ^ (w[i-15] >> 3)
		s1 := rotr(w[i-2], 17) ^ rotr(w[i-2], 19) ^ (w[i-2] >> 10)
		w[i] = w[i-16] + s0 + w[i-7] + s1
	}
	a, b, c, d, e, f, g, hh := h[0], h[1], h[2], h[3], h[4], h[5], h[6], h[7]
	for i := 0; i < 64; i++ {
		S1 := rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25)
		ch := (e & f) ^ (^e & g)
		t1 := hh + S1 + ch + k256[i] + w[i]
		S0 := rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22)
		maj := (a & b) ^ (a & c) ^ (b & c)
		t2 := S0 + maj
		hh, g, f, e, d, c, b, a = g, f, e, d+t1, c, b, a, t1+t2
	}
	h[0] += a
	h[1] += b
	h[2] += c
	h[3] += d
	h[4] += e
	h[5] += f
	h[6] += g
	h[7] += hh
	return h
}

// Sum256 returns the SHA-256 digest of msg (full padding + chaining,
// exercising compress256 exactly as the asm block function is exercised).
func Sum256(msg []byte) [32]byte {
	h := hinit
	// Padding: append 0x80, zeros, and the 64-bit big-endian bit length
	// so the total is a multiple of 64.
	bitlen := uint64(len(msg)) * 8
	padded := make([]byte, 0, len(msg)+1+8+64)
	padded = append(padded, msg...)
	padded = append(padded, 0x80)
	for len(padded)%64 != 56 {
		padded = append(padded, 0)
	}
	for i := 7; i >= 0; i-- {
		padded = append(padded, byte(bitlen>>(8*uint(i))))
	}
	for i := 0; i < len(padded); i += 64 {
		h = compress256(h, padded[i:i+64])
	}
	var out [32]byte
	for i := 0; i < 8; i++ {
		out[4*i] = byte(h[i] >> 24)
		out[4*i+1] = byte(h[i] >> 16)
		out[4*i+2] = byte(h[i] >> 8)
		out[4*i+3] = byte(h[i])
	}
	return out
}
