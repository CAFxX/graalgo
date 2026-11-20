package main

// L11 (REVIEW-2026-11-18-05): html — zero corpus imports before this
// program. Escaping and unescaping text for safe HTML inclusion:
// EscapeString, UnescapeString, round-trips, and named/numeric
// entities.

import (
	"fmt"
	"html"
)

func main() {
	cases := []string{
		`<a href="x">&amp; 'quoted'`,
		"plain text",
		"",
		"5 > 3 && 2 < 4",
		"café ☃",
	}
	for _, c := range cases {
		esc := html.EscapeString(c)
		fmt.Printf("esc %q => %q roundtrip=%v\n", c, esc, html.UnescapeString(esc) == c)
	}
	entities := []string{
		"&lt;tag&gt;",
		"&quot;q&quot;",
		"&#39;",
		"&#x27;",
		"&amp;",
		"&copy;",
		"&nbsp;",
		"&unknown;",
		"&lt",
		"&#x41;",
		"&#65;",
	}
	for _, e := range entities {
		fmt.Printf("unesc %q => %q\n", e, html.UnescapeString(e))
	}
}
