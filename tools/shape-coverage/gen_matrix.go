// gen_matrix.go — R2-P1-3 shape-coverage matrix generator (read-only).
// synth_*.go files embed the synthetic package as raw-string literal(s)
// (const syntheticXSource = `package ...`, or syntheticOsPart1..7
// concatenated, or a copyright comment before `package`). This program
// extracts each embedded source, parses it with go/ast, finds every
// panic("... not in M[5789] scope ...") call, attributes it to its
// enclosing FuncDecl (line numbers mapped back to the original synth
// file), and joins it against the pair tables in
// internal/frontend/mrdesugar_m5.go. It also scans the outer file AST
// for plain-code panics. Emits TSV on stdout.
// Usage: go run gen_matrix.go [repo-root]
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var scopeRe = regexp.MustCompile(`not in (M[0-9]+) scope`)
var msgNameRe = regexp.MustCompile(`^(.+?)(?::)? not in M[0-9]+ scope`)

type site struct {
	file, msg, scope, msgPkg, msgName string
	line                              int
	recv, funcName                    string
	nResults                          int
	inFuncLit                         bool
}

func main() {
	root := "/home/hatch/workspace/graalgo"
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	fset := token.NewFileSet()
	all, _ := filepath.Glob(filepath.Join(root, "internal/frontend/synth_*.go"))
	sort.Strings(all)
	panicLine := regexp.MustCompile(`panic\(".*not in M[5789] scope`)
	var files []string
	for _, f := range all {
		b, err := os.ReadFile(f)
		if err == nil && panicLine.Match(b) {
			files = append(files, f)
		}
	}
	var sites []site
	for _, f := range files {
		fnode, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse %s: %v\n", f, err)
			os.Exit(1)
		}
		base := filepath.Base(f)
		// Outer-file scan (plain-code panics, e.g. none expected).
		sites = append(sites, scanNode(fset, fnode, base, 0, nil)...)
		// Embedded sources: raw-string literals assigned to consts/vars
		// named synthetic*. Group by name stem (trailing digits stripped)
		// so syntheticOsPart1..7 concatenate into one source.
		type part struct {
			content   string
			startLine int
		}
		groups := map[string][]part{}
		var order []string
		ast.Inspect(fnode, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			if len(vs.Names) == 0 || len(vs.Values) == 0 {
				return true
			}
			name := vs.Names[0].Name
			if !strings.HasPrefix(name, "synthetic") {
				return true
			}
			lit, ok := vs.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || len(lit.Value) == 0 || lit.Value[0] != '`' {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			stem := strings.TrimRight(name, "0123456789")
			if _, ok := groups[stem]; !ok {
				order = append(order, stem)
			}
			pos := fset.Position(lit.Pos())
			groups[stem] = append(groups[stem], part{content: s, startLine: pos.Line})
			return true
		})
		for _, stem := range order {
			parts := groups[stem]
			if !isPackageSource(parts[0].content) {
				continue
			}
			// Concatenate parts; build concat-line -> orig-line map.
			var sb strings.Builder
			var lineMap []int // index = concat line-1 -> orig line
			for i, p := range parts {
				if i > 0 {
					// If previous part did not end with \n, this part
					// continues the same concat line.
					if len(lineMap) > 0 && !strings.HasSuffix(parts[i-1].content, "\n") {
						// merge: drop the last mapping entry's line break
						// by keeping lineMap as-is; the first line of this
						// part maps onto the last concat line.
						_ = lineMap
					}
				}
				lines := strings.Split(p.content, "\n")
				startConcat := len(lineMap) + 1
				_ = startConcat
				for j := range lines {
					// line j of this part (0-based) is orig line startLine+j.
					orig := p.startLine + j
					// Phantom trailing element when content ends with "\n":
					// not a real line; the next part starts on it.
					if j == len(lines)-1 && lines[j] == "" && strings.HasSuffix(p.content, "\n") {
						continue
					}
					if i > 0 && j == 0 && !strings.HasSuffix(parts[i-1].content, "\n") {
						if strings.HasPrefix(p.content, "\n") {
							// Leading "\n": a new blank line whose orig
							// line is the opening-backtick line.
							lineMap = append(lineMap, orig)
						}
						// else: continuation of the previous part's last
						// line; keep that line's mapping, add nothing.
						continue
					}
					lineMap = append(lineMap, orig)
				}
				sb.WriteString(p.content)
			}
			// Trailing empty element from a final "\n" adds a phantom
			// line; harmless (no code there).
			src := sb.String()
			efset := token.NewFileSet()
			enode, err := parser.ParseFile(efset, base, src, 0)
			if err != nil {
				fmt.Fprintf(os.Stderr, "WARN parse embedded %s (%s): %v\n", base, stem, err)
				continue
			}
			raw := scanNode(efset, enode, base, 0, nil)
			for _, s := range raw {
				if s.line-1 < len(lineMap) {
					s.line = lineMap[s.line-1]
				}
				sites = append(sites, s)
			}
		}
	}
	tables := parseTables(filepath.Join(root, "internal/frontend/mrdesugar_m5.go"))
	fmt.Println("file\tline\tscope\tmsgPkg\tmsgName\trecv\tfuncName\tnResults\tinFuncLit\ttableMatch\tdirect\tmethodValue\tifaceDispatch\treflect\tmsg")
	for _, s := range sites {
		tm := matchTable(tables, s)
		direct := "PANICS"
		if tm != "" && !strings.HasPrefix(tm, "iface:") {
			direct = "COVERED"
		}
		iface := "PROBE-OWED(PANICS?)"
		if strings.HasPrefix(tm, "iface:") {
			iface = "COVERED"
		}
		fmt.Printf("%s\t%d\t%s\t%s\t%s\t%s\t%s\t%d\t%v\t%s\t%s\tPANICS\t%s\tPROBE-OWED\t%s\n",
			s.file, s.line, s.scope, s.msgPkg, s.msgName, s.recv, s.funcName, s.nResults, s.inFuncLit, tm, direct, iface, s.msg)
	}
	fmt.Fprintf(os.Stderr, "sites=%d\n", len(sites))
}

