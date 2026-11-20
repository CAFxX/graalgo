// reflect_probe.go — the reflect API surface, one check per line (the abi-spike
// probe test/spikes/abi/reflectprobe, 126 checks): types, method tables,
// run-time-made types, fields, sets, maps, channels, Value.Call, MakeFunc,
// method values, Select, DeepEqual. One check of the original is left out:
// panic.unexported-set, whose message names the calling reflect method
// through runtime.Callers (BUG-0047).
//
// Command reflectprobe exercises the reflect API surface one check at a
// time. Each check prints one line "name value"; a check that panics prints
// "name PANIC". The output under gc is the oracle (run.sh diffs it against
// the output under graalgo), so every line can fail.
//
// With an argument N it runs only check N (process isolation for backends
// where a failing check takes the process down); with no argument it runs
// all checks.
package main

import (
	"os"
	"reflect"
	"strconv"
	"unsafe"
)

type Inner struct {
	A int8
	B float64
}

type P struct {
	X     int
	Y     string `json:"y,omitempty" k:"v"`
	Z     []byte
	In    Inner
	Ptr   *Inner
	M     map[string]int
	F     func(int) string
	E     error
	Any   any
	Arr   [3]uint16
	C     chan int
	small bool
	Inner // embedded
}

func (p P) Get() int         { return p.X }
func (p *P) Set(x int)       { p.X = x }
func (p P) Add(a, b int) int { return p.X + a + b }
func (p P) String() string   { return "P(" + strconv.Itoa(p.X) + ")" }
func (i Inner) Sum() float64 { return float64(i.A) + i.B }

type Stringer interface{ String() string }
type MyInt int
type MyErr struct{ msg string }

func (e *MyErr) Error() string { return e.msg }

type check struct {
	name string
	f    func() string
}

func b(v bool) string    { return strconv.FormatBool(v) }
func i(v int) string     { return strconv.Itoa(v) }
func u(v uintptr) string { return strconv.FormatUint(uint64(v), 10) }

var sample = P{X: 42, Y: "hi", Z: []byte("zz"), In: Inner{1, 2.5}, M: map[string]int{"a": 1},
	Arr: [3]uint16{7, 8, 9}, Inner: Inner{3, 4.5}}

func tp() reflect.Type { return reflect.TypeOf(sample) }

