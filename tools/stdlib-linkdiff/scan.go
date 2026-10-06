package main

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Linkname is one //go:linkname directive found in the scanned tree.
type Linkname struct {
	File      string // path relative to the tree root, slash-separated
	Package   string // package clause of the file
	Local     string // fields[0] (the local name)
	Declared  string // identifier the directive is attached to
	Target    string // raw target symbol (fields[1]); "" for self-links
	SelfLink  bool   // single-field form: target is Package.Local
	Malformed string // non-empty when the directive is malformed (never dropped)
}

// ScanResult is the outcome of scanning one Go source tree.
type ScanResult struct {
	Root               string
	Goos               string
	Goarch             string
	Linknames          []Linkname
	FilesScanned       int
	FilesWithLinknames int
	TestFilesExcluded  int
	// ConstraintExcluded counts non-test .go files rejected by
	// build-constraint/filename filtering for the target platform.
	ConstraintExcluded int
	// ExcludedByConstraint lists a sample of constraint-excluded files
	// (capped) so the filter's effect stays auditable.
	ExcludedByConstraint []string
	ParseErrors          []string
}

// ScanTree walks root, selects .go files for (goos, goarch) with
// go/build.MatchFile (build tags + filename suffixes), and extracts
// every //go:linkname directive. _test.go files are excluded from the
// production contract (counted, never scanned). Directories named
// "testdata" are skipped (not part of any build).
//
// tagFilter, when non-nil, replaces the platform filter entirely: it is
// the testing hook that proves the filter has an effect (a nil filter
// means "use the real go/build platform filter").
func ScanTree(root, goos, goarch string, extraTags []string) (*ScanResult, error) {
	return scanTree(root, goos, goarch, extraTags, nil)
}

func scanTree(root, goos, goarch string, extraTags []string,
	tagFilter func(dir, name string) (bool, error)) (*ScanResult, error) {
	ctxt := build.Default
	ctxt.GOOS = goos
	ctxt.GOARCH = goarch
	ctxt.BuildTags = append([]string(nil), extraTags...)

	res := &ScanResult{Root: root, Goos: goos, Goarch: goarch}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if base == "testdata" || strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			res.TestFilesExcluded++
			return nil
		}
		dir := filepath.Dir(path)
		var match bool
		if tagFilter != nil {
			var ferr error
			match, ferr = tagFilter(dir, d.Name())
			if ferr != nil {
				return ferr
			}
		} else {
			var merr error
			match, merr = ctxt.MatchFile(dir, d.Name())
			if merr != nil {
				return fmt.Errorf("constraint check %s: %w", path, merr)
			}
		}
		if !match {
			res.ConstraintExcluded++
			if len(res.ExcludedByConstraint) < 50 {
				rel, _ := filepath.Rel(root, path)
				res.ExcludedByConstraint = append(res.ExcludedByConstraint, filepath.ToSlash(rel))
			}
			return nil
		}
		res.FilesScanned++
		lns, perr := extractLinknames(root, path)
		if perr != nil {
			rel, _ := filepath.Rel(root, path)
			res.ParseErrors = append(res.ParseErrors,
				filepath.ToSlash(rel)+": "+perr.Error())
			return nil
		}
		if len(lns) > 0 {
			res.FilesWithLinknames++
			res.Linknames = append(res.Linknames, lns...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// extractLinknames parses one file and returns its //go:linkname
// directives. A directive is recognized only as a `//go:linkname`
// line comment (exactly that prefix — `//go:linknamestd` and friends
// are not directives) attached as documentation to a top-level
// declaration. Malformed directives are returned with an empty target
// and a reason recorded in Target==""; they are never dropped.
func extractLinknames(root, path string) ([]Linkname, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	var out []Linkname
	emit := func(doc *ast.CommentGroup, declared string) {
		for _, ln := range linknamesInDoc(doc, rel, f.Name.Name, declared) {
			out = append(out, ln)
		}
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			emit(d.Doc, d.Name.Name)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				var name string
				var sdoc *ast.CommentGroup
				switch s := spec.(type) {
				case *ast.ValueSpec:
					if len(s.Names) > 0 {
						name = s.Names[0].Name
					}
					sdoc = s.Doc
				case *ast.TypeSpec:
					name = s.Name.Name
					sdoc = s.Doc
				}
				if sdoc != nil {
					for _, ln := range linknamesInDoc(sdoc, rel, f.Name.Name, name) {
						out = append(out, ln)
					}
				} else if d.Doc != nil && (d.Tok == token.VAR || d.Tok == token.CONST) {
					// A doc comment on a parenthesized var/const block
					// applies to the first spec only (gc convention for
					// the directive is per-declaration docs; the block
					// form is vanishingly rare for linkname).
					if spec == d.Specs[0] {
						for _, ln := range linknamesInDoc(d.Doc, rel, f.Name.Name, name) {
							out = append(out, ln)
						}
					}
				}
			}
		}
	}
	return out, nil
}

// linknamesInDoc extracts //go:linkname directives from one doc comment.
func linknamesInDoc(doc *ast.CommentGroup, file, pkg, declared string) []Linkname {
	if doc == nil {
		return nil
	}
	var out []Linkname
	for _, c := range doc.List {
		for _, line := range strings.Split(c.Text, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "//go:linkname") {
				continue
			}
			rest := line[len("//go:linkname"):]
			if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
				// //go:linknamestd etc. are not directives.
				continue
			}
			fields := strings.Fields(rest)
			ln := Linkname{File: file, Package: pkg, Declared: declared}
			switch len(fields) {
			case 0:
				ln.Local = declared
				ln.Malformed = "directive has no fields (want 1 or 2)"
			case 1:
				ln.Local = fields[0]
				ln.SelfLink = true
			case 2:
				ln.Local = fields[0]
				ln.Target = fields[1]
			default:
				// Malformed: keep the entry so it is never silently
				// dropped; classification marks it MISSING with reason.
				ln.Local = fields[0]
				ln.Malformed = fmt.Sprintf("directive has %d fields (want 1 or 2)", len(fields))
			}
			out = append(out, ln)
		}
	}
	return out
}
