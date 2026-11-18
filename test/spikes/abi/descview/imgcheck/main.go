// Command imgcheck is the strictest consumer the synthesized descriptors
// can be given: gc's own, unmodified reflect package running on real
// memory. It loads the relocatable dump written by the Java prototype
// (abispike.Main dump), materializes every blob outside the Go heap,
// resolves pointer and name/type-offset relocations, and then asks
// reflect the same questions about each synthesized descriptor and about
// the corresponding real type of package fixture.
//
// Exit status 1 if any answer differs. With -oracle it also writes gc's
// answers for the real types (the oracle the Java-side transliterated
// code is compared with).
//
// Linux-only (mmap); build with -ldflags=-checklinkname=0 (it calls
// reflect.addReflectOff, the registration gc's runtime offers for type
// data that lives outside a module — exactly graalgo's situation).
package main

import (
	"bufio"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"abispike/fixture"
)

//go:linkname addReflectOff reflect.addReflectOff
func addReflectOff(ptr unsafe.Pointer) int32

type blob struct {
	addr unsafe.Pointer
	size int
}

// equalStub stands in for the equality intrinsic of comparable types.
// reflect only tests the word for nil in everything this program asks.
func equalStub(p, q unsafe.Pointer) bool { return p == q }

func main() {
	oracle := flag.String("oracle", "", "write gc's answers for the real types to this file")
	benchFlag := flag.Bool("bench", false, "print gc reflect ns/op for the operations the Java bench measures")
	flag.Parse()
	if *benchFlag {
		bench()
		return
	}
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: imgcheck [-oracle file] images.dump")
		os.Exit(2)
	}
	blobs, roots := load(flag.Arg(0))
	_ = blobs
	if len(roots) != len(fixture.Roots) {
		fatal("dump has %d roots, fixture has %d", len(roots), len(fixture.Roots))
	}
	var lines []string
	bad, asked := 0, 0
	hdrSame, hdrTotal := map[string]int{}, 0
	for i, r := range fixture.Roots {
		realT := reflect.TypeOf(r)
		fakeT := asType(roots[i])
		want := describe(realT)
		got := describe(fakeT)
		for _, w := range want {
			lines = append(lines, fmt.Sprintf("t %d %s", i, w))
		}
		asked += len(want)
		bad += diff(fmt.Sprintf("root %d (%s)", i, realT), want, got)
		// Header bytes, field by field, against the descriptor gc's compiler emitted.
		hdrTotal++
		for name, same := range headerFields(typePtr(realT), roots[i]) {
			if same {
				hdrSame[name]++
			}
		}
	}
	vWant, vGot := values(roots)
	lines = append(lines, vWant...)
	asked += len(vWant)
	bad += diff("values", vWant, vGot)
	if *oracle != "" {
		if err := os.WriteFile(*oracle, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			fatal("%v", err)
		}
	}
	for _, name := range []string{"Size_", "PtrBytes", "Hash", "TFlag", "Align_", "FieldAlign_", "Kind_", "Equal!=nil"} {
		fmt.Printf("imgcheck: header field %-11s identical to gc's descriptor for %d/%d root types\n", name, hdrSame[name], hdrTotal)
	}
	fmt.Printf("imgcheck: gc reflect gave %d/%d identical answers on synthesized vs compiler-emitted descriptors\n", asked-bad, asked)
	if bad != 0 {
		os.Exit(1)
	}
}

var sink int

// bench: gc's own numbers for the three operations abispike.Main bench times.
func bench() {
	var boxed any = fixture.Pair{K: "key", V: -7, W: 200}
	t := reflect.TypeOf(boxed)
	const n = 20_000_000
	best := [3]time.Duration{1 << 62, 1 << 62, 1 << 62}
	for round := 0; round < 5; round++ {
		t0 := time.Now()
		for i := 0; i < n; i++ {
			sink += int(reflect.ValueOf(boxed).Field(1).Int())
		}
		t1 := time.Now()
		for i := 0; i < n; i++ {
			sink += int(t.Kind()) + int(t.Size())
		}
		t2 := time.Now()
		for i := 0; i < n/20; i++ {
			sink += len(t.String())
		}
		t3 := time.Now()
		best[0], best[1], best[2] = min(best[0], t1.Sub(t0)), min(best[1], t2.Sub(t1)), min(best[2], t3.Sub(t2)*20)
	}
	fmt.Printf("bench gc   ValueOf(x).Field(1).Int() %.1f ns/op\n", float64(best[0])/n)
	fmt.Printf("bench gc   t.Kind()+t.Size()         %.1f ns/op\n", float64(best[1])/n)
	fmt.Printf("bench gc   t.String()                %.1f ns/op\n", float64(best[2])/n)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "imgcheck: "+format+"\n", args...)
	os.Exit(2)
}

func diff(what string, want, got []string) int {
	bad := 0
	for i := range want {
		if i >= len(got) || want[i] != got[i] {
			g := "<missing>"
			if i < len(got) {
				g = got[i]
			}
			fmt.Printf("imgcheck: MISMATCH %s: want %q got %q\n", what, want[i], g)
			bad++
		}
	}
	if len(got) > len(want) {
		bad += len(got) - len(want)
	}
	return bad
}

