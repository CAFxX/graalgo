package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runVetOnNamedFile runs vet on a single source file with an explicit name
// (needed for the tests analyzer, which only inspects *_test.go files).
func runVetOnNamedFile(t *testing.T, name, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
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
	return strings.Join(msgs, "\n")
}

// RV18-P1-21(2): runTests flagged a correct func TestMain(m *testing.M) as
// "Test functions must take *testing.T", and the TestMain check never verified
// the parameter is actually *testing.M.
func TestTestsAnalyzerTestMain(t *testing.T) {
	good := `package p

import "testing"

func TestMain(m *testing.M) {
	m.Run()
}

func TestFoo(t *testing.T) {
	_ = t
}
`
	joined := runVetOnNamedFile(t, "good_test.go", good)
	if strings.Contains(joined, "Test functions must take") {
		t.Errorf("false positive on correct TestMain/TestFoo:\n%s", joined)
	}
	if strings.Contains(joined, "TestMain") {
		t.Errorf("false positive mentioning TestMain on correct code:\n%s", joined)
	}

	// TestMain with the wrong parameter type must be flagged.
	wrongType := `package p

import "testing"

func TestMain(t *testing.T) {
	_ = t
}
`
	joined = runVetOnNamedFile(t, "wrongtype_test.go", wrongType)
	if !strings.Contains(joined, "TestMain must take *testing.M") {
		t.Errorf("expected TestMain *testing.M diagnostic, got:\n%s", joined)
	}

	// TestMain with the wrong arity must still be flagged.
	noParam := `package p

import "testing"

func TestMain() {
}
`
	joined = runVetOnNamedFile(t, "noparam_test.go", noParam)
	if !strings.Contains(joined, "TestMain must take one *testing.M parameter") {
		t.Errorf("expected TestMain arity diagnostic, got:\n%s", joined)
	}
}
