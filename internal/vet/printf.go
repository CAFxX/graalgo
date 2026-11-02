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
			info, ok := printfFormatIndex(pass, call)
			if !ok || info.formatIdx >= len(call.Args) {
				return true
			}
			formatArg := call.Args[info.formatIdx]
			tv, ok := pass.Info.Types[formatArg]
			if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
				return true // non-constant format; can't check
			}
			format := constant.StringVal(tv.Value)
			args := call.Args[info.formatIdx+1:]
			out = append(out, checkPrintfFormat(pass, call, format, args, info.scan, info.errorf)...)
			return true
		})
	}
	return out
}

// printfCallInfo describes a printf-like call: the index of its
// format-string argument, whether it is a scan call (whose arguments
// are destinations, i.e. pointers), and whether it is fmt.Errorf
// (the only call where %w is allowed).
type printfCallInfo struct {
	formatIdx int
	scan      bool
	errorf    bool
}

// printfFormatIndex reports whether call is a printf-like call and
// describes it.
func printfFormatIndex(pass *Pass, call *ast.CallExpr) (printfCallInfo, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return printfCallInfo{}, false
	}
	name := sel.Sel.Name
	// Resolve via type info first.
	if obj := pass.Info.Uses[sel.Sel]; obj != nil {
		if fn, ok := obj.(*types.Func); ok && fn.Pkg() != nil {
			switch fn.Pkg().Path() + "." + fn.Name() {
			case "fmt.Printf", "fmt.Sprintf", "log.Printf", "log.Fatalf", "log.Panicf":
				return printfCallInfo{formatIdx: 0}, true
			case "fmt.Errorf":
				return printfCallInfo{formatIdx: 0, errorf: true}, true
			case "fmt.Scanf":
				return printfCallInfo{formatIdx: 0, scan: true}, true
			case "fmt.Fprintf", "fmt.Appendf":
				return printfCallInfo{formatIdx: 1}, true
			case "fmt.Fscanf", "fmt.Sscanf":
				return printfCallInfo{formatIdx: 1, scan: true}, true
			}
			// testing.T printf-like methods.
			if isTestingTMethod(pass, sel, fn) {
				switch name {
				case "Errorf", "Fatalf", "Logf", "Skipf":
					return printfCallInfo{formatIdx: 0}, true
				}
			}
			return printfCallInfo{}, false
		}
	}
	// Syntactic fallback on package-qualified names.
	if id, ok := sel.X.(*ast.Ident); ok {
		switch id.Name + "." + name {
		case "fmt.Printf", "fmt.Sprintf", "log.Printf", "log.Fatalf", "log.Panicf":
			return printfCallInfo{formatIdx: 0}, true
		case "fmt.Errorf":
			return printfCallInfo{formatIdx: 0, errorf: true}, true
		case "fmt.Scanf":
			return printfCallInfo{formatIdx: 0, scan: true}, true
		case "fmt.Fprintf", "fmt.Appendf":
			return printfCallInfo{formatIdx: 1}, true
		case "fmt.Fscanf", "fmt.Sscanf":
			return printfCallInfo{formatIdx: 1, scan: true}, true
		}
	}
	return printfCallInfo{}, false
}

func isTestingT(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	recv := sig.Recv().Type().String()
	return recv == "*testing.T"
}

// isTestingTMethod reports whether call invokes a method on a *testing.T
// value. The selection's receiver type is authoritative here: several
// testing.T methods (Logf, Skipf) are promoted from an embedded type,
// so the declared receiver of the method is not *testing.T even though
// the call's receiver is.
func isTestingTMethod(pass *Pass, sel *ast.SelectorExpr, fn *types.Func) bool {
	if s, ok := pass.Info.Selections[sel]; ok && s != nil {
		return s.Recv().String() == "*testing.T"
	}
	return isTestingT(fn)
}

type printfVerb struct {
	text               string
	verb               rune
	indexed            bool
	argIndex           int
	starWidth          bool
	starWidthIndex     int // 0 = sequential (next argument); >0 = explicit [n]*
	starPrecision      bool
	starPrecisionIndex int // 0 = sequential (next argument); >0 = explicit [n]*
}

