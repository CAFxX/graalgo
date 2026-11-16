package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyLocksPointerNotFlagged(t *testing.T) {
	// Passing a *sync.Mutex (or a struct holding only a *sync.Mutex)
	// copies only the pointer, never the lock, so copylocks must stay
	// silent. The single "copies lock" diagnostic below is the positive
	// control: _ = mu copies a sync.Mutex value and must still fire.
	src := `package main

import "sync"

type holder struct {
	mu *sync.Mutex
}

func usePtr(mu *sync.Mutex) {
	_ = mu
}

func useHolder(h holder) {
	_ = h
}

func makePtr(m sync.Mutex) *sync.Mutex {
	return &m
}

func main() {
	var mu sync.Mutex
	usePtr(&mu)
	p := &mu
	_ = p
	h := holder{mu: &mu}
	useHolder(h)
	_ = mu
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "ptr.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range ds {
		msgs = append(msgs, d.Message)
	}
	joined := strings.Join(msgs, "\n")
	t.Logf("diagnostics:\n%s", joined)
	n := strings.Count(joined, "copies lock")
	if n != 1 {
		t.Errorf("expected exactly 1 \"copies lock\" diagnostic (the _ = mu positive control), got %d:\n%s", n, joined)
	}
}

func TestCopyLocksArrayAndDefinedTypes(t *testing.T) {
	// gc ground truth (go1.27.1 go vet): copying an array value copies
	// its elements, so [2]sync.Mutex, a named array type, a defined type
	// aliasing sync.Mutex, and a struct with an array field must all
	// fire. Copying a slice, map, or interface value copies only the
	// header, never the elements, so gc stays silent on all of those --
	// including a struct whose only lock is behind a slice, map, or
	// interface field. Exactly 4 "copies lock" diagnostics are expected.
	// (Helper bodies are empty on purpose: `_ = param` inside a helper
	// would itself be a flagged copy and pollute the count.)
	src := `package main

import "sync"

type withArr struct{ ms [2]sync.Mutex }
type withSlice struct{ ms []sync.Mutex }
type withMap struct{ ms map[string]sync.Mutex }
type withIface struct{ x interface{} }
type Mutexes [2]sync.Mutex
type MyMutex sync.Mutex
type MySlice []sync.Mutex

func useArr(a [2]sync.Mutex)         {}
func useSlice(s []sync.Mutex)        {}
func useMap(m map[string]sync.Mutex) {}
func useIface(i interface{})         {}
func useWithArr(w withArr)           {}
func useWithSlice(w withSlice)       {}
func useWithMap(w withMap)           {}
func useWithIface(w withIface)       {}
func useMutexes(a Mutexes)           {}
func useMyMutex(m MyMutex)           {}
func useMySlice(s MySlice)           {}

func main() {
	var arr [2]sync.Mutex
	useArr(arr)
	var ma Mutexes
	useMutexes(ma)
	var mm MyMutex
	useMyMutex(mm)
	var wa withArr
	useWithArr(wa)
	var sl []sync.Mutex
	useSlice(sl)
	var ms MySlice
	useMySlice(ms)
	var mp map[string]sync.Mutex
	useMap(mp)
	var ws withSlice
	useWithSlice(ws)
	var wm withMap
	useWithMap(wm)
	var iface interface{}
	useIface(iface)
	var wi withIface
	useWithIface(wi)
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "arr.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range ds {
		msgs = append(msgs, d.Message)
	}
	joined := strings.Join(msgs, "\n")
	t.Logf("diagnostics:\n%s", joined)
	n := strings.Count(joined, "copies lock")
	if n != 4 {
		t.Errorf("expected exactly 4 \"copies lock\" diagnostics (array, named array, defined Mutex alias, struct-with-array), got %d:\n%s", n, joined)
	}
}

