package vet

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strconv"
	"strings"
	"unicode/utf8"
)

func init() {
	register(&Analyzer{
		Name: "printf",
		Doc:  "check consistency of Printf format strings and arguments",
		Run:  runPrintf,
	})
}

func runPrintf(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			formatIdx, ok := printfFormatIndex(pass, call)
			if !ok || formatIdx >= len(call.Args) {
				return true
			}
			formatArg := call.Args[formatIdx]
			tv, ok := pass.Info.Types[formatArg]
			if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
				return true // non-constant format; can't check
			}
			format := constant.StringVal(tv.Value)
			args := call.Args[formatIdx+1:]
			out = append(out, checkPrintfFormat(pass, call, format, args)...)
			return true
		})
	}
	return out
}

// printfFormatIndex reports whether call is a printf-like call and the
// index of its format-string argument.
func printfFormatIndex(pass *Pass, call *ast.CallExpr) (int, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return 0, false
	}
	name := sel.Sel.Name
	// Resolve via type info first.
	if obj := pass.Info.Uses[sel.Sel]; obj != nil {
		if fn, ok := obj.(*types.Func); ok && fn.Pkg() != nil {
			switch fn.Pkg().Path() + "." + fn.Name() {
			case "fmt.Printf", "fmt.Sprintf", "fmt.Errorf", "fmt.Scanf",
				"log.Printf", "log.Fatalf", "log.Panicf":
				return 0, true
			case "fmt.Fprintf", "fmt.Fscanf":
				return 1, true
			case "fmt.Sscanf":
				return 1, true
			}
			// testing.T.Errorf / Fatalf are methods.
			if (name == "Errorf" || name == "Fatalf") && isTestingT(fn) {
				return 0, true
			}
			return 0, false
		}
	}
	// Syntactic fallback on package-qualified names.
	if id, ok := sel.X.(*ast.Ident); ok {
		switch id.Name + "." + name {
		case "fmt.Printf", "fmt.Sprintf", "fmt.Errorf", "fmt.Scanf",
			"log.Printf", "log.Fatalf", "log.Panicf":
			return 0, true
		case "fmt.Fprintf", "fmt.Fscanf", "fmt.Sscanf":
			return 1, true
		}
	}
	return 0, false
}

func isTestingT(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	recv := sig.Recv().Type().String()
	return recv == "*testing.T"
}

type printfVerb struct {
	text      string
	verb      rune
	indexed   bool
	argIndex  int
	starWidth bool
	starPrec  bool
}

// checkPrintfFormat validates args against the format string verbs.
func checkPrintfFormat(pass *Pass, call *ast.CallExpr, format string, args []ast.Expr) []Diagnostic {
	var out []Diagnostic
	pos := pass.position(call)
	verbs := parsePrintfVerbs(format)
	argIdx := 0
	for _, v := range verbs {
		if v.verb == '%' {
			continue
		}
		if v.indexed {
			if v.argIndex < 1 || v.argIndex > len(args) {
				out = append(out, Diagnostic{pos, "invalid argument index %[" +
					strconv.Itoa(v.argIndex) + "]"})
			}
			continue
		}
		// Each '*' in the width or precision consumes one argument,
		// before the value argument. (R05-V-06: the precision '*'
		// used to consume nothing, so every later verb was checked
		// against the wrong argument.)
		stars := 0
		if v.starWidth {
			stars++
		}
		if v.starPrec {
			stars++
		}
		missing := false
		for i := 0; i < stars; i++ {
			if argIdx >= len(args) {
				out = append(out, Diagnostic{pos,
					"missing argument for " + v.text})
				missing = true
				break
			}
			argIdx++
		}
		if missing {
			continue
		}
		if argIdx >= len(args) {
			out = append(out, Diagnostic{pos,
				"missing argument for " + v.text})
			continue
		}
		if msg := checkVerbArg(pass, v, args[argIdx]); msg != "" {
			out = append(out, Diagnostic{pass.position(args[argIdx]),
				v.text + ": " + msg})
		}
		argIdx++
	}
	return out
}

// parsePrintfVerbs extracts format verbs from a format string.
func parsePrintfVerbs(format string) []printfVerb {
	var out []printfVerb
	i := 0
	for i < len(format) {
		if format[i] != '%' {
			i++
			continue
		}
		start := i
		i++
		if i < len(format) && format[i] == '%' {
			out = append(out, printfVerb{text: "%%", verb: '%'})
			i++
			continue
		}
		v := printfVerb{}
		if i < len(format) && format[i] == '[' {
			if j := strings.IndexByte(format[i:], ']'); j > 0 {
				if n, err := strconv.Atoi(format[i+1 : i+j]); err == nil {
					v.indexed = true
					v.argIndex = n
					i += j + 1
				}
			}
		}
		for i < len(format) && strings.ContainsRune("+-# 0", rune(format[i])) {
			i++
		}
		if i < len(format) && format[i] == '*' {
			v.starWidth = true
			i++
		} else {
			for i < len(format) && format[i] >= '0' && format[i] <= '9' {
				i++
			}
		}
		if i < len(format) && format[i] == '.' {
			i++
			if i < len(format) && format[i] == '*' {
				v.starPrec = true
				i++
			} else {
				for i < len(format) && format[i] >= '0' && format[i] <= '9' {
					i++
				}
			}
		}
		if i < len(format) {
			r, _ := utf8.DecodeRuneInString(format[i:])
			v.verb = r
			i += utf8.RuneLen(r)
		}
		v.text = format[start:i]
		out = append(out, v)
	}
	return out
}

