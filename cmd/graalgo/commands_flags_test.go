package main

import (
	"strings"
	"testing"
)

// L-L F3: the public `graalgo` CLI never forwards --test-package-dir:
// it is an internal privilege signal for `graalgo test`'s own driver
// invocation only. graalgo run/build must reject it as an unknown flag
// rather than letting a user-supplied value reach the backend launcher.
func TestParseBuildFlagsRejectsTestPackageDir(t *testing.T) {
	for _, flag := range []string{"--test-package-dir=/tmp/x", "--test-package-dir="} {
		if _, err := parseBuildFlags([]string{flag, "prog.go"}); err == nil {
			t.Fatalf("parseBuildFlags(%q): got nil error, want rejection", flag)
		} else if !strings.Contains(err.Error(), "unknown flag") {
			t.Fatalf("parseBuildFlags(%q) error = %q, want unknown-flag rejection", flag, err)
		}
	}
}
