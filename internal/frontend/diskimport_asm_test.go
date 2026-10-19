package main

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"graalgo-frontend/asmstrategy"
)

// ADR-0007 addendum D9: buildDiskPackage-level tests for the D2 hook.
//
// The hook is DISABLED by default (D8 hard gate); these tests enable it
// explicitly via GRAALGO_ASM_FAILFAST=1. Neutering the hook body or the
// IsStdlibDir gate turns the fail-fast test red with
// "build proceeded silently" (red-green proof).

// TestAsmFailFastDisabledByDefault proves the D8 hard gate: with the
// hook disabled, disk builds behave exactly as before -- no error, no
// resolutions, nothing on the wire.
func TestAsmFailFastDisabledByDefault(t *testing.T) {
	t.Setenv("GRAALGO_ASM_FAILFAST", "")
	restore := setTestAsmInventory(nil)
	defer restore()
	sp, err := buildDiskPackage("sync/atomic")
	if err != nil {
		t.Fatalf("hook disabled: buildDiskPackage(sync/atomic): %v", err)
	}
	if len(sp.asmResolutions) != 0 {
		t.Fatalf("hook disabled: expected no asm resolutions, got %v", sp.asmResolutions)
	}
}

// TestAsmFailFastHookResolves proves the enabled hook resolves the .s
// files gc would assemble against the embedded inventory and carries
// the resolutions on the syntheticPackage for the wire.
func TestAsmFailFastHookResolves(t *testing.T) {
	t.Setenv("GRAALGO_ASM_FAILFAST", "1")
	restore := setTestAsmInventory(nil)
	defer restore()

	sp, err := buildDiskPackage("sync/atomic")
	if err != nil {
		t.Fatalf("hook enabled: buildDiskPackage(sync/atomic): %v", err)
	}
	if len(sp.asmResolutions) == 0 {
		t.Fatal("hook enabled: sync/atomic has buildable .s files but carried no resolutions")
	}
	for _, r := range sp.asmResolutions {
		if r.Category != "intrinsify" {
			t.Errorf("sync/atomic: %s category = %q, want intrinsify", filepath.Base(r.File), r.Category)
		}
		if r.Replacement == "" {
			t.Errorf("sync/atomic: %s has empty replacement", filepath.Base(r.File))
		}
	}

	sp, err = buildDiskPackage("internal/cpu")
	if err != nil {
		t.Fatalf("hook enabled: buildDiskPackage(internal/cpu): %v", err)
	}
	if len(sp.asmResolutions) == 0 {
		t.Fatal("hook enabled: internal/cpu has buildable .s files but carried no resolutions")
	}
	for _, r := range sp.asmResolutions {
		if r.Category != "evaporate" {
			t.Errorf("internal/cpu: %s category = %q, want evaporate", filepath.Base(r.File), r.Category)
		}
	}
}

// testAsmInventoryBytes builds a minimal inventory payload for the D2
// hook's test seam, pinned to the running toolchain's version.
func testAsmInventoryBytes(t *testing.T, files []asmstrategy.AsmEntry) []byte {
	t.Helper()
	payload := map[string]any{
		"go_version": runtime.Version(),
		"categories": []string{"evaporate", "intrinsify", "emulate-then-intrinsify", "jvm-math", "unsupported"},
		"files":      files,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshaling test inventory: %v", err)
	}
	return data
}

// TestAsmFailFastBuildError proves the fail-fast: with a test inventory
// marking sync/atomic's .s file unsupported, the disk build fails with
// the exact ADR error format (file+package+category+reason).
func TestAsmFailFastBuildError(t *testing.T) {
	t.Setenv("GRAALGO_ASM_FAILFAST", "1")
	inv, err := asmstrategy.NewInventory(testAsmInventoryBytes(t, []asmstrategy.AsmEntry{
		{
			Path: "sync/atomic/asm.s", Package: "sync/atomic",
			Category: "unsupported", Reason: "test: no strategy decided",
		},
	}), "gofrontend")
	if err != nil {
		t.Fatalf("test inventory: %v", err)
	}
	restore := setTestAsmInventory(inv)
	defer restore()

	_, err = buildDiskPackage("sync/atomic")
	if err == nil {
		t.Fatal("unsupported .s file in GOROOT/src: build proceeded silently")
	}
	msg := err.Error()
	for _, want := range []string{
		"gofrontend: asm fail-fast:",
		"sync/atomic",
		`"sync/atomic"`,
		`"unsupported"`,
		"test: no strategy decided",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("fail-fast error missing %q:\n%s", want, msg)
		}
	}
}

// TestAsmFailFastThirdPartyUnaffected proves the structural scoping: a
// stdlib-shaped package outside GOROOT/src is not subject to the hook
// (IsStdlibDir is false), keeping the purego-fallback pattern working.
func TestAsmFailFastThirdPartyUnaffected(t *testing.T) {
	t.Setenv("GRAALGO_ASM_FAILFAST", "1")
	dir := t.TempDir()
	if asmstrategy.IsStdlibDir(dir, "/nonexistent-goroot") {
		t.Fatal("IsStdlibDir: temp dir must not be a stdlib dir")
	}
	if got := asmstrategy.IsStdlibDir(dir, ""); got {
		t.Fatal("IsStdlibDir: empty goroot must be false")
	}
}