// checkVerbArg validates one argument against a verb; "" means OK.
func checkVerbArg(pass *Pass, v printfVerb, arg ast.Expr) string {
	tv, ok := pass.Info.Types[arg]
	if !ok || tv.Type == nil {
		return ""
	}
	t := tv.Type
	basic, isBasic := t.Underlying().(*types.Basic)

	switch v.verb {
	case 'd', 'b', 'o', 'O', 'c', 'U':
		if !isBasic || !isIntKind(basic.Kind()) {
			return "expects integer"
		}
	case 'x', 'X':
		// Works on ints, floats, strings, []byte — only flag clearly wrong types.
		if isBasic && basic.Kind() == types.Bool {
			return "expects integer, float, string, or slice"
		}
	case 'e', 'E', 'f', 'F', 'g', 'G':
		if !isBasic || !isFloatKind(basic.Kind()) {
			return "expects floating point"
		}
	case 's', 'q':
		// Accept string (including named string types), byte slices
		// and byte arrays (including named variants such as net.IP
		// and [N]byte), a single pointer to a byte slice/array,
		// error values and fmt.Stringer values. (R05-V-02: the old
		// code compared t.String() against "[]byte"/"[]uint8" and so
		// flagged all of these valid uses.) The pointer rule is
		// one level only, matching upstream vet: *string and
		// **[]byte are still flagged, as are []rune and [N]rune.
		if isBasic && basic.Kind() == types.String {
			break
		}
		if isByteSliceOrArray(t) {
			break
		}
		if implementsErrorOrStringer(t) {
			break
		}
		if _, isIface := t.Underlying().(*types.Interface); isIface {
			// The dynamic type is unknown; it may be a string,
			// []byte, error or Stringer. Don't flag.
			break
		}
		return "expects string or []byte"
	case 'v':
		// %v accepts anything.
	case 't':
		if !isBasic || basic.Kind() != types.Bool {
			return "expects boolean"
		}
	case 'p':
		if _, ok := t.Underlying().(*types.Pointer); !ok && t.String() != "unsafe.Pointer" {
			return "expects pointer"
		}
	case 'T':
		// Anything goes.
	}
	return ""
}

// errorInterface is the predeclared error interface.
var errorInterface = types.Universe.Lookup("error").Type().Underlying().(*types.Interface)

// stringerInterface is the fmt.Stringer interface, built structurally so
// no resolution of the fmt package is needed.
var stringerInterface = func() *types.Interface {
	stringMethod := types.NewFunc(token.NoPos, nil, "String",
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(),
			types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Typ[types.String])),
			false))
	return types.NewInterfaceType([]*types.Func{stringMethod}, nil).Complete()
}()

// isByteSliceOrArray reports whether t's underlying type is a slice or
// array whose element type is uint8 (byte), covering []byte, []uint8,
// [N]byte and named variants such as net.IP — or a single pointer to
// one of those. One pointer level only: **[]byte is not accepted, and
// neither is a pointer to a string (*string is flagged by upstream vet).
func isByteSliceOrArray(t types.Type) bool {
	u := t.Underlying()
	if p, ok := u.(*types.Pointer); ok {
		u = p.Elem().Underlying()
	}
	var elem types.Type
	switch s := u.(type) {
	case *types.Slice:
		elem = s.Elem()
	case *types.Array:
		elem = s.Elem()
	default:
		return false
	}
	b, ok := elem.Underlying().(*types.Basic)
	return ok && b.Kind() == types.Uint8
}

// implementsErrorOrStringer reports whether t implements error or
// fmt.Stringer. The pointer form is checked as well: a value whose
// pointer implements Stringer is routinely passed to %s/%q, and
// flagging it would be a false positive on ordinary code.
func implementsErrorOrStringer(t types.Type) bool {
	if types.Implements(t, errorInterface) || types.Implements(t, stringerInterface) {
		return true
	}
	if _, isPtr := t.Underlying().(*types.Pointer); isPtr {
		return false
	}
	if _, isIface := t.Underlying().(*types.Interface); isIface {
		return false
	}
	pt := types.NewPointer(t)
	return types.Implements(pt, errorInterface) || types.Implements(pt, stringerInterface)
}

func isIntKind(k types.BasicKind) bool {
	switch k {
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64,
		types.Uintptr, types.UntypedInt, types.UntypedRune:
		return true
	}
	return false
}

func isFloatKind(k types.BasicKind) bool {
	return k == types.Float32 || k == types.Float64
}
