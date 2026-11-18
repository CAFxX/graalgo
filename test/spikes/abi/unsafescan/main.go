// Command unsafescan inventories, from the type-checked source of the
// toolchain that builds it, every construct in a set of std packages that
// reads or writes memory through unsafe.Pointer or depends on the memory
// layout of gc's type descriptors. It is the measurement behind
// docs/arch/abi-spike.md (P1).
//
// Each occurrence is assigned exactly one pattern. Patterns are syntactic
// and type-directed; the mapping from pattern to mechanism (handle table,
// descriptor view, intrinsic) is in the document, not in this tool.
//
// Usage: unsafescan [-funcs] [pkg ...]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var defaultPkgs = []string{
	"internal/abi", "internal/reflectlite", "reflect", "internal/unsafeheader",
	"sync", "internal/sync", "sync/atomic", "strings", "bytes", "fmt", "encoding/json", "sort", "slices",
}

// Patterns, in report order.
const (
	pTypePos      = "type-position unsafe.Pointer (no operation)"
	pToUPAddr     = "unsafe.Pointer(&x) address-of"
	pToUPPtr      = "unsafe.Pointer(p) from typed pointer"
	pToUPArith    = "unsafe.Pointer(uintptr arithmetic)"
	pToUPInt      = "unsafe.Pointer(integer) forged"
	pToUPOther    = "unsafe.Pointer(x) other (nil, identity, func/map/chan word)"
	pFromDesc     = "(*T)(p): T is a type descriptor"
	pFromHeader   = "(*T)(p): T is an interface/slice/string header"
	pFromScalar   = "(*T)(p): T is scalar or array of scalars"
	pFromRef      = "(*T)(p): T is pointer/string/slice/interface/func/map/chan"
	pFromStruct   = "(*T)(p): T is another struct"
	pUintptr      = "uintptr(p) pointer to integer"
	pAdd          = "unsafe.Add / add() pointer arithmetic"
	pSliceString  = "unsafe.Slice/String/SliceData/StringData"
	pSizeof       = "unsafe.Sizeof/Offsetof/Alignof (constant)"
	pAtomicPtr    = "atomic op on unsafe.Pointer word"
	pBodyless     = "body-less function (linkname/assembly)"
	pDescField    = "field read/write on descriptor struct"
	pDescTrailing = "descriptor trailing-data access (uncommon/methods/in-out arrays/name bytes)"
)

var order = []string{pTypePos, pToUPAddr, pToUPPtr, pToUPArith, pToUPInt, pToUPOther, pFromDesc, pFromHeader,
	pFromScalar, pFromRef, pFromStruct, pUintptr, pAdd, pSliceString, pSizeof, pAtomicPtr, pBodyless, pDescField, pDescTrailing}

type listPkg struct {
	Dir        string
	ImportPath string
	GoFiles    []string
}

type scan struct {
	pkg     string
	info    *types.Info
	counts  map[string]int
	targets map[string]int    // (*T)(p) target type -> count
	fields  map[string]int    // descriptor field -> count
	funcs   map[string]string // function -> worst pattern group
	nfuncs  int
	cur     string
	handled map[ast.Node]bool
}

