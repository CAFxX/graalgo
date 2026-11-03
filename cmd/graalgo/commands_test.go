package main

import (
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

// stubFrontendCheck replaces frontendCheck with a recording stub for the
// duration of the test and returns the recorded calls. The stub fn decides
// the exit code from the invocation's args.
func stubFrontendCheck(t *testing.T, fn func(args []string) int) *[][]string {
	t.Helper()
	calls := &[][]string{}
	old := frontendCheck
	frontendCheck = func(_ string, args []string) int {
		*calls = append(*calls, append([]string{}, args...))
		return fn(args)
	}
	t.Cleanup(func() { frontendCheck = old })
	return calls
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// fn's exit code plus everything fn printed. cmdVet prints diagnostics and
// the "no issues found" line to os.Stdout, so tests assert on that output.
func captureStdout(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	code := fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return code, string(out)
}

// TestCmdVetChecksMultiFilePackageTogether is the regression test for the
// review finding (REVIEW-2026-11-03-10, P1-3) that `graalgo vet` ran the
// frontend gate one file at a time: a file calling a symbol declared in
// another file of the same package failed its solo check ("undefined:
// ...") before the analyzers ever ran, while `graalgo build` passed the
// same package. vet must invoke the frontend gate exactly once, over all
// package files together. This test fails on the old per-file loop (it
// records two single-file calls instead of one combined call).
func TestCmdVetChecksMultiFilePackageTogether(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package p\n\nfunc A() string { return B() }\n")
	writeFile(t, dir, "b.go", "package p\n\nfunc B() string { return \"x\" }\n")
	a, b := dir+"/a.go", dir+"/b.go"

	calls := stubFrontendCheck(t, func(args []string) int { return 0 })

	code, out := captureStdout(t, func() int { return cmdVet("", []string{a, b}) })
	if code != 0 {
		t.Fatalf("cmdVet = %d, want 0", code)
	}
	if want := [][]string{{"--check", a, b}}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("frontendCheck calls = %v, want %v", *calls, want)
	}
	if !strings.Contains(out, "vet: no issues found") {
		t.Fatalf("cmdVet output = %q, want it to contain %q", out, "vet: no issues found")
	}
}

// TestCmdVetAnalyzerFiresAfterPassingGate is the negative case: a
// multi-file package whose combined frontend gate passes, but where a real
// analyzer fires (unreachable code in file A, which calls a symbol from
// file B). vet must exit non-zero with the analyzer's diagnostic, proving
// the combined gate does not swallow analyzer results. The call-shape
// assertion also pins the gate: a per-file loop would record two calls.
func TestCmdVetAnalyzerFiresAfterPassingGate(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package p\n\nfunc A() int {\n\treturn B()\n\tx := 1\n\t_ = x\n\treturn 0\n}\n")
	writeFile(t, dir, "b.go", "package p\n\nfunc B() int { return 7 }\n")
	a, b := dir+"/a.go", dir+"/b.go"

	calls := stubFrontendCheck(t, func(args []string) int { return 0 })

	code, out := captureStdout(t, func() int { return cmdVet("", []string{a, b}) })
	if code == 0 {
		t.Fatalf("cmdVet = 0, want non-zero: unreachable-code analyzer should fire")
	}
	if want := [][]string{{"--check", a, b}}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("frontendCheck calls = %v, want %v", *calls, want)
	}
	if !strings.Contains(out, "unreachable code") {
		t.Fatalf("cmdVet output = %q, want it to contain %q", out, "unreachable code")
	}
}

// TestCmdVetGateFailureBlocksAnalyzers verifies the gate still gates: when
// the combined frontend check fails, vet exits non-zero without running
// the analyzers (no "no issues found" line, no analyzer diagnostics) and
// without re-running the check per file. The stub fails only combined
// invocations, so a per-file loop would pass the gate and reach the
// analyzers — this test fails on the old loop.
func TestCmdVetGateFailureBlocksAnalyzers(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package p\n\nfunc A() string { return B() }\n")
	writeFile(t, dir, "b.go", "package p\n\nfunc B() string { return \"x\" }\n")
	a, b := dir+"/a.go", dir+"/b.go"

	calls := stubFrontendCheck(t, func(args []string) int {
		if len(args) > 2 {
			return 1 // combined check fails
		}
		return 0 // a per-file check would pass
	})

	code, out := captureStdout(t, func() int { return cmdVet("", []string{a, b}) })
	if code == 0 {
		t.Fatalf("cmdVet = 0, want non-zero despite passing per-file checks")
	}
	if want := [][]string{{"--check", a, b}}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("frontendCheck calls = %v, want %v", *calls, want)
	}
	if strings.Contains(out, "vet: no issues found") {
		t.Fatalf("cmdVet output = %q, want no %q after a gate failure", out, "vet: no issues found")
	}
}

// TestCmdVetSingleFileInputChecksOnce pins the single-file shape: with one
// input file the combined gate IS the per-file check, so vet makes exactly
// one frontend invocation and reports "no issues found" on success.
func TestCmdVetSingleFileInputChecksOnce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package p\n\nfunc A() string { return \"x\" }\n")
	a := dir + "/a.go"

	calls := stubFrontendCheck(t, func(args []string) int { return 0 })

	code, out := captureStdout(t, func() int { return cmdVet("", []string{a}) })
	if code != 0 {
		t.Fatalf("cmdVet = %d, want 0", code)
	}
	if want := [][]string{{"--check", a}}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("frontendCheck calls = %v, want %v", *calls, want)
	}
	if !strings.Contains(out, "vet: no issues found") {
		t.Fatalf("cmdVet output = %q, want it to contain %q", out, "vet: no issues found")
	}
}

// TestCmdVetSingleFileGateFailure verifies a single-file gate failure exits
// non-zero with exactly one frontend invocation (no redundant re-check).
func TestCmdVetSingleFileGateFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package p\n\nfunc A() string { return \"x\" }\n")
	a := dir + "/a.go"

	calls := stubFrontendCheck(t, func(args []string) int { return 1 })

	code, _ := captureStdout(t, func() int { return cmdVet("", []string{a}) })
	if code == 0 {
		t.Fatalf("cmdVet = 0, want non-zero")
	}
	if want := [][]string{{"--check", a}}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("frontendCheck calls = %v, want %v", *calls, want)
	}
}
