package main

import (
	"go/types"
	"testing"
)

// TestM7StrconvDesugarWiring pins the 9 GAP-1 m7MultiResultFuncs
// entries added for strconv (P3-M7 wave 1, Appendix M). The 6
// encoding/json entries were deleted with the retirement of the
// synthetic json source (review-08-synth-json, R08-S-01): real
// encoding/json compiles from GOROOT via the disk importer, so its
// multi-result members desugar via the disk table, not this table.
// For each entry it verifies, against the synthetic source in
// synth_strconv.go:
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

var m7StrconvTable = []m7StrconvJsonExpect{
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
}

var m7StringsBuilderTable = []m7StrconvJsonExpect{
	// strings: Builder.Write/WriteString/WriteRune (P3-M10).
	{"strings", "Write", "Builder", "__builderWrite", "__builderWritePair", []string{"n", "err"}},
	{"strings", "WriteString", "Builder", "__builderWriteString", "__builderWriteStringPair", []string{"n", "err"}},
	{"strings", "WriteRune", "Builder", "__builderWriteRune", "__builderWriteRunePair", []string{"n", "err"}},
}

var m7BytesBufferWriteStringTable = []m7StrconvJsonExpect{
	// bytes: Buffer.WriteString (P3-M10).
	{"bytes", "WriteString", "Buffer", "__bytesBufferWriteString", "__bytesWriteStringPair", []string{"n", "err"}},
}

var m7ArchiveMimeTable = []m7StrconvJsonExpect{
	// mime/multipart: Reader.ReadForm + FileHeader.Open (A-P1-11,
	// REVIEW-2026-11-05-15): the __-helpers existed with real bodies but
	// no table rows, so the calls failed with the export scope error.
	{"mime/multipart", "ReadForm", "Reader", "__mpReaderReadForm", "__mpReaderReadFormPair", []string{"f0", "f1"}},
	{"mime/multipart", "Open", "FileHeader", "__mpFileHeaderOpen", "__mpFileHeaderOpenPair", []string{"f0", "f1"}},
	// archive/zip: fileListEntry.Info, openDir.Stat, openDir.Read
	// (A-P1-11). The Info/Stat/Read rows also wire the itab via
	// multiResultHelperFor, so the fs.DirEntry/fs.File interface
	// dispatch (classifyUserIfaceMultiResultCall fallback, f0/f1 pair)
	// reaches the concrete helpers. ifaceFallback stays false for all
	// six: the flag only fires when the selected method's package is
	// the entry's own package, and the three interface-dispatched
	// methods here are declared in io/fs and io, never archive/zip.
	{"archive/zip", "Info", "fileListEntry", "__zipfileListEntryInfo", "__zipfileListEntryInfoPair", []string{"f0", "f1"}},
	{"archive/zip", "Stat", "openDir", "__zipopenDirStat", "__zipopenDirStatPair", []string{"f0", "f1"}},
	{"archive/zip", "Read", "openDir", "__zipopenDirRead", "__zipopenDirReadPair", []string{"f0", "f1"}},
	// archive/zip: headerFileInfo.Info (NEEDS-FIX re-review of
	// acab9a30, 2026-11-05): the guard's EXCLUSIONS wrongly excused
	// this row as unreachable, but headerFileInfo implements
	// fs.DirEntry, so it is reachable two ways — (a) a file entry
	// from fs.ReadDir on a zip IS a headerFileInfo value (gc
	// fileListEntry.stat returns headerFileInfo for files), so
	// e.Info() dispatches to it; (b) FileHeader.FileInfo() returns a
	// headerFileInfo as fs.FileInfo, which asserts to fs.DirEntry.
	// Without this row the itab has no entry for headerFileInfo.Info
	// (multiResultHelperFor finds no helper) and interface dispatch
	// fails fast with "missing method".
	{"archive/zip", "Info", "headerFileInfo", "__zipheaderFileInfoInfo", "__zipheaderFileInfoInfoPair", []string{"f0", "f1"}},
}