func main() {
	showFuncs := flag.Bool("funcs", false, "list functions per group")
	flag.Parse()
	pkgs := flag.Args()
	if len(pkgs) == 0 {
		pkgs = defaultPkgs
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "source", nil)
	total := map[string]int{}
	var scans []*scan
	for _, p := range pkgs {
		s, err := scanPkg(fset, imp, p)
		if err != nil {
			fmt.Fprintln(os.Stderr, "unsafescan:", err)
			os.Exit(1)
		}
		scans = append(scans, s)
		for k, v := range s.counts {
			total[k] += v
		}
	}
	fmt.Printf("%-74s", "pattern")
	for _, s := range scans {
		fmt.Printf(" %6s", short(s.pkg))
	}
	fmt.Printf(" %6s\n", "total")
	for _, p := range order {
		fmt.Printf("%-74s", p)
		for _, s := range scans {
			fmt.Printf(" %6d", s.counts[p])
		}
		fmt.Printf(" %6d\n", total[p])
	}
	fmt.Println()
	fmt.Println("functions (declared funcs+methods) by the strongest mechanism any of their operations needs:")
	fmt.Printf("%-22s %6s %6s %8s %6s %10s %10s\n", "package", "funcs", "none", "handle", "view", "intrinsic", "trailing")
	for _, s := range scans {
		g := map[string]int{}
		for _, v := range s.funcs {
			g[v]++
		}
		fmt.Printf("%-22s %6d %6d %8d %6d %10d %10d\n", s.pkg, s.nfuncs, s.nfuncs-len(s.funcs), g["handle"], g["view"], g["intrinsic"], g["trailing"])
	}
	for _, s := range scans {
		if len(s.targets) > 0 {
			fmt.Printf("\n%s: (*T)(unsafe.Pointer) targets: %s\n", s.pkg, hist(s.targets))
		}
		if len(s.fields) > 0 {
			fmt.Printf("%s: descriptor fields: %s\n", s.pkg, hist(s.fields))
		}
		if *showFuncs {
			var names []string
			for n, g := range s.funcs {
				names = append(names, g+" "+n)
			}
			sort.Strings(names)
			fmt.Printf("%s functions:\n  %s\n", s.pkg, strings.Join(names, "\n  "))
		}
	}
}

func short(p string) string {
	p = strings.TrimPrefix(p, "internal/")
	p = strings.TrimPrefix(p, "encoding/")
	if len(p) > 6 {
		p = p[:6]
	}
	return p
}

func hist(m map[string]int) string {
	type kv struct {
		k string
		v int
	}
	var kvs []kv
	for k, v := range m {
		kvs = append(kvs, kv{k, v})
	}
	sort.Slice(kvs, func(i, j int) bool {
		if kvs[i].v != kvs[j].v {
			return kvs[i].v > kvs[j].v
		}
		return kvs[i].k < kvs[j].k
	})
	var sb strings.Builder
	for _, e := range kvs {
		fmt.Fprintf(&sb, "%s=%d ", e.k, e.v)
	}
	return strings.TrimSpace(sb.String())
}

func scanPkg(fset *token.FileSet, imp types.Importer, path string) (*scan, error) {
	out, err := exec.Command("go", "list", "-json", path).Output()
	if err != nil {
		return nil, fmt.Errorf("go list %s: %v", path, err)
	}
	var lp listPkg
	if err := json.Unmarshal(out, &lp); err != nil {
		return nil, err
	}
	var files []*ast.File
	for _, f := range lp.GoFiles {
		af, err := parser.ParseFile(fset, filepath.Join(lp.Dir, f), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, af)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{}, Defs: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: imp, Sizes: types.SizesFor("gc", "amd64"), Error: func(error) {}}
	if _, err := conf.Check(path, fset, files, info); err != nil {
		return nil, fmt.Errorf("typecheck %s: %v", path, err)
	}
	s := &scan{pkg: path, info: info, counts: map[string]int{}, targets: map[string]int{}, fields: map[string]int{},
		funcs: map[string]string{}, handled: map[ast.Node]bool{}}
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				s.cur = ""
				ast.Inspect(d, s.visit)
				continue
			}
			s.nfuncs++
			s.cur = funcName(fd)
			if fd.Body == nil {
				s.hit(pBodyless)
			}
			ast.Inspect(fd, s.visit)
		}
	}
	return s, nil
}

func funcName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	return types.ExprString(fd.Recv.List[0].Type) + "." + fd.Name.Name
}

var rank = map[string]int{"handle": 1, "view": 2, "trailing": 3, "intrinsic": 4}

// group maps a pattern to the strongest mechanism it needs (see the
// document for the argument per pattern).
func group(p string) string {
	switch p {
	case pTypePos, pSizeof, pDescField:
		return ""
	case pFromDesc, pFromHeader:
		return "view"
	case pDescTrailing:
		return "trailing"
	case pBodyless, pToUPInt:
		return "intrinsic"
	}
	return "handle"
}

