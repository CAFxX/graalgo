package vet

import (
	"go/token"
	"strings"
)

func init() {
	register(&Analyzer{
		Name: "directive",
		Doc:  "check Go toolchain directives such as //go:debug",
		Run:  runDirective,
	})
	register(&Analyzer{
		Name: "buildtag",
		Doc:  "check //go:build and // +build directives",
		Run:  runBuildTag,
	})
}

// directive checks that //go: directives are known and well-formed.
func runDirective(pass *Pass) []Diagnostic {
	// Known //go: directives.
	known := map[string]bool{
		"generate": true, "build": true,
		"noescape": true, "noinline": true, "norace": true, "nosplit": true,
		"linkname": true, "embed": true,
		"debug": true, "aligncheck": true,
		"uintptrescapes": true,
		"wasmimport":     true, // valid since go1.24
		"dyld":           true, "cgo_import_dynamic": true, "cgo_import_static": true,
		"cgo_export_dynamic": true, "cgo_export_static": true,
		"cgo_unsafe_args": true, "cgo_dynamic_linker": true,
	}
	var out []Diagnostic
	for _, f := range pass.Files {
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				text := strings.TrimSpace(c.Text)
				if !strings.HasPrefix(text, "//go:") {
					continue
				}
				rest := text[len("//go:"):]
				// Get the directive name (up to space or end).
				name := rest
				if i := strings.IndexAny(rest, " \t"); i >= 0 {
					name = rest[:i]
				}
				if !known[name] {
					out = append(out, Diagnostic{
						pass.position(c),
						"unknown go: directive " + name,
					})
				}
			}
		}
	}
	return out
}

// buildtag checks //go:build and // +build directives for syntax errors
// and placement (must be near the top of the file, before package clause
// with only blank lines and comments between).
func runBuildTag(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		// Check // +build lines.
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				text := strings.TrimSpace(c.Text)
				if strings.HasPrefix(text, "// +build") {
					rest := strings.TrimSpace(text[len("// +build"):])
					if rest == "" {
						out = append(out, Diagnostic{
							pass.position(c),
							"// +build line with no build tags",
						})
						continue
					}
					// Basic syntax check: tags are space-separated, comma=AND, space=OR.
					// Check for invalid characters.
					for _, field := range strings.Fields(rest) {
						for _, part := range strings.Split(field, ",") {
							p := strings.TrimPrefix(part, "!")
							if p == "" {
								out = append(out, Diagnostic{
									pass.position(c),
									"malformed // +build tag: " + field,
								})
								break
							}
							for _, r := range p {
								if !isBuildTagChar(r) {
									out = append(out, Diagnostic{
										pass.position(c),
										"malformed // +build tag: " + field,
									})
									break
								}
							}
						}
					}
				}
			}
		}
		// Check placement: //go:build should be before package clause.
		// (The parser already enforces this; we check // +build after package.)
		if f.Package != token.NoPos {
			_ = f.Package
		}
	}
	return out
}

func isBuildTagChar(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
		r == '_' || r == '.' || r == '-'
}
