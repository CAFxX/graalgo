// Command gen regenerates the Java syscall role table
// (internal/backend/truffle/src/main/java/graalgo/syscall/SyscallTable.java)
// from ../table.txt. Run it with `go generate ./syscalls` from
// internal/toolexec.
package main

import (
	"fmt"
	"os"

	"graalgo-toolexec/syscalls"
)

func main() {
	// go generate runs in the package directory, internal/toolexec/syscalls.
	if err := syscalls.Default.WriteJava(".."); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}
