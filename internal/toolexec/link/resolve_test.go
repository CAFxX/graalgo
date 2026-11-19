package link

import (
	"reflect"
	"strings"
	"testing"

	"graalgo-toolexec/bundle"
	"graalgo-toolexec/intrinsics"
	"graalgo-toolexec/ir"
)

// pb builds the parts of a package's GIR that resolution reads.
type pb struct{ f *ir.File }

func newPkg(path string) *pb {
	name := path[strings.LastIndexByte(path, '/')+1:]
	return &pb{&ir.File{Pkg: ir.Package{Path: path, Name: name}, Init: ir.Init{VarInit: -1}}}
}

func (p *pb) typ(key string, t ir.Type) ir.TypeID {
	p.f.Types = append(p.f.Types, ir.TypeEntry{Key: key, Type: t})
	return ir.TypeID(len(p.f.Types))
}

const (
	bodyless = 1
	method   = 4
)

// fn adds a function; refs are the functions its body names, as
// "pkg.Name" (another package, or a method) or "Name" (this package).
func (p *pb) fn(name string, flags uint64, linkname, sig string, refs ...string) *pb {
	fn := ir.Func{Name: name, Flags: flags, Linkname: linkname, Sig: p.typ(sig, &ir.SignatureType{})}
	for _, r := range refs {
		if t, ok := strings.CutPrefix(r, "type:"); ok {
			dot := strings.LastIndexByte(t, '.')
			p.f.SymRefs = append(p.f.SymRefs, ir.SymRef{Pkg: t[:dot], Name: t[dot+1:], Kind: ir.SymKindTypeName})
			fn.Syms = append(fn.Syms, uint32(len(p.f.SymRefs)-1))
			continue
		}
		if dot := strings.LastIndexByte(r, '.'); dot >= 0 && !strings.HasPrefix(r, "(") {
			kind := ir.SymKindFunc
			if strings.Count(r[strings.LastIndexByte(r, '/')+1:], ".") > 1 {
				kind = ir.SymKindMethod
			}
			pkg, sym := r[:dot], r[dot+1:]
			if kind == ir.SymKindMethod {
				first := strings.IndexByte(r[strings.LastIndexByte(r, '/')+1:], '.') + strings.LastIndexByte(r, '/') + 1
				pkg, sym = r[:first], r[first+1:]
			}
			p.f.SymRefs = append(p.f.SymRefs, ir.SymRef{Pkg: pkg, Name: sym, Kind: kind})
			fn.Syms = append(fn.Syms, uint32(len(p.f.SymRefs)-1))
			continue
		}
		p.f.Objects = append(p.f.Objects, ir.Object{Name: r, Kind: ir.SymKindFunc, Fn: -1})
		fn.Objs = append(fn.Objs, uint32(len(p.f.Objects)-1))
	}
	p.f.Funcs = append(p.f.Funcs, fn)
	return p
}

// finish points the function objects at their FUNCINDEX entries.
func (p *pb) finish() *ir.File {
	for i := range p.f.Objects {
		o := &p.f.Objects[i]
		if o.Kind != ir.SymKindFunc {
			continue
		}
		for j := range p.f.Funcs {
			if p.f.Funcs[j].Name == o.Name {
				o.Fn = int32(j)
			}
		}
	}
	return p.f
}

// typedef declares defined type name with the given methods in its method
// set (each a FUNCINDEX entry "name.M" of this package).
func (p *pb) typedef(name string, methods ...string) *pb {
	p.f.SymRefs = append(p.f.SymRefs, ir.SymRef{Pkg: p.f.Pkg.Path, Name: name, Kind: ir.SymKindTypeName})
	td := ir.TypeDef{Type: p.typ(p.f.Pkg.Path+"."+name, &ir.NamedType{Sym: uint32(len(p.f.SymRefs) - 1)})}
	for _, m := range methods {
		p.f.SymRefs = append(p.f.SymRefs, ir.SymRef{Pkg: p.f.Pkg.Path, Name: name + "." + m, Kind: ir.SymKindMethod})
		td.Mset = append(td.Mset, ir.MsetEntry{Name: m, Method: int32(len(p.f.SymRefs) - 1)})
	}
	p.f.TypeDefs = append(p.f.TypeDefs, td)
	return p
}

func (p *pb) variable(name string) *pb {
	p.f.Objects = append(p.f.Objects, ir.Object{Name: name, Kind: ir.SymKindVar, Fn: -1})
	return p
}

func (p *pb) linkname(local, remote string) *pb {
	p.f.Pkg.Linknames = append(p.f.Pkg.Linknames, ir.Linkname{Local: local, Remote: remote})
	return p
}

