package main

import (
	"archive/tar"
	"bytes"
)

func main() {
	// No iface-table row for Next: interface dispatch must fail loud.
	// DRAFT (probe-owed): the expected outcome is TBD by probe P-IF-1 —
	// runtime panic with the tar.Next message, or a compile-time scope
	// error. Promote to 02_iface_dispatch_tar_next.go + .divergent + .panic
	// once the probe pins it. Skipped by the runner (no NN_ prefix).
	tr := tar.NewReader(bytes.NewReader(nil))
	var x interface{ Next() (*tar.Header, error) } = tr
	_, _ = x.Next()
}
