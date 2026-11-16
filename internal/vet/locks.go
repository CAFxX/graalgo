package vet

import (
	"go/ast"
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

// atomic checks:
// - direct assignment to sync/atomic.Value or atomic.Int64 etc. (should use Store)
// - copying a lock/atomic value (overlaps copylocks, but atomic-specific)
func runAtomic(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			// Look for assignments to fields of atomic types.
			assign, ok := n.(*ast.AssignStmt)
			if !ok || assign.Tok != token.ASSIGN {
				return true
			}
			for _, lhs := range assign.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				if isAtomicType(pass, sel.X) {
					out = append(out, Diagnostic{
						pass.position(assign),
						"direct assignment to atomic value; use atomic.Store or the typed Store method",
					})
				}
			}
			return true
		})
	}
	return out
}

// isAtomicType reports whether e denotes a sync/atomic typed value
// (atomic.Int64, atomic.Bool, atomic.Value, etc.).
func isAtomicType(pass *Pass, e ast.Expr) bool {
	tv, ok := pass.Info.Types[e]
	if !ok || tv.Type == nil {
		return false
	}
	t := tv.Type
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
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
				// Check arguments passed by value. Builtins such as len
				// use their argument for its type only; skip them.
				if isBuiltinCall(pass, n) {
					return true
				}
				for _, arg := range n.Args {
					if rhsCopiesLock(pass, arg) {
						out = append(out, Diagnostic{
							pass.position(arg),
							"call copies lock value",
						})
					}
				}
			case *ast.ReturnStmt:
				for _, r := range n.Results {
					if rhsCopiesLock(pass, r) {
						out = append(out, Diagnostic{
							pass.position(r),
							"return copies lock value",
						})
					}
				}
			case *ast.GenDecl:
				// var x T = lockVal copies, just like an assignment.
				if n.Tok != token.VAR {
					return true
				}
				for _, spec := range n.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, v := range vs.Values {
						if i < len(vs.Names) && rhsCopiesLock(pass, v) {
							out = append(out, Diagnostic{
								pass.position(v),
								"variable declaration copies lock value",
							})
						}
					}
				}
			case *ast.CompositeLit:
				// A lock value copied INTO a literal is a real copy,
				// even though the literal itself constructs a fresh
				// value (so it is skipped as an assignment/call/return
				// argument). Check each element.
				for _, elt := range n.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						elt = kv.Value
					}
					if rhsCopiesLock(pass, elt) {
						out = append(out, Diagnostic{
							pass.position(elt),
							"literal copies lock value",
						})
					}
				}
			case *ast.AssignStmt:
				// x = lockVal copies; but x := lockVal at declaration
				// is also a copy. Flag plain assignments of lock values.
				if n.Tok == token.ASSIGN || n.Tok == token.DEFINE {
					for i, lhs := range n.Lhs {
						if i < len(n.Rhs) && rhsCopiesLock(pass, n.Rhs[i]) {
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

// rhsCopiesLock reports whether copying the value denoted by e copies a
// lock, mirroring gc's lockPathRhs: a composite literal constructs a fresh
// value, and a call result may be a zero value, so neither counts as a
// copy at the assignment/call/return/var-declaration site (any copy is
// reported where the value is built or returned instead). A dereference
// of a call result is likewise exempt; a dereference of an existing
// pointer is a real copy and still counts.
func rhsCopiesLock(pass *Pass, e ast.Expr) bool {
	x := e
	for {
		if par, ok := x.(*ast.ParenExpr); ok {
			x = par.X
			continue
		}
		break
	}
	switch y := x.(type) {
	case *ast.CompositeLit:
		return false
	case *ast.CallExpr:
		return false
	case *ast.StarExpr:
		inner := y.X
		for {
			if par, ok := inner.(*ast.ParenExpr); ok {
				inner = par.X
				continue
			}
			break
		}
		if _, ok := inner.(*ast.CallExpr); ok {
			return false
		}
	}
	return isLock(pass, e)
}

// isBuiltinCall reports whether ce calls len, cap, Sizeof, Offsetof, or
// Alignof -- builtins whose argument is used for its type only (or, for
// len/cap of a slice, does not copy it). gc's vet exempts them.
func isBuiltinCall(pass *Pass, ce *ast.CallExpr) bool {
	var id *ast.Ident
	switch fun := ce.Fun.(type) {
	case *ast.Ident:
		id = fun
	case *ast.SelectorExpr:
		id = fun.Sel
	default:
		return false
	}
	if obj, ok := pass.Info.Uses[id].(*types.Builtin); ok {
		switch obj.Name() {
		case "len", "cap", "Sizeof", "Offsetof", "Alignof":
			return true
		}
	}
	return false
}

// isLock reports whether e has a type that contains a sync lock.
func isLock(pass *Pass, e ast.Expr) bool {
	tv, ok := pass.Info.Types[e]
	if !ok || tv.Type == nil {
		return false
	}
	return typeContainsLock(tv.Type, make(map[types.Type]bool))
}

// lockerIface is the sync.Locker interface (Lock/Unlock with no
// arguments or results), built literally so the check needs no import
// of sync. A type is a lock when a pointer to it implements Locker but
// a value does not -- the same rule gc's copylocks uses.
var lockerIface = types.NewInterfaceType([]*types.Func{
	types.NewFunc(token.NoPos, nil, "Lock", types.NewSignatureType(nil, nil, nil, types.NewTuple(), types.NewTuple(), false)),
	types.NewFunc(token.NoPos, nil, "Unlock", types.NewSignatureType(nil, nil, nil, types.NewTuple(), types.NewTuple(), false)),
}, nil).Complete()

func typeContainsLock(t types.Type, seen map[types.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	// A pointer is not a lock value: passing *sync.Mutex copies only the
	// pointer, never the lock. Do not unwrap pointers.
	if _, ok := t.(*types.Pointer); ok {
		return false
	}
	// An array value embeds its elements, so copying the array copies
	// any lock it holds. (Named array types unwrap through Underlying,
	// so type Mutexes [2]sync.Mutex is covered too.)
	if arr, ok := t.Underlying().(*types.Array); ok {
		return typeContainsLock(arr.Elem(), seen)
	}
	// Only structs can contain a lock value. Slices, maps, interfaces,
	// channels, and function values are headers or pointers to the data:
	// copying them copies no lock, and gc's vet is silent on all of these
	// (verified against go1.27.1 go vet).
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	// A type is a lock when a pointer to it implements sync.Locker but
	// a value does not. This is gc's copylocks rule, applied
	// structurally: it covers sync.Mutex and sync.RWMutex directly,
	// sync.noCopy and sync/atomic.noCopy (both have pointer-receiver
	// Lock/Unlock), and any user type with pointer-receiver Lock/Unlock
	// methods. Defined types aliasing a lock (type MyMutex sync.Mutex)
	// are caught through their noCopy field, not by name.
	if types.Implements(types.NewPointer(t), lockerIface) && !types.Implements(t, lockerIface) {
		return true
	}
	return structContainsLock(st, seen)
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
