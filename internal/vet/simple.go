package vet

import (
	"go/ast"
	"go/token"
	"go/types"
)

func init() {
	register(&Analyzer{
		Name: "stringintconv",
		Doc:  "check for string(int) conversions",
		Run:  runStringIntConv,
	})
	register(&Analyzer{
		Name: "nilfunc",
		Doc:  "check for useless comparisons between functions and nil",
		Run:  runNilFunc,
	})
	register(&Analyzer{
		Name: "bools",
		Doc:  "check for common mistakes involving boolean operators",
		Run:  runBools,
	})
}

// string(int) yields a string holding the rune, which is almost never
// what the author intended (they usually want strconv.Itoa).
func runStringIntConv(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != "string" {
				return true
			}
			// Must be a conversion, not a call: check the type info.
			tv, ok := pass.Info.Types[call]
			if !ok || !tv.IsValue() {
				return true
			}
			argTv, ok := pass.Info.Types[call.Args[0]]
			if !ok || argTv.Type == nil {
				return true
			}
			// Flag string(int) where int is not rune/byte. Check the actual
			// type name, not just the kind, since rune is int32.
			t := argTv.Type
			typeName := t.String()
			if typeName == "rune" || typeName == "byte" ||
				typeName == "untyped rune" {
				return true // legitimate
			}
			if b, ok := t.Underlying().(*types.Basic); ok && isIntKind(b.Kind()) {
				out = append(out, Diagnostic{
					pass.position(call),
					"conversion from int to string yields a string of one rune, not a string of digits",
				})
			}
			return true
		})
	}
	return out
}

// Comparing a func value to nil is always false (except for a nil
// func variable, which is useless to compare). go vet flags
// comparisons where one side is a func type and the other is nil.
func runNilFunc(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			be, ok := n.(*ast.BinaryExpr)
			if !ok || (be.Op != token.EQL && be.Op != token.NEQ) {
				return true
			}
			if isNil(pass, be.X) && isFuncType(pass, be.Y) ||
				isNil(pass, be.Y) && isFuncType(pass, be.X) {
				out = append(out, Diagnostic{
					pass.position(be),
					"comparison of function to nil is always " + constBool(be.Op == token.NEQ),
				})
			}
			return true
		})
	}
	return out
}

func isNil(pass *Pass, e ast.Expr) bool {
	tv, ok := pass.Info.Types[e]
	return ok && tv.IsNil()
}

func isFuncType(pass *Pass, e ast.Expr) bool {
	tv, ok := pass.Info.Types[e]
	if !ok || tv.Type == nil {
		return false
	}
	_, ok = tv.Type.Underlying().(*types.Signature)
	return ok
}

func constBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Common boolean mistakes:
//   - x == true / x != false  (redundant)
//   - !x == y  (precedence: (!x) == y, usually meant !(x == y))
func runBools(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			be, ok := n.(*ast.BinaryExpr)
			if !ok {
				return true
			}
			// !x == y or !x != y: suspicious precedence.
			if un, ok := be.X.(*ast.UnaryExpr); ok && un.Op == token.NOT &&
				(be.Op == token.EQL || be.Op == token.NEQ) {
				out = append(out, Diagnostic{
					pass.position(be),
					"suspicious !x == y, use !(x == y) instead",
				})
				return true
			}
			if un, ok := be.Y.(*ast.UnaryExpr); ok && un.Op == token.NOT &&
				(be.Op == token.EQL || be.Op == token.NEQ) {
				out = append(out, Diagnostic{
					pass.position(be),
					"suspicious x == !y",
				})
				return true
			}
			// x == true / x == false / x != true / x != false
			if be.Op == token.EQL || be.Op == token.NEQ {
				if isBoolConst(pass, be.X) || isBoolConst(pass, be.Y) {
					// Only flag when the other side is a boolean expression
					// (not another constant).
					other := be.X
					if isBoolConst(pass, be.X) {
						other = be.Y
					}
					if tv, ok := pass.Info.Types[other]; ok && tv.Type != nil {
						if b, ok := tv.Type.Underlying().(*types.Basic); ok && b.Kind() == types.Bool {
							if !isBoolConst(pass, other) {
								out = append(out, Diagnostic{
									pass.position(be),
									"redundant comparison to boolean constant",
								})
							}
						}
					}
				}
			}
			return true
		})
	}
	return out
}

func isBoolConst(pass *Pass, e ast.Expr) bool {
	tv, ok := pass.Info.Types[e]
	return ok && tv.Value != nil && tv.Type != nil &&
		tv.Type.Underlying().String() == "bool"
}
