package vet

import (
	"strings"
	"testing"
)

// RV18-P1-21(6): //go:systemstack was missing from the known-directive map,
// producing false "unknown directive" positives on Go's own runtime sources
// (which graalgo compiles). The known list was audited against the pragmas
// actually used in the Go toolchain source (go1.27.1).
func TestDirectiveSystemstack(t *testing.T) {
	src := `package main

//go:systemstack
func f() {}

//go:nowritebarrier
func g() {}

//go:registerparams
func h() {}
`
	joined := runVetOnSrc(t, src)
	for _, d := range []string{"systemstack", "nowritebarrier", "registerparams"} {
		if strings.Contains(joined, "unknown go: directive "+d) {
			t.Errorf("false positive on //go:%s:\n%s", d, joined)
		}
	}
}

func TestDirectiveUnknownStillFlagged(t *testing.T) {
	src := `package main

//go:unknownpragma123
func f() {}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "unknown go: directive unknownpragma123") {
		t.Errorf("expected unknown-directive diagnostic, got:\n%s", joined)
	}
}
