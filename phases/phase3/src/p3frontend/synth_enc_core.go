package p3frontend

// P3-M7 (wave 1): p3frontend mirrors of the phase-1 synthetic encoding/*
// primitive packages (m7-wenc-core). The importer reimplements the frozen
// exporter's synthetic signatures (it cannot reuse them — unexported);
// these mirrors pin the signatures so drift fails fast.
//
// The signatures match phases/phase1/frontend/synth_{base32,base64,hex,
// binary,ascii85,pem,asn1,encoding}.go exactly. Unexported implementation
// types (base32DecodePair, __ helpers, ...) are not visible to user code
// and are not mirrored — except the Appendix M pair/triple structs,
// which the mirrors carry as unexported TypeNames (the csv/gob precedent:
// they document the desugar contract; the drift test skips unexported
// names). Multi-result members (Decode, Uvarint, ...) keep their exact
// gc signatures in the mirror; they fail fast at call sites (WS D) or
// desugar via Appendix M (mrM7) in the phase-1 exporter.
//
// encoding/pem's synthetic source imports synthetic encoding/base64;
// the importer case ensures base64 is built and cached first (the
// context->time precedent).

import (
	"go/token"
	"go/types"
)

// encCoreHelpers bundles the shared builders for the encoding mirrors.
type encCoreHelpers struct {
	pkg *types.Package
}

func newEncCoreHelpers(pkg *types.Package) *encCoreHelpers {
	return &encCoreHelpers{pkg: pkg}
}

func (h *encCoreHelpers) param(name string, t types.Type) *types.Var {
	return types.NewParam(token.NoPos, h.pkg, name, t)
}

func (h *encCoreHelpers) field(name string, t types.Type, embedded bool) *types.Var {
	return types.NewField(token.NoPos, h.pkg, name, t, embedded)
}

// insertFunc adds a package-level function.
func (h *encCoreHelpers) insertFunc(name string, params, results []*types.Var, variadic bool) {
	sig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(params...), types.NewTuple(results...), variadic)
	h.pkg.Scope().Insert(types.NewFunc(token.NoPos, h.pkg, name, sig))
}

// method adds a method to a named type (recv is the receiver type,
// e.g. the pointer for *Encoding methods).
func (h *encCoreHelpers) method(named *types.Named, recv types.Type, name string, params, results []*types.Var) {
	msig := types.NewSignatureType(
		types.NewParam(token.NoPos, h.pkg, "", recv),
		nil, nil,
		types.NewTuple(params...), types.NewTuple(results...), false)
	named.AddMethod(types.NewFunc(token.NoPos, h.pkg, name, msig))
}

// insertPair adds an unexported Appendix M pair/triple struct TypeName.
func (h *encCoreHelpers) insertPair(name string, fields []*types.Var) *types.Named {
	tn := types.NewTypeName(token.NoPos, h.pkg, name, nil)
	h.pkg.Scope().Insert(tn)
	return types.NewNamed(tn, types.NewStruct(fields, nil), nil)
}

// encodingSharedTypes are the types shared by the base32/base64 mirrors.
type encodingSharedTypes struct {
	byteT, runeT, intT     types.Type
	stringT, boolT, errorT types.Type
	byteSliceT            types.Type
}

func encShared() *encodingSharedTypes {
	byteT := types.Universe.Lookup("byte").Type()
	return &encodingSharedTypes{
		byteT:      byteT,
		runeT:      types.Universe.Lookup("rune").Type(),
		intT:       types.Typ[types.Int],
		stringT:    types.Typ[types.String],
		boolT:      types.Typ[types.Bool],
		errorT:     types.Universe.Lookup("error").Type(),
		byteSliceT: types.NewSlice(byteT),
	}
}