func TestCopyLocksCompositeLiteralNotFlagged(t *testing.T) {
	// Constructing a fresh value copies no lock, so gc's vet stays
	// silent when a composite literal is passed, assigned, or returned.
	// Genuine copies of an existing lock holder must still fire. The 8
	// expected diagnostics are: `return m` in retVal, `_ = v`, `_ = w`,
	// useVal(existing), `v2 := existing`, `_ = v2`, the retVal(mu) call
	// argument, and `_ = mu`. (The bare retLit()/retMutexLit() calls are
	// statements on purpose: `_ = retLit()` would itself be a flagged
	// copy of the returned value and pollute the count.)
	src := `package main

import "sync"

type withVal struct{ mu sync.Mutex }

func useVal(w withVal) {}

func retLit() withVal             { return withVal{} }
func retMutexLit() sync.Mutex     { return sync.Mutex{} }
func retVal(m sync.Mutex) sync.Mutex { return m }

func main() {
	useVal(withVal{})
	v := withVal{}
	_ = v
	w := withVal{mu: sync.Mutex{}}
	_ = w
	existing := withVal{}
	useVal(existing)
	v2 := existing
	_ = v2
	retLit()
	retMutexLit()
	var mu sync.Mutex
	retVal(mu)
	_ = mu
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "lit.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range ds {
		msgs = append(msgs, d.Message)
	}
	joined := strings.Join(msgs, "\n")
	t.Logf("diagnostics:\n%s", joined)
	n := strings.Count(joined, "copies lock")
	if n != 8 {
		t.Errorf("expected exactly 8 \"copies lock\" diagnostics (genuine copies only), got %d:\n%s", n, joined)
	}
}

func TestCopyLocksLockerRule(t *testing.T) {
	// gc's copylocks rule, verified against go1.27.1 go vet: a type is a
	// lock when a pointer to it implements sync.Locker but a value does
	// not. That covers sync.Mutex/RWMutex directly, sync.Once and
	// sync.WaitGroup (via their Mutex/noCopy fields), sync/atomic values
	// (via sync/atomic.noCopy), and any user type with pointer-receiver
	// Lock/Unlock methods. A type whose VALUE implements Locker (value
	// receivers) is safe to copy and must stay silent -- that pins the
	// second half of the rule. Exactly 5 "copies lock" diagnostics are
	// expected.
	src := `package main

import (
	"sync"
	"sync/atomic"
)

type MyLock struct{ state int32 }

func (m *MyLock) Lock()   {}
func (m *MyLock) Unlock() {}

type VLock struct{}

func (VLock) Lock()   {}
func (VLock) Unlock() {}

func useMyLock(m MyLock)       {}
func useVLock(v VLock)         {}
func useAtomic(a atomic.Int64) {}
func useOnce(o sync.Once)      {}
func useWG(w sync.WaitGroup)   {}
func useRWMutex(m sync.RWMutex) {}

func main() {
	var l MyLock
	useMyLock(l)
	var v VLock
	useVLock(v)
	var a atomic.Int64
	useAtomic(a)
	var o sync.Once
	useOnce(o)
	var w sync.WaitGroup
	useWG(w)
	var r sync.RWMutex
	useRWMutex(r)
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "locker.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range ds {
		msgs = append(msgs, d.Message)
	}
	joined := strings.Join(msgs, "\n")
	t.Logf("diagnostics:\n%s", joined)
	n := strings.Count(joined, "copies lock")
	if n != 5 {
		t.Errorf("expected exactly 5 \"copies lock\" diagnostics (user lock, atomic, Once, WaitGroup, RWMutex), got %d:\n%s", n, joined)
	}
}