func registry(t *testing.T, text string) *intrinsics.Table {
	t.Helper()
	reg, err := intrinsics.Parse("version 1\n" + text)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func resolve(t *testing.T, reg *intrinsics.Table, pkgs ...*pb) (*bundle.Resolution, []string) {
	t.Helper()
	var files []*ir.File
	for _, p := range pkgs {
		files = append(files, p.finish())
	}
	res, err := Resolve(files, reg)
	if err != nil {
		le, ok := err.(*Error)
		if !ok {
			t.Fatal(err)
		}
		return nil, le.Problems
	}
	return res, nil
}

func TestBindings(t *testing.T) {
	reg := registry(t, `
intrinsic rt.nanotime C func() int64
intrinsic m.archFloor C func(float64) float64
replace m.Bits C func(float64) uint64
`)
	main := newPkg("main").
		fn("main", 0, "", "func()", "now", "pushed", "m.Floor", "m.Bits", "m.T.Get").
		fn("now", bodyless, "rt.nanotime", "func() int64").
		fn("pushed", bodyless, "", "func(int) int")
	m := newPkg("m").
		fn("Floor", 0, "", "func(float64) float64", "archFloor").
		fn("archFloor", bodyless, "", "func(float64) float64").
		fn("Bits", 0, "", "func(float64) uint64", "unsafeTrick").
		fn("unsafeTrick", bodyless, "", "func()"). // only the replaced body reaches it
		fn("T.Get", method, "", "func() int").
		fn("impl", 0, "main.pushed", "func(int) int")
	rt := newPkg("rt").fn("nanotime", bodyless, "", "func() int64")
	res, problems := resolve(t, reg, main, m, rt)
	if problems != nil {
		t.Fatalf("link failed: %v", problems)
	}
	want := []bundle.FuncBinding{
		{Pkg: "m", Name: "Bits", Intrinsic: "m.Bits"},
		{Pkg: "m", Name: "archFloor", Intrinsic: "m.archFloor"},
		{Pkg: "main", Name: "now", Intrinsic: "rt.nanotime"},
		{Pkg: "main", Name: "pushed", TargetPkg: "m", TargetName: "impl"},
		{Pkg: "rt", Name: "nanotime", Intrinsic: "rt.nanotime"},
	}
	if !reflect.DeepEqual(res.Funcs, want) {
		t.Errorf("bindings:\n got %+v\nwant %+v", res.Funcs, want)
	}
	if res.RegistryVersion != 1 || res.RegistryHash == "" {
		t.Errorf("registry identity %d %q", res.RegistryVersion, res.RegistryHash)
	}
}

func TestReachability(t *testing.T) {
	reg := registry(t, "")
	build := func(mainRefs ...string) []*pb {
		return []*pb{
			newPkg("main").fn("main", 0, "", "func()", mainRefs...),
			newPkg("a/lib").
				fn("Used", 0, "", "func()", "helper").
				fn("helper", 0, "", "func()", "asm").
				fn("asm", bodyless, "", "func()").
				fn("Other", 0, "", "func()", "asm2").
				fn("asm2", bodyless, "sys.raw", "func()").
				fn("T.Close", method, "", "func()", "asm3").
				fn("asm3", bodyless, "", "func()"),
		}
	}
	// Nothing reaches the body-less functions: the link succeeds and
	// records nothing for them.
	if res, problems := resolve(t, reg, build()...); problems != nil || len(res.Funcs) != 0 {
		t.Fatalf("unreachable undefined functions: %v, %+v", problems, res)
	}
	_, problems := resolve(t, reg, build("a/lib.Used", "a/lib.Other")...)
	want := []string{
		"undefined: a/lib.asm (no Go body and no graalgo intrinsic), referenced by a/lib.helper",
		"undefined: sys.raw (no Go body and no graalgo intrinsic; //go:linkname of a/lib.asm2), referenced by a/lib.Other",
	}
	if !reflect.DeepEqual(problems, want) {
		t.Errorf("problems:\n got %q\nwant %q", problems, want)
	}
	// A method nobody names is reached when a reachable function has an
	// expression of its receiver type and an interface type of the link
	// has a method of its name — and not by either alone.
	iface := func(p *pb) {
		p.typ("interface { Close() }", &ir.InterfaceType{Methods: []ir.IfaceMethod{{Name: "Close"}}})
	}
	pkgs := build()
	pkgs[1].typedef("T", "Close")
	iface(pkgs[0])
	if _, problems = resolve(t, reg, pkgs...); problems != nil {
		t.Errorf("an interface method name alone reached the method: %q", problems)
	}
	pkgs = build("type:a/lib.T")
	pkgs[1].typedef("T", "Close")
	if _, problems = resolve(t, reg, pkgs...); problems != nil {
		t.Errorf("a type reference alone reached the method: %q", problems)
	}
	pkgs = build("type:a/lib.T")
	pkgs[1].typedef("T", "Close")
	iface(pkgs[0])
	_, problems = resolve(t, reg, pkgs...)
	want = []string{"undefined: a/lib.asm3 (no Go body and no graalgo intrinsic), referenced by a/lib.T.Close"}
	if !reflect.DeepEqual(problems, want) {
		t.Errorf("problems:\n got %q\nwant %q", problems, want)
	}
	// An initializer is a root.
	pkgs = build()
	pkgs[1].fn("init.0", 8, "", "func()", "asm")
	pkgs[1].f.Init.Inits = []uint32{uint32(len(pkgs[1].f.Funcs) - 1)}
	if _, problems = resolve(t, reg, pkgs...); len(problems) != 1 || !strings.Contains(problems[0], "a/lib.asm ") {
		t.Errorf("init root: %q", problems)
	}
}

func TestSignatureMismatchAndDuplicates(t *testing.T) {
	reg := registry(t, "intrinsic rt.nanotime C func() int64\nreplace m.Bits C func(float64) uint64\n")
	_, problems := resolve(t, reg,
		newPkg("main").fn("main", 0, "", "func()"),
		newPkg("rt").fn("nanotime", bodyless, "", "func() int32"),
		newPkg("m").fn("Bits", 0, "", "func(float32) uint32"))
	want := []string{
		"m.Bits is declared func(float32) uint32; intrinsic m.Bits of registry version 1 is func(float64) uint64",
		"rt.nanotime is declared func() int32; intrinsic rt.nanotime of registry version 1 is func() int64",
	}
	if !reflect.DeepEqual(problems, want) {
		t.Errorf("problems:\n got %q\nwant %q", problems, want)
	}
	_, problems = resolve(t, reg,
		newPkg("main").fn("main", 0, "", "func()"),
		newPkg("a").fn("f", 0, "x.sym", "func()"),
		newPkg("b").fn("g", 0, "x.sym", "func()"))
	if len(problems) != 1 || !strings.Contains(problems[0], "duplicated definition of symbol x.sym") {
		t.Errorf("duplicate definition: %q", problems)
	}
}

// A method binds to a `method` entry and a function to another kind: the
// two calling conventions differ by the receiver.
func TestMethodEntries(t *testing.T) {
	reg := registry(t, "method rt.(*Func).Name C func() string\nintrinsic rt.nanotime C func() int64\nmethod rt.now C func() int64\nintrinsic rt.T.M C func()\n")
	res, problems := resolve(t, reg,
		newPkg("main").fn("main", 0, "", "func()"),
		newPkg("rt").fn("(*Func).Name", bodyless|method, "", "func() string").fn("nanotime", bodyless, "", "func() int64"))
	if len(problems) != 0 || len(res.Funcs) != 2 {
		t.Errorf("matching entries: problems %q, bindings %v", problems, res.Funcs)
	}
	_, problems = resolve(t, reg,
		newPkg("main").fn("main", 0, "", "func()"),
		newPkg("rt").fn("now", bodyless, "", "func() int64").fn("T.M", bodyless|method, "", "func()"))
	if len(problems) != 2 || !strings.Contains(problems[0], "rt.T.M: a method binds to a `method` entry") ||
		!strings.Contains(problems[1], "rt.now: a method binds to a `method` entry") {
		t.Errorf("mismatched entries: %q", problems)
	}
}

func TestVariableAliases(t *testing.T) {
	reg := registry(t, "")
	res, problems := resolve(t, reg,
		newPkg("main").fn("main", 0, "", "func()"),
		newPkg("rt").variable("overflowError"),
		newPkg("math/bits").variable("overflowError").linkname("overflowError", "rt.overflowError"),
		// No package declares gone.shared: the two share the first one's storage.
		newPkg("b").variable("x").linkname("x", "gone.shared"),
		newPkg("c").variable("y").linkname("y", "gone.shared"),
		// A linkname on a function is not a variable alias.
		newPkg("d").fn("f", 0, "rt.f", "func()").linkname("f", "rt.f"))
	if problems != nil {
		t.Fatal(problems)
	}
	want := []bundle.VarAlias{
		{Pkg: "c", Name: "y", TargetPkg: "b", TargetName: "x"},
		{Pkg: "math/bits", Name: "overflowError", TargetPkg: "rt", TargetName: "overflowError"},
	}
	if !reflect.DeepEqual(res.Vars, want) {
		t.Errorf("aliases:\n got %+v\nwant %+v", res.Vars, want)
	}
}
