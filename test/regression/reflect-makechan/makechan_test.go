// Regression test for REVIEW-2026-11-03-13 P1-1 (probe lane
// rev1103-reflect-makechan-probe): reflect.MakeChan on the real reflect
// package died with a host arity error instead of creating a channel.
//
// Root cause: ReflectLinknames registered "reflect.makechan" with arity 1
// while its lambda reads two arguments (type, buffer) — the exact call
// shape of gc's reflect.makechan (GOROOT src/reflect/value.go: MakeChan
// calls makechan(t, buffer); src/runtime/chan.go: reflect_makechan(t,
// size)). Linknames.dispatch therefore threw FrontendException
// ("takes 1 arguments, got 2") before the lambda ran, and that host
// exception surfaced as a fatal internal error that a guest recover()
// could not catch: the plain make(chan int, 2) control in the same
// program printed OK, then the reflect path aborted the process.
// The fix is the registration arity 1 -> 2; the lambda was already
// correct. Pre-fix, this test aborts the run at the MakeChan call.
//
// Run under gc with `go test`, under graalgo with `graalgo test ./dir`
// (via `make test-regression`, which picks up every test/regression/*/).
package reflectmakechan

import (
	"reflect"
	"testing"
)

func TestReflectMakeChanBuffered(t *testing.T) {
	v := reflect.MakeChan(reflect.TypeOf(make(chan int)), 2)
	if v.Kind() != reflect.Chan {
		t.Fatalf("MakeChan kind = %v, want Chan", v.Kind())
	}
	if got := v.Cap(); got != 2 {
		t.Fatalf("MakeChan cap = %d, want 2", got)
	}
	if got := v.Len(); got != 0 {
		t.Fatalf("MakeChan len = %d, want 0", got)
	}
	if got := v.Type(); got != reflect.TypeOf(make(chan int)) {
		t.Fatalf("MakeChan type = %v, want chan int", got)
	}
}

func TestReflectMakeChanUnbuffered(t *testing.T) {
	v := reflect.MakeChan(reflect.TypeOf(make(chan string)), 0)
	if v.Kind() != reflect.Chan {
		t.Fatalf("MakeChan kind = %v, want Chan", v.Kind())
	}
	if got := v.Cap(); got != 0 {
		t.Fatalf("MakeChan cap = %d, want 0", got)
	}
}
