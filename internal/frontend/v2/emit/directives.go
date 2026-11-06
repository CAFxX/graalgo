// Directive rejection: //go:embed and //go:linkname are not supported.
//
// The v2 emitter never reads Doc comments, so these two directives were
// silently dropped: an embed var emitted as a plain vardecl with no
// values (the zero value instead of the embedded file content), and a
// linkname func emitted as an ordinary bodyless funcdecl (the linkname
// gone, the backend receiving a function with a null body and no way to
// resolve the symbol). Both are silent gc divergences on valid input
// (REVIEW-2026-11-06-12 V12-P1-7, same silent-success class as
// func init(), R-P1-3).
//
// Real embed/linkname support needs design (embed.FS plumbing,
// cross-package symbol resolution) and is a designed follow-up, not
// this change. Until then the emitter rejects both loudly, at the top
// of emitDocument, before any node is emitted — a rejected program
// produces no partial document.
//
// Detection uses the toolchain's own directive grammar
// (ast.ParseDirective, go1.27): a comment line is a directive only if
// it parses as "//tool:name args". That is the same interpretation gc
// applies, so a line the toolchain would act on is a line we reject,
// and prose merely mentioning a directive name (or a directive inside
// a block comment, which ParseDirective does not recognize) still
// emits. Only "go:embed" and "go:linkname" are rejected: other
// directives (//go:build, //go:generate, //go:noinline, ...) carry no
// program semantics the emitter drops, and must keep emitting.
//
// This scans file.Comments — every comment group in the file, not
// just declaration Doc fields — so a directive in any position is
// caught. It depends on the parser using parser.ParseComments, which
// stage 1 guarantees (parser.go); without it file.Comments is nil
// and the scan silently passes, so the dependency is stated here
// rather than hidden.
package emit

import (
	"fmt"
	"go/ast"
	"sort"
)

// unsupportedDirectives are the //go: directive names the emitter
// rejects loudly until they are implemented. Keyed on the directive
// name — the mechanism — never on a package, symbol, or test name.
var unsupportedDirectives = map[string]string{
	"embed":    "go:embed support needs design (embed.FS plumbing); the v2 emitter does not implement it",
	"linkname": "go:linkname support needs design (cross-package symbol resolution); the v2 emitter does not implement it",
}

// rejectUnsupportedDirectives scans every comment in every file of
// the package for //go:embed and //go:linkname directives and fails
// loudly on the first one found, with its source position. Files are
// visited in deterministic (sorted) order so the reported directive
// is stable run to run.
func (e *Emitter) rejectUnsupportedDirectives(pkg *ast.Package) error {
	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		file := pkg.Files[name]
		for _, cg := range file.Comments {
			for _, c := range cg.List {
				d, ok := ast.ParseDirective(c.Slash, c.Text)
				if !ok || d.Tool != "go" {
					continue
				}
				if reason, unsupported := unsupportedDirectives[d.Name]; unsupported {
					pos := e.fset.Position(c.Slash)
					return fmt.Errorf("emit: //go:%s is not supported (%s) at %s:%d:%d",
						d.Name, reason, fileBase(pos.Filename), pos.Line, pos.Column)
				}
			}
		}
	}
	return nil
}
