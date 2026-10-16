package main

// Unit tests for -tags handling (P0-1, P1-3 remediation):
// - parseBuildFlags captures -tags (and still rejects unknown flags)
// - parseTestFlags accepts the same build-flag surface as parseBuildFlags
// - buildableGoFiles and filterBuildableFiles honor tags via go/build
//   constraints
//
// All tests are pure logic: no backend is invoked.

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBuildFlagsTags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    string // expected bf.tags
		wantErr bool
	}{
		{"separate value", []string{"-tags", "netgo"}, "netgo", false},
		{"equals form", []string{"-tags=netgo,osusergo"}, "netgo,osusergo", false},
		{"empty value", []string{"-tags="}, "", false},
		{"missing value", []string{"-tags"}, "", true},
		{"unknown flag", []string{"-frobnicate"}, "", true},
		{"tags among packages", []string{"-a", "-tags", "foo", "./pkg"}, "foo", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bf, err := parseBuildFlags(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseBuildFlags(%q) = no error, want error", tc.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseBuildFlags(%q) = error %v, want tags %q", tc.args, err, tc.want)
			}
			if bf.tags != tc.want {
				t.Errorf("parseBuildFlags(%q).tags = %q, want %q", tc.args, bf.tags, tc.want)
			}
		})
	}
}

func TestParseBuildFlagsTagsPreservesOthers(t *testing.T) {
	// -tags must not disturb the other build flags or package args.
	bf, err := parseBuildFlags([]string{"-o", "out", "-tags", "x", "-a", "./pkg"})
	if err != nil {
		t.Fatal(err)
	}
	if bf.output != "out" || bf.tags != "x" {
		t.Errorf("got output=%q tags=%q, want output=%q tags=%q", bf.output, bf.tags, "out", "x")
	}
	if len(bf.pkgs) != 1 || bf.pkgs[0] != "./pkg" {
		t.Errorf("got pkgs=%q, want [./pkg]", bf.pkgs)
	}
}

func TestParseTestFlagsTags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantTags string
		wantPkgs []string
		wantCode int // expected *flagError code; 0 = no error
		wantHelp bool
	}{
		{"separate value", []string{"-tags", "netgo", "pkg"}, "netgo", []string{"pkg"}, 0, false},
		{"equals form", []string{"-tags=netgo"}, "netgo", nil, 0, false},
		{"missing value", []string{"-tags"}, "", nil, 2, false},
		{"unknown flag", []string{"-bogus"}, "", nil, 2, false},
		{"unknown flag with equals", []string{"-bogus=x"}, "", nil, 2, false},
		{"ldflags unsupported", []string{"-ldflags", "x"}, "", nil, 1, false},
		{"o rejected", []string{"-o", "x"}, "", nil, 2, false},
		{"accepted and ignored", []string{"-a", "-x", "-work", "-n", "pkg"}, "", []string{"pkg"}, 0, false},
		{"help", []string{"-h"}, "", nil, 0, true},
		{"bad count", []string{"-count", "0"}, "", nil, 2, false},
		{"test flags still work", []string{"-v", "-run=Foo", "-count=2", "pkg"}, "", []string{"pkg"}, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tf, pkgs, err := parseTestFlags(tc.args)
			if tc.wantHelp {
				if !errors.Is(err, errHelp) {
					t.Fatalf("parseTestFlags(%q) err = %v, want errHelp", tc.args, err)
				}
				return
			}
			if tc.wantCode != 0 {
				var fe *flagError
				if !errors.As(err, &fe) {
					t.Fatalf("parseTestFlags(%q) err = %v, want *flagError", tc.args, err)
				}
				if fe.code != tc.wantCode {
					t.Errorf("parseTestFlags(%q) exit code = %d, want %d", tc.args, fe.code, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTestFlags(%q) = error %v", tc.args, err)
			}
			if tf.tags != tc.wantTags {
				t.Errorf("parseTestFlags(%q).tags = %q, want %q", tc.args, tf.tags, tc.wantTags)
			}
			if len(pkgs) != len(tc.wantPkgs) {
				t.Fatalf("parseTestFlags(%q).pkgs = %q, want %q", tc.args, pkgs, tc.wantPkgs)
			}
			for i := range pkgs {
				if pkgs[i] != tc.wantPkgs[i] {
					t.Errorf("parseTestFlags(%q).pkgs = %q, want %q", tc.args, pkgs, tc.wantPkgs)
					break
				}
			}
		})
	}
}

func TestParseTestFlagsValues(t *testing.T) {
	// The pre-existing test flags keep their semantics through the new parser.
	tf, _, err := parseTestFlags([]string{"-v", "-run", "Foo.*", "-count", "3"})
	if err != nil {
		t.Fatal(err)
	}
	if !tf.verbose || tf.runFilter != "Foo.*" || tf.count != 3 {
		t.Errorf("got %+v, want verbose runFilter=Foo.* count=3", tf)
	}
}

