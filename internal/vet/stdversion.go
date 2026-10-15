package vet

import (
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

func init() {
	register(&Analyzer{
		Name: "stdversion",
		Doc:  "report uses of too-new standard library symbols",
		Run:  runStdVersion,
	})
}

// stdVersionDB maps a standard library symbol key to the Go version that
// introduced it, e.g. "go1.21". Keys look like:
//
//	"bytes.ContainsFunc"                       funcs, types, vars, consts
//	"(*bytes.Buffer).Available"                methods (pointer receiver)
//	"(bytes.Buffer).Len"                       methods (value receiver)
//	"crypto/tls.ConnectionState.TLSUnique"     struct fields
var (
	stdVersionOnce sync.Once
	stdVersionDB   map[string]string
	stdVersionErr  error
)

// stdAPIVersion parses "go1.21" into (1,21). "go1" becomes (1,0).
func stdAPIVersion(v string) (int, int) {
	v = strings.TrimPrefix(v, "go")
	if i := strings.Index(v, "."); i >= 0 {
		major, _ := strconv.Atoi(v[:i])
		minor, _ := strconv.Atoi(v[i+1:])
		return major, minor
	}
	major, _ := strconv.Atoi(v)
	return major, 0
}

// stdVersionAtLeast reports whether version v >= min.
func stdVersionAtLeast(v, min string) bool {
	maj, mino := stdAPIVersion(v)
	mmaj, mmino := stdAPIVersion(min)
	return maj > mmaj || (maj == mmaj && mino >= mmino)
}

// loadStdVersionDB parses $GOROOT/api/go1*.txt in version order to build the
// symbol introduction database. The first file listing a symbol wins, since
// each file is the delta for that release.
func loadStdVersionDB() {
	stdVersionOnce.Do(func() {
		db := make(map[string]string)
		apiDir := filepath.Join(runtime.GOROOT(), "api")
		entries, err := os.ReadDir(apiDir)
		if err != nil {
			stdVersionErr = err
			return
		}
		type vf struct {
			name string
			maj  int
			min  int
		}
		var files []vf
		for _, e := range entries {
			n := e.Name()
			if !strings.HasPrefix(n, "go1") || !strings.HasSuffix(n, ".txt") {
				continue
			}
			ver := strings.TrimSuffix(n, ".txt")
			maj, min := stdAPIVersion(ver)
			files = append(files, vf{n, maj, min})
		}
		sort.Slice(files, func(i, j int) bool {
			if files[i].maj != files[j].maj {
				return files[i].maj < files[j].maj
			}
			return files[i].min < files[j].min
		})
		// except.txt lists symbols whose history is unusual; treat them as
		// unknown so we never report a false positive for them.
		ignored := make(map[string]bool)
		if data, err := os.ReadFile(filepath.Join(apiDir, "except.txt")); err == nil {
			for _, key := range parseAPIKeys(string(data)) {
				ignored[key] = true
			}
		}
		for _, f := range files {
			data, err := os.ReadFile(filepath.Join(apiDir, f.name))
			if err != nil {
				continue
			}
			ver := strings.TrimSuffix(f.name, ".txt")
			for _, key := range parseAPIKeys(string(data)) {
				if _, exists := db[key]; !exists && !ignored[key] {
					db[key] = ver
				}
			}
		}
		stdVersionDB = db
	})
}

// parseAPIKeys extracts symbol keys from api file content.
func parseAPIKeys(content string) []string {
	var keys []string
	for _, line := range strings.Split(content, "\n") {
		// Strip issue references (" #12345") and "//deprecated" notes.
		if i := strings.Index(line, " #"); i >= 0 {
			line = line[:i]
		}
		if i := strings.Index(line, " //"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "pkg ") {
			continue
		}
		rest := strings.TrimPrefix(line, "pkg ")
		i := strings.Index(rest, ", ")
		if i < 0 {
			continue
		}
		pkg := rest[:i]
		decl := strings.TrimSpace(rest[i+2:])
		switch {
		case strings.HasPrefix(decl, "func "):
			name := decl[len("func "):]
			if i := strings.IndexAny(name, "[("); i >= 0 {
				name = name[:i]
			}
			if name = strings.TrimSpace(name); name != "" {
				keys = append(keys, pkg+"."+name)
			}
		case strings.HasPrefix(decl, "method "):
			m := decl[len("method "):]
			// m looks like "(*Buffer) Available() ..." or "(T) M(...)"
			if !strings.HasPrefix(m, "(") {
				continue
			}
			j := strings.Index(m, ")")
			if j < 0 {
				continue
			}
			recv := m[1:j] // "*Buffer" or "Buffer"
			if k := strings.Index(recv, "["); k >= 0 {
				recv = recv[:k] // strip generic args
			}
			name := strings.TrimSpace(m[j+1:])
			if i := strings.IndexAny(name, "[("); i >= 0 {
				name = name[:i]
			}
			name = strings.TrimSpace(name)
			if name == "" || recv == "" {
				continue
			}
			if strings.HasPrefix(recv, "*") {
				keys = append(keys, "(*"+pkg+"."+recv[1:]+")."+name)
			} else {
				keys = append(keys, "("+pkg+"."+recv+")."+name)
			}
		case strings.HasPrefix(decl, "type "):
			t := decl[len("type "):]
			name, sub := t, ""
			if i := strings.Index(t, ", "); i >= 0 {
				name, sub = t[:i], strings.TrimSpace(t[i+2:])
			}
			if i := strings.IndexAny(name, "[ "); i >= 0 {
				name = name[:i]
			}
			if name = strings.TrimSpace(name); name == "" {
				continue
			}
			keys = append(keys, pkg+"."+name)
			if sub != "" {
				if strings.Contains(t, "interface") {
					keys = append(keys, "("+pkg+"."+name+")."+sub)
				} else {
					keys = append(keys, pkg+"."+name+"."+sub)
				}
			}
		case strings.HasPrefix(decl, "var ") || strings.HasPrefix(decl, "const "):
			name := decl[4:]
			if i := strings.IndexAny(name, " =("); i >= 0 {
				name = name[:i]
			}
			if name = strings.TrimSpace(name); name != "" {
				keys = append(keys, pkg+"."+name)
			}
		}
	}
	return keys
}

// stdGoModCache caches directory -> go directive version ("go1.21" or "").
var stdGoModCache sync.Map

// stdGoModVersion finds the go.mod enclosing dir and returns its "go"
// directive normalized as "go1.21", or "" if none is found.
func stdGoModVersion(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if v, ok := stdGoModCache.Load(abs); ok {
		return v.(string)
	}
	ver := ""
	d := abs
	for {
		if data, err := os.ReadFile(filepath.Join(d, "go.mod")); err == nil {
			ver = parseGoDirective(string(data))
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	stdGoModCache.Store(abs, ver)
	return ver
}

// parseGoDirective extracts the "go" directive version as "go1.21".
func parseGoDirective(content string) string {
	for _, line := range strings.Split(content, "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) >= 2 && f[0] == "go" {
			v := strings.TrimPrefix(f[1], "go")
			// Normalize "1.21.0" to "1.21".
			if i := strings.Index(v, "."); i >= 0 {
				if j := strings.Index(v[i+1:], "."); j >= 0 {
					v = v[:i+1+j]
				}
			}
			return "go" + v
		}
	}
	return ""
}

// stdFileVersion computes the effective Go version for a file: the maximum
// of the module's go directive and any "go1.X" in its //go:build lines.
func stdFileVersion(f *ast.File, modVer string) string {
	best := modVer
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if !strings.HasPrefix(c.Text, "//go:build") {
				continue
			}
			for _, tok := range strings.Fields(c.Text) {
				if !strings.HasPrefix(tok, "go1") {
					continue
				}
				v := tok
				for len(v) > 3 && v[len(v)-1] != '.' && (v[len(v)-1] < '0' || v[len(v)-1] > '9') {
					v = v[:len(v)-1]
				}
				if best == "" || stdVersionAtLeast(v, best) {
					best = v
				}
			}
		}
	}
	return best
}

