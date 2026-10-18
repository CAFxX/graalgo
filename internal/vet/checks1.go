package vet

import (
	"bytes"
	"go/ast"
	"go/constant"
	"go/printer"
	"go/token"
	"go/types"
	"reflect"
	"strconv"
)

func init() {
	register(&Analyzer{
		Name: "structtag",
		Doc:  "check that struct field tags conform to reflect.StructTag.Get",
		Run:  runStructTag,
	})
	register(&Analyzer{
		Name: "assign",
		Doc:  "check for useless assignments",
		Run:  runAssign,
	})
	register(&Analyzer{
		Name: "shift",
		Doc:  "check for shifts that equal or exceed the width of the integer",
		Run:  runShift,
	})
}

// structtag checks that struct field tags are well-formed: they must be
// key:"value" pairs with no duplicate keys, parseable by reflect.StructTag.
func runStructTag(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range st.Fields.List {
				if field.Tag == nil {
					continue
				}
				tagStr, err := strconv.Unquote(field.Tag.Value)
				if err != nil {
					out = append(out, Diagnostic{
						pass.position(field.Tag),
						"struct field tag is not a valid quoted string",
					})
					continue
				}
				// Use reflect.StructTag to validate, same as go vet.
				tag := reflect.StructTag(tagStr)
				seen := make(map[string]bool)
				// Manual parse to detect duplicates and malformed entries.
				for tagStr != "" {
					// Skip leading space.
					i := 0
					for i < len(tagStr) && tagStr[i] == ' ' {
						i++
					}
					tagStr = tagStr[i:]
					if tagStr == "" {
						break
					}
					// Scan key.
					i = 0
					for i < len(tagStr) && tagStr[i] > ' ' && tagStr[i] != ':' && tagStr[i] != '"' && tagStr[i] != 0x7f {
						i++
					}
					if i == 0 || i+1 >= len(tagStr) || tagStr[i] != ':' || tagStr[i+1] != '"' {
						out = append(out, Diagnostic{
							pass.position(field.Tag),
							"struct field tag has malformed key:\"value\" pair",
						})
						break
					}
					key := tagStr[:i]
					tagStr = tagStr[i+1:]
					// Scan quoted value.
					i = 1
					for i < len(tagStr) && tagStr[i] != '"' {
						if tagStr[i] == '\\' {
							i++
						}
						i++
					}
					if i >= len(tagStr) {
						out = append(out, Diagnostic{
							pass.position(field.Tag),
							"struct field tag has malformed key:\"value\" pair",
						})
						break
					}
					tagStr = tagStr[i+1:]
					if seen[key] {
						fieldName := "<field>"
						if len(field.Names) > 0 {
							fieldName = field.Names[0].Name
						}
						out = append(out, Diagnostic{
							pass.position(field.Tag),
							"struct field " + fieldName + " has duplicate tag key " + strconv.Quote(key),
						})
					}
					seen[key] = true
					_ = tag
				}
			}
			return true
		})
	}
	return out
}

// assign checks for useless assignments: x = x.
func runAssign(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok || assign.Tok != token.ASSIGN {
				return true
			}
			if len(assign.Lhs) != len(assign.Rhs) {
				return true
			}
			for i := range assign.Lhs {
				desc, ok := selfAssignDesc(pass, assign.Lhs[i], assign.Rhs[i])
				if !ok {
					continue
				}
				out = append(out, Diagnostic{
					pass.position(assign),
					"self-assignment of " + desc,
				})
			}
			return true
		})
	}
	return out
}

