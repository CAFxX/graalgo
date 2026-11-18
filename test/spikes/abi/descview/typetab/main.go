// Command typetab plays the role of the graalgo frontend's TYPEDEFS
// section (toolexec-design §3.2/§3.4): from go/types alone, with gc's size
// model, it emits everything a backend needs to synthesize gc-layout type
// descriptors for the fixture package's root types. No gc compiler output
// is consulted. Output: one record per line, strings hex-encoded.
package main

import (
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strings"
)

var sizes = types.SizesFor("gc", "amd64")

type table struct {
	ids   map[string]int
	types []types.Type
	out   []string
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: typetab <fixture dir>")
		os.Exit(2)
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, os.Args[1], nil, 0) //nolint:staticcheck // one plain package
	if err != nil {
		panic(err)
	}
	var files []*ast.File
	for _, p := range pkgs {
		for _, f := range p.Files {
			files = append(files, f)
		}
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil), Sizes: sizes}
	pkg, err := conf.Check("abispike/fixture", fset, files, info)
	if err != nil {
		panic(err)
	}
	t := &table{ids: map[string]int{}}
	var roots []int
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "Roots" {
				return true
			}
			for _, e := range vs.Values[0].(*ast.CompositeLit).Elts {
				roots = append(roots, t.id(types.Default(info.Types[e].Type)))
			}
			return false
		})
	}
	_ = pkg
	// The descriptor structs' own layout comes from the same source of truth:
	// internal/abi as go/types sees it. The backend hard-codes no offset.
	abiPkg, err := conf.Importer.Import("internal/abi")
	if err != nil {
		panic(err)
	}
	for _, name := range abiPkg.Scope().Names() {
		tn, ok := abiPkg.Scope().Lookup(name).(*types.TypeName)
		if !ok || tn.IsAlias() {
			continue
		}
		st, ok := tn.Type().Underlying().(*types.Struct)
		if !ok || tn.Type().(*types.Named).TypeParams().Len() > 0 {
			continue
		}
		t.out = append(t.out, fmt.Sprintf("layout struct=%s size=%d", name, sizes.Sizeof(tn.Type())))
		offs := sizes.Offsetsof(fields(st))
		for i := 0; i < st.NumFields(); i++ {
			t.out = append(t.out, fmt.Sprintf("lfield struct=%s name=%s offset=%d size=%d", name, st.Field(i).Name(), offs[i], sizes.Sizeof(st.Field(i).Type())))
		}
	}
	for i := 0; i < len(t.types); i++ { // closure grows while we emit
		t.emit(i)
	}
	for _, r := range roots {
		t.out = append(t.out, fmt.Sprintf("root type=%d", r))
	}
	fmt.Println(strings.Join(t.out, "\n"))
}

func hx(s string) string { return "x" + hex.EncodeToString([]byte(s)) }

func key(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string { return p.Path() })
}

