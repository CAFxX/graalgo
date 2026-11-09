package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runFixture writes src to a temp file, runs all analyzers on it, and
// returns the diagnostics.
func runFixture(t *testing.T, src string) []Diagnostic {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func lockDiags(ds []Diagnostic) []Diagnostic {
	var out []Diagnostic
	for _, d := range ds {
		if strings.Contains(d.Message, "copies lock") {
			out = append(out, d)
		}
	}
	return out
}

func atomicDiags(ds []Diagnostic) []Diagnostic {
	var out []Diagnostic
	for _, d := range ds {
		if strings.Contains(d.Message, "direct assignment to atomic value") {
			out = append(out, d)
		}
	}
	return out
}

// R05-V-04: passing a pointer to a lock must be quiet — passing a
// pointer copies no lock. Only passing/returning/assigning the lock
// value itself may fire.
func TestCopyLocksPointerQuiet(t *testing.T) {
	src := `package main

import "sync"

type holder struct {
	mu sync.Mutex
}

func takeMutexPtr(p *sync.Mutex)  {}
func takeHolderPtr(p *holder)     {}

func quiet(mu sync.Mutex, s holder) *sync.Mutex {
	takeMutexPtr(&mu)
	takeHolderPtr(&s)
	p := &mu
	_ = p
	q := &s
	_ = q
	return &mu
}
`
	ds := lockDiags(runFixture(t, src))
	if len(ds) != 0 {
		t.Fatalf("expected no copylocks diagnostics for pointer uses, got %d: %v", len(ds), ds)
	}
}

func TestCopyLocksValueStillFires(t *testing.T) {
	src := `package main

import "sync"

type holder struct {
	mu sync.Mutex
}

func takeMutex(m sync.Mutex) {}
func takeHolder(h holder)    {}

func noisy(mu sync.Mutex, s holder) sync.Mutex {
	takeMutex(mu)
	takeHolder(s)
	r := mu
	_ = r
	u := s
	_ = u
	return mu
}
`
	ds := lockDiags(runFixture(t, src))
	// takeMutex(mu), takeHolder(s), r := mu, _ = r, u := s, _ = u,
	// return mu. The `_ = r` / `_ = u` lines re-copy the lock value,
	// so they fire too.
	if len(ds) != 7 {
		t.Fatalf("expected 7 copylocks diagnostics for value copies, got %d: %v", len(ds), ds)
	}
}

// R05-V-14: upstream's atomic check is the Add* self-assignment
// (x = atomic.AddInt32(&x, 1)); direct assignment to an atomic-typed
// variable or field must also fire. The old field-of-atomic pattern
// cannot occur on compilable code and is gone.
func TestAtomicAddSelfAssignment(t *testing.T) {
	src := `package main

import "sync/atomic"

func f(x int32, p *int32) {
	x = atomic.AddInt32(&x, 1)
	y := int32(0)
	y = atomic.AddInt32(&x, 1)
	atomic.AddInt32(&x, 1)
	*p = atomic.AddInt32(p, 1)
	z := atomic.AddInt32(&x, 1)
	_ = z
}
`
	ds := atomicDiags(runFixture(t, src))
	// x = atomic.AddInt32(&x, 1) and *p = atomic.AddInt32(p, 1).
	if len(ds) != 2 {
		t.Fatalf("expected 2 atomic diagnostics for Add* self-assignment, got %d: %v", len(ds), ds)
	}
}

func TestAtomicDirectAssignment(t *testing.T) {
	src := `package main

import "sync/atomic"

type holder struct {
	v atomic.Int64
}

func f(v atomic.Int64, p *atomic.Int64, s *holder) {
	v = atomic.Int64{}
	*p = atomic.Int64{}
	s.v = atomic.Int64{}
	w := atomic.Int64{}
	_ = w
	v.Store(1)
}
`
	ds := atomicDiags(runFixture(t, src))
	// v = ..., *p = ..., s.v = ...; the := declaration and Store are quiet.
	if len(ds) != 3 {
		t.Fatalf("expected 3 atomic diagnostics for direct assignment, got %d: %v", len(ds), ds)
	}
}
