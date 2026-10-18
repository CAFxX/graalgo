package vet

import (
	"strings"
	"testing"
)

// RV18-P1-21(1): typeContainsLock unwrapped pointers, so passing *sync.Mutex
// (which copies no lock) was flagged as "call copies lock value", contradicting
// the comment above the code and go vet's behavior.
func TestCopyLocksPointerNotFlagged(t *testing.T) {
	src := `package main

import "sync"

func takePtr(m *sync.Mutex) { _ = m }

type Holder struct{ mu *sync.Mutex }

func takeHolder(h Holder) { _ = h }

func takeVal(m sync.Mutex) {} // body must not assign m: that would be a real copy

func main() {
	var m sync.Mutex
	takePtr(&m)                 // must not flag: a pointer copies no lock
	takeHolder(Holder{mu: &m})  // must not flag: struct holds only a pointer
	_ = &m                      // must not flag: taking an address copies no lock
	takeVal(m)                  // MUST flag: copies the lock value
}
`
	joined := runVetOnSrc(t, src)
	if n := strings.Count(joined, "call copies lock value"); n != 1 {
		t.Errorf("expected exactly 1 'call copies lock value' diagnostic (takeVal), got %d:\n%s", n, joined)
	}
	if strings.Contains(joined, "assignment copies lock value") {
		t.Errorf("false positive on pointer assignments:\n%s", joined)
	}
}

func TestCopyLocksReturnPointerNotFlagged(t *testing.T) {
	src := `package main

import "sync"

var g sync.Mutex

func ptr() *sync.Mutex { return &g } // must not flag

func val() sync.Mutex { return g } // MUST flag

func main() {
	_ = ptr()
	_ = val()
}
`
	joined := runVetOnSrc(t, src)
	// Only val (returning the lock value) may be flagged; ptr (returning
	// a pointer) copies no lock.
	if n := strings.Count(joined, "return copies lock value"); n != 1 {
		t.Errorf("expected exactly 1 'return copies lock value' diagnostic (val), got %d:\n%s", n, joined)
	}
}
