package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestStripProgSeparator is the regression test for the "--" leak:
// `graalgo run main.go -- -x` must pass "-x", not "--" then "-x", to
// the program (the driver adds its own "--" when invoking the backend).
func TestStripProgSeparator(t *testing.T) {
	cases := []struct {
		in, want []string
	}{
		{[]string{"--", "-x"}, []string{"-x"}},
		{[]string{"--"}, []string{}},
		{[]string{"-x"}, []string{"-x"}},
		{[]string{}, []string{}},
		// Only one separator is consumed: a program that genuinely
		// wants "--" as its first argument spells it "-- --".
		{[]string{"--", "--"}, []string{"--"}},
		{[]string{"--", "--", "a"}, []string{"--", "a"}},
	}
	for _, c := range cases {
		got := stripProgSeparator(c.in)
		if len(got) != len(c.want) {
			t.Errorf("stripProgSeparator(%q) = %q, want %q", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("stripProgSeparator(%q) = %q, want %q", c.in, got, c.want)
				break
			}
		}
	}
}

func TestSplitTags(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"mytag", []string{"mytag"}},
		{"a b c", []string{"a", "b", "c"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{"a, b  c", []string{"a", "b", "c"}},
	}
	for _, c := range cases {
		got := splitTags(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitTags(%q) = %q, want %q", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitTags(%q) = %q, want %q", c.in, got, c.want)
				break
			}
		}
	}
}

func containsFile(files []string, base string) bool {
	for _, f := range files {
		if filepath.Base(f) == base {
			return true
		}
	}
	return false
}

// TestResolveFilesTagsConstraints is the regression test for the
// plain-glob file selection: GOOS/GOARCH-suffixed alternatives and
// //go:build-gated files must not be returned together, or they
// redeclare each other's symbols.
func TestResolveFilesTagsConstraints(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package p\n\nfunc A() int { return 1 }\n")
	// Platform alternatives: only the current platform's file is in.
	other := "darwin"
	if runtime.GOOS == "darwin" {
		other = "linux"
	}
	writeFile(t, dir, "b_"+runtime.GOOS+".go", "package p\n\nfunc B() int { return 2 }\n")
	writeFile(t, dir, "b_"+other+".go", "package p\n\nfunc B() int { return 3 }\n")
	// Tag-gated alternatives: only the tagged file is in with the tag.
	writeFile(t, dir, "c_tag.go", "//go:build mytag\n\npackage p\n\nfunc C() int { return 4 }\n")
	writeFile(t, dir, "c_notag.go", "//go:build !mytag\n\npackage p\n\nfunc C() int { return 5 }\n")

	plain, err := resolveFilesTags([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFile(plain, "b_"+runtime.GOOS+".go") {
		t.Errorf("no-tag selection %v missing current-platform file", plain)
	}
	if containsFile(plain, "b_"+other+".go") {
		t.Errorf("no-tag selection %v wrongly includes other-platform file", plain)
	}
	if !containsFile(plain, "c_notag.go") {
		t.Errorf("no-tag selection %v missing !mytag file", plain)
	}
	if containsFile(plain, "c_tag.go") {
		t.Errorf("no-tag selection %v wrongly includes mytag file", plain)
	}

	tagged, err := resolveFilesTags([]string{dir}, []string{"mytag"})
	if err != nil {
		t.Fatal(err)
	}
	if !containsFile(tagged, "c_tag.go") {
		t.Errorf("-tags selection %v missing mytag file", tagged)
	}
	if containsFile(tagged, "c_notag.go") {
		t.Errorf("-tags selection %v wrongly includes !mytag file", tagged)
	}
}

// TestResolveFilesTagsImportPath is the regression test for import-path
// operands: `graalgo run/vet fmt` and same-module import paths must
// resolve via the enclosing module / GOROOT instead of failing with
// "no such file or directory".
func TestResolveFilesTagsImportPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/m\n\ngo 1.21\n")
	sub := filepath.Join(root, "sub")
	writeFile(t, sub, "sub.go", "package sub\n\nfunc Hi() string { return \"hi\" }\n")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	files, err := resolveFilesTags([]string{"example.com/m/sub"}, nil)
	if err != nil {
		t.Fatalf("same-module import path: %v", err)
	}
	if !containsFile(files, "sub.go") {
		t.Errorf("same-module import path resolved to %v, want sub.go", files)
	}

	// Standard library import path (GOROOT).
	stdFiles, err := resolveFilesTags([]string{"fmt"}, nil)
	if err != nil {
		t.Fatalf("stdlib import path: %v", err)
	}
	if len(stdFiles) == 0 {
		t.Errorf("stdlib import path resolved to no files")
	}

	// An unresolvable operand keeps the old, recognizable error.
	if _, err := resolveFilesTags([]string{"example.com/no/such/pkg"}, nil); err == nil || !strings.Contains(err.Error(), "no such file or directory") {
		t.Errorf("unknown import path error = %v, want no-such-file error", err)
	}
}

// TestPartitionByPackage is the regression test for vet's package
// grouping: internal test files ride with their package, external
// test files form a separate group — go/types cannot check foo and
// foo_test files as one package.
func TestPartitionByPackage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package p\n\nfunc A() int { return 1 }\n")
	writeFile(t, dir, "b.go", "package p\n\nfunc B() int { return A() }\n")
	writeFile(t, dir, "internal_test.go", "package p\n\nfunc helper() {}\n")
	writeFile(t, dir, "external_test.go", "package p_test\n\nfunc helper2() {}\n")
	files := []string{
		filepath.Join(dir, "a.go"),
		filepath.Join(dir, "b.go"),
		filepath.Join(dir, "internal_test.go"),
		filepath.Join(dir, "external_test.go"),
	}
	groups := partitionByPackage(files)
	if len(groups) != 2 {
		t.Fatalf("partitionByPackage = %d groups, want 2: %v", len(groups), groups)
	}
	if len(groups[0]) != 3 {
		t.Errorf("first group = %v, want the 3 package-p files", groups[0])
	}
	if len(groups[1]) != 1 || filepath.Base(groups[1][0]) != "external_test.go" {
		t.Errorf("second group = %v, want only external_test.go", groups[1])
	}
	// Order within a group is preserved.
	if filepath.Base(groups[0][0]) != "a.go" || filepath.Base(groups[0][2]) != "internal_test.go" {
		t.Errorf("first group order = %v, want a.go, b.go, internal_test.go", groups[0])
	}
}