func (s *scan) hit(p string) {
	s.counts[p]++
	g := group(p)
	if s.cur == "" || g == "" {
		return
	}
	if rank[g] > rank[s.funcs[s.cur]] {
		s.funcs[s.cur] = g
	}
}

func (s *scan) isUnsafePointer(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.UnsafePointer
}

func (s *scan) isUintptr(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.Uintptr
}

// descriptor reports whether t is a struct declared in internal/abi (or
// internal/runtime/maps), or a struct whose first field is such a struct
// (reflect's rtype, ptrType, structType, ... wrap the abi types that way).
func descriptor(t types.Type) bool {
	for depth := 0; depth < 4; depth++ {
		t = types.Unalias(t)
		if n, ok := t.(*types.Named); ok && n.Obj().Pkg() != nil {
			switch n.Obj().Pkg().Path() {
			case "internal/abi", "internal/runtime/maps":
				switch n.Obj().Name() {
				case "EmptyInterface", "NonEmptyInterface", "RegArgs", "IntArgRegBitmap":
					return false
				}
				_, isStruct := n.Underlying().(*types.Struct)
				return isStruct
			}
		}
		st, ok := t.Underlying().(*types.Struct)
		if !ok || st.NumFields() == 0 {
			return false
		}
		t = st.Field(0).Type()
	}
	return false
}

func header(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	switch strings.ToLower(n.Obj().Name()) {
	case "emptyinterface", "nonemptyinterface", "slice", "string", "sliceheader", "stringheader", "eface", "iface":
		_, isStruct := n.Underlying().(*types.Struct)
		return isStruct
	}
	return false
}

func scalarish(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Kind() != types.String && u.Kind() != types.UnsafePointer
	case *types.Array:
		return scalarish(u.Elem())
	}
	return false
}

func (s *scan) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.CallExpr:
		s.call(n)
	case *ast.SelectorExpr:
		if s.handled[n] {
			return true
		}
		if sel, ok := s.info.Selections[n]; ok && sel.Kind() == types.FieldVal {
			recv := sel.Recv()
			if p, ok := recv.Underlying().(*types.Pointer); ok {
				recv = p.Elem()
			}
			if descriptor(recv) {
				s.hit(pDescField)
				s.fields[sel.Obj().Name()]++
			}
			return true
		}
		// unsafe.Pointer in type position.
		if id, ok := n.X.(*ast.Ident); ok && n.Sel.Name == "Pointer" {
			if pn, ok := s.info.Uses[id].(*types.PkgName); ok && pn.Imported().Path() == "unsafe" {
				s.hit(pTypePos)
			}
		}
	}
	return true
}

