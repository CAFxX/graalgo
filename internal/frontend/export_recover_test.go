package main

// Regression test for RV18-P1-09c: a synthetic package that fails to
// build (parse/type errors in its generated source) must surface from
// ExportFiles as a clean gofrontend: error — never as a raw Go panic
// escaping to the caller. (--check-synthetic mode already recovers in
// main.go; this pins the same behavior on the normal export path, where
// the panic travels through overlayImporter.Import.)

import (
	"strings"
	"testing"
)

// TestExportFilesRecoversSyntheticBuildPanic registers a deliberately
// broken synthetic source under a fixture-only import path, then exports
// a program importing it. buildSyntheticPackage panics on the parse
// error; ExportFiles must convert that into a gofrontend: error.
func TestExportFilesRecoversSyntheticBuildPanic(t *testing.T) {
	const fakePath = "example.com/synthetic-broken-fixture"
	syntheticSources[fakePath] = "package broken\nfunc broken( { // deliberate parse error\n"
	defer delete(syntheticSources, fakePath)

	src := "package main\n\nimport _ \"" + fakePath + "\"\n\nfunc main() {}\n"
	// No assert.Panics wrapper: the test PASSES only if ExportFiles
	// returns normally. A propagated panic fails the test outright.
	_, err := ExportFile(writeSyncProg(t, src))
	if err == nil {
		t.Fatalf("ExportFile with broken synthetic package: expected error, got nil")
	}
	if !strings.HasPrefix(err.Error(), "gofrontend:") {
		t.Errorf("ExportFile with broken synthetic package: error %q lacks the gofrontend: prefix", err.Error())
	}
	if !strings.Contains(err.Error(), "parse error") {
		t.Errorf("ExportFile with broken synthetic package: error %q does not name the parse failure", err.Error())
	}
}
