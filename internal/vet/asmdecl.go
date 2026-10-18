package vet

// asmdecl reports mismatches between assembly files and Go declarations.
//
// The implementation is adapted from
// golang.org/x/tools/go/analysis/passes/asmdecl (BSD licensed) to the
// graalgo vet Pass API: assembly files are discovered in the directories
// containing the analyzed Go files, and diagnostics use token.Position
// values directly.

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// asmKind is a kind of assembly variable. The kinds 1, 2, 4, 8 stand for
// values of that size. The special kinds below are not valid sizes.
type asmKind int

const (
	asmString asmKind = 100 + iota
	asmSlice
	asmArray
	asmInterface
	asmEmptyInterface
	asmStruct
	asmComplex
)

// asmArch describes assembly parameters for an architecture.
type asmArch struct {
	name      string
	bigEndian bool
	stack     string // name of the SP register
	lr        bool   // has link register

	// retRegs lists registers holding return values under ABIInternal.
	retRegs []string
	// writeResult lists instructions that implicitly write results.
	writeResult []string

	// calculated during initialization
	sizes    types.Sizes
	intSize  int
	ptrSize  int
	maxAlign int
}

// asmFunc describes the expected variables for a function on an arch.
type asmFunc struct {
	arch        *asmArch
	size        int // size of all arguments
	vars        map[string]*asmVar
	varByOffset map[int]*asmVar
}

// asmVar describes a single assembly variable.
type asmVar struct {
	name  string
	kind  asmKind
	typ   string
	off   int
	size  int
	inner []*asmVar
}

var (
	asmArch386     = asmArch{name: "386", bigEndian: false, stack: "SP", lr: false}
	asmArchArm     = asmArch{name: "arm", bigEndian: false, stack: "R13", lr: true}
	asmArchArm64   = asmArch{name: "arm64", bigEndian: false, stack: "RSP", lr: true, retRegs: []string{"R0", "F0"}, writeResult: []string{"SVC"}}
	asmArchAmd64   = asmArch{name: "amd64", bigEndian: false, stack: "SP", lr: false, retRegs: []string{"AX", "X0"}, writeResult: []string{"SYSCALL"}}
	asmArchMips    = asmArch{name: "mips", bigEndian: true, stack: "R29", lr: true}
	asmArchMipsLE  = asmArch{name: "mipsle", bigEndian: false, stack: "R29", lr: true}
	asmArchMips64  = asmArch{name: "mips64", bigEndian: true, stack: "R29", lr: true}
	asmArchMips64L = asmArch{name: "mips64le", bigEndian: false, stack: "R29", lr: true}
	asmArchPpc64   = asmArch{name: "ppc64", bigEndian: true, stack: "R1", lr: true, retRegs: []string{"R3", "F1"}, writeResult: []string{"SYSCALL"}}
	asmArchPpc64LE = asmArch{name: "ppc64le", bigEndian: false, stack: "R1", lr: true, retRegs: []string{"R3", "F1"}, writeResult: []string{"SYSCALL"}}
	asmArchRISCV64 = asmArch{name: "riscv64", bigEndian: false, stack: "SP", lr: true, retRegs: []string{"X10", "F10"}, writeResult: []string{"ECALL"}}
	asmArchS390X   = asmArch{name: "s390x", bigEndian: true, stack: "R15", lr: true}
	asmArchWasm    = asmArch{name: "wasm", bigEndian: false, stack: "SP", lr: false}
	asmArchLoong64 = asmArch{name: "loong64", bigEndian: false, stack: "R3", lr: true, retRegs: []string{"R4", "F0"}, writeResult: []string{"SYSCALL"}}

	asmArches = []*asmArch{
		&asmArch386, &asmArchArm, &asmArchArm64, &asmArchAmd64,
		&asmArchMips, &asmArchMipsLE, &asmArchMips64, &asmArchMips64L,
		&asmArchPpc64, &asmArchPpc64LE, &asmArchRISCV64, &asmArchS390X,
		&asmArchWasm, &asmArchLoong64,
	}
)

