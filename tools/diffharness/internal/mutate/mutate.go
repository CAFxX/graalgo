// Package mutate implements the mutation generator used for oracle
// validation: it applies small, semantics-changing, still-compiling edits
// to corpus programs via go/ast. Each mutation changes exactly one site
// (the first eligible site in source order) and reports a human-readable
// description with the source position. The harness must report DIVERGE for
// every generated mutation (red); unmutated programs must report SAME (green).
package mutate

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"strconv"
)

// Classes is the full set of mutation classes the generator supports.
var Classes = []string{
	"OpSwap",      // swap arithmetic operator: +<->-, *<->/
	"CmpSwap",     // swap comparison operator: < <-> <= etc.
	"OffByOne",    // integer literal inside a for statement: n -> n+1
	"DropDefer",   // remove a defer statement
	"CondNegate",  // negate an if condition
	"ConstChange", // change a const/var literal initializer
	"ReturnSwap",  // swap the first two results of a multi-value return
	"ChanOp",      // turn a channel send into a receive (wrong channel op)
	"SliceBound",  // shrink a slice high bound by one
}

// Mutation is one generated mutant.
type Mutation struct {
	Class       string
	Description string
	Src         []byte
}

// Apply parses src and applies one mutation of the given class.
func Apply(class string, src []byte, filename string) (*Mutation, error) {
	valid := false
	for _, c := range Classes {
		if c == class {
			valid = true
		}
	}
	if !valid {
		return nil, fmt.Errorf("unknown mutation class %q (known: %v)", class, Classes)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	m := &mutator{fset: fset}
	var desc string
	var ok bool
	switch class {
	case "OpSwap":
		desc, ok = m.swapOp(f, arithPairs)
	case "CmpSwap":
		desc, ok = m.swapOp(f, cmpPairs)
	case "OffByOne":
		desc, ok = m.offByOne(f)
	case "DropDefer":
		desc, ok = m.dropDefer(f)
	case "CondNegate":
		desc, ok = m.condNegate(f)
	case "ConstChange":
		desc, ok = m.constChange(f)
	case "ReturnSwap":
		desc, ok = m.returnSwap(f)
	case "ChanOp":
		desc, ok = m.chanOp(f)
	case "SliceBound":
		desc, ok = m.sliceBound(f)
	}
	if !ok {
		return nil, fmt.Errorf("class %s: no eligible site in %s", class, filename)
	}
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, f); err != nil {
		return nil, fmt.Errorf("print mutated source: %w", err)
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format mutated source: %w", err)
	}
	return &Mutation{Class: class, Description: desc, Src: formatted}, nil
}

type mutator struct {
	fset *token.FileSet
}

func (m *mutator) at(p token.Pos) string {
	return m.fset.Position(p).String()
}

var arithPairs = map[token.Token]token.Token{
	token.ADD: token.SUB,
	token.SUB: token.ADD,
	token.MUL: token.QUO,
	token.QUO: token.MUL,
}

var cmpPairs = map[token.Token]token.Token{
	token.LSS: token.LEQ,
	token.LEQ: token.LSS,
	token.GTR: token.GEQ,
	token.GEQ: token.GTR,
	token.EQL: token.NEQ,
	token.NEQ: token.EQL,
}

// swapOp replaces the operator of the first binary expression whose
// operator is in pairs.
func (m *mutator) swapOp(f *ast.File, pairs map[token.Token]token.Token) (string, bool) {
	var desc string
	done := false
	ast.Inspect(f, func(n ast.Node) bool {
		if done {
			return false
		}
		if be, ok := n.(*ast.BinaryExpr); ok {
			if to, ok := pairs[be.Op]; ok {
				from := be.Op
				be.Op = to
				desc = fmt.Sprintf("swapped operator %q -> %q at %s", from, to, m.at(be.OpPos))
				done = true
				return false
			}
		}
		return !done
	})
	return desc, done
}

// offByOne increments the first integer literal found in a for statement's
// condition or post statement (loop bounds / step counts).
func (m *mutator) offByOne(f *ast.File) (string, bool) {
	var desc string
	done := false
	ast.Inspect(f, func(n ast.Node) bool {
		if done {
			return false
		}
		fs, ok := n.(*ast.ForStmt)
		if !ok {
			return true
		}
		for _, sub := range []ast.Node{fs.Cond, fs.Post} {
			if sub == nil {
				continue
			}
			ast.Inspect(sub, func(n2 ast.Node) bool {
				if done {
					return false
				}
				if lit, ok := n2.(*ast.BasicLit); ok && lit.Kind == token.INT {
					v, err := strconv.Atoi(lit.Value)
					if err != nil {
						return true
					}
					lit.Value = strconv.Itoa(v + 1)
					desc = fmt.Sprintf("off-by-one: int literal %d -> %d at %s", v, v+1, m.at(lit.Pos()))
					done = true
					return false
				}
				return !done
			})
			if done {
				return false
			}
		}
		return !done
	})
	return desc, done
}

