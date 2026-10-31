package main

import (
	"reflect"
	"testing"
)

// TestCmdVetChecksMultiFilePackageTogether is the regression test for the
// review finding that `graalgo vet` ran the frontend check one file at a
// time: a file calling a symbol declared in another file of the same
// package failed its solo check ("undefined: ...") before the analyzers
// ever ran, while `graalgo run`/`build` passed the same package. vet must
// invoke the frontend gate exactly once, over all package files together.
// The stubbed frontendCheck makes this test fail on the old per-file loop
// (it would record two single-file calls instead of one combined call).
func TestCmdVetChecksMultiFilePackageTogether(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package p\n\nfunc A() string { return B() }\n")
	writeFile(t, dir, "b.go", "package p\n\nfunc B() string { return \"x\" }\n")
	a, b := dir+"/a.go", dir+"/b.go"

	var calls [][]string
	old := frontendCheck
	frontendCheck = func(root string, args []string) int {
		calls = append(calls, append([]string{}, args...))
		return 0
	}
	defer func() { frontendCheck = old }()

	if code := cmdVet("", []string{a, b}); code != 0 {
		t.Fatalf("cmdVet = %d, want 0", code)
	}
	want := [][]string{{"--check", a, b}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("frontendCheck calls = %v, want %v", calls, want)
	}
}

// TestCmdVetCombinedFailureFallsBackToPerFileDiagnostics verifies the
// fallback is diagnostic-only: when the combined check fails, vet re-runs
// the check per file for diagnostics, but still exits non-zero even if
// every per-file check passes (a per-file pass cannot rescue a combined
// failure).
func TestCmdVetCombinedFailureFallsBackToPerFileDiagnostics(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package p\n\nfunc A() string { return B() }\n")
	writeFile(t, dir, "b.go", "package p\n\nfunc B() string { return \"x\" }\n")
	a, b := dir+"/a.go", dir+"/b.go"

	var calls [][]string
	old := frontendCheck
	frontendCheck = func(root string, args []string) int {
		calls = append(calls, append([]string{}, args...))
		if len(args) > 2 {
			return 1 // combined check fails
		}
		return 0 // per-file diagnostics pass
	}
	defer func() { frontendCheck = old }()

	if code := cmdVet("", []string{a, b}); code == 0 {
		t.Fatalf("cmdVet = 0, want non-zero despite passing per-file diagnostics")
	}
	want := [][]string{{"--check", a, b}, {"--check", a}, {"--check", b}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("frontendCheck calls = %v, want %v", calls, want)
	}
}

// TestCmdVetSingleFileFailureHasNoFallback verifies a single-file package
// gets no redundant fallback: the combined check IS the per-file check,
// so re-running it would only repeat the same output.
func TestCmdVetSingleFileFailureHasNoFallback(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package p\n\nfunc A() string { return \"x\" }\n")
	a := dir + "/a.go"

	var calls [][]string
	old := frontendCheck
	frontendCheck = func(root string, args []string) int {
		calls = append(calls, append([]string{}, args...))
		return 1
	}
	defer func() { frontendCheck = old }()

	if code := cmdVet("", []string{a}); code == 0 {
		t.Fatalf("cmdVet = 0, want non-zero")
	}
	want := [][]string{{"--check", a}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("frontendCheck calls = %v, want %v", calls, want)
	}
}
