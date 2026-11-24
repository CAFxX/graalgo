// stdhot: the standard-library paths of the realistic programs
// (test/bench/realistic), run hot under the pe-check gate (test/m1a/pe_std.sh):
// every function they make hot must compile. The shapes are the ones that
// failed before: goto state machines (regexp's backtracker, jsonwire's number
// scanner, flate's Huffman decoder), slices of arrays of defined types
// (flate's tokens and Huffman encoder), counted loops whose bound is an
// expression (text/template's field chains), plus strings, maps and sorting.
package main

import (
	"bytes"
	"compress/flate"
	"encoding/json"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

type item struct {
	ID    int      `json:"id"`
	Name  string   `json:"name"`
	Price float64  `json:"price"`
	Tags  []string `json:"tags"`
	Inner struct {
		Qty int `json:"qty"`
	} `json:"inner"`
}

type page struct {
	Title string
	Rows  []item
}

var (
	reLine = regexp.MustCompile(`ip=(\d+)\.(\d+)\.(\d+)\.(\d+) latency=(\d+)ms`)
	tmpl   = template.Must(template.New("p").Parse(
		`{{.Title}}{{range .Rows}}<{{.ID}} {{.Name}} {{.Inner.Qty}} {{range .Tags}}{{.}},{{end}}>{{end}}`))
)

func rnd(seed *uint32, n int) int {
	*seed = *seed*1664525 + 1013904223
	return int(*seed>>8) % n
}

func main() {
	seed := uint32(7)
	var items []item
	var lines []string
	var text strings.Builder
	for i := 0; i < 64; i++ {
		it := item{ID: i, Name: "n" + strconv.Itoa(rnd(&seed, 1000)), Price: float64(rnd(&seed, 100000)) / 100}
		it.Inner.Qty = rnd(&seed, 50)
		for t := 0; t < rnd(&seed, 4); t++ {
			it.Tags = append(it.Tags, "t"+strconv.Itoa(rnd(&seed, 9)))
		}
		items = append(items, it)
		lines = append(lines, "ip=10."+strconv.Itoa(rnd(&seed, 256))+".0."+strconv.Itoa(rnd(&seed, 256))+
			" latency="+strconv.Itoa(rnd(&seed, 900))+"ms")
		text.WriteString(strings.Repeat("word"+strconv.Itoa(rnd(&seed, 40))+" ", 1+rnd(&seed, 5)))
	}
	corpus := []byte(strings.Repeat(text.String(), 8))
	sum := 0
	for round := 0; round < 60; round++ {
		// encoding/json both ways.
		b, err := json.Marshal(items)
		if err != nil {
			panic(err)
		}
		var back []item
		if err := json.Unmarshal(b, &back); err != nil {
			panic(err)
		}
		sum += len(b) + back[round%len(back)].Inner.Qty
		// regexp submatches (the backtracker).
		for _, ln := range lines {
			if m := reLine.FindStringSubmatch(ln); m != nil {
				n, _ := strconv.Atoi(m[5])
				sum += n + len(m[1])
			}
		}
		// compress/flate round trip.
		var zb bytes.Buffer
		w, _ := flate.NewWriter(&zb, flate.BestSpeed+round%3)
		w.Write(corpus)
		w.Close()
		out, err := io.ReadAll(flate.NewReader(&zb))
		if err != nil || len(out) != len(corpus) {
			panic("flate round trip")
		}
		sum += zb.Len()
		// text/template with field chains.
		var tb bytes.Buffer
		if err := tmpl.Execute(&tb, page{Title: "t", Rows: items}); err != nil {
			panic(err)
		}
		sum += tb.Len()
		// strings, maps, sorting.
		freq := map[string]int{}
		for _, f := range strings.Fields(text.String()) {
			freq[strings.ToUpper(f)]++
		}
		keys := make([]string, 0, len(freq))
		for k := range freq {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return freq[keys[i]] > freq[keys[j]] || freq[keys[i]] == freq[keys[j]] && keys[i] < keys[j] })
		sum += freq[keys[0]] + len(keys)
	}
	println(sum)
}
