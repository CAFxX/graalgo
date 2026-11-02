package vet

import (
	"go/ast"
	"go/types"
	"strings"
)

func init() {
	register(&Analyzer{
		Name: "errorsas",
		Doc:  "report passing non-pointer or non-error values to errors.As",
		Run:  runErrorsAs,
	})
	register(&Analyzer{
		Name: "unmarshal",
		Doc:  "report passing non-pointer or non-interface values to unmarshal",
		Run:  runUnmarshal,
	})
	register(&Analyzer{
		Name: "unusedresult",
		Doc:  "check for unused results of calls to some functions",
		Run:  runUnusedResult,
	})
	register(&Analyzer{
		Name: "timeformat",
		Doc:  "check for calls of (time.Time).Format or time.Parse with 2006-02-01",
		Run:  runTimeFormat,
	})
	register(&Analyzer{
		Name: "appends",
		Doc:  "check for missing values after append",
		Run:  runAppends,
	})
	register(&Analyzer{
		Name: "slog",
		Doc:  "check for invalid structured logging calls",
		Run:  runSlog,
	})
}

// errorsas checks that the second argument to errors.As is a non-nil
// pointer to either a type that implements error, or to any interface type.
func runErrorsAs(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			if !pass.isPkgFunc(call, "errors", "As") {
				return true
			}
			target := call.Args[1]
			tv, ok := pass.Info.Types[target]
			if !ok || tv.Type == nil {
				return true
			}
			if !isValidErrorsAsTarget(tv.Type) {
				out = append(out, Diagnostic{
					pass.position(call.Args[1]),
					"second argument to errors.As must be a non-nil pointer to either a type that implements error, or to any interface type",
				})
			}
			return true
		})
	}
	return out
}

func isValidErrorsAsTarget(t types.Type) bool {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	elem := ptr.Elem()
	// Pointer to interface: always OK.
	if _, ok := elem.Underlying().(*types.Interface); ok {
		return true
	}
	// Pointer to type implementing error: OK.
	if named, ok := elem.(*types.Named); ok {
		// Check if *named implements error, or named implements error.
		errIface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
		if types.Implements(named, errIface) || types.Implements(types.NewPointer(named), errIface) {
			return true
		}
	}
	// Pointer to struct etc. that implements error via methods.
	errIface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	return types.Implements(elem, errIface) || types.Implements(types.NewPointer(elem), errIface)
}

// unmarshal checks that values passed to encoding/json.Unmarshal,
// encoding/xml.Unmarshal, etc. are pointers (or maps/slices/interfaces).
// The same check applies to Decoder.Decode methods (encoding/json,
// encoding/xml, encoding/gob), whose value is the first argument.
// Note: encoding/gob has no package-level Unmarshal function, and
// gob.NewDecoder takes a single reader argument, so there is no
// gob entry in unmarshalFuncs below.
func runUnmarshal(pass *Pass) []Diagnostic {
	var out []Diagnostic
	unmarshalFuncs := []struct{ pkg, name string }{
		{"encoding/json", "Unmarshal"},
		{"encoding/xml", "Unmarshal"},
	}
	// Decoder.Decode methods: the value is the first (and only) argument.
	decodeMethods := []struct{ pkg, recv, name string }{
		{"encoding/json", "Decoder", "Decode"},
		{"encoding/xml", "Decoder", "Decode"},
		{"encoding/gob", "Decoder", "Decode"},
	}
	checkValue := func(valueArg ast.Expr, op string) {
		tv, ok := pass.Info.Types[valueArg]
		if !ok || tv.Type == nil {
			return
		}
		t := tv.Type
		// OK: pointer, map, slice, interface.
		switch t.Underlying().(type) {
		case *types.Pointer, *types.Map, *types.Slice, *types.Interface:
			return
		}
		out = append(out, Diagnostic{
			pass.position(valueArg),
			"unmarshal: passing non-pointer value to " + op,
		})
	}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			// Package-level unmarshal functions: value is arg 1.
			if len(call.Args) >= 2 {
				for _, uf := range unmarshalFuncs {
					if pass.isPkgFunc(call, uf.pkg, uf.name) {
						checkValue(call.Args[1], uf.name)
						return true
					}
				}
			}
			// Decoder.Decode methods: value is arg 0.
			for _, dm := range decodeMethods {
				if pass.isPkgMethod(call, dm.pkg, dm.recv, dm.name) {
					checkValue(call.Args[0], dm.name)
					return true
				}
			}
			return true
		})
	}
	return out
}

