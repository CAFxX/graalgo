package main

import (
	"archive/tar"
	"bytes"
)

func main() {
	// Direct call desugars (table row exists); the method value must fail loud.
	tr := tar.NewReader(bytes.NewReader(nil))
	f := tr.Read
	_, _ = f(make([]byte, 8))
}
