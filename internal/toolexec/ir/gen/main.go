// Command gen regenerates the GIR codecs from ../schema.txt:
// ../gir_gen.go and the Java decoder/encoder under
// internal/backend/gir/src/main/java/graalgo/gir/. Run it with
// `go generate ./ir` from internal/toolexec.
package main

import (
	"fmt"
	"os"

	"graalgo-toolexec/ir/schema"
)

func main() {
	if err := schema.WriteAll("."); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}
