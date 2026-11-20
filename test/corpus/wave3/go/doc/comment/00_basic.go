package main

// L11 (REVIEW-2026-11-18-05): go/doc/comment — zero corpus imports
// before this program. Parsing doc comments: plain text rendering,
// links, lists, code blocks, headings, and the printer's HTML and
// Markdown output.

import (
	"fmt"
	"go/doc/comment"
)

const docText = "Package demo greets people.\n" +
	"\n" +
	"This paragraph has a link to [Greeter] and a link to\n" +
	"[encoding/json.Marshal].\n" +
	"\n" +
	"# Heading\n" +
	"\n" +
	"A list follows:\n" +
	"\n" +
	"  - one\n" +
	"  - two\n" +
	"  - three\n" +
	"\n" +
	"Code:\n" +
	"\n" +
	"\tfmt.Println(\"hello\")\n" +
	"\n" +
	"A second paragraph with *emphasis* and `code`.\n"

func main() {
	var p comment.Parser
	var pr comment.Printer
	d := p.Parse(docText)
	fmt.Printf("text=%q\n", string(pr.Text(d)))
	fmt.Printf("html=%q\n", string(pr.HTML(d)))
	fmt.Printf("markdown=%q\n", string(pr.Markdown(d)))
	// Heading level option.
	pr2 := comment.Printer{HeadingLevel: 2}
	fmt.Printf("html-h2=%q\n", string(pr2.HTML(d)))
	// A list rendered on its own.
	var p2 comment.Parser
	d2 := p2.Parse("Items:\n\n  - a\n  - b\n")
	fmt.Printf("list-text=%q\n", string(pr.Text(d2)))
	// A doc comment with only a code block.
	var p3 comment.Parser
	d3 := p3.Parse("\tcode only\n")
	fmt.Printf("code-text=%q\n", string(pr.Text(d3)))
	// Empty comment.
	var p4 comment.Parser
	d4 := p4.Parse("")
	fmt.Printf("empty-text=%q\n", string(pr.Text(d4)))
}
