package srctest

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeYAML stages an exclusion YAML file under dir and returns its path.
func writeYAML(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "exclusions.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const runnableProg = "package main\n\nfunc main() {}\n"

func TestVerifyExclusionsClean(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "hello.go", runnableProg)
	// hello.go is "run" naturally, so a YAML entry is a genuine override.
	yp := writeYAML(t, dir, `version: 1
exclusions:
- file: hello.go
  reason: deferred-parity
  detail: triaged divergence, needs scheduler
`)
	problems, err := VerifyExclusions(dir, yp)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("want no problems, got %v", problems)
	}
}

func TestVerifyExclusionsStale(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "hello.go", runnableProg)
	yp := writeYAML(t, dir, `version: 1
exclusions:
- file: gone.go
  reason: backend-specific
  detail: file was renamed upstream
`)
	problems, err := VerifyExclusions(dir, yp)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "gone.go") || !strings.Contains(problems[0], "stale") {
		t.Fatalf("want one stale-entry problem for gone.go, got %v", problems)
	}
}

func TestVerifyExclusionsBadReason(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "hello.go", runnableProg)
	yp := writeYAML(t, dir, `version: 1
exclusions:
- file: hello.go
  reason: not-a-real-code
  detail: bogus reason
`)
	problems, err := VerifyExclusions(dir, yp)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "invalid reason code") {
		t.Fatalf("want one invalid-reason problem, got %v", problems)
	}
}

func TestVerifyExclusionsEmptyDetail(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "hello.go", runnableProg)
	yp := writeYAML(t, dir, `version: 1
exclusions:
- file: hello.go
  reason: deferred-parity
`)
	problems, err := VerifyExclusions(dir, yp)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "empty exclusion detail") {
		t.Fatalf("want one empty-detail problem, got %v", problems)
	}
}

func TestVerifyExclusionsShadowed(t *testing.T) {
	dir := t.TempDir()
	// lib.go is package foo: the scanner already excludes it structurally,
	// so a YAML entry for it is redundant dead weight.
	writeFile(t, dir, "lib.go", "package foo\n\nfunc Foo() {}\n")
	yp := writeYAML(t, dir, `version: 1
exclusions:
- file: lib.go
  reason: backend-specific
  detail: not really needed, scanner excludes it anyway
`)
	problems, err := VerifyExclusions(dir, yp)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "shadowed") {
		t.Fatalf("want one shadowed-entry problem, got %v", problems)
	}
}

func TestVerifyExclusionsMissingVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "hello.go", runnableProg)
	yp := writeYAML(t, dir, `exclusions:
- file: hello.go
  reason: deferred-parity
  detail: no version line
`)
	if _, err := VerifyExclusions(dir, yp); err == nil {
		t.Fatal("want an error for a version-less exclusions file")
	}
}

func TestVerifyExclusionsRealList(t *testing.T) {
	// The committed srctest-exclusions.yaml must verify clean against the
	// real GOROOT/test tree when one is available; without it the test
	// skips (the VM's toolchain is stripped of test/).
	goroot := os.Getenv("DH_GOROOT_TEST")
	if goroot == "" {
		t.Skip("no GOROOT/test available (set DH_GOROOT_TEST)")
	}
	_, thisFile, _, _ := runtime.Caller(0)
	yp := filepath.Join(filepath.Dir(thisFile), "..", "..", "srctest-exclusions.yaml")
	problems, err := VerifyExclusions(goroot, yp)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Errorf("exclusion problem: %s", p)
	}
}
