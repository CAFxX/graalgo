// The batch runner's per-program process view (test/m1r/batch.sh): its
// arguments, its stdin, its working directory, stdout and stderr apart.
package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	in, _ := io.ReadAll(os.Stdin)
	wd, _ := os.Getwd()
	os.Stdout.WriteString("args " + strings.Join(os.Args[1:], ",") + "\n")
	os.Stdout.WriteString("stdin " + strings.TrimSpace(string(in)) + "\n")
	os.Stdout.WriteString("dir " + filepath.Base(wd) + "\n")
	println("to stderr")
}