// syntheticBase32Package mirrors the P3-M7 synthetic "encoding/base32".
func syntheticBase32Package() *types.Package {
	// ioStreamingIfaces builds the minimal synthetic "io" package (P3-M8)
	// for the base32 streaming signatures: the Writer/Reader/WriteCloser
	// named interfaces with exact gc 1.27.1 method sets. Only the
	// "io.X" rendering matters for the drift comparison.
	ioStreamingIfaces := func() (writer, reader, writeCloser *types.Named) {
		pkg := types.NewPackage("io", "io")
		byteSliceT := types.NewSlice(types.Typ[types.Byte])
		intT := types.Typ[types.Int]
		errT := types.Universe.Lookup("error").Type()
		newParam := func(name string, t types.Type) *types.Var {
			return types.NewParam(token.NoPos, pkg, name, t)
		}
		newMethod := func(name string, params, results []*types.Var) *types.Func {
			sig := types.NewSignatureType(nil, nil, nil,
				types.NewTuple(params...), types.NewTuple(results...), false)
			return types.NewFunc(token.NoPos, pkg, name, sig)
		}
		write := newMethod("Write",
			[]*types.Var{newParam("p", byteSliceT)},
			[]*types.Var{newParam("n", intT), newParam("err", errT)})
		read := newMethod("Read",
			[]*types.Var{newParam("p", byteSliceT)},
			[]*types.Var{newParam("n", intT), newParam("err", errT)})
		close := newMethod("Close", nil,
			[]*types.Var{newParam("err", errT)})
		mk := func(name string, methods []*types.Func) *types.Named {
			tn := types.NewTypeName(token.NoPos, pkg, name, nil)
			pkg.Scope().Insert(tn)
			return types.NewNamed(tn, types.NewInterfaceType(methods, nil).Complete(), nil)
		}
		writer = mk("Writer", []*types.Func{write})
		reader = mk("Reader", []*types.Func{read})
		writeCloser = mk("WriteCloser", []*types.Func{write, close})
		pkg.MarkComplete()
		return writer, reader, writeCloser
	}
	pkg := types.NewPackage("encoding/base32", "base32")
	h := newEncCoreHelpers(pkg)
	st := encShared()

	// CorruptInputError = int64, with Error() string.
	cieName := types.NewTypeName(token.NoPos, pkg, "CorruptInputError", nil)
	pkg.Scope().Insert(cieName)
	cieNamed := types.NewNamed(cieName, types.Typ[types.Int64], nil)
	h.method(cieNamed, cieNamed, "Error", nil,
		[]*types.Var{h.param("", st.stringT)})

	// Encoding = struct{ encode [32]byte; decodeMap [256]uint8; padChar rune }.
	encName := types.NewTypeName(token.NoPos, pkg, "Encoding", nil)
	pkg.Scope().Insert(encName)
	encStruct := types.NewStruct([]*types.Var{
		h.field("encode", types.NewArray(types.Typ[types.Byte], 32), false),
		h.field("decodeMap", types.NewArray(types.Typ[types.Uint8], 256), false),
		h.field("padChar", st.runeT, false),
	}, nil)
	encNamed := types.NewNamed(encName, encStruct, nil)
	encPtr := types.NewPointer(encNamed)

	dst := h.param("dst", st.byteSliceT)
	src := h.param("src", st.byteSliceT)
	// Methods on *Encoding (gc 1.27.1 signatures; Decode/DecodeString/
	// AppendDecode are multi-result and desugar via mrM7).
	h.method(encNamed, encPtr, "WithPadding",
		[]*types.Var{h.param("padding", st.runeT)},
		[]*types.Var{h.param("", encPtr)})
	h.method(encNamed, encPtr, "Encode",
		[]*types.Var{dst, src}, nil)
	h.method(encNamed, encPtr, "AppendEncode",
		[]*types.Var{dst, src},
		[]*types.Var{h.param("", st.byteSliceT)})
	h.method(encNamed, encPtr, "EncodeToString",
		[]*types.Var{h.param("src", st.byteSliceT)},
		[]*types.Var{h.param("", st.stringT)})
	h.method(encNamed, encPtr, "EncodedLen",
		[]*types.Var{h.param("n", st.intT)},
		[]*types.Var{h.param("", st.intT)})
	h.method(encNamed, encPtr, "Decode",
		[]*types.Var{dst, src},
		[]*types.Var{h.param("n", st.intT), h.param("err", st.errorT)})
	h.method(encNamed, encPtr, "DecodeString",
		[]*types.Var{h.param("s", st.stringT)},
		[]*types.Var{h.param("", st.byteSliceT), h.param("", st.errorT)})
	h.method(encNamed, encPtr, "AppendDecode",
		[]*types.Var{dst, src},
		[]*types.Var{h.param("", st.byteSliceT), h.param("", st.errorT)})
	h.method(encNamed, encPtr, "DecodedLen",
		[]*types.Var{h.param("n", st.intT)},
		[]*types.Var{h.param("", st.intT)})

	// NewEncoding(encoder string) *Encoding.
	h.insertFunc("NewEncoding",
		[]*types.Var{h.param("encoder", st.stringT)},
		[]*types.Var{h.param("", encPtr)}, false)

	// Consts: StdPadding rune = '=', NoPadding rune = -1.
	runeT := types.Universe.Lookup("rune").Type()
	pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, "StdPadding",
		runeT, constantValue('=')))
	pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, "NoPadding",
		runeT, constantValue(-1)))

	// P3-M8: streaming API (exact gc 1.27.1 signatures; the encoder/decoder
	// types are unexported, their Read/Write/Close fail fast — WS D).
	ioWriter, ioReader, ioWriteCloser := ioStreamingIfaces()
	h.insertFunc("NewEncoder",
		[]*types.Var{h.param("enc", encPtr), h.param("w", ioWriter)},
		[]*types.Var{h.param("", ioWriteCloser)}, false)
	h.insertFunc("NewDecoder",
		[]*types.Var{h.param("enc", encPtr), h.param("r", ioReader)},
		[]*types.Var{h.param("", ioReader)}, false)

	// Vars: StdEncoding, HexEncoding *Encoding.
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "StdEncoding", encPtr))
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "HexEncoding", encPtr))

	// Appendix M pair structs (unexported).
	h.insertPair("base32DecodePair", []*types.Var{
		h.field("n", st.intT, false), h.field("err", st.errorT, false),
	})
	h.insertPair("base32DecodeStringPair", []*types.Var{
		h.field("b", st.byteSliceT, false), h.field("err", st.errorT, false),
	})

	pkg.MarkComplete()
	return pkg
}