// isPackageSource reports whether raw-string content is a Go package
// source: optional leading whitespace, //-comments and blank lines,
// then `package `.
func isPackageSource(s string) bool {
	for _, ln := range strings.Split(s, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "//") {
			continue
		}
		return strings.HasPrefix(t, "package ")
	}
	return false
}

// scanNode finds scope-panic calls in one parsed file. lineBase/lineMap
// are unused here (mapping is applied by the caller); kept for clarity.
func scanNode(fset *token.FileSet, fnode *ast.File, base string, _ int, _ []int) []site {
	type decl struct {
		pos, end   token.Pos
		recv, name string
		nres       int
	}
	var decls []decl
	var litRanges [][2]token.Pos
	var sites []site
	ast.Inspect(fnode, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			recv := ""
			if x.Recv != nil && len(x.Recv.List) > 0 {
				recv = exprString(x.Recv.List[0].Type)
			}
			decls = append(decls, decl{pos: x.Pos(), end: x.End(), recv: recv, name: x.Name.Name, nres: countResults(x.Type)})
		case *ast.FuncLit:
			litRanges = append(litRanges, [2]token.Pos{x.Pos(), x.End()})
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "panic" && len(x.Args) > 0 {
				if lit, ok := x.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					s, err := strconv.Unquote(lit.Value)
					if err != nil {
						return true
					}
					m := scopeRe.FindStringSubmatch(s)
					if m == nil {
						return true
					}
					pos := fset.Position(x.Pos())
					recv, fname, nres := "", "<top-level>", 0
					for _, d := range decls {
						if x.Pos() >= d.pos && x.Pos() <= d.end {
							recv, fname, nres = d.recv, d.name, d.nres
						}
					}
					inLit := false
					for _, r := range litRanges {
						if x.Pos() >= r[0] && x.Pos() <= r[1] {
							inLit = true
						}
					}
					msgPkg, msgName := splitMsgName(s)
					sites = append(sites, site{
						file: base, line: pos.Line, msg: s, scope: m[1],
						msgPkg: msgPkg, msgName: msgName,
						recv: recv, funcName: fname, nResults: nres,
						inFuncLit: inLit,
					})
				}
			}
		}
		return true
	})
	return sites
}

