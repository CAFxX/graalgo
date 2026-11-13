package p3frontend

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReflectStructFieldIndex verifies the StructField.Index backing store
// (bodge-scan 2026-11-06, pattern 7 family): reflect.StructField.Index is
// []int, and Go int is 64-bit (D5: int → INT64 → long[]). The backend packed
// it on an int[] backing, so the first element read — Index[0], range, or
// fmt-print — threw ClassCastException in Boxing.arrayGet(INT64), which
// casts to long[]. The test reads Index elements three ways (index expr,
// range, fmt %v) for a plain struct (single-element path [0]) and for a
// struct with an embedded field (multi-element path [1,0] via FieldByName
// and FieldByIndex), and compares stdout byte-for-byte with gc.
func TestReflectStructFieldIndex(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"go.mod": "module example.com/reflectindex\n\ngo 1.27\n",
		"main.go": `package main

import (
	"fmt"
	"reflect"
)

type Inner struct {
	X int
}

type Outer struct {
	Y string
	Inner
}

type Plain struct {
	A int
	B string
}

func main() {
	// Plain struct: single-element Index path [0].
	t := reflect.TypeOf(Plain{})
	f := t.Field(0)
	fmt.Println(f.Index[0], len(f.Index))
	for _, i := range f.Index {
		fmt.Println(i)
	}
	fmt.Println(f.Index)
	// Embedded field at position 1: promoted X has Index [1, 0].
	ot := reflect.TypeOf(Outer{})
	xf, _ := ot.FieldByName("X")
	fmt.Println(xf.Index[0], xf.Index[1], len(xf.Index))
	fmt.Println(xf.Index)
	xf2 := ot.FieldByIndex([]int{1, 0})
	// FieldByIndex returns the field with Index relative to the innermost
	// struct ([0], len 1) — NOT the full [1, 0] path: indexing xf2.Index[1]
	// panics under gc, so only Index[0] is readable here.
	fmt.Println(xf2.Index[0], len(xf2.Index))
	fmt.Println(xf2.Index)
	yf := ot.Field(0)
	fmt.Println(yf.Index[0], len(yf.Index))
}
`,
	})

	merged, err := MergeDir(dir)
	if err != nil {
		t.Fatalf("MergeDir: %v", err)
	}
	mergedPath := filepath.Join(t.TempDir(), "merged_reflectindex.go")
	if err := os.WriteFile(mergedPath, []byte(merged), 0644); err != nil {
		t.Fatal(err)
	}
	progPath := filepath.Join(t.TempDir(), "reflectindexprog")
	if res := runCommand(t, dir, goTool(t), "build", "-o", progPath, "."); res.code != 0 {
		t.Fatalf("go build: exit=%d stderr=%q", res.code, res.stderr)
	}
	gc := runCommand(t, dir, progPath)
	gr := graalgoRun(t, mergedPath)
	const want = "0 1\n0\n[0]\n1 0 2\n[1 0]\n0 1\n[0]\n0 1\n"
	if gc.stdout != want {
		t.Fatalf("gc oracle moved: stdout=%q want=%q", gc.stdout, want)
	}
	if gr.code != gc.code || gr.stdout != gc.stdout {
		t.Fatalf("gc divergence (StructField.Index):\n gc exit=%d stdout=%q stderr=%q\n gr exit=%d stdout=%q stderr=%q",
			gc.code, gc.stdout, gc.stderr, gr.code, gr.stdout, gr.stderr)
	}
	t.Logf("StructField.Index: gc and graalgo agree on %q", want)
}
