package main

// REVIEW-2026-11-03-16, P1-S6/F4: disk imports must resolve through the
// importing module's go.mod. These tests pin a fake module cache
// (GOMODCACHE) and a fake importing module (go.mod + go.sum) and assert:
// the pinned (older) version wins over a newer cached version, a pinned
// version absent from the cache fails loudly (and is not the
// errDiskNotFound scope error), go.sum entry presence and download-hash
// agreement are enforced, local replaces resolve, and the semver
// comparison orders pseudo-versions correctly. The ExportFiles tests
// prove the same behaviour end to end through the real export path,
// where a wrong-version pick would fail type-checking (the older
// version exports a symbol the newer one lacks) and a missing pin
// must surface as the pinned error, not the scope error.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// cachedModuleDir creates $GOMODCACHE/<module>@<version>/ with the given
// files (relative paths -> contents) and returns the module directory.
func cachedModuleDir(t *testing.T, modCache, modulePath, version string, files map[string]string) string {
	t.Helper()
	elems := strings.Split(modulePath, "/")
	last := elems[len(elems)-1] + "@" + version
	dir := filepath.Join(append([]string{modCache}, append(elems[:len(elems)-1], last)...)...)
	for rel, content := range files {
		writeTestFile(t, filepath.Join(dir, rel), content)
	}
	return dir
}

func mainModuleDir(t *testing.T, goMod, goSum string) string {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), goMod)
	if goSum != "" {
		writeTestFile(t, filepath.Join(dir, "go.sum"), goSum)
	}
	return dir
}

func setModuleContext(t *testing.T, dir string) {
	t.Helper()
	restore, err := setDiskModuleContextFromDir(dir)
	if err != nil {
		t.Fatalf("setDiskModuleContextFromDir(%s): %v", dir, err)
	}
	t.Cleanup(restore)
}

func TestDiskImportPinnedOlderVersionWins(t *testing.T) {
	modCache := t.TempDir()
	t.Setenv("GOMODCACHE", modCache)
	cachedModuleDir(t, modCache, "example.com/dep", "v1.0.0", map[string]string{
		"dep.go":     "package dep\n\nfunc OldOnly() {}\n",
		"sub/sub.go": "package sub\n\nfunc Sub() {}\n",
	})
	cachedModuleDir(t, modCache, "example.com/dep", "v2.0.0", map[string]string{
		"dep.go": "package dep\n\nfunc NewOnly() {}\n",
	})
	modDir := mainModuleDir(t, `module example.com/main

go 1.21

require (
	example.com/dep v1.0.0
)
`, "example.com/dep v1.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n")
	setModuleContext(t, modDir)

	dir, err := resolveDiskPackageDir("example.com/dep")
	if err != nil {
		t.Fatalf("resolveDiskPackageDir: %v", err)
	}
	if !strings.HasSuffix(dir, "dep@v1.0.0") {
		t.Fatalf("resolved to %s, want the pinned v1.0.0 directory", dir)
	}
	sub, err := resolveDiskPackageDir("example.com/dep/sub")
	if err != nil {
		t.Fatalf("resolveDiskPackageDir(sub): %v", err)
	}
	if !strings.HasSuffix(sub, filepath.Join("dep@v1.0.0", "sub")) {
		t.Fatalf("subpackage resolved to %s, want dep@v1.0.0/sub", sub)
	}
}

func TestDiskImportMissingPinnedVersionFailsLoudly(t *testing.T) {
	modCache := t.TempDir()
	t.Setenv("GOMODCACHE", modCache)
	cachedModuleDir(t, modCache, "example.com/dep", "v2.0.0", map[string]string{
		"dep.go": "package dep\n\nfunc NewOnly() {}\n",
	})
	modDir := mainModuleDir(t, `module example.com/main

go 1.21

require example.com/dep v1.5.0
`, "example.com/dep v1.5.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n")
	setModuleContext(t, modDir)

	dir, err := resolveDiskPackageDir("example.com/dep")
	if err == nil {
		t.Fatalf("resolveDiskPackageDir succeeded with %s, want a loud failure for the absent pinned v1.5.0", dir)
	}
	if errors.Is(err, errDiskNotFound) {
		t.Fatalf("missing-pin error wraps errDiskNotFound (%v); the importer would degrade it to a scope error", err)
	}
	if !strings.Contains(err.Error(), "v1.5.0") || !strings.Contains(err.Error(), "go.mod") {
		t.Fatalf("missing-pin error %q does not name the pinned version and its go.mod", err)
	}
}

