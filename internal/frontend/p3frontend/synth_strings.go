package p3frontend

// P3-M7 (wave 1): p3frontend mirror of the phase-1 synthetic "strings"
// package. The importer reimplements the frozen exporter's synthetic
// signatures (it cannot reuse them — unexported); this mirror pins the
// signatures so drift fails fast.
//
// The signatures match internal/frontend/synth_strings.go exactly
// (the full gc 1.27.1 in-scope API: ~50 functions plus Builder, Replacer,
// Reader with all methods). Unexported implementation types
// (stringsCaseRange, stringsTrieNode, ...) are not visible to user code
// and are not mirrored.
//
// Deliberate omissions (inexpressible in the synthetic source; documented
// in the phase-1 header): ToUpperSpecial/ToLowerSpecial/ToTitleSpecial
// (need unicode.SpecialCase), Replacer.WriteString and Reader.WriteTo
// (need io.Writer), Lines/SplitAfterSeq/FieldsSeq/FieldsFuncSeq
// (need iter.Seq). SplitSeq LANDED in the phase-1 source (its iter import
// is now available) and is mirrored below. Title is present with exact
// gc signature and a fail-fast body (deprecated; needs unicode tables).

import (
	"fmt"
	"go/token"
	"go/types"
)

// syntheticStringsPackage mirrors the P3-M7 synthetic "strings". It
// returns an error only if the iter.Seq[string] instantiation for
// SplitSeq fails to build (a construction bug, never input-dependent);
// callers propagate it as an import error.
func syntheticStringsPackage() (*types.Package, error) {
	pkg := types.NewPackage("strings", "strings")
	intT := types.Typ[types.Int]
	int64T := types.Typ[types.Int64]
	stringT := types.Typ[types.String]
	boolT := types.Typ[types.Bool]
	// NB: types.Typ[types.Byte] renders as "uint8"; the universe "byte"
	// object renders as the source spells it.
	byteT := types.Universe.Lookup("byte").Type()
	runeT := types.Universe.Lookup("rune").Type()
	errorT := types.Universe.Lookup("error").Type()

	newParam := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	insertFunc := func(name string, params, results []*types.Var, variadic bool) {
		sig := types.NewSignatureType(nil, nil, nil,
			types.NewTuple(params...), types.NewTuple(results...), variadic)
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, name, sig))
	}

	str := []*types.Var{newParam("s", stringT)}
	strRes := []*types.Var{newParam("", stringT)}
	boolRes := []*types.Var{newParam("", boolT)}
	intRes := []*types.Var{newParam("", intT)}

	// ---- Functions: (string, string) -> bool ----
	for _, name := range []string{"Contains", "HasPrefix", "HasSuffix", "EqualFold"} {
		var p []*types.Var
		switch name {
		case "EqualFold":
			p = []*types.Var{newParam("s", stringT), newParam("t", stringT)}
		case "Contains":
			p = []*types.Var{newParam("s", stringT), newParam("substr", stringT)}
		case "HasSuffix":
			// NB: types.Var has no SetName in go1.27 (the old
			// p[1].SetName("suffix") never compiled); name it directly.
			p = []*types.Var{newParam("s", stringT), newParam("suffix", stringT)}
		default:
			p = []*types.Var{newParam("s", stringT), newParam("prefix", stringT)}
		}
		insertFunc(name, p, boolRes, false)
	}

	// ContainsAny(s, chars string) bool
	insertFunc("ContainsAny",
		[]*types.Var{newParam("s", stringT), newParam("chars", stringT)},
		boolRes, false)
	// ContainsRune(s string, r rune) bool
	insertFunc("ContainsRune",
		[]*types.Var{newParam("s", stringT), newParam("r", runeT)},
		boolRes, false)
	// ContainsFunc(s string, f func(rune) bool) bool
	// NB: the func param is unnamed in the phase-1 source (renders as
	// "func(rune) bool"); naming it would drift the rendered signature.
	funcRuneBool := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(newParam("", runeT)), types.NewTuple(newParam("", boolT)), false)
	insertFunc("ContainsFunc",
		[]*types.Var{newParam("s", stringT), newParam("f", funcRuneBool)},
		boolRes, false)

	// Count(s, substr string) int
	insertFunc("Count",
		[]*types.Var{newParam("s", stringT), newParam("substr", stringT)},
		intRes, false)

	// Cut family (multi-result).
	insertFunc("Cut",
		[]*types.Var{newParam("s", stringT), newParam("sep", stringT)},
		[]*types.Var{newParam("before", stringT), newParam("after", stringT), newParam("found", boolT)},
		false)
	insertFunc("CutPrefix",
		[]*types.Var{newParam("s", stringT), newParam("prefix", stringT)},
		[]*types.Var{newParam("after", stringT), newParam("found", boolT)},
		false)
	insertFunc("CutSuffix",
		[]*types.Var{newParam("s", stringT), newParam("suffix", stringT)},
		[]*types.Var{newParam("before", stringT), newParam("found", boolT)},
		false)
	// CutLast(s, sep string) (before, after string, found bool) — gc 1.27.
	insertFunc("CutLast",
		[]*types.Var{newParam("s", stringT), newParam("sep", stringT)},
		[]*types.Var{newParam("before", stringT), newParam("after", stringT), newParam("found", boolT)},
		false)

	// Fields(s string) []string
	strSlice := types.NewSlice(stringT)
	insertFunc("Fields", str, []*types.Var{newParam("", strSlice)}, false)
	// FieldsFunc(s string, f func(rune) bool) []string
	insertFunc("FieldsFunc",
		[]*types.Var{newParam("s", stringT), newParam("f", funcRuneBool)},
		[]*types.Var{newParam("", strSlice)}, false)

	// Index family.
	insertFunc("Index",
		[]*types.Var{newParam("s", stringT), newParam("substr", stringT)},
		intRes, false)
	insertFunc("IndexAny",
		[]*types.Var{newParam("s", stringT), newParam("chars", stringT)},
		intRes, false)
	insertFunc("IndexByte",
		[]*types.Var{newParam("s", stringT), newParam("c", byteT)},
		intRes, false)
	insertFunc("IndexFunc",
		[]*types.Var{newParam("s", stringT), newParam("f", funcRuneBool)},
		intRes, false)
	insertFunc("IndexRune",
		[]*types.Var{newParam("s", stringT), newParam("r", runeT)},
		intRes, false)
	insertFunc("LastIndex",
		[]*types.Var{newParam("s", stringT), newParam("substr", stringT)},
		intRes, false)
	insertFunc("LastIndexAny",
		[]*types.Var{newParam("s", stringT), newParam("chars", stringT)},
		intRes, false)
	insertFunc("LastIndexByte",
		[]*types.Var{newParam("s", stringT), newParam("c", byteT)},
		intRes, false)
	insertFunc("LastIndexFunc",
		[]*types.Var{newParam("s", stringT), newParam("f", funcRuneBool)},
		intRes, false)

	// Join(elems []string, sep string) string
	insertFunc("Join",
		[]*types.Var{newParam("elems", strSlice), newParam("sep", stringT)},
		strRes, false)

	// Map(mapping func(rune) rune, s string) string
	// NB: unnamed func param (see ContainsFunc above).
	funcRuneRune := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(newParam("", runeT)), types.NewTuple(newParam("", runeT)), false)
	insertFunc("Map",
		[]*types.Var{newParam("mapping", funcRuneRune), newParam("s", stringT)},
		strRes, false)

	// Repeat(s string, count int) string
	insertFunc("Repeat",
		[]*types.Var{newParam("s", stringT), newParam("count", intT)},
		strRes, false)

	// Replace(s, old, new string, n int) string; ReplaceAll(s, old, new string) string
	insertFunc("Replace",
		[]*types.Var{newParam("s", stringT), newParam("old", stringT), newParam("new", stringT), newParam("n", intT)},
		strRes, false)
	insertFunc("ReplaceAll",
		[]*types.Var{newParam("s", stringT), newParam("old", stringT), newParam("new", stringT)},
		strRes, false)

	// Split family.
	insertFunc("Split",
		[]*types.Var{newParam("s", stringT), newParam("sep", stringT)},
		[]*types.Var{newParam("", strSlice)}, false)
	insertFunc("SplitAfter",
		[]*types.Var{newParam("s", stringT), newParam("sep", stringT)},
		[]*types.Var{newParam("", strSlice)}, false)
	insertFunc("SplitN",
		[]*types.Var{newParam("s", stringT), newParam("sep", stringT), newParam("n", intT)},
		[]*types.Var{newParam("", strSlice)}, false)
	insertFunc("SplitAfterN",
		[]*types.Var{newParam("s", stringT), newParam("sep", stringT), newParam("n", intT)},
		[]*types.Var{newParam("", strSlice)}, false)

	// SplitSeq(s, sep string) iter.Seq[string]. The mirror is
	// constructor-only (it cannot import the real iter package), so
	// iter.Seq[V any] = func(yield func(V) bool) is redeclared here
	// and instantiated at string; the drift test pins the rendered
	// signature against the phase-1 source.
	iterPkg := types.NewPackage("iter", "iter")
	anyT := types.Universe.Lookup("any").Type()
	vParam := types.NewTypeParam(types.NewTypeName(token.NoPos, iterPkg, "V", nil), anyT)
	yieldFn := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewParam(token.NoPos, iterPkg, "", vParam)),
		types.NewTuple(types.NewParam(token.NoPos, iterPkg, "", boolT)), false)
	seqSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewParam(token.NoPos, iterPkg, "yield", yieldFn)), nil, false)
	seqName := types.NewTypeName(token.NoPos, iterPkg, "Seq", nil)
	seqNamed := types.NewNamed(seqName, seqSig, nil)
	seqNamed.SetTypeParams([]*types.TypeParam{vParam})
	iterPkg.Scope().Insert(seqName)
	iterSeqString, err := types.Instantiate(nil, seqNamed, []types.Type{stringT}, true)
	if err != nil {
		return nil, fmt.Errorf("instantiating iter.Seq[string]: %w", err)
	}
	insertFunc("SplitSeq",
		[]*types.Var{newParam("s", stringT), newParam("sep", stringT)},
		[]*types.Var{newParam("", iterSeqString)}, false)

	// Case mapping.
	for _, name := range []string{"ToUpper", "ToLower", "ToTitle", "Title", "TrimSpace", "Clone"} {
		insertFunc(name, str, strRes, false)
	}
	// ToValidUTF8(s, replacement string) string
	insertFunc("ToValidUTF8",
		[]*types.Var{newParam("s", stringT), newParam("replacement", stringT)},
		strRes, false)

	// Compare(a, b string) int
	insertFunc("Compare",
		[]*types.Var{newParam("a", stringT), newParam("b", stringT)},
		intRes, false)

	// Trim family.
	insertFunc("Trim",
		[]*types.Var{newParam("s", stringT), newParam("cutset", stringT)},
		strRes, false)
	insertFunc("TrimLeft",
		[]*types.Var{newParam("s", stringT), newParam("cutset", stringT)},
		strRes, false)
	insertFunc("TrimRight",
		[]*types.Var{newParam("s", stringT), newParam("cutset", stringT)},
		strRes, false)
	insertFunc("TrimFunc",
		[]*types.Var{newParam("s", stringT), newParam("f", funcRuneBool)},
		strRes, false)
	insertFunc("TrimLeftFunc",
		[]*types.Var{newParam("s", stringT), newParam("f", funcRuneBool)},
		strRes, false)
	insertFunc("TrimRightFunc",
		[]*types.Var{newParam("s", stringT), newParam("f", funcRuneBool)},
		strRes, false)
	insertFunc("TrimPrefix",
		[]*types.Var{newParam("s", stringT), newParam("prefix", stringT)},
		strRes, false)
	insertFunc("TrimSuffix",
		[]*types.Var{newParam("s", stringT), newParam("suffix", stringT)},
		strRes, false)

	// ---- Types ----

	// Builder (struct with unexported fields; methods on *Builder).
	builderName := types.NewTypeName(token.NoPos, pkg, "Builder", nil)
	builder := types.NewNamed(builderName, types.NewStruct(nil, nil), nil)
	pkg.Scope().Insert(builderName)
	builderPtr := types.NewPointer(builder)
	addMethod := func(name string, params, results []*types.Var) {
		sig := types.NewSignatureType(types.NewParam(token.NoPos, pkg, "", builderPtr), nil, nil,
			types.NewTuple(params...), types.NewTuple(results...), false)
		builder.AddMethod(types.NewFunc(token.NoPos, pkg, name, sig))
	}
	addMethod("Len", nil, intRes)
	addMethod("Cap", nil, intRes)
	addMethod("Grow", []*types.Var{newParam("n", intT)}, nil)
	addMethod("Reset", nil, nil)
	addMethod("String", nil, strRes)
	// Write(p []byte) (int, error)
	addMethod("Write",
		[]*types.Var{newParam("p", types.NewSlice(byteT))},
		[]*types.Var{newParam("", intT), newParam("", errorT)})
	// WriteByte(c byte) error
	addMethod("WriteByte",
		[]*types.Var{newParam("c", byteT)},
		[]*types.Var{newParam("", errorT)})
	// WriteRune(r rune) (int, error)
	addMethod("WriteRune",
		[]*types.Var{newParam("r", runeT)},
		[]*types.Var{newParam("", intT), newParam("", errorT)})
	// WriteString(s string) (int, error)
	addMethod("WriteString", str,
		[]*types.Var{newParam("", intT), newParam("", errorT)})

	// Replacer (struct; Replace on *Replacer; NewReplacer variadic).
	replacerName := types.NewTypeName(token.NoPos, pkg, "Replacer", nil)
	replacer := types.NewNamed(replacerName, types.NewStruct(nil, nil), nil)
	pkg.Scope().Insert(replacerName)
	replacerPtr := types.NewPointer(replacer)
	replaceSig := types.NewSignatureType(types.NewParam(token.NoPos, pkg, "", replacerPtr), nil, nil,
		types.NewTuple(str...), types.NewTuple(strRes...), false)
	replacer.AddMethod(types.NewFunc(token.NoPos, pkg, "Replace", replaceSig))
	insertFunc("NewReplacer",
		[]*types.Var{newParam("oldnew", strSlice)},
		[]*types.Var{newParam("", replacerPtr)}, true)

	// Reader (struct; methods on *Reader).
	readerName := types.NewTypeName(token.NoPos, pkg, "Reader", nil)
	reader := types.NewNamed(readerName, types.NewStruct(nil, nil), nil)
	pkg.Scope().Insert(readerName)
	readerPtr := types.NewPointer(reader)
	addReaderMethod := func(name string, params, results []*types.Var) {
		sig := types.NewSignatureType(types.NewParam(token.NoPos, pkg, "", readerPtr), nil, nil,
			types.NewTuple(params...), types.NewTuple(results...), false)
		reader.AddMethod(types.NewFunc(token.NoPos, pkg, name, sig))
	}
	addReaderMethod("Len", nil, intRes)
	addReaderMethod("Size", nil, []*types.Var{newParam("", int64T)})
	addReaderMethod("Reset", str, nil)
	// Read(b []byte) (n int, err error)
	addReaderMethod("Read",
		[]*types.Var{newParam("b", types.NewSlice(byteT))},
		[]*types.Var{newParam("n", intT), newParam("err", errorT)})
	// ReadAt(b []byte, off int64) (n int, err error)
	addReaderMethod("ReadAt",
		[]*types.Var{newParam("b", types.NewSlice(byteT)), newParam("off", int64T)},
		[]*types.Var{newParam("n", intT), newParam("err", errorT)})
	// ReadByte() (byte, error)
	addReaderMethod("ReadByte", nil,
		[]*types.Var{newParam("", byteT), newParam("", errorT)})
	// ReadRune() (ch rune, size int, err error)
	addReaderMethod("ReadRune", nil,
		[]*types.Var{newParam("ch", runeT), newParam("size", intT), newParam("err", errorT)})
	// Seek(offset int64, whence int) (int64, error)
	addReaderMethod("Seek",
		[]*types.Var{newParam("offset", int64T), newParam("whence", intT)},
		[]*types.Var{newParam("", int64T), newParam("", errorT)})
	// UnreadByte() error; UnreadRune() error
	addReaderMethod("UnreadByte", nil, []*types.Var{newParam("", errorT)})
	addReaderMethod("UnreadRune", nil, []*types.Var{newParam("", errorT)})
	insertFunc("NewReader", str, []*types.Var{newParam("", readerPtr)}, false)

	return pkg, nil
}
