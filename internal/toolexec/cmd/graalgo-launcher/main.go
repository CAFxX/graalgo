// Command graalgo-launcher is the graalgo launcher of
// docs/arch/toolexec-design.md §1 (decision D1): it execs the stock go
// command of graalgo's GOROOT with graalgo-tool injected as -toolexec. It
// reimplements no go command. Installed as GRAALGO_HOME/bin/graalgo; it
// replaces the cmd/graalgo driver at the M3 cutover (§8).
//
// Because the launcher execs go (execve), go's exit status and signal
// disposition are the launcher's. The one exception is a bare
// `graalgo version`, whose single output line gets graalgo's version
// appended; that go runs as a child and its exit status is forwarded.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"graalgo-toolexec/launcher"
	"graalgo-toolexec/release"
)

func main() {
	home := os.Getenv("GRAALGO_HOME")
	if home == "" {
		exe, err := os.Executable()
		if err != nil {
			fatal(err)
		}
		home = launcher.HomeFromExecutable(exe)
	}
	plan, err := launcher.Compose(launcher.Request{Home: home, Args: os.Args[1:], Env: os.Environ()})
	if err != nil {
		fatal(err)
	}
	if plan.AppendVersion {
		os.Exit(version(plan))
	}
	fatal(syscall.Exec(plan.Go, plan.Argv, plan.Env))
}

// version runs `go version` and appends " graalgo-<version>" to its line.
func version(plan *launcher.Plan) int {
	cmd := exec.Command(plan.Go, plan.Argv[1:]...)
	cmd.Env, cmd.Stdin, cmd.Stderr = plan.Env, os.Stdin, os.Stderr
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	line := strings.TrimSuffix(out.String(), "\n")
	if line != "" {
		fmt.Printf("%s graalgo-%s\n", line, release.Version)
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		return ee.ExitCode()
	default:
		fatal(err)
		return 2
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "graalgo: %v\n", err)
	os.Exit(2)
}
