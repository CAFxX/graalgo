// Retirement mirror: see ../../../docs/arch/synth-retirement-tracker.md#encodingcsv (row for encoding/csv; this file mirrors its signatures).
// Retirement mirror: see ../../../docs/arch/synth-retirement-tracker.md#encodinggob (row for encoding/gob; this file mirrors its signatures).
package p3frontend

// P3-M7 (wave 1): p3frontend mirrors of the phase-1 synthetic
// "encoding/csv" and "encoding/gob" packages. The importer reimplements
// the frozen exporter's synthetic signatures (it cannot reuse them —
// unexported and frozen); these mirrors pin the signatures so drift
// fails fast.
//
// The signatures match internal/frontend/synth_enc_csv_gob.go
// exactly.

import (
	"go/token"
	"go/types"
)

// newSig is a helper for types.NewSignatureType with no type params.
func newSig(recv *types.Var, params, results *types.Tuple) *types.Signature {
	return types.NewSignatureType(recv, nil, nil, params, results, false)
}

// syntheticCsvPackage mirrors the frozen P3-M7 synthetic "encoding/csv".
func syntheticCsvPackage() *types.Package {
	pkg := types.NewPackage("encoding/csv", "csv")
	intT := types.Typ[types.Int]
	int64T := types.Typ[types.Int64]
	stringT := types.Typ[types.String]
	boolT := types.Typ[types.Bool]
	runeT := types.Universe.Lookup("rune").Type()
	errorT := types.Universe.Lookup("error").Type()
	// NB: types.Typ[types.Byte] renders as "uint8"; the universe "byte"
	// object renders as the source spells it (cf. synth_bytes.go).
	byteSliceT := types.NewSlice(types.Universe.Lookup("byte").Type())
	stringSliceT := types.NewSlice(stringT)
	stringSliceSliceT := types.NewSlice(stringSliceT)

	newParam := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	newField := func(name string, t types.Type) *types.Var {
		return types.NewField(token.NoPos, pkg, name, t, false)
	}
	tuple := func(vars ...*types.Var) *types.Tuple {
		return types.NewTuple(vars...)
	}

	// ParseError struct.
	peName := types.NewTypeName(token.NoPos, pkg, "ParseError", nil)
	pkg.Scope().Insert(peName)
	peNamed := types.NewNamed(peName, types.NewStruct([]*types.Var{
		newField("StartLine", intT),
		newField("Line", intT),
		newField("Column", intT),
		newField("Err", errorT),
	}, nil), nil)
	pePtr := types.NewPointer(peNamed)
	peNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Error",
		newSig(types.NewParam(token.NoPos, pkg, "e", pePtr),
			tuple(), tuple(newParam("", stringT)))))
	peNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Unwrap",
		newSig(types.NewParam(token.NoPos, pkg, "e", pePtr),
			tuple(), tuple(newParam("", errorT)))))

	// Error vars.
	for _, name := range []string{"ErrBareQuote", "ErrQuote", "ErrFieldCount", "ErrTrailingComma"} {
		pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, name, errorT))
	}

	// Reader struct.
	rName := types.NewTypeName(token.NoPos, pkg, "Reader", nil)
	pkg.Scope().Insert(rName)
	rNamed := types.NewNamed(rName, types.NewStruct([]*types.Var{
		newField("Comma", runeT),
		newField("Comment", runeT),
		newField("FieldsPerRecord", intT),
		newField("LazyQuotes", boolT),
		newField("TrimLeadingSpace", boolT),
		newField("ReuseRecord", boolT),
		newField("TrailingComma", boolT),
	}, nil), nil)
	rPtr := types.NewPointer(rNamed)
	rRecv := types.NewParam(token.NoPos, pkg, "r", rPtr)
	rNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Read",
		newSig(rRecv, tuple(), tuple(newParam("", stringSliceT), newParam("", errorT)))))
	rNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "ReadAll",
		newSig(rRecv, tuple(), tuple(newParam("", stringSliceSliceT), newParam("", errorT)))))
	rNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "FieldPos",
		newSig(rRecv, tuple(newParam("field", intT)),
			tuple(newParam("line", intT), newParam("column", intT)))))
	rNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "InputOffset",
		newSig(rRecv, tuple(), tuple(newParam("", int64T)))))

	// Writer struct.
	wName := types.NewTypeName(token.NoPos, pkg, "Writer", nil)
	pkg.Scope().Insert(wName)
	wNamed := types.NewNamed(wName, types.NewStruct([]*types.Var{
		newField("Comma", runeT),
		newField("UseCRLF", boolT),
	}, nil), nil)
	wPtr := types.NewPointer(wNamed)
	wRecv := types.NewParam(token.NoPos, pkg, "w", wPtr)
	wNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Write",
		newSig(wRecv, tuple(newParam("record", stringSliceT)), tuple(newParam("", errorT)))))
	wNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "WriteAll",
		newSig(wRecv, tuple(newParam("records", stringSliceSliceT)), tuple(newParam("", errorT)))))
	wNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Flush",
		newSig(wRecv, tuple(), tuple())))
	wNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Error",
		newSig(wRecv, tuple(), tuple(newParam("", errorT)))))
	wNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Bytes",
		newSig(wRecv, tuple(), tuple(newParam("", byteSliceT)))))

	// Package-level functions.
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "NewReaderString",
		newSig(nil, tuple(newParam("s", stringT)), tuple(newParam("", rPtr)))))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "NewBufferWriter",
		newSig(nil, tuple(), tuple(newParam("", wPtr)))))
	// NewReader/NewWriter (exact gc signatures): the params are the
	// synthetic io package's Reader/Writer interfaces (shared mirror —
	// the same objects the bytes/strings mirrors use for
	// io.Writer/io.Reader), so signatures render as "io.Reader"/
	// "io.Writer" under the drift test's qualifier and the importer
	// checks assignability against the real method sets.
	ioPkg := syntheticIoPackage()
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "NewReader",
		newSig(nil, tuple(newParam("r", ioReader(ioPkg))),
			tuple(newParam("", rPtr)))))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "NewWriter",
		newSig(nil, tuple(newParam("w", ioWriter(ioPkg))),
			tuple(newParam("", wPtr)))))

	// Pair structs for desugar.
	for _, p := range []struct {
		name   string
		fields []*types.Var
	}{
		{"csvReadPair", []*types.Var{newField("rec", stringSliceT), newField("err", errorT)}},
		{"csvReadAllPair", []*types.Var{newField("recs", stringSliceSliceT), newField("err", errorT)}},
		{"csvFieldPosPair", []*types.Var{newField("line", intT), newField("column", intT)}},
	} {
		tn := types.NewTypeName(token.NoPos, pkg, p.name, nil)
		pkg.Scope().Insert(tn)
		types.NewNamed(tn, types.NewStruct(p.fields, nil), nil)
	}

	return pkg
}

