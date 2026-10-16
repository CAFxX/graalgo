package main

import (
	"strings"
	"testing"
)

// TestImportPkgTreeSurvivesSyntheticRetirement (P1-2, review-r2) is the
// falsifying test for the fragile-getSyntheticPackage fix. It simulates the
// retirement of a synthetic package ("cmp") by removing it from the
// syntheticSources overlay, then drives the needX import path in export.go
// for a program that imports it.
//
// The old flow — e.importSyntheticPackage(getSyntheticPackage("cmp")) —
// panicked with 'gofrontend: no synthetic source for cmp' (the PKG-077
// failure mode, fixed for reflect only by commit 0260c83d). importPkgTree
// must resolve the retired path through the disk importer instead of
// panicking.
//
// The test also pins the retirement-safe behavior of the MR-plan guards in
// mrdesugar_m5.go: syntheticPkgForPlan returns nil for the retired path, so
// the pair-table lookups yield no plan instead of panicking. Full
// retirement handling (shrinking the tables as packages retire) is
// bridge-inventory B-3 Phase 2 (docs/arch/bridge-inventory.md).
func TestImportPkgTreeSurvivesSyntheticRetirement(t *testing.T) {
	const retired = "cmp"
	src, ok := syntheticSources[retired]
	if !ok {
		t.Fatalf("precondition: %q must be synthetic for this test", retired)
	}
	// Simulate retirement: drop the overlay entry and any cached build,
	// so both resolvePkgTree and getSyntheticPackage see the retired world.
	synthMu.Lock()
	cached, hadCached := synthCache[retired]
	delete(synthCache, retired)
	synthMu.Unlock()
	delete(syntheticSources, retired)
	defer func() {
		syntheticSources[retired] = src
		synthMu.Lock()
		if hadCached {
			synthCache[retired] = cached
		}
		synthMu.Unlock()
	}()

	// Positive control: the raw accessor panics exactly the way the old
	// needX flow did. Without this, the simulation below could pass
	// vacuously.
	didPanic := func() (panicked bool) {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				if msg, ok := r.(string); !ok || !strings.Contains(msg, "no synthetic source for "+retired) {
					t.Fatalf("unexpected panic from getSyntheticPackage: %v", r)
				}
			}
		}()
		getSyntheticPackage(retired)
		return false
	}()
	if !didPanic {
		t.Fatalf("positive control failed: getSyntheticPackage(%q) did not panic after overlay removal", retired)
	}

	// The fixed resolution path: a retired package resolves from disk,
	// never through the panicking synthetic accessor.
	sp, err := resolvePkgTree(retired)
	if err != nil {
		t.Fatalf("resolvePkgTree(%q) after retirement: %v", retired, err)
	}
	if sp == nil {
		t.Fatalf("resolvePkgTree(%q) after retirement returned nil", retired)
	}
	if sp.path != retired {
		t.Fatalf("resolvePkgTree(%q).path = %q", retired, sp.path)
	}

	// The MR-plan guards: a retired package yields no plan, never a panic.
	if got := syntheticPkgForPlan(retired); got != nil {
		t.Fatalf("syntheticPkgForPlan(%q) = non-nil after retirement", retired)
	}

	// End-to-end: export a program importing the retired package. The
	// needCmp block in export.go now calls e.importPkgTree("cmp"), which
	// must merge the real cmp sources from disk without panicking.
	out := exportSrc(t, `package main

import "cmp"

func main() {
	_ = cmp.Compare(1, 2)
}
`)
	if !strings.Contains(out, "Compare") {
		t.Fatalf("exported document does not mention cmp.Compare")
	}
}
