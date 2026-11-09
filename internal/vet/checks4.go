package vet

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
)

func init() {
	register(&Analyzer{
		Name: "stdmethods",
		Doc:  "check signature of methods of well-known interfaces",
		Run:  runStdMethods,
	})
	register(&Analyzer{
		Name: "testinggoroutine",
		Doc:  "report calls to (*testing.T).Fatal from goroutines started by a test",
		Run:  runTestingGoroutine,
	})
	register(&Analyzer{
		Name: "unsafeptr",
		Doc:  "check for invalid conversions of uintptr to unsafe.Pointer",
		Run:  runUnsafePtr,
	})
	register(&Analyzer{
		Name: "sigchanyzer",
		Doc:  "check for unbuffered channel of os.Signal",
		Run:  runSigChanyzer,
	})
	register(&Analyzer{
		Name: "hostport",
		Doc:  "check format of addresses passed to net.Dial",
		Run:  runHostPort,
	})
}

// stdmethods checks that methods implementing well-known interfaces
// have the correct signatures:
// - String() string
// - Error() string
// - Read([]byte) (int, error)
// - Write([]byte) (int, error)
func runStdMethods(pass *Pass) []Diagnostic {
	var out []Diagnostic
	// method name -> expected signature (as string for comparison)
	checks := []struct {
		name string
		sig  string // expected signature string
	}{
		{"String", "func() string"},
		{"Error", "func() string"},
	}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Type == nil {
				return true
			}
			name := fn.Name.Name
			for _, c := range checks {
				if name != c.name {
					continue
				}
				// Get the type of the method.
				obj := pass.Info.Defs[fn.Name]
				if obj == nil {
					continue
				}
				sig, ok := obj.Type().(*types.Signature)
				if !ok {
					continue
				}
				// Build signature string without receiver.
				var sb strings.Builder
				sb.WriteString("func(")
				params := sig.Params()
				for i := 0; i < params.Len(); i++ {
					if i > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString(params.At(i).Type().String())
				}
				sb.WriteString(")")
				results := sig.Results()
				if results.Len() == 1 {
					sb.WriteString(" " + results.At(0).Type().String())
				} else if results.Len() > 1 {
					sb.WriteString(" (")
					for i := 0; i < results.Len(); i++ {
						if i > 0 {
							sb.WriteString(", ")
						}
						sb.WriteString(results.At(i).Type().String())
					}
					sb.WriteString(")")
				}
				if sb.String() != c.sig {
					out = append(out, Diagnostic{
						pass.position(fn),
						"method " + name + sb.String() + " should have signature " + c.sig,
					})
				}
			}
			// Check Read/Write signatures.
			if name == "Read" || name == "Write" {
				obj := pass.Info.Defs[fn.Name]
				if obj == nil {
					return true
				}
				sig, ok := obj.Type().(*types.Signature)
				if !ok {
					return true
				}
				// Should be func([]byte) (int, error).
				params := sig.Params()
				results := sig.Results()
				okSig := params.Len() == 1 && results.Len() == 2
				if okSig {
					if s, ok := params.At(0).Type().(*types.Slice); !ok {
						okSig = false
					} else if b, ok := s.Elem().Underlying().(*types.Basic); !ok || b.Kind() != types.Byte {
						okSig = false
					}
					if r0, ok := results.At(0).Type().Underlying().(*types.Basic); !ok || r0.Kind() != types.Int {
						okSig = false
					}
					if !isErrorType(results.At(1).Type()) {
						okSig = false
					}
				}
				if !okSig {
					out = append(out, Diagnostic{
						pass.position(fn),
						"method " + name + " should have signature func([]byte) (int, error)",
					})
				}
			}
			return true
		})
	}
	return out
}

func isErrorType(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Pkg() == nil && obj.Name() == "error"
}

