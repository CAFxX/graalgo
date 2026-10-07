package p3frontend

// P3-M7 (wave 1): p3frontend drift tests for the synthetic "sort"
// package. The importer reimplements the frozen exporter's synthetic
// signatures (it cannot reuse them — unexported); these tests pin the
// mirror against the REAL phase-1 synthetic source so drift fails fast.

import (
	"go/types"
	"testing"
)

// methodSigs collects a named type's method set as name -> rendered
// signature (package-qualified), for drift comparison.
func methodSigs(t *testing.T, pkgName string, named *types.Named) map[string]string {
	t.Helper()
	qual := func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Name()
	}
	out := map[string]string{}
	for i := 0; i < named.NumMethods(); i++ {
		m := named.Method(i)
		sig, ok := m.Type().(*types.Signature)
		if !ok {
			t.Fatalf("%s: method %s is not a signature", pkgName, m.Name())
		}
		out[m.Name()] = types.TypeString(sig, qual)
	}
	return out
}

// ifaceMethodSigs collects an interface type's method set the same way.
func ifaceMethodSigs(t *testing.T, pkgName string, iface *types.Interface) map[string]string {
	t.Helper()
	qual := func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Name()
	}
	out := map[string]string{}
	for i := 0; i < iface.NumMethods(); i++ {
		m := iface.Method(i)
		sig, ok := m.Type().(*types.Signature)
		if !ok {
			t.Fatalf("%s: interface method %s is not a signature", pkgName, m.Name())
		}
		out[m.Name()] = types.TypeString(sig, qual)
	}
	return out
}

// namedOf looks up a package-level named type.
func namedOf(t *testing.T, pkg *types.Package, name string) *types.Named {
	t.Helper()
	obj := pkg.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s.%s missing", pkg.Name(), name)
	}
	named, ok := obj.Type().(*types.Named)
	if !ok {
		t.Fatalf("%s.%s is %T, want *types.Named", pkg.Name(), name, obj.Type())
	}
	return named
}

// TestSortSyntheticSignatures pins the mirrored synthetic sort package
// against the phase-1 source: the Interface method set, the three
// convenience slice types (underlying + methods), and every
// package-level function's signature.
func TestSortSyntheticSignatures(t *testing.T) {
	mirror := syntheticSortPackage()
	if mirror.Name() != "sort" || mirror.Path() != "sort" {
		t.Fatalf("package name/path = %s/%s, want sort/sort",
			mirror.Name(), mirror.Path())
	}
	phase1 := phase1SyntheticPackageForTest(t,
		"../../../phase1/frontend/synth_sort.go", "syntheticSortSource",
		"sort", rejectImports{})

	// Full exported-scope drift check (names + rendered types). This
	// covers every package-level function (Sort, Stable, IsSorted,
	// Reverse, Ints/Float64s/Strings, IntsAreSorted/..., Search, Find,
	// SearchInts/..., Slice/SliceStable/SliceIsSorted) and the types
	// themselves.
	assertScopeMatchesPhase1(t, "sort", mirror, phase1)

	// Interface method set, method-by-method.
	mirrorIface, ok := namedOf(t, mirror, "Interface").Underlying().(*types.Interface)
	if !ok {
		t.Fatal("sort.Interface underlying is not an interface")
	}
	phase1Iface, ok := namedOf(t, phase1, "Interface").Underlying().(*types.Interface)
	if !ok {
		t.Fatal("phase-1 sort.Interface underlying is not an interface")
	}
	mIface, pIface := ifaceMethodSigs(t, "sort", mirrorIface), ifaceMethodSigs(t, "sort", phase1Iface)
	if len(mIface) != len(pIface) {
		t.Fatalf("mirror Interface has %d methods, phase-1 has %d", len(mIface), len(pIface))
	}
	for name, want := range pIface {
		got, ok := mIface[name]
		if !ok {
			t.Fatalf("mirror Interface missing method %q", name)
		}
		if got != want {
			t.Fatalf("mirror Interface.%s signature drift:\n phase-1: %s\n mirror:  %s",
				name, want, got)
		}
	}

	// Convenience slice types: underlying slices + full method sets.
	for _, name := range []string{"IntSlice", "Float64Slice", "StringSlice"} {
		mNamed, pNamed := namedOf(t, mirror, name), namedOf(t, phase1, name)
		if _, ok := mNamed.Underlying().(*types.Slice); !ok {
			t.Fatalf("mirror sort.%s underlying is %T, want *types.Slice",
				name, mNamed.Underlying())
		}
		mMeths, pMeths := methodSigs(t, "sort", mNamed), methodSigs(t, "sort", pNamed)
		if len(mMeths) != len(pMeths) {
			t.Fatalf("mirror sort.%s has %d methods, phase-1 has %d",
				name, len(mMeths), len(pMeths))
		}
		for mname, want := range pMeths {
			got, ok := mMeths[mname]
			if !ok {
				t.Fatalf("mirror sort.%s missing method %q", name, mname)
			}
			if got != want {
				t.Fatalf("mirror sort.%s.%s signature drift:\n phase-1: %s\n mirror:  %s",
					name, mname, want, got)
			}
		}
	}

	// Unexported internals (reverse, pdqsort, ...) are not mirrored.
	for _, name := range []string{"reverse", "pdqsort", "insertionSort", "sortMedian"} {
		if mirror.Scope().Lookup(name) != nil {
			t.Fatalf("mirror must not contain unexported %q", name)
		}
	}
}

// TestSortImportResolvesSynthetic verifies the loader resolves "sort"
// as synthetic (never GOROOT).
func TestSortImportResolvesSynthetic(t *testing.T) {
	l := NewLoader()
	ref, err := l.ResolveImport("/tmp", "sort")
	if err != nil {
		t.Fatalf("ResolveImport(%q) failed: %v", "sort", err)
	}
	if ref.Kind != kindSynthetic {
		t.Fatalf("ResolveImport(%q).Kind = %v, want kindSynthetic", "sort", ref.Kind)
	}
}

// TestSortImporterReturnsMirror verifies the Importer serves the mirror
// for "sort" (and that importing it twice returns the cached instance,
// like the other synthetic packages).
func TestSortImporterReturnsMirror(t *testing.T) {
	l := NewLoader()
	imp := NewImporter(l, "/tmp", nil)
	p1, err := imp.Import("sort")
	if err != nil {
		t.Fatalf("Import(sort) failed: %v", err)
	}
	p2, err := imp.Import("sort")
	if err != nil {
		t.Fatalf("Import(sort) again failed: %v", err)
	}
	if p1 != p2 {
		t.Fatal("Import(sort) did not return the cached instance")
	}
	if p1.Scope().Lookup("IsSorted") == nil {
		t.Fatal("mirrored sort package lacks IsSorted")
	}
}

// TestErrorsImporterReturnsMirror is the errors counterpart.
func TestErrorsImporterReturnsMirror(t *testing.T) {
	l := NewLoader()
	imp := NewImporter(l, "/tmp", nil)
	p1, err := imp.Import("errors")
	if err != nil {
		t.Fatalf("Import(errors) failed: %v", err)
	}
	p2, err := imp.Import("errors")
	if err != nil {
		t.Fatalf("Import(errors) again failed: %v", err)
	}
	if p1 != p2 {
		t.Fatal("Import(errors) did not return the cached instance")
	}
	if p1.Scope().Lookup("Join") == nil {
		t.Fatal("mirrored errors package lacks Join")
	}
}
