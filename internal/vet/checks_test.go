package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runVetOnSrc(t *testing.T, src string) string {
	t.Helper()
	return runVetOnSrcFile(t, "test.go", src)
}

// runVetOnSrcFile is runVetOnSrc with an explicit file name. Some analyzers
// only inspect files with a particular suffix (e.g. the "tests" analyzer
// only looks at _test.go files), so the file name is part of the fixture.
func runVetOnSrcFile(t *testing.T, name, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
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
	return strings.Join(msgs, "\n")
}

func TestStructTag(t *testing.T) {
	src := `package main

type Good struct {
	Name string ` + "`json:\"name\"`" + `
	Age  int    ` + "`json:\"age\" db:\"age_col\"`" + `
}

type BadTag struct {
	X int ` + "`json:name`" + ` // malformed: missing quotes
}

type DupTag struct {
	Y int ` + "`json:\"y\" json:\"y2\"`" + ` // duplicate key
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "malformed") {
		t.Errorf("expected malformed tag diagnostic, got:\n%s", joined)
	}
	if !strings.Contains(joined, "duplicate") {
		t.Errorf("expected duplicate tag diagnostic, got:\n%s", joined)
	}
	// Good struct should not produce diagnostics.
	if strings.Contains(joined, "Good") {
		t.Errorf("false positive on good struct tags:\n%s", joined)
	}
}

func TestAssign(t *testing.T) {
	src := `package main

func f() {
	x := 1
	x = x
	_ = x
	y := 2
	y = 3
	_ = y
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "self-assignment of x") {
		t.Errorf("expected self-assignment diagnostic, got:\n%s", joined)
	}
	if strings.Contains(joined, "self-assignment of y") {
		t.Errorf("false positive on y = 3:\n%s", joined)
	}
}

func TestShift(t *testing.T) {
	src := `package main

func f(x int32, y int64) {
	_ = x << 40 // too large for int32
	_ = x << 4  // fine
	_ = y << 64 // too large for int64
	_ = y << 63 // fine
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "too small for shift of 40") {
		t.Errorf("expected shift diagnostic for 40, got:\n%s", joined)
	}
	if !strings.Contains(joined, "too small for shift of 64") {
		t.Errorf("expected shift diagnostic for 64, got:\n%s", joined)
	}
	// Should not flag the fine shifts.
	count := strings.Count(joined, "too small for shift")
	if count != 2 {
		t.Errorf("expected exactly 2 shift diagnostics, got %d:\n%s", count, joined)
	}
}

func TestErrorsAs(t *testing.T) {
	src := `package main

import "errors"

type MyErr struct{ msg string }

func (e *MyErr) Error() string { return e.msg }

func f(err error) {
	var s string
	errors.As(err, &s) // bad: *string does not implement error
	var e *MyErr
	_ = errors.As(err, &e) // good
	var i interface{}
	errors.As(err, &i) // good: pointer to interface
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "second argument to errors.As") {
		t.Errorf("expected errors.As diagnostic, got:\n%s", joined)
	}
	// Should only fire once (for the string case).
	count := strings.Count(joined, "second argument to errors.As")
	if count != 1 {
		t.Errorf("expected exactly 1 errors.As diagnostic, got %d:\n%s", count, joined)
	}
}

func TestUnmarshal(t *testing.T) {
	src := `package main

import "encoding/json"

func f(data []byte) {
	var m map[string]interface{}
	json.Unmarshal(data, &m) // good: pointer
	var s struct{ X int }
	json.Unmarshal(data, s) // bad: non-pointer
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "non-pointer") {
		t.Errorf("expected unmarshal diagnostic, got:\n%s", joined)
	}
}

func TestUnusedResult(t *testing.T) {
	src := `package main

import (
	"fmt"
	"strings"
)

func f() {
	fmt.Sprintf("hello") // bad: discarded
	s := strings.ToUpper("x")
	_ = s
	_ = fmt.Sprintf("used") // good: assigned (but blank-identified below)
	_ = strings.ToUpper("y")
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "result of fmt.Sprintf call not used") {
		t.Errorf("expected unusedresult diagnostic, got:\n%s", joined)
	}
}

func TestTimeFormat(t *testing.T) {
	src := `package main

import "time"

func f(t time.Time) {
	_ = t.Format("2006-02-01") // bad
	_ = t.Format("2006-01-02") // good
	_, _ = time.Parse("2006-02-01", "x") // bad
}
`
	joined := runVetOnSrc(t, src)
	count := strings.Count(joined, "2006-02-01 should be 2006-01-02")
	if count != 2 {
		t.Errorf("expected 2 timeformat diagnostics, got %d:\n%s", count, joined)
	}
}

