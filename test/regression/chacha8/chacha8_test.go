package chacha8

import (
	"bytes"
	"encoding/hex"
	"io"
	randv2 "math/rand/v2"
	"testing"
)

// TestChaCha8ReadBytes pins the guest ChaCha8 stream to gc vectors
// (review23-chacha8, P1-7): the ChaCha8 type was a panic stub before
// this fix, so any Read here failed. Vectors are gc 1.27.1 output for
// seed [32]byte{0,1,...,31}: the first 64 Read bytes, and the
// MarshalBinary encoding after a 37-byte Read (which leaves 3 pending
// readBuf bytes, encoded with the "readbuf:" prefix).
//
// NOTE (scope): this package deliberately imports ONLY math/rand/v2
// (plus bytes/hex/io) — no crypto/ecdsa. The ecdsa non-standard-curve
// signing test lives pending at
// ~/workspace/goals/graalgo-go-toolchain-on-graalvm-truffle/hidden_files/codebase-review/ecdsa-pending-test/
// because every program importing crypto/ecdsa currently fails at
// backend AST build with "unsupported expression kind funcfieldcall"
// (crypto/tls ConnectionState.ExportKeyingMaterial, a pre-existing
// backend gap unrelated to ChaCha8). Land it under test/regression/
// once the backend implements the funcfieldcall wire kind.
// TestChaCha8ReadBytes pins the guest ChaCha8 stream to gc vectors
// (review23-chacha8, P1-7): the ChaCha8 type was a panic stub before
// this fix, so any Read here failed. Vectors are gc 1.27.1 output for
// seed [32]byte{0,1,...,31}: the first 64 Read bytes, and the
// MarshalBinary encoding after a 37-byte Read (which leaves 3 pending
// readBuf bytes, encoded with the "readbuf:" prefix).
func TestChaCha8ReadBytes(t *testing.T) {
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i)
	}
	want64, err := hex.DecodeString("db9c412e11a2fdadddfe0e8df43bb39a00ae8a375a4e98261b5309c367d5bc7aa7bb0402c37c91351a098e61de3bab18bd5ff2c7a3a4542ea0984bdd11db209b")
	if err != nil {
		t.Fatal(err)
	}
	c := randv2.NewChaCha8(seed)
	buf := make([]byte, 64)
	var r io.Reader = c
	if n, err := io.ReadFull(r, buf); err != nil || n != 64 {
		t.Fatalf("io.ReadFull via io.Reader: n=%d err=%v", n, err)
	}
	if !bytes.Equal(buf, want64) {
		t.Fatalf("Read64 mismatch:\n got %x\nwant %x", buf, want64)
	}
	// A second, fresh instance must produce the same stream, and a
	// 37-byte Read must be a prefix of the 64-byte stream.
	c2 := randv2.NewChaCha8(seed)
	b37 := make([]byte, 37)
	if n, err := c2.Read(b37); err != nil || n != 37 {
		t.Fatalf("direct Read(37): n=%d err=%v", n, err)
	}
	if !bytes.Equal(b37, want64[:37]) {
		t.Fatalf("Read37 is not a prefix of Read64:\n got %x\nwant %x", b37, want64[:37])
	}
	// MarshalBinary after the partial Read: exact gc encoding.
	wantEnc, err := hex.DecodeString("726561646275663a037c9135636861636861383a0000000000000005000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if err != nil {
		t.Fatal(err)
	}
	enc, err := c2.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	if !bytes.Equal(enc, wantEnc) {
		t.Fatalf("MarshalBinary mismatch:\n got %x\nwant %x", enc, wantEnc)
	}
	// Unmarshal into a fresh instance must resume the same stream.
	c3 := randv2.NewChaCha8(seed)
	if err := c3.UnmarshalBinary(enc); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	// c2 and c3 are now at the same position: their next bytes agree.
	next2 := make([]byte, 32)
	next3 := make([]byte, 32)
	if _, err := c2.Read(next2); err != nil {
		t.Fatal(err)
	}
	if _, err := c3.Read(next3); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(next2, next3) {
		t.Fatalf("post-unmarshal streams diverged:\n got %x\nwant %x", next3, next2)
	}
}