func init() {
	register(&Analyzer{
		Name: "asmdecl",
		Doc:  "report mismatches between assembly files and Go declarations",
		Run:  runAsmDecl,
	})
	for _, arch := range asmArches {
		arch.sizes = types.SizesFor("gc", arch.name)
		if arch.sizes == nil {
			arch.sizes = types.SizesFor("gc", "amd64")
		}
		arch.intSize = int(arch.sizes.Sizeof(types.Typ[types.Int]))
		arch.ptrSize = int(arch.sizes.Sizeof(types.Typ[types.UnsafePointer]))
		arch.maxAlign = int(arch.sizes.Alignof(types.Typ[types.Int64]))
	}
}

var (
	asmRePlusBuild = regexp.MustCompile(`//\s+\+build\s+([^\n]+)`)
	asmReGoBuild   = regexp.MustCompile(`//go:build\s+([^\n]+)`)
	asmReTEXT      = regexp.MustCompile(`\bTEXT\b(.*)·([^\(]+)\(SB\)(?:\s*,\s*([0-9A-Z|+()]+))?(?:\s*,\s*\$(-?[0-9]+)(?:-([0-9]+))?)?`)
	asmReDATA      = regexp.MustCompile(`\b(DATA|GLOBL)\b`)
	asmReNamedFP   = regexp.MustCompile(`\$?([a-zA-Z0-9_\xFF-\x{10FFFF}]+)(?:\+([0-9]+))\(FP\)`)
	asmReUnnamedFP = regexp.MustCompile(`[^\+\-0-9]([0-9]+)\(FP\)`)
	asmReSP        = regexp.MustCompile(`[^+\-0-9](([0-9]+)\(([A-Z0-9]+)\))`)
	asmReOpcode    = regexp.MustCompile(`^\s*(?:[A-Z0-9a-z_]+:)?\s*([A-Z]+)\s*([^,]*)(?:,\s*(.*))?`)
	asmRePpc64Suff = regexp.MustCompile(`([BHWD])(ZU|Z|U|BR)?$`)
	asmReABISuff   = regexp.MustCompile(`^(.+)<(ABI.+)>$`)
)

// runAsmDecl implements the asmdecl analyzer.
func runAsmDecl(pass *Pass) []Diagnostic {
	sfiles := asmFilesForPass(pass)
	if sfiles == nil {
		return nil
	}
	var out []Diagnostic

	// Gather declarations. knownFunc[name][arch] is the func description.
	// skippedAsm names decls with no measurable assembly layout (generics):
	// the declaration exists, so a matching TEXT is not "missing".
	knownFunc := make(map[string]map[string]*asmFunc)
	skippedAsm := make(map[string]bool)
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			if decl, ok := decl.(*ast.FuncDecl); ok && decl.Body == nil {
				m, skipped := asmParseDecl(pass, decl)
				knownFunc[decl.Name.Name] = m
				if skipped {
					skippedAsm[decl.Name.Name] = true
				}
			}
		}
	}
	pkgName := asmPkgName(pass)

