package main

import (
	"strings"
	"testing"
)

// Regression: *_test.go files contribute only `verifies:` annotations to
// the scan. Fixture code inside test sources deliberately mentions
// fake/invalid IDs (GG-0099, GG-42, …); those must not become refs or
// format problems, or `req lint` over the repo can never be clean while
// the linter's own test suite exists.
// verifies: GG-0001
func TestScan_TestFilesOnlyVerifiesScanned(t *testing.T) {
	_, c := scanCtx(t, map[string]string{
		"code.go":      "package x\n\n// GG-0001: real ref\nvar _ = 1\n",
		"code_test.go": "package x\n\n// GG-0099: fixture ref, must be ignored\n// GG-42 sloppy, must be ignored\n// verifies: GG-0001\n",
	})
	refs, fmts, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(fmts) != 0 {
		t.Fatalf("test-file fixtures must not raise format problems: %v", fmts)
	}
	byID := refsByID(refs)
	if _, ok := byID["GG-0099"]; ok {
		t.Fatalf("fixture ref GG-0099 in _test.go was scanned: %v", byID["GG-0099"])
	}
	if _, ok := byID["GG-42"]; ok {
		t.Fatalf("sloppy fixture ID GG-42 in _test.go was scanned")
	}
	got := byID["GG-0001"]
	if len(got) != 2 {
		t.Fatalf("expected 2 refs to GG-0001 (code.go ref + code_test.go verifies), got %d", len(got))
	}
	var sawVerifies bool
	for _, r := range got {
		if r.Kind == KindVerifies && strings.HasSuffix(r.File, "_test.go") {
			sawVerifies = true
		}
	}
	if !sawVerifies {
		t.Fatalf("verifies: annotation in _test.go was not picked up: %+v", got)
	}
}
