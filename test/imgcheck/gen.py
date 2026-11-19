#!/usr/bin/env python3
"""gen.py — writes the type corpus of the imgcheck gate (corpus.go).

Usage: gen.py > corpus.go   (run.sh fails when corpus.go is stale)

The corpus is every root type below, each reached as *T so that interface
types are covered too; the walker (main.go) follows every descriptor
reachable from them. Hand-written declarations cover what a generator cannot
vary meaningfully (method sets, embedding, tags, generics, local types,
layout corner cases); the generated part crosses a list of base types with
every type constructor.
"""

import re

DECLS = r'''
// --- defined basic types, with and without methods --------------------------

type MyInt int
type MyString string
type MyFloat float64
type MyBool bool
type myUnexported uint16
type MyBytes []byte
type MyFunc func(int) string
type MyChan chan int
type MyMap map[string]MyInt
type MyArray [4]uint32
type MyPtr *MyInt
type MyUnsafe unsafe.Pointer
type MyComplex complex64

func (m MyInt) String() string  { return "MyInt" }
func (m *MyInt) Set(v int)      { *m = MyInt(v) }
func (m MyInt) unexported() int { return int(m) }
func (m MyString) Len() int     { return len(m) }
func (b MyBytes) Len() int      { return len(b) }
func (f MyFunc) Call(i int) string {
	return f(i)
}
func (m MyMap) Get(k string) (MyInt, bool) { v, ok := m[k]; return v, ok }

// --- structs: layout corners -----------------------------------------------

type Empty struct{}
type OneInt struct{ X int }
type OneFloat struct{ F float64 }
type OnePtr struct{ P *int }
type OneString struct{ S string }
type Padded struct {
	A int8
	B int64
	C int8
}
type Packed struct {
	A, B int32
	C    int64
}
type Blank struct {
	A int32
	_ int32
	B int64
}
type TrailingZero struct {
	A int64
	Z struct{}
}
type LeadingZero struct {
	Z [0]int64
	A int8
}
type Mixed struct {
	B   bool
	I8  int8
	U16 uint16
	I32 int32
	F32 float32
	U64 uint64
	C64 complex64
	C   complex128
	S   string
	P   *Mixed
	I   any
	E   error
	Sl  []byte
	M   map[string]int
	Ch  chan int
	Fn  func()
	U   unsafe.Pointer
	Arr [3]int16
}
type Tagged struct {
	Name  string `json:"name,omitempty" xml:"n"`
	Value int    `json:"value"`
	raw   []byte `quote:"a \"b\" \\ c"`
	Uni   bool   "uni:\"héllo\""
}
type unexportedStruct struct {
	a int
	b string
}
type Nested struct {
	In  Padded
	Arr [2]Packed
	P   *Nested
	Un  unexportedStruct
}
type Tree struct {
	Left, Right *Tree
	Kids        []Tree
	ByName      map[string]*Tree
	Visit       func(*Tree) bool
}
type Big struct {
	Words [200]*int
	Tail  int
}
type BigNoPtr struct{ Words [4096]int64 }
type BigMixed struct {
	Head [130]uintptr
	P    *int
	Rest [10]string
}

// --- embedding and promoted methods ------------------------------------------

type Base struct{ ID int }

func (b Base) Ident() int      { return b.ID }
func (b *Base) SetID(id int)   { b.ID = id }
func (b Base) hidden() string  { return "h" }

type Derived struct {
	Base
	Name string
}

func (d Derived) Describe() string { return d.Name }

type DerivedPtr struct {
	*Base
	extra int
}
type EmbedIface struct {
	error
	Stringer
	n int
}
type EmbedBasic struct {
	MyInt
	int
	string
	*MyString
}
type Deep struct{ Derived }
type Shadow struct {
	Base
	ID string
}

// --- interfaces ----------------------------------------------------------------

type Stringer interface{ String() string }
type Setter interface{ Set(v int) }
type StringSetter interface {
	Stringer
	Setter
}
type private interface {
	hidden() string
	Ident() int
}
type Wide interface {
	Zeta(a int, b ...string) (int, error)
	Alpha()
	beta(x [2]int) *Base
	Mid(func(int) bool) chan<- int
	alpha() MyInt
}
type ErrorPlus interface {
	error
	Code() int
}
type IfaceOfIface interface {
	Get() Stringer
	Put(Stringer, any, error)
}

// --- generics ----------------------------------------------------------------------

type Pair[K comparable, V any] struct {
	Key K
	Val V
}

func (p Pair[K, V]) First() K   { return p.Key }
func (p *Pair[K, V]) Put(v V)   { p.Val = v }

type List[T any] struct {
	head *node[T]
	n    int
}
type node[T any] struct {
	v    T
	next *node[T]
}

func (l *List[T]) Len() int { return l.n }

type Num interface{ ~int | ~float64 }
type Vec[T Num] []T

func (v Vec[T]) Sum() T {
	var s T
	for _, x := range v {
		s += x
	}
	return s
}

type Opt[T any] struct {
	ok bool
	v  T
}
type Fn[A, B any] func(A) B
type Table[K comparable, V any] map[K][]V

// --- local types ---------------------------------------------------------------------

func locals() []any {
	type Local struct{ a, b int }
	type localIface interface{ m() }
	var out []any
	out = append(out, p[Local](), p[localIface]())
	{
		type Local struct{ s string }
		out = append(out, p[Local](), p[[]Local]())
	}
	return out
}

func genericLocals[T any]() []any {
	type Box struct{ v T }
	return []any{p[Box](), p[map[string]Box]()}
}
'''