func TestDiskImportPinnedGoSumChecks(t *testing.T) {
	modCache := t.TempDir()
	t.Setenv("GOMODCACHE", modCache)
	cachedModuleDir(t, modCache, "example.com/dep", "v1.0.0", map[string]string{
		"dep.go": "package dep\n\nfunc OldOnly() {}\n",
	})
	// go.sum names a different module only: the pinned entry is missing.
	modDir := mainModuleDir(t, `module example.com/main

go 1.21

require example.com/dep v1.0.0
`, "example.com/other v3.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n")
	setModuleContext(t, modDir)
	if _, err := resolveDiskPackageDir("example.com/dep"); err == nil || !strings.Contains(err.Error(), "go.sum") {
		t.Fatalf("missing go.sum entry: got err %v, want an error naming go.sum", err)
	}

	// Entry present but disagreeing with the cache download hash.
	modDir2 := mainModuleDir(t, `module example.com/main

go 1.21

require example.com/dep v1.0.0
`, "example.com/dep v1.0.0 h1:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=\n")
	writeTestFile(t, filepath.Join(modCache, "cache", "download", "example.com", "dep", "@v", "v1.0.0.ziphash"), "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n")
	setModuleContext(t, modDir2)
	if _, err := resolveDiskPackageDir("example.com/dep"); err == nil || !strings.Contains(err.Error(), "disagrees") {
		t.Fatalf("ziphash mismatch: got err %v, want an error about disagreement", err)
	}

	// Positive control: matching download hash resolves.
	writeTestFile(t, filepath.Join(modCache, "cache", "download", "example.com", "dep", "@v", "v1.0.0.ziphash"), "h1:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=\n")
	dir, err := resolveDiskPackageDir("example.com/dep")
	if err != nil {
		t.Fatalf("matching ziphash should resolve, got %v", err)
	}
	if !strings.HasSuffix(dir, "dep@v1.0.0") {
		t.Fatalf("resolved to %s, want dep@v1.0.0", dir)
	}
}

func TestDiskImportReplaceLocalDirWins(t *testing.T) {
	modCache := t.TempDir()
	t.Setenv("GOMODCACHE", modCache)
	cachedModuleDir(t, modCache, "example.com/dep", "v2.0.0", map[string]string{
		"dep.go": "package dep\n\nfunc NewOnly() {}\n",
	})
	modDir := mainModuleDir(t, `module example.com/main

go 1.21

require example.com/dep v1.0.0

replace example.com/dep => ./localdep
`, "")
	writeTestFile(t, filepath.Join(modDir, "localdep", "dep.go"), "package dep\n\nfunc OldOnly() {}\n")
	setModuleContext(t, modDir)

	dir, err := resolveDiskPackageDir("example.com/dep")
	if err != nil {
		t.Fatalf("resolveDiskPackageDir: %v", err)
	}
	if dir != filepath.Join(modDir, "localdep") {
		t.Fatalf("resolved to %s, want the local replace directory", dir)
	}
}

