package main

import (
	"fmt"
	"reflect"
)

type E struct{ X int }

func (e E) Get() int        { return e.X }
func (e E) Pub() string     { return fmt.Sprintf("pub %d", e.X) }
func (e E) unexp() int      { return e.X + 1 }
func (e *E) Ptr() int       { return e.X + 1 }

type MyInt int

func (m MyInt) Get() int  { return int(m) }
func (m *MyInt) Ptr() int { return int(*m) + 1 }

type MyChan chan int

func (c MyChan) Get() int { return 1 }

type MyFunc func() int

func (f MyFunc) Get() int { return 2 }

type Iface interface{ M() int }

type IfaceExp interface {
	M() int
	unexp() int
}

type I2 interface {
	IfaceExp
	Extra() int
}

func methods(t reflect.Type) {
	fmt.Printf("numMethod=%d [", t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		fmt.Printf("%s:%q:%s ", m.Name, m.PkgPath, m.Type.String())
	}
	fmt.Println("]")
}

func try(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("%s panic: %v\n", name, r)
		}
	}()
	fn()
	fmt.Printf("%s NO PANIC\n", name)
}

func main() {
	// A: struct{E} — value promotion incl. unexported
	sA := reflect.StructOf([]reflect.StructField{{Name: "E", Type: reflect.TypeOf(E{}), Anonymous: true}})
	methods(sA)
	// A-call: Get on a value
	vA := reflect.New(sA).Elem()
	vA.Field(0).Field(0).SetInt(42)
	mA, _ := sA.MethodByName("Get")
	fmt.Println("A-call:", mA.Func.Call([]reflect.Value{vA})[0].Interface())
	// A-mtype: Method(0).Type.String() — the C3 dispatch that died on the itab
	m0 := sA.Method(0)
	fmt.Println("A-mtype:", m0.Type.String())
	fmt.Printf("A-types: sA %%T=%T m.Type %%T=%T\n", sA, m0.Type)
	// B: struct{*E} — pointer set then elem set, duplicates kept
	sB := reflect.StructOf([]reflect.StructField{{Name: "E", Type: reflect.PtrTo(reflect.TypeOf(E{})), Anonymous: true}})
	methods(sB)
	mB, _ := sB.MethodByName("Get")
	fmt.Println("B-first-Get-index:", mB.Index)
	// B-call: value-receiver method via *E embed
	vB := reflect.New(sB).Elem()
	vB.Field(0).Set(reflect.ValueOf(&E{X: 7}))
	fmt.Println("B-call:", mB.Func.Call([]reflect.Value{vB})[0].Interface())
	// C: struct{MyInt}
	sC := reflect.StructOf([]reflect.StructField{{Name: "MyInt", Type: reflect.TypeOf(MyInt(0)), Anonymous: true}})
	methods(sC)
	// D: struct{*MyInt} — elem methods appended for any kind
	sD := reflect.StructOf([]reflect.StructField{{Name: "MyInt", Type: reflect.PtrTo(reflect.TypeOf(MyInt(0))), Anonymous: true}})
	methods(sD)
	// F: struct{MyFunc}
	sF := reflect.StructOf([]reflect.StructField{{Name: "MyFunc", Type: reflect.TypeOf(MyFunc(nil)), Anonymous: true}})
	methods(sF)
	// G: struct{MyChan}
	sG := reflect.StructOf([]reflect.StructField{{Name: "MyChan", Type: reflect.TypeOf(MyChan(nil)), Anonymous: true}})
	methods(sG)
	// I: struct{Iface} + IM-call: stub panics on call
	sI := reflect.StructOf([]reflect.StructField{{Name: "Iface", Type: reflect.TypeOf((*Iface)(nil)).Elem(), Anonymous: true}})
	methods(sI)
	try("IM-call", func() {
		mI, _ := sI.MethodByName("M")
		mI.Func.Call([]reflect.Value{reflect.New(sI).Elem()})
	})
	// J: struct{IfaceExp} — no panic at StructOf despite unexported method
	sJ := reflect.StructOf([]reflect.StructField{{Name: "IfaceExp", Type: reflect.TypeOf((*IfaceExp)(nil)).Elem(), Anonymous: true}})
	methods(sJ)
	try("J-call", func() {
		mJ, _ := sJ.MethodByName("M")
		mJ.Func.Call([]reflect.Value{reflect.New(sJ).Elem()})
	})
	// K/L/M: Issue-15924 position/count panics (gc-faithful)
	try("K", func() {
		reflect.StructOf([]reflect.StructField{{Name: "E", Type: reflect.PtrTo(reflect.TypeOf(E{})), Anonymous: true}, {Name: "X", Type: reflect.TypeOf(0)}})
	})
	try("L", func() {
		reflect.StructOf([]reflect.StructField{{Name: "X", Type: reflect.TypeOf(0)}, {Name: "E", Type: reflect.TypeOf(E{}), Anonymous: true}})
	})
	try("M", func() {
		reflect.StructOf([]reflect.StructField{{Name: "X", Type: reflect.TypeOf(0)}, {Name: "E", Type: reflect.PtrTo(reflect.TypeOf(E{})), Anonymous: true}})
	})
	// N: struct{E; X int} — non-pointer first field, multi-field: OK
	sN := reflect.StructOf([]reflect.StructField{{Name: "E", Type: reflect.TypeOf(E{}), Anonymous: true}, {Name: "X", Type: reflect.TypeOf(0)}})
	methods(sN)
	// P: struct{MyInt; X int} — OK
	sP := reflect.StructOf([]reflect.StructField{{Name: "MyInt", Type: reflect.TypeOf(MyInt(0)), Anonymous: true}, {Name: "X", Type: reflect.TypeOf(0)}})
	methods(sP)
	// V3/V4: direct-iface embeds in multi-field structs panic even with methods
	try("V3", func() {
		reflect.StructOf([]reflect.StructField{{Name: "MyChan", Type: reflect.TypeOf(MyChan(nil)), Anonymous: true}, {Name: "X", Type: reflect.TypeOf(0)}})
	})
	try("V4", func() {
		reflect.StructOf([]reflect.StructField{{Name: "MyFunc", Type: reflect.TypeOf(MyFunc(nil)), Anonymous: true}, {Name: "X", Type: reflect.TypeOf(0)}})
	})
	// V8: struct{*MyInt; X int} — pointer + multi-field panic
	try("V8", func() {
		reflect.StructOf([]reflect.StructField{{Name: "MyInt", Type: reflect.PtrTo(reflect.TypeOf(MyInt(0))), Anonymous: true}, {Name: "X", Type: reflect.TypeOf(0)}})
	})
	// V5: interface embedding an interface promotes all methods
	sV5 := reflect.StructOf([]reflect.StructField{{Name: "I2", Type: reflect.TypeOf((*I2)(nil)).Elem(), Anonymous: true}})
	methods(sV5)
	// UNEXP: promoted unexported method is findable, PkgPath ""
	mU, okU := sA.MethodByName("unexp")
	fmt.Printf("UNEXP ok=%v PkgPath=%q Index=%d\n", okU, mU.PkgPath, mU.Index)
	// IMP: StructOf product implements interfaces (computed method set)
	ifaceGet := reflect.TypeOf((*interface{ Get() int })(nil)).Elem()
	ifaceNope := reflect.TypeOf((*interface{ Nope() })(nil)).Elem()
	fmt.Println("IMP Get:", sA.Implements(ifaceGet), "Nope:", sA.Implements(ifaceNope))
	// BND: Method/MethodVal index out of range
	try("BND-Method", func() { sA.Method(99) })
	vAv := reflect.New(sA).Elem()
	try("BND-MethodVal", func() { vAv.Method(99) })
	try("BND-neg", func() { sA.Method(-1) })
}
