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
	// Dot-imported callees: a bare Printf(...) where fmt (or log) is
	// dot-imported has an *ast.Ident callee, not a SelectorExpr.
	if id, ok := call.Fun.(*ast.Ident); ok {
		// Resolve via type info first.
		if obj := pass.Info.Uses[id]; obj != nil {
			if fn, ok := obj.(*types.Func); ok && fn.Pkg() != nil {
				switch fn.Pkg().Path() + "." + fn.Name() {
				case "fmt.Printf", "fmt.Sprintf", "fmt.Errorf", "fmt.Scanf",
					"log.Printf", "log.Fatalf", "log.Panicf":
					return 0, true
				case "fmt.Fprintf", "fmt.Fscanf", "fmt.Sscanf":
					return 1, true
				}
			}
			// Resolved to something that is not a printf-like func
			// (e.g. a local Printf, or fmt.Println): not a printf call.
			return 0, false
		}
		// Syntactic fallback when type info is unavailable: a bare
		// Printf-like name in a file that dot-imports the package.
		switch id.Name {
		case "Printf", "Scanf":
			if dotImports(pass, "fmt") || dotImports(pass, "log") {
				return 0, true
			}
		case "Sprintf", "Errorf":
			if dotImports(pass, "fmt") {
				return 0, true
			}
		case "Fatalf", "Panicf":
			if dotImports(pass, "log") {
				return 0, true
			}
		case "Fprintf", "Fscanf", "Sscanf":
			if dotImports(pass, "fmt") {
				return 1, true
			}
		}
		return 0, false
	}
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

// dotImports reports whether any file in the pass dot-imports pkgPath
// (import . "fmt"). Used only as a syntactic fallback when type info
// cannot resolve a bare printf-like identifier.
func dotImports(pass *Pass, pkgPath string) bool {
	for _, f := range pass.Files {
		for _, imp := range f.Imports {
			if imp.Name != nil && imp.Name.Name == "." && importPath(imp) == pkgPath {
				return true
			}
		}
	}
	return false
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
}

// checkPrintfFormat validates args against the format string verbs.
func checkPrintfFormat(pass *Pass, call *ast.CallExpr, format string, args []ast.Expr) []Diagnostic {
	var out []Diagnostic
	pos := pass.position(call)
	verbs := parsePrintfVerbs(format)
	argIdx := 0
	hasIndexed := false
	for _, v := range verbs {
		if v.verb == '%' {
			continue
		}
		if v.indexed {
			hasIndexed = true
			if v.argIndex < 1 || v.argIndex > len(args) {
				out = append(out, Diagnostic{pos, "invalid argument index %[" +
					strconv.Itoa(v.argIndex) + "]"})
			}
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
		if v.starWidth {
			argIdx++
		}
	}
	// Surplus arguments: every supplied argument must be consumed
	// by a verb (one argument per verb, plus one per '*' width or
	// precision). Consumption is counted from the verbs themselves
	// (a '*' in the verb text consumes one argument) rather than
	// from argIdx: on the current base argIdx does not yet count
	// '.*' precision stars - that accounting is the in-flight
	// F-P1-4 fix (REVIEW-2026-11-11-01) - so argIdx alone would
	// false-positive on e.g. Sprintf("%.*f", 2, 3.14). Counting
	// from the verb text is correct under either accounting.
	// Indexed verbs address arguments by number, so surplus is
	// only well-defined for sequential verbs and is skipped when
	// any indexed verb is present.
	if !hasIndexed {
		consumed := 0
		for _, v := range verbs {
			if v.verb == '%' {
				continue
			}
			consumed += 1 + strings.Count(v.text, "*")
		}
		for ; consumed < len(args); consumed++ {
			out = append(out, Diagnostic{pass.position(args[consumed]),
				"extra argument not used by format string"})
		}
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
