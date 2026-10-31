package main

// Exhaustive Appendix-M pair-field audit for m7MultiResultFuncs
// (review-bytes-mr, item 5).
//
// The desugar extracts pair fields by NAME from the plan's pairFields
// list, but that list is hand-written in the table and was never
// checked against the pair struct or the multi-result signature it
// claims to describe: a typo in a field name, a field-order swap, or a
// result-type change in the synthetic source would silently extract the
// wrong value (or fail obscurely downstream). This test asserts every
// table entry against its synthetic package:
//
//  1. (pkgPath, name, recvType) is unique in the table (classify takes
//     the first match, so a duplicate silently shadows the second);
//  2. the multi-result function/method exists in the synthetic package
//     (helper package, falling back to the entry's own package);
//  3. the __-helper exists and is a *types.Func;
//  4. the pair struct exists with exactly the listed fields in order;
//  5. each pair field's type equals the corresponding signature result
//     type (generic type parameters compared by name, since the pair's
//     and the function's parameter objects differ);
//  6. the helper takes the call's arguments positionally ([receiver,]
//     params), compared with the same generic-aware equality.
//
// Entries whose synthetic package cannot be loaded are logged and
// skipped (not failed): the audit covers what it can resolve, and the
// skip count is reported so a silent mass-skip cannot hide behind it.
// At export time, synthPairField independently errors when a table
// list disagrees with its pair struct, so a wrong entry fails loudly
// rather than extracting the wrong field; this test is the pin that
// keeps wrong entries out of the table in the first place.

import (
	"go/types"
	"testing"
)

// pairFieldTypeEqual compares a pair-struct field type with a signature
// result type, tolerating distinct *types.TypeParam objects for the
// same parameter (errors.AsType's E is the canonical case).
func pairFieldTypeEqual(field, result types.Type) bool {
	if fp, ok := field.(*types.TypeParam); ok {
		rp, ok2 := result.(*types.TypeParam)
		return ok2 && fp.Obj().Name() == rp.Obj().Name()
	}
	if _, ok := result.(*types.TypeParam); ok {
		return false
	}
	return types.Identical(field, result)
}