func parse(line string) (string, map[string]string) {
	parts := strings.Fields(line)
	kv := map[string]string{}
	for _, p := range parts[1:] {
		k, v, _ := strings.Cut(p, "=")
		kv[k] = v
	}
	return parts[0], kv
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		fatal("%v", err)
	}
	return n
}

// load materializes the dump. All blobs live in one anonymous mapping: the
// collector never scans it, and nothing in it points into the Go heap.
func load(path string) ([]blob, []unsafe.Pointer) {
	f, err := os.Open(path)
	if err != nil {
		fatal("%v", err)
	}
	defer f.Close()
	var recs []string
	total := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		recs = append(recs, sc.Text())
		if kind, kv := parse(sc.Text()); kind == "blob" {
			total += (atoi(kv["size"]) + 15) &^ 7
		}
	}
	mem, err := syscall.Mmap(-1, 0, total+4096, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		fatal("mmap: %v", err)
	}
	base := unsafe.Pointer(unsafe.SliceData(mem))
	var blobs []blob
	var roots []unsafe.Pointer
	next := 0
	eq := equalStub
	eqWord := *(*unsafe.Pointer)(unsafe.Pointer(&eq))
	for _, rec := range recs {
		kind, kv := parse(rec)
		at := func() unsafe.Pointer { return unsafe.Add(blobs[atoi(kv["blob"])].addr, atoi(kv["off"])) }
		switch kind {
		case "blob":
			data, err := hex.DecodeString(kv["data"][1:])
			if err != nil {
				fatal("%v", err)
			}
			copy(mem[next:], data)
			blobs = append(blobs, blob{unsafe.Add(base, next), len(data)})
			next += (len(data) + 15) &^ 7
		case "ptr":
			*(*unsafe.Pointer)(at()) = unsafe.Add(blobs[atoi(kv["tblob"])].addr, atoi(kv["toff"]))
		case "equal":
			*(*unsafe.Pointer)(at()) = eqWord
		case "offreloc":
			*(*int32)(at()) = addReflectOff(blobs[atoi(kv["tblob"])].addr)
		case "root":
			roots = append(roots, blobs[atoi(kv["blob"])].addr)
		default:
			fatal("unknown record %q", rec)
		}
	}
	return blobs, roots
}

// asType wraps a descriptor pointer as a reflect.Type (*reflect.rtype).
func asType(p unsafe.Pointer) reflect.Type {
	t := reflect.TypeOf(0)
	(*[2]unsafe.Pointer)(unsafe.Pointer(&t))[1] = p
	return t
}

func typePtr(t reflect.Type) unsafe.Pointer {
	return (*[2]unsafe.Pointer)(unsafe.Pointer(&t))[1]
}

// headerFields compares the fixed abi.Type header of two descriptors.
// Offsets are amd64's (the same numbers typetab derives from go/types).
func headerFields(a, b unsafe.Pointer) map[string]bool {
	u64 := func(p unsafe.Pointer, off int) uint64 { return *(*uint64)(unsafe.Add(p, off)) }
	u32 := func(p unsafe.Pointer, off int) uint32 { return *(*uint32)(unsafe.Add(p, off)) }
	u8 := func(p unsafe.Pointer, off int) uint8 { return *(*uint8)(unsafe.Add(p, off)) }
	const onDemand = 1 << 4 // TFlagGCMaskOnDemand: a gc space optimization the prototype never uses
	return map[string]bool{
		"Size_":       u64(a, 0) == u64(b, 0),
		"PtrBytes":    u64(a, 8) == u64(b, 8),
		"Hash":        u32(a, 16) == u32(b, 16),
		"TFlag":       u8(a, 20)&^onDemand == u8(b, 20)&^onDemand,
		"Align_":      u8(a, 21) == u8(b, 21),
		"FieldAlign_": u8(a, 22) == u8(b, 22),
		"Kind_":       u8(a, 23) == u8(b, 23),
		"Equal!=nil":  (u64(a, 24) != 0) == (u64(b, 24) != 0),
	}
}

func hx(s string) string { return "x" + hex.EncodeToString([]byte(s)) }

