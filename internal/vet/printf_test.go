package vet

import (
	"strings"
	"testing"
)

// P0-J(1): scanf-family args are pointers by contract. fmt.Sscanf(s, "%d", &n)
// must not be flagged "expects integer" — the analyzer must dereference one
// pointer level before verb checking.
func TestPrintfScanfDerefPointer(t *testing.T) {
	src := `package main

import (
	"fmt"
	"strings"
)

func main() {
	s := "42"
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		fmt.Println(err)
	}
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		fmt.Println(err)
	}
	r := strings.NewReader("7")
	var m int
	if _, err := fmt.Fscanf(r, "%d", &m); err != nil {
		fmt.Println(err)
	}
	var k int
	if _, err := fmt.Scanf("%d", &k); err != nil {
		fmt.Println(err)
	}
	_ = n
	_ = f
	_ = m
	_ = k
}
`
	joined := runVetOnSrc(t, src)
	for _, w := range []string{"expects integer", "expects floating point"} {
		if strings.Contains(joined, w) {
			t.Errorf("false positive: scanf %%d/%%f with pointer arg flagged %q; diagnostics:\n%s", w, joined)
		}
	}
}

// P0-J(2a): %s/%q accept named slice-of-uint8 types via their underlying type.
// `type B []byte` must not be flagged.
func TestPrintfByteSliceNamed(t *testing.T) {
	src := `package main

import "fmt"

type B []byte

func main() {
	var b B = B("hi")
	fmt.Printf("%s", b)
	fmt.Printf("%q", b)
	fmt.Printf("%s", []byte("hi"))
}
`
	joined := runVetOnSrc(t, src)
	if strings.Contains(joined, "expects string or []byte") {
		t.Errorf("false positive: %%s/%%q with []byte-kind flagged; diagnostics:\n%s", joined)
	}
}

// P0-J(2b): %s/%q accept types implementing fmt.Stringer.
func TestPrintfStringer(t *testing.T) {
	src := `package main

import "fmt"

type money struct{ cents int }

func (m money) String() string { return "money" }

func main() {
	var m money
	fmt.Printf("%s", m)
	fmt.Printf("%q", m)
	fmt.Printf("%s", &m)
}
`
	joined := runVetOnSrc(t, src)
	if strings.Contains(joined, "expects string or []byte") {
		t.Errorf("false positive: %%s/%%q with Stringer flagged; diagnostics:\n%s", joined)
	}
}

// Negative control: %d with a string arg MUST still be flagged, in both
// printf and scanf modes. If this stops flagging, the checks went slack.
func TestPrintfNegativeControl(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	fmt.Printf("%d", "hello")
	s := "abc"
	var str string
	if _, err := fmt.Sscanf(s, "%d", &str); err != nil {
		fmt.Println(err)
	}
}
`
	joined := runVetOnSrc(t, src)
	if n := strings.Count(joined, "expects integer"); n != 2 {
		t.Errorf("negative control broken: want 2 %q diagnostics, got %d; diagnostics:\n%s",
			"expects integer", n, joined)
	}
}
