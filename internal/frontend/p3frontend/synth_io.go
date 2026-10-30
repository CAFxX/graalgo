// Retirement mirror: see ../../../docs/arch/synth-retirement-tracker.md#io (row for io; this file mirrors its signatures).
package p3frontend

// P3-M10 (self-attack FINDING-5 follow-up): p3frontend mirror of the
// phase-1 synthetic "io" package. Minimal by design: it mirrors the
// Reader and Writer interfaces — the types the bytes/strings mirrors
// reference (P3-M8: their WriteTo/ReadFrom now spell io.Writer/io.Reader)
// — plus ReadAll, which the csv mirror's NewReader needs. The full
// synthetic io API (Copy, ReadFull, SectionReader, ...) is not mirrored
// yet; importing "io" directly yields Reader/Writer/ReadAll only. This
// is strictly better than the previous state (no "io" at all, and the
// bytes mirror used unexported byteReader/byteWriter stand-ins that
// drifted from the phase-1 source).
//
// The signatures match internal/frontend/synth_io.go exactly.

import (
	"go/token"
	"go/types"
)

// syntheticIoPackage mirrors the P3-M8 synthetic "io" (Reader/Writer).
func syntheticIoPackage() *types.Package {
	pkg := types.NewPackage("io", "io")
	byteT := types.Universe.Lookup("byte").Type()
	intT := types.Typ[types.Int]
	errorT := types.Universe.Lookup("error").Type()
	bytesSlice := types.NewSlice(byteT)

	newParam := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}

	// Reader is the interface that wraps the basic Read method.
	readerName := types.NewTypeName(token.NoPos, pkg, "Reader", nil)
	pkg.Scope().Insert(readerName)
	readerIface := types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, pkg, "Read",
			types.NewSignatureType(nil, nil, nil,
				types.NewTuple(newParam("p", bytesSlice)),
				types.NewTuple(newParam("n", intT), newParam("err", errorT)), false)),
	}, nil).Complete()
	readerIfaceType := types.NewNamed(readerName, readerIface, nil)

	// Writer is the interface that wraps the basic Write method.
	writerName := types.NewTypeName(token.NoPos, pkg, "Writer", nil)
	pkg.Scope().Insert(writerName)
	writerIface := types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, pkg, "Write",
			types.NewSignatureType(nil, nil, nil,
				types.NewTuple(newParam("p", bytesSlice)),
				types.NewTuple(newParam("n", intT), newParam("err", errorT)), false)),
	}, nil).Complete()
	writerIfaceType := types.NewNamed(writerName, writerIface, nil)

	_ = readerIfaceType
	_ = writerIfaceType

	// ReadAll mirrors the synthetic io.ReadAll (needed by the csv
	// mirror's NewReader): func ReadAll(r Reader) ([]byte, error).
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "ReadAll",
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(newParam("r", readerIfaceType)),
			types.NewTuple(newParam("", bytesSlice), newParam("", errorT)), false)))

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
