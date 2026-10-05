package main

// compare.go — normalization and verdicts for the M8 fuzzer.
//
// Compared: exit codes, stdout bytes (exact), stderr (normalized).
// Verdicts:
//   SAME         — everything matches
//   DIVERGED     — real behavioral difference (bug signal)
//   SCOPE_REJECT — graalgo --check failed fast with a scope error (expected)
//   GC_INVALID   — gc rejected the program (generator bug, not counted)
//   TRACE_DIFF   — only panic-trace formatting differs (known 27_panic_trace
//                  limitation: PC offsets/paths/qualifiers)

import (
	"regexp"
	"strings"
)

type verdict int

const (
	vSame verdict = iota
	vDiverged
	vScopeReject
	vGCInvalid
	vTraceDiff
)

func (v verdict) String() string {
	switch v {
	case vSame:
		return "SAME"
	case vDiverged:
		return "DIVERGED"
	case vScopeReject:
		return "SCOPE_REJECT"
	case vGCInvalid:
		return "GC_INVALID"
	case vTraceDiff:
		return "TRACE_DIFF"
	}
	return "?"
}

var (
	reJavaWarn  = regexp.MustCompile(`(?m)^WARNING: .*\n`)
	reAddr      = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	reGoroutine = regexp.MustCompile(`goroutine \d+`)
	rePath      = regexp.MustCompile(`/[^\s:]*fuzz/work[^\s:]*`)
	reDuration  = regexp.MustCompile(`\d+(\.\d+)?(µs|ms|s)\b`)
)

// normalizeStderr scrubs machine-specific noise from stderr before comparison.
// Stdout is compared byte-exact (the generator never emits addresses there).
func normalizeStderr(s string) string {
	s = reJavaWarn.ReplaceAllString(s, "")
	s = reAddr.ReplaceAllString(s, "0xADDR")
	s = reGoroutine.ReplaceAllString(s, "goroutine N")
	s = rePath.ReplaceAllString(s, "<WORKDIR>")
	s = reDuration.ReplaceAllString(s, "<DUR>")
	return strings.TrimSpace(s)
}

// isScopeError reports whether a graalgo --check failure is a fail-fast scope
// rejection (non-zero exit, message on stderr) rather than a crash.
func isScopeError(buildErr string) bool {
	l := strings.ToLower(buildErr)
	for _, kw := range []string{"scope", "not supported", "workstream", "ws d", "ws e", "ws g", "ws h", "phase 2", "phase 3"} {
		if strings.Contains(l, kw) {
			return true
		}
	}
	return false
}

// firstLine returns the first line of s.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// compare runs the full verdict logic for one program.
func compare(gc, gr runResult) verdict {
	if !gc.buildOK {
		return vGCInvalid
	}
	if !gr.buildOK {
		if gr.buildCode == 2 && isScopeError(gr.buildErr) {
			return vScopeReject
		}
		// graalgo rejected (or crashed on) a program gc accepts: divergence
		// in build behavior. Even a scope-looking rejection with the wrong
		// exit code / a Go panic stack is a bug signal.
		return vDiverged
	}
	if gc.timedOut || gr.timedOut {
		// Timeouts are environmental noise for the fuzzer's purposes unless
		// one side hangs and the other doesn't — treat asymmetric hang as
		// divergence (possible infinite loop bug), symmetric as SAME-ish.
		if gc.timedOut != gr.timedOut {
			return vDiverged
		}
		return vSame
	}
	if gc.exit != gr.exit {
		return vDiverged
	}
	if gc.stdout != gr.stdout {
		return vDiverged
	}
	nsGC := normalizeStderr(gc.stderr)
	nsGR := normalizeStderr(gr.stderr)
	if nsGC == nsGR {
		return vSame
	}
	// Panic-trace formatting is a known limitation (27_panic_trace): if the
	// program panicked and the panic message line matches, only the trace
	// body differs.
	if gc.exit == 2 && strings.Contains(nsGC, "panic:") {
		if firstLine(nsGC) == firstLine(nsGR) {
			return vTraceDiff
		}
	}
	return vDiverged
}
