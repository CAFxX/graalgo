package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyLocksPointerNotFlagged(t *testing.T) {
	// Passing a *sync.Mutex (or a struct holding only a *sync.Mutex)
	// copies only the pointer, never the lock, so copylocks must stay
	// silent. The single "copies lock" diagnostic below is the positive
	// control: _ = mu copies a sync.Mutex value and must still fire.
	src := `package main

import "sync"

type holder struct {
	mu *sync.Mutex
}

func usePtr(mu *sync.Mutex) {
	_ = mu
}

func useHolder(h holder) {
	_ = h
}

func makePtr(m sync.Mutex) *sync.Mutex {
	return &m
}

func main() {
	var mu sync.Mutex
	usePtr(&mu)
	p := &mu
	_ = p
	h := holder{mu: &mu}
	useHolder(h)
	_ = mu
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "ptr.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range ds {
		msgs = append(msgs, d.Message)
	}
	joined := strings.Join(msgs, "\n")
	t.Logf("diagnostics:\n%s", joined)
	n := strings.Count(joined, "copies lock")
	if n != 1 {
		t.Errorf("expected exactly 1 \"copies lock\" diagnostic (the _ = mu positive control), got %d:\n%s", n, joined)
	}
}

func TestAnalyzers(t *testing.T) {
	src := `package main

import (
	"context"
	"fmt"
	"sync"
)

func main() {
	fmt.Printf("%d", "hello")
	fmt.Printf("%d %d", 1)
	s := string(65)
	_ = s
	var f func()
	if f == nil {
		fmt.Println("nil")
	}
	x := true
	if x == true {
		fmt.Println("x")
	}
	fmt.Println("done")
	return
	fmt.Println("unreachable")
}

func leaky() {
	ctx, cancel := context.WithCancel(context.Background())
	_ = ctx
	_ = cancel
}

func copyLock(m sync.Mutex) {
	_ = m
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range ds {
		msgs = append(msgs, d.Message)
	}
	joined := strings.Join(msgs, "\n")
	t.Logf("diagnostics:\n%s", joined)

	// Each analyzer should fire at least once.
	want := []string{
		"expects integer",       // printf %d with string
		"missing argument",      // printf missing arg
		"one rune",              // stringintconv
		"always false",          // nilfunc
		"redundant comparison",  // bools
		"unreachable",           // unreachable
		"context leak",          // lostcancel
		"copies lock",           // copylocks
	}
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Errorf("expected diagnostic containing %q, got:\n%s", w, joined)
		}
	}
}
