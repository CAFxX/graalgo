// Package asmstrategy owns the Plan 9 assembly strategy (P3-M6, SPEC
// §2.8 GG-0009, ADR-0007 and its M7 addendum): the generated inventory
// plus the *resolution* half of the mechanism.
//
// Selection stays frontend-specific (p3frontend: Loader.SelectAsmFiles;
// gofrontend: go/build's bp.SFiles) -- resolution is shared data +
// policy. The inventory is a constructor parameter, never a package
// global: LoadEmbedded is the production constructor, tests and the
// p3frontend adapter build small inventories directly.
//
// Fail-fast error format (the contract, ADR-0007 D7):
//
//	<caller>: asm fail-fast: <path> (package "<pkg>"): category "<category>": <reason>
//
// where <caller> is "gofrontend" or "p3frontend". The text after the
// caller prefix is byte-identical across callers.
package asmstrategy

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// AsmCategory is one SPEC §2.8 disposition of a Plan 9 assembly file.
type AsmCategory string

const (
	AsmEvaporate             AsmCategory = "evaporate"
	AsmIntrinsify            AsmCategory = "intrinsify"
	AsmEmulateThenIntrinsify AsmCategory = "emulate-then-intrinsify"
	AsmJVMIntrinsicMath      AsmCategory = "jvm-math"
	AsmUnsupported           AsmCategory = "unsupported"
	// AsmUncategorized is not a stored category: it is the absence of an
	// inventory entry, reported in fail-fast errors.
	AsmUncategorized AsmCategory = "uncategorized"
)

// AsmEntry is one row of the embedded inventory.
type AsmEntry struct {
	Path        string `json:"path"`        // GOROOT/src-relative, slash-separated
	Package     string `json:"package"`     // import path of the containing dir
	Category    string `json:"category"`    // one of the five stored categories
	Lines       int    `json:"lines"`       // source lines (inventory metadata)
	Replacement string `json:"replacement"` // intrinsic name / Java class / emulation note; "" for unsupported
	Reason      string `json:"reason"`      // why this category (fail-fast payload for unsupported)
}

// AsmResolution is the mechanism's verdict for one buildable .s file:
// the file is replaced (build proceeds) per Category/Replacement.
type AsmResolution struct {
	File        string // absolute path of the .s file
	Package     string // import path of the package being compiled
	Category    string // stored category (never "uncategorized" here)
	Replacement string // pointer to the replacement
	Reason      string // why this category
}

// Inventory is a parsed, validated asm inventory. Construct via
// LoadEmbedded (production) or NewInventory (tests, adapters); the
// zero value is not usable.
type Inventory struct {
	caller string
	byPath map[string]AsmEntry
}

