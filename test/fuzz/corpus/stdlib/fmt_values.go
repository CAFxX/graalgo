package main

// fmt over the value shapes the generator builds (toolexec M1d): %v, %+v,
// %#v and %T of nested structs, maps, slices, arrays, pointers, interfaces
// and named types with methods; width/precision on numbers; Sprintf round
// trips through strconv.

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
)

type Color int

func (c Color) String() string { return [...]string{"red", "green", "blue"}[c%3] }

type Point struct{ X, Y int }

type Shape struct {
	Name   string
	At     Point
	Tags   []string
	Meta   map[string]any
	Parent *Shape
	Hue    Color
	hidden float64
}

type wrapped struct{ err error }

func (w wrapped) Error() string { return "wrapped(" + w.err.Error() + ")" }
func (w wrapped) Unwrap() error { return w.err }

func main() {
	base := errors.New("base")
	s := Shape{"sq", Point{1, -2}, []string{"a", "b"}, map[string]any{"k": 1, "z": []int{1, 2}, "a": nil}, nil, 4, 2.5}
	t := Shape{Name: "child", Parent: &s, Hue: 2}
	for _, verb := range []string{"%v", "%+v", "%T"} {
		fmt.Printf(verb+"\n", s)
		fmt.Printf(verb+"\n", []any{s.At, &s.At == nil, s.Tags, s.Meta, s.Hue, [2]Point{{1, 2}, {3, 4}}})
	}
	fmt.Printf("%#v\n", s.At)
	fmt.Printf("%#v %#v %#v\n", s.Tags, s.Hue, map[Color]Point{1: {5, 6}})
	fmt.Printf("%v %s %d %x\n", t.Hue, t.Hue, t.Hue, t.Hue)
	fmt.Println(t.Parent.Name, t.Parent == &s, t.Parent.Meta["z"])
	var e error = wrapped{fmt.Errorf("ctx %d: %w", 7, base)}
	fmt.Println(e, errors.Is(e, base), errors.Unwrap(errors.Unwrap(e)) == base)
	for _, f := range []float64{0, -0.5, 1e-9, 123456789.125, 1e100} {
		fmt.Printf("%v|%8.3f|%-12.4e|%g|%s\n", f, f, f, f, strconv.FormatFloat(f, 'g', -1, 64))
	}
	for _, i := range []int64{0, -1, 1 << 40, -1 << 63} {
		fmt.Printf("%d|%6d|%-6d|%+d|%x|%o|%b|%c|%q|%U\n", i, i, i, i, i, uint8(i), uint8(i), rune(i&0x7f), rune(i&0x7f), rune(i&0x7f))
	}
	keys := []string{}
	for k := range s.Meta {
		keys = append(keys, fmt.Sprintf("%q", k))
	}
	sort.Strings(keys)
	n, err := strconv.Atoi(fmt.Sprintf("%03d", 42))
	fmt.Fprintln(os.Stdout, keys, n, err, fmt.Sprint(nil, []byte("hi"), struct{}{}, [0]int{}, (*int)(nil), any(nil)))
	fmt.Printf("%v %v\n", struct {
		A any
		B fmt.Stringer
		C error
	}{}, [][]map[string][]Point{{{"p": {{1, 1}}}}})
	fmt.Printf("%5t|%-5t|%q|%x|% x|%10.3s|%-10s|\n", true, false, "q\"uote\n", "hex", "hex", "truncate", "left")
}
