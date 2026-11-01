package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Package-pattern expansion for the drop-in driver (review R-07).
//
// Only "./..." is expanded. Every other "..." pattern is rejected loudly
// by the resolvers (see rejectOtherPattern): silently narrowing a pattern
// the driver cannot honour would be worse than failing, and the wider
// pattern language (per-directory "...", exclusions, "all") is not
// implemented yet.
//
// Expansion mirrors `go list ./...` for the common cases, verified
// against gc by TestExpandDotDotDotMatchesGoList:
//   - walks the tree rooted at the current directory;
//   - a directory counts as a package directory if it contains at least
//     one Go file the go tool would consider (files whose base name
//     starts with "." or "_" are ignored, like the go tool ignores them);
//   - vendor/, testdata/, hidden ("."-prefixed) and "_"-prefixed
//     directories are never descended into;
//   - a nested directory containing its own go.mod is a separate module
//     and is never descended into (its packages belong to that module,
//     not to "./..." rooted here);
//   - symlinked directories are not followed (filepath.WalkDir does not
//     follow them), matching the go tool.
//
// Deliberate approximation: build constraints are NOT evaluated during
// expansion — a directory whose only Go files are excluded by
// GOOS/GOARCH/build tags still counts as a package directory, exactly
// as `go list ./...` still lists such a package. The per-package
// consumers (test's buildableGoFiles, build's non-test filter) apply
// the finer file selection afterwards.

// expandDotDotDot expands "./..." to the package directories under the
// current directory, sorted, in the form WalkDir reports them ("." for
// the root itself, slash-separated relative paths otherwise).
func expandDotDotDot() ([]string, error) {
	found := map[string]bool{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." {
				name := d.Name()
				// Never descend: hidden, "_"-prefixed, testdata,
				// vendor, or a nested module boundary.
				if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
					name == "testdata" || name == "vendor" {
					return filepath.SkipDir
				}
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		// Only Go files the go tool would consider count toward a
		// package directory; "_"- and "."-prefixed files are ignored.
		name := d.Name()
		if strings.HasSuffix(name, ".go") &&
			!strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "_") {
			found[filepath.Dir(path)] = true
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("expanding ./...: %v", err)
	}
	dirs := make([]string, 0, len(found))
	for dir := range found {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no packages matched ./...")
	}
	return dirs, nil
}

// rejectOtherPattern reports whether arg is an unimplemented package
// pattern: anything containing "..." other than exactly "./...".
// It returns a non-nil error for such patterns, nil otherwise.
func rejectOtherPattern(arg string) error {
	if strings.Contains(arg, "...") && arg != "./..." {
		return fmt.Errorf("package pattern %q not yet supported (only ./... is supported)", arg)
	}
	return nil
}

// dedupStrings removes duplicates preserving first-seen order.
func dedupStrings(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// dedupDirs removes duplicate directories preserving first-seen order,
// comparing after filepath.Clean so "./a" and "a" count as one directory.
func dedupDirs(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		c := filepath.Clean(s)
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}
