package main

// 00_clean_join: filepath.Clean and filepath.Join — dot/dotdot
// elimination, duplicate separators, trailing slashes, empty elements.
// NOTE: frontend/v1 cannot pack variadic call arguments (WS D), so Join
// is exercised in spread form: Join([]string{...}...). The signature
// stays gc-exact (variadic).
import (
	"fmt"
	"path/filepath"
)

func main() {
	cleans := []string{
		"",
		".",
		"a//b/./c/../d",
		"/a/b/../../c",
		"a/b/",
		"/",
		"///",
		"a/././b",
		"../a",
		"a/../../../b",
	}
	for _, p := range cleans {
		fmt.Printf("clean %q -> %q\n", p, filepath.Clean(p))
	}

	joins := [][]string{
		{},
		{""},
		{"", ""},
		{"a"},
		{"a", "b", "c"},
		{"a", "", "b"},
		{"/a", "b"},
		{"a/", "/b"},
		{"a", "..", "b"},
	}
	for _, e := range joins {
		fmt.Printf("join %q -> %q\n", e, filepath.Join(append([]string{}, e...)...))
	}

	fmt.Println("ok")
}