func TestM7StringsBuilderDesugarWiring(t *testing.T) {
	if len(m7StringsBuilderTable) != 3 {
		t.Fatalf("m7StringsBuilderTable has %d entries, want 3", len(m7StringsBuilderTable))
	}
	for _, want := range m7StringsBuilderTable {
		name := want.pkgPath + "." + want.recvType + "." + want.name
		t.Run(name, func(t *testing.T) {
			checkM7DesugarWiring(t, want)
		})
	}
}

func TestM7BytesBufferWriteStringDesugarWiring(t *testing.T) {
	if len(m7BytesBufferWriteStringTable) != 1 {
		t.Fatalf("m7BytesBufferWriteStringTable has %d entries, want 1", len(m7BytesBufferWriteStringTable))
	}
	for _, want := range m7BytesBufferWriteStringTable {
		name := want.pkgPath + "." + want.recvType + "." + want.name
		t.Run(name, func(t *testing.T) {
			checkM7DesugarWiring(t, want)
		})
	}
}

// TestM7ArchiveMimeDesugarWiring pins the 6 A-P1-11 m7MultiResultFuncs
// entries for archive/zip and mime/multipart (REVIEW-2026-11-05-15;
// the 6th, headerFileInfo.Info, added by the NEEDS-FIX fix-up — the
// guard's EXCLUSIONS had wrongly excused it as unreachable).
// Table-only by design (step 1 of checkM7DesugarWiring): the full
// synthetic-package check (steps 2-5) cannot run on this branch's base
// because the two synthetic packages do not type-check there yet —
// mime/multipart is blocked by the carried type-error fix (branch
// review-1105-multipart-typeerr, tip 5225acbe: synth_mime_multipart.go:934,
// `os` used as a value) and archive/zip by the carried FixedZone fix
// (branch review-1105-zip-fixedzone, tip ddd8b37d:
// synth_archive_zip.go:2397, time.FixedZone); buildSyntheticPackage
// panics on a type error, so loading either package would crash the
// test binary. The full checkM7DesugarWiring coverage for these five
// rows was verified in the merged checkout (this lane + both carried
// branches) and must replace this table-only pinning once the carried
// branches land — the helper/pair/field values pinned here are the
// same ones that check verifies (pair structs read from the synth
// sources; the subset check in check_mr_table_subset.py re-verifies
// them against the generator on every `make check`).
func TestM7ArchiveMimeDesugarWiring(t *testing.T) {
	if len(m7ArchiveMimeTable) != 6 {
		t.Fatalf("m7ArchiveMimeTable has %d entries, want 6", len(m7ArchiveMimeTable))
	}
	for _, want := range m7ArchiveMimeTable {
		name := want.pkgPath + "." + want.recvType + "." + want.name
		t.Run(name, func(t *testing.T) {
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
			if got.ifaceFallback {
				t.Fatalf("table ifaceFallback = true for %s, want false (interface dispatch for these methods goes through classifyUserIfaceMultiResultCall, not the m7 ifaceFallback retry)", name)
			}
		})
	}
}

func TestM7StrconvDesugarWiring(t *testing.T) {
	if len(m7StrconvTable) != 9 {
		t.Fatalf("m7StrconvTable has %d entries, want 9", len(m7StrconvTable))
	}
	for _, want := range m7StrconvTable {
		name := want.pkgPath + "." + want.name
		if want.recvType != "" {
			name = want.pkgPath + "." + want.recvType + "." + want.name
		}
		t.Run(name, func(t *testing.T) {
			checkM7DesugarWiring(t, want)
		})
	}
}

