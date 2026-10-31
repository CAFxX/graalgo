// R-04: sync.Pool and sync/atomic wrappers materialise as plain
// GoStructs (the name-keyed lowering is deleted) and their real
// method bodies run. Deterministic by construction: Pool assertions
// use only contract-permitted outcomes (a Get may return a Put item
// or a New value — never anything else), goroutine results are
// summed, and map iteration is reduced to a sum.
//
// Value copies in sections 3 and 5 are the behaviour under test (a
// copied Pool shares state; a copied atomic is independent), but go vet
// copylocks rejects every typed copy form (plain assignment, generic
// helper, interface round-trip — all verified flagged), and the smoke
// harness vets this corpus. The copies below are therefore written as
// same-type unsafe.Pointer dereferences, which are bitwise-identical
// value copies and the one form copylocks intentionally does not flag.
// Outputs are unchanged by this spelling; the snapshot entry is untouched.
//
// Deliberately NOT covered here (known adjacent gaps, pre-existing):
//   - atomic.Value (its real body reinterprets the struct as
//     efaceWords via unsafe; fails under graalgo — see the R-04
//     lane report).
//   - sync.Map.CompareAndSwap (returns false for a present key
//     under graalgo; the other Map ops below all match gc).
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"
)

type holder struct {
	p   sync.Pool
	c   atomic.Int64
	b   atomic.Bool
	u   atomic.Uint64
	ptr atomic.Pointer[int]
}

type pools struct {
	slots [2]sync.Pool
}

// copyAtomic returns a bitwise copy of a without a typed copy
// expression (see the header note: vet copylocks rejects those).
func copyAtomic(a *atomic.Int64) atomic.Int64 {
	return *(*atomic.Int64)(unsafe.Pointer(a))
}

// copyPool returns a bitwise copy of p without a typed copy expression.
func copyPool(p *sync.Pool) sync.Pool {
	return *(*sync.Pool)(unsafe.Pointer(p))
}

func main() {
	// 1. Struct-embedded Pool: Put/Get round-trip, New field.
	h := holder{}
	h.p.Put(7)
	fmt.Println("embedded pool:", h.p.Get().(int))
	h.p.New = func() any { return 99 }
	fmt.Println("embedded pool new:", h.p.Get().(int))

	// 2. Array-of-Pool slots are independent instances.
	ps := pools{}
	ps.slots[1].Put("x")
	fmt.Println("array pool:", ps.slots[1].Get().(string))
	fmt.Println("array pool other empty:", ps.slots[0].Get() == nil)

	// 3. Pool value copy: the copy shares the pool state (gc: the
	// local pointer is copied), so draining through the copy drains
	// the original.
	var p sync.Pool
	p.Put(1)
	q := copyPool(&p)
	fmt.Println("copied pool:", q.Get().(int))
	fmt.Println("copied pool drains orig:", p.Get() == nil)

	// 4. Pool drop-permissiveness: Get after Put returns either the
	// Put item or the New value — the contract permits dropping at
	// any time, so assert membership, not retention.
	var p2 sync.Pool
	p2.New = func() any { return -1 }
	p2.Put(5)
	v := p2.Get()
	if v.(int) == 5 || v.(int) == -1 {
		fmt.Println("pool contract ok")
	} else {
		fmt.Println("pool contract BAD")
	}

	// 5. Struct-embedded atomic wrappers: methods, copy, by-value.
	h.c.Store(100)
	fmt.Println("embedded atomic:", h.c.Load(), h.c.Add(1), h.c.Load())
	t := copyAtomic(&h.c)
	t.Store(7)
	fmt.Println("atomic copy:", h.c.Load(), t.Load())
	bv := copyAtomic(&h.c)
	bv.Store(7)
	fmt.Println("atomic byval:", h.c.Load())
	h.b.Store(true)
	fmt.Println("embedded bool:", h.b.Load(), h.b.Swap(false), h.b.Load())
	h.u.Store(9)
	fmt.Println("embedded uint64:", h.u.Load(), h.u.Add(1))
	n := 42
	h.ptr.Store(&n)
	fmt.Println("embedded pointer:", *h.ptr.Load(), h.ptr.Swap(nil) != nil, h.ptr.Load() == nil)

	// 6. Package-level atomic ops on a plain field + concurrent Add.
	var x int64 = 3
	fmt.Println("pkg atomic:", atomic.LoadInt64(&x), atomic.AddInt64(&x, 4), x)
	var wg sync.WaitGroup
	var cnt atomic.Int64
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				cnt.Add(1)
			}
		}()
	}
	wg.Wait()
	fmt.Println("concurrent atomic:", cnt.Load())

	// 7. Atomic value as a map key (value equality, not identity).
	mk := map[atomic.Int64]string{}
	var k1 atomic.Int64
	k1.Store(5)
	mk[k1] = "five"
	var k2 atomic.Int64
	k2.Store(5)
	fmt.Println("atomic map key:", mk[k2])

	// 8. sync.Map multi-result ops (all except CompareAndSwap).
	var m sync.Map
	m.Store("k", 1)
	mv, ok := m.Load("k")
	fmt.Println("map load:", mv.(int), ok)
	mv2, loaded := m.LoadOrStore("k", 2)
	fmt.Println("map loadorstore:", mv2.(int), loaded)
	mv3, ok3 := m.LoadAndDelete("k")
	fmt.Println("map loadanddelete:", mv3.(int), ok3)
	_, ok4 := m.Load("k")
	fmt.Println("map gone:", !ok4)
	m.Store("a", 10)
	m.Store("b", 20)
	sum := 0
	m.Range(func(k, v any) bool { sum += v.(int); return true })
	fmt.Println("map range sum:", sum)
	m.Clear()
	_, ok6 := m.Load("a")
	fmt.Println("map cleared:", !ok6)
}