// checkPrintfFormat validates args against the format string verbs.
// Arguments are consumed in order: a width star, then a precision star,
// then the value. Indexed verbs check the argument they name and, per
// Go fmt semantics, make the following sequential verb use the next
// argument after the indexed one. Surplus arguments are reported for
// print-family calls only, and only when the format uses no explicit
// argument index: with indices, arguments may legitimately be skipped
// or revisited, so arity cannot be judged positionally (this matches
// upstream go vet, which likewise skips the count check for indexed
// formats). Scan calls take destinations, not values, so their arity
// is not judged here.
func checkPrintfFormat(pass *Pass, call *ast.CallExpr, format string, args []ast.Expr, scan, errorf bool) []Diagnostic {
	var out []Diagnostic
	pos := pass.position(call)
	verbs := parsePrintfVerbs(format)
	argIdx := 0
	hasIndexed := false
	for _, v := range verbs {
		if v.verb == '%' {
			continue
		}
		if v.verb == 0 {
			out = append(out, Diagnostic{pos, "missing verb in " + v.text})
			continue
		}
		if v.indexed {
			hasIndexed = true
			if v.argIndex < 1 || v.argIndex > len(args) {
				out = append(out, Diagnostic{pos, "invalid argument index %[" +
					strconv.Itoa(v.argIndex) + "]"})
				continue
			}
			// Explicitly indexed stars name their own argument;
			// an unindexed star on an indexed verb has no
			// well-defined sequential position, so it is left
			// unchecked rather than checked against the wrong arg.
			if v.starWidth && v.starWidthIndex > 0 {
				out = append(out, checkIndexedStar(pass, pos, v.text, v.starWidthIndex, args, "width")...)
			}
			if v.starPrecision && v.starPrecisionIndex > 0 {
				out = append(out, checkIndexedStar(pass, pos, v.text, v.starPrecisionIndex, args, "precision")...)
			}
			if d, ok := printfValueDiagnostic(pass, pos, v, args[v.argIndex-1], scan, errorf); ok {
				out = append(out, d)
			}
			argIdx = v.argIndex
			continue
		}
		if v.starWidth {
			if argIdx >= len(args) {
				out = append(out, Diagnostic{pos, "missing argument for " + v.text})
				continue
			}
			if msg := checkStarArg(pass, args[argIdx]); msg != "" {
				out = append(out, Diagnostic{pass.position(args[argIdx]),
					v.text + ": width argument " + msg})
			}
			argIdx++
		}
		if v.starPrecision {
			if argIdx >= len(args) {
				out = append(out, Diagnostic{pos, "missing argument for " + v.text})
				continue
			}
			if msg := checkStarArg(pass, args[argIdx]); msg != "" {
				out = append(out, Diagnostic{pass.position(args[argIdx]),
					v.text + ": precision argument " + msg})
			}
			argIdx++
		}
		if argIdx >= len(args) {
			out = append(out, Diagnostic{pos,
				"missing argument for " + v.text})
			continue
		}
		if d, ok := printfValueDiagnostic(pass, pos, v, args[argIdx], scan, errorf); ok {
			out = append(out, d)
		}
		argIdx++
	}
	if !scan && !hasIndexed {
		for i := argIdx; i < len(args); i++ {
			out = append(out, Diagnostic{pass.position(args[i]),
				"too many arguments"})
		}
	}
	return out
}

// checkIndexedStar validates one explicitly indexed star argument.
func checkIndexedStar(pass *Pass, pos token.Position, verbText string, starIndex int, args []ast.Expr, kind string) []Diagnostic {
	if starIndex < 1 || starIndex > len(args) {
		return []Diagnostic{{pos, "invalid argument index %[" +
			strconv.Itoa(starIndex) + "]"}}
	}
	if msg := checkStarArg(pass, args[starIndex-1]); msg != "" {
		return []Diagnostic{{pass.position(args[starIndex-1]),
			verbText + ": " + kind + " argument " + msg}}
	}
	return nil
}