// syntheticBase64Package mirrors the P3-M7 synthetic "encoding/base64".
func syntheticBase64Package() *types.Package {
	pkg := types.NewPackage("encoding/base64", "base64")
	h := newEncCoreHelpers(pkg)
	st := encShared()

	// CorruptInputError = int64, with Error() string.
	cieName := types.NewTypeName(token.NoPos, pkg, "CorruptInputError", nil)
	pkg.Scope().Insert(cieName)
	cieNamed := types.NewNamed(cieName, types.Typ[types.Int64], nil)
	h.method(cieNamed, cieNamed, "Error", nil,
		[]*types.Var{h.param("", st.stringT)})

	// Encoding = struct{ encode [64]byte; decodeMap [256]uint8;
	// padChar rune; strict bool }.
	encName := types.NewTypeName(token.NoPos, pkg, "Encoding", nil)
	pkg.Scope().Insert(encName)
	encStruct := types.NewStruct([]*types.Var{
		h.field("encode", types.NewArray(types.Typ[types.Byte], 64), false),
		h.field("decodeMap", types.NewArray(types.Typ[types.Uint8], 256), false),
		h.field("padChar", st.runeT, false),
		h.field("strict", st.boolT, false),
	}, nil)
	encNamed := types.NewNamed(encName, encStruct, nil)
	encPtr := types.NewPointer(encNamed)

	dst := h.param("dst", st.byteSliceT)
	src := h.param("src", st.byteSliceT)
	h.method(encNamed, encPtr, "WithPadding",
		[]*types.Var{h.param("padding", st.runeT)},
		[]*types.Var{h.param("", encPtr)})
	h.method(encNamed, encPtr, "Strict", nil,
		[]*types.Var{h.param("", encPtr)})
	h.method(encNamed, encPtr, "Encode",
		[]*types.Var{dst, src}, nil)
	h.method(encNamed, encPtr, "AppendEncode",
		[]*types.Var{dst, src},
		[]*types.Var{h.param("", st.byteSliceT)})
	h.method(encNamed, encPtr, "EncodeToString",
		[]*types.Var{h.param("src", st.byteSliceT)},
		[]*types.Var{h.param("", st.stringT)})
	h.method(encNamed, encPtr, "EncodedLen",
		[]*types.Var{h.param("n", st.intT)},
		[]*types.Var{h.param("", st.intT)})
	h.method(encNamed, encPtr, "Decode",
		[]*types.Var{dst, src},
		[]*types.Var{h.param("n", st.intT), h.param("err", st.errorT)})
	h.method(encNamed, encPtr, "DecodeString",
		[]*types.Var{h.param("s", st.stringT)},
		[]*types.Var{h.param("", st.byteSliceT), h.param("", st.errorT)})
	h.method(encNamed, encPtr, "AppendDecode",
		[]*types.Var{dst, src},
		[]*types.Var{h.param("", st.byteSliceT), h.param("", st.errorT)})
	h.method(encNamed, encPtr, "DecodedLen",
		[]*types.Var{h.param("n", st.intT)},
		[]*types.Var{h.param("", st.intT)})

	// NewEncoding(encoder string) *Encoding.
	h.insertFunc("NewEncoding",
		[]*types.Var{h.param("encoder", st.stringT)},
		[]*types.Var{h.param("", encPtr)}, false)

	// Consts: StdPadding rune = '=', NoPadding rune = -1.
	runeT := types.Universe.Lookup("rune").Type()
	pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, "StdPadding",
		runeT, constantValue('=')))
	pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, "NoPadding",
		runeT, constantValue(-1)))

	// Vars: StdEncoding, URLEncoding, RawStdEncoding, RawURLEncoding.
	for _, v := range []string{"StdEncoding", "URLEncoding", "RawStdEncoding", "RawURLEncoding"} {
		pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, v, encPtr))
	}

	// Appendix M pair structs (unexported).
	h.insertPair("base64DecodePair", []*types.Var{
		h.field("n", st.intT, false), h.field("err", st.errorT, false),
	})
	h.insertPair("base64DecodeStringPair", []*types.Var{
		h.field("b", st.byteSliceT, false), h.field("err", st.errorT, false),
	})

	pkg.MarkComplete()
	return pkg
}

