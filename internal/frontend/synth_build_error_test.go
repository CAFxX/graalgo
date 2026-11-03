package main

import (
	"strings"
	"testing"
)

// L-L F5: synthetic-package build failures are errors, never panics.
// The builder is exercised through a scratch synthetic source injected
// into syntheticSources for the duration of the test (restored on
// return); the scratch path is never a real package, so no other test
// can observe it, and failures are never cached.

func TestBuildSyntheticPackageUnknownPathIsError(t *testing.T) {
	if _, err := buildSyntheticPackage("definitely/not-a-synthetic-package"); err == nil {
		t.Fatal("buildSyntheticPackage(unknown path): got nil error, want an error (not a panic)")
	} else if !strings.Contains(err.Error(), "no synthetic source") {
		t.Fatalf("buildSyntheticPackage(unknown path) error = %q, want it to name the missing source", err)
	}
	if _, err := getSyntheticPackage("definitely/not-a-synthetic-package"); err == nil {
		t.Fatal("getSyntheticPackage(unknown path): got nil error, want an error")
	}
}

func TestSyntheticBuildFailureIsErrorNotPanic(t *testing.T) {
	const path = "test/broken-ll-f5"
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"parse error", "package broken\nfunc (\n", "parse error"},
		{"type error", "package broken\nfunc F() int { return \"x\" }\n", "type error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			syntheticSources[path] = c.src
			defer func() {
				delete(syntheticSources, path)
				synthMu.Lock()
				delete(synthCache, path)
				synthMu.Unlock()
			}()
			// Direct builder: error, not panic.
			if _, err := buildSyntheticPackage(path); err == nil {
				t.Fatalf("buildSyntheticPackage(%s): got nil error", c.name)
			} else if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("buildSyntheticPackage(%s) error = %q, want it to contain %q", c.name, err, c.want)
			}
			// Cached accessor: error on first call AND on retry
			// (failures are never cached), still no panic.
			for i := 0; i < 2; i++ {
				if _, err := getSyntheticPackage(path); err == nil {
					t.Fatalf("getSyntheticPackage(%s) attempt %d: got nil error", c.name, i)
				} else if !strings.Contains(err.Error(), c.want) {
					t.Fatalf("getSyntheticPackage(%s) attempt %d error = %q, want %q", c.name, i, err, c.want)
				}
			}
			// End to end: a program importing the broken synthetic
			// package fails ExportFiles with an error, not a crash.
			// (Blank import: the overlay importer still builds it.)
			prog := "package main\n\nimport _ \"test/broken-ll-f5\"\n\nfunc main() {}\n"
			if _, err := ExportFiles([]string{writeSyncProg(t, prog)}, ""); err == nil {
				t.Fatalf("ExportFiles importing broken synthetic %s: got nil error", c.name)
			} else if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ExportFiles importing broken synthetic %s error = %q, want %q", c.name, err, c.want)
			}
		})
	}
	// The scratch entry is gone and a real synthetic package still builds.
	if _, err := getSyntheticPackage("errors"); err != nil {
		t.Fatalf("getSyntheticPackage(errors) after broken-source tests: %v", err)
	}
}
