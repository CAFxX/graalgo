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
	if f == nil { // not flagged: a nilable func variable
		fmt.Println("nil")
	}
	if leaky == nil { // flagged: comparison of a declared function to nil
		fmt.Println("never")
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
		"expects integer",      // printf %d with string
		"missing argument",     // printf missing arg
		"one rune",             // stringintconv
		"always false",         // nilfunc
		"redundant comparison", // bools
		"unreachable",          // unreachable
		"context leak",         // lostcancel
		"copies lock",          // copylocks
	}
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Errorf("expected diagnostic containing %q, got:\n%s", w, joined)
		}
	}
}

// TestNilFunc pins nilfunc to go vet's semantics (verified against
// golang.org/x/tools@v0.52.0's nilfunc analyzer and the go1.27.1
// toolchain's go vet): only a *declared* function compared to nil
// fires — never a nilable func variable.
func TestNilFunc(t *testing.T) {
	src := `package main

import "fmt"

func declared() {}

func generic[T any](T) {}

type recv struct{}

func (recv) method() {}

func main() {
	var f func()
	_ = f == nil // want-clean: nilable func variable
	_ = declared == nil // want-nilfunc: declared function
	_ = declared != nil // want-nilfunc: declared function
	_ = fmt.Println == nil // want-nilfunc: qualified declared function
	var r recv
	_ = r.method == nil // want-nilfunc: method value
	_ = generic[int] == nil // want-nilfunc: instantiated generic function
	_ = (declared) == nil // want-nilfunc: parenthesized
	var g = declared
	_ = g == nil // want-clean: variable of func type
	f = declared
	_ = f == nil // want-clean: variable assigned a func
	_ = f != nil // want-clean: variable assigned a func
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "nilfunc.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}

	wantDiag := map[int]bool{}
	for i, ln := range strings.Split(src, "\n") {
		switch {
		case strings.Contains(ln, "want-nilfunc"):
			wantDiag[i+1] = true
		case strings.Contains(ln, "want-clean"):
			wantDiag[i+1] = false
		}
	}
	if len(wantDiag) != 10 {
		t.Fatalf("expected 10 marked lines, found %d", len(wantDiag))
	}
	got := map[int][]string{}
	for _, d := range ds {
		if strings.Contains(d.Message, "comparison of function") {
			got[d.Pos.Line] = append(got[d.Pos.Line], d.Message)
		}
	}
	for ln, want := range wantDiag {
		msgs := got[ln]
		if want && len(msgs) == 0 {
			t.Errorf("line %d: expected nilfunc diagnostic, got none", ln)
		}
		if !want && len(msgs) > 0 {
			t.Errorf("line %d: expected no nilfunc diagnostic, got %q", ln, msgs)
		}
	}
	for ln, msgs := range got {
		if _, ok := wantDiag[ln]; !ok {
			t.Errorf("line %d: unexpected nilfunc diagnostic %q", ln, msgs)
		}
	}

	// Pin the diagnostic wording for the plain declared-function cases.
	msgOn := func(fragment string) string {
		for i, ln := range strings.Split(src, "\n") {
			if strings.Contains(ln, fragment) && strings.Contains(ln, "want-nilfunc") {
				return strings.Join(got[i+1], "\n")
			}
		}
		return ""
	}
	if m := msgOn("_ = declared == nil"); !strings.Contains(m, "comparison of function declared == nil is always false") {
		t.Errorf(`bad message for "declared == nil": %q`, m)
	}
	if m := msgOn("_ = declared != nil"); !strings.Contains(m, "comparison of function declared != nil is always true") {
		t.Errorf(`bad message for "declared != nil": %q`, m)
	}
	if m := msgOn("_ = fmt.Println == nil"); !strings.Contains(m, "comparison of function Println == nil is always false") {
		t.Errorf(`bad message for "fmt.Println == nil": %q`, m)
	}
	if m := msgOn("_ = r.method == nil"); !strings.Contains(m, "comparison of function method == nil is always false") {
		t.Errorf(`bad message for "r.method == nil": %q`, m)
	}
	if m := msgOn("_ = generic[int] == nil"); !strings.Contains(m, "comparison of function generic == nil is always false") {
		t.Errorf(`bad message for "generic[int] == nil": %q`, m)
	}
}