Files:
	for _, fname := range sfiles {
		content, err := os.ReadFile(fname)
		if err != nil {
			continue
		}
		// Determine architecture from file name if possible.
		var arch string
		var archDef *asmArch
		for _, a := range asmArches {
			if strings.HasSuffix(fname, "_"+a.name+".s") {
				arch, archDef = a.name, a
				break
			}
		}
		lines := strings.SplitAfter(string(content), "\n")
		var fn *asmFunc
		var fnName, abi string
		var localSize, argSize int
		var wroteSP, noframe, haveRetArg bool
		var retLine []int

		flushRet := func() {
			if fn != nil && fn.vars["ret"] != nil && !haveRetArg && len(retLine) > 0 {
				v := fn.vars["ret"]
				resultStr := fmt.Sprintf("%d-byte ret+%d(FP)", v.size, v.off)
				if abi == "ABIInternal" {
					resultStr = "result register"
				}
				for _, line := range retLine {
					out = append(out, Diagnostic{
						token.Position{Filename: fname, Line: line},
						fmt.Sprintf("[%s] %s: RET without writing to %s", arch, fnName, resultStr),
					})
				}
			}
			retLine = nil
		}
		trimABI := func(fnName string) (string, string) {
			if m := asmReABISuff.FindStringSubmatch(fnName); m != nil {
				return m[1], m[2]
			}
			return fnName, ""
		}
		for lineno, line := range lines {
			lineno++
			pos := token.Position{Filename: fname, Line: lineno}
			badf := func(format string, args ...any) {
				out = append(out, Diagnostic{
					pos,
					fmt.Sprintf("[%s] %s: %s", arch, fnName, fmt.Sprintf(format, args...)),
				})
			}
			if arch == "" {
				// Determine architecture from build lines if possible.
				var buildLine string
				if m := asmRePlusBuild.FindStringSubmatch(line); m != nil {
					buildLine = m[1]
				} else if m := asmReGoBuild.FindStringSubmatch(line); m != nil {
					buildLine = m[1]
				}
				if buildLine != "" {
					var archCandidates []*asmArch
					for _, fld := range strings.Fields(buildLine) {
						fld = strings.Trim(fld, "()!")
						for _, a := range asmArches {
							if a.name == fld {
								archCandidates = append(archCandidates, a)
							}
						}
					}
					for _, a := range archCandidates {
						if a.name == runtime.GOARCH {
							archCandidates = []*asmArch{a}
							break
						}
					}
					if len(archCandidates) > 0 {
						arch, archDef = archCandidates[0].name, archCandidates[0]
					}
				}
			}
			// Ignore comments and commented-out code.
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			if m := asmReTEXT.FindStringSubmatch(line); m != nil {
				flushRet()
				if arch == "" {
					// Fall back to the build's GOARCH.
					for _, a := range asmArches {
						if a.name == runtime.GOARCH {
							arch, archDef = a.name, a
							break
						}
					}
					if arch == "" {
						continue Files
					}
				}
				fnName = m[2]
				if pkgPath := strings.TrimSpace(m[1]); pkgPath != "" {
					// The assembler uses Unicode division slash within
					// identifiers to represent the directory separator.
					pkgPath = strings.Replace(pkgPath, "∕", "/", -1)
					if pkgPath != pkgName && !strings.HasSuffix(pkgPath, "/"+pkgName) {
						fn, fnName, abi = nil, "", ""
						continue
					}
				}
				// Trim off optional ABI selector.
				fnName, abi = trimABI(fnName)
				flag := m[3]
				fn = knownFunc[fnName][arch]
				if fn != nil {
					size, _ := strconv.Atoi(m[5])
					if size != fn.size && (flag != "7" && !strings.Contains(flag, "NOSPLIT") || size != 0) {
						badf("wrong argument size %d; expected $...-%d", size, fn.size)
					}
				}
				localSize, _ = strconv.Atoi(m[4])
				localSize += archDef.intSize
				if archDef.lr && !strings.Contains(flag, "NOFRAME") {
					// Account for caller's saved LR
					localSize += archDef.intSize
				}
				argSize, _ = strconv.Atoi(m[5])
				noframe = strings.Contains(flag, "NOFRAME")
				if fn == nil && !skippedAsm[fnName] && !strings.Contains(fnName, "<>") && !noframe {
					badf("function %s missing Go declaration", fnName)
				}
				wroteSP = false
				haveRetArg = false
				continue
			} else if strings.Contains(line, "TEXT") && strings.Contains(line, "SB") {
				// function, but not visible from Go (didn't match asmReTEXT), so stop checking
				flushRet()
				fn, fnName, abi = nil, "", ""
				continue
			}
			if strings.Contains(line, "RET") && !strings.Contains(line, "(SB)") {
				// RET f(SB) is a tail call. It is okay to not write the results.
				retLine = append(retLine, lineno)
			}
			if fnName == "" {
				continue
			}
			if asmReDATA.FindStringSubmatch(line) != nil {
				fn = nil
			}
			if archDef == nil {
				continue
			}
			if strings.Contains(line, ", "+archDef.stack) || strings.Contains(line, ",\t"+archDef.stack) ||
				strings.Contains(line, "NOP "+archDef.stack) || strings.Contains(line, "NOP\t"+archDef.stack) {
				wroteSP = true
				continue
			}
			if arch == "wasm" && strings.Contains(line, "CallImport") {
				// CallImport is a call out to magic that can write the result.
				haveRetArg = true
			}
			if abi == "ABIInternal" && !haveRetArg {
				for _, ins := range archDef.writeResult {
					if strings.Contains(line, ins) {
						haveRetArg = true
						break
					}
				}
				for _, reg := range archDef.retRegs {
					if strings.Contains(line, reg) {
						haveRetArg = true
						break
					}
				}
			}
			for _, m := range asmReSP.FindAllStringSubmatch(line, -1) {
				if m[3] != archDef.stack || wroteSP || noframe {
					continue
				}
				off := 0
				if m[1] != "" {
					off, _ = strconv.Atoi(m[2])
				}
				if off >= localSize {
					if fn != nil {
						v := fn.varByOffset[off-localSize]
						if v != nil {
							badf("%s should be %s+%d(FP)", m[1], v.name, off-localSize)
							continue
						}
					}
					if off >= localSize+argSize {
						badf("use of %s points beyond argument frame", m[1])
						continue
					}
					badf("use of %s to access argument frame", m[1])
				}
			}
			if fn == nil {
				continue
			}
			for _, m := range asmReUnnamedFP.FindAllStringSubmatch(line, -1) {
				off, _ := strconv.Atoi(m[2])
				v := fn.varByOffset[off]
				if v != nil {
					badf("use of unnamed argument %s; offset %d is %s+%d(FP)", m[1], off, v.name, v.off)
				} else {
					badf("use of unnamed argument %s", m[1])
				}
			}
			for _, m := range asmReNamedFP.FindAllStringSubmatch(line, -1) {
				name := m[1]
				off := 0
				if m[2] != "" {
					off, _ = strconv.Atoi(m[2])
				}
				if name == "ret" || strings.HasPrefix(name, "ret_") {
					haveRetArg = true
				}
				v := fn.vars[name]
				if v == nil {
					// Allow argframe+0(FP).
					if name == "argframe" && off == 0 {
						continue
					}
					v = fn.varByOffset[off]
					if v != nil {
						badf("unknown variable %s; offset %d is %s+%d(FP)", name, off, v.name, v.off)
					} else {
						badf("unknown variable %s", name)
					}
					continue
				}
				asmCheckVar(badf, fn, line, m[0], off, v, archDef)
			}
		}
		flushRet()
	}
	return out
}