var checks = []check{
	// ---- Type: header fields
	{"T.Kind.struct", func() string { return tp().Kind().String() }},
	{"T.Size.struct", func() string { return u(tp().Size()) }},
	{"T.Align", func() string { return i(tp().Align()) }},
	{"T.FieldAlign", func() string { return i(reflect.TypeOf(Inner{}).FieldAlign()) }},
	{"T.Kind.basic", func() string {
		return reflect.TypeOf(1).Kind().String() + reflect.TypeOf("").Kind().String() + reflect.TypeOf(1.5).Kind().String() +
			reflect.TypeOf(uint8(1)).Kind().String() + reflect.TypeOf(true).Kind().String() + reflect.TypeOf(complex(1, 2)).Kind().String()
	}},
	{"T.Bits", func() string { return i(reflect.TypeOf(int32(1)).Bits()) }},
	{"T.Comparable", func() string { return b(tp().Comparable()) + b(reflect.TypeOf(1).Comparable()) }},
	// ---- Type: names (Str / TFlag / UncommonType)
	{"T.String.struct", func() string { return tp().String() }},
	{"T.Name.struct", func() string { return tp().Name() }},
	{"T.PkgPath.struct", func() string { return tp().PkgPath() }},
	{"T.String.ptr", func() string { return reflect.TypeOf(&sample).String() }},
	{"T.String.slice", func() string { return reflect.TypeOf([]map[string]int{}).String() }},
	{"T.String.func", func() string {
		return reflect.TypeOf(func(int, ...string) (bool, error) { return false, nil }).String()
	}},
	{"T.String.chan", func() string { return reflect.TypeOf(make(<-chan int)).String() }},
	{"T.String.array", func() string { return reflect.TypeOf([4]*MyInt{}).String() }},
	{"T.String.anon", func() string { return reflect.TypeOf(struct{ A, B int }{}).String() }},
	{"T.Name.unnamed", func() string { return "[" + reflect.TypeOf([]int{}).Name() + "]" }},
	{"T.Name.MyInt", func() string { return reflect.TypeOf(MyInt(1)).Name() }},
	{"T.PkgPath.builtin", func() string { return "[" + reflect.TypeOf(1).PkgPath() + "]" }},
	// ---- Type: composite navigation
	{"T.Elem.ptr", func() string { return reflect.TypeOf(&sample).Elem().Name() }},
	{"T.Elem.slice", func() string { return reflect.TypeOf([]int8{}).Elem().Kind().String() }},
	{"T.Elem.map", func() string { return reflect.TypeOf(map[string][]int{}).Elem().String() }},
	{"T.Key", func() string { return reflect.TypeOf(map[MyInt]int{}).Key().String() }},
	{"T.Elem.chan", func() string { return reflect.TypeOf(make(chan string)).Elem().Kind().String() }},
	{"T.ChanDir", func() string { return reflect.TypeOf(make(chan<- int)).ChanDir().String() }},
	{"T.Len", func() string { return i(reflect.TypeOf([7]int{}).Len()) }},
	{"T.NumField", func() string { return i(tp().NumField()) }},
	{"T.Field.Name", func() string { return tp().Field(1).Name }},
	{"T.Field.Type", func() string { return tp().Field(2).Type.String() }},
	{"T.Field.Offset", func() string { return u(tp().Field(3).Offset) }},
	{"T.Field.Tag", func() string { return string(tp().Field(1).Tag) }},
	{"T.Field.Tag.Get", func() string { return tp().Field(1).Tag.Get("json") }},
	{"T.Field.Anonymous", func() string { return b(tp().Field(12).Anonymous) + tp().Field(12).Name }},
	{"T.Field.PkgPath", func() string { return tp().Field(11).PkgPath + b(tp().Field(11).IsExported()) }},
	{"T.Field.Index", func() string { return i(len(tp().Field(4).Index)) + i(tp().Field(4).Index[0]) }},
	{"T.FieldByName", func() string { f, ok := tp().FieldByName("Arr"); return f.Type.String() + b(ok) }},
	{"T.FieldByName.promoted", func() string { f, ok := tp().FieldByName("B"); return f.Type.String() + b(ok) + i(len(f.Index)) }},
	{"T.FieldByIndex", func() string { return tp().FieldByIndex([]int{3, 1}).Name }},
	{"T.NumIn/Out", func() string {
		t := reflect.TypeOf(func(int, string) error { return nil })
		return i(t.NumIn()) + i(t.NumOut()) + t.In(1).String() + t.Out(0).String()
	}},
	{"T.IsVariadic", func() string { return b(reflect.TypeOf(func(...int) {}).IsVariadic()) }},
	// ---- Type: methods (UncommonType method table)
	{"T.NumMethod.value", func() string { return i(tp().NumMethod()) }},
	{"T.NumMethod.ptr", func() string { return i(reflect.TypeOf(&sample).NumMethod()) }},
	{"T.Method.Name", func() string { return tp().Method(0).Name + tp().Method(1).Name }},
	{"T.Method.Type", func() string { return tp().Method(1).Type.String() }},
	{"T.MethodByName", func() string {
		m, ok := reflect.TypeOf(&sample).MethodByName("Set")
		return m.Name + b(ok) + m.Type.String()
	}},
	{"T.NumMethod.iface", func() string { return i(reflect.TypeOf((*Stringer)(nil)).Elem().NumMethod()) }},
	{"T.Method.iface", func() string { return reflect.TypeOf((*error)(nil)).Elem().Method(0).Name }},
	// ---- Type: relations
	{"T.Implements", func() string {
		st := reflect.TypeOf((*Stringer)(nil)).Elem()
		return b(tp().Implements(st)) + b(reflect.TypeOf(1).Implements(st))
	}},
	{"T.Implements.error", func() string {
		et := reflect.TypeOf((*error)(nil)).Elem()
		return b(reflect.TypeOf(&MyErr{}).Implements(et)) + b(reflect.TypeOf(MyErr{}).Implements(et))
	}},
	{"T.AssignableTo", func() string {
		return b(reflect.TypeOf(MyInt(1)).AssignableTo(reflect.TypeOf(1))) + b(tp().AssignableTo(tp()))
	}},
	{"T.ConvertibleTo", func() string { return b(reflect.TypeOf(MyInt(1)).ConvertibleTo(reflect.TypeOf(1.5))) }},
	{"T.identity", func() string {
		return b(reflect.TypeOf(sample) == reflect.TypeOf(P{})) + b(reflect.TypeOf(1) == reflect.TypeOf(""))
	}},
	{"T.identity.elem", func() string {
		return b(reflect.TypeOf(&sample).Elem() == tp()) + b(reflect.TypeOf([]P{}).Elem() == tp())
	}},
	// ---- Type constructors
	{"PointerTo", func() string {
		return reflect.PointerTo(tp()).String() + b(reflect.PointerTo(tp()) == reflect.TypeOf(&sample))
	}},
	{"SliceOf", func() string {
		return reflect.SliceOf(tp()).String() + b(reflect.SliceOf(reflect.TypeOf(1)) == reflect.TypeOf([]int{}))
	}},
	{"MapOf", func() string { return reflect.MapOf(reflect.TypeOf(""), tp()).String() }},
	{"ArrayOf", func() string { return reflect.ArrayOf(5, reflect.TypeOf(uint8(0))).String() }},
	{"ChanOf", func() string { return reflect.ChanOf(reflect.BothDir, reflect.TypeOf(1)).String() }},
	{"FuncOf", func() string {
		return reflect.FuncOf([]reflect.Type{reflect.TypeOf(1)}, []reflect.Type{reflect.TypeOf("")}, false).String()
	}},
	{"StructOf", func() string {
		t := reflect.StructOf([]reflect.StructField{{Name: "A", Type: reflect.TypeOf(1)}, {Name: "B", Type: reflect.TypeOf("")}})
		return t.String() + u(t.Size())
	}},
	{"TypeFor", func() string { return reflect.TypeFor[map[string]P]().String() }},
	// ---- Value: scalar reads
	{"V.Field.Int", func() string { return strconv.FormatInt(reflect.ValueOf(sample).Field(0).Int(), 10) }},
	{"V.Field.String", func() string { return reflect.ValueOf(sample).Field(1).String() }},
	{"V.Field.Bytes", func() string { return string(reflect.ValueOf(sample).Field(2).Bytes()) }},
	{"V.Field.nested", func() string {
		v := reflect.ValueOf(sample).Field(3)
		return strconv.FormatInt(v.Field(0).Int(), 10) + strconv.FormatFloat(v.Field(1).Float(), 'g', -1, 64)
	}},
	{"V.Field.embedded", func() string {
		return strconv.FormatFloat(reflect.ValueOf(sample).FieldByName("B").Float(), 'g', -1, 64)
	}},
	{"V.Field.array", func() string { return strconv.FormatUint(reflect.ValueOf(sample).Field(9).Index(2).Uint(), 10) }},
	{"V.Field.unexported", func() string { v := reflect.ValueOf(sample).Field(11); return b(v.Bool()) + b(v.CanInterface()) }},
	{"V.Kind", func() string {
		return reflect.ValueOf(sample).Kind().String() + reflect.ValueOf(&sample).Kind().String()
	}},
	{"V.Type", func() string { return reflect.ValueOf(sample).Type().String() }},
	{"V.Bool/Uint/Float/Complex", func() string {
		return b(reflect.ValueOf(true).Bool()) + strconv.FormatUint(reflect.ValueOf(uint16(9)).Uint(), 10) +
			strconv.FormatFloat(reflect.ValueOf(float32(1.5)).Float(), 'g', -1, 64) +
			strconv.FormatFloat(imag(reflect.ValueOf(complex(1, 2)).Complex()), 'g', -1, 64)
	}},
	{"V.Int.widths", func() string {
		return strconv.FormatInt(reflect.ValueOf(int8(-3)).Int()+reflect.ValueOf(int16(-300)).Int()+reflect.ValueOf(int32(-70000)).Int()+reflect.ValueOf(int64(1<<40)).Int(), 10)
	}},
	{"V.IsValid/IsZero/IsNil", func() string {
		return b(reflect.Value{}.IsValid()) + b(reflect.ValueOf(0).IsZero()) + b(reflect.ValueOf(sample).IsZero()) +
			b(reflect.ValueOf((*P)(nil)).IsNil()) + b(reflect.ValueOf(sample).Field(4).IsNil())
	}},
	{"V.IsZero.struct", func() string { return b(reflect.ValueOf(Inner{}).IsZero()) + b(reflect.ValueOf([3]int{}).IsZero()) }},
	{"V.Interface", func() string { return i(reflect.ValueOf(sample).Field(0).Interface().(int)) }},
	{"V.Interface.struct", func() string { return i(reflect.ValueOf(sample).Interface().(P).X) }},
	{"V.Len/Cap", func() string {
		return i(reflect.ValueOf([]int{1, 2, 3}).Len()) + i(reflect.ValueOf(make([]int, 2, 9)).Cap()) + i(reflect.ValueOf("héllo").Len()) +
			i(reflect.ValueOf(map[int]int{1: 1}).Len()) + i(reflect.ValueOf([4]int{}).Len())
	}},
	{"V.Index.slice", func() string { return reflect.ValueOf([]string{"a", "b"}).Index(1).String() }},
	{"V.Index.string", func() string { return strconv.FormatUint(reflect.ValueOf("abc").Index(1).Uint(), 10) }},
	{"V.Slice", func() string {
		v := reflect.ValueOf([]int{1, 2, 3, 4}).Slice(1, 3)
		return i(v.Len()) + i(int(v.Index(0).Int()))
	}},
	{"V.String.nonstring", func() string { return reflect.ValueOf(12).String() }},
	// ---- Value: pointers, addressability, writes
	{"V.Elem.ptr", func() string { p := sample; return strconv.FormatInt(reflect.ValueOf(&p).Elem().Field(0).Int(), 10) }},
	{"V.CanSet/CanAddr", func() string {
		p := sample
		return b(reflect.ValueOf(p).Field(0).CanSet()) + b(reflect.ValueOf(&p).Elem().Field(0).CanSet()) + b(reflect.ValueOf(&p).Elem().CanAddr())
	}},
	{"V.SetInt", func() string { p := sample; reflect.ValueOf(&p).Elem().Field(0).SetInt(7); return i(p.X) }},
	{"V.SetString", func() string { p := sample; reflect.ValueOf(&p).Elem().Field(1).SetString("new"); return p.Y }},
	{"V.SetFloat.nested", func() string {
		p := sample
		reflect.ValueOf(&p).Elem().Field(3).Field(1).SetFloat(9.25)
		return strconv.FormatFloat(p.In.B, 'g', -1, 64)
	}},
	{"V.Set.struct", func() string {
		p := sample
		reflect.ValueOf(&p).Elem().Field(3).Set(reflect.ValueOf(Inner{9, 9}))
		return i(int(p.In.A))
	}},
	{"V.Set.slice-elem", func() string { s := []int{1, 2}; reflect.ValueOf(s).Index(1).SetInt(5); return i(s[1]) }},
	{"V.Set.array-elem", func() string {
		p := sample
		reflect.ValueOf(&p).Elem().Field(9).Index(0).SetUint(99)
		return i(int(p.Arr[0]))
	}},
	{"V.SetBytes/SetLen", func() string {
		p := sample
		v := reflect.ValueOf(&p).Elem().Field(2)
		v.SetBytes([]byte("abcd"))
		v.SetLen(3)
		return string(p.Z)
	}},
	{"V.Addr", func() string {
		p := sample
		q := reflect.ValueOf(&p).Elem().Field(3).Addr().Interface().(*Inner)
		q.A = 77
		return i(int(p.In.A))
	}},
	{"V.Set.ptr-field", func() string {
		p := sample
		in := &Inner{5, 6}
		reflect.ValueOf(&p).Elem().Field(4).Set(reflect.ValueOf(in))
		return b(p.Ptr == in)
	}},
	{"V.Set.iface-field", func() string {
		p := sample
		reflect.ValueOf(&p).Elem().Field(8).Set(reflect.ValueOf("s"))
		return p.Any.(string) + reflect.ValueOf(&p).Elem().Field(8).Elem().Kind().String()
	}},
	{"V.Pointer.eq", func() string { p := &sample; return b(reflect.ValueOf(p).Pointer() == uintptr(unsafe.Pointer(p))) }},
	{"V.UnsafePointer", func() string { p := &sample; return b(reflect.ValueOf(p).UnsafePointer() == unsafe.Pointer(p)) }},
	{"New/Zero/Indirect", func() string {
		v := reflect.New(tp())
		v.Elem().Field(0).SetInt(3)
		return i(v.Interface().(*P).X) + b(reflect.Zero(tp()).IsZero()) + reflect.Indirect(v).Type().Name()
	}},
	// ---- Value: maps
	{"V.MapIndex", func() string {
		return strconv.FormatInt(reflect.ValueOf(sample).Field(5).MapIndex(reflect.ValueOf("a")).Int(), 10)
	}},
	{"V.MapKeys", func() string { return reflect.ValueOf(map[string]int{"k": 1}).MapKeys()[0].String() }},
	{"V.MapRange", func() string {
		it := reflect.ValueOf(map[string]int{"k": 5}).MapRange()
		s := ""
		for it.Next() {
			s += it.Key().String() + strconv.FormatInt(it.Value().Int(), 10)
		}
		return s
	}},
	{"V.SetMapIndex", func() string {
		m := map[string]int{}
		reflect.ValueOf(m).SetMapIndex(reflect.ValueOf("z"), reflect.ValueOf(4))
		return i(m["z"])
	}},
	{"MakeMap", func() string {
		v := reflect.MakeMap(reflect.TypeOf(map[int]string{}))
		v.SetMapIndex(reflect.ValueOf(1), reflect.ValueOf("x"))
		return v.Interface().(map[int]string)[1]
	}},
	// ---- Value: slices, chans, funcs
	{"MakeSlice/Append/Copy", func() string {
		v := reflect.MakeSlice(reflect.TypeOf([]int{}), 1, 4)
		v = reflect.Append(v, reflect.ValueOf(8))
		n := reflect.Copy(v, reflect.ValueOf([]int{5}))
		s := v.Interface().([]int)
		return i(len(s)) + i(s[0]) + i(s[1]) + i(n)
	}},
	{"MakeChan/Send/Recv", func() string {
		c := reflect.MakeChan(reflect.TypeOf(make(chan int)), 1)
		c.Send(reflect.ValueOf(3))
		x, ok := c.Recv()
		return strconv.FormatInt(x.Int(), 10) + b(ok)
	}},
	{"V.Call", func() string {
		f := func(a int, s string) string { return s + strconv.Itoa(a) }
		return reflect.ValueOf(f).Call([]reflect.Value{reflect.ValueOf(2), reflect.ValueOf("n")})[0].String()
	}},
	{"V.Call.variadic", func() string {
		f := func(xs ...int) int { return len(xs) }
		return strconv.FormatInt(reflect.ValueOf(f).Call([]reflect.Value{reflect.ValueOf(2), reflect.ValueOf(3)})[0].Int(), 10)
	}},
	{"V.Method.Call", func() string {
		return strconv.FormatInt(reflect.ValueOf(sample).MethodByName("Add").Call([]reflect.Value{reflect.ValueOf(1), reflect.ValueOf(2)})[0].Int(), 10)
	}},
	{"V.Method.ptr.Call", func() string {
		p := sample
		reflect.ValueOf(&p).MethodByName("Set").Call([]reflect.Value{reflect.ValueOf(11)})
		return i(p.X)
	}},
	{"V.NumMethod", func() string { return i(reflect.ValueOf(sample).NumMethod()) }},
	{"MakeFunc", func() string {
		var add func(int, int) int
		fn := reflect.MakeFunc(reflect.TypeOf(add), func(a []reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(int(a[0].Int() + a[1].Int()))}
		})
		reflect.ValueOf(&add).Elem().Set(fn)
		return i(add(3, 4))
	}},
	{"Swapper", func() string { s := []string{"a", "b", "c"}; reflect.Swapper(s)(0, 2); return s[0] + s[2] }},
	{"V.Convert", func() string {
		return strconv.FormatFloat(reflect.ValueOf(3).Convert(reflect.TypeOf(1.5)).Float(), 'g', -1, 64) +
			reflect.ValueOf(MyInt(4)).Convert(reflect.TypeOf(1)).Type().String()
	}},
	{"V.Convert.iface", func() string {
		v := reflect.ValueOf(sample).Convert(reflect.TypeOf((*Stringer)(nil)).Elem())
		return v.Kind().String() + v.Interface().(Stringer).String()
	}},
	{"V.Elem.iface", func() string {
		var a any = sample
		return reflect.ValueOf(&a).Elem().Elem().Type().String()
	}},
	{"V.CanInt/CanFloat", func() string { return b(reflect.ValueOf(1).CanInt()) + b(reflect.ValueOf(1).CanFloat()) }},
	{"V.Comparable/Equal", func() string {
		return b(reflect.ValueOf(sample.In).Comparable()) + b(reflect.ValueOf(sample.In).Equal(reflect.ValueOf(Inner{1, 2.5})))
	}},
	{"V.OverflowInt", func() string { return b(reflect.ValueOf(int8(0)).OverflowInt(300)) }},
	{"V.Grow/Clear", func() string {
		s := []int{1}
		v := reflect.ValueOf(&s).Elem()
		v.Grow(10)
		v.Clear()
		return b(cap(s) >= 11) + i(s[0])
	}},
	{"V.SetZero", func() string { p := sample; reflect.ValueOf(&p).Elem().Field(3).SetZero(); return i(int(p.In.A)) }},
	// ---- package-level algorithms
	{"DeepEqual", func() string {
		a, c := sample, sample
		c.Z = []byte("zz")
		return b(reflect.DeepEqual(a, c)) + b(reflect.DeepEqual([]int{1}, []int{2})) + b(reflect.DeepEqual(map[string][]int{"a": {1}}, map[string][]int{"a": {1}}))
	}},
	{"VisibleFields", func() string { return i(len(reflect.VisibleFields(tp()))) }},
	{"Select", func() string {
		c := make(chan int, 1)
		c <- 5
		ch, v, ok := reflect.Select([]reflect.SelectCase{{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(c)}})
		return i(ch) + strconv.FormatInt(v.Int(), 10) + b(ok)
	}},
	{"TypeAssert", func() string { x, ok := reflect.TypeAssert[int](reflect.ValueOf(5)); return i(x) + b(ok) }},
	{"V.Seq", func() string {
		n := 0
		for v := range reflect.ValueOf([]int{4, 5}).Seq() {
			n += int(v.Int())
		}
		return i(n)
	}},
	{"T.Fields.iter", func() string {
		s := ""
		for f := range tp().Fields() {
			s += f.Name[:1]
		}
		return s
	}},
	{"panic.ValueError", func() (s string) {
		defer func() { s = recover().(*reflect.ValueError).Error() }()
		reflect.ValueOf("x").Int()
		return ""
	}},
}

func runOne(c check) {
	defer func() {
		if r := recover(); r != nil {
			println(c.name, "PANIC")
		}
	}()
	println(c.name, c.f())
}

func main() {
	if len(os.Args) > 1 {
		if os.Args[1] == "count" {
			println(len(checks))
			return
		}
		n, _ := strconv.Atoi(os.Args[1])
		runOne(checks[n])
		return
	}
	for _, c := range checks {
		runOne(c)
	}
}
