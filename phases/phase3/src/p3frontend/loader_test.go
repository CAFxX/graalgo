package p3frontend

import (
	"go/build"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// releaseTags mirrors the loader's tag set: go1.1 through go1.27.
func releaseTags() []string {
	var tags []string
	for v := 1; v <= 27; v++ {
		tags = append(tags, "go1."+strconv.Itoa(v))
	}
	return tags
}

// oracleGoFiles is the go/build oracle: (*build.Context{GOOS:linux,
// GOARCH:amd64}).ImportDir, i.e. the file set the stock toolchain builds.
func oracleGoFiles(t *testing.T, dir string) []string {
	t.Helper()
	ctxt := &build.Context{GOOS: "linux", GOARCH: "amd64", Compiler: "gc", ReleaseTags: releaseTags()}
	p, err := ctxt.ImportDir(dir, 0)
	if err != nil {
		t.Fatalf("oracle ImportDir(%s): %v", dir, err)
	}
	got := append([]string{}, p.GoFiles...)
	sort.Strings(got)
	return got
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestBuildTagFiltering verifies: GG-0023 — SelectFiles must agree
// file-for-file with the stock go/build oracle, on a synthetic corpus
// exercising suffix rules, //go:build lines, legacy // +build lines and
// exclusions, and on two real stdlib directories (src/os, src/strings).
func TestBuildTagFiltering(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"plain.go":             "package corpus\n",
		"a_linux.go":           "package corpus\n",
		"a_windows.go":         "package corpus\n",
		"a_darwin.go":          "package corpus\n",
		"a_amd64.go":           "package corpus\n",
		"a_arm64.go":           "package corpus\n",
		"a_linux_amd64.go":     "package corpus\n",
		"a_linux_arm64.go":     "package corpus\n",
		"a_amd64_linux.go":     "package corpus\n", // last segment is a known OS -> included
		"ignore.go":            "//go:build ignore\n\npackage corpus\n",
		"both.go":              "//go:build linux && amd64\n\npackage corpus\n",
		"either.go":            "//go:build linux || windows\n\npackage corpus\n",
		"neither.go":           "//go:build windows && amd64\n\npackage corpus\n",
		"notlinux.go":          "//go:build !linux\n\npackage corpus\n",
		"go127.go":             "//go:build go1.27\n\npackage corpus\n",
		"go128.go":             "//go:build go1.28\n\npackage corpus\n",
		"go116.go":             "//go:build go1.16\n\npackage corpus\n",
		"unix.go":              "//go:build unix\n\npackage corpus\n",
		"legacy.go":            "// +build linux\n\npackage corpus\n",
		"legacy_excl.go":       "// +build windows\n\npackage corpus\n",
		"legacy_multi.go":      "// +build linux\n// +build amd64\n\npackage corpus\n",
		"legacy_multi_excl.go": "// +build linux\n// +build windows\n\npackage corpus\n",
		"legacy_or.go":         "// +build linux,windows\n\npackage corpus\n",
		"not_test.go":          "package corpus\n",
		"skip_test.go":         "package corpus\n",
		"_hidden.go":           "package corpus\n",
		".hidden.go":           "package corpus\n",
		"suffix_and_tag.go":    "//go:build linux\n\npackage corpus\n",
		"x_windows.go":         "//go:build linux\n\npackage corpus\n",
	})

	loader := NewLoader()
	dirs := []string{dir, filepath.Join(loader.GOROOT(), "src", "os"), filepath.Join(loader.GOROOT(), "src", "strings")}
	for _, d := range dirs {
		got, err := loader.SelectFiles(d)
		if err != nil {
			t.Fatalf("SelectFiles(%s): %v", d, err)
		}
		want := oracleGoFiles(t, d)
		if !reflect.DeepEqual(got, want) {
			var extra, missing []string
			gset, wset := map[string]bool{}, map[string]bool{}
			for _, f := range got {
				gset[f] = true
			}
			for _, f := range want {
				wset[f] = true
			}
			for _, f := range got {
				if !wset[f] {
					extra = append(extra, f)
				}
			}
			for _, f := range want {
				if !gset[f] {
					missing = append(missing, f)
				}
			}
			t.Fatalf("SelectFiles(%s) != oracle:\n extra=%v\n missing=%v", d, extra, missing)
		}
	}
}

// runWithTimeout runs fn and fails the test if it does not return within d.
// It proves fail-fast behavior (an import-cycle hang would trip it).
func runWithTimeout(t *testing.T, d time.Duration, fn func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		t.Fatalf("timed out after %v: not fail-fast", d)
		return nil
	}
}

// TestImportCycle verifies: GG-0012 — an a<->b import cycle and a
// self-import cycle fail fast with an error containing "import cycle".
// The timeout guard proves the resolver cannot hang.
func TestImportCycle(t *testing.T) {
	t.Run("mutual", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{
			"go.mod":  "module example.com/cyc\n\ngo 1.27\n",
			"main.go": "package main\n\nimport _ \"example.com/cyc/a\"\n\nfunc main() {}\n",
			"a/a.go":  "package a\n\nimport _ \"example.com/cyc/b\"\n",
			"b/b.go":  "package b\n\nimport _ \"example.com/cyc/a\"\n",
		})
		err := runWithTimeout(t, 15*time.Second, func() error {
			_, err := MergeDir(dir)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "import cycle") {
			t.Fatalf("expected import-cycle error, got: %v", err)
		}
		t.Logf("cycle error: %v", err)
	})

	t.Run("self", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{
			"go.mod":  "module example.com/self\n\ngo 1.27\n",
			"main.go": "package main\n\nimport _ \"example.com/self/c\"\n\nfunc main() {}\n",
			"c/c.go":  "package c\n\nimport _ \"example.com/self/c\"\n",
		})
		err := runWithTimeout(t, 15*time.Second, func() error {
			_, err := MergeDir(dir)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "import cycle") {
			t.Fatalf("expected import-cycle error, got: %v", err)
		}
		t.Logf("cycle error: %v", err)
	})
}
