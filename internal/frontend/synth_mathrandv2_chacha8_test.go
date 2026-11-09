package main

import (
	"go/types"
	"strings"
	"testing"
)

// TestMathRandV2ChaCha8DesugarWiring pins the 3 R08-S-03
// m7MultiResultFuncs entries for math/rand/v2 ChaCha8 (Read,
// AppendBinary, MarshalBinary). For each entry it verifies, against
// the synthetic source in synth_mathrandv2.go (via
// checkM7DesugarWiring):
//
//  1. exactly one table entry exists for (pkgPath, name, recvType),
//     with the expected helper name, pair type, and pair fields;
//  2. the multi-result method exists in the synthetic package scope;
//  3. the __-helper exists and takes ([receiver,] params) positionally;
//  4. the pair struct exists with exactly the listed fields in order,
//     each field's type identical to the corresponding result type.
//
// The table rows also wire the itab via multiResultHelperFor, so the
// io.Reader interface dispatch in crypto/ecdsa's legacy signing path
// (__mrrandFieldElement -> io.ReadFull -> __readFull/__readAtLeast ->
// r.Read) reaches __chacha8Read instead of the old fail-fast stub.
//
// This test fails on the pre-fix tree (no table entries, no helpers,
// no pair types) and fails on any wiring typo.
var m7MathRandV2ChaCha8Table = []m7StrconvJsonExpect{
	// math/rand/v2: ChaCha8.Read (Appendix M; the ecdsa legacy path).
	{"math/rand/v2", "Read", "ChaCha8", "__chacha8Read", "__chacha8ReadPair", []string{"n", "err"}},
	// math/rand/v2: ChaCha8.AppendBinary / MarshalBinary (Appendix M;
	// the full ChaCha8 vendor — binary marshal round-trips with
	// UnmarshalBinary, which is single-result and needs no row).
	{"math/rand/v2", "AppendBinary", "ChaCha8", "__chacha8AppendBinary", "__chacha8AppendBinaryPair", []string{"b", "err"}},
	{"math/rand/v2", "MarshalBinary", "ChaCha8", "__chacha8MarshalBinary", "__chacha8MarshalBinaryPair", []string{"b", "err"}},
}

func TestMathRandV2ChaCha8DesugarWiring(t *testing.T) {
	if len(m7MathRandV2ChaCha8Table) != 3 {
		t.Fatalf("m7MathRandV2ChaCha8Table has %d entries, want 3", len(m7MathRandV2ChaCha8Table))
	}
	for _, want := range m7MathRandV2ChaCha8Table {
		name := want.pkgPath + "." + want.recvType + "." + want.name
		t.Run(name, func(t *testing.T) {
			checkM7DesugarWiring(t, want)
		})
	}
}

// TestMathRandV2ChaCha8StateMachine pins the vendored ChaCha8 state
// machine behind the three helpers above: the inlined
// internal/chacha8rand declarations (Deviation 2) must exist as real
// code, not comment-only stubs, and the ChaCha8 type must carry the
// real upstream state (state/readBuf/readLen) plus the full upstream
// API (Seed/Uint64/UnmarshalBinary are single-result and live as real
// method bodies; Read/AppendBinary/MarshalBinary are the Appendix-M
// stubs desugared to the helpers above).
//
// This test fails on the pre-fix tree: the old file has only comments
// where the __chacha8* declarations belong, a stub struct with just
// a seed field, no Seed/Uint64 methods, and the "ChaCha8.Read not
// implemented" panic.
func TestMathRandV2ChaCha8StateMachine(t *testing.T) {
	sp := getSyntheticPackage("math/rand/v2")
	if sp == nil || sp.pkg == nil {
		t.Fatalf("synthetic package %q not loaded", "math/rand/v2")
	}

	// 1. The old fail-fast stub text must be gone.
	if strings.Contains(syntheticMathRandV2Source, "ChaCha8.Read not implemented") {
		t.Fatalf("synthetic math/rand/v2 still contains the ChaCha8.Read fail-fast stub")
	}

	// 2. The inlined chacha8rand declarations exist as real code.
	for _, name := range []string{
		"__chacha8State",
		"__chacha8Next", "__chacha8NextPair",
		"__chacha8Init", "__chacha8Init64",
		"__chacha8Refill", "__chacha8Reseed",
		"__chacha8Marshal", "__chacha8Unmarshal",
		"__chacha8Setup", "__chacha8Qr", "__chacha8Block",
		"__cutPrefix", "__cutPrefixPair",
		"__readUint8LengthPrefixed", "__readUint8LengthPrefixedPair",
	} {
		if obj := sp.pkg.Scope().Lookup(name); obj == nil {
			t.Errorf("synthetic math/rand/v2: %s missing from package scope", name)
		}
	}

	// 3. The ChaCha8 type carries the real upstream state.
	obj := sp.pkg.Scope().Lookup("ChaCha8")
	if obj == nil {
		t.Fatalf("synthetic math/rand/v2: ChaCha8 missing from package scope")
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		t.Fatalf("synthetic ChaCha8 is %T, want *types.TypeName", obj)
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		t.Fatalf("synthetic ChaCha8 type is %T, want *types.Named", tn.Type())
	}
	st, ok := named.Underlying().(*types.Struct)
	if !ok {
		t.Fatalf("synthetic ChaCha8 underlying is %T, want struct", named.Underlying())
	}
	wantFields := []struct {
		name  string
		check func(types.Type) bool
	}{
		{"state", func(typ types.Type) bool {
			n, ok := typ.(*types.Named)
			return ok && n.Obj() != nil && n.Obj().Name() == "__chacha8State"
		}},
		{"readBuf", func(typ types.Type) bool {
			a, ok := typ.(*types.Array)
			return ok && a.Len() == 8 && types.Identical(a.Elem(), types.Typ[types.Uint8])
		}},
		{"readLen", func(typ types.Type) bool {
			return types.Identical(typ, types.Typ[types.Int])
		}},
	}
	if st.NumFields() != len(wantFields) {
		t.Fatalf("ChaCha8 struct has %d fields, want %d (state, readBuf, readLen)", st.NumFields(), len(wantFields))
	}
	for i, wf := range wantFields {
		f := st.Field(i)
		if f.Name() != wf.name {
			t.Errorf("ChaCha8 field %d is %q, want %q", i, f.Name(), wf.name)
			continue
		}
		if !wf.check(f.Type()) {
			t.Errorf("ChaCha8 field %q has type %v, want the upstream state field", wf.name, f.Type())
		}
	}

	// 4. The full upstream ChaCha8 API exists on *ChaCha8.
	ms := types.NewMethodSet(types.NewPointer(named))
	for _, name := range []string{"Seed", "Uint64", "Read", "AppendBinary", "MarshalBinary", "UnmarshalBinary"} {
		if sel := ms.Lookup(nil, name); sel == nil {
			t.Errorf("(*ChaCha8).%s missing from method set", name)
		}
	}
	// NewChaCha8 exists as a package-level func.
	if obj := sp.pkg.Scope().Lookup("NewChaCha8"); obj == nil {
		t.Errorf("synthetic math/rand/v2: NewChaCha8 missing from package scope")
	}
}