// isStdPkgPath reports whether path is a standard library import path.
func isStdPkgPath(path string) bool {
	if i := strings.Index(path, "/"); i >= 0 {
		path = path[:i]
	}
	return path != "" && !strings.Contains(path, ".")
}

// stdObjKey builds the api-database key for a package-level object
// (func without receiver, type name, var, const). Methods and struct
// fields are handled via selections; see stdSelectionKey.
func stdObjKey(obj types.Object) (string, bool) {
	pkg := obj.Pkg()
	if pkg == nil {
		return "", false
	}
	path := pkg.Path()
	switch o := obj.(type) {
	case *types.Func:
		if sig, ok := o.Type().(*types.Signature); ok && sig.Recv() != nil {
			return "", false // method: use stdSelectionKey
		}
		return path + "." + o.Origin().Name(), true
	case *types.TypeName:
		if named, ok := o.Type().(*types.Named); ok {
			tobj := named.Origin().Obj()
			if tobj.Pkg() == nil {
				return "", false
			}
			return tobj.Pkg().Path() + "." + tobj.Name(), true
		}
		return path + "." + o.Name(), true
	case *types.Var:
		if o.IsField() {
			return "", false
		}
		return path + "." + o.Name(), true
	case *types.Const:
		return path + "." + o.Name(), true
	}
	return "", false
}

