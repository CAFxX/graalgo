package main

// Exporter tests for the synthetic `math/rand/v2` package's ChaCha8
// (N4, review 2026-11-02-12). Red-green: before the fix the exported
// ChaCha8 was a stub — no Uint64/Seed (so `rand.New(NewChaCha8(seed))`
// failed type-checking with "missing method Uint64"), no live
// __chacha8* machinery (the generator stripped it while leaving the
// comments), and Read exported to a panic stub whose message blamed
// "non-standard ECDSA curves". After the fix ChaCha8 is gc's chacha8.go
// with Appendix-M __-helpers, and the same programs export to the
// helpers with no panic stub.

import (
	"go/types"
	"strings"
	"testing"
)

// TestSyntheticMathRandV2ChaCha8Surface pins the ChaCha8 surface the
// generator must emit: the full method set (Uint64 makes it a
// rand.Source; Seed resets it; Read/AppendBinary/MarshalBinary are the
// Appendix-M methods) and the live __chacha8* helpers plus the
// Appendix-M helper/pair types in the synthetic package scope.
func TestSyntheticMathRandV2ChaCha8Surface(t *testing.T) {
	pkg, err := overlayImporter{}.Import("math/rand/v2")
	if err != nil {
		t.Fatalf("import math/rand/v2: %v", err)
	}
	obj := pkg.Scope().Lookup("ChaCha8")
	if obj == nil {
		t.Fatal("math/rand/v2 scope missing ChaCha8")
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		t.Fatalf("ChaCha8 is %T, want *types.TypeName", obj)
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		t.Fatalf("ChaCha8 type is %T, want *types.Named", tn.Type())
	}
	ms := types.NewMethodSet(types.NewPointer(named))
	for _, m := range []string{"Uint64", "Seed", "Read", "AppendBinary", "MarshalBinary", "UnmarshalBinary"} {
		if sel := ms.Lookup(pkg, m); sel == nil {
			t.Errorf("ChaCha8 missing method %s", m)
		}
	}
	// The Appendix-M helpers and pair types must be declared in the
	// synthetic source, or the mrdesugar table entries have nothing
	// to desugar to.
	for _, name := range []string{
		"__chacha8Read", "__chacha8ReadPair",
		"__chacha8AppendBinary", "__chacha8MarshalBinary", "__chacha8BinaryPair",
		"__chacha8State", "__chacha8Block",
	} {
		if pkg.Scope().Lookup(name) == nil {
			t.Errorf("math/rand/v2 scope missing %s (helper stripped or not generated)", name)
		}
	}
	// NewChaCha8 keeps gc's exact signature.
	fnObj := pkg.Scope().Lookup("NewChaCha8")
	if fnObj == nil {
		t.Fatal("math/rand/v2 scope missing NewChaCha8")
	}
	fn, ok := fnObj.(*types.Func)
	if !ok {
		t.Fatalf("NewChaCha8 is %T, want *types.Func", fnObj)
	}
	if sig := fn.Type().(*types.Signature); sig.Params().Len() != 1 || sig.Results().Len() != 1 {
		t.Errorf("NewChaCha8 signature wrong: %s", sig)
	}
}

// TestSyntheticMathRandV2ChaCha8Export exports a program using ChaCha8
// as a rand.Source (rand.New), through a direct Read call, through an
// io.Reader interface value (the shape crypto/ecdsa's signLegacy uses),
// and through MarshalBinary. It must export successfully, desugar to
// the __-helpers, and contain no trace of the old panic stub.
func TestSyntheticMathRandV2ChaCha8Export(t *testing.T) {
	src := `package main

import (
	"io"
	randv2 "math/rand/v2"
)

func main() {
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i)
	}
	c := randv2.NewChaCha8(seed)
	buf := make([]byte, 20)
	n, err := c.Read(buf)
	_, _ = n, err
	// ChaCha8 as a rand.Source.
	r := randv2.New(c)
	_ = r.Uint64()
	// ChaCha8 as an io.Reader (ecdsa signLegacy's shape).
	var rd io.Reader = c
	m, err2 := rd.Read(buf)
	_, _ = m, err2
	// BinaryMarshaler shape.
	b, err3 := c.MarshalBinary()
	_, _ = b, err3
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with ChaCha8 as rand.Source: %v", err)
	}
	for _, want := range []string{"__chacha8Read", "__chacha8MarshalBinary"} {
		if !strings.Contains(out, want) {
			t.Errorf("exported JSON missing %s (Read/MarshalBinary did not desugar to the helper)", want)
		}
	}
	for _, gone := range []string{
		"ChaCha8 not vendored",
		"not implemented (P3-M9 tier-5b",
		"non-standard ECDSA curves",
	} {
		if strings.Contains(out, gone) {
			t.Errorf("exported JSON still contains old stub text %q", gone)
		}
	}
}
