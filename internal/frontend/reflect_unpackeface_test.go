package main

// fix-redmaster-unpackeface (RC-4a): exporter tests for the
// reflect.unpackEface intrinsic.
//
// reflect.unpackEface(i any) Value uses unsafe interface reinterpretation
// (*(*abi.EmptyInterface)(unsafe.Pointer(&i))) which cannot work on
// graalgo. It is lowered to the "reflectunpack" wire node.

import (
	"strings"
	"testing"
)

// TestReflectUnpackEfaceIntrinsic: a direct call to reflect.unpackEface
// lowers to the reflectunpack wire node, not a regular call.
func TestReflectUnpackEfaceIntrinsic(t *testing.T) {
	src := `package main

import "reflect"

func main() {
	v := reflect.ValueOf("hello")
	_ = v
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with reflect.ValueOf: %v", err)
	}
	// The unpackEface call inside ValueOf should lower to reflectunpack.
	if !strings.Contains(out, `"reflectunpack"`) {
		t.Errorf("exported JSON missing reflectunpack wire node")
	}
}

// TestReflectUnpackEfaceNotBridged: the intrinsic is structural, not
// name-keyed. A user-defined unpackEface function should NOT be intercepted.
func TestReflectUnpackEfaceNotBridged(t *testing.T) {
	src := `package main

func unpackEface(x int) int {
	return x + 1
}

func main() {
	y := unpackEface(41)
	_ = y
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with user unpackEface: %v", err)
	}
	// User-defined unpackEface (wrong signature) should NOT lower to reflectunpack.
	if strings.Contains(out, `"reflectunpack"`) {
		t.Errorf("user-defined unpackEface incorrectly lowered to reflectunpack")
	}
}
