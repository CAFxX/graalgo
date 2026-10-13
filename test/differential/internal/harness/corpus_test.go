package harness

import (
	"path/filepath"
	"testing"
)

// A relative corpus root must still yield absolute program paths: backend
// run steps execute in a fresh empty working directory, so a relative
// {SRC} (e.g. "corpus/01_hello/main.go") is unresolvable there and the
// backend fails with "cannot read input file" — a harness artifact, not a
// real divergence. (M7: found via 17/46 graalgo corpus divergences.)
func TestDiscoverCorpus_RelativeRootYieldsAbsolutePaths(t *testing.T) {
	progs, err := DiscoverCorpus("../../corpus")
	if err != nil {
		t.Fatal(err)
	}
	if len(progs) == 0 {
		t.Fatal("no programs discovered")
	}
	for _, p := range progs {
		if !filepath.IsAbs(p.Main) {
			t.Errorf("program %s: Main %q is not absolute", p.Name, p.Main)
		}
		if !filepath.IsAbs(p.Dir) {
			t.Errorf("program %s: Dir %q is not absolute", p.Name, p.Dir)
		}
	}
}
