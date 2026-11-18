package emit

import (
	"bytes"
	"testing"

	"graalgo-toolexec/ir"
)

// The compile-only std gate of milestone M1a, at W2 scope: every
// non-empty std package (graalgo's configuration: purego, no cgo) emits
// without an unsupported construct, and its GIR decodes, validates and
// re-encodes to the same bytes. W3 keeps this green while it completes
// the lowering.
func TestStd(t *testing.T) {
	if testing.Short() {
		t.Skip("type-checks all of std; skipped in -short mode")
	}
	pkgs, errs, err := Load("std")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range errs {
		t.Errorf("load: %v", e)
	}
	if len(pkgs) < 350 {
		t.Fatalf("only %d std packages loaded", len(pkgs))
	}
	total := newStats()
	for _, p := range pkgs {
		f, st, err := Emit(p.Config)
		total.Add(st)
		if err != nil {
			t.Errorf("%s: %v", p.ImportPath, err)
			continue
		}
		assertNoUntyped(t, f)
		data, err := ir.Encode(f)
		if err != nil {
			t.Fatalf("%s: %v", p.ImportPath, err)
		}
		g, err := ir.DecodeValidate(data)
		if err != nil {
			t.Fatalf("%s: %v", p.ImportPath, err)
		}
		again, err := ir.Encode(g)
		if err != nil || !bytes.Equal(again, data) {
			t.Fatalf("%s: round trip differs", p.ImportPath)
		}
	}
	if total.MaxDepth*4 > ir.MaxDepth {
		t.Errorf("std nests %d deep; ir.MaxDepth %d leaves less than 4x headroom", total.MaxDepth, ir.MaxDepth)
	}
	t.Logf("%d packages, %d function bodies, max nesting %d", len(pkgs), total.Funcs, total.MaxDepth)
}