// stdSelectionKey builds the api-database key for a method or field
// selection, plus the origin object of the receiver's named type.
func stdSelectionKey(pass *Pass, sel *ast.SelectorExpr) (key string, recvObj types.Object, ok bool) {
	s, found := pass.Info.Selections[sel]
	if !found {
		return "", nil, false
	}
	rt := s.Recv()
	if p, isPtr := rt.(*types.Pointer); isPtr {
		rt = p.Elem()
	}
	named, isNamed := rt.(*types.Named)
	if !isNamed {
		return "", nil, false
	}
	tobj := named.Origin().Obj()
	if tobj.Pkg() == nil {
		return "", nil, false
	}
	tpath := tobj.Pkg().Path() + "." + tobj.Name()
	switch o := s.Obj().(type) {
	case *types.Func:
		// Match the api "method (Recv) Name" key format using the
		// method's own declared receiver.
		sig, ok := o.Type().(*types.Signature)
		if !ok || sig.Recv() == nil {
			return "", nil, false
		}
		rtype := sig.Recv().Type()
		ptr := ""
		if p, isPtr := rtype.(*types.Pointer); isPtr {
			rtype = p.Elem()
			ptr = "*"
		}
		rnamed, ok := rtype.(*types.Named)
		if !ok {
			return "", nil, false
		}
		robj := rnamed.Origin().Obj()
		if robj.Pkg() == nil {
			return "", nil, false
		}
		return "(" + ptr + robj.Pkg().Path() + "." + robj.Name() + ")." + o.Origin().Name(), tobj, true
	case *types.Var:
		if !o.IsField() {
			return "", nil, false
		}
		return tpath + "." + o.Name(), tobj, true
	}
	return "", nil, false
}

