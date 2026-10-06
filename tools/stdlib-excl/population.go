package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// population.go — the measured population shared by "validate" (test-binary
// count for the 5% cap clause) and "measure" (the per-dir classification).
//
// Population definition (documented in
// phases/phase3/stdlib-fork/coupled-packages-go1.27.1.md): directories under
// the stdlib source root containing at least one non-test .go file,
// excluding:
//   - cmd/            (tool binaries, not stdlib packages)
//   - vendor/         (third-party vendored deps, not stdlib)
//   - any testdata/ subtree (test fixtures, not packages)
//
// On go1.27.1 this yields 399 dirs; SPEC §2.8's earlier-tree measurement
// counted 398, i.e. the denominator drifted by +1 (tree growth).

// isExcludedDir reports whether rel (slash-separated, relative to the
// source root) is outside the measured population for structural reasons.
func isExcludedDir(rel string) bool {
	if rel == "." || rel == "" {
		return true
	}
	segs := strings.Split(rel, "/")
	if segs[0] == "cmd" || segs[0] == "vendor" {
		return true
	}
	for _, s := range segs {
		if s == "testdata" {
			return true
		}
	}
	return false
}

// listGoFiles returns the .go file names (not paths) directly inside dir,
// split into non-test and test files.
func listGoFiles(dir string) (nonTest, test []string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			test = append(test, name)
		} else {
			nonTest = append(nonTest, name)
		}
	}
	return nonTest, test, nil
}

// walkPopulation calls fn for every population dir under srcRoot.
// rel is the slash-separated path relative to srcRoot.
func walkPopulation(srcRoot string, fn func(rel, abs string, nonTest, test []string) error) error {
	return filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if isExcludedDir(rel) {
			if rel != "." && rel != "" {
				return filepath.SkipDir
			}
			return nil
		}
		nonTest, test, err := listGoFiles(path)
		if err != nil {
			return err
		}
		if len(nonTest) == 0 {
			return nil
		}
		return fn(rel, path, nonTest, test)
	})
}

// countTestBinaries counts population dirs containing at least one
// _test.go file: the "stdlib test binaries" of the review-cap proposal
// (P3-M6 exclusion list: 25 packages OR 5% of stdlib test binaries,
// whichever binds first).
func countTestBinaries(srcRoot string) (int, error) {
	n := 0
	err := walkPopulation(srcRoot, func(rel, abs string, nonTest, test []string) error {
		if len(test) > 0 {
			n++
		}
		return nil
	})
	return n, err
}
