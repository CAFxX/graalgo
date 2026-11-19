// Package link is graalgo-tool's link-time symbol resolution
// (docs/arch/toolexec-design.md D9, §3.3, §5.3; GG-0020, GG-0021).
//
// Resolve binds, by symbol name, every function declared without a body to
// a Go body somewhere in the link (//go:linkname in either direction) or
// to a Java intrinsic of the registry; binds functions the registry
// replaces (gc's compiler intrinsics); unifies variables that
// //go:linkname makes one; and computes which functions are reachable from
// main and the package initializers. A reachable function that stays
// unbound fails the link, naming the symbol. The same rule serves the
// standard library, the fork and third-party modules: nothing here names a
// package.
package link

import (
	"fmt"
	"sort"
	"strings"

	"graalgo-toolexec/bundle"
	"graalgo-toolexec/intrinsics"
	"graalgo-toolexec/ir"
)

// pkg is one package of the link.
type pkg struct {
	file    *ir.File
	path    string
	funcs   map[string]int // gc symbol name without the package -> FUNCINDEX
	objects map[string]int // package-level name -> OBJECTS
}

type funcID struct {
	pkg *pkg
	fn  int
}

func (f funcID) symbol() string { return f.pkg.path + "." + f.pkg.file.Funcs[f.fn].Name }

const (
	flagBodyless = 1
	flagMethod   = 4
)

