package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// vetCheckStub is the fake-backend script for cmdVet tests: it emulates
// the real --check gate by type-checking exactly the files it is given,
// as one package, with the gc toolchain. `go vet` on a file list
// type-checks the files together and fails on an undefined sibling
// declaration — the same shape as the frontend's conf.Check gate, which
// type-checks exactly the files given and reports a missing sibling
// declaration as a hard error. Every invocation is appended to the log
// file so the test can also assert how many --check runs happened and
// over which files. GOCACHE is pointed at a temp dir so the stub never
// writes to the user's home build cache.
func vetCheckStub(t *testing.T, logPath, cacheDir string) string {
	t.Helper()
	return fmt.Sprintf(`log=%q
echo "run: $*" >> "$log"
if [ "$1" = "--check" ]; then
  shift
  GOCACHE=%q %q vet "$@"
  exit $?
fi
exit 0
`, logPath, cacheDir, goBinary(t))
}

// Regression for R05-C-01: `graalgo vet` on a multi-file package must
// run the --check gate once over all files as one unit. The old code
// ran --check once per file, so a.go (which calls helper(), declared
// in b.go) failed the gate with "undefined: helper" and vet returned 1
// for a correct package.
func TestCmdVetMultiFilePackage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package vetpkg\n\nfunc Greet() string { return helper() }\n")
	writeFile(t, dir, "b.go", "package vetpkg\n\nfunc helper() string { return \"hi\" }\n")
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	root := fakeBackendRoot(t, vetCheckStub(t, logPath, t.TempDir()))
	var code int
	out := captureOutput(t, func() {
		code = cmdVet(root, []string{dir})
	})
	if code != 0 {
		t.Errorf("cmdVet exit = %d, want 0 (two-file package with a cross-file call must vet clean):\n%s", code, out)
	}
	mustContain(t, out, "vet: no issues found")
	invocations, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading backend invocation log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(invocations)), "\n")
	if len(lines) != 1 {
		t.Fatalf("--check ran %d times, want exactly 1 (one gate over all files as one unit):\n%s", len(lines), invocations)
	}
	if !strings.Contains(lines[0], "--check") || !strings.Contains(lines[0], "a.go") || !strings.Contains(lines[0], "b.go") {
		t.Errorf("--check invocation = %q, want one --check over both a.go and b.go", lines[0])
	}
}