// isPkgMethod reports whether call is a call to the method (recv).name
// declared in package pkgPath, resolved via type info. It matches
// (*encoding/json.Decoder).Decode but not an unrelated type's Decode,
// which a bare function-name match would conflate.
func (p *Pass) isPkgMethod(call *ast.CallExpr, pkgPath, recvName, methodName string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != methodName {
		return false
	}
	obj := p.Info.Uses[sel.Sel]
	fn, ok := obj.(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != pkgPath {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	recv := sig.Recv().Type()
	if ptr, ok := recv.(*types.Pointer); ok {
		recv = ptr.Elem()
	}
	named, ok := recv.(*types.Named)
	return ok && named.Obj() != nil && named.Obj().Name() == recvName
}

// unusedresult checks for unused results of calls to pure functions
// like strings.Replace, fmt.Sprintf, etc.
func runUnusedResult(pass *Pass) []Diagnostic {
	// Functions whose results should not be discarded.
	// strings/bytes coverage mirrors upstream vet's unusedresult list
	// for the pure string/byte helpers (constructors such as
	// strings.NewReader are intentionally excluded: discarding a
	// freshly built value is not a lost computation).
	pureFuncs := map[string]bool{
		"fmt.Sprintf": true, "fmt.Sprint": true, "fmt.Sprintln": true,
		"strings.Replace": true, "strings.ReplaceAll": true,
		"strings.ToUpper": true, "strings.ToLower": true,
		"strings.Trim": true, "strings.TrimSpace": true,
		"strings.TrimPrefix": true, "strings.TrimSuffix": true,
		"strings.TrimLeft": true, "strings.TrimRight": true,
		"strings.TrimFunc": true,
		"strings.Split":    true, "strings.SplitN": true,
		"strings.SplitAfter": true, "strings.SplitAfterN": true,
		"strings.Join": true, "strings.Repeat": true,
		"strings.Fields": true, "strings.FieldsFunc": true,
		"strings.Contains": true, "strings.HasPrefix": true, "strings.HasSuffix": true,
		"strings.Index": true, "strings.LastIndex": true,
		"strings.IndexByte": true, "strings.IndexAny": true, "strings.IndexFunc": true,
		"strings.Count": true, "strings.EqualFold": true, "strings.Compare": true,
		"strings.Map": true, "strings.Clone": true, "strings.ToValidUTF8": true,
		"strings.Cut": true, "strings.CutPrefix": true, "strings.CutSuffix": true,
		"bytes.Replace": true, "bytes.ReplaceAll": true,
		"bytes.ToUpper": true, "bytes.ToLower": true,
		"bytes.Trim": true, "bytes.TrimSpace": true,
		"bytes.TrimPrefix": true, "bytes.TrimSuffix": true,
		"bytes.TrimLeft": true, "bytes.TrimRight": true,
		"bytes.TrimFunc": true,
		"bytes.Split":    true, "bytes.SplitN": true,
		"bytes.SplitAfter": true, "bytes.SplitAfterN": true,
		"bytes.Join": true, "bytes.Repeat": true,
		"bytes.Fields": true, "bytes.FieldsFunc": true,
		"bytes.Contains": true, "bytes.HasPrefix": true, "bytes.HasSuffix": true,
		"bytes.Index": true, "bytes.LastIndex": true,
		"bytes.IndexByte": true, "bytes.IndexAny": true, "bytes.IndexFunc": true,
		"bytes.Count": true, "bytes.EqualFold": true, "bytes.Compare": true,
		"bytes.Map": true, "bytes.Clone": true, "bytes.ToValidUTF8": true,
		"bytes.Cut": true, "bytes.CutPrefix": true, "bytes.CutSuffix": true,
		"strconv.Itoa": true, "strconv.FormatInt": true, "strconv.FormatFloat": true,
	}
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			// Expression statement that is a call.
			stmt, ok := n.(*ast.ExprStmt)
			if !ok {
				return true
			}
			call, ok := stmt.X.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// Resolve the full package path + name.
			var key string
			if obj := pass.Info.Uses[sel.Sel]; obj != nil {
				if fn, ok := obj.(*types.Func); ok && fn.Pkg() != nil {
					key = fn.Pkg().Path() + "." + fn.Name()
				}
			}
			if key == "" {
				// Fallback: try syntactic.
				if id, ok := sel.X.(*ast.Ident); ok {
					key = id.Name + "." + sel.Sel.Name
					// Map common import names to paths.
					for _, f2 := range pass.Files {
						for _, imp := range f2.Imports {
							if importName(imp) == id.Name {
								key = importPath(imp) + "." + sel.Sel.Name
								break
							}
						}
					}
				}
			}
			if pureFuncs[key] {
				out = append(out, Diagnostic{
					pass.position(call),
					"result of " + key + " call not used",
				})
			}
			return true
		})
	}
	return out
}

