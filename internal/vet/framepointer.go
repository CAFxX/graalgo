package vet

import (
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"unicode"
)

func init() {
	register(&Analyzer{
		Name: "framepointer",
		Doc:  "report assembly that clobbers the frame pointer before saving it",
		Run:  runFramePointer,
	})
}

// asmFilesForPass returns the sorted, deduplicated list of assembly (.s)
// files found in the directories containing the analyzed Go files.
func asmFilesForPass(pass *Pass) []string {
	seen := make(map[string]bool)
	var out []string
	for _, f := range pass.Files {
		dir := filepath.Dir(pass.Fset.Position(f.Pos()).Filename)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".s") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// asmPkgName returns the package clause name of the analyzed files.
func asmPkgName(pass *Pass) string {
	if len(pass.Files) > 0 && pass.Files[0].Name != nil {
		return pass.Files[0].Name.Name
	}
	return ""
}

// fpArch describes per-architecture frame pointer checks. Lines are
// assumed to have comments stripped and surrounding space trimmed.
type fpArch struct {
	isFPWrite             func(string) bool
	isFPRead              func(string) bool
	isUnconditionalBranch func(string) bool
}

func fpHasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

var fpArches = map[string]fpArch{
	"amd64": {
		isFPWrite: regexp.MustCompile(`,\s*BP$`).MatchString,
		isFPRead:  regexp.MustCompile(`\bBP\b`).MatchString,
		isUnconditionalBranch: func(s string) bool {
			return fpHasAnyPrefix(s, "JMP", "RET")
		},
	},
	"arm64": {
		isFPWrite: func(s string) bool {
			if i := strings.LastIndex(s, ","); i > 0 && strings.HasSuffix(s[i:], "R29") {
				return true
			}
			if fpHasAnyPrefix(s, "LDP", "LDAXP", "LDXP", "CASP") {
				// Instructions which write to a pair of registers, e.g.
				// LDP 8(R0), (R26, R29)
				lp := strings.LastIndex(s, "(")
				rp := strings.LastIndex(s, ")")
				if lp > -1 && lp < rp {
					return strings.Contains(s[lp:rp], ",") && strings.Contains(s[lp:rp], "R29")
				}
			}
			return false
		},
		isFPRead: regexp.MustCompile(`\bR29\b`).MatchString,
		isUnconditionalBranch: func(s string) bool {
			if i := strings.IndexFunc(s, unicode.IsSpace); i > 0 {
				s = s[:i]
			}
			return s == "B" || s == "JMP" || s == "RET"
		},
	},
}

// runFramePointer implements the framepointer analyzer: it reports
// assembly code in frameless functions that clobbers the frame pointer
// before saving it.
func runFramePointer(pass *Pass) []Diagnostic {
	arch, ok := fpArches[runtime.GOARCH]
	if !ok {
		return nil
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return nil
	}
	if asmPkgName(pass) == "runtime" {
		return nil
	}
	var out []Diagnostic
	for _, fname := range asmFilesForPass(pass) {
		content, err := os.ReadFile(fname)
		if err != nil {
			continue
		}
		lines := strings.SplitAfter(string(content), "\n")
		active := false
		for lineno, line := range lines {
			lineno++
			// Ignore comments and commented-out code.
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// We start checking code at a TEXT line for a frameless function.
			if strings.HasPrefix(line, "TEXT") && strings.Contains(line, "(SB)") && strings.Contains(line, "$0") {
				active = true
				continue
			}
			if !active {
				continue
			}
			if arch.isFPWrite(line) {
				out = append(out, Diagnostic{
					token.Position{Filename: fname, Line: lineno, Column: 1},
					"frame pointer is clobbered before saving",
				})
				active = false
				continue
			}
			if arch.isFPRead(line) || arch.isUnconditionalBranch(line) {
				active = false
				continue
			}
		}
	}
	return out
}