func (s *scan) call(c *ast.CallExpr) {
	fun := ast.Unparen(c.Fun)
	// Builtins and helpers.
	if sel, ok := fun.(*ast.SelectorExpr); ok {
		if id, ok := sel.X.(*ast.Ident); ok {
			if pn, ok := s.info.Uses[id].(*types.PkgName); ok {
				switch pn.Imported().Path() {
				case "unsafe":
					switch sel.Sel.Name {
					case "Add":
						s.hit(pAdd)
					case "Slice", "String", "SliceData", "StringData":
						s.hit(pSliceString)
					case "Sizeof", "Offsetof", "Alignof":
						s.hit(pSizeof)
					}
				case "sync/atomic", "internal/runtime/atomic":
					if strings.HasSuffix(sel.Sel.Name, "Pointer") {
						s.hit(pAtomicPtr)
					}
				}
			}
		}
	}
	if id, ok := fun.(*ast.Ident); ok && len(c.Args) >= 2 {
		// reflect/reflectlite's add(p, off, whySafe) and abi's addChecked.
		if fn, ok := s.info.Uses[id].(*types.Func); ok && (fn.Name() == "add" || fn.Name() == "addChecked") {
			if sig := fn.Type().(*types.Signature); sig.Results().Len() == 1 && s.isUnsafePointer(sig.Results().At(0).Type()) {
				if a0, ok := s.info.Types[c.Args[0]]; ok && descriptorPtrExpr(s, c.Args[0], a0.Type) {
					s.hit(pDescTrailing)
				} else {
					s.hit(pAdd)
				}
			}
		}
	}
	tv, ok := s.info.Types[c.Fun]
	if !ok || !tv.IsType() || len(c.Args) != 1 {
		return
	}
	// A conversion.
	at, ok := s.info.Types[c.Args[0]]
	if !ok {
		return
	}
	to, from := tv.Type, at.Type
	arg := ast.Unparen(c.Args[0])
	switch {
	case s.isUnsafePointer(to):
		// Do not count the unsafe.Pointer selector of the conversion itself as type-position.
		if sel, ok := fun.(*ast.SelectorExpr); ok {
			s.handled[sel] = true
		}
		switch {
		case at.IsNil():
			s.hit(pToUPOther)
		case isAddrOf(arg):
			s.hit(pToUPAddr)
		case s.isUintptr(from) || isInteger(from):
			if containsUintptrOfPointer(s, arg) {
				s.hit(pToUPArith)
			} else {
				s.hit(pToUPInt)
			}
		default:
			if _, isPtr := from.Underlying().(*types.Pointer); isPtr {
				s.hit(pToUPPtr)
			} else {
				s.hit(pToUPOther)
			}
		}
	case s.isUnsafePointer(from):
		if s.isUintptr(to) {
			s.hit(pUintptr)
			return
		}
		p, isPtr := to.Underlying().(*types.Pointer)
		if !isPtr {
			return // unsafe.Pointer -> named unsafe.Pointer type etc.
		}
		el := p.Elem()
		s.targets[types.TypeString(el, func(p *types.Package) string { return p.Name() })]++
		switch {
		case descriptor(el):
			s.hit(pFromDesc)
		case header(el):
			s.hit(pFromHeader)
		case scalarish(el):
			s.hit(pFromScalar)
		default:
			if _, isStruct := el.Underlying().(*types.Struct); isStruct {
				s.hit(pFromStruct)
			} else if _, isArr := el.Underlying().(*types.Array); isArr {
				s.hit(pFromStruct)
			} else {
				s.hit(pFromRef)
			}
		}
	}
}

// descriptorPtrExpr reports whether e is unsafe.Pointer(x) (possibly
// nested) with x a pointer to a descriptor struct.
func descriptorPtrExpr(s *scan, e ast.Expr, t types.Type) bool {
	e = ast.Unparen(e)
	if sel, ok := e.(*ast.SelectorExpr); ok {
		// n.Bytes of an abi.Name (reflectlite: n.bytes of its own name type): the encoded name.
		if s.info.Selections[sel] != nil && strings.EqualFold(sel.Sel.Name, "bytes") {
			return true
		}
	}
	if p, ok := t.Underlying().(*types.Pointer); ok {
		return descriptor(p.Elem())
	}
	if c, ok := e.(*ast.CallExpr); ok && len(c.Args) == 1 {
		if at, ok := s.info.Types[c.Args[0]]; ok {
			return descriptorPtrExpr(s, c.Args[0], at.Type)
		}
	}
	return false
}

func isAddrOf(e ast.Expr) bool {
	u, ok := e.(*ast.UnaryExpr)
	return ok && u.Op == token.AND
}

func isInteger(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0
}

// containsUintptrOfPointer reports whether the integer expression is
// visibly derived from a pointer (uintptr(p) + off and friends).
func containsUintptrOfPointer(s *scan, e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok || len(c.Args) != 1 {
			return true
		}
		if tv, ok := s.info.Types[c.Fun]; ok && tv.IsType() && s.isUintptr(tv.Type) {
			if at, ok := s.info.Types[c.Args[0]]; ok && s.isUnsafePointer(at.Type) {
				found = true
			}
		}
		return !found
	})
	return found
}
