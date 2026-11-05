package main

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"
)

// writeTagPackage creates a tag-alternating main package: a.go is always
// in, foo.go requires the foo tag, bar.go requires !foo, one
// plat_<GOOS>.go file matches the host platform and one plat_<other>.go
// file does not, and the test files mirror the same split.
func writeTagPackage(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, dir, "a.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, "foo.go", "//go:build foo\n\npackage main\n\nfunc Which() string { return \"foo\" }\n")
	writeFile(t, dir, "bar.go", "//go:build !foo\n\npackage main\n\nfunc Which() string { return \"bar\" }\n")
	writeFile(t, dir, "plat_"+runtime.GOOS+".go", "package main\n\nfunc Plat() string { return \""+runtime.GOOS+"\" }\n")
	other := "linux"
	if runtime.GOOS == "linux" {
		other = "darwin"
	}
	writeFile(t, dir, "plat_"+other+".go", "package main\n\nfunc Plat() string { return \""+other+"\" }\n")
	writeFile(t, dir, "a_test.go", "package main\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n")
	writeFile(t, dir, "foo_test.go", "//go:build foo\n\npackage main\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) {}\n")
}

func baseSet(t *testing.T, files []string) []string {
	t.Helper()
	var out []string
	for _, f := range files {
		out = append(out, filepath.Base(f))
	}
	sort.Strings(out)
	return out
}

func TestBuildableGoFilesWithTags(t *testing.T) {
	dir := t.TempDir()
	writeTagPackage(t, dir)
	plat := "plat_" + runtime.GOOS + ".go"

	got := baseSet(t, buildableGoFilesWithTags(dir, nil))
	want := []string{"a.go", "a_test.go", "bar.go", plat}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("no tags: got %v, want %v", got, want)
	}

	got = baseSet(t, buildableGoFilesWithTags(dir, []string{"foo"}))
	want = []string{"a.go", "a_test.go", "foo.go", "foo_test.go", plat}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tags [foo]: got %v, want %v", got, want)
	}

	// A tagged call must not leak its tags into a later untagged call
	// (build.Default's tag slice must not be mutated in place).
	got = baseSet(t, buildableGoFilesWithTags(dir, nil))
	want = []string{"a.go", "a_test.go", "bar.go", plat}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("no tags after tagged call: got %v, want %v (tag leak?)", got, want)
	}
}

func TestBuildableGoFilesWithTagsAllExcluded(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "foo.go", "//go:build foo\n\npackage main\n\nfunc Which() string { return \"foo\" }\n")
	if got := buildableGoFilesWithTags(dir, nil); len(got) != 0 {
		t.Fatalf("all-excluded package: got %v, want empty (fallback to the raw glob would reintroduce the redeclare bug)", got)
	}
}

func TestResolveFilesWithTags(t *testing.T) {
	dir := t.TempDir()
	writeTagPackage(t, dir)
	plat := "plat_" + runtime.GOOS + ".go"

	got, err := resolveFilesWithTags([]string{dir}, []string{"foo"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.go", "a_test.go", "foo.go", "foo_test.go", plat}
	if !reflect.DeepEqual(baseSet(t, got), want) {
		t.Fatalf("dir with tags [foo]: got %v, want %v", baseSet(t, got), want)
	}

	got, err = resolveFilesWithTags([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"a.go", "a_test.go", "bar.go", plat}
	if !reflect.DeepEqual(baseSet(t, got), want) {
		t.Fatalf("dir no tags: got %v, want %v", baseSet(t, got), want)
	}

	// Explicitly listed files bypass constraints, like `go run`/`go build`
	// with file arguments: foo.go is returned even with no tags.
	fooPath := filepath.Join(dir, "foo.go")
	got, err = resolveFilesWithTags([]string{fooPath}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{fooPath}) {
		t.Fatalf("explicit file: got %v, want [%s]", got, fooPath)
	}

	// No args in the package directory selects the untagged set.
	// (os.Chdir, not t.Chdir: the module language version predates it.)
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	got, err = resolveFilesWithTags(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baseSet(t, got), want) {
		t.Fatalf("no args no tags: got %v, want %v", baseSet(t, got), want)
	}

	// An all-excluded directory errors instead of compiling excluded files.
	excl := t.TempDir()
	writeFile(t, excl, "foo.go", "//go:build foo\n\npackage main\n\nfunc Which() string { return \"foo\" }\n")
	if _, err := resolveFilesWithTags([]string{excl}, nil); err == nil {
		t.Fatal("all-excluded directory: expected an error, got none")
	}
}

func TestSplitBuildTags(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"foo", []string{"foo"}},
		{"foo,bar", []string{"foo", "bar"}},
		{"foo bar", []string{"foo", "bar"}},
		{" foo, bar ", []string{"foo", "bar"}},
		{"foo,,bar", []string{"foo", "bar"}},
	}
	for _, c := range cases {
		// Compare element-wise: Fields returns an empty non-nil slice
		// for "", which DeepEqual distinguishes from nil.
		got := splitBuildTags(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitBuildTags(%q): got %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitBuildTags(%q): got %v, want %v", c.in, got, c.want)
			}
		}
	}
}

func TestBackendTagsEnv(t *testing.T) {
	if got := backendTagsEnv(""); got != nil {
		t.Errorf("backendTagsEnv(\"\"): got %v, want nil (ambient env must pass through untouched)", got)
	}
	want := []string{"GRAALGO_BUILD_TAGS=foo bar"}
	if got := backendTagsEnv("foo,bar"); !reflect.DeepEqual(got, want) {
		t.Errorf("backendTagsEnv(\"foo,bar\"): got %v, want %v", got, want)
	}
}

func TestParseBuildFlagsTags(t *testing.T) {
	bf, err := parseBuildFlags([]string{"-tags=foo,bar", "."})
	if err != nil {
		t.Fatal(err)
	}
	if bf.tags != "foo,bar" || len(bf.pkgs) != 1 || bf.pkgs[0] != "." {
		t.Fatalf("-tags= form: got tags=%q pkgs=%v", bf.tags, bf.pkgs)
	}
	bf, err = parseBuildFlags([]string{"-tags", "foo bar", "."})
	if err != nil {
		t.Fatal(err)
	}
	if bf.tags != "foo bar" {
		t.Fatalf("-tags value form: got tags=%q", bf.tags)
	}
	if _, err := parseBuildFlags([]string{"-bogus"}); err == nil {
		t.Fatal("unknown flag: expected an error, got none")
	}
}