// printfValueDiagnostic validates one value argument against a verb.
// It returns a diagnostic and true when the argument is wrong; unknown
// verbs and %w outside Errorf are format problems, not type problems,
// but they still consume their argument so later accounting stays right.
func printfValueDiagnostic(pass *Pass, callPos token.Position, v printfVerb, arg ast.Expr, scan, errorf bool) (Diagnostic, bool) {
	if !isKnownPrintfVerb(v.verb) {
		return Diagnostic{callPos, v.text + ": unknown verb"}, true
	}
	if v.verb == 'w' && !errorf {
		return Diagnostic{pass.position(arg), v.text + ": %w verb is only allowed in Errorf"}, true
	}
	if msg := checkVerbArg(pass, v, arg, scan); msg != "" {
		return Diagnostic{pass.position(arg), v.text + ": " + msg}, true
	}
	return Diagnostic{}, false
}

// isKnownPrintfVerb reports whether r is a verb Go's fmt package
// understands ('%' is handled by the caller, not here).
func isKnownPrintfVerb(r rune) bool {
	switch r {
	case 'b', 'c', 'd', 'e', 'E', 'f', 'F', 'g', 'G', 'o', 'O',
		'p', 'q', 's', 't', 'T', 'U', 'v', 'w', 'x', 'X':
		return true
	}
	return false
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
			v.starWidthIndex = parseStarIndex(format, &i)
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
				v.starPrecisionIndex = parseStarIndex(format, &i)
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

// parseStarIndex parses an optional [n] following a star width or
// precision, advancing i past it; it returns 0 when there is none.
func parseStarIndex(format string, i *int) int {
	if *i < len(format) && format[*i] == '[' {
		if j := strings.IndexByte(format[*i:], ']'); j > 0 {
			if n, err := strconv.Atoi(format[*i+1 : *i+j]); err == nil {
				*i += j + 1
				return n
			}
		}
	}
	return 0
}

// checkStarArg validates a width/precision star argument; "" means OK.
// Interface-typed arguments are left alone: the concrete value's kind
// is not statically knowable.
func checkStarArg(pass *Pass, arg ast.Expr) string {
	tv, ok := pass.Info.Types[arg]
	if !ok || tv.Type == nil || tv.Type == types.Typ[types.Invalid] {
		return ""
	}
	if _, isIface := tv.Type.Underlying().(*types.Interface); isIface {
		return ""
	}
	if basic, isBasic := tv.Type.Underlying().(*types.Basic); isBasic && isIntKind(basic.Kind()) {
		return ""
	}
	return "expects integer"
}

// checkVerbArg validates one argument against a verb; "" means OK.
// For scan calls, arg is a destination: pointer layers are dereferenced
// and the pointed-to type is checked, and a type implementing
// fmt.Scanner is accepted for any verb.
func checkVerbArg(pass *Pass, v printfVerb, arg ast.Expr, scan bool) string {
	tv, ok := pass.Info.Types[arg]
	if !ok || tv.Type == nil {
		return ""
	}
	t := tv.Type
	if t == types.Typ[types.Invalid] {
		return ""
	}
	if scan {
		if implementsScannerType(t) {
			return ""
		}
		if v.verb == 'p' {
			// A scan %p destination is itself a pointer.
			if _, ok := t.Underlying().(*types.Pointer); !ok && t.String() != "unsafe.Pointer" {
				return "expects pointer"
			}
			return ""
		}
		t = derefType(t)
		if t == types.Typ[types.Invalid] {
			return ""
		}
	} else if isFormatterType(t) {
		// A fmt.Formatter handles every verb.
		return ""
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
		if isStringLike(t) {
			break
		}
		// An interface value may hold a string, a Stringer, or an
		// error at runtime; it cannot be statically condemned.
		if _, isIface := t.Underlying().(*types.Interface); isIface {
			break
		}
		return "expects string or []byte"
	case 'q':
		if isStringLike(t) {
			break
		}
		if isBasic && isIntKind(basic.Kind()) {
			break // %q on an integer is a quoted character
		}
		if _, isIface := t.Underlying().(*types.Interface); isIface {
			break
		}
		return "expects string, []byte, or integer"
	case 'v':
		// %v accepts anything.
	case 't':
		if !isBasic || !isBoolKind(basic.Kind()) {
			return "expects boolean"
		}
	case 'p':
		if _, ok := t.Underlying().(*types.Pointer); !ok && t.String() != "unsafe.Pointer" {
			return "expects pointer"
		}
	case 'T':
		// Anything goes.
	case 'w':
		// %w in Errorf: the caller already rejected it elsewhere;
		// its argument type is left unchecked here so a non-error
		// argument is not newly condemned beyond what fmt itself
		// reports at runtime.
	}
	return ""
}

// derefType unwraps every pointer layer of t (including named pointer
// types, via their underlying type).
func derefType(t types.Type) types.Type {
	for {
		p, ok := t.Underlying().(*types.Pointer)
		if !ok {
			return t
		}
		t = p.Elem()
	}
}

// isStringLike reports whether t is printable with %s or %q: a string
// (or a named type with a string underlying type), a []byte (or a
// named type with that underlying type), or a type implementing
// String() string or error.
func isStringLike(t types.Type) bool {
	if implementsStringerOrError(t) {
		return true
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Kind() == types.String || u.Kind() == types.UntypedString
	case *types.Slice:
		return isByteElem(u.Elem())
	}
	return false
}

func isByteElem(t types.Type) bool {
	basic, ok := t.Underlying().(*types.Basic)
	return ok && basic.Kind() == types.Uint8
}

// implementsStringerOrError reports whether t implements fmt.Stringer
// (String() string) or error (Error() string).
func implementsStringerOrError(t types.Type) bool {
	return types.Implements(t, stringerInterface) || types.Implements(t, errorInterface)
}

var stringerInterface = singleMethodInterface("String", stringResultSignature())
var errorInterface = singleMethodInterface("Error", stringResultSignature())

func stringResultSignature() *types.Signature {
	return types.NewSignatureType(nil, nil, nil, types.NewTuple(),
		types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Typ[types.String])), false)
}