func asmKindForType(t types.Type, size int) asmKind {
	switch t := t.Underlying().(type) {
	case *types.Basic:
		switch t.Kind() {
		case types.String:
			return asmString
		case types.Complex64, types.Complex128:
			return asmComplex
		}
		return asmKind(size)
	case *types.Pointer, *types.Chan, *types.Map, *types.Signature:
		return asmKind(size)
	case *types.Struct:
		return asmStruct
	case *types.Interface:
		if t.Empty() {
			return asmEmptyInterface
		}
		return asmInterface
	case *types.Array:
		return asmArray
	case *types.Slice:
		return asmSlice
	case *types.Union:
		// A union constraint has no single assembly representation;
		// treat it as an opaque blob of its declared size rather than
		// crashing the tool. (A *types.TypeParam never reaches here:
		// Underlying normalizes it to its constraint interface, handled
		// above.)
		return asmKind(size)
	}
	panic("unreachable")
}

// asmHasLayout reports whether t has a concrete, measurable assembly layout.
// Type parameters and union constraints from generic code have neither a
// defined size (go/types panics measuring them) nor a single assembly
// representation, so assembly validation skips anything whose value layout
// contains them.
func asmHasLayout(t types.Type) bool {
	return asmHasLayoutSeen(t, make(map[types.Type]bool))
}

func asmHasLayoutSeen(t types.Type, seen map[types.Type]bool) bool {
	if seen[t] {
		return true
	}
	seen[t] = true
	switch t := t.(type) {
	case *types.TypeParam, *types.Union:
		return false
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan,
		*types.Signature, *types.Interface:
		// Reference-like or fixed-size: measuring never touches the
		// element type.
		return true
	case *types.Array:
		return asmHasLayoutSeen(t.Elem(), seen)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if !asmHasLayoutSeen(t.Field(i).Type(), seen) {
				return false
			}
		}
		return true
	case *types.Named:
		return asmHasLayoutSeen(t.Underlying(), seen)
	default:
		// Basic and any other core types are always measurable.
		return true
	}
}

// component is an assembly-addressable component of a composite type,
// or a composite type itself.
type component struct {
	size   int
	offset int
	kind   asmKind
	typ    string
	suffix string // e.g. _base for string base
	outer  string // suffix of the immediately containing composite type
}

func componentsOfType(arch *asmArch, t types.Type) []component {
	// Generic code can surface *types.TypeParam and *types.Union, which
	// have neither a defined size nor a single assembly representation
	// (go/types panics measuring them). Skip them instead of crashing.
	if !asmHasLayout(t) {
		return nil
	}
	return appendComponentsRecursive(arch, t, nil, "", 0)
}

