// Targets sync.Map: Load/Store/LoadOrStore/LoadAndDelete/Delete/Swap/
// CompareAndSwap/CompareAndDelete/Range (early stop) /Clear, with keys of
// mixed dynamic types, concurrent stores, and a CompareAndSwap with an
// uncomparable old value panicking. Range output is sorted before print.
package main

import (
	"slices"
	"strconv"
	"sync"
)

type key struct {
	a int
	b string
}

func dump(m *sync.Map) string {
	var parts []string
	m.Range(func(k, v any) bool {
		var ks string
		switch kk := k.(type) {
		case int:
			ks = "i" + strconv.Itoa(kk)
		case string:
			ks = "s" + kk
		case key:
			ks = "k" + strconv.Itoa(kk.a) + kk.b
		}
		parts = append(parts, ks+"="+strconv.Itoa(v.(int)))
		return true
	})
	slices.Sort(parts)
	out := ""
	for _, p := range parts {
		out += p + " "
	}
	return out
}

func main() {
	var m sync.Map
	m.Store(1, 10)
	m.Store("one", 1)
	m.Store(key{1, "x"}, 100)
	v, ok := m.Load(1)
	println("load", v.(int), ok)
	_, ok = m.Load(2)
	println("load missing", ok)
	_, ok = m.Load(int64(1))
	println("load int64(1)", ok)
	v, ok = m.Load(key{1, "x"})
	println("load struct key", v.(int), ok)

	act, loaded := m.LoadOrStore(1, 999)
	println("LoadOrStore existing", act.(int), loaded)
	act, loaded = m.LoadOrStore(2, 20)
	println("LoadOrStore new", act.(int), loaded)

	v, loaded = m.LoadAndDelete("one")
	println("LoadAndDelete", v.(int), loaded)
	v, loaded = m.LoadAndDelete("one")
	println("LoadAndDelete again", v == nil, loaded)

	prev, loaded := m.Swap(2, 22)
	println("Swap existing", prev.(int), loaded)
	prev, loaded = m.Swap(3, 30)
	println("Swap new", prev == nil, loaded)

	println("CAS wrong old", m.CompareAndSwap(3, 31, 33))
	println("CAS right old", m.CompareAndSwap(3, 30, 33))
	println("CAS missing", m.CompareAndSwap(4, nil, 4))
	println("CAD wrong", m.CompareAndDelete(2, 20))
	println("CAD right", m.CompareAndDelete(2, 22))
	println("CAD missing", m.CompareAndDelete(42, 1))
	m.Delete(999)
	println(dump(&m))

	func() {
		defer func() {
			switch r := recover().(type) {
			case nil:
				println("CAS uncomparable: no panic")
			case error:
				println("CAS uncomparable:", r.Error())
			case string:
				println("CAS uncomparable string:", r)
			}
		}()
		println("CAS mismatched type", m.CompareAndSwap(1, "10", 6))
		m.Store("sl", []int{1})
		m.CompareAndSwap("sl", []int{1}, 6)
	}()

	visited := 0
	m.Range(func(k, v any) bool {
		visited++
		return visited < 2
	})
	println("range early stop visited", visited)

	m.Clear()
	println("after clear", dump(&m) == "")

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Go(func() {
			for i := 0; i < 50; i++ {
				m.LoadOrStore(i, i)
				m.Store(1000+g*50+i, 1)
			}
		})
	}
	wg.Wait()
	cnt, sum := 0, 0
	m.Range(func(k, v any) bool {
		cnt++
		sum += v.(int)
		return true
	})
	println("concurrent count", cnt, "sum", sum)

	// Range while deleting
	m.Range(func(k, v any) bool {
		if k.(int) >= 1000 {
			m.Delete(k)
		}
		return true
	})
	cnt = 0
	m.Range(func(k, v any) bool { cnt++; return true })
	println("after delete-in-range", cnt)
}
