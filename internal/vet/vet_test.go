package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