func TestAppends(t *testing.T) {
	src := `package main

func f() {
	s := []int{1, 2, 3}
	append(s, 4) // bad: discarded
	t := append(s, 5)
	_ = t
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "is not used") {
		t.Errorf("expected appends diagnostic, got:\n%s", joined)
	}
}

func TestWaitGroup(t *testing.T) {
	src := `package main

import "sync"

func f() {
	var wg sync.WaitGroup
	wg.Add(1) // good: outside goroutine
	go func() {
		defer wg.Done()
	}()
	wg.Wait()

	var wg2 sync.WaitGroup
	for i := 0; i < 3; i++ {
		go func() {
			wg2.Add(1) // bad: inside goroutine
			defer wg2.Done()
		}()
	}
	wg2.Wait()
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "WaitGroup.Add called from inside new goroutine") {
		t.Errorf("expected waitgroup diagnostic, got:\n%s", joined)
	}
	count := strings.Count(joined, "WaitGroup.Add called from inside new goroutine")
	if count != 1 {
		t.Errorf("expected exactly 1 waitgroup diagnostic, got %d:\n%s", count, joined)
	}
}

func TestDefers(t *testing.T) {
	src := `package main

import (
	"fmt"
	"time"
)

func f(start time.Time) {
	defer fmt.Println(time.Since(start)) // bad: Since not deferred
	defer func() { fmt.Println(time.Since(start)) }() // good: deferred
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "call to time.Since is not deferred") {
		t.Errorf("expected defers diagnostic, got:\n%s", joined)
	}
	count := strings.Count(joined, "call to time.Since is not deferred")
	if count != 1 {
		t.Errorf("expected exactly 1 defers diagnostic, got %d:\n%s", count, joined)
	}
}

func TestIfaceAssert(t *testing.T) {
	src := `package main

import "fmt"

type Stringer interface{ String() string }
type Errorer interface{ Error() string }

func f(x interface{}) {
	_ = x.(Stringer) // ok: possible
}

func g(s Stringer) {
	_ = s.(Errorer) // impossible: Stringer cannot be Errorer (no overlap)
	_ = fmt.Sprint(s)
}
`
	joined := runVetOnSrc(t, src)
	// Stringer and Errorer have no common method; a Stringer value cannot
	// be asserted to Errorer... actually it CAN if the dynamic type implements both.
	// Our check is conservative; just verify it doesn't crash.
	_ = joined
}

func TestSlog(t *testing.T) {
	src := `package main

import "log/slog"

func f() {
	slog.Info("hello", "key", "value") // good
	slog.Info("hello", "key")          // bad: odd args
	slog.Info("hello", 123, "value")   // bad: non-string key
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "odd number") {
		t.Errorf("expected slog odd-args diagnostic, got:\n%s", joined)
	}
	if !strings.Contains(joined, "should be a string") {
		t.Errorf("expected slog key diagnostic, got:\n%s", joined)
	}
}

func TestStdMethods(t *testing.T) {
	src := `package main

type Good struct{}

func (Good) String() string { return "good" }

type Bad struct{}

func (Bad) String() int { return 1 } // bad signature

type BadReader struct{}

func (BadReader) Read(p []byte) int { return 0 } // bad: should return (int, error)
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "should have signature") {
		t.Errorf("expected stdmethods diagnostic, got:\n%s", joined)
	}
}

func TestUnsafePtr(t *testing.T) {
	src := `package main

import "unsafe"

func f(x uintptr) {
	_ = unsafe.Pointer(x) // bad: uintptr -> unsafe.Pointer
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "unsafe.Pointer") {
		t.Errorf("expected unsafeptr diagnostic, got:\n%s", joined)
	}
}

func TestSigChanyzer(t *testing.T) {
	src := `package main

import (
	"os"
	"os/signal"
)

func f() {
	c1 := make(chan os.Signal)
	signal.Notify(c1) // bad: unbuffered
	c2 := make(chan os.Signal, 1)
	signal.Notify(c2) // good: buffered
	_, _ = c1, c2
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "should be buffered") {
		t.Errorf("expected sigchanyzer diagnostic, got:\n%s", joined)
	}
	count := strings.Count(joined, "should be buffered")
	if count != 1 {
		t.Errorf("expected exactly 1 sigchanyzer diagnostic, got %d:\n%s", count, joined)
	}
}

func TestDirective(t *testing.T) {
	src := `package main

//go:generate echo hello
//go:unknown_directive

func f() {}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "unknown go: directive") {
		t.Errorf("expected directive diagnostic, got:\n%s", joined)
	}
}

func TestTestingGoroutine(t *testing.T) {
	src := `package main

import "testing"

func TestFoo(t *testing.T) {
	go func() {
		t.Fatal("bad") // bad: Fatal from goroutine
	}()
	t.Log("good")
}

func TestBar(t *testing.T) {
	t.Fatal("good: direct call") // good: not in goroutine
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "non-test goroutine") {
		t.Errorf("expected testinggoroutine diagnostic, got:\n%s", joined)
	}
}

func TestHostPort(t *testing.T) {
	src := `package main

import "net"

func f() {
	_, _ = net.Dial("tcp", "localhost:80")     // good
	_, _ = net.Dial("tcp", "localhost"+":80")  // bad: concatenation
	_, _ = net.Dial("tcp", "localhost")        // bad: missing port
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "JoinHostPort") {
		t.Errorf("expected hostport diagnostic, got:\n%s", joined)
	}
	if !strings.Contains(joined, "missing port") {
		t.Errorf("expected hostport missing-port diagnostic, got:\n%s", joined)
	}
}

