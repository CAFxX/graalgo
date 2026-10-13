package harness

import "regexp"

// Normalization rules for backend output comparison.
//
// Rationale (FEASIBILITY.md §7): panic traces and fatal-error output embed
// machine addresses, goroutine ids, and source line numbers that are not
// semantic observables of the program. Two correct backends may legitimately
// differ in code addresses and (for traces) line attribution after inlining,
// so these are normalized before comparison. The same applies to addresses a
// program itself prints on stdout (%p): they vary run to run even for one
// binary, so stdout is normalized with the address rule too. Everything
// else — stdout/stderr message text, exit codes, panic messages, timeout
// behavior — is compared exactly.
//
// Explicitly NOT normalized (deliberate divergence signal): message text,
// exit codes, timeouts, build failures.
var (
	// 0x4a3b2c -> 0xADDR : code/data addresses in traces and fatal errors.
	reHexAddr = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	// goroutine 18 [running]: -> goroutine # [running]:
	reGoroutineID = regexp.MustCompile(`goroutine \d+ \[`)
	// in goroutine 18 -> in goroutine #  ("created by ... in goroutine N")
	reCreatedBy = regexp.MustCompile(`in goroutine \d+`)
	// main.go:123 -> main.go:LINE ; line attribution may shift with inlining.
	reGoLine = regexp.MustCompile(`(\.go):\d+`)
	// [signal SIGSEGV: segmentation violation code=0x1 addr=0x8 ...] — the
	// signal code/addr hex is covered by reHexAddr; keep the signal name.
)

// Normalize applies all normalization rules to s (used for stderr, panic
// traces, and build logs).
func Normalize(s string) string {
	s = reHexAddr.ReplaceAllString(s, "0xADDR")
	s = reGoroutineID.ReplaceAllString(s, "goroutine # [")
	s = reCreatedBy.ReplaceAllString(s, "in goroutine #")
	s = reGoLine.ReplaceAllString(s, "$1:LINE")
	return s
}
