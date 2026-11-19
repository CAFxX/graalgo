package main

// L11 (REVIEW-2026-11-18-05): regexp — zero corpus imports before this
// program. Compile/MatchString/FindAllString/ReplaceAll/Split, submatches
// and named groups, plus regexp/syntax Parse + Simplify.

import (
	"fmt"
	"regexp"
	"regexp/syntax"
)

func main() {
	re := regexp.MustCompile(`(?P<word>[a-z]+)@(?P<host>[a-z]+)\.com`)
	s := "mail alice@example.com and bob@test.com now"
	fmt.Printf("match=%v\n", re.MatchString(s))
	fmt.Printf("findall=%q\n", re.FindAllString(s, -1))
	fmt.Printf("replace=%q\n", re.ReplaceAllString(s, "$word at $host dot com"))
	fmt.Printf("split=%q\n", regexp.MustCompile(`,\s*`).Split("a,b, c", -1))

	m := re.FindStringSubmatch("alice@example.com")
	fmt.Printf("submatch=%q\n", m)
	fmt.Printf("named-word=%q\n", m[re.SubexpIndex("word")])
	fmt.Printf("named-host=%q\n", m[re.SubexpIndex("host")])

	// Anchored, case-insensitive, multiline.
	re2 := regexp.MustCompile(`(?im)^err:\s*(.*)$`)
	fmt.Printf("multiline=%q\n", re2.FindStringSubmatch("ok\nERR: bad input\nok"))

	// syntax tree: Parse + Simplify.
	ast, err := syntax.Parse(`a+|b*c`, syntax.Perl)
	if err != nil {
		fmt.Printf("parse-err=%v\n", err)
		return
	}
	fmt.Printf("parsed-op=%v\n", ast.Op)
	simpl := ast.Simplify()
	fmt.Printf("simplified-op=%v\n", simpl.Op)
	prog, err := syntax.Compile(ast)
	if err != nil {
		fmt.Printf("compile-err=%v\n", err)
		return
	}
	fmt.Printf("prog-insts=%d\n", len(prog.Inst))
}
