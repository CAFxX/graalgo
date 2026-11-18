package release

import (
	"runtime"
	"testing"
)

// D10: the frontend is the tracked release's go/parser + go/types, which is
// true only if this module is built by the tracked toolchain.
//
// verifies: GG-0069 (the build-and-test gate fails when graalgo-tool's
// module is not built by exactly the tracked release)
func TestBuiltByTrackedToolchain(t *testing.T) {
	if runtime.Version() != GoVersion {
		t.Fatalf("built by %s; graalgo tracks %s (update release.GoVersion together with the toolchain)", runtime.Version(), GoVersion)
	}
}
