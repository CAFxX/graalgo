package main

// Review 2026-10-30 P1-14: math/rand/v2 ChaCha8.Read panicked with a
// message naming "non-standard ECDSA curves" as the blocker —
// nonsense for ChaCha8 (leaked from the crypto/ecdsa signLegacy
// use-case) — while the file header named the real blocker (exported
// multi-result methods were not emittable) and the real retest
// trigger (frontend multi-result method emission). That trigger has
// fired (r40 machinery: m7 table + itab helper mapping), so the full
// ChaCha8 (gc chacha8.go + inlined chacha8rand, via
// gen_synth_mathrandv2.py) is restored: Read desugars through the
// __chacha8Read pair helper, directly and through io.Reader dispatch
// (the path crypto/ecdsa signLegacy uses). These tests pin that:
//
//   - exactly one m7 entry exists for (math/rand/v2, Read, ChaCha8)
//     with helper __chacha8Read and pair fields [n err];
//   - the helper and pair struct exist in the synthetic package with
//     matching shapes, and ChaCha8.Read exists with 2 results;
//   - the old stub (seed field, ECDSA-curves panic message) is gone;
//   - a program using ChaCha8 directly, through io.ReadFull, and via
//     Uint64/Seed/UnmarshalBinary exports without error — the r40
//     re-test, including the old "& on SelectorExpr" scope limit
//     (__chacha8Block(&s.seed, &s.buf, ...) in the restored helpers).
//
// Runtime byte-identity vs gc is NOT pinned here (no JVM in this
// environment); the restored code is gc-verbatim modulo the documented
// Deviation 2 adaptations, and a JVM differential run should confirm
// fixed-seed ChaCha8 output equals gc before the claim is repeated.

import (
	"go/types"
	"strings"
	"testing"
)

func TestChaCha8ReadWiring(t *testing.T) {
	var got *m7MultiResultFunc
	for i := range m7MultiResultFuncs {
		mf := &m7MultiResultFuncs[i]
		if mf.pkgPath == "math/rand/v2" && mf.name == "Read" && mf.recvType == "ChaCha8" {
			if got != nil {
				t.Fatal("duplicate m7MultiResultFuncs entry for math/rand/v2 ChaCha8.Read")
			}
			got = mf
		}
	}
	if got == nil {
		t.Fatal("no m7MultiResultFuncs entry for math/rand/v2 ChaCha8.Read")
	}
	if got.helper != "__chacha8Read" {
		t.Fatalf("table helper = %q, want %q", got.helper, "__chacha8Read")
	}
	if got.pairType != "__chacha8ReadPair" {
		t.Fatalf("table pairType = %q, want %q", got.pairType, "__chacha8ReadPair")
	}
	if len(got.pairFields) != 2 || got.pairFields[0] != "n" || got.pairFields[1] != "err" {
		t.Fatalf("table pairFields = %v, want [n err]", got.pairFields)
	}

	sp := getSyntheticPackage("math/rand/v2")
	if sp == nil || sp.pkg == nil {
		t.Fatal("synthetic package math/rand/v2 not loaded")
	}
	// ChaCha8.Read exists as a 2-result method on *ChaCha8.
	tnObj := sp.pkg.Scope().Lookup("ChaCha8")
	tn, ok := tnObj.(*types.TypeName)
	if !ok {
		t.Fatalf("synthetic ChaCha8 is %T, want *types.TypeName", tnObj)
	}
	ms := types.NewMethodSet(types.NewPointer(tn.Type()))
	sel := ms.Lookup(nil, "Read")
	if sel == nil {
		t.Fatal("synthetic *ChaCha8 has no Read method")
	}
	mfn, ok := sel.Obj().(*types.Func)
	if !ok {
		t.Fatalf("ChaCha8.Read is %T, want *types.Func", sel.Obj())
	}
	if sig, ok := mfn.Type().(*types.Signature); !ok || sig.Results().Len() != 2 {
		t.Fatalf("ChaCha8.Read does not have 2 results")
	}
	// Helper exists, takes (recv, []byte), returns the pair only.
	hobj := sp.pkg.Scope().Lookup("__chacha8Read")
	helper, ok := hobj.(*types.Func)
	if !ok {
		t.Fatalf("synthetic __chacha8Read is %T, want *types.Func", hobj)
	}
	hsig, ok := helper.Type().(*types.Signature)
	if !ok {
		t.Fatalf("synthetic __chacha8Read has type %T, want signature", helper.Type())
	}
	if hsig.Params().Len() != 2 || hsig.Results().Len() != 1 {
		t.Fatalf("__chacha8Read params/results = %d/%d, want 2/1", hsig.Params().Len(), hsig.Results().Len())
	}
	// Pair struct fields [n err] in order.
	pobj := sp.pkg.Scope().Lookup("__chacha8ReadPair")
	pnt, ok := pobj.(*types.TypeName)
	if !ok {
		t.Fatalf("synthetic __chacha8ReadPair is %T, want *types.TypeName", pobj)
	}
	pst, ok := pnt.Type().Underlying().(*types.Struct)
	if !ok {
		t.Fatalf("__chacha8ReadPair underlying is %T, want struct", pnt.Type().Underlying())
	}
	if pst.NumFields() != 2 || pst.Field(0).Name() != "n" || pst.Field(1).Name() != "err" {
		t.Fatalf("__chacha8ReadPair fields wrong (want [n err])")
	}
	// The old tier-5b stub is gone: no seed field, no ECDSA message.
	if strings.Contains(syntheticMathRandV2Source, "non-standard ECDSA curves") {
		t.Errorf("synthetic math/rand/v2 source still contains the wrong ChaCha8.Read panic message")
	}
	if strings.Contains(syntheticMathRandV2Source, "ChaCha8 not vendored") {
		t.Errorf("synthetic math/rand/v2 source still contains the ChaCha8 stub")
	}
	if !strings.Contains(syntheticMathRandV2Source, "func __chacha8Read(c *ChaCha8, p []byte) __chacha8ReadPair") {
		t.Errorf("synthetic math/rand/v2 source missing the __chacha8Read helper")
	}
}

func TestChaCha8Exports(t *testing.T) {
	src := `package main

import (
	"io"
	"math/rand/v2"
)

func main() {
	var seed [32]byte
	c := rand.NewChaCha8(seed)
	buf := make([]byte, 40)
	n, err := c.Read(buf)
	_, _ = n, err
	_, err = io.ReadFull(c, buf)
	_ = err
	_ = c.Uint64()
	c.Seed(seed)
	_ = c.UnmarshalBinary(make([]byte, 0))
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
		t.Fatalf("ChaCha8 program should export, got: %v", err)
	}
}
