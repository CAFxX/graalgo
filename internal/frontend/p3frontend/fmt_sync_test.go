package p3frontend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFmtFprintlnSyncPool pins the fmt/sync retirement
// (REVIEW-2026-11-05-11): a program using fmt.Fprintln, fmt.Fscanf,
// sync.Pool and sync.Map must type-check through MergeDir against the
// REAL stdlib packages from GOROOT. Previously it failed two ways: the
// fmt stub declared only the 7 pure functions (no F-family, no Scan*),
// and the stdlib scope error refused the real packages outright
// ("could not import os ... P3-M7+ waves").
func TestFmtFprintlnSyncPool(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import (
	"fmt"
	"os"
	"sync"
)

func main() {
	var n int
	fmt.Fprintln(os.Stdout, "hello")
	fmt.Fscanf(os.Stdin, "%d", &n)
	p := &sync.Pool{New: func() any { return 0 }}
	_ = p.Get()
	var m sync.Map
	m.Store("k", 1)
	_, _ = m.Load("k")
	fmt.Println(n)
}
`
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	merged, err := MergeDir(dir)
	if err != nil {
		t.Fatalf("MergeDir: %v", err)
	}
	for _, want := range []string{`"fmt"`, `"sync"`, "Fprintln", "Fscanf", "sync.Pool", "sync.Map"} {
		if !strings.Contains(merged, want) {
			t.Errorf("merged source lacks %q", want)
		}
	}
}
