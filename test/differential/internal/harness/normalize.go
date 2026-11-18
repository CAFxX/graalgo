package harness

import (
	"fmt"
	"regexp"
	"strings"
)

// Normalization rules for backend output comparison (SPEC §5.5).
//
// Principle: normalize ONLY what is genuinely nondeterministic or
// address-valued, never program-computed values. Two runs of the same gc
// binary differ in goroutine ids, code addresses (PC offsets, frame
// argument words, signal addresses) and heap pointers; nothing else.
//
// Global rules (Normalize: stderr, panic traces, build logs):
//   - goroutine ids:  "goroutine 18 [" / "in goroutine 18";
//   - trace PC offsets: the " +0x1d" suffix of a trace file line
//     ("\t/path/main.go:12 +0x1d"), and register annotations
//     ("fp=0x… sp=0x… pc=0x…");
//   - frame argument words: hex inside the argument list of a trace frame
//     line ("main.f(0xc000014260, 0x1)", "panic({0x4b1e20?, 0xc0000…?})"):
//     register/stack dumps, not program output;
//   - signal lines: "[signal SIGSEGV: … code=0x1 addr=0x8 pc=0x4a3b2c]".
//
// Deliberately NOT normalized (a difference is a real divergence): source
// line numbers in traces (gc and graalgo must attribute a panic to the
// same line), any 0x… a program prints on stdout or in a panic message,
// message text, exit codes, timeouts, build failures.
//
// Per-program opt-in rules (NormalizeStdout): a corpus program that prints
// addresses on stdout (%p) declares it in a NORMALIZE file in its
// directory; the "pointers" rule then scrubs 0x… on that program's stdout
// only. Annotated per test, never global (SPEC §5.5).
var (
	reGoroutineID = regexp.MustCompile(`goroutine \d+ \[`)
	reCreatedBy   = regexp.MustCompile(`in goroutine \d+`)
	// "\t/abs/main.go:12 +0x1d" -> "\t/abs/main.go:12"
	rePCOffset = regexp.MustCompile(`(?m)(\.go:\d+) \+0x[0-9a-fA-F]+$`)
	// "fp=0xc000046f58 sp=0xc000046f38 pc=0x4a3b2c"
	reRegAnnot = regexp.MustCompile(`\b(fp|sp|pc|gp|mp)=0x[0-9a-fA-F]+\b`)
	// a trace frame line: no leading space, a function name, an argument
	// list closing the line ("...)" for elided args too).
	reFrameLine = regexp.MustCompile(`(?m)^[A-Za-z_][\w./*()\[\]-]*\([^\n]*\)$`)
	// \b: never re-match an already-normalized "0xADDR" ("0xADD" is hex),
	// which made the normalizer non-idempotent ("pc=0xADDRRR").
	reHex    = regexp.MustCompile(`0x[0-9a-fA-F]+\b`)
	reSignal = regexp.MustCompile(`(?m)^\[signal [^\n]*\]$`)
)

// Normalize applies the global rules to s (stderr, traces, build logs).
func Normalize(s string) string {
	s = reGoroutineID.ReplaceAllString(s, "goroutine # [")
	s = reCreatedBy.ReplaceAllString(s, "in goroutine #")
	s = rePCOffset.ReplaceAllString(s, "$1")
	s = reRegAnnot.ReplaceAllString(s, "$1=0xADDR")
	s = reFrameLine.ReplaceAllStringFunc(s, func(line string) string {
		// The argument list is the last top-level parenthesized group;
		// method frames ("pkg.(*T).m(args)") have a receiver group first.
		i := argListStart(line)
		if i < 0 {
			return line
		}
		return line[:i] + reHex.ReplaceAllString(line[i:], "0xADDR")
	})
	s = reSignal.ReplaceAllStringFunc(s, func(line string) string {
		return reHex.ReplaceAllString(line, "0xADDR")
	})
	return s
}

// argListStart returns the index of the "(" opening the parenthesized
// group that closes line, or -1.
func argListStart(line string) int {
	depth := 0
	for i := len(line) - 1; i >= 0; i-- {
		switch line[i] {
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// StdoutRules are the per-program opt-in stdout normalization rules.
var StdoutRules = map[string]*regexp.Regexp{
	// %p / pointer-valued output: heap addresses differ run to run.
	"pointers": reHex,
}

// NormalizeStdout applies the program's declared opt-in rules to stdout.
func NormalizeStdout(s string, rules []string) string {
	for _, r := range rules {
		if re, ok := StdoutRules[r]; ok {
			s = re.ReplaceAllString(s, "0xPTR")
		}
	}
	return s
}

// ParseNormalizeFile parses a corpus program's NORMALIZE file: one rule
// name per line, "#" comments. Unknown rules are an error.
func ParseNormalizeFile(text string) ([]string, error) {
	var rules []string
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, ok := StdoutRules[line]; !ok {
			return nil, fmt.Errorf("unknown normalization rule %q", line)
		}
		rules = append(rules, line)
	}
	return rules, nil
}
