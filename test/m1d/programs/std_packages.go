// std_packages.go — the next ring of packages from upstream source, printing
// through fmt: bufio, bytes, io, path/filepath, encoding/json (reflect-driven
// marshal and unmarshal), regexp, text/tabwriter, time, sort, strings.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

type Rec struct {
	Name  string         `json:"name"`
	Age   int            `json:"age,omitempty"`
	Tags  []string       `json:"tags"`
	Attrs map[string]any `json:"attrs,omitempty"`
	Sub   *Rec           `json:"sub,omitempty"`
	F     float64        `json:"f"`
}

func main() {
	w := bufio.NewWriter(os.Stdout)
	fmt.Fprintf(w, "buffered %d\n", 1)
	w.Flush()
	sc := bufio.NewScanner(strings.NewReader("l1\nl2 a b\n\nl4"))
	for sc.Scan() {
		fmt.Printf("%q %d\n", sc.Text(), len(strings.Fields(sc.Text())))
	}
	var buf bytes.Buffer
	io.Copy(&buf, io.LimitReader(strings.NewReader("0123456789"), 4))
	fmt.Println(buf.String(), filepath.Join("a", "../b", "c.txt"), filepath.Ext("x.tar.gz"))
	b, err := json.Marshal(Rec{Name: "n", Tags: []string{"a", "b"}, Attrs: map[string]any{"k": 1.5, "z": nil, "a": []int{1}}, Sub: &Rec{Name: "s"}, F: 2.5})
	fmt.Println(string(b), err)
	var r Rec
	err = json.Unmarshal([]byte(`{"name":"x","age":7,"tags":["t"],"attrs":{"a":[1,"b",true,null],"n":{"q":1}},"sub":{"name":"y","f":1e3}}`), &r)
	fmt.Printf("%+v %v %v %v\n", r.Name, r.Age, r.Tags, err)
	keys := make([]string, 0)
	for k := range r.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println(keys, r.Attrs["a"], r.Sub.Name, r.Sub.F)
	var anyv any
	fmt.Println(json.Unmarshal([]byte(`[1,{"a":"b"}]`), &anyv), anyv)
	ib, _ := json.MarshalIndent(map[string]int{"b": 2, "a": 1}, "", "  ")
	fmt.Println(string(ib))
	fmt.Println(json.Unmarshal([]byte(`{bad`), &anyv))
	re := regexp.MustCompile(`(\w+)@(\w+)\.com`)
	fmt.Println(re.FindStringSubmatch("mail bob@example.com now"), re.MatchString("nope"), re.ReplaceAllString("a@b.com c@d.com", "$2:$1"))
	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "a\tbbb\tc")
	fmt.Fprintln(tw, "aaaa\tb\tc")
	tw.Flush()
	d := 90 * time.Minute
	t := time.Date(2026, 11, 20, 10, 30, 0, 0, time.UTC)
	fmt.Println(d, t.Format(time.RFC3339), t.Add(d).Weekday(), t.Unix())
}
