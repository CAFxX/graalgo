package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// G5: only a divergence verdict kills a mutant; a harness error that also
// exits 1 (snapshot/corpus inconsistency, bad flags) must not.
func TestReportsDivergence(t *testing.T) {
	for out, want := range map[string]bool{
		"DRIFT     50_shifts   stdout differs at line 3\ncheck: ...\n":                            true,
		"STALE     58_x  listed but matches gc\n":                                                 true,
		"FAIL test/m1a/programs/x.go: stdout differs\nm1a: 0 passed, 1 failed of 1\n":             true,
		"diffharness snapshot: error: snapshot/corpus inconsistency:\n  a: not in the snapshot\n": false,
		"flag provided but not defined: -bogus\n":                                                 false,
		"INFRA: cannot build graalgo-tool\n":                                                      false,
		"":                                                                                        false,
	} {
		if got := reportsDivergence(out); got != want {
			t.Errorf("reportsDivergence(%q) = %v, want %v", out, got, want)
		}
	}
}

// The spec loader rejects what would make a mutant unscorable.
func TestLoadRejects(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"no kill":   `{"mutants":[{"id":"A","class":"c","file":"f","old":"x","new":"y"}]}`,
		"bad gate":  `{"mutants":[{"id":"A","class":"c","file":"f","old":"x","new":"y","kill":["m9:p.go"]}]}`,
		"no prog":   `{"mutants":[{"id":"A","class":"c","file":"f","old":"x","new":"y","kill":["m1a:"]}]}`,
		"dup id":    `{"mutants":[{"id":"A","class":"c","file":"f","old":"x","new":"y","kill":["m1a:p"]},{"id":"A","class":"c","file":"f","old":"x","new":"y","kill":["m1a:p"]}]}`,
		"no change": `{"mutants":[{"id":"A","class":"c","file":"f","old":"x","new":"x","kill":["m1a:p"]}]}`,
	} {
		p := filepath.Join(dir, "m.json")
		os.WriteFile(p, []byte(body), 0o644)
		if _, err := load(p); err == nil {
			t.Errorf("%s: load accepted %s", name, body)
		}
	}
}

// The committed mutant set stays applicable between nightly runs (make
// check-repo runs this): every anchor occurs exactly once in its file and
// every kill program exists, so a refactor that rots a mutant fails per
// push instead of the nightly job.
func TestMutantSetApplies(t *testing.T) {
	root, _ := filepath.Abs("../..")
	ms, err := load(filepath.Join(root, "tools/mutation/mutants.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) < 20 {
		t.Fatalf("%d mutants: the new-path set (m3-plan G6) has at least 20", len(ms))
	}
	for _, m := range ms {
		src, err := os.ReadFile(filepath.Join(root, m.File))
		if err != nil {
			t.Errorf("%s: %v", m.ID, err)
			continue
		}
		if n := strings.Count(string(src), m.Old); n != 1 {
			t.Errorf("%s: 'old' occurs %d times in %s (must be exactly 1)", m.ID, n, m.File)
		}
		for g, progs := range slices(m.Kill) {
			for _, p := range progs {
				path := filepath.Join(root, p)
				if g == "snapshot" {
					path = filepath.Join(root, "test/differential/corpus", p)
				}
				if _, err := os.Stat(path); err != nil {
					t.Errorf("%s: kill program %s:%s: %v", m.ID, g, p, err)
				}
			}
		}
	}
}
