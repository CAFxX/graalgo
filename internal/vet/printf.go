package vet

import (
	"go/ast"
	"go/constant"
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
	text          string
	verb          rune
	indexed       bool
	argIndex      int
	starWidth     bool
	starPrecision bool
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
		// Star arguments come BEFORE the value argument in the
		// argument list, so they must be consumed (and type-checked)
		// before the value is checked. Previously the value was
		// checked against the first star argument and the stars were
		// skipped afterwards, misaligning every following verb.
		starMissing := false
		for _, star := range []bool{v.starWidth, v.starPrecision} {
			if !star {
				continue
			}
			if argIdx >= len(args) {
				out = append(out, Diagnostic{pos,
					"missing argument for " + v.text})
				starMissing = true
				break
			}
			if msg := checkStarArg(pass, args[argIdx]); msg != "" {
				out = append(out, Diagnostic{pass.position(args[argIdx]),
					v.text + ": " + msg})
			}
			argIdx++
		}
		if starMissing {
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
				v.starPrecision = true
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
		if isBasic && basic.Kind() == types.String {
			break
		}
		if t.String() == "[]byte" || t.String() == "[]uint8" {
			break
		}
		// fmt renders String() / Error() results for %s and %q,
		// so Stringer and error values are legal here.
		if implementsStringMethod(t, "String") || implementsStringMethod(t, "Error") {
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

// checkStarArg validates a width/precision '*' argument; "" means OK.
// A star argument must be an integer (any integer kind, named or not).
func checkStarArg(pass *Pass, arg ast.Expr) string {
	tv, ok := pass.Info.Types[arg]
	if !ok || tv.Type == nil {
		return ""
	}
	if basic, isBasic := tv.Type.Underlying().(*types.Basic); isBasic && isIntKind(basic.Kind()) {
		return ""
	}
	return "expects integer"
}

// implementsStringMethod reports whether t's method set contains a method
// named name with no parameters and a single string result — i.e. the
// String() or Error() method of fmt.Stringer / error.
func implementsStringMethod(t types.Type, name string) bool {
	sel := types.NewMethodSet(t).Lookup(nil, name)
	if sel == nil {
		return false
	}
	sig, ok := sel.Type().(*types.Signature)
	if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}
	basic, isBasic := sig.Results().At(0).Type().Underlying().(*types.Basic)
	return isBasic && basic.Kind() == types.String
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