func (t *table) id(typ types.Type) int {
	typ = types.Unalias(typ)
	k := key(typ)
	if id, ok := t.ids[k]; ok {
		return id
	}
	id := len(t.types)
	t.ids[k] = id
	t.types = append(t.types, typ)
	return id
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (t *table) emit(id int) {
	typ := t.types[id]
	var sb strings.Builder
	mask := ptrMask(typ)
	ptrBytes := 0
	for i, m := range mask {
		if m {
			ptrBytes = (i + 1) * 8
		}
	}
	maskBytes := make([]byte, ((ptrBytes/8+7)/8+7)&^7)
	for i, m := range mask {
		if m {
			maskBytes[i/8] |= 1 << (i % 8)
		}
	}
	named, isNamed := typ.(*types.Named)
	_, isBasic := typ.(*types.Basic)
	name, pkgPath := "", ""
	if isNamed {
		name = named.Obj().Name()
		if named.Obj().Pkg() != nil {
			pkgPath = named.Obj().Pkg().Path()
		}
	} else if isBasic {
		name = gcString(typ, false)
	}
	fmt.Fprintf(&sb, "type id=%d kind=%d size=%d align=%d fieldalign=%d ptrbytes=%d gcmask=%s str=%s link=%s named=%d name=%s pkgpath=%s comparable=%d regular=%d direct=%d",
		id, kindOf(typ), sizes.Sizeof(typ), sizes.Alignof(typ), sizes.Alignof(typ), ptrBytes, hx(string(maskBytes)),
		hx(gcString(typ, false)), hx(gcString(typ, true)), b2i(isNamed || isBasic), hx(name), hx(pkgPath),
		b2i(types.Comparable(typ)), b2i(regularMemory(typ)), b2i(directIface(typ)))
	var extra []string
	switch u := typ.Underlying().(type) {
	case *types.Pointer:
		fmt.Fprintf(&sb, " elem=%d", t.id(u.Elem()))
	case *types.Slice:
		fmt.Fprintf(&sb, " elem=%d", t.id(u.Elem()))
	case *types.Array:
		fmt.Fprintf(&sb, " elem=%d len=%d slice=%d", t.id(u.Elem()), u.Len(), t.id(types.NewSlice(u.Elem())))
	case *types.Chan:
		dir := map[types.ChanDir]int{types.RecvOnly: 1, types.SendOnly: 2, types.SendRecv: 3}[u.Dir()]
		fmt.Fprintf(&sb, " elem=%d dir=%d", t.id(u.Elem()), dir)
	case *types.Map:
		fmt.Fprintf(&sb, " key=%d elem=%d", t.id(u.Key()), t.id(u.Elem()))
	case *types.Signature:
		fmt.Fprintf(&sb, " variadic=%d in=%s out=%s", b2i(u.Variadic()), t.tuple(u.Params()), t.tuple(u.Results()))
	case *types.Struct:
		spkg := ""
		for i := 0; i < u.NumFields(); i++ {
			f := u.Field(i)
			if !f.Exported() && spkg == "" {
				spkg = f.Pkg().Path()
			}
		}
		fmt.Fprintf(&sb, " structpkg=%s", hx(spkg))
		offs := sizes.Offsetsof(fields(u))
		for i := 0; i < u.NumFields(); i++ {
			f := u.Field(i)
			extra = append(extra, fmt.Sprintf("field owner=%d name=%s type=%d offset=%d embedded=%d exported=%d tag=%s",
				id, hx(f.Name()), t.id(f.Type()), offs[i], b2i(f.Embedded()), b2i(f.Exported()), hx(u.Tag(i))))
		}
	case *types.Interface:
		ipkg := ""
		if isNamed && named.Obj().Pkg() != nil {
			ipkg = named.Obj().Pkg().Path()
		}
		fmt.Fprintf(&sb, " ifacepkg=%s", hx(ipkg))
		var ms []*types.Func
		for i := 0; i < u.NumMethods(); i++ {
			ms = append(ms, u.Method(i))
		}
		sortMethods(ms)
		for _, m := range ms {
			extra = append(extra, fmt.Sprintf("imethod owner=%d name=%s exported=%d type=%d", id, hx(m.Name()), b2i(m.Exported()), t.id(m.Type())))
		}
	}
	// Method set of a non-interface type (gc emits it in the uncommon type).
	if _, isIface := typ.Underlying().(*types.Interface); !isIface {
		mset := types.NewMethodSet(typ)
		var ms []*types.Func
		for i := 0; i < mset.Len(); i++ {
			ms = append(ms, mset.At(i).Obj().(*types.Func))
		}
		sortMethods(ms)
		for _, m := range ms {
			sig := m.Type().(*types.Signature)
			noRecv := types.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
			extra = append(extra, fmt.Sprintf("method owner=%d name=%s exported=%d type=%d", id, hx(m.Name()), b2i(m.Exported()), t.id(noRecv)))
		}
		// *T of a defined type carries T's pointer-receiver methods; make sure it exists,
		// and record it so the descriptor can point at it (PtrToThis).
		if isNamed {
			fmt.Fprintf(&sb, " ptrto=%d", t.id(types.NewPointer(typ)))
		}
		if p, ok := typ.(*types.Pointer); ok {
			if n, ok := types.Unalias(p.Elem()).(*types.Named); ok && n.Obj().Pkg() != nil {
				fmt.Fprintf(&sb, " upkg=%s", hx(n.Obj().Pkg().Path()))
			}
		}
	}
	t.out = append(t.out, sb.String())
	t.out = append(t.out, extra...)
}

func (t *table) tuple(tu *types.Tuple) string {
	if tu.Len() == 0 {
		return "-"
	}
	var ids []string
	for i := 0; i < tu.Len(); i++ {
		ids = append(ids, fmt.Sprint(t.id(tu.At(i).Type())))
	}
	return strings.Join(ids, ",")
}

func fields(s *types.Struct) []*types.Var {
	var fs []*types.Var
	for i := 0; i < s.NumFields(); i++ {
		fs = append(fs, s.Field(i))
	}
	return fs
}

// sortMethods orders as gc does: exported names first, then by name.
func sortMethods(ms []*types.Func) {
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].Exported() != ms[j].Exported() {
			return ms[i].Exported()
		}
		return ms[i].Name() < ms[j].Name()
	})
}

