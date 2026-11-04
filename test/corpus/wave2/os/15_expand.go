// 15_expand: os.Expand / os.ExpandEnv (gc-faithful shell expansion).
package main

import (
	"fmt"
	"os"
)

func mapping(name string) string {
	switch name {
	case "FOO":
		return "bar"
	case "BAR":
		return "baz"
	case "FOO_BAR1":
		return "qux"
	case "$":
		return "DOLLAR"
	case "9":
		return "NINE"
	default:
		return ""
	}
}

func check(got, want string) {
	if got != want {
		fmt.Printf("FAIL: got %q want %q\n", got, want)
		os.Exit(1)
	}
}

func main() {
	// Plain $var and ${var} forms.
	check(os.Expand("a $FOO b", mapping), "a bar b")
	check(os.Expand("${FOO}/x", mapping), "bar/x")
	check(os.Expand("$FOO$BAR", mapping), "barbaz")
	check(os.Expand("${FOO}suffix", mapping), "barsuffix")
	check(os.Expand("$FOO_BAR1!", mapping), "qux!")

	// Unknown names expand to the empty string.
	check(os.Expand("$UNKNOWN", mapping), "")
	check(os.Expand("pre-${UNKNOWN}-post", mapping), "pre--post")

	// No variables: input returned unchanged.
	check(os.Expand("no vars here", mapping), "no vars here")
	check(os.Expand("", mapping), "")

	// A trailing $ and a $ not followed by a name stay untouched.
	check(os.Expand("trailing $", mapping), "trailing $")
	check(os.Expand("$", mapping), "$")
	check(os.Expand("$ FOO", mapping), "$ FOO")

	// Shell special variables.
	check(os.Expand("$$", mapping), "DOLLAR")
	check(os.Expand("$9", mapping), "NINE")
	check(os.Expand("a$-b", mapping), "ab")
	check(os.Expand("x${*}y", mapping), "xy")

	// Bad ${} syntax is eaten, like gc.
	check(os.Expand("a${}b", mapping), "ab")
	check(os.Expand("a${b", mapping), "ab")
	check(os.Expand("${FOO", mapping), "FOO")

	// ExpandEnv reads the real (overlay) environment.
	if err := os.Setenv("GRAALGO_OS_EXPAND_TEST", "expval"); err != nil {
		fmt.Println("FAIL: setenv:", err)
		os.Exit(1)
	}
	check(os.ExpandEnv("pre-$GRAALGO_OS_EXPAND_TEST-post"), "pre-expval-post")
	check(os.ExpandEnv("${GRAALGO_OS_EXPAND_TEST}"), "expval")
	check(os.ExpandEnv("$GRAALGO_OS_EXPAND_MISSING"), "")

	fmt.Println("ok")
}
