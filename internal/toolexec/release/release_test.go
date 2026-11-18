package release

import (
	"os"
	"runtime"
	"strings"
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

// verifies: GG-0069 (graalgo-tool refuses to run when built by another
// release, naming both versions)
func TestCheckBuiltBy(t *testing.T) {
	if err := CheckBuiltBy(GoVersion); err != nil {
		t.Fatalf("tracked release refused: %v", err)
	}
	for _, v := range []string{"go1.27.0", "go1.28rc1", "devel go1.27-abc", ""} {
		err := CheckBuiltBy(v)
		if err == nil {
			t.Fatalf("built by %q accepted", v)
		}
		if !strings.Contains(err.Error(), "built by "+v+",") || !strings.Contains(err.Error(), GoVersion) {
			t.Errorf("message does not name both versions: %v", err)
		}
	}
}

// verifies: GG-0069 (the module names the tracked release as its toolchain,
// so a `go` command with GOTOOLCHAIN=auto selects it for graalgo-tool)
func TestGoModToolchain(t *testing.T) {
	data, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "toolchain" {
			if f[1] != GoVersion {
				t.Fatalf("go.mod: toolchain %s; graalgo tracks %s", f[1], GoVersion)
			}
			return
		}
	}
	t.Fatalf("go.mod has no toolchain line (want toolchain %s)", GoVersion)
}
