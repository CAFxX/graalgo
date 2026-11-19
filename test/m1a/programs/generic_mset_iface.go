package main

// Method sets of generic types reached through interfaces: the type's own
// value and pointer methods, methods promoted from an embedded instance,
// and instantiations nested in other types.

type Pair[K comparable, V any] struct {
	Key K
	Val V
}

func (p Pair[K, V]) First() K  { return p.Key }
func (p *Pair[K, V]) Put(v V)  { p.Val = v }
func (p Pair[K, V]) Second() V { return p.Val }

type Firster[K any] interface{ First() K }
type Putter[V any] interface{ Put(V) }
type Both[K, V any] interface {
	Firster[K]
	Putter[V]
	Second() V
}

type Wrap[T any] struct {
	Pair[string, T]
	n int
}

type WrapPtr struct {
	*Pair[int, string]
}

type Stack[T any] []T

func (s *Stack[T]) Push(v T) { *s = append(*s, v) }
func (s Stack[T]) Len() int  { return len(s) }

type Lener interface{ Len() int }
type Pusher[T any] interface{ Push(T) }

func describe(x any) {
	if f, ok := x.(Firster[string]); ok {
		println("Firster[string]", f.First())
	}
	if f, ok := x.(Firster[int]); ok {
		println("Firster[int]", f.First())
	}
	if p, ok := x.(Putter[int]); ok {
		p.Put(99)
		println("Putter[int]")
	}
	if p, ok := x.(Putter[string]); ok {
		p.Put("put")
		println("Putter[string]")
	}
	if b, ok := x.(Both[string, int]); ok {
		println("Both[string,int]", b.First(), b.Second())
	}
	if l, ok := x.(Lener); ok {
		println("Lener", l.Len())
	}
}

func main() {
	var f Firster[string] = Pair[string, int]{"k", 1}
	println(f.First())
	pp := &Pair[string, int]{"q", 2}
	var b Both[string, int] = pp
	b.Put(7)
	println(b.First(), b.Second(), pp.Val)

	describe(Pair[string, int]{"v", 3})
	describe(pp)
	describe(Pair[int, string]{4, "s"})
	describe(&Pair[int, string]{5, "t"})
	w := &Wrap[int]{Pair[string, int]{"w", 6}, 0}
	describe(*w)
	describe(w)
	println(w.Val)
	describe(WrapPtr{&Pair[int, string]{8, "u"}})

	s := &Stack[string]{}
	var p Pusher[string] = s
	p.Push("a")
	p.Push("b")
	describe(s)
	describe(*s)
	describe(Stack[int]{1, 2, 3})
}
