package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resetDiskCaches clears the disk-package caches so a subtest builds the
// same import path fresh under a different tag set. Tests run
// sequentially in one process, and the caches are keyed by import path
// alone, so a build under one tag set would otherwise shadow a build
// under another.
func resetDiskCaches() {
	diskMu.Lock()
	defer diskMu.Unlock()
	diskCache = map[string]*syntheticPackage{}
	diskDirCache = map[string]string{}
	diskBuilding = map[string]bool{}
}

// V12-P1-4: the -tags values given on the gofrontend command line must
// select which files of a disk package are built, exactly like
// GRAALGO_BUILD_TAGS does: a file gated on `foo` is built only when the
// tag is set, and its `!foo` sibling only when it is not.
func TestBuildDiskPackageBuildTags(t *testing.T) {
	gopath := t.TempDir()
	pkgDir := filepath.Join(gopath, "src", "example.com", "tagpkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{
		"a.go":   "package tagpkg\n\nfunc Base() string { return \"base\" }\n",
		"foo.go": "//go:build foo\n\npackage tagpkg\n\nfunc OnlyFoo() string { return \"tag-foo-selected\" }\n",
		"bar.go": "//go:build !foo\n\npackage tagpkg\n\nfunc OnlyBar() string { return \"tag-bar-selected\" }\n",
	} {
		if err := os.WriteFile(filepath.Join(pkgDir, name), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GOPATH", gopath)
	// Point the module cache at an empty dir so the lookup goes to
	// GOPATH/src instead of scanning the real module cache.
	t.Setenv("GOMODCACHE", filepath.Join(gopath, "pkg", "mod"))

	oldTags := userBuildTags
	t.Cleanup(func() { userBuildTags = oldTags })

	for _, tc := range []struct {
		name    string
		tags    []string
		wantFoo bool
		wantBar bool
	}{
		{name: "tags=foo", tags: []string{"foo"}, wantFoo: true, wantBar: false},
		{name: "no tags", tags: nil, wantFoo: false, wantBar: true},
		{name: "tags=bar", tags: []string{"bar"}, wantFoo: false, wantBar: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			userBuildTags = tc.tags
			resetDiskCaches()
			sp, err := buildDiskPackage("example.com/tagpkg")
			if err != nil {
				t.Fatalf("buildDiskPackage: %v", err)
			}
			scope := sp.pkg.Scope()
			if got := scope.Lookup("OnlyFoo") != nil; got != tc.wantFoo {
				t.Errorf("OnlyFoo present = %v, want %v (tags=%v)", got, tc.wantFoo, tc.tags)
			}
			if got := scope.Lookup("OnlyBar") != nil; got != tc.wantBar {
				t.Errorf("OnlyBar present = %v, want %v (tags=%v)", got, tc.wantBar, tc.tags)
			}
			if scope.Lookup("Base") == nil {
				t.Errorf("Base missing: the untagged file must always be built")
			}
		})
	}
}

// V12-P1-4: end to end through ExportFiles: a main package that uses the
// foo-gated symbol exports only when the tag is set, and the !foo symbol
// only when it is not.
func TestExportFilesBuildTags(t *testing.T) {
	gopath := t.TempDir()
	pkgDir := filepath.Join(gopath, "src", "example.com", "tagpkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{
		"a.go":   "package tagpkg\n\nfunc Base() string { return \"base\" }\n",
		"foo.go": "//go:build foo\n\npackage tagpkg\n\nfunc OnlyFoo() string { return \"tag-foo-selected\" }\n",
		"bar.go": "//go:build !foo\n\npackage tagpkg\n\nfunc OnlyBar() string { return \"tag-bar-selected\" }\n",
	} {
		if err := os.WriteFile(filepath.Join(pkgDir, name), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	workDir := t.TempDir()
	mainFoo := filepath.Join(workDir, "mainfoo.go")
	if err := os.WriteFile(mainFoo, []byte("package main\n\nimport \"example.com/tagpkg\"\n\nfunc main() { println(tagpkg.OnlyFoo()) }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mainBar := filepath.Join(workDir, "mainbar.go")
	if err := os.WriteFile(mainBar, []byte("package main\n\nimport \"example.com/tagpkg\"\n\nfunc main() { println(tagpkg.OnlyBar()) }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPATH", gopath)
	t.Setenv("GOMODCACHE", filepath.Join(gopath, "pkg", "mod"))

	oldTags := userBuildTags
	t.Cleanup(func() {
		userBuildTags = oldTags
		resetDiskCaches()
	})

	t.Run("foo main with foo tag exports", func(t *testing.T) {
		userBuildTags = []string{"foo"}
		resetDiskCaches()
		out, err := ExportFiles([]string{mainFoo}, "")
		if err != nil {
			t.Fatalf("ExportFiles(mainfoo, tags=foo): %v", err)
		}
		if !strings.Contains(out, "tag-foo-selected") {
			t.Errorf("exported JSON missing the foo-gated symbol value; got:\n%s", out)
		}
		if strings.Contains(out, "tag-bar-selected") {
			t.Errorf("exported JSON contains the !foo symbol value under tags=foo")
		}
	})
	t.Run("bar main with foo tag fails", func(t *testing.T) {
		userBuildTags = []string{"foo"}
		resetDiskCaches()
		if _, err := ExportFiles([]string{mainBar}, ""); err == nil {
			t.Errorf("ExportFiles(mainbar, tags=foo): want type error (OnlyBar not built), got nil")
		}
	})
	t.Run("bar main without tags exports", func(t *testing.T) {
		userBuildTags = nil
		resetDiskCaches()
		out, err := ExportFiles([]string{mainBar}, "")
		if err != nil {
			t.Fatalf("ExportFiles(mainbar, no tags): %v", err)
		}
		if !strings.Contains(out, "tag-bar-selected") {
			t.Errorf("exported JSON missing the !foo symbol value; got:\n%s", out)
		}
	})
	t.Run("foo main without tags fails", func(t *testing.T) {
		userBuildTags = nil
		resetDiskCaches()
		if _, err := ExportFiles([]string{mainFoo}, ""); err == nil {
			t.Errorf("ExportFiles(mainfoo, no tags): want type error (OnlyFoo not built), got nil")
		}
	})
}
