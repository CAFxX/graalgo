package girinterp

import (
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"graalgo-toolexec/ir"
)

// mutant simulates one emitter bug by rewriting the emitted GIR before it
// is encoded.
type mutant struct {
	name string
	hook emitHook
}

// rewriteExprs replaces every expression x (bottom-up) with f(x).
func rewriteExprs(v reflect.Value, f func(ir.Expr) ir.Expr) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			rewriteExprs(v.Elem(), f)
		}
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		rewriteExprs(v.Elem(), f)
		if x, ok := v.Interface().(ir.Expr); ok && v.CanSet() && v.Type() == reflect.TypeOf((*ir.Expr)(nil)).Elem() {
			v.Set(reflect.ValueOf(f(x)))
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				rewriteExprs(v.Field(i), f)
			}
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return
		}
		for i := range v.Len() {
			rewriteExprs(v.Index(i), f)
		}
	}
}

func eachBody(f *ir.File, fn func(*ir.FuncBody)) {
	for _, b := range f.Bodies {
		if b != nil {
			fn(b)
			for _, lit := range nodesIn[*ir.FuncLitExpr](b) {
				fn(&lit.Body)
			}
		}
	}
}

func nodesIn[T any](root any) []T {
	var out []T
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if v.IsNil() {
				return
			}
			if x, ok := v.Interface().(T); ok {
				out = append(out, x)
			}
			walk(v.Elem())
		case reflect.Struct:
			for i := range v.NumField() {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		case reflect.Slice:
			if v.Type().Elem().Kind() != reflect.Uint8 {
				for i := range v.Len() {
					walk(v.Index(i))
				}
			}
		}
	}
	walk(reflect.ValueOf(root))
	return out
}

var mutants = []mutant{
	{"drop implicit conversions", func(f *ir.File) {
		rewriteExprs(reflect.ValueOf(f), func(x ir.Expr) ir.Expr {
			if c, ok := x.(*ir.ConvertExpr); ok && c.Implicit {
				return c.X
			}
			return x
		})
	}},
	{"drop element-wise tuple conversions", func(f *ir.File) {
		rewriteExprs(reflect.ValueOf(f), func(x ir.Expr) ir.Expr {
			if c, ok := x.(*ir.TupleConvertExpr); ok {
				return c.X
			}
			return x
		})
	}},
	{"wrong iota (named integer constants off by one)", func(f *ir.File) {
		rewriteExprs(reflect.ValueOf(f), func(x ir.Expr) ir.Expr {
			c, ok := x.(*ir.ConstExpr)
			if ok && c.Value.Kind == ir.ConstKindInt {
				if _, named := f.Types[c.Type-1].Type.(*ir.NamedType); named {
					c.Value.Text += "1" // 0 -> 01 == 1, 2 -> 21, ...
				}
			}
			return x
		})
	}},
	{"wrong init order (initializers reversed)", func(f *ir.File) {
		if f.Init.VarInit >= 0 {
			slices.Reverse(f.Bodies[f.Init.VarInit].Body)
		}
	}},
	{"init functions out of order", func(f *ir.File) {
		slices.Reverse(f.Init.Inits)
	}},
	{"wrong capture mode (go1.22 per-iteration loop variables dropped)", func(f *ir.File) {
		eachBody(f, func(b *ir.FuncBody) {
			for _, s := range nodesIn[*ir.ForStmt](b) {
				s.PerIter = nil
			}
			for _, s := range nodesIn[*ir.RangeStmt](b) {
				s.PerIter = false
			}
		})
	}},
	{"captured flag dropped", func(f *ir.File) {
		eachBody(f, func(b *ir.FuncBody) {
			for i := range b.Locals {
				b.Locals[i].Flags &^= 8
			}
		})
	}},
	{"promoted methods missing from method sets", func(f *ir.File) {
		for i := range f.TypeDefs {
			td := &f.TypeDefs[i]
			keep := func(ms []ir.MsetEntry) []ir.MsetEntry {
				return slices.DeleteFunc(ms, func(m ir.MsetEntry) bool { return len(m.Path) > 0 })
			}
			td.Mset, td.PtrMset = keep(td.Mset), keep(td.PtrMset)
		}
	}},
	{"local types of generic functions keyed without the instantiation", func(f *ir.File) {
		for i := range f.Types {
			if n, ok := f.Types[i].Type.(*ir.NamedType); ok && strings.Contains(f.SymRefs[n.Sym].Name, "·") && len(n.Targs) > 0 {
				n.Targs = nil
				f.Types[i].Key = f.Types[i].Key[:strings.IndexByte(f.Types[i].Key, '[')]
			}
		}
		for i := range f.TypeDefs {
			if n, ok := f.Types[f.TypeDefs[i].Type-1].Type.(*ir.NamedType); ok && strings.Contains(f.SymRefs[n.Sym].Name, "·") {
				f.TypeDefs[i].Tparams = nil
			}
		}
	}},
	{"implicit return at the closing brace dropped", func(f *ir.File) {
		eachBody(f, func(b *ir.FuncBody) {
			if n := len(b.Body); n > 0 && len(b.Results) == 0 {
				if r, ok := b.Body[n-1].(*ir.ReturnStmt); ok && len(r.Results) == 0 {
					b.Body = b.Body[:n-1]
				}
			}
		})
	}},
	{"call positions at the start of the callee expression", func(f *ir.File) {
		eachBody(f, func(b *ir.FuncBody) {
			for _, c := range nodesIn[*ir.MethodCallExpr](b) {
				c.Pos = recvPos(c.Recv)
			}
		})
	}},
	{"shift count typed as the shifted operand", func(f *ir.File) {
		rewriteExprs(reflect.ValueOf(f), func(x ir.Expr) ir.Expr {
			if b, ok := x.(*ir.BinaryExpr); ok && (b.Op == ir.BinOpShl || b.Op == ir.BinOpShr) {
				if c, ok := b.Y.(*ir.ConstExpr); ok {
					c.Type = b.Type // a uint8 operand truncates a count like 300
					if v := c.Value.Text; v == "8" || v == "16" || v == "32" || v == "64" {
						c.Value.Text = "0"
					}
				}
			}
			return x
		})
	}},
}

// recvPos is the position of the receiver expression (a different line
// than the call's '(' when the selector is split across lines).
func recvPos(x ir.Expr) ir.PosID {
	v := reflect.ValueOf(x).Elem().FieldByName("Pos")
	return ir.PosID(v.Uint())
}

// TestMutants checks that the differential harness kills simulated
// emitter bugs: for each mutant, at least one program must differ from gc
// (or be rejected by the interpreter's GIR consistency checks).
func TestMutants(t *testing.T) {
	if testing.Short() {
		t.Skip("builds every program with gc")
	}
	progs := append(corpusPrograms(t), ownPrograms(t)...)
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			var mu sync.Mutex
			var killers []string
			sem := make(chan struct{}, 4)
			var wg sync.WaitGroup
			for _, p := range progs {
				wg.Add(1)
				go func() {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					gc := runGC(t, p)
					gir, err := runGIR(p, m.hook)
					if err == nil && compare(gc, gir) == "" {
						return
					}
					mu.Lock()
					killers = append(killers, p.name)
					mu.Unlock()
				}()
			}
			wg.Wait()
			if len(killers) == 0 {
				t.Fatalf("mutant survived: no program detects %q", m.name)
			}
			slices.Sort(killers)
			t.Logf("killed by %d programs, e.g. %s", len(killers), strings.Join(killers[:min(3, len(killers))], ", "))
		})
	}
}
