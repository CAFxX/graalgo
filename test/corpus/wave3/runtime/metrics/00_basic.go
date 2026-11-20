package main

// L11 (REVIEW-2026-11-18-05): runtime/metrics — zero corpus imports
// before this program. The metrics registry: every descriptor's
// name, kind, and unit (metadata only — values are live and never
// printed), plus a Read of one sample whose kind is pinned.

import (
	"fmt"
	"runtime/metrics"
	"sort"
)

func main() {
	descs := metrics.All()
	fmt.Printf("descriptors=%d\n", len(descs))
	names := make([]string, 0, len(descs))
	byName := map[string]metrics.Description{}
	for _, d := range descs {
		names = append(names, d.Name)
		byName[d.Name] = d
	}
	sort.Strings(names)
	for _, n := range names {
		d := byName[n]
		fmt.Printf("metric %s kind=%v cumulative=%v\n", n, d.Kind, d.Cumulative)
	}
	// Read one sample: the value itself is live, only its kind is pinned.
	sample := []metrics.Sample{{Name: "/sched/goroutines:goroutines"}}
	metrics.Read(sample)
	fmt.Printf("sample kind=%v\n", sample[0].Value.Kind())
	// A bogus name reads back as KindBad.
	bad := []metrics.Sample{{Name: "/nosuch/metric:bytes"}}
	metrics.Read(bad)
	fmt.Printf("bad-sample kind=%v\n", bad[0].Value.Kind())
}