func kindOf(t types.Type) int {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return map[types.BasicKind]int{types.Bool: 1, types.Int: 2, types.Int8: 3, types.Int16: 4, types.Int32: 5, types.Int64: 6,
			types.Uint: 7, types.Uint8: 8, types.Uint16: 9, types.Uint32: 10, types.Uint64: 11, types.Uintptr: 12,
			types.Float32: 13, types.Float64: 14, types.Complex64: 15, types.Complex128: 16, types.String: 24, types.UnsafePointer: 26}[u.Kind()]
	case *types.Array:
		return 17
	case *types.Chan:
		return 18
	case *types.Signature:
		return 19
	case *types.Interface:
		return 20
	case *types.Map:
		return 21
	case *types.Pointer:
		return 22
	case *types.Slice:
		return 23
	case *types.Struct:
		return 25
	}
	panic("kind: " + t.String())
}

// ptrMask returns one bool per pointer-sized word of t: may hold a pointer.
func ptrMask(t types.Type) []bool {
	words := int((sizes.Sizeof(t) + 7) / 8)
	m := make([]bool, words)
	var fill func(t types.Type, off int64)
	fill = func(t types.Type, off int64) {
		switch u := t.Underlying().(type) {
		case *types.Basic:
			if u.Kind() == types.String || u.Kind() == types.UnsafePointer {
				m[off/8] = true
			}
		case *types.Pointer, *types.Chan, *types.Map, *types.Signature, *types.Slice:
			m[off/8] = true
		case *types.Interface:
			m[off/8], m[off/8+1] = true, true
		case *types.Array:
			es := sizes.Sizeof(u.Elem())
			for i := int64(0); i < u.Len(); i++ {
				fill(u.Elem(), off+i*es)
			}
		case *types.Struct:
			offs := sizes.Offsetsof(fields(u))
			for i := 0; i < u.NumFields(); i++ {
				fill(u.Field(i).Type(), off+offs[i])
			}
		}
	}
	fill(t, 0)
	return m
}

// regularMemory mirrors cmd/compile/internal/compare.IsRegularMemory:
// equality is a plain memory comparison of the whole object.
func regularMemory(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&(types.IsFloat|types.IsComplex|types.IsString) != 0:
			return false
		}
		return true
	case *types.Pointer, *types.Chan:
		return true
	case *types.Array:
		return regularMemory(u.Elem())
	case *types.Struct:
		offs := sizes.Offsetsof(fields(u))
		end := int64(0)
		for i := 0; i < u.NumFields(); i++ {
			f := u.Field(i)
			if f.Name() == "_" || !regularMemory(f.Type()) || offs[i] != end {
				return false
			}
			end = offs[i] + sizes.Sizeof(f.Type())
		}
		return end == sizes.Sizeof(t)
	}
	return false
}