// TestM7ErrorsAsTypeDesugarWiring pins the m7MultiResultFuncs entry for
// errors.AsType and its synthetic wiring. AsType is the first generic
// entry in the table, so the pair-field check is generic-aware: the
// pair's E and AsType's E are distinct *types.TypeParam objects (never
// types.Identical), so they are compared by name and constraint
// instead. Uses a dedicated checker rather than checkM7DesugarWiring,
// whose step 5 requires Identical field/result types.
func TestM7ErrorsAsTypeDesugarWiring(t *testing.T) {
	// 1. Exactly one table entry for errors.AsType with the expected
	// helper, pair type, and fields in result order.
	var got *m7MultiResultFunc
	for i := range m7MultiResultFuncs {
		mf := &m7MultiResultFuncs[i]
		if mf.pkgPath == "errors" && mf.name == "AsType" && mf.recvType == "" {
			if got != nil {
				t.Fatal("duplicate m7MultiResultFuncs entry for errors.AsType")
			}
			got = mf
		}
	}
	if got == nil {
		t.Fatal("no m7MultiResultFuncs entry for errors.AsType")
	}
	if got.helper != "__asTypePub" {
		t.Fatalf("table helper = %q, want %q", got.helper, "__asTypePub")
	}
	if got.pairType != "asTypePair" {
		t.Fatalf("table pairType = %q, want %q", got.pairType, "asTypePair")
	}
	if len(got.pairFields) != 2 || got.pairFields[0] != "v" || got.pairFields[1] != "ok" {
		t.Fatalf("table pairFields = %v, want [v ok]", got.pairFields)
	}

	// 2. errors.AsType exists in the synthetic package with 2 results.
	sp := getSyntheticPackage("errors")
	if sp == nil || sp.pkg == nil {
		t.Fatal("synthetic package \"errors\" not loaded")
	}
	mrObj := sp.pkg.Scope().Lookup("AsType")
	mrFn, ok := mrObj.(*types.Func)
	if !ok {
		t.Fatalf("synthetic errors.AsType is %T, want *types.Func", mrObj)
	}
	sig, ok := mrFn.Type().(*types.Signature)
	if !ok {
		t.Fatalf("synthetic errors.AsType has type %T, want signature", mrFn.Type())
	}
	if sig.Results().Len() != 2 {
		t.Fatalf("synthetic errors.AsType has %d results, want 2", sig.Results().Len())
	}

	// 3. The __-helper exists and takes AsType's params positionally.
	hobj := sp.pkg.Scope().Lookup("__asTypePub")
	helper, ok := hobj.(*types.Func)
	if !ok {
		t.Fatalf("synthetic errors.__asTypePub is %T, want *types.Func", hobj)
	}
	hsig, ok := helper.Type().(*types.Signature)
	if !ok {
		t.Fatalf("synthetic helper __asTypePub has type %T, want signature", helper.Type())
	}
	if hsig.Params().Len() != sig.Params().Len() {
		t.Fatalf("helper __asTypePub takes %d params, AsType has %d",
			hsig.Params().Len(), sig.Params().Len())
	}
	for i := 0; i < sig.Params().Len(); i++ {
		if !types.Identical(hsig.Params().At(i).Type(), sig.Params().At(i).Type()) {
			t.Fatalf("helper __asTypePub param %d type %v != AsType param type %v",
				i, hsig.Params().At(i).Type(), sig.Params().At(i).Type())
		}
	}
	if hsig.Results().Len() != 1 {
		t.Fatalf("helper __asTypePub has %d results, want 1 (the pair)", hsig.Results().Len())
	}

	// 4. The pair struct has fields (v, ok) in order; ok is bool, and
	// v's type parameter matches AsType's result-0 type parameter by
	// name and constraint.
	pobj := sp.pkg.Scope().Lookup("asTypePair")
	nt, ok := pobj.Type().(*types.Named)
	if !ok {
		t.Fatalf("synthetic asTypePair has type %T, want named struct", pobj.Type())
	}
	st, ok := nt.Underlying().(*types.Struct)
	if !ok {
		t.Fatalf("synthetic asTypePair underlying is %T, want struct", nt.Underlying())
	}
	if st.NumFields() != 2 || st.Field(0).Name() != "v" || st.Field(1).Name() != "ok" {
		t.Fatalf("asTypePair fields = %v, want [v ok]", st.NumFields())
	}
	if !types.Identical(st.Field(1).Type(), sig.Results().At(1).Type()) {
		t.Fatalf("pair field ok type %v != AsType result bool type %v",
			st.Field(1).Type(), sig.Results().At(1).Type())
	}
	fp, ok := st.Field(0).Type().(*types.TypeParam)
	rp, ok2 := sig.Results().At(0).Type().(*types.TypeParam)
	if !ok || !ok2 {
		t.Fatalf("pair field v type %T / AsType result-0 type %T: want both type params",
			st.Field(0).Type(), sig.Results().At(0).Type())
	}
	if fp.Obj().Name() != rp.Obj().Name() ||
		!types.Identical(fp.Constraint(), rp.Constraint()) {
		t.Fatalf("pair field v type param %v (constraint %v) != AsType E %v (constraint %v)",
			fp, fp.Constraint(), rp, rp.Constraint())
	}
}

