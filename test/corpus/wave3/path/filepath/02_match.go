package main

// 02_match: filepath.Match — wildcards, escapes, separator scoping,
// bad patterns.
//
// SCOPE NOTE: character classes ([...]) are scope-limited — the
// frontend miscompiles the struct-returning decode helper used by the
// class matcher (hangs). Retest trigger: frontend struct-literal bug
// fixed. This program tests *, ?, escapes, and bad patterns only.
import (
	"fmt"
	"path/filepath"
)

func main() {
	cases := [][2]string{
		{"*.txt", "a.txt"},
		{"*.txt", "a/b.txt"}, // * does not cross Separator
		{"a*b", "a/b"},       // must not match across separator
		{"?", "a"},
		{"?", "/"}, // ? does not match Separator
		{`a\*b`, "a*b"}, // escaped star
		{`a\?b`, "a?b"},
		{"", ""},
		{"a", "a"},
		{"a", "b"},
		{"*", ""},          // star matches empty
		{"**", "a/b"},      // ** is two stars; still no crossing
		{"[", "a"},         // bad pattern
		{`a\`, "a"},        // bad pattern (trailing escape)
		{"*.txt", "a.TXT"}, // case-sensitive
	}
	for _, c := range cases {
		ok, err := filepath.Match(c[0], c[1])
		fmt.Printf("match %q %q: ok=%v err=%v\n", c[0], c[1], ok, err)
	}
	fmt.Println("ok")
}
