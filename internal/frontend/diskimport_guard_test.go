package main

import (
	"errors"
	"testing"
)

// TestResolveDiskPackageDirRejectsSyntheticOverlay is the regression test
// for the dedicated-synthetic-stub guard hole: resolveDiskPackageDir must
// never resolve a synthetic-overlay path from disk. That covers the two
// dedicated synthetic stubs (runtime, unsafe) as well as the
// syntheticSources map — the stubs are not syntheticSources keys, and
// before the fix the guard only consulted syntheticSources, so
// getDiskPackage("runtime") fell through to GOROOT and built the real
// package from disk, registering its multi-result members in the global
// disk-MR table and shadowing the synthetic fail-fast/desugar contracts
// for every later export in the process.
//
// "bytes" is the positive control: a syntheticSources entry that the
// pre-existing guard already rejected. A bogus path exercises the plain
// not-found tail.
func TestResolveDiskPackageDirRejectsSyntheticOverlay(t *testing.T) {
	for _, path := range []string{"runtime", "unsafe", "bytes", "no/such/package/xyz"} {
		dir, err := resolveDiskPackageDir(path)
		if !errors.Is(err, errDiskNotFound) {
			t.Errorf("resolveDiskPackageDir(%q) = (%q, %v), want errDiskNotFound", path, dir, err)
		}
	}
}

// TestDedicatedSyntheticStubsStayOutOfSyntheticSources pins the invariant
// the guard relies on: runtime/unsafe resolve through
// isDedicatedSyntheticStub, not through the syntheticSources map. If a
// future change moves them into syntheticSources, the guard still holds
// (both disjuncts), but this test forces the author to notice the
// invariant shift.
func TestDedicatedSyntheticStubsStayOutOfSyntheticSources(t *testing.T) {
	for _, path := range []string{"runtime", "unsafe"} {
		if _, ok := syntheticSources[path]; ok {
			t.Errorf("%q is now a syntheticSources key; update the resolveDiskPackageDir comment and this test", path)
		}
		if !isDedicatedSyntheticStub(path) {
			t.Errorf("isDedicatedSyntheticStub(%q) = false, want true", path)
		}
	}
}
