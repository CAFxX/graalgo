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
// A write failure is returned as an error: treating it as "no longer
// diverges" would silently accept a broken minimization.
func divergesOn(workdir, prog string) (bool, error) {
	src := filepath.Join(workdir, "min.go")
	if err := os.WriteFile(src, []byte(prog), 0644); err != nil {
		return false, err
	}
	gc := runGC(workdir, src)
	gr := runGraal(workdir, src)
	return compare(gc, gr) == vDiverged, nil
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
func minimize(workdir string, seed int64) (string, int, error) {
	pre, stmts, post := generateParts(seed)
	probes := 0
	var firstErr error
	test := func(ss []string) bool {
		probes++
		ok, err := divergesOn(workdir, assemble(pre, ss, post))
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return err == nil && ok
	}
	// sanity: the full program must diverge
	if !test(stmts) {
		return assemble(pre, stmts, post), probes, firstErr
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
	return assemble(pre, stmts, post), probes, firstErr
}

// saveRegression writes the minimized program to the regressions directory.
// A lost regression is a lost bug report — the error is returned, never
// swallowed.
func saveRegression(workdir string, seed int64, prog string) (string, error) {
	dir := filepath.Join(workdir, "regressions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("mkdir regressions: %w", err)
	}
	name := fmt.Sprintf("fuzz_%d.go", seed)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(prog), 0644); err != nil {
		return "", fmt.Errorf("write regression %s: %w", name, err)
	}
	return name, nil
}
