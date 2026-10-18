// 15_expand: os.Expand / os.ExpandEnv (real since the 2026-10-18
// review-p0-os remediation; previously a runtime panic stub).
package main

import (
	"fmt"
	"os"
)

func check(what, got, want string) {
	if got != want {
		fmt.Printf("FAIL %s: got %q want %q\n", what, got, want)
		os.Exit(1)
	}
}

func main() {
	const key = "GRAALGO_OS_W2_EXPAND"
	if err := os.Setenv(key, "world"); err != nil {
		fmt.Println("FAIL: setenv:", err)
		os.Exit(1)
	}
	m := func(s string) string {
		if s == "X" {
			return "ex"
		}
		return "?"
	}

	// $var and ${var} forms.
	check("dollar", os.Expand("a$X b", m), "aex b")
	check("braces", os.Expand("a${X}b", m), "aexb")
	// Unknown names go through the mapping.
	check("mapping", os.Expand("${Y}", m), "?")
	// Empty expansion and invalid syntax are eaten.
	check("empty-braces", os.Expand("a${}b", m), "ab")
	check("unterminated", os.Expand("a${b", m), "ab")
	// A lone $ at end of string is left untouched.
	check("lone-dollar", os.Expand("a$", m), "a$")
	// Special shell vars.
	check("special", os.Expand("a$?b", func(s string) string { return "Q" }), "aQb")
	// No $ at all returns the input unchanged.
	check("plain", os.Expand("abc", m), "abc")

	// ExpandEnv reads the (overlay) environment.
	check("expandenv", os.ExpandEnv("hello ${"+key+"}!"), "hello world!")
	check("expandenv-unset", os.ExpandEnv("x${GRAALGO_OS_W2_EXPAND_UNSET}y"), "xy")

	if err := os.Unsetenv(key); err != nil {
		fmt.Println("FAIL: unsetenv:", err)
		os.Exit(1)
	}
	fmt.Println("ok")
}