func appendComponentsRecursive(arch *asmArch, t types.Type, cc []component, suffix string, off int) []component {
	s := t.String()
	size := int(arch.sizes.Sizeof(t))
	kind := asmKindForType(t, size)
	cc = append(cc, component{suffix: suffix, kind: kind, typ: s, offset: off, size: size, outer: suffix})
	switch kind {
	case 8:
		if arch.ptrSize == 4 {
			w1, w2 := "lo", "hi"
			if arch.bigEndian {
				w1, w2 = w2, w1
			}
			cc = append(cc, component{suffix: suffix + "_" + w1, kind: 4, typ: "half " + s, offset: off, size: 4, outer: suffix})
			cc = append(cc, component{suffix: suffix + "_" + w2, kind: 4, typ: "half " + s, offset: off + 4, size: 4, outer: suffix})
		}
	case asmEmptyInterface:
		cc = append(cc, component{suffix: suffix + "_type", kind: asmKind(arch.ptrSize), typ: "interface type", offset: off, size: arch.ptrSize, outer: suffix})
		cc = append(cc, component{suffix: suffix + "_data", kind: asmKind(arch.ptrSize), typ: "interface data", offset: off + arch.ptrSize, size: arch.ptrSize, outer: suffix})
	case asmInterface:
		cc = append(cc, component{suffix: suffix + "_itable", kind: asmKind(arch.ptrSize), typ: "interface itable", offset: off, size: arch.ptrSize, outer: suffix})
		cc = append(cc, component{suffix: suffix + "_data", kind: asmKind(arch.ptrSize), typ: "interface data", offset: off + arch.ptrSize, size: arch.ptrSize, outer: suffix})
	case asmSlice:
		cc = append(cc, component{suffix: suffix + "_base", kind: asmKind(arch.ptrSize), typ: "slice base", offset: off, size: arch.ptrSize, outer: suffix})
		cc = append(cc, component{suffix: suffix + "_len", kind: asmKind(arch.intSize), typ: "slice len", offset: off + arch.ptrSize, size: arch.intSize, outer: suffix})
		cc = append(cc, component{suffix: suffix + "_cap", kind: asmKind(arch.intSize), typ: "slice cap", offset: off + arch.ptrSize + arch.intSize, size: arch.intSize, outer: suffix})
	case asmString:
		cc = append(cc, component{suffix: suffix + "_base", kind: asmKind(arch.ptrSize), typ: "string base", offset: off, size: arch.ptrSize, outer: suffix})
		cc = append(cc, component{suffix: suffix + "_len", kind: asmKind(arch.intSize), typ: "string len", offset: off + arch.ptrSize, size: arch.intSize, outer: suffix})
	case asmComplex:
		fsize := size / 2
		cc = append(cc, component{suffix: suffix + "_real", kind: asmKind(fsize), typ: fmt.Sprintf("real(complex%d)", size*8), offset: off, size: fsize, outer: suffix})
		cc = append(cc, component{suffix: suffix + "_imag", kind: asmKind(fsize), typ: fmt.Sprintf("imag(complex%d)", size*8), offset: off + fsize, size: fsize, outer: suffix})
	case asmStruct:
		tu := t.Underlying().(*types.Struct)
		fields := make([]*types.Var, tu.NumFields())
		for i := 0; i < tu.NumFields(); i++ {
			fields[i] = tu.Field(i)
		}
		offsets := arch.sizes.Offsetsof(fields)
		for i, f := range fields {
			cc = appendComponentsRecursive(arch, f.Type(), cc, suffix+"_"+f.Name(), off+int(offsets[i]))
		}
	case asmArray:
		tu := t.Underlying().(*types.Array)
		elem := tu.Elem()
		fields := []*types.Var{
			types.NewField(token.NoPos, nil, "fake0", elem, false),
			types.NewField(token.NoPos, nil, "fake1", elem, false),
		}
		offsets := arch.sizes.Offsetsof(fields)
		elemoff := int(offsets[1])
		for i := 0; i < int(tu.Len()); i++ {
			cc = appendComponentsRecursive(arch, elem, cc, suffix+"_"+strconv.Itoa(i), off+i*elemoff)
		}
	}
	return cc
}

