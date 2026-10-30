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
			formatIdx, isScan, ok := printfFormatIndex(pass, call)
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
			out = append(out, checkPrintfFormat(pass, call, format, args, isScan)...)
			return true
		})
	}
	return out
}

// printfFormatIndex reports whether call is a printf-like call, the
// index of its format-string argument, and whether it is a Scanf-family
// call (whose arguments are pointers to the scanned values, so verb
// checks must dereference one pointer level first).
func printfFormatIndex(pass *Pass, call *ast.CallExpr) (int, bool, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return 0, false, false
	}
	name := sel.Sel.Name
	// Resolve via type info first.
	if obj := pass.Info.Uses[sel.Sel]; obj != nil {
		if fn, ok := obj.(*types.Func); ok && fn.Pkg() != nil {
			switch fn.Pkg().Path() + "." + fn.Name() {
			case "fmt.Printf", "fmt.Sprintf", "fmt.Errorf",
				"log.Printf", "log.Fatalf", "log.Panicf":
				return 0, false, true
			case "fmt.Scanf":
				return 0, true, true
			case "fmt.Fprintf":
				return 1, false, true
			case "fmt.Fscanf", "fmt.Sscanf":
				return 1, true, true
			}
			// testing.T.Errorf / Fatalf are methods.
			if (name == "Errorf" || name == "Fatalf") && isTestingT(fn) {
				return 0, false, true
			}
			return 0, false, false
		}
	}
	// Syntactic fallback on package-qualified names.
	if id, ok := sel.X.(*ast.Ident); ok {
		switch id.Name + "." + name {
		case "fmt.Printf", "fmt.Sprintf", "fmt.Errorf",
			"log.Printf", "log.Fatalf", "log.Panicf":
			return 0, false, true
		case "fmt.Scanf":
			return 0, true, true
		case "fmt.Fprintf":
			return 1, false, true
		case "fmt.Fscanf", "fmt.Sscanf":
			return 1, true, true
		}
	}
	return 0, false, false
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
	text     string
	verb     rune
	indexed  bool
	argIndex int
	// stars counts '*' width/precision operands. Each consumes one
	// argument, preceding the verb's own argument.
	stars int
}

// checkPrintfFormat validates args against the format string verbs.
// scan reports whether the call is a Scanf-family call, whose
// arguments are pointers to the values being scanned.
func checkPrintfFormat(pass *Pass, call *ast.CallExpr, format string, args []ast.Expr, scan bool) []Diagnostic {
	var out []Diagnostic
	pos := pass.position(call)
	verbs := parsePrintfVerbs(format)
	argIdx := 0
	maxIndexed := 0
	for _, v := range verbs {
		if v.verb == '%' {
			continue
		}
		if v.indexed {
			if v.argIndex < 1 || v.argIndex > len(args) {
				out = append(out, Diagnostic{pos, "invalid argument index %[" +
					strconv.Itoa(v.argIndex) + "]"})
			} else {
				if v.argIndex > maxIndexed {
					maxIndexed = v.argIndex
				}
				if msg := checkVerbArg(pass, v, args[v.argIndex-1], scan); msg != "" {
					out = append(out, Diagnostic{pass.position(args[v.argIndex-1]),
						v.text + ": " + msg})
				}
			}
			continue
		}
		// '*' operands come before the verb's argument.
		starOK := true
		for s := 0; s < v.stars; s++ {
			if argIdx >= len(args) {
				out = append(out, Diagnostic{pos,
					"missing argument for " + v.text})
				starOK = false
				break
			}
			if msg := checkStarArg(pass, args[argIdx]); msg != "" {
				out = append(out, Diagnostic{pass.position(args[argIdx]),
					v.text + ": " + msg})
			}
			argIdx++
		}
		if !starOK {
			continue
		}
		if argIdx >= len(args) {
			out = append(out, Diagnostic{pos,
				"missing argument for " + v.text})
			continue
		}
		if msg := checkVerbArg(pass, v, args[argIdx], scan); msg != "" {
			out = append(out, Diagnostic{pass.position(args[argIdx]),
				v.text + ": " + msg})
		}
		argIdx++
	}
	// Arguments never consumed by any verb are printed by fmt as
	// %!(EXTRA ...); indexed verbs consume up to their index.
	used := argIdx
	if maxIndexed > used {
		used = maxIndexed
	}
	if used < len(args) {
		out = append(out, Diagnostic{pos,
			"too many arguments for format string"})
	}
	return out
}

// checkStarArg validates a '*' width/precision argument; "" means OK.
// Star arguments must be integers; unknown types are not flagged.
func checkStarArg(pass *Pass, arg ast.Expr) string {
	tv, ok := pass.Info.Types[arg]
	if !ok || tv.Type == nil {
		return ""
	}
	if b, ok := tv.Type.Underlying().(*types.Basic); ok && isIntKind(b.Kind()) {
		return ""
	}
	return "expects integer"
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
			v.stars++
			i++
		} else {
			for i < len(format) && format[i] >= '0' && format[i] <= '9' {
				i++
			}
		}
		if i < len(format) && format[i] == '.' {
			i++
			if i < len(format) && format[i] == '*' {
				v.stars++
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
// When scan is true the argument is a Scanf target, so one pointer
// level is dereferenced before the verb-type check: the only legal
// scan target for %d is *int, for %s *string, and so on.
func checkVerbArg(pass *Pass, v printfVerb, arg ast.Expr, scan bool) string {
	tv, ok := pass.Info.Types[arg]
	if !ok || tv.Type == nil {
		return ""
	}
	t := tv.Type
	if scan {
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
	}
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
	case 's':
		// %s accepts strings, byte slices (including named ones, via
		// the underlying type), and anything fmt formats via String()
		// or Error() (Stringer, error).
		if isBasic && basic.Kind() == types.String {
			break
		}
		if isByteSlice(t) {
			break
		}
		if implementsStringMethod(t) {
			break
		}
		return "expects string or []byte"
	case 'q':
		// %q accepts strings, byte slices, quoted chars (integers),
		// and Stringer/error values (fmt quotes their String() result).
		if isBasic && (basic.Kind() == types.String || isIntKind(basic.Kind())) {
			break
		}
		if isByteSlice(t) {
			break
		}
		if implementsStringMethod(t) {
			break
		}
		return "expects string, []byte, or integer"
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

// isByteSlice reports whether t's underlying type is a slice of bytes
// (uint8), covering named []byte types as well as plain []byte.
func isByteSlice(t types.Type) bool {
	s, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	b, ok := s.Elem().Underlying().(*types.Basic)
	return ok && b.Kind() == types.Uint8
}

// implementsStringMethod reports whether t (or *t) has a String() string
// or Error() string method, i.e. fmt formats it via String() or Error()
// for %s/%q. Method sets cover embedded and pointer-receiver methods.
func implementsStringMethod(t types.Type) bool {
	for _, cand := range []types.Type{t, types.NewPointer(t)} {
		for _, name := range []string{"String", "Error"} {
			sel := types.NewMethodSet(cand).Lookup(nil, name)
			if sel == nil {
				continue
			}
			sig, ok := sel.Obj().Type().(*types.Signature)
			if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
				continue
			}
			if b, ok := sig.Results().At(0).Type().Underlying().(*types.Basic); ok && b.Kind() == types.String {
				return true
			}
		}
	}
	return false
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
