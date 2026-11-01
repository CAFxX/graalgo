package chacha8

import (
	"bytes"
	"encoding/hex"
	"io"
	randv2 "math/rand/v2"
	"testing"
)

// Regression test for review-2026-11-01 F-03: math/rand/v2.ChaCha8 was
// a fail-fast stub (NewChaCha8 kept the seed, Read panicked), so any
// program using it — including crypto/ecdsa signLegacy for non-standard
// curves, which reads its ChaCha8 through io.Reader — crashed.
//
// The fix ports gc 1.27.1's chacha8.go (with internal/chacha8rand
// inlined as __chacha8* helpers) and pair-transforms the multi-result
// methods (WS D, bytes.Buffer.Read precedent, wired via
// m7MultiResultFuncs).
//
// Golden vectors below were printed by stock gc 1.27.1 for the fixed
// seed 0..31 (and the all-zero seed). They pin the stream across block
// refills and the reseed boundary (~draw 156), so a regression in the
// block, refill, or reseed path fails loudly instead of drifting.
// Pre-fix, every test here panics in ChaCha8.Read (or fails to link
// the stub), so the suite is falsifiable, not tautological.
func fixedSeed() [32]byte {
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i)
	}
	return seed
}

var chacha8Golden = map[int]uint64{
	0: 0xadfda2112e419cdb, 1: 0x9ab33bf48d0efedd,
	2: 0x26984e5a378aae00, 3: 0x7abcd567c309531b,
	4: 0x35917cc30204bba7, 5: 0x18ab3bde618e091a,
	6: 0x2e54a4a3c7f25fbd, 7: 0x9b20db11dd4b98a0,
	8: 0xce23b0b027178ff7, 9: 0x5be364a802d70964,
	10: 0x9b65d7794fd9b290, 11: 0xa3af83cb2db2343b,
	12: 0x3779ee32d72e6b32, 13: 0x7ac00930712a4a36,
	14: 0x8972723ecbbebcff, 15: 0x22e294e08e884f8f,
	100: 0x43d6dcadedccc266, 101: 0x8597e8685fe08f15,
	102: 0xba310d86c5a419ec, 103: 0x4ff8b2b8e11aaa56,
	104: 0x6d9b42e022658ea8, 105: 0xcf64a1748f2c0e5c,
	150: 0x58641d4c64b32849, 151: 0x58d56dc821a55ae5,
	152: 0x5ba8dc6fd8b02453, 153: 0x17925c770ebbf894,
	154: 0x7a5a2441c1bb2ad8, 155: 0x1f51781d91c24b43,
	156: 0x96c40a2f8f4e7dc0, 157: 0xf6dd148b52b0ec3f,
	158: 0xe5626085e4a62a87, 159: 0xe282b73676500b26,
	160: 0x7902488b0fc26c24, 161: 0x713ddb0cd548a25b,
	162: 0xc26c72317158d54a, 163: 0x6414a22ca8a92315,
	164: 0xf81e3fd75a8f9866, 165: 0x90cf0fe034baf981,
	200: 0x5776f60812f6f79d, 201: 0xc275e08ca651cf5d,
	202: 0xdac4fd8d67653250, 203: 0xe145441771d90c8e,
	204: 0xab8ca91118176721, 205: 0xbc35e936ad15448d,
}

func TestChaCha8Uint64Golden(t *testing.T) {
	c := randv2.NewChaCha8(fixedSeed())
	for i := 0; i <= 205; i++ {
		v := c.Uint64()
		if want, ok := chacha8Golden[i]; ok && v != want {
			t.Fatalf("draw %d: got %x, want %x", i, v, want)
		}
	}
	// All-zero seed, first value.
	var z [32]byte
	if got := randv2.NewChaCha8(z).Uint64(); got != 0xac8a366dce7e87d9 {
		t.Fatalf("zero seed draw 0: got %x, want ac8a366dce7e87d9", got)
	}
}

