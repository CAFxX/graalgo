package main

// L11 (REVIEW-2026-11-18-05): expvar — zero corpus imports before
// this program. Published process variables: Int, Float, String,
// Map, and a custom Var; sorted iteration via Do; Get and lookup of
// missing names.

import (
	"expvar"
	"fmt"
	"sort"
	"strconv"
)

type customVar struct{ v int }

func (c *customVar) String() string { return strconv.Itoa(c.v * 2) }

func main() {
	i := expvar.NewInt("demo.count")
	i.Add(5)
	i.Add(-2)
	f := expvar.NewFloat("demo.ratio")
	f.Add(1.5)
	s := expvar.NewString("demo.label")
	s.Set("hello")
	m := expvar.NewMap("demo.byname")
	m.Add("alice", 3)
	m.Add("bob", 7)
	m.Add("alice", 1)
	expvar.Publish("demo.custom", &customVar{v: 21})
	// Sorted iteration: Do visits keys in sorted order. Only the
	// demo.* keys are ours; the runtime publishes memstats/cmdline
	// whose values change per run, so they are excluded.
	var keys []string
	expvar.Do(func(kv expvar.KeyValue) {
		if len(kv.Key) >= 5 && kv.Key[:5] == "demo." {
			keys = append(keys, kv.Key+"="+kv.Value.String())
		}
	})
	sort.Strings(keys)
	fmt.Printf("all=%q\n", keys)
	fmt.Printf("count=%s ratio=%s label=%s\n", i.String(), f.String(), s.String())
	fmt.Printf("map-alice=%s\n", m.Get("alice").String())
	fmt.Printf("map-missing=%v\n", m.Get("nosuch") == nil)
	fmt.Printf("get-count=%v\n", expvar.Get("demo.count") != nil)
	fmt.Printf("get-missing=%v\n", expvar.Get("demo.nosuch") == nil)
	// A second NewInt with the same name panics; a different name is fine.
	i2 := expvar.NewInt("demo.other")
	i2.Set(99)
	fmt.Printf("other=%s\n", i2.String())
}
