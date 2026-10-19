package main

// M10 (WS I): the post-M9 provided surface and need set include the
// embedded synthetic stdlib packages. Two registries are read
// mechanically from the graalgo tree every run — never hand-maintained:
//
//   - internal/frontend/synth_machinery.go's syntheticSources map
//     (import path -> embedded-source const expression): the sources
//     themselves, resolved through const string fragments, '+'
//     concatenations, and parenthesized forms.
//   - internal/frontend/export.go's overlayImporter.Import switch:
//     the authoritative set of import paths the overlay substitutes,
//     i.e. the real-stdlib files shadowed by synthetic packages.
//
// A vendored-tree linkname whose file's import path is shadowed is still
// scanned (the P3-M6 population is preserved for the SPEC §3.6
// skip-list-growth comparison) but marked ShadowedBy, so the report can
// split the skip-list into the comparable total and the live subset
// (targets referenced by code graalgo actually compiles: non-shadowed
// stdlib files plus the synthetic sources).

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// synthRegistry is the resolved synthetic-package registry.
type synthRegistry struct {
	// Sources maps import path -> full embedded Go source.
	Sources map[string]string
	// ConstOf maps import path -> primary const name (provenance).
	ConstOf map[string]string
	// FileOf maps import path -> frontend file declaring the const.
	FileOf map[string]string
	// Shadowed is the set of import paths substituted by the overlay
	// importer: their real-stdlib files are never compiled by graalgo.
	Shadowed map[string]bool
}

var synthRegCache = map[string]*synthRegistry{}

// frontendDir is internal/frontend under the graalgo root.
func frontendDir(graalgo string) string {
	return filepath.Join(graalgo, "internal", "frontend")
}

// loadSyntheticRegistry resolves the synthetic registry for the graalgo
// tree, cached per directory. It fails loudly on drift (missing map,
// unresolvable const, empty result) rather than silently under-scanning.
func loadSyntheticRegistry(graalgo string) (*synthRegistry, error) {
	dir := frontendDir(graalgo)
	if c, ok := synthRegCache[dir]; ok {
		return c, nil
	}
	reg, err := buildSyntheticRegistry(dir)
	if err != nil {
		return nil, err
	}
	synthRegCache[dir] = reg
	return reg, nil
}

func buildSyntheticRegistry(dir string) (*synthRegistry, error) {
	reg := &synthRegistry{
		Sources:  map[string]string{},
		ConstOf:  map[string]string{},
		FileOf:   map[string]string{},
		Shadowed: map[string]bool{},
	}
	mapVals, err := parseSyntheticSourcesMap(dir)
	if err != nil {
		return nil, err
	}
	frags, fragFiles, err := collectConstStrings(dir)
	if err != nil {
		return nil, err
	}
	strs := resolveAllConsts(frags)
	for path, vexpr := range mapVals {
		src, ok := resolveConstExprStr(vexpr, strs)
		if !ok {
			return nil, fmt.Errorf("syntheticSources[%q]: cannot resolve source expression (source drift?)", path)
		}
		reg.Sources[path] = src
		reg.ConstOf[path] = primaryIdent(vexpr)
		reg.FileOf[path] = fragFiles[primaryIdent(vexpr)]
	}
	if len(reg.Sources) == 0 {
		return nil, fmt.Errorf("syntheticSources map: no entries resolved (source drift?)")
	}
	shadowed, err := overlayShadowed(dir, reg.Sources)
	if err != nil {
		return nil, err
	}
	reg.Shadowed = shadowed
	return reg, nil
}

// parseSyntheticSourcesMap parses synth_machinery.go and returns the
// syntheticSources map literal: import path -> value expression.
func parseSyntheticSourcesMap(dir string) (map[string]ast.Expr, error) {
	path := filepath.Join(dir, "synth_machinery.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := map[string]ast.Expr{}
	found := false
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "syntheticSources" ||
				len(vs.Values) != 1 {
				continue
			}
			lit, ok := vs.Values[0].(*ast.CompositeLit)
			if !ok {
				return nil, fmt.Errorf("%s: syntheticSources is not a map literal", path)
			}
			found = true
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.BasicLit)
				if !ok || key.Kind != token.STRING {
					continue
				}
				p, err := strconv.Unquote(key.Value)
				if err != nil {
					return nil, fmt.Errorf("%s: bad map key: %w", path, err)
				}
				out[p] = kv.Value
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("%s: syntheticSources map not found (source drift?)", path)
	}
	return out, nil
}