// asmFieldType resolves the type of a parameter/result field, or nil if
// type information is unavailable.
func asmFieldType(pass *Pass, fld *ast.Field) types.Type {
	t := pass.Info.Types[fld.Type].Type
	if t == nil {
		if ell, ok := fld.Type.(*ast.Ellipsis); ok {
			t = types.NewSlice(pass.Info.Types[ell.Elt].Type)
		}
	}
	return t
}

// asmParseDecl parses a function decl for expected assembly variables.
// It reports whether the decl was skipped: a generic function (type
// parameters or union constraints in its signature) has no single assembly
// layout, so there is nothing to validate.
func asmParseDecl(pass *Pass, decl *ast.FuncDecl) (map[string]*asmFunc, bool) {
	var arch *asmArch
	var fn *asmFunc
	var offset int

	// A generic function (type parameters or union constraints in its
	// signature) has no single assembly layout: sizes are instantiation-
	// specific, so the declared offsets are meaningless — and go/types
	// panics measuring type parameters. Skip validation for it rather
	// than crashing the tool.
	var fieldLists [][]*ast.Field
	if decl.Type.Params != nil {
		fieldLists = append(fieldLists, decl.Type.Params.List)
	}
	if decl.Type.Results != nil {
		fieldLists = append(fieldLists, decl.Type.Results.List)
	}
	for _, list := range fieldLists {
		for _, fld := range list {
			if t := asmFieldType(pass, fld); t != nil && !asmHasLayout(t) {
				return make(map[string]*asmFunc), true
			}
		}
	}

	// addParams adds asmVars for each of the parameters in list.
	// isret indicates whether the list holds results rather than params.
	addParams := func(list []*ast.Field, isret bool) {
		argnum := 0
		for _, fld := range list {
			t := asmFieldType(pass, fld)
			if t == nil {
				continue
			}
			align := int(arch.sizes.Alignof(t))
			size := int(arch.sizes.Sizeof(t))
			offset += -offset & (align - 1)
			cc := componentsOfType(arch, t)
			names := fld.Names
			if len(names) == 0 {
				// Anonymous args are called arg, arg1, arg2, ...
				// Similarly for return values: ret, ret1, ret2, ...
				name := "arg"
				if isret {
					name = "ret"
				}
				if argnum > 0 {
					name += strconv.Itoa(argnum)
				}
				names = []*ast.Ident{ast.NewIdent(name)}
			}
			argnum += len(names)
			for _, id := range names {
				name := id.Name
				for _, c := range cc {
					outer := name + c.outer
					v := asmVar{
						name: name + c.suffix,
						kind: c.kind,
						typ:  c.typ,
						off:  offset + c.offset,
						size: c.size,
					}
					if vo := fn.vars[outer]; vo != nil {
						vo.inner = append(vo.inner, &v)
					}
					fn.vars[v.name] = &v
					for i := 0; i < v.size; i++ {
						fn.varByOffset[v.off+i] = &v
					}
				}
				offset += size
			}
		}
	}
	m := make(map[string]*asmFunc)
	for _, arch = range asmArches {
		fn = &asmFunc{
			arch:        arch,
			vars:        make(map[string]*asmVar),
			varByOffset: make(map[int]*asmVar),
		}
		offset = 0
		if decl.Type.Params != nil {
			addParams(decl.Type.Params.List, false)
		}
		if decl.Type.Results != nil && len(decl.Type.Results.List) > 0 {
			offset += -offset & (arch.maxAlign - 1)
			addParams(decl.Type.Results.List, true)
		}
		fn.size = offset
		m[arch.name] = fn
	}
	return m, false
}

