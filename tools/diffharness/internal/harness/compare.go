package harness

import (
	"fmt"
	"strings"
)

// Comparison is the verdict of comparing one reference outcome against one
// alternate-backend outcome for the same program.
type Comparison struct {
	Program string   `json:"program"`
	Ref     string   `json:"ref"`
	Alt     string   `json:"alt"`
	Same    bool     `json:"same"`
	Reasons []string `json:"reasons"`
}

// Compare decides whether two outcomes of the same program are equivalent.
// Both outcomes are expected to carry already-normalized Stderr/BuildLog
// (the runner normalizes at capture).
func Compare(ref, alt Outcome) Comparison {
	c := Comparison{Program: ref.Program, Ref: ref.Backend, Alt: alt.Backend, Same: true}
	add := func(format string, args ...any) {
		c.Same = false
		c.Reasons = append(c.Reasons, fmt.Sprintf(format, args...))
	}

	switch {
	case ref.BuildOK && !alt.BuildOK:
		add("build: ref ok, alt failed: %s", firstLines(alt.BuildLog, 5))
	case !ref.BuildOK && alt.BuildOK:
		add("build: ref failed: %s, alt ok", firstLines(ref.BuildLog, 5))
	case !ref.BuildOK && !alt.BuildOK:
		if ref.BuildLog != alt.BuildLog {
			add("build: both failed but logs differ: %s", firstDiffLine(ref.BuildLog, alt.BuildLog))
		}
		return c // nothing else to compare
	}

	if ref.TimedOut != alt.TimedOut {
		add("timeout: ref=%v alt=%v", ref.TimedOut, alt.TimedOut)
	}
	if ref.ExitCode != alt.ExitCode {
		add("exit code: ref=%d alt=%d", ref.ExitCode, alt.ExitCode)
	}
	if ref.Stdout != alt.Stdout {
		add("stdout differs at %s", firstDiffLine(ref.Stdout, alt.Stdout))
	}
	if ref.Stderr != alt.Stderr {
		add("stderr differs at %s", firstDiffLine(ref.Stderr, alt.Stderr))
	}
	return c
}

// firstDiffLine locates the first differing line between a and b for compact
// reporting. Empty diff context means one side is a prefix of the other.
func firstDiffLine(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			return fmt.Sprintf("line %d: ref=%q alt=%q", i+1, trunc(al[i], 120), trunc(bl[i], 120))
		}
	}
	if len(al) != len(bl) {
		n := min(len(al), len(bl))
		var extra string
		if len(al) > len(bl) {
			extra = trunc(al[n], 120)
		} else {
			extra = trunc(bl[n], 120)
		}
		return fmt.Sprintf("line %d: length differs, extra=%q", n+1, extra)
	}
	return "<identical>"
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return trunc(strings.Join(lines, "\\n"), 300)
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
