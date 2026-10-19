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

// Comparing a declared function to nil is always false (==) or always
// true (!=). Mirrors go vet's nilfunc analyzer
// (golang.org/x/tools/go/analysis/passes/nilfunc): only the *declared
// function* case fires — never a func variable, which is nilable. The
// operand is resolved through type info, so a package-qualified reference
// (pkg.Fn), a method value (v.M), an instantiated generic (F[int]), or a
// parenthesized reference all resolve to the *types.Func declaration.
func runNilFunc(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			be, ok := n.(*ast.BinaryExpr)
			if !ok || (be.Op != token.EQL && be.Op != token.NEQ) {
				return true
			}
			// Only want comparisons with a nil identifier on one side.
			var other ast.Expr
			switch {
			case isNil(pass, be.X):
				other = be.Y
			case isNil(pass, be.Y):
				other = be.X
			default:
				return true
			}
			// Only want declared functions: the operand must resolve to
			// a *types.Func in type info's Uses map.
			fn, ok := pass.Info.Uses[usedIdent(pass, other)].(*types.Func)
			if !ok {
				return true
			}
			out = append(out, Diagnostic{
				pass.position(be),
				"comparison of function " + fn.Name() + " " + be.Op.String() +
					" nil is always " + constBool(be.Op == token.NEQ),
			})
			return true
		})
	}
	return out
}

// unparen strips parentheses exactly like go/ast.Unparen (go1.22+); the
// vet package lives in the root module with a go1.21 floor, so the
// stdlib helper cannot be used here.
func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// usedIdent mirrors golang.org/x/tools nilfunc's use of
// typesinternal.UsedIdent: it returns the identifier that the type
// checker's Uses map keys on for a (possibly parenthesized, qualified,
// or instantiated) function reference.
func usedIdent(pass *Pass, e ast.Expr) *ast.Ident {
	e = unparen(e)
	// Look through type instantiation if necessary (e.g. F[int]).
	switch d := e.(type) {
	case *ast.IndexExpr:
		if pass.Info.Types[d.Index].IsType() {
			e = d.X
		}
	case *ast.IndexListExpr:
		e = d.X
	}
	switch e := unparen(e).(type) {
	// Info.Uses holds the resolved object even for selector expressions.
	case *ast.Ident:
		return e
	case *ast.SelectorExpr:
		return e.Sel
	}
	return nil
}

func isNil(pass *Pass, e ast.Expr) bool {
	tv, ok := pass.Info.Types[e]
	return ok && tv.IsNil()
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
