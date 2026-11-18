// Command gen regenerates the Java intrinsic dispatch table
// (internal/backend/truffle/src/main/java/graalgo/go/intrinsics/IntrinsicTable.java)
// from ../registry.txt. Run it with `go generate ./intrinsics` from
// internal/toolexec.
package main

import (
	"fmt"
	"os"

	"graalgo-toolexec/intrinsics"
)

func main() {
	// go generate runs in the package directory, internal/toolexec/intrinsics.
	if err := intrinsics.Registry.WriteJava(".."); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}
