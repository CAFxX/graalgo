// 12_stdio: os.Stdout/os.Stderr writes land on the captured streams.
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
	if _, err := fmt.Fprintln(os.Stderr, "STDERR-LINE"); err != nil {
		fmt.Println("FAIL: stderr:", err)
		os.Exit(1)
	}
	fmt.Println("ok")
}
