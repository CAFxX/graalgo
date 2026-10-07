// rand.Reader/Read/Text are REAL in P3-M8 via graalgo.os.getrandom
// (SecureRandom). Random VALUES can't be byte-compared, so this program
// checks deterministic PROPERTIES: lengths, nil errors, alphabet
// membership, and Reader non-nil. Output is fixed strings only.
package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"strings"
)

func fail(msg string) {
	fmt.Println("FAIL:", msg)
	os.Exit(1)
}

func main() {
	if rand.Reader == nil {
		fail("Reader is nil")
	}
	b := make([]byte, 16)
	n, err := rand.Read(b)
	if err != nil {
		fail("Read err")
	}
	if n != 16 {
		fail("Read n")
	}
	n, err = rand.Reader.Read(b)
	if err != nil || n != 16 {
		fail("Reader.Read")
	}
	// empty read: deterministic (0, nil)
	n, err = rand.Read(b[:0])
	if err != nil || n != 0 {
		fail("empty Read")
	}
	t := rand.Text()
	if len(t) != 26 {
		fail("Text len")
	}
	const base32 = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	for i := 0; i < len(t); i++ {
		if !strings.ContainsRune(base32, rune(t[i])) {
			fail("Text alphabet")
		}
	}
	fmt.Println("PASS")
}