// Veto fix B2: a go.sum that exists but cannot be read must fail loudly,
// never silently downgrade to the sum:absent (no-verification) path.
// A go.sum that is a directory is unreadable on every platform and for
// every user (including root, for whom chmod 000 is still readable),
// so it exercises the non-NotExist read-error branch deterministically.
func TestDiskImportUnreadableGoSumFailsLoudly(t *testing.T) {
	modDir := mainModuleDir(t, `module example.com/main

go 1.21

require example.com/dep v1.0.0
`, "")
	if err := os.Mkdir(filepath.Join(modDir, "go.sum"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := setDiskModuleContextFromDir(modDir)
	if err == nil {
		t.Fatalf("unreadable go.sum (a directory) was silently treated as absent")
	}
	if !strings.Contains(err.Error(), "go.sum") {
		t.Fatalf("unreadable-go.sum error %q does not name go.sum", err)
	}
}

// Same B2 branch via permissions; skipped where the test user can still
// read the file (e.g. root), since chmod 000 does not block root reads.
func TestDiskImportPermissionDeniedGoSumFailsLoudly(t *testing.T) {
	modDir := mainModuleDir(t, `module example.com/main

go 1.21

require example.com/dep v1.0.0
`, "example.com/dep v1.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n")
	sumPath := filepath.Join(modDir, "go.sum")
	if err := os.Chmod(sumPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(sumPath, 0o644) })
	if _, err := os.ReadFile(sumPath); err == nil {
		t.Skip("test user can still read a chmod-000 go.sum (e.g. running as root); the directory-based test above covers the branch")
	}
	_, err := setDiskModuleContextFromDir(modDir)
	if err == nil {
		t.Fatalf("permission-denied go.sum was silently treated as absent")
	}
	if !strings.Contains(err.Error(), "go.sum") {
		t.Fatalf("permission-denied go.sum error %q does not name go.sum", err)
	}
}

// P2: a go.sum carrying only a go.mod hash for the pinned module must
// not yield the misleading "disagrees with the download hash" error:
// a go.mod hash attests no module zip, so it can neither agree nor
// disagree with the cache ziphash.
func TestDiskImportGoModOnlyGoSumEntryIsNotDisagreement(t *testing.T) {
	modCache := t.TempDir()
	t.Setenv("GOMODCACHE", modCache)
	cachedModuleDir(t, modCache, "example.com/dep", "v1.0.0", map[string]string{
		"dep.go": "package dep\n\nfunc OldOnly() {}\n",
	})
	writeTestFile(t, filepath.Join(modCache, "cache", "download", "example.com", "dep", "@v", "v1.0.0.ziphash"), "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n")
	modDir := mainModuleDir(t, `module example.com/main

go 1.21

require example.com/dep v1.0.0
`, "example.com/dep v1.0.0/go.mod h1:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=\n")
	setModuleContext(t, modDir)
	_, err := resolveDiskPackageDir("example.com/dep")
	if err == nil {
		t.Fatalf("go.mod-only go.sum entry resolved as if verified")
	}
	if strings.Contains(err.Error(), "disagrees") {
		t.Fatalf("go.mod-only entry yielded a disagreement error: %v", err)
	}
	if !strings.Contains(err.Error(), "go.mod hash") {
		t.Fatalf("go.mod-only entry error %q does not explain the missing module hash", err)
	}
}

// P2: the resolved cache root is part of the disk cache key, so the
// same go.mod resolving against a different GOMODCACHE cannot hit
// entries resolved from the old cache.
func TestDiskImportCacheKeyIncludesCacheRoot(t *testing.T) {
	modDir := mainModuleDir(t, `module example.com/main

go 1.21

require example.com/dep v1.0.0
`, "")
	ctx, err := loadGoModContext(modDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOMODCACHE", "/tmp/cache-a")
	keyA := diskCacheKey(ctx, "example.com/dep")
	t.Setenv("GOMODCACHE", "/tmp/cache-b")
	keyB := diskCacheKey(ctx, "example.com/dep")
	if keyA == keyB {
		t.Fatalf("diskCacheKey identical across cache roots: %q", keyA)
	}
	if keyA == diskCacheKey(nil, "example.com/dep") {
		t.Fatalf("context key collided with the no-context key")
	}
}

// Concurrency proof (veto B1 question): two concurrent ExportFiles from
// different importing modules pinning different versions of the same
// dependency must each resolve under their own module's pins. The
// rendezvous gate forces the overlap deterministically: both exports
// install their context, then both wait at the gate, then both resolve.
// Without ExportFiles serialization the second install wins, so the
// first export resolves under the other's pins and fails type-checking
// (OldOnly is absent from v2.0.0) — verified by running this test with
// the mutex removed. With serialization the second export blocks at
// the mutex and never reaches the gate, so the first proceeds alone
// after its gate timeout and both succeed. The shared cache root is
// deliberate: GOMODCACHE is process-global env, so concurrent exports
// in one process always share it; what differs is the go.mod.
func TestDiskImportConcurrentExportsKeepOwnPins(t *testing.T) {
	modCache := t.TempDir()
	t.Setenv("GOMODCACHE", modCache)
	cachedModuleDir(t, modCache, "example.com/dep", "v1.0.0", map[string]string{
		"dep.go": "package dep\n\nfunc OldOnly() {}\n",
	})
	cachedModuleDir(t, modCache, "example.com/dep", "v2.0.0", map[string]string{
		"dep.go": "package dep\n\nfunc NewOnly() {}\n",
	})
	modA := mainModuleDir(t, `module example.com/maina

go 1.21

require example.com/dep v1.0.0
`, "example.com/dep v1.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n")
	mainA := filepath.Join(modA, "main.go")
	writeTestFile(t, mainA, "package main\n\nimport \"example.com/dep\"\n\nfunc main() { dep.OldOnly() }\n")
	modB := mainModuleDir(t, `module example.com/mainb

go 1.21

require example.com/dep v2.0.0
`, "example.com/dep v2.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n")
	mainB := filepath.Join(modB, "main.go")
	writeTestFile(t, mainB, "package main\n\nimport \"example.com/dep\"\n\nfunc main() { dep.NewOnly() }\n")
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	go func() {
		// Release once both exports reach the gate; if only one ever
		// arrives (serialized case) the gate's own timeout releases it,
		// and this coordinator's timeout closes the gate for the late
		// export instead of leaking a goroutine.
		n := 0
		timeout := time.After(3 * time.Second)
		for n < 2 {
			select {
			case <-arrived:
				n++
			case <-timeout:
				close(release)
				return
			}
		}
		close(release)
	}()
	exportTestGate = func() {
		arrived <- struct{}{}
		select {
		case <-release:
		case <-time.After(2 * time.Second):
		}
	}
	t.Cleanup(func() { exportTestGate = nil })
	errs := make(chan error, 2)
	go func() { _, err := ExportFiles([]string{mainA}, ""); errs <- err }()
	go func() { _, err := ExportFiles([]string{mainB}, ""); errs <- err }()
	for j := 0; j < 2; j++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent export failed: %v", err)
		}
	}
}

