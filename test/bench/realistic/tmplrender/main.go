// tmplrender: text/template rendering of a report page (static-site / code
// generator class): range, if, nested templates, printf, a FuncMap.
package main

import (
	"bytes"
	"strconv"
	"strings"
	"text/template"

	"graalgo/bench/h"
)

const page = `{{define "row"}}<tr class="{{if .Active}}on{{else}}off{{end}}"><td>{{.ID}}</td><td>{{upper .Name}}</td><td>{{printf "%.2f" .Price}}</td><td>{{range $i, $t := .Tags}}{{if $i}}, {{end}}{{$t}}{{end}}</td></tr>
{{end}}<html><head><title>{{.Title}}</title></head><body>
<h1>{{.Title}} ({{len .Rows}} rows)</h1>
<table>
{{range .Rows}}{{template "row" .}}{{end}}</table>
{{with .Footer}}<p>{{.}}</p>{{end}}
</body></html>
`

type Row struct {
	ID     int
	Name   string
	Price  float64
	Active bool
	Tags   []string
}

type Page struct {
	Title  string
	Rows   []Row
	Footer string
}

var (
	tmpl = template.Must(template.New("page").Funcs(template.FuncMap{"upper": strings.ToUpper}).Parse(page))
	data Page
)

func init() {
	seed := uint32(5)
	rnd := func(n int) int { seed = seed*1664525 + 1013904223; return int(seed>>8) % n }
	data.Title = "Inventory"
	data.Footer = "generated"
	for i := 0; i < 300; i++ {
		r := Row{ID: i, Name: "product-" + strconv.Itoa(rnd(10000)), Price: float64(rnd(1000000)) / 100, Active: rnd(2) == 0}
		for t := 0; t < rnd(4); t++ {
			r.Tags = append(r.Tags, "t"+strconv.Itoa(rnd(30)))
		}
		data.Rows = append(data.Rows, r)
	}
}

func work() int {
	var buf bytes.Buffer
	sum := 0
	for r := 0; r < 5; r++ {
		buf.Reset()
		if err := tmpl.Execute(&buf, &data); err != nil {
			panic(err)
		}
		b := buf.Bytes()
		sum += len(b) + int(b[len(b)/2])
	}
	return sum
}

func main() { h.Run("tmplrender", 10, work) }
