package release

import (
	"runtime"
	"testing"
)

// D10: the frontend is the tracked release's go/parser + go/types, which is
// true only if this module is built by the tracked toolchain.
func TestBuiltByTrackedToolchain(t *testing.T) {
	if runtime.Version() != GoVersion {
		t.Fatalf("built by %s; graalgo tracks %s (update release.GoVersion together with the toolchain)", runtime.Version(), GoVersion)
	}
}
