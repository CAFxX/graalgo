package p3frontend

// P3-M10 (self-attack FINDING-5 follow-up; extended 2026-11-05): p3frontend
// mirror of the phase-1 synthetic "io" package (internal/frontend/
// synth_io.go). The mirror covers the FULL gc 1.27.1 public API of the
// synthetic source — all 22 interfaces, the Seek consts, the error vars,
// Discard, and every package-level function — so real stdlib packages
// that import io (io/fs, fmt, ...) type-check against it. Unexported
// structure (teeReader, discard, multiReader, multiWriter, eofReader,
// onceError, the pair carriers, ...) is not mirrored, except where the
// public API needs it: PipeReader embeds *pipe, so the mirror declares
// an (unexported, empty-struct) pipe type to carry the embedding;
// SectionReader/OffsetWriter have no exported fields and are mirrored
// as empty structs with their full method sets.
//
// Drift is pinned by TestIoMirrorMatchesPhase1 in io_test.go, which
// compares this mirror against the phase-1 syntheticIoSource scope and
// the method sets of every named type.
//
// The signatures match internal/frontend/synth_io.go exactly.

import (
	"go/constant"
	"go/token"
	"go/types"
)

// syntheticIoPackage mirrors the P3-M8 synthetic "io" (full public API).
func syntheticIoPackage() *types.Package {
	pkg := types.NewPackage("io", "io")
	byteT := types.Universe.Lookup("byte").Type()
	runeT := types.Universe.Lookup("rune").Type()
	intT := types.Typ[types.Int]
	int64T := types.Typ[types.Int64]
	stringT := types.Typ[types.String]
	errorT := types.Universe.Lookup("error").Type()
	bytesSlice := types.NewSlice(byteT)

	newParam := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	newField := func(name string, t types.Type, embedded bool) *types.Var {
		return types.NewField(token.NoPos, pkg, name, t, embedded)
	}
	ifaceMethod := func(name string, params, results []*types.Var) *types.Func {
		return types.NewFunc(token.NoPos, pkg, name,
			types.NewSignatureType(nil, nil, nil,
				types.NewTuple(params...), types.NewTuple(results...), false))
	}
	// mkIface declares an interface type with the given explicit methods
	// and embedded interfaces, and returns its named type.
	mkIface := func(name string, methods []*types.Func, embedded []types.Type) *types.Named {
		tname := types.NewTypeName(token.NoPos, pkg, name, nil)
		pkg.Scope().Insert(tname)
		iface := types.NewInterfaceType(methods, embedded).Complete()
		return types.NewNamed(tname, iface, nil)
	}
	// addMethod adds a pointer-receiver method to a named struct type.
	addMethod := func(named *types.Named, name string, params, results []*types.Var) {
		recv := types.NewParam(token.NoPos, pkg, "", types.NewPointer(named))
		sig := types.NewSignatureType(recv, nil, nil,
			types.NewTuple(params...), types.NewTuple(results...), false)
		named.AddMethod(types.NewFunc(token.NoPos, pkg, name, sig))
	}
	insertFunc := func(name string, params, results []*types.Var, variadic bool) {
		sig := types.NewSignatureType(nil, nil, nil,
			types.NewTuple(params...), types.NewTuple(results...), variadic)
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, name, sig))
	}

	// -- Interfaces (22) -------------------------------------------------
	readRes := []*types.Var{newParam("n", intT), newParam("err", errorT)}
	writeRes := []*types.Var{newParam("n", intT), newParam("err", errorT)}
	reader := mkIface("Reader",
		[]*types.Func{ifaceMethod("Read", []*types.Var{newParam("p", bytesSlice)}, readRes)}, nil)
	writer := mkIface("Writer",
		[]*types.Func{ifaceMethod("Write", []*types.Var{newParam("p", bytesSlice)}, writeRes)}, nil)
	closer := mkIface("Closer",
		[]*types.Func{ifaceMethod("Close", nil, []*types.Var{newParam("", errorT)})}, nil)
	seeker := mkIface("Seeker",
		[]*types.Func{ifaceMethod("Seek",
			[]*types.Var{newParam("offset", int64T), newParam("whence", intT)},
			[]*types.Var{newParam("", int64T), newParam("", errorT)})}, nil)
	readWriter := mkIface("ReadWriter", nil, []types.Type{reader, writer})
	readCloser := mkIface("ReadCloser", nil, []types.Type{reader, closer})
	writeCloser := mkIface("WriteCloser", nil, []types.Type{writer, closer})
	readWriteCloser := mkIface("ReadWriteCloser", nil, []types.Type{reader, writer, closer})
	readSeeker := mkIface("ReadSeeker", nil, []types.Type{reader, seeker})
	readSeekCloser := mkIface("ReadSeekCloser", nil, []types.Type{reader, seeker, closer})
	writeSeeker := mkIface("WriteSeeker", nil, []types.Type{writer, seeker})
	readWriteSeeker := mkIface("ReadWriteSeeker", nil, []types.Type{reader, writer, seeker})
	readerFrom := mkIface("ReaderFrom",
		[]*types.Func{ifaceMethod("ReadFrom", []*types.Var{newParam("r", reader)},
			[]*types.Var{newParam("n", int64T), newParam("err", errorT)})}, nil)
	writerTo := mkIface("WriterTo",
		[]*types.Func{ifaceMethod("WriteTo", []*types.Var{newParam("w", writer)},
			[]*types.Var{newParam("n", int64T), newParam("err", errorT)})}, nil)
	readerAt := mkIface("ReaderAt",
		[]*types.Func{ifaceMethod("ReadAt",
			[]*types.Var{newParam("p", bytesSlice), newParam("off", int64T)}, readRes)}, nil)
	writerAt := mkIface("WriterAt",
		[]*types.Func{ifaceMethod("WriteAt",
			[]*types.Var{newParam("p", bytesSlice), newParam("off", int64T)}, writeRes)}, nil)
	byteReader := mkIface("ByteReader",
		[]*types.Func{ifaceMethod("ReadByte", nil,
			[]*types.Var{newParam("", byteT), newParam("", errorT)})}, nil)
	byteScanner := mkIface("ByteScanner",
		[]*types.Func{ifaceMethod("UnreadByte", nil, []*types.Var{newParam("", errorT)})},
		[]types.Type{byteReader})
	byteWriter := mkIface("ByteWriter",
		[]*types.Func{ifaceMethod("WriteByte",
			[]*types.Var{newParam("c", byteT)},
			[]*types.Var{newParam("", errorT)})}, nil)
	runeReader := mkIface("RuneReader",
		[]*types.Func{ifaceMethod("ReadRune", nil,
			[]*types.Var{newParam("r", runeT), newParam("size", intT), newParam("err", errorT)})}, nil)
	runeScanner := mkIface("RuneScanner",
		[]*types.Func{ifaceMethod("UnreadRune", nil, []*types.Var{newParam("", errorT)})},
		[]types.Type{runeReader})
	stringWriter := mkIface("StringWriter",
		[]*types.Func{ifaceMethod("WriteString",
			[]*types.Var{newParam("s", stringT)}, writeRes)}, nil)
	_ = readWriter
	_ = readCloser
	_ = writeCloser
	_ = readWriteCloser
	_ = readSeeker
	_ = readSeekCloser
	_ = writeSeeker
	_ = readWriteSeeker
	_ = readerFrom
	_ = writerTo
	_ = byteScanner
	_ = byteWriter
	_ = runeScanner
	_ = stringWriter

	// -- Named struct types + methods ------------------------------------
	// LimitedReader = struct{ R Reader; N int64 }.
	limitedName := types.NewTypeName(token.NoPos, pkg, "LimitedReader", nil)
	pkg.Scope().Insert(limitedName)
	limitedStruct := types.NewStruct(
		[]*types.Var{newField("R", reader, false), newField("N", int64T, false)},
		[]string{"R", "N"})
	limited := types.NewNamed(limitedName, limitedStruct, nil)
	addMethod(limited, "Read", []*types.Var{newParam("p", bytesSlice)}, readRes)

	// SectionReader (no exported fields; methods only).
	sectionName := types.NewTypeName(token.NoPos, pkg, "SectionReader", nil)
	pkg.Scope().Insert(sectionName)
	section := types.NewNamed(sectionName, types.NewStruct(nil, nil), nil)
	addMethod(section, "Read", []*types.Var{newParam("p", bytesSlice)}, readRes)
	addMethod(section, "Seek",
		[]*types.Var{newParam("offset", int64T), newParam("whence", intT)},
		[]*types.Var{newParam("", int64T), newParam("", errorT)})
	addMethod(section, "ReadAt",
		[]*types.Var{newParam("p", bytesSlice), newParam("off", int64T)}, readRes)
	addMethod(section, "Size", nil, []*types.Var{newParam("", int64T)})
	addMethod(section, "Outer", nil,
		[]*types.Var{newParam("r", readerAt), newParam("off", int64T), newParam("n", int64T)})

	// OffsetWriter (no exported fields; methods only).
	offsetName := types.NewTypeName(token.NoPos, pkg, "OffsetWriter", nil)
	pkg.Scope().Insert(offsetName)
	offset := types.NewNamed(offsetName, types.NewStruct(nil, nil), nil)
	addMethod(offset, "Write", []*types.Var{newParam("p", bytesSlice)}, writeRes)
	addMethod(offset, "WriteAt",
		[]*types.Var{newParam("p", bytesSlice), newParam("off", int64T)}, writeRes)
	addMethod(offset, "Seek",
		[]*types.Var{newParam("offset", int64T), newParam("whence", intT)},
		[]*types.Var{newParam("", int64T), newParam("", errorT)})

	// pipe (unexported): the embedding target of PipeReader.
	pipeName := types.NewTypeName(token.NoPos, pkg, "pipe", nil)
	pkg.Scope().Insert(pipeName)
	pipe := types.NewNamed(pipeName, types.NewStruct(nil, nil), nil)

	// PipeReader = struct{ *pipe }.
	pipeReaderName := types.NewTypeName(token.NoPos, pkg, "PipeReader", nil)
	pkg.Scope().Insert(pipeReaderName)
	pipeReaderStruct := types.NewStruct(
		[]*types.Var{newField("pipe", types.NewPointer(pipe), true)},
		[]string{"pipe"})
	pipeReader := types.NewNamed(pipeReaderName, pipeReaderStruct, nil)
	addMethod(pipeReader, "Read", []*types.Var{newParam("data", bytesSlice)}, readRes)
	addMethod(pipeReader, "Close", nil, []*types.Var{newParam("", errorT)})
	addMethod(pipeReader, "CloseWithError",
		[]*types.Var{newParam("err", errorT)}, []*types.Var{newParam("", errorT)})

	// PipeWriter = struct{ r PipeReader }.
	pipeWriterName := types.NewTypeName(token.NoPos, pkg, "PipeWriter", nil)
	pkg.Scope().Insert(pipeWriterName)
	pipeWriterStruct := types.NewStruct(
		[]*types.Var{newField("r", pipeReader, false)},
		[]string{"r"})
	pipeWriter := types.NewNamed(pipeWriterName, pipeWriterStruct, nil)
	addMethod(pipeWriter, "Write", []*types.Var{newParam("data", bytesSlice)}, writeRes)
	addMethod(pipeWriter, "Close", nil, []*types.Var{newParam("", errorT)})
	addMethod(pipeWriter, "CloseWithError",
		[]*types.Var{newParam("err", errorT)}, []*types.Var{newParam("", errorT)})

	// -- Seek consts (untyped, as in the source) --------------------------
	pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, "SeekStart",
		types.Typ[types.UntypedInt], constant.MakeInt64(0)))
	pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, "SeekCurrent",
		types.Typ[types.UntypedInt], constant.MakeInt64(1)))
	pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, "SeekEnd",
		types.Typ[types.UntypedInt], constant.MakeInt64(2)))

	// -- Error vars + Discard ---------------------------------------------
	for _, name := range []string{"EOF", "ErrClosedPipe", "ErrNoProgress",
		"ErrShortBuffer", "ErrShortWrite", "ErrUnexpectedEOF"} {
		pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, name, errorT))
	}
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "Discard", writer))

	// -- Package-level functions ------------------------------------------
	insertFunc("WriteString",
		[]*types.Var{newParam("w", writer), newParam("s", stringT)}, writeRes, false)
	insertFunc("ReadAtLeast",
		[]*types.Var{newParam("r", reader), newParam("buf", bytesSlice), newParam("min", intT)},
		readRes, false)
	insertFunc("ReadFull",
		[]*types.Var{newParam("r", reader), newParam("buf", bytesSlice)}, readRes, false)
	insertFunc("CopyN",
		[]*types.Var{newParam("dst", writer), newParam("src", reader), newParam("n", int64T)},
		[]*types.Var{newParam("written", int64T), newParam("err", errorT)}, false)
	insertFunc("Copy",
		[]*types.Var{newParam("dst", writer), newParam("src", reader)},
		[]*types.Var{newParam("written", int64T), newParam("err", errorT)}, false)
	insertFunc("CopyBuffer",
		[]*types.Var{newParam("dst", writer), newParam("src", reader), newParam("buf", bytesSlice)},
		[]*types.Var{newParam("written", int64T), newParam("err", errorT)}, false)
	insertFunc("LimitReader",
		[]*types.Var{newParam("r", reader), newParam("n", int64T)},
		[]*types.Var{newParam("", reader)}, false)
	insertFunc("NewSectionReader",
		[]*types.Var{newParam("r", readerAt), newParam("off", int64T), newParam("n", int64T)},
		[]*types.Var{newParam("", types.NewPointer(section))}, false)
	insertFunc("NewOffsetWriter",
		[]*types.Var{newParam("w", writerAt), newParam("off", int64T)},
		[]*types.Var{newParam("", types.NewPointer(offset))}, false)
	insertFunc("TeeReader",
		[]*types.Var{newParam("r", reader), newParam("w", writer)},
		[]*types.Var{newParam("", reader)}, false)
	insertFunc("NopCloser",
		[]*types.Var{newParam("r", reader)},
		[]*types.Var{newParam("", readCloser)}, false)
	insertFunc("ReadAll",
		[]*types.Var{newParam("r", reader)},
		[]*types.Var{newParam("", bytesSlice), newParam("", errorT)}, false)
	insertFunc("MultiReader",
		[]*types.Var{newParam("readers", types.NewSlice(reader))},
		[]*types.Var{newParam("", reader)}, true)
	insertFunc("MultiWriter",
		[]*types.Var{newParam("writers", types.NewSlice(writer))},
		[]*types.Var{newParam("", writer)}, true)
	insertFunc("Pipe", nil,
		[]*types.Var{newParam("", types.NewPointer(pipeReader)), newParam("", types.NewPointer(pipeWriter))}, false)

	pkg.MarkComplete()
	return pkg
}

// ioReader returns the io.Reader type from a synthetic io package.
func ioReader(ioPkg *types.Package) types.Type {
	return ioPkg.Scope().Lookup("Reader").Type()
}

// ioWriter returns the io.Writer type from a synthetic io package.
func ioWriter(ioPkg *types.Package) types.Type {
	return ioPkg.Scope().Lookup("Writer").Type()
}