// syntheticHexPackage mirrors the P3-M7 synthetic "encoding/hex".
func syntheticHexPackage() *types.Package {
	pkg := types.NewPackage("encoding/hex", "hex")
	h := newEncCoreHelpers(pkg)
	st := encShared()

	dst := h.param("dst", st.byteSliceT)
	src := h.param("src", st.byteSliceT)

	h.insertFunc("EncodedLen",
		[]*types.Var{h.param("n", st.intT)},
		[]*types.Var{h.param("", st.intT)}, false)
	h.insertFunc("Encode",
		[]*types.Var{dst, src},
		[]*types.Var{h.param("", st.intT)}, false)
	h.insertFunc("AppendEncode",
		[]*types.Var{dst, src},
		[]*types.Var{h.param("", st.byteSliceT)}, false)
	h.insertFunc("DecodedLen",
		[]*types.Var{h.param("x", st.intT)},
		[]*types.Var{h.param("", st.intT)}, false)
	// Multi-result: desugar via mrM7.
	h.insertFunc("Decode",
		[]*types.Var{dst, src},
		[]*types.Var{h.param("", st.intT), h.param("", st.errorT)}, false)
	h.insertFunc("AppendDecode",
		[]*types.Var{dst, src},
		[]*types.Var{h.param("", st.byteSliceT), h.param("", st.errorT)}, false)
	h.insertFunc("EncodeToString",
		[]*types.Var{h.param("src", st.byteSliceT)},
		[]*types.Var{h.param("", st.stringT)}, false)
	h.insertFunc("DecodeString",
		[]*types.Var{h.param("s", st.stringT)},
		[]*types.Var{h.param("", st.byteSliceT), h.param("", st.errorT)}, false)
	h.insertFunc("Dump",
		[]*types.Var{h.param("data", st.byteSliceT)},
		[]*types.Var{h.param("", st.stringT)}, false)

	// InvalidByteError = byte, with Error() string.
	ibeName := types.NewTypeName(token.NoPos, pkg, "InvalidByteError", nil)
	pkg.Scope().Insert(ibeName)
	ibeNamed := types.NewNamed(ibeName, st.byteT, nil)
	h.method(ibeNamed, ibeNamed, "Error", nil,
		[]*types.Var{h.param("", st.stringT)})

	// hexErrorString (unexported): struct{ s string }, Error() on
	// *hexErrorString. ErrLength is *hexErrorString.
	hesName := types.NewTypeName(token.NoPos, pkg, "hexErrorString", nil)
	pkg.Scope().Insert(hesName)
	hesNamed := types.NewNamed(hesName, types.NewStruct([]*types.Var{
		h.field("s", st.stringT, false),
	}, nil), nil)
	hesPtr := types.NewPointer(hesNamed)
	h.method(hesNamed, hesPtr, "Error", nil,
		[]*types.Var{h.param("", st.stringT)})
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "ErrLength", hesPtr))

	// Appendix M pair structs (unexported).
	h.insertPair("hexDecodePair", []*types.Var{
		h.field("n", st.intT, false), h.field("err", st.errorT, false),
	})
	h.insertPair("hexDecodeStringPair", []*types.Var{
		h.field("b", st.byteSliceT, false), h.field("err", st.errorT, false),
	})

	pkg.MarkComplete()
	return pkg
}