// timeformat checks for the common mistake of using 2006-02-01 instead
// of 2006-01-02 in time.Format/time.Parse layouts.
func runTimeFormat(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			var layoutArg ast.Expr
			// (time.Time).Format(layout)
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Format" {
				if tv, ok := pass.Info.Types[sel.X]; ok && tv.Type != nil {
					if isTimeType(tv.Type) {
						layoutArg = call.Args[0]
					}
				}
			}
			// time.Parse(layout, value)
			if pass.isPkgFunc(call, "time", "Parse") && len(call.Args) >= 2 {
				layoutArg = call.Args[0]
			}
			if layoutArg == nil {
				return true
			}
			// Get the layout string constant.
			tv, ok := pass.Info.Types[layoutArg]
			if !ok || tv.Value == nil {
				return true
			}
			layout := constantString(tv.Value)
			if layout == "" {
				return true
			}
			if strings.Contains(layout, "2006-02-01") {
				out = append(out, Diagnostic{
					pass.position(layoutArg),
					"2006-02-01 should be 2006-01-02",
				})
			}
			return true
		})
	}
	return out
}

func isTimeType(t types.Type) bool {
	// Unwrap pointers: (*time.Time).Format must be checked just like
	// (time.Time).Format, since Format has a value receiver and is
	// callable on pointer values.
	for {
		ptr, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Pkg() != nil &&
		obj.Pkg().Path() == "time" && obj.Name() == "Time"
}

func constantString(v interface{}) string {
	// v is constant.Value; extract string.
	type stringer interface{ ExactString() string }
	if s, ok := v.(stringer); ok {
		es := s.ExactString()
		// ExactString for strings includes quotes.
		if len(es) >= 2 && es[0] == '"' {
			// Unquote.
			var out strings.Builder
			for i := 1; i < len(es)-1; i++ {
				if es[i] == '\\' && i+1 < len(es)-1 {
					i++
					switch es[i] {
					case 'n':
						out.WriteByte('\n')
					case 't':
						out.WriteByte('\t')
					case '"':
						out.WriteByte('"')
					case '\\':
						out.WriteByte('\\')
					default:
						out.WriteByte(es[i])
					}
				} else {
					out.WriteByte(es[i])
				}
			}
			return out.String()
		}
		return es
	}
	return ""
}

// appends checks for append calls whose result is discarded.
// Since append may return a new slice, discarding the result is a bug.
func runAppends(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			stmt, ok := n.(*ast.ExprStmt)
			if !ok {
				return true
			}
			call, ok := stmt.X.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "append" {
				// Check it's the builtin append (not shadowed).
				if obj := pass.Info.Uses[id]; obj != nil {
					if _, ok := obj.(*types.Builtin); !ok {
						return true
					}
				}
				tv, ok := pass.Info.Types[call]
				if ok && tv.Type != nil {
					out = append(out, Diagnostic{
						pass.position(call),
						"append(" + exprString(call.Args[0]) + ", ...) (value of type " + tv.Type.String() + ") is not used",
					})
				}
			}
			return true
		})
	}
	return out
}

