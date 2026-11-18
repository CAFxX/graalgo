package emit

import (
	"reflect"
	"slices"
	"testing"

	"graalgo-toolexec/ir"
)

// //go:linkname and go1.27's //go:linknamestd are recorded alike (the link
// resolves by symbol name under either spelling, design §3.3), and a
// directive names a package-level object: a method that shares the name is
// not linknamed.
func TestLinknameDirectives(t *testing.T) {
	f := emitSrc(t, `package p

import _ "unsafe"

//go:linkname now runtime.nanotime
func now() int64

//go:linknamestd coro runtime.newcoro
func coro()

//go:linkname exported
func exported() {}

//go:linkname shared runtime.shared
var shared int

type T struct{}

func (T) now() int64 { return 0 }
`)
	want := []ir.Linkname{{Local: "now", Remote: "runtime.nanotime"}, {Local: "coro", Remote: "runtime.newcoro"},
		{Local: "exported"}, {Local: "shared", Remote: "runtime.shared"}}
	if !reflect.DeepEqual(f.Pkg.Linknames, want) {
		t.Errorf("linknames = %+v, want %+v", f.Pkg.Linknames, want)
	}
	got := map[string]string{}
	for _, fn := range f.Funcs {
		got[fn.Name] = fn.Linkname
	}
	for name, remote := range map[string]string{"now": "runtime.nanotime", "coro": "runtime.newcoro", "exported": "", "T.now": ""} {
		if l, ok := got[name]; !ok || l != remote {
			t.Errorf("function %s: linkname %q (present %v), want %q", name, l, ok, remote)
		}
	}
}

// A function's symbol references name the defined types with methods that
// occur in it (the linker's method reachability, design §5.3), and only
// those.
func TestTypeReferences(t *testing.T) {
	f := emitSrc(t, `package p

type T struct{ n int }

func (t *T) Close() {}

type Plain struct{ n int }

type G[E any] struct{ e E }

func (G[E]) Len() int { return 0 }

type I interface{ Close() }

var global = &T{}

func uses() any {
	var g G[*T]
	return g
}

func viaVar() any {
	return global
}

func none(p Plain, i I) int {
	return p.n
}
`)
	refs := func(name string) []string {
		for _, fn := range f.Funcs {
			if fn.Name != name {
				continue
			}
			var out []string
			for _, s := range fn.Syms {
				if r := f.SymRefs[s]; r.Kind == ir.SymKindTypeName {
					out = append(out, r.Pkg+"."+r.Name)
				}
			}
			return out
		}
		t.Fatalf("no function %s", name)
		return nil
	}
	for name, want := range map[string][]string{
		"uses":      {"p.T", "p.G"},
		"viaVar":    {"p.T"},
		"none":      nil,
		"init·vars": {"p.T"},
	} {
		got := refs(name)
		slices.Sort(got)
		slices.Sort(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s references types %v, want %v", name, got, want)
		}
	}
}
