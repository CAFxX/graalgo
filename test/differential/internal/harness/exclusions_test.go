package harness

import (
	"os"
	"strings"
	"testing"
)

func TestParseGroupsAndSignatures(t *testing.T) {
	_, ex, err := ParseReviewedExclusions("t", `version: 1
exclusions:
- file: a.go
  reason: deferred-parity
  detail: BUG-0001: x
  signature: 0123456789abcdef
groups:
- group: v1-multi-file
  reason: deferred-parity
  expires: M3
  detail: frontend/v1 is single-file
  files: b.go c.go
  files: d.go
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(ex) != 4 {
		t.Fatalf("want 4 entries, got %d: %v", len(ex), ex)
	}
	if ex["a.go"].Signature != "0123456789abcdef" {
		t.Errorf("signature not parsed: %+v", ex["a.go"])
	}
	d := ex["d.go"]
	if d.Group != "v1-multi-file" || !strings.Contains(d.Detail, "expires M3") || d.Reason != "deferred-parity" {
		t.Errorf("group expansion wrong: %+v", d)
	}
}

func TestParseGroupErrors(t *testing.T) {
	for name, body := range map[string]string{
		"no expires":   "version: 1\ngroups:\n- group: g\n  reason: deferred-parity\n  detail: d\n  files: a.go\n",
		"no files":     "version: 1\ngroups:\n- group: g\n  reason: deferred-parity\n  detail: d\n  expires: M3\n",
		"bad reason":   "version: 1\ngroups:\n- group: g\n  reason: whatever\n  detail: d\n  expires: M3\n  files: a.go\n",
		"dup in group": "version: 1\nexclusions:\n- file: a.go\n  reason: deferred-parity\ngroups:\n- group: g\n  reason: deferred-parity\n  detail: d\n  expires: M3\n  files: a.go\n",
	} {
		if _, _, err := ParseReviewedExclusions("t", body); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestDiffSignatureStableAndDistinct(t *testing.T) {
	a, b := DiffSignature("stdout differs at line 1"), DiffSignature("exitCode: want=0 got=2")
	if len(a) != 16 || a == b || a != DiffSignature("stdout differs at line 1") {
		t.Fatalf("signatures: %q %q", a, b)
	}
}

func TestRegisterProblems(t *testing.T) {
	bugs := map[string]bool{"BUG-0001": true}
	_, ex, err := ParseReviewedExclusions("t", `version: 1
reason_codes:
- code: CGO
  description: skipped by this parser
  class: deferred-parity
exclusions:
- file: ok.go
  reason: deferred-parity
  detail: BUG-0001: pinned
  signature: 0123456789abcdef
- file: marker.go
  reason: deferred-parity
  detail: BUG-0001: timings
  signature: unpinned (timings)
- file: nobug.go
  reason: deferred-parity
  detail: no id here
  signature: 0123456789abcdef
- file: unknownbug.go
  reason: unspecified-behavior
  detail: BUG-0999 cited but not registered
- file: nosig.go
  reason: deferred-parity
  detail: BUG-0001
- file: emptymarker.go
  reason: deferred-parity
  detail: BUG-0001
  signature: unpinned ()
- file: spec.go
  reason: unspecified-behavior
  detail: map order
groups:
- group: g
  reason: deferred-parity
  expires: M3
  detail: BUG-0001: group needs no signature
  files: g1.go
- group: h
  reason: deferred-parity
  expires: M3
  detail: group without a bug id
  files: h1.go
`)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(RegisterProblems("t", ex, bugs, true), "\n")
	for _, want := range []string{"nobug.go: deferred-parity entry cites no BUG id", "unknownbug.go: cites BUG-0999",
		"nosig.go: deferred-parity entry pins no signature", "emptymarker.go: deferred-parity entry pins no signature",
		"h1.go: deferred-parity entry cites no BUG id"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing problem %q in:\n%s", want, got)
		}
	}
	for _, clean := range []string{"ok.go", "marker.go", "spec.go", "g1.go"} {
		if strings.Contains(got, "t: "+clean+":") {
			t.Errorf("%s must be clean:\n%s", clean, got)
		}
	}
	// Unsigned registers (never run) need the bug id only.
	if s := strings.Join(RegisterProblems("t", ex, bugs, false), "\n"); strings.Contains(s, "nosig.go") {
		t.Errorf("unsigned register must not require a signature:\n%s", s)
	}
}

func TestIsPinned(t *testing.T) {
	if !IsPinned("0123456789abcdef") || IsPinned("unpinned (x)") || IsPinned("") || IsPinned("0123") {
		t.Fatal("IsPinned")
	}
}

func TestLoadBugIDs(t *testing.T) {
	p := t.TempDir() + "/bugs.md"
	if err := os.WriteFile(p, []byte("| Id | Bug |\n|---|---|\n| BUG-0001 | x |\nprose BUG-0002\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ids, err := LoadBugIDs(p)
	if err != nil || !ids["BUG-0001"] || ids["BUG-0002"] {
		t.Fatalf("ids %v err %v", ids, err)
	}
}
