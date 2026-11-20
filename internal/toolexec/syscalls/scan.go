package syscalls

import (
	"bytes"
	"encoding/json"
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

// Site is one raw system call in the standard library's source, with the
// argument roles derived from the typed call (abi-spike §2.1; ported from
// the measurement prototype test/spikes/abi/sysscan).
type Site struct {
	Pos  string // file:line
	Func string // enclosing function
	Nr   int64  // -1: the number is not a constant
	Name string // the number's SYS_ name, lower case (or the expression)
	// Roles per argument word a1.. as derived from the expression passed:
	// "val", "uintptr-param" (a caller-supplied word), "cstr",
	// "buf:<elem type>" (&p[0] of a slice or array), "ptr:<type>",
	// "byteptr-param" (a *byte parameter), "rawptr" (an unsafe.Pointer
	// parameter), "ptr:?".
	Roles []string
	// Buf: per argument, the slice expression a "buf:" role points into
	// (p of &p[0]); Len: the x of an argument uintptr(len(x)). Both "" when
	// not applicable. They tie a buffer to its length argument.
	Buf, Len []string
	// Size: per argument, for a "ptr:<type>" role whose type holds no
	// pointer, the type's size in bytes under gc on linux/amd64; -1 for
	// every other role and for a pointee with pointers.
	Size []int64
}

// amd64 is gc's layout of the tracked platform.
var amd64 = types.SizesFor("gc", "amd64")

// The analysis is defined by three packages of the tracked release: the one
// whose entry points every raw system call outside the runtime goes through,
// the pseudo-package without source, and the runtime family the facade
// replaces.
const (
	entryPkg    = "syscall"
	unsafePkg   = "unsafe"
	runtimePkg  = "runtime"
	runtimeTree = runtimePkg + "/"
	runtimeInt  = "internal/" + runtimeTree
)

// The functions through which every raw system call outside the runtime
// enters (package syscall).
var entries = map[string]bool{
	"syscall.Syscall": true, "syscall.Syscall6": true, "syscall.RawSyscall": true, "syscall.RawSyscall6": true,
	"syscall.rawSyscallNoError": true, "syscall.rawVforkSyscall": true,
	"syscall.AllThreadsSyscall": true, "syscall.AllThreadsSyscall6": true,
}

type listPkg struct {
	Dir        string
	ImportPath string
	GoFiles    []string
	Imports    []string
	ImportMap  map[string]string
}

// Scan type-checks, from the source of the toolchain `goCmd` runs
// (GOOS=linux GOARCH=amd64 CGO_ENABLED=0), every standard-library package
// that imports syscall, with its dependencies, and returns every raw system
// call site outside the runtime family, sorted by position.
func Scan(goCmd string) ([]Site, error) {
	env := append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64", "GOFLAGS=", "GOWORK=off")
	var roots []string
	{
		cmd := exec.Command(goCmd, "list", "-f", "{{.ImportPath}} {{join .Imports \" \"}}", "std")
		cmd.Env = env
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("go list std: %v", err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) == 0 || runtimeFamily(f[0]) {
				continue
			}
			for _, imp := range f[1:] {
				if imp == entryPkg || f[0] == entryPkg {
					roots = append(roots, f[0])
					break
				}
			}
		}
	}
	cmd := exec.Command(goCmd, append([]string{"list", "-deps", "-json=Dir,ImportPath,GoFiles,Imports,ImportMap"}, roots...)...)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -deps: %v: %s", err, stderr.String())
	}
	w := &world{fset: token.NewFileSet(), sizes: types.SizesFor("gc", "amd64"), checked: map[string]*types.Package{}}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listPkg
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		if p.ImportPath == unsafePkg {
			continue
		}
		if err := w.load(p); err != nil {
			return nil, err
		}
	}
	sort.Slice(w.sites, func(i, j int) bool { return w.sites[i].Pos < w.sites[j].Pos })
	return w.sites, nil
}

func runtimeFamily(path string) bool {
	return path == runtimePkg || strings.HasPrefix(path, runtimeInt) || strings.HasPrefix(path, runtimeTree)
}

type world struct {
	fset    *token.FileSet
	sizes   types.Sizes
	checked map[string]*types.Package
	sites   []Site
}

type mapImporter struct {
	w  *world
	im map[string]string
}

func (m mapImporter) Import(path string) (*types.Package, error) {
	if path == unsafePkg {
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

func (w *world) load(p listPkg) error {
	var files []*ast.File
	for _, f := range p.GoFiles {
		af, err := parser.ParseFile(w.fset, filepath.Join(p.Dir, f), nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		files = append(files, af)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	var firstErr error
	conf := types.Config{Importer: mapImporter{w, p.ImportMap}, Sizes: w.sizes, Error: func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}}
	pkg, _ := conf.Check(p.ImportPath, w.fset, files, info)
	w.checked[p.ImportPath] = pkg
	if runtimeFamily(p.ImportPath) {
		return nil // replaced by the facade: its system calls are the JVM's business
	}
	if firstErr != nil {
		// A role derived from a partially checked package could be wrong: refuse.
		return fmt.Errorf("type-checking %s: %v", p.ImportPath, firstErr)
	}
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			fn, _ := info.Defs[fd.Name].(*types.Func)
			if fn == nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					w.callSite(info, fd, fn, c)
				}
				return true
			})
		}
	}
	return nil
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
	pos := w.fset.Position(c.Pos())
	s := Site{Pos: fmt.Sprintf("%s/%s:%d", fn.Pkg().Path(), filepath.Base(pos.Filename), pos.Line), Func: fn.FullName(), Nr: -1}
	s.Name = types.ExprString(c.Args[0])
	if tv := info.Types[c.Args[0]]; tv.Value != nil {
		if v, ok := constant.Int64Val(tv.Value); ok {
			s.Nr = v
		}
	}
	s.Name = strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(s.Name, "syscall."), "_"), "SYS_"), "sys_"))
	for _, a := range c.Args[1:] {
		role, buf := w.role(info, fd, a)
		s.Roles = append(s.Roles, role)
		s.Buf = append(s.Buf, buf)
		s.Len = append(s.Len, lenOf(info, a))
		size := int64(-1)
		if t := pointee(info, a); t != nil && strings.HasPrefix(role, "ptr:") && !hasPointers(t) {
			size = amd64.Sizeof(t)
		}
		s.Size = append(s.Size, size)
	}
	w.sites = append(w.sites, s)
}

