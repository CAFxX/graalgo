package vet

import (
	"strings"
	"testing"
)

// runVetOnTestSrc writes src to a _test.go file and runs vet on it. The
// tests analyzer only inspects _test.go files, so a fixture named
// test.go (as runVetOnSrc writes) never exercises it (C4).
func runVetOnTestSrc(t *testing.T, src string) string {
	t.Helper()
	return runVetOnSrcAs(t, src, "x_test.go")
}

func TestTestsAnalyzerGood(t *testing.T) {
	// A correct TestXxx and a correct TestMain must produce no
	// test-related diagnostics.
	src := `package p

import "testing"

func TestGood(t *testing.T) {
	t.Log("ok")
}

func TestMain(m *testing.M) {
}
`
	joined := runVetOnTestSrc(t, src)
	for _, bad := range []string{
		"Test functions must take",
		"TestMain must",
		"could not import",
	} {
		if strings.Contains(joined, bad) {
			t.Errorf("false positive on correct test file, found %q in:\n%s", bad, joined)
		}
	}
}

func TestTestsAnalyzerBadSignatures(t *testing.T) {
	// Wrong TestXxx signatures must be flagged.
	src := `package p

import "testing"

func TestBadParam(x int) {}

func TestNoParams() {}
`
	joined := runVetOnTestSrc(t, src)
	if !strings.Contains(joined, "Test functions must take *testing.T") {
		t.Errorf("expected *testing.T diagnostic for TestBadParam, got:\n%s", joined)
	}
	if !strings.Contains(joined, "Test functions must take one *testing.T parameter") {
		t.Errorf("expected one-parameter diagnostic for TestNoParams, got:\n%s", joined)
	}
}

func TestTestsAnalyzerTestMainExcluded(t *testing.T) {
	// A correct TestMain must not be swept up by the Test* loop: it
	// takes *testing.M, not *testing.T (C3a).
	src := `package p

import "testing"

func TestMain(m *testing.M) {
}
`
	joined := runVetOnTestSrc(t, src)
	if strings.Contains(joined, "Test functions must take") {
		t.Errorf("TestMain wrongly flagged by the Test* loop:\n%s", joined)
	}
}

func TestTestsAnalyzerBadTestMain(t *testing.T) {
	// TestMain with a genuinely wrong signature must be flagged (C3b):
	// no parameter, wrong parameter type, and result values.
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"no params", "func TestMain() {}\n",
			"TestMain must take one *testing.M parameter"},
		{"wrong param type", "func TestMain(m *testing.T) {}\n",
			"TestMain must take one *testing.M parameter"},
		{"results", "func TestMain(m *testing.M) int { return 0 }\n",
			"TestMain must not return results"},
	}
	for _, c := range cases {
		src := "package p\n\nimport \"testing\"\n\n" + c.src
		if joined := runVetOnTestSrc(t, src); !strings.Contains(joined, c.want) {
			t.Errorf("%s: expected %q, got:\n%s", c.name, c.want, joined)
		}
	}
}

func TestTestsAnalyzerUnresolvedImport(t *testing.T) {
	// When the import backing a test parameter type cannot be resolved,
	// the failure is reported (C2/QN-13) and the unresolvable type is
	// treated as unknown rather than as a signature violation (C3).
	src := `package p

import missing "example.com/does-not-exist"

func TestFoo(t *missing.T) {}
`
	joined := runVetOnTestSrc(t, src)
	if !strings.Contains(joined, `could not import "example.com/does-not-exist"`) {
		t.Errorf("expected could-not-import diagnostic, got:\n%s", joined)
	}
	if strings.Contains(joined, "Test functions must take") {
		t.Errorf("unresolvable parameter type wrongly flagged as a signature violation:\n%s", joined)
	}
}

func TestSourceImporterResolvesStdlib(t *testing.T) {
	// With imports resolved from source, import-dependent analyzers see
	// real type information: copylocks must fire on a sync.Mutex passed
	// by value (C2). Before the fix this was silently a no-op.
	src := `package p

import "sync"

func takeLock(m sync.Mutex) { _ = m }

func mk() {
	var mu sync.Mutex
	takeLock(mu)
}
`
	joined := runVetOnSrc(t, src)
	if strings.Contains(joined, "could not import") {
		t.Errorf("stdlib import should resolve, got:\n%s", joined)
	}
	if !strings.Contains(joined, "call copies lock value") {
		t.Errorf("expected copylocks diagnostic with resolved imports, got:\n%s", joined)
	}
}

func TestBuildTagPlacement(t *testing.T) {
	// A // +build line after the package clause is silently ignored as
	// a build constraint, so it must be reported (C5).
	bad := `package p

// +build linux

var _ = 1
`
	if joined := runVetOnSrc(t, bad); !strings.Contains(joined, "misplaced // +build") {
		t.Errorf("expected misplaced-buildtag diagnostic, got:\n%s", joined)
	}
	// A // +build line before the package clause is fine.
	good := `// +build linux

package p

var _ = 1
`
	if joined := runVetOnSrc(t, good); strings.Contains(joined, "misplaced // +build") {
		t.Errorf("false positive on correctly placed // +build:\n%s", joined)
	}
}
