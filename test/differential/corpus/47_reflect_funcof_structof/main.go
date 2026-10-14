package main

import (
	"fmt"
	"reflect"
)

type E struct{ X int }

func (e E) Hello() string   { return fmt.Sprintf("hello %d", e.X) }
func (e *E) Pong(n int) int { return e.X + n }

type Iface interface{ M() int }

func show(t reflect.Type) {
	fmt.Printf("kind=%v str=%q\n", t.Kind(), t.String())
}

func main() {
	// Control: New(T).Elem() on a NAMED struct (pre-existing reflectElem
	// POINTER-case bug: Cell-unwrapped referent hit a GoPointer cast).
	nv := reflect.New(reflect.TypeOf(E{})).Elem()
	nv.Field(0).SetInt(7)
	fmt.Println("named new-elem:", nv.Field(0).Interface())

	// FuncOf basics
	f1 := reflect.FuncOf([]reflect.Type{reflect.TypeOf(0), reflect.TypeOf("")}, []reflect.Type{reflect.TypeOf(0.0)}, false)
	show(f1)
	fmt.Println("variadic:", f1.IsVariadic(), "in:", f1.NumIn(), "out:", f1.NumOut())
	f2 := reflect.FuncOf([]reflect.Type{reflect.TypeOf([]int{})}, nil, true)
	fmt.Println("f2:", f2.String(), "variadic:", f2.IsVariadic(), "in0:", f2.In(0))
	f3 := reflect.FuncOf([]reflect.Type{reflect.TypeOf(0), reflect.TypeOf("")}, []reflect.Type{reflect.TypeOf(0.0)}, false)
	fmt.Println("canonical:", f1 == f3, "ptr:", reflect.PtrTo(f1).String())

	// FuncOf panics
	try(func() { reflect.FuncOf([]reflect.Type{nil}, nil, false) })
	try(func() { reflect.FuncOf([]reflect.Type{reflect.TypeOf(0)}, nil, true) })
	try(func() { reflect.FuncOf(nil, nil, true) })

	// StructOf basics
	s1 := reflect.StructOf([]reflect.StructField{
		{Name: "A", Type: reflect.TypeOf(0)},
		{Name: "b", Type: reflect.TypeOf(""), PkgPath: "main"},
		{Name: "C", Type: reflect.TypeOf(0.0), Tag: `json:"c"`},
	})
	show(s1)
	f, ok := s1.FieldByName("b")
	fmt.Println("field b:", ok, f.Name, f.Type, f.PkgPath, f.Offset)
	fmt.Println("numfield:", s1.NumField(), "field1off:", s1.Field(1).Offset, "field2off:", s1.Field(2).Offset, "size:", s1.Size(), "align:", s1.Align())

	// StructOf panics
	try(func() { reflect.StructOf([]reflect.StructField{{Type: reflect.TypeOf(0)}}) })
	try(func() {
		reflect.StructOf([]reflect.StructField{{Name: "A", Type: reflect.TypeOf(0)}, {Name: "A", Type: reflect.TypeOf("")}})
	})
	try(func() { reflect.StructOf([]reflect.StructField{{Name: "a", Type: reflect.TypeOf(0)}}) })
	try(func() { reflect.StructOf([]reflect.StructField{{Name: "a", Type: reflect.TypeOf(0), Anonymous: true}}) })

	// Embedded promotion
	s2 := reflect.StructOf([]reflect.StructField{{Name: "E", Type: reflect.TypeOf(E{}), Anonymous: true}})
	show(s2)
	m, ok := s2.MethodByName("Hello")
	fmt.Println("promoted Hello:", ok, m.Name, m.Func.IsValid())
	v := reflect.New(s2).Elem()
	v.Field(0).Field(0).SetInt(42)
	out := m.Func.Call([]reflect.Value{v})
	fmt.Println("call:", out[0].Interface())
	s3 := reflect.StructOf([]reflect.StructField{{Name: "E", Type: reflect.PtrTo(reflect.TypeOf(E{})), Anonymous: true}})
	m2, ok2 := s3.MethodByName("Pong")
	fmt.Println("ptr-embed Pong:", ok2, m2.Name)
	v3 := reflect.New(s3).Elem()
	v3.Field(0).Set(reflect.ValueOf(&E{X: 7}))
	out3 := m2.Func.Call([]reflect.Value{v3, reflect.ValueOf(3)})
	fmt.Println("call3:", out3[0].Interface())
	_, ok4 := s3.MethodByName("Hello")
	fmt.Println("ptr-embed value method:", ok4)

	// MakeFunc on synthesized type
	mv := reflect.MakeFunc(f1, func(args []reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(float64(args[0].Interface().(int)) + float64(len(args[1].Interface().(string))))}
	})
	fmt.Println("makefunc:", mv.Call([]reflect.Value{reflect.ValueOf(3), reflect.ValueOf("ab")})[0].Interface())

	// New + Set on synthesized struct
	sv := reflect.New(s1).Elem()
	sv.FieldByName("A").SetInt(9)
	fmt.Println("setfield:", sv.FieldByName("A").Interface())
	fmt.Println("tag:", s1.Field(2).Tag.Get("json"))
}

func try(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("panic:", r)
		}
	}()
	fn()
	fmt.Println("NO PANIC")
}