// testinggoroutine reports calls to (*testing.T).Fatal, FailNow, etc.
// from goroutines started by a test function.
func runTestingGoroutine(pass *Pass) []Diagnostic {
	var out []Diagnostic
	fatalMethods := map[string]bool{
		"Fatal": true, "Fatalf": true, "FailNow": true, "Skip": true, "Skipf": true, "SkipNow": true,
	}
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			// Only check test functions (TestXxx with *testing.T).
			if !isTestFunc(pass, fn) {
				continue
			}
			// Find go statements and check for Fatal calls inside.
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				goStmt, ok := n.(*ast.GoStmt)
				if !ok {
					return true
				}
				ast.Inspect(goStmt.Call, func(m ast.Node) bool {
					call, ok := m.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || !fatalMethods[sel.Sel.Name] {
						return true
					}
					// Check if sel.X is a *testing.T.
					tv, ok := pass.Info.Types[sel.X]
					if !ok || tv.Type == nil {
						return true
					}
					if isTestingTType(tv.Type) {
						out = append(out, Diagnostic{
							pass.position(call),
							"call to (*testing.T)." + sel.Sel.Name + " from a non-test goroutine",
						})
					}
					return true
				})
				return true
			})
		}
	}
	return out
}

func isTestFunc(pass *Pass, fn *ast.FuncDecl) bool {
	if !strings.HasPrefix(fn.Name.Name, "Test") {
		return false
	}
	if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return false
	}
	param := fn.Type.Params.List[0]
	tv, ok := pass.Info.Types[param.Type]
	if !ok || tv.Type == nil {
		// Fallback: check syntactic *testing.T.
		if star, ok := param.Type.(*ast.StarExpr); ok {
			if sel, ok := star.X.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "testing" && sel.Sel.Name == "T" {
					return true
				}
			}
		}
		return false
	}
	return isTestingTType(tv.Type)
}

func isTestingTType(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Pkg() != nil &&
		obj.Pkg().Path() == "testing" && obj.Name() == "T"
}

// unsafeptr checks for invalid conversions of uintptr to unsafe.Pointer,
// following upstream go vet (golang.org/x/tools/go/analysis/passes/unsafeptr):
// the conversion is allowed only for the safe patterns of the unsafe.Pointer
// rules — reflect.Value.Pointer/UnsafeAddr results, reflect header Data
// fields behind a pointer, and the uintptr round-trip with arithmetic —
// and dereferencing or taking the address of a reflect.SliceHeader or
// reflect.StringHeader is itself misuse. The conversion is matched by the
// target *type* (unsafe.Pointer), never by the qualifier name, so aliased
// imports (import u "unsafe") are handled.
func runUnsafePtr(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if len(x.Args) == 1 &&
					hasBasicType(pass.Info, x.Fun, types.UnsafePointer) &&
					hasBasicType(pass.Info, x.Args[0], types.Uintptr) &&
					!isSafeUintptr(pass.Info, x.Args[0]) {
					out = append(out, Diagnostic{
						pass.position(x),
						"possible misuse of unsafe.Pointer",
					})
				}
			case *ast.StarExpr:
				if t := pass.Info.Types[x].Type; isReflectHeader(t) {
					out = append(out, Diagnostic{
						pass.position(x),
						fmt.Sprintf("possible misuse of %s", t),
					})
				}
			case *ast.UnaryExpr:
				if x.Op != token.AND {
					return true
				}
				if t := pass.Info.Types[x.X].Type; isReflectHeader(t) {
					out = append(out, Diagnostic{
						pass.position(x),
						fmt.Sprintf("possible misuse of %s", t),
					})
				}
			}
			return true
		})
	}
	return out
}

