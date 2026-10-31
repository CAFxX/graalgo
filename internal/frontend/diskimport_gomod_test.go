package main

// Tests for go.mod-pinned disk import resolution (review lane L4).
// These tests build fake module caches and fake enclosing go.mod files in
// temp dirs; they never touch the real module cache or GOROOT.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// withImportingModule sets the process-scoped importing-module context
// for the test and restores the previous context afterwards, so later
// tests in this package never see a stale temp-dir module.
func withImportingModule(t *testing.T, dir string) {
	t.Helper()
	importingModuleMu.Lock()
	old := importingModule
	importingModuleMu.Unlock()
	setImportingModule(dir)
	t.Cleanup(func() {
		importingModuleMu.Lock()
		importingModule = old
		importingModuleMu.Unlock()
	})
}

func TestParseGoModDirectives(t *testing.T) {
	data := []byte(`module example.com/main

go 1.27

toolchain go1.27.1

require example.com/dep v1.2.3 // indirect

require (
	example.com/other v0.9.0
	example.com/multi v2.0.0
)

replace example.com/dep => example.com/dep v1.2.4

replace (
	example.com/local => ../local
	example.com/pinned v1.0.0 => example.com/pinned v1.0.1
)
`)
	m := parseGoMod(data)
	if m.path != "example.com/main" {
		t.Fatalf("module = %q, want example.com/main", m.path)
	}
	if m.goVersion != "1.27" {
		t.Fatalf("go = %q, want 1.27", m.goVersion)
	}
	if m.toolchain != "go1.27.1" {
		t.Fatalf("toolchain = %q, want go1.27.1", m.toolchain)
	}
	if m.requires["example.com/dep"] != "v1.2.3" {
		t.Fatalf("require dep = %q, want v1.2.3", m.requires["example.com/dep"])
	}
	if m.requires["example.com/multi"] != "v2.0.0" {
		t.Fatalf("require multi = %q, want v2.0.0", m.requires["example.com/multi"])
	}
	rep, ok := m.replaces["example.com/dep"]
	if !ok || rep.newVersion != "v1.2.4" {
		t.Fatalf("replace dep = %+v, want newVersion v1.2.4", rep)
	}
	rep, ok = m.replaces["example.com/local"]
	if !ok || !rep.local {
		t.Fatalf("replace local = %+v, want local replacement", rep)
	}
	rep, ok = m.replaces["example.com/pinned"]
	if !ok || rep.oldVersion != "v1.0.0" || rep.newVersion != "v1.0.1" {
		t.Fatalf("replace pinned = %+v, want old v1.0.0 new v1.0.1", rep)
	}
}

func TestPinnedRequirementLongestPrefix(t *testing.T) {
	m := parseGoMod([]byte(`module example.com/main

require (
	example.com/dep v1.0.0
	example.com/dep/sub v9.9.9
)
`))
	// Longest prefix wins: the subpackage import pins to the sub-module.
	modPath, ver, local, pinned := m.pinnedRequirement("example.com/dep/sub/pkg")
	if !pinned || modPath != "example.com/dep/sub" || ver != "v9.9.9" || local != "" {
		t.Fatalf("sub import pinned = (%q, %q, %q, %v), want (example.com/dep/sub, v9.9.9, \"\", true)", modPath, ver, local, pinned)
	}
	modPath, ver, _, pinned = m.pinnedRequirement("example.com/dep/other")
	if !pinned || modPath != "example.com/dep" || ver != "v1.0.0" {
		t.Fatalf("sibling import pinned = (%q, %q, %v), want (example.com/dep, v1.0.0, true)", modPath, ver, pinned)
	}
	if _, _, _, pinned := m.pinnedRequirement("example.com/unrelated"); pinned {
		t.Fatalf("unrelated import unexpectedly pinned")
	}
}