// runStdVersion implements the stdversion analyzer: it reports references
// to standard library symbols introduced by a Go release newer than the
// version in force for the referring file (the module's go directive,
// raised by any //go:build go1.X constraint on the file).
func runStdVersion(pass *Pass) []Diagnostic {
	loadStdVersionDB()
	if stdVersionErr != nil || len(stdVersionDB) == 0 {
		return nil // no database available; best effort
	}
	var out []Diagnostic
	for _, f := range pass.Files {
		if ast.IsGenerated(f) {
			continue
		}
		filename := pass.Fset.Position(f.Pos()).Filename
		modVer := stdGoModVersion(filepath.Dir(filename))
		if modVer == "" || !stdVersionAtLeast(modVer, "go1.21") {
			continue
		}
		fileVer := stdFileVersion(f, modVer)
		if fileVer == "" {
			continue
		}
		// tooNew records origin objects of named types that are themselves
		// too new, so we can suppress diagnostics for their fields and
		// methods (avoids false positives through version-guarded aliases).
		tooNew := make(map[types.Object]bool)
		for _, obj := range pass.Info.Uses {
			tn, ok := obj.(*types.TypeName)
			if !ok || tn.Pkg() == nil || !isStdPkgPath(tn.Pkg().Path()) {
				continue
			}
			key, ok := stdObjKey(tn)
			if !ok {
				continue
			}
			if ver, ok := stdVersionDB[key]; ok && !stdVersionAtLeast(fileVer, ver) {
				if named, ok := tn.Type().(*types.Named); ok {
					tooNew[named.Origin().Obj()] = true
				} else {
					tooNew[tn] = true
				}
			}
		}
		report := func(pos ast.Node, pkgName, symName, ver string) {
			what := "module"
			if fileVer != modVer {
				what = "file"
			}
			out = append(out, Diagnostic{
				pass.position(pos),
				pkgName + "." + symName + " requires " + ver + " or later (" + what + " is " + fileVer + ")",
			})
		}
		// Direct uses: package-level funcs, types, vars, consts, including
		// qualified references like bytes.ContainsFunc.
		for id, obj := range pass.Info.Uses {
			if obj.Pkg() == nil || !isStdPkgPath(obj.Pkg().Path()) {
				continue
			}
			if sel := parentSelector(f, id); sel != nil {
				if _, isSel := pass.Info.Selections[sel]; isSel {
					continue // method/field: handled below
				}
			}
			key, ok := stdObjKey(obj)
			if !ok {
				continue
			}
			ver, ok := stdVersionDB[key]
			if !ok || stdVersionAtLeast(fileVer, ver) {
				continue
			}
			report(id, obj.Pkg().Name(), obj.Name(), ver)
		}
		// Selections: methods and fields.
		for sel := range pass.Info.Selections {
			key, recvObj, ok := stdSelectionKey(pass, sel)
			if !ok {
				continue
			}
			if tooNew[recvObj] {
				continue
			}
			ver, ok := stdVersionDB[key]
			if !ok || stdVersionAtLeast(fileVer, ver) {
				continue
			}
			obj := pass.Info.Selections[sel].Obj()
			pkgName := ""
			if obj.Pkg() != nil {
				pkgName = obj.Pkg().Name()
			}
			// Match go vet's display format: pkg.(*Recv).Method for
			// methods, pkg.Type.Field for fields.
			display := pkgName + "." + obj.Name()
			switch o := obj.(type) {
			case *types.Func:
				if sig, ok := o.Type().(*types.Signature); ok && sig.Recv() != nil {
					rtype := sig.Recv().Type()
					ptr := ""
					if p, isPtr := rtype.(*types.Pointer); isPtr {
						rtype, ptr = p.Elem(), "*"
					}
					if rnamed, ok := rtype.(*types.Named); ok {
						display = pkgName + ".(" + ptr + rnamed.Origin().Obj().Name() + ")." + o.Origin().Name()
					}
				}
			case *types.Var:
				if o.IsField() && recvObj != nil {
					display = pkgName + "." + recvObj.Name() + "." + o.Name()
				}
			}
			what := "module"
			if fileVer != modVer {
				what = "file"
			}
			out = append(out, Diagnostic{
				pass.position(sel.Sel),
				display + " requires " + ver + " or later (" + what + " is " + fileVer + ")",
			})
		}
	}
	return out
}

// parentSelector returns the *ast.SelectorExpr whose Sel is id, or nil.
func parentSelector(f *ast.File, id *ast.Ident) *ast.SelectorExpr {
	var found *ast.SelectorExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel == id {
			found = sel
			return false
		}
		return found == nil
	})
	return found
}