func singleMethodInterface(name string, sig *types.Signature) *types.Interface {
	fn := types.NewFunc(token.NoPos, nil, name, sig)
	iface := types.NewInterfaceType([]*types.Func{fn}, nil)
	iface.Complete()
	return iface
}

// methodSignature returns the signature of the named method in t's
// method set, or nil when t has no such method.
func methodSignature(t types.Type, name string) *types.Signature {
	ms := types.NewMethodSet(t)
	for i := 0; i < ms.Len(); i++ {
		obj := ms.At(i).Obj()
		if obj.Name() != name {
			continue
		}
		if sig, ok := obj.Type().(*types.Signature); ok {
			return sig
		}
	}
	return nil
}

func isRuneType(t types.Type) bool {
	basic, ok := t.Underlying().(*types.Basic)
	return ok && basic.Kind() == types.Int32
}

// isFormatterType reports whether t implements fmt.Formatter
// (Format(fmt.State, rune)), identified by method shape: the state
// parameter's type prints as fmt.State, which holds regardless of the
// import alias used at the definition site.
func isFormatterType(t types.Type) bool {
	sig := methodSignature(t, "Format")
	if sig == nil || sig.Params().Len() != 2 || sig.Results().Len() != 0 {
		return false
	}
	if !isRuneType(sig.Params().At(1).Type()) {
		return false
	}
	return sig.Params().At(0).Type().String() == "fmt.State"
}

// implementsScannerType reports whether t implements fmt.Scanner
// (Scan(fmt.State, rune) error).
func implementsScannerType(t types.Type) bool {
	sig := methodSignature(t, "Scan")
	if sig == nil || sig.Params().Len() != 2 || sig.Results().Len() != 1 {
		return false
	}
	if !isRuneType(sig.Params().At(1).Type()) {
		return false
	}
	if sig.Params().At(0).Type().String() != "fmt.State" {
		return false
	}
	return types.Implements(sig.Results().At(0).Type(), errorInterface)
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

func isBoolKind(k types.BasicKind) bool {
	return k == types.Bool || k == types.UntypedBool
}

func isFloatKind(k types.BasicKind) bool {
	switch k {
	case types.Float32, types.Float64, types.Complex64, types.Complex128,
		types.UntypedFloat, types.UntypedComplex:
		return true
	}
	return false
}