func TestPinnedRequirementReplace(t *testing.T) {
	m := parseGoMod([]byte(`module example.com/main

require example.com/dep v1.0.0

replace example.com/dep => example.com/dep v2.0.0
`))
	modPath, ver, _, pinned := m.pinnedRequirement("example.com/dep")
	if !pinned || modPath != "example.com/dep" || ver != "v2.0.0" {
		t.Fatalf("replaced import pinned = (%q, %q, %v), want (example.com/dep, v2.0.0, true)", modPath, ver, pinned)
	}

	// A version-specific replace only applies to its version.
	m2 := parseGoMod([]byte(`module example.com/main

require example.com/dep v1.0.0

replace example.com/dep v9.9.9 => example.com/dep v2.0.0
`))
	_, ver, _, _ = m2.pinnedRequirement("example.com/dep")
	if ver != "v1.0.0" {
		t.Fatalf("version-specific replace applied to wrong version: got %q, want v1.0.0", ver)
	}
}

func TestCheckModuleToolchain(t *testing.T) {
	tool := "go1.27.1"
	if err := checkModuleToolchain(nil, tool); err != nil {
		t.Fatalf("nil module: unexpected error %v", err)
	}
	cases := []struct {
		gomod string
		ok    bool
	}{
		{"module m\n\ngo 1.27\n", true},
		{"module m\n\ngo 1.23\n", true},  // go directive is a floor
		{"module m\n\ngo 1.28\n", false}, // newer than the toolchain
		{"module m\n\ngotoolchain go1.27.1\n", true},
		{"module m\n\ntoolchain go1.27.1\n", true},
		{"module m\n\ntoolchain go1.26.0\n", false},
		{"module m\n", true},
	}
	// Note: "gotoolchain" is not a directive; it must be ignored, which
	// the toolchain-mismatch case below covers via "toolchain".
	for _, c := range cases {
		m := parseGoMod([]byte(c.gomod))
		err := checkModuleToolchain(m, tool)
		if c.ok && err != nil {
			t.Errorf("go.mod %q: unexpected error %v", c.gomod, err)
		}
		if !c.ok && err == nil {
			t.Errorf("go.mod %q: expected error, got nil", c.gomod)
		}
	}
}

func TestPinnedGorootMatchesBuildToolchain(t *testing.T) {
	if os.Getenv("GOROOT") != "" {
		t.Skip("GOROOT override set; pin check depends on the ambient toolchain")
	}
	goroot, err := pinnedGoroot()
	if err != nil {
		t.Fatalf("pinnedGoroot: %v", err)
	}
	if goroot != runtime.GOROOT() {
		t.Fatalf("pinnedGoroot = %q, want runtime.GOROOT() %q", goroot, runtime.GOROOT())
	}
	data, err := os.ReadFile(filepath.Join(goroot, "VERSION"))
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	if first := strings.SplitN(string(data), "\n", 2)[0]; first != runtime.Version() {
		t.Fatalf("VERSION first line = %q, want %q", first, runtime.Version())
	}
}

