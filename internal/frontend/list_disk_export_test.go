package main

// P1-5 (2026-10-31): container/list retirement test. The synthetic
// list package is deleted; a program importing "container/list" must
// export via the general disk importer from real GOROOT go1.27.1
// sources.
//
// The real and synthetic list sources expose identical function and
// method names (verified by diffing the two sources' funcdecl sets),
// so — unlike the crypto/subtle retirement test — no real-only
// funcdecl name can distinguish them. This test therefore proves
// retirement by composition, and each half is capable of failing:
//   - the syntheticSources assertion fails if the entry is re-added
//     (the program below would then export from the synthetic source
//     again, silently un-retiring the package);
//   - the ExportFile assertions fail if the general disk route does
//     not serve container/list (missing entry + broken disk route
//     surfaces as an export error or missing merged funcdecls).
// Runtime behavior is additionally covered by the wave-3
// container/list differential corpus (test/corpus/wave3/container/list),
// run against gc and graalgo as part of this lane's evidence.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestListDiskExport(t *testing.T) {
	if _, ok := syntheticSources["container/list"]; ok {
		t.Fatalf("container/list still has a syntheticSources entry (retirement reverted?)")
	}
	src := `package main

import "container/list"

func main() {
	l := list.New()
	l.PushBack(1)
	l.PushFront(0)
	l.PushBack(2)
	println(l.Len())
	println(l.Front().Value.(int))
	println(l.Back().Value.(int))
	for e := l.Front(); e != nil; e = e.Next() {
		println(e.Value.(int))
	}
	mid := l.Front().Next()
	l.InsertBefore(99, mid)
	l.InsertAfter(98, mid)
	l.MoveToFront(l.Back())
	l.MoveToBack(l.Front())
	l.MoveAfter(l.Back(), l.Front())
	l.MoveBefore(l.Front(), l.Back())
	l.Remove(mid)
	var zero list.List
	zero.PushBack(7)
	other := list.New()
	other.PushBack(8)
	l.PushFrontList(other)
	l.PushBackList(&zero)
	println(l.Len())
	println(l.Front().Prev() == nil)
	println(l.Back().Next() == nil)
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "list.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					if n, ok := f["name"].(string); ok {
						names[n] = true
					}
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(raw)
	for _, want := range []string{"New", "PushBack", "PushFront", "Remove", "InsertBefore", "InsertAfter", "MoveToFront", "MoveToBack", "PushFrontList", "PushBackList", "Len", "Front", "Back", "Next", "Prev"} {
		if !names[want] {
			t.Errorf("missing funcdecl %s (merged container/list API)", want)
		}
	}
}
