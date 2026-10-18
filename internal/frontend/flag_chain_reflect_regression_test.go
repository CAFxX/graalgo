package main

// fix-0260c83d (fix-pkg077: dependency-chain imports use importPkgTree):
// the needFlag/needTesting/needTestingQuick dependency chains hardcoded
// getSyntheticPackage(p) for each package in their lists. After the
// real-reflect migration retired synthetic reflect to the disk importer,
// any program triggering needFlag (e.g. encoding/pem tests importing
// flag) panicked with "gofrontend: no synthetic source for reflect".
// The general fix replaces the direct getSyntheticPackage calls with
// importPkgTree, which resolves via resolvePkgTree: synthetic sources
// use getSyntheticPackage, retired packages fall through to
// getDiskPackage (real GOROOT via the disk importer), and dedicated
// stubs are skipped. This future-proofs all the dependency-chain loops
// against further synthetic retirements.
//
// Red-green: before the fix, TestFlagChainReflectRetired panics with
// that message; after, the flag dependency chain (including
// disk-imported reflect) exports.

import (
	"testing"
)

// TestFlagChainReflectRetired pins that importing "flag" — whose
// dependency chain includes the retired-from-synthetic reflect package
// — no longer panics the exporter.
func TestFlagChainReflectRetired(t *testing.T) {
	const src = `package main

import "flag"

func main() {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	_ = fs
}
`
	if _, err := ExportFile(writeIfaceProg(t, src)); err != nil {
		t.Fatalf("export failed: %v", err)
	}
}