// fakeModCache builds mc/<module>@<version>/<sub...> dirs and returns mc.
func fakeModCache(t *testing.T, modules map[string][]string) string {
	t.Helper()
	mc := filepath.Join(t.TempDir(), "modcache")
	for modVer, subs := range modules {
		for _, sub := range subs {
			dir := filepath.Join(mc, modVer)
			if sub != "" {
				dir = filepath.Join(dir, filepath.FromSlash(sub))
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	return mc
}

// fakeModuleDir writes go.mod content into a temp dir and returns it.
func fakeModuleDir(t *testing.T, gomod string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestResolveDiskPackageDirPinned(t *testing.T) {
	mc := fakeModCache(t, map[string][]string{
		"example.com/dep@v1.0.0": {"sub"},
		"example.com/dep@v2.0.0": {"sub"},
	})
	t.Setenv("GOMODCACHE", mc)
	modDir := fakeModuleDir(t, "module example.com/main\n\nrequire example.com/dep v1.0.0\n")
	withImportingModule(t, modDir)

	dir, err := resolveDiskPackageDir("example.com/dep/sub")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := filepath.Join(mc, "example.com", "dep@v1.0.0", "sub")
	if dir != want {
		t.Fatalf("resolved = %q, want pinned %q (highest on disk must not win)", dir, want)
	}
}

func TestResolveDiskPackageDirPinnedMissingFallsBack(t *testing.T) {
	mc := fakeModCache(t, map[string][]string{
		"example.com/dep@v1.0.0": {"sub"},
		"example.com/dep@v2.0.0": {"sub"},
	})
	t.Setenv("GOMODCACHE", mc)
	// Pinned v3.0.0 is absent from the cache: fallback is the highest
	// present version (with a stderr warning, not asserted here).
	modDir := fakeModuleDir(t, "module example.com/main\n\nrequire example.com/dep v3.0.0\n")
	withImportingModule(t, modDir)

	dir, err := resolveDiskPackageDir("example.com/dep/sub")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := filepath.Join(mc, "example.com", "dep@v2.0.0", "sub")
	if dir != want {
		t.Fatalf("resolved = %q, want fallback %q", dir, want)
	}
}

func TestResolveDiskPackageDirUnpinnedFallsBack(t *testing.T) {
	mc := fakeModCache(t, map[string][]string{
		"example.com/dep@v1.0.0": {"sub"},
		"example.com/dep@v2.0.0": {"sub"},
	})
	t.Setenv("GOMODCACHE", mc)
	modDir := fakeModuleDir(t, "module example.com/main\n")
	withImportingModule(t, modDir)

	dir, err := resolveDiskPackageDir("example.com/dep/sub")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := filepath.Join(mc, "example.com", "dep@v2.0.0", "sub")
	if dir != want {
		t.Fatalf("resolved = %q, want highest %q", dir, want)
	}
}

func TestResolveDiskPackageDirLocalReplace(t *testing.T) {
	local := filepath.Join(t.TempDir(), "localdep", "sub")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	mc := fakeModCache(t, map[string][]string{
		"example.com/dep@v2.0.0": {"sub"},
	})
	t.Setenv("GOMODCACHE", mc)
	modDir := fakeModuleDir(t, "module example.com/main\n\nrequire example.com/dep v1.0.0\n\nreplace example.com/dep => "+filepath.Dir(local)+"\n")
	// The replace target's parent is localdep; the subpackage dir is
	// localdep/sub. Recompute: replace path must be the module root.
	withImportingModule(t, modDir)

	dir, err := resolveDiskPackageDir("example.com/dep/sub")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if dir != local {
		t.Fatalf("resolved = %q, want local replace %q", dir, local)
	}
}

func TestExportFilesToolchainMismatch(t *testing.T) {
	write := func(dir, gomod, src string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "main.go")
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	src := "package main\n\nfunc main() {}\n"

	// Newer go directive than the build toolchain: must fail.
	dir := t.TempDir()
	p := write(dir, "module example.com/main\n\ngo 1.99\n", src)
	if _, err := ExportFiles([]string{p}, ""); err == nil {
		t.Fatalf("go 1.99 module: expected toolchain error, got nil")
	} else if !strings.Contains(err.Error(), "1.99") {
		t.Fatalf("go 1.99 module: error %v does not name the directive", err)
	}

	// Different toolchain directive: must fail.
	dir2 := t.TempDir()
	p2 := write(dir2, "module example.com/main\n\ntoolchain go9.9.9\n", src)
	if _, err := ExportFiles([]string{p2}, ""); err == nil {
		t.Fatalf("toolchain go9.9.9 module: expected error, got nil")
	}

	// Compatible module: must succeed, and the document records the
	// pinned toolchain version in Generator.GoRoot.
	dir3 := t.TempDir()
	p3 := write(dir3, "module example.com/main\n\ngo 1.27\n", src)
	out, err := ExportFiles([]string{p3}, "")
	if err != nil {
		t.Fatalf("compatible module: %v", err)
	}
	// MarshalIndent separates keys from values with ": ".
	if !strings.Contains(out, `"goroot": `+`"`+runtime.Version()+`"`) {
		t.Fatalf("export document does not record pinned GOROOT version %q", runtime.Version())
	}
}
