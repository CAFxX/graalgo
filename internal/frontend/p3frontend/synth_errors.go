package p3frontend

// P3-M7 (wave 1): p3frontend mirror of the phase-1 synthetic "errors"
// package. The importer reimplements the frozen exporter's synthetic
// signatures (it cannot reuse them — unexported); this mirror pins the
// signatures so drift fails fast.
//
// The signatures match internal/frontend/synth_errors.go exactly
// (the full gc 1.27.1 public API: New, Is, As, AsType, Unwrap, Join,
// ErrUnsupported). Unexported implementation types (errorString,
// joinError, unwrapper, ...) are not visible to user code and are not
// mirrored. As keeps its exact gc signature with a fail-fast body in
// the synthetic source (it needs reflect); AsType is a real
// implementation there (generics; no reflect needed). AsType's generic
// signature is mirrored with an explicit *types.TypeParam.

import (
	"go/token"
	"go/types"
)

// syntheticErrorsPackage mirrors the P3-M7 synthetic "errors".
func syntheticErrorsPackage() *types.Package {
	pkg := types.NewPackage("errors", "errors")
	stringT := types.Typ[types.String]
	boolT := types.Typ[types.Bool]
	errorT := types.Universe.Lookup("error").Type()
	anyT := types.Universe.Lookup("any").Type()

	newParam := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	insertFunc := func(name string, params, results []*types.Var, variadic bool) {
		sig := types.NewSignatureType(nil, nil, nil,
			types.NewTuple(params...), types.NewTuple(results...), variadic)
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, name, sig))
	}

	// New(text string) error
	insertFunc("New",
		[]*types.Var{newParam("text", stringT)},
		[]*types.Var{newParam("", errorT)}, false)

	// Is(err, target error) bool
	insertFunc("Is",
		[]*types.Var{newParam("err", errorT), newParam("target", errorT)},
		[]*types.Var{newParam("", boolT)}, false)

	// As(err error, target any) bool
	insertFunc("As",
		[]*types.Var{newParam("err", errorT), newParam("target", anyT)},
		[]*types.Var{newParam("", boolT)}, false)

	// AsType[E error](err error) (E, bool): the generic signature,
	// mirrored with an explicit type parameter.
	eName := types.NewTypeName(token.NoPos, pkg, "E", nil)
	eParam := types.NewTypeParam(eName, errorT)
	asTypeSig := types.NewSignatureType(nil, nil,
		[]*types.TypeParam{eParam},
		types.NewTuple(newParam("err", errorT)),
		types.NewTuple(newParam("", eParam), newParam("", boolT)), false)
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "AsType", asTypeSig))

	// Unwrap(err error) error
	insertFunc("Unwrap",
		[]*types.Var{newParam("err", errorT)},
		[]*types.Var{newParam("", errorT)}, false)

	// Join(errs ...error) error (variadic: param type []error).
	errSlice := types.NewSlice(errorT)
	insertFunc("Join",
		[]*types.Var{newParam("errs", errSlice)},
		[]*types.Var{newParam("", errorT)}, true)

	// ErrUnsupported is a package-level error variable.
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "ErrUnsupported", errorT))

	return pkg
}
