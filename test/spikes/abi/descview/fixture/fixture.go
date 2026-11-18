// Package fixture declares the types the descriptor-view prototype
// synthesizes gc-layout descriptors for. typetab reads this file with
// go/types (the frontend's view); imgcheck imports it (gc's view).
package fixture

import "strconv"

type Inner struct {
	A int8
	B float64
}

func (i Inner) Sum() float64 { return float64(i.A) + i.B }

type P struct {
	X     int
	Y     string `json:"y,omitempty" k:"v"`
	Z     []byte
	In    Inner
	Ptr   *Inner
	M     map[string]int
	F     func(int, ...string) (bool, error)
	E     error
	Any   any
	Arr   [3]uint16
	C     chan<- int
	small bool
	Inner // embedded
	N     *P
}

func (p P) Get() int         { return p.X }
func (p *P) Set(x int)       { p.X = x }
func (p P) Add(a, b int) int { return p.X + a + b }
func (p P) String() string   { return "P(" + strconv.Itoa(p.X) + ")" }
func (p P) hidden()          {}

type Stringer interface{ String() string }

type MyInt int

func (m MyInt) Double() MyInt { return m * 2 }

type Pair struct {
	K string
	V int32
	W uint8
}

type Wide struct {
	F32 float32
	I16 int16
	U64 uint64
	B   bool
	S   string
	P   *int
}

// Roots lists one value per type the prototype must describe. Element
// order is the order of the "root" records typetab emits.
var Roots = []any{
	P{},
	&P{},
	Inner{},
	Pair{},
	Wide{},
	MyInt(0),
	[]map[string]int{},
	map[MyInt][]Pair{},
	[4]*MyInt{},
	make(<-chan Pair),
	func(int, ...string) (bool, error) { return false, nil },
	struct {
		A, B int
		C    string `t:"1"`
	}{},
	(*Stringer)(nil),
	(*error)(nil),
	(*any)(nil),
	0, "", 1.5, uint8(0), true, complex64(0), uintptr(0),
	[]string{},
	[2][3]int32{},
	&[]*Pair{},
}
