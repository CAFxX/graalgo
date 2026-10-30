// Retirement: see ../../docs/arch/synth-retirement-tracker.md#cryptointernalfips140sha3 for this package's root cause and retires-when trigger (crypto/internal/fips140/sha3).
package main

// P3-M9 tier-5: synthetic 'crypto/internal/fips140/sha3' package.
const syntheticCryptoFips140Sha3Source = `// P3-M9 tier-5: synthetic 'crypto/internal/fips140/sha3'.
// go1.27.1 vendored pure-Go sources (sha3_noasm.go selected for
// purego; amd64 asm excluded). Synthetic-only.
// DEVIATION (documented): keccakF1600Generic always takes the
// copy-in/copy-out path (the unsafe.Pointer aliasing path is not in
// frontend/v1 scope); behavior-identical on little-endian.
package sha3

import (
	"crypto/internal/fips140deps/byteorder"
	"math/bits"
	"crypto/internal/fips140"
	"crypto/internal/fips140/subtle"
	"errors"
	"bytes"
)


// ---- vendored from crypto/internal/fips140/sha3/hashes.go ----


// New224 returns a new Digest computing the SHA3-224 hash.
func New224() *Digest {
	return &Digest{rate: rateK448, outputLen: 28, dsbyte: dsbyteSHA3}
}

// New256 returns a new Digest computing the SHA3-256 hash.
func New256() *Digest {
	return &Digest{rate: rateK512, outputLen: 32, dsbyte: dsbyteSHA3}
}

// New384 returns a new Digest computing the SHA3-384 hash.
func New384() *Digest {
	return &Digest{rate: rateK768, outputLen: 48, dsbyte: dsbyteSHA3}
}

// New512 returns a new Digest computing the SHA3-512 hash.
func New512() *Digest {
	return &Digest{rate: rateK1024, outputLen: 64, dsbyte: dsbyteSHA3}
}

const (
	dsbyteSHA3   = 0b00000110
	dsbyteKeccak = 0b00000001
	dsbyteShake  = 0b00011111
	dsbyteCShake = 0b00000100

	// rateK[c] is the rate in bytes for Keccak[c] where c is the capacity in
	// bits. Given the sponge size is 1600 bits, the rate is 1600 - c bits.
	rateK256  = (1600 - 256) / 8
	rateK448  = (1600 - 448) / 8
	rateK512  = (1600 - 512) / 8
	rateK768  = (1600 - 768) / 8
	rateK1024 = (1600 - 1024) / 8
)

// NewLegacyKeccak256 returns a new Digest computing the legacy, non-standard
// Keccak-256 hash.
func NewLegacyKeccak256() *Digest {
	return &Digest{rate: rateK512, outputLen: 32, dsbyte: dsbyteKeccak}
}

// NewLegacyKeccak512 returns a new Digest computing the legacy, non-standard
// Keccak-512 hash.
func NewLegacyKeccak512() *Digest {
	return &Digest{rate: rateK1024, outputLen: 64, dsbyte: dsbyteKeccak}
}


// ---- vendored from crypto/internal/fips140/sha3/keccakf.go ----




// rc stores the round constants for use in the ι step.
var rc = [24]uint64{
	0x0000000000000001,
	0x0000000000008082,
	0x800000000000808A,
	0x8000000080008000,
	0x000000000000808B,
	0x0000000080000001,
	0x8000000080008081,
	0x8000000000008009,
	0x000000000000008A,
	0x0000000000000088,
	0x0000000080008009,
	0x000000008000000A,
	0x000000008000808B,
	0x800000000000008B,
	0x8000000000008089,
	0x8000000000008003,
	0x8000000000008002,
	0x8000000000000080,
	0x000000000000800A,
	0x800000008000000A,
	0x8000000080008081,
	0x8000000000008080,
	0x0000000080000001,
	0x8000000080008008,
}

// keccakF1600Generic applies the Keccak permutation.
func keccakF1600Generic(da *[200]byte) {
	// P3-M9: always use the copy-in/copy-out path (unsafe.Pointer
	// aliasing is not in frontend/v1 scope); behavior-identical on
	// little-endian.
	var a *[25]uint64
	a = new([25]uint64)
	for i := range a {
		a[i] = byteorder.LEUint64(da[i*8:])
	}
	defer func() {
		for i := range a {
			byteorder.LEPutUint64(da[i*8:], a[i])
		}
	}()

	// Implementation translated from Keccak-inplace.c
	// in the keccak reference code.
	var t, bc0, bc1, bc2, bc3, bc4, d0, d1, d2, d3, d4 uint64

	for i := 0; i < 24; i += 4 {
		// Combines the 5 steps in each round into 2 steps.
		// Unrolls 4 rounds per loop and spreads some steps across rounds.

		// Round 1
		bc0 = a[0] ^ a[5] ^ a[10] ^ a[15] ^ a[20]
		bc1 = a[1] ^ a[6] ^ a[11] ^ a[16] ^ a[21]
		bc2 = a[2] ^ a[7] ^ a[12] ^ a[17] ^ a[22]
		bc3 = a[3] ^ a[8] ^ a[13] ^ a[18] ^ a[23]
		bc4 = a[4] ^ a[9] ^ a[14] ^ a[19] ^ a[24]
		d0 = bc4 ^ (bc1<<1 | bc1>>63)
		d1 = bc0 ^ (bc2<<1 | bc2>>63)
		d2 = bc1 ^ (bc3<<1 | bc3>>63)
		d3 = bc2 ^ (bc4<<1 | bc4>>63)
		d4 = bc3 ^ (bc0<<1 | bc0>>63)

		bc0 = a[0] ^ d0
		t = a[6] ^ d1
		bc1 = bits.RotateLeft64(t, 44)
		t = a[12] ^ d2
		bc2 = bits.RotateLeft64(t, 43)
		t = a[18] ^ d3
		bc3 = bits.RotateLeft64(t, 21)
		t = a[24] ^ d4
		bc4 = bits.RotateLeft64(t, 14)
		a[0] = bc0 ^ (bc2 &^ bc1) ^ rc[i]
		a[6] = bc1 ^ (bc3 &^ bc2)
		a[12] = bc2 ^ (bc4 &^ bc3)
		a[18] = bc3 ^ (bc0 &^ bc4)
		a[24] = bc4 ^ (bc1 &^ bc0)

		t = a[10] ^ d0
		bc2 = bits.RotateLeft64(t, 3)
		t = a[16] ^ d1
		bc3 = bits.RotateLeft64(t, 45)
		t = a[22] ^ d2
		bc4 = bits.RotateLeft64(t, 61)
		t = a[3] ^ d3
		bc0 = bits.RotateLeft64(t, 28)
		t = a[9] ^ d4
		bc1 = bits.RotateLeft64(t, 20)
		a[10] = bc0 ^ (bc2 &^ bc1)
		a[16] = bc1 ^ (bc3 &^ bc2)
		a[22] = bc2 ^ (bc4 &^ bc3)
		a[3] = bc3 ^ (bc0 &^ bc4)
		a[9] = bc4 ^ (bc1 &^ bc0)

		t = a[20] ^ d0
		bc4 = bits.RotateLeft64(t, 18)
		t = a[1] ^ d1
		bc0 = bits.RotateLeft64(t, 1)
		t = a[7] ^ d2
		bc1 = bits.RotateLeft64(t, 6)
		t = a[13] ^ d3
		bc2 = bits.RotateLeft64(t, 25)
		t = a[19] ^ d4
		bc3 = bits.RotateLeft64(t, 8)
		a[20] = bc0 ^ (bc2 &^ bc1)
		a[1] = bc1 ^ (bc3 &^ bc2)
		a[7] = bc2 ^ (bc4 &^ bc3)
		a[13] = bc3 ^ (bc0 &^ bc4)
		a[19] = bc4 ^ (bc1 &^ bc0)

		t = a[5] ^ d0
		bc1 = bits.RotateLeft64(t, 36)
		t = a[11] ^ d1
		bc2 = bits.RotateLeft64(t, 10)
		t = a[17] ^ d2
		bc3 = bits.RotateLeft64(t, 15)
		t = a[23] ^ d3
		bc4 = bits.RotateLeft64(t, 56)
		t = a[4] ^ d4
		bc0 = bits.RotateLeft64(t, 27)
		a[5] = bc0 ^ (bc2 &^ bc1)
		a[11] = bc1 ^ (bc3 &^ bc2)
		a[17] = bc2 ^ (bc4 &^ bc3)
		a[23] = bc3 ^ (bc0 &^ bc4)
		a[4] = bc4 ^ (bc1 &^ bc0)

		t = a[15] ^ d0
		bc3 = bits.RotateLeft64(t, 41)
		t = a[21] ^ d1
		bc4 = bits.RotateLeft64(t, 2)
		t = a[2] ^ d2
		bc0 = bits.RotateLeft64(t, 62)
		t = a[8] ^ d3
		bc1 = bits.RotateLeft64(t, 55)
		t = a[14] ^ d4
		bc2 = bits.RotateLeft64(t, 39)
		a[15] = bc0 ^ (bc2 &^ bc1)
		a[21] = bc1 ^ (bc3 &^ bc2)
		a[2] = bc2 ^ (bc4 &^ bc3)
		a[8] = bc3 ^ (bc0 &^ bc4)
		a[14] = bc4 ^ (bc1 &^ bc0)

		// Round 2
		bc0 = a[0] ^ a[5] ^ a[10] ^ a[15] ^ a[20]
		bc1 = a[1] ^ a[6] ^ a[11] ^ a[16] ^ a[21]
		bc2 = a[2] ^ a[7] ^ a[12] ^ a[17] ^ a[22]
		bc3 = a[3] ^ a[8] ^ a[13] ^ a[18] ^ a[23]
		bc4 = a[4] ^ a[9] ^ a[14] ^ a[19] ^ a[24]
		d0 = bc4 ^ (bc1<<1 | bc1>>63)
		d1 = bc0 ^ (bc2<<1 | bc2>>63)
		d2 = bc1 ^ (bc3<<1 | bc3>>63)
		d3 = bc2 ^ (bc4<<1 | bc4>>63)
		d4 = bc3 ^ (bc0<<1 | bc0>>63)

		bc0 = a[0] ^ d0
		t = a[16] ^ d1
		bc1 = bits.RotateLeft64(t, 44)
		t = a[7] ^ d2
		bc2 = bits.RotateLeft64(t, 43)
		t = a[23] ^ d3
		bc3 = bits.RotateLeft64(t, 21)
		t = a[14] ^ d4
		bc4 = bits.RotateLeft64(t, 14)
		a[0] = bc0 ^ (bc2 &^ bc1) ^ rc[i+1]
		a[16] = bc1 ^ (bc3 &^ bc2)
		a[7] = bc2 ^ (bc4 &^ bc3)
		a[23] = bc3 ^ (bc0 &^ bc4)
		a[14] = bc4 ^ (bc1 &^ bc0)

		t = a[20] ^ d0
		bc2 = bits.RotateLeft64(t, 3)
		t = a[11] ^ d1
		bc3 = bits.RotateLeft64(t, 45)
		t = a[2] ^ d2
		bc4 = bits.RotateLeft64(t, 61)
		t = a[18] ^ d3
		bc0 = bits.RotateLeft64(t, 28)
		t = a[9] ^ d4
		bc1 = bits.RotateLeft64(t, 20)
		a[20] = bc0 ^ (bc2 &^ bc1)
		a[11] = bc1 ^ (bc3 &^ bc2)
		a[2] = bc2 ^ (bc4 &^ bc3)
		a[18] = bc3 ^ (bc0 &^ bc4)
		a[9] = bc4 ^ (bc1 &^ bc0)

		t = a[15] ^ d0
		bc4 = bits.RotateLeft64(t, 18)
		t = a[6] ^ d1
		bc0 = bits.RotateLeft64(t, 1)
		t = a[22] ^ d2
		bc1 = bits.RotateLeft64(t, 6)
		t = a[13] ^ d3
		bc2 = bits.RotateLeft64(t, 25)
		t = a[4] ^ d4
		bc3 = bits.RotateLeft64(t, 8)
		a[15] = bc0 ^ (bc2 &^ bc1)
		a[6] = bc1 ^ (bc3 &^ bc2)
		a[22] = bc2 ^ (bc4 &^ bc3)
		a[13] = bc3 ^ (bc0 &^ bc4)
		a[4] = bc4 ^ (bc1 &^ bc0)

		t = a[10] ^ d0
		bc1 = bits.RotateLeft64(t, 36)
		t = a[1] ^ d1
		bc2 = bits.RotateLeft64(t, 10)
		t = a[17] ^ d2
		bc3 = bits.RotateLeft64(t, 15)
		t = a[8] ^ d3
		bc4 = bits.RotateLeft64(t, 56)
		t = a[24] ^ d4
		bc0 = bits.RotateLeft64(t, 27)
		a[10] = bc0 ^ (bc2 &^ bc1)
		a[1] = bc1 ^ (bc3 &^ bc2)
		a[17] = bc2 ^ (bc4 &^ bc3)
		a[8] = bc3 ^ (bc0 &^ bc4)
		a[24] = bc4 ^ (bc1 &^ bc0)

		t = a[5] ^ d0
		bc3 = bits.RotateLeft64(t, 41)
		t = a[21] ^ d1
		bc4 = bits.RotateLeft64(t, 2)
		t = a[12] ^ d2
		bc0 = bits.RotateLeft64(t, 62)
		t = a[3] ^ d3
		bc1 = bits.RotateLeft64(t, 55)
		t = a[19] ^ d4
		bc2 = bits.RotateLeft64(t, 39)
		a[5] = bc0 ^ (bc2 &^ bc1)
		a[21] = bc1 ^ (bc3 &^ bc2)
		a[12] = bc2 ^ (bc4 &^ bc3)
		a[3] = bc3 ^ (bc0 &^ bc4)
		a[19] = bc4 ^ (bc1 &^ bc0)

		// Round 3
		bc0 = a[0] ^ a[5] ^ a[10] ^ a[15] ^ a[20]
		bc1 = a[1] ^ a[6] ^ a[11] ^ a[16] ^ a[21]
		bc2 = a[2] ^ a[7] ^ a[12] ^ a[17] ^ a[22]
		bc3 = a[3] ^ a[8] ^ a[13] ^ a[18] ^ a[23]
		bc4 = a[4] ^ a[9] ^ a[14] ^ a[19] ^ a[24]
		d0 = bc4 ^ (bc1<<1 | bc1>>63)
		d1 = bc0 ^ (bc2<<1 | bc2>>63)
		d2 = bc1 ^ (bc3<<1 | bc3>>63)
		d3 = bc2 ^ (bc4<<1 | bc4>>63)
		d4 = bc3 ^ (bc0<<1 | bc0>>63)

		bc0 = a[0] ^ d0
		t = a[11] ^ d1
		bc1 = bits.RotateLeft64(t, 44)
		t = a[22] ^ d2
		bc2 = bits.RotateLeft64(t, 43)
		t = a[8] ^ d3
		bc3 = bits.RotateLeft64(t, 21)
		t = a[19] ^ d4
		bc4 = bits.RotateLeft64(t, 14)
		a[0] = bc0 ^ (bc2 &^ bc1) ^ rc[i+2]
		a[11] = bc1 ^ (bc3 &^ bc2)
		a[22] = bc2 ^ (bc4 &^ bc3)
		a[8] = bc3 ^ (bc0 &^ bc4)
		a[19] = bc4 ^ (bc1 &^ bc0)

		t = a[15] ^ d0
		bc2 = bits.RotateLeft64(t, 3)
		t = a[1] ^ d1
		bc3 = bits.RotateLeft64(t, 45)
		t = a[12] ^ d2
		bc4 = bits.RotateLeft64(t, 61)
		t = a[23] ^ d3
		bc0 = bits.RotateLeft64(t, 28)
		t = a[9] ^ d4
		bc1 = bits.RotateLeft64(t, 20)
		a[15] = bc0 ^ (bc2 &^ bc1)
		a[1] = bc1 ^ (bc3 &^ bc2)
		a[12] = bc2 ^ (bc4 &^ bc3)
		a[23] = bc3 ^ (bc0 &^ bc4)
		a[9] = bc4 ^ (bc1 &^ bc0)

		t = a[5] ^ d0
		bc4 = bits.RotateLeft64(t, 18)
		t = a[16] ^ d1
		bc0 = bits.RotateLeft64(t, 1)
		t = a[2] ^ d2
		bc1 = bits.RotateLeft64(t, 6)
		t = a[13] ^ d3
		bc2 = bits.RotateLeft64(t, 25)
		t = a[24] ^ d4
		bc3 = bits.RotateLeft64(t, 8)
		a[5] = bc0 ^ (bc2 &^ bc1)
		a[16] = bc1 ^ (bc3 &^ bc2)
		a[2] = bc2 ^ (bc4 &^ bc3)
		a[13] = bc3 ^ (bc0 &^ bc4)
		a[24] = bc4 ^ (bc1 &^ bc0)

		t = a[20] ^ d0
		bc1 = bits.RotateLeft64(t, 36)
		t = a[6] ^ d1
		bc2 = bits.RotateLeft64(t, 10)
		t = a[17] ^ d2
		bc3 = bits.RotateLeft64(t, 15)
		t = a[3] ^ d3
		bc4 = bits.RotateLeft64(t, 56)
		t = a[14] ^ d4
		bc0 = bits.RotateLeft64(t, 27)
		a[20] = bc0 ^ (bc2 &^ bc1)
		a[6] = bc1 ^ (bc3 &^ bc2)
		a[17] = bc2 ^ (bc4 &^ bc3)
		a[3] = bc3 ^ (bc0 &^ bc4)
		a[14] = bc4 ^ (bc1 &^ bc0)

		t = a[10] ^ d0
		bc3 = bits.RotateLeft64(t, 41)
		t = a[21] ^ d1
		bc4 = bits.RotateLeft64(t, 2)
		t = a[7] ^ d2
		bc0 = bits.RotateLeft64(t, 62)
		t = a[18] ^ d3
		bc1 = bits.RotateLeft64(t, 55)
		t = a[4] ^ d4
		bc2 = bits.RotateLeft64(t, 39)
		a[10] = bc0 ^ (bc2 &^ bc1)
		a[21] = bc1 ^ (bc3 &^ bc2)
		a[7] = bc2 ^ (bc4 &^ bc3)
		a[18] = bc3 ^ (bc0 &^ bc4)
		a[4] = bc4 ^ (bc1 &^ bc0)

		// Round 4
		bc0 = a[0] ^ a[5] ^ a[10] ^ a[15] ^ a[20]
		bc1 = a[1] ^ a[6] ^ a[11] ^ a[16] ^ a[21]
		bc2 = a[2] ^ a[7] ^ a[12] ^ a[17] ^ a[22]
		bc3 = a[3] ^ a[8] ^ a[13] ^ a[18] ^ a[23]
		bc4 = a[4] ^ a[9] ^ a[14] ^ a[19] ^ a[24]
		d0 = bc4 ^ (bc1<<1 | bc1>>63)
		d1 = bc0 ^ (bc2<<1 | bc2>>63)
		d2 = bc1 ^ (bc3<<1 | bc3>>63)
		d3 = bc2 ^ (bc4<<1 | bc4>>63)
		d4 = bc3 ^ (bc0<<1 | bc0>>63)

		bc0 = a[0] ^ d0
		t = a[1] ^ d1
		bc1 = bits.RotateLeft64(t, 44)
		t = a[2] ^ d2
		bc2 = bits.RotateLeft64(t, 43)
		t = a[3] ^ d3
		bc3 = bits.RotateLeft64(t, 21)
		t = a[4] ^ d4
		bc4 = bits.RotateLeft64(t, 14)
		a[0] = bc0 ^ (bc2 &^ bc1) ^ rc[i+3]
		a[1] = bc1 ^ (bc3 &^ bc2)
		a[2] = bc2 ^ (bc4 &^ bc3)
		a[3] = bc3 ^ (bc0 &^ bc4)
		a[4] = bc4 ^ (bc1 &^ bc0)

		t = a[5] ^ d0
		bc2 = bits.RotateLeft64(t, 3)
		t = a[6] ^ d1
		bc3 = bits.RotateLeft64(t, 45)
		t = a[7] ^ d2
		bc4 = bits.RotateLeft64(t, 61)
		t = a[8] ^ d3
		bc0 = bits.RotateLeft64(t, 28)
		t = a[9] ^ d4
		bc1 = bits.RotateLeft64(t, 20)
		a[5] = bc0 ^ (bc2 &^ bc1)
		a[6] = bc1 ^ (bc3 &^ bc2)
		a[7] = bc2 ^ (bc4 &^ bc3)
		a[8] = bc3 ^ (bc0 &^ bc4)
		a[9] = bc4 ^ (bc1 &^ bc0)

		t = a[10] ^ d0
		bc4 = bits.RotateLeft64(t, 18)
		t = a[11] ^ d1
		bc0 = bits.RotateLeft64(t, 1)
		t = a[12] ^ d2
		bc1 = bits.RotateLeft64(t, 6)
		t = a[13] ^ d3
		bc2 = bits.RotateLeft64(t, 25)
		t = a[14] ^ d4
		bc3 = bits.RotateLeft64(t, 8)
		a[10] = bc0 ^ (bc2 &^ bc1)
		a[11] = bc1 ^ (bc3 &^ bc2)
		a[12] = bc2 ^ (bc4 &^ bc3)
		a[13] = bc3 ^ (bc0 &^ bc4)
		a[14] = bc4 ^ (bc1 &^ bc0)

		t = a[15] ^ d0
		bc1 = bits.RotateLeft64(t, 36)
		t = a[16] ^ d1
		bc2 = bits.RotateLeft64(t, 10)
		t = a[17] ^ d2
		bc3 = bits.RotateLeft64(t, 15)
		t = a[18] ^ d3
		bc4 = bits.RotateLeft64(t, 56)
		t = a[19] ^ d4
		bc0 = bits.RotateLeft64(t, 27)
		a[15] = bc0 ^ (bc2 &^ bc1)
		a[16] = bc1 ^ (bc3 &^ bc2)
		a[17] = bc2 ^ (bc4 &^ bc3)
		a[18] = bc3 ^ (bc0 &^ bc4)
		a[19] = bc4 ^ (bc1 &^ bc0)

		t = a[20] ^ d0
		bc3 = bits.RotateLeft64(t, 41)
		t = a[21] ^ d1
		bc4 = bits.RotateLeft64(t, 2)
		t = a[22] ^ d2
		bc0 = bits.RotateLeft64(t, 62)
		t = a[23] ^ d3
		bc1 = bits.RotateLeft64(t, 55)
		t = a[24] ^ d4
		bc2 = bits.RotateLeft64(t, 39)
		a[20] = bc0 ^ (bc2 &^ bc1)
		a[21] = bc1 ^ (bc3 &^ bc2)
		a[22] = bc2 ^ (bc4 &^ bc3)
		a[23] = bc3 ^ (bc0 &^ bc4)
		a[24] = bc4 ^ (bc1 &^ bc0)
	}
}


// ---- vendored from crypto/internal/fips140/sha3/sha3.go ----




// spongeDirection indicates the direction bytes are flowing through the sponge.
type spongeDirection int

const (
	// spongeAbsorbing indicates that the sponge is absorbing input.
	spongeAbsorbing spongeDirection = iota
	// spongeSqueezing indicates that the sponge is being squeezed.
	spongeSqueezing
)

type Digest struct {
	a [1600 / 8]byte // main state of the hash

	// a[n:rate] is the buffer. If absorbing, it's the remaining space to XOR
	// into before running the permutation. If squeezing, it's the remaining
	// output to produce before running the permutation.
	n, rate int

	// dsbyte contains the "domain separation" bits and the first bit of
	// the padding. Sections 6.1 and 6.2 of [1] separate the outputs of the
	// SHA-3 and SHAKE functions by appending bitstrings to the message.
	// Using a little-endian bit-ordering convention, these are "01" for SHA-3
	// and "1111" for SHAKE, or 00000010b and 00001111b, respectively. Then the
	// padding rule from section 5.1 is applied to pad the message to a multiple
	// of the rate, which involves adding a "1" bit, zero or more "0" bits, and
	// a final "1" bit. We merge the first "1" bit from the padding into dsbyte,
	// giving 00000110b (0x06) and 00011111b (0x1f).
	// [1] http://csrc.nist.gov/publications/drafts/fips-202/fips_202_draft.pdf
	//     "Draft FIPS 202: SHA-3 Standard: Permutation-Based Hash and
	//      Extendable-Output Functions (May 2014)"
	dsbyte byte

	outputLen int             // the default output size in bytes
	state     spongeDirection // whether the sponge is absorbing or squeezing
}

// BlockSize returns the rate of sponge underlying this hash function.
func (d *Digest) BlockSize() int { return d.rate }

// Size returns the output size of the hash function in bytes.
func (d *Digest) Size() int { return d.outputLen }

// Reset resets the Digest to its initial state.
func (d *Digest) Reset() {
	// Zero the permutation's state.
	clear(d.a[:])
	d.state = spongeAbsorbing
	d.n = 0
}

func (d *Digest) Clone() *Digest {
	ret := *d
	return &ret
}

// permute applies the KeccakF-1600 permutation.
func (d *Digest) permute() {
	keccakF1600(&d.a)
	d.n = 0
}

// padAndPermute appends the domain separation bits in dsbyte, applies
// the multi-bitrate 10..1 padding rule, and permutes the state.
func (d *Digest) padAndPermute() {
	// Pad with this instance's domain-separator bits. We know that there's
	// at least one byte of space in the sponge because, if it were full,
	// permute would have been called to empty it. dsbyte also contains the
	// first one bit for the padding. See the comment in the state struct.
	d.a[d.n] ^= d.dsbyte
	// This adds the final one bit for the padding. Because of the way that
	// bits are numbered from the LSB upwards, the final bit is the MSB of
	// the last byte.
	d.a[d.rate-1] ^= 0x80
	// Apply the permutation
	d.permute()
	d.state = spongeSqueezing
}

// Write absorbs more data into the hash's state.
type __mrDigest_WritePair struct {
	n int
	err error
}

func __mrDigest_Write(d *Digest, p []byte) __mrDigest_WritePair { __fw0, __fw1 := d.write(p)
return __mrDigest_WritePair{__fw0, __fw1} }

func (d *Digest) Write(p []byte) (n int, err error) {
	panic("crypto/internal/fips140/sha3: Write: multi-result desugar")
}
type __mrDigest_writeGenericPair struct {
	n int
	err error
}

func __mrDigest_writeGeneric(d *Digest, p []byte) __mrDigest_writeGenericPair {
	var n int
	var err error

	if d.state != spongeAbsorbing {
		panic("sha3: Write after Read")
	}

	n = len(p)

	for len(p) > 0 {
		x := subtle.XORBytes(d.a[d.n:d.rate], d.a[d.n:d.rate], p)
		d.n += x
		p = p[x:]

		// If the sponge is full, apply the permutation.
		if d.n == d.rate {
			d.permute()
		}
	}

	return __mrDigest_writeGenericPair{n, err}
}

func (d *Digest) writeGeneric(p []byte) (n int, err error) {
	panic("crypto/internal/fips140/sha3: writeGeneric: multi-result desugar")
}

// read squeezes an arbitrary number of bytes from the sponge.
type __mrDigest_readGenericPair struct {
	n int
	err error
}

func __mrDigest_readGeneric(d *Digest, out []byte) __mrDigest_readGenericPair {
	var n int
	var err error

	// If we're still absorbing, pad and apply the permutation.
	if d.state == spongeAbsorbing {
		d.padAndPermute()
	}

	n = len(out)

	// Now, do the squeezing.
	for len(out) > 0 {
		// Apply the permutation if we've squeezed the sponge dry.
		if d.n == d.rate {
			d.permute()
		}

		x := copy(out, d.a[d.n:d.rate])
		d.n += x
		out = out[x:]
	}

	return __mrDigest_readGenericPair{n, err}
}

func (d *Digest) readGeneric(out []byte) (n int, err error) {
	panic("crypto/internal/fips140/sha3: readGeneric: multi-result desugar")
}

// Sum appends the current hash to b and returns the resulting slice.
// It does not change the underlying hash state.
func (d *Digest) Sum(b []byte) []byte {
	fips140.RecordApproved()
	return d.sum(b)
}

func (d *Digest) sumGeneric(b []byte) []byte {
	if d.state != spongeAbsorbing {
		panic("sha3: Sum after Read")
	}

	// Make a copy of the original hash so that caller can keep writing
	// and summing.
	dup := d.Clone()
	hash := make([]byte, dup.outputLen, 64) // explicit cap to allow stack allocation
	dup.read(hash)
	return append(b, hash...)
}

const (
	magicSHA3   = "sha\x08"
	magicShake  = "sha\x09"
	magicCShake = "sha\x0a"
	magicKeccak = "sha\x0b"
	// magic || rate || main state || n || sponge direction
	marshaledSize = len(magicSHA3) + 1 + 200 + 1 + 1
)

type __mrDigest_MarshalBinaryPair struct {
	v0 []byte
	v1 error
}

func __mrDigest_MarshalBinary(d *Digest) __mrDigest_MarshalBinaryPair {
	__fw0, __fw1 := d.AppendBinary(make([]byte, 0, marshaledSize))
	return __mrDigest_MarshalBinaryPair{__fw0, __fw1}
}

func (d *Digest) MarshalBinary() ([]byte, error) {
	panic("crypto/internal/fips140/sha3: MarshalBinary: multi-result desugar")
}

type __mrDigest_AppendBinaryPair struct {
	v0 []byte
	v1 error
}

func __mrDigest_AppendBinary(d *Digest, b []byte) __mrDigest_AppendBinaryPair {
	switch d.dsbyte {
	case dsbyteSHA3:
		b = append(b, magicSHA3...)
	case dsbyteShake:
		b = append(b, magicShake...)
	case dsbyteCShake:
		b = append(b, magicCShake...)
	case dsbyteKeccak:
		b = append(b, magicKeccak...)
	default:
		panic("unknown dsbyte")
	}
	// rate is at most 168, and n is at most rate.
	b = append(b, byte(d.rate))
	b = append(b, d.a[:]...)
	b = append(b, byte(d.n), byte(d.state))
	return __mrDigest_AppendBinaryPair{b, nil}
}

func (d *Digest) AppendBinary(b []byte) ([]byte, error) {
	panic("crypto/internal/fips140/sha3: AppendBinary: multi-result desugar")
}

func (d *Digest) UnmarshalBinary(b []byte) error {
	if len(b) != marshaledSize {
		return errors.New("sha3: invalid hash state")
	}

	magic := string(b[:len(magicSHA3)])
	b = b[len(magicSHA3):]
	switch {
	case magic == magicSHA3 && d.dsbyte == dsbyteSHA3:
	case magic == magicShake && d.dsbyte == dsbyteShake:
	case magic == magicCShake && d.dsbyte == dsbyteCShake:
	case magic == magicKeccak && d.dsbyte == dsbyteKeccak:
	default:
		return errors.New("sha3: invalid hash state identifier")
	}

	rate := int(b[0])
	b = b[1:]
	if rate != d.rate {
		return errors.New("sha3: invalid hash state function")
	}

	copy(d.a[:], b)
	b = b[len(d.a):]

	n, state := int(b[0]), spongeDirection(b[1])
	if n > d.rate {
		return errors.New("sha3: invalid hash state")
	}
	d.n = n
	if state != spongeAbsorbing && state != spongeSqueezing {
		return errors.New("sha3: invalid hash state")
	}
	d.state = state

	return nil
}


// ---- vendored from crypto/internal/fips140/sha3/sha3_noasm.go ----


func keccakF1600(a *[200]byte) {
	keccakF1600Generic(a)
}

type __mrDigest_writePair struct {
	n int
	err error
}

func __mrDigest_write(d *Digest, p []byte) __mrDigest_writePair {
	__fw0, __fw1 := d.writeGeneric(p)
	return __mrDigest_writePair{__fw0, __fw1}
}

func (d *Digest) write(p []byte) (n int, err error) {
	panic("crypto/internal/fips140/sha3: write: multi-result desugar")
}
type __mrDigest_readPair struct {
	n int
	err error
}

func __mrDigest_read(d *Digest, out []byte) __mrDigest_readPair {
	__fw0, __fw1 := d.readGeneric(out)
	return __mrDigest_readPair{__fw0, __fw1}
}

func (d *Digest) read(out []byte) (n int, err error) {
	panic("crypto/internal/fips140/sha3: read: multi-result desugar")
}
func (d *Digest) sum(b []byte) []byte {
	return d.sumGeneric(b)
}


// ---- vendored from crypto/internal/fips140/sha3/shake.go ----




type SHAKE struct {
	d Digest // SHA-3 state context and Read/Write operations

	// initBlock is the cSHAKE specific initialization set of bytes. It is initialized
	// by newCShake function and stores concatenation of N followed by S, encoded
	// by the method specified in 3.3 of [1].
	// It is stored here in order for Reset() to be able to put context into
	// initial state.
	initBlock []byte
}

func bytepadWrite(c *SHAKE, data []byte, rate int) {
	rateEnc := leftEncode(uint64(rate))
	c.Write(rateEnc)
	c.Write(data)
	if padlen := rate - (len(rateEnc)+len(data))%rate; padlen < rate {
		const maxRate = rateK256
		c.Write(make([]byte, padlen, maxRate)) // explicit cap to allow stack allocation
	}
}

func leftEncode(x uint64) []byte {
	// Let n be the smallest positive integer for which 2^(8n) > x.
	n := (bits.Len64(x) + 7) / 8
	if n == 0 {
		n = 1
	}
	// Return n || x with n as a byte and x an n bytes in big-endian order.
	b := make([]byte, 9)
	byteorder.BEPutUint64(b[1:], x)
	b = b[9-n-1:]
	b[0] = byte(n)
	return b
}

func newCShake(c *SHAKE, N, S []byte, rate, outputLen int, dsbyte byte) *SHAKE {
	c.d = Digest{rate: rate, outputLen: outputLen, dsbyte: dsbyte}
	c.initBlock = make([]byte, 0, 9+len(N)+9+len(S)) // leftEncode returns max 9 bytes
	c.initBlock = append(c.initBlock, leftEncode(uint64(len(N))*8)...)
	c.initBlock = append(c.initBlock, N...)
	c.initBlock = append(c.initBlock, leftEncode(uint64(len(S))*8)...)
	c.initBlock = append(c.initBlock, S...)
	bytepadWrite(c, c.initBlock, c.d.rate)
	return c
}

func (s *SHAKE) BlockSize() int { return s.d.BlockSize() }
func (s *SHAKE) Size() int      { return s.d.Size() }

// Sum appends a portion of output to b and returns the resulting slice. The
// output length is selected to provide full-strength generic security: 32 bytes
// for SHAKE128 and 64 bytes for SHAKE256. It does not change the underlying
// state. It panics if any output has already been read.
func (s *SHAKE) Sum(in []byte) []byte { return s.d.Sum(in) }

// Write absorbs more data into the hash's state.
// It panics if any output has already been read.
type __mrSHAKE_WritePair struct {
	n int
	err error
}

func __mrSHAKE_Write(s *SHAKE, p []byte) __mrSHAKE_WritePair { __fw0, __fw1 := s.d.Write(p)
return __mrSHAKE_WritePair{__fw0, __fw1} }

func (s *SHAKE) Write(p []byte) (n int, err error) {
	panic("crypto/internal/fips140/sha3: Write: multi-result desugar")
}

type __mrSHAKE_ReadPair struct {
	n int
	err error
}

func __mrSHAKE_Read(s *SHAKE, out []byte) __mrSHAKE_ReadPair {
	fips140.RecordApproved()
	// Note that read is not exposed on Digest since SHA-3 does not offer
	// variable output length. It is only used internally by Sum.
	__fw0, __fw1 := s.d.read(out)
	return __mrSHAKE_ReadPair{__fw0, __fw1}
}

func (s *SHAKE) Read(out []byte) (n int, err error) {
	panic("crypto/internal/fips140/sha3: Read: multi-result desugar")
}

// Reset resets the hash to initial state.
func (s *SHAKE) Reset() {
	s.d.Reset()
	if len(s.initBlock) != 0 {
		bytepadWrite(s, s.initBlock, s.d.rate)
	}
}

// Clone returns a copy of the SHAKE context in its current state.
func (s *SHAKE) Clone() *SHAKE {
	ret := *s
	return &ret
}

type __mrSHAKE_MarshalBinaryPair struct {
	v0 []byte
	v1 error
}

func __mrSHAKE_MarshalBinary(s *SHAKE) __mrSHAKE_MarshalBinaryPair {
	__fw0, __fw1 := s.AppendBinary(make([]byte, 0, marshaledSize+len(s.initBlock)))
	return __mrSHAKE_MarshalBinaryPair{__fw0, __fw1}
}

func (s *SHAKE) MarshalBinary() ([]byte, error) {
	panic("crypto/internal/fips140/sha3: MarshalBinary: multi-result desugar")
}

type __mrSHAKE_AppendBinaryPair struct {
	v0 []byte
	v1 error
}

func __mrSHAKE_AppendBinary(s *SHAKE, b []byte) __mrSHAKE_AppendBinaryPair {
	b, err := s.d.AppendBinary(b)
	if err != nil {
		return __mrSHAKE_AppendBinaryPair{nil, err}
	}
	b = append(b, s.initBlock...)
	return __mrSHAKE_AppendBinaryPair{b, nil}
}

func (s *SHAKE) AppendBinary(b []byte) ([]byte, error) {
	panic("crypto/internal/fips140/sha3: AppendBinary: multi-result desugar")
}

func (s *SHAKE) UnmarshalBinary(b []byte) error {
	if len(b) < marshaledSize {
		return errors.New("sha3: invalid hash state")
	}
	if err := s.d.UnmarshalBinary(b[:marshaledSize]); err != nil {
		return err
	}
	s.initBlock = bytes.Clone(b[marshaledSize:])
	return nil
}

// NewShake128 creates a new SHAKE128 XOF.
func NewShake128() *SHAKE {
	return &SHAKE{d: Digest{rate: rateK256, outputLen: 32, dsbyte: dsbyteShake}}
}

// NewShake256 creates a new SHAKE256 XOF.
func NewShake256() *SHAKE {
	return &SHAKE{d: Digest{rate: rateK512, outputLen: 64, dsbyte: dsbyteShake}}
}

// NewCShake128 creates a new cSHAKE128 XOF.
//
// N is used to define functions based on cSHAKE, it can be empty when plain
// cSHAKE is desired. S is a customization byte string used for domain
// separation. When N and S are both empty, this is equivalent to NewShake128.
func NewCShake128(N, S []byte) *SHAKE {
	// The actual logic is in a separate function to outline this allocation.
	c := &SHAKE{}
	return newCShake128(c, N, S)
}

func newCShake128(c *SHAKE, N, S []byte) *SHAKE {
	if len(N) == 0 && len(S) == 0 {
		*c = *NewShake128()
		return c
	}
	return newCShake(c, N, S, rateK256, 32, dsbyteCShake)
}

// NewCShake256 creates a new cSHAKE256 XOF.
//
// N is used to define functions based on cSHAKE, it can be empty when plain
// cSHAKE is desired. S is a customization byte string used for domain
// separation. When N and S are both empty, this is equivalent to NewShake256.
func NewCShake256(N, S []byte) *SHAKE {
	// The actual logic is in a separate function to outline this allocation.
	c := &SHAKE{}
	return newCShake256(c, N, S)
}

func newCShake256(c *SHAKE, N, S []byte) *SHAKE {
	if len(N) == 0 && len(S) == 0 {
		*c = *NewShake256()
		return c
	}
	return newCShake(c, N, S, rateK512, 64, dsbyteCShake)
}

`
