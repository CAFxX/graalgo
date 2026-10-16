package main

import (
	"sync/atomic"
	"unsafe"
)

// A multi-result call in return position desugars like the assignment
// case: hoist to a temp, return the unpacked fields packed into the
// function's pair.
func pair(x int) ([]byte, error) {
	if x < 0 {
		return nil, errNegative
	}
	return []byte{byte(x)}, nil
}

type errorString string

func (e errorString) Error() string { return string(e) }

const errNegative = errorString("negative")

func marshalIt(x int) ([]byte, error) {
	return pair(x)
}

// Named results in a multi-result function are defined as locals so
// assignments to them work; the return packs them into the pair.
func namedResults() (sec, nsec int64) {
	sec = int64(123)
	nsec = int64(456)
	return sec, nsec
}

// Package-level sync/atomic word ops lower to atomicaddr+syncop wire nodes.
// The interception keys on the package path (not identifier spelling), so
// direct use here pins the same emission the old fixture pinned incidentally
// through its encoding/json import closure.
var wordCounter int64
var wordSlot uintptr

func atomicUse() int64 {
	atomic.StoreInt64(&wordCounter, 41)
	atomic.AddInt64(&wordCounter, 1)
	v := atomic.LoadInt64(&wordCounter)
	atomic.StoreUintptr(&wordSlot, 7)
	_ = atomic.LoadUintptr(&wordSlot)
	return v
}

// Channel, select, and go-statement surface.
func chanUse() int {
	ch := make(chan int, 1)
	done := make(chan bool)
	go func() {
		v := <-ch
		ch <- v + 1
		close(done)
	}()
	ch <- 41
	select {
	case v := <-ch:
		<-done
		return v
	default:
		return -1
	}
}

// Maps, address-of/deref, unsafe round-trips, generics, labeled flow.
func miscUse() int {
	m := make(map[string]int)
	m["k"] = 3
	x := m["k"]
	p := &x
	*p++
	up := unsafe.Pointer(p)
	back := (*int)(up)
	u := uintptr(up)
	_ = u
	s := unsafe.String((*byte)(up), 1)
	bs := unsafe.Slice((*byte)(up), 1)
	_ = s
	_ = bs
	n := ident[int](21)
	total := 0
loop:
	for i := 0; i < n; i++ {
		switch i % 3 {
		case 0:
			continue
		default:
			total += i
		}
		if total > 500 {
			break loop
		}
	}
	return *back + total
}

func ident[T any](v T) T { return v }

// Interface dispatch, type switches/assertions, range, defer.
func ifaceUse() int {
	var e error = errNegative
	msg := e.Error()
	m := map[string]int{"a": 1, "b": 2}
	total := 0
	for k, v := range m {
		_ = k
		total += v
	}
	var x any = 42
	switch v := x.(type) {
	case int:
		total += v
	case string:
		total += len(v)
	default:
		total--
	}
	if s, ok := x.(string); ok {
		total += len(s)
	}
	defer func() { total++ }()
	_ = msg
	return total
}

func main() {
	b, err := marshalIt(42)
	if err != nil {
		println("err")
		return
	}
	println("marshal:", string(b[:1]))
	s, n := namedResults()
	println("named:", s, n)
	println("atomic:", atomicUse())
	println("chan:", chanUse())
	println("misc:", miscUse())
	println("iface:", ifaceUse())
}
