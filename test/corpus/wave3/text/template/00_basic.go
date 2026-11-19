package main

// L11 (REVIEW-2026-11-18-05): text/template + text/template/parse —
// zero corpus imports before this program. Parse + Execute to a Builder
// with funcs, if/range/with; text/template/parse tree walk.

import (
	"fmt"
	"strings"
	"text/template"
	"text/template/parse"
)

type item struct {
	Name  string
	Price int
}

func main() {
	funcs := template.FuncMap{
		"upper": strings.ToUpper,
		"price": func(p int) string { return fmt.Sprintf("$%d.%02d", p/100, p%100) },
	}
	src := `{{- range .Items}}{{if .Price}}{{upper .Name}}: {{price .Price}}` +
		`{{else}}{{.Name}}: free{{end}}; {{end}}` +
		`{{with .Owner}}owner={{.}}{{end}}`
	tmpl, err := template.New("shop").Funcs(funcs).Parse(src)
	if err != nil {
		fmt.Printf("parse-err=%v\n", err)
		return
	}
	var b strings.Builder
	data := map[string]any{
		"Items": []item{{"apple", 150}, {"pear", 0}, {"plum", 275}},
		"Owner": "carlo",
	}
	if err := tmpl.Execute(&b, data); err != nil {
		fmt.Printf("exec-err=%v\n", err)
		return
	}
	fmt.Printf("rendered=%q\n", b.String())

	// parse tree walk: count node kinds (parse.Parse gives the trees
	// directly; text/template.Template exposes none).
	trees, err := parse.Parse("shop", src, "{{", "}}", funcs)
	if err != nil {
		fmt.Printf("tree-parse-err=%v\n", err)
		return
	}
	root := trees["shop"].Root
	nodeNames := map[parse.NodeType]string{
		parse.NodeAction: "Action", parse.NodeBool: "Bool",
		parse.NodeBreak: "Break", parse.NodeCommand: "Command",
		parse.NodeContinue: "Continue", parse.NodeDot: "Dot",
		parse.NodeField: "Field", parse.NodeIdentifier: "Identifier",
		parse.NodeIf: "If", parse.NodeList: "List",
		parse.NodeNil: "Nil", parse.NodeNumber: "Number",
		parse.NodePipe: "Pipe", parse.NodeRange: "Range",
		parse.NodeString: "String", parse.NodeTemplate: "Template",
		parse.NodeText: "Text", parse.NodeVariable: "Variable",
		parse.NodeWith: "With",
	}
	counts := map[string]int{}
	var walk func(n parse.Node)
	walk = func(n parse.Node) {
		if n == nil {
			return
		}
		// A typed-nil *ListNode arrives as a non-nil interface; its
		// Type() would panic, so treat a nil list as absent.
		if ln, ok := n.(*parse.ListNode); ok && ln == nil {
			return
		}
		counts[nodeNames[n.Type()]]++
		switch t := n.(type) {
		case *parse.ListNode:
			for _, c := range t.Nodes {
				walk(c)
			}
		case *parse.IfNode:
			walk(t.List)
			if t.ElseList != nil {
				walk(t.ElseList)
			}
		case *parse.RangeNode:
			walk(t.List)
			if t.ElseList != nil {
				walk(t.ElseList)
			}
		case *parse.WithNode:
			walk(t.List)
			if t.ElseList != nil {
				walk(t.ElseList)
			}
		case *parse.ActionNode:
			walk(t.Pipe)
		case *parse.PipeNode:
			for _, c := range t.Cmds {
				walk(c)
			}
		case *parse.CommandNode:
			for _, c := range t.Args {
				walk(c)
			}
		}
	}
	walk(root)
	// Deterministic order: sort keys.
	keys := []string{"Action", "Bool", "Command", "Dot", "Field",
		"Identifier", "If", "List", "Nil", "Number", "Pipe", "Range",
		"String", "Text", "Variable", "With"}
	for _, k := range keys {
		if counts[k] > 0 {
			fmt.Printf("node-%s=%d\n", k, counts[k])
		}
	}

	// Missing key is an error under Option("missingkey=error").
	t2, _ := template.New("m").Option("missingkey=error").Parse(`{{.Nope}}`)
	err = t2.Execute(&strings.Builder{}, map[string]any{})
	fmt.Printf("missingkey-err=%v\n", err != nil)
}