// syntheticBinaryPackage mirrors the P3-M7 synthetic "encoding/binary".
func syntheticBinaryPackage() *types.Package {
	pkg := types.NewPackage("encoding/binary", "binary")
	h := newEncCoreHelpers(pkg)
	st := encShared()
	uint16T := types.Typ[types.Uint16]
	uint32T := types.Typ[types.Uint32]
	uint64T := types.Typ[types.Uint64]
	int64T := types.Typ[types.Int64]
	anyT := types.Universe.Lookup("any").Type()

	b := h.param("b", st.byteSliceT)
	buf := h.param("buf", st.byteSliceT)

	// ByteOrder interface (params unnamed in the source).
	byteOrderObj := types.NewTypeName(token.NoPos, pkg, "ByteOrder", nil)
	pkg.Scope().Insert(byteOrderObj)
	mkIfaceMethod := func(name string, params, results []*types.Var) *types.Func {
		return types.NewFunc(token.NoPos, pkg, name,
			types.NewSignatureType(nil, nil, nil,
				types.NewTuple(params...), types.NewTuple(results...), false))
	}
	byteOrderIface := types.NewInterfaceType([]*types.Func{
		mkIfaceMethod("Uint16",
			[]*types.Var{h.param("", st.byteSliceT)},
			[]*types.Var{h.param("", uint16T)}),
		mkIfaceMethod("Uint32",
			[]*types.Var{h.param("", st.byteSliceT)},
			[]*types.Var{h.param("", uint32T)}),
		mkIfaceMethod("Uint64",
			[]*types.Var{h.param("", st.byteSliceT)},
			[]*types.Var{h.param("", uint64T)}),
		mkIfaceMethod("PutUint16",
			[]*types.Var{h.param("", st.byteSliceT), h.param("", uint16T)}, nil),
		mkIfaceMethod("PutUint32",
			[]*types.Var{h.param("", st.byteSliceT), h.param("", uint32T)}, nil),
		mkIfaceMethod("PutUint64",
			[]*types.Var{h.param("", st.byteSliceT), h.param("", uint64T)}, nil),
		mkIfaceMethod("String", nil,
			[]*types.Var{h.param("", st.stringT)}),
	}, nil).Complete()
	byteOrderNamed := types.NewNamed(byteOrderObj, byteOrderIface, nil)

	// AppendByteOrder interface (explicit methods, no embedding, per the
	// synthetic source).
	appendByteOrderObj := types.NewTypeName(token.NoPos, pkg, "AppendByteOrder", nil)
	pkg.Scope().Insert(appendByteOrderObj)
	appendByteOrderIface := types.NewInterfaceType([]*types.Func{
		mkIfaceMethod("AppendUint16",
			[]*types.Var{h.param("", st.byteSliceT), h.param("", uint16T)},
			[]*types.Var{h.param("", st.byteSliceT)}),
		mkIfaceMethod("AppendUint32",
			[]*types.Var{h.param("", st.byteSliceT), h.param("", uint32T)},
			[]*types.Var{h.param("", st.byteSliceT)}),
		mkIfaceMethod("AppendUint64",
			[]*types.Var{h.param("", st.byteSliceT), h.param("", uint64T)},
			[]*types.Var{h.param("", st.byteSliceT)}),
		mkIfaceMethod("String", nil,
			[]*types.Var{h.param("", st.stringT)}),
	}, nil).Complete()
	types.NewNamed(appendByteOrderObj, appendByteOrderIface, nil)

	// endianMethods adds the 8 numeric methods + String + GoString to an
	// endian named type (methods on the value type, per the source).
	endianMethods := func(named *types.Named) {
		v := h.param("v", uint16T)
		h.method(named, named, "Uint16",
			[]*types.Var{b}, []*types.Var{h.param("", uint16T)})
		h.method(named, named, "PutUint16",
			[]*types.Var{b, v}, nil)
		h.method(named, named, "AppendUint16",
			[]*types.Var{b, v}, []*types.Var{h.param("", st.byteSliceT)})
		v32 := h.param("v", uint32T)
		h.method(named, named, "Uint32",
			[]*types.Var{b}, []*types.Var{h.param("", uint32T)})
		h.method(named, named, "PutUint32",
			[]*types.Var{b, v32}, nil)
		h.method(named, named, "AppendUint32",
			[]*types.Var{b, v32}, []*types.Var{h.param("", st.byteSliceT)})
		v64 := h.param("v", uint64T)
		h.method(named, named, "Uint64",
			[]*types.Var{b}, []*types.Var{h.param("", uint64T)})
		h.method(named, named, "PutUint64",
			[]*types.Var{b, v64}, nil)
		h.method(named, named, "AppendUint64",
			[]*types.Var{b, v64}, []*types.Var{h.param("", st.byteSliceT)})
	}

	// littleEndian struct{}.
	leName := types.NewTypeName(token.NoPos, pkg, "littleEndian", nil)
	pkg.Scope().Insert(leName)
	leNamed := types.NewNamed(leName, types.NewStruct(nil, nil), nil)
	endianMethods(leNamed)
	h.method(leNamed, leNamed, "String", nil,
		[]*types.Var{h.param("", st.stringT)})
	h.method(leNamed, leNamed, "GoString", nil,
		[]*types.Var{h.param("", st.stringT)})
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "LittleEndian", leNamed))

	// bigEndian struct{}.
	beName := types.NewTypeName(token.NoPos, pkg, "bigEndian", nil)
	pkg.Scope().Insert(beName)
	beNamed := types.NewNamed(beName, types.NewStruct(nil, nil), nil)
	endianMethods(beNamed)
	h.method(beNamed, beNamed, "String", nil,
		[]*types.Var{h.param("", st.stringT)})
	h.method(beNamed, beNamed, "GoString", nil,
		[]*types.Var{h.param("", st.stringT)})
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "BigEndian", beNamed))

	// nativeEndian struct{ littleEndian } (embedded).
	neName := types.NewTypeName(token.NoPos, pkg, "nativeEndian", nil)
	pkg.Scope().Insert(neName)
	neNamed := types.NewNamed(neName, types.NewStruct([]*types.Var{
		h.field("littleEndian", leNamed, true),
	}, nil), nil)
	h.method(neNamed, neNamed, "String", nil,
		[]*types.Var{h.param("", st.stringT)})
	h.method(neNamed, neNamed, "GoString", nil,
		[]*types.Var{h.param("", st.stringT)})
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "NativeEndian", neNamed))

	// Consts: MaxVarintLen16/32/64 (untyped int).
	untypedInt := types.Typ[types.UntypedInt]
	pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, "MaxVarintLen16",
		untypedInt, constantValue(3)))
	pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, "MaxVarintLen32",
		untypedInt, constantValue(5)))
	pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, "MaxVarintLen64",
		untypedInt, constantValue(10)))

	// Package-level funcs. Uvarint/Varint are multi-result (mrM7 desugar);
	// Append/Encode/Decode/Size need reflect (fail-fast bodies).
	x64 := h.param("x", uint64T)
	h.insertFunc("AppendUvarint",
		[]*types.Var{buf, x64},
		[]*types.Var{h.param("", st.byteSliceT)}, false)
	h.insertFunc("PutUvarint",
		[]*types.Var{buf, x64},
		[]*types.Var{h.param("", st.intT)}, false)
	h.insertFunc("Uvarint",
		[]*types.Var{buf},
		[]*types.Var{h.param("", uint64T), h.param("", st.intT)}, false)
	x := h.param("x", int64T)
	h.insertFunc("AppendVarint",
		[]*types.Var{buf, x},
		[]*types.Var{h.param("", st.byteSliceT)}, false)
	h.insertFunc("PutVarint",
		[]*types.Var{buf, x},
		[]*types.Var{h.param("", st.intT)}, false)
	h.insertFunc("Varint",
		[]*types.Var{buf},
		[]*types.Var{h.param("", int64T), h.param("", st.intT)}, false)
	order := h.param("order", byteOrderNamed)
	data := h.param("data", anyT)
	h.insertFunc("Append",
		[]*types.Var{buf, order, data},
		[]*types.Var{h.param("", st.byteSliceT), h.param("", st.errorT)}, false)
	h.insertFunc("Encode",
		[]*types.Var{buf, order, data},
		[]*types.Var{h.param("", st.intT), h.param("", st.errorT)}, false)
	h.insertFunc("Decode",
		[]*types.Var{buf, order, data},
		[]*types.Var{h.param("", st.intT), h.param("", st.errorT)}, false)
	h.insertFunc("Size",
		[]*types.Var{h.param("v", anyT)},
		[]*types.Var{h.param("", st.intT)}, false)

	// Appendix M pair structs (unexported).
	h.insertPair("binaryUvarintPair", []*types.Var{
		h.field("x", uint64T, false), h.field("n", st.intT, false),
	})
	h.insertPair("binaryVarintPair", []*types.Var{
		h.field("x", int64T, false), h.field("n", st.intT, false),
	})

	pkg.MarkComplete()
	return pkg
}

