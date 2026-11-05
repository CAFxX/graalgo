package p3frontend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProbeFmtFprintlnSyncPool is the mechanism probe for the fmt/sync
// retirement: a program using fmt.Fprintln, fmt.Fscanf, sync.Pool and
// sync.Map must type-check through MergeDir against the REAL stdlib
// packages from GOROOT (previously: "could not import os ... P3-M7+
// waves" / undefined F-family symbols against the stub).
func TestProbeFmtFprintlnSyncPool(t *testing.T) {
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
