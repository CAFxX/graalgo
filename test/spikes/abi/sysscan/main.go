// Command sysscan inventories the raw system calls that std issues from Go
// source outside the runtime family on the host platform (CGO_ENABLED=0),
// and derives, from the typed call sites themselves, what each argument is:
// a value, a pointer to one T, a buffer of T, or a C string. It is the
// measurement behind docs/arch/abi-spike.md (P2).
//
// For each root package (default: os, net, os/exec, and the two wrapper
// packages themselves) it reports the system calls reachable from the
// functions the root package declares, through static references
// (interface dispatch is not followed; os.File and net's netFD reach
// internal/poll.FD and the syscall wrappers through concrete types).
//
// Usage: sysscan [-sites] [root ...]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type listPkg struct {
	Dir        string
	ImportPath string
	GoFiles    []string
	Imports    []string
	ImportMap  map[string]string
}

type site struct {
	fn    *types.Func // enclosing function
	pos   token.Position
	nr    int64 // -1 if not constant
	name  string
	entry string
	roles []string
}

type world struct {
	fset    *token.FileSet
	sizes   types.Sizes
	checked map[string]*types.Package
	callees map[*types.Func]map[*types.Func]bool
	sites   map[*types.Func][]*site
	pkgFns  map[string][]*types.Func
	varRefs map[string]map[*types.Func]bool // functions referenced by package-level var initializers
	nested  map[string]string               // pointee type -> description
}

// The functions through which every raw system call outside the runtime enters.
var entries = map[string]bool{
	"syscall.Syscall": true, "syscall.Syscall6": true, "syscall.RawSyscall": true, "syscall.RawSyscall6": true,
	"syscall.rawSyscallNoError": true, "syscall.rawVforkSyscall": true,
	"syscall.AllThreadsSyscall": true, "syscall.AllThreadsSyscall6": true,
}

func runtimeFamily(path string) bool {
	return path == "runtime" || strings.HasPrefix(path, "internal/runtime/") || strings.HasPrefix(path, "runtime/")
}