// tagTestDir builds a small package exercising build constraints:
// plain.go (always built), tagged.go (needs -tags mytag), excluded.go
// (excluded under -tags mytag).
func tagTestDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/tagtest\n\ngo 1.21\n")
	writeFile(t, root, "p.go", "package tagtest\n\nfunc F() int { return 1 }\n")
	writeFile(t, root, "tagged.go", "//go:build mytag\n\npackage tagtest\n\nfunc G() int { return 2 }\n")
	writeFile(t, root, "excluded.go", "//go:build !mytag\n\npackage tagtest\n\nfunc H() int { return 3 }\n")
	return root
}

func baseNames(files []string) []string {
	var out []string
	for _, f := range files {
		out = append(out, filepath.Base(f))
	}
	return out
}

func hasBase(files []string, base string) bool {
	for _, f := range files {
		if filepath.Base(f) == base {
			return true
		}
	}
	return false
}

func TestBuildableGoFilesTags(t *testing.T) {
	root := tagTestDir(t)

	// No tags: tagged.go (needs mytag) is out, the rest are in.
	plain := buildableGoFiles(root, nil)
	if !hasBase(plain, "p.go") || !hasBase(plain, "excluded.go") {
		t.Errorf("buildableGoFiles(root, nil) = %v, want p.go and excluded.go", baseNames(plain))
	}
	if hasBase(plain, "tagged.go") {
		t.Errorf("buildableGoFiles(root, nil) = %v, must exclude tagged.go without the tag", baseNames(plain))
	}

	// -tags mytag: tagged.go is in, excluded.go (!mytag) is out.
	with := buildableGoFiles(root, []string{"mytag"})
	if !hasBase(with, "p.go") || !hasBase(with, "tagged.go") {
		t.Errorf("buildableGoFiles(root, [mytag]) = %v, want p.go and tagged.go", baseNames(with))
	}
	if hasBase(with, "excluded.go") {
		t.Errorf("buildableGoFiles(root, [mytag]) = %v, must exclude excluded.go", baseNames(with))
	}
}

func TestFilterBuildableFilesTags(t *testing.T) {
	root := tagTestDir(t)
	matches, err := filepath.Glob(filepath.Join(root, "*.go"))
	if err != nil || len(matches) != 3 {
		t.Fatalf("glob = %v, %v; want 3 files", matches, err)
	}

	// Empty/whitespace tags: identity, like the pre-fix path.
	if got := filterBuildableFiles(matches, ""); len(got) != 3 {
		t.Errorf("filterBuildableFiles(files, %q) = %v, want all files", "", baseNames(got))
	}
	if got := filterBuildableFiles(matches, "  "); len(got) != 3 {
		t.Errorf("filterBuildableFiles(files, %q) = %v, want all files", "  ", baseNames(got))
	}

	// The P0-1 case: -tags mytag must actually select files.
	got := filterBuildableFiles(matches, "mytag")
	if len(got) != 2 || !hasBase(got, "p.go") || !hasBase(got, "tagged.go") {
		t.Errorf("filterBuildableFiles(files, %q) = %v, want [p.go tagged.go]", "mytag", baseNames(got))
	}

	// Multiple tags, space-separated like the flag value.
	got = filterBuildableFiles(matches, "mytag anothertag")
	if len(got) != 2 || !hasBase(got, "p.go") || !hasBase(got, "tagged.go") {
		t.Errorf("filterBuildableFiles(files, %q) = %v, want [p.go tagged.go]", "mytag anothertag", baseNames(got))
	}

	// Comma-separated, like `go -tags tag,list`.
	got = filterBuildableFiles(matches, "mytag,anothertag")
	if len(got) != 2 || !hasBase(got, "p.go") || !hasBase(got, "tagged.go") {
		t.Errorf("filterBuildableFiles(files, %q) = %v, want [p.go tagged.go]", "mytag,anothertag", baseNames(got))
	}

	// A file that does not exist on disk is kept so the backend reports
	// the real error (the no-tags path behaves the same).
	missing := filepath.Join(root, "does-not-exist.go")
	got = filterBuildableFiles([]string{missing}, "mytag")
	if len(got) != 1 || got[0] != missing {
		t.Errorf("filterBuildableFiles([missing], %q) = %v, want the file kept", "mytag", got)
	}
}

func TestFilterBuildableFilesOrderStable(t *testing.T) {
	// Filtering preserves input order; it only drops files.
	root := tagTestDir(t)
	in := []string{
		filepath.Join(root, "tagged.go"),
		filepath.Join(root, "p.go"),
		filepath.Join(root, "excluded.go"),
	}
	got := filterBuildableFiles(in, "mytag")
	if len(got) != 2 || got[0] != in[0] || got[1] != in[1] {
		t.Errorf("filterBuildableFiles order = %v, want [%s %s]", baseNames(got), "tagged.go", "p.go")
	}
	if strings.Join(baseNames(got), ",") != "tagged.go,p.go" {
		t.Errorf("unexpected result: %v", baseNames(got))
	}
}
