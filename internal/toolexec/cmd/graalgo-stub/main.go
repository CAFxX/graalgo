// Command graalgo-stub is the launcher stub that the -toolexec link
// wrapper copies to the front of every linked program. At run time it
// finds the program bundle appended to its own executable and hands the
// executable to the graalgo runner, forwarding arguments, environment,
// standard streams and (because it execs) the exit status.
//
// It must be linked with -ldflags=-buildid=<bundle.BuildIDPlaceholder>;
// see docs/arch/toolexec-design.md §2.4.
package main

import (
	"fmt"
	"os"
	"syscall"

	"graalgo-toolexec/bundle"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	b, err := bundle.ReadFile(exe)
	if err != nil {
		fatal(err)
	}
	runner := os.Getenv("GRAALGO_RUNNER")
	if runner == "" {
		runner = b.Runner
	}
	if runner == "" {
		fatal(fmt.Errorf("no runner recorded at link time; set GRAALGO_RUNNER"))
	}
	// The runner receives the executable (it locates the bundle through
	// the trailer) followed by the program's own argv, argv[0] as this
	// process was invoked included: os.Args of the Go program.
	argv := append([]string{runner, exe}, os.Args...)
	fatal(syscall.Exec(runner, argv, os.Environ()))
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "graalgo: %v\n", err)
	os.Exit(2)
}
