package main

import (
	"go/types"
	"testing"
)

// TestM7StrconvJsonDesugarWiring pins the 15 GAP-1 m7MultiResultFuncs
// entries added for strconv and encoding/json (P3-M7 wave 1, Appendix M).
// For each entry it verifies, against the synthetic sources in
// synth_strconv.go / synth_json.go:
//
//  1. exactly one table entry exists for (pkgPath, name, recvType), with
//     the expected helper name, pair type, and pair fields in order;
//  2. the multi-result function (or method on the named receiver type)
//     exists in the synthetic package scope;
//  3. the __-helper exists in the synthetic package scope and is a
//     *types.Func;
//  4. the helper's parameters accept the multi-result call's arguments
//     positionally (the desugar passes the original call's args straight
//     through, receiver prepended for methods);
//  5. the pair struct exists with exactly the listed fields in order,
//     each field's type identical to the multi-result signature's
//     corresponding result type.
//
// This test fails on the pre-GAP-1 tree (no table entries for these
// functions) and fails on any wiring typo (wrong helper name, wrong pair
// type, wrong field name/order, arity mismatch).
type m7StrconvJsonExpect struct {
	pkgPath    string
	name       string
	recvType   string
	helper     string
	pairType   string
	pairFields []string
}

var m7StrconvJsonTable = []m7StrconvJsonExpect{
	// strconv: package-level funcs.
	{"strconv", "ParseBool", "", "__parseBoolPub", "parseBoolPair", []string{"v", "err"}},
	{"strconv", "ParseInt", "", "__parseIntPub", "parseIntPair", []string{"v", "err"}},
	{"strconv", "ParseUint", "", "__parseUintPub", "parseUintPair", []string{"v", "err"}},
	{"strconv", "Atoi", "", "__atoiPub", "atoiPair", []string{"v", "err"}},
	{"strconv", "ParseFloat", "", "__parseFloat", "parseFloatPair", []string{"v", "err"}},
	{"strconv", "ParseComplex", "", "__parseComplexPub", "parseComplexPair", []string{"v", "err"}},
	{"strconv", "Unquote", "", "__unquotePub", "unquotePair", []string{"v", "err"}},
	{"strconv", "QuotedPrefix", "", "__quotedPrefix", "quotedPrefixPair", []string{"v", "err"}},
	// UnquoteChar is the 4-result entry: (value, multibyte, tail, err).
	{"strconv", "UnquoteChar", "", "__unquoteChar", "unquoteCharPair", []string{"value", "multibyte", "tail", "err"}},
	// encoding/json: package-level funcs.
	{"encoding/json", "Marshal", "", "__marshal", "marshalPair", []string{"data", "err"}},
	{"encoding/json", "MarshalIndent", "", "__marshalIndent", "marshalIndentPair", []string{"data", "err"}},
	// encoding/json: methods (recvType names the receiver's named type).
	{"encoding/json", "Token", "Decoder", "__decoderToken", "decoderTokenPair", []string{"tok", "err"}},
	{"encoding/json", "Float64", "Number", "__numberFloat64", "numberFloat64Pair", []string{"f", "err"}},
	{"encoding/json", "Int64", "Number", "__numberInt64", "numberInt64Pair", []string{"i", "err"}},
	{"encoding/json", "MarshalJSON", "RawMessage", "__rawMessageMarshalJSON", "rawMessageMarshalJSONPair", []string{"data", "err"}},
}