func goList(args ...string) []listPkg {
	cmd := exec.Command("go", append([]string{"list", "-deps", "-json=Dir,ImportPath,GoFiles,Imports,ImportMap"}, args...)...)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sysscan: go list:", err)
		os.Exit(1)
	}
	var pkgs []listPkg
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for {
		var p listPkg
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			panic(err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

type mapImporter struct {
	w  *world
	im map[string]string
}

func (m mapImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if r, ok := m.im[path]; ok {
		path = r
	}
	if p, ok := m.w.checked[path]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("package %s not loaded", path)
}

func main() {
	showSites := flag.Bool("sites", false, "list every call site")
	flag.Parse()
	roots := flag.Args()
	if len(roots) == 0 {
		roots = []string{"os", "net", "os/exec", "syscall", "internal/syscall/unix"}
	}
	w := &world{fset: token.NewFileSet(), sizes: types.SizesFor("gc", "amd64"), checked: map[string]*types.Package{},
		callees: map[*types.Func]map[*types.Func]bool{}, sites: map[*types.Func][]*site{}, pkgFns: map[string][]*types.Func{},
		varRefs: map[string]map[*types.Func]bool{}, nested: map[string]string{}}
	for _, p := range goList(roots...) { // dependency order
		if p.ImportPath != "unsafe" {
			w.load(p)
		}
	}
	type row struct {
		nr    int64
		name  string
		sites int
		reach []bool
		fork  bool // every site is in the fork/exec child path
		roles [][]string
	}
	rows := map[string]*row{}
	var all []*site
	for ri, root := range roots {
		for _, s := range w.reach(root) {
			key := fmt.Sprintf("%06d", s.nr)
			if s.nr < 0 {
				key = "var/" + s.name
			}
			r := rows[key]
			if r == nil {
				r = &row{nr: s.nr, name: s.name, reach: make([]bool, len(roots)), fork: true}
				rows[key] = r
			}
			if strings.HasSuffix(r.name, "trap") && !strings.HasSuffix(s.name, "trap") {
				r.name = s.name // prefer syscall's SYS_ name over internal/syscall/unix's fooTrap
			}
			if !r.reach[ri] {
				r.reach[ri] = true
			}
			if ri == 0 || !contains(all, s) {
				if !contains(all, s) {
					all = append(all, s)
					r.sites++
					if !strings.HasPrefix(filepath.Base(s.pos.Filename), "exec_") {
						r.fork = false
					}
					for i, role := range s.roles {
						for len(r.roles) <= i {
							r.roles = append(r.roles, nil)
						}
						if !containsStr(r.roles[i], role) {
							r.roles[i] = append(r.roles[i], role)
						}
					}
				}
			}
		}
	}
	keys := make([]string, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Printf("%-4s %-18s %5s  %-*s  %s\n", "nr", "name", "sites", 3*len(roots), "reach", "argument roles (a1..a6; alternatives joined by |)")
	counts := make([]int, len(roots))
	countsNoFork := make([]int, len(roots))
	shapes := map[string]int{}
	for _, k := range keys {
		r := rows[k]
		var reach strings.Builder
		for i := range roots {
			if r.reach[i] {
				reach.WriteString(" x ")
				if r.nr >= 0 { // a trap number held in a variable (fcntl64Syscall) aliases a numbered row
					counts[i]++
					if !r.fork {
						countsNoFork[i]++
					}
				}
			} else {
				reach.WriteString(" . ")
			}
		}
		var rs []string
		for _, alt := range r.roles {
			sort.Strings(alt)
			rs = append(rs, strings.Join(alt, "|"))
			for _, a := range alt {
				shapes[strings.SplitN(a, ":", 2)[0]]++
			}
		}
		for len(rs) > 0 && rs[len(rs)-1] == "val" {
			rs = rs[:len(rs)-1]
		}
		nr := fmt.Sprint(r.nr)
		if r.nr < 0 {
			nr = "var"
		}
		tag := ""
		if r.fork {
			tag = "  [fork/exec child only]"
		}
		fmt.Printf("%-4s %-18s %5d  %s  %s%s\n", nr, r.name, r.sites, reach.String(), strings.Join(rs, ", "), tag)
	}
	fmt.Println()
	for i, root := range roots {
		fmt.Printf("reachable from %-22s: %3d distinct system calls (%d outside the fork/exec child path)\n", root, counts[i], countsNoFork[i])
	}
	distinct := 0
	for _, r := range rows {
		if r.nr >= 0 {
			distinct++
		}
	}
	fmt.Printf("distinct system calls overall: %d; call sites: %d\n", distinct, len(all))
	fmt.Printf("argument shapes over (syscall, position, alternative): %v\n", shapes)
	fmt.Println("\npointee types (size in bytes; pointer-typed fields listed = nested pointers the table must describe):")
	var names []string
	for n := range w.nested {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Printf("  %-28s %s\n", n, w.nested[n])
	}
	if *showSites {
		fmt.Println()
		for _, s := range all {
			fmt.Printf("%s:%d %s %s(%s) %v\n", filepath.Base(s.pos.Filename), s.pos.Line, s.fn.FullName(), s.entry, s.name, s.roles)
		}
	}
}

func contains(ss []*site, s *site) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func (w *world) load(p listPkg) {
	var files []*ast.File
	for _, f := range p.GoFiles {
		af, err := parser.ParseFile(w.fset, filepath.Join(p.Dir, f), nil, parser.SkipObjectResolution)
		if err != nil {
			panic(err)
		}
		files = append(files, af)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: mapImporter{w, p.ImportMap}, Sizes: w.sizes, Error: func(error) {}}
	pkg, _ := conf.Check(p.ImportPath, w.fset, files, info)
	w.checked[p.ImportPath] = pkg
	if runtimeFamily(p.ImportPath) {
		return // replaced by the facade: its system calls are the JVM's business
	}
	w.varRefs[p.ImportPath] = map[*types.Func]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.VAR {
				// var pollCopyFileRange = poll.CopyFileRange and friends: calls through such
				// variables are attributed to every function of the declaring package.
				ast.Inspect(gd, func(n ast.Node) bool {
					if id, ok := n.(*ast.Ident); ok {
						if c, ok := info.Uses[id].(*types.Func); ok {
							w.varRefs[p.ImportPath][c.Origin()] = true
						}
					}
					return true
				})
			}
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			fn, _ := info.Defs[fd.Name].(*types.Func)
			if fn == nil {
				continue
			}
			w.pkgFns[p.ImportPath] = append(w.pkgFns[p.ImportPath], fn)
			cs := map[*types.Func]bool{}
			w.callees[fn] = cs
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.Ident:
					if c, ok := info.Uses[n].(*types.Func); ok {
						cs[c.Origin()] = true
					}
				case *ast.CallExpr:
					w.callSite(info, fd, fn, n)
				}
				return true
			})
		}
	}
}