func TestCopyLocksVarDeclAndCallResults(t *testing.T) {
	// gc ground truth (go1.27.1 go vet): `+"`"+`var x T = lockVal`+"`"+` copies
	// just like an assignment, but assigning a call result does not count
	// as a copy at the assignment site (the call may return a zero value;
	// any copy is the callee's return). Expected diagnostics, by kind:
	// 2 variable-declaration, 4 assignment, 1 call, 1 return.
	src := `package main

import "sync"

type withVal struct{ mu sync.Mutex }

func useVal(w withVal) {}

func makeVal() withVal               { return withVal{} }
func retVal(m sync.Mutex) sync.Mutex { return m }

func main() {
	var mu sync.Mutex
	var iface interface{} = mu
	_ = iface
	existing := withVal{}
	var v2 withVal = existing
	_ = v2
	var v3 = withVal{}
	_ = v3
	var v4 = makeVal()
	_ = v4
	_ = makeVal()
	_ = retVal(mu)
	_ = mu
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "decl.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range ds {
		msgs = append(msgs, d.Message)
	}
	joined := strings.Join(msgs, "\n")
	t.Logf("diagnostics:\n%s", joined)
	want := map[string]int{
		"variable declaration copies lock value": 2,
		"assignment copies lock value":           4,
		"call copies lock value":                 1,
		"return copies lock value":               1,
	}
	for msg, n := range want {
		if got := strings.Count(joined, msg); got != n {
			t.Errorf("expected %d %q diagnostics, got %d:\n%s", n, msg, got, joined)
		}
	}
}

func TestCopyLocksLiteralElementsAndBuiltins(t *testing.T) {
	// gc ground truth (go1.27.1 go vet): a lock value copied INTO a
	// composite literal is a real copy (the literal itself constructs a
	// fresh value, but its lock-valued elements are copied in), while the
	// len/cap/Sizeof/Offsetof/Alignof builtins use their argument for its
	// type only and never count as a copy. Expected: 2 literal, 2
	// assignment, 0 call diagnostics.
	src := `package main

import (
	"sync"
	"unsafe"
)

type withVal struct{ mu sync.Mutex }

func useVal(w withVal) {}
func useInt(n int)     {}

func main() {
	var mu sync.Mutex
	h := withVal{mu: mu}
	_ = h
	useVal(withVal{mu: mu})
	useVal(withVal{})
	v := withVal{mu: sync.Mutex{}}
	_ = v
	var arr [2]sync.Mutex
	useInt(len(arr))
	useInt(int(unsafe.Sizeof(arr)))
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "litelem.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range ds {
		msgs = append(msgs, d.Message)
	}
	joined := strings.Join(msgs, "\n")
	t.Logf("diagnostics:\n%s", joined)
	want := map[string]int{
		"literal copies lock value":    2,
		"assignment copies lock value": 2,
		"call copies lock value":       0,
	}
	for msg, n := range want {
		if got := strings.Count(joined, msg); got != n {
			t.Errorf("expected %d %q diagnostics, got %d:\n%s", n, msg, got, joined)
		}
	}
}

func TestAnalyzers(t *testing.T) {
	src := `package main

import (
	"context"
	"fmt"
	"sync"
)

func main() {
	fmt.Printf("%d", "hello")
	fmt.Printf("%d %d", 1)
	s := string(65)
	_ = s
	var f func()
	if f == nil {
		fmt.Println("nil")
	}
	x := true
	if x == true {
		fmt.Println("x")
	}
	fmt.Println("done")
	return
	fmt.Println("unreachable")
}

func leaky() {
	ctx, cancel := context.WithCancel(context.Background())
	_ = ctx
	_ = cancel
}

func copyLock(m sync.Mutex) {
	_ = m
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range ds {
		msgs = append(msgs, d.Message)
	}
	joined := strings.Join(msgs, "\n")
	t.Logf("diagnostics:\n%s", joined)

	// Each analyzer should fire at least once.
	want := []string{
		"expects integer",       // printf %d with string
		"missing argument",      // printf missing arg
		"one rune",              // stringintconv
		"always false",          // nilfunc
		"redundant comparison",  // bools
		"unreachable",           // unreachable
		"context leak",          // lostcancel
		"copies lock",           // copylocks
	}
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Errorf("expected diagnostic containing %q, got:\n%s", w, joined)
		}
	}
}
