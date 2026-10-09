// 05_subtests: nested subtests, t.Run return values, name dedup (#01),
// FailNow in subtest does not kill siblings, t.Name().
package main

import (
	"fmt"
	"testing"
)

func TestSubtests(t *testing.T) {
	fmt.Println("name:", t.Name())
	ok := t.Run("pass", func(t *testing.T) {
		fmt.Println("sub name:", t.Name())
	})
	fmt.Println("pass returned:", ok)
	ok = t.Run("fail", func(t *testing.T) {
		t.Error([]any{"sub boom"}...)
	})
	fmt.Println("fail returned:", ok, "parent failed:", t.Failed())
	// Duplicate subtest names get #01 suffixes.
	t.Run("dup", func(t *testing.T) { fmt.Println("dup name:", t.Name()) })
	t.Run("dup", func(t *testing.T) { fmt.Println("dup name:", t.Name()) })
	// Nested levels.
	t.Run("outer", func(t *testing.T) {
		t.Run("inner", func(t *testing.T) {
			fmt.Println("nested name:", t.Name())
		})
	})
	// FailNow in a subtest: sibling still runs.
	t.Run("failnow", func(t *testing.T) {
		t.FailNow()
		fmt.Println("UNREACHABLE-SUB")
	})
	t.Run("after", func(t *testing.T) { fmt.Println("AFTER-RAN") })
}

func TestCleanupOrder(t *testing.T) {
	t.Cleanup(func() { fmt.Println("CLEANUP-1") })
	t.Cleanup(func() { fmt.Println("CLEANUP-2") })
	t.Cleanup(func() { fmt.Println("CLEANUP-3") })
}

func main() {
	tests := []testing.InternalTest{
		{"TestSubtests", TestSubtests},
		{"TestCleanupOrder", TestCleanupOrder},
	}
	testing.Main(nil, tests, nil, nil)
}