// describe asks reflect everything the prototype covers about a type.
// The first block of keys is what the Java-side transliteration also
// prints (same names); the rest only gc answers.
func describe(t reflect.Type) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	add("kind %d", uint(t.Kind()))
	add("size %d", t.Size())
	add("align %d", t.Align())
	add("fieldalign %d", t.FieldAlign())
	add("string %s", hx(t.String()))
	add("name %s", hx(t.Name()))
	add("pkgpath %s", hx(t.PkgPath()))
	add("nummethod %d", t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		add("m%d.name %s", i, hx(t.Method(i).Name))
	}
	switch t.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Pointer, reflect.Slice:
		add("elem %s", hx(t.Elem().String()))
	}
	switch t.Kind() {
	case reflect.Map:
		add("key %s", hx(t.Key().String()))
	case reflect.Array:
		add("len %d", t.Len())
	case reflect.Chan:
		add("chandir %d", int(t.ChanDir()))
	case reflect.Func:
		add("numin %d", t.NumIn())
		for i := 0; i < t.NumIn(); i++ {
			add("in%d %s", i, hx(t.In(i).String()))
		}
		add("numout %d", t.NumOut())
		for i := 0; i < t.NumOut(); i++ {
			add("out%d %s", i, hx(t.Out(i).String()))
		}
		add("variadic %v", t.IsVariadic())
	case reflect.Struct:
		add("numfield %d", t.NumField())
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			add("f%d.name %s", i, hx(f.Name))
			add("f%d.type %s", i, hx(f.Type.String()))
			add("f%d.offset %d", i, f.Offset)
			add("f%d.tag %s", i, hx(string(f.Tag)))
			add("f%d.embedded %v", i, f.Anonymous)
			add("f%d.exported %v", i, f.IsExported())
		}
	}
	// gc-only keys: deeper reflect paths over the same descriptors.
	add("comparable %v", t.Comparable())
	add("ptrto %s", hx(reflect.PointerTo(t).String()))
	add("sliceof %s", hx(reflect.SliceOf(t).String()))
	for i := 0; i < t.NumMethod(); i++ {
		add("m%d.type %s", i, hx(t.Method(i).Type.String()))
	}
	if t.Kind() == reflect.Struct {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			add("f%d.pkgpath %s", i, hx(f.PkgPath))
			add("f%d.typekind %d", i, uint(f.Type.Kind()))
			g, ok := t.FieldByName(f.Name)
			add("f%d.byname %v %v", i, ok, g.Index)
		}
		add("visible %d", len(reflect.VisibleFields(t)))
	}
	if t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Interface {
		add("elem.nummethod %d", t.Elem().NumMethod())
		for i := 0; i < t.Elem().NumMethod(); i++ {
			add("elem.m%d %s %s", i, hx(t.Elem().Method(i).Name), hx(t.Elem().Method(i).Type.String()))
		}
	}
	return out
}

// values reads sample values through reflect twice: with the real type and
// with the synthesized descriptor laid over the same memory (NewAt).
func values(roots []unsafe.Pointer) (want, got []string) {
	rootOf := func(v any) reflect.Type {
		for i, r := range fixture.Roots {
			if reflect.TypeOf(r) == reflect.TypeOf(v) {
				return asType(roots[i])
			}
		}
		fatal("no root for %T", v)
		return nil
	}
	pair := fixture.Pair{K: "key", V: -7, W: 200}
	wide := fixture.Wide{F32: 1.5, I16: -2, U64: 1 << 63, B: true, S: "wide"}
	five := fixture.MyInt(5)
	s := "s"
	run := func(fake bool) []string {
		var out []string
		add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
		val := func(p any) reflect.Value { // p is a pointer to the sample
			if !fake {
				return reflect.ValueOf(p).Elem()
			}
			return reflect.NewAt(rootOf(reflect.ValueOf(p).Elem().Interface()), reflect.ValueOf(p).UnsafePointer()).Elem()
		}
		v := val(&pair)
		add("v Pair f0.string %s", hx(v.Field(0).String()))
		add("v Pair f0.len %d", v.Field(0).Len())
		add("v Pair f1.int %d", v.Field(1).Int())
		add("v Pair f2.uint %d", v.Field(2).Uint())
		v = val(&wide)
		add("v Wide f0.float %v", v.Field(0).Float())
		add("v Wide f1.int %d", v.Field(1).Int())
		add("v Wide f2.uint %d", v.Field(2).Uint())
		add("v Wide f3.bool %v", v.Field(3).Bool())
		add("v Wide f4.string %s", hx(v.Field(4).String()))
		p := fixture.P{X: 42, Y: "hi", In: fixture.Inner{A: 1, B: 2.5}, Inner: fixture.Inner{A: 3, B: 4.5}, Arr: [3]uint16{0, 0, 9}}
		v = val(&p)
		add("v P f0.int %d", v.Field(0).Int())
		add("v P f1.string %s", hx(v.Field(1).String()))
		add("v P f3.f0.int %d", v.Field(3).Field(0).Int())
		add("v P f3.f1.float %v", v.Field(3).Field(1).Float())
		add("v P f12.f1.float %v", v.Field(12).Field(1).Float())
		add("v P f9.i2.uint %d", v.Field(9).Index(2).Uint())
		v.Field(0).SetInt(7)
		v.Field(3).Field(1).SetFloat(9.25)
		add("v P set.f0 %d", p.X)
		add("v P set.f3.f1 %v", p.In.B)
		func() {
			defer func() {
				if recover() != nil {
					add("v P set.unexported panic")
				} else {
					add("v P set.unexported no panic")
				}
			}()
			v.Field(11).SetInt(1)
		}()
		add("v MyInt int %d", val(&five).Int())
		add("v string string %s", hx(val(&s).String()))
		add("v P escape trap") // gc has no such check; the Java side must trap (GG-0016)
		return out
	}
	return run(false), run(true)
}
