package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTypeCheckFailureSurfaced is the regression test for P1-12
// (review 2026-10-30): a package that fails go/types checking must not
// yield a clean vet run. Before the fix, Run discarded the conf.Check
// error, so such a file produced zero diagnostics and the caller
// printed "vet: no issues found" — a false clean on partial type
// information.
func TestTypeCheckFailureSurfaced(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "unresolvable import",
			src: `package p

import "example.com/nonexistent/pkg"

var _ = pkg.X
`,
		},
		{
			name: "undefined identifier",
			src: `package p

func f() {
	_ = undefinedIdent
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "bad.go")
			if err := os.WriteFile(path, []byte(tc.src), 0644); err != nil {
				t.Fatal(err)
			}
			_, err := Run([]string{path})
			if err == nil {
				t.Errorf("Run returned nil error for %s; a clean verdict must be impossible when type checking fails", tc.name)
			}
		})
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