func exprString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return "*" + exprString(x.X)
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprString(x.X) + "." + x.Sel.Name
	case *ast.IndexExpr:
		return exprString(x.X) + "[...]"
	case *ast.IndexListExpr:
		return exprString(x.X) + "[...]"
	default:
		return fmt.Sprintf("%T", e)
	}
}

func countResults(ft *ast.FuncType) int {
	if ft.Results == nil {
		return 0
	}
	n := 0
	for _, f := range ft.Results.List {
		if len(f.Names) > 0 {
			n += len(f.Names)
		} else {
			n++
		}
	}
	return n
}

func splitMsgName(s string) (string, string) {
	m := msgNameRe.FindStringSubmatch(s)
	if m == nil {
		return "", s
	}
	head := strings.TrimSpace(m[1])
	// Messages come in three shapes:
	//   "archive/tar.Next: not in ..."          -> pkg archive/tar, name Next
	//   "encoding/asn1: Marshal: not in ..."    -> pkg encoding/asn1, name Marshal
	//   "reflect: reflect.Type.FieldByName: ..." -> pkg reflect.Type, name FieldByName
	//   "gob: Register: type not in ..."        -> pkg gob, name Register
	// Strip a leading "<pkg>: " prefix first (pkg has no spaces/dots
	// beyond the path), then strip any trailing ": ..." fragment.
	var prefix string
	if i := strings.Index(head, ": "); i >= 0 {
		cand := head[:i]
		if !strings.Contains(cand, " ") && !strings.Contains(cand, ".") {
			prefix = cand
			head = strings.TrimSpace(head[i+2:])
		}
	}
	if i := strings.Index(head, ":"); i >= 0 {
		head = strings.TrimSpace(head[:i])
	}
	if i := strings.LastIndex(head, "."); i >= 0 {
		return head[:i], head[i+1:]
	}
	return prefix, head
}

var tableVarRe = regexp.MustCompile(`(?m)^var (m5StaticMREntries|m7MultiResultFuncs|m8MultiResultFuncs|osMultiResultFuncs|m9MultiResultFuncs|ioMultiResultFuncs|m8IfaceMultiResultMethods) = \[\]`)
var rowRe = regexp.MustCompile(`\{"((?:[^"\\]|\\.)*)",\s*"((?:[^"\\]|\\.)*)"(?:,\s*"((?:[^"\\]|\\.)*)")?`)

func parseTables(path string) map[string]string {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read tables: %v\n", err)
		os.Exit(1)
	}
	text := string(b)
	out := map[string]string{}
	locs := tableVarRe.FindAllStringSubmatchIndex(text, -1)
	for i, loc := range locs {
		name := text[loc[2]:loc[3]]
		start := loc[1]
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		seg := text[start:end]
		for _, m := range rowRe.FindAllStringSubmatch(seg, -1) {
			pkg, method, recv := m[1], m[2], m[3]
			key := pkg + "|" + method + "|" + recv
			if name == "m8IfaceMultiResultMethods" {
				key = pkg + "|" + method + "|IFACE"
			}
			if prev, ok := out[key]; ok {
				out[key] = prev + "+" + name
			} else {
				out[key] = name
			}
		}
	}
	return out
}

func matchTable(tables map[string]string, s site) string {
	recv := strings.TrimPrefix(s.recv, "*")
	if i := strings.Index(recv, "["); i >= 0 {
		recv = recv[:i]
	}
	if v, ok := tables[s.msgPkg+"|"+s.msgName+"|"+recv]; ok {
		return v
	}
	if v, ok := tables[s.msgPkg+"|"+s.msgName+"|"]; ok {
		return v
	}
	if v, ok := tables[s.msgPkg+"|"+s.msgName+"|IFACE"]; ok {
		return "iface:" + v
	}
	if s.msgPkg == "" {
		if v, ok := tables["|"+s.msgName+"|"+recv]; ok {
			return v
		}
	}
	return ""
}