// syntheticGobPackage mirrors the frozen P3-M7 synthetic "encoding/gob".
func syntheticGobPackage() *types.Package {
	pkg := types.NewPackage("encoding/gob", "gob")
	stringT := types.Typ[types.String]
	errorT := types.Universe.Lookup("error").Type()
	anyT := types.Universe.Lookup("any").Type()
	// NB: types.Typ[types.Byte] renders as "uint8"; the universe "byte"
	// object renders as the source spells it (cf. synth_bytes.go).
	byteSliceT := types.NewSlice(types.Universe.Lookup("byte").Type())

	newParam := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	tuple := func(vars ...*types.Var) *types.Tuple {
		return types.NewTuple(vars...)
	}

	// Encoder struct.
	encName := types.NewTypeName(token.NoPos, pkg, "Encoder", nil)
	pkg.Scope().Insert(encName)
	encNamed := types.NewNamed(encName, types.NewStruct(nil, nil), nil)
	encPtr := types.NewPointer(encNamed)
	encRecv := types.NewParam(token.NoPos, pkg, "enc", encPtr)
	encNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Encode",
		newSig(encRecv, tuple(newParam("v", anyT)), tuple(newParam("", errorT)))))
	encNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Bytes",
		newSig(encRecv, tuple(), tuple(newParam("", byteSliceT)))))

	// Decoder struct.
	decName := types.NewTypeName(token.NoPos, pkg, "Decoder", nil)
	pkg.Scope().Insert(decName)
	decNamed := types.NewNamed(decName, types.NewStruct(nil, nil), nil)
	decPtr := types.NewPointer(decNamed)
	decRecv := types.NewParam(token.NoPos, pkg, "dec", decPtr)
	decNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Decode",
		newSig(decRecv, tuple(newParam("e", anyT)), tuple(newParam("", errorT)))))

	// Package-level functions.
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "NewBufferEncoder",
		newSig(nil, tuple(), tuple(newParam("", encPtr)))))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "NewBufferDecoder",
		newSig(nil, tuple(newParam("data", byteSliceT)), tuple(newParam("", decPtr)))))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "Register",
		newSig(nil, tuple(newParam("value", anyT)), tuple())))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "RegisterName",
		newSig(nil, tuple(newParam("name", stringT), newParam("value", anyT)), tuple())))

	return pkg
}