// NewInventory parses and validates inventory JSON (the shape emitted by
// tools/stdlib-fork/gen-asm-inventory.py). caller names the frontend in
// fail-fast errors ("gofrontend" / "p3frontend").
//
// The payload's go_version must equal runtime.Version() of the building
// toolchain: a mismatch means the toolchain moved without the inventory
// being regenerated (ADR-0007 D6), and resolving against a stale
// inventory would miscategorize files -- so it fails here, loudly, at
// first use instead.
func NewInventory(data []byte, caller string) (*Inventory, error) {
	var payload struct {
		GoVersion  string     `json:"go_version"`
		Categories []string   `json:"categories"`
		Files      []AsmEntry `json:"files"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("%s: parsing asm inventory: %w", caller, err)
	}
	if payload.GoVersion != runtime.Version() {
		return nil, fmt.Errorf("%s: asm inventory go_version mismatch: inventory is %q, building toolchain is %q "+
			"(re-run tools/stdlib-fork/gen-asm-inventory.py on toolchain upgrade)", caller, payload.GoVersion, runtime.Version())
	}
	known := map[string]bool{}
	for _, c := range payload.Categories {
		known[c] = true
	}
	byPath := make(map[string]AsmEntry, len(payload.Files))
	for _, e := range payload.Files {
		if e.Path == "" {
			return nil, fmt.Errorf("%s: asm inventory entry with empty path", caller)
		}
		if !known[e.Category] {
			return nil, fmt.Errorf("%s: asm inventory: %s has unknown category %q", caller, e.Path, e.Category)
		}
		if _, dup := byPath[e.Path]; dup {
			return nil, fmt.Errorf("%s: asm inventory: duplicate entry for %s", caller, e.Path)
		}
		byPath[e.Path] = e
	}
	return &Inventory{caller: caller, byPath: byPath}, nil
}

// LoadEmbedded loads the generated inventory (zz_asm_inventory_gen.go)
// naming the gofrontend caller in fail-fast errors.
func LoadEmbedded() (*Inventory, error) {
	return LoadEmbeddedAs("gofrontend")
}

// LoadEmbeddedAs loads the generated inventory naming caller in
// fail-fast errors. The p3frontend adapter passes "p3frontend" to keep
// its test-pinned error format byte-identical.
func LoadEmbeddedAs(caller string) (*Inventory, error) {
	return NewInventory(asmInventoryJSON, caller)
}

// Entries returns a copy of the inventory keyed by slash-separated
// GOROOT/src-relative path. For completeness auditing and tests;
// resolution goes through ResolveAsmFile/CheckAsmFiles.
func (in *Inventory) Entries() map[string]AsmEntry {
	out := make(map[string]AsmEntry, len(in.byPath))
	for k, v := range in.byPath {
		out[k] = v
	}
	return out
}

// IsStdlibDir reports whether dir is under <goroot>/src -- the
// structural scoping predicate for the D2 hook (a GOROOT path prefix,
// not a package-name list). The inventory only covers GOROOT/src pinned
// to the generating toolchain; third-party dirs (module cache, GOPATH)
// are out of scope by construction.
func IsStdlibDir(dir, goroot string) bool {
	if goroot == "" {
		return false
	}
	root := filepath.Join(goroot, "src")
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	return abs == root || strings.HasPrefix(abs, root+string(filepath.Separator))
}

// ResolveAsmFile resolves one assembly file (basename or path, resolved
// under the package dir by CheckAsmFiles) to its inventory category.
// Files replaced by a category (evaporate, intrinsify,
// emulate-then-intrinsify, jvm-math) return an AsmResolution and the
// build proceeds. Files categorized "unsupported", files outside
// GOROOT/src, and files with no inventory entry ("uncategorized") fail
// fast: the error names the file path, the package, the category, and
// the reason.
//
// A replaced category with an empty replacement pointer is a
// generator/inventory bug (the pointer is the wave work list): fail fast
// structurally rather than carrying a dangling pointer to the backend.
func (in *Inventory) ResolveAsmFile(goroot, absPath, importPath string) (AsmResolution, error) {
	abs, err := filepath.Abs(absPath)
	if err != nil {
		abs = absPath
	}
	fail := func(category AsmCategory, reason string) (AsmResolution, error) {
		return AsmResolution{}, fmt.Errorf(
			"%s: asm fail-fast: %s (package %q): category %q: %s",
			in.caller, abs, importPath, string(category), reason)
	}
	srcRoot := filepath.Join(goroot, "src")
	rel, relErr := filepath.Rel(srcRoot, abs)
	outside := relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
	if goroot == "" || outside {
		return fail(AsmUncategorized,
			fmt.Sprintf("file is outside the inventoried GOROOT/src (%s); only stdlib assembly "+
				"pinned to go1.27.1 has a disposition -- module-local Plan 9 assembly is not "+
				"supported by graalgo, rewrite in Go or via a graalgo intrinsic (SPEC §2.8 GG-0009)",
				srcRoot))
	}
	slashRel := filepath.ToSlash(rel)
	entry, ok := in.byPath[slashRel]
	if !ok {
		return fail(AsmUncategorized,
			fmt.Sprintf("no inventory entry for %q in the go1.27.1 asm inventory -- re-run "+
				"tools/stdlib-fork/gen-asm-inventory.py on toolchain upgrade; "+
				"uncategorized assembly fails the build by design", slashRel))
	}
	if entry.Category == string(AsmUnsupported) {
		return fail(AsmUnsupported, entry.Reason)
	}
	if entry.Replacement == "" {
		return fail(AsmCategory(entry.Category),
			fmt.Sprintf("replaced category %q with an empty replacement pointer (generator/inventory bug -- "+
				"re-run tools/stdlib-fork/gen-asm-inventory.py)", entry.Category))
	}
	return AsmResolution{
		File:        abs,
		Package:     importPath,
		Category:    entry.Category,
		Replacement: entry.Replacement,
		Reason:      entry.Reason,
	}, nil
}

// CheckAsmFiles resolves each basename in sFiles (the assembly files the
// frontend selected as build inputs, e.g. go/build's bp.SFiles) under
// dir against the inventory. It returns the resolutions for every
// replaced file, or the first fail-fast error. A package with no
// assembly files returns an empty slice and no error.
func (in *Inventory) CheckAsmFiles(dir, importPath string, sFiles []string, goroot string) ([]AsmResolution, error) {
	var out []AsmResolution
	for _, name := range sFiles {
		res, err := in.ResolveAsmFile(goroot, filepath.Join(dir, name), importPath)
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, nil
}
