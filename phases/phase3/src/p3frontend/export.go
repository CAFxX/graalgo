package p3frontend

import (
	"bytes"
	"fmt"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ExportDir compiles the multi-file Go package in dir (which must be
// package main) into one frontend/v1 JSON document: load the dependency
// closure, type-check every package with the graduated importer, flatten
// everything into one merged source file, and hand it to the frozen
// gofrontend service. Deterministic: the same input yields byte-identical
// output.
func ExportDir(dir string) (string, error) {
	merged, err := MergeDir(dir)
	if err != nil {
		return "", err
	}
	tmpDir, err := os.MkdirTemp("", "p3frontend")
	if err != nil {
		return "", fmt.Errorf("p3frontend: creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	// Fixed basename: the document records package.files, so a random name
	// would break determinism. The temp dir itself never enters the doc.
	tmpFile := filepath.Join(tmpDir, "p3merged.go")
	if err := os.WriteFile(tmpFile, []byte(merged), 0644); err != nil {
		return "", fmt.Errorf("p3frontend: writing merged source: %w", err)
	}
	return runGofrontend(tmpFile)
}

// MergeDir runs the ExportDir pipeline up to (but excluding) the frozen
// exporter and returns the merged package-main source. Tests use it to
// inspect or execute the flattened program directly.
func MergeDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("p3frontend: resolving dir %s: %w", dir, err)
	}
	loader := NewLoader()
	mainRef, err := loader.mainRef(abs)
	if err != nil {
		return "", err
	}
	deps, err := loader.LoadClosure(abs, mainRef)
	if err != nil {
		return "", err
	}
	fset := token.NewFileSet()
	imp := NewImporter(loader, abs, fset)
	for _, dep := range deps {
		if _, err := imp.Import(dep.ImportPath); err != nil {
			return "", err
		}
	}
	if _, err := imp.ImportRef(mainRef); err != nil {
		return "", err
	}
	return Flatten(fset, imp, mainRef, deps)
}

// ---------------------------------------------------------------------------
// Frozen gofrontend consumption.
// ---------------------------------------------------------------------------

// The frontend module (graalgo-frontend) is package main by construction —
// unimportable — so the frozen Go API func ExportFile(path) is consumed
// through its frozen CLI contract (INTERFACE_FREEZE.md §2.1): the
// gofrontend binary's stdout is ExportFile's return value. Binary
// resolution mirrors graalgo.launcher.ToolPaths.gofrontend(): the
// GRAALGO_GOFRONTEND environment variable, then
// <repo>/phases/phase1/frontend/gofrontend, then bare "gofrontend" on PATH.
func gofrontendPath() (string, error) {
	if v := os.Getenv("GRAALGO_GOFRONTEND"); v != "" {
		return v, nil
	}
	if _, thisFile, _, ok := runtime.Caller(0); ok {
		// thisFile = <repo>/phases/phase3/src/p3frontend/export.go
		cand := filepath.Join(filepath.Dir(thisFile),
			"..", "..", "..", "..", "phases", "phase1", "frontend", "gofrontend")
		if isExecutable(cand) {
			return cand, nil
		}
	}
	if p, err := exec.LookPath("gofrontend"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("p3frontend: gofrontend binary not found: set GRAALGO_GOFRONTEND or build phases/phase1/frontend (./build.sh build)")
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode()&0111 != 0
}

// runGofrontend invokes the frozen service and returns the frontend/v1
// JSON document. Exit 2 (frontend error) surfaces the stderr verbatim.
func runGofrontend(goFile string) (string, error) {
	bin, err := gofrontendPath()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(bin, goFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
			return "", fmt.Errorf("p3frontend: %s", detail)
		}
		return "", fmt.Errorf("p3frontend: gofrontend failed: %v: %s", err, detail)
	}
	// main.go prints ExportFile's return value with fmt.Println.
	return strings.TrimSuffix(stdout.String(), "\n"), nil
}
