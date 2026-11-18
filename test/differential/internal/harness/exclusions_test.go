package harness

import (
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
