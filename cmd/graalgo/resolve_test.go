package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// TestResolveFilesTagsBuildGated (N2): a package directory holding two
// //go:build-gated files that declare the SAME symbol must never be
// handed to the backend as one unit — that is the duplicate-declaration
// failure build/run/vet hit with the old bare-glob expansion. With a
// tag, only that tag's variant is returned; without a tag, neither
// variant is returned (go build semantics: a tag-gated file belongs to
// the package only when its tag is requested).
func TestResolveFilesTagsBuildGated(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("plain.go", "package p\n\nfunc Plain() string { return \"plain\" }\n")
	write("variant_a.go", "//go:build taga\n\npackage p\n\nfunc Which() string { return \"a\" }\n")
	write("variant_b.go", "//go:build tagb\n\npackage p\n\nfunc Which() string { return \"b\" }\n")

	base := func(files []string) []string {
		var names []string
		for _, f := range files {
			names = append(names, filepath.Base(f))
		}
		return names
	}

	for _, tc := range []struct {
		tags string
		want []string
	}{
		{"taga", []string{"plain.go", "variant_a.go"}},
		{"tagb", []string{"plain.go", "variant_b.go"}},
		// No tags: both gated variants are excluded, so the two
		// definitions of Which can never collide.
		{"", []string{"plain.go"}},
	} {
		files, err := resolveFilesTags([]string{dir}, tc.tags)
		if err != nil {
			t.Fatalf("tags=%q: resolveFilesTags: %v", tc.tags, err)
		}
		got := base(files)
		slices.Sort(got)
		slices.Sort(tc.want)
		if !slices.Equal(got, tc.want) {
			t.Errorf("tags=%q: files = %v, want %v", tc.tags, got, tc.want)
		}
	}
}

// TestResolveFilesTagsPlatformVariants (N2): filename-suffix platform
// variants (foo_linux.go / foo_windows.go) declaring the same symbol
// are filtered to the current platform even with no -tags.
func TestResolveFilesTagsPlatformVariants(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("plat_linux.go", "package p\n\nfunc Platform() string { return \"linux\" }\n")
	write("plat_windows.go", "package p\n\nfunc Platform() string { return \"windows\" }\n")
	write("plat_darwin.go", "package p\n\nfunc Platform() string { return \"darwin\" }\n")

	files, err := resolveFilesTags([]string{dir}, "")
	if err != nil {
		t.Fatalf("resolveFilesTags: %v", err)
	}
	var names []string
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	want := "plat_" + runtime.GOOS + ".go"
	if len(names) != 1 || names[0] != want {
		t.Errorf("platform files = %v, want [%s] on GOOS=%s", names, want, runtime.GOOS)
	}
}

