package link

import (
	"fmt"
	"slices"
	"strings"

	"graalgo-toolexec/ir"
)

// XSetting is one link -X setting: string variable Pkg.Name gets Value.
type XSetting struct {
	Pkg, Name, Value string
}

// Symbol is gc's symbol name of the variable (objabi.PathToPrefix applied
// to the package path), the name cmd/link's diagnostics print.
func (s XSetting) Symbol() string { return PathToPrefix(s.Pkg) + "." + s.Name }

// ParseX parses the arguments of the link's -X flags in command-line
// order, as cmd/link does (ld.addstrdata1): the first '=' ends the symbol,
// the last '.' before it separates the package path from the name, and a
// later setting of the same symbol replaces the value while the first one
// keeps its position.
func ParseX(args []string) ([]XSetting, error) {
	var out []XSetting
	at := map[string]int{}
	for _, arg := range args {
		eq := strings.Index(arg, "=")
		dot := strings.LastIndex(arg[:eq+1], ".")
		if eq < 0 || dot < 0 {
			return nil, fmt.Errorf("-X flag requires argument of the form importpath.name=value")
		}
		s := XSetting{Pkg: arg[:dot], Name: arg[dot+1 : eq], Value: arg[eq+1:]}
		if i, ok := at[s.Symbol()]; ok {
			out[i].Value = s.Value
			continue
		}
		at[s.Symbol()] = len(out)
		out = append(out, s)
	}
	return out, nil
}

// ApplyX applies the -X settings to the packages of a link by rewriting
// their variable initializers (toolexec-design §3.6), with cmd/link's
// semantics (ld.addstrdata): a setting naming no package-level variable of
// the link is ignored; one naming a variable whose type is not the
// predeclared string fails the link; otherwise the variable's static data
// becomes the value. That is observable as gc's rule: the value holds when
// gc initializes the variable statically (no initializer, a constant one,
// or one its static-init pass evaluates, such as an inlined call), and a
// dynamic initializer overwrites it when the package initializes.
//
// gcStatic reports whether gc's object for package files[i] holds static
// data for symbol; it is asked only about a variable with a non-constant
// initializer. ApplyX returns the indexes of the files it changed.
func ApplyX(files []*ir.File, settings []XSetting, gcStatic func(i int, symbol string) (bool, error)) ([]int, error) {
	byPath := map[string]int{}
	for i, f := range files {
		byPath[f.Pkg.Path] = i
	}
	var problems []string
	changed := map[int]bool{}
	for _, s := range settings {
		fi, ok := byPath[s.Pkg]
		if !ok {
			continue
		}
		f := files[fi]
		obj := slices.IndexFunc(f.Objects, func(o ir.Object) bool { return o.Name == s.Name && o.Kind == ir.SymKindVar })
		if obj < 0 {
			continue
		}
		te := f.Types[f.Objects[obj].Type-1]
		if b, ok := te.Type.(*ir.BasicType); !ok || b.Kind != ir.BasicKindString {
			problems = append(problems, fmt.Sprintf("%s: cannot set with -X: not a var of type string (type:%s)", s.Symbol(), te.Key))
			continue
		}
		if !changed[fi] {
			// The file is re-encoded: every body must be decoded.
			for i := range f.Funcs {
				if _, err := f.Body(i); err != nil {
					return nil, err
				}
			}
		}
		static := true
		body, at := initializerOf(f, uint32(obj))
		if at >= 0 && !isConst(body.Body[at].(*ir.AssignStmt).Rhs[0]) {
			var err error
			if static, err = gcStatic(fi, s.Symbol()); err != nil {
				return nil, fmt.Errorf("-X %s: %v", s.Symbol(), err)
			}
		}
		if at >= 0 && static {
			// gc's static data, which the linker overwrites: no code.
			body.Body = slices.Delete(body.Body, at, at+1)
		}
		body = varInitBody(f)
		t := f.Objects[obj].Type
		set := &ir.AssignStmt{Op: ir.AssignOpPlain, Lhs: []ir.Expr{&ir.GlobalExpr{Type: t, Obj: uint32(obj)}},
			Rhs: []ir.Expr{&ir.ConstExpr{Type: t, Value: ir.ConstValue{Kind: ir.ConstKindString, Text: s.Value}}}}
		// Before any initializer code, as static data is.
		body.Body = slices.Insert(body.Body, 0, ir.Stmt(set))
		fn := &f.Funcs[f.Init.VarInit]
		if i, found := slices.BinarySearch(fn.Objs, uint32(obj)); !found {
			fn.Objs = slices.Insert(fn.Objs, i, uint32(obj))
		}
		changed[fi] = true
	}
	if len(problems) > 0 {
		return nil, &Error{Problems: problems}
	}
	out := make([]int, 0, len(changed))
	for i := range changed {
		out = append(out, i)
	}
	slices.Sort(out)
	return out, nil
}

// initializerOf returns the package's variable-initializer body and the
// index of the statement that initializes object obj alone (-1: none).
func initializerOf(f *ir.File, obj uint32) (*ir.FuncBody, int) {
	if f.Init.VarInit < 0 || f.Bodies[f.Init.VarInit] == nil {
		return nil, -1
	}
	body := f.Bodies[f.Init.VarInit]
	for i, st := range body.Body {
		if a, ok := st.(*ir.AssignStmt); ok && len(a.Lhs) == 1 && len(a.Rhs) == 1 {
			if g, ok := a.Lhs[0].(*ir.GlobalExpr); ok && g.Obj == obj {
				return body, i
			}
		}
	}
	return body, -1
}

// isConst reports whether an initializer is a constant expression, which
// gc always initializes statically.
func isConst(x ir.Expr) bool {
	for {
		switch e := x.(type) {
		case *ir.ConstExpr:
			return true
		case *ir.ConvertExpr:
			x = e.X
		default:
			return false
		}
	}
}

// varInitBody returns the package's variable-initializer body, creating
// the function (init·vars, as the emitter names it) when the package has
// no initializer.
func varInitBody(f *ir.File) *ir.FuncBody {
	if f.Init.VarInit >= 0 && f.Bodies[f.Init.VarInit] != nil {
		return f.Bodies[f.Init.VarInit]
	}
	sig := slices.IndexFunc(f.Types, func(t ir.TypeEntry) bool { return t.Key == "func()" })
	if sig < 0 {
		f.Types = append(f.Types, ir.TypeEntry{Key: "func()", Type: &ir.SignatureType{}})
		sig = len(f.Types) - 1
	}
	body := &ir.FuncBody{Recv: -1}
	if f.Init.VarInit >= 0 {
		f.Bodies[f.Init.VarInit] = body
		return body
	}
	file := ""
	if len(f.Pkg.Files) > 0 {
		file = f.Pkg.Files[0]
	}
	f.Funcs = append(f.Funcs, ir.Func{Name: "init·vars", Flags: 16, Sig: ir.TypeID(sig + 1), File: file})
	f.Bodies = append(f.Bodies, body)
	f.Init.VarInit = int32(len(f.Funcs) - 1)
	return body
}

// PathToPrefix is cmd/internal/objabi.PathToPrefix: the package-path
// prefix of gc's symbol names, with control characters, space, '%', '"',
// non-ASCII bytes and the dots of the last path element escaped as %xx.
func PathToPrefix(s string) string {
	slash := strings.LastIndex(s, "/")
	var b strings.Builder
	for r := 0; r < len(s); r++ {
		if c := s[r]; c <= ' ' || (c == '.' && r > slash) || c == '%' || c == '"' || c >= 0x7F {
			const hex = "0123456789abcdef"
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xF])
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}