func TestChaCha8ReadGolden(t *testing.T) {
	// Direct Read: 64 bytes, little-endian Uint64 words.
	c := randv2.NewChaCha8(fixedSeed())
	buf := make([]byte, 64)
	n, err := c.Read(buf)
	if err != nil || n != 64 {
		t.Fatalf("Read(64): n=%d err=%v", n, err)
	}
	want, _ := hex.DecodeString("db9c412e11a2fdadddfe0e8df43bb39a00ae8a375a4e98261b5309c367d5bc7aa7bb0402c37c91351a098e61de3bab18bd5ff2c7a3a4542ea0984bdd11db209b")
	if !bytes.Equal(buf, want) {
		t.Fatalf("Read(64) mismatch:\n got %x\nwant %x", buf, want)
	}
	// io.Reader interface path (the signLegacy shape): ReadFull must
	// return the same bytes as a direct Read of a fresh instance.
	var rd io.Reader = randv2.NewChaCha8(fixedSeed())
	full := make([]byte, 64)
	if _, err := io.ReadFull(rd, full); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	if !bytes.Equal(full, want) {
		t.Fatalf("ReadFull mismatch:\n got %x\nwant %x", full, want)
	}
	// Short reads carry the remainder in readBuf: 5 + 13 bytes must
	// equal the first 18 bytes of the direct stream.
	cs := randv2.NewChaCha8(fixedSeed())
	small := make([]byte, 5)
	if n, _ := cs.Read(small); n != 5 {
		t.Fatalf("Read(5): n=%d", n)
	}
	mid := make([]byte, 13)
	if n, _ := cs.Read(mid); n != 13 {
		t.Fatalf("Read(13): n=%d", n)
	}
	if got := append(append([]byte{}, small...), mid...); !bytes.Equal(got, want[:18]) {
		t.Fatalf("short reads mismatch:\n got %x\nwant %x", got, want[:18])
	}
}

func TestChaCha8MarshalRoundTrip(t *testing.T) {
	// Marshal after 3 draws: exact encoding bytes.
	cm := randv2.NewChaCha8(fixedSeed())
	for i := 0; i < 3; i++ {
		cm.Uint64()
	}
	enc, err := cm.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	want, _ := hex.DecodeString("636861636861383a0000000000000003000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if !bytes.Equal(enc, want) {
		t.Fatalf("MarshalBinary mismatch:\n got %x\nwant %x", enc, want)
	}
	var cu randv2.ChaCha8
	if err := cu.UnmarshalBinary(enc); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	for i := 0; i < 10; i++ {
		if a, b := cm.Uint64(), cu.Uint64(); a != b {
			t.Fatalf("round-trip draw %d: %x != %x", i, a, b)
		}
	}
	// Marshal with a partial readBuf (Read(5) leaves 3 bytes): the
	// encoding carries a "readbuf:" prefix and must still round-trip.
	cp := randv2.NewChaCha8(fixedSeed())
	if _, err := cp.Read(make([]byte, 5)); err != nil {
		t.Fatalf("Read(5): %v", err)
	}
	enc2, err := cp.AppendBinary(nil)
	if err != nil {
		t.Fatalf("AppendBinary: %v", err)
	}
	want2, _ := hex.DecodeString("726561646275663a03a2fdad636861636861383a0000000000000001000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if !bytes.Equal(enc2, want2) {
		t.Fatalf("AppendBinary mismatch:\n got %x\nwant %x", enc2, want2)
	}
	var cu2 randv2.ChaCha8
	if err := cu2.UnmarshalBinary(enc2); err != nil {
		t.Fatalf("UnmarshalBinary(readbuf): %v", err)
	}
	for i := 0; i < 10; i++ {
		if a, b := cp.Uint64(), cu2.Uint64(); a != b {
			t.Fatalf("readbuf round-trip draw %d: %x != %x", i, a, b)
		}
	}
	// Malformed encodings are rejected, not fatal.
	var bad randv2.ChaCha8
	if err := bad.UnmarshalBinary([]byte("garbage")); err == nil {
		t.Fatalf("UnmarshalBinary(garbage): want error, got nil")
	}
}
