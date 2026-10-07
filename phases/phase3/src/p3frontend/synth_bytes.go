package p3frontend

// P3-M7 (wave 1): p3frontend mirror of the phase-1 synthetic "bytes"
// package. The importer reimplements the frozen exporter's synthetic
// signatures (it cannot reuse them — unexported and frozen); this mirror
// pins the signatures so drift fails fast.
//
// The signatures match phases/phase1/frontend/synth_bytes.go exactly
// (not gc's real package — e.g. Buffer.ReadFrom takes the synthetic
// byteReader stand-in, and the iter/SpecialCase members are omitted).
// The drift test (bytes_test.go) compares this mirror method-by-method
// against the type-checked phase-1 synthetic source.

import (
	"go/token"
	"go/types"
)

// syntheticBytesPackage mirrors the P3-M7 synthetic "bytes".
func syntheticBytesPackage() *types.Package {
	pkg := types.NewPackage("bytes", "bytes")
	// NB: types.Typ[types.Byte]/types.Typ[types.Rune] render as
	// "uint8"/"int32"; the universe "byte"/"rune" objects render as the
	// source spells them.
	byteT := types.Universe.Lookup("byte").Type()
	runeT := types.Universe.Lookup("rune").Type()
	intT := types.Typ[types.Int]
	int64T := types.Typ[types.Int64]
	stringT := types.Typ[types.String]
	boolT := types.Typ[types.Bool]
	errorT := types.Universe.Lookup("error").Type()
	bytesSlice := types.NewSlice(byteT)
	bytesSliceSlice := types.NewSlice(bytesSlice)
	runeSlice := types.NewSlice(runeT)

	newParam := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	newField := func(name string, t types.Type) *types.Var {
		return types.NewField(token.NoPos, pkg, name, t, false)
	}
	// runeFunc is func(r rune) bool.
	runeFunc := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(newParam("r", runeT)),
		types.NewTuple(newParam("", boolT)), false)
	// runeMapFunc is func(r rune) rune.
	runeMapFunc := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(newParam("r", runeT)),
		types.NewTuple(newParam("", runeT)), false)

	sig := func(params, results []*types.Var) *types.Signature {
		return types.NewSignatureType(nil, nil, nil,
			types.NewTuple(params...), types.NewTuple(results...), false)
	}

	// readOp = int8 (unexported; Buffer's lastRead field type).
	readOpName := types.NewTypeName(token.NoPos, pkg, "readOp", nil)
	pkg.Scope().Insert(readOpName)
	readOp := types.NewNamed(readOpName, types.Typ[types.Int8], nil)

	// Buffer = struct{ buf []byte; off int; lastRead readOp }.
	bufferName := types.NewTypeName(token.NoPos, pkg, "Buffer", nil)
	pkg.Scope().Insert(bufferName)
	bufferStruct := types.NewStruct(
		[]*types.Var{
			newField("buf", bytesSlice),
			newField("off", intT),
			newField("lastRead", readOp),
		},
		[]string{"buf", "off", "lastRead"})
	bufferType := types.NewNamed(bufferName, bufferStruct, nil)
	bufferPtr := types.NewPointer(bufferType)

	// Reader = struct{ s []byte; i int64; prevRune int }.
	readerName := types.NewTypeName(token.NoPos, pkg, "Reader", nil)
	pkg.Scope().Insert(readerName)
	readerStruct := types.NewStruct(
		[]*types.Var{
			newField("s", bytesSlice),
			newField("i", int64T),
			newField("prevRune", intT),
		},
		[]string{"s", "i", "prevRune"})
	readerType := types.NewNamed(readerName, readerStruct, nil)
	readerPtr := types.NewPointer(readerType)

	// byteReader/byteWriter: the synthetic io.Reader/io.Writer stand-ins
	// (see the synthetic source header; P3-M8 brings io).
	byteReaderName := types.NewTypeName(token.NoPos, pkg, "byteReader", nil)
	pkg.Scope().Insert(byteReaderName)
	byteReaderIface := types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, pkg, "Read",
			types.NewSignatureType(nil, nil, nil,
				types.NewTuple(newParam("p", bytesSlice)),
				types.NewTuple(newParam("n", intT), newParam("err", errorT)), false)),
	}, nil).Complete()
	byteReader := types.NewNamed(byteReaderName, byteReaderIface, nil)

	byteWriterName := types.NewTypeName(token.NoPos, pkg, "byteWriter", nil)
	pkg.Scope().Insert(byteWriterName)
	byteWriterIface := types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, pkg, "Write",
			types.NewSignatureType(nil, nil, nil,
				types.NewTuple(newParam("p", bytesSlice)),
				types.NewTuple(newParam("n", intT), newParam("err", errorT)), false)),
	}, nil).Complete()
	byteWriter := types.NewNamed(byteWriterName, byteWriterIface, nil)

	// method adds a method to a named type.
	method := func(named *types.Named, recv types.Type, name string, params, results []*types.Var) {
		msig := types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", recv),
			nil, nil,
			types.NewTuple(params...), types.NewTuple(results...), false)
		named.AddMethod(types.NewFunc(token.NoPos, pkg, name, msig))
	}

	// Buffer methods (exported).
	bufferMethods := []struct {
		name    string
		params  []*types.Var
		results []*types.Var
	}{
		{"Available", nil, []*types.Var{newParam("", intT)}},
		{"AvailableBuffer", nil, []*types.Var{newParam("", bytesSlice)}},
		{"Bytes", nil, []*types.Var{newParam("", bytesSlice)}},
		{"Cap", nil, []*types.Var{newParam("", intT)}},
		{"Grow", []*types.Var{newParam("n", intT)}, nil},
		{"Len", nil, []*types.Var{newParam("", intT)}},
		{"Next", []*types.Var{newParam("n", intT)}, []*types.Var{newParam("", bytesSlice)}},
		{"Peek", []*types.Var{newParam("n", intT)},
			[]*types.Var{newParam("", bytesSlice), newParam("", errorT)}},
		{"Read", []*types.Var{newParam("p", bytesSlice)},
			[]*types.Var{newParam("n", intT), newParam("err", errorT)}},
		{"ReadByte", nil,
			[]*types.Var{newParam("", byteT), newParam("", errorT)}},
		{"ReadBytes", []*types.Var{newParam("delim", byteT)},
			[]*types.Var{newParam("line", bytesSlice), newParam("err", errorT)}},
		{"ReadFrom", []*types.Var{newParam("r", byteReader)},
			[]*types.Var{newParam("n", int64T), newParam("err", errorT)}},
		{"ReadRune", nil,
			[]*types.Var{newParam("r", runeT), newParam("size", intT), newParam("err", errorT)}},
		{"ReadString", []*types.Var{newParam("delim", byteT)},
			[]*types.Var{newParam("line", stringT), newParam("err", errorT)}},
		{"Reset", nil, nil},
		{"String", nil, []*types.Var{newParam("", stringT)}},
		{"Truncate", []*types.Var{newParam("n", intT)}, nil},
		{"UnreadByte", nil, []*types.Var{newParam("", errorT)}},
		{"UnreadRune", nil, []*types.Var{newParam("", errorT)}},
		{"Write", []*types.Var{newParam("p", bytesSlice)},
			[]*types.Var{newParam("n", intT), newParam("err", errorT)}},
		{"WriteByte", []*types.Var{newParam("c", byteT)},
			[]*types.Var{newParam("", errorT)}},
		{"WriteRune", []*types.Var{newParam("r", runeT)},
			[]*types.Var{newParam("n", intT), newParam("err", errorT)}},
		{"WriteString", []*types.Var{newParam("s", stringT)},
			[]*types.Var{newParam("n", intT), newParam("err", errorT)}},
		{"WriteTo", []*types.Var{newParam("w", byteWriter)},
			[]*types.Var{newParam("n", int64T), newParam("err", errorT)}},
	}
	for _, m := range bufferMethods {
		method(bufferType, bufferPtr, m.name, m.params, m.results)
	}

	// Reader methods (exported).
	readerMethods := []struct {
		name    string
		params  []*types.Var
		results []*types.Var
	}{
		{"Len", nil, []*types.Var{newParam("", intT)}},
		{"Read", []*types.Var{newParam("b", bytesSlice)},
			[]*types.Var{newParam("n", intT), newParam("err", errorT)}},
		{"ReadAt", []*types.Var{newParam("b", bytesSlice), newParam("off", int64T)},
			[]*types.Var{newParam("n", intT), newParam("err", errorT)}},
		{"ReadByte", nil,
			[]*types.Var{newParam("", byteT), newParam("", errorT)}},
		{"ReadRune", nil,
			[]*types.Var{newParam("ch", runeT), newParam("size", intT), newParam("err", errorT)}},
		{"Reset", []*types.Var{newParam("b", bytesSlice)}, nil},
		{"Seek", []*types.Var{newParam("offset", int64T), newParam("whence", intT)},
			[]*types.Var{newParam("", int64T), newParam("", errorT)}},
		{"Size", nil, []*types.Var{newParam("", int64T)}},
		{"UnreadByte", nil, []*types.Var{newParam("", errorT)}},
		{"UnreadRune", nil, []*types.Var{newParam("", errorT)}},
		{"WriteTo", []*types.Var{newParam("w", byteWriter)},
			[]*types.Var{newParam("n", int64T), newParam("err", errorT)}},
	}
	for _, m := range readerMethods {
		method(readerType, readerPtr, m.name, m.params, m.results)
	}

	// Package-level functions.
	insertFunc := func(name string, params, results []*types.Var) {
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, name, sig(params, results)))
	}
	insertFunc("Clone", []*types.Var{newParam("b", bytesSlice)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("Compare",
		[]*types.Var{newParam("a", bytesSlice), newParam("b", bytesSlice)},
		[]*types.Var{newParam("", intT)})
	insertFunc("Contains",
		[]*types.Var{newParam("b", bytesSlice), newParam("subslice", bytesSlice)},
		[]*types.Var{newParam("", boolT)})
	insertFunc("ContainsAny",
		[]*types.Var{newParam("b", bytesSlice), newParam("chars", stringT)},
		[]*types.Var{newParam("", boolT)})
	insertFunc("ContainsFunc",
		[]*types.Var{newParam("b", bytesSlice), newParam("f", runeFunc)},
		[]*types.Var{newParam("", boolT)})
	insertFunc("ContainsRune",
		[]*types.Var{newParam("b", bytesSlice), newParam("r", runeT)},
		[]*types.Var{newParam("", boolT)})
	insertFunc("Count",
		[]*types.Var{newParam("s", bytesSlice), newParam("sep", bytesSlice)},
		[]*types.Var{newParam("", intT)})
	insertFunc("Cut",
		[]*types.Var{newParam("s", bytesSlice), newParam("sep", bytesSlice)},
		[]*types.Var{newParam("before", bytesSlice), newParam("after", bytesSlice), newParam("found", boolT)})
	insertFunc("CutLast",
		[]*types.Var{newParam("s", bytesSlice), newParam("sep", bytesSlice)},
		[]*types.Var{newParam("before", bytesSlice), newParam("after", bytesSlice), newParam("found", boolT)})
	insertFunc("CutPrefix",
		[]*types.Var{newParam("s", bytesSlice), newParam("prefix", bytesSlice)},
		[]*types.Var{newParam("after", bytesSlice), newParam("found", boolT)})
	insertFunc("CutSuffix",
		[]*types.Var{newParam("s", bytesSlice), newParam("suffix", bytesSlice)},
		[]*types.Var{newParam("before", bytesSlice), newParam("found", boolT)})
	insertFunc("Equal",
		[]*types.Var{newParam("a", bytesSlice), newParam("b", bytesSlice)},
		[]*types.Var{newParam("", boolT)})
	insertFunc("EqualFold",
		[]*types.Var{newParam("s", bytesSlice), newParam("t", bytesSlice)},
		[]*types.Var{newParam("", boolT)})
	insertFunc("Fields", []*types.Var{newParam("s", bytesSlice)},
		[]*types.Var{newParam("", bytesSliceSlice)})
	insertFunc("FieldsFunc",
		[]*types.Var{newParam("s", bytesSlice), newParam("f", runeFunc)},
		[]*types.Var{newParam("", bytesSliceSlice)})
	insertFunc("HasPrefix",
		[]*types.Var{newParam("s", bytesSlice), newParam("prefix", bytesSlice)},
		[]*types.Var{newParam("", boolT)})
	insertFunc("HasSuffix",
		[]*types.Var{newParam("s", bytesSlice), newParam("suffix", bytesSlice)},
		[]*types.Var{newParam("", boolT)})
	insertFunc("Index",
		[]*types.Var{newParam("s", bytesSlice), newParam("sep", bytesSlice)},
		[]*types.Var{newParam("", intT)})
	insertFunc("IndexAny",
		[]*types.Var{newParam("s", bytesSlice), newParam("chars", stringT)},
		[]*types.Var{newParam("", intT)})
	insertFunc("IndexByte",
		[]*types.Var{newParam("b", bytesSlice), newParam("c", byteT)},
		[]*types.Var{newParam("", intT)})
	insertFunc("IndexFunc",
		[]*types.Var{newParam("s", bytesSlice), newParam("f", runeFunc)},
		[]*types.Var{newParam("", intT)})
	insertFunc("IndexRune",
		[]*types.Var{newParam("s", bytesSlice), newParam("r", runeT)},
		[]*types.Var{newParam("", intT)})
	insertFunc("Join",
		[]*types.Var{newParam("s", bytesSliceSlice), newParam("sep", bytesSlice)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("LastIndex",
		[]*types.Var{newParam("s", bytesSlice), newParam("sep", bytesSlice)},
		[]*types.Var{newParam("", intT)})
	insertFunc("LastIndexAny",
		[]*types.Var{newParam("s", bytesSlice), newParam("chars", stringT)},
		[]*types.Var{newParam("", intT)})
	insertFunc("LastIndexByte",
		[]*types.Var{newParam("s", bytesSlice), newParam("c", byteT)},
		[]*types.Var{newParam("", intT)})
	insertFunc("LastIndexFunc",
		[]*types.Var{newParam("s", bytesSlice), newParam("f", runeFunc)},
		[]*types.Var{newParam("", intT)})
	insertFunc("Map",
		[]*types.Var{newParam("mapping", runeMapFunc), newParam("s", bytesSlice)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("NewBuffer", []*types.Var{newParam("buf", bytesSlice)},
		[]*types.Var{newParam("", bufferPtr)})
	insertFunc("NewBufferString", []*types.Var{newParam("s", stringT)},
		[]*types.Var{newParam("", bufferPtr)})
	insertFunc("NewReader", []*types.Var{newParam("b", bytesSlice)},
		[]*types.Var{newParam("", readerPtr)})
	insertFunc("Repeat",
		[]*types.Var{newParam("b", bytesSlice), newParam("count", intT)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("Replace",
		[]*types.Var{newParam("s", bytesSlice), newParam("old", bytesSlice),
			newParam("new", bytesSlice), newParam("n", intT)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("ReplaceAll",
		[]*types.Var{newParam("s", bytesSlice), newParam("old", bytesSlice),
			newParam("new", bytesSlice)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("Runes", []*types.Var{newParam("s", bytesSlice)},
		[]*types.Var{newParam("", runeSlice)})
	insertFunc("Split",
		[]*types.Var{newParam("s", bytesSlice), newParam("sep", bytesSlice)},
		[]*types.Var{newParam("", bytesSliceSlice)})
	insertFunc("SplitAfter",
		[]*types.Var{newParam("s", bytesSlice), newParam("sep", bytesSlice)},
		[]*types.Var{newParam("", bytesSliceSlice)})
	insertFunc("SplitAfterN",
		[]*types.Var{newParam("s", bytesSlice), newParam("sep", bytesSlice),
			newParam("n", intT)},
		[]*types.Var{newParam("", bytesSliceSlice)})
	insertFunc("SplitN",
		[]*types.Var{newParam("s", bytesSlice), newParam("sep", bytesSlice),
			newParam("n", intT)},
		[]*types.Var{newParam("", bytesSliceSlice)})
	insertFunc("Title", []*types.Var{newParam("s", bytesSlice)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("ToLower", []*types.Var{newParam("s", bytesSlice)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("ToTitle", []*types.Var{newParam("s", bytesSlice)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("ToUpper", []*types.Var{newParam("s", bytesSlice)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("ToValidUTF8",
		[]*types.Var{newParam("s", bytesSlice), newParam("replacement", bytesSlice)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("Trim",
		[]*types.Var{newParam("s", bytesSlice), newParam("cutset", stringT)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("TrimFunc",
		[]*types.Var{newParam("s", bytesSlice), newParam("f", runeFunc)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("TrimLeft",
		[]*types.Var{newParam("s", bytesSlice), newParam("cutset", stringT)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("TrimLeftFunc",
		[]*types.Var{newParam("s", bytesSlice), newParam("f", runeFunc)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("TrimPrefix",
		[]*types.Var{newParam("s", bytesSlice), newParam("prefix", bytesSlice)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("TrimRight",
		[]*types.Var{newParam("s", bytesSlice), newParam("cutset", stringT)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("TrimRightFunc",
		[]*types.Var{newParam("s", bytesSlice), newParam("f", runeFunc)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("TrimSpace", []*types.Var{newParam("s", bytesSlice)},
		[]*types.Var{newParam("", bytesSlice)})
	insertFunc("TrimSuffix",
		[]*types.Var{newParam("s", bytesSlice), newParam("suffix", bytesSlice)},
		[]*types.Var{newParam("", bytesSlice)})

	// Package-level var and const.
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "ErrTooLarge", errorT))
	pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, "MinRead",
		types.Typ[types.UntypedInt], constantValue(512)))

	pkg.MarkComplete()
	return pkg
}
