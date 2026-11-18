// Package dep is imported by package all, so that the golden vectors
// exercise references to another package's objects (Ext, SymRefs).
package dep

var Counter int

type Box[T any] struct{ V T }

func (b Box[T]) Get() T { return b.V }

func Wrap[T any](v T) Box[T] { return Box[T]{V: v} }

func Hello() string { return "hello" }
