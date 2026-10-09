// Appendix-M transform for P3-M9 tier-5 crypto synthetic sources.
//
// For each multi-result func/method (from mrscan3 JSON), replaces the
// declaration with:
//   type __mr<Key>Pair struct { <f0> <T0>; ... }
//   func __mr<Key>[<tparams>](<recv,> <params>) __mr<Key>Pair { <body, returns rewritten> }
//   <original signature> { panic("<pkgpath>: <name>: multi-result desugar") }
//
// Return rewriting is AST-based and closure-aware: only ReturnStmts that
// belong directly to the FuncDecl (not to nested FuncLits) are rewritten.
//   - `return` (bare)            -> `return __mr<Key>Pair{<result names>}`
//   - `return a, b`              -> `return __mr<Key>Pair{a, b}`
//   - `return f(x)` (forwarding) -> left as-is, REPORTED for manual fix.
//
// Named result variables referenced in the body are declared as locals
// at the top of the helper (math/big precedent).
//
// Usage: transform <merged.go> <pkgpath> <members.json> <out.go> <out.table>
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strings"
)

type Result struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type Member struct {
	Pkg       string   `json:"pkg"`
	Recv      string   `json:"recv"`
	RecvVar   string   `json:"recvVar"`
	Name      string   `json:"name"`
	TypeParams string  `json:"typeParams"`
	Params    string   `json:"params"`
	Results   []Result `json:"results"`
	Start     int      `json:"start"`
	End       int      `json:"end"`
	HeaderEnd int      `json:"headerEnd"`
}

var identRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func main() {
	if len(os.Args) != 6 {
		fmt.Fprintln(os.Stderr, "usage: transform <merged.go> <pkgpath> <members.json> <out.go> <out.table>")
		os.Exit(1)
	}
	mergedPath, pkgPath, membersPath, outPath, tablePath := os.Args[1], os.Args[2], os.Args[3], os.Args[4], os.Args[5]
	srcBytes, err := os.ReadFile(mergedPath)
	if err != nil { panic(err) }
	src := string(srcBytes)
	mj, err := os.ReadFile(membersPath)
	if err != nil { panic(err) }
	var members []Member
	if err := json.Unmarshal(mj, &members); err != nil { panic(err) }

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, mergedPath, srcBytes, parser.SkipObjectResolution)
	if err != nil { panic(err) }

	// index FuncDecls by (recv,name) -> *ast.FuncDecl
	type key struct{ recv, name string }
	decls := map[key]*ast.FuncDecl{}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			recv := ""
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				rt := typeString(fd.Recv.List[0].Type)
				rt = strings.TrimPrefix(rt, "*")
				if i := strings.Index(rt, "["); i >= 0 { rt = rt[:i] }
				recv = rt
			}
			decls[key{recv, fd.Name.Name}] = fd
		}
	}

	sort.Slice(members, func(i, j int) bool { return members[i].Start > members[j].Start })

	var table []string
	var forwards []string
	seen := map[string]bool{}

	for _, m := range members {
		k := key{m.Recv, m.Name}
		fd, ok := decls[k]
		if !ok {
			fmt.Fprintf(os.Stderr, "no FuncDecl for %s.%s\n", m.Recv, m.Name)
			os.Exit(1)
		}
		ckey := m.Recv + "_" + m.Name
		if m.Recv == "" { ckey = m.Name }
		ckey = sanitize(ckey)
		if seen[ckey] {
			fmt.Fprintf(os.Stderr, "key collision: %s\n", ckey)
			os.Exit(1)
		}
		seen[ckey] = true
		helper := "__mr" + ckey
		pair := helper + "Pair"
		var fields []string
		for _, r := range m.Results { fields = append(fields, r.Name) }

		// helper signature (type params from mrscan3's AST extraction,
		// which handles nested brackets like [P Point[P]])
		header := strings.TrimSpace(src[m.Start:m.HeaderEnd])
		tparams := m.TypeParams
		params := m.Params // includes parens
		var helperSig string
		if m.Recv != "" {
			// receiver source text from the AST
			rf := fd.Recv.List[0]
			rvStart := fset.Position(rf.Pos()).Offset
			rvEnd := fset.Position(rf.End()).Offset
			rr := strings.TrimSpace(src[rvStart:rvEnd])
			inner := strings.TrimSpace(params[1 : len(params)-1])
			if inner == "" {
				helperSig = fmt.Sprintf("func %s%s(%s) %s", helper, tparams, rr, pair)
			} else {
				helperSig = fmt.Sprintf("func %s%s(%s, %s) %s", helper, tparams, rr, inner, pair)
			}
		} else {
			helperSig = fmt.Sprintf("func %s%s%s %s", helper, tparams, params, pair)
		}
		_ = header
		stubSig := strings.TrimSpace(src[m.Start:m.HeaderEnd])

		// find direct returns (not inside func literals) via AST
		type retEdit struct{ start, end int; text string }
		var edits []retEdit
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok { return false }
			if rs, ok := n.(*ast.ReturnStmt); ok {
				rsStart := fset.Position(rs.Pos()).Offset
				rsEnd := fset.Position(rs.End()).Offset
				var newText string
				if len(rs.Results) == 0 {
					newText = fmt.Sprintf("return %s{%s}", pair, strings.Join(fields, ", "))
				} else {
					if len(rs.Results) == 1 {
						if _, isCall := rs.Results[0].(*ast.CallExpr); isCall {
							// Forwarding return: rewrite to an N-target
							// assignment (which the desugar handles) plus
							// pair construction.
							//   return f(x)
							// becomes:
							//   __fw0, __fw1 := f(x)
							//   return __mr<Key>Pair{__fw0, __fw1}
							e0 := fset.Position(rs.Results[0].Pos()).Offset
							e1 := fset.Position(rs.Results[0].End()).Offset
							callText := strings.TrimSpace(src[e0:e1])
							var fws []string
							for i := range fields {
								fws = append(fws, fmt.Sprintf("__fw%d", i))
							}
							// Use a sentinel that the line-based applier
							// expands with proper indentation. We embed a
							// newline; the applier preserves indent.
							newText = fmt.Sprintf("%s := %s\nreturn %s{%s}",
								strings.Join(fws, ", "), callText, pair,
								strings.Join(fws, ", "))
							edits = append(edits, retEdit{rsStart, rsEnd, newText})
							return true
						}
					}
					var exprs []string
					for _, e := range rs.Results {
						e0 := fset.Position(e.Pos()).Offset
						e1 := fset.Position(e.End()).Offset
						exprs = append(exprs, strings.TrimSpace(src[e0:e1]))
					}
					newText = fmt.Sprintf("return %s{%s}", pair, strings.Join(exprs, ", "))
				}
				edits = append(edits, retEdit{rsStart, rsEnd, newText})
			}
			return true
		})

		// apply return edits to body (reverse order)
		body := src[m.HeaderEnd+1 : m.End-1]
		bodyStart := m.HeaderEnd + 1
		sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
		for _, e := range edits {
			// preserve leading indentation on every emitted line
			ls := strings.LastIndex(src[:e.start], "\n") + 1
			indent := src[ls:e.start]
			if strings.TrimSpace(indent) != "" { indent = "" }
			lines := strings.Split(e.text, "\n")
			// The body prefix already ends with the original
			// indentation, so only indent continuation lines.
			for i := 1; i < len(lines); i++ {
				lines[i] = indent + lines[i]
			}
			body = body[:e.start-bodyStart] + strings.Join(lines, "\n") + body[e.end-bodyStart:]
		}

		// declare named-result locals used in body (as variables, not
		// field selectors — manual '.' check since Go regexp lacks
		// lookbehind)
		bodyNC := regexp.MustCompile(`//[^\n]*`).ReplaceAllString(body, "")
		bodyNC = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(bodyNC, "")
		var varDecls []string
		for _, r := range m.Results {
			re := regexp.MustCompile(`\b` + regexp.QuoteMeta(r.Name) + `\b`)
			used := false
			for _, loc := range re.FindAllStringIndex(bodyNC, -1) {
				if loc[0] == 0 || bodyNC[loc[0]-1] != '.' {
					used = true
					break
				}
			}
			if used {
				varDecls = append(varDecls, fmt.Sprintf("\tvar %s %s", r.Name, r.Type))
			}
		}
		if len(varDecls) > 0 {
			body = "\n" + strings.Join(varDecls, "\n") + "\n" + body
		}

		pairDecl := fmt.Sprintf("type %s struct {\n%s\n}", pair,
			strings.Join(map2(m.Results, func(r Result) string { return fmt.Sprintf("\t%s %s", r.Name, r.Type) }), "\n"))
		helperDecl := fmt.Sprintf("%s {%s}", helperSig, body)
		stubDecl := fmt.Sprintf("%s {\n\tpanic(\"%s: %s: multi-result desugar\")\n}", stubSig, pkgPath, m.Name)
		replacement := pairDecl + "\n\n" + helperDecl + "\n\n" + stubDecl
		src = src[:m.Start] + replacement + src[m.End:]

		table = append(table, fmt.Sprintf("{\"%s\", \"%s\", \"%s\", \"%s\", \"\", \"%s\", []string{%s}},",
			pkgPath, m.Name, m.Recv, helper, pair,
			strings.Join(map2(fields, func(s string) string { return `"` + s + `"` }), ", ")))
	}

	sort.Strings(table)
	os.WriteFile(outPath, []byte(src), 0644)
	os.WriteFile(tablePath, []byte(strings.Join(table, "\n")+"\n"), 0644)
	fmt.Printf("wrote %s (%d members), %s (%d entries)\n", outPath, len(members), tablePath, len(table))
	if len(forwards) > 0 {
		fmt.Printf("FORWARDING RETURNS needing manual fix (%d):\n", len(forwards))
		for _, fw := range forwards {
			fmt.Printf("  %s\n", fw)
		}
	}
}

func map2[T any](s []T, f func(T) string) []string {
	var out []string
	for _, x := range s { out = append(out, f(x)) }
	return out
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func typeString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.SelectorExpr:
		return typeString(t.X) + "." + t.Sel.Name
	case *ast.IndexExpr:
		return typeString(t.X) + "[" + typeString(t.Index) + "]"
	case *ast.IndexListExpr:
		var a []string
		for _, x := range t.Indices { a = append(a, typeString(x)) }
		return typeString(t.X) + "[" + strings.Join(a, ", ") + "]"
	default:
		return "?"
	}
}