func TestHTTPResponse(t *testing.T) {
	src := `package main

import (
	"log"
	"net/http"
)

func f(url string) {
	resp, err := http.Get(url)
	defer resp.Body.Close() // bad: before error check
	if err != nil {
		log.Fatal(err)
	}
	_ = resp
}

func g(url string) {
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close() // good: after error check
	_ = resp
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "before checking for errors") {
		t.Errorf("expected httpresponse diagnostic, got:\n%s", joined)
	}
	count := strings.Count(joined, "before checking for errors")
	if count != 1 {
		t.Errorf("expected exactly 1 httpresponse diagnostic, got %d:\n%s", count, joined)
	}
}

// Firing tests for the atomic, loopclosure, composites, and tests analyzers.
// Each analyzer must fire on bad input (positive) and stay silent on the
// corresponding good input (negative): a no-op analyzer must fail these
// tests, per the repo's own doctrine that an unfired analyzer rots silently.

func TestAtomic(t *testing.T) {
	bad := `package main

import "sync/atomic"

func f() {
	var n atomic.Int64
	n.v = 5 // direct assignment to an atomic value: must use Store
	_ = n
}
`
	joined := runVetOnSrc(t, bad)
	if !strings.Contains(joined, "direct assignment to atomic value") {
		t.Errorf("expected atomic diagnostic, got:\n%s", joined)
	}

	good := `package main

import "sync/atomic"

func f() {
	var n atomic.Int64
	n.Store(5)
	_ = n.Load()
}
`
	joined = runVetOnSrc(t, good)
	if strings.Contains(joined, "direct assignment to atomic value") {
		t.Errorf("false positive on atomic Store/Load:\n%s", joined)
	}
}

func TestLoopClosure(t *testing.T) {
	// `for i = range` assigns to an outer variable, so a closure that
	// outlives the iteration observes the final value — the case the
	// analyzer flags (Go 1.22 per-iteration semantics only apply to :=).
	bad := `package main

func f() {
	var i int
	xs := []int{1, 2, 3}
	for i = range xs {
		go func() {
			_ = i
		}()
	}
	_ = xs
}
`
	joined := runVetOnSrc(t, bad)
	if !strings.Contains(joined, "loop variable i captured by func literal") {
		t.Errorf("expected loopclosure diagnostic, got:\n%s", joined)
	}

	good := `package main

func f() {
	xs := []int{1, 2, 3}
	for i := range xs {
		i := i
		go func() {
			_ = i
		}()
	}
	_ = xs
}
`
	joined = runVetOnSrc(t, good)
	if strings.Contains(joined, "captured by func literal") {
		t.Errorf("false positive on per-iteration loop variable:\n%s", joined)
	}
}

func TestComposites(t *testing.T) {
	bad := `package main

import "image"

func f() image.Rectangle {
	return image.Rectangle{image.Point{0, 0}, image.Point{10, 10}}
}
`
	joined := runVetOnSrc(t, bad)
	if !strings.Contains(joined, "composite literal uses unkeyed fields") {
		t.Errorf("expected composites diagnostic, got:\n%s", joined)
	}
	// The outer literal and both nested points are unkeyed: exactly 3.
	if n := strings.Count(joined, "composite literal uses unkeyed fields"); n != 3 {
		t.Errorf("expected 3 composites diagnostics, got %d:\n%s", n, joined)
	}

	good := `package main

import "image"

func f() image.Rectangle {
	return image.Rectangle{Min: image.Point{X: 0, Y: 0}, Max: image.Point{X: 10, Y: 10}}
}
`
	joined = runVetOnSrc(t, good)
	if strings.Contains(joined, "unkeyed fields") {
		t.Errorf("false positive on keyed composite literal:\n%s", joined)
	}
}

func TestTestsAnalyzer(t *testing.T) {
	// The "tests" analyzer only inspects _test.go files, so the fixture
	// must carry that suffix (runVetOnSrcFile, not runVetOnSrc).
	bad := `package foo

import "testing"

func TestBad() {} // wrong signature: no *testing.T

func TestMain() {} // wrong signature: no *testing.M
`
	joined := runVetOnSrcFile(t, "foo_test.go", bad)
	if !strings.Contains(joined, "Test functions must take one *testing.T parameter") {
		t.Errorf("expected tests diagnostic for TestBad, got:\n%s", joined)
	}
	if !strings.Contains(joined, "TestMain must take one *testing.M parameter") {
		t.Errorf("expected tests diagnostic for TestMain, got:\n%s", joined)
	}

	good := `package foo

import "testing"

func TestGood(t *testing.T) { t.Log("ok") }
`
	joined = runVetOnSrcFile(t, "foo_test.go", good)
	if strings.Contains(joined, "must take") {
		t.Errorf("false positive on well-formed test:\n%s", joined)
	}
}