// collectConstStrings gathers every `const Name = <string expr>` from
// the synth_*.go files: const name -> value expression, plus the
// declaring file (base name) for provenance.
func collectConstStrings(dir string) (map[string]ast.Expr, map[string]string, error) {
	frags := map[string]ast.Expr{}
	files := map[string]string{}
	matches, err := filepath.Glob(filepath.Join(dir, "synth_*.go"))
	if err != nil {
		return nil, nil, err
	}
	for _, path := range matches {
		// Skip test files: their consts are fixtures, not sources.
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", path, err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
					continue
				}
				name := vs.Names[0].Name
				if !strings.HasPrefix(name, "synthetic") {
					continue
				}
				frags[name] = vs.Values[0]
				files[name] = filepath.Base(path)
			}
		}
	}
	return frags, files, nil
}

// resolveAllConsts resolves every collected const expression to its
// string value by fixpoint iteration (fragments reference each other
// across files, e.g. syntheticOsPart1..7 -> syntheticOsSource).
func resolveAllConsts(frags map[string]ast.Expr) map[string]string {
	strs := map[string]string{}
	// Seed: consts whose expression is directly a string literal.
	for name, expr := range frags {
		if s, ok := resolveConstExprStr(expr, strs); ok {
			strs[name] = s
		}
	}
	// Fixpoint: resolve the rest through already-resolved idents.
	for {
		progress := false
		for name, expr := range frags {
			if _, ok := strs[name]; ok {
				continue
			}
			if s, ok := resolveConstExprStr(expr, strs); ok {
				strs[name] = s
				progress = true
			}
		}
		if !progress {
			break
		}
	}
	return strs
}

// resolveConstExprStr evaluates a const string expression: string
// literals (raw or interpreted), identifiers (via strs), '+'
// concatenations, and parenthesized forms. ok=false when an identifier
// is not yet resolved.
func resolveConstExprStr(expr ast.Expr, strs map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.Ident:
		s, ok := strs[e.Name]
		return s, ok
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		l, ok := resolveConstExprStr(e.X, strs)
		if !ok {
			return "", false
		}
		r, ok := resolveConstExprStr(e.Y, strs)
		if !ok {
			return "", false
		}
		return l + r, true
	case *ast.ParenExpr:
		return resolveConstExprStr(e.X, strs)
	}
	return "", false
}

// primaryIdent returns the first identifier of a source expression
// (for provenance): the ident itself, or the leftmost ident of a '+'
// chain.
func primaryIdent(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			return primaryIdent(e.X)
		}
	case *ast.ParenExpr:
		return primaryIdent(e.X)
	}
	return ""
}

// overlayShadowed returns every import path the overlay substitutes —
// whose real-stdlib files graalgo never compiles. overlayImporter.Import
// substitutes two classes of packages: the dedicated synthetic stubs
// (the dedicatedSyntheticStubs map in export.go) and every syntheticSources
// registry entry (resolved via getSyntheticPackage before disk). The set
// is therefore the union of the stub-map keys and the registry keys.
func overlayShadowed(dir string, syntheticPaths map[string]string) (map[string]bool, error) {
	out := map[string]bool{}
	for p := range syntheticPaths {
		out[p] = true
	}
	path := filepath.Join(dir, "export.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	found := false
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "dedicatedSyntheticStubs" {
				continue
			}
			if len(vs.Values) != 1 {
				continue
			}
			cl, ok := vs.Values[0].(*ast.CompositeLit)
			if !ok {
				continue
			}
			found = true
			for _, elt := range cl.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				lit, ok := kv.Key.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				out[s] = true
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("%s: dedicatedSyntheticStubs not found (source drift?)", path)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no shadowed packages resolved (source drift?)", path)
	}
	return out, nil
}