// asmCheckVar checks a single variable reference against its declaration.
func asmCheckVar(badf func(string, ...any), fn *asmFunc, line, expr string, off int, v *asmVar, archDef *asmArch) {
	m := asmReOpcode.FindStringSubmatch(line)
	if m == nil {
		if !strings.HasPrefix(strings.TrimSpace(line), "//") {
			badf("cannot find assembly opcode")
		}
		return
	}
	addr := strings.HasPrefix(expr, "$")
	// Determine operand sizes from instruction.
	var src, dst, kind asmKind
	op := m[1]
	switch fn.arch.name + "." + op {
	case "386.FMOVLP":
		src, dst = 8, 4
	case "arm.MOVD":
		src = 8
	case "arm.MOVW":
		src = 4
	case "arm.MOVH", "arm.MOVHU":
		src = 2
	case "arm.MOVB", "arm.MOVBU":
		src = 1
	case "386.LEAL":
		dst = 4
		addr = true
	case "amd64.LEAQ":
		dst = 8
		addr = true
	default:
		switch fn.arch.name {
		case "386", "amd64":
			if strings.HasPrefix(op, "F") && (strings.HasSuffix(op, "D") || strings.HasSuffix(op, "DP")) {
				src = 8 // FMOVDP, FXCHD, etc
				break
			}
			if strings.HasPrefix(op, "P") && strings.HasSuffix(op, "RD") {
				src = 4 // PINSRD, PEXTRD, etc
				break
			}
			if strings.HasPrefix(op, "F") && (strings.HasSuffix(op, "F") || strings.HasSuffix(op, "FP")) {
				src = 4 // FMOVFP, FXCHF, etc
				break
			}
			if strings.HasSuffix(op, "SD") {
				src = 8 // MOVSD, SQRTSD, etc
				break
			}
			if strings.HasSuffix(op, "SS") {
				src = 4 // MOVSS, SQRTSS, etc
				break
			}
			if op == "MOVO" || op == "MOVOU" {
				src = 16
				break
			}
			if strings.HasPrefix(op, "SET") {
				src = 1 // SETEQ, etc
				break
			}
			switch op[len(op)-1] {
			case 'B':
				src = 1
			case 'W':
				src = 2
			case 'L':
				src = 4
			case 'D', 'Q':
				src = 8
			}
		case "ppc64", "ppc64le":
			if m := asmRePpc64Suff.FindStringSubmatch(op); m != nil {
				switch m[1][0] {
				case 'B':
					src = 1
				case 'H':
					src = 2
				case 'W':
					src = 4
				case 'D':
					src = 8
				}
			}
		case "loong64", "mips", "mipsle", "mips64", "mips64le":
			switch op {
			case "MOVB", "MOVBU":
				src = 1
			case "MOVH", "MOVHU":
				src = 2
			case "MOVW", "MOVWU", "MOVF":
				src = 4
			case "MOVV", "MOVD":
				src = 8
			}
		case "s390x":
			switch op {
			case "MOVB", "MOVBZ":
				src = 1
			case "MOVH", "MOVHZ":
				src = 2
			case "MOVW", "MOVWZ", "FMOVS":
				src = 4
			case "MOVD", "FMOVD":
				src = 8
			}
		}
	}
	if dst == 0 {
		dst = src
	}
	// Determine whether the match is the first or second argument.
	if strings.Index(line, expr) > strings.Index(line, ",") {
		kind = dst
	} else {
		kind = src
	}
	vk, vs, vt := v.kind, v.size, v.typ
	switch vk {
	case asmInterface, asmEmptyInterface, asmString, asmSlice:
		// allow reference to first word (pointer)
		vk = v.inner[0].kind
		vs = v.inner[0].size
		vt = v.inner[0].typ
	case asmComplex:
		// Allow a single instruction to load both parts of a complex.
		if int(kind) == vs {
			kind = asmComplex
		}
	}
	if addr {
		vk, vs, vt = asmKind(archDef.ptrSize), archDef.ptrSize, "address"
	}
	if off != v.off {
		var inner bytes.Buffer
		for i, vi := range v.inner {
			if len(v.inner) > 1 {
				fmt.Fprintf(&inner, ",")
			}
			fmt.Fprintf(&inner, " ")
			if i == len(v.inner)-1 {
				fmt.Fprintf(&inner, "or ")
			}
			fmt.Fprintf(&inner, "%s+%d(FP)", vi.name, vi.off)
		}
		badf("invalid offset %s; expected %s+%d(FP)%s", expr, v.name, v.off, inner.String())
		return
	}
	if kind != 0 && kind != vk {
		var inner bytes.Buffer
		if len(v.inner) > 0 {
			fmt.Fprintf(&inner, " containing")
			for i, vi := range v.inner {
				if i > 0 && len(v.inner) > 2 {
					fmt.Fprintf(&inner, ",")
				}
				fmt.Fprintf(&inner, " ")
				if i > 0 && i == len(v.inner)-1 {
					fmt.Fprintf(&inner, "and ")
				}
				fmt.Fprintf(&inner, "%s+%d(FP)", vi.name, vi.off)
			}
		}
		badf("invalid %s of %s; %s is %d-byte value%s", op, expr, vt, vs, inner.String())
	}
}
