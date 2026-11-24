package typeparam

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"diffharness/internal/harness"
)

// scriptBackend is a fake backend whose "build" writes a /bin/sh script as
// the binary: no Go toolchain, so the timeout paths cost milliseconds.
func scriptBackend(name, body string) *harness.Backend {
	return &harness.Backend{
		Name:  name,
		Build: []string{"/bin/sh", "-c", `printf '#!/bin/sh\n%s\n' "$1" > "$0"; chmod +x "$0"`, "{BIN}", body},
		Run:   []string{"{BIN}"},
	}
}

func runPerFileOne(t *testing.T, ref, alt *harness.Backend) Comparison {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "p.go")
	if err := os.WriteFile(src, []byte("package main\nfunc main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// One 100ms unit per second of timeout: a "sleep 5" program times out.
	cfg := &harness.Config{TimeoutSec: 1, TimeoutUnit: 100 * time.Millisecond}
	files := []TFile{{Name: "p.go", Class: ClassRun, Srcs: []string{src}}}
	comps := runPerFile(cfg, ref, alt, files, []int{0}, t.TempDir(), 0)
	return comps[0]
}

const hang = "sleep 5"
const quick = "exit 0"

// The failure this guards (CI run 45373249999): gc timed out on a slow
// runner and the comparison reported `timeout: ref=true alt=false`, an
// unlisted DIVERGE. A reference timeout is INFRA: no verdict, no reasons
// that could match (or break) a pinned signature.
func TestRefTimeoutIsInfraNotDivergence(t *testing.T) {
	c := runPerFileOne(t, scriptBackend("gc", hang), scriptBackend("alt", quick))
	if !c.Infra || c.Same {
		t.Fatalf("ref timeout: want Infra, got %+v", c)
	}
	for _, r := range c.Reasons {
		if strings.Contains(r, "timeout: ref=") {
			t.Fatalf("ref timeout leaked into a divergence reason: %q", r)
		}
	}
}

func TestBothTimeoutIsInfra(t *testing.T) {
	c := runPerFileOne(t, scriptBackend("gc", hang), scriptBackend("alt", hang))
	if !c.Infra {
		t.Fatalf("both time out: want Infra, got %+v", c)
	}
}

// Only the alternate timing out stays a real divergence (a graalgo hang).
func TestAltTimeoutStaysDivergence(t *testing.T) {
	c := runPerFileOne(t, scriptBackend("gc", quick), scriptBackend("alt", hang))
	if c.Infra || c.Same {
		t.Fatalf("alt-only timeout: want a divergence, got %+v", c)
	}
	if len(c.Reasons) == 0 || !strings.Contains(c.Reasons[0], "timeout: ref=false alt=true") {
		t.Fatalf("reasons = %q", c.Reasons)
	}
}

// A reference that is merely slow (times out once, finishes on the 2x retry)
// is a match, and the alternate gets the same extended timeout.
func TestRefSlowButFinishesOnRetryMatches(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "first")
	slowOnce := `[ -e ` + marker + ` ] || { : > ` + marker + `; sleep 5; }; exit 0`
	c := runPerFileOne(t, scriptBackend("gc", slowOnce), scriptBackend("alt", quick))
	if c.Infra || !c.Same {
		t.Fatalf("slow reference: want a match, got %+v", c)
	}
}
