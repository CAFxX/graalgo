package vet

import (
	"go/ast"
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

// unsafeptr checks for invalid conversions of uintptr to unsafe.Pointer.
// The only valid pattern is via reflect.Value.Pointer or similar.
func runUnsafePtr(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			// Look for unsafe.Pointer(x) conversions.
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Pointer" {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "unsafe" {
				return true
			}
			// Check if the argument is a uintptr.
			arg := call.Args[0]
			tv, ok := pass.Info.Types[arg]
			if !ok || tv.Type == nil {
				return true
			}
			if b, ok := tv.Type.Underlying().(*types.Basic); ok && b.Kind() == types.Uintptr {
				// Check if it's from a valid source (reflect.Value.Pointer etc.).
				// For now, flag all uintptr -> unsafe.Pointer.
				// The valid pattern is: unsafe.Pointer(reflect.ValueOf(x).Pointer())
				// We allow if arg is a call to .Pointer().
				if !isValidUnsafePtrSource(arg) {
					out = append(out, Diagnostic{
						pass.position(call),
						"possible misuse of unsafe.Pointer: converting uintptr to unsafe.Pointer",
					})
				}
			}
			return true
		})
	}
	return out
}

func isValidUnsafePtrSource(e ast.Expr) bool {
	// Allow reflect.Value.Pointer() and reflect.Value.UnsafeAddr().
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel.Name == "Pointer" || sel.Sel.Name == "UnsafeAddr"
}

// sigchanyzer checks for signal.Notify with an unbuffered channel.
// The channel should be buffered (cap >= 1) to avoid missing signals.
func runSigChanyzer(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		// Map variable names to their make() call (for channel definitions).
		makeCalls := make(map[string]*ast.CallExpr)
		ast.Inspect(f, func(n ast.Node) bool {
			if assign, ok := n.(*ast.AssignStmt); ok && assign.Tok == token.DEFINE {
				for i, lhs := range assign.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && i < len(assign.Rhs) {
						if mc, ok := assign.Rhs[i].(*ast.CallExpr); ok {
							makeCalls[id.Name] = mc
						}
					}
				}
			}
			return true
		})

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
			// Direct make() call.
			if m, ok := ch.(*ast.CallExpr); ok {
				mc = m
			} else if id, ok := ch.(*ast.Ident); ok {
				// Variable: look up its definition.
				mc = makeCalls[id.Name]
			}
			if mc == nil {
				return true
			}
			if id, ok := mc.Fun.(*ast.Ident); ok && id.Name == "make" && len(mc.Args) >= 1 {
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

// hostport checks the format of addresses passed to net.Dial:
// - should use net.JoinHostPort instead of string concatenation
// - address should have a port
func runHostPort(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			if !pass.isPkgFunc(call, "net", "Dial") && !pass.isPkgFunc(call, "net", "DialTimeout") {
				return true
			}
			// Unix-domain sockets have no port: net.Dial("unix", "/tmp/sock")
			// is correct, and the net.JoinHostPort advice would be wrong
			// for it. Skip all address checks for unix networks.
			if tv, ok := pass.Info.Types[call.Args[0]]; ok && tv.Value != nil {
				switch constantString(tv.Value) {
				case "unix", "unixpacket":
					return true
				}
			}
			addr := call.Args[1]
			// Check for string concatenation: "host" + ":" + port
			if be, ok := addr.(*ast.BinaryExpr); ok && be.Op == token.ADD {
				out = append(out, Diagnostic{
					pass.position(addr),
					"address should use net.JoinHostPort instead of string concatenation",
				})
				return true
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