// syntheticAscii85Package mirrors the P3-M7 synthetic "encoding/ascii85".
func syntheticAscii85Package() *types.Package {
	pkg := types.NewPackage("encoding/ascii85", "ascii85")
	h := newEncCoreHelpers(pkg)
	st := encShared()

	dst := h.param("dst", st.byteSliceT)
	src := h.param("src", st.byteSliceT)

	h.insertFunc("Encode",
		[]*types.Var{dst, src},
		[]*types.Var{h.param("", st.intT)}, false)
	// Decode is 3-result: the Appendix M triple desugar.
	h.insertFunc("Decode",
		[]*types.Var{dst, src, h.param("flush", st.boolT)},
		[]*types.Var{h.param("ndst", st.intT), h.param("nsrc", st.intT), h.param("err", st.errorT)}, false)
	h.insertFunc("MaxEncodedLen",
		[]*types.Var{h.param("n", st.intT)},
		[]*types.Var{h.param("", st.intT)}, false)

	// CorruptInputError = int64, with Error() string.
	cieName := types.NewTypeName(token.NoPos, pkg, "CorruptInputError", nil)
	pkg.Scope().Insert(cieName)
	cieNamed := types.NewNamed(cieName, types.Typ[types.Int64], nil)
	h.method(cieNamed, cieNamed, "Error", nil,
		[]*types.Var{h.param("", st.stringT)})

	// Appendix M triple struct (unexported).
	h.insertPair("ascii85DecodeTriple", []*types.Var{
		h.field("ndst", st.intT, false),
		h.field("nsrc", st.intT, false),
		h.field("err", st.errorT, false),
	})

	pkg.MarkComplete()
	return pkg
}

