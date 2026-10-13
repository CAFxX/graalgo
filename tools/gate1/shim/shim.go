// Package shim provides a minimal testing.T replacement for the Gate 1
// trial harness. Real stdlib tests import "testing"; graalgo's single-file
// mode cannot use the full testing package (it depends on runtime test
// binary infrastructure). This shim provides the subset of testing.T
// methods used by stdlib tests, with identical API signatures.
//
// The harness rewrites `import "testing"` to import this shim in synthesized
// drivers, so the same test source compiles under both graalgo and gc.
package shim

import (
	"fmt"
	"os"
)

// T is a minimal replacement for testing.T.
type T struct {
	name   string
	failed bool
	skipped bool
	logs   []string
}

// Name returns the test name.
func (t *T) Name() string { return t.name }

// Error reports a test failure.
func (t *T) Error(args ...interface{}) {
	t.failed = true
	t.logs = append(t.logs, fmt.Sprint(args...))
}

// Errorf reports a formatted test failure.
func (t *T) Errorf(format string, args ...interface{}) {
	t.failed = true
	t.logs = append(t.logs, fmt.Sprintf(format, args...))
}

// Fail marks the test as failed.
func (t *T) Fail() { t.failed = true }

// Failed reports whether the test failed.
func (t *T) Failed() bool { return t.failed }

// Fatal aborts the test (panics with a sentinel; the runner recovers).
func (t *T) Fatal(args ...interface{}) {
	t.failed = true
	t.logs = append(t.logs, fmt.Sprint(args...))
	panic(fatalSentinel{})
}

// Fatalf aborts the test with a formatted message.
func (t *T) Fatalf(format string, args ...interface{}) {
	t.failed = true
	t.logs = append(t.logs, fmt.Sprintf(format, args...))
	panic(fatalSentinel{})
}

// Log records a log message.
func (t *T) Log(args ...interface{}) {
	t.logs = append(t.logs, fmt.Sprint(args...))
}

// Logf records a formatted log message.
func (t *T) Logf(format string, args ...interface{}) {
	t.logs = append(t.logs, fmt.Sprintf(format, args...))
}

// Helper is a no-op (marks the caller as a helper).
func (t *T) Helper() {}

// Skip marks the test as skipped.
func (t *T) Skip(args ...interface{}) {
	t.skipped = true
	t.logs = append(t.logs, fmt.Sprint(args...))
	panic(skipSentinel{})
}

// Skipf marks the test as skipped with a formatted message.
func (t *T) Skipf(format string, args ...interface{}) {
	t.skipped = true
	t.logs = append(t.logs, fmt.Sprintf(format, args...))
	panic(skipSentinel{})
}

// Skipped reports whether the test was skipped.
func (t *T) Skipped() bool { return t.skipped }

// Parallel is a no-op (tests run sequentially in the harness).
func (t *T) Parallel() {}

// Cleanup registers a cleanup function (runs at test end).
func (t *T) Cleanup(f func()) {
	cleanups = append(cleanups, f)
}

type fatalSentinel struct{}
type skipSentinel struct{}

var cleanups []func()

// Result holds the outcome of a single test.
type Result struct {
	Name    string
	Passed  bool
	Skipped bool
	Logs    []string
}

// Run executes a test function and captures the result.
func Run(name string, fn func(*T)) (res Result) {
	res.Name = name
	t := &T{name: name}
	cleanups = nil
	defer func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
		if r := recover(); r != nil {
			switch r.(type) {
			case fatalSentinel, skipSentinel:
				// Expected control flow; t.failed/skipped already set.
			default:
				t.failed = true
				t.logs = append(t.logs, fmt.Sprintf("panic: %v", r))
			}
		}
		res.Passed = !t.failed && !t.skipped
		res.Skipped = t.skipped
		res.Logs = t.logs
	}()
	fn(t)
	return res
}

// Report prints results in a stable format for differential comparison.
func Report(results []Result) {
	passed, failed, skipped := 0, 0, 0
	for _, r := range results {
		switch {
		case r.Skipped:
			skipped++
			fmt.Printf("SKIP %s\n", r.Name)
		case r.Passed:
			passed++
			fmt.Printf("PASS %s\n", r.Name)
		default:
			failed++
			fmt.Printf("FAIL %s\n", r.Name)
			for _, l := range r.Logs {
				fmt.Printf("  %s\n", l)
			}
		}
	}
	fmt.Printf("SUMMARY passed=%d failed=%d skipped=%d\n", passed, failed, skipped)
	if failed > 0 {
		os.Exit(1)
	}
}