// sortedKeys returns the map's keys in sorted order (deterministic
// reports).
func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// extractSyntheticProvided adds every embedded synthetic package's
// exported symbols, qualified by import path (the qualifier linkname
// targets use), to the provided set. A source that does not parse is
// recorded on Skipped — loudly in the report — and does not abort the
// extraction (one broken synthetic package must not block the diff;
// cf. ScanResult.ParseErrors, same precedent).
func (p *ProvidedSet) extractSyntheticProvided(reg *synthRegistry) error {
	for _, path := range sortedKeys(reg.Sources) {
		prov := fmt.Sprintf("synthetic %q: internal/frontend/%s %s",
			path, reg.FileOf[path], reg.ConstOf[path])
		if err := p.extractGoSourceAs(path, reg.Sources[path], prov); err != nil {
			p.Skipped = append(p.Skipped,
				fmt.Sprintf("synthetic %q: %s", path, err))
		}
	}
	return nil
}

// scanSyntheticLinknames extracts //go:linkname directives from every
// embedded synthetic source (the exporter's linkname scan covers them —
// synth_machinery.go buildSyntheticPackage parses with comments — so
// they are real needs). File is "synthetic/<importPath>"; Package is the
// import path, the qualifier linkname targets use. It also returns each
// source's top-level declared names (gc's package-scope local-name rule).
// Unparseable sources are reported via skipped, not fatal (same precedent
// as extractSyntheticProvided).
func scanSyntheticLinknames(reg *synthRegistry) (out []Linkname, decls map[string][]string, skipped []string) {
	decls = map[string][]string{}
	for _, path := range sortedKeys(reg.Sources) {
		src := reg.Sources[path]
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "synthetic/"+path+".go", src, parser.ParseComments)
		if err != nil {
			skipped = append(skipped,
				fmt.Sprintf("synthetic %q: %s", path, err))
			continue
		}
		out = append(out, linknamesInDecls(f.Decls, "synthetic/"+path, path)...)
		decls["synthetic/"+path] = declaredNames(f)
	}
	return out, decls, skipped
}

// importPathOfRel maps a tree-relative file path to its import path:
// the slash-separated directory, with the cmd/vendor/ prefix stripped
// (vendored golang.org/x/... packages import as golang.org/x/...).
func importPathOfRel(rel string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." {
		return ""
	}
	if strings.HasPrefix(dir, "cmd/vendor/") {
		return strings.TrimPrefix(dir, "cmd/vendor/")
	}
	return dir
}

// markShadowed records, on each scanned linkname, the synthetic import
// path whose package supersedes the file's real-stdlib package (""
// when the file is live). Synthetic-source entries (File
// "synthetic/...") are always live: they are the code graalgo compiles.
func markShadowed(scan *ScanResult, shadowed map[string]bool) {
	for i := range scan.Linknames {
		ln := &scan.Linknames[i]
		if strings.HasPrefix(ln.File, "synthetic/") {
			continue
		}
		if ip := importPathOfRel(ln.File); ip != "" && shadowed[ip] {
			ln.ShadowedBy = ip
		}
	}
}

// targetPackage returns the package qualifier of a normalized linkname
// target for grouping: the import path for pkg.sym, the receiver-less
// prefix for pkg.(*T).M / pkg.T.M method targets.
func targetPackage(t string) string {
	if i := strings.Index(t, ".("); i >= 0 {
		return t[:i]
	}
	rest := t
	if i := strings.Index(t, "."); i >= 0 {
		// Value-receiver method form pkg.T.M: the type segment starts
		// uppercase and is followed by another dot-segment.
		seg := t[i+1:]
		if j := strings.Index(seg, "."); j >= 0 {
			if typ := seg[:j]; typ != "" && isExported(typ) {
				return t[:i]
			}
		}
		rest = t
	}
	if i := strings.LastIndex(rest, "."); i >= 0 {
		return rest[:i]
	}
	return t
}
