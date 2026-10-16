package main

import "testing"

// runCheckExitCode must implement `go run`'s build-phase exit contract:
// success stays 0, every backend failure code (input error, build
// failure, signal death) becomes 1. The P1-1 regression case is backend
// code 2 (build failure) mapping to 1 instead of leaking raw. See the
// wart comment on graalgoRunProgram in main.go.
func TestRunCheckExitCode(t *testing.T) {
	cases := []struct {
		name string
		code int
		want int
	}{
		{"success", 0, 0},          // backend EXIT_OK
		{"input error", 1, 1},      // backend EXIT_INPUT_ERROR
		{"build failure", 2, 1},    // backend EXIT_BUILD_FAILURE (P1-1)
		{"signal death", -1, 1},    // exec.ExitError.ExitCode on signal
		{"killed", 137, 1},         // e.g. OOM kill
		{"unknown nonzero", 42, 1}, // any other failure still exits 1
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := runCheckExitCode(c.code); got != c.want {
				t.Errorf("runCheckExitCode(%d) = %d, want %d", c.code, got, c.want)
			}
		})
	}
}