// unparenExpr strips parenthesised expressions. It is ast.Unparen
// reimplemented locally: ast.Unparen is a go1.22 API and this module's
// floor is go1.21 (see go.mod), so the stdlib helper cannot be used.
func unparenExpr(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// isSafeUintptr reports whether x - already known to be a uintptr -
// is safe to convert to unsafe.Pointer.
// (Port of upstream unsafeptr.isSafeUintptr, with ast.Unparen replaced
// by unparenExpr — see above — and without alias unwrapping:
// types.Unalias is also a go1.22 API, so an explicit alias of
// reflect.Value or of a reflect header is not recognised as one.
// That is a narrow, conservative edge: such aliases are flagged rather
// than silently allowed.)
func isSafeUintptr(info *types.Info, x ast.Expr) bool {
	// Check unsafe.Pointer safety rules according to
	// https://golang.org/pkg/unsafe/#Pointer.

	switch x := unparenExpr(x).(type) {
	case *ast.SelectorExpr:
		// "(6) Conversion of a reflect.SliceHeader or
		// reflect.StringHeader Data field to or from Pointer."
		if x.Sel.Name != "Data" {
			break
		}
		// reflect.SliceHeader and reflect.StringHeader are okay,
		// but only if they are pointing at a real slice or string.
		// It's not okay to do:
		//	var x SliceHeader
		//	x.Data = uintptr(unsafe.Pointer(...))
		//	... use x ...
		//	p := unsafe.Pointer(x.Data)
		// because in the middle the garbage collector doesn't
		// see x.Data as a pointer and so x.Data may be dangling
		// by the time we get to the conversion at the end.
		// For now approximate by saying that *Header is okay
		// but Header is not.
		pt, ok := info.Types[x.X].Type.(*types.Pointer)
		if ok && isReflectHeader(pt.Elem()) {
			return true
		}

	case *ast.CallExpr:
		// "(5) Conversion of the result of reflect.Value.Pointer or
		// reflect.Value.UnsafeAddr from uintptr to Pointer."
		if len(x.Args) != 0 {
			break
		}
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok {
			break
		}
		switch sel.Sel.Name {
		case "Pointer", "UnsafeAddr":
			if isNamedType(info.Types[sel.X].Type, "reflect", "Value") {
				return true
			}
		}
	}

	// "(3) Conversion of a Pointer to a uintptr and back, with arithmetic."
	return isSafeArith(info, x)
}

// isSafeArith reports whether x is a pointer arithmetic expression that is safe
// to convert to unsafe.Pointer.
// (Port of upstream unsafeptr.isSafeArith, with ast.Unparen replaced
// by unparenExpr — see above.)
func isSafeArith(info *types.Info, x ast.Expr) bool {
	switch x := unparenExpr(x).(type) {
	case *ast.CallExpr:
		// Base case: initial conversion from unsafe.Pointer to uintptr.
		return len(x.Args) == 1 &&
			hasBasicType(info, x.Fun, types.Uintptr) &&
			hasBasicType(info, x.Args[0], types.UnsafePointer)

	case *ast.BinaryExpr:
		// "It is valid both to add and to subtract offsets from a
		// pointer in this way. It is also valid to use &^ to round
		// pointers, usually for alignment."
		switch x.Op {
		case token.ADD, token.SUB, token.AND_NOT:
			return isSafeArith(info, x.X) && !isSafeArith(info, x.Y)
		}
	}

	return false
}

// hasBasicType reports whether x's type is a types.Basic with the given kind.
// (Port of upstream unsafeptr.hasBasicType.)
func hasBasicType(info *types.Info, x ast.Expr, kind types.BasicKind) bool {
	t := info.Types[x].Type
	if t != nil {
		t = t.Underlying()
	}
	b, ok := t.(*types.Basic)
	return ok && b.Kind() == kind
}

// isReflectHeader reports whether t is reflect.SliceHeader or reflect.StringHeader.
func isReflectHeader(t types.Type) bool {
	return isNamedType(t, "reflect", "SliceHeader", "StringHeader")
}

// isNamedType reports whether t is a named type from package pkgPath
// with one of the given names. Alias types are not unwrapped (see
// isSafeUintptr): an explicit alias of a named type is not recognised.
func isNamedType(t types.Type, pkgPath string, names ...string) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != pkgPath {
		return false
	}
	for _, name := range names {
		if obj.Name() == name {
			return true
		}
	}
	return false
}