// pointee returns T when e is uintptr(unsafe.Pointer(x)) with x of type *T.
func pointee(info *types.Info, e ast.Expr) types.Type {
	c, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(c.Args) != 1 {
		return nil
	}
	pc, ok := ast.Unparen(c.Args[0]).(*ast.CallExpr)
	if !ok || len(pc.Args) != 1 || !info.Types[pc.Fun].IsType() {
		return nil
	}
	if pt, ok := info.Types[ast.Unparen(pc.Args[0])].Type.Underlying().(*types.Pointer); ok {
		return pt.Elem()
	}
	return nil
}

// hasPointers reports whether a value of type t holds a pointer word.
func hasPointers(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Kind() == types.String || u.Kind() == types.UnsafePointer
	case *types.Array:
		return u.Len() > 0 && hasPointers(u.Elem())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if hasPointers(u.Field(i).Type()) {
				return true
			}
		}
		return false
	}
	return true // pointer, slice, map, chan, func, interface
}

// lenOf returns x when e is uintptr(len(x)).
func lenOf(info *types.Info, e ast.Expr) string {
	c, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(c.Args) != 1 || !info.Types[c.Fun].IsType() {
		return ""
	}
	l, ok := ast.Unparen(c.Args[0]).(*ast.CallExpr)
	if !ok || len(l.Args) != 1 {
		return ""
	}
	if id, ok := ast.Unparen(l.Fun).(*ast.Ident); !ok || id.Name != "len" {
		return ""
	}
	if _, ok := info.Uses[ast.Unparen(l.Fun).(*ast.Ident)].(*types.Builtin); !ok {
		return ""
	}
	return types.ExprString(ast.Unparen(l.Args[0]))
}

// role classifies one argument expression of a raw system call; for a
// buffer it also returns the slice expression the pointer points into.
func (w *world) role(info *types.Info, fd *ast.FuncDecl, e ast.Expr) (string, string) {
	e = ast.Unparen(e)
	if tv := info.Types[e]; tv.Value != nil {
		return "val", ""
	}
	c, ok := e.(*ast.CallExpr)
	if !ok || len(c.Args) != 1 || !info.Types[c.Fun].IsType() {
		if b, ok := info.Types[e].Type.Underlying().(*types.Basic); ok && b.Kind() == types.Uintptr {
			if id, ok := e.(*ast.Ident); ok && isParam(info, fd, id) {
				return "uintptr-param", "" // caller-supplied word: value or opaque pointer (ioctl arg, mmap addr)
			}
		}
		return "val", ""
	}
	inner := ast.Unparen(c.Args[0])
	it := info.Types[inner].Type
	if b, ok := it.Underlying().(*types.Basic); !ok || b.Kind() != types.UnsafePointer {
		return "val", "" // uintptr(fd), uintptr(len(p)), ...
	}
	// uintptr(<unsafe.Pointer expr>)
	if id, ok := inner.(*ast.Ident); ok { // _p0, assigned from &p[0] earlier in the wrapper
		if el, sl := assignedSliceElem(info, fd, id); el != nil {
			return "buf:" + typeName(el), sl
		}
		return "rawptr", "" // unsafe.Pointer parameter (sockaddr, sockopt value): its length is a sibling argument
	}
	pc, ok := inner.(*ast.CallExpr)
	if !ok || len(pc.Args) != 1 {
		return "ptr:?", ""
	}
	x := ast.Unparen(pc.Args[0])
	if u, ok := x.(*ast.UnaryExpr); ok && u.Op == token.AND {
		if ix, ok := ast.Unparen(u.X).(*ast.IndexExpr); ok { // &v[0]
			switch ct := info.Types[ix.X].Type.Underlying().(type) {
			case *types.Slice:
				return "buf:" + typeName(ct.Elem()), types.ExprString(ast.Unparen(ix.X))
			case *types.Array:
				return "buf:" + typeName(ct.Elem()), types.ExprString(ast.Unparen(ix.X))
			}
		}
	}
	pt, ok := info.Types[x].Type.Underlying().(*types.Pointer)
	if !ok {
		return "ptr:?", ""
	}
	if b, ok := pt.Elem().Underlying().(*types.Basic); ok && b.Kind() == types.Uint8 {
		if id, ok := x.(*ast.Ident); ok && isParam(info, fd, id) {
			return "byteptr-param", "" // a caller's *byte: a buffer whose length is a sibling parameter (readlen)
		}
		return "cstr", "" // *byte from BytePtrFromString: NUL-terminated
	}
	return "ptr:" + typeName(pt.Elem()), ""
}

func typeName(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string { return p.Name() })
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

func assignedSliceElem(info *types.Info, fd *ast.FuncDecl, id *ast.Ident) (types.Type, string) {
	obj := info.Uses[id]
	var el types.Type
	var sl string
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
				sl = types.ExprString(ast.Unparen(ix.X))
			}
		}
		return true
	})
	return el, sl
}
