package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lazyFixture writes a corpus of three programs, a config with a reference
// backend (prints the source) and a LazyBuild backend whose build step
// leaves a "built" marker, and records the reference snapshot. The lazy
// backend prints the source too, except: in a dir with "flaky", "wrong"
// until the build step ran; in a dir with "broken", "wrong" always.
func lazyFixture(t *testing.T, programs map[string][]string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	for name, marks := range programs {
		pd := filepath.Join(dir, "corpus", name)
		if err := os.MkdirAll(pd, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pd, "main.go"), []byte("package main // "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, m := range marks {
			if err := os.WriteFile(filepath.Join(pd, m), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	run := `d=$(dirname {SRC}); if [ -e $d/broken ] || { [ -e $d/flaky ] && [ ! -e $d/built ]; }; then echo wrong; else cat {SRC}; fi`
	cfg := map[string]any{
		"timeoutSec": 30,
		"backends": []map[string]any{
			{"name": "ref", "build": []string{"true"}, "run": []string{"cat", "{SRC}"}},
			{"name": "lazy", "lazyBuild": true,
				"build": []string{"sh", "-c", "touch $(dirname {SRC})/built"},
				"run":   []string{"sh", "-c", run}},
		},
	}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "cfg.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdSnapshotRecord([]string{"-config", filepath.Join(dir, "cfg.json"), "-corpus", filepath.Join(dir, "corpus"),
		"-backend", "ref", "-out", filepath.Join(dir, "snap.json"), "-toolchain", "t", "-runs", "1"}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func lazyCheck(dir string) error {
	return cmdSnapshotCheck([]string{"-config", filepath.Join(dir, "cfg.json"), "-corpus", filepath.Join(dir, "corpus"),
		"-backend", "lazy", "-snapshot", filepath.Join(dir, "snap.json"), "-jobs", "2"})
}

func built(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, "corpus", name, "built"))
	return err == nil
}

// A matching program never pays for the build step; a diverging one gets
// the full build+run, whose verdict stands.
func TestLazyBuildSkipsBuildOnlyWhenMatching(t *testing.T) {
	dir := lazyFixture(t, map[string][]string{"a": nil, "b": nil})
	if err := lazyCheck(dir); err != nil {
		t.Fatalf("all programs match: %v", err)
	}
	if built(dir, "a") || built(dir, "b") {
		t.Fatal("the build step ran for a matching program")
	}
	dir = lazyFixture(t, map[string][]string{"a": nil, "c": {"broken"}})
	err := lazyCheck(dir)
	if err == nil || !strings.Contains(err.Error(), "DRIFT") && !strings.Contains(err.Error(), "drift") {
		t.Fatalf("a diverging program must fail as DRIFT, got %v", err)
	}
	if !built(dir, "c") || built(dir, "a") {
		t.Fatal("the diverging program must be re-run with its build step, the matching one not")
	}
}

// Diverging without the build step but matching with it is a flake, and
// the retry budget (0) fails it: never a silent pass.
func TestLazyBuildFlakeFailsTheRetryBudget(t *testing.T) {
	dir := lazyFixture(t, map[string][]string{"a": nil, "f": {"flaky"}})
	err := lazyCheck(dir)
	if err == nil || !strings.Contains(err.Error(), "retry") {
		t.Fatalf("want a retry-budget failure, got %v", err)
	}
}

// validate compares the reference and the alternate only: a third
// configured backend (backends-p3.json's graalgo) must never run.
func TestValidateRunsOnlyTwoBackends(t *testing.T) {
	dir := t.TempDir()
	pd := filepath.Join(dir, "corpus", "p")
	if err := os.MkdirAll(pd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pd, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "third-ran")
	cfg := map[string]any{
		"timeoutSec": 30,
		"backends": []map[string]any{
			{"name": "a", "build": []string{"true"}, "run": []string{"cat", "{SRC}"}},
			{"name": "b", "build": []string{"true"}, "run": []string{"cat", "{SRC}"}},
			{"name": "c", "build": []string{"touch", marker}, "run": []string{"touch", marker}},
		},
	}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "cfg.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mut.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// no mutations: validate fails (nothing proven), but runs the corpus
	_ = cmdValidate([]string{"-config", filepath.Join(dir, "cfg.json"), "-corpus", filepath.Join(dir, "corpus"),
		"-mutations", filepath.Join(dir, "mut.txt"), "-workdir", filepath.Join(dir, "w"),
		"-out", filepath.Join(dir, "v.json"), "-jobs", "2"})
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("validate ran the third backend")
	}
	if _, err := os.Stat(filepath.Join(dir, "v.json")); err != nil {
		t.Fatalf("validate did not run the corpus: %v", err)
	}
}
