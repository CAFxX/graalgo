package fork

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"graalgo-toolexec/release"
)

func TestApply(t *testing.T) {
	orig := "a\nb\nc\nd\ne\nf\n"
	patch := "--- a/x\n+++ b/x\n@@ -2,3 +2,3 @@\n b\n-c\n+C\n d\n@@ -5,0 +6,2 @@\n+x\n+y\n@@ -6 +8 @@\n-f\n+F\n"
	got, added, deleted, err := Apply([]byte(orig), []byte(patch))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a\nb\nC\nd\ne\nx\ny\nF\n" || added != 4 || deleted != 2 {
		t.Errorf("got %q, +%d -%d", got, added, deleted)
	}
	for name, bad := range map[string]string{
		"context differs":   "@@ -2,2 +2,2 @@\n B\n-c\n+C\n",
		"wrong line":        "@@ -3,2 +3,2 @@\n b\n-c\n+C\n",
		"deleted differs":   "@@ -3 +3 @@\n-x\n+C\n",
		"truncated":         "@@ -2,3 +2,3 @@\n b\n-c\n+C\n",
		"past the end":      "@@ -9 +9 @@\n-f\n+F\n",
		"no hunks":          "--- a/x\n+++ b/x\n",
		"overlapping hunks": "@@ -2 +2 @@\n-b\n+B\n@@ -2 +2 @@\n-b\n+B\n",
	} {
		if _, _, _, err := Apply([]byte(orig), []byte(bad)); err == nil {
			t.Errorf("%s: patch applied", name)
		}
	}
	// A file without a final newline.
	got, _, _, err = Apply([]byte("a\nb"), []byte("@@ -2 +2 @@\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n"))
	if err != nil || string(got) != "a\nc" {
		t.Errorf("no final newline: %q, %v", got, err)
	}
}

// tree writes files (name -> content) under a fresh directory.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

var upstreamFiles = map[string]string{
	"VERSION":                 "go9.9.9\ntime 2026-01-01T00:00:00Z\n",
	"bin/go":                  "binary",
	"src/rt/a.go":             "package rt\n",
	"src/rt/a_test.go":        "package rt\n",
	"src/rt/asm.s":            "TEXT\n",
	"src/rt/sub/s.go":         "package sub\n",
	"src/lib/x.go":            "package lib\n\nfunc F() int {\n\treturn 1\n}\n",
	"src/lib/y.go":            "package lib\n",
	"src/other/deep/z.go":     "package deep\n",
	"src/other/deep/data.txt": "data",
}

var forkFiles = map[string]string{
	"fork/rt/facade.go":      "package rt\n\nfunc Intrinsic()\n",
	"fork/rt/stubs.s":        "",
	"patches/lib/x.go.patch": "--- a/src/lib/x.go\n+++ b/src/lib/x.go\n@@ -4 +4 @@\n-\treturn 1\n+\treturn 2\n",
	"patches/.gitkeep":       "",
}

