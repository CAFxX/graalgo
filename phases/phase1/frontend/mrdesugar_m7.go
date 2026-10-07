// P3-M7 (wave 1): Appendix M desugar for the encoding/* synthetic
// packages' multi-result functions.
//
// Frontend/v1 cannot emit multi-result functions (WS D). The synthetic
// encoding packages declare the exact gc 1.27.1 signatures with
// fail-fast panic bodies (type-checking only); the real implementations
// live in single-result `__`-prefixed helpers returning pair/triple
// structs. When a multi-result call is the RHS of an N-target
// assignment (`:=` or `=`), the exporter desugars it to a helper call
// plus field extractions, following the P3-M5 Appendix M precedent
// (mrdesugar_m5.go).
//
// Desugared operations (all 2-result except ascii85.Decode which is
// 3-result):
//   encoding/base32: (*Encoding).Decode, (*Encoding).DecodeString,
//     (*Encoding).AppendDecode
//   encoding/base64: (*Encoding).Decode, (*Encoding).DecodeString,
//     (*Encoding).AppendDecode
//   encoding/hex: Decode, DecodeString, AppendDecode (package-level)
//   encoding/binary: Uvarint, Varint (package-level)
//   encoding/ascii85: Decode (package-level, 3 results)
//   encoding/pem: Decode (package-level)
//   encoding/asn1: Marshal, Unmarshal (package-level)
//
// The desugar requires 3 additive edits to mrdesugar_m5.go (all
// backward-compatible):
//   1. mrPlan.prependRecv field: for methods, the receiver (sel.X)
//      becomes the first helper argument.
//   2. desugarMultiResult prepends sel.X to argExprs when
//      prependRecv is set.
//   3. mrM7Pair/mrM7Triple kinds in the pair-fields extraction case,
//      and exportMultiResultDesugar allows 2- or 3-target assignments.
//
// Any other call position (single-target assignment, expression
// context, etc.) fails fast with the named M7 scope error from
// multiResultSyntheticError (synth_machinery.go).
package main

import (
	"go/ast"
	"go/types"
)

// m7MultiResultFunc describes one desugarable M7 multi-result function.
type m7MultiResultFunc struct {
	pkgPath    string   // synthetic package path
	name       string   // function/method name
	recvType   string   // receiver named type ("" for package-level funcs)
	helper     string   // __-helper name
	pairType   string   // pair/triple struct type name
	pairFields []string // field names in result order
}

// m7MultiResultFuncs is the desugar table for the encoding/* packages.
var m7MultiResultFuncs = []m7MultiResultFunc{
	// encoding/base32
	{"encoding/base32", "Decode", "Encoding", "__base32Decode", "base32DecodePair", []string{"n", "err"}},
	{"encoding/base32", "DecodeString", "Encoding", "__base32DecodeString", "base32DecodeStringPair", []string{"b", "err"}},
	{"encoding/base32", "AppendDecode", "Encoding", "__base32AppendDecode", "base32AppendPair", []string{"b", "err"}},
	// encoding/base64
	{"encoding/base64", "Decode", "Encoding", "__base64Decode", "base64DecodePair", []string{"n", "err"}},
	{"encoding/base64", "DecodeString", "Encoding", "__base64DecodeString", "base64DecodeStringPair", []string{"b", "err"}},
	{"encoding/base64", "AppendDecode", "Encoding", "__base64AppendDecode", "base64AppendPair", []string{"b", "err"}},
	// encoding/hex (package-level)
	{"encoding/hex", "Decode", "", "__hexDecode", "hexDecodePair", []string{"n", "err"}},
	{"encoding/hex", "DecodeString", "", "__hexDecodeString", "hexDecodeStringPair", []string{"b", "err"}},
	{"encoding/hex", "AppendDecode", "", "__hexAppendDecode", "hexAppendPair", []string{"b", "err"}},
	// encoding/binary (package-level)
	{"encoding/binary", "Uvarint", "", "__binaryUvarint", "binaryUvarintPair", []string{"x", "n"}},
	{"encoding/binary", "Varint", "", "__binaryVarint", "binaryVarintPair", []string{"x", "n"}},
	// encoding/ascii85 (package-level, 3 results)
	{"encoding/ascii85", "Decode", "", "__ascii85Decode", "ascii85DecodeTriple", []string{"ndst", "nsrc", "err"}},
	// encoding/pem (package-level)
	{"encoding/pem", "Decode", "", "__pemDecode", "pemDecodePair", []string{"p", "rest"}},
	// encoding/asn1 (package-level)
	{"encoding/asn1", "Marshal", "", "__asn1Marshal", "asn1MarshalPair", []string{"b", "err"}},
	{"encoding/asn1", "Unmarshal", "", "__asn1Unmarshal", "asn1UnmarshalPair", []string{"rest", "err"}},
}

// classifyM7MultiResultCall recognizes the M7 Appendix M operations. It
// returns nil when c is not one of them. Called from
// classifyMultiResultCall (mrdesugar_m5.go) after the M5 cases.
func (e *exporter) classifyM7MultiResultCall(c *ast.CallExpr, sel *ast.SelectorExpr, fn *types.Func) *mrPlan {
	if fn == nil || fn.Pkg() == nil {
		return nil
	}
	pkgPath := fn.Pkg().Path()
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil
	}
	// Must be multi-result (2 or 3 results).
	nResults := sig.Results().Len()
	if nResults != 2 && nResults != 3 {
		return nil
	}
	for _, mf := range m7MultiResultFuncs {
		if mf.pkgPath != pkgPath || mf.name != fn.Name() {
			continue
		}
		if len(mf.pairFields) != nResults {
			continue
		}
		// Check receiver for methods.
		prependRecv := false
		if mf.recvType != "" {
			recv := sig.Recv()
			if recv == nil {
				continue
			}
			rt := recv.Type()
			if pt, ok := rt.(*types.Pointer); ok {
				rt = pt.Elem()
			}
			nt, ok := rt.(*types.Named)
			if !ok || nt.Obj().Name() != mf.recvType {
				continue
			}
			// Must be the synthetic package's type, not a user type.
			if nt.Obj().Pkg() == nil || nt.Obj().Pkg().Path() != pkgPath {
				continue
			}
			prependRecv = true
		} else if sig.Recv() != nil {
			// Package-level function must not have a receiver.
			continue
		}
		// Look up the pair/triple type.
		sp := getSyntheticPackage(pkgPath)
		obj := sp.pkg.Scope().Lookup(mf.pairType)
		if obj == nil {
			return nil
		}
		nt, ok := obj.Type().(*types.Named)
		if !ok {
			return nil
		}
		kind := mrM7Pair
		if nResults == 3 {
			kind = mrM7Triple
		}
		return &mrPlan{
			kind:        kind,
			helperPkg:   pkgPath,
			helperName:  mf.helper,
			tmpType:     nt,
			pairFields:  mf.pairFields,
			prependRecv: prependRecv,
		}
	}
	return nil
}