func TestDiskImportMalformedGoModFailsLoudly(t *testing.T) {
	modDir := mainModuleDir(t, `module example.com/main

go 1.21

require example.com/dep
`, "")
	if _, err := setDiskModuleContextFromDir(modDir); err == nil {
		t.Fatalf("malformed go.mod (require without version) was accepted silently")
	}
}

func TestCompareSemverPseudoVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		// Pseudo-version ranks below its own release.
		{"v1.2.3-0.20240101120000-abcdefabcdef", "v1.2.3", -1},
		{"v1.2.3", "v1.2.3-0.20240101120000-abcdefabcdef", 1},
		// Core still dominates the prerelease.
		{"v1.2.3-0.20240101120000-abcdefabcdef", "v1.2.4", -1},
		{"v0.0.0-20240101120000-abcdefabcdef", "v1.0.0", -1},
		// Semver prerelease ordering (spec §11 examples).
		{"v1.0.0-alpha", "v1.0.0-alpha.1", -1},
		{"v1.0.0-alpha.1", "v1.0.0-alpha.beta", -1},
		{"v1.0.0-alpha.beta", "v1.0.0-beta", -1},
		{"v1.0.0-beta", "v1.0.0-beta.2", -1},
		{"v1.0.0-beta.2", "v1.0.0-beta.11", -1},
		{"v1.0.0-beta.11", "v1.0.0-rc.1", -1},
		{"v1.0.0-rc.1", "v1.0.0", -1},
		// Numeric prerelease identifiers compare numerically, and
		// rank below alphanumeric ones.
		{"v1.0.0-2", "v1.0.0-11", -1},
		{"v1.0.0-1", "v1.0.0-alpha", -1},
		// Build metadata is ignored.
		{"v1.0.0+build", "v1.0.0", 0},
		{"v1.0.0", "v1.0.0", 0},
		{"v2.0.0", "v1.9.9", 1},
	}
	for _, c := range cases {
		if got := compareSemver(c.a, c.b); got != c.want {
			t.Errorf("compareSemver(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestDiskImportPinnedVersionEndToEnd(t *testing.T) {
	modCache := t.TempDir()
	t.Setenv("GOMODCACHE", modCache)
	cachedModuleDir(t, modCache, "example.com/dep", "v1.0.0", map[string]string{
		"dep.go": "package dep\n\nfunc OldOnly() {}\n",
	})
	cachedModuleDir(t, modCache, "example.com/dep", "v2.0.0", map[string]string{
		"dep.go": "package dep\n\nfunc NewOnly() {}\n",
	})
	modDir := mainModuleDir(t, `module example.com/main

go 1.21

require example.com/dep v1.0.0
`, "example.com/dep v1.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n")
	mainFile := filepath.Join(modDir, "main.go")
	writeTestFile(t, mainFile, "package main\n\nimport \"example.com/dep\"\n\nfunc main() { dep.OldOnly() }\n")
	// OldOnly exists only in v1.0.0: success proves the pinned older
	// version won; the legacy highest-version pick (v2.0.0) would fail
	// type-checking here.
	if _, err := ExportFiles([]string{mainFile}, ""); err != nil {
		t.Fatalf("ExportFiles with pinned v1.0.0: %v", err)
	}
}

func TestDiskImportMissingPinnedVersionEndToEndFailsLoudly(t *testing.T) {
	modCache := t.TempDir()
	t.Setenv("GOMODCACHE", modCache)
	cachedModuleDir(t, modCache, "example.com/dep", "v2.0.0", map[string]string{
		"dep.go": "package dep\n\nfunc NewOnly() {}\n",
	})
	modDir := mainModuleDir(t, `module example.com/main

go 1.21

require example.com/dep v1.5.0
`, "example.com/dep v1.5.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n")
	mainFile := filepath.Join(modDir, "main.go")
	writeTestFile(t, mainFile, "package main\n\nimport \"example.com/dep\"\n\nfunc main() { dep.NewOnly() }\n")
	_, err := ExportFiles([]string{mainFile}, "")
	if err == nil {
		t.Fatalf("ExportFiles succeeded despite the absent pinned v1.5.0")
	}
	if strings.Contains(err.Error(), "not supported in frontend/v1 scope") {
		t.Fatalf("missing pin was masked as a scope error: %v", err)
	}
	if !strings.Contains(err.Error(), "v1.5.0") {
		t.Fatalf("ExportFiles error %q does not name the pinned v1.5.0", err)
	}
}
