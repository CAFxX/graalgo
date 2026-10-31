package main

// Regression tests for the L1 veto fix-up (review-bytes-mr, commit
// f62a267f vetoed):
//
// F1 (P0): a fail-fast synthetic multi-result method in method-value
// position (t.Clock, t.Zone, t.Date) leaked past the stub policy: the
// dummy methodIndex entry carries funcID 0, so ExportFile emitted
// methodvalue nodes with obj = 0 (main's funcdecl in that doc) — a
// silent misdispatch at guest runtime, worse than the named runtime
// panic the pre-lane code produced. These tests pin the export-time
// refusal.
//
// F2 (P1): a bodyless disk multi-result method called through an
// interface dispatched to its panic helper at guest runtime, because
// the intrinsic gate was missing from the interface-dispatch path.
// TestDiskMRIntrinsicGateIfaceExport drives a seeded bodyless disk
// spec through an interface call to export failure.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVetoFixMethodValueFailFastStubs: fail-fast synthetic
// multi-result methods must fail at export in method-value position,
// exactly as they already do in direct-call position.
func TestVetoFixMethodValueFailFastStubs(t *testing.T) {
	for _, name := range []string{"Clock", "Zone", "Date"} {
		t.Run(name, func(t *testing.T) {
			src := `package main

import "time"

func main() {
	t := time.Now()
	_ = t.` + name + `
}
`
			_, err := ExportFile(writeIfaceProg(t, src))
			if err == nil {
				t.Fatalf("ExportFile succeeded for time.Time.%s in method-value position, want export-time failure (stub-policy leak: obj 0 misdispatch)", name)
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("error = %q, want it to name the method %s", err.Error(), name)
			}
		})
	}
}

// TestVetoFixMethodValueControl: a real (non-stub) method value still
// exports — the F1 gate must refuse only fail-fast stubs, not every
// method value.
func TestVetoFixMethodValueControl(t *testing.T) {
	src := `package main

import "time"

func main() {
	t := time.Now()
	_ = t.Unix
}
`
	if _, err := ExportFile(writeIfaceProg(t, src)); err != nil {
		t.Fatalf("ExportFile failed for time.Time.Unix method value, want success: %v", err)
	}
}

// seedVetofixDiskPackage writes src as a real on-disk package and
// seeds it into diskDirCache, so the import gate and the overlay
// importer resolve it exactly as a genuine disk package (the transform
// then runs on it via buildDiskPackage). Bodyless method declarations
// in src are intentionally bodyless; the transform rewrites them to a
// panic-helper + stub pair before type-checking.
func seedVetofixDiskPackage(t *testing.T, pkgPath, src string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	diskMu.Lock()
	oldDirCache := diskDirCache
	newDirCache := map[string]string{pkgPath: dir}
	for k, v := range oldDirCache {
		newDirCache[k] = v
	}
	diskDirCache = newDirCache
	oldCache := diskCache
	_, wasCached := diskCache[pkgPath]
	delete(diskCache, pkgPath)
	oldSpecs := diskMRSpecs
	diskMu.Unlock()
	t.Cleanup(func() {
		diskMu.Lock()
		diskDirCache = oldDirCache
		diskCache = oldCache
		if !wasCached {
			// oldCache is the same map object the export repopulated.
			delete(diskCache, pkgPath)
		}
		diskMRSpecs = oldSpecs
		diskMu.Unlock()
	})
}

// TestDiskMRIntrinsicGateIfaceExport: a bodyless disk multi-result
// method called through an interface of its own package must fail at
// export when its required intrinsic is unprovided — it must not emit
// an ifacemethod node whose itab targets the panic helper.
//
// transformDiskMultiResult records requiresIntrinsic on T's spec only;
// the interface spec carries none — so the gate refuses the interface
// call because dispatch could land on T's panic helper, not because
// the interface spec itself is gated.
func TestDiskMRIntrinsicGateIfaceExport(t *testing.T) {
	const pkgPath = "example.com/vetofix-iface-p"
	seedVetofixDiskPackage(t, pkgPath, `package p

import "unsafe"

//go:noescape
func (t *T) Foo(x unsafe.Pointer) (int, error)

type T struct{}

type Iface interface {
	Foo(x unsafe.Pointer) (int, error)
}
`)

	mainSrc := `package main

import (
	p "example.com/vetofix-iface-p"
	"unsafe"
)

func main() {
	var v p.Iface = &p.T{}
	_, _ = v.Foo(unsafe.Pointer(nil))
}
`
	_, err := ExportFile(writeIfaceProg(t, mainSrc))
	if err == nil {
		t.Fatal("ExportFile succeeded for interface call to bodyless disk method with unprovided intrinsic, want export-time failure (panic-helper dispatch)")
	}
	if !strings.Contains(err.Error(), "Foo") {
		t.Fatalf("error = %q, want it to name the method Foo", err.Error())
	}
}

// TestVetoFixDiskMethodValueFailFast: the same bodyless disk method in
// method-value position must also fail at export. Pre-fix, the probe
// emitted a "methodvalue" node whose obj had no funcdecl in the doc
// (a dangling reference, not even the panic helper) — the disk
// intrinsic gate did not cover value positions at all.
func TestVetoFixDiskMethodValueFailFast(t *testing.T) {
	const pkgPath = "example.com/vetofix-mv-p"
	seedVetofixDiskPackage(t, pkgPath, `package p

import "unsafe"

//go:noescape
func (t *T) Foo(x unsafe.Pointer) (int, error)

type T struct{}
`)

	mainSrc := `package main

import p "example.com/vetofix-mv-p"

func main() {
	t := &p.T{}
	_ = t.Foo
}
`
	_, err := ExportFile(writeIfaceProg(t, mainSrc))
	if err == nil {
		t.Fatal("ExportFile succeeded for bodyless disk method in method-value position, want export-time failure (dangling methodvalue obj)")
	}
	if !strings.Contains(err.Error(), "Foo") {
		t.Fatalf("error = %q, want it to name the method Foo", err.Error())
	}
}
