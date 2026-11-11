package main

import (
	"archive/tar"
	"bytes"
	"reflect"
)

func main() {
	// Reflect call into a panicking method must fail loud, not silently misbehave.
	// DRAFT (probe-owed): the expected outcome is TBD by probe P-RF-1 —
	// compile-time scope error vs runtime panic, and the exact message.
	// Promote to 03_reflect_tar_read.go + .divergent + .panic once the probe
	// pins it. Skipped by the runner (no NN_ prefix).
	tr := tar.NewReader(bytes.NewReader(nil))
	m := reflect.ValueOf(tr).MethodByName("Read")
	m.Call([]reflect.Value{reflect.ValueOf(make([]byte, 8))})
}
