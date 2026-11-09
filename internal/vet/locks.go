package vet

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
)

func init() {
	register(&Analyzer{
		Name: "atomic",
		Doc:  "check for common mistakes using the sync/atomic package",
		Run:  runAtomic,
	})
	register(&Analyzer{
		Name: "copylocks",
		Doc:  "check for locks erroneously passed by value",
		Run:  runCopyLocks,
	})
	register(&Analyzer{
		Name: "lostcancel",
		Doc:  "check cancel func returned by context.WithCancel is called",
		Run:  runLostCancel,
	})
}

// atomic checks, mirroring upstream go vet's atomic analyzer:
//   - assigning the result of atomic.Add* back to the same variable:
//     x = atomic.AddInt32(&x, 1)
//   - direct assignment to an atomic-typed variable or field:
//     v = atomic.Int64{} (use v.Store instead)
//
// The previous check flagged assignments to a *field of* an atomic
// value (v.field = ...), which cannot occur on compilable code:
// atomic.Value, atomic.Int64, atomic.Bool, etc. have no exported
// fields. It is replaced by the two checks above, which operate on
// the type of the assignment target instead.
func runAtomic(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			if len(assign.Lhs) != len(assign.Rhs) {
				return true
			}
			// A single := declares a fresh variable; assigning an
			// Add* result to it is fine.
			if len(assign.Lhs) == 1 && assign.Tok == token.DEFINE {
				return true
			}
			for i, rhs := range assign.Rhs {
				if call, ok := rhs.(*ast.CallExpr); ok && isAtomicAdd(pass, call) {
					checkAtomicAddAssignment(pass, assign.Lhs[i], call, &out)
				}
			}
			if assign.Tok != token.ASSIGN {
				return true
			}
			for _, lhs := range assign.Lhs {
				if isAtomicValueType(pass, lhs) {
					out = append(out, Diagnostic{
						pass.position(lhs),
						"direct assignment to atomic value; use atomic.Store or the typed Store method",
					})
				}
			}
			return true
		})
	}
	return out
}

// atomicAddFuncs is the upstream set of sync/atomic functions whose
// return value must not be assigned back to the same variable.
var atomicAddFuncs = []string{
	"AddInt32", "AddInt64", "AddUint32", "AddUint64", "AddUintptr",
}

// isAtomicAdd reports whether call is a call to one of the
// sync/atomic Add* functions.
func isAtomicAdd(pass *Pass, call *ast.CallExpr) bool {
	for _, name := range atomicAddFuncs {
		if pass.isPkgFunc(call, "sync/atomic", name) {
			return true
		}
	}
	return false
}

// checkAtomicAddAssignment flags assignments of the form
// x = atomic.AddInt32(&x, 1) and *p = atomic.AddInt32(p, 1), where
// the Add* result is assigned back to the same variable being
// operated on. This mirrors upstream's checkAtomicAddAssignment.
func checkAtomicAddAssignment(pass *Pass, lhs ast.Expr, call *ast.CallExpr, out *[]Diagnostic) {
	if len(call.Args) != 2 {
		return
	}
	left := formatExpr(pass, lhs)
	if left == "" {
		return
	}
	arg := call.Args[0]
	broken := false
	if uarg, ok := arg.(*ast.UnaryExpr); ok && uarg.Op == token.AND {
		if right := formatExpr(pass, uarg.X); right != "" {
			broken = left == right
		}
	} else if star, ok := unparen(lhs).(*ast.StarExpr); ok {
		if right := formatExpr(pass, arg); right != "" {
			broken = formatExpr(pass, star.X) == right
		}
	}
	if broken {
		*out = append(*out, Diagnostic{
			pass.position(lhs),
			"direct assignment to atomic value",
		})
	}
}

// formatExpr renders e in canonical form for comparison, or "" if it
// cannot be rendered. Parentheses are stripped so (x) and x compare
// equal.
func formatExpr(pass *Pass, e ast.Expr) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, pass.Fset, unparen(e)); err != nil {
		return ""
	}
	return buf.String()
}

// isAtomicValueType reports whether e denotes an atomic value itself
// (atomic.Int64, atomic.Bool, atomic.Value, etc.) — i.e. its type is
// a named type from sync/atomic. Pointers to atomic values are not
// atomic values: assigning a *atomic.Int64 is fine.
func isAtomicValueType(pass *Pass, e ast.Expr) bool {
	tv, ok := pass.Info.Types[e]
	if !ok || tv.Type == nil {
		return false
	}
	named, ok := tv.Type.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "sync/atomic"
}

// copylocks: passing a lock (sync.Mutex, sync.RWMutex, sync.WaitGroup,
// or a struct containing one) by value.
func runCopyLocks(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				// Check arguments passed by value.
				for _, arg := range n.Args {
					if isLock(pass, arg) {
						out = append(out, Diagnostic{
							pass.position(arg),
							"call copies lock value",
						})
					}
				}
			case *ast.ReturnStmt:
				for _, r := range n.Results {
					if isLock(pass, r) {
						out = append(out, Diagnostic{
							pass.position(r),
							"return copies lock value",
						})
					}
				}
			case *ast.AssignStmt:
				// x = lockVal copies; but x := lockVal at declaration
				// is also a copy. Flag plain assignments of lock values.
				if n.Tok == token.ASSIGN || n.Tok == token.DEFINE {
					for i, lhs := range n.Lhs {
						if i < len(n.Rhs) && isLock(pass, n.Rhs[i]) {
							// Don't flag if LHS is also a lock-typed var
							// being initialized (still a copy, but idiomatic
							// in some patterns). go vet flags all copies.
							_ = lhs
							out = append(out, Diagnostic{
								pass.position(n.Rhs[i]),
								"assignment copies lock value",
							})
						}
					}
				}
			}
			return true
		})
	}
	return out
}