// Resolve resolves the packages of one link. The result is deterministic:
// bindings and aliases are sorted by (package, name).
func Resolve(files []*ir.File, reg *intrinsics.Table) (*bundle.Resolution, error) {
	pkgs := map[string]*pkg{}
	var order []*pkg
	for _, f := range files {
		p := &pkg{file: f, path: f.Pkg.Path, funcs: map[string]int{}, objects: map[string]int{}}
		if pkgs[p.path] != nil {
			return nil, fmt.Errorf("package %s is in the link twice", p.path)
		}
		for i := range f.Funcs {
			p.funcs[f.Funcs[i].Name] = i
		}
		for i := range f.Objects {
			p.objects[f.Objects[i].Name] = i
		}
		pkgs[p.path] = p
		order = append(order, p)
	}
	sort.Slice(order, func(i, j int) bool { return order[i].path < order[j].path })
	var errs []string

	// Rule 1's table: symbol -> the function with a Go body that defines
	// it. A body carrying //go:linkname local remote defines remote (gc
	// renames the symbol) and stays callable by its own name in its
	// package.
	bodies := map[string]funcID{}
	define := func(sym string, id funcID) {
		if other, dup := bodies[sym]; dup && other != id {
			errs = append(errs, fmt.Sprintf("duplicated definition of symbol %s (%s and %s)", sym, other.symbol(), id.symbol()))
			return
		}
		bodies[sym] = id
	}
	for _, p := range order {
		for i := range p.file.Funcs {
			fn := &p.file.Funcs[i]
			if fn.Flags&flagBodyless != 0 {
				continue
			}
			id := funcID{p, i}
			if fn.Linkname != "" && fn.Linkname != id.symbol() {
				define(fn.Linkname, id)
			} else {
				define(id.symbol(), id)
			}
		}
	}

	res := &bundle.Resolution{RegistryVersion: uint64(reg.Version), RegistryHash: fmt.Sprintf("%x", reg.Hash[:])}
	// binding of a function that is not simply its own body.
	type binding struct {
		target    funcID // Go body, if hasTarget
		hasTarget bool
		intrinsic string
	}
	bound := map[funcID]binding{}
	unbound := map[funcID]bool{}
	checkSig := func(id funcID, e intrinsics.Entry) bool {
		fn := &id.pkg.file.Funcs[id.fn]
		if isMethod := fn.Flags&flagMethod != 0; isMethod != e.Method {
			errs = append(errs, fmt.Sprintf("%s: a method binds to a `method` entry of the registry and a function to another; intrinsic %s is not that", id.symbol(), e.Symbol))
			return false
		}
		if got := id.pkg.file.Types[fn.Sig-1].Key; got != e.Sig {
			errs = append(errs, fmt.Sprintf("%s is declared %s; intrinsic %s of registry version %d is %s", id.symbol(), got, e.Symbol, reg.Version, e.Sig))
			return false
		}
		return true
	}
	for _, p := range order {
		for i := range p.file.Funcs {
			fn := &p.file.Funcs[i]
			id := funcID{p, i}
			if fn.Flags&flagBodyless == 0 {
				// gc's compiler intrinsics: the registry's implementation
				// takes the place of the Go body.
				if e, ok := reg.Lookup(id.symbol()); ok && e.Replace && fn.Linkname == "" && checkSig(id, e) {
					bound[id] = binding{intrinsic: e.Symbol}
					res.Funcs = append(res.Funcs, bundle.FuncBinding{Pkg: p.path, Name: fn.Name, Intrinsic: e.Symbol})
				}
				continue
			}
			sym := id.symbol()
			if fn.Linkname != "" {
				sym = fn.Linkname
			}
			if target, ok := bodies[sym]; ok {
				bound[id] = binding{target: target, hasTarget: true}
				res.Funcs = append(res.Funcs, bundle.FuncBinding{Pkg: p.path, Name: fn.Name, TargetPkg: target.pkg.path, TargetName: target.pkg.file.Funcs[target.fn].Name})
			} else if e, ok := reg.Lookup(sym); ok && !e.Replace {
				if checkSig(id, e) {
					bound[id] = binding{intrinsic: e.Symbol}
					res.Funcs = append(res.Funcs, bundle.FuncBinding{Pkg: p.path, Name: fn.Name, Intrinsic: e.Symbol})
				}
			} else {
				unbound[id] = true
			}
		}
	}

	// Variables that //go:linkname makes one symbol share storage: the
	// variable of the package the remote name belongs to when that package
	// declares it, else the first member in package order.
	type varID struct {
		pkg  *pkg
		name string
	}
	groups := map[string][]varID{}
	var remotes []string
	for _, p := range order {
		for _, ln := range p.file.Pkg.Linknames {
			if ln.Remote == "" || ln.Remote == p.path+"."+ln.Local {
				continue
			}
			if o, ok := p.objects[ln.Local]; !ok || p.file.Objects[o].Kind != ir.SymKindVar {
				continue
			}
			if groups[ln.Remote] == nil {
				remotes = append(remotes, ln.Remote)
			}
			groups[ln.Remote] = append(groups[ln.Remote], varID{p, ln.Local})
		}
	}
	sort.Strings(remotes)
	for _, remote := range remotes {
		members := groups[remote]
		owner := members[0]
		rest := members[1:]
		// The remote symbol is pkgpath.name; the name has no dot.
		if dot := strings.LastIndexByte(remote, '.'); dot > 0 {
			if rp := pkgs[remote[:dot]]; rp != nil {
				if o, ok := rp.objects[remote[dot+1:]]; ok && rp.file.Objects[o].Kind == ir.SymKindVar {
					owner = varID{rp, remote[dot+1:]}
					rest = members
				}
			}
		}
		for _, m := range rest {
			if m != owner {
				res.Vars = append(res.Vars, bundle.VarAlias{Pkg: m.pkg.path, Name: m.name, TargetPkg: owner.pkg.path, TargetName: owner.name})
			}
		}
	}

	// Reachability (rule 3), the gc linker's deadcode rule over FUNCINDEX
	// references: roots are main.main and every package's initializers; a
	// function reaches the functions and methods its body names. A method
	// nobody names can only be called through an interface: it is reached
	// when a reachable function has an expression of its receiver type (a
	// TypeName reference, which is how a value gets into an interface) and
	// some interface type of the link has a method of its name. gc
	// additionally matches the signature and requires the conversion
	// itself; this rule can only reach more, never less.
	ifaceMethods := map[string]bool{}
	// typeDefs: defined type -> its TYPEDEFS entry (the complete method
	// sets, promoted methods included). structDefs: the entries of unnamed
	// struct types, which no reference names.
	type typeID struct{ pkg, name string }
	type typeDef struct {
		pkg *pkg
		def *ir.TypeDef
	}
	typeDefs := map[typeID]typeDef{}
	var structDefs []typeDef
	for _, p := range order {
		for _, te := range p.file.Types {
			if it, ok := te.Type.(*ir.InterfaceType); ok {
				for _, m := range it.Methods {
					ifaceMethods[m.Name] = true
				}
			}
		}
		for i := range p.file.TypeDefs {
			td := &p.file.TypeDefs[i]
			if nt, ok := p.file.Types[td.Type-1].Type.(*ir.NamedType); ok {
				typeDefs[typeID{p.path, p.file.SymRefs[nt.Sym].Name}] = typeDef{p, td}
			} else {
				structDefs = append(structDefs, typeDef{p, td})
			}
		}
	}
	reached := map[funcID]funcID{} // function -> first referrer (itself for roots)
	var work []funcID
	reach := func(id, from funcID) {
		if b, ok := bound[id]; ok && b.hasTarget {
			if _, seen := reached[id]; !seen {
				reached[id] = from
			}
			id = b.target
		}
		if _, seen := reached[id]; !seen {
			reached[id] = from
			work = append(work, id)
		}
	}
	for _, p := range order {
		f := p.file
		if f.Init.VarInit >= 0 {
			id := funcID{p, int(f.Init.VarInit)}
			reach(id, id)
		}
		for _, fn := range f.Init.Inits {
			id := funcID{p, int(fn)}
			reach(id, id)
		}
		if f.Pkg.Name == "main" {
			if fn, ok := p.funcs["main"]; ok {
				id := funcID{p, fn}
				reach(id, id)
			}
		}
	}
	// useType reaches the methods of a type's method sets that an
	// interface call could select.
	usedTypes := map[typeID]bool{}
	useDef := func(td typeDef, from funcID) {
		for _, set := range [][]ir.MsetEntry{td.def.Mset, td.def.PtrMset} {
			for _, m := range set {
				if m.Method < 0 || !ifaceMethods[m.Name] {
					continue
				}
				ref := td.pkg.file.SymRefs[m.Method]
				if tp := pkgs[ref.Pkg]; tp != nil {
					if t, ok := tp.funcs[ref.Name]; ok {
						id := funcID{tp, t}
						if from.pkg == nil {
							reach(id, id)
						} else {
							reach(id, from)
						}
					}
				}
			}
		}
	}
	for _, td := range structDefs {
		useDef(td, funcID{})
	}
	for len(work) > 0 {
		id := work[len(work)-1]
		work = work[:len(work)-1]
		if b, ok := bound[id]; ok && !b.hasTarget {
			continue // an intrinsic: the body, if any, is not executed
		}
		f := id.pkg.file
		fn := &f.Funcs[id.fn]
		for _, s := range fn.Syms {
			ref := f.SymRefs[s]
			if ref.Kind == ir.SymKindTypeName {
				tid := typeID{ref.Pkg, ref.Name}
				if td, ok := typeDefs[tid]; ok && !usedTypes[tid] {
					usedTypes[tid] = true
					useDef(td, id)
				}
				continue
			}
			if ref.Kind != ir.SymKindFunc && ref.Kind != ir.SymKindMethod {
				continue
			}
			tp := pkgs[ref.Pkg]
			if tp == nil {
				errs = append(errs, fmt.Sprintf("%s references %s.%s, but package %s is not in the link", id.symbol(), ref.Pkg, ref.Name, ref.Pkg))
				continue
			}
			if t, ok := tp.funcs[ref.Name]; ok {
				reach(funcID{tp, t}, id)
			}
		}
		for _, o := range fn.Objs {
			if obj := f.Objects[o]; obj.Kind == ir.SymKindFunc && obj.Fn >= 0 {
				reach(funcID{id.pkg, int(obj.Fn)}, id)
			}
		}
	}
	for id := range unbound {
		from, ok := reached[id]
		if !ok {
			continue // absent: calling it through reflection panics at run time
		}
		fn := &id.pkg.file.Funcs[id.fn]
		msg := "undefined: " + id.symbol() + " (no Go body and no graalgo intrinsic)"
		if fn.Linkname != "" && fn.Linkname != id.symbol() {
			msg = "undefined: " + fn.Linkname + " (no Go body and no graalgo intrinsic; //go:linkname of " + id.symbol() + ")"
		}
		if from != id {
			msg += ", referenced by " + from.symbol()
		}
		errs = append(errs, msg)
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return nil, &Error{Problems: errs}
	}
	sort.Slice(res.Funcs, func(i, j int) bool {
		a, b := res.Funcs[i], res.Funcs[j]
		if a.Pkg != b.Pkg {
			return a.Pkg < b.Pkg
		}
		return a.Name < b.Name
	})
	sort.Slice(res.Vars, func(i, j int) bool {
		a, b := res.Vars[i], res.Vars[j]
		if a.Pkg != b.Pkg {
			return a.Pkg < b.Pkg
		}
		return a.Name < b.Name
	})
	return res, nil
}

// methodName is the method's own name in a symbol name "T.M" or "(*T).M".
func methodName(sym string) string {
	return sym[strings.LastIndexByte(sym, '.')+1:]
}

// Error is a failed link: every problem, sorted.
type Error struct {
	Problems []string
}

func (e *Error) Error() string { return strings.Join(e.Problems, "\n") }
