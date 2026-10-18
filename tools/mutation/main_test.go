package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRoot walks up from this test file to the repo root (tools/mutation/ -> root).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func TestNewLayoutPaths(t *testing.T) {
	repo := repoRoot(t)
	l, err := newLayout(repo)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"truffle":    filepath.Join(repo, "internal", "backend", "truffle"),
		"runtime":    filepath.Join(repo, "internal", "backend", "runtime"),
		"frontend":   filepath.Join(repo, "internal", "frontend"),
		"gofrontend": filepath.Join(repo, "internal", "frontend", "gofrontend"),
	}
	got := map[string]string{
		"truffle": l.truffle, "runtime": l.runtime, "frontend": l.frontend,
		"gofrontend": l.gofrontend,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
	// The no-debris rule: the persist dir must never be under hidden_files/
	// or the deleted phases/ tree.
	for _, bad := range []string{"hidden_files", "phases"} {
		if strings.Contains(l.persistDir, bad) {
			t.Errorf("persistDir %q resurrects %q", l.persistDir, bad)
		}
	}
	// GRAALGO_MUTATION_REPORTS overrides the persist dir.
	t.Setenv("GRAALGO_MUTATION_REPORTS", filepath.Join(t.TempDir(), "reports"))
	l2, err := newLayout(repo)
	if err != nil {
		t.Fatal(err)
	}
	if l2.persistDir != os.Getenv("GRAALGO_MUTATION_REPORTS") {
		t.Errorf("persistDir = %q, want env override %q",
			l2.persistDir, os.Getenv("GRAALGO_MUTATION_REPORTS"))
	}
}

// TestMutationReportsDirDefault pins the out-of-repo default: per-run
// evidence must not land in the repo tree, and the phases/ location must
// never come back.
func TestMutationReportsDirDefault(t *testing.T) {
	t.Setenv("GRAALGO_MUTATION_REPORTS", "")
	d := mutationReportsDir()
	for _, bad := range []string{"hidden_files", "phases"} {
		if strings.Contains(d, bad) {
			t.Errorf("default persist dir %q contains %q", d, bad)
		}
	}
	if filepath.Base(d) != "graalgo-mutation-reports" {
		t.Errorf("default persist dir %q has unexpected name", d)
	}
}

func TestTestClasspathOrder(t *testing.T) {
	// /tmp/x is not a real repo; only the relative order matters here.
	l := layout{
		repo:    "/tmp/x",
		truffle: "/tmp/x/internal/backend/truffle",
		runtime: "/tmp/x/internal/backend/runtime",
		jars:    []string{"/tmp/x/internal/backend/truffle/lib/a.jar"},
	}
	cp := l.testClasspath("/tmp/x/ov")
	parts := strings.Split(cp, ":")
	want := []string{
		"/tmp/x/ov",
		"/tmp/x/internal/backend/truffle/out/test-classes",
		"/tmp/x/internal/backend/runtime/out/classes",
		"/tmp/x/internal/backend/truffle/out/classes",
		"/tmp/x/internal/backend/truffle/lib/a.jar",
	}
	if len(parts) != len(want) {
		t.Fatalf("classpath = %q, want %d parts", cp, len(want))
	}
	for i := range want {
		if parts[i] != want[i] {
			t.Errorf("classpath part %d = %q, want %q (full: %q)", i, parts[i], want[i], cp)
		}
	}
	// No overlay: must not start with a stray separator.
	if strings.HasPrefix(l.testClasspath(""), ":") {
		t.Errorf("empty overlay produced leading separator: %q", l.testClasspath(""))
	}
}

func TestOverlayClasspath(t *testing.T) {
	l := layout{
		truffle: "/tmp/x/internal/backend/truffle",
		runtime: "/tmp/x/internal/backend/runtime",
		jars:    []string{"/tmp/x/internal/backend/truffle/lib/a.jar"},
	}
	cp := l.overlayClasspath()
	// Joint-round classes first (they hold every non-stub class).
	if !strings.HasPrefix(cp, "/tmp/x/internal/backend/runtime/out/classes:") {
		t.Errorf("overlay classpath must start with runtime/out/classes: %q", cp)
	}
	if strings.Contains(cp, "phases") {
		t.Errorf("overlay classpath references the deleted phases/ tree: %q", cp)
	}
}

func TestSetEnvVar(t *testing.T) {
	env := []string{"A=1", "B=2"}
	env = setEnvVar(env, "B", "3")
	env = setEnvVar(env, "C", "4")
	want := []string{"A=1", "B=3", "C=4"}
	if len(env) != len(want) {
		t.Fatalf("env = %q", env)
	}
	for i := range want {
		if env[i] != want[i] {
			t.Errorf("env[%d] = %q, want %q", i, env[i], want[i])
		}
	}
}

// TestMutantsJsonCurated keeps the curated mutant set honest: every mutant
// must name a file that exists in the current tree and an old-string that is
// still present verbatim. A source move that orphans a mutant fails here,
// not as a mysterious ERROR at 3am.
func TestMutantsJsonCurated(t *testing.T) {
	repo := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(repo, "tools", "mutation", "mutants.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Mutants []Mutant `json:"mutants"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Mutants) == 0 {
		t.Fatal("mutants.json has no mutants")
	}
	seen := map[string]bool{}
	for _, m := range spec.Mutants {
		if m.ID == "" || m.Description == "" || m.Old == "" || m.New == "" {
			t.Errorf("mutant %+v has empty fields", m)
		}
		if seen[m.ID] {
			t.Errorf("duplicate mutant id %q", m.ID)
		}
		seen[m.ID] = true
		if m.Lang != "java" && m.Lang != "go" {
			t.Errorf("mutant %s: unknown lang %q", m.ID, m.Lang)
		}
		if strings.Contains(m.File, "phases/") {
			t.Errorf("mutant %s: file %q references the deleted phases/ tree", m.ID, m.File)
		}
		src, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(m.File)))
		if err != nil {
			t.Errorf("mutant %s: cannot read %q: %v", m.ID, m.File, err)
			continue
		}
		if !strings.Contains(string(src), m.Old) {
			t.Errorf("mutant %s: old string no longer present in %q", m.ID, m.File)
		}
	}
}