// defRHS maps each variable's defining object to the right-hand side
// expression that initializes it, from a := declaration or a var
// declaration with values. The map is keyed by object, not by name, so
// same-named variables in different scopes never collide, and only the
// defining declaration is recorded (later plain assignments do not
// overwrite it).
func defRHS(pass *Pass, f *ast.File) map[types.Object]ast.Expr {
	decls := make(map[types.Object]ast.Expr)
	ast.Inspect(f, func(n ast.Node) bool {
		switch stmt := n.(type) {
		case *ast.AssignStmt:
			if stmt.Tok != token.DEFINE {
				return true
			}
			for i, lhs := range stmt.Lhs {
				if i >= len(stmt.Rhs) {
					break
				}
				if id, ok := lhs.(*ast.Ident); ok {
					if obj := pass.Info.Defs[id]; obj != nil {
						decls[obj] = stmt.Rhs[i]
					}
				}
			}
		case *ast.ValueSpec:
			for i, name := range stmt.Names {
				if i >= len(stmt.Values) {
					break
				}
				if obj := pass.Info.Defs[name]; obj != nil {
					decls[obj] = stmt.Values[i]
				}
			}
		}
		return true
	})
	return decls
}

// isBuiltinCall reports whether call is a call to the builtin named name.
// The builtin-ness is confirmed via type info (not the bare name), so a
// user-declared function shadowing the builtin is not mistaken for it.
func isBuiltinCall(pass *Pass, call *ast.CallExpr, name string) bool {
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != name {
		return false
	}
	if tv, ok := pass.Info.Types[call.Fun]; ok && tv.IsBuiltin() {
		return true
	}
	if obj, ok := pass.Info.Uses[id].(*types.Builtin); ok && obj.Name() == name {
		return true
	}
	return false
}

// sigchanyzer checks for signal.Notify with an unbuffered channel.
// The channel should be buffered (cap >= 1) to avoid missing signals.
// The channel argument is resolved to its defining object (scope-aware),
// so same-named channels in different functions never interfere, and
// var declarations are recognised alongside := declarations.
func runSigChanyzer(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		decls := defRHS(pass, f)

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			if !pass.isPkgFunc(call, "os/signal", "Notify") {
				return true
			}
			ch := call.Args[0]
			var mc *ast.CallExpr
			switch arg := ch.(type) {
			case *ast.CallExpr:
				// Direct make() call.
				mc = arg
			case *ast.Ident:
				// Variable: look up its defining declaration by object.
				if obj := pass.Info.Uses[arg]; obj != nil {
					if rhs, ok := decls[obj]; ok {
						mc, _ = rhs.(*ast.CallExpr)
					}
				}
			}
			if mc == nil || !isBuiltinCall(pass, mc, "make") || len(mc.Args) < 1 {
				return true
			}
			// make(chan T) or make(chan T, 0) or make(chan T, 1).
			if len(mc.Args) == 1 {
				out = append(out, Diagnostic{
					pass.position(call),
					"misuse of signal.Notify: channel should be buffered",
				})
			} else if len(mc.Args) == 2 {
				if tv, ok := pass.Info.Types[mc.Args[1]]; ok && tv.Value != nil {
					if n, ok := constantInt64FromValue(tv.Value); ok && n < 1 {
						out = append(out, Diagnostic{
							pass.position(call),
							"misuse of signal.Notify: channel should be buffered",
						})
					}
				}
			}
			return true
		})
	}
	return out
}

func constantInt64FromValue(v interface{}) (int64, bool) {
	type exacter interface{ ExactString() string }
	if e, ok := v.(exacter); ok {
		s := e.ExactString()
		var n int64
		_, err := parseInt(s, &n)
		return n, err == nil
	}
	return 0, false
}

func parseInt(s string, n *int64) (int, error) {
	// Simple parse.
	neg := false
	if len(s) > 0 && s[0] == '-' {
		neg = true
		s = s[1:]
	}
	var v int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errParse
		}
		v = v*10 + int64(c-'0')
	}
	if neg {
		v = -v
	}
	*n = v
	return 0, nil
}

var errParse = errorString("parse error")

type errorString string

func (e errorString) Error() string { return string(e) }

