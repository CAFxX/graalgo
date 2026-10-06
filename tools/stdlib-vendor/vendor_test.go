package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGoroot builds a minimal fake GOROOT tree: VERSION + src files.
func fakeGoroot(t *testing.T, version string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte(version+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, body := range files {
		p := filepath.Join(root, "src", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

var stdFiles = map[string]string{
	"a.go":       "package a\n",
	"pkg/b.go":   "package b\n",
	"pkg/c_test": "package b\n",
}

func vendorOptsFor(t *testing.T, goroot, target string) vendorOptions {
	t.Helper()
	work := t.TempDir()
	return vendorOptions{
		Goroot:       goroot,
		Target:       target,
		ManifestPath: filepath.Join(work, "vendor-manifest.json"),
		PinPath:      filepath.Join(work, "pinned-version.txt"),
	}
}

func readManifest(t *testing.T, path string) manifest {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// (a) Two consecutive vendor runs produce byte-identical manifests.
func TestVendorManifestDeterministic(t *testing.T) {
	goroot := fakeGoroot(t, "go1.27.1", stdFiles)
	target := filepath.Join(t.TempDir(), "vendored")
	o := vendorOptsFor(t, goroot, target)

	var log1, log2 bytes.Buffer
	if err := vendorTree(o, &log1); err != nil {
		t.Fatalf("first vendor: %v", err)
	}
	o.Force = true
	if err := vendorTree(o, &log2); err != nil {
		t.Fatalf("second vendor: %v", err)
	}
	m1, err := os.ReadFile(o.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	// Re-run once more and compare again to rule out single-fluke equality.
	o2 := o
	if err := vendorTree(o2, &log2); err != nil {
		t.Fatalf("third vendor: %v", err)
	}
	m2, err := os.ReadFile(o.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(m1, m2) {
		t.Fatalf("manifests differ across consecutive runs:\n--- run1 ---\n%s\n--- run2 ---\n%s", m1, m2)
	}
	m := readManifest(t, o.ManifestPath)
	if m.GoVersion != "go1.27.1" {
		t.Fatalf("manifest goVersion = %q", m.GoVersion)
	}
	// Entries must be sorted by path.
	for i := 1; i < len(m.Files); i++ {
		if m.Files[i-1].Path >= m.Files[i].Path {
			t.Fatalf("manifest entries not sorted: %q >= %q", m.Files[i-1].Path, m.Files[i].Path)
		}
	}
	if len(m.Files) != len(stdFiles) {
		t.Fatalf("manifest has %d files, want %d", len(m.Files), len(stdFiles))
	}
}

// First run creates the pin; later runs reuse it.
func TestPinCreatedOnFirstRun(t *testing.T) {
	goroot := fakeGoroot(t, "go1.27.1", stdFiles)
	target := filepath.Join(t.TempDir(), "vendored")
	o := vendorOptsFor(t, goroot, target)
	var log bytes.Buffer
	if err := vendorTree(o, &log); err != nil {
		t.Fatalf("vendor: %v", err)
	}
	raw, err := os.ReadFile(o.PinPath)
	if err != nil {
		t.Fatalf("pin not created: %v", err)
	}
	if strings.TrimSpace(string(raw)) != "go1.27.1" {
		t.Fatalf("pin = %q", raw)
	}
}

// (b) verify passes on a clean tree, fails after a one-byte tamper.
func TestVerifyDetectsTamper(t *testing.T) {
	goroot := fakeGoroot(t, "go1.27.1", stdFiles)
	target := filepath.Join(t.TempDir(), "vendored")
	o := vendorOptsFor(t, goroot, target)
	var log bytes.Buffer
	if err := vendorTree(o, &log); err != nil {
		t.Fatalf("vendor: %v", err)
	}
	vo := verifyOptions{Target: target, ManifestPath: o.ManifestPath}
	var out bytes.Buffer
	if _, err := verifyTree(vo, &out); err != nil {
		t.Fatalf("verify on clean tree failed: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "VERIFY OK") {
		t.Fatalf("clean verify output missing VERIFY OK:\n%s", out.String())
	}

	// Tamper: flip one byte in one vendored file.
	victim := filepath.Join(target, "pkg", "b.go")
	f, err := os.OpenFile(victim, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("// tampered\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	out.Reset()
	rep, err := verifyTree(vo, &out)
	if err == nil {
		t.Fatalf("verify passed after tampering with pkg/b.go")
	}
	if rep == nil || len(rep.modified) != 1 || rep.modified[0] != "pkg/b.go" {
		t.Fatalf("expected exactly modified=[pkg/b.go], got %+v\n%s", rep, out.String())
	}
	if !strings.Contains(out.String(), "MODIFIED pkg/b.go") {
		t.Fatalf("output missing MODIFIED line:\n%s", out.String())
	}
}

func TestVerifyDetectsMissingAndExtra(t *testing.T) {
	goroot := fakeGoroot(t, "go1.27.1", stdFiles)
	target := filepath.Join(t.TempDir(), "vendored")
	o := vendorOptsFor(t, goroot, target)
	var log bytes.Buffer
	if err := vendorTree(o, &log); err != nil {
		t.Fatalf("vendor: %v", err)
	}
	if err := os.Remove(filepath.Join(target, "a.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "stowaway.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	vo := verifyOptions{Target: target, ManifestPath: o.ManifestPath}
	var out bytes.Buffer
	rep, err := verifyTree(vo, &out)
	if err == nil {
		t.Fatalf("verify passed with missing+extra files")
	}
	if len(rep.missing) != 1 || rep.missing[0] != "a.go" {
		t.Fatalf("missing = %v", rep.missing)
	}
	if len(rep.extra) != 1 || rep.extra[0] != "stowaway.txt" {
		t.Fatalf("extra = %v", rep.extra)
	}
}

// (c) A GOROOT whose version differs from the pin is refused without
// --allow-version-drift, accepted with it.
func TestVersionDriftRefused(t *testing.T) {
	goroot127 := fakeGoroot(t, "go1.27.1", stdFiles)
	target := filepath.Join(t.TempDir(), "vendored")
	o := vendorOptsFor(t, goroot127, target)
	var log bytes.Buffer
	if err := vendorTree(o, &log); err != nil {
		t.Fatalf("first vendor: %v", err)
	}

	goroot128 := fakeGoroot(t, "go1.28.0", stdFiles)
	o2 := vendorOptsFor(t, goroot128, filepath.Join(t.TempDir(), "vendored2"))
	// Share the pin from the first run.
	o2.PinPath = o.PinPath
	o2.ManifestPath = o.ManifestPath
	o2.Force = true
	err := vendorTree(o2, &log)
	if !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("expected ErrVersionMismatch, got %v", err)
	}

	o2.AllowVersionDrift = true
	if err := vendorTree(o2, &log); err != nil {
		t.Fatalf("vendor with --allow-version-drift: %v", err)
	}
	// The pin must NOT move on a drift run — updating the pin is a
	// deliberate step (see VENDORING.md).
	raw, _ := os.ReadFile(o2.PinPath)
	if strings.TrimSpace(string(raw)) != "go1.27.1" {
		t.Fatalf("drift run moved the pin: %q", raw)
	}
	m := readManifest(t, o2.ManifestPath)
	if !m.VersionDrift {
		t.Fatalf("manifest does not record the drift run")
	}
}

// (d) Vendoring into an existing non-empty target without --force is refused.
func TestExistingTargetRefused(t *testing.T) {
	goroot := fakeGoroot(t, "go1.27.1", stdFiles)
	target := filepath.Join(t.TempDir(), "vendored")
	o := vendorOptsFor(t, goroot, target)
	var log bytes.Buffer
	if err := vendorTree(o, &log); err != nil {
		t.Fatalf("first vendor: %v", err)
	}
	o2 := vendorOptsFor(t, goroot, target)
	o2.ManifestPath = o.ManifestPath
	o2.PinPath = o.PinPath
	err := vendorTree(o2, &log)
	if !errors.Is(err, ErrTargetExists) {
		t.Fatalf("expected ErrTargetExists, got %v", err)
	}
	o2.Force = true
	if err := vendorTree(o2, &log); err != nil {
		t.Fatalf("vendor with --force: %v", err)
	}
}

// Malformed VERSION files are refused, not vendored.
func TestMalformedVersionRefused(t *testing.T) {
	goroot := fakeGoroot(t, "not-a-version", stdFiles)
	o := vendorOptsFor(t, goroot, filepath.Join(t.TempDir(), "vendored"))
	var log bytes.Buffer
	if err := vendorTree(o, &log); err == nil {
		t.Fatalf("vendored a GOROOT with a malformed VERSION")
	}
}

// Manifest entries carry real SHA-256 digests, not placeholders.
func TestManifestHashesAreReal(t *testing.T) {
	goroot := fakeGoroot(t, "go1.27.1", stdFiles)
	target := filepath.Join(t.TempDir(), "vendored")
	o := vendorOptsFor(t, goroot, target)
	var log bytes.Buffer
	if err := vendorTree(o, &log); err != nil {
		t.Fatalf("vendor: %v", err)
	}
	m := readManifest(t, o.ManifestPath)
	for _, e := range m.Files {
		if len(e.SHA256) != 64 {
			t.Fatalf("entry %q has non-SHA-256 digest %q", e.Path, e.SHA256)
		}
		got, err := hashFile(filepath.Join(target, filepath.FromSlash(e.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if got != e.SHA256 {
			t.Fatalf("entry %q digest mismatch", e.Path)
		}
	}
}