// directIface mirrors types.IsDirectIface: the value is pointer-shaped.
func directIface(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Kind() == types.UnsafePointer
	case *types.Pointer, *types.Chan, *types.Map, *types.Signature:
		return true
	case *types.Array:
		return u.Len() == 1 && directIface(u.Elem())
	case *types.Struct:
		return u.NumFields() == 1 && directIface(u.Field(0).Type())
	}
	return false
}

// gcString renders t the way gc's reflect metadata does (Type.String);
// with link set, packages are written by import path (gc's LinkString, the
// input of the type hash).
func gcString(t types.Type, link bool) string {
	var sb strings.Builder
	writeType(&sb, t, link)
	return sb.String()
}

func writeType(sb *strings.Builder, t types.Type, link bool) {
	t = types.Unalias(t)
	switch u := t.(type) {
	case *types.Named:
		if p := u.Obj().Pkg(); p != nil {
			if link {
				sb.WriteString(p.Path())
			} else {
				sb.WriteString(p.Name())
			}
			sb.WriteByte('.')
		}
		sb.WriteString(u.Obj().Name())
	case *types.Basic:
		sb.WriteString(types.Typ[u.Kind()].Name()) // byte and rune print as uint8 and int32
	case *types.Pointer:
		sb.WriteByte('*')
		writeType(sb, u.Elem(), link)
	case *types.Slice:
		sb.WriteString("[]")
		writeType(sb, u.Elem(), link)
	case *types.Array:
		fmt.Fprintf(sb, "[%d]", u.Len())
		writeType(sb, u.Elem(), link)
	case *types.Map:
		sb.WriteString("map[")
		writeType(sb, u.Key(), link)
		sb.WriteByte(']')
		writeType(sb, u.Elem(), link)
	case *types.Chan:
		sb.WriteString(map[types.ChanDir]string{types.RecvOnly: "<-chan ", types.SendOnly: "chan<- ", types.SendRecv: "chan "}[u.Dir()])
		writeType(sb, u.Elem(), link)
	case *types.Signature:
		sb.WriteString("func")
		writeSig(sb, u, link)
	case *types.Struct:
		sb.WriteString("struct {")
		for i := 0; i < u.NumFields(); i++ {
			if i > 0 {
				sb.WriteByte(';')
			}
			sb.WriteByte(' ')
			f := u.Field(i)
			if !f.Embedded() {
				sb.WriteString(f.Name())
				sb.WriteByte(' ')
			}
			writeType(sb, f.Type(), link)
			if tag := u.Tag(i); tag != "" {
				fmt.Fprintf(sb, " %q", tag)
			}
		}
		if u.NumFields() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteByte('}')
	case *types.Interface:
		sb.WriteString("interface {")
		for i := 0; i < u.NumMethods(); i++ {
			if i > 0 {
				sb.WriteByte(';')
			}
			sb.WriteByte(' ')
			sb.WriteString(u.Method(i).Name())
			writeSig(sb, u.Method(i).Type().(*types.Signature), link)
		}
		if u.NumMethods() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteByte('}')
	default:
		panic("gcString: " + t.String())
	}
}

func writeSig(sb *strings.Builder, s *types.Signature, link bool) {
	sb.WriteByte('(')
	for i := 0; i < s.Params().Len(); i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		pt := s.Params().At(i).Type()
		if s.Variadic() && i == s.Params().Len()-1 {
			sb.WriteString("...")
			pt = pt.(*types.Slice).Elem()
		}
		writeType(sb, pt, link)
	}
	sb.WriteByte(')')
	switch r := s.Results(); r.Len() {
	case 0:
	case 1:
		sb.WriteByte(' ')
		writeType(sb, r.At(0).Type(), link)
	default:
		sb.WriteString(" (")
		for i := 0; i < r.Len(); i++ {
			if i > 0 {
				sb.WriteString(", ")
			}
			writeType(sb, r.At(i).Type(), link)
		}
		sb.WriteByte(')')
	}
}