func TestLoadOverlayAssemble(t *testing.T) {
	up := tree(t, upstreamFiles)
	std := tree(t, forkFiles)
	f, err := Load(std, up, "go9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Replaced) != 1 || f.Replaced[0].ImportPath != "rt" || len(f.Patched) != 1 || f.Patched[0].Rel != "lib/x.go" {
		t.Fatalf("fork = %+v", f)
	}
	audit, err := f.Audit()
	if err != nil || !strings.Contains(audit, "total: 1 replaced packages (2 files, 3 lines), 1 patched files (+1 -1)") {
		t.Errorf("audit:\n%s%v", audit, err)
	}

	work := t.TempDir()
	data, err := f.Overlay(work)
	if err != nil {
		t.Fatal(err)
	}
	var ov struct{ Replace map[string]string }
	if err := json.Unmarshal(data, &ov); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(up, "src")
	want := map[string]string{
		filepath.Join(src, "rt/a.go"):      "",
		filepath.Join(src, "rt/a_test.go"): "",
		filepath.Join(src, "rt/asm.s"):     "",
		filepath.Join(src, "rt/facade.go"): filepath.Join(std, "fork/rt/facade.go"),
		filepath.Join(src, "rt/stubs.s"):   filepath.Join(std, "fork/rt/stubs.s"),
		filepath.Join(src, "lib/x.go"):     filepath.Join(work, "lib/x.go"),
	}
	if len(ov.Replace) != len(want) {
		t.Errorf("overlay has %d entries, want %d: %v", len(ov.Replace), len(want), ov.Replace)
	}
	for k, v := range want {
		if ov.Replace[k] != v {
			t.Errorf("overlay[%s] = %q, want %q", k, ov.Replace[k], v)
		}
	}
	if patched, _ := os.ReadFile(filepath.Join(work, "lib/x.go")); !strings.Contains(string(patched), "return 2") {
		t.Errorf("patched file: %q", patched)
	}

	for _, shallow := range []bool{false, true} {
		out := filepath.Join(t.TempDir(), "goroot")
		if err := f.Assemble(out, shallow); err != nil {
			t.Fatal(err)
		}
		read := func(rel string) string {
			data, err := os.ReadFile(filepath.Join(out, rel))
			if err != nil {
				return "<" + err.Error() + ">"
			}
			return string(data)
		}
		for rel, want := range map[string]string{
			"VERSION":                 upstreamFiles["VERSION"],
			"bin/go":                  "binary",
			"src/rt/facade.go":        forkFiles["fork/rt/facade.go"],
			"src/rt/sub/s.go":         "package sub\n", // a subpackage of a replaced package is not replaced
			"src/lib/x.go":            "package lib\n\nfunc F() int {\n\treturn 2\n}\n",
			"src/lib/y.go":            "package lib\n",
			"src/other/deep/data.txt": "data",
		} {
			if got := read(rel); got != want {
				t.Errorf("shallow=%v: %s = %q, want %q", shallow, rel, got, want)
			}
		}
		for _, hidden := range []string{"src/rt/a.go", "src/rt/a_test.go", "src/rt/asm.s"} {
			if _, err := os.Lstat(filepath.Join(out, hidden)); err == nil {
				t.Errorf("shallow=%v: %s survives the replacement", shallow, hidden)
			}
		}
		// Only src is materialized; with shallow, only what the fork touches.
		for rel, wantLink := range map[string]bool{"bin": true, "src": false, "src/rt": false, "src/lib": false, "src/other": shallow, "src/rt/sub": shallow} {
			st, err := os.Lstat(filepath.Join(out, rel))
			if err != nil {
				t.Fatal(err)
			}
			if isLink := st.Mode()&os.ModeSymlink != 0; isLink != wantLink {
				t.Errorf("shallow=%v: %s symlink = %v, want %v", shallow, rel, isLink, wantLink)
			}
		}
		if err := f.Assemble(out, shallow); err == nil {
			t.Errorf("Assemble overwrote an existing directory")
		}
	}
	// Upstream is never modified.
	if got, _ := os.ReadFile(filepath.Join(up, "src/lib/x.go")); string(got) != upstreamFiles["src/lib/x.go"] {
		t.Errorf("upstream file changed: %q", got)
	}
}

func TestLoadRejects(t *testing.T) {
	up := tree(t, upstreamFiles)
	for name, c := range map[string]struct {
		files   map[string]string
		version string
		want    string
	}{
		"other release":           {forkFiles, "go9.9.8", "the fork is against go9.9.8"},
		"replaced not upstream":   {map[string]string{"fork/nope/a.go": "package nope\n"}, "go9.9.9", "replaced package nope does not exist upstream"},
		"patch does not apply":    {map[string]string{"patches/lib/x.go.patch": "@@ -4 +4 @@\n-\treturn 7\n+\treturn 2\n"}, "go9.9.9", "does not apply to go9.9.9 lib/x.go"},
		"patched file missing":    {map[string]string{"patches/lib/nope.go.patch": "@@ -1 +1 @@\n-a\n+b\n"}, "go9.9.9", "does not exist upstream"},
		"copy instead of a patch": {map[string]string{"patches/lib/x.go": "package lib\n"}, "go9.9.9", "never copies of upstream"},
		"replaced and patched":    {map[string]string{"fork/lib/a.go": "package lib\n", "patches/lib/x.go.patch": forkFiles["patches/lib/x.go.patch"]}, "go9.9.9", "both replaced and patched"},
		"patch among fork files":  {map[string]string{"fork/rt/a.go": "package rt\n", "fork/rt/a.go.patch": "x"}, "go9.9.9", "patches belong under stdlib/patches"},
		"no Go source":            {map[string]string{"fork/rt/README": "x"}, "go9.9.9", "no Go source"},
		"no-op patch":             {map[string]string{"patches/lib/x.go.patch": "@@ -4 +4 @@\n-\treturn 1\n+\treturn 1\n"}, "go9.9.9", "changes nothing"},
	} {
		_, err := Load(tree(t, c.files), up, c.version)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one containing %q", name, err, c.want)
		}
	}
}

// The repository's fork loads against the tracked release: every replaced
// package exists upstream and every patch applies exactly. This is the
// gate that keeps stdlib/ honest (AGENTS §4: diffs, never copies).
func TestRepositoryFork(t *testing.T) {
	f, err := Load(filepath.Join("..", "..", "..", "stdlib"), runtime.GOROOT(), release.GoVersion)
	if err != nil {
		t.Fatal(err)
	}
	var replaced []string
	for _, r := range f.Replaced {
		replaced = append(replaced, r.ImportPath)
	}
	if got := strings.Join(replaced, " "); got != "internal/runtime/maps runtime runtime/race" {
		t.Errorf("replaced packages = %q; update this test and the fork-size record in docs/arch/toolexec-design.md §13 together", got)
	}
	audit, err := f.Audit()
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + audit)
}
