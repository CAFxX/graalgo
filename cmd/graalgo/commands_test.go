package main

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// TestVetCheckArgs: the vet frontend gate is a single --check invocation
// carrying every file (arg-building unit test).
func TestVetCheckArgs(t *testing.T) {
	// Multiple files: --check first, then every file exactly once, in order.
	files := []string{"a.go", "b.go", "c_test.go"}
	if got, want := vetCheckArgs(files), []string{"--check", "a.go", "b.go", "c_test.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("vetCheckArgs(%v) = %v, want %v", files, got, want)
	}
	// Single file goes through the same single-invocation path.
	if got, want := vetCheckArgs([]string{"only.go"}), []string{"--check", "only.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("vetCheckArgs single = %v, want %v", got, want)
	}
	// The result must not alias the input's backing array: mutating the
	// returned args must leave the caller's slice untouched.
	in := []string{"x.go"}
	out := vetCheckArgs(in)
	out[1] = "MUTATED"
	if in[0] != "x.go" {
		t.Errorf("vetCheckArgs aliased the input slice: %v", in)
	}
}

// stubGraalgoRun replaces the graalgoRun variable with a recorder for the
// duration of a test, restoring the original afterwards.
func stubGraalgoRun(t *testing.T, exitCode int) *[][]string {
	t.Helper()
	old := graalgoRun
	var calls [][]string
	graalgoRun = func(root string, args []string) int {
		cp := append([]string(nil), args...)
		calls = append(calls, cp)
		return exitCode
	}
	t.Cleanup(func() { graalgoRun = old })
	return &calls
}

// writeVetFixture creates a small multi-file package: two non-test files
// plus a _test.go. b.go has a printf verb/arg mismatch that the vet printf
// analyzer flags, so a passing gate must still yield diagnostics.
func writeVetFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "a.go", `package p

import "fmt"

func Greet() string { return fmt.Sprintf("hi %s", "x") }
`)
	writeFile(t, dir, "b.go", `package p

import "fmt"

func Bad() { fmt.Printf("%d\n", "not a number") }
`)
	writeFile(t, dir, "c_test.go", `package p

import "testing"

func TestNothing(t *testing.T) {}
`)
	return dir
}

// TestCmdVetSingleCheckInvocation: cmdVet must run the frontend gate as
// exactly ONE backend invocation carrying every file, then still run the
// analyzers (regression test for the old one-invocation-per-file loop).
func TestCmdVetSingleCheckInvocation(t *testing.T) {
	dir := writeVetFixture(t)
	calls := stubGraalgoRun(t, 0) // gate passes

	code := cmdVet("testroot", []string{dir})

	if len(*calls) != 1 {
		t.Fatalf("cmdVet made %d backend invocations, want exactly 1", len(*calls))
	}
	got := (*calls)[0]
	if len(got) == 0 || got[0] != "--check" {
		t.Fatalf("backend invocation args = %v, want --check first", got)
	}
	rest := append([]string(nil), got[1:]...)
	sort.Strings(rest)
	want := []string{
		filepath.Join(dir, "a.go"),
		filepath.Join(dir, "b.go"),
		filepath.Join(dir, "c_test.go"),
	}
	sort.Strings(want)
	if !reflect.DeepEqual(rest, want) {
		t.Errorf("backend invocation files = %v, want %v", rest, want)
	}
	// The stubbed gate passed, so vet.Run must have run: the printf
	// mismatch in b.go is a diagnostic, and any diagnostic makes cmdVet
	// exit 1.
	if code != 1 {
		t.Errorf("cmdVet exit = %d, want 1 (vet diagnostic expected from b.go)", code)
	}
}

// TestCmdVetGateFailureShortCircuits: a failing frontend gate exits 1
// without running the analyzers.
func TestCmdVetGateFailureShortCircuits(t *testing.T) {
	// Clean package (no vet diagnostics): exit 1 here can only come from
	// the gate, not from vet.Run.
	dir := t.TempDir()
	writeFile(t, dir, "ok.go", "package p\n\nfunc Ok() int { return 1 }\n")
	calls := stubGraalgoRun(t, 2) // gate reports a build failure

	if code := cmdVet("testroot", []string{dir}); code != 1 {
		t.Errorf("cmdVet exit = %d, want 1 on gate failure", code)
	}
	if len(*calls) != 1 {
		t.Errorf("gate invoked %d times, want exactly 1", len(*calls))
	}
}