// checkM7DesugarWiring verifies a single m7MultiResultFuncs table entry
// against its synthetic package. Shared by TestM7StrconvJsonDesugarWiring
// and TestM7CompressDesugarWiring.
func checkM7DesugarWiring(t *testing.T, want m7StrconvJsonExpect) {
	t.Helper()
	name := want.pkgPath + "." + want.name
	if want.recvType != "" {
		name = want.pkgPath + "." + want.recvType + "." + want.name
	}
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

	// Steps 2-5 (synthetic-package verification) are shared with the m9
	// table's checker below.
	checkDesugarWiringSynthetic(t, want)
}

// checkDesugarWiringSynthetic verifies a desugar table expectation
// against its synthetic package (steps 2-5 of checkM7DesugarWiring):
// the multi-result function/method exists, the __-helper exists with
// matching params, and the pair struct's fields match the result types.
// The caller has already pinned the table entry itself.
func checkDesugarWiringSynthetic(t *testing.T, want m7StrconvJsonExpect) {
	t.Helper()
	name := want.pkgPath + "." + want.name
	if want.recvType != "" {
		name = want.pkgPath + "." + want.recvType + "." + want.name
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
}

// TestM8AcceptItabWiring pins the two P3-M9 (x509/TLS lane)
// m8MultiResultFuncs entries that wire net.Listener.Accept interface
// dispatch: (*crypto/tls.listener).Accept -> __listenerAccept and
// (*net.TCPListener).Accept -> __tcpListenerAccept. The second entry was
// missing, which made __listenerAccept's inner l.Listener.Accept()
// dispatch fail with 'missing method "Accept" for dynamic type
// "*net.TCPListener"' (01_handshake legs red in legs-nilmap-fix and
// legs-verify-fix). Reuses the m7 expectation struct and the shared
// synthetic-package checker (steps 2-5); step 1 pins the m8 table entry.
func TestM8AcceptItabWiring(t *testing.T) {
	wants := []m7StrconvJsonExpect{
		{"crypto/tls", "Accept", "listener", "__listenerAccept", "__listenerAcceptPair", []string{"c", "err"}},
		{"net", "Accept", "TCPListener", "__tcpListenerAccept", "acceptPair", []string{"c", "err"}},
	}
	for _, want := range wants {
		name := want.pkgPath + "." + want.recvType + "." + want.name
		t.Run(name, func(t *testing.T) {
			// 1. Exactly one m8MultiResultFuncs entry for
			//    (pkgPath, name, recvType) with the expected wiring.
			var got *osMultiResultFunc
			for i := range m8MultiResultFuncs {
				mf := &m8MultiResultFuncs[i]
				if mf.pkgPath == want.pkgPath && mf.name == want.name && mf.recvType == want.recvType {
					if got != nil {
						t.Fatalf("duplicate m8MultiResultFuncs entry for %s", name)
					}
					got = mf
				}
			}
			if got == nil {
				t.Fatalf("no m8MultiResultFuncs entry for %s", name)
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
			// 2-5. Synthetic-package verification (shared checker).
			checkDesugarWiringSynthetic(t, want)
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
