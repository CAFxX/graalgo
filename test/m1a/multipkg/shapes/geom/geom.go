// Package geom is the library half of the multi-package gate program.
package geom

type Shape interface {
	Area() int
	name() string
}

type Point struct{ X, Y int }

type Rect struct {
	Min, Max Point
	label    string
}

var Count = initCount()

var registry = map[string]int{}

func initCount() int { return 10 }

func init() {
	registry["init"] = Count
	Count++
}

func NewRect(w, h int, label string) *Rect {
	Count++
	registry[label] = w * h
	return &Rect{Max: Point{w, h}, label: label}
}

func (r *Rect) Area() int    { return (r.Max.X - r.Min.X) * (r.Max.Y - r.Min.Y) }
func (r *Rect) name() string { return r.label }

func Describe(s Shape) string { return s.name() }

func Registered(label string) (int, bool) {
	v, ok := registry[label]
	return v, ok
}

type List[T any] struct{ items []T }

func (l *List[T]) Add(v T) *List[T] { l.items = append(l.items, v); return l }
func (l *List[T]) Len() int         { return len(l.items) }

func Reduce[T, A any](l *List[T], acc A, f func(A, T) A) A {
	for _, v := range l.items {
		acc = f(acc, v)
	}
	return acc
}
