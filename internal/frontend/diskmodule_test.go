package main

// Tests for module-aware disk resolution (review 2026-11-04, P1-04/P1-05).
// Every test restores the process-wide disk globals it touches, because
// the frontend test suite shares one process and runs sequentially.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resetDiskGlobals restores the disk-resolution globals after the test.
func resetDiskGlobals(t *testing.T) {
	t.Helper()
	oldModCache, oldGoroot, oldTags, oldTagsSet :=
		diskFlagModCache, diskFlagGoroot, diskFlagBuildTags, diskFlagBuildTagsSet
	t.Cleanup(func() {
		diskFlagModCache, diskFlagGoroot, diskFlagBuildTags, diskFlagBuildTagsSet =
			oldModCache, oldGoroot, oldTags, oldTagsSet
		setModuleContext(nil)
	})
}

// writeModule creates dir/go.mod with content and returns dir.
func writeModule(t *testing.T, dir, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeCacheMod creates cache/<escaped module>@<version>[/sub] as a
// directory, returning its path.
func writeCacheMod(t *testing.T, cache, modPath, version, sub string) string {
	t.Helper()
	dir := moduleCacheDir(cache, modPath, version, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestParseGoMod(t *testing.T) {
	resetDiskGlobals(t)
	src := `module example.com/main

go 1.27

require example.com/dep v1.2.3 // indirect

require (
	example.com/other v0.9.0
	example.com/third v2.0.0 // indirect
)

replace example.com/dep => example.com/fork v1.2.4

replace (
	example.com/old => ../localold
	example.com/pinned v1.0.0 => example.com/pinned v1.0.1
)

exclude example.com/bad v3.0.0

retract (
	v0.1.0
)
`
	path, requires, replaces, err := parseGoMod([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if path != "example.com/main" {
		t.Fatalf("module = %q", path)
	}
	want := map[string]string{
		"example.com/dep":   "v1.2.3",
		"example.com/other": "v0.9.0",
		"example.com/third": "v2.0.0",
	}
	for m, v := range want {
		if requires[m] != v {
			t.Fatalf("require %s = %q, want %q", m, requires[m], v)
		}
	}
	if rep := replaces["example.com/dep"]; rep.isDir || rep.version != "v1.2.4" {
		t.Fatalf("replace dep = %+v", rep)
	}
	if rep := replaces["example.com/old"]; !rep.isDir || rep.dir != "../localold" {
		t.Fatalf("replace old = %+v", rep)
	}
	if rep := replaces["example.com/pinned"]; rep.isDir || rep.version != "v1.0.1" || rep.oldVersion != "v1.0.0" {
		t.Fatalf("replace pinned = %+v", rep)
	}
}

func TestParseGoModErrors(t *testing.T) {
	resetDiskGlobals(t)
	for _, src := range []string{
		"require example.com/dep v1.0.0\n",       // no module directive
		"module example.com/m\nrequire (\n",      // unterminated require block
		"module example.com/m\nreplace (\n",      // unterminated replace block
		"module example.com/m\nreplace a b c\n",  // replace without =>
		"module example.com/m\nreplace a => b\n", // module target without version
	} {
		if _, _, _, err := parseGoMod([]byte(src)); err == nil {
			t.Fatalf("parseGoMod(%q) = no error, want error", src)
		}
	}
}

func TestResolveModuleDirPinnedVersion(t *testing.T) {
	resetDiskGlobals(t)
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	modDir := writeModule(t, filepath.Join(root, "main"), `module example.com/main

go 1.27

require example.com/dep v1.0.0
`)
	// Cache holds BOTH versions; go.mod pins v1.0.0. The old code
	// picked v2.0.0 (max); the fix must pick v1.0.0.
	v1sub := writeCacheMod(t, cache, "example.com/dep", "v1.0.0", "sub")
	writeCacheMod(t, cache, "example.com/dep", "v2.0.0", "sub")
	writeCacheMod(t, cache, "example.com/dep", "v2.0.0", "")
	ctx, err := loadModuleContext(modDir)
	if err != nil {
		t.Fatal(err)
	}
	setModuleContext(ctx)
	diskFlagModCache = cache
	dir, handled, err := ctx.resolveModuleDir("example.com/dep/sub", goModCache())
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if dir != v1sub {
		t.Fatalf("resolved %s, want pinned %s", dir, v1sub)
	}
	// The public path must agree (and must not fall back to max).
	got, err := resolveDiskPackageDir("example.com/dep/sub")
	if err != nil || got != v1sub {
		t.Fatalf("resolveDiskPackageDir = %s, %v; want %s", got, err, v1sub)
	}
}

func TestResolveModuleDirFailLoud(t *testing.T) {
	resetDiskGlobals(t)
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	modDir := writeModule(t, filepath.Join(root, "main"), `module example.com/main

go 1.27

require (
	example.com/dep v3.0.0
	example.com/other v1.0.0
)
`)
	writeCacheMod(t, cache, "example.com/dep", "v1.0.0", "")
	writeCacheMod(t, cache, "example.com/dep", "v2.0.0", "")
	writeCacheMod(t, cache, "example.com/other", "v1.0.0", "")
	ctx, err := loadModuleContext(modDir)
	if err != nil {
		t.Fatal(err)
	}
	setModuleContext(ctx)
	diskFlagModCache = cache

	// Required version absent from the cache: loud error, NOT the
	// newest cached version.
	if _, _, err := ctx.resolveModuleDir("example.com/dep", goModCache()); err == nil {
		t.Fatal("dep@v3.0.0 absent from cache: no error")
	} else if !strings.Contains(err.Error(), "v3.0.0") || !strings.Contains(err.Error(), "module cache") {
		t.Fatalf("error %q does not name the version and the cache", err)
	}
	if _, err := resolveDiskPackageDir("example.com/dep"); err == nil {
		t.Fatal("resolveDiskPackageDir(dep): no error for absent version")
	}

	// Module not required at all: loud error naming go.mod.
	if _, _, err := ctx.resolveModuleDir("example.com/unlisted", goModCache()); err == nil {
		t.Fatal("unlisted module: no error")
	} else if !strings.Contains(err.Error(), "go.mod") {
		t.Fatalf("error %q does not name go.mod", err)
	}
	if _, err := resolveDiskPackageDir("example.com/unlisted"); err == nil {
		t.Fatal("resolveDiskPackageDir(unlisted): no error")
	}
}

func TestResolveModuleDirReplace(t *testing.T) {
	resetDiskGlobals(t)
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	local := filepath.Join(root, "localdep")
	if err := os.MkdirAll(filepath.Join(local, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	modDir := writeModule(t, filepath.Join(root, "main"), `module example.com/main

go 1.27

require (
	example.com/dep v1.0.0
	example.com/ver v1.0.0
)

replace example.com/dep => ../localdep

replace example.com/ver => example.com/ver v9.9.9
`)
	writeCacheMod(t, cache, "example.com/ver", "v9.9.9", "sub")
	ctx, err := loadModuleContext(modDir)
	if err != nil {
		t.Fatal(err)
	}
	setModuleContext(ctx)
	diskFlagModCache = cache
	dir, _, err := ctx.resolveModuleDir("example.com/dep/sub", goModCache())
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(local, "sub") {
		t.Fatalf("local replace resolved %s", dir)
	}
	dir, _, err = ctx.resolveModuleDir("example.com/ver/sub", goModCache())
	if err != nil {
		t.Fatal(err)
	}
	if want := moduleCacheDir(cache, "example.com/ver", "v9.9.9", "sub"); dir != want {
		t.Fatalf("version replace resolved %s, want %s", dir, want)
	}
}

func TestResolveModuleDirSameModuleAndLongestPrefix(t *testing.T) {
	resetDiskGlobals(t)
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	modDir := writeModule(t, filepath.Join(root, "main"), `module example.com/main

go 1.27

require (
	example.com/a v1.0.0
	example.com/a/b v2.0.0
)
`)
	subDir := filepath.Join(modDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCacheMod(t, cache, "example.com/a", "v1.0.0", "")
	writeCacheMod(t, cache, "example.com/a/b", "v2.0.0", "c")
	ctx, err := loadModuleContext(modDir)
	if err != nil {
		t.Fatal(err)
	}
	setModuleContext(ctx)
	diskFlagModCache = cache
	// Same-module package resolves into the module tree.
	dir, handled, err := ctx.resolveModuleDir("example.com/main/sub", goModCache())
	if !handled || err != nil || dir != subDir {
		t.Fatalf("same-module: dir=%s handled=%v err=%v, want %s", dir, handled, err, subDir)
	}
	// Longest required prefix wins over the shorter one.
	dir, handled, err = ctx.resolveModuleDir("example.com/a/b/c", goModCache())
	if !handled || err != nil {
		t.Fatalf("longest-prefix: handled=%v err=%v", handled, err)
	}
	if want := moduleCacheDir(cache, "example.com/a/b", "v2.0.0", "c"); dir != want {
		t.Fatalf("longest-prefix resolved %s, want %s", dir, want)
	}
}

func TestResolveModuleDirStdlibBypassesModule(t *testing.T) {
	resetDiskGlobals(t)
	ctx, err := loadModuleContext(writeModule(t, filepath.Join(t.TempDir(), "main"), "module example.com/main\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, handled, err := ctx.resolveModuleDir("database/sql/driver", ""); handled || err != nil {
		t.Fatalf("stdlib path: handled=%v err=%v, want not-handled", handled, err)
	}
	if _, handled, _ := ctx.resolveModuleDir("fmt", ""); handled {
		t.Fatal("stdlib path fmt: handled, want not-handled")
	}
}

func TestLegacyNoModuleMaxVersion(t *testing.T) {
	resetDiskGlobals(t)
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	writeCacheMod(t, cache, "example.com/dep", "v1.0.0", "")
	v2 := writeCacheMod(t, cache, "example.com/dep", "v2.0.0", "")
	setModuleContext(nil)
	diskFlagModCache = cache
	// No module context: legacy newest-version behaviour is preserved.
	dir, err := resolveDiskPackageDir("example.com/dep")
	if err != nil || dir != v2 {
		t.Fatalf("legacy resolve = %s, %v; want %s", dir, err, v2)
	}
}

func TestModuleContextCacheInvalidation(t *testing.T) {
	resetDiskGlobals(t)
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	writeCacheMod(t, cache, "example.com/dep", "v1.0.0", "")
	writeCacheMod(t, cache, "example.com/dep", "v2.0.0", "")
	modA := writeModule(t, filepath.Join(root, "a"), "module example.com/a\n\nrequire example.com/dep v1.0.0\n")
	modB := writeModule(t, filepath.Join(root, "b"), "module example.com/b\n\nrequire example.com/dep v2.0.0\n")
	ctxA, err := loadModuleContext(modA)
	if err != nil {
		t.Fatal(err)
	}
	ctxB, err := loadModuleContext(modB)
	if err != nil {
		t.Fatal(err)
	}
	diskFlagModCache = cache
	setModuleContext(ctxA)
	d1, err := resolveDiskPackageDir("example.com/dep")
	if err != nil {
		t.Fatal(err)
	}
	// Switching modules must NOT reuse A's cached directory.
	setModuleContext(ctxB)
	d2, err := resolveDiskPackageDir("example.com/dep")
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d2 {
		t.Fatalf("context switch reused cached dir %s", d1)
	}
	if !strings.HasSuffix(d2, "v2.0.0") {
		t.Fatalf("after switch resolved %s, want v2.0.0", d2)
	}
}

func TestDiskFlagPrecedence(t *testing.T) {
	resetDiskGlobals(t)
	t.Setenv("GOMODCACHE", "/env/cache")
	t.Setenv("GOPATH", "/env/gopath")
	if got := goModCache(); got != "/env/cache" {
		t.Fatalf("env fallback: goModCache() = %s", got)
	}
	diskFlagModCache = "/flag/cache"
	if got := goModCache(); got != "/flag/cache" {
		t.Fatalf("flag precedence: goModCache() = %s, want /flag/cache", got)
	}
	diskFlagGoroot = "/flag/goroot"
	if got := diskGoroot(); got != "/flag/goroot" {
		t.Fatalf("flag precedence: diskGoroot() = %s", got)
	}
}

func TestExportFilesModuleImportFailsLoud(t *testing.T) {
	resetDiskGlobals(t)
	root := t.TempDir()
	modDir := writeModule(t, filepath.Join(root, "main"), "module example.com/main\n\ngo 1.27\n")
	src := "package main\n\nimport _ \"example.com/dep\"\n\nfunc main() {}\n"
	f := filepath.Join(modDir, "main.go")
	if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// The import is not required by go.mod: ExportFiles must fail, and
	// the error must name the cause (not the generic scope error).
	_, err := ExportFiles([]string{f}, "")
	if err == nil {
		t.Fatal("ExportFiles with unrequired import: no error")
	}
	if !strings.Contains(err.Error(), "go.mod") {
		t.Fatalf("error %q does not name go.mod", err)
	}
}

func TestExportFilesModuleImportPinned(t *testing.T) {
	resetDiskGlobals(t)
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	depDir := writeCacheMod(t, cache, "example.com/dep", "v1.0.0", "")
	if err := os.WriteFile(filepath.Join(depDir, "dep.go"), []byte("package dep\n\nfunc Hi() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A newer version exists in the cache but is NOT the required one;
	// the export must compile against v1.0.0.
	writeCacheMod(t, cache, "example.com/dep", "v2.0.0", "")
	modDir := writeModule(t, filepath.Join(root, "main"), "module example.com/main\n\ngo 1.27\n\nrequire example.com/dep v1.0.0\n")
	f := filepath.Join(modDir, "main.go")
	if err := os.WriteFile(f, []byte("package main\n\nimport \"example.com/dep\"\n\nvar X = dep.Hi\n\nfunc main() { println(X) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diskFlagModCache = cache
	out, err := ExportFiles([]string{f}, "")
	if err != nil {
		t.Fatalf("ExportFiles: %v", err)
	}
	if !strings.Contains(out, "dep.Hi") && !strings.Contains(out, "Hi") {
		t.Fatalf("export output does not mention dep.Hi:\n%s", out[:min(500, len(out))])
	}
}

func TestExportFilesMalformedGoModFailsLoud(t *testing.T) {
	resetDiskGlobals(t)
	root := t.TempDir()
	modDir := writeModule(t, filepath.Join(root, "main"), "module example.com/main\n\nrequire (\n")
	f := filepath.Join(modDir, "main.go")
	if err := os.WriteFile(f, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportFiles([]string{f}, ""); err == nil {
		t.Fatal("ExportFiles with malformed go.mod: no error")
	} else if !strings.Contains(err.Error(), "go.mod") {
		t.Fatalf("error %q does not name go.mod", err)
	}
}

func TestBuildTagsFlagBeatsEnv(t *testing.T) {
	resetDiskGlobals(t)
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	depDir := writeCacheMod(t, cache, "example.com/dep", "v1.0.0", "")
	if err := os.WriteFile(filepath.Join(depDir, "b.go"), []byte("package dep\n\nfunc B() int { return 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "a_tagged.go"), []byte("//go:build mytag\n\npackage dep\n\nfunc A() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	modDir := writeModule(t, filepath.Join(root, "main"), "module example.com/main\n\nrequire example.com/dep v1.0.0\n")
	ctx, err := loadModuleContext(modDir)
	if err != nil {
		t.Fatal(err)
	}
	diskFlagModCache = cache
	lookup := func() (a, b bool) {
		sp, err := getDiskPackage("example.com/dep")
		if err != nil {
			t.Fatal(err)
		}
		return sp.pkg.Scope().Lookup("A") != nil, sp.pkg.Scope().Lookup("B") != nil
	}
	// Env tag on, flag absent: tagged file compiles.
	t.Setenv("GRAALGO_BUILD_TAGS", "mytag")
	setModuleContext(nil)
	setModuleContext(ctx)
	if a, b := lookup(); !a || !b {
		t.Fatalf("env tags: A=%v B=%v, want both", a, b)
	}
	// Explicit empty flag beats the env var: tagged file excluded.
	setDiskFlags(cache, "", []string{}, true)
	setModuleContext(nil)
	setModuleContext(ctx)
	if a, b := lookup(); a || !b {
		t.Fatalf("explicit empty flag: A=%v B=%v, want A=false B=true", a, b)
	}
	// Explicit flag with the tag: tagged file compiles.
	setDiskFlags(cache, "", []string{"mytag"}, true)
	setModuleContext(nil)
	setModuleContext(ctx)
	if a, b := lookup(); !a || !b {
		t.Fatalf("flag tags: A=%v B=%v, want both", a, b)
	}
}
