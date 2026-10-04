// Command req manages the graalgo requirements registry.
//
// Subcommands: add | lint | trace | tree | supersede | retire | lock | activate
package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run executes one CLI invocation and returns the process exit code.
// It is separated from main so tests can exercise the full dispatch path
// (unknown commands, help, per-command errors) without a subprocess.
func run(args []string) int {
	if len(args) < 1 {
		usage()
		return 2
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "add":
		err = cmdAdd(rest)
	case "lint":
		err = cmdLint(rest)
	case "trace":
		err = cmdTrace(rest)
	case "tree":
		err = cmdTree(rest)
	case "supersede":
		err = cmdSupersede(rest)
	case "retire":
		err = cmdRetire(rest)
	case "lock":
		err = cmdLock(rest)
	case "activate":
		err = cmdActivate(rest)
	case "help", "-h", "--help":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "req: unknown command %q\n", cmd)
		usage()
		return 2
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "req %s: %v\n", cmd, err)
		return 1
	}
	return 0
}

func usage() {
	fmt.Print(`req — graalgo requirements registry CLI

Usage: req <command> [flags]

Commands:
  add        mint a new draft requirement ID
  lint       check ID annotations and traceability rules (a)-(d)
  trace      show requirement -> code sites -> tests
  tree       render the requirement hierarchy
  supersede  retire/supersede a requirement (atomic workflow)
  retire     retire a requirement with no successor (atomic workflow)
  lock       lock draft requirement(s) at SPEC acceptance (atomic workflow)
  activate   activate locked requirement(s) as implementation starts (atomic)

Common flags: --root DIR  --req-dir DIR  --scan-dir DIR
`)
}
