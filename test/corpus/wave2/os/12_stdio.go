// 12_stdio: os.Stdout/os.Stderr writes must be byte-identical to the live
// gc leg's captured streams. The harness compares stdout bytes, exit codes,
// and — for exit-0 programs like this one — the full filtered stderr
// against gc, so a stderr write landing anywhere else fails the gate.
package main

import (
	"fmt"
	"os"
)

func main() {
	if _, err := os.Stdout.WriteString("STDOUT-LINE\n"); err != nil {
		fmt.Println("FAIL: stdout:", err)
		os.Exit(1)
	}
	if _, err := os.Stderr.WriteString("STDERR-LINE\n"); err != nil {
		fmt.Println("FAIL: stderr:", err)
		os.Exit(1)
	}
	fmt.Println("ok")
}