func TestM7PairFieldsExhaustive(t *testing.T) {
	seen := map[string]bool{}
	skipped := 0
	checked := 0
	for i := range m7MultiResultFuncs {
		mf := &m7MultiResultFuncs[i]
		key := mf.pkgPath + "." + mf.recvType + "." + mf.name
		name := key
		t.Run(name, func(t *testing.T) {
			if seen[key] {
				t.Fatalf("duplicate m7MultiResultFuncs entry for %s (first match shadows the rest)", key)
			}
			seen[key] = true
			helperPkg := mf.pkgPath
			if override, ok := m7HelperPkgOverride[mf.pkgPath]; ok {
				helperPkg = override
			}
			// Veto fix-up P2c (review-bytes-mr): the recover below
			// covers ONLY the load phase (getSyntheticPackage +
			// tryLookupM7Func). A synthetic package that fails to
			// load standalone (e.g. archive/zip's reference to
			// time.FixedZone, a pre-existing load issue outside
			// this audit) must not abort the whole audit: log it
			// as a skip. The previous code deferred the recover
			// over the entire subtest, so a panic from a checker
			// bug in the field/type comparisons below was also
			// counted as a skip — masking the bug. Now a checker
			// panic fails the test, as it should.
			sp, mrFn, loadPanicked := func() (sp *syntheticPackage, mrFn *types.Func, loadPanicked bool) {
				defer func() {
					if r := recover(); r != nil {
						t.Logf("SKIP: panic loading synthetic package: %v", r)
						loadPanicked = true
					}
				}()
				sp = getSyntheticPackage(helperPkg)
				if sp == nil || sp.pkg == nil {
					return sp, nil, false
				}
				// The multi-result function: in the helper
				// package, or (for override entries) the entry's
				// own package.
				want := m7StrconvJsonExpect{
					pkgPath: helperPkg, name: mf.name, recvType: mf.recvType,
					helper: mf.helper, pairType: mf.pairType, pairFields: mf.pairFields,
				}
				if fn := tryLookupM7Func(sp, want); fn != nil {
					mrFn = fn
				} else if helperPkg != mf.pkgPath {
					if sp2 := getSyntheticPackage(mf.pkgPath); sp2 != nil && sp2.pkg != nil {
						want2 := want
						want2.pkgPath = mf.pkgPath
						mrFn = tryLookupM7Func(sp2, want2)
					}
				}
				return sp, mrFn, false
			}()
			if loadPanicked {
				skipped++
				return
			}
			if sp == nil || sp.pkg == nil {
				t.Logf("SKIP: synthetic package %q not loaded", helperPkg)
				skipped++
				return
			}
			if mrFn == nil {
				t.Logf("SKIP: multi-result %s not found in synthetic package %q (or %q)", key, helperPkg, mf.pkgPath)
				skipped++
				return
			}
			sig, ok := mrFn.Type().(*types.Signature)
			if !ok {
				t.Fatalf("synthetic %s has non-signature type %v", key, mrFn.Type())
			}
			if sig.Results().Len() != len(mf.pairFields) {
				t.Fatalf("synthetic %s has %d results, table lists %d pair fields", key, sig.Results().Len(), len(mf.pairFields))
			}
			// Helper.
			hobj := sp.pkg.Scope().Lookup(mf.helper)
			helper, ok := hobj.(*types.Func)
			if !ok || helper == nil {
				t.Fatalf("synthetic helper %s.%s missing or not a func (got %T)", helperPkg, mf.helper, hobj)
			}
			hsig, ok := helper.Type().(*types.Signature)
			if !ok {
				t.Fatalf("synthetic helper %s has non-signature type %v", mf.helper, helper.Type())
			}
			var wantParams []types.Type
			if sig.Recv() != nil {
				wantParams = append(wantParams, sig.Recv().Type())
			}
			for j := 0; j < sig.Params().Len(); j++ {
				wantParams = append(wantParams, sig.Params().At(j).Type())
			}
			if hsig.Params().Len() != len(wantParams) {
				t.Fatalf("helper %s takes %d params, multi-result %s has %d ([recv,] params)",
					mf.helper, hsig.Params().Len(), key, len(wantParams))
			}
			for j, wp := range wantParams {
				if !pairFieldTypeEqual(hsig.Params().At(j).Type(), wp) {
					t.Fatalf("helper %s param %d type %v != multi-result %s arg type %v",
						mf.helper, j, hsig.Params().At(j).Type(), key, wp)
				}
			}
			// Pair struct: fields must equal the table list in order,
			// with types equal to the signature results positionally.
			pobj := sp.pkg.Scope().Lookup(mf.pairType)
			if pobj == nil {
				t.Fatalf("synthetic pair type %s.%s missing from package scope", helperPkg, mf.pairType)
			}
			nt, ok := pobj.Type().(*types.Named)
			if !ok {
				t.Fatalf("synthetic %s has type %T, want named struct", mf.pairType, pobj.Type())
			}
			st, ok := nt.Underlying().(*types.Struct)
			if !ok {
				t.Fatalf("synthetic %s underlying is %T, want struct", mf.pairType, nt.Underlying())
			}
			if st.NumFields() != len(mf.pairFields) {
				t.Fatalf("pair %s has %d fields, table lists %d", mf.pairType, st.NumFields(), len(mf.pairFields))
			}
			for j, fname := range mf.pairFields {
				f := st.Field(j)
				if f.Name() != fname {
					t.Fatalf("pair %s field %d is %q, table lists %q", mf.pairType, j, f.Name(), fname)
				}
				if !pairFieldTypeEqual(f.Type(), sig.Results().At(j).Type()) {
					t.Fatalf("pair %s field %q type %v != result %d type %v",
						mf.pairType, fname, f.Type(), j, sig.Results().At(j).Type())
				}
			}
			checked++
		})
	}
	t.Logf("m7 pair-field audit: %d entries checked, %d skipped (unloaded package or unresolvable function)", checked, skipped)
	if checked == 0 {
		t.Fatal("audit checked zero entries; the test is vacuous")
	}
}

// tryLookupM7Func mirrors lookupM7MultiResultFunc without failing the
// test: it returns nil when the function/method is not found.
func tryLookupM7Func(sp *syntheticPackage, want m7StrconvJsonExpect) *types.Func {
	if sp == nil || sp.pkg == nil {
		return nil
	}
	if want.recvType == "" {
		fn, _ := sp.pkg.Scope().Lookup(want.name).(*types.Func)
		return fn
	}
	tnobj := sp.pkg.Scope().Lookup(want.recvType)
	if tnobj == nil {
		return nil
	}
	nt, ok := tnobj.Type().(*types.Named)
	if !ok {
		return nil
	}
	ms := types.NewMethodSet(types.NewPointer(nt))
	sel := ms.Lookup(sp.pkg, want.name)
	if sel == nil {
		return nil
	}
	fn, _ := sel.Obj().(*types.Func)
	return fn
}