// isLock reports whether e has a type that contains a sync lock.
func isLock(pass *Pass, e ast.Expr) bool {
	tv, ok := pass.Info.Types[e]
	if !ok || tv.Type == nil {
		return false
	}
	return typeContainsLock(tv.Type, make(map[types.Type]bool))
}

func typeContainsLock(t types.Type, seen map[types.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	// Pointers are safe to copy: *sync.Mutex is fine, sync.Mutex is
	// the lock. Do not unwrap — copying a pointer copies no lock.
	if _, ok := t.(*types.Pointer); ok {
		return false
	}
	// Arrays copy their elements, so recurse into the element type.
	// (Slices are not recursed into: copying a slice header copies
	// no element.)
	if a, ok := t.Underlying().(*types.Array); ok {
		return typeContainsLock(a.Elem(), seen)
	}
	named, ok := t.(*types.Named)
	if !ok {
		// Check struct types directly.
		if s, ok := t.Underlying().(*types.Struct); ok {
			return structContainsLock(s, seen)
		}
		return false
	}
	obj := named.Obj()
	if obj.Pkg() != nil && obj.Pkg().Path() == "sync" {
		switch obj.Name() {
		case "Mutex", "RWMutex", "WaitGroup":
			return true
		}
	}
	return structContainsLock(named.Underlying(), seen)
}

func structContainsLock(t types.Type, seen map[types.Type]bool) bool {
	s, ok := t.(*types.Struct)
	if !ok {
		return false
	}
	for i := 0; i < s.NumFields(); i++ {
		if typeContainsLock(s.Field(i).Type(), seen) {
			return true
		}
	}
	return false
}

// lostcancel: the cancel func returned by context.WithCancel(WithTimeout,
// WithDeadline) must be called, or the context leaks.
func runLostCancel(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			out = append(out, checkLostCancelInFunc(pass, fn)...)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.FuncLit)
			if ok && lit.Body != nil {
				// Wrap as synthetic FuncDecl for reuse.
				fd := &ast.FuncDecl{Body: lit.Body}
				out = append(out, checkLostCancelInFunc(pass, fd)...)
			}
			return true
		})
	}
	return out
}

func checkLostCancelInFunc(pass *Pass, fn *ast.FuncDecl) []Diagnostic {
	var out []Diagnostic
	// Find ctx, cancel := context.WithCancel(...) assignments.
	type cancelVar struct {
		id   *ast.Ident
		pos  token.Position
		used bool
	}
	var cancels []cancelVar
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || (assign.Tok != token.DEFINE && assign.Tok != token.ASSIGN) {
			return true
		}
		if len(assign.Lhs) != 2 || len(assign.Rhs) != 1 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok || !pass.isPkgFunc(call, "context", "WithCancel") &&
			!pass.isPkgFunc(call, "context", "WithTimeout") &&
			!pass.isPkgFunc(call, "context", "WithDeadline") &&
			!pass.isPkgFunc(call, "context", "WithCancelCause") {
			return true
		}
		if id, ok := assign.Lhs[1].(*ast.Ident); ok && id.Name != "_" {
			cancels = append(cancels, cancelVar{id: id, pos: pass.position(assign)})
		}
		return true
	})
	if len(cancels) == 0 {
		return nil
	}
	// Check each cancel var is called or returned/passed.
	used := make(map[*ast.Ident]bool)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			// cancel()
			if id, ok := n.Fun.(*ast.Ident); ok {
				for _, c := range cancels {
					if id.Name == c.id.Name {
						used[c.id] = true
					}
				}
			}
			// defer cancel()
			if id, ok := n.Fun.(*ast.Ident); ok {
				_ = id
			}
		case *ast.DeferStmt:
			if call, ok := n.Call.Fun.(*ast.Ident); ok {
				for _, c := range cancels {
					if call.Name == c.id.Name {
						used[c.id] = true
					}
				}
			}
		case *ast.GoStmt:
			if call, ok := n.Call.Fun.(*ast.Ident); ok {
				for _, c := range cancels {
					if call.Name == c.id.Name {
						used[c.id] = true
					}
				}
			}
		case *ast.ReturnStmt:
			for _, r := range n.Results {
				if id, ok := r.(*ast.Ident); ok {
					for _, c := range cancels {
						if id.Name == c.id.Name {
							used[c.id] = true
						}
					}
				}
			}
		}
		// Also: passing cancel as an argument counts as use.
		if call, ok := n.(*ast.CallExpr); ok {
			for _, arg := range call.Args {
				if id, ok := arg.(*ast.Ident); ok {
					for _, c := range cancels {
						if id.Name == c.id.Name {
							used[c.id] = true
						}
					}
				}
			}
		}
		return true
	})
	for _, c := range cancels {
		if !used[c.id] {
			out = append(out, Diagnostic{
				c.pos,
				"the cancel function is not used on all paths (possible context leak)",
			})
		}
	}
	return out
}

// unparen strips parentheses from e. (ast.Unparen requires go1.22;
// the module is go1.21.)
func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}