// selfAssignDesc reports whether lhs = rhs is a self-assignment, returning a
// description of the assigned location. Plain identifiers are verified to
// denote the same object via type info (so shadowing is not misreported).
// Structurally identical composite lvalues such as s.x = s.x, a[i] = a[i]
// or *p = *p are also self-assignments: both sides are evaluated in the
// same statement, so identical syntax denotes the same location. Expressions
// containing function calls are skipped: m[k()] = m[k()] may address
// different locations when k has side effects.
func selfAssignDesc(pass *Pass, lhs, rhs ast.Expr) (string, bool) {
	if li, ok := lhs.(*ast.Ident); ok {
		ri, ok := rhs.(*ast.Ident)
		if !ok || li.Name != ri.Name {
			return "", false
		}
		// Make sure they refer to the same object (not shadowed).
		lo := pass.Info.Uses[li]
		ro := pass.Info.Uses[ri]
		if lo == nil || ro == nil || lo != ro {
			// Fall back to Defs for definitions.
			if d, ok := pass.Info.Defs[li]; ok && d != nil {
				lo = d
			}
			if d, ok := pass.Info.Defs[ri]; ok && d != nil {
				ro = d
			}
		}
		if (lo != nil && lo == ro) || (lo == nil && ro == nil) {
			return li.Name, true
		}
		return "", false
	}
	// Composite lvalue: require structural equality and no calls.
	lhs = unwrapParens(lhs)
	rhs = unwrapParens(rhs)
	if !exprEqual(pass.Fset, lhs, rhs) {
		return "", false
	}
	if exprHasCall(lhs) {
		return "", false
	}
	return exprString(pass.Fset, lhs), true
}

// unwrapParens strips redundant parentheses from e.
func unwrapParens(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// exprString renders e without source positions, for structural comparison
// and diagnostic messages.
func exprString(fset *token.FileSet, e ast.Expr) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, e); err != nil {
		return ""
	}
	return buf.String()
}

// exprEqual reports whether a and b are structurally identical expressions.
func exprEqual(fset *token.FileSet, a, b ast.Expr) bool {
	sa, sb := exprString(fset, a), exprString(fset, b)
	return sa != "" && sa == sb
}

// exprHasCall reports whether e contains any function or method call.
func exprHasCall(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if _, ok := n.(*ast.CallExpr); ok {
			found = true
			return false
		}
		return true
	})
	return found
}

// shift checks for shifts where the shift count equals or exceeds the
// width of the (integer) left operand, e.g. x << 40 for int32 x.
func runShift(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			be, ok := n.(*ast.BinaryExpr)
			if !ok || (be.Op != token.SHL && be.Op != token.SHR) {
				return true
			}
			// Get the width of the left operand's type.
			tv, ok := pass.Info.Types[be.X]
			if !ok || tv.Type == nil {
				return true
			}
			var width int64
			if b, ok := tv.Type.Underlying().(*types.Basic); ok {
				switch b.Kind() {
				case types.Int8, types.Uint8:
					width = 8
				case types.Int16, types.Uint16:
					width = 16
				case types.Int32, types.Uint32, types.Float32:
					width = 32
				case types.Int64, types.Uint64, types.Float64:
					width = 64
				case types.Int, types.Uint, types.Uintptr:
					width = strconv.IntSize
				}
			}
			if width == 0 {
				return true
			}
			// Get the shift count if it's a constant.
			ctv, ok := pass.Info.Types[be.Y]
			if !ok || ctv.Value == nil {
				return true
			}
			count, ok := constantInt64(ctv.Value)
			if !ok {
				return true
			}
			if count >= width {
				// Format like go vet: "x (32 bits) too small for shift of 40".
				xstr := exprString(be.X)
				out = append(out, Diagnostic{
					pass.position(be),
					xstr + " (" + strconv.FormatInt(width, 10) + " bits) too small for shift of " + strconv.FormatInt(count, 10),
				})
			}
			return true
		})
	}
	return out
}

func exprString(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return "value"
}

func constantInt64(v constant.Value) (int64, bool) {
	if v == nil || v.Kind() != constant.Int {
		return 0, false
	}
	i := constant.ToInt(v)
	if i.Kind() != constant.Int {
		return 0, false
	}
	s := i.ExactString()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
