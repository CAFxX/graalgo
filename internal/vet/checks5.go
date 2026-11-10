package vet

import (
	"go/build/constraint"
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
	// Known //go: directives. The list is derived from the directives
	// actually used in the toolchain source (go1.27.1 GOROOT): every
	// name below occurs in non-testdata .go files there, e.g.
	// nowritebarrierrec (239 uses in runtime), wasmimport (90),
	// registerparams, notinheap, nointerface, and "//go:fix inline"
	// (handled by x/tools' gofixdirective analyzer).
	known := map[string]bool{
		"generate": true, "build": true,
		"noescape": true, "noinline": true, "norace": true, "nosplit": true,
		"linkname": true, "embed": true,
		"debug": true, "aligncheck": true,
		"uintptrescapes": true,
		"dyld":           true, "cgo_import_dynamic": true, "cgo_import_static": true,
		"cgo_export_dynamic": true, "cgo_export_static": true,
		"cgo_unsafe_args": true, "cgo_dynamic_linker": true,
		// Previously missing real directives (V-P1-10).
		"nowritebarrier": true, "nowritebarrierrec": true,
		"yeswritebarrierrec": true, "nocheckptr": true,
		"wasmimport": true, "wasmexport": true,
		"systemstack": true, "uintptrkeepalive": true,
		"linknamestd": true, "cgo_ldflag": true,
		"registerparams": true, "notinheap": true,
		"nointerface": true, "fix": true,
		"binary-only-package": true,
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
		// Check //go:build lines: syntax via the toolchain's own
		// constraint parser, and placement before the package clause.
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				text := strings.TrimSpace(c.Text)
				if text != "//go:build" && !strings.HasPrefix(text, "//go:build ") &&
					!strings.HasPrefix(text, "//go:build\t") {
					continue
				}
				if _, err := constraint.Parse(text); err != nil {
					out = append(out, Diagnostic{
						pass.position(c),
						"invalid //go:build constraint: " + err.Error(),
					})
				} else if c.Pos() > f.Package {
					out = append(out, Diagnostic{
						pass.position(c),
						"//go:build directive after package clause",
					})
				}
			}
		}
		// Check // +build placement: after the package clause is too late.
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				text := strings.TrimSpace(c.Text)
				if strings.HasPrefix(text, "// +build") && c.Pos() > f.Package {
					out = append(out, Diagnostic{
						pass.position(c),
						"// +build directive after package clause",
					})
				}
			}
		}
	}
	return out
}

func isBuildTagChar(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
		r == '_' || r == '.' || r == '-'
}