func (w *world) callSite(info *types.Info, fd *ast.FuncDecl, fn *types.Func, c *ast.CallExpr) {
	var id *ast.Ident
	switch f := ast.Unparen(c.Fun).(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	}
	if id == nil {
		return
	}
	callee, ok := info.Uses[id].(*types.Func)
	if !ok || callee.Pkg() == nil || !entries[callee.Pkg().Path()+"."+callee.Name()] || len(c.Args) == 0 {
		return
	}
	if entries[fn.Pkg().Path()+"."+fn.Name()] {
		return // an entry point forwarding its own trap number
	}
	s := &site{fn: fn, pos: w.fset.Position(c.Pos()), nr: -1, entry: callee.Name()}
	s.name = types.ExprString(c.Args[0])
	if tv := info.Types[c.Args[0]]; tv.Value != nil {
		if v, ok := constant.Int64Val(tv.Value); ok {
			s.nr = v
		}
	}
	s.name = strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(s.name, "syscall."), "_"), "SYS_"), "sys_"))
	for _, a := range c.Args[1:] {
		s.roles = append(s.roles, w.role(info, fd, a))
	}
	w.sites[fn] = append(w.sites[fn], s)
}

// role classifies one argument expression of a raw system call.
func (w *world) role(info *types.Info, fd *ast.FuncDecl, e ast.Expr) string {
	e = ast.Unparen(e)
	if tv := info.Types[e]; tv.Value != nil {
		return "val"
	}
	c, ok := e.(*ast.CallExpr)
	if !ok || len(c.Args) != 1 || !info.Types[c.Fun].IsType() {
		if b, ok := info.Types[e].Type.Underlying().(*types.Basic); ok && b.Kind() == types.Uintptr {
			if id, ok := e.(*ast.Ident); ok && isParam(info, fd, id) {
				return "uintptr-param" // caller-supplied word: value or opaque pointer (ioctl arg, mmap addr)
			}
		}
		return "val"
	}
	inner := ast.Unparen(c.Args[0])
	it := info.Types[inner].Type
	if b, ok := it.Underlying().(*types.Basic); !ok || b.Kind() != types.UnsafePointer {
		return "val" // uintptr(fd), uintptr(len(p)), ...
	}
	// uintptr(<unsafe.Pointer expr>)
	if id, ok := inner.(*ast.Ident); ok { // _p0, assigned from &p[0] earlier in the wrapper
		if el := w.assignedSliceElem(info, fd, id); el != nil {
			return "buf:" + w.pointee(el)
		}
		return "rawptr" // unsafe.Pointer parameter (sockaddr, sockopt value): its length is a sibling argument
	}
	pc, ok := inner.(*ast.CallExpr)
	if !ok || len(pc.Args) != 1 {
		return "ptr:?"
	}
	x := ast.Unparen(pc.Args[0])
	if u, ok := x.(*ast.UnaryExpr); ok && u.Op == token.AND {
		if ix, ok := ast.Unparen(u.X).(*ast.IndexExpr); ok { // &v[0]
			switch ct := info.Types[ix.X].Type.Underlying().(type) {
			case *types.Slice:
				return "buf:" + w.pointee(ct.Elem())
			case *types.Array:
				return "buf:" + w.pointee(ct.Elem())
			}
		}
	}
	pt, ok := info.Types[x].Type.Underlying().(*types.Pointer)
	if !ok {
		return "ptr:?"
	}
	if b, ok := pt.Elem().Underlying().(*types.Basic); ok && b.Kind() == types.Uint8 {
		return "cstr" // *byte from BytePtrFromString, or a caller's NUL-terminated buffer
	}
	return "ptr:" + w.pointee(pt.Elem())
}