// syntheticPemPackage mirrors the P3-M7 synthetic "encoding/pem".
func syntheticPemPackage() *types.Package {
	pkg := types.NewPackage("encoding/pem", "pem")
	h := newEncCoreHelpers(pkg)
	st := encShared()

	// Block = struct{ Type string; Headers map[string]string; Bytes []byte }.
	blockName := types.NewTypeName(token.NoPos, pkg, "Block", nil)
	pkg.Scope().Insert(blockName)
	blockNamed := types.NewNamed(blockName, types.NewStruct([]*types.Var{
		h.field("Type", st.stringT, false),
		h.field("Headers", types.NewMap(st.stringT, st.stringT), false),
		h.field("Bytes", st.byteSliceT, false),
	}, nil), nil)
	blockPtr := types.NewPointer(blockNamed)

	// Decode(data []byte) (p *Block, rest []byte): multi-result, mrM7.
	h.insertFunc("Decode",
		[]*types.Var{h.param("data", st.byteSliceT)},
		[]*types.Var{h.param("p", blockPtr), h.param("rest", st.byteSliceT)}, false)
	h.insertFunc("EncodeToMemory",
		[]*types.Var{h.param("b", blockPtr)},
		[]*types.Var{h.param("", st.byteSliceT)}, false)

	// Appendix M pair struct (unexported).
	h.insertPair("pemDecodePair", []*types.Var{
		h.field("p", blockPtr, false), h.field("rest", st.byteSliceT, false),
	})

	pkg.MarkComplete()
	return pkg
}

// syntheticAsn1Package mirrors the P3-M7 synthetic "encoding/asn1".
func syntheticAsn1Package() *types.Package {
	pkg := types.NewPackage("encoding/asn1", "asn1")
	h := newEncCoreHelpers(pkg)
	st := encShared()
	intT := st.intT
	anyT := types.Universe.Lookup("any").Type()

	// Class consts (untyped int).
	untypedInt := types.Typ[types.UntypedInt]
	for _, c := range []struct {
		name string
		val  int64
	}{
		{"ClassUniversal", 0}, {"ClassApplication", 1},
		{"ClassContextSpecific", 2}, {"ClassPrivate", 3},
	} {
		pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, c.name,
			untypedInt, constantValue(c.val)))
	}
	// Tag consts (untyped int).
	for _, c := range []struct {
		name string
		val  int64
	}{
		{"TagBoolean", 1}, {"TagInteger", 2}, {"TagBitString", 3},
		{"TagOctetString", 4}, {"TagNull", 5}, {"TagOID", 6},
		{"TagEnum", 10}, {"TagUTF8String", 12}, {"TagSequence", 16},
		{"TagSet", 17}, {"TagNumericString", 18},
		{"TagPrintableString", 19}, {"TagT61String", 20},
		{"TagIA5String", 22}, {"TagUTCTime", 23},
		{"TagGeneralizedTime", 24}, {"TagGeneralString", 27},
		{"TagBMPString", 30},
	} {
		pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, c.name,
			untypedInt, constantValue(c.val)))
	}

	// StructuralError = struct{ Msg string }, Error() string.
	seName := types.NewTypeName(token.NoPos, pkg, "StructuralError", nil)
	pkg.Scope().Insert(seName)
	seNamed := types.NewNamed(seName, types.NewStruct([]*types.Var{
		h.field("Msg", st.stringT, false),
	}, nil), nil)
	h.method(seNamed, seNamed, "Error", nil,
		[]*types.Var{h.param("", st.stringT)})

	// SyntaxError = struct{ Msg string }, Error() string.
	syeName := types.NewTypeName(token.NoPos, pkg, "SyntaxError", nil)
	pkg.Scope().Insert(syeName)
	syeNamed := types.NewNamed(syeName, types.NewStruct([]*types.Var{
		h.field("Msg", st.stringT, false),
	}, nil), nil)
	h.method(syeNamed, syeNamed, "Error", nil,
		[]*types.Var{h.param("", st.stringT)})

	// BitString = struct{ Bytes []byte; BitLength int }.
	bsName := types.NewTypeName(token.NoPos, pkg, "BitString", nil)
	pkg.Scope().Insert(bsName)
	bsNamed := types.NewNamed(bsName, types.NewStruct([]*types.Var{
		h.field("Bytes", st.byteSliceT, false),
		h.field("BitLength", intT, false),
	}, nil), nil)
	h.method(bsNamed, bsNamed, "At",
		[]*types.Var{h.param("i", intT)},
		[]*types.Var{h.param("", intT)})
	h.method(bsNamed, bsNamed, "RightAlign", nil,
		[]*types.Var{h.param("", st.byteSliceT)})

	// ObjectIdentifier = []int, with Equal and String.
	oiName := types.NewTypeName(token.NoPos, pkg, "ObjectIdentifier", nil)
	pkg.Scope().Insert(oiName)
	oiNamed := types.NewNamed(oiName, types.NewSlice(intT), nil)
	h.method(oiNamed, oiNamed, "Equal",
		[]*types.Var{h.param("other", oiNamed)},
		[]*types.Var{h.param("", st.boolT)})
	h.method(oiNamed, oiNamed, "String", nil,
		[]*types.Var{h.param("", st.stringT)})

	// Enumerated = int; Flag = bool; RawContent = []byte.
	for _, td := range []struct {
		name string
		typ  types.Type
	}{
		{"Enumerated", intT},
		{"Flag", st.boolT},
		{"RawContent", st.byteSliceT},
	} {
		tn := types.NewTypeName(token.NoPos, pkg, td.name, nil)
		pkg.Scope().Insert(tn)
		types.NewNamed(tn, td.typ, nil)
	}

	// RawValue = struct{ Class, Tag int; IsCompound bool; Bytes []byte;
	// FullBytes []byte }.
	rvName := types.NewTypeName(token.NoPos, pkg, "RawValue", nil)
	pkg.Scope().Insert(rvName)
	rvNamed := types.NewNamed(rvName, types.NewStruct([]*types.Var{
		h.field("Class", intT, false),
		h.field("Tag", intT, false),
		h.field("IsCompound", st.boolT, false),
		h.field("Bytes", st.byteSliceT, false),
		h.field("FullBytes", st.byteSliceT, false),
	}, nil), nil)

	// Vars: NullRawValue RawValue; NullBytes []byte.
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "NullRawValue", rvNamed))
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "NullBytes", st.byteSliceT))

	// Multi-result funcs (exact gc signatures; Marshal desugars via mrM7,
	// the rest fail fast).
	val := h.param("val", anyT)
	h.insertFunc("Marshal",
		[]*types.Var{val},
		[]*types.Var{h.param("", st.byteSliceT), h.param("", st.errorT)}, false)
	h.insertFunc("MarshalWithParams",
		[]*types.Var{val, h.param("params", st.stringT)},
		[]*types.Var{h.param("", st.byteSliceT), h.param("", st.errorT)}, false)
	b := h.param("b", st.byteSliceT)
	h.insertFunc("Unmarshal",
		[]*types.Var{b, val},
		[]*types.Var{h.param("rest", st.byteSliceT), h.param("err", st.errorT)}, false)
	h.insertFunc("UnmarshalWithParams",
		[]*types.Var{b, val, h.param("params", st.stringT)},
		[]*types.Var{h.param("rest", st.byteSliceT), h.param("err", st.errorT)}, false)

	// Appendix M pair struct for Marshal (unexported). (Unmarshal has no
	// desugar — WS D pointer store — so no pair struct.)
	h.insertPair("asn1MarshalPair", []*types.Var{
		h.field("b", st.byteSliceT, false), h.field("err", st.errorT, false),
	})

	pkg.MarkComplete()
	return pkg
}

