package main

// minimize.go — delta-debugging minimizer for divergent programs.
//
// On DIVERGED, the minimizer removes main-body statements (ddmin) while the
// divergence reproduces, yielding a small regression case. Per SPEC §5.4,
// minimized cases become permanent regressions (tools/fuzz/regressions/).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// divergesOn reports whether the assembled program still diverges.
func divergesOn(workdir, prog string) bool {
	src := filepath.Join(workdir, "min.go")
	if err := os.WriteFile(src, []byte(prog), 0644); err != nil {
		return false
	}
	gc := runGC(workdir, src)
	gr := runGraal(workdir, src)
	return compare(gc, gr) == vDiverged
}

func assemble(pre string, stmts []string, post string) string {
	var b strings.Builder
	b.WriteString(pre)
	for _, s := range stmts {
		b.WriteString(s)
	}
	b.WriteString(post)
	return b.String()
}

// minimize runs ddmin over the statement list. Returns the minimized program
// and the number of backend-pair probes used.
func minimize(workdir string, seed int64) (string, int) {
	pre, stmts, post := generateParts(seed)
	probes := 0
	test := func(ss []string) bool {
		probes++
		return divergesOn(workdir, assemble(pre, ss, post))
	}
	// sanity: the full program must diverge
	if !test(stmts) {
		return assemble(pre, stmts, post), probes
	}
	// ddmin: repeatedly try removing chunks
	n := 2
	for len(stmts) >= 2 {
		chunk := (len(stmts) + n - 1) / n
		reduced := false
		for i := 0; i < len(stmts); i += chunk {
			end := i + chunk
			if end > len(stmts) {
				end = len(stmts)
			}
			cand := append(append([]string{}, stmts[:i]...), stmts[end:]...)
			if test(cand) {
				stmts = cand
				n = 2
				reduced = true
				break
			}
		}
		if !reduced {
			if n >= len(stmts) {
				break
			}
			n *= 2
		}
	}
	// final 1-minimal pass
	changed := true
	for changed {
		changed = false
		for i := 0; i < len(stmts); i++ {
			cand := append(append([]string{}, stmts[:i]...), stmts[i+1:]...)
			if test(cand) {
				stmts = cand
				changed = true
				break
			}
		}
	}
	return assemble(pre, stmts, post), probes
}

// saveRegression writes the minimized program to the regressions directory
// and records it in regressions.json.
func saveRegression(workdir string, seed int64, prog string) string {
	dir := filepath.Join(workdir, "regressions")
	os.MkdirAll(dir, 0755)
	name := fmt.Sprintf("fuzz_%d.go", seed)
	os.WriteFile(filepath.Join(dir, name), []byte(prog), 0644)
	return name
}
