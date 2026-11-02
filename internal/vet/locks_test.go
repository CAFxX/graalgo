package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression test: passing/returning/assigning a *sync.Mutex copies no
// lock, so copylocks must stay silent for pointer sites; copying a
// sync.Mutex value (or a struct holding one by value) must still be
// flagged. The two programs are analyzed separately so the pointer
// program must yield exactly zero copylocks diagnostics.
func TestCopyLocksPointerTypes(t *testing.T) {
	ptrSrc := `package main

import "sync"

type byPtr struct{ Mu *sync.Mutex }

func sinkPtr(m *sync.Mutex) { _ = m }
func sinkByPtr(h byPtr)     { _ = h }

func main() {
	var mu sync.Mutex
	muPtr := &mu
	sinkPtr(muPtr)
	_ = muPtr
	sinkByPtr(byPtr{Mu: muPtr})
	_ = func() *sync.Mutex { return muPtr }()
}
`
	valSrc := `package main

import "sync"

type byVal struct{ Mu sync.Mutex }

func sinkVal(m sync.Mutex) { _ = m }
func sinkByVal(h byVal)    { _ = h }

func main() {
	var mu sync.Mutex
	sinkVal(mu)
	sinkByVal(byVal{Mu: mu})
	_ = func() sync.Mutex { return mu }()
}
`
	run := func(t *testing.T, name, src string) []string {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, name+".go")
		if err := os.WriteFile(path, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		ds, err := Run([]string{path})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, d := range ds {
			if strings.Contains(d.Message, "copies lock") {
				got = append(got, d.Message)
			}
		}
		return got
	}

	if got := run(t, "ptr", ptrSrc); len(got) != 0 {
		t.Errorf("pointer program: expected 0 copylocks diagnostics, got %d: %v", len(got), got)
	}
	got := run(t, "val", valSrc)
	joined := strings.Join(got, "\n")
	for _, want := range []string{"call copies lock value", "return copies lock value", "assignment copies lock value"} {
		if !strings.Contains(joined, want) {
			t.Errorf("value program: expected diagnostic %q, got: %v", want, got)
		}
	}
}
