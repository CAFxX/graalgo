package main

// L11 (REVIEW-2026-11-18-05): os/exec — zero corpus imports before this
// program. LookPath, /bin/echo args + captured stdout, exit code of a
// failing command, CombinedOutput, stdin piping. No shell, no network.

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func main() {
	path, err := exec.LookPath("echo")
	fmt.Printf("lookpath-echo=%q err=%v\n", path, err)
	_, err = exec.LookPath("definitely-not-a-real-binary-1805")
	fmt.Printf("lookpath-missing-err=%v\n", err != nil)

	out, err := exec.Command("/bin/echo", "hello", "world").Output()
	fmt.Printf("echo-out=%q err=%v\n", strings.TrimSpace(string(out)), err)

	// Failing command: exit code via ExitError.
	cmd := exec.Command("/bin/false")
	err = cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		fmt.Printf("false-exit=%d\n", ee.ExitCode())
	} else {
		fmt.Printf("false-err=%v\n", err)
	}

	// CombinedOutput merges stdout + stderr.
	co, err := exec.Command("/bin/sh", "-c", "echo out; echo err 1>&2").CombinedOutput()
	fmt.Printf("combined=%q err=%v\n", strings.TrimSpace(string(co)), err)

	// Stdin piping: cat echoes its input.
	c := exec.Command("/bin/cat")
	c.Stdin = strings.NewReader("piped-input\n")
	var buf bytes.Buffer
	c.Stdout = &buf
	if err := c.Run(); err != nil {
		fmt.Printf("cat-err=%v\n", err)
		return
	}
	fmt.Printf("cat-out=%q\n", strings.TrimSpace(buf.String()))

	// Args are passed verbatim (no shell splitting).
	out, err = exec.Command("/bin/echo", "a b", "c*d").Output()
	fmt.Printf("args-verbatim=%q err=%v\n", strings.TrimSpace(string(out)), err)
}