// hostport checks the format of addresses passed to net.Dial, following
// upstream go vet (golang.org/x/tools/go/analysis/passes/hostport): an
// address built with fmt.Sprintf("%s:%d"/"%s:%s", host, port) does not work
// with IPv6 and should use net.JoinHostPort instead. The Sprintf call is
// detected directly and through a variable holding it. Two local checks
// are kept alongside it: string concatenation (only for non-constant
// addresses — a constant folding to a valid address is fine) and a
// missing-port check for constant addresses. Neither local check applies
// to unix-family networks, which have no port by design.
func runHostPort(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		decls := defRHS(pass, f)

		// checkAddr reports a diagnostic (and returns true) if e
		// is a call of the form fmt.Sprintf("%s:%d", ...) or
		// fmt.Sprintf("%s:%s", ...).
		// dialCall is non-nil if the Dial call is non-local
		// but within the same file.
		checkAddr := func(e ast.Expr, dialCall *ast.CallExpr) {
			call, ok := e.(*ast.CallExpr)
			if !ok || len(call.Args) != 3 || !pass.isPkgFunc(call, "fmt", "Sprintf") {
				return
			}
			// Examine format string.
			formatArg := call.Args[0]
			tv, ok := pass.Info.Types[formatArg]
			if !ok || tv.Value == nil {
				return
			}
			format := constant.StringVal(tv.Value)
			if format != "%s:%d" && format != "%s:%s" {
				return
			}
			// Refer to Dial call, if not adjacent.
			suffix := ""
			if dialCall != nil {
				suffix = fmt.Sprintf(" (passed to net.Dial at L%d)",
					pass.position(dialCall).Line)
			}
			out = append(out, Diagnostic{
				pass.position(formatArg),
				fmt.Sprintf("address format %q does not work with IPv6%s", format, suffix),
			})
		}

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if !pass.isPkgFunc(call, "net", "Dial") && !pass.isPkgFunc(call, "net", "DialTimeout") {
				return true
			}
			// Dial, DialTimeout and (net.Dialer).Dial all take
			// (network, address) — the address is Args[1].
			// (Dialer.DialContext's (ctx, network, address) form is
			// not covered, matching upstream.)
			if len(call.Args) < 2 {
				return true
			}
			network, addr := call.Args[0], call.Args[1]
			// Upstream check: fmt.Sprintf("%s:%d"/"%s:%s", host, port).
			switch a := addr.(type) {
			case *ast.CallExpr:
				checkAddr(a, nil)
			case *ast.Ident:
				// addr := fmt.Sprintf("%s:%d", ...)
				// ...
				// net.Dial("tcp", addr)
				if obj, ok := pass.Info.Uses[a].(*types.Var); ok {
					if rhs, ok := decls[obj]; ok {
						checkAddr(rhs, call)
					}
				}
			}
			// The remaining checks only make sense for host:port
			// addresses; unix-family networks have no port by design.
			if isUnixNetwork(pass, network) {
				return true
			}
			// Check for string concatenation: "host" + ":" + port.
			// A compile-time constant concatenation folds to a fixed
			// address and is not a JoinHostPort candidate.
			if be, ok := addr.(*ast.BinaryExpr); ok && be.Op == token.ADD {
				if tv, ok := pass.Info.Types[addr]; !ok || tv.Value == nil {
					out = append(out, Diagnostic{
						pass.position(addr),
						"address should use net.JoinHostPort instead of string concatenation",
					})
					return true
				}
			}
			// Check for address without port (constant string without colon).
			if tv, ok := pass.Info.Types[addr]; ok && tv.Value != nil {
				s := constantString(tv.Value)
				if s != "" && !strings.Contains(s, ":") {
					out = append(out, Diagnostic{
						pass.position(addr),
						"address " + s + " is missing port",
					})
				}
			}
			return true
		})
	}
	return out
}

// isUnixNetwork reports whether the network argument is a constant
// unix-family network ("unix", "unixgram", "unixpacket").
func isUnixNetwork(pass *Pass, network ast.Expr) bool {
	tv, ok := pass.Info.Types[network]
	if !ok || tv.Value == nil {
		return false
	}
	switch constantString(tv.Value) {
	case "unix", "unixgram", "unixpacket":
		return true
	}
	return false
}