// dropDefer removes the first defer statement found in any block.
func (m *mutator) dropDefer(f *ast.File) (string, bool) {
	var desc string
	done := false
	ast.Inspect(f, func(n ast.Node) bool {
		if done {
			return false
		}
		blk, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for i, s := range blk.List {
			if ds, ok := s.(*ast.DeferStmt); ok {
				call := "defer"
				if ds.Call != nil {
					var buf bytes.Buffer
					printer.Fprint(&buf, m.fset, ds.Call.Fun)
					call = "defer " + buf.String() + "(...)"
				}
				blk.List = append(blk.List[:i], blk.List[i+1:]...)
				desc = fmt.Sprintf("dropped %s at %s", call, m.at(ds.Pos()))
				done = true
				return false
			}
		}
		return !done
	})
	return desc, done
}

// condNegate wraps the first if condition in !(...).
func (m *mutator) condNegate(f *ast.File) (string, bool) {
	var desc string
	done := false
	ast.Inspect(f, func(n ast.Node) bool {
		if done {
			return false
		}
		if is, ok := n.(*ast.IfStmt); ok {
			pos := m.at(is.Cond.Pos())
			is.Cond = &ast.UnaryExpr{OpPos: is.Cond.Pos(), Op: token.NOT, X: is.Cond}
			desc = fmt.Sprintf("negated if condition at %s", pos)
			done = true
			return false
		}
		return !done
	})
	return desc, done
}

// constChange alters the first string or int literal used as a const/var
// initializer: strings get a "!" suffix, ints grow by 100.
func (m *mutator) constChange(f *ast.File) (string, bool) {
	var desc string
	done := false
	ast.Inspect(f, func(n ast.Node) bool {
		if done {
			return false
		}
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Values) == 0 {
			return !done
		}
		for _, v := range vs.Values {
			lit, ok := v.(*ast.BasicLit)
			if !ok {
				continue
			}
			switch lit.Kind {
			case token.STRING:
				unq, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				lit.Value = strconv.Quote(unq + "!")
				desc = fmt.Sprintf("const string %q -> %q at %s", unq, unq+"!", m.at(lit.Pos()))
				done = true
				return false
			case token.INT:
				iv, err := strconv.Atoi(lit.Value)
				if err != nil {
					continue
				}
				lit.Value = strconv.Itoa(iv + 100)
				desc = fmt.Sprintf("const int %d -> %d at %s", iv, iv+100, m.at(lit.Pos()))
				done = true
				return false
			}
		}
		return !done
	})
	return desc, done
}

// returnSwap swaps the first two results of the first multi-value return.
func (m *mutator) returnSwap(f *ast.File) (string, bool) {
	var desc string
	done := false
	ast.Inspect(f, func(n ast.Node) bool {
		if done {
			return false
		}
		if rs, ok := n.(*ast.ReturnStmt); ok && len(rs.Results) >= 2 {
			rs.Results[0], rs.Results[1] = rs.Results[1], rs.Results[0]
			desc = fmt.Sprintf("swapped first two return values at %s", m.at(rs.Pos()))
			done = true
			return false
		}
		return !done
	})
	return desc, done
}

// chanOp turns the first channel send statement (ch <- v) into a bare
// receive (<-ch), a classic wrong-channel-op that typically deadlocks.
// The send's value expression is kept alive as `_ = <expr>` inside a nested
// block: without it, replacing the send can leave a variable (e.g. a range
// loop variable used only by the send) unused, and the mutant would fail to
// compile — a manifest bug, not a harness finding.
func (m *mutator) chanOp(f *ast.File) (string, bool) {
	var desc string
	done := false
	ast.Inspect(f, func(n ast.Node) bool {
		if done {
			return false
		}
		blk, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for i, s := range blk.List {
			if ss, ok := s.(*ast.SendStmt); ok {
				blank := &ast.Ident{Name: "_"}
				keepUsed := &ast.AssignStmt{
					Lhs: []ast.Expr{blank},
					Tok: token.ASSIGN,
					Rhs: []ast.Expr{ss.Value},
				}
				recv := &ast.ExprStmt{
					X: &ast.UnaryExpr{Op: token.ARROW, X: ss.Chan},
				}
				blk.List[i] = &ast.BlockStmt{List: []ast.Stmt{keepUsed, recv}}
				desc = fmt.Sprintf("channel send -> { _ = <expr>; <-ch } at %s", m.at(ss.Pos()))
				done = true
				return false
			}
		}
		return !done
	})
	return desc, done
}

// sliceBound shrinks the high bound of the first slice expression by one:
// s[a:b] -> s[a:b-1].
func (m *mutator) sliceBound(f *ast.File) (string, bool) {
	var desc string
	done := false
	ast.Inspect(f, func(n ast.Node) bool {
		if done {
			return false
		}
		if se, ok := n.(*ast.SliceExpr); ok && se.High != nil {
			se.High = &ast.BinaryExpr{
				X:     se.High,
				Op:    token.SUB,
				Y:     &ast.BasicLit{Kind: token.INT, Value: "1"},
				OpPos: se.High.End(),
			}
			desc = fmt.Sprintf("slice high bound -1 at %s", m.at(se.Pos()))
			done = true
			return false
		}
		return !done
	})
	return desc, done
}
