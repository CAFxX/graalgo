package main

import (
	"fmt"
	"strings"
	"text/scanner"
)

// scanIdents prints one TokenText per line for the idents in src,
// with "---" after each input. Under gc, non-ASCII letters are ident
// runes (unicode.IsLetter) and non-ASCII digits continue idents
// (unicode.IsDigit), so each input below scans as whole idents.
func scanIdents(src string) {
	var s scanner.Scanner
	s.Init(strings.NewReader(src))
	s.Mode = scanner.ScanIdents
	for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
		fmt.Println(s.TokenText())
	}
	fmt.Println("---")
}

func main() {
	scanIdents("café")
	scanIdents("éclair naïve ß αβγ")
	scanIdents("a٣")
}