func isParam(info *types.Info, fd *ast.FuncDecl, id *ast.Ident) bool {
	obj := info.Uses[id]
	for _, f := range fd.Type.Params.List {
		for _, n := range f.Names {
			if info.Defs[n] == obj {
				return true
			}
		}
	}
	return false
}

func (w *world) assignedSliceElem(info *types.Info, fd *ast.FuncDecl, id *ast.Ident) types.Type {
	obj := info.Uses[id]
	var el types.Type
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		l, ok := as.Lhs[0].(*ast.Ident)
		if !ok || (info.Uses[l] != obj && info.Defs[l] != obj) {
			return true
		}
		c, ok := ast.Unparen(as.Rhs[0]).(*ast.CallExpr)
		if !ok || len(c.Args) != 1 {
			return true
		}
		u, ok := ast.Unparen(c.Args[0]).(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			return true
		}
		if ix, ok := ast.Unparen(u.X).(*ast.IndexExpr); ok {
			if s, ok := info.Types[ix.X].Type.Underlying().(*types.Slice); ok {
				el = s.Elem()
			}
		}
		return true
	})
	return el
}

// pointee names a pointee type and records its size and nested pointers.
func (w *world) pointee(t types.Type) string {
	name := types.TypeString(t, func(p *types.Package) string { return p.Name() })
	var ptrs []string
	var walk func(t types.Type, path string)
	walk = func(t types.Type, path string) {
		switch u := t.Underlying().(type) {
		case *types.Pointer:
			ptrs = append(ptrs, path+" "+types.TypeString(u, func(p *types.Package) string { return p.Name() }))
		case *types.Basic:
			if u.Kind() == types.UnsafePointer {
				ptrs = append(ptrs, path+" unsafe.Pointer")
			}
		case *types.Struct:
			for i := 0; i < u.NumFields(); i++ {
				walk(u.Field(i).Type(), path+"."+u.Field(i).Name())
			}
		case *types.Array:
			walk(u.Elem(), path+"[]")
		}
	}
	walk(t, "")
	desc := fmt.Sprintf("%4d", w.sizes.Sizeof(t))
	if len(ptrs) > 0 {
		desc += "  NESTED: " + strings.Join(ptrs, "; ")
	}
	w.nested[name] = desc
	return name
}

// reach returns the raw call sites reachable from the functions root
// declares (its whole API surface, exported or not).
func (w *world) reach(root string) []*site {
	seen := map[*types.Func]bool{}
	var work []*types.Func
	for _, fn := range w.pkgFns[root] {
		seen[fn] = true
		work = append(work, fn)
	}
	var out []*site
	for len(work) > 0 {
		fn := work[len(work)-1]
		work = work[:len(work)-1]
		out = append(out, w.sites[fn]...)
		push := func(c *types.Func) {
			if !seen[c] {
				seen[c] = true
				work = append(work, c)
			}
		}
		for c := range w.callees[fn] {
			push(c)
		}
		if fn.Pkg() != nil {
			for c := range w.varRefs[fn.Pkg().Path()] {
				push(c)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].pos.Filename != out[j].pos.Filename {
			return out[i].pos.Filename < out[j].pos.Filename
		}
		return out[i].pos.Line < out[j].pos.Line
	})
	return out
}