// syntheticEncodingPackage mirrors the P3-M7 synthetic top-level
// "encoding" package: exactly the six gc 1.27.1 marshaling interfaces,
// no code, no vars, no funcs — nothing to desugar, nothing to fail fast.
func syntheticEncodingPackage() *types.Package {
	pkg := types.NewPackage("encoding", "encoding")
	h := newEncCoreHelpers(pkg)
	st := encShared()

	mkIfaceMethod := func(name string, params, results []*types.Var) *types.Func {
		return types.NewFunc(token.NoPos, pkg, name,
			types.NewSignatureType(nil, nil, nil,
				types.NewTuple(params...), types.NewTuple(results...), false))
	}
	data := h.param("data", st.byteSliceT)
	text := h.param("text", st.byteSliceT)
	mkIface := func(name string, methods ...*types.Func) {
		tn := types.NewTypeName(token.NoPos, pkg, name, nil)
		pkg.Scope().Insert(tn)
		types.NewNamed(tn,
			types.NewInterfaceType(methods, nil).Complete(), nil)
	}

	mkIface("BinaryMarshaler",
		mkIfaceMethod("MarshalBinary", nil,
			[]*types.Var{h.param("data", st.byteSliceT), h.param("err", st.errorT)}))
	mkIface("BinaryUnmarshaler",
		mkIfaceMethod("UnmarshalBinary",
			[]*types.Var{data},
			[]*types.Var{h.param("", st.errorT)}))
	mkIface("BinaryAppender",
		mkIfaceMethod("AppendBinary",
			[]*types.Var{h.param("b", st.byteSliceT)},
			[]*types.Var{h.param("", st.byteSliceT), h.param("", st.errorT)}))
	mkIface("TextMarshaler",
		mkIfaceMethod("MarshalText", nil,
			[]*types.Var{text, h.param("err", st.errorT)}))
	mkIface("TextUnmarshaler",
		mkIfaceMethod("UnmarshalText",
			[]*types.Var{text},
			[]*types.Var{h.param("", st.errorT)}))
	mkIface("TextAppender",
		mkIfaceMethod("AppendText",
			[]*types.Var{h.param("b", st.byteSliceT)},
			[]*types.Var{h.param("", st.byteSliceT), h.param("", st.errorT)}))

	pkg.MarkComplete()
	return pkg
}