BASES = [
    "bool", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
    "float32", "float64", "complex64", "complex128", "string", "unsafe.Pointer", "error", "any",
    "MyInt", "MyString", "MyFloat", "myUnexported", "Empty", "OneInt", "Padded", "Mixed", "Derived", "Stringer",
    "*Base", "[]byte", "struct{}", "[3]string", "Pair[string, int]", "func()",
]

# Type constructors over a base type %s.
CTORS = [
    "*%s", "[]%s", "[0]%s", "[1]%s", "[5]%s", "map[string]%s", "chan %s", "<-chan %s", "chan<- %s",
    "func(%s)", "func() %s", "func(int, ...%s) (%s, error)",
    "struct{ A %s; b int8 }", "struct{ %s }" , "interface{ M(%s) %s }", "**%s", "[][]%s", "[2][2]%s",
    "map[int]*%s", "Opt[%s]",
]

EXTRA = [
    # hand-written types and their pointers' method sets
    "MyBool", "MyBytes", "MyFunc", "MyChan", "MyMap", "MyArray", "MyPtr", "MyUnsafe", "MyComplex",
    "OneFloat", "OnePtr", "OneString", "Packed", "Blank", "TrailingZero", "LeadingZero", "Tagged",
    "unexportedStruct", "Nested", "Tree", "Big", "BigNoPtr", "BigMixed", "Base", "DerivedPtr", "EmbedIface",
    "EmbedBasic", "Deep", "Shadow", "Setter", "StringSetter", "private", "Wide", "ErrorPlus", "IfaceOfIface",
    # generic instances
    "Pair[int, string]", "Pair[MyInt, *Base]", "Pair[string, Pair[int, int]]", "List[int]", "List[Stringer]",
    "Vec[int]", "Vec[float64]", "Vec[MyInt]", "Opt[error]", "Opt[[]Opt[int]]", "Fn[int, string]",
    "Fn[*Base, error]", "Table[string, int]", "Table[Pair[int, int], *Tree]",
    # map keys of every hashing class, indirect keys and elems, zero-size slots
    "map[int]int", "map[string]string", "map[float64]bool", "map[any]any", "map[error]int", "map[*int]struct{}",
    "map[struct{}]struct{}", "map[[2]string]int", "map[Padded]Padded", "map[[17]int64]int", "map[int][17]int64",
    "map[[40]int32][200]byte", "map[MyString]MyMap", "map[Pair[int, string]]bool", "map[complex128]chan int",
    "map[uint8]uint8", "map[OneFloat][3]any", "map[chan int]func()", "map[unsafe.Pointer]uintptr",
    "map[struct{ a any; b string }][]int",
    # functions
    "func(int, string) bool", "func(...int)", "func(a, b float64) (x, y float64)", "func(func(int) int) func(int) int",
    "func(*Tree, map[string]Tree, ...any) (Stringer, error)", "func(chan<- int, <-chan string)",
    # channels of channels, arrays of arrays, deep pointers
    "chan (<-chan int)", "chan<- chan int", "<-chan chan<- string", "chan chan chan bool",
    "[3][4][5]int8", "[2]map[string][2]int", "[]map[int][]map[int]string", "***int", "*[]*[2]*string",
    "[129]*int", "[130]string", "[1024]any", "[16]unsafe.Pointer", "[100000]struct{}",
    # anonymous structs and interfaces
    "struct{ a, b, c int8; d int64 }", "struct{ _ int; _ string }", "struct{ F func(); M map[int]int; s []int }",
    "struct{ Base; *Derived; x int }", "struct{ sync sortIface }",
    "struct{ A int `k:\"v\"`; B int `k:\"v\"` }", "struct{ A [0]func(); B int }",
    "interface{ String() string; Error() string }", "interface{ a(); B(); c(); D() }", "interface{ Stringer; private }",
    "interface{ M(interface{ N() }) }",
    # standard-library types the corpus program links
    "sort.IntSlice", "sort.StringSlice", "sort.Float64Slice", "sortIface",
]


