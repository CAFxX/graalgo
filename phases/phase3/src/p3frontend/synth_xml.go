package p3frontend

// P3-M7 (wave 1): p3frontend mirror of the phase-1 synthetic
// "encoding/xml" package. The importer reimplements the frozen exporter's
// synthetic signatures (it cannot reuse them — unexported and frozen);
// this mirror pins the signatures so drift fails fast.
//
// The signatures match internal/frontend/synth_xml.go exactly (the
// M7 honest subset: token stream over the structural Reader/Writer,
// fail-fast reflect-driven members).

import (
	"go/token"
	"go/types"
)

// syntheticXmlPackage mirrors the frozen P3-M7 synthetic "encoding/xml".
func syntheticXmlPackage() *types.Package {
	pkg := types.NewPackage("encoding/xml", "xml")
	// NB: types.Typ[types.Byte] renders as "uint8"; the universe "byte"
	// object renders as the source spells it ("[]byte").
	byteT := types.Universe.Lookup("byte").Type()
	intT := types.Typ[types.Int]
	int64T := types.Typ[types.Int64]
	stringT := types.Typ[types.String]
	boolT := types.Typ[types.Bool]
	errorT := types.Universe.Lookup("error").Type()
	anyT := types.Universe.Lookup("any").Type()
	byteSliceT := types.NewSlice(byteT)

	newParam := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	newField := func(name string, t types.Type) *types.Var {
		return types.NewField(token.NoPos, pkg, name, t, false)
	}

	// Token = any.
	tokenObj := types.NewTypeName(token.NoPos, pkg, "Token", nil)
	_ = types.NewNamed(tokenObj, anyT, nil)
	pkg.Scope().Insert(tokenObj)
	tokenT := tokenObj.Type()

	// ReadResult = struct{ Byte byte; OK bool }.
	readResultObj := types.NewTypeName(token.NoPos, pkg, "ReadResult", nil)
	readResultNamed := types.NewNamed(readResultObj,
		types.NewStruct([]*types.Var{
			newField("Byte", byteT),
			newField("OK", boolT),
		}, nil), nil)
	pkg.Scope().Insert(readResultObj)

	// Reader = interface{ Next() ReadResult }.
	readerObj := types.NewTypeName(token.NoPos, pkg, "Reader", nil)
	readerSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(), types.NewTuple(newParam("", readResultNamed)), false)
	readerNamed := types.NewNamed(readerObj,
		types.NewInterfaceType([]*types.Func{
			types.NewFunc(token.NoPos, pkg, "Next", readerSig),
		}, nil).Complete(), nil)
	pkg.Scope().Insert(readerObj)

	// Writer = interface{ WriteBytes(p []byte) }.
	writerObj := types.NewTypeName(token.NoPos, pkg, "Writer", nil)
	writerSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(newParam("p", byteSliceT)), types.NewTuple(), false)
	writerNamed := types.NewNamed(writerObj,
		types.NewInterfaceType([]*types.Func{
			types.NewFunc(token.NoPos, pkg, "WriteBytes", writerSig),
		}, nil).Complete(), nil)
	pkg.Scope().Insert(writerObj)

	// SyntaxError = struct{ Msg string; Line int }, with Error() string.
	syntaxErrorObj := types.NewTypeName(token.NoPos, pkg, "SyntaxError", nil)
	syntaxErrorNamed := types.NewNamed(syntaxErrorObj,
		types.NewStruct([]*types.Var{newField("Msg", stringT), newField("Line", intT)}, nil), nil)
	syntaxErrorNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Error",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "e", syntaxErrorNamed),
			nil, nil, types.NewTuple(), types.NewTuple(newParam("", stringT)), false)))
	pkg.Scope().Insert(syntaxErrorObj)

	// Name = struct{ Space, Local string }.
	nameObj := types.NewTypeName(token.NoPos, pkg, "Name", nil)
	nameNamed := types.NewNamed(nameObj,
		types.NewStruct([]*types.Var{
			newField("Space", stringT),
			newField("Local", stringT),
		}, nil), nil)
	pkg.Scope().Insert(nameObj)

	// Attr = struct{ Name Name; Value string }.
	attrObj := types.NewTypeName(token.NoPos, pkg, "Attr", nil)
	attrNamed := types.NewNamed(attrObj,
		types.NewStruct([]*types.Var{
			newField("Name", nameNamed),
			newField("Value", stringT),
		}, nil), nil)
	pkg.Scope().Insert(attrObj)

	// StartElement = struct{ Name Name; Attr []Attr }, with Copy() and End().
	startElementObj := types.NewTypeName(token.NoPos, pkg, "StartElement", nil)
	startElementNamed := types.NewNamed(startElementObj,
		types.NewStruct([]*types.Var{
			newField("Name", nameNamed),
			newField("Attr", types.NewSlice(attrNamed)),
		}, nil), nil)
	startElementNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Copy",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "e", startElementNamed),
			nil, nil, types.NewTuple(), types.NewTuple(newParam("", startElementNamed)), false)))
	pkg.Scope().Insert(startElementObj)

	// EndElement = struct{ Name Name }.
	endElementObj := types.NewTypeName(token.NoPos, pkg, "EndElement", nil)
	endElementNamed := types.NewNamed(endElementObj,
		types.NewStruct([]*types.Var{newField("Name", nameNamed)}, nil), nil)
	pkg.Scope().Insert(endElementObj)

	// StartElement.End() EndElement (needs endElementNamed).
	startElementNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "End",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "e", startElementNamed),
			nil, nil, types.NewTuple(), types.NewTuple(newParam("", endElementNamed)), false)))

	// CharData = []byte, with Copy().
	charDataObj := types.NewTypeName(token.NoPos, pkg, "CharData", nil)
	charDataNamed := types.NewNamed(charDataObj, byteSliceT, nil)
	charDataNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Copy",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "c", charDataNamed),
			nil, nil, types.NewTuple(), types.NewTuple(newParam("", charDataNamed)), false)))
	pkg.Scope().Insert(charDataObj)

	// Comment = []byte, with Copy().
	commentObj := types.NewTypeName(token.NoPos, pkg, "Comment", nil)
	commentNamed := types.NewNamed(commentObj, byteSliceT, nil)
	commentNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Copy",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "c", commentNamed),
			nil, nil, types.NewTuple(), types.NewTuple(newParam("", commentNamed)), false)))
	pkg.Scope().Insert(commentObj)

	// ProcInst = struct{ Target string; Inst []byte }, with Copy().
	procInstObj := types.NewTypeName(token.NoPos, pkg, "ProcInst", nil)
	procInstNamed := types.NewNamed(procInstObj,
		types.NewStruct([]*types.Var{
			newField("Target", stringT),
			newField("Inst", byteSliceT),
		}, nil), nil)
	procInstNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Copy",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "p", procInstNamed),
			nil, nil, types.NewTuple(), types.NewTuple(newParam("", procInstNamed)), false)))
	pkg.Scope().Insert(procInstObj)

	// Directive = []byte, with Copy().
	directiveObj := types.NewTypeName(token.NoPos, pkg, "Directive", nil)
	directiveNamed := types.NewNamed(directiveObj, byteSliceT, nil)
	directiveNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Copy",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "d", directiveNamed),
			nil, nil, types.NewTuple(), types.NewTuple(newParam("", directiveNamed)), false)))
	pkg.Scope().Insert(directiveObj)

	// TokenReader = interface{ Token() (Token, error) }.
	tokenReaderObj := types.NewTypeName(token.NoPos, pkg, "TokenReader", nil)
	tokenReaderSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(),
		types.NewTuple(newParam("", tokenT), newParam("", errorT)), false)
	tokenReaderNamed := types.NewNamed(tokenReaderObj,
		types.NewInterfaceType([]*types.Func{
			types.NewFunc(token.NoPos, pkg, "Token", tokenReaderSig),
		}, nil).Complete(), nil)
	pkg.Scope().Insert(tokenReaderObj)

	// Decoder struct with exported fields (Strict, AutoClose, Entity,
	// DefaultSpace); unexported fields omitted (the drift test checks
	// exported fields only).
	decoderObj := types.NewTypeName(token.NoPos, pkg, "Decoder", nil)
	decoderNamed := types.NewNamed(decoderObj, types.NewStruct([]*types.Var{
		newField("Strict", boolT),
		newField("AutoClose", types.NewSlice(stringT)),
		newField("Entity", types.NewMap(stringT, stringT)),
		newField("DefaultSpace", stringT),
	}, nil), nil)
	decoderPtr := types.NewPointer(decoderNamed)
	// Methods: Token, RawToken, InputOffset, InputPos, Skip, Decode, DecodeElement.
	decoderNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Token",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "d", decoderPtr),
			nil, nil, types.NewTuple(),
			types.NewTuple(newParam("", tokenT), newParam("", errorT)), false)))
	decoderNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "RawToken",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "d", decoderPtr),
			nil, nil, types.NewTuple(),
			types.NewTuple(newParam("", tokenT), newParam("", errorT)), false)))
	decoderNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "InputOffset",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "d", decoderPtr),
			nil, nil, types.NewTuple(), types.NewTuple(newParam("", int64T)), false)))
	decoderNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "InputPos",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "d", decoderPtr),
			nil, nil, types.NewTuple(),
			types.NewTuple(newParam("line", intT), newParam("column", intT)), false)))
	decoderNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Skip",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "d", decoderPtr),
			nil, nil, types.NewTuple(), types.NewTuple(newParam("", errorT)), false)))
	decoderNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Decode",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "d", decoderPtr),
			nil, nil, types.NewTuple(newParam("v", anyT)),
			types.NewTuple(newParam("", errorT)), false)))
	decoderNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "DecodeElement",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "d", decoderPtr),
			nil, nil, types.NewTuple(newParam("v", anyT), newParam("start", types.NewPointer(startElementNamed))),
			types.NewTuple(newParam("", errorT)), false)))
	pkg.Scope().Insert(decoderObj)

	// Encoder = struct{ ... } (opaque), with Indent, EncodeToken, Flush, Close, Encode, EncodeElement.
	encoderObj := types.NewTypeName(token.NoPos, pkg, "Encoder", nil)
	encoderNamed := types.NewNamed(encoderObj, types.NewStruct(nil, nil), nil)
	encoderPtr := types.NewPointer(encoderNamed)
	encoderNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Indent",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "enc", encoderPtr),
			nil, nil, types.NewTuple(newParam("prefix", stringT), newParam("indent", stringT)),
			types.NewTuple(), false)))
	encoderNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "EncodeToken",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "enc", encoderPtr),
			nil, nil, types.NewTuple(newParam("t", tokenT)),
			types.NewTuple(newParam("", errorT)), false)))
	encoderNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Flush",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "enc", encoderPtr),
			nil, nil, types.NewTuple(), types.NewTuple(newParam("", errorT)), false)))
	encoderNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Close",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "enc", encoderPtr),
			nil, nil, types.NewTuple(), types.NewTuple(newParam("", errorT)), false)))
	encoderNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Encode",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "enc", encoderPtr),
			nil, nil, types.NewTuple(newParam("v", anyT)),
			types.NewTuple(newParam("", errorT)), false)))
	encoderNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "EncodeElement",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "enc", encoderPtr),
			nil, nil, types.NewTuple(newParam("v", anyT), newParam("start", startElementNamed)),
			types.NewTuple(newParam("", errorT)), false)))
	pkg.Scope().Insert(encoderObj)

	// Marshaler = interface{ MarshalXML(e *Encoder, start StartElement) error }.
	marshalerObj := types.NewTypeName(token.NoPos, pkg, "Marshaler", nil)
	marshalerSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(newParam("e", encoderPtr), newParam("start", startElementNamed)),
		types.NewTuple(newParam("", errorT)), false)
	_ = types.NewNamed(marshalerObj,
		types.NewInterfaceType([]*types.Func{
			types.NewFunc(token.NoPos, pkg, "MarshalXML", marshalerSig),
		}, nil).Complete(), nil)
	pkg.Scope().Insert(marshalerObj)

	// MarshalerAttr = interface{ MarshalXMLAttr(name Name) (Attr, error) }.
	marshalerAttrObj := types.NewTypeName(token.NoPos, pkg, "MarshalerAttr", nil)
	marshalerAttrSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(newParam("name", nameNamed)),
		types.NewTuple(newParam("", attrNamed), newParam("", errorT)), false)
	_ = types.NewNamed(marshalerAttrObj,
		types.NewInterfaceType([]*types.Func{
			types.NewFunc(token.NoPos, pkg, "MarshalXMLAttr", marshalerAttrSig),
		}, nil).Complete(), nil)
	pkg.Scope().Insert(marshalerAttrObj)

	// UnmarshalError = string, with Error() string.
	unmarshalErrorObj := types.NewTypeName(token.NoPos, pkg, "UnmarshalError", nil)
	unmarshalErrorNamed := types.NewNamed(unmarshalErrorObj, stringT, nil)
	unmarshalErrorNamed.AddMethod(types.NewFunc(token.NoPos, pkg, "Error",
		types.NewSignatureType(types.NewParam(token.NoPos, pkg, "e", unmarshalErrorNamed),
			nil, nil, types.NewTuple(), types.NewTuple(newParam("", stringT)), false)))
	pkg.Scope().Insert(unmarshalErrorObj)

	// Unmarshaler = interface{ UnmarshalXML(d *Decoder, start StartElement) error }.
	unmarshalerObj := types.NewTypeName(token.NoPos, pkg, "Unmarshaler", nil)
	unmarshalerSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(newParam("d", decoderPtr), newParam("start", startElementNamed)),
		types.NewTuple(newParam("", errorT)), false)
	_ = types.NewNamed(unmarshalerObj,
		types.NewInterfaceType([]*types.Func{
			types.NewFunc(token.NoPos, pkg, "UnmarshalXML", unmarshalerSig),
		}, nil).Complete(), nil)
	pkg.Scope().Insert(unmarshalerObj)

	// UnmarshalerAttr = interface{ UnmarshalXMLAttr(attr Attr) error }.
	unmarshalerAttrObj := types.NewTypeName(token.NoPos, pkg, "UnmarshalerAttr", nil)
	unmarshalerAttrSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(newParam("attr", attrNamed)),
		types.NewTuple(newParam("", errorT)), false)
	_ = types.NewNamed(unmarshalerAttrObj,
		types.NewInterfaceType([]*types.Func{
			types.NewFunc(token.NoPos, pkg, "UnmarshalXMLAttr", unmarshalerAttrSig),
		}, nil).Complete(), nil)
	pkg.Scope().Insert(unmarshalerAttrObj)

	// Package-level functions.
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "NewDecoder",
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(newParam("r", readerNamed)),
			types.NewTuple(newParam("", decoderPtr)), false)))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "NewTokenDecoder",
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(newParam("t", tokenReaderNamed)),
			types.NewTuple(newParam("", decoderPtr)), false)))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "CopyToken",
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(newParam("t", tokenObj.Type())),
			types.NewTuple(newParam("", tokenObj.Type())), false)))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "NewEncoder",
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(newParam("w", writerNamed)),
			types.NewTuple(newParam("", encoderPtr)), false)))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "EscapeText",
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(newParam("w", writerNamed), newParam("s", byteSliceT)),
			types.NewTuple(newParam("", errorT)), false)))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "Escape",
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(newParam("w", writerNamed), newParam("s", byteSliceT)),
			types.NewTuple(), false)))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "Unmarshal",
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(newParam("data", byteSliceT), newParam("v", anyT)),
			types.NewTuple(newParam("", errorT)), false)))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "Marshal",
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(newParam("v", anyT)),
			types.NewTuple(newParam("", byteSliceT), newParam("", errorT)), false)))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "MarshalIndent",
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(newParam("v", anyT), newParam("prefix", stringT), newParam("indent", stringT)),
			types.NewTuple(newParam("", byteSliceT), newParam("", errorT)), false)))

	// Vars: HTMLEntity map[string]string, HTMLAutoClose []string.
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "HTMLEntity", types.NewMap(stringT, stringT)))
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "HTMLAutoClose", types.NewSlice(stringT)))

	return pkg
}
