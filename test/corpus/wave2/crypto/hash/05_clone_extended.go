// Extended clone/marshal coverage vs gc: clone independence, clone-of-clone,
// marshal determinism, unmarshal-over-state. Relational assertions fail with
// os.Exit(1); the differential harness additionally byte-compares vs gc.
// A broken Clone (e.g. shallow copy sharing state) fails here under graalgo
// while passing under gc.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"fmt"
	"hash"
	"os"
)

var failures int

func fail(format string, args ...interface{}) {
	fmt.Printf("FAIL "+format+"\n", args...)
	failures++
}

func sumHex(h hash.Hash) string { return hex.EncodeToString(h.Sum(nil)) }

func main() {
	// Anchor KATs (universally known; hard-coded so the test can fail).
	if got := sumHex(sha256.New()); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		fail("sha256 empty = %s", got)
	}
	h0 := sha256.New()
	h0.Write([]byte("abc"))
	if got := sumHex(h0); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		fail("sha256 abc = %s", got)
	}

	// 1. Clone at empty state; identical writes -> identical sums.
	e1 := sha256.New()
	c1, err := e1.(hash.Cloner).Clone()
	if err != nil {
		fail("clone empty: %v", err)
	}
	e1.Write([]byte("data"))
	c1.(hash.Hash).Write([]byte("data"))
	if sumHex(e1) != sumHex(c1.(hash.Hash)) {
		fail("clone-empty divergence")
	}

	// 2. Clone independence: after cloning, the two states diverge.
	h := sha256.New()
	h.Write([]byte("a"))
	c, err := h.(hash.Cloner).Clone()
	if err != nil {
		fail("clone: %v", err)
	}
	h.Write([]byte("b"))
	c.(hash.Hash).Write([]byte("c"))
	sh, sc := sumHex(h), sumHex(c.(hash.Hash))
	if sh == sc {
		fail("clone not independent: both %s", sh)
	}
	// Each side must equal a fresh hash with the same input.
	ha := sha256.New()
	ha.Write([]byte("ab"))
	if sh != sumHex(ha) {
		fail("original after clone: got %s want %s", sh, sumHex(ha))
	}
	hb := sha256.New()
	hb.Write([]byte("ac"))
	if sc != sumHex(hb) {
		fail("clone after clone: got %s want %s", sc, sumHex(hb))
	}

	// 3. Clone of a clone.
	c2, err := c.(hash.Cloner).Clone()
	if err != nil {
		fail("clone-of-clone: %v", err)
	}
	c.(hash.Hash).Write([]byte("d"))
	c2.(hash.Hash).Write([]byte("d"))
	if sumHex(c.(hash.Hash)) != sumHex(c2.(hash.Hash)) {
		fail("clone-of-clone divergence")
	}

	// 4. Marshal determinism: same state marshals to identical bytes.
	m1, err := h.(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		fail("marshal: %v", err)
	}
	m2, err := h.(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		fail("marshal2: %v", err)
	}
	if !bytes.Equal(m1, m2) {
		fail("marshal not deterministic")
	}

	// 5. Unmarshal over non-empty state replaces it.
	h2 := sha256.New()
	h2.Write([]byte("junk junk junk"))
	if err := h2.(encoding.BinaryUnmarshaler).UnmarshalBinary(m1); err != nil {
		fail("unmarshal: %v", err)
	}
	if sumHex(h2) != sh {
		fail("unmarshal-over-state: got %s want %s", sumHex(h2), sh)
	}
	// And the restored hash continues correctly.
	h2.Write([]byte("e"))
	he := sha256.New()
	he.Write([]byte("abe"))
	if sumHex(h2) != sumHex(he) {
		fail("restored hash continuation: got %s want %s", sumHex(h2), sumHex(he))
	}

	// 6. Marshal empty state round-trip.
	me, err := sha256.New().(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		fail("marshal empty: %v", err)
	}
	h3 := sha256.New()
	h3.Write([]byte("pollutes the state"))
	if err := h3.(encoding.BinaryUnmarshaler).UnmarshalBinary(me); err != nil {
		fail("unmarshal empty: %v", err)
	}
	if sumHex(h3) != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		fail("empty round-trip sum = %s", sumHex(h3))
	}

	if failures > 0 {
		os.Exit(1)
	}
	fmt.Println("PASS")
}
