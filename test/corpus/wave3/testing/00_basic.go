// 00_basic: TestMain driver via testing.Main; Log/Logf, Error (continues),
// Fatal (stops), Skip (stops), Fail/Failed. Log lines carry no file:line
// under graalgo (no runtime.Caller); the package runner strips gc's
// "file:line:" prefixes before comparing (see testing/.normalize.sed).
package main

import (
	"fmt"
	"testing"
)

func TestPass(t *testing.T) {
	t.Log([]any{"hello from TestPass"}...)
	t.Logf("num=%d str=%s", []any{42, "x"}...)
	fmt.Println("PASS-MARKER")
}

func TestFail(t *testing.T) {
	t.Error([]any{"boom"}...)
	fmt.Println("AFTER-ERROR")
	if !t.Failed() {
		fmt.Println("BAD: Failed() false after Error")
	}
}

func TestFatal(t *testing.T) {
	t.Fatal([]any{"fatal boom"}...)
	fmt.Println("UNREACHABLE-AFTER-FATAL")
}

func TestSkip(t *testing.T) {
	t.Log([]any{"about to skip"}...)
	t.Skip([]any{"not today"}...)
	fmt.Println("UNREACHABLE-AFTER-SKIP")
}

func TestSkipf(t *testing.T) {
	t.Skipf("skip %d", []any{7}...)
	fmt.Println("UNREACHABLE-AFTER-SKIPF")
}

func TestFailNow(t *testing.T) {
	t.FailNow()
	fmt.Println("UNREACHABLE-AFTER-FAILNOW")
}

func main() {
	tests := []testing.InternalTest{
		{"TestPass", TestPass},
		{"TestFail", TestFail},
		{"TestFatal", TestFatal},
		{"TestSkip", TestSkip},
		{"TestSkipf", TestSkipf},
		{"TestFailNow", TestFailNow},
	}
	testing.Main(nil, tests, nil, nil)
}

