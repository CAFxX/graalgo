package p3frontend

// Tests for the p3frontend target gate (REVIEW-2026-11-03-16, P0-4):
// the loader targets the supported linux/amd64 target, and checkRef
// refuses to type-check when that target does not match the host,
// instead of silently checking host-selected files against another
// target's sizes.

import (
	"go/build"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewLoaderTargetsSupported: the loader's target is the supported
// pair, pinned here so a future edit cannot silently retarget the
// loader without updating this test and the checkRef gate.
func TestNewLoaderTargetsSupported(t *testing.T) {
	l := NewLoader()
	if l.goos != "linux" || l.goarch != "amd64" {
		t.Errorf("NewLoader target = %s/%s, want linux/amd64", l.goos, l.goarch)
	}
	if l.goos != supportedGOOS || l.goarch != supportedGOARCH {
		t.Errorf("NewLoader target = %s/%s, want supportedGOOS/supportedGOARCH", l.goos, l.goarch)
	}
}

// TestCheckRefTargetGate: a loader whose target differs from the host
// fails ImportRef with a target error (positive control: the supported
// loader type-checks the same package). ImportRef drives checkRef
// directly, so no GOROOT resolution is involved.
func TestCheckRefTargetGate(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\n\nfunc F() int { return 1 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ref := &PackageRef{ImportPath: "p", Kind: kindLocal, Dir: dir, Files: []string{"a.go"}}

	supported := NewLoader()
	imp := NewImporter(supported, dir, token.NewFileSet())
	if _, err := imp.ImportRef(ref); err != nil {
		t.Fatalf("supported loader ImportRef: %v", err)
	}

	mismatched := &Loader{goos: "darwin", goarch: "arm64", goroot: supported.GOROOT()}
	imp2 := NewImporter(mismatched, dir, token.NewFileSet())
	_, err := imp2.ImportRef(ref)
	if err == nil {
		t.Fatalf("mismatched loader ImportRef: got success, want a target error")
	}
	if !strings.Contains(err.Error(), "darwin/arm64") || !strings.Contains(err.Error(), build.Default.GOOS) {
		t.Errorf("mismatched loader error %q must name both the loader target and the host", err)
	}
}