// TestResolveFilesTagsNoArg (N2): the no-arg current-directory case
// gets the same constraint filtering as an explicit directory.
func TestResolveFilesTagsNoArg(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("//go:build taga\n\npackage p\n\nfunc Which() string { return \"a\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("//go:build tagb\n\npackage p\n\nfunc Which() string { return \"b\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// t.Chdir needs go1.24; the root module is go1.21, so chdir
	// manually (restored on return).
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	files, err := resolveFilesTags(nil, "tagb")
	if err != nil {
		t.Fatalf("resolveFilesTags(nil, tagb): %v", err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "b.go" {
		t.Errorf("no-arg tagged files = %v, want [b.go]", files)
	}
}

// TestResolveFilesTagsExplicitFilesUnfiltered (N2): explicit .go file
// arguments are never constraint-filtered, matching `go build` —
// otherwise a user could not build a single gated file on purpose.
func TestResolveFilesTagsExplicitFilesUnfiltered(t *testing.T) {
	dir := t.TempDir()
	gated := filepath.Join(dir, "gated.go")
	if err := os.WriteFile(gated, []byte("//go:build taga\n\npackage p\n\nfunc Which() string { return \"a\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := resolveFilesTags([]string{gated}, "")
	if err != nil {
		t.Fatalf("resolveFilesTags(explicit): %v", err)
	}
	if len(files) != 1 || files[0] != gated {
		t.Errorf("explicit files = %v, want [%s]", files, gated)
	}
}

// TestResolveFilesUnconstrainedSeesConstrainedFiles (B1 fix-up): fmt's
// expansion must include tag-gated and other-platform files — the
// vetoed commit routed fmt through resolveFilesTags, which silently
// dropped exactly these files from the format list. The contrast
// assertion pins the distinction: the same directory through
// resolveFilesTags("") keeps only the plain file and the current
// platform's variant.
func TestResolveFilesUnconstrainedSeesConstrainedFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("plain.go", "package p\n")
	write("gated.go", "//go:build mytag\n\npackage p\n")
	write("win_windows.go", "package p\n")
	write("lin_linux.go", "package p\n")

	files, err := resolveFilesUnconstrained([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	slices.Sort(names)
	wantAll := []string{"gated.go", "lin_linux.go", "plain.go", "win_windows.go"}
	if !slices.Equal(names, wantAll) {
		t.Errorf("unconstrained files = %v, want %v", names, wantAll)
	}

	tagged, err := resolveFilesTags([]string{dir}, "")
	if err != nil {
		t.Fatal(err)
	}
	var taggedNames []string
	for _, f := range tagged {
		taggedNames = append(taggedNames, filepath.Base(f))
	}
	slices.Sort(taggedNames)
	wantTagged := []string{"plain.go"}
	if runtime.GOOS == "linux" {
		wantTagged = append(wantTagged, "lin_linux.go")
	} else if runtime.GOOS == "windows" {
		wantTagged = append(wantTagged, "win_windows.go")
	}
	slices.Sort(wantTagged)
	if !slices.Equal(taggedNames, wantTagged) {
		t.Errorf("constrained files = %v, want %v", taggedNames, wantTagged)
	}
}

// TestCmdFmtFormatsConstrainedFiles (B1 fix-up): the same guarantee
// through cmdFmt itself, not just its resolver — a directory holding a
// tag-gated file and a foreign-platform file must have both reformatted
// by `graalgo fmt`, not silently skipped. Skipped when no gofmt is
// available in the test environment.
func TestCmdFmtFormatsConstrainedFiles(t *testing.T) {
	if findGofmt() == "" {
		t.Skip("no gofmt available")
	}
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	gated := write("gated.go", "//go:build mytag\n\npackage p\n\nfunc Bar(  ) int { return 2 }\n")
	foreign := write("win_windows.go", "package p\n\nfunc Foo(  ) int { return 1 }\n")
	if code := cmdFmt("", []string{dir}); code != 0 {
		t.Fatalf("cmdFmt(dir) = %d, want 0", code)
	}
	for p, want := range map[string]string{
		gated:   "//go:build mytag\n\npackage p\n\nfunc Bar() int { return 2 }\n",
		foreign: "package p\n\nfunc Foo() int { return 1 }\n",
	} {
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s after fmt = %q, want %q (file was skipped or not formatted)", p, got, want)
		}
	}
}

// TestCmdRunPackageTagsSelectTaggedVariant (B2 fix-up): `graalgo run
// -tags taga <dir>` must hand the backend the taga variant, not the
// !taga/untagged alternative. This exercises cmdRun's package path
// end to end through the fake-backend seam, in the order the driver
// actually runs it: resolveFilesTags with the retained tags first,
// then the per-file filter. The vetoed code passed "" to
// resolveFilesTags, so ImportDir dropped the taga file before the
// filter — which can only keep files it is given — ever saw it.
func TestCmdRunPackageTagsSelectTaggedVariant(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("plain.go", "package main\n\nfunc main() {}\n")
	write("variant_a.go", "//go:build taga\n\npackage main\n\nfunc Which() string { return \"a\" }\n")
	write("variant_b.go", "//go:build tagb\n\npackage main\n\nfunc Which() string { return \"b\" }\n")
	write("a_test.go", "package main\n")

	log := filepath.Join(t.TempDir(), "run.log")
	t.Setenv("GRAALGO_RUN_LOG", log)
	root := fakeBackendRoot(t, "#!/bin/sh\necho \"$@\" >> \"$GRAALGO_RUN_LOG\"\n")
	if code := cmdRun(root, []string{"-tags", "taga", dir}); code != 0 {
		t.Fatalf("cmdRun(-tags taga dir) = %d, want 0", code)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("backend invocations = %d, want 2 (--check + run): %q", len(lines), string(data))
	}
	for _, line := range lines {
		if !strings.Contains(line, "variant_a.go") {
			t.Errorf("backend argv %q: missing variant_a.go (tagged variant not selected)", line)
		}
		if strings.Contains(line, "variant_b.go") {
			t.Errorf("backend argv %q: contains variant_b.go (wrong variant)", line)
		}
		if strings.Contains(line, "a_test.go") {
			t.Errorf("backend argv %q: contains a_test.go (test files must be excluded for run)", line)
		}
	}
}

// TestResolveFilesTagsCommaSeparated (B3 fix-up): comma-separated tags
// must select through resolveFilesTags itself, not only through Lane
// A's filterFilesByBuildTags. With strings.Fields alone, "mytag,other"
// is one (nonexistent) tag, so the mytag file is dropped in favour of
// its !mytag alternative — the exact bug the veto caught.
func TestResolveFilesTagsCommaSeparated(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("gated.go", "//go:build mytag\n\npackage p\n")
	write("alt.go", "//go:build !mytag\n\npackage p\n")

	base := func(files []string) []string {
		var names []string
		for _, f := range files {
			names = append(names, filepath.Base(f))
		}
		slices.Sort(names)
		return names
	}
	for _, tags := range []string{"mytag,other", "other, mytag", "mytag other"} {
		files, err := resolveFilesTags([]string{dir}, tags)
		if err != nil {
			t.Fatal(err)
		}
		if got := base(files); !slices.Equal(got, []string{"gated.go"}) {
			t.Errorf("tags=%q: files = %v, want [gated.go]", tags, got)
		}
	}
}

// TestResolveFilesTagsPuregoSeed (B3 fix-up): buildableGoFilesWithTags
// must use the same tag context as Lane A's filter and the frontend
// disk-import path, purego seed included — so a purego file is kept
// and its !purego alternative dropped, even with no -tags at all.
func TestResolveFilesTagsPuregoSeed(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("pure.go", "//go:build purego\n\npackage p\n")
	write("asm.go", "//go:build !purego\n\npackage p\n")

	for _, tags := range []string{"mytag", ""} {
		files, err := resolveFilesTags([]string{dir}, tags)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, f := range files {
			names = append(names, filepath.Base(f))
		}
		if !slices.Equal(names, []string{"pure.go"}) {
			t.Errorf("tags=%q: files = %v, want [pure.go] (purego seeded)", tags, names)
		}
	}
}