EMBEDDABLE = re.compile(r"^\*?[A-Za-z_][A-Za-z0-9_.]*$")


def main():
    roots = []
    for b in BASES:
        roots.append(b)
        for c in CTORS:
            if c == "struct{ %s }" and (b == "unsafe.Pointer" or not EMBEDDABLE.match(b)):
                continue  # only a (pointer to a) type name can be embedded
            roots.append(c.replace("%s", b))
    roots += EXTRA
    seen, uniq = set(), []
    for r in roots:
        if r not in seen:
            seen.add(r)
            uniq.append(r)
    out = []
    out.append("// Code generated by test/imgcheck/gen.py. DO NOT EDIT.\n")
    out.append("package main\n")
    out.append('import (\n\t"sort"\n\t"unsafe"\n)\n')
    out.append("// p is the root for type T: *T, so that an interface type is reachable too.")
    out.append("func p[T any]() any { return (*T)(nil) }\n")
    out.append("type sortIface = sort.Interface\n")
    out.append(DECLS.strip("\n") + "\n")
    out.append("// roots: %d root types (plus the local ones)." % len(uniq))
    out.append("var roots = append(append(locals(), genericLocals[int]()...), genericLocals[*Base]()...)\n")
    out.append("func init() {\n\troots = append(roots,")
    for r in uniq:
        out.append("\t\tp[%s]()," % r)
    out.append("\t)\n}\n")
    out.append('''// equalPairs: two distinct values per type whose descriptor has an equality
// function that takes pointers to the values.
var equalPairs = [][2]any{
	{int8(1), int8(2)},
	{uint16(1), uint16(2)},
	{int64(1) << 40, int64(2)},
	{float32(1.5), float32(2.5)},
	{0.0, 1.0},
	{complex(1, 2), complex(1, 3)},
	{"a", "b"},
	{"", "longer string"},
	{MyInt(3), MyInt(4)},
	{MyString("x"), MyString("y")},
	{Padded{1, 2, 3}, Padded{1, 2, 4}},
	{Blank{A: 1, B: 2}, Blank{A: 1, B: 3}},
	{Packed{1, 2, 3}, Packed{1, 9, 3}},
	{[3]string{"a", "b", "c"}, [3]string{"a", "b", "d"}},
	{[2][2]int8{{1, 2}, {3, 4}}, [2][2]int8{{1, 2}, {3, 5}}},
	{OneFloat{1}, OneFloat{2}},
	{OneString{"s"}, OneString{"t"}},
	{Derived{Base{1}, "n"}, Derived{Base{2}, "n"}},
	{Pair[string, int]{"k", 1}, Pair[string, int]{"k", 2}},
	{struct{ a any; b error }{1, nil}, struct{ a any; b error }{"1", nil}},
	{struct{ p *int; s string }{nil, "x"}, struct{ p *int; s string }{new(int), "x"}},
	{[1]any{MyInt(1)}, [1]any{1}},
	{Empty{}, Empty{}},
	{[]int(nil), []int{1}},
	{map[string]int(nil), map[string]int{}},
	{Tree{}, Tree{}},
}''')
    print("\n".join(out))


if __name__ == "__main__":
    main()