func TestM7StrconvJsonDesugarWiring(t *testing.T) {
	if len(m7StrconvJsonTable) != 15 {
		t.Fatalf("m7StrconvJsonTable has %d entries, want 15", len(m7StrconvJsonTable))
	}
	for _, want := range m7StrconvJsonTable {
		name := want.pkgPath + "." + want.name
		if want.recvType != "" {
			name = want.pkgPath + "." + want.recvType + "." + want.name
		}
		t.Run(name, func(t *testing.T) {
			// 1. Exactly one table entry for (pkgPath, name, recvType)
			//    carrying the expected helper/pair/fields.
			var got *m7MultiResultFunc
			for i := range m7MultiResultFuncs {
				mf := &m7MultiResultFuncs[i]
				if mf.pkgPath == want.pkgPath && mf.name == want.name && mf.recvType == want.recvType {
					if got != nil {
						t.Fatalf("duplicate m7MultiResultFuncs entry for %s (recv %q)", name, want.recvType)
					}
					got = mf
				}
			}
			if got == nil {
				t.Fatalf("no m7MultiResultFuncs entry for %s (recv %q)", name, want.recvType)
			}
			if got.helper != want.helper {
				t.Fatalf("table helper = %q, want %q", got.helper, want.helper)
			}
			if got.pairType != want.pairType {
				t.Fatalf("table pairType = %q, want %q", got.pairType, want.pairType)
			}
			if len(got.pairFields) != len(want.pairFields) {
				t.Fatalf("table pairFields = %v, want %v", got.pairFields, want.pairFields)
			}
			for i, f := range want.pairFields {
				if got.pairFields[i] != f {
					t.Fatalf("table pairFields = %v, want %v", got.pairFields, want.pairFields)
				}
			}

			sp := getSyntheticPackage(want.pkgPath)
			if sp == nil || sp.pkg == nil {
				t.Fatalf("synthetic package %q not loaded", want.pkgPath)
			}

			// 2. The multi-result function/method exists in the synthetic
			//    package.
			mrFn := lookupM7MultiResultFunc(t, sp, want)
			sig, ok := mrFn.Type().(*types.Signature)
			if !ok {
				t.Fatalf("synthetic %s is not a function signature", name)
			}
			if sig.Results().Len() != len(want.pairFields) {
				t.Fatalf("synthetic %s has %d results, want %d (pairFields)", name, sig.Results().Len(), len(want.pairFields))
			}

			// 3. The __-helper exists in the synthetic package scope and
			//    is a *types.Func.
			hobj := sp.pkg.Scope().Lookup(want.helper)
			if hobj == nil {
				t.Fatalf("synthetic helper %s.%s missing from package scope", want.pkgPath, want.helper)
			}
			helper, ok := hobj.(*types.Func)
			if !ok {
				t.Fatalf("synthetic %s.%s is %T, want *types.Func", want.pkgPath, want.helper, hobj)
			}
			hsig, ok := helper.Type().(*types.Signature)
			if !ok {
				t.Fatalf("synthetic helper %s has non-signature type %v", want.helper, helper.Type())
			}

			// 4. The helper's params accept the multi-result call's args
			//    positionally: [receiver,] params — the desugar passes the
			//    original args straight through, receiver prepended.
			var wantParams []types.Type
			if sig.Recv() != nil {
				wantParams = append(wantParams, sig.Recv().Type())
			}
			for i := 0; i < sig.Params().Len(); i++ {
				wantParams = append(wantParams, sig.Params().At(i).Type())
			}
			if hsig.Params().Len() != len(wantParams) {
				t.Fatalf("helper %s takes %d params, multi-result %s has %d ([recv,] params)",
					want.helper, hsig.Params().Len(), name, len(wantParams))
			}
			for i, wp := range wantParams {
				if !types.Identical(hsig.Params().At(i).Type(), wp) {
					t.Fatalf("helper %s param %d type %v != multi-result %s arg type %v",
						want.helper, i, hsig.Params().At(i).Type(), name, wp)
				}
			}

			// 5. The pair struct exists with exactly the listed fields in
			//    order, each field type identical to the corresponding
			//    multi-result result type.
			pobj := sp.pkg.Scope().Lookup(want.pairType)
			if pobj == nil {
				t.Fatalf("synthetic pair type %s.%s missing from package scope", want.pkgPath, want.pairType)
			}
			nt, ok := pobj.Type().(*types.Named)
			if !ok {
				t.Fatalf("synthetic %s has type %T, want named struct", want.pairType, pobj.Type())
			}
			st, ok := nt.Underlying().(*types.Struct)
			if !ok {
				t.Fatalf("synthetic %s underlying is %T, want struct", want.pairType, nt.Underlying())
			}
			if st.NumFields() != len(want.pairFields) {
				t.Fatalf("pair %s has %d fields, want %d", want.pairType, st.NumFields(), len(want.pairFields))
			}
			for i, fname := range want.pairFields {
				f := st.Field(i)
				if f.Name() != fname {
					t.Fatalf("pair %s field %d is %q, want %q", want.pairType, i, f.Name(), fname)
				}
				rt := sig.Results().At(i).Type()
				if !types.Identical(f.Type(), rt) {
					t.Fatalf("pair %s field %q type %v != result %d type %v",
						want.pairType, fname, f.Type(), i, rt)
				}
			}
		})
	}
}

// lookupM7MultiResultFunc finds the multi-result function (package-level)
// or method (on the table entry's recvType named type) in the synthetic
// package scope.
func lookupM7MultiResultFunc(t *testing.T, sp *syntheticPackage, want m7StrconvJsonExpect) *types.Func {
	t.Helper()
	if want.recvType == "" {
		obj := sp.pkg.Scope().Lookup(want.name)
		fn, ok := obj.(*types.Func)
		if !ok || fn == nil {
			t.Fatalf("synthetic %s.%s missing or not a func (got %T)", want.pkgPath, want.name, obj)
		}
		return fn
	}
	tnobj := sp.pkg.Scope().Lookup(want.recvType)
	if tnobj == nil {
		t.Fatalf("synthetic receiver type %s.%s missing", want.pkgPath, want.recvType)
	}
	nt, ok := tnobj.Type().(*types.Named)
	if !ok {
		t.Fatalf("synthetic %s.%s has type %T, want named type", want.pkgPath, want.recvType, tnobj.Type())
	}
	// Pointer method set covers both pointer and value receivers.
	ms := types.NewMethodSet(types.NewPointer(nt))
	sel := ms.Lookup(sp.pkg, want.name)
	if sel == nil {
		t.Fatalf("synthetic method %s.%s(%s) missing", want.pkgPath, want.recvType, want.name)
	}
	fn, ok := sel.Obj().(*types.Func)
	if !ok {
		t.Fatalf("synthetic %s.%s(%s) is %T, want *types.Func", want.pkgPath, want.recvType, want.name, sel.Obj())
	}
	return fn
}
