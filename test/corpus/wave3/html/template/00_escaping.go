package main

// L11 (REVIEW-2026-11-18-05): html/template — zero corpus imports before
// this program. Escaping pins: the same input rendered by html/template
// vs text/template must differ exactly in the escaped spans.

import (
	"fmt"
	"html/template"
	"strings"
	texttemplate "text/template"
)

func renderHTML(name, src string, data any) (string, error) {
	t, err := template.New(name).Parse(src)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

func renderText(name, src string, data any) (string, error) {
	t, err := texttemplate.New(name).Parse(src)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

func main() {
	evil := `<script>alert("x")</script> & "quoted"`
	htmlOut, err := renderHTML("h", `{{.}}`, evil)
	if err != nil {
		fmt.Printf("html-err=%v\n", err)
		return
	}
	textOut, err := renderText("t", `{{.}}`, evil)
	if err != nil {
		fmt.Printf("text-err=%v\n", err)
		return
	}
	fmt.Printf("html=%q\n", htmlOut)
	fmt.Printf("text=%q\n", textOut)
	fmt.Printf("differ=%v\n", htmlOut != textOut)

	// Attribute context: quotes are escaped inside href="...".
	attrOut, err := renderHTML("a", `<a href="{{.}}">x</a>`, `javascript:alert(1)`)
	if err != nil {
		fmt.Printf("attr-err=%v\n", err)
		return
	}
	fmt.Printf("attr=%q\n", attrOut)

	// Trusted template.HTML is NOT escaped.
	safeOut, err := renderHTML("s", `{{.}}`, template.HTML("<b>bold</b>"))
	if err != nil {
		fmt.Printf("safe-err=%v\n", err)
		return
	}
	fmt.Printf("trusted=%q\n", safeOut)

	// URL escaping in an href with a query string.
	urlOut, err := renderHTML("u", `<a href="/s?q={{.}}">x</a>`, "a&b")
	if err != nil {
		fmt.Printf("url-err=%v\n", err)
		return
	}
	fmt.Printf("url=%q\n", urlOut)
}