// slog checks for invalid structured logging calls:
// - odd number of args (not key-value pairs)
// - non-string keys
// Arguments of type slog.Attr (including calls such as slog.String
// that produce one) are not key-value pairs and are skipped before
// pair-checking, as in real slog calls.
func runSlog(pass *Pass) []Diagnostic {
	var out []Diagnostic
	slogFuncs := map[string]bool{
		"Info": true, "Debug": true, "Warn": true, "Error": true,
		"Log": true, "LogContext": true,
		"InfoContext": true, "DebugContext": true,
		"WarnContext": true, "ErrorContext": true, "LogAttrs": true,
	}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !slogFuncs[sel.Sel.Name] {
				return true
			}
			// Check if it's log/slog.
			isSlog := false
			if obj := pass.Info.Uses[sel.Sel]; obj != nil {
				if fn, ok := obj.(*types.Func); ok && fn.Pkg() != nil {
					isSlog = fn.Pkg().Path() == "log/slog"
				}
			}
			if !isSlog {
				return true
			}
			// Args after msg should be key-value pairs.
			// For *Context variants, first arg is ctx.
			// Log/LogContext take (ctx, level, msg, args...), so
			// three leading arguments are skipped, not one/two.
			args := call.Args
			start := 1 // skip msg
			switch sel.Sel.Name {
			case "Log", "LogContext":
				start = 3 // skip ctx, level, msg
			default:
				if strings.HasSuffix(sel.Sel.Name, "Context") {
					start = 2 // skip ctx, msg
				}
			}
			if sel.Sel.Name == "LogAttrs" {
				return true // takes attrs, not pairs
			}
			if len(args) < start {
				return true // malformed call; nothing to pair-check
			}
			// Skip slog.Attr arguments: they are single values, not
			// key-value pairs, and may be mixed with real pairs.
			rest := make([]ast.Expr, 0, len(args)-start)
			for _, arg := range args[start:] {
				if isSlogAttr(pass, arg) {
					continue
				}
				rest = append(rest, arg)
			}
			if len(rest)%2 != 0 {
				out = append(out, Diagnostic{
					pass.position(call),
					"slog " + sel.Sel.Name + " call has odd number of key-value arguments",
				})
				return true
			}
			for i := 0; i < len(rest); i += 2 {
				// Key should be a string.
				tv, ok := pass.Info.Types[rest[i]]
				if !ok || tv.Type == nil {
					continue
				}
				if b, ok := tv.Type.Underlying().(*types.Basic); !ok || b.Kind() != types.String {
					out = append(out, Diagnostic{
						pass.position(rest[i]),
						"slog " + sel.Sel.Name + " key should be a string",
					})
				}
			}
			return true
		})
	}
	return out
}

// isSlogAttr reports whether arg has type log/slog.Attr. This covers
// Attr values, Attr variables, composite literals, and calls that
// produce an Attr (slog.String, slog.Int, slog.Any, slog.Group, ...),
// since all of them carry the Attr type in the type info.
func isSlogAttr(pass *Pass, arg ast.Expr) bool {
	tv, ok := pass.Info.Types[arg]
	if !ok || tv.Type == nil {
		return false
	}
	t := tv.Type
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Pkg() != nil &&
		obj.Pkg().Path() == "log/slog" && obj.Name() == "Attr"
}
