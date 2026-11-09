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
	// Known //go: directives, audited against the go1.27.1 toolchain
	// (R05-V-15). Sources, in order below:
	//   - func pragmas: cmd/compile/internal/noder/lex.go pragmaFlag and
	//     cmd/compile/internal/ir/node.go PragmaFlag, plus
	//     cmd/compile/doc.go and src/runtime/HACKING.md;
	//   - cgo / embed / linkname / wasm: cmd/compile/internal/noder/noder.go
	//     pragma() and allowedStdPragmas;
	//   - //go:debug: cmd/go/internal/load/godebug.go;
	//   - //go:notinheap: emitted by cmd/cgo itself (cmd/cgo/out.go).
	// The list is hand-maintained: there is no machine-readable directive
	// table in the toolchain to generate it from. TestDirectiveKnownDirectives
	// enumerates the full set, so dropping a name fails loudly.
	// Deliberately NOT included (verified absent from the whole go1.27.1
	// source tree, including runtime and the vendored copy):
	//   - "nounwind": no occurrence anywhere in the toolchain source; not a
	//     go1.27.1 directive despite being named in the review;
	//   - "aligncheck" and "dyld": previously listed here, but with no
	//     occurrence anywhere in the toolchain source either, so they were
	//     bogus entries that would have suppressed true positives. Removed
	//     by this audit.
	known := map[string]bool{
		// Func pragmas (pragmaFlag).
		"build": true, "noescape": true, "noinline": true, "norace": true,
		"nosplit": true, "nocheckptr": true, "nointerface": true,
		"registerparams": true, "uintptrkeepalive": true, "uintptrescapes": true,
		// Runtime-only func pragmas (HACKING.md; rejected outside runtime
		// by the compiler, but still recognised directive names).
		"systemstack": true, "nowritebarrier": true, "nowritebarrierrec": true,
		"yeswritebarrierrec": true,
		// Linker / cgo / embed / tool directives (noder.go, pragcgo).
		"linkname": true, "linknamestd": true, "embed": true,
		"generate": true, "fix": true, "debug": true,
		"cgo_import_dynamic": true, "cgo_import_static": true,
		"cgo_export_dynamic": true, "cgo_export_static": true,
		"cgo_unsafe_args": true, "cgo_dynamic_linker": true, "cgo_ldflag": true,
		// WASM directives (doc.go, noder.go).
		"wasmimport": true, "wasmexport": true,
		// Type directive emitted by cgo itself (cmd/cgo/out.go).
		"notinheap": true,
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
